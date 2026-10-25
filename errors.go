package silt

import "errors"

// HTTPError is a request that was sent and failed: a transport error, a
// non-2xx status, or a response body that could not be read or decoded.
type HTTPError struct {
	// StatusCode is the response status, or 0 if no response arrived.
	StatusCode int
	Err        error
}

func (e *HTTPError) Error() string { return "http request failed: " + e.Err.Error() }
func (e *HTTPError) Unwrap() error { return e.Err }

// HTTPClientError means the shared HTTP client could not be constructed at
// all, as distinct from [HTTPError], a request that was sent and failed.
// Building a net/http client cannot fail, so silt never returns this today;
// it exists so that [IsRetryable] keeps patent's full error taxonomy.
type HTTPClientError struct {
	Err error
}

func (e *HTTPClientError) Error() string { return "failed to build HTTP client: " + e.Err.Error() }
func (e *HTTPClientError) Unwrap() error { return e.Err }

// ParseError is a response that arrived but could not be understood.
type ParseError struct {
	Msg string
}

func (e *ParseError) Error() string { return "failed to parse response: " + e.Msg }

// UnavailableError means a source's search surface is genuinely, persistently
// unavailable — not a transient blip but a wall a retry cannot get past (e.g.
// PyPI's search page is bot-walled to non-browser clients and it has no
// keyless search API). Msg carries accurate, user-facing wording. Distinct
// from [HTTPError] and [ParseError] so the fan-out can skip the retry it would
// waste hitting the same wall.
type UnavailableError struct {
	Msg string
}

func (e *UnavailableError) Error() string { return e.Msg }

// LLMUnreachableError means the LLM endpoint could not be reached. Msg
// carries the address and a hint.
type LLMUnreachableError struct {
	Msg string
}

func (e *LLMUnreachableError) Error() string { return e.Msg }

// LLMRejectedError means the LLM endpoint was reached but rejected the
// request (unknown model, bad key, server error). Msg carries the reason and
// a hint.
type LLMRejectedError struct {
	Msg string
}

func (e *LLMRejectedError) Error() string { return e.Msg }

// EmbeddingError is a failure to load the embedding model or to embed text.
type EmbeddingError struct {
	Msg string
}

func (e *EmbeddingError) Error() string { return "embedding failed: " + e.Msg }

// IsRetryable reports whether a failed source search is worth a second
// attempt. Transient failures (network blips, HTML parse drift) are; a
// persistently unavailable search surface is not — a retry would hit the same
// wall and only add latency. Neither is a client that could not be built.
func IsRetryable(err error) bool {
	var (
		unavailable *UnavailableError
		client      *HTTPClientError
	)
	return !errors.As(err, &unavailable) && !errors.As(err, &client)
}
