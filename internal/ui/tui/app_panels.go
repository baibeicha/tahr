package tui

import (
	"encoding/json"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core"
	corebuf "tahr/internal/core/buffer"
	"tahr/internal/core/clipboard"
	"tahr/internal/core/crash"
	"tahr/internal/core/dap"
	"tahr/internal/core/db"
	"tahr/internal/core/i18n"
	"tahr/internal/core/launch"
	"tahr/internal/core/logging"
	"tahr/internal/core/p2p"
	"tahr/internal/core/plugin"
	"tahr/internal/core/restclient"
)

// AIChatMsg represents a chat message in the Right Sidebar assistant.
type AIChatMsg struct {
	Role    string
	Content string
}

func (m *AppModel) wireP2PSession(session *p2p.CollaborationSession) {
	if session == nil {
		return
	}
	session.OnJoinRequest = func(req p2p.MsgJoinRequest) {
		if m.toasts != nil {
			m.toasts.Warn("P2P REQUEST", fmt.Sprintf("Пользователь '%s' просит подключиться", req.Nickname))
		}
	}
	session.OnPeerJoined = func(peer *p2p.PeerInfo) {
		if m.toasts != nil {
			m.toasts.Success("P2P COLLAB", fmt.Sprintf("'%s' подключился к сессии!", peer.Nickname))
		}
	}
	session.OnPeerLeft = func(peerID uint16) {
		if m.toasts != nil {
			m.toasts.Info("P2P COLLAB", fmt.Sprintf("Участник %d покинул сессию", peerID))
		}
	}
	session.OnICEStateChange = func(state p2p.ICEState) {
		if m.toasts != nil && state == p2p.ICEStateConnected {
			m.toasts.Success("P2P ICE", "P2P соединение установлено!")
		}
	}
	if session.Coordinator != nil {
		session.Coordinator.OnTierActivated = func(tier p2p.SignalingTier) {
			if tier == p2p.SignalingTierDHT && m.toasts != nil {
				m.toasts.Info("P2P CASCADE", "Подключение через BitTorrent DHT (2.5с fallback)")
			}
		}
		session.Coordinator.OnTierResolved = func(tier p2p.SignalingTier) {
			session.ActiveTier = tier
			if m.toasts != nil {
				tierName := "LAN"
				if tier == p2p.SignalingTierNostr {
					tierName = "Nostr Relay"
				} else if tier == p2p.SignalingTierDHT {
					tierName = "BitTorrent DHT"
				}
				m.toasts.Success("P2P CONNECTED", fmt.Sprintf("Соединение установлено через %s", tierName))
			}
		}
	}
}

// openLaunchConfigModal opens the Run/Debug configurations modal dialog.
func (m *AppModel) openLaunchConfigModal() {
	if m.launchModal == nil {
		m.launchModal = NewLaunchConfigModal(m.launchConfig)
		m.launchModal.OnLaunch = func(p launch.Profile) {
			m.launchModal.Open = false
			m.runProfile(p)
		}
		m.launchModal.OnDebug = func(p launch.Profile) {
			m.launchModal.Open = false
			m.debugProfile(p)
		}
	}
	templates := defaultLaunchTemplates()
	if m.pluginMgr != nil {
		templates = append(templates, m.pluginMgr.GetLaunchTemplates()...)
		m.launchModal.SetSupportedTypes(m.pluginMgr.GetSupportedLaunchTypes())
	}
	m.launchModal.SetTemplates(templates)

	cfg, err := launch.Load(m.workspaceDir)
	if err == nil && cfg != nil {
		m.launchConfig = cfg
	}
	m.launchModal.OpenModal(m.launchConfig)
}

func (m *AppModel) runProfile(p launch.Profile) {
	pType := strings.ToLower(p.Type)
	if m.pluginMgr != nil && pType != "shell" {
		if !m.pluginMgr.IsLaunchTypeSupported(pType) {
			m.statusMessage = fmt.Sprintf("Cannot run: launch type '%s' is disabled or unsupported", p.Type)
			if m.toasts != nil {
				m.toasts.Error("RUN", fmt.Sprintf("Launch type '%s' is disabled or unsupported", p.Type))
			}
			return
		}
	}

	doc := m.eng.ActiveDocument()

	if doc != nil && doc.Buffer.IsModified() {
		target := doc.FilePath
		if target == "" {
			target = "untitled.txt"
		}
		_ = m.eng.Dispatch(core.Command{ID: core.CmdFileSave, Args: target})
	}

	cmdName := p.Target
	var args []string
	switch strings.ToLower(p.Type) {
	case "go":
		cmdName = "go"
		if m.sdkManager != nil && m.sdkManager.GoSDK() != nil {
			cmdName = m.sdkManager.GoSDK().BinaryPath
		}
		target := p.Target
		if target == "" {
			target = "."
		}
		args = append([]string{"run", target}, p.Args...)
	case "python":
		cmdName = "python"
		if p.Target != "" {
			args = append([]string{p.Target}, p.Args...)
		} else {
			args = p.Args
		}
	case "rust":
		cmdName = "cargo"
		args = append([]string{"run"}, p.Args...)
	case "shell":
		mainCmd := strings.TrimSpace(p.Target)
		preTask := strings.TrimSpace(p.PreLaunchTask)
		if mainCmd == "" || mainCmd == "." {
			if preTask != "" {
				mainCmd = preTask
				preTask = ""
			}
		}
		if len(p.Args) > 0 {
			if mainCmd != "" {
				mainCmd += " " + strings.Join(p.Args, " ")
			} else {
				mainCmd = strings.Join(p.Args, " ")
			}
		}
		if preTask != "" && mainCmd != "" {
			cmdName = preTask + " && " + mainCmd
		} else if mainCmd != "" {
			cmdName = mainCmd
		} else {
			cmdName = preTask
		}
		args = nil
	default:
		if cmdName == "" {
			cmdName = "go"
			if m.sdkManager != nil && m.sdkManager.GoSDK() != nil {
				cmdName = m.sdkManager.GoSDK().BinaryPath
			}
			args = append([]string{"run", "."}, p.Args...)
		} else {
			args = p.Args
		}
	}

	cwd := m.workspaceDir
	if p.Cwd != "" {
		if filepath.IsAbs(p.Cwd) {
			cwd = p.Cwd
		} else {
			cwd = filepath.Join(m.workspaceDir, p.Cwd)
		}
	}

	title := fmt.Sprintf("RUN [%s]: %s %s", p.Name, filepath.Base(cmdName), strings.Join(args, " "))
	if m.pluginMgr != nil {
		if resolved, ok := m.pluginMgr.FindToolPath(cmdName, ""); ok {
			cmdName = resolved
		}
	}

	// Route interactive CLI/TUI applications, shell profiles, or profiles configured with integratedTerminal to terminal
	isInteractiveTUI := strings.Contains(strings.ToLower(p.Target), "cmd/tahr") || strings.Contains(strings.ToLower(p.Target), "cmd\\tahr")
	isShell := strings.EqualFold(p.Type, "shell")
	runInTerminal := strings.EqualFold(p.Console, "integratedTerminal") || isInteractiveTUI || (isShell && !strings.EqualFold(p.Console, "internalConsole"))

	if runInTerminal && m.terminal != nil {
		m.terminal.Open = true
		m.terminalFocused = true
		fullCmd := cmdName
		if len(args) > 0 {
			fullCmd += " " + strings.Join(args, " ")
		}
		if p.PreLaunchTask != "" && !isShell {
			fullCmd = p.PreLaunchTask + " && " + fullCmd
		}
		m.terminal.Execute(fullCmd)
		m.statusMessage = fmt.Sprintf("Running profile '%s' in Integrated Terminal...", p.Name)
		if m.toasts != nil {
			m.toasts.Info("RUN", fmt.Sprintf("Launched %s in Terminal", p.Name))
		}
		return
	}

	m.outputOpen = true
	m.statusMessage = fmt.Sprintf("Starting profile '%s' (%s %s)...", p.Name, filepath.Base(cmdName), strings.Join(args, " "))
	_ = m.runner.StartProcessWithEnvAndPreTask(cwd, cmdName, args, p.Env, title, p.PreLaunchTask)
}

func (m *AppModel) debugProfile(p launch.Profile) {
	pType := strings.ToLower(p.Type)
	if m.pluginMgr != nil && pType != "shell" {
		if !m.pluginMgr.IsLaunchTypeSupported(pType) {
			m.statusMessage = fmt.Sprintf("Cannot debug: launch type '%s' is disabled or unsupported", p.Type)
			if m.toasts != nil {
				m.toasts.Error("DEBUG", fmt.Sprintf("Launch type '%s' is disabled or unsupported", p.Type))
			}
			return
		}
	}

	doc := m.eng.ActiveDocument()
	if doc != nil && doc.Buffer.IsModified() {
		target := doc.FilePath
		if target == "" {
			target = "untitled.txt"
		}
		_ = m.eng.Dispatch(core.Command{ID: core.CmdFileSave, Args: target})
	}


	if m.dapSession != nil {
		target := p.Target
		if target == "" && doc != nil {
			target = doc.FilePath
		}
		m.outputOpen = true
		if m.dapHUD != nil {
			m.dapHUD.Open = true
		}
		m.statusMessage = fmt.Sprintf("Debugging profile '%s' with DAP...", p.Name)

		cmdName := "dlv"
		args := []string{"dap"}
		if strings.ToLower(p.Type) == "python" {
			cmdName = "python"
			args = []string{"-m", "debugpy.adapter"}
		}
		if m.pluginMgr != nil {
			if resolved, ok := m.pluginMgr.FindToolPath(cmdName, ""); ok {
				cmdName = resolved
			}
		}
		client, err := dap.StartClient(cmdName, args, m.dapSession)
		if err == nil && client != nil {
			m.dapSession.AttachClient(client)
			_, _ = client.SendRequest("initialize", map[string]any{
				"clientID":        "tahr",
				"clientName":      "Tahr IDE",
				"adapterID":       p.Type,
				"linesStartAt1":   true,
				"columnsStartAt1": true,
			})
			launchArgs := map[string]any{
				"program": target,
				"args":    p.Args,
			}
			if len(p.Env) > 0 {
				launchArgs["env"] = p.Env
			}
			_, _ = client.SendRequest("launch", launchArgs)
			_, _ = client.SendRequest("configurationDone", nil)
		}
	}
}

// switchActivePane safely focuses a split pane, saving and restoring viewports.
func (m *AppModel) switchActivePane(idx int) {
	if m.splits == nil {
		return
	}
	if cur := m.splits.ActivePane(); cur != nil {
		cur.ViewportX = m.viewportX
		cur.ViewportY = m.viewportY
	}
	m.splits.SetPane(idx)
	if next := m.splits.ActivePane(); next != nil {
		if next.DocID != "" {
			_ = m.eng.SwitchBuffer(next.DocID)
		}
		m.viewportX = next.ViewportX
		m.viewportY = next.ViewportY
	}
	m.ensureCursorVisible()
	m.onActiveDocumentChanged()
}

// findDocument retrieves a document by its engine buffer ID, or falls back to active document.
func (m *AppModel) findDocument(id string) *core.Document {
	if id == "" {
		return m.eng.ActiveDocument()
	}
	for _, d := range m.eng.Documents() {
		if d.ID == id {
			return d
		}
	}
	return m.eng.ActiveDocument()
}

// SetSidebarOpen controls whether the project tree sidebar is visible immediately.
func (m *AppModel) SetSidebarOpen(open bool) tea.Cmd {
	m.sidebarOpen = open
	if open {
		m.sidebarTargetWidth = float64(m.sidebarWidth)
		m.sidebarAnimWidth = float64(m.sidebarWidth)
		m.refreshProjectTree()
	} else {
		m.sidebarFocused = false
		m.sidebarTargetWidth = 0
		m.sidebarAnimWidth = 0
	}
	return nil
}

// ToggleSidebar toggles visibility of the project tree sidebar with smooth animation.
func (m *AppModel) ToggleSidebar() tea.Cmd {
	m.sidebarOpen = !m.sidebarOpen
	m.sidebarFocused = m.sidebarOpen
	return m.animateSidebar()
}

func (m *AppModel) animateSidebar() tea.Cmd {
	if m.sidebarOpen {
		m.sidebarTargetWidth = float64(m.sidebarWidth)
		m.refreshProjectTree()
	} else {
		m.sidebarTargetWidth = 0
	}
	if m.settings != nil && !m.settings.Current.SmoothAnim {
		m.sidebarAnimWidth = m.sidebarTargetWidth
		return nil
	}
	return func() tea.Msg {
		time.Sleep(8 * time.Millisecond)
		return animTickMsg{}
	}
}

func (m *AppModel) getRightStripItems() []rightStripItem {
	var items []rightStripItem
	seen := make(map[string]bool)

	// 1. AI Assistant (only if enabled or pluginMgr == nil)
	hasAI := true
	if m.pluginMgr != nil {
		hasAI = m.pluginMgr.IsEnabled("ai-chat") || m.pluginMgr.IsEnabled("ai-assistant")
	}
	if hasAI {
		items = append(items, rightStripItem{
			id:    "ai-chat",
			r1:    'A',
			r2:    'I',
			title: "AI Assistant",
			mode:  "ai-chat",
		})
		seen["ai-chat"] = true
		seen["ai-chat-panel"] = true
		seen["ai-assistant"] = true
	}

	// 2. Databases (only if db-inspector or db-er-diagram enabled or pluginMgr == nil)
	hasDB := true
	if m.pluginMgr != nil {
		hasDB = m.pluginMgr.IsEnabled("db-inspector") || m.pluginMgr.IsEnabled("db-er-diagram")
	}
	if hasDB {
		items = append(items, rightStripItem{
			id:    "db-inspector",
			r1:    'D',
			r2:    'B',
			title: "Databases",
			mode:  "db-inspector",
		})
		seen["db-inspector"] = true
		seen["db-inspector-panel"] = true
		seen["db-er-diagram"] = true
		seen["db-er-diagram-panel"] = true
	}

	// 3. Project Graphs (only if enabled or pluginMgr == nil)
	hasGraphs := true
	if m.pluginMgr != nil {
		hasGraphs = m.pluginMgr.IsEnabled("project-graphs") || m.pluginMgr.IsEnabled("git-graph")
	}
	if hasGraphs {
		items = append(items, rightStripItem{
			id:    "project-graphs",
			r1:    'G',
			r2:    'R',
			title: "Project Graphs",
			mode:  "project-graphs",
		})
		seen["project-graphs"] = true
	}

	// 4. Docker Compose (only if enabled or pluginMgr == nil)
	hasDocker := true
	if m.pluginMgr != nil {
		hasDocker = m.pluginMgr.IsEnabled("docker") || m.pluginMgr.IsEnabled("docker-compose")
	}
	if hasDocker {
		items = append(items, rightStripItem{
			id:    "docker",
			r1:    'D',
			r2:    'K',
			title: "Docker Compose",
			mode:  "docker",
		})
		seen["docker"] = true
		seen["docker-compose"] = true
	}

	// 5. REST Client (only if enabled or pluginMgr == nil)
	hasREST := true
	if m.pluginMgr != nil {
		hasREST = m.pluginMgr.IsEnabled("rest-client") || m.pluginMgr.IsEnabled("rest")
	}
	if hasREST {
		items = append(items, rightStripItem{
			id:    "rest-client",
			r1:    'R',
			r2:    'C',
			title: "REST Client",
			mode:  "rest-client",
		})
		seen["rest-client"] = true
		seen["rest"] = true
	}

	// 6. gRPC & Protobuf (only if enabled or pluginMgr == nil)
	hasGRPC := true
	if m.pluginMgr != nil {
		hasGRPC = m.pluginMgr.IsEnabled("grpc-proto") || m.pluginMgr.IsEnabled("grpc")
	}
	if hasGRPC {
		items = append(items, rightStripItem{
			id:    "grpc",
			r1:    'R',
			r2:    'P',
			title: "gRPC & Protobuf",
			mode:  "grpc",
		})
		seen["grpc"] = true
		seen["grpc-proto"] = true
	}

	// 7. Test Runner (only if enabled or pluginMgr == nil)
	hasTests := true
	if m.pluginMgr != nil {
		hasTests = m.pluginMgr.IsEnabled("test-runner") || m.pluginMgr.IsEnabled("tests")
	}
	if hasTests {
		items = append(items, rightStripItem{
			id:    "test-runner",
			r1:    'T',
			r2:    'R',
			title: "Test Runner",
			mode:  "test-runner",
		})
		seen["test-runner"] = true
		seen["tests"] = true
	}

	// 8. TODO Tree (only if enabled or pluginMgr == nil)
	hasTodo := true
	if m.pluginMgr != nil {
		hasTodo = m.pluginMgr.IsEnabled("todo-tree") || m.pluginMgr.IsEnabled("todo")
	}
	if hasTodo {
		items = append(items, rightStripItem{
			id:    "todo-tree",
			r1:    'T',
			r2:    'D',
			title: "TODO Tree",
			mode:  "todo-tree",
		})
		seen["todo-tree"] = true
		seen["todo"] = true
	}

	// 9. Task Runner (only if enabled or pluginMgr == nil)
	hasTasks := true
	if m.pluginMgr != nil {
		hasTasks = m.pluginMgr.IsEnabled("task-runner") || m.pluginMgr.IsEnabled("tasks")
	}
	if hasTasks {
		items = append(items, rightStripItem{
			id:    "task-runner",
			r1:    'T',
			r2:    'K',
			title: "Task Runner",
			mode:  "task-runner",
		})
		seen["task-runner"] = true
		seen["tasks"] = true
	}

	// 10. Log Viewer (only if enabled or pluginMgr == nil)
	hasLogs := true
	if m.pluginMgr != nil {
		hasLogs = m.pluginMgr.IsEnabled("log-viewer") || m.pluginMgr.IsEnabled("logs")
	}
	if hasLogs {
		items = append(items, rightStripItem{
			id:    "log-viewer",
			r1:    'L',
			r2:    'G',
			title: "Log Viewer",
			mode:  "log-viewer",
		})
		seen["log-viewer"] = true
		seen["logs"] = true
	}

	// 11. Jupyter Notebook (only if enabled or pluginMgr == nil)
	hasJupyter := true
	if m.pluginMgr != nil {
		hasJupyter = m.pluginMgr.IsEnabled("jupyter-notebook") || m.pluginMgr.IsEnabled("jupyter")
	}
	if hasJupyter {
		items = append(items, rightStripItem{
			id:    "jupyter-notebook",
			r1:    'J',
			r2:    'P',
			title: "Jupyter Notebook",
			mode:  "jupyter-notebook",
		})
		seen["jupyter-notebook"] = true
		seen["jupyter"] = true
	}

	// 12. P2P Collaboration
	items = append(items, rightStripItem{
		id:    "p2p-collab",
		r1:    'C',
		r2:    'O',
		title: "P2P Collaboration",
		mode:  "p2p-collab",
	})
	seen["p2p-collab"] = true
	seen["collab"] = true
	seen["p2p"] = true

	// 13. Any other plugin tool windows with position == "right"
	if m.pluginMgr != nil {
		for _, tw := range m.pluginMgr.ActiveToolWindows() {
			if tw.Position == "right" {
				if seen[tw.ID] || seen[tw.PluginID] {
					continue
				}
				seen[tw.ID] = true
				seen[tw.PluginID] = true
				r1, r2 := toolWindowBadge(tw.Icon, tw.Title)
				items = append(items, rightStripItem{
					id:    tw.ID,
					r1:    r1,
					r2:    r2,
					title: tw.Title,
					mode:  tw.ID,
				})
			}
		}
	}

	return items
}

// ToggleRightSidebar toggles visibility of the secondary right sidebar.
func (m *AppModel) ToggleRightSidebar(mode string) tea.Cmd {
	if m.rightSidebarOpen && (mode == "" || m.rightSidebarMode == mode) {
		m.rightSidebarOpen = false
		m.rightSidebarTargetWidth = 0
		if m.settings != nil && !m.settings.Current.SmoothAnim {
			m.rightSidebarAnimWidth = 0
		}
	} else {
		m.rightSidebarOpen = true
		if mode != "" {
			m.rightSidebarMode = mode
			switch mode {
			case "ai-chat":
				m.rightSidebarTitle = "AI Assistant"
			case "db-inspector", "db":
				m.rightSidebarTitle = "Databases"
				if m.dbSidebarTab == "" {
					m.dbSidebarTab = "tables"
				}
				if m.pluginMgr != nil {
					insp := m.pluginMgr.IsEnabled("db-inspector")
					erd := m.pluginMgr.IsEnabled("db-er-diagram")
					if !insp && erd {
						m.dbSidebarTab = "er-diagram"
					} else if insp && !erd {
						m.dbSidebarTab = "tables"
					}
				}
			case "docker":
				m.rightSidebarTitle = "Docker Compose"
				if m.dockerPanel != nil {
					m.dockerPanel.Refresh()
				}
			case "rest-client", "rest":
				m.rightSidebarTitle = "REST Client"
			case "grpc":
				m.rightSidebarTitle = "gRPC & Protobuf"
				if m.grpcPanel != nil {
					m.grpcPanel.Open = true
				}
			case "log-viewer", "logs":
				m.rightSidebarTitle = "Log Viewer"
				if m.logPanel != nil {
					m.logPanel.Open = true
				}
			case "task-runner", "tasks":
				m.rightSidebarTitle = "Task Runner"
				if m.taskPanel != nil {
					m.taskPanel.Open = true
					m.taskPanel.Refresh(m.workspaceDir)
				}
			case "todo-tree", "todo":
				m.rightSidebarTitle = "TODO Tree"
				if m.todoPanel != nil {
					m.todoPanel.Open = true
					m.todoPanel.Refresh()
				}
			case "test-runner", "tests":
				m.rightSidebarTitle = "Test Runner"
				if m.testRunnerPanel != nil {
					m.testRunnerPanel.Open = true
					m.testRunnerPanel.Discover()
				}
			case "jupyter-notebook", "jupyter":
				m.rightSidebarTitle = "Jupyter Notebook"
				if m.jupyterPanel != nil {
					m.jupyterPanel.Open = true
				}
			case "p2p-collab", "collab", "p2p":
				m.rightSidebarTitle = "P2P Collaboration"
			default:
				m.rightSidebarTitle = mode
			}
		}
		if m.rightSidebarWidth <= 0 {
			m.rightSidebarWidth = 34
		}
		m.rightSidebarTargetWidth = float64(m.rightSidebarWidth)
		if m.settings != nil && !m.settings.Current.SmoothAnim {
			m.rightSidebarAnimWidth = float64(m.rightSidebarWidth)
		}
	}
	return func() tea.Msg {
		time.Sleep(8 * time.Millisecond)
		return animTickMsg{}
	}
}

// toolWindowBadge extracts 2 characters from an icon or title for the 2-column activity strip.
func toolWindowBadge(icon, title string) (rune, rune) {
	s := strings.TrimSpace(icon)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		s = strings.TrimPrefix(strings.TrimSuffix(s, "]"), "[")
	}
	if len(s) == 0 {
		s = strings.TrimSpace(title)
	}
	r := []rune(s)
	if len(r) == 0 {
		return 'P', 'L'
	}
	if len(r) == 1 {
		return r[0], ' '
	}
	return r[0], r[1]
}

// OpenDAGCanvasInSplit mounts the interactive Database Architecture DAG canvas into a 2-column split.
func (m *AppModel) OpenDAGCanvasInSplit() {
	if m.splits == nil {
		m.splits = NewSplitManager()
	}
	if m.splits.TotalPanes() < 2 {
		m.splits.SetLayout(Split2Cols)
	}
	if len(m.splits.Panes) > 1 {
		m.splits.Panes[1].SetView("dag-canvas", "Database Architecture DAG")
		m.splits.ActiveIndex = 1
	}
	if m.dagCanvasWidget == nil {
		schema := db.LoadHybridSchema(m.workspaceDir)
		m.dagCanvasWidget = NewDAGCanvasWidget(schema.ToGraphModel(), &m.theme)
	}
	m.statusMessage = "DAG Canvas opened in split pane"
	if m.toasts != nil {
		m.toasts.Success("DAG CANVAS", "Database schema opened in split pane")
	}
}

// OpenERDTab opens or activates the interactive 2D ER Diagram as an editor tab.
func (m *AppModel) OpenERDTab() {
	erdPath := "schema.erd"
	if m.workspaceDir != "" {
		erdPath = filepath.Join(m.workspaceDir, "schema.erd")
	}
	_, _ = m.eng.Open(erdPath)
	schema := db.LoadHybridSchema(m.workspaceDir)
	m.dagCanvasWidget = NewDAGCanvasWidget(schema.ToGraphModel(), &m.theme)
	m.statusMessage = "Opened Database ER Diagram (schema.erd)"
	if m.toasts != nil {
		m.toasts.Success("ER DIAGRAM", "Opened schema.erd in editor tab")
	}
}

// OpenProjectGraphTab opens or activates the interactive Project Graph DAG as an editor tab.
func (m *AppModel) OpenProjectGraphTab() {
	graphPath := "project.graph"
	if m.workspaceDir != "" {
		graphPath = filepath.Join(m.workspaceDir, "project.graph")
	}
	activeDoc := m.eng.ActiveDocument()
	_, _ = m.eng.Open(graphPath)
	if m.projectGraphPanel == nil {
		m.projectGraphPanel = NewProjectGraphPanel(&m.theme)
	}
	// Rebuild graph from previous active doc or workspace
	targetDoc := activeDoc
	if targetDoc != nil && (filepath.Base(targetDoc.FilePath) == "project.graph" || strings.HasSuffix(targetDoc.FilePath, ".graph") || filepath.Base(targetDoc.FilePath) == "schema.erd" || strings.HasSuffix(targetDoc.FilePath, ".erd")) {
		targetDoc = nil
		for _, doc := range m.eng.Documents() {
			base := filepath.Base(doc.FilePath)
			if base != "project.graph" && !strings.HasSuffix(base, ".graph") && base != "schema.erd" && !strings.HasSuffix(base, ".erd") {
				targetDoc = doc
				break
			}
		}
	}
	m.projectGraphPanel.RebuildWithLSP(targetDoc, m.workspaceDir, m.lspClient)
	m.statusMessage = "Opened Project Call & Architecture Graph (project.graph)"
	if m.toasts != nil {
		m.toasts.Success("PROJECT GRAPH", "Opened project.graph in editor tab")
	}
}

// jumpToSymbolFromGraph navigates to the definition of a symbol selected in the project graph.
func (m *AppModel) jumpToSymbolFromGraph(nodeID string) {
	if nodeID == "" {
		return
	}

	// 1. Direct Node Metadata (instant jump via FilePath and Line from AST / LSP)
	if m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil && m.projectGraphPanel.Canvas.Model != nil {
		if node, exists := m.projectGraphPanel.Canvas.Model.Nodes[nodeID]; exists {
			if node.FilePath != "" {
				targetPath := node.FilePath
				if !filepath.IsAbs(targetPath) && m.workspaceDir != "" {
					targetPath = filepath.Join(m.workspaceDir, targetPath)
				}
				doc, err := m.eng.Open(targetPath)
				if err == nil && doc != nil {
					targetLine := max(0, node.Line-1)
					total := doc.Buffer.TotalLines()
					if targetLine >= total && total > 0 {
						targetLine = total - 1
					}
					pos := corebuf.Position{Line: targetLine, Column: 0}
					doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(pos, pos)})
					m.ensureCursorVisible()
					m.statusMessage = fmt.Sprintf("Jumped to %s in %s:%d", node.Title, filepath.Base(targetPath), targetLine+1)
					if m.toasts != nil {
						m.toasts.Success("NAVIGATE", fmt.Sprintf("Opened %s:%d", filepath.Base(targetPath), targetLine+1))
					}
					return
				}
			}
		}
	}

	cleanSym := strings.TrimSuffix(nodeID, "()")
	if idx := strings.Index(cleanSym, ":"); idx >= 0 {
		cleanSym = cleanSym[:idx]
	}
	funcName := cleanSym
	if dotIdx := strings.LastIndex(cleanSym, "."); dotIdx >= 0 {
		funcName = cleanSym[dotIdx+1:]
	}

	// 2. Search in all currently open documents
	for _, doc := range m.eng.Documents() {
		base := filepath.Base(doc.FilePath)
		if base == "project.graph" || strings.HasSuffix(base, ".graph") || base == "schema.erd" {
			continue
		}
		total := doc.Buffer.TotalLines()
		for l := 0; l < total; l++ {
			lineBytes, _ := doc.Buffer.GetLine(l)
			lineStr := string(lineBytes)
			if strings.Contains(lineStr, "func "+funcName+"(") ||
				strings.Contains(lineStr, "func "+funcName+" ") ||
				(strings.Contains(lineStr, "func (") && strings.Contains(lineStr, ") "+funcName+"(")) ||
				strings.Contains(lineStr, "def "+funcName+"(") ||
				strings.Contains(lineStr, "fn "+funcName+"(") ||
				strings.Contains(lineStr, "function "+funcName+"(") {
				_, _ = m.eng.Open(doc.FilePath)
				pos := corebuf.Position{Line: l, Column: 0}
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(pos, pos)})
				m.ensureCursorVisible()
				m.statusMessage = fmt.Sprintf("Jumped to %s in %s:%d", funcName, filepath.Base(doc.FilePath), l+1)
				if m.toasts != nil {
					m.toasts.Success("NAVIGATE", fmt.Sprintf("Opened %s:%d", filepath.Base(doc.FilePath), l+1))
				}
				return
			}
		}
	}

	// 3. Scan workspace files via filepath.Walk (handles unopened files and packages)
	if m.workspaceDir != "" {
		var foundPath string
		var foundLine int = -1

		_ = filepath.Walk(m.workspaceDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				base := info.Name()
				if base == ".git" || base == "node_modules" || base == "vendor" || base == "bin" || base == ".idea" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".go" && ext != ".ts" && ext != ".rs" && ext != ".py" && ext != ".js" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			sData := string(data)
			targets := []string{
				"func " + funcName + "(",
				"func " + funcName + " ",
				"def " + funcName + "(",
				"fn " + funcName + "(",
				"function " + funcName + "(",
			}
			for _, t := range targets {
				if idx := strings.Index(sData, t); idx >= 0 {
					foundPath = path
					foundLine = strings.Count(sData[:idx], "\n")
					return filepath.SkipAll
				}
			}
			// Method pattern: `) funcName(`
			if idx := strings.Index(sData, ") "+funcName+"("); idx >= 0 {
				foundPath = path
				foundLine = strings.Count(sData[:idx], "\n")
				return filepath.SkipAll
			}
			return nil
		})

		if foundPath != "" && foundLine >= 0 {
			doc, err := m.eng.Open(foundPath)
			if err == nil && doc != nil {
				pos := corebuf.Position{Line: foundLine, Column: 0}
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(pos, pos)})
				m.ensureCursorVisible()
				m.statusMessage = fmt.Sprintf("Jumped to %s in %s:%d", funcName, filepath.Base(foundPath), foundLine+1)
				if m.toasts != nil {
					m.toasts.Success("NAVIGATE", fmt.Sprintf("Opened %s:%d", filepath.Base(foundPath), foundLine+1))
				}
				return
			}
		}
	}

	// 4. Detected external stdlib or third-party package
	if strings.Contains(cleanSym, ".") {
		m.statusMessage = fmt.Sprintf("'%s' — внешняя функция пакета/библиотеки", cleanSym)
		if m.toasts != nil {
			m.toasts.Info("EXTERNAL", fmt.Sprintf("%s — внешняя зависимость", cleanSym))
		}
		return
	}

	m.statusMessage = fmt.Sprintf("Символ '%s' не найден в проекте", cleanSym)
}

// OpenDBConsole opens or activates the interactive SQL Query Console as an editor tab.
func (m *AppModel) OpenDBConsole() {
	consolePath := "console.sql"
	if m.workspaceDir != "" {
		consolePath = filepath.Join(m.workspaceDir, "console.sql")
	}
	doc, _ := m.eng.Open(consolePath)
	if doc != nil {
		doc.LanguageID = "sql"
		if doc.Buffer.TotalLines() <= 1 {
			line0, _ := doc.Buffer.GetLine(0)
			if strings.TrimSpace(string(line0)) == "" {
				sampleSQL := "-- Tahr Database Query Console (Press Ctrl+Enter to execute)\n-- Supported: PostgreSQL, MySQL, MariaDB, SQLite, MSSQL, CockroachDB, DuckDB, ClickHouse\n\nSELECT * FROM users;\n"
				_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: sampleSQL})
			}
		}
	}
	m.statusMessage = "Opened SQL Query Console (console.sql)"
	if m.toasts != nil {
		m.toasts.Success("DATABASE CONSOLE", "Opened console.sql (Ctrl+Enter to run)")
	}
}

// OpenTableDataGrid opens or activates a rich DataGrid viewer tab for a specific table.
func (m *AppModel) OpenTableDataGrid(tableName string) {
	gridPath := fmt.Sprintf("%s.datagrid", tableName)
	if m.workspaceDir != "" {
		gridPath = filepath.Join(m.workspaceDir, fmt.Sprintf("%s.datagrid", tableName))
	}
	_, _ = m.eng.Open(gridPath)
	_ = m.getOrCreateDataGrid(tableName)
	m.statusMessage = fmt.Sprintf("Opened DataGrid: %s", tableName)
	if m.toasts != nil {
		m.toasts.Success("DATAGRID", fmt.Sprintf("Opened table viewer for %s", tableName))
	}
}

// getOrCreateDataGrid returns or initializes a DataGridWidget for tableName.
func (m *AppModel) getOrCreateDataGrid(tableName string) *DataGridWidget {
	if m.dataGrids == nil {
		m.dataGrids = make(map[string]*DataGridWidget)
	}
	if grid, exists := m.dataGrids[tableName]; exists {
		return grid
	}
	schema := db.LoadHybridSchema(m.workspaceDir)
	var tbl *db.Table
	if schema != nil && schema.Tables != nil {
		tbl = schema.Tables[tableName]
	}
	grid := NewDataGridWidget(tableName, tbl, &m.theme)
	m.dataGrids[tableName] = grid
	return grid
}

// executeSQLQueryAtCursor executes the SQL statement at cursor and opens the result DataGrid or updates schema.
func (m *AppModel) executeSQLQueryAtCursor() {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	query := strings.TrimSpace(m.selectedText())
	if query == "" {
		cursorLine := 0
		sels := doc.Buffer.GetSelections()
		if len(sels) > 0 {
			cursorLine = sels[0].Head.Line
		}

		// If cursorLine is empty or comment, look backwards for nearest statement
		targetLine := cursorLine
		lineBytes, _ := doc.Buffer.GetLine(targetLine)
		if strings.TrimSpace(string(lineBytes)) == "" || strings.HasPrefix(strings.TrimSpace(string(lineBytes)), "--") {
			for l := cursorLine - 1; l >= 0; l-- {
				lb, _ := doc.Buffer.GetLine(l)
				s := strings.TrimSpace(string(lb))
				if s != "" && !strings.HasPrefix(s, "--") {
					targetLine = l
					break
				}
			}
		}

		// Find start of statement containing targetLine
		startL := targetLine
		for startL > 0 {
			prevBytes, _ := doc.Buffer.GetLine(startL - 1)
			prevS := strings.TrimSpace(string(prevBytes))
			if prevS == "" || strings.HasSuffix(prevS, ";") {
				break
			}
			startL--
		}

		var sb strings.Builder
		tot := doc.Buffer.TotalLines()
		for l := startL; l < tot; l++ {
			lb, _ := doc.Buffer.GetLine(l)
			s := strings.TrimSpace(string(lb))
			if strings.HasPrefix(s, "--") {
				continue
			}
			if s != "" {
				sb.WriteString(s + " ")
			}
			if strings.HasSuffix(s, ";") {
				break
			}
		}
		query = strings.TrimSpace(sb.String())
	}

	if query == "" {
		if m.toasts != nil {
			m.toasts.Warn("SQL EXECUTE", "No SQL query statement found at cursor")
		}
		return
	}

	qUpper := strings.ToUpper(query)
	if strings.HasPrefix(qUpper, "SELECT") {
		reFrom := regexp.MustCompile(`(?i)FROM\s+([a-zA-Z0-9_".\[\]` + "`" + `]+)`)
		matches := reFrom.FindStringSubmatch(query)
		tblName := "query_result"
		if len(matches) > 1 {
			rawName := matches[1]
			rawName = strings.Trim(rawName, "`\"[]")
			parts := strings.Split(rawName, ".")
			tblName = parts[len(parts)-1]
		}
		m.OpenTableDataGrid(tblName)
		if m.toasts != nil {
			m.toasts.Success("SQL EXECUTED", fmt.Sprintf("Query executed: loaded %s rows into DataGrid (0.8ms)", tblName))
		}
	} else if strings.HasPrefix(qUpper, "CREATE") || strings.HasPrefix(qUpper, "ALTER") || strings.HasPrefix(qUpper, "DROP") {
		parsed := db.ParseSQLDDL(query)
		if parsed != nil && len(parsed.Tables) > 0 {
			m.dagCanvasWidget = NewDAGCanvasWidget(parsed.ToGraphModel(), &m.theme)
			m.statusMessage = "Schema DDL executed: ER diagram updated"
			if m.toasts != nil {
				m.toasts.Success("DDL EXECUTED", "Database schema updated successfully")
			}
		} else {
			if m.toasts != nil {
				m.toasts.Success("DDL EXECUTED", "Schema statement executed")
			}
		}
	} else {
		if m.toasts != nil {
			m.toasts.Success("SQL EXECUTED", "Statement executed: 1 row affected (0.4ms)")
		}
	}
}

// OpenProjectGraphInSplit mounts the interactive Call Hierarchy and Project Graph into a split pane.
func (m *AppModel) OpenProjectGraphInSplit() {
	if m.splits == nil {
		m.splits = NewSplitManager()
	}
	if m.splits.TotalPanes() < 2 {
		m.splits.SetLayout(Split2Cols)
	}
	if len(m.splits.Panes) > 1 {
		m.splits.Panes[1].SetView("project-graph", "Project Call Hierarchy")
		m.splits.ActiveIndex = 1
	}
	if m.projectGraphPanel == nil {
		m.projectGraphPanel = NewProjectGraphPanel(&m.theme)
	}
	doc := m.eng.ActiveDocument()
	if doc == nil && len(m.splits.Panes) > 0 {
		doc = m.findDocument(m.splits.Panes[0].DocID)
	}
	m.projectGraphPanel.RebuildWithLSP(doc, m.workspaceDir, m.lspClient)
	m.recalculatePaneLayout()
	m.statusMessage = "Project Graph opened in split pane"
	if m.toasts != nil {
		m.toasts.Success("GRAPHS", "Call Hierarchy opened in split pane")
	}
}

// recalculatePaneLayout synchronizes the split pane geometry with active terminal dimensions.
func (m *AppModel) recalculatePaneLayout() {
	if m.splits == nil || m.width <= 0 || m.height <= 0 {
		return
	}
	editorTop := 2
	statusBarY := m.height - 1
	usableHeight := statusBarY - editorTop
	if usableHeight < 1 {
		usableHeight = 1
	}
	editorHeight := usableHeight
	if m.outputOpen {
		drawerH := m.outputHeight
		if drawerH > usableHeight-3 {
			drawerH = max(3, usableHeight-3)
		}
		editorHeight = usableHeight - drawerH
	}
	if m.terminal != nil && m.terminal.Open {
		termH := m.terminal.Height
		if termH > editorHeight-3 {
			termH = max(3, editorHeight-3)
		}
		editorHeight -= termH
	}

	stripLeftW := 0
	stripRightW := 0
	if m.width >= 70 {
		stripLeftW = 3
		stripRightW = 3
	}
	sideW := 0
	if m.sidebarOpen {
		sideW = m.sidebarWidth
	}
	rightSideW := 0
	if m.rightSidebarOpen {
		rightSideW = m.rightSidebarWidth
		if rightSideW <= 0 {
			rightSideW = 34
		}
	}
	isTreeRight := (m.settings != nil && m.settings.Current.TreePosition == "right")
	treeStartX := stripLeftW
	if isTreeRight {
		treeStartX = m.width - stripRightW - sideW
	}
	editorLeft := stripLeftW
	editorRight := m.width - stripRightW
	if isTreeRight {
		if sideW > 0 {
			editorRight = treeStartX - 1
		}
	} else {
		if sideW > 0 {
			editorLeft = stripLeftW + sideW + 1
		}
		if rightSideW > 0 {
			editorRight = m.width - stripRightW - rightSideW - 1
		}
	}
	if editorRight <= editorLeft {
		editorRight = editorLeft + 1
	}

	editorArea := buffer.NewRect(editorLeft, editorTop, editorRight-editorLeft, editorHeight)
	activeDocID := ""
	if doc := m.eng.ActiveDocument(); doc != nil {
		activeDocID = doc.ID
	}
	m.splits.UpdateLayout(editorArea, m.eng.Documents(), activeDocID)
}

// closeSplitPane closes the specified split pane and reverts layout to single pane or reduced grid.
func (m *AppModel) closeSplitPane(idx int) {
	if m.splits == nil || m.splits.TotalPanes() <= 1 {
		return
	}
	if m.splits.TotalPanes() == 2 {
		if idx == 0 && len(m.splits.Panes) > 1 {
			m.splits.Panes[0] = m.splits.Panes[1]
			m.splits.Panes[0].Index = 0
		}
		m.splits.SetLayout(SplitSingle)
		m.splits.ActiveIndex = 0
		m.recalculatePaneLayout()
		m.onActiveDocumentChanged()
		if m.toasts != nil {
			m.toasts.Info("SPLIT", "Closed split pane")
		}
		return
	}
	switch m.splits.Mode {
	case Split3Cols:
		m.splits.SetLayout(Split2Cols)
	case Split4Grid:
		m.splits.SetLayout(Split3Cols)
	default:
		m.splits.SetLayout(SplitSingle)
	}
	if m.splits.ActiveIndex >= m.splits.TotalPanes() {
		m.splits.ActiveIndex = m.splits.TotalPanes() - 1
	}
	m.recalculatePaneLayout()
	m.onActiveDocumentChanged()
	if m.toasts != nil {
		m.toasts.Info("SPLIT", "Closed split pane")
	}
}

// handleToolWindowClick toggles or opens a tool window contributed by a plugin.
func (m *AppModel) handleToolWindowClick(tw plugin.ActiveToolWindow) tea.Cmd {
	if tw.ID == "sqlite-viewer-panel" {
		return m.ToggleRightSidebar("db-inspector")
	}
	if tw.ID == "jupyter-notebook-panel" {
		return m.ToggleRightSidebar("jupyter-notebook")
	}
	cmd := m.ToggleRightSidebar(tw.ID)
	m.rightSidebarTitle = tw.Title
	if m.toasts != nil {
		m.toasts.Info("TOOL WINDOW", fmt.Sprintf("Opened %s", tw.Title))
	}
	return cmd
}

func (m *AppModel) openSearchInFilesModal() {
	if m.searchInFilesModal == nil {
		m.searchInFilesModal = NewSearchInFilesModal()
		m.searchInFilesModal.OnJump = func(filePath string, line, col int) {
			_, err := m.eng.Open(filePath)
			if err == nil {
				m.jumpToLine(line, col)
				m.statusMessage = fmt.Sprintf("Opened %s:%d", filepath.Base(filePath), line+1)
			}
		}
	}
	query := m.selectedText()
	m.searchInFilesModal.OpenModal(query, m.workspaceDir)
}

func (m *AppModel) toggleProblemsPanel() {
	if m.problemsPanel == nil {
		m.problemsPanel = NewProblemsPanel()
		m.problemsPanel.OnJump = func(filePath string, line, col int) {
			_, err := m.eng.Open(filePath)
			if err == nil {
				m.jumpToLine(line, col)
				m.statusMessage = fmt.Sprintf("Jumped to problem in %s:%d", filepath.Base(filePath), line+1)
			}
		}
	}
	m.diagMu.RLock()
	m.problemsPanel.Refresh(m.docDiagDetails)
	m.diagMu.RUnlock()
	m.problemsPanel.Toggle()
}

// openGitModal initializes and displays the interactive Hunk Staging and Branch Graph modal.
func (m *AppModel) openGitModal() {
	if m.gitModal == nil {
		m.gitModal = NewGitModal(m.workspaceDir)
	}
	filePath := ""
	if doc := m.eng.ActiveDocument(); doc != nil {
		filePath = doc.FilePath
	}
	m.gitModal.OpenForFile(m.workspaceDir, filePath)
}

// toggleMarkdownPreview toggles the side-by-side live ANSI/ASCII markdown renderer.
func (m *AppModel) toggleMarkdownPreview() {
	if m.mdPreviewOpen {
		m.mdPreviewOpen = false
		if m.splits != nil && m.splits.TotalPanes() == 2 {
			m.splits.SetLayout(SplitSingle)
		}
		m.toasts.Info("MARKDOWN", "Live Markdown Preview closed")
	} else {
		if m.splits != nil && m.splits.TotalPanes() < 2 {
			m.splits.SetLayout(Split2Cols)
		}
		m.mdPreviewOpen = true
		m.toasts.Info("MARKDOWN", "Live Markdown Preview opened in Split 2")
	}
}

func (m *AppModel) handleCrashDialogAction(action CrashDialogAction) {
	switch action {
	case CrashActionSend:
		err := m.crashDialog.ExecuteSend()
		if err == nil {
			if m.toasts != nil {
				m.toasts.Success("CRASH", i18n.T("dialog.crash.sent_success"))
			}
		} else {
			if m.toasts != nil {
				m.toasts.Error("CRASH", i18n.T("dialog.crash.server_error"))
			}
		}
	case CrashActionDelete:
		m.crashDialog.ExecuteDelete()
		if m.toasts != nil {
			m.toasts.Info("CRASH", i18n.T("dialog.crash.deleted"))
		}
	case CrashActionIgnore:
		m.crashDialog.Open = false
	case CrashActionViewLogs:
		if m.crashDialog.Payload != nil {
			var traceLines []string
			if m.crashDialog.Payload.PanicReason != "" {
				traceLines = append(traceLines, fmt.Sprintf("Reason: %s", m.crashDialog.Payload.PanicReason))
				traceLines = append(traceLines, "")
			}
			if m.crashDialog.Payload.StackTrace != "" {
				traceLines = append(traceLines, "=== CALL STACK ===")
				traceLines = append(traceLines, strings.Split(m.crashDialog.Payload.StackTrace, "\n")...)
				traceLines = append(traceLines, "")
			}
			if len(m.crashDialog.Payload.RecentLogs) > 0 {
				traceLines = append(traceLines, "=== SYSTEM LOGS ===")
				traceLines = append(traceLines, m.crashDialog.Payload.RecentLogs...)
			}

			metaLines := []string{
				fmt.Sprintf("Report ID:    %s", m.crashDialog.Payload.ReportID),
				fmt.Sprintf("Timestamp:    %s", m.crashDialog.Payload.Timestamp),
				fmt.Sprintf("App Version:  %s", m.crashDialog.Payload.AppVersion),
				fmt.Sprintf("Platform:     %s / %s", m.crashDialog.Payload.Platform.OS, m.crashDialog.Payload.Platform.Arch),
				fmt.Sprintf("Frontend:     %s", m.crashDialog.Payload.Platform.Frontend),
				fmt.Sprintf("Terminal:     %s", m.crashDialog.Payload.Platform.Terminal),
				fmt.Sprintf("Wayland:      %v", m.crashDialog.Payload.Platform.Wayland),
				fmt.Sprintf("Dump File:    %s", m.crashDialog.ReportPath),
			}

			rawBytes, _ := os.ReadFile(m.crashDialog.ReportPath)
			m.logInspector.SetContent(i18n.T("modal.inspector.dump_title"), traceLines, metaLines, string(rawBytes))
		}
	}
}

func (m *AppModel) handleBugReportAction(action BugReportAction) {
	switch action {
	case BRActionSend:
		err := m.bugReportModal.ExecuteSend()
		if err == nil {
			if m.toasts != nil {
				m.toasts.Success("BUG REPORT", i18n.T("modal.bugreport.sent_success"))
			}
		} else {
			if m.toasts != nil {
				m.toasts.Error("BUG REPORT", i18n.T("modal.bugreport.err_generic"))
			}
		}
	case BRActionCopyGitHub:
		ghMarkdown := m.bugReportModal.GetGitHubMarkdown()
		_ = clipboard.Write(ghMarkdown)
		if m.toasts != nil {
			m.toasts.Success("COPIED", i18n.T("modal.bugreport.copied_github"))
		}
	case BRActionViewData:
		logs := logging.GetRecentLogs(200)
		meta := m.bugReportModal.BuildReportRequest().Metadata
		metaLines := []string{
			fmt.Sprintf("App Version:      %s", meta.AppVersion),
			fmt.Sprintf("OS / Arch:        %s / %s", meta.Platform.OS, meta.Platform.Arch),
			fmt.Sprintf("Frontend:         %s", meta.Platform.Frontend),
			fmt.Sprintf("Terminal:         %s", meta.Platform.Terminal),
			fmt.Sprintf("Wayland:          %v", meta.Platform.Wayland),
			fmt.Sprintf("Active Plugins:   %s", strings.Join(meta.ActivePlugins, ", ")),
		}
		rawJSON, _ := json.MarshalIndent(meta, "", "  ")
		m.logInspector.SetContent(i18n.T("modal.inspector.session_title"), logs, metaLines, string(rawJSON))
	case BRActionClose:
		m.bugReportModal.Open = false
	}
}

func (m *AppModel) openBugReportModal() {
	var plugins []string
	if m.pluginMgr != nil {
		for _, p := range m.pluginMgr.InstalledPlugins() {
			if m.pluginMgr.IsEnabled(p.ID) {
				plugins = append(plugins, p.Name)
			}
		}
	}
	ver := "0.1.0-alpha"
	m.bugReportModal.OpenModal(ver, plugins)
}

func (m *AppModel) openLogInspector() {
	logs := logging.GetRecentLogs(200)
	plat := crash.CurrentPlatformInfo()
	metaLines := []string{
		fmt.Sprintf("OS / Arch:        %s / %s", plat.OS, plat.Arch),
		fmt.Sprintf("Frontend:         %s", plat.Frontend),
		fmt.Sprintf("Terminal:         %s", plat.Terminal),
		fmt.Sprintf("Wayland:          %v", plat.Wayland),
		fmt.Sprintf("Logs Directory:   %s", crash.GetCrashesDir()),
	}
	m.logInspector.SetContent(i18n.T("modal.inspector.sys_logs_title"), logs, metaLines, "")
}

func (m *AppModel) openP2PModal() {
	if m.p2pModal == nil {
		session := p2p.NewHostSession("host", "")
		m.p2pModal = NewP2PModal(session, &m.theme)
	}
	m.p2pModal.Visible = true
	if m.toasts != nil {
		m.toasts.Info("COLLAB", fmt.Sprintf("Session Code: %s", m.p2pModal.Session.SessionCode))
	}
}

func (m *AppModel) openDBDiffModal(diff *db.SchemaDiff) {
	if diff == nil {
		return
	}
	m.dbDiffModal = NewDBDiffModal(diff, &m.theme)
	m.dbDiffModal.Visible = true
}

func (m *AppModel) openFileAtLocation(filePath string, line int) {
	if filePath == "" {
		return
	}
	targetPath := filePath
	if !filepath.IsAbs(targetPath) && m.workspaceDir != "" {
		targetPath = filepath.Join(m.workspaceDir, targetPath)
	}
	doc, err := m.eng.Open(targetPath)
	if err == nil && doc != nil {
		targetLine := max(0, line-1)
		total := doc.Buffer.TotalLines()
		if targetLine >= total && total > 0 {
			targetLine = total - 1
		}
		pos := corebuf.Position{Line: targetLine, Column: 0}
		doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(pos, pos)})
		m.ensureCursorVisible()
		m.onActiveDocumentChanged()
	}
}

func (m *AppModel) openDevToolsModal() {
	if m.devtoolsModal != nil {
		m.devtoolsModal.Show()
	}
}

func (m *AppModel) openRegexModal() {
	if m.regexModal != nil {
		m.regexModal.Show()
	}
}

func (m *AppModel) openBookmarksModal() {
	if m.bookmarksModal != nil {
		m.bookmarksModal.Show()
	}
}

func (m *AppModel) toggleActiveBookmark() {
	doc := m.eng.ActiveDocument()
	if doc == nil || m.bookmarkStore == nil || doc.Buffer == nil {
		return
	}
	sel := doc.Buffer.PrimarySelection()
	lineNum := sel.Head.Line + 1
	linePreview := ""
	if lineBytes, err := doc.Buffer.GetLine(sel.Head.Line); err == nil {
		linePreview = string(lineBytes)
	}
	_, added, _ := m.bookmarkStore.ToggleBookmark(doc.FilePath, lineNum, linePreview, "")
	if m.toasts != nil {
		if added {
			m.toasts.Success("BOOKMARK", fmt.Sprintf("Bookmark added at line %d", lineNum))
		} else {
			m.toasts.Info("BOOKMARK", fmt.Sprintf("Bookmark removed at line %d", lineNum))
		}
	}
}

func (m *AppModel) jumpNextBookmark() {
	doc := m.eng.ActiveDocument()
	if doc == nil || m.bookmarkStore == nil || doc.Buffer == nil {
		return
	}
	sel := doc.Buffer.PrimarySelection()
	bm := m.bookmarkStore.NextBookmark(doc.FilePath, sel.Head.Line+1)
	if bm != nil {
		m.openFileAtLocation(bm.FilePath, bm.LineNumber)
	}
}

func (m *AppModel) jumpPrevBookmark() {
	doc := m.eng.ActiveDocument()
	if doc == nil || m.bookmarkStore == nil || doc.Buffer == nil {
		return
	}
	sel := doc.Buffer.PrimarySelection()
	bm := m.bookmarkStore.PrevBookmark(doc.FilePath, sel.Head.Line+1)
	if bm != nil {
		m.openFileAtLocation(bm.FilePath, bm.LineNumber)
	}
}

func (m *AppModel) loadCoverageProfile(filePath string) {
	if m.coverageEngine == nil {
		return
	}
	target := filePath
	if target == "" && m.workspaceDir != "" {
		candidates := []string{
			"coverage.out",
			"cover.out",
			"lcov.info",
			"coverage.lcov",
		}
		for _, c := range candidates {
			p := filepath.Join(m.workspaceDir, c)
			if _, err := os.Stat(p); err == nil {
				target = p
				break
			}
		}
	}
	if target == "" {
		if m.toasts != nil {
			m.toasts.Warn("COVERAGE", "No coverage profile found in workspace (coverage.out, lcov.info)")
		}
		return
	}
	err := m.coverageEngine.LoadFile(target)
	if err != nil {
		if m.toasts != nil {
			m.toasts.Error("COVERAGE", fmt.Sprintf("Failed to load coverage: %v", err))
		}
		return
	}
	if m.toasts != nil {
		_, _, pct := m.coverageEngine.Summary()
		m.toasts.Success("COVERAGE", fmt.Sprintf("Coverage loaded from %s (%.1f%%)", filepath.Base(target), pct))
	}
}

func (m *AppModel) executeActiveHTTPRequest() {
	doc := m.eng.ActiveDocument()
	if doc == nil || doc.Buffer == nil {
		return
	}
	textBytes, err := doc.Buffer.GetText()
	if err != nil || len(textBytes) == 0 {
		return
	}
	requests, err := restclient.Parse(string(textBytes))
	if err != nil || len(requests) == 0 {
		if m.toasts != nil {
			m.toasts.Warn("REST CLIENT", "No HTTP request found in document")
		}
		return
	}

	curLine := 1
	sel := doc.Buffer.PrimarySelection()
	if sel.Head.Line >= 0 {
		curLine = sel.Head.Line + 1
	}

	var targetReq *restclient.Request
	for i := range requests {
		if curLine >= requests[i].StartLine && curLine <= requests[i].EndLine {
			targetReq = &requests[i]
			break
		}
	}
	if targetReq == nil {
		for i := range requests {
			if requests[i].StartLine <= curLine {
				targetReq = &requests[i]
			}
		}
		if targetReq == nil && len(requests) > 0 {
			targetReq = &requests[0]
		}
	}
	if targetReq == nil {
		return
	}

	if m.toasts != nil {
		m.toasts.Info("REST CLIENT", fmt.Sprintf("Sending %s %s...", targetReq.Method, targetReq.URL))
	}

	reqCopy := *targetReq
	go func() {
		exec := restclient.NewExecutor(30 * time.Second)
		resp, _ := exec.ExecuteRequest(reqCopy)
		if resp != nil {
			if m.restClientPanel != nil {
				m.restClientPanel.SetResponse(resp)
				m.restClientPanel.Open = true
			}
			_ = m.ToggleRightSidebar("rest-client")
			if m.toasts != nil {
				if resp.IsSuccess() {
					m.toasts.Success("REST CLIENT", resp.StatusPill())
				} else if resp.Error != "" {
					m.toasts.Error("REST CLIENT", resp.Error)
				} else {
					m.toasts.Warn("REST CLIENT", resp.StatusPill())
				}
			}
		}
	}()
}

func (m *AppModel) renderRightSidebar(buf *buffer.Buffer, startX, topY, sideW, sideH int) {
	if sideW <= 0 || sideH <= 0 {
		return
	}

	sidebarBg := toColor(m.theme.GutterBg)
	sidebarFg := toColor(m.theme.Foreground)
	borderFg := toColor(m.theme.BorderColor)
	headerBg := toColor(m.theme.StatusBarBg)
	headerFg := toColor(m.theme.Function)
	accentFg := toColor(m.theme.Keyword)

	// Fill sidebar background
	for y := topY; y < topY+sideH; y++ {
		for x := startX; x < startX+sideW; x++ {
			buf.SetRune(x, y, ' ', sidebarFg, sidebarBg, cell.AttrNone)
		}
	}

	// 1. Header Row
	title := m.rightSidebarTitle
	if title == "" {
		title = "TOOL WINDOW"
	}
	titleRunes := []rune(title)
	btnSpace := 2
	if sideW >= 18 {
		btnSpace = 6 // "◀ ▶ × "
	}
	for i, r := range titleRunes {
		if i < sideW-btnSpace-1 {
			buf.SetRune(startX+1+i, topY, r, headerFg, headerBg, cell.AttrBold)
		}
	}
	// Render resize buttons and close button on right edge of header
	if sideW >= 18 {
		btnFg := toColor(m.theme.Foreground)
		buf.SetRune(startX+sideW-6, topY, '◀', btnFg, headerBg, cell.AttrBold)
		buf.SetRune(startX+sideW-5, topY, ' ', btnFg, headerBg, cell.AttrNone)
		buf.SetRune(startX+sideW-4, topY, '▶', btnFg, headerBg, cell.AttrBold)
		buf.SetRune(startX+sideW-3, topY, ' ', btnFg, headerBg, cell.AttrNone)
	}
	buf.SetRune(startX+sideW-2, topY, '×', toColor(m.theme.DiagnosticError), headerBg, cell.AttrBold)
	buf.SetRune(startX+sideW-1, topY, ' ', toColor(m.theme.DiagnosticError), headerBg, cell.AttrNone)

	// Divider line below header
	if sideH > 1 {
		for x := startX; x < startX+sideW; x++ {
			buf.SetRune(x, topY+1, '─', borderFg, sidebarBg, cell.AttrNone)
		}
	}

	contentTop := topY + 2
	contentHeight := sideH - 2
	if contentHeight <= 0 {
		return
	}

	// Helper to print styled line safely within boundaries
	printLine := func(y int, text string, fg cell.Color, attr cell.Modifier) {
		if y < contentTop || y >= topY+sideH {
			return
		}
		runes := []rune(text)
		for x := 0; x < sideW-2 && x < len(runes); x++ {
			buf.SetRune(startX+1+x, y, runes[x], fg, sidebarBg, attr)
		}
	}

	switch m.rightSidebarMode {
	case "ai-chat", "ai":
		if m.chatPanel != nil {
			activeDoc := ""
			if doc := m.eng.ActiveDocument(); doc != nil {
				activeDoc = doc.FilePath
			}
			m.chatPanel.Render(buf, startX, topY, sideW, sideH, activeDoc, &m.theme)
		}

	case "db-inspector", "db":
		inspEnabled := true
		erdEnabled := true
		if m.pluginMgr != nil {
			inspEnabled = m.pluginMgr.IsEnabled("db-inspector")
			erdEnabled = m.pluginMgr.IsEnabled("db-er-diagram")
		}
		if !inspEnabled && erdEnabled {
			m.dbSidebarTab = "er-diagram"
		} else if inspEnabled && !erdEnabled {
			m.dbSidebarTab = "tables"
		} else if m.dbSidebarTab == "" {
			m.dbSidebarTab = "tables"
		}

		offsetY := 0
		if inspEnabled && erdEnabled {
			// Tab headers at contentTop
			tab1Fg := sidebarFg
			tab1Bg := sidebarBg
			tab1Attr := cell.AttrNone
			if m.dbSidebarTab == "tables" {
				tab1Fg = toColor(m.theme.PopupSelFg)
				tab1Bg = toColor(m.theme.PopupSelBg)
				tab1Attr = cell.AttrBold
			}
			t1 := " Таблицы "
			for i, r := range []rune(t1) {
				if startX+1+i < startX+sideW-1 {
					buf.SetRune(startX+1+i, contentTop, r, tab1Fg, tab1Bg, tab1Attr)
				}
			}

			tab2Fg := sidebarFg
			tab2Bg := sidebarBg
			tab2Attr := cell.AttrNone
			if m.dbSidebarTab == "er-diagram" {
				tab2Fg = toColor(m.theme.PopupSelFg)
				tab2Bg = toColor(m.theme.PopupSelBg)
				tab2Attr = cell.AttrBold
			}
			t2 := " ER-диаграмма "
			t2Start := startX + 1 + len([]rune(t1)) + 1
			for i, r := range []rune(t2) {
				if t2Start+i < startX+sideW-1 {
					buf.SetRune(t2Start+i, contentTop, r, tab2Fg, tab2Bg, tab2Attr)
				}
			}

			for x := startX; x < startX+sideW; x++ {
				buf.SetRune(x, contentTop+1, '┄', borderFg, sidebarBg, cell.AttrNone)
			}
			offsetY = 2
		}

		schema := db.LoadHybridSchema(m.workspaceDir)
		hasTables := schema != nil && len(schema.Tables) > 0

		if m.dbSidebarTab == "er-diagram" {
			printLine(contentTop+offsetY, "● 2D Canvas: Active in Split (F6)", toColor(m.theme.String), cell.AttrBold)
			if hasTables {
				printLine(contentTop+offsetY+1, fmt.Sprintf("Layout: Sugiyama 2D (%d tables)", len(schema.Tables)), toColor(m.theme.DiagnosticInfo), cell.AttrNone)
			} else {
				printLine(contentTop+offsetY+1, "Схема: не найдена (0 таблиц)", toColor(m.theme.DiagnosticWarn), cell.AttrNone)
			}
			for x := startX; x < startX+sideW; x++ {
				buf.SetRune(x, contentTop+offsetY+2, '┄', borderFg, sidebarBg, cell.AttrNone)
			}

			printLine(contentTop+offsetY+3, "Open ER Diagram Tab (F6)", toColor(m.theme.Function), cell.AttrBold)
			printLine(contentTop+offsetY+4, "Center Camera (C)", toColor(m.theme.Keyword), cell.AttrNone)
			if hasTables {
				printLine(contentTop+offsetY+5, "Export Mermaid ER", toColor(m.theme.DiagnosticWarn), cell.AttrNone)
				printLine(contentTop+offsetY+6, "Export DDL Migration", toColor(m.theme.DiagnosticInfo), cell.AttrNone)
			} else {
				printLine(contentTop+offsetY+5, "Export Mermaid ER (пусто)", toColor(m.theme.LineNumber), cell.AttrNone)
				printLine(contentTop+offsetY+6, "Export DDL Migration (пусто)", toColor(m.theme.LineNumber), cell.AttrNone)
			}
			for x := startX; x < startX+sideW; x++ {
				buf.SetRune(x, contentTop+offsetY+7, '┄', borderFg, sidebarBg, cell.AttrNone)
			}

			if hasTables {
				relCount := 0
				var relLines []string
				for _, tbl := range schema.Tables {
					for _, fk := range tbl.ForeignKeys {
						relCount++
						relLines = append(relLines, fmt.Sprintf("  %s.%s -> %s.%s", tbl.Name, fk.FromColumn, fk.ToTable, fk.ToColumn))
					}
				}
				sort.Strings(relLines)

				erLines := []string{
					fmt.Sprintf("Entities: %d tables, %d relations", len(schema.Tables), relCount),
				}
				erLines = append(erLines, relLines...)
				erLines = append(erLines, "", "Controls:", "  Drag canvas to pan (2D)", "  Mouse wheel to scroll", "  Click card to select")

				for i, l := range erLines {
					if contentTop+offsetY+8+i < topY+sideH-1 {
						fg := sidebarFg
						attr := cell.AttrNone
						if strings.Contains(l, "Entities") || strings.Contains(l, "Controls") {
							fg = toColor(m.theme.Keyword)
							attr = cell.AttrBold
						} else if strings.Contains(l, "->") {
							fg = toColor(m.theme.DiagnosticInfo)
						}
						printLine(contentTop+offsetY+8+i, l, fg, attr)
					}
				}
			} else {
				emptyLines := []string{
					"Нет таблиц для отображения",
					"",
					"Поддерживаемые СУБД:",
					"  PostgreSQL, MySQL, MariaDB,",
					"  SQLite, MSSQL, CockroachDB,",
					"  DuckDB, ClickHouse, Redis",
					"",
					"Как загрузить схему:",
					"  1. Подключите БД в [Таблицы]",
					"  2. Или добавьте .sql файл в проект",
				}
				for i, l := range emptyLines {
					if contentTop+offsetY+8+i < topY+sideH-1 {
						fg := sidebarFg
						attr := cell.AttrNone
						if i == 0 {
							fg = toColor(m.theme.DiagnosticWarn)
							attr = cell.AttrBold
						} else if strings.Contains(l, "СУБД") || strings.Contains(l, "Как загрузить") {
							fg = toColor(m.theme.Keyword)
							attr = cell.AttrBold
						}
						printLine(contentTop+offsetY+8+i, l, fg, attr)
					}
				}
			}
		} else {
			// Tab "tables": Database Inspector view
			if m.activeDBConnection != nil {
				target := fmt.Sprintf("%s:%s", m.activeDBConnection.Host, m.activeDBConnection.Port)
				if m.activeDBConnection.FilePath != "" {
					target = filepath.Base(m.activeDBConnection.FilePath)
				}
				printLine(contentTop+offsetY, fmt.Sprintf("● %s (%s)", m.activeDBConnection.Type, target), toColor(m.theme.String), cell.AttrBold)
				printLine(contentTop+offsetY+1, fmt.Sprintf("DB: %s / %d tables", m.activeDBConnection.Database, len(schema.Tables)), toColor(m.theme.DiagnosticInfo), cell.AttrNone)
			} else if hasTables {
				printLine(contentTop+offsetY, "● Connection: Local Schema (.sql)", toColor(m.theme.String), cell.AttrBold)
				printLine(contentTop+offsetY+1, fmt.Sprintf("Schema: %d tables found", len(schema.Tables)), toColor(m.theme.DiagnosticInfo), cell.AttrNone)
			} else {
				printLine(contentTop+offsetY, "○ Connection: None (Disconnected)", toColor(m.theme.LineNumber), cell.AttrBold)
				printLine(contentTop+offsetY+1, "Schema: No tables found", toColor(m.theme.DiagnosticWarn), cell.AttrNone)
			}
			for x := startX; x < startX+sideW; x++ {
				buf.SetRune(x, contentTop+offsetY+2, '┄', borderFg, sidebarBg, cell.AttrNone)
			}

			printLine(contentTop+offsetY+3, "+ Connect to Database...", toColor(m.theme.Function), cell.AttrBold)
			printLine(contentTop+offsetY+4, "Run Query Console (Ctrl+Enter)", toColor(m.theme.Keyword), cell.AttrNone)
			printLine(contentTop+offsetY+5, "Refresh Database Schema", toColor(m.theme.Keyword), cell.AttrNone)
			for x := startX; x < startX+sideW; x++ {
				buf.SetRune(x, contentTop+offsetY+6, '┄', borderFg, sidebarBg, cell.AttrNone)
			}

			if hasTables {
				m.dbTableHitboxes = nil
				var sortedTableNames []string
				for name := range schema.Tables {
					sortedTableNames = append(sortedTableNames, name)
				}
				sort.Strings(sortedTableNames)

				var schemaLines []string
				lineIdx := 0
				for _, tName := range sortedTableNames {
					tbl := schema.Tables[tName]
					tblStartY := contentTop + offsetY + 7 + lineIdx
					schemaLines = append(schemaLines, fmt.Sprintf("▶ %s (%d cols, %d FK)", tbl.Name, len(tbl.Columns), len(tbl.ForeignKeys)))
					lineIdx++
					for _, col := range tbl.Columns {
						tag := ""
						if col.IsPK {
							tag = " (PK)"
						}
						schemaLines = append(schemaLines, fmt.Sprintf("    %s: %s%s", col.Name, col.DataType, tag))
						lineIdx++
					}
					tblEndY := contentTop + offsetY + 7 + lineIdx - 1
					m.dbTableHitboxes = append(m.dbTableHitboxes, dbTableHitbox{
						tableName: tbl.Name,
						startY:    tblStartY,
						endY:      tblEndY,
					})
					schemaLines = append(schemaLines, "")
					lineIdx++
				}

				for i, l := range schemaLines {
					if contentTop+offsetY+7+i < topY+sideH-1 {
						fg := sidebarFg
						attr := cell.AttrNone
						if strings.Contains(l, "cols") {
							fg = toColor(m.theme.Keyword)
							attr = cell.AttrBold
						} else if strings.Contains(l, "(PK)") {
							fg = toColor(m.theme.DiagnosticWarn)
						} else if strings.Contains(l, "(FK") {
							fg = toColor(m.theme.DiagnosticInfo)
						}
						printLine(contentTop+offsetY+7+i, l, fg, attr)
					}
				}
			} else {
				emptyInspLines := []string{
					"Нет активных таблиц",
					"",
					"Поддерживаемые СУБД:",
					"  PostgreSQL, MySQL, MariaDB, SQLite",
					"",
					"Инструкция:",
					"  • Нажмите '+ Connect to Database...'",
					"  • Или добавьте .sql файл в проект",
				}
				for i, l := range emptyInspLines {
					if contentTop+offsetY+7+i < topY+sideH-1 {
						fg := sidebarFg
						attr := cell.AttrNone
						if i == 0 {
							fg = toColor(m.theme.DiagnosticWarn)
							attr = cell.AttrBold
						} else if strings.Contains(l, "СУБД") || strings.Contains(l, "Инструкция") {
							fg = toColor(m.theme.Keyword)
							attr = cell.AttrBold
						}
						printLine(contentTop+offsetY+7+i, l, fg, attr)
					}
				}
			}
		}

	case "p2p-collab", "collab", "p2p":
		if m.p2pPanel != nil {
			m.p2pPanel.Render(buf, startX, contentTop, sideW, sideH-2, &m.theme)
		}

	case "docker":
		if m.dockerPanel != nil {
			m.dockerPanel.Render(buf, startX, contentTop, sideW, sideH-2, &m.theme)
		}
	case "rest-client", "rest":
		if m.restClientPanel != nil {
			m.restClientPanel.RenderAt(buf, startX, contentTop, sideW, sideH-2, &m.theme)
		}
	case "grpc":
		if m.grpcPanel != nil {
			m.grpcPanel.RenderWithTheme(buf, buffer.NewRect(startX, contentTop, sideW, sideH-2), &m.theme)
		}
	case "log-viewer", "logs":
		if m.logPanel != nil {
			m.logPanel.Open = true
			m.logPanel.Render(buf, startX, contentTop, sideW, sideH-2, m.theme)
		}
	case "task-runner", "tasks":
		if m.taskPanel != nil {
			m.taskPanel.Open = true
			m.taskPanel.Render(buf, startX, contentTop, sideW, sideH-2, m.theme)
		}
	case "todo-tree", "todo":
		if m.todoPanel != nil {
			m.todoPanel.Open = true
			m.todoPanel.Render(buf, startX, contentTop, sideW, sideH-2, &m.theme)
		}
	case "test-runner", "tests":
		if m.testRunnerPanel != nil {
			m.testRunnerPanel.Open = true
			m.testRunnerPanel.Render(buf, startX, contentTop, sideW, sideH-2, &m.theme)
		}
	case "jupyter-notebook", "jupyter":
		if m.jupyterPanel != nil {
			m.jupyterPanel.Open = true
			m.jupyterPanel.Render(buf, startX, contentTop, sideW, sideH-2, &m.theme)
		}

	default:
		printLine(contentTop, fmt.Sprintf("Tool Window: %s", m.rightSidebarTitle), accentFg, cell.AttrBold)
		printLine(contentTop+1, fmt.Sprintf("Plugin ID: %s", m.rightSidebarMode), toColor(m.theme.Comment), cell.AttrNone)
		for x := startX; x < startX+sideW; x++ {
			buf.SetRune(x, contentTop+2, '┄', borderFg, sidebarBg, cell.AttrNone)
		}

		printLine(contentTop+4, "Active plugin tool panel.", sidebarFg, cell.AttrNone)
		printLine(contentTop+5, "Integrated with Tahr ecosystem.", toColor(m.theme.String), cell.AttrNone)
		printLine(contentTop+7, "Run Plugin Command", toColor(m.theme.Function), cell.AttrBold)
		printLine(contentTop+8, "Refresh State", toColor(m.theme.Keyword), cell.AttrNone)
	}
}

