package p2p

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// SignalingTier represents the communication layer used for SDP exchange.
type SignalingTier int

const (
	SignalingTierLocalLAN SignalingTier = 1 // mDNS Multicast (0-30 ms)
	SignalingTierNostr    SignalingTier = 2 // Ephemeral Nostr WebSocket relay (150-300 ms)
	SignalingTierDHT      SignalingTier = 3 // BitTorrent DHT BEP 44 (+2.5s fallback)
)

// CascadeConfig defines timing and parameters for the Zero-Server discovery cascade.
type CascadeConfig struct {
	DHTFallbackDelay time.Duration
	TurnServer       string
	NostrRelays      []string
}

// DefaultCascadeConfig returns standard timing and public relays.
func DefaultCascadeConfig() CascadeConfig {
	return CascadeConfig{
		DHTFallbackDelay: 2500 * time.Millisecond, // 2.5 seconds fallback for DHT
		NostrRelays: []string{
			"wss://relay.damus.io",
			"wss://nos.lol",
			"wss://relay.nostr.band",
		},
	}
}

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
// Tier 1 (LAN mDNS) and Tier 2 (Nostr WebSocket) start simultaneously immediately at t=0.
// Tier 3 (BitTorrent DHT) starts automatically after 2.5 seconds if not yet resolved.
type SignalingCoordinator struct {
	mu          sync.RWMutex
	sessionCode string
	crypto      *SessionCrypto
	config      CascadeConfig
	activeTier  SignalingTier
	activeTiers map[SignalingTier]bool
	isResolved  bool
	dhtTimer    *time.Timer
	cancelFunc  context.CancelFunc

	OnTierActivated func(tier SignalingTier)
	OnTierResolved  func(tier SignalingTier)
	OnExchange      func(ex SignalingExchange)
}

// NewSignalingCoordinator initializes the cascade coordinator with default 2.5s DHT delay.
func NewSignalingCoordinator(turnServer string) *SignalingCoordinator {
	cfg := DefaultCascadeConfig()
	cfg.TurnServer = turnServer
	return NewSignalingCoordinatorWithConfig(cfg)
}

// NewSignalingCoordinatorWithConfig initializes coordinator with custom config.
func NewSignalingCoordinatorWithConfig(cfg CascadeConfig) *SignalingCoordinator {
	if cfg.DHTFallbackDelay <= 0 {
		cfg.DHTFallbackDelay = 2500 * time.Millisecond
	}
	return &SignalingCoordinator{
		config:      cfg,
		activeTier:  SignalingTierLocalLAN,
		activeTiers: make(map[SignalingTier]bool),
	}
}

// StartCascade starts simultaneous LAN + Nostr discovery at t=0, and schedules DHT fallback at t=2.5s.
func (sc *SignalingCoordinator) StartCascade(ctx context.Context, sessionCode string, isHost bool) {
	sc.mu.Lock()
	if sc.cancelFunc != nil {
		sc.cancelFunc()
	}
	if sc.dhtTimer != nil {
		sc.dhtTimer.Stop()
	}

	cascadeCtx, cancel := context.WithCancel(ctx)
	sc.cancelFunc = cancel
	sc.sessionCode = sessionCode
	sc.crypto = DeriveSessionCrypto(sessionCode)
	sc.isResolved = false
	sc.activeTiers = make(map[SignalingTier]bool)

	// Step 1: Start LAN (mDNS 0-30ms) AND Nostr (WebSocket 150-300ms) simultaneously at t=0
	sc.activeTiers[SignalingTierLocalLAN] = true
	sc.activeTiers[SignalingTierNostr] = true

	onActivated := sc.OnTierActivated
	delay := sc.config.DHTFallbackDelay

	// Step 2: Schedule BitTorrent DHT fallback after 2.5 seconds
	sc.dhtTimer = time.AfterFunc(delay, func() {
		sc.mu.Lock()
		if sc.isResolved {
			sc.mu.Unlock()
			return
		}
		sc.activeTiers[SignalingTierDHT] = true
		cb := sc.OnTierActivated
		sc.mu.Unlock()

		if cb != nil {
			cb(SignalingTierDHT)
		}
	})
	sc.mu.Unlock()

	if onActivated != nil {
		onActivated(SignalingTierLocalLAN)
		onActivated(SignalingTierNostr)
	}

	_ = cascadeCtx
}

// ResolveTier marks a winning tier as connected, cancelling DHT fallback if pending.
func (sc *SignalingCoordinator) ResolveTier(tier SignalingTier) {
	sc.mu.Lock()
	sc.isResolved = true
	sc.activeTier = tier
	if sc.dhtTimer != nil {
		sc.dhtTimer.Stop()
		sc.dhtTimer = nil
	}
	cb := sc.OnTierResolved
	sc.mu.Unlock()

	if cb != nil {
		cb(tier)
	}
}

// ActiveTier returns the currently active/resolved discovery mechanism.
func (sc *SignalingCoordinator) ActiveTier() SignalingTier {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.activeTier
}

// IsTierActive reports whether a specific tier is currently running in the cascade.
func (sc *SignalingCoordinator) IsTierActive(tier SignalingTier) bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.activeTiers[tier]
}

// IsResolved reports whether connection has already been established by any tier.
func (sc *SignalingCoordinator) IsResolved() bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.isResolved
}

// Stop terminates all active cascade timers and background workers.
func (sc *SignalingCoordinator) Stop() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.cancelFunc != nil {
		sc.cancelFunc()
		sc.cancelFunc = nil
	}
	if sc.dhtTimer != nil {
		sc.dhtTimer.Stop()
		sc.dhtTimer = nil
	}
	sc.activeTiers = make(map[SignalingTier]bool)
	sc.isResolved = false
}

// VerifyPayloadHMAC checks payload integrity.
func (sc *SignalingCoordinator) VerifyPayloadHMAC(key, data, expectedMAC []byte) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hmac.Equal(mac.Sum(nil), expectedMAC)
}
