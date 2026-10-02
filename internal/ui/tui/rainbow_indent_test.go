package tui

import (
	"testing"

	corebuf "tahr/internal/core/buffer"
)

func TestCalculateLineRainbowDepths(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[int]int
	}{
		{
			name:     "Simple nested parentheses",
			input:    "func foo() { return ((1 + 2)); }",
			// '(': idx 8 (depth 0), ')': idx 9 (depth 0)
			// '{': idx 11 (depth 0)
			// '(': idx 20 (depth 1), '(': idx 21 (depth 2)
			// ')': idx 27 (depth 2), ')': idx 28 (depth 1)
			// '}': idx 31 (depth 0)
			expected: map[int]int{
				8:  0,
				9:  0,
				11: 0,
				20: 1,
				21: 2,
				27: 2,
				28: 1,
				31: 0,
			},
		},
		{
			name:     "Mixed brackets and braces",
			input:    "[ { ( ) } ]",
			// '[': idx 0 (0), '{': idx 2 (1), '(': idx 4 (2)
			// ')': idx 6 (2), '}': idx 8 (1), ']': idx 10 (0)
			expected: map[int]int{
				0:  0,
				2:  1,
				4:  2,
				6:  2,
				8:  1,
				10: 0,
			},
		},
		{
			name:     "Empty line",
			input:    "",
			expected: map[int]int{},
		},
		{
			name:     "No delimiters",
			input:    "var x = 10 + 20",
			expected: map[int]int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := calculateLineRainbowDepths(tc.input)
			if len(res) != len(tc.expected) {
				t.Fatalf("expected %d entries, got %d: %+v", len(tc.expected), len(res), res)
			}
			for k, v := range tc.expected {
				if actual, ok := res[k]; !ok || actual != v {
					t.Errorf("at col %d: expected depth %d, got %d (found=%v)", k, v, actual, ok)
				}
			}
		})
	}
}

func TestVirtualTextManager_ShiftingAndRendering(t *testing.T) {
	vtm := corebuf.NewVirtualTextManager()
	vtm.SetAnnotations("git", []corebuf.VirtualAnnotation{
		{
			ID:      "eol-1",
			Source:  "git",
			Line:    5,
			Kind:    corebuf.AnnotationEndOfLine,
			Text:    "Konstantin, 2 hours ago • refactor: add virtual text",
			FgColor: 0x6e738d,
		},
	})
	vtm.SetAnnotations("env", []corebuf.VirtualAnnotation{
		{
			ID:      "conceal-1",
			Source:  "env",
			Line:    10,
			Col:     8,
			EndCol:  24,
			Kind:    corebuf.AnnotationConceal,
			Text:    "••••••••",
		},
	})

	if len(vtm.GetLineAnnotations(5)) != 1 {
		t.Fatalf("expected 1 annotation on line 5, got %d", len(vtm.GetLineAnnotations(5)))
	}
	if len(vtm.GetLineAnnotations(10)) != 1 {
		t.Fatalf("expected 1 annotation on line 10, got %d", len(vtm.GetLineAnnotations(10)))
	}

	// Insert line before line 5 -> lines shift down by 2
	vtm.AdjustOnLineDelta(2, 2)
	if len(vtm.GetLineAnnotations(5)) != 0 {
		t.Errorf("expected 0 annotations on line 5 after shift, got %d", len(vtm.GetLineAnnotations(5)))
	}
	if len(vtm.GetLineAnnotations(7)) != 1 {
		t.Errorf("expected 1 annotation on line 7 after shift, got %d", len(vtm.GetLineAnnotations(7)))
	}
	if len(vtm.GetLineAnnotations(12)) != 1 {
		t.Errorf("expected 1 annotation on line 12 after shift, got %d", len(vtm.GetLineAnnotations(12)))
	}

	// Remove annotation via ClearSource
	vtm.ClearSource("git")
	if len(vtm.GetLineAnnotations(7)) != 0 {
		t.Errorf("expected 0 annotations after removal, got %d", len(vtm.GetLineAnnotations(7)))
	}
}
