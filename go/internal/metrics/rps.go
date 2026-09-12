// Package metrics has no dependency on the calculator package or cgo —
// it can be built and tested without the C/Rust libraries present.
package metrics

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// RPSWindow tracks how many requests were recorded during each of the
// last 60 seconds, using a fixed 60-slot ring indexed by unix-second %
// 60. Each slot remembers exactly which second it currently holds data
// for, so both Record and Snapshot can tell a fresh slot from a stale
// one no matter how uneven traffic has been — a slot last touched
// several minutes ago (not just "one rotation ago") is still correctly
// recognized as stale rather than reported as if it belonged to the
// current window.
type RPSWindow struct {
	mu      sync.Mutex
	buckets [60]int64
	seconds [60]int64

	desc *prometheus.Desc

	now func() time.Time // overridable in tests
}

func NewRPSWindow() *RPSWindow {
	return &RPSWindow{
		desc: prometheus.NewDesc(
			"http_requests_per_second",
			"Number of /calc requests received during a given second within the last 60 seconds.",
			[]string{"seconds_ago"},
			nil,
		),
		now: time.Now,
	}
}

// Record marks one request as having happened at the current time.
func (w *RPSWindow) Record() {
	now := w.now().Unix()
	idx := now % 60

	w.mu.Lock()
	if w.seconds[idx] != now {
		w.buckets[idx] = 0
		w.seconds[idx] = now
	}
	w.buckets[idx]++
	w.mu.Unlock()
}

// Snapshot returns, for each of the last 60 seconds, how many requests
// were recorded during that exact second. Index 0 is the current
// second, index 59 is 59 seconds ago. A second with no matching bucket
// (nothing recorded then, or the slot holds stale data from an earlier
// rotation) correctly reads 0.
func (w *RPSWindow) Snapshot() [60]int64 {
	now := w.now().Unix()

	w.mu.Lock()
	buckets := w.buckets
	seconds := w.seconds
	w.mu.Unlock()

	var out [60]int64
	for offset := int64(0); offset < 60; offset++ {
		targetSecond := now - offset
		idx := targetSecond % 60
		if idx < 0 {
			idx += 60
		}

		if seconds[idx] == targetSecond {
			out[offset] = buckets[idx]
		}
	}
	return out
}

// Describe and Collect implement prometheus.Collector, so RPSWindow can
// be registered directly alongside the latency Summaries and exposed
// through the same promhttp.Handler — no hand-written metrics text.

func (w *RPSWindow) Describe(ch chan<- *prometheus.Desc) {
	ch <- w.desc
}

func (w *RPSWindow) Collect(ch chan<- prometheus.Metric) {
	snap := w.Snapshot()
	for offset, value := range snap {
		ch <- prometheus.MustNewConstMetric(w.desc, prometheus.GaugeValue, float64(value), strconv.Itoa(offset))
	}
}
