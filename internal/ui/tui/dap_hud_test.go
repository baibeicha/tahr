package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core/dap"
	"tahr/internal/ui"
)

func TestDAPHUD_RenderAndHitTest(t *testing.T) {
	hud := NewDAPHUDState()
	hud.Open = true

	sess := dap.NewSession()
	sess.SetVariables([]dap.Variable{
		{Name: "count", Type: "int", Value: "42"},
	})
	sess.SetStackFrames([]dap.StackFrame{
		{ID: 1, Name: "main", Line: 10, Source: dap.Source{Path: "main.go"}},
	})

	buf := buffer.NewBuffer(80, 24)
	th := ui.DefaultTheme()

	RenderDAPHUD(buf, th, hud, sess, 0, 16, 80, 7)

	if len(hud.Buttons) == 0 {
		t.Fatal("expected buttons to be recorded during render")
	}

	foundCont := false
	for _, b := range hud.Buttons {
		if b.ID == "dap_cont" {
			foundCont = true
			break
		}
	}
	if !foundCont {
		t.Error("expected to find dap_cont button")
	}

	// Test hit test
	for _, b := range hud.Buttons {
		clicked := hud.FindDAPHUDBtn(b.MinX+1, b.Y)
		if clicked != b.ID {
			t.Errorf("expected %s, got %s", b.ID, clicked)
		}
	}
}
