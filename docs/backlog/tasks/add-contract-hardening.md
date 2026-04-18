---
title: M7 — contract hardening (`--dry-run`, `--json-errors`, `schema`)
type: task
priority: P2
status: todo
created: 2026-04-18
---

# M7 — contract hardening

## Problem Statement

The workspace `CLAUDE.md` contract requires `--dry-run`, `--json-errors`, `--rate-limit`, `--timeout`, `--user-agent`, stdin `-` support, and a `schema` subcommand on every CLI. `exa-cli` ships several of these but not all, and any new milestone commands must respect them uniformly.

## Acceptance Criteria

- `exa schema` — emits full command tree as JSON (commands, flags, output shapes).
- `--dry-run` — prints HTTP method + URL + headers (API key redacted) + JSON body, exits 0, no network call. Works on every subcommand that makes network calls.
- `--json-errors` — switches stderr errors to `{error:{message,code,hint?,docs_url?}}`.
- `--rate-limit N/s` — client-side semaphore, default 5/s (per ROADMAP §5.3).
- `--timeout SEC` — default 30s for search/contents, 120s for answer, 900s for research check/start.
- `--user-agent` flag + `EXA_USER_AGENT` env override.
- stdin `-` support on any command that takes a URL or query.

## Context / Notes

- Can be implemented in parallel with or after M2–M6, but must be done before `v0.2.0`.
- Budget: ~300 LoC (mostly root.go + helper package + test updates across commands).
- Keep API key redaction tests strict — add a golden test asserting the bearer token never appears in `--verbose` or `--dry-run` output.
