package file

import (
	"bufio"
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"
)

// PropertiesFileHandler is a handler for properties files. It parses the file line by line, like Java's Properties,
// and writes every line as it was read unless its value changed, so comments, blank lines, separators, continuation
// lines, duplicate keys and line endings are kept.
type PropertiesFileHandler struct {
	lines   []*propertiesLine
	wrapped []ConfigEntry
	// bom is set if the file starts with a UTF-8 byte order mark, which is not part of the first key
	bom bool
}

// propertiesLine is a logical line of a properties file: a blank line, a comment or an entry,
// which may span several lines joined by a backslash at their end
type propertiesLine struct {
	// raw is the text of the line as read, including its continuation lines and its line ending
	raw string
	// entry is the key and value of the line, or nil for blank lines and comments
	entry *PropertiesConfigEntry
	// prefix is the text before the value as read, e.g. "  key = ", which is kept when the value changes
	prefix string
	// ending is the line ending of the last line, which is empty at the end of the file
	ending string
}

// PropertiesConfigEntry is a configuration entry for properties files
type PropertiesConfigEntry struct {
	key string
	val string
	// original is the value as read, so lines are only rewritten if their value changed
	original string
}

// Key returns the key of the configuration entry
func (p *PropertiesConfigEntry) Key() string {
	return p.key
}

// Path returns the path of the configuration entry
func (p *PropertiesConfigEntry) Path() string {
	return p.key
}

// GetValue returns the value of the configuration entry
func (p *PropertiesConfigEntry) GetValue() string {
	return p.val
}

// SetValue sets the value of the configuration entry
func (p *PropertiesConfigEntry) SetValue(value string) {
	p.val = value
}

// NewPropertiesConfigFileHandler creates a new PropertiesFileHandler
func NewPropertiesConfigFileHandler() *PropertiesFileHandler {
	return &PropertiesFileHandler{}
}

// keepsFormat returns true, since every line is written with its line ending as read and the handler keeps the BOM
func (p *PropertiesFileHandler) keepsFormat() bool {
	return true
}

// Read reads the configuration file
func (p *PropertiesFileHandler) Read(source io.Reader) error {
	// The lines refer to the content, which is read into a string directly, so the file is kept in memory only once
	var data strings.Builder
	if _, err := io.Copy(&data, source); err != nil {
		return err
	}

	content := data.String()
	p.bom = strings.HasPrefix(content, string(utf8BOM))
	lines, err := parsePropertiesLines(strings.TrimPrefix(content, string(utf8BOM)))
	if err != nil {
		return err
	}
	p.lines = lines
	return nil
}

// Process processes the configuration file and returns an entry for every key, including duplicate keys
func (p *PropertiesFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	p.wrapped = make([]ConfigEntry, 0, len(p.lines))
	for _, line := range p.lines {
		if line.entry != nil {
			p.wrapped = append(p.wrapped, line.entry)
		}
	}

	return func(yield func(ConfigEntry) bool) {
		for _, entry := range p.wrapped {
			if !yield(entry) {
				return
			}
		}
	}, nil
}

// Write writes the configuration file. Lines whose value changed are written as their prefix and the escaped value,
// so a value spanning several lines is written as a single line. All other lines are written as read.
func (p *PropertiesFileHandler) Write(destination io.Writer) error {
	// Errors of the destination are kept by the buffered writer and returned by Flush
	output := bufio.NewWriter(destination)
	if p.bom {
		_, _ = output.Write(utf8BOM)
	}
	for _, line := range p.lines {
		if line.entry == nil || line.entry.val == line.entry.original {
			_, _ = output.WriteString(line.raw)
			continue
		}
		_, _ = output.WriteString(line.prefix)
		_, _ = output.WriteString(encodePropertiesValue(line.entry.val))
		_, _ = output.WriteString(line.ending)
	}
	return output.Flush()
}

// propertiesWhitespace are the characters separating keys and values and starting lines, see Java's Properties.load
const propertiesWhitespace = " \t\f"

// parsePropertiesLines splits the content into logical lines and parses the key and value of each entry.
// Lines end with LF, CRLF or CR. A line ending with an odd number of backslashes continues on the next line,
// whose leading whitespace is skipped. Comments start with # or ! and are never continued.
func parsePropertiesLines(content string) ([]*propertiesLine, error) {
	var lines []*propertiesLine

	for pos, number := 0, 1; pos < len(content); {
		start := pos
		text, ending := naturalLine(content[pos:])
		pos += len(text) + len(ending)

		trimmed := strings.TrimLeft(text, propertiesWhitespace)
		if trimmed == "" || trimmed[0] == '#' || trimmed[0] == '!' {
			lines = append(lines, &propertiesLine{raw: content[start:pos]})
			number++
			continue
		}

		// The logical line starts with the first line without its continuing backslash, which keeps the text before the value
		first := text
		if continues(first) {
			first = first[:len(first)-1]
		}
		logical := text
		firstLine := number
		for continues(logical) {
			if ending == "" {
				return nil, fmt.Errorf("line %d ends with an escape character at the end of the file", number)
			}
			logical = logical[:len(logical)-1]
			text, ending = naturalLine(content[pos:])
			pos += len(text) + len(ending)
			number++
			logical += strings.TrimLeft(text, propertiesWhitespace)
		}
		number++

		line, err := parsePropertiesEntry(logical, first)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", firstLine, err)
		}
		line.raw = content[start:pos]
		line.ending = ending
		lines = append(lines, line)
	}

	return lines, nil
}

// naturalLine returns the first line of the content and its line ending: LF, CRLF, CR or empty at the end
func naturalLine(content string) (text string, ending string) {
	i := strings.IndexAny(content, "\r\n")
	switch {
	case i == -1:
		return content, ""
	case content[i] == '\r' && i+1 < len(content) && content[i+1] == '\n':
		return content[:i], "\r\n"
	default:
		return content[:i], content[i : i+1]
	}
}

// continues returns whether the line ends with an odd number of backslashes, which continues it on the next line
func continues(line string) bool {
	backslashes := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

// parsePropertiesEntry parses the key and value of a logical line. first is the beginning of the logical line as read
// in the first line, which keeps the text before the value if the value starts within it.
func parsePropertiesEntry(logical string, first string) (*propertiesLine, error) {
	i := len(logical) - len(strings.TrimLeft(logical, propertiesWhitespace))

	// The key ends at the first unescaped whitespace, = or :
	keyStart := i
	for i < len(logical) && !strings.ContainsRune(propertiesWhitespace+":=", rune(logical[i])) {
		if logical[i] == '\\' {
			i++
		}
		i++
	}
	i = min(i, len(logical))
	key, err := decodeProperties(logical[keyStart:i])
	if err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	// The separator is whitespace, = or : with optional whitespace around it
	i += len(logical[i:]) - len(strings.TrimLeft(logical[i:], propertiesWhitespace))
	if i < len(logical) && (logical[i] == '=' || logical[i] == ':') {
		i++
	}
	i += len(logical[i:]) - len(strings.TrimLeft(logical[i:], propertiesWhitespace))

	value, err := decodeProperties(logical[i:])
	if err != nil {
		return nil, fmt.Errorf("invalid value of %s: %w", key, err)
	}

	// A value starting on a continuation line, or keys spanning lines, get a prefix without the line breaks
	prefix := first[:min(i, len(first))]
	if i > len(first) {
		prefix = logical[:keyStart] + encodePropertiesKey(key) + "="
	}

	return &propertiesLine{entry: &PropertiesConfigEntry{key: key, val: value, original: value}, prefix: prefix}, nil
}

// decodeProperties replaces the escape sequences of a key or value: \t, \n, \r, \f, \uXXXX,
// and a backslash before any other character, which stands for the character itself.
// Logical lines never end with an unpaired backslash, see parsePropertiesLines, so neither do keys and values.
func decodeProperties(text string) (string, error) {
	if !strings.Contains(text, `\`) {
		return text, nil
	}

	var decoded strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] != '\\' || i+1 == len(text) {
			decoded.WriteByte(text[i])
			continue
		}
		i++
		switch text[i] {
		case 't':
			decoded.WriteByte('\t')
		case 'n':
			decoded.WriteByte('\n')
		case 'r':
			decoded.WriteByte('\r')
		case 'f':
			decoded.WriteByte('\f')
		case 'u':
			code, err := strconv.ParseUint(text[i+1:min(i+5, len(text))], 16, 16)
			if err != nil || i+5 > len(text) {
				return "", fmt.Errorf("invalid unicode escape %q", text[i-1:min(i+5, len(text))])
			}
			decoded.WriteRune(rune(code))
			i += 4
		default:
			decoded.WriteByte(text[i])
		}
	}
	return decoded.String(), nil
}

// encodePropertiesValue escapes a value written after a separator: backslashes, line breaks, tabs and form feeds,
// and a leading space, which would be part of the separator otherwise. Other characters are written as they are.
func encodePropertiesValue(value string) string {
	return encodeProperties(value, "")
}

// encodePropertiesKey escapes a key, including spaces, = and :, which would end it otherwise,
// and a leading # or !, which would start a comment
func encodePropertiesKey(key string) string {
	encoded := encodeProperties(key, " :=")
	if strings.HasPrefix(key, "#") || strings.HasPrefix(key, "!") {
		encoded = `\` + encoded
	}
	return encoded
}

// encodeProperties escapes the text like encodePropertiesValue, and additionally the special characters.
// All escaped characters are ASCII, so the text is processed byte by byte and other bytes are kept as they are.
func encodeProperties(text string, special string) string {
	var encoded strings.Builder
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\\':
			encoded.WriteString(`\\`)
		case c == '\n':
			encoded.WriteString(`\n`)
		case c == '\r':
			encoded.WriteString(`\r`)
		case c == '\t':
			encoded.WriteString(`\t`)
		case c == '\f':
			encoded.WriteString(`\f`)
		case (c == ' ' && i == 0) || strings.IndexByte(special, c) >= 0:
			encoded.WriteByte('\\')
			encoded.WriteByte(c)
		default:
			encoded.WriteByte(c)
		}
	}
	return encoded.String()
}
