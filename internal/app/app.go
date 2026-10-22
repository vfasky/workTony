// Package app is the GUI: the chat window and the settings window, drawn
// in native UI by MyGo over the engine.
package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	// MyGo's own strings in Chinese: the search fields, menus, Copy.
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"worktony/internal/engine"
)

// Prefs are the GUI's own preferences; models and credentials stay in
// PiG's files.
type Prefs struct {
	CWD       string `json:"cwd,omitempty"`
	FullTools bool   `json:"fullTools,omitempty"`
}

// Locale is the one language the app is written in.
const Locale = "zh-Hans"

// Host is what the app needs from the windows it is shown in.
type Host struct {
	// Post runs fn on the main thread, then redraws every window. Nil runs
	// fn at once, as tests do.
	Post func(fn func())
	// PostChat runs fn on the main thread, then redraws the chat window.
	PostChat func(fn func())
	// OpenSettings shows the settings window at a page.
	OpenSettings func(page string)
	// ChooseFolder asks for a folder, off the main thread; "" when
	// canceled.
	ChooseFolder func() string
	// OpenURL opens a page in the default browser, from any goroutine.
	OpenURL func(url string)
	// Reveal shows a file in Finder or Explorer.
	Reveal func(path string)
}

// App is the state both windows show. It belongs to the main thread.
type App struct {
	eng       *engine.Engine
	host      Host
	prefs     Prefs
	prefsPath string

	models    []engine.Model
	providers []engine.Provider
	customs   []engine.CustomProvider
	loaded    bool
	loadErr   string
	defModel  string

	tabs     []*tab
	selected string // the sidebar's choice: a tab's ID
	nextTab  int

	draft     string
	submitted string // sent with Enter, for the next frame to send
	chatToast string // shown by the chat window's next frame
	picker    pickerState
	md        markdown.Options

	settings settingsState
}

// tab is a chat in the sidebar. Its chat starts with the first message.
type tab struct {
	id    string
	chat  *engine.Chat
	model string // "provider/id"
	list  ui.ListState
	open  map[string]bool // thinking and tool parts shown open
}

// New makes the app's state and loads its data in the background.
func New(eng *engine.Engine, host Host, prefsPath string) *App {
	a := &App{eng: eng, host: host, prefsPath: prefsPath}
	if a.host.Post == nil {
		a.host.Post = func(fn func()) { fn() }
	}
	if a.host.PostChat == nil {
		a.host.PostChat = a.host.Post
	}
	a.loadPrefs()
	if a.prefs.CWD != "" && a.prefs.CWD != eng.CWD() {
		if err := eng.SetCWD(a.prefs.CWD); err != nil {
			a.prefs.CWD = ""
		}
	}
	// Links in replies open in the browser, as markdown's default does.
	a.md = markdown.Options{}
	a.newTab()
	eng.OnChange(func() { a.Refresh() })
	return a
}

func (a *App) loadPrefs() {
	if a.prefsPath == "" {
		return
	}
	if b, err := os.ReadFile(a.prefsPath); err == nil {
		_ = json.Unmarshal(b, &a.prefs)
	}
}

func (a *App) savePrefs() {
	if a.prefsPath == "" {
		return
	}
	b, err := json.MarshalIndent(a.prefs, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(a.prefsPath), 0o755); err == nil {
		_ = os.WriteFile(a.prefsPath, b, 0o644)
	}
}

// Refresh reloads models and providers off the main thread.
func (a *App) Refresh() {
	go func() {
		ctx := context.Background()
		models, err := a.eng.AvailableModels(ctx)
		providers := a.eng.Providers(ctx)
		customs, _ := a.eng.CustomProviders()
		def := a.eng.DefaultModel()
		a.host.Post(func() {
			a.models, a.providers, a.customs, a.defModel, a.loaded = models, providers, customs, def, true
			a.loadErr = ""
			if err != nil {
				a.loadErr = err.Error()
			}
			for _, t := range a.tabs {
				if t.chat == nil {
					t.model = a.modelFor(t.model)
				}
			}
		})
	}()
}

// RefreshSync reloads at once, for tests.
func (a *App) RefreshSync() {
	ctx := context.Background()
	a.models, _ = a.eng.AvailableModels(ctx)
	a.providers = a.eng.Providers(ctx)
	a.customs, _ = a.eng.CustomProviders()
	a.defModel = a.eng.DefaultModel()
	a.loaded = true
	for _, t := range a.tabs {
		if t.chat == nil {
			t.model = a.modelFor(t.model)
		}
	}
}

// modelFor is the model a new chat takes: want when it is available.
func (a *App) modelFor(want string) string {
	m, ok := engine.PickModel(a.models, want, a.defModel)
	if !ok {
		return ""
	}
	return m.Ref
}

func (a *App) model(ref string) (engine.Model, bool) {
	i := slices.IndexFunc(a.models, func(m engine.Model) bool { return m.Ref == ref })
	if i < 0 {
		return engine.Model{}, false
	}
	return a.models[i], true
}

func (a *App) current() *tab {
	for _, t := range a.tabs {
		if t.id == a.selected {
			return t
		}
	}
	return a.tabs[0]
}

// NewChat shows a new, empty chat.
func (a *App) NewChat() { a.newTab() }

// newTab shows a new, empty chat, reusing the empty one there is. Either
// way it takes the default model as it is now.
func (a *App) newTab() *tab {
	for _, t := range a.tabs {
		if t.chat == nil {
			t.model = a.modelFor("")
			a.selected = t.id
			return t
		}
	}
	a.nextTab++
	t := &tab{id: "chat-" + strconv.Itoa(a.nextTab), open: map[string]bool{}, model: a.modelFor("")}
	a.tabs = append([]*tab{t}, a.tabs...)
	a.selected = t.id
	a.draft = ""
	return t
}

func (a *App) closeTab(t *tab) {
	i := slices.Index(a.tabs, t)
	if i < 0 {
		return
	}
	if t.chat != nil {
		t.chat.Close()
	}
	a.tabs = slices.Delete(a.tabs, i, i+1)
	if len(a.tabs) == 0 {
		a.newTab()
		return
	}
	if a.selected == t.id {
		a.selected = a.tabs[min(i, len(a.tabs)-1)].id
	}
}

func (a *App) tools() []string {
	if a.prefs.FullTools {
		return engine.FullTools
	}
	return engine.ReadOnlyTools
}

// send sends the draft in the current tab, starting its chat.
func (a *App) send(c *ui.Context, text string) bool {
	t := a.current()
	if t.chat == nil {
		if t.model == "" {
			c.Toast("还没有可用的模型，请先在设置中登录或添加 API Key")
			return false
		}
		chat, err := a.eng.NewChat(engine.ChatOptions{Model: t.model, Tools: a.tools(), Post: a.host.PostChat})
		if err != nil {
			c.Toast("无法开始对话: " + err.Error())
			return false
		}
		t.chat = chat
	}
	t.chat.Send(text)
	t.list.FollowEnd = true
	return true
}

// setModel picks the model of the current tab and remembers it.
func (a *App) setModel(c *ui.Context, m engine.Model) {
	t := a.current()
	if t.chat != nil {
		if err := t.chat.SetModel(m); err != nil {
			c.Toast("切换模型失败: " + err.Error())
			return
		}
	}
	t.model = m.Ref
	a.setDefaultModel(m)
}

// setDefaultModel makes m the model of new chats, the empty tab's too, and
// saves it in settings.json. It saves at once, a small file, so a refresh
// reading the default after it reads m.
func (a *App) setDefaultModel(m engine.Model) {
	a.defModel = m.Ref
	for _, t := range a.tabs {
		if t.chat == nil {
			t.model = m.Ref
		}
	}
	_ = a.eng.SetDefaultModel(m.Ref)
}

// Close ends the chats.
func (a *App) Close() {
	for _, t := range a.tabs {
		if t.chat != nil {
			t.chat.Close()
		}
	}
}
