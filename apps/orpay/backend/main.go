// Command orpay-backend serves the ORPay username directory and a devnet faucet.
//
// ORPay is non-custodial: keys live in the user's browser and transactions
// go straight to the node. This service only maps @usernames to addresses
// (each registration is signed by the address's key) and, on devnets, hands
// out test ORP.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/openreserve/node/keys"
	"github.com/openreserve/node/ratelimit"
	"github.com/openreserve/node/types"
)

func main() {
	var (
		listen     = flag.String("listen", ":4000", "listen address")
		dbPath     = flag.String("db", "orpay-users.json", "username directory file")
		nodeURL    = flag.String("node", "http://localhost:8080", "OpenReserve node API")
		faucetKey  = flag.String("faucet-key", "", "key file funding the devnet faucet; or set ORPAY_FAUCET_SEED (disabled if neither)")
		faucetAmt  = flag.String("faucet-amount", "100", "ORP per faucet request")
		faucetWait = flag.Duration("faucet-cooldown", time.Hour, "minimum time between faucet requests per address")
		trustProxy = flag.Bool("trust-proxy", false, "use X-Forwarded-For for client IPs (only behind a reverse proxy)")
	)
	flag.Parse()

	dir, err := openDirectory(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	s := &server{dir: dir, node: strings.TrimRight(*nodeURL, "/")}
	key, err := keys.Resolve(*faucetKey, "ORPAY_FAUCET_SEED")
	if err != nil {
		log.Fatal(err)
	}
	if key != nil {
		amt, err := types.ParseAmount(*faucetAmt)
		if err != nil {
			log.Fatal(err)
		}
		s.faucet = &faucet{key: key, amount: amt, cooldown: *faucetWait, last: map[types.Address]time.Time{}}
		log.Printf("faucet enabled: %s ORP from %s", types.FormatAmount(amt), keys.Address(key))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("GET /api/users/{name}", s.resolve)
	mux.HandleFunc("GET /api/addresses/{addr}", s.lookup)
	// Per-IP limits: signups and faucet requests are the abuse targets.
	mux.Handle("POST /api/users", ratelimit.Middleware(ratelimit.New(10, 3), *trustProxy, http.HandlerFunc(s.register)))
	mux.Handle("POST /api/faucet", ratelimit.Middleware(ratelimit.New(5, 2), *trustProxy, http.HandlerFunc(s.drip)))

	log.Printf("ORPay backend listening on %s (node %s)", *listen, s.node)
	handler := ratelimit.Middleware(ratelimit.New(300, 60), *trustProxy, mux)
	srv := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

type server struct {
	dir    *directory
	node   string
	faucet *faucet
}

func (s *server) config(w http.ResponseWriter, _ *http.Request) {
	cfg := map[string]any{"faucet": s.faucet != nil}
	if s.faucet != nil {
		cfg["faucet_amount"] = s.faucet.amount
	}
	writeJSON(w, http.StatusOK, cfg)
}

var usernameRe = regexp.MustCompile(`^[a-z0-9_]{3,20}$`)

// RegisterMessage is what the wallet signs to claim a username.
func RegisterMessage(name string, addr types.Address) string {
	return fmt.Sprintf("orpay/register/v1\n%s\n%s", name, addr)
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string        `json:"username"`
		Address  types.Address `json:"address"`
		Sig      string        `json:"sig"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.Username = strings.ToLower(req.Username)
	if !usernameRe.MatchString(req.Username) {
		writeErr(w, http.StatusBadRequest, errors.New("username must be 3-20 characters: a-z, 0-9, _"))
		return
	}
	pub, err := req.Address.PubKey()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sig, err := hex.DecodeString(req.Sig)
	if err != nil || !ed25519.Verify(pub, []byte(RegisterMessage(req.Username, req.Address)), sig) {
		writeErr(w, http.StatusUnauthorized, errors.New("signature does not match address"))
		return
	}
	if err := s.dir.claim(req.Username, req.Address); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"username": req.Username, "address": req.Address})
}

func (s *server) resolve(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimPrefix(r.PathValue("name"), "@"))
	addr, ok := s.dir.byName(name)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no user @%s", name))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": name, "address": addr})
}

func (s *server) lookup(w http.ResponseWriter, r *http.Request) {
	addr := types.Address(r.PathValue("addr"))
	// An address without a username is a normal case, not an error, so
	// respond 200 with a null username.
	var username any
	if name, ok := s.dir.byAddress(addr); ok {
		username = name
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": username, "address": addr})
}

// directory is a username <-> address map persisted as JSON. Each address
// may hold one username and names are first come, first served.
type directory struct {
	mu     sync.RWMutex
	path   string
	users  map[string]types.Address
	byAddr map[types.Address]string
}

func openDirectory(path string) (*directory, error) {
	d := &directory{path: path, users: map[string]types.Address{}, byAddr: map[types.Address]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &d.users); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for name, addr := range d.users {
		d.byAddr[addr] = name
	}
	return d, nil
}

func (d *directory) claim(name string, addr types.Address) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if owner, ok := d.users[name]; ok {
		if owner == addr {
			return nil
		}
		return fmt.Errorf("@%s is taken", name)
	}
	if existing, ok := d.byAddr[addr]; ok {
		return fmt.Errorf("this wallet is already @%s", existing)
	}
	d.users[name] = addr
	d.byAddr[addr] = name
	if err := d.save(); err != nil {
		delete(d.users, name)
		delete(d.byAddr, addr)
		return err
	}
	return nil
}

// save writes atomically so a crash never leaves a half-written file.
func (d *directory) save() error {
	data, _ := json.MarshalIndent(d.users, "", "  ")
	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".orpay-users-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	return os.Rename(tmp.Name(), d.path)
}

func (d *directory) byName(name string) (types.Address, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	a, ok := d.users[name]
	return a, ok
}

func (d *directory) byAddress(addr types.Address) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	n, ok := d.byAddr[addr]
	return n, ok
}

// faucet sends test ORP from a funded devnet key.
type faucet struct {
	mu       sync.Mutex
	key      ed25519.PrivateKey
	amount   types.Amount
	cooldown time.Duration
	last     map[types.Address]time.Time
}

func (s *server) drip(w http.ResponseWriter, r *http.Request) {
	f := s.faucet
	if f == nil {
		writeErr(w, http.StatusNotFound, errors.New("faucet is disabled on this network"))
		return
	}
	var req struct {
		Address types.Address `json:"address"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := req.Address.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	// Serialize faucet sends so nonces never collide.
	f.mu.Lock()
	defer f.mu.Unlock()
	if wait := f.cooldown - time.Since(f.last[req.Address]); wait > 0 {
		writeErr(w, http.StatusTooManyRequests, fmt.Errorf("try again in %s", wait.Round(time.Minute)))
		return
	}
	id, err := s.send(f.key, req.Address, f.amount, "ORPay faucet")
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	f.last[req.Address] = time.Now()
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "amount": f.amount})
}

func (s *server) send(key ed25519.PrivateKey, to types.Address, amt types.Amount, memo string) (string, error) {
	from := keys.Address(key)
	var st struct {
		ChainID string       `json:"chain_id"`
		MinFee  types.Amount `json:"min_fee"`
	}
	if err := s.getJSON("/v1/status", &st); err != nil {
		return "", err
	}
	var acc struct {
		NextNonce uint64 `json:"next_nonce"`
	}
	if err := s.getJSON("/v1/accounts/"+string(from), &acc); err != nil {
		return "", err
	}
	tx := &types.Tx{ChainID: st.ChainID, From: from, To: to, Amount: amt, Fee: st.MinFee, Nonce: acc.NextNonce, Memo: memo}
	tx.Sign(key)
	body, _ := json.Marshal(tx)
	resp, err := http.Post(s.node+"/v1/txs", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("node unreachable: %w", err)
	}
	defer resp.Body.Close()
	var res struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&res)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("node rejected faucet tx: %s", res.Error)
	}
	return res.ID, nil
}

func (s *server) getJSON(path string, out any) error {
	resp, err := http.Get(s.node + path)
	if err != nil {
		return fmt.Errorf("node unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node returned %s for %s", resp.Status, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
