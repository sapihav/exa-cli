package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultBaseURL is the production Exa API endpoint.
const DefaultBaseURL = "https://api.exa.ai"

// userAgent is sent with every request so providers can identify the client.
const userAgent = "exa-cli/0.1.0"

// ErrMissingAPIKey is returned when the Client is constructed without a key.
// The CLI layer maps this to exit code 2 (user/config error).
var ErrMissingAPIKey = errors.New("EXA_API_KEY is not set; get a key at https://dashboard.exa.ai/api-keys")

// APIError represents a non-2xx HTTP response from the Exa API.
// Exit code 1 at the CLI layer.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("exa api error: status=%d body=%s", e.StatusCode, truncate(e.Body, 500))
}

// NetworkError wraps transport-level failures (DNS, TCP, TLS, timeout).
// Exit code 3 at the CLI layer.
type NetworkError struct {
	Err error
}

func (e *NetworkError) Error() string { return fmt.Sprintf("network error: %v", e.Err) }
func (e *NetworkError) Unwrap() error { return e.Err }

// Client is a thin wrapper around net/http for calling the Exa API.
//
// Construct via New. Zero values are not safe to use.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	maxRetries int

	// backoff returns the delay before retry attempt n (1-indexed).
	// Exposed as a field so tests can inject a zero-delay backoff and
	// keep runtime under a second.
	backoff func(attempt int) time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the production endpoint. Used by tests.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithHTTPClient injects a custom http.Client (e.g., with a test transport).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithMaxRetries sets the maximum retry count for 429/5xx responses.
// Total attempts = maxRetries + 1 (the initial attempt).
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithBackoff overrides the exponential-backoff function.
// Primarily for tests; production callers should use the default.
func WithBackoff(f func(attempt int) time.Duration) Option {
	return func(c *Client) { c.backoff = f }
}

// New returns a Client ready to call the Exa API.
// Returns ErrMissingAPIKey if apiKey is empty.
func New(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	c := &Client{
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		maxRetries: 3,
		backoff:    defaultBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Search calls POST /search and decodes the response.
//
// Retries on 429 and 5xx up to Client.maxRetries times with exponential
// backoff. Does NOT retry on 4xx (other than 429) — those are client errors
// and retrying will not help.
func (c *Client) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.backoff(attempt)):
			}
		}

		resp, err := c.doRequest(ctx, body)
		if err != nil {
			// Network errors are retriable up to maxRetries.
			lastErr = &NetworkError{Err: err}
			continue
		}

		// 2xx → parse + return.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			defer resp.Body.Close()
			var out SearchResponse
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				return nil, fmt.Errorf("decode response: %w", err)
			}
			return &out, nil
		}

		// Drain + close body so the connection can be reused.
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		apiErr := &APIError{StatusCode: resp.StatusCode, Body: string(bodyBytes)}

		// Retriable status codes: 429 (rate limit) and 5xx (server error).
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = apiErr
			continue
		}

		// Non-retriable 4xx — fail immediately.
		return nil, apiErr
	}

	return nil, lastErr
}

// doRequest builds and sends a single HTTP request. No retry logic here.
func (c *Client) doRequest(ctx context.Context, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)
	httpReq.Header.Set("x-api-key", c.apiKey)
	return c.httpClient.Do(httpReq)
}

// defaultBackoff returns exponential delays capped at 8s:
// attempt 1 → 500ms, 2 → 1s, 3 → 2s, 4 → 4s, …
func defaultBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := 250 * time.Millisecond * (1 << attempt)
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	return d
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
