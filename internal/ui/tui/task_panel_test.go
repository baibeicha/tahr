package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

func TestTaskPanel_EmptyStateAndButtons(t *testing.T) {
	p := NewTaskPanel()
	if !p.Open {
		t.Fatalf("expected TaskPanel to start with Open=true")
	}

	buf := buffer.NewBuffer(34, 20)
	theme := ui.CatppuccinMocha()
	p.Render(buf, 0, 0, 34, 20, theme)

	// Collect rendered text
	var lines []string
	for y := 0; y < 20; y++ {
		var sb strings.Builder
		for x := 0; x < 34; x++ {
			c := buf.Cell(x, y)
			if c != nil && c.Rune != 0 {
				sb.WriteRune(c.Rune)
			} else {
				sb.WriteByte(' ')
			}
		}
		lines = append(lines, sb.String())
	}
	rendered := strings.Join(lines, "\n")

	// Verify informative empty state is present
	emptyTitle := i18n.T("task.empty_title")
	if !strings.Contains(rendered, emptyTitle) {
		t.Errorf("expected empty state title %q, got:\n%s", emptyTitle, rendered)
	}
	if !strings.Contains(rendered, "Makefile") || !strings.Contains(rendered, "package.json") {
		t.Errorf("expected supported formats in empty state, got:\n%s", rendered)
	}

	// Verify buttons have no brackets [ ]
	if strings.Contains(rendered, "[ Run ]") || strings.Contains(rendered, "[ Refresh ]") || strings.Contains(rendered, "[ Stop ]") {
		t.Errorf("found bracketed buttons in rendered output:\n%s", rendered)
	}

	// Verify buttons exist
	if !strings.Contains(rendered, "Run") || !strings.Contains(rendered, "Refresh") {
		t.Errorf("expected Run and Refresh buttons to be rendered, got:\n%s", rendered)
	}
}
