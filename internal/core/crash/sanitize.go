package crash

import (
	"bytes"
	"os"
	"regexp"
	"strings"
)

var (
	bearerRegex     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9_\-\.~+/=]{8,}`)
	githubPatRegex  = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,255}\b`)
	privateKeyRegex = regexp.MustCompile(`-----BEGIN [A-Z0-9 _-]+ PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 _-]+ PRIVATE KEY-----`)
	secretKvRegex   = regexp.MustCompile(`(?i)\b(password|passwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token)\s*([:=])\s*["']?([^"' \t\r\n,;]+)["']?`)
	localIPRegex    = regexp.MustCompile(`\b(127\.0\.0\.1|0\.0\.0\.0|192\.168\.\d{1,3}\.\d{1,3}|10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2[0-9]|3[0-1])\.\d{1,3}\.\d{1,3})\b`)
)

// SanitizeLogData masks sensitive identifiers, tokens, passwords, private keys,
// local IP addresses and user home directory paths from byte slices.
func SanitizeLogData(rawLogs []byte) []byte {
	if len(rawLogs) == 0 {
		return rawLogs
	}

	result := rawLogs

	// 1. Mask private keys
	result = privateKeyRegex.ReplaceAll(result, []byte("[PRIVATE_KEY_REDACTED]"))

	// 2. Mask GitHub personal access tokens
	result = githubPatRegex.ReplaceAll(result, []byte("[GITHUB_TOKEN_REDACTED]"))

	// 3. Mask Bearer authorization headers
	result = bearerRegex.ReplaceAll(result, []byte("Bearer [REDACTED]"))

	// 4. Mask key-value secrets (password=xyz, api_key: abc)
	result = secretKvRegex.ReplaceAll(result, []byte("$1$2[REDACTED]"))

	// 5. Mask local IPs
	result = localIPRegex.ReplaceAll(result, []byte("[LOCAL_IP]"))

	// 6. Mask user home directory paths
	result = sanitizeHomePaths(result)

	return result
}

func sanitizeHomePaths(data []byte) []byte {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		// Clean both backslash and forward slash forms
		cleanHomeBack := home
		cleanHomeFwd := strings.ReplaceAll(home, `\`, `/`)

		// Replace backslash version
		data = bytes.ReplaceAll(data, []byte(cleanHomeBack), []byte("~"))
		// Replace forward slash version
		data = bytes.ReplaceAll(data, []byte(cleanHomeFwd), []byte("~"))

		// Case-insensitive replacement on Windows if needed
		data = replaceCaseInsensitive(data, cleanHomeBack, "~")
		data = replaceCaseInsensitive(data, cleanHomeFwd, "~")
	}

	userProfile := os.Getenv("USERPROFILE")
	if userProfile != "" && userProfile != home {
		cleanProfileBack := userProfile
		cleanProfileFwd := strings.ReplaceAll(userProfile, `\`, `/`)
		data = bytes.ReplaceAll(data, []byte(cleanProfileBack), []byte("~"))
		data = bytes.ReplaceAll(data, []byte(cleanProfileFwd), []byte("~"))
		data = replaceCaseInsensitive(data, cleanProfileBack, "~")
		data = replaceCaseInsensitive(data, cleanProfileFwd, "~")
	}

	return data
}

func replaceCaseInsensitive(data []byte, target, replacement string) []byte {
	if target == "" {
		return data
	}
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(target))
	if err != nil {
		return data
	}
	return re.ReplaceAll(data, []byte(replacement))
}
