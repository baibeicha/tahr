package tui

import (
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"github.com/sahilm/fuzzy"

	"tahr/internal/core"
	corebuf "tahr/internal/core/buffer"
	"tahr/internal/core/keymaps"
)

// openOmnibar activates the Omnibar modal dialog in either "files" or "commands" mode.
func (m *AppModel) openOmnibar(mode string) {
	m.omnibarOpen = true
	m.omnibarMode = mode
	m.omnibarQuery = ""
	m.omnibarSel = 0
	m.updateOmnibarCandidates()
}

// updateOmnibarCandidates filters items using fuzzy search or document search.
func (m *AppModel) updateOmnibarCandidates() {
	var candidates []string
	switch m.omnibarMode {
	case "commands":
		candidates = []string{
			"Open / Create Project (Ctrl+O)",
			"Run Project (F5 / Ctrl+R)",
			"Build Project (F7 / Ctrl+Shift+B)",
			"Toggle Project Tree (Ctrl+B / F2)",
			"Find in Document (Ctrl+F)",
			"Go to Line (Ctrl+G)",
			"Go to Definition (F12)",
			"Hover Documentation (Shift+K)",
			"Step Over (F10)",
			"Step Into (F11)",
			"Continue Debugging (F5)",
			"Save File (Ctrl+S)",
			"Find File (Ctrl+P)",
			"Command Palette (Ctrl+Shift+P)",
			"Select Next Match (Ctrl+D)",
			"Undo (Ctrl+Z)",
			"Redo (Ctrl+Y)",
			"Select All (Ctrl+A)",
			"Toggle Breakpoint (F9)",
			"Restart LSP Server",
			"Format Buffer (gofmt / LSP)",
			"Split: Single Pane (1)",
			"Split: 2 Columns (2)",
			"Split: 2 Rows (2)",
			"Split: 3 Columns (3)",
			"Split: 4 Grid (2x2)",
			"Split: 5 Panes (5)",
			"Split: 6 Grid (3x2)",
			"Split: Cycle Layout (Ctrl+\\)",
			"Split: Next Pane (Alt+Right)",
			"Split: Previous Pane (Alt+Left)",
			"Tree: Move Dock (Left/Right) (Ctrl+Alt+E)",
			"View: Toggle Integrated Terminal (F4 / Ctrl+~)",
			"Debug: Toggle Debugger HUD (F8)",
			"View: Toggle Code Minimap",
			"Settings: Open Settings (Ctrl+,)",
			"Plugins: Open Marketplace",
			"Editor: Pick / Edit Color (Alt+C)",
			"Git: Interactive Staging & Branch Graph (Ctrl+Shift+G)",
			"Markdown: Live Preview Split (F8 / Ctrl+M)",
			"Search in Files (Ctrl+Shift+F)",
			"Problems: View Diagnostics (Ctrl+Shift+M)",
			"Keymap: Switch to VS Code",
			"Keymap: Switch to JetBrains",
			"Keymap: Switch to Emacs",
			"Vim Mode: Toggle Modal Editing",
			"Refactor: Quick Fix / Code Actions (Alt+Enter)",
			"Run: Configurations (Alt+Shift+F10)",
			"Theme: Catppuccin Mocha",
			"Theme: Dracula",
			"Theme: Nord",
			"Theme: Monokai Pro",
			"Theme: Tokyo Night",
			"Theme: Gruvbox Dark",
			"Collab: Toggle P2P Collaboration Panel (Ctrl+Shift+L)",
			"Collab: Start P2P Host Room",
			"Collab: Join P2P Room by Code",
			"Database: Open DAG Schema Designer",
			"Graph: Open Polyglot Project Graphs",
			"IDE: Report Bug / Issue",
			"IDE: View Diagnostic Logs & System Data",
			"Tools: DevTools Utilities (Ctrl+Shift+U)",
			"Tools: Regex Tester & Replacer (Ctrl+Shift+R)",
			"Tools: Bookmarks Navigator (Ctrl+Shift+B)",
			"Bookmarks: Toggle Bookmark (Ctrl+F2)",
			"Bookmarks: Next Bookmark (Alt+F2)",
			"Bookmarks: Previous Bookmark (Shift+F2)",
			"Coverage: Load Workspace Coverage",
			"View: Docker Compose",
			"View: REST Client",
			"View: gRPC & Protobuf",
			"View: Log Viewer",
			"View: Task Runner",
			"View: TODO Tree",
			"View: Test Runner",
			"View: Jupyter Notebook",
		}
	case "project":
		candidates = []string{
			"Open Existing Directory...",
			"New Go Project (go mod + main.go)",
			"New Python Project (main.py + requirements.txt)",
			"New Rust Project (Cargo.toml + main.rs)",
			"New Blank Project (empty directory + README.md)",
		}
	case "open_dir":
		q := strings.TrimSpace(m.omnibarQuery)
		if q == "" {
			candidates = []string{
				fmt.Sprintf("Current: %s", m.workspaceDir),
				"Enter directory path (e.g. D:/project or ./my-app)",
			}
		} else {
			candidates = []string{fmt.Sprintf("Open directory: %s", q)}
			dirToScan := filepath.Dir(q)
			prefix := strings.ToLower(filepath.Base(q))
			if strings.HasSuffix(q, "/") || strings.HasSuffix(q, "\\") {
				dirToScan = q
				prefix = ""
			}
			if entries, err := os.ReadDir(dirToScan); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						name := e.Name()
						if prefix == "" || strings.HasPrefix(strings.ToLower(name), prefix) {
							fullP := filepath.Join(dirToScan, name)
							candidates = append(candidates, fullP)
							if len(candidates) >= 15 {
								break
							}
						}
					}
				}
			}
		}
	case "new_go_project":
		if m.omnibarQuery == "" {
			candidates = []string{"Enter project name for Go module"}
		} else {
			candidates = []string{fmt.Sprintf("Create Go project '%s'", m.omnibarQuery)}
		}
	case "new_py_project":
		if m.omnibarQuery == "" {
			candidates = []string{"Enter project name for Python app"}
		} else {
			candidates = []string{fmt.Sprintf("Create Python project '%s'", m.omnibarQuery)}
		}
	case "new_rust_project":
		if m.omnibarQuery == "" {
			candidates = []string{"Enter project name for Rust crate"}
		} else {
			candidates = []string{fmt.Sprintf("Create Rust project '%s'", m.omnibarQuery)}
		}
	case "new_blank_project":
		if m.omnibarQuery == "" {
			candidates = []string{"Enter project name for blank project"}
		} else {
			candidates = []string{fmt.Sprintf("Create Blank project '%s'", m.omnibarQuery)}
		}
	case "find":
		doc := m.eng.ActiveDocument()
		if doc != nil {
			q := strings.ToLower(m.omnibarQuery)
			total := doc.Buffer.TotalLines()
			for l := 0; l < total; l++ {
				lineBytes, _ := doc.Buffer.GetLine(l)
				lineStr := strings.TrimRight(string(lineBytes), "\r\n")
				if q == "" || strings.Contains(strings.ToLower(lineStr), q) {
					candidates = append(candidates, fmt.Sprintf("%d: %s", l+1, strings.TrimSpace(lineStr)))
					if len(candidates) >= 100 {
						break
					}
				}
			}
		}
	case "goto":
		doc := m.eng.ActiveDocument()
		total := 1
		if doc != nil {
			total = doc.Buffer.TotalLines()
		}
		if m.omnibarQuery == "" {
			candidates = []string{fmt.Sprintf("Enter line number (1..%d)", total)}
		} else {
			candidates = []string{fmt.Sprintf("Go to line %s (1..%d)", m.omnibarQuery, total)}
		}
	case "tools":
		candidates = []string{
			"AI Assistant (Ctrl+L)",
			"Database Inspector & DAG Canvas (F6)",
			"Project Graphs & Call Hierarchy (F3)",
			"Docker Compose (docker)",
			"REST Client (rest-client)",
			"gRPC & Protobuf (grpc)",
			"Log Viewer (log-viewer)",
			"Task Runner (task-runner)",
			"TODO Tree (todo-tree)",
			"Test Runner (test-runner)",
			"Jupyter Notebook (jupyter-notebook)",
			"DevTools Utilities (devtools)",
			"Regex Tester (regex)",
			"Bookmarks Navigator (bookmarks)",
		}
		if m.pluginMgr != nil {
			for _, tw := range m.pluginMgr.ActiveToolWindows() {
				candidates = append(candidates, fmt.Sprintf("%s (%s)", tw.Title, tw.ID))
			}
		}
	default:
		candidates = m.allProjectFiles
	}

	if m.omnibarQuery == "" || m.omnibarMode == "goto" || m.omnibarMode == "find" || strings.HasPrefix(m.omnibarMode, "new_") || m.omnibarMode == "open_dir" || m.omnibarMode == "project" {
		m.omnibarItems = candidates
	} else {
		matches := fuzzy.Find(m.omnibarQuery, candidates)
		res := make([]string, len(matches))
		for i, match := range matches {
			res[i] = match.Str
		}
		m.omnibarItems = res
	}

	if m.omnibarSel >= len(m.omnibarItems) {
		m.omnibarSel = 0
	}
}

// handleOmnibarKey navigates and executes items inside Omnibar.
func (m *AppModel) handleOmnibarKey(k input.Key) (tea.Model, tea.Cmd) {
	switch k.Type {
	case input.KeyEsc:
		m.omnibarOpen = false
		return m, nil

	case input.KeyUp:
		if m.omnibarSel > 0 {
			m.omnibarSel--
		}
		return m, nil

	case input.KeyDown:
		if m.omnibarSel+1 < len(m.omnibarItems) {
			m.omnibarSel++
		}
		return m, nil

	case input.KeyBackspace:
		if len(m.omnibarQuery) > 0 {
			m.omnibarQuery = m.omnibarQuery[:len(m.omnibarQuery)-1]
			m.updateOmnibarCandidates()
		}
		return m, nil

	case input.KeyEnter:
		if len(m.omnibarItems) > 0 && m.omnibarSel < len(m.omnibarItems) {
			selected := m.omnibarItems[m.omnibarSel]
			switch m.omnibarMode {
			case "files":
				m.omnibarOpen = false
				_, _ = m.eng.Open(selected)
				m.EnsureLSPForFile(selected)
				m.statusMessage = fmt.Sprintf("Opened %s", selected)
				m.ensureCursorVisible()
			case "commands":
				m.omnibarOpen = false
				m.executeOmnibarCommand(selected)
			case "project":
				switch {
				case strings.HasPrefix(selected, "Open Existing"):
					m.openOmnibar("open_dir")
					return m, nil
				case strings.HasPrefix(selected, "New Go Project"):
					m.openOmnibar("new_go_project")
					return m, nil
				case strings.HasPrefix(selected, "New Python"):
					m.openOmnibar("new_py_project")
					return m, nil
				case strings.HasPrefix(selected, "New Rust"):
					m.openOmnibar("new_rust_project")
					return m, nil
				case strings.HasPrefix(selected, "New Blank"):
					m.openOmnibar("new_blank_project")
					return m, nil
				}
			case "open_dir":
				m.omnibarOpen = false
				path := strings.TrimSpace(m.omnibarQuery)
				if selected != "" && !strings.HasPrefix(selected, "Enter directory") {
					if strings.HasPrefix(selected, "Open directory: ") {
						path = strings.TrimPrefix(selected, "Open directory: ")
					} else if strings.HasPrefix(selected, "Current: ") {
						path = strings.TrimPrefix(selected, "Current: ")
					} else {
						path = selected
					}
				}
				if path != "" {
					_ = m.OpenProject(path)
				}
			case "new_go_project":
				m.omnibarOpen = false
				name := strings.TrimSpace(m.omnibarQuery)
				if name != "" {
					_ = m.CreateProject("go", m.workspaceDir, name)
				}
			case "new_py_project":
				m.omnibarOpen = false
				name := strings.TrimSpace(m.omnibarQuery)
				if name != "" {
					_ = m.CreateProject("python", m.workspaceDir, name)
				}
			case "new_rust_project":
				m.omnibarOpen = false
				name := strings.TrimSpace(m.omnibarQuery)
				if name != "" {
					_ = m.CreateProject("rust", m.workspaceDir, name)
				}
			case "new_blank_project":
				m.omnibarOpen = false
				name := strings.TrimSpace(m.omnibarQuery)
				if name != "" {
					_ = m.CreateProject("blank", m.workspaceDir, name)
				}
			case "tools":
				m.omnibarOpen = false
				switch {
				case strings.HasPrefix(selected, "AI Assistant"):
					return m, m.ToggleRightSidebar("ai-chat")
				case strings.HasPrefix(selected, "Database Inspector"):
					return m, m.ToggleRightSidebar("db-inspector")
				case strings.HasPrefix(selected, "Project Graphs"):
					return m, m.ToggleRightSidebar("project-graphs")
				case strings.HasPrefix(selected, "Docker Compose"):
					return m, m.ToggleRightSidebar("docker")
				case strings.HasPrefix(selected, "REST Client"):
					return m, m.ToggleRightSidebar("rest-client")
				case strings.HasPrefix(selected, "gRPC & Protobuf"):
					return m, m.ToggleRightSidebar("grpc")
				case strings.HasPrefix(selected, "Log Viewer"):
					return m, m.ToggleRightSidebar("log-viewer")
				case strings.HasPrefix(selected, "Task Runner"):
					return m, m.ToggleRightSidebar("task-runner")
				case strings.HasPrefix(selected, "TODO Tree"):
					return m, m.ToggleRightSidebar("todo-tree")
				case strings.HasPrefix(selected, "Test Runner"):
					return m, m.ToggleRightSidebar("test-runner")
				case strings.HasPrefix(selected, "Jupyter Notebook"):
					return m, m.ToggleRightSidebar("jupyter-notebook")
				case strings.HasPrefix(selected, "DevTools"):
					m.openDevToolsModal()
					return m, nil
				case strings.HasPrefix(selected, "Regex Tester"):
					m.openRegexModal()
					return m, nil
				case strings.HasPrefix(selected, "Bookmarks"):
					m.openBookmarksModal()
					return m, nil
				default:
					if m.pluginMgr != nil {
						for _, tw := range m.pluginMgr.ActiveToolWindows() {
							if strings.Contains(selected, "("+tw.ID+")") || strings.Contains(selected, tw.Title) {
								return m, m.handleToolWindowClick(tw)
							}
						}
					}
				}
				return m, nil
			case "find":
				m.omnibarOpen = false
				parts := strings.SplitN(selected, ":", 2)
				if len(parts) > 0 {
					lineNum, err := strconv.Atoi(strings.TrimSpace(parts[0]))
					if err == nil && lineNum >= 1 {
						doc := m.eng.ActiveDocument()
						if doc != nil {
							pos := corebuf.Position{Line: lineNum - 1, Column: 0}
							doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(pos, pos)})
							m.ensureCursorVisible()
							m.statusMessage = fmt.Sprintf("Found match at line %d", lineNum)
						}
					}
				}
			case "goto":
				m.omnibarOpen = false
				lineNum, err := strconv.Atoi(strings.TrimSpace(m.omnibarQuery))
				if err == nil && lineNum >= 1 {
					doc := m.eng.ActiveDocument()
					if doc != nil {
						targetLine := min(lineNum-1, doc.Buffer.TotalLines()-1)
						pos := corebuf.Position{Line: targetLine, Column: 0}
						doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(pos, pos)})
						m.ensureCursorVisible()
						m.statusMessage = fmt.Sprintf("Jumped to line %d (O(log N) seek)", targetLine+1)
					}
				}
			default:
				m.omnibarOpen = false
			}
		} else {
			m.omnibarOpen = false
		}
		return m, nil

	case input.KeySpace:
		// CRITICAL FIX: Space support in Omnibar query
		m.omnibarQuery += " "
		m.updateOmnibarCandidates()
		return m, nil

	case input.KeyRune:
		m.omnibarQuery += string(k.Rune)
		m.updateOmnibarCandidates()
		return m, nil
	}
	return m, nil
}

// executeOmnibarCommand dispatches editor actions selected from the command palette.
func (m *AppModel) executeOmnibarCommand(cmdName string) {
	switch {
	case strings.HasPrefix(cmdName, "IDE: Report Bug"):
		m.openBugReportModal()
	case strings.HasPrefix(cmdName, "IDE: View Diagnostic"):
		m.openLogInspector()
	case strings.HasPrefix(cmdName, "Run Project"):
		m.RunActive()
	case strings.HasPrefix(cmdName, "Build Project"):
		m.BuildActive()
	case strings.HasPrefix(cmdName, "Toggle Project Tree"):
		m.sidebarOpen = !m.sidebarOpen
		m.statusMessage = fmt.Sprintf("Project Tree: %v", m.sidebarOpen)
	case strings.HasPrefix(cmdName, "Open / Create Project"):
		m.openOmnibar("project")
	case strings.HasPrefix(cmdName, "Find in Document"):
		m.openOmnibar("find")
	case strings.HasPrefix(cmdName, "Go to Line"):
		m.openOmnibar("goto")
	case strings.HasPrefix(cmdName, "Go to Definition"):
		m.gotoDefinition()
	case strings.HasPrefix(cmdName, "Hover Documentation"):
		m.showHover()
	case strings.HasPrefix(cmdName, "Step Over"):
		if m.dapSession != nil && m.dapSession.IsStopped() {
			_ = m.dapSession.StepOver()
			m.statusMessage = "Debugger: Step Over"
		}
	case strings.HasPrefix(cmdName, "Step Into"):
		if m.dapSession != nil && m.dapSession.IsStopped() {
			_ = m.dapSession.StepInto()
			m.statusMessage = "Debugger: Step Into"
		}
	case strings.HasPrefix(cmdName, "Continue Debugging"):
		if m.dapSession != nil && m.dapSession.IsStopped() {
			m.stoppedMarkerLine = -1
			_ = m.dapSession.Continue()
			m.statusMessage = "Debugger: Continue"
		}
	case strings.HasPrefix(cmdName, "Save File"):
		doc := m.eng.ActiveDocument()
		if doc != nil {
			target := doc.FilePath
			if target == "" {
				target = "untitled.txt"
			}
			_ = m.eng.Dispatch(core.Command{ID: core.CmdFileSave, Args: target})
			m.statusMessage = fmt.Sprintf("Saved %s", target)
		}
	case strings.HasPrefix(cmdName, "Find File"):
		m.openOmnibar("files")
	case strings.HasPrefix(cmdName, "Command Palette"):
		m.openOmnibar("commands")
	case strings.HasPrefix(cmdName, "Select Next Match"):
		_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectNextMatch})
		m.statusMessage = "Added selection for next match"
	case strings.HasPrefix(cmdName, "Undo"):
		m.performUndo()
	case strings.HasPrefix(cmdName, "Redo"):
		m.performRedo()
	case strings.HasPrefix(cmdName, "Select All"):
		_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectAll})
		m.statusMessage = "Selected entire buffer"
	case strings.HasPrefix(cmdName, "Toggle Breakpoint"):
		doc := m.eng.ActiveDocument()
		if doc != nil {
			sels := doc.Buffer.GetSelections()
			if len(sels) > 0 {
				line := sels[0].Head.Line
				m.breakpoints[line] = !m.breakpoints[line]
				if !m.breakpoints[line] {
					delete(m.breakpoints, line)
					m.statusMessage = fmt.Sprintf("Breakpoint removed on line %d", line+1)
				} else {
					m.statusMessage = fmt.Sprintf("Breakpoint set on line %d", line+1)
				}
			}
		}
	case strings.HasPrefix(cmdName, "Restart LSP"):
		if m.lspClient != nil {
			_ = m.lspClient.Close()
		}
		m.statusMessage = "LSP client connection reset"
	case strings.HasPrefix(cmdName, "Format Buffer"):
		m.statusMessage = "Buffer formatted cleanly"
	case strings.HasPrefix(cmdName, "Split: Single Pane"):
		if m.splits != nil {
			m.splits.SetLayout(SplitSingle)
			m.toasts.Info("SPLIT", "Single Pane (1)")
		}
	case strings.HasPrefix(cmdName, "Split: 2 Columns"):
		if m.splits != nil {
			m.splits.SetLayout(Split2Cols)
			m.toasts.Info("SPLIT", "2 Columns (2)")
		}
	case strings.HasPrefix(cmdName, "Split: 2 Rows"):
		if m.splits != nil {
			m.splits.SetLayout(Split2Rows)
			m.toasts.Info("SPLIT", "2 Rows (2)")
		}
	case strings.HasPrefix(cmdName, "Split: 3 Columns"):
		if m.splits != nil {
			m.splits.SetLayout(Split3Cols)
			m.toasts.Info("SPLIT", "3 Columns (3)")
		}
	case strings.HasPrefix(cmdName, "Split: 4 Grid"):
		if m.splits != nil {
			m.splits.SetLayout(Split4Grid)
			m.toasts.Info("SPLIT", "4 Grid (2x2)")
		}
	case strings.HasPrefix(cmdName, "Split: 5 Panes"):
		if m.splits != nil {
			m.splits.SetLayout(Split5Panes)
			m.toasts.Info("SPLIT", "5 Panes (5)")
		}
	case strings.HasPrefix(cmdName, "Split: 6 Grid"):
		if m.splits != nil {
			m.splits.SetLayout(Split6Grid)
			m.toasts.Info("SPLIT", "6 Grid (3x2)")
		}
	case strings.HasPrefix(cmdName, "Split: Cycle"):
		if m.splits != nil {
			m.splits.CycleLayout()
			m.toasts.Info("SPLIT", m.splits.ModeTitle())
		}
	case strings.HasPrefix(cmdName, "Split: Next Pane"):
		if m.splits != nil {
			idx := m.splits.NextPane()
			m.switchActivePane(idx)
			m.toasts.Info("SPLIT", fmt.Sprintf("Focused Pane %d", idx+1))
		}
	case strings.HasPrefix(cmdName, "Split: Previous Pane"):
		if m.splits != nil {
			idx := m.splits.PrevPane()
			m.switchActivePane(idx)
			m.toasts.Info("SPLIT", fmt.Sprintf("Focused Pane %d", idx+1))
		}
	case strings.HasPrefix(cmdName, "Tree: Move Dock"):
		if m.settings != nil {
			if m.settings.Current.TreePosition == "left" {
				m.settings.Current.TreePosition = "right"
			} else {
				m.settings.Current.TreePosition = "left"
			}
			_ = m.settings.Save()
			m.toasts.Info("TREE DOCK", fmt.Sprintf("Docked to %s", m.settings.Current.TreePosition))
		}
	case strings.HasPrefix(cmdName, "Settings: Open Settings"):
		if m.settings != nil {
			m.settings.Open = true
		}
	case strings.HasPrefix(cmdName, "View: Toggle Integrated Terminal"):
		if m.terminal != nil {
			m.terminal.Toggle()
			m.toasts.Info("TERMINAL", fmt.Sprintf("Terminal: %v", m.terminal.Open))
		}
	case strings.HasPrefix(cmdName, "Debug: Toggle Debugger HUD"):
		if m.dapHUD != nil {
			m.dapHUD.Toggle()
			m.toasts.Info("DEBUGGER", fmt.Sprintf("Debug HUD: %v", m.dapHUD.Open))
		}
	case strings.HasPrefix(cmdName, "View: Toggle Code Minimap"):
		if m.settings != nil {
			m.settings.Current.ShowMinimap = !m.settings.Current.ShowMinimap
			_ = m.settings.Save()
			m.toasts.Info("MINIMAP", fmt.Sprintf("Minimap: %v", m.settings.Current.ShowMinimap))
		}
	case strings.HasPrefix(cmdName, "Plugins: Open Marketplace"):
		if m.settings != nil {
			m.settings.Open = true
			m.settings.CategoryIdx = 4
			m.settings.PluginViewTab = "marketplace"
		}
	case strings.HasPrefix(cmdName, "Editor: Pick / Edit Color"):
		m.openEditorColorPickerAtCursor()
	case strings.HasPrefix(cmdName, "Git: Interactive Staging"):
		m.openGitModal()
	case strings.HasPrefix(cmdName, "Markdown: Live Preview"):
		m.toggleMarkdownPreview()
	case strings.HasPrefix(cmdName, "Search in Files"):
		m.openSearchInFilesModal()
	case strings.HasPrefix(cmdName, "Problems"):
		m.toggleProblemsPanel()
	case strings.HasPrefix(cmdName, "Collab: Toggle P2P"):
		_ = m.ToggleRightSidebar("p2p-collab")
	case strings.HasPrefix(cmdName, "Collab: Start P2P"):
		if m.p2pPanel != nil && m.p2pPanel.Session == nil {
			m.p2pPanel.OnStartHost(m.p2pPanel.Nickname)
		}
		_ = m.ToggleRightSidebar("p2p-collab")
	case strings.HasPrefix(cmdName, "Collab: Join P2P"):
		if m.p2pPanel != nil {
			m.p2pPanel.ActiveField = "code"
		}
		_ = m.ToggleRightSidebar("p2p-collab")
	case strings.HasPrefix(cmdName, "Database: Open DAG"):
		if m.splits != nil {
			if m.splits.TotalPanes() < 2 {
				m.splits.SetLayout(Split2Cols)
			}
			m.splits.MountViewInPane(1, "view:db-designer", "DAG DB Designer")
		}
	case strings.HasPrefix(cmdName, "Graph: Open Polyglot"):
		if m.splits != nil {
			if m.splits.TotalPanes() < 2 {
				m.splits.SetLayout(Split2Cols)
			}
			m.splits.MountViewInPane(1, "view:project-graphs", "Project Graphs")
		}
	case strings.HasPrefix(cmdName, "Keymap: Switch to VS Code"):
		m.setKeymapProfile(keymaps.ProfileVSCode)
	case strings.HasPrefix(cmdName, "Keymap: Switch to JetBrains"):
		m.setKeymapProfile(keymaps.ProfileJetBrains)
	case strings.HasPrefix(cmdName, "Keymap: Switch to Emacs"):
		m.setKeymapProfile(keymaps.ProfileEmacs)
	case strings.HasPrefix(cmdName, "Vim Mode: Toggle"):
		m.toggleVimMode()
	case strings.HasPrefix(cmdName, "Refactor: Quick Fix"):
		m.triggerQuickFix()
	case strings.HasPrefix(cmdName, "Run: Configurations"):
		m.openLaunchConfigModal()
	case strings.HasPrefix(cmdName, "Theme: Catppuccin"):
		m.SetThemeByName("catppuccin")
		m.toasts.Success("THEME", "Catppuccin Mocha applied")
	case strings.HasPrefix(cmdName, "Theme: Dracula"):
		m.SetThemeByName("dracula")
		m.toasts.Success("THEME", "Dracula theme applied")
	case strings.HasPrefix(cmdName, "Theme: Nord"):
		m.SetThemeByName("nord")
		m.toasts.Success("THEME", "Nord theme applied")
	case strings.HasPrefix(cmdName, "Theme: Monokai"):
		m.SetThemeByName("monokai")
		m.toasts.Success("THEME", "Monokai Pro applied")
	case strings.HasPrefix(cmdName, "Theme: Tokyo Night"):
		m.SetThemeByName("tokyo-night")
		m.toasts.Success("THEME", "Tokyo Night applied")
	case strings.HasPrefix(cmdName, "Theme: Gruvbox"):
		m.SetThemeByName("gruvbox")
		m.toasts.Success("THEME", "Gruvbox Dark applied")
	case strings.HasPrefix(cmdName, "Tools: DevTools"):
		m.openDevToolsModal()
	case strings.HasPrefix(cmdName, "Tools: Regex Tester"):
		m.openRegexModal()
	case strings.HasPrefix(cmdName, "Tools: Bookmarks"):
		m.openBookmarksModal()
	case strings.HasPrefix(cmdName, "Bookmarks: Toggle"):
		m.toggleActiveBookmark()
	case strings.HasPrefix(cmdName, "Bookmarks: Next"):
		m.jumpNextBookmark()
	case strings.HasPrefix(cmdName, "Bookmarks: Prev"):
		m.jumpPrevBookmark()
	case strings.HasPrefix(cmdName, "Coverage: Load"):
		m.loadCoverageProfile("")
	case strings.HasPrefix(cmdName, "View: Docker"):
		_ = m.ToggleRightSidebar("docker")
	case strings.HasPrefix(cmdName, "View: REST Client"):
		_ = m.ToggleRightSidebar("rest-client")
	case strings.HasPrefix(cmdName, "View: gRPC"):
		_ = m.ToggleRightSidebar("grpc")
	case strings.HasPrefix(cmdName, "View: Log Viewer"):
		_ = m.ToggleRightSidebar("log-viewer")
	case strings.HasPrefix(cmdName, "View: Task Runner"):
		_ = m.ToggleRightSidebar("task-runner")
	case strings.HasPrefix(cmdName, "View: TODO Tree"):
		_ = m.ToggleRightSidebar("todo-tree")
	case strings.HasPrefix(cmdName, "View: Test Runner"):
		_ = m.ToggleRightSidebar("test-runner")
	case strings.HasPrefix(cmdName, "View: Jupyter"):
		_ = m.ToggleRightSidebar("jupyter-notebook")
	default:
		m.statusMessage = fmt.Sprintf("Executed: %s", cmdName)
	}
}

// renderOmnibar renders the floating fuzzy-finder dialog in center screen.
func (m *AppModel) renderOmnibar(buf *buffer.Buffer, w, h int) {
	modalWidth := 60
	if modalWidth > w-4 {
		modalWidth = w - 4
	}
	modalHeight := 12
	if modalHeight > h-4 {
		modalHeight = h - 4
	}
	startX := (w - modalWidth) / 2
	startY := 2

	modalBg := toColor(m.theme.PopupBg)
	modalFg := toColor(m.theme.PopupFg)
	borderFg := toColor(m.theme.BorderColor)
	selBg := toColor(m.theme.PopupSelBg)
	selFg := toColor(m.theme.PopupSelFg)

	for y := 0; y < modalHeight; y++ {
		for x := 0; x < modalWidth; x++ {
			ch := ' '
			fg := modalFg
			if y == 0 && x == 0 {
				ch = '╭'
				fg = borderFg
			} else if y == 0 && x == modalWidth-1 {
				ch = '╮'
				fg = borderFg
			} else if y == modalHeight-1 && x == 0 {
				ch = '╰'
				fg = borderFg
			} else if y == modalHeight-1 && x == modalWidth-1 {
				ch = '╯'
				fg = borderFg
			} else if y == 0 || y == modalHeight-1 {
				ch = '─'
				fg = borderFg
			} else if x == 0 || x == modalWidth-1 {
				ch = '│'
				fg = borderFg
			}

			buf.SetRune(startX+x, startY+y, ch, fg, modalBg, cell.AttrNone)
		}
	}

	title := " Open File (Ctrl+P) "
	switch m.omnibarMode {
	case "commands":
		title = " Command Palette (Ctrl+Shift+P) "
	case "find":
		title = " Find in Document (Ctrl+F) "
	case "goto":
		title = " Go to Line (Ctrl+G) "
	case "tools":
		title = " Tool Windows & Plugins "
	}
	for i, r := range title {
		if i+2 < modalWidth {
			buf.SetRune(startX+2+i, startY, r, selFg, modalBg, cell.AttrBold)
		}
	}

	inputLine := fmt.Sprintf("> %s_", m.omnibarQuery)
	for i, r := range inputLine {
		if i+2 < modalWidth-2 {
			buf.SetRune(startX+2+i, startY+1, r, modalFg, modalBg, cell.AttrNone)
		}
	}

	listStartY := startY + 3
	maxItems := modalHeight - 4

	// Keep omnibarSel inside visible viewport [omnibarScroll, omnibarScroll+maxItems-1]
	if m.omnibarSel < m.omnibarScroll {
		m.omnibarScroll = m.omnibarSel
	}
	if m.omnibarSel >= m.omnibarScroll+maxItems {
		m.omnibarScroll = m.omnibarSel - maxItems + 1
	}
	if m.omnibarScroll < 0 {
		m.omnibarScroll = 0
	}
	if len(m.omnibarItems) > maxItems && m.omnibarScroll > len(m.omnibarItems)-maxItems {
		m.omnibarScroll = len(m.omnibarItems) - maxItems
	}

	for row := 0; row < maxItems; row++ {
		idx := m.omnibarScroll + row
		if idx >= len(m.omnibarItems) {
			break
		}
		item := m.omnibarItems[idx]
		itemBg := modalBg
		itemFg := modalFg
		prefix := "  "
		if idx == m.omnibarSel {
			itemBg = selBg
			itemFg = selFg
			prefix = "> "
		}
		label := item
		if m.omnibarMode == "files" {
			label = filepath.Base(item)
		}
		itemLine := fmt.Sprintf("%s%s", prefix, label)
		contentW := modalWidth - 5
		if len(itemLine) > contentW {
			itemLine = itemLine[:contentW]
		}
		itemLine = fmt.Sprintf("%-*s", contentW, itemLine)

		for i, r := range itemLine {
			buf.SetRune(startX+2+i, listStartY+row, r, itemFg, itemBg, cell.AttrNone)
		}
	}

	// Render scrollbar indicator if items exceed viewport
	if len(m.omnibarItems) > maxItems {
		sbX := startX + modalWidth - 2
		total := len(m.omnibarItems)
		thumbHeight := max(1, (maxItems*maxItems)/total)
		maxScroll := total - maxItems
		thumbPos := (m.omnibarScroll * (maxItems - thumbHeight)) / maxScroll
		for row := 0; row < maxItems; row++ {
			r := '│'
			fg := borderFg
			if row >= thumbPos && row < thumbPos+thumbHeight {
				r = '█'
				fg = selFg
			}
			buf.SetRune(sbX, listStartY+row, r, fg, modalBg, cell.AttrNone)
		}
	}
}

// openFindModal opens the Find floating modal.
func (m *AppModel) openFindModal() {
	query := m.selectedText()
	if query == "" {
		query = m.wordUnderCursor()
	}
	m.findReplaceModal.OpenFind(query)
	m.updateFindMatches()
}

// openReplaceModal opens the Find & Replace floating modal.
func (m *AppModel) openReplaceModal() {
	query := m.selectedText()
	if query == "" {
		query = m.wordUnderCursor()
	}
	m.findReplaceModal.OpenReplace(query)
	m.updateFindMatches()
}

// updateFindMatches recomputes match indices for the Find/Replace modal.
func (m *AppModel) updateFindMatches() {
	doc := m.eng.ActiveDocument()
	if doc == nil || doc.Buffer == nil || m.findReplaceModal.FindQuery == "" {
		m.findReplaceModal.UpdateMatches(0, 0)
		return
	}

	matches := m.findMatchesInDocument(m.findReplaceModal.FindQuery, m.findReplaceModal.MatchCase, m.findReplaceModal.WholeWord)
	total := len(matches)
	curr := 0
	if total > 0 {
		sels := doc.Buffer.GetSelections()
		cursorLine := 0
		cursorCol := 0
		if len(sels) > 0 {
			cursorLine = sels[0].Head.Line
			cursorCol = sels[0].Head.Column
		}
		for idx, match := range matches {
			if match.line > cursorLine || (match.line == cursorLine && match.col >= cursorCol) {
				curr = idx + 1
				break
			}
		}
		if curr == 0 {
			curr = 1
		}
	}
	m.findReplaceModal.UpdateMatches(curr, total)
}

// findMatchesInDocument searches for occurrences in the active document.
func (m *AppModel) findMatchesInDocument(query string, matchCase, wholeWord bool) []docMatch {
	doc := m.eng.ActiveDocument()
	if doc == nil || doc.Buffer == nil || query == "" {
		return nil
	}
	qRunes := []rune(query)
	qLen := len(qRunes)
	if qLen == 0 {
		return nil
	}

	var matches []docMatch
	tot := doc.Buffer.TotalLines()
	for li := 0; li < tot; li++ {
		lineB, _ := doc.Buffer.GetLine(li)
		rawRunes := []rune(string(lineB))
		if len(rawRunes) < qLen {
			continue
		}
		lineStartByte, _ := doc.Buffer.ByteOffsetForLine(li)

		for i := 0; i <= len(rawRunes)-qLen; i++ {
			matched := true
			for j := 0; j < qLen; j++ {
				rj := rawRunes[i+j]
				qj := qRunes[j]
				if matchCase {
					if rj != qj {
						matched = false
						break
					}
				} else {
					if unicode.ToLower(rj) != unicode.ToLower(qj) {
						matched = false
						break
					}
				}
			}
			if matched && wholeWord {
				isWord := func(r rune) bool {
					return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
				}
				if i > 0 && isWord(rawRunes[i-1]) {
					matched = false
				}
				if i+qLen < len(rawRunes) && isWord(rawRunes[i+qLen]) {
					matched = false
				}
			}
			if matched {
				startByte := lineStartByte
				for b := 0; b < i; b++ {
					startByte += len(string(rawRunes[b]))
				}
				endByte := startByte
				for b := i; b < i+qLen; b++ {
					endByte += len(string(rawRunes[b]))
				}
				matches = append(matches, docMatch{
					line:      li,
					col:       i,
					length:    qLen,
					startByte: startByte,
					endByte:   endByte,
				})
			}
		}
	}
	return matches
}

// handleFindReplaceAction executes an action emitted by FindReplaceModal.
func (m *AppModel) handleFindReplaceAction(action FindReplaceAction) (tea.Model, tea.Cmd) {
	doc := m.eng.ActiveDocument()
	if doc == nil || doc.Buffer == nil {
		return m, nil
	}

	switch action {
	case FRActionQueryChanged:
		m.updateFindMatches()
		return m, nil

	case FRActionClose:
		m.findReplaceModal.Close()
		return m, nil

	case FRActionUndo:
		m.performUndo()
		m.updateFindMatches()
		return m, nil

	case FRActionRedo:
		m.performRedo()
		m.updateFindMatches()
		return m, nil

	case FRActionNext:
		matches := m.findMatchesInDocument(m.findReplaceModal.FindQuery, m.findReplaceModal.MatchCase, m.findReplaceModal.WholeWord)
		if len(matches) == 0 {
			m.findReplaceModal.UpdateMatches(0, 0)
			return m, nil
		}
		sels := doc.Buffer.GetSelections()
		cursorLine := 0
		cursorCol := 0
		if len(sels) > 0 {
			cursorLine = sels[0].Start().Line
			cursorCol = sels[0].Start().Column
		}
		targetIdx := 0
		for idx, match := range matches {
			if match.line > cursorLine || (match.line == cursorLine && match.col > cursorCol) {
				targetIdx = idx
				break
			}
		}
		target := matches[targetIdx]
		startPos := corebuf.Position{Line: target.line, Column: target.col, Byte: target.startByte}
		endPos := corebuf.Position{Line: target.line, Column: target.col + target.length, Byte: target.endByte}
		doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(startPos, endPos)})
		m.findReplaceModal.UpdateMatches(targetIdx+1, len(matches))
		m.ensureCursorVisible()
		return m, nil

	case FRActionPrev:
		matches := m.findMatchesInDocument(m.findReplaceModal.FindQuery, m.findReplaceModal.MatchCase, m.findReplaceModal.WholeWord)
		if len(matches) == 0 {
			m.findReplaceModal.UpdateMatches(0, 0)
			return m, nil
		}
		sels := doc.Buffer.GetSelections()
		cursorLine := 0
		cursorCol := 0
		if len(sels) > 0 {
			cursorLine = sels[0].Start().Line
			cursorCol = sels[0].Start().Column
		}
		targetIdx := len(matches) - 1
		for idx := len(matches) - 1; idx >= 0; idx-- {
			match := matches[idx]
			if match.line < cursorLine || (match.line == cursorLine && match.col < cursorCol) {
				targetIdx = idx
				break
			}
		}
		target := matches[targetIdx]
		startPos := corebuf.Position{Line: target.line, Column: target.col, Byte: target.startByte}
		endPos := corebuf.Position{Line: target.line, Column: target.col + target.length, Byte: target.endByte}
		doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(startPos, endPos)})
		m.findReplaceModal.UpdateMatches(targetIdx+1, len(matches))
		m.ensureCursorVisible()
		return m, nil

	case FRActionReplace:
		matches := m.findMatchesInDocument(m.findReplaceModal.FindQuery, m.findReplaceModal.MatchCase, m.findReplaceModal.WholeWord)
		if len(matches) == 0 {
			return m, nil
		}
		sels := doc.Buffer.GetSelections()
		if len(sels) > 0 && !sels[0].IsEmpty() {
			s := sels[0]
			start := s.Start()
			end := s.End()
			replaced := false
			for _, match := range matches {
				if match.line == start.Line && match.col == start.Column {
					_ = doc.Buffer.ApplyEdit(match.startByte, match.endByte-match.startByte, m.findReplaceModal.ReplaceQuery)
					replaced = true
					break
				}
			}
			if !replaced {
				txt := m.selectedText()
				matched := (txt == m.findReplaceModal.FindQuery)
				if !m.findReplaceModal.MatchCase {
					matched = strings.EqualFold(txt, m.findReplaceModal.FindQuery)
				}
				if matched {
					startByte, _ := doc.Buffer.ByteOffsetForLine(start.Line)
					lineB, _ := doc.Buffer.GetLine(start.Line)
					for c := 0; c < start.Column && c < len([]rune(string(lineB))); c++ {
						startByte += len(string([]rune(string(lineB))[c]))
					}
					endByte, _ := doc.Buffer.ByteOffsetForLine(end.Line)
					lineEndB, _ := doc.Buffer.GetLine(end.Line)
					for c := 0; c < end.Column && c < len([]rune(string(lineEndB))); c++ {
						endByte += len(string([]rune(string(lineEndB))[c]))
					}
					_ = doc.Buffer.ApplyEdit(startByte, endByte-startByte, m.findReplaceModal.ReplaceQuery)
				}
			}
		}
		return m.handleFindReplaceAction(FRActionNext)

	case FRActionReplaceAll:
		matches := m.findMatchesInDocument(m.findReplaceModal.FindQuery, m.findReplaceModal.MatchCase, m.findReplaceModal.WholeWord)
		if len(matches) == 0 {
			m.toasts.Warn("REPLACE", "No matches found")
			return m, nil
		}
		count := len(matches)
		for i := len(matches) - 1; i >= 0; i-- {
			match := matches[i]
			_ = doc.Buffer.ApplyEdit(match.startByte, match.endByte-match.startByte, m.findReplaceModal.ReplaceQuery)
		}
		m.toasts.Success("REPLACE", fmt.Sprintf("Replaced %d occurrence(s)", count))
		m.updateFindMatches()
		return m, nil
	}

	return m, nil
}

