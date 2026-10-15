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

- **`services/`**: Per-application deployment files (Nginx site + systemd unit +
  start scripts) used by `io-bound` and `memory-bound`. `cpu-bound` is deployed
  manually (see below).

- **`benchmark/`**: End-to-end benchmark scenarios and SAR collection scripts (e.g. file-upload load runs).

- **`load-tests/`**: Load-testing scenarios used to evaluate application behavior under different levels of concurrency and workload (k6 scripts and the Go memory-bound load generator).

- **`scripts/`**: VM provisioning (`setup-vm.sh`) and resource monitoring (`monitor-sar.sh`) helpers shared by all applications.

## Applications at a Glance

| Application | Profile | Stack | App port (localhost) | Exposed via | Load test |
| --- | --- | --- | --- | --- | --- |
| [`apps/cpu-bound/`](apps/cpu-bound/) | CPU | Python (FastAPI + Uvicorn) | `8001` | Nginx (edited default site) | [`load-tests/cpu-bound.js`](load-tests/cpu-bound.js) (k6) |
| [`apps/io-bound/`](apps/io-bound/) | Disk I/O | Python (FastAPI + aiofiles) | `8003` | Nginx site `services/io-bound/io-bound` (port 80) | [`benchmark/io-bound/`](benchmark/io-bound/) (k6) |
| [`apps/memory-bound/`](apps/memory-bound/) | Memory | Go (in-memory KV HTTP server) | `8080` | Nginx site `services/memory-bound/memory-bound` (port 80) | [`load-tests/memory-bound/`](load-tests/memory-bound/) (Go loadgen + Python plots) |

### CPU-bound — integer factorization

FastAPI service that decomposes integers into prime factors; CPU load is
tunable through the `number` and `iterations` query parameters.

- Endpoints: `GET /`, `/health`, `/metrics`, `/api/factor?number=&iterations=`
- Runbook (Nginx, k6 demands, `sar`): [`apps/cpu-bound/README.md`](apps/cpu-bound/README.md)
- Load script: [`load-tests/cpu-bound.js`](load-tests/cpu-bound.js)

### I/O-bound — file storage API

FastAPI service for experimenting with file I/O performance: asynchronous
chunked upload, download, and listing against local storage.

- Endpoints: `GET /health`, `POST /files`, `GET /files`, `GET /files/{id}/content`
- App: [`apps/io-bound/app/main.py`](apps/io-bound/app/main.py)
- Deployment files: [`services/io-bound/`](services/io-bound/)
- Benchmark (uploads CSV fixtures with k6, exports JSON summaries): [`benchmark/io-bound/run-benchmark.sh`](benchmark/io-bound/run-benchmark.sh)

### Memory-bound — in-memory key-value benchmark

In-memory key-value HTTP server plus an independent load generator for
performance experiments (dataset size, access patterns, concurrency, instance
types), with automatic resource monitoring and Matplotlib plots after every run.

- Server (Go): [`apps/memory-bound/`](apps/memory-bound/) — architecture, API,
  and experiment guide in its [README](apps/memory-bound/README.md).
- Load generator + plotting (Go + Python): [`load-tests/memory-bound/`](load-tests/memory-bound/) —
  workload flags, outputs, and comparison mode in its
  [README](load-tests/memory-bound/README.md).
- Deployment files: [`services/memory-bound/`](services/memory-bound/)

## Provisioning and Operating the VM

Each application has its own configuration. Do the common step once, then
follow only the profile you want to run.

### 0. Common setup (once per VM)

```bash
bash scripts/setup-vm.sh
```

Installs Python, Nginx, and Sysstat, and enables `sysstat` data collection.

### 1. CPU-bound — manual (venv + default Nginx site + foreground process)

No systemd unit and no Nginx site file in the repo: the app runs in the
foreground and Nginx's default site is edited by hand.

```bash
# 1.1 Virtualenv + dependencies
cd apps/cpu-bound
bash scripts/python-env.sh

# 1.2 Point the default Nginx site at the app (port 8001)
sudo nano /etc/nginx/sites-available/default
#   inside  location / { ... }  →  proxy_pass http://127.0.0.1:8001;
sudo systemctl restart nginx

# 1.3 Start the app (foreground; background: nohup bash scripts/init-app.sh > app.log 2>&1 &)
bash scripts/init-app.sh
```

Full details: [`apps/cpu-bound/README.md`](apps/cpu-bound/README.md).

### 2. I/O-bound — systemd + Nginx site (`services/io-bound/`)

| File | Purpose |
| --- | --- |
| `services/io-bound/io-bound` | Nginx site: listen 80 → `127.0.0.1:8003`, `client_max_body_size 100m` |
| `services/io-bound/io-bound.service` | systemd unit: uvicorn on `127.0.0.1:8003` |
| `services/io-bound/start-nginx.sh` | enables the site and reloads Nginx (removes `default`) |
| `services/io-bound/start-service.sh` | enables and starts the unit |

```bash
# 2.1 Virtualenv (the unit runs .venv/bin/uvicorn, so it must exist)
cd apps/io-bound
bash scripts/python-env.sh

# 2.2 Nginx site: copy the file, then let the script enable it
sudo cp services/io-bound/io-bound /etc/nginx/sites-available/io-bound
sudo bash services/io-bound/start-nginx.sh

# 2.3 systemd service
sudo cp services/io-bound/io-bound.service /etc/systemd/system/
sudo bash services/io-bound/start-service.sh

# 2.4 Load test (from the repo root, inside the VM)
bash benchmark/io-bound/run-benchmark.sh
```

Manual alternative (without systemd): `bash apps/io-bound/scripts/init-app.sh`.
Adjust `User=` and the paths in the unit if your VM user/home differ.

### 3. Memory-bound — systemd + Nginx site (`services/memory-bound/`)

| File | Purpose |
| --- | --- |
| `services/memory-bound/memory-bound` | Nginx site: listen 80 → `127.0.0.1:8080`, plus `/demo/` static alias |
| `services/memory-bound/memory-bound.service` | systemd unit: `./server -addr 127.0.0.1:8080 -admin` |
| `services/memory-bound/start-nginx.sh` | copies the site to `sites-available`, enables it, reloads Nginx |
| `services/memory-bound/start-service.sh` | enables and starts the unit |

```bash
# 3.1 Build the server
cd apps/memory-bound
go build -o server ./cmd/server

# 3.2 Nginx site (the script copies + enables + reloads)
sudo bash services/memory-bound/start-nginx.sh

# 3.3 systemd service
sudo cp services/memory-bound/memory-bound.service /etc/systemd/system/
sudo bash services/memory-bound/start-service.sh
```

Full guide (port forwarding, dataset prepopulation, demo page, experiments):
[`apps/memory-bound/README.md`](apps/memory-bound/README.md).

### 4. Resource monitoring (all profiles)

During each load test, collect CPU, memory, and I/O counters on the VM:

```bash
bash scripts/monitor-sar.sh <TEST_ID> <duration_seconds>
# e.g. bash scripts/monitor-sar.sh P1_D1 60
```

Outputs are written to `metrics/` as text files ready to be parsed or plotted.

## Documentation

- [Relatório de infraestrutura e experimentos (Google Docs)](https://docs.google.com/document/d/1IE4x5i314BrOXRzOMBGNLph9f00jGkIg_GoFSW3fs2U/edit?tab=t.hnscrko26v9h)
