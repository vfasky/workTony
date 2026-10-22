// Package store keeps the app's own data in SQLite: its settings, the
// projects of the sidebar and the chats it lists. PiG's files, as
// credentials, models and session logs, stay where PiG keeps them. It uses modernc.org/sqlite, SQLite in
// pure Go, so the app still builds without cgo.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"worktony/locales"
)

// migrations make the schema, in order: PRAGMA user_version counts those
// applied. Add to the end; never change one released.
var migrations = []string{
	// 1: settings and projects.
	`CREATE TABLE settings (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT;
	CREATE TABLE projects (
		id         INTEGER PRIMARY KEY,
		path       TEXT NOT NULL UNIQUE,
		name       TEXT NOT NULL,
		collapsed  INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		opened_at  INTEGER NOT NULL
	) STRICT;`,
	// 2: the chats of the sidebar, by their session logs.
	`CREATE TABLE chats (
		path       TEXT PRIMARY KEY,
		cwd        TEXT NOT NULL,
		title      TEXT NOT NULL,
		branched   INTEGER NOT NULL DEFAULT 0,
		full_tools INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) STRICT;`,
}

// Store is the app's database. Its methods are safe for concurrent use.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens the database at path, making it and its directory when
// missing, and brings its schema up to date. An empty path opens one in
// memory, which nothing keeps.
func Open(path string) (*Store, error) {
	dsn := ":memory:"
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		// A path without "file:" is a plain file name: the driver cuts the
		// parameters at "?", and spaces stay as they are.
		dsn = path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection: the app writes little, from one goroutine mostly, and
	// a database in memory lives as long as its connection.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Path is the database's file, "" for one in memory.
func (s *Store) Path() string { return s.path }

func (s *Store) migrate() error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return errors.New(locales.ErrDatabaseTooNew(version, len(migrations)))
	}
	if version == len(migrations) {
		return nil
	}
	for _, m := range migrations[version:] {
		if _, err := tx.ExecContext(ctx, m); err != nil {
			return err
		}
	}
	// PRAGMA takes no parameters; the number is ours.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations))); err != nil {
		return err
	}
	return tx.Commit()
}

func now() int64 { return time.Now().UnixMilli() }

// Setting returns the value of a setting, and whether it is set.
func (s *Store) Setting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetSetting sets a setting.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, now())
	return err
}

// SetSettings sets several settings together: all or none.
func (s *Store) SetSettings(values map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t := now()
	for k, v := range values {
		_, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, k, v, t)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteSetting unsets a setting.
func (s *Store) DeleteSetting(key string) error {
	_, err := s.db.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

// Project is a folder of the sidebar, under which its chats show.
type Project struct {
	ID   int64
	Path string // absolute and clean
	Name string // the folder's name unless renamed
	// Collapsed hides its chats in the sidebar.
	Collapsed bool
	CreatedAt time.Time
	// OpenedAt is when new chats last went to it.
	OpenedAt time.Time
}

const projectColumns = "id, path, name, collapsed, created_at, opened_at"

type scanner interface{ Scan(dest ...any) error }

func scanProject(r scanner) (Project, error) {
	var p Project
	var created, opened int64
	if err := r.Scan(&p.ID, &p.Path, &p.Name, &p.Collapsed, &created, &opened); err != nil {
		return Project{}, err
	}
	p.CreatedAt, p.OpenedAt = time.UnixMilli(created), time.UnixMilli(opened)
	return p, nil
}

// CleanPath is the form of a project's path: absolute and clean.
func CleanPath(path string) (string, error) {
	if path == "" {
		return "", errors.New(locales.ErrProjectPathEmpty())
	}
	return filepath.Abs(path)
}

// Projects lists the projects, the newest added first.
func (s *Store) Projects() ([]Project, error) {
	rows, err := s.db.Query("SELECT " + projectColumns + " FROM projects ORDER BY created_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddProject adds the folder at path, named after it, and returns it. A
// folder added already is opened: it keeps its name and place.
func (s *Store) AddProject(path string) (Project, error) {
	path, err := CleanPath(path)
	if err != nil {
		return Project{}, err
	}
	name := filepath.Base(path)
	t := now()
	row := s.db.QueryRow(`INSERT INTO projects (path, name, created_at, opened_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET opened_at = excluded.opened_at
		RETURNING `+projectColumns, path, name, t, t)
	return scanProject(row)
}

// ProjectByPath finds the project of a folder.
func (s *Store) ProjectByPath(path string) (Project, bool, error) {
	path, err := CleanPath(path)
	if err != nil {
		return Project{}, false, err
	}
	p, err := scanProject(s.db.QueryRow("SELECT "+projectColumns+" FROM projects WHERE path = ?", path))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, nil
	}
	return p, err == nil, err
}

func (s *Store) updateProject(query string, args ...any) error {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		// The project was removed.
		return errors.New(locales.ErrProjectNotFound())
	}
	return nil
}

// RenameProject names a project; its folder keeps its name.
func (s *Store) RenameProject(id int64, name string) error {
	if name == "" {
		return errors.New(locales.ErrProjectNameEmpty())
	}
	return s.updateProject("UPDATE projects SET name = ? WHERE id = ?", name, id)
}

// SetProjectCollapsed hides or shows a project's chats.
func (s *Store) SetProjectCollapsed(id int64, collapsed bool) error {
	return s.updateProject("UPDATE projects SET collapsed = ? WHERE id = ?", collapsed, id)
}

// OpenProject records that new chats go to a project now, and shows its
// chats.
func (s *Store) OpenProject(id int64) error {
	return s.updateProject("UPDATE projects SET opened_at = ?, collapsed = 0 WHERE id = ?", now(), id)
}

// RemoveProject takes a project off the sidebar. Its folder stays.
func (s *Store) RemoveProject(id int64) error {
	return s.updateProject("DELETE FROM projects WHERE id = ?", id)
}

// Chat is a chat of the sidebar: the session log PiG writes it to, which
// holds its messages, and what the sidebar shows of it before it opens.
type Chat struct {
	// Path is the session's log, a JSONL file of PiG's.
	Path string
	// CWD is the directory its tools work in.
	CWD   string
	Title string
	// Branched is a chat that goes on from a reply of another.
	Branched bool
	// FullTools is a chat whose tools change files and run commands.
	FullTools bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Chats lists the chats, the newest started first.
func (s *Store) Chats() ([]Chat, error) {
	rows, err := s.db.Query("SELECT path, cwd, title, branched, full_tools, created_at, updated_at FROM chats ORDER BY created_at DESC, rowid DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var ch Chat
		var created, updated int64
		if err := rows.Scan(&ch.Path, &ch.CWD, &ch.Title, &ch.Branched, &ch.FullTools, &created, &updated); err != nil {
			return nil, err
		}
		ch.CreatedAt, ch.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
		out = append(out, ch)
	}
	return out, rows.Err()
}

// SaveChat adds a chat, or updates the one of its log, which keeps its
// place.
func (s *Store) SaveChat(ch Chat) error {
	if ch.Path == "" {
		return errors.New(locales.ErrChatPathEmpty())
	}
	t := now()
	_, err := s.db.Exec(`INSERT INTO chats (path, cwd, title, branched, full_tools, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET cwd = excluded.cwd, title = excluded.title, branched = excluded.branched,
			full_tools = excluded.full_tools, updated_at = excluded.updated_at`,
		ch.Path, ch.CWD, ch.Title, ch.Branched, ch.FullTools, t, t)
	return err
}

// RemoveChat takes a chat off the sidebar. Its log stays.
func (s *Store) RemoveChat(path string) error {
	_, err := s.db.Exec("DELETE FROM chats WHERE path = ?", path)
	return err
}
