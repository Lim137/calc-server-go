# Apply() locking strategy: benchmark notes

This branch exists to answer one question with data instead of
intuition: should `Calculator.Apply()` run the C `add()` and Rust
`sub()` calls sequentially under one lock (the shipped design), or in
parallel somehow? It is intentionally never merged into `go-rewrite`
or `main` — the alternative implementations here were rejected, not
adopted.

## What was compared

Three locking strategies (`go/internal/calculator/bench_variants_test.go`):

- **Sequential** (shipped): one mutex, `add()` then `sub()` one after
  the other inside it.
- **SplitMutexGoroutines**: independent locks per value, two goroutines
  per `Apply()` call. Faster in principle when both native calls are
  expensive, but gives up `sum`/`sub` pair consistency across a
  concurrent `Snapshot()` — a semantic cost, not just a speed number.
- **Fanout**: one mutex for the whole call (so `Snapshot()` consistency
  is preserved exactly like Sequential), but the two native calls run
  in parallel goroutines inside that single critical section.

## Experiment 1: isolated microbenchmark

`go test -bench=BenchmarkApply -benchmem -count=10 -cpu=1,4,8` against
one shared `Calculator` instance per benchmark (`b.RunParallel`), on
macOS/arm64, Go 1.27, Apple M4 — both against the real C/Rust libraries
and a synthetic "both calls expensive" scenario (`callAdd` used for
both operations, since its busy-loop is protected by `volatile` in the
original C source and so is guaranteed not to be optimized away).

Result: on the **real libraries**, `Fanout` was ~20% faster than both
`Sequential` and `SplitMutexGoroutines`, which performed identically to
each other (own run, `-count=10`, `-cpu=1,4,8`, real libraries). An
independent replication (different reviewer, longer `-benchtime=1s`,
5+3=8 combined repeats, `GOMAXPROCS=4`, comparing specifically
`Sequential` vs `Fanout` — not `SplitMutexGoroutines`) confirmed the gap
statistically: `benchstat`, p=0.000, n=8, -19.82% ns/op. On the
**synthetic both-expensive** scenario, `SplitMutexGoroutines` won
instead, by letting different concurrent requests' `add`/`sub` overlap
with each other — something `Fanout`'s single call-wide lock can't do.

A fourth diagnostic variant (same outer lock as `Fanout`, but sequential
inside a single spawned goroutine) showed the same speed as `Sequential`
and `SplitMutexGoroutines`, not `Fanout` — consistent with the `Fanout`
speedup coming from running the two calls in parallel rather than some
side effect of spawning a goroutine per se. This wasn't a fully clean
control, though: the diagnostic variant waits via a channel instead of
`sync.WaitGroup` (same synchronization *idea*, different primitive), so
it doesn't isolate parallelism as the only changed variable.

**The exact mechanism behind the ~20% real-library speedup was not
established.** It's consistent with avoiding scheduler/mutex-contention
overhead, but that wasn't proven — see the caveat below.

## Experiment 2: HTTP-level comparison (the one that mattered)

Before treating the microbenchmark result as a reason to change the
shipped code, the same comparison was run at the actual service level:
two Docker images (Linux/arm64, Go 1.25 — matching `build/Dockerfile`,
not the macOS/Go 1.27 environment of experiment 1), each running
`calculator_server` with one of the two `calculator.go` variants in
`bench-http/calculator_sequential.go` / `bench-http/calculator_fanout.go`,
load-tested with [`hey`](https://github.com/rakyll/hey) at 50
concurrent connections for 15s per run, across 4 rounds that alternate
*which variant goes first* (round 1: Sequential then Fanout; round 2:
Fanout then Sequential; and so on) specifically to rule out an
ordering/warm-up artifact rather than a real difference between the
two implementations.

Reproduce with `ROUNDS=4 ./bench-http/run_http_comparison.sh` (needs
Docker and `hey`; it refuses to run if `calculator.go` has uncommitted
changes, since it overwrites that file and restores it via `git
checkout --`, which would otherwise silently discard anything not
already committed). Each invocation writes into its own timestamped
subdirectory under `bench-http/results/`, so re-running never
overwrites a previous run. Raw output for the run these notes describe
is in `bench-http/results/2026-09-13-alternating-4-rounds/`.

Result: **reversed**, and the direction holds regardless of which
variant ran first in a given round:

| Round | Order | Sequential RPS | Fanout RPS |
|---|---|---|---|
| 1 | Sequential first | 14,188 | 13,157 |
| 2 | Fanout first | 14,475 | 12,292 |
| 3 | Sequential first | 11,982 | 10,642 |
| 4 | Fanout first | 12,511 | 12,145 |

`Sequential` won RPS in all four rounds. Absolute numbers vary
run-to-run (background load on the test machine, Docker/VM
scheduling). `Sequential`'s p50 was also at or below `Fanout`'s in
every round, but the tail (p95/p99) advantage was **not** consistent —
`Fanout` actually had a lower p99 in round 3 and lower p95/p99 in round
4:

| Round | Sequential p50/p95/p99 | Fanout p50/p95/p99 |
|---|---|---|
| 1 | 3.3 / 4.7 / 6.1 ms | 3.6 / 4.9 / 6.6 ms |
| 2 | 3.3 / 4.6 / 5.9 ms | 3.7 / 6.1 / 8.3 ms |
| 3 | 3.9 / 6.2 / 9.5 ms | 4.5 / 6.5 / **9.2** ms |
| 4 | 3.8 / **5.7** / **7.5** ms | 3.9 / **5.5** / **7.2** ms |

So the honest claim is narrower than "wins on everything": `Sequential`
had higher throughput in every round tested, and better median latency
in every round tested; its tail-latency advantage was real in rounds 1
and 2 but did not hold in rounds 3 and 4.

This result was independently reproduced twice more (by a different
reviewer, on separate Docker runs with different warm-up/round
counts and, in one case, genuinely alternating pair order): `Fanout`
was consistently slower by roughly 7–13% depending on the exact run,
never faster. The magnitude varies with run conditions; the direction
does not.

## Conclusion

**The shipped `Sequential` implementation was kept as-is.** `Fanout`
won the isolated microbenchmark but lost the HTTP-level comparison that
actually reflects the deployed service — a microbenchmark result that
doesn't hold up under the real workload isn't a reason to add
complexity. `SplitMutexGoroutines` was not carried into the HTTP-level
comparison at all: it showed no benefit over `Sequential` even in the
isolated microbenchmark on the real libraries, and it additionally
sacrifices `sum`/`sub` consistency for no upside in the scenario that
actually matters here.

**Caveat on causation:** between experiment 1 and experiment 2, the OS
(macOS → Linux), Go version (1.27 → 1.25), and workload (isolated call
vs. full HTTP request handling) all changed at once. It would be
overreaching to claim the reversal is specifically caused by "HTTP
server goroutine scheduling pressure" or any other single factor — that
is a plausible hypothesis, not something these benchmarks isolate or
prove. What the data supports is narrower and still sufficient for the
decision: in the environment and workload that actually matters (the
deployed HTTP service, Linux, Go 1.25), `Sequential` measurably
outperforms `Fanout`.
