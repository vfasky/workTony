package app

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"worktony/internal/engine"
)

// Pages of the settings window.
const (
	pageAccounts  = "accounts"
	pageAPIKeys   = "apikeys"
	pageEndpoints = "endpoints"
	pageGeneral   = "general"
)

type settingsState struct {
	page  string
	login *loginFlow
	// toast is shown by the settings window's next frame.
	toast string

	keyQuery   string
	keyEditing string
	keyInput   string
	keyErr     string
	showAll    bool

	form          *endpointForm
	confirmDelete bool
	deleting      string
}

type endpointForm struct {
	open      bool
	editing   string // the ID edited, "" for a new endpoint
	name      string
	id        string
	idTouched bool
	baseURL   string
	api       string
	key       string
	models    string
	busy      bool
	fetching  bool
	err       string
}

// ShowSettingsPage selects a page of the settings window.
func (a *App) ShowSettingsPage(page string) {
	if page != "" {
		a.settings.page = page
	}
}

// SettingsView is the settings window's view.
func (a *App) SettingsView(c *ui.Context) {
	p := applyTheme(c)
	if a.settings.page == "" {
		a.settings.page = pageAccounts
	}
	if a.settings.toast != "" {
		c.Toast(a.settings.toast)
		a.settings.toast = ""
	}
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		nav := ui.Column(c).Width(212).Shrink(0)
		if !vibrant {
			nav.Background(p.sidebar)
		}
		nav.Children(func() {
			ui.Box(c).Height(titleBarHeight).Shrink(0).DragWindow()
			list := ui.Sidebar(c, &a.settings.page, func() {
				ui.SidebarItem(c, pageAccounts, icUser, "账号登录")
				ui.SidebarItem(c, pageAPIKeys, icKey, "API Key")
				ui.SidebarItem(c, pageEndpoints, icPlug, "自定义端点")
				ui.SidebarItem(c, pageGeneral, icSliders, "通用")
			}).Grow(1).Label("设置")
			list.Background(ui.Transparent)
		})
		ui.Box(c).Width(1).Background(p.hairline)
		bar := c.TitleBar()
		ui.Column(c).Grow(1).MinWidth(0).Background(p.pane).Children(func() {
			titles := map[string]string{pageAccounts: "账号登录", pageAPIKeys: "API Key", pageEndpoints: "自定义端点", pageGeneral: "通用"}
			ui.Row(c).Height(titleBarHeight).Shrink(0).Padding(0, bar.Right+20, 0, 28).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, titles[a.settings.page]).Bold().FontSize(15)
			})
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Padding(4, 28, 32, 28).Gap(12).MaxWidth(680).Children(func() {
					switch a.settings.page {
					case pageAccounts:
						a.accountsPage(c, p)
					case pageAPIKeys:
						a.apiKeysPage(c, p)
					case pageEndpoints:
						a.endpointsPage(c, p)
					default:
						a.generalPage(c, p)
					}
				})
			})
		})
	})
	a.loginDialog(c, p)
	a.endpointDialog(c, p)
}

func intro(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(13).TextColor(c.Theme().TextMuted).LineHeight(1.5).Margin(0, 0, 6, 0)
}

func card(c *ui.Context, key string, p palette) ui.Element {
	return ui.Row(c.Key(key)).Padding(14, 16).Radius(12).Background(p.card).Border(1, p.hairline).Gap(14).AlignItems(ui.Center)
}

func (a *App) accountsPage(c *ui.Context, p palette) {
	t := c.Theme()
	intro(c, "用已有的订阅账号登录，无需 API Key。登录会在浏览器中完成，凭据只保存在本机。")
	if !a.loaded {
		ui.Spinner(c)
		return
	}
	for _, pr := range engine.SubscriptionProviders(a.providers) {
		if pr.ID == "openai-codex" {
			continue // the legacy sign-in of the same ChatGPT plan
		}
		oauth := pr.State.Configured && pr.State.Kind == engine.KindOAuth && pr.State.Source == "stored"
		card(c, "acct-"+pr.ID, p).Children(func() {
			providerMark(c, pr.ID, oauthName(pr.ID), 38)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, oauthName(pr.ID)).Bold()
					if pr.OAuth.Subscription {
						ui.Text(c, "订阅").FontSize(10).TextColor(t.Accent).Padding(1, 6).Radius(6).Background(p.accentSoft)
					}
				})
				ui.Text(c, oauthDesc(pr)).FontSize(12).TextColor(t.TextMuted).SingleLine()
			})
			switch {
			case a.loggingIn(pr.ID):
				ui.Spinner(c).Size(16, 16).Label("登录中")
			case oauth:
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(7, 7).Radius(4).Background(p.success)
					ui.Text(c, "已登录").FontSize(12).TextColor(t.TextMuted)
				})
				if ui.Button(c.Key("logout-"+pr.ID), "退出").Clicked() {
					a.logout(pr.ID, oauthName(pr.ID))
				}
			default:
				// ChatGPT leads; the others are quieter.
				var b ui.Element
				if pr.ID == "openai" {
					b = ui.PrimaryButton(c.Key("login-"+pr.ID), "使用 ChatGPT 登录")
				} else {
					b = ui.Button(c.Key("login-"+pr.ID), "登录").Label("登录 " + oauthName(pr.ID))
				}
				if b.Clicked() {
					a.startLogin(pr.ID, loginOAuth)
				}
			}
		})
	}
	ui.Text(c, "登录 ChatGPT 使用的是 Codex 订阅通道，额度与 ChatGPT 账户共享。").FontSize(12).TextColor(t.TextMuted).Margin(6, 0, 0, 0)
}

func (a *App) logout(id, name string) {
	go func() {
		err := a.eng.Logout(context.Background(), id)
		a.host.Post(func() {
			if err != nil {
				a.settings.toast = "退出失败: " + err.Error()
				return
			}
			a.settings.toast = "已退出 " + name
			a.Refresh()
		})
	}()
}

func (a *App) apiKeysPage(c *ui.Context, p palette) {
	t := c.Theme()
	intro(c, "填入服务商的 API Key 即可使用它的全部模型。密钥保存在 "+shortPath(a.eng.AgentDir())+"/auth.json；环境变量中已有的密钥会自动识别。")
	ui.SearchField(c, &a.settings.keyQuery).Placeholder("搜索服务商").Margin(0, 0, 4, 0)
	if !a.loaded {
		ui.Spinner(c)
		return
	}
	q := strings.ToLower(strings.TrimSpace(a.settings.keyQuery))
	all := engine.APIKeyProviders(a.providers)
	hidden := 0
	for _, pr := range all {
		// Custom endpoints have a page of their own; a provider without
		// chat models, as TypeSafe of classifiers, has nothing to chat with.
		if pr.Custom || pr.APIKey == nil || pr.Models == 0 {
			continue
		}
		if q != "" {
			if !strings.Contains(strings.ToLower(pr.Name+" "+pr.ID), q) {
				continue
			}
		} else if !a.settings.showAll && !pr.State.Configured && !featuredProvider(pr.ID) && a.settings.keyEditing != pr.ID {
			hidden++
			continue
		}
		a.apiKeyRow(c, p, pr)
	}
	if hidden > 0 {
		more := ui.ButtonBase(c.Key("show-all")).Padding(8, 4).TextColor(t.Accent).Label("显示全部")
		more.Children(func() { ui.Text(c, "显示其余 "+itoa(hidden)+" 个服务商").FontSize(13) })
		if more.Clicked() {
			a.settings.showAll = true
		}
	}
}

func (a *App) apiKeyRow(c *ui.Context, p palette, pr engine.Provider) {
	t := c.Theme()
	editing := a.settings.keyEditing == pr.ID
	ui.Column(c.Key("key-"+pr.ID)).Padding(12, 16).Radius(12).Background(p.card).Border(1, p.hairline).Gap(10).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			providerMark(c, pr.ID, pr.Name, 30)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, pr.Name).Bold().FontSize(13)
				status, ok := keyStatus(pr)
				s := ui.Text(c, status).FontSize(12).TextColor(t.TextMuted).SingleLine()
				if ok {
					s.TextColor(p.success)
				}
			})
			if a.loggingIn(pr.ID) && a.settings.login.kind == loginAPIKey {
				ui.Spinner(c).Size(16, 16).Label("保存中")
				return
			}
			if editing {
				return
			}
			stored := pr.State.Source == "stored" && pr.State.Kind == engine.KindAPIKey
			label := "设置"
			if stored {
				label = "更换"
			}
			if ui.Button(c.Key("edit-"+pr.ID), label).Label(label + " " + pr.Name + " API Key").Clicked() {
				a.settings.keyEditing, a.settings.keyInput, a.settings.keyErr = pr.ID, "", ""
			}
			if stored && iconButton(c, "remove-"+pr.ID, icTrash, "移除 API Key", p).Clicked() {
				a.logout(pr.ID, pr.Name)
			}
		})
		if !editing {
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c.Key("key-input-"+pr.ID), &a.settings.keyInput).Password().Placeholder("粘贴 API Key").AutoFocus().Grow(1).Label(pr.Name + " API Key")
			save := ui.PrimaryButton(c.Key("save-"+pr.ID), "保存").Label("保存 " + pr.Name + " API Key").Disabled(strings.TrimSpace(a.settings.keyInput) == "" || a.loggingIn(pr.ID))
			if (save.Clicked() || in.Submitted()) && strings.TrimSpace(a.settings.keyInput) != "" {
				a.settings.keyErr = ""
				a.startLogin(pr.ID, loginAPIKey, a.settings.keyInput)
			}
			if ui.Button(c.Key("cancel-"+pr.ID), "取消").Clicked() {
				a.settings.keyEditing, a.settings.keyInput, a.settings.keyErr = "", "", ""
			}
		})
		if url := keyURLs[pr.ID]; url != "" {
			ui.Link(c, "获取 "+pr.Name+" API Key ↗", url).FontSize(12)
		}
		if a.settings.keyErr != "" {
			ui.Text(c, a.settings.keyErr).FontSize(12).TextColor(t.Danger).Selectable()
		}
	})
}

func keyStatus(pr engine.Provider) (string, bool) {
	st := pr.State
	switch {
	case !st.Configured:
		return "未配置", false
	case st.Source == "stored" && st.Kind == engine.KindOAuth:
		return "已通过账号登录", true
	case st.Source == "stored":
		return "已保存 API Key", true
	case st.Source == "environment":
		label := "环境变量"
		if st.Label != "" {
			label += " " + st.Label
		}
		return "来自" + label, true
	}
	return "已配置", true
}

func (a *App) endpointsPage(c *ui.Context, p palette) {
	t := c.Theme()
	intro(c, "接入 Ollama、LM Studio、vLLM，或任意 OpenAI / Anthropic 兼容的网关。地址和模型写入 models.json，密钥保存在 auth.json。")
	ui.Row(c).Children(func() {
		add := ui.PrimaryButton(c, "").Children(func() {
			ui.Icon(c, icPlus)
			ui.Text(c, "添加端点").SingleLine()
		})
		if add.Clicked() {
			a.settings.form = &endpointForm{open: true, api: engine.CustomAPIs[0].ID}
		}
	})
	if len(a.customs) == 0 {
		ui.Column(c).Padding(28).Radius(12).Border(1, p.hairline).Center().Gap(6).Children(func() {
			ui.Icon(c, icGlobe).FontSize(22).TextColor(t.TextMuted)
			ui.Text(c, "还没有自定义端点").TextColor(t.TextMuted)
		})
	}
	for _, cp := range a.customs {
		card(c, "ep-"+cp.ID, p).Children(func() {
			ui.Box(c).Size(34, 34).Radius(9).Background(p.accentSoft).Center().Children(func() {
				ui.Icon(c, icPlug).Size(16, 16).TextColor(t.Accent)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					name := cp.Name
					if name == "" {
						name = cp.ID
					}
					ui.Text(c, name).Bold()
					ui.Text(c, itoa(len(cp.Models))+" 个模型").FontSize(11).TextColor(t.TextMuted)
				})
				ui.Text(c, cp.BaseURL).Font("monospace").FontSize(12).TextColor(t.TextMuted).SingleLine()
			})
			if ui.Button(c.Key("ep-edit-"+cp.ID), "编辑").Clicked() {
				a.settings.form = &endpointForm{open: true, editing: cp.ID, name: cp.Name, id: cp.ID, idTouched: true, baseURL: cp.BaseURL, api: cp.API, models: strings.Join(cp.Models, "\n")}
			}
			if iconButton(c, "ep-del-"+cp.ID, icTrash, "删除端点", p).Clicked() {
				a.settings.deleting, a.settings.confirmDelete = cp.ID, true
			}
		})
	}
	if a.settings.confirmDelete {
		switch ui.AlertDialog(c, &a.settings.confirmDelete, "删除端点 "+a.settings.deleting+"？", "它的配置和保存的密钥都会被移除。", "取消", "删除") {
		case 1:
			id := a.settings.deleting
			go func() {
				err := a.eng.DeleteCustomProvider(context.Background(), id)
				a.host.Post(func() {
					if err != nil {
						a.settings.toast = "删除失败: " + err.Error()
					}
					a.Refresh()
				})
			}()
		}
	}
}

var presets = []struct{ name, id, url string }{
	{"Ollama", "ollama", "http://localhost:11434/v1"},
	{"LM Studio", "lmstudio", "http://localhost:1234/v1"},
	{"vLLM", "vllm", "http://localhost:8000/v1"},
	{"OpenAI 兼容", "", "https://"},
}

var idUnsafe = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(name string) string {
	s := strings.Trim(idUnsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-.")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func (a *App) endpointDialog(c *ui.Context, p palette) {
	f := a.settings.form
	if f == nil {
		return
	}
	t := c.Theme()
	ui.DialogBase(c, &f.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.28))
		panel.Width(520).Padding(22).Radius(16).Background(p.card).Border(1, p.hairline).Gap(14).
			Shadow(0, 24, 60, 0, ui.RGBA(0, 0, 0, 0.25))
		title := "添加端点"
		if f.editing != "" {
			title = "编辑端点"
		}
		ui.Text(c, title).Bold().FontSize(16)
		if f.editing == "" {
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for _, pre := range presets {
					b := chip(c, "preset-"+pre.name, nil, pre.name, p).Border(1, p.hairline)
					if b.Clicked() {
						f.name, f.baseURL, f.api = pre.name, pre.url, engine.CustomAPIs[0].ID
						if pre.id != "" {
							f.id, f.idTouched = pre.id, false
						} else {
							f.name, f.id = "", ""
						}
					}
				}
			})
		}
		// The fields scroll in a window too short for them; the title and
		// the buttons stay.
		_, winH := c.Size()
		ui.Scroll(c).MaxHeight(max(180, winH-240)).Children(func() {
			ui.Column(c).Gap(12).Padding(0, 2, 2, 2).Children(func() { a.endpointFields(c, f) })
		})
		if f.err != "" {
			ui.Text(c, f.err).FontSize(12).TextColor(t.Danger).Selectable()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				f.open = false
			}
			if ui.PrimaryButton(c, "保存").Disabled(f.busy).Clicked() {
				a.saveEndpoint(f)
			}
		})
	})
	if !f.open {
		a.settings.form = nil
	}
}

func (a *App) endpointFields(c *ui.Context, f *endpointForm) {
	ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
			ui.Field(c, "名称", func() {
				if ui.TextInput(c.Key("ep-name"), &f.name).Placeholder("例如 公司网关").AutoFocus().Changed() && !f.idTouched && f.editing == "" {
					f.id = slug(f.name)
				}
			})
		})
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
			ui.Field(c, "ID", func() {
				in := ui.TextInput(c.Key("ep-id"), &f.id).Placeholder("例如 my-gateway").ReadOnly(f.editing != "")
				if in.Changed() {
					f.idTouched = true
				}
			}).Description("模型显示为 " + orDash(f.id) + "/<模型 ID>")
		})
	})
	ui.Field(c, "Base URL", func() {
		ui.TextInput(c.Key("ep-url"), &f.baseURL).Placeholder("http://localhost:11434/v1")
	})
	ui.Field(c, "接口协议", func() {
		labels := make([]string, len(engine.CustomAPIs))
		current := ""
		for i, api := range engine.CustomAPIs {
			labels[i] = api.Label
			if api.ID == f.api {
				current = api.Label
			}
		}
		if ui.Select(c.Key("ep-api"), &current, labels).Changed() {
			for _, api := range engine.CustomAPIs {
				if api.Label == current {
					f.api = api.ID
				}
			}
		}
	})
	keyHint := "本地服务通常无需密钥，可留空"
	if f.editing != "" {
		keyHint = "留空则保持现有密钥"
	}
	ui.Field(c, "API Key", func() {
		ui.TextInput(c.Key("ep-key"), &f.key).Password().Placeholder(keyHint)
	})
	ui.Field(c, "模型", func() {
		ui.Column(c).Gap(6).Children(func() {
			ui.TextArea(c.Key("ep-models"), &f.models).Lines(3, 6).Placeholder("每行一个模型 ID，例如 llama3.2")
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				fetch := ui.Button(c.Key("ep-fetch"), "从服务获取模型列表").Disabled(f.fetching || !strings.HasPrefix(f.baseURL, "http"))
				if fetch.Clicked() {
					a.fetchModels(f)
				}
				if f.fetching {
					ui.Spinner(c).Size(14, 14)
				}
			})
		})
	})
}

func orDash(s string) string {
	if s == "" {
		return "<ID>"
	}
	return s
}

func (a *App) fetchModels(f *endpointForm) {
	f.fetching, f.err = true, ""
	base, key := f.baseURL, f.key
	go func() {
		ids, err := engine.FetchModelIDs(context.Background(), base, key)
		a.host.Post(func() {
			f.fetching = false
			switch {
			case err != nil:
				f.err = "获取模型列表失败: " + err.Error()
			case len(ids) == 0:
				f.err = "服务没有返回模型"
			default:
				f.models = strings.Join(ids, "\n")
			}
		})
	}()
}

func (a *App) saveEndpoint(f *endpointForm) {
	f.busy, f.err = true, ""
	cp := engine.CustomProvider{ID: f.id, Name: f.name, BaseURL: f.baseURL, API: f.api, Models: strings.Split(f.models, "\n")}
	key := f.key
	go func() {
		err := a.eng.SaveCustomProvider(context.Background(), cp, key)
		a.host.Post(func() {
			f.busy = false
			if err != nil {
				f.err = err.Error()
				return
			}
			f.open = false
			a.settings.toast = "已保存端点 " + orDash(cp.Name)
			a.Refresh()
		})
	}()
}

func (a *App) generalPage(c *ui.Context, p palette) {
	ui.Column(c).Gap(22).Children(func() { a.generalFields(c, p) })
}

func (a *App) generalFields(c *ui.Context, p palette) {
	t := c.Theme()
	ui.Field(c, "默认模型", func() {
		if len(a.models) == 0 {
			ui.Text(c, "尚未连接模型").TextColor(t.TextMuted)
			return
		}
		labels := make([]string, len(a.models))
		current := ""
		effective := a.modelFor(a.defModel)
		for i, m := range a.models {
			labels[i] = m.Name + " · " + m.ProviderName
			if m.Ref == effective {
				current = labels[i]
			}
		}
		if ui.Select(c.Key("default-model"), &current, labels).Width(360).Changed() {
			if i := slices.Index(labels, current); i >= 0 {
				m := a.models[i]
				a.setDefaultModel(m)
				a.settings.toast = "新对话将使用 " + m.Name
			}
		}
	}).Description("新对话默认使用的模型，与 pig 命令行共用 settings.json")
	ui.Field(c, "工具权限", func() {
		mode := "read"
		if a.prefs.FullTools {
			mode = "full"
		}
		ui.RadioGroup(c, func() {
			ui.Radio(c, &mode, "read", "只读：读取、搜索和列出文件")
			ui.Radio(c, &mode, "full", "完整：还可以编辑、写入文件和执行命令")
		})
		if full := mode == "full"; full != a.prefs.FullTools {
			a.prefs.FullTools = full
			a.savePrefs()
		}
	}).Description("对新对话生效。Pi 执行工具前不会逐次确认，开启完整权限请选择可信的工作目录。")
	ui.Field(c, "工作目录", func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, shortPath(a.eng.CWD())).Font("monospace").FontSize(12).SingleLine().Grow(1).MinWidth(0)
			if ui.Button(c, "选择…").Clicked() {
				a.chooseFolder(false)
			}
		})
	}).Description("新对话的工作目录；已开始的对话继续在各自的目录中工作")
	ui.Field(c, "配置目录", func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, shortPath(a.eng.AgentDir())).Font("monospace").FontSize(12).SingleLine().Grow(1).MinWidth(0)
			if a.host.Reveal != nil && ui.Button(c, "打开").Clicked() {
				a.host.Reveal(a.eng.AgentDir())
			}
		})
	}).Description("auth.json、models.json、settings.json 与会话记录，与 pig 命令行共用")
	ui.Text(c, "引擎 PiG v0.5.1 · 界面 MyGo v0.6.2").FontSize(12).TextColor(t.TextMuted).Margin(12, 0, 0, 0)
}

// OAuth sign-ins by their friendly names.
var oauthNames = map[string]string{
	"openai":         "ChatGPT",
	"anthropic":      "Claude",
	"github-copilot": "GitHub Copilot",
	"xai":            "Grok",
	"kimi-coding":    "Kimi Code",
	"meta":           "Meta",
	"openrouter":     "OpenRouter",
	"radius":         "Radius",
}

var oauthDescs = map[string]string{
	"openai":         "ChatGPT Plus / Pro / Team 订阅",
	"anthropic":      "Claude Pro / Max 订阅",
	"github-copilot": "Copilot 订阅，设备码登录",
	"xai":            "SuperGrok 或 X Premium 订阅",
	"kimi-coding":    "Kimi 编程订阅",
	"meta":           "Meta Muse 订阅",
	"openrouter":     "授权 OpenRouter 账户，按量计费",
	"radius":         "Radius 网关账户",
}

func oauthName(id string) string {
	if s := oauthNames[id]; s != "" {
		return s
	}
	return id
}

func oauthDesc(pr engine.Provider) string {
	if s := oauthDescs[pr.ID]; s != "" {
		return s
	}
	if pr.OAuth != nil {
		return pr.OAuth.Name
	}
	return pr.Name
}

// keyURLs are where providers hand out API keys.
var keyURLs = map[string]string{
	"openai":      "https://platform.openai.com/api-keys",
	"anthropic":   "https://console.anthropic.com/settings/keys",
	"google":      "https://aistudio.google.com/apikey",
	"deepseek":    "https://platform.deepseek.com/api_keys",
	"openrouter":  "https://openrouter.ai/keys",
	"groq":        "https://console.groq.com/keys",
	"mistral":     "https://console.mistral.ai/api-keys",
	"xai":         "https://console.x.ai",
	"huggingface": "https://huggingface.co/settings/tokens",
}

var featuredIDs = []string{"openai", "anthropic", "google", "deepseek", "openrouter", "xai", "moonshotai", "zai", "minimax", "mistral", "groq", "github-copilot"}

func featuredProvider(id string) bool { return slices.Contains(featuredIDs, id) }

var brandColors = map[string]string{
	"openai":         "#10A37F",
	"openai-codex":   "#10A37F",
	"anthropic":      "#D97757",
	"github-copilot": "#6E40C9",
	"google":         "#4285F4",
	"google-vertex":  "#4285F4",
	"deepseek":       "#4D6BFE",
	"openrouter":     "#6467F2",
	"xai":            "#3F3F46",
	"kimi-coding":    "#1F6FEB",
	"moonshotai":     "#1F6FEB",
	"moonshotai-cn":  "#1F6FEB",
	"meta":           "#0866FF",
	"mistral":        "#FA520F",
	"groq":           "#F55036",
	"zai":            "#2563EB",
	"zai-coding-cn":  "#2563EB",
	"minimax":        "#E5484D",
	"minimax-cn":     "#E5484D",
	"radius":         "#F97316",
	"amazon-bedrock": "#FF9900",
	"azure":          "#0078D4",
	"huggingface":    "#FFB000",
	"nvidia":         "#76B900",
}

// providerMark is a provider's initial on its color.
func providerMark(c *ui.Context, id, name string, size float32) {
	color := ui.Hex("#71717A")
	if hex := brandColors[id]; hex != "" {
		color = ui.Hex(hex)
	}
	initial := "?"
	for _, r := range name {
		initial = strings.ToUpper(string(r))
		break
	}
	ui.Box(c).Size(size, size).Shrink(0).Radius(size * 0.28).Background(color).Center().Children(func() {
		ui.Text(c, initial).Bold().FontSize(size * 0.45).TextColor(ui.RGB(255, 255, 255))
	})
}
