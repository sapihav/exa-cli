package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sapihav/exa-cli/internal/client"
	"github.com/spf13/cobra"
)

// `exa answer` reuses the envelope/writeJSON/mapClientError/userError helpers
// from search.go. Only the request shape and a couple of flags differ.
//
// The /answer endpoint is **not exposed by the Exa MCP server** — it is a
// CLI-native advantage, advertised in PARITY.md and the README.
var (
	flagAnswerText         bool
	flagAnswerOutputSchema string
	flagAnswerMaxRetries   int
	flagAnswerDryRun       bool
)

var answerCmd = &cobra.Command{
	Use:   "answer \"<question>\"",
	Short: "Get a synthesized, cited answer via the Exa /answer endpoint",
	Long: `Synthesize a direct answer with citations for a question via POST /answer
and print the response as JSON on stdout.

The question can be passed as a positional arg or piped on stdin (use "-" as
the single arg). When --text is set, each citation also includes the source
page text. When --output-schema is set, the answer is returned as structured
JSON matching the supplied JSON Schema (Draft 7) rather than a plain string.

Streaming (SSE) is not yet supported — see the backlog Ideas.

Example:
  EXA_API_KEY=... exa answer "what is the capital of France?"
  EXA_API_KEY=... exa answer "summarize the latest LLM scaling research" --text
  echo "who founded Stripe?" | EXA_API_KEY=... exa answer -
  EXA_API_KEY=... exa answer "list the top 3 vector DBs" \
    --output-schema '{"type":"object","properties":{"dbs":{"type":"array","items":{"type":"string"}}}}'

Exit codes:
  0  success
  1  API error (HTTP >= 400)
  2  user/config error (missing key, invalid flag)
  3  network error`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAnswer,
}

func init() {
	answerCmd.Flags().BoolVar(&flagAnswerText, "text", false, "include the full source text on each citation")
	answerCmd.Flags().StringVar(&flagAnswerOutputSchema, "output-schema", "", "JSON Schema (Draft 7) to coerce the answer into structured JSON")
	answerCmd.Flags().IntVar(&flagAnswerMaxRetries, "max-retries", 3, "maximum retry attempts on 429/5xx")
	answerCmd.Flags().BoolVar(&flagAnswerDryRun, "dry-run", false, "print the planned request body and exit; no network call")
	rootCmd.AddCommand(answerCmd)
}

// newAnswerClient is a package-level seam so tests can inject a client
// pointed at httptest.NewServer without setting EXA_API_KEY. Same pattern as
// newSearchClient/newFindSimilarClient.
var newAnswerClient = func() (*client.Client, error) {
	return client.New(os.Getenv("EXA_API_KEY"), client.WithMaxRetries(flagAnswerMaxRetries))
}

// runAnswer is the RunE for `exa answer`.
func runAnswer(cmd *cobra.Command, args []string) error {
	start := time.Now()

	query, err := resolveAnswerQuery(args, cmd.InOrStdin())
	if err != nil {
		return userError(err.Error())
	}
	if query == "" {
		return userError("question is required (pass as arg or pipe via stdin with '-')")
	}

	req, err := buildAnswerRequest(query)
	if err != nil {
		return userError(err.Error())
	}

	if flagAnswerDryRun {
		return writeAnswerDryRun(req)
	}

	c, err := newAnswerClient()
	if err != nil {
		if errors.Is(err, client.ErrMissingAPIKey) {
			return userError(err.Error())
		}
		return userError("client init: " + err.Error())
	}

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "exa: POST /answer text=%t schema=%t query=%q\n",
			flagAnswerText, flagAnswerOutputSchema != "", query)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	resp, err := c.Answer(ctx, req)
	if err != nil {
		return mapClientError(err)
	}

	return writeJSON(envelope{
		SchemaVersion: "1",
		Provider:      "exa",
		Command:       "answer",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        resp,
	})
}

// resolveAnswerQuery extracts the question from argv or stdin. A bare "-"
// (or no arg when stdin is non-interactive) reads from stdin: every
// non-empty line is joined with a single space so multi-line questions are
// preserved as a single query string. Cobra's MaximumNArgs(1) on the command
// guarantees len(args) <= 1, so we only branch on the single-arg vs stdin
// cases here.
func resolveAnswerQuery(args []string, stdin io.Reader) (string, error) {
	if len(args) == 1 && args[0] != "-" {
		return strings.TrimSpace(args[0]), nil
	}
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var parts []string
	for scanner.Scan() {
		s := strings.TrimSpace(scanner.Text())
		if s != "" {
			parts = append(parts, s)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return strings.Join(parts, " "), nil
}

// buildAnswerRequest validates flags and assembles the request body.
// Validation errors are returned as plain errors so the caller can wrap them
// in userError for exit-code-2 mapping.
func buildAnswerRequest(query string) (client.AnswerRequest, error) {
	if flagAnswerMaxRetries < 0 {
		return client.AnswerRequest{}, errors.New("--max-retries must be >= 0")
	}

	req := client.AnswerRequest{
		Query: query,
		Text:  flagAnswerText,
	}

	// --output-schema must be valid JSON. We do not validate that it is a
	// well-formed JSON Schema — Exa's server rejects malformed schemas with
	// a 400, which the CLI surfaces as exit 1. We only guard against
	// shipping non-JSON garbage, which would also fail upstream but with a
	// less helpful error.
	if flagAnswerOutputSchema != "" {
		var probe any
		if err := json.Unmarshal([]byte(flagAnswerOutputSchema), &probe); err != nil {
			return client.AnswerRequest{}, fmt.Errorf("invalid --output-schema (not valid JSON): %v", err)
		}
		req.OutputSchema = json.RawMessage(flagAnswerOutputSchema)
	}

	return req, nil
}

// writeAnswerDryRun prints the planned POST /answer request without making a
// network call. The API key never appears in the body — Authorization lives
// at the HTTP layer and the dry-run output reflects that.
func writeAnswerDryRun(req client.AnswerRequest) error {
	plan := map[string]any{
		"schema_version": "1",
		"provider":       "exa",
		"command":        "answer",
		"dry_run":        true,
		"request": map[string]any{
			"method":  "POST",
			"url":     client.DefaultBaseURL + "/answer",
			"headers": map[string]string{"x-api-key": "[REDACTED]"},
			"body":    req,
		},
	}

	var out io.Writer = os.Stdout
	if flagOut != "" {
		f, err := os.Create(flagOut)
		if err != nil {
			return userError("open --out: " + err.Error())
		}
		defer f.Close()
		out = f
	}
	enc := json.NewEncoder(out)
	if flagPretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(plan); err != nil {
		return userError("encode json: " + err.Error())
	}
	return nil
}
