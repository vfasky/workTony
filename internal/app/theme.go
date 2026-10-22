package app

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// palette is the app's colors beyond the widgets' theme.
type palette struct {
	sidebar    ui.Color // the sidebar where no material shows
	pane       ui.Color // the chat and settings panes
	card       ui.Color // composer, cards and bubbles
	userBubble ui.Color
	hairline   ui.Color
	subtle     ui.Color // hover of plain rows
	accentSoft ui.Color
	codeBg     ui.Color
	dangerSoft ui.Color
	success    ui.Color
	logoFrom   ui.Color
	logoTo     ui.Color
}

func lightPalette() palette {
	return palette{
		sidebar:    ui.Hex("#F2F1EE"),
		pane:       ui.Hex("#FBFAF8"),
		card:       ui.Hex("#FFFFFF"),
		userBubble: ui.Hex("#EFEDE8"),
		hairline:   ui.RGBA(20, 20, 30, 0.09),
		subtle:     ui.RGBA(20, 20, 30, 0.05),
		accentSoft: ui.RGBA(91, 91, 214, 0.11),
		codeBg:     ui.Hex("#F4F3F0"),
		dangerSoft: ui.RGBA(220, 38, 38, 0.08),
		success:    ui.Hex("#16A34A"),
		logoFrom:   ui.Hex("#6366F1"),
		logoTo:     ui.Hex("#A855F7"),
	}
}

func darkPalette() palette {
	return palette{
		sidebar:    ui.Hex("#18181B"),
		pane:       ui.Hex("#1C1C1F"),
		card:       ui.Hex("#26262A"),
		userBubble: ui.Hex("#2E2E33"),
		hairline:   ui.RGBA(255, 255, 255, 0.08),
		subtle:     ui.RGBA(255, 255, 255, 0.05),
		accentSoft: ui.RGBA(139, 141, 248, 0.16),
		codeBg:     ui.Hex("#232327"),
		dangerSoft: ui.RGBA(248, 113, 113, 0.12),
		success:    ui.Hex("#4ADE80"),
		logoFrom:   ui.Hex("#818CF8"),
		logoTo:     ui.Hex("#C084FC"),
	}
}

// applyTheme sets the window's theme from the system's appearance and
// returns the palette that goes with it.
func applyTheme(c *ui.Context) palette {
	base := c.Theme()
	t := *base
	p := lightPalette()
	if t.Dark {
		p = darkPalette()
		t.Accent, t.AccentHover, t.AccentPressed = ui.Hex("#8B8DF8"), ui.Hex("#9EA0FA"), ui.Hex("#7577F0")
		t.AccentText = ui.Hex("#0F0F1A")
		t.Text, t.TextMuted = ui.Hex("#ECECEE"), ui.Hex("#9B9BA4")
		t.Surface, t.SurfaceHover, t.SurfacePressed = ui.Hex("#2A2A2F"), ui.Hex("#323238"), ui.Hex("#3A3A41")
	} else {
		t.Accent, t.AccentHover, t.AccentPressed = ui.Hex("#5B5BD6"), ui.Hex("#5151CD"), ui.Hex("#4747C2")
		t.AccentText = ui.Hex("#FFFFFF")
		t.Text, t.TextMuted = ui.Hex("#1F1E1C"), ui.Hex("#76746E")
		t.Surface, t.SurfaceHover, t.SurfacePressed = ui.Hex("#FFFFFF"), ui.Hex("#F5F4F1"), ui.Hex("#ECEBE7")
	}
	t.Background = p.pane
	t.Border = p.hairline
	t.Radius = 8
	t.FontSize = 14
	t.Focus = t.Accent.Alpha(0.45)
	c.SetTheme(&t)
	return p
}

//go:embed icons/*.svg
var iconFiles embed.FS

// icon parses icons/<name>.svg.
func icon(name string) *ui.SVG {
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

// Icons, from Lucide (ISC license).
var (
	icPlus     = icon("plus")
	icSettings = icon("settings")
	icArrowUp  = icon("arrow-up")
	icStop     = icon("stop")
	icFolder   = icon("folder")
	icFolderOp = icon("folder-open")
	icFolderAd = icon("folder-plus")
	icShield   = icon("shield-check")
	icZap      = icon("zap")
	icChevDown = icon("chevron-down")
	icChevR    = icon("chevron-right")
	icCheck    = icon("check")
	icX        = icon("x")
	icCopy     = icon("copy")
	icExternal = icon("external-link")
	icTerminal = icon("terminal")
	icFile     = icon("file-text")
	icSearch   = icon("search")
	icList     = icon("list")
	icPencil   = icon("pencil")
	icSparkles = icon("sparkles")
	icKey      = icon("key-round")
	icUser     = icon("user-round")
	icPlug     = icon("plug")
	icSliders  = icon("sliders-horizontal")
	icTrash    = icon("trash")
	icRefresh  = icon("refresh-cw")
	icAlert    = icon("circle-alert")
	icBulb     = icon("lightbulb")
	icMessage  = icon("message-square")
	icSidebar  = icon("panel-left")
	icLogOut   = icon("log-out")
	icGlobe    = icon("globe")
	icBranch   = icon("git-branch")
	icBot      = icon("bot")
)

// toolIcon is the icon of a built-in tool.
func toolIcon(name string) *ui.SVG {
	switch name {
	case "bash":
		return icTerminal
	case "read":
		return icFile
	case "edit", "write":
		return icPencil
	case "grep", "find":
		return icSearch
	case "ls":
		return icList
	case "subagent":
		return icBot
	}
	return icSparkles
}
