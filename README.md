# OpenReserve 🌐

> The Internet Financial Protocol. Built for speed, security, and global scale.

OpenReserve is a next-generation, high-performance Layer 1 blockchain protocol written entirely in Go. Designed to function as the base layer for global finance, it features a custom Byzantine Fault Tolerant (BFT) consensus engine, natively integrated Smart Contracts, Institutional Hardware Custody, and a suite of consumer applications that drive value back to the network.

---

## 🏗️ Architecture & Features

### Core Protocol (`/node`)
- **Ledger State**: A highly concurrent, thread-safe `sync.RWMutex` state machine.
- **P2P Networking**: Custom TCP connection engine with a highly efficient Gossip protocol for block propagation.
- **BFT Consensus Engine**: Proof-of-Stake (PoS) validator voting, preventing Sybil attacks and ensuring deterministic finality.
- **Smart Contract VM**: Supports both ultra-fast Native Contracts and dynamic WebAssembly (WASM) execution.
- **Governance & DAO**: On-chain voting mechanism allowing token holders to control the Protocol Treasury.
- **Cross-Chain Bridges**: Built-in cryptographic "Lock-and-Mint" relay mechanisms to interoperate with Ethereum (EVM) and Solana.

### Security & Auditing (`/tests/audit`)
- Mathematically proven protection against High-Concurrency Double-Spend Race Conditions.
- Automated Sybil defense mechanisms at the consensus layer.
- Comprehensive Smart Contract fuzzing architecture.

### Developer SDK (`/wallet`)
- **Universal Signers**: Support for hot (in-memory) keys and cold Hardware Security Modules (HSMs).
- **Identity Abstraction**: Native support for Enterprise `M-of-N` Multi-Sig accounts.
- **Key Generation**: Full BIP39 Mnemonic Seed generation and Ed25519 cryptography.

### Ecosystem (`/apps`)
- **ORPay**: A sleek, consumer-facing Vite/JS web app functioning as the primary gateway, featuring live network burn tracking.
- **ORPScan Block Explorer**: A custom, standalone Indexer and ultra-fast memory database separating heavy search loads from the core consensus nodes.
- **Analytics Watchdog**: An automated ML/Heuristics engine monitoring network traffic for Wash Trading, Sybil Attacks, and Whale movements in real-time.

---

## 🚀 Getting Started

### Prerequisites
- Go 1.21+
- Docker & Docker Compose
- Node.js (for frontend applications)

### Running the DevNet
You can simulate a globally distributed network of OpenReserve validators locally on your machine using Docker.

```bash
cd openreserve
docker-compose up --build
```
This will spin up:
- 1 Bootnode (P2P Discovery)
- 2 Independent Validator Nodes running BFT Consensus
- 1 ORPScan Block Explorer (Accessible at `http://localhost:5000`)

### Running ORPay (Consumer App)
To run the consumer application UI:
```bash
# Start Backend
cd apps/orpay/backend
go run main.go

# Start Frontend
cd apps/orpay/frontend
npm install && npm run dev
```

---

## 🪙 Tokenomics
The OpenReserve Network is powered by the **ORP** token.

- **Total Supply**: 1,000,000,000 ORP (Capped permanently at Genesis `Block 0`)
- **Deflationary Model**: 0.1% network convenience fees are permanently burned, mathematically driving scarcity over time.

### Genesis Allocation
- **20%** - Core Team & Founders (Time-locked)
- **40%** - Protocol Treasury (DAO Controlled)
- **40%** - Public Distribution & Community Airdrops

---

## 🛡️ License
OpenReserve is released under the MIT License. See `LICENSE` for details.
