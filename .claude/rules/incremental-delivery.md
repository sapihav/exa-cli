# Incremental Delivery Rules

## When You See a Multi-Milestone Plan

Before implementing ANY milestone:
1. Count total milestones in the plan
2. State which SINGLE milestone you will implement in THIS PR
3. Confirm the milestone is independently deployable (CLI still builds, `--help` works, existing commands still run)
4. Refuse to proceed with more than one milestone per PR

## PR Size Guardrails

- **Target:** ~300 lines of source code per PR
- **Hard max:** 500 lines of source code (in `src/` or root, excluding tests, generated code, vendored deps, and trivial init files)
- **Excluded from count:** test files, golden fixtures, schema mirrors (e.g., `OUTPUT.md`), CHANGELOG, README
- If a milestone exceeds 500 lines estimate, split into sub-milestones BEFORE starting

## After Each Milestone PR

- Verify: `<bin> --help` works, `<bin> schema` parses as JSON, tests pass, no regressions in existing commands
- Do NOT start next milestone in same session
- Tell user: "M{N} is ready for review. After merge, we can start M{N+1}"

## Anti-Patterns (lessons from past projects)

| What Happened | What Should Have Happened |
|---------------|--------------------------|
| Multiple milestones in one commit (thousands of lines) | One PR per milestone (~300-500 lines each) |
| Review after all milestones | Review after each milestone |
| Critical bugs found post-merge | Caught in per-milestone review |
| Plan said "one milestone per PR" | Orchestrator ignored the plan |
