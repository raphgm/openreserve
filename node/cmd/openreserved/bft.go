package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	cmtnode "github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	rpclocal "github.com/cometbft/cometbft/rpc/client/local"
	"github.com/spf13/viper"

	"github.com/openreserve/node/abci"
	"github.com/openreserve/node/chain"
)

// startCometBFT runs a CometBFT node in-process with the OpenReserve
// application. It returns an HTTP middleware that routes submitted txs into
// CometBFT's mempool (gossiped to every validator) and a stop function.
func startCometBFT(home string, c *chain.Chain, h *health) (func(http.Handler) http.Handler, func(), error) {
	conf := cmtcfg.DefaultConfig()
	v := viper.New()
	v.SetConfigFile(filepath.Join(home, "config", "config.toml"))
	if err := v.ReadInConfig(); err != nil {
		return nil, nil, fmt.Errorf("read CometBFT config: %w", err)
	}
	if err := v.Unmarshal(conf); err != nil {
		return nil, nil, err
	}
	conf.SetRoot(home)
	if err := conf.ValidateBasic(); err != nil {
		return nil, nil, fmt.Errorf("CometBFT config: %w", err)
	}
	nodeKey, err := p2p.LoadNodeKey(conf.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}
	logger := cmtlog.NewFilter(cmtlog.NewTMLogger(cmtlog.NewSyncWriter(os.Stderr)), cmtlog.AllowError())
	n, err := cmtnode.NewNode(conf,
		privval.LoadOrGenFilePV(conf.PrivValidatorKeyFile(), conf.PrivValidatorStateFile()),
		nodeKey, proxy.NewLocalClientCreator(abci.New(c)),
		cmtnode.DefaultGenesisDocProviderFunc(conf), cmtcfg.DefaultDBProvider,
		cmtnode.DefaultMetricsProvider(conf.Instrumentation), logger)
	if err != nil {
		return nil, nil, fmt.Errorf("start CometBFT: %w", err)
	}
	if err := n.Start(); err != nil {
		return nil, nil, err
	}
	log.Printf("CometBFT node %s running (p2p %s)", nodeKey.ID(), conf.P2P.ListenAddress)
	rpc := rpclocal.New(n)

	// Liveness: the consensus engine is running. Height advances only when
	// there are transactions (empty blocks are off by default).
	go func() {
		for range time.Tick(3 * time.Second) {
			if n.IsRunning() {
				h.beat()
			}
		}
	}()

	submit := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/v1/txs" {
				next.ServeHTTP(w, r)
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
			if err != nil {
				httpErr(w, http.StatusBadRequest, err)
				return
			}
			tx, err := abci.DecodeTx(body)
			if err != nil {
				httpErr(w, http.StatusBadRequest, err)
				return
			}
			// Track it locally so next_nonce and tx lookups see it as pending.
			id, err := c.Submit(tx)
			dup := errors.Is(err, chain.ErrDuplicate)
			if err != nil && !dup {
				httpErr(w, http.StatusUnprocessableEntity, err)
				return
			}
			raw, _ := json.Marshal(tx)
			res, err := rpc.BroadcastTxSync(r.Context(), raw)
			switch {
			case err != nil && strings.Contains(err.Error(), "already exists in cache"):
				dup = true
			case err != nil:
				c.Forget(id)
				httpErr(w, http.StatusServiceUnavailable, fmt.Errorf("consensus unavailable: %w", err))
				return
			case res.Code != 0:
				c.Forget(id)
				httpErr(w, http.StatusUnprocessableEntity, errors.New(res.Log))
				return
			}
			status, code := "pending", http.StatusAccepted
			if dup {
				status, code = "duplicate", http.StatusOK
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(map[string]any{"id": id, "status": status})
		})
	}
	stop := func() {
		n.Stop()
		n.Wait()
	}
	return submit, stop, nil
}

func httpErr(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
