package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/ui"
)

func TestMarkdownRenderer_ParseAndRender(t *testing.T) {
	mdText := `# Project Title
This is intro text.

## Features
- Fast Augmented Rope
- Multi-selection
- [ ] Inlay Hints
- [x] Fullscreen Marketplace

> Quote block

` + "```go" + `
func main() {
    println("hello")
}
` + "```" + `

| Name | Status |
| --- | --- |
| Tahr | Ready |
---
`

	mr := NewMarkdownRenderer(mdText)
	if mr.TotalLines() == 0 {
		t.Fatal("expected parsed markdown lines, got 0")
	}

	// Verify classifications
	foundH1 := false
	foundH2 := false
	foundCheckUnchecked := false
	foundCheckChecked := false
	foundCodeStart := false
	foundTable := false

	for _, l := range mr.Lines {
		switch l.Kind {
		case MdH1:
			if l.Text == "Project Title" {
				foundH1 = true
			}
		case MdH2:
			if l.Text == "Features" {
				foundH2 = true
			}
		case MdTaskUnchecked:
			if l.Text == "Inlay Hints" {
				foundCheckUnchecked = true
			}
		case MdTaskChecked:
			if l.Text == "Fullscreen Marketplace" {
				foundCheckChecked = true
			}
		case MdCodeFenceStart:
			if l.Aux == "go" {
				foundCodeStart = true
			}
		case MdTable:
			foundTable = true
		}
	}

	if !foundH1 || !foundH2 || !foundCheckUnchecked || !foundCheckChecked || !foundCodeStart || !foundTable {
		t.Errorf("markdown parser failed to identify all elements: h1=%v h2=%v unchk=%v chk=%v code=%v table=%v",
			foundH1, foundH2, foundCheckUnchecked, foundCheckChecked, foundCodeStart, foundTable)
	}

	// Test render into buffer
	buf := buffer.NewBuffer(80, 30)
	th := ui.CatppuccinMocha()
	mr.Render(buf, 0, 0, 80, 25, 0, &th)

	// Verify buffer has non-empty cells
	c := buf.Cell(2, 0)
	if c == nil || (c.Rune == ' ' && c.Bg == 0) {
		t.Errorf("expected rendered content in buffer, got empty cell: %+v", c)
	}
}
