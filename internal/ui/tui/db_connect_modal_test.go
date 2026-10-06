package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/dag"
	"tahr/internal/core/db"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

func TestDBConnectModal_BasicsAndKeyNav(t *testing.T) {
	theme := ui.DefaultTheme()
	modal := NewDBConnectModal(&theme)

	if modal.Visible {
		t.Fatalf("expected modal to be hidden initially")
	}

	modal.OpenDialog()
	if !modal.Visible {
		t.Fatalf("expected modal to be visible after OpenDialog()")
	}

	// 1. Initial type is PostgreSQL
	if modal.Types[modal.SelectedType].Name != "PostgreSQL" {
		t.Fatalf("expected default type PostgreSQL, got %s", modal.Types[modal.SelectedType].Name)
	}

	// 2. Switch type with Right Arrow
	modal.ActiveField = 0
	modal.HandleKey(input.Key{Type: input.KeyRight})
	if modal.Types[modal.SelectedType].Name != "MySQL" {
		t.Fatalf("expected next type MySQL, got %s", modal.Types[modal.SelectedType].Name)
	}

	// 3. Switch to SQLite (file-based)
	for modal.Types[modal.SelectedType].Name != "SQLite" {
		modal.HandleKey(input.Key{Type: input.KeyRight})
	}
	if !modal.Types[modal.SelectedType].IsFileBased {
		t.Fatalf("expected SQLite to be file based")
	}

	// 4. Test connection
	modal.FilePath = "./test.db"
	modal.TestConnection()
	if !modal.StatusSuccess {
		t.Fatalf("expected successful test connection for sqlite: %s", modal.StatusMsg)
	}

	// 5. Save & Connect callback
	var savedProfile *db.ConnectionProfile
	modal.OnSave = func(p db.ConnectionProfile) {
		savedProfile = &p
	}
	modal.ActiveField = 8 // Connect button
	modal.HandleKey(input.Key{Type: input.KeyEnter})
	if modal.Visible {
		t.Fatalf("expected modal to close after connect")
	}
	if savedProfile == nil || savedProfile.Type != "SQLite" || savedProfile.FilePath != "./test.db" {
		t.Fatalf("unexpected saved profile: %+v", savedProfile)
	}
}

func TestDBConnectModal_MouseInteraction(t *testing.T) {
	theme := ui.DefaultTheme()
	modal := NewDBConnectModal(&theme)
	modal.OpenDialog()

	screenW := 100
	screenH := 30
	dialogW := 64
	dialogH := 18
	startX := (screenW - dialogW) / 2
	startY := (screenH - dialogH) / 2

	// 1. Click outside modal -> closes
	consumed, action := modal.HandleMouse(input.Mouse{
		X:      startX - 5,
		Y:      startY - 5,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}, screenW, screenH)
	if !consumed || action != "cancel" || modal.Visible {
		t.Fatalf("expected clicking outside to cancel and close modal")
	}

	// Reopen
	modal.OpenDialog()

	// 2. Click Type Switcher ◀ at startX+18, startY+2
	prevType := modal.SelectedType
	consumed, _ = modal.HandleMouse(input.Mouse{
		X:      startX + 18,
		Y:      startY + 2,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}, screenW, screenH)
	if !consumed || modal.SelectedType == prevType {
		t.Fatalf("expected clicking ◀ to change selected type")
	}

	// 3. Click Test button [ ⟳ Проверить ] at startX+10, startY+dialogH-2
	btnY := startY + dialogH - 2
	consumed, action = modal.HandleMouse(input.Mouse{
		X:      startX + 10,
		Y:      btnY,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}, screenW, screenH)
	if !consumed || action != "test" {
		t.Fatalf("expected clicking test button to run test connection")
	}

	// 4. Click Connect button at startX+30, btnY
	var saved db.ConnectionProfile
	modal.OnSave = func(p db.ConnectionProfile) {
		saved = p
	}
	consumed, action = modal.HandleMouse(input.Mouse{
		X:      startX + 30,
		Y:      btnY,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}, screenW, screenH)
	if !consumed || action != "connect" || modal.Visible {
		t.Fatalf("expected clicking connect button to connect and close modal")
	}
	if saved.Type == "" {
		t.Fatalf("expected valid saved profile")
	}
}

func TestRightSidebar_ResizeButtonsAndWheel(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.ToggleRightSidebar("db-inspector")
	m.rightSidebarAnimWidth = float64(m.rightSidebarWidth)
	initialW := m.rightSidebarWidth

	// Header row: editorTop = 2.
	// Click ◀ button to make sidebar wider: startX+sideW-6
	rightStartX := 100 - 3 - m.rightSidebarWidth
	btnWidenX := rightStartX + m.rightSidebarWidth - 6
	msgWiden := tea.MouseMsg{Mouse: input.Mouse{
		X:      btnWidenX,
		Y:      2,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgWiden)
	if m.rightSidebarWidth <= initialW {
		t.Fatalf("expected right sidebar to widen, was %d, got %d", initialW, m.rightSidebarWidth)
	}

	// Click ▶ button to make sidebar narrower: startX+sideW-4
	narrowW := m.rightSidebarWidth
	rightStartX = 100 - 3 - m.rightSidebarWidth
	btnNarrowX := rightStartX + m.rightSidebarWidth - 4
	msgNarrow := tea.MouseMsg{Mouse: input.Mouse{
		X:      btnNarrowX,
		Y:      2,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(msgNarrow)
	if m.rightSidebarWidth >= narrowW {
		t.Fatalf("expected right sidebar to narrow, was %d, got %d", narrowW, m.rightSidebarWidth)
	}

	// MouseWheelUp on header row to widen
	curW := m.rightSidebarWidth
	msgWheel := tea.MouseMsg{Mouse: input.Mouse{
		X:      rightStartX + 5,
		Y:      2,
		Button: input.MouseWheelUp,
	}}
	_, _ = m.handleMouse(msgWheel)
	if m.rightSidebarWidth <= curW {
		t.Fatalf("expected mouse wheel up to widen right sidebar, was %d, got %d", curW, m.rightSidebarWidth)
	}
}

func TestDAGCanvas_KindEmptyState(t *testing.T) {
	theme := ui.DefaultTheme()
	emptyModel := dag.NewGraphModel()

	// 1. Kind = "project-graph"
	pgCanvas := NewDAGCanvasWidget(emptyModel, &theme)
	pgCanvas.Kind = "project-graph"

	bufPG := buffer.NewBuffer(80, 20)
	rect := buffer.NewRect(0, 0, 80, 20)
	pgCanvas.Render(bufPG, rect)

	foundPGTitle := false
	for y := 0; y < 20; y++ {
		line := ""
		for x := 0; x < 80; x++ {
			if c := bufPG.Cell(x, y); c != nil {
				line += string(c.Rune)
			}
		}
		if line != "" && (containsString(line, "Граф проекта") || containsString(line, "Call Hierarchy") || containsString(line, "Project Graph") || containsString(line, i18n.T("dag.project_graph_title"))) {
			foundPGTitle = true
			break
		}
	}
	if !foundPGTitle {
		t.Fatalf("expected project-graph empty state to contain 'Граф проекта' or 'Project Graph'")
	}

	// 2. Kind = "db"
	dbCanvas := NewDAGCanvasWidget(emptyModel, &theme)
	dbCanvas.Kind = "db"

	bufDB := buffer.NewBuffer(80, 20)
	dbCanvas.Render(bufDB, rect)

	foundDBTitle := false
	for y := 0; y < 20; y++ {
		line := ""
		for x := 0; x < 80; x++ {
			if c := bufDB.Cell(x, y); c != nil {
				line += string(c.Rune)
			}
		}
		if line != "" && (containsString(line, "Схема базы данных не найдена") || containsString(line, "Database Schema Not Found") || containsString(line, i18n.T("dag.db_schema_not_found"))) {
			foundDBTitle = true
			break
		}
	}
	if !foundDBTitle {
		t.Fatalf("expected db canvas empty state to contain 'Схема базы данных не найдена' or 'Database Schema Not Found'")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 && (stringContains(s, substr)))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
