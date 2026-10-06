package p2p

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"unicode/utf8"
)

// Magic bytes for Tahr P2P protocol frame: 'T', 'P'
var ProtocolMagic = [2]byte{0x54, 0x50}

// Protocol message types
const (
	MsgTypeJoinRequest  byte = 0x01
	MsgTypeJoinResponse byte = 0x02
	MsgTypeSnapshot     byte = 0x03
	MsgTypeOpEdit       byte = 0x04
	MsgTypePresence     byte = 0x05
	MsgTypeLspDiag      byte = 0x06
	MsgTypeTerminal     byte = 0x07
	MsgTypeHeartbeat    byte = 0x08
)

// MsgJoinRequest is sent by a guest wishing to join the session.
type MsgJoinRequest struct {
	PeerID    uint16 `json:"peer_id"`
	Nickname  string `json:"nickname"`
	PublicKey string `json:"public_key"`
	Role      string `json:"role"` // "editor" or "viewer"
}

// MsgJoinResponse is returned by the host indicating admission status.
type MsgJoinResponse struct {
	PeerID      uint16 `json:"peer_id"`
	Accepted    bool   `json:"accepted"`
	Reason      string `json:"reason,omitempty"`
	Role        string `json:"role"` // "editor" or "viewer"
	CanTerminal bool   `json:"can_terminal"`
}

// MsgSnapshot contains full document state for initial sync.
type MsgSnapshot struct {
	DocURI   string `json:"doc_uri"`
	Revision uint64 `json:"revision"`
	Content  string `json:"content"`
}

// MsgOpEdit represents an atomic text modification expressed in Unicode runes.
type MsgOpEdit struct {
	DocURI      string `json:"doc_uri"`
	Revision    uint64 `json:"revision"`
	PeerID      uint16 `json:"peer_id"`
	PosRune     int    `json:"pos_rune"`
	DeleteCount int    `json:"delete_count"`
	InsertText  string `json:"insert_text"`
}

// MsgPresence carries ephemeral cursor and selection state.
type MsgPresence struct {
	PeerID         uint16 `json:"peer_id"`
	Nickname       string `json:"nickname"`
	ColorHex       string `json:"color_hex"`
	ActiveURI      string `json:"active_uri"`
	CursorRune     int    `json:"cursor_rune"`
	SelectionStart int    `json:"selection_start"`
	SelectionEnd   int    `json:"selection_end"`
}

// MsgLspDiag broadcasts host LSP diagnostics to connected guests.
type MsgLspDiag struct {
	DocURI    string `json:"doc_uri"`
	Severity  int    `json:"severity"` // 1: Error, 2: Warning, 3: Info, 4: Hint
	StartRune int    `json:"start_rune"`
	EndRune   int    `json:"end_rune"`
	Message   string `json:"message"`
}

// MsgTerminal carries interactive PTY terminal streaming bytes.
type MsgTerminal struct {
	SessionID string `json:"session_id"`
	Data      []byte `json:"data"`
}

// MsgHeartbeat monitors peer connection liveness and latency.
type MsgHeartbeat struct {
	Timestamp int64 `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Binary Framing & Codec
// ---------------------------------------------------------------------------

// EncodePacket serializes a typed message into a framed binary packet:
// [2 bytes Magic 'TP'][1 byte MsgType][4 bytes Payload Length BE][Payload...]
func EncodePacket(msgType byte, payloadObj interface{}) ([]byte, error) {
	data, err := json.Marshal(payloadObj)
	if err != nil {
		return nil, fmt.Errorf("marshal payload error: %w", err)
	}

	length := uint32(len(data))
	frame := make([]byte, 7+length)
	frame[0] = ProtocolMagic[0]
	frame[1] = ProtocolMagic[1]
	frame[2] = msgType
	binary.BigEndian.PutUint32(frame[3:7], length)
	copy(frame[7:], data)

	return frame, nil
}

// DecodePacket extracts the message type and decodes the payload into targetObj.
func DecodePacket(frame []byte, targetObj interface{}) (byte, error) {
	if len(frame) < 7 {
		return 0, errors.New("frame too short for header")
	}
	if frame[0] != ProtocolMagic[0] || frame[1] != ProtocolMagic[1] {
		return 0, errors.New("invalid protocol magic")
	}

	msgType := frame[2]
	length := binary.BigEndian.Uint32(frame[3:7])

	if uint32(len(frame)-7) < length {
		return 0, fmt.Errorf("truncated frame: expected %d payload bytes, got %d", length, len(frame)-7)
	}

	payload := frame[7 : 7+length]
	if targetObj != nil {
		if err := json.Unmarshal(payload, targetObj); err != nil {
			return msgType, fmt.Errorf("unmarshal payload error: %w", err)
		}
	}

	return msgType, nil
}

// ---------------------------------------------------------------------------
// Coordinate Conversions (Rune <-> Byte <-> LSP UTF-16)
// ---------------------------------------------------------------------------

// RuneOffsetToByteOffset converts a 0-based rune index to a UTF-8 byte offset.
func RuneOffsetToByteOffset(text string, runeOffset int) int {
	if runeOffset <= 0 {
		return 0
	}
	rIdx := 0
	for bIdx := range text {
		if rIdx == runeOffset {
			return bIdx
		}
		rIdx++
	}
	return len(text)
}

// ByteOffsetToRuneOffset converts a 0-based UTF-8 byte offset to a rune index.
func ByteOffsetToRuneOffset(text string, byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	if byteOffset >= len(text) {
		return utf8.RuneCountInString(text)
	}
	rIdx := 0
	for bIdx := range text {
		if bIdx >= byteOffset {
			return rIdx
		}
		rIdx++
	}
	return rIdx
}

// RuneOffsetToLSPPosition converts a 0-based rune offset to LSP 0-based (line, characterUTF16).
// In LSP, character is counted in UTF-16 code units (surrogate pairs count as 2).
func RuneOffsetToLSPPosition(text string, runeOffset int) (line, charUTF16 int) {
	if runeOffset <= 0 {
		return 0, 0
	}
	curRune := 0
	curLine := 0
	curCharUTF16 := 0

	for _, r := range text {
		if curRune == runeOffset {
			return curLine, curCharUTF16
		}
		if r == '\n' {
			curLine++
			curCharUTF16 = 0
		} else {
			if r > 0xFFFF {
				curCharUTF16 += 2
			} else {
				curCharUTF16++
			}
		}
		curRune++
	}
	return curLine, curCharUTF16
}

// LSPPositionToRuneOffset converts LSP 0-based (line, characterUTF16) to 0-based rune offset.
func LSPPositionToRuneOffset(text string, targetLine, targetCharUTF16 int) int {
	if targetLine < 0 || targetCharUTF16 < 0 {
		return 0
	}
	curRune := 0
	curLine := 0
	curCharUTF16 := 0

	for _, r := range text {
		if curLine == targetLine && curCharUTF16 >= targetCharUTF16 {
			return curRune
		}
		if r == '\n' {
			if curLine == targetLine {
				return curRune
			}
			curLine++
			curCharUTF16 = 0
		} else {
			if r > 0xFFFF {
				curCharUTF16 += 2
			} else {
				curCharUTF16++
			}
		}
		curRune++
	}
	return curRune
}

// ---------------------------------------------------------------------------
// Operational Transformation & Host Sequencer
// ---------------------------------------------------------------------------

// TransformOperation transforms clientOp against concurrentOp that occurred at the same base revision.
func TransformOperation(clientOp MsgOpEdit, concurrentOp MsgOpEdit) MsgOpEdit {
	res := clientOp

	// First handle concurrent deletion
	if concurrentOp.DeleteCount > 0 {
		delStart := concurrentOp.PosRune
		delEnd := concurrentOp.PosRune + concurrentOp.DeleteCount

		if res.PosRune >= delEnd {
			// client is strictly after deleted region: shift backwards
			res.PosRune -= concurrentOp.DeleteCount
		} else if res.PosRune > delStart {
			// client position is inside deleted region: clamp to start of deletion
			res.PosRune = delStart
		}

		// Also adjust res.DeleteCount if client has a deletion
		if res.DeleteCount > 0 {
			resStart := clientOp.PosRune
			resEnd := clientOp.PosRune + clientOp.DeleteCount

			// Find overlap between [delStart, delEnd) and [resStart, resEnd)
			overlapStart := max(delStart, resStart)
			overlapEnd := min(delEnd, resEnd)
			if overlapStart < overlapEnd {
				overlapLen := overlapEnd - overlapStart
				res.DeleteCount -= overlapLen
				if res.DeleteCount < 0 {
					res.DeleteCount = 0
				}
			}
		}
	}

	// Then handle concurrent insertion
	insLen := utf8.RuneCountInString(concurrentOp.InsertText)
	if insLen > 0 {
		if concurrentOp.PosRune < res.PosRune {
			res.PosRune += insLen
		} else if concurrentOp.PosRune == res.PosRune {
			// Tie-breaker: lower peer ID stays ahead, or host (PeerID 0) priority
			if clientOp.PeerID > concurrentOp.PeerID {
				res.PosRune += insLen
			}
		} else if res.DeleteCount > 0 && concurrentOp.PosRune < res.PosRune+res.DeleteCount {
			// Insertion occurred inside client's deletion span: expand delete count to cover target region
			res.DeleteCount += insLen
		}
	}

	return res
}

// ApplyOpToText applies a MsgOpEdit directly to a Unicode string.
func ApplyOpToText(content string, op MsgOpEdit) string {
	runes := []rune(content)
	total := len(runes)

	pos := op.PosRune
	if pos < 0 {
		pos = 0
	}
	if pos > total {
		pos = total
	}

	del := op.DeleteCount
	if del < 0 {
		del = 0
	}
	if pos+del > total {
		del = total - pos
	}

	insRunes := []rune(op.InsertText)

	res := make([]rune, 0, total-del+len(insRunes))
	res = append(res, runes[:pos]...)
	res = append(res, insRunes...)
	res = append(res, runes[pos+del:]...)

	return string(res)
}

// HostSequencer coordinates master revision numbering and OT reconciliation.
type HostSequencer struct {
	mu       sync.Mutex
	revision uint64
	history  []MsgOpEdit
}

// NewHostSequencer initializes a host OT sequencer at base revision 0.
func NewHostSequencer() *HostSequencer {
	return &HostSequencer{
		revision: 0,
		history:  make([]MsgOpEdit, 0, 64),
	}
}

// Revision returns the current host master revision.
func (hs *HostSequencer) Revision() uint64 {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	return hs.revision
}

// SubmitOp reconciles a client edit against concurrent edits, increments revision, and records it.
func (hs *HostSequencer) SubmitOp(clientOp MsgOpEdit) (MsgOpEdit, uint64, error) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	transformed := clientOp

	// Reconcile clientOp against all operations applied after clientOp.Revision
	for _, pastOp := range hs.history {
		if pastOp.Revision > clientOp.Revision {
			transformed = TransformOperation(transformed, pastOp)
		}
	}

	hs.revision++
	transformed.Revision = hs.revision
	hs.history = append(hs.history, transformed)

	return transformed, hs.revision, nil
}
