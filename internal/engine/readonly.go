package engine

import (
	"context"
	"encoding/json"
	"slices"
	"sync/atomic"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/extension/builtin/codemode"

	"worktony/locales"
)

// A read-only chat promises to read files and change nothing. Its built-in
// tools only read, but extensions add tools of their own: the MCP servers
// of mcp.json and the plugins, at any time while the chat runs, and the
// extensions of the settings. A tool that may change something stays out
// of a read-only chat twice over:
//
//   - the MCP extension registers only the MCP tools that declare
//     themselves read-only (readOnlyHint), so the model sees no other;
//   - every call, the model's own and a codemode script's, passes a guard
//     that runs only the tools known to read, and refuses the others,
//     whichever extension registered them and whenever.

// Names of the extensions every chat loads (chatExtensions), which name
// the source of the tools they register.
const (
	codemodeExtension  = "codemode"
	mcpExtension       = "mcp"
	pluginsExtension   = "worktony-plugins"
	subagentsExtension = "worktony-subagents"
)

// readOnly reports whether tools only read.
func readOnly(tools []string) bool {
	return !slices.ContainsFunc(tools, func(name string) bool { return !slices.Contains(ReadOnlyTools, name) })
}

// declaresReadOnly reports whether a tool's annotations say it changes
// nothing, as an MCP server's readOnlyHint does.
func declaresReadOnly(a *extension.ToolAnnotations) bool {
	return a != nil && a.ReadOnlyHint != nil && *a.ReadOnlyHint
}

// readingTool reports whether a read-only chat runs the tool: a built-in
// tool that reads; codemode, whose scripts call the other tools past the
// guard; the sub-agents, which have the chat's tools alone; or a tool that
// declares itself read-only. The source tells the app's tools from those
// another extension registers under their names.
func readingTool(info extension.ToolInfo) bool {
	switch info.SourceInfo.Path {
	case "builtin:" + info.Name:
		return slices.Contains(ReadOnlyTools, info.Name)
	case inlineSource(codemodeExtension):
		return info.Name == codemode.ToolName
	case inlineSource(subagentsExtension):
		return info.Name == subagentToolName
	}
	return declaresReadOnly(info.Annotations)
}

// inlineSource is the source path of the tools an inline extension
// registers.
func inlineSource(name string) string { return "<inline:" + name + ">" }

// readOnlyGuard refuses the calls of a read-only session to tools that may
// change something. Its hook runs before every tool call of the session,
// the nested calls of codemode scripts too.
type readOnlyGuard struct {
	// sess is the session guarded, set once it starts; until then every
	// call is refused.
	sess atomic.Pointer[coding.Session]
}

func (g *readOnlyGuard) beforeToolCall(_ context.Context, _, name string, _ json.RawMessage) agent.ToolCallHookResult {
	if sess := g.sess.Load(); sess != nil {
		for _, info := range sess.GetAllTools() {
			if info.Name == name && readingTool(info) {
				return agent.ToolCallHookResult{}
			}
		}
	}
	return agent.ToolCallHookResult{Block: true, Reason: locales.ChatToolReadOnly(name)}
}

// guardReadOnly makes a session of opts, when its tools only read, run no
// tool that may change something. Store the session in the guard it
// returns, nil when the session is not read-only.
func guardReadOnly(opts *coding.SessionOptions, tools []string) *readOnlyGuard {
	if !readOnly(tools) {
		return nil
	}
	g := &readOnlyGuard{}
	opts.BeforeToolCall = append(opts.BeforeToolCall, g.beforeToolCall)
	return g
}

// readOnlyMCP is the MCP extension of a read-only chat: of the tools of its
// servers, it registers those that declare themselves read-only.
func readOnlyMCP(factory extension.ExtensionFactory) extension.ExtensionFactory {
	return func(pi extension.API) error { return factory(readOnlyMCPAPI{pi}) }
}

// readOnlyMCPAPI is the extension API the MCP extension of a read-only chat
// registers its tools on.
type readOnlyMCPAPI struct{ extension.API }

// keeps reports whether the MCP extension of a read-only chat registers a
// tool: one of its own, or an MCP tool that declares itself read-only.
func (readOnlyMCPAPI) keeps(definition extension.ToolDefinition) bool {
	return !extension.IsMcpToolName(definition.Name) || declaresReadOnly(definition.Annotations)
}

func (a readOnlyMCPAPI) RegisterTool(definition extension.ToolDefinition) {
	if a.keeps(definition) {
		a.API.RegisterTool(definition)
	}
}

// RegisterTools registers a server's tools as one change, as the runner the
// MCP extension looks for does, when the API underneath can.
func (a readOnlyMCPAPI) RegisterTools(definitions []extension.ToolDefinition) []error {
	var kept []extension.ToolDefinition
	var at []int
	for i, definition := range definitions {
		if a.keeps(definition) {
			kept, at = append(kept, definition), append(at, i)
		}
	}
	errs := make([]error, len(definitions))
	batch, ok := a.API.(interface {
		RegisterTools([]extension.ToolDefinition) []error
	})
	if !ok {
		for _, definition := range kept {
			a.API.RegisterTool(definition)
		}
		return errs
	}
	for i, err := range batch.RegisterTools(kept) {
		errs[at[i]] = err
	}
	return errs
}
