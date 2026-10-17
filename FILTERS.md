# Filters

Filters transform the values that gonfig reads from environment variables before they are written into a config file.
This page documents the placeholder syntax, every built-in filter and some common recipes.

- [Placeholder syntax](#placeholder-syntax)
- [Order of operations](#order-of-operations)
- [Filter params](#filter-params)
- [Built-in filters](#built-in-filters)
- [Lists](#lists)
- [Types](#types)
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

## Order of operations

Every placeholder is resolved in the following steps:

1. **Environment variable lookup:** The value of the environment variable is read. A variable that is not set results in an empty string, not in an error.
1. **File inclusion:** If the value starts with `@`, the rest is treated as a file path and the value is replaced by the file's content.
   If the file does not exist, the value is kept as is (including the `@`). If it exists but cannot be read, processing fails.
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
| [`to_int`](#to_int--to_float--to_bool) | | Converts the value to an integer |
| [`to_float`](#to_int--to_float--to_bool) | | Converts the value to a floating point number |
| [`to_bool`](#to_int--to_float--to_bool) | | Converts the value to a boolean |
| [`multiply`](#multiply) | `m` | Multiplies an integer |
| [`md5`](#md5) | | Hashes the value with MD5 |
| [`bcrypt`](#bcrypt) | | Hashes the value with bcrypt |
| [`base64`](#base64--base64_decode) | | Encodes the value to base64 |
| [`base64_decode`](#base64--base64_decode) | | Decodes a base64 value |
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

### to_int / to_float / to_bool

Convert the value to an integer, a floating point number or a boolean, and fail if it cannot be converted.
YAML and JSON files write the result as a number or boolean, see [types](#types).

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
```

### multiply

Multiplies an integer by the named param `m`, which defaults to `1`.
The value has to be an integer or a numeric string, so it is usually combined with `to_int`.
`m` has to be given by name: `multiply(2)` is ignored and multiplies by `1`.

```console
$ WORKERS=4 gonfig value '${WORKERS | to_int | multiply(m=2)}'
8
```

### md5

Hashes the value with MD5 and returns the hex encoded digest.
MD5 is not suitable for passwords, use [bcrypt](#bcrypt) instead.

```console
$ V=hello gonfig value '${V | md5}'
5d41402abc4b2a76b9719d911017c592
```

### bcrypt

Hashes the value with bcrypt using the default cost of 10. The salt is random, so the result differs on every run.
Values longer than 72 bytes cannot be hashed and cause an error.

```console
$ ADMIN_PASSWORD=s3cret gonfig value '${ADMIN_PASSWORD | bcrypt}'
$2a$10$3nh7.N18au9F.xLeAg3Hs.7sPqH.aHmJE7eBoMMv8gEDIQHslpm3i
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
The delimiter defaults to `,` for both.

```console
$ HOSTS='db1 , db2,db3' gonfig value '${HOSTS | split | trim | join(\, )}'
db1, db2, db3
$ PATHS='/a:/b' gonfig value '${PATHS | split(:) | join(;)}'
/a;/b
```

## Lists

`split` produces a list instead of a single value. The following filters are applied to each element of a list:
`upper`, `lower`, `trim`, `trimleft`, `trimright`, `base64`, `base64_decode`, `replace` and `regex`.

```console
$ LIST='a, b ,c' gonfig value '${LIST | split | trim | upper | join(;)}'
A;B;C
```

Always `join` a list before the placeholder ends. A list that is not joined is written in Go's list format, e.g. `[a b c]`.

## Types

Placeholders are replaced by text, so processed values are strings by default, even if they look like numbers.
This keeps values like `01701` or `123456` unchanged, e.g. for zip codes or passwords.

`to_int`, `to_float` and `to_bool` return a number or boolean instead.
If a placeholder with such a result makes up the whole value, file types supporting types write it as such:

| File type | `${PORT \| to_int}` | `${ENABLED \| to_bool}` | `${ZIP}` |
| --- | --- | --- | --- |
| YAML | `8080` | `true` | `"01701"` |
| JSON | `8080` | `true` | `"01701"` |
| XML, properties, plain text | `8080` as text | `true` as text | `01701` as text |

In YAML, this also applies to quoted placeholders like `"${PORT | to_int}"`, while keys of mappings are always text.
In JSON, placeholders are always written within strings, e.g. `"port": "${PORT | to_int}"` becomes `"port": 8080`.

If such a placeholder is surrounded by other text, its result is formatted as text:

```console
$ PORT=8080 gonfig value 'http://localhost:${PORT | to_int}/api'
http://localhost:8080/api
```

## Recipes

### Read secrets from files

Docker and Kubernetes usually mount secrets as files. Point the environment variable to the file with a leading `@`.
Files often end with a newline, which `trim` removes:

```console
$ printf 's3cret\n' > /run/secrets/db_password
$ DB_PASSWORD=@/run/secrets/db_password gonfig value 'password=${DB_PASSWORD | trim}'
password=s3cret
```

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

- **Missing variables are empty:** A variable that is not set is replaced by an empty string without an error.
- **Types in config files:** Processed values are strings unless a filter like `to_int` converts them, see [types](#types).
- **YAML flow collections:** Within `[...]` or `{...}` an unquoted `${...}` is invalid YAML. Quote it, e.g. `hosts: ["${HOST_A}", "${HOST_B}"]`.
- **Plain text files:** Every `${...}` is treated as a placeholder, including shell variables that were meant to be expanded by the shell later.
- **No nesting:** Placeholders cannot be nested, `${A_${B}}` does not work.
