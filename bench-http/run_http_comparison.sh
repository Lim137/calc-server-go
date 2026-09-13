#!/usr/bin/env bash
# Reproduces the HTTP-level comparison referenced in BENCHMARK_NOTES.md.
#
# Builds two Docker images -- one with each calculator_*.go snapshot in
# this directory swapped into internal/calculator/calculator.go -- then
# load-tests both with `hey` across a few rounds that alternate which
# variant goes first, saving raw output under results/<run timestamp>/.
# Requires `hey` (brew install hey) and Docker.
#
# Each invocation gets its own timestamped subdirectory, so re-running
# this never overwrites or mixes with a previous run's saved reports
# (including the ones already committed from earlier runs).
#
# Run from the repository root: ./bench-http/run_http_comparison.sh
# Override ROUNDS/DURATION/CONCURRENCY as env vars, e.g.:
#   ROUNDS=4 ./bench-http/run_http_comparison.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CALC_GO="$REPO_ROOT/go/internal/calculator/calculator.go"
RUN_ID="$(date +%Y%m%dT%H%M%S)"
RESULTS_DIR="$SCRIPT_DIR/results/$RUN_ID"

ROUNDS="${ROUNDS:-3}"
DURATION="${DURATION:-15s}"
CONCURRENCY="${CONCURRENCY:-50}"
SEQ_PORT=18090
FAN_PORT=18091
RUN_CONTAINER="bench-http-run-$RUN_ID"

mkdir -p "$RESULTS_DIR"

# This script overwrites calculator.go and restores it via `git
# checkout --` when done. That restore only knows about the last
# *committed* version -- if calculator.go already had uncommitted
# changes before this script ran, they would be silently lost with no
# way to recover them. Refuse to run rather than risk that.
if [ -n "$(git -C "$REPO_ROOT" status --porcelain -- "$CALC_GO")" ]; then
	echo "error: $CALC_GO has uncommitted changes." >&2
	echo "Commit, stash, or discard them first -- this script overwrites" >&2
	echo "the file and restores it via 'git checkout --', which would" >&2
	echo "silently discard anything not already committed." >&2
	exit 1
fi

cleanup() {
	git -C "$REPO_ROOT" checkout -- "$CALC_GO" 2>/dev/null || true
	# Must match the exact name run_load() gives the container -- an
	# earlier version of this script cleaned up different, stale names
	# here, which meant an interrupted run left a real container behind.
	docker rm -f "$RUN_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

build_image() {
	local variant=$1 tag=$2
	cp "$SCRIPT_DIR/calculator_${variant}.go" "$CALC_GO"
	docker build -f "$REPO_ROOT/build/Dockerfile" -t "$tag" "$REPO_ROOT" \
		> "$RESULTS_DIR/build-${variant}.log" 2>&1
}

run_load() {
	local image=$1 port=$2 out=$3
	docker rm -f "$RUN_CONTAINER" >/dev/null 2>&1 || true
	docker run -d --rm -p "${port}:8080" --name "$RUN_CONTAINER" "$image" \
		> "$RESULTS_DIR/${out}.container_id.txt"
	sleep 1
	curl -s -X POST "http://localhost:${port}/calc?num=1" -o /dev/null \
		-w "warmup status: %{http_code}\n" > "$RESULTS_DIR/${out}.warmup.txt"
	hey -z "$DURATION" -c "$CONCURRENCY" -m POST \
		"http://localhost:${port}/calc?num=5" \
		> "$RESULTS_DIR/${out}.hey.txt" 2>&1
	curl -s "http://localhost:${port}/metrics" > "$RESULTS_DIR/${out}.metrics.txt"
	docker logs "$RUN_CONTAINER" > "$RESULTS_DIR/${out}.server.log" 2>&1
	docker stop "$RUN_CONTAINER" > /dev/null
}

echo "== Building images =="
build_image sequential calc-http-bench-sequential
build_image fanout calc-http-bench-fanout
git checkout -- "$CALC_GO"

echo "== Running $ROUNDS rounds, alternating which variant goes first =="
for i in $(seq 1 "$ROUNDS"); do
	if [ $((i % 2)) -eq 1 ]; then
		echo "--- Round $i: Sequential, then Fanout ---"
		run_load calc-http-bench-sequential "$SEQ_PORT" "round${i}-sequential"
		run_load calc-http-bench-fanout "$FAN_PORT" "round${i}-fanout"
	else
		echo "--- Round $i: Fanout, then Sequential ---"
		run_load calc-http-bench-fanout "$FAN_PORT" "round${i}-fanout"
		run_load calc-http-bench-sequential "$SEQ_PORT" "round${i}-sequential"
	fi
done

echo "== Summary (RPS from each run) =="
grep -H "Requests/sec" "$RESULTS_DIR"/round*.hey.txt

docker rmi calc-http-bench-sequential calc-http-bench-fanout >/dev/null 2>&1 || true
echo "Done. Raw reports in $RESULTS_DIR"
