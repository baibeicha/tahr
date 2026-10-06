package devtools

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// Base64Encode encodes raw bytes to a standard Base64 string with padding.
func Base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// Base64EncodeString encodes a string to a standard Base64 string with padding.
func Base64EncodeString(s string) string {
	return Base64Encode([]byte(s))
}

// Base64Decode decodes a standard Base64 string (with or without padding) to bytes.
func Base64Decode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []byte{}, nil
	}
	// Try standard decoding with padding
	data, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return data, nil
	}
	// Try standard decoding without padding
	if rawData, rawErr := base64.RawStdEncoding.DecodeString(s); rawErr == nil {
		return rawData, nil
	}
	// Normalize padding
	padded := s
	switch len(padded) % 4 {
	case 2:
		padded += "=="
	case 3:
		padded += "="
	}
	return base64.StdEncoding.DecodeString(padded)
}

// Base64DecodeString decodes a standard Base64 string to a string.
func Base64DecodeString(s string) (string, error) {
	b, err := Base64Decode(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Base64URLEncode encodes raw bytes to a URL-safe Base64 string with padding.
func Base64URLEncode(data []byte) string {
	return base64.URLEncoding.EncodeToString(data)
}

// Base64RawURLEncode encodes raw bytes to an unpadded URL-safe Base64 string.
func Base64RawURLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// Base64URLEncodeString encodes a string to a URL-safe Base64 string with padding.
func Base64URLEncodeString(s string) string {
	return Base64URLEncode([]byte(s))
}

// Base64RawURLEncodeString encodes a string to an unpadded URL-safe Base64 string.
func Base64RawURLEncodeString(s string) string {
	return Base64RawURLEncode([]byte(s))
}

// Base64URLDecode decodes a URL-safe Base64 string (with or without padding) to bytes.
func Base64URLDecode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []byte{}, nil
	}
	// Try unpadded URL-safe
	if data, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	// Try padded URL-safe
	if data, err := base64.URLEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	// Try unpadded Std as fallback
	if data, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	// Try padding normalization and character substitution
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

// Base64URLDecodeString decodes a URL-safe Base64 string to a string.
func Base64URLDecodeString(s string) (string, error) {
	b, err := Base64URLDecode(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// HexDump generates a canonical hex dump of data similar to `hexdump -C`.
func HexDump(data []byte) string {
	return hex.Dump(data)
}

// HexDumpString generates a canonical hex dump of a string.
func HexDumpString(s string) string {
	return HexDump([]byte(s))
}

// HexEncode encodes bytes to a lowercase hex string.
func HexEncode(data []byte) string {
	return hex.EncodeToString(data)
}

// HexEncodeString encodes a string to a lowercase hex string.
func HexEncodeString(s string) string {
	return HexEncode([]byte(s))
}

// HexDecode decodes a hex string to bytes, ignoring 0x prefix, spaces, colons, and newlines.
func HexDecode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", "")
	return hex.DecodeString(s)
}

// HexDecodeString decodes a hex string to string.
func HexDecodeString(s string) (string, error) {
	b, err := HexDecode(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
