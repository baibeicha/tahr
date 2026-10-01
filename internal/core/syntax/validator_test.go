package syntax

import (
	"testing"
)

func TestValidateStructure_Balanced(t *testing.T) {
	lines := []string{
		"func main() {",
		"\tx := []int{1, 2, 3}",
		"\tprintln(\"hello world\")",
		"}",
	}
	diags := ValidateStructure(lines)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for balanced code, got %d: %+v", len(diags), diags)
	}
}

func TestValidateStructure_UnmatchedClosing(t *testing.T) {
	lines := []string{
		"func main() {",
		"\tx := 1 }",
		"}",
	}
	diags := ValidateStructure(lines)
	if len(diags) == 0 {
		t.Fatalf("expected diagnostic for extra closing brace")
	}
}

func TestValidateStructure_UnclosedOpening(t *testing.T) {
	lines := []string{
		"func main() {",
		"\titems := [3]int{1, 2, 3",
	}
	diags := ValidateStructure(lines)
	if len(diags) == 0 {
		t.Fatalf("expected diagnostic for unclosed opening bracket")
	}
}
