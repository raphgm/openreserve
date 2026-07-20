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

# Getting Started

## Requirements

- Go 1.21+
- Docker
- Docker Compose
- Node.js

---

## Start the Development Network

```bash
docker compose up --build
```

This starts

- Bootnode
- Validator 1
- Validator 2
- Block Explorer

---

## Run ORPay

```bash
cd apps/orpay/backend
go run main.go
```

```bash
cd apps/orpay/frontend
npm install
npm run dev
```

---

# Repository Structure

```
openreserve/

node/
wallet/
apps/
sdk/
contracts/
explorer/
governance/
docs/
examples/
scripts/
tests/
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
