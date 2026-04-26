---
title: M5 — `answer` subcommand (beyond MCP parity)
type: task
priority: P2
status: done
created: 2026-04-18
---

# M5 — `answer` subcommand

## Problem Statement

Exa's `POST /answer` returns a direct synthesized answer with citations — a one-shot alternative to `search` + manual synthesis. Not exposed by MCP. Useful when an agent wants a fast, cited answer without fetching full contents.

## Acceptance Criteria

- `exa answer "<question>"` — POST `/answer`.
  - Accepts query as arg or stdin.
  - Flags: `--text` (include raw source text alongside answer), `--model` (if the API surfaces model selection — verify at implementation time).
- Envelope: `result.{answer, citations[]}` where `citations[].{title, url, published_date?, text?}`.

## Context / Notes

- Endpoint: https://docs.exa.ai/reference/answer
- Independent of M3/M4 (different endpoint, simpler surface).
- Budget: ~200 LoC.
- **CLI-native advantage over MCP** — advertise in README.
