package calculator

// Three Apply() locking strategies, kept only on this experimental
// branch to decide between them with real numbers instead of
// intuition. Not part of the shipped Calculator.
//
// Each variant is parameterized by opA/opB so the same code can be
// benchmarked against the real add()/sub() pair and against a
// synthetic "both expensive" pair (opA twice) without duplicating the
// locking logic itself.

import (
	"sync"
	"time"
)

type applier interface {
	Apply(num int64) (a, b int64, aElapsed, bElapsed time.Duration)
}

// sequentialCalc is today's shipped design: one mutex, both ops run
// one after the other inside it.
type sequentialCalc struct {
	mu       sync.Mutex
	a, b     int64
	opA, opB func(int64, int64) int64
}

func newSequentialCalc(opA, opB func(int64, int64) int64) applier {
	return &sequentialCalc{opA: opA, opB: opB}
}

func (c *sequentialCalc) Apply(num int64) (a, b int64, aElapsed, bElapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	c.a = c.opA(c.a, num)
	aElapsed = time.Since(start)

	start = time.Now()
	c.b = c.opB(c.b, num)
	bElapsed = time.Since(start)

	return c.a, c.b, aElapsed, bElapsed
}

// splitMutexCalc is the original design: independent locks per value,
// two goroutines per Apply call. Faster in principle when both ops are
// expensive, but gives up sum/sub pair consistency across a concurrent
// Snapshot -- that's a semantic cost, not just a speed number.
type splitMutexCalc struct {
	aMu      sync.Mutex
	a        int64
	bMu      sync.Mutex
	b        int64
	opA, opB func(int64, int64) int64
}

func newSplitMutexCalc(opA, opB func(int64, int64) int64) applier {
	return &splitMutexCalc{opA: opA, opB: opB}
}

func (c *splitMutexCalc) Apply(num int64) (a, b int64, aElapsed, bElapsed time.Duration) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		c.aMu.Lock()
		start := time.Now()
		c.a = c.opA(c.a, num)
		aElapsed = time.Since(start)
		a = c.a
		c.aMu.Unlock()
	}()

	go func() {
		defer wg.Done()
		c.bMu.Lock()
		start := time.Now()
		c.b = c.opB(c.b, num)
		bElapsed = time.Since(start)
		b = c.b
		c.bMu.Unlock()
	}()

	wg.Wait()
	return a, b, aElapsed, bElapsed
}

// fanoutCalc: one mutex for the whole call (so Snapshot consistency is
// preserved exactly like sequentialCalc), but the two ops still run in
// parallel inside the critical section via two goroutines.
type fanoutCalc struct {
	mu       sync.Mutex
	a, b     int64
	opA, opB func(int64, int64) int64
}

func newFanoutCalc(opA, opB func(int64, int64) int64) applier {
	return &fanoutCalc{opA: opA, opB: opB}
}

func (c *fanoutCalc) Apply(num int64) (a, b int64, aElapsed, bElapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		start := time.Now()
		c.a = c.opA(c.a, num)
		aElapsed = time.Since(start)
	}()
	go func() {
		defer wg.Done()
		start := time.Now()
		c.b = c.opB(c.b, num)
		bElapsed = time.Since(start)
	}()
	wg.Wait()

	return c.a, c.b, aElapsed, bElapsed
}

// fanoutSingleGoroutineCalc is a diagnostic-only fourth variant: same
// outer mutex as fanoutCalc, but the work runs sequentially inside a
// single spawned goroutine instead of two parallel ones. If this turns
// out to be as fast as fanoutCalc, the earlier speed gap wasn't really
// about parallelizing add/sub -- it was about running the native call
// in a freshly spawned goroutine rather than directly in the
// long-lived RunParallel worker goroutine.
type fanoutSingleGoroutineCalc struct {
	mu       sync.Mutex
	a, b     int64
	opA, opB func(int64, int64) int64
}

func newFanoutSingleGoroutineCalc(opA, opB func(int64, int64) int64) applier {
	return &fanoutSingleGoroutineCalc{opA: opA, opB: opB}
}

func (c *fanoutSingleGoroutineCalc) Apply(num int64) (a, b int64, aElapsed, bElapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		start := time.Now()
		c.a = c.opA(c.a, num)
		aElapsed = time.Since(start)

		start = time.Now()
		c.b = c.opB(c.b, num)
		bElapsed = time.Since(start)
	}()
	<-done

	return c.a, c.b, aElapsed, bElapsed
}
