package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core"
	cbuf "tahr/internal/core/buffer"
)

// Helper: generate test key event for Undo/Redo variations.
func makeUndoRedoKey(kind string) input.Key {
	switch kind {
	case "ctrl_z":
		return input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}
	case "ctrl_y":
		return input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}
	case "ctrl_shift_z":
		return input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl | input.ModShift}
	case "ctrl_cyrillic_ya":
		return input.Key{Type: input.KeyRune, Rune: 'я', Mod: input.ModCtrl}
	case "ctrl_cyrillic_en":
		return input.Key{Type: input.KeyRune, Rune: 'н', Mod: input.ModCtrl}
	case "ctrl_shift_cyrillic_ya":
		return input.Key{Type: input.KeyRune, Rune: 'я', Mod: input.ModCtrl | input.ModShift}
	case "legacy_0x1A":
		return input.Key{Type: input.KeyRune, Rune: 26, Mod: input.ModCtrl}
	case "legacy_0x19":
		return input.Key{Type: input.KeyRune, Rune: 25, Mod: input.ModCtrl}
	default:
		panic("unknown kind: " + kind)
	}
}

func isRedoKind(kind string) bool {
	return kind == "ctrl_y" || kind == "ctrl_shift_z" || kind == "ctrl_cyrillic_en" ||
		kind == "ctrl_shift_cyrillic_ya" || kind == "legacy_0x19"
}

// ---------------------------------------------------------------------------
// 1. Systematic Matrix: Terminal Drawer Key Isolation across all 3 States
// ---------------------------------------------------------------------------

func TestAdversarial_TerminalDrawer_KeyIsolation_Matrix(t *testing.T) {
	undoRedoVariations := []string{
		"ctrl_z",
		"ctrl_y",
		"ctrl_shift_z",
		"ctrl_cyrillic_ya",
		"ctrl_cyrillic_en",
		"ctrl_shift_cyrillic_ya",
		"legacy_0x1A",
		"legacy_0x19",
	}

	for _, kind := range undoRedoVariations {
		t.Run("Variation_"+kind, func(t *testing.T) {
			// ------------------------------------------------------------------
			// State 1: Drawer OPEN and UNFOCUSED (m.terminal.Open=true, m.terminalFocused=false)
			// ------------------------------------------------------------------
			t.Run("Open_Unfocused_EmptyPrompt", func(t *testing.T) {
				eng := core.NewEngine()
				model := NewAppModel(eng)

				// Type "edit1" into editor
				updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'A'}})
				m := updated.(*AppModel)

				// Open terminal with F4 (initially focused)
				updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
				m = updated.(*AppModel)

				// Unfocus terminal via Esc
				updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
				m = updated.(*AppModel)

				if !m.terminal.Open {
					t.Fatal("expected terminal drawer to be open")
				}
				if m.terminalFocused {
					t.Fatal("expected terminal to be unfocused after Esc")
				}
				if m.terminal.InputText != "" {
					t.Fatalf("expected empty terminal prompt, got %q", m.terminal.InputText)
				}

				key := makeUndoRedoKey(kind)
				if isRedoKind(kind) {
					// To test Redo, first perform an undo so there is a redo stack
					m.performUndo()
					doc := m.eng.ActiveDocument()
					txt, _ := doc.Buffer.GetText()
					if string(txt) != "" {
						t.Fatalf("expected empty buffer after initial undo, got %q", string(txt))
					}
				}

				// Send the key event
				updated, _ = m.Update(tea.KeyMsg{Key: key})
				m = updated.(*AppModel)

				doc := m.eng.ActiveDocument()
				txt, _ := doc.Buffer.GetText()

				if isRedoKind(kind) {
					if string(txt) != "A" {
						t.Fatalf("[%s] expected editor to redo to 'A', got %q", kind, string(txt))
					}
					if m.statusMessage != "Redo" {
						t.Fatalf("[%s] expected status 'Redo', got %q", kind, m.statusMessage)
					}
				} else {
					if string(txt) != "" {
						t.Fatalf("[%s] expected editor to undo to empty, got %q", kind, string(txt))
					}
					if m.statusMessage != "Undo" {
						t.Fatalf("[%s] expected status 'Undo', got %q", kind, m.statusMessage)
					}
				}
				// Terminal prompt must remain empty
				if m.terminal.InputText != "" {
					t.Fatalf("expected terminal prompt to remain empty, got %q", m.terminal.InputText)
				}
			})

			t.Run("Open_Unfocused_NonEmptyPrompt_PromptPreserved", func(t *testing.T) {
				eng := core.NewEngine()
				model := NewAppModel(eng)

				// Type "Hello" in editor
				updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'H'}})
				m := updated.(*AppModel)

				// Open terminal with F4
				updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
				m = updated.(*AppModel)

				// Set terminal input to active command
				m.terminal.mu.Lock()
				m.terminal.InputText = "cargo build --release"
				m.terminal.CursorPos = len("cargo build --release")
				m.terminal.mu.Unlock()

				// Unfocus terminal via Esc
				updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
				m = updated.(*AppModel)

				if !m.terminal.Open || m.terminalFocused {
					t.Fatal("expected terminal open and unfocused")
				}

				key := makeUndoRedoKey(kind)
				if isRedoKind(kind) {
					m.performUndo()
				}

				// Send key
				updated, _ = m.Update(tea.KeyMsg{Key: key})
				m = updated.(*AppModel)

				// Editor must have processed undo/redo
				doc := m.eng.ActiveDocument()
				txt, _ := doc.Buffer.GetText()
				if isRedoKind(kind) {
					if string(txt) != "H" {
						t.Fatalf("[%s] expected editor to redo to 'H', got %q", kind, string(txt))
					}
				} else {
					if string(txt) != "" {
						t.Fatalf("[%s] expected editor to undo to '', got %q", kind, string(txt))
					}
				}

				// CRITICAL ADVERSARIAL ORACLE:
				// Terminal's InputText MUST NOT have been cleared or modified while unfocused!
				m.terminal.mu.Lock()
				termPrompt := m.terminal.InputText
				m.terminal.mu.Unlock()
				if termPrompt != "cargo build --release" {
					t.Fatalf("[%s] terminal prompt was corrupted or wiped while unfocused: got %q, want 'cargo build --release'", kind, termPrompt)
				}
			})

			// ------------------------------------------------------------------
			// State 2: Drawer OPEN and FOCUSED with EMPTY PROMPT
			// ------------------------------------------------------------------
			t.Run("Open_Focused_EmptyPrompt_Passthrough", func(t *testing.T) {
				eng := core.NewEngine()
				model := NewAppModel(eng)

				// Type "B" in editor
				updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'B'}})
				m := updated.(*AppModel)

				// Open terminal with F4 (focused, empty prompt)
				updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
				m = updated.(*AppModel)

				if !m.terminal.Open || !m.terminalFocused {
					t.Fatal("expected terminal open and focused")
				}
				if m.terminal.IsActivelyTyping() {
					t.Fatal("expected empty terminal prompt")
				}

				key := makeUndoRedoKey(kind)
				if isRedoKind(kind) {
					m.performUndo()
				}

				// Send key
				updated, _ = m.Update(tea.KeyMsg{Key: key})
				m = updated.(*AppModel)

				doc := m.eng.ActiveDocument()
				txt, _ := doc.Buffer.GetText()

				// Because prompt is empty, key MUST pass through to editor!
				if isRedoKind(kind) {
					if string(txt) != "B" {
						t.Fatalf("[%s] expected editor redo passthrough, got %q", kind, string(txt))
					}
					if m.statusMessage != "Redo" {
						t.Fatalf("[%s] expected status 'Redo', got %q", kind, m.statusMessage)
					}
				} else {
					if string(txt) != "" {
						t.Fatalf("[%s] expected editor undo passthrough, got %q", kind, string(txt))
					}
					if m.statusMessage != "Undo" {
						t.Fatalf("[%s] expected status 'Undo', got %q", kind, m.statusMessage)
					}
				}
			})

			// ------------------------------------------------------------------
			// State 3: Drawer OPEN and FOCUSED with NON-EMPTY PROMPT
			// ------------------------------------------------------------------
			t.Run("Open_Focused_NonEmptyPrompt_Consumed", func(t *testing.T) {
				eng := core.NewEngine()
				model := NewAppModel(eng)

				// Type "KeepMe" in editor
				var updated tea.Model
				for _, r := range "KeepMe" {
					updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
					model = updated.(*AppModel)
				}
				m := model

				doc := m.eng.ActiveDocument()
				txt, _ := doc.Buffer.GetText()
				if string(txt) != "KeepMe" {
					t.Fatalf("expected 'KeepMe', got %q", string(txt))
				}

				// Open terminal with F4
				updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
				m = updated.(*AppModel)

				// Populate terminal prompt
				m.terminal.mu.Lock()
				m.terminal.InputText = "pytest -v"
				m.terminal.CursorPos = 9
				m.terminal.mu.Unlock()

				if !m.terminal.IsActivelyTyping() {
					t.Fatal("expected actively typing in terminal")
				}

				m.statusMessage = "Idle"

				key := makeUndoRedoKey(kind)

				// Send key
				updated, _ = m.Update(tea.KeyMsg{Key: key})
				m = updated.(*AppModel)

				// CRITICAL ADVERSARIAL ORACLE 1:
				// Terminal prompt MUST be cleared
				m.terminal.mu.Lock()
				promptAfter := m.terminal.InputText
				m.terminal.mu.Unlock()
				if promptAfter != "" {
					t.Fatalf("[%s] expected terminal prompt to be cleared, got %q", kind, promptAfter)
				}

				// CRITICAL ADVERSARIAL ORACLE 2:
				// Editor buffer MUST NOT be modified! "KeepMe" must remain intact!
				txtAfter, _ := doc.Buffer.GetText()
				if string(txtAfter) != "KeepMe" {
					t.Fatalf("[%s] CRITICAL REGRESSION: editor buffer was modified when terminal was typing! got %q, want 'KeepMe'", kind, string(txtAfter))
				}

				// CRITICAL ADVERSARIAL ORACLE 3:
				// Status message must NOT indicate editor undo/redo was performed
				if m.statusMessage == "Undo" || m.statusMessage == "Redo" {
					t.Fatalf("[%s] editor status message was set to %q when typing in terminal", kind, m.statusMessage)
				}
			})
		})
	}
}

// ---------------------------------------------------------------------------
// 2. Non-Undo Keys Isolation in Terminal Drawer
// ---------------------------------------------------------------------------

func TestAdversarial_TerminalDrawer_NonUndoKeys_Isolation(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Populate editor with initial line
	updated, _ := model.Update(tea.PasteMsg{Text: "initial editor line\n"})
	m := updated.(*AppModel)

	// Open terminal
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)

	if !m.terminal.Open || !m.terminalFocused {
		t.Fatal("expected terminal open & focused")
	}

	// Type characters 'g', 'i', 't'
	for _, r := range "git" {
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
		m = updated.(*AppModel)
	}

	// Verify terminal received 'git'
	m.terminal.mu.Lock()
	p := m.terminal.InputText
	m.terminal.mu.Unlock()
	if p != "git" {
		t.Fatalf("expected terminal prompt 'git', got %q", p)
	}

	// Verify editor buffer was untouched
	doc := m.eng.ActiveDocument()
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "initial editor line\n" {
		t.Fatalf("keystrokes leaked into editor buffer: got %q", string(txt))
	}

	// Backspace in terminal
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyBackspace}})
	m = updated.(*AppModel)

	m.terminal.mu.Lock()
	p = m.terminal.InputText
	m.terminal.mu.Unlock()
	if p != "gi" {
		t.Fatalf("expected terminal prompt 'gi', got %q", p)
	}

	txt, _ = doc.Buffer.GetText()
	if string(txt) != "initial editor line\n" {
		t.Fatalf("backspace leaked into editor: got %q", string(txt))
	}

	// Press Esc to unfocus terminal
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	m = updated.(*AppModel)

	if !m.terminal.Open {
		t.Fatal("terminal should still be open")
	}
	if m.terminalFocused {
		t.Fatal("terminal should now be unfocused")
	}

	// Now type 'Z' in editor
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'Z'}})
	m = updated.(*AppModel)

	txt, _ = doc.Buffer.GetText()
	if !strings.Contains(string(txt), "Z") {
		t.Fatalf("expected 'Z' to be typed into editor when unfocused, got %q", string(txt))
	}

	// Terminal prompt 'gi' must still be intact
	m.terminal.mu.Lock()
	p = m.terminal.InputText
	m.terminal.mu.Unlock()
	if p != "gi" {
		t.Fatalf("terminal prompt corrupted by editor typing: got %q", p)
	}
}

// ---------------------------------------------------------------------------
// 3. Interleaved Transitions Stress Test
// ---------------------------------------------------------------------------

func TestAdversarial_TerminalDrawer_StateTransitions_InterleavedStress(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)
	var updated tea.Model

	// Put 2 distinct word edits into editor: "first " and "second"
	for _, r := range "first " {
		updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
		model = updated.(*AppModel)
	}
	for _, r := range "second" {
		updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
		model = updated.(*AppModel)
	}
	m := model

	doc := m.eng.ActiveDocument()
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "first second" {
		t.Fatalf("expected 'first second', got %q", string(txt))
	}

	// Open terminal (F4) -> focused, empty prompt
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)

	// Step 1: Ctrl+Z with empty terminal prompt -> undoes "second", leaves "first "
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "first " {
		t.Fatalf("Step 1 failed: expected 'first ', got %q", string(txt))
	}

	// Step 2: Type in terminal "cat"
	for _, r := range "cat" {
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
		m = updated.(*AppModel)
	}
	if m.terminal.InputText != "cat" {
		t.Fatalf("Step 2 failed: expected 'cat', got %q", m.terminal.InputText)
	}

	// Step 3: Ctrl+Z with non-empty prompt -> consumes "cat", clears prompt, editor STILL "first "
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	if m.terminal.InputText != "" {
		t.Fatalf("Step 3 failed: expected prompt cleared, got %q", m.terminal.InputText)
	}
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "first " {
		t.Fatalf("Step 3 failed: editor buffer changed when prompt was non-empty! got %q", string(txt))
	}

	// Step 4: Immediate second Ctrl+Z -> now prompt is empty, passthrough undoes whitespace, leaves "first"
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "first" {
		t.Fatalf("Step 4 failed: expected 'first', got %q", string(txt))
	}

	// Step 4b: Third Ctrl+Z -> undoes word "first", leaves ""
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("Step 4b failed: expected '', got %q", string(txt))
	}

	// Step 5: Redo via Ctrl+Y -> passthrough redoes to "first"
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "first" {
		t.Fatalf("Step 5 failed: expected 'first', got %q", string(txt))
	}

	// Step 6: Unfocus terminal (Esc) -> type in editor 'X' -> "firstX"
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	m = updated.(*AppModel)
	if m.terminalFocused {
		t.Fatal("expected unfocused terminal")
	}
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'X'}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "firstX" {
		t.Fatalf("Step 6 failed: expected 'firstX', got %q", string(txt))
	}

	// Step 7: Undo 'X' in editor via Russian Ctrl+я -> leaves "first"
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'я', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "first" {
		t.Fatalf("Step 7 failed: expected 'first', got %q", string(txt))
	}

	// Step 8: Close terminal (F4)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)
	if m.terminal.Open {
		t.Fatal("expected terminal to be closed")
	}
}

// ---------------------------------------------------------------------------
// 4. Viewport Offset Synchronization Under Rapid Animation Ticks (stepSmoothScroll)
// ---------------------------------------------------------------------------

func TestAdversarial_SmoothScroll_RapidAnimationTicks_Lockdown(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Set window size 100x30
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)

	// Create 1000 lines of text
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("line %04d: func TestPayload%d() { return }", i, i))
	}
	bigDoc := strings.Join(lines, "\n")
	updated, _ = m.Update(tea.PasteMsg{Text: bigDoc})
	m = updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	if doc.Buffer.TotalLines() < 1000 {
		t.Fatalf("expected >= 1000 lines, got %d", doc.Buffer.TotalLines())
	}

	// Place cursor at line 15, Column 0
	off15, _ := doc.Buffer.ByteOffsetForLine(15)
	doc.Buffer.SetSelections([]cbuf.Selection{
		{Anchor: cbuf.Position{Line: 15, Column: 0, Byte: off15}, Head: cbuf.Position{Line: 15, Column: 0, Byte: off15}},
	})
	// Insert edit at line 15
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '#'}})
	m = updated.(*AppModel)

	// Now simulate user scrolling deep down to line 850
	m.viewportY = 850
	ap := m.splits.ActivePane()
	if ap == nil {
		t.Fatal("expected active pane")
	}
	ap.ViewportY = 850
	ap.TargetViewportY = 850
	ap.SmoothScrollY = 850
	ap.ScrollVelocity = 12.5

	// Trigger Undo: reverts edit at line 15, cursor restored to line 15
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	ap = m.splits.ActivePane()
	initialViewportY := m.viewportY
	if initialViewportY > 15 {
		t.Fatalf("viewportY did not snap near restored cursor (line 15): got %d", initialViewportY)
	}

	// Check pre-condition: offsets synchronized
	if ap.TargetViewportY != float64(initialViewportY) {
		t.Fatalf("expected TargetViewportY == %d, got %f", initialViewportY, ap.TargetViewportY)
	}
	if ap.SmoothScrollY != float64(initialViewportY) {
		t.Fatalf("expected SmoothScrollY == %d, got %f", initialViewportY, ap.SmoothScrollY)
	}
	if ap.ScrollVelocity != 0 {
		t.Fatalf("expected ScrollVelocity == 0, got %f", ap.ScrollVelocity)
	}

	// ADVERSARIAL STRESS: Run 500 consecutive rapid animation ticks
	// Under NO circumstances should stepSmoothScroll pull the viewport away towards 850!
	for tick := 1; tick <= 500; tick++ {
		// Run via tea.Update with animTickMsg
		updated, cmd := m.Update(animTickMsg{})
		m = updated.(*AppModel)

		// Also verify direct stepSmoothScroll returns false (no redraw needed because diff == 0)
		needsRedraw := m.stepSmoothScroll()
		if needsRedraw {
			t.Fatalf("tick %d: stepSmoothScroll requested unnecessary redraw (offsets drifted!)", tick)
		}

		if cmd != nil {
			t.Fatalf("tick %d: animTickMsg scheduled another animation command even though offsets are locked", tick)
		}

		if m.viewportY != initialViewportY {
			t.Fatalf("tick %d: viewportY drifted! got %d, expected %d", tick, m.viewportY, initialViewportY)
		}
		if ap.ViewportY != initialViewportY {
			t.Fatalf("tick %d: active pane ViewportY drifted! got %d, expected %d", tick, ap.ViewportY, initialViewportY)
		}
		if ap.TargetViewportY != float64(initialViewportY) {
			t.Fatalf("tick %d: TargetViewportY drifted! got %f, expected %d", tick, ap.TargetViewportY, initialViewportY)
		}
		if ap.SmoothScrollY != float64(initialViewportY) {
			t.Fatalf("tick %d: SmoothScrollY drifted! got %f, expected %d", tick, ap.SmoothScrollY, initialViewportY)
		}

		// Verify restored cursor at line 15 remains within visible screen
		cursorLine := doc.Buffer.GetSelections()[0].Head.Line
		if cursorLine < m.viewportY || cursorLine >= m.viewportY+25 {
			t.Fatalf("tick %d: restored cursor (line %d) pulled off screen by smooth scroll! viewportY=%d",
				tick, cursorLine, m.viewportY)
		}
	}
}

// ---------------------------------------------------------------------------
// 5. Fuzz Stress: Edits, Viewport Scrambling, Undos, and Animation Tick Storm
// ---------------------------------------------------------------------------

func TestAdversarial_SmoothScroll_RapidEditsAndTicksFuzz(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)

	// Populate 500 lines
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf("line %03d", i))
	}
	updated, _ = m.Update(tea.PasteMsg{Text: strings.Join(lines, "\n")})
	m = updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	rng := rand.New(rand.NewSource(42))

	targetLines := []int{2, 10, 45, 120, 280, 390, 480}

	for cycle := 0; cycle < 50; cycle++ {
		// Pick target line
		lineIdx := targetLines[rng.Intn(len(targetLines))]
		off, err := doc.Buffer.ByteOffsetForLine(lineIdx)
		if err != nil {
			continue
		}

		// Move cursor
		doc.Buffer.SetSelections([]cbuf.Selection{
			{Anchor: cbuf.Position{Line: lineIdx, Column: 0, Byte: off}, Head: cbuf.Position{Line: lineIdx, Column: 0, Byte: off}},
		})

		// Make an edit (type a rune)
		char := rune('a' + (cycle % 26))
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: char}})
		m = updated.(*AppModel)

		// Deliberately perturb viewport to simulate aggressive user scroll
		fakeScrollY := rng.Intn(400)
		m.viewportY = fakeScrollY
		if ap := m.splits.ActivePane(); ap != nil {
			ap.ViewportY = fakeScrollY
			ap.TargetViewportY = float64(fakeScrollY)
			ap.SmoothScrollY = float64(fakeScrollY)
		}

		// Perform Undo
		undoKey := makeUndoRedoKey("ctrl_z")
		if cycle%2 == 1 {
			undoKey = makeUndoRedoKey("ctrl_cyrillic_ya")
		}
		updated, _ = m.Update(tea.KeyMsg{Key: undoKey})
		m = updated.(*AppModel)

		ap := m.splits.ActivePane()
		if ap == nil {
			t.Fatal("expected active pane")
		}

		// Immediate verification of lock
		if ap.TargetViewportY != float64(m.viewportY) || ap.SmoothScrollY != float64(m.viewportY) {
			t.Fatalf("cycle %d: offsets desynced on undo: viewportY=%d, target=%f, smooth=%f",
				cycle, m.viewportY, ap.TargetViewportY, ap.SmoothScrollY)
		}

		// Fire a storm of 15 to 30 rapid animation ticks
		numTicks := 15 + rng.Intn(15)
		for tCount := 0; tCount < numTicks; tCount++ {
			updated, _ = m.Update(animTickMsg{})
			m = updated.(*AppModel)

			ap = m.splits.ActivePane()
			if ap.TargetViewportY != float64(m.viewportY) || ap.SmoothScrollY != float64(m.viewportY) {
				t.Fatalf("cycle %d, tick %d: animation tick caused offset drift! viewportY=%d, target=%f, smooth=%f",
					cycle, tCount, m.viewportY, ap.TargetViewportY, ap.SmoothScrollY)
			}
		}

		// Verify restored cursor is still visible
		sels := doc.Buffer.GetSelections()
		if len(sels) > 0 {
			curLine := sels[0].Head.Line
			editorHeight := m.height - 2
			if curLine < m.viewportY || curLine >= m.viewportY+editorHeight {
				t.Fatalf("cycle %d: cursor at line %d is outside viewport [%d, %d)",
					cycle, curLine, m.viewportY, m.viewportY+editorHeight)
			}
		}

		// Redo and fire another animation storm
		redoKey := makeUndoRedoKey("ctrl_y")
		if cycle%2 == 1 {
			redoKey = makeUndoRedoKey("ctrl_cyrillic_en")
		}
		updated, _ = m.Update(tea.KeyMsg{Key: redoKey})
		m = updated.(*AppModel)

		for tCount := 0; tCount < 10; tCount++ {
			updated, _ = m.Update(animTickMsg{})
			m = updated.(*AppModel)
		}

		ap = m.splits.ActivePane()
		if ap.TargetViewportY != float64(m.viewportY) || ap.SmoothScrollY != float64(m.viewportY) {
			t.Fatalf("cycle %d: offsets desynced after redo and ticks: viewportY=%d, target=%f, smooth=%f",
				cycle, m.viewportY, ap.TargetViewportY, ap.SmoothScrollY)
		}
	}
}

// ---------------------------------------------------------------------------
// 6. Multi-Pane Split Synchronization Under Ticks
// ---------------------------------------------------------------------------

func TestAdversarial_SmoothScroll_MultiPaneSplit_Synchronization(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m := updated.(*AppModel)

	// Create 200 lines
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, fmt.Sprintf("line %03d: multi-pane test content", i))
	}
	updated, _ = m.Update(tea.PasteMsg{Text: strings.Join(lines, "\n")})
	m = updated.(*AppModel)

	// Configure 2 columns split layout
	if m.splits != nil {
		m.splits.SetLayout(Split2Cols)
		area := buffer.NewRect(0, 2, 120, 36)
		doc := m.eng.ActiveDocument()
		var docs []*core.Document
		if doc != nil {
			docs = []*core.Document{doc}
		}
		m.splits.UpdateLayout(area, docs, doc.ID)
	}

	if m.splits.TotalPanes() < 2 {
		t.Fatalf("expected at least 2 panes, got %d", m.splits.TotalPanes())
	}

	// Pane 0: scrolled to 120
	m.splits.Panes[0].ViewportY = 120
	m.splits.Panes[0].TargetViewportY = 120
	m.splits.Panes[0].SmoothScrollY = 120

	// Pane 1: active, cursor at line 5, insert edit
	m.splits.ActiveIndex = 1
	doc := m.eng.ActiveDocument()
	off5, _ := doc.Buffer.ByteOffsetForLine(5)
	doc.Buffer.SetSelections([]cbuf.Selection{
		{Anchor: cbuf.Position{Line: 5, Column: 0, Byte: off5}, Head: cbuf.Position{Line: 5, Column: 0, Byte: off5}},
	})
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '@'}})
	m = updated.(*AppModel)

	// Now scroll active pane (Pane 1) to line 180
	m.viewportY = 180
	m.splits.Panes[1].ViewportY = 180
	m.splits.Panes[1].TargetViewportY = 180
	m.splits.Panes[1].SmoothScrollY = 180

	// Perform Undo
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	// Pane 1 should have snapped near line 5
	if m.splits.Panes[1].ViewportY > 10 {
		t.Fatalf("expected Pane 1 ViewportY <= 10, got %d", m.splits.Panes[1].ViewportY)
	}

	// Verify all panes have synchronized TargetViewportY and SmoothScrollY
	for i := range m.splits.Panes {
		p := &m.splits.Panes[i]
		if p.TargetViewportY != float64(p.ViewportY) {
			t.Fatalf("pane %d TargetViewportY %f != ViewportY %d", i, p.TargetViewportY, p.ViewportY)
		}
		if p.SmoothScrollY != float64(p.ViewportY) {
			t.Fatalf("pane %d SmoothScrollY %f != ViewportY %d", i, p.SmoothScrollY, p.ViewportY)
		}
	}

	// Run 100 animation ticks across splits
	for tick := 0; tick < 100; tick++ {
		updated, _ = m.Update(animTickMsg{})
		m = updated.(*AppModel)

		// Assert pane 0 remained at 120
		if m.splits.Panes[0].ViewportY != 120 {
			t.Fatalf("tick %d: non-active Pane 0 shifted! got %d, expected 120", tick, m.splits.Panes[0].ViewportY)
		}
		// Assert active pane remained <= 10
		if m.splits.Panes[1].ViewportY > 10 {
			t.Fatalf("tick %d: active Pane 1 drifted! got %d", tick, m.splits.Panes[1].ViewportY)
		}
	}
}

// ---------------------------------------------------------------------------
// 7. Terminal Drawer Open & Height Constrained Viewport Sync
// ---------------------------------------------------------------------------

func TestAdversarial_SmoothScroll_TerminalDrawerHeightResize_Stability(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := updated.(*AppModel)

	// Create 100 lines
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("content line %d", i))
	}
	updated, _ = m.Update(tea.PasteMsg{Text: strings.Join(lines, "\n")})
	m = updated.(*AppModel)

	// Open terminal drawer (takes 10 rows of 24)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)

	if !m.terminal.Open {
		t.Fatal("expected terminal open")
	}

	// Move cursor to line 18 and edit
	doc := m.eng.ActiveDocument()
	off18, _ := doc.Buffer.ByteOffsetForLine(18)
	doc.Buffer.SetSelections([]cbuf.Selection{
		{Anchor: cbuf.Position{Line: 18, Column: 0, Byte: off18}, Head: cbuf.Position{Line: 18, Column: 0, Byte: off18}},
	})
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '$'}})
	m = updated.(*AppModel)

	// With terminal open, usable height is reduced (24 - 1 - 2 - 10 = 11)
	// Cursor at line 18 forces viewportY to scroll down so 18 is visible
	if m.viewportY < 8 {
		t.Fatalf("expected viewportY >= 8 with terminal open, got %d", m.viewportY)
	}

	ap := m.splits.ActivePane()
	if ap.TargetViewportY != float64(m.viewportY) || ap.SmoothScrollY != float64(m.viewportY) {
		t.Fatalf("offsets desynced with terminal open: viewportY=%d, target=%f, smooth=%f",
			m.viewportY, ap.TargetViewportY, ap.SmoothScrollY)
	}

	// 50 animation ticks
	for i := 0; i < 50; i++ {
		updated, _ = m.Update(animTickMsg{})
		m = updated.(*AppModel)
	}

	if ap.TargetViewportY != float64(m.viewportY) || ap.SmoothScrollY != float64(m.viewportY) {
		t.Fatalf("offsets drifted after 50 ticks with terminal open")
	}

	// Send Ctrl+Z with empty terminal prompt (passthrough undo)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	// Offsets must still be locked
	if ap.TargetViewportY != float64(m.viewportY) || ap.SmoothScrollY != float64(m.viewportY) {
		t.Fatalf("offsets desynced after passthrough undo with terminal open")
	}

	// 50 more ticks
	for i := 0; i < 50; i++ {
		updated, _ = m.Update(animTickMsg{})
		m = updated.(*AppModel)
	}

	if ap.TargetViewportY != float64(m.viewportY) || ap.SmoothScrollY != float64(m.viewportY) {
		t.Fatalf("offsets drifted after passthrough undo + 50 ticks")
	}
}

// ---------------------------------------------------------------------------
// 8. Exponential Damping Convergence Test
// ---------------------------------------------------------------------------

func TestAdversarial_SmoothScroll_ExponentialDampingConvergence(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := updated.(*AppModel)

	ap := m.splits.ActivePane()
	if ap == nil {
		t.Fatal("expected active pane")
	}

	// Intentionally set a delta (e.g. SmoothScrollY = 0, TargetViewportY = 100)
	ap.SmoothScrollY = 0
	ap.TargetViewportY = 100
	ap.ViewportY = 0

	// Step smooth scroll repeatedly until convergence
	ticks := 0
	for {
		ticks++
		needsRedraw := m.stepSmoothScroll()
		if !needsRedraw {
			break
		}
		if ticks > 100 {
			t.Fatalf("stepSmoothScroll failed to converge within 100 ticks: SmoothScrollY=%f, Target=%f",
				ap.SmoothScrollY, ap.TargetViewportY)
		}
	}

	// After convergence:
	if ap.SmoothScrollY != 100 {
		t.Fatalf("expected SmoothScrollY == 100, got %f", ap.SmoothScrollY)
	}
	if ap.ViewportY != 100 {
		t.Fatalf("expected ViewportY == 100, got %d", ap.ViewportY)
	}
	if m.viewportY != 100 {
		t.Fatalf("expected m.viewportY == 100, got %d", m.viewportY)
	}

	// Next tick must not request redraw
	if m.stepSmoothScroll() {
		t.Fatal("expected stepSmoothScroll to return false once converged")
	}
}
