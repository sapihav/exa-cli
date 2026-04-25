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

// find-similar shares the category enum, dateRE, validLivecrawlModes, and
// the envelope/writeJSON/mapClientError/userError helpers from search.go and
// contents.go. Only the request shape and a few flags differ.
var (
	flagFSNumResults     int
	flagFSExcludeSource  bool
	flagFSCategory       string
	flagFSIncludeDomains []string
	flagFSExcludeDomains []string
	flagFSStartPublished string
	flagFSEndPublished   string
	flagFSStartCrawl     string
	flagFSEndCrawl       string
	flagFSIncludeText    []string
	flagFSExcludeText    []string
	flagFSUserLocation   string
	flagFSModeration     bool
	flagFSText           bool
	flagFSSummary        bool
	flagFSHighlights     int
	flagFSSubpages       int
	flagFSLivecrawl      string
	flagFSDryRun         bool
	flagFSMaxRetries     int
)

var findSimilarCmd = &cobra.Command{
	Use:   "find-similar <url>",
	Short: "Find pages similar to the given URL via the Exa /findSimilar endpoint",
	Long: `Find pages similar to a URL via POST /findSimilar and print the response
as JSON on stdout.

The URL can be passed as a positional arg or piped on stdin (use "-" as
the single arg). The CLI exposes the same filter and enrichment surface as
` + "`exa search`" + ` plus ` + "`--exclude-source-domain`" + ` to drop hits from the
same domain as the input URL.

Example:
  EXA_API_KEY=... exa find-similar https://arxiv.org/abs/2307.06435 --num-results 5
  EXA_API_KEY=... exa find-similar https://stripe.com --exclude-source-domain --text
  echo "https://exa.ai" | EXA_API_KEY=... exa find-similar - --category company

Exit codes:
  0  success
  1  API error (HTTP >= 400)
  2  user/config error (missing key, invalid flag)
  3  network error`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFindSimilar,
}

func init() {
	findSimilarCmd.Flags().IntVarP(&flagFSNumResults, "num-results", "n", 10, "number of results to return")
	findSimilarCmd.Flags().IntVar(&flagFSMaxRetries, "max-retries", 3, "maximum retry attempts on 429/5xx")

	findSimilarCmd.Flags().BoolVar(&flagFSExcludeSource, "exclude-source-domain", false, "exclude results from the same domain as the input URL")
	findSimilarCmd.Flags().StringVar(&flagFSCategory, "category", "", "result category (research_paper|news|pdf|github|tweet|movie|song|personal_site|linkedin_profile|financial_report|company|people)")
	findSimilarCmd.Flags().StringArrayVar(&flagFSIncludeDomains, "include-domain", nil, "only return results from this domain (repeatable)")
	findSimilarCmd.Flags().StringArrayVar(&flagFSExcludeDomains, "exclude-domain", nil, "exclude results from this domain (repeatable)")
	findSimilarCmd.Flags().StringVar(&flagFSStartPublished, "start-published", "", "earliest publish date YYYY-MM-DD")
	findSimilarCmd.Flags().StringVar(&flagFSEndPublished, "end-published", "", "latest publish date YYYY-MM-DD")
	findSimilarCmd.Flags().StringVar(&flagFSStartCrawl, "start-crawl", "", "earliest crawl date YYYY-MM-DD")
	findSimilarCmd.Flags().StringVar(&flagFSEndCrawl, "end-crawl", "", "latest crawl date YYYY-MM-DD")
	findSimilarCmd.Flags().StringArrayVar(&flagFSIncludeText, "include-text", nil, "text that must appear in the result (repeatable, max 5)")
	findSimilarCmd.Flags().StringArrayVar(&flagFSExcludeText, "exclude-text", nil, "text that must not appear in the result (repeatable, max 5)")
	findSimilarCmd.Flags().StringVar(&flagFSUserLocation, "user-location", "", "ISO 3166-1 alpha-2 country code (e.g. US)")
	findSimilarCmd.Flags().BoolVar(&flagFSModeration, "moderation", false, "enable Exa content moderation")

	findSimilarCmd.Flags().BoolVar(&flagFSText, "text", false, "return the full page text inline on each result")
	findSimilarCmd.Flags().BoolVar(&flagFSSummary, "summary", false, "return an LLM-generated summary inline on each result")
	findSimilarCmd.Flags().IntVar(&flagFSHighlights, "highlights", 0, "return top-N highlight snippets per result (0 = off)")
	findSimilarCmd.Flags().IntVar(&flagFSSubpages, "subpages", 0, "crawl up to N subpages per result (0 = off)")
	findSimilarCmd.Flags().StringVar(&flagFSLivecrawl, "livecrawl", "", "livecrawl mode: never|fallback|always|preferred")
	findSimilarCmd.Flags().BoolVar(&flagFSDryRun, "dry-run", false, "print the planned request body and exit; no network call")

	rootCmd.AddCommand(findSimilarCmd)
}

// newFindSimilarClient is a package-level seam so tests can inject a client
// pointed at httptest.NewServer without setting EXA_API_KEY. Same pattern as
// newSearchClient in search.go.
var newFindSimilarClient = func() (*client.Client, error) {
	return client.New(os.Getenv("EXA_API_KEY"), client.WithMaxRetries(flagFSMaxRetries))
}

// runFindSimilar is the RunE for `exa find-similar`.
func runFindSimilar(cmd *cobra.Command, args []string) error {
	start := time.Now()

	url, err := resolveFindSimilarURL(args, cmd.InOrStdin())
	if err != nil {
		return userError(err.Error())
	}
	if url == "" {
		return userError("URL is required (pass as arg or pipe via stdin with '-')")
	}

	req, err := buildFindSimilarRequest(url)
	if err != nil {
		return userError(err.Error())
	}

	if flagFSDryRun {
		return writeFindSimilarDryRun(req)
	}

	c, err := newFindSimilarClient()
	if err != nil {
		if errors.Is(err, client.ErrMissingAPIKey) {
			return userError(err.Error())
		}
		return userError("client init: " + err.Error())
	}

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "exa: POST /findSimilar url=%q num=%d category=%q exclude_source=%t\n",
			url, flagFSNumResults, flagFSCategory, flagFSExcludeSource)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	resp, err := c.FindSimilar(ctx, req)
	if err != nil {
		return mapClientError(err)
	}

	return writeJSON(envelope{
		SchemaVersion: "1",
		Provider:      "exa",
		Command:       "find-similar",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        resp,
	})
}

// resolveFindSimilarURL extracts the input URL from argv or stdin. A bare "-"
// arg (or no arg at all when stdin is non-interactive) reads the first
// non-empty line from stdin. Multi-line stdin only consumes the first URL —
// /findSimilar takes a single URL per request.
func resolveFindSimilarURL(args []string, stdin io.Reader) (string, error) {
	if len(args) == 1 && args[0] != "-" {
		return strings.TrimSpace(args[0]), nil
	}
	if len(args) == 0 || args[0] == "-" {
		scanner := bufio.NewScanner(stdin)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			s := strings.TrimSpace(scanner.Text())
			if s != "" {
				return s, nil
			}
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
	}
	return "", nil
}

// buildFindSimilarRequest validates flags and assembles the request body.
// Validation rules mirror buildSearchRequest where the API surface overlaps;
// errors are returned as plain errors so the caller can wrap them in userError
// for exit-code-2 mapping.
func buildFindSimilarRequest(url string) (client.FindSimilarRequest, error) {
	if flagFSNumResults < 1 {
		return client.FindSimilarRequest{}, errors.New("--num-results must be >= 1")
	}
	if flagFSMaxRetries < 0 {
		return client.FindSimilarRequest{}, errors.New("--max-retries must be >= 0")
	}
	if flagFSCategory != "" && !validCategories[flagFSCategory] {
		return client.FindSimilarRequest{}, fmt.Errorf("invalid --category %q", flagFSCategory)
	}
	for _, d := range []struct{ name, val string }{
		{"--start-published", flagFSStartPublished},
		{"--end-published", flagFSEndPublished},
		{"--start-crawl", flagFSStartCrawl},
		{"--end-crawl", flagFSEndCrawl},
	} {
		if d.val != "" && !dateRE.MatchString(d.val) {
			return client.FindSimilarRequest{}, fmt.Errorf("invalid %s %q (want YYYY-MM-DD)", d.name, d.val)
		}
	}
	if len(flagFSIncludeText) > 5 {
		return client.FindSimilarRequest{}, errors.New("--include-text accepts at most 5 values")
	}
	if len(flagFSExcludeText) > 5 {
		return client.FindSimilarRequest{}, errors.New("--exclude-text accepts at most 5 values")
	}
	if flagFSHighlights < 0 {
		return client.FindSimilarRequest{}, errors.New("--highlights must be >= 0")
	}
	if flagFSSubpages < 0 {
		return client.FindSimilarRequest{}, errors.New("--subpages must be >= 0")
	}
	if flagFSLivecrawl != "" && !validLivecrawlModes[flagFSLivecrawl] {
		return client.FindSimilarRequest{}, fmt.Errorf("invalid --livecrawl %q (want never|fallback|always|preferred)", flagFSLivecrawl)
	}

	req := client.FindSimilarRequest{
		URL:                 url,
		NumResults:          flagFSNumResults,
		ExcludeSourceDomain: flagFSExcludeSource,
		Category:            flagFSCategory,
		IncludeDomains:      flagFSIncludeDomains,
		ExcludeDomains:      flagFSExcludeDomains,
		StartPublishedDate:  flagFSStartPublished,
		EndPublishedDate:    flagFSEndPublished,
		StartCrawlDate:      flagFSStartCrawl,
		EndCrawlDate:        flagFSEndCrawl,
		IncludeText:         flagFSIncludeText,
		ExcludeText:         flagFSExcludeText,
		UserLocation:        flagFSUserLocation,
		Moderation:          flagFSModeration,
	}

	// Mirror search.go: only attach the nested contents object when at least
	// one enrichment knob is set, otherwise an empty `"contents":{}` would
	// change server-side semantics (Exa treats it as "return text by default").
	if flagFSText || flagFSSummary || flagFSHighlights > 0 || flagFSSubpages > 0 || flagFSLivecrawl != "" {
		req.Contents = &client.SearchContents{
			Text:       flagFSText,
			Summary:    flagFSSummary,
			Highlights: flagFSHighlights,
			Subpages:   flagFSSubpages,
		}
	}

	return req, nil
}

// writeFindSimilarDryRun prints the planned POST /findSimilar request without
// making a network call. The API key never appears in the body — Authorization
// lives at the HTTP layer and the dry-run output reflects that.
func writeFindSimilarDryRun(req client.FindSimilarRequest) error {
	plan := map[string]any{
		"schema_version": "1",
		"provider":       "exa",
		"command":        "find-similar",
		"dry_run":        true,
		"request": map[string]any{
			"method":  "POST",
			"url":     client.DefaultBaseURL + "/findSimilar",
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
