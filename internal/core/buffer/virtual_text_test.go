package buffer

import (
	"testing"
)

func TestVirtualTextManager_Basic(t *testing.T) {
	vm := NewVirtualTextManager()

	anns := []VirtualAnnotation{
		{ID: "inlay-1", Source: "lsp", Kind: AnnotationInlay, Line: 10, Col: 5, Text: ": int"},
		{ID: "inlay-2", Source: "lsp", Kind: AnnotationInlay, Line: 10, Col: 2, Text: "x:"},
		{ID: "blame-1", Source: "git", Kind: AnnotationEndOfLine, Line: 10, Col: 0, Text: "Konstantin, 2h ago"},
		{ID: "conceal-1", Source: "env", Kind: AnnotationConceal, Line: 12, Col: 4, EndCol: 20, Text: "••••••••"},
	}

	vm.SetAnnotations("lsp", anns[:2])
	vm.SetAnnotations("git", anns[2:3])
	vm.SetAnnotations("env", anns[3:])

	// Line 10 should have 3 annotations sorted by Col: Col 0 (blame), Col 2 (x:), Col 5 (: int)
	line10 := vm.GetLineAnnotations(10)
	if len(line10) != 3 {
		t.Fatalf("expected 3 annotations on line 10, got %d", len(line10))
	}
	if line10[0].Text != "Konstantin, 2h ago" || line10[1].Text != "x:" || line10[2].Text != ": int" {
		t.Fatalf("unexpected order on line 10: %+v", line10)
	}

	// Line 12 has conceal
	line12 := vm.GetLineAnnotations(12)
	if len(line12) != 1 || line12[0].Kind != AnnotationConceal {
		t.Fatalf("expected 1 conceal annotation on line 12, got %+v", line12)
	}

	// Line 5 has nothing
	if vm.HasAnnotations(5) {
		t.Fatalf("expected no annotations on line 5")
	}

	// Adjust line delta (+2 lines inserted at line 8: lines 10->12, lines 12->14)
	vm.AdjustOnLineDelta(8, 2)
	if vm.HasAnnotations(10) {
		t.Fatalf("expected line 10 annotations to have shifted to 12")
	}
	line12AfterShift := vm.GetLineAnnotations(12)
	if len(line12AfterShift) != 3 {
		t.Fatalf("expected 3 annotations shifted to line 12, got %d", len(line12AfterShift))
	}
	line14AfterShift := vm.GetLineAnnotations(14)
	if len(line14AfterShift) != 1 {
		t.Fatalf("expected conceal shifted to line 14, got %d", len(line14AfterShift))
	}

	// Delete line 5 (delta -1): line 12 shifts to 11, line 14 shifts to 13
	vm.AdjustOnLineDelta(5, -1)
	line11AfterDel := vm.GetLineAnnotations(11)
	if len(line11AfterDel) != 3 {
		t.Fatalf("expected 3 annotations shifted to line 11, got %d", len(line11AfterDel))
	}
	line13AfterDel := vm.GetLineAnnotations(13)
	if len(line13AfterDel) != 1 {
		t.Fatalf("expected conceal shifted to line 13, got %d", len(line13AfterDel))
	}

	// Clear source lsp: line 11 should now only have git annotation
	vm.ClearSource("lsp")
	line11AfterClear := vm.GetLineAnnotations(11)
	if len(line11AfterClear) != 1 || line11AfterClear[0].Source != "git" {
		t.Fatalf("expected only git annotation on line 11 after clearing lsp, got %+v", line11AfterClear)
	}

	// Clear all
	vm.ClearAll()
	if vm.HasAnnotations(11) || vm.HasAnnotations(13) {
		t.Fatalf("expected all annotations cleared")
	}
}
