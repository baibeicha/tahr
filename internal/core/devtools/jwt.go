package devtools

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// JWTInfo represents the parsed details of a JSON Web Token.
type JWTInfo struct {
	Raw          string         `json:"raw"`
	Algorithm    string         `json:"algorithm"`
	Header       map[string]any `json:"header"`
	Payload      map[string]any `json:"payload"`
	HeaderJSON   string         `json:"header_json"`
	PayloadJSON  string         `json:"payload_json"`
	SignatureHex string         `json:"signature_hex"`
	SignatureRaw []byte         `json:"-"`
	IssuedAt     *time.Time     `json:"issued_at,omitempty"`
	ExpiresAt    *time.Time     `json:"expires_at,omitempty"`
	NotBefore    *time.Time     `json:"not_before,omitempty"`
	IsExpired    bool           `json:"is_expired"`
	ExpiresIn    time.Duration  `json:"expires_in"`
	ExpiryHuman  string         `json:"expiry_human"`
	ValidFormat  bool           `json:"valid_format"`
	Error        string         `json:"error,omitempty"`
}

// ParseJWT parses a JWT string against time.Now().
func ParseJWT(token string) (*JWTInfo, error) {
	return ParseJWTAt(token, time.Now())
}

// ParseJWTAt parses a JWT string relative to a given reference time.
func ParseJWTAt(token string, now time.Time) (*JWTInfo, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("empty JWT token")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return &JWTInfo{
			Raw:         token,
			ValidFormat: false,
			Error:       fmt.Sprintf("invalid JWT format: expected 3 segments separated by dots, got %d", len(parts)),
		}, fmt.Errorf("invalid JWT format: expected 3 segments, got %d", len(parts))
	}

	info := &JWTInfo{
		Raw:         token,
		ValidFormat: true,
	}

	// 1. Decode Header
	headerBytes, err := decodeBase64URLPart(parts[0])
	if err != nil {
		info.Error = fmt.Sprintf("failed to decode header base64: %v", err)
		return info, err
	}
	info.Header = make(map[string]any)
	if err := json.Unmarshal(headerBytes, &info.Header); err != nil {
		info.Error = fmt.Sprintf("invalid header JSON: %v", err)
		return info, err
	}
	info.HeaderJSON = formatJSON(headerBytes)

	if alg, ok := info.Header["alg"].(string); ok {
		info.Algorithm = alg
	} else {
		info.Algorithm = "none"
	}

	// 2. Decode Payload
	payloadBytes, err := decodeBase64URLPart(parts[1])
	if err != nil {
		info.Error = fmt.Sprintf("failed to decode payload base64: %v", err)
		return info, err
	}
	info.Payload = make(map[string]any)
	if err := json.Unmarshal(payloadBytes, &info.Payload); err != nil {
		info.Error = fmt.Sprintf("invalid payload JSON: %v", err)
		return info, err
	}
	info.PayloadJSON = formatJSON(payloadBytes)

	// 3. Decode Signature
	sigBytes, err := decodeBase64URLPart(parts[2])
	if err == nil {
		info.SignatureRaw = sigBytes
		info.SignatureHex = hex.EncodeToString(sigBytes)
	} else {
		info.SignatureHex = hex.EncodeToString([]byte(parts[2]))
	}

	// 4. Claims: exp, iat, nbf
	if expVal, ok := getNumericClaim(info.Payload, "exp"); ok {
		expTime := time.Unix(expVal, 0).UTC()
		info.ExpiresAt = &expTime

		if now.After(expTime) {
			info.IsExpired = true
			diff := now.Sub(expTime)
			info.ExpiresIn = -diff
			info.ExpiryHuman = fmt.Sprintf("Expired %s ago", formatDuration(diff))
		} else {
			info.IsExpired = false
			diff := expTime.Sub(now)
			info.ExpiresIn = diff
			info.ExpiryHuman = fmt.Sprintf("Expires in %s", formatDuration(diff))
		}
	} else {
		info.ExpiryHuman = "No expiration (claim 'exp' not found)"
	}

	if iatVal, ok := getNumericClaim(info.Payload, "iat"); ok {
		iatTime := time.Unix(iatVal, 0).UTC()
		info.IssuedAt = &iatTime
	}

	if nbfVal, ok := getNumericClaim(info.Payload, "nbf"); ok {
		nbfTime := time.Unix(nbfVal, 0).UTC()
		info.NotBefore = &nbfTime
	}

	return info, nil
}

// decodeBase64URLPart decodes a base64url encoded segment (with or without padding).
func decodeBase64URLPart(s string) ([]byte, error) {
	// Try RawURLEncoding (no padding)
	if data, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	// Try URLEncoding (with padding)
	if data, err := base64.URLEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	// Try RawStdEncoding as fallback
	if data, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	// Try StdEncoding with padding normalization
	padded := s
	switch len(padded) % 4 {
	case 2:
		padded += "=="
	case 3:
		padded += "="
	}
	padded = strings.ReplaceAll(padded, "-", "+")
	padded = strings.ReplaceAll(padded, "_", "/")
	return base64.StdEncoding.DecodeString(padded)
}

// formatJSON formats raw JSON bytes with 2-space indentation.
func formatJSON(data []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return string(data)
	}
	return buf.String()
}

// getNumericClaim extracts an int64 timestamp claim from payload map.
func getNumericClaim(m map[string]any, key string) (int64, bool) {
	v, exists := m[key]
	if !exists {
		return 0, false
	}
	switch num := v.(type) {
	case float64:
		return int64(num), true
	case int64:
		return num, true
	case int:
		return int64(num), true
	case json.Number:
		if n, err := num.Int64(); err == nil {
			return n, true
		}
	}
	return 0, false
}

// formatDuration converts a duration into human friendly units (e.g. 2d 5h, 3h 12m, 45s).
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	sec := int(d.Seconds())
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	min := sec / 60
	if min < 60 {
		remSec := sec % 60
		if remSec > 0 {
			return fmt.Sprintf("%dm %ds", min, remSec)
		}
		return fmt.Sprintf("%dm", min)
	}
	hrs := min / 60
	remMin := min % 60
	if hrs < 24 {
		if remMin > 0 {
			return fmt.Sprintf("%dh %dm", hrs, remMin)
		}
		return fmt.Sprintf("%dh", hrs)
	}
	days := hrs / 24
	remHrs := hrs % 24
	if remHrs > 0 {
		return fmt.Sprintf("%dd %dh", days, remHrs)
	}
	return fmt.Sprintf("%dd", days)
}
