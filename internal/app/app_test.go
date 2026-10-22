package app

import (
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"worktony/internal/engine"
	"worktony/internal/store"
)

// queue stands in for the main thread: functions posted from other
// goroutines wait until the test pumps them between frames.
type queue struct {
	mu  sync.Mutex
	fns []func()
}

func (q *queue) post(fn func()) {
	q.mu.Lock()
	q.fns = append(q.fns, fn)
	q.mu.Unlock()
}

func (q *queue) pump() int {
	q.mu.Lock()
	fns := q.fns
	q.fns = nil
	q.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
	return len(fns)
}

// mockLLM streams tokens as an OpenAI-compatible endpoint.
func mockLLM(t *testing.T, reply ...string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, tok := range reply {
			b, _ := json.Marshal(map[string]string{"content": tok})
			fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m1\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]}\n\n", b)
			w.(http.Flusher).Flush()
			time.Sleep(3 * time.Millisecond)
		}
		fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

type fixture struct {
	app   *App
	eng   *engine.Engine
	db    *store.Store
	q     *queue
	pages []string
}

func newFixture(t *testing.T, llm *httptest.Server) *fixture {
	t.Helper()
	cwd := t.TempDir()
	eng, err := engine.Open(engine.Options{AgentDir: t.TempDir(), CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	// No retries, so a failing request fails at once.
	os.WriteFile(filepath.Join(eng.AgentDir(), "settings.json"), []byte(`{"retry":{"enabled":false}}`), 0o600)
	if llm != nil {
		err := eng.SaveCustomProvider(t.Context(), engine.CustomProvider{ID: "mock", Name: "Mock", BaseURL: llm.URL + "/v1", API: "openai-completions", Models: []string{"m1", "m2"}}, "sk-mock")
		if err != nil {
			t.Fatal(err)
		}
		if err := eng.SetDefaultModel("mock/m1"); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "worktony.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &fixture{eng: eng, db: db, q: &queue{}}
	f.app = New(eng, Host{
		Post:         f.q.post,
		OpenSettings: func(page string) { f.pages = append(f.pages, page) },
	}, db)
	f.app.RefreshSync()
	t.Cleanup(f.app.Close)
	return f
}

// until pumps and draws frames until ok holds.
func (f *fixture) until(t *testing.T, tt *ui.Tester, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		f.q.pump()
		tt.Advance(50 * time.Millisecond) // streamed text comes in at a pace
		tt.Frame()
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; texts %q", what, tt.Texts())
}

func shot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("WORKTONY_SHOTS")
	if dir == "" {
		return
	}
	tt.Frame()
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestChatSendsAndStreams(t *testing.T) {
	f := newFixture(t, mockLLM(t, "## 你好\n\n", "我是 **Pi**，", "可以帮你阅读和理解代码。\n\n", "```go\nfmt.Println(\"hi\")\n```\n"))
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	tt.Frame()
	if !tt.HasText("有什么可以帮你？") || !tt.HasText("m1") {
		t.Fatalf("empty state texts %q", tt.Texts())
	}
	shot(t, tt, "chat-empty")
	if err := tt.Click("消息"); err != nil {
		t.Fatal(err)
	}
	tt.Type("介绍一下你自己")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if f.app.draft != "" {
		t.Fatalf("Enter should send and clear the draft, it is %q", f.app.draft)
	}
	tb := f.app.current()
	if tb.chat == nil {
		t.Fatal("no chat started")
	}
	f.until(t, tt, "the reply", func() bool { return !tb.chat.Busy && tt.HasText("可以帮你阅读和理解代码。") })
	if !tt.HasText("介绍一下你自己") {
		t.Errorf("the user message is not shown: %q", tt.Texts())
	}
	tt.Advance(2 * time.Second) // the reveal of streamed text
	shot(t, tt, "chat-reply")
	tt.SetDark(true)
	tt.Frame()
	shot(t, tt, "chat-reply-dark")
	// The sidebar is titled by the first message.
	if !tt.HasText("介绍一下你自己") {
		t.Errorf("sidebar title missing")
	}
}

// toolLLM asks for an ls of the working directory, then answers.
func toolLLM(t *testing.T) *httptest.Server {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			fmt.Fprint(w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m1","choices":[{"index":0,"delta":{"content":"我先看看目录里有什么。"},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"ls","arguments":"{\"path\":\".\"}"}}]},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m1","choices":[{"index":0,"delta":{"content":"目录里有 **README.md** 和 **main.go** 两个文件。"},"finish_reason":null}]}`+"\n\n")
		fmt.Fprint(w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestToolCard(t *testing.T) {
	f := newFixture(t, toolLLM(t))
	os.WriteFile(filepath.Join(f.eng.CWD(), "README.md"), []byte("# hi"), 0o644)
	os.WriteFile(filepath.Join(f.eng.CWD(), "main.go"), []byte("package main"), 0o644)
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	tt.Frame()
	f.app.send(nil, "列一下文件")
	tb := f.app.current()
	f.until(t, tt, "the answer", func() bool { return !tb.chat.Busy && tt.HasText("两个文件。") })
	if !tt.HasText("列出目录") {
		t.Fatalf("no tool card: %q", tt.Texts())
	}
	if err := tt.Click("列出目录 ."); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("README.md\nmain.go") && !tt.HasText("main.go\nREADME.md") {
		t.Errorf("the expanded card should show the listing: %q", tt.Texts())
	}
	shot(t, tt, "chat-tool")
}

func TestChatWithoutModels(t *testing.T) {
	f := newFixture(t, nil)
	f.app.models = nil // as on a machine without any credential
	f.app.current().model = ""
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.Frame()
	if !tt.HasText("先连接一个模型") {
		t.Fatalf("texts %q", tt.Texts())
	}
	shot(t, tt, "chat-connect")
	if err := tt.Click("添加 API Key"); err != nil {
		t.Fatal(err)
	}
	if len(f.pages) != 1 || f.pages[0] != pageAPIKeys {
		t.Fatalf("opened settings pages %q", f.pages)
	}
}

func TestSettingsSavesAPIKey(t *testing.T) {
	f := newFixture(t, nil)
	tt := ui.NewTester(f.app.SettingsView, 860, 620)
	tt.SetLocale(testLocale)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	tt.Frame()
	if !tt.HasText("使用 ChatGPT 登录") || !tt.HasText("Claude") || !tt.HasText("GitHub Copilot") {
		t.Fatalf("accounts page texts %q", tt.Texts())
	}
	shot(t, tt, "settings-accounts")
	if err := tt.Click("API Key"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	shot(t, tt, "settings-apikeys-list")
	f.app.settings.keyQuery = "deep"
	tt.Frame()
	if err := tt.Click("设置 DeepSeek API Key"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	tt.Frame()
	tt.Type("sk-deepseek-test")
	shot(t, tt, "settings-apikeys")
	if err := tt.Click("保存 DeepSeek API Key"); err != nil {
		t.Fatal(err)
	}
	f.until(t, tt, "the key saved", func() bool {
		return f.app.settings.login != nil && f.app.settings.login.done && f.app.settings.keyEditing == ""
	})
	f.app.RefreshSync()
	tt.Frame()
	b, _ := os.ReadFile(filepath.Join(f.eng.AgentDir(), "auth.json"))
	if !strings.Contains(string(b), "sk-deepseek-test") {
		t.Fatalf("auth.json %s", b)
	}
	if !tt.HasText("已保存 API Key") {
		t.Fatalf("texts %q", tt.Texts())
	}
}

// Cloudflare asks for its account ID after the key: the sign-in's prompt
// shows in the dialog.
func TestLoginPromptDialog(t *testing.T) {
	f := newFixture(t, nil)
	tt := ui.NewTester(f.app.SettingsView, 860, 620)
	tt.SetLocale(testLocale)
	f.app.settings.page = pageAPIKeys
	tt.Frame()
	flow := f.app.startLogin("cloudflare-workers-ai", loginAPIKey, "cf-key")
	f.until(t, tt, "the account ID prompt", func() bool { return flow.prompt != nil || flow.err != "" || flow.done })
	if flow.prompt == nil {
		t.Fatalf("no prompt: err %q done %v", flow.err, flow.done)
	}
	if !flow.open {
		t.Fatal("a prompt should open the dialog")
	}
	shot(t, tt, "login-prompt")
	tt.Type("acct-123")
	tt.Key(0, ui.KeyEnter)
	f.until(t, tt, "the sign-in", func() bool { return flow.done || flow.err != "" })
	if flow.err != "" {
		t.Fatal(flow.err)
	}
	b, _ := os.ReadFile(filepath.Join(f.eng.AgentDir(), "auth.json"))
	if !strings.Contains(string(b), "acct-123") || !strings.Contains(string(b), "cf-key") {
		t.Fatalf("auth.json %s", b)
	}
}

func TestLoginDialogStates(t *testing.T) {
	f := newFixture(t, nil)
	tt := ui.NewTester(f.app.SettingsView, 860, 620)
	tt.SetLocale(testLocale)
	f.app.settings.login = &loginFlow{providerID: "github-copilot", name: "GitHub Copilot", open: true, code: "ABCD-1234", codeURL: "https://github.com/login/device", status: "等待授权"}
	tt.Frame()
	if !tt.HasText("ABCD-1234") {
		t.Fatalf("texts %q", tt.Texts())
	}
	shot(t, tt, "login-device-code")
	f.app.settings.login = &loginFlow{providerID: "openai", name: "ChatGPT", open: true, url: "https://auth.openai.com/oauth/authorize?x", status: "已在浏览器中打开登录页面",
		prompt: &engine.Prompt{Kind: "manual_code", Message: "粘贴授权码"}, reply: make(chan answer, 1)}
	tt.Frame()
	if !tt.HasText("复制登录链接") || !tt.HasText("粘贴授权码") {
		t.Fatalf("texts %q", tt.Texts())
	}
	shot(t, tt, "login-browser")
	// Cancel stops it.
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.app.settings.login.open || !f.app.settings.login.done {
		t.Fatal("cancel should close and stop the sign-in")
	}
}

func TestEndpointForm(t *testing.T) {
	f := newFixture(t, nil)
	tt := ui.NewTester(f.app.SettingsView, 860, 620)
	tt.SetLocale(testLocale)
	f.app.settings.page = pageEndpoints
	tt.Frame()
	if err := tt.Click("添加端点"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Ollama"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	form := f.app.settings.form
	if form == nil || form.id != "ollama" || form.baseURL != "http://localhost:11434/v1" {
		t.Fatalf("form %+v", form)
	}
	form.models = "llama3.2\nqwen3:8b"
	tt.Frame()
	shot(t, tt, "settings-endpoint-form")
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	f.until(t, tt, "the endpoint saved", func() bool { return f.app.settings.form == nil })
	f.app.RefreshSync()
	tt.Frame()
	if len(f.app.customs) != 1 || f.app.customs[0].ID != "ollama" || len(f.app.customs[0].Models) != 2 {
		t.Fatalf("customs %+v", f.app.customs)
	}
	if !tt.HasText("http://localhost:11434/v1") {
		t.Fatalf("texts %q", tt.Texts())
	}
	shot(t, tt, "settings-endpoints")
	found := false
	for _, m := range f.app.models {
		found = found || m.Ref == "ollama/qwen3:8b"
	}
	if !found {
		t.Fatalf("the endpoint's models are not available: %+v", f.app.models)
	}
	f.app.settings.page = pageGeneral
	tt.Frame()
	shot(t, tt, "settings-general")
}
