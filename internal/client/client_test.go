package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a Client pointed at srv with zero backoff, so retry
// tests finish in milliseconds.
func newTestClient(t *testing.T, srv *httptest.Server, maxRetries int) *Client {
	t.Helper()
	c, err := New(
		"test-key",
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithMaxRetries(maxRetries),
		WithBackoff(func(int) time.Duration { return 0 }),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestNew_MissingAPIKey: constructing without a key must error with the
// canonical sentinel so the CLI can map to exit 2.
func TestNew_MissingAPIKey(t *testing.T) {
	_, err := New("")
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("want ErrMissingAPIKey, got %v", err)
	}
	if !strings.Contains(err.Error(), "dashboard.exa.ai") {
		t.Errorf("error should point user to key page, got %q", err.Error())
	}
}

// TestSearch_HappyPath: 200 OK with a valid body decodes into SearchResponse.
// Also asserts the outgoing request carries the correct auth header, method,
// path, and marshalled body.
func TestSearch_HappyPath(t *testing.T) {
	var gotBody SearchRequest
	var gotAuthHeader, gotMethod, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("x-api-key")
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"requestId": "req_123",
			"autopromptString": "best vector dbs",
			"results": [
				{"title":"Weaviate","url":"https://weaviate.io","id":"w1","score":0.91},
				{"title":"Qdrant","url":"https://qdrant.tech","id":"q1"}
			]
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 0)
	resp, err := c.Search(context.Background(), SearchRequest{
		Query:      "vector dbs",
		NumResults: 2,
		Type:       "auto",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Request assertions.
	if gotMethod != http.MethodPost {
		t.Errorf("method: want POST, got %s", gotMethod)
	}
	if gotPath != "/search" {
		t.Errorf("path: want /search, got %s", gotPath)
	}
	if gotAuthHeader != "test-key" {
		t.Errorf("x-api-key: want test-key, got %q", gotAuthHeader)
	}
	if gotBody.Query != "vector dbs" || gotBody.NumResults != 2 || gotBody.Type != "auto" {
		t.Errorf("request body mismatch: %+v", gotBody)
	}

	// Response assertions.
	if resp.RequestID != "req_123" {
		t.Errorf("RequestID: %q", resp.RequestID)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("want 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].Title != "Weaviate" {
		t.Errorf("result[0].Title: %q", resp.Results[0].Title)
	}
	if resp.Results[0].Score == nil || *resp.Results[0].Score != 0.91 {
		t.Errorf("result[0].Score: %v", resp.Results[0].Score)
	}
}

// TestSearch_RetryOn429: first call returns 429, second returns 200. Client
// must retry and surface the eventual success.
func TestSearch_RetryOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"ok","url":"https://ok"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 3)
	resp, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("want 2 calls (1 retry), got %d", calls.Load())
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "ok" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

// TestSearch_RetryOn5xxThenExhaust: 500 every time, exhaust retries → APIError.
func TestSearch_RetryOn5xxThenExhaust(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`boom`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 2)
	_, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	if err == nil {
		t.Fatal("want error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("status: %d", apiErr.StatusCode)
	}
	// maxRetries=2 → 3 total attempts.
	if calls.Load() != 3 {
		t.Errorf("want 3 calls, got %d", calls.Load())
	}
}

// TestSearch_NoRetryOn4xx: 401 is non-retriable. Fail immediately.
func TestSearch_NoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 5)
	_, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401 APIError, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("4xx must not retry; got %d calls", calls.Load())
	}
}

// TestSearch_ContextCancel: a context canceled between retries aborts cleanly.
func TestSearch_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c, err := New(
		"k",
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithMaxRetries(5),
		// Backoff long enough that cancel wins the race.
		WithBackoff(func(int) time.Duration { return 200 * time.Millisecond }),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the first response so the sleep-between-retries
	// path is exercised.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err = c.Search(ctx, SearchRequest{Query: "q"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// TestDefaultBackoff_Grows: sanity check on the backoff schedule — it must
// be monotonically non-decreasing and capped.
func TestDefaultBackoff_Grows(t *testing.T) {
	var last time.Duration
	for i := 1; i <= 10; i++ {
		d := defaultBackoff(i)
		if d < last {
			t.Errorf("backoff not monotonic at %d: %v < %v", i, d, last)
		}
		if d > 8*time.Second {
			t.Errorf("backoff exceeds cap at %d: %v", i, d)
		}
		last = d
	}
}
