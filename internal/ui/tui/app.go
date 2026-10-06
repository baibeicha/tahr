package tui

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core"
	"tahr/internal/core/ai"
	corebuf "tahr/internal/core/buffer"
	"tahr/internal/core/clipboard"
	"tahr/internal/core/dap"
	"tahr/internal/core/bookmarks"
	"tahr/internal/core/coverage"
	"tahr/internal/core/db"
	"tahr/internal/core/git"
	"tahr/internal/core/gitlens"
	"tahr/internal/core/grpcproto"
	"tahr/internal/core/i18n"
	"tahr/internal/core/launch"
	"tahr/internal/core/logging"
	"tahr/internal/core/lsp"
	"tahr/internal/core/p2p"
	"tahr/internal/core/plugin"
	"tahr/internal/core/sdk"
	"tahr/internal/core/syntax"
	"tahr/internal/core/taskrunner"
	"tahr/internal/core/vim"
	"tahr/internal/ui"
	goatui "github.com/baibeicha/goatui/pkg/ui"
)









var latinToCyrillic = map[rune]rune{
	'q': 'й', 'w': 'ц', 'e': 'у', 'r': 'к', 't': 'е', 'y': 'н', 'u': 'г', 'i': 'ш', 'o': 'щ', 'p': 'з',
	'[': 'х', ']': 'ъ', 'a': 'ф', 's': 'ы', 'd': 'в', 'f': 'а', 'g': 'п', 'h': 'р', 'j': 'о', 'k': 'л',
	'l': 'д', ';': 'ж', '\'': 'э', 'z': 'я', 'x': 'ч', 'c': 'с', 'v': 'м', 'b': 'и', 'n': 'т', 'm': 'ь',
	',': 'б', '.': 'ю',
}



// AppModel implements the goatui/pkg/tea.Model interface for Tahr TUI.
type AppModel struct {
	eng               *core.Engine
	theme             ui.Theme
	treeEngine        *syntax.TreeSitterEngine
	highlighter       *syntax.DefaultHighlighter
	treeSitterEnabled bool

	// LSP and Plugins
	lspClient     *lsp.Client
	lspDocVersion int
	lspCurrentCmd string
	pluginMgr     *plugin.Manager

	// DAP (Debug Adapter Protocol)
	dapSession        *dap.Session
	stoppedMarkerLine int // -1 when not paused, line index when paused at breakpoint

	// Clipboard
	clipboardText string

	width  int
	height int

	// Viewport offset for active document
	viewportY int
	viewportX int

	// Omnibar modal state
	omnibarOpen   bool
	omnibarMode   string // "files", "commands", "find", "goto", "open_dir"
	omnibarQuery  string
	omnibarItems  []string
	omnibarSel    int
	omnibarScroll int

	// Popup state (completions, hover)
	popupVisible bool
	popupTitle   string
	popupItems   []string
	popupActive  int
	popupDoc     string

	// Diagnostics & Breakpoints
	diagMu         sync.RWMutex
	diagnostics    map[int]string
	diagDetails    map[int][]lsp.Diagnostic
	docDiagnostics map[string]map[int]string
	docDiagDetails map[string]map[int][]lsp.Diagnostic
	breakpoints    map[int]bool

	// Workspace & File Explorer / Structure Sidebar
	workspaceDir    string
	allProjectFiles []string
	sidebarOpen     bool
	sidebarFocused  bool
	sidebarWidth    int
	sidebarScrollY  int
	sidebarMode     int // 0: Project Explorer, 1: Document Structure
	treeRoot        *FileNode
	treeFlat        []*FileNode
	treeSel         int
	structurePanel  *StructurePanel

	// Build & Run Output Drawer
	runner       *Runner
	outputOpen   bool
	outputHeight int

	// Terminal dynamic resize state
	termDragging    bool
	termDragStartY  int
	termDragStartH  int

	// Sidebar dynamic resize state
	sidebarDragging   bool
	sidebarDragStartX int
	sidebarDragStartW int

	// Right Sidebar dynamic resize state
	rightSidebarDragging    bool
	rightSidebarDragStartX int
	rightSidebarDragStartW int

	// Editor click & double click tracking
	lastEditorClickTime time.Time
	lastEditorClickLine int
	lastEditorClickCol  int

	// Modals: Find & Replace, Project Rename, New Project, Search in Files, Problems, P2P & DB Diff
	findReplaceModal   *FindReplaceModal
	renameModal        *RenameModal
	newProjectModal    *NewProjectModal
	searchInFilesModal *SearchInFilesModal
	problemsPanel      *ProblemsPanel
	p2pModal           *P2PModal
	dbDiffModal        *DBDiffModal

	// Vim Modal Editing & Snippets
	vimFSM         *vim.FSM
	snippetSession *corebuf.SnippetSession

	// Toasts & Notifications
	toasts *goatui.ToastManager

	// Settings & Launch Configurations
	settings     *SettingsState
	launchConfig *launch.Config
	launchModal  *LaunchConfigModal

	// Split View Manager (1 to 6 panes)
	splits *SplitManager

	// Ghost Text & Autocompletion scroll
	ghostText         string
	ghostPrefix       string
	popupScrollOffset int

	// AI Inline Shadow Coder
	aiEngine        *ai.Engine
	aiCancel        context.CancelFunc
	aiDebounceTimer *time.Timer
	aiMu            sync.Mutex

	// Animations
	sidebarAnimWidth   float64
	sidebarTargetWidth float64

	// Secondary Right Sidebar & Plugin Tool Windows
	rightSidebarOpen        bool
	rightSidebarWidth       int
	rightSidebarTargetWidth float64
	rightSidebarAnimWidth   float64
	rightSidebarMode        string // "ai-chat", "db-inspector", "project-graphs", etc.
	rightSidebarPluginID    string
	rightSidebarTitle       string
	rightSidebarMessages    []AIChatMsg
	dbSidebarTab            string // "tables" or "er-diagram"
	chatPanel               *ChatPanel
	dagCanvasWidget         *DAGCanvasWidget
	projectGraphPanel       *ProjectGraphPanel
	canvasDragging          bool
	canvasDragStartX        int
	canvasDragStartY        int
	dataGrids               map[string]*DataGridWidget
	dbTableHitboxes         []dbTableHitbox
	dbConnectModal          *DBConnectModal
	activeDBConnection      *db.ConnectionProfile

	// Tree file operations prompt
	treePromptOpen   bool
	treePromptMode   string // "new_file", "new_dir", "rename", "delete", "move"
	treePromptText   string
	treePromptTarget string // node path

	// Project tree right-click context menu
	contextMenuOpen   bool
	contextMenuX      int
	contextMenuY      int
	contextMenuSel    int
	contextMenuItems  []contextMenuItem
	contextMenuTarget string

	// Mouse hover tooltip
	tooltipText string
	tooltipX    int
	tooltipY    int

	// Terminal hardware cursor tracking
	activeCursorScreenX int
	activeCursorScreenY int

	// Git Tracker & Background Watcher
	gitTracker *git.Tracker
	gitWatcher *git.BackgroundGitWatcher
	sdkManager *sdk.Manager

	// Top dropdowns
	mainMenuOpen        bool
	mainMenuSel         int
	profileDropdownOpen bool
	profileDropdownSel  int

	// Terminal tab hitboxes & double click detection
	termTabHitboxes       []termTabHitbox
	termHeaderBtnHitboxes []termHeaderBtnHitbox
	lastTabClickTime      time.Time
	lastTabClickIdx       int

	// Integrated Terminal Drawer
	terminal        *TerminalDrawer
	terminalFocused bool

	// DAP Debugger HUD
	dapHUD *DAPHUDState

	// Marketplace Modal
	marketplace *MarketplaceModal

	// Mouse Wheel Acceleration & Auto-Save Timer
	lastWheelTime time.Time
	lastEditTime  time.Time

	// LSP Inlay Hints & Semantic Tokens
	inlayHints     map[int][]lsp.InlayHint
	semanticTokens map[int][]lsp.DecodedSemanticToken
	program        ProgramSender

	// Quick Fix & Code Actions Popup (Alt+Enter)
	quickFixModal *QuickFixModal

	// Interactive Git Modal (Hunk Staging & Branch Graph)
	gitModal *GitModal

	// Missing tool / auto-install prompt modal
	toolPrompt *ToolPromptModal

	// Live Markdown ANSI/ASCII Preview
	mdPreviewOpen bool
	mdRenderer    *MarkdownRenderer

	// Minimap Hitboxes
	minimapHitboxes []minimapHitbox

	// In-Editor Color Swatches and Palette Picker
	editorColorPicker   *ColorPickerModal
	editorColorSwatches []EditorColorSwatch

	// Image Viewer Cache & Floating Hover Documentation Popup
	imageStateCache map[string]*ImageViewerState
	hoverDoc        *HoverDocState

	// Startup splash, crash recovery, bug reporter, and data inspector
	splash         *SplashScreenState
	crashDialog    *CrashRecoveryDialog
	bugReportModal *BugReportModal
	logInspector   *LogInspectorModal

	// Hitboxes for interactive mouse clicks
	toolbarButtons []toolbarBtn
	tabHitboxes    []tabHitbox

	// Ecosystem Modals
	devtoolsModal  *DevToolsModal
	regexModal     *RegexModal
	bookmarksModal *BookmarksModal

	// Ecosystem Panels
	dockerPanel     *DockerPanel
	restClientPanel *RESTClientPanel
	grpcPanel       *GRPCPanel
	logPanel        *LogPanel
	taskPanel       *TaskPanel
	todoPanel       *TodoPanel
	testRunnerPanel *TestRunnerPanel
	jupyterPanel    *JupyterPanel
	p2pPanel        *P2PPanel

	// Ecosystem Services
	bookmarkStore  *bookmarks.Store
	coverageEngine *coverage.Engine
	gitlensTracker *gitlens.GutterTracker

	statusMessage string
	quitting      bool
}




type animTickMsg struct{}

type termTickMsg struct{}


type aiCompletionMsg struct {
	docID      string
	cursorLine int
	cursorCol  int
	completion string
}

// ProgramSender allows background goroutines (such as LSP handlers) to dispatch events to the TEA event loop.
type ProgramSender interface {
	Send(msg tea.Msg)
}

func tickTerm() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(30 * time.Millisecond)
		return termTickMsg{}
	}
}

type toolbarBtn struct {
	id   string
	minX int
	maxX int
}

type tabHitbox struct {
	docID  string
	minX   int
	maxX   int
	closeX int
}

type termTabHitbox struct {
	minX        int
	maxX        int
	closeMinX   int
	closeMaxX   int
	instanceIdx int
	isAdd       bool
	isSplit     bool
}

type termHeaderBtnHitbox struct {
	id   string
	minX int
	maxX int
}


// NewAppModel instantiates a new Tahr TUI Model.
func NewAppModel(eng *core.Engine) *AppModel {
	if eng == nil {
		eng = core.NewEngine()
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	logging.Info("Initializing AppModel for workspace: %s", cwd)

	var pm *plugin.Manager
	if mgr, err := plugin.NewManager(""); err == nil {
		pm = mgr
	}
	settingsState := NewSettingsState()
	if pm != nil && settingsState != nil {
		settingsState.SetPluginManager(pm)
		if len(settingsState.Current.PreferredLanguageProviders) > 0 {
			pm.SetPreferredProviders(settingsState.Current.PreferredLanguageProviders)
		}
	}

	launchCfg, _ := launch.Load(cwd)
	if launchCfg == nil {
		launchCfg = launch.DefaultProfiles(cwd)
	}

	initialSidebarWidth := 26
	if settingsState != nil && settingsState.Current.SidebarWidth >= 12 {
		initialSidebarWidth = settingsState.Current.SidebarWidth
	}

	m := &AppModel{
		eng:               eng,
		theme:             ui.DefaultTheme(),
		treeEngine:        syntax.NewTreeSitterEngine(),
		highlighter:       syntax.NewDefaultHighlighter(),
		treeSitterEnabled: true,
		dapSession:        dap.NewSession(),
		stoppedMarkerLine: -1,
		width:             80,
		height:            24,
		diagnostics:         make(map[int]string),
		diagDetails:         make(map[int][]lsp.Diagnostic),
		docDiagnostics:      make(map[string]map[int]string),
		docDiagDetails:      make(map[string]map[int][]lsp.Diagnostic),
		breakpoints:         make(map[int]bool),
		workspaceDir:        cwd,
		sidebarOpen:         false,
		sidebarFocused:      false,
		sidebarWidth:        initialSidebarWidth,
		sidebarScrollY:      0,
		sidebarMode:         0,
		structurePanel:      NewStructurePanel(),
		treeFlat:            make([]*FileNode, 0),
		runner:              NewRunner(),
		outputOpen:          false,
		outputHeight:        7,
		findReplaceModal:    NewFindReplaceModal(),
		renameModal:         NewRenameModal(),
		newProjectModal:     NewNewProjectModal(cwd),
		searchInFilesModal:  NewSearchInFilesModal(),
		problemsPanel:       NewProblemsPanel(),
		vimFSM:              vim.NewFSM(),
		toasts:              goatui.NewToastManager(4),
		settings:            settingsState,
		launchConfig:        launchCfg,
		launchModal:         NewLaunchConfigModal(launchCfg),
		splits:              NewSplitManager(),
		gitTracker:          git.NewTracker(),
		gitWatcher:          git.NewBackgroundGitWatcher(cwd, nil),
		sdkManager:          sdk.GetManager(),
		terminal:            NewTerminalDrawer(cwd),
		terminalFocused:     false,
		dapHUD:              NewDAPHUDState(),
		marketplace:         NewMarketplaceModal(pm),
		pluginMgr:           pm,
		lastWheelTime:       time.Now(),
		sidebarAnimWidth:        0,
		sidebarTargetWidth:      0,
		rightSidebarOpen:        false,
		rightSidebarWidth:       34,
		rightSidebarTargetWidth: 0,
		rightSidebarAnimWidth:   0,
		rightSidebarMode:        "ai-chat",
		rightSidebarTitle:       "AI Assistant",
		rightSidebarMessages: []AIChatMsg{
			{Role: "ai", Content: "Hello! I am your Tahr AI Assistant. Ask questions, generate tests, or inspect symbols."},
		},
		dbSidebarTab:        "tables",
		inlayHints:          make(map[int][]lsp.InlayHint),
		semanticTokens:      make(map[int][]lsp.DecodedSemanticToken),
		quickFixModal:       NewQuickFixModal(),
		gitModal:            NewGitModal(cwd),
		toolPrompt:          NewToolPromptModal(),
		mdRenderer:          NewMarkdownRenderer(""),
		editorColorPicker:   NewColorPickerModal(),
		editorColorSwatches: make([]EditorColorSwatch, 0),
		imageStateCache:     make(map[string]*ImageViewerState),
		hoverDoc:            NewHoverDocState(),
		splash:              NewSplashScreenState(),
		crashDialog:         NewCrashRecoveryDialog(),
		bugReportModal:      NewBugReportModal(),
		logInspector:        NewLogInspectorModal(),
		allProjectFiles:     make([]string, 0),
	}

	m.dbConnectModal = NewDBConnectModal(&m.theme)
	m.dbConnectModal.OnSave = func(p db.ConnectionProfile) {
		_ = db.SaveConnectionProfile(m.workspaceDir, p)
		m.activeDBConnection = &p
		m.statusMessage = fmt.Sprintf("Connected to %s", p.Type)
		if m.toasts != nil {
			target := p.Host + ":" + p.Port
			if p.FilePath != "" {
				target = p.FilePath
			}
			m.toasts.Success("DB CONNECTED", fmt.Sprintf("Connected to %s (%s)", p.Type, target))
		}
	}
	savedConns := db.LoadConnectionProfiles(cwd)
	if len(savedConns) > 0 {
		m.activeDBConnection = &savedConns[0]
	}

	if m.splash != nil {
		m.splash.Active = false
	}

	if m.crashDialog != nil {
		m.crashDialog.CheckAndOpen()
	}

	if m.pluginMgr != nil {
		m.pluginMgr.SetDynamicSDKProvider(func(sdkID string) string {
			if sdkID == "go" && m.sdkManager != nil {
				return m.sdkManager.GoVersionShort()
			}
			return ""
		})
	}

	aiCfg := m.getAIConfig()
	m.aiEngine = ai.NewEngine(aiCfg, cwd)
	if aiCfg.Enabled && flag.Lookup("test.v") == nil {
		go func() { _ = m.aiEngine.Start() }()
		m.triggerAIAutoDownload()
	}

	chatCfg := m.getAIChatConfig()
	chatEng := ai.NewChatEngine(chatCfg)
	m.chatPanel = NewChatPanel(chatEng)
	m.chatPanel.OnApplyCode = func(code string) {
		doc := m.eng.ActiveDocument()
		if doc == nil {
			return
		}
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: code})
		m.notifyLSPChange()
		m.ensureCursorVisible()
		_ = m.onTextMutation()
		if m.toasts != nil {
			m.toasts.Success("AI", "Applied code snippet to document")
		}
	}
	m.chatPanel.OnInsertCode = func(code string) {
		doc := m.eng.ActiveDocument()
		if doc == nil {
			return
		}
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: code})
		m.notifyLSPChange()
		m.ensureCursorVisible()
		_ = m.onTextMutation()
		if m.toasts != nil {
			m.toasts.Success("AI", "Inserted code snippet at cursor")
		}
	}
	m.chatPanel.OnCopyCode = func(code string) {
		_ = clipboard.Write(code)
		m.clipboardText = code
		if m.toasts != nil {
			m.toasts.Success("CLIPBOARD", fmt.Sprintf("Copied %d characters", len(code)))
		}
	}
	m.chatPanel.OnToast = func(level, title, msg string) {
		if m.toasts == nil {
			return
		}
		switch level {
		case "error":
			m.toasts.Error(title, msg)
		case "warn":
			m.toasts.Warn(title, msg)
		default:
			m.toasts.Info(title, msg)
		}
	}
	m.chatPanel.OnClose = func() {
		_ = m.ToggleRightSidebar(m.rightSidebarMode)
	}

	if m.settings != nil {
		m.settings.OnColorApplied = func(key, hexVal string) {
			m.applyCurrentSettings()
		}
		m.settings.OnThemeChanged = func(themeName string) {
			m.applyCurrentSettings()
		}
		m.settings.OnSettingsChanged = func() {
			m.applyCurrentSettings()
			if m.aiEngine != nil {
				aiCfg := m.getAIConfig()
				m.aiEngine.UpdateConfig(aiCfg)
				if aiCfg.Enabled && flag.Lookup("test.v") == nil {
					m.triggerAIAutoDownload()
				}
			}
			if m.chatPanel != nil && m.chatPanel.Engine != nil {
				m.chatPanel.Engine.UpdateConfig(m.getAIChatConfig())
			}
		}
		m.settings.OnToolInstallStarted = func(toolName, cmd string) {
			m.toasts.Info("INSTALL", fmt.Sprintf("Installing %s via '%s'...", toolName, cmd))
		}
		m.settings.OnToolInstallFinished = func(toolName string, success bool, errMsg string) {
			if success {
				m.toasts.Success("INSTALL", fmt.Sprintf("%s installed successfully!", toolName))
			} else {
				m.toasts.Error("INSTALL", fmt.Sprintf("Failed to install %s: %s", toolName, errMsg))
			}
		}
	}

	if m.launchModal != nil {
		m.launchModal.OnLaunch = func(p launch.Profile) {
			m.launchModal.Open = false
			m.runProfile(p)
		}
		m.launchModal.OnDebug = func(p launch.Profile) {
			m.launchModal.Open = false
			m.debugProfile(p)
		}
	}

	if m.newProjectModal != nil {
		m.newProjectModal.OnCreateTemplate = func(tpl plugin.ProjectTemplate, name, location, module string) {
			_ = m.CreateProjectFromTemplate(tpl, name, location, module)
		}
		m.newProjectModal.OnCreate = func(kind, targetPath string) {
			_ = m.CreateProject(kind, m.workspaceDir, targetPath)
		}
	}

	if m.searchInFilesModal != nil {
		m.searchInFilesModal.OnJump = func(filePath string, line, col int) {
			_, err := m.eng.Open(filePath)
			if err == nil {
				m.jumpToLine(line, col)
				m.statusMessage = fmt.Sprintf("Opened %s:%d", filepath.Base(filePath), line+1)
			}
		}
	}

	if m.problemsPanel != nil {
		m.problemsPanel.OnJump = func(filePath string, line, col int) {
			_, err := m.eng.Open(filePath)
			if err == nil {
				m.jumpToLine(line, col)
				m.statusMessage = fmt.Sprintf("Jumped to problem in %s:%d", filepath.Base(filePath), line+1)
			}
		}
	}

	if m.terminal != nil {
		m.terminal.OnData = func() {
			if m.program != nil {
				m.program.Send(termTickMsg{})
			}
		}
	}

	m.applyCurrentSettings()

	m.dapSession.SetOnStopped(func(file string, line int, reason string) {
		m.stoppedMarkerLine = line
		m.outputOpen = true
		if m.dapHUD != nil {
			m.dapHUD.Open = true
		}
		vars := m.dapSession.Variables()
		varLines := []string{
			fmt.Sprintf("─── Breakpoint Hit at %s:%d (%s) ───", filepath.Base(file), line+1, reason),
			"Local Variables:",
		}
		for _, v := range vars {
			varLines = append(varLines, fmt.Sprintf("  %s (%s) = %s", v.Name, v.Type, v.Value))
		}
		if len(vars) == 0 {
			varLines = append(varLines, "  (no local variables)")
		}
		m.runner.lines = varLines
		m.statusMessage = fmt.Sprintf("Debugging: Paused at %s:%d", filepath.Base(file), line+1)
		doc := m.eng.ActiveDocument()
		if doc != nil {
			pos := corebuf.Position{Line: line, Column: 0}
			doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(pos, pos)})
			m.ensureCursorVisible()
		}
	})

	// Initialize Ecosystem Components
	m.bookmarkStore = bookmarks.NewStore(cwd)
	_ = m.bookmarkStore.Load()
	m.bookmarksModal = NewBookmarksModal(m.bookmarkStore)
	m.bookmarksModal.OnJump = func(bm bookmarks.Bookmark) {
		m.openFileAtLocation(bm.FilePath, bm.LineNumber)
	}

	m.devtoolsModal = NewDevToolsModal()
	m.devtoolsModal.OnToast = func(level, title, msg string) {
		if m.toasts != nil {
			switch level {
			case "success":
				m.toasts.Success(title, msg)
			case "error":
				m.toasts.Error(title, msg)
			default:
				m.toasts.Info(title, msg)
			}
		}
	}

	m.regexModal = NewRegexModal()
	m.regexModal.OnToast = func(level, title, msg string) {
		if m.toasts != nil {
			switch level {
			case "success":
				m.toasts.Success(title, msg)
			case "error":
				m.toasts.Error(title, msg)
			default:
				m.toasts.Info(title, msg)
			}
		}
	}

	m.dockerPanel = NewDockerPanel(cwd)
	m.dockerPanel.OnToast = func(level, title, msg string) {
		if m.toasts != nil {
			switch level {
			case "success":
				m.toasts.Success(title, msg)
			case "error":
				m.toasts.Error(title, msg)
			default:
				m.toasts.Info(title, msg)
			}
		}
	}

	m.restClientPanel = NewRESTClientPanel(&m.theme)
	m.restClientPanel.OnCopy = func(text string) {
		_ = clipboard.Write(text)
		if m.toasts != nil {
			m.toasts.Success("REST CLIENT", "Copied to clipboard")
		}
	}

	protoWs := grpcproto.NewProtoWorkspace(cwd)
	m.grpcPanel = NewGRPCPanel(protoWs, &m.theme)
	m.grpcPanel.OnToast = func(level, title, msg string) {
		if m.toasts != nil {
			switch level {
			case "success":
				m.toasts.Success(title, msg)
			case "error":
				m.toasts.Error(title, msg)
			default:
				m.toasts.Info(title, msg)
			}
		}
	}
	m.grpcPanel.OnCopy = func(text string) {
		_ = clipboard.Write(text)
		if m.toasts != nil {
			m.toasts.Success("gRPC", "Copied to clipboard")
		}
	}

	m.logPanel = NewLogPanel()

	m.taskPanel = NewTaskPanel()
	m.taskPanel.OnRunTask = func(task taskrunner.Task) {
		if m.toasts != nil {
			m.toasts.Info("TASK RUNNER", fmt.Sprintf("Running task: %s", task.Name))
		}
		if m.runner != nil && task.Command != "" {
			parts := strings.Fields(task.Command)
			if len(parts) > 0 {
				_ = m.runner.StartProcess(m.workspaceDir, parts[0], parts[1:], "Task: "+task.Name)
				m.outputOpen = true
			}
		}
	}

	m.todoPanel = NewTodoPanel(cwd)
	m.todoPanel.OnOpenFile = func(filePath string, line int) {
		m.openFileAtLocation(filePath, line)
	}
	m.todoPanel.OnToast = func(level, title, msg string) {
		if m.toasts != nil {
			switch level {
			case "error":
				m.toasts.Error(title, msg)
			default:
				m.toasts.Info(title, msg)
			}
		}
	}

	m.testRunnerPanel = NewTestRunnerPanel(cwd)
	m.testRunnerPanel.OnOpenFile = func(path string, line int) {
		m.openFileAtLocation(path, line)
	}
	m.testRunnerPanel.OnToast = func(level, title, msg string) {
		if m.toasts != nil {
			switch level {
			case "success":
				m.toasts.Success(title, msg)
			case "error":
				m.toasts.Error(title, msg)
			default:
				m.toasts.Info(title, msg)
			}
		}
	}

	m.jupyterPanel = NewJupyterPanel(cwd)
	m.jupyterPanel.OnToast = func(level, title, msg string) {
		if m.toasts != nil {
			switch level {
			case "success":
				m.toasts.Success(title, msg)
			case "error":
				m.toasts.Error(title, msg)
			default:
				m.toasts.Info(title, msg)
			}
		}
	}

	m.p2pPanel = NewP2PPanel(&m.theme)
	m.p2pPanel.OnToast = func(level, title, msg string) {
		if m.toasts != nil {
			switch level {
			case "success":
				m.toasts.Success(title, msg)
			case "error":
				m.toasts.Error(title, msg)
			case "warn":
				m.toasts.Warn(title, msg)
			default:
				m.toasts.Info(title, msg)
			}
		}
	}
	m.p2pPanel.OnCopyCode = func(code string) {
		_ = clipboard.Write(code)
		if m.toasts != nil {
			m.toasts.Success("P2P COLLAB", i18n.T("p2p.toast_copied", code))
		}
	}
	m.p2pPanel.OnStartHost = func(nickname string) {
		session := p2p.NewHostSession(nickname, "")
		m.p2pPanel.Session = session
		m.wireP2PSession(session)
		session.Coordinator.StartCascade(context.Background(), session.SessionCode, true)
		if m.toasts != nil {
			m.toasts.Success("P2P HOST", i18n.T("p2p.toast_host_created", session.SessionCode))
		}
	}
	m.p2pPanel.OnJoinSession = func(code, nickname string) {
		session := p2p.NewGuestSession(code, nickname)
		m.p2pPanel.Session = session
		m.wireP2PSession(session)
		session.Coordinator.StartCascade(context.Background(), session.SessionCode, false)
		if m.toasts != nil {
			m.toasts.Info("P2P JOIN", i18n.T("p2p.toast_joining", code))
		}
	}
	m.p2pPanel.OnLeaveSession = func() {
		if m.p2pPanel.Session != nil {
			_ = m.p2pPanel.Session.Close()
			m.p2pPanel.Session = nil
			if m.toasts != nil {
				m.toasts.Info("P2P COLLAB", i18n.T("p2p.toast_session_closed"))
			}
		}
	}
	m.p2pPanel.OnAcceptGuest = func(peerID uint16, role string, pty bool) {
		if m.p2pPanel.Session != nil {
			err := m.p2pPanel.Session.AcceptGuest(peerID, role, pty)
			if err == nil && m.toasts != nil {
				m.toasts.Success("P2P ADMIT", i18n.T("p2p.toast_admitted", peerID, role))
			}
		}
	}
	m.p2pPanel.OnDeclineGuest = func(peerID uint16) {
		if m.p2pPanel.Session != nil {
			_ = m.p2pPanel.Session.DeclineGuest(peerID, "rejected by host")
			if m.toasts != nil {
				m.toasts.Warn("P2P ADMIT", i18n.T("p2p.toast_declined", peerID))
			}
		}
	}
	m.p2pPanel.OnFollowPeer = func(peer *p2p.PeerInfo) {
		if peer != nil && peer.ActiveURI != "" {
			targetLine := 1
			if peer.CursorRune > 0 {
				targetLine = max(1, peer.CursorRune/80+1)
			}
			m.openFileAtLocation(peer.ActiveURI, targetLine)
			if m.toasts != nil {
				m.toasts.Info("P2P FOLLOW", i18n.T("p2p.toast_following", peer.Nickname, peer.ActiveURI))
			}
		}
	}

	m.coverageEngine = coverage.NewEngine()
	m.gitlensTracker = gitlens.NewGutterTracker()

	m.refreshProjectTree()
	m.applyCurrentSettings()
	return m
}


// SetThemeByName updates the active theme by name and saves it to settings.json.
func (m *AppModel) SetThemeByName(themeName string) {
	themeName = strings.TrimSpace(themeName)
	if themeName == "" {
		return
	}
	if m.settings != nil {
		m.settings.Current.Theme = themeName
		_ = m.settings.Save()
	}
	m.applyCurrentSettings()
}




// applyCurrentSettings merges active theme, custom hex colors, indentation, scrolloff, and default split layout.
func (m *AppModel) applyCurrentSettings() {
	if m.settings == nil {
		return
	}
	if m.pluginMgr != nil {
		m.pluginMgr.SetPreferredProviders(m.settings.Current.PreferredLanguageProviders)
	}
	var th ui.Theme
	appliedTheme := false
	themeName := strings.ToLower(m.settings.Current.Theme)
	switch themeName {
	case "gruvbox", "gruvbox-dark":
		th = ui.GruvboxDark()
		appliedTheme = true
	case "tokyo", "tokyonight", "tokyo-night":
		th = ui.TokyoNight()
		appliedTheme = true
	case "catppuccin", "catppuccin-mocha":
		th = ui.CatppuccinMocha()
		appliedTheme = true
	case "dracula", "dracula-theme":
		th = ui.Dracula()
		appliedTheme = true
	case "nord", "nord-theme":
		th = ui.Nord()
		appliedTheme = true
	case "monokai", "monokai-pro", "monokai-theme":
		th = ui.Monokai()
		appliedTheme = true
	default:
		if m.pluginMgr != nil {
			for tName, tCfg := range m.pluginMgr.InstalledThemes() {
				if strings.EqualFold(tName, themeName) {
					th = ui.ThemeFromMap(tName, ui.CatppuccinMocha(), tCfg.Colors)
					appliedTheme = true
					break
				}
			}
		}
		if !appliedTheme {
			th = ui.CatppuccinMocha()
		}
	}
	m.settings.ApplyCustomColors(&th)
	m.SetTheme(th)

	// Apply dynamic indentation and scrolloff to core Engine and viewports
	ts := m.settings.Current.TabSize
	if ts <= 0 {
		ts = 4
	}
	if m.eng != nil {
		m.eng.SetIndentation(ts, m.settings.Current.UseSpaces)
		m.eng.SetAutoPairsEnabled(m.settings.Current.AutoPairs)
		if doc := m.eng.ActiveDocument(); doc != nil {
			doc.Viewport.ScrolloffY = m.settings.Current.ScrolloffY
			doc.Viewport.GutterWidth = m.gutterWidth()
		}
	}

	if m.splits != nil {
		switch m.settings.Current.DefaultSplit {
		case 2:
			m.splits.SetLayout(Split2Cols)
		case 3:
			m.splits.SetLayout(Split3Cols)
		case 4:
			m.splits.SetLayout(Split4Grid)
		case 5:
			m.splits.SetLayout(Split5Panes)
		case 6:
			m.splits.SetLayout(Split6Grid)
		default:
			m.splits.SetLayout(SplitSingle)
		}
	}
}






type rightStripItem struct {
	id    string
	r1    rune
	r2    rune
	title string
	mode  string
}





type dbTableHitbox struct {
	tableName string
	startY    int
	endY      int
}















// EnableSplashScreen activates the startup splash animation.
func (m *AppModel) EnableSplashScreen() {
	if m.splash == nil {
		m.splash = NewSplashScreenState()
	}
	m.splash.Active = true
	m.splash.Done = false
	m.splash.Started = false
}











// SetProgram attaches the GoatUI program event loop sender.
func (m *AppModel) SetProgram(p ProgramSender) {
	m.program = p
	if m.terminal != nil {
		m.terminal.OnData = func() {
			if m.program != nil {
				m.program.Send(termTickMsg{})
			}
		}
	}
}



// Close safely stops background workers, sidecars, watchers, and language servers.
func (m *AppModel) Close() {
	if m.aiEngine != nil {
		m.aiEngine.Stop()
	}
	if m.gitWatcher != nil {
		m.gitWatcher.Stop()
	}
	if m.lspClient != nil {
		_ = m.lspClient.Close()
	}
	if m.dapSession != nil {
		_ = m.dapSession.Stop()
	}
}







// SetProjectFiles updates the file list available for Omnibar search.
func (m *AppModel) SetProjectFiles(files []string) {
	m.allProjectFiles = files
}

// SetTheme updates the editor color theme.
func (m *AppModel) SetTheme(t ui.Theme) {
	m.theme = t
}

// Engine returns the underlying core engine.
func (m *AppModel) Engine() *core.Engine {
	return m.eng
}

type toastTickMsg time.Time

func toastTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return toastTickMsg(t)
	})
}

type splashTickMsg struct{}

func splashTick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(30 * time.Millisecond)
		return splashTickMsg{}
	}
}

// Init initializes the TEA model.
func (m *AppModel) Init() tea.Cmd {
	cmds := []tea.Cmd{toastTick()}
	if m.splash != nil && m.splash.Active {
		m.splash.Started = true
		m.splash.StartTime = time.Now()
		cmds = append(cmds, splashTick())
	}
	return tea.Batch(cmds...)
}

type autoSaveDelayMsg struct{}

func scheduleAutoSaveDelay() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(1000 * time.Millisecond)
		return autoSaveDelayMsg{}
	}
}

// autoSaveAllModified atomically saves any modified buffers with valid file paths.
func (m *AppModel) autoSaveAllModified() {
	if m.eng == nil {
		return
	}
	for _, doc := range m.eng.Documents() {
		if doc.Buffer != nil && doc.Buffer.IsModified() && doc.FilePath != "" {
			if err := doc.Buffer.SaveAtomic(doc.FilePath); err == nil {
				m.statusMessage = fmt.Sprintf("Auto-saved %s", filepath.Base(doc.FilePath))
				if m.toasts != nil {
					m.toasts.Success("AUTO-SAVE", filepath.Base(doc.FilePath))
				}
			}
		}
	}
}

func (m *AppModel) onTextMutation() tea.Cmd {
	m.lastEditTime = time.Now()
	if m.settings != nil && strings.ToLower(m.settings.Current.AutoSave) == "after_delay" {
		return scheduleAutoSaveDelay()
	}
	return nil
}

// Update handles state changes via incoming messages.
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case aiCompletionMsg:
		doc := m.eng.ActiveDocument()
		if doc != nil && doc.ID == msg.docID {
			sel := doc.Buffer.PrimarySelection()
			if sel.Head.Line == msg.cursorLine && sel.Head.Column == msg.cursorCol {
				m.ghostText = msg.completion
			}
		}
		return m, nil

	case toastTickMsg:
		if m.toasts != nil {
			m.toasts.Tick(time.Now())
		}
		m.checkHoverDocTrigger(time.Now())
		return m, toastTick()

	case splashTickMsg:
		if m.splash != nil && m.splash.Active {
			m.splash.Tick()
			return m, splashTick()
		}
		return m, nil

	case tea.BlurMsg:
		if m.settings != nil {
			mode := strings.ToLower(m.settings.Current.AutoSave)
			if mode == "on_focus_loss" || mode == "after_delay" {
				m.autoSaveAllModified()
			}
		}
		return m, nil

	case autoSaveDelayMsg:
		if m.settings != nil && strings.ToLower(m.settings.Current.AutoSave) == "after_delay" {
			if time.Since(m.lastEditTime) >= 950*time.Millisecond {
				m.autoSaveAllModified()
			}
		}
		return m, nil

	case animTickMsg:
		animSidebar := false
		diff := m.sidebarTargetWidth - m.sidebarAnimWidth
		if math.Abs(diff) > 0.8 {
			step := diff * 0.6
			if math.Abs(step) < 2.0 {
				if diff > 0 {
					step = 2.0
				} else {
					step = -2.0
				}
			}
			m.sidebarAnimWidth += step
			if (diff > 0 && m.sidebarAnimWidth >= m.sidebarTargetWidth) || (diff < 0 && m.sidebarAnimWidth <= m.sidebarTargetWidth) {
				m.sidebarAnimWidth = m.sidebarTargetWidth
			} else {
				animSidebar = true
			}
		} else {
			m.sidebarAnimWidth = m.sidebarTargetWidth
		}

		diffR := m.rightSidebarTargetWidth - m.rightSidebarAnimWidth
		if math.Abs(diffR) > 0.8 {
			stepR := diffR * 0.6
			if math.Abs(stepR) < 2.0 {
				if diffR > 0 {
					stepR = 2.0
				} else {
					stepR = -2.0
				}
			}
			m.rightSidebarAnimWidth += stepR
			if (diffR > 0 && m.rightSidebarAnimWidth >= m.rightSidebarTargetWidth) || (diffR < 0 && m.rightSidebarAnimWidth <= m.rightSidebarTargetWidth) {
				m.rightSidebarAnimWidth = m.rightSidebarTargetWidth
			} else {
				animSidebar = true
			}
		} else {
			m.rightSidebarAnimWidth = m.rightSidebarTargetWidth
		}

		animScroll := m.stepSmoothScroll()
		animGit := false
		if m.gitModal != nil && m.gitModal.Open {
			animGit = m.gitModal.StepAnimation()
		}

		if animSidebar || animScroll || animGit {
			return m, func() tea.Msg {
				time.Sleep(8 * time.Millisecond) // 120 FPS target
				return animTickMsg{}
			}
		}
		return m, nil

	case lspDiagnosticsMsg:
		m.syncActiveDiagnostics()
		return m, nil

	case termTickMsg:
		if m.terminal != nil && m.terminal.Open {
			return m, tickTerm()
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		_ = m.eng.Dispatch(core.Command{
			ID:   core.CmdResize,
			Args: [2]int{msg.Width, msg.Height},
		})
		return m, nil

	case tea.PasteMsg:
		// Bracketed paste event: insert atomically without auto-indent staircase
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: msg.Text})
		m.notifyLSPChange()
		m.ensureCursorVisible()
		cmd := m.onTextMutation()
		return m, cmd

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg.Key)
	}

	return m, nil
}












































type visChar struct {
	r       rune
	origCol int
}




















type menuItem struct {
	label    string
	shortcut string
	action   string
}









type docMatch struct {
	line      int
	col       int
	length    int
	startByte int
	endByte   int
}




















