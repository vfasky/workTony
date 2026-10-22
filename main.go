// WorkTony is a desktop GUI for Pi: PiG, Pi's Go implementation, runs the
// agent in process, and MyGo draws the windows natively on the GPU.
//
//	go tool mygo dev
package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"worktony/internal/app"
	"worktony/internal/engine"
	"worktony/internal/store"
	"worktony/locales"
)

const titleBarHeight = 52

func main() {
	// The app's languages are its catalogs': English and Simplified
	// Chinese (package locales).
	//
	// A development build, as mygo dev makes, is "WorkTony Debug", and runs
	// once: mygo dev quits the running build before it starts the next, and
	// a second launch, as of a second mygo dev, hands over to the first and
	// quits, which ends that mygo dev too.
	debug := mygo.IsDev()
	name := "WorkTony"
	if debug {
		name += " Debug"
		mygo.App.SetName(name)
		if !mygo.App.RequestSingleInstanceLock() {
			log.Println(name + " is running already: showing it")
			return
		}
	}
	eng, err := engine.Open(engine.Options{})
	if err != nil {
		log.Fatal(err)
	}
	// The app's settings and projects are in SQLite, in the system's
	// configuration directory: the debug build keeps its own apart from the
	// installed app's. Without the directory, or the database, they live in
	// memory, and the chat window says so.
	var db *store.Store
	dbErr := errors.New(locales.AppNoConfigDir())
	if dir, err := os.UserConfigDir(); err == nil {
		db, dbErr = store.Open(filepath.Join(dir, name, "worktony.db"))
		if dbErr == nil {
			if err := app.ImportPrefs(db, filepath.Join(dir, name, "prefs.json")); err != nil {
				log.Printf("import prefs.json: %v", err)
			}
		}
	}
	if dbErr != nil {
		log.Printf("open database: %v", dbErr)
		if db, err = store.Open(""); err != nil {
			log.Fatal(err)
		}
	}

	// The windows, which goroutines posting to the main thread read too.
	var chatWin, settingsWin atomic.Pointer[mygo.Window]
	var a *app.App
	redraw := func() {
		for _, w := range []*mygo.Window{chatWin.Load(), settingsWin.Load()} {
			if w != nil {
				w.Invalidate()
			}
		}
	}
	// post queues fn on the main thread without waiting for it, in order, so
	// a streaming reply never waits for a frame. Window.Update queues it;
	// with no window, RunOnMain does, and waits.
	post := func(fn func()) {
		w := chatWin.Load()
		if w == nil {
			w = settingsWin.Load()
		}
		if w == nil {
			mygo.RunOnMain(fn)
			return
		}
		w.Update(fn)
	}
	var openSettings func(page string)
	host := app.Host{
		Post: func(fn func()) {
			post(func() {
				fn()
				redraw()
			})
		},
		// Update redraws the chat window.
		PostChat:     post,
		OpenSettings: func(page string) { openSettings(page) },
		ChooseFolder: func() string {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: chatWin.Load(), Title: locales.AppChooseFolder(), Directory: true, CreateDirectories: true})
			if err != nil || len(paths) == 0 {
				return ""
			}
			return paths[0]
		},
		Debug:   debug,
		OpenURL: func(url string) { _ = mygo.Shell.OpenExternal(url) },
		Reveal:  func(path string) { _ = mygo.Shell.OpenPath(path) },
		// MyGo keeps the language too; the database's choice wins.
		SetLocale: mygo.App.SetLocale,
	}
	a = app.New(eng, host, db)
	mygo.App.SetLocale(a.Language())
	if dbErr != nil {
		a.Notice(locales.AppDataFileUnavailable(dbErr.Error()))
	}

	openSettings = func(page string) {
		a.ShowSettingsPage(page)
		if w := settingsWin.Load(); w != nil {
			w.Show()
			w.Focus()
			w.Invalidate()
			return
		}
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:          locales.AppSettingsWindowTitle(name),
			Width:          860,
			Height:         620,
			MinWidth:       680,
			MinHeight:      460,
			StateKey:       "settings",
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: titleBarHeight,
			Vibrancy:       mygo.VibrancySidebar,
			Content:        ui.View(a.SettingsView),
		})
		settingsWin.Store(w)
		w.OnClosed(func() { settingsWin.Store(nil) })
	}
	openChat := func() {
		if w := chatWin.Load(); w != nil {
			w.Show()
			w.Focus()
			return
		}
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:          name,
			Width:          1100,
			Height:         740,
			MinWidth:       720,
			MinHeight:      480,
			StateKey:       "chat",
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: titleBarHeight,
			Vibrancy:       mygo.VibrancySidebar,
			Content:        ui.View(a.ChatView),
		})
		chatWin.Store(w)
		w.OnClosed(func() { chatWin.Store(nil) })
	}

	// menu is the menu bar in the language the app shows; the items of a
	// role say theirs themselves.
	menu := func() *mygo.Menu {
		return mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Label: locales.MenuFile(), Submenu: []*mygo.MenuItem{
				{Label: locales.MenuNewChat(), Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) {
					openChat()
					a.NewChat()
					redraw()
				}},
				{Label: locales.MenuSettings(), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { openSettings("") }},
				mygo.Separator(),
				{Role: mygo.RoleClose},
			}},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindowMenu},
		})
	}
	// The windows of native UI draw anew in another language; what was set
	// once, the menu and the settings window's title, is set again.
	mygo.App.OnLocaleChanged(func(string) {
		mygo.App.SetMenu(menu())
		if w := settingsWin.Load(); w != nil {
			w.SetTitle(locales.AppSettingsWindowTitle(name))
		}
	})

	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(menu())
		openChat()
		a.Refresh()
	})
	// Another launch of the debug build shows this one's chat.
	mygo.App.OnSecondInstance(func([]string, string) { mygo.RunOnMain(openChat) })
	// Clicking the Dock icon with no window open shows the chat again.
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			openChat()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	a.Close()
	eng.Close()
	db.Close()
}
