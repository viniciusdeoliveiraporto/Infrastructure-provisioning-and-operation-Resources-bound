# Memory-Bound KV Load Generator

Independent Go load generator for the in-memory key-value server in
[`apps/memory-bound`](../../apps/memory-bound). It drives configurable
workloads, records latency/throughput, collects client- and server-side
resource metrics, and generates plots automatically after every run.

See the [server README](../../apps/memory-bound/README.md) for the HTTP API,
architecture, and end-to-end experiment guides.

## Build

```bash
go build -o loadgen ./cmd/loadgen
pip install -r scripts/requirements.txt   # matplotlib for plots
```

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `-url` | `http://127.0.0.1:8080` | Server base URL (works across machines) |
| `-mode` | `random-read` | `random-read`, `read-write`, `sequential-read`, `batch-read`, `hotset` |
| `-duration` | `30s` | Measurement window (warmup excluded from results) |
| `-warmup` | `5s` | Warm-up window; samples discarded |
| `-concurrency` | `8` | Concurrent workers (each with its own keep-alive connection pool entry, RNG, and buffers) |
| `-read-ratio` | `100` | Percent of reads in `read-write` mode |
| `-value-size` | `1024` | Write payload bytes |
| `-dataset-entries` | `0` | Key-space size; `0` reads it from server `/stats` |
| `-batch-size` | `16` | Values per request in `batch-read` mode |
| `-seed` | `42` | RNG seed (recorded in `summary.json` for reproducibility) |
| `-rate` | `0` | Target HTTP req/s via token bucket; `0` = unlimited |
| `-hotset-fraction` | `0.01` | Hot-set size as a fraction of the dataset (`hotset` mode) |
| `-label` | `""` | Free-form infrastructure label (e.g. `m5.large`) recorded in results |
| `-results-dir` | `results` | Parent directory for run outputs |
| `-plot-script` | `scripts/plot_results.py` | Plot script path; empty string disables plotting |
| `-no-populate` | `false` | Skip `POST /admin/populate` (use a prepopulated server) |

The key space is bounded: operations always address `key-%09d` indices inside
`[0, dataset-entries)`, so repeated runs never grow the store.

## Workload modes

| Mode | Behavior |
|---|---|
| `random-read` | `GET /kv/<random existing key>` |
| `read-write` | `read-ratio`% reads, remainder `PUT` random keys in the bounded key space |
| `sequential-read` | Global sequential traversal of `[0, dataset-entries)` (shared atomic cursor, wraps) |
| `batch-read` | `GET /batch?n=<batch-size>`; reports HTTP req/s **and** values/s separately |
| `hotset` | Reads concentrated on the first `hotset-fraction × dataset` keys |

All modes use persistent HTTP connections (`MaxIdleConnsPerHost` sized to the
concurrency), disable compression, reuse per-worker buffers, and avoid
per-request logging or JSON on the value path.

## Outputs

Each run creates `results/<UTC-timestamp>-<mode>-c<conc>[-label]/`:

- `metrics.csv` — one row per measurement second: rps, payload/logical MiB/s,
  values/s, avg/p50/p95/p99 latency, counters, and all server/client resource
  columns (empty cells = metric unavailable). This is the join of benchmark
  intervals and resource samples by elapsed second.
- `summary.json` — effective configuration (including seed), aggregate
  metrics, and final server stats.
- `observations.txt` — evidence-based interpretation hints (client saturation,
  latency/CPU trends, GC coincidence, memory growth, disk I/O, PSI).
- `latency.png`, `throughput.png`, `resources.png`, `latency_vs_cpu.png`,
  `latency_vs_memory.png`, `system_activity.png`, `gc_pressure.png`,
  `hardware.png` (PMU IPC/cache misses, page faults, swap — panels skipped
  when the guest has no PMU, as in VirtualBox).

Hardware columns (`server_ipc`, `server_cache_miss_*`, `server_page_faults_*`,
`server_swap_*`) are filled from the server's `perf_event_open` counters and
`/proc`. Inside a VirtualBox guest the PMU is typically unavailable — run
`scripts/host_perf.sh <vm-name> <seconds>` **on the host** to capture the same
counters for the VM process, then match its 1 Hz lines to `metrics.csv`
`elapsed_s`.

Server-side columns require the server to run with `-admin` so the load
generator can call `GET /admin/metrics` after the run. Raw data is written
before plotting; a plotting failure prints an error and keeps the CSV/JSON.

## Plotting and comparison

```bash
# Regenerate plots for one run
python3 scripts/plot_results.py results/<run-id>

# Compare runs (any mix of modes/concurrency/instance labels)
python3 scripts/plot_results.py compare results/runA results/runB \
  -o results/comparison --labels "c16,c64"
```

Comparison plots: `compare_latency.png` (p50/p95/p99 panels),
`compare_throughput.png` (req/s + MiB/s), `compare_cpu.png`,
`compare_memory.png`. The x-axis is elapsed time, so runs of different
durations overlay cleanly; missing metrics are skipped with a printed message
and never abort the whole comparison. The script forces the headless `Agg`
backend — no display required (works over SSH).

## Memory-bound experiment runner

`scripts/run_memory_bound_experiments.sh` executes the E1/E2/E3 plan from the
server README (cache-resident vs DRAM-resident, dataset-size ladder,
concurrency wall) against a local server, then emits compare plots under
`results/e1-compare/`, `results/e2-compare/`, `results/e3-compare/`.

```bash
# defaults: 30s + 5s warmup, E1×3 repeats, E2/E3×2 repeats
scripts/run_memory_bound_experiments.sh

# quicker smoke (plots off)
DURATION=3s WARMUP=1s REPEATS_E1=1 REPEATS_E2E3=1 SKIP_PLOTS=1 \
  scripts/run_memory_bound_experiments.sh
```

See the server README's "Memory-bound experiment plan" for the evidence
checklist (swap/major-faults guardrails, IPC, cache-miss rate) and an example
results table.

## Reproducibility checklist

- Pass the same `-seed` and record the instance type in `-label`.
- `summary.json` stores the complete effective configuration.
- Use `-no-populate` (plus a fixed server-side `-populate-entries`) when you
  want identical datasets across server restarts.
- For rate-controlled experiments, remember that `-rate` applies during
  warmup too.

## Testing

```bash
go test ./...
go test -race ./...
```

Covers workload-configuration validation, percentile/interval math (including
retention of failed-request latencies and trailing-partial-interval handling),
batch frame parsing, worker request generation against an HTTP test server,
CSV/JSON export schemas, and the observation heuristics.
