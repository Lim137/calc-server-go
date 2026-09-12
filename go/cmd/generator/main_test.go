package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoOneRequest_SuccessCountsAsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := doOneRequest(ctx, srv.URL+"?num=5", srv.Client()); err != nil {
		t.Errorf("doOneRequest() = %v, want nil", err)
	}
}

// TestDoOneRequest_NotFoundCountsAsError reproduces the exact bug from
// the first review round: client.Do() returns a nil error for a 404
// (that's not a network failure), so status must be checked explicitly.
func TestDoOneRequest_NotFoundCountsAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := doOneRequest(ctx, srv.URL, srv.Client()); err == nil {
		t.Error("doOneRequest() = nil, want an error for a 404 response")
	}
}

func TestDoOneRequest_ServerErrorCountsAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := doOneRequest(ctx, srv.URL, srv.Client()); err == nil {
		t.Error("doOneRequest() = nil, want an error for a 500 response")
	}
}

// TestDoOneRequest_TruncatedBodyCountsAsError reproduces a body-read
// failure: the server advertises more bytes than it actually sends,
// then hangs up mid-body, so io.Copy hits an unexpected EOF.
func TestDoOneRequest_TruncatedBodyCountsAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort")
		buf.Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := doOneRequest(ctx, srv.URL, srv.Client()); err == nil {
		t.Error("doOneRequest() = nil, want an error for a truncated body")
	}
}

func TestDoOneRequest_NetworkFailureCountsAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	unreachableURL := srv.URL
	srv.Close() // closed before use: connection refused

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := doOneRequest(ctx, unreachableURL, http.DefaultClient); err == nil {
		t.Error("doOneRequest() = nil, want an error when the server is unreachable")
	}
}
