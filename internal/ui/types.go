package ui

import (
	"strconv"
	"strings"

	"tahr/internal/core/buffer"
)

// CellStyle represents the rendering attributes of a single screen cell.
type CellStyle struct {
	FgColor   uint32 // 24-bit TrueColor RGB (0xRRGGBB)
	BgColor   uint32 // 24-bit TrueColor RGB
	Bold      bool
	Italic    bool
	Underline bool
}

// Cell represents a single character on the screen.
type Cell struct {
	Char  rune
	Style CellStyle
}

// RenderLine represents a rendered line with its line number and gutter metadata.
type RenderLine struct {
	LineNumber int
	Cells      []Cell
	GutterIcon rune
	GutterFg   uint32
}

// EditorViewModel represents the complete state needed to render the editor view.
type EditorViewModel struct {
	ViewportY    int
	ViewportX    int
	Height       int
	Width        int
	GutterWidth  int
	Lines        []RenderLine
	CursorRow    int
	CursorCol    int
	StatusText   string
	PopupVisible bool
	PopupTitle   string
	PopupItems   []string
	PopupActive  int
	PopupDoc     string
	PopupRow     int
	PopupCol     int
	OmnibarOpen  bool
	OmnibarMode  string // "files" or "commands"
	OmnibarQuery string
	OmnibarItems []string
	OmnibarSel   int
}

// Theme defines the 24-bit RGB TrueColor palette for Tahr.
type Theme struct {
	Name            string
	Background      uint32
	Foreground      uint32
	LineNumber      uint32
	LineNumberCurr  uint32
	CursorLineBg    uint32
	SelectionBg     uint32
	GutterBg        uint32
	StatusBarBg     uint32
	StatusBarFg     uint32
	PopupBg         uint32
	PopupFg         uint32
	PopupSelBg      uint32
	PopupSelFg      uint32
	BorderColor     uint32
	DiagnosticError uint32
	DiagnosticWarn  uint32
	DiagnosticInfo  uint32
	Keyword         uint32
	Function        uint32
	String          uint32
	Comment         uint32
	Type            uint32
	Constant        uint32
	OccurrenceBg    uint32
}

// CatppuccinMocha returns the Catppuccin Mocha theme.
func CatppuccinMocha() Theme {
	return Theme{
		Name:            "catppuccin-mocha",
		Background:      0x1e1e2e,
		Foreground:      0xcdd6f4,
		LineNumber:      0x6c7086,
		LineNumberCurr:  0xcba6f7,
		CursorLineBg:    0x313244,
		SelectionBg:     0x45475a,
		OccurrenceBg:    0x393b4f,
		GutterBg:        0x181825,
		StatusBarBg:     0x11111b,
		StatusBarFg:     0xa6adc8,
		PopupBg:         0x181825,
		PopupFg:         0xcdd6f4,
		PopupSelBg:      0x313244,
		PopupSelFg:      0xf5c2e7,
		BorderColor:     0x585b70,
		DiagnosticError: 0xf38ba8,
		DiagnosticWarn:  0xf9e2af,
		DiagnosticInfo:  0x89dceb,
		Keyword:         0xcba6f7,
		Function:        0x89b4fa,
		String:          0xa6e3a1,
		Comment:         0x6c7086,
		Type:            0xf9e2af,
		Constant:        0xfab387,
	}
}

// GruvboxDark returns the Gruvbox Dark theme.
func GruvboxDark() Theme {
	return Theme{
		Name:            "gruvbox-dark",
		Background:      0x282828,
		Foreground:      0xebdbb2,
		LineNumber:      0x7c6f64,
		LineNumberCurr:  0xfe8019,
		CursorLineBg:    0x3c3836,
		SelectionBg:     0x504945,
		OccurrenceBg:    0x3c3836,
		GutterBg:        0x1d2021,
		StatusBarBg:     0x3c3836,
		StatusBarFg:     0xd5c4a1,
		PopupBg:         0x1d2021,
		PopupFg:         0xebdbb2,
		PopupSelBg:      0x504945,
		PopupSelFg:      0xfb4934,
		BorderColor:     0x665c54,
		DiagnosticError: 0xfb4934,
		DiagnosticWarn:  0xfabd2f,
		DiagnosticInfo:  0x83a598,
		Keyword:         0xfb4934,
		Function:        0xb8bb26,
		String:          0xb8bb26,
		Comment:         0x928374,
		Type:            0xfabd2f,
		Constant:        0xd3869b,
	}
}

// TokyoNight returns the Tokyo Night theme.
func TokyoNight() Theme {
	return Theme{
		Name:            "tokyo-night",
		Background:      0x1a1b26,
		Foreground:      0xc0caf5,
		LineNumber:      0x565f89,
		LineNumberCurr:  0x7aa2f7,
		CursorLineBg:    0x292e42,
		SelectionBg:     0x364a82,
		OccurrenceBg:    0x292e42,
		GutterBg:        0x16161e,
		StatusBarBg:     0x16161e,
		StatusBarFg:     0x7aa2f7,
		PopupBg:         0x1f2335,
		PopupFg:         0xc0caf5,
		PopupSelBg:      0x3b4261,
		PopupSelFg:      0x7aa2f7,
		BorderColor:     0x3b4261,
		DiagnosticError: 0xf7768e,
		DiagnosticWarn:  0xe0af68,
		DiagnosticInfo:  0x7dcfff,
		Keyword:         0xbb9af7,
		Function:        0x7aa2f7,
		String:          0x9ece6a,
		Comment:         0x565f89,
		Type:            0x2ac3de,
		Constant:        0xff9e64,
	}
}

// Dracula returns the Dracula theme.
func Dracula() Theme {
	return Theme{
		Name:            "dracula",
		Background:      0x282a36,
		Foreground:      0xf8f8f2,
		LineNumber:      0x6272a4,
		LineNumberCurr:  0xf1fa8c,
		CursorLineBg:    0x44475a,
		SelectionBg:     0x44475a,
		OccurrenceBg:    0x383a59,
		GutterBg:        0x21222c,
		StatusBarBg:     0x191a21,
		StatusBarFg:     0xf8f8f2,
		PopupBg:         0x21222c,
		PopupFg:         0xf8f8f2,
		PopupSelBg:      0x44475a,
		PopupSelFg:      0x50fa7b,
		BorderColor:     0x6272a4,
		DiagnosticError: 0xff5555,
		DiagnosticWarn:  0xffb86c,
		DiagnosticInfo:  0x8be9fd,
		Keyword:         0xff79c6,
		Function:        0x50fa7b,
		String:          0xf1fa8c,
		Comment:         0x6272a4,
		Type:            0x8be9fd,
		Constant:        0xbd93f9,
	}
}

// Nord returns the Nord theme.
func Nord() Theme {
	return Theme{
		Name:            "nord",
		Background:      0x2e3440,
		Foreground:      0xd8dee9,
		LineNumber:      0x4c566a,
		LineNumberCurr:  0x88c0d0,
		CursorLineBg:    0x3b4252,
		SelectionBg:     0x434c5e,
		OccurrenceBg:    0x3b4252,
		GutterBg:        0x242933,
		StatusBarBg:     0x242933,
		StatusBarFg:     0xd8dee9,
		PopupBg:         0x242933,
		PopupFg:         0xd8dee9,
		PopupSelBg:      0x434c5e,
		PopupSelFg:      0x88c0d0,
		BorderColor:     0x4c566a,
		DiagnosticError: 0xbf616a,
		DiagnosticWarn:  0xebcb8b,
		DiagnosticInfo:  0x88c0d0,
		Keyword:         0x81a1c1,
		Function:        0x88c0d0,
		String:          0xa3be8c,
		Comment:         0x4c566a,
		Type:            0x8fbcbb,
		Constant:        0xb48ead,
	}
}

// Monokai returns the Monokai Pro theme.
func Monokai() Theme {
	return Theme{
		Name:            "monokai",
		Background:      0x272822,
		Foreground:      0xf8f8f2,
		LineNumber:      0x75715e,
		LineNumberCurr:  0xa6e22e,
		CursorLineBg:    0x3e3d32,
		SelectionBg:     0x49483e,
		OccurrenceBg:    0x3e3d32,
		GutterBg:        0x1e1f1c,
		StatusBarBg:     0x1e1f1c,
		StatusBarFg:     0xf8f8f2,
		PopupBg:         0x1e1f1c,
		PopupFg:         0xf8f8f2,
		PopupSelBg:      0x49483e,
		PopupSelFg:      0xa6e22e,
		BorderColor:     0x75715e,
		DiagnosticError: 0xf92672,
		DiagnosticWarn:  0xfd971f,
		DiagnosticInfo:  0x66d9ef,
		Keyword:         0xf92672,
		Function:        0xa6e22e,
		String:          0xe6db74,
		Comment:         0x75715e,
		Type:            0x66d9ef,
		Constant:        0xae81ff,
	}
}

// DefaultTheme returns the default theme (Catppuccin Mocha).
func DefaultTheme() Theme {
	return CatppuccinMocha()
}

// ViewportHelper computes visible lines from a buffer given viewport parameters.
func BuildViewModel(
	buf buffer.Buffer,
	viewportY, viewportX int,
	height, width int,
	theme Theme,
	diagnostics map[int]string,
	breakpoints map[int]bool,
) EditorViewModel {
	totalLines := buf.TotalLines()
	gutterWidth := 5
	if totalLines >= 10000 {
		gutterWidth = 6
	}
	textWidth := width - gutterWidth
	if textWidth < 10 {
		textWidth = 10
	}

	renderLines := make([]RenderLine, 0, height)
	for row := 0; row < height; row++ {
		lineIdx := viewportY + row
		if lineIdx >= totalLines {
			renderLines = append(renderLines, RenderLine{
				LineNumber: lineIdx + 1,
				Cells:      nil,
			})
			continue
		}

		lineBytes, _ := buf.GetLine(lineIdx)
		runes := []rune(string(lineBytes))

		// Gutter metadata
		gutterIcon := ' '
		gutterFg := theme.LineNumber
		if breakpoints != nil && breakpoints[lineIdx] {
			gutterIcon = '*'
			gutterFg = theme.DiagnosticError
		} else if diagnostics != nil {
			if _, ok := diagnostics[lineIdx]; ok {
				gutterIcon = '!'
				gutterFg = theme.DiagnosticWarn
			}
		}

		// Horizontal scroll slice
		var cells []Cell
		if viewportX < len(runes) {
			endRune := viewportX + textWidth
			if endRune > len(runes) {
				endRune = len(runes)
			}
			visibleRunes := runes[viewportX:endRune]
			cells = make([]Cell, len(visibleRunes))
			for i, r := range visibleRunes {
				if r == '\n' || r == '\r' {
					continue
				}
				cells[i] = Cell{
					Char: r,
					Style: CellStyle{
						FgColor: theme.Foreground,
						BgColor: theme.Background,
					},
				}
			}
		}

		renderLines = append(renderLines, RenderLine{
			LineNumber: lineIdx + 1,
			Cells:      cells,
			GutterIcon: gutterIcon,
			GutterFg:   gutterFg,
		})
	}

	sels := buf.GetSelections()
	cursorRow := 0
	cursorCol := 0
	if len(sels) > 0 {
		head := sels[0].Head
		cursorRow = head.Line - viewportY
		cursorCol = gutterWidth + (head.Column - viewportX)
	}

	return EditorViewModel{
		ViewportY:   viewportY,
		ViewportX:   viewportX,
		Height:      height,
		Width:       width,
		GutterWidth: gutterWidth,
		Lines:       renderLines,
		CursorRow:   cursorRow,
		CursorCol:   cursorCol,
	}
}

// ParseHexUint parses a hex string like "#1e1e2e" or "1e1e2e" into uint32.
func ParseHexUint(hexStr string) (uint32, bool) {
	clean := strings.TrimPrefix(strings.TrimSpace(hexStr), "#")
	if len(clean) == 3 {
		r := string(clean[0]) + string(clean[0])
		g := string(clean[1]) + string(clean[1])
		b := string(clean[2]) + string(clean[2])
		clean = r + g + b
	}
	if len(clean) >= 6 {
		val, err := strconv.ParseUint(clean[:6], 16, 32)
		if err == nil {
			return uint32(val), true
		}
	}
	return 0, false
}

// ThemeFromMap constructs a Theme from fallback and a map of hex strings.
func ThemeFromMap(name string, base Theme, colors map[string]string) Theme {
	t := base
	t.Name = name
	for k, v := range colors {
		col, ok := ParseHexUint(v)
		if !ok {
			continue
		}
		switch strings.ToLower(k) {
		case "background", "bg":
			t.Background = col
		case "foreground", "fg":
			t.Foreground = col
		case "linenumber", "line_number":
			t.LineNumber = col
		case "cursorlinebg", "cursor_line_bg":
			t.CursorLineBg = col
		case "selectionbg", "selection_bg":
			t.SelectionBg = col
		case "gutterbg", "gutter_bg":
			t.GutterBg = col
		case "statuslinebg", "statusbar_bg", "statusbarbg":
			t.StatusBarBg = col
		case "statuslinefg", "statusbar_fg", "statusbarfg":
			t.StatusBarFg = col
		case "border", "bordercolor", "border_color":
			t.BorderColor = col
		case "error", "diagnosticerror", "diagnostic_error":
			t.DiagnosticError = col
		case "warn", "warning", "diagnosticwarn", "diagnostic_warn":
			t.DiagnosticWarn = col
		case "info", "diagnosticinfo", "diagnostic_info":
			t.DiagnosticInfo = col
		case "keyword":
			t.Keyword = col
		case "function":
			t.Function = col
		case "string":
			t.String = col
		case "comment":
			t.Comment = col
		case "type":
			t.Type = col
		case "constant":
			t.Constant = col
		case "occurrencebg", "occurrence_bg", "word_highlight", "wordhighlight":
			t.OccurrenceBg = col
		}
	}
	return t
}
