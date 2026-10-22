package app

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"worktony/internal/store"
	"worktony/locales"
)

// chatIndent is the left padding of a project's chats, which lines their
// icons up with the project's folder: the row's padding, its arrow and the
// gap after it.
const chatIndent = 8 + 12 + 6

// renameForm is the dialog renaming a project.
type renameForm struct {
	open bool
	id   int64
	path string
	name string
}

func (a *App) loadProjects() {
	list, err := a.db.Projects()
	if err != nil {
		a.chatToast = locales.ProjectsLoadFailed(err.Error())
		return
	}
	a.projects = list
}

// projectOf is the project of the folder dir, nil for none.
func (a *App) projectOf(dir string) *store.Project {
	for i := range a.projects {
		if samePath(a.projects[i].Path, dir) {
			return &a.projects[i]
		}
	}
	return nil
}

// tabDir is the directory a tab's chat works in: its chat's, or, before it
// starts or opens, the one it will work in.
func (a *App) tabDir(t *tab) string {
	if t.chat != nil {
		return t.chat.CWD()
	}
	return t.dir
}

func samePath(x, y string) bool {
	x, y = filepath.Clean(x), filepath.Clean(y)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(x, y)
	}
	return x == y
}

// openProject makes the folder dir, added as a project when new, the
// project new chats go to, and shows its chats.
func (a *App) openProject(dir string) {
	p, err := a.db.AddProject(dir)
	if err == nil {
		err = a.db.OpenProject(p.ID)
	}
	if err != nil {
		a.chatToast = locales.ProjectSaveFailed(err.Error())
		return
	}
	a.loadProjects()
}

// newChatIn shows a new chat in a project, where new chats go from then
// on. Moving there rebuilds PiG's services, off the main thread.
func (a *App) newChatIn(p store.Project) {
	if a.cwdPending == 0 && samePath(p.Path, a.eng.CWD()) {
		a.openProject(p.Path)
		a.newTab()
		return
	}
	a.switchCWD(p.Path, func(err error, latest bool) {
		if err != nil {
			a.chatToast = locales.ProjectOpenFailed(p.Name, err.Error())
			return
		}
		a.cwdMoved(p.Path)
		if latest {
			a.newTabIn(p.Path)
		}
	})
}

// switchCWD moves new chats to dir off the main thread, then runs done on
// it. Moves run one at a time in the order asked, so the engine ends in
// the directory asked last, and their done functions run in that order
// too. latest is false for a move asked for before another: what the
// later one shows wins.
func (a *App) switchCWD(dir string, done func(err error, latest bool)) {
	a.cwdSeq++
	a.cwdPending++
	seq, prev, next := a.cwdSeq, a.cwdDone, make(chan struct{})
	a.cwdDone = next
	go func() {
		if prev != nil {
			<-prev
		}
		err := a.eng.SetCWD(dir)
		a.host.Post(func() {
			a.cwdPending--
			done(err, seq == a.cwdSeq)
		})
		// The next move starts once this one has posted its done, which
		// then runs first.
		close(next)
	}()
}

// cwdMoved records that new chats go to dir: it is saved, it is the open
// project, and the empty chat, which shows where new chats go, starts
// there.
func (a *App) cwdMoved(dir string) {
	a.prefs.CWD = dir
	a.savePrefs()
	a.openProject(dir)
	for _, t := range a.tabs {
		if t.empty() {
			t.dir = dir
		}
	}
}

func (a *App) setCollapsed(p store.Project, collapsed bool) {
	if err := a.db.SetProjectCollapsed(p.ID, collapsed); err != nil {
		a.chatToast = locales.ProjectSaveFailed(err.Error())
		return
	}
	if pr := a.projectOf(p.Path); pr != nil {
		pr.Collapsed = collapsed
	}
}

// removeProject takes a project off the sidebar; its chats show under the
// others, and its folder stays.
func (a *App) removeProject(p store.Project) {
	if err := a.db.RemoveProject(p.ID); err != nil {
		a.chatToast = locales.ProjectRemoveFailed(err.Error())
		return
	}
	a.loadProjects()
	a.chatToast = locales.ProjectRemoved(p.Name)
}

// renameProject saves the dialog's name; an empty one is the folder's.
func (a *App) renameProject(f *renameForm) {
	name := strings.TrimSpace(f.name)
	if name == "" {
		name = filepath.Base(f.path)
	}
	if err := a.db.RenameProject(f.id, name); err != nil {
		a.chatToast = locales.ProjectRenameFailed(err.Error())
		return
	}
	a.loadProjects()
}

// sidebarList builds the sidebar's rows: the projects, each over its
// chats, then the chats in no project. What the rows do waits in later,
// to run once they are built.
func (a *App) sidebarList(c *ui.Context, p palette, later *[]func()) {
	groups := map[int64][]*tab{}
	var loose []*tab
	for _, tb := range a.tabs {
		if pr := a.projectOf(a.tabDir(tb)); pr != nil {
			groups[pr.ID] = append(groups[pr.ID], tb)
		} else {
			loose = append(loose, tb)
		}
	}
	a.projectsHeader(c, p, later)
	if len(a.projects) == 0 {
		a.noProjects(c, p, later)
	}
	cwd := a.eng.CWD()
	for _, pr := range a.projects {
		a.projectRow(c, p, pr, samePath(pr.Path, cwd), later)
		if pr.Collapsed {
			continue
		}
		chats := groups[pr.ID]
		if len(chats) == 0 {
			a.emptyProject(c, p, pr, later)
		}
		for _, tb := range chats {
			a.chatItem(c, tb, chatIndent, later)
		}
	}
	if len(loose) > 0 {
		title := locales.ProjectsOtherChats()
		if len(a.projects) == 0 {
			title = locales.ChatList()
		}
		ui.SidebarSection(c, title, nil, func() {
			for _, tb := range loose {
				a.chatItem(c, tb, 0, later)
			}
		})
	}
}

// projectsHeader titles the projects as a SidebarSection does, with a
// button adding one.
func (a *App) projectsHeader(c *ui.Context, p palette, later *[]func()) {
	t := c.Theme()
	ui.Row(c).Height(28).Shrink(0).Padding(0, 3, 0, 8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.ProjectsTitle()).FontSize(t.FontSize - 1).FontWeight(600).TextColor(t.TextMuted).Grow(1).SingleLine()
		if smallButton(c, "add-project", icFolderAd, locales.ProjectAdd(), p).Clicked() {
			*later = append(*later, func() { a.chooseFolder(true) })
		}
	})
}

// noProjects offers a project, before there is any.
func (a *App) noProjects(c *ui.Context, p palette, later *[]func()) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("first-project")).Shrink(0).Padding(6, 8).Radius(8).Gap(8).AlignItems(ui.Center).Justify(ui.Start).TextColor(t.TextMuted).
		Label(locales.ProjectAddFolder()).Tooltip(locales.ProjectAddFolderTooltip())
	if b.Hovered() {
		b.Background(p.subtle).TextColor(t.Text)
	}
	b.Children(func() {
		ui.Icon(c, icFolderAd).Size(16, 16)
		ui.Text(c, locales.ProjectAddFolderEllipsis())
	})
	if b.Clicked() {
		*later = append(*later, func() { a.chooseFolder(true) })
	}
}

// projectRow is a project's row: a click shows or hides its chats, and a
// button, as the pointer rests on it, starts one there.
func (a *App) projectRow(c *ui.Context, p palette, pr store.Project, current bool, later *[]func()) {
	t := c.Theme()
	id := strconv.FormatInt(pr.ID, 10)
	tip := shortPath(pr.Path)
	if current {
		tip = locales.ProjectCurrentTooltip(tip)
	}
	row := ui.ButtonBase(c.Key("project-"+id)).Height(30).Shrink(0).Padding(0, 3, 0, 8).Radius(8).Gap(6).AlignItems(ui.Center).
		Label(pr.Name).Tooltip(tip)
	hovered := row.Hovered()
	if hovered {
		row.Background(p.subtle)
	}
	row.Children(func() {
		arrow := icChevDown
		if pr.Collapsed {
			arrow = icChevR
		}
		ui.Icon(c, arrow).Size(12, 12).Shrink(0).TextColor(t.TextMuted)
		folder, color := icFolder, t.TextMuted
		if current {
			folder, color = icFolderOp, t.Accent
		}
		ui.Icon(c, folder).Size(16, 16).Shrink(0).TextColor(color)
		name := ui.Text(c, pr.Name).SingleLine().Grow(1).MinWidth(0)
		if current {
			name.FontWeight(600)
		}
		if hovered && smallButton(c, "project-new-"+id, icPencil, locales.ProjectNewChatIn(pr.Name), p).Clicked() {
			*later = append(*later, func() { a.newChatIn(pr) })
		}
	})
	if row.Clicked() {
		*later = append(*later, func() { a.setCollapsed(pr, !pr.Collapsed) })
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.ChatNewChat()).Chosen() {
			*later = append(*later, func() { a.newChatIn(pr) })
		}
		if a.host.Reveal != nil && m.Item(locales.ProjectOpenFolder()).Chosen() {
			a.host.Reveal(pr.Path)
		}
		if m.Item(locales.ProjectRenameEllipsis()).Chosen() {
			a.renaming = &renameForm{open: true, id: pr.ID, path: pr.Path, name: pr.Name}
		}
		m.Separator()
		if m.Item(locales.ProjectRemoveFromSidebar()).Chosen() {
			*later = append(*later, func() { a.removeProject(pr) })
		}
	})
}

// emptyProject offers a chat in a project without any.
func (a *App) emptyProject(c *ui.Context, p palette, pr store.Project, later *[]func()) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("project-start-"+strconv.FormatInt(pr.ID, 10))).Shrink(0).Padding(5, 8, 5, chatIndent).Radius(8).Gap(8).Justify(ui.Start).
		AlignItems(ui.Center).TextColor(t.TextMuted).Label(locales.ProjectStartChatIn(pr.Name))
	if b.Hovered() {
		b.Background(p.subtle).TextColor(t.Text)
	}
	b.Children(func() {
		ui.Icon(c, icPlus).Size(14, 14)
		ui.Text(c, locales.ProjectStartChat()).FontSize(12)
	})
	if b.Clicked() {
		*later = append(*later, func() { a.newChatIn(pr) })
	}
}

// chatItem is a chat's item, indented under its project.
func (a *App) chatItem(c *ui.Context, tb *tab, indent float32, later *[]func()) {
	title := a.tabTitle(tb)
	ic := icMessage
	if tb.branched {
		ic = icBranch
	}
	item := ui.SidebarItem(c.Key(tb.id), tb.id, ic, title)
	if indent > 0 {
		item.Padding(6, 8, 6, indent)
	}
	if tb.chat != nil && tb.chat.Busy {
		item.Tooltip(locales.ChatReplyingEllipsis())
	}
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.ProjectCloseChat()).Chosen() {
			*later = append(*later, func() { a.closeTab(tb) })
		}
	})
}

// smallButton is an icon button in a row of the sidebar.
func smallButton(c *ui.Context, key string, ic *ui.SVG, tip string, p palette) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(24, 24).Shrink(0).Radius(6).Center().TextColor(t.TextMuted).Label(tip).Tooltip(tip)
	if b.Hovered() {
		b.Background(p.subtle).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, ic).Size(14, 14) })
	return b
}

// renameDialog renames a project.
func (a *App) renameDialog(c *ui.Context, p palette) {
	f := a.renaming
	if f == nil {
		return
	}
	t := c.Theme()
	ui.DialogBase(c, &f.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.28))
		panel.Width(400).Padding(22).Radius(16).Background(p.card).Border(1, p.hairline).Gap(14).
			Shadow(0, 24, 60, 0, ui.RGBA(0, 0, 0, 0.25))
		ui.Text(c, locales.ProjectRenameTitle()).Bold().FontSize(16)
		in := ui.TextInput(c.Key("project-name"), &f.name).Placeholder(locales.ProjectRenamePlaceholder(filepath.Base(f.path))).AutoFocus().Label(locales.ProjectName())
		ui.Text(c, shortPath(f.path)).Font("monospace").FontSize(12).TextColor(t.TextMuted).SingleLine()
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				f.open = false
			}
			if ui.PrimaryButton(c, locales.CommonSave()).Clicked() || in.Submitted() {
				a.renameProject(f)
				f.open = false
			}
		})
	})
	if !f.open {
		a.renaming = nil
	}
}
