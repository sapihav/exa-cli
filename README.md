# exa-cli

Thin Go CLI wrapping the [Exa AI](https://exa.ai) search API. Binary name: `exa`.
Agent-friendly: JSON on stdout, logs on stderr, documented exit codes.

> Unrelated to the [`exa`](https://the.exa.website/) file-listing tool. Same
> name on `$PATH` — pick whichever you need per shell.

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
```

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

## Status

Milestone 2 shipped: `search`, `contents`. Follow-ups on the backlog:
richer search filters, `find-similar`, `answer`, and async `research`.

## License

MIT — see [LICENSE](./LICENSE).
