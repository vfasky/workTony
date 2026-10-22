package engine

import (
	"encoding/json"
	"errors"
	"slices"
	"sync"

	"github.com/MichaelKinsy/PiG/coding/extension"
)

// Plugins are how the app extends the agent: each is an MCP server, which
// every chat gets besides the servers of mcp.json. A chat registers them
// with PiG's MCP extension as it starts, and registers and unregisters them
// as they change while it runs, so a plugin reaches the open chats at once.
// A server of the same name in mcp.json takes precedence.

// SetPlugin gives every chat the plugin name, an MCP server, or replaces
// the plugin of that name.
func (e *Engine) SetPlugin(name string, config extension.McpServerConfig) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if _, message := extension.ValidateMcpServerConfig(name, raw); message != "" {
		return errors.New(message)
	}
	e.plugins.set(name, config)
	return nil
}

// RemovePlugin takes the plugin name from every chat, which closes its
// connections.
func (e *Engine) RemovePlugin(name string) { e.plugins.remove(name) }

// Plugins are the names of the plugins, in the order they were added.
func (e *Engine) Plugins() []string {
	names, _ := e.plugins.snapshot()
	return names
}

// pluginSet holds the plugins and tells the chats of their changes.
type pluginSet struct {
	mu        sync.Mutex
	names     []string
	configs   map[string]extension.McpServerConfig
	listeners map[int]func()
	next      int
}

func (p *pluginSet) set(name string, config extension.McpServerConfig) {
	p.mu.Lock()
	if p.configs == nil {
		p.configs = map[string]extension.McpServerConfig{}
	}
	if _, ok := p.configs[name]; !ok {
		p.names = append(p.names, name)
	}
	p.configs[name] = config
	p.mu.Unlock()
	p.notify()
}

func (p *pluginSet) remove(name string) {
	p.mu.Lock()
	_, ok := p.configs[name]
	delete(p.configs, name)
	p.names = slices.DeleteFunc(p.names, func(n string) bool { return n == name })
	p.mu.Unlock()
	if ok {
		p.notify()
	}
}

func (p *pluginSet) snapshot() ([]string, map[string]extension.McpServerConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	configs := make(map[string]extension.McpServerConfig, len(p.configs))
	for name, config := range p.configs {
		configs[name] = config
	}
	return slices.Clone(p.names), configs
}

// subscribe calls fn after each change until unsubscribe.
func (p *pluginSet) subscribe(fn func()) (unsubscribe func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listeners == nil {
		p.listeners = map[int]func(){}
	}
	id := p.next
	p.next++
	p.listeners[id] = fn
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.listeners, id)
	}
}

// notify runs the listeners on goroutines of their own: a chat connects a
// plugin while SetPlugin returns. Each listener reads the plugins as they
// are when it runs, so the order the listeners run in does not matter.
func (p *pluginSet) notify() {
	p.mu.Lock()
	listeners := make([]func(), 0, len(p.listeners))
	for _, fn := range p.listeners {
		listeners = append(listeners, fn)
	}
	p.mu.Unlock()
	for _, fn := range listeners {
		go fn()
	}
}

// pluginLink is a chat's end of the plugins: an extension that registers
// them as MCP servers of the chat's session, and follows their changes
// until the chat closes.
type pluginLink struct {
	plugins *pluginSet

	mu          sync.Mutex
	pi          extension.API
	registered  map[string]string // the config each registered plugin had, as JSON
	unsubscribe func()
}

func newPluginLink(plugins *pluginSet) *pluginLink {
	return &pluginLink{plugins: plugins, registered: map[string]string{}}
}

func (l *pluginLink) extension(pi extension.API) error {
	l.mu.Lock()
	l.pi = pi
	l.mu.Unlock()
	// The link listens before it reads the plugins: a change made while it
	// registers those it read syncs again, where one made before it listened
	// would be lost.
	l.unsubscribe = l.plugins.subscribe(l.sync)
	l.sync()
	return nil
}

// sync registers the plugins added or changed since the last sync, and
// unregisters those removed. A plugin whose name another extension
// registered stays out.
func (l *pluginLink) sync() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pi == nil {
		return
	}
	names, configs := l.plugins.snapshot()
	for _, name := range names {
		raw, err := json.Marshal(configs[name])
		if err != nil || l.registered[name] == string(raw) {
			continue
		}
		if l.pi.RegisterMcpServer(name, configs[name]) == nil {
			l.registered[name] = string(raw)
		}
	}
	for name := range l.registered {
		if _, ok := configs[name]; !ok {
			l.pi.UnregisterMcpServer(name)
			delete(l.registered, name)
		}
	}
}

// stop ends the link before the chat closes.
func (l *pluginLink) stop() {
	if l.unsubscribe != nil {
		l.unsubscribe()
	}
	l.mu.Lock()
	l.pi = nil
	l.mu.Unlock()
}
