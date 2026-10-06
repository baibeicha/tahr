package restclient

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Request represents a parsed HTTP request from a .http or .rest file.
type Request struct {
	Name        string            // Optional name from `### Name`
	Method      string            // HTTP method (GET, POST, PUT, DELETE, etc.)
	URL         string            // Target URL with variables substituted
	Proto       string            // Protocol version, e.g. "HTTP/1.1"
	Headers     map[string]string // Key-value headers map
	Body        string            // Request body with variables substituted
	StartLine   int               // 1-based start line in source document
	EndLine     int               // 1-based end line in source document
	RawText     string            // Raw block text before/after parsing
}

// Document represents a complete parsed .http/.rest file.
type Document struct {
	Variables map[string]string
	Requests  []Request
}

// varDefRegex matches file-level variable definitions: `@name = value` or `@name=value`.
var varDefRegex = regexp.MustCompile(`^@([a-zA-Z0-9_\-\.]+)\s*=\s*(.*)$`)

// varPlaceholderRegex matches placeholder variables: `{{name}}` or `{{ name }}`.
var varPlaceholderRegex = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_\-\.\$]+)\s*\}\}`)

// Parse parses a .http or .rest file content into a slice of Requests.
// Multiple requests are split on `###`.
// File-level variables `@baseUrl = http://...` and placeholders `{{var}}`,
// `{{$guid}}`, and `{{$timestamp}}` are extracted and substituted.
func Parse(content string, env ...map[string]string) ([]Request, error) {
	doc, err := ParseDocument(content, env...)
	if err != nil {
		return nil, err
	}
	return doc.Requests, nil
}

// ParseDocument parses a .http or .rest file and returns both extracted variables and requests.
func ParseDocument(content string, env ...map[string]string) (*Document, error) {
	lines := strings.Split(content, "\n")
	vars := make(map[string]string)

	// Combine external environment variables first
	for _, envMap := range env {
		for k, v := range envMap {
			vars[k] = v
		}
	}

	// 1. First pass: extract file-level variables `@name = value`
	// Variables can be referenced by subsequently defined variables.
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := varDefRegex.FindStringSubmatch(trimmed); m != nil {
			varName := strings.TrimSpace(m[1])
			varVal := strings.TrimSpace(m[2])
			// Resolve any existing variables inside the variable definition itself
			varVal = SubstituteVariables(varVal, vars)
			vars[varName] = varVal
		}
	}

	// Resolve any nested inter-variable dependencies
	for iter := 0; iter < 5; iter++ {
		changed := false
		for k, v := range vars {
			res := SubstituteVariables(v, vars)
			if res != v {
				vars[k] = res
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// 2. Second pass: split content into request blocks on `###`
	type rawBlock struct {
		name      string
		lines     []string
		startLine int // 1-based
		endLine   int // 1-based
	}

	var blocks []rawBlock
	var currentBlock *rawBlock

	for lineIdx, rawLine := range lines {
		lineNum := lineIdx + 1
		trimmed := strings.TrimSpace(rawLine)

		if strings.HasPrefix(trimmed, "###") {
			// Save completed block
			if currentBlock != nil && len(currentBlock.lines) > 0 {
				currentBlock.endLine = lineNum - 1
				blocks = append(blocks, *currentBlock)
			}

			// Extract optional name from delimiter: `### Request Name`
			reqName := strings.TrimSpace(strings.TrimPrefix(trimmed, "###"))
			currentBlock = &rawBlock{
				name:      reqName,
				lines:     make([]string, 0),
				startLine: lineNum,
			}
			continue
		}

		if currentBlock == nil {
			// Lines before the first `###` may be variable definitions or the first request
			if strings.HasPrefix(trimmed, "@") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") || trimmed == "" {
				// Header comment or variable definition before any request
				continue
			}
			// It's a request without a leading `###`
			currentBlock = &rawBlock{
				lines:     make([]string, 0),
				startLine: lineNum,
			}
		}

		currentBlock.lines = append(currentBlock.lines, rawLine)
	}

	if currentBlock != nil && len(currentBlock.lines) > 0 {
		currentBlock.endLine = len(lines)
		blocks = append(blocks, *currentBlock)
	}

	// 3. Third pass: parse each block into a Request
	var requests []Request
	for _, blk := range blocks {
		req, ok := parseRequestBlock(blk.name, blk.lines, blk.startLine, blk.endLine, vars)
		if ok {
			requests = append(requests, req)
		}
	}

	return &Document{
		Variables: vars,
		Requests:  requests,
	}, nil
}

// parseRequestBlock converts a block of lines into a structured Request.
func parseRequestBlock(name string, blockLines []string, startLine, endLine int, vars map[string]string) (Request, bool) {
	// Check for # @name or //@name comment if name wasn't set by delimiter
	for _, l := range blockLines {
		trimmed := strings.TrimSpace(l)
		if (strings.HasPrefix(trimmed, "# @name") || strings.HasPrefix(trimmed, "//@name")) && name == "" {
			tag := "# @name"
			if strings.HasPrefix(trimmed, "//@name") {
				tag = "//@name"
			}
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, tag))
			val = strings.TrimPrefix(val, "=")
			name = strings.TrimSpace(val)
		}
	}

	// Find the request line (skipping comments and blank lines)
	reqLineIdx := -1
	for i, l := range blockLines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "@") {
			continue
		}
		reqLineIdx = i
		break
	}

	if reqLineIdx == -1 {
		return Request{}, false
	}

	reqLine := strings.TrimSpace(blockLines[reqLineIdx])
	reqLine = SubstituteVariables(reqLine, vars)

	method, rawURL, proto := parseRequestLine(reqLine)
	if rawURL == "" {
		return Request{}, false
	}

	// Next lines until empty line are headers
	headers := make(map[string]string)
	bodyStartIdx := len(blockLines)

	for i := reqLineIdx + 1; i < len(blockLines); i++ {
		line := blockLines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			// Empty line marks end of headers and start of body
			bodyStartIdx = i + 1
			break
		}

		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			// Skip comment line inside headers
			continue
		}

		colonIdx := strings.Index(trimmed, ":")
		if colonIdx > 0 {
			hKey := strings.TrimSpace(trimmed[:colonIdx])
			hVal := strings.TrimSpace(trimmed[colonIdx+1:])
			hVal = SubstituteVariables(hVal, vars)
			if existing, ok := headers[hKey]; ok && !strings.EqualFold(hKey, "Set-Cookie") {
				headers[hKey] = existing + ", " + hVal
			} else {
				headers[hKey] = hVal
			}
		}
	}

	// Everything after the blank line is body
	var bodyBuilder strings.Builder
	if bodyStartIdx < len(blockLines) {
		for i := bodyStartIdx; i < len(blockLines); i++ {
			if i > bodyStartIdx {
				bodyBuilder.WriteString("\n")
			}
			bodyBuilder.WriteString(blockLines[i])
		}
	}

	rawBody := bodyBuilder.String()
	body := strings.TrimRight(SubstituteVariables(rawBody, vars), "\r\n")

	// Adjust start line if delimiter wasn't at startLine
	reqStart := startLine
	rawText := strings.Join(blockLines, "\n")

	return Request{
		Name:      name,
		Method:    method,
		URL:       rawURL,
		Proto:     proto,
		Headers:   headers,
		Body:      body,
		StartLine: reqStart,
		EndLine:   endLine,
		RawText:   rawText,
	}, true
}

// parseRequestLine extracts method, target URL, and optional HTTP version.
// e.g. "POST https://api.example.com/v1/users HTTP/1.1" -> ("POST", "https://api.example.com/v1/users", "HTTP/1.1")
// e.g. "https://api.example.com/v1/users" -> ("GET", "https://api.example.com/v1/users", "")
func parseRequestLine(line string) (method, urlStr, proto string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", "", ""
	}

	first := fields[0]
	firstUpper := strings.ToUpper(first)

	isStandardMethod := false
	switch firstUpper {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		isStandardMethod = true
	}

	if isStandardMethod {
		method = firstUpper
		if len(fields) == 2 {
			urlStr = fields[1]
		} else if len(fields) > 2 {
			last := fields[len(fields)-1]
			if strings.HasPrefix(strings.ToUpper(last), "HTTP/") {
				proto = last
				urlStr = strings.Join(fields[1:len(fields)-1], " ")
			} else {
				urlStr = strings.Join(fields[1:], " ")
			}
		}
	} else {
		// Method might be omitted; check if first token looks like a URL or path
		if strings.HasPrefix(first, "http://") || strings.HasPrefix(first, "https://") || strings.HasPrefix(first, "/") {
			method = "GET"
			last := fields[len(fields)-1]
			if len(fields) > 1 && strings.HasPrefix(strings.ToUpper(last), "HTTP/") {
				proto = last
				urlStr = strings.Join(fields[:len(fields)-1], " ")
			} else {
				urlStr = strings.Join(fields, " ")
			}
		} else {
			// Custom or non-standard HTTP method (e.g. QUERY, GRAPHQL)
			method = firstUpper
			if len(fields) == 2 {
				urlStr = fields[1]
			} else if len(fields) > 2 {
				last := fields[len(fields)-1]
				if strings.HasPrefix(strings.ToUpper(last), "HTTP/") {
					proto = last
					urlStr = strings.Join(fields[1:len(fields)-1], " ")
				} else {
					urlStr = strings.Join(fields[1:], " ")
				}
			}
		}
	}

	return method, urlStr, proto
}

// SubstituteVariables replaces variable tokens in text:
// - `{{name}}`: mapped from vars
// - `{{$guid}}`: generated RFC 4122 v4 UUID
// - `{{$timestamp}}`: current Unix timestamp in seconds
func SubstituteVariables(text string, vars map[string]string) string {
	if text == "" {
		return ""
	}

	prev := text
	for pass := 0; pass < 5; pass++ {
		cur := varPlaceholderRegex.ReplaceAllStringFunc(prev, func(match string) string {
			name := strings.TrimSpace(match[2 : len(match)-2])

			switch name {
			case "$guid", "guid":
				return NewUUID()
			case "$timestamp", "timestamp":
				return strconv.FormatInt(time.Now().Unix(), 10)
			default:
				if vars != nil {
					if val, exists := vars[name]; exists {
						return val
					}
				}
				return match // Keep unresolved placeholder
			}
		})
		if cur == prev {
			break
		}
		prev = cur
	}
	return prev
}

// FindRequestAtLine locates the Request encompassing the given 1-based line number.
func FindRequestAtLine(requests []Request, line int) *Request {
	if len(requests) == 0 {
		return nil
	}

	// 1. Direct match within [StartLine, EndLine]
	for i := range requests {
		if line >= requests[i].StartLine && line <= requests[i].EndLine {
			return &requests[i]
		}
	}

	// 2. Tolerance: if cursor is above first request or between requests, find closest
	if line < requests[0].StartLine {
		return &requests[0]
	}

	for i := 0; i < len(requests)-1; i++ {
		if line > requests[i].EndLine && line < requests[i+1].StartLine {
			return &requests[i+1]
		}
	}

	// If beyond last request, return last
	if line > requests[len(requests)-1].EndLine {
		return &requests[len(requests)-1]
	}

	return nil
}

// NewUUID generates a cryptographically random RFC 4122 v4 UUID.
func NewUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10xx
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ToHTTPHeader converts map[string]string to net/http.Header.
func ToHTTPHeader(h map[string]string) http.Header {
	header := make(http.Header)
	for k, v := range h {
		header.Set(k, v)
	}
	return header
}
