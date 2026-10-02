package tahr_sdk

import (
	"fmt"
	"strings"
	"sync"
)

var (
	registryMu sync.RWMutex
	commands   = make(map[string]func())
	toolWins   = make(map[string]func())
	configs    = make(map[string]string)
)

// Log writes a message to Tahr's developer log or editor status line.
func Log(msg string) {
	bridgeLog(msg)
}

// ShowToast pushes a floating notification card in Tahr's top-right corner.
func ShowToast(level ToastLevel, title, message string) {
	bridgeShowToast(int(level), title, message)
}

// ToastInfo shows an informational notification.
func ToastInfo(title, message string) {
	ShowToast(ToastLevelInfo, title, message)
}

// ToastSuccess shows a success notification with green border.
func ToastSuccess(title, message string) {
	ShowToast(ToastLevelSuccess, title, message)
}

// ToastWarn shows a warning notification with yellow border.
func ToastWarn(title, message string) {
	ShowToast(ToastLevelWarn, title, message)
}

// ToastError shows an error notification with red border.
func ToastError(title, message string) {
	ShowToast(ToastLevelError, title, message)
}

// TotalLines returns the total number of lines in the active document.
func TotalLines() int {
	return bridgeGetTotalLines()
}

// GetLine returns the content of the specified 0-based line.
func GetLine(line int) (string, error) {
	if line < 0 {
		return "", fmt.Errorf("invalid line index: %d", line)
	}
	return bridgeGetLine(line)
}

// GetText returns the entire text of the active document.
func GetText() string {
	total := TotalLines()
	lines := make([]string, total)
	for i := 0; i < total; i++ {
		l, _ := GetLine(i)
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

// InsertText inserts text at the specified line and column in the active document.
func InsertText(line, col int, text string) error {
	if line < 0 || col < 0 {
		return fmt.Errorf("invalid position (%d, %d)", line, col)
	}
	return bridgeInsertText(line, col, text)
}

// InsertAtCursor inserts text at the current cursor position.
func InsertAtCursor(text string) error {
	pos := GetCursor()
	return InsertText(pos.Line, pos.Column, text)
}

// DeleteRange deletes the text between (startLine, startCol) and (endLine, endCol).
func DeleteRange(startLine, startCol, endLine, endCol int) error {
	return bridgeDeleteRange(startLine, startCol, endLine, endCol)
}

// GetCursor returns the current primary cursor position in the active document.
func GetCursor() Position {
	return bridgeGetCursor()
}

// SetCursor moves the primary cursor to the specified position.
func SetCursor(line, col int) error {
	if line < 0 || col < 0 {
		return fmt.Errorf("invalid cursor coordinates (%d, %d)", line, col)
	}
	return bridgeSetCursor(line, col)
}

// GetActiveFilePath returns the filesystem path of the current open file.
func GetActiveFilePath() string {
	return bridgeGetActiveFilePath()
}

// GetWorkspaceRoot returns the absolute directory path of the active project root.
func GetWorkspaceRoot() string {
	return bridgeGetWorkspaceRoot()
}

// Save requests an atomic save of the currently active document.
func Save() error {
	return bridgeSave()
}

// Prompt displays an input modal asking the user for a text string.
func Prompt(title, placeholder string) (string, error) {
	return bridgePrompt(title, placeholder)
}

// Confirm displays a confirmation dialog returning true if user accepts.
func Confirm(title, message string) (bool, error) {
	return bridgeConfirm(title, message)
}

// Exec executes a system command if the plugin has the "process:exec" capability.
func Exec(cmd string) (int, error) {
	return bridgeExec(cmd)
}

// GetConfig reads a configuration key from plugin settings.
func GetConfig(key string) string {
	registryMu.RLock()
	val, ok := configs[key]
	registryMu.RUnlock()
	if ok {
		return val
	}
	return bridgeGetConfig(key)
}

// SetConfig updates a configuration key in plugin settings.
func SetConfig(key, value string) {
	registryMu.Lock()
	configs[key] = value
	registryMu.Unlock()
	bridgeSetConfig(key, value)
}

// RegisterCommand registers an action handler associated with a command ID.
func RegisterCommand(id string, handler func()) {
	registryMu.Lock()
	defer registryMu.Unlock()
	commands[id] = handler
	bridgeRegisterCommand(id)
}

// DispatchCommand executes a registered plugin command.
func DispatchCommand(id string) error {
	registryMu.RLock()
	handler, ok := commands[id]
	registryMu.RUnlock()

	if !ok {
		return fmt.Errorf("command not registered: %s", id)
	}
	handler()
	return nil
}

// RegisterToolWindow registers a custom tool window panel.
func RegisterToolWindow(id, title, icon, pos string, onRender func()) {
	registryMu.Lock()
	defer registryMu.Unlock()
	toolWins[id] = onRender
	bridgeRegisterToolWindow(id, title, icon, pos)
}

// RegisterStatusBarItem registers a custom item in Tahr's status bar.
func RegisterStatusBarItem(id, text string) {
	bridgeRegisterStatusBarItem(id, text)
}

// SetVirtualText replaces or registers virtual text decorations for the active buffer.
func SetVirtualText(items []VirtualTextItem) {
	bridgeSetVirtualText(items)
}

// ClearVirtualText removes virtual text decorations matching the source prefix.
func ClearVirtualText(source string) {
	bridgeClearVirtualText(source)
}

// AddVirtualText registers an individual virtual text item.
func AddVirtualText(item VirtualTextItem) {
	bridgeAddVirtualText(item)
}

// SetGutterMarkers registers gutter indicator glyphs.
func SetGutterMarkers(markers []GutterMarker) {
	bridgeSetGutterMarkers(markers)
}

// ClearGutterMarkers removes gutter glyphs matching the source prefix.
func ClearGutterMarkers(source string) {
	bridgeClearGutterMarkers(source)
}

// AddGutterMarker adds an individual gutter glyph.
func AddGutterMarker(marker GutterMarker) {
	bridgeAddGutterMarker(marker)
}

// ToggleBookmark toggles or creates a bookmark on the given file and line.
func ToggleBookmark(filePath string, line int, label string) bool {
	return bridgeToggleBookmark(filePath, line, label)
}

// GetBookmarks returns all saved workspace bookmarks.
func GetBookmarks() []Bookmark {
	return bridgeGetBookmarks()
}

// ClearBookmarks removes all workspace bookmarks.
func ClearBookmarks() {
	bridgeClearBookmarks()
}

// SetCodeLens registers clickable CodeLens actions for a file.
func SetCodeLens(filePath string, lenses []CodeLensItem) {
	bridgeSetCodeLens(filePath, lenses)
}

// GetCodeLens retrieves all active CodeLens items for a file.
func GetCodeLens(filePath string) []CodeLensItem {
	return bridgeGetCodeLens(filePath)
}

// PublishDiagnostics sends diagnostics/issues for a file to the Problems view.
func PublishDiagnostics(filePath string, source string, diags []DiagnosticItem) {
	bridgePublishDiagnostics(filePath, source, diags)
}

// ClearDiagnostics removes diagnostics for a file from a specific source.
func ClearDiagnostics(filePath string, source string) {
	bridgeClearDiagnostics(filePath, source)
}

// GetDiagnostics retrieves all diagnostics reported for a file.
func GetDiagnostics(filePath string) []DiagnosticItem {
	return bridgeGetDiagnostics(filePath)
}

// SetDataGrid sets or updates a tabular DataGrid view.
func SetDataGrid(grid DataGridDef) {
	bridgeSetDataGrid(grid)
}

// GetDataGrid retrieves the current DataGrid state by ID.
func GetDataGrid(id string) (DataGridDef, bool) {
	return bridgeGetDataGrid(id)
}

// SetTreeView sets or updates a hierarchical TreeView.
func SetTreeView(tree TreeViewDef) {
	bridgeSetTreeView(tree)
}

// GetTreeView retrieves the current TreeView by ID.
func GetTreeView(id string) (TreeViewDef, bool) {
	return bridgeGetTreeView(id)
}

// SetChatView sets or updates an AI Chat view.
func SetChatView(chat ChatViewDef) {
	bridgeSetChatView(chat)
}

// AppendChatMessage appends a new message to the chat view.
func AppendChatMessage(chatID string, msg ChatMessage) {
	bridgeAppendChatMessage(chatID, msg)
}

// GetChatView retrieves the current ChatView by ID.
func GetChatView(id string) (ChatViewDef, bool) {
	return bridgeGetChatView(id)
}

// SetSplitView sets or updates a comparative SplitView.
func SetSplitView(split SplitViewDef) {
	bridgeSetSplitView(split)
}

// GetSplitView retrieves the current SplitView by ID.
func GetSplitView(id string) (SplitViewDef, bool) {
	return bridgeGetSplitView(id)
}

// SetNotebookView sets or updates an interactive Notebook view.
func SetNotebookView(nb NotebookDef) {
	bridgeSetNotebookView(nb)
}

// GetNotebookView retrieves the current Notebook view by ID.
func GetNotebookView(id string) (NotebookDef, bool) {
	return bridgeGetNotebookView(id)
}

// UpdateNotebookCell updates an individual cell in a notebook view.
func UpdateNotebookCell(notebookID string, cell NotebookCell) {
	bridgeUpdateNotebookCell(notebookID, cell)
}

// SetConnectionProfiles sets or updates active connection profiles.
func SetConnectionProfiles(profiles []ConnectionProfile) {
	bridgeSetConnectionProfiles(profiles)
}

// GetConnectionProfiles returns all stored or detected connection profiles.
func GetConnectionProfiles() []ConnectionProfile {
	return bridgeGetConnectionProfiles()
}


