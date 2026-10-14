package benchmark

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleRun() *Run {
	base := time.Now()
	l := &WorkerLog{}
	for i := 0; i < 20; i++ {
		l.Add(mkSample(base, float64(i*50), 12, 1, true, i%2 == 0, 0, 64))
	}
	res := Summarize([]*WorkerLog{l}, base, base.Add(time.Second), 64, 4)
	cpu := 55.5
	rss := 120.25
	pause := 1.5
	gc := uint64(7)
	return &Run{
		Cfg: Config{
			URL: "http://127.0.0.1:8080", Mode: ModeRandomRead,
			Duration: time.Second, Concurrency: 4, ValueSize: 64,
			DatasetEntries: 100, Seed: 42, Label: "test-instance",
			ResultsDir: "results",
		},
		Result:       res,
		MeasureStart: base,
		MeasureEnd:   base.Add(time.Second),
		RunID:        "test-run",
		Client: []ClientSample{
			{UnixNano: base.Add(time.Second).UnixNano(), CPUPct: 40, CPUOK: true, RSSMiB: 50, RSSOK: true},
		},
		Server: []ServerSample{
			{UnixNano: base.Add(time.Second).UnixNano(), CPU: &cpu, RSS: &rss, GCCount: gc, GCPause: &pause},
		},
		StatsAfter: &ServerStats{Entries: 100, ValueSize: 64, LogicalBytes: 6400, TotalGets: 15, TotalPuts: 5},
	}
}

func TestWriteMetricsCSVFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.csv")
	r := sampleRun()
	if err := WriteMetricsCSV(path, r); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatalf("rows = %d; want header + data", len(rows))
	}
	header := rows[0]
	if header[0] != "elapsed_s" || header[1] != "rps" {
		t.Fatalf("unexpected header: %v", header[:3])
	}
	want := map[string]bool{
		"p99_ms": false, "server_cpu_pct": false, "server_rss_mib": false,
		"client_cpu_pct": false, "server_gc_pause_ms": false, "logical_mib_s": false,
		"server_ipc": false, "server_cache_miss_rate": false,
		"server_page_faults_per_s": false, "server_swap_out_mib_s": false,
	}
	for _, h := range header {
		if _, ok := want[h]; ok {
			want[h] = true
		}
	}
	for h, seen := range want {
		if !seen {
			t.Errorf("missing column %q in header", h)
		}
	}
	data := rows[1]
	// Elapsed and latency columns must be non-empty; resource columns joined
	// from the +1s sample fall outside this 1s interval and stay empty.
	if data[0] != "0.000" {
		t.Fatalf("elapsed_s = %q", data[0])
	}
	if data[7] == "" {
		t.Fatal("p95_ms empty")
	}
	if len(data) != len(header) {
		t.Fatalf("row width %d != header %d", len(data), len(header))
	}
}

func TestWriteSummaryJSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary.json")
	r := sampleRun()
	if err := WriteSummaryJSON(path, r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"run_id", "config", "summary"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
	summary := got["summary"].(map[string]any)
	for _, key := range []string{"requests", "requests_per_s", "latency_p99_ms", "effective_read_ratio_pct"} {
		if _, ok := summary[key]; !ok {
			t.Errorf("summary missing %q", key)
		}
	}
	cfg := got["config"].(map[string]any)
	if cfg["seed"].(float64) != 42 {
		t.Errorf("seed = %v", cfg["seed"])
	}
	if !strings.Contains(string(b), "test-instance") {
		t.Error("label not recorded")
	}
}

func TestRunIDForSanitized(t *testing.T) {
	c := baseConfig()
	c.Label = "m5.large / zone a"
	id := RunIDFor(c, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if strings.ContainsAny(id, " /") {
		t.Fatalf("run id not sanitized: %q", id)
	}
	if !strings.Contains(id, "random-read") || !strings.Contains(id, "c4") {
		t.Fatalf("run id missing config info: %q", id)
	}
}

func TestObserveClientSaturation(t *testing.T) {
	r := sampleRun()
	r.Client = []ClientSample{
		{CPUPct: 95, CPUOK: true}, {CPUPct: 97, CPUOK: true}, {CPUPct: 96, CPUOK: true},
	}
	obs := Observe(r)
	found := false
	for _, o := range obs {
		if strings.Contains(o, "client process CPU") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected client saturation observation; got %v", obs)
	}
}

func TestObserveMemoryGrowth(t *testing.T) {
	r := sampleRun()
	lo, hi := 100.0, 200.0
	r.Server = []ServerSample{
		{UnixNano: r.MeasureStart.UnixNano(), RSS: &lo},
		{UnixNano: r.MeasureStart.Add(2 * time.Second).UnixNano(), RSS: &hi},
	}
	obs := Observe(r)
	found := false
	for _, o := range obs {
		if strings.Contains(o, "RSS grew") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected memory growth observation; got %v", obs)
	}
}

func TestObserveSwapEvidence(t *testing.T) {
	r := sampleRun()
	lo, hi := 0.0, 2.5
	r.Server = []ServerSample{
		{SwapIn: &lo, SwapOut: &lo},
		{SwapIn: &hi, SwapOut: &hi},
		{SwapIn: &hi, SwapOut: &lo},
	}
	obs := Observe(r)
	found := false
	for _, o := range obs {
		if strings.Contains(o, "swap traffic") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected swap evidence observation; got %v", obs)
	}
}

func TestObserveHWEvidence(t *testing.T) {
	r := sampleRun()
	rate, ipc := 0.25, 0.42
	r.Server = []ServerSample{
		{CacheMissRate: &rate, IPC: &ipc},
		{CacheMissRate: &rate, IPC: &ipc},
		{CacheMissRate: &rate, IPC: &ipc},
	}
	obs := Observe(r)
	found := false
	for _, o := range obs {
		if strings.Contains(o, "cache miss rate") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cache-miss observation; got %v", obs)
	}
}

func TestObserveNoHWWhenUnavailable(t *testing.T) {
	r := sampleRun()
	obs := Observe(r)
	for _, o := range obs {
		if strings.Contains(o, "cache miss rate") || strings.Contains(o, "IPC averaged") {
			t.Fatalf("must not invent hardware evidence when counters absent: %s", o)
		}
	}
}
