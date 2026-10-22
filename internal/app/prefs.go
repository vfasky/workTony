package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"strconv"
	"time"

	"worktony/internal/store"
)

// Keys of the app's settings in the database.
const (
	keyCWD       = "cwd"
	keyFullTools = "full_tools"
	keyLanguage  = "language"
	// keyPrefsImported marks prefs.json, of earlier versions, imported.
	keyPrefsImported = "prefs_json_imported"
)

// Prefs are the GUI's own preferences, which the database keeps; models
// and credentials stay in PiG's files.
type Prefs struct {
	// CWD is the directory new chats start in.
	CWD       string `json:"cwd,omitempty"`
	FullTools bool   `json:"fullTools,omitempty"`
	// Language is the language the app shows, as "en" or "zh-Hans", ""
	// for the system's.
	Language string `json:"-"`
}

func (a *App) loadPrefs() {
	if v, ok, err := a.db.Setting(keyCWD); err == nil && ok {
		a.prefs.CWD = v
	}
	if v, ok, err := a.db.Setting(keyFullTools); err == nil && ok {
		a.prefs.FullTools, _ = strconv.ParseBool(v)
	}
	if v, ok, err := a.db.Setting(keyLanguage); err == nil && ok {
		a.prefs.Language = v
	}
}

func (a *App) savePrefs() {
	err := a.db.SetSettings(map[string]string{
		keyCWD:       a.prefs.CWD,
		keyFullTools: strconv.FormatBool(a.prefs.FullTools),
		keyLanguage:  a.prefs.Language,
	})
	if err != nil {
		log.Printf("save preferences: %v", err)
	}
}

// Language is the language the user chose for the app, "" for the
// system's.
func (a *App) Language() string { return a.prefs.Language }

// setLanguage shows the app in the language of tag, "" for the system's,
// and keeps it.
func (a *App) setLanguage(tag string) {
	a.prefs.Language = tag
	a.savePrefs()
	if a.host.SetLocale != nil {
		a.host.SetLocale(tag)
	}
}

// ImportPrefs moves the preferences that earlier versions kept in the
// prefs.json at path into db, once, and makes their working directory a
// project. Settings db has already win. The file stays, for an earlier
// version to read.
func ImportPrefs(db *store.Store, path string) error {
	if _, done, err := db.Setting(keyPrefsImported); err != nil || done {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var p Prefs
	if err == nil {
		// A broken file has nothing to import, as earlier versions read it.
		_ = json.Unmarshal(b, &p)
	}
	values := map[string]string{}
	if p.CWD != "" {
		if _, ok, _ := db.Setting(keyCWD); !ok {
			values[keyCWD] = p.CWD
		}
		if info, err := os.Stat(p.CWD); err == nil && info.IsDir() {
			if _, err := db.AddProject(p.CWD); err != nil {
				return err
			}
		}
	}
	if p.FullTools {
		if _, ok, _ := db.Setting(keyFullTools); !ok {
			values[keyFullTools] = "true"
		}
	}
	values[keyPrefsImported] = time.Now().Format(time.RFC3339)
	return db.SetSettings(values)
}
