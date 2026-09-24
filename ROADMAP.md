# OpenReserve Roadmap

OpenReserve is being built in distinct, deliberate phases. This roadmap outlines our path from initial specification to a globally adopted financial protocol.

## Phase 1: Foundation (Current)

The goal of Phase 1 is to establish the core data structures, basic networking, and wallet tooling.

* **Whitepaper & Protocol Specification**: Defining the economic and technical foundations.
* **Core Ledger** (done): Accounts, signed transfers, integer amounts, fee burn, persisted blocks with state roots, verifying replicas, REST API and `orctl` CLI.
* **Next up**: Wire ORPay and the explorer to the node API; a JavaScript SDK for signing transactions in the browser.
* **Wallet SDK**: Releasing the initial BIP39-compatible key management tool.
* **P2P Networking**: Bootstrapping peer discovery and gossip protocols.

## Phase 2: Consensus & Smart Contracts

Phase 2 introduces decentralized agreement and programmability.

* **BFT Consensus Engine**: Integrating a fast, deterministic Byzantine Fault Tolerant consensus mechanism.
* **Smart Contract Layer (WASM)**: Enabling developers to write and deploy WebAssembly-based smart contracts.
* **Block Explorer (ORPScan)**: Launching a visual interface for network indexing and search.
* **Developer SDKs**: Releasing robust SDKs for Go, JavaScript, and Python.

## Phase 3: Testnet & Governance

Phase 3 is about hardening the network with community participation.

* **Public Testnet Launch**: Opening the network for external validators and stress-testing.
* **On-Chain Governance**: Deploying the mechanisms for on-chain voting and proposal management.
* **Treasury System**: Activating the deflationary fee-burn and community treasury allocation.
* **Mobile Wallet MVP**: Releasing a consumer-friendly mobile application (ORPay).

## Phase 4: Mainnet & Interoperability

Phase 4 marks the official launch of the OpenReserve network.

* **Mainnet Genesis**: The official launch of the OpenReserve network.
* **Cross-Chain Bridges**: Activating trust-minimized bridges to Ethereum and Solana.
* **Institutional APIs**: Providing dedicated, high-throughput APIs for exchanges and custodians.
* **Hardware Security Module (HSM) Support**: Enabling enterprise-grade validator security.

## Phase 5: The Global Ecosystem

Phase 5 focuses on expanding OpenReserve's reach and capabilities.

* **AI Payments Integration**: Native support for AI agent micropayments and autonomous wallets.
* **Offline Transactions**: Implementing secure, hardware-backed offline payment capabilities.
* **Fiat Gateways**: Expanding native integrations with traditional banking infrastructure.
* **Global Ecosystem Expansion**: Sustained focus on developer onboarding and enterprise partnerships.
