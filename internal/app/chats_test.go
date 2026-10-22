package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"worktony/internal/engine"
)

// restart closes the app and starts it again over the same engine and
// database, as the next run does.
func (f *fixture) restart(t *testing.T) *ui.Tester {
	t.Helper()
	f.app.Close()
	// The app closed holds nothing for the cleanup to close again.
	f.app.tabs = nil
	f.app = New(f.eng, Host{Post: f.q.post}, f.db)
	f.app.RefreshSync()
	t.Cleanup(f.app.Close)
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	tt.Frame()
	return tt
}

// The chats of the sidebar outlive the app: the next run lists them under
// the new chat, opens one once it is shown, with its messages, and goes
// on with it. Closing a chat takes it off the list; one whose log is gone
// drops off.
func TestChatsAfterRestart(t *testing.T) {
	f := newFixture(t, mockLLM(t, "好的。"))
	tt := chatTester(f)
	first := f.app.current()
	f.app.send(nil, "读一下 README")
	f.until(t, tt, "the reply", func() bool { return !first.chat.Busy && tt.HasText("好的。") })
	f.app.branch(first, first.chat.Items[1])
	f.until(t, tt, "the branch", func() bool { return f.app.current() != first })
	list, err := f.db.Chats()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[1].Path != first.chat.Path() || list[1].Title != "读一下 README" || list[1].CWD != first.chat.CWD() || list[1].Branched || !list[0].Branched {
		t.Fatalf("chats %+v", list)
	}
	path, branchPath := list[1].Path, list[0].Path

	tt = f.restart(t)
	tabs := f.app.tabs
	if len(tabs) != 3 || !tabs[0].empty() || f.app.current() != tabs[0] || tabs[1].saved.Path != branchPath || !tabs[1].branched || tabs[2].saved.Path != path {
		t.Fatalf("tabs %+v", tabs)
	}
	if tabs[1].chat != nil || tabs[2].chat != nil {
		t.Fatal("a chat of an earlier run opens once shown")
	}
	if !tt.HasText("读一下 README") || !tt.HasText("有什么可以帮你？") {
		t.Fatalf("the sidebar lists the chats under the new one: %q", tt.Texts())
	}
	shot(t, tt, "chats-restored")

	restored := tabs[2]
	f.app.selected = restored.id
	tt.Frame()
	if !restored.opening && restored.chat == nil {
		t.Fatal("the chat shown opens")
	}
	f.until(t, tt, "the chat to open", func() bool { return restored.chat != nil && tt.HasText("好的。") })
	if restored.chat.Path() != path || len(restored.chat.Items) != 2 || restored.model != "mock/m1" || tabs[1].chat != nil {
		t.Fatalf("restored %q, %d items, model %q", restored.chat.Path(), len(restored.chat.Items), restored.model)
	}
	f.app.send(nil, "继续")
	f.until(t, tt, "the next reply", func() bool { return !restored.chat.Busy && len(restored.chat.Items) == 4 })
	if list, _ := f.db.Chats(); len(list) != 2 {
		t.Fatalf("going on adds no chat: %+v", list)
	}

	f.app.closeTab(restored)
	if list, _ := f.db.Chats(); len(list) != 1 || list[0].Path != branchPath {
		t.Fatalf("closed, the chat is off the list: %+v", list)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("its log stays: %v", err)
	}

	os.Remove(branchPath)
	f.restart(t)
	if len(f.app.tabs) != 1 {
		t.Fatalf("tabs %+v", f.app.tabs)
	}
	if list, _ := f.db.Chats(); len(list) != 0 {
		t.Fatalf("a chat whose log is gone drops off: %+v", list)
	}
}

// A chat of an earlier run whose folder is gone says it cannot open; its
// composer sends nothing and starts no chat.
func TestChatOpenFails(t *testing.T) {
	f := newFixture(t, mockLLM(t, "好的。"))
	dir := mkdir(t, filepath.Join(t.TempDir(), "gone"))
	chat, err := f.eng.NewChat(engine.ChatOptions{Model: "mock/m1", CWD: dir, Post: f.q.post})
	if err != nil {
		t.Fatal(err)
	}
	chat.Send("你好")
	tt := chatTester(f)
	f.until(t, tt, "the reply", func() bool { return !chat.Busy })
	tb := &tab{chat: chat}
	f.app.saveChat(tb)
	chat.Close()
	os.RemoveAll(dir)
	// The log stays where PiG keeps it.
	if _, err := os.Stat(chat.Path()); err != nil {
		t.Fatal(err)
	}

	tt = f.restart(t)
	saved := f.app.tabs[1]
	f.app.selected = saved.id
	f.until(t, tt, "the error", func() bool { return saved.openErr != "" && tt.HasText(saved.openErr) })
	if saved.chat != nil || saved.opening {
		t.Fatalf("tab %+v", saved)
	}
	if f.app.sendTo(nil, saved, "还在吗") {
		t.Fatal("a chat that cannot open sends nothing")
	}
	if saved.chat != nil {
		t.Fatal("no chat started")
	}
}
