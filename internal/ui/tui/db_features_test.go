package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/db"
)

func TestRightSidebar_ResizeDrag(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Open right sidebar
	m.ToggleRightSidebar("db-inspector")
	if !m.rightSidebarOpen {
		t.Fatalf("expected right sidebar to be open")
	}

	initialW := m.rightSidebarWidth
	stripRightW := 3
	rightDividerX := m.width - stripRightW - initialW - 1

	// 1. Mouse press on divider starts dragging
	pressMsg := tea.MouseMsg{Mouse: input.Mouse{
		X:      rightDividerX,
		Y:      10,
		Button: input.MouseLeft,
		Action: input.MousePress,
	}}
	_, _ = m.handleMouse(pressMsg)
	if !m.rightSidebarDragging {
		t.Fatalf("expected rightSidebarDragging to be true after clicking divider")
	}
	if m.rightSidebarDragStartX != rightDividerX {
		t.Fatalf("expected dragStartX %d, got %d", rightDividerX, m.rightSidebarDragStartX)
	}

	// 2. Drag 10 columns to the left (expanding sidebar)
	dragMsg := tea.MouseMsg{Mouse: input.Mouse{
		X:      rightDividerX - 10,
		Y:      10,
		Button: input.MouseLeft,
		Action: input.MouseDrag,
	}}
	_, _ = m.handleMouse(dragMsg)
	if m.rightSidebarWidth != initialW+10 {
		t.Fatalf("expected sidebar width %d, got %d", initialW+10, m.rightSidebarWidth)
	}

	// 3. Mouse release stops dragging
	releaseMsg := tea.MouseMsg{Mouse: input.Mouse{
		X:      rightDividerX - 10,
		Y:      10,
		Button: input.MouseLeft,
		Action: input.MouseRelease,
	}}
	_, _ = m.handleMouse(releaseMsg)
	if m.rightSidebarDragging {
		t.Fatalf("expected rightSidebarDragging to be false after release")
	}
}

func TestERD_EditorTab(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// 1. Open ER Diagram tab
	m.OpenERDTab()

	activeDoc := m.eng.ActiveDocument()
	if activeDoc == nil {
		t.Fatalf("expected active document, got nil")
	}
	if !strings.HasSuffix(activeDoc.FilePath, "schema.erd") {
		t.Fatalf("expected active document to be schema.erd, got %s", activeDoc.FilePath)
	}
	if m.dagCanvasWidget == nil {
		t.Fatalf("expected dagCanvasWidget to be initialized")
	}

	// 2. Render pane with schema.erd
	frameBuf := buffer.NewBuffer(120, 40)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	// Verify tab title in tab bar
	cTab := frameBuf.Cell(1, 1)
	if cTab == nil {
		t.Fatalf("expected tab cell at (1,1)")
	}

	// 3. Pan canvas with keyboard in editor tab
	initialPanX := m.dagCanvasWidget.PanX
	initialPanY := m.dagCanvasWidget.PanY

	// Press Right arrow (pans canvas -4 on X)
	_, _ = m.handleKey(input.Key{Type: input.KeyRight})
	if m.dagCanvasWidget.PanX != initialPanX-4 {
		t.Fatalf("expected panX %d, got %d", initialPanX-4, m.dagCanvasWidget.PanX)
	}

	// Press Up arrow (pans canvas +2 on Y)
	_, _ = m.handleKey(input.Key{Type: input.KeyUp})
	if m.dagCanvasWidget.PanY != initialPanY+2 {
		t.Fatalf("expected panY %d, got %d", initialPanY+2, m.dagCanvasWidget.PanY)
	}

	// Press 'C' to center origin
	_, _ = m.handleKey(input.Key{Type: input.KeyRune, Rune: 'c'})
	if m.dagCanvasWidget.PanX != 0 || m.dagCanvasWidget.PanY != 0 {
		t.Fatalf("expected centered camera (0, 0), got (%d, %d)", m.dagCanvasWidget.PanX, m.dagCanvasWidget.PanY)
	}
}

func TestDBConsole_InteractiveExecution(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// 1. Open DB Console
	m.OpenDBConsole()
	doc := m.eng.ActiveDocument()
	if doc == nil || !strings.HasSuffix(doc.FilePath, "console.sql") {
		t.Fatalf("expected console.sql tab to be opened")
	}
	if doc.LanguageID != "sql" {
		t.Fatalf("expected LanguageID 'sql', got %s", doc.LanguageID)
	}

	// 2. Insert query: SELECT * FROM customers;
	_ = m.eng.Dispatch(core.Command{ID: core.CmdClearSelections})
	_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: "\nSELECT * FROM customers;\n"})

	// 3. Trigger Ctrl+Enter to execute SQL query at cursor
	ctrlEnter := input.Key{Type: input.KeyEnter, Mod: input.ModCtrl}
	_, _ = m.handleKey(ctrlEnter)

	// Verify DataGrid tab for 'customers' was opened
	gridDoc := m.eng.ActiveDocument()
	if gridDoc == nil || !strings.HasSuffix(gridDoc.FilePath, "customers.datagrid") {
		t.Fatalf("expected customers.datagrid tab to open on SELECT query execution, got %v", gridDoc)
	}

	// 4. Test DDL Execution: CREATE TABLE orders ...
	m.OpenDBConsole()
	_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: "\nCREATE TABLE orders (id INT PRIMARY KEY, total NUMERIC);\n"})
	_, _ = m.handleKey(ctrlEnter)

	if m.dagCanvasWidget == nil || m.dagCanvasWidget.Model == nil {
		t.Fatalf("expected ER diagram model to update on DDL execution")
	}
	if _, exists := m.dagCanvasWidget.Model.Nodes["orders"]; !exists {
		t.Fatalf("expected 'orders' table to be added to ER diagram DAG model")
	}
}

func TestDataGrid_ViewerAndCSVExport(t *testing.T) {
	tempDir := t.TempDir()

	testTable := &db.Table{
		Name: "users",
		Columns: []db.Column{
			{Name: "id", DataType: "uuid", IsPK: true},
			{Name: "email", DataType: "varchar(255)", IsNullable: false},
			{Name: "role_id", DataType: "int", IsNullable: false},
			{Name: "status", DataType: "varchar(50)", IsNullable: false},
		},
		ForeignKeys: []db.ForeignKey{
			{Name: "fk_users_roles", FromColumn: "role_id", ToTable: "roles", ToColumn: "id"},
		},
	}

	grid := NewDataGridWidget("users", testTable, nil)
	if grid.TableName != "users" {
		t.Fatalf("expected TableName 'users', got %s", grid.TableName)
	}
	if len(grid.Columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(grid.Columns))
	}
	if !grid.FKColumns["role_id"] {
		t.Fatalf("expected 'role_id' to be flagged as FKColumn")
	}

	// 1. Pagination
	if grid.TotalPages() < 2 {
		t.Fatalf("expected at least 2 pages for 48 sample rows, got %d", grid.TotalPages())
	}
	if grid.Page != 0 {
		t.Fatalf("expected initial page 0, got %d", grid.Page)
	}
	grid.NextPage()
	if grid.Page != 1 {
		t.Fatalf("expected page 1 after NextPage(), got %d", grid.Page)
	}
	grid.PrevPage()
	if grid.Page != 0 {
		t.Fatalf("expected page 0 after PrevPage(), got %d", grid.Page)
	}

	// 2. Row Selection
	if grid.SelectedRow != 0 {
		t.Fatalf("expected initial selected row 0")
	}
	grid.SelectNextRow()
	if grid.SelectedRow != 1 {
		t.Fatalf("expected selected row 1, got %d", grid.SelectedRow)
	}
	grid.SelectPrevRow()
	if grid.SelectedRow != 0 {
		t.Fatalf("expected selected row 0, got %d", grid.SelectedRow)
	}

	// 3. Search Filter
	grid.SetFilter("ACTIVE")
	if len(grid.FilteredRows) == 0 {
		t.Fatalf("expected filtered rows for 'ACTIVE', got 0")
	}
	grid.SetFilter("")
	if len(grid.FilteredRows) != len(grid.AllRows) {
		t.Fatalf("expected unfiltered rows to restore all rows")
	}

	// 4. CSV Export
	csvData := grid.ExportCSV(tempDir)
	if !strings.Contains(csvData, `"id","email","role_id","status"`) {
		t.Fatalf("expected CSV header row, got %s", csvData)
	}
	exportFile := filepath.Join(tempDir, "users_export.csv")
	bytes, err := os.ReadFile(exportFile)
	if err != nil {
		t.Fatalf("expected users_export.csv to be written to disk: %v", err)
	}
	if len(bytes) == 0 {
		t.Fatalf("expected non-empty exported CSV file")
	}
}
