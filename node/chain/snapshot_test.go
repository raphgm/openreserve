package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openreserve/node/types"
)

func TestSnapshotRestart(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	c := f.open(dir)
	c.SnapshotEvery = 10
	for i := range 25 {
		mustSubmit(t, c, f.tx(f.alice, f.bob, types.Unit, uint64(i)))
		f.produce(c)
	}
	want := c.Status()
	c.Close()

	c2 := f.open(dir)
	if c2.ReplayedFrom != 20 {
		t.Fatalf("replayed from %d, want 20 (latest snapshot)", c2.ReplayedFrom)
	}
	if got := c2.Status(); got.StateRoot != want.StateRoot || got.Height != 25 {
		t.Fatalf("after snapshot restart %+v, want %+v", got, want)
	}
	// Indexes cover blocks before the snapshot too.
	if h := c2.History(f.bob.addr, 100); len(h) != 25 {
		t.Errorf("bob history %d entries, want 25", len(h))
	}
	// The chain keeps working from the restored state.
	mustSubmit(t, c2, f.tx(f.alice, f.bob, types.Unit, 25))
	if b := f.produce(c2); b == nil || b.Header.Height != 26 {
		t.Fatal("could not extend chain after snapshot restart")
	}
}

func TestSnapshotTamperingFallsBackToReplay(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	c := f.open(dir)
	c.SnapshotEvery = 5
	for i := range 5 {
		mustSubmit(t, c, f.tx(f.alice, f.bob, types.Unit, uint64(i)))
		f.produce(c)
	}
	want := c.Status().StateRoot
	c.Close()

	// Give bob a fortune in the snapshot: its root no longer matches block 5.
	path := filepath.Join(dir, snapshotName)
	b, _ := os.ReadFile(path)
	forged := strings.Replace(string(b), `"balance": 5000000`, `"balance": 999000000000`, 1)
	if forged == string(b) {
		t.Fatal("test setup: balance not found in snapshot")
	}
	os.WriteFile(path, []byte(forged), 0o644)

	c2 := f.open(dir)
	if c2.ReplayedFrom != 0 {
		t.Fatal("tampered snapshot was trusted")
	}
	if c2.Status().StateRoot != want {
		t.Fatal("state differs after fallback replay")
	}
	if acc, _ := c2.Account(f.bob.addr); acc.Balance != 5*types.Unit {
		t.Errorf("bob balance %d", acc.Balance)
	}
}

func TestSnapshotFromOtherChainIgnored(t *testing.T) {
	f := newFixture(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	a := f.open(dirA)
	a.SnapshotEvery = 1
	mustSubmit(t, a, f.tx(f.alice, f.bob, types.Unit, 0))
	f.produce(a)
	a.Close()
	b := f.open(dirB)
	mustSubmit(t, b, f.tx(f.alice, f.bob, 2*types.Unit, 0))
	f.produce(b)
	b.Close()
	// Copy A's snapshot next to B's block log.
	data, _ := os.ReadFile(filepath.Join(dirA, snapshotName))
	os.WriteFile(filepath.Join(dirB, snapshotName), data, 0o644)
	b2 := f.open(dirB)
	if b2.ReplayedFrom != 0 {
		t.Fatal("snapshot from a different block history was used")
	}
	if acc, _ := b2.Account(f.bob.addr); acc.Balance != 2*types.Unit {
		t.Errorf("bob balance %d, want 2 ORP from chain B", acc.Balance)
	}
}
