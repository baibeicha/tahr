package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core/p2p"
	"tahr/internal/ui"
)

func TestP2PModalRenderingAndKeyEvents(t *testing.T) {
	theme := ui.CatppuccinMocha()
	session := p2p.NewHostSession("alice-host", "")

	modal := NewP2PModal(session, &theme)
	if !modal.Visible {
		t.Fatal("expected modal to be visible")
	}

	buf := buffer.NewBuffer(80, 24)
	modal.Render(buf, 80, 24)

	// Check title was rendered into buffer
	cell := buf.Cell(10, 2)
	if cell == nil {
		t.Fatal("expected rendered cell at modal coordinate")
	}

	// Test Toggling Role
	if modal.PendingRole != "editor" {
		t.Fatalf("expected initial role editor, got %s", modal.PendingRole)
	}
	modal.HandleKeyEvent("t")
	if modal.PendingRole != "viewer" {
		t.Fatalf("expected toggled role viewer, got %s", modal.PendingRole)
	}
	modal.HandleKeyEvent("t")
	if modal.PendingRole != "editor" {
		t.Fatalf("expected toggled role editor, got %s", modal.PendingRole)
	}

	// Test Toggling PTY
	if modal.PendingPTY {
		t.Fatal("expected initial pending PTY to be false")
	}
	modal.HandleKeyEvent("p")
	if !modal.PendingPTY {
		t.Fatal("expected toggled pending PTY to be true")
	}

	// Add pending guest join request
	session.PendingJoins = append(session.PendingJoins, p2p.MsgJoinRequest{
		PeerID:   105,
		Nickname: "bob",
		Role:     "editor",
	})

	// Render with pending request
	modal.Render(buf, 80, 24)

	// Test Accept
	modal.HandleKeyEvent("y")
	if len(session.PendingJoins) != 0 {
		t.Fatalf("expected pending joins to be empty after accept, got %d", len(session.PendingJoins))
	}
	if session.PeerCount() != 1 {
		t.Fatalf("expected 1 joined peer, got %d", session.PeerCount())
	}

	// Render with 1 joined peer
	modal.Render(buf, 80, 24)

	// Test Escape closes modal
	modal.HandleKeyEvent("Escape")
	if modal.Visible {
		t.Fatal("expected modal to be invisible after Escape")
	}
}
