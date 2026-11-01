package file

import (
	"fmt"
	"io"
	"strings"
)

// DotenvFileHandler is a handler for dotenv files, e.g. the .env files of Docker Compose. It supports the common
// subset of the dotenv implementations: KEY=value lines, optionally preceded by export, comments starting with #,
// unquoted values ending at a # after whitespace, single-quoted values without escapes and double-quoted values with
// the escapes \n, \r, \t, \\ and \". Quoted values may span several lines. Changed values are written with the quotes
// they were read with where possible, so everything else is kept as read.
type DotenvFileHandler struct {
	lineFileHandler
}

// NewDotenvConfigFileHandler creates a new DotenvFileHandler
func NewDotenvConfigFileHandler() *DotenvFileHandler {
	return &DotenvFileHandler{lineFileHandler{encode: encodeDotenv}}
}

// Read reads the configuration file
func (d *DotenvFileHandler) Read(source io.Reader) error {
	return d.read(source, parseDotenvLines)
}

// parseDotenvLines splits the content into lines and parses the key and value of each pair. Lines end with LF, CRLF
// or CR. Lines that are neither blank, comments nor pairs are an error, since they would be lost silently otherwise.
func parseDotenvLines(content string) ([]*valueLine, error) {
	var lines []*valueLine
	for pos, number := 0, 1; pos < len(content); {
		start := pos
		text, ending := naturalLine(content[pos:])
		pos += len(text) + len(ending)

		trimmed := strings.TrimLeft(text, lineWhitespace)
		if trimmed == "" || trimmed[0] == '#' {
			lines = append(lines, &valueLine{raw: content[start:pos]})
			number++
			continue
		}

		key, i, err := parseDotenvKey(text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", number, err)
		}

		line := &valueLine{prefix: text[:i]}
		var value string
		if i < len(text) && (text[i] == '"' || text[i] == '\'') {
			line.quote = text[i]
			end, decoded, ok := scanDotenvQuoted(content, start+i+1, line.quote)
			if !ok {
				return nil, fmt.Errorf("line %d: the quoted value of %s is not terminated", number, key)
			}
			value = decoded
			rest, restEnding := naturalLine(content[end+1:])
			if after := strings.TrimLeft(rest, lineWhitespace); after != "" && after[0] != '#' {
				return nil, fmt.Errorf("line %d: unexpected text after the quoted value of %s", number, key)
			}
			line.suffix = rest
			ending = restEnding
			number += lineBreaks(content[start:end])
			pos = end + 1 + len(rest) + len(restEnding)
		} else {
			value = strings.TrimRight(text[i:dotenvCommentStart(text, i)], lineWhitespace)
			line.suffix = text[i+len(value):]
		}
		number++

		line.raw = content[start:pos]
		line.ending = ending
		line.entry = &LineConfigEntry{key: key, path: key, val: value, original: value}
		lines = append(lines, line)
	}
	return lines, nil
}

// parseDotenvKey parses the key of a pair, optionally preceded by export, and returns it with the position of its
// value, after the = and the whitespace following it
func parseDotenvKey(text string) (string, int, error) {
	i := skipWhitespace(text, 0)
	// export KEY=value exports the variable in shells; a key named export is still possible, e.g. export=1
	if rest := text[i:]; strings.HasPrefix(rest, "export") && len(rest) > len("export") && strings.IndexByte(lineWhitespace, rest[len("export")]) >= 0 {
		if j := skipWhitespace(text, i+len("export")); j < len(text) && text[j] != '=' {
			i = j
		}
	}

	keyStart := i
	for i < len(text) && text[i] != '=' && strings.IndexByte(lineWhitespace, text[i]) < 0 {
		i++
	}
	key := text[keyStart:i]
	i = skipWhitespace(text, i)
	if key == "" || i == len(text) || text[i] != '=' {
		return "", 0, fmt.Errorf("expected KEY=value or a comment starting with #")
	}
	return key, skipWhitespace(text, i+1), nil
}

// dotenvCommentStart returns the position of the comment of an unquoted value starting at start: a # following
// whitespace, or the end of the line. A # within the value, e.g. in a URL fragment, belongs to the value.
func dotenvCommentStart(text string, start int) int {
	for i := start; i < len(text); i++ {
		if text[i] == '#' && i > 0 && strings.IndexByte(lineWhitespace, text[i-1]) >= 0 {
			return i
		}
	}
	return len(text)
}

// scanDotenvQuoted scans the quoted value starting at from, after its opening quote, and returns the position of the
// closing quote and the value. Double-quoted values replace the escapes \n, \r, \t, \\ and \", and keep other
// backslashes. It returns false if the value is not terminated.
func scanDotenvQuoted(content string, from int, quote byte) (int, string, bool) {
	if quote == '\'' {
		end := strings.IndexByte(content[from:], '\'')
		if end < 0 {
			return 0, "", false
		}
		return from + end, content[from : from+end], true
	}

	var value strings.Builder
	for i := from; i < len(content); i++ {
		switch c := content[i]; {
		case c == '"':
			return i, value.String(), true
		case c == '\\' && i+1 < len(content):
			i++
			switch content[i] {
			case 'n':
				value.WriteByte('\n')
			case 'r':
				value.WriteByte('\r')
			case 't':
				value.WriteByte('\t')
			case '\\', '"':
				value.WriteByte(content[i])
			default:
				value.WriteByte('\\')
				value.WriteByte(content[i])
			}
		default:
			value.WriteByte(c)
		}
	}
	return 0, "", false
}

// lineBreaks returns the number of line breaks in the text: LF, CRLF and CR
func lineBreaks(text string) int {
	return strings.Count(text, "\n") + strings.Count(text, "\r") - strings.Count(text, "\r\n")
}

// encodeDotenv returns the value written with the quote it was read with. Single-quoted values containing a single
// quote and unquoted values that would be read differently, e.g. because they contain a line break or a # after
// whitespace, are written as double-quoted values.
func encodeDotenv(entry *LineConfigEntry, quote byte) (string, error) {
	value := entry.val
	switch {
	case quote == '\'' && !strings.Contains(value, "'"):
		return "'" + value + "'", nil
	case quote == 0 && !dotenvNeedsQuotes(value):
		return value, nil
	}

	var encoded strings.Builder
	encoded.Grow(len(value) + 2)
	encoded.WriteByte('"')
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '\\', '"':
			encoded.WriteByte('\\')
			encoded.WriteByte(c)
		case '\n':
			encoded.WriteString(`\n`)
		case '\r':
			encoded.WriteString(`\r`)
		default:
			encoded.WriteByte(c)
		}
	}
	encoded.WriteByte('"')
	return encoded.String(), nil
}

// dotenvNeedsQuotes returns whether an unquoted value would be read differently: if it contains quotes, backslashes,
// # or line breaks, or starts or ends with whitespace. Implementations differ in how they read quotes, backslashes
// and # within unquoted values, so these values are quoted as well.
func dotenvNeedsQuotes(value string) bool {
	if strings.ContainsAny(value, "\"'\\#\r\n") {
		return true
	}
	return value != strings.Trim(value, lineWhitespace)
}
