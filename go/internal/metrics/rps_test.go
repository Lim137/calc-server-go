package metrics

import (
	"testing"
	"time"
)

// TestRPSWindow_StaleCellsDoNotLeakIntoReport reproduces the exact bug
// found while designing this window: with uneven traffic, a bucket can
// go untouched for far longer than one 60-second rotation. When it's
// finally reused, its old data must not leak into the report for the
// second that currently maps to that same array slot.
func TestRPSWindow_StaleCellsDoNotLeakIntoReport(t *testing.T) {
	w := NewRPSWindow()

	var current int64 = 1_000_000_000 // arbitrary fixed base second
	w.now = func() time.Time { return time.Unix(current, 0) }

	// A burst of 3 requests at the base second.
	for i := 0; i < 3; i++ {
		w.Record()
	}

	// Jump forward 90 seconds with NO traffic in between — more than a
	// full 60-second rotation, so the bucket touched above is now well
	// past merely "one minute old".
	current += 90

	// One request at the new time.
	w.Record()

	snap := w.Snapshot()

	// 90 % 60 == 30, so "90 seconds ago" and "30 seconds ago" map to the
	// same array slot. That slot's real data is from 90s ago, not 30s
	// ago — it must read as 0 for "seconds_ago=30", not leak the old
	// burst.
	if snap[30] != 0 {
		t.Errorf("snapshot[30] = %d, want 0 (stale bucket from 90s ago must not leak into a different second's report)", snap[30])
	}

	if snap[0] != 1 {
		t.Errorf("snapshot[0] = %d, want 1", snap[0])
	}
}

// TestRPSWindow_IdleSecondsReadZero is the simpler baseline case: a
// second with no requests at all must report 0, not whatever
// leftover value happens to sit in that array slot.
func TestRPSWindow_IdleSecondsReadZero(t *testing.T) {
	w := NewRPSWindow()

	var current int64 = 2_000_000_000
	w.now = func() time.Time { return time.Unix(current, 0) }

	w.Record() // one request at the current second only

	snap := w.Snapshot()
	for offset := 1; offset < 60; offset++ {
		if snap[offset] != 0 {
			t.Errorf("snapshot[%d] = %d, want 0 (no request was recorded then)", offset, snap[offset])
		}
	}
	if snap[0] != 1 {
		t.Errorf("snapshot[0] = %d, want 1", snap[0])
	}
}
