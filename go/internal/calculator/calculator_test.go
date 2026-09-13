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

	if sum, _, _, _ := c.Apply(5); sum != 5 {
		t.Errorf("Apply(5) sum = %d, want 5", sum)
	}
	if sum, _, _, _ := c.Apply(3); sum != 8 {
		t.Errorf("Apply(3) after Apply(5) sum = %d, want 8", sum)
	}

	_, sub, _, _ := c.Apply(0)
	if sub != -8 {
		t.Errorf("sub after Apply(5), Apply(3), Apply(0) = %d, want -8", sub)
	}
}

// TestCalculator_ConcurrentApplyIsRaceFree fires many goroutines at
// Apply concurrently and checks the final sum/sub are mathematically
// exact and consistent with each other. Run with -race: this is what
// actually proves the locking is correct, not just "looks right on
// inspection".
func TestCalculator_ConcurrentApplyIsRaceFree(t *testing.T) {
	c := New()

	const goroutines = 50
	const perGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				c.Apply(1)
			}
		}()
	}
	wg.Wait()

	sum, sub := c.Snapshot()
	want := int64(goroutines * perGoroutine)
	if sum != want {
		t.Errorf("final sum = %d, want %d (lost updates under concurrency)", sum, want)
	}
	if sub != -want {
		t.Errorf("final sub = %d, want %d (lost updates under concurrency)", sub, -want)
	}
}
