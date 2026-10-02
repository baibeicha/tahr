//go:build !wasm

package tahr_sdk

import (
	"fmt"
	"sync"
)

var (
	mockMu          sync.RWMutex
	mockLines       = []string{""}
	mockPos         = Position{Line: 0, Column: 0}
	mockLogs        = []string{}
	mockToasts      = []string{}
	mockFilePath    = "main.go"
	mockWorkspace   = "."
	mockConfig      = make(map[string]string)
	mockVirtualText = []VirtualTextItem{}
	mockGutterMarks = []GutterMarker{}
	mockBookmarks   = []Bookmark{}
	mockCodeLenses  = make(map[string][]CodeLensItem)
	mockDiagnostics = make(map[string][]DiagnosticItem)
	mockDataGrids   = make(map[string]DataGridDef)
	mockTreeViews   = make(map[string]TreeViewDef)
	mockChatViews   = make(map[string]ChatViewDef)
	mockSplitViews  = make(map[string]SplitViewDef)
	mockNotebooks   = make(map[string]NotebookDef)
	mockConnections = []ConnectionProfile{}
)

// SetMockDocument sets the in-memory document for local testing of plugin logic.
func SetMockDocument(lines []string) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockLines = append([]string(nil), lines...)
	mockPos = Position{Line: 0, Column: 0}
}

// GetMockLogs returns all logged messages recorded in native mode.
func GetMockLogs() []string {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]string(nil), mockLogs...)
}

// GetMockToasts returns all toasts recorded in native mode.
func GetMockToasts() []string {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]string(nil), mockToasts...)
}

// GetMockVirtualText returns all registered virtual text decorations in native mode.
func GetMockVirtualText() []VirtualTextItem {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]VirtualTextItem(nil), mockVirtualText...)
}

// GetMockGutterMarkers returns all registered gutter glyphs in native mode.
func GetMockGutterMarkers() []GutterMarker {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]GutterMarker(nil), mockGutterMarks...)
}

// GetMockBookmarks returns all registered bookmarks in native mode.
func GetMockBookmarks() []Bookmark {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]Bookmark(nil), mockBookmarks...)
}

// GetMockDiagnostics returns all registered diagnostics for a file in native mode.
func GetMockDiagnostics(filePath string) []DiagnosticItem {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]DiagnosticItem(nil), mockDiagnostics[filePath]...)
}


func bridgeLog(msg string) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockLogs = append(mockLogs, msg)
}

func bridgeShowToast(level int, title, message string) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockToasts = append(mockToasts, fmt.Sprintf("[%d] %s: %s", level, title, message))
}

func bridgeGetTotalLines() int {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return len(mockLines)
}

func bridgeGetLine(line int) (string, error) {
	mockMu.RLock()
	defer mockMu.RUnlock()
	if line < 0 || line >= len(mockLines) {
		return "", fmt.Errorf("line out of range: %d", line)
	}
	return mockLines[line], nil
}

func bridgeInsertText(line, col int, text string) error {
	mockMu.Lock()
	defer mockMu.Unlock()
	if line < 0 || line >= len(mockLines) {
		return fmt.Errorf("line out of range: %d", line)
	}
	cur := mockLines[line]
	if col > len(cur) {
		col = len(cur)
	}
	mockLines[line] = cur[:col] + text + cur[col:]
	return nil
}

func bridgeDeleteRange(startLine, startCol, endLine, endCol int) error {
	mockMu.Lock()
	defer mockMu.Unlock()
	if startLine < 0 || startLine >= len(mockLines) || endLine < startLine || endLine >= len(mockLines) {
		return fmt.Errorf("invalid range")
	}
	if startLine == endLine {
		cur := mockLines[startLine]
		if startCol < len(cur) && endCol <= len(cur) && startCol <= endCol {
			mockLines[startLine] = cur[:startCol] + cur[endCol:]
		}
	}
	return nil
}

func bridgeGetCursor() Position {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return mockPos
}

func bridgeSetCursor(line, col int) error {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockPos = Position{Line: line, Column: col}
	return nil
}

func bridgeGetActiveFilePath() string {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return mockFilePath
}

func bridgeGetWorkspaceRoot() string {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return mockWorkspace
}

func bridgeSave() error {
	return nil
}

func bridgePrompt(title, placeholder string) (string, error) {
	return "test_value", nil
}

func bridgeConfirm(title, message string) (bool, error) {
	return true, nil
}

func bridgeExec(cmd string) (int, error) {
	return 0, nil
}

func bridgeGetConfig(key string) string {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return mockConfig[key]
}

func bridgeSetConfig(key, value string) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockConfig[key] = value
}

func bridgeRegisterCommand(id string) {}

func bridgeRegisterToolWindow(id, title, icon, pos string) {}

func bridgeRegisterStatusBarItem(id, text string) {}

func bridgeSetVirtualText(items []VirtualTextItem) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockVirtualText = append([]VirtualTextItem(nil), items...)
}

func bridgeClearVirtualText(source string) {
	mockMu.Lock()
	defer mockMu.Unlock()
	if source == "" {
		mockVirtualText = nil
		return
	}
	filtered := make([]VirtualTextItem, 0, len(mockVirtualText))
	for _, it := range mockVirtualText {
		if it.Source != source {
			filtered = append(filtered, it)
		}
	}
	mockVirtualText = filtered
}

func bridgeAddVirtualText(item VirtualTextItem) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockVirtualText = append(mockVirtualText, item)
}

func bridgeSetGutterMarkers(markers []GutterMarker) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockGutterMarks = append([]GutterMarker(nil), markers...)
}

func bridgeClearGutterMarkers(source string) {
	mockMu.Lock()
	defer mockMu.Unlock()
	if source == "" {
		mockGutterMarks = nil
		return
	}
	filtered := make([]GutterMarker, 0, len(mockGutterMarks))
	for _, m := range mockGutterMarks {
		if m.Source != source {
			filtered = append(filtered, m)
		}
	}
	mockGutterMarks = filtered
}

func bridgeAddGutterMarker(marker GutterMarker) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockGutterMarks = append(mockGutterMarks, marker)
}

func bridgeToggleBookmark(filePath string, line int, label string) bool {
	mockMu.Lock()
	defer mockMu.Unlock()
	for i, b := range mockBookmarks {
		if b.FilePath == filePath && b.Line == line {
			mockBookmarks = append(mockBookmarks[:i], mockBookmarks[i+1:]...)
			return false
		}
	}
	mockBookmarks = append(mockBookmarks, Bookmark{FilePath: filePath, Line: line, Label: label})
	return true
}

func bridgeGetBookmarks() []Bookmark {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]Bookmark(nil), mockBookmarks...)
}

func bridgeClearBookmarks() {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockBookmarks = nil
}

func bridgeSetCodeLens(filePath string, lenses []CodeLensItem) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockCodeLenses[filePath] = append([]CodeLensItem(nil), lenses...)
}

func bridgeGetCodeLens(filePath string) []CodeLensItem {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]CodeLensItem(nil), mockCodeLenses[filePath]...)
}

func bridgePublishDiagnostics(filePath string, source string, diags []DiagnosticItem) {
	mockMu.Lock()
	defer mockMu.Unlock()
	cur := mockDiagnostics[filePath]
	filtered := make([]DiagnosticItem, 0, len(cur))
	for _, d := range cur {
		if d.Source != source {
			filtered = append(filtered, d)
		}
	}
	filtered = append(filtered, diags...)
	mockDiagnostics[filePath] = filtered
}

func bridgeClearDiagnostics(filePath string, source string) {
	mockMu.Lock()
	defer mockMu.Unlock()
	if source == "" {
		delete(mockDiagnostics, filePath)
		return
	}
	cur := mockDiagnostics[filePath]
	filtered := make([]DiagnosticItem, 0, len(cur))
	for _, d := range cur {
		if d.Source != source {
			filtered = append(filtered, d)
		}
	}
	mockDiagnostics[filePath] = filtered
}

func bridgeGetDiagnostics(filePath string) []DiagnosticItem {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]DiagnosticItem(nil), mockDiagnostics[filePath]...)
}

func bridgeSetDataGrid(grid DataGridDef) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockDataGrids[grid.ID] = grid
}

func bridgeGetDataGrid(id string) (DataGridDef, bool) {
	mockMu.RLock()
	defer mockMu.RUnlock()
	g, ok := mockDataGrids[id]
	return g, ok
}

func bridgeSetTreeView(tree TreeViewDef) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockTreeViews[tree.ID] = tree
}

func bridgeGetTreeView(id string) (TreeViewDef, bool) {
	mockMu.RLock()
	defer mockMu.RUnlock()
	t, ok := mockTreeViews[id]
	return t, ok
}

func bridgeSetChatView(chat ChatViewDef) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockChatViews[chat.ID] = chat
}

func bridgeAppendChatMessage(chatID string, msg ChatMessage) {
	mockMu.Lock()
	defer mockMu.Unlock()
	c := mockChatViews[chatID]
	c.Messages = append(c.Messages, msg)
	mockChatViews[chatID] = c
}

func bridgeGetChatView(id string) (ChatViewDef, bool) {
	mockMu.RLock()
	defer mockMu.RUnlock()
	c, ok := mockChatViews[id]
	return c, ok
}

func bridgeSetSplitView(split SplitViewDef) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockSplitViews[split.ID] = split
}

func bridgeGetSplitView(id string) (SplitViewDef, bool) {
	mockMu.RLock()
	defer mockMu.RUnlock()
	s, ok := mockSplitViews[id]
	return s, ok
}

func bridgeSetNotebookView(nb NotebookDef) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockNotebooks[nb.ID] = nb
}

func bridgeGetNotebookView(id string) (NotebookDef, bool) {
	mockMu.RLock()
	defer mockMu.RUnlock()
	nb, ok := mockNotebooks[id]
	return nb, ok
}

func bridgeUpdateNotebookCell(notebookID string, cell NotebookCell) {
	mockMu.Lock()
	defer mockMu.Unlock()
	nb, ok := mockNotebooks[notebookID]
	if !ok {
		return
	}
	for i, c := range nb.Cells {
		if c.ID == cell.ID {
			nb.Cells[i] = cell
			mockNotebooks[notebookID] = nb
			return
		}
	}
	nb.Cells = append(nb.Cells, cell)
	mockNotebooks[notebookID] = nb
}

func bridgeSetConnectionProfiles(profiles []ConnectionProfile) {
	mockMu.Lock()
	defer mockMu.Unlock()
	mockConnections = append([]ConnectionProfile(nil), profiles...)
}

func bridgeGetConnectionProfiles() []ConnectionProfile {
	mockMu.RLock()
	defer mockMu.RUnlock()
	return append([]ConnectionProfile(nil), mockConnections...)
}


