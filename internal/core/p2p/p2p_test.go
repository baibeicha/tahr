package p2p

import (
	"testing"
	"time"
)

// MockTransport simulates a duplex pipe in memory.
type MockTransport struct {
	target *MockTransport
	onRecv func(data []byte)
}

func (m *MockTransport) Send(frame []byte) error {
	if m.target != nil && m.target.onRecv != nil {
		m.target.onRecv(frame)
	}
	return nil
}

func (m *MockTransport) Close() error {
	return nil
}

func TestSessionCryptoAndCode(t *testing.T) {
	code := GenerateSessionCode()
	if len(code) == 0 {
		t.Fatal("expected non-empty session code")
	}

	crypto := DeriveSessionCrypto(code)
	if crypto.SessionCode != code {
		t.Fatalf("crypto session code mismatch: %s != %s", crypto.SessionCode, code)
	}
	if len(crypto.TopicHash) != 32 {
		t.Fatalf("expected 32-char topic hash, got: %d", len(crypto.TopicHash))
	}
	if len(crypto.SecretKey) != 32 {
		t.Fatalf("expected 32-byte secret key, got: %d", len(crypto.SecretKey))
	}
}

func TestBinaryPacketFraming(t *testing.T) {
	req := MsgJoinRequest{
		PeerID:    42,
		Nickname:  "alice",
		PublicKey: "pubkey-123",
		Role:      "editor",
	}

	frame, err := EncodePacket(MsgTypeJoinRequest, req)
	if err != nil {
		t.Fatalf("failed to encode packet: %v", err)
	}

	var decoded MsgJoinRequest
	msgType, err := DecodePacket(frame, &decoded)
	if err != nil {
		t.Fatalf("failed to decode packet: %v", err)
	}

	if msgType != MsgTypeJoinRequest {
		t.Fatalf("expected msgType %d, got %d", MsgTypeJoinRequest, msgType)
	}
	if decoded.PeerID != 42 || decoded.Nickname != "alice" || decoded.Role != "editor" {
		t.Fatalf("decoded request mismatch: %+v", decoded)
	}

	// Test invalid magic
	badFrame := append([]byte(nil), frame...)
	badFrame[0] = 0xFF
	if _, err := DecodePacket(badFrame, &decoded); err == nil {
		t.Fatal("expected error on invalid magic byte")
	}

	// Test truncated frame
	if _, err := DecodePacket(frame[:4], &decoded); err == nil {
		t.Fatal("expected error on truncated header")
	}
}

func TestUnicodeCoordinateNormalization(t *testing.T) {
	// Text with ASCII, Cyrillic (2 bytes per rune), and Emojis (4 bytes per rune, 2 UTF-16 code units)
	// "Hi " (3 bytes, 3 runes, 3 utf-16)
	// "мир" (6 bytes, 3 runes, 3 utf-16)
	// "\n" (1 byte, 1 rune)
	// "🚀" (4 bytes, 1 rune, 2 utf-16)
	// "go" (2 bytes, 2 runes, 2 utf-16)
	content := "Hi мир\n🚀go"

	// Rune offsets:
	// 'H'=0, 'i'=1, ' '=2, 'м'=3, 'и'=4, 'р'=5, '\n'=6
	// '🚀'=7, 'g'=8, 'o'=9
	totalRunes := 10

	// 1. Rune <-> Byte conversions
	byteOffset7 := RuneOffsetToByteOffset(content, 7) // Start of '🚀'
	// "Hi " (3) + "мир" (6) + "\n" (1) = 10 bytes
	if byteOffset7 != 10 {
		t.Fatalf("expected byte offset 10 for rune 7, got %d", byteOffset7)
	}

	runeOffsetBack := ByteOffsetToRuneOffset(content, byteOffset7)
	if runeOffsetBack != 7 {
		t.Fatalf("expected rune offset 7 from byte offset %d, got %d", byteOffset7, runeOffsetBack)
	}

	byteOffset8 := RuneOffsetToByteOffset(content, 8) // Start of 'g' (after 4-byte emoji)
	if byteOffset8 != 14 {
		t.Fatalf("expected byte offset 14 for rune 8, got %d", byteOffset8)
	}

	// 2. Rune <-> LSP (line, utf-16 character)
	// Line 0: "Hi мир" -> rune 0..5
	l, c := RuneOffsetToLSPPosition(content, 3) // 'м'
	if l != 0 || c != 3 {
		t.Fatalf("expected line 0, char 3, got line %d, char %d", l, c)
	}

	// Line 1: '🚀' is at line 1, char 0
	l, c = RuneOffsetToLSPPosition(content, 7)
	if l != 1 || c != 0 {
		t.Fatalf("expected line 1, char 0 for emoji, got line %d, char %d", l, c)
	}

	// 'g' is after '🚀', which is 2 UTF-16 code units!
	l, c = RuneOffsetToLSPPosition(content, 8)
	if l != 1 || c != 2 {
		t.Fatalf("expected line 1, char 2 for 'g' after emoji, got line %d, char %d", l, c)
	}

	// Convert back from LSP (line 1, char 2) to rune offset (should be 8)
	r := LSPPositionToRuneOffset(content, 1, 2)
	if r != 8 {
		t.Fatalf("expected rune 8 from LSP line 1, char 2, got %d", r)
	}

	// Verify all runes round-trip
	for i := 0; i < totalRunes; i++ {
		b := RuneOffsetToByteOffset(content, i)
		rTest := ByteOffsetToRuneOffset(content, b)
		if rTest != i {
			t.Fatalf("rune round-trip failed at %d: got %d", i, rTest)
		}
	}
}

func TestOperationalTransformation(t *testing.T) {
	initialText := "hello world"

	// Op 1: User 1 inserts "big " at pos 6 ("hello big world")
	op1 := MsgOpEdit{
		Revision:    0,
		PeerID:      1,
		PosRune:     6,
		DeleteCount: 0,
		InsertText:  "big ",
	}

	// Op 2: User 2 inserts "!" at pos 11 ("hello world!")
	op2 := MsgOpEdit{
		Revision:    0,
		PeerID:      2,
		PosRune:     11,
		DeleteCount: 0,
		InsertText:  "!",
	}

	// Transform op2 against op1: op2's position should shift by len("big ") = 4 -> pos 15
	transOp2 := TransformOperation(op2, op1)
	if transOp2.PosRune != 15 {
		t.Fatalf("expected transformed pos 15, got %d", transOp2.PosRune)
	}

	// Apply op1 then transOp2
	text := ApplyOpToText(initialText, op1)
	if text != "hello big world" {
		t.Fatalf("unexpected text after op1: %q", text)
	}

	text = ApplyOpToText(text, transOp2)
	if text != "hello big world!" {
		t.Fatalf("unexpected text after transOp2: %q", text)
	}
}

func TestHostSequencerReconciliation(t *testing.T) {
	seq := NewHostSequencer()

	// Initial text
	docText := "func Main() {}"

	// Client 1 sends edit based on rev 0
	op1 := MsgOpEdit{
		Revision:   0,
		PeerID:     101,
		PosRune:    5,
		InsertText: "my",
	}

	res1, rev1, err := seq.SubmitOp(op1)
	if err != nil || rev1 != 1 {
		t.Fatalf("failed submit op1: rev=%d, err=%v", rev1, err)
	}
	docText = ApplyOpToText(docText, res1)
	// "func myMain() {}"

	// Client 2 concurrently sends edit based on rev 0 (replacing "Main" with "Run")
	// PosRune: 5, DeleteCount: 4, InsertText: "Run"
	op2 := MsgOpEdit{
		Revision:    0,
		PeerID:      102,
		PosRune:     5,
		DeleteCount: 4,
		InsertText:  "Run",
	}

	res2, rev2, err := seq.SubmitOp(op2)
	if err != nil || rev2 != 2 {
		t.Fatalf("failed submit op2: rev=%d, err=%v", rev2, err)
	}

	// Since res1 was inserted at 5 with len 2, op2's PosRune should be adjusted past "my"
	if res2.PosRune != 7 {
		t.Fatalf("expected res2.PosRune 7, got %d", res2.PosRune)
	}

	docText = ApplyOpToText(docText, res2)
	if docText != "func myRun() {}" {
		t.Fatalf("expected 'func myRun() {}', got %q", docText)
	}
}

func TestCollaborationSessionLifecycle(t *testing.T) {
	host := NewHostSession("host-user", "")
	guest := NewGuestSession(host.SessionCode, "guest-user")

	var hostToGuest MockTransport
	var guestToHost MockTransport

	hostToGuest.target = &guestToHost
	guestToHost.target = &hostToGuest

	hostToGuest.onRecv = func(frame []byte) {
		_ = host.HandleIncomingPacket(guest.LocalPeerID, frame)
	}
	guestToHost.onRecv = func(frame []byte) {
		_ = guest.HandleIncomingPacket(0, frame)
	}

	host.RegisterPeerTransport(guest.LocalPeerID, &hostToGuest)
	guest.RegisterPeerTransport(0, &guestToHost)

	// Step 1: Guest sends JoinRequest
	req := MsgJoinRequest{
		PeerID:   guest.LocalPeerID,
		Nickname: guest.LocalNickname,
		Role:     "editor",
	}
	reqFrame, _ := EncodePacket(MsgTypeJoinRequest, req)
	_ = guestToHost.Send(reqFrame)

	if len(host.PendingJoins) != 1 {
		t.Fatalf("expected 1 pending join, got %d", len(host.PendingJoins))
	}

	// Step 2: Host accepts guest
	err := host.AcceptGuest(guest.LocalPeerID, "editor", true)
	if err != nil {
		t.Fatalf("host accept guest error: %v", err)
	}

	if guest.ICEState != ICEStateConnected {
		t.Fatalf("expected guest ICE state connected, got %s", guest.ICEState)
	}
	if host.PeerCount() != 1 {
		t.Fatalf("expected host peer count 1, got %d", host.PeerCount())
	}

	// Step 3: Guest sends OpEdit
	var receivedByHost MsgOpEdit
	host.OnOpEdit = func(op MsgOpEdit) {
		receivedByHost = op
	}

	editOp := MsgOpEdit{
		DocURI:     "file:///main.go",
		Revision:   0,
		PeerID:     guest.LocalPeerID,
		PosRune:    0,
		InsertText: "package main\n",
	}
	editFrame, _ := EncodePacket(MsgTypeOpEdit, editOp)
	_ = guestToHost.Send(editFrame)

	time.Sleep(10 * time.Millisecond)
	if receivedByHost.InsertText != "package main\n" {
		t.Fatalf("host did not receive edit: %+v", receivedByHost)
	}

	// Step 4: Presence broadcast
	var presenceReceived MsgPresence
	guest.OnPresence = func(p MsgPresence) {
		presenceReceived = p
	}

	_ = host.BroadcastPresence(42, 40, 42, "file:///main.go")
	if presenceReceived.CursorRune != 42 {
		t.Fatalf("guest did not receive presence: %+v", presenceReceived)
	}

	// Step 5: Close session
	_ = host.Close()
	if host.ICEState != ICEStateClosed {
		t.Fatalf("expected host ICEStateClosed, got %s", host.ICEState)
	}
}

func TestViewerPermissionRejection(t *testing.T) {
	host := NewHostSession("host-user", "")
	guest := NewGuestSession(host.SessionCode, "guest-viewer")

	var hostToGuest MockTransport
	var guestToHost MockTransport

	hostToGuest.target = &guestToHost
	guestToHost.target = &hostToGuest

	hostToGuest.onRecv = func(frame []byte) {
		_ = host.HandleIncomingPacket(guest.LocalPeerID, frame)
	}

	host.RegisterPeerTransport(guest.LocalPeerID, &hostToGuest)
	guest.RegisterPeerTransport(0, &guestToHost)

	// Host registers guest as "viewer"
	host.PendingJoins = append(host.PendingJoins, MsgJoinRequest{
		PeerID:   guest.LocalPeerID,
		Nickname: "guest-viewer",
		Role:     "viewer",
	})
	_ = host.AcceptGuest(guest.LocalPeerID, "viewer", false)

	// Guest attempts to edit
	editOp := MsgOpEdit{
		DocURI:     "file:///main.go",
		Revision:   0,
		PeerID:     guest.LocalPeerID,
		PosRune:    0,
		InsertText: "evil edit",
	}
	frame, _ := EncodePacket(MsgTypeOpEdit, editOp)
	err := host.HandleIncomingPacket(guest.LocalPeerID, frame)
	if err == nil {
		t.Fatal("expected error when viewer tries to edit, got nil")
	}
}

func TestOverlappingDeletions(t *testing.T) {
	// Original text: "0123456789"
	// Op1: deletes [2..6) -> pos 2, count 4 -> text becomes "016789"
	// Op2 concurrently: deletes [4..8) -> pos 4, count 4
	op1 := MsgOpEdit{
		Revision:    0,
		PeerID:      1,
		PosRune:     2,
		DeleteCount: 4,
	}
	op2 := MsgOpEdit{
		Revision:    0,
		PeerID:      2,
		PosRune:     4,
		DeleteCount: 4,
	}

	// Transform op2 against op1:
	// op1 deleted [2, 6).
	// op2 originally wanted [4, 8).
	// [4, 6) was already deleted by op1.
	// op2 start clamped to 2, remaining count is 2 (for [6, 8)).
	transOp2 := TransformOperation(op2, op1)
	if transOp2.PosRune != 2 {
		t.Fatalf("expected transOp2.PosRune 2, got %d", transOp2.PosRune)
	}
	if transOp2.DeleteCount != 2 {
		t.Fatalf("expected transOp2.DeleteCount 2, got %d", transOp2.DeleteCount)
	}

	text := "0123456789"
	text = ApplyOpToText(text, op1)
	if text != "016789" {
		t.Fatalf("expected '016789', got %q", text)
	}
	text = ApplyOpToText(text, transOp2)
	if text != "0189" {
		t.Fatalf("expected '0189', got %q", text)
	}
}

func BenchmarkOperationalTransformation(b *testing.B) {
	opA := MsgOpEdit{Revision: 1, PeerID: 10, PosRune: 100, DeleteCount: 5, InsertText: "var x = 10;"}
	opB := MsgOpEdit{Revision: 1, PeerID: 20, PosRune: 50, DeleteCount: 2, InsertText: "const y = 20;"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TransformOperation(opA, opB)
	}
}

func BenchmarkBinaryFraming(b *testing.B) {
	op := MsgOpEdit{
		DocURI:      "file:///src/main.go",
		Revision:    142,
		PeerID:      12,
		PosRune:     450,
		DeleteCount: 0,
		InsertText:  "fmt.Println(\"p2p ok\")",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame, _ := EncodePacket(MsgTypeOpEdit, op)
		var decoded MsgOpEdit
		_, _ = DecodePacket(frame, &decoded)
	}
}

