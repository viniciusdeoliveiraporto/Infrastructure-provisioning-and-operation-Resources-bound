package benchmark

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer mimics the parts of the KV API used by the workers.
func fakeServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /kv/{key}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("key") == "key-000000099" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("value-1234567890"))
	})
	mux.HandleFunc("PUT /kv/{key}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /batch", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("n") == "bad" {
			http.Error(w, "invalid", http.StatusBadRequest)
			return
		}
		var buf []byte
		for i := 0; i < 3; i++ {
			var l [4]byte
			binary.BigEndian.PutUint32(l[:], 5)
			buf = append(buf, l[:]...)
			buf = append(buf, "abcde"...)
		}
		w.Write(buf)
	})
	return httptest.NewServer(mux)
}

func testConfig(url string) Config {
	c := baseConfig()
	c.URL = url
	c.Mode = ModeRandomRead
	return c
}

func TestWorkerReadAndWriteSamples(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()
	cfg := testConfig(srv.URL)
	measuring := &atomic.Bool{}
	measuring.Store(true)
	seq := &atomic.Uint64{}
	w := newWorker(0, &cfg, newClient(1), seq, measuring)

	s := w.readIdx(1)
	if !s.OK || s.Values != 1 || s.BytesOut == 0 || s.Dur < 0 {
		t.Fatalf("read sample = %+v", s)
	}
	s = w.readIdx(99) // 404 from fake server
	if s.OK {
		t.Fatalf("missing key should fail: %+v", s)
	}
	s = w.writeIdx(1)
	if !s.OK || !s.Write || s.BytesIn != cfg.ValueSize {
		t.Fatalf("write sample = %+v", s)
	}
	s = w.readBatch()
	if !s.OK || s.Values != 3 || s.BytesOut != 3*(4+5) {
		t.Fatalf("batch sample = %+v", s)
	}
}

func TestWorkerRunRecordsOnlyWhileMeasuring(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Mode = ModeSequentialRead
	measuring := &atomic.Bool{}
	seq := &atomic.Uint64{}
	w := newWorker(0, &cfg, newClient(1), seq, measuring)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { w.run(stop, nil); close(done) }()
	time.Sleep(50 * time.Millisecond)
	if n := len(w.log.Samples); n != 0 {
		t.Fatalf("recorded %d samples while not measuring", n)
	}
	measuring.Store(true)
	time.Sleep(50 * time.Millisecond)
	measuring.Store(false)
	close(stop)
	<-done
	if len(w.log.Samples) == 0 {
		t.Fatal("no samples recorded while measuring")
	}
}

func TestSequentialReadsCoverKeySpace(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Mode = ModeSequentialRead
	cfg.DatasetEntries = 10
	measuring := &atomic.Bool{}
	measuring.Store(true)
	w := newWorker(0, &cfg, newClient(1), &atomic.Uint64{}, measuring)
	for i := 0; i < 10; i++ {
		if s := w.doOp(); !s.OK {
			t.Fatalf("sequential op %d failed", i)
		}
	}
	// Cursor wrapped: next read hits key-000000000 again.
	if got := w.seqCursor.Load(); got != 10 {
		t.Fatalf("cursor = %d; want 10", got)
	}
}

func TestHotsetStaysWithinSubset(t *testing.T) {
	srv := fakeServer(t)
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Mode = ModeHotset
	cfg.DatasetEntries = 100
	cfg.HotsetFraction = 0.05
	w := newWorker(0, &cfg, newClient(1), &atomic.Uint64{}, &atomic.Bool{})
	for i := 0; i < 50; i++ {
		idx := w.pick(HotsetSize(&cfg))
		if idx >= 5 {
			t.Fatalf("hotset index %d outside subset of 5", idx)
		}
	}
}
