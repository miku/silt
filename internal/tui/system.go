package tui

import (
	"context"
	"os/exec"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
)

// isSafeURL reports whether url may be handed to the system's opener: match
// URLs come from registries, so only http and https links are opened.
func isSafeURL(url string) bool {
	lower := strings.ToLower(url)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

// openURL opens url in the default browser, without waiting for it.
func openURL(url string) error {
	var name string
	args := []string{url}
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name = "xdg-open"
	}
	// Not tied to the TUI's context: the browser outlives silt.
	cmd := exec.CommandContext(context.Background(), name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // only reaps the opener
	return nil
}

// copyText puts text on the system clipboard. On Linux this needs xclip,
// xsel or wl-copy; without one, the caller falls back to OSC 52.
func copyText(text string) error { return clipboard.WriteAll(text) }
