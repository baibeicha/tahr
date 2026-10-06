package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

func TestJupyterPanel_EmptyStateAndButtons(t *testing.T) {
	p := NewJupyterPanel("")
	if !p.Open {
		t.Fatalf("expected JupyterPanel to start with Open=true")
	}

	buf := buffer.NewBuffer(34, 20)
	theme := ui.CatppuccinMocha()
	p.Render(buf, 0, 0, 34, 20, &theme)

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

	// Verify Kernel status is present
	if !strings.Contains(rendered, "Kernel: Python 3") {
		t.Errorf("expected kernel status, got:\n%s", rendered)
	}

	// Verify informative empty state
	emptyTitle := i18n.T("jupyter.empty_title")
	if !strings.Contains(rendered, emptyTitle) {
		t.Errorf("expected empty state title %q, got:\n%s", emptyTitle, rendered)
	}
	if !strings.Contains(rendered, "+ Code") {
		t.Errorf("expected instructions mentioning '+ Code', got:\n%s", rendered)
	}

	// Verify all buttons rendered without brackets
	if strings.Contains(rendered, "[ Run Cell ]") || strings.Contains(rendered, "[ + Code ]") {
		t.Errorf("found bracketed buttons in rendered output:\n%s", rendered)
	}

	// Verify wrapped buttons registered hits
	if len(p.ButtonHits) < 6 {
		t.Errorf("expected at least 6 action buttons registered, got %d", len(p.ButtonHits))
	}
}
