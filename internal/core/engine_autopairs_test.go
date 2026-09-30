package core

import (
	"testing"
)

func getText(doc *Document) string {
	b, _ := doc.Buffer.GetText()
	return string(b)
}

func TestEngine_AutoPairs_Basic(t *testing.T) {
	eng := NewEngine()
	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}

	// 1. Typing '(' should insert '()' with cursor at index 1
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: "("})
	if text := getText(doc); text != "()" {
		t.Fatalf("expected '()', got %q", text)
	}
	sel := doc.Buffer.PrimarySelection()
	if sel.Head.Byte != 1 || sel.Head.Column != 1 {
		t.Fatalf("expected cursor at byte 1, got %+v", sel.Head)
	}

	// 2. Typing ')' right now should skip the existing ')' without duplicating
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: ")"})
	if text := getText(doc); text != "()" {
		t.Fatalf("expected '()' after skip, got %q", text)
	}
	sel = doc.Buffer.PrimarySelection()
	if sel.Head.Byte != 2 || sel.Head.Column != 2 {
		t.Fatalf("expected cursor at byte 2, got %+v", sel.Head)
	}

	// 3. Typing '{' should insert '{}' and cursor at byte 3
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: "{"})
	if text := getText(doc); text != "(){}" {
		t.Fatalf("expected '(){}', got %q", text)
	}
	// Backspace should delete both '{' and '}'
	_ = eng.Dispatch(Command{ID: CmdDeleteBackward})
	if text := getText(doc); text != "()" {
		t.Fatalf("expected '()' after pair backspace, got %q", text)
	}
}

func TestEngine_AutoPairs_Wrapping(t *testing.T) {
	eng := NewEngine()
	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}

	// Insert "hello"
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: "hello"})
	if text := getText(doc); text != "hello" {
		t.Fatalf("expected 'hello', got %q", text)
	}

	// Select all
	_ = eng.Dispatch(Command{ID: CmdSelectAll})

	// Type '[' -> should wrap to "[hello]"
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: "["})
	if text := getText(doc); text != "[hello]" {
		t.Fatalf("expected '[hello]', got %q", text)
	}
}

func TestEngine_AutoPairs_Toggle(t *testing.T) {
	eng := NewEngine()
	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}

	// Disable auto-pairs
	eng.SetAutoPairsEnabled(false)

	// Typing '(' should insert only '('
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: "("})
	if text := getText(doc); text != "(" {
		t.Fatalf("expected '(', got %q", text)
	}

	// Toggle back on via command
	_ = eng.Dispatch(Command{ID: CmdToggleAutoPairs})
	if !eng.AutoPairsEnabled() {
		t.Fatal("expected auto-pairs to be enabled after toggle")
	}
}
