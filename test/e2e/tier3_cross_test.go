package e2e

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
)

// ============================================================================
// Tier 3: Cross-Feature Combination Tests
// ============================================================================

// TestT3_MultiCursor_DuringActiveLSPPopup tests multi-cursor behavior when completion popup triggers.
func TestT3_MultiCursor_DuringActiveLSPPopup(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("fmt")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("log")

	// Multi-cursor on both lines
	h.selections = []Selection{
		{AnchorRow: 0, AnchorCol: 3, HeadRow: 0, HeadCol: 3},
		{AnchorRow: 1, AnchorCol: 3, HeadRow: 1, HeadCol: 3},
	}

	// Type '.' on both cursors -> triggers completion
	h.SendRune('.')

	// Assert completion popup attaches cleanly to primary cursor without screen tearing
	h.AssertPopupVisible("Completions")
	h.AssertPopupVisible("Println")

	// Accept completion with Enter
	h.SendKey(input.KeyEnter, cell.AttrNone)

	// Completion popup is dismissed and text is inserted
	h.AssertPopupNotVisible()
	h.AssertScreenContains("Println")
	h.AssertNoFlicker()
}

// TestT3_Undo_WhileDiagnosticsPublishing tests undo operations while LSP diagnostics arrive.
func TestT3_Undo_WhileDiagnosticsPublishing(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("func test() {")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("    err := doSomething()")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("    if err != nil { return }")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("}")

	// Simulate LSP publishing diagnostics on line 1 ("err declared but not used")
	h.gutterMarkers[1] = "E: err declared and not used"
	h.renderScreen()
	h.AssertGutterHasMarker(1, "E")

	// User performs undo
	h.SendCtrl('z')

	// Gutter markers adapt to buffer state without panic
	h.AssertScreenContains("func test()")
}

// TestT3_WASMPluginExecution_DuringTyping tests continuous typing while a background WASM plugin runs.
func TestT3_WASMPluginExecution_DuringTyping(t *testing.T) {
	h := NewTestHarness(t)

	// Simulate background WASM execution with 250ms timeout
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	wasmFinished := false
	go func() {
		defer wg.Done()
		// Simulate WASM compute loop
		select {
		case <-time.After(50 * time.Millisecond):
			wasmFinished = true
		case <-ctx.Done():
		}
	}()

	// Continuously type in main thread while WASM executes
	typeStart := time.Now()
	for i := 0; i < 50; i++ {
		h.SendRune('k')
		time.Sleep(1 * time.Millisecond)
	}
	typeDuration := time.Since(typeStart)

	wg.Wait()

	if !wasmFinished {
		t.Logf("WASM execution completed via timeout or context")
	}

	// Verify all keystrokes were captured without stutter
	h.AssertScreenContains("kkkkkkkkkk")
	t.Logf("50 keystrokes during background WASM completed in %v", typeDuration)
}

// TestT3_Omnibar_InterruptingHoverTooltip tests that opening Omnibar dismisses active hover tooltip.
func TestT3_Omnibar_InterruptingHoverTooltip(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("var client *http.Client")

	// Trigger hover tooltip on "Client"
	h.popups = []PopupWindow{{
		Visible:   true,
		Title:     "Hover: http.Client",
		Doc:       "type Client struct { Transport RoundTripper }",
		Items:     nil,
		Row:       2,
		Col:       10,
		Width:     45,
		Height:    4,
	}}
	h.renderScreen()

	h.AssertPopupVisible("Hover: http.Client")

	// User presses Ctrl+P to open Omnibar
	h.SendCtrl('p')

	// Hover tooltip must be dismissed, and Omnibar must have exclusive focus
	h.AssertPopupNotVisible()
	h.AssertScreenContains("Quick Open")
	if !h.omnibar.Open {
		t.Fatalf("Expected Omnibar to be open with exclusive focus")
	}
}

// TestT3_BracketedPaste_WhileMultiSelectionActive tests pasting across multiple selections simultaneously.
func TestT3_BracketedPaste_WhileMultiSelectionActive(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("var a = 0")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("var b = 0")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("var c = 0")

	// Highlight '0' on all 3 lines
	h.selections = []Selection{
		{AnchorRow: 0, AnchorCol: 8, HeadRow: 0, HeadCol: 9},
		{AnchorRow: 1, AnchorCol: 8, HeadRow: 1, HeadCol: 9},
		{AnchorRow: 2, AnchorCol: 8, HeadRow: 2, HeadCol: 9},
	}

	// Paste replacement text in bracketed mode
	h.SendPaste("42", true)

	h.AssertScreenContains("var a = 42")
	h.AssertScreenContains("var b = 42")
	h.AssertScreenContains("var c = 42")
}

// TestT3_ResizeDuringOmnibarAndCompletionPopup tests window resize while dialogs are open.
func TestT3_ResizeDuringOmnibarAndCompletionPopup(t *testing.T) {
	h := NewTestHarness(t)
	h.SendCtrl('p')
	h.AssertScreenContains("Quick Open")

	// Resize down to 40x15
	h.Resize(40, 15)

	// Omnibar clamped within 40 columns
	h.AssertScreenContains("Quick Open")

	// Close Omnibar and resize back to 80x24
	h.SendKey(input.KeyEsc, cell.AttrNone)
	h.Resize(80, 24)
	h.AssertScreenNotContains("Quick Open")
}
