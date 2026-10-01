package vim

import (
	"testing"
)

func TestVimFSM_NavigationAndCounts(t *testing.T) {
	fsm := NewFSM()
	if fsm.Mode != ModeNormal {
		t.Fatalf("expected ModeNormal, got %s", fsm.Mode)
	}

	// '5' then 'j' -> cursor.down with count 5
	act := fsm.HandleKey('5', "", false, false, false)
	if !act.Handled || act.Type != EventNone {
		t.Errorf("expected count accumulation, got %+v", act)
	}

	act = fsm.HandleKey('j', "", false, false, false)
	if !act.Handled || act.Type != EventCommand || act.Command != "cursor.down" || act.Count != 5 {
		t.Errorf("expected cursor.down count 5, got %+v", act)
	}

	// 'h' single
	act = fsm.HandleKey('h', "", false, false, false)
	if act.Command != "cursor.left" || act.Count != 1 {
		t.Errorf("expected cursor.left count 1, got %+v", act)
	}
}

func TestVimFSM_TwoKeyOperators(t *testing.T) {
	fsm := NewFSM()

	// 'd' then 'd' -> edit.delete_line
	act := fsm.HandleKey('d', "", false, false, false)
	if act.Type != EventNone {
		t.Errorf("expected pending op, got %+v", act)
	}

	act = fsm.HandleKey('d', "", false, false, false)
	if act.Command != "edit.delete_line" {
		t.Errorf("expected edit.delete_line, got %+v", act)
	}

	// 'g' then 'g' -> cursor.top
	_ = fsm.HandleKey('g', "", false, false, false)
	act = fsm.HandleKey('g', "", false, false, false)
	if act.Command != "cursor.top" {
		t.Errorf("expected cursor.top, got %+v", act)
	}
}

func TestVimFSM_ModeTransitions(t *testing.T) {
	fsm := NewFSM()

	// 'i' -> Insert mode
	act := fsm.HandleKey('i', "", false, false, false)
	if act.Type != EventEnterInsert || fsm.Mode != ModeInsert {
		t.Fatalf("expected transition to ModeInsert, got mode=%s, act=%+v", fsm.Mode, act)
	}

	// Typing in insert mode passes through
	act = fsm.HandleKey('a', "", false, false, false)
	if act.Handled {
		t.Errorf("expected typing in insert mode to not be swallowed by FSM, got %+v", act)
	}

	// Escape -> Normal mode
	act = fsm.HandleKey(27, "escape", false, false, false)
	if act.Type != EventEnterNormal || fsm.Mode != ModeNormal {
		t.Fatalf("expected return to ModeNormal, got mode=%s, act=%+v", fsm.Mode, act)
	}

	// 'v' -> Visual mode
	act = fsm.HandleKey('v', "", false, false, false)
	if act.Type != EventEnterVisual || fsm.Mode != ModeVisual {
		t.Fatalf("expected ModeVisual, got mode=%s, act=%+v", fsm.Mode, act)
	}

	// 'y' in visual mode -> yank and return to normal
	act = fsm.HandleKey('y', "", false, false, false)
	if act.Type != EventEnterNormal || act.Command != "visual.yank" || fsm.Mode != ModeNormal {
		t.Fatalf("expected visual.yank and return to ModeNormal, got mode=%s, act=%+v", fsm.Mode, act)
	}
}
