package engine

import (
	"encoding/json"
	"strings"

	"github.com/MichaelKinsy/PiG/ai"
)

// summaryArgs is the argument that says what a call of each built-in tool
// does.
var summaryArgs = map[string][]string{
	"read":  {"path", "file_path"},
	"write": {"path", "file_path"},
	"edit":  {"path", "file_path"},
	"ls":    {"path"},
	"bash":  {"command"},
	"grep":  {"pattern"},
	"find":  {"pattern", "path"},
}

func summarize(tool string, raw json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return ""
	}
	return summarizeMap(tool, args)
}

func summarizeObject(tool string, args ai.JsonObject) string {
	return summarizeMap(tool, args)
}

func summarizeMap(tool string, args map[string]any) string {
	keys := summaryArgs[tool]
	for _, k := range keys {
		if s, ok := args[k].(string); ok && s != "" {
			return oneLine(s)
		}
	}
	// Another tool: its first string argument.
	for _, v := range args {
		if s, ok := v.(string); ok && s != "" {
			return oneLine(s)
		}
	}
	return ""
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}
