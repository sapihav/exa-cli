package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sapihav/exa-cli/internal/client"
	"github.com/spf13/cobra"
)

// Valid values for --type. Anything else is rejected with exit 2.
var validSearchTypes = map[string]bool{
	"neural":  true,
	"keyword": true,
	"auto":    true,
}

// envelope is the common output shape emitted on stdout for every successful
// invocation. M1.5 introduces this so agents can route on `command` and
// measure latency without parsing stderr. Error path is unchanged.
type envelope struct {
	SchemaVersion string `json:"schema_version"`
	Provider      string `json:"provider"`
	Command       string `json:"command"`
	ElapsedMs     int64  `json:"elapsed_ms"`
	Result        any    `json:"result"`
}

var (
	flagNumResults int
	flagType       string
	flagMaxRetries int
)

var searchCmd = &cobra.Command{
	Use:   "search \"<query>\"",
	Short: "Run a search query against the Exa /search endpoint",
	Long: `Run a search query against the Exa /search endpoint and print the
response as JSON on stdout.

Example:
  EXA_API_KEY=... exa search "best open-source vector databases"
  EXA_API_KEY=... exa search "site:news.ycombinator.com rust async" --type keyword --num-results 5

Exit codes:
  0  success
  1  API error (HTTP >= 400)
  2  user/config error (missing key, invalid flag)
  3  network error`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().IntVarP(&flagNumResults, "num-results", "n", 10, "number of results to return")
	searchCmd.Flags().StringVarP(&flagType, "type", "t", "auto", "search type: neural, keyword, or auto")
	searchCmd.Flags().IntVar(&flagMaxRetries, "max-retries", 3, "maximum retry attempts on 429/5xx")
	rootCmd.AddCommand(searchCmd)
}

// runSearch is the RunE for `exa search`. Returns an *exitCodeError so
// Execute() can translate the failure into a process exit code; stderr
// messages are printed here so the user sees them before the process exits.
func runSearch(cmd *cobra.Command, args []string) error {
	start := time.Now()
	query := args[0]
	if query == "" {
		return userError("query cannot be empty")
	}

	if flagNumResults < 1 {
		return userError("--num-results must be >= 1")
	}
	if !validSearchTypes[flagType] {
		return userError(fmt.Sprintf("invalid --type %q (want neural|keyword|auto)", flagType))
	}
	if flagMaxRetries < 0 {
		return userError("--max-retries must be >= 0")
	}

	apiKey := os.Getenv("EXA_API_KEY")
	c, err := client.New(
		apiKey,
		client.WithMaxRetries(flagMaxRetries),
	)
	if err != nil {
		if errors.Is(err, client.ErrMissingAPIKey) {
			return userError(err.Error())
		}
		return userError("client init: " + err.Error())
	}

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "exa: POST /search type=%s num=%d query=%q\n", flagType, flagNumResults, query)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	resp, err := c.Search(ctx, client.SearchRequest{
		Query:      query,
		NumResults: flagNumResults,
		Type:       flagType,
	})
	if err != nil {
		return mapClientError(err)
	}

	return writeJSON(envelope{
		SchemaVersion: "1",
		Provider:      "exa",
		Command:       "search",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        resp,
	})
}

// writeJSON marshals v and writes it to --out (or stdout). --pretty controls
// indentation.
func writeJSON(v any) error {
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
	if err := enc.Encode(v); err != nil {
		return userError("encode json: " + err.Error())
	}
	return nil
}

// mapClientError turns a client error into the right exit code, printing a
// human-readable line to stderr first.
func mapClientError(err error) error {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		fmt.Fprintf(os.Stderr, "exa: api error (status=%d): %s\n", apiErr.StatusCode, apiErr.Body)
		return &exitCodeError{code: ExitAPIError, msg: err.Error()}
	}
	var netErr *client.NetworkError
	if errors.As(err, &netErr) {
		fmt.Fprintf(os.Stderr, "exa: network error: %v\n", netErr.Unwrap())
		return &exitCodeError{code: ExitNetworkErr, msg: err.Error()}
	}
	fmt.Fprintln(os.Stderr, "exa:", err)
	return &exitCodeError{code: ExitAPIError, msg: err.Error()}
}

func userError(msg string) error {
	fmt.Fprintln(os.Stderr, "exa:", msg)
	return &exitCodeError{code: ExitConfigError, msg: msg}
}
