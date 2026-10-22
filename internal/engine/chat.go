package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/extension/builtin/codemode"

	"worktony/locales"
)

// Tool sets a chat can start with. The GUI asks no approval before a tool
// runs, so chats read by default and change files only when allowed.
var (
	ReadOnlyTools = []string{"read", "grep", "find", "ls"}
	FullTools     = []string{"read", "bash", "edit", "write", "grep", "find", "ls"}
)

// Role is who an item of a transcript is from.
type Role int

const (
	RoleUser Role = iota
	RoleAssistant
	// RoleNotice is a line from the app, as an error or a retry.
	RoleNotice
)

// PartKind is what a part of an assistant reply holds.
type PartKind int

const (
	PartText PartKind = iota
	PartThinking
	PartTool
)

// ToolStatus is how far a tool call has got.
type ToolStatus int

const (
	ToolPending ToolStatus = iota // the model is still writing its arguments
	ToolRunning
	ToolDone
	ToolFailed
)

// Tool is a tool call and its result.
type Tool struct {
	ID   string
	Name string
	// Summary is the argument that says what the call does, as a path or
	// a command.
	Summary  string
	Output   string
	Status   ToolStatus
	Duration time.Duration
}

// Part is a block of an assistant reply, in the order the model wrote it.
type Part struct {
	Kind PartKind
	Text string
	Tool *Tool
}

// Item is an entry of a transcript.
type Item struct {
	ID   int
	Role Role
	// Text is a user message or a notice.
	Text  string
	Parts []Part
	// Streaming is a reply still coming in.
	Streaming bool
	// Queued is a user message waiting for the reply before it.
	Queued  bool
	Error   string
	Aborted bool
	Model   string
	Tokens  int

	// confirmed is a user message the session has started.
	confirmed bool
}

// ChatOptions configures a new chat.
type ChatOptions struct {
	// Model is "provider/id".
	Model    string
	Thinking ai.ModelThinkingLevel
	// Tools are the tool names to start with; nil is ReadOnlyTools.
	Tools []string
	// Post runs fn on the goroutine that owns the transcript, the UI's main
	// thread, in order. Nil runs it at once under a lock, for tests.
	Post func(fn func())
}

// Chat is a conversation with the agent. Its transcript fields belong to
// the goroutine Post runs functions on: read them there.
type Chat struct {
	Items []*Item
	// Busy is true from a prompt until the agent settles.
	Busy bool
	// Version grows whenever the transcript changes.
	Version uint64

	sess    *coding.Session
	svcs    *coding.AgentSessionServices
	loader  *coding.DefaultResourceLoader
	plugins *pluginLink
	post    func(func())
	ctx     context.Context
	cancel  context.CancelFunc
	unsub   func()
	nextID  int
	model   Model
	// cwd and toolNames are what the session was started with: its
	// tools work in cwd for its whole life.
	cwd       string
	toolNames []string

	tools   map[string]*Tool
	current *Item // the reply streaming in

	pendingMu sync.Mutex
	pending   *[]Part // the newest parts of the streaming reply
	scheduled bool
}

// NewChat starts a chat.
func (e *Engine) NewChat(opts ChatOptions) (*Chat, error) {
	e.mu.Lock()
	svcs := e.svcs
	e.mu.Unlock()
	if opts.Model == "" {
		return nil, errors.New(locales.ChatNoModel())
	}
	model, err := coding.BuildModel(opts.Model, svcs)
	if err != nil {
		return nil, err
	}
	tools := opts.Tools
	if tools == nil {
		tools = ReadOnlyTools
	}
	plugins, subagents := newPluginLink(&e.plugins), newSubagents(svcs, tools)
	loader, err := newResourceLoader(svcs, e.chatExtensions(svcs, plugins, subagents, readOnly(tools)), false)
	if err != nil {
		plugins.stop()
		return nil, err
	}
	// codemode is always on: the model reaches MCP tools and sub-agents
	// through it.
	sessOpts := coding.SessionOptions{
		Model:                  model,
		ThinkingLevel:          opts.Thinking,
		InitialActiveToolNames: append(slices.Clone(tools), codemode.ToolName),
		ResourceLoader:         loader,
	}
	guard := guardReadOnly(&sessOpts, tools)
	sess, err := newSession(svcs, sessOpts)
	if err != nil {
		plugins.stop()
		loader.Close()
		return nil, err
	}
	if guard != nil {
		guard.sess.Store(sess)
	}
	subagents.parent.Store(sess)
	// The chat listens with Subscribe and reads nothing from Events: without
	// this, a reply of more events than its buffer holds stops streaming.
	sess.DiscardEvents()
	c := &Chat{sess: sess, svcs: svcs, loader: loader, plugins: plugins, post: opts.Post, tools: map[string]*Tool{}, model: modelOf(model, e.providerNames()),
		cwd: svcs.CWD(), toolNames: slices.Clone(tools)}
	if c.post == nil {
		var mu sync.Mutex
		c.post = func(fn func()) {
			mu.Lock()
			defer mu.Unlock()
			fn()
		}
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.unsub = sess.Subscribe(c.onEvent)
	// Binding runs the extensions' session_start, where the MCP extension
	// starts connecting its servers, and adds the tools the extensions
	// registered to the session's. A chat whose extensions fail still runs,
	// with the built-in tools, and says why.
	if err := sess.BindExtensions(c.ctx, coding.ExtensionBindings{Mode: extension.ModeRPC}); err != nil {
		c.post(func() { c.notice(locales.ChatExtensionsFailed(err.Error())) })
	}
	return c, nil
}

// Model is the chat's current model.
func (c *Chat) Model() Model { return c.model }

// SetModel switches the model for the next turns.
func (c *Chat) SetModel(m Model) error {
	model, err := coding.BuildModel(m.Ref, c.svcs)
	if err != nil {
		return err
	}
	if err := c.sess.SetModel(model); err != nil {
		return err
	}
	c.model = m
	return nil
}

// ThinkingLevels are the reasoning levels the current model takes.
func (c *Chat) ThinkingLevels() []ai.ModelThinkingLevel { return c.sess.AvailableThinkingLevels() }

// Thinking is the current reasoning level.
func (c *Chat) Thinking() ai.ModelThinkingLevel { return c.sess.ThinkingLevel() }

// SetThinking sets the reasoning level.
func (c *Chat) SetThinking(level ai.ModelThinkingLevel) error { return c.sess.SetThinkingLevel(level) }

// Tools are the names of the active tools.
func (c *Chat) Tools() []string { return c.sess.ActiveToolNames() }

// CWD is the directory the chat's tools work in, which it keeps when the
// engine moves new chats elsewhere.
func (c *Chat) CWD() string { return c.cwd }

// FullTools reports whether the chat was started with tools that change
// files or run commands.
func (c *Chat) FullTools() bool { return !readOnly(c.toolNames) }

// Send sends text, or, while a reply comes in, queues it to send once the
// turns before it end. Call it on the transcript's goroutine.
func (c *Chat) Send(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	it := &Item{Role: RoleUser, Text: text, Queued: c.Busy}
	c.add(it)
	if !c.Busy {
		c.prompt(it)
	}
}

// prompt runs a turn for the user message it. The function it posts when
// the turn ends clears Busy and starts the next queued message together,
// on the transcript's goroutine, so a message queued until then is never
// left behind, and the queue goes in order.
func (c *Chat) prompt(it *Item) {
	c.Busy = true
	it.Queued = false
	text := it.Text
	go func() {
		err := c.sess.Prompt(c.ctx, text)
		c.post(func() {
			c.Busy = false
			c.finishCurrent()
			closed := c.ctx.Err() != nil
			if err != nil && !errors.Is(err, context.Canceled) && !closed {
				c.notice(err.Error())
			}
			c.Version++
			if !closed {
				c.sendQueued()
			}
		})
	}()
}

// sendQueued starts the first queued message, moved to the end, where its
// reply comes.
func (c *Chat) sendQueued() {
	for _, it := range c.Items {
		if it.Role == RoleUser && it.Queued {
			c.moveToEnd(it)
			c.prompt(it)
			return
		}
	}
}

// Queued counts the messages waiting to be sent.
func (c *Chat) Queued() int {
	n := 0
	for _, it := range c.Items {
		if it.Role == RoleUser && it.Queued {
			n++
		}
	}
	return n
}

// Abort stops the running turn. Stopping drops the messages queued behind
// it from the transcript; it returns their text, joined, for the composer
// to give back.
func (c *Chat) Abort() string {
	var queued []string
	kept := make([]*Item, 0, len(c.Items))
	for _, it := range c.Items {
		if it.Role == RoleUser && it.Queued {
			queued = append(queued, it.Text)
			continue
		}
		kept = append(kept, it)
	}
	if len(queued) > 0 {
		c.Items = kept
		c.Version++
	}
	go func() { _ = c.sess.Abort(context.Background()) }()
	return strings.Join(queued, "\n\n")
}

// Close ends the chat. Its session file stays.
func (c *Chat) Close() {
	c.cancel()
	c.unsub()
	c.plugins.stop()
	_ = c.sess.Close()
	c.loader.Close()
}

// newSession starts a session of svcs whose extensions are those of its
// loader, run by a runner of its own. PiG's Runtime gives the sessions it
// starts one runner, for one session at a time as in the CLI.
func newSession(svcs *coding.AgentSessionServices, opts coding.SessionOptions) (*coding.Session, error) {
	opts.Transport = ai.Transport(svcs.Settings().Transport)
	return coding.NewSession(svcs, opts)
}

// newResourceLoader loads what a session reads besides its messages:
// skills, prompt templates, context files such as AGENTS.md, and
// extensions, the inline ones given and, unless noExtensions, those of the
// settings, as the loader PiG makes for a session does, but not the TUI's
// themes. The caller owns it: PiG never closes the loader it makes for a
// session, so the Extension Host of its extensions would outlive the
// session, and the Services' loader, which PiG closes, is one for all
// sessions, whose extensions act on the session started last.
func newResourceLoader(svcs *coding.AgentSessionServices, inline []extension.InlineExtension, noExtensions bool) (*coding.DefaultResourceLoader, error) {
	loader := coding.NewDefaultResourceLoader(coding.DefaultResourceLoaderOptions{
		CWD:                svcs.CWD(),
		AgentDir:           svcs.AgentDir(),
		SettingsManager:    svcs.SettingsManager(),
		NoThemes:           true,
		NoExtensions:       noExtensions,
		ExtensionFactories: inline,
	})
	if err := loader.Reload(); err != nil {
		loader.Close()
		return nil, err
	}
	return loader, nil
}

// Title is the start of the first user message.
func (c *Chat) Title() string {
	for _, it := range c.Items {
		if it.Role == RoleUser {
			t := strings.Join(strings.Fields(it.Text), " ")
			if r := []rune(t); len(r) > 28 {
				t = string(r[:28]) + "…"
			}
			return t
		}
	}
	return ""
}

func (c *Chat) add(it *Item) {
	c.nextID++
	it.ID = c.nextID
	c.Items = append(c.Items, it)
	c.Version++
}

func (c *Chat) notice(text string) { c.add(&Item{Role: RoleNotice, Text: text}) }

// onEvent runs on PiG's goroutine, synchronously with the session: it
// copies what the transcript needs out of the event and posts it.
func (c *Chat) onEvent(ev agent.AgentEvent) {
	switch e := ev.(type) {
	case agent.MessageStartEvent:
		if u := e.Message.User; u != nil {
			text := userText(u.Content)
			c.post(func() { c.userStarted(text) })
		} else if a := e.Message.Assistant; a != nil {
			parts := partsOf(a.Content)
			c.post(func() { c.assistantStarted(parts) })
		}
	case agent.MessageUpdateEvent:
		if a := e.Message.Assistant; a != nil {
			c.queueParts(partsOf(a.Content))
		}
	case agent.MessageEndEvent:
		if a := e.Message.Assistant; a != nil {
			parts, stop, msg := partsOf(a.Content), a.StopReason, a.ErrorMessage
			tokens := 0
			if a.Usage != nil {
				tokens = a.Usage.Output
			}
			c.post(func() { c.assistantEnded(parts, stop, msg, tokens) })
		}
	case agent.ToolExecutionStartEvent:
		id, name, summary := e.ToolCallID, e.ToolName, summarize(e.ToolName, e.Args)
		c.post(func() {
			t := c.tool(id, name)
			t.Status = ToolRunning
			if summary != "" {
				t.Summary = summary
			}
			c.Version++
		})
	case agent.ToolExecutionUpdateEvent:
		id, out := e.ToolCallID, e.PartialResult.Text()
		c.post(func() {
			c.tool(id, "").Output = out
			c.Version++
		})
	case agent.ToolExecutionEndEvent:
		id, out, failed := e.ToolCallID, e.Result.Text(), e.IsError
		var took time.Duration
		if e.DurationMs != nil {
			took = time.Duration(*e.DurationMs) * time.Millisecond
		}
		c.post(func() {
			t := c.tool(id, "")
			t.Output, t.Duration, t.Status = out, took, ToolDone
			if failed {
				t.Status = ToolFailed
			}
			c.Version++
		})
	case agent.AutoRetryStartEvent:
		msg := locales.ChatRetrying(float64(e.DelayMs)/1000, e.Attempt, e.MaxAttempts, e.ErrorMessage)
		c.post(func() { c.notice(msg) })
	case agent.CompactionStartEvent:
		c.post(func() { c.notice(locales.ChatCompacting()) })
	}
}

// queueParts keeps the newest parts of the streaming reply and posts one
// function for however many updates come before it runs, so a fast stream
// costs a frame, not a frame per token.
func (c *Chat) queueParts(parts []Part) {
	c.pendingMu.Lock()
	c.pending = &parts
	if c.scheduled {
		c.pendingMu.Unlock()
		return
	}
	c.scheduled = true
	c.pendingMu.Unlock()
	c.post(c.flushParts)
}

func (c *Chat) flushParts() {
	c.pendingMu.Lock()
	parts := c.pending
	c.pending, c.scheduled = nil, false
	c.pendingMu.Unlock()
	if parts != nil && c.current != nil {
		c.current.Parts = c.resolve(*parts)
		c.Version++
	}
}

// userStarted confirms the message prompt sent: the newest user message not
// queued. Another, as one an extension sends, is added.
func (c *Chat) userStarted(text string) {
	for i := len(c.Items) - 1; i >= 0; i-- {
		it := c.Items[i]
		if it.Role != RoleUser || it.Queued {
			continue
		}
		if !it.confirmed && it.Text == text {
			it.confirmed = true
			return
		}
		break
	}
	c.add(&Item{Role: RoleUser, Text: text, confirmed: true})
}

func (c *Chat) moveToEnd(it *Item) {
	for i, other := range c.Items {
		if other == it {
			c.Items = append(append(c.Items[:i:i], c.Items[i+1:]...), it)
			return
		}
	}
}

func (c *Chat) assistantStarted(parts []Part) {
	c.finishCurrent()
	it := &Item{Role: RoleAssistant, Streaming: true, Model: c.model.Name, Parts: c.resolve(parts)}
	c.add(it)
	c.current = it
}

func (c *Chat) assistantEnded(parts []Part, stop ai.StopReason, msg string, tokens int) {
	it := c.current
	if it == nil {
		it = &Item{Role: RoleAssistant, Model: c.model.Name}
		c.add(it)
	}
	c.pendingMu.Lock()
	c.pending = nil
	c.pendingMu.Unlock()
	it.Parts = c.resolve(parts)
	it.Streaming = false
	it.Tokens = tokens
	switch stop {
	case ai.StopReasonError:
		it.Error = msg
		if it.Error == "" {
			it.Error = locales.ChatRequestFailed()
		}
	case ai.StopReasonAborted:
		it.Aborted = true
	}
	c.current = nil
	c.Version++
}

func (c *Chat) finishCurrent() {
	if c.current != nil {
		c.current.Streaming = false
		c.current = nil
	}
}

// resolve points the tool parts at the transcript's tool states, which the
// execution events update.
func (c *Chat) resolve(parts []Part) []Part {
	for i := range parts {
		if p := &parts[i]; p.Kind == PartTool {
			t := c.tool(p.Tool.ID, p.Tool.Name)
			if p.Tool.Summary != "" && t.Status == ToolPending {
				t.Summary = p.Tool.Summary
			}
			p.Tool = t
		}
	}
	return parts
}

func (c *Chat) tool(id, name string) *Tool {
	t := c.tools[id]
	if t == nil {
		t = &Tool{ID: id}
		c.tools[id] = t
	}
	if t.Name == "" {
		t.Name = name
	}
	return t
}

func userText(content ai.UserContent) string {
	switch v := content.(type) {
	case ai.UserText:
		return string(v)
	case ai.UserContentBlocks:
		var sb strings.Builder
		for _, b := range v {
			if t, ok := b.(ai.TextContent); ok {
				sb.WriteString(t.Text)
			}
		}
		return sb.String()
	}
	return ""
}

// partsOf copies the blocks of an assistant message, joining adjacent text.
func partsOf(blocks []ai.AssistantContentBlock) []Part {
	parts := make([]Part, 0, len(blocks))
	for _, b := range blocks {
		switch v := b.(type) {
		case ai.TextContent:
			if n := len(parts); n > 0 && parts[n-1].Kind == PartText {
				parts[n-1].Text += v.Text
			} else if v.Text != "" {
				parts = append(parts, Part{Kind: PartText, Text: v.Text})
			}
		case ai.ThinkingContent:
			if v.Thinking != "" && !v.Redacted {
				parts = append(parts, Part{Kind: PartThinking, Text: v.Thinking})
			}
		case ai.ToolCall:
			parts = append(parts, Part{Kind: PartTool, Tool: &Tool{ID: v.ID, Name: v.Name, Summary: summarizeObject(v.Name, v.Arguments)}})
		}
	}
	return parts
}
