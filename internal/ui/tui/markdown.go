package tui

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/ui"
)

// MarkdownLineKind classifies parsed markdown lines for styled presentation.
type MarkdownLineKind int

const (
	MdText MarkdownLineKind = iota
	MdH1
	MdH2
	MdH3
	MdH4
	MdBlockquote
	MdBullet
	MdNumbered
	MdTaskUnchecked
	MdTaskChecked
	MdCodeFenceStart
	MdCodeLine
	MdCodeFenceEnd
	MdTable
	MdDivider
)

// FormattedMdLine represents a rendered line with visual attributes.
type FormattedMdLine struct {
	Kind   MarkdownLineKind
	Text   string
	Aux    string // Language tag for code fence, or original row
	Indent int
}

// MarkdownRenderer parses raw markdown text and renders a formatted terminal preview buffer.
type MarkdownRenderer struct {
	Lines []FormattedMdLine
}

// NewMarkdownRenderer parses markdown text into structured preview lines.
func NewMarkdownRenderer(rawText string) *MarkdownRenderer {
	mr := &MarkdownRenderer{}
	mr.Parse(rawText)
	return mr
}

// Parse converts raw markdown lines into FormattedMdLine slices.
func (mr *MarkdownRenderer) Parse(rawText string) {
	mr.Lines = nil
	scanner := bufio.NewScanner(strings.NewReader(rawText))
	inCodeFence := false
	codeLang := ""

	for scanner.Scan() {
		rawLine := scanner.Text()
		trimmed := strings.TrimSpace(rawLine)

		// Code fence toggle: ```lang
		if strings.HasPrefix(trimmed, "```") {
			if !inCodeFence {
				inCodeFence = true
				codeLang = strings.TrimPrefix(trimmed, "```")
				if codeLang == "" {
					codeLang = "code"
				}
				mr.Lines = append(mr.Lines, FormattedMdLine{
					Kind: MdCodeFenceStart,
					Aux:  codeLang,
				})
			} else {
				inCodeFence = false
				mr.Lines = append(mr.Lines, FormattedMdLine{
					Kind: MdCodeFenceEnd,
				})
			}
			continue
		}

		if inCodeFence {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdCodeLine,
				Text: rawLine,
				Aux:  codeLang,
			})
			continue
		}

		if trimmed == "" {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdText,
				Text: "",
			})
			continue
		}

		// Horizontal rule: --- or ***
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdDivider,
			})
			continue
		}

		// Headers
		if strings.HasPrefix(rawLine, "# ") {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdH1,
				Text: strings.TrimPrefix(rawLine, "# "),
			})
			continue
		}
		if strings.HasPrefix(rawLine, "## ") {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdH2,
				Text: strings.TrimPrefix(rawLine, "## "),
			})
			continue
		}
		if strings.HasPrefix(rawLine, "### ") {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdH3,
				Text: strings.TrimPrefix(rawLine, "### "),
			})
			continue
		}
		if strings.HasPrefix(rawLine, "#### ") {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdH4,
				Text: strings.TrimPrefix(rawLine, "#### "),
			})
			continue
		}

		// Blockquote
		if strings.HasPrefix(trimmed, "> ") {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdBlockquote,
				Text: strings.TrimPrefix(trimmed, "> "),
			})
			continue
		}

		// Checkboxes
		if strings.HasPrefix(trimmed, "- [ ] ") || strings.HasPrefix(trimmed, "* [ ] ") {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdTaskUnchecked,
				Text: trimmed[6:],
			})
			continue
		}
		if strings.HasPrefix(trimmed, "- [x] ") || strings.HasPrefix(trimmed, "* [x] ") ||
			strings.HasPrefix(trimmed, "- [X] ") || strings.HasPrefix(trimmed, "* [X] ") {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdTaskChecked,
				Text: trimmed[6:],
			})
			continue
		}

		// Bullet lists
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
			indent := len(rawLine) - len(strings.TrimLeft(rawLine, " \t"))
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind:   MdBullet,
				Text:   trimmed[2:],
				Indent: indent,
			})
			continue
		}

		// Numbered list: 1.
		if len(trimmed) > 3 && trimmed[1] == '.' && trimmed[2] == ' ' && trimmed[0] >= '0' && trimmed[0] <= '9' {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdNumbered,
				Text: trimmed,
			})
			continue
		}

		// Table line
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			mr.Lines = append(mr.Lines, FormattedMdLine{
				Kind: MdTable,
				Text: trimmed,
			})
			continue
		}

		// Regular paragraph text
		mr.Lines = append(mr.Lines, FormattedMdLine{
			Kind: MdText,
			Text: rawLine,
		})
	}
}

// TotalLines returns the total count of rendered markdown lines.
func (mr *MarkdownRenderer) TotalLines() int {
	return len(mr.Lines)
}

// Render renders the formatted markdown preview into a screen rectangle buffer.
func (mr *MarkdownRenderer) Render(buf *buffer.Buffer, startX, startY, width, height, scrollY int, theme *ui.Theme) {
	if buf == nil || width < 4 || height < 1 {
		return
	}

	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	h1Fg := toColor(theme.Function)
	h2Fg := toColor(theme.DiagnosticWarn)
	h3Fg := toColor(theme.String)
	commentFg := toColor(theme.Comment)
	codeBg := toColor(theme.SelectionBg)
	codeFg := toColor(theme.Foreground)

	for row := 0; row < height; row++ {
		lineIdx := scrollY + row
		screenY := startY + row

		// Clear row background
		for x := 0; x < width; x++ {
			buf.SetRune(startX+x, screenY, ' ', fg, bg, cell.AttrNone)
		}

		if lineIdx >= len(mr.Lines) {
			buf.SetRune(startX+1, screenY, '~', toColor(theme.LineNumber), bg, cell.AttrDim)
			continue
		}

		md := mr.Lines[lineIdx]

		switch md.Kind {
		case MdH1:
			headerText := " " + strings.ToUpper(md.Text) + " "
			for i, r := range headerText {
				if i < width-2 {
					buf.SetRune(startX+2+i, screenY, r, toColor(theme.Background), h1Fg, cell.AttrBold)
				}
			}

		case MdH2:
			title := "## " + md.Text
			for i, r := range title {
				if i < width-2 {
					buf.SetRune(startX+2+i, screenY, r, h2Fg, bg, cell.AttrBold)
				}
			}

		case MdH3:
			title := "▸ " + md.Text
			for i, r := range title {
				if i < width-2 {
					buf.SetRune(startX+2+i, screenY, r, h3Fg, bg, cell.AttrBold)
				}
			}

		case MdH4:
			title := "▪ " + md.Text
			for i, r := range title {
				if i < width-2 {
					buf.SetRune(startX+2+i, screenY, r, fg, bg, cell.AttrBold)
				}
			}

		case MdBlockquote:
			buf.SetRune(startX+2, screenY, '│', borderFg, bg, cell.AttrBold)
			qText := " " + md.Text
			for i, r := range qText {
				if i+3 < width-2 {
					buf.SetRune(startX+3+i, screenY, r, commentFg, bg, cell.AttrNone)
				}
			}

		case MdBullet:
			indent := 2 + (md.Indent / 2) * 2
			buf.SetRune(startX+indent, screenY, '•', h1Fg, bg, cell.AttrNone)
			bText := " " + md.Text
			for i, r := range bText {
				if indent+1+i < width-2 {
					buf.SetRune(startX+indent+1+i, screenY, r, fg, bg, cell.AttrNone)
				}
			}

		case MdNumbered:
			for i, r := range md.Text {
				if i+2 < width-2 {
					attr := cell.AttrNone
					cColor := fg
					if i < 3 {
						attr = cell.AttrBold
						cColor = h2Fg
					}
					buf.SetRune(startX+2+i, screenY, r, cColor, bg, attr)
				}
			}

		case MdTaskUnchecked:
			box := "○ "
			for i, r := range box {
				buf.SetRune(startX+2+i, screenY, r, commentFg, bg, cell.AttrNone)
			}
			for i, r := range md.Text {
				if i+5 < width-2 {
					buf.SetRune(startX+5+i, screenY, r, fg, bg, cell.AttrNone)
				}
			}

		case MdTaskChecked:
			box := "● "
			for i, r := range box {
				buf.SetRune(startX+2+i, screenY, r, toColor(theme.String), bg, cell.AttrBold)
			}
			for i, r := range md.Text {
				if i+5 < width-2 {
					buf.SetRune(startX+5+i, screenY, r, fg, bg, cell.AttrNone)
				}
			}

		case MdCodeFenceStart:
			tag := fmt.Sprintf("┌──  %s  ", md.Aux)
			for col := 0; col < width-4; col++ {
				r := '─'
				if col < len(tag) {
					r = rune(tag[col])
				}
				buf.SetRune(startX+2+col, screenY, r, borderFg, bg, cell.AttrNone)
			}
			buf.SetRune(startX+width-2, screenY, '┐', borderFg, bg, cell.AttrNone)

		case MdCodeLine:
			buf.SetRune(startX+2, screenY, '│', borderFg, bg, cell.AttrNone)
			for col := 0; col < width-5; col++ {
				r := ' '
				if col < len(md.Text) {
					r = rune(md.Text[col])
				}
				buf.SetRune(startX+3+col, screenY, r, codeFg, codeBg, cell.AttrNone)
			}
			buf.SetRune(startX+width-2, screenY, '│', borderFg, bg, cell.AttrNone)

		case MdCodeFenceEnd:
			for col := 0; col < width-4; col++ {
				buf.SetRune(startX+2+col, screenY, '─', borderFg, bg, cell.AttrNone)
			}
			buf.SetRune(startX+2, screenY, '└', borderFg, bg, cell.AttrNone)
			buf.SetRune(startX+width-2, screenY, '┘', borderFg, bg, cell.AttrNone)

		case MdDivider:
			for col := 0; col < width-4; col++ {
				buf.SetRune(startX+2+col, screenY, '─', borderFg, bg, cell.AttrNone)
			}

		case MdTable:
			// Highlight table pipes
			for i, r := range md.Text {
				if i+2 < width-2 {
					cColor := fg
					attr := cell.AttrNone
					if r == '|' || r == '-' || r == ':' {
						cColor = borderFg
					}
					buf.SetRune(startX+2+i, screenY, r, cColor, bg, attr)
				}
			}

		default: // MdText
			for i, r := range md.Text {
				if i+2 < width-2 {
					buf.SetRune(startX+2+i, screenY, r, fg, bg, cell.AttrNone)
				}
			}
		}
	}
}
