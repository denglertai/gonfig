package benchmarks

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"strings"
	"testing"
	"time"

	"github.com/denglertai/gonfig/internal/file"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/logging"
	"github.com/denglertai/gonfig/internal/schema"
	"github.com/denglertai/gonfig/internal/value"
)

// minFileSize is the minimum size of the generated config files
const minFileSize = 1 << 20

// TestMain uses the log level the CLI uses by default, so disabled debug logging is part of the measurements
func TestMain(m *testing.M) {
	if err := logging.InitLogging("info", "json", false); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// setEnv sets the environment variables referenced by the placeholders of the benchmarks
func setEnv(b *testing.B) {
	b.Setenv("BENCH_HOST", "db.internal.example.com")
	b.Setenv("BENCH_PASSWORD", " s3cr&t<pa$$word> ")
	b.Setenv("BENCH_LIST", "alpha, beta ,gamma,delta , epsilon")
}

// entryGenerator writes the i-th entry of a config file. If placeholders is false, static values are written instead.
type entryGenerator func(buf *bytes.Buffer, i int, placeholders bool)

// pick returns the placeholder or the static value
func pick(placeholders bool, placeholder, static string) string {
	if placeholders {
		return placeholder
	}
	return static
}

func yamlEntry(buf *bytes.Buffer, i int, placeholders bool) {
	fmt.Fprintf(buf, "  service%d:\n", i)
	fmt.Fprintf(buf, "    # Settings of service %d\n", i)
	fmt.Fprintf(buf, "    host: %s\n", pick(placeholders, "${BENCH_HOST}", "db.internal.example.com"))
	fmt.Fprintf(buf, "    port: %d\n", 8000+i%1000)
	fmt.Fprintf(buf, "    password: %s\n", pick(placeholders, "${BENCH_PASSWORD | trim}", "s3cret"))
	fmt.Fprintf(buf, "    url: %s/api/%d\n", pick(placeholders, "https://${BENCH_HOST}", "https://db.internal.example.com"), i)
	fmt.Fprintf(buf, "    tags: [a, b, c]\n")
	fmt.Fprintf(buf, "    description: A static description that is not processed by any filter\n")
}

func jsonEntry(buf *bytes.Buffer, i int, placeholders bool) {
	if i > 0 {
		buf.WriteString(",\n")
	}
	fmt.Fprintf(buf, `    {"name": "service%d", "host": "%s", "port": %d, "password": "%s", "url": "%s/api/%d", "enabled": true, "tags": ["a", "b", "c"], "description": "A static description that is not processed by any filter"}`,
		i, pick(placeholders, "${BENCH_HOST}", "db.internal.example.com"), 8000+i%1000,
		pick(placeholders, "${BENCH_PASSWORD | trim}", "s3cret"), pick(placeholders, "https://${BENCH_HOST}", "https://db.internal.example.com"), i)
}

func xmlEntry(buf *bytes.Buffer, i int, placeholders bool) {
	fmt.Fprintf(buf, "  <!-- Settings of service %d -->\n", i)
	fmt.Fprintf(buf, "  <service name=\"service%d\" host=\"%s\" port=\"%d\">\n", i, pick(placeholders, "${BENCH_HOST}", "db.internal.example.com"), 8000+i%1000)
	fmt.Fprintf(buf, "    <password>%s</password>\n", pick(placeholders, "${BENCH_PASSWORD | trim}", "s3cret"))
	fmt.Fprintf(buf, "    <url>%s/api/%d</url>\n", pick(placeholders, "https://${BENCH_HOST}", "https://db.internal.example.com"), i)
	fmt.Fprintf(buf, "    <description>A static description that is not processed by any filter</description>\n")
	fmt.Fprintf(buf, "  </service>\n")
}

func propertiesEntry(buf *bytes.Buffer, i int, placeholders bool) {
	fmt.Fprintf(buf, "# Settings of service %d\n", i)
	fmt.Fprintf(buf, "service%d.host=%s\n", i, pick(placeholders, "${BENCH_HOST}", "db.internal.example.com"))
	fmt.Fprintf(buf, "service%d.port=%d\n", i, 8000+i%1000)
	fmt.Fprintf(buf, "service%d.password=%s\n", i, pick(placeholders, "${BENCH_PASSWORD | trim}", "s3cret"))
	fmt.Fprintf(buf, "service%d.url=%s/api/%d\n", i, pick(placeholders, "https://${BENCH_HOST}", "https://db.internal.example.com"), i)
	fmt.Fprintf(buf, "service%d.description=A static description that is not processed by any filter\n", i)
}

// tomlEntry writes the i-th service as an element of an array of tables, with a comment and an inline array
func tomlEntry(buf *bytes.Buffer, i int, placeholders bool) {
	fmt.Fprintf(buf, "# Settings of service %d\n[[services]]\n", i)
	fmt.Fprintf(buf, "host = \"%s\"\n", pick(placeholders, "${BENCH_HOST}", "db.internal.example.com"))
	fmt.Fprintf(buf, "port = %d\n", 8000+i%1000)
	fmt.Fprintf(buf, "password = '%s'\n", pick(placeholders, "${BENCH_PASSWORD | trim}", "s3cret"))
	fmt.Fprintf(buf, "url = \"%s/api/%d\"\n", pick(placeholders, "https://${BENCH_HOST}", "https://db.internal.example.com"), i)
	fmt.Fprintf(buf, "tags = [\"a\", \"b\", \"c\"]\n")
	fmt.Fprintf(buf, "description = \"A static description that is not processed by any filter\"\n")
}

func plainEntry(buf *bytes.Buffer, i int, placeholders bool) {
	fmt.Fprintf(buf, "# Settings of service %d\n", i)
	fmt.Fprintf(buf, "export SERVICE%d_URL=\"%s/api/%d\"\n", i, pick(placeholders, "https://${BENCH_HOST}", "https://db.internal.example.com"), i)
	fmt.Fprintf(buf, "export SERVICE%d_PASSWORD='%s'\n", i, pick(placeholders, "${BENCH_PASSWORD | trim}", "s3cret"))
	fmt.Fprintf(buf, "echo \"A static line that is not processed by any filter\"\n")
}

// generateFile writes a config file of at least minFileSize bytes and returns its path
func generateFile(b *testing.B, fileType general.FileType, entry entryGenerator, placeholders bool) string {
	var buf bytes.Buffer
	switch fileType {
	case general.YAML:
		buf.WriteString("services:\n")
	case general.JSON:
		buf.WriteString("{\n  \"services\": [\n")
	case general.XML:
		buf.WriteString("<?xml version=\"1.0\"?>\n<services>\n")
	}

	for i := 0; buf.Len() < minFileSize; i++ {
		entry(&buf, i, placeholders)
	}

	switch fileType {
	case general.JSON:
		buf.WriteString("\n  ]\n}\n")
	case general.XML:
		buf.WriteString("</services>\n")
	}

	path := filepath.Join(b.TempDir(), "config."+string(fileType))
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

// heapSampler records the peak size of the heap while a benchmark runs.
// The runtime does not track the peak itself, so the heap size is sampled periodically.
type heapSampler struct {
	stop chan struct{}
	done chan struct{}
	peak uint64
}

// startHeapSampler starts sampling the heap size every millisecond
func startHeapSampler() *heapSampler {
	s := &heapSampler{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}

	// Start from the live heap, without garbage left over from preparing the benchmark
	runtime.GC()

	go func() {
		defer close(s.done)

		sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()

		for {
			metrics.Read(sample)
			s.peak = max(s.peak, sample[0].Value.Uint64())

			select {
			case <-s.stop:
				return
			case <-ticker.C:
			}
		}
	}()

	return s
}

// Stop stops sampling and reports the peak heap size in MiB as the metric "peak-heap-MiB"
func (s *heapSampler) Stop(b *testing.B) {
	close(s.stop)
	<-s.done
	b.ReportMetric(float64(s.peak)/(1<<20), "peak-heap-MiB")
}

// BenchmarkProcessFile processes config files of at least 1 MiB per file type.
// The "static" variants contain no placeholders and measure reading, parsing and writing only.
// Besides time and allocations, the peak heap size is reported, which drives the memory usage of the process.
func BenchmarkProcessFile(b *testing.B) {
	fileTypes := []struct {
		fileType general.FileType
		entry    entryGenerator
	}{
		{general.YAML, yamlEntry},
		{general.JSON, jsonEntry},
		{general.XML, xmlEntry},
		{general.PROPERTIES, propertiesEntry},
		{general.TOML, tomlEntry},
		{general.PLAIN, plainEntry},
	}

	for _, ft := range fileTypes {
		for _, placeholders := range []bool{false, true} {
			name := fmt.Sprintf("%s/static", ft.fileType)
			if placeholders {
				name = fmt.Sprintf("%s/placeholders", ft.fileType)
			}

			b.Run(name, func(b *testing.B) {
				setEnv(b)
				path := generateFile(b, ft.fileType, ft.entry, placeholders)
				info, err := os.Stat(path)
				if err != nil {
					b.Fatal(err)
				}

				b.SetBytes(info.Size())
				b.ReportAllocs()
				sampler := startHeapSampler()
				for b.Loop() {
					if err := file.NewFileProcessor(path, ft.fileType, io.Discard).Process(); err != nil {
						b.Fatal(err)
					}
				}
				sampler.Stop(b)
			})
		}
	}
}

// BenchmarkProcessValue processes single values, covering the tokenizer and the filter chain
func BenchmarkProcessValue(b *testing.B) {
	many := strings.Repeat("https://${BENCH_HOST}/api?password=${BENCH_PASSWORD | trim | base64}&", 50)

	values := []struct {
		name  string
		value string
	}{
		{"no_placeholder", "https://db.internal.example.com/api?param1=value&param2=something"},
		{"single_placeholder", "${BENCH_HOST}"},
		{"embedded_placeholders", "https://${BENCH_HOST}/api?param1=value&param2=${BENCH_HOST}"},
		{"string_filter_chain", "${BENCH_PASSWORD | trim | upper | lower | replace(a,b) | base64}"},
		{"list_filter_chain", "${BENCH_LIST | split | trim | upper | join(;)}"},
		{"regex_filter", `${BENCH_HOST | regex(^\([a-z]+\)\..*,\1)}`},
		{"hash_filter", "${BENCH_PASSWORD | md5}"},
		{"100_placeholders", many},
	}

	for _, v := range values {
		b.Run(v.name, func(b *testing.B) {
			setEnv(b)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := value.ProcessValue(v.value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// fragmentMapping returns a YAML or JSON mapping with n keys named prefix0 to prefixN, each holding a nested mapping,
// or n XML elements
func fragmentMapping(fileType general.FileType, n int, prefix string) string {
	entries := make([]string, n)
	for i := range entries {
		switch fileType {
		case general.JSON:
			entries[i] = fmt.Sprintf(`"%s%d": {"value": %d}`, prefix, i, i)
		case general.XML:
			entries[i] = fmt.Sprintf(`<%s id="%d"><value>%d</value></%s>`, prefix, i, i, prefix)
		default:
			entries[i] = fmt.Sprintf("%s%d:\n  value: %d", prefix, i, i)
		}
	}

	switch fileType {
	case general.JSON:
		return strings.Join(entries, ", ")
	case general.XML:
		return strings.Join(entries, "")
	default:
		return strings.Join(entries, "\n")
	}
}

// BenchmarkProcessFragment inserts fragments with n keys into YAML and JSON files, or n elements into XML files.
// The "value" variants replace a value or the content of an XML element by the fragment, the "merge" variants merge
// the fragment into a mapping with n keys, of which half are overwritten by the fragment and half are added.
func BenchmarkProcessFragment(b *testing.B) {
	for _, fileType := range []general.FileType{general.YAML, general.JSON, general.XML} {
		for _, mode := range []string{"value", "merge"} {
			// XML only supports fragments as the content of elements
			if fileType == general.XML && mode == "merge" {
				continue
			}

			for _, n := range []int{1000, 10000} {
				b.Run(fmt.Sprintf("%s/%s/%d", fileType, mode, n), func(b *testing.B) {
					fragment := fragmentMapping(fileType, n/2, "key") + "\n" + fragmentMapping(fileType, n/2, "new")
					content := "root: ${BENCH_FRAGMENT | fragment}\n"
					if mode == "merge" {
						target := strings.ReplaceAll(fragmentMapping(fileType, n, "key"), "\n", "\n  ")
						content = "root:\n  " + target + "\n  ${BENCH_FRAGMENT | fragment}:\n"
					}

					if fileType == general.XML {
						fragment = fragmentMapping(fileType, n, "entry")
						content = "<root><entries>${BENCH_FRAGMENT | fragment}</entries></root>"
					}

					if fileType == general.JSON {
						fragment = "{" + strings.ReplaceAll(fragment, "\n", ", ") + "}"
						content = `{"root": "${BENCH_FRAGMENT | fragment}"}`
						if mode == "merge" {
							content = `{"root": {` + fragmentMapping(fileType, n, "key") + `, "${BENCH_FRAGMENT | fragment}": null}}`
						}
					}
					b.Setenv("BENCH_FRAGMENT", fragment)

					path := filepath.Join(b.TempDir(), "config."+string(fileType))
					if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
						b.Fatal(err)
					}

					b.ReportAllocs()
					sampler := startHeapSampler()
					for b.Loop() {
						if err := file.NewFileProcessor(path, fileType, io.Discard).Process(); err != nil {
							b.Fatal(err)
						}
					}
					sampler.Stop(b)
				})
			}
		}
	}
}

// BenchmarkProcessYAMLScaling processes YAML files with n service entries (1k, 4k and 16k) with and without
// placeholders. 4x the entries should take about 4x the time (linear), about 16x is quadratic.
func BenchmarkProcessYAMLScaling(b *testing.B) {
	for _, placeholders := range []bool{false, true} {
		kind := "static"
		if placeholders {
			kind = "placeholders"
		}

		for _, n := range []int{1000, 4000, 16000} {
			b.Run(fmt.Sprintf("%s/%d", kind, n), func(b *testing.B) {
				setEnv(b)

				var buf bytes.Buffer
				buf.WriteString("services:\n")
				for i := range n {
					yamlEntry(&buf, i, placeholders)
				}

				path := filepath.Join(b.TempDir(), "config.yaml")
				if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
					b.Fatal(err)
				}

				b.SetBytes(int64(buf.Len()))
				b.ReportAllocs()
				sampler := startHeapSampler()
				for b.Loop() {
					if err := file.NewFileProcessor(path, general.YAML, io.Discard).Process(); err != nil {
						b.Fatal(err)
					}
				}
				sampler.Stop(b)
			})
		}
	}
}

// leafSchema accepts objects, arrays and integers at every level, so every string leaf is a violation
const leafSchema = `{"$id": "leaf", "type": ["object", "array", "integer"], "additionalProperties": {"$ref": "#"}, "items": {"$ref": "#"}}`

// keyScalingFormats are the formats of BenchmarkProcessKeysScaling, each generating n entries with keys that contain
// placeholders. Every entry has a placeholder key with a string leaf, which is a violation of leafSchema.
var keyScalingFormats = []struct {
	name     string
	fileType general.FileType
	generate func(buf *bytes.Buffer, n int)
}{
	{"yaml/keys", general.YAML, func(buf *bytes.Buffer, n int) {
		for i := range n {
			fmt.Fprintf(buf, "svc${BENCH_ID}_%d:\n  host: ${BENCH_HOST}\n  \"opt_${BENCH_ID}\": x\n  port: %d\n", i, i)
		}
	}},
	{"yaml/anchors", general.YAML, func(buf *bytes.Buffer, n int) {
		// Every entry merges its own anchor, which holds placeholder keys
		for i := range n {
			fmt.Fprintf(buf, "base%d: &a%d\n  \"k${BENCH_ID}_%d\": x\n  host: ${BENCH_HOST}\n", i, i, i)
			fmt.Fprintf(buf, "svc%d:\n  <<: *a%d\n  name: n%d\n", i, i, i)
		}
	}},
	{"yaml/shared-anchor", general.YAML, func(buf *bytes.Buffer, n int) {
		// One anchor with placeholder keys is aliased and merged by every entry
		buf.WriteString("common: &common\n")
		for i := range 10 {
			fmt.Fprintf(buf, "  \"c${BENCH_ID}_%d\": x\n", i)
		}
		for i := range n {
			fmt.Fprintf(buf, "svc%d:\n  <<: *common\n  alias: *common\n  \"own${BENCH_ID}\": x\n", i)
		}
	}},
	{"json/keys", general.JSON, func(buf *bytes.Buffer, n int) {
		buf.WriteString("{\n")
		for i := range n {
			if i > 0 {
				buf.WriteString(",\n")
			}
			fmt.Fprintf(buf, `  "svc${BENCH_ID}_%d": {"host": "${BENCH_HOST}", "opt_${BENCH_ID}": "x", "port": %d}`, i, i)
		}
		buf.WriteString("\n}\n")
	}},
	{"toml/dotted", general.TOML, func(buf *bytes.Buffer, n int) {
		for i := range n {
			fmt.Fprintf(buf, "\"svc${BENCH_ID}_%d\".host = \"${BENCH_HOST}\"\n\"svc${BENCH_ID}_%d\".\"opt_${BENCH_ID}\" = \"x\"\n", i, i)
		}
	}},
	{"toml/tables", general.TOML, func(buf *bytes.Buffer, n int) {
		for i := range n {
			fmt.Fprintf(buf, "[\"svc${BENCH_ID}_%d\"]\nhost = \"${BENCH_HOST}\"\n\"opt_${BENCH_ID}\" = \"x\"\n", i)
		}
	}},
	{"toml/inline", general.TOML, func(buf *bytes.Buffer, n int) {
		for i := range n {
			fmt.Fprintf(buf, "svc%d = { \"k${BENCH_ID}\" = \"x\", host = \"${BENCH_HOST}\", n = { \"i${BENCH_ID}\" = \"x\" } }\n", i)
		}
	}},
}

// BenchmarkProcessKeysScaling processes files with n entries (1k, 4k and 16k) whose keys contain placeholders.
// The "process" variants only process the file, the "schema" variants also validate the result against a schema that
// every entry violates and redact the keys of the violations like config process --schema does.
// 4x the entries should take about 4x the time (linear), about 16x is quadratic.
func BenchmarkProcessKeysScaling(b *testing.B) {
	for _, f := range keyScalingFormats {
		for _, withSchema := range []bool{false, true} {
			mode := "process"
			if withSchema {
				mode = "schema"
			}

			for _, n := range []int{1000, 4000, 16000} {
				b.Run(fmt.Sprintf("%s/%s/%d", f.name, mode, n), func(b *testing.B) {
					if withSchema && f.name == "yaml/shared-anchor" && n == 16000 {
						// The aliases expand to more than yaml's decoder accepts, so validation fails with
						// "invalid YAML" before any key is redacted, on main as well
						b.Skip("the expanded document exceeds the alias limit of the YAML decoder")
					}
					setEnv(b)
					b.Setenv("BENCH_ID", "id-s3cret")

					var buf bytes.Buffer
					f.generate(&buf, n)
					dir := b.TempDir()
					path := filepath.Join(dir, "config."+string(f.fileType))
					if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
						b.Fatal(err)
					}

					var sch *schema.Schema
					if withSchema {
						schemaPath := filepath.Join(dir, "leaf.json")
						if err := os.WriteFile(schemaPath, []byte(leafSchema), 0o600); err != nil {
							b.Fatal(err)
						}
						var err error
						if sch, err = schema.Load(schemaPath); err != nil {
							b.Fatal(err)
						}
					}

					b.SetBytes(int64(buf.Len()))
					b.ReportAllocs()
					sampler := startHeapSampler()
					for b.Loop() {
						var out bytes.Buffer
						processor := file.NewFileProcessor(path, f.fileType, &out)
						// Keys are only recorded for redaction if the output is validated, like config process --schema
						processor.Validated = withSchema
						if err := processor.Process(); err != nil {
							b.Fatal(err)
						}
						if !withSchema {
							continue
						}
						violations, err := sch.Validate(f.fileType, out.Bytes(), processor.RedactKeys)
						if err != nil {
							b.Fatal(err)
						}
						if len(violations) < n {
							b.Fatalf("got %d violations, want at least %d", len(violations), n)
						}
					}
					sampler.Stop(b)
				})
			}
		}
	}
}

// BenchmarkProcessYAMLFragmentAnchorsScaling inserts a small fragment with an anchor and an alias into each of n
// entries (1k, 4k and 16k). Every entry has an anchor and an alias of its own. In the "same" variant all fragments use
// the same anchor name, so every insert but the first renames it (k_1, k_2, ...), in the "distinct" variant no anchor
// collides. 4x the entries should take about 4x the time (linear), about 16x is quadratic.
func BenchmarkProcessYAMLFragmentAnchorsScaling(b *testing.B) {
	for _, mode := range []string{"same", "distinct"} {
		for _, n := range []int{1000, 4000, 16000} {
			b.Run(fmt.Sprintf("%s/%d", mode, n), func(b *testing.B) {
				b.Setenv("BENCH_FRAGMENT", "x: &k made-up-x\ny: *k\nz:\n  - 1\n  - 2")

				var buf bytes.Buffer
				for i := range n {
					fmt.Fprintf(&buf, "a%d: &own%d ${BENCH_FRAGMENT | fragment}\nb%d: *own%d\n", i, i, i, i)
				}
				content := buf.String()
				if mode == "distinct" {
					buf.Reset()
					for i := range n {
						b.Setenv(fmt.Sprintf("BENCH_FRAGMENT_%d", i), fmt.Sprintf("x: &k%d made-up-x\ny: *k%d\nz:\n  - 1\n  - 2", i, i))
						fmt.Fprintf(&buf, "a%d: &own%d ${BENCH_FRAGMENT_%d | fragment}\nb%d: *own%d\n", i, i, i, i, i)
					}
					content = buf.String()
				}

				path := filepath.Join(b.TempDir(), "config.yaml")
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				for b.Loop() {
					if err := file.NewFileProcessor(path, general.YAML, io.Discard).Process(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// checkScalingFormats are the formats of BenchmarkCheckScaling, each generating n entries with placeholders
var checkScalingFormats = []struct {
	name     string
	fileType general.FileType
	generate func(buf *bytes.Buffer, n int)
}{
	{"yaml", general.YAML, func(buf *bytes.Buffer, n int) {
		buf.WriteString("services:\n")
		for i := range n {
			yamlEntry(buf, i, true)
		}
	}},
	{"toml/tables", general.TOML, func(buf *bytes.Buffer, n int) {
		// An array of tables, which does not trigger the quadratic behavior of the TOML library
		for i := range n {
			fmt.Fprintf(buf, "[[services]]\n# Settings of service %d\nhost = \"${BENCH_HOST}\"\nport = %d\npassword = \"${BENCH_PASSWORD | trim}\"\n", i, i)
		}
	}},
	{"toml/inline", general.TOML, func(buf *bytes.Buffer, n int) {
		for i := range n {
			fmt.Fprintf(buf, "svc%d = { host = \"${BENCH_HOST}\", port = %d }\n", i, i)
		}
	}},
	{"toml/keys", general.TOML, func(buf *bytes.Buffer, n int) {
		// Placeholder keys are masked as "***" by check, so the masked document has duplicate keys (distinctKeysDocument)
		for i := range n {
			fmt.Fprintf(buf, "[[services]]\n\"opt_${BENCH_ID}\" = \"x\"\n\"${BENCH_ID}\" = %d\n", i)
		}
	}},
	{"toml/keys-inline", general.TOML, func(buf *bytes.Buffer, n int) {
		for i := range n {
			fmt.Fprintf(buf, "svc%d = { \"k${BENCH_ID}\" = \"x\", \"${BENCH_ID}\" = %d }\n", i, i)
		}
	}},
	{"json", general.JSON, func(buf *bytes.Buffer, n int) {
		buf.WriteString("{\"services\": [\n")
		for i := range n {
			if i > 0 {
				buf.WriteString(",\n")
			}
			fmt.Fprintf(buf, `{"host": "${BENCH_HOST}", "port": %d, "password": "${BENCH_PASSWORD | trim}"}`, i)
		}
		buf.WriteString("]}\n")
	}},
}

// BenchmarkCheckScaling runs config check (FileProcessor.Check) without --diff on files with n entries (1k, 4k and 16k)
// with placeholders. Check writes the file into nothing, with values masked. 4x the entries should take about 4x the time (linear), about 16x is quadratic.
func BenchmarkCheckScaling(b *testing.B) {
	for _, f := range checkScalingFormats {
		for _, n := range []int{1000, 4000, 16000} {
			b.Run(fmt.Sprintf("%s/%d", f.name, n), func(b *testing.B) {
				setEnv(b)
				b.Setenv("BENCH_ID", "id-s3cret")

				var buf bytes.Buffer
				f.generate(&buf, n)
				path := filepath.Join(b.TempDir(), "config."+string(f.fileType))
				if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
					b.Fatal(err)
				}

				b.SetBytes(int64(buf.Len()))
				b.ReportAllocs()
				for b.Loop() {
					report, err := file.NewFileProcessor(path, f.fileType, io.Discard).Check()
					if err != nil {
						b.Fatal(err)
					}
					if report.Error != "" || report.Errors() > 0 {
						b.Fatalf("check failed: %s", report.Error)
					}
				}
			})
		}
	}
}

// BenchmarkCheckFragmentScaling runs FileProcessor.Check without a diff on files with n fragments (1k, 4k and 16k), whose
// comments are removed (TOML, YAML) while values are masked. 4x the entries should take about 4x the time (linear).
func BenchmarkCheckFragmentScaling(b *testing.B) {
	formats := []struct {
		name     string
		fileType general.FileType
		fragment string
		line     string
	}{
		{"yaml", general.YAML, "# managed\nx: &k made-up-x # c\ny: *k\n", "a%d: &own%d ${BENCH_FRAGMENT | fragment}\nb%d: *own%d\n"},
		{"toml", general.TOML, "[\n  1, # first\n  2, # second\n]", "k%d = \"${BENCH_FRAGMENT | fragment}\"\n"},
	}
	for _, f := range formats {
		for _, n := range []int{1000, 4000, 16000} {
			b.Run(fmt.Sprintf("%s/%d", f.name, n), func(b *testing.B) {
				b.Setenv("BENCH_FRAGMENT", f.fragment)

				var buf bytes.Buffer
				for i := range n {
					if f.fileType == general.YAML {
						fmt.Fprintf(&buf, f.line, i, i, i, i)
					} else {
						fmt.Fprintf(&buf, f.line, i)
					}
				}
				path := filepath.Join(b.TempDir(), "config."+string(f.fileType))
				if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				for b.Loop() {
					report, err := file.NewFileProcessor(path, f.fileType, io.Discard).Check()
					if err != nil {
						b.Fatal(err)
					}
					if report.Error != "" || report.Errors() > 0 {
						b.Fatalf("check failed: %s %v", report.Error, report.Entries[0])
					}
				}
			})
		}
	}
}
