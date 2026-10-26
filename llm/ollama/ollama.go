// Package ollama is a minimal client for Ollama's generate API
// (localhost:11434/api/generate). It returns a clear, actionable error when
// the daemon is unreachable.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/miku/silt"
	"github.com/miku/silt/llm"
)

// DefaultEndpoint is where a local Ollama daemon listens.
const DefaultEndpoint = "http://localhost:11434"

// DefaultModel is the generation model: qwen3.5 (9B) for quality, or
// qwen2.5:3b if RAM is tight. Override it with --model.
const DefaultModel = "qwen3.5"

// Client talks to the Ollama daemon at Endpoint.
type Client struct {
	Endpoint   string
	Model      string
	HTTPClient *http.Client
}

var _ llm.LLM = (*Client)(nil)

// New returns a client for model on the daemon at endpoint.
func New(endpoint, model string) *Client {
	return &Client{Endpoint: endpoint, Model: model, HTTPClient: llm.NewHTTPClient()}
}

// Label implements [llm.LLM].
func (c *Client) Label() string { return "Ollama" }

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	// Thinking models (qwen3.5 and later) otherwise spend the whole
	// num_predict budget on a reasoning trace that Ollama returns in a
	// separate "thinking" field, leaving "response" empty -- every verdict
	// would silently fall back. Measured: 512 tokens of thinking and no
	// answer, vs 11 tokens with thinking off. The verdict is a small fixed
	// JSON object, so the trace buys nothing. Accepted (HTTP 200, ignored) by
	// non-thinking models like qwen2.5.
	Think   bool    `json:"think"`
	Options options `json:"options"`
}

type options struct {
	Temperature float64 `json:"temperature"`
	NumPredict  int     `json:"num_predict"`
}

// Generate sends prompt to /api/generate and returns the completion text.
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(generateRequest{
		Model:   c.Model,
		Prompt:  prompt,
		Options: options{Temperature: 0, NumPredict: 512},
	})
	if err != nil {
		return "", err
	}
	resp, err := c.post(ctx, body)
	if err != nil {
		return "", &silt.LLMUnreachableError{Msg: fmt.Sprintf(
			"Ollama not reachable at %s (%v). Run `ollama serve` and `ollama pull %s`.", c.Endpoint, err, c.Model)}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &silt.ParseError{Msg: err.Error()}
	}

	// A non-success status (most commonly 404 for a model that hasn't been
	// pulled, or a 5xx from a proxy in front of Ollama) is a recoverable
	// error: surface it as LLMRejected so the caller still renders results
	// instead of aborting. We try to lift Ollama's {"error": "..."} message
	// but never depend on the body being JSON.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var (
			v      any
			reason string
			ok     bool
		)
		if json.Unmarshal(b, &v) == nil {
			reason, ok = errorField(v)
		}
		if !ok {
			reason = fmt.Sprintf("Ollama returned HTTP %d", resp.StatusCode)
		}
		msg := fmt.Sprintf("%s (model `%s`). Run `ollama pull %s`.", reason, c.Model, c.Model)
		if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
			return "", &silt.LLMUnreachableError{Msg: msg}
		}
		return "", &silt.LLMRejectedError{Msg: msg}
	}

	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return "", &silt.ParseError{Msg: err.Error()}
	}
	// Some Ollama builds return 200 with an {"error": ...} body instead.
	if reason, ok := errorField(v); ok {
		return "", &silt.LLMRejectedError{Msg: fmt.Sprintf(
			"%s (model `%s`). Run `ollama pull %s`.", reason, c.Model, c.Model)}
	}
	obj, _ := v.(map[string]any)
	text, ok := obj["response"].(string)
	if !ok {
		return "", &silt.ParseError{Msg: "missing 'response' field"}
	}
	return text, nil
}

func (c *Client) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.HTTPClient.Do(req)
}

// errorField returns the "error" string of a decoded JSON object, if there is
// one.
func errorField(v any) (string, bool) {
	obj, _ := v.(map[string]any)
	s, ok := obj["error"].(string)
	return s, ok
}
