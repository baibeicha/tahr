package tui

import (
	"unicode"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/ui"
)

// MinimapConfig holds geometry and visual settings for the code minimap.
type MinimapConfig struct {
	Width   int  // default: 4 columns
	Enabled bool // default: true
}

// DefaultMinimapConfig returns default minimap settings.
func DefaultMinimapConfig() MinimapConfig {
	return MinimapConfig{Width: 4, Enabled: true}
}

// MinimapLineInfo abstracts line content for minimap rendering.
type MinimapLineProvider func(lineIdx int) string

// RenderMinimap draws a compact code density overview on the right edge of a pane.
func RenderMinimap(
	buf *buffer.Buffer,
	th ui.Theme,
	x, y, w, h int,
	totalLines int,
	vpY, vpHeight int,
	getLine MinimapLineProvider,
) {
	if buf == nil || w <= 0 || h <= 0 || totalLines <= 0 {
		return
	}

	bg := toColor(th.GutterBg)
	dimFg := toColor(th.Comment)
	codeFg := toColor(th.LineNumber)
	activeFg := toColor(th.Function)
	borderFg := toColor(th.BorderColor)

	scale := float64(totalLines) / float64(h)
	if scale < 1.0 {
		scale = 1.0
	}

	// Calculate which minimap rows fall inside the current viewport
	vpMinimapStart := int(float64(vpY) / scale)
	vpMinimapEnd := int(float64(vpY+vpHeight) / scale)
	if vpMinimapEnd < vpMinimapStart {
		vpMinimapEnd = vpMinimapStart
	}

	for row := 0; row < h; row++ {
		screenY := y + row
		calcLine := int(float64(row) * scale)
		hasLine := calcLine < totalLines
		targetLine := calcLine
		if !hasLine {
			targetLine = -1
		}

		inViewport := (row >= vpMinimapStart && row <= vpMinimapEnd && hasLine)

		rowBg := bg
		rowFg := codeFg
		leftRune := ' '
		leftFg := borderFg
		if hasLine {
			leftRune = '│'
			if inViewport {
				leftRune = '▌'
				leftFg = activeFg
				rowFg = activeFg
			}
		}

		// Left edge divider for minimap
		buf.SetRune(x, screenY, leftRune, leftFg, bg, cell.AttrNone)

		var lineStr string
		if hasLine && getLine != nil && targetLine >= 0 && targetLine < totalLines {
			lineStr = getLine(targetLine)
		}

		densityRunes := computeLineDensityRunes(lineStr, w-1)
		for col := 0; col < w-1; col++ {
			r := ' '
			fg := rowFg
			if col < len(densityRunes) {
				r = densityRunes[col]
				if r == '·' {
					fg = dimFg
				}
			}
			buf.SetRune(x+1+col, screenY, r, fg, rowBg, cell.AttrNone)
		}
	}
}

func computeLineDensityRunes(line string, width int) []rune {
	if len(line) == 0 {
		return nil
	}

	indent := 0
	contentRunes := 0
	for _, r := range line {
		if unicode.IsSpace(r) {
			if contentRunes == 0 {
				indent++
			}
		} else {
			contentRunes++
		}
	}

	result := make([]rune, width)
	for i := range result {
		result[i] = ' '
	}

	scaledIndent := indent / 4
	if scaledIndent >= width {
		scaledIndent = width - 1
	}

	for i := 0; i < scaledIndent; i++ {
		result[i] = '·'
	}

	// Fill content with delicate high-resolution Braille density characters
	for i := scaledIndent; i < width; i++ {
		rem := contentRunes - (i-scaledIndent)*6
		if rem > 18 {
			result[i] = '⠿' // full Braille cell (dense)
		} else if rem > 10 {
			result[i] = '⠶' // 4-dot pattern
		} else if rem > 4 {
			result[i] = '⠤' // 2-dot pattern
		} else if rem > 0 {
			result[i] = '⠒' // light 2-dot top line
		} else {
			break
		}
	}

	return result
}

// MinimapHitTest maps a click inside the minimap column to a target line index.
func MinimapHitTest(clickY, minY, height, totalLines int) int {
	if height <= 0 || totalLines <= 0 {
		return 0
	}
	relY := clickY - minY
	if relY < 0 {
		relY = 0
	}
	if relY >= height {
		relY = height - 1
	}

	targetLine := int(float64(relY) / float64(height) * float64(totalLines))
	if targetLine < 0 {
		targetLine = 0
	}
	if targetLine >= totalLines {
		targetLine = totalLines - 1
	}
	return targetLine
}
