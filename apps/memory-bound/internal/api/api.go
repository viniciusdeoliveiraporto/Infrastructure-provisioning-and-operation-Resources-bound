// Package api exposes the HTTP endpoints of the key-value server.
//
// Endpoints:
//
//	GET  /health            liveness check
//	GET  /stats             store and counter statistics (JSON)
//	GET  /kv/{key}          raw value lookup
//	PUT  /kv/{key}          raw byte insert/replace
//	GET  /random            random existing value
//	GET  /batch?n=N         N distinct random values, length-prefixed frames
//	POST /admin/populate    prepopulate dataset (admin only)
//	GET  /admin/metrics     buffered resource samples (admin only)
//
// Stored values are never JSON-encoded on the hot path.
package api

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"memory-bound/internal/monitor"
	"memory-bound/internal/store"
)

// Config configures the HTTP surface.
type Config struct {
	MaxBodyBytes int64         // maximum PUT payload size
	MaxBatch     int           // maximum n accepted by /batch
	Admin        bool          // enables /admin/populate and /admin/metrics
	Monitor      time.Duration // resource sampling interval reported to clients
	MemLimitPct  float64       // dataset memory budget as % of available RAM (0 = unlimited), for /stats reporting
}

// Defaults applied when fields are zero.
const (
	DefaultMaxBodyBytes = 1 << 20 // 1 MiB
	DefaultMaxBatch     = 64
)

// Server routes HTTP requests to the store.
type Server struct {
	st    *store.Store
	cfg   Config
	mon   *monitor.Monitor
	start time.Time
}

// New builds a Server.
func New(st *store.Store, cfg Config, mon *monitor.Monitor) *Server {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = DefaultMaxBatch
	}
	if cfg.Monitor <= 0 {
		cfg.Monitor = time.Second
	}
	return &Server{st: st, cfg: cfg, mon: mon, start: time.Now()}
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /stats", s.handleStats)
	mux.HandleFunc("GET /kv/{key}", s.handleGetKV)
	mux.HandleFunc("PUT /kv/{key}", s.handlePutKV)
	mux.HandleFunc("GET /random", s.handleRandom)
	mux.HandleFunc("GET /batch", s.handleBatch)
	if s.cfg.Admin {
		mux.HandleFunc("POST /admin/populate", s.handlePopulate)
		mux.HandleFunc("GET /admin/metrics", s.handleMetrics)
	}
	return mux
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"uptime_seconds": time.Since(s.start).Seconds(),
	})
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	snap := s.st.Snapshot()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"entries":          snap.Entries,
		"value_size":       snap.ValueSize,
		"logical_bytes":    snap.LogicalBytes,
		"total_gets":       snap.Gets,
		"total_puts":       snap.Puts,
		"uptime_seconds":   time.Since(s.start).Seconds(),
		"max_body_bytes":   s.cfg.MaxBodyBytes,
		"max_batch":        s.cfg.MaxBatch,
		"admin_enabled":    s.cfg.Admin,
		"mem_budget_bytes": s.st.Budget(),
		"mem_limit_pct":    s.cfg.MemLimitPct,
	})
}

func (s *Server) handleGetKV(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	v, ok := s.st.Get(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.st.CountGet()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(v)))
	_, _ = w.Write(v)
}

func (s *Server) handlePutKV(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	// The store takes ownership of body (application-owned copy).
	if err := s.st.Put(key, body); err != nil {
		if errors.Is(err, store.ErrBudgetExceeded) {
			http.Error(w, err.Error(), http.StatusInsufficientStorage)
			return
		}
		http.Error(w, "store write failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRandom(w http.ResponseWriter, r *http.Request) {
	key, ok := s.st.RandomKey()
	if !ok {
		http.NotFound(w, r)
		return
	}
	v, ok := s.st.Get(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.st.CountGet()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(v)))
	_, _ = w.Write(v)
}

// handleBatch returns n values as concatenated frames of
// [4-byte big-endian length][value bytes]. Fewer frames are returned when
// the store holds fewer than n entries.
func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	if err != nil {
		http.Error(w, "invalid n", http.StatusBadRequest)
		return
	}
	if n < 1 || n > s.cfg.MaxBatch {
		http.Error(w, fmt.Sprintf("n must be in [1,%d]", s.cfg.MaxBatch), http.StatusBadRequest)
		return
	}
	vals := s.st.BatchValues(n)
	if len(vals) == 0 {
		http.NotFound(w, r)
		return
	}
	size := 0
	for _, v := range vals {
		size += 4 + len(v)
	}
	// One pre-sized buffer instead of many small writes.
	buf := make([]byte, 0, size)
	var lenBuf [4]byte
	for _, v := range vals {
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(v)))
		buf = append(buf, lenBuf[:]...)
		buf = append(buf, v...)
	}
	s.st.CountGet()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	_, _ = w.Write(buf)
}

type populateRequest struct {
	Entries   int `json:"entries"`
	ValueSize int `json:"value_size"`
}

type populateResponse struct {
	Entries      int     `json:"entries"`
	ValueSize    int     `json:"value_size"`
	LogicalBytes int64   `json:"logical_bytes"`
	DurationMs   float64 `json:"duration_ms"`
}

func (s *Server) handlePopulate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req populateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.ValueSize < 0 || req.Entries < 0 {
		http.Error(w, "entries and value_size must be non-negative", http.StatusBadRequest)
		return
	}
	if req.ValueSize > int(s.cfg.MaxBodyBytes) {
		http.Error(w, "value_size exceeds max body bytes", http.StatusBadRequest)
		return
	}
	start := time.Now()
	stats, err := s.st.Populate(req.Entries, req.ValueSize)
	if err != nil {
		if errors.Is(err, store.ErrPopulating) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, store.ErrBudgetExceeded) {
			http.Error(w, err.Error(), http.StatusInsufficientStorage)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, populateResponse{
		Entries:      stats.Entries,
		ValueSize:    stats.ValueSize,
		LogicalBytes: stats.LogicalBytes,
		DurationMs:   float64(time.Since(start).Microseconds()) / 1000,
	})
}

type metricDTO struct {
	UnixNano  int64    `json:"unix_nano"`
	CPU       *float64 `json:"cpu_pct,omitempty"`
	SystemCPU *float64 `json:"system_cpu_pct,omitempty"`
	RSS       *float64 `json:"rss_mib,omitempty"`
	Heap      *float64 `json:"heap_mib,omitempty"`
	MemAvail  *float64 `json:"mem_available_mib,omitempty"`
	GCCount   uint64   `json:"gc_count"`
	GCPause   *float64 `json:"gc_pause_ms,omitempty"`
	DiskRead  *float64 `json:"disk_read_mib_s,omitempty"`
	DiskWrite *float64 `json:"disk_write_mib_s,omitempty"`
	NetRx     *float64 `json:"net_rx_mib_s,omitempty"`
	NetTx     *float64 `json:"net_tx_mib_s,omitempty"`
	PSICPU    *float64 `json:"psi_cpu_some_avg10,omitempty"`
	PSIMem    *float64 `json:"psi_mem_some_avg10,omitempty"`

	PageFaults  *float64 `json:"page_faults_per_s,omitempty"`
	MajorFaults *float64 `json:"major_faults_per_s,omitempty"`
	SwapIn      *float64 `json:"swap_in_mib_s,omitempty"`
	SwapOut     *float64 `json:"swap_out_mib_s,omitempty"`

	IPC             *float64 `json:"ipc,omitempty"`
	CacheMissesPerS *float64 `json:"cache_misses_per_s,omitempty"`
	CacheMissRate   *float64 `json:"cache_miss_rate,omitempty"`
}

func fptr(v float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return &v
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.mon == nil {
		http.Error(w, "monitoring disabled", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	since := s.mon.StartWall()
	if v := q.Get("since"); v != "" {
		ns, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "invalid since", http.StatusBadRequest)
			return
		}
		since = time.Unix(0, ns)
	}
	until := time.Now()
	if v := q.Get("until"); v != "" {
		ns, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "invalid until", http.StatusBadRequest)
			return
		}
		until = time.Unix(0, ns)
	}
	samples := s.mon.SamplesSince(since)
	out := make([]metricDTO, 0, len(samples))
	for _, sm := range samples {
		if sm.Time.After(until) {
			continue
		}
		out = append(out, metricDTO{
			UnixNano:  sm.Time.UnixNano(),
			CPU:       fptr(sm.CPUPct, sm.CPUOK),
			SystemCPU: fptr(sm.SystemCPU, sm.SystemOK),
			RSS:       fptr(sm.RSSMiB, sm.RSSOK),
			Heap:      fptr(sm.HeapMiB, sm.HeapOK),
			MemAvail:  fptr(sm.MemAvailableMiB, sm.MemAvailOK),
			GCCount:   sm.GCCount,
			GCPause:   fptr(sm.GCPauseMs, sm.GCOK),
			DiskRead:  fptr(sm.DiskReadMiBs, sm.DiskOK),
			DiskWrite: fptr(sm.DiskWriteMiBs, sm.DiskOK),
			NetRx:     fptr(sm.NetRxMiBs, sm.NetOK),
			NetTx:     fptr(sm.NetTxMiBs, sm.NetOK),
			PSICPU:    fptr(sm.PSICPU, sm.PSIOK),
			PSIMem:    fptr(sm.PSIMem, sm.PSIOK),

			PageFaults:  fptr(sm.PageFaultsPerS, sm.FaultsOK),
			MajorFaults: fptr(sm.MajorFaultsPerS, sm.FaultsOK),
			SwapIn:      fptr(sm.SwapInMiBs, sm.SwapOK),
			SwapOut:     fptr(sm.SwapOutMiBs, sm.SwapOK),

			IPC:             fptr(sm.IPC, sm.HWOK),
			CacheMissesPerS: fptr(sm.CacheMissesPerS, sm.HWOK),
			CacheMissRate:   fptr(sm.CacheMissRate, sm.HWOK),
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"interval_ms": int(s.cfg.Monitor.Milliseconds()),
		"samples":     out,
	})
}
