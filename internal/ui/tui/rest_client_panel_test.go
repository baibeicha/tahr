package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/restclient"
	"tahr/internal/ui"
)

func sampleTestTheme() *ui.Theme {
	t := ui.CatppuccinMocha()
	return &t
}

func sampleTestResponse() *restclient.Response {
	req := &restclient.Request{
		Method: "POST",
		URL:    "https://api.example.com/v1/users",
		Proto:  "HTTP/1.1",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: `{"name":"Alice"}`,
	}

	return &restclient.Response{
		StatusCode: 200,
		StatusText: "200 OK",
		Proto:      "HTTP/1.1",
		Headers: map[string][]string{
			"Content-Type": {"application/json; charset=utf-8"},
			"Server":       {"Cloudflare"},
		},
		Body:          []byte(`{"id":"123","status":"active"}`),
		BodyString:    `{"id":"123","status":"active"}`,
		PrettyBody:    "{\n  \"id\": \"123\",\n  \"status\": \"active\"\n}",
		ContentLength: 32,
		Duration:      45 * time.Millisecond,
		DurationMs:    45,
		ContentType:   "application/json",
		Request:       req,
		RawRequest:    "POST https://api.example.com/v1/users HTTP/1.1\r\nContent-Type: application/json\r\n\r\n{\"name\":\"Alice\"}",
		Timestamp:     time.Now(),
	}
}

func TestRESTClientPanel_Initialization(t *testing.T) {
	theme := sampleTestTheme()
	panel := NewRESTClientPanel(theme)

	if panel.Open {
		t.Errorf("Expected panel to be initially closed")
	}
	if panel.ActiveTab != RESTTabBody {
		t.Errorf("Expected initial tab to be RESTTabBody (0), got %v", panel.ActiveTab)
	}
	if panel.Response != nil {
		t.Errorf("Expected initially nil response")
	}

	panel.Toggle()
	if !panel.Open {
		t.Errorf("Expected panel to be open after Toggle()")
	}
}

func TestRESTClientPanel_SetResponseAndClear(t *testing.T) {
	theme := sampleTestTheme()
	panel := NewRESTClientPanel(theme)
	resp := sampleTestResponse()

	panel.SetResponse(resp)
	if panel.Response == nil || panel.Response.StatusCode != 200 {
		t.Fatalf("Response not set properly")
	}

	cleared := false
	panel.OnClear = func() {
		cleared = true
	}

	panel.Clear()
	if panel.Response != nil {
		t.Errorf("Expected Response to be nil after Clear()")
	}
	if !cleared {
		t.Errorf("Expected OnClear callback to be invoked")
	}
}

func TestRESTClientPanel_KeyNavigation(t *testing.T) {
	theme := sampleTestTheme()
	panel := NewRESTClientPanel(theme)
	resp := sampleTestResponse()
	panel.SetResponse(resp)
	panel.Open = true

	// Tab switching
	panel.HandleKey(input.Key{Type: input.KeyTab})
	if panel.ActiveTab != RESTTabHeaders {
		t.Errorf("Expected tab to switch to RESTTabHeaders, got %v", panel.ActiveTab)
	}

	panel.HandleKey(input.Key{Type: input.KeyTab})
	if panel.ActiveTab != RESTTabRequest {
		t.Errorf("Expected tab to switch to RESTTabRequest, got %v", panel.ActiveTab)
	}

	panel.HandleKey(input.Key{Type: input.KeyBacktab})
	if panel.ActiveTab != RESTTabHeaders {
		t.Errorf("Expected backtab to switch to RESTTabHeaders, got %v", panel.ActiveTab)
	}

	// Scrolling
	panel.HandleKey(input.Key{Type: input.KeyDown})
	if panel.ScrollY != 1 {
		t.Errorf("Expected ScrollY to be 1, got %d", panel.ScrollY)
	}

	panel.HandleKey(input.Key{Type: input.KeyUp})
	if panel.ScrollY != 0 {
		t.Errorf("Expected ScrollY to be 0, got %d", panel.ScrollY)
	}

	// Esc closes panel
	closed := false
	panel.OnClose = func() {
		closed = true
	}
	panel.HandleKey(input.Key{Type: input.KeyEsc})
	if panel.Open {
		t.Errorf("Expected panel.Open to be false after Esc")
	}
	if !closed {
		t.Errorf("Expected OnClose callback after Esc")
	}
}

func TestRESTClientPanel_MouseInteraction(t *testing.T) {
	theme := sampleTestTheme()
	panel := NewRESTClientPanel(theme)
	resp := sampleTestResponse()
	panel.SetResponse(resp)
	panel.Open = true

	// Perform render to populate action hitboxes
	buf := buffer.NewBuffer(80, 24)
	panel.RenderAt(buf, 0, 0, 80, 24, theme)

	if len(panel.ActionHits) == 0 {
		t.Fatalf("Expected action hitboxes to be registered during render")
	}

	// Mouse Wheel Down
	panel.HandleMouse(input.Mouse{Button: input.MouseWheelDown}, 80, 24)
	if panel.ScrollY <= 0 {
		t.Errorf("Expected ScrollY to increase after wheel down, got %d", panel.ScrollY)
	}

	// Mouse Wheel Up
	panel.HandleMouse(input.Mouse{Button: input.MouseWheelUp}, 80, 24)
	if panel.ScrollY != 0 {
		t.Errorf("Expected ScrollY to return to 0 after wheel up, got %d", panel.ScrollY)
	}

	// Click on Headers Tab
	var headersHit *RESTActionHit
	for i := range panel.ActionHits {
		if panel.ActionHits[i].Action == "tab_headers" {
			headersHit = &panel.ActionHits[i]
			break
		}
	}
	if headersHit == nil {
		t.Fatalf("tab_headers hitbox not found")
	}
	panel.HandleClick(headersHit.X, headersHit.Y)
	if panel.ActiveTab != RESTTabHeaders {
		t.Errorf("Expected tab to switch to Headers on click, got %v", panel.ActiveTab)
	}

	// Click Copy Button
	copiedText := ""
	panel.OnCopy = func(txt string) {
		copiedText = txt
	}
	var copyHit *RESTActionHit
	for i := range panel.ActionHits {
		if panel.ActionHits[i].Action == "copy" {
			copyHit = &panel.ActionHits[i]
			break
		}
	}
	if copyHit == nil {
		t.Fatalf("copy hitbox not found")
	}
	panel.HandleClick(copyHit.X, copyHit.Y)
	if copiedText == "" || !strings.Contains(copiedText, "Server") {
		t.Errorf("Expected copiedText to contain headers, got %q", copiedText)
	}
}

func TestRESTClientPanel_RenderingAllTabs(t *testing.T) {
	theme := sampleTestTheme()
	panel := NewRESTClientPanel(theme)
	resp := sampleTestResponse()
	panel.SetResponse(resp)

	buf := buffer.NewBuffer(90, 24)

	// Render Body tab
	panel.ActiveTab = RESTTabBody
	panel.Render(buf, buffer.NewRect(0, 0, 90, 24))

	// Render Headers tab
	panel.ActiveTab = RESTTabHeaders
	panel.Render(buf, buffer.NewRect(0, 0, 90, 24))

	// Render Request tab
	panel.ActiveTab = RESTTabRequest
	panel.Render(buf, buffer.NewRect(0, 0, 90, 24))

	// Render 4xx Client Error
	resp.StatusCode = 404
	resp.StatusText = "404 Not Found"
	panel.Render(buf, buffer.NewRect(0, 0, 90, 24))

	// Render 5xx Server Error
	resp.StatusCode = 500
	resp.StatusText = "500 Internal Server Error"
	panel.Render(buf, buffer.NewRect(0, 0, 90, 24))

	// Render Empty / Nil Response
	panel.Clear()
	panel.Render(buf, buffer.NewRect(0, 0, 90, 24))
}
