package app

import (
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"unicode"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// testLocale is the language the tests read the app in: their texts are
// the Chinese messages of the catalog.
const testLocale = "zh-Hans"

// shown is the language the catalog's messages are said in, which
// mygo.App.Locale returns in the app.
var shown atomic.Value

func TestMain(m *testing.M) {
	shown.Store(testLocale)
	i18n.SetLocaleFunc(func() string { return shown.Load().(string) })
	os.Exit(m.Run())
}

// showIn shows the app in the language of tag for the rest of the test,
// as mygo.App.SetLocale does: its messages, and the testers' text.
func showIn(t *testing.T, tag string, tts ...*ui.Tester) {
	t.Helper()
	shown.Store(tag)
	t.Cleanup(func() { shown.Store(testLocale) })
	for _, tt := range tts {
		tt.SetLocale(tag)
	}
}

// chinese returns the texts of the frame with Han characters.
func chinese(tt *ui.Tester) []string {
	var out []string
	for _, s := range tt.Texts() {
		if strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0 {
			out = append(out, s)
		}
	}
	return out
}

// In English, no window says a word of Chinese.
func TestEnglish(t *testing.T) {
	f := newFixture(t, mockLLM(t, "Hi"))
	chat := ui.NewTester(f.app.ChatView, 1100, 740)
	settings := ui.NewTester(f.app.SettingsView, 860, 620)
	showIn(t, "en", chat, settings)
	check := func(tt *ui.Tester, what string) {
		t.Helper()
		tt.Frame()
		if zh := chinese(tt); len(zh) > 0 {
			t.Errorf("%s shows Chinese: %q", what, zh)
		}
	}

	check(chat, "the empty chat")
	if !chat.HasText("What can I help with?") || !chat.HasText("Map the project") {
		t.Fatalf("empty chat texts %q", chat.Texts())
	}
	shot(t, chat, "en-chat-empty")
	if err := chat.Click("Model: m1"); err != nil {
		t.Fatal(err)
	}
	check(chat, "the model picker")
	if !chat.HasText("Manage accounts and API keys…") {
		t.Fatalf("model picker texts %q", chat.Texts())
	}
	f.app.picker.open = false

	for _, page := range []string{pageAccounts, pageAPIKeys, pageEndpoints, pageGeneral} {
		f.app.settings.page = page
		check(settings, "settings page "+page)
		shot(t, settings, "en-settings-"+page)
	}
	if !settings.HasText("Language") || !settings.HasText("Same as the system") {
		t.Fatalf("general page texts %q", settings.Texts())
	}
	f.app.settings.page = pageEndpoints
	settings.Frame()
	if err := settings.Click("Add endpoint"); err != nil {
		t.Fatal(err)
	}
	check(settings, "the endpoint dialog")
	if !settings.HasText("OpenAI compatible") || !settings.HasText("API protocol") {
		t.Fatalf("endpoint dialog texts %q", settings.Texts())
	}

	f.app.models = nil // as on a machine without any credential
	f.app.current().model = ""
	check(chat, "the chat without models")
	if !chat.HasText("Connect a model first") {
		t.Fatalf("connect card texts %q", chat.Texts())
	}
}

// The language chosen in the general settings shows at once, and is kept.
func TestChooseLanguage(t *testing.T) {
	f := newFixture(t, nil)
	var set []string
	f.app.host.SetLocale = func(tag string) {
		set = append(set, tag)
		shown.Store(tag)
	}
	t.Cleanup(func() { shown.Store(testLocale) })
	tt := ui.NewTester(f.app.SettingsView, 860, 620)
	tt.SetLocale(testLocale)
	f.app.settings.page = pageGeneral
	tt.Frame()
	if !tt.HasText("语言") || !tt.HasText("跟随系统") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("跟随系统"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("English"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	tt.Frame()
	tt.Frame()
	if len(set) != 1 || set[0] != "en" || f.app.Language() != "en" {
		t.Fatalf("chose %q, language %q", set, f.app.Language())
	}
	if !tt.HasText("Language") || !tt.HasText("English") {
		t.Fatalf("texts after choosing English %q", tt.Texts())
	}
	if v, ok, err := f.db.Setting(keyLanguage); err != nil || !ok || v != "en" {
		t.Fatalf("saved language %q %v %v", v, ok, err)
	}
	// A new launch reads it.
	again := &App{db: f.db}
	again.loadPrefs()
	if again.Language() != "en" {
		t.Fatalf("reloaded language %q", again.Language())
	}
}
