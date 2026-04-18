package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sapihav/exa-cli/internal/client"
)

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
