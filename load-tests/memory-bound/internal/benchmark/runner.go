package benchmark

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Run holds everything produced by one benchmark execution.
type Run struct {
	Cfg          Config
	Result       Result
	Client       []ClientSample
	Server       []ServerSample
	StatsBefore  *ServerStats
	StatsAfter   *ServerStats
	MeasureStart time.Time
	MeasureEnd   time.Time
	Dir          string
	Observations []string
	RunID        string
	PlotScript   string // path used for automatic plotting (recorded in summary.json)
}

// RunIDFor builds the unique result directory name for a configuration.
func RunIDFor(cfg Config, now time.Time) string {
	id := fmt.Sprintf("%s-%s-c%d", now.UTC().Format("20060102T150405"), cfg.Mode, cfg.Concurrency)
	if cfg.Label != "" {
		id += "-" + sanitize(cfg.Label)
	}
	return id
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.' || r == '/':
			b.WriteRune('-')
		}
	}
	if b.Len() > 40 {
		return b.String()[:40]
	}
	return b.String()
}

// Run executes the full benchmark: preflight, optional population, warmup,
// measurement, metric collection, export, and plotting.
func Execute(ctx context.Context, cfg Config) (*Run, error) {
	client := newClient(cfg.Concurrency)
	if err := CheckHealth(ctx, client, cfg.URL, 15*time.Second); err != nil {
		return nil, err
	}
	stats, err := FetchStats(ctx, client, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch /stats (server must be reachable): %w", err)
	}
	if err := cfg.Resolve(stats.Entries); err != nil {
		return nil, err
	}
	// Pre-flight: refuse a dataset the server will reject anyway, with a
	// clearer message than the populate failure.
	if stats.MemBudgetBytes > 0 {
		logical := int64(cfg.DatasetEntries) * int64(cfg.ValueSize)
		if logical > int64(stats.MemBudgetBytes) {
			return nil, fmt.Errorf(
				"dataset would be %.1f MiB (%d entries x %d B) but the server memory budget is %.1f MiB (%.0f%% of available RAM); reduce -dataset-entries or -value-size",
				float64(logical)/(1<<20), cfg.DatasetEntries, cfg.ValueSize,
				float64(stats.MemBudgetBytes)/(1<<20), stats.MemLimitPct)
		}
	}
	if !cfg.NoPopulate {
		if !stats.Admin {
			return nil, fmt.Errorf("server admin API disabled; start the server with -admin or pass -no-populate and prepopulate externally")
		}
		log.Printf("populating dataset: %d entries x %d bytes", cfg.DatasetEntries, cfg.ValueSize)
		if err := Populate(ctx, client, cfg.URL, cfg.DatasetEntries, cfg.ValueSize); err != nil {
			return nil, err
		}
	}

	before, err := FetchStats(ctx, client, cfg.URL)
	if err != nil {
		return nil, err
	}
	if before.Entries == 0 {
		return nil, fmt.Errorf("dataset is empty; populate it before measuring")
	}

	run := &Run{Cfg: cfg, RunID: RunIDFor(cfg, time.Now())}
	seq := &atomic.Uint64{}
	measuring := &atomic.Bool{}

	workers := make([]*worker, cfg.Concurrency)
	logs := make([]*WorkerLog, cfg.Concurrency)
	for i := 0; i < cfg.Concurrency; i++ {
		workers[i] = newWorker(i, &cfg, client, seq, measuring)
		logs[i] = workers[i].log
	}

	stop := make(chan struct{})
	var tokens <-chan struct{}
	var tokenStop chan struct{}
	if cfg.Rate > 0 {
		ch := make(chan struct{})
		tokenStop = make(chan struct{})
		tokens = ch
		go func() {
			interval := time.Duration(float64(time.Second) / cfg.Rate)
			t := time.NewTicker(interval)
			defer t.Stop()
			for {
				select {
				case <-tokenStop:
					return
				case <-t.C:
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		}()
	}

	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func(w *worker) {
			defer wg.Done()
			w.run(stop, tokens)
		}(w)
	}

	if cfg.Warmup > 0 {
		log.Printf("warmup for %s (samples discarded)", cfg.Warmup)
		select {
		case <-ctx.Done():
			close(stop)
			wg.Wait()
			return nil, ctx.Err()
		case <-time.After(cfg.Warmup):
		}
	}

	log.Printf("measuring for %s with concurrency=%d mode=%s", cfg.Duration, cfg.Concurrency, cfg.Mode)
	cm := newClientMonitor()
	cm.start()
	measureStart := time.Now()
	measuring.Store(true)

	select {
	case <-ctx.Done():
		measuring.Store(false)
		close(stop)
		wg.Wait()
		cm.stopSampling()
		return nil, ctx.Err()
	case <-time.After(cfg.Duration):
	}
	measureEnd := time.Now()
	measuring.Store(false)
	close(stop)
	wg.Wait()
	cm.stopSampling()
	if tokenStop != nil {
		close(tokenStop)
	}

	after, err := FetchStats(ctx, client, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch /stats after run: %w", err)
	}

	run.MeasureStart = measureStart
	run.MeasureEnd = measureEnd
	run.Result = Summarize(logs, measureStart, measureEnd, cfg.ValueSize, cfg.Concurrency)
	run.Client = cm.snapshot()
	run.StatsBefore = &before
	run.StatsAfter = &after

	serverSamples, err := FetchServerSamples(ctx, client, cfg.URL, measureStart, measureEnd)
	if err != nil {
		log.Printf("warning: server metrics unavailable (admin disabled?): %v", err)
	} else if len(serverSamples) == 0 {
		log.Printf("warning: server metrics empty — check clock sync between client and server (enable NTP, e.g. `sudo timedatectl set-ntp true` on the server)")
	}
	run.Server = serverSamples
	run.Observations = Observe(run)

	return run, nil
}

// Export writes metrics.csv, summary.json, and observations.txt into the
// run directory, then attempts automatic plot generation. Plot failures are
// reported but never delete the raw data.
func (r *Run) Export(plotScript string) error {
	dir := filepath.Join(r.Cfg.ResultsDir, r.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	r.Dir = dir
	if err := WriteMetricsCSV(filepath.Join(dir, "metrics.csv"), r); err != nil {
		return err
	}
	if err := WriteSummaryJSON(filepath.Join(dir, "summary.json"), r); err != nil {
		return err
	}
	if err := WriteObservations(filepath.Join(dir, "observations.txt"), r.Observations); err != nil {
		return err
	}
	if plotScript != "" {
		GeneratePlots(plotScript, dir)
	}
	return nil
}

// GeneratePlots invokes scripts/plot_results.py for a single run directory.
// Failures are printed clearly; raw data is kept either way.
func GeneratePlots(script, dir string) {
	if _, err := os.Stat(script); err != nil {
		fmt.Fprintf(os.Stderr, "plot generation skipped: %s not found (%v)\n", script, err)
		return
	}
	python := "python3"
	if _, err := exec.LookPath(python); err != nil {
		python = "python"
		if _, err := exec.LookPath(python); err != nil {
			fmt.Fprintln(os.Stderr, "plot generation skipped: python3/python not found in PATH")
			return
		}
	}
	cmd := exec.Command(python, script, dir)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		fmt.Print(string(out))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "plot generation failed: %v (raw data kept in %s)\n", err, dir)
	}
}
