package p2p

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ICEState tracks WebRTC / transport connectivity state.
type ICEState string

const (
	ICEStateNew          ICEState = "new"
	ICEStateConnecting   ICEState = "connecting"
	ICEStateConnected    ICEState = "connected"
	ICEStateDisconnected ICEState = "disconnected"
	ICEStateFailed       ICEState = "failed"
	ICEStateClosed       ICEState = "closed"
)

// PeerTransport abstracts the underlying bidirectional channel (WebRTC DataChannel or pipe).
type PeerTransport interface {
	Send(frame []byte) error
	Close() error
}

// PeerInfo records metadata, permissions, and transport for a connected peer.
type PeerInfo struct {
	ID          uint16    `json:"id"`
	Nickname    string    `json:"nickname"`
	Role        string    `json:"role"` // "editor" or "viewer"
	CanTerminal bool      `json:"can_terminal"`
	ColorHex    string    `json:"color_hex"`
	CursorRune  int       `json:"cursor_rune"`
	ActiveURI   string    `json:"active_uri"`
	LastSeen    time.Time `json:"last_seen"`
	Transport   PeerTransport `json:"-"`
}

// CollaborationSession manages star-topology P2P collaboration sessions.
type CollaborationSession struct {
	mu             sync.RWMutex
	SessionCode    string
	Crypto         *SessionCrypto
	IsHost         bool
	LocalPeerID    uint16
	LocalNickname  string
	ActiveTier     SignalingTier
	ICEState       ICEState
	STUNServers    []string
	TURNServer     string
	Sequencer      *HostSequencer
	Peers          map[uint16]*PeerInfo
	PendingJoins   []MsgJoinRequest
	Transports     map[uint16]PeerTransport // Active data transports
	GuestTransport PeerTransport            // For guest role: direct transport to host

	// Event callbacks
	OnJoinRequest    func(req MsgJoinRequest)
	OnPeerJoined     func(peer *PeerInfo)
	OnPeerLeft       func(peerID uint16)
	OnDocSnapshot    func(snap MsgSnapshot)
	OnOpEdit         func(op MsgOpEdit)
	OnPresence       func(p MsgPresence)
	OnLspDiag        func(diag MsgLspDiag)
	OnTerminalData   func(data []byte)
	OnICEStateChange func(state ICEState)
}

// DefaultSTUNServers lists standard zero-cost STUN reflectors.
var DefaultSTUNServers = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun1.l.google.com:19302",
}

var peerColors = []string{
	"#50FA7B", // Green
	"#FF79C6", // Pink
	"#8BE9FD", // Cyan
	"#BD93F9", // Purple
	"#FFB86C", // Orange
	"#F1FA8C", // Yellow
}

// NewHostSession initializes a new collaboration room as host.
func NewHostSession(nickname string, turnServer string) *CollaborationSession {
	code := GenerateSessionCode()
	crypto := DeriveSessionCrypto(code)

	return &CollaborationSession{
		SessionCode:   code,
		Crypto:        crypto,
		IsHost:        true,
		LocalPeerID:   0, // Host is always Peer 0
		LocalNickname: nickname,
		ActiveTier:    SignalingTierLocalLAN,
		ICEState:      ICEStateConnected,
		STUNServers:   DefaultSTUNServers,
		TURNServer:    turnServer,
		Sequencer:     NewHostSequencer(),
		Peers:         make(map[uint16]*PeerInfo),
		PendingJoins:  make([]MsgJoinRequest, 0),
		Transports:    make(map[uint16]PeerTransport),
	}
}

// NewGuestSession initializes a guest session joining an existing room.
func NewGuestSession(sessionCode, nickname string) *CollaborationSession {
	crypto := DeriveSessionCrypto(sessionCode)

	return &CollaborationSession{
		SessionCode:   sessionCode,
		Crypto:        crypto,
		IsHost:        false,
		LocalPeerID:   uint16(100 + (time.Now().UnixNano() % 900)),
		LocalNickname: nickname,
		ActiveTier:    SignalingTierLocalLAN,
		ICEState:      ICEStateConnecting,
		STUNServers:   DefaultSTUNServers,
		Peers:         make(map[uint16]*PeerInfo),
		Transports:    make(map[uint16]PeerTransport),
	}
}

// SetICEState updates connection state and notifies listener.
func (cs *CollaborationSession) SetICEState(state ICEState) {
	cs.mu.Lock()
	cs.ICEState = state
	cb := cs.OnICEStateChange
	cs.mu.Unlock()

	if cb != nil {
		cb(state)
	}
}

// RegisterPeerTransport attaches a network transport to a peer.
func (cs *CollaborationSession) RegisterPeerTransport(peerID uint16, t PeerTransport) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.IsHost {
		cs.Transports[peerID] = t
		if p, ok := cs.Peers[peerID]; ok {
			p.Transport = t
		}
	} else {
		cs.GuestTransport = t
	}
}

// HandleIncomingPacket processes an encoded packet frame from a peer.
func (cs *CollaborationSession) HandleIncomingPacket(senderPeerID uint16, frame []byte) error {
	if len(frame) < 7 {
		return errors.New("frame too small")
	}
	msgType := frame[2]

	switch msgType {
	case MsgTypeJoinRequest:
		var req MsgJoinRequest
		if _, err := DecodePacket(frame, &req); err != nil {
			return err
		}
		return cs.handleJoinRequest(req)

	case MsgTypeJoinResponse:
		var resp MsgJoinResponse
		if _, err := DecodePacket(frame, &resp); err != nil {
			return err
		}
		return cs.handleJoinResponse(resp)

	case MsgTypeSnapshot:
		var snap MsgSnapshot
		if _, err := DecodePacket(frame, &snap); err != nil {
			return err
		}
		if cs.OnDocSnapshot != nil {
			cs.OnDocSnapshot(snap)
		}

	case MsgTypeOpEdit:
		var op MsgOpEdit
		if _, err := DecodePacket(frame, &op); err != nil {
			return err
		}
		return cs.handleOpEdit(senderPeerID, op)

	case MsgTypePresence:
		var p MsgPresence
		if _, err := DecodePacket(frame, &p); err != nil {
			return err
		}
		return cs.handlePresence(p)

	case MsgTypeLspDiag:
		var diag MsgLspDiag
		if _, err := DecodePacket(frame, &diag); err != nil {
			return err
		}
		if cs.OnLspDiag != nil {
			cs.OnLspDiag(diag)
		}

	case MsgTypeTerminal:
		var term MsgTerminal
		if _, err := DecodePacket(frame, &term); err != nil {
			return err
		}
		return cs.handleTerminal(senderPeerID, term)

	case MsgTypeHeartbeat:
		cs.mu.Lock()
		if p, ok := cs.Peers[senderPeerID]; ok {
			p.LastSeen = time.Now()
		}
		cs.mu.Unlock()
	}

	return nil
}

func (cs *CollaborationSession) handleJoinRequest(req MsgJoinRequest) error {
	cs.mu.Lock()
	if !cs.IsHost {
		cs.mu.Unlock()
		return errors.New("only host can process join requests")
	}

	cs.PendingJoins = append(cs.PendingJoins, req)
	cb := cs.OnJoinRequest
	cs.mu.Unlock()

	if cb != nil {
		cb(req)
	}
	return nil
}

func (cs *CollaborationSession) handleJoinResponse(resp MsgJoinResponse) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.IsHost {
		return errors.New("host received join response")
	}

	if resp.Accepted {
		cs.ICEState = ICEStateConnected
		cs.LocalPeerID = resp.PeerID
	} else {
		cs.ICEState = ICEStateFailed
	}

	if cs.OnICEStateChange != nil {
		cs.OnICEStateChange(cs.ICEState)
	}
	return nil
}

// AcceptGuest approves a pending guest admission request.
func (cs *CollaborationSession) AcceptGuest(peerID uint16, role string, canTerminal bool) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if !cs.IsHost {
		return errors.New("only host can accept guests")
	}

	var req *MsgJoinRequest
	idx := -1
	for i, r := range cs.PendingJoins {
		if r.PeerID == peerID {
			req = &cs.PendingJoins[i]
			idx = i
			break
		}
	}
	if req == nil {
		return fmt.Errorf("peer %d not found in pending queue", peerID)
	}
	cs.PendingJoins = append(cs.PendingJoins[:idx], cs.PendingJoins[idx+1:]...)

	color := peerColors[int(peerID)%len(peerColors)]
	transport := cs.Transports[peerID]
	peerInfo := &PeerInfo{
		ID:          peerID,
		Nickname:    req.Nickname,
		Role:        role,
		CanTerminal: canTerminal,
		ColorHex:    color,
		LastSeen:    time.Now(),
		Transport:   transport,
	}
	cs.Peers[peerID] = peerInfo

	resp := MsgJoinResponse{
		PeerID:      peerID,
		Accepted:    true,
		Role:        role,
		CanTerminal: canTerminal,
	}
	frame, err := EncodePacket(MsgTypeJoinResponse, resp)
	if err == nil {
		if transport != nil {
			_ = transport.Send(frame)
		}
	}

	if cs.OnPeerJoined != nil {
		cs.OnPeerJoined(peerInfo)
	}
	return nil
}

// DeclineGuest rejects a pending guest admission request.
func (cs *CollaborationSession) DeclineGuest(peerID uint16, reason string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if !cs.IsHost {
		return errors.New("only host can decline guests")
	}

	idx := -1
	for i, r := range cs.PendingJoins {
		if r.PeerID == peerID {
			idx = i
			break
		}
	}
	if idx >= 0 {
		cs.PendingJoins = append(cs.PendingJoins[:idx], cs.PendingJoins[idx+1:]...)
	}

	resp := MsgJoinResponse{
		PeerID:   peerID,
		Accepted: false,
		Reason:   reason,
	}
	frame, err := EncodePacket(MsgTypeJoinResponse, resp)
	if err == nil {
		if t, ok := cs.Transports[peerID]; ok && t != nil {
			_ = t.Send(frame)
		}
	}
	return nil
}

func (cs *CollaborationSession) handleOpEdit(senderPeerID uint16, op MsgOpEdit) error {
	if cs.IsHost {
		cs.mu.RLock()
		peer, ok := cs.Peers[senderPeerID]
		cs.mu.RUnlock()

		if !ok || peer.Role != "editor" {
			return fmt.Errorf("peer %d does not have write permissions", senderPeerID)
		}

		// Reconcile via Host Sequencer
		transformed, _, err := cs.Sequencer.SubmitOp(op)
		if err != nil {
			return err
		}

		// Broadcast transformed edit to all other peers
		frame, err := EncodePacket(MsgTypeOpEdit, transformed)
		if err == nil {
			cs.broadcastFrame(frame, senderPeerID)
		}

		if cs.OnOpEdit != nil {
			cs.OnOpEdit(transformed)
		}
	} else {
		// Guest receives authoritative edit from host
		if cs.OnOpEdit != nil {
			cs.OnOpEdit(op)
		}
	}
	return nil
}

func (cs *CollaborationSession) handlePresence(p MsgPresence) error {
	cs.mu.Lock()
	if peer, ok := cs.Peers[p.PeerID]; ok {
		peer.CursorRune = p.CursorRune
		peer.ActiveURI = p.ActiveURI
		peer.LastSeen = time.Now()
	}
	cs.mu.Unlock()

	if cs.IsHost {
		frame, err := EncodePacket(MsgTypePresence, p)
		if err == nil {
			cs.broadcastFrame(frame, p.PeerID)
		}
	}

	if cs.OnPresence != nil {
		cs.OnPresence(p)
	}
	return nil
}

func (cs *CollaborationSession) handleTerminal(senderPeerID uint16, term MsgTerminal) error {
	if cs.IsHost {
		cs.mu.RLock()
		peer, ok := cs.Peers[senderPeerID]
		cs.mu.RUnlock()

		if !ok || !peer.CanTerminal {
			return errors.New("terminal access not permitted for peer")
		}

		if cs.OnTerminalData != nil {
			cs.OnTerminalData(term.Data)
		}
	} else {
		if cs.OnTerminalData != nil {
			cs.OnTerminalData(term.Data)
		}
	}
	return nil
}

// BroadcastEdit broadcasts a local edit to connected peers.
func (cs *CollaborationSession) BroadcastEdit(op MsgOpEdit) error {
	if cs.IsHost {
		transformed, _, err := cs.Sequencer.SubmitOp(op)
		if err != nil {
			return err
		}
		frame, err := EncodePacket(MsgTypeOpEdit, transformed)
		if err != nil {
			return err
		}
		cs.broadcastFrame(frame, 0)
		return nil
	}

	// Guest sends edit to host
	frame, err := EncodePacket(MsgTypeOpEdit, op)
	if err != nil {
		return err
	}
	if cs.GuestTransport != nil {
		return cs.GuestTransport.Send(frame)
	}
	return errors.New("no transport connected to host")
}

// BroadcastPresence sends cursor/selection presence to peers.
func (cs *CollaborationSession) BroadcastPresence(cursorRune, selStart, selEnd int, activeURI string) error {
	p := MsgPresence{
		PeerID:         cs.LocalPeerID,
		Nickname:       cs.LocalNickname,
		ActiveURI:      activeURI,
		CursorRune:     cursorRune,
		SelectionStart: selStart,
		SelectionEnd:   selEnd,
	}
	frame, err := EncodePacket(MsgTypePresence, p)
	if err != nil {
		return err
	}

	if cs.IsHost {
		cs.broadcastFrame(frame, 0)
	} else if cs.GuestTransport != nil {
		return cs.GuestTransport.Send(frame)
	}
	return nil
}

// BroadcastDiagnostics pushes host LSP diagnostics to all guest peers.
func (cs *CollaborationSession) BroadcastDiagnostics(diag MsgLspDiag) error {
	if !cs.IsHost {
		return errors.New("only host broadcasts diagnostics")
	}
	frame, err := EncodePacket(MsgTypeLspDiag, diag)
	if err != nil {
		return err
	}
	cs.broadcastFrame(frame, 0)
	return nil
}

// SendTerminalOutput streams host PTY output to all guests with terminal access.
func (cs *CollaborationSession) SendTerminalOutput(sessionID string, data []byte) error {
	term := MsgTerminal{
		SessionID: sessionID,
		Data:      data,
	}
	frame, err := EncodePacket(MsgTypeTerminal, term)
	if err != nil {
		return err
	}

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	for _, p := range cs.Peers {
		if p.CanTerminal && p.Transport != nil {
			_ = p.Transport.Send(frame)
		}
	}
	return nil
}

func (cs *CollaborationSession) broadcastFrame(frame []byte, excludePeerID uint16) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	for id, peer := range cs.Peers {
		if id != excludePeerID && peer.Transport != nil {
			_ = peer.Transport.Send(frame)
		}
	}
}

// PeerCount returns the number of active joined peers.
func (cs *CollaborationSession) PeerCount() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.Peers)
}

// Close disconnects all peers and closes active transports.
func (cs *CollaborationSession) Close() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.ICEState = ICEStateClosed
	for _, p := range cs.Peers {
		if p.Transport != nil {
			_ = p.Transport.Close()
		}
	}
	if cs.GuestTransport != nil {
		_ = cs.GuestTransport.Close()
	}
	return nil
}
