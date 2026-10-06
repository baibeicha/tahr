package tui

import (
	"context"
	"flag"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"
	"time"
	"unicode"


	"tahr/internal/core/ai"
)

// updateGhostText calculates the inline faint prediction for the word being typed or AI shadow completion.
func (m *AppModel) updateGhostText() {
	prefix := m.wordPrefixUnderCursor()
	if len(prefix) >= 2 {
		candidates := m.extractCompletionSymbols()
		for _, c := range candidates {
			cand := c
			if idx := strings.Index(cand, " - "); idx != -1 {
				cand = cand[:idx]
			}
			cand = strings.TrimSpace(cand)
			matchTarget := cand
			if strings.Contains(cand, ".") {
				parts := strings.Split(cand, ".")
				matchTarget = parts[len(parts)-1]
			}
			if strings.HasPrefix(strings.ToLower(matchTarget), strings.ToLower(prefix)) && len(matchTarget) > len(prefix) {
				m.ghostText = matchTarget[len(prefix):]
				m.ghostPrefix = prefix
				m.cancelPendingAI()
				return
			}
		}
	}

	m.ghostText = ""
	m.ghostPrefix = ""

	// If no local match, trigger debounced AI shadow completion
	m.triggerAICompletion(false /* multiline */)
}

// cancelPendingAI aborts active debounce timers and in-flight completion requests.
func (m *AppModel) cancelPendingAI() {
	m.aiMu.Lock()
	defer m.aiMu.Unlock()
	if m.aiCancel != nil {
		m.aiCancel()
		m.aiCancel = nil
	}
	if m.aiDebounceTimer != nil {
		m.aiDebounceTimer.Stop()
		m.aiDebounceTimer = nil
	}
}

// dismissGhostText clears ghost text and cancels running AI completion requests.
func (m *AppModel) dismissGhostText() {
	m.ghostText = ""
	m.ghostPrefix = ""
	m.cancelPendingAI()
}

// triggerAIAutoDownload launches background download of missing model weights or server binary.
func (m *AppModel) triggerAIAutoDownload() bool {
	if flag.Lookup("test.v") != nil || os.Getenv("TAHR_DISABLE_AI_DOWNLOAD") == "1" {
		return false
	}
	if m.aiEngine == nil || m.pluginMgr == nil || !m.pluginMgr.IsEnabled("ai-completion") {
		return false
	}
	return m.aiEngine.CheckAndAutoDownload(m.workspaceDir,
		func() {
			m.toasts.Info("AI", "Downloading AI model in background (~390 MB)...")
		},
		func() {
			m.toasts.Success("AI", "AI model downloaded and ready.")
		},
		func(err error) {
			m.toasts.Error("AI", fmt.Sprintf("AI download failed: %v", err))
		},
	)
}

// triggerAICompletion starts debounced AI completion in background.
func (m *AppModel) triggerAICompletion(multiline bool) {
	if m.aiEngine == nil {
		return
	}
	if m.aiEngine.IsDownloading() {
		return
	}
	cfg := m.aiEngine.Config()
	if !cfg.Enabled {
		return
	}

	m.cancelPendingAI()

	delay := time.Duration(cfg.DebounceMs) * time.Millisecond
	if delay <= 0 {
		delay = 150 * time.Millisecond
	}
	if multiline {
		delay = 10 * time.Millisecond
	}

	m.aiMu.Lock()
	m.aiDebounceTimer = time.AfterFunc(delay, func() {
		m.requestAICompletion(multiline)
	})
	m.aiMu.Unlock()
}

// requestAICompletion fetches completion from AI engine and dispatches message to TEA event loop.
func (m *AppModel) requestAICompletion(multiline bool) {
	if m.aiEngine == nil || m.program == nil {
		return
	}
	prefix, suffix, curLine, curCol, docID := m.getCursorPrefixAndSuffix()
	if docID == "" || (prefix == "" && suffix == "") {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	m.aiMu.Lock()
	m.aiCancel = cancel
	m.aiMu.Unlock()

	defer func() {
		m.aiMu.Lock()
		m.aiCancel = nil
		m.aiMu.Unlock()
	}()

	completion, err := m.aiEngine.RequestCompletion(ctx, prefix, suffix, multiline)
	if err != nil {
		if multiline {
			m.toasts.Error("AI", fmt.Sprintf("AI Completion error: %v", err))
		}
		return
	}
	if completion == "" {
		if multiline {
			m.toasts.Info("AI", "No suggestion available for current position")
		}
		return
	}

	m.program.Send(aiCompletionMsg{
		docID:      docID,
		cursorLine: curLine,
		cursorCol:  curCol,
		completion: completion,
	})
}

// getCursorPrefixAndSuffix extracts the text before and after the cursor for FIM prompting.
func (m *AppModel) getCursorPrefixAndSuffix() (prefix, suffix string, line, col int, docID string) {
	doc := m.eng.ActiveDocument()
	if doc == nil || doc.Buffer == nil {
		return "", "", 0, 0, ""
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return "", "", 0, 0, doc.ID
	}
	curLine, curCol := sels[0].Head.Line, sels[0].Head.Column

	text, err := doc.Buffer.GetText()
	if err != nil {
		return "", "", curLine, curCol, doc.ID
	}
	offset, err := doc.Buffer.ByteOffsetForLine(curLine)
	if err != nil {
		return "", "", curLine, curCol, doc.ID
	}
	lineBytes, _ := doc.Buffer.GetLine(curLine)
	lineRunes := []rune(string(lineBytes))
	colBytes := 0
	for i := 0; i < curCol && i < len(lineRunes); i++ {
		colBytes += len(string(lineRunes[i]))
	}
	cursorByteOffset := offset + colBytes
	if cursorByteOffset < 0 {
		cursorByteOffset = 0
	}
	if cursorByteOffset > len(text) {
		cursorByteOffset = len(text)
	}

	prefix = string(text[:cursorByteOffset])
	suffix = string(text[cursorByteOffset:])
	return prefix, suffix, curLine, curCol, doc.ID
}

// getAIConfig builds an ai.Config merged with user settings and active plugin status.
func (m *AppModel) getAIConfig() ai.Config {
	cfg := ai.DefaultConfig()
	if m.pluginMgr != nil {
		cfg.Enabled = m.pluginMgr.IsEnabled("ai-completion")
	}
	if m.settings != nil && m.settings.Current.PluginSettings != nil {
		if pSettings, ok := m.settings.Current.PluginSettings["ai-completion"]; ok {
			if v, ok := pSettings["provider"].(string); ok && v != "" {
				cfg.Provider = v
			}
			if v, ok := pSettings["endpoint"].(string); ok && v != "" {
				cfg.Endpoint = v
			}
			if v, ok := pSettings["port"]; ok {
				switch val := v.(type) {
				case float64:
					cfg.Port = int(val)
				case int:
					cfg.Port = val
				}
			}
			if v, ok := pSettings["model"].(string); ok && v != "" {
				cfg.ModelName = v
			}
			if v, ok := pSettings["model_path"].(string); ok && v != "" {
				cfg.ModelPath = v
			}
			if v, ok := pSettings["llama_server_path"].(string); ok && v != "" {
				cfg.LlamaServerPath = v
			}
			if v, ok := pSettings["api_key"].(string); ok {
				cfg.APIKey = v
			}
			if v, ok := pSettings["debounce_ms"]; ok {
				switch val := v.(type) {
				case float64:
					cfg.DebounceMs = int(val)
				case int:
					cfg.DebounceMs = val
				}
			}
			if v, ok := pSettings["max_tokens"]; ok {
				switch val := v.(type) {
				case float64:
					cfg.MaxTokens = int(val)
				case int:
					cfg.MaxTokens = val
				}
			}
		}
	}
	return cfg
}

// getAIChatConfig builds an ai.ChatConfig merged with user settings and active plugin status.
func (m *AppModel) getAIChatConfig() ai.ChatConfig {
	cfg := ai.DefaultChatConfig()
	if m.settings != nil && m.settings.Current.PluginSettings != nil {
		if pSettings, ok := m.settings.Current.PluginSettings["ai-chat"]; ok {
			if v, ok := pSettings["provider"].(string); ok && v != "" {
				cfg.Provider = v
			}
			if v, ok := pSettings["endpoint"].(string); ok && v != "" {
				cfg.Endpoint = v
			}
			if v, ok := pSettings["model"].(string); ok && v != "" {
				cfg.Model = v
			}
			if v, ok := pSettings["api_key"].(string); ok && v != "" {
				cfg.APIKey = v
			}
			if v, ok := pSettings["temperature"]; ok {
				switch val := v.(type) {
				case float64:
					cfg.Temperature = val
				case int:
					cfg.Temperature = float64(val)
				}
			}
		}
	}
	return cfg
}

// extractBufferWords extracts identifiers from the active document for contextual completion.
func (m *AppModel) extractBufferWords() []string {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return nil
	}
	seen := make(map[string]bool)
	var words []string

	total := doc.Buffer.TotalLines()
	for line := 0; line < total && line < 1000; line++ {
		lineBytes, _ := doc.Buffer.GetLine(line)
		runes := []rune(string(lineBytes))
		i := 0
		for i < len(runes) {
			if unicode.IsLetter(runes[i]) || runes[i] == '_' {
				start := i
				for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
					i++
				}
				w := string(runes[start:i])
				if len(w) >= 3 && !seen[w] {
					seen[w] = true
					words = append(words, w)
					if len(words) >= 50 {
						return words
					}
				}
			} else {
				i++
			}
		}
	}
	return words
}

