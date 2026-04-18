---
title: M3 — `search` filter upgrade (advanced/company/people parity)
type: task
priority: P1
status: todo
created: 2026-04-18
---

# M3 — `search` filter upgrade

## Problem Statement

Exa MCP's `web_search_advanced_exa`, `company_research_exa`, and `people_search_exa` (and the deprecated `linkedin_search_exa`) are all `/search` calls with richer filters. Our CLI's `search` supports only `--num-results` and `--type` — it cannot replicate any of these tools. All four are one flag-expansion away.

## Acceptance Criteria

Extend `exa search` with:

- `--category research_paper|news|pdf|github|tweet|movie|song|personal_site|linkedin_profile|financial_report|company|people` — covers company/people/linkedin MCP tools via `--category`.
- `--include-domain DOMAIN` (repeatable), `--exclude-domain DOMAIN` (repeatable).
- `--start-published YYYY-MM-DD`, `--end-published YYYY-MM-DD`.
- `--start-crawl YYYY-MM-DD`, `--end-crawl YYYY-MM-DD`.
- `--include-text STR` (repeatable, ≤5), `--exclude-text STR` (repeatable, ≤5).
- `--user-location ISO-COUNTRY`.
- `--moderation` (boolean).
- Content enrichment: `--highlights`, `--summary`, `--subpages N`, `--text` (inline contents in search response — avoids separate `/contents` call).

Output: preserve existing envelope shape; add optional `result.results[].{highlights?, summary?, subpages?, text?}` fields when requested.

## Context / Notes

- Depends on M2 models (shares `ContentsOptions` struct).
- Endpoint: https://docs.exa.ai/reference/search
- Budget: ~400 LoC.
- Drop a short note in README: "For company-scoped or people-scoped search use `--category company` / `--category people` (replaces MCP's `company_research_exa` / `people_search_exa`)."
