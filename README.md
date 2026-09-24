# OpenReserve 🌐

> **The Internet Financial Protocol.**
>
> An open protocol for moving value as seamlessly as the Internet moves information.

[Documentation](docs/) • [Whitepaper](docs/Whitepaper.md) • [Architecture](docs/Architecture.md) • [Roadmap](ROADMAP.md) • [Contributing](CONTRIBUTING.md)

---

# Why OpenReserve?

Today's financial infrastructure was never designed for a globally connected, AI-powered world.

International payments remain slow and expensive. Financial systems are fragmented across banks, mobile money providers, blockchains, payment processors, and national borders. Developers integrate dozens of APIs to build products that should work everywhere.

OpenReserve exists to solve this.

OpenReserve is an **open financial infrastructure protocol** that provides a common settlement layer for people, businesses, governments, banks, fintechs, and AI agents.

Instead of replacing existing financial systems, OpenReserve connects them through a programmable, secure, and interoperable network.

---

# Vision

Our mission is simple.

> Build the financial equivalent of the Internet Protocol.

Just as TCP/IP standardized communication between computers, OpenReserve aims to standardize the movement of value.

Money should be:

- Instant
- Programmable
- Borderless
- Secure
- Offline-capable
- Interoperable
- Open

---

# Core Principles

Every design decision follows these principles.

- Open by default
- Security before speed
- Developer-first
- Institution-ready
- Offline-first
- AI-native
- Community governed
- Long-term sustainability

---

# Architecture

```
                 Applications
──────────────────────────────────────────────

ORPay
Merchant Apps
Bank APIs
Government Services
AI Agents
Wallets

──────────────────────────────────────────────

Developer Platform

REST API
gRPC
SDKs
CLI

──────────────────────────────────────────────

Smart Contract Layer

Native Contracts
WebAssembly (WASM)

──────────────────────────────────────────────

Settlement Layer

Accounts
Transactions
Asset Management
Treasury

──────────────────────────────────────────────

Consensus Layer

Proof of Stake
BFT Finality
Validator Network

──────────────────────────────────────────────

Networking Layer

Peer Discovery
Gossip Protocol
Block Propagation

──────────────────────────────────────────────

Cryptography

Ed25519
Merkle Trees
BLAKE3
Secure Key Management
```

---

# Components

## Core Protocol

Located in `/node`

Responsible for:

- Distributed ledger
- Consensus
- State management
- Networking
- Validator coordination
- Governance
- Cross-chain communication

---

## Wallet SDK

Located in `/wallet`

Features

- BIP39 wallets
- Hardware wallet support
- Multi-signature accounts
- Enterprise custody
- Transaction signing
- Identity abstraction

---

## Consumer Applications

Located in `/apps`

### ORPay

A consumer payment application for sending and receiving digital assets.

Features

- QR payments
- Wallet management
- Live burn tracking
- Transaction history

### ORPScan

A dedicated indexing engine providing fast blockchain search without impacting validator performance.

### Analytics Watchdog

Real-time monitoring for

- Network health
- Validator performance
- Whale activity
- Suspicious transactions
- Sybil detection

---

# Network Features

## High Performance

- Deterministic finality
- Parallel execution
- Concurrent state management

## Security

- Byzantine Fault Tolerance
- Double-spend protection
- Hardware Security Module support
- Smart contract isolation

## Interoperability

- Ethereum compatibility
- Solana compatibility
- Cross-chain asset bridges

## Governance

- On-chain proposals
- Treasury management
- Validator voting

---

# Project Status

What works today is the **ledger core**. Everything else in this README is design direction.

| Area | Status |
|------|--------|
| Accounts, signed transfers (Ed25519), nonces, fee burn | Working |
| Integer amounts (1 ORP = 1,000,000 micro-ORP) | Working |
| Blocks with tx Merkle root and state root, persisted and fsynced | Working |
| Crash recovery (full replay and re-verification on start) | Working |
| Single authorized block producer | Working |
| Replica nodes that re-execute and verify every block | Working |
| REST API and `orctl` CLI wallet | Working |
| ORPay web wallet: @usernames, QR codes and pay links, 24 recovery words | Working |
| Per-IP rate limits, protected key loading, HTTPS deploy kit with backups | Working |
| Multi-validator BFT consensus, P2P gossip | Planned (see [ROADMAP](ROADMAP.md)) |
| VM, bridge, governance, reserve minting | Prototypes in `node/_experimental`, not built |

The chain currently trusts one block producer. That is fine for a devnet or a
closed-loop pilot, but not for a public network.

---

# Getting Started

## Requirements

- Go 1.26+
- Docker (optional)

## Run a local devnet

```bash
make devnet-init   # keys + genesis in ./devnet, alice gets 1,000,000 ORP
make run           # block producer on :8080
```

Then, in another terminal:

```bash
bin/orctl keygen -key bob.json
bin/orctl send -key devnet/alice.json -to $(bin/orctl address -key bob.json) -amount 25.5 -memo "first payment" -wait
bin/orctl balance -key bob.json
bin/orctl history -key bob.json
bin/orctl status
```

Or run a producer plus a verifying replica in Docker:

```bash
make devnet-init
docker compose up --build   # producer on :8080, replica on :8081
```

## Run ORPay (web wallet)

With the node running (`make run`), start these in two more terminals:

```bash
make orpay-backend   # usernames + faucet on :4000
make orpay           # web app on http://localhost:5173
```

Create a wallet, tap **Get 100 test ORP**, claim an @username and send a payment.
ORPay is non-custodial: keys are generated in the browser, encrypted with your
password (PBKDF2 + AES-GCM) and never leave the device. The backend only maps
usernames to addresses, and each claim must be signed by the address's key.

## Deploy

See [deploy/README.md](deploy/README.md) to run the node and ORPay on your own
domain with HTTPS and automatic backups.

## Run the tests

```bash
make test
```

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/status` | Height, tip, state root, supply, min fee |
| GET | `/v1/genesis` | Genesis document |
| GET | `/v1/accounts/{addr}` | Balance, nonce and next usable nonce |
| GET | `/v1/accounts/{addr}/txs` | Committed transfers for an address |
| GET | `/v1/blocks?from=N&limit=L&wait=S` | Block range; `wait` long-polls for new blocks |
| GET | `/v1/blocks/{height}` | One block |
| GET | `/v1/txs/{id}` | Transaction and status (`pending` or `committed`) |
| POST | `/v1/txs` | Submit a signed transaction |

Amounts in the API are integers in micro-ORP. A transaction is signed over a
length-prefixed binary encoding (`types.Tx.SignBytes`); its ID is the SHA-256
of those bytes.

---

# Repository Structure

```
node/
  types/          addresses, amounts, transactions, blocks, signing
  ledger/         account state and transfer rules
  chain/          genesis, block log, mempool, block production and verification
  api/            HTTP API
  keys/           key files
  cmd/openreserved  node binary
  cmd/orctl         CLI wallet
  _experimental/  earlier prototypes (not compiled)
wallet/           wallet prototypes
apps/             ORPay, explorer and analytics prototypes
docs/             whitepaper and design documents
```

---

# Roadmap

## Phase 1

- Whitepaper
- Core Ledger
- Wallet
- Networking

## Phase 2

- Consensus
- Smart Contracts
- Explorer
- SDK

## Phase 3

- Testnet
- Governance
- Mobile Wallet

## Phase 4

- Mainnet
- Cross-chain Bridges
- Institutional APIs

## Phase 5

- AI Payments
- Offline Transactions
- Global Ecosystem

---

# Documentation

- Architecture
- Whitepaper
- Consensus
- Cryptography
- Governance
- SDK
- API Reference

---

# Contributing

OpenReserve is community-driven.

We welcome

- Developers
- Researchers
- Economists
- Security Engineers
- Documentation Writers
- Designers

Please read

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

---

# License

MIT License

---

# Our Mission

We believe value should move across the world as freely as information moves across the Internet.

OpenReserve is building the open financial infrastructure that makes that possible.
