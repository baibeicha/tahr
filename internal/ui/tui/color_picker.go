package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

// ColorPickerModal provides an interactive 2D radial color wheel and brightness slider.
type ColorPickerModal struct {
	Open       bool
	ColorKey   string
	ColorLabel string
	OrigHex    string
	CurHex     string
	Hue        float64 // 0 .. 360
	Sat        float64 // 0 .. 1.0
	Val        float64 // 0 .. 1.0 (Brightness)
	FocusMode  int     // 0: Wheel, 1: Value slider, 2: Hex text input
	HexInput   string
	OnApply    func(key, hexVal string)
	OnPreview  func(key, hexVal string)
	Radius     int // Wheel radius in characters
}

// NewColorPickerModal initializes a color picker modal.
func NewColorPickerModal() *ColorPickerModal {
	return &ColorPickerModal{
		Open:      false,
		Hue:       210,
		Sat:       0.8,
		Val:       0.95,
		Radius:    6,
		FocusMode: 0,
	}
}

// OpenForColor activates the modal for a specific color key and hex value.
func (cp *ColorPickerModal) OpenForColor(key, label, hexVal string, applyFn func(key, hexVal string)) {
	cp.Open = true
	cp.ColorKey = key
	cp.ColorLabel = label
	cp.OrigHex = hexVal
	cp.CurHex = hexVal
	cp.HexInput = strings.TrimPrefix(hexVal, "#")
	cp.OnApply = applyFn
	cp.FocusMode = 0

	// Parse current hex into HSV
	if c, ok := HexToRGBColor(hexVal); ok {
		h, s, v := RGBToHSV(c.R(), c.G(), c.B())
		cp.Hue = h
		cp.Sat = s
		cp.Val = v
	}
}

// HandleKey handles keyboard interaction inside the Color Picker modal.
func (cp *ColorPickerModal) HandleKey(k input.Key) (handled bool, shouldClose bool) {
	if !cp.Open {
		return false, false
	}

	switch k.Type {
	case input.KeyEsc:
		cp.Open = false
		return true, true

	case input.KeyEnter:
		if cp.FocusMode == 2 && cp.HexInput != "" {
			hStr := "#" + strings.TrimPrefix(cp.HexInput, "#")
			if c, ok := HexToRGBColor(hStr); ok {
				cp.CurHex = hStr
				h, s, v := RGBToHSV(c.R(), c.G(), c.B())
				cp.Hue = h
				cp.Sat = s
				cp.Val = v
			}
		}
		if cp.OnApply != nil {
			cp.OnApply(cp.ColorKey, cp.CurHex)
		}
		cp.Open = false
		return true, true

	case input.KeyTab:
		cp.FocusMode = (cp.FocusMode + 1) % 3
		return true, false

	case input.KeyBacktab:
		cp.FocusMode = (cp.FocusMode + 2) % 3
		return true, false

	case input.KeyLeft:
		if cp.FocusMode == 0 { // Wheel: rotate hue counter-clockwise
			cp.Hue -= 10
			if cp.Hue < 0 {
				cp.Hue += 360
			}
			cp.syncHex()
		} else if cp.FocusMode == 1 { // Value slider: decrease brightness
			cp.Val = math.Max(0.0, cp.Val-0.05)
			cp.syncHex()
		}
		return true, false

	case input.KeyRight:
		if cp.FocusMode == 0 { // Wheel: rotate hue clockwise
			cp.Hue += 10
			if cp.Hue >= 360 {
				cp.Hue -= 360
			}
			cp.syncHex()
		} else if cp.FocusMode == 1 { // Value slider: increase brightness
			cp.Val = math.Min(1.0, cp.Val+0.05)
			cp.syncHex()
		}
		return true, false

	case input.KeyUp:
		if cp.FocusMode == 0 { // Wheel: increase saturation
			cp.Sat = math.Min(1.0, cp.Sat+0.08)
			cp.syncHex()
		} else if cp.FocusMode == 1 {
			cp.FocusMode = 0
		}
		return true, false

	case input.KeyDown:
		if cp.FocusMode == 0 { // Wheel: decrease saturation
			cp.Sat = math.Max(0.0, cp.Sat-0.08)
			cp.syncHex()
		} else if cp.FocusMode == 0 && cp.Sat <= 0.05 {
			cp.FocusMode = 1
		}
		return true, false

	case input.KeyBackspace:
		if cp.FocusMode == 2 && len(cp.HexInput) > 0 {
			r := []rune(cp.HexInput)
			cp.HexInput = string(r[:len(r)-1])
			if len(cp.HexInput) == 6 {
				if c, ok := HexToRGBColor("#" + cp.HexInput); ok {
					cp.CurHex = "#" + cp.HexInput
					h, s, v := RGBToHSV(c.R(), c.G(), c.B())
					cp.Hue = h
					cp.Sat = s
					cp.Val = v
					if cp.OnPreview != nil {
						cp.OnPreview(cp.ColorKey, cp.CurHex)
					}
				}
			}
		}
		return true, false

	default:
		if cp.FocusMode == 2 && k.Rune >= 32 {
			if len(cp.HexInput) < 6 {
				cp.HexInput += string(k.Rune)
				if len(cp.HexInput) == 6 {
					if c, ok := HexToRGBColor("#" + cp.HexInput); ok {
						cp.CurHex = "#" + cp.HexInput
						h, s, v := RGBToHSV(c.R(), c.G(), c.B())
						cp.Hue = h
						cp.Sat = s
						cp.Val = v
						if cp.OnPreview != nil {
							cp.OnPreview(cp.ColorKey, cp.CurHex)
						}
					}
				}
			}
			return true, false
		}
	}

	return false, false
}

// HandleClick processes mouse clicks inside or outside the modal.
func (cp *ColorPickerModal) HandleClick(mouseX, mouseY, w, h int) (handled bool, shouldClose bool) {
	if !cp.Open {
		return false, false
	}
	modalW := 58
	modalH := 24
	if modalW > w-4 {
		modalW = w - 4
	}
	if modalH > h-2 {
		modalH = h - 2
	}
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	// Click outside -> dismiss modal
	if mouseX < startX || mouseX >= startX+modalW || mouseY < startY || mouseY >= startY+modalH {
		cp.Open = false
		return true, true
	}

	// Click on bottom instruction row -> if on Apply, Cancel or Mode
	if mouseY == startY+modalH-1 {
		cancelStartX := startX + modalW - 2 - 13
		applyStartX := cancelStartX - 14 - 2
		if mouseX >= cancelStartX && mouseX < cancelStartX+13 {
			// Cancel
			cp.Open = false
			return true, true
		} else if mouseX >= applyStartX && mouseX < applyStartX+14 {
			// Apply
			if cp.OnApply != nil {
				cp.OnApply(cp.ColorKey, cp.CurHex)
			}
			cp.Open = false
			return true, true
		} else if mouseX >= startX+2 && mouseX < startX+15 {
			cp.FocusMode = (cp.FocusMode + 1) % 3
			return true, false
		}
	}

	sliderY := startY + cp.Radius*2 + 3
	swatchY := sliderY + 2
	origPrefix := "Original: "
	origHexText := " " + cp.OrigHex
	arrow := " ──► "
	newPrefix := "New: "
	hashPrefix := " #"
	hexBoxX := startX + 4 + len(origPrefix) + 4 + len(origHexText) + len(arrow) + len(newPrefix) + 4 + len(hashPrefix)
	if mouseY == swatchY && mouseX >= hexBoxX && mouseX <= hexBoxX+6 {
		cp.FocusMode = 2
		return true, false
	}

	// Click inside wheel area
	wheelCenterX := float64(startX + (modalW / 2))
	wheelCenterY := float64(startY + 2 + cp.Radius)
	normDx := (float64(mouseX) - wheelCenterX) / 2.0
	normDy := float64(mouseY) - wheelCenterY
	dist := math.Hypot(normDx, normDy)
	if dist <= float64(cp.Radius)+0.5 {
		ang := math.Atan2(normDy, normDx) * 180.0 / math.Pi
		if ang < 0 {
			ang += 360.0
		}
		cp.Hue = ang
		sat := dist / float64(cp.Radius)
		if sat > 1.0 {
			sat = 1.0
		}
		cp.Sat = sat
		cp.FocusMode = 0
		cp.syncHex()
		return true, false
	}

	// Click inside Brightness slider
	valLabelLen := len(fmt.Sprintf("Brightness: %3d%% [◄", int(cp.Val*100)))
	sliderStartX := startX + 4 + valLabelLen
	sliderLen := 22
	if (mouseY == sliderY || mouseY == sliderY+1) && mouseX >= sliderStartX && mouseX <= sliderStartX+sliderLen {
		relX := float64(mouseX - sliderStartX)
		cp.Val = math.Max(0.0, math.Min(1.0, relX/float64(sliderLen-1)))
		cp.FocusMode = 1
		cp.syncHex()
		return true, false
	}

	return true, false
}

// HandleDrag processes continuous mouse drag events across wheel or slider.
func (cp *ColorPickerModal) HandleDrag(mouseX, mouseY, w, h int) bool {
	if !cp.Open {
		return false
	}
	modalW := 58
	modalH := 24
	if modalW > w-4 {
		modalW = w - 4
	}
	if modalH > h-2 {
		modalH = h - 2
	}
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	// 1. Wheel drag
	wheelCenterX := float64(startX + (modalW / 2))
	wheelCenterY := float64(startY + 2 + cp.Radius)
	normDx := (float64(mouseX) - wheelCenterX) / 2.0
	normDy := float64(mouseY) - wheelCenterY
	dist := math.Hypot(normDx, normDy)
	if dist <= float64(cp.Radius)+1.0 {
		ang := math.Atan2(normDy, normDx) * 180.0 / math.Pi
		if ang < 0 {
			ang += 360.0
		}
		cp.Hue = ang
		sat := dist / float64(cp.Radius)
		if sat > 1.0 {
			sat = 1.0
		}
		cp.Sat = sat
		cp.FocusMode = 0
		cp.syncHex()
		return true
	}

	// 2. Slider drag
	sliderY := startY + cp.Radius*2 + 3
	valLabelLen := len(fmt.Sprintf("Brightness: %3d%% [◄", int(cp.Val*100)))
	sliderStartX := startX + 4 + valLabelLen
	sliderLen := 22
	if (mouseY >= sliderY-1 && mouseY <= sliderY+1) && mouseX >= sliderStartX-2 && mouseX <= sliderStartX+sliderLen+2 {
		relX := float64(mouseX - sliderStartX)
		cp.Val = math.Max(0.0, math.Min(1.0, relX/float64(sliderLen-1)))
		cp.FocusMode = 1
		cp.syncHex()
		return true
	}

	return false
}

// syncHex updates CurHex and HexInput from current HSV.
func (cp *ColorPickerModal) syncHex() {
	c := HSVToRGB(cp.Hue, cp.Sat, cp.Val)
	cp.CurHex = fmt.Sprintf("#%02x%02x%02x", c.R(), c.G(), c.B())
	cp.HexInput = fmt.Sprintf("%02x%02x%02x", c.R(), c.G(), c.B())
	if cp.OnPreview != nil {
		cp.OnPreview(cp.ColorKey, cp.CurHex)
	}
}

// Render draws the interactive 2D radial color wheel modal.
func (cp *ColorPickerModal) Render(buf *buffer.Buffer, w, h int, th ui.Theme) {
	if !cp.Open || buf == nil || w < 40 || h < 18 {
		return
	}

	modalW := 58
	modalH := 24
	if modalW > w-4 {
		modalW = w - 4
	}
	if modalH > h-2 {
		modalH = h - 2
	}

	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	themeBg := toColor(th.Background)
	themeFg := toColor(th.Foreground)
	borderFg := toColor(th.BorderColor)
	accentFg := toColor(th.Function)
	commentFg := toColor(th.Comment)
	selBg := toColor(th.SelectionBg)
	selFg := toColor(th.Foreground)

	// 1. Draw outer rounded box & fill
	for y := 0; y < modalH; y++ {
		for x := 0; x < modalW; x++ {
			ch := ' '
			fg := borderFg
			if y == 0 && x == 0 {
				ch = '╭'
			} else if y == 0 && x == modalW-1 {
				ch = '╮'
			} else if y == modalH-1 && x == 0 {
				ch = '╰'
			} else if y == modalH-1 && x == modalW-1 {
				ch = '╯'
			} else if y == 0 || y == modalH-1 {
				ch = '─'
			} else if x == 0 || x == modalW-1 {
				ch = '│'
			}
			buf.SetRune(startX+x, startY+y, ch, fg, themeBg, cell.AttrNone)
		}
	}

	// Title
	title := fmt.Sprintf(" %s: %s ", i18n.T("colorpicker.title"), cp.ColorLabel)
	for i, r := range title {
		if startX+2+i < startX+modalW-2 {
			buf.SetRune(startX+2+i, startY, r, accentFg, themeBg, cell.AttrBold)
		}
	}

	// 2. 2D Radial Color Wheel
	wheelRadius := cp.Radius
	wheelH := wheelRadius * 2
	wheelCenterX := startX + (modalW / 2)
	wheelCenterY := startY + 2 + wheelRadius

	// Calculate cursor cell position from current Hue & Sat
	radAngle := cp.Hue * math.Pi / 180.0
	curRelX := int(math.Round(cp.Sat * float64(wheelRadius*2) * math.Cos(radAngle)))
	curRelY := int(math.Round(cp.Sat * float64(wheelRadius) * math.Sin(radAngle)))
	targetCursorX := wheelCenterX + curRelX
	targetCursorY := wheelCenterY + curRelY

	for dy := -wheelRadius; dy <= wheelRadius; dy++ {
		rowY := wheelCenterY + dy
		for dx := -wheelRadius * 2; dx <= wheelRadius * 2; dx++ {
			colX := wheelCenterX + dx

			// Aspect ratio normalized distance
			normDy := float64(dy)
			normDx := float64(dx) / 2.0
			dist := math.Hypot(normDx, normDy)

			if dist <= float64(wheelRadius)+0.4 {
				ang := math.Atan2(normDy, normDx) * 180.0 / math.Pi
				if ang < 0 {
					ang += 360
				}
				sat := dist / float64(wheelRadius)
				if sat > 1.0 {
					sat = 1.0
				}

				c := HSVToRGB(ang, sat, cp.Val)
				r := '●'
				fg := c
				bg := themeBg

				// Check if cursor is on this position
				if colX == targetCursorX && rowY == targetCursorY {
					r = '┼'
					fg = toColor(0xffffff)
					bg = c
				} else if math.Abs(float64(colX-targetCursorX)) <= 1 && rowY == targetCursorY {
					if colX < targetCursorX {
						r = '►'
					} else {
						r = '◄'
					}
					fg = toColor(0xffffff)
				}

				buf.SetRune(colX, rowY, r, fg, bg, cell.AttrNone)
			}
		}
	}

	// 3. Brightness / Value Slider (Row startY + wheelH + 3)
	sliderY := startY + wheelH + 3
	valLabel := fmt.Sprintf(i18n.T("colorpicker.brightness"), int(cp.Val*100))
	for i, r := range []rune(valLabel) {
		buf.SetRune(startX+4+i, sliderY, r, themeFg, themeBg, cell.AttrNone)
	}

	sliderStartX := startX + 4 + len([]rune(valLabel))
	sliderLen := 22
	sliderThumb := int(cp.Val * float64(sliderLen-1))

	sliderBg := themeBg
	if cp.FocusMode == 1 {
		sliderBg = selBg
	}

	for i := 0; i < sliderLen; i++ {
		r := '─'
		fg := commentFg
		if i == sliderThumb {
			r = '●'
			fg = accentFg
		} else if i < sliderThumb {
			r = '━'
			fg = themeFg
		}
		buf.SetRune(sliderStartX+i, sliderY, r, fg, sliderBg, cell.AttrBold)
	}
	buf.SetRune(sliderStartX+sliderLen, sliderY, '►', themeFg, themeBg, cell.AttrNone)

	// 4. Live Swatches & Hex Input Row
	swatchY := sliderY + 2
	curColor := HSVToRGB(cp.Hue, cp.Sat, cp.Val)
	origColor, _ := HexToRGBColor(cp.OrigHex)

	origPrefix := i18n.T("colorpicker.original")
	origStartX := startX + 4
	for i, r := range []rune(origPrefix) {
		buf.SetRune(origStartX+i, swatchY, r, themeFg, themeBg, cell.AttrNone)
	}
	curX := origStartX + len([]rune(origPrefix))
	// Broad TrueColor swatch: ████
	for i := 0; i < 4; i++ {
		buf.SetRune(curX+i, swatchY, '█', origColor, themeBg, cell.AttrNone)
	}
	curX += 4
	origHexText := " " + cp.OrigHex
	for i, r := range origHexText {
		buf.SetRune(curX+i, swatchY, r, themeFg, themeBg, cell.AttrBold)
	}
	curX += len(origHexText)

	arrow := " ──► "
	for i, r := range arrow {
		buf.SetRune(curX+i, swatchY, r, commentFg, themeBg, cell.AttrNone)
	}
	curX += len(arrow)

	newPrefix := i18n.T("colorpicker.new")
	for i, r := range []rune(newPrefix) {
		buf.SetRune(curX+i, swatchY, r, themeFg, themeBg, cell.AttrNone)
	}
	curX += len([]rune(newPrefix))
	for i := 0; i < 4; i++ {
		buf.SetRune(curX+i, swatchY, '█', curColor, themeBg, cell.AttrNone)
	}
	curX += 4
	hashPrefix := " #"
	for i, r := range hashPrefix {
		buf.SetRune(curX+i, swatchY, r, themeFg, themeBg, cell.AttrBold)
	}
	curX += len(hashPrefix)

	hexInputBox := fmt.Sprintf("%-6s", cp.HexInput)
	hexBoxX := curX
	hexBg := themeBg
	hexFg := themeFg
	if cp.FocusMode == 2 {
		hexBg = selBg
		hexFg = selFg
	}
	for i, r := range hexInputBox {
		buf.SetRune(hexBoxX+i, swatchY, r, hexFg, hexBg, cell.AttrBold)
	}

	// 5. Bottom Instructions Bar with explicit button coordinates
	instY := startY + modalH - 1
	tabModeStr := fmt.Sprintf(" %s ", i18n.T("colorpicker.hint_tab"))
	for i, r := range []rune(tabModeStr) {
		buf.SetRune(startX+2+i, instY, r, commentFg, themeBg, cell.AttrNone)
	}

	cancelStr := fmt.Sprintf(" %s ", i18n.T("colorpicker.hint_cancel"))
	cancelStartX := startX + modalW - 2 - len([]rune(cancelStr))
	for i, r := range []rune(cancelStr) {
		buf.SetRune(cancelStartX+i, instY, r, themeFg, themeBg, cell.AttrNone)
	}

	applyStr := fmt.Sprintf(" %s ", i18n.T("colorpicker.hint_apply"))
	applyStartX := cancelStartX - len([]rune(applyStr)) - 1
	for i, r := range []rune(applyStr) {
		buf.SetRune(applyStartX+i, instY, r, accentFg, themeBg, cell.AttrBold)
	}
}

// HSVToRGB converts hue (0..360), saturation (0..1), and value (0..1) to 24-bit TrueColor.
func HSVToRGB(h, s, v float64) cell.Color {
	if s <= 0 {
		val := uint8(math.Round(v * 255.0))
		colorVal := (uint32(val) << 16) | (uint32(val) << 8) | uint32(val)
		return toColor(colorVal)
	}

	hh := h / 60.0
	i := int(math.Floor(hh)) % 6
	ff := hh - math.Floor(hh)
	p := v * (1.0 - s)
	q := v * (1.0 - (s * ff))
	t := v * (1.0 - (s * (1.0 - ff)))

	var r, g, b float64
	switch i {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}

	rUint := uint8(math.Round(r * 255.0))
	gUint := uint8(math.Round(g * 255.0))
	bUint := uint8(math.Round(b * 255.0))
	return toColor((uint32(rUint) << 16) | (uint32(gUint) << 8) | uint32(bUint))
}

// RGBToHSV converts 8-bit RGB components to Hue (0..360), Saturation (0..1), and Value (0..1).
func RGBToHSV(r, g, b uint8) (h, s, v float64) {
	rf := float64(r) / 255.0
	gf := float64(g) / 255.0
	bf := float64(b) / 255.0

	maxVal := math.Max(rf, math.Max(gf, bf))
	minVal := math.Min(rf, math.Min(gf, bf))
	delta := maxVal - minVal

	v = maxVal
	if maxVal > 0 {
		s = delta / maxVal
	} else {
		s = 0
		h = 0
		return
	}

	if delta == 0 {
		h = 0
		return
	}

	if rf == maxVal {
		h = (gf - bf) / delta
	} else if gf == maxVal {
		h = 2.0 + (bf-rf)/delta
	} else {
		h = 4.0 + (rf-gf)/delta
	}

	h *= 60.0
	if h < 0 {
		h += 360.0
	}
	return
}

// HexToRGBColor parses a hex color string into cell.Color.
func HexToRGBColor(hexStr string) (cell.Color, bool) {
	clean := strings.TrimPrefix(strings.TrimSpace(hexStr), "#")
	if len(clean) == 3 {
		r, _ := strconv.ParseUint(string(clean[0])+string(clean[0]), 16, 8)
		g, _ := strconv.ParseUint(string(clean[1])+string(clean[1]), 16, 8)
		b, _ := strconv.ParseUint(string(clean[2])+string(clean[2]), 16, 8)
		colorVal := (uint32(r) << 16) | (uint32(g) << 8) | uint32(b)
		return toColor(colorVal), true
	}
	if len(clean) >= 6 {
		val, err := strconv.ParseUint(clean[:6], 16, 32)
		if err == nil {
			return toColor(uint32(val)), true
		}
	}
	return cell.Color{}, false
}
