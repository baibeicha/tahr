package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Supported chat providers
const (
	ChatProviderAnthropic = "anthropic"
	ChatProviderOpenAI    = "openai"
	ChatProviderDeepSeek  = "deepseek"
	ChatProviderOllama    = "ollama"
	ChatProviderCustom    = "custom"
	ChatProviderBuiltin   = "builtin"
)

// ChatConfig describes settings for the conversational AI assistant.
type ChatConfig struct {
	Provider     string   `json:"provider"`
	Endpoint     string   `json:"endpoint"`
	Model        string   `json:"model"`
	APIKey       string   `json:"api_key"`
	Temperature  float64  `json:"temperature"`
	MaxTokens    int      `json:"max_tokens"`
	SystemPrompt string   `json:"system_prompt"`
	ModelPresets []string `json:"model_presets"`
}

// DefaultChatConfig returns sensible defaults for chat assistants.
func DefaultChatConfig() ChatConfig {
	return ChatConfig{
		Provider:    ChatProviderOllama,
		Endpoint:    "http://127.0.0.1:11434",
		Model:       "qwen2.5-coder:7b",
		APIKey:      "",
		Temperature: 0.2,
		MaxTokens:   4096,
		SystemPrompt: "You are Tahr AI Assistant, an expert programming companion embedded directly into Tahr IDE. " +
			"Provide concise, high-quality, bug-free code. Always format code using standard markdown blocks with the language identifier (e.g. ```go).",
		ModelPresets: []string{
			"Claude-3.7-Sonnet",
			"Claude-3.5-Sonnet",
			"GPT-4o",
			"GPT-4o-mini",
			"DeepSeek-V3",
			"Ollama: qwen2.5-coder:7b",
			"Ollama: llama3",
			"Local llama-server",
		},
	}
}

// CodeSnippet represents an extracted code block from a chat message.
type CodeSnippet struct {
	Language string
	Code     string
	Index    int
}

// ChatMessage represents a single message in the chat conversation.
type ChatMessage struct {
	Role       string        `json:"role"` // "system", "user", "assistant"
	Content    string        `json:"content"`
	CodeBlocks []CodeSnippet `json:"code_blocks,omitempty"`
	Timestamp  time.Time     `json:"timestamp"`
}

// ChatEngine handles multi-provider chat completions and SSE token streaming.
type ChatEngine struct {
	mu         sync.RWMutex
	cfg        ChatConfig
	httpClient *http.Client
}

// NewChatEngine creates a new conversational AI engine.
func NewChatEngine(cfg ChatConfig) *ChatEngine {
	if cfg.Endpoint == "" {
		cfg = DefaultChatConfig()
	}
	return &ChatEngine{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Config returns the current chat configuration.
func (c *ChatEngine) Config() ChatConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// UpdateConfig updates the engine's configuration.
func (c *ChatEngine) UpdateConfig(cfg ChatConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = cfg
}

// NormalizeChatProvider normalizes provider strings.
func NormalizeChatProvider(p string) string {
	lower := strings.ToLower(strings.TrimSpace(p))
	switch lower {
	case "anthropic", "claude":
		return ChatProviderAnthropic
	case "openai", "gpt":
		return ChatProviderOpenAI
	case "deepseek":
		return ChatProviderDeepSeek
	case "ollama":
		return ChatProviderOllama
	case "builtin", "local", "llama-server":
		return ChatProviderBuiltin
	default:
		return ChatProviderCustom
	}
}

// StreamChat sends the conversation history to the configured provider and streams back tokens.
func (c *ChatEngine) StreamChat(
	ctx context.Context,
	messages []ChatMessage,
	onToken func(token string),
	onDone func(),
) error {
	c.mu.RLock()
	cfg := c.cfg
	c.mu.RUnlock()

	provider := NormalizeChatProvider(cfg.Provider)

	switch provider {
	case ChatProviderAnthropic:
		return c.streamAnthropic(ctx, cfg, messages, onToken, onDone)
	case ChatProviderOllama:
		// If endpoint contains /v1, use OpenAI format, otherwise native Ollama /api/chat
		if strings.Contains(cfg.Endpoint, "/v1") {
			return c.streamOpenAI(ctx, cfg, messages, onToken, onDone)
		}
		return c.streamOllama(ctx, cfg, messages, onToken, onDone)
	default:
		// OpenAI, DeepSeek, Custom, and llama-server all implement the standard OpenAI chat format
		return c.streamOpenAI(ctx, cfg, messages, onToken, onDone)
	}
}

// streamOpenAI handles standard OpenAI / DeepSeek / llama-server SSE streaming.
func (c *ChatEngine) streamOpenAI(
	ctx context.Context,
	cfg ChatConfig,
	messages []ChatMessage,
	onToken func(token string),
	onDone func(),
) error {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/chat/completions"
	}
	if !strings.HasSuffix(endpoint, "/chat/completions") && !strings.Contains(endpoint, "completions") {
		endpoint = strings.TrimSuffix(endpoint, "/") + "/v1/chat/completions"
	}

	type apiMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	apiMessages := make([]apiMsg, 0, len(messages)+1)
	if cfg.SystemPrompt != "" {
		apiMessages = append(apiMessages, apiMsg{Role: "system", Content: cfg.SystemPrompt})
	}
	for _, m := range messages {
		apiMessages = append(apiMessages, apiMsg{Role: m.Role, Content: m.Content})
	}

	payload := map[string]any{
		"model":       cfg.Model,
		"messages":    apiMessages,
		"temperature": cfg.Temperature,
		"stream":      true,
	}
	if cfg.MaxTokens > 0 {
		payload["max_tokens"] = cfg.MaxTokens
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "data: [DONE]" {
			break
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
					onToken(chunk.Choices[0].Delta.Content)
				}
			}
		}
	}

	if onDone != nil {
		onDone()
	}
	return scanner.Err()
}

// streamAnthropic handles Anthropic Claude SSE messages format (/v1/messages).
func (c *ChatEngine) streamAnthropic(
	ctx context.Context,
	cfg ChatConfig,
	messages []ChatMessage,
	onToken func(token string),
	onDone func(),
) error {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1/messages"
	}
	if !strings.HasSuffix(endpoint, "/messages") {
		endpoint = strings.TrimSuffix(endpoint, "/") + "/v1/messages"
	}

	type apiMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	apiMessages := make([]apiMsg, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			continue
		}
		role := m.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}
		apiMessages = append(apiMessages, apiMsg{Role: role, Content: m.Content})
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	modelName := cfg.Model
	if modelName == "" {
		modelName = "claude-3-7-sonnet-20250219"
	}

	payload := map[string]any{
		"model":       modelName,
		"messages":    apiMessages,
		"max_tokens":  maxTokens,
		"temperature": cfg.Temperature,
		"stream":      true,
	}
	if cfg.SystemPrompt != "" {
		payload["system"] = cfg.SystemPrompt
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if cfg.APIKey != "" {
		req.Header.Set("x-api-key", cfg.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Claude API error %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var chunk struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				if chunk.Delta.Text != "" {
					onToken(chunk.Delta.Text)
				}
			}
		}
	}

	if onDone != nil {
		onDone()
	}
	return scanner.Err()
}

// streamOllama handles Ollama native /api/chat streaming format.
func (c *ChatEngine) streamOllama(
	ctx context.Context,
	cfg ChatConfig,
	messages []ChatMessage,
	onToken func(token string),
	onDone func(),
) error {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434"
	}
	endpoint = strings.TrimSuffix(endpoint, "/") + "/api/chat"

	type apiMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	apiMessages := make([]apiMsg, 0, len(messages)+1)
	if cfg.SystemPrompt != "" {
		apiMessages = append(apiMessages, apiMsg{Role: "system", Content: cfg.SystemPrompt})
	}
	for _, m := range messages {
		apiMessages = append(apiMessages, apiMsg{Role: m.Role, Content: m.Content})
	}

	payload := map[string]any{
		"model":    cfg.Model,
		"messages": apiMessages,
		"stream":   true,
		"options": map[string]any{
			"temperature": cfg.Temperature,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Ollama API error %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if err := json.Unmarshal([]byte(line), &chunk); err == nil {
			if chunk.Message.Content != "" {
				onToken(chunk.Message.Content)
			}
			if chunk.Done {
				break
			}
		}
	}

	if onDone != nil {
		onDone()
	}
	return scanner.Err()
}

var codeBlockRegex = regexp.MustCompile("(?s)```([a-zA-Z0-9_-]*)\r?\n(.*?)\r?\n```")

// ExtractCodeBlocks parses markdown and extracts all fenced code snippets.
func ExtractCodeBlocks(markdown string) []CodeSnippet {
	matches := codeBlockRegex.FindAllStringSubmatch(markdown, -1)
	if len(matches) == 0 {
		return nil
	}
	snippets := make([]CodeSnippet, 0, len(matches))
	for i, m := range matches {
		lang := strings.TrimSpace(m[1])
		if lang == "" {
			lang = "text"
		}
		code := strings.TrimRight(m[2], "\r\n")
		snippets = append(snippets, CodeSnippet{
			Language: lang,
			Code:     code,
			Index:    i,
		})
	}
	return snippets
}
