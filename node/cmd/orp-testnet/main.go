// Command orp-testnet generates a multi-validator OpenReserve network run by
// CometBFT: one directory per validator (CometBFT keys and config), a shared
// CometBFT genesis listing the validators, and the OpenReserve genesis.
//
//	orp-testnet -n 4 -out testnet            # all nodes on this machine
//	orp-testnet -n 4 -out testnet -docker    # hostnames node0..node3 for docker compose
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/openreserve/node/chain"
	"github.com/openreserve/node/keys"
	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

func main() {
	n := flag.Int("n", 4, "number of validators (4 tolerates 1 faulty validator)")
	out := flag.String("out", "testnet", "output directory")
	chainID := flag.String("chain-id", "openreserve-testnet-1", "chain id")
	docker := flag.Bool("docker", false, "address peers as node0..nodeN (docker compose) instead of 127.0.0.1")
	flag.Parse()
	if *n < 1 {
		log.Fatal("-n must be at least 1")
	}
	if _, err := os.Stat(*out); err == nil {
		log.Fatalf("%s already exists; remove it to start over", *out)
	}

	type val struct {
		home   string
		id     p2p.ID
		p2p    int
		rpc    int
		api    int
		pubKey cmttypes.GenesisValidator
	}
	vals := make([]val, *n)
	for i := range vals {
		home := filepath.Join(*out, fmt.Sprintf("node%d", i))
		for _, d := range []string{"config", "data"} {
			must(os.MkdirAll(filepath.Join(home, d), 0o700))
		}
		conf := cmtcfg.DefaultConfig()
		conf.SetRoot(home)
		pv := privval.GenFilePV(conf.PrivValidatorKeyFile(), conf.PrivValidatorStateFile())
		pv.Save()
		nk, err := p2p.LoadOrGenNodeKey(conf.NodeKeyFile())
		must(err)
		pub, err := pv.GetPubKey()
		must(err)
		vals[i] = val{home: home, id: nk.ID(), p2p: 26656 + 10*i, rpc: 26657 + 10*i, api: 8080 + i,
			pubKey: cmttypes.GenesisValidator{Address: pub.Address(), PubKey: pub, Power: 10, Name: fmt.Sprintf("node%d", i)}}
		if *docker {
			vals[i].p2p, vals[i].rpc, vals[i].api = 26656, 26657, 8080
		}
	}

	gv := make([]cmttypes.GenesisValidator, *n)
	for i, v := range vals {
		gv[i] = v.pubKey
	}
	cgen := &cmttypes.GenesisDoc{GenesisTime: time.Now().UTC(), ChainID: *chainID, InitialHeight: 1, Validators: gv,
		ConsensusParams: cmttypes.DefaultConsensusParams()}
	must(cgen.ValidateAndComplete())

	for i, v := range vals {
		conf := cmtcfg.DefaultConfig()
		conf.SetRoot(v.home)
		conf.Moniker = fmt.Sprintf("node%d", i)
		conf.P2P.ListenAddress = fmt.Sprintf("tcp://0.0.0.0:%d", v.p2p)
		conf.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", v.rpc)
		conf.P2P.AddrBookStrict = false
		conf.P2P.AllowDuplicateIP = true
		var peers []string
		for j, p := range vals {
			if j == i {
				continue
			}
			host := "127.0.0.1"
			if *docker {
				host = fmt.Sprintf("node%d", j)
			}
			peers = append(peers, fmt.Sprintf("%s@%s:%d", p.id, host, p.p2p))
		}
		conf.P2P.PersistentPeers = strings.Join(peers, ",")
		// Blocks only when there are transactions; commit ~1s after agreement.
		conf.Consensus.CreateEmptyBlocks = false
		conf.Consensus.TimeoutCommit = time.Second
		cmtcfg.WriteConfigFile(filepath.Join(v.home, "config", "config.toml"), conf)
		must(cgen.SaveAs(filepath.Join(v.home, "config", "genesis.json")))
	}

	// OpenReserve genesis: consensus by CometBFT, a funded demo wallet and
	// the NGN asset with its issuer.
	must(os.MkdirAll(filepath.Join(*out, "keys"), 0o700))
	alice, err := keys.Generate(filepath.Join(*out, "keys", "alice.json"))
	must(err)
	issuer, err := keys.Generate(filepath.Join(*out, "keys", "issuer.json"))
	must(err)
	g := &chain.Genesis{
		ChainID: *chainID, Time: time.Now().UnixMilli(), Consensus: chain.ConsensusCometBFT, MinFee: 1000,
		Allocations: []chain.Allocation{{Address: keys.Address(alice), Amount: 1_000_000 * types.Unit}},
		Assets:      []ledger.AssetDef{{Symbol: "NGN", Name: "Nigerian naira", Issuer: keys.Address(issuer), MinFee: 20 * types.Unit, Decimals: 2}},
	}
	must(g.Validate())
	b, _ := json.MarshalIndent(g, "", "  ")
	must(os.WriteFile(filepath.Join(*out, "genesis.json"), append(b, '\n'), 0o644))

	fmt.Printf("Generated %d validators in %s/ (chain %s)\n", *n, *out, *chainID)
	for i, v := range vals {
		fmt.Printf("  node%d: p2p %d, API :%d  ->  openreserved -genesis %s/genesis.json -data %s/orp -cometbft-home %s -listen :%d\n",
			i, v.p2p, v.api, *out, v.home, v.home, v.api)
	}
	fmt.Printf("Demo wallet: %s/keys/alice.json (1,000,000 ORP)\n", *out)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
