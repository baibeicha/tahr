package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	tea "github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/p2p"
	"tahr/internal/ui"
)

func TestP2PPanel_InitialDisconnectedState(t *testing.T) {
	theme := ui.DefaultTheme()
	panel := NewP2PPanel(&theme)

	buf := buffer.NewBuffer(50, 25)
	panel.Render(buf, 0, 0, 50, 25, &theme)

	// Check title and host button rendered
	foundTitle := false
	foundHost := false
	hasBracket := false
	for y := 0; y < 25; y++ {
		var line strings.Builder
		for x := 0; x < 50; x++ {
			c := buf.Cell(x, y)
			if c != nil && c.Rune != 0 {
				line.WriteRune(c.Rune)
			} else {
				line.WriteRune(' ')
			}
		}
		str := line.String()
		if strings.Contains(str, "Совместная работа") {
			foundTitle = true
		}
		if strings.Contains(str, "Создать комнату") {
			foundHost = true
		}
		if strings.Contains(str, "[") || strings.Contains(str, "]") {
			hasBracket = true
		}
	}

	if !foundTitle {
		t.Fatalf("expected title 'Совместная работа' to be rendered")
	}
	if !foundHost {
		t.Fatalf("expected 'Создать комнату' to be rendered")
	}
	if hasBracket {
		t.Fatalf("expected NO brackets [ or ] in rendered panel")
	}
}

func TestP2PPanel_StartHostAndCopyCode(t *testing.T) {
	theme := ui.DefaultTheme()
	panel := NewP2PPanel(&theme)

	var startedNick string
	panel.OnStartHost = func(nickname string) {
		startedNick = nickname
	}

	buf := buffer.NewBuffer(50, 25)
	panel.Render(buf, 0, 0, 50, 25, &theme)

	// Find and click the host button
	hostHit := false
	for _, hit := range panel.ButtonHits {
		if hit.Action == "host" {
			handled := panel.HandleClick(hit.X+1, hit.Y)
			if !handled {
				t.Fatalf("expected host button click to be handled")
			}
			hostHit = true
			break
		}
	}
	if !hostHit {
		t.Fatalf("host button hitbox not found")
	}
	if startedNick != "dev" {
		t.Fatalf("expected nickname 'dev', got %q", startedNick)
	}

	// Now simulate active session
	session := p2p.NewHostSession("dev", "")
	panel.Session = session

	var copiedCode string
	panel.OnCopyCode = func(code string) {
		copiedCode = code
	}

	buf = buffer.NewBuffer(50, 25)
	panel.Render(buf, 0, 0, 50, 25, &theme)

	// Click copy button
	copyHit := false
	for _, hit := range panel.ButtonHits {
		if hit.Action == "copy_code" {
			handled := panel.HandleClick(hit.X+1, hit.Y)
			if !handled {
				t.Fatalf("expected copy_code click to be handled")
			}
			copyHit = true
			break
		}
	}
	if !copyHit {
		t.Fatalf("copy_code button hitbox not found")
	}
	if copiedCode != session.SessionCode {
		t.Fatalf("expected copied code %q, got %q", session.SessionCode, copiedCode)
	}
}

func TestP2PPanel_GuestAdmissionAndFollow(t *testing.T) {
	theme := ui.DefaultTheme()
	panel := NewP2PPanel(&theme)

	session := p2p.NewHostSession("alice", "")
	panel.Session = session

	// Add pending guest
	session.PendingJoins = append(session.PendingJoins, p2p.MsgJoinRequest{
		PeerID:   105,
		Nickname: "bob",
		Role:     "editor",
	})

	var acceptedPeerID uint16
	panel.OnAcceptGuest = func(peerID uint16, role string, pty bool) {
		acceptedPeerID = peerID
		_ = session.AcceptGuest(peerID, role, pty)
	}

	buf := buffer.NewBuffer(50, 25)
	panel.Render(buf, 0, 0, 50, 25, &theme)

	// Accept guest via click
	acceptHit := false
	for _, hit := range panel.ButtonHits {
		if hit.Action == "accept_guest" {
			handled := panel.HandleClick(hit.X+1, hit.Y)
			if !handled {
				t.Fatalf("expected accept_guest click to be handled")
			}
			acceptHit = true
			break
		}
	}
	if !acceptHit {
		t.Fatalf("accept_guest button hitbox not found")
	}
	if acceptedPeerID != 105 {
		t.Fatalf("expected accepted peer ID 105, got %d", acceptedPeerID)
	}

	// Verify bob is now in peers list
	peer, ok := session.Peers[105]
	if !ok {
		t.Fatalf("peer 105 not found in session peers")
	}
	peer.ActiveURI = "d:/tahr/cmd/main.go"
	peer.CursorRune = 120

	var followedPeer *p2p.PeerInfo
	panel.OnFollowPeer = func(p *p2p.PeerInfo) {
		followedPeer = p
	}

	buf = buffer.NewBuffer(50, 25)
	panel.Render(buf, 0, 0, 50, 25, &theme)

	followHit := false
	for _, hit := range panel.ButtonHits {
		if hit.Action == "follow_peer" && hit.PeerID == 105 {
			handled := panel.HandleClick(hit.X+1, hit.Y)
			if !handled {
				t.Fatalf("expected follow_peer click to be handled")
			}
			followHit = true
			break
		}
	}
	if !followHit {
		t.Fatalf("follow_peer button hitbox not found")
	}
	if followedPeer == nil || followedPeer.Nickname != "bob" {
		t.Fatalf("expected followed peer bob, got %v", followedPeer)
	}
}

func TestAppModel_P2PCollabIntegration(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	// 1. Verify right activity strip includes P2P Collab item
	stripItems := app.getRightStripItems()
	foundCollab := false
	for _, it := range stripItems {
		if it.id == "p2p-collab" && it.r1 == 'C' && it.r2 == 'O' {
			foundCollab = true
			break
		}
	}
	if !foundCollab {
		t.Fatalf("expected p2p-collab in getRightStripItems")
	}

	// 2. Toggle Right Sidebar with p2p-collab
	app.ToggleRightSidebar("p2p-collab")
	if !app.rightSidebarOpen {
		t.Fatalf("expected right sidebar to be open")
	}
	if app.rightSidebarMode != "p2p-collab" {
		t.Fatalf("expected right sidebar mode p2p-collab, got %q", app.rightSidebarMode)
	}
	if app.rightSidebarTitle != "P2P Collaboration" {
		t.Fatalf("expected title 'P2P Collaboration', got %q", app.rightSidebarTitle)
	}

	// 3. Render frame to verify no panic
	app.rightSidebarAnimWidth = 34
	buf := buffer.NewBuffer(100, 30)
	app.renderRightSidebar(buf, 66, 2, 34, 26)

	// 4. Test Ctrl+Shift+L shortcut
	key := tea.KeyMsg{
		Key: input.Key{
			Type: input.KeyRune,
			Rune: 'l',
			Mod:  input.ModCtrl | input.ModShift,
		},
	}
	app.rightSidebarOpen = false
	_, cmd := app.Update(key)
	if cmd == nil {
		t.Fatalf("expected tea.Cmd returned from Ctrl+Shift+L")
	}
	if !app.rightSidebarOpen || app.rightSidebarMode != "p2p-collab" {
		t.Fatalf("expected right sidebar open in p2p-collab mode after Ctrl+Shift+L")
	}
}
