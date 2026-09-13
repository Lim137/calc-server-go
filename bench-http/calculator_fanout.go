// Package calculator wraps the native C add() and Rust sub() calls and
// the running totals they maintain. This is the only package in the
// module that touches cgo — isolating it here means the rest of the
// codebase (and its tests) don't need the C/Rust libraries built just
// to compile or run.
package calculator

/*
#cgo CFLAGS: -I${SRCDIR}/../../../c_lib -I${SRCDIR}/../../../rust_lib
#cgo LDFLAGS: -L${SRCDIR}/../../../ -lcalculator -lcalculator_rust
#include "calculator.h"
#include "rust_calculator.h"
*/
import "C"

import (
	"sync"
	"time"
)

// Calculator holds the running sum/sub totals, guarded by a single
// lock. Both totals are updated together in Apply so a concurrent
// Snapshot always sees a pair that belongs to the same set of applied
// requests, never sum reflecting one more request than sub.
type Calculator struct {
	mu  sync.Mutex
	sum int64
	sub int64
}

func New() *Calculator {
	return &Calculator{}
}

// Apply calls both the C add() function and the Rust sub() function
// with num, atomically updating sum and sub together, and returns the
// new totals plus how long each native call itself took (lock wait is
// excluded — each timer starts only after the lock is already held).
// The two calls run in parallel goroutines inside the critical
// section: since only one Apply/Snapshot can be active at a time
// anyway, this doesn't cost any consistency, and the isolated
// microbenchmark on this branch showed it faster than running them
// one after another. See BENCHMARK_NOTES.md for why this variant was
// NOT shipped despite that result.
func (c *Calculator) Apply(num int64) (sum, sub int64, addElapsed, subElapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		start := time.Now()
		c.sum = callAdd(c.sum, num)
		addElapsed = time.Since(start)
	}()
	go func() {
		defer wg.Done()
		start := time.Now()
		c.sub = callSub(c.sub, num)
		subElapsed = time.Since(start)
	}()
	wg.Wait()

	return c.sum, c.sub, addElapsed, subElapsed
}

// Snapshot returns the current sum and sub totals as a consistent pair.
func (c *Calculator) Snapshot() (sum, sub int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sum, c.sub
}

func callAdd(a, b int64) int64 {
	return int64(C.add(C.int64_t(a), C.int64_t(b)))
}

func callSub(a, b int64) int64 {
	return int64(C.sub(C.int64_t(a), C.int64_t(b)))
}
