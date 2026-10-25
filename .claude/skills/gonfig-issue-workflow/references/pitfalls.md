# Known pitfalls in gonfig

Traps found while working on this codebase. Check the relevant section before changing these areas.

## Tokenizer (github.com/bzick/tokenizer)

- **Token key 0 is reserved** for undefined tokens, and `DefineTokens` silently ignores keys below 1. The filter separator once used key 0 and was never registered. Token constants therefore start at `iota + 1`.
- **Framed strings end at the first unescaped closing bracket**, so brackets cannot be nested. Params use `\` to escape `,` `=` `(` `)` `{` `}` `\`.
- **NUL bytes** end the input for the tokenizer. Exclude them from fuzz equivalence tests.
- Placeholders themselves are found without the tokenizer (`findPlaceholders`), only their content is tokenized.

## Filter params

- `,` separates params, so `split(,)` passes two empty params. Empty delimiters of `split` and `join` fall back to the default comma.
- Positional params are stored by position (`"0"`, `"1"`), `param(params, name, position)` reads either form.

## Config (viper)

- **Keys are case-insensitive** and converted to lower case, including template names.
- `ReadInConfig` errors must only be ignored for `viper.ConfigFileNotFoundError`; ignoring all errors silently dropped invalid config files.
- **The flag default only matters for `--help`** when `SetDefault` exists, so tests check flag defaults separately.
- Nested maps (templates) can only come from `.gonfig.yaml`, not from flags or environment variables.

## YAML (go.yaml.in/yaml/v3)

- `@` cannot start a plain YAML value: template references like `filters: "@clean"` must be quoted.
- Unquoted `${...}` within flow collections (`[...]`, `{...}`) is invalid YAML.
- The encoder's event queue grows with the document; it allocates ~60% of the memory for large YAML files and cannot be fixed without changing the library.
- A line comment on `key: value` belongs to the value node, but to the key node when the value is a block mapping or sequence.
- Resetting quote and flow styles is safe: the encoder re-adds quotes where a string would read as another type (`"123"`, `"true"`).

## XML (github.com/beevik/etree)

- Each **text node** (`etree.CharData`) of an element is an entry, including the root element's text, text after child elements and CDATA sections, which keep their CDATA flag when their value is set. Text nodes consisting of whitespace only have no entries. Don't use `Element.Text()`/`SetText()` for values: they only cover the leading text and replace or merge text nodes, which used to lose text around CDATA sections.
- `RemoveChild`/`AddChild` shift and re-index all following children, so moving many children from the front is quadratic. Remove from the end.

## JSON

- Values keep the type of the original value unless a typed filter result replaces them; placeholders are always inside strings.
- Keys containing `${` have entries (renamed via `SetValue`), other keys don't.

## TOML (github.com/pelletier/go-toml/v2)

- The handler edits files in place using the positions of the `unstable` parser, which doesn't check semantics (duplicate keys, redefined tables), so documents are also unmarshalled once to validate them.
- **Nodes are only valid until the next `NextExpression`**, which reuses the parser's node storage: walk an expression completely before parsing the next one, or `Children()` panics.
- **Arrays and inline tables have no `Raw` range** (empty, or just `{`). The range of the containing `KeyValue` expression is complete.
- go-toml accepts TOML 1.1, e.g. line breaks within inline tables, which TOML 1.0 parsers like Python's `tomllib` reject.
- A BOM is rejected by go-toml, so the handler strips and restores it.

## Plugins

- A stale `.so` in `./plugins` of the working directory can crash the binary on startup (`runtime: no plugin module data`). Run manual tests from a scratch directory with `--plugin-path ./none`.
- Plugins must match the Go version and `-trimpath` of the binary; `go version -m` shows both.

## go-snaps

- YAML document separators `---` are stored as `/-/-/-/` in snapshot files.
- With `CI=true`, missing snapshots fail instead of being created, so commit new `.snap` files.

## Tests and tooling

- The tests of `cmd`, `internal/config` and `internal/file` call `testutil.IsolateFromEnvironment` in `TestMain`: all `GONFIG_*` variables are removed and `HOME` and `GONFIG_CONFIG_PATH` point to an empty directory. New options therefore need no changes to the tests' isolation, but new test packages reading config need the same `TestMain`. Tests checking the default config search clear `GONFIG_CONFIG_PATH` themselves.
- Run `scripts/test_polluted.sh` after adding options or tests that depend on config: it runs all tests with every option enabled through the environment, a home config file and a config path, which caught several tests relying on a clean environment.

- Go coverage is per package; `cmd` tests don't count for `internal/*`.
- The JSON writer of `encoding/json` escapes `<` and `>` as `<`/`>`; that's valid JSON, not a bug.
- `gonfig value` prints without a trailing newline.
