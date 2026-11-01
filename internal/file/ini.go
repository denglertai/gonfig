package file

import (
	"fmt"
	"io"
	"strings"
)

// IniFileHandler is a handler for INI files, e.g. php.ini, systemd units or git config. It supports the common subset
// of the dialects: [section] headers, key = value and key: value pairs, and comments starting with ; or #. Entries
// are named section.key, or key before the first section. Values are read as written up to the end of the line, so
// inline comments are part of the value and kept, except for double-quoted values, which may be followed by a comment
// and whose quotes are not part of the value. They have the escapes \n, \r, \t, \\ and \". Lines without
// a separator, e.g. keys without a value or continuation lines, are kept as read.
type IniFileHandler struct {
	lineFileHandler
}

// NewIniConfigFileHandler creates a new IniFileHandler
func NewIniConfigFileHandler() *IniFileHandler {
	return &IniFileHandler{lineFileHandler{encode: encodeIni}}
}

// Read reads the configuration file
func (h *IniFileHandler) Read(source io.Reader) error {
	return h.read(source, parseIniLines)
}

// parseIniLines splits the content into lines and parses the section headers and the key and value of each pair.
// Lines end with LF, CRLF or CR.
func parseIniLines(content string) ([]*valueLine, error) {
	var lines []*valueLine
	section := ""
	for pos, number := 0, 1; pos < len(content); number++ {
		start := pos
		text, ending := naturalLine(content[pos:])
		pos += len(text) + len(ending)
		line := &valueLine{raw: content[start:pos], ending: ending}
		lines = append(lines, line)

		trimmed := strings.TrimLeft(text, lineWhitespace)
		switch {
		case trimmed == "" || trimmed[0] == ';' || trimmed[0] == '#':
			continue
		case trimmed[0] == '[':
			end := strings.IndexByte(trimmed, ']')
			if end < 0 {
				return nil, fmt.Errorf("line %d: the section header has no closing ]", number)
			}
			section = strings.TrimSpace(trimmed[1:end])
			continue
		}

		separator := strings.IndexAny(text, "=:")
		key := ""
		if separator >= 0 {
			key = strings.Trim(text[:separator], lineWhitespace)
		}
		if key == "" {
			continue
		}

		i := skipWhitespace(text, separator+1)
		end := max(i, len(strings.TrimRight(text, lineWhitespace)))
		value := text[i:end]
		line.prefix = text[:i]
		line.suffix = text[end:]
		if closing, ok := iniQuoteEnd(text, i); ok {
			line.quote = '"'
			value = decodeIniQuoted(text[i+1 : closing])
			line.suffix = text[closing+1:]
		}

		path := key
		if section != "" {
			path = section + "." + key
		}
		line.entry = &LineConfigEntry{key: key, path: path, val: value, original: value}
	}
	return lines, nil
}

// iniQuoteEnd returns the position of the closing quote of a double-quoted value starting at start, and whether the
// value is double-quoted: it starts with a quote whose closing quote is followed by whitespace or a comment only
func iniQuoteEnd(text string, start int) (int, bool) {
	if start >= len(text) || text[start] != '"' {
		return 0, false
	}
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			rest := strings.TrimLeft(text[i+1:], lineWhitespace)
			return i, rest == "" || rest[0] == ';' || rest[0] == '#'
		}
	}
	return 0, false
}

// decodeIniQuoted replaces the escapes \n, \r, \t, \\ and \" of a double-quoted value and keeps other backslashes
func decodeIniQuoted(text string) string {
	if !strings.Contains(text, `\`) {
		return text
	}
	var decoded strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] != '\\' || i+1 == len(text) {
			decoded.WriteByte(text[i])
			continue
		}
		i++
		switch text[i] {
		case 'n':
			decoded.WriteByte('\n')
		case 'r':
			decoded.WriteByte('\r')
		case 't':
			decoded.WriteByte('\t')
		case '\\', '"':
			decoded.WriteByte(text[i])
		default:
			decoded.WriteByte('\\')
			decoded.WriteByte(text[i])
		}
	}
	return decoded.String()
}

// encodeIni returns the value written with the quotes it was read with. Unquoted values are written as they are,
// since dialects differ in quoting, so a line break, which would end the value, is an error.
func encodeIni(entry *LineConfigEntry, quote byte) (string, error) {
	value := entry.val
	if quote == 0 {
		if strings.ContainsAny(value, "\r\n") {
			return "", fmt.Errorf("the value of %s contains a line break, which an unquoted INI value cannot hold; "+
				`quote the value in the file, e.g. key = "${VAR}", to write it as \n`, entry.path)
		}
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
