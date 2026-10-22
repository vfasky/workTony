package app

import (
	"strings"
	"time"

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
			if endsTurn(items, i, tb.chat.Busy) {
				a.replyFooter(c, p, tb, items, i)
			}
		})
	}
}

// endsTurn reports whether items[i], a reply, is the last of a turn that
// has ended: a message sent after it follows, or the chat is idle.
func endsTurn(items []*engine.Item, i int, busy bool) bool {
	if items[i].Streaming {
		return false
	}
	for _, it := range items[i+1:] {
		switch {
		case it.Role == engine.RoleAssistant:
			return false
		case it.Role == engine.RoleUser && !it.Queued:
			return true
		}
	}
	return !busy
}

// replyFooter is the bar under the replies of a turn, items[i] the last:
// it copies the reply, opens a new chat that goes on from it, and says how
// fast the model wrote.
func (a *App) replyFooter(c *ui.Context, p palette, tb *tab, items []*engine.Item, i int) {
	t := c.Theme()
	first := i
	for first > 0 && (items[first-1].Role != engine.RoleUser || items[first-1].Queued) {
		first--
	}
	// The text copied is the turn's last, the answer that follows the
	// steps before it.
	var text string
	tokens, took := 0, time.Duration(0)
	for _, it := range items[first : i+1] {
		if it.Role != engine.RoleAssistant {
			continue
		}
		if s := replyText(it); s != "" {
			text = s
		}
		if it.Tokens > 0 && it.GenTime > 0 {
			tokens, took = tokens+it.Tokens, took+it.GenTime
		}
	}
	last := items[i]
	key := "reply-" + itoa(last.ID)
	ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
		if text != "" && replyButton(c, key+"-copy", icCopy, locales.MessageCopy(), p).Clicked() {
			c.Services().WriteClipboard(text)
			c.Toast(locales.CommonCopied())
		}
		if replyButton(c, key+"-branch", icBranch, locales.MessageBranch(), p).Clicked() {
			a.branch(tb, last)
		}
		if tokens > 0 {
			ui.Text(c, locales.MessageSpeed(float64(tokens)/took.Seconds())).FontSize(12).TextColor(t.TextMuted).Margin(0, 0, 0, 6).
				Tooltip(locales.MessageSpeedTooltip(tokens, took.Seconds()))
		}
	})
}

// replyText is the text of a reply, without its thoughts and tool calls.
func replyText(it *engine.Item) string {
	var texts []string
	for _, part := range it.Parts {
		if part.Kind == engine.PartText && strings.TrimSpace(part.Text) != "" {
			texts = append(texts, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(texts, "\n\n")
}

// replyButton is an icon button of the bar under a reply.
func replyButton(c *ui.Context, key string, ic *ui.SVG, tip string, p palette) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(26, 26).Radius(6).Center().TextColor(t.TextMuted).Label(tip).Tooltip(tip)
	if b.Hovered() {
		b.Background(p.subtle).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, ic).Size(14, 14) })
	return b
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
	"read":     locales.ToolRead,
	"bash":     locales.ToolBash,
	"edit":     locales.ToolEdit,
	"write":    locales.ToolWrite,
	"grep":     locales.ToolGrep,
	"find":     locales.ToolFind,
	"ls":       locales.ToolLs,
	"subagent": locales.ToolSubagent,
}

func toolTitle(name string) string {
	if title := toolTitles[name]; title != nil {
		return title()
	}
	return name
}

func running(tool *engine.Tool) bool {
	return tool.Status == engine.ToolPending || tool.Status == engine.ToolRunning
}

// toolOutput is the output a call shows: its last lines, or what it
// waits for.
func toolOutput(tool *engine.Tool) string {
	out := clip(tool.Output, 400, 24_000)
	switch {
	case out != "":
		return out
	case running(tool):
		return locales.ToolWaitingOutput()
	}
	return locales.ToolNoOutput()
}

// toolStatus shows how far a call has got: a spinner while it runs, then
// how long it took and whether it worked.
func toolStatus(c *ui.Context, p palette, tool *engine.Tool) {
	t := c.Theme()
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
}

func (a *App) toolCard(c *ui.Context, p palette, tb *tab, tool *engine.Tool) {
	t := c.Theme()
	key := "tool-" + tool.ID
	open := tb.open[key]
	title := toolTitle(tool.Name)
	busy := running(tool)
	ui.Column(c.Key(key)).Radius(10).Border(1, p.hairline).Background(p.card).Clip().Children(func() {
		head := ui.ButtonBase(c.Key(key+"-head")).Padding(8, 12).Gap(8).AlignItems(ui.Center).Label(title + " " + tool.Summary)
		if head.Hovered() {
			head.Background(p.subtle)
		}
		head.Children(func() {
			ui.Icon(c, toolIcon(tool.Name)).Size(14, 14).TextColor(t.TextMuted)
			ui.Text(c, title).FontSize(12).Bold().SingleLine().Shrink(0)
			ui.Text(c, tool.Summary).Font("monospace").FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
			if n := len(tool.Children); n > 0 && !busy && !open {
				ui.Text(c, locales.ToolCallCount(n)).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			}
			toolStatus(c, p, tool)
			chev := ui.Icon(c, icChevR).Size(12, 12).TextColor(t.TextMuted)
			if open {
				chev.Rotate(90)
			}
		})
		if head.Clicked() {
			tb.open[key] = !open
		}
		// The calls the tool made, as a codemode script's, show while it
		// runs, as what it does, and once it is done, when it is open.
		if len(tool.Children) > 0 && (busy || open) {
			ui.Box(c).Height(1).Background(p.hairline)
			ui.Column(c).Padding(4, 0).Children(func() {
				for _, child := range tool.Children {
					a.toolChild(c, p, tb, child)
				}
			})
		}
		if open && !(busy && len(tool.Children) > 0 && tool.Output == "") {
			ui.Box(c).Height(1).Background(p.hairline)
			ui.Scroll(c).MaxHeight(280).Background(p.codeBg).Children(func() {
				o := ui.Text(c, toolOutput(tool)).Font("monospace").FontSize(12).Selectable().Padding(10, 12).LineHeight(1.45)
				if tool.Status == engine.ToolFailed {
					o.TextColor(t.Danger)
				}
			})
		}
	})
}

// toolChild is a row of a call another tool made. While it runs, the last
// line of its output, as a sub-agent's progress, says what it does; a
// click shows the output.
func (a *App) toolChild(c *ui.Context, p palette, tb *tab, tool *engine.Tool) {
	t := c.Theme()
	key := "tool-" + tool.ID
	open := tb.open[key]
	title := toolTitle(tool.Name)
	// The rows line up under the title of the card.
	row := ui.ButtonBase(c.Key(key)).Column().AlignItems(ui.Stretch).Justify(ui.Start).Padding(5, 12, 5, 34).Gap(2).Label(title + " " + tool.Summary)
	if row.Hovered() {
		row.Background(p.subtle)
	}
	row.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, toolIcon(tool.Name)).Size(13, 13).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, title).FontSize(12).SingleLine().Shrink(0)
			ui.Text(c, tool.Summary).Font("monospace").FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
			toolStatus(c, p, tool)
		})
		if line := lastLine(tool.Output); line != "" && running(tool) && !open {
			ui.Text(c, line).FontSize(11).TextColor(t.TextMuted).SingleLine().Margin(0, 0, 0, 21)
		}
	})
	if row.Clicked() {
		tb.open[key] = !open
	}
	if open {
		ui.Scroll(c).MaxHeight(220).Radius(6).Background(p.codeBg).Margin(2, 12, 6, 55).Children(func() {
			o := ui.Text(c, toolOutput(tool)).Font("monospace").FontSize(12).Selectable().Padding(8, 10).LineHeight(1.45)
			if tool.Status == engine.ToolFailed {
				o.TextColor(t.Danger)
			}
		})
	}
}

// lastLine is the last line of s with text.
func lastLine(s string) string {
	s = strings.TrimRight(s, " \t\r\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
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
