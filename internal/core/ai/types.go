package ai

import (
	"runtime"
	"strings"
)

// Provider types
const (
	ProviderBuiltin  = "builtin"
	ProviderOllama   = "ollama"
	ProviderOpenAI   = "openai"
	ProviderDeepSeek = "deepseek"
	ProviderCustom   = "custom"
)

// Config describes AI code completion runtime settings.
type Config struct {
	Enabled         bool    `json:"enabled"`
	Provider        string  `json:"provider"`
	ModelPath       string  `json:"model_path"`
	LlamaServerPath string  `json:"llama_server_path"`
	Endpoint        string  `json:"endpoint"`
	Port            int     `json:"port"`
	APIKey          string  `json:"api_key"`
	ModelName       string  `json:"model_name"`
	DebounceMs      int     `json:"debounce_ms"`
	MaxTokens       int     `json:"max_tokens"`
	Temperature     float64 `json:"temperature"`
	Threads         int     `json:"threads"`
	ContextSize     int     `json:"context_size"`
}

// DefaultConfig returns optimal defaults for local and cloud completion.
func DefaultConfig() Config {
	threads := runtime.NumCPU() / 2
	if threads < 2 {
		threads = 2
	}
	return Config{
		Enabled:         true,
		Provider:        ProviderBuiltin,
		ModelPath:       "plugins/ai-completion/models/qwen2.5-coder-0.5b.gguf",
		LlamaServerPath: "plugins/ai-completion/bin/llama-server",
		Endpoint:        "http://127.0.0.1:8989/v1/completions",
		Port:            8989,
		ModelName:       "qwen2.5-coder",
		DebounceMs:      150,
		MaxTokens:       32,
		Temperature:     0.1,
		Threads:         threads,
		ContextSize:     4096,
	}
}

// NormalizeProvider resolves shorthand aliases to standard provider keys.
func NormalizeProvider(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "builtin", "local", "llama", "llama-server", "llamacpp":
		return ProviderBuiltin
	case "ollama":
		return ProviderOllama
	case "openai":
		return ProviderOpenAI
	case "deepseek":
		return ProviderDeepSeek
	default:
		return ProviderCustom
	}
}
