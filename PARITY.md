# exa-cli Parity Matrix

Capability map across the upstream Exa AI HTTP API, the official Exa MCP server, and the `exa` CLI in this repo. Use this to see at a glance what is shipped, planned (with milestone), or intentionally skipped.

Last audited: 2026-04-25 (M4)
Sources: `https://exa.ai/docs/reference` (REST API), `https://github.com/exa-labs/exa-mcp-server` (MCP tools), `docs/backlog/_index.md` + `docs/backlog/tasks/*` (planned work), shipped commands via `exa --help` / `cmd/*.go`.

## Matrix

| API endpoint | MCP tool | CLI command | Status | Notes |
|---|---|---|---|---|
| `POST /search` (auto/neural/keyword) | `web_search_exa` (default) | `exa search "<q>"` | shipped (M1) | Core search. |
| `POST /search` (advanced filters) | `web_search_advanced_exa` (off-by-default) | `exa search` w/ `--include-domain`, `--exclude-domain`, `--start-published`, `--end-published`, `--start-crawl`, `--end-crawl`, `--include-text`, `--exclude-text`, `--user-location`, `--moderation`, `--highlights`, `--summary`, `--subpages`, `--text` | shipped (M3) | One unified `search` command covers default + advanced. |
| `POST /search` (`category=company`) | `company_research_exa` (deprecated) | `exa search --category company` | shipped (M3) | MCP tool deprecated; CLI uses `--category`. |
| `POST /search` (`category=people`) | `people_search_exa` (deprecated) | `exa search --category people` | shipped (M3) | MCP tool deprecated. |
| `POST /search` (`category=linkedin_profile`) | `linkedin_search_exa` (deprecated) | `exa search --category linkedin_profile` | shipped (M3) | MCP tool deprecated. |
| `POST /contents` | `web_fetch_exa` (default) | `exa contents <url>...` | shipped (M2) | Batched single request; supports `--text`, `--summary`, `--highlights`, `--subpages`, `--livecrawl`, stdin `-`, `--urls`. |
| `POST /contents` (code-context vertical) | `get_code_context_exa` (deprecated) | — | skipped | MCP tool deprecated upstream; backlog idea only (`exa context`). Skip unless requested. |
| `POST /search` (crawl mode) | `crawling_exa` (deprecated) | partial via `exa search --subpages N` and `exa contents --subpages N --livecrawl ...` | shipped (M2/M3) | No standalone `crawl` command; subpage/livecrawl knobs cover the use case. |
| `POST /findSimilar` | — (not exposed by MCP) | `exa find-similar <url>` | shipped (M4) | CLI-native advantage over MCP — full filter + enrichment surface plus `--exclude-source-domain`. |
| `POST /answer` | — (not exposed by MCP) | `exa answer "<q>"` | planned (M5) | CLI-native advantage over MCP. P2. Streaming SSE listed in backlog Ideas. |
| `POST /research` (create task) | `deep_researcher_start` (deprecated) | `exa research start "<prompt>"` | planned (M6) | P1. Backlog also defines `exa research run` as submit+poll convenience. |
| `GET /research/{id}` | `deep_researcher_check` (deprecated) | `exa research check <id>` | planned (M6) | Pairs with `research start`. |
| `POST /research` (single-shot) | `deep_search_exa` (deprecated) | covered by `exa research run` | planned (M6) | The `run` convenience subsumes the deprecated single-shot tool. |
| Websets API (`/websets/...`) | — (separate Websets MCP server, out of scope here) | — | skipped (separate product) | Listed under backlog Ideas; would be its own CLI track or subcommand group. |
| Monitors API | — | — | skipped (out of scope) | Not in backlog; recurring searches + webhook delivery are not a CLI primitive. |
| `schema` (introspection, workspace contract) | n/a | `exa schema` | planned (M7) | Required by workspace `CLAUDE.md` contract; not yet shipped (`exa schema` currently returns "unknown command"). |
| n/a | n/a | `exa version` | shipped | Local. |
| n/a | n/a | `exa completion` | shipped | Cobra default. |

## Contract flags (workspace standard)

These are CLI-side, not API/MCP — tracked here because M7 closes the gap.

| Flag | Status | Notes |
|---|---|---|
| `--pretty`, `--quiet`, `--verbose`, `--out`, `-h/--help` | shipped | Root flags. |
| `--dry-run` | shipped on `search`, `contents` | M7 will assert it on every future networked subcommand. |
| `--max-retries` | shipped on `search`, `contents` | Default 3. |
| `--json-errors` | planned (M7) | Workspace contract. |
| `--rate-limit N/s` | planned (M7) | Default 5/s per ROADMAP §5.3. |
| `--timeout SEC` | planned (M7) | Per-command defaults. |
| `--user-agent` (+ `EXA_USER_AGENT`) | planned (M7) | |
| stdin `-` | shipped on `contents`; planned for all query/URL inputs (M7) | `search` does not yet accept `-`. |

## Gaps

Items in the upstream API or MCP with no CLI counterpart and no current backlog entry — surfaced for the user to triage:

- **Websets API** (`/websets/*`) — flagged as an Idea in the backlog, not a task. No milestone assigned.
- **Monitors API** (recurring searches + webhooks) — not in backlog at all. Likely intentional (CLIs are stateless), but worth confirming.
- **`web_search_exa` "instant / fast / deep-lite / deep / deep-reasoning" search-type variants** — the upstream `/search` endpoint accepts richer `type` values than the CLI's `--type {neural|keyword|auto}` flag exposes.? Not on the backlog.
- **`get_code_context_exa` / Code Context** — listed as Idea ("skip unless requested"). MCP deprecated, but the underlying `POST /context` (?) endpoint still exists.
- **`exa schema`** — required by the workspace contract; currently absent. Tracked under M7 but worth calling out as a gap today.
- **Streaming output (`--stream`)** for `answer` / `research run` via SSE — listed as Idea, no milestone.
