#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "== Building C library =="
gcc -shared -fPIC -O2 -o libcalculator.so c_lib/calculator.c
echo "  -> libcalculator.so"

echo "== Building Rust library =="
(cd rust_lib && cargo build --release)
cp rust_lib/target/release/libcalculator_rust.so .
echo "  -> libcalculator_rust.so"

echo "== Building Go binaries =="
(cd go && CGO_ENABLED=1 go build -o ../calculator_server ./cmd/calculator_server)
echo "  -> calculator_server"
(cd go && go build -o ../generator ./cmd/generator)
echo "  -> generator"

echo "Build complete."
echo "Run: ./calculator_server [--port 8080]"
echo "Then: ./generator [--url http://localhost:8080/calc]"
echo "Metrics: curl http://localhost:8080/metrics"
