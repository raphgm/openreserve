package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/openreserve/node/chain"
	"github.com/openreserve/node/types"
)

// health tracks liveness of the node's background loop: the producer's
// ticker, or the replica's last successful sync with its upstream.
type health struct {
	role     string // "producer" or "replica"
	dataDir  string
	lastBeat atomic.Int64 // unix ms of the last healthy loop iteration
	started  time.Time
}

func (h *health) beat() { h.lastBeat.Store(time.Now().UnixMilli()) }

// minFreeBytes below which the node reports unhealthy: a full disk stops
// blocks from being written.
const minFreeBytes = 512 << 20

func (h *health) problems() []string {
	var out []string
	maxAge := 10 * time.Second
	if h.role == "replica" {
		maxAge = 90 * time.Second // long-polls last up to 30s, plus backoff
	}
	if age := time.Since(time.UnixMilli(h.lastBeat.Load())); age > maxAge && time.Since(h.started) > maxAge {
		out = append(out, fmt.Sprintf("%s loop stalled for %s", h.role, age.Round(time.Second)))
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(h.dataDir, &fs); err == nil {
		if free := fs.Bavail * uint64(fs.Bsize); free < minFreeBytes {
			out = append(out, fmt.Sprintf("low disk space: %d MB free", free>>20))
		}
	}
	return out
}

// routes adds /healthz (200 or 503 with reasons) and /metrics (Prometheus
// text format) in front of the API.
func (h *health) routes(c *chain.Chain, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			p := h.problems()
			w.Header().Set("Content-Type", "application/json")
			if len(p) > 0 {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			st := c.Status()
			json.NewEncoder(w).Encode(map[string]any{"ok": len(p) == 0, "problems": p, "role": h.role, "height": st.Height})
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			fmt.Fprint(w, h.metrics(c))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func (h *health) metrics(c *chain.Chain) string {
	st, n := c.Status(), c.Counts()
	var b strings.Builder
	g := func(name, help string, v any, labels ...string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
		if len(labels) > 0 {
			fmt.Fprintf(&b, "%s{%s} %v\n", name, strings.Join(labels, ","), v)
		} else {
			fmt.Fprintf(&b, "%s %v\n", name, v)
		}
	}
	g("openreserve_height", "Current block height.", st.Height)
	tipAge := 0.0
	if st.Height > 0 {
		tipAge = time.Since(time.UnixMilli(st.TipTime)).Seconds()
	}
	g("openreserve_tip_age_seconds", "Seconds since the last block (blocks are only made when there are txs).", tipAge)
	g("openreserve_mempool_size", "Transactions waiting for a block.", st.MempoolSize)
	g("openreserve_txs_total", "Committed transactions.", n.TxsTotal)
	g("openreserve_accounts", "Accounts with state.", st.Accounts)
	g("openreserve_orp_supply", "ORP in existence (whole ORP).", float64(st.Supply)/float64(types.Unit))
	g("openreserve_orp_burned", "ORP burned in fees (whole ORP).", float64(st.Burned)/float64(types.Unit))
	g("openreserve_pools", "Savings pools.", n.Pools)
	g("openreserve_pools_active", "Savings pools in progress.", n.PoolsActive)
	g("openreserve_escrows_open", "Escrows holding funds.", n.EscrowsOpen)
	g("openreserve_escrows_disputed", "Escrows waiting for an arbiter.", n.EscrowsDispute)
	for _, a := range st.Assets {
		fmt.Fprintf(&b, "openreserve_asset_supply{asset=%q} %v\n", a.Symbol, float64(a.Supply)/float64(types.Unit))
	}
	for asset, v := range n.EscrowLocked {
		fmt.Fprintf(&b, "openreserve_escrow_locked{asset=%q} %v\n", types.AssetName(asset), float64(v)/float64(types.Unit))
	}
	up := 1
	if len(h.problems()) > 0 {
		up = 0
	}
	g("openreserve_healthy", "1 if /healthz reports no problems.", up)
	return b.String()
}
