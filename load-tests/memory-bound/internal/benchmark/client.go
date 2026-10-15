package benchmark

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// keyValueKey returns the deterministic server key for dataset index i.
func keyValueKey(i int) string {
	return fmt.Sprintf("key-%09d", i)
}

// newClient builds a shared HTTP client tuned for keep-alive benchmarking.
func newClient(concurrency int) *http.Client {
	tr := &http.Transport{
		MaxIdleConns:        concurrency * 2,
		MaxIdleConnsPerHost: concurrency,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true, // compression only burns CPU here
		ForceAttemptHTTP2:   false,
	}
	return &http.Client{Transport: tr}
}

// worker executes the workload of a single concurrent client. All buffers
// and the RNG are per-worker; no locks are taken on the request path.
type worker struct {
	id     int
	cfg    *Config
	client *http.Client
	rnd    *rand.Rand
	log    *WorkerLog

	sb        strings.Builder
	writeBuf  []byte
	seqCursor *atomic.Uint64 // shared for sequential-read
	measuring *atomic.Bool
}

func newWorker(id int, cfg *Config, client *http.Client, seq *atomic.Uint64, measuring *atomic.Bool) *worker {
	w := &worker{
		id:        id,
		cfg:       cfg,
		client:    client,
		rnd:       rand.New(rand.NewPCG(uint64(cfg.Seed), uint64(cfg.Seed)+uint64(id)*0x9E3779B97F4A7C15)),
		log:       &WorkerLog{},
		writeBuf:  make([]byte, cfg.ValueSize),
		seqCursor: seq,
		measuring: measuring,
	}
	// Deterministic write payload derived from the seed.
	rr := rand.New(rand.NewPCG(uint64(cfg.Seed)+uint64(id), 0x1234567890ABCDEF))
	for i := range w.writeBuf {
		w.writeBuf[i] = byte(rr.IntN(256))
	}
	return w
}

// run loops until stop is closed. tokens, when non-nil, rate-limits the
// worker: each request consumes one token.
func (w *worker) run(stop <-chan struct{}, tokens <-chan struct{}) {
	for {
		if tokens != nil {
			select {
			case <-stop:
				return
			case <-tokens:
			}
		} else {
			select {
			case <-stop:
				return
			default:
			}
		}
		s := w.doOp()
		if w.measuring.Load() {
			w.log.Add(s)
		}
	}
}

func (w *worker) doOp() Sample {
	switch w.cfg.Mode {
	case ModeRandomRead:
		return w.readIdx(w.pick(w.cfg.DatasetEntries))
	case ModeReadWrite:
		if w.rnd.IntN(100) < w.cfg.ReadRatio {
			return w.readIdx(w.pick(w.cfg.DatasetEntries))
		}
		return w.writeIdx(w.pick(w.cfg.DatasetEntries))
	case ModeSequentialRead:
		i := w.seqCursor.Add(1) - 1
		return w.readIdx(int(i % uint64(w.cfg.DatasetEntries)))
	case ModeBatchRead:
		return w.readBatch()
	case ModeHotset:
		return w.readIdx(w.pick(HotsetSize(w.cfg)))
	}
	return Sample{OK: false, Values: 1, T: time.Now()}
}

func (w *worker) pick(n int) int {
	if n <= 1 {
		return 0
	}
	return w.rnd.IntN(n)
}

func (w *worker) kvURL(idx int) string {
	w.sb.Reset()
	w.sb.WriteString(w.cfg.URL)
	w.sb.WriteString("/kv/")
	w.sb.WriteString(keyValueKey(idx))
	return w.sb.String()
}

func (w *worker) readIdx(idx int) Sample {
	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, w.kvURL(idx), nil)
	if err != nil {
		return Sample{OK: false, Values: 1, T: time.Now(), Dur: time.Since(start)}
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return Sample{OK: false, Values: 1, T: time.Now(), Dur: time.Since(start)}
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return Sample{
		T:        time.Now(),
		Dur:      time.Since(start),
		BytesOut: int(n),
		Values:   1,
		OK:       resp.StatusCode == http.StatusOK,
	}
}

func (w *worker) writeIdx(idx int) Sample {
	start := time.Now()
	req, err := http.NewRequest(http.MethodPut, w.kvURL(idx), bytes.NewReader(w.writeBuf))
	if err != nil {
		return Sample{OK: false, Write: true, T: time.Now(), Dur: time.Since(start)}
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := w.client.Do(req)
	if err != nil {
		return Sample{OK: false, Write: true, T: time.Now(), Dur: time.Since(start)}
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return Sample{
		T:        time.Now(),
		Dur:      time.Since(start),
		BytesIn:  len(w.writeBuf),
		BytesOut: int(n),
		Values:   1,
		OK:       resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK,
		Write:    true,
	}
}

// readBatch fetches w.cfg.BatchSize values and parses the length-prefixed
// response format: repeated [4-byte big-endian length][value bytes].
func (w *worker) readBatch() Sample {
	start := time.Now()
	w.sb.Reset()
	w.sb.WriteString(w.cfg.URL)
	w.sb.WriteString("/batch?n=")
	w.sb.WriteString(strconv.Itoa(w.cfg.BatchSize))
	req, err := http.NewRequest(http.MethodGet, w.sb.String(), nil)
	if err != nil {
		return Sample{OK: false, T: time.Now(), Dur: time.Since(start)}
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return Sample{OK: false, T: time.Now(), Dur: time.Since(start)}
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		return Sample{T: time.Now(), Dur: time.Since(start), BytesOut: len(body), OK: false}
	}
	values, bad := parseFrames(body)
	return Sample{
		T:        time.Now(),
		Dur:      time.Since(start),
		BytesOut: len(body),
		Values:   values,
		OK:       !bad && values > 0,
	}
}

// parseFrames counts complete length-prefixed frames in body. bad is true
// when a frame header or payload is truncated or malformed.
func parseFrames(body []byte) (n int, bad bool) {
	off := 0
	for off < len(body) {
		if off+4 > len(body) {
			return n, true
		}
		l := int(binary.BigEndian.Uint32(body[off:]))
		off += 4
		if l < 0 || off+l > len(body) {
			return n, true
		}
		off += l
		n++
	}
	return n, false
}
