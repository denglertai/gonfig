# gonfig - working with Claude Code

Follow the `gonfig-issue-workflow` skill for every issue and PR. The main session is the **lead**: it owns the conversation with the user, the branch, commits and the PR. It delegates focused work to the subagents in `.claude/agents/` and merges their reports. Subagents can't spawn subagents and can't talk to each other, so all hand-offs go through the lead.

## Roles

| Agent | Writes | Job |
| --- | --- | --- |
| lead (main session) | branch, commits, PR, issue text | talks to the user, plans, delegates, consolidates |
| `gonfig-analyst` | nothing | current behavior, design, open decisions, needed lanes |
| `gonfig-implementer` | production code + minimal tests | implements the approved design |
| `gonfig-test-engineer` | `*_test.go`, snapshots, testdata | coverage of changed lines, snapshots, fuzz, polluted env |
| `gonfig-docs-verifier` | `*.md` | docs from real output, doc/link/table checkers |
| `gonfig-perf-analyst` | `cmd/benchmarks/` | benchstat vs main, scaling at 3 sizes |
| `gonfig-secrets-auditor` | nothing | leaks, escaping/injection, plugins, govulncheck |
| `gonfig-reviewer` | nothing | lint, docscan, conventions, commit plan, PR draft |

## Flow

1. **Analyse:** lead runs `gh issue develop <N> --base main --checkout`, then `gonfig-analyst`.
2. **Decide:** lead asks the user only the open decisions from the report (AskUserQuestion, recommended option first). Vague issue → lead writes the issue text with the user first.
3. **Implement:** `gonfig-implementer` with the report and the decisions.
4. **Verify in parallel:** `gonfig-test-engineer`, `gonfig-docs-verifier`, `gonfig-secrets-auditor` - only the lanes the analyst marked as needed. Their write scopes don't overlap.
5. **Benchmark alone:** `gonfig-perf-analyst` afterwards, if a hot path changed (never in parallel - CPU contention skews benchstat).
6. **Fix loop:** blockers go back to `gonfig-implementer` with the finding; repeat only the affected lanes.
7. **Gate:** `gonfig-reviewer` last. Lead shows the user the summary, commit plan and PR draft, and commits/pushes/opens the PR only with the user's OK (check `gh pr view <N> --json state` before pushing to an existing branch).
8. **Feedback:** Codecov → `gonfig-test-engineer`; CodeRabbit docstrings/style → `gonfig-implementer` + `gonfig-reviewer`.

Small changes (typo, one-line fix, dependency bump) skip the agents: the lead does them directly with the skill's checks.

## Rules for every agent

- Never put processed values or secrets into logs, errors, issues or PR text; use made-up secrets.
- Run the binary from a scratch dir with `--plugin-path ./none -l error` unless plugins are under test.
- No `git stash`, no `git add -A`, no commits or pushes - only the lead commits.
- Report results faithfully, including what was skipped.
