package p2p

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"
)

// SignalingTier represents the communication layer used for SDP exchange.
type SignalingTier int

const (
	SignalingTierLocalLAN SignalingTier = 1 // mDNS Multicast (0-30 ms)
	SignalingTierNostr    SignalingTier = 2 // Ephemeral Nostr WebSocket relay (150-300 ms)
	SignalingTierDHT      SignalingTier = 3 // BitTorrent DHT BEP 44 (+2.5s fallback)
)

// SessionCrypto holds derived cryptographic keys for a collaboration session code.
type SessionCrypto struct {
	SessionCode string
	TopicHash   string // Public topic hash for relay lookup
	SecretKey   []byte // 32-byte symmetric payload encryption key
}

// DeriveSessionCrypto derives deterministic lookup topic and encryption key from a session code.
func DeriveSessionCrypto(code string) *SessionCrypto {
	hKey := sha256.Sum256([]byte("tahr-collab-v1-key:" + code))
	hTopic := sha256.Sum256([]byte("tahr-collab-v1-topic:" + code))

	return &SessionCrypto{
		SessionCode: code,
		TopicHash:   hex.EncodeToString(hTopic[:16]), // 32-char hex topic
		SecretKey:   hKey[:],
	}
}

// GenerateSessionCode generates an ergonomic human-friendly session pairing code.
func GenerateSessionCode() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	nouns := []string{"ibex", "eagle", "falcon", "snow", "peak", "summit", "stone", "fjord"}
	noun := nouns[r.Intn(len(nouns))]
	num := 1000 + r.Intn(9000)
	return fmt.Sprintf("tahr-%s-%d", noun, num)
}

// SignalingExchange represents the SDP offer/answer payload exchanged between peers.
type SignalingExchange struct {
	SessionCode string        `json:"session_code"`
	PeerID      uint16        `json:"peer_id"`
	Role        string        `json:"role"` // "host" or "guest"
	SDP         string        `json:"sdp"`
	Candidates  []string      `json:"candidates"`
	Tier        SignalingTier `json:"tier"`
	Timestamp   int64         `json:"timestamp"`
}

// SignalingCoordinator manages the staggered multi-tier zero-server discovery cascade.
type SignalingCoordinator struct {
	activeTier SignalingTier
	turnServer string
}

// NewSignalingCoordinator initializes the cascade coordinator.
func NewSignalingCoordinator(turnServer string) *SignalingCoordinator {
	return &SignalingCoordinator{
		activeTier: SignalingTierLocalLAN,
		turnServer: turnServer,
	}
}

// ActiveTier returns the currently active discovery mechanism.
func (sc *SignalingCoordinator) ActiveTier() SignalingTier {
	return sc.activeTier
}

// VerifyPayloadHMAC checks payload integrity.
func (sc *SignalingCoordinator) VerifyPayloadHMAC(key, data, expectedMAC []byte) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hmac.Equal(mac.Sum(nil), expectedMAC)
}
