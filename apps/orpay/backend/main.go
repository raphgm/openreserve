// Command orpay-backend serves ORPay's off-chain services: the @username
// directory, merchant invoices with webhooks, and a devnet faucet.
//
// ORPay is non-custodial: keys live in the user's wallet and every payment
// goes straight to the node. This service never holds user funds; savings
// pools live on-chain. Invoice payment status is read back from the chain.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openreserve/node/client"
	"github.com/openreserve/node/jsonstore"
	"github.com/openreserve/node/keys"
	"github.com/openreserve/node/ratelimit"
	"github.com/openreserve/node/reqauth"
	"github.com/openreserve/node/types"
)

func main() {
	var (
		listen        = flag.String("listen", ":4000", "listen address")
		dbPath        = flag.String("db", "orpay-users.json", "username directory file; other state files live beside it")
		nodeURL       = flag.String("node", "http://localhost:8080", "OpenReserve node API")
		faucetKey     = flag.String("faucet-key", "", "key file funding the devnet faucet; or set ORPAY_FAUCET_SEED (disabled if neither)")
		faucetAmt     = flag.String("faucet-amount", "100", "ORP per faucet request")
		faucetWait    = flag.Duration("faucet-cooldown", time.Hour, "minimum time between faucet requests per address")
		trustProxy    = flag.Bool("trust-proxy", false, "use X-Forwarded-For for client IPs (only behind a reverse proxy)")
		privateHooks  = flag.Bool("allow-private-webhooks", false, "allow webhooks to private/loopback addresses (development only)")
		watchInterval = flag.Duration("watch-interval", 3*time.Second, "how often to check pending invoices for payment")
		admins        = flag.String("admins", os.Getenv("ORPAY_ADMINS"), "comma-separated wallet addresses allowed to approve partner apps")
		publicURL     = flag.String("public-url", "http://localhost:5173", "public URL of the ORPay web app (used in checkout links)")
		defaultCur    = flag.String("default-currency", "ORP", "currency for checkouts that do not name one (e.g. NGN)")
		cardPayments  = flag.Bool("card-payments", false, "offer card/bank payment via the Paystack gateway on checkouts in the default currency")
	)
	flag.Parse()

	s, err := newServer(serverConfig{
		usersPath:     *dbPath,
		stateDir:      filepath.Dir(*dbPath),
		node:          client.New(*nodeURL),
		privateHooks:  *privateHooks,
		faucetAmount:  *faucetAmt,
		faucetWait:    *faucetWait,
		faucetKeyPath: *faucetKey,
		admins:        *admins,
		publicURL:     *publicURL,
		defaultCur:    *defaultCur,
		cardPayments:  *cardPayments,
	})
	if err != nil {
		log.Fatal(err)
	}
	go s.watchInvoices(*watchInterval)
	go s.refreshLogos(24 * time.Hour)

	handler := ratelimit.Middleware(ratelimit.New(300, 60), *trustProxy, s.routes(*trustProxy))
	log.Printf("ORPay backend listening on %s (node %s)", *listen, *nodeURL)
	srv := &http.Server{Addr: *listen, Handler: cors(handler), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

type serverConfig struct {
	usersPath, stateDir string
	node                *client.Client
	privateHooks        bool
	faucetAmount        string
	faucetWait          time.Duration
	faucetKeyPath       string
	admins              string
	publicURL           string
	defaultCur          string
	cardPayments        bool
}

type server struct {
	dir          *directory
	node         *client.Client
	faucet       *faucet
	invoices     *jsonstore.Store[map[string]*Invoice]
	apps         *jsonstore.Store[map[string]*App]
	escrows      *jsonstore.Store[map[string]*EscrowRequest]
	terms        *jsonstore.Store[map[string]*Terms]
	chats        *jsonstore.Store[map[string][]*EscrowMessage]
	drafts       *jsonstore.Store[map[string]*PoolDraft]
	secret       []byte
	stateDir     string
	admins       []types.Address
	publicURL    string
	privateHooks bool
	defaultAsset string
	cardPayments bool
	hookClient   *http.Client
	now          func() time.Time
}

func newServer(cfg serverConfig) (*server, error) {
	if err := os.MkdirAll(cfg.stateDir, 0o755); err != nil {
		return nil, err
	}
	dir, err := openDirectory(cfg.usersPath)
	if err != nil {
		return nil, err
	}
	invoices, err := jsonstore.Open(filepath.Join(cfg.stateDir, "orpay-invoices.json"), map[string]*Invoice{})
	if err != nil {
		return nil, err
	}
	apps, err := jsonstore.Open(filepath.Join(cfg.stateDir, "orpay-apps.json"), map[string]*App{})
	if err != nil {
		return nil, err
	}
	escrows, err := jsonstore.Open(filepath.Join(cfg.stateDir, "orpay-escrows.json"), map[string]*EscrowRequest{})
	if err != nil {
		return nil, err
	}
	terms, err := jsonstore.Open(filepath.Join(cfg.stateDir, "orpay-terms.json"), map[string]*Terms{})
	if err != nil {
		return nil, err
	}
	chats, err := jsonstore.Open(filepath.Join(cfg.stateDir, "orpay-escrow-chat.json"), map[string][]*EscrowMessage{})
	if err != nil {
		return nil, err
	}
	drafts, err := jsonstore.Open(filepath.Join(cfg.stateDir, "orpay-ajo-invites.json"), map[string]*PoolDraft{})
	if err != nil {
		return nil, err
	}
	secret, err := loadSecret(cfg.stateDir)
	if err != nil {
		return nil, err
	}
	s := &server{
		terms: terms, chats: chats, secret: secret, drafts: drafts,
		dir: dir, node: cfg.node, invoices: invoices, apps: apps, escrows: escrows, now: time.Now, stateDir: cfg.stateDir,
		publicURL: strings.TrimRight(cfg.publicURL, "/"), privateHooks: cfg.privateHooks,
		hookClient: webhookClient(cfg.privateHooks),
	}
	if c := strings.ToUpper(cfg.defaultCur); c != "" && c != "ORP" {
		if !types.ValidAsset(c) {
			return nil, fmt.Errorf("invalid default currency %q", cfg.defaultCur)
		}
		s.defaultAsset = c
	}
	s.cardPayments = cfg.cardPayments
	for _, a := range strings.Split(cfg.admins, ",") {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		if err := types.Address(a).Validate(); err != nil {
			return nil, fmt.Errorf("admin %q: %w", a, err)
		}
		s.admins = append(s.admins, types.Address(a))
	}
	key, err := keys.Resolve(cfg.faucetKeyPath, "ORPAY_FAUCET_SEED")
	if err != nil {
		return nil, err
	}
	if key != nil {
		amt, err := types.ParseAmount(cfg.faucetAmount)
		if err != nil {
			return nil, err
		}
		s.faucet, err = newFaucet(key, amt, cfg.faucetWait, filepath.Join(cfg.stateDir, "orpay-faucet.json"))
		if err != nil {
			return nil, err
		}
		log.Printf("faucet enabled: %s ORP from %s", types.FormatAmount(amt), keys.Address(key))
	}
	return s, nil
}

func (s *server) routes(trustProxy bool) http.Handler {
	strict := func(perMin, burst int, h http.HandlerFunc) http.Handler {
		return ratelimit.Middleware(ratelimit.New(perMin, burst), trustProxy, h)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.config)

	mux.HandleFunc("GET /api/users/{name}", s.resolve)
	mux.HandleFunc("GET /api/addresses/{addr}", s.lookup)
	mux.Handle("POST /api/users", strict(10, 3, s.register))

	mux.Handle("POST /api/faucet", strict(5, 2, s.drip))

	// Partner apps: request access, admin review, keys and settings.
	mux.Handle("POST /api/apps", strict(5, 2, reqauth.Signed(s.requestApp)))
	mux.HandleFunc("GET /api/apps", reqauth.Signed(s.listApps))
	mux.HandleFunc("GET /api/apps/{id}", s.publicApp)
	mux.HandleFunc("GET /api/apps/{id}/logo", s.serveLogo)
	mux.Handle("POST /api/apps/{id}/logo", strict(10, 3, reqauth.Signed(s.refreshLogoHandler)))
	mux.Handle("POST /api/apps/{id}/review", strict(30, 10, reqauth.Signed(s.reviewApp)))
	mux.Handle("POST /api/apps/{id}/keys", strict(10, 3, reqauth.Signed(s.rotateKey)))
	mux.Handle("POST /api/apps/{id}/settings", strict(30, 10, reqauth.Signed(s.updateApp)))

	// Server-to-server API for approved apps (API key auth).
	mux.Handle("POST /api/v1/checkout", strict(120, 40, s.apiKeyAuth(s.createCheckout)))
	mux.HandleFunc("GET /api/v1/checkout", s.apiKeyAuth(s.listCheckouts))
	mux.HandleFunc("GET /api/v1/checkout/{id}", s.apiKeyAuth(s.getCheckout))

	// Escrow requests: an app asks a buyer to lock funds (e.g. a developer's
	// payout held until work is approved).
	mux.Handle("POST /api/v1/escrows", strict(120, 40, s.apiKeyAuth(s.createEscrowRequest)))
	mux.HandleFunc("GET /api/v1/escrows", s.apiKeyAuth(s.listAppEscrows))
	mux.HandleFunc("GET /api/v1/escrows/{id}", s.apiKeyAuth(s.getAppEscrow))
	mux.HandleFunc("GET /api/escrow-requests/{id}", s.getEscrowRequest)

	// Escrow terms (no returns: buyers accept terms before funding), and a
	// private buyer/seller/arbiter conversation with photo evidence.
	mux.Handle("POST /api/escrow-terms", strict(30, 10, http.HandlerFunc(s.postTerms)))
	mux.HandleFunc("GET /api/escrow-terms/{hash}", s.getTerms)
	mux.Handle("POST /api/escrows/{id}/messages", strict(30, 10, reqauth.SignedN(16<<20, s.postEscrowMessage)))
	mux.HandleFunc("GET /api/escrows/{id}/messages", reqauth.Signed(s.listEscrowMessages))
	mux.HandleFunc("GET /api/escrow-files/{id}", s.serveEvidence)

	// Ajo invite links: join a pool before it exists on-chain.
	mux.Handle("POST /api/ajo-invites", strict(20, 5, reqauth.Signed(s.createDraft)))
	mux.HandleFunc("GET /api/ajo-invites", reqauth.Signed(s.myDrafts))
	mux.HandleFunc("GET /api/ajo-invites/{id}", s.getDraft)
	mux.Handle("POST /api/ajo-invites/{id}/join", strict(30, 10, reqauth.Signed(s.joinDraft)))
	mux.Handle("POST /api/ajo-invites/{id}/leave", strict(30, 10, reqauth.Signed(s.leaveDraft)))
	mux.Handle("POST /api/ajo-invites/{id}/order", strict(30, 10, reqauth.Signed(s.orderDraft)))
	mux.Handle("POST /api/ajo-invites/{id}/started", strict(30, 10, reqauth.Signed(s.startedDraft)))

	// Public invoice view for the hosted checkout page and receipts.
	mux.HandleFunc("GET /api/invoices/{id}", s.getInvoice)
	return mux
}

func (s *server) config(w http.ResponseWriter, _ *http.Request) {
	cfg := map[string]any{"faucet": s.faucet != nil}
	if s.faucet != nil {
		cfg["faucet_amount"] = s.faucet.amount
	}
	writeJSON(w, http.StatusOK, cfg)
}

func decodeBody(r *http.Request, v any) error { return decodeBodyN(r, v, 16<<10) }

func decodeBodyN(r *http.Request, v any, limit int64) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, limit))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// cors lets the native (Capacitor) app and other origins call the API. Auth
// is by per-request signatures, not cookies, so a wildcard origin is safe.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-ORP-Address, X-ORP-Time, X-ORP-Sig")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
