# exa-cli

Thin CLI wrapper for the Exa AI API (semantic + neural search, people/company, crawl, deep research) — `exa` binary. Agent-friendly JSON output.

## Sources of truth

- `../CLI-tools-ROADMAP.md` §5.3 — full spec: commands, env vars, rate limits
- `../CLI-tools-ROADMAP.md` §3 — shared conventions across all CLIs
- `../CLI-tools-ROADMAP.md` §4 — security invariants (env-only auth, no shell injection, secret redaction)
- `docs/backlog/_index.md` — current work queue. **Read before starting any task.**

## Status

**M1 shipped** — `search` subcommand. Stack: **Go 1.25.6** + `spf13/cobra` + stdlib `net/http` (overrides ROADMAP §2's Python stack).

## Auth

`EXA_API_KEY` env var only. No file fallback. If absent: exit code 2 with link to provider's key page.

## Output contract

- **stdout**: always valid JSON, one object per invocation, common envelope with `schema_version`, `provider`, `command`, `query`, `result`, optional `citations`, `cost_usd`, `elapsed_ms`
- **stderr**: human-readable progress + errors. With `--json-errors`: structured `{error:{message,code,hint?,docs_url?}}`
- **Exit codes**: `0` success / `1` user/config error / `2` API error / `3` network error
- **`--pretty`**: indent JSON for humans

## Standard flags (every subcommand)

`--out <file>`, `--pretty`, `--quiet`, `--verbose`, `--rate-limit N/s` (default 5/s for Exa), `--max-retries N`, `--timeout SEC`, `--dry-run`, `--user-agent`, `--json-errors`. Stdin: `-` accepted wherever a query/URL is.

## Self-describing

`exa schema` returns the full command tree as JSON. Agents prefer this over scraping `--help`.

## Conventions

- **Commits**: conventional — `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`, `perf:`, `ci:`. Breaking change: `type(scope)!:`.
- **Branches**: `feat/<slug>`, `fix/<slug>`, `chore/<slug>`, `docs/<slug>` matching the backlog item's filename.
- **One milestone = one PR**, ≤500 LoC source code (see `.claude/rules/incremental-delivery.md`). Refuse to batch milestones.
- **No shell injection by construction.** Build payloads via JSON serializer, never string-interpolate user input (ROADMAP §4.1).
- **No telemetry, no auto-update, no config files** (ROADMAP §9).

## Backlog workflow

`.claude/rules/backlog.md` is the canonical rule. Quick form:
- Check `docs/backlog/_index.md` before starting
- Set frontmatter `status: in-progress` when you begin
- Set `status: done` and remove from `_index.md` when finished

Use `/feature-create`, `/feature-implement`, `/bug-fix`, `/bug-report` (global skills) — they already write to `docs/backlog/`.

## Subagent delegation

Selective, not default.

- **Explore** — open-ended "where/how" searches across the repo
- **code-reviewer** — mandatory before opening a PR for a milestone
- **senior-backend-engineer** — multi-file milestones (≥3 files)
- **system-architect** — when designing a new command surface or breaking output change
- **Skip subagents for**: typo fixes, one-line changes, single-file edits

## Testing expectations

Per ROADMAP §8: mocked HTTP per command, golden-response replay, error-path tests (401/429/500/timeout), `--dry-run` assertions including secret redaction. Coverage ≥ 80%, 100% on retry/ratelimit logic. No real-API hits in CI.
