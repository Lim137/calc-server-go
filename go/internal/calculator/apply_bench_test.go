package calculator

// Perf/alloc comparison of the three Apply() locking strategies.
// Not part of the shipped code -- exists only on this branch to make
// the design decision with data.
//
// Run: go test -bench=BenchmarkApply -benchmem -count=10 -cpu=1,4,8 ./internal/calculator/...
// (needs the C/Rust libraries built first, same as the other calculator tests)

import "testing"

func BenchmarkApply(b *testing.B) {
	scenarios := []struct {
		name     string
		opA, opB func(int64, int64) int64
	}{
		// The actual shipped C add() / Rust sub() pair -- this is the
		// real-world result for the decision.
		{"RealLibs", callAdd, callSub},
		// Synthetic: both ops do the real, non-optimized-away C busy
		// loop. Tests the "what if both were genuinely expensive"
		// hypothesis without touching the Rust library or relying on
		// disassembly -- callAdd's cost is guaranteed real because its
		// loop is behind `volatile` in the original C source.
		{"SyntheticBothExpensive", callAdd, callAdd},
	}

	variants := []struct {
		name    string
		newCalc func(opA, opB func(int64, int64) int64) applier
	}{
		{"Sequential", newSequentialCalc},
		{"SplitMutexGoroutines", newSplitMutexCalc},
		{"Fanout", newFanoutCalc},
		{"FanoutSingleGoroutine", newFanoutSingleGoroutineCalc},
	}

	for _, sc := range scenarios {
		for _, v := range variants {
			b.Run(sc.name+"/"+v.name, func(b *testing.B) {
				// One shared instance for all parallel workers --
				// without this there would be no lock contention to
				// measure at all.
				c := v.newCalc(sc.opA, sc.opB)

				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					var n int64
					for pb.Next() {
						n++
						c.Apply(n)
					}
				})
			})
		}
	}
}

// Ground-truth sanity check: bare native call cost, no locking at all,
// single goroutine. Used only to sanity-check the Apply() numbers
// above against a known baseline.
func BenchmarkBareCall(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		var x int64
		for i := 0; i < b.N; i++ {
			x = callAdd(x, 1)
		}
		_ = x
	})
	b.Run("Sub", func(b *testing.B) {
		var x int64
		for i := 0; i < b.N; i++ {
			x = callSub(x, 1)
		}
		_ = x
	})
}
