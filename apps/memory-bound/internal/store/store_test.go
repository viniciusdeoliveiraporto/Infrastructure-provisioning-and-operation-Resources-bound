package store

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestPutGetOverwrite(t *testing.T) {
	s := New()
	mustPut(t, s, "a", []byte("one"))
	if v, ok := s.Get("a"); !ok || string(v) != "one" {
		t.Fatalf("Get(a) = %q, %v; want \"one\", true", v, ok)
	}
	mustPut(t, s, "a", []byte("two"))
	if v, ok := s.Get("a"); !ok || string(v) != "two" {
		t.Fatalf("after overwrite Get(a) = %q, %v; want \"two\", true", v, ok)
	}
	if got := s.Len(); got != 1 {
		t.Fatalf("Len() = %d; want 1", got)
	}
	if got := s.Snapshot().Puts; got != 2 {
		t.Fatalf("Puts = %d; want 2", got)
	}
}

func TestGetMissingKey(t *testing.T) {
	s := New()
	if _, ok := s.Get("nope"); ok {
		t.Fatal("Get(missing) returned ok=true")
	}
	if _, ok := s.RandomKey(); ok {
		t.Fatal("RandomKey on empty store returned ok=true")
	}
	if v := s.BatchValues(5); v != nil {
		t.Fatalf("BatchValues on empty store = %v; want nil", v)
	}
}

func TestPopulateDeterministicKeys(t *testing.T) {
	s := New()
	stats, err := s.Populate(100, 16)
	if err != nil {
		t.Fatalf("Populate: %v", err)
	}
	if stats.Entries != 100 || stats.LogicalBytes != 1600 {
		t.Fatalf("stats = %+v; want 100 entries / 1600 bytes", stats)
	}
	for i := 0; i < 100; i++ {
		v, ok := s.Get(Key(i))
		if !ok {
			t.Fatalf("missing key %q", Key(i))
		}
		if len(v) != 16 {
			t.Fatalf("value len = %d; want 16", len(v))
		}
		if got := binary.BigEndian.Uint32(v); got != uint32(i) {
			t.Fatalf("value index = %d; want %d", got, i)
		}
	}
	if _, ok := s.Get(Key(100)); ok {
		t.Fatal("key beyond dataset exists")
	}
	if got := s.Snapshot().ValueSize; got != 16 {
		t.Fatalf("ValueSize = %d; want 16", got)
	}
}

func TestPopulateReplacesDataset(t *testing.T) {
	s := New()
	if _, err := s.Populate(50, 8); err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, "extra", []byte("x"))
	if _, err := s.Populate(10, 8); err != nil {
		t.Fatal(err)
	}
	if got := s.Len(); got != 10 {
		t.Fatalf("Len after repopulate = %d; want 10", got)
	}
	if _, ok := s.Get("extra"); ok {
		t.Fatal("extra key survived repopulate")
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	s := New()
	if _, err := s.Populate(200, 32); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				k := Key((w*500 + i) % 200)
				if err := s.Put(k, []byte(fmt.Sprintf("w%02d-%06d", w, i))); err != nil {
					t.Errorf("Put: %v", err)
					return
				}
				if _, ok := s.Get(k); !ok {
					t.Errorf("missing key %s", k)
					return
				}
				if len(s.BatchValues(3)) == 0 {
					t.Error("BatchValues returned nothing on non-empty store")
					return
				}
				if _, ok := s.RandomKey(); !ok {
					t.Error("RandomKey failed on non-empty store")
					return
				}
			}
		}(w)
	}
	wg.Wait()
	if got := s.Snapshot().Puts; got != 8*500 {
		t.Fatalf("Puts = %d; want %d", got, 8*500)
	}
}

func TestBatchValuesDistinctAndBounded(t *testing.T) {
	s := New()
	if _, err := s.Populate(100, 4); err != nil {
		t.Fatal(err)
	}
	vals := s.BatchValues(10)
	if len(vals) != 10 {
		t.Fatalf("len = %d; want 10", len(vals))
	}
	seen := map[uint32]bool{}
	for _, v := range vals {
		if len(v) != 4 {
			t.Fatalf("bad value size %d", len(v))
		}
		idx := binary.BigEndian.Uint32(v)
		if seen[idx] {
			t.Fatalf("duplicate value index %d", idx)
		}
		seen[idx] = true
	}
	// n larger than the dataset clamps to dataset size.
	if got := len(s.BatchValues(500)); got != 100 {
		t.Fatalf("clamped batch = %d; want 100", got)
	}
	// n larger than half the dataset uses the Fisher-Yates path.
	if got := len(s.BatchValues(80)); got != 80 {
		t.Fatalf("large batch = %d; want 80", got)
	}
	if got := s.BatchValues(0); got != nil {
		t.Fatalf("BatchValues(0) = %v; want nil", got)
	}
}

func TestBoundedKeySpace(t *testing.T) {
	s := New()
	if _, err := s.Populate(25, 8); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		mustPut(t, s, Key(i%25), []byte("01234567"))
	}
	if got := s.Len(); got != 25 {
		t.Fatalf("key space grew: Len = %d; want 25", got)
	}
	if _, ok := s.Get(Key(25)); ok {
		t.Fatal("key outside bound exists")
	}
	if got := s.Snapshot().LogicalBytes; got != 25*8 {
		t.Fatalf("LogicalBytes = %d; want %d", got, 25*8)
	}
}

func TestPopulateValidation(t *testing.T) {
	s := New()
	if _, err := s.Populate(-1, 8); err == nil {
		t.Fatal("negative entries accepted")
	}
	if _, err := s.Populate(10, -1); err == nil {
		t.Fatal("negative value size accepted")
	}
	if _, err := s.Populate(MaxEntries+1, 8); err == nil {
		t.Fatal("oversized entries accepted")
	}
}

func mustPut(t *testing.T, s *Store, key string, val []byte) {
	t.Helper()
	if err := s.Put(key, val); err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
}

func TestBudgetExceeded(t *testing.T) {
	s := New()
	s.SetBudget(100)

	// Populate within budget.
	if _, err := s.Populate(10, 10); err != nil {
		t.Fatalf("populate within budget: %v", err)
	}
	// Populate exceeding budget is rejected before any allocation.
	if _, err := s.Populate(11, 10); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("populate over budget: err = %v; want ErrBudgetExceeded", err)
	}
	if got := s.Len(); got != 10 {
		t.Fatalf("entries after rejected populate = %d; want 10 (unchanged)", got)
	}

	// New keys that fit are accepted; beyond the budget are rejected.
	if err := s.Put("extra", make([]byte, 0)); err != nil {
		t.Fatalf("put within budget: %v", err)
	}
	if err := s.Put("over", make([]byte, 1)); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("put over budget: err = %v; want ErrBudgetExceeded", err)
	}
	if _, ok := s.Get("over"); ok {
		t.Fatal("budget-exceeded value was stored")
	}
	// Overwrites that do not grow the dataset always succeed.
	if err := s.Put(Key(0), make([]byte, 10)); err != nil {
		t.Fatalf("overwrite same size: %v", err)
	}
	if err := s.Put(Key(0), make([]byte, 5)); err != nil {
		t.Fatalf("shrink overwrite: %v", err)
	}
	// Puts are not counted when rejected.
	if got := s.Snapshot().Puts; got != 3 {
		t.Fatalf("puts = %d; want 3 (rejections not counted)", got)
	}
}

func TestBudgetUnlimitedWhenZero(t *testing.T) {
	s := New()
	s.SetBudget(0)
	if _, err := s.Populate(1000, 1024); err != nil {
		t.Fatalf("populate with budget disabled: %v", err)
	}
	if err := s.Put(Key(1000), make([]byte, 4096)); err != nil {
		t.Fatalf("put with budget disabled: %v", err)
	}
}
