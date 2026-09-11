# calc-server-go

A Go rewrite of the `calculator_server` + `generator` test service, originally written in Python. The server updates two counters via calls into external C and Rust libraries, and exposes metrics in Prometheus format.

## Platform support

- **Docker** — tested, works; should be cross-platform (Mac/Linux/Windows with Docker Desktop), since the build always happens inside a Linux container regardless of the host OS. Recommended if reproducibility matters.
- **Local build without Docker** — tested on **macOS**; should work natively on **Linux** (a plain bash script, ordinary `.so` files), but this hasn't been verified on an actual Linux machine. Probably won't build on **Windows** without WSL: `build.sh` is a bash script, and the dynamic-library lookup model itself is fundamentally different there (`.dll` instead of `.so`, no `LD_LIBRARY_PATH`/`DYLD_LIBRARY_PATH`).

## Quick start with Docker

The simplest way to run the server (not the generator — that's a separate utility, not a service) is via Docker: no need to install Go/gcc/Rust locally, the build is fully isolated and reproducible the same way on any host OS (this also sidesteps the `.so`/`.dylib` naming difference between Linux and macOS).

```bash
docker build -f build/Dockerfile -t calc-server-go .
docker run --rm -p 8080:8080 calc-server-go
```

From there, as usual: `curl -X POST "http://localhost:8080/calc?num=5"`, `curl http://localhost:8080/metrics`.

## Requirements for a local build (without Docker)

- Go 1.25+
- gcc (or another C compiler)
- Rust toolchain (`cargo`)

## Build

```bash
./build.sh
```

Builds, in order:
1. `libcalculator.so` — the C library (`c_lib/calculator.c`)
2. `libcalculator_rust.so` — the Rust library (`rust_lib/`)
3. `calculator_server` — the server Go binary
4. `generator` — the load generator Go binary

## Structure

```
build/Dockerfile             builds and packages calculator_server into a container
c_lib/                       C library (add)
rust_lib/                    Rust library (sub)
go/
  cmd/calculator_server/     entry point: HTTP handlers, main()
  cmd/generator/             load generator entry point
  internal/calculator/       the only package that touches cgo —
                              wraps add()/sub() and the sum/sub counters themselves
  internal/metrics/          RPS sliding window (no cgo dependency,
                              tested and built independently of the native code)
```

## Running without Docker

`libcalculator.so`/`libcalculator_rust.so` are built into the repository root, but are not embedded into the binary via rpath (this mechanism behaves differently on macOS vs Linux) — the library search path must be set explicitly when running:

```bash
# Linux
LD_LIBRARY_PATH=. ./calculator_server --port 8080

# macOS
DYLD_LIBRARY_PATH=. ./calculator_server --port 8080
```

Flags: `--host` (default `0.0.0.0`), `--port` (default `8080`), `--interval` (interval for printing the current `sum`/`sub` values to the console, default `5s`).

Load generator (in another terminal):

```bash
./generator --url http://localhost:8080/calc --threads 10 --interval 100ms
```

Flags: `--url`, `--threads` (number of worker goroutines, default `10`), `--interval` (pause between requests per worker, `0` = as fast as possible), `--timeout` (HTTP request timeout).

## Verifying the result

A single request:

```bash
curl -X POST "http://localhost:8080/calc?num=5"
```

Metrics:

```bash
curl http://localhost:8080/metrics
```

The output includes:
- `http_requests_per_second{seconds_ago="0..59"}` — number of `/calc` requests received during each of the last 60 seconds
- `c_call_duration_seconds{quantile="0.95"|"0.99"}` — p95/p99 execution time of the C `add` call
- `rust_call_duration_seconds{quantile="0.95"|"0.99"}` — p95/p99 execution time of the Rust `sub` call

Stop either process with `Ctrl+C` (both shut down gracefully and print their final totals).

## Tests

```bash
cd go
LD_LIBRARY_PATH=.. CGO_ENABLED=1 go test -race ./...
```

(On macOS, use `DYLD_LIBRARY_PATH=..` instead of `LD_LIBRARY_PATH`; the libraries must already be built — run `./build.sh` before testing.)

- `internal/metrics` — needs no C/Rust libraries built at all; tests the RPS sliding window, including a specific edge case: with uneven traffic, a bucket can go unused for longer than one full rotation (60s) and must not "leak" into the report as if it were data for the current window.
- `internal/calculator` — needs the libraries built (tests the cgo integration itself). Two tests: correctness of type conversion across the C boundary, and concurrent correctness of the counter updates under `-race` (many goroutines at once, checking the final sum).
