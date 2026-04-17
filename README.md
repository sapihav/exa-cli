# exa-cli

Thin Go CLI wrapping the [Exa AI](https://exa.ai) search API. Binary name: `exa`.
Agent-friendly: JSON on stdout, logs on stderr, documented exit codes.

> Unrelated to the [`exa`](https://the.exa.website/) file-listing tool. Same
> name on `$PATH` — pick whichever you need per shell.

## Install

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

```json
{
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
```

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | API error (HTTP `>= 400` after retries) |
| `2` | User / config error (missing key, bad flag, empty query) |
| `3` | Network error (DNS, TCP, TLS, timeout) |

## Status

Milestone 1: `search` only. Follow-ups (`find-similar`, `contents`, `answer`)
are out of scope for now.

## License

MIT.
