package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
)

func TestTerminalDrawer_FocusAndBlur(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Initially drawer is closed
	if model.terminal.Open {
		t.Fatal("expected terminal to start closed")
	}

	// Press F4 to open drawer
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m := updated.(*AppModel)

	if !m.terminal.Open {
		t.Fatal("expected terminal to open on F4")
	}
	if !m.terminalFocused {
		t.Fatal("expected terminal to be focused upon opening")
	}

	// Press Esc to return focus to editor without closing drawer
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	m = updated.(*AppModel)

	if !m.terminal.Open {
		t.Fatal("expected terminal to remain open")
	}
	if m.terminalFocused {
		t.Fatal("expected terminal to lose focus on Esc")
	}

	// Press F4 again to close drawer
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)

	if m.terminal.Open {
		t.Fatal("expected terminal to close on second F4")
	}
}

func TestTerminalDrawer_KeyIsolation(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Write text into editor
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'E'}})
	m := updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "E" {
		t.Fatalf("expected editor text 'E', got %q", string(txt))
	}

	// Open terminal drawer
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)

	// Type characters into terminal prompt
	for _, ch := range "ls -la" {
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: ch}})
		m = updated.(*AppModel)
	}

	if m.terminal.InputText != "ls -la" {
		t.Fatalf("expected terminal input 'ls -la', got %q", m.terminal.InputText)
	}

	// Editor buffer must remain unchanged
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "E" {
		t.Fatalf("editor buffer leaked input while terminal focused: %q", string(txt))
	}

	// Press Ctrl+Z with non-empty terminal input: must clear input and NOT undo editor
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	if m.terminal.InputText != "" {
		t.Fatalf("expected empty terminal prompt after Ctrl+Z, got %q", m.terminal.InputText)
	}
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "E" {
		t.Fatalf("editor buffer was modified when terminal prompt was non-empty: %q", string(txt))
	}

	// Press Ctrl+Z again with empty terminal input: should cascade to editor undo
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	txt, _ = doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("expected editor undo after second Ctrl+Z with empty prompt, got %q", string(txt))
	}
}
