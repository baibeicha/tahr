package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
)

// newTestApp initializes an AppModel with a fresh engine and standard 100x30 dimensions.
func newTestApp(t testing.TB) *AppModel {
	t.Helper()
	eng := core.NewEngine()
	model := NewAppModel(eng)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated.(*AppModel)
}

// sendKey dispatches a single key message to the AppModel and returns the updated model.
func sendKey(m *AppModel, k input.Key) *AppModel {
	updated, _ := m.Update(tea.KeyMsg{Key: k})
	return updated.(*AppModel)
}

// sendRune dispatches a rune key message to the AppModel and returns the updated model.
func sendRune(m *AppModel, r rune) *AppModel {
	return sendKey(m, input.Key{Type: input.KeyRune, Rune: r})
}

// sendPaste dispatches a paste message to the AppModel and returns the updated model.
func sendPaste(m *AppModel, text string) *AppModel {
	updated, _ := m.Update(tea.PasteMsg{Text: text})
	return updated.(*AppModel)
}
