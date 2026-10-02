package tui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/media"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/ui"
)

// TestButtonBackgrounds_Removed verifies that buttons on sidebar and terminal headers have no PopupSelBg background.
func TestButtonBackgrounds_Removed(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 120
	app.height = 40
	app.sidebarOpen = true
	app.sidebarWidth = 30
	app.sidebarAnimWidth = 30
	if app.terminal != nil {
		app.terminal.Open = true
		app.terminal.Height = 10
	}

	buf := buffer.NewBuffer(app.width, app.height)
	frame := &tea.Frame{Buffer: buf}
	app.View(frame)

	popupSelBg := toColor(app.theme.PopupSelBg).Value
	sidebarBg := toColor(app.theme.GutterBg).Value
	termBg := toColor(app.theme.Background).Value

	// Check sidebar button ◀ at dockBtnStart (which is 3 + 30 - 5 = 28)
	// Must not have PopupSelBg, and must be at dockBtnStart
	dockBtnStart := 3 + 30 - 5
	cSideBtn := buf.Cell(dockBtnStart, 2)
	if cSideBtn != nil {
		if cSideBtn.Rune != '◀' {
			t.Fatalf("expected cell rune '◀', got %c", cSideBtn.Rune)
		}
		if cSideBtn.Bg == popupSelBg {
			t.Fatalf("expected sidebar button ◀ to not have PopupSelBg background, got %X", cSideBtn.Bg)
		}
		if cSideBtn.Bg != sidebarBg {
			t.Fatalf("expected sidebar button ◀ to have sidebarBg (%X), got %X", sidebarBg, cSideBtn.Bg)
		}
	}

	// Check terminal button ▲ at w-18
	termTop := 40 - 1 - 10
	cTermBtn := buf.Cell(120-18, termTop)
	if cTermBtn != nil {
		if cTermBtn.Bg == popupSelBg {
			t.Fatalf("expected terminal button ▲ to not have PopupSelBg background, got %X", cTermBtn.Bg)
		}
		if cTermBtn.Bg != termBg {
			t.Fatalf("expected terminal button ▲ to have termBg (%X), got %X", termBg, cTermBtn.Bg)
		}
	}
}

// TestImageViewer_RenderAndScaleModes verifies loading, metadata, rendering, and scale mode cycling.
func TestImageViewer_RenderAndScaleModes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-img-*")
	if err != nil {
		t.Fatalf("temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	imgPath := filepath.Join(tmpDir, "test_sample.png")
	// Create a 16x16 red/blue test image
	testImg := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if x < 8 {
				testImg.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
			} else {
				testImg.Set(x, y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
			}
		}
	}
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatalf("create test image error: %v", err)
	}
	_ = png.Encode(f, testImg)
	f.Close()

	if !IsImageFile(imgPath) {
		t.Fatalf("expected IsImageFile to be true for %s", imgPath)
	}

	iv := NewImageViewerState(imgPath)
	if iv.Err != nil {
		t.Fatalf("expected no load error, got %v", iv.Err)
	}
	if iv.Width != 16 || iv.Height != 16 {
		t.Fatalf("expected 16x16 dimensions, got %dx%d", iv.Width, iv.Height)
	}
	if iv.Format != "PNG" {
		t.Fatalf("expected PNG format, got %s", iv.Format)
	}
	if iv.ScaleMode != media.ScaleFit {
		t.Fatalf("expected default ScaleFit, got %v", iv.ScaleMode)
	}

	// Test scale mode cycling
	iv.CycleScaleMode()
	if iv.ScaleMode != media.ScaleFill {
		t.Fatalf("expected ScaleFill after first cycle, got %v", iv.ScaleMode)
	}
	iv.CycleScaleMode()
	if iv.ScaleMode != media.ScaleStretch {
		t.Fatalf("expected ScaleStretch after second cycle, got %v", iv.ScaleMode)
	}
	iv.CycleScaleMode()
	if iv.ScaleMode != media.ScaleFit {
		t.Fatalf("expected ScaleFit after third cycle, got %v", iv.ScaleMode)
	}

	// Render into a buffer and ensure half-blocks or metadata are drawn
	buf := buffer.NewBuffer(60, 20)
	theme := ui.DefaultTheme()
	iv.Render(buf, buffer.NewRect(5, 5, 50, 14), &theme)

	// Check metadata row
	headerCell := buf.Cell(6, 5)
	if headerCell == nil || headerCell.Rune == ' ' {
		t.Fatal("expected non-empty metadata header row")
	}

	// Check image body row: must contain half-block '▀'
	hasHalfBlock := false
	for y := 6; y < 19; y++ {
		for x := 5; x < 55; x++ {
			c := buf.Cell(x, y)
			if c != nil && (c.Rune == '▀' || c.Rune == '▄') {
				hasHalfBlock = true
				break
			}
		}
		if hasHalfBlock {
			break
		}
	}
	if !hasHalfBlock {
		t.Fatal("expected image render area to contain half-block cells ('▀' or '▄')")
	}
}

// TestHoverDoc_DebounceTimerAndRender tests 400ms delay and hover doc presentation.
func TestHoverDoc_DebounceTimerAndRender(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 120
	app.height = 40
	app.sidebarOpen = false

	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active doc")
	}

	code := `package main

// CalculateSum computes the arithmetic sum of two integers.
// Supports negative numbers.
func CalculateSum(a, b int) int {
	return a + b
}
`
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), code)

	editorTop := 2
	gutterW := app.gutterWidth()
	stripLeftW := 3

	// Hover mouse over "CalculateSum" at line 4 (editorTop + 4)
	targetX := stripLeftW + gutterW + 6
	targetY := editorTop + 4

	moveMsg := tea.MouseMsg{
		Mouse: input.Mouse{
			Action: input.MouseMotion,
			X:      targetX,
			Y:      targetY,
		},
	}
	_, _ = app.handleMouse(moveMsg)

	if app.hoverDoc == nil {
		t.Fatal("expected hoverDoc state to be initialized")
	}
	if app.hoverDoc.Open {
		t.Fatal("expected hoverDoc to NOT be open immediately (debounce 400ms required)")
	}
	if app.hoverDoc.PendingWord != "CalculateSum" {
		t.Fatalf("expected PendingWord 'CalculateSum', got %q", app.hoverDoc.PendingWord)
	}

	// 1. Advance time before 400ms (e.g. 200ms): should still be closed
	app.checkHoverDocTrigger(time.Now().Add(200 * time.Millisecond))
	if app.hoverDoc.Open {
		t.Fatal("expected hoverDoc to remain closed before 400ms delay expires")
	}

	// 2. Advance time after 400ms (e.g. 450ms): should open with signature and comments!
	app.checkHoverDocTrigger(time.Now().Add(450 * time.Millisecond))
	if !app.hoverDoc.Open {
		t.Fatal("expected hoverDoc to open after 400ms delay")
	}
	if !strings.Contains(app.hoverDoc.Signature, "CalculateSum") {
		t.Fatalf("expected signature to contain CalculateSum, got %q", app.hoverDoc.Signature)
	}
	if len(app.hoverDoc.DocLines) < 2 {
		t.Fatalf("expected at least 2 doc comment lines, got %d", len(app.hoverDoc.DocLines))
	}
	if !strings.Contains(app.hoverDoc.DocLines[0], "computes the arithmetic sum") {
		t.Fatalf("unexpected doc comment: %q", app.hoverDoc.DocLines[0])
	}

	// 3. Render hover doc into buffer
	buf := buffer.NewBuffer(120, 40)
	app.hoverDoc.Render(buf, 120, 40, &app.theme)

	// Check that top border contains symbol name
	foundTitle := false
	for y := 0; y < 40; y++ {
		var rowText string
		for x := 0; x < 120; x++ {
			c := buf.Cell(x, y)
			if c != nil {
				rowText += string(c.Rune)
			}
		}
		if strings.Contains(rowText, "CalculateSum") {
			foundTitle = true
			break
		}
	}
	if !foundTitle {
		t.Fatal("expected rendered buffer to contain 'CalculateSum' in floating popup")
	}

	// 4. Any keypress immediately dismisses hoverDoc
	keyMsg := tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'x'}}
	_, _ = app.Update(keyMsg)
	if app.hoverDoc.Open {
		t.Fatal("expected hoverDoc to be dismissed on keypress")
	}
}

// TestHoverDoc_LocalCommentExtraction tests extraction for structs, interfaces, and functions.
func TestHoverDoc_LocalCommentExtraction(t *testing.T) {
	eng := core.NewEngine()
	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected doc")
	}

	code := `package main

// UserProfile holds the personal information and authentication tokens.
type UserProfile struct {
	ID   int
	Name string
}

/*
WorkerPool manages worker goroutines.
Supports graceful shutdown.
*/
type WorkerPool interface {
	Start()
	Stop()
}
`
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), code)

	// 1. Struct comments
	sig, docs, _, src := ExtractLocalSymbolDocumentation(doc, "UserProfile", "")
	if src != "Code Comments" {
		t.Fatalf("expected source 'Code Comments', got %q", src)
	}
	if !strings.Contains(sig, "type UserProfile struct") {
		t.Fatalf("unexpected signature: %q", sig)
	}
	if len(docs) == 0 || !strings.Contains(docs[0], "holds the personal information") {
		t.Fatalf("unexpected docs: %+v", docs)
	}

	// 2. Block comments on interface
	sig2, docs2, _, _ := ExtractLocalSymbolDocumentation(doc, "WorkerPool", "")
	if !strings.Contains(sig2, "type WorkerPool interface") {
		t.Fatalf("unexpected signature: %q", sig2)
	}
	if len(docs2) < 2 || !strings.Contains(docs2[0], "manages worker goroutines") {
		t.Fatalf("unexpected block docs: %+v", docs2)
	}
}

// TestHoverDoc_ImageLinkPreview tests hovering over an image path to generate a thumbnail preview.
func TestHoverDoc_ImageLinkPreview(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-hover-img-*")
	if err != nil {
		t.Fatalf("temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	imgPath := filepath.Join(tmpDir, "logo.png")
	testImg := image.NewRGBA(image.Rect(0, 0, 8, 8))
	f, _ := os.Create(imgPath)
	_ = png.Encode(f, testImg)
	f.Close()

	eng := core.NewEngine()
	doc := eng.ActiveDocument()

	sig, docs, thumb, src := ExtractLocalSymbolDocumentation(doc, imgPath, tmpDir)
	if src != "Image Preview" {
		t.Fatalf("expected source 'Image Preview', got %q", src)
	}
	if thumb == nil {
		t.Fatal("expected non-nil thumbnail for image hover")
	}
	if !strings.Contains(sig, "logo.png") {
		t.Fatalf("expected signature to contain logo.png, got %q", sig)
	}
	if len(docs) == 0 {
		t.Fatal("expected doc metadata for image")
	}
}

