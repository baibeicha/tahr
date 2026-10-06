package devtools

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// --- JWT Tests ---

func TestJWTParse(t *testing.T) {
	// Sample valid JWT:
	// Header: {"alg":"HS256","typ":"JWT"}
	// Payload: {"sub":"1234567890","name":"John Doe","iat":1516239022,"exp":1700000000}
	header := `{"alg":"HS256","typ":"JWT"}`
	payload := `{"sub":"1234567890","name":"John Doe","iat":1516239022,"exp":1700000000}`
	hB64 := base64.RawURLEncoding.EncodeToString([]byte(header))
	pB64 := base64.RawURLEncoding.EncodeToString([]byte(payload))
	token := hB64 + "." + pB64 + ".SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

	// 1. Unexpired check (ref time before exp 1700000000 -> 2023-11-14T22:13:20Z)
	refTimeBefore := time.Unix(1650000000, 0)
	info, err := ParseJWTAt(token, refTimeBefore)
	if err != nil {
		t.Fatalf("unexpected error parsing JWT: %v", err)
	}
	if !info.ValidFormat {
		t.Errorf("expected ValidFormat true")
	}
	if info.Algorithm != "HS256" {
		t.Errorf("expected alg HS256, got %s", info.Algorithm)
	}
	if info.Payload["name"] != "John Doe" {
		t.Errorf("expected name 'John Doe', got %v", info.Payload["name"])
	}
	if info.IsExpired {
		t.Errorf("expected token not to be expired at %v", refTimeBefore)
	}
	if !strings.Contains(info.ExpiryHuman, "Expires in") {
		t.Errorf("expected ExpiryHuman to contain 'Expires in', got %q", info.ExpiryHuman)
	}
	if info.ExpiresAt == nil || info.ExpiresAt.Unix() != 1700000000 {
		t.Errorf("expected exp 1700000000, got %v", info.ExpiresAt)
	}
	if info.IssuedAt == nil || info.IssuedAt.Unix() != 1516239022 {
		t.Errorf("expected iat 1516239022, got %v", info.IssuedAt)
	}

	// 2. Expired check (ref time after exp)
	refTimeAfter := time.Unix(1750000000, 0)
	infoExpired, err := ParseJWTAt(token, refTimeAfter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !infoExpired.IsExpired {
		t.Errorf("expected token to be expired at %v", refTimeAfter)
	}
	if !strings.Contains(infoExpired.ExpiryHuman, "Expired") {
		t.Errorf("expected ExpiryHuman to contain 'Expired', got %q", infoExpired.ExpiryHuman)
	}

	// 3. Token without exp
	payloadNoExp := `{"sub":"123","iat":1516239022}`
	pNoExpB64 := base64.RawURLEncoding.EncodeToString([]byte(payloadNoExp))
	tokenNoExp := hB64 + "." + pNoExpB64 + ".sig"
	infoNoExp, err := ParseJWT(tokenNoExp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if infoNoExp.ExpiresAt != nil {
		t.Errorf("expected nil ExpiresAt, got %v", infoNoExp.ExpiresAt)
	}
	if !strings.Contains(infoNoExp.ExpiryHuman, "not found") {
		t.Errorf("expected notice that exp claim not found, got %q", infoNoExp.ExpiryHuman)
	}

	// 4. Invalid token formats
	if _, err := ParseJWT(""); err == nil {
		t.Errorf("expected error for empty token")
	}
	if _, err := ParseJWT("part1.part2"); err == nil {
		t.Errorf("expected error for 2-part token")
	}
	if _, err := ParseJWT("part1.part2.part3.part4"); err == nil {
		t.Errorf("expected error for 4-part token")
	}
	if _, err := ParseJWT("???invalid_b64???.payload.sig"); err == nil {
		t.Errorf("expected error for invalid base64 in header")
	}
	badHeader := base64.RawURLEncoding.EncodeToString([]byte(`not a json`))
	if _, err := ParseJWT(badHeader + "." + pB64 + ".sig"); err == nil {
		t.Errorf("expected error for invalid JSON in header")
	}
}

// --- UUID Tests ---

func TestUUID(t *testing.T) {
	// Generate with hyphens and uppercase
	uuids := GenerateUUID(5, true, true)
	if len(uuids) != 5 {
		t.Fatalf("expected 5 uuids, got %d", len(uuids))
	}
	for _, u := range uuids {
		if !IsValidUUID(u) {
			t.Errorf("expected %q to be valid UUID", u)
		}
		if u != strings.ToUpper(u) {
			t.Errorf("expected uppercase UUID, got %s", u)
		}
		if !strings.Contains(u, "-") {
			t.Errorf("expected hyphenated UUID, got %s", u)
		}
	}

	// Generate without hyphens and lowercase
	uuidsNoHyphen := GenerateUUID(3, false, false)
	if len(uuidsNoHyphen) != 3 {
		t.Fatalf("expected 3 uuids, got %d", len(uuidsNoHyphen))
	}
	for _, u := range uuidsNoHyphen {
		if !IsValidUUID(u) {
			t.Errorf("expected %q to be valid UUID without hyphens", u)
		}
		if strings.Contains(u, "-") {
			t.Errorf("expected unhyphenated UUID, got %s", u)
		}
		if u != strings.ToLower(u) {
			t.Errorf("expected lowercase UUID, got %s", u)
		}
	}

	// Boundary checks on count
	if list := GenerateUUID(0, false, false); len(list) != 1 {
		t.Errorf("expected count 0 to default to 1, got %d", len(list))
	}
	if list := GenerateUUID(200, false, false); len(list) != 100 {
		t.Errorf("expected count 200 to clamp to 100, got %d", len(list))
	}

	// IsValidUUID validation cases
	validV4 := "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	if !IsValidUUID(validV4) {
		t.Errorf("expected %s to be valid", validV4)
	}
	validV4NoHyphens := "f47ac10b58cc4372a5670e02b2c3d479"
	if !IsValidUUID(validV4NoHyphens) {
		t.Errorf("expected %s to be valid", validV4NoHyphens)
	}

	// Invalid test cases (wrong version, wrong variant, bad chars, length)
	invalidUUIDs := []string{
		"",
		"not-a-uuid",
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8", // UUID v1, not v4
		"f47ac10b-58cc-4372-c567-0e02b2c3d479", // Invalid variant ('c' instead of 8, 9, a, b)
		"f47ac10b-58cc-4372-a567-0e02b2c3d47",   // Too short
		"f47ac10b-58cc-4372-a567-0e02b2c3d4799", // Too long
	}
	for _, inv := range invalidUUIDs {
		if IsValidUUID(inv) {
			t.Errorf("expected %q to be recognized as invalid v4 UUID", inv)
		}
	}
}

// --- Base64 & Hex Tests ---

func TestBase64(t *testing.T) {
	// 1. Standard Base64 encode and decode
	testStrings := []string{
		"",
		"f",
		"fo",
		"foo",
		"foob",
		"fooba",
		"foobar",
		"Hello, World! @#$%^&*()_+",
		"Русский текст и UTF-8 эмодзи 🚀✨",
	}

	for _, str := range testStrings {
		enc := Base64EncodeString(str)
		dec, err := Base64DecodeString(enc)
		if err != nil {
			t.Fatalf("failed to decode Base64 for %q: %v", str, err)
		}
		if dec != str {
			t.Errorf("roundtrip mismatch for %q: got %q", str, dec)
		}
	}

	// 2. Decode without padding
	rawB64 := "YW55IGNhcm5hbCBwbGVhc3Vy" // "any carnal pleasur" without ==
	dec, err := Base64DecodeString(rawB64)
	if err != nil {
		t.Fatalf("failed to decode unpadded Base64: %v", err)
	}
	if dec != "any carnal pleasur" {
		t.Errorf("expected 'any carnal pleasur', got %q", dec)
	}

	// 3. URL-safe Base64
	// Bytes that yield '+' and '/' in standard base64: [0xfb, 0xff, 0xfe] -> "+//+" -> URL-safe "-__-"
	specialBytes := []byte{0xfb, 0xff, 0xfe}
	stdEnc := Base64Encode(specialBytes)
	urlEnc := Base64URLEncode(specialBytes)
	urlRawEnc := Base64RawURLEncode(specialBytes)

	if !strings.Contains(stdEnc, "+") && !strings.Contains(stdEnc, "/") {
		t.Errorf("expected standard base64 to contain + or /, got %s", stdEnc)
	}
	if strings.Contains(urlEnc, "+") || strings.Contains(urlEnc, "/") {
		t.Errorf("url-safe base64 must not contain + or /, got %s", urlEnc)
	}

	decURL, err := Base64URLDecode(urlEnc)
	if err != nil {
		t.Fatalf("failed to decode url base64: %v", err)
	}
	if string(decURL) != string(specialBytes) {
		t.Errorf("url-safe decode mismatch")
	}

	decRawURL, err := Base64URLDecode(urlRawEnc)
	if err != nil {
		t.Fatalf("failed to decode raw url base64: %v", err)
	}
	if string(decRawURL) != string(specialBytes) {
		t.Errorf("raw url-safe decode mismatch")
	}

	// URL-safe string helpers
	urlStrEnc := Base64URLEncodeString("test/data+value")
	urlStrDec, err := Base64URLDecodeString(urlStrEnc)
	if err != nil || urlStrDec != "test/data+value" {
		t.Errorf("URL string helpers mismatch: %v, got %q", err, urlStrDec)
	}
	rawUrlStrEnc := Base64RawURLEncodeString("test/data+value")
	rawUrlStrDec, err := Base64URLDecodeString(rawUrlStrEnc)
	if err != nil || rawUrlStrDec != "test/data+value" {
		t.Errorf("Raw URL string helpers mismatch: %v, got %q", err, rawUrlStrDec)
	}

	// 4. Hex Dump
	dump := HexDumpString("hello world\n")
	if !strings.Contains(dump, "00000000") || !strings.Contains(dump, "68 65 6c 6c 6f") {
		t.Errorf("unexpected hex dump output:\n%s", dump)
	}

	// 5. Hex Encode and Decode
	hexStr := HexEncodeString("Hello")
	if hexStr != "48656c6c6f" {
		t.Errorf("expected 48656c6c6f, got %s", hexStr)
	}

	// HexDecode with formatting (0x prefix, colons, spaces, uppercase)
	decodedHex, err := HexDecodeString("0x48:65 6C:6C 6F")
	if err != nil {
		t.Fatalf("HexDecodeString failed: %v", err)
	}
	if decodedHex != "Hello" {
		t.Errorf("expected 'Hello', got %q", decodedHex)
	}

	// Invalid hex test
	if _, err := HexDecode("zzzz"); err == nil {
		t.Errorf("expected error for invalid hex string")
	}
	if _, err := HexDecodeString("zzzz"); err == nil {
		t.Errorf("expected error for invalid HexDecodeString")
	}

	// Base64 decode error cases
	if _, err := Base64Decode("!!!not_base64!!!"); err == nil {
		t.Errorf("expected error for invalid Base64 string")
	}
	if _, err := Base64DecodeString("!!!not_base64!!!"); err == nil {
		t.Errorf("expected error for invalid Base64DecodeString")
	}
	if _, err := Base64URLDecode("!!!not_base64!!!"); err == nil {
		t.Errorf("expected error for invalid Base64URLDecode")
	}
	if _, err := Base64URLDecodeString("!!!not_base64!!!"); err == nil {
		t.Errorf("expected error for invalid Base64URLDecodeString")
	}
}

// --- Hash Tests ---

func TestHash(t *testing.T) {
	input := "The quick brown fox jumps over the lazy dog"
	empty := ""

	// MD5
	if h := MD5String(empty); h != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Errorf("expected empty MD5 d41d8cd98f00b204e9800998ecf8427e, got %s", h)
	}
	if h := MD5String(input); h != "9e107d9d372bb6826bd81d3542a419d6" {
		t.Errorf("MD5 mismatch for input: got %s", h)
	}

	// SHA-1
	if h := SHA1String(empty); h != "da39a3ee5e6b4b0d3255bfef95601890afd80709" {
		t.Errorf("expected empty SHA1 da39a3ee5e6b4b0d3255bfef95601890afd80709, got %s", h)
	}
	if h := SHA1String(input); h != "2fd4e1c67a2d28fced849ee1bb76e7391b93eb12" {
		t.Errorf("SHA1 mismatch for input: got %s", h)
	}

	// SHA-256
	if h := SHA256String(empty); h != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("expected empty SHA256 e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855, got %s", h)
	}
	if h := SHA256String(input); h != "d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592" {
		t.Errorf("SHA256 mismatch for input: got %s", h)
	}

	// SHA-512
	if h := SHA512String(empty); h != "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e" {
		t.Errorf("expected empty SHA512 cf83e135..., got %s", h)
	}
	if h := SHA512String(input); h != "07e547d9586f6a73f73fbac0435ed76951218fb7d0c8d788a309d785436bbb642e93a252a954f23912547d1e8a3b5ed6e1bfd7097821233fa0538f3db854fee6" {
		t.Errorf("SHA512 mismatch for input: got %s", h)
	}

	// HashAll
	all := HashAllString(input)
	if all.MD5 != MD5String(input) || all.SHA1 != SHA1String(input) ||
		all.SHA256 != SHA256String(input) || all.SHA512 != SHA512String(input) {
		t.Errorf("HashAll mismatch: %+v", all)
	}

	// ComputeHash generic function
	for _, alg := range []string{"md5", "MD5", "sha1", "sha-1", "SHA-256", "sha512", "sha384", "sha224"} {
		res, err := ComputeHashString(alg, input)
		if err != nil || res == "" {
			t.Errorf("ComputeHash failed for %s: %v", alg, err)
		}
	}
	if _, err := ComputeHashString("invalid-alg", input); err == nil {
		t.Errorf("expected error for unknown hash algorithm")
	}

	// HMAC
	key := []byte("secret-key")
	data := []byte("message")
	hmac256 := HMACSHA256(key, data)
	if hmac256 == "" {
		t.Errorf("expected non-empty HMAC SHA256")
	}
	computedHMAC, err := ComputeHMAC("sha256", key, data)
	if err != nil || computedHMAC != hmac256 {
		t.Errorf("ComputeHMAC mismatch: %v, got %s", err, computedHMAC)
	}
	hmacMD5 := HMACMD5(key, data)
	hmac1 := HMACSHA1(key, data)
	hmac512 := HMACSHA512(key, data)
	if hmacMD5 == "" || hmac1 == "" || hmac512 == "" {
		t.Errorf("expected non-empty HMAC digests")
	}
	hmacStr, err := ComputeHMACString("sha256", "secret-key", "message")
	if err != nil || hmacStr != hmac256 {
		t.Errorf("ComputeHMACString mismatch: %v, got %s", err, hmacStr)
	}
	if _, err := ComputeHMAC("unknown", key, data); err == nil {
		t.Errorf("expected error for invalid HMAC algorithm")
	}
	if _, err := ComputeHMACString("unknown", "key", "data"); err == nil {
		t.Errorf("expected error for invalid HMAC algorithm")
	}
}

// --- Timestamp Tests ---

func TestTimestamp(t *testing.T) {
	// Known epoch: 1700000000 = 2023-11-14T22:13:20Z
	sec := int64(1700000000)
	isoUTC := UnixToISO8601UTC(sec)
	if isoUTC != "2023-11-14T22:13:20Z" {
		t.Errorf("expected 2023-11-14T22:13:20Z, got %s", isoUTC)
	}

	isoLocal := UnixToISO8601Local(sec)
	if !strings.HasPrefix(isoLocal, "2023-11-1") {
		t.Errorf("unexpected local ISO: %s", isoLocal)
	}

	// Milliseconds: 1700000000500 = 2023-11-14T22:13:20.500Z
	milli := int64(1700000000500)
	milliUTC := UnixMilliToISO8601UTC(milli)
	if milliUTC != "2023-11-14T22:13:20.500Z" {
		t.Errorf("expected 2023-11-14T22:13:20.500Z, got %s", milliUTC)
	}

	milliLocal := UnixMilliToISO8601Local(milli)
	if !strings.Contains(milliLocal, ".500") {
		t.Errorf("expected local milli ISO to contain .500, got %s", milliLocal)
	}

	// Reverse parsing: ISO8601ToUnix and ISO8601ToUnixMilli
	parsedSec, err := ISO8601ToUnix("2023-11-14T22:13:20Z")
	if err != nil || parsedSec != sec {
		t.Errorf("ISO8601ToUnix failed: %v, got %d, expected %d", err, parsedSec, sec)
	}

	parsedMilli, err := ISO8601ToUnixMilli("2023-11-14T22:13:20.500Z")
	if err != nil || parsedMilli != milli {
		t.Errorf("ISO8601ToUnixMilli failed: %v, got %d, expected %d", err, parsedMilli, milli)
	}

	// ParseTimestamp with numeric string (seconds)
	infoSec, err := ParseTimestamp("1700000000")
	if err != nil {
		t.Fatalf("ParseTimestamp failed for seconds: %v", err)
	}
	if infoSec.UnixSec != sec {
		t.Errorf("expected UnixSec %d, got %d", sec, infoSec.UnixSec)
	}
	if infoSec.ISO8601UTC != "2023-11-14T22:13:20Z" {
		t.Errorf("expected ISO8601UTC 2023-11-14T22:13:20Z, got %s", infoSec.ISO8601UTC)
	}

	// ParseTimestamp with numeric string (milliseconds)
	infoMilli, err := ParseTimestamp("1700000000500")
	if err != nil {
		t.Fatalf("ParseTimestamp failed for millis: %v", err)
	}
	if infoMilli.UnixMilli != milli {
		t.Errorf("expected UnixMilli %d, got %d", milli, infoMilli.UnixMilli)
	}

	// ParseTimestamp with ISO 8601 string
	infoISO, err := ParseTimestamp("2023-11-14T22:13:20Z")
	if err != nil {
		t.Fatalf("ParseTimestamp failed for ISO: %v", err)
	}
	if infoISO.UnixSec != sec {
		t.Errorf("expected UnixSec %d, got %d", sec, infoISO.UnixSec)
	}

	// FromTimeAt and Relative formatting
	refTime := time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)

	// Past cases
	past10s := refTime.Add(-10 * time.Second)
	infoPast10s := FromTimeAt(past10s, refTime)
	if infoPast10s.Relative != "10 seconds ago" {
		t.Errorf("expected '10 seconds ago', got %q", infoPast10s.Relative)
	}

	past2h := refTime.Add(-2 * time.Hour)
	infoPast2h := FromTimeAt(past2h, refTime)
	if infoPast2h.Relative != "2 hours ago" {
		t.Errorf("expected '2 hours ago', got %q", infoPast2h.Relative)
	}

	past5d := refTime.Add(-5 * 24 * time.Hour)
	infoPast5d := FromTimeAt(past5d, refTime)
	if infoPast5d.Relative != "5 days ago" {
		t.Errorf("expected '5 days ago', got %q", infoPast5d.Relative)
	}

	// Future cases
	future30m := refTime.Add(30 * time.Minute)
	infoFuture30m := FromTimeAt(future30m, refTime)
	if infoFuture30m.Relative != "in 30 minutes" {
		t.Errorf("expected 'in 30 minutes', got %q", infoFuture30m.Relative)
	}

	// Just now
	infoJustNow := FromTimeAt(refTime, refTime)
	if infoJustNow.Relative != "just now" {
		t.Errorf("expected 'just now', got %q", infoJustNow.Relative)
	}

	// Format helpers
	if FormatISO8601UTC(refTime) != "2023-11-14T22:13:20Z" {
		t.Errorf("FormatISO8601UTC mismatch")
	}
	if FormatISO8601Local(refTime) == "" {
		t.Errorf("FormatISO8601Local empty")
	}

	// Convenience constructors
	infoFromNow := FromTime(time.Now())
	if infoFromNow == nil || infoFromNow.UnixSec == 0 {
		t.Errorf("FromTime failed")
	}
	infoFromSec := FromUnixSec(sec)
	if infoFromSec == nil || infoFromSec.UnixSec != sec {
		t.Errorf("FromUnixSec failed")
	}
	infoFromMilli := FromUnixMilli(milli)
	if infoFromMilli == nil || infoFromMilli.UnixMilli != milli {
		t.Errorf("FromUnixMilli failed")
	}

	// Additional relative time boundaries
	past1m := refTime.Add(-65 * time.Second)
	if rel := FromTimeAt(past1m, refTime).Relative; rel != "1 minute ago" {
		t.Errorf("expected '1 minute ago', got %q", rel)
	}

	past1h := refTime.Add(-65 * time.Minute)
	if rel := FromTimeAt(past1h, refTime).Relative; rel != "1 hour ago" {
		t.Errorf("expected '1 hour ago', got %q", rel)
	}

	pastYesterday := refTime.Add(-25 * time.Hour)
	if rel := FromTimeAt(pastYesterday, refTime).Relative; rel != "yesterday" {
		t.Errorf("expected 'yesterday', got %q", rel)
	}

	future1m := refTime.Add(65 * time.Second)
	if rel := FromTimeAt(future1m, refTime).Relative; rel != "in 1 minute" {
		t.Errorf("expected 'in 1 minute', got %q", rel)
	}

	future1h := refTime.Add(65 * time.Minute)
	if rel := FromTimeAt(future1h, refTime).Relative; rel != "in 1 hour" {
		t.Errorf("expected 'in 1 hour', got %q", rel)
	}

	futureTomorrow := refTime.Add(25 * time.Hour)
	if rel := FromTimeAt(futureTomorrow, refTime).Relative; rel != "tomorrow" {
		t.Errorf("expected 'tomorrow', got %q", rel)
	}

	futureDays := refTime.Add(72 * time.Hour)
	if rel := FromTimeAt(futureDays, refTime).Relative; rel != "in 3 days" {
		t.Errorf("expected 'in 3 days', got %q", rel)
	}

	// Microsecond and Nanosecond ParseTimestamp
	infoMicro, err := ParseTimestamp("1700000000123456")
	if err != nil || infoMicro.UnixSec != sec {
		t.Errorf("ParseTimestamp micro failed: %v", err)
	}
	infoNano, err := ParseTimestamp("1700000000123456789")
	if err != nil || infoNano.UnixSec != sec {
		t.Errorf("ParseTimestamp nano failed: %v", err)
	}

	// Error cases
	if _, err := ParseTimestamp(""); err == nil {
		t.Errorf("expected error for empty timestamp string")
	}
	if _, err := ParseISO8601("not-a-date"); err == nil {
		t.Errorf("expected error for unparseable date")
	}
	if _, err := ISO8601ToUnix("not-a-date"); err == nil {
		t.Errorf("expected error for invalid ISO8601ToUnix")
	}
	if _, err := ISO8601ToUnixMilli("not-a-date"); err == nil {
		t.Errorf("expected error for invalid ISO8601ToUnixMilli")
	}
}
