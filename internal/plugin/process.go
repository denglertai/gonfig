package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	pkgplugin "github.com/denglertai/gonfig/pkg/plugin"
)

// executablePrefix is the prefix of the names of executable plugins, so other executables in the plugin directory,
// e.g. build scripts next to the sources of a plugin, are not started
const executablePrefix = "gonfig-plugin-"

// Timeouts of executable plugins, variables so tests can shorten them
var (
	// handshakeTimeout limits how long a plugin may take to start and write its handshake
	handshakeTimeout = 10 * time.Second
	// requestTimeout limits how long a plugin may take to answer a request
	requestTimeout = 30 * time.Second
	// closeTimeout limits how long a plugin may take to exit after its stdin is closed, before it is killed
	closeTimeout = 5 * time.Second
)

// isExecutablePlugin returns whether a file is an executable plugin: its name starts with gonfig-plugin- and it is
// executable, which on Windows means that its name ends with .exe
func isExecutablePlugin(name string, mode os.FileMode, goos string) bool {
	if !strings.HasPrefix(name, executablePrefix) || !mode.IsRegular() {
		return false
	}
	if goos == "windows" {
		return strings.HasSuffix(strings.ToLower(name), ".exe")
	}
	return mode&0o111 != 0
}

// message is a line read from a plugin, or the error that ended reading
type message struct {
	data json.RawMessage
	err  error
}

// processPlugin is a running executable plugin
type processPlugin struct {
	path     string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	encoder  *json.Encoder
	messages chan message
	// exited is closed when the plugin has exited
	exited chan struct{}
	// filters are the names of the filters registered for the plugin
	filters []string
	// handshake is the handshake of the plugin, with its optional version, description and filter info
	handshake pkgplugin.Handshake

	// mu serializes requests, since responses are read in the order of the requests
	mu     sync.Mutex
	nextID uint64
	// err is set once the plugin failed, so further requests fail immediately
	err error
}

// startPlugin starts the executable plugin and reads its handshake
func startPlugin(path string) (*processPlugin, []string, error) {
	cmd := exec.Command(path)
	// Plugin logs stay visible, and the environment is inherited, e.g. for keys
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	p := &processPlugin{
		path:     path,
		cmd:      cmd,
		stdin:    stdin,
		encoder:  json.NewEncoder(stdin),
		messages: make(chan message),
		exited:   make(chan struct{}),
	}
	go p.read(stdout)

	handshake, err := p.readHandshake()
	if err != nil {
		p.close()
		return nil, nil, err
	}
	p.handshake = handshake
	return p, handshake.Filters, nil
}

// read passes every line the plugin writes to messages, until the output ends, and waits for the plugin to exit
func (p *processPlugin) read(stdout io.Reader) {
	decoder := json.NewDecoder(stdout)
	for {
		var data json.RawMessage
		if err := decoder.Decode(&data); err != nil {
			p.messages <- message{err: err}
			break
		}
		p.messages <- message{data: data}
	}
	close(p.messages)

	// Wait closes the pipes, so it is only called once everything was read
	_ = p.cmd.Wait()
	close(p.exited)
}

// receive returns the next line of the plugin, or an error if there is none before the deadline
func (p *processPlugin) receive(deadline <-chan time.Time, timeout time.Duration) (json.RawMessage, error) {
	select {
	case msg, ok := <-p.messages:
		if !ok || errors.Is(msg.err, io.EOF) {
			return nil, errors.New("the plugin exited")
		}
		if msg.err != nil {
			return nil, fmt.Errorf("invalid output of the plugin: %w", msg.err)
		}
		return msg.data, nil
	case <-deadline:
		return nil, fmt.Errorf("the plugin did not answer within %s", timeout)
	}
}

// readHandshake reads and checks the handshake of the plugin
func (p *processPlugin) readHandshake() (pkgplugin.Handshake, error) {
	var handshake pkgplugin.Handshake

	timer := time.NewTimer(handshakeTimeout)
	defer timer.Stop()

	data, err := p.receive(timer.C, handshakeTimeout)
	if err != nil {
		return handshake, fmt.Errorf("no handshake: %w", err)
	}
	if err := json.Unmarshal(data, &handshake); err != nil {
		return handshake, fmt.Errorf("invalid handshake: %w", err)
	}
	if handshake.Protocol != pkgplugin.ProtocolVersion {
		return handshake, fmt.Errorf("unsupported protocol version %d, gonfig supports version %d", handshake.Protocol, pkgplugin.ProtocolVersion)
	}
	return handshake, nil
}

// process sends the value to the plugin to be processed by the filter and returns the result
func (p *processPlugin) process(filter string, params map[string]string, value any) (any, error) {
	encoded, err := pkgplugin.EncodeValue(value)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.err != nil {
		return nil, p.err
	}

	response, err := p.request(pkgplugin.Request{ID: p.nextID, Filter: filter, Params: params, Value: encoded})
	p.nextID++
	if err != nil {
		// The plugin cannot be relied on anymore, e.g. a late response would be read for the next request
		p.err = fmt.Errorf("plugin %s failed: %w", p.path, err)
		p.kill()
		return nil, p.err
	}

	if response.Error != "" {
		return nil, errors.New(response.Error)
	}
	return pkgplugin.DecodeValue(response.Value)
}

// request writes the request and reads its response, both within the request timeout
func (p *processPlugin) request(request pkgplugin.Request) (pkgplugin.Response, error) {
	var response pkgplugin.Response

	timer := time.NewTimer(requestTimeout)
	defer timer.Stop()

	// Writing blocks if the plugin does not read its stdin, so it is limited by the timeout as well.
	// After a timeout the plugin is killed, which ends the write.
	written := make(chan error, 1)
	go func() { written <- p.encoder.Encode(request) }()
	select {
	case err := <-written:
		if err != nil {
			return response, fmt.Errorf("failed to send the request: %w", err)
		}
	case <-timer.C:
		return response, fmt.Errorf("the plugin did not read the request within %s", requestTimeout)
	}

	data, err := p.receive(timer.C, requestTimeout)
	if err != nil {
		return response, err
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return response, fmt.Errorf("invalid response: %w", err)
	}
	if response.ID != request.ID {
		return response, fmt.Errorf("response %d does not match request %d", response.ID, request.ID)
	}
	return response, nil
}

// kill stops the plugin immediately
func (p *processPlugin) kill() {
	// The plugin may have exited already
	_ = p.cmd.Process.Kill()
}

// close closes the stdin of the plugin, so it exits, and kills it if it does not exit within the timeout
func (p *processPlugin) close() {
	// Closing a pipe that is already closed is no problem
	_ = p.stdin.Close()

	timer := time.NewTimer(closeTimeout)
	defer timer.Stop()

	// Lines the plugin writes after the last request are dropped until it has exited, so read can reach the end
	// of the output. This includes the time after killing the plugin, since read reports the end of the output, too.
	messages, deadline := p.messages, timer.C
	for {
		select {
		case <-p.exited:
			return
		case _, ok := <-messages:
			if !ok {
				messages = nil
			}
		case <-deadline:
			p.kill()
			deadline = nil
		}
	}
}

// remoteFilter is a filter of an executable plugin. It is registered as a pointer, so every usage gets its own copy
// with its own params.
type remoteFilter struct {
	plugin *processPlugin
	name   string
	params map[string]string
}

// Process sends the value to the plugin
func (f *remoteFilter) Process(value any) (any, error) {
	result, err := f.plugin.process(f.name, f.params, value)
	if err != nil {
		return nil, fmt.Errorf("filter %s: %w", f.name, err)
	}
	return result, nil
}

// AcceptParams keeps the params of the usage, which are sent with every request
func (f *remoteFilter) AcceptParams(params map[string]string) {
	f.params = params
}

// declaredRemoteFilter is a filter of an executable plugin declaring its params, which gonfig validates before sending
// a request, like the params of its built-in filters, see filter.ValidateParams
type declaredRemoteFilter struct {
	remoteFilter
	declared []string
}

// DeclaredParams returns the params the plugin declared for the filter
func (f *declaredRemoteFilter) DeclaredParams() []string {
	return f.declared
}
