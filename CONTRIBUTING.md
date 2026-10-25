# Contributing

Thanks for your interest in gonfig! Bug reports, ideas and pull requests are welcome.
By participating, you agree to follow the [code of conduct](./CODE_OF_CONDUCT.md).

- [Issues](#issues)
- [Development setup](#development-setup)
- [Making changes](#making-changes)
- [Tests](#tests)
- [Documentation](#documentation)
- [Pull requests](#pull-requests)

## Issues

- **Bugs:** open a [bug report](https://github.com/denglertai/gonfig/issues/new?template=bug_report.yml) with the version (`gonfig --version`), the command, a minimal file and the environment variables needed to reproduce it. Use made-up secrets.
- **Features:** open a [feature request](https://github.com/denglertai/gonfig/issues/new?template=feature_request.yml) describing the use case, ideally with the placeholder or command you have in mind.
- **Vulnerabilities:** don't open an issue, report them privately as described in the [security policy](./SECURITY.md).

For larger changes, please open an issue first, so the design can be discussed before you spend time on it.

## Development setup

gonfig is written in Go. The Go version is set by the `toolchain` line of [go.mod](./go.mod), which the `go` command downloads automatically.

```bash
git clone https://github.com/denglertai/gonfig.git
cd gonfig
go build -o gonfig .
./gonfig --version
```

The repository consists of several Go modules:

| Directory | Content |
| --- | --- |
| `.` | The CLI: `cmd` for the commands, `internal/value` for placeholders and templates, `internal/filter` for the built-in filters, `internal/file` for the file type handlers, `internal/config` for the configuration |
| `pkg/plugin` | The protocol and helpers for writing plugins, see [PLUGINS.md](./PLUGINS.md) |
| `plugins/openbao`, `plugins/azure-keyvault` | The plugins for secret managers, each a module of its own |
| `cmd/benchmarks` | Benchmarks, see [cmd/benchmarks/README.md](./cmd/benchmarks/README.md) |

When running a self-built binary, pass `--plugin-path ./none` unless you test plugins, so no plugins of the system or of `./plugins` are loaded.

## Making changes

- Keep changes focused: one issue per pull request, and independent changes in separate commits.
- Match the surrounding code. Every function, including test functions, has a doc comment starting with its name, and comments explain why rather than what.
- Error messages are lower case and name what failed, e.g. the entry, file, filter or template.
- Never include processed values in logs or error messages unless `--log-values` is set, since they may contain secrets.
- Behavior changes are deliberate: if existing tests or snapshots fail, find out whether they captured a bug or intended behavior, and say which in the pull request.

## Tests

Run the same checks as CI before opening a pull request:

```bash
gofmt -l cmd internal pkg plugins   # must print nothing
go vet ./...
make lint                           # golangci-lint with the version and linters used in CI
make test                           # the root module and the plugin modules
```

- **Snapshots:** some tests compare their output with [go-snaps](https://github.com/gkampitakis/go-snaps) snapshots in `__snapshots__` directories. After an intended change, regenerate them with `UPDATE_SNAPS=true go test ./...` and review the diff of the `.snap` files.
- **Fuzzing:** the file handlers and placeholder processing have fuzz tests, e.g. `go test -run '^$' -fuzz FuzzYAML -fuzztime 60s ./internal/file/`. Inputs that fail are written to `testdata/fuzz` and should be committed as regression tests together with the fix.
- **Benchmarks:** for changes to placeholder processing or the file handlers, compare `make bench` before and after the change, see [cmd/benchmarks/README.md](./cmd/benchmarks/README.md).
- **OpenBao plugin:** the integration tests run against a dev server, e.g. `bao server -dev -dev-root-token-id=root`, with `TEST_OPENBAO_ADDR=http://127.0.0.1:8200 TEST_OPENBAO_TOKEN=root go test -run Integration ./...` in `plugins/openbao`.

Tests must not depend on the `GONFIG_*` variables or config files of the machine running them. The packages reading config isolate themselves in `TestMain`, so new test packages reading config need the same.

## Documentation

User-facing behavior is documented in [README.md](./README.md), [FILTERS.md](./FILTERS.md) and [PLUGINS.md](./PLUGINS.md). Update them together with the code.

Write examples from the real output of a fresh build, not from expectations. The scripts in [.claude/skills/gonfig-issue-workflow/scripts](./.claude/skills/gonfig-issue-workflow/scripts/) check them:

```bash
go build -o /tmp/gonfig-bin/gonfig .
python3 .claude/skills/gonfig-issue-workflow/scripts/check_docs.py README.md --bin /tmp/gonfig-bin
python3 .claude/skills/gonfig-issue-workflow/scripts/check_links.py . README.md FILTERS.md PLUGINS.md
```

`check_docs.py` runs every `$ command` of a document and compares its output. A line like `` `app.yaml`: `` directly before a code block writes that file first.

## Pull requests

- Use [conventional commits](https://www.conventionalcommits.org/), e.g. `feat(filter): add one_of`, `fix(file): keep the BOM of input files`, `docs: …`, `test: …`, `chore(deps): …`. The body explains why, and `Closes #N` links the issue. Incompatible changes get a `BREAKING CHANGE:` footer.
- Fill in the [pull request template](./.github/pull_request_template.md): a summary with a short example, the behavior and breaking changes, and how you verified the change.
- CI runs the tests on Linux, macOS and Windows, the race detector, the linters, fuzzing and security scans. [Codecov](https://codecov.io/gh/denglertai/gonfig) comments on the coverage of the changed lines, and [CodeRabbit](https://coderabbit.ai) reviews every pull request. Please answer or address their comments.
- Pull requests are merged by the maintainer, and releases are created as described in the [README](./README.md#releasing).
