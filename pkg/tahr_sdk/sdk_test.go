package tahr_sdk

import (
	"testing"
)

func TestSDK_NativeOperations(t *testing.T) {
	SetMockDocument([]string{"package main", "func hello() {}"})

	if TotalLines() != 2 {
		t.Fatalf("expected 2 lines, got %d", TotalLines())
	}

	line0, err := GetLine(0)
	if err != nil || line0 != "package main" {
		t.Fatalf("unexpected line 0: %q, err: %v", line0, err)
	}

	if err := InsertText(1, 12, "/* comment */"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	line1, err := GetLine(1)
	if err != nil || line1 != "func hello()/* comment */ {}" {
		t.Fatalf("unexpected line 1: %q", line1)
	}

	full := GetText()
	if full != "package main\nfunc hello()/* comment */ {}" {
		t.Fatalf("unexpected full text: %q", full)
	}

	if err := SetCursor(1, 5); err != nil {
		t.Fatalf("set cursor failed: %v", err)
	}
	cur := GetCursor()
	if cur.Line != 1 || cur.Column != 5 {
		t.Fatalf("unexpected cursor: %+v", cur)
	}

	Log("Hello from plugin!")
	logs := GetMockLogs()
	if len(logs) == 0 || logs[len(logs)-1] != "Hello from plugin!" {
		t.Fatalf("expected logged message in mock logs: %v", logs)
	}

	// Test Toast API
	ToastSuccess("BUILD", "Compilation succeeded")
	toasts := GetMockToasts()
	if len(toasts) == 0 {
		t.Fatalf("expected recorded toast")
	}

	// Test Workspace API
	if GetActiveFilePath() != "main.go" {
		t.Fatalf("expected main.go, got %s", GetActiveFilePath())
	}
	if GetWorkspaceRoot() != "." {
		t.Fatalf("expected ., got %s", GetWorkspaceRoot())
	}

	// Test Config API
	SetConfig("tabSize", "4")
	if GetConfig("tabSize") != "4" {
		t.Fatalf("expected tabSize=4, got %s", GetConfig("tabSize"))
	}

	// Test command dispatch
	executed := false
	RegisterCommand("my.plugin.format", func() {
		executed = true
	})

	if err := DispatchCommand("my.plugin.format"); err != nil || !executed {
		t.Fatalf("expected command execution: err=%v, executed=%v", err, executed)
	}

	// Test Virtual Text API
	ClearVirtualText("")
	AddVirtualText(VirtualTextItem{
		ID:     "blame-1",
		Source: "git-lens",
		Kind:   VirtualTextEndOfLine,
		Line:   0,
		Text:   "John Doe, 2 hours ago • Add main package",
	})
	vtext := GetMockVirtualText()
	if len(vtext) != 1 || vtext[0].Text != "John Doe, 2 hours ago • Add main package" {
		t.Fatalf("unexpected virtual text: %+v", vtext)
	}
	ClearVirtualText("git-lens")
	if len(GetMockVirtualText()) != 0 {
		t.Fatalf("expected virtual text cleared, got %d", len(GetMockVirtualText()))
	}

	// Test Gutter Marker API
	ClearGutterMarkers("")
	AddGutterMarker(GutterMarker{
		ID:     "cov-1",
		Source: "test-coverage",
		Line:   1,
		Glyph:  "✔",
	})
	markers := GetMockGutterMarkers()
	if len(markers) != 1 || markers[0].Glyph != "✔" {
		t.Fatalf("unexpected gutter markers: %+v", markers)
	}
	ClearGutterMarkers("test-coverage")
	if len(GetMockGutterMarkers()) != 0 {
		t.Fatalf("expected gutter markers cleared")
	}

	// Test Bookmark API
	ClearBookmarks()
	added := ToggleBookmark("main.go", 10, "Important entrypoint")
	if !added {
		t.Fatalf("expected bookmark added")
	}
	bms := GetBookmarks()
	if len(bms) != 1 || bms[0].Line != 10 || bms[0].Label != "Important entrypoint" {
		t.Fatalf("unexpected bookmarks: %+v", bms)
	}
	// Toggle again should remove
	removed := ToggleBookmark("main.go", 10, "")
	if removed {
		t.Fatalf("expected bookmark removed upon second toggle")
	}
	if len(GetBookmarks()) != 0 {
		t.Fatalf("expected empty bookmarks after untoggle")
	}

	// Test CodeLens API
	SetCodeLens("main.go", []CodeLensItem{
		{
			Range:     Range{Start: Position{Line: 1, Column: 0}, End: Position{Line: 1, Column: 10}},
			CommandID: "test.run",
			Title:     "▶ Run Test",
		},
	})
	lenses := GetCodeLens("main.go")
	if len(lenses) != 1 || lenses[0].Title != "▶ Run Test" {
		t.Fatalf("unexpected lenses: %+v", lenses)
	}

	// Test Diagnostics API
	ClearDiagnostics("main.go", "")
	PublishDiagnostics("main.go", "compiler", []DiagnosticItem{
		{
			Range:    Range{Start: Position{Line: 0, Column: 0}, End: Position{Line: 0, Column: 5}},
			Severity: DiagnosticSeverityError,
			Message:  "syntax error",
			Source:   "compiler",
		},
	})
	diags := GetDiagnostics("main.go")
	if len(diags) != 1 || diags[0].Message != "syntax error" {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	ClearDiagnostics("main.go", "compiler")
	if len(GetDiagnostics("main.go")) != 0 {
		t.Fatalf("expected diagnostics cleared")
	}

	// Test DataGrid API
	SetDataGrid(DataGridDef{
		ID: "sqlite-users",
		Columns: []DataGridColumn{
			{ID: "id", Title: "ID", Width: 5, Sortable: true},
			{ID: "name", Title: "Name", Width: 20, Sortable: true},
		},
		Rows: [][]string{
			{"1", "Alice"},
			{"2", "Bob"},
		},
		TotalRows: 2,
		Page:      1,
		PageSize:  50,
	})
	grid, ok := GetDataGrid("sqlite-users")
	if !ok || len(grid.Rows) != 2 || grid.Columns[1].Title != "Name" {
		t.Fatalf("unexpected grid: %+v", grid)
	}

	// Test TreeView API
	SetTreeView(TreeViewDef{
		ID:    "todo-tree",
		Title: "Workspace TODOs",
		Root: []TreeNode{
			{
				ID:    "file-1",
				Label: "main.go",
				Icon:  "file",
				Children: []TreeNode{
					{ID: "todo-1", Label: "TODO: implement caching", Line: 42, Badge: "HIGH"},
				},
			},
		},
	})
	tree, ok := GetTreeView("todo-tree")
	if !ok || len(tree.Root) != 1 || len(tree.Root[0].Children) != 1 {
		t.Fatalf("unexpected tree: %+v", tree)
	}

	// Test ChatView API
	SetChatView(ChatViewDef{
		ID:    "ai-main",
		Title: "AI Assistant",
		Messages: []ChatMessage{
			{ID: "m1", Role: "user", Content: "How to parse JSON in Go?"},
		},
	})
	AppendChatMessage("ai-main", ChatMessage{
		ID:      "m2",
		Role:    "assistant",
		Content: "Use encoding/json package.",
		Status:  "done",
	})
	chat, ok := GetChatView("ai-main")
	if !ok || len(chat.Messages) != 2 || chat.Messages[1].Role != "assistant" {
		t.Fatalf("unexpected chat: %+v", chat)
	}

	// Test SplitView API
	SetSplitView(SplitViewDef{
		ID:          "diff-1",
		Orientation: "horizontal",
		LeftTitle:   "Original",
		RightTitle:  "Modified",
		LeftText:    "func hello() {}",
		RightText:   "func hello(msg string) {}",
	})
	split, ok := GetSplitView("diff-1")
	if !ok || split.LeftTitle != "Original" || split.RightText != "func hello(msg string) {}" {
		t.Fatalf("unexpected split view: %+v", split)
	}

	// Test Notebook API
	SetNotebookView(NotebookDef{
		ID:         "nb-1",
		FilePath:   "analysis.ipynb",
		KernelName: "python3",
		Language:   "python",
		Cells: []NotebookCell{
			{
				ID:             "cell-1",
				CellType:       CellTypeCode,
				Source:         "import math\nprint(math.pi)",
				ExecutionCount: 1,
				Outputs: []NotebookCellOutput{
					{
						OutputType: "stream",
						Name:       "stdout",
						Text:       "3.141592653589793\n",
					},
				},
				Status: "idle",
			},
		},
	})
	nb, ok := GetNotebookView("nb-1")
	if !ok || len(nb.Cells) != 1 || nb.Cells[0].Outputs[0].Text != "3.141592653589793\n" {
		t.Fatalf("unexpected notebook: %+v", nb)
	}
	UpdateNotebookCell("nb-1", NotebookCell{
		ID:             "cell-2",
		CellType:       CellTypeMarkdown,
		Source:         "# Summary Results",
		ExecutionCount: 0,
		Status:         "idle",
	})
	nb, _ = GetNotebookView("nb-1")
	if len(nb.Cells) != 2 || nb.Cells[1].Source != "# Summary Results" {
		t.Fatalf("expected updated cells: %+v", nb.Cells)
	}

	// Test Connection Profiles API
	SetConnectionProfiles([]ConnectionProfile{
		{
			ID:     "prod-pg",
			Type:   "postgres",
			Name:   "Production DB",
			Host:   "db.internal",
			Port:   5432,
			User:   "postgres",
			Status: "connected",
		},
		{
			ID:     "dev-k8s",
			Type:   "k8s",
			Name:   "Minikube Cluster",
			Host:   "https://127.0.0.1:8443",
			Port:   8443,
			Status: "connected",
		},
	})
	profiles := GetConnectionProfiles()
	if len(profiles) != 2 || profiles[0].ID != "prod-pg" || profiles[1].Type != "k8s" {
		t.Fatalf("unexpected connection profiles: %+v", profiles)
	}
}

