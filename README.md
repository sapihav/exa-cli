# exa-cli

Thin Go CLI wrapping the [Exa AI](https://exa.ai) search API. Binary name: `exa`.
Agent-friendly: JSON on stdout, logs on stderr, documented exit codes.

> Unrelated to the [`exa`](https://the.exa.website/) file-listing tool. Same
> name on `$PATH` — pick whichever you need per shell.

## Parity

`███████████████░░░░░` **75%** — `search` (incl. category variants), `contents`, `find-similar`, `answer` shipped. Deep `research` (M6), `exa schema` + contract-flag hardening (M7) remain. See [PARITY.md](PARITY.md).

## Install

**Homebrew (macOS)** — recommended on Mac:

```sh
brew install sapihav/tap/exa
```

The tap auto-installs on first use; subsequent `brew upgrade` picks up new releases. Note: scoping with `sapihav/tap/` avoids any collision with the deprecated upstream `exa` (ls replacement, now `eza`).

**One-line install (Linux / macOS)** — no Go toolchain required:

```sh
curl -sSL https://raw.githubusercontent.com/sapihav/exa-cli/main/install.sh | bash
```

Downloads the latest release for your OS/arch, verifies SHA-256, installs `exa` to `/usr/local/bin`. Override with `INSTALL_DIR=$HOME/.local/bin`. Requires `curl` + `jq`.

**From source** (requires Go 1.25+):

```sh
go install github.com/sapihav/exa-cli@latest
```

The binary is named `exa` and installed to `$(go env GOBIN)` (or
`$(go env GOPATH)/bin`). Add it to your `PATH`.

## Auth

Get a key at https://dashboard.exa.ai/api-keys and export it:

```sh
export EXA_API_KEY="exa_..."
```

Env var is the only accepted source. Missing key → exit code 2.

## Usage — `exa search`

```sh
exa search "best open-source vector databases"
exa search "rust async runtime" --type keyword --num-results 5 --pretty
exa search "OSINT tools 2026" --out results.json

# Company / people research (MCP parity)
exa search "Stripe" --category company --num-results 3
exa search "Guido van Rossum" --category people

# Filter + inline contents in one call
exa search "attention is all you need" --category research_paper \
  --include-domain arxiv.org --start-published 2026-01-01 \
  --text --summary --highlights 3
```

> `--category company` and `--category people` replace the Exa MCP server's
> `company_research_exa` and `people_search_exa` tools — one flag, same
> upstream endpoint, no separate subcommand.

### Flags

| Flag | Default | Description |
|---|---|---|
| `-n, --num-results N` | `10` | Number of results to return |
| `-t, --type neural\|keyword\|auto` | `auto` | Search type |
| `--max-retries N` | `3` | Retry attempts on `429` / `5xx` (exponential backoff) |
| `--pretty` | `false` | Indent JSON output |
| `-o, --out FILE` | stdout | Write JSON to file |
| `-v, --verbose` | `false` | Log request summary to stderr |
| `-q, --quiet` | `false` | Suppress stderr logs |
| `--dry-run` | `false` | Print the planned request (API key redacted) and exit |

#### Filters

| Flag | Description |
|---|---|
| `--category CAT` | One of: `research_paper`, `news`, `pdf`, `github`, `tweet`, `movie`, `song`, `personal_site`, `linkedin_profile`, `financial_report`, `company`, `people` |
| `--include-domain DOMAIN` | Only return results from this domain (repeatable) |
| `--exclude-domain DOMAIN` | Exclude results from this domain (repeatable) |
| `--start-published YYYY-MM-DD` | Earliest publish date |
| `--end-published YYYY-MM-DD` | Latest publish date |
| `--start-crawl YYYY-MM-DD` | Earliest crawl date |
| `--end-crawl YYYY-MM-DD` | Latest crawl date |
| `--include-text STR` | Text that must appear in the result (repeatable, max 5) |
| `--exclude-text STR` | Text that must not appear in the result (repeatable, max 5) |
| `--user-location CC` | ISO 3166-1 alpha-2 country code (e.g. `US`) |
| `--moderation` | Enable Exa content moderation |

Upstream rejects some combinations (e.g. date filters with `--category company`
or `--category people`) with a 400 — the CLI does not duplicate that policy;
the server's error is surfaced as-is.

#### Inline content enrichment

Match the flags on `exa contents`; setting any of these embeds the enrichment
directly in each search result and avoids a separate `/contents` round-trip.

| Flag | Description |
|---|---|
| `--text` | Return the full page text inline |
| `--summary` | Return an LLM-generated summary inline |
| `--highlights N` | Return top-N highlight snippets (0 = off) |
| `--subpages N` | Crawl up to N subpages per result (0 = off) |

### Example output

Successful invocations emit a single JSON envelope on stdout. Errors go to
stderr and are not wrapped (see [Exit codes](#exit-codes)).

```json
{
  "schema_version": "1",
  "provider": "exa",
  "command": "search",
  "elapsed_ms": 1234,
  "result": {
    "requestId": "req_abc123",
    "autopromptString": "best open-source vector databases",
    "results": [
      {
        "title": "Weaviate: open-source vector database",
        "url": "https://weaviate.io",
        "id": "w_1",
        "publishedDate": "2025-11-14",
        "author": "Weaviate",
        "score": 0.912
      },
      {
        "title": "Qdrant — vector similarity search engine",
        "url": "https://qdrant.tech",
        "id": "q_1",
        "score": 0.887
      }
    ]
  }
}
```

- `schema_version` — output contract version. Bumped on breaking changes.
- `provider` — always `exa`.
- `command` — the subcommand that was run (`search` in M1).
- `elapsed_ms` — wall-clock time from command start to response marshal, integer ms.
- `result` — the raw provider response (see Exa's `/search` docs for the full schema).

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | API error (HTTP `>= 400` after retries) |
| `2` | User / config error (missing key, bad flag, empty query) |
| `3` | Network error (DNS, TCP, TLS, timeout) |

## Usage — `exa contents`

Fetches clean content (text, summary, highlights, subpages) for one or more
URLs via Exa's `/contents` endpoint. Batches multiple URLs into a single
request.

```sh
exa contents https://example.com https://anotherexample.com --pretty
exa contents https://example.com --text --highlights 3
exa contents https://example.com --summary --livecrawl preferred
exa contents --urls https://a.com,https://b.com --subpages 2

# From stdin (newline-separated)
printf 'https://a.com\nhttps://b.com\n' | exa contents - --text
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--urls a,b,c` | — | Comma-separated URLs (in addition to args / stdin) |
| `--text` | `false` | Return the full page text |
| `--summary` | `false` | Return an LLM-generated summary |
| `--highlights N` | `0` | Return the top N highlight snippets |
| `--subpages N` | `0` | Crawl N subpages per URL |
| `--livecrawl never\|fallback\|always\|preferred` | unset | Live-crawl behaviour |
| `--max-retries N` | `3` | Retry attempts on `429` / `5xx` |
| `--dry-run` | `false` | Print the planned request (API key redacted) and exit |

URLs can be passed as positional args, via `--urls`, or on stdin when any
arg is `-`. Duplicates are deduped preserving first-seen order.

## Usage — `exa find-similar`

Find pages similar to a URL via Exa's `/findSimilar` endpoint. This capability
is **not exposed by the Exa MCP server** — it is a CLI-native advantage.
Shares the full filter + enrichment surface of `exa search`, plus
`--exclude-source-domain` to drop hits from the input URL's own domain.

```sh
exa find-similar https://arxiv.org/abs/2307.06435 --num-results 5 --pretty
exa find-similar https://stripe.com --exclude-source-domain --category company
echo "https://exa.ai" | exa find-similar - --text --summary
```

URL can be passed as the positional arg or piped on stdin (use `-`). All the
filter / enrichment flags from `exa search` are accepted (`--category`,
`--include-domain`, `--exclude-domain`, `--start-published`, `--end-published`,
`--start-crawl`, `--end-crawl`, `--include-text`, `--exclude-text`,
`--user-location`, `--moderation`, `--text`, `--summary`, `--highlights N`,
`--subpages N`, `--livecrawl`).

## Usage — `exa answer`

Synthesize a direct, cited answer to a question via Exa's `/answer` endpoint.
This capability is **not exposed by the Exa MCP server** — it is a CLI-native
advantage. One round-trip beats `search` + manual synthesis when you just
want a fast, cited reply.

```sh
exa answer "what is the capital of France?" --pretty
exa answer "summarize the latest LLM scaling research" --text
echo "who founded Stripe?" | exa answer -
exa answer "list the top 3 vector DBs" \
  --output-schema '{"type":"object","properties":{"dbs":{"type":"array","items":{"type":"string"}}}}'
```

The question can be a positional arg or piped via stdin (use `-`). Multi-line
stdin is joined into a single query.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--text` | `false` | Include source text on each citation |
| `--output-schema <json>` | unset | JSON Schema (Draft 7) — answer is returned as structured JSON |
| `--max-retries N` | `3` | Retry attempts on `429` / `5xx` |
| `--dry-run` | `false` | Print the planned request (API key redacted) and exit |

Streaming (SSE) is not yet supported — see the backlog Ideas.

## Status

Milestones 1-5 shipped: `search` (with category/domain/date/text filters and
inline contents), `contents`, `find-similar`, `answer`. Follow-ups on the
backlog: async `research` (M6), `exa schema` + contract-flag hardening (M7).

## License

MIT — see [LICENSE](./LICENSE).
