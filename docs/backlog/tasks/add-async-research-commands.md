---
title: M6 — `research start` + `research check` (async)
type: task
priority: P1
status: todo
created: 2026-04-18
---

# M6 — `research start` + `research check`

## Problem Statement

MCP's `deep_researcher_start` / `deep_researcher_check` wrap `POST /research` and `GET /research/{id}` — an async job flow. The CLI has no access to Exa's deep research product today.

## Acceptance Criteria

- `exa research start "<prompt>"` — POST `/research`.
  - Flags: `--model` (research tier if applicable), `--output-schema @file.json` for structured output.
  - Envelope: `result.{research_id, status, created_at}`.
- `exa research check <research_id>` — GET `/research/{id}`.
  - Envelope: `result.{research_id, status, report?, sources[]?, created_at, completed_at?}`.
- `exa research run "<prompt>"` (convenience) — submit + poll with exponential backoff until terminal state.
  - `--poll-interval SEC` (default 15s), `--max-wait SEC` (default 1800s).

## Context / Notes

- Endpoint: https://docs.exa.ai/reference/create-research-task (verify exact paths at implementation).
- Independent of M3–M5.
- Budget: ~400 LoC (two/three commands + polling client + tests).
- Matches the async pattern we're building for perplexity-cli — reuse design.
