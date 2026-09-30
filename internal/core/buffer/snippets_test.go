package buffer

import (
	"testing"
)

func TestParseSnippet_Basic(t *testing.T) {
	raw := "for ${1:i} := 0; $1 < ${2:n}; $1++ {\n\t$0\n}"
	parsed := ParseSnippet(raw)

	expectedPlain := "for i := 0;  < n; ++ {\n\t\n}"
	if parsed.PlainText != expectedPlain {
		t.Errorf("expected plain text %q, got %q", expectedPlain, parsed.PlainText)
	}
	if len(parsed.Tabstops) == 0 {
		t.Fatalf("expected tabstops, got none")
	}

	// First tabstop must be 1, terminal must be 0
	if parsed.Tabstops[0].Index != 1 {
		t.Errorf("expected first tabstop index 1, got %d", parsed.Tabstops[0].Index)
	}
	last := parsed.Tabstops[len(parsed.Tabstops)-1]
	if last.Index != 0 {
		t.Errorf("expected final tabstop index 0, got %d", last.Index)
	}
}

func TestParseSnippet_Escapes(t *testing.T) {
	raw := "echo \\$VAR ${1:default\\}value} \\$0"
	parsed := ParseSnippet(raw)

	// \$ -> $, \} inside placeholder -> }
	if parsed.PlainText != "echo $VAR default}value $0" {
		t.Errorf("unexpected plain text: %q", parsed.PlainText)
	}

	if len(parsed.Tabstops) != 1 {
		t.Fatalf("expected 1 tabstop, got %d", len(parsed.Tabstops))
	}
	if parsed.Tabstops[0].DefaultText != "default}value" {
		t.Errorf("expected default text 'default}value', got %q", parsed.Tabstops[0].DefaultText)
	}
}

func TestSnippetSession_StepThrough(t *testing.T) {
	raw := "func ${1:name}(${2:params}) ${3:error} {\n\t$0\n}"
	parsed := ParseSnippet(raw)

	session := NewSnippetSession(parsed, 10, 0)
	if !session.Active {
		t.Fatalf("expected session to be active")
	}

	// 1st: name
	cur, ok := session.Current()
	if !ok || cur.Index != 1 || cur.DefaultText != "name" {
		t.Errorf("expected tabstop 1 (name), got %+v", cur)
	}

	// 2nd: params
	cur, ok = session.Next()
	if !ok || cur.Index != 2 || cur.DefaultText != "params" {
		t.Errorf("expected tabstop 2 (params), got %+v", cur)
	}

	// 3rd: error
	cur, ok = session.Next()
	if !ok || cur.Index != 3 || cur.DefaultText != "error" {
		t.Errorf("expected tabstop 3 (error), got %+v", cur)
	}

	// Prev -> back to params
	cur, ok = session.Prev()
	if !ok || cur.Index != 2 {
		t.Errorf("expected step back to tabstop 2, got %+v", cur)
	}

	// Next -> error
	cur, ok = session.Next()
	if !ok || cur.Index != 3 {
		t.Errorf("expected step forward to tabstop 3, got %+v", cur)
	}

	// Next -> $0 (terminal tabstop)
	cur, ok = session.Next()
	if !ok || cur.Index != 0 {
		t.Errorf("expected terminal tabstop 0, got %+v", cur)
	}
	if session.Active {
		t.Errorf("expected session to become inactive after reaching $0")
	}
}
