# exa-cli Backlog

## Active

_(work currently in flight)_

## Up Next

1. [M3 — `search` filter upgrade](tasks/upgrade-search-filters.md) — P1 (MCP `web_search_advanced_exa` + company/people parity)
2. [M4 — `find-similar` subcommand](tasks/add-find-similar-subcommand.md) — P2 (beyond MCP)
3. [M5 — `answer` subcommand](tasks/add-answer-subcommand.md) — P2 (beyond MCP)
4. [M6 — `research start` + `check` (async)](tasks/add-async-research-commands.md) — P1 (MCP `deep_researcher_*`)
5. [M7 — contract hardening](tasks/add-contract-hardening.md) — P2 (workspace standard)

## Backlog

_(known work, not yet prioritized)_

## Ideas

- Websets (`exa websets …`) — separate upstream product with its own MCP server; tackle as a second-track CLI or a subcommand group if demand surfaces.
- Code context (`exa context`) — wraps `POST /context`. MCP tool is deprecated upstream; skip unless requested.
- Streaming (`--stream`) for `answer` and `research run` via SSE.

<!-- Done items are not tracked here. Completed items have status: done in their frontmatter. Files remain in tasks/bugs/ for historical reference. -->
