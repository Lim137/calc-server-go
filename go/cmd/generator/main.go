package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

func worker(id int, baseURL string, stop <-chan struct{}, interval, timeout time.Duration, okCount, errCount *int64, client *http.Client) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		num := rand.Intn(201) - 100 // -100..100

		q := url.Values{}
		q.Set("num", strconv.Itoa(num))
		reqURL := baseURL + "?" + q.Encode()

		func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
			if err != nil {
				atomic.AddInt64(errCount, 1)
				fmt.Printf("[worker %d] request build failed: %v\n", id, err)
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				atomic.AddInt64(errCount, 1)
				fmt.Printf("[worker %d] request failed: %v\n", id, err)
				return
			}
			defer resp.Body.Close()
			atomic.AddInt64(okCount, 1)
		}()

		if interval > 0 {
			select {
			case <-time.After(interval):
			case <-stop:
				return
			}
		}
	}
}

func main() {
	baseURL := flag.String("url", "http://localhost:8080/calc", "calculator endpoint")
	threads := flag.Int("threads", 10, "number of worker goroutines")
	interval := flag.Duration("interval", 100*time.Millisecond, "pause between requests per worker (0 = as fast as possible)")
	timeout := flag.Duration("timeout", 5*time.Second, "HTTP request timeout")
	flag.Parse()

	stop := make(chan struct{})
	var okCount, errCount int64

	client := &http.Client{}

	var wg sync.WaitGroup
	for i := 0; i < *threads; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(id, *baseURL, stop, *interval, *timeout, &okCount, &errCount, client)
		}(i)
	}

	fmt.Printf("Generator started: %d threads -> %s\n", *threads, *baseURL)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	fmt.Println("\nSIGINT received, stopping generator...")
	close(stop)
	wg.Wait()

	fmt.Printf("Total requests: ok=%d errors=%d\n", atomic.LoadInt64(&okCount), atomic.LoadInt64(&errCount))
}
