package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/lsp"
)

func TestProblemsPanel_RefreshAndCounts(t *testing.T) {
	panel := NewProblemsPanel()
	if panel.Open {
		t.Errorf("expected panel to start closed")
	}

	docDiag := map[string]map[int][]lsp.Diagnostic{
		"file:///workspace/main.go": {
			10: []lsp.Diagnostic{
				{
					Range:    lsp.Range{Start: lsp.Position{Line: 10, Character: 4}},
					Severity: lsp.SeverityError,
					Message:  "undefined: foo",
				},
			},
			15: []lsp.Diagnostic{
				{
					Range:    lsp.Range{Start: lsp.Position{Line: 15, Character: 2}},
					Severity: lsp.SeverityWarning,
					Message:  "variable bar declared and not used",
				},
			},
		},
	}

	panel.Refresh(docDiag)

	if len(panel.Items) != 2 {
		t.Fatalf("expected 2 problem items, got %d", len(panel.Items))
	}

	if panel.ErrorCount() != 1 {
		t.Errorf("expected 1 error, got %d", panel.ErrorCount())
	}
	if panel.WarningCount() != 1 {
		t.Errorf("expected 1 warning, got %d", panel.WarningCount())
	}

	// Error must be sorted first
	if panel.Items[0].Severity != lsp.SeverityError {
		t.Errorf("expected first item to be error, got severity %d", panel.Items[0].Severity)
	}

	// Test Jump
	jumped := false
	panel.OnJump = func(fp string, line, col int) {
		jumped = true
		if line != 10 || col != 4 {
			t.Errorf("unexpected jump destination: %d:%d", line, col)
		}
	}

	panel.Open = true
	panel.SelectedIndex = 0
	_ = panel.HandleKey(MakeKeyEnter())

	if !jumped {
		t.Errorf("expected Enter to trigger OnJump")
	}
}

func MakeKeyEnter() input.Key {
	return input.Key{Type: input.KeyEnter}
}
