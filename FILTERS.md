# Filters

Filters transform the values that gonfig reads from environment variables before they are written into a config file.
This page documents the placeholder syntax, every built-in filter and some common recipes.

- [Placeholder syntax](#placeholder-syntax)
- [Order of operations](#order-of-operations)
- [Filter params](#filter-params)
- [Built-in filters](#built-in-filters)
- [Lists](#lists)
- [Types](#types)
- [Templates](#templates)
- [Fragments](#fragments)
- [Recipes](#recipes)
- [Caveats](#caveats)

All examples use `gonfig value`, which processes a single string and prints the result to stdout.
The same placeholders work in every config file processed by `gonfig config process`.

## Placeholder syntax

A placeholder starts with `${`, contains the name of an environment variable, optionally followed by filters separated by pipes `|`, and ends with `}`:

```console
${ENV_VAR}
${ENV_VAR | filter1 | filter2(param) | filter3}
```

Whitespace around the variable name, the pipes and the filter names is ignored.
A value may contain any number of placeholders, mixed with plain text:

```console
$ DB_USER=app DB_PASSWORD=s3cret DB_HOST=db gonfig value 'postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:5432/app'
postgres://app:s3cret@db:5432/app
```

Note: Use single quotes in the shell, otherwise the shell expands `${...}` itself before gonfig sees it.

A placeholder ends at the first `}` that is not escaped. A `${` without a closing `}` is an error, e.g. for a typo, so it is not written to the output as is:

```console
$ V=abc gonfig value '${V | upper'
Error: unterminated placeholder "${V | upper", the closing } is missing
```

To write a literal `${`, double the dollar sign: `$${` is written as `${` without starting a placeholder, like in docker compose.
Each `$$` directly before a `{` stands for one `$`, so `$$${NAME}` is a `$` followed by the value.
Dollar signs that are not followed by a `{` are kept as they are:

```console
$ NAME=gonfig gonfig value '$${NAME} is ${NAME}, $$${NAME} costs $$5'
${NAME} is gonfig, $gonfig costs $$5
```

The escape covers the `${` only, so placeholders after it are processed as usual, e.g. to fill in the default of a shell variable. Escape them as well to keep them:

```console
$ FALLBACK=staging gonfig value '$${TARGET:-${FALLBACK}} $${TARGET:-$${FALLBACK}}'
${TARGET:-staging} ${TARGET:-${FALLBACK}}
```

## Order of operations

Every placeholder is resolved in the following steps:

1. **Environment variable lookup:** The value of the environment variable is read. A variable that is not set results in an empty string, unless [`fail-on-missing`](#missing-variables) is enabled.
1. **File inclusion:** If the value starts with `@`, the rest is treated as a file path and the value is replaced by the file's content.
   Paths that are absolute or start with `./` or `../`, e.g. `@/run/secrets/db_password`, have to refer to a readable file, otherwise processing fails, e.g. if a secret is not mounted.
   On Windows, this includes paths like `C:\secrets\db`, `\\server\share\db` and `\secrets\db`, and `.\` and `..\` as well.
   Other values starting with `@`, e.g. `@x9!kQ` or `@secret`, are only read from a file if it exists, and kept as they are (including the `@`) otherwise.
1. **Filters:** The filters are applied from left to right, each one receiving the output of the previous one.
1. **Replacement:** The placeholder is replaced by the result.

The order of filters matters:

```console
$ V=' abc ' gonfig value '[${V | trim | base64}]'
[YWJj]
$ V=' abc ' gonfig value '[${V | base64 | trim}]'
[IGFiYyA=]
```

If a filter fails, for example `to_int` on a non-numeric value, gonfig stops with an error and does not write any output file.

A filter name that is unknown, for example because of a typo or a [plugin](./PLUGINS.md) that failed to load, is an error as well.
This prevents writing a value unchanged that was meant to be hashed or encrypted. Filter names are case-sensitive, so `UPPER` is unknown:

```console
$ V=abc gonfig value '${V | UPPER}'
Error: unknown filter "UPPER": check the filter name, or the log for plugins that failed to load
```

The option `allow-unknown-filters` restores the behavior of earlier versions, which logged a warning and passed the value on unchanged.
It may be set with the flag `--allow-unknown-filters`, the environment variable `GONFIG_ALLOW_UNKNOWN_FILTERS` or in `.gonfig.yaml`, see [configuration](./README.md#configuration).
Only use it if values passed on unchanged cannot contain secrets:

```console
$ V=abc gonfig --allow-unknown-filters value '${V | UPPER}'
abc
```

## Filter params

Some filters accept params in brackets, either by position or by name:

```console
$ V=foo gonfig value '${V | replace(foo,bar)}'
bar
$ V=foo gonfig value '${V | replace(old=foo,new=bar)}'
bar
```

Params are separated by `,` and named params use `key=value`.
The characters `,`, `=`, `(`, `)`, `{`, `}` and `\` have to be escaped with a backslash when they are part of a param:

```console
$ V='a,b' gonfig value '${V | replace(\,,;)}'
a;b
```

Params without a closing `)` are an error, like placeholders without a closing `}`, and so are params without a filter name in front of them, e.g. `${V | (foo)}`.
Params cannot contain placeholders, since the placeholder ends at the first unescaped `}`:

```console
$ V=foo gonfig value '${V | replace(foo,bar}'
Error: unterminated params "(foo,bar" of replace, the closing ) is missing
$ V=foo gonfig value '${V | default(${OTHER})}'
Error: unterminated params "(${OTHER" of default, the closing ) is missing; params cannot contain placeholders, escape { and } with a backslash
```

Params a filter does not accept are an error, e.g. a typo in a param name or params of a filter without any, so they are not silently ignored.
The params of each filter are listed in the [table below](#built-in-filters):

```console
$ V=foo gonfig value '${V | replace(old=foo,nwe=bar)}'
Error: filter replace has no param "nwe", it accepts old, new
$ V=foo gonfig value '${V | trim(both)}'
Error: filter trim accepts no params, but got 1
```

Empty positional params beyond the accepted ones are allowed, e.g. `split(,)`, see [split / join](#split--join).
Filters of [plugins](./PLUGINS.md) receive any params and may check them themselves.

This matters most for regular expressions, whose groups and quantifiers use brackets:

```console
$ API_URL=https://api.example.com/v1 gonfig value '${API_URL | regex(^https?://\([^/]+\).*,\1)}'
api.example.com
```

## Built-in filters

| Filter | Params | Description |
| --- | --- | --- |
| [`upper`](#upper--lower) | | Converts the value to upper case |
| [`lower`](#upper--lower) | | Converts the value to lower case |
| [`trim`](#trim--trimleft--trimright) | | Removes leading and trailing whitespace |
| [`trimleft`](#trim--trimleft--trimright) | | Removes leading spaces |
| [`trimright`](#trim--trimleft--trimright) | | Removes trailing spaces |
| [`default`](#default--required) | `value` | Replaces an empty value, e.g. of a variable that is not set |
| [`required`](#default--required) | | Fails if the value is empty |
| [`one_of`](#one_of) | `values` | Fails unless the value is one of the given values |
| [`to_int`](#to_int--to_float--to_bool) | | Converts the value to an integer |
| [`to_float`](#to_int--to_float--to_bool) | | Converts the value to a floating point number |
| [`to_bool`](#to_int--to_float--to_bool) | | Converts the value to a boolean |
| [`multiply`](#multiply) | `m` | Multiplies a number |
| [`md5`](#md5) | | Hashes the value with MD5 |
| [`sha256`](#sha256--sha512) | | Hashes the value with SHA-256 |
| [`sha512`](#sha256--sha512) | | Hashes the value with SHA-512 |
| [`bcrypt`](#bcrypt) | `cost` | Hashes the value with bcrypt |
| [`base64`](#base64--base64_decode) | | Encodes the value to base64 |
| [`base64_decode`](#base64--base64_decode) | | Decodes a base64 value |
| [`hex`](#hex--hex_decode) | | Encodes the value to hex |
| [`hex_decode`](#hex--hex_decode) | | Decodes a hex value |
| [`url_encode`](#url_encode--url_decode) | `mode` | Escapes the value for use within URLs |
| [`url_decode`](#url_encode--url_decode) | `mode` | Reverses `url_encode` |
| [`fragment`](#fragments) | | Inserts the value as a structure, e.g. a YAML mapping, XML elements or a [list](#lists-as-fragments) |
| [`replace`](#replace) | `old`, `new` | Replaces text |
| [`regex`](#regex) | `pattern`, `replacement` | Replaces text using a regular expression |
| [`split`](#split--join) | `delimiter` | Splits the value into a [list](#lists) |
| [`join`](#split--join) | `delimiter` | Joins a [list](#lists) into a single value |

Additional filters may be added by [plugins](./PLUGINS.md).

### upper / lower

Convert the value to upper or lower case.

```console
$ ENV=Production gonfig value '${ENV | upper} ${ENV | lower}'
PRODUCTION production
```

### trim / trimleft / trimright

`trim` removes leading and trailing whitespace, including tabs and newlines.
`trimleft` and `trimright` only remove spaces from the respective side.

```console
$ V='  abc  ' gonfig value '[${V | trim}] [${V | trimleft}] [${V | trimright}]'
[abc] [abc  ] [  abc]
```

### default / required

`default` replaces an empty value by its param, e.g. for a variable that is not set. Other values are kept.
`required` fails if the value is empty, both for variables that are not set and for variables set to an empty value.

```console
$ gonfig value 'port=${DB_PORT | default(5432)}'
port=5432
$ DB_PORT=6543 gonfig value 'port=${DB_PORT | default(5432)}'
port=6543
$ DB_PASSWORD= gonfig value '${DB_PASSWORD | required}'
Error: filter required: the value is empty or the environment variable is not set
```

A default containing a comma has to be [escaped](#filter-params), e.g. `default(a\,b)`.

### one_of

Fails unless the value is one of the values of the param `values`, separated by `|`, e.g. to validate log levels or
environment names. The value is passed on unchanged. The comparison is case-sensitive, so combine it with `lower` if needed.

```console
$ LOG_LEVEL=info gonfig value '${LOG_LEVEL | default(info) | one_of(debug|info|warn|error)}'
info
$ LOG_LEVEL=verbose gonfig value '${LOG_LEVEL | default(info) | one_of(debug|info|warn|error)}'
Error: filter one_of: the value is not one of debug, info, warn, error
```

An empty entry allows empty values, e.g. `one_of(|debug|info)`. Each element of a [list](#lists) has to be one of the values.
The error includes the value only with [`log-values`](./README.md#options).

### Missing variables

By default, a variable that is not set is replaced by an empty string, so a typo like `${DB_PASWORD}` goes unnoticed.
The option `fail-on-missing` makes such placeholders fail instead. It may be set with the flag `--fail-on-missing`, the environment variable `GONFIG_FAIL_ON_MISSING` or in `.gonfig.yaml`, see [configuration](./README.md#configuration):

```console
$ gonfig value 'password=${DB_PASWORD}'
password=
$ gonfig --fail-on-missing value 'password=${DB_PASWORD}'
Error: environment variable "DB_PASWORD" is not set
```

- **Only variables that are not set fail.** A variable set to an empty value, e.g. `DB_PASSWORD=`, is deliberate and allowed. Use `required` to reject empty values as well.
- **Defaults satisfy the check.** A placeholder providing a default never fails for a missing variable: a `default` filter in the same chain, also within a [filter template](#templates), or the `default` of a [placeholder template](#templates).

```console
$ gonfig --fail-on-missing value 'port=${DB_PORT | default(5432)}'
port=5432
```

To find missing variables before processing a file, use [`gonfig config check`](./README.md#check), which reports all placeholders and their variables without writing anything.

### to_int / to_float / to_bool

Convert the value to an integer, a floating point number or a boolean, and fail if it cannot be converted.
YAML and JSON files write the result as a number or boolean, see [types](#types).
Error messages don't contain the value, since it may be a secret, unless `log-values` is enabled, see [configuration](./README.md#configuration).

- `to_int` accepts integers, e.g. `8080` or `-1`.
- `to_float` accepts finite numbers, e.g. `1.5`, `2` or `1e3`.
- `to_bool` accepts `true`, `false`, `1`, `0`, `t`, `f` and their upper case variants, but not `yes` or `on`.

```console
$ PORT=8080 gonfig value '${PORT | to_int}'
8080
$ RATIO=0.75 gonfig value '${RATIO | to_float}'
0.75
$ ENABLED=1 gonfig value '${ENABLED | to_bool}'
true
$ PORT=eighty gonfig value '${PORT | to_int}'
Error: filter to_int: the value is not an integer
```

### multiply

Multiplies a number by the param `m`, which defaults to `1` and may be given by name or by position.
The value and `m` may be integers or floats, and the value may be a numeric string.

The result is an integer if the value and `m` are integers, and a float otherwise, which file types supporting types
write as such, see [types](#types). Floats are written in their shortest exact form, so `3 * 0.1` gives `0.30000000000000004`.

```console
$ WORKERS=4 gonfig value '${WORKERS | to_int | multiply(m=2)}'
8
$ WORKERS=4 gonfig value '${WORKERS | to_int | multiply(3)}'
12
$ MEM_MB=1024 gonfig value '${MEM_MB | multiply(0.75)}'
768
$ TIMEOUT=1.5 gonfig value '${TIMEOUT | multiply(1000)}'
1500
$ WORKERS=4 gonfig value '${WORKERS | multiply(two)}'
Error: filter multiply: invalid param m "two", expected a number
```

Results that don't fit into an integer fail instead of wrapping around.

### md5

Hashes the value with MD5 and returns the hex encoded digest.
MD5 is not suitable for passwords, use [bcrypt](#bcrypt) instead.

```console
$ V=hello gonfig value '${V | md5}'
5d41402abc4b2a76b9719d911017c592
```

### sha256 / sha512

Hash the value with SHA-256 or SHA-512 and return the hex encoded digest, e.g. for checksums or fingerprints.
Like MD5, they are fast, so use [bcrypt](#bcrypt) for passwords.

```console
$ TOKEN=s3cret gonfig value '${TOKEN | sha256}'
1ec1c26b50d5d3c58d9583181af8076655fe00756bf7285940ba3670f99fcba0
$ TOKEN=s3cret gonfig value '${TOKEN | sha512}'
95c89addde506357ec5efd0ee41ac241efd6fb1009a7680c1501ea8170342b8cbf0f2d938b56295490f19f1fc5fa28d09b1584eaa09c9a6b2f777623299cd521
```

### bcrypt

Hashes the value with bcrypt. The salt is random, so the result differs on every run.
Values longer than 72 bytes cannot be hashed and cause an error.

The param `cost` sets the cost, given by name or by position, from 4 to 31. It defaults to 10. Each step doubles the time hashing and checking a password takes, so a higher cost makes hashes stronger against brute force, but slows down every login. The cost is part of the hash, e.g. `$2a$12$` for 12.

```console
$ ADMIN_PASSWORD=s3cret gonfig value '${ADMIN_PASSWORD | bcrypt}'
$2a$10$3nh7.N18au9F.xLeAg3Hs.7sPqH.aHmJE7eBoMMv8gEDIQHslpm3i
$ ADMIN_PASSWORD=s3cret gonfig value '${ADMIN_PASSWORD | bcrypt(cost=12)}'
$2a$12$8GU85KD04YphWSZl7KUjW.peGO5.UoUfO7v.7JU.Wsig57BTQGlZK
$ ADMIN_PASSWORD=s3cret gonfig value '${ADMIN_PASSWORD | bcrypt(3)}'
Error: filter bcrypt: the cost "3" is not an integer from 4 to 31
```

### base64 / base64_decode

Encode the value to standard base64 (with padding) or decode it again.
`base64_decode` fails if the value is not valid base64.

```console
$ V=s3cret gonfig value '${V | base64}'
czNjcmV0
$ V=czNjcmV0 gonfig value '${V | base64_decode}'
s3cret
```

### hex / hex_decode

Encode the value to lower case hex or decode it again, e.g. for keys or IDs expected in hex.
`hex_decode` accepts upper and lower case and fails if the value is not valid hex.

```console
$ KEY=s3cret gonfig value '${KEY | hex}'
733363726574
$ KEY=733363726574 gonfig value '${KEY | hex_decode}'
s3cret
$ KEY=xyz gonfig value '${KEY | hex_decode}'
Error: filter hex_decode: the value is not valid hex
```

### url_encode / url_decode

Escape the value for use within a URL or decode it again, e.g. for passwords containing `@`, `/` or `&`.
The param `mode` selects the part of the URL:

- `query` (default) escapes the value for query parameters like `?password=...`, encoding spaces as `+`.
- `path` escapes the value for a path segment like `/files/...`, encoding spaces as `%20` and keeping characters allowed in paths, like `@` and `:`.

`url_decode` fails if the value contains invalid escape sequences.

```console
$ DB_PASSWORD='p@ss/w rd&' gonfig value '?password=${DB_PASSWORD | url_encode}'
?password=p%40ss%2Fw+rd%26
$ FILE='my report.pdf' gonfig value '/files/${FILE | url_encode(path)}'
/files/my%20report.pdf
$ V='p%40ss+word' gonfig value '${V | url_decode}'
p@ss word
```

### replace

Replaces every occurrence of `old` by `new`. Both params are required, `new` may be empty.

```console
$ ENV=production gonfig value 'app-${ENV | replace(production,prod)}'
app-prod
$ V=a-b-c gonfig value '${V | replace(-,)}'
abc
```

### regex

Replaces every match of the [regular expression](https://pkg.go.dev/regexp/syntax) `pattern` by `replacement`.
Both params are required, `replacement` may be empty.
Capture groups are referenced by `\1`, `\2` and so on. Go's `${1}` syntax works as well but has to be escaped as `$\{1\}`.

```console
$ EMAIL=jane@example.com gonfig value '${EMAIL | regex(^\(\w+\)@.+,\1)}'
jane
$ VERSION=v1.2.3 gonfig value '${VERSION | regex(^v,)}'
1.2.3
$ V=aaaa gonfig value '${V | regex(a\{2\},b)}'
bb
```

Remember to [escape](#filter-params) `(`, `)`, `{`, `}` and `,` within the pattern.
To match one of these characters literally, escape both the backslash and the character, e.g. `\\\(` matches `(`:

```console
$ V='f(x)' gonfig value '${V | regex(\\\(x\\\),[y])}'
f[y]
```

### split / join

`split` turns the value into a [list](#lists) and `join` turns a list into a single value again.
The delimiter defaults to `,` for both. Since `,` separates params, `split(,)` passes two empty params, which use the default `,` as well.
An empty value becomes an empty list.

```console
$ HOSTS='db1 , db2,db3' gonfig value '${HOSTS | split | trim | join(\, )}'
db1, db2, db3
$ PATHS='/a:/b' gonfig value '${PATHS | split(:) | join(;)}'
/a;/b
```

## Lists

`split` produces a list instead of a single value. The following filters are applied to each element of a list:
`upper`, `lower`, `trim`, `trimleft`, `trimright`, `sha256`, `sha512`, `base64`, `base64_decode`, `hex`, `hex_decode`,
`url_encode`, `url_decode`, `replace` and `regex`. `one_of` checks each element and passes the list on unchanged.

```console
$ LIST='a, b ,c' gonfig value '${LIST | split | trim | upper | join(;)}'
A;B;C
```

Always `join` a list before the placeholder ends, or insert it as a list into YAML and JSON files with [`fragment`](#lists-as-fragments).
A list that is neither joined nor inserted as a fragment is written in Go's list format, e.g. `[a b c]`.

## Types

Placeholders are replaced by text, so processed values are strings by default, even if they look like numbers.
This keeps values like `01701` or `123456` unchanged, e.g. for zip codes or passwords.

`to_int`, `to_float`, `to_bool` and `multiply` return a number or boolean instead.
If a placeholder with such a result makes up the whole value, file types supporting types write it as such:

| File type | `${PORT \| to_int}` | `${ENABLED \| to_bool}` | `${ZIP}` |
| --- | --- | --- | --- |
| YAML | `8080` | `true` | `"01701"` |
| JSON | `8080` | `true` | `"01701"` |
| TOML | `8080` | `true` | `"01701"` |
| XML, properties, plain text | `8080` as text | `true` as text | `01701` as text |

In YAML, this also applies to quoted placeholders like `"${PORT | to_int}"`, while keys of mappings are always text.
In JSON and TOML, placeholders are always written within strings, e.g. `"port": "${PORT | to_int}"` becomes `"port": 8080`, and `port = "${PORT | to_int}"` becomes `port = 8080`.

If such a placeholder is surrounded by other text, its result is formatted as text:

```console
$ PORT=8080 gonfig value 'http://localhost:${PORT | to_int}/api'
http://localhost:8080/api
```

## Templates

Templates make filter chains and placeholders reusable. They are defined in the [config file](./README.md#config-file) `.gonfig.yaml`
and referenced with a leading `@`, so they never clash with filters:

- **Filter templates** are reusable filter chains, used as `${VAR | @name}`.
- **Placeholder templates** are reusable placeholders, used as `${@name}`. They read either an environment variable with an optional default (`env`)
  or a value containing placeholders (`value`), e.g. to build URLs from several variables.

```yaml
# .gonfig.yaml
templates:
  filters:
    secret:
      description: Trims a secret and encodes it to base64
      filters: trim | base64

  placeholders:
    db_host:
      description: Host of the database
      env: DB_HOST
      default: localhost
    db_port:
      env: DB_PORT
      default: "5432"
    db_name:
      env: DB_NAME
      default: app
      filters: trim | lower
    db_url:
      description: JDBC URL of the database
      value: "jdbc:postgresql://${@db_host}:${@db_port}/${@db_name}"
```

```console
$ DB_PASSWORD=' s3cret ' gonfig value '${DB_PASSWORD | @secret}'
czNjcmV0
$ gonfig value '${@db_url}'
jdbc:postgresql://localhost:5432/app
$ DB_HOST=db.internal DB_NAME=' Orders ' gonfig value '${@db_url}?ssl=true'
jdbc:postgresql://db.internal:5432/orders?ssl=true
$ gonfig value '${@db_port | to_int}'
5432
```

### Filter templates

| Field | Required | Description |
| --- | --- | --- |
| `filters` | yes | The filter chain in the same syntax as in placeholders, e.g. `trim \| replace(a,b)`. It may use other filter templates, e.g. `"@clean \| @encode"`. |
| `description` | no | Documents the template |

A filter template is applied like a single filter, so further filters may follow, e.g. `${VAR | @secret | lower}`.

### Placeholder templates

| Field | Required | Description |
| --- | --- | --- |
| `env` | either `env` or `value` | The environment variable to read |
| `default` | no, only with `env` | The value used if the environment variable is not set or empty. Like any value, it may refer to a file with `@path`. |
| `value` | either `env` or `value` | A value containing placeholders, which is processed like any config value. It may use other placeholder templates. |
| `filters` | no | The filter chain applied to the value of the environment variable, the default or the processed value. It may use filter templates. |
| `description` | no | Documents the template |

Filters following a placeholder template are applied to its result, e.g. `${@db_port | to_int}`.
Placeholder templates may be embedded into other text like any placeholder, and a typed result is written as a number or boolean if the template makes up the whole value, see [types](#types).

### Rules

- **Names** consist of lower case letters, digits and underscores, and are unique across filter and placeholder templates.
  Since the config file is read case-insensitively, names are converted to lower case.
- **Validation:** All templates are checked when gonfig starts. Unknown filters, unknown or misused references, cycles and invalid definitions
  fail before any file is processed, with an error naming the template. Unknown keys within `templates`, e.g. `filter:`
  instead of `filters:`, fail as well, naming the key like `templates.filters.secret.filter` and the expected keys.
- **References within config files** to templates that are not defined fail processing, even with `allow-unknown-filters`.
- **Chaining:** Filter templates can be chained with each other and with filters, in placeholders as well as in the `filters` of templates, e.g. `${VAR | @clean | @encode | upper}`.
- **Quoting:** Quote values in `.gonfig.yaml` that start with a template reference, e.g. `filters: "@clean | upper"`, since `@` cannot start an unquoted YAML value.
  Quote values containing placeholders as well. gonfig does not process its own config file, so placeholders there are only replaced when a template is used.
  A config file that cannot be read, e.g. because of invalid YAML, fails with an error naming the file.

## Fragments

The filter `fragment` inserts whole structures (fragments) into a config file instead of text, e.g. a list of YAML mappings or XML elements.
A fragment is read like any value, from an environment variable or from a file referenced with `@path`.
The file it is inserted into parses it in its own format, so YAML files take YAML fragments, JSON files take JSON fragments and XML files take XML fragments.
JSON is valid YAML as well, so JSON fragments work in YAML files too.

The position of the placeholder decides what happens.

### Fragments as values

A fragment used as a value replaces the value:

`logging.yaml`:
```yaml
logging:
  level: info
  appenders: ${LOG_APPENDERS | fragment}
```

```console
$ LOG_APPENDERS='[{"type": "console"}, {"type": "file", "path": "/var/log/app.log"}]' gonfig config process -f logging.yaml
logging:
  level: info
  appenders:
    - type: console
    - type: file
      path: /var/log/app.log
```

In XML files, a fragment replaces the whole content of the element containing the placeholder, including other text and child elements. It may consist of several elements and text:

`server.xml`:
```xml
<server>
  <resources>${DATASOURCES | fragment}</resources>
</server>
```

```console
$ DATASOURCES='<datasource name="main"/><datasource name="readonly"/>' gonfig config process -f server.xml
<server>
  <resources><datasource name="main"/><datasource name="readonly"/></resources>
</server>
```

### Fragments as keys

A fragment used as a key without a value is merged into the mapping or object containing it:

`database.yaml`:
```yaml
database:
  host: localhost # development
  port: 5432
  pool:
    timeout: 30
  ${DB_OVERRIDES | fragment}:
```

`db-prod.yaml`:
```yaml
host: db.prod
pool:
  size: 20
```

```console
$ DB_OVERRIDES=@db-prod.yaml gonfig config process -f database.yaml
database:
  host: db.prod # development
  port: 5432
  pool:
    timeout: 30
    size: 20
```

The fragment is merged deeply:

- **Mappings and objects** are merged recursively.
- **Other values**, like scalars and lists, are replaced by the ones of the fragment.
- **Comments and key order** of the file are kept, and new keys are appended in the order of the fragment.

In JSON files, the key has to have the value `null`, e.g. `"${DB_OVERRIDES | fragment}": null`. XML files support fragments only as the content of elements.

### Lists as fragments

A list created by `split` becomes a list in YAML and JSON files when it is inserted as a fragment:

`list.yaml`:
```yaml
a: "val"
b: "${ENVVAR | split(,) | fragment}"
```

```console
$ ENVVAR=alpha,beta,gamma gonfig config process -f list.yaml
a: "val"
b:
  - alpha
  - beta
  - gamma
```

`list.json`:
```json
{"a": "val", "b": "${ENVVAR | split | fragment}"}
```

```console
$ ENVVAR=alpha,beta gonfig config process -f list.json
{
  "a": "val",
  "b": [
    "alpha",
    "beta"
  ]
}
```

An empty value becomes an empty list:

```console
$ ENVVAR= gonfig config process -f list.yaml
a: "val"
b: []
```

This does not work in the following cases:

- **Whitespace is kept.** Values like `alpha, beta` keep the space before `beta`. Add `trim`, which is applied to each element of the list:

  `trimmed.yaml`:
  ```yaml
  b: ${ENVVAR | split | trim | fragment}
  ```

  ```console
  $ ENVVAR='alpha, beta ,gamma' gonfig config process -f trimmed.yaml
  b:
    - alpha
    - beta
    - gamma
  ```

- **Elements are always strings.** `split` creates a list of strings, so numbers are quoted, and `to_int` cannot convert a list:

  `numbers.yaml`:
  ```yaml
  strings: ${ENVVAR | split | fragment}
  ```

  `to_int.yaml`:
  ```yaml
  numbers: ${ENVVAR | split | to_int | fragment}
  ```

  ```console
  $ ENVVAR=1,2,3 gonfig config process -f numbers.yaml
  strings:
    - "1"
    - "2"
    - "3"
  $ ENVVAR=1,2,3 gonfig config process -f to_int.yaml
  Error: failed to process the value of numbers in to_int.yaml: filter to_int: a value of type []string cannot be converted to an integer
  ```

- **Elements cannot contain the delimiter.** `split` has no escaping, so a value like `a\,b` is split at the comma anyway.

For typed elements or elements containing commas, provide the list as a JSON array and insert it without `split`:

`array.yaml`:
```yaml
b: ${ENVVAR | fragment}
```

```console
$ ENVVAR='["a,b", "c", 1, true]' gonfig config process -f array.yaml
b:
  - a,b
  - c
  - 1
  - true
```

- **XML files have no lists**, so inserting a list fails:

  `list.xml`:
  ```xml
  <r><b>${ENVVAR | split | fragment}</b></r>
  ```

  ```console
  $ ENVVAR=alpha,beta gonfig config process -f list.xml
  Error: failed to insert the fragment of /r/b in list.xml: lists can only be inserted into YAML and JSON files
  ```

- **Lists cannot be embedded into text**, like any fragment, e.g. `b: "hosts: ${ENVVAR | split | fragment}"` fails.
- **Without `fragment`**, a list is written as text in Go's list format, e.g. `b: '[alpha beta]'`.

### Rules

- **Whole values only:** A fragment has to make up the whole value or key, otherwise processing fails.
- **Placeholders within fragments** are processed as well, e.g. a fragment may contain `${DB_PASSWORD | @secret}`. Fragments cannot contain further fragments.
- **Empty fragments** replace a value by null, remove a key or remove the content of an XML element.
- **Formatting:** YAML fragments are written in block style. Quotes are only kept where they are needed to keep a string a string.
- **Errors:** Invalid fragments, fragments in properties, TOML or plain text files and fragments in XML attributes fail processing, so no output is written. The error of an invalid fragment names the format and, if known, the line, e.g. `invalid YAML fragment on line 3`. The parser's message may quote parts of the fragment, so it is only included with `log-values`, see [configuration](./README.md#configuration).
- **Templates:** Fragment files can be provided by [placeholder templates](#templates), e.g. with `default: "@/etc/app/logging.yaml"` and `${@logging | fragment}`.

## Recipes

### Read secrets from files

Docker and Kubernetes usually mount secrets as files. Point the environment variable to the file with a leading `@`.
Files often end with a newline, which `trim` removes:

```console
$ printf 's3cret\n' > /run/secrets/db_password
$ DB_PASSWORD=@/run/secrets/db_password gonfig value 'password=${DB_PASSWORD | trim}'
password=s3cret
```

If the file does not exist, e.g. because the secret is not mounted, processing fails instead of writing the path:

```console
$ DB_PASSWORD=@/run/secrets/missing gonfig value 'password=${DB_PASSWORD | trim}'
Error: failed to read the file referenced with @: open /run/secrets/missing: no such file or directory
```

Use an absolute path or one starting with `./` or `../` (or `.\` or `..\` on Windows) to ensure processing fails for missing files.

### Read secrets from secret managers

The [plugins for secret managers](./PLUGINS.md#plugins-for-secret-managers) read secrets from OpenBao, HashiCorp Vault and Azure Key Vault. The variable holds the reference to the secret.
This example uses a local dev server (`bao server -dev -dev-root-token-id=root`), whose root token and plain HTTP are only meant for development. Use an `https://` address and a token with a policy limited to the secrets needed, or AppRole or Kubernetes authentication, for real servers, see the [OpenBao plugin](./plugins/openbao/#authentication):

```console
$ export BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN=root
$ bao kv put secret/app/db user=app password=s3cret
$ DB_PASSWORD='secret/app/db#password' gonfig value 'password=${DB_PASSWORD | openbao}'
password=s3cret
```

A placeholder template provides the reference by default, so config files only use `${@db_password}`:

```yaml
# .gonfig.yaml
templates:
  placeholders:
    db_password:
      env: DB_PASSWORD_REF
      default: "secret/app/db#password"
      filters: "openbao"
```

Without plugins, an agent of the secret manager can write the secrets to files, which gonfig reads as [files](#read-secrets-from-files), e.g. the agents of OpenBao and Vault, or the [Secrets Store CSI driver](https://secrets-store-csi-driver.sigs.k8s.io/) in Kubernetes, which mounts secrets of Vault, Azure Key Vault, AWS and GCP into pods:

```yaml
# Pod with a volume of the CSI driver mounted at /mnt/secrets
env:
  - name: DB_PASSWORD
    value: "@/mnt/secrets/db-password"
```

The plugins need no agent, sidecar or volume and read every secret when gonfig runs; the files need no credentials in gonfig's environment and work with any secret manager the agent supports.

### Generate password hashes

Store a bcrypt hash instead of the plain password, e.g. for an htpasswd file processed with `-t plain`:

```console
$ ADMIN_USER=admin ADMIN_PASSWORD=s3cret gonfig value '${ADMIN_USER}:${ADMIN_PASSWORD | bcrypt}'
admin:$2a$10$3nh7.N18au9F.xLeAg3Hs.7sPqH.aHmJE7eBoMMv8gEDIQHslpm3i
```

### Build connection strings and URLs

Combine several variables and plain text in one value:

```console
$ DB_USER=app DB_PASSWORD=s3cret DB_HOST=db gonfig value 'postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:5432/app'
postgres://app:s3cret@db:5432/app
```

Escape credentials with `url_encode` if they may contain special characters, e.g. as query parameters of a JDBC URL:

```console
$ DB_USER=app DB_PASSWORD='p@ss/w rd&' gonfig value 'jdbc:postgresql://db:5432/app?user=${DB_USER | url_encode}&password=${DB_PASSWORD | url_encode}'
jdbc:postgresql://db:5432/app?user=app&password=p%40ss%2Fw+rd%26
```

To use the same URL in several places, define it once as a [placeholder template](#templates).

### HTTP basic authentication

The credentials have to be base64 encoded as a whole, so provide them as a single variable:

```console
$ CREDENTIALS=admin:s3cret gonfig value 'Authorization: Basic ${CREDENTIALS | base64}'
Authorization: Basic YWRtaW46czNjcmV0
```

### Normalize lists

Remove stray whitespace from comma separated values:

```console
$ HOSTS='db1 , db2,db3' gonfig value '${HOSTS | split | trim | join}'
db1,db2,db3
```

## Caveats

- **Missing variables are empty:** A variable that is not set is replaced by an empty string without an error, unless [`fail-on-missing`](#missing-variables) is enabled.
- **Types in config files:** Processed values are strings unless a filter like `to_int` converts them, see [types](#types).
- **YAML flow collections:** Within `[...]` or `{...}` an unquoted `${...}` is invalid YAML. Quote it, e.g. `hosts: ["${HOST_A}", "${HOST_B}"]`.
- **Plain text files:** Every `${...}` is treated as a placeholder, including shell variables that were meant to be expanded by the shell later. Write those as `$${...}`, see [placeholder syntax](#placeholder-syntax).
- **No nesting:** Placeholders cannot be nested, `${A_${B}}` does not work.
