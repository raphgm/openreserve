# Contributing to OpenReserve

Thank you for your interest in contributing to OpenReserve! We believe value should move across the world as freely as information moves across the Internet. 

OpenReserve is building the open financial infrastructure that makes that possible, and this is a community-driven effort. We welcome developers, researchers, economists, security engineers, documentation writers, and designers.

This document provides guidelines and workflows for contributing to the project.

## Code of Conduct

By participating in this project, you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md). We expect all contributors to foster an open, welcoming, diverse, inclusive, and healthy community.

## How Can I Contribute?

### 1. Reporting Bugs

If you find a bug, please open an issue in the repository. Provide as much detail as possible:
* A clear and descriptive title.
* Steps to reproduce the issue.
* Expected behavior vs. observed behavior.
* Environment details (OS, Go version, Node version, Docker version, etc.).

### 2. Suggesting Enhancements

Have an idea for a new feature or an improvement? We'd love to hear it!
* Open an issue describing the feature.
* Explain *why* this enhancement would be useful to the broader ecosystem.
* If you have an idea of how it could be implemented, include a rough architectural proposal.

### 3. Submitting Pull Requests

We gladly accept Pull Requests (PRs) for bug fixes, features, documentation updates, and tooling improvements.

#### Development Workflow

1. **Fork the repository**: Create your own fork of `openreserve`.
2. **Create a branch**: Branch off of `main`. Use a descriptive name (e.g., `feat/add-cross-chain-bridge`, `fix/consensus-timeout`, `docs/update-architecture`).
3. **Make your changes**: Write clean, modular, and well-documented code.
4. **Test your code**: Ensure all existing tests pass and write new tests for any new functionality.
5. **Format and lint**: Ensure your code adheres to the project's formatting and linting rules (e.g., `gofmt` for Go code).
6. **Commit your changes**: Write clear, concise commit messages. We recommend using [Conventional Commits](https://www.conventionalcommits.org/).
7. **Push to your fork**: Push your feature branch to your GitHub fork.
8. **Open a Pull Request**: Submit a PR against the `main` branch of the upstream repository.

#### Pull Request Guidelines

* **Keep it focused**: A PR should ideally address a single issue or feature. If you have multiple unrelated changes, submit them as separate PRs.
* **Link issues**: If your PR resolves an open issue, link to it in the PR description (e.g., "Fixes #123").
* **Review process**: Maintainers will review your PR. Be prepared to respond to feedback and make necessary adjustments. We value security and stability, so reviews may be thorough.

## Development Setup

To build and run the OpenReserve network locally, you will need:
- Go 1.26+
- Node.js
- Docker & Docker Compose

### Building the Project

We use a standard `Makefile` at the root of the repository to manage builds across our various languages and components.

To build everything (the core node, consumer apps, wallet SDK, and explorer), simply run:
```bash
make all
```

To run a local development network consisting of a bootnode, validators, and a block explorer:
```bash
docker compose up --build
```

## Security

If you discover a potential security vulnerability in OpenReserve, please **do not** open a public issue. Instead, refer to our [Security Policy](SECURITY.md) (if available) or contact the core team privately. We take security very seriously and will address disclosures promptly.

## Community

Join our community discussions! Check the main README for links to our Discord, forums, or community calls. 

Thank you for helping us build the Internet Financial Protocol!
