// Package jsonstore keeps a value in memory and persists it to a JSON file
// with atomic replace, so a crash never leaves a half-written file. It suits
// pilot-scale data (thousands of records); a database comes later.
package jsonstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Store[T any] struct {
	mu   sync.RWMutex
	path string
	data T
}

// Open loads path, or starts from empty if the file does not exist.
func Open[T any](path string, empty T) (*Store[T], error) {
	s := &Store[T]{path: path, data: empty}
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

// Read runs fn with a read lock.
func (s *Store[T]) Read(fn func(T)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.data)
}

// Update runs fn with a write lock and saves if fn returns nil.
func (s *Store[T]) Update(fn func(T) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(s.data); err != nil {
		return err
	}
	return WriteAtomic(s.path, s.data)
}

// WriteAtomic writes v as JSON to path via a synced temp file and rename.
func WriteAtomic(path string, v any) error {
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
