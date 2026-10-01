package dap

import (
	"encoding/json"
	"testing"
)

func TestDAP_MessageSerialization(t *testing.T) {
	req := Request{
		Seq:     1,
		Type:    "request",
		Command: "setBreakpoints",
		Arguments: SetBreakpointsArguments{
			Source: Source{Path: "/test/main.go"},
			Breakpoints: []SourceBreakpoint{
				{Line: 10},
				{Line: 25},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var parsed Request
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if parsed.Command != "setBreakpoints" || parsed.Seq != 1 {
		t.Errorf("unexpected parsed request: %+v", parsed)
	}
}

func TestDAP_SessionStateLifecycle(t *testing.T) {
	session := NewSession()
	if session.IsActive() {
		t.Error("expected new session to not be active")
	}

	// Configure breakpoints
	session.SetBreakpoints("main.go", []int{15, 30})
	if len(session.breakpoints) != 1 {
		t.Errorf("expected 1 file in breakpoints, got %d", len(session.breakpoints))
	}

	// Trigger OnStopped
	stoppedHit := false
	session.SetOnStopped(func(file string, line int, reason string) {
		stoppedHit = true
		if reason != "breakpoint" {
			t.Errorf("expected reason 'breakpoint', got %q", reason)
		}
	})

	session.OnStopped("breakpoint", 1)
	if !session.IsStopped() {
		t.Error("expected session to be stopped")
	}
	if !stoppedHit {
		t.Error("expected onStopped callback to be called")
	}

	// Terminate
	session.OnTerminated()
	if session.IsActive() || session.IsStopped() {
		t.Error("expected session to be inactive after termination")
	}
}
