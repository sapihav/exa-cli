---
globs:
  - "docs/backlog/**"
---

# Backlog Management Rules

## Structure
- `docs/backlog/_index.md` — prioritized task list (source of truth for ordering)
- `docs/backlog/tasks/` — actionable work items with clear acceptance criteria
- `docs/backlog/bugs/` — bug reports with evidence
- `docs/backlog/ideas/` — unprioritized ideas and future possibilities

## File Naming
- Use kebab-case slugs: `add-people-search-command.md`, `fix-pagination-cursor.md`
- No issue IDs in filenames

## Frontmatter
Every backlog item MUST have:
```yaml
---
title: Short descriptive title
type: task | bug | idea
priority: P1 | P2 | P3 | P4
status: todo | in-progress | done
created: YYYY-MM-DD
---
```

## Item Body Structure
- **tasks/**: Problem Statement, Acceptance Criteria, Context/Notes
- **bugs/**: Bug Description (Observed/Expected), Steps to Reproduce, Evidence (logs, exit code, dry-run output)
- **ideas/**: Idea Description, Motivation, Open Questions

## Workflow
- Before starting work: read the relevant backlog item for full context
- When starting: update status to `in-progress` in the item's frontmatter
- When done: update status to `done` in frontmatter, remove from `_index.md`
- When creating new work: add file + update `_index.md`
- Branch naming: `feat/slug`, `fix/slug`, `chore/slug`, `docs/slug` (use the backlog item's filename without .md)

## _index.md Maintenance
- Keep items in priority order within each section (Active / Up Next / Backlog / Ideas)
- Link to the markdown file for each item
- Completed items are NOT listed — only tracked via `status: done` in their frontmatter
