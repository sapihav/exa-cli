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

// Valid values for --livecrawl. Anything else is rejected with exit 2.
var validLivecrawlModes = map[string]bool{
	"never":     true,
	"fallback":  true,
	"always":    true,
	"preferred": true,
}

var (
	flagContentsText       bool
	flagContentsSummary    bool
	flagContentsHighlights int
	flagContentsSubpages   int
	flagContentsLivecrawl  string
	flagContentsURLs       string
	flagContentsDryRun     bool
	flagContentsRetries    int
)

var contentsCmd = &cobra.Command{
	Use:   "contents <url> [<url>...]",
	Short: "Fetch clean contents for one or more URLs via the Exa /contents endpoint",
	Long: `Fetch clean contents for one or more URLs via POST /contents and print the
response as JSON on stdout.

URLs can be provided as positional args, piped newline-separated on stdin
(use "-" as the single arg), or comma-separated via --urls. Mixing sources
is allowed; duplicates and blanks are removed.

All requested URLs go out in a single batched request — there is no per-URL
loop and no per-URL retry fan-out.

Example:
  EXA_API_KEY=... exa contents https://arxiv.org/abs/1706.03762 --text --summary
  printf "https://a.example\nhttps://b.example\n" | EXA_API_KEY=... exa contents -
  EXA_API_KEY=... exa contents --urls https://a.example,https://b.example --highlights 3

Exit codes:
  0  success
  1  API error (HTTP >= 400)
  2  user/config error (missing key, invalid flag)
  3  network error`,
	RunE: runContents,
}

func init() {
	contentsCmd.Flags().BoolVar(&flagContentsText, "text", false, "return plain text content for each URL")
	contentsCmd.Flags().BoolVar(&flagContentsSummary, "summary", false, "return an LLM-generated summary for each URL")
	contentsCmd.Flags().IntVar(&flagContentsHighlights, "highlights", 0, "return top-N highlight snippets per URL (0 = off)")
	contentsCmd.Flags().IntVar(&flagContentsSubpages, "subpages", 0, "crawl up to N subpages per URL (0 = off)")
	contentsCmd.Flags().StringVar(&flagContentsLivecrawl, "livecrawl", "", "livecrawl mode: never|fallback|always|preferred")
	contentsCmd.Flags().StringVar(&flagContentsURLs, "urls", "", "comma-separated list of URLs (alternative to positional args)")
	contentsCmd.Flags().BoolVar(&flagContentsDryRun, "dry-run", false, "print the planned request body and exit; no network call")
	contentsCmd.Flags().IntVar(&flagContentsRetries, "max-retries", 3, "maximum retry attempts on 429/5xx")
	rootCmd.AddCommand(contentsCmd)
}

// runContents is the RunE for `exa contents`.
func runContents(cmd *cobra.Command, args []string) error {
	start := time.Now()

	urls, err := collectURLs(args, flagContentsURLs, cmd.InOrStdin())
	if err != nil {
		return userError(err.Error())
	}
	if len(urls) == 0 {
		return userError("no URLs provided (pass as args, via --urls, or on stdin with '-')")
	}

	if flagContentsHighlights < 0 {
		return userError("--highlights must be >= 0")
	}
	if flagContentsSubpages < 0 {
		return userError("--subpages must be >= 0")
	}
	if flagContentsLivecrawl != "" && !validLivecrawlModes[flagContentsLivecrawl] {
		return userError(fmt.Sprintf("invalid --livecrawl %q (want never|fallback|always|preferred)", flagContentsLivecrawl))
	}
	if flagContentsRetries < 0 {
		return userError("--max-retries must be >= 0")
	}

	req := client.ContentsRequest{
		URLs:       urls,
		Text:       flagContentsText,
		Summary:    flagContentsSummary,
		Highlights: flagContentsHighlights,
		Subpages:   flagContentsSubpages,
		Livecrawl:  flagContentsLivecrawl,
	}

	if flagContentsDryRun {
		return writeDryRun(req)
	}

	apiKey := os.Getenv("EXA_API_KEY")
	c, err := client.New(apiKey, client.WithMaxRetries(flagContentsRetries))
	if err != nil {
		if errors.Is(err, client.ErrMissingAPIKey) {
			return userError(err.Error())
		}
		return userError("client init: " + err.Error())
	}

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "exa: POST /contents urls=%d text=%t summary=%t highlights=%d subpages=%d livecrawl=%q\n",
			len(urls), flagContentsText, flagContentsSummary, flagContentsHighlights, flagContentsSubpages, flagContentsLivecrawl)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	resp, err := c.Contents(ctx, req)
	if err != nil {
		return mapClientError(err)
	}

	return writeJSON(envelope{
		SchemaVersion: "1",
		Provider:      "exa",
		Command:       "contents",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        resp,
	})
}

// collectURLs merges URLs from positional args, the --urls flag, and stdin
// (activated when any arg is "-"). Whitespace is trimmed, empty entries
// and duplicates are dropped while preserving first-seen order.
func collectURLs(args []string, urlsFlag string, stdin io.Reader) ([]string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(args))

	add := func(raw string) {
		s := strings.TrimSpace(raw)
		if s == "" {
			return
		}
		if _, dup := seen[s]; dup {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	wantStdin := false
	for _, a := range args {
		if a == "-" {
			wantStdin = true
			continue
		}
		add(a)
	}

	if urlsFlag != "" {
		for _, part := range strings.Split(urlsFlag, ",") {
			add(part)
		}
	}

	if wantStdin {
		scanner := bufio.NewScanner(stdin)
		// Allow long lines (default 64KB may be tight for piped lists).
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			add(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
	}

	return out, nil
}

// writeDryRun prints the planned POST /contents request as JSON without
// making a network call. The API key is intentionally not rendered — the
// Authorization header lives at the HTTP layer and is not part of the body
// we ever echo back.
func writeDryRun(req client.ContentsRequest) error {
	plan := map[string]any{
		"schema_version": "1",
		"provider":       "exa",
		"command":        "contents",
		"dry_run":        true,
		"request": map[string]any{
			"method":  "POST",
			"url":     client.DefaultBaseURL + "/contents",
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
