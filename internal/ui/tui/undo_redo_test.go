package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
)

func TestUndoRedo_KeyboardShortcuts(t *testing.T) {
	testCases := []struct {
		name       string
		undoKey    input.Key
		redoKey    input.Key
		expectUndo bool
		expectRedo bool
	}{
		{
			name:       "Standard English Ctrl+Z / Ctrl+Y",
			undoKey:    input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl},
			redoKey:    input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl},
			expectUndo: true,
			expectRedo: true,
		},
		{
			name:       "Standard English Ctrl+Z / Ctrl+Shift+Z",
			undoKey:    input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl},
			redoKey:    input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl | input.ModShift},
			expectUndo: true,
			expectRedo: true,
		},
		{
			name:       "Russian Cyrillic Ctrl+Я / Ctrl+Н",
			undoKey:    input.Key{Type: input.KeyRune, Rune: 'я', Mod: input.ModCtrl},
			redoKey:    input.Key{Type: input.KeyRune, Rune: 'н', Mod: input.ModCtrl},
			expectUndo: true,
			expectRedo: true,
		},
		{
			name:       "Russian Cyrillic Ctrl+Я / Ctrl+Shift+Я",
			undoKey:    input.Key{Type: input.KeyRune, Rune: 'я', Mod: input.ModCtrl},
			redoKey:    input.Key{Type: input.KeyRune, Rune: 'я', Mod: input.ModCtrl | input.ModShift},
			expectUndo: true,
			expectRedo: true,
		},
		{
			name:       "Legacy Control Bytes 0x1A / 0x19",
			undoKey:    input.Key{Type: input.KeyRune, Rune: 26, Mod: input.ModCtrl},
			redoKey:    input.Key{Type: input.KeyRune, Rune: 25, Mod: input.ModCtrl},
			expectUndo: true,
			expectRedo: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng := core.NewEngine()
			model := NewAppModel(eng)

			// Type character 'X'
			updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'X'}})
			m := updated.(*AppModel)

			doc := m.eng.ActiveDocument()
			txt, _ := doc.Buffer.GetText()
			if string(txt) != "X" {
				t.Fatalf("expected initial text 'X', got %q", string(txt))
			}

			// Perform Undo
			updated, _ = m.Update(tea.KeyMsg{Key: tc.undoKey})
			m = updated.(*AppModel)

			txt, _ = doc.Buffer.GetText()
			if string(txt) != "" {
				t.Fatalf("expected empty text after undo, got %q", string(txt))
			}
			if m.statusMessage != "Undo" {
				t.Fatalf("expected status 'Undo', got %q", m.statusMessage)
			}

			// Perform Redo
			updated, _ = m.Update(tea.KeyMsg{Key: tc.redoKey})
			m = updated.(*AppModel)

			txt, _ = doc.Buffer.GetText()
			if string(txt) != "X" {
				t.Fatalf("expected restored text 'X' after redo, got %q", string(txt))
			}
			if m.statusMessage != "Redo" {
				t.Fatalf("expected status 'Redo', got %q", m.statusMessage)
			}
		})
	}
}

func TestUndoRedo_BoundaryConditions(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Undo on empty buffer must not panic or error
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m := updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("expected empty buffer, got %q", string(txt))
	}
	if m.statusMessage != "Already at oldest change" {
		t.Fatalf("expected 'Already at oldest change', got %q", m.statusMessage)
	}

	// Redo on empty stack must not panic
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	txt, _ = doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("expected empty buffer, got %q", string(txt))
	}
	if m.statusMessage != "Already at newest change" {
		t.Fatalf("expected 'Already at newest change', got %q", m.statusMessage)
	}
}

func TestUndoRedo_TerminalDrawerIsolation(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Type in editor
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'H'}})
	m := updated.(*AppModel)

	// Open terminal drawer (F4)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)

	// Case 1: Terminal OPEN and UNFOCUSED (Esc back to editor)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	m = updated.(*AppModel)

	if !m.terminal.Open || m.terminalFocused {
		t.Fatal("expected terminal open but unfocused")
	}

	// Undo should affect the editor
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("expected editor undo when terminal unfocused, got %q", string(txt))
	}
}

func TestUndoRedo_EmptyAndNilEngine(t *testing.T) {
	// Empty engine without documents
	emptyEng := &core.Engine{}
	model := NewAppModel(emptyEng)

	// Press Ctrl+Z without active document
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m := updated.(*AppModel)
	if m.statusMessage != "No active document" {
		t.Fatalf("expected status 'No active document', got %q", m.statusMessage)
	}

	// Press Ctrl+Y without active document
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	if m.statusMessage != "No active document" {
		t.Fatalf("expected status 'No active document', got %q", m.statusMessage)
	}

	// Nil engine model must not panic
	nilModel := NewAppModel(nil)
	nilModel.performUndo()
	nilModel.performRedo()
}
