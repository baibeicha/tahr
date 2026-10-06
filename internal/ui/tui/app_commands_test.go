package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core"
)

func TestAppModel_OmnibarCommandExecution(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Open omnibar in commands mode
	m := model
	m.openOmnibar("commands")
	if !m.omnibarOpen || m.omnibarMode != "commands" {
		t.Fatalf("expected omnibar open in commands mode")
	}

	// Filter for "Toggle Breakpoint"
	m.omnibarQuery = "Breakpoint"
	m.updateOmnibarCandidates()
	if len(m.omnibarItems) == 0 {
		t.Fatalf("expected to find Toggle Breakpoint command")
	}

	// Press Enter to execute
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	m = updated.(*AppModel)

	if len(m.breakpoints) != 1 || !m.breakpoints[0] {
		t.Errorf("expected breakpoint on line 0, got %v", m.breakpoints)
	}
}

func TestAppModel_CyrillicHotkeys(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// Type initial text
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'x'}})

	// Ctrl + 'ы' (Russian Ctrl+S -> Save)
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'ы',
		Mod:	input.ModCtrl,
	}})
	m = updated.(*AppModel)
	if !strings.HasPrefix(m.statusMessage, "Saved") {
		t.Errorf("expected Ctrl+ы to save, got status %q", m.statusMessage)
	}

	// Ctrl + 'з' (Russian Ctrl+P -> Omnibar)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'з',
		Mod:	input.ModCtrl,
	}})
	m = updated.(*AppModel)
	if !m.omnibarOpen {
		t.Fatalf("expected Ctrl+з to open Omnibar")
	}
	m.omnibarOpen = false

	// Ctrl + 'и' (Russian Ctrl+B -> Toggle Project Tree)
	origOpen := m.sidebarOpen
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'и',
		Mod:	input.ModCtrl,
	}})
	m = updated.(*AppModel)
	if m.sidebarOpen == origOpen {
		t.Errorf("expected Ctrl+и to toggle sidebar")
	}

	// Ctrl + 'ф' (Russian Ctrl+A -> Select All)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'ф',
		Mod:	input.ModCtrl,
	}})
	m = updated.(*AppModel)
	doc := m.eng.ActiveDocument()
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		t.Fatalf("expected active selection after Ctrl+ф")
	}
}

func TestAppModel_BuildAndRunHotkeys(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// F5 (Run)
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF5}})
	m = updated.(*AppModel)
	if !m.outputOpen && (m.terminal == nil || !m.terminal.Open) {
		t.Fatalf("expected F5 to open output drawer or terminal")
	}
	if !strings.Contains(m.statusMessage, "Starting") && !strings.Contains(m.statusMessage, "Running") {
		t.Errorf("expected Run status message, got %q", m.statusMessage)
	}
	m.outputOpen = false
	if m.terminal != nil {
		m.terminal.Open = false
	}

	// F7 (Build)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF7}})
	m = updated.(*AppModel)
	if !m.outputOpen {
		t.Fatalf("expected F7 to open output drawer")
	}
	if !strings.Contains(m.statusMessage, "Building") {
		t.Errorf("expected Build status message, got %q", m.statusMessage)
	}
}

func TestAppModel_HeaderToolbarClicks(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	frameBuf := buffer.NewBuffer(80, 24)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	var menuBtn, termBtn *toolbarBtn
	for i := range m.toolbarButtons {
		if m.toolbarButtons[i].id == "menu" {
			menuBtn = &m.toolbarButtons[i]
		}
		if m.toolbarButtons[i].id == "term" {
			termBtn = &m.toolbarButtons[i]
		}
	}
	if menuBtn == nil || termBtn == nil {
		t.Fatalf("expected toolbar buttons to be initialized")
	}

	// Click Menu button on top toolbar
	updated, _ := m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	menuBtn.minX + 1,
			Y:	0,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if !m.mainMenuOpen {
		t.Fatalf("expected clicking Menu button to open main menu")
	}
	m.mainMenuOpen = false

	// Click Term button on top toolbar
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	termBtn.minX + 1,
			Y:	0,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if m.terminal == nil || !m.terminal.Open {
		t.Fatalf("expected clicking Term button to open terminal drawer")
	}

	// Click Tree icon on left Activity Bar (1, 2)
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	1,
			Y:	2,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if !m.sidebarOpen {
		t.Fatalf("expected clicking Activity Bar tree to open sidebar")
	}

	// Click Settings icon on left Activity Bar (1, m.height-2)
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	1,
			Y:	m.height - 2,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if m.settings == nil || !m.settings.Open {
		t.Fatalf("expected clicking Activity Bar settings to open settings modal")
	}
}

func TestAppModel_DAPAndLSPControls(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// F9 toggles breakpoint
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF9}})
	m = updated.(*AppModel)
	if !m.breakpoints[0] {
		t.Fatalf("expected breakpoint on line 0 after F9")
	}

	// F10 step over
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF10}})
	m = updated.(*AppModel)
	if !strings.Contains(m.statusMessage, "Step Over") {
		t.Errorf("expected Step Over status, got %q", m.statusMessage)
	}

	// F11 step in
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF11}})
	m = updated.(*AppModel)
	if !strings.Contains(m.statusMessage, "Step In") {
		t.Errorf("expected Step In status, got %q", m.statusMessage)
	}

	// F12 goto definition
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF12}})
	m = updated.(*AppModel)
	if !strings.Contains(m.statusMessage, "definition") {
		t.Errorf("expected Definition status, got %q", m.statusMessage)
	}

	// Shift+K hover documentation
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'K', Mod: input.ModShift}})
	m = updated.(*AppModel)
	if !m.popupVisible {
		t.Fatalf("expected hover tooltip popup to be visible after Shift+K")
	}
}

func TestAppModel_Phase5_Terminal_Git_Minimap_Breadcrumbs_DAPHUD(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"
	code := "package main\n\ntype Engine struct {}\n\nfunc (e *Engine) Start() {\n\tprintln(\"run\")\n}\n"
	_, _ = m.Update(tea.PasteMsg{Text: code})

	// 1. Test Terminal Drawer Toggle (F4 and click)
	if m.terminal.Open {
		t.Fatal("expected terminal to start closed")
	}
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	if !m.terminal.Open {
		t.Fatal("expected terminal to open on F4")
	}
	// Type into terminal
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'l'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 's'}})
	if m.terminal.InputText != "ls" {
		t.Fatalf("expected terminal input 'ls', got %q", m.terminal.InputText)
	}
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	if m.terminal.Open {
		t.Fatal("expected terminal to close on second F4")
	}

	// 2. Test DAP Debugger HUD (F8 and Shift+F5)
	if m.dapHUD.Open {
		t.Fatal("expected DAP HUD to start closed")
	}
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF8}})
	if !m.dapHUD.Open {
		t.Fatal("expected DAP HUD to open on F8")
	}

	// Render view to populate buttons and minimap
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	if len(m.dapHUD.Buttons) == 0 {
		t.Fatal("expected DAP HUD buttons to be rendered")
	}

	// Test click on dap_stop button
	for _, b := range m.dapHUD.Buttons {
		if b.ID == "dap_stop" {
			_, _ = m.Update(tea.MouseMsg{
				Mouse: input.Mouse{
					X:	b.MinX + 1,
					Y:	b.Y,
					Button:	input.MouseLeft,
					Action:	input.MousePress,
				},
			})
			break
		}
	}
	if m.dapHUD.Open {
		t.Fatal("expected DAP HUD to close on stop button click")
	}

	// 3. Test Minimap hitboxes & click-to-scroll
	m.View(frame)
	if len(m.minimapHitboxes) == 0 {
		t.Fatal("expected minimap hitboxes when width >= 40")
	}
	mm := m.minimapHitboxes[0]
	// Click near bottom of minimap
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	mm.minX + 1,
			Y:	mm.maxY,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
		},
	})

	// 4. Test Breadcrumbs on Row 1 (Tab Bar right side)
	m.View(frame)
	foundBreadcrumbs := false
	for x := 0; x < 100; x++ {
		cell := frameBuf.Cell(x, 1)
		if cell != nil && (cell.Rune == '›' || cell.Rune == '⚡' || cell.Rune == '🔷') {
			foundBreadcrumbs = true
			break
		}
	}
	if !foundBreadcrumbs {
		t.Errorf("expected breadcrumbs glyph on row 1")
	}

	// 5. Test Omnibar command execution for terminal and minimap
	m.executeOmnibarCommand("View: Toggle Integrated Terminal")
	if !m.terminal.Open {
		t.Errorf("expected terminal open via omnibar")
	}
	m.executeOmnibarCommand("View: Toggle Integrated Terminal")
	if m.terminal.Open {
		t.Errorf("expected terminal closed via omnibar")
	}

	m.executeOmnibarCommand("View: Toggle Code Minimap")
	if m.settings.Current.ShowMinimap {
		t.Errorf("expected minimap toggled off")
	}
}

func TestAppModel_Phase6_Focus_Undo_Minimap_Marketplace_Colors(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Terminal Open vs Editor Undo (Ctrl+Z)
	// Type into active editor
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'h'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'i'}})

	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "hi" {
		t.Fatalf("expected 'hi' in buffer, got %q", string(txt))
	}

	// Open terminal drawer via F4
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	if !m.terminal.Open {
		t.Fatal("expected terminal drawer to be open")
	}
	if !m.terminalFocused {
		t.Fatal("expected terminal to be focused initially on F4")
	}

	// Click in editor area to switch focus to editor
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	20,
			Y:	5,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
		},
	})
	if m.terminalFocused {
		t.Fatal("expected terminalFocused to be false after clicking in editor")
	}
	if !m.terminal.Open {
		t.Fatal("expected terminal to remain visible at bottom")
	}

	// Press Ctrl+Z (Undo) in editor
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'z',
		Mod:	input.ModCtrl,
	}})
	txtAfterUndo, _ := doc.Buffer.GetText()
	if string(txtAfterUndo) == "hi" {
		t.Errorf("expected Ctrl+Z to undo editor text while terminal drawer is open, but text was %q", string(txtAfterUndo))
	}

	// 2. Test Alt+M Minimap Toggle
	initialMinimapState := m.settings.Current.ShowMinimap
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'm',
		Mod:	input.ModAlt,
	}})
	if m.settings.Current.ShowMinimap == initialMinimapState {
		t.Errorf("expected Alt+M to flip ShowMinimap state")
	}
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'm',
		Mod:	input.ModAlt,
	}})
	if m.settings.Current.ShowMinimap != initialMinimapState {
		t.Errorf("expected second Alt+M to restore ShowMinimap state")
	}

	// 3. Test Fast Scrolling (Mouse Wheel & Keyboard)
	// Add lines to document so scrolling is possible
	for i := 0; i < 50; i++ {
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertNewline})
	}
	initialVpY := m.viewportY
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyDown,
		Mod:	input.ModCtrl,
	}})
	if m.viewportY != initialVpY+5 {
		t.Errorf("expected Ctrl+Down to scroll 5 lines, got vpY=%d", m.viewportY)
	}

	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyUp,
		Mod:	input.ModCtrl,
	}})
	if m.viewportY != initialVpY {
		t.Errorf("expected Ctrl+Up to scroll back 5 lines, got vpY=%d", m.viewportY)
	}

	// Mouse wheel down acceleration
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	20,
			Y:	5,
			Button:	input.MouseWheelDown,
		},
	})
	if m.viewportY < 5 {
		t.Errorf("expected mouse wheel down to scroll at least 5 lines, got %d", m.viewportY)
	}

	// 4. Test Marketplace Modal (Ctrl+Shift+X)
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'x',
		Mod:	input.ModCtrl | input.ModShift,
	}})
	if !m.marketplace.Open {
		t.Fatal("expected marketplace modal to open on Ctrl+Shift+X")
	}

	// Switch to Repositories tab in Marketplace (press '3')
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'3',
	}})
	if m.marketplace.ActiveTab != 2 {
		t.Errorf("expected active tab 2 (Repositories), got %d", m.marketplace.ActiveTab)
	}

	// Close marketplace on Esc
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	if m.marketplace.Open {
		t.Fatal("expected marketplace modal to close on Esc")
	}

	// 5. Test Settings Swatches & Color Picker
	m.settings.Open = true
	m.settings.CategoryIdx = 4	// Color Palette
	m.settings.FocusRight = true
	m.settings.FieldIdx = 0

	// Press Enter to open Color Picker
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	if m.settings.ColorPicker == nil || !m.settings.ColorPicker.Open {
		t.Fatal("expected ColorPicker modal to open on Enter in Color Palette category")
	}

	// Move wheel cursor right
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRight}})
	// Apply color on Enter
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	if m.settings.ColorPicker.Open {
		t.Fatal("expected ColorPicker to close on Enter after applying color")
	}
}
