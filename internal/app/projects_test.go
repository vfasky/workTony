package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"worktony/internal/store"
)

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// chooseDir does what choosing a folder in the composer does.
func (f *fixture) chooseDir(t *testing.T, dir string) {
	t.Helper()
	err := f.eng.SetCWD(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.app.cwdChanged(dir, err, true)
}

func (f *fixture) project(t *testing.T, dir string) store.Project {
	t.Helper()
	p, ok, err := f.db.ProjectByPath(dir)
	if err != nil || !ok {
		t.Fatalf("no project %s: %v", dir, err)
	}
	return p
}

// Folders chosen for new chats become projects of the sidebar, which hold
// their chats, and the database keeps them.
func TestSidebarProjects(t *testing.T) {
	f := newFixture(t, mockLLM(t, "好"))
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
	tt.Frame()
	if !tt.HasText("添加项目文件夹…") || !tt.HasText("添加项目") {
		t.Fatalf("without projects the sidebar offers one: %q", tt.Texts())
	}
	shot(t, tt, "sidebar-no-projects")

	root := t.TempDir()
	alpha := mkdir(t, filepath.Join(root, "alpha"))
	beta := mkdir(t, filepath.Join(root, "beta"))
	f.chooseDir(t, alpha)
	tt.Frame()
	first := f.app.current()
	if pr := f.app.projectOf(f.app.tabDir(first)); pr == nil || pr.Path != alpha {
		t.Fatalf("the empty chat should be in alpha: %+v", pr)
	}
	f.app.send(nil, "第一个问题")
	f.until(t, tt, "the reply", func() bool { return !first.chat.Busy })

	f.chooseDir(t, beta)
	tt.Frame()
	second := f.app.current()
	if second == first || second.chat != nil {
		t.Fatal("choosing beta from a started chat opens a new chat")
	}
	shot(t, tt, "sidebar-projects")

	list, _ := f.db.Projects()
	if len(list) != 2 || list[0].Path != beta || list[1].Path != alpha {
		t.Fatalf("projects %+v: want beta, then alpha", list)
	}
	if v, _, _ := f.db.Setting(keyCWD); v != beta {
		t.Fatalf("saved cwd %q", v)
	}
	// alpha's chat shows under it; beta, the current project, has the
	// empty chat.
	if f.app.projectOf(first.chat.CWD()).Path != alpha || f.app.projectOf(f.app.tabDir(second)).Path != beta {
		t.Fatal("chats are in the wrong projects")
	}

	// A click on a project hides its chats, and the database remembers.
	if err := tt.Click("alpha"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("第一个问题") {
		t.Fatalf("collapsed alpha still shows its chat: %q", tt.Texts())
	}
	if !f.project(t, alpha).Collapsed {
		t.Fatal("alpha should be saved collapsed")
	}
	if err := tt.Click("alpha"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("第一个问题") || f.project(t, alpha).Collapsed {
		t.Fatalf("alpha should show its chat again: %q", tt.Texts())
	}

	// Renaming names the project, not the folder.
	if err := tt.RightClick("beta"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Equal(menu, []string{"新对话", "重命名…", "-", "从侧栏移除"}) {
		t.Fatalf("project menu %q", menu)
	}
	if err := tt.ChooseMenuItem("重命名…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.app.renaming == nil || f.app.renaming.name != "beta" {
		t.Fatalf("rename dialog %+v", f.app.renaming)
	}
	f.app.renaming.name = "Beta 应用"
	tt.Frame()
	shot(t, tt, "sidebar-rename")
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if p := f.project(t, beta); p.Name != "Beta 应用" || !tt.HasText("Beta 应用") {
		t.Fatalf("renamed %+v; texts %q", p, tt.Texts())
	}

	// Removing alpha keeps its chat, under the others, and its folder.
	if err := tt.RightClick("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("从侧栏移除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok, _ := f.db.ProjectByPath(alpha); ok {
		t.Fatal("alpha should be removed")
	}
	if !tt.HasText("其他对话") || !tt.HasText("第一个问题") {
		t.Fatalf("alpha's chat should show under the others: %q", tt.Texts())
	}
	if _, err := os.Stat(alpha); err != nil {
		t.Fatal("the folder should stay")
	}
}

// The button on a project starts a chat there, which new chats then follow.
func TestNewChatInProject(t *testing.T) {
	f := newFixture(t, mockLLM(t, "好"))
	root := t.TempDir()
	alpha := mkdir(t, filepath.Join(root, "alpha"))
	beta := mkdir(t, filepath.Join(root, "beta"))
	f.chooseDir(t, alpha)
	f.chooseDir(t, beta)
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.Frame()
	f.app.send(nil, "在 beta 里")
	started := f.app.current()
	f.until(t, tt, "the reply", func() bool { return !started.chat.Busy })

	if !tt.HasText("开始新对话") {
		t.Fatalf("alpha, without chats, offers one: %q", tt.Texts())
	}
	shot(t, tt, "sidebar-empty-project")

	// The button shows as the pointer rests on the project.
	r, ok := tt.Find("alpha")
	if !ok {
		t.Fatalf("no alpha: %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("在 alpha 中新建对话"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	f.until(t, tt, "the chat in alpha", func() bool { return samePath(f.eng.CWD(), alpha) && f.app.current() != started })
	tb := f.app.current()
	if tb.chat != nil || f.app.projectOf(f.app.tabDir(tb)).Path != alpha {
		t.Fatal("the new chat should be in alpha")
	}
	if v, _, _ := f.db.Setting(keyCWD); v != alpha {
		t.Fatalf("saved cwd %q", v)
	}
	if !tt.HasText("工作目录 alpha") {
		t.Fatalf("the composer should show alpha: %q", tt.Texts())
	}
	shot(t, tt, "sidebar-new-in-project")

	// ⌘N opens the next chat in alpha too, shown even when collapsed.
	f.app.setCollapsed(f.project(t, alpha), true)
	f.app.selected = started.id
	f.app.NewChat()
	if f.app.current() != tb || f.project(t, alpha).Collapsed {
		t.Fatal("a new chat goes to alpha, which shows it")
	}

	// A project whose folder went away says so.
	gone := mkdir(t, filepath.Join(root, "gone"))
	f.chooseDir(t, gone)
	f.chooseDir(t, beta)
	os.Remove(gone)
	f.app.newChatIn(f.project(t, gone))
	deadline := time.Now().Add(5 * time.Second)
	for !strings.HasPrefix(f.app.chatToast, "无法打开项目 gone") && time.Now().Before(deadline) {
		f.q.pump()
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.HasPrefix(f.app.chatToast, "无法打开项目 gone") {
		t.Fatalf("toast %q", f.app.chatToast)
	}
	if !samePath(f.eng.CWD(), beta) {
		t.Fatalf("cwd moved to %q", f.eng.CWD())
	}
}

// Preferences live in the database: another start reads them.
func TestPrefsPersist(t *testing.T) {
	f := newFixture(t, nil)
	dir := mkdir(t, filepath.Join(t.TempDir(), "work"))
	f.chooseDir(t, dir)
	tt := ui.NewTester(f.app.ChatView, 1100, 740)
	tt.SetLocale(testLocale)
	tt.Frame()
	if err := tt.Click("只读"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if v, _, _ := f.db.Setting(keyFullTools); v != "true" {
		t.Fatalf("full_tools %q", v)
	}

	again := New(f.eng, Host{}, f.db)
	t.Cleanup(again.Close)
	if !again.prefs.FullTools || again.prefs.CWD != dir || len(again.projects) != 1 {
		t.Fatalf("prefs %+v, projects %+v", again.prefs, again.projects)
	}
}

// prefs.json of earlier versions moves into the database once.
func TestImportPrefs(t *testing.T) {
	dir := t.TempDir()
	work := mkdir(t, filepath.Join(dir, "work"))
	legacy := filepath.Join(dir, "prefs.json")
	os.WriteFile(legacy, []byte(`{"cwd":"`+filepath.ToSlash(work)+`","fullTools":true}`), 0o644)
	db, err := store.Open(filepath.Join(dir, "worktony.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ImportPrefs(db, legacy); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := db.Setting(keyCWD); filepath.Clean(v) != work {
		t.Fatalf("cwd %q", v)
	}
	if v, _, _ := db.Setting(keyFullTools); v != "true" {
		t.Fatalf("full_tools %q", v)
	}
	if list, _ := db.Projects(); len(list) != 1 || list[0].Path != work {
		t.Fatalf("projects %+v", list)
	}

	// Once: the file, changed after, imports no more.
	os.WriteFile(legacy, []byte(`{"cwd":"/elsewhere"}`), 0o644)
	if err := ImportPrefs(db, legacy); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := db.Setting(keyCWD); filepath.Clean(v) != work {
		t.Fatalf("imported twice: cwd %q", v)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("prefs.json should stay")
	}

	// Without prefs.json there is nothing to import.
	empty, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	if err := ImportPrefs(empty, filepath.Join(dir, "missing.json")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := empty.Setting(keyCWD); ok {
		t.Fatal("nothing to import")
	}
}
