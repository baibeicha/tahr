package devtools

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"
)

// MD5 calculates the MD5 hex digest of data.
func MD5(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

// MD5String calculates the MD5 hex digest of the string s.
func MD5String(s string) string {
	return MD5([]byte(s))
}

// SHA1 calculates the SHA-1 hex digest of data.
func SHA1(data []byte) string {
	h := sha1.Sum(data)
	return hex.EncodeToString(h[:])
}

// SHA1String calculates the SHA-1 hex digest of the string s.
func SHA1String(s string) string {
	return SHA1([]byte(s))
}

// SHA256 calculates the SHA-256 hex digest of data.
func SHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// SHA256String calculates the SHA-256 hex digest of the string s.
func SHA256String(s string) string {
	return SHA256([]byte(s))
}

// SHA512 calculates the SHA-512 hex digest of data.
func SHA512(data []byte) string {
	h := sha512.Sum512(data)
	return hex.EncodeToString(h[:])
}

// SHA512String calculates the SHA-512 hex digest of the string s.
func SHA512String(s string) string {
	return SHA512([]byte(s))
}

// HashResult contains standard cryptographic digests computed for an input.
type HashResult struct {
	MD5    string `json:"md5"`
	SHA1   string `json:"sha1"`
	SHA256 string `json:"sha256"`
	SHA512 string `json:"sha512"`
}

// HashAll computes MD5, SHA-1, SHA-256, and SHA-512 digests for data.
func HashAll(data []byte) HashResult {
	return HashResult{
		MD5:    MD5(data),
		SHA1:   SHA1(data),
		SHA256: SHA256(data),
		SHA512: SHA512(data),
	}
}

// HashAllString computes MD5, SHA-1, SHA-256, and SHA-512 digests for a string.
func HashAllString(s string) HashResult {
	return HashAll([]byte(s))
}

// getHashFunc returns the hash constructor for an algorithm identifier.
func getHashFunc(algorithm string) (func() hash.Hash, error) {
	norm := strings.ToLower(strings.TrimSpace(algorithm))
	norm = strings.ReplaceAll(norm, "-", "")
	norm = strings.ReplaceAll(norm, "_", "")

	switch norm {
	case "md5":
		return md5.New, nil
	case "sha1":
		return sha1.New, nil
	case "sha256":
		return sha256.New, nil
	case "sha512":
		return sha512.New, nil
	case "sha384":
		return sha512.New384, nil
	case "sha224":
		return sha256.New224, nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm: %s", algorithm)
	}
}

// ComputeHash computes a hex digest for the specified algorithm and data.
func ComputeHash(algorithm string, data []byte) (string, error) {
	fn, err := getHashFunc(algorithm)
	if err != nil {
		return "", err
	}
	h := fn()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ComputeHashString computes a hex digest for the specified algorithm and string input.
func ComputeHashString(algorithm string, s string) (string, error) {
	return ComputeHash(algorithm, []byte(s))
}

// HMACMD5 calculates the HMAC-MD5 hex digest.
func HMACMD5(key, data []byte) string {
	mac := hmac.New(md5.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// HMACSHA1 calculates the HMAC-SHA1 hex digest.
func HMACSHA1(key, data []byte) string {
	mac := hmac.New(sha1.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// HMACSHA256 calculates the HMAC-SHA256 hex digest.
func HMACSHA256(key, data []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// HMACSHA512 calculates the HMAC-SHA512 hex digest.
func HMACSHA512(key, data []byte) string {
	mac := hmac.New(sha512.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// ComputeHMAC computes an HMAC hex digest for the given algorithm, key, and data.
func ComputeHMAC(algorithm string, key, data []byte) (string, error) {
	fn, err := getHashFunc(algorithm)
	if err != nil {
		return "", err
	}
	mac := hmac.New(fn, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// ComputeHMACString computes an HMAC hex digest for string key and data.
func ComputeHMACString(algorithm string, key, data string) (string, error) {
	return ComputeHMAC(algorithm, []byte(key), []byte(data))
}
