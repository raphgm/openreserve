# Architecture

OpenReserve is built on a modular architecture to ensure separation of concerns, high throughput, and developer ergonomics.

## 1. Core Node (`node/`)
Written in Go, the core node handles the underlying P2P networking, consensus, and state transitions.
- **Networking**: Gossip protocol for transaction mempool diffusion and block relay.
- **Consensus**: BFT engine for deterministic finality.
- **State Machine**: Processes transactions and updates the ledger securely using a Merkle-Patricia Trie.

## 2. Cryptography
OpenReserve uses modern cryptographic primitives:
- **Signatures**: `Ed25519` for fast and secure digital signatures.
- **Hashing**: `SHA256` (with planned migration paths to `BLAKE3` for performance).

## 3. Interfaces
- **gRPC API**: Fast, binary-encoded protocol for node-to-node and backend-to-node communication.
- **REST API**: Standardized JSON interface for wallets, block explorers, and lightweight clients.

## 4. Smart Contracts (Phase 11)
The execution layer will support WASM (WebAssembly) to allow developers to write financial smart contracts in Rust, C++, and Go, executing in a highly performant and secure sandbox.
