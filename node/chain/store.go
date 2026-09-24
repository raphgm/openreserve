package chain

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/openreserve/node/types"
)

// blockLog is an append-only file of JSON blocks, one per line. Each append
// is fsynced before the block is considered committed. On open, a torn final
// line (from a crash mid-write) is truncated away.
type blockLog struct {
	f *os.File
}

func openBlockLog(path string) (*blockLog, []*types.Block, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, nil, err
	}
	var blocks []*types.Block
	r := bufio.NewReader(f)
	var good int64
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			if len(bytes.TrimSpace(line)) > 0 {
				fmt.Fprintf(os.Stderr, "blocklog: dropping torn trailing record (%d bytes)\n", len(line))
			}
			break
		}
		if err != nil {
			f.Close()
			return nil, nil, err
		}
		var b types.Block
		if err := json.Unmarshal(line, &b); err != nil {
			f.Close()
			return nil, nil, fmt.Errorf("blocklog corrupt at offset %d: %w", good, err)
		}
		blocks = append(blocks, &b)
		good += int64(len(line))
	}
	if err := f.Truncate(good); err != nil {
		f.Close()
		return nil, nil, err
	}
	if _, err := f.Seek(good, io.SeekStart); err != nil {
		f.Close()
		return nil, nil, err
	}
	return &blockLog{f: f}, blocks, nil
}

func (l *blockLog) append(b *types.Block) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if _, err := l.f.Write(append(data, '\n')); err != nil {
		return err
	}
	return l.f.Sync()
}

func (l *blockLog) close() error { return l.f.Close() }
