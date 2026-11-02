// Package llm defines the text-generation backend that writes the verdict.
//
// There are two implementations: [github.com/miku/silt/llm/ollama] (a local
// Ollama daemon) and [github.com/miku/silt/llm/openai] (any OpenAI-compatible
// chat API).
package llm

import (
	"context"
	"net"
	"net/http"
	"time"
)

// LLM is a backend that turns a prompt into completion text.
type LLM interface {
	// Generate returns a completion for prompt.
	Generate(ctx context.Context, prompt string) (string, error)
	// Label names the backend and the server it talks to, for progress
	// output, e.g. "Ollama at http://localhost:11434".
	Label() string
}

// NewHTTPClient returns the client the backends use: 120 s per request, since
// a local model can take a while to answer, and 5 s to connect.
func NewHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	tr.TLSHandshakeTimeout = 5 * time.Second
	return &http.Client{Timeout: 120 * time.Second, Transport: tr}
}
