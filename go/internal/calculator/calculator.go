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

// Calculator holds the running sum/sub totals. sum and sub each have
// their own lock so an Add and a Sub — whether from different callers
// or the two halves of one logical request — never block each other;
// only genuine contention on the *same* value serializes.
type Calculator struct {
	addMu sync.Mutex
	sum   int64

	subMu sync.Mutex
	sub   int64
}

func New() *Calculator {
	return &Calculator{}
}

// Add calls the C add() function with the current running sum and num,
// stores and returns the new sum. elapsed measures only the C call
// itself, not time spent waiting for the lock.
func (c *Calculator) Add(num int64) (result int64, elapsed time.Duration) {
	c.addMu.Lock()
	start := time.Now()
	c.sum = callAdd(c.sum, num)
	elapsed = time.Since(start)
	result = c.sum
	c.addMu.Unlock()
	return
}

// Sub calls the Rust sub() function analogously to Add.
func (c *Calculator) Sub(num int64) (result int64, elapsed time.Duration) {
	c.subMu.Lock()
	start := time.Now()
	c.sub = callSub(c.sub, num)
	elapsed = time.Since(start)
	result = c.sub
	c.subMu.Unlock()
	return
}

// Snapshot returns the current sum and sub totals.
func (c *Calculator) Snapshot() (sum, sub int64) {
	c.addMu.Lock()
	sum = c.sum
	c.addMu.Unlock()

	c.subMu.Lock()
	sub = c.sub
	c.subMu.Unlock()
	return
}

func callAdd(a, b int64) int64 {
	return int64(C.add(C.int64_t(a), C.int64_t(b)))
}

func callSub(a, b int64) int64 {
	return int64(C.sub(C.int64_t(a), C.int64_t(b)))
}
