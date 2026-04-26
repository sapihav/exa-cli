package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapihav/exa-cli/internal/client"
)

// resetAnswerFlags resets every answer flag global to its declared default.
// Cobra flag globals are package-level, so a leak from one test would silently
// pollute the next; reset is centralized here to avoid drift as flags are added.
func resetAnswerFlags(t *testing.T) {
	t.Helper()
	flagAnswerText = false
	flagAnswerOutputSchema = ""
	flagAnswerMaxRetries = 3
	flagAnswerDryRun = false
}

// TestResolveAnswerQuery covers arg, "-" stdin, no-arg fallback, whitespace,
// and multi-line stdin (which is joined with single spaces).
func TestResolveAnswerQuery(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"arg", []string{"who founded Stripe?"}, "", "who founded Stripe?"},
		{"trim arg", []string{"  who founded Stripe?  "}, "", "who founded Stripe?"},
		{"dash stdin single line", []string{"-"}, "what is the capital of France?\n", "what is the capital of France?"},
		{"dash stdin multi line joined", []string{"-"}, "list\ntop 3\nLLMs\n", "list top 3 LLMs"},
		{"dash stdin skip blank", []string{"-"}, "\n\n  hello?\n\nworld?\n", "hello? world?"},
		{"no args, no stdin", nil, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveAnswerQuery(tc.args, strings.NewReader(tc.stdin))
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// errReader satisfies io.Reader and always returns a non-EOF error so we can
// drive the scanner.Err() branch in resolveAnswerQuery.
type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("boom") }

// TestResolveAnswerQuery_ScannerError exercises the error-return path when the
// underlying reader fails mid-scan. We need a payload longer than the scanner
// buffer can swallow in one Read so it actually surfaces the I/O error.
func TestResolveAnswerQuery_ScannerError(t *testing.T) {
	_, err := resolveAnswerQuery([]string{"-"}, errReader{})
	if err == nil {
		t.Fatalf("expected error from broken stdin reader")
	}
	if !strings.Contains(err.Error(), "read stdin") {
		t.Errorf("wrong error: %v", err)
	}
}

// TestBuildAnswerRequest_Defaults: with only the query set, the body must
// serialize to a minimal {"query"} object. Guards the wire shape.
func TestBuildAnswerRequest_Defaults(t *testing.T) {
	resetAnswerFlags(t)
	req, err := buildAnswerRequest("hi?")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := json.Marshal(req)
	want := `{"query":"hi?"}`
	if string(raw) != want {
		t.Errorf("body drifted:\n got: %s\nwant: %s", raw, want)
	}
}

// TestBuildAnswerRequest_WithText pins the wire shape when --text is set.
func TestBuildAnswerRequest_WithText(t *testing.T) {
	resetAnswerFlags(t)
	flagAnswerText = true
	req, err := buildAnswerRequest("hi?")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := json.Marshal(req)
	want := `{"query":"hi?","text":true}`
	if string(raw) != want {
		t.Errorf("body drifted:\n got: %s\nwant: %s", raw, want)
	}
}

// TestBuildAnswerRequest_WithOutputSchema asserts the schema flows through as
// raw JSON (not double-encoded as a string).
func TestBuildAnswerRequest_WithOutputSchema(t *testing.T) {
	resetAnswerFlags(t)
	flagAnswerOutputSchema = `{"type":"object","properties":{"x":{"type":"string"}}}`
	req, err := buildAnswerRequest("hi?")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := json.Marshal(req)

	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	schema, ok := got["outputSchema"]
	if !ok {
		t.Fatalf("outputSchema missing from body: %s", raw)
	}
	// Verify it is an object literal on the wire, not a stringified blob.
	if !bytes.HasPrefix(bytes.TrimSpace(schema), []byte("{")) {
		t.Errorf("outputSchema double-encoded: %s", schema)
	}
	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Fatalf("schema not valid JSON object: %v", err)
	}
	if s["type"] != "object" {
		t.Errorf("schema content lost: %+v", s)
	}
}

// TestBuildAnswerRequest_Validation covers each validation branch:
// bad output schema (not JSON) and a negative retry count.
func TestBuildAnswerRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		wantSub string
	}{
		{"negative retries", func() { flagAnswerMaxRetries = -1 }, "--max-retries"},
		{"bad output schema", func() { flagAnswerOutputSchema = "not json" }, "--output-schema"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetAnswerFlags(t)
			tc.setup()
			_, err := buildAnswerRequest("hi?")
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}

// TestRunAnswer_RequiresQuery: no arg + empty stdin -> ExitConfigError with
// the "question is required" message.
func TestRunAnswer_RequiresQuery(t *testing.T) {
	resetAnswerFlags(t)
	silenceStderr(t)

	answerCmd.SetIn(strings.NewReader(""))
	answerCmd.SetContext(context.Background())
	err := runAnswer(answerCmd, nil)
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
}

// TestRunAnswer_StdinReadError: a stdin reader that errors mid-scan must
// surface as ExitConfigError, not a panic.
func TestRunAnswer_StdinReadError(t *testing.T) {
	resetAnswerFlags(t)
	silenceStderr(t)

	answerCmd.SetIn(errReader{})
	answerCmd.SetContext(context.Background())
	err := runAnswer(answerCmd, []string{"-"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
}

// TestRunAnswer_HappyPath drives the command end to end: stub server returns
// a synthesized answer + citations, the envelope round-trips, and the request
// hits /answer with the right body.
func TestRunAnswer_HappyPath(t *testing.T) {
	resetAnswerFlags(t)

	var gotPath string
	var gotBody client.AnswerRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answer":"Paris.","citations":[{"id":"c1","url":"https://en.wikipedia.org/wiki/Paris","title":"Paris"}]}`))
	}))
	defer srv.Close()

	prev := newAnswerClient
	newAnswerClient = func() (*client.Client, error) {
		return client.New("k", client.WithBaseURL(srv.URL), client.WithHTTPClient(srv.Client()))
	}
	t.Cleanup(func() { newAnswerClient = prev })

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	prevOut, prevVerbose := flagOut, flagVerbose
	flagOut, flagVerbose = path, true
	t.Cleanup(func() { flagOut, flagVerbose = prevOut, prevVerbose })

	flagAnswerText = true
	silenceStderr(t)

	answerCmd.SetContext(context.Background())
	if err := runAnswer(answerCmd, []string{"what is the capital of France?"}); err != nil {
		t.Fatalf("runAnswer: %v", err)
	}

	if gotPath != "/answer" {
		t.Errorf("path: %q", gotPath)
	}
	if gotBody.Query != "what is the capital of France?" || !gotBody.Text {
		t.Errorf("body lost: %+v", gotBody)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env struct {
		SchemaVersion string          `json:"schema_version"`
		Command       string          `json:"command"`
		Result        json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Command != "answer" {
		t.Errorf("command: %q", env.Command)
	}
	if env.SchemaVersion != "1" {
		t.Errorf("schema_version: %q", env.SchemaVersion)
	}

	var result client.AnswerResponse
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result unmarshal: %v", err)
	}
	if string(bytes.TrimSpace(result.Answer)) != `"Paris."` {
		t.Errorf("answer lost: %s", result.Answer)
	}
	if len(result.Citations) != 1 || result.Citations[0].URL != "https://en.wikipedia.org/wiki/Paris" {
		t.Errorf("citations lost: %+v", result.Citations)
	}
}

// TestRunAnswer_StdinQuery drives the command with "-" and a piped question.
func TestRunAnswer_StdinQuery(t *testing.T) {
	resetAnswerFlags(t)

	var gotBody client.AnswerRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answer":"ok"}`))
	}))
	defer srv.Close()

	prev := newAnswerClient
	newAnswerClient = func() (*client.Client, error) {
		return client.New("k", client.WithBaseURL(srv.URL), client.WithHTTPClient(srv.Client()))
	}
	t.Cleanup(func() { newAnswerClient = prev })

	dir := t.TempDir()
	prevOut := flagOut
	flagOut = filepath.Join(dir, "out.json")
	t.Cleanup(func() { flagOut = prevOut })

	answerCmd.SetIn(strings.NewReader("who founded Stripe?\n"))
	answerCmd.SetContext(context.Background())
	if err := runAnswer(answerCmd, []string{"-"}); err != nil {
		t.Fatalf("runAnswer: %v", err)
	}
	if gotBody.Query != "who founded Stripe?" {
		t.Errorf("body query: %q", gotBody.Query)
	}
}

// TestRunAnswer_DryRunRedactsAPIKey is the security invariant — the API key
// must never appear in dry-run output even when EXA_API_KEY is set.
func TestRunAnswer_DryRunRedactsAPIKey(t *testing.T) {
	resetAnswerFlags(t)
	flagAnswerDryRun = true
	flagAnswerText = true
	flagAnswerOutputSchema = `{"type":"object"}`

	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	prevOut, prevPretty := flagOut, flagPretty
	flagOut, flagPretty = path, true
	t.Cleanup(func() { flagOut, flagPretty = prevOut, prevPretty })

	t.Setenv("EXA_API_KEY", "sk-super-secret-do-not-leak")

	answerCmd.SetContext(context.Background())
	if err := runAnswer(answerCmd, []string{"hi?"}); err != nil {
		t.Fatalf("runAnswer: %v", err)
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
	if plan.Command != "answer" || !plan.DryRun {
		t.Errorf("envelope: %+v", plan)
	}
	if !strings.HasSuffix(plan.Request.URL, "/answer") {
		t.Errorf("url: %q", plan.Request.URL)
	}
	if plan.Request.Headers["x-api-key"] != "[REDACTED]" {
		t.Errorf("x-api-key not redacted")
	}

	var body client.AnswerRequest
	if err := json.Unmarshal(plan.Request.Body, &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if body.Query != "hi?" || !body.Text {
		t.Errorf("body fields lost: %+v", body)
	}
	if len(body.OutputSchema) == 0 {
		t.Errorf("output schema lost")
	}
}

// TestRunAnswer_DryRunSkipsAPIKey: --dry-run must short-circuit before the
// EXA_API_KEY check, mirroring runSearch / runFindSimilar ordering.
func TestRunAnswer_DryRunSkipsAPIKey(t *testing.T) {
	resetAnswerFlags(t)
	flagAnswerDryRun = true
	t.Setenv("EXA_API_KEY", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	prevOut := flagOut
	flagOut = path
	t.Cleanup(func() { flagOut = prevOut })

	answerCmd.SetContext(context.Background())
	if err := runAnswer(answerCmd, []string{"hi?"}); err != nil {
		t.Fatalf("runAnswer dry-run: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dry-run output not written: %v", err)
	}
}

// TestRunAnswer_DryRunOutOpenError: --out pointing at a directory must fail
// with ExitConfigError so dry-run errors are not swallowed.
func TestRunAnswer_DryRunOutOpenError(t *testing.T) {
	resetAnswerFlags(t)
	flagAnswerDryRun = true
	silenceStderr(t)

	dir := t.TempDir()
	prevOut := flagOut
	flagOut = dir // directory, not a file → os.Create fails
	t.Cleanup(func() { flagOut = prevOut })

	answerCmd.SetContext(context.Background())
	err := runAnswer(answerCmd, []string{"hi?"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
}

// TestRunAnswer_DryRunEncodeError forces the json.Encoder.Encode branch in
// writeAnswerDryRun to fail by redirecting os.Stdout to the read-end of a
// pipe and closing the write-end first. Covers the otherwise-unreachable
// "encode json:" error wrapping so the new code hits 100% line coverage.
func TestRunAnswer_DryRunEncodeError(t *testing.T) {
	resetAnswerFlags(t)
	flagAnswerDryRun = true
	silenceStderr(t)

	// Replace os.Stdout with a pipe whose writer is already closed; any
	// Write call (Encoder.Encode → buf.WriteTo) returns an error.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = r.Close()
	_ = w.Close()
	prevStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = prevStdout })

	prevOut := flagOut
	flagOut = "" // route to os.Stdout
	t.Cleanup(func() { flagOut = prevOut })

	answerCmd.SetContext(context.Background())
	gotErr := runAnswer(answerCmd, []string{"hi?"})
	ec, ok := gotErr.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", gotErr)
	}
	if !strings.Contains(ec.msg, "encode json") {
		t.Errorf("error message should mention encode json, got %q", ec.msg)
	}
}

// TestRunAnswer_MissingAPIKey: real factory + empty env -> ExitConfigError.
func TestRunAnswer_MissingAPIKey(t *testing.T) {
	resetAnswerFlags(t)
	t.Setenv("EXA_API_KEY", "")
	silenceStderr(t)

	answerCmd.SetContext(context.Background())
	err := runAnswer(answerCmd, []string{"hi?"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
}

// TestRunAnswer_APIErrorExits1: 401 from server -> ExitAPIError.
func TestRunAnswer_APIErrorExits1(t *testing.T) {
	resetAnswerFlags(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	prev := newAnswerClient
	newAnswerClient = func() (*client.Client, error) {
		return client.New("k", client.WithBaseURL(srv.URL), client.WithHTTPClient(srv.Client()))
	}
	t.Cleanup(func() { newAnswerClient = prev })

	silenceStderr(t)

	answerCmd.SetContext(context.Background())
	err := runAnswer(answerCmd, []string{"hi?"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitAPIError {
		t.Fatalf("want ExitAPIError, got %v", err)
	}
}

// TestRunAnswer_ClientInitErrorWrapped: a non-ErrMissingAPIKey error from the
// client factory must surface as ExitConfigError with the wrapped message,
// not a panic. Covers the "client init: ..." branch.
func TestRunAnswer_ClientInitErrorWrapped(t *testing.T) {
	resetAnswerFlags(t)
	silenceStderr(t)

	prev := newAnswerClient
	newAnswerClient = func() (*client.Client, error) {
		return nil, errors.New("transport rigged")
	}
	t.Cleanup(func() { newAnswerClient = prev })

	answerCmd.SetContext(context.Background())
	err := runAnswer(answerCmd, []string{"hi?"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
	if !strings.Contains(ec.msg, "transport rigged") {
		t.Errorf("error message: %q", ec.msg)
	}
}

// TestRunAnswer_InvalidFlagBubblesUp: any buildAnswerRequest failure surfaces
// as ExitConfigError, never an API call.
func TestRunAnswer_InvalidFlagBubblesUp(t *testing.T) {
	resetAnswerFlags(t)
	flagAnswerOutputSchema = "not json"
	silenceStderr(t)

	answerCmd.SetContext(context.Background())
	err := runAnswer(answerCmd, []string{"hi?"})
	ec, ok := err.(*exitCodeError)
	if !ok || ec.code != ExitConfigError {
		t.Fatalf("want ExitConfigError, got %v", err)
	}
}

// TestAnswer_CommandRegistered makes sure every documented flag is wired onto
// the command so `exa answer --help` surfaces them.
func TestAnswer_CommandRegistered(t *testing.T) {
	got, _, err := rootCmd.Find([]string{"answer"})
	if err != nil {
		t.Fatalf("find answer: %v", err)
	}
	for _, name := range []string{"text", "output-schema", "max-retries", "dry-run"} {
		if got.Flag(name) == nil {
			t.Errorf("missing --%s flag on answer command", name)
		}
	}
}
