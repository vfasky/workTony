package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"worktony/internal/engine"
)

// A message that cannot be sent stays in the composer, sent with Enter or
// with the button.
func TestSendFailureKeepsDraft(t *testing.T) {
	f := newFixture(t, nil)
	f.app.models = nil
	f.app.current().model = ""
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(Locale)
	tt.Frame()
	if err := tt.Click("消息"); err != nil {
		t.Fatal(err)
	}
	tt.Type("你好")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	if f.app.draft != "你好" {
		t.Fatalf("after Enter the draft is %q", f.app.draft)
	}
	if err := tt.Click("发送"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.app.draft != "你好" || f.app.current().chat != nil {
		t.Fatalf("after the button the draft is %q, chat %v", f.app.draft, f.app.current().chat)
	}
}

func (f *fixture) modelOf(t *testing.T, ref string) engine.Model {
	t.Helper()
	m, ok := f.app.model(ref)
	if !ok {
		t.Fatalf("no model %s in %+v", ref, f.app.models)
	}
	return m
}

// New chats take the default model as it is when they open, whatever the
// chat shown uses.
func TestNewChatTakesDefaultModel(t *testing.T) {
	f := newFixture(t, mockLLM(t, "好"))
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(Locale)
	tt.Frame()
	first := f.app.current()
	if first.model != "mock/m1" {
		t.Fatalf("first chat model %q", first.model)
	}
	f.app.send(nil, "hi")
	f.until(t, tt, "the reply", func() bool { return !first.chat.Busy })

	// The settings window's default model changes while the chat runs m1.
	st := ui.NewTester(f.app.SettingsView, 860, 620)
	st.SetLocale(Locale)
	f.app.settings.page = pageGeneral
	st.Frame()
	f.app.setDefaultModel(f.modelOf(t, "mock/m2"))
	if first.model != "mock/m1" {
		t.Fatal("a chat already started keeps its model")
	}
	if err := tt.Click("新对话"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tb := f.app.current(); tb == first || tb.model != "mock/m2" {
		t.Fatalf("new chat model %q", tb.model)
	}
	if !tt.HasText("m2") {
		t.Fatalf("the composer should show m2: %q", tt.Texts())
	}

	// The empty tab, reused, follows the default too.
	empty := f.app.current()
	f.app.selected = first.id
	f.app.setDefaultModel(f.modelOf(t, "mock/m1"))
	f.app.newTab()
	if f.app.current() != empty || empty.model != "mock/m1" {
		t.Fatalf("reused tab %v, model %q", f.app.current() == empty, empty.model)
	}
	f.app.defModel = "mock/m2" // as a refresh reads after pig changed it
	f.app.newTab()
	if empty.model != "mock/m2" {
		t.Fatalf("reused tab model %q, want the default as it is now", empty.model)
	}
}

// The composer shows the directory and the tools a chat started with;
// changing them starts a new chat.
func TestComposerShowsChatDirectory(t *testing.T) {
	f := newFixture(t, mockLLM(t, "好"))
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(Locale)
	tt.Frame()
	started := f.app.current()
	f.app.send(nil, "hi")
	f.until(t, tt, "the reply", func() bool { return !started.chat.Busy })
	firstDir := started.chat.CWD()

	other := filepath.Join(t.TempDir(), "other-project")
	os.Mkdir(other, 0o755)
	err := f.eng.SetCWD(other)
	f.app.cwdChanged(other, err, true)
	tt.Frame()
	if tb := f.app.current(); tb == started || tb.chat != nil {
		t.Fatal("choosing a directory from a started chat should open a new chat")
	}
	if !tt.HasText("other-project") {
		t.Fatalf("the new chat should show the new directory: %q", tt.Texts())
	}
	f.app.selected = started.id
	tt.Advance(10 * time.Second) // the toast goes
	tt.Frame()
	if !tt.HasText(filepath.Base(firstDir)) || tt.HasText("other-project") {
		t.Fatalf("the started chat should show its own directory %q: %q", firstDir, tt.Texts())
	}
	if started.chat.CWD() != firstDir {
		t.Fatalf("the chat moved to %q", started.chat.CWD())
	}

	// Its tools stay read-only whatever new chats get.
	f.app.prefs.FullTools = true
	tt.Frame()
	if !tt.HasText("只读") || tt.HasText("可修改") {
		t.Fatalf("the started chat is read-only: %q", tt.Texts())
	}
	f.app.prefs.FullTools = false
	if err := tt.Click("只读"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tb := f.app.current(); tb == started || !f.app.prefs.FullTools || !tt.HasText("可修改") {
		t.Fatalf("switching tools should open a new chat with them: full %v, texts %q", f.app.prefs.FullTools, tt.Texts())
	}
}
