package devtools

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
var uuidNoHyphenRegex = regexp.MustCompile(`^[0-9a-fA-F]{12}4[0-9a-fA-F]{3}[89abAB][0-9a-fA-F]{15}$`)

// GenerateUUID generates RFC 4122 v4 UUIDs.
func GenerateUUID(count int, uppercase, hyphens bool) []string {
	if count < 1 {
		count = 1
	}
	if count > 100 {
		count = 100
	}

	res := make([]string, count)
	for i := 0; i < count; i++ {
		var b [16]byte
		_, _ = rand.Read(b[:])

		// RFC 4122 variant and version 4
		b[6] = (b[6] & 0x0f) | 0x40 // Version 4
		b[8] = (b[8] & 0x3f) | 0x80 // Variant 10xxxxxx

		var s string
		if hyphens {
			s = fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
				b[0], b[1], b[2], b[3],
				b[4], b[5],
				b[6], b[7],
				b[8], b[9],
				b[10], b[11], b[12], b[13], b[14], b[15])
		} else {
			s = fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x",
				b[0], b[1], b[2], b[3],
				b[4], b[5],
				b[6], b[7],
				b[8], b[9],
				b[10], b[11], b[12], b[13], b[14], b[15])
		}

		if uppercase {
			s = strings.ToUpper(s)
		}
		res[i] = s
	}
	return res
}

// IsValidUUID checks whether a string is a valid RFC 4122 v4 UUID.
func IsValidUUID(s string) bool {
	s = strings.TrimSpace(s)
	return uuidRegex.MatchString(s) || uuidNoHyphenRegex.MatchString(s)
}
