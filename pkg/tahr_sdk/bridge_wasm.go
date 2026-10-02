//go:build wasm

package tahr_sdk

import (
	"fmt"
	"unsafe"
)

// Host function imports from "env" module
//go:wasmimport env tahr_ui_log
func hostLog(ptr, len uint32)

//go:wasmimport env tahr_ui_toast
func hostToast(level uint32, tPtr, tLen, mPtr, mLen uint32)

//go:wasmimport env tahr_buffer_get_total_lines
func hostGetTotalLines() uint32

//go:wasmimport env tahr_buffer_get_line
func hostGetLine(line uint32, outPtr, maxLen uint32) uint32

//go:wasmimport env tahr_buffer_insert_text
func hostInsertText(line, col uint32, ptr, len uint32) uint32

//go:wasmimport env tahr_buffer_delete_range
func hostDeleteRange(startLine, startCol, endLine, endCol uint32) uint32

//go:wasmimport env tahr_buffer_get_cursor
func hostGetCursor(outPtr uint32) uint32

//go:wasmimport env tahr_buffer_set_cursor
func hostSetCursor(line, col uint32) uint32

//go:wasmimport env tahr_buffer_save
func hostSave() uint32

//go:wasmimport env tahr_sys_exec
func hostSysExec(cmdPtr, cmdLen uint32) uint32

func bridgeLog(msg string) {
	bytes := []byte(msg)
	if len(bytes) == 0 {
		return
	}
	hostLog(uint32(uintptr(unsafe.Pointer(&bytes[0]))), uint32(len(bytes)))
}

func bridgeShowToast(level int, title, message string) {
	tBytes := []byte(title)
	mBytes := []byte(message)
	var tPtr, mPtr uint32
	if len(tBytes) > 0 {
		tPtr = uint32(uintptr(unsafe.Pointer(&tBytes[0])))
	}
	if len(mBytes) > 0 {
		mPtr = uint32(uintptr(unsafe.Pointer(&mBytes[0])))
	}
	hostToast(uint32(level), tPtr, uint32(len(tBytes)), mPtr, uint32(len(mBytes)))
}

func bridgeGetTotalLines() int {
	return int(hostGetTotalLines())
}

func bridgeGetLine(line int) (string, error) {
	buf := make([]byte, 65536)
	bytesRead := hostGetLine(uint32(line), uint32(uintptr(unsafe.Pointer(&buf[0]))), uint32(len(buf)))
	if bytesRead == 0xFFFFFFFF {
		return "", fmt.Errorf("line not found or error reading line %d", line)
	}
	return string(buf[:bytesRead]), nil
}

func bridgeInsertText(line, col int, text string) error {
	bytes := []byte(text)
	if len(bytes) == 0 {
		return nil
	}
	res := hostInsertText(uint32(line), uint32(col), uint32(uintptr(unsafe.Pointer(&bytes[0]))), uint32(len(bytes)))
	if res != 0 {
		return fmt.Errorf("failed to insert text (code %d)", res)
	}
	return nil
}

func bridgeDeleteRange(startLine, startCol, endLine, endCol int) error {
	res := hostDeleteRange(uint32(startLine), uint32(startCol), uint32(endLine), uint32(endCol))
	if res != 0 {
		return fmt.Errorf("failed to delete range (code %d)", res)
	}
	return nil
}

func bridgeGetCursor() Position {
	var coords [2]uint32
	hostGetCursor(uint32(uintptr(unsafe.Pointer(&coords[0]))))
	return Position{
		Line:   int(coords[0]),
		Column: int(coords[1]),
	}
}

func bridgeSetCursor(line, col int) error {
	res := hostSetCursor(uint32(line), uint32(col))
	if res != 0 {
		return fmt.Errorf("failed to set cursor (code %d)", res)
	}
	return nil
}

func bridgeGetActiveFilePath() string {
	return ""
}

func bridgeGetWorkspaceRoot() string {
	return "."
}

func bridgeSave() error {
	res := hostSave()
	if res != 0 {
		return fmt.Errorf("failed to save document (code %d)", res)
	}
	return nil
}

func bridgePrompt(title, placeholder string) (string, error) {
	return "", nil
}

func bridgeConfirm(title, message string) (bool, error) {
	return true, nil
}

func bridgeExec(cmd string) (int, error) {
	bytes := []byte(cmd)
	if len(bytes) == 0 {
		return 0, nil
	}
	exitCode := hostSysExec(uint32(uintptr(unsafe.Pointer(&bytes[0]))), uint32(len(bytes)))
	return int(exitCode), nil
}

func bridgeGetConfig(key string) string {
	return ""
}

func bridgeSetConfig(key, value string) {}

func bridgeRegisterCommand(id string) {}

func bridgeRegisterToolWindow(id, title, icon, pos string) {}

func bridgeRegisterStatusBarItem(id, text string) {}

func bridgeSetVirtualText(items []VirtualTextItem) {}
func bridgeClearVirtualText(source string) {}
func bridgeAddVirtualText(item VirtualTextItem) {}
func bridgeSetGutterMarkers(markers []GutterMarker) {}
func bridgeClearGutterMarkers(source string) {}
func bridgeAddGutterMarker(marker GutterMarker) {}
func bridgeToggleBookmark(filePath string, line int, label string) bool { return false }
func bridgeGetBookmarks() []Bookmark { return nil }
func bridgeClearBookmarks() {}
func bridgeSetCodeLens(filePath string, lenses []CodeLensItem) {}
func bridgeGetCodeLens(filePath string) []CodeLensItem { return nil }
func bridgePublishDiagnostics(filePath string, source string, diags []DiagnosticItem) {}
func bridgeClearDiagnostics(filePath string, source string) {}
func bridgeGetDiagnostics(filePath string) []DiagnosticItem { return nil }

func bridgeSetDataGrid(grid DataGridDef) {}
func bridgeGetDataGrid(id string) (DataGridDef, bool) { return DataGridDef{}, false }
func bridgeSetTreeView(tree TreeViewDef) {}
func bridgeGetTreeView(id string) (TreeViewDef, bool) { return TreeViewDef{}, false }
func bridgeSetChatView(chat ChatViewDef) {}
func bridgeAppendChatMessage(chatID string, msg ChatMessage) {}
func bridgeGetChatView(id string) (ChatViewDef, bool) { return ChatViewDef{}, false }
func bridgeSetSplitView(split SplitViewDef) {}
func bridgeGetSplitView(id string) (SplitViewDef, bool) { return SplitViewDef{}, false }
func bridgeSetNotebookView(nb NotebookDef) {}
func bridgeGetNotebookView(id string) (NotebookDef, bool) { return NotebookDef{}, false }
func bridgeUpdateNotebookCell(notebookID string, cell NotebookCell) {}
func bridgeSetConnectionProfiles(profiles []ConnectionProfile) {}
func bridgeGetConnectionProfiles() []ConnectionProfile { return nil }


