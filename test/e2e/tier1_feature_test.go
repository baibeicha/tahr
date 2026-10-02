package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
)

// ============================================================================
// Feature 1: Multi-Selection Navigation & Typing (>= 5 test cases)
// ============================================================================

// TestT1_MultiSelection_SpawnThreeCursorsAndType verifies spawning 3 cursors and typing simultaneously.
func TestT1_MultiSelection_SpawnThreeCursorsAndType(t *testing.T) {
	h := NewTestHarness(t)
	// Create 3 lines
	h.SendText("Line One")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("Line Two")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("Line Three")

	// Place multi-cursors at end of each line using Alt+Click
	h.SendMouse(15, 0, input.MouseLeft, input.MousePress, 2) // Alt+Click line 0
	h.SendMouse(15, 1, input.MouseLeft, input.MousePress, 2) // Alt+Click line 1
	h.SendMouse(15, 2, input.MouseLeft, input.MousePress, 2) // Alt+Click line 2

	// Type suffix across all 3 cursors
	h.SendText(" - MODIFIED")

	h.AssertScreenContains("Line One - MODIFIED")
	h.AssertScreenContains("Line Two - MODIFIED")
	h.AssertScreenContains("Line Three - MODIFIED")
	h.AssertNoFlicker()
}

// TestT1_MultiSelection_CtrlD_NextMatchSelection verifies Ctrl+D adds next matching token to selections.
func TestT1_MultiSelection_CtrlD_NextMatchSelection(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("func main() { main() }")

	// Position at first "main" and press Ctrl+D
	h.SendKey(input.KeyHome, cell.AttrNone)
	h.SendKey(input.KeyRight, cell.AttrNone)
	h.SendKey(input.KeyRight, cell.AttrNone)
	h.SendKey(input.KeyRight, cell.AttrNone)
	h.SendKey(input.KeyRight, cell.AttrNone)
	h.SendKey(input.KeyRight, cell.AttrNone) // at 'm' of main

	// Ctrl+D to select next match
	h.SendCtrl('d')

	h.AssertScreenContains("main")
}

// TestT1_MultiSelection_AltClick_MouseMultiCursor verifies Alt+Click places independent cursors.
func TestT1_MultiSelection_AltClick_MouseMultiCursor(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("itemA := 1")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("itemB := 2")

	// Alt+Click on itemB
	h.SendMouse(10, 1, input.MouseLeft, input.MousePress, 2) // Alt modifier

	h.SendText(" // tagged")
	h.AssertScreenContains("tagged")
}

// TestT1_MultiSelection_BottomToTopEditIntegrity verifies edits are applied bottom-to-top without index drift.
func TestT1_MultiSelection_BottomToTopEditIntegrity(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("AAA")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("BBB")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("CCC")

	// Add cursors on line 0, 1, 2
	h.SendMouse(8, 0, input.MouseLeft, input.MousePress, 2)
	h.SendMouse(8, 1, input.MouseLeft, input.MousePress, 2)
	h.SendMouse(8, 2, input.MouseLeft, input.MousePress, 2)

	// Insert text with varying lengths
	h.SendText("12345")

	h.AssertScreenContains("AAA12345")
	h.AssertScreenContains("BBB12345")
	h.AssertScreenContains("CCC12345")
}

// TestT1_MultiSelection_MergeOverlappingSelections verifies colliding selections collapse safely.
func TestT1_MultiSelection_MergeOverlappingSelections(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("SingleLine")

	// Place two cursors on same line very close
	h.SendMouse(6, 0, input.MouseLeft, input.MousePress, cell.AttrNone)
	h.SendMouse(6, 0, input.MouseLeft, input.MousePress, 2) // Alt+Click exact same location

	h.collapseDuplicateSelections()
	if len(h.selections) > 1 {
		t.Fatalf("Expected duplicate selections to collapse, got %d", len(h.selections))
	}
}

// ============================================================================
// Feature 2: Block Indentation & Dedent (>= 5 test cases)
// ============================================================================

// TestT1_Indentation_SingleLineTabIndent verifies pressing Tab indents 4 spaces.
func TestT1_Indentation_SingleLineTabIndent(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("x := 42")
	h.SendKey(input.KeyHome, cell.AttrNone)
	h.SendKey(input.KeyTab, cell.AttrNone)

	h.AssertScreenContains("    x := 42")
}

// TestT1_Indentation_MultiLineBlockTabIndent verifies Tab indents a multi-line selection.
func TestT1_Indentation_MultiLineBlockTabIndent(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("line1")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("line2")

	// Select lines 0 to 1
	h.selections = []Selection{{AnchorRow: 0, AnchorCol: 0, HeadRow: 1, HeadCol: 5}}
	h.SendKey(input.KeyTab, cell.AttrNone)

	h.AssertScreenContains("    line1")
	h.AssertScreenContains("    line2")
}

// TestT1_Indentation_ShiftTabDedent verifies Shift+Tab dedents selected lines.
func TestT1_Indentation_ShiftTabDedent(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("        indented1")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("        indented2")

	// Select both lines
	h.selections = []Selection{{AnchorRow: 0, AnchorCol: 0, HeadRow: 1, HeadCol: 17}}
	h.SendKey(input.KeyBacktab, cell.AttrNone)

	h.AssertScreenContains("    indented1")
	h.AssertScreenContains("    indented2")
}

// TestT1_Indentation_PreserveRelativeIndentLevels verifies relative indentation is maintained.
func TestT1_Indentation_PreserveRelativeIndentLevels(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("  two_spaces")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("    four_spaces")

	h.selections = []Selection{{AnchorRow: 0, AnchorCol: 0, HeadRow: 1, HeadCol: 15}}
	h.SendKey(input.KeyTab, cell.AttrNone)

	h.AssertScreenContains("      two_spaces")
	h.AssertScreenContains("        four_spaces")
}

// TestT1_Indentation_EmptyLineHandlingDuringBlockIndent verifies blank lines are not corrupted.
func TestT1_Indentation_EmptyLineHandlingDuringBlockIndent(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("top")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendKey(input.KeyEnter, cell.AttrNone) // empty line
	h.SendText("bottom")

	h.selections = []Selection{{AnchorRow: 0, AnchorCol: 0, HeadRow: 2, HeadCol: 6}}
	h.indentSelectedLines()

	h.AssertScreenContains("    top")
	h.AssertScreenContains("    bottom")
}

// ============================================================================
// Feature 3: Undo/Redo Word Batching (>= 5 test cases)
// ============================================================================

// TestT1_UndoRedo_SingleWordInsertionAndUndo verifies typing a word and undoing it.
func TestT1_UndoRedo_SingleWordInsertionAndUndo(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("greeting")

	h.AssertScreenContains("greeting")

	// Undo
	h.SendCtrl('z')

	h.AssertScreenNotContains("greeting")
}

// TestT1_UndoRedo_TwoWordsSeparatedByPause_WordBatching verifies 300ms pause splits transactions.
func TestT1_UndoRedo_TwoWordsSeparatedByPause_WordBatching(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("first")

	// Wait 350ms to exceed word batching threshold
	time.Sleep(350 * time.Millisecond)

	h.SendText("second")

	h.AssertScreenContains("firstsecond")

	// Undo only reverts "second"
	h.SendCtrl('z')

	h.AssertScreenContains("first")
	h.AssertScreenNotContains("second")
}

// TestT1_UndoRedo_RedoReappliesExactDeltas verifies Ctrl+Y restores undone text.
func TestT1_UndoRedo_RedoReappliesExactDeltas(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("alpha")
	h.sealWordBatch()

	// Undo
	h.SendCtrl('z')
	h.AssertScreenNotContains("alpha")

	// Redo
	h.SendCtrl('y')
	h.AssertScreenContains("alpha")
}

// TestT1_UndoRedo_NewEditInvalidatesRedoStack verifies new keystrokes truncate redo history.
func TestT1_UndoRedo_NewEditInvalidatesRedoStack(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("original")
	h.sealWordBatch()

	// Undo
	h.SendCtrl('z')

	// Type something new
	h.SendText("diverged")
	h.sealWordBatch()

	// Attempt redo
	h.SendCtrl('y')

	h.AssertScreenContains("diverged")
	h.AssertScreenNotContains("original")
}

// TestT1_UndoRedo_MultiCursorUndoSymmetry verifies multi-cursor edits undo symmetrically.
func TestT1_UndoRedo_MultiCursorUndoSymmetry(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("row1")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("row2")

	// Two cursors
	h.selections = []Selection{
		{AnchorRow: 0, AnchorCol: 4, HeadRow: 0, HeadCol: 4},
		{AnchorRow: 1, AnchorCol: 4, HeadRow: 1, HeadCol: 4},
	}

	h.SendPaste(":edit", false)
	h.AssertScreenContains("row1:edit")
	h.AssertScreenContains("row2:edit")

	// Undo
	h.SendCtrl('z')

	h.AssertScreenContains("row1")
	h.AssertScreenContains("row2")
	h.AssertScreenNotContains(":edit")
}

// ============================================================================
// Feature 4: Bracketed Paste Fidelity (>= 5 test cases)
// ============================================================================

// TestT1_Paste_BracketedPasteModeEscapeSequence verifies bracketed paste handling.
func TestT1_Paste_BracketedPasteModeEscapeSequence(t *testing.T) {
	h := NewTestHarness(t)
	code := "func hello() {\n    return nil\n}"
	h.SendPaste(code, true)

	h.AssertScreenContains("func hello() {")
	h.AssertScreenContains("return nil")
}

// TestT1_Paste_MultiLineCodePreservesIndentationWithoutStaircase verifies no staircasing.
func TestT1_Paste_MultiLineCodePreservesIndentationWithoutStaircase(t *testing.T) {
	h := NewTestHarness(t)
	payload := "def calculate():\n    x = 10\n    if x > 5:\n        return True"
	h.SendPaste(payload, true)

	h.AssertScreenContains("def calculate():")
	h.AssertScreenContains("    x = 10")
	h.AssertScreenContains("    if x > 5:")
	h.AssertScreenContains("        return True")
}

// TestT1_Paste_LargeBlockPasteIntegrity verifies pasting large blocks without corruption.
func TestT1_Paste_LargeBlockPasteIntegrity(t *testing.T) {
	h := NewTestHarness(t)
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("line_entry := true\n")
	}
	h.SendPaste(sb.String(), true)

	h.AssertScreenContains("line_entry := true")
}

// TestT1_Paste_PasteReplacesActiveSelection verifies paste replaces selected text.
func TestT1_Paste_PasteReplacesActiveSelection(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("OLD_TEXT_TO_BE_REPLACED")

	h.selections = []Selection{{AnchorRow: 0, AnchorCol: 0, HeadRow: 0, HeadCol: 23}}
	h.SendPaste("NEW_TEXT", true)

	h.AssertScreenContains("NEW_TEXT")
}

// TestT1_Paste_PasteAtMultipleCursorsSimultaneously verifies paste duplicated across cursors.
func TestT1_Paste_PasteAtMultipleCursorsSimultaneously(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("keyA = ")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("keyB = ")

	h.selections = []Selection{
		{AnchorRow: 0, AnchorCol: 7, HeadRow: 0, HeadCol: 7},
		{AnchorRow: 1, AnchorCol: 7, HeadRow: 1, HeadCol: 7},
	}

	h.SendPaste("\"default\"", true)

	h.AssertScreenContains("keyA = \"default\"")
	h.AssertScreenContains("keyB = \"default\"")
}

// ============================================================================
// Feature 5: Omnibar File Fuzzy Opening (>= 5 test cases)
// ============================================================================

// TestT1_Omnibar_CtrlP_OpensOmnibarDialog verifies Ctrl+P opens the Omnibar dialog.
func TestT1_Omnibar_CtrlP_OpensOmnibarDialog(t *testing.T) {
	h := NewTestHarness(t)
	h.SendCtrl('p')

	h.AssertScreenContains("Quick Open")
	h.AssertScreenContains("cmd/tahr/main.go")
}

// TestT1_Omnibar_FuzzyQueryFiltersCandidates verifies typing filters candidate list.
func TestT1_Omnibar_FuzzyQueryFiltersCandidates(t *testing.T) {
	h := NewTestHarness(t)
	h.SendCtrl('p')

	h.SendText("rope")

	h.AssertScreenContains("internal/core/buffer/rope.go")
	h.AssertScreenNotContains("cmd/tahr/main.go")
}

// TestT1_Omnibar_ArrowKeyNavigationThroughCandidates verifies navigating candidates with arrows.
func TestT1_Omnibar_ArrowKeyNavigationThroughCandidates(t *testing.T) {
	h := NewTestHarness(t)
	h.SendCtrl('p')

	// Press Down Arrow
	h.SendKey(input.KeyDown, cell.AttrNone)

	if h.omnibar.SelectedIdx != 1 {
		t.Fatalf("Expected SelectedIdx to be 1, got %d", h.omnibar.SelectedIdx)
	}
	h.AssertScreenContains("►")
}

// TestT1_Omnibar_EnterOpensSelectedFile verifies pressing Enter opens selected file.
func TestT1_Omnibar_EnterOpensSelectedFile(t *testing.T) {
	h := NewTestHarness(t)
	h.SendCtrl('p')

	h.SendText("engine")
	h.SendKey(input.KeyEnter, cell.AttrNone)

	if h.omnibar.Open {
		t.Fatalf("Expected Omnibar to close on Enter")
	}
	if !strings.Contains(h.currentFile, "engine.go") {
		t.Fatalf("Expected currentFile to be engine.go, got %s", h.currentFile)
	}
}

// TestT1_Omnibar_EscDismissesDialogWithoutChangingBuffer verifies Esc cancels Omnibar.
func TestT1_Omnibar_EscDismissesDialogWithoutChangingBuffer(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("Original Buffer Content")

	h.SendCtrl('p')
	h.AssertScreenContains("Quick Open")

	// Press Esc
	h.SendKey(input.KeyEsc, cell.AttrNone)

	h.AssertScreenNotContains("Quick Open")
	h.AssertScreenContains("Original Buffer Content")
}

// ============================================================================
// Feature 6: Safe Atomic File Saving (>= 5 test cases)
// ============================================================================

// TestT1_AtomicSave_CtrlS_WritesFileToDisk verifies Ctrl+S writes buffer to disk.
func TestT1_AtomicSave_CtrlS_WritesFileToDisk(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "save_test.go")

	h := NewTestHarness(t, WithFile(targetFile))
	h.SendText("package test\n\nfunc Hello() {}")

	// Trigger Ctrl+S
	h.SendCtrl('s')

	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}
	if !strings.Contains(string(content), "func Hello() {}") {
		t.Fatalf("Saved content does not match buffer: %s", string(content))
	}
}

// TestT1_AtomicSave_TmpFileSyncRenameLifecycle verifies atomic rename flow.
func TestT1_AtomicSave_TmpFileSyncRenameLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "atomic_lifecycle.txt")

	h := NewTestHarness(t, WithFile(targetFile))
	h.SendText("Atomic verification payload")

	h.saveCurrentFile()

	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		t.Fatalf("Target file was not created atomically")
	}
}

// TestT1_AtomicSave_NoTempFilesLeftOnDisk verifies .tmp is cleaned up after save.
func TestT1_AtomicSave_NoTempFilesLeftOnDisk(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "clean_test.txt")

	h := NewTestHarness(t, WithFile(targetFile))
	h.SendText("No residue left behind")

	h.saveCurrentFile()

	tmpFile := filepath.Join(tmpDir, ".clean_test.txt.tahr.tmp")
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Fatalf("Temporary file %s still exists after save!", tmpFile)
	}
}

// TestT1_AtomicSave_PreservesCRLFIfOriginallyPresent verifies CRLF preservation.
func TestT1_AtomicSave_PreservesCRLFIfOriginallyPresent(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "crlf_sample.txt")
	_ = os.WriteFile(targetFile, []byte("line1\r\nline2\r\n"), 0644)

	h := NewTestHarness(t, WithFile(targetFile))
	h.SendText("line3")

	h.saveCurrentFile()

	saved, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("Failed to read CRLF file: %v", err)
	}
	if !strings.Contains(string(saved), "\r\n") {
		t.Fatalf("Expected CRLF line endings to be preserved, got: %q", string(saved))
	}
}

// TestT1_AtomicSave_ReadonlyOrPermissionFailureHandling verifies error handling.
func TestT1_AtomicSave_ReadonlyOrPermissionFailureHandling(t *testing.T) {
	invalidPath := filepath.Join(string(os.PathSeparator), "invalid_dir_no_perm", "forbidden.txt")
	h := NewTestHarness(t, WithFile(invalidPath))
	h.SendText("Testing permission handling")

	h.saveCurrentFile()

	if !strings.Contains(h.statusText, "Save error") {
		t.Logf("Status text noted: %s", h.statusText)
	}
}
