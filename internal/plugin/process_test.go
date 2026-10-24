package plugin

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/denglertai/gonfig/internal/filter"
	pkgplugin "github.com/denglertai/gonfig/pkg/plugin"
	"github.com/stretchr/testify/assert"
)

// testPluginEnv makes the test binary act as an executable plugin, with the behavior named by its value
const testPluginEnv = "GONFIG_TEST_PLUGIN"

// testPluginPrefixEnv is the prefix of the filter names of the test plugin. Registered filters cannot be removed,
// so every test uses its own names.
const testPluginPrefixEnv = "GONFIG_TEST_PLUGIN_PREFIX"

// TestMain runs the test binary as a plugin if testPluginEnv is set, and runs the tests otherwise
func TestMain(m *testing.M) {
	if mode := os.Getenv(testPluginEnv); mode != "" {
		os.Exit(runTestPlugin(mode, os.Getenv(testPluginPrefixEnv)))
	}
	os.Exit(m.Run())
}

// testFilter is a filter of the test plugin provided by a function, which receives the params of the usage
type testFilter struct {
	fn     func(value any, params map[string]string) (any, error)
	params map[string]string
}

// Process calls the function with the value and the params
func (f *testFilter) Process(value any) (any, error) {
	return f.fn(value, f.params)
}

// AcceptParams keeps the params
func (f *testFilter) AcceptParams(params map[string]string) {
	f.params = params
}

// describedFilter is a filter of the test plugin with a description and declared params
type describedFilter struct {
	testFilter
	description string
	params      []string
}

// Description describes the filter
func (f *describedFilter) Description() string {
	return f.description
}

// Params returns the declared params
func (f *describedFilter) Params() []string {
	return f.params
}

// runTestPlugin acts as a plugin behaving as described by the mode and returns its exit code
func runTestPlugin(mode string, prefix string) int {
	handshake := func(filters ...string) {
		names := make([]string, len(filters))
		for i, name := range filters {
			names[i] = fmt.Sprintf("%q", prefix+name)
		}
		fmt.Printf(`{"protocol":1,"filters":[%s]}`+"\n", strings.Join(names, ","))
	}
	stdin := bufio.NewScanner(os.Stdin)
	stdin.Buffer(nil, 4<<20)

	switch mode {
	case "serve":
		err := pkgplugin.Serve(map[string]any{
			prefix + "upper": &testFilter{fn: func(value any, _ map[string]string) (any, error) {
				return strings.ToUpper(value.(string)), nil
			}},
			prefix + "params": &testFilter{fn: func(_ any, params map[string]string) (any, error) {
				return fmt.Sprint(params), nil
			}},
			prefix + "type": &testFilter{fn: func(value any, _ map[string]string) (any, error) {
				return fmt.Sprintf("%T", value), nil
			}},
			prefix + "fail": &testFilter{fn: func(any, map[string]string) (any, error) {
				return nil, errors.New("the value is not encrypted")
			}},
		})
		if err != nil {
			return 1
		}
	case "described":
		err := pkgplugin.Serve(map[string]any{
			prefix + "declared": &describedFilter{testFilter: testFilter{fn: func(_ any, params map[string]string) (any, error) {
				return fmt.Sprint(params), nil
			}}, description: "Shows its params", params: []string{"first", "second"}},
			prefix + "any": &testFilter{fn: func(_ any, params map[string]string) (any, error) {
				return fmt.Sprint(params), nil
			}},
		}, pkgplugin.WithVersion("1.2.3"), pkgplugin.WithDescription("A described test plugin"))
		if err != nil {
			return 1
		}
	case "silent":
		time.Sleep(time.Minute)
	case "exit":
	case "garbage":
		fmt.Println("not json")
	case "array":
		fmt.Println("[1]")
	case "protocol":
		fmt.Println(`{"protocol":99,"filters":["a"]}`)
	case "crash":
		handshake("crash")
		stdin.Scan()
		return 3
	case "hang":
		// Neither reads requests nor exits when stdin is closed
		handshake("hang")
		time.Sleep(time.Minute)
	case "mismatch":
		handshake("mismatch")
		stdin.Scan()
		fmt.Println(`{"id":7,"value":"x"}`)
		stdin.Scan()
	case "badresponse":
		handshake("badresponse")
		stdin.Scan()
		fmt.Println(`[1]`)
		stdin.Scan()
	case "garbageresponse":
		handshake("garbageresponse")
		stdin.Scan()
		fmt.Println(`not json`)
		stdin.Scan()
	}
	return 0
}

// installTestPlugin links the test binary as an executable plugin into a new directory, which it returns. The plugin
// behaves as described by the mode, and its filter names start with a prefix derived from the test name, which is
// returned as well.
func installTestPlugin(t *testing.T, mode string) (string, string) {
	t.Helper()
	skipOnWindows(t)

	executable, err := os.Executable()
	assert.NoError(t, err)

	dir := t.TempDir()
	assert.NoError(t, os.Symlink(executable, filepath.Join(dir, executablePrefix+"test")))

	prefix := strings.ToLower(regexp.MustCompile(`\W+`).ReplaceAllString(t.Name(), "_")) + "_"
	t.Setenv(testPluginEnv, mode)
	t.Setenv(testPluginPrefixEnv, prefix)
	return dir, prefix
}

// skipOnWindows skips tests relying on symbolic links, executable permissions or shell scripts, which Windows lacks.
// Plugins on Windows are covered by TestIsExecutablePlugin and the end-to-end test of the dummy plugin in cmd.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires symbolic links, executable permissions and shell scripts")
	}
}

// shortenTimeouts shortens the timeouts of executable plugins for the test. The handshake timeout stays longer,
// since the test binary starts slowly as a plugin when built with the race detector.
func shortenTimeouts(t *testing.T) {
	t.Helper()

	handshake, request, closing := handshakeTimeout, requestTimeout, closeTimeout
	handshakeTimeout, requestTimeout, closeTimeout = 3*time.Second, 500*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { handshakeTimeout, requestTimeout, closeTimeout = handshake, request, closing })
}

// process creates a usage of the filter with the params, like a placeholder, and processes the value
func process(name string, params map[string]string, value any) (any, error) {
	f := filter.NewFilter(name)
	if params != nil {
		f.(filter.FilterParams).AcceptParams(params)
	}
	return f.Process(value)
}

// TestExecutablePlugin checks that the filters of an executable plugin are registered and process values
// with the params of their usage
func TestExecutablePlugin(t *testing.T) {
	dir, prefix := installTestPlugin(t, "serve")
	InitPlugins(dir)
	t.Cleanup(ClosePlugins)

	for _, name := range []string{"upper", "params", "type", "fail"} {
		assert.True(t, filter.IsKnown(prefix+name), name)
	}

	result, err := process(prefix+"upper", nil, "s3cret")
	assert.NoError(t, err)
	assert.Equal(t, "S3CRET", result)

	// Every usage has its own params
	result, err = process(prefix+"params", map[string]string{"0": "base64"}, "x")
	assert.NoError(t, err)
	assert.Equal(t, "map[0:base64]", result)
	result, err = process(prefix+"params", nil, "x")
	assert.NoError(t, err)
	assert.Equal(t, "map[]", result)

	// Values keep their type
	for value, want := range map[any]string{"x": "string", 42: "int", 1.0: "float64", true: "bool"} {
		result, err = process(prefix+"type", nil, value)
		assert.NoError(t, err)
		assert.Equal(t, want, result)
	}
	result, err = process(prefix+"type", nil, []string{"a"})
	assert.NoError(t, err)
	assert.Equal(t, "[]string", result)

	_, err = process(prefix+"fail", nil, "s3cret")
	assert.EqualError(t, err, "filter "+prefix+"fail: the value is not encrypted")

	_, err = process(prefix+"upper", nil, map[string]string{})
	assert.EqualError(t, err, "filter "+prefix+"upper: values of type map[string]string are not supported")

	// A failing request does not affect the following ones
	result, err = process(prefix+"upper", nil, "abc")
	assert.NoError(t, err)
	assert.Equal(t, "ABC", result)
}

// TestExecutablePluginStartFailures checks that plugins that do not start correctly are rejected
func TestExecutablePluginStartFailures(t *testing.T) {
	testCases := []struct {
		mode    string
		wantErr string
	}{
		{mode: "silent", wantErr: "no handshake: the plugin did not answer within 3s"},
		{mode: "exit", wantErr: "no handshake: the plugin exited"},
		{mode: "garbage", wantErr: "no handshake: invalid output of the plugin: invalid character"},
		{mode: "array", wantErr: "invalid handshake: json: cannot unmarshal array"},
		{mode: "protocol", wantErr: "unsupported protocol version 99, gonfig supports version 1"},
	}
	for _, tC := range testCases {
		t.Run(tC.mode, func(t *testing.T) {
			shortenTimeouts(t)
			dir, _ := installTestPlugin(t, tC.mode)

			start := time.Now()
			_, _, err := startPlugin(filepath.Join(dir, executablePrefix+"test"))
			assert.ErrorContains(t, err, tC.wantErr)
			// The plugin is stopped, at the latest when the close timeout kills it
			assert.Less(t, time.Since(start), 8*time.Second)
		})
	}

	_, _, err := startPlugin(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
}

// TestExecutablePluginRequestFailures checks that a plugin failing a request fails this and all following requests
func TestExecutablePluginRequestFailures(t *testing.T) {
	testCases := []struct {
		mode    string
		value   any
		wantErr string
	}{
		{mode: "crash", value: "x", wantErr: "the plugin exited"},
		{mode: "hang", value: "x", wantErr: "the plugin did not answer within 500ms"},
		// A value larger than the pipe buffer cannot be written to a plugin that does not read it
		{mode: "hang", value: strings.Repeat("x", 1<<20), wantErr: "the plugin did not read the request within 500ms"},
		{mode: "mismatch", value: "x", wantErr: "response 7 does not match request 0"},
		{mode: "badresponse", value: "x", wantErr: "invalid response: json: cannot unmarshal array"},
		{mode: "garbageresponse", value: "x", wantErr: "invalid output of the plugin: invalid character"},
	}
	for _, tC := range testCases {
		t.Run(tC.mode, func(t *testing.T) {
			shortenTimeouts(t)
			dir, prefix := installTestPlugin(t, tC.mode)
			InitPlugins(dir)
			t.Cleanup(ClosePlugins)

			_, err := process(prefix+tC.mode, nil, tC.value)
			assert.ErrorContains(t, err, "filter "+prefix+tC.mode+": plugin "+filepath.Join(dir, executablePrefix+"test")+" failed: ")
			assert.ErrorContains(t, err, tC.wantErr)

			start := time.Now()
			_, again := process(prefix+tC.mode, nil, "x")
			assert.Equal(t, err, again)
			assert.Less(t, time.Since(start), 100*time.Millisecond)
		})
	}
}

// TestExecutablePluginSendFailure checks that a request to a plugin that has exited fails
func TestExecutablePluginSendFailure(t *testing.T) {
	dir, prefix := installTestPlugin(t, "crash")
	p, _, err := startPlugin(filepath.Join(dir, executablePrefix+"test"))
	assert.NoError(t, err)
	t.Cleanup(p.close)

	// Let the plugin exit by closing its stdin, and wait until it has
	assert.NoError(t, p.stdin.Close())
	_, err = p.receive(nil, 0)
	assert.EqualError(t, err, "the plugin exited")
	<-p.exited

	_, err = p.process(prefix+"crash", nil, "x")
	assert.ErrorContains(t, err, "failed to send the request")
}

// TestIsExecutablePlugin checks which files are started as executable plugins
func TestIsExecutablePlugin(t *testing.T) {
	testCases := []struct {
		name string
		mode os.FileMode
		goos string
		want bool
	}{
		{name: "gonfig-plugin-crypto", mode: 0o755, goos: "linux", want: true},
		{name: "gonfig-plugin-crypto", mode: 0o700, goos: "darwin", want: true},
		{name: "gonfig-plugin-crypto", mode: 0o644, goos: "linux", want: false},
		{name: "build.sh", mode: 0o755, goos: "linux", want: false},
		{name: "gonfig-plugin-crypto", mode: os.ModeDir | 0o755, goos: "linux", want: false},
		{name: "gonfig-plugin-crypto.exe", mode: 0o644, goos: "windows", want: true},
		{name: "gonfig-plugin-crypto.EXE", mode: 0o644, goos: "windows", want: true},
		{name: "gonfig-plugin-crypto", mode: 0o755, goos: "windows", want: false},
	}
	for _, tC := range testCases {
		assert.Equal(t, tC.want, isExecutablePlugin(tC.name, tC.mode, tC.goos), "%s %v %s", tC.name, tC.mode, tC.goos)
	}
}

// TestInitPluginsSkipsOtherFiles checks that only executables named like plugins are started
func TestInitPluginsSkipsOtherFiles(t *testing.T) {
	skipOnWindows(t)

	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "started")
	script := "#!/bin/sh\ntouch " + marker + "\n"

	assert.NoError(t, os.WriteFile(filepath.Join(dir, "build.sh"), []byte(script), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, executablePrefix+"not-executable"), []byte(script), 0o644))
	assert.NoError(t, os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, executablePrefix+"broken-link")))

	InitPlugins(dir)
	t.Cleanup(ClosePlugins)

	assert.NoFileExists(t, marker)
	assert.Empty(t, running)
}

// TestInitPluginsStartFailure checks that a plugin failing to start is skipped, so its filters are unknown
func TestInitPluginsStartFailure(t *testing.T) {
	shortenTimeouts(t)
	dir, prefix := installTestPlugin(t, "protocol")

	InitPlugins(dir)
	t.Cleanup(ClosePlugins)

	assert.Empty(t, running)
	assert.False(t, filter.IsKnown(prefix+"a"))
}

// BenchmarkExecutablePlugin measures the overhead of processing a value with a filter of an executable plugin,
// compared with the built-in upper filter
func BenchmarkExecutablePlugin(b *testing.B) {
	executable, err := os.Executable()
	if err != nil {
		b.Fatal(err)
	}
	dir := b.TempDir()
	if err := os.Symlink(executable, filepath.Join(dir, executablePrefix+"bench")); err != nil {
		b.Fatal(err)
	}
	b.Setenv(testPluginEnv, "serve")
	b.Setenv(testPluginPrefixEnv, "bench_")
	InitPlugins(dir)
	b.Cleanup(ClosePlugins)

	value := strings.Repeat("s3cret", 10)
	for _, name := range []string{"upper", "bench_upper"} {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				if _, err := process(name, nil, value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}

	b.Run("startup", func(b *testing.B) {
		for b.Loop() {
			p, _, err := startPlugin(filepath.Join(dir, executablePrefix+"bench"))
			if err != nil {
				b.Fatal(err)
			}
			p.close()
		}
	})
}

// TestDefaultPluginPaths checks that the system directory is searched before the local one, except on Windows
func TestDefaultPluginPaths(t *testing.T) {
	assert.Equal(t, []string{"/usr/libexec/gonfig/plugins", "./plugins"}, defaultPluginPaths("linux"))
	assert.Equal(t, []string{"/usr/libexec/gonfig/plugins", "./plugins"}, defaultPluginPaths("darwin"))
	assert.Equal(t, []string{"./plugins"}, defaultPluginPaths("windows"))
}

// useSystemPluginDir makes the directory the system plugin directory for the test
func useSystemPluginDir(t *testing.T, dir string) {
	t.Helper()
	previous := systemPluginDir
	systemPluginDir = dir
	t.Cleanup(func() { systemPluginDir = previous })
}

// TestInitPluginsSearchesDefaultPaths checks that both default directories are searched without a plugin path, and
// that the plugin of the system directory keeps its filters if a plugin in ./plugins provides the same ones
func TestInitPluginsSearchesDefaultPaths(t *testing.T) {
	systemDir, prefix := installTestPlugin(t, "serve")
	useSystemPluginDir(t, systemDir)

	executable, err := os.Executable()
	assert.NoError(t, err)
	workDir := t.TempDir()
	assert.NoError(t, os.Mkdir(filepath.Join(workDir, "plugins"), 0o755))
	assert.NoError(t, os.Symlink(executable, filepath.Join(workDir, "plugins", executablePrefix+"local")))
	t.Chdir(workDir)

	InitPlugins("")
	t.Cleanup(ClosePlugins)

	if assert.Len(t, running, 2) {
		assert.Equal(t, filepath.Join(systemDir, executablePrefix+"test"), running[0].path)
		assert.Contains(t, running[0].filters, prefix+"upper")
		assert.Equal(t, filepath.Join("plugins", executablePrefix+"local"), running[1].path)
		assert.Empty(t, running[1].filters)
	}
}

// TestInitPluginsConfiguredPathOnly checks that a configured plugin path replaces the default ones
func TestInitPluginsConfiguredPathOnly(t *testing.T) {
	systemDir, prefix := installTestPlugin(t, "serve")
	useSystemPluginDir(t, systemDir)

	InitPlugins(t.TempDir())
	t.Cleanup(ClosePlugins)

	assert.Empty(t, running)
	assert.False(t, filter.IsKnown(prefix+"upper"))
}

// TestPluginInfo checks the version, description and filter info of a plugin providing them, and that declared params
// are validated by gonfig while filters without declared params receive any params
func TestPluginInfo(t *testing.T) {
	dir, prefix := installTestPlugin(t, "described")

	InitPlugins(dir)
	t.Cleanup(ClosePlugins)

	assert.Equal(t, []string{dir}, SearchedPaths())
	assert.Equal(t, []Info{{
		Path:        filepath.Join(dir, executablePrefix+"test"),
		Type:        TypeExecutable,
		Version:     "1.2.3",
		Description: "A described test plugin",
		Filters: []FilterInfo{
			{Name: prefix + "any"},
			{Name: prefix + "declared", Description: "Shows its params", Params: []string{"first", "second"}},
		},
	}}, Plugins())

	declared := filter.NewFilter(prefix + "declared")
	assert.EqualError(t, filter.ValidateParams(prefix+"declared", declared, map[string]string{"thrid": "x"}),
		`filter `+prefix+`declared has no param "thrid", it accepts first, second`)
	assert.NoError(t, filter.ValidateParams(prefix+"declared", declared, map[string]string{"0": "a", "second": "b"}))
	assert.NoError(t, filter.ValidateParams(prefix+"any", filter.NewFilter(prefix+"any"), map[string]string{"anything": "x"}))

	result, err := process(prefix+"declared", map[string]string{"first": "a"}, "v")
	assert.NoError(t, err)
	assert.Equal(t, "map[first:a]", result)
}

// TestPluginInfoWithoutOptionalFields checks plugins without version, description and filter info, like plugins built
// before these fields existed
func TestPluginInfoWithoutOptionalFields(t *testing.T) {
	dir, prefix := installTestPlugin(t, "serve")

	InitPlugins(dir)
	t.Cleanup(ClosePlugins)

	if assert.Len(t, Plugins(), 1) {
		info := Plugins()[0]
		assert.Empty(t, info.Version)
		assert.Empty(t, info.Description)
		assert.Contains(t, info.Filters, FilterInfo{Name: prefix + "upper"})
	}
}

// TestPluginInfoFailures checks that plugins failing to start are listed with their error, and that filters whose name
// is taken are marked as skipped
func TestPluginInfoFailures(t *testing.T) {
	shortenTimeouts(t)
	dir, prefix := installTestPlugin(t, "serve")
	executable, err := os.Executable()
	assert.NoError(t, err)
	// A second link starts the same plugin again, whose filters are registered already
	assert.NoError(t, os.Symlink(executable, filepath.Join(dir, executablePrefix+"twice")))
	broken := filepath.Join(dir, executablePrefix+"broken")
	assert.NoError(t, os.WriteFile(broken, []byte("#!/bin/sh\nexit 3\n"), 0o755))

	InitPlugins(dir)
	t.Cleanup(ClosePlugins)

	plugins := Plugins()
	if assert.Len(t, plugins, 3) {
		assert.Equal(t, broken, plugins[0].Path)
		assert.Equal(t, "no handshake: the plugin exited", plugins[0].Error)
		assert.Empty(t, plugins[0].Filters)
		assert.Contains(t, plugins[1].Filters, FilterInfo{Name: prefix + "upper"})
		assert.Contains(t, plugins[2].Filters, FilterInfo{Name: prefix + "upper", Skipped: true})
	}
}

// TestPluginInfoGoPlugin checks that a Go plugin failing to load is listed with its error
func TestPluginInfoGoPlugin(t *testing.T) {
	dir := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "broken.so"), []byte("not a plugin"), 0o644))

	InitPlugins(dir)
	t.Cleanup(ClosePlugins)

	if assert.Len(t, Plugins(), 1) {
		assert.Equal(t, TypeGo, Plugins()[0].Type)
		assert.NotEmpty(t, Plugins()[0].Error)
	}
}
