package calculator

import (
	"sync"
	"testing"
)

// TestCalculator_ArithmeticIsCorrect exercises the cgo bridge itself:
// this is not really about whether C's add() or Rust's sub() can do
// arithmetic (that's guaranteed by their own source), it's about
// whether *our* type conversions and argument wiring across the C
// boundary (C.int64_t <-> Go int64) are correct.
func TestCalculator_ArithmeticIsCorrect(t *testing.T) {
	c := New()

	if got, _ := c.Add(5); got != 5 {
		t.Errorf("Add(5) = %d, want 5", got)
	}
	if got, _ := c.Add(3); got != 8 {
		t.Errorf("Add(3) after Add(5) = %d, want 8", got)
	}

	if got, _ := c.Sub(5); got != -5 {
		t.Errorf("Sub(5) = %d, want -5", got)
	}
	if got, _ := c.Sub(3); got != -8 {
		t.Errorf("Sub(3) after Sub(5) = %d, want -8", got)
	}
}

// TestCalculator_ConcurrentAddIsRaceFree fires many goroutines at Add
// concurrently and checks the final sum is mathematically exact. Run
// with -race: this is what actually proves the per-value locking is
// correct, not just "looks right on inspection".
func TestCalculator_ConcurrentAddIsRaceFree(t *testing.T) {
	c := New()

	const goroutines = 50
	const perGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				c.Add(1)
			}
		}()
	}
	wg.Wait()

	sum, _ := c.Snapshot()
	want := int64(goroutines * perGoroutine)
	if sum != want {
		t.Errorf("final sum = %d, want %d (lost updates under concurrency)", sum, want)
	}
}
