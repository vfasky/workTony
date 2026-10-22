package app

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"worktony/internal/engine"
	"worktony/internal/store"
	"worktony/locales"
)

// loadChats lists the chats of earlier runs, as the database keeps them,
// after the tabs there are. Each opens once it is shown. A chat whose log
// is gone is forgotten.
func (a *App) loadChats() {
	list, err := a.db.Chats()
	if err != nil {
		a.chatToast = locales.ChatsLoadFailed(err.Error())
		return
	}
	for _, ch := range list {
		if _, err := os.Stat(ch.Path); errors.Is(err, fs.ErrNotExist) {
			_ = a.db.RemoveChat(ch.Path)
			continue
		}
		a.nextTab++
		saved := ch
		a.tabs = append(a.tabs, &tab{id: "chat-" + strconv.Itoa(a.nextTab), dir: ch.CWD, saved: &saved, branched: ch.Branched, open: map[string]bool{}})
	}
}

// saveChat keeps the tab's chat, started now, in the sidebar of later
// runs.
func (a *App) saveChat(t *tab) {
	ch := store.Chat{Path: t.chat.Path(), CWD: t.chat.CWD(), Title: t.chat.Title(), Branched: t.branched, FullTools: t.chat.FullTools()}
	if err := a.db.SaveChat(ch); err != nil {
		a.chatToast = locales.ChatSaveFailed(err.Error())
		return
	}
	t.saved = &ch
}

// forgetChat takes the tab's chat off the sidebar of later runs. Its log
// stays.
func (a *App) forgetChat(t *tab) {
	if t.saved == nil {
		return
	}
	if err := a.db.RemoveChat(t.saved.Path); err != nil {
		a.chatToast = locales.ChatForgetFailed(err.Error())
	}
}

// openShown opens the chat shown when it is one of an earlier run, off
// the main thread: it reads the chat's log.
func (a *App) openShown() {
	t := a.current()
	if t.chat != nil || t.saved == nil || t.opening || t.openErr != "" {
		return
	}
	t.opening = true
	saved, post := *t.saved, a.host.PostChat
	opts := engine.ChatOptions{Model: a.modelFor(""), CWD: saved.CWD, Tools: engine.ReadOnlyTools, Post: post}
	if saved.FullTools {
		opts.Tools = engine.FullTools
	}
	go func() {
		chat, err := a.eng.OpenChat(saved.Path, opts)
		post(func() {
			t.opening = false
			if !slices.Contains(a.tabs, t) {
				// Closed while it opened.
				if chat != nil {
					chat.Close()
				}
				return
			}
			if err != nil {
				t.openErr = locales.ChatOpenFailed(err.Error())
				return
			}
			t.chat, t.model = chat, chat.Model().Ref
			t.list.FollowEnd = true
		})
	}()
}

// tabTitle is what the sidebar and the title bar call a tab.
func (a *App) tabTitle(t *tab) string {
	if t.chat != nil {
		if s := t.chat.Title(); s != "" {
			return s
		}
	}
	if t.saved != nil && t.saved.Title != "" {
		return t.saved.Title
	}
	return locales.ChatNewChat()
}

// openingState shows a chat of an earlier run while it opens, or why it
// cannot.
func (a *App) openingState(c *ui.Context, tb *tab) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(12).Padding(24).Children(func() {
		if tb.openErr != "" {
			ui.Text(c, tb.openErr).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(520)
			return
		}
		ui.Spinner(c)
		ui.Text(c, locales.ChatOpening()).TextColor(t.TextMuted)
	})
}
