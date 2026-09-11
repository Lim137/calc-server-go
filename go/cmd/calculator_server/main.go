package main

/*
#cgo CFLAGS: -I${SRCDIR}/../../../c_lib -I${SRCDIR}/../../../rust_lib
#cgo LDFLAGS: -L${SRCDIR}/../../../ -lcalculator -lcalculator_rust -Wl,-rpath,${SRCDIR}/../../../
#include "calculator.h"
#include "rust_calculator.h"
*/
import "C"

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---------------------------------------------------------------------------
// cgo calls
// ---------------------------------------------------------------------------

func callAdd(a, b int64) int64 {
	return int64(C.add(C.int64_t(a), C.int64_t(b)))
}

func callSub(a, b int64) int64 {
	return int64(C.sub(C.int64_t(a), C.int64_t(b)))
}

// ---------------------------------------------------------------------------
// Shared state — separate locks so an add-update and a sub-update from
// different requests (or from the same request, see handleCalc) never
// block each other, only genuine same-value contention does.
// ---------------------------------------------------------------------------

var (
	addMu    sync.Mutex
	sumValue int64

	subMu    sync.Mutex
	subValue int64
)

func snapshotTotals() (sum, sub int64) {
	addMu.Lock()
	sum = sumValue
	addMu.Unlock()

	subMu.Lock()
	sub = subValue
	subMu.Unlock()
	return
}

// ---------------------------------------------------------------------------
// RPS sliding window: 60 buckets, one per second, reused cyclically.
// rpsSeconds[i] records which exact unix second the data in rpsBuckets[i]
// belongs to, so both writers and the /metrics reader can tell a fresh
// bucket from a stale one regardless of how uneven traffic has been.
// ---------------------------------------------------------------------------

var (
	rpsMu      sync.Mutex
	rpsBuckets [60]int64
	rpsSeconds [60]int64
)

func recordRequest() {
	now := time.Now().Unix()
	idx := now % 60

	rpsMu.Lock()
	if rpsSeconds[idx] != now {
		rpsBuckets[idx] = 0
		rpsSeconds[idx] = now
	}
	rpsBuckets[idx]++
	rpsMu.Unlock()
}

// rpsCollector exposes the sliding window as a Prometheus metric vector,
// one sample per "seconds_ago" value (0..59), so it plugs into the same
// registry/exposition pipeline as the latency Summaries below — the whole
// /metrics response is produced by promhttp.Handler, no hand-rolled text.
type rpsCollector struct {
	desc *prometheus.Desc
}

func newRPSCollector() *rpsCollector {
	return &rpsCollector{
		desc: prometheus.NewDesc(
			"http_requests_per_second",
			"Number of /calc requests received during a given second within the last 60 seconds.",
			[]string{"seconds_ago"},
			nil,
		),
	}
}

func (c *rpsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

func (c *rpsCollector) Collect(ch chan<- prometheus.Metric) {
	now := time.Now().Unix()
	for offset := int64(0); offset < 60; offset++ {
		targetSecond := now - offset
		idx := targetSecond % 60
		if idx < 0 {
			idx += 60
		}

		rpsMu.Lock()
		var value float64
		if rpsSeconds[idx] == targetSecond {
			value = float64(rpsBuckets[idx])
		}
		// mismatch -> this bucket belongs to some other second, so there
		// were genuinely 0 requests during targetSecond; value stays 0
		rpsMu.Unlock()

		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, value, strconv.FormatInt(offset, 10))
	}
}

// ---------------------------------------------------------------------------
// Latency metrics for the C and Rust calls specifically (not counting
// mutex wait time — only the call itself).
// ---------------------------------------------------------------------------

var (
	cCallLatency = prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "c_call_duration_seconds",
		Help:       "Execution time of the C add() call.",
		Objectives: map[float64]float64{0.95: 0.01, 0.99: 0.001},
	})

	rustCallLatency = prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "rust_call_duration_seconds",
		Help:       "Execution time of the Rust sub() call.",
		Objectives: map[float64]float64{0.95: 0.01, 0.99: 0.001},
	})
)

// ---------------------------------------------------------------------------
// HTTP handler
// ---------------------------------------------------------------------------

func handleCalc(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/calc" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawNum := r.URL.Query().Get("num")
	if rawNum == "" {
		http.Error(w, "missing 'num' query parameter", http.StatusBadRequest)
		return
	}
	num, err := strconv.ParseInt(rawNum, 10, 64)
	if err != nil {
		http.Error(w, "'num' must be an integer", http.StatusBadRequest)
		return
	}

	recordRequest()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		addMu.Lock()
		start := time.Now()
		sumValue = callAdd(sumValue, num)
		elapsed := time.Since(start)
		addMu.Unlock()
		cCallLatency.Observe(elapsed.Seconds())
	}()

	go func() {
		defer wg.Done()
		subMu.Lock()
		start := time.Now()
		subValue = callSub(subValue, num)
		elapsed := time.Since(start)
		subMu.Unlock()
		rustCallLatency.Observe(elapsed.Seconds())
	}()

	wg.Wait()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// ---------------------------------------------------------------------------
// Periodic totals printer (feature parity with the original Python server;
// unrelated to HTTP/metrics)
// ---------------------------------------------------------------------------

func printTotals(label string) {
	sum, sub := snapshotTotals()
	fmt.Printf("[%s] sum=%d sub=%d\n", label, sum, sub)
}

func periodicPrinter(stop <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			printTotals("periodic")
		case <-stop:
			return
		}
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	host := flag.String("host", "0.0.0.0", "listen host")
	port := flag.Int("port", 8080, "listen port")
	interval := flag.Duration("interval", 5*time.Second, "interval between periodic sum/sub reports")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/calc", handleCalc)

	registry := prometheus.NewRegistry()
	registry.MustRegister(cCallLatency, rustCallLatency, newRPSCollector())
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", *host, *port),
		Handler: mux,
	}

	stop := make(chan struct{})
	go periodicPrinter(stop, *interval)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nSIGINT received, shutting down...")
		printTotals("final")
		close(stop)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	fmt.Printf("Calculator server listening on %s:%d\n", *host, *port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
