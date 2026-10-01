package syntax

import (
	"testing"
)

func TestHighlighter_GoSyntax(t *testing.T) {
	h := NewDefaultHighlighter()
	line := "package main // comment"
	spans := h.HighlightLine("go", line)

	if len(spans) < 2 {
		t.Fatalf("expected at least 2 spans (keyword + comment), got %d", len(spans))
	}

	if spans[0].Type != TokenKeyword || spans[0].StartCol != 0 || spans[0].EndCol != 7 {
		t.Fatalf("expected keyword 'package', got %+v", spans[0])
	}

	last := spans[len(spans)-1]
	if last.Type != TokenComment {
		t.Fatalf("expected comment span, got %+v", last)
	}
}

func TestHighlighter_StringAndNumbers(t *testing.T) {
	h := NewDefaultHighlighter()
	line := `msg := "hello" + 123`
	spans := h.HighlightLine("go", line)

	foundString := false
	foundNumber := false
	for _, s := range spans {
		if s.Type == TokenString {
			foundString = true
		}
		if s.Type == TokenNumber {
			foundNumber = true
		}
	}

	if !foundString || !foundNumber {
		t.Fatalf("expected both string and number tokens, got %+v", spans)
	}
}

func TestTreeSitter_ASTQueries(t *testing.T) {
	ts := NewTreeSitterEngine()

	line := `func CalculateTotal(amount int) string { return "done" }`
	ast := ts.ParseLineAST("go", line)

	if ast == nil || len(ast.Children) == 0 {
		t.Fatalf("expected parsed AST nodes, got empty")
	}

	spans := ts.HighlightLine("go", line)
	if len(spans) == 0 {
		t.Fatalf("expected highlighted spans from Tree-sitter")
	}

	foundFunc := false
	foundType := false
	foundKw := false
	for _, s := range spans {
		if s.Type == TokenFunction {
			foundFunc = true
		}
		if s.Type == TokenTypeIdent {
			foundType = true
		}
		if s.Type == TokenKeyword {
			foundKw = true
		}
	}

	if !foundFunc || !foundType || !foundKw {
		t.Fatalf("expected func, type, and keyword in Tree-sitter spans: %+v", spans)
	}
}
