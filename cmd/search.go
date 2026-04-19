package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
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

// Valid values for --category. Kept as a map for O(1) lookup + easy help text
// generation. The two M3-critical values are "company" and "people", which
// give us parity with the Exa MCP server's company_research_exa /
// people_search_exa tools without a dedicated subcommand.
var validCategories = map[string]bool{
	"research_paper":    true,
	"news":              true,
	"pdf":               true,
	"github":            true,
	"tweet":             true,
	"movie":             true,
	"song":              true,
	"personal_site":     true,
	"linkedin_profile":  true,
	"financial_report":  true,
	"company":           true,
	"people":            true,
}

// dateRE matches the subset of YYYY-MM-DD the Exa API expects. We validate
// shape only — out-of-range dates (e.g. 2026-13-40) are the server's problem
// to reject with a 400. The regex is anchored so partial matches cannot slip
// through.
var dateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

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

// searchFlags groups every flag on `exa search`. Globals (rather than closed-
// over RunE state) match M1/M2 style and keep test setup simple.
var (
	flagNumResults     int
	flagType           string
	flagMaxRetries     int
	flagCategory       string
	flagIncludeDomains []string
	flagExcludeDomains []string
	flagStartPublished string
	flagEndPublished   string
	flagStartCrawl     string
	flagEndCrawl       string
	flagIncludeText    []string
	flagExcludeText    []string
	flagUserLocation   string
	flagModeration     bool
	flagSearchText     bool
	flagSearchSummary  bool
	flagSearchHL       int
	flagSearchSubpages int
	flagSearchDryRun   bool
)

var searchCmd = &cobra.Command{
	Use:   "search \"<query>\"",
	Short: "Run a search query against the Exa /search endpoint",
	Long: `Run a search query against the Exa /search endpoint and print the
response as JSON on stdout.

Example:
  EXA_API_KEY=... exa search "best open-source vector databases"
  EXA_API_KEY=... exa search "site:news.ycombinator.com rust async" --type keyword --num-results 5
  EXA_API_KEY=... exa search "Pinecone" --category company --num-results 3
  EXA_API_KEY=... exa search "AI papers" --category research_paper \
    --start-published 2026-01-01 --include-domain arxiv.org --text --summary

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

	// M3 filters. Every flag is optional and absence omits the field from
	// the request body entirely, so agents that only pass --num-results/--type
	// see zero change in wire format.
	searchCmd.Flags().StringVar(&flagCategory, "category", "", "result category (research_paper|news|pdf|github|tweet|movie|song|personal_site|linkedin_profile|financial_report|company|people)")
	searchCmd.Flags().StringArrayVar(&flagIncludeDomains, "include-domain", nil, "only return results from this domain (repeatable)")
	searchCmd.Flags().StringArrayVar(&flagExcludeDomains, "exclude-domain", nil, "exclude results from this domain (repeatable)")
	searchCmd.Flags().StringVar(&flagStartPublished, "start-published", "", "earliest publish date YYYY-MM-DD")
	searchCmd.Flags().StringVar(&flagEndPublished, "end-published", "", "latest publish date YYYY-MM-DD")
	searchCmd.Flags().StringVar(&flagStartCrawl, "start-crawl", "", "earliest crawl date YYYY-MM-DD")
	searchCmd.Flags().StringVar(&flagEndCrawl, "end-crawl", "", "latest crawl date YYYY-MM-DD")
	searchCmd.Flags().StringArrayVar(&flagIncludeText, "include-text", nil, "text that must appear in the result (repeatable, max 5)")
	searchCmd.Flags().StringArrayVar(&flagExcludeText, "exclude-text", nil, "text that must not appear in the result (repeatable, max 5)")
	searchCmd.Flags().StringVar(&flagUserLocation, "user-location", "", "ISO 3166-1 alpha-2 country code (e.g. US)")
	searchCmd.Flags().BoolVar(&flagModeration, "moderation", false, "enable Exa content moderation")

	// Content enrichment — maps to the nested `contents` object on /search.
	searchCmd.Flags().BoolVar(&flagSearchText, "text", false, "return the full page text inline on each result")
	searchCmd.Flags().BoolVar(&flagSearchSummary, "summary", false, "return an LLM-generated summary inline on each result")
	searchCmd.Flags().IntVar(&flagSearchHL, "highlights", 0, "return top-N highlight snippets per result (0 = off)")
	searchCmd.Flags().IntVar(&flagSearchSubpages, "subpages", 0, "crawl up to N subpages per result (0 = off)")
	searchCmd.Flags().BoolVar(&flagSearchDryRun, "dry-run", false, "print the planned request body and exit; no network call")

	rootCmd.AddCommand(searchCmd)
}

// newSearchClient constructs the HTTP client used by runSearch. Held as a
// package-level variable (rather than inlined) so tests can swap in a client
// pointed at a httptest.Server without touching env vars or the real API.
var newSearchClient = func() (*client.Client, error) {
	return client.New(os.Getenv("EXA_API_KEY"), client.WithMaxRetries(flagMaxRetries))
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

	req, err := buildSearchRequest(query)
	if err != nil {
		return userError(err.Error())
	}

	if flagSearchDryRun {
		return writeSearchDryRun(req)
	}

	c, err := newSearchClient()
	if err != nil {
		if errors.Is(err, client.ErrMissingAPIKey) {
			return userError(err.Error())
		}
		return userError("client init: " + err.Error())
	}

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "exa: POST /search type=%s num=%d category=%q query=%q\n",
			flagType, flagNumResults, flagCategory, query)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	resp, err := c.Search(ctx, req)
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

// buildSearchRequest validates flags and assembles the SearchRequest payload.
// Validation errors are returned as plain errors; the caller wraps them in
// userError so the CLI exits 2 (config error).
func buildSearchRequest(query string) (client.SearchRequest, error) {
	if flagNumResults < 1 {
		return client.SearchRequest{}, errors.New("--num-results must be >= 1")
	}
	if !validSearchTypes[flagType] {
		return client.SearchRequest{}, fmt.Errorf("invalid --type %q (want neural|keyword|auto)", flagType)
	}
	if flagMaxRetries < 0 {
		return client.SearchRequest{}, errors.New("--max-retries must be >= 0")
	}
	if flagCategory != "" && !validCategories[flagCategory] {
		return client.SearchRequest{}, fmt.Errorf("invalid --category %q", flagCategory)
	}
	for _, d := range []struct{ name, val string }{
		{"--start-published", flagStartPublished},
		{"--end-published", flagEndPublished},
		{"--start-crawl", flagStartCrawl},
		{"--end-crawl", flagEndCrawl},
	} {
		if d.val != "" && !dateRE.MatchString(d.val) {
			return client.SearchRequest{}, fmt.Errorf("invalid %s %q (want YYYY-MM-DD)", d.name, d.val)
		}
	}
	if len(flagIncludeText) > 5 {
		return client.SearchRequest{}, errors.New("--include-text accepts at most 5 values")
	}
	if len(flagExcludeText) > 5 {
		return client.SearchRequest{}, errors.New("--exclude-text accepts at most 5 values")
	}
	if flagSearchHL < 0 {
		return client.SearchRequest{}, errors.New("--highlights must be >= 0")
	}
	if flagSearchSubpages < 0 {
		return client.SearchRequest{}, errors.New("--subpages must be >= 0")
	}

	req := client.SearchRequest{
		Query:              query,
		NumResults:         flagNumResults,
		Type:               flagType,
		Category:           flagCategory,
		IncludeDomains:     flagIncludeDomains,
		ExcludeDomains:     flagExcludeDomains,
		StartPublishedDate: flagStartPublished,
		EndPublishedDate:   flagEndPublished,
		StartCrawlDate:     flagStartCrawl,
		EndCrawlDate:       flagEndCrawl,
		IncludeText:        flagIncludeText,
		ExcludeText:        flagExcludeText,
		UserLocation:       flagUserLocation,
		Moderation:         flagModeration,
	}

	// Only attach the nested `contents` object if at least one enrichment
	// flag is set; a zero-valued struct would still emit `"contents":{}` on
	// the wire, which Exa treats as "return text by default" — not what the
	// user asked for.
	if flagSearchText || flagSearchSummary || flagSearchHL > 0 || flagSearchSubpages > 0 {
		req.Contents = &client.SearchContents{
			Text:       flagSearchText,
			Summary:    flagSearchSummary,
			Highlights: flagSearchHL,
			Subpages:   flagSearchSubpages,
		}
	}

	return req, nil
}

// writeSearchDryRun prints the planned POST /search request as JSON without
// making a network call. Mirrors `exa contents --dry-run`: the API key is
// never rendered — Authorization lives at the HTTP layer, not in the body.
func writeSearchDryRun(req client.SearchRequest) error {
	plan := map[string]any{
		"schema_version": "1",
		"provider":       "exa",
		"command":        "search",
		"dry_run":        true,
		"request": map[string]any{
			"method":  "POST",
			"url":     client.DefaultBaseURL + "/search",
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
