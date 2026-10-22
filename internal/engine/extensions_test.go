package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/coding/extension"
)

// mcpTool is a tool of a test MCP server, which replies "<name>: <text>".
// A read-only tool says so with readOnlyHint; another declares that it
// changes things.
type mcpTool struct {
	name     string
	readOnly bool
}

// newMCPServer is a streamable HTTP MCP server with one tool, echo, which
// replies "echo: <text>" and changes nothing.
func newMCPServer(t *testing.T) *httptest.Server {
	s, _ := newMCPServerWith(t, mcpTool{name: "echo", readOnly: true})
	return s
}

// newMCPServerWith is a streamable HTTP MCP server with tools; called
// returns the names of the tools called, in order.
func newMCPServerWith(t *testing.T, tools ...mcpTool) (s *httptest.Server, called func() []string) {
	var mu sync.Mutex
	var calls []string
	listed := []any{}
	for _, tool := range tools {
		listed = append(listed, map[string]any{"name": tool.name, "description": "Reply with the text.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}},
			"annotations": map[string]any{"readOnlyHint": tool.readOnly, "destructiveHint": !tool.readOnly}})
	}
	s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == nil { // a notification
			w.WriteHeader(http.StatusAccepted)
			return
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &p)
			reply["result"] = map[string]any{"protocolVersion": p.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "demo", "version": "1.0.0"}}
		case "tools/list":
			reply["result"] = map[string]any{"tools": listed}
		case "tools/call":
			var p struct {
				Name      string `json:"name"`
				Arguments struct {
					Text string `json:"text"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			mu.Lock()
			calls = append(calls, p.Name)
			mu.Unlock()
			reply["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": p.Name + ": " + p.Arguments.Text}}}
		default:
			reply["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(s.Close)
	return s, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(calls)
	}
}

// waitTool waits until the chat's active tools have name, or have it no
// more.
func waitTool(t *testing.T, c *Chat, name string, present bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if slices.Contains(c.Tools(), name) == present {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waited for %s present=%v: tools %v", name, present, c.Tools())
}

// codemodeCall is a reply that runs script in codemode.
func codemodeCall(script string) func(w io.Writer) {
	args, _ := json.Marshal(map[string]string{"code": script})
	return toolReply("call_1", "codemode", string(args))
}

// A plugin added while a chat is open reaches it at once, as an MCP server
// whose tools codemode scripts call, and leaves it when removed.
func TestPluginReachesOpenChat(t *testing.T) {
	server := newMCPServer(t)
	m := newMock(t, codemodeCall(`return await tools.mcp__demo__echo({ text: "hi" })`), textReply("好了。"))
	e := openEngine(t)
	addMock(t, e, m)
	c, err := e.NewChat(ChatOptions{Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := e.SetPlugin("demo", extension.McpServerConfig{URL: server.URL, Exposure: extension.McpExposureDirect}); err != nil {
		t.Fatal(err)
	}
	if got := e.Plugins(); !slices.Equal(got, []string{"demo"}) {
		t.Errorf("plugins %v", got)
	}
	waitTool(t, c, "mcp__demo__echo", true)
	c.post(func() { c.Send("echo hi") })
	settle(t, c)
	if len(m.requests) != 2 {
		t.Fatalf("%d requests", len(m.requests))
	}
	if body := fmt.Sprint(m.requests[1]["messages"]); !strings.Contains(body, "echo: hi") {
		t.Errorf("the script's result is not sent back: %.600s", body)
	}

	e.RemovePlugin("demo")
	waitTool(t, c, "mcp__demo__echo", false)
	if got := e.Plugins(); len(got) != 0 {
		t.Errorf("plugins %v", got)
	}
}

// Each chat has extensions of its own: closing one, which ends its MCP
// connections, leaves another's working.
func TestChatsKeepTheirOwnExtensions(t *testing.T) {
	server := newMCPServer(t)
	m := newMock(t, codemodeCall(`return await tools.mcp__demo__echo({ text: "two" })`), textReply("好了。"))
	e := openEngine(t)
	addMock(t, e, m)
	if err := e.SetPlugin("demo", extension.McpServerConfig{URL: server.URL, Exposure: extension.McpExposureDirect}); err != nil {
		t.Fatal(err)
	}
	first, err := e.NewChat(ChatOptions{Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.NewChat(ChatOptions{Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	waitTool(t, first, "mcp__demo__echo", true)
	waitTool(t, second, "mcp__demo__echo", true)
	first.Close()

	second.post(func() { second.Send("echo two") })
	settle(t, second)
	if len(m.requests) != 2 {
		t.Fatalf("%d requests", len(m.requests))
	}
	if body := fmt.Sprint(m.requests[1]["messages"]); !strings.Contains(body, "echo: two") {
		t.Errorf("the second chat's MCP tool failed: %.600s", body)
	}
}

func TestPluginConfigIsChecked(t *testing.T) {
	e := openEngine(t)
	if err := e.SetPlugin("demo", extension.McpServerConfig{}); err == nil {
		t.Fatal("a plugin with neither a command nor a URL is accepted")
	}
	if got := e.Plugins(); len(got) != 0 {
		t.Errorf("plugins %v", got)
	}
}

// The servers of mcp.json, which the pig CLI shares, connect as a chat
// starts.
func TestMCPConfig(t *testing.T) {
	server := newMCPServer(t)
	e := openEngine(t)
	config := fmt.Sprintf(`{"mcpServers":{"demo":{"url":%q,"exposure":"direct"}}}`, server.URL)
	if err := os.WriteFile(filepath.Join(e.agentDir, "mcp.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newMock(t)
	addMock(t, e, m)
	c, err := e.NewChat(ChatOptions{Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitTool(t, c, "mcp__demo__echo", true)
}

// A codemode script runs sub-agents at once. Each works on its task alone,
// with the chat's tools, and its reply is the script's result; it stays out
// of the session list.
func TestSubagents(t *testing.T) {
	m := newMock(t,
		codemodeCall(`return (await Promise.all([tools.subagent({ task: "任务一" }), tools.subagent({ task: "任务二" })])).join(" + ")`),
		textReply("结果甲"),
		textReply("结果乙"),
		textReply("两个子任务都完成了。"),
	)
	e := openEngine(t)
	addMock(t, e, m)
	c, err := e.NewChat(ChatOptions{Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.post(func() { c.Send("分两路处理") })
	items := settle(t, c)
	if last := items[len(items)-1]; last.Error != "" || last.Parts[len(last.Parts)-1].Text != "两个子任务都完成了。" {
		t.Fatalf("last item %+v", last)
	}
	if len(m.requests) != 4 {
		t.Fatalf("%d requests", len(m.requests))
	}

	// codemode tells the model of the sub-agents.
	if body := fmt.Sprint(m.requests[0]["tools"]); !strings.Contains(body, "subagent") {
		t.Errorf("codemode does not list subagent: %.600s", body)
	}
	tasks := map[string]bool{}
	for _, child := range m.requests[1:3] {
		if got := toolNames(child); !slices.Equal(got, ReadOnlyTools) {
			t.Errorf("a sub-agent has the tools %v, want %v", got, ReadOnlyTools)
		}
		body := fmt.Sprint(child["messages"])
		if strings.Contains(body, "分两路处理") {
			t.Errorf("a sub-agent sees the chat: %.600s", body)
		}
		for _, task := range []string{"任务一", "任务二"} {
			if strings.Contains(body, task) {
				tasks[task] = true
			}
		}
	}
	if len(tasks) != 2 {
		t.Errorf("the sub-agents got the tasks %v", tasks)
	}
	body := fmt.Sprint(m.requests[3]["messages"])
	if !strings.Contains(body, "结果甲") || !strings.Contains(body, "结果乙") {
		t.Errorf("the replies of the sub-agents are not sent back: %.800s", body)
	}

	var logs []string
	_ = filepath.WalkDir(filepath.Join(e.agentDir, "sessions"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
			logs = append(logs, path)
		}
		return nil
	})
	if len(logs) != 1 {
		t.Errorf("session logs %v, want the chat's alone", logs)
	}
}
