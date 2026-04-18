# exa-cli

Thin Go CLI wrapping the Exa AI search API.

## Stack override

This CLI is **Go 1.25.6** (not Python per `~/src/clis/CLI-tools-ROADMAP.md` §2). The roadmap's Python/Typer/httpx/Pydantic stack does NOT apply here. CLI framework is `spf13/cobra`, HTTP is stdlib `net/http`.

## Milestones

- M1 — `search` subcommand (shipped).
- M1.5 — JSON envelope wrapper, LICENSE (this change).
- M2+ — see roadmap.

## Output contract

Stdout emits ONE JSON envelope per invocation:

```json
{"schema_version":"1","provider":"exa","command":"search","elapsed_ms":0,"result":{...}}
```

Error path: stderr + exit code (no envelope). Documented exit codes in README.
