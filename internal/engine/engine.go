// Package engine embeds PiG, the Go implementation of Pi, for the GUI: it
// owns the agent services and runtime, lists models and providers, signs in
// and out, and runs chats. It has no UI dependency, so tests drive it
// without a window.
package engine

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"

	"worktony/locales"
)

// Options configures an Engine.
type Options struct {
	// AgentDir holds auth.json, models.json, settings.json and sessions.
	// Empty selects PiG's default, ~/.pig/agent, shared with the pig CLI.
	AgentDir string
	// CWD is the working directory chats start in. Empty is the user's home.
	CWD string
	// OpenURL opens a page in the browser, as the sign-in page of an MCP
	// server. Nil opens none.
	OpenURL func(url string)
}

// Engine is the process-wide PiG host. Its methods are safe for concurrent
// use.
type Engine struct {
	agentDir string
	openURL  func(url string)
	plugins  pluginSet

	mu   sync.Mutex
	cwd  string
	svcs *coding.AgentSessionServices
	// retired are the services of earlier working directories, which
	// chats started there still use.
	retired []func()

	// modelsMu orders the changes of models.json.
	modelsMu sync.Mutex

	listenersMu sync.Mutex
	listeners   map[int]func()
	nextID      int
}

// Open creates the services.
func Open(opts Options) (*Engine, error) {
	agentDir := opts.AgentDir
	if agentDir == "" {
		agentDir = coding.DefaultAgentDir()
	}
	cwd := opts.CWD
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		return nil, err
	}
	e := &Engine{agentDir: agentDir, openURL: opts.OpenURL, listeners: map[int]func(){}}
	if err := e.load(cwd); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Engine) load(cwd string) error {
	svcs, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{CWD: cwd, AgentDir: e.agentDir})
	if err != nil {
		return err
	}
	svcs.ModelRuntime().SetChangeListener(e.notify)
	e.cwd, e.svcs = cwd, svcs
	return nil
}

// Close releases the services. Close chats first.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retire()
	for _, release := range e.retired {
		release()
	}
	e.retired = nil
}

// retire moves the current services to the retired list.
func (e *Engine) retire() {
	svcs := e.svcs
	if svcs == nil {
		return
	}
	e.retired = append(e.retired, svcs.Close)
	e.svcs = nil
}

// AgentDir is where PiG keeps its configuration.
func (e *Engine) AgentDir() string { return e.agentDir }

// CWD is the working directory new chats start in.
func (e *Engine) CWD() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cwd
}

// SetCWD moves new chats to dir. PiG's services belong to one project
// directory, so they are rebuilt; running chats keep theirs.
func (e *Engine) SetCWD(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New(locales.ErrNotAFolder(dir))
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if dir == e.cwd {
		return nil
	}
	svcs, cwd := e.svcs, e.cwd
	e.retire()
	if err := e.load(dir); err != nil {
		// Keep the previous directory.
		e.retired = e.retired[:len(e.retired)-1]
		e.svcs, e.cwd = svcs, cwd
		return err
	}
	go e.notify()
	return nil
}

func (e *Engine) services() *coding.AgentSessionServices {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.svcs
}

// OnChange registers fn to run, on any goroutine, when providers,
// credentials or models change. It returns the function that removes it.
func (e *Engine) OnChange(fn func()) func() {
	e.listenersMu.Lock()
	defer e.listenersMu.Unlock()
	e.nextID++
	id := e.nextID
	e.listeners[id] = fn
	return func() {
		e.listenersMu.Lock()
		defer e.listenersMu.Unlock()
		delete(e.listeners, id)
	}
}

func (e *Engine) notify() {
	e.listenersMu.Lock()
	fns := make([]func(), 0, len(e.listeners))
	for _, fn := range e.listeners {
		fns = append(fns, fn)
	}
	e.listenersMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Model describes a chat model.
type Model struct {
	// Ref is "provider/id", the spec PiG resolves.
	Ref          string
	Provider     string
	ProviderName string
	ID           string
	Name         string
	Reasoning    bool
	Images       bool
	Context      int
}

func modelOf(m *ai.Model, providerNames map[string]string) Model {
	provider := m.ProviderMeta.ProviderID
	name := m.DisplayName
	if name == "" {
		name = m.ID
	}
	pname := providerNames[provider]
	if pname == "" {
		pname = provider
	}
	return Model{
		Ref:          provider + "/" + m.ID,
		Provider:     provider,
		ProviderName: pname,
		ID:           m.ID,
		Name:         name,
		Reasoning:    m.ProviderMeta.Reasoning,
		Images:       m.Capabilities.SupportsImages,
		Context:      m.Capabilities.ContextWindow,
	}
}

func (e *Engine) providerNames() map[string]string {
	names := map[string]string{}
	for _, p := range e.services().ModelRuntime().GetProviders() {
		names[p.ID] = p.Name
	}
	return names
}

// AvailableModels lists the chat models whose provider has working
// credentials, grouped by provider in PiG's order.
func (e *Engine) AvailableModels(ctx context.Context) ([]Model, error) {
	models, err := e.services().ModelRuntime().GetAvailable(ctx)
	if err != nil {
		return nil, err
	}
	names := e.providerNames()
	out := make([]Model, 0, len(models))
	for _, m := range models {
		if m.Type != "" && m.Type != ai.ModelTypeChat {
			continue
		}
		out = append(out, modelOf(m, names))
	}
	return out, nil
}

// DefaultModel is the saved default "provider/id", or "".
func (e *Engine) DefaultModel() string {
	sm := e.services().SettingsManager()
	provider, model := sm.GetDefaultProvider(), sm.GetDefaultModel()
	if provider == "" || model == "" {
		return ""
	}
	return provider + "/" + model
}

// SetDefaultModel saves ref as the default in settings.json, as pig's
// model selector does.
func (e *Engine) SetDefaultModel(ref string) error {
	provider, model, ok := strings.Cut(ref, "/")
	if !ok {
		return errors.New(locales.ErrModelRef(ref))
	}
	return e.services().SettingsManager().SetDefaultModelAndProvider(provider, model)
}

// PickModel chooses the model to start a chat with: want when available,
// else the saved default, else the first available.
func PickModel(models []Model, want, def string) (Model, bool) {
	for _, ref := range []string{want, def} {
		if ref == "" {
			continue
		}
		if i := slices.IndexFunc(models, func(m Model) bool { return m.Ref == ref }); i >= 0 {
			return models[i], true
		}
	}
	if len(models) > 0 {
		return models[0], true
	}
	return Model{}, false
}
