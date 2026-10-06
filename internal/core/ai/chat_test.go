package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestExtractCodeBlocks(t *testing.T) {
	text := `Here is some Go code:
` + "```go\nfunc add(a, b int) int {\n\treturn a + b\n}\n```\n" +
		`And some Python:
` + "```python\ndef add(a, b):\n    return a + b\n```\nDone."

	snippets := ExtractCodeBlocks(text)
	if len(snippets) != 2 {
		t.Fatalf("expected 2 snippets, got %d", len(snippets))
	}
	if snippets[0].Language != "go" || !strings.Contains(snippets[0].Code, "func add") {
		t.Errorf("unexpected snippet 0: %+v", snippets[0])
	}
	if snippets[1].Language != "python" || !strings.Contains(snippets[1].Code, "def add") {
		t.Errorf("unexpected snippet 1: %+v", snippets[1])
	}
}

func TestStreamChatOpenAI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"world!\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	cfg := DefaultChatConfig()
	cfg.Provider = ChatProviderOpenAI
	cfg.Endpoint = ts.URL

	engine := NewChatEngine(cfg)

	var mu sync.Mutex
	var collected strings.Builder
	var doneCalled bool

	err := engine.StreamChat(context.Background(), []ChatMessage{
		{Role: "user", Content: "Hi"},
	}, func(token string) {
		mu.Lock()
		collected.WriteString(token)
		mu.Unlock()
	}, func() {
		mu.Lock()
		doneCalled = true
		mu.Unlock()
	})

	if err != nil {
		t.Fatalf("StreamChat failed: %v", err)
	}

	mu.Lock()
	res := collected.String()
	isDone := doneCalled
	mu.Unlock()

	if res != "Hello world!" {
		t.Errorf("expected 'Hello world!', got '%s'", res)
	}
	if !isDone {
		t.Errorf("expected onDone to be called")
	}
}

func TestStreamChatAnthropic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Claude \"}}\n\n")
		flusher.Flush()
		fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"response\"}}\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	cfg := DefaultChatConfig()
	cfg.Provider = ChatProviderAnthropic
	cfg.Endpoint = ts.URL
	cfg.APIKey = "test-key"

	engine := NewChatEngine(cfg)

	var collected strings.Builder
	err := engine.StreamChat(context.Background(), []ChatMessage{
		{Role: "user", Content: "Explain"},
	}, func(token string) {
		collected.WriteString(token)
	}, nil)

	if err != nil {
		t.Fatalf("Anthropic StreamChat failed: %v", err)
	}
	if collected.String() != "Claude response" {
		t.Errorf("expected 'Claude response', got '%s'", collected.String())
	}
}

func TestStreamChatOllama(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		fmt.Fprintf(w, "{\"message\":{\"content\":\"Local \"},\"done\":false}\n")
		flusher.Flush()
		fmt.Fprintf(w, "{\"message\":{\"content\":\"Ollama\"},\"done\":true}\n")
		flusher.Flush()
	}))
	defer ts.Close()

	cfg := DefaultChatConfig()
	cfg.Provider = ChatProviderOllama
	cfg.Endpoint = ts.URL

	engine := NewChatEngine(cfg)

	var collected strings.Builder
	err := engine.StreamChat(context.Background(), []ChatMessage{
		{Role: "user", Content: "Test"},
	}, func(token string) {
		collected.WriteString(token)
	}, nil)

	if err != nil {
		t.Fatalf("Ollama StreamChat failed: %v", err)
	}
	if collected.String() != "Local Ollama" {
		t.Errorf("expected 'Local Ollama', got '%s'", collected.String())
	}
}
