// Command server runs the in-memory key-value HTTP server used for
// performance experiments.
//
// Example:
//
//	server -addr :8080 -admin -populate-entries 100000 -populate-value-size 1024
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"memory-bound/internal/api"
	"memory-bound/internal/monitor"
	"memory-bound/internal/store"
)

func main() {
	var (
		addr            = flag.String("addr", ":8080", "listen address")
		admin           = flag.Bool("admin", false, "enable /admin/populate and /admin/metrics (restrict to trusted networks)")
		maxBody         = flag.Int64("max-body-bytes", api.DefaultMaxBodyBytes, "maximum PUT request body size in bytes")
		maxBatch        = flag.Int("max-batch", api.DefaultMaxBatch, "maximum n accepted by GET /batch")
		populateEntries = flag.Int("populate-entries", 0, "prepopulate this many entries at startup (0 = start empty)")
		populateValSize = flag.Int("populate-value-size", 1024, "value size in bytes used for startup prepopulation")
		memLimitPct     = flag.Float64("mem-limit-pct", 80, "dataset memory budget as a percentage of available RAM (0 disables the limit)")
		monitorInterval = flag.Duration("monitor-interval", time.Second, "resource metric sampling interval")
		shutdownTimeout = flag.Duration("shutdown-timeout", 10*time.Second, "graceful shutdown timeout")
	)
	flag.Parse()

	st := store.New()
	mon := monitor.New(*monitorInterval, 0)
	mon.Start()

	// Dataset memory budget: reject population/writes that would grow the
	// logical dataset beyond mem-limit-pct of currently available RAM.
	refreshBudget := func() {
		if *memLimitPct <= 0 {
			st.SetBudget(0)
			return
		}
		avail, ok := monitor.AvailableMemoryBytes()
		if !ok {
			st.SetBudget(0)
			return
		}
		st.SetBudget(uint64(float64(avail) * *memLimitPct / 100))
	}
	refreshBudget()
	if *memLimitPct <= 0 {
		log.Printf("dataset memory budget disabled (-mem-limit-pct=0)")
	} else {
		log.Printf("dataset memory budget: %.0f%% of available RAM = %.1f MiB (refreshed every %s)",
			*memLimitPct, float64(st.Budget())/(1<<20), *monitorInterval)
		go func() {
			t := time.NewTicker(*monitorInterval)
			defer t.Stop()
			for range t.C {
				refreshBudget()
			}
		}()
	}

	if *populateEntries > 0 {
		start := time.Now()
		stats, err := st.Populate(*populateEntries, *populateValSize)
		if err != nil {
			log.Fatalf("startup populate: %v", err)
		}
		log.Printf("populated %d entries x %d bytes (%.2f MiB logical) in %s",
			stats.Entries, stats.ValueSize,
			float64(stats.LogicalBytes)/(1<<20), time.Since(start).Round(time.Millisecond))
	}

	srv := api.New(st, api.Config{
		MaxBodyBytes: *maxBody,
		MaxBatch:     *maxBatch,
		Admin:        *admin,
		Monitor:      *monitorInterval,
		MemLimitPct:  *memLimitPct,
	}, mon)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("kv server listening on %s (admin=%v, max_body=%d, max_batch=%d)",
			*addr, *admin, *maxBody, *maxBatch)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
	}
	mon.Stop()
}
