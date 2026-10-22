package app

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	titleBarHeight = 52
	sidebarWidth   = 248
	contentWidth   = 760
)

// ChatView is the chat window's view.
func (a *App) ChatView(c *ui.Context) {
	p := applyTheme(c)
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.newTab()
	}
	if a.chatToast != "" {
		c.Toast(a.chatToast)
		a.chatToast = ""
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.host.OpenSettings("")
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c, p, vibrant)
		ui.Box(c).Width(1).Background(p.hairline)
		a.chatPane(c, p)
	})
}

func (a *App) sidebar(c *ui.Context, p palette, vibrant bool) {
	t := c.Theme()
	side := ui.Column(c).Width(sidebarWidth).Shrink(0)
	if !vibrant {
		side.Background(p.sidebar)
	}
	side.Children(func() {
		bar := c.TitleBar()
		ui.Row(c).Height(titleBarHeight).Shrink(0).Padding(0, 10, 0, bar.Left+12).AlignItems(ui.Center).DragWindow().Children(func() {
			if bar.Left == 0 {
				brand(c, p, 22)
				ui.Text(c, "Pi").Bold().FontSize(15).Margin(0, 0, 0, 8)
			}
		})
		ui.Column(c).Padding(2, 10, 6, 10).Children(func() {
			row := ui.ButtonBase(c.Key("new")).Padding(8, 10).Radius(8).Gap(10).AlignItems(ui.Center).Label("新对话")
			if row.Hovered() {
				row.Background(p.subtle)
			}
			row.Children(func() {
				ui.Icon(c, icPencil).FontSize(15).TextColor(t.Text)
				ui.Text(c, "新对话").Grow(1)
				ui.Text(c, "⌘N").FontSize(11).TextColor(t.TextMuted)
			})
			if row.Clicked() {
				a.newTab()
			}
		})
		var closing *tab
		list := ui.Sidebar(c, &a.selected, func() {
			ui.SidebarSection(c, "最近", nil, func() {
				for _, tb := range a.tabs {
					title := "新对话"
					if tb.chat != nil {
						if s := tb.chat.Title(); s != "" {
							title = s
						}
					}
					item := ui.SidebarItem(c.Key(tb.id), tb.id, icMessage, title)
					if tb.chat != nil && tb.chat.Busy {
						item.Tooltip("正在回复…")
					}
					item.ContextMenu(func(m *ui.Menu) {
						if m.Item("关闭对话").Chosen() {
							closing = tb
						}
					})
				}
			})
		}).Grow(1).Label("对话")
		// The sidebar's color, or the material, shows under the list.
		list.Background(ui.Transparent)
		if closing != nil {
			a.closeTab(closing)
		}
		ui.Box(c).Height(1).Background(p.hairline).Margin(0, 12)
		ui.Column(c).Padding(8, 10, 12, 10).Children(func() {
			row := ui.ButtonBase(c.Key("settings")).Padding(8, 10).Radius(8).Gap(10).AlignItems(ui.Center).Label("设置")
			if row.Hovered() {
				row.Background(p.subtle)
			}
			row.Children(func() {
				ui.Icon(c, icSettings).FontSize(15)
				ui.Text(c, "设置").Grow(1)
				if n := a.signedIn(); n > 0 {
					ui.Text(c, "已连接 "+itoa(n)).FontSize(11).TextColor(t.TextMuted)
				}
			})
			if row.Clicked() {
				a.host.OpenSettings("")
			}
		})
	})
}

// signedIn counts the providers with a credential.
func (a *App) signedIn() int {
	n := 0
	for _, p := range a.providers {
		if p.State.Configured {
			n++
		}
	}
	return n
}

func (a *App) chatPane(c *ui.Context, p palette) {
	tb := a.current()
	bar := c.TitleBar()
	ui.Column(c).Grow(1).MinWidth(0).Background(p.pane).Children(func() {
		ui.Row(c).Height(titleBarHeight).Shrink(0).Padding(0, bar.Right+16, 0, 20).AlignItems(ui.Center).Gap(10).DragWindow().Children(func() {
			title := "新对话"
			if tb.chat != nil && tb.chat.Title() != "" {
				title = tb.chat.Title()
			}
			ui.Text(c, title).Bold().FontSize(14).SingleLine().Grow(1).MinWidth(0)
			if tb.chat != nil && tb.chat.Busy {
				ui.Spinner(c).Size(14, 14).Label("正在回复")
			}
		})
		if tb.chat == nil || len(tb.chat.Items) == 0 {
			a.emptyState(c, p, tb)
		} else {
			a.messages(c, p, tb)
		}
		a.composer(c, p, tb)
	})
}

func (a *App) emptyState(c *ui.Context, p palette, tb *tab) {
	t := c.Theme()
	ui.Column(c).Grow(1).Center().Gap(14).Padding(24).Children(func() {
		brand(c, p, 56)
		ui.Text(c, "有什么可以帮你？").FontSize(26).Bold().Margin(6, 0, 0, 0)
		where := "Pi 会在 " + shortPath(a.eng.CWD()) + " 中工作"
		if a.prefs.FullTools {
			where += "，可以读写文件并执行命令。"
		} else {
			where += "，只读取文件，不会修改。"
		}
		ui.Text(c, where).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(520)
		if !a.loaded {
			ui.Spinner(c).Margin(16, 0, 0, 0)
			return
		}
		if len(a.models) == 0 {
			a.connectCard(c, p)
			return
		}
		suggestions := [][2]string{
			{"梳理项目结构", "看看这个目录里的项目是怎么组织的，给我一个简要说明。"},
			{"找出待办事项", "找出代码里所有的 TODO 和 FIXME，按文件列出来。"},
			{"总结 README", "读一下 README，用三句话总结这个项目。"},
			{"解释一段代码", "挑一个最核心的源文件，解释它的主要逻辑。"},
		}
		ui.Grid(c).Columns(2).Gap(10).Width(560).Margin(18, 0, 0, 0).Children(func() {
			for i, s := range suggestions {
				card := ui.ButtonBase(c.Key("suggest-"+itoa(i))).Column().Padding(12, 14).Gap(4).Radius(12).
					Background(p.card).Border(1, p.hairline).Label(s[0])
				if card.Hovered() {
					card.Border(1, t.Accent.Alpha(0.35)).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.05))
				}
				card.Children(func() {
					ui.Text(c, s[0]).Bold().FontSize(13)
					ui.Text(c, s[1]).FontSize(12).TextColor(t.TextMuted).MaxLines(2)
				})
				if card.Clicked() {
					a.send(c, s[1])
				}
			}
		})
		_ = tb
	})
}

// connectCard asks to connect a model, before any is.
func (a *App) connectCard(c *ui.Context, p palette) {
	t := c.Theme()
	ui.Column(c).Width(460).Margin(18, 0, 0, 0).Padding(20).Gap(12).Radius(14).Background(p.card).Border(1, p.hairline).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icPlug).FontSize(18).TextColor(t.Accent)
			ui.Text(c, "先连接一个模型").Bold()
		})
		ui.Text(c, "用 ChatGPT、Claude 或 GitHub Copilot 等订阅账号登录，或者填入任意服务商的 API Key。").FontSize(13).TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Margin(4, 0, 0, 0).Children(func() {
			if ui.PrimaryButton(c, "使用 ChatGPT 登录").Clicked() {
				a.host.OpenSettings(pageAccounts)
				a.startLogin("openai", loginOAuth)
			}
			if ui.Button(c, "添加 API Key").Clicked() {
				a.host.OpenSettings(pageAPIKeys)
			}
		})
	})
}

func (a *App) composer(c *ui.Context, p palette, tb *tab) {
	busy := tb.chat != nil && tb.chat.Busy
	ui.Row(c).Justify(ui.Center).Padding(4, 24, 18, 24).Shrink(0).Children(func() {
		box := ui.Column(c).Grow(1).MaxWidth(contentWidth).Radius(18).Background(p.card).Border(1, p.hairline).
			Shadow(0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.06)).Padding(12, 12, 10, 16).Gap(8)
		box.Children(func() {
			placeholder := "给 Pi 发消息，Enter 发送，Shift+Enter 换行"
			if busy {
				placeholder = "补充说明会在本轮回复后发送…"
			}
			// OnTextKey can run between frames: it leaves the text to send
			// for the next build, which clears the draft once it is sent and
			// keeps it when it cannot be.
			if text := a.submitted; text != "" {
				a.submitted = ""
				if a.send(c, text) && a.draft == text {
					a.draft = ""
				}
			}
			in := ui.TextAreaBase(c.Key("draft-"+tb.id), &a.draft).Lines(1, 10).Placeholder(placeholder).AutoFocus().Label("消息").Margin(4, 0, 0, 0)
			in.OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == 0 {
					if strings.TrimSpace(k.Text()) != "" {
						a.submitted = k.Text()
						c.Services().Invalidate()
					}
					return true
				}
				return false
			})
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				// A chat's tools work in the directory and with the tools
				// it started with: the chips show those, and changing
				// either starts a new chat. An empty tab shows what a new
				// chat gets.
				dir, full := a.eng.CWD(), a.prefs.FullTools
				dirTip := "工作目录: " + dir
				toolsTip := "工具权限：点击切换"
				if tb.chat != nil {
					dir, full = tb.chat.CWD(), tb.chat.FullTools()
					dirTip = "本对话的工作目录: " + dir + "；选择其他目录会开始新对话"
					toolsTip = "本对话的工具权限；切换会开始新对话"
				}
				folder := chip(c, "cwd", icFolder, filepath.Base(dir), p).Tooltip(dirTip)
				if folder.Clicked() {
					a.chooseFolder(true)
				}
				mode, label := icShield, "只读"
				if full {
					mode, label = icZap, "可修改"
				}
				tools := chip(c, "tools", mode, label, p).Tooltip(toolsTip)
				if tools.Clicked() {
					a.prefs.FullTools = !full
					a.savePrefs()
					msg := "只读取文件"
					if a.prefs.FullTools {
						msg = "可读写文件并执行命令"
					}
					if tb.chat != nil {
						a.newTab()
						c.Toast("已开始新对话：" + msg)
					} else {
						c.Toast("新对话" + msg)
					}
				}
				if tb.chat != nil {
					a.thinkingPicker(c, p, tb)
				}
				ui.Spacer(c)
				a.modelPicker(c, p, tb)
				a.sendButton(c, p, tb, busy)
			})
		})
	})
}

func (a *App) sendButton(c *ui.Context, p palette, tb *tab, busy bool) {
	t := c.Theme()
	stop := busy && strings.TrimSpace(a.draft) == ""
	ready := strings.TrimSpace(a.draft) != ""
	b := ui.ButtonBase(c.Key("send")).Size(32, 32).Radius(16).Center()
	switch {
	case stop:
		b.Background(t.Text).TextColor(p.card).Label("停止").Tooltip("停止生成")
	case ready:
		b.Background(t.Accent).TextColor(t.AccentText).Label("发送")
		if b.Hovered() {
			b.Background(t.AccentHover)
		}
	default:
		b.Background(p.subtle).TextColor(t.TextMuted).Label("发送").Disabled(true)
	}
	b.Children(func() {
		if stop {
			ui.Icon(c, icStop).Size(14, 14)
		} else {
			ui.Icon(c, icArrowUp).Size(16, 16)
		}
	})
	if b.Clicked() {
		if stop {
			// Messages queued behind the reply go back to the composer.
			if rest := tb.chat.Abort(); rest != "" {
				a.draft = rest
				c.Toast("已停止，未发送的消息已放回输入框")
			}
		} else if ready && a.send(c, a.draft) {
			a.draft = ""
		}
	}
}

// chooseFolder asks for the directory of new chats. From the composer of a
// chat already started, newChat opens a new chat there: the chat shown
// keeps working where it started.
func (a *App) chooseFolder(newChat bool) {
	if a.host.ChooseFolder == nil {
		return
	}
	go func() {
		dir := a.host.ChooseFolder()
		if dir == "" {
			return
		}
		err := a.eng.SetCWD(dir)
		a.host.Post(func() { a.cwdChanged(dir, err, newChat) })
	}()
}

func (a *App) cwdChanged(dir string, err error, newChat bool) {
	if err != nil {
		msg := "无法使用该目录: " + err.Error()
		if newChat {
			a.chatToast = msg
		} else {
			a.settings.toast = msg
		}
		return
	}
	a.prefs.CWD = dir
	a.savePrefs()
	if newChat && a.current().chat != nil {
		a.newTab()
		a.chatToast = "已在 " + filepath.Base(dir) + " 中开始新对话"
	}
}

// brand is the app's mark: π on a gradient.
func brand(c *ui.Context, p palette, size float32) {
	ui.Box(c).Size(size, size).Radius(size*0.28).Center().
		LinearGradient(ui.LinearGradient{From: p.logoFrom, To: p.logoTo, Angle: 135, Oklab: true}).
		Shadow(0, size*0.08, size*0.3, 0, p.logoFrom.Alpha(0.25)).
		Children(func() {
			ui.Text(c, "π").FontSize(size*0.56).Bold().TextColor(ui.RGB(255, 255, 255)).Margin(0, 0, size*0.06, 0)
		})
}

// chip is a small button of the composer.
func chip(c *ui.Context, key string, ic *ui.SVG, label string, p palette) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("chip-"+key)).Padding(5, 9).Radius(8).Gap(6).AlignItems(ui.Center).TextColor(t.TextMuted).Label(label)
	if b.Hovered() {
		b.Background(p.subtle).TextColor(t.Text)
	}
	b.Children(func() {
		if ic != nil {
			ui.Icon(c, ic).Size(14, 14)
		}
		ui.Text(c, label).FontSize(12).SingleLine().MaxWidth(180)
	})
	return b
}

func iconButton(c *ui.Context, key string, ic *ui.SVG, tip string, p palette) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("icon-"+key)).Size(30, 30).Radius(8).Center().TextColor(t.TextMuted).Label(tip).Tooltip(tip)
	if b.Hovered() {
		b.Background(p.subtle).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, ic).Size(16, 16) })
	return b
}

func shortPath(path string) string {
	if home, err := userHome(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+string(filepath.Separator)) {
			return "~" + path[len(home):]
		}
	}
	return path
}

func formatDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Second:
		return itoa(int(d.Milliseconds())) + "ms"
	case d < time.Minute:
		return strings.TrimSuffix(strings.TrimRight(ftoa(d.Seconds()), "0"), ".") + "s"
	}
	return d.Round(time.Second).String()
}
