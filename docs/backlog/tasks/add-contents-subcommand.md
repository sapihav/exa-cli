---
title: M2 — `contents` subcommand (web_fetch_exa parity)
type: task
priority: P1
status: todo
created: 2026-04-18
---

# M2 — `contents` subcommand

## Problem Statement

Exa MCP's `web_fetch_exa` (default-on tool) wraps `POST /contents` to return clean markdown + metadata for one or more URLs. Our CLI has no way to fetch URL contents — agents must shell out to a different tool.

## Acceptance Criteria

- `exa contents <url> [<url>...]` — POST `/contents`.
  - Accepts URLs as args, newline-separated on stdin (`-`), or comma-separated via `--urls`.
  - Flags: `--text` (return plain text, default false), `--summary` (LLM summary), `--highlights N` (top-N highlight snippets), `--subpages N` (crawl N subpages), `--livecrawl never|fallback|always|preferred`.
  - Envelope: `result.contents[].{id, url, title, text?, summary?, highlights[]?, subpages[]?}`.
- Standard flags (`--pretty`, `--out`, `--max-retries`, etc.) work.
- Dry-run prints exact request body.

## Context / Notes

- Endpoint: https://docs.exa.ai/reference/get-contents
- Budget: ~300 LoC (new `cmd/contents.go`, client method, models, tests).
- Batch capability: send multiple URLs in one request — don't loop N single-URL calls.
