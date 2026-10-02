package tui

import (
	"math"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/ui"
)

func TestColorPicker_HSVConversion(t *testing.T) {
	// Test pure red: H=0, S=1, V=1
	cRed := HSVToRGB(0, 1.0, 1.0)
	if cRed.R() != 255 || cRed.G() != 0 || cRed.B() != 0 {
		t.Errorf("expected pure red (255, 0, 0), got (%d, %d, %d)", cRed.R(), cRed.G(), cRed.B())
	}

	// Test RGBToHSV on pure green
	h, s, v := RGBToHSV(0, 255, 0)
	if math.Abs(h-120) > 1.0 || math.Abs(s-1.0) > 0.01 || math.Abs(v-1.0) > 0.01 {
		t.Errorf("expected H=120, S=1.0, V=1.0 for pure green, got H=%.1f, S=%.2f, V=%.2f", h, s, v)
	}

	// Test Hex parsing
	cHex, ok := HexToRGBColor("#89b4fa")
	if !ok || cHex.R() != 0x89 || cHex.G() != 0xb4 || cHex.B() != 0xfa {
		t.Errorf("failed parsing #89b4fa: got %v, ok=%v", cHex, ok)
	}
}

func TestColorPicker_Interaction(t *testing.T) {
	cp := NewColorPickerModal()
	var appliedKey, appliedVal string
	applyFn := func(k, v string) {
		appliedKey = k
		appliedVal = v
	}

	cp.OpenForColor("function", "Function", "#89b4fa", applyFn)
	if !cp.Open {
		t.Fatalf("expected color picker to be open")
	}

	// Press Right to shift hue
	initialHue := cp.Hue
	cp.HandleKey(input.Key{Type: input.KeyRight})
	if cp.Hue <= initialHue && initialHue < 350 {
		t.Errorf("expected hue to increase on right key")
	}

	// Press Enter to apply
	cp.HandleKey(input.Key{Type: input.KeyEnter})
	if cp.Open {
		t.Errorf("expected color picker to close on Enter")
	}
	if appliedKey != "function" || appliedVal == "" {
		t.Errorf("expected apply callback to receive key and val, got %s: %s", appliedKey, appliedVal)
	}
}

func TestColorPicker_Render(t *testing.T) {
	cp := NewColorPickerModal()
	cp.OpenForColor("keyword", "Keyword", "#cba6f7", nil)
	buf := buffer.NewBuffer(80, 30)
	th := ui.DefaultTheme()

	cp.Render(buf, 80, 30, th)
	if !cp.Open {
		t.Errorf("expected cp to remain open after render")
	}
}

func TestColorPicker_MouseClickAndDrag(t *testing.T) {
	cp := NewColorPickerModal()
	cp.OpenForColor("keyword", "Keyword", "#ff0000", nil)

	// In 80x30 screen: modalW=58, modalH=24
	// startX = (80-58)/2 = 11, startY = (30-24)/2 = 3
	// wheelCenterX = 11 + 29 = 40, wheelCenterY = 3 + 2 + 6 = 11
	// Click on wheel near center
	handled, shouldClose := cp.HandleClick(40, 11, 80, 30)
	if !handled || shouldClose {
		t.Errorf("expected click on wheel center to be handled without closing")
	}

	// Drag on wheel
	dragHandled := cp.HandleDrag(43, 11, 80, 30)
	if !dragHandled {
		t.Errorf("expected drag on wheel to be handled")
	}

	// Click slider: sliderY = 3 + 12 + 3 = 18
	// valLabelLen = len("Brightness: 100% [◄") = 19
	// sliderStartX = 11 + 4 + 19 = 34
	handled, _ = cp.HandleClick(34, 18, 80, 30)
	if !handled {
		t.Errorf("expected click on brightness slider to be handled")
	}

	// Drag slider
	dragSlider := cp.HandleDrag(45, 18, 80, 30)
	if !dragSlider {
		t.Errorf("expected drag on brightness slider to be handled")
	}

	// Click outside modal -> closes
	handled, shouldClose = cp.HandleClick(0, 0, 80, 30)
	if !handled || !shouldClose || cp.Open {
		t.Errorf("expected click outside modal to dismiss color picker")
	}
}
