// WorkTony is a desktop GUI for Pi: PiG, Pi's Go implementation, runs the
// agent in process, and MyGo draws the windows natively on the GPU.
//
//	go tool mygo dev
package main

import (
	"log"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"worktony/internal/app"
	"worktony/internal/engine"
)

const titleBarHeight = 52

func main() {
	mygo.App.SetLanguages(app.Locale)
	eng, err := engine.Open(engine.Options{})
	if err != nil {
		log.Fatal(err)
	}
	prefsPath := ""
	if dir, err := os.UserConfigDir(); err == nil {
		prefsPath = filepath.Join(dir, "WorkTony", "prefs.json")
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
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: chatWin.Load(), Title: "选择工作目录", Directory: true, CreateDirectories: true})
			if err != nil || len(paths) == 0 {
				return ""
			}
			return paths[0]
		},
		OpenURL: func(url string) { _ = mygo.Shell.OpenExternal(url) },
		Reveal:  func(path string) { _ = mygo.Shell.OpenPath(path) },
	}
	a = app.New(eng, host, prefsPath)

	openSettings = func(page string) {
		a.ShowSettingsPage(page)
		if w := settingsWin.Load(); w != nil {
			w.Show()
			w.Focus()
			w.Invalidate()
			return
		}
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:          "设置",
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
			Title:          "WorkTony",
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

	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Label: "文件", Submenu: []*mygo.MenuItem{
				{Label: "新对话", Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) {
					openChat()
					a.NewChat()
					redraw()
				}},
				{Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { openSettings("") }},
				mygo.Separator(),
				{Role: mygo.RoleClose},
			}},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		openChat()
		a.Refresh()
	})
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
}
