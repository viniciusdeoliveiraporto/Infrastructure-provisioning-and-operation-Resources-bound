// Command loadgen drives configurable workloads against the in-memory
// key-value server and records latency, throughput, and resource metrics.
//
// Example:
//
//	loadgen -url http://server:8080 -mode random-read -dataset-entries 100000 \
//	        -concurrency 16 -duration 30s -warmup 5s -seed 42
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"memory-bound-loadgen/internal/benchmark"
)

func main() {
	var cfg benchmark.Config
	flag.StringVar(&cfg.URL, "url", "http://127.0.0.1:8080", "server base URL")
	flag.StringVar(&cfg.Mode, "mode", benchmark.ModeRandomRead,
		"workload mode: random-read, read-write, sequential-read, batch-read, hotset")
	flag.DurationVar(&cfg.Duration, "duration", 30*time.Second, "measurement duration")
	flag.DurationVar(&cfg.Warmup, "warmup", 5*time.Second, "warm-up duration (samples discarded)")
	flag.IntVar(&cfg.Concurrency, "concurrency", 8, "number of concurrent workers")
	flag.IntVar(&cfg.ReadRatio, "read-ratio", 100, "percentage of read operations (read-write mode)")
	flag.IntVar(&cfg.ValueSize, "value-size", 1024, "write payload size in bytes")
	flag.IntVar(&cfg.DatasetEntries, "dataset-entries", 0, "dataset size (0 = read from server /stats)")
	flag.IntVar(&cfg.BatchSize, "batch-size", 16, "values per request (batch-read mode)")
	flag.Int64Var(&cfg.Seed, "seed", 42, "random seed for reproducibility")
	flag.Float64Var(&cfg.Rate, "rate", 0, "target HTTP requests per second (0 = unlimited)")
	flag.Float64Var(&cfg.HotsetFraction, "hotset-fraction", 0.01, "fraction of the dataset forming the hot set (hotset mode)")
	flag.StringVar(&cfg.Label, "label", "", "free-form infrastructure label recorded in results (e.g. m5.large)")
	flag.StringVar(&cfg.ResultsDir, "results-dir", "results", "directory for result run directories")
	flag.StringVar(&cfg.PlotScript, "plot-script", "scripts/plot_results.py", "plot script path; empty disables plotting")
	flag.BoolVar(&cfg.NoPopulate, "no-populate", false, "skip POST /admin/populate (use a prepopulated server)")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("interrupt received, stopping...")
		cancel()
	}()

	run, err := benchmark.Execute(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark failed: %v\n", err)
		os.Exit(1)
	}
	run.PlotScript = cfg.PlotScript
	if err := run.Export(cfg.PlotScript); err != nil {
		fmt.Fprintf(os.Stderr, "export failed: %v\n", err)
		os.Exit(1)
	}

	printSummary(run)
	plots := "plots disabled"
	if cfg.PlotScript != "" {
		plots = "plots via " + cfg.PlotScript
	}
	fmt.Printf("\nresults written to %s (%s)\n", run.Dir, plots)
}

func printSummary(r *benchmark.Run) {
	s := r.Result.Summary
	c := r.Cfg
	fmt.Println("=== benchmark summary ===")
	fmt.Printf("run id            : %s\n", r.RunID)
	if c.Label != "" {
		fmt.Printf("label             : %s\n", c.Label)
	}
	fmt.Printf("mode              : %s\n", c.Mode)
	fmt.Printf("concurrency       : %d\n", c.Concurrency)
	fmt.Printf("dataset entries   : %d (value size %d B)\n", c.DatasetEntries, c.ValueSize)
	fmt.Printf("intended ratio    : %d%% reads\n", c.EffectiveReadRatio())
	fmt.Printf("effective ratio   : %.1f%% reads (%d gets / %d puts)\n", s.ReadRatio, s.Gets, s.Puts)
	fmt.Printf("duration          : %.1fs (warmup %.1fs excluded)\n", s.DurationS, c.Warmup.Seconds())
	fmt.Printf("requests          : %d total, %d ok, %d errors\n", s.Requests, s.Success, s.Errors)
	fmt.Printf("requests/s        : %.1f\n", s.RPS)
	fmt.Printf("values/s          : %.1f\n", s.ValuesPerSec)
	fmt.Printf("http payload      : %.2f MiB/s (in %.1f MiB, out %.1f MiB)\n",
		s.PayloadMiBs, float64(s.BytesIn)/(1<<20), float64(s.BytesOut)/(1<<20))
	fmt.Printf("logical access    : %.2f MiB/s\n", s.LogicalMiBs)
	fmt.Printf("latency avg       : %.3f ms\n", s.AvgMs)
	fmt.Printf("latency p50/p95/p99: %.3f / %.3f / %.3f ms\n", s.P50Ms, s.P95Ms, s.P99Ms)
	fmt.Printf("latency min/max   : %.3f / %.3f ms\n", s.MinMs, s.MaxMs)
	fmt.Println("=== observations (possible explanations) ===")
	for i, o := range r.Observations {
		fmt.Printf("%d. %s\n\n", i+1, o)
	}
}
