package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
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
		// Every request that reaches this handler was routed here as
		// "/calc" by the mux, so this counts all /calc traffic,
		// including requests that fail validation below.
		rps.Record()

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

		_, _, addElapsed, subElapsed := calc.Apply(num)
		cCallLatency.Observe(addElapsed.Seconds())
		rustCallLatency.Observe(subElapsed.Seconds())

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

	if *interval <= 0 {
		fmt.Fprintln(os.Stderr, "--interval must be positive (time.NewTicker panics otherwise)")
		os.Exit(1)
	}

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

	// stop is closed only after Shutdown returns, so the printer's final
	// sum/sub snapshot reflects every request that finished by then. If
	// Shutdown hits its timeout instead of draining cleanly, in-flight
	// handlers may still be running when that snapshot is taken — the
	// log line below says which case happened.
	stop := make(chan struct{})
	printerDone := make(chan struct{})
	go func() {
		periodicPrinter(calc, stop, *interval)
		close(printerDone)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.ListenAndServe()
	}()

	fmt.Printf("Calculator server listening on %s:%d\n", *host, *port)

	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case sig := <-sigCh:
		fmt.Printf("\n%s received, shutting down...\n", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown timed out after 5s (%v) — forcing close, some requests may not have finished\n", err)
			_ = server.Close()
		} else {
			fmt.Println("shutdown complete, all in-flight requests finished")
		}
		<-serveErr // wait for ListenAndServe to actually return
	}

	close(stop)
	<-printerDone // wait for the final sum/sub line to actually print
}
