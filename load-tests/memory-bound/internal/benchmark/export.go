package benchmark

import (
	"encoding/csv"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"time"
)

// metricRow is one line of metrics.csv: the per-second benchmark interval
// joined with client and server resource samples that fall into the same
// elapsed-second bucket. Resource columns are empty when unavailable.
type metricRow struct {
	ElapsedS     float64
	RPS          float64
	MiBs         float64 // HTTP payload MiB/s
	LogicalMiBs  float64
	ValuesPerSec float64
	AvgMs        float64
	P50Ms        float64
	P95Ms        float64
	P99Ms        float64
	Requests     uint64
	Errors       uint64
	Gets         uint64
	Puts         uint64

	ServerCPU       *float64
	ServerSystemCPU *float64
	ServerRSS       *float64
	ServerHeap      *float64
	ServerMemAvail  *float64
	ServerGCCount   *uint64
	ServerGCPause   *float64
	ServerDiskRead  *float64
	ServerDiskWrite *float64
	ServerNetRx     *float64
	ServerNetTx     *float64
	ServerPSICPU    *float64
	ServerPSIMem    *float64

	ServerPageFaults  *float64
	ServerMajorFaults *float64
	ServerSwapIn      *float64
	ServerSwapOut     *float64

	ServerIPC             *float64
	ServerCacheMissesPerS *float64
	ServerCacheMissRate   *float64

	ClientCPU *float64
	ClientRSS *float64
}

var csvHeader = []string{
	"elapsed_s",
	"rps", "payload_mib_s", "logical_mib_s", "values_per_s",
	"avg_ms", "p50_ms", "p95_ms", "p99_ms",
	"requests", "errors", "gets", "puts",
	"server_cpu_pct", "server_system_cpu_pct",
	"server_rss_mib", "server_heap_mib", "server_mem_available_mib",
	"server_gc_count", "server_gc_pause_ms",
	"server_disk_read_mib_s", "server_disk_write_mib_s",
	"server_net_rx_mib_s", "server_net_tx_mib_s",
	"server_psi_cpu_some_avg10", "server_psi_mem_some_avg10",
	"server_page_faults_per_s", "server_major_faults_per_s",
	"server_swap_in_mib_s", "server_swap_out_mib_s",
	"server_ipc", "server_cache_misses_per_s", "server_cache_miss_rate",
	"client_cpu_pct", "client_rss_mib",
}

// buildRows joins interval benchmark data with resource samples by elapsed
// second since measurement start.
func buildRows(r *Run) []metricRow {
	bySecond := func(unixNano int64) int {
		return int((time.Unix(0, unixNano).Sub(r.MeasureStart)) / time.Second)
	}
	server := map[int]ServerSample{}
	for _, s := range r.Server {
		server[bySecond(s.UnixNano)] = s
	}
	client := map[int]ClientSample{}
	for _, s := range r.Client {
		client[bySecond(s.UnixNano)] = s
	}

	rows := make([]metricRow, 0, len(r.Result.Intervals))
	for _, iv := range r.Result.Intervals {
		bucket := int(iv.ElapsedS)
		row := metricRow{
			ElapsedS:     iv.ElapsedS,
			RPS:          float64(iv.Requests),
			ValuesPerSec: float64(iv.Values),
			AvgMs:        iv.AvgMs,
			P50Ms:        iv.P50Ms,
			P95Ms:        iv.P95Ms,
			P99Ms:        iv.P99Ms,
			Requests:     iv.Requests,
			Errors:       iv.Errors,
			Gets:         iv.Gets,
			Puts:         iv.Puts,
		}
		if iv.Requests > 0 {
			row.MiBs = float64(iv.BytesIn+iv.BytesOut) / (1 << 20)
			row.LogicalMiBs = float64(iv.Values) * float64(r.Cfg.ValueSize) / (1 << 20)
		}
		if ss, ok := server[bucket]; ok {
			row.ServerCPU = ss.CPU
			row.ServerSystemCPU = ss.SystemCPU
			row.ServerRSS = ss.RSS
			row.ServerHeap = ss.Heap
			row.ServerMemAvail = ss.MemAvail
			row.ServerGCCount = &ss.GCCount
			row.ServerGCPause = ss.GCPause
			row.ServerDiskRead = ss.DiskRead
			row.ServerDiskWrite = ss.DiskWrite
			row.ServerNetRx = ss.NetRx
			row.ServerNetTx = ss.NetTx
			row.ServerPSICPU = ss.PSICPU
			row.ServerPSIMem = ss.PSIMem
			row.ServerPageFaults = ss.PageFaults
			row.ServerMajorFaults = ss.MajorFaults
			row.ServerSwapIn = ss.SwapIn
			row.ServerSwapOut = ss.SwapOut
			row.ServerIPC = ss.IPC
			row.ServerCacheMissesPerS = ss.CacheMissesPerS
			row.ServerCacheMissRate = ss.CacheMissRate
		}
		if cs, ok := client[bucket]; ok {
			if cs.CPUOK {
				row.ClientCPU = &cs.CPUPct
			}
			if cs.RSSOK {
				row.ClientRSS = &cs.RSSMiB
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func f(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', 3, 64)
}

func u(v *uint64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatUint(*v, 10)
}

// WriteMetricsCSV exports the joined per-second metrics used by the plot
// script. Unavailable metrics are written as empty cells.
func WriteMetricsCSV(path string, r *Run) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	w := csv.NewWriter(file)
	defer w.Flush()
	if err := w.Write(csvHeader); err != nil {
		return err
	}
	for _, row := range buildRows(r) {
		record := []string{
			strconv.FormatFloat(row.ElapsedS, 'f', 3, 64),
			strconv.FormatFloat(row.RPS, 'f', 3, 64),
			strconv.FormatFloat(row.MiBs, 'f', 3, 64),
			strconv.FormatFloat(row.LogicalMiBs, 'f', 3, 64),
			strconv.FormatFloat(row.ValuesPerSec, 'f', 3, 64),
			strconv.FormatFloat(row.AvgMs, 'f', 3, 64),
			strconv.FormatFloat(row.P50Ms, 'f', 3, 64),
			strconv.FormatFloat(row.P95Ms, 'f', 3, 64),
			strconv.FormatFloat(row.P99Ms, 'f', 3, 64),
			strconv.FormatUint(row.Requests, 10),
			strconv.FormatUint(row.Errors, 10),
			strconv.FormatUint(row.Gets, 10),
			strconv.FormatUint(row.Puts, 10),
			f(row.ServerCPU), f(row.ServerSystemCPU),
			f(row.ServerRSS), f(row.ServerHeap), f(row.ServerMemAvail),
			u(row.ServerGCCount), f(row.ServerGCPause),
			f(row.ServerDiskRead), f(row.ServerDiskWrite),
			f(row.ServerNetRx), f(row.ServerNetTx),
			f(row.ServerPSICPU), f(row.ServerPSIMem),
			f(row.ServerPageFaults), f(row.ServerMajorFaults),
			f(row.ServerSwapIn), f(row.ServerSwapOut),
			f(row.ServerIPC), f(row.ServerCacheMissesPerS), f(row.ServerCacheMissRate),
			f(row.ClientCPU), f(row.ClientRSS),
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return w.Error()
}

// summaryJSON is the schema of summary.json.
type summaryJSON struct {
	RunID      string  `json:"run_id"`
	Label      string  `json:"label"`
	StartedUTC string  `json:"started_utc"`
	ElapsedS   float64 `json:"measure_elapsed_s"`

	Config  configJSON  `json:"config"`
	Summary benchJSON   `json:"summary"`
	Server  *serverJSON `json:"server_stats,omitempty"`

	PlotScript string `json:"plot_script,omitempty"`
}

type configJSON struct {
	URL            string  `json:"url"`
	Mode           string  `json:"mode"`
	DurationS      float64 `json:"duration_s"`
	WarmupS        float64 `json:"warmup_s"`
	Concurrency    int     `json:"concurrency"`
	ReadRatioPct   int     `json:"read_ratio_pct"`
	ValueSize      int     `json:"value_size"`
	DatasetEntries int     `json:"dataset_entries"`
	BatchSize      int     `json:"batch_size"`
	Seed           int64   `json:"seed"`
	Rate           float64 `json:"rate"`
	HotsetFraction float64 `json:"hotset_fraction"`
}

type benchJSON struct {
	Requests     uint64  `json:"requests"`
	Success      uint64  `json:"success"`
	Errors       uint64  `json:"errors"`
	RPS          float64 `json:"requests_per_s"`
	ValuesPerSec float64 `json:"values_per_s"`
	PayloadMiBs  float64 `json:"payload_mib_s"`
	LogicalMiBs  float64 `json:"logical_mib_s"`
	BytesIn      uint64  `json:"bytes_in"`
	BytesOut     uint64  `json:"bytes_out"`
	Values       uint64  `json:"values"`
	DurationS    float64 `json:"duration_s"`
	AvgMs        float64 `json:"latency_avg_ms"`
	P50Ms        float64 `json:"latency_p50_ms"`
	P95Ms        float64 `json:"latency_p95_ms"`
	P99Ms        float64 `json:"latency_p99_ms"`
	MinMs        float64 `json:"latency_min_ms"`
	MaxMs        float64 `json:"latency_max_ms"`
	ReadRatioPct float64 `json:"effective_read_ratio_pct"`
	Concurrency  int     `json:"concurrency"`
}

type serverJSON struct {
	Entries      int     `json:"entries"`
	ValueSize    int     `json:"value_size"`
	LogicalBytes int64   `json:"logical_bytes"`
	TotalGets    uint64  `json:"total_gets"`
	TotalPuts    uint64  `json:"total_puts"`
	UptimeSec    float64 `json:"uptime_seconds"`
}

// WriteSummaryJSON exports configuration and aggregate results.
func WriteSummaryJSON(path string, r *Run) error {
	s := r.Result.Summary
	sum := benchJSON{
		Requests: s.Requests, Success: s.Success, Errors: s.Errors,
		RPS: round3(s.RPS), ValuesPerSec: round3(s.ValuesPerSec),
		PayloadMiBs: round3(s.PayloadMiBs), LogicalMiBs: round3(s.LogicalMiBs),
		BytesIn: s.BytesIn, BytesOut: s.BytesOut, Values: s.Values,
		DurationS: round3(s.DurationS),
		AvgMs:     round3(s.AvgMs), P50Ms: round3(s.P50Ms), P95Ms: round3(s.P95Ms),
		P99Ms: round3(s.P99Ms), MinMs: round3(s.MinMs), MaxMs: round3(s.MaxMs),
		ReadRatioPct: round3(s.ReadRatio), Concurrency: s.Concurrency,
	}
	out := summaryJSON{
		RunID:      r.RunID,
		Label:      r.Cfg.Label,
		StartedUTC: r.MeasureStart.UTC().Format(time.RFC3339),
		ElapsedS:   round3(r.MeasureEnd.Sub(r.MeasureStart).Seconds()),
		Config: configJSON{
			URL: r.Cfg.URL, Mode: r.Cfg.Mode,
			DurationS: r.Cfg.Duration.Seconds(), WarmupS: r.Cfg.Warmup.Seconds(),
			Concurrency: r.Cfg.Concurrency, ReadRatioPct: r.Cfg.ReadRatio,
			ValueSize: r.Cfg.ValueSize, DatasetEntries: r.Cfg.DatasetEntries,
			BatchSize: r.Cfg.BatchSize, Seed: r.Cfg.Seed, Rate: r.Cfg.Rate,
			HotsetFraction: r.Cfg.HotsetFraction,
		},
		Summary:    sum,
		PlotScript: r.PlotScript,
	}
	if r.StatsAfter != nil {
		out.Server = &serverJSON{
			Entries: r.StatsAfter.Entries, ValueSize: r.StatsAfter.ValueSize,
			LogicalBytes: r.StatsAfter.LogicalBytes,
			TotalGets:    r.StatsAfter.TotalGets, TotalPuts: r.StatsAfter.TotalPuts,
			UptimeSec: round3(r.StatsAfter.UptimeSec),
		}
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

// WriteObservations saves the evidence-based run summary as text.
func WriteObservations(path string, obs []string) error {
	var b []byte
	b = append(b, []byte("Evidence-based observations (possible explanations, not conclusions):\n\n")...)
	for i, o := range obs {
		b = append(b, []byte(strconv.Itoa(i+1)+". "+o+"\n\n")...)
	}
	return os.WriteFile(path, b, 0o644)
}

func round3(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*1000) / 1000
}
