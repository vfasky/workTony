package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"worktony/internal/engine"
	"worktony/locales"
)

func (a *App) messages(c *ui.Context, p palette, tb *tab) {
	items := tb.chat.Items
	tb.list.Key = func(i int) any { return items[i].ID }
	tb.list.FollowEnd = true
	tb.list.FollowAnimation = &ui.Smooth
	ui.List(c, &tb.list, len(items), func(i int) {
		ui.Row(c).Justify(ui.Center).Padding(0, 24).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(contentWidth).MinWidth(0).Children(func() {
				a.item(c, p, tb, items, i)
			})
		})
	}).Grow(1).Padding(8, 0, 28, 0).Gap(20).Label(locales.MessageList())
}

func (a *App) item(c *ui.Context, p palette, tb *tab, items []*engine.Item, i int) {
	t := c.Theme()
	it := items[i]
	switch it.Role {
	case engine.RoleUser:
		ui.Column(c).AlignItems(ui.End).Gap(4).Children(func() {
			b := ui.Text(c, it.Text).Selectable().Padding(10, 14).Radius(18).Background(p.userBubble).MaxWidth(560).LineHeight(1.5)
			if it.Queued {
				b.Opacity(0.55)
				ui.Text(c, locales.MessageQueued()).FontSize(11).TextColor(t.TextMuted)
			}
		})
	case engine.RoleNotice:
		ui.Row(c).Justify(ui.Center).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icAlert).Size(13, 13).TextColor(t.TextMuted)
			ui.Text(c, it.Text).FontSize(12).TextColor(t.TextMuted).Selectable().MaxWidth(620)
		})
	case engine.RoleAssistant:
		first := i == 0 || items[i-1].Role != engine.RoleAssistant
		ui.Column(c).Gap(10).Children(func() {
			if first {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					brand(c, p, 20)
					ui.Text(c, it.Model).FontSize(12).TextColor(t.TextMuted).SingleLine()
				})
			}
			last := len(it.Parts) - 1
			for j, part := range it.Parts {
				live := it.Streaming && j == last
				key := itoa(it.ID) + "-" + itoa(j)
				switch part.Kind {
				case engine.PartText:
					ui.Column(c.Key("md-" + key)).Children(func() {
						if live {
							markdown.Stream(c, part.Text, &a.md)
						} else {
							markdown.View(c, part.Text, &a.md)
						}
					})
				case engine.PartThinking:
					a.thinking(c, p, tb, key, part.Text, live)
				case engine.PartTool:
					a.toolCard(c, p, tb, part.Tool)
				}
			}
			if it.Streaming && len(it.Parts) == 0 {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, locales.MessageThinking()).FontSize(13).TextColor(t.TextMuted)
				})
			}
			if it.Error != "" {
				a.errorCard(c, p, it.Error)
			}
			if it.Aborted {
				ui.Text(c, locales.MessageStopped()).FontSize(12).TextColor(t.TextMuted)
			}
		})
	}
}

func (a *App) thinking(c *ui.Context, p palette, tb *tab, key, text string, live bool) {
	t := c.Theme()
	open := tb.open["think-"+key]
	label := locales.MessageThoughts()
	if live {
		label = locales.MessageThoughtsLive()
	}
	trigger := ui.ButtonBase(c.Key("think-"+key)).AlignSelf(ui.Start).Gap(6).AlignItems(ui.Center).TextColor(t.TextMuted).Label(label).Padding(2, 0)
	if trigger.Hovered() {
		trigger.TextColor(t.Text)
	}
	trigger.Children(func() {
		ui.Icon(c, icBulb).Size(14, 14)
		ui.Text(c, label).FontSize(13)
		chev := ui.Icon(c, icChevR).Size(12, 12)
		if open {
			chev.Rotate(90)
		}
	})
	if trigger.Clicked() {
		tb.open["think-"+key] = !open
	}
	if open {
		ui.Column(c).Padding(2, 0, 2, 12).BorderWidth(0, 0, 0, 2).BorderColor(p.hairline).Children(func() {
			ui.Text(c, strings.TrimSpace(text)).FontSize(13).TextColor(t.TextMuted).Selectable().LineHeight(1.55)
		})
	}
}

var toolTitles = map[string]func() string{
	"read":  locales.ToolRead,
	"bash":  locales.ToolBash,
	"edit":  locales.ToolEdit,
	"write": locales.ToolWrite,
	"grep":  locales.ToolGrep,
	"find":  locales.ToolFind,
	"ls":    locales.ToolLs,
}

func (a *App) toolCard(c *ui.Context, p palette, tb *tab, tool *engine.Tool) {
	t := c.Theme()
	key := "tool-" + tool.ID
	open := tb.open[key]
	title := tool.Name
	if name := toolTitles[tool.Name]; name != nil {
		title = name()
	}
	ui.Column(c.Key(key)).Radius(10).Border(1, p.hairline).Background(p.card).Clip().Children(func() {
		head := ui.ButtonBase(c.Key(key+"-head")).Padding(8, 12).Gap(8).AlignItems(ui.Center).Label(title + " " + tool.Summary)
		if head.Hovered() {
			head.Background(p.subtle)
		}
		head.Children(func() {
			ui.Icon(c, toolIcon(tool.Name)).Size(14, 14).TextColor(t.TextMuted)
			ui.Text(c, title).FontSize(12).Bold().SingleLine().Shrink(0)
			ui.Text(c, tool.Summary).Font("monospace").FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
			switch tool.Status {
			case engine.ToolPending, engine.ToolRunning:
				ui.Spinner(c).Size(12, 12).Label(locales.ToolRunning())
			case engine.ToolDone:
				if d := formatDuration(tool.Duration); d != "" {
					ui.Text(c, d).FontSize(11).TextColor(t.TextMuted)
				}
				ui.Icon(c, icCheck).Size(13, 13).TextColor(p.success)
			case engine.ToolFailed:
				ui.Icon(c, icX).Size(13, 13).TextColor(t.Danger)
			}
			chev := ui.Icon(c, icChevR).Size(12, 12).TextColor(t.TextMuted)
			if open {
				chev.Rotate(90)
			}
		})
		if head.Clicked() {
			tb.open[key] = !open
		}
		if open {
			ui.Box(c).Height(1).Background(p.hairline)
			out := clip(tool.Output, 400, 24_000)
			if out == "" {
				out = locales.ToolNoOutput()
			}
			ui.Scroll(c).MaxHeight(280).Background(p.codeBg).Children(func() {
				o := ui.Text(c, out).Font("monospace").FontSize(12).Selectable().Padding(10, 12).LineHeight(1.45)
				if tool.Status == engine.ToolFailed {
					o.TextColor(t.Danger)
				}
			})
		}
	})
}

func (a *App) errorCard(c *ui.Context, p palette, msg string) {
	t := c.Theme()
	ui.Row(c).Gap(10).Padding(10, 12).Radius(10).Background(p.dangerSoft).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, icAlert).Size(16, 16).TextColor(t.Danger).Margin(1, 0, 0, 0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, msg).FontSize(13).Selectable()
			if authError(msg) {
				if ui.Button(c, locales.MessageOpenSettings()).Clicked() {
					a.host.OpenSettings(pageAccounts)
				}
			}
		})
	})
}

func authError(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range []string{"not configured", "401", "unauthorized", "api key", "credential", "authentication", "re-authenticate"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// clip keeps the last lines of long output, at most maxLines lines and
// maxBytes bytes.
func clip(s string, maxLines, maxBytes int) string {
	s = strings.TrimRight(s, "\n")
	cut := false
	if len(s) > maxBytes {
		s, cut = s[len(s)-maxBytes:], true
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	if n := strings.Count(s, "\n") + 1; n > maxLines {
		lines := strings.Split(s, "\n")
		s, cut = strings.Join(lines[len(lines)-maxLines:], "\n"), true
	}
	if cut {
		s = "…\n" + s
	}
	return s
}
