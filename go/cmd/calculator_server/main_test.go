package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"calc-server-go/internal/calculator"
	"calc-server-go/internal/metrics"
)

func newTestHandler() (http.HandlerFunc, *calculator.Calculator, *metrics.RPSWindow) {
	calc := calculator.New()
	rps := metrics.NewRPSWindow()
	return newHandler(calc, rps), calc, rps
}

func TestHandler_ValidRequestAppliesAndReturnsOK(t *testing.T) {
	h, calc, _ := newTestHandler()

	req := httptest.NewRequest(http.MethodPost, "/calc?num=5", nil)
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	sum, sub := calc.Snapshot()
	if sum != 5 || sub != -5 {
		t.Errorf("sum=%d sub=%d, want sum=5 sub=-5", sum, sub)
	}
}

func TestHandler_WrongPath(t *testing.T) {
	h, _, _ := newTestHandler()

	req := httptest.NewRequest(http.MethodPost, "/other?num=5", nil)
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandler_WrongMethod(t *testing.T) {
	h, _, _ := newTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/calc?num=5", nil)
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow header = %q, want %q", got, http.MethodPost)
	}
}

func TestHandler_MissingNum(t *testing.T) {
	h, _, _ := newTestHandler()

	req := httptest.NewRequest(http.MethodPost, "/calc", nil)
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandler_InvalidNum(t *testing.T) {
	h, _, _ := newTestHandler()

	req := httptest.NewRequest(http.MethodPost, "/calc?num=not-a-number", nil)
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestHandler_RecordsRPSForInvalidRequestsToo reproduces the exact
// scenario from the code review: a request that fails validation must
// still count toward the RPS window, since it's still a real /calc
// request the server received.
func TestHandler_RecordsRPSForInvalidRequestsToo(t *testing.T) {
	h, _, rps := newTestHandler()

	valid := httptest.NewRequest(http.MethodPost, "/calc?num=1", nil)
	h(httptest.NewRecorder(), valid)

	missingNum := httptest.NewRequest(http.MethodPost, "/calc", nil)
	h(httptest.NewRecorder(), missingNum)

	wrongMethod := httptest.NewRequest(http.MethodGet, "/calc?num=1", nil)
	h(httptest.NewRecorder(), wrongMethod)

	// Sum across the whole window rather than asserting snap[0] == 3:
	// the three requests above and this Snapshot() call are not
	// guaranteed to land in the same calendar second, so a fixed-index
	// check is flaky right at a second boundary. What this test cares
	// about is that all three got recorded somewhere, not which slot.
	snap := rps.Snapshot()
	var total int64
	for _, v := range snap {
		total += v
	}
	if total != 3 {
		t.Errorf("sum of rps window = %d, want 3 (all three requests counted, including the two invalid ones)", total)
	}
}
