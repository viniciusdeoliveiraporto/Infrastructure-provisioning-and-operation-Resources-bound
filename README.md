# Infrastructure-provisioning-and-operation-Resources-bound

CPU-bound, I/O-bound, and memory-bound web applications, with the infrastructure,
load tests, and monitoring needed to provision and operate them under different
resource profiles.

## Project Structure

The project is organized into directories that separate the web applications,
execution infrastructure, benchmarks, and load tests, facilitating the
development and performance analysis of applications with different
computational profiles.

### Directory Organization

```text
.
├── apps/
│   ├── cpu-bound/
│   ├── io-bound/
│   └── memory-bound/
├── services/
│   ├── io-bound/
│   └── memory-bound/
├── benchmark/
│   └── io-bound/
├── load-tests/
│   ├── cpu-bound.js
│   └── memory-bound/
├── scripts/
└── README.md
```

- **`apps/`**: Contains the three backend web applications, each targeting a specific computational profile:
  - `cpu-bound/`: An application focused on CPU-intensive processing (prime factorization).
  - `io-bound/`: An application focused on input/output (I/O) operations (file upload/download on disk).
  - `memory-bound/`: An application focused on memory-intensive operations (in-memory key-value store).

- **`services/`**: systemd unit files and helper scripts to run the applications as VM services behind Nginx.

- **`benchmark/`**: End-to-end benchmark scenarios and SAR collection scripts (e.g. file-upload load runs).

- **`load-tests/`**: Load-testing scenarios used to evaluate application behavior under different levels of concurrency and workload (k6 scripts and the Go memory-bound load generator).

- **`scripts/`**: VM provisioning (`setup-vm.sh`) and resource monitoring (`monitor-sar.sh`) helpers shared by all applications.

## Applications at a Glance

| Application | Profile | Stack | Port (localhost) | Load test |
| --- | --- | --- | --- | --- |
| [`apps/cpu-bound/`](apps/cpu-bound/) | CPU | Python (FastAPI + Uvicorn) | `8001` | [`load-tests/cpu-bound.js`](load-tests/cpu-bound.js) (k6) |
| [`apps/io-bound/`](apps/io-bound/) | Disk I/O | Python (FastAPI + aiofiles) | `8003` | [`benchmark/io-bound/`](benchmark/io-bound/) (k6) |
| [`apps/memory-bound/`](apps/memory-bound/) | Memory | Go (in-memory KV HTTP server) | `8080` | [`load-tests/memory-bound/`](load-tests/memory-bound/) (Go loadgen + Python plots) |

All applications expose a `GET /health` endpoint and are fronted by Nginx as a
reverse proxy on the VM; each app also provides `GET /metrics` (CPU) or an
admin/metrics API (memory-bound) for resource observation.

### CPU-bound — integer factorization

FastAPI service that decomposes integers into prime factors; CPU load is
tunable through the `number` and `iterations` query parameters.

- Runbook, Nginx setup, k6 demands and `sar` monitoring:
  [`apps/cpu-bound/README.md`](apps/cpu-bound/README.md).
- Load script: [`load-tests/cpu-bound.js`](load-tests/cpu-bound.js).

```bash
bash scripts/setup-vm.sh
cd apps/cpu-bound && bash scripts/python-env.sh && bash scripts/init-app.sh
k6 run -e VUS=10 -e ITERATIONS=5 load-tests/cpu-bound.js
```

### I/O-bound — file storage API

FastAPI service for experimenting with file I/O performance: asynchronous
chunked upload (`POST /files`), download (`GET /files/{id}/content`) and
listing (`GET /files`) against local storage.

- App: [`apps/io-bound/app/main.py`](apps/io-bound/app/main.py).
- systemd/Nginx helpers: [`services/io-bound/`](services/io-bound/).
- Benchmark (uploads CSV fixtures with k6, exports JSON summaries):
  [`benchmark/io-bound/run-benchmark.sh`](benchmark/io-bound/run-benchmark.sh).

```bash
cd apps/io-bound && bash scripts/python-env.sh && bash scripts/init-app.sh
bash benchmark/io-bound/run-benchmark.sh
```

### Memory-bound — in-memory key-value benchmark

The memory-bound profile is implemented as an in-memory key-value HTTP server
plus an independent load generator for performance experiments (dataset size,
access patterns, concurrency, instance types), with automatic resource
monitoring and Matplotlib plots after every run:

- Server (Go): [`apps/memory-bound/`](apps/memory-bound/) — architecture, API,
  and experiment guide in its [README](apps/memory-bound/README.md).
- Load generator + plotting (Go + Python): [`load-tests/memory-bound/`](load-tests/memory-bound/) —
  workload flags, outputs, and comparison mode in its
  [README](load-tests/memory-bound/README.md).
- systemd/Nginx helpers: [`services/memory-bound/`](services/memory-bound/).

## Provisioning and Operating the VM

```bash
# 1. Install system packages (Python, Nginx, Sysstat) and enable data collection
bash scripts/setup-vm.sh

# 2. Create the virtualenv for a Python application
cd apps/cpu-bound && bash scripts/python-env.sh   # or apps/io-bound

# 3. Build the Go memory-bound server
cd apps/memory-bound && go build -o server ./cmd/server

# 4. Install and start a service (requires the unit files in services/)
sudo cp services/memory-bound/memory-bound.service /etc/systemd/system/
bash services/memory-bound/start-service.sh
bash services/memory-bound/start-nginx.sh
```

### Resource monitoring (sar)

During each load test, collect CPU, memory, and I/O counters on the VM:

```bash
bash scripts/monitor-sar.sh <TEST_ID> <duration_seconds>
# e.g. bash scripts/monitor-sar.sh P1_D1 60
```

Outputs are written to `metrics/` as text files ready to be parsed or plotted.

## Documentation

- [Relatório de infraestrutura e experimentos (Google Docs)](https://docs.google.com/document/d/1IE4x5i314BrOXRzOMBGNLph9f00jGkIg_GoFSW3fs2U/edit?tab=t.hnscrko26v9h)
