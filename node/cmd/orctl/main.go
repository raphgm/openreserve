// Command orctl is the OpenReserve command-line wallet.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/openreserve/node/chain"
	"github.com/openreserve/node/keys"
	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/mnemonic"
	"github.com/openreserve/node/types"
)

const usage = `orctl - OpenReserve wallet

Usage:
  orctl keygen   -key FILE
  orctl address  -key FILE
  orctl words    -key FILE              show the 24 recovery words for a key
  orctl restore  -key FILE              create a key file from recovery words (read from stdin)
  orctl export-seed -key FILE           print the raw seed, e.g. for ORP_PROPOSER_SEED
  orctl genesis  -chain-id ID -proposer ADDR [-min-fee ORP] [-alloc ADDR=ORP ...]
                 [-asset SYMBOL:ISSUER_ADDR:MIN_FEE[:DECIMALS[:NAME]] ...] > genesis.json
  orctl status
  orctl balance  ADDR | -key FILE
  orctl send     -key FILE -to ADDR -amount ORP [-fee ORP] [-memo TEXT] [-wait]
  orctl tx       ID
  orctl history  ADDR | -key FILE

Commands that talk to a node use -node URL (default $ORP_NODE or http://localhost:8080).
Amounts are in ORP with up to 6 decimals.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "keygen":
		err = keygen(args)
	case "address":
		err = address(args)
	case "words":
		err = wordsCmd(args)
	case "restore":
		err = restore(args)
	case "export-seed":
		err = exportSeed(args)
	case "genesis":
		err = genesis(args)
	case "status":
		err = status(args)
	case "balance":
		err = balance(args)
	case "send":
		err = send(args)
	case "tx":
		err = txCmd(args)
	case "history":
		err = history(args)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func nodeFlag(fs *flag.FlagSet) *string {
	def := os.Getenv("ORP_NODE")
	if def == "" {
		def = "http://localhost:8080"
	}
	return fs.String("node", def, "node API URL")
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	keyPath := fs.String("key", "", "where to write the new key")
	fs.Parse(args)
	if *keyPath == "" {
		return errors.New("-key is required")
	}
	priv, err := keys.Generate(*keyPath)
	if err != nil {
		return err
	}
	fmt.Println(keys.Address(priv))
	return nil
}

func address(args []string) error {
	fs := flag.NewFlagSet("address", flag.ExitOnError)
	keyPath := fs.String("key", "", "key file")
	fs.Parse(args)
	priv, err := keys.Load(*keyPath)
	if err != nil {
		return err
	}
	fmt.Println(keys.Address(priv))
	return nil
}

func loadKeyFlag(name string, args []string) (ed25519.PrivateKey, error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	keyPath := fs.String("key", "", "key file")
	fs.Parse(args)
	return keys.Load(*keyPath)
}

func wordsCmd(args []string) error {
	priv, err := loadKeyFlag("words", args)
	if err != nil {
		return err
	}
	phrase, err := mnemonic.Encode(priv.Seed())
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Anyone with these words can spend this wallet. Write them down; do not share them.")
	for i, w := range strings.Fields(phrase) {
		fmt.Printf("%2d. %-10s", i+1, w)
		if i%4 == 3 {
			fmt.Println()
		}
	}
	return nil
}

func restore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	keyPath := fs.String("key", "", "key file to create")
	fs.Parse(args)
	if *keyPath == "" {
		return errors.New("-key is required")
	}
	fmt.Fprintln(os.Stderr, "Enter the 24 recovery words, then press Ctrl-D:")
	in, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	seed, err := mnemonic.Decode(string(in))
	if err != nil {
		return err
	}
	priv := ed25519.NewKeyFromSeed(seed)
	if err := keys.Write(*keyPath, priv); err != nil {
		return err
	}
	fmt.Println(keys.Address(priv))
	return nil
}

func exportSeed(args []string) error {
	priv, err := loadKeyFlag("export-seed", args)
	if err != nil {
		return err
	}
	fmt.Println(keys.SeedHex(priv))
	return nil
}

type assetFlag []ledger.AssetDef

func (a *assetFlag) String() string { return "" }
func (a *assetFlag) Set(v string) error {
	parts := strings.SplitN(v, ":", 5)
	if len(parts) < 3 {
		return errors.New("expected SYMBOL:ISSUER_ADDR:MIN_FEE[:DECIMALS[:NAME]]")
	}
	fee, err := types.ParseAmount(parts[2])
	if err != nil {
		return err
	}
	def := ledger.AssetDef{Symbol: parts[0], Issuer: types.Address(parts[1]), MinFee: fee, Decimals: 2, Name: parts[0]}
	if len(parts) > 3 {
		if _, err := fmt.Sscanf(parts[3], "%d", &def.Decimals); err != nil {
			return fmt.Errorf("decimals: %w", err)
		}
	}
	if len(parts) > 4 {
		def.Name = parts[4]
	}
	*a = append(*a, def)
	return nil
}

type allocFlag []chain.Allocation

func (a *allocFlag) String() string { return "" }
func (a *allocFlag) Set(v string) error {
	addr, amt, ok := strings.Cut(v, "=")
	if !ok {
		return errors.New("expected ADDR=ORP")
	}
	n, err := types.ParseAmount(amt)
	if err != nil {
		return err
	}
	*a = append(*a, chain.Allocation{Address: types.Address(addr), Amount: n})
	return nil
}

func genesis(args []string) error {
	fs := flag.NewFlagSet("genesis", flag.ExitOnError)
	chainID := fs.String("chain-id", "", "chain id, e.g. openreserve-devnet-1")
	proposer := fs.String("proposer", "", "block proposer address")
	minFee := fs.String("min-fee", "0.001", "minimum tx fee in ORP")
	var allocs allocFlag
	fs.Var(&allocs, "alloc", "initial balance ADDR=ORP (repeatable)")
	var assets assetFlag
	fs.Var(&assets, "asset", "issued asset SYMBOL:ISSUER_ADDR:MIN_FEE[:DECIMALS[:NAME]] (repeatable)")
	fs.Parse(args)
	fee, err := types.ParseAmount(*minFee)
	if err != nil {
		return err
	}
	g := &chain.Genesis{
		ChainID: *chainID, Time: time.Now().UnixMilli(), Proposer: types.Address(*proposer),
		MinFee: fee, Allocations: allocs, Assets: assets,
	}
	if g.Allocations == nil {
		g.Allocations = []chain.Allocation{}
	}
	if err := g.Validate(); err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(g)
}

func status(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	node := nodeFlag(fs)
	fs.Parse(args)
	var st chain.Status
	if err := get(*node, "/v1/status", &st); err != nil {
		return err
	}
	fmt.Printf("chain     %s\nheight    %d\ntip       %s\nsupply    %s ORP\nburned    %s ORP\nmin fee   %s ORP\naccounts  %d\nmempool   %d\n",
		st.ChainID, st.Height, st.TipHash, types.FormatAmount(st.Supply), types.FormatAmount(st.Burned),
		types.FormatAmount(st.MinFee), st.Accounts, st.MempoolSize)
	return nil
}

// addrArg takes an address as a positional arg or derives it from -key.
func addrArg(fs *flag.FlagSet, keyPath string) (types.Address, error) {
	if fs.NArg() == 1 {
		a := types.Address(fs.Arg(0))
		return a, a.Validate()
	}
	if keyPath == "" {
		return "", errors.New("give an address or -key")
	}
	priv, err := keys.Load(keyPath)
	if err != nil {
		return "", err
	}
	return keys.Address(priv), nil
}

type accountResp struct {
	Balance   types.Amount `json:"balance"`
	Nonce     uint64       `json:"nonce"`
	NextNonce uint64       `json:"next_nonce"`
}

func balance(args []string) error {
	fs := flag.NewFlagSet("balance", flag.ExitOnError)
	node := nodeFlag(fs)
	keyPath := fs.String("key", "", "key file")
	fs.Parse(args)
	addr, err := addrArg(fs, *keyPath)
	if err != nil {
		return err
	}
	var acc accountResp
	if err := get(*node, "/v1/accounts/"+string(addr), &acc); err != nil {
		return err
	}
	fmt.Printf("%s ORP (nonce %d)\n", types.FormatAmount(acc.Balance), acc.Nonce)
	return nil
}

func send(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	node := nodeFlag(fs)
	keyPath := fs.String("key", "", "sender key file")
	to := fs.String("to", "", "recipient address")
	amount := fs.String("amount", "", "amount in ORP")
	fee := fs.String("fee", "", "fee in ORP (default: chain minimum)")
	memo := fs.String("memo", "", "optional memo")
	wait := fs.Bool("wait", false, "wait until the tx is in a block")
	fs.Parse(args)

	priv, err := keys.Load(*keyPath)
	if err != nil {
		return err
	}
	amt, err := types.ParseAmount(*amount)
	if err != nil {
		return err
	}
	var st chain.Status
	if err := get(*node, "/v1/status", &st); err != nil {
		return err
	}
	feeAmt := st.MinFee
	if *fee != "" {
		if feeAmt, err = types.ParseAmount(*fee); err != nil {
			return err
		}
	}
	from := keys.Address(priv)
	var acc accountResp
	if err := get(*node, "/v1/accounts/"+string(from), &acc); err != nil {
		return err
	}
	tx := &types.Tx{
		ChainID: st.ChainID, From: from, To: types.Address(*to),
		Amount: amt, Fee: feeAmt, Nonce: acc.NextNonce, Memo: *memo,
	}
	tx.Sign(priv)
	if err := tx.CheckStateless(st.ChainID); err != nil {
		return err
	}
	var res struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := post(*node, "/v1/txs", tx, &res); err != nil {
		return err
	}
	fmt.Printf("tx %s %s\n", res.ID, res.Status)
	if !*wait {
		return nil
	}
	for range 60 {
		var t struct {
			Status   string            `json:"status"`
			Location *chain.TxLocation `json:"location"`
		}
		if err := get(*node, "/v1/txs/"+res.ID, &t); err == nil && t.Status == "committed" {
			fmt.Printf("committed in block %d\n", t.Location.Height)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("timed out waiting for commit")
}

func txCmd(args []string) error {
	fs := flag.NewFlagSet("tx", flag.ExitOnError)
	node := nodeFlag(fs)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: orctl tx ID")
	}
	var raw json.RawMessage
	if err := get(*node, "/v1/txs/"+fs.Arg(0), &raw); err != nil {
		return err
	}
	return printJSON(raw)
}

func history(args []string) error {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	node := nodeFlag(fs)
	keyPath := fs.String("key", "", "key file")
	fs.Parse(args)
	addr, err := addrArg(fs, *keyPath)
	if err != nil {
		return err
	}
	var entries []chain.HistoryEntry
	if err := get(*node, "/v1/accounts/"+string(addr)+"/txs", &entries); err != nil {
		return err
	}
	for _, e := range entries {
		dir, other := "out", e.Tx.To
		if e.Tx.To == addr {
			dir, other = "in ", e.Tx.From
		}
		fmt.Printf("%s  #%-6d %s %12s ORP  %s…  %s\n", time.UnixMilli(e.Time).Format(time.DateTime),
			e.Height, dir, types.FormatAmount(e.Tx.Amount), other[:12], e.Tx.Memo)
	}
	return nil
}

var client = &http.Client{Timeout: 15 * time.Second}

func get(node, path string, out any) error {
	resp, err := client.Get(strings.TrimRight(node, "/") + path)
	if err != nil {
		return err
	}
	return decode(resp, out)
}

func post(node, path string, body, out any) error {
	b, _ := json.Marshal(body)
	resp, err := client.Post(strings.TrimRight(node, "/")+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	return decode(resp, out)
}

func decode(resp *http.Response, out any) error {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("node: %s", e.Error)
		}
		return fmt.Errorf("node returned %s", resp.Status)
	}
	return json.Unmarshal(data, out)
}

func printJSON(raw json.RawMessage) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return err
	}
	fmt.Println(buf.String())
	return nil
}
