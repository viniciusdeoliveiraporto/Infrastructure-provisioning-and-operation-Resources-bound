// Package store implements a bounded in-memory key-value store guarded by
// sync.RWMutex. All values live in RAM; nothing is persisted to disk.
package store

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
)

// ErrPopulating is returned when a second population is attempted while
// another one is still in progress.
var ErrPopulating = errors.New("populate already in progress")

// ErrBudgetExceeded is returned when an operation would grow the logical
// dataset beyond the configured memory budget.
var ErrBudgetExceeded = errors.New("dataset would exceed the memory budget")

// MaxEntries bounds the prepopulated dataset so a misconfigured request
// cannot exhaust memory.
const MaxEntries = 10_000_000

// Key returns the deterministic key for index i (e.g. "key-000000001").
// The load generator uses the same format.
func Key(i int) string {
	return fmt.Sprintf("key-%09d", i)
}

// Store is a concurrent in-memory key-value store.
//
// Read path: Get returns the application-owned slice directly (no copy);
// callers must treat it as read-only. Write path: Put takes ownership of the
// value slice, which callers must not mutate afterwards.
type Store struct {
	mu    sync.RWMutex
	popMu sync.Mutex // serializes Populate; allows rejecting concurrent calls

	data       map[string][]byte
	keys       []string // index of keys for O(1) random/batch selection
	valueBytes int64    // logical size of stored values
	presetSize int      // value size used by the last Populate (0 if none)

	gets atomic.Uint64
	puts atomic.Uint64

	// budget is the maximum logical dataset size in bytes (sum of value
	// lengths); 0 disables the limit. Refreshed periodically by the server
	// from system available memory.
	budget atomic.Uint64
}

// Stats is a point-in-time snapshot of store counters.
type Stats struct {
	Entries      int    `json:"entries"`
	ValueSize    int    `json:"value_size"`
	LogicalBytes int64  `json:"logical_bytes"`
	Gets         uint64 `json:"total_gets"`
	Puts         uint64 `json:"total_puts"`
}

// New creates an empty store.
func New() *Store {
	return &Store{data: make(map[string][]byte)}
}

// SetBudget sets the maximum logical dataset size in bytes (0 = unlimited).
func (s *Store) SetBudget(bytes uint64) { s.budget.Store(bytes) }

// Budget returns the current logical dataset budget in bytes (0 = unlimited).
func (s *Store) Budget() uint64 { return s.budget.Load() }

// Get returns the value for key. ok is false if the key does not exist.
func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	v, ok := s.data[key]
	s.mu.RUnlock()
	return v, ok
}

// CountGet increments the successful GET counter.
func (s *Store) CountGet() { s.gets.Add(1) }

// Put inserts or replaces key. It takes ownership of value. When the write
// would grow the logical dataset beyond the budget, ErrBudgetExceeded is
// returned and nothing is stored. Overwrites that do not grow the dataset
// are always allowed.
func (s *Store) Put(key string, value []byte) error {
	budget := s.budget.Load()
	s.mu.Lock()
	old, existed := s.data[key]
	if budget > 0 {
		growth := int64(len(value))
		if existed {
			growth -= int64(len(old))
		}
		if growth > 0 && s.valueBytes+growth > int64(budget) {
			s.mu.Unlock()
			return ErrBudgetExceeded
		}
	}
	if existed {
		s.valueBytes += int64(len(value) - len(old))
		s.data[key] = value
	} else {
		s.data[key] = value
		s.keys = append(s.keys, key)
		s.valueBytes += int64(len(value))
	}
	s.mu.Unlock()
	s.puts.Add(1)
	return nil
}

// Populate replaces the whole dataset with entries deterministic keys
// ("key-%09d") holding fixed-size values. Values embed the entry index in
// their first 4 bytes (big endian) when valueSize >= 4 so tests and
// experiments can verify content.
//
// A concurrent second Populate returns ErrPopulating; concurrent reads and
// writes block until population completes.
func (s *Store) Populate(entries, valueSize int) (Stats, error) {
	if entries < 0 || entries > MaxEntries || valueSize < 0 {
		return Stats{}, fmt.Errorf("invalid populate parameters: entries=%d value_size=%d", entries, valueSize)
	}
	if budget := s.budget.Load(); budget > 0 && int64(entries)*int64(valueSize) > int64(budget) {
		return Stats{}, fmt.Errorf("%w: dataset would be %d bytes, budget is %d bytes",
			ErrBudgetExceeded, int64(entries)*int64(valueSize), budget)
	}
	if !s.popMu.TryLock() {
		return Stats{}, ErrPopulating
	}
	defer s.popMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = make(map[string][]byte, entries)
	s.keys = make([]string, 0, entries)
	for i := 0; i < entries; i++ {
		v := make([]byte, valueSize)
		if valueSize >= 4 {
			binary.BigEndian.PutUint32(v, uint32(i))
		} else if valueSize > 0 {
			v[0] = byte(i)
		}
		k := Key(i)
		s.data[k] = v
		s.keys = append(s.keys, k)
	}
	s.valueBytes = int64(entries) * int64(valueSize)
	s.presetSize = valueSize
	return s.snapshotLocked(), nil
}

// RandomKey returns a uniformly random existing key without materializing
// the key list per request (an index over keys is maintained incrementally).
func (s *Store) RandomKey() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.keys) == 0 {
		return "", false
	}
	return s.keys[rand.IntN(len(s.keys))], true
}

// BatchValues returns up to n distinct randomly selected values. Selection
// avoids building a full key list; a small rejection sampler is used for
// small n and a partial Fisher-Yates shuffle when n is large relative to the
// dataset. The returned slices are shared with the store (read-only).
func (s *Store) BatchValues(n int) [][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.keys)
	if n <= 0 || total == 0 {
		return nil
	}
	if n > total {
		n = total
	}
	out := make([][]byte, 0, n)
	if n == total {
		for _, k := range s.keys {
			out = append(out, s.data[k])
		}
		return out
	}
	if n*2 <= total {
		seen := make(map[int]struct{}, n)
		for len(seen) < n {
			i := rand.IntN(total)
			if _, ok := seen[i]; ok {
				continue
			}
			seen[i] = struct{}{}
			out = append(out, s.data[s.keys[i]])
		}
		return out
	}
	idx := make([]int, total)
	for i := range idx {
		idx[i] = i
	}
	for i := 0; i < n; i++ {
		j := i + rand.IntN(total-i)
		idx[i], idx[j] = idx[j], idx[i]
		out = append(out, s.data[s.keys[idx[i]]])
	}
	return out
}

// Len returns the number of entries.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

// Snapshot returns the current counters without mutating them.
func (s *Store) Snapshot() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Stats {
	return Stats{
		Entries:      len(s.data),
		ValueSize:    s.presetSize,
		LogicalBytes: s.valueBytes,
		Gets:         s.gets.Load(),
		Puts:         s.puts.Load(),
	}
}
