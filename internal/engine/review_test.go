package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// postQueue holds posted functions until the test runs them, as a busy
// main thread would.
type postQueue struct {
	mu  sync.Mutex
	fns []func()
}

func (q *postQueue) post(fn func()) {
	q.mu.Lock()
	q.fns = append(q.fns, fn)
	q.mu.Unlock()
}

func (q *postQueue) pump() {
	for {
		q.mu.Lock()
		fns := q.fns
		q.fns = nil
		q.mu.Unlock()
		if len(fns) == 0 {
			return
		}
		for _, fn := range fns {
			fn()
		}
	}
}

// settleQueue pumps until the chat is idle with nothing queued.
func settleQueue(t *testing.T, q *postQueue, c *Chat) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		q.pump()
		if !c.Busy && c.Queued() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the chat did not settle: busy %v, %d queued", c.Busy, c.Queued())
}

// lastUser is the text of the last user message of a request.
func lastUser(req map[string]any) string {
	msgs, _ := req["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if m["role"] != "user" {
			continue
		}
		switch v := m["content"].(type) {
		case string:
			return v
		case []any:
			var sb strings.Builder
			for _, part := range v {
				if p, ok := part.(map[string]any); ok {
					s, _ := p["text"].(string)
					sb.WriteString(s)
				}
			}
			return sb.String()
		}
	}
	return ""
}

func roles(items []*Item) string {
	var sb strings.Builder
	for _, it := range items {
		switch it.Role {
		case RoleUser:
			sb.WriteString("U:" + it.Text + " ")
		case RoleAssistant:
			sb.WriteString("A ")
		default:
			sb.WriteString("N:" + it.Text + " ")
		}
	}
	return strings.TrimSpace(sb.String())
}

// A message sent after a turn ended but before the transcript heard of it,
// while Busy still reads true, is answered.
func TestMessageQueuedAsTurnEnds(t *testing.T) {
	m := newMock(t, textReply("一"), textReply("二"))
	e := openEngine(t)
	addMock(t, e, m)
	q := &postQueue{}
	c, err := e.NewChat(ChatOptions{Model: "mock/m1", Post: q.post})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Send("A")
	// The reply streams in and Prompt returns; the transcript has not run
	// what was posted, so it is still busy.
	deadline := time.Now().Add(10 * time.Second)
	for {
		m.mu.Lock()
		n := len(m.requests)
		m.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if !c.Busy {
		t.Fatal("the transcript should not have heard the turn end yet")
	}
	c.Send("B")
	settleQueue(t, q, c)
	if got := roles(c.Items); got != "U:A A U:B A" {
		t.Fatalf("transcript %q", got)
	}
	if len(m.requests) != 2 || lastUser(m.requests[1]) != "B" {
		t.Fatalf("%d requests; the second asks %q", len(m.requests), lastUser(m.requests[len(m.requests)-1]))
	}
	if c.Items[3].Parts[0].Text != "二" {
		t.Fatalf("B's reply %+v", c.Items[3].Parts)
	}
}

// Messages sent while a reply streams go in order, each after the reply
// before it, and the same text twice is two messages.
func TestQueuedMessagesInOrder(t *testing.T) {
	m := newMock(t, slowReply(40), textReply("二"), textReply("三"))
	e := openEngine(t)
	addMock(t, e, m)
	q := &postQueue{}
	c, err := e.NewChat(ChatOptions{Model: "mock/m1", Post: q.post})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Send("继续")
	time.Sleep(50 * time.Millisecond)
	q.pump()
	c.Send("继续")
	c.Send("C")
	if c.Queued() != 2 {
		t.Fatalf("%d queued, want 2", c.Queued())
	}
	settleQueue(t, q, c)
	if got := roles(c.Items); got != "U:继续 A U:继续 A U:C A" {
		t.Fatalf("transcript %q", got)
	}
	var asked []string
	for _, r := range m.requests {
		asked = append(asked, lastUser(r))
	}
	if strings.Join(asked, ",") != "继续,继续,C" {
		t.Fatalf("requests asked %q", asked)
	}
}

// Stopping gives back the messages queued behind the reply, and sends
// none of them.
func TestAbortGivesBackQueued(t *testing.T) {
	m := newMock(t, slowReply(400))
	e := openEngine(t)
	addMock(t, e, m)
	q := &postQueue{}
	c, err := e.NewChat(ChatOptions{Model: "mock/m1", Post: q.post})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Send("A")
	time.Sleep(100 * time.Millisecond)
	q.pump()
	c.Send("B")
	c.Send("C")
	if rest := c.Abort(); rest != "B\n\nC" {
		t.Fatalf("Abort gave back %q", rest)
	}
	settleQueue(t, q, c)
	if got := roles(c.Items); got != "U:A A" {
		t.Fatalf("transcript %q", got)
	}
	if !c.Items[1].Aborted {
		t.Fatal("the reply should be marked stopped")
	}
	if len(m.requests) != 1 {
		t.Fatalf("%d requests, want 1", len(m.requests))
	}
}

// A chat keeps the directory it started in when new chats move elsewhere,
// and its tools work there.
func TestChatKeepsItsDirectory(t *testing.T) {
	m := newMock(t, toolReply("call_1", "ls", `{"path":"."}`), textReply("好"))
	e := openEngine(t)
	addMock(t, e, m)
	first := e.CWD()
	c, err := e.NewChat(ChatOptions{Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "elsewhere.txt"), []byte("x"), 0o644)
	if err := e.SetCWD(other); err != nil {
		t.Fatal(err)
	}
	if c.CWD() != first || e.CWD() != other {
		t.Fatalf("chat in %q, engine in %q", c.CWD(), e.CWD())
	}
	c.post(func() { c.Send("列一下") })
	items := settle(t, c)
	out := items[1].Parts[0].Tool.Output
	if !strings.Contains(out, "hello.txt") || strings.Contains(out, "elsewhere.txt") {
		t.Fatalf("ls ran in another directory than the chat shows: %q", out)
	}
	next, err := e.NewChat(ChatOptions{Model: "mock/m1", Tools: FullTools})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.CWD() != other || !next.FullTools() || c.FullTools() {
		t.Fatalf("new chat in %q, full tools %v; first full tools %v", next.CWD(), next.FullTools(), c.FullTools())
	}
}

// Endpoints saved and deleted at once all take effect.
func TestConcurrentEndpointChanges(t *testing.T) {
	e := openEngine(t)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		if err := e.SaveCustomProvider(ctx, CustomProvider{ID: fmt.Sprintf("old-%d", i), BaseURL: "http://127.0.0.1:1/v1", API: "openai-completions", Models: []string{"x"}}, ""); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- e.SaveCustomProvider(ctx, CustomProvider{ID: fmt.Sprintf("new-%d", i), BaseURL: "http://127.0.0.1:1/v1", API: "openai-completions", Models: []string{"x"}}, fmt.Sprintf("key-%d", i))
		}()
	}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- e.DeleteCustomProvider(ctx, fmt.Sprintf("old-%d", i))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := e.CustomProviders()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range list {
		ids = append(ids, p.ID)
	}
	if len(ids) != 16 || strings.Contains(strings.Join(ids, ","), "old-") {
		t.Fatalf("endpoints %v", ids)
	}
	b, _ := os.ReadFile(filepath.Join(e.AgentDir(), "auth.json"))
	for i := 0; i < 16; i++ {
		if !strings.Contains(string(b), fmt.Sprintf(`"key-%d"`, i)) {
			t.Fatalf("key-%d missing from auth.json", i)
		}
	}
	tmp, _ := filepath.Glob(filepath.Join(e.AgentDir(), "*.tmp"))
	if len(tmp) > 0 {
		t.Fatalf("temporary files left: %v", tmp)
	}
}
