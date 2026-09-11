# syntax=docker/dockerfile:1

# ---- Build stage ----
# Everything is built inside a Linux container regardless of the host
# OS, which sidesteps the .so vs .dylib naming difference that shows up
# when building the Rust library directly on macOS.
FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
        gcc \
        curl \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

WORKDIR /src
COPY c_lib/ c_lib/
COPY rust_lib/ rust_lib/
COPY go/ go/

RUN gcc -shared -fPIC -O2 -o /src/libcalculator.so c_lib/calculator.c

RUN cd rust_lib && cargo build --release \
    && cp target/release/libcalculator_rust.so /src/libcalculator_rust.so

RUN cd go && CGO_ENABLED=1 go build -o /out/calculator_server ./cmd/calculator_server

RUN mkdir -p /out && cp /src/libcalculator.so /src/libcalculator_rust.so /out/

# ---- Runtime stage ----
FROM debian:bookworm-slim

WORKDIR /app
COPY --from=builder /out/calculator_server /app/calculator_server
COPY --from=builder /out/libcalculator.so /out/libcalculator_rust.so /app/

ENV LD_LIBRARY_PATH=/app
EXPOSE 8080

ENTRYPOINT ["/app/calculator_server"]
