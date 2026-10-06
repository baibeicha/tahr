package tui

import (
	"fmt"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/core/p2p"
	"tahr/internal/ui"
)

// P2PModal renders the collaboration status, session code, and guest admission modal.
type P2PModal struct {
	Session          *p2p.CollaborationSession
	Visible          bool
	Theme            *ui.Theme
	PendingRole      string // "editor" or "viewer"
	PendingPTY       bool   // Terminal access granted
	CopiedHint       bool
}

// NewP2PModal creates a modal manager for a P2P collaboration session.
func NewP2PModal(session *p2p.CollaborationSession, theme *ui.Theme) *P2PModal {
	return &P2PModal{
		Session:     session,
		Visible:     true,
		Theme:       theme,
		PendingRole: "editor",
		PendingPTY:  false,
	}
}

// HandleKeyEvent handles interactive key inputs on the modal.
func (m *P2PModal) HandleKeyEvent(key string) bool {
	if !m.Visible || m.Session == nil {
		return false
	}

	switch key {
	case "Escape":
		m.Visible = false
		return true

	case "t", "T":
		if m.PendingRole == "editor" {
			m.PendingRole = "viewer"
		} else {
			m.PendingRole = "editor"
		}
		return true

	case "p", "P":
		m.PendingPTY = !m.PendingPTY
		return true

	case "y", "Y", "Enter":
		if m.Session.IsHost && len(m.Session.PendingJoins) > 0 {
			req := m.Session.PendingJoins[0]
			_ = m.Session.AcceptGuest(req.PeerID, m.PendingRole, m.PendingPTY)
			return true
		}
		m.CopiedHint = true
		return true

	case "n", "N":
		if m.Session.IsHost && len(m.Session.PendingJoins) > 0 {
			req := m.Session.PendingJoins[0]
			_ = m.Session.DeclineGuest(req.PeerID, "rejected by host")
			return true
		}
	}

	return false
}

// Render draws the P2P modal dialog centered over screen bounds.
func (m *P2PModal) Render(buf *buffer.Buffer, screenW, screenH int) {
	if !m.Visible || m.Session == nil {
		return
	}

	w := screenW - 8
	if w > 74 {
		w = 74
	}
	h := screenH - 4
	if h > 20 {
		h = 20
	}
	x := (screenW - w) / 2
	y := (screenH - h) / 2

	fg := toColor(m.Theme.Foreground)
	bg := toColor(m.Theme.Background)
	borderFg := toColor(m.Theme.Function)
	accentFg := toColor(0x31748F) // Foam/Blue accent
	greenFg := toColor(0x9CCFD8)  // Mint green

	// 1. Draw Modal Window Frame
	for cx := x; cx < x+w; cx++ {
		for cy := y; cy < y+h; cy++ {
			buf.SetRune(cx, cy, ' ', fg, bg, cell.AttrNone)
		}
	}
	for cx := x; cx < x+w; cx++ {
		buf.SetRune(cx, y, '─', borderFg, bg, cell.AttrNone)
		buf.SetRune(cx, y+h-1, '─', borderFg, bg, cell.AttrNone)
	}
	for cy := y; cy < y+h; cy++ {
		buf.SetRune(x, cy, '│', borderFg, bg, cell.AttrNone)
		buf.SetRune(x+w-1, cy, '│', borderFg, bg, cell.AttrNone)
	}
	buf.SetRune(x, y, '┌', borderFg, bg, cell.AttrNone)
	buf.SetRune(x+w-1, y, '┐', borderFg, bg, cell.AttrNone)
	buf.SetRune(x, y+h-1, '└', borderFg, bg, cell.AttrNone)
	buf.SetRune(x+w-1, y+h-1, '┘', borderFg, bg, cell.AttrNone)

	// 2. Title Header
	title := " P2P Multi-User Collaboration "
	for i, r := range []rune(title) {
		buf.SetRune(x+3+i, y, r, borderFg, bg, cell.AttrBold)
	}

	// 3. Session Code Banner
	rowY := y + 2
	codePrompt := fmt.Sprintf("Session Code: %s", m.Session.SessionCode)
	if m.CopiedHint {
		codePrompt += " (Copied to clipboard!)"
	} else {
		codePrompt += " (Enter: Copy)"
	}
	for i, r := range []rune(codePrompt) {
		buf.SetRune(x+3+i, rowY, r, accentFg, bg, cell.AttrBold)
	}
	rowY += 2

	// 4. Status Metadata
	tierName := "LAN (mDNS Multicast 0-30ms)"
	switch m.Session.ActiveTier {
	case p2p.SignalingTierNostr:
		tierName = "Nostr WebSocket Relay (150-300ms)"
	case p2p.SignalingTierDHT:
		tierName = "BitTorrent DHT (+2.5s fallback)"
	}
	roleLabel := "Host (Authority Master)"
	if !m.Session.IsHost {
		roleLabel = "Guest (Connected to Host)"
	}

	statusLine := fmt.Sprintf("Role: %s  │  ICE: %s  │  Tier: %s", roleLabel, m.Session.ICEState, tierName)
	for i, r := range []rune(statusLine) {
		if x+3+i < x+w-3 {
			buf.SetRune(x+3+i, rowY, r, fg, bg, cell.AttrNone)
		}
	}
	rowY += 2

	// Separator
	for cx := x + 2; cx < x+w-2; cx++ {
		buf.SetRune(cx, rowY, '┄', borderFg, bg, cell.AttrNone)
	}
	rowY += 2

	// 5. Admission Box if pending request
	if m.Session.IsHost && len(m.Session.PendingJoins) > 0 {
		req := m.Session.PendingJoins[0]
		reqLine := fmt.Sprintf(">> ADMISSION REQUEST: Guest '%s' (ID %d) wants to join", req.Nickname, req.PeerID)
		for i, r := range []rune(reqLine) {
			if x+3+i < x+w-3 {
				buf.SetRune(x+3+i, rowY, r, toColor(0xEB6F92), bg, cell.AttrBold)
			}
		}
		rowY++

		ptyState := "Locked"
		if m.PendingPTY {
			ptyState = "Enabled"
		}
		toggleLine := fmt.Sprintf("   Role: %s (T to toggle)   Terminal PTY: %s (P to toggle)", m.PendingRole, ptyState)
		for i, r := range []rune(toggleLine) {
			if x+3+i < x+w-3 {
				buf.SetRune(x+3+i, rowY, r, accentFg, bg, cell.AttrNone)
			}
		}
		rowY++

		actionLine := "   Accept: Press Y / Enter    Decline: Press N"
		for i, r := range []rune(actionLine) {
			if x+3+i < x+w-3 {
				buf.SetRune(x+3+i, rowY, r, greenFg, bg, cell.AttrBold)
			}
		}
		rowY += 2
	} else {
		// Connected Peers summary
		peersHeader := fmt.Sprintf("Connected Peers (%d):", m.Session.PeerCount())
		for i, r := range []rune(peersHeader) {
			buf.SetRune(x+3+i, rowY, r, fg, bg, cell.AttrBold)
		}
		rowY++

		if m.Session.PeerCount() == 0 {
			noPeers := "  No remote peers connected yet. Share the session code with teammates!"
			for i, r := range []rune(noPeers) {
				if x+3+i < x+w-3 {
					buf.SetRune(x+3+i, rowY, r, toColor(m.Theme.LineNumber), bg, cell.AttrNone)
				}
			}
			rowY++
		} else {
			for _, p := range m.Session.Peers {
				ptyTag := ""
				if p.CanTerminal {
					ptyTag = " (PTY)"
				}
				peerLine := fmt.Sprintf("  * %-12s  Role: %s%s  Cursor: %d", p.Nickname, p.Role, ptyTag, p.CursorRune)
				for i, r := range []rune(peerLine) {
					if x+3+i < x+w-3 {
						buf.SetRune(x+3+i, rowY, r, greenFg, bg, cell.AttrNone)
					}
				}
				rowY++
				if rowY >= y+h-3 {
					break
				}
			}
		}
	}

	// 6. Action Footer
	footer := " Close (Esc) "
	footX := x + (w-len(footer))/2
	for i, r := range []rune(footer) {
		buf.SetRune(footX+i, y+h-2, r, borderFg, bg, cell.AttrBold)
	}
}
