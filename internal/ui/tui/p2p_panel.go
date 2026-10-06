package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/core/p2p"
	"tahr/internal/ui"
)

// P2PButtonHit records screen coordinates and metadata for interactive buttons.
type P2PButtonHit struct {
	Action string // "host", "join", "leave", "copy_code", "accept_guest", "decline_guest", "follow_peer", "toggle_role", "toggle_pty", "focus_nick", "focus_code"
	PeerID uint16
	X, Y   int
	Width  int
}

// P2PPanel manages state, rendering, and mouse/keyboard interactions for P2P collaboration.
type P2PPanel struct {
	Session        *p2p.CollaborationSession
	Nickname       string
	JoinCodeInput  string
	ActiveField    string // "nick", "code", or ""
	PendingRole    string // "editor" or "viewer"
	PendingPTY     bool
	AutoFollow     bool
	FollowedPeerID uint16
	CopiedHint     bool
	CopiedTimer    time.Time
	ScrollOffset   int

	ButtonHits []P2PButtonHit

	// Callbacks
	OnStartHost    func(nickname string)
	OnJoinSession  func(code, nickname string)
	OnLeaveSession func()
	OnAcceptGuest  func(peerID uint16, role string, pty bool)
	OnDeclineGuest func(peerID uint16)
	OnFollowPeer   func(peer *p2p.PeerInfo)
	OnCopyCode     func(code string)
	OnToast        func(level, title, msg string)
}

// NewP2PPanel creates a new P2P collaboration panel.
func NewP2PPanel(theme *ui.Theme) *P2PPanel {
	return &P2PPanel{
		Nickname:    "dev",
		PendingRole: "editor",
		PendingPTY:  false,
		AutoFollow:  true,
		ButtonHits:  make([]P2PButtonHit, 0),
	}
}

// IsFocused returns true if any text input in the panel is currently active.
func (p *P2PPanel) IsFocused() bool {
	return p.ActiveField != ""
}

// HandleKey handles keyboard events for active input fields inside P2PPanel.
func (p *P2PPanel) HandleKey(k input.Key) bool {
	if p.ActiveField == "" {
		return false
	}

	switch k.Type {
	case input.KeyEsc:
		if p.ActiveField == "nick" && strings.TrimSpace(p.Nickname) == "" {
			p.Nickname = "dev"
		}
		p.ActiveField = ""
		return true

	case input.KeyEnter:
		if p.ActiveField == "nick" {
			p.Nickname = strings.TrimSpace(p.Nickname)
			if p.Nickname == "" {
				p.Nickname = "dev"
			}
			p.ActiveField = ""
			return true
		} else if p.ActiveField == "code" && strings.TrimSpace(p.JoinCodeInput) != "" {
			if p.OnJoinSession != nil {
				nick := strings.TrimSpace(p.Nickname)
				if nick == "" {
					nick = "dev"
				}
				p.OnJoinSession(strings.TrimSpace(p.JoinCodeInput), nick)
			}
			p.ActiveField = ""
			return true
		}
		p.ActiveField = ""
		return true

	case input.KeyBackspace:
		if p.ActiveField == "nick" {
			runes := []rune(p.Nickname)
			if len(runes) > 0 {
				p.Nickname = string(runes[:len(runes)-1])
			}
			return true
		} else if p.ActiveField == "code" {
			runes := []rune(p.JoinCodeInput)
			if len(runes) > 0 {
				p.JoinCodeInput = string(runes[:len(runes)-1])
			}
			return true
		}

	default:
		if k.Rune != 0 {
			if p.ActiveField == "nick" {
				if len(p.Nickname) < 20 {
					p.Nickname += string(k.Rune)
				}
				return true
			} else if p.ActiveField == "code" {
				if len(p.JoinCodeInput) < 30 {
					p.JoinCodeInput += strings.ToLower(string(k.Rune))
				}
				return true
			}
		}
	}

	return false
}

// HandleClick processes mouse clicks on interactive buttons and inputs.
func (p *P2PPanel) HandleClick(x, y int) bool {
	for _, hit := range p.ButtonHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.Width {
			switch hit.Action {
			case "focus_nick":
				p.ActiveField = "nick"
				return true

			case "focus_code":
				p.ActiveField = "code"
				return true

			case "host":
				nick := strings.TrimSpace(p.Nickname)
				if nick == "" {
					nick = "dev"
				}
				if p.OnStartHost != nil {
					p.OnStartHost(nick)
				}
				p.ActiveField = ""
				return true

			case "join":
				code := strings.TrimSpace(p.JoinCodeInput)
				if code == "" {
					p.ActiveField = "code"
					if p.OnToast != nil {
						p.OnToast("warn", "COLLAB", i18n.T("p2p.toast_enter_code"))
					}
					return true
				}
				nick := strings.TrimSpace(p.Nickname)
				if nick == "" {
					nick = "dev"
				}
				if p.OnJoinSession != nil {
					p.OnJoinSession(code, nick)
				}
				p.ActiveField = ""
				return true

			case "leave":
				if p.OnLeaveSession != nil {
					p.OnLeaveSession()
				}
				return true

			case "copy_code":
				if p.Session != nil {
					p.CopiedHint = true
					p.CopiedTimer = time.Now()
					if p.OnCopyCode != nil {
						p.OnCopyCode(p.Session.SessionCode)
					}
				}
				return true

			case "accept_guest":
				if p.OnAcceptGuest != nil {
					p.OnAcceptGuest(hit.PeerID, p.PendingRole, p.PendingPTY)
				}
				return true

			case "decline_guest":
				if p.OnDeclineGuest != nil {
					p.OnDeclineGuest(hit.PeerID)
				}
				return true

			case "follow_peer":
				if p.Session != nil {
					p.FollowedPeerID = hit.PeerID
					if peer, ok := p.Session.Peers[hit.PeerID]; ok && p.OnFollowPeer != nil {
						p.OnFollowPeer(peer)
					}
				}
				return true

			case "toggle_role":
				if p.PendingRole == "editor" {
					p.PendingRole = "viewer"
				} else {
					p.PendingRole = "editor"
				}
				return true

			case "toggle_pty":
				p.PendingPTY = !p.PendingPTY
				return true

			case "toggle_autofollow":
				p.AutoFollow = !p.AutoFollow
				return true
			}
		}
	}

	// Click outside inputs clears focus
	if p.ActiveField != "" {
		p.ActiveField = ""
		return true
	}
	return false
}

// Render draws the P2P collaboration panel inside the given rectangle.
func (p *P2PPanel) Render(buf *buffer.Buffer, startX, topY, sideW, sideH int, theme *ui.Theme) {
	if sideW <= 0 || sideH <= 0 {
		return
	}

	p.ButtonHits = p.ButtonHits[:0]

	fg := toColor(theme.Foreground)
	bg := toColor(theme.GutterBg)
	accentFg := toColor(theme.Function)
	dimFg := toColor(theme.Comment)
	keywordFg := toColor(theme.Keyword)
	stringFg := toColor(theme.String)
	warnFg := toColor(theme.DiagnosticWarn)
	greenFg := toColor(0x9CCFD8)
	borderFg := toColor(theme.BorderColor)

	printLine := func(y int, text string, color cell.Color, attr cell.Modifier) {
		if y < topY || y >= topY+sideH {
			return
		}
		runes := []rune(text)
		for i, r := range runes {
			if startX+1+i < startX+sideW-1 {
				buf.SetRune(startX+1+i, y, r, color, bg, attr)
			}
		}
	}

	drawDivider := func(y int) {
		if y < topY || y >= topY+sideH {
			return
		}
		for x := startX + 1; x < startX+sideW-1; x++ {
			buf.SetRune(x, y, '┄', borderFg, bg, cell.AttrNone)
		}
	}

	registerHit := func(action string, peerID uint16, x, y, width int) {
		p.ButtonHits = append(p.ButtonHits, P2PButtonHit{
			Action: action,
			PeerID: peerID,
			X:      x,
			Y:      y,
			Width:  width,
		})
	}

	curY := topY

	// 1. Header (clean title, no emojis)
	printLine(curY, i18n.T("p2p.title"), accentFg, cell.AttrBold)
	curY++

	if p.Session == nil {
		// ==================== DISCONNECTED STATE ====================
		printLine(curY, i18n.T("p2p.subtitle"), dimFg, cell.AttrNone)
		curY++
		drawDivider(curY)
		curY += 2

		// Nickname Input
		printLine(curY, i18n.T("p2p.your_nickname"), keywordFg, cell.AttrNone)
		curY++

		nickVal := p.Nickname
		var nickBox string
		if p.ActiveField == "nick" {
			nickBox = i18n.T("p2p.nick_prefix", nickVal) + "█"
		} else {
			if nickVal == "" {
				nickVal = "dev"
			}
			nickBox = i18n.T("p2p.nick_prefix", nickVal)
		}
		printLine(curY, nickBox, fg, cell.AttrBold)
		registerHit("focus_nick", 0, startX+1, curY, len([]rune(nickBox)))
		curY += 2

		// Host Action Button (clean text, no [ ])
		hostBtn := i18n.T("p2p.create_room")
		printLine(curY, hostBtn, greenFg, cell.AttrBold)
		registerHit("host", 0, startX+1, curY, len([]rune(hostBtn)))
		curY += 2

		drawDivider(curY)
		curY += 2

		// Join Session Section
		printLine(curY, i18n.T("p2p.connect_to_friend"), keywordFg, cell.AttrNone)
		curY++

		codeColor := fg
		var codeBox string
		if p.ActiveField == "code" {
			codeBox = i18n.T("p2p.code_prefix", p.JoinCodeInput) + "█"
		} else {
			if p.JoinCodeInput == "" {
				codeBox = i18n.T("p2p.code_prefix", i18n.T("p2p.code_placeholder"))
				codeColor = dimFg
			} else {
				codeBox = i18n.T("p2p.code_prefix", p.JoinCodeInput)
			}
		}
		printLine(curY, codeBox, codeColor, cell.AttrNone)
		registerHit("focus_code", 0, startX+1, curY, len([]rune(codeBox)))
		curY += 2

		// Join Action Button (clean text, no [ ])
		joinBtn := i18n.T("p2p.connect_button")
		printLine(curY, joinBtn, accentFg, cell.AttrBold)
		registerHit("join", 0, startX+1, curY, len([]rune(joinBtn)))
		curY += 2

		drawDivider(curY)
		curY++

		// Automated Cascade Discovery details
		infoLines := []string{
			i18n.T("p2p.cascade_discovery"),
			i18n.T("p2p.discovery_mdns"),
			i18n.T("p2p.discovery_nostr"),
			i18n.T("p2p.discovery_dht"),
			"",
			i18n.T("p2p.features"),
			i18n.T("p2p.feat_collab_edit"),
			i18n.T("p2p.feat_colored_cursors"),
			i18n.T("p2p.feat_follow_cursor"),
			i18n.T("p2p.feat_e2e_encryption"),
		}
		for _, info := range infoLines {
			if curY >= topY+sideH {
				break
			}
			c := dimFg
			attr := cell.AttrNone
			if strings.HasSuffix(info, ":") {
				c = keywordFg
				attr = cell.AttrBold
			}
			printLine(curY, info, c, attr)
			curY++
		}

	} else {
		// ==================== CONNECTED / ACTIVE STATE ====================
		roleLabel := i18n.T("p2p.role_host")
		if !p.Session.IsHost {
			roleLabel = i18n.T("p2p.role_guest")
		}

		iceState := string(p.Session.ICEState)
		if iceState == "" {
			iceState = "connected"
		}

		statusLine := i18n.T("p2p.status_line", roleLabel, iceState)
		printLine(curY, statusLine, greenFg, cell.AttrBold)
		curY++

		// Multi-tier cascade live status
		tierName := "LAN (mDNS)"
		if p.Session.Coordinator != nil {
			if p.Session.Coordinator.IsResolved() {
				switch p.Session.ActiveTier {
				case p2p.SignalingTierNostr:
					tierName = "Nostr WebSocket"
				case p2p.SignalingTierDHT:
					tierName = "BitTorrent DHT"
				default:
					tierName = "LAN mDNS"
				}
				printLine(curY, i18n.T("p2p.signaling_prefix", tierName), dimFg, cell.AttrNone)
			} else {
				if p.Session.Coordinator.IsTierActive(p2p.SignalingTierDHT) {
					printLine(curY, i18n.T("p2p.signaling_searching"), dimFg, cell.AttrNone)
				} else {
					printLine(curY, i18n.T("p2p.signaling_cascade_delay"), dimFg, cell.AttrNone)
				}
			}
		} else {
			printLine(curY, i18n.T("p2p.signaling_prefix", tierName), dimFg, cell.AttrNone)
		}
		curY++
		drawDivider(curY)
		curY += 2

		// Session Code Banner
		printLine(curY, i18n.T("p2p.share_code_title"), keywordFg, cell.AttrNone)
		curY++

		codeDisplay := i18n.T("p2p.code_prefix", p.Session.SessionCode)
		printLine(curY, codeDisplay, accentFg, cell.AttrBold)
		registerHit("copy_code", 0, startX+1, curY, len([]rune(codeDisplay)))
		curY++

		copyText := i18n.T("p2p.copy_code")
		if p.CopiedHint && time.Since(p.CopiedTimer) < 3*time.Second {
			copyText = i18n.T("p2p.copy_code_done")
		}
		printLine(curY, copyText, stringFg, cell.AttrNone)
		registerHit("copy_code", 0, startX+1, curY, len([]rune(copyText)))
		curY += 2

		// Pending Guest Admissions (Host only)
		if p.Session.IsHost && len(p.Session.PendingJoins) > 0 {
			drawDivider(curY)
			curY++
			req := p.Session.PendingJoins[0]
			printLine(curY, i18n.T("p2p.join_request", req.Nickname, req.PeerID), warnFg, cell.AttrBold)
			curY++

			ptyLabel := i18n.T("p2p.perm_pty_off")
			if p.PendingPTY {
				ptyLabel = i18n.T("p2p.perm_pty_on")
			}
			permLine := i18n.T("p2p.perm_line", p.PendingRole, ptyLabel)
			printLine(curY, permLine, fg, cell.AttrNone)

			roleHitX := startX + 1 + strings.Index(permLine, p.PendingRole)
			registerHit("toggle_role", req.PeerID, roleHitX, curY, len(p.PendingRole))
			ptyHitX := startX + 1 + strings.Index(permLine, ptyLabel)
			registerHit("toggle_pty", req.PeerID, ptyHitX, curY, len([]rune(ptyLabel)))
			curY++

			acceptText := i18n.T("p2p.action_accept")
			declineText := i18n.T("p2p.action_decline")
			actionsLine := fmt.Sprintf("%s    %s", acceptText, declineText)
			printLine(curY, actionsLine, greenFg, cell.AttrBold)
			registerHit("accept_guest", req.PeerID, startX+1, curY, len([]rune(acceptText)))
			registerHit("decline_guest", req.PeerID, startX+1+len([]rune(acceptText))+4, curY, len([]rune(declineText)))
			curY += 2
		}

		drawDivider(curY)
		curY++

		// Connected Peers List
		peerCount := p.Session.PeerCount()
		peersTitle := i18n.T("p2p.participants_title", peerCount)
		printLine(curY, peersTitle, keywordFg, cell.AttrBold)
		curY++

		if peerCount == 0 {
			printLine(curY, i18n.T("p2p.no_participants"), dimFg, cell.AttrNone)
			curY++
		} else {
			for _, peer := range p.Session.Peers {
				if curY >= topY+sideH-3 {
					break
				}
				pColor := greenFg
				if peer.ColorHex != "" {
					pColor = toColor(cell.ColorHex(peer.ColorHex).Value)
				}
				ptyBadge := ""
				if peer.CanTerminal {
					ptyBadge = " (PTY)"
				}
				peerHeader := fmt.Sprintf("* %s (%s)%s", peer.Nickname, peer.Role, ptyBadge)
				printLine(curY, peerHeader, pColor, cell.AttrBold)
				curY++

				fileLoc := i18n.T("p2p.in_editor")
				if peer.ActiveURI != "" {
					fileLoc = peer.ActiveURI
					if strings.Contains(fileLoc, "/") || strings.Contains(fileLoc, "\\") {
						parts := strings.FieldsFunc(fileLoc, func(r rune) bool { return r == '/' || r == '\\' })
						if len(parts) > 0 {
							fileLoc = parts[len(parts)-1]
						}
					}
				}
				peerDetail := i18n.T("p2p.peer_location", fileLoc)
				printLine(curY, peerDetail, dimFg, cell.AttrNone)
				curY++

				followBtn := i18n.T("p2p.follow_start")
				if p.FollowedPeerID == peer.ID {
					followBtn = i18n.T("p2p.following")
				}
				printLine(curY, followBtn, accentFg, cell.AttrNone)
				registerHit("follow_peer", peer.ID, startX+1, curY, len([]rune(followBtn)))
				curY += 2
			}
		}

		// Disconnect Button
		if curY < topY+sideH-1 {
			drawDivider(curY)
			curY++
			leaveBtn := i18n.T("p2p.leave_session")
			printLine(curY, leaveBtn, warnFg, cell.AttrBold)
			registerHit("leave", 0, startX+1, curY, len([]rune(leaveBtn)))
		}
	}
}
