---
title: M4 — `find-similar` subcommand (beyond MCP parity)
type: task
priority: P2
status: done
created: 2026-04-18
---

# M4 — `find-similar` subcommand

## Problem Statement

Exa's `POST /findSimilar` endpoint ("find pages similar to this URL") is not exposed by the MCP server but is a first-class API capability. Useful for OSINT / research workflows where the agent has an anchor URL and wants to expand outward.

## Acceptance Criteria

- `exa find-similar <url>` — POST `/findSimilar`.
  - Accepts URL as arg or stdin (`-`).
  - Shares the full M3 filter set (category, domains, dates, include/exclude-text, user-location, moderation, content enrichment).
  - Additional: `--exclude-source-domain` (bool) — exclude the same domain as the input URL from results.
- Envelope identical to `search` results shape.

## Context / Notes

- Endpoint: https://docs.exa.ai/reference/find-similar-links
- Depends on M3 (reuses filter struct + parsing).
- Budget: ~200 LoC.
- **CLI-native advantage over MCP** — advertise in README.
