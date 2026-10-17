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

- Only an element's **leading text** is processed (`Text()`); placeholders after child elements are not (#220). The root element's own text is not processed (#212).
- `RemoveChild`/`AddChild` shift and re-index all following children, so moving many children from the front is quadratic. Remove from the end.

## JSON

- Values keep the type of the original value unless a typed filter result replaces them; placeholders are always inside strings.
- Keys containing `${` have entries (renamed via `SetValue`), other keys don't.

## Plugins

- A stale `.so` in `./plugins` of the working directory can crash the binary on startup (`runtime: no plugin module data`). Run manual tests from a scratch directory with `--plugin-path ./none`.
- Plugins must match the Go version and `-trimpath` of the binary; `go version -m` shows both.

## go-snaps

- YAML document separators `---` are stored as `/-/-/-/` in snapshot files.
- With `CI=true`, missing snapshots fail instead of being created, so commit new `.snap` files.

## Tests and tooling

- Go coverage is per package; `cmd` tests don't count for `internal/*`.
- The JSON writer of `encoding/json` escapes `<` and `>` as `<`/`>`; that's valid JSON, not a bug.
- `gonfig value` prints without a trailing newline.
