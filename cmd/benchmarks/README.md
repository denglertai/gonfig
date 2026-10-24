# Benchmarks

Benchmarks for processing config files and single values.

| Benchmark | Measures |
| --- | --- |
| `BenchmarkProcessFile/<type>/static` | Reading, parsing and writing a config file of at least 1 MiB without placeholders |
| `BenchmarkProcessFile/<type>/placeholders` | The same file with three placeholders per entry, e.g. `${BENCH_PASSWORD \| trim}` |
| `BenchmarkProcessValue/<case>` | Finding placeholders and executing filter chains for a single value |
| `BenchmarkProcessFragment/<type>/<mode>/<n>` | Inserting a [fragment](../../FILTERS.md#fragments) with `n` keys or XML elements as a value (`value`), or merging it into a mapping with `n` keys (`merge`) |

The difference between `static` and `placeholders` shows the cost of the placeholders compared to reading and writing the file.

All benchmarks report allocations (`B/op` and `allocs/op`), which drive the work of the garbage collector.
The file benchmarks also report the peak size of the heap (`peak-heap-MiB`), which drives the memory usage of the process.
The runtime does not track the peak heap size, so it is sampled every millisecond.
It is therefore less precise for short operations, e.g. ±20% for plain text files.

## Running

```console
$ make bench
$ make bench BENCH=ProcessFile/yaml
```

Or with `go test` directly:

```console
$ go test -run '^$' -bench . -benchmem ./cmd/benchmarks
```

Compare two runs with [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat). Run each side at least 6 times:

```console
$ go test -run '^$' -bench . -benchmem -count=6 ./cmd/benchmarks > old.txt
$ # apply changes
$ go test -run '^$' -bench . -benchmem -count=6 ./cmd/benchmarks > new.txt
$ go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
```

## Profiling

`make profile` writes a CPU and a memory profile and opens the CPU profile in the pprof web UI:

```console
$ make profile BENCH=ProcessFile/xml
```

Or with `go test` and `go tool pprof` directly:

```console
$ go test -run '^$' -bench ProcessFile/xml -benchmem -cpuprofile cpu.out -memprofile mem.out -o benchmarks.test ./cmd/benchmarks
$ go tool pprof -top -cum benchmarks.test cpu.out
$ go tool pprof -top -sample_index=alloc_space benchmarks.test mem.out
```

## Baseline

Measured on an AMD Ryzen 7 5800X3D with Go 1.27.2 on Linux, 6 runs each, median values.
"Before" is the state before [#165](https://github.com/denglertai/gonfig/issues/165), "after" includes the optimizations described below.

### Files (≥ 1 MiB)

| Benchmark | Time before | Time after | Allocated before | Allocated after | Allocations before | Allocations after |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| yaml/static | 178.8 ms | 129.2 ms | 161.2 MiB | 128.0 MiB | 1,108.8k | 574.7k |
| yaml/placeholders | 182.6 ms | 141.6 ms | 170.2 MiB | 137.6 MiB | 1,321.5k | 807.6k |
| json/static | 61.7 ms | 34.9 ms | 48.8 MiB | 27.2 MiB | 843.1k | 501.9k |
| json/placeholders | 72.4 ms | 51.1 ms | 61.4 MiB | 40.5 MiB | 1,055.1k | 735.0k |
| xml/static | 58.0 ms | 30.8 ms | 31.7 MiB | 13.3 MiB | 696.7k | 292.1k |
| xml/placeholders | 67.2 ms | 41.8 ms | 39.2 MiB | 21.5 MiB | 885.7k | 499.5k |
| properties/static | 92.7 ms | 81.2 ms | 44.8 MiB | 33.2 MiB | 2,079k | 1,894k |
| properties/placeholders | 104.7 ms | 96.3 ms | 54.5 MiB | 43.7 MiB | 2,383k | 2,220k |
| plain/static | 35.6 ms | 3.7 ms | 21.9 MiB | 8.1 MiB | 360.1k | 67.6k |
| plain/placeholders | 46.5 ms | 17.7 ms | 29.4 MiB | 17.1 MiB | 545.2k | 269.1k |

Peak heap size after the optimizations:

| Benchmark | static | placeholders |
| --- | ---: | ---: |
| yaml | 96.4 MiB | 96.2 MiB |
| json | 21.8 MiB | 23.1 MiB |
| xml | 14.0 MiB | 15.2 MiB |
| properties | 10.5 MiB | 11.1 MiB |
| plain | 11.1 MiB | 10.6 MiB |

### Values

| Benchmark | Time before | Time after | Allocated before | Allocated after | Allocations before | Allocations after |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| no_placeholder | 1,370 ns | 23 ns | 432 B | 16 B | 4 | 1 |
| single_placeholder | 1,086 ns | 653 ns | 1,098 B | 624 B | 18 | 13 |
| embedded_placeholders | 2,740 ns | 1,207 ns | 1,957 B | 1,329 B | 36 | 22 |
| string_filter_chain | 3,323 ns | 2,704 ns | 2,061 B | 1,601 B | 49 | 43 |
| list_filter_chain | 3,198 ns | 2,506 ns | 2,205 B | 1,721 B | 46 | 40 |
| regex_filter | 5,300 ns | 4,641 ns | 4,463 B | 4,001 B | 59 | 53 |
| hash_filter | 1,611 ns | 1,212 ns | 1,267 B | 808 B | 24 | 19 |
| 100_placeholders | 297.7 µs | 93.2 µs | 488.6 KiB | 80.8 KiB | 2,302 | 1,595 |

## Scaling

Time and memory grow linearly with the size of a file, so the benchmarks use files of 1 MiB.
Larger files would make the benchmarks slower without revealing different behavior.

Measured with the gonfig binary, `/usr/bin/time` and files with two placeholders per entry, after the optimizations:

| File type | Time 1 MiB | Time 8 MiB | Time 32 MiB | Peak RSS 1 MiB | Peak RSS 8 MiB | Peak RSS 32 MiB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| yaml | 0.29 s | 2.15 s | 8.32 s | 154 MiB | 906 MiB | 3,543 MiB |
| json | 0.08 s | 0.60 s | 2.43 s | 44 MiB | 298 MiB | 987 MiB |
| xml | 0.07 s | 0.56 s | 2.31 s | 36 MiB | 199 MiB | 748 MiB |
| properties | 0.06 s | 0.43 s | 1.81 s | 28 MiB | 103 MiB | 359 MiB |
| plain | 0.02 s | 0.19 s | 0.76 s | 26 MiB | 114 MiB | 380 MiB |

The peak memory of the process is a multiple of the file size: about 110 times for YAML and 11 to 31 times for the other file types.
Config files are usually much smaller than 1 MiB, but very large YAML files may exceed the memory limit of a container.

## Fragments

Inserting and merging [fragments](../../FILTERS.md#fragments) takes linear time in the size of the fragment and the target mapping.
For `merge`, half of the fragment's keys overwrite keys of the target and half are added. XML only supports fragments as the content of elements, so there is no `merge` variant for XML.

| Benchmark | 1,000 keys | 10,000 keys | Peak heap 10,000 keys |
| --- | ---: | ---: | ---: |
| yaml/value | 6.4 ms | 63 ms | 74 MiB |
| yaml/merge | 11.7 ms | 115 ms | 94 MiB |
| json/value | 1.3 ms | 16 ms | 9 MiB |
| json/merge | 2.9 ms | 32 ms | 19 MiB |
| xml/value | 1.7 ms | 20 ms | 14 MiB |

Merging initially looked up each key of the fragment by a linear search through the target mapping, which took quadratic time:
merging 16,000 keys took 915 ms for YAML and 325 ms for JSON. The keys of the target are now indexed once per merge, which reduced it to 192 ms and 55 ms.

Inserting XML fragments took quadratic time as well, since etree shifts all following children when a child is removed,
and the fragment's children were moved from the front: 64,000 elements took 7.4 s. They are now removed from the end, which reduced it to 85 ms.

## Plugins

`BenchmarkExecutablePlugin` in [internal/plugin](../../internal/plugin/) measures filters of [executable plugins](../../PLUGINS.md), which are separate processes:

```console
$ go test -run '^$' -bench ExecutablePlugin -benchmem ./internal/plugin
```

Measured with Go 1.27.2 under WSL2 on the same machine as the baseline, 3 runs each:

| Benchmark | Time | Measures |
| --- | ---: | --- |
| `upper` | 0.26 µs | The built-in filter `upper`, for comparison |
| `bench_upper` | 138 µs | The same filter in a plugin: a request and its response through the plugin's stdin and stdout |
| `startup` | 2.9 ms | Starting a plugin written in Go, reading its handshake and stopping it |

A filter of a plugin costs about 0.14 s per 1,000 values, which is negligible for typical config files with a few values processed by plugins.
Most of it is the round trip between the processes: a line sent through `cat` and back takes 64 µs on the same system.
The plugin is started once per run of gonfig, so its startup time is added once, whether its filters are used or not.

## Findings

### Optimized

- **Finding placeholders:** Every value was split into tokens by a general purpose tokenizer to find the placeholders, which took about 1.4 µs even for values without any placeholders, and up to 57% of the CPU time of processing a file.
  Placeholders are now found with `strings.Index` and a scan for the closing bracket, and values without `${` are returned immediately.
  The tokenizer is still used to parse the content of a placeholder.
- **Building the result:** Each placeholder rebuilt the whole value, so the cost grew quadratically with the number of placeholders in a value. The result is now built in a single pass.
- **Entries without placeholders:** Every entry of a file was formatted with `fmt.Sprintf` and set again, and its path was computed for debug logging even if debug logging was disabled.
  Entries without placeholders are now skipped, and paths are only computed if a debug log record is written.

### Remaining

- **YAML encoding:** About 60% of the memory allocated for YAML files is allocated by the encoder of `go.yaml.in/yaml/v3`, whose event queue grows with the size of the document instead of being reused.
  This is also the main reason for the high peak memory of YAML files, see [scaling](#scaling).
  The archived `gopkg.in/yaml.v3`, which gonfig used before, has the same implementation and performance. Fixing it requires changes to the library.
- **Properties:** Most of the time was spent reading and writing by `github.com/magiconair/properties`, which rewrote the whole file.
  Since [#315](https://github.com/denglertai/gonfig/issues/315), properties files are parsed line by line and unchanged lines are written as read, which takes 72 to 86% less time,
  50 to 67% less allocated memory and 82 to 93% fewer allocations than with the library (1 MiB, Go 1.27.3). The scaling table above was measured afterwards.
- **Parsing filters:** Parsing the content of a placeholder makes up about half of the cost of a placeholder, mostly allocations of the tokenizer, but only 10 to 15% of processing a file with placeholders.
  Caching parsed placeholders would only help for placeholders used repeatedly, a dedicated parser would avoid the allocations in general.
- **Streaming:** Files are read into memory as a whole, because all supported libraries parse complete documents and gonfig must not write any output if a value fails to process.
  The peak heap size of 10 to 23 MiB for 1 MiB files of all types except YAML is acceptable for config files, so streaming is not worth the added complexity.
