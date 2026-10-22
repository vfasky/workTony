package app

import (
	"context"
	"errors"

	"github.com/egoist/mygo/ui"

	"worktony/internal/engine"
)

type loginKind int

const (
	loginOAuth loginKind = iota
	loginAPIKey
)

type answer struct {
	text string
	err  error
}

// loginFlow is a sign-in running in the background: what it shows, and
// the question it waits on.
type loginFlow struct {
	providerID string
	name       string
	kind       loginKind
	// open shows the dialog. An API key saved from its row shows the
	// dialog only when the provider asks more.
	open   bool
	cancel context.CancelFunc

	status       string
	url          string
	instructions string
	code         string
	codeURL      string
	infos        []string
	links        []engine.Link

	prompt *engine.Prompt
	reply  chan answer
	input  string
	choice string

	err  string
	done bool
}

func (a *App) provider(id string) (engine.Provider, bool) {
	for _, p := range a.providers {
		if p.ID == id {
			return p, true
		}
	}
	return engine.Provider{}, false
}

// startLogin signs in to a provider: with OAuth, or with the API key key.
func (a *App) startLogin(providerID string, kind loginKind, key ...string) *loginFlow {
	if f := a.settings.login; f != nil && !f.done && f.err == "" && f.cancel != nil {
		f.cancel()
	}
	name := oauthName(providerID)
	if p, ok := a.provider(providerID); ok && kind == loginAPIKey {
		name = p.Name
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &loginFlow{providerID: providerID, name: name, kind: kind, open: kind == loginOAuth, cancel: cancel, status: "正在连接…"}
	a.settings.login = f
	post := func(fn func()) {
		a.host.Post(func() {
			if a.settings.login == f {
				fn()
			}
		})
	}
	in := engine.Interaction{
		Prompt: func(ctx context.Context, p engine.Prompt) (string, error) {
			reply := make(chan answer, 1)
			post(func() {
				f.prompt, f.reply, f.input, f.open = &p, reply, "", true
				f.choice = ""
				if len(p.Options) > 0 {
					f.choice = p.Options[0].ID
				}
			})
			select {
			case ans := <-reply:
				return ans.text, ans.err
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
		Notify: func(n engine.Notice) {
			post(func() {
				switch n.Kind {
				case "auth_url":
					f.url, f.instructions = n.URL, n.Message
					f.status = "已在浏览器中打开登录页面"
					if a.host.OpenURL != nil {
						a.host.OpenURL(n.URL)
					}
				case "device_code":
					f.code, f.codeURL = n.Code, n.URL
				case "progress":
					f.status = n.Message
				case "info":
					if n.Message != "" {
						f.infos = append(f.infos, n.Message)
					}
					f.links = append(f.links, n.Links...)
				}
			})
		},
	}
	go func() {
		var err error
		if kind == loginAPIKey && len(key) > 0 {
			err = a.eng.SaveAPIKey(ctx, providerID, key[0], in)
		} else {
			kindOf := engine.KindOAuth
			if kind == loginAPIKey {
				kindOf = engine.KindAPIKey
			}
			err = a.eng.Login(ctx, providerID, kindOf, in)
		}
		post(func() {
			f.prompt = nil
			switch {
			case err == nil:
				f.done, f.open = true, false
				a.settings.toast = "已连接 " + f.name
				a.settings.keyEditing, a.settings.keyInput = "", ""
				a.Refresh()
			case errors.Is(err, context.Canceled) || ctx.Err() != nil:
				f.done, f.open = true, false
			default:
				f.err = err.Error()
				if f.kind == loginAPIKey && !f.open {
					a.settings.keyErr = f.err
				}
			}
		})
	}()
	return f
}

func (f *loginFlow) answer(text string) {
	if f.reply == nil {
		return
	}
	f.reply <- answer{text: text}
	f.prompt, f.reply = nil, nil
	f.status = "正在验证…"
}

func (f *loginFlow) stop() {
	if f.cancel != nil {
		f.cancel()
	}
	if f.reply != nil {
		f.reply <- answer{err: context.Canceled}
		f.reply = nil
	}
	f.open, f.done = false, true
}

// busy reports a sign-in to the provider still running.
func (a *App) loggingIn(providerID string) bool {
	f := a.settings.login
	return f != nil && f.providerID == providerID && !f.done && f.err == ""
}

// loginDialog shows the running sign-in.
func (a *App) loginDialog(c *ui.Context, p palette) {
	f := a.settings.login
	if f == nil {
		return
	}
	t := c.Theme()
	wasOpen := f.open
	ui.DialogBase(c, &f.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.28))
		panel.Width(440).Padding(22).Radius(16).Background(p.card).Border(1, p.hairline).Gap(14).
			Shadow(0, 24, 60, 0, ui.RGBA(0, 0, 0, 0.25))
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			providerMark(c, f.providerID, f.name, 36)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				title := "登录 " + f.name
				if f.kind == loginAPIKey {
					title = "配置 " + f.name
				}
				ui.Text(c, title).Bold().FontSize(16)
				ui.Text(c, "凭据保存在本机 auth.json，仅自己可读").FontSize(12).TextColor(t.TextMuted)
			})
		})
		// A pasted code is the fallback of a browser sign-in, asked beside it.
		manual := f.prompt != nil && f.prompt.Kind == "manual_code"
		switch {
		case f.err != "":
			a.errorCard(c, p, f.err)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "关闭").Clicked() {
					f.open = false
				}
				if ui.PrimaryButton(c, "重试").Clicked() {
					a.startLogin(f.providerID, f.kind)
				}
			})
			return
		case f.prompt != nil && !manual:
			a.promptForm(c, p, f)
		case f.code != "":
			ui.Text(c, "在浏览器中打开下面的网址，输入这个代码完成授权：").FontSize(13)
			ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(12, 16).Radius(12).Background(p.codeBg).Children(func() {
				ui.Text(c, f.code).Font("monospace").FontSize(22).Bold().LetterSpacing(2).Selectable().Grow(1)
				if iconButton(c, "copy-code", icCopy, "复制代码", p).Clicked() {
					c.Services().WriteClipboard(f.code)
					c.Toast("已复制")
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, f.codeURL).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
				if ui.Button(c, "打开网页").Clicked() {
					c.Services().OpenURL(f.codeURL)
				}
			})
			waiting(c, "等待授权完成…")
		case f.url != "":
			ui.Text(c, "已在浏览器中打开登录页面，授权完成后会自动回到这里。").FontSize(13)
			if f.instructions != "" {
				ui.Text(c, f.instructions).FontSize(12).TextColor(t.TextMuted)
			}
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "重新打开浏览器").Clicked() {
					c.Services().OpenURL(f.url)
				}
				if ui.Button(c, "复制登录链接").Clicked() {
					c.Services().WriteClipboard(f.url)
					c.Toast("已复制登录链接")
				}
			})
			waiting(c, f.status)
		default:
			waiting(c, f.status)
		}
		if manual {
			ui.Box(c).Height(1).Background(p.hairline)
			a.promptForm(c, p, f)
		}
		for _, info := range f.infos {
			ui.Text(c, info).FontSize(12).TextColor(t.TextMuted).Selectable()
		}
		for _, l := range f.links {
			label := l.Label
			if label == "" {
				label = l.URL
			}
			ui.Link(c, label, l.URL).FontSize(12)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Margin(4, 0, 0, 0).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				f.stop()
			}
			if f.prompt != nil && ui.PrimaryButton(c, "继续").Disabled(!f.ready()).Clicked() {
				f.submit()
			}
		})
	})
	// Escape or a click outside closes the dialog: that cancels a sign-in
	// still running.
	if wasOpen && !f.open && !f.done && f.err == "" {
		f.stop()
	}
}

func (a *App) promptForm(c *ui.Context, p palette, f *loginFlow) {
	t := c.Theme()
	pr := f.prompt
	if pr.Message != "" {
		ui.Text(c, pr.Message).FontSize(13).Selectable()
	}
	if pr.Kind == "select" {
		ui.RadioGroup(c, func() {
			for _, o := range pr.Options {
				ui.Column(c).Gap(2).Children(func() {
					ui.Radio(c, &f.choice, o.ID, o.Label)
					if o.Description != "" {
						ui.Text(c, o.Description).FontSize(12).TextColor(t.TextMuted).Margin(0, 0, 0, 26)
					}
				})
			}
		})
		return
	}
	in := ui.TextInput(c.Key("prompt-input"), &f.input).Placeholder(pr.Placeholder).AutoFocus().Label(pr.Message)
	if pr.Kind == "secret" {
		in.Password()
	}
	if pr.Kind == "manual_code" {
		ui.Text(c, "如果浏览器没有自动跳回，可以把页面上显示的代码或完整回调网址粘贴到这里。").FontSize(12).TextColor(t.TextMuted)
	}
	if in.Submitted() && f.ready() {
		f.submit()
	}
}

// ready reports an answer to give the prompt.
func (f *loginFlow) ready() bool {
	if f.prompt == nil {
		return false
	}
	if f.prompt.Kind == "select" {
		return f.choice != ""
	}
	return f.input != ""
}

func (f *loginFlow) submit() {
	if f.prompt != nil && f.prompt.Kind == "select" {
		f.answer(f.choice)
	} else {
		f.answer(f.input)
	}
}

func waiting(c *ui.Context, text string) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Spinner(c).Size(14, 14)
		ui.Text(c, text).FontSize(13).TextColor(c.Theme().TextMuted)
	})
}
