package restclient

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParser_SingleRequest(t *testing.T) {
	content := `GET https://httpbin.org/get HTTP/1.1
Accept: application/json
User-Agent: Tahr-REST/1.0
`
	reqs, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("Expected 1 request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Method != "GET" {
		t.Errorf("Expected GET, got %s", req.Method)
	}
	if req.URL != "https://httpbin.org/get" {
		t.Errorf("Expected URL https://httpbin.org/get, got %s", req.URL)
	}
	if req.Proto != "HTTP/1.1" {
		t.Errorf("Expected Proto HTTP/1.1, got %s", req.Proto)
	}
	if req.Headers["Accept"] != "application/json" {
		t.Errorf("Expected Accept header, got %s", req.Headers["Accept"])
	}
	if req.Headers["User-Agent"] != "Tahr-REST/1.0" {
		t.Errorf("Expected User-Agent header, got %s", req.Headers["User-Agent"])
	}
	if req.Body != "" {
		t.Errorf("Expected empty body, got %q", req.Body)
	}
}

func TestParser_MultipleRequestsWithDelimiters(t *testing.T) {
	content := `### Get Profile
GET https://api.example.com/profile HTTP/1.1
Authorization: Bearer token123

### Create Item
# @name createItem
POST https://api.example.com/items HTTP/1.1
Content-Type: application/json

{
  "name": "Widget",
  "qty": 10
}

### Delete Item
DELETE https://api.example.com/items/42
`
	doc, err := ParseDocument(content)
	if err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	if len(doc.Requests) != 3 {
		t.Fatalf("Expected 3 requests, got %d", len(doc.Requests))
	}

	r0 := doc.Requests[0]
	if r0.Name != "Get Profile" {
		t.Errorf("Expected name 'Get Profile', got %q", r0.Name)
	}
	if r0.Method != "GET" {
		t.Errorf("Expected GET, got %s", r0.Method)
	}

	r1 := doc.Requests[1]
	if r1.Name != "Create Item" {
		t.Errorf("Expected name 'Create Item', got %q", r1.Name)
	}
	if r1.Method != "POST" {
		t.Errorf("Expected POST, got %s", r1.Method)
	}
	if !strings.Contains(r1.Body, `"name": "Widget"`) {
		t.Errorf("Expected body to contain widget, got %q", r1.Body)
	}

	r2 := doc.Requests[2]
	if r2.Name != "Delete Item" {
		t.Errorf("Expected name 'Delete Item', got %q", r2.Name)
	}
	if r2.Method != "DELETE" {
		t.Errorf("Expected DELETE, got %s", r2.Method)
	}
	if r2.URL != "https://api.example.com/items/42" {
		t.Errorf("Expected URL https://api.example.com/items/42, got %s", r2.URL)
	}
}

func TestParser_VariablesAndSubstitutions(t *testing.T) {
	content := `@host = api.example.com
@port = 8443
@baseUrl = https://{{host}}:{{port}}
@authToken = secret-xyz-789

### Authenticated Request
POST {{baseUrl}}/v1/auth
Authorization: Bearer {{authToken}}
Content-Type: application/json

{
  "endpoint": "{{baseUrl}}",
  "client": "Tahr"
}
`
	reqs, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("Expected 1 request, got %d", len(reqs))
	}

	r := reqs[0]
	if r.URL != "https://api.example.com:8443/v1/auth" {
		t.Errorf("Unexpected URL after substitution: %s", r.URL)
	}
	if r.Headers["Authorization"] != "Bearer secret-xyz-789" {
		t.Errorf("Unexpected Authorization: %s", r.Headers["Authorization"])
	}
	if !strings.Contains(r.Body, `"https://api.example.com:8443"`) {
		t.Errorf("Variable not substituted in body: %s", r.Body)
	}
}

func TestParser_DynamicVariables(t *testing.T) {
	content := `POST https://example.com/telemetry
X-Trace-ID: {{$guid}}
X-Timestamp: {{$timestamp}}

{
  "uuid": "{{$guid}}",
  "ts": {{$timestamp}}
}
`
	reqs, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	r := reqs[0]

	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	traceID := r.Headers["X-Trace-ID"]
	if !uuidRegex.MatchString(traceID) {
		t.Errorf("Header X-Trace-ID is not a valid UUID v4: %q", traceID)
	}

	tsStr := r.Headers["X-Timestamp"]
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		t.Errorf("Header X-Timestamp is not integer: %q", tsStr)
	}
	now := time.Now().Unix()
	if ts < now-5 || ts > now+5 {
		t.Errorf("Timestamp %d is far from current time %d", ts, now)
	}

	if !strings.Contains(r.Body, `"uuid": "`) {
		t.Errorf("Expected uuid in body: %s", r.Body)
	}
}

func TestParser_FindRequestAtLine(t *testing.T) {
	content := `@baseUrl = https://api.example.com

### Request One
GET {{baseUrl}}/one
Accept: text/plain

### Request Two
POST {{baseUrl}}/two
Content-Type: application/json

{"hello":"world"}
`
	doc, err := ParseDocument(content)
	if err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	if len(doc.Requests) != 2 {
		t.Fatalf("Expected 2 requests, got %d", len(doc.Requests))
	}

	req1 := FindRequestAtLine(doc.Requests, 4)
	if req1 == nil || req1.Name != "Request One" {
		t.Errorf("Expected Request One at line 4, got %+v", req1)
	}

	req2 := FindRequestAtLine(doc.Requests, 9)
	if req2 == nil || req2.Name != "Request Two" {
		t.Errorf("Expected Request Two at line 9, got %+v", req2)
	}

	// Line 1 above first request should resolve to first request
	reqTop := FindRequestAtLine(doc.Requests, 1)
	if reqTop == nil || reqTop.Name != "Request One" {
		t.Errorf("Expected Request One at line 1, got %+v", reqTop)
	}
}

func TestExecutor_GetRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("X-Custom") != "TahrHeader" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Server", "TahrServer/1.0")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","message":"hello world"}`))
	}))
	defer server.Close()

	exec := NewExecutor(5 * time.Second)
	req := Request{
		Method: "GET",
		URL:    server.URL,
		Headers: map[string]string{
			"X-Custom": "TahrHeader",
		},
	}

	resp, err := exec.ExecuteRequest(req)
	if err != nil {
		t.Fatalf("ExecuteRequest failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
	if !resp.IsSuccess() {
		t.Errorf("Expected IsSuccess to be true")
	}
	if resp.StatusPill() != "200 OK" {
		t.Errorf("Expected 200 OK, got %q", resp.StatusPill())
	}
	if !strings.Contains(resp.PrettyBody, `"status": "ok"`) {
		t.Errorf("Expected formatted JSON in PrettyBody, got %q", resp.PrettyBody)
	}
	if resp.Headers["X-Server"][0] != "TahrServer/1.0" {
		t.Errorf("Missing X-Server response header")
	}
	if resp.Duration <= 0 {
		t.Errorf("Expected positive duration, got %v", resp.Duration)
	}
	if resp.ContentLength <= 0 {
		t.Errorf("Expected positive content length, got %d", resp.ContentLength)
	}
	if resp.FormatSize() == "" {
		t.Errorf("Expected non-empty FormatSize")
	}
}

func TestExecutor_PostJSON(t *testing.T) {
	type payload struct {
		Action string `json:"action"`
		Count  int    `json:"count"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var p payload
		if err := json.Unmarshal(body, &p); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if p.Action != "sync" || p.Count != 5 {
			http.Error(w, "invalid payload data", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result":"created","id":12345}`))
	}))
	defer server.Close()

	exec := NewExecutor(5 * time.Second)
	req := Request{
		Method: "POST",
		URL:    server.URL,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: `{"action":"sync","count":5}`,
	}

	resp, err := exec.ExecuteRequest(req)
	if err != nil {
		t.Fatalf("ExecuteRequest failed: %v", err)
	}

	if resp.StatusCode != 201 {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}
	if !resp.IsSuccess() {
		t.Errorf("Expected 201 to be success")
	}
	if resp.FormatDuration() == "" {
		t.Errorf("Expected non-empty FormatDuration")
	}
	if !strings.Contains(resp.PrettyBody, `"id": 12345`) {
		t.Errorf("Expected formatted JSON with id, got %q", resp.PrettyBody)
	}
}

func TestExecutor_ErrorStatuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/404") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/500") {
			http.Error(w, "crash", http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	exec := NewExecutor(5 * time.Second)

	// 404 test
	resp404, err := exec.ExecuteRequest(Request{Method: "GET", URL: server.URL + "/404"})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp404.StatusCode != 404 || !resp404.IsClientError() {
		t.Errorf("Expected 404 client error, got %d", resp404.StatusCode)
	}

	// 500 test
	resp500, err := exec.ExecuteRequest(Request{Method: "GET", URL: server.URL + "/500"})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp500.StatusCode != 500 || !resp500.IsServerError() {
		t.Errorf("Expected 500 server error, got %d", resp500.StatusCode)
	}
}

func TestExecutor_TransportError(t *testing.T) {
	exec := NewExecutor(500 * time.Millisecond)
	// Non-routable address to trigger quick network error
	req := Request{
		Method: "GET",
		URL:    "http://127.0.0.1:59999/unreachable",
	}

	resp, err := exec.ExecuteRequest(req)
	if err == nil {
		t.Errorf("Expected transport error for unreachable host")
	}
	if resp == nil {
		t.Fatalf("Expected non-nil Response even on transport error")
	}
	if !resp.IsServerError() {
		t.Errorf("Expected IsServerError to be true on transport failure")
	}
	if resp.StatusPill() != "ERROR" {
		t.Errorf("Expected StatusPill ERROR, got %q", resp.StatusPill())
	}
}

func TestAutoFormatting(t *testing.T) {
	// JSON formatting
	rawJSON := []byte(`{"a":1,"b":["x","y"]}`)
	prettyJSON := FormatJSON(rawJSON)
	if !strings.Contains(prettyJSON, "\n  \"a\": 1,") {
		t.Errorf("JSON formatting failed: %q", prettyJSON)
	}

	// XML formatting
	rawXML := []byte(`<user id="1"><name>Alice</name></user>`)
	prettyXML := FormatXML(rawXML)
	if !strings.Contains(prettyXML, "Alice") {
		t.Errorf("XML formatting failed: %q", prettyXML)
	}

	// Auto format XML
	autoXML := AutoFormatBody(rawXML, "application/xml")
	if !strings.Contains(autoXML, "Alice") {
		t.Errorf("AutoFormatBody XML failed: %q", autoXML)
	}

	// Size formatting
	if s := FormatBytes(500); s != "500 B" {
		t.Errorf("Expected '500 B', got %q", s)
	}
	if s := FormatBytes(1228); s != "1.2 KB" {
		t.Errorf("Expected '1.2 KB', got %q", s)
	}
	if s := FormatBytes(5 * 1024 * 1024); s != "5.0 MB" {
		t.Errorf("Expected '5.0 MB', got %q", s)
	}
}
