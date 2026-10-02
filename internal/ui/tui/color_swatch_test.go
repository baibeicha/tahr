package tui

import (
	"testing"
)

func TestFindHexColorsInLine(t *testing.T) {
	line := `const primary = "#1e1e2e"; // and secondary: #ff0077 and small #fff`
	matches := FindHexColorsInLine(line)

	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}

	if matches[0].Hex != "#1e1e2e" {
		t.Errorf("match 0: expected #1e1e2e, got %s", matches[0].Hex)
	}
	if matches[1].Hex != "#ff0077" {
		t.Errorf("match 1: expected #ff0077, got %s", matches[1].Hex)
	}
	if matches[2].Hex != "#fff" {
		t.Errorf("match 2: expected #fff, got %s", matches[2].Hex)
	}

	// Negative tests
	negLine := `hash#tag and variable_#123456 and #invalid`
	negMatches := FindHexColorsInLine(negLine)
	if len(negMatches) != 0 {
		t.Fatalf("expected 0 matches for invalid hexes, got %d", len(negMatches))
	}
}
