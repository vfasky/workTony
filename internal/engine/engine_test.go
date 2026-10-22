package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockServer is an OpenAI-compatible endpoint that streams the replies it
// is given, one per request, and records the requests.
type mockServer struct {
	*httptest.Server
	mu       sync.Mutex
	replies  []func(w io.Writer)
	auth     []string
	requests []map[string]any
}

func newMock(t *testing.T, replies ...func(w io.Writer)) *mockServer {
	m := &mockServer{replies: replies}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"m2"},{"id":"m1"}]}`)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.auth = append(m.auth, r.Header.Get("Authorization"))
		m.requests = append(m.requests, body)
		var reply func(io.Writer)
		if len(m.replies) > 0 {
			reply, m.replies = m.replies[0], m.replies[1:]
		}
		m.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if reply == nil {
			reply = textReply("（没有更多回复）")
		}
		reply(flushWriter{w})
	}))
	t.Cleanup(m.Close)
	return m
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(b []byte) (int, error) {
	n, err := f.w.Write(b)
	f.w.(http.Flusher).Flush()
	return n, err
}

func chunk(w io.Writer, delta string, finish string) {
	f := "null"
	if finish != "" {
		f = fmt.Sprintf("%q", finish)
	}
	fmt.Fprintf(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m1\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":%s}]}\n\n", delta, f)
}

func textReply(tokens ...string) func(io.Writer) {
	return func(w io.Writer) {
		for _, tok := range tokens {
			b, _ := json.Marshal(map[string]string{"content": tok})
			chunk(w, string(b), "")
			time.Sleep(5 * time.Millisecond)
		}
		chunk(w, "{}", "stop")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

func toolReply(id, name, args string) func(io.Writer) {
	return func(w io.Writer) {
		call, _ := json.Marshal(map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": id, "type": "function",
			"function": map[string]any{"name": name, "arguments": args},
		}}})
		chunk(w, string(call), "")
		chunk(w, "{}", "tool_calls")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

func openEngine(t *testing.T) *Engine {
	t.Helper()
	cwd := t.TempDir()
	os.WriteFile(filepath.Join(cwd, "hello.txt"), []byte("hi"), 0o644)
	agentDir := t.TempDir()
	// No automatic retries, so a failing request fails at once.
	os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(`{"retry":{"enabled":false}}`), 0o600)
	e, err := Open(Options{AgentDir: agentDir, CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}

func addMock(t *testing.T, e *Engine, m *mockServer) {
	t.Helper()
	err := e.SaveCustomProvider(context.Background(), CustomProvider{ID: "mock", Name: "Mock", BaseURL: m.URL + "/v1", API: "openai-completions", Models: []string{"m1"}}, "sk-test")
	if err != nil {
		t.Fatal(err)
	}
}

// settle waits for the chat to finish its turn and returns its items.
func settle(t *testing.T, c *Chat) []*Item {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var busy bool
		var items []*Item
		c.post(func() { busy, items = c.Busy, append([]*Item(nil), c.Items...) })
		if !busy {
			return items
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the chat did not settle")
	return nil
}

func TestCustomProviderChat(t *testing.T) {
	m := newMock(t, textReply("你好", "，我是", " **mock**", " 模型。"))
	e := openEngine(t)
	addMock(t, e, m)

	models, err := e.AvailableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	model, ok := PickModel(models, "mock/m1", "")
	if !ok || model.Ref != "mock/m1" || model.ProviderName != "Mock" {
		t.Fatalf("mock/m1 is not available: %+v", models)
	}
	c, err := e.NewChat(ChatOptions{Model: model.Ref})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.post(func() { c.Send("hello") })
	items := settle(t, c)
	if len(items) != 2 || items[0].Role != RoleUser || items[1].Role != RoleAssistant {
		t.Fatalf("items %+v", items)
	}
	reply := items[1]
	if reply.Streaming || reply.Error != "" || len(reply.Parts) != 1 || reply.Parts[0].Text != "你好，我是 **mock** 模型。" {
		t.Fatalf("reply %+v", reply)
	}
	if c.Title() != "hello" {
		t.Errorf("title %q", c.Title())
	}
	if m.auth[0] != "Bearer sk-test" {
		t.Errorf("auth %q: the key should come from auth.json", m.auth[0])
	}
	// The default Pi prompt, with the read-only tools.
	msgs := m.requests[0]["messages"].([]any)
	if sys := fmt.Sprint(msgs[0]); !strings.Contains(sys, "coding assistant") {
		t.Errorf("system prompt %.200s", sys)
	}
	if tools := m.requests[0]["tools"].([]any); len(tools) != len(ReadOnlyTools) {
		t.Errorf("%d tools sent, want %d", len(tools), len(ReadOnlyTools))
	}
}

func TestToolCall(t *testing.T) {
	m := newMock(t, toolReply("call_1", "ls", `{"path":"."}`), textReply("目录里有 hello.txt。"))
	e := openEngine(t)
	addMock(t, e, m)
	c, err := e.NewChat(ChatOptions{Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.post(func() { c.Send("列一下文件") })
	items := settle(t, c)
	if len(items) != 3 {
		t.Fatalf("%d items: %+v", len(items), items)
	}
	call := items[1]
	if len(call.Parts) != 1 || call.Parts[0].Kind != PartTool {
		t.Fatalf("tool item %+v", call)
	}
	tool := call.Parts[0].Tool
	if tool.Name != "ls" || tool.Summary != "." || tool.Status != ToolDone || !strings.Contains(tool.Output, "hello.txt") {
		t.Fatalf("tool %+v", tool)
	}
	if got := items[2].Parts[0].Text; got != "目录里有 hello.txt。" {
		t.Fatalf("final reply %q", got)
	}
}

func TestProviderError(t *testing.T) {
	e := openEngine(t)
	// Nothing listens there.
	err := e.SaveCustomProvider(context.Background(), CustomProvider{ID: "down", BaseURL: "http://127.0.0.1:1/v1", API: "openai-completions", Models: []string{"x"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.NewChat(ChatOptions{Model: "down/x"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.post(func() { c.Send("hi") })
	items := settle(t, c)
	last := items[len(items)-1]
	if last.Role != RoleAssistant || last.Error == "" {
		t.Fatalf("want an error reply, got %+v", last)
	}
}

func TestAPIKeyLoginLogout(t *testing.T) {
	e := openEngine(t)
	find := func(id string) Provider {
		for _, p := range e.Providers(context.Background()) {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("no provider %s", id)
		return Provider{}
	}
	if find("deepseek").State.Source == "stored" {
		t.Fatal("deepseek starts stored")
	}
	if err := e.SaveAPIKey(context.Background(), "deepseek", "  sk-deep  ", Interaction{}); err != nil {
		t.Fatal(err)
	}
	p := find("deepseek")
	if !p.State.Configured || p.State.Source != "stored" || p.State.Kind != KindAPIKey {
		t.Fatalf("after saving: %+v", p.State)
	}
	b, _ := os.ReadFile(filepath.Join(e.AgentDir(), "auth.json"))
	if !strings.Contains(string(b), `"sk-deep"`) {
		t.Fatalf("auth.json %s", b)
	}
	if err := e.Logout(context.Background(), "deepseek"); err != nil {
		t.Fatal(err)
	}
	if p := find("deepseek"); p.State.Source == "stored" {
		t.Fatalf("after logout: %+v", p.State)
	}
	openai := find("openai")
	if openai.OAuth == nil || openai.OAuth.Label != "Sign in with ChatGPT" {
		t.Fatalf("openai OAuth %+v", openai.OAuth)
	}
}

func TestCustomProviderKeepsModelsJSON(t *testing.T) {
	e := openEngine(t)
	path := filepath.Join(e.AgentDir(), "models.json")
	os.WriteFile(path, []byte(`{"providers":{"ollama":{"baseUrl":"http://localhost:11434/v1","api":"openai-completions","apiKey":"ollama","models":[{"id":"llama3.2","contextWindow":128000}]}},"other":1}`), 0o600)
	err := e.SaveCustomProvider(context.Background(), CustomProvider{ID: "ollama", Name: "Ollama", BaseURL: "http://localhost:11434/v1/", API: "openai-completions", Models: []string{"llama3.2", "qwen3"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]any
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &f)
	o := f["providers"].(map[string]any)["ollama"].(map[string]any)
	models := o["models"].([]any)
	if f["other"] != 1.0 || o["apiKey"] != "ollama" || o["baseUrl"] != "http://localhost:11434/v1" || len(models) != 2 || models[0].(map[string]any)["contextWindow"] != 128000.0 {
		t.Fatalf("models.json %s", b)
	}
	list, _ := e.CustomProviders()
	if len(list) != 1 || list[0].Name != "Ollama" || len(list[0].Models) != 2 {
		t.Fatalf("custom providers %+v", list)
	}
	if err := e.SaveCustomProvider(context.Background(), CustomProvider{ID: "openai", BaseURL: "http://x", API: "openai-completions", Models: []string{"a"}}, ""); err == nil {
		t.Fatal("a built-in ID should be refused")
	}
	if err := e.DeleteCustomProvider(context.Background(), "ollama"); err != nil {
		t.Fatal(err)
	}
	if list, _ := e.CustomProviders(); len(list) != 0 {
		t.Fatalf("after delete %+v", list)
	}
}

func TestFetchModelIDs(t *testing.T) {
	m := newMock(t)
	ids, err := FetchModelIDs(context.Background(), m.URL+"/v1/", "k")
	if err != nil || strings.Join(ids, ",") != "m1,m2" {
		t.Fatalf("ids %v, %v", ids, err)
	}
}
