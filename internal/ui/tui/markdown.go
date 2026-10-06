package tui

import (
	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/widgets"
	"tahr/internal/ui"
)

// MarkdownLineKind classifies parsed markdown lines for styled presentation.
type MarkdownLineKind = widgets.MarkdownLineKind

const (
	MdText           = widgets.MdText
	MdH1             = widgets.MdH1
	MdH2             = widgets.MdH2
	MdH3             = widgets.MdH3
	MdH4             = widgets.MdH4
	MdBlockquote     = widgets.MdBlockquote
	MdBullet         = widgets.MdBullet
	MdNumbered       = widgets.MdNumbered
	MdTaskUnchecked  = widgets.MdTaskUnchecked
	MdTaskChecked    = widgets.MdTaskChecked
	MdCodeFenceStart = widgets.MdCodeFenceStart
	MdCodeLine       = widgets.MdCodeLine
	MdCodeFenceEnd   = widgets.MdCodeFenceEnd
	MdTable          = widgets.MdTable
	MdDivider        = widgets.MdDivider
)

// FormattedMdLine represents a rendered line with visual attributes.
type FormattedMdLine = widgets.FormattedMdLine

// MarkdownRenderer parses raw markdown text and renders a formatted terminal preview buffer.
type MarkdownRenderer struct {
	inner *widgets.MarkdownRenderer
	Lines []FormattedMdLine
}

// NewMarkdownRenderer parses markdown text into structured preview lines.
func NewMarkdownRenderer(rawText string) *MarkdownRenderer {
	inner := widgets.NewMarkdownRenderer(rawText)
	return &MarkdownRenderer{
		inner: inner,
		Lines: inner.Lines,
	}
}

// Parse converts raw markdown lines into FormattedMdLine slices.
func (mr *MarkdownRenderer) Parse(rawText string) {
	if mr.inner == nil {
		mr.inner = &widgets.MarkdownRenderer{}
	}
	mr.inner.Parse(rawText)
	mr.Lines = mr.inner.Lines
}

// TotalLines returns the total count of parsed markdown lines.
func (mr *MarkdownRenderer) TotalLines() int {
	return len(mr.Lines)
}

// Render writes formatted markdown content into a GoatUI buffer using theme styling.
func (mr *MarkdownRenderer) Render(buf *buffer.Buffer, startX, startY, width, height, scrollY int, th *ui.Theme) {
	if mr.inner == nil {
		mr.inner = &widgets.MarkdownRenderer{Lines: mr.Lines}
	}
	if th == nil {
		mr.inner.Render(buf, startX, startY, width, height, scrollY, widgets.DefaultMarkdownStyle())
		return
	}
	st := widgets.MarkdownStyle{
		Fg:       toColor(th.Foreground),
		Bg:       toColor(th.Background),
		H1Fg:     toColor(th.Keyword),
		H2Fg:     toColor(th.Function),
		BorderFg: toColor(th.BorderColor),
		CodeFg:   toColor(th.String),
		CodeBg:   toColor(th.CursorLineBg),
		MutedFg:  toColor(th.Comment),
		StringFg: toColor(th.String),
	}
	mr.inner.Render(buf, startX, startY, width, height, scrollY, st)
}
