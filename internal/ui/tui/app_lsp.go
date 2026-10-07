package tui

import (
	"encoding/json"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core"
	corebuf "tahr/internal/core/buffer"
	"tahr/internal/core/gogen"
	"tahr/internal/core/lsp"
	"tahr/internal/core/plugin"
)

type lspDiagnosticsMsg struct {
	URI string
}

// EnsureLSPForFile autodetects, spawns and initializes LSP for filePath.
func (m *AppModel) EnsureLSPForFile(filePath string) {
	if filePath == "" {
		return
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	var lspCmd string
	var lspArgs []string
	var toolName, pluginName, installCmd string
	var customPath string

	// 1. Check if an active enabled plugin defines an LSP for this extension
	if m.pluginMgr != nil {
		ws := m.workspaceDir
		m.pluginMgr.SetWorkspaceRoot(ws)


		if lspCfg := m.pluginMgr.GetLSPForExt(ext); lspCfg != nil && lspCfg.Command != "" {
			toolName = lspCfg.Command
			pluginName = lspCfg.ServerName
			if pluginName == "" {
				pluginName = toolName
			}
			installCmd = lspCfg.InstallCmd
			lspArgs = lspCfg.Args
		} else {
			// No enabled plugin provides an LSP for this extension.
			// Treat file as plain text without starting any LSP server.
			return
		}
	} else {
		// 2. Standalone fallback (only when no plugin manager is attached, e.g. minimal test)
		switch ext {
		case ".go":
			toolName = "gopls"
			pluginName = "Go Language Support"
			installCmd = "go install golang.org/x/tools/gopls@latest"
		case ".py":
			toolName = "pyright-langserver"
			pluginName = "Python Language Support"
			installCmd = "pip install pyright || npm install -g pyright"
			lspArgs = []string{"--stdio"}
		case ".rs":
			toolName = "rust-analyzer"
			pluginName = "Rust Language Support"
			installCmd = "rustup component add rust-analyzer"
		}
	}

	// 3. Resolve customPath now that toolName is known
	if m.settings != nil {
		if m.settings.Current.CustomToolPaths != nil && toolName != "" {
			customPath = m.settings.Current.CustomToolPaths[toolName]
		}
		if customPath == "" {
			switch ext {
			case ".go":
				if m.settings.Current.GoplsPath != "" {
					customPath = m.settings.Current.GoplsPath
				} else if m.settings.Current.GoSDKPath != "" {
					customPath = m.settings.Current.GoSDKPath
				}
			case ".py":
				if m.settings.Current.PyrightPath != "" {
					customPath = m.settings.Current.PyrightPath
				}
			case ".rs":
				if m.settings.Current.RustAnalyzer != "" {
					customPath = m.settings.Current.RustAnalyzer
				}
			}
		}
	}

	// 4. Locate executable
	if toolName != "" {
		if m.pluginMgr != nil {
			lspCmd, _ = m.pluginMgr.FindToolPath(toolName, customPath)
			if lspCmd == "" && ext == ".py" {
				lspCmd, _ = m.pluginMgr.FindToolPath("pylsp", customPath)
			}
		} else {
			if customPath != "" {
				if fi, err := os.Stat(customPath); err == nil && !fi.IsDir() {
					lspCmd = customPath
				}
			}
			if lspCmd == "" {
				if path, err := exec.LookPath(toolName); err == nil {
					lspCmd = path
				} else if ext == ".py" {
					if path, err := exec.LookPath("pylsp"); err == nil {
						lspCmd = path
					}
				}
			}
		}
	}

	// 5. Prompt user if tool is missing
	if lspCmd == "" && toolName != "" {
		if m.promptedTools == nil {
			m.promptedTools = make(map[string]bool)
		}
		if !m.promptedTools[toolName] {
			m.promptedTools[toolName] = true
			if m.toolPrompt != nil && !m.toolPrompt.Open {
				bestInstallCmd, fallbackCmds := plugin.ResolveSmartInstallCommand(installCmd, toolName, m.pluginMgr)
				m.toolPrompt.OpenForTool(
				toolName,
				pluginName,
				bestInstallCmd,
				ext,
				func() {
					if m.terminal != nil {
						m.terminal.Open = true
						m.terminal.Execute(bestInstallCmd)
					}
					go func() {
						runCmd := func(cmdStr string) error {
							parts := strings.Fields(cmdStr)
							if len(parts) == 0 {
								return fmt.Errorf("empty command")
							}
							bin := parts[0]
							if m.pluginMgr != nil {
								if found, ok := m.pluginMgr.FindToolPath(bin, ""); ok {
									bin = found
								}
							}
							return exec.Command(bin, parts[1:]...).Run()
						}
						err := runCmd(bestInstallCmd)
						if err != nil && len(fallbackCmds) > 0 {
							_ = runCmd(fallbackCmds[0])
						}
						time.Sleep(500 * time.Millisecond)
						m.EnsureLSPForFile(filePath)
					}()
					m.toasts.Info("INSTALL", fmt.Sprintf("Installing %s via %s", toolName, bestInstallCmd))
				},
				func(cp string) {
					if m.settings != nil {
						if m.settings.Current.CustomToolPaths == nil {
							m.settings.Current.CustomToolPaths = make(map[string]string)
						}
						m.settings.Current.CustomToolPaths[toolName] = cp
						switch toolName {
						case "gopls":
							m.settings.Current.GoplsPath = cp
						case "pyright-langserver", "pyright":
							m.settings.Current.PyrightPath = cp
						case "rust-analyzer":
							m.settings.Current.RustAnalyzer = cp
						}
						_ = m.settings.Save()
					}
					m.EnsureLSPForFile(filePath)
				},
			)
			}
		}
		return
	}

	if lspCmd == "" {
		return
	}

	if m.lspClient == nil || m.lspCurrentCmd != lspCmd {
		if m.lspClient != nil {
			_ = m.lspClient.Close()
			m.lspClient = nil
		}
		client, err := lsp.StartClient(lspCmd, lspArgs, m)
		if err == nil {
			m.lspClient = client
			m.lspCurrentCmd = lspCmd
			rootURI := "file:///" + filepath.ToSlash(m.workspaceDir)
			_ = client.Initialize(rootURI)
		}
	}

	if m.lspClient != nil {
		doc := m.eng.ActiveDocument()
		if doc != nil && doc.FilePath != "" {
			uri := "file:///" + filepath.ToSlash(doc.FilePath)
			text, _ := doc.Buffer.GetText()
			langID := strings.TrimPrefix(ext, ".")
			m.lspDocVersion = 1
			_ = m.lspClient.DidOpen(uri, langID, string(text), 1)
			m.refreshDocumentStructure()
			m.syncActiveDiagnostics()
		}
	}
}

// refreshDocumentStructure updates document structure symbols using LSP or regex fallback.
func (m *AppModel) refreshDocumentStructure() {
	if m.structurePanel == nil {
		return
	}
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	ext := filepath.Ext(doc.FilePath)
	text, _ := doc.Buffer.GetText()

	// 1. Instantly populate structure using fast regex (0ms turnaround) if language plugin is enabled
	var items []StructureItem
	if m.pluginMgr == nil || (ext != "" && m.pluginMgr.IsExtensionActive(ext)) {
		items = ExtractSymbolsRegex(ext, string(text))
	}
	m.structurePanel.SetItems(items)

	// 2. Query richer LSP document symbols in the background without blocking the UI thread
	if m.lspClient != nil && doc.FilePath != "" {
		uri := "file:///" + filepath.ToSlash(doc.FilePath)
		go func(client *lsp.Client, targetURI string, panel *StructurePanel, localItems []StructureItem) {
			symbols, err := client.DocumentSymbols(targetURI)
			if err == nil && len(symbols) > 0 {
				lspItems := ConvertLSPSymbols(symbols)
				merged := MergeSymbols(lspItems, localItems)
				panel.SetItems(merged)
			}
		}(m.lspClient, uri, m.structurePanel, items)
	}
}

// onActiveDocumentChanged ensures LSP tracking, active diagnostics, and structure panel are synchronized.
func (m *AppModel) onActiveDocumentChanged() {
	if m.eng == nil {
		return
	}
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	if doc.FilePath != "" {
		m.EnsureLSPForFile(doc.FilePath)
	}
	m.syncActiveDiagnostics()
	m.refreshDocumentStructure()
}

// jumpToLine navigates cursor to the specified line and column in the active document.
func (m *AppModel) jumpToLine(line, col int) {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	totalLines := doc.Buffer.TotalLines()
	if line < 0 {
		line = 0
	}
	if line >= totalLines {
		line = max(0, totalLines-1)
	}
	off, _ := doc.Buffer.ByteOffsetForLine(line)
	pos := corebuf.Position{Line: line, Column: col, Byte: off + col}
	doc.Buffer.SetSelections([]corebuf.Selection{
		corebuf.NewCursor(pos),
	})
	m.sidebarFocused = false

	// Center target line in viewport
	editorHeight := m.height - 2
	if m.outputOpen {
		editorHeight -= m.outputHeight
	}
	if editorHeight < 1 {
		editorHeight = 1
	}
	m.viewportY = max(0, line-editorHeight/2)
	m.ensureCursorVisible()
	m.syncViewportOffsets()
}

// extractCompletionSymbols provides rich multi-file and standard library symbols when LSP is unavailable.
func (m *AppModel) extractCompletionSymbols() []string {
	seen := make(map[string]bool)
	var symbols []string

	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			symbols = append(symbols, s)
		}
	}

	doc := m.eng.ActiveDocument()
	ext := ""
	if doc != nil {
		ext = strings.ToLower(filepath.Ext(doc.FilePath))
	}

	// 1. In plain text mode or when the language plugin is disabled:
	// ONLY provide active buffer words (zero language-specific builtins, keywords, or stdlib).
	if m.pluginMgr != nil && (ext == "" || !m.pluginMgr.IsExtensionActive(ext)) {
		for _, w := range m.extractBufferWords() {
			add(w)
		}
		return symbols
	}

	// 2. Language standard library and built-ins (only when language plugin is active)
	switch ext {
	case ".go":
		goBuiltins := []string{
			"fmt.Println", "fmt.Printf", "fmt.Sprintf", "fmt.Errorf",
			"os.Open", "os.Create", "os.ReadFile", "os.WriteFile", "os.Stat", "os.MkdirAll", "os.Exit",
			"io.ReadAll", "io.Copy", "io.Reader", "io.Writer", "io.Closer",
			"strings.Contains", "strings.HasPrefix", "strings.HasSuffix", "strings.Split", "strings.Join", "strings.TrimSpace", "strings.ReplaceAll",
			"filepath.Join", "filepath.Base", "filepath.Dir", "filepath.Clean", "filepath.Abs", "filepath.Ext",
			"json.Marshal", "json.Unmarshal", "json.NewDecoder", "json.NewEncoder",
			"sync.Mutex", "sync.RWMutex", "sync.WaitGroup", "sync.Once",
			"context.Background", "context.TODO", "context.WithTimeout", "context.WithCancel",
			"time.Sleep", "time.Now", "time.Since", "time.Duration", "time.Second",
			"errors.New", "errors.Is", "errors.As",
			"make", "new", "append", "copy", "delete", "len", "cap", "panic", "recover",
			"func", "return", "package", "import", "type", "struct", "interface",
			"if", "else", "switch", "case", "default", "for", "range", "nil", "true", "false",
		}
		for _, b := range goBuiltins {
			add(b)
		}
	case ".py":
		pyBuiltins := []string{
			"print", "len", "range", "enumerate", "isinstance", "open", "zip", "map", "filter",
			"str", "int", "float", "bool", "list", "dict", "set", "tuple",
			"os.path.join", "os.path.exists", "os.path.basename", "os.path.dirname",
			"sys.exit", "sys.argv", "sys.path",
			"json.dumps", "json.loads", "json.dump", "json.load",
			"def", "return", "import", "from", "class", "if", "elif", "else",
			"for", "while", "try", "except", "finally", "with", "as", "None", "True", "False",
		}
		for _, b := range pyBuiltins {
			add(b)
		}
	}

	// 3. Active buffer words
	for _, w := range m.extractBufferWords() {
		add(w)
	}


	// 3. Scan declarations from other project files
	for i, relPath := range m.allProjectFiles {
		if i >= 15 {
			break
		}
		fullPath := filepath.Join(m.workspaceDir, relPath)
		if doc != nil && fullPath == doc.FilePath {
			continue
		}
		if data, err := os.ReadFile(fullPath); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "func ") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						fnName := strings.Split(parts[1], "(")[0]
						add(fnName)
					}
				} else if strings.HasPrefix(line, "type ") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						add(parts[1])
					}
				} else if strings.HasPrefix(line, "def ") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						defName := strings.Split(parts[1], "(")[0]
						add(defName)
					}
				}
			}
		}
	}

	return symbols
}

// SetLSPClient attaches an active language server client to the TUI.
func (m *AppModel) SetLSPClient(client *lsp.Client) {
	m.lspClient = client
}

// PluginManager returns the attached plugin manager, or nil if none.
func (m *AppModel) PluginManager() *plugin.Manager {
	return m.pluginMgr
}

// SetPluginManager attaches the plugin manager for language configurations.
func (m *AppModel) SetPluginManager(mgr *plugin.Manager) {
	m.pluginMgr = mgr
	if m.launchModal != nil && mgr != nil {
		m.launchModal.SetSupportedTypes(mgr.GetSupportedLaunchTypes())
	}
	if m.marketplace != nil {
		m.marketplace.mgr = mgr
	} else {
		m.marketplace = NewMarketplaceModal(mgr)
	}
	if m.settings != nil {
		m.settings.SetPluginManager(mgr)
	}
	if mgr != nil {
		mgr.SetDynamicSDKProvider(func(sdkID string) string {
			if sdkID == "go" && m.sdkManager != nil {
				return m.sdkManager.GoVersionShort()
			}
			return ""
		})
		mgr.AddLifecycleListener(func(pluginID string, enabled bool) {
			m.onPluginLifecycleChanged(pluginID, enabled)
		})
	}
	if m.aiEngine != nil {
		aiCfg := m.getAIConfig()
		m.aiEngine.UpdateConfig(aiCfg)
		if aiCfg.Enabled {
			go func() { _ = m.aiEngine.Start() }()
		}
	}
	m.applyCurrentSettings()
}

func (m *AppModel) onPluginLifecycleChanged(pluginID string, enabled bool) {
	if pluginID == "ai-completion" {
		if m.aiEngine != nil {
			aiCfg := m.getAIConfig()
			aiCfg.Enabled = enabled
			m.aiEngine.UpdateConfig(aiCfg)
			if enabled {
				go func() { _ = m.aiEngine.Start() }()
			} else {
				m.aiEngine.Stop()
				m.dismissGhostText()
			}
		}
	}
	if !enabled {
		// A plugin was disabled: if active document's language is no longer active,
		// terminate running LSP, clear diagnostics and revert to plain text.
		doc := m.eng.ActiveDocument()
		if doc != nil {
			ext := filepath.Ext(doc.FilePath)
			if m.pluginMgr != nil && !m.pluginMgr.IsExtensionActive(ext) {
				if m.lspClient != nil {
					_ = m.lspClient.Close()
					m.lspClient = nil
				}
				m.diagDetails = make(map[int][]lsp.Diagnostic)
				m.docDiagDetails = make(map[string]map[int][]lsp.Diagnostic)
				if m.problemsPanel != nil {
					m.problemsPanel.Refresh(m.docDiagDetails)
				}
				m.refreshDocumentStructure()
			}
		}
	} else {
		// A plugin was enabled: re-initialize LSP and symbols if active document matches.
		doc := m.eng.ActiveDocument()
		if doc != nil && doc.FilePath != "" {
			m.EnsureLSPForFile(doc.FilePath)
			m.refreshDocumentStructure()
		}
	}
	if m.launchModal != nil && m.pluginMgr != nil {
		m.launchModal.SetSupportedTypes(m.pluginMgr.GetSupportedLaunchTypes())
	}
}

// normalizeURI standardizes Windows and POSIX URIs and file paths for reliable matching.
func normalizeURI(u string) string {
	if unescaped, err := url.PathUnescape(u); err == nil {
		u = unescaped
	}
	u = strings.ReplaceAll(u, "\\", "/")

	// Handle URI schemes
	if strings.HasPrefix(u, "file:////") {
		// UNC path file URI: file:////server/share/file -> //server/share/file
		u = u[len("file://"):]
	} else if strings.HasPrefix(u, "file:///") {
		remainder := u[len("file:///"):]
		// Check if followed by Windows drive letter like C:/ or c:/
		if len(remainder) >= 2 && remainder[1] == ':' && ((remainder[0] >= 'a' && remainder[0] <= 'z') || (remainder[0] >= 'A' && remainder[0] <= 'Z')) {
			u = remainder
		} else {
			// POSIX absolute path like /home/user/...
			u = "/" + remainder
		}
	} else if strings.HasPrefix(u, "file://") {
		remainder := u[len("file://"):]
		if !strings.HasPrefix(remainder, "/") && strings.Contains(remainder, "/") {
			// UNC file URI: file://server/share/file -> //server/share/file
			u = "//" + remainder
		} else {
			u = remainder
		}
	} else if strings.HasPrefix(u, "file:") {
		u = strings.TrimPrefix(u, "file:")
	}

	// Handle Windows drive with leading slash: /c:/ -> c:/ or /C:/ -> c:/
	if len(u) >= 3 && u[0] == '/' && u[2] == ':' && ((u[1] >= 'a' && u[1] <= 'z') || (u[1] >= 'A' && u[1] <= 'Z')) {
		u = u[1:]
	}

	// Lowercase drive letter if present
	if len(u) >= 2 && u[1] == ':' && ((u[0] >= 'a' && u[0] <= 'z') || (u[0] >= 'A' && u[0] <= 'Z')) {
		u = strings.ToLower(string(u[0])) + u[1:]
	}

	cleaned := filepath.Clean(filepath.FromSlash(u))
	return strings.ToLower(cleaned)
}

// cleanDiagnosticMessage sanitizes diagnostic strings by removing \r, taking the first line before \n,
// and stripping non-printable control characters to prevent terminal scroll corruption.
func cleanDiagnosticMessage(msg string) string {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")
	if idx := strings.Index(msg, "\n"); idx != -1 {
		msg = msg[:idx]
	}
	msg = strings.TrimSpace(msg)
	var sb strings.Builder
	for _, r := range msg {
		if r < 32 || r == 127 || (r >= 0x80 && r <= 0x9f) {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// OnDiagnostics receives asynchronous diagnostic reports from the LSP client.
func (m *AppModel) OnDiagnostics(uri string, diags []lsp.Diagnostic) {
	normIncoming := normalizeURI(uri)

	m.diagMu.Lock()
	if m.docDiagnostics == nil {
		m.docDiagnostics = make(map[string]map[int]string)
		m.docDiagDetails = make(map[string]map[int][]lsp.Diagnostic)
	}

	fileDiags := make(map[int]string)
	fileDiagDetails := make(map[int][]lsp.Diagnostic)
	for _, d := range diags {
		line := d.Range.Start.Line
		fileDiags[line] = cleanDiagnosticMessage(d.Message)
		fileDiagDetails[line] = append(fileDiagDetails[line], d)
	}

	m.docDiagnostics[normIncoming] = fileDiags
	m.docDiagDetails[normIncoming] = fileDiagDetails

	// Update active document diagnostics if incoming matches active document
	doc := m.eng.ActiveDocument()
	if doc != nil {
		normDoc := normalizeURI(doc.FilePath)
		if normIncoming == normDoc {
			m.diagnostics = fileDiags
			m.diagDetails = fileDiagDetails
		}
	} else {
		m.diagnostics = fileDiags
		m.diagDetails = fileDiagDetails
	}
	if m.problemsPanel != nil {
		m.problemsPanel.Refresh(m.docDiagDetails)
	}
	m.diagMu.Unlock()

	// Immediately notify TEA event loop to repaint editor frame with latest diagnostics
	if m.program != nil {
		m.program.Send(lspDiagnosticsMsg{URI: uri})
	}
}

// syncActiveDiagnostics synchronizes diagnostics for the currently active document.
func (m *AppModel) syncActiveDiagnostics() {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	m.diagMu.Lock()
	defer m.diagMu.Unlock()
	if m.docDiagnostics != nil {
		normDoc := normalizeURI(doc.FilePath)
		if fileDiags, ok := m.docDiagnostics[normDoc]; ok {
			m.diagnostics = fileDiags
			m.diagDetails = m.docDiagDetails[normDoc]
			return
		}
	}
	m.diagnostics = make(map[int]string)
	m.diagDetails = make(map[int][]lsp.Diagnostic)
}

// getDiagnosticsForLine returns all diagnostics on lineIdx from active LSP.
func (m *AppModel) getDiagnosticsForLine(line int) ([]lsp.Diagnostic, bool) {
	m.diagMu.RLock()
	defer m.diagMu.RUnlock()

	var result []lsp.Diagnostic
	found := false

	doc := m.eng.ActiveDocument()
	if doc != nil && m.docDiagDetails != nil {
		normDoc := normalizeURI(doc.FilePath)
		if fileMap, ok := m.docDiagDetails[normDoc]; ok {
			if list, ok2 := fileMap[line]; ok2 && len(list) > 0 {
				result = append(result, list...)
				found = true
			}
		}
	} else if m.diagDetails != nil {
		if list, ok := m.diagDetails[line]; ok && len(list) > 0 {
			result = append(result, list...)
			found = true
		}
	}

	if !found && doc != nil && m.docDiagnostics != nil {
		normDoc := normalizeURI(doc.FilePath)
		if fileMap, ok := m.docDiagnostics[normDoc]; ok {
			if msg, ok2 := fileMap[line]; ok2 && msg != "" {
				result = append(result, lsp.Diagnostic{
					Severity: 1,
					Message:  msg,
					Range: lsp.Range{
						Start: lsp.Position{Line: line, Character: 0},
						End:   lsp.Position{Line: line, Character: 999},
					},
				})
				found = true
			}
		}
	}
	if !found && m.diagnostics != nil {
		if msg, ok := m.diagnostics[line]; ok && msg != "" {
			result = append(result, lsp.Diagnostic{
				Severity: 1,
				Message:  msg,
				Range: lsp.Range{
					Start: lsp.Position{Line: line, Character: 0},
					End:   lsp.Position{Line: line, Character: 999},
				},
			})
			found = true
		}
	}

	return result, found
}

// getDiagnosticAtCursor returns any diagnostic message on the active cursor line.
func (m *AppModel) getDiagnosticAtCursor() (string, bool) {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return "", false
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return "", false
	}
	cursorLine := sels[0].Head.Line

	m.diagMu.RLock()
	defer m.diagMu.RUnlock()

	normDoc := normalizeURI(doc.FilePath)
	if m.docDiagnostics != nil {
		if fileMap, ok := m.docDiagnostics[normDoc]; ok {
			if msg, ok2 := fileMap[cursorLine]; ok2 {
				return cleanDiagnosticMessage(msg), true
			}
		}
	}

	if msg, ok := m.diagnostics[cursorLine]; ok {
		return cleanDiagnosticMessage(msg), true
	}
	return "", false
}

// notifyLSPChange synchronizes document content with the connected LSP server.
func (m *AppModel) notifyLSPChange() {
	if m.lspClient == nil {
		return
	}
	doc := m.eng.ActiveDocument()
	if doc == nil || doc.FilePath == "" {
		return
	}
	m.lspDocVersion++
	uri := "file:///" + filepath.ToSlash(doc.FilePath)
	text, _ := doc.Buffer.GetText()
	_ = m.lspClient.DidChange(uri, m.lspDocVersion, -1, -1, -1, -1, string(text))
}

// triggerCompletionPopup activates completion items at current cursor.
func (m *AppModel) triggerCompletionPopup() {
	m.popupVisible = true
	m.popupTitle = "Completions"
	m.popupActive = 0

	// 1. Query active language server if attached
	if m.lspClient != nil {
		doc := m.eng.ActiveDocument()
		if doc != nil && doc.FilePath != "" {
			sels := doc.Buffer.GetSelections()
			if len(sels) > 0 {
				head := sels[0].Head
				uri := "file:///" + filepath.ToSlash(doc.FilePath)
				resp, err := m.lspClient.SendRequest("textDocument/completion", lsp.CompletionParams{
					TextDocument: lsp.TextDocumentIdentifier{URI: uri},
					Position:     lsp.Position{Line: head.Line, Character: head.Column},
				})
				if err == nil && resp != nil && resp.Result != nil {
					data, err := json.Marshal(resp.Result)
					if err == nil {
						var compList lsp.CompletionList
						_ = json.Unmarshal(data, &compList)
						if len(compList.Items) == 0 {
							var directItems []lsp.CompletionItem
							_ = json.Unmarshal(data, &directItems)
							compList.Items = directItems
						}
						if len(compList.Items) > 0 {
							items := make([]string, 0, len(compList.Items))
							for _, item := range compList.Items {
								s := item.Label
								if item.Detail != "" {
									s += " - " + item.Detail
								}
								items = append(items, s)
							}
							m.popupItems = items
							m.popupDoc = "LSP completion from active language server"
							return
						}
					}
				}
			}
		}
	}

	// 2. Rich multi-file, project and standard library symbol completion
	symbols := m.extractCompletionSymbols()
	if len(symbols) > 0 {
		m.popupItems = symbols
		m.popupDoc = "Project & standard library completions"
	} else {
		m.popupVisible = false
		m.popupItems = nil
		return
	}
}

// wordPrefixUnderCursor returns the identifier prefix immediately preceding the active cursor.
func (m *AppModel) wordPrefixUnderCursor() string {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return ""
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return ""
	}
	head := sels[0].Head
	lineBytes, err := doc.Buffer.GetLine(head.Line)
	if err != nil {
		return ""
	}
	runes := []rune(string(lineBytes))
	col := head.Column
	if col > len(runes) {
		col = len(runes)
	}
	start := col
	for start > 0 && (unicode.IsLetter(runes[start-1]) || unicode.IsDigit(runes[start-1]) || runes[start-1] == '_') {
		start--
	}
	if start >= col {
		return ""
	}
	return string(runes[start:col])
}

// insertCompletion inserts a chosen completion item, replacing the already typed prefix.
func (m *AppModel) insertCompletion(item string) {
	cleanItem := item
	if idx := strings.Index(item, " - "); idx != -1 {
		cleanItem = item[:idx]
	}
	cleanItem = strings.TrimSpace(cleanItem)

	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return
	}
	head := sels[0].Head
	lineBytes, _ := doc.Buffer.GetLine(head.Line)
	lineRunes := []rune(string(lineBytes))
	col := head.Column
	if col > len(lineRunes) {
		col = len(lineRunes)
	}

	prefix := m.wordPrefixUnderCursor()
	prefixLen := len([]rune(prefix))

	// Check if there is a qualifier preceding the prefix, e.g. "fmt." before cursor
	qualifierDotPos := col - prefixLen - 1
	if qualifierDotPos >= 0 && lineRunes[qualifierDotPos] == '.' {
		// Scan backwards to find qualifier word, e.g. "fmt"
		qStart := qualifierDotPos
		for qStart > 0 && (unicode.IsLetter(lineRunes[qStart-1]) || unicode.IsDigit(lineRunes[qStart-1]) || lineRunes[qStart-1] == '_') {
			qStart--
		}
		qualifier := string(lineRunes[qStart:qualifierDotPos])
		if qualifier != "" && strings.Contains(cleanItem, ".") {
			parts := strings.Split(cleanItem, ".")
			if strings.EqualFold(parts[0], qualifier) {
				// Strip the duplicated qualifier package prefix (e.g. "fmt.Println" -> "Println")
				cleanItem = strings.Join(parts[1:], ".")
			}
		}
	} else if strings.Contains(cleanItem, ".") {
		parts := strings.Split(cleanItem, ".")
		lastPart := parts[len(parts)-1]
		if prefix != "" && strings.HasPrefix(strings.ToLower(lastPart), strings.ToLower(prefix)) {
			cleanItem = lastPart
		}
	}

	for i := 0; i < prefixLen; i++ {
		_ = m.eng.Dispatch(core.Command{ID: core.CmdDeleteBackward})
	}

	if strings.Contains(cleanItem, "$") {
		parsed := corebuf.ParseSnippet(cleanItem)
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: parsed.PlainText})
		if len(parsed.Tabstops) > 0 {
			m.snippetSession = corebuf.NewSnippetSession(parsed, head.Line, col-prefixLen)
			if cur, ok := m.snippetSession.Current(); ok && cur.Index != 0 {
				m.statusMessage = m.snippetSession.FormatSnippetPrompt()
			}
		}
	} else {
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: cleanItem})
	}
	m.notifyLSPChange()
}

// gotoDefinition jumps to the definition of the symbol under cursor (F12, idea.md section 9).
func (m *AppModel) gotoDefinition() {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return
	}
	head := sels[0].Head

	// 1. Try LSP definition query
	if m.lspClient != nil && doc.FilePath != "" {
		uri := "file:///" + filepath.ToSlash(doc.FilePath)
		loc, err := m.lspClient.Definition(uri, head.Line, head.Column)
		if err == nil && loc != nil && loc.URI != "" {
			targetPath := strings.TrimPrefix(loc.URI, "file:///")
			targetPath = filepath.FromSlash(targetPath)
			if filepath.Clean(targetPath) != filepath.Clean(doc.FilePath) {
				_, _ = m.eng.Open(targetPath)
				doc = m.eng.ActiveDocument()
			}
			if doc != nil {
				pos := corebuf.Position{
					Line:   loc.Range.Start.Line,
					Column: loc.Range.Start.Character,
				}
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(pos, pos)})
				m.ensureCursorVisible()
				m.statusMessage = fmt.Sprintf("Definition: %s:%d", filepath.Base(targetPath), loc.Range.Start.Line+1)
				return
			}
		}
	}

	// 2. Fallback: Search symbol under cursor in active document
	word := m.wordUnderCursor()
	if word != "" {
		total := doc.Buffer.TotalLines()
		for l := 0; l < total; l++ {
			lineBytes, _ := doc.Buffer.GetLine(l)
			lineStr := string(lineBytes)
			if strings.Contains(lineStr, "func "+word) ||
				strings.Contains(lineStr, "type "+word) ||
				strings.Contains(lineStr, "const "+word) ||
				strings.Contains(lineStr, "var "+word) ||
				strings.Contains(lineStr, "def "+word) ||
				strings.Contains(lineStr, "class "+word) {
				pos := corebuf.Position{Line: l, Column: 0}
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(pos, pos)})
				m.ensureCursorVisible()
				m.statusMessage = fmt.Sprintf("Found definition of '%s' at line %d", word, l+1)
				return
			}
		}
		m.statusMessage = fmt.Sprintf("Definition not found for '%s'", word)
	} else {
		m.statusMessage = "No symbol under cursor for definition"
	}
}

// showHover displays signature and documentation for the symbol under cursor (Shift+K, idea.md section 9).
func (m *AppModel) showHover() {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return
	}
	head := sels[0].Head

	word := m.wordUnderCursor()
	if word == "" {
		m.popupVisible = true
		m.popupTitle = "Hover"
		m.popupItems = []string{
			fmt.Sprintf("Position: Line %d, Col %d", head.Line+1, head.Column+1),
			"No symbol under cursor",
		}
		m.popupActive = 0
		m.popupDoc = "Cursor info"
		return
	}

	// 1. Try LSP Hover if client is connected
	if m.lspClient != nil && doc.FilePath != "" {
		uri := "file:///" + filepath.ToSlash(doc.FilePath)
		h, err := m.lspClient.Hover(uri, head.Line, head.Column)
		if err == nil && h != nil && h.Contents != nil {
			var docStr string
			switch val := h.Contents.(type) {
			case string:
				docStr = val
			case map[string]any:
				if v, ok := val["value"].(string); ok {
					docStr = v
				}
			default:
				bytes, _ := json.Marshal(val)
				docStr = string(bytes)
			}

			lines := strings.Split(docStr, "\n")
			cleanLines := make([]string, 0, len(lines))
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if l != "" && !strings.HasPrefix(l, "```") {
					cleanLines = append(cleanLines, l)
				}
			}
			if len(cleanLines) > 0 {
				m.popupVisible = true
				m.popupTitle = fmt.Sprintf("Hover: %s", word)
				m.popupItems = cleanLines
				m.popupActive = 0
				m.popupDoc = "LSP Documentation"
				return
			}
		}
	}

	// 2. Fallback: Contextual hover info
	m.popupVisible = true
	m.popupTitle = fmt.Sprintf("Symbol: %s", word)
	m.popupItems = []string{
		fmt.Sprintf("Identifier: %s", word),
		fmt.Sprintf("File: %s", filepath.Base(doc.FilePath)),
		fmt.Sprintf("Line: %d, Column: %d", head.Line+1, head.Column+1),
		"Press F12 to go to definition",
	}
	m.popupActive = 0
	m.popupDoc = "Symbol Information"
}

// triggerQuickFix queries LSP and local rules for code actions at current cursor and opens popup.
func (m *AppModel) triggerQuickFix() {
	doc := m.eng.ActiveDocument()
	if doc == nil || doc.Buffer == nil {
		return
	}
	sel := doc.Buffer.PrimarySelection()
	head := sel.Head

	var actions []lsp.CodeAction

	// 1. If LSP client is connected and document has file path, request code actions
	if m.lspClient != nil && doc.FilePath != "" {
		uri := "file:///" + filepath.ToSlash(doc.FilePath)
		var diags []lsp.Diagnostic
		if diagMsg, ok := m.diagnostics[head.Line]; ok {
			diags = append(diags, lsp.Diagnostic{
				Range: lsp.Range{
					Start: lsp.Position{Line: head.Line, Character: 0},
					End:   lsp.Position{Line: head.Line, Character: 120},
				},
				Severity: lsp.SeverityError,
				Message:  diagMsg,
			})
		}
		lspActions, err := m.lspClient.CodeActions(uri, head.Line, head.Column, head.Line, head.Column, diags)
		if err == nil && len(lspActions) > 0 {
			actions = append(actions, lspActions...)
		}
	}

	// 2. Contextual smart quick fixes
	localActions := m.generateContextualQuickFixes(doc, head.Line, head.Column)
	actions = append(actions, localActions...)

	if len(actions) == 0 {
		m.toasts.Info("QUICK FIX", "No code actions available at cursor")
		return
	}

	if m.quickFixModal == nil {
		m.quickFixModal = NewQuickFixModal()
	}

	screenX := 12
	screenY := 6
	if m.splits != nil {
		pane := m.splits.PaneAt(m.splits.ActiveIndex)
		if pane != nil {
			screenX = pane.Bounds.X + pane.GutterWidth + (head.Column - pane.ViewportX)
			screenY = pane.Bounds.Y + 1 + (head.Line - pane.ViewportY)
		}
	}

	m.quickFixModal.OpenForActions(actions, screenX, screenY, m.width, m.height)
}

// applyCodeAction executes workspace text edits or commands from selected code action.
func (m *AppModel) applyCodeAction(action *lsp.CodeAction) {
	if action == nil {
		return
	}
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}

	applied := false
	if action.Edit != nil && len(action.Edit.Changes) > 0 {
		uri := "file:///" + filepath.ToSlash(doc.FilePath)
		if edits, ok := action.Edit.Changes[uri]; ok && len(edits) > 0 {
			_ = doc.ApplyTextEdits(edits)
			applied = true
		} else {
			for _, edits := range action.Edit.Changes {
				if len(edits) > 0 {
					_ = doc.ApplyTextEdits(edits)
					applied = true
					break
				}
			}
		}
	}

	if applied {
		m.toasts.Success("QUICK FIX", fmt.Sprintf("Applied: %s", action.Title))
	} else {
		m.toasts.Info("QUICK FIX", fmt.Sprintf("Executed: %s", action.Title))
	}
}

// generateContextualQuickFixes inspects current line context to provide instantaneous editor fixes.
func (m *AppModel) generateContextualQuickFixes(doc *core.Document, line, col int) []lsp.CodeAction {
	var actions []lsp.CodeAction
	if doc == nil || doc.Buffer == nil {
		return actions
	}
	lineBytes, err := doc.Buffer.GetLine(line)
	if err != nil {
		return actions
	}
	lineStr := string(lineBytes)
	trimmed := strings.TrimSpace(lineStr)
	uri := "file:///" + filepath.ToSlash(doc.FilePath)

	// Quick Fix: Remove trailing whitespace
	if len(lineStr) > len(strings.TrimRight(lineStr, " \t\r\n")) {
		cleanLine := strings.TrimRight(lineStr, " \t\r\n")
		actions = append(actions, lsp.CodeAction{
			Title: "Remove trailing whitespace",
			Kind:  "quickfix",
			Edit: &lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					uri: {
						{
							Range: lsp.Range{
								Start: lsp.Position{Line: line, Character: 0},
								End:   lsp.Position{Line: line, Character: len(lineStr)},
							},
							NewText: cleanLine,
						},
					},
				},
			},
		})
	}

	// Quick Fix: Add doc comment
	if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "type ") {
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 {
			name := parts[1]
			if idx := strings.IndexAny(name, "( [{"); idx != -1 {
				name = name[:idx]
			}
			comment := fmt.Sprintf("// %s ...\n", name)
			actions = append(actions, lsp.CodeAction{
				Title: fmt.Sprintf("Add doc comment for %s", name),
				Kind:  "refactor",
				Edit: &lsp.WorkspaceEdit{
					Changes: map[string][]lsp.TextEdit{
						uri: {
							{
								Range: lsp.Range{
									Start: lsp.Position{Line: line, Character: 0},
									End:   lsp.Position{Line: line, Character: 0},
								},
								NewText: comment,
							},
						},
					},
				},
			})
		}
	}

	// Quick Fix: Wrap with error handling if line contains err assignment
	if strings.Contains(lineStr, "err :=") || strings.Contains(lineStr, "err =") {
		indent := ""
		for _, ch := range lineStr {
			if ch == ' ' || ch == '\t' {
				indent += string(ch)
			} else {
				break
			}
		}
		errCheck := fmt.Sprintf("\n%sif err != nil {\n%s\treturn err\n%s}", indent, indent, indent)
		actions = append(actions, lsp.CodeAction{
			Title: "Add 'if err != nil' error check",
			Kind:  "quickfix",
			Edit: &lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					uri: {
						{
							Range: lsp.Range{
								Start: lsp.Position{Line: line, Character: len(lineStr)},
								End:   lsp.Position{Line: line, Character: len(lineStr)},
							},
							NewText: errCheck,
						},
					},
				},
			},
		})
	}

	// Go Code Generator Quick Fixes (Struct tags, constructors, getters/setters, deepcopy, interfaces)
	if strings.HasSuffix(doc.FilePath, ".go") {
		if src, err := doc.Buffer.GetText(); err == nil && len(src) > 0 {
			goActions := gogen.GenerateQuickFixActions(uri, src, line, col)
			actions = append(actions, goActions...)
		}
	}

	return actions
}

// openRenameModal opens the Symbol Rename modal.
func (m *AppModel) openRenameModal() {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	oldName := m.selectedText()
	if oldName == "" {
		oldName = m.wordUnderCursor()
	}
	if oldName == "" {
		m.toasts.Warn("RENAME", "Place cursor on a symbol or select text to rename")
		return
	}
	sels := doc.Buffer.GetSelections()
	line := 0
	col := 0
	if len(sels) > 0 {
		line = sels[0].Head.Line
		col = sels[0].Head.Column
	}
	m.renameModal.OpenModal(oldName, line, col, doc.FilePath)
}

// executeSymbolRename performs complete project-wide symbol renaming via LSP or AST fallback.
func (m *AppModel) executeSymbolRename(oldName, newName string, line, col int, uri string) (tea.Model, tea.Cmd) {
	if oldName == "" || newName == "" || oldName == newName {
		return m, nil
	}

	renamedFiles := 0
	renamedOccurrences := 0

	// 1. Try LSP Rename if active
	if m.lspClient != nil {
		we, err := m.lspClient.Rename(uri, line, col, newName)
		if err == nil && we != nil && (len(we.Changes) > 0 || len(we.DocumentChanges) > 0) {
			for fileURI, edits := range we.Changes {
				filePath := fileURI
				if strings.HasPrefix(filePath, "file://") {
					filePath = strings.TrimPrefix(filePath, "file://")
					if strings.HasPrefix(filePath, "/") && len(filePath) > 2 && filePath[2] == ':' {
						filePath = filePath[1:]
					}
				}
				sort.Slice(edits, func(i, j int) bool {
					if edits[i].Range.Start.Line != edits[j].Range.Start.Line {
						return edits[i].Range.Start.Line > edits[j].Range.Start.Line
					}
					return edits[i].Range.Start.Character > edits[j].Range.Start.Character
				})

				var openDoc *core.Document
				for _, d := range m.eng.Documents() {
					if strings.EqualFold(filepath.Clean(d.FilePath), filepath.Clean(filePath)) {
						openDoc = d
						break
					}
				}

				if openDoc != nil && openDoc.Buffer != nil {
					for _, ed := range edits {
						startByte, _ := openDoc.Buffer.ByteOffsetForLine(ed.Range.Start.Line)
						lineB, _ := openDoc.Buffer.GetLine(ed.Range.Start.Line)
						runes := []rune(string(lineB))
						for c := 0; c < ed.Range.Start.Character && c < len(runes); c++ {
							startByte += len(string(runes[c]))
						}

						endByte, _ := openDoc.Buffer.ByteOffsetForLine(ed.Range.End.Line)
						lineEndB, _ := openDoc.Buffer.GetLine(ed.Range.End.Line)
						endRunes := []rune(string(lineEndB))
						for c := 0; c < ed.Range.End.Character && c < len(endRunes); c++ {
							endByte += len(string(endRunes[c]))
						}

						if startByte <= endByte && startByte >= 0 && endByte <= openDoc.Buffer.TotalBytes() {
							_ = openDoc.Buffer.ApplyEdit(startByte, endByte-startByte, ed.NewText)
							renamedOccurrences++
						}
					}
					_ = openDoc.Buffer.SaveAtomic(openDoc.FilePath)
					renamedFiles++
				} else {
					contentBytes, err := os.ReadFile(filePath)
					if err == nil {
						lines := strings.Split(string(contentBytes), "\n")
						for _, ed := range edits {
							if ed.Range.Start.Line < len(lines) && ed.Range.End.Line < len(lines) {
								if ed.Range.Start.Line == ed.Range.End.Line {
									l := lines[ed.Range.Start.Line]
									lRunes := []rune(l)
									if ed.Range.Start.Character <= len(lRunes) && ed.Range.End.Character <= len(lRunes) {
										newL := string(lRunes[:ed.Range.Start.Character]) + ed.NewText + string(lRunes[ed.Range.End.Character:])
										lines[ed.Range.Start.Line] = newL
										renamedOccurrences++
									}
								}
							}
						}
						_ = os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0644)
						renamedFiles++
					}
				}
			}

			for _, docEdit := range we.DocumentChanges {
				filePath := docEdit.TextDocument.URI
				if strings.HasPrefix(filePath, "file://") {
					filePath = strings.TrimPrefix(filePath, "file://")
					if strings.HasPrefix(filePath, "/") && len(filePath) > 2 && filePath[2] == ':' {
						filePath = filePath[1:]
					}
				}
				edits := docEdit.Edits
				sort.Slice(edits, func(i, j int) bool {
					if edits[i].Range.Start.Line != edits[j].Range.Start.Line {
						return edits[i].Range.Start.Line > edits[j].Range.Start.Line
					}
					return edits[i].Range.Start.Character > edits[j].Range.Start.Character
				})

				var openDoc *core.Document
				for _, d := range m.eng.Documents() {
					if strings.EqualFold(filepath.Clean(d.FilePath), filepath.Clean(filePath)) {
						openDoc = d
						break
					}
				}

				if openDoc != nil && openDoc.Buffer != nil {
					for _, ed := range edits {
						startByte, _ := openDoc.Buffer.ByteOffsetForLine(ed.Range.Start.Line)
						lineB, _ := openDoc.Buffer.GetLine(ed.Range.Start.Line)
						runes := []rune(string(lineB))
						for c := 0; c < ed.Range.Start.Character && c < len(runes); c++ {
							startByte += len(string(runes[c]))
						}

						endByte, _ := openDoc.Buffer.ByteOffsetForLine(ed.Range.End.Line)
						lineEndB, _ := openDoc.Buffer.GetLine(ed.Range.End.Line)
						endRunes := []rune(string(lineEndB))
						for c := 0; c < ed.Range.End.Character && c < len(endRunes); c++ {
							endByte += len(string(endRunes[c]))
						}

						if startByte <= endByte && startByte >= 0 && endByte <= openDoc.Buffer.TotalBytes() {
							_ = openDoc.Buffer.ApplyEdit(startByte, endByte-startByte, ed.NewText)
							renamedOccurrences++
						}
					}
					_ = openDoc.Buffer.SaveAtomic(openDoc.FilePath)
					renamedFiles++
				} else {
					contentBytes, err := os.ReadFile(filePath)
					if err == nil {
						lines := strings.Split(string(contentBytes), "\n")
						for _, ed := range edits {
							if ed.Range.Start.Line < len(lines) && ed.Range.End.Line < len(lines) {
								if ed.Range.Start.Line == ed.Range.End.Line {
									l := lines[ed.Range.Start.Line]
									lRunes := []rune(l)
									if ed.Range.Start.Character <= len(lRunes) && ed.Range.End.Character <= len(lRunes) {
										newL := string(lRunes[:ed.Range.Start.Character]) + ed.NewText + string(lRunes[ed.Range.End.Character:])
										lines[ed.Range.Start.Line] = newL
										renamedOccurrences++
									}
								}
							}
						}
						_ = os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0644)
						renamedFiles++
					}
				}
			}

			if renamedFiles > 0 {
				m.toasts.Success("RENAME", fmt.Sprintf("Renamed '%s' -> '%s' across %d file(s) (%d occurrences)", oldName, newName, renamedFiles, renamedOccurrences))
				m.onActiveDocumentChanged()
				return m, nil
			}
		}
	}

	// 2. Fallback Project Scanner / AST boundary replacement
	targetDir := m.workspaceDir
	if targetDir == "" {
		targetDir = "."
	}
	rePattern := `\b` + regexp.QuoteMeta(oldName) + `\b`
	re, err := regexp.Compile(rePattern)
	if err != nil {
		m.toasts.Error("RENAME", "Invalid symbol name")
		return m, nil
	}

	validExts := map[string]bool{
		".go": true, ".rs": true, ".py": true, ".js": true, ".ts": true,
		".jsx": true, ".tsx": true, ".c": true, ".cpp": true, ".h": true,
		".hpp": true, ".json": true, ".md": true, ".txt": true, ".yaml": true,
		".yml": true, ".toml": true, ".sh": true, ".bash": true,
	}

	_ = filepath.Walk(targetDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.IsDir() {
			if info != nil && info.IsDir() {
				name := info.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "bin" || name == "obj" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !validExts[ext] {
			return nil
		}

		var openDoc *core.Document
		for _, d := range m.eng.Documents() {
			if strings.EqualFold(filepath.Clean(d.FilePath), filepath.Clean(path)) {
				openDoc = d
				break
			}
		}

		if openDoc != nil && openDoc.Buffer != nil {
			var fullText strings.Builder
			tot := openDoc.Buffer.TotalLines()
			for li := 0; li < tot; li++ {
				lb, _ := openDoc.Buffer.GetLine(li)
				fullText.WriteString(string(lb))
			}
			original := fullText.String()
			matches := re.FindAllStringIndex(original, -1)
			if len(matches) > 0 {
				newText := re.ReplaceAllString(original, newName)
				_ = openDoc.Buffer.ApplyEdit(0, openDoc.Buffer.TotalBytes(), newText)
				_ = openDoc.Buffer.SaveAtomic(openDoc.FilePath)
				renamedFiles++
				renamedOccurrences += len(matches)
			}
		} else {
			data, err := os.ReadFile(path)
			if err == nil {
				original := string(data)
				matches := re.FindAllStringIndex(original, -1)
				if len(matches) > 0 {
					newText := re.ReplaceAllString(original, newName)
					_ = os.WriteFile(path, []byte(newText), info.Mode())
					renamedFiles++
					renamedOccurrences += len(matches)
				}
			}
		}
		return nil
	})

	if renamedFiles > 0 {
		m.toasts.Success("RENAME", fmt.Sprintf("Renamed '%s' -> '%s' in %d file(s) (%d occurrences)", oldName, newName, renamedFiles, renamedOccurrences))
	} else {
		m.toasts.Warn("RENAME", fmt.Sprintf("No occurrences of '%s' found", oldName))
	}
	m.onActiveDocumentChanged()
	return m, nil
}

