package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapihav/exa-cli/internal/client"
)

// resetSearchFlags resets every flag global to its declared default. Required
// because Cobra flags are package-level; a test that sets --category would
// leak into the next test otherwise. Intentionally centralized (rather than
// per-test t.Cleanup) to avoid drift the next time a flag is added.
func resetSearchFlags(t *testing.T) {
	t.Helper()
	flagNumResults = 10
	flagType = "auto"
	flagMaxRetries = 3
	flagCategory = ""
	flagIncludeDomains = nil
	flagExcludeDomains = nil
	flagStartPublished = ""
	flagEndPublished = ""
	flagStartCrawl = ""
	flagEndCrawl = ""
	flagIncludeText = nil
	flagExcludeText = nil
	flagUserLocation = ""
	flagModeration = false
	flagSearchText = false
	flagSearchSummary = false
	flagSearchHL = 0
	flagSearchSubpages = 0
	flagSearchDryRun = false
}

// TestWriteJSON_Envelope verifies the M1.5 stdout contract: successful search
// output is wrapped in a schema_version=1 envelope with provider/command/
// elapsed_ms and a nested result object.
func TestWriteJSON_Envelope(t *testing.T) {
	// writeJSON sends to stdout unless --out is set; easiest to round-trip
	// through a temp file so we don't have to swap os.Stdout.
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	prevOut := flagOut
	flagOut = path
	t.Cleanup(func() { flagOut = prevOut })

	score := 0.91
	resp := &client.SearchResponse{
		RequestID: "req_test",
		Results: []client.SearchResult{
			{Title: "Weaviate", URL: "https://weaviate.io", Score: &score},
		},
	}

	env := envelope{
		SchemaVersion: "1",
		Provider:      "exa",
		Command:       "search",
		ElapsedMs:     42,
		Result:        resp,
	}
	if err := writeJSON(env); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	// Must be valid JSON with the canonical envelope fields at the top level.
	var decoded struct {
		SchemaVersion string          `json:"schema_version"`
		Provider      string          `json:"provider"`
		Command       string          `json:"command"`
		ElapsedMs     int64           `json:"elapsed_ms"`
		Result        json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw=%s", err, raw)
	}

	if decoded.SchemaVersion != "1" {
		t.Errorf("schema_version: want 1, got %q", decoded.SchemaVersion)
	}
	if decoded.Provider != "exa" {
		t.Errorf("provider: want exa, got %q", decoded.Provider)
	}
	if decoded.Command != "search" {
		t.Errorf("command: want search, got %q", decoded.Command)
	}
	if decoded.ElapsedMs != 42 {
		t.Errorf("elapsed_ms: want 42, got %d", decoded.ElapsedMs)
	}

	// Nested result must decode back to SearchResponse unchanged.
	var gotResp client.SearchResponse
	if err := json.Unmarshal(decoded.Result, &gotResp); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if gotResp.RequestID != "req_test" {
		t.Errorf("result.requestId: %q", gotResp.RequestID)
	}
	if len(gotResp.Results) != 1 || gotResp.Results[0].Title != "Weaviate" {
		t.Errorf("result.results mismatch: %+v", gotResp.Results)
	}
}

// TestBuildSearchRequest_DefaultsBackCompat is the M1/M2 back-compat guard:
// with only `--num-results` and `--type` set, the serialized body must match
// M1's three-field shape byte-for-byte. If this breaks, downstream agents
// pinned to the old contract break.
func TestBuildSearchRequest_DefaultsBackCompat(t *testing.T) {
	resetSearchFlags(t)
	flagNumResults = 5
	flagType = "keyword"

	req, err := buildSearchRequest("vector dbs")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"query":"vector dbs","numResults":5,"type":"keyword"}`
	if string(raw) != want {
		t.Errorf("back-compat body drifted:\n got: %s\nwant: %s", raw, want)
	}
}

// TestBuildSearchRequest_AllFilters exercises every M3 flag simultaneously and
// pins the resulting JSON shape. This is the golden request the agents
// contract on.
func TestBuildSearchRequest_AllFilters(t *testing.T) {
	resetSearchFlags(t)
	flagNumResults = 3
	flagType = "neural"
	flagCategory = "research_paper"
	flagIncludeDomains = []string{"arxiv.org", "nature.com"}
	flagExcludeDomains = []string{"example.com"}
	flagStartPublished = "2026-01-01"
	flagEndPublished = "2026-04-19"
	flagStartCrawl = "2026-02-01"
	flagEndCrawl = "2026-04-01"
	flagIncludeText = []string{"transformer", "attention"}
	flagExcludeText = []string{"press release"}
	flagUserLocation = "US"
	flagModeration = true
	flagSearchText = true
	flagSearchSummary = true
	flagSearchHL = 3
	flagSearchSubpages = 2

	req, err := buildSearchRequest("attention is all you need")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Pin the exact top-level fields we expect to see. Using a round-trip
	// into a map is slightly looser than byte-comparison (avoids coupling
	// to struct field ordering) while still catching accidental drift.
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	expectKeys := []string{
		"query", "numResults", "type", "category",
		"includeDomains", "excludeDomains",
		"startPublishedDate", "endPublishedDate",
		"startCrawlDate", "endCrawlDate",
		"includeText", "excludeText",
		"userLocation", "moderation", "contents",
	}
	for _, k := range expectKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q in serialized body: %s", k, raw)
		}
	}

	// Spot-check a nested value and the contents object.
	if got["userLocation"] != "US" {
		t.Errorf("userLocation: %v", got["userLocation"])
	}
	contents, ok := got["contents"].(map[string]any)
	if !ok {
		t.Fatalf("contents not a map: %v", got["contents"])
	}
	if contents["text"] != true || contents["summary"] != true {
		t.Errorf("contents text/summary: %+v", contents)
	}
	if contents["highlights"].(float64) != 3 || contents["subpages"].(float64) != 2 {
		t.Errorf("contents highlights/subpages: %+v", contents)
	}
}

// TestBuildSearchRequest_NoContentsObjectWhenUnset guards the regression where
// a zero-valued *SearchContents would still emit `"contents":{}` on the wire.
// Exa treats an empty contents object as "return text by default" — not what
// the user asked for when they didn't set any enrichment flags.
func TestBuildSearchRequest_NoContentsObjectWhenUnset(t *testing.T) {
	resetSearchFlags(t)

	req, err := buildSearchRequest("q")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if req.Contents != nil {
		t.Errorf("Contents should be nil when no enrichment flags set, got %+v", req.Contents)
	}

	raw, _ := json.Marshal(req)
	if strings.Contains(string(raw), `"contents"`) {
		t.Errorf("unexpected contents field in body: %s", raw)
	}
}

// TestBuildSearchRequest_Validation covers each validation branch. Consolidated
// into one table-driven test because they share the same reset/build pattern.
func TestBuildSearchRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		wantSub string
	}{
		{
			name:    "invalid type",
			setup:   func() { flagType = "bogus" },
			wantSub: "invalid --type",
		},
		{
			name:    "invalid category",
			setup:   func() { flagCategory = "planets" },
			wantSub: "invalid --category",
		},
		{
			name:    "num-results too low",
			setup:   func() { flagNumResults = 0 },
			wantSub: "--num-results must be >= 1",
		},
		{
			name:    "negative retries",
			setup:   func() { flagMaxRetries = -1 },
			wantSub: "--max-retries",
		},
		{
			name:    "bad start-published shape",
			setup:   func() { flagStartPublished = "2026/01/01" },
			wantSub: "--start-published",
		},
		{
			name:    "bad end-crawl shape",
			setup:   func() { flagEndCrawl = "yesterday" },
			wantSub: "--end-crawl",
		},
		{
			name: "too many include-text",
			setup: func() {
				flagIncludeText = []string{"a", "b", "c", "d", "e", "f"}
			},
			wantSub: "--include-text",
		},
		{
			name: "too many exclude-text",
			setup: func() {
				flagExcludeText = []string{"a", "b", "c", "d", "e", "f"}
			},
			wantSub: "--exclude-text",
		},
		{
			name:    "negative highlights",
			setup:   func() { flagSearchHL = -1 },
			wantSub: "--highlights",
		},
		{
			name:    "negative subpages",
			setup:   func() { flagSearchSubpages = -2 },
			wantSub: "--subpages",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetSearchFlags(t)
			tc.setup()
			_, err := buildSearchRequest("q")
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}

// TestBuildSearchRequest_ValidCategories asserts every category listed in the
// spec is accepted. Prevents the enum map from silently drifting from the
// user-facing docs.
func TestBuildSearchRequest_ValidCategories(t *testing.T) {
	cats := []string{
		"research_paper", "news", "pdf", "github", "tweet", "movie",
		"song", "personal_site", "linkedin_profile", "financial_report",
		"company", "people",
	}
	for _, cat := range cats {
		resetSearchFlags(t)
		flagCategory = cat
		if _, err := buildSearchRequest("q"); err != nil {
			t.Errorf("category %q rejected: %v", cat, err)
		}
	}
}

// TestSearch_DryRunRedactsAPIKey: like contents --dry-run, search must render
// the full planned body but never the API key. This is the security invariant
// from CLAUDE.md / ROADMAP §4.
func TestSearch_DryRunRedactsAPIKey(t *testing.T) {
	resetSearchFlags(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")

	prevOut, prevPretty := flagOut, flagPretty
	flagOut, flagPretty = path, true
	t.Cleanup(func() { flagOut, flagPretty = prevOut, prevPretty })

	t.Setenv("EXA_API_KEY", "sk-super-secret-do-not-leak")

	flagCategory = "company"
	flagIncludeDomains = []string{"stripe.com"}
	flagUserLocation = "US"
	flagSearchText = true
	flagSearchSummary = true

	req, err := buildSearchRequest("Stripe")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := writeSearchDryRun(req); err != nil {
		t.Fatalf("writeSearchDryRun: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "sk-super-secret-do-not-leak") {
		t.Fatalf("API key leaked in dry-run output:\n%s", raw)
	}
	if !strings.Contains(string(raw), "[REDACTED]") {
		t.Errorf("expected [REDACTED] placeholder in dry-run output:\n%s", raw)
	}

	// Structural assertions — documented output contract.
	var plan struct {
		SchemaVersion string `json:"schema_version"`
		Command       string `json:"command"`
		DryRun        bool   `json:"dry_run"`
		Request       struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Body    json.RawMessage   `json:"body"`
		} `json:"request"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &plan); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if plan.SchemaVersion != "1" || plan.Command != "search" || !plan.DryRun {
		t.Errorf("envelope mismatch: %+v", plan)
	}
	if plan.Request.Method != "POST" {
		t.Errorf("method: %q", plan.Request.Method)
	}
	if !strings.HasSuffix(plan.Request.URL, "/search") {
		t.Errorf("url: %q", plan.Request.URL)
	}
	if plan.Request.Headers["x-api-key"] != "[REDACTED]" {
		t.Errorf("x-api-key not redacted: %q", plan.Request.Headers["x-api-key"])
	}

	// Body must round-trip back to a SearchRequest with our flags intact.
	var gotBody client.SearchRequest
	if err := json.Unmarshal(plan.Request.Body, &gotBody); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if gotBody.Category != "company" || gotBody.UserLocation != "US" {
		t.Errorf("body fields lost: %+v", gotBody)
	}
	if gotBody.Contents == nil || !gotBody.Contents.Text || !gotBody.Contents.Summary {
		t.Errorf("contents missing in dry-run body: %+v", gotBody.Contents)
	}
}

// TestSearch_GoldenRequestWireFormat is the end-to-end wire-format assertion:
// spin up a mock /search server, drive the client with a full filter
// payload, and assert the exact JSON the server received. Guards against any
// future struct tag drift that would silently change what agents put on the
// wire.
func TestSearch_GoldenRequestWireFormat(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"requestId":"req_1","results":[{"title":"A","url":"https://a","text":"body","summary":"s","highlights":["h1"]}]}`))
	}))
	defer srv.Close()

	c, err := client.New("k", client.WithBaseURL(srv.URL), client.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}

	req := client.SearchRequest{
		Query:              "hello",
		NumResults:         2,
		Type:               "neural",
		Category:           "company",
		IncludeDomains:     []string{"stripe.com"},
		StartPublishedDate: "2026-01-01",
		IncludeText:        []string{"payments"},
		UserLocation:       "US",
		Moderation:         true,
		Contents: &client.SearchContents{
			Text:       true,
			Highlights: 2,
		},
	}
	resp, err := c.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Server-side body: pin the wire shape for a realistic composite request.
	var got map[string]any
	if err := json.Unmarshal(gotBody, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"query", "numResults", "type", "category", "includeDomains", "startPublishedDate", "includeText", "userLocation", "moderation", "contents"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing wire field %q in: %s", k, gotBody)
		}
	}
	if _, present := got["excludeDomains"]; present {
		t.Errorf("excludeDomains must be omitted when unset: %s", gotBody)
	}

	// Response side: enrichment fields on results must decode.
	if len(resp.Results) != 1 {
		t.Fatalf("want 1 result, got %d", len(resp.Results))
	}
	r0 := resp.Results[0]
	if r0.Text != "body" || r0.Summary != "s" || len(r0.Highlights) != 1 {
		t.Errorf("enrichment fields lost: %+v", r0)
	}
}

// TestMapClientError covers the three branches: APIError -> exit 1,
// NetworkError -> exit 3, anything else -> exit 1 with generic message.
// Each branch also writes to stderr, so we swap os.Stderr to capture it and
// assert the human-readable line agents log-scrape on.
func TestMapClientError(t *testing.T) {
	tests := []struct {
		name     string
		in       error
		wantCode int
		wantErr  string
	}{
		{"api error", &client.APIError{StatusCode: 401, Body: "bad key"}, ExitAPIError, "status=401"},
		{"network error", &client.NetworkError{Err: io.EOF}, ExitNetworkErr, "network error"},
		{"generic", io.ErrUnexpectedEOF, ExitAPIError, "unexpected EOF"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Swap stderr so the test output stays clean and we can
			// inspect what the CLI would print to a user.
			r, w, _ := os.Pipe()
			prev := os.Stderr
			os.Stderr = w
			t.Cleanup(func() { os.Stderr = prev })

			err := mapClientError(tc.in)
			w.Close()

			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(r)

			ec, ok := err.(*exitCodeError)
			if !ok {
				t.Fatalf("want *exitCodeError, got %T", err)
			}
			if ec.code != tc.wantCode {
				t.Errorf("code: got %d, want %d", ec.code, tc.wantCode)
			}
			if !strings.Contains(buf.String(), tc.wantErr) && !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("missing %q in stderr %q or err %q", tc.wantErr, buf.String(), err)
			}
		})
	}
}

// TestRunSearch_EmptyQuery exercises the top-of-func validation path. Uses
// cobra's RunE directly with an empty-string arg.
func TestRunSearch_EmptyQuery(t *testing.T) {
	resetSearchFlags(t)
	// Silence stderr so the "query cannot be empty" line doesn't pollute
	// test output.
	prev := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { w.Close(); os.Stderr = prev })

	err := runSearch(searchCmd, []string{""})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want config error, got %v", err)
	}
}

// TestRunSearch_InvalidFlagBubblesUp: any buildSearchRequest failure must
// surface as a config error (exit 2), not an API call.
func TestRunSearch_InvalidFlagBubblesUp(t *testing.T) {
	resetSearchFlags(t)
	flagCategory = "not-real"

	prev := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { w.Close(); os.Stderr = prev })

	err := runSearch(searchCmd, []string{"q"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want config error, got %v", err)
	}
}

// TestRunSearch_HappyPath drives runSearch end-to-end against a mock server
// via the newSearchClient seam. This is the only way to exercise the success
// branch in runSearch without standing up the real API; it also asserts that
// the M3 filters survive the full buildRequest -> HTTP round-trip.
func TestRunSearch_HappyPath(t *testing.T) {
	resetSearchFlags(t)
	flagCategory = "news"
	flagIncludeDomains = []string{"nytimes.com"}
	flagSearchText = true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"requestId":"req_rs","results":[{"title":"T","url":"https://x","text":"inline"}]}`))
	}))
	defer srv.Close()

	// Swap the client factory to point at httptest instead of api.exa.ai.
	prevFactory := newSearchClient
	newSearchClient = func() (*client.Client, error) {
		return client.New("k", client.WithBaseURL(srv.URL), client.WithHTTPClient(srv.Client()))
	}
	t.Cleanup(func() { newSearchClient = prevFactory })

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	prevOut := flagOut
	flagOut = path
	t.Cleanup(func() { flagOut = prevOut })

	searchCmd.SetContext(context.Background())
	if err := runSearch(searchCmd, []string{"elections"}); err != nil {
		t.Fatalf("runSearch: %v", err)
	}

	// Envelope round-trip.
	raw, _ := os.ReadFile(path)
	var env struct {
		Command string          `json:"command"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Command != "search" {
		t.Errorf("command: %q", env.Command)
	}
	var resp client.SearchResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Text != "inline" {
		t.Errorf("enrichment lost: %+v", resp.Results)
	}
}

// TestRunSearch_APIErrorExits1: an API error from the server must map to
// ExitAPIError (1), not bubble up as a raw error.
func TestRunSearch_APIErrorExits1(t *testing.T) {
	resetSearchFlags(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	prevFactory := newSearchClient
	newSearchClient = func() (*client.Client, error) {
		return client.New("k", client.WithBaseURL(srv.URL), client.WithHTTPClient(srv.Client()))
	}
	t.Cleanup(func() { newSearchClient = prevFactory })

	// Swallow stderr.
	prevErr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { w.Close(); os.Stderr = prevErr })

	searchCmd.SetContext(context.Background())
	err := runSearch(searchCmd, []string{"q"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitAPIError {
		t.Fatalf("want ExitAPIError, got %v", err)
	}
}

// TestRunSearch_MissingAPIKey: no EXA_API_KEY -> ExitConfigError (2) with
// the canonical missing-key message.
func TestRunSearch_MissingAPIKey(t *testing.T) {
	resetSearchFlags(t)
	// Do not override newSearchClient; we want the real factory that reads
	// EXA_API_KEY. Clearing it should cause client.New to return
	// ErrMissingAPIKey.
	t.Setenv("EXA_API_KEY", "")

	prevErr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { w.Close(); os.Stderr = prevErr })

	err := runSearch(searchCmd, []string{"q"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
}

// TestRunSearch_DryRunShortCircuits: --dry-run must not require EXA_API_KEY
// (no network call is made). Regression guard for the ordering of the
// dry-run check vs the apiKey lookup in runSearch.
func TestRunSearch_DryRunShortCircuits(t *testing.T) {
	resetSearchFlags(t)
	flagSearchDryRun = true
	t.Setenv("EXA_API_KEY", "") // explicitly unset

	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	prevOut := flagOut
	flagOut = path
	t.Cleanup(func() { flagOut = prevOut })

	if err := runSearch(searchCmd, []string{"q"}); err != nil {
		t.Fatalf("runSearch dry-run: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dry-run output not written: %v", err)
	}
}

// TestWriteJSON_BadOutPath: writing to a non-creatable path returns a
// user/config error. Covers the --out error branch.
func TestWriteJSON_BadOutPath(t *testing.T) {
	prev := flagOut
	// A path inside a non-existent dir that we don't have permission to
	// create is the most portable bad-path.
	flagOut = "/this/path/should/never/exist/out.json"
	t.Cleanup(func() { flagOut = prev })

	// Swallow the stderr message from userError.
	prevErr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { w.Close(); os.Stderr = prevErr })

	err := writeJSON(envelope{SchemaVersion: "1"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

// TestSearch_CommandRegistered ensures every M3 flag is wired onto the
// `search` subcommand so `exa search --help` surfaces them.
func TestSearch_CommandRegistered(t *testing.T) {
	got, _, err := rootCmd.Find([]string{"search"})
	if err != nil {
		t.Fatalf("find search: %v", err)
	}
	for _, name := range []string{
		"num-results", "type", "max-retries",
		"category", "include-domain", "exclude-domain",
		"start-published", "end-published", "start-crawl", "end-crawl",
		"include-text", "exclude-text", "user-location", "moderation",
		"text", "summary", "highlights", "subpages", "dry-run",
	} {
		if got.Flag(name) == nil {
			t.Errorf("missing --%s flag on search command", name)
		}
	}
}
