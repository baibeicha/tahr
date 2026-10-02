package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/ui"
)

// ToolPromptModal provides an interactive prompt when a required binary is missing.
type ToolPromptModal struct {
	Open        bool
	ToolName    string
	PluginName  string
	InstallCmd  string
	FileExt     string
	InputMode   bool
	InputPath   string
	StatusMsg   string
	BorderRounded bool
	OnInstall     func()
	OnSetPath     func(path string)
}

// NewToolPromptModal creates a new missing tool prompt modal.
func NewToolPromptModal() *ToolPromptModal {
	return &ToolPromptModal{
		Open: false,
	}
}

// OpenForTool opens the modal for a missing tool.
func (tp *ToolPromptModal) OpenForTool(toolName, pluginName, installCmd, ext string, onInstall func(), onSetPath func(path string)) {
	tp.Open = true
	tp.ToolName = toolName
	tp.PluginName = pluginName
	tp.InstallCmd = installCmd
	tp.FileExt = ext
	tp.InputMode = false
	tp.InputPath = ""
	tp.StatusMsg = ""
	tp.OnInstall = onInstall
	tp.OnSetPath = onSetPath
}

// Close dismisses the modal.
func (tp *ToolPromptModal) Close() {
	tp.Open = false
	tp.InputMode = false
}

// HandleKey handles keyboard input inside the tool prompt modal.
func (tp *ToolPromptModal) HandleKey(k input.Key) (handled bool, shouldClose bool) {
	if !tp.Open {
		return false, false
	}

	if tp.InputMode {
		switch k.Type {
		case input.KeyEsc:
			tp.InputMode = false
			return true, false
		case input.KeyEnter:
			p := strings.TrimSpace(tp.InputPath)
			if p != "" && tp.OnSetPath != nil {
				tp.OnSetPath(p)
			}
			tp.Close()
			return true, true
		case input.KeyBackspace:
			runes := []rune(tp.InputPath)
			if len(runes) > 0 {
				tp.InputPath = string(runes[:len(runes)-1])
			}
			return true, false
		case input.KeySpace:
			tp.InputPath += " "
			return true, false
		case input.KeyRune:
			if k.Rune >= 32 {
				tp.InputPath += string(k.Rune)
			}
			return true, false
		}
		return true, false
	}

	switch k.Type {
	case input.KeyEsc:
		tp.Close()
		return true, true
	case input.KeyRune:
		switch k.Rune {
		case 'd', 'D':
			if tp.OnInstall != nil {
				tp.OnInstall()
			}
			tp.Close()
			return true, true
		case 'p', 'P':
			tp.InputMode = true
			return true, false
		}
	}

	return true, false
}

// HandleClick handles mouse clicks inside the tool prompt modal.
func (tp *ToolPromptModal) HandleClick(mouseX, mouseY, w, h int) bool {
	if !tp.Open {
		return false
	}

	modalW := 68
	if modalW > w-4 {
		modalW = w - 4
	}
	modalH := 11
	if tp.InputMode {
		modalH = 13
	}
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2

	if mouseX < startX || mouseX >= startX+modalW || mouseY < startY || mouseY >= startY+modalH {
		tp.Close()
		return true
	}

	// Click on [D: Install Automatically] button (rendered at startY+4)
	if mouseY == startY+4 {
		if tp.OnInstall != nil {
			tp.OnInstall()
		}
		tp.Close()
		return true
	}

	// Click on [P: Specify Path] button (rendered at startY+5)
	if mouseY == startY+5 {
		tp.InputMode = true
		return true
	}

	// Click on [Esc: Ignore] button (rendered at startY+6)
	if mouseY == startY+6 {
		tp.Close()
		return true
	}

	return true
}

// Render draws the tool prompt modal.
func (tp *ToolPromptModal) Render(buf *buffer.Buffer, w, h int, th *ui.Theme) {
	if !tp.Open || buf == nil {
		return
	}

	modalW := 68
	if modalW > w-4 {
		modalW = w - 4
	}
	modalH := 11
	if tp.InputMode {
		modalH = 13
	}
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	themeBg := toColor(th.Background)
	themeFg := toColor(th.Foreground)
	borderFg := toColor(th.DiagnosticWarn)
	accentFg := toColor(th.Function)
	errFg := toColor(th.DiagnosticError)
	commentFg := toColor(th.Comment)
	selBg := toColor(th.SelectionBg)
	selFg := toColor(th.Foreground)

	// Draw frame
	tl, tr, bl, br := '┌', '┐', '└', '┘'
	if tp.BorderRounded {
		tl, tr, bl, br = '╭', '╮', '╰', '╯'
	}
	for y := 0; y < modalH; y++ {
		for x := 0; x < modalW; x++ {
			ch := ' '
			fg := borderFg
			if y == 0 && x == 0 {
				ch = tl
			} else if y == 0 && x == modalW-1 {
				ch = tr
			} else if y == modalH-1 && x == 0 {
				ch = bl
			} else if y == modalH-1 && x == modalW-1 {
				ch = br
			} else if y == 0 || y == modalH-1 {
				ch = '─'
			} else if x == 0 || x == modalW-1 {
				ch = '│'
			}
			buf.SetRune(startX+x, startY+y, ch, fg, themeBg, cell.AttrNone)
		}
	}

	// Title
	title := fmt.Sprintf(" Missing Tool: %s ", tp.ToolName)
	for i, r := range title {
		if startX+2+i < startX+modalW-2 {
			buf.SetRune(startX+2+i, startY, r, errFg, themeBg, cell.AttrBold)
		}
	}

	// Description
	desc := fmt.Sprintf("Language support for '%s' requires %s (%s).", tp.FileExt, tp.ToolName, tp.PluginName)
	for i, r := range desc {
		if startX+3+i < startX+modalW-3 {
			buf.SetRune(startX+3+i, startY+2, r, themeFg, themeBg, cell.AttrNone)
		}
	}

	// Options
	opt1 := fmt.Sprintf("  D: Download & Install: %s", tp.InstallCmd)
	if tp.InstallCmd == "" {
		opt1 = "  D: Download & Install Automatically"
	}
	for i, r := range opt1 {
		if startX+2+i < startX+modalW-2 {
			fg := accentFg
			if r == 'D' || r == ':' {
				fg = toColor(th.Keyword)
			}
			buf.SetRune(startX+2+i, startY+4, r, fg, themeBg, cell.AttrBold)
		}
	}

	opt2 := "  P: Specify Path to Binary Manually"
	for i, r := range opt2 {
		if startX+2+i < startX+modalW-2 {
			fg := themeFg
			if r == 'P' || r == ':' {
				fg = toColor(th.Keyword)
			}
			buf.SetRune(startX+2+i, startY+5, r, fg, themeBg, cell.AttrBold)
		}
	}

	opt3 := "  Esc: Ignore / Continue without Language Server"
	for i, r := range opt3 {
		if startX+2+i < startX+modalW-2 {
			buf.SetRune(startX+2+i, startY+6, r, commentFg, themeBg, cell.AttrNone)
		}
	}

	if tp.InputMode {
		promptLbl := "Enter Binary Path: "
		for i, r := range promptLbl {
			buf.SetRune(startX+4+i, startY+8, r, accentFg, themeBg, cell.AttrBold)
		}
		inX := startX + 4 + len(promptLbl)
		inW := modalW - 8 - len(promptLbl)
		inVal := tp.InputPath + "_"
		for i := 0; i < inW; i++ {
			r := ' '
			if i < len(inVal) {
				r = rune(inVal[i])
			}
			buf.SetRune(inX+i, startY+8, r, selFg, selBg, cell.AttrBold)
		}

		hint := " Enter: Confirm  │  Esc: Back "
		for i, r := range hint {
			buf.SetRune(startX+4+i, startY+10, r, commentFg, themeBg, cell.AttrNone)
		}
	}
}
