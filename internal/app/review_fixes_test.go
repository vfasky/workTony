package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"worktony/internal/engine"
	"worktony/locales"
)

// settleMoves pumps until the moves of new chats asked for have run.
func (f *fixture) settleMoves(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for f.app.cwdPending > 0 && time.Now().Before(deadline) {
		f.q.pump()
		time.Sleep(5 * time.Millisecond)
	}
	if f.app.cwdPending > 0 {
		t.Fatalf("%d moves still pending", f.app.cwdPending)
	}
}

// Projects chosen quickly one after another end in the last chosen: the
// engine, the saved directory and the chat shown agree, and the chat
// works there.
func TestProjectSwitchesInOrder(t *testing.T) {
	f := newFixture(t, mockLLM(t, "好"))
	root := t.TempDir()
	alpha := mkdir(t, filepath.Join(root, "alpha"))
	beta := mkdir(t, filepath.Join(root, "beta"))
	gamma := mkdir(t, filepath.Join(root, "gamma"))
	f.chooseDir(t, alpha)
	f.chooseDir(t, beta)
	f.chooseDir(t, gamma)
	pa, pb := f.project(t, alpha), f.project(t, beta)
	for range 3 {
		f.app.newChatIn(pa)
		f.app.newChatIn(pb)
	}
	f.settleMoves(t)
	tb := f.app.current()
	if !samePath(f.eng.CWD(), beta) || f.app.prefs.CWD != beta || tb.chat != nil || tb.dir != beta {
		t.Fatalf("engine in %q, saved %q, chat shown in %q", f.eng.CWD(), f.app.prefs.CWD, tb.dir)
	}
	if v, _, _ := f.db.Setting(keyCWD); v != beta {
		t.Fatalf("saved cwd %q", v)
	}

	// The empty chat starts where it shows, though new chats moved behind
	// its back.
	if err := f.eng.SetCWD(alpha); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.Frame()
	if !tt.HasText("工作目录 beta") {
		t.Fatalf("the composer should show beta: %q", tt.Texts())
	}
	f.app.sendTo(nil, tb, "在哪里")
	if tb.chat == nil || tb.chat.CWD() != beta {
		t.Fatalf("the chat started in %q, want beta", tb.chat.CWD())
	}
	f.until(t, tt, "the reply", func() bool { return !tb.chat.Busy })
}

// A message sent with Enter goes to the chat it was written in, though
// another shows by the frame that sends it, and nowhere once that chat is
// closed.
func TestEnterSendsToItsChat(t *testing.T) {
	f := newFixture(t, mockLLM(t, "好"))
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.Frame()
	first := f.app.current()
	f.app.sendTo(nil, first, "一")
	f.until(t, tt, "the reply", func() bool { return !first.chat.Busy })
	second := f.app.newTab()
	if second == first {
		t.Fatal("a new tab should open")
	}

	// Enter in the first chat; the second shows before the next frame.
	f.app.submitted = &submission{tab: first.id, text: "二"}
	f.app.draft = "二"
	f.app.selected = second.id
	tt.Frame()
	if second.chat != nil {
		t.Fatal("the message went to the chat shown")
	}
	if f.app.draft != "" {
		t.Fatalf("the draft sent stays: %q", f.app.draft)
	}
	f.until(t, tt, "the second reply", func() bool { return !first.chat.Busy && len(first.chat.Items) == 4 })
	if it := first.chat.Items[2]; it.Role != engine.RoleUser || it.Text != "二" {
		t.Fatalf("the first chat got %+v", it)
	}

	// Its chat closed, the message goes nowhere and stays in the composer.
	f.app.submitted = &submission{tab: first.id, text: "三"}
	f.app.draft = "三"
	f.app.closeTab(first)
	tt.Frame()
	if second.chat != nil || f.app.draft != "三" {
		t.Fatalf("second chat %v, draft %q", second.chat, f.app.draft)
	}
}

// Fetching the models of an endpoint edited without a new key uses its
// saved key; at another base URL it says the key was not sent.
func TestEndpointFetchUsesSavedKey(t *testing.T) {
	f := newFixture(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-saved" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"m1"},{"id":"m2"}]}`)
	}))
	t.Cleanup(srv.Close)
	base := srv.URL + "/v1"
	if err := f.eng.SaveCustomProvider(context.Background(), engine.CustomProvider{ID: "gw", BaseURL: base, API: "openai-completions", Models: []string{"m1"}}, "sk-saved"); err != nil {
		t.Fatal(err)
	}
	fetch := func(form *endpointForm) {
		t.Helper()
		f.app.fetchModels(form)
		deadline := time.Now().Add(10 * time.Second)
		for form.fetching && time.Now().Before(deadline) {
			f.q.pump()
			time.Sleep(5 * time.Millisecond)
		}
	}
	form := &endpointForm{open: true, editing: "gw", id: "gw", baseURL: base, savedURL: base, api: "openai-completions"}
	fetch(form)
	if form.err != "" || form.models != "m1\nm2" {
		t.Fatalf("models %q, error %q", form.models, form.err)
	}

	form = &endpointForm{open: true, editing: "gw", id: "gw", baseURL: "http://127.0.0.1:1/v1", savedURL: base, api: "openai-completions"}
	fetch(form)
	if !strings.Contains(form.err, locales.EndpointsKeyNotSent()) {
		t.Fatalf("error %q", form.err)
	}
}
