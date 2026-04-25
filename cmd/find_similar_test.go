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

// resetFSFlags resets every find-similar flag global to its declared default.
// Cobra flag globals are package-level, so a leak from one test would silently
// pollute the next; reset is centralized here to avoid drift as flags are
// added.
func resetFSFlags(t *testing.T) {
	t.Helper()
	flagFSNumResults = 10
	flagFSMaxRetries = 3
	flagFSExcludeSource = false
	flagFSCategory = ""
	flagFSIncludeDomains = nil
	flagFSExcludeDomains = nil
	flagFSStartPublished = ""
	flagFSEndPublished = ""
	flagFSStartCrawl = ""
	flagFSEndCrawl = ""
	flagFSIncludeText = nil
	flagFSExcludeText = nil
	flagFSUserLocation = ""
	flagFSModeration = false
	flagFSText = false
	flagFSSummary = false
	flagFSHighlights = 0
	flagFSSubpages = 0
	flagFSLivecrawl = ""
	flagFSDryRun = false
}

// silenceStderr swaps os.Stderr for the duration of the test so userError /
// stderr logging from the command path doesn't pollute go test output.
func silenceStderr(t *testing.T) {
	t.Helper()
	prev := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() { w.Close(); os.Stderr = prev })
}

// TestResolveFindSimilarURL covers arg, "-" stdin, no-arg fallback to stdin,
// and whitespace handling. Multi-line stdin uses only the first non-empty
// line because /findSimilar takes a single URL.
func TestResolveFindSimilarURL(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"arg", []string{"https://exa.ai"}, "", "https://exa.ai"},
		{"trim arg", []string{"  https://exa.ai  "}, "", "https://exa.ai"},
		{"dash stdin", []string{"-"}, "https://exa.ai\nhttps://other.example\n", "https://exa.ai"},
		{"dash stdin skip blank", []string{"-"}, "\n\n  https://exa.ai\n", "https://exa.ai"},
		{"no args, no stdin", nil, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveFindSimilarURL(tc.args, strings.NewReader(tc.stdin))
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBuildFindSimilarRequest_Defaults: with only the URL set, the body must
// serialize to a minimal {"url","numResults"} pair. Guards the wire shape.
func TestBuildFindSimilarRequest_Defaults(t *testing.T) {
	resetFSFlags(t)
	req, err := buildFindSimilarRequest("https://exa.ai")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := json.Marshal(req)
	want := `{"url":"https://exa.ai","numResults":10}`
	if string(raw) != want {
		t.Errorf("body drifted:\n got: %s\nwant: %s", raw, want)
	}
}

// TestBuildFindSimilarRequest_AllFilters exercises every flag together and
// pins the resulting top-level keys + nested contents object.
func TestBuildFindSimilarRequest_AllFilters(t *testing.T) {
	resetFSFlags(t)
	flagFSNumResults = 5
	flagFSExcludeSource = true
	flagFSCategory = "research_paper"
	flagFSIncludeDomains = []string{"arxiv.org"}
	flagFSExcludeDomains = []string{"example.com"}
	flagFSStartPublished = "2026-01-01"
	flagFSEndPublished = "2026-04-25"
	flagFSStartCrawl = "2026-02-01"
	flagFSEndCrawl = "2026-04-01"
	flagFSIncludeText = []string{"transformer"}
	flagFSExcludeText = []string{"press release"}
	flagFSUserLocation = "US"
	flagFSModeration = true
	flagFSText = true
	flagFSSummary = true
	flagFSHighlights = 3
	flagFSSubpages = 2
	flagFSLivecrawl = "fallback"

	req, err := buildFindSimilarRequest("https://arxiv.org/abs/2307.06435")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := json.Marshal(req)

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{
		"url", "numResults", "excludeSourceDomain", "category",
		"includeDomains", "excludeDomains",
		"startPublishedDate", "endPublishedDate", "startCrawlDate", "endCrawlDate",
		"includeText", "excludeText", "userLocation", "moderation", "contents",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing wire key %q in: %s", k, raw)
		}
	}
	if got["excludeSourceDomain"] != true {
		t.Errorf("excludeSourceDomain: %v", got["excludeSourceDomain"])
	}
	contents, ok := got["contents"].(map[string]any)
	if !ok {
		t.Fatalf("contents not a map: %v", got["contents"])
	}
	if contents["text"] != true || contents["summary"] != true {
		t.Errorf("contents text/summary: %+v", contents)
	}
}

// TestBuildFindSimilarRequest_NoContentsWhenUnset guards the same regression
// as search.go — `"contents":{}` must not appear when no enrichment knob is set.
func TestBuildFindSimilarRequest_NoContentsWhenUnset(t *testing.T) {
	resetFSFlags(t)
	req, err := buildFindSimilarRequest("https://exa.ai")
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

// TestBuildFindSimilarRequest_Validation covers each validation branch.
func TestBuildFindSimilarRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		wantSub string
	}{
		{"num-results too low", func() { flagFSNumResults = 0 }, "--num-results"},
		{"negative retries", func() { flagFSMaxRetries = -1 }, "--max-retries"},
		{"invalid category", func() { flagFSCategory = "planets" }, "--category"},
		{"bad start-published", func() { flagFSStartPublished = "2026/01/01" }, "--start-published"},
		{"bad end-published", func() { flagFSEndPublished = "yesterday" }, "--end-published"},
		{"bad start-crawl", func() { flagFSStartCrawl = "1d ago" }, "--start-crawl"},
		{"bad end-crawl", func() { flagFSEndCrawl = "tomorrow" }, "--end-crawl"},
		{"too many include-text", func() { flagFSIncludeText = []string{"a", "b", "c", "d", "e", "f"} }, "--include-text"},
		{"too many exclude-text", func() { flagFSExcludeText = []string{"a", "b", "c", "d", "e", "f"} }, "--exclude-text"},
		{"negative highlights", func() { flagFSHighlights = -1 }, "--highlights"},
		{"negative subpages", func() { flagFSSubpages = -1 }, "--subpages"},
		{"invalid livecrawl", func() { flagFSLivecrawl = "sometimes" }, "--livecrawl"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetFSFlags(t)
			tc.setup()
			_, err := buildFindSimilarRequest("https://exa.ai")
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}

// TestRunFindSimilar_RequiresURL: no arg + empty stdin -> ExitConfigError with
// the "URL is required" message.
func TestRunFindSimilar_RequiresURL(t *testing.T) {
	resetFSFlags(t)
	silenceStderr(t)

	findSimilarCmd.SetIn(strings.NewReader(""))
	findSimilarCmd.SetContext(context.Background())
	err := runFindSimilar(findSimilarCmd, nil)
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
}

// TestRunFindSimilar_StdinURL drives the command with "-" and a piped URL.
// Asserts the URL flows into the actual outbound POST body.
func TestRunFindSimilar_StdinURL(t *testing.T) {
	resetFSFlags(t)

	var gotPath string
	var gotBody client.FindSimilarRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"requestId":"req_fs","results":[{"title":"X","url":"https://x"}]}`))
	}))
	defer srv.Close()

	prev := newFindSimilarClient
	newFindSimilarClient = func() (*client.Client, error) {
		return client.New("k", client.WithBaseURL(srv.URL), client.WithHTTPClient(srv.Client()))
	}
	t.Cleanup(func() { newFindSimilarClient = prev })

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	prevOut := flagOut
	flagOut = path
	t.Cleanup(func() { flagOut = prevOut })

	findSimilarCmd.SetIn(strings.NewReader("https://arxiv.org/abs/2307.06435\n"))
	findSimilarCmd.SetContext(context.Background())
	if err := runFindSimilar(findSimilarCmd, []string{"-"}); err != nil {
		t.Fatalf("runFindSimilar: %v", err)
	}

	if gotPath != "/findSimilar" {
		t.Errorf("path: %q", gotPath)
	}
	if gotBody.URL != "https://arxiv.org/abs/2307.06435" {
		t.Errorf("body url: %q", gotBody.URL)
	}

	raw, _ := os.ReadFile(path)
	var env struct {
		Command string          `json:"command"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Command != "find-similar" {
		t.Errorf("command: %q", env.Command)
	}
}

// TestRunFindSimilar_RepeatedIncludeDomain: parses multiple --include-domain
// flags via cobra and asserts both reach the wire.
func TestRunFindSimilar_RepeatedIncludeDomain(t *testing.T) {
	resetFSFlags(t)

	var gotBody client.FindSimilarRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	prev := newFindSimilarClient
	newFindSimilarClient = func() (*client.Client, error) {
		return client.New("k", client.WithBaseURL(srv.URL), client.WithHTTPClient(srv.Client()))
	}
	t.Cleanup(func() { newFindSimilarClient = prev })

	dir := t.TempDir()
	prevOut := flagOut
	flagOut = filepath.Join(dir, "out.json")
	t.Cleanup(func() { flagOut = prevOut })

	flagFSIncludeDomains = []string{"arxiv.org", "nature.com"}

	findSimilarCmd.SetContext(context.Background())
	if err := runFindSimilar(findSimilarCmd, []string{"https://exa.ai"}); err != nil {
		t.Fatalf("runFindSimilar: %v", err)
	}
	if len(gotBody.IncludeDomains) != 2 || gotBody.IncludeDomains[0] != "arxiv.org" || gotBody.IncludeDomains[1] != "nature.com" {
		t.Errorf("include-domains lost: %v", gotBody.IncludeDomains)
	}
}

// TestRunFindSimilar_DryRunRedactsAPIKey is the security invariant — the API
// key must never appear in dry-run output even when EXA_API_KEY is set.
func TestRunFindSimilar_DryRunRedactsAPIKey(t *testing.T) {
	resetFSFlags(t)
	flagFSDryRun = true
	flagFSText = true
	flagFSExcludeSource = true

	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	prevOut, prevPretty := flagOut, flagPretty
	flagOut, flagPretty = path, true
	t.Cleanup(func() { flagOut, flagPretty = prevOut, prevPretty })

	t.Setenv("EXA_API_KEY", "sk-super-secret-do-not-leak")

	findSimilarCmd.SetContext(context.Background())
	if err := runFindSimilar(findSimilarCmd, []string{"https://exa.ai"}); err != nil {
		t.Fatalf("runFindSimilar: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "sk-super-secret-do-not-leak") {
		t.Fatalf("API key leaked: %s", raw)
	}
	if !strings.Contains(string(raw), "[REDACTED]") {
		t.Errorf("missing [REDACTED] placeholder:\n%s", raw)
	}

	var plan struct {
		Command string `json:"command"`
		DryRun  bool   `json:"dry_run"`
		Request struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Body    json.RawMessage   `json:"body"`
		} `json:"request"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &plan); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if plan.Command != "find-similar" || !plan.DryRun {
		t.Errorf("envelope: %+v", plan)
	}
	if !strings.HasSuffix(plan.Request.URL, "/findSimilar") {
		t.Errorf("url: %q", plan.Request.URL)
	}
	if plan.Request.Headers["x-api-key"] != "[REDACTED]" {
		t.Errorf("x-api-key not redacted")
	}

	var body client.FindSimilarRequest
	if err := json.Unmarshal(plan.Request.Body, &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if body.URL != "https://exa.ai" || !body.ExcludeSourceDomain {
		t.Errorf("body fields lost: %+v", body)
	}
	if body.Contents == nil || !body.Contents.Text {
		t.Errorf("contents lost: %+v", body.Contents)
	}
}

// TestRunFindSimilar_DryRunSkipsAPIKey: --dry-run must short-circuit before
// the EXA_API_KEY check, mirroring runSearch's ordering.
func TestRunFindSimilar_DryRunSkipsAPIKey(t *testing.T) {
	resetFSFlags(t)
	flagFSDryRun = true
	t.Setenv("EXA_API_KEY", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	prevOut := flagOut
	flagOut = path
	t.Cleanup(func() { flagOut = prevOut })

	findSimilarCmd.SetContext(context.Background())
	if err := runFindSimilar(findSimilarCmd, []string{"https://exa.ai"}); err != nil {
		t.Fatalf("runFindSimilar dry-run: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dry-run output not written: %v", err)
	}
}

// TestRunFindSimilar_MissingAPIKey: real factory + empty env -> ExitConfigError.
func TestRunFindSimilar_MissingAPIKey(t *testing.T) {
	resetFSFlags(t)
	t.Setenv("EXA_API_KEY", "")
	silenceStderr(t)

	findSimilarCmd.SetContext(context.Background())
	err := runFindSimilar(findSimilarCmd, []string{"https://exa.ai"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
}

// TestRunFindSimilar_APIErrorExits1: 401 from server -> ExitAPIError.
func TestRunFindSimilar_APIErrorExits1(t *testing.T) {
	resetFSFlags(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	prev := newFindSimilarClient
	newFindSimilarClient = func() (*client.Client, error) {
		return client.New("k", client.WithBaseURL(srv.URL), client.WithHTTPClient(srv.Client()))
	}
	t.Cleanup(func() { newFindSimilarClient = prev })

	silenceStderr(t)

	findSimilarCmd.SetContext(context.Background())
	err := runFindSimilar(findSimilarCmd, []string{"https://exa.ai"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitAPIError {
		t.Fatalf("want ExitAPIError, got %v", err)
	}
}

// TestRunFindSimilar_InvalidFlagBubblesUp: any buildFindSimilarRequest failure
// surfaces as ExitConfigError, never an API call.
func TestRunFindSimilar_InvalidFlagBubblesUp(t *testing.T) {
	resetFSFlags(t)
	flagFSCategory = "not-real"
	silenceStderr(t)

	findSimilarCmd.SetContext(context.Background())
	err := runFindSimilar(findSimilarCmd, []string{"https://exa.ai"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
}

// TestFindSimilar_CommandRegistered makes sure every documented flag is wired
// onto the command so `exa find-similar --help` surfaces them.
func TestFindSimilar_CommandRegistered(t *testing.T) {
	got, _, err := rootCmd.Find([]string{"find-similar"})
	if err != nil {
		t.Fatalf("find find-similar: %v", err)
	}
	for _, name := range []string{
		"num-results", "max-retries", "exclude-source-domain",
		"category", "include-domain", "exclude-domain",
		"start-published", "end-published", "start-crawl", "end-crawl",
		"include-text", "exclude-text", "user-location", "moderation",
		"text", "summary", "highlights", "subpages", "livecrawl", "dry-run",
	} {
		if got.Flag(name) == nil {
			t.Errorf("missing --%s flag on find-similar command", name)
		}
	}
}
