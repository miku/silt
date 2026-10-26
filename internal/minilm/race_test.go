//go:build race

package minilm

// raceEnabled skips the heavy numeric tests: instrumented float loops run
// many times slower and the race detector has nothing to find in them.
const raceEnabled = true
