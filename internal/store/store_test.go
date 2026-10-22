package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// The database lives in a folder made for it, whose name may have spaces,
// and keeps what it holds across opens.
func TestSettingsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "WorkTony Debug", "worktony.db")
	s := open(t, path)
	if _, ok, err := s.Setting("cwd"); ok || err != nil {
		t.Fatalf("a new database has cwd: %v %v", ok, err)
	}
	if err := s.SetSetting("cwd", "/a"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSettings(map[string]string{"cwd": "/b", "full_tools": "true"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	s = open(t, path)
	if v, ok, err := s.Setting("cwd"); v != "/b" || !ok || err != nil {
		t.Fatalf("cwd %q %v %v", v, ok, err)
	}
	if v, _, _ := s.Setting("full_tools"); v != "true" {
		t.Fatalf("full_tools %q", v)
	}
	if err := s.DeleteSetting("cwd"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Setting("cwd"); ok {
		t.Fatal("cwd should be unset")
	}
}

func TestProjects(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "db"))
	root := t.TempDir()
	a, err := s.AddProject(filepath.Join(root, "alpha", "..", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Path != filepath.Join(root, "alpha") || a.Name != "alpha" || a.Collapsed {
		t.Fatalf("project %+v", a)
	}
	b, err := s.AddProject(filepath.Join(root, "beta"))
	if err != nil {
		t.Fatal(err)
	}
	// Adding a folder again keeps the project, its name and its place.
	if err := s.RenameProject(a.ID, "Alpha App"); err != nil {
		t.Fatal(err)
	}
	again, err := s.AddProject(filepath.Join(root, "alpha") + string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != a.ID || again.Name != "Alpha App" || again.CreatedAt != a.CreatedAt {
		t.Fatalf("added again %+v, was %+v", again, a)
	}

	list, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("projects %+v: want the newest added first", list)
	}

	if err := s.SetProjectCollapsed(b.ID, true); err != nil {
		t.Fatal(err)
	}
	p, ok, err := s.ProjectByPath(filepath.Join(root, "beta"))
	if !ok || err != nil || !p.Collapsed {
		t.Fatalf("beta %+v %v %v", p, ok, err)
	}
	if err := s.OpenProject(b.ID); err != nil {
		t.Fatal(err)
	}
	if p, _, _ := s.ProjectByPath(p.Path); p.Collapsed || p.OpenedAt.Before(b.OpenedAt) {
		t.Fatalf("an opened project shows its chats: %+v", p)
	}

	if err := s.RemoveProject(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ProjectByPath(a.Path); ok {
		t.Fatal("alpha was removed")
	}
	if err := s.RenameProject(a.ID, "x"); err == nil {
		t.Fatal("renaming a project removed should fail")
	}
	if err := s.RenameProject(b.ID, ""); err == nil {
		t.Fatal("a project needs a name")
	}
}

// A database in memory works, and keeps nothing.
func TestMemory(t *testing.T) {
	s := open(t, "")
	if s.Path() != "" {
		t.Fatalf("path %q", s.Path())
	}
	if _, err := s.AddProject(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Projects(); len(list) != 1 {
		t.Fatalf("projects %+v", list)
	}
}

// A database of a newer app, with migrations this one lacks, is left as it
// is.
func TestNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s := open(t, path)
	s.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "99") {
		t.Fatalf("opening a newer database: %v", err)
	}
}

// The chats of the sidebar are kept by their logs, the newest started
// first; saving one again updates it in its place.
func TestChats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s := open(t, path)
	if list, err := s.Chats(); len(list) != 0 || err != nil {
		t.Fatalf("a new database has chats: %+v %v", list, err)
	}
	if err := s.SaveChat(Chat{CWD: "/w", Title: "x"}); err == nil {
		t.Fatal("a chat needs its log")
	}
	if err := s.SaveChat(Chat{Path: "/logs/a.jsonl", CWD: "/w", Title: "第一个"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveChat(Chat{Path: "/logs/b.jsonl", CWD: "/v", Title: "分支", Branched: true, FullTools: true}); err != nil {
		t.Fatal(err)
	}
	list, err := s.Chats()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Path != "/logs/b.jsonl" || list[1].Path != "/logs/a.jsonl" {
		t.Fatalf("chats %+v: want b, then a", list)
	}
	if b := list[0]; b.CWD != "/v" || b.Title != "分支" || !b.Branched || !b.FullTools || b.CreatedAt.IsZero() {
		t.Fatalf("b %+v", b)
	}
	a := list[1]
	if err := s.SaveChat(Chat{Path: a.Path, CWD: a.CWD, Title: "改名"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s = open(t, path)
	list, _ = s.Chats()
	if len(list) != 2 || list[1].Path != a.Path || list[1].Title != "改名" || !list[1].CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("chats %+v: a keeps its place", list)
	}
	if err := s.RemoveChat(a.Path); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Chats(); len(list) != 1 || list[0].Path != "/logs/b.jsonl" {
		t.Fatalf("chats %+v", list)
	}
}

// A database of the first version, before chats were kept, gains them and
// keeps what it held.
func TestMigrateChats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s := open(t, path)
	if _, err := s.AddProject(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE chats; PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s = open(t, path)
	if list, _ := s.Projects(); len(list) != 1 {
		t.Fatalf("projects %+v", list)
	}
	if err := s.SaveChat(Chat{Path: "/logs/a.jsonl", CWD: "/w", Title: "a"}); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Chats(); len(list) != 1 {
		t.Fatalf("chats %+v", list)
	}
}
