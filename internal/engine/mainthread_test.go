package engine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
)

// mainThread runs posted functions on one goroutine, and its caller waits
// for each, as mygo.RunOnMain does; frame runs between them, as the views
// do.
type mainThread struct {
	fns chan func()
}

func startMain(ctx context.Context, frame func()) *mainThread {
	m := &mainThread{fns: make(chan func())}
	go func() {
		tick := time.NewTicker(16 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case fn := <-m.fns:
				fn()
			case <-tick.C:
				frame()
			case <-ctx.Done():
				return
			}
		}
	}()
	return m
}

// do runs fn on the main thread and waits for it.
func (m *mainThread) do(fn func()) {
	done := make(chan struct{})
	m.fns <- func() { fn(); close(done) }
	<-done
}

func slowReply(n int) func(w io.Writer) {
	return func(w io.Writer) {
		for i := 0; i < n; i++ {
			chunk(w, `{"content":"字"}`, "")
			time.Sleep(2 * time.Millisecond)
		}
		chunk(w, "{}", "stop")
		io.WriteString(w, "data: [DONE]\n\n")
	}
}

// A long reply streams to the end while the views read the chat every frame
// on the main thread and the user changes its thinking level, with the
// session's events waiting for the main thread. It once stopped after as
// many events as the session's unread Events channel holds.
func TestLongReplyOnMainThread(t *testing.T) {
	m := newMock(t, slowReply(400))
	e := openEngine(t)
	b := `{"providers":{"mock":{"name":"Mock","baseUrl":"` + m.URL + `/v1","api":"openai-completions","apiKey":"k","models":[{"id":"r1","reasoning":true}]}}}`
	os.WriteFile(filepath.Join(e.AgentDir(), "models.json"), []byte(b), 0o600)
	e.reloadModels()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var c *Chat
	main := startMain(ctx, func() {
		if c != nil {
			_ = c.ThinkingLevels()
			_ = c.Thinking()
			_ = c.Tools()
		}
	})
	var err error
	main.do(func() {
		c, err = e.NewChat(ChatOptions{Model: "mock/r1", Post: main.do})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if levels := c.ThinkingLevels(); len(levels) < 2 {
		t.Fatalf("a reasoning model should take levels, got %v", levels)
	}
	main.do(func() { c.Send("hi") })
	time.Sleep(200 * time.Millisecond)
	main.do(func() {
		if err := c.SetThinking(ai.ThinkingHigh); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var busy bool
		main.do(func() { busy = c.Busy })
		if !busy {
			var text string
			main.do(func() { text = c.Items[len(c.Items)-1].Parts[0].Text })
			if n := len([]rune(text)); n != 400 {
				t.Fatalf("reply has %d characters, want 400", n)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the reply never settled")
}
