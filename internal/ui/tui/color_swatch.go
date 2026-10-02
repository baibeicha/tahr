package tui

import (
	"strings"

	"github.com/baibeicha/goatui/pkg/core/cell"
)

// EditorColorSwatch records a detected hex color on screen in the editor document.
type EditorColorSwatch struct {
	PaneIndex int
	ScreenX   int
	ScreenY   int
	Line      int
	StartCol  int // Rune index in line
	EndCol    int // Rune index in line
	StartByte int
	EndByte   int
	Hex       string
	Color     cell.Color
}

// CodeColorMatch represents a hex color occurrence inside a source line.
type CodeColorMatch struct {
	StartRune int
	EndRune   int
	StartByte int
	EndByte   int
	Hex       string
	Color     cell.Color
}

// FindHexColorsInLine finds all #RGB, #RRGGBB, and #RRGGBBAA hex color codes in a line.
func FindHexColorsInLine(line string) []CodeColorMatch {
	if len(line) < 4 || !strings.ContainsRune(line, '#') {
		return nil
	}

	var matches []CodeColorMatch
	runes := []rune(line)
	nR := len(runes)

	// Precompute byte offsets of runes
	runeByteOffsets := make([]int, nR+1)
	curByte := 0
	for idx, r := range runes {
		runeByteOffsets[idx] = curByte
		curByte += len(string(r))
	}
	runeByteOffsets[nR] = curByte

	for i := 0; i < nR; i++ {
		if runes[i] != '#' {
			continue
		}
		// Boundary check before '#'
		if i > 0 {
			prev := runes[i-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') || prev == '_' {
				continue
			}
		}

		// Count hex digits
		j := i + 1
		for j < nR {
			r := runes[j]
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				j++
			} else {
				break
			}
		}

		hexLen := j - (i + 1)
		if hexLen == 3 || hexLen == 6 || hexLen == 8 {
			// Boundary check after hex digits
			if j < nR {
				next := runes[j]
				if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') || next == '_' {
					continue
				}
			}

			hexStr := string(runes[i:j])
			if col, ok := HexToRGBColor(hexStr); ok {
				matches = append(matches, CodeColorMatch{
					StartRune: i,
					EndRune:   j,
					StartByte: runeByteOffsets[i],
					EndByte:   runeByteOffsets[j],
					Hex:       hexStr,
					Color:     col,
				})
			}
		}
		i = j - 1
	}

	return matches
}
