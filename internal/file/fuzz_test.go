package file

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// FuzzPlain checks processing plain text files, see fuzzHandler
func FuzzPlain(f *testing.F) {
	fuzzHandler(f, general.PLAIN, "plain", func(value string) string {
		return "line " + value + "\n" + value + "\n"
	})
}
