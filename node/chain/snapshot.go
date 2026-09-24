package chain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/openreserve/node/jsonstore"
	"github.com/openreserve/node/ledger"
	"github.com/openreserve/node/types"
)

// A snapshot is the full ledger state after a given block, so a restart
// only re-executes blocks after it. It is trusted only if the block log
// still holds that block and the snapshot's state hashes to that block's
// state root; otherwise the node replays from genesis.

const snapshotName = "snapshot.json"

// DefaultSnapshotEvery is how many blocks pass between snapshots.
const DefaultSnapshotEvery = 1000

type snapshot struct {
	Height    uint64        `json:"height"`
	BlockHash types.Hash    `json:"block_hash"`
	State     *ledger.State `json:"state"`
}

func loadSnapshot(dataDir string, stored []*types.Block) (*snapshot, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, snapshotName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s snapshot
	if err := json.Unmarshal(b, &s); err != nil || s.State == nil {
		return nil, fmt.Errorf("unreadable snapshot: %v", err)
	}
	switch {
	case s.Height == 0 || s.Height > uint64(len(stored)):
		return nil, fmt.Errorf("snapshot at height %d is beyond the block log (%d blocks)", s.Height, len(stored))
	case stored[s.Height-1].Header.Hash() != s.BlockHash:
		return nil, errors.New("snapshot is from a different chain")
	case s.State.Root() != stored[s.Height-1].Header.StateRoot:
		return nil, errors.New("snapshot state does not match the block's state root")
	}
	s.State.Normalize()
	return &s, nil
}

// maybeSnapshot writes a snapshot every SnapshotEvery blocks. It runs with
// c.mu held. A failed write is logged by the caller and retried next time.
func (c *Chain) maybeSnapshot() error {
	h := uint64(len(c.blocks))
	if c.SnapshotEvery <= 0 || h == 0 || h%uint64(c.SnapshotEvery) != 0 || c.dataDir == "" {
		return nil
	}
	return jsonstore.WriteAtomic(filepath.Join(c.dataDir, snapshotName), snapshot{
		Height: h, BlockHash: c.blocks[h-1].Header.Hash(), State: c.state,
	})
}
