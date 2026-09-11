package main

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

	"calc-server-go/internal/calculator"
	"calc-server-go/internal/metrics"
)

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

func newHandler(calc *calculator.Calculator, rps *metrics.RPSWindow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		rps.Record()

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			_, elapsed := calc.Add(num)
			cCallLatency.Observe(elapsed.Seconds())
		}()

		go func() {
			defer wg.Done()
			_, elapsed := calc.Sub(num)
			rustCallLatency.Observe(elapsed.Seconds())
		}()

		wg.Wait()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}

func periodicPrinter(calc *calculator.Calculator, stop <-chan struct{}, interval time.Duration) {
	print := func(label string) {
		sum, sub := calc.Snapshot()
		fmt.Printf("[%s] sum=%d sub=%d\n", label, sum, sub)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			print("periodic")
		case <-stop:
			print("final")
			return
		}
	}
}

func main() {
	host := flag.String("host", "0.0.0.0", "listen host")
	port := flag.Int("port", 8080, "listen port")
	interval := flag.Duration("interval", 5*time.Second, "interval between periodic sum/sub reports")
	flag.Parse()

	calc := calculator.New()
	rps := metrics.NewRPSWindow()

	mux := http.NewServeMux()
	mux.HandleFunc("/calc", newHandler(calc, rps))

	registry := prometheus.NewRegistry()
	registry.MustRegister(cCallLatency, rustCallLatency, rps)
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", *host, *port),
		Handler: mux,
	}

	stop := make(chan struct{})
	go periodicPrinter(calc, stop, *interval)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nSIGINT received, shutting down...")
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
