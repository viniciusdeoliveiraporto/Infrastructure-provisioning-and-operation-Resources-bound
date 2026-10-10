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