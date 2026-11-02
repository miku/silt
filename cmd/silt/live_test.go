//go:build live

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestJSONOutputIsValidJSON hits the network: go test -tags live.
func TestJSONOutputIsValidJSON(t *testing.T) {
	stdout, stderr, code := runSilt(t, "--json", "--keyword-only", "cli tool that kills a process on a given port")
	// The exit code reflects the verdict (0 Open, 1 Crowded, 2 Saturated), so
	// only check that the process didn't fail outright.
	if code > 2 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("stdout should be valid JSON: %v", err)
	}
	for _, key := range []string{"schema_version", "query", "verdict", "matches"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("JSON should have %q", key)
		}
	}
}

// TestCheckSources hits the network: go test -tags live.
func TestCheckSources(t *testing.T) {
	stdout, stderr, code := runSilt(t, "--check-sources", "--sources", "crates-io,npm")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"Searching each source once for \"json\"", "crates.io", "npm", "reached, ", "2 of 2 sources reached."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}
