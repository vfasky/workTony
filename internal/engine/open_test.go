package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// A chat of an earlier run opens again from its log: it shows the
// conversation, keeps its directory, tools and model, and goes on from its
// last reply in the same log.
func TestOpenChat(t *testing.T) {
	m := newMock(t, usageReply(7, "答一"), textReply("答二"))
	e := openEngine(t)
	addMock(t, e, m)
	c, err := e.NewChat(ChatOptions{Model: "mock/m1", Tools: FullTools})
	if err != nil {
		t.Fatal(err)
	}
	c.post(func() { c.Send("第一问") })
	settle(t, c)
	path, dir := c.Path(), c.CWD()
	c.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the log %q: %v", path, err)
	}

	o, err := e.OpenChat(path, ChatOptions{CWD: dir, Tools: FullTools})
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if got := roles(o.Items); got != "U:第一问 A" {
		t.Fatalf("transcript %q", got)
	}
	if reply := o.Items[1]; reply.Parts[0].Text != "答一" || reply.Tokens != 7 || reply.Model != "m1" {
		t.Fatalf("reply %+v", reply)
	}
	if o.Title() != "第一问" || o.CWD() != dir || !o.FullTools() || o.Model().Ref != "mock/m1" || o.Path() != path {
		t.Fatalf("title %q, directory %q, full tools %v, model %q, log %q", o.Title(), o.CWD(), o.FullTools(), o.Model().Ref, o.Path())
	}

	o.post(func() { o.Send("第二问") })
	settle(t, o)
	if body := fmt.Sprint(m.requests[1]["messages"]); !strings.Contains(body, "第一问") || !strings.Contains(body, "答一") {
		t.Fatalf("the request goes on from the log: %.600s", body)
	}
	if got := roles(o.Items); got != "U:第一问 A U:第二问 A" {
		t.Fatalf("transcript %q", got)
	}
	// The next run opens it with both turns.
	o.Close()
	again, err := e.OpenChat(path, ChatOptions{CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if got := roles(again.Items); got != "U:第一问 A U:第二问 A" {
		t.Fatalf("reopened transcript %q", got)
	}

	if _, err := e.OpenChat(path+".missing", ChatOptions{CWD: dir}); err == nil {
		t.Fatal("a missing log cannot be opened")
	}
}

// A chat whose model is no longer available opens with the one given.
func TestOpenChatModelGone(t *testing.T) {
	m := newMock(t, textReply("答一"))
	e := openEngine(t)
	save := func(models ...string) {
		t.Helper()
		err := e.SaveCustomProvider(context.Background(), CustomProvider{ID: "mock", Name: "Mock", BaseURL: m.URL + "/v1", API: "openai-completions", Models: models}, "sk-test")
		if err != nil {
			t.Fatal(err)
		}
	}
	save("m1", "m2")
	c, err := e.NewChat(ChatOptions{Model: "mock/m2"})
	if err != nil {
		t.Fatal(err)
	}
	c.post(func() { c.Send("第一问") })
	settle(t, c)
	c.Close()

	save("m1")
	o, err := e.OpenChat(c.Path(), ChatOptions{CWD: c.CWD(), Model: "mock/m1"})
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if o.Model().Ref != "mock/m1" || len(o.Items) != 2 {
		t.Fatalf("model %q, %d items", o.Model().Ref, len(o.Items))
	}
}
