package app

import (
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

	"github.com/egoist/mygo/ui"

	"worktony/internal/engine"
)

// seqLLM is an OpenAI-compatible endpoint that answers each request with
// the next of replies.
func seqLLM(t *testing.T, replies ...func(w io.Writer)) *httptest.Server {
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		var reply func(io.Writer)
		if len(replies) > 0 {
			reply, replies = replies[0], replies[1:]
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if reply == nil {
			reply = sseText(0, "（没有更多回复）")
		}
		reply(flusher{w})
	}))
	t.Cleanup(srv.Close)
	return srv
}

type flusher struct{ w http.ResponseWriter }

func (f flusher) Write(b []byte) (int, error) {
	n, err := f.w.Write(b)
	f.w.(http.Flusher).Flush()
	return n, err
}

func sseChunk(w io.Writer, delta, finish string) {
	f := "null"
	if finish != "" {
		f = fmt.Sprintf("%q", finish)
	}
	fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m1\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":%s}]}\n\n", delta, f)
}

// sseText streams tokens, then, when output is more than 0, the usage that
// reports that many output tokens.
func sseText(output int, tokens ...string) func(io.Writer) {
	return func(w io.Writer) {
		for _, tok := range tokens {
			b, _ := json.Marshal(map[string]string{"content": tok})
			sseChunk(w, string(b), "")
			time.Sleep(20 * time.Millisecond)
		}
		sseChunk(w, "{}", "stop")
		if output > 0 {
			fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m1\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", output, output+10)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

// sseTool calls the tool name with args.
func sseTool(id, name string, args any) func(io.Writer) {
	return func(w io.Writer) {
		raw, _ := json.Marshal(args)
		call, _ := json.Marshal(map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": id, "type": "function",
			"function": map[string]any{"name": name, "arguments": string(raw)},
		}}})
		sseChunk(w, string(call), "")
		sseChunk(w, "{}", "tool_calls")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

// after holds a reply until release closes.
func after(release chan struct{}, reply func(io.Writer)) func(io.Writer) {
	return func(w io.Writer) {
		<-release
		reply(w)
	}
}

func chatTester(f *fixture) *ui.Tester {
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	tt.Frame()
	return tt
}

// A finished reply has a bar under it: copy, branch, and how fast the
// model wrote it.
func TestReplyFooter(t *testing.T) {
	f := newFixture(t, seqLLM(t,
		sseTool("call_1", "ls", map[string]string{"path": "."}),
		sseText(120, "目录里有 **README.md**。", "\n\n要我读一下它吗？"),
		sseText(0, "好，这是分支里的回答。"),
	))
	os.WriteFile(filepath.Join(f.eng.CWD(), "README.md"), []byte("# hi"), 0o644)
	tt := chatTester(f)
	first := f.app.current()
	f.app.send(nil, "看看目录")
	f.until(t, tt, "the reply", func() bool { return !first.chat.Busy && tt.HasText("要我读一下它吗？") })
	tt.Advance(2 * time.Second)
	tt.Frame()
	speed := ""
	for _, s := range tt.Texts() {
		if strings.HasSuffix(s, " tok/s") {
			speed = s
		}
	}
	if speed == "" {
		t.Fatalf("no speed under the reply: %q", tt.Texts())
	}
	shot(t, tt, "chat-reply-footer")

	// One bar for the turn, under its last reply: the copy takes the
	// answer, not the step before it.
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != "目录里有 **README.md**。\n\n要我读一下它吗？" {
		t.Fatalf("copied %q", got)
	}

	if err := tt.Click("从这里创建分支对话"); err != nil {
		t.Fatal(err)
	}
	f.until(t, tt, "the branch", func() bool { return f.app.current() != first })
	branch := f.app.current()
	if !branch.branched || branch.chat == nil || len(branch.chat.Items) != 3 || f.app.tabs[0] != branch {
		t.Fatalf("branch tab %+v", branch)
	}
	f.app.send(nil, "继续")
	f.until(t, tt, "the branch's reply", func() bool { return !branch.chat.Busy && tt.HasText("好，这是分支里的回答。") })
	if len(first.chat.Items) != 3 {
		t.Fatalf("the chat branched from changed: %d items", len(first.chat.Items))
	}
	tt.Advance(10 * time.Second) // the toast goes
	tt.Frame()
	shot(t, tt, "chat-branch")
}

// A codemode script's card lists the calls it makes as it runs; a
// sub-agent's says what it does now.
func TestScriptCallsCard(t *testing.T) {
	release := make(chan struct{})
	f := newFixture(t, seqLLM(t,
		sseTool("call_1", "codemode", map[string]string{"code": `return await tools.subagent({ task: "检查 README 写了什么" })`}),
		sseTool("call_s1", "read", map[string]string{"path": "README.md"}),
		after(release, sseText(0, "README 只有一个标题。")),
		sseText(0, "子代理看过了：README 只有一个标题。"),
	))
	os.WriteFile(filepath.Join(f.eng.CWD(), "README.md"), []byte("# hi"), 0o644)
	tt := chatTester(f)
	tb := f.app.current()
	f.app.send(nil, "让子代理看看 README")
	f.until(t, tt, "the sub-agent's read", func() bool { return tt.HasText("read README.md") })
	for _, s := range []string{"codemode", "子代理", "检查 README 写了什么"} {
		if !tt.HasText(s) {
			t.Errorf("the running script's card lacks %q: %q", s, tt.Texts())
		}
	}
	if tt.HasText("（无输出）") {
		t.Errorf("a running script says it has no output")
	}
	shot(t, tt, "chat-script-running")

	// Open, the sub-agent's row shows its progress whole.
	if err := tt.Click("子代理 检查 README 写了什么"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	shot(t, tt, "chat-script-subagent-open")

	close(release)
	f.until(t, tt, "the answer", func() bool { return !tb.chat.Busy && tt.HasText("子代理看过了：README 只有一个标题。") })
	if !tt.HasText("1 个调用") {
		t.Errorf("the finished script's card should count its calls: %q", tt.Texts())
	}
	tt.Advance(2 * time.Second) // the reveal of streamed text
	shot(t, tt, "chat-script-done")
	// Open, the finished card lists its calls again, over the result.
	if err := tt.Click("codemode " + `return await tools.subagent({ task: "检查 README 写了什么" })`); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("子代理") || tt.HasText("1 个调用") {
		t.Errorf("the open card should list its calls: %q", tt.Texts())
	}
	shot(t, tt, "chat-script-done-open")
	var script *engine.Tool
	for _, it := range tb.chat.Items {
		for _, part := range it.Parts {
			if part.Kind == engine.PartTool && part.Tool.Name == "codemode" {
				script = part.Tool
			}
		}
	}
	if script == nil || len(script.Children) != 1 || script.Children[0].Output != "README 只有一个标题。" {
		t.Fatalf("script %+v", script)
	}
}
