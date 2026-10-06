package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Status indicators for AI completion readiness.
const (
	StatusReady       = "Ready"
	StatusDownloading = "Downloading"
	StatusGenerating  = "Generating"
	StatusStandby     = "Standby"
	StatusOffline     = "Offline"
	StatusNoModel     = "No Model"
	StatusNoServer    = "No Server"
	StatusNoAPIKey    = "No API Key"
	StatusDisabled    = "Disabled"
)

// Engine orchestrates AI code completion requests, caching, and sidecar daemon.
type Engine struct {
	mu         sync.RWMutex
	cfg        Config
	cache      *CompletionCache
	sidecar    *LlamaServerSidecar
	httpClient *http.Client
	generating atomic.Bool
	downloader *Downloader
}

// NewEngine creates an AI completion engine with the given configuration.
func NewEngine(cfg Config, workspaceDir string) *Engine {
	sidecar := NewLlamaServerSidecar(cfg, workspaceDir)
	return &Engine{
		cfg:        cfg,
		cache:      NewCompletionCache(256),
		sidecar:    sidecar,
		httpClient: &http.Client{Timeout: 4 * time.Second},
		downloader: NewDownloader(),
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

// Status inspects the current readiness of the configured AI provider.
func (e *Engine) Status() (string, string) {
	if e == nil {
		return StatusDisabled, "ai engine not initialized"
	}
	e.mu.RLock()
	cfg := e.cfg
	sidecar := e.sidecar
	e.mu.RUnlock()

	if !cfg.Enabled {
		return StatusDisabled, "plugin is disabled"
	}
	if e.downloader != nil && e.downloader.IsDownloading() {
		pct, msg := e.downloader.Progress()
		return StatusDownloading, fmt.Sprintf("%d%% (%s)", pct, msg)
	}
	if e.generating.Load() {
		return StatusGenerating, "generating suggestion..."
	}

	provider := NormalizeProvider(cfg.Provider)
	switch provider {
	case ProviderBuiltin:
		port := cfg.Port
		if port <= 0 {
			port = 8989
		}
		if sidecar != nil && sidecar.IsRunning() {
			return StatusReady, fmt.Sprintf("llama-server running on 127.0.0.1:%d", port)
		}
		if sidecar != nil {
			bin := sidecar.ResolveServerBinary()
			model := sidecar.ResolveModelFile()
			if bin == "" && model == "" {
				return StatusNoModel, "llama-server and model .gguf not found in plugins/ai-completion/"
			}
			if bin == "" {
				return StatusNoServer, "llama-server binary not found in plugins/ai-completion/bin/"
			}
			if model == "" {
				return StatusNoModel, "model .gguf not found in plugins/ai-completion/models/"
			}
			return StatusStandby, "local model ready (will start on demand)"
		}
		return StatusOffline, "sidecar daemon not available"

	case ProviderOllama:
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = "http://127.0.0.1:11434"
		}
		client := http.Client{Timeout: 200 * time.Millisecond}
		pingURL := strings.TrimSuffix(endpoint, "/api/generate")
		resp, err := client.Get(pingURL + "/api/tags")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return StatusReady, fmt.Sprintf("Ollama connected (%s)", cfg.ModelName)
		}
		return StatusOffline, fmt.Sprintf("cannot connect to Ollama at %s", endpoint)

	case ProviderOpenAI, ProviderDeepSeek, ProviderCustom:
		if cfg.APIKey == "" && provider == ProviderOpenAI {
			return StatusNoAPIKey, "api_key not configured"
		}
		if cfg.Endpoint == "" {
			return StatusOffline, "endpoint URL not configured"
		}
		return StatusReady, fmt.Sprintf("Cloud API (%s)", cfg.ModelName)

	default:
		return StatusOffline, "unknown provider"
	}
}

// IsDownloading returns true if a background download is in progress.
func (e *Engine) IsDownloading() bool {
	if e == nil || e.downloader == nil {
		return false
	}
	return e.downloader.IsDownloading()
}

// DownloadProgress returns current percent (0-100) and status description.
func (e *Engine) DownloadProgress() (int, string) {
	if e == nil || e.downloader == nil {
		return 0, ""
	}
	return e.downloader.Progress()
}

// CheckAndAutoDownload checks if local model/binary are missing and triggers background download.
func (e *Engine) CheckAndAutoDownload(workspaceDir string, onStart func(), onDone func(), onError func(err error)) bool {
	if e == nil || e.downloader == nil {
		return false
	}
	e.mu.RLock()
	cfg := e.cfg
	sidecar := e.sidecar
	e.mu.RUnlock()

	if !cfg.Enabled || NormalizeProvider(cfg.Provider) != ProviderBuiltin {
		return false
	}

	needServer := false
	needModel := false
	if sidecar != nil {
		if sidecar.ResolveServerBinary() == "" {
			needServer = true
		}
		if sidecar.ResolveModelFile() == "" {
			needModel = true
		}
	}

	if !needServer && !needModel {
		return false
	}

	targetWs := workspaceDir
	if targetWs == "" && sidecar != nil {
		targetWs = sidecar.workspaceDir
	}
	if targetWs == "" {
		targetWs = "."
	}

	modelsDir := filepath.Join(targetWs, "plugins", "ai-completion", "models")
	binDir := filepath.Join(targetWs, "plugins", "ai-completion", "bin")

	return e.downloader.StartBackgroundDownload(modelsDir, binDir, needServer, needModel, onStart, func() {
		// On successful completion, start sidecar daemon
		_ = e.Start()
		if onDone != nil {
			onDone()
		}
	}, onError)
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

	e.generating.Store(true)
	defer e.generating.Store(false)

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
