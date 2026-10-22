# WorkTony

Pi 的桌面 GUI 客户端，支持 macOS 与 Windows。

- 引擎：[PiG](https://pi-in-go.dev)（Pi 的 Go 实现）v0.5.1，以 Go SDK 方式嵌入进程内，不启动子进程
- 界面：[MyGo](https://mygo.egoist.dev) v0.6.2 Native UI，用 GPU 自绘（macOS 用 Metal，Windows 用 Direct3D 11），不依赖 WebView
- 纯 Go 实现，无 cgo，可以在任意平台交叉编译出 macOS / Windows / Linux 版本

## 功能（v0.1）

对话窗口
- 流式 Markdown 回复（代码高亮、表格、任务列表），可折叠的思考过程，工具调用卡片（状态、耗时、可展开输出）
- 多会话侧栏，`⌘N` 新建对话；Enter 发送、Shift+Enter 换行；生成中可以停止，也可以追加补充消息（排队发送）
- 输入框内可以切换模型（支持搜索）、推理强度、工作目录和工具权限（只读 / 可修改）

设置窗口（`⌘,`，独立窗口）
- 账号登录：ChatGPT、Claude、GitHub Copilot、Grok、Kimi Code、OpenRouter 等 OAuth 登录，支持浏览器回调、设备码，以及手动粘贴授权码
- API Key：PiG 内置的 40 多家服务商，可搜索，自动识别环境变量中的密钥；多步配置（如 Cloudflare 的 Account ID）会在对话框中继续追问
- 自定义端点：Ollama / LM Studio / vLLM / 任意 OpenAI、Anthropic、Gemini 兼容网关，可从服务端拉取模型列表
- 通用：默认模型、工具权限、工作目录、配置目录

## 开发

需要 Go 1.27+。

```sh
go tool mygo dev      # 开发模式，修改代码后自动重新构建并重启
go test ./...         # 引擎与界面测试（界面测试不需要窗口）
go tool mygo vet .    # go vet，外加 Native UI 构建值生命周期检查
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
| GUI 偏好（工作目录、工具权限） | 系统配置目录下的 `WorkTony/prefs.json` |

以上 PiG 文件与 `pig` 命令行共用；设置 `PIG_HOME` 环境变量可以改用其他目录。

## 代码结构

```
main.go                 应用入口：窗口、菜单、主线程调度
internal/engine/        PiG 封装，不依赖 UI
  engine.go             Services / Runtime、模型列表、默认模型
  auth.go               服务商、登录状态、OAuth 与 API Key 登录
  custom.go             自定义端点（models.json）
  chat.go               会话：把 PiG 事件转换成界面消息，合并流式更新
internal/app/           MyGo 界面
  chatview.go           对话窗口、输入框
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
