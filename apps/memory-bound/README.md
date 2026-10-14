# Memory-Bound KV Benchmark Application

An in-memory key-value HTTP server in Go, built for performance experiments on
AWS EC2 (and anywhere else Linux runs). It is intentionally minimal: a
`map[string][]byte` behind a `sync.RWMutex`, a small HTTP API, and a background
resource monitor. Together with the independent load generator in
[`load-tests/memory-bound`](../../load-tests/memory-bound), it lets you measure
how dataset size, access patterns, concurrency, and machine characteristics
affect latency and throughput — and automatically produces plots that help
explain the numbers.

There is no persistence, no replication, no eviction, no Redis, and no external
database. Everything lives in RAM.

## Repository layout

```text
apps/memory-bound/               # this module: the KV server
  cmd/server/                    # server binary
  internal/store/                # in-memory store (map + RWMutex + key index)
  internal/api/                  # HTTP handlers
  internal/monitor/              # background resource sampler (/proc + runtime)
load-tests/memory-bound/         # sibling module: the load generator
  cmd/loadgen/                   # loadgen binary
  internal/benchmark/            # workloads, recording, export, observations
  scripts/plot_results.py        # Matplotlib plot generation + compare mode
  scripts/requirements.txt       # matplotlib
  results/                       # one directory per benchmark run
```

The server and load generator are **separate Go modules and binaries**, so the
client can run on a different machine from the server.

## Architecture

```text
┌────────────┐   HTTP (keep-alive)   ┌─────────────────────────────┐
│  loadgen   │ ────────────────────▶ │ server                      │
│ workers    │  GET/PUT /kv, /random │  ┌───────────────────────┐  │
│ recorder   │  GET /batch, /stats   │  │ store: map + RWMutex  │  │
│ client     │  POST /admin/populate │  │ + incremental key idx │  │
│ monitor    │  GET /admin/metrics   │  └───────────────────────┘  │
└────────────┘                       │  monitor goroutine (1 Hz)   │
       │                             └─────────────────────────────┘
       ▼
 results/<run-id>/{metrics.csv, summary.json, observations.txt, *.png}
```

- Every worker keeps its own unbounded sample slice; nothing locks on the
  request path in the client. Percentiles are computed once after the run.
- The server monitor samples about once per second into a buffer, independent
  of request handlers. After the run the load generator pulls the samples via
  `GET /admin/metrics` and merges them into `metrics.csv`.
- Plot generation is a subprocess call to `scripts/plot_results.py`; raw data
  is written first, so a plotting failure never loses the run.

## Build

Requires Go 1.21+ (developed with Go 1.27).

```bash
cd apps/memory-bound
go build -o server ./cmd/server

cd ../../load-tests/memory-bound
go build -o loadgen ./cmd/loadgen
pip install -r scripts/requirements.txt   # matplotlib, for plots
```

## Run the server

```bash
# Empty store, admin API enabled (needed for loadgen-driven population
# and server-side metric collection).
./server -addr :8080 -admin

# Prepopulate at startup: 100k entries, 1 KiB values (~98 MiB logical).
./server -addr :8080 -admin \
  -populate-entries 100000 -populate-value-size 1024
```

Flags:

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-admin` | `false` | Enables `POST /admin/populate` and `GET /admin/metrics` |
| `-max-body-bytes` | `1048576` | Maximum PUT payload size |
| `-max-batch` | `64` | Maximum `n` accepted by `/batch` |
| `-populate-entries` | `0` | Prepopulate this many entries at startup |
| `-populate-value-size` | `1024` | Value size for startup prepopulation |
| `-mem-limit-pct` | `80` | Dataset memory budget: max percentage of *available* RAM the logical dataset may occupy (`0` disables) |
| `-monitor-interval` | `1s` | Resource sampling interval |

**Security note:** the admin endpoints are unauthenticated. They are disabled by
default; enable `-admin` only on trusted/private networks (security groups,
VPC peering, or `localhost` with an SSH tunnel). With `-admin` off you can still
prepopulate at startup via `-populate-entries`, but the load generator cannot
trigger population or fetch server-side metrics.

## Deploy on a VM (nginx + systemd)

Run the server behind nginx on a Linux VM (Debian/Ubuntu, `apt`) so it is
reachable from your PC. Deployment files mirror the `services/io-bound/`
convention: an nginx site, a systemd unit, and two start scripts.

1. Prepare the VM (installs nginx and sysstat):

   ```bash
   sudo bash scripts/setup-vm.sh
   ```

2. Put the repository on the VM at the path the unit expects (skip if it is
   already there), e.g. from the project root on your PC:

   ```bash
   ssh <user>@<vm> mkdir -p ~/Infrastructure-provisioning-and-operation-Resources-bound
   scp -r apps services <user>@<vm>:~/Infrastructure-provisioning-and-operation-Resources-bound/
   ```

3. Install and start nginx + the server service:

   ```bash
   cd Infrastructure-provisioning-and-operation-Resources-bound
   sudo cp services/memory-bound/memory-bound /etc/nginx/sites-available/
   sudo bash services/memory-bound/start-nginx.sh
   sudo cp services/memory-bound/memory-bound.service /etc/systemd/system/
   sudo bash services/memory-bound/start-service.sh
   ```

   The unit runs `./server -addr 127.0.0.1:8080 -admin`, so the Go server is
   reachable only through nginx (port 80 in the guest). Adjust `User=` and the
   paths in the unit if your VM's user/home differ.

4. From your PC, forward a host port to the guest with VirtualBox NAT
   (GUI: Settings → Network → Advanced → Port Forwarding), or:

   ```bash
   VBoxManage modifyvm "<vm-name>" --natpf1 "memory-bound,tcp,127.0.0.1,8080,,80"
   ```

5. Verify and use it:

   ```bash
   curl http://127.0.0.1:8080/health
   curl -X POST http://127.0.0.1:8080/admin/populate -d '{"entries":10000,"value_size":1024}'
   curl http://127.0.0.1:8080/kv/key-000000042
   ```

   Or open the manual demo page (store/retrieve a key from the browser):
   **http://127.0.0.1:8080/demo/** — served by nginx from
   `apps/memory-bound/demo/index.html`.

Caveats: the demo/admin path adds nginx + NAT overhead, so run the real
benchmarks inside the VM with loadgen pointed at `127.0.0.1:8080` directly
(bypassing nginx). The admin endpoints are unauthenticated — keep the VM on a
private/NAT-only network.

## HTTP API

| Endpoint | Description |
|---|---|
| `GET /health` | `{"status":"ok","uptime_seconds":...}` |
| `GET /stats` | JSON: `entries`, `value_size` (last populate size), `logical_bytes` (sum of stored value lengths), `total_gets`, `total_puts`, `uptime_seconds`, limits, `mem_budget_bytes`, `mem_limit_pct` |
| `GET /kv/{key}` | Raw value bytes (`application/octet-stream`) or `404` |
| `PUT /kv/{key}` | Insert/replace with a raw byte body; `204` on success, `413` if the body exceeds `-max-body-bytes` |
| `GET /random` | A uniformly random existing value; `404` on an empty store |
| `GET /batch?n=N` | N distinct random values, `1 ≤ N ≤ max-batch`. Response: concatenated frames of `[4-byte big-endian length][value bytes]`. Fewer frames are returned when the store has fewer than N entries; `400` for invalid N |
| `POST /admin/populate` | `{"entries":100000,"value_size":1024}` — replaces the whole dataset with deterministic keys `key-000000000…`. Returns entry count, value size, logical bytes, duration. A second concurrent populate is rejected with `409` |
| `GET /admin/metrics?since=<unixnano>&until=<unixnano>` | Buffered resource samples (see below) |

Keys are deterministic: entry *i* is `key-%09d` (e.g. `key-000000042`). Values
created by populate embed the big-endian entry index in their first 4 bytes.

Stored values are never JSON-encoded on the hot path.

## Dataset prepopulation

Population can happen three ways; all finish before measurement starts:

1. **Startup flag** — `-populate-entries N -populate-value-size S`.
2. **Load generator** — by default the loadgen calls `POST /admin/populate`
   before the warmup (disable with `-no-populate`).
3. **Manual** — `curl -X POST http://host:8080/admin/populate -d
   '{"entries":100000,"value_size":1024}'`.

Population replaces the entire dataset (no merges), holds the store write lock
for its duration, and is serialized: overlapping calls get `409 Conflict`.

## Memory budget (dataset size guard)

The server refuses to let the dataset grow past a configured fraction of the
machine's available memory, so benchmarks cannot push the host into swap or
OOM. Defaults to **80% of `MemAvailable`**.

- The budget is recomputed every `-monitor-interval` (default 1 s) from
  `/proc/meminfo` `MemAvailable`, so it adapts as other processes come and go.
- It applies to the **logical dataset size** (sum of stored value bytes), not
  RSS. Go runtime overhead (~10–40 MiB) sits on top, leaving the remainder of
  the machine as headroom.
- Enforced at three points; violations return `507 Insufficient Storage`:
  - startup `-populate-entries` (fails fast with a clear log message),
  - `POST /admin/populate` (dataset unchanged on rejection),
  - `PUT /kv/{key}` when the write would **grow** the dataset past the budget
    (overwrites that keep the size are always allowed).
- `/stats` exposes `mem_budget_bytes` and `mem_limit_pct` so clients can
  pre-flight. The load generator checks the budget before populating and
  reports exactly how much to reduce `-dataset-entries`/`-value-size`.
- `-mem-limit-pct 0` disables the guard entirely (not recommended on
  production-sized instances). On non-Linux hosts, where `MemAvailable` cannot
  be read, the budget is disabled and logged.

## Resource monitoring

The server samples every second on a background goroutine:

| Metric | Source | Notes |
|---|---|---|
| Process CPU % | `/proc/self/stat` | Percent of **one** core; can exceed 100 on multi-core |
| System CPU % | `/proc/stat` | Whole host, 0–100 |
| Process RSS MiB | `/proc/self/status` `VmRSS` | |
| Go heap MiB | `runtime.ReadMemStats` | `HeapAlloc` |
| GC count / pause ms | `runtime.ReadMemStats` | Pause summed over the interval via `PauseTotalNs` delta |
| Disk read/write MiB/s | `/proc/diskstats` | Whole-disk devices only (partitions excluded) |
| Net rx/tx MiB/s | `/proc/net/dev` | All interfaces except loopback |
| CPU / memory PSI | `/proc/pressure/*` `some avg10` | Linux ≥ 4.20; empty elsewhere |
| Page faults/s | `/proc/self/stat` minflt/majflt | Process minor and major (swap-in) fault rates |
| Swap in/out MiB/s | `/proc/vmstat` pswpin/pswpout | Whole-system paging activity — direct evidence of RAM exhaustion |
| IPC, cache misses/s, miss rate | PMU via `perf_event_open(2)` | Per-process hardware counters; see below |

Rates are computed from cumulative counter deltas between consecutive samples.
Any metric that cannot be read is recorded as *unavailable* (empty CSV cell,
omitted JSON field) — never invented. The load generator additionally records
its own CPU % and RSS to detect client saturation.

### Hardware counters (PMU) and where they work

`IPC`, `cache_misses_per_s`, and `cache_miss_rate` come from real
performance-monitoring counters opened per process with
`perf_event_open(2)` (user-space only when `perf_event_paranoid` requires it):

| Environment | In-guest PMU | What to do |
|---|---|---|
| Bare metal, EC2 (most instances) | ✅ works | Columns populate automatically in `metrics.csv` |
| **VirtualBox guest** | ❌ usually not exposed | Use `load-tests/memory-bound/scripts/host_perf.sh` on the **host** (below) |
| Non-Linux guest | ❌ | Only `/proc` metrics unavailable as well |

Zero-progress PMU (a guest that opens counters but does not advance them) is
detected per sample and published as unavailable rather than as zeros.

**Host-side measurement for VirtualBox scenarios** — from a Linux host, while
the benchmark runs inside the guest:

```bash
./scripts/host_perf.sh <vm-name> 60 host_perf.txt   # 1 Hz interval report
```

It records cycles, instructions, cache references/misses, LLC/TLB misses, and
page faults for the `VBoxHeadless` process. Match its one-sample-per-second
lines to `metrics.csv` `elapsed_s`: rising miss rate and falling IPC as the
dataset grows is the hardware-level confirmation of the memory bottleneck.

## Running benchmarks

See [`load-tests/memory-bound/README.md`](../../load-tests/memory-bound/README.md)
for all flags and workload modes. Quick examples (client and server on the same
machine, server prepopulated via admin):

```bash
# Random reads over a 100k x 1KiB dataset, 16 workers, 30s + 5s warmup.
./loadgen -url http://SERVER:8080 -mode random-read \
  -dataset-entries 100000 -value-size 1024 \
  -concurrency 16 -duration 30s -warmup 5s -seed 42 -label t3.medium

# Mixed workload: 70% reads / 30% writes.
./loadgen -url http://SERVER:8080 -mode read-write -read-ratio 70 \
  -dataset-entries 100000 -value-size 1024 -concurrency 16 -duration 30s

# Batch reads: 16 values per HTTP request.
./loadgen -url http://SERVER:8080 -mode batch-read -batch-size 16 \
  -dataset-entries 100000 -concurrency 16 -duration 30s

# Sequential scan of the key space.
./loadgen -url http://SERVER:8080 -mode sequential-read \
  -dataset-entries 100000 -concurrency 4 -duration 30s

# Hot set: 1% of the dataset absorbs all traffic.
./loadgen -url http://SERVER:8080 -mode hotset -hotset-fraction 0.01 \
  -dataset-entries 100000 -concurrency 16 -duration 30s

# Fixed request rate instead of open-loop max throughput.
./loadgen -url http://SERVER:8080 -mode random-read -rate 20000 \
  -dataset-entries 100000 -concurrency 16 -duration 30s
```

Each run writes `results/<run-id>/` with `metrics.csv`, `summary.json`,
`observations.txt`, and eight PNG plots (latency, throughput, resources,
latency-vs-CPU, latency-vs-memory, system activity, GC pressure, hardware
counters).

## Running client and server on separate EC2 instances

1. Launch two instances in the same VPC/security group (e.g. one
   `t3.medium` server, one `c5.large` client — the client should not be the
   bottleneck).
2. On the **server**:

   ```bash
   scp server ec2-user@SERVER:~
   ssh ec2-user@SERVER
   ./server -addr :8080 -admin -populate-entries 1000000 -populate-value-size 1024
   ```

3. On the **client**:

   ```bash
   scp -r loadgen scripts ec2-user@CLIENT:~
   ssh ec2-user@CLIENT
   pip install -r scripts/requirements.txt
   ./loadgen -url http://SERVER_PRIVATE_IP:8080 -mode random-read \
     -dataset-entries 1000000 -value-size 1024 \
     -concurrency 16 -duration 60s -warmup 10s -seed 42 -label c5.large
   ```

4. Copy `results/` back if you want to archive or compare across machines:

   ```bash
   scp -r ec2-user@CLIENT:results ./results-remote
   ```

Use private IPs within the VPC, keep clocks roughly in sync (chrony/NTP is
usually on by default) so client and server samples align on the shared time
axis, and open the security group only between the two instances.

## Generating and comparing plots

Plots are generated automatically after each run. To regenerate manually:

```bash
python3 scripts/plot_results.py results/<run-id>
```

Comparison mode (plots every metric against elapsed time, one line per run,
labels from `-label` + mode + concurrency):

```bash
python3 scripts/plot_results.py compare results/runA results/runB results/runC \
  -o results/comparison --labels "m5.large-c16,m5.large-c32,m5.large-c64"
```

Produces `compare_latency.png` (p50/p95/p99 panels), `compare_throughput.png`
(req/s and MiB/s), `compare_cpu.png`, and `compare_memory.png`. Runs with
different durations are fine — the x-axis is elapsed time and each line simply
ends when its run ends. Missing metrics (e.g. server columns when `-admin` is
off) are skipped with a message; the remaining panels still render. The script
forces the headless Agg backend, so it works over plain SSH.

## Metrics reference

`metrics.csv` (one row per measurement second):

| Column | Unit | Meaning |
|---|---|---|
| `elapsed_s` | s | Seconds since measurement start (warmup excluded) |
| `rps` | req/s | HTTP requests completed in that second |
| `payload_mib_s` | MiB/s | HTTP payload throughput (request + response bodies) |
| `logical_mib_s` | MiB/s | Estimated logical bytes accessed (values × value size) |
| `values_per_s` | values/s | Logical values accessed per second (≈ req/s × batch size for batch workloads) |
| `avg_ms`, `p50_ms`, `p95_ms`, `p99_ms` | ms | Latency in that second |
| `requests`, `errors`, `gets`, `puts` | count | Per-second counters |
| `server_cpu_pct` | % of 1 core | Server process CPU |
| `server_system_cpu_pct` | % | Whole-system CPU |
| `server_rss_mib`, `server_heap_mib` | MiB | Server process memory / Go heap |
| `server_mem_available_mib` | MiB | System available memory |
| `server_gc_count`, `server_gc_pause_ms` | count / ms | GC cycles and total pause in the interval |
| `server_disk_read_mib_s`, `server_disk_write_mib_s` | MiB/s | Whole-system disk throughput |
| `server_net_rx_mib_s`, `server_net_tx_mib_s` | MiB/s | Whole-system network throughput (no loopback) |
| `server_psi_cpu_some_avg10`, `server_psi_mem_some_avg10` | % | Pressure-stall information |
| `server_page_faults_per_s`, `server_major_faults_per_s` | faults/s | Process minor/major page faults |
| `server_swap_in_mib_s`, `server_swap_out_mib_s` | MiB/s | System swap throughput (0 unless the guest is paging) |
| `server_ipc` | instr/cycle | Hardware instructions-per-cycle (PMU; empty where unsupported) |
| `server_cache_misses_per_s`, `server_cache_miss_rate` | misses/s, ratio | Hardware cache miss counters (PMU) |
| `client_cpu_pct`, `client_rss_mib` | % / MiB | Load generator process |

`summary.json` records the full effective configuration (seed included, for
reproducibility) plus aggregate results: total/success/error counts, requests/s,
values/s, payload and logical MiB/s, avg/p50/p95/p99/min/max latency, effective
read/write ratio, concurrency, and final server stats.

**Percentiles:** nearest-rank over sorted per-request durations
(`p` → element at rank ⌈p·N⌉). Latencies of **failed** requests are retained in
the percentile population — errors are counted separately, never silently
dropped. Warmup samples are discarded entirely.

## Interpreting results

The plots exist to explain numbers, not just display them. Typical readings:

- **Latency rose while CPU rose** — the server is doing more work per request
  or is contended; check `resources.png` and `latency_vs_cpu.png`.
- **Throughput stopped scaling as concurrency grew** — compare req/s across
  concurrency levels with compare mode; if client CPU is near/above 100% of a
  core per worker thread, the *generator* may be the limit
  (`client_cpu_pct`).
- **Latency spikes coincide with GC pauses** — correlation only; rerun with
  `GOGC=off` (or a much larger GOGC) to test the hypothesis.
- **RSS grew significantly during the run** — check `gc_pressure.png`; steady
  growth with an unchanging dataset usually means the client is retaining
  samples, or the server heap is fragmented.
- **Unexpected disk I/O or memory PSI** — the workload is memory-only; these
  point to other activity on the host, or genuine system memory pressure.
- **Rate-limited runs** (`-rate`) reflect the fixed arrival rate, not the
  server's maximum capacity.

Every run prints an evidence-based `observations.txt` with these heuristics.
They are *possible explanations*, not diagnoses. Do not infer a memory-bandwidth
bottleneck from dataset size or CPU utilization alone. If a resource looks
saturated, state the evidence and design a follow-up (different instance type,
lower concurrency, bigger client) to validate.

### Limitations of the monitoring

- Process CPU assumes `USER_HZ = 100` (true on standard x86/arm64 Linux).
- Disk stats aggregate whole-disk devices; virtualized devices (EBS) report
  what the guest kernel sees, which may differ from the hypervisor.
- Network rates include all non-loopback interfaces and all peers, not just the
  load generator.
- PSI requires Linux ≥ 4.20 and may be missing on older/containerized kernels.
- `runtime.ReadMemStats` briefly stops the world once per interval — sampling
  overhead is small at 1 Hz but not zero.
- Client and server clocks must be roughly synchronized for cross-machine
  alignment (NTP); samples are bucketed by whole elapsed seconds.
- The final partial second of the measurement window is dropped from the
  per-second series (it would look like an artificial dip); summary totals
  still include every request.

## Memory-bound experiment plan (VirtualBox guests)

Goal: show the application is **memory-bound** without accidentally measuring
an I/O-bound or CPU-bound regime. Three one-axis experiments; every run must
pass the evidence checklist below.

### Invariants (stay inside the memory-bound regime)

| Risk | Control |
|---|---|
| io-bound via swap | Working set (`entries × value-size`) stays under the 80 % RAM budget — enforced by the store, the loadgen preflight, and `POST /admin/populate`. Never populate beyond budget. Verify `server_swap_* ≈ 0` and major faults = 0 in every accepted run. |
| io-bound via disk | Server hot path does no disk I/O (values live in RAM only). Loadgen writes CSV on its own machine. |
| cpu-bound (server) | Read-only modes only (`random-read`, `hotset`). Do **not** use `read-write` (PUTs allocate → GC becomes the bottleneck) or `batch-read` (server copies the whole response per request). |
| cpu-bound (client) | Keep `-concurrency` modest (≤ 16 on small VMs). Discard any run where observation #2 fires (client CPU ≥ 90 % of one core). |
| GC noise | Optional: start the server with `GOGC=off` to isolate pure memory effects. |

**PMU on VirtualBox:** the guest PMU is usually not exposed, so in-guest
`hardware.png` shows "no PMU data" and the IPC / cache-miss columns stay
empty. Run `scripts/host_perf.sh` on the **host** (targets the `VBoxHeadless`
PID) to capture IPC and cache-miss rate from outside the guest. Fault and
swap counters still work in-guest (they come from `/proc`, not the PMU).

### E1 — Cache-resident vs DRAM-resident (core proof)

Two **random-read** runs against different resident dataset sizes. The small
set (map buckets + values) fits in LLC; the large set forces every lookup to
DRAM.

| Cell | Entries | Resident set | Mode |
|---|---|---|---|
| A | ~2 048 × 1 KiB | ≈ 2 MiB (fits LLC) | `random-read` |
| B | ~524 288 × 1 KiB | ≈ 512 MiB ≫ LLC | `random-read` |

Fixed: `-concurrency 8`, 30 s + 5 s warmup, 3 repeats each, same `-seed`.

> **Why not `hotset`?** A Go map's bucket array for 500k entries is tens of
> MiB spread across DRAM. Restricting *keys* to a hot range still scatters
> lookups across those buckets, so hotset ≈ random-read on this
> implementation. The clean locality contrast is therefore *small resident
> map vs large resident map*, not key-range hotset.

**Expected signature (B vs A):** p99 rises; **IPC drops** below ~0.8;
**cache-miss rate** (PMU: `cache-misses / cache-references`, *not* page
faults) jumps well above 10 %; server CPU stays well below 100 %; no swap, no
major faults. Low IPC means the CPU is stalled waiting on data — that stall,
under high miss rate and without CPU/disk saturation, *is* the memory-bound
proof.

### E2 — Dataset size sweep (capacity curve)

`random-read`, fixed 1 KiB values, working set ≫ LLC and growing, each size
under the VM budget (example ladder; scale to your RAM):

| Cell | Entries | Working set |
|---|---|---|
| 10 MiB | 10 240 | ≈ 10 MiB |
| 100 MiB | 102 400 | ≈ 100 MiB |
| 512 MiB | 524 288 | ≈ 512 MiB |
| 1 GiB | 1 048 576 | ≈ 1 GiB (only if budget allows) |

3 repeats per size (2 for quick local demos). **Expected:** p99 and miss rate
grow as the set outgrows LLC; IPC falls; throughput plateaus rather than
scaling — capacity bound, still no swap.

### E3 — Concurrency wall

`random-read`, one large dataset (e.g. 512 MiB), `-concurrency`
1 / 4 / 8 / 16 / 32, 3 repeats. **Expected:** throughput plateaus
(memory-latency serialization) while per-request latency grows; server CPU
never pegged; IPC stays low. Contrast with CPU-bound, where throughput would
scale with cores until CPU = 100 %.

### Evidence checklist (per run)

| Signal | Memory-bound? | io-bound? | cpu-bound? |
|---|---|---|---|
| Swap in+out (system) | ≈ 0 | **> 0** | ≈ 0 |
| Major faults (server process) | ≈ 0 | **> 0** | ≈ 0 |
| Server CPU | moderate, not pegged | moderate | **≈ 100 % of cores** |
| Client CPU (obs #2) | < 90 % of a core | < 90 % | **≥ 90 %** |
| IPC (PMU) | **< 0.8** | low or high | high (≥ ~1.5) |
| Cache-miss rate (PMU) | **≥ 10 %** | varies | low (< a few %) |
| p99 small vs large resident set | **much higher on large** | much higher | similar |

IPC = instructions per cycle (CPU stall detector). Cache-miss rate = PMU
`cache-misses / cache-references` (DRAM traffic), **not** page faults —
page faults/swap are only guardrails that prove you did not leave the
memory-bound regime.

### Measured example (bare-metal i5-8250U, client co-located)

2 repeats, 30 s + 5 s warmup, 1 KiB values, `random-read`, seed 42. Client
and server shared the same 8-thread laptop, so client-CPU saturation
(observation #2) appears at high concurrency — on VirtualBox run the client
on the host (or another VM) to avoid that confound.

| Cell | req/s | p50 (ms) | p99 (ms) | server CPU (% of 1 core) | IPC | cache-miss rate | swap | major faults |
|---|---|---|---|---|---|---|---|---|
| E1 small ~2 MiB | 37 862 | 0.187 | 0.738 | 239 | 0.53 | 3.5 % | ≈ 0 | 0 |
| E1 large 512 MiB | 36 740 | 0.191 | 0.771 | 236 | 0.50 | 3.8 % | ≈ 0 | 0 |
| E2 10 MiB | 37 735 | 0.190 | 0.663 | 242 | 0.52 | 3.5 % | ≈ 0 | 0 |
| E2 100 MiB | 36 089 | 0.195 | 0.836 | 233 | 0.51 | 3.9 % | ≈ 0 | 0 |
| E2 512 MiB | 35 736 | 0.198 | 0.777 | 231 | 0.50 | 4.4 % | ≈ 0 | 0 |
| E2 1 GiB | 35 363 | 0.198 | 0.821 | 233 | 0.50 | 4.1 % | ≈ 0 | 0 |
| E3 c1 | 9 817 | 0.098 | 0.164 | 90 | 0.52 | 3.1 % | ≈ 0 | 0 |
| E3 c4 | 25 403 | 0.134 | 0.584 | 185 | 0.49 | 4.6 % | ≈ 0 | 0 |
| E3 c8 | 32 387 | 0.206 | 1.040 | 218 | 0.49 | 5.5 % | ≈ 0 | 0 |
| E3 c16 | 49 829 | 0.254 | 1.524 | 243 | 0.56 | 4.8 % | ≈ 0 | 0 |
| E3 c32 | 57 317 | 0.430 | 2.773 | 244 | 0.58 | 5.7 % | ≈ 0 | 0 |

How to read this as memory-bound:

1. **Not io-bound:** swap ≈ 0 and major faults = 0 in every cell.
2. **Not cpu-bound:** server CPU tops out near 2.4 cores of 8 (≈ 244 % of one
   core); IPC stays ≈ 0.5 (the CPU spends about half its cycles stalled).
3. **E3 is the smoking gun:** concurrency 1→32 multiplies throughput only
   ×5.8 while p99 grows ×17 and server CPU flattens after c8 — classic
   memory-latency serialization, not compute scaling.
4. **E1/E2 capacity effect is mild on this host:** HTTP framing + co-located
   client dominate; on a dedicated VM client the p99 gap between 2 MiB and
   512 MiB widens. Low IPC alone is already sufficient stall evidence.

Runner: `load-tests/memory-bound/scripts/run_memory_bound_experiments.sh`.
Plots: each run's `hardware.png` (IPC / cache misses / faults / swap) plus
`results/e{1,2,3}-compare/compare_{latency,throughput,cpu,memory}.png`.

### Example commands (E1)

```bash
# Cell A: cache-resident (~2 MiB) — populate at server start so PMU baselines
# cover a warm heap
./server -addr :8080 -admin -populate-entries 2048 -populate-value-size 1024
./loadgen -url http://127.0.0.1:8080 -mode random-read \
  -dataset-entries 2048 -value-size 1024 -concurrency 8 \
  -duration 30s -warmup 5s -no-populate -label e1-small

# Cell B: DRAM-resident (512 MiB)
./server -addr :8080 -admin -populate-entries 524288 -populate-value-size 1024
./loadgen -url http://127.0.0.1:8080 -mode random-read \
  -dataset-entries 524288 -value-size 1024 -concurrency 8 \
  -duration 30s -warmup 5s -no-populate -label e1-large

# Compare the two
python3 scripts/plot_results.py compare results/e1-small-run results/e1-large-run \
  -o results/e1-compare --labels "small (LLC),large (DRAM)"
```

A ready-made local runner lives at
`load-tests/memory-bound/scripts/run_memory_bound_experiments.sh`.

## Beyond memory-bound (other bottlenecks)

Vary one axis at a time; keep `-seed` fixed for comparability. These leave
the memory-bound regime above and are useful only as contrast / follow-ups.

1. **Value size** — 64 B / 1 KiB / 16 KiB at fixed dataset bytes. Compare
   `payload_mib_s` against `rps`; large values shift the bound from
   request-rate to bandwidth.
2. **Read ratio** — `read-write` at 100/0, 95/5, 70/30, 0/100. Writes allocate;
   watch GC pause and heap in `gc_pressure.png`.
3. **Sequential access** — `sequential-read` stresses prefetch; a shared
   atomic cursor can become a client-side artifact at high concurrency.
4. **Batch size** — `batch-read` with n = 1/8/64. Compare `rps` against
   `values_per_s`: batching trades per-request latency for values per second.
5. **Rate limiting** — replay one workload at `-rate` 5k/20k/50k to separate
   "latency at offered load" from "latency at saturation".
6. **Swap / budget breach** — deliberately exceed RAM only when you *want*
   to show the fall into io-bound (major faults + swap traffic) or the 507
   budget guard; this is scenario evidence, not a memory-bound run.

### Experiment matrix (instance types × workloads)

Run each cell 3× (or more) and compare with the compare mode. Example for two
instance types:

| Workload (fixed 30s + 5s warmup) | t3.medium | m5.large |
|---|---|---|
| random-read, c=16, 1M × 1KiB | ✓ | ✓ |
| read-write 70/30, c=16, 1M × 1KiB | ✓ | ✓ |
| batch-read n=16, c=16, 1M × 1KiB | ✓ | ✓ |
| hotset 1%, c=16, 1M × 1KiB | ✓ | ✓ |
| random-read, c=64 (scaling point) | ✓ | ✓ |

```bash
./loadgen -url http://SERVER:8080 -mode random-read -dataset-entries 1000000 \
  -value-size 1024 -concurrency 16 -duration 30s -warmup 5s -seed 42 -label t3.medium
# ... switch instance, repeat with -label m5.large, then:
python3 scripts/plot_results.py compare results/t3medium-run results/m5large-run \
  -o results/t3-vs-m5 --labels "t3.medium,m5.large"
```

Remember that burstable instances (t3) accrue CPU credits: run sustained
benchmarks long enough to observe (or disable) credit throttling, and check
`server_cpu_pct` for the tell-tale sawtooth.

## Testing

```bash
cd apps/memory-bound
go test ./...
go test -race ./...

cd ../../load-tests/memory-bound
go test ./...
go test -race ./...
```

The suite covers store semantics (insert/retrieve/overwrite, missing keys,
concurrent access, prepopulation, bounded key space, memory-budget
enforcement), HTTP behavior (batch format, invalid parameters, body limits,
admin gating, counters, concurrent population rejection, 507 budget
responses), workload configuration validation, percentile math, CSV/JSON
export formats, and plot-script behavior on sample data.
