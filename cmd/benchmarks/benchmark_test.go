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
	"github.com/denglertai/gonfig/internal/value"
)

// minFileSize is the minimum size of the generated config files
const minFileSize = 1 << 20

// TestMain uses the log level the CLI uses by default, so disabled debug logging is part of the measurements
func TestMain(m *testing.M) {
	if err := logging.InitLogging("info", false); err != nil {
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
