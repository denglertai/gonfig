package file

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	"go.yaml.in/yaml/v4"

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

// FuzzDotenv checks processing dotenv files, see fuzzHandler
func FuzzDotenv(f *testing.F) {
	fuzzHandler(f, general.ENV, "env", func(value string) string {
		return "# comment\nexport A=" + value + " # c\nB=\"" + value + "\"\nC='" + value + "'\n"
	})
}

// FuzzDotenvRoundTrip checks that values written to dotenv files are read as they were, whatever quotes the value had
func FuzzDotenvRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "'", "\"", "\\", "a #b", " lead", "a\nb\r\n", "\\n", "#", "x'y\"z"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		for _, quote := range []byte{0, '\'', '"'} {
			encoded, err := encodeDotenv(&LineConfigEntry{val: text}, quote)
			if err != nil {
				t.Fatal(err)
			}
			lines, err := parseDotenvLines("A=" + encoded + " # c\n")
			if err != nil {
				t.Fatalf("%q written as %s cannot be read: %v", text, encoded, err)
			}
			if lines[0].entry.val != text {
				t.Fatalf("%q written as %s is read as %q", text, encoded, lines[0].entry.val)
			}
		}
	})
}

// FuzzIni checks processing INI files, see fuzzHandler
func FuzzIni(f *testing.F) {
	fuzzHandler(f, general.INI, "ini", func(value string) string {
		return "; comment\na = " + value + "\n[s]\nb = \"" + value + "\" ; c\n"
	})
}

// FuzzIniRoundTrip checks that values written to double-quoted INI values are read as they were
func FuzzIniRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "\"", "\\", "a ; b", " lead ", "a\nb\r\n", "\\n", "\\\""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		encoded, err := encodeIni(&LineConfigEntry{val: text}, '"')
		if err != nil {
			t.Fatal(err)
		}
		lines, err := parseIniLines("[s]\na = " + encoded + " ; c\n")
		if err != nil {
			t.Fatalf("%q written as %s cannot be read: %v", text, encoded, err)
		}
		if lines[1].entry == nil || lines[1].entry.val != text {
			t.Fatalf("%q written as %s is read as %+v", text, encoded, lines[1].entry)
		}
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

// FuzzTomlKeyFragment checks that a fragment used as a key is merged into its table as a shallow override: the pairs
// of the fragment replace the pairs with the same key and are added otherwise, and the rest of the document is kept
func FuzzTomlKeyFragment(f *testing.F) {
	for _, seed := range []string{"a = 2", "b = { y = 2 }", "c = [1, # c\n  2]\nd.e = 'x'", "z = 1979-05-27 # c", "'a' = 3\n\"q\" = 4",
		"b.y = 1", "[t2]\nx = 1", "a = 1\na = 2", ""} {
		f.Add(seed)
	}
	const document = "[t]\na = 1 # c\nb = { x = 1 }\n  \"${X}\" = \"\" # c\nz = \"s\"\n[u]\nv = 1\n"
	f.Fuzz(func(t *testing.T, text string) {
		var fragment map[string]any
		if toml.Unmarshal([]byte(text), &fragment) != nil {
			t.Skip("not a valid document")
		}

		handler := NewTomlConfigFileHandler()
		if err := handler.Read(strings.NewReader(document)); err != nil {
			t.Fatal(err)
		}
		entries, _ := handler.Process()
		for entry := range entries {
			if entry.(*TomlConfigEntry).isKey {
				if _, err := entry.(FragmentConfigEntry).SetFragment(filter.Fragment{Text: text}); err != nil {
					t.Skip("rejected")
				}
			}
		}

		var output bytes.Buffer
		if err := handler.Write(&output); err != nil {
			t.Fatalf("fragment %q: %v", text, err)
		}
		var got map[string]any
		if err := toml.Unmarshal(output.Bytes(), &got); err != nil {
			t.Fatalf("the output of fragment %q cannot be read: %v\n%s", text, err, output.String())
		}
		var want map[string]any
		if err := toml.Unmarshal([]byte(strings.Replace(document, "  \"${X}\" = \"\" # c\n", "", 1)), &want); err != nil {
			t.Fatal(err)
		}
		table := want["t"].(map[string]any)
		maps.Copy(table, fragment)
		if !tomlValuesEqual(got, want) {
			t.Fatalf("fragment %q is merged as %v, want %v\n%s", text, got, want, output.String())
		}
	})
}

// FuzzTomlComments checks that removing the comments found by tomlComments from valid TOML, as a document and as the
// value of a pair, leaves valid TOML that is read as the original, so no # within a string is taken for a comment and
// no comment is left behind
func FuzzTomlComments(f *testing.F) {
	for _, seed := range []string{"a = 1 # c", "a = 1#c", "a = \"x#y\" # c", "a = 'x#y'#c", "a = \"\"\"x\\\"\"\"#\"\"\" # c",
		"a = \"\"\"\"\"\"\"#\"\"\"", "a = '''x#''''' # c", "a = [\n  1, # c\n  \"#\", #d\n]\r\n# e\r\nb = { c = '#' } # f", "# only",
		"a = \"\\\\\" # c", "a = \"\"\"\\\n  #\"\"\"", "# \"\n'#'=1", "a = \"\"\"\"\"\"\" # c"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		for _, document := range []string{text, "v = " + text + "\n"} {
			var want map[string]any
			if toml.Unmarshal([]byte(document), &want) != nil {
				continue
			}
			result := []byte(document)
			comments := tomlComments(result)
			for i := len(comments) - 1; i >= 0; i-- {
				result = slices.Delete(result, comments[i].start, comments[i].end)
			}
			var got map[string]any
			if err := toml.Unmarshal(result, &got); err != nil {
				t.Fatalf("%q without its comments is %q, which cannot be read: %v", document, result, err)
			}
			if !tomlValuesEqual(got, want) {
				t.Fatalf("%q without its comments is %q, which is read as %v, want %v", document, result, got, want)
			}
		}
	})
}

// FuzzJsonKeyFragment checks that a fragment used as a key is deep merged into its object, also when it replaces the
// values of later keys containing placeholders, which are matched as written and keep their rename. The keys of the
// fragment are not processed, and fragments with duplicate keys or keys equal to the renamed ones are skipped, since
// decoding into maps cannot express them.
func FuzzJsonKeyFragment(f *testing.F) {
	for _, seed := range []string{`{"a": 2}`, `{"${P}": {"y": 2}}`, `{"${P}": 2}`, `{"b": {"${P}": 3, "z": [1]}}`, `{"b": 1}`,
		`{"${X}": 1, "n": null}`, `{"a": {"deep": {"er": true}}}`, `{}`, ""} {
		f.Add(seed)
	}
	const document = `{"t": {"a": 1, "${X}": null, "${P}": 1, "b": {"x": 1, "${P}": 2}, "z": "s"}, "u": {"v": 1}}`
	const renamed = "made-up-p"
	f.Fuzz(func(t *testing.T, text string) {
		var fragment map[string]any
		if strings.TrimSpace(text) != "" {
			node, err := parseJsonFragment(text)
			if err != nil || node.kind != jsonObject || jsonHasDuplicateKeys(node) || decodeJsonNumbers(text, &fragment) != nil {
				t.Skip("not an object with unique keys")
			}
		}

		handler := NewJsonConfigFileHandler()
		if err := handler.Read(strings.NewReader(document)); err != nil {
			t.Fatal(err)
		}
		entries, _ := handler.Process()
		for entry := range entries {
			switch jce := entry.(*JsonConfigEntry); {
			case jce.isKey && jce.key == "${X}":
				if _, err := jce.SetFragment(filter.Fragment{Text: text}); err != nil {
					t.Fatalf("fragment %q: %v", text, err)
				}
			case jce.isKey:
				jce.SetValue(renamed)
			}
		}

		var output bytes.Buffer
		if err := handler.Write(&output); err != nil {
			t.Fatalf("fragment %q: %v", text, err)
		}
		var got map[string]any
		if err := decodeJsonNumbers(output.String(), &got); err != nil {
			t.Fatalf("the output of fragment %q cannot be read: %v\n%s", text, err, output.String())
		}

		var want map[string]any
		if err := decodeJsonNumbers(strings.Replace(document, `"${X}": null, `, "", 1), &want); err != nil {
			t.Fatal(err)
		}
		table := want["t"].(map[string]any)
		deepMergeJson(table, fragment)
		for _, object := range []any{table, table["b"]} {
			if object, ok := object.(map[string]any); ok {
				if _, found := object[renamed]; found {
					t.Skip("the fragment contains a renamed key")
				}
				if value, found := object["${P}"]; found {
					delete(object, "${P}")
					object[renamed] = value
				}
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("fragment %q is merged as %v, want %v\n%s", text, got, want, output.String())
		}
	})
}

// jsonHasDuplicateKeys returns whether an object of the node contains a key twice, which a map cannot represent
func jsonHasDuplicateKeys(node *jsonNode) bool {
	if node.kind == jsonObject && len(slices.Compact(slices.Sorted(slices.Values(node.keys)))) != len(node.keys) {
		return true
	}
	return slices.ContainsFunc(node.children, jsonHasDuplicateKeys)
}

// decodeJsonNumbers decodes the JSON text into target keeping numbers as written, like the JSON handler
func decodeJsonNumbers(text string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	return decoder.Decode(target)
}

// deepMergeJson merges the source into the target like mergeObjects: objects are merged recursively, and other values
// of the source replace the ones of the target
func deepMergeJson(target, source map[string]any) {
	for key, value := range source {
		targetObject, targetIsObject := target[key].(map[string]any)
		sourceObject, sourceIsObject := value.(map[string]any)
		if targetIsObject && sourceIsObject {
			deepMergeJson(targetObject, sourceObject)
			continue
		}
		target[key] = value
	}
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

// FuzzYamlFragmentAnchors checks that inserting any fragment into anchored values and keys of YAML documents with
// aliases of them never panics, and that the output is either rejected by Write or decodes as YAML, so no alias is
// left without its anchor and no merge key has a value other than mappings
func FuzzYamlFragmentAnchors(f *testing.F) {
	for _, seed := range []string{"s", "x: 1", "[1, 2]", "&k s", "&k {x: &k 1, y: *k}", "x: &k 1\ny: *k", "&k_1 [1]", "a: &k 1\nb: &k_1 2\nc: *k",
		"*k", "<<: *k", "a: &m {y: 1}\nb: *m", "{}", "[]", "", "# c", "a: &k 1\n---\nb: &k 2", "<<: [{x: 1}]", "- &k a\n- *k", "a: &k\n  b: *k"} {
		for variant := range 4 {
			f.Add(uint8(variant), seed)
		}
	}
	documents := []string{
		// the value of an anchored mapping entry, with aliases as value, key and merge value
		"a: &k ${X}\nb: *k\nm: {*k : 1}\nn:\n  <<: *k\nl: [*k]\n---\nc: &k ${X}\nd: *k\n",
		// a key overriding anchored values, and merge keys with lists
		"a: &k {o: 1}\nm: &m {p: 1}\n${X}:\nb: *k\nn:\n  <<: [*m, *k]\n  q: *m\n",
		// an anchored key and value of the fragment key and aliases before and after a fragment
		"a: &k ${X}\nb: &k_1 ${X}\nc: *k\nd:\n  <<: [*k_1, *k]\n---\ne: &k ${X}\nf: *k\n",
		// a fragment in a sequence and as a merge value
		"- &k ${X}\n- *k\n- <<: ${X}\n  y: *k\n- <<: [*k]\n",
	}
	f.Fuzz(func(t *testing.T, variant uint8, text string) {
		handler := NewYamlConfigFileHandler()
		if err := handler.Read(strings.NewReader(documents[int(variant)%len(documents)])); err != nil {
			t.Fatal(err)
		}
		// Processing stops at the first fragment that cannot be inserted, so nothing is written then, like a placeholder
		// left in a merge key of the input, which is written as read
		entries, _ := handler.Process()
		for entry := range entries {
			if !strings.Contains(entry.GetValue(), "${X}") {
				continue
			}
			if _, err := entry.(FragmentConfigEntry).SetFragment(filter.Fragment{Text: text}); err != nil {
				t.Skip("rejected")
			}
		}

		// Write only rejects the documents with the problems its checks are for, so it doesn't reject too much
		var output bytes.Buffer
		if err := handler.Write(&output); err != nil {
			if !slices.ContainsFunc([]string{"not defined before it", "merge key", "not valid UTF-8"},
				func(known string) bool { return strings.Contains(err.Error(), known) }) {
				t.Fatalf("unexpected error writing fragment %q: %v", text, err)
			}
			return
		}
		decoder := yaml.NewDecoder(bytes.NewReader(output.Bytes()))
		for {
			var document any
			err := decoder.Decode(&document)
			if errors.Is(err, io.EOF) {
				return
			}
			// An alias of a mapping or sequence used as a key, an anchor containing an alias of itself and duplicate keys
			// are accepted by the parser, but not by the decoder, no matter whether they come from a fragment or the input
			if err != nil && !slices.ContainsFunc([]string{"as a map key", "contains itself", "already defined"},
				func(known string) bool { return strings.Contains(err.Error(), known) }) {
				t.Fatalf("the output of fragment %q cannot be read: %v\n%s", text, err, output.String())
			}
		}
	})
}
