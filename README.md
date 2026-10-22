# WorkTony

Pi 的桌面 GUI 客户端，支持 macOS 与 Windows。

- 引擎：[PiG](https://pi-in-go.dev)（Pi 的 Go 实现）v0.5.1，以 Go SDK 方式嵌入进程内，不启动子进程
- 界面：[MyGo](https://mygo.egoist.dev) v0.6.2 Native UI，用 GPU 自绘（macOS 用 Metal，Windows 用 Direct3D 11），不依赖 WebView
- 数据：应用自己的设置和项目列表存放在 SQLite（[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)，纯 Go 版 SQLite）
- 纯 Go 实现，无 cgo，可以在任意平台交叉编译出 macOS / Windows / Linux 版本

## 功能（v0.1）

对话窗口
- 流式 Markdown 回复（代码高亮、表格、任务列表），可折叠的思考过程，工具调用卡片（状态、耗时、可展开输出）
- 多会话侧栏，`⌘N` 新建对话；Enter 发送、Shift+Enter 换行；生成中可以停止，也可以追加补充消息（排队发送）
- 侧栏按项目目录归类对话：添加文件夹作为项目，点击项目折叠或展开，悬停时可在该项目中新建对话；右键可重命名、打开文件夹或从侧栏移除（不影响文件夹）。选择过的工作目录会自动成为项目，不属于任何项目的对话列在“其他对话”
- 输入框内可以切换模型（支持搜索）、推理强度、工作目录和工具权限（只读 / 可修改）

设置窗口（`⌘,`，独立窗口）
- 账号登录：ChatGPT、Claude、GitHub Copilot、Grok、Kimi Code、OpenRouter 等 OAuth 登录，支持浏览器回调、设备码，以及手动粘贴授权码
- API Key：PiG 内置的 40 多家服务商，可搜索，自动识别环境变量中的密钥；多步配置（如 Cloudflare 的 Account ID）会在对话框中继续追问
- 自定义端点：Ollama / LM Studio / vLLM / 任意 OpenAI、Anthropic、Gemini 兼容网关，可从服务端拉取模型列表
- 通用：界面语言（跟随系统 / English / 简体中文）、默认模型、工具权限、工作目录、配置目录、数据文件位置

## 开发

需要 Go 1.27+，以及 Bun 或 Node.js 22.6+（`go tool mygo` 用它执行 `mygo.config.ts`）。

```sh
go tool mygo dev      # 开发模式，修改代码后自动重新构建并重启
go test ./...         # 引擎与界面测试（界面测试不需要窗口）
go tool mygo vet .    # go vet，外加 Native UI 构建值生命周期检查
```

开发模式（`mygo dev`）下：

- 应用显示为 WorkTony Debug：菜单栏、窗口标题和窗口里的 DEBUG 角标都会标明，Dock 图标带 DEBUG 徽标。磁盘上的包名和标识符由 MyGo 固定为 `WorkTony Dev.app` / `com.yizhiai.worktony.dev`（正式版为 `com.yizhiai.worktony`），与正式版互不干扰。
- 同时只运行一个实例：热重载时先退出旧进程再启动新进程；再次启动（包括另开一个 `go tool mygo dev`）会聚焦已有窗口后自行退出，那个 `mygo dev` 也随之结束。
- 设置和项目单独存放在 `WorkTony Debug/worktony.db`；PiG 的凭据和模型配置仍与正式版共用，需要隔离时设置 `PIG_HOME`。

界面文案在 `locales/` 的字典里：`en.json` 是源语言，`zh-Hans.json` 是简体中文翻译，key 统一用英文 camelCase，按区域加前缀（`chat…`、`settings…`、`common…` 等），带参数的用 ICU 占位符，如 `"loginTitle": "Sign in to {name}"`。代码通过 `locales.LoginTitle(name)` 这样的类型化函数取当前语言的文案。增改文案时两个文件一起改，`go tool mygo dev` 会自动重新生成 `locales/catalog_gen.go`（需要提交）；也可以手动运行：

```sh
go tool mygo generate     # 检查字典并生成 catalog_gen.go
go tool mygo i18n check   # 只检查：缺失的翻译、占位符不一致等
go tool mygo dev -lang en # 临时用英文运行，不改变保存的语言
```

图标由 `assets/icon-master.png` 生成：`resources/icon.png` 按 macOS 图标网格缩放（主体 824/1024）并加柔和阴影，`resources/icon-dev.png` 额外带 DEBUG 徽标，只用于开发版。更换母版后运行：

```sh
go run ./scripts/icons
```

导出界面截图（用于检查视觉效果）：

```sh
mkdir -p /tmp/shots && WORKTONY_SHOTS=/tmp/shots go test ./internal/app/
```

## 构建

```sh
go tool mygo build -platform darwin/arm64,windows/amd64
```

产物位于 `build/`：macOS 生成 `.app` 和 `.dmg`，Windows 生成 `.exe`（安装包需要 NSIS）。正式分发还需要配置签名，参见 MyGo 的 Building and distributing 文档。

## 配置与数据

| 内容 | 位置 |
|---|---|
| 凭据（OAuth token、API Key） | `~/.pig/agent/auth.json`（仅本人可读） |
| 自定义端点 | `~/.pig/agent/models.json` |
| 默认模型等设置 | `~/.pig/agent/settings.json` |
| 会话记录 | `~/.pig/agent/sessions/` |
| WorkTony 的设置（工作目录、工具权限）与项目列表 | 系统配置目录下的 `WorkTony/worktony.db`（SQLite） |

以上 PiG 文件与 `pig` 命令行共用；设置 `PIG_HOME` 环境变量可以改用其他目录。

`worktony.db` 有两张表：`settings`（键值配置）和 `projects`（项目路径、名称、折叠状态、添加与最近使用时间），表结构按 `PRAGMA user_version` 逐版迁移。旧版本的 `prefs.json` 会在首次启动时导入一次，原文件保留不动。数据库打不开时（例如被更新版本的 WorkTony 升级过），本次运行改用内存数据库，对话窗口会提示设置不会保存。

## 代码结构

```
main.go                 应用入口：窗口、菜单、主线程调度、打开数据库
internal/store/         SQLite 存储：设置、项目，表结构迁移
internal/engine/        PiG 封装，不依赖 UI
  engine.go             Services / Runtime、模型列表、默认模型
  auth.go               服务商、登录状态、OAuth 与 API Key 登录
  custom.go             自定义端点（models.json）
  chat.go               会话：把 PiG 事件转换成界面消息，合并流式更新
internal/app/           MyGo 界面
  chatview.go           对话窗口、输入框
  projects.go           侧栏的项目与对话分组、项目操作
  prefs.go              偏好的读写，导入旧版 prefs.json
  messages.go           消息、思考过程、工具卡片
  picker.go             模型与推理强度选择
  settings.go           设置窗口
  login.go              登录流程对话框
  theme.go              配色与图标
```

## 下一步

- 会话持久化后的历史列表与恢复（PiG 已经写入 JSONL，界面尚未读取）
- 扩展运行时切换到 Bun（PiG 的 `extensionsTypescriptRuntime: "bun"`，要求 Bun 1.4.2）
- 工具执行前的审批（目前只能通过“只读 / 可修改”整体控制）
- 图片输入、拖放文件、`@` 引用文件
