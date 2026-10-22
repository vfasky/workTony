package engine

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// usageReply streams tokens, then reports output tokens of usage, as
// providers do in a last chunk.
func usageReply(output int, tokens ...string) func(io.Writer) {
	return func(w io.Writer) {
		for _, tok := range tokens {
			chunk(w, fmt.Sprintf("{\"content\":%q}", tok), "")
			time.Sleep(5 * time.Millisecond)
		}
		chunk(w, "{}", "stop")
		fmt.Fprintf(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m1\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", output, output+10)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

// A reply keeps its output tokens and how long it took to write, which
// give its speed.
func TestReplySpeed(t *testing.T) {
	m := newMock(t, usageReply(42, "一", "二", "三"))
	e := openEngine(t)
	addMock(t, e, m)
	c, err := e.NewChat(ChatOptions{Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.post(func() { c.Send("hi") })
	items := settle(t, c)
	reply := items[1]
	if reply.Tokens != 42 || reply.GenTime < 10*time.Millisecond || reply.GenTime > 10*time.Second {
		t.Fatalf("tokens %d in %v", reply.Tokens, reply.GenTime)
	}
}

// A branch goes on from a reply with the conversation up to it, in a
// session of its own; the chat it came from goes on as it was.
func TestBranch(t *testing.T) {
	m := newMock(t, usageReply(42, "答一"), textReply("答二"), textReply("接着答一"), textReply("答三"))
	e := openEngine(t)
	addMock(t, e, m)
	c, err := e.NewChat(ChatOptions{Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.post(func() { c.Send("第一问") })
	settle(t, c)
	c.post(func() { c.Send("第二问") })
	items := settle(t, c)
	if got := roles(items); got != "U:第一问 A U:第二问 A" {
		t.Fatalf("transcript %q", got)
	}
	path := c.sess.Path()

	if _, err := e.Branch(c, items[0], nil); err == nil {
		t.Fatal("a user message cannot be branched from")
	}
	b, err := e.Branch(c, items[1], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := roles(b.Items); got != "U:第一问 A" {
		t.Fatalf("branch transcript %q", got)
	}
	reply := b.Items[1]
	if reply.Parts[0].Text != "答一" || reply.Tokens != 42 || reply.Model != "m1" {
		t.Fatalf("branch reply %+v", reply)
	}
	if b.Title() != "第一问" || b.CWD() != c.CWD() || b.Model().Ref != "mock/m1" {
		t.Fatalf("branch title %q, directory %q, model %q", b.Title(), b.CWD(), b.Model().Ref)
	}

	b.post(func() { b.Send("追问") })
	settle(t, b)
	if body := fmt.Sprint(m.requests[2]["messages"]); !strings.Contains(body, "第一问") || !strings.Contains(body, "答一") || strings.Contains(body, "第二问") {
		t.Fatalf("the branch's request: %.600s", body)
	}
	if got := roles(b.Items); got != "U:第一问 A U:追问 A" {
		t.Fatalf("branch transcript %q", got)
	}

	c.post(func() { c.Send("第三问") })
	settle(t, c)
	if body := fmt.Sprint(m.requests[3]["messages"]); !strings.Contains(body, "第二问") || strings.Contains(body, "追问") {
		t.Fatalf("the first chat's request: %.600s", body)
	}
	if c.sess.Path() != path || b.sess.Path() == path || b.sess.Path() == "" {
		t.Fatalf("sessions %q and %q, the first was %q", c.sess.Path(), b.sess.Path(), path)
	}
}

// The calls of a codemode script come under its card as they run: a
// sub-agent tells each tool it calls and the reply it writes, and ends
// with the reply. A branch shows the calls again.
func TestScriptCallsAndProgress(t *testing.T) {
	m := newMock(t,
		codemodeCall(`return await tools.subagent({ task: "列一下文件" })`),
		toolReply("call_s1", "ls", `{"path":"."}`),
		textReply("有 hello.txt。"),
		textReply("子代理说有 hello.txt。"),
	)
	e := openEngine(t)
	addMock(t, e, m)
	// The progress a running sub-agent shows, as each posted function
	// leaves it.
	var mu sync.Mutex
	var c *Chat
	var shown []string
	post := func(fn func()) {
		mu.Lock()
		defer mu.Unlock()
		fn()
		if c == nil {
			return
		}
		for _, it := range c.Items {
			for _, part := range it.Parts {
				if part.Kind != PartTool {
					continue
				}
				for _, child := range part.Tool.Children {
					if child.Status == ToolRunning && child.Output != "" && !slices.Contains(shown, child.Output) {
						shown = append(shown, child.Output)
					}
				}
			}
		}
	}
	chat, err := e.NewChat(ChatOptions{Model: "mock/m1", Post: post})
	if err != nil {
		t.Fatal(err)
	}
	defer chat.Close()
	mu.Lock()
	c = chat
	mu.Unlock()
	c.post(func() { c.Send("看看有什么文件") })
	items := settle(t, c)

	script := items[1].Parts[0].Tool
	if script.Name != "codemode" || script.Status != ToolDone || len(script.Children) != 1 {
		t.Fatalf("script %+v", script)
	}
	sub := script.Children[0]
	if sub.Name != "subagent" || sub.Summary != "列一下文件" || sub.Status != ToolDone || sub.Output != "有 hello.txt。" {
		t.Fatalf("sub-agent call %+v", sub)
	}
	if !slices.ContainsFunc(shown, func(s string) bool { return strings.HasPrefix(s, "ls .") }) {
		t.Fatalf("the sub-agent's progress did not show its ls: %q", shown)
	}
	// The script's own card holds no nested call as a part of its own.
	for _, it := range items {
		for _, part := range it.Parts {
			if part.Kind == PartTool && part.Tool.Name == "subagent" {
				t.Fatalf("the sub-agent call shows as a call of the chat: %+v", it)
			}
		}
	}

	b, err := e.Branch(c, items[len(items)-1], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	restored := b.Items[1].Parts[0].Tool
	if restored.Name != "codemode" || restored.Status != ToolDone || !strings.Contains(restored.Output, "有 hello.txt。") || len(restored.Children) != 1 {
		t.Fatalf("restored script %+v", restored)
	}
	if child := restored.Children[0]; child.Name != "subagent" || child.Summary != "列一下文件" || child.Status != ToolDone {
		t.Fatalf("restored sub-agent call %+v", child)
	}
}
