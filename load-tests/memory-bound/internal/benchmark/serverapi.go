package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ServerStats mirrors the server's GET /stats payload.
type ServerStats struct {
	Entries        int     `json:"entries"`
	ValueSize      int     `json:"value_size"`
	LogicalBytes   int64   `json:"logical_bytes"`
	TotalGets      uint64  `json:"total_gets"`
	TotalPuts      uint64  `json:"total_puts"`
	UptimeSec      float64 `json:"uptime_seconds"`
	MaxBody        int64   `json:"max_body_bytes"`
	MaxBatch       int     `json:"max_batch"`
	Admin          bool    `json:"admin_enabled"`
	MemBudgetBytes uint64  `json:"mem_budget_bytes"`
	MemLimitPct    float64 `json:"mem_limit_pct"`
}

// ServerSample is one resource sample exported by GET /admin/metrics.
type ServerSample struct {
	UnixNano  int64
	CPU       *float64
	SystemCPU *float64
	RSS       *float64
	Heap      *float64
	MemAvail  *float64
	GCCount   uint64
	GCPause   *float64
	DiskRead  *float64
	DiskWrite *float64
	NetRx     *float64
	NetTx     *float64
	PSICPU    *float64
	PSIMem    *float64

	PageFaults  *float64
	MajorFaults *float64
	SwapIn      *float64
	SwapOut     *float64

	IPC             *float64
	CacheMissesPerS *float64
	CacheMissRate   *float64
}

type serverSampleDTO struct {
	UnixNano  int64    `json:"unix_nano"`
	CPU       *float64 `json:"cpu_pct"`
	SystemCPU *float64 `json:"system_cpu_pct"`
	RSS       *float64 `json:"rss_mib"`
	Heap      *float64 `json:"heap_mib"`
	MemAvail  *float64 `json:"mem_available_mib"`
	GCCount   uint64   `json:"gc_count"`
	GCPause   *float64 `json:"gc_pause_ms"`
	DiskRead  *float64 `json:"disk_read_mib_s"`
	DiskWrite *float64 `json:"disk_write_mib_s"`
	NetRx     *float64 `json:"net_rx_mib_s"`
	NetTx     *float64 `json:"net_tx_mib_s"`
	PSICPU    *float64 `json:"psi_cpu_some_avg10"`
	PSIMem    *float64 `json:"psi_mem_some_avg10"`

	PageFaults  *float64 `json:"page_faults_per_s"`
	MajorFaults *float64 `json:"major_faults_per_s"`
	SwapIn      *float64 `json:"swap_in_mib_s"`
	SwapOut     *float64 `json:"swap_out_mib_s"`

	IPC             *float64 `json:"ipc"`
	CacheMissesPerS *float64 `json:"cache_misses_per_s"`
	CacheMissRate   *float64 `json:"cache_miss_rate"`
}

// FetchStats retrieves GET /stats.
func FetchStats(ctx context.Context, client *http.Client, base string) (ServerStats, error) {
	var out ServerStats
	err := getJSON(ctx, client, base+"/stats", &out)
	return out, err
}

// CheckHealth retries GET /health until it succeeds or the timeout elapses.
func CheckHealth(ctx context.Context, client *http.Client, base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("health status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("server not healthy within %s: %w", timeout, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Populate calls POST /admin/populate and returns the server response.
func Populate(ctx context.Context, client *http.Client, base string, entries, valueSize int) error {
	body, err := json.Marshal(map[string]int{"entries": entries, "value_size": valueSize})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/admin/populate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("populate failed: status %d: %s", resp.StatusCode, bytes.TrimSpace(b))
	}
	return nil
}

// FetchServerSamples retrieves buffered server resource samples in
// [since, until] (absolute unix nanoseconds).
func FetchServerSamples(ctx context.Context, client *http.Client, base string, since, until time.Time) ([]ServerSample, error) {
	url := fmt.Sprintf("%s/admin/metrics?since=%d&until=%d", base, since.UnixNano(), until.UnixNano())
	var payload struct {
		IntervalMs int               `json:"interval_ms"`
		Samples    []serverSampleDTO `json:"samples"`
	}
	if err := getJSON(ctx, client, url, &payload); err != nil {
		return nil, err
	}
	out := make([]ServerSample, 0, len(payload.Samples))
	for _, s := range payload.Samples {
		out = append(out, ServerSample{
			UnixNano: s.UnixNano, CPU: s.CPU, SystemCPU: s.SystemCPU,
			RSS: s.RSS, Heap: s.Heap, MemAvail: s.MemAvail,
			GCCount: s.GCCount, GCPause: s.GCPause,
			DiskRead: s.DiskRead, DiskWrite: s.DiskWrite,
			NetRx: s.NetRx, NetTx: s.NetTx,
			PSICPU: s.PSICPU, PSIMem: s.PSIMem,
			PageFaults: s.PageFaults, MajorFaults: s.MajorFaults,
			SwapIn: s.SwapIn, SwapOut: s.SwapOut,
			IPC: s.IPC, CacheMissesPerS: s.CacheMissesPerS, CacheMissRate: s.CacheMissRate,
		})
	}
	return out, nil
}

func getJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, bytes.TrimSpace(b))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
