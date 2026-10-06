package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/ai"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

// CodeActionHit tracks screen regions for interactive code block buttons.
type CodeActionHit struct {
	Action string // "apply", "insert", "copy"
	Code   string
	Lang   string
	X, Y   int
	W      int
}

// ChatPanel manages state and rendering for the AI Chat Assistant sidebar.
type ChatPanel struct {
	Engine        *ai.ChatEngine
	Messages      []ai.ChatMessage
	InputText     string
	InputCursor   int
	InputFocused  bool
	History       []string
	HistoryIdx    int
	IsGenerating  bool
	CancelFunc    context.CancelFunc
	ScrollOffset  int

	// Model Selection Menu
	ModelMenuOpen bool
	ModelMenuSel  int
	ActiveModel   string
	ModelPresets  []string

	// Clickable buttons in current frame
	CodeActionHits []CodeActionHit

	// Callbacks
	OnApplyCode  func(code string)
	OnInsertCode func(code string)
	OnCopyCode   func(code string)
	OnToast      func(level, title, msg string)
	OnClose      func()
}

// NewChatPanel creates a new AI Chat Assistant panel.
func NewChatPanel(engine *ai.ChatEngine) *ChatPanel {
	presets := []string{
		"Claude-3.7-Sonnet",
		"Claude-3.5-Sonnet",
		"GPT-4o",
		"GPT-4o-mini",
		"DeepSeek-V3",
		"Ollama: qwen2.5-coder:7b",
		"Ollama: llama3",
		"Local llama-server",
	}

	activeModel := "Claude-3.7-Sonnet"
	if engine != nil {
		cfg := engine.Config()
		if cfg.Model != "" {
			activeModel = cfg.Model
		}
	}

	return &ChatPanel{
		Engine:       engine,
		ActiveModel:  activeModel,
		ModelPresets: presets,
		History:      make([]string, 0, 32),
		HistoryIdx:   -1,
		Messages: []ai.ChatMessage{
			{
				Role:      "assistant",
				Content:   "Hello! I am your Tahr AI Assistant. Ask questions, explain code, generate unit tests, or refactor.",
				Timestamp: time.Now(),
			},
		},
	}
}

// SetActiveModel updates the selected model and syncs with engine config.
func (cp *ChatPanel) SetActiveModel(modelName string) {
	cp.ActiveModel = modelName
	if cp.Engine == nil {
		return
	}
	cfg := cp.Engine.Config()
	cfg.Model = modelName

	// Auto-configure provider based on preset name
	lower := strings.ToLower(modelName)
	switch {
	case strings.Contains(lower, "claude"):
		cfg.Provider = ai.ChatProviderAnthropic
		cfg.Endpoint = "https://api.anthropic.com/v1/messages"
	case strings.Contains(lower, "gpt"):
		cfg.Provider = ai.ChatProviderOpenAI
		cfg.Endpoint = "https://api.openai.com/v1/chat/completions"
	case strings.Contains(lower, "deepseek"):
		cfg.Provider = ai.ChatProviderDeepSeek
		cfg.Endpoint = "https://api.deepseek.com/v1/chat/completions"
	case strings.Contains(lower, "ollama"):
		cfg.Provider = ai.ChatProviderOllama
		cfg.Endpoint = "http://127.0.0.1:11434"
		parts := strings.Split(modelName, ":")
		if len(parts) > 1 {
			cfg.Model = strings.TrimSpace(parts[1])
		}
	case strings.Contains(lower, "llama-server") || strings.Contains(lower, "local"):
		cfg.Provider = ai.ChatProviderBuiltin
		cfg.Endpoint = "http://127.0.0.1:8989/v1/chat/completions"
	}

	cp.Engine.UpdateConfig(cfg)
}

// HandleKey processes keyboard input when ChatPanel or its input has focus.
func (cp *ChatPanel) HandleKey(k input.Key, activeDocPath, activeSelection, activeDiagnostics, activeTerminal string) bool {
	// 1. Model Dropdown intercepts keys
	if cp.ModelMenuOpen {
		switch k.Type {
		case input.KeyUp:
			if cp.ModelMenuSel > 0 {
				cp.ModelMenuSel--
			} else {
				cp.ModelMenuSel = len(cp.ModelPresets) - 1
			}
			return true
		case input.KeyDown:
			if cp.ModelMenuSel < len(cp.ModelPresets)-1 {
				cp.ModelMenuSel++
			} else {
				cp.ModelMenuSel = 0
			}
			return true
		case input.KeyEnter:
			if cp.ModelMenuSel >= 0 && cp.ModelMenuSel < len(cp.ModelPresets) {
				cp.SetActiveModel(cp.ModelPresets[cp.ModelMenuSel])
				if cp.OnToast != nil {
					cp.OnToast("info", "AI MODEL", "Switched to "+cp.ActiveModel)
				}
			}
			cp.ModelMenuOpen = false
			return true
		case input.KeyEsc:
			cp.ModelMenuOpen = false
			return true
		}
		return true
	}

	// 2. Alt+M toggles Model Menu
	if k.HasAlt() && (k.Rune == 'm' || k.Rune == 'M' || k.BaseKey == 'm') {
		cp.ModelMenuOpen = !cp.ModelMenuOpen
		return true
	}

	// 3. PageUp / PageDown scrolls message history
	if k.Type == input.KeyPgUp {
		cp.ScrollOffset += 5
		return true
	}
	if k.Type == input.KeyPgDown {
		if cp.ScrollOffset > 5 {
			cp.ScrollOffset -= 5
		} else {
			cp.ScrollOffset = 0
		}
		return true
	}

	// 4. In-sidebar text input handling
	if !cp.InputFocused {
		return false
	}

	switch k.Type {
	case input.KeyEsc:
		if cp.IsGenerating {
			if cp.CancelFunc != nil {
				cp.CancelFunc()
			}
			cp.IsGenerating = false
			if cp.OnToast != nil {
				cp.OnToast("warn", "AI", "Generation canceled")
			}
			return true
		}
		cp.InputFocused = false
		return true

	case input.KeyEnter:
		if k.HasShift() {
			// Insert newline
			runes := []rune(cp.InputText)
			pos := cp.InputCursor
			if pos > len(runes) {
				pos = len(runes)
			}
			cp.InputText = string(runes[:pos]) + "\n" + string(runes[pos:])
			cp.InputCursor = pos + 1
			return true
		}
		// Send prompt
		cp.SubmitPrompt(activeDocPath, activeSelection, activeDiagnostics, activeTerminal)
		return true

	case input.KeyUp:
		if len(cp.History) > 0 {
			if cp.HistoryIdx == -1 {
				cp.HistoryIdx = len(cp.History) - 1
			} else if cp.HistoryIdx > 0 {
				cp.HistoryIdx--
			}
			cp.InputText = cp.History[cp.HistoryIdx]
			cp.InputCursor = len([]rune(cp.InputText))
		}
		return true

	case input.KeyDown:
		if len(cp.History) > 0 && cp.HistoryIdx != -1 {
			if cp.HistoryIdx < len(cp.History)-1 {
				cp.HistoryIdx++
				cp.InputText = cp.History[cp.HistoryIdx]
			} else {
				cp.HistoryIdx = -1
				cp.InputText = ""
			}
			cp.InputCursor = len([]rune(cp.InputText))
		}
		return true

	case input.KeyLeft:
		if cp.InputCursor > 0 {
			cp.InputCursor--
		}
		return true

	case input.KeyRight:
		runes := []rune(cp.InputText)
		if cp.InputCursor < len(runes) {
			cp.InputCursor++
		}
		return true

	case input.KeyHome:
		cp.InputCursor = 0
		return true

	case input.KeyEnd:
		cp.InputCursor = len([]rune(cp.InputText))
		return true

	case input.KeyBackspace:
		runes := []rune(cp.InputText)
		if cp.InputCursor > 0 && len(runes) > 0 {
			cp.InputText = string(runes[:cp.InputCursor-1]) + string(runes[cp.InputCursor:])
			cp.InputCursor--
		}
		return true

	case input.KeyDelete:
		runes := []rune(cp.InputText)
		if cp.InputCursor < len(runes) {
			cp.InputText = string(runes[:cp.InputCursor]) + string(runes[cp.InputCursor+1:])
		}
		return true

	default:
		if k.Rune != 0 && !k.HasCtrl() && !k.HasAlt() {
			runes := []rune(cp.InputText)
			pos := cp.InputCursor
			if pos > len(runes) {
				pos = len(runes)
			}
			cp.InputText = string(runes[:pos]) + string(k.Rune) + string(runes[pos:])
			cp.InputCursor = pos + 1
			return true
		}
	}

	return true
}

// SubmitPrompt processes the current input, expands context tags, and begins streaming.
func (cp *ChatPanel) SubmitPrompt(activeDocPath, activeSelection, activeDiagnostics, activeTerminal string) {
	rawPrompt := strings.TrimSpace(cp.InputText)
	if rawPrompt == "" {
		return
	}

	// 1. Slash commands handling
	if strings.HasPrefix(rawPrompt, "/") {
		parts := strings.Fields(rawPrompt)
		cmd := strings.ToLower(parts[0])
		switch cmd {
		case "/clear":
			cp.Messages = nil
			cp.InputText = ""
			cp.InputCursor = 0
			if cp.OnToast != nil {
				cp.OnToast("info", "AI CHAT", "Cleared conversation history")
			}
			return
		case "/model":
			if len(parts) > 1 {
				modelArg := strings.Join(parts[1:], " ")
				cp.SetActiveModel(modelArg)
				if cp.OnToast != nil {
					cp.OnToast("info", "AI MODEL", "Switched to "+cp.ActiveModel)
				}
			} else {
				cp.ModelMenuOpen = true
			}
			cp.InputText = ""
			cp.InputCursor = 0
			return
		case "/explain":
			rawPrompt = "Explain this code and its logic:\n" + activeSelection
			if activeSelection == "" {
				rawPrompt = "Explain the active file and its primary functions."
			}
		case "/fix":
			rawPrompt = fmt.Sprintf("Analyze and fix these diagnostics in the code:\n%s\nCode context:\n%s", activeDiagnostics, activeSelection)
		case "/test":
			rawPrompt = "Generate comprehensive unit tests for this code:\n" + activeSelection
		}
	}

	// 2. Expand context tags
	expandedPrompt := rawPrompt
	if strings.Contains(expandedPrompt, "@file") {
		fileName := filepath.Base(activeDocPath)
		expandedPrompt = strings.ReplaceAll(expandedPrompt, "@file", fmt.Sprintf("[File: %s]", fileName))
	}
	if strings.Contains(expandedPrompt, "@selection") && activeSelection != "" {
		expandedPrompt = strings.ReplaceAll(expandedPrompt, "@selection", fmt.Sprintf("\n```\n%s\n```\n", activeSelection))
	}
	if strings.Contains(expandedPrompt, "@diagnostics") && activeDiagnostics != "" {
		expandedPrompt = strings.ReplaceAll(expandedPrompt, "@diagnostics", fmt.Sprintf("\n[Diagnostics:\n%s]\n", activeDiagnostics))
	}
	if strings.Contains(expandedPrompt, "@terminal") && activeTerminal != "" {
		expandedPrompt = strings.ReplaceAll(expandedPrompt, "@terminal", fmt.Sprintf("\n[Terminal Output:\n%s]\n", activeTerminal))
	}

	// Automatic context inclusion if selection is active
	if !strings.Contains(rawPrompt, "@selection") && activeSelection != "" && !strings.HasPrefix(rawPrompt, "/") {
		expandedPrompt += fmt.Sprintf("\n\n```\n%s\n```", activeSelection)
	}

	// Record history
	cp.History = append(cp.History, rawPrompt)
	cp.HistoryIdx = -1
	cp.InputText = ""
	cp.InputCursor = 0

	// Add User message
	userMsg := ai.ChatMessage{
		Role:      "user",
		Content:   expandedPrompt,
		Timestamp: time.Now(),
	}
	cp.Messages = append(cp.Messages, userMsg)

	// Add placeholder Assistant message for streaming
	assistantMsg := ai.ChatMessage{
		Role:      "assistant",
		Content:   "",
		Timestamp: time.Now(),
	}
	cp.Messages = append(cp.Messages, assistantMsg)
	asstIdx := len(cp.Messages) - 1

	if cp.Engine == nil {
		cp.Messages[asstIdx].Content = "AI engine not configured."
		return
	}

	// Start streaming
	ctx, cancel := context.WithCancel(context.Background())
	cp.CancelFunc = cancel
	cp.IsGenerating = true
	cp.ScrollOffset = 0 // Auto-scroll to bottom

	go func() {
		defer func() {
			cp.IsGenerating = false
		}()

		err := cp.Engine.StreamChat(ctx, cp.Messages[:asstIdx], func(token string) {
			cp.Messages[asstIdx].Content += token
			cp.ScrollOffset = 0
		}, func() {
			// Extract code blocks when streaming completes
			cp.Messages[asstIdx].CodeBlocks = ai.ExtractCodeBlocks(cp.Messages[asstIdx].Content)
		})

		if err != nil && err != context.Canceled {
			cp.Messages[asstIdx].Content += fmt.Sprintf("\n\n[Error: %v]", err)
			if cp.OnToast != nil {
				cp.OnToast("error", "AI", fmt.Sprintf("Error: %v", err))
			}
		}
	}()
}

// HandleClick processes mouse interactions with sidebar controls and code block action buttons.
func (cp *ChatPanel) HandleClick(x, y, startX, startY, sideW, sideH int) bool {
	// 1. Close button × at top right
	if y == startY && x >= startX+sideW-2 {
		if cp.OnClose != nil {
			cp.OnClose()
		}
		return true
	}

	// 2. Click on @model row
	if y == startY+2 && x >= startX && x < startX+sideW-4 {
		cp.ModelMenuOpen = !cp.ModelMenuOpen
		return true
	}

	// 3. Click inside Model Menu dropdown
	if cp.ModelMenuOpen {
		menuY := startY + 3
		menuH := len(cp.ModelPresets) + 2
		if y >= menuY && y < menuY+menuH && x >= startX+2 && x < startX+sideW-2 {
			selIdx := y - menuY - 1
			if selIdx >= 0 && selIdx < len(cp.ModelPresets) {
				cp.SetActiveModel(cp.ModelPresets[selIdx])
				if cp.OnToast != nil {
					cp.OnToast("info", "AI MODEL", "Switched to "+cp.ActiveModel)
				}
			}
			cp.ModelMenuOpen = false
			return true
		}
		cp.ModelMenuOpen = false
		return true
	}

	// 4. Click on Code Action badges [Apply] [Insert] [Copy]
	for _, hit := range cp.CodeActionHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.W {
			switch hit.Action {
			case "apply":
				if cp.OnApplyCode != nil {
					cp.OnApplyCode(hit.Code)
				}
			case "insert":
				if cp.OnInsertCode != nil {
					cp.OnInsertCode(hit.Code)
				}
			case "copy":
				if cp.OnCopyCode != nil {
					cp.OnCopyCode(hit.Code)
				}
			}
			return true
		}
	}

	// 5. Input Box focus
	promptY := startY + sideH - 4
	if y == promptY+1 && x >= startX && x < startX+sideW {
		cp.InputFocused = true
		return true
	}

	// 6. Button Bar at bottom
	if y == promptY+2 && x >= startX && x < startX+sideW {
		relX := x - startX
		if relX < 10 { // Send / Stop
			if cp.IsGenerating {
				if cp.CancelFunc != nil {
					cp.CancelFunc()
				}
				cp.IsGenerating = false
			} else {
				cp.InputFocused = true
			}
		} else if relX < 20 { // Clear
			cp.Messages = nil
			if cp.OnToast != nil {
				cp.OnToast("info", "AI CHAT", "Cleared conversation history")
			}
		} else { // Close
			if cp.OnClose != nil {
				cp.OnClose()
			}
		}
		return true
	}

	return false
}

// Render draws the entire chat panel into the Goatui buffer.
func (cp *ChatPanel) Render(buf *buffer.Buffer, startX, startY, sideW, sideH int, activeDocPath string, theme *ui.Theme) {
	cp.CodeActionHits = cp.CodeActionHits[:0]

	sidebarBg := toColor(theme.GutterBg)
	sidebarFg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	accentFg := toColor(theme.Function)

	// Clear sidebar background
	for y := startY; y < startY+sideH; y++ {
		for x := startX; x < startX+sideW; x++ {
			buf.SetRune(x, y, ' ', sidebarFg, sidebarBg, cell.AttrNone)
		}
	}

	// Helper for printing line with clipping
	printLine := func(y int, text string, fg cell.Color, attr cell.Modifier) {
		runes := []rune(text)
		for x := 0; x < len(runes) && startX+1+x < startX+sideW-1; x++ {
			buf.SetRune(startX+1+x, y, runes[x], fg, sidebarBg, attr)
		}
	}

	// Header: Title & Close Button
	title := i18n.T("ai.title")
	if title == "" {
		title = "AI Assistant"
	}
	titleRunes := []rune(title)
	btnSpace := 2
	if sideW >= 18 {
		btnSpace = 6 // "◀ ▶ × "
	}
	for i, r := range titleRunes {
		if i < sideW-btnSpace-1 {
			buf.SetRune(startX+1+i, startY, r, accentFg, sidebarBg, cell.AttrBold)
		}
	}
	if sideW >= 18 {
		btnFg := toColor(theme.Foreground)
		buf.SetRune(startX+sideW-6, startY, '◀', btnFg, sidebarBg, cell.AttrBold)
		buf.SetRune(startX+sideW-5, startY, ' ', btnFg, sidebarBg, cell.AttrNone)
		buf.SetRune(startX+sideW-4, startY, '▶', btnFg, sidebarBg, cell.AttrBold)
		buf.SetRune(startX+sideW-3, startY, ' ', btnFg, sidebarBg, cell.AttrNone)
	}
	buf.SetRune(startX+sideW-2, startY, '×', toColor(theme.DiagnosticError), sidebarBg, cell.AttrBold)
	buf.SetRune(startX+sideW-1, startY, ' ', toColor(theme.DiagnosticError), sidebarBg, cell.AttrNone)

	// Context row: @file
	docName := "none"
	if activeDocPath != "" {
		docName = filepath.Base(activeDocPath)
	}
	printLine(startY+1, fmt.Sprintf("@file: %s", docName), toColor(theme.DiagnosticInfo), cell.AttrNone)

	// Context row: @model (Interactive selector)
	modelLabel := fmt.Sprintf("@model: %s ▼", cp.ActiveModel)
	printLine(startY+2, modelLabel, toColor(theme.String), cell.AttrBold)

	// Divider
	for x := startX; x < startX+sideW; x++ {
		buf.SetRune(x, startY+3, '┄', borderFg, sidebarBg, cell.AttrNone)
	}

	msgStartY := startY + 4
	inputAreaH := 4
	availH := sideH - 4 - inputAreaH

	// Render messages or Welcome hints
	if len(cp.Messages) == 0 {
		hints := []string{
			"Ask questions, explain code,",
			"generate tests, or refactor.",
			"",
			"Quick Commands:",
			" • /explain  Explain selection",
			" • /fix      Fix diagnostics",
			" • /test     Generate unit tests",
			" • /clear    Clear chat history",
			" • /model    Switch AI model",
			"",
			"Context Tags:",
			" • @file     Current file",
			" • @selection Active snippet",
			" • @diagnostics Active errors",
			" • @terminal Last shell output",
			"",
			"Shortcuts:",
			" • Ctrl+L    Focus input",
			" • Alt+M     Switch model",
			" • Esc       Exit / Cancel",
		}
		for i, h := range hints {
			if i < availH {
				fg := sidebarFg
				attr := cell.AttrNone
				if strings.HasPrefix(h, "Quick") || strings.HasPrefix(h, "Context") || strings.HasPrefix(h, "Shortcuts") {
					fg = toColor(theme.Keyword)
					attr = cell.AttrBold
				} else if strings.HasPrefix(h, " •") {
					fg = toColor(theme.Function)
				}
				printLine(msgStartY+i, h, fg, attr)
			}
		}
	} else {
		// Collect visual lines from all messages
		type renderedRow struct {
			text     string
			fg       cell.Color
			attr     cell.Modifier
			isAction bool
			action   string
			code     string
		}

		rows := make([]renderedRow, 0, 128)
		wrapW := sideW - 3
		if wrapW < 10 {
			wrapW = 10
		}

		for _, m := range cp.Messages {
			prefix := "You: "
			pfg := toColor(theme.Keyword)
			if m.Role != "user" {
				prefix = "AI: "
				pfg = toColor(theme.String)
			}

			// Header line of message
			rows = append(rows, renderedRow{text: prefix, fg: pfg, attr: cell.AttrBold})

			// Content lines word-wrapped
			rawLines := strings.Split(m.Content, "\n")
			inCodeBlock := false
			curCodeLang := ""
			curCodeLines := make([]string, 0, 16)

			for _, l := range rawLines {
				if strings.HasPrefix(strings.TrimSpace(l), "```") {
					if !inCodeBlock {
						inCodeBlock = true
						curCodeLang = strings.TrimPrefix(strings.TrimSpace(l), "```")
						curCodeLines = curCodeLines[:0]
						rows = append(rows, renderedRow{
							text: fmt.Sprintf("┌── %s ──────────", curCodeLang),
							fg:   toColor(theme.Comment),
							attr: cell.AttrNone,
						})
					} else {
						inCodeBlock = false
						fullCode := strings.Join(curCodeLines, "\n")
						rows = append(rows, renderedRow{
							text:     "└──  Apply   Insert   Copy ",
							fg:       toColor(theme.Function),
							attr:     cell.AttrBold,
							isAction: true,
							code:     fullCode,
						})
					}
					continue
				}

				if inCodeBlock {
					curCodeLines = append(curCodeLines, l)
					rows = append(rows, renderedRow{
						text: "│ " + l,
						fg:   toColor(theme.Foreground),
						attr: cell.AttrNone,
					})
				} else {
					// Word-wrap plain message text
					rText := []rune(l)
					for len(rText) > 0 {
						chunkLen := min(len(rText), wrapW)
						rows = append(rows, renderedRow{
							text: string(rText[:chunkLen]),
							fg:   sidebarFg,
							attr: cell.AttrNone,
						})
						rText = rText[chunkLen:]
					}
				}
			}
			rows = append(rows, renderedRow{text: "", fg: sidebarFg, attr: cell.AttrNone})
		}

		if cp.IsGenerating {
			rows = append(rows, renderedRow{
				text: "AI: Generating response █",
				fg:   toColor(theme.DiagnosticWarn),
				attr: cell.AttrBold,
			})
		}

		// Calculate scrolling offset
		totalRows := len(rows)
		startRow := 0
		if totalRows > availH {
			startRow = totalRows - availH - cp.ScrollOffset
			if startRow < 0 {
				startRow = 0
			}
		}

		// Draw visible rows
		for i := 0; i < availH && startRow+i < totalRows; i++ {
			row := rows[startRow+i]
			y := msgStartY + i
			printLine(y, row.text, row.fg, row.attr)

			if row.isAction {
				// Register click hit regions for buttons
				applyIdx := strings.Index(row.text, " Apply ")
				insertIdx := strings.Index(row.text, " Insert ")
				copyIdx := strings.Index(row.text, " Copy ")

				if applyIdx != -1 {
					cp.CodeActionHits = append(cp.CodeActionHits, CodeActionHit{
						Action: "apply",
						Code:   row.code,
						X:      startX + 1 + applyIdx,
						Y:      y,
						W:      7,
					})
				}
				if insertIdx != -1 {
					cp.CodeActionHits = append(cp.CodeActionHits, CodeActionHit{
						Action: "insert",
						Code:   row.code,
						X:      startX + 1 + insertIdx,
						Y:      y,
						W:      8,
					})
				}
				if copyIdx != -1 {
					cp.CodeActionHits = append(cp.CodeActionHits, CodeActionHit{
						Action: "copy",
						Code:   row.code,
						X:      startX + 1 + copyIdx,
						Y:      y,
						W:      6,
					})
				}
			}
		}
	}

	// Bottom Input Area
	promptY := startY + sideH - 4
	for x := startX; x < startX+sideW; x++ {
		buf.SetRune(x, promptY, '─', borderFg, sidebarBg, cell.AttrNone)
	}

	inputBoxBg := toColor(theme.Background)
	for x := startX + 1; x < startX+sideW-1; x++ {
		buf.SetRune(x, promptY+1, ' ', sidebarFg, inputBoxBg, cell.AttrNone)
	}

	if cp.InputFocused {
		inputRunes := []rune(cp.InputText)
		cursorPos := cp.InputCursor
		if cursorPos > len(inputRunes) {
			cursorPos = len(inputRunes)
		}

		// Render active input text
		for i, r := range inputRunes {
			if startX+2+i < startX+sideW-2 {
				buf.SetRune(startX+2+i, promptY+1, r, toColor(theme.Foreground), inputBoxBg, cell.AttrNone)
			}
		}

		// Render cursor block
		cursorX := startX + 2 + cursorPos
		if cursorX < startX+sideW-2 {
			cursorRune := '█'
			if cursorPos < len(inputRunes) {
				cursorRune = inputRunes[cursorPos]
			}
			buf.SetRune(cursorX, promptY+1, cursorRune, toColor(theme.Background), toColor(theme.Function), cell.AttrBold)
		}
	} else {
		placeholder := "> Ask AI... (Ctrl+L)"
		for i, r := range []rune(placeholder) {
			if startX+2+i < startX+sideW-2 {
				buf.SetRune(startX+2+i, promptY+1, r, toColor(theme.Comment), inputBoxBg, cell.AttrNone)
			}
		}
	}

	// Button Bar at bottom
	btnLine := "Send ↵    Clear    Close"
	if cp.IsGenerating {
		btnLine = "Stop ■    Clear    Close"
	}
	printLine(promptY+2, btnLine, toColor(theme.Function), cell.AttrNone)

	// Render Model Selector Popup if open
	if cp.ModelMenuOpen {
		cp.renderModelDropdown(buf, startX+2, startY+3, sideW-4, theme)
	}
}

func (cp *ChatPanel) renderModelDropdown(buf *buffer.Buffer, posX, posY, menuW int, theme *ui.Theme) {
	bg := toColor(theme.PopupBg)
	fg := toColor(theme.PopupFg)
	selBg := toColor(theme.PopupSelBg)
	borderFg := toColor(theme.Function)

	menuH := len(cp.ModelPresets) + 2

	// Fill background
	for y := 0; y < menuH; y++ {
		for x := 0; x < menuW; x++ {
			buf.SetRune(posX+x, posY+y, ' ', fg, bg, cell.AttrNone)
		}
	}

	// Border
	box := DefaultBoxChars()
	buf.SetRune(posX, posY, box.TopLeft, borderFg, bg, cell.AttrNone)
	buf.SetRune(posX+menuW-1, posY, box.TopRight, borderFg, bg, cell.AttrNone)
	buf.SetRune(posX, posY+menuH-1, box.BottomLeft, borderFg, bg, cell.AttrNone)
	buf.SetRune(posX+menuW-1, posY+menuH-1, box.BottomRight, borderFg, bg, cell.AttrNone)
	for x := 1; x < menuW-1; x++ {
		buf.SetRune(posX+x, posY, box.Horiz, borderFg, bg, cell.AttrNone)
		buf.SetRune(posX+x, posY+menuH-1, box.Horiz, borderFg, bg, cell.AttrNone)
	}
	for y := 1; y < menuH-1; y++ {
		buf.SetRune(posX, posY+y, box.Vert, borderFg, bg, cell.AttrNone)
		buf.SetRune(posX+menuW-1, posY+y, box.Vert, borderFg, bg, cell.AttrNone)
	}

	// Title in top border
	title := " Select Model "
	for i, r := range []rune(title) {
		if posX+2+i < posX+menuW-2 {
			buf.SetRune(posX+2+i, posY, r, borderFg, bg, cell.AttrBold)
		}
	}

	// Model options
	for i, opt := range cp.ModelPresets {
		isSel := i == cp.ModelMenuSel
		isActive := opt == cp.ActiveModel || strings.Contains(opt, cp.ActiveModel)

		cBg := bg
		cFg := fg
		attr := cell.AttrNone

		if isSel {
			cBg = selBg
			cFg = toColor(theme.Foreground)
			attr = cell.AttrBold
		}

		line := "  " + opt
		if isActive {
			line = "● " + opt
		}
		if isSel {
			line = "> " + opt
		}

		runes := []rune(line)
		for c := 0; c < len(runes) && c < menuW-2; c++ {
			buf.SetRune(posX+1+c, posY+1+i, runes[c], cFg, cBg, attr)
		}
	}
}
