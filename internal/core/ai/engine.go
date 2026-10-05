package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Engine orchestrates AI code completion requests, caching, and sidecar daemon.
type Engine struct {
	mu         sync.RWMutex
	cfg        Config
	cache      *CompletionCache
	sidecar    *LlamaServerSidecar
	httpClient *http.Client
}

// NewEngine creates an AI completion engine with the given configuration.
func NewEngine(cfg Config, workspaceDir string) *Engine {
	sidecar := NewLlamaServerSidecar(cfg, workspaceDir)
	return &Engine{
		cfg:        cfg,
		cache:      NewCompletionCache(256),
		sidecar:    sidecar,
		httpClient: &http.Client{Timeout: 4 * time.Second},
	}
}

// SetWorkspaceDir updates the workspace directory for the engine and sidecar.
func (e *Engine) SetWorkspaceDir(dir string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sidecar != nil {
		e.sidecar.SetWorkspaceDir(dir)
	}
}

// UpdateConfig updates the engine's configuration and passes it down to the sidecar.
func (e *Engine) UpdateConfig(cfg Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
	if e.sidecar != nil {
		e.sidecar.UpdateConfig(cfg)
	}
}

// Config returns a copy of the current configuration.
func (e *Engine) Config() Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// Start activates the engine and starts the local sidecar if configured.
func (e *Engine) Start() error {
	e.mu.RLock()
	provider := NormalizeProvider(e.cfg.Provider)
	enabled := e.cfg.Enabled
	e.mu.RUnlock()

	if !enabled {
		return nil
	}

	if provider == ProviderBuiltin && e.sidecar != nil {
		if e.sidecar.IsAvailable() {
			return e.sidecar.Start()
		}
	}
	return nil
}

// Stop deactivates the engine and shuts down the local sidecar.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sidecar != nil {
		e.sidecar.Stop()
	}
	if e.cache != nil {
		e.cache.Clear()
	}
}

// Sidecar returns the underlying LlamaServerSidecar.
func (e *Engine) Sidecar() *LlamaServerSidecar {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.sidecar
}

// ClearCache purges the in-memory LRU completion cache.
func (e *Engine) ClearCache() {
	if e.cache != nil {
		e.cache.Clear()
	}
}

// RequestCompletion requests an inline code completion given prefix and suffix context.
func (e *Engine) RequestCompletion(ctx context.Context, prefix, suffix string, multiline bool) (string, error) {
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()

	if !cfg.Enabled {
		return "", nil
	}

	// Instant cache lookup (zero network latency)
	if val, ok := e.cache.Get(prefix, suffix); ok {
		return val, nil
	}

	provider := NormalizeProvider(cfg.Provider)
	port := cfg.Port
	if port <= 0 {
		port = 8989
	}

	// For builtin provider, ensure daemon is responding
	if provider == ProviderBuiltin {
		if e.sidecar != nil && !e.sidecar.IsRunning() {
			if e.sidecar.IsAvailable() {
				// Attempt lazy background start
				_ = e.sidecar.Start()
			}
			if !e.sidecar.IsRunning() {
				// Daemon not available; exit silently
				return "", nil
			}
		}
	}

	prompt := BuildFIMPrompt(prefix, suffix)
	var stopTokens []string
	if multiline {
		stopTokens = MultiLineStopTokens
	} else {
		stopTokens = SingleLineStopTokens
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 32
	}
	temp := cfg.Temperature
	if temp < 0 {
		temp = 0.1
	}

	var endpoint string
	var reqBody []byte
	var err error

	if provider == ProviderOllama {
		endpoint = cfg.Endpoint
		if endpoint == "" {
			endpoint = "http://127.0.0.1:11434/api/generate"
		}
		ollamaPayload := map[string]interface{}{
			"model":  cfg.ModelName,
			"prompt": prefix,
			"suffix": suffix,
			"options": map[string]interface{}{
				"num_predict": maxTokens,
				"temperature": temp,
				"stop":        stopTokens,
			},
			"stream": false,
		}
		reqBody, err = json.Marshal(ollamaPayload)
	} else {
		// Builtin, OpenAI, DeepSeek, Custom
		endpoint = cfg.Endpoint
		if provider == ProviderBuiltin || endpoint == "" {
			endpoint = fmt.Sprintf("http://127.0.0.1:%d/v1/completions", port)
		}
		payload := map[string]interface{}{
			"model":       cfg.ModelName,
			"prompt":      prompt,
			"max_tokens":  maxTokens,
			"temperature": temp,
			"stop":        stopTokens,
			"stream":      false,
		}
		reqBody, err = json.Marshal(payload)
	}

	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		// Suppress or return network/cancellation errors
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ai completion returned HTTP %d", resp.StatusCode)
	}

	respData, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}

	rawText := extractCompletionText(respData)
	if rawText == "" {
		return "", nil
	}

	cleaned := CleanCompletion(rawText, multiline)
	if cleaned != "" {
		e.cache.Put(prefix, suffix, cleaned)
	}
	return cleaned, nil
}

// extractCompletionText parses OpenAI, Ollama, or llama-server JSON responses.
func extractCompletionText(data []byte) string {
	// Try OpenAI format: {"choices": [{"text": "..."}]}
	var openAIResp struct {
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &openAIResp); err == nil && len(openAIResp.Choices) > 0 {
		return openAIResp.Choices[0].Text
	}

	// Try Ollama format: {"response": "..."}
	var ollamaResp struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(data, &ollamaResp); err == nil && ollamaResp.Response != "" {
		return ollamaResp.Response
	}

	// Try native llama.cpp /completion format: {"content": "..."}
	var llamaResp struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(data, &llamaResp); err == nil && llamaResp.Content != "" {
		return llamaResp.Content
	}

	return ""
}
