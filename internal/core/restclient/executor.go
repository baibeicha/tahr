package restclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

// Response captures the complete result of executing an HTTP request.
type Response struct {
	StatusCode    int                 `json:"statusCode"`
	StatusText    string              `json:"statusText"`
	Proto         string              `json:"proto"`
	Headers       map[string][]string `json:"headers"`
	Body          []byte              `json:"-"`
	BodyString    string              `json:"bodyString"`
	PrettyBody    string              `json:"prettyBody"`
	ContentLength int64               `json:"contentLength"`
	Duration      time.Duration       `json:"duration"`
	DurationMs    int64               `json:"durationMs"`
	ContentType   string              `json:"contentType"`
	Error         string              `json:"error,omitempty"`
	Timestamp     time.Time           `json:"timestamp"`
	Request       *Request            `json:"request,omitempty"`
	RawRequest    string              `json:"rawRequest,omitempty"`
}

// StatusPill returns human-readable status text e.g. "200 OK" or "404 Not Found".
func (r *Response) StatusPill() string {
	if r == nil {
		return "READY"
	}
	if r.StatusText != "" {
		return r.StatusText
	}
	if r.StatusCode > 0 {
		txt := http.StatusText(r.StatusCode)
		if txt != "" {
			return fmt.Sprintf("%d %s", r.StatusCode, txt)
		}
		return fmt.Sprintf("%d", r.StatusCode)
	}
	if r.Error != "" {
		return "ERROR"
	}
	return "READY"
}

// IsSuccess returns true for 2xx responses.
func (r *Response) IsSuccess() bool {
	return r != nil && r.StatusCode >= 200 && r.StatusCode < 300
}

// IsClientError returns true for 4xx responses.
func (r *Response) IsClientError() bool {
	return r != nil && r.StatusCode >= 400 && r.StatusCode < 500
}

// IsServerError returns true for 5xx responses or transport errors.
func (r *Response) IsServerError() bool {
	return r != nil && (r.StatusCode >= 500 && r.StatusCode < 600 || (r.StatusCode == 0 && r.Error != ""))
}

// FormatSize formats ContentLength into human-readable e.g. "1.2 KB", "45 B".
func (r *Response) FormatSize() string {
	if r == nil {
		return "0 B"
	}
	size := r.ContentLength
	if size <= 0 {
		size = int64(len(r.Body))
	}
	return FormatBytes(size)
}

// FormatDuration returns e.g. "45 ms" or "1.24 s".
func (r *Response) FormatDuration() string {
	if r == nil {
		return "0 ms"
	}
	if r.Duration < time.Second {
		return fmt.Sprintf("%d ms", r.DurationMs)
	}
	return fmt.Sprintf("%.2f s", float64(r.Duration.Milliseconds())/1000.0)
}

// FormatBytes formats a byte count into clean human-readable units.
func FormatBytes(b int64) string {
	if b < 0 {
		return "0 B"
	}
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		kb := float64(b) / 1024.0
		return fmt.Sprintf("%.1f KB", kb)
	}
	if b < 1024*1024*1024 {
		mb := float64(b) / (1024.0 * 1024.0)
		return fmt.Sprintf("%.1f MB", mb)
	}
	gb := float64(b) / (1024.0 * 1024.0 * 1024.0)
	return fmt.Sprintf("%.1f GB", gb)
}

// FormatJSON attempts to indent JSON data with 2 spaces. If invalid, returns original string.
func FormatJSON(data []byte) string {
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err == nil {
		return out.String()
	}
	return string(data)
}

// FormatXML attempts to pretty-print XML with 2 spaces indentation.
func FormatXML(data []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	encoder.Indent("", "  ")
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return string(data)
		}
		if err := encoder.EncodeToken(token); err != nil {
			return string(data)
		}
	}
	if err := encoder.Flush(); err != nil {
		return string(data)
	}
	return out.String()
}

// AutoFormatBody detects JSON or XML and formats appropriately.
func AutoFormatBody(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}
	ct := strings.ToLower(contentType)
	trimmed := bytes.TrimSpace(body)

	if strings.Contains(ct, "json") || (len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')) {
		return FormatJSON(trimmed)
	}
	if strings.Contains(ct, "xml") || (len(trimmed) > 0 && trimmed[0] == '<') {
		return FormatXML(trimmed)
	}
	return string(body)
}

// RawRequestString reconstructs the standard wire HTTP request text.
func RawRequestString(req *Request) string {
	if req == nil {
		return ""
	}
	var sb strings.Builder
	method := req.Method
	if method == "" {
		method = "GET"
	}
	proto := req.Proto
	if proto == "" {
		proto = "HTTP/1.1"
	}
	sb.WriteString(fmt.Sprintf("%s %s %s\r\n", method, req.URL, proto))
	for k, v := range req.Headers {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	if req.Body != "" {
		if _, ok := req.Headers["Content-Length"]; !ok {
			sb.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(req.Body)))
		}
		sb.WriteString("\r\n")
		sb.WriteString(req.Body)
	} else {
		sb.WriteString("\r\n")
	}
	return sb.String()
}

// Executor coordinates HTTP execution using net/http.Client.
type Executor struct {
	Client *http.Client
}

// NewExecutor creates a new client executor with configurable timeout.
func NewExecutor(timeout ...time.Duration) *Executor {
	to := 30 * time.Second
	if len(timeout) > 0 && timeout[0] > 0 {
		to = timeout[0]
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		Proxy:           http.ProxyFromEnvironment,
	}
	return &Executor{
		Client: &http.Client{
			Transport: tr,
			Timeout:   to,
		},
	}
}

// ExecuteRequest executes a Request synchronously.
func (e *Executor) ExecuteRequest(req Request) (*Response, error) {
	return e.Execute(context.Background(), req)
}

// Execute executes a Request with context cancellation and measures timing.
func (e *Executor) Execute(ctx context.Context, req Request) (*Response, error) {
	targetURL := strings.TrimSpace(req.URL)
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "http://" + targetURL
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}

	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		now := time.Now()
		return &Response{
			StatusCode: 0,
			StatusText: "ERROR",
			Error:      err.Error(),
			Request:    &req,
			RawRequest: RawRequestString(&req),
			Timestamp:  now,
		}, err
	}

	for k, v := range req.Headers {
		if strings.EqualFold(k, "Host") {
			httpReq.Host = v
		} else {
			httpReq.Header.Set(k, v)
		}
	}

	// Capture outgoing raw request text
	var rawReq string
	if dump, dumpErr := httputil.DumpRequestOut(httpReq, true); dumpErr == nil {
		rawReq = string(dump)
	} else {
		rawReq = RawRequestString(&req)
	}

	start := time.Now()
	resp, err := e.Client.Do(httpReq)
	duration := time.Since(start)
	durationMs := duration.Milliseconds()

	if err != nil {
		return &Response{
			StatusCode: 0,
			StatusText: "ERROR",
			Duration:   duration,
			DurationMs: durationMs,
			Error:      err.Error(),
			Request:    &req,
			RawRequest: rawReq,
			Timestamp:  time.Now(),
		}, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	totalDuration := time.Since(start)
	totalDurationMs := totalDuration.Milliseconds()

	respHeaders := make(map[string][]string)
	for k, v := range resp.Header {
		respHeaders[k] = v
	}

	contentType := resp.Header.Get("Content-Type")
	pretty := AutoFormatBody(bodyBytes, contentType)

	contentLength := resp.ContentLength
	if contentLength < 0 {
		contentLength = int64(len(bodyBytes))
	}

	return &Response{
		StatusCode:    resp.StatusCode,
		StatusText:    resp.Status,
		Proto:         resp.Proto,
		Headers:       respHeaders,
		Body:          bodyBytes,
		BodyString:    string(bodyBytes),
		PrettyBody:    pretty,
		ContentLength: contentLength,
		Duration:      totalDuration,
		DurationMs:    totalDurationMs,
		ContentType:   contentType,
		Request:       &req,
		RawRequest:    rawReq,
		Timestamp:     time.Now(),
	}, nil
}

// DefaultExecutor is a package-level client with 30s timeout.
var DefaultExecutor = NewExecutor(30 * time.Second)

// Execute executes a request using the default executor.
func Execute(req Request) (*Response, error) {
	return DefaultExecutor.ExecuteRequest(req)
}
