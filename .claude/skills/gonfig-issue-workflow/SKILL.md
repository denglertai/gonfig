---
name: gonfig-issue-workflow
description: End-to-end workflow for working on GitHub issues and pull requests in the gonfig repository - from creating the linked branch with gh issue develop, through implementation, tests (unit, snapshot, fuzz equivalence), verifying documented examples against a fresh build, coverage of changed lines and benchmarks, to opening a pull request with a structured description and handling Codecov and CodeRabbit feedback. Use this skill whenever the user asks to start, continue, implement or fix a gonfig issue (e.g. "continue with #165", "start on the next issue"), to write or refine an issue description, to open or update a PR, to check or fix a Codecov or CodeRabbit report, or to clean up after a merge - even if they don't mention the workflow explicitly.
---

# gonfig issue workflow

This is how issues are taken from "open" to "merged" in gonfig. Each step exists because skipping it caused a real problem before: untested doc examples that were wrong, a commit pushed after its PR had merged, quadratic algorithms only found by benchmarking, Codecov and CodeRabbit flagging lines that could have been checked locally.

gonfig is a Go CLI that fills config files (YAML, JSON, XML, properties, plain text) with environment variables via placeholders like `${VAR | filter}`. Key packages:

| Package | Responsibility |
| --- | --- |
| `internal/value` | Finding placeholders, parsing filter chains, templates (`template.go`) |
| `internal/filter` | Built-in filters, `filterMap`, unknown filter handling |
| `internal/file` | One handler per file type, `FileProcessor` pipeline, typed values, fragments |
| `internal/config` | Viper based config (`.gonfig.yaml`, `GONFIG_*`, flags) |
| `cmd` | Cobra commands, wiring config into the packages |
| `cmd/benchmarks` | Benchmark suite (`make bench`, `make profile`) |

User-facing docs live in `README.md`, `FILTERS.md`, `PLUGINS.md` and `cmd/benchmarks/README.md`.

The helper scripts in `scripts/` are referenced as `$SKILL/scripts/...`, where `$SKILL` is `.claude/skills/gonfig-issue-workflow`.

## 1. Start from an issue

```bash
git checkout main && git pull --ff-only
gh issue view <N> --json title,body,comments,labels
gh issue develop <N> --base main --checkout
```

`gh issue develop` links the branch to the issue, which is what the user expects. Delete merged local branches with `git branch -d` along the way.

**Empty or vague issue?** Help write it before coding. Read the relevant code first so you can propose a concrete design, then ask the user only about decisions that change the outcome (use AskUserQuestion with a recommended option and previews where useful). Write the result into the issue with `gh issue edit <N> --body-file -`, fixing the title if needed. A good description has usage examples, rules, and an explicit "scope of the first version" for anything deferred. Run every example you put into an issue against the real binary first.

## 2. Understand before changing

- Read the code paths involved and record the current behavior of anything you are about to change, including edge cases. A quick table-driven dump test of inputs and outputs is enough; delete it afterwards.
- When the request is ambiguous in a way that changes the result (e.g. how types are inferred, merge semantics), ask instead of guessing. Explain the trade-off concretely, e.g. "zip code `01701` would silently become an integer".
- Check `references/pitfalls.md` for known traps in this codebase before touching the tokenizer, viper config, XML, YAML encoding or plugins.

## 3. Implement

Match the surrounding code:

- Every function gets a doc comment starting with its name (`// SetFragment replaces ...`), including test functions. CodeRabbit's docstring check counts every function a PR touches, tests included.
- Comments explain *why*, briefly. Error messages are lower case and name the failing entry or template, e.g. `failed to process the value of database.host in config.yaml: ...`.
- Keep behavior changes deliberate: if existing tests or snapshots fail, find out whether they captured a bug (update them) or real behavior (fix the code). Say which in the summary.

## 4. Verify

Run these before reporting the work as done. Report results faithfully, including anything skipped.

### Tests

```bash
gofmt -l cmd internal          # must print nothing
go vet ./...
make lint                      # golangci-lint as in CI, configured in .golangci.yml
go test ./...
go test -shuffle=on -count=3 ./cmd/ ./internal/...   # when package-level state is involved
```

Package-level state exists (allowed unknown filters, templates); tests that change it reset it with `t.Cleanup`.

Tests must not depend on the machine's `GONFIG_*` variables or config files. When adding options or tests that depend on config, run them in a polluted environment:

```bash
sh $SKILL/scripts/test_polluted.sh
```

### Snapshots

`go-snaps` snapshots live in `__snapshots__/`. For behavior that is easier to review visually, add cases to the readable snapshot helpers in `internal/file` (`runBehaviorSnapshots`, `TestFragmentSnapshots`), which render the `.gonfig.yaml`, environment, file and result together.

```bash
UPDATE_SNAPS=true go test ./internal/file/   # regenerate after an intended change, then review the .snap diff
CI=true go test -run Snapshots ./internal/file/   # CI mode only compares, never creates
```

Review every changed snapshot line and state in the PR which changes are intended. Note that `go-snaps` stores YAML's `---` as `/-/-/-/`.

### Fuzzing refactors

When rewriting parsing or placeholder handling, prove equivalence: copy the old implementation into a temporary `zz_*_test.go` file with renamed identifiers, write a fuzz test comparing old and new results, and run it for a few minutes (`go test -run '^$' -fuzz FuzzX -fuzztime 120s ./internal/value/`). Exclude inputs where a difference is intended and document why. Afterwards delete the temporary file and `testdata/fuzz/`. Crashes found this way are real bugs: fix them and add regression tests. `FuzzProcessValue` stays as a permanent panic check. Permanent fuzz targets, like the per-file-type `Fuzz<Type>` tests, only run in CI if they are listed in the matrix of `.github/workflows/fuzz.yml`, so add new ones there.

### The real binary

Tests can miss wiring problems, so run the feature end to end:

```bash
go build -o "$SCRATCH/gonfig" .
cd "$SCRATCH" && VAR=value ./gonfig -l error --plugin-path ./none config process -f example.yaml
```

Run it from a scratch directory with `--plugin-path ./none`: the repo's `./plugins` may contain a stale `.so` that crashes the binary. Use `-l error` to keep logs out of the output.

### Documented examples

Every `$ command` in the docs must produce the documented output. Rebuild first, then:

```bash
python3 $SKILL/scripts/check_docs.py FILTERS.md --bin "$SCRATCH" --config-from "## Templates" --replace /run/secrets=WORKDIR
python3 $SKILL/scripts/check_docs.py README.md --bin "$SCRATCH"
python3 $SKILL/scripts/check_links.py . README.md FILTERS.md PLUGINS.md cmd/benchmarks/README.md
sh $SKILL/scripts/render_tables.sh README.md FILTERS.md     # tables with escaped pipes must render with consistent columns
```

Examples of plugins, e.g. the secret plugins, need the built plugins and the secret manager: pass `--plugin-path <dir>` with the built plugins, and start a dev server first, e.g. `bao server -dev -dev-root-token-id=root` with `BAO_ADDR` and `BAO_TOKEN` exported. Without `--plugin-path`, no plugins are loaded.

To make an example checkable, introduce its file with a line like `` `app.yaml`: `` directly before the code block; the checker writes it before running the following commands. Expected README failures: examples with placeholder paths (`/path/to/...`, `config.template.yaml`), the repo-relative `cmd/testdata` example, and the `nginx.conf` example whose file name sits mid-sentence.

Write doc examples from real output, never from expectations. When an example fails, check whether the doc or the code is wrong before "fixing" either.

### Coverage of changed lines

Codecov comments on patch coverage. Predict it locally:

```bash
python3 $SKILL/scripts/changed_coverage.py main
```

Go measures coverage per package, so code in `internal/file` exercised only by `cmd` tests counts as uncovered. Add tests in the same package. If a block is unreachable (e.g. an error that cannot occur), remove it with a comment explaining why instead of testing the impossible.

### Doc comments of touched functions

```bash
git add -N <new files> && git diff -U0 main | go run ./$SKILL/scripts/docscan
```

### Benchmarks

When touching hot paths (placeholder processing, file handlers, merging), compare against `main`:

```bash
git worktree add "$SCRATCH/main-wt" main && cp -r cmd/benchmarks "$SCRATCH/main-wt/cmd/"   # if the suite is new
(cd "$SCRATCH/main-wt" && go test -run '^$' -bench . -benchmem -count=6 ./cmd/benchmarks > "$SCRATCH/old.txt")
go test -run '^$' -bench . -benchmem -count=6 ./cmd/benchmarks > "$SCRATCH/new.txt"
go run golang.org/x/perf/cmd/benchstat@latest "$SCRATCH/old.txt" "$SCRATCH/new.txt"
git worktree remove --force "$SCRATCH/main-wt"
```

For anything that scales with input size, measure at least three sizes (e.g. 1k, 4k, 16k). Linear means ~4× time per 4× size; ~13-16× means quadratic, which happened twice (linear key lookups during merges, etree shifting children). Run benchmarks sequentially, never in parallel, so they don't compete for CPU. Record findings in `cmd/benchmarks/README.md`.

## 5. Commit and open the pull request

Ask before committing, pushing or opening a PR unless the user already asked for it in this request. Then:

- **Check the PR state first** when pushing to an existing PR branch: `gh pr view <N> --json state`. Pushing to a merged PR's branch silently strands the commit. If it is merged, create a new branch from the updated `main`, `git cherry-pick` the commit, verify the patch is identical (`diff <(git diff A^ A) <(git diff B^ B)`), and open a follow-up PR.
- Stage files explicitly (no `git add -A` of unrelated files) and check whether a file exists before writing it, since `cat >` and Write overwrite silently.
- Use conventional commits (`feat(file):`, `fix(value):`, `test:`, `docs:`, `perf:`, `chore(deps):`) with a body explaining why, `Closes #N`, a `BREAKING CHANGE:` footer when behavior changes incompatibly, and the attribution trailer the environment specifies. Split independent changes into separate commits.
- PR description structure (see `references/pr-template.md`): Summary with a short example, Changes or Behavior, Behavior/Breaking change, Performance if relevant, Verification (what was run, with numbers), Related issues. Lead with breaking changes when there are any.

Never use `git stash` casually in this repo: the user keeps old stashes. If you must stash, pop immediately and confirm with `git stash list` that only your entry was removed.

## 6. Handle PR feedback

```bash
gh pr view <N> --json comments --jq '[.comments[] | select(.author.login=="codecov-commenter")] | last | .body'
gh api repos/denglertai/gonfig/issues/<N>/comments     # CodeRabbit summary, pre-merge checks, bot actions
gh api repos/denglertai/gonfig/pulls/<N>/comments      # inline review comments
gh run list --branch <branch> --limit 1
```

- **Codecov:** map the missing lines with `changed_coverage.py`, add tests in the same package, push, then watch CI and the updated comment in the background (`gh run watch <id> --exit-status`, then poll the comment) rather than sleeping in the foreground.
- **CodeRabbit docstring coverage:** run `docscan` to find touched functions without doc comments. CodeRabbit's own count differs slightly; covering every touched function is the reliable fix.
- **Bot-generated commits** (e.g. CodeRabbit's "Generate docstrings") land directly on the branch: `git pull --ff-only`, then review every sentence against the code and correct inaccuracies in a follow-up commit.
- When a PR's description no longer matches what was merged, correct it with `gh pr edit <N> --body-file`.

## 7. After the merge

```bash
git checkout main && git pull --ff-only
git branch -d <merged branch>
gh issue list --limit 20
```

Keep a running list of breaking and behavior changes for the release notes and remind the user of it. Suggest the next issue based on priority labels and on what recent changes made easy.

## Communicating

Keep the user informed with short updates between steps and finish each turn with a summary: what changed, how it was verified (with numbers), anything unexpected (bugs found along the way, mistakes made and fixed), and the next decision for the user. Findings outside the current scope become suggested issues rather than silent fixes; create them only when the user agrees.
