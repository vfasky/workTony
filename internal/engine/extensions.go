package engine

import (
	"os"
	"path/filepath"

	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/extension/builtin/codemode"
	"github.com/MichaelKinsy/PiG/coding/mcpext"
)

// chatExtensions are the extensions every chat loads besides those of the
// settings:
//
//   - codemode, which runs the model's JavaScript in a sandbox, where it
//     calls the other tools, MCP tools and sub-agents among them;
//   - PiG's MCP extension, headless, which connects the servers of mcp.json
//     and the plugins, and registers their tools;
//   - the chat's link to the plugins, which registers them with it;
//   - the sub-agents.
//
// codemode and MCP are what the pig CLI loads as builtin:codemode and
// builtin:mcp; an extension of the settings that registers a tool of the
// same name replaces them. The MCP extension of a read-only chat registers
// only the MCP tools that declare themselves read-only (readOnlyMCP).
func (e *Engine) chatExtensions(svcs *coding.AgentSessionServices, plugins *pluginLink, subagents *subagents, readOnly bool) []extension.InlineExtension {
	mcp := mcpext.CreateMcpExtension(mcpext.Options{
		AgentDir:      svcs.AgentDir(),
		ConfigDirName: projectConfigDir(),
		OpenURL:       e.openURL,
		ClientName:    "WorkTony",
	})
	if readOnly {
		mcp = readOnlyMCP(mcp)
	}
	return []extension.InlineExtension{
		extension.NamedInlineExtension{Name: codemodeExtension, Hidden: true, Replaceable: true,
			Factory: codemode.CreateCodemodeExtension(codemode.Options{CacheDir: wazeroCacheDir()})},
		extension.NamedInlineExtension{Name: mcpExtension, Hidden: true, Replaceable: true, Factory: mcp},
		extension.NamedInlineExtension{Name: pluginsExtension, Hidden: true, Factory: plugins.extension},
		extension.NamedInlineExtension{Name: subagentsExtension, Hidden: true, Factory: subagents.extension},
	}
}

// wazeroCacheDir is where codemode keeps its compiled sandbox, the pig
// CLI's directory, so the two share it: a chat's first script then starts
// in some 20 ms rather than 0.4 s.
func wazeroCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "pig", "wazero")
	}
	return ""
}

// projectConfigDir is the directory of a project's mcp.json, as PiG chooses
// it: .pig, or Pi's .pi when PIG_USE_PI_DIRS=1 shares Pi's.
func projectConfigDir() string {
	if os.Getenv("PIG_USE_PI_DIRS") == "1" {
		return ".pi"
	}
	return ".pig"
}
