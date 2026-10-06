package tui

import (

	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core"
	cbuf "tahr/internal/core/buffer"
)

func TestAppModel_BracketedPaste(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	pastedText := "func test() {\n    return 42\n}"
	updated, _ := model.Update(tea.PasteMsg{Text: pastedText})
	m := updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	l0, _ := doc.Buffer.GetLine(0)
	if string(l0) != "func test() {\n" {
		t.Fatalf("unexpected line 0: %q", string(l0))
	}
}

func TestAppModel_SyntaxHighlightingInView(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)
	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"

	// Paste Go code
	code := "package main\nfunc hello() {\n return 42\n}"
	updated, _ := model.Update(tea.PasteMsg{Text: code})
	m := updated.(*AppModel)

	// Render into frame
	frameBuf := buffer.NewBuffer(80, 24)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	// Row 0 is Header, Row 1 is TabBar, Row 2 is Editor line 0.
	// Columns: 0..2 is Left Activity Strip, then gutter (width 5), then column 8 is first text character ('p' of "package")
	gutterW := m.gutterWidth()
	stripLeftW := 3
	cellP := frameBuf.Cell(stripLeftW+gutterW, 2)
	if cellP == nil || cellP.Rune != 'p' {
		t.Fatalf("expected 'p' at text start, got %v", cellP)
	}

	expectedKwColor := m.theme.Keyword
	if cellP.Fg != expectedKwColor {
		t.Errorf("expected syntax color for keyword (0x%x), got 0x%x", expectedKwColor, cellP.Fg)
	}
}

func TestAppModel_ContextualCompletionPopup(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Write code with custom function name
	code := "func calculateQuantumEntropy() int {\n return 100\n}"
	updated, _ := model.Update(tea.PasteMsg{Text: code})
	m := updated.(*AppModel)

	// Trigger completion popup
	m.triggerCompletionPopup()
	if !m.popupVisible {
		t.Fatalf("expected completion popup to be visible")
	}

	found := false
	for _, item := range m.popupItems {
		if item == "calculateQuantumEntropy" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'calculateQuantumEntropy' in popup items, got: %v", m.popupItems)
	}
}

func TestAppModel_SpaceBarTyping(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// 1. Editor space bar typing
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'a'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeySpace}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'b'}})

	doc := m.eng.ActiveDocument()
	text, _ := doc.Buffer.GetText()
	if string(text) != "a b" {
		t.Fatalf("expected 'a b' in buffer, got %q", string(text))
	}

	// 2. Omnibar space bar typing
	m.openOmnibar("files")
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'm'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeySpace}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'g'}})
	if m.omnibarQuery != "m g" {
		t.Fatalf("expected omnibar query 'm g', got %q", m.omnibarQuery)
	}
}

func TestAppModel_FindInDocument(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()

	// Put multiple lines
	code := "first line\nsecond search_target line\nthird line\nfourth search_target line\n"
	_, _ = m.Update(tea.PasteMsg{Text: code})

	// Open Omnibar in find mode
	m.openOmnibar("find")
	if !m.omnibarOpen || m.omnibarMode != "find" {
		t.Fatalf("expected Omnibar to open in 'find' mode, got %v (%s)", m.omnibarOpen, m.omnibarMode)
	}

	// Type search query
	var updated tea.Model
	for _, r := range "search_target" {
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
		m = updated.(*AppModel)
	}

	if len(m.omnibarItems) != 2 {
		t.Fatalf("expected 2 matching lines, got %d: %v", len(m.omnibarItems), m.omnibarItems)
	}

	// Press Enter to jump to the first match
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	m = updated.(*AppModel)

	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 || sels[0].Head.Line != 1 {
		t.Errorf("expected cursor line 1 after selecting first match, got %v", sels)
	}
}

func TestAppModel_GotoLine(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()

	// Add 50 lines
	var sb strings.Builder
	for i := 1; i <= 50; i++ {
		sb.WriteString("line\n")
	}
	_, _ = m.Update(tea.PasteMsg{Text: sb.String()})

	// Press Ctrl+G
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'g', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	if !m.omnibarOpen || m.omnibarMode != "goto" {
		t.Fatalf("expected Omnibar in 'goto' mode")
	}

	// Type line number '2', '5' -> line 25
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '2'}})
	m = updated.(*AppModel)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '5'}})
	m = updated.(*AppModel)

	// Press Enter
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	m = updated.(*AppModel)

	sels := doc.Buffer.GetSelections()
	// Line 25 (1-based) is index 24 (0-based)
	if len(sels) == 0 || sels[0].Head.Line != 24 {
		t.Errorf("expected cursor at line 24, got %v", sels)
	}
}

func TestAppModel_ClipboardOperations(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()

	// Paste text
	_, _ = m.Update(tea.PasteMsg{Text: "clipboard_secret_token"})

	// Select all via Ctrl+A
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'a', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	// Copy via Ctrl+C
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'c', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	if m.clipboardText != "clipboard_secret_token" {
		t.Fatalf("expected clipboard text 'clipboard_secret_token', got %q", m.clipboardText)
	}

	// Move cursor to end
	doc.Buffer.SetSelections([]cbuf.Selection{{
		Anchor:	cbuf.Position{Line: 0, Column: 22, Byte: 22},
		Head:	cbuf.Position{Line: 0, Column: 22, Byte: 22},
	}})

	// Paste via Ctrl+V
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'v', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	txt, _ := doc.Buffer.GetText()
	expected := "clipboard_secret_tokenclipboard_secret_token"
	if string(txt) != expected {
		t.Fatalf("expected %q, got %q", expected, string(txt))
	}

	// Select all and Cut via Ctrl+X
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'a', Mod: input.ModCtrl}})
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'x', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	txtAfterCut, _ := doc.Buffer.GetText()
	if string(txtAfterCut) != "" {
		t.Fatalf("expected empty buffer after cut, got %q", string(txtAfterCut))
	}
	if m.clipboardText != expected {
		t.Fatalf("expected cut content in clipboard, got %q", m.clipboardText)
	}
}

func TestAppModel_GhostTextAndTabAccept(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"

	// Type "fmt.Pr"
	for _, r := range "fmt.Pr" {
		_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
	}

	m.updateGhostText()
	if m.ghostText == "" {
		t.Fatalf("expected ghost text prediction for prefix 'Pr', got empty")
	}
	if !strings.HasPrefix("Println", m.ghostPrefix+m.ghostText) && !strings.HasPrefix("Printf", m.ghostPrefix+m.ghostText) {
		t.Errorf("unexpected ghost text %q with prefix %q", m.ghostText, m.ghostPrefix)
	}

	// Press Tab to accept ghost text
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyTab}})
	m = updated.(*AppModel)

	text, _ := doc.Buffer.GetText()
	textStr := string(text)
	if !strings.Contains(textStr, "fmt.Print") {
		t.Fatalf("expected buffer to contain completed text, got %q", textStr)
	}
	if m.ghostText != "" {
		t.Errorf("expected ghost text to be cleared after Tab accept")
	}
}

func TestAppModel_AutocompletionPrefixReplacement(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"

	// Type "fmt.Prin"
	for _, r := range "fmt.Prin" {
		_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
	}

	// Insert completion "fmt.Println"
	m.insertCompletion("fmt.Println")

	text, _ := doc.Buffer.GetText()
	textStr := string(text)
	// Must NOT contain duplicate "PrinPrintln"
	if strings.Contains(textStr, "PrinPrintln") {
		t.Fatalf("autocompletion caused duplicate text: %q", textStr)
	}
	if textStr != "fmt.Println" {
		t.Fatalf("expected 'fmt.Println', got %q", textStr)
	}
}

func TestAppModel_AutocompletePrefixStripping(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Case 1: Cursor right after "fmt."
	updated, _ := model.Update(tea.PasteMsg{Text: "fmt."})
	m := updated.(*AppModel)

	m.insertCompletion("fmt.Println - func(a ...any)")
	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	lineBytes, _ := doc.Buffer.GetLine(0)
	lineStr := string(lineBytes)
	if lineStr != "fmt.Println" {
		t.Errorf("expected 'fmt.Println', got %q", lineStr)
	}

	// Case 2: Cursor after "fmt.Pr"
	eng2 := core.NewEngine()
	model2 := NewAppModel(eng2)
	updated2, _ := model2.Update(tea.PasteMsg{Text: "fmt.Pr"})
	m2 := updated2.(*AppModel)

	m2.insertCompletion("fmt.Println - func(a ...any)")
	doc2 := m2.eng.ActiveDocument()
	if doc2 == nil {
		t.Fatal("expected active document")
	}
	lineBytes2, _ := doc2.Buffer.GetLine(0)
	lineStr2 := string(lineBytes2)
	if lineStr2 != "fmt.Println" {
		t.Errorf("expected 'fmt.Println', got %q", lineStr2)
	}
}
