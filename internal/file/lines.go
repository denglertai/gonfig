package file

import (
	"bufio"
	"io"
	"iter"
	"strings"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/schema"
)

// lineFileHandler is the base of the handlers of line based formats with key/value pairs, like dotenv and INI files.
// Every line is written as read unless the value of its entry changed, so comments, blank lines, quotes, separators
// and line endings are kept.
type lineFileHandler struct {
	lines []*valueLine
	// encode returns the text of a changed value written with the quote of its line, see valueLine.quote
	encode func(entry *LineConfigEntry, quote byte) (string, error)
	// bom is set if the file starts with a UTF-8 byte order mark, which is not part of the first key
	bom bool
}

// valueLine is a line of a line based file: a blank line, a comment, a header or a key/value pair, whose quoted value
// may span several lines
type valueLine struct {
	// raw is the text of the line as read, including its line ending
	raw string
	// entry is the key and value of the line, or nil for lines without a value
	entry *LineConfigEntry
	// prefix is the text before the value as read, e.g. "export KEY=", and suffix the text after it up to the line
	// ending, e.g. " # comment", both without the value's quotes
	prefix string
	suffix string
	// quote is the quote character of the value, or 0 for unquoted values
	quote byte
	// ending is the line ending of the last line, which is empty at the end of the file
	ending string
}

// LineConfigEntry is a configuration entry of a line based file: the value of a key, whose path includes its section
type LineConfigEntry struct {
	key  string
	path string
	val  string
	// original is the value as read, so lines are only rewritten if their value changed
	original string
}

// Key returns the key of the configuration entry
func (l *LineConfigEntry) Key() string {
	return l.key
}

// Path returns the path of the configuration entry: the key, preceded by its section in INI files
func (l *LineConfigEntry) Path() string {
	return l.path
}

// GetValue returns the value of the configuration entry
func (l *LineConfigEntry) GetValue() string {
	return l.val
}

// SetValue sets the value of the configuration entry
func (l *LineConfigEntry) SetValue(value string) {
	l.val = value
}

// keepsFormat returns true, since every line is written with its line ending as read and the handler keeps the BOM
func (h *lineFileHandler) keepsFormat() bool {
	return true
}

// read reads the content and splits it into lines with parse, which gets the content without the BOM
func (h *lineFileHandler) read(source io.Reader, parse func(content string) ([]*valueLine, error)) error {
	// The lines refer to the content, which is read into a string directly, so the file is kept in memory only once
	var data strings.Builder
	if _, err := io.Copy(&data, source); err != nil {
		return err
	}

	content := data.String()
	h.bom = strings.HasPrefix(content, string(utf8BOM))
	lines, err := parse(strings.TrimPrefix(content, string(utf8BOM)))
	if err != nil {
		return err
	}
	h.lines = lines
	return nil
}

// Process processes the configuration file and returns an entry for every key with a value, including duplicate keys
func (h *lineFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return func(yield func(ConfigEntry) bool) {
		for _, line := range h.lines {
			if line.entry != nil && !yield(line.entry) {
				return
			}
		}
	}, nil
}

// Write writes the configuration file. Lines whose value changed are written as their prefix, the encoded value and
// their suffix. All other lines are written as read.
func (h *lineFileHandler) Write(destination io.Writer) error {
	// Errors of the destination are kept by the buffered writer and returned by Flush
	output := bufio.NewWriter(destination)
	if h.bom {
		_, _ = output.Write(utf8BOM)
	}
	for _, line := range h.lines {
		if line.entry == nil || line.entry.val == line.entry.original {
			_, _ = output.WriteString(line.raw)
			continue
		}
		value, err := h.encode(line.entry, line.quote)
		if err != nil {
			return err
		}
		_, _ = output.WriteString(line.prefix)
		_, _ = output.WriteString(value)
		_, _ = output.WriteString(line.suffix)
		_, _ = output.WriteString(line.ending)
	}
	return output.Flush()
}

// lineWhitespace are the characters separating keys, separators and values in dotenv and INI files
const lineWhitespace = " \t"

// skipWhitespace returns the position of the first character at or after i that is no space or tab
func skipWhitespace(text string, i int) int {
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return i
}

// init registers the decoders of dotenv and INI files for the schema validation, which validates dotenv files as an
// object of strings and INI files as an object of sections, which are objects of strings, besides the keys before the
// first section. The last value of a key wins, like for most implementations.
func init() {
	decodeDotenv := func(data []byte) (any, error) {
		lines, err := parseDotenvLines(string(data))
		if err != nil {
			return nil, err
		}
		document := map[string]any{}
		for _, line := range lines {
			if line.entry != nil {
				document[line.entry.key] = line.entry.val
			}
		}
		return document, nil
	}
	schema.RegisterDecoder(general.ENV, decodeDotenv)
	schema.RegisterDecoder(general.DOTENV, decodeDotenv)

	schema.RegisterDecoder(general.INI, func(data []byte) (any, error) {
		lines, err := parseIniLines(string(data))
		if err != nil {
			return nil, err
		}
		document := map[string]any{}
		for _, line := range lines {
			if line.entry == nil {
				continue
			}
			table := document
			if section, isSection := strings.CutSuffix(line.entry.path, "."+line.entry.key); isSection {
				sectionTable, ok := document[section].(map[string]any)
				if !ok {
					sectionTable = map[string]any{}
					document[section] = sectionTable
				}
				table = sectionTable
			}
			table[line.entry.key] = line.entry.val
		}
		return document, nil
	})
}
