# Infrastructure-provisioning-and-operation-Resources-bound
CPU-bound, I/O-bound, and memory-bound web applications

## Project Structure

The project is organized into directories that separate the web applications, execution infrastructure, and load tests, facilitating the development and performance analysis of applications with different computational profiles.

### Directory Organization

```text
.
├── apps/
│   ├── cpu-bound/
│   ├── memory-bound/
│   └── io-bound/
├── infrastructure/
└── load-tests/
```

- **`apps/`**: Contains the three backend web applications to be developed, each targeting a specific computational profile:
  - `cpu-bound/`: An application focused on CPU-intensive processing.
  - `memory-bound/`: An application focused on memory-intensive operations.
  - `io-bound/`: An application focused on input/output (I/O) operations, such as file access, database queries, and network communication.

- **`infrastructure/`**: Intended for configuration files and resources required to run and manage the application environment.

- **`load-tests/`**: Intended for load-testing scenarios and scripts used to evaluate application behavior under different levels of concurrency and workload.

## Memory-bound KV benchmark

The memory-bound profile is implemented as an in-memory key-value HTTP server
plus an independent load generator for performance experiments (dataset size,
access patterns, concurrency, instance types), with automatic resource
monitoring and Matplotlib plots after every run:

- Server (Go): [`apps/memory-bound/`](apps/memory-bound/) — architecture, API,
  and experiment guide in its [README](apps/memory-bound/README.md).
- Load generator + plotting (Go + Python): [`load-tests/memory-bound/`](load-tests/memory-bound/) —
  workload flags, outputs, and comparison mode in its
  [README](load-tests/memory-bound/README.md).