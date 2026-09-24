// Command openreserved runs an OpenReserve node.
//
// A node started with -key matching the genesis proposer produces blocks.
// A node started with -follow URL replicates and re-verifies blocks from
// another node, and forwards submitted transactions to it.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openreserve/node/api"
	"github.com/openreserve/node/chain"
	"github.com/openreserve/node/keys"
	"github.com/openreserve/node/ratelimit"
	"github.com/openreserve/node/types"
)

func main() {
	var (
		dataDir     = flag.String("data", "./data", "data directory")
		genesisPath = flag.String("genesis", "genesis.json", "genesis file")
		keyPath     = flag.String("key", "", "proposer key file (enables block production); or set ORP_PROPOSER_SEED")
		follow      = flag.String("follow", "", "URL of an upstream node to replicate from")
		listen      = flag.String("listen", ":8080", "HTTP API listen address")
		interval    = flag.Duration("block-interval", time.Second, "how often to produce a block")
		readRate    = flag.Int("rate-read", 600, "read requests per minute per client IP")
		submitRate  = flag.Int("rate-submit", 60, "transaction submissions per minute per client IP")
		trustProxy  = flag.Bool("trust-proxy", false, "use X-Forwarded-For for client IPs (only behind a reverse proxy)")
	)
	flag.Parse()
	key, err := keys.Resolve(*keyPath, "ORP_PROPOSER_SEED")
	if err != nil {
		log.Fatal(err)
	}
	if (key == nil) == (*follow == "") {
		log.Fatal("give exactly one of a proposer key (-key or ORP_PROPOSER_SEED) or -follow URL")
	}

	g, err := chain.LoadGenesis(*genesisPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	c, err := chain.Open(*dataDir, g)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	st := c.Status()
	log.Printf("chain %s loaded at height %d, state root %s", st.ChainID, st.Height, st.StateRoot)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler := api.Handler(c)
	h := &health{dataDir: *dataDir, started: time.Now()}
	h.beat()
	if key != nil {
		h.role = "producer"
		if keys.Address(key) != g.Proposer {
			log.Fatalf("key %s is not the genesis proposer %s", keys.Address(key), g.Proposer)
		}
		go produce(ctx, c, key, *interval, h)
	} else {
		h.role = "replica"
		up, err := url.Parse(*follow)
		if err != nil {
			log.Fatal(err)
		}
		handler = forwardSubmits(handler, c, up)
		go replicate(ctx, c, up, h)
	}

	handler = h.routes(c, limit(handler, *readRate, *submitRate, *trustProxy))
	srv := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("API listening on %s", *listen)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func produce(ctx context.Context, c *chain.Chain, key ed25519.PrivateKey, every time.Duration, h *health) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			b, err := c.Produce(key, now.UnixMilli())
			if err != nil {
				log.Printf("produce: %v", err)
				continue
			}
			h.beat()
			if b != nil {
				log.Printf("block %d: %d txs, hash %s", b.Header.Height, len(b.Txs), b.Header.Hash())
			}
		}
	}
}

// replicate long-polls the upstream for new blocks and verifies each one
// locally, so a replica never trusts upstream state it has not re-executed.
func replicate(ctx context.Context, c *chain.Chain, up *url.URL, h *health) {
	client := &http.Client{Timeout: 40 * time.Second}
	backoff := time.Second
	for ctx.Err() == nil {
		u := up.JoinPath("/v1/blocks")
		u.RawQuery = fmt.Sprintf("from=%d&limit=100&wait=30", c.Height()+1)
		blocks, err := fetchBlocks(ctx, client, u.String())
		if err == nil {
			for _, b := range blocks {
				if err = c.AddBlock(b); err != nil {
					err = fmt.Errorf("block %d rejected: %w", b.Header.Height, err)
					break
				}
				log.Printf("replicated block %d", b.Header.Height)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("replicate: %v (retrying in %s)", err, backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		h.beat()
	}
}

func fetchBlocks(ctx context.Context, client *http.Client, u string) ([]*types.Block, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned %s", resp.Status)
	}
	var blocks []*types.Block
	return blocks, json.NewDecoder(resp.Body).Decode(&blocks)
}

// forwardSubmits sends POST /v1/txs to the upstream producer; everything
// else is answered from the replica's own verified state. Accepted txs are
// also kept in the local mempool so next_nonce and pending lookups work.
func forwardSubmits(local http.Handler, c *chain.Chain, up *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(up)
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusAccepted {
			if tx, ok := resp.Request.Context().Value(txKey{}).(*types.Tx); ok {
				c.Submit(tx) // best effort; the producer is authoritative
			}
		}
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/txs" {
			local.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			http.Error(w, `{"error":"request too large"}`, http.StatusRequestEntityTooLarge)
			return
		}
		var tx types.Tx
		if json.Unmarshal(body, &tx) == nil {
			r = r.WithContext(context.WithValue(r.Context(), txKey{}, &tx))
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		proxy.ServeHTTP(w, r)
	})
}

type txKey struct{}

// limit applies a strict per-IP budget to submissions and a looser one to reads.
func limit(next http.Handler, readPerMin, submitPerMin int, trustProxy bool) http.Handler {
	reads := ratelimit.New(readPerMin, max(readPerMin/5, 1))
	submits := ratelimit.New(submitPerMin, max(submitPerMin/3, 1))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l := reads
		if r.Method == http.MethodPost {
			l = submits
		}
		if ok, wait := l.Allow(ratelimit.ClientIP(r, trustProxy)); !ok {
			ratelimit.TooMany(w, wait)
			return
		}
		next.ServeHTTP(w, r)
	})
}
