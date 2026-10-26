//go:build race

package rank

// raceEnabled skips the heavy numeric tests, whose instrumented float loops
// run many times slower; the race detector has nothing to find in them.
const raceEnabled = true
