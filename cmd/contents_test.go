package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapihav/exa-cli/internal/client"
)

// TestCollectURLs covers the three input paths (args, --urls, stdin) plus
// dedup, blank filtering, and the "-" stdin trigger.
func TestCollectURLs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		urlsFlag string
		stdin    string
		want     []string
	}{
		{
			name: "args only",
			args: []string{"https://a.example", "https://b.example"},
			want: []string{"https://a.example", "https://b.example"},
		},
		{
			name:     "flag only",
			urlsFlag: "https://a.example,https://b.example",
			want:     []string{"https://a.example", "https://b.example"},
		},
		{
			name:  "stdin only via dash",
			args:  []string{"-"},
			stdin: "https://a.example\nhttps://b.example\n",
			want:  []string{"https://a.example", "https://b.example"},
		},
		{
			name:  "stdin with blank lines and whitespace",
			args:  []string{"-"},
			stdin: "  https://a.example  \n\n\t\nhttps://b.example\n",
			want:  []string{"https://a.example", "https://b.example"},
		},
		{
			name:     "merge args + flag + stdin with dedup",
			args:     []string{"https://a.example", "-"},
			urlsFlag: "https://b.example, https://c.example",
			stdin:    "https://a.example\nhttps://d.example\n",
			want: []string{
				"https://a.example",
				"https://b.example",
				"https://c.example",
				"https://d.example",
			},
		},
		{
			name: "empty everywhere",
			args: nil,
			want: []string{},
		},
		{
			name:     "flag with empty parts",
			urlsFlag: ",,https://a.example,,",
			want:     []string{"https://a.example"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collectURLs(tc.args, tc.urlsFlag, strings.NewReader(tc.stdin))
			if err != nil {
				t.Fatalf("collectURLs: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v, want %v", got, tc.want)
			}
			for i, u := range got {
				if u != tc.want[i] {
					t.Errorf("[%d] got %q, want %q", i, u, tc.want[i])
				}
			}
		})
	}
}

// TestContents_DryRunRedactsAPIKey asserts that --dry-run renders the planned
// request but never leaks the API key into stdout. This is the security
// invariant called out in CLAUDE.md and ROADMAP §4.
func TestContents_DryRunRedactsAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")

	// Preserve + restore globals touched by writeDryRun.
	prevOut, prevPretty := flagOut, flagPretty
	flagOut, flagPretty = path, true
	t.Cleanup(func() { flagOut, flagPretty = prevOut, prevPretty })

	// A plausible-looking secret we must not see echoed anywhere.
	t.Setenv("EXA_API_KEY", "sk-super-secret-key-do-not-leak")

	req := client.ContentsRequest{
		URLs:       []string{"https://example.com"},
		Text:       true,
		Summary:    true,
		Highlights: 3,
		Subpages:   2,
		Livecrawl:  "always",
	}
	if err := writeDryRun(req); err != nil {
		t.Fatalf("writeDryRun: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if strings.Contains(string(raw), "sk-super-secret-key-do-not-leak") {
		t.Fatalf("API key leaked in dry-run output:\n%s", raw)
	}
	if !strings.Contains(string(raw), "[REDACTED]") {
		t.Errorf("expected [REDACTED] placeholder in dry-run output:\n%s", raw)
	}

	// Structural assertions — this is also the documented output contract.
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
	if plan.SchemaVersion != "1" || plan.Command != "contents" || !plan.DryRun {
		t.Errorf("envelope mismatch: %+v", plan)
	}
	if plan.Request.Method != "POST" {
		t.Errorf("method: %q", plan.Request.Method)
	}
	if !strings.HasSuffix(plan.Request.URL, "/contents") {
		t.Errorf("url: %q", plan.Request.URL)
	}
	if plan.Request.Headers["x-api-key"] != "[REDACTED]" {
		t.Errorf("x-api-key not redacted: %q", plan.Request.Headers["x-api-key"])
	}

	// Body must be the exact request we'd send — no string interpolation,
	// proper JSON shape.
	var gotBody client.ContentsRequest
	if err := json.Unmarshal(plan.Request.Body, &gotBody); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(gotBody.URLs) != 1 || gotBody.URLs[0] != "https://example.com" {
		t.Errorf("urls: %v", gotBody.URLs)
	}
	if !gotBody.Text || !gotBody.Summary || gotBody.Highlights != 3 || gotBody.Subpages != 2 {
		t.Errorf("body fields lost: %+v", gotBody)
	}
	if gotBody.Livecrawl != "always" {
		t.Errorf("livecrawl: %q", gotBody.Livecrawl)
	}
}

// TestContents_Envelope verifies the M1.5 stdout contract applies to the
// `contents` command too: the success response is wrapped in the schema_version=1
// envelope with provider/command/elapsed_ms/result.
func TestContents_Envelope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	prevOut := flagOut
	flagOut = path
	t.Cleanup(func() { flagOut = prevOut })

	resp := &client.ContentsResponse{
		RequestID: "req_test",
		Results: []client.ContentsResult{
			{
				ID:    "id_1",
				URL:   "https://example.com",
				Title: "Example",
				Text:  "hello world",
			},
		},
	}

	env := envelope{
		SchemaVersion: "1",
		Provider:      "exa",
		Command:       "contents",
		ElapsedMs:     17,
		Result:        resp,
	}
	if err := writeJSON(env); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var decoded struct {
		SchemaVersion string          `json:"schema_version"`
		Provider      string          `json:"provider"`
		Command       string          `json:"command"`
		ElapsedMs     int64           `json:"elapsed_ms"`
		Result        json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &decoded); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, raw)
	}
	if decoded.Provider != "exa" || decoded.Command != "contents" {
		t.Errorf("provider/command: %+v", decoded)
	}

	var gotResp client.ContentsResponse
	if err := json.Unmarshal(decoded.Result, &gotResp); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(gotResp.Results) != 1 || gotResp.Results[0].Text != "hello world" {
		t.Errorf("result mismatch: %+v", gotResp.Results)
	}
}

// TestContents_CommandRegistered ensures the contents subcommand is wired into
// the root command so `exa contents --help` works.
func TestContents_CommandRegistered(t *testing.T) {
	got, _, err := rootCmd.Find([]string{"contents"})
	if err != nil {
		t.Fatalf("find contents: %v", err)
	}
	if got == nil || got.Name() != "contents" {
		t.Fatalf("contents command not registered (got=%v)", got)
	}

	// Flag surface assertions — if any of these disappear, dependents break.
	for _, name := range []string{"text", "summary", "highlights", "subpages", "livecrawl", "urls", "dry-run", "max-retries"} {
		if got.Flag(name) == nil {
			t.Errorf("missing --%s flag on contents command", name)
		}
	}
}

