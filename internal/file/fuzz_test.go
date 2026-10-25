package file

import (
	"bytes"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
)

// fuzzEnv are the environment variables available to the placeholders of fuzzed files.
// Their values contain characters that have to be escaped in most file types.
var fuzzEnv = map[string]string{
	"FUZZ_VALUE":    "a&b<c>\"d'e\\f:g #h{i}[j],k=l\n\tm",
	"FUZZ_INT":      "42",
	"FUZZ_LIST":     "x,y,z",
	"FUZZ_FRAGMENT": "key: value",
}

// fuzzSeeds are values with placeholders added to the seed corpus of every file type, besides its test data
var fuzzSeeds = []string{
	"${FUZZ_VALUE}",
	"${FUZZ_INT | to_int}",
	"${FUZZ_LIST | split | fragment}",
	"${FUZZ_FRAGMENT | fragment}",
	"a ${FUZZ_VALUE | upper} b",
}

// fuzzHandler checks for a file type that processing any input does not panic, and that the output of a successful run
// can be read again by the handler of the file type, so gonfig never writes a file it cannot read itself, e.g. by
// escaping a value wrongly. The seed corpus consists of the test data of the file type and documents with fuzzSeeds.
func fuzzHandler(f *testing.F, fileType general.FileType, testdata string, document func(value string) string) {
	for key, value := range fuzzEnv {
		f.Setenv(key, value)
	}

	files, err := filepath.Glob(filepath.Join("testdata", testdata, "*"))
	if err != nil {
		f.Fatal(err)
	}
	for _, name := range files {
		content, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(content))
	}
	for _, seed := range fuzzSeeds {
		f.Add(document(seed))
	}

	f.Fuzz(func(t *testing.T, input string) {
		output, ok := processFuzzInput(t, fileType, input, true)
		if !ok {
			return
		}
		if readFuzzOutput(t, fileType, output) == nil {
			return
		}

		// Some documents cannot be written readably by the library of the file type at all, e.g. YAML tags with escaped
		// characters like "!%21", which is written as "!!". Only output that is unreadable due to processing is a failure.
		unprocessed, ok := processFuzzInput(t, fileType, input, false)
		if ok && readFuzzOutput(t, fileType, unprocessed) != nil {
			t.Skip("the library of the file type cannot write the document readably, even without processing it")
		}
		t.Fatalf("the output cannot be read again: %v\ninput:\n%s\noutput:\n%s", readFuzzOutput(t, fileType, output), input, output)
	})
}

// readFuzzOutput reads the output with the handler of the file type and returns the error
func readFuzzOutput(t *testing.T, fileType general.FileType, output []byte) error {
	t.Helper()

	handler, err := (&FileProcessor{FileType: fileType}).getFileProcessor()
	if err != nil {
		t.Fatal(err)
	}
	return handler.Read(bytes.NewReader(output))
}

// processFuzzInput reads the input like a file of the file type, processes its entries if process is set, and returns the output,
// or false if reading or processing it fails, which is expected for most fuzzed inputs
func processFuzzInput(t *testing.T, fileType general.FileType, input string, process bool) ([]byte, bool) {
	t.Helper()

	var output bytes.Buffer
	fp := &FileProcessor{FileName: "fuzz", FileType: fileType, Output: &output}
	handler, err := fp.getFileProcessor()
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Read(strings.NewReader(input)); err != nil {
		return nil, false
	}
	entries, err := handler.Process()
	if err != nil {
		return nil, false
	}
	for entry := range entries {
		if !process {
			break
		}
		if err := fp.processEntry(entry, true, fp.fileGroup()); err != nil {
			return nil, false
		}
	}
	if err := handler.Write(&output); err != nil {
		return nil, false
	}
	return output.Bytes(), true
}

// FuzzYAML checks processing YAML files, see fuzzHandler
func FuzzYAML(f *testing.F) {
	fuzzHandler(f, general.YAML, "yaml", func(value string) string {
		return "a:\n  b: " + value + "\n  " + value + ": c\nl:\n  - " + value + "\n"
	})
}

// FuzzJSON checks processing JSON files, see fuzzHandler
func FuzzJSON(f *testing.F) {
	fuzzHandler(f, general.JSON, "json", func(value string) string {
		return `{"a": {"b": "` + value + `", "` + value + `": "c"}, "l": ["` + value + `", 1]}`
	})
}

// FuzzXML checks processing XML files, see fuzzHandler
func FuzzXML(f *testing.F) {
	fuzzHandler(f, general.XML, "xml", func(value string) string {
		return `<?xml version="1.0"?><a b="` + value + `">` + value + `<c>` + value + `</c><![CDATA[` + value + `]]>tail ` + value + `</a>`
	})
}

// FuzzProperties checks processing properties files, see fuzzHandler
func FuzzProperties(f *testing.F) {
	fuzzHandler(f, general.PROPERTIES, "properties", func(value string) string {
		return "# comment\na=" + value + "\nb = x " + value + "\n"
	})
}

// FuzzPropertiesRoundTrip checks that values and keys written to properties files are read as they were,
// so escaping never changes them
func FuzzPropertiesRoundTrip(f *testing.F) {
	for _, seed := range []string{"", " lead", "\\", "a\nb\r\tc\fd", "=:# !", "\\u00e4", "trail \\", "\xff\x00", "!0", "#k"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		lines, err := parsePropertiesLines(encodePropertiesKey(text) + "=" + encodePropertiesValue(text) + "\n")
		if err != nil {
			t.Fatalf("cannot read the encoded %q: %v", text, err)
		}
		if len(lines) != 1 || lines[0].entry == nil || lines[0].entry.key != text || lines[0].entry.val != text {
			t.Fatalf("encoded %q is read differently: %+v", text, lines)
		}
	})
}

// FuzzPlain checks processing plain text files, see fuzzHandler
func FuzzPlain(f *testing.F) {
	fuzzHandler(f, general.PLAIN, "plain", func(value string) string {
		return "line " + value + "\n" + value + "\n"
	})
}

// FuzzTOML checks processing TOML files, see fuzzHandler
func FuzzTOML(f *testing.F) {
	fuzzHandler(f, general.TOML, "toml", func(value string) string {
		return "# comment\na = \"" + value + "\"\n\"" + value + "\" = 'c'\nl = [\"\"\"" + value + "\"\"\", '''" + value + "''']\n"
	})
}

// FuzzTomlRoundTrip checks that strings and keys written to TOML files are read as they were, whatever quotes
// they were written with before, so escaping never changes them. Invalid UTF-8 is replaced, so it is skipped.
func FuzzTomlRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "'", "\"", `"""`, "'''", "a\nb\r\n\tc", "\\", "\x00\x7f", "\"\"\"\"", "x'", "\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if !utf8.ValidString(text) {
			t.Skip("invalid UTF-8 is replaced")
		}
		for _, raw := range []string{`"a"`, `'a'`, `"""a"""`, `'''a'''`, "\"\"\"\na\"\"\"", "'''\na'''"} {
			value := (&TomlConfigEntry{raw: raw, val: text}).encode()
			key := (&TomlConfigEntry{raw: raw[:1] + "k" + raw[:1], val: text}).encode()
			var document map[string]any
			if err := toml.Unmarshal([]byte("v = "+value+"\n[keys]\n"+key+" = 1\n"), &document); err != nil {
				t.Fatalf("cannot read %q written as %s: %v\n%s\n%s", text, raw, err, value, key)
			}
			if document["v"] != text {
				t.Fatalf("%q written as %s is read as %q: %s", text, raw, document["v"], value)
			}
			if keys, _ := document["keys"].(map[string]any); keys[text] == nil {
				t.Fatalf("key %q is read differently: %v", text, document)
			}
		}
	})
}

// FuzzTomlFragment checks that a fragment accepted as a value is valid where it is accepted: as the value of a pair,
// within an array and within an inline table, and that it is read as it was parsed on its own.
// Fragments ending with a comment are only accepted as the value of a pair.
func FuzzTomlFragment(f *testing.F) {
	for _, seed := range []string{`"s"`, "[1, 2]", "{ a = 1 }", "nan", "[1979-05-27T07:32:00-07:00, 1.0]", "[\n  1, # c\n  2,\n]", "1979-05-27", `"""a"""`, "'b' # c", "1\n# c", "1\nx = 2", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		var want map[string]any
		if toml.Unmarshal([]byte("v = "+text+"\n"), &want) != nil {
			t.Skip("not a valid value")
		}

		handler := NewTomlConfigFileHandler()
		if err := handler.Read(strings.NewReader("a = \"${X}\" # c\nb = [\"${X}\", 1]\nc = { d = \"${X}\" }\n")); err != nil {
			t.Fatal(err)
		}
		entries, _ := handler.Process()
		var accepted []string
		for entry := range entries {
			if _, err := entry.(FragmentConfigEntry).SetFragment(filter.Fragment{Text: text}); err == nil {
				accepted = append(accepted, entry.Path())
			}
		}
		if len(accepted) == 0 {
			t.Skip("rejected")
		}

		var output bytes.Buffer
		if err := handler.Write(&output); err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := toml.Unmarshal(output.Bytes(), &got); err != nil {
			t.Fatalf("the output of fragment %q cannot be read: %v\n%s", text, err, output.String())
		}
		values := map[string]any{"a": got["a"]}
		if b, ok := got["b"].([]any); ok && len(b) == 2 {
			values["b.0"] = b[0]
		}
		if c, ok := got["c"].(map[string]any); ok {
			values["c.d"] = c["d"]
		}
		for _, path := range accepted {
			if !tomlValuesEqual(values[path], want["v"]) {
				t.Fatalf("fragment %q at %s is read differently: %v\n%s", text, path, got, output.String())
			}
		}
	})
}

// tomlValuesEqual returns whether two decoded TOML values are equal, including their types, e.g. 1 and "1" differ.
// NaN equals NaN, and times are equal if they are the same instant with the same offset, since each parsed time
// gets its own location.
func tomlValuesEqual(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || math.IsNaN(x) && math.IsNaN(y))
	case time.Time:
		y, ok := b.(time.Time)
		_, xOffset := x.Zone()
		_, yOffset := y.Zone()
		return ok && x.Equal(y) && xOffset == yOffset
	case []any:
		y, ok := b.([]any)
		return ok && slices.EqualFunc(x, y, tomlValuesEqual)
	case map[string]any:
		y, ok := b.(map[string]any)
		return ok && maps.EqualFunc(x, y, tomlValuesEqual)
	default:
		return reflect.DeepEqual(a, b)
	}
}
