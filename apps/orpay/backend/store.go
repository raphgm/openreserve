package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// jsonStore keeps a value in memory and persists it to a JSON file with
// atomic replace, so a crash never leaves a half-written file. It suits the
// pilot's data sizes (thousands of records); a database comes later.
type jsonStore[T any] struct {
	mu   sync.RWMutex
	path string
	data T
}

func openStore[T any](path string, empty T) (*jsonStore[T], error) {
	s := &jsonStore[T]{path: path, data: empty}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// read runs fn with a read lock.
func (s *jsonStore[T]) read(fn func(T)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.data)
}

// update runs fn with a write lock and saves if fn returns nil. If saving
// fails the in-memory change is kept but the error is returned, and the
// next successful save persists it.
func (s *jsonStore[T]) update(fn func(T) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(s.data); err != nil {
		return err
	}
	return writeAtomic(s.path, s.data)
}

func writeAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
