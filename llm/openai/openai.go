// Package openai is a backend for OpenAI-compatible chat-completions APIs.
//
// It talks to any server that implements POST {base}/chat/completions:
// OpenAI, OpenRouter, Groq, vLLM, LM Studio, llama.cpp, and others. The base
// URL is set with --api-base (ending in /v1); auth, when the server needs it,
// comes from --api-key or the OPENAI_API_KEY environment variable.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/miku/silt"
	"github.com/miku/silt/internal/textutil"
	"github.com/miku/silt/llm"
)

// Client talks to the chat endpoint under BaseURL.
type Client struct {
	BaseURL string
	Model   string
	// APIKey, if not nil, is sent as a bearer token.
	APIKey     *string
	HTTPClient *http.Client
}

var _ llm.LLM = (*Client)(nil)

// New returns a client for model on the API at baseURL.
func New(baseURL, model string, apiKey *string) *Client {
	return &Client{BaseURL: baseURL, Model: model, APIKey: apiKey, HTTPClient: llm.NewHTTPClient()}
}

// Label implements [llm.LLM].
func (c *Client) Label() string { return "OpenAI API" }

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	// A reasoning model spends this budget on its trace before it writes any
	// answer: the same prompt measured 347 completion tokens with thinking on
	// against 11 with it off. 512 left no room for the verdict, so the run
	// silently fell back to the no-LLM path. There is no portable way to turn
	// thinking off here -- an unknown parameter is a hard 400 on strict
	// servers -- so the fix is headroom.
	MaxTokens           int `json:"max_tokens,omitempty"`
	MaxCompletionTokens int `json:"max_completion_tokens,omitempty"`
}

// Generate implements [llm.LLM].
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	messages := []message{{Role: "user", Content: prompt}}
	temperature := 0.0
	status, body, err := c.post(ctx, chatRequest{Model: c.Model, Messages: messages, Temperature: &temperature, MaxTokens: 2048})
	if err != nil {
		return "", err
	}

	// OpenAI's own reasoning models (o-series, GPT-5) reject both of the
	// parameters above: max_tokens must be max_completion_tokens, and
	// temperature must be left at its default. Every other OpenAI-compatible
	// server in the wild -- vLLM, llama.cpp, LM Studio, Ollama's shim --
	// speaks the older spelling and may not know the new one, so we cannot
	// simply switch. Retry once, driven by the server's own complaint rather
	// than by sniffing the model name (proxies rename models freely), and
	// only for this documented incompatibility.
	if status == http.StatusBadRequest && rejectsOurParameters(body) {
		status, body, err = c.post(ctx, chatRequest{Model: c.Model, Messages: messages, MaxCompletionTokens: 2048})
		if err != nil {
			return "", err
		}
	}

	// A non-2xx is recoverable (bad model, missing/invalid key, server
	// down): surface the API's error message so the run degrades to a
	// search-only result instead of aborting.
	if status < 200 || status > 299 {
		reason, ok := str(decode(body), "error", "message")
		if !ok {
			reason = fmt.Sprintf("HTTP %d", status)
		}
		msg := fmt.Sprintf("%s (model `%s`). Check --model and --api-key.", reason, c.Model)
		if status >= 500 && status <= 599 {
			return "", &silt.LLMUnreachableError{Msg: msg}
		}
		return "", &silt.LLMRejectedError{Msg: msg}
	}

	// A 200 with an unexpected body (non-JSON proxy page, empty choices, null
	// content) is the server misbehaving, not our bug: treat it as
	// LLMRejected so the run still degrades to a search-only result.
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return "", &silt.LLMRejectedError{Msg: fmt.Sprintf(
			"API at %s returned a non-JSON response (model `%s`).", c.BaseURL, c.Model)}
	}
	content, ok := str(v, "choices", 0, "message", "content")
	if !ok {
		return "", &silt.LLMRejectedError{Msg: fmt.Sprintf("API returned no message content (model `%s`).", c.Model)}
	}
	return content, nil
}

// post sends one chat-completions request and returns the status and the raw
// body.
func (c *Client) post(ctx context.Context, r chatRequest) (int, []byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return 0, nil, err
	}
	resp, err := c.send(ctx, b)
	if err != nil {
		return 0, nil, &silt.LLMUnreachableError{Msg: fmt.Sprintf(
			"OpenAI-compatible API at %s not reachable (%v). Check --api-base.", c.BaseURL, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, &silt.ParseError{Msg: err.Error()}
	}
	return resp.StatusCode, body, nil
}

func (c *Client) send(ctx context.Context, body []byte) (*http.Response, error) {
	url := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != nil {
		req.Header.Set("Authorization", "Bearer "+*c.APIKey)
	}
	return c.HTTPClient.Do(req)
}

// rejectsOurParameters reports whether a 400's body says the server refused
// max_tokens or temperature.
//
// It matches the error text rather than the model name because proxies
// rename models freely (openai/gpt-5, azure/o3, a local alias), so the
// server's own complaint is the only reliable signal.
func rejectsOurParameters(body []byte) bool {
	msg, ok := str(decode(body), "error", "message")
	if !ok {
		msg = string(body)
	}
	msg = textutil.ToLower(msg)
	return strings.Contains(msg, "max_completion_tokens") ||
		(strings.Contains(msg, "temperature") && strings.Contains(msg, "unsupported"))
}

// decode returns body as a JSON value, or nil if it isn't JSON.
func decode(body []byte) any {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return nil
	}
	return v
}

// str looks up the string at path, a sequence of object keys and array
// indexes, in the JSON value v, like serde_json's v["a"][0]["b"].as_str():
// a missing key or index, or a value of another type, gives false.
func str(v any, path ...any) (string, bool) {
	for _, p := range path {
		switch p := p.(type) {
		case string:
			obj, _ := v.(map[string]any)
			v = obj[p]
		case int:
			arr, _ := v.([]any)
			if p >= len(arr) {
				return "", false
			}
			v = arr[p]
		}
	}
	s, ok := v.(string)
	return s, ok
}
