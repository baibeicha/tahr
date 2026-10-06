package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/clipboard"
	"tahr/internal/core/regextester"
	"tahr/internal/ui"
)

// RegexField identifies the active input field inside RegexModal.
type RegexField int

const (
	RegexFieldPattern RegexField = iota
	RegexFieldText
	RegexFieldReplace
)

// RegexModalButtonHit records the bounding box of an interactive button or flag.
type RegexModalButtonHit struct {
	Action string
	X, Y   int
	Width  int
}

// RegexModal provides an interactive regular expression testing and replacement modal.
type RegexModal struct {
	Open        bool
	ActiveField RegexField

	// Input fields
	PatternText string
	TargetText  string
	ReplaceText string

	// Flags
	FlagCaseInsensitive bool // (?i)
	FlagMultiline       bool // (?m)
	FlagDotAll          bool // (?s)

	// Results
	Result      *regextester.Result
	ReplaceDone bool
	ReplaceOut  string
	StatusMsg   string
	IsError     bool

	ButtonHits []RegexModalButtonHit
	OnToast    func(level, title, msg string)
}

// NewRegexModal creates a new Regex tester modal.
func NewRegexModal() *RegexModal {
	return &RegexModal{
		Open:        false,
		ActiveField: RegexFieldPattern,
	}
}

// Show opens the modal and evaluates current text.
func (m *RegexModal) Show() {
	m.Open = true
	m.ActiveField = RegexFieldPattern
	m.Evaluate()
}

// Close closes the modal.
func (m *RegexModal) Close() {
	m.Open = false
	m.StatusMsg = ""
}

// Evaluate runs the regex engine over current target text.
func (m *RegexModal) Evaluate() {
	m.StatusMsg = ""
	m.IsError = false
	m.ReplaceDone = false

	if strings.TrimSpace(m.PatternText) == "" {
		m.Result = nil
		return
	}

	flags := regextester.Flags{
		CaseInsensitive: m.FlagCaseInsensitive,
		Multiline:       m.FlagMultiline,
		DotAll:          m.FlagDotAll,
	}

	res := regextester.Evaluate(m.PatternText, flags, m.TargetText)
	m.Result = res

	if !res.IsValid {
		m.StatusMsg = "Regex error: " + res.Error
		m.IsError = true
	} else {
		m.StatusMsg = fmt.Sprintf("Found %d matches", res.TotalCount)
	}
}

// ExecuteReplace performs regular expression substitution.
func (m *RegexModal) ExecuteReplace() {
	m.StatusMsg = ""
	m.IsError = false

	if strings.TrimSpace(m.PatternText) == "" {
		m.StatusMsg = "Pattern cannot be empty"
		m.IsError = true
		return
	}

	flags := regextester.Flags{
		CaseInsensitive: m.FlagCaseInsensitive,
		Multiline:       m.FlagMultiline,
		DotAll:          m.FlagDotAll,
	}

	eng, err := regextester.Compile(m.PatternText, flags)
	if err != nil {
		m.StatusMsg = "Replace error: " + err.Error()
		m.IsError = true
		return
	}

	m.ReplaceDone = true
	m.ReplaceOut = eng.ReplaceAll(m.TargetText, m.ReplaceText)
	m.StatusMsg = "Replacement completed"
}

// CopyMatches copies the matched strings to system clipboard.
func (m *RegexModal) CopyMatches() {
	if (m.Result == nil || len(m.Result.Matches) == 0) && !m.ReplaceDone {
		return
	}

	var textToCopy string
	if m.ReplaceDone {
		textToCopy = m.ReplaceOut
	} else if m.Result != nil {
		var parts []string
		for _, match := range m.Result.Matches {
			parts = append(parts, match.Text)
		}
		textToCopy = strings.Join(parts, "\n")
	}

	_ = clipboard.Write(textToCopy)
	m.StatusMsg = "Copied to clipboard"
	if m.OnToast != nil {
		m.OnToast("info", "REGEX", "Copied to clipboard")
	}
}

// HandleKey handles interactive keyboard events.
func (m *RegexModal) HandleKey(k input.Key) bool {
	if !m.Open {
		return false
	}

	switch k.Type {
	case input.KeyEsc:
		m.Close()
		return true

	case input.KeyTab:
		m.ActiveField = (m.ActiveField + 1) % 3
		return true

	case input.KeyBacktab:
		m.ActiveField = (m.ActiveField + 2) % 3
		return true

	case input.KeyEnter:
		if m.ActiveField == RegexFieldReplace {
			m.ExecuteReplace()
		} else {
			m.Evaluate()
		}
		return true

	case input.KeyBackspace:
		switch m.ActiveField {
		case RegexFieldPattern:
			if len(m.PatternText) > 0 {
				m.PatternText = m.PatternText[:len(m.PatternText)-1]
				m.Evaluate()
			}
		case RegexFieldText:
			if len(m.TargetText) > 0 {
				m.TargetText = m.TargetText[:len(m.TargetText)-1]
				m.Evaluate()
			}
		case RegexFieldReplace:
			if len(m.ReplaceText) > 0 {
				m.ReplaceText = m.ReplaceText[:len(m.ReplaceText)-1]
			}
		}
		return true
	}

	if k.Rune != 0 {
		switch m.ActiveField {
		case RegexFieldPattern:
			m.PatternText += string(k.Rune)
			m.Evaluate()
		case RegexFieldText:
			m.TargetText += string(k.Rune)
			m.Evaluate()
		case RegexFieldReplace:
			m.ReplaceText += string(k.Rune)
		}
		return true
	}

	return false
}

// HandleClick processes clicks on buttons and flag toggles.
func (m *RegexModal) HandleClick(x, y int) bool {
	if !m.Open {
		return false
	}

	for _, hit := range m.ButtonHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.Width {
			switch hit.Action {
			case "flag_i":
				m.FlagCaseInsensitive = !m.FlagCaseInsensitive
				m.Evaluate()
			case "flag_m":
				m.FlagMultiline = !m.FlagMultiline
				m.Evaluate()
			case "flag_s":
				m.FlagDotAll = !m.FlagDotAll
				m.Evaluate()
			case "focus_pattern":
				m.ActiveField = RegexFieldPattern
			case "focus_text":
				m.ActiveField = RegexFieldText
			case "focus_replace":
				m.ActiveField = RegexFieldReplace
			case "match":
				m.Evaluate()
			case "replace":
				m.ExecuteReplace()
			case "copy":
				m.CopyMatches()
			case "clear":
				m.PatternText = ""
				m.TargetText = ""
				m.ReplaceText = ""
				m.Result = nil
				m.ReplaceDone = false
				m.StatusMsg = ""
			case "close":
				m.Close()
			}
			return true
		}
	}

	return false
}

// Render draws the Regex tester modal.
func (m *RegexModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !m.Open {
		return
	}

	modalW := 84
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 26
	if modalH > screenH-4 {
		modalH = screenH - 4
	}

	x := (screenW - modalW) / 2
	y := (screenH - modalH) / 2

	m.ButtonHits = m.ButtonHits[:0]

	bg := toColor(theme.StatusBarBg)
	cardBg := toColor(theme.Background)
	textFg := toColor(theme.Foreground)
	dimFg := toColor(theme.Comment)
	borderFg := toColor(theme.BorderColor)
	selBg := toColor(theme.SelectionBg)
	accentFg := toColor(theme.Function)
	errFg := toColor(theme.DiagnosticError)
	greenFg := toColor(theme.String)
	activeFieldBg := toColor(theme.CursorLineBg)

	// Backdrop
	for row := 0; row < modalH; row++ {
		for col := 0; col < modalW; col++ {
			buf.SetRune(x+col, y+row, ' ', textFg, bg, cell.AttrNone)
		}
	}

	// Border
	drawBorderBox(buf, x, y, modalW, modalH, borderFg, bg)

	// Title
	title := " Regular Expression Tester (Regex) "
	drawText(buf, x+2, y, title, accentFg, bg, cell.AttrBold)

	// Close button
	closeLabel := " Close "
	closeX := x + modalW - len(closeLabel) - 2
	drawText(buf, closeX, y, closeLabel, dimFg, toColor(theme.CursorLineBg), cell.AttrNone)
	m.ButtonHits = append(m.ButtonHits, RegexModalButtonHit{
		Action: "close",
		X:      closeX,
		Y:      y,
		Width:  len(closeLabel),
	})

	curY := y + 2

	// 1. Pattern input row with flag toggles
	drawText(buf, x+2, curY, "Pattern:", textFg, bg, cell.AttrBold)
	flags := []struct {
		Label  string
		Action string
		Active bool
	}{
		{" Case-Insensitive (i) ", "flag_i", m.FlagCaseInsensitive},
		{" Multiline (m) ", "flag_m", m.FlagMultiline},
		{" DotAll (s) ", "flag_s", m.FlagDotAll},
	}
	flagX := x + 12
	for _, f := range flags {
		fBg := toColor(theme.CursorLineBg)
		fFg := dimFg
		if f.Active {
			fBg = selBg
			fFg = textFg
		}
		drawText(buf, flagX, curY, f.Label, fFg, fBg, cell.AttrNone)
		m.ButtonHits = append(m.ButtonHits, RegexModalButtonHit{
			Action: f.Action,
			X:      flagX,
			Y:      curY,
			Width:  len(f.Label),
		})
		flagX += len(f.Label) + 1
	}
	curY++

	// Pattern Box
	patBg := cardBg
	if m.ActiveField == RegexFieldPattern {
		patBg = activeFieldBg
	}
	drawBorderBox(buf, x+2, curY, modalW-4, 3, borderFg, patBg)
	patDisp := m.PatternText
	if patDisp == "" {
		patDisp = "Enter regex pattern, e.g. ([a-zA-Z0-9]+)@([a-z]+)\\.com"
		drawText(buf, x+3, curY+1, patDisp, dimFg, patBg, cell.AttrNone)
	} else {
		drawText(buf, x+3, curY+1, patDisp, textFg, patBg, cell.AttrNone)
	}
	m.ButtonHits = append(m.ButtonHits, RegexModalButtonHit{
		Action: "focus_pattern",
		X:      x + 2,
		Y:      curY,
		Width:  modalW - 4,
	})
	curY += 4

	// 2. Test Text input
	drawText(buf, x+2, curY, "Test Text:", textFg, bg, cell.AttrBold)
	curY++

	textBg := cardBg
	if m.ActiveField == RegexFieldText {
		textBg = activeFieldBg
	}
	textBoxH := 4
	drawBorderBox(buf, x+2, curY, modalW-4, textBoxH, borderFg, textBg)
	if m.TargetText == "" {
		drawText(buf, x+3, curY+1, "Enter sample text to match against...", dimFg, textBg, cell.AttrNone)
	} else {
		// Render text with match highlighting
		targetRunes := []rune(m.TargetText)
		matchMap := make(map[int]bool)
		if m.Result != nil {
			for _, sp := range m.Result.MatchSpans {
				for idx := sp.Start; idx < sp.End && idx < len(targetRunes); idx++ {
					matchMap[idx] = true
				}
			}
		}

		line := 0
		col := 0
		for idx, r := range targetRunes {
			if r == '\n' {
				line++
				col = 0
				continue
			}
			if line >= textBoxH-2 || col >= modalW-6 {
				continue
			}
			rBg := textBg
			rFg := textFg
			rAttr := cell.AttrNone
			if matchMap[idx] {
				rBg = selBg
				rFg = toColor(theme.DiagnosticWarn)
				rAttr = cell.AttrBold
			}
			buf.SetRune(x+3+col, curY+1+line, r, rFg, rBg, rAttr)
			col++
		}
	}
	m.ButtonHits = append(m.ButtonHits, RegexModalButtonHit{
		Action: "focus_text",
		X:      x + 2,
		Y:      curY,
		Width:  modalW - 4,
	})
	curY += textBoxH + 1

	// 3. Replacement input & buttons row
	drawText(buf, x+2, curY, "Replace Expression:", textFg, bg, cell.AttrBold)
	curY++

	repBg := cardBg
	if m.ActiveField == RegexFieldReplace {
		repBg = activeFieldBg
	}
	drawBorderBox(buf, x+2, curY, modalW-4, 3, borderFg, repBg)
	repDisp := m.ReplaceText
	if repDisp == "" {
		repDisp = "Replacement string (e.g. $1 or redacted)"
		drawText(buf, x+3, curY+1, repDisp, dimFg, repBg, cell.AttrNone)
	} else {
		drawText(buf, x+3, curY+1, repDisp, textFg, repBg, cell.AttrNone)
	}
	m.ButtonHits = append(m.ButtonHits, RegexModalButtonHit{
		Action: "focus_replace",
		X:      x + 2,
		Y:      curY,
		Width:  modalW - 4,
	})
	curY += 4

	// Action buttons
	actions := []struct {
		Label  string
		Action string
	}{
		{" Match ", "match"},
		{" Replace ", "replace"},
		{" Copy ", "copy"},
		{" Clear ", "clear"},
	}
	btnX := x + 2
	for _, a := range actions {
		drawText(buf, btnX, curY, a.Label, textFg, toColor(theme.CursorLineBg), cell.AttrBold)
		m.ButtonHits = append(m.ButtonHits, RegexModalButtonHit{
			Action: a.Action,
			X:      btnX,
			Y:      curY,
			Width:  len(a.Label),
		})
		btnX += len(a.Label) + 1
	}

	// Status message on right of buttons
	if m.StatusMsg != "" {
		sFg := greenFg
		if m.IsError {
			sFg = errFg
		}
		drawText(buf, btnX+2, curY, "● "+m.StatusMsg, sFg, bg, cell.AttrNone)
	}
	curY += 2

	// Replacement output preview if active
	if m.ReplaceDone {
		drawText(buf, x+2, curY, "Replaced Output Preview:", accentFg, bg, cell.AttrBold)
		curY++
		previewH := y + modalH - curY - 1
		if previewH > 1 {
			drawBorderBox(buf, x+2, curY, modalW-4, previewH, borderFg, cardBg)
			drawText(buf, x+3, curY+1, m.ReplaceOut, textFg, cardBg, cell.AttrNone)
		}
	} else if m.Result != nil && len(m.Result.Matches) > 0 {
		// Matches list preview
		matchSummary := fmt.Sprintf("Matches (%d):", len(m.Result.Matches))
		drawText(buf, x+2, curY, matchSummary, accentFg, bg, cell.AttrBold)
		curY++
		for i, mMatch := range m.Result.Matches {
			if curY >= y+modalH-2 {
				break
			}
			mLine := fmt.Sprintf("#%d: %q [span: %d..%d]", i+1, mMatch.Text, mMatch.Span.Start, mMatch.Span.End)
			drawText(buf, x+3, curY, mLine, textFg, bg, cell.AttrNone)
			curY++
		}
	}
}
