package app

import (
	"strings"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/egoist/mygo/ui"

	"worktony/locales"
)

type pickerState struct {
	open      bool
	query     string
	thinkOpen bool
}

func (a *App) modelPicker(c *ui.Context, p palette, tb *tab) {
	t := c.Theme()
	label := locales.PickerChooseModel()
	if m, ok := a.model(tb.model); ok {
		label = m.Name
	} else if !a.loaded {
		label = locales.CommonLoading()
	} else if len(a.models) == 0 {
		label = locales.PickerNoModel()
	}
	anchor := ui.ButtonBase(c.Key("model-picker")).Padding(5, 8, 5, 10).Radius(8).Gap(5).AlignItems(ui.Center).TextColor(t.TextMuted).Label(locales.PickerModelLabel(label))
	if anchor.Hovered() || a.picker.open {
		anchor.Background(p.subtle).TextColor(t.Text)
	}
	anchor.Children(func() {
		ui.Text(c, label).FontSize(12).SingleLine().MaxWidth(220)
		ui.Icon(c, icChevDown).Size(12, 12)
	})
	if anchor.Clicked() {
		if len(a.models) == 0 && a.loaded {
			a.host.OpenSettings(pageAccounts)
			return
		}
		a.picker.open, a.picker.query = !a.picker.open, ""
	}
	ui.PopoverBase(c, anchor, &a.picker.open, func(panel ui.Element) {
		panel.Width(340).Radius(12).Background(p.card).Border(1, p.hairline).
			Shadow(0, 14, 36, 0, ui.RGBA(0, 0, 0, 0.16)).Margin(6, 0, 6, 0).Padding(6).Gap(4)
		ui.SearchField(c, &a.picker.query).Placeholder(locales.PickerSearch()).AutoFocus().Margin(2, 2, 4, 2)
		q := strings.ToLower(strings.TrimSpace(a.picker.query))
		ui.Scroll(c).MaxHeight(320).Children(func() {
			group := ""
			shown := 0
			for _, m := range a.models {
				if q != "" && !strings.Contains(strings.ToLower(m.Name+" "+m.ID+" "+m.ProviderName), q) {
					continue
				}
				shown++
				if m.ProviderName != group {
					group = m.ProviderName
					ui.Text(c, group).FontSize(11).Bold().TextColor(t.TextMuted).Padding(8, 10, 4, 10)
				}
				row := ui.ButtonBase(c.Key("pick-"+m.Ref)).Padding(7, 10).Radius(7).Gap(8).AlignItems(ui.Center).Label(m.Name)
				if row.Hovered() {
					row.Background(p.subtle)
				}
				row.Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, m.Name).FontSize(13).SingleLine()
						if m.Name != m.ID {
							ui.Text(c, m.ID).FontSize(11).TextColor(t.TextMuted).SingleLine()
						}
					})
					if m.Reasoning {
						ui.Icon(c, icBulb).Size(12, 12).TextColor(t.TextMuted).Tooltip(locales.PickerReasoning())
					}
					if m.Ref == tb.model {
						ui.Icon(c, icCheck).Size(14, 14).TextColor(t.Accent)
					}
				})
				if row.Clicked() {
					a.setModel(c, m)
					a.picker.open = false
				}
			}
			if shown == 0 {
				ui.Text(c, locales.PickerNoMatch()).FontSize(12).TextColor(t.TextMuted).Padding(12)
			}
		})
		ui.Box(c).Height(1).Background(p.hairline).Margin(2, 4)
		manage := ui.ButtonBase(c.Key("pick-manage")).Padding(7, 10).Radius(7).Gap(8).AlignItems(ui.Center).TextColor(t.TextMuted).Label(locales.PickerManage())
		if manage.Hovered() {
			manage.Background(p.subtle).TextColor(t.Text)
		}
		manage.Children(func() {
			ui.Icon(c, icSettings).Size(14, 14)
			ui.Text(c, locales.PickerManageEllipsis()).FontSize(12)
		})
		if manage.Clicked() {
			a.picker.open = false
			a.host.OpenSettings(pageAccounts)
		}
	})
}

var thinkingNames = map[ai.ModelThinkingLevel]func() string{
	ai.ThinkingOff:     locales.ThinkingOff,
	ai.ThinkingMinimal: locales.ThinkingMinimal,
	ai.ThinkingLow:     locales.ThinkingLow,
	ai.ThinkingMedium:  locales.ThinkingMedium,
	ai.ThinkingHigh:    locales.ThinkingHigh,
	"xhigh":            locales.ThinkingXHigh,
	ai.ThinkingMax:     locales.ThinkingMax,
}

func thinkingName(l ai.ModelThinkingLevel) string {
	if name := thinkingNames[l]; name != nil {
		return name()
	}
	return string(l)
}

func (a *App) thinkingPicker(c *ui.Context, p palette, tb *tab) {
	t := c.Theme()
	levels := tb.chat.ThinkingLevels()
	if len(levels) <= 1 {
		return
	}
	current := tb.chat.Thinking()
	anchor := chip(c, "thinking", icBulb, locales.ThinkingLabel(thinkingName(current)), p).Tooltip(locales.ThinkingTooltip())
	if anchor.Clicked() {
		a.picker.thinkOpen = !a.picker.thinkOpen
	}
	ui.PopoverBase(c, anchor, &a.picker.thinkOpen, func(panel ui.Element) {
		panel.Width(160).Radius(12).Background(p.card).Border(1, p.hairline).
			Shadow(0, 14, 36, 0, ui.RGBA(0, 0, 0, 0.16)).Margin(6, 0, 6, 0).Padding(6)
		for _, l := range levels {
			row := ui.ButtonBase(c.Key("think-level-"+string(l))).Padding(7, 10).Radius(7).Gap(8).AlignItems(ui.Center).Label(thinkingName(l))
			if row.Hovered() {
				row.Background(p.subtle)
			}
			row.Children(func() {
				ui.Text(c, thinkingName(l)).FontSize(13).Grow(1)
				if l == current {
					ui.Icon(c, icCheck).Size(14, 14).TextColor(t.Accent)
				}
			})
			if row.Clicked() {
				if err := tb.chat.SetThinking(l); err != nil {
					c.Toast(err.Error())
				}
				a.picker.thinkOpen = false
			}
		}
	})
}
