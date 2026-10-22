package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding/extension"
)

// tryTools is a codemode script that calls each tool named, with the text
// "x", and returns what each call gave or why it failed.
func tryTools(names ...string) func(w io.Writer) {
	list, _ := json.Marshal(names)
	return codemodeCall(fmt.Sprintf(`const out = [];
for (const name of %s) {
  try { out.push(name + " ran: " + JSON.stringify(await tools[name]({ text: "x" }))); }
  catch (e) { out.push(name + " failed: " + e.message); }
}
return out.join("\n");`, list))
}

// scriptResult is what the codemode script of a chat's first reply gave,
// as the second request sends it back.
func scriptResult(t *testing.T, m *mockServer) string {
	t.Helper()
	if len(m.requests) < 2 {
		t.Fatalf("%d requests", len(m.requests))
	}
	return fmt.Sprint(m.requests[1]["messages"])
}

// A read-only chat registers only the MCP tools that declare themselves
// read-only: a codemode script can call no other, and the server never
// hears of it. A chat with full tools has them all.
func TestReadOnlyChatMCPTools(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprint("full=", full), func(t *testing.T) {
			server, called := newMCPServerWith(t, mcpTool{name: "read_note", readOnly: true}, mcpTool{name: "write_note"})
			m := newMock(t, tryTools("mcp__notes__read_note", "mcp__notes__write_note"), textReply("好了。"))
			e := openEngine(t)
			addMock(t, e, m)
			if err := e.SetPlugin("notes", extension.McpServerConfig{URL: server.URL}); err != nil {
				t.Fatal(err)
			}
			opts := ChatOptions{Model: "mock/m1"}
			if full {
				opts.Tools = FullTools
			}
			c, err := e.NewChat(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			waitCallable(t, c, "mcp__notes__read_note")
			if got := slices.Contains(c.sess.CallableToolNames(), "mcp__notes__write_note"); got != full {
				t.Fatalf("write_note callable %v in a chat with full tools %v: %v", got, full, c.sess.CallableToolNames())
			}

			c.post(func() { c.Send("take notes") })
			settle(t, c)
			result := scriptResult(t, m)
			if !strings.Contains(result, "read_note ran") || !strings.Contains(result, "read_note: x") {
				t.Errorf("the read-only tool did not run: %.800s", result)
			}
			want := []string{"read_note"}
			if full {
				want = append(want, "write_note")
				if !strings.Contains(result, "write_note: x") {
					t.Errorf("the chat with full tools did not write: %.800s", result)
				}
			} else if !strings.Contains(result, "write_note failed") {
				t.Errorf("the read-only chat's script reached write_note: %.800s", result)
			}
			if got := called(); !slices.Equal(got, want) {
				t.Errorf("the server got calls of %v, want %v", got, want)
			}
		})
	}
}

// waitCallable waits until codemode scripts of the chat can call name.
func waitCallable(t *testing.T, c *Chat, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if slices.Contains(c.sess.CallableToolNames(), name) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s is not callable: %v", name, c.sess.CallableToolNames())
}

// probeTools registers, from the chat's plugin extension as any extension
// may while the chat runs, a tool touch that declares nothing and a tool
// peek that declares itself read-only, both of exposure. ran returns the
// tools that ran.
func probeTools(t *testing.T, c *Chat, exposure extension.ToolExposure) (ran func() []string) {
	t.Helper()
	var mu sync.Mutex
	var names []string
	c.plugins.mu.Lock()
	pi := c.plugins.pi
	c.plugins.mu.Unlock()
	for _, tool := range []struct {
		name        string
		annotations *extension.ToolAnnotations
	}{{"touch", nil}, {"peek", &extension.ToolAnnotations{ReadOnlyHint: new(true)}}} {
		pi.RegisterTool(extension.ToolDefinition{
			Name: tool.name, Label: tool.name, Description: "A probe.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
			Exposure:    exposure,
			Annotations: tool.annotations,
			Execute: func(context.Context, string, json.RawMessage, extension.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
				mu.Lock()
				names = append(names, tool.name)
				mu.Unlock()
				return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: tool.name + " done"}}}, nil
			},
		})
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(names)
	}
}

// Whichever extension registers a tool, and whenever, a read-only chat runs
// it only when it declares itself read-only: neither a codemode script nor
// the model itself gets past the guard. A chat with full tools runs both.
func TestReadOnlyGuard(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprint("codemode full=", full), func(t *testing.T) {
			m := newMock(t, tryTools("touch", "peek"), textReply("好了。"))
			e := openEngine(t)
			addMock(t, e, m)
			opts := ChatOptions{Model: "mock/m1"}
			if full {
				opts.Tools = FullTools
			}
			c, err := e.NewChat(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ran := probeTools(t, c, extension.ToolExposureCodemode)
			c.post(func() { c.Send("probe") })
			settle(t, c)
			result := scriptResult(t, m)
			want := []string{"peek"}
			if full {
				want = []string{"touch", "peek"}
			} else if !strings.Contains(result, "touch is not available in a read-only chat") {
				t.Errorf("the script was not told why touch failed: %.800s", result)
			}
			if got := ran(); !slices.Equal(got, want) {
				t.Errorf("ran %v, want %v; script result %.800s", got, want, result)
			}
		})
	}

	t.Run("direct", func(t *testing.T) {
		m := newMock(t, toolReply("call_1", "touch", `{"text":"x"}`), toolReply("call_2", "peek", `{"text":"x"}`), textReply("好了。"))
		e := openEngine(t)
		addMock(t, e, m)
		c, err := e.NewChat(ChatOptions{Model: "mock/m1"})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		ran := probeTools(t, c, extension.ToolExposureDirect)
		waitTool(t, c, "touch", true)
		c.post(func() { c.Send("probe") })
		items := settle(t, c)
		if got := ran(); !slices.Equal(got, []string{"peek"}) {
			t.Errorf("ran %v, want [peek]", got)
		}
		var touch *Tool
		for _, it := range items {
			for _, part := range it.Parts {
				if part.Tool != nil && part.Tool.Name == "touch" {
					touch = part.Tool
				}
			}
		}
		if touch == nil || touch.Status != ToolFailed || !strings.Contains(touch.Output, "read-only chat") {
			t.Errorf("the touch card %+v", touch)
		}
	})
}

// A tool of the app's own name from another extension is not the app's:
// only the source makes the built-in read tools, codemode and the
// sub-agents run in a read-only chat.
func TestReadingToolSource(t *testing.T) {
	for _, tc := range []struct {
		info extension.ToolInfo
		want bool
	}{
		{extension.ToolInfo{Name: "read", SourceInfo: extension.SourceInfo{Path: "builtin:read"}}, true},
		{extension.ToolInfo{Name: "bash", SourceInfo: extension.SourceInfo{Path: "builtin:bash"}}, false},
		{extension.ToolInfo{Name: "read", SourceInfo: extension.SourceInfo{Path: "/ext/read.ts"}}, false},
		{extension.ToolInfo{Name: "codemode", SourceInfo: extension.SourceInfo{Path: "<inline:codemode>"}}, true},
		{extension.ToolInfo{Name: "codemode", SourceInfo: extension.SourceInfo{Path: "/ext/codemode.ts"}}, false},
		{extension.ToolInfo{Name: "subagent", SourceInfo: extension.SourceInfo{Path: "<inline:worktony-subagents>"}}, true},
		{extension.ToolInfo{Name: "subagent", SourceInfo: extension.SourceInfo{Path: "<inline:worktony-plugins>"}}, false},
		{extension.ToolInfo{Name: "mcp__notes__read", SourceInfo: extension.SourceInfo{Path: "<inline:mcp>"},
			Annotations: &extension.ToolAnnotations{ReadOnlyHint: new(true)}}, true},
		{extension.ToolInfo{Name: "mcp__notes__write", SourceInfo: extension.SourceInfo{Path: "<inline:mcp>"},
			Annotations: &extension.ToolAnnotations{ReadOnlyHint: new(false)}}, false},
	} {
		if got := readingTool(tc.info); got != tc.want {
			t.Errorf("readingTool(%s from %s) = %v", tc.info.Name, tc.info.SourceInfo.Path, got)
		}
	}
}

// serversAPI is the part of an extension API a pluginLink uses: it keeps
// the MCP servers registered. during runs inside the first registration.
type serversAPI struct {
	extension.API
	mu      sync.Mutex
	servers map[string]bool
	during  func()
}

func (a *serversAPI) RegisterMcpServer(name string, _ extension.McpServerConfig) error {
	a.mu.Lock()
	a.servers[name] = true
	during := a.during
	a.during = nil
	a.mu.Unlock()
	if during != nil {
		during()
	}
	return nil
}

func (a *serversAPI) UnregisterMcpServer(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.servers, name)
}

func (a *serversAPI) names() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var names []string
	for name := range a.servers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Plugins added and removed while a chat links to them, after it read
// them and before it is done, reach it all the same.
func TestPluginChangesWhileLinking(t *testing.T) {
	var plugins pluginSet
	config := extension.McpServerConfig{URL: "http://localhost:1/mcp"}
	plugins.set("a", config)
	plugins.set("b", config)
	api := &serversAPI{servers: map[string]bool{}}
	api.during = func() {
		plugins.set("c", config)
		plugins.remove("b")
	}
	link := newPluginLink(&plugins)
	if err := link.extension(api); err != nil {
		t.Fatal(err)
	}
	defer link.stop()
	want := []string{"a", "c"}
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Equal(api.names(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("the chat has the plugins %v, want %v", api.names(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
