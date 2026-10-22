package app

import "github.com/egoist/mygo/ui"

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

func icon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

// Icons, from Lucide (ISC license).
var (
	icPlus     = icon(`<path d="M5 12h14"/><path d="M12 5v14"/>`)
	icSettings = icon(`<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>`)
	icArrowUp  = icon(`<path d="m5 12 7-7 7 7"/><path d="M12 19V5"/>`)
	icStop     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="2.5" fill="currentColor"/></svg>`))
	icFolder   = icon(`<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)
	icShield   = icon(`<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/><path d="m9 12 2 2 4-4"/>`)
	icZap      = icon(`<path d="M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z"/>`)
	icChevDown = icon(`<path d="m6 9 6 6 6-6"/>`)
	icChevR    = icon(`<path d="m9 18 6-6-6-6"/>`)
	icCheck    = icon(`<path d="M20 6 9 17l-5-5"/>`)
	icX        = icon(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	icCopy     = icon(`<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>`)
	icExternal = icon(`<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>`)
	icTerminal = icon(`<path d="m4 17 6-6-6-6"/><path d="M12 19h8"/>`)
	icFile     = icon(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>`)
	icSearch   = icon(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`)
	icList     = icon(`<path d="M3 12h.01"/><path d="M3 18h.01"/><path d="M3 6h.01"/><path d="M8 12h13"/><path d="M8 18h13"/><path d="M8 6h13"/>`)
	icPencil   = icon(`<path d="M21.17 6.81a1 1 0 0 0-3.99-3.99L3.84 16.17a2 2 0 0 0-.5.83l-1.32 4.35a.5.5 0 0 0 .62.62l4.35-1.32a2 2 0 0 0 .83-.5z"/><path d="m15 5 4 4"/>`)
	icSparkles = icon(`<path d="M9.94 15.5A2 2 0 0 0 8.5 14.06l-6.13-1.58a.5.5 0 0 1 0-.96L8.5 9.94A2 2 0 0 0 9.94 8.5l1.58-6.13a.5.5 0 0 1 .96 0L14.06 8.5A2 2 0 0 0 15.5 9.94l6.13 1.58a.5.5 0 0 1 0 .96L15.5 14.06a2 2 0 0 0-1.44 1.44l-1.58 6.13a.5.5 0 0 1-.96 0z"/>`)
	icKey      = icon(`<path d="M2.59 18.41A2 2 0 0 0 2 19.83V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.17a2 2 0 0 0 1.42-.59l.81-.81a6.5 6.5 0 1 0-4.24-4.24z"/><circle cx="16.5" cy="7.5" r=".5" fill="currentColor"/>`)
	icUser     = icon(`<circle cx="12" cy="8" r="5"/><path d="M20 21a8 8 0 0 0-16 0"/>`)
	icPlug     = icon(`<path d="M12 22v-5"/><path d="M9 8V2"/><path d="M15 8V2"/><path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z"/>`)
	icSliders  = icon(`<path d="M10 5H3"/><path d="M12 19H3"/><path d="M14 3v4"/><path d="M16 17v4"/><path d="M21 12h-9"/><path d="M21 19h-5"/><path d="M21 5h-7"/><path d="M8 10v4"/><path d="M8 12H3"/>`)
	icTrash    = icon(`<path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/>`)
	icRefresh  = icon(`<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>`)
	icAlert    = icon(`<circle cx="12" cy="12" r="10"/><path d="M12 8v4"/><path d="M12 16h.01"/>`)
	icBulb     = icon(`<path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5"/><path d="M9 18h6"/><path d="M10 22h4"/>`)
	icMessage  = icon(`<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>`)
	icSidebar  = icon(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/>`)
	icLogOut   = icon(`<path d="m16 17 5-5-5-5"/><path d="M21 12H9"/><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>`)
	icGlobe    = icon(`<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>`)
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
	}
	return icSparkles
}
