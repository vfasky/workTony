package engine

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
)

const (
	subagentToolName = "subagent"
	// maxSubagents is how many sub-agents of a chat run at once; the calls
	// beyond wait their turn.
	maxSubagents = 4
)

var subagentParameters = json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": {
      "type": "string",
      "description": "The complete task. The sub-agent sees neither this conversation nor the other sub-agents, so give it every path, fact and constraint it needs, and say what its reply should contain."
    }
  },
  "required": ["task"],
  "additionalProperties": false
}`)

var subagentGuidelines = []string{
	"Use sub-agents for work that splits into independent parts, as reading or changing several areas of a project: run them at once with `await Promise.all(tasks.map(task => tools.subagent({ task })))`, then combine their replies.",
	"A sub-agent has this chat's model, working directory and tools, but neither codemode, MCP tools nor sub-agents of its own.",
	"Its reply is its final message only: ask for the findings, paths and results to come back in it.",
}

// subagents is the `subagent` tool of a chat. The tool runs a task in a new
// session with the chat's model, thinking level, working directory and
// tools, and resolves to the session's last reply. Its exposure is
// codemode: the model reaches it from codemode scripts, which run several
// at once with Promise.all. The sessions are kept in memory only, out of
// the session list.
type subagents struct {
	svcs  *coding.AgentSessionServices
	tools []string
	slots chan struct{}
	// parent is the chat's session, from which a call takes the model and
	// thinking level of the moment. It is set once the session starts.
	parent atomic.Pointer[coding.Session]
}

func newSubagents(svcs *coding.AgentSessionServices, tools []string) *subagents {
	return &subagents{svcs: svcs, tools: slices.Clone(tools), slots: make(chan struct{}, maxSubagents)}
}

func (s *subagents) extension(pi extension.API) error {
	pi.RegisterTool(extension.ToolDefinition{
		Name:             subagentToolName,
		Label:            "Sub-agent",
		Description:      "Run a sub-agent: a new agent works on the task on its own and resolves to its final reply.",
		PromptGuidelines: subagentGuidelines,
		Parameters:       subagentParameters,
		Exposure:         extension.ToolExposureCodemode,
		Execute:          s.execute,
	})
	return nil
}

func (s *subagents) execute(ctx context.Context, _ string, params json.RawMessage, _ extension.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
	var args struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return agent.AgentToolResult{}, err
	}
	if strings.TrimSpace(args.Task) == "" {
		return agent.AgentToolResult{}, errors.New("task must not be empty")
	}
	parent := s.parent.Load()
	if parent == nil {
		return agent.AgentToolResult{}, errors.New("the chat has not started")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return agent.AgentToolResult{}, ctx.Err()
	}
	reply, usage, err := s.run(ctx, parent, args.Task)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: reply}}, Usage: usage}, nil
}

// run runs task in a new session and returns its last reply. Cancelling ctx
// aborts the session.
func (s *subagents) run(ctx context.Context, parent *coding.Session, task string) (string, *ai.Usage, error) {
	loader, err := newResourceLoader(s.svcs, nil, true)
	if err != nil {
		return "", nil, err
	}
	defer loader.Close()
	log, err := coding.NewInMemorySessionManager(s.svcs.CWD())
	if err != nil {
		return "", nil, err
	}
	opts := coding.SessionOptions{
		Model:                  parent.Model(),
		ThinkingLevel:          parent.ThinkingLevel(),
		InitialActiveToolNames: s.tools,
		ResourceLoader:         loader,
		SessionManager:         log,
	}
	// The sub-agent of a read-only chat reads only, as the chat does.
	guard := guardReadOnly(&opts, s.tools)
	sess, err := newSession(s.svcs, opts)
	if err != nil {
		return "", nil, err
	}
	if guard != nil {
		guard.sess.Store(sess)
	}
	defer sess.Close()
	sess.DiscardEvents()
	stop := context.AfterFunc(ctx, func() { _ = sess.Abort(context.Background()) })
	defer stop()
	if err := sess.Prompt(ctx, task); err != nil {
		return "", nil, err
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	stats := sess.GetSessionStats()
	usage := &ai.Usage{
		Input: stats.Tokens.Input, Output: stats.Tokens.Output, CacheRead: stats.Tokens.CacheRead, CacheWrite: stats.Tokens.CacheWrite,
		TotalTokens: stats.Tokens.Total, Cost: ai.UsageCost{Total: stats.Cost},
	}
	if reply := sess.LastAssistantText(); reply != nil {
		return *reply, usage, nil
	}
	return "", usage, errors.New(subagentFailure(sess.Messages()))
}

// subagentFailure says why a sub-agent ended without a reply: the error of
// its last message, when it has one.
func subagentFailure(messages []agent.AgentMessage) string {
	for _, msg := range slices.Backward(messages) {
		if a := msg.Assistant; a != nil {
			if a.ErrorMessage != "" {
				return "the sub-agent failed: " + a.ErrorMessage
			}
			break
		}
	}
	return "the sub-agent ended without a reply"
}
