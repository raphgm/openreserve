# Experimental prototypes

These packages are early sketches (BFT consensus, P2P, VM, bridge,
governance, reserve minting). They are **not built or used** by the node:
the leading underscore makes the Go toolchain ignore this directory.

They use `float64` balances and in-memory state and do not compile against
the current `types`/`ledger`/`chain` packages. Each is kept as a design
reference and will be rebuilt on the working core when its roadmap
milestone comes up, rather than patched in place.
