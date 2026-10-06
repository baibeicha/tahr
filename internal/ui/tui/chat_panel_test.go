package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/ai"
	"tahr/internal/ui"
)

func TestChatPanel_InputAndHistory(t *testing.T) {
	panel := NewChatPanel(nil)
	panel.InputFocused = true

	// Type "hello"
	for _, r := range "hello" {
		panel.HandleKey(input.Key{Rune: r}, "main.go", "", "", "")
	}

	if panel.InputText != "hello" {
		t.Fatalf("expected InputText to be 'hello', got '%s'", panel.InputText)
	}

	// Backspace
	panel.HandleKey(input.Key{Type: input.KeyBackspace}, "main.go", "", "", "")
	if panel.InputText != "hell" {
		t.Fatalf("expected 'hell', got '%s'", panel.InputText)
	}

	// Home and type 'x'
	panel.HandleKey(input.Key{Type: input.KeyHome}, "main.go", "", "", "")
	panel.HandleKey(input.Key{Rune: 'x'}, "main.go", "", "", "")
	if panel.InputText != "xhell" {
		t.Fatalf("expected 'xhell', got '%s'", panel.InputText)
	}

	// Submit with Enter
	panel.HandleKey(input.Key{Type: input.KeyEnter}, "main.go", "", "", "")
	if panel.InputText != "" {
		t.Fatalf("expected InputText to be cleared, got '%s'", panel.InputText)
	}
	if len(panel.History) != 1 || panel.History[0] != "xhell" {
		t.Fatalf("expected history 'xhell', got %+v", panel.History)
	}

	// Up arrow recovers history
	panel.HandleKey(input.Key{Type: input.KeyUp}, "main.go", "", "", "")
	if panel.InputText != "xhell" {
		t.Fatalf("expected history recall 'xhell', got '%s'", panel.InputText)
	}
}

func TestChatPanel_SlashCommands(t *testing.T) {
	panel := NewChatPanel(nil)
	panel.InputFocused = true

	// /clear
	panel.InputText = "/clear"
	panel.InputCursor = 6
	var toastTitle string
	panel.OnToast = func(lvl, title, msg string) {
		toastTitle = title
	}
	panel.HandleKey(input.Key{Type: input.KeyEnter}, "main.go", "", "", "")
	if len(panel.Messages) != 0 {
		t.Fatalf("expected Messages to be empty after /clear, got %d", len(panel.Messages))
	}
	if toastTitle != "AI CHAT" {
		t.Errorf("expected AI CHAT toast, got %s", toastTitle)
	}

	// /model
	panel.InputText = "/model GPT-4o"
	panel.InputCursor = len([]rune(panel.InputText))
	panel.HandleKey(input.Key{Type: input.KeyEnter}, "main.go", "", "", "")
	if panel.ActiveModel != "GPT-4o" {
		t.Fatalf("expected ActiveModel to be 'GPT-4o', got '%s'", panel.ActiveModel)
	}
}

func TestChatPanel_ModelDropdown(t *testing.T) {
	panel := NewChatPanel(nil)

	// Alt+M toggles dropdown
	panel.HandleKey(input.Key{Rune: 'm', Mod: input.ModAlt}, "", "", "", "")
	if !panel.ModelMenuOpen {
		t.Fatalf("expected ModelMenuOpen to be true")
	}

	// Down arrow
	panel.HandleKey(input.Key{Type: input.KeyDown}, "", "", "", "")
	if panel.ModelMenuSel != 1 {
		t.Fatalf("expected ModelMenuSel to be 1, got %d", panel.ModelMenuSel)
	}

	// Enter selects model
	panel.HandleKey(input.Key{Type: input.KeyEnter}, "", "", "", "")
	if panel.ModelMenuOpen {
		t.Fatalf("expected ModelMenuOpen to be false after selection")
	}
	if panel.ActiveModel != panel.ModelPresets[1] {
		t.Fatalf("expected ActiveModel to be '%s', got '%s'", panel.ModelPresets[1], panel.ActiveModel)
	}
}

func TestChatPanel_ContextExpansion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	cfg := ai.DefaultChatConfig()
	cfg.Provider = ai.ChatProviderOpenAI
	cfg.Endpoint = ts.URL
	engine := ai.NewChatEngine(cfg)

	panel := NewChatPanel(engine)
	panel.InputText = "Inspect @file with @selection and @diagnostics"
	panel.InputCursor = len([]rune(panel.InputText))

	panel.SubmitPrompt("pkg/core/main.go", "func test() {}", "line 10: undefined var", "")

	// Give a moment for goroutine to append message
	time.Sleep(50 * time.Millisecond)

	if len(panel.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(panel.Messages))
	}
	var userMsg string
	for _, m := range panel.Messages {
		if m.Role == "user" {
			userMsg = m.Content
			break
		}
	}
	if userMsg == "" {
		t.Fatalf("expected user message in panel.Messages, got %+v", panel.Messages)
	}
	if !strings.Contains(userMsg, "[File: main.go]") {
		t.Errorf("expected @file expansion, got %s", userMsg)
	}
	if !strings.Contains(userMsg, "func test() {}") {
		t.Errorf("expected @selection expansion, got %s", userMsg)
	}
	if !strings.Contains(userMsg, "line 10: undefined var") {
		t.Errorf("expected @diagnostics expansion, got %s", userMsg)
	}
}

func TestChatPanel_RenderAndClickActions(t *testing.T) {
	panel := NewChatPanel(nil)
	panel.Messages = []ai.ChatMessage{
		{
			Role: "assistant",
			Content: "Here is your code:\n```go\nfunc hello() {\n\tprintln(\"hi\")\n}\n```\nEnjoy!",
			Timestamp: time.Now(),
		},
	}

	theme := ui.CatppuccinMocha()
	buf := buffer.NewBuffer(60, 30)

	panel.Render(buf, 20, 0, 40, 30, "main.go", &theme)

	if len(panel.CodeActionHits) != 3 {
		t.Fatalf("expected 3 code action buttons ([Apply], [Insert], [Copy]), got %d", len(panel.CodeActionHits))
	}

	var appliedCode string
	panel.OnApplyCode = func(code string) {
		appliedCode = code
	}

	// Click on [Apply]
	applyHit := panel.CodeActionHits[0]
	handled := panel.HandleClick(applyHit.X, applyHit.Y, 20, 0, 40, 30)
	if !handled {
		t.Fatalf("expected click to be handled")
	}
	if !strings.Contains(appliedCode, "func hello()") {
		t.Fatalf("expected appliedCode to contain 'func hello()', got '%s'", appliedCode)
	}
}
