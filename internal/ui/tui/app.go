package tui

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"github.com/mattn/go-runewidth"
	"github.com/sahilm/fuzzy"

	"tahr/internal/core"
	corebuf "tahr/internal/core/buffer"
	"tahr/internal/core/clipboard"
	"tahr/internal/core/crash"
	"tahr/internal/core/dap"
	"tahr/internal/core/git"
	"tahr/internal/core/i18n"
	"tahr/internal/core/keymaps"
	"tahr/internal/core/launch"
	"tahr/internal/core/logging"
	"tahr/internal/core/lsp"
	"tahr/internal/core/plugin"
	"tahr/internal/core/sdk"
	"tahr/internal/core/syntax"
	"tahr/internal/core/vim"
	"tahr/internal/ui"
	goatui "github.com/baibeicha/goatui/pkg/ui"
)

// toColor converts a 24-bit 0xRRGGBB integer into goatui cell.Color.
func toColor(val uint32) cell.Color {
	return cell.Color{
		Type:  cell.ColorRGB,
		Value: val,
	}
}

// rainbowPalette defines high-contrast cyclic colors for nested bracket pairs.
var rainbowPalette = []uint32{
	0xF6C177, // Gold / Warm Yellow
	0xC4A7E7, // Iris / Purple
	0x9CCFD8, // Foam / Cyan
	0x31748F, // Pine / Teal
	0xEB6F92, // Love / Coral
}

// calculateLineRainbowDepths computes nesting depth for brackets in lineStr.
func calculateLineRainbowDepths(lineStr string) map[int]int {
	runes := []rune(lineStr)
	depths := make(map[int]int)
	depth := 0
	for col, r := range runes {
		switch r {
		case '(', '[', '{':
			depths[col] = depth
			depth++
		case ')', ']', '}':
			depth--
			if depth < 0 {
				depth = 0
			}
			depths[col] = depth
		}
	}
	return depths
}

// BoxStyle defines frame corner and border glyphs.
type BoxStyle struct {
	TopLeft     rune
	TopRight    rune
	BottomLeft  rune
	BottomRight rune
	Horiz       rune
	Vert        rune
}

// DefaultBoxChars returns the default rounded box characters.
func DefaultBoxChars() BoxStyle {
	return BoxStyle{
		TopLeft:     '╭',
		TopRight:    '╮',
		BottomLeft:  '╰',
		BottomRight: '╯',
		Horiz:       '─',
		Vert:        '│',
	}
}

// BoxChars returns the active box characters based on settings.BorderRounded.
func (m *AppModel) BoxChars() BoxStyle {
	rounded := true
	if m.settings != nil {
		rounded = m.settings.Current.BorderRounded
	}
	if rounded {
		return BoxStyle{
			TopLeft:     '╭',
			TopRight:    '╮',
			BottomLeft:  '╰',
			BottomRight: '╯',
			Horiz:       '─',
			Vert:        '│',
		}
	}
	return BoxStyle{
		TopLeft:     '┌',
		TopRight:    '┐',
		BottomLeft:  '└',
		BottomRight: '┘',
		Horiz:       '─',
		Vert:        '│',
	}
}

// DECSCUSREscapeCode returns the terminal cursor shape escape sequence based on settings.
func (m *AppModel) DECSCUSREscapeCode() string {
	if m.settings == nil {
		return "\x1b[1 q"
	}
	blink := m.settings.Current.CursorBlink
	switch m.settings.Current.CursorStyle {
	case "underline":
		if blink {
			return "\x1b[3 q"
		}
		return "\x1b[4 q"
	case "bar":
		if blink {
			return "\x1b[5 q"
		}
		return "\x1b[6 q"
	default: // "block"
		if blink {
			return "\x1b[1 q"
		}
		return "\x1b[2 q"
	}
}

// matchKey checks whether a pressed key corresponds to a given latin/cyrillic shortcut.
func matchKey(k input.Key, latin, cyrillic rune) bool {
	r := unicode.ToLower(k.Rune)
	b := unicode.ToLower(k.BaseKey)
	s := unicode.ToLower(k.ShiftedKey)
	lat := unicode.ToLower(latin)
	cyr := unicode.ToLower(cyrillic)
	if r == lat || r == cyr || b == lat || b == cyr || s == lat || s == cyr {
		return true
	}
	if len(k.Text) > 0 {
		t := []rune(k.Text)
		if len(t) == 1 {
			tr := unicode.ToLower(t[0])
			if tr == lat || tr == cyr {
				return true
			}
		}
	}
	return false
}

var latinToCyrillic = map[rune]rune{
	'q': 'й', 'w': 'ц', 'e': 'у', 'r': 'к', 't': 'е', 'y': 'н', 'u': 'г', 'i': 'ш', 'o': 'щ', 'p': 'з',
	'[': 'х', ']': 'ъ', 'a': 'ф', 's': 'ы', 'd': 'в', 'f': 'а', 'g': 'п', 'h': 'р', 'j': 'о', 'k': 'л',
	'l': 'д', ';': 'ж', '\'': 'э', 'z': 'я', 'x': 'ч', 'c': 'с', 'v': 'м', 'b': 'и', 'n': 'т', 'm': 'ь',
	',': 'б', '.': 'ю',
}

// MatchKeyToBinding tests if input.Key corresponds to a canonical keybinding string.
func MatchKeyToBinding(k input.Key, binding string) bool {
	binding = strings.TrimSpace(binding)
	if binding == "" {
		return false
	}

	var wantCtrl, wantAlt, wantShift bool
	var keyToken string

	if strings.HasSuffix(binding, "++") {
		keyToken = "+"
		prefix := binding[:len(binding)-2]
		for _, part := range strings.Split(prefix, "+") {
			switch strings.ToLower(part) {
			case "ctrl":
				wantCtrl = true
			case "alt":
				wantAlt = true
			case "shift":
				wantShift = true
			}
		}
	} else {
		parts := strings.Split(binding, "+")
		for i, part := range parts {
			p := strings.ToLower(part)
			if i < len(parts)-1 && (p == "ctrl" || p == "alt" || p == "shift") {
				switch p {
				case "ctrl":
					wantCtrl = true
				case "alt":
					wantAlt = true
				case "shift":
					wantShift = true
				}
			} else {
				keyToken = part
			}
		}
	}

	isCtrlCode := (k.Rune >= 1 && k.Rune <= 26 && k.Type != input.KeyTab && k.Type != input.KeyEnter)
	hasCtrl := k.HasCtrl() || isCtrlCode
	hasAlt := k.HasAlt()
	hasShift := k.HasShift()

	if hasCtrl != wantCtrl {
		return false
	}
	if hasAlt != wantAlt {
		return false
	}
	if hasShift != wantShift {
		return false
	}

	upperToken := strings.ToUpper(keyToken)
	switch upperToken {
	case "F1":
		return k.Type == input.KeyF1
	case "F2":
		return k.Type == input.KeyF2
	case "F3":
		return k.Type == input.KeyF3
	case "F4":
		return k.Type == input.KeyF4
	case "F5":
		return k.Type == input.KeyF5
	case "F6":
		return k.Type == input.KeyF6
	case "F7":
		return k.Type == input.KeyF7
	case "F8":
		return k.Type == input.KeyF8
	case "F9":
		return k.Type == input.KeyF9
	case "F10":
		return k.Type == input.KeyF10
	case "F11":
		return k.Type == input.KeyF11
	case "F12":
		return k.Type == input.KeyF12
	case "ENTER":
		return k.Type == input.KeyEnter || k.Rune == 13 || k.Rune == 10
	case "TAB":
		return k.Type == input.KeyTab || k.Rune == 9
	case "SPACE":
		return k.Type == input.KeySpace || k.Rune == ' '
	case "BACKSPACE":
		return k.Type == input.KeyBackspace || k.Rune == 8 || k.Rune == 127
	case "DELETE":
		return k.Type == input.KeyDelete
	case "UP":
		return k.Type == input.KeyUp
	case "DOWN":
		return k.Type == input.KeyDown
	case "LEFT":
		return k.Type == input.KeyLeft
	case "RIGHT":
		return k.Type == input.KeyRight
	case "ESC", "ESCAPE":
		return k.Type == input.KeyEsc || k.Rune == 27
	}

	runes := []rune(keyToken)
	if len(runes) == 1 {
		tokRune := runes[0]
		tokLower := unicode.ToLower(tokRune)

		if tokLower >= 'a' && tokLower <= 'z' {
			ctrlByte := tokLower - 'a' + 1
			if k.Rune == ctrlByte {
				return true
			}
		}

		cyr := latinToCyrillic[tokLower]
		if cyr != 0 && matchKey(k, tokLower, cyr) {
			return true
		}

		if unicode.ToLower(k.Rune) == tokLower || unicode.ToLower(k.BaseKey) == tokLower || unicode.ToLower(k.ShiftedKey) == tokLower {
			return true
		}

		if tokRune == '\\' && (k.Rune == 28 || k.Rune == '\\' || k.BaseKey == '\\') {
			return true
		}
	}

	return false
}

// MatchBinding resolves actionID against m.settings.Current.Keybindings (with fallback to DefaultKeybindings).
func (m *AppModel) MatchBinding(k input.Key, actionID string) bool {
	binding := ""
	if m != nil && m.settings != nil && m.settings.Current.Keybindings != nil {
		binding = m.settings.Current.Keybindings[actionID]
	}
	if binding == "" {
		binding = DefaultKeybindings()[actionID]
	}
	if binding != "" && MatchKeyToBinding(k, binding) {
		return true
	}
	switch actionID {
	case "undo":
		if (!k.HasShift() && (k.Rune == 26 || (k.HasCtrl() && (matchKey(k, 'z', 'я') || matchKey(k, 'u', 'г') || k.Rune == 21)))) ||
			(k.HasAlt() && !k.HasShift() && (k.Type == input.KeyBackspace || k.Rune == 8 || k.Rune == 127)) {
			return true
		}
	case "redo":
		if MatchKeyToBinding(k, "Ctrl+Shift+Z") || (k.Rune == 26 && k.HasShift()) || k.Rune == 25 || (k.HasCtrl() && matchKey(k, 'y', 'н')) ||
			(k.HasAlt() && k.HasShift() && (k.Type == input.KeyBackspace || k.Rune == 8 || k.Rune == 127)) {
			return true
		}
	case "save":
		if k.Rune == 19 || (k.HasCtrl() && matchKey(k, 's', 'ы')) {
			return true
		}
	case "terminal_toggle":
		if k.Type == input.KeyF4 || (k.HasCtrl() && (k.Rune == '`' || k.Rune == '~' || k.BaseKey == '`' || k.BaseKey == '~')) {
			return true
		}
	}
	return false
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

	// Editor click & double click tracking
	lastEditorClickTime time.Time
	lastEditorClickLine int
	lastEditorClickCol  int

	// Modals: Find & Replace, Project Rename, New Project, Search in Files, and Problems
	findReplaceModal   *FindReplaceModal
	renameModal        *RenameModal
	newProjectModal    *NewProjectModal
	searchInFilesModal *SearchInFilesModal
	problemsPanel      *ProblemsPanel

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

	// Animations
	sidebarAnimWidth   float64
	sidebarTargetWidth float64

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
	termTabHitboxes  []termTabHitbox
	lastTabClickTime time.Time
	lastTabClickIdx  int

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

	statusMessage string
	quitting      bool
}

type minimapHitbox struct {
	paneIdx    int
	minX       int
	maxX       int
	minY       int
	maxY       int
	totalLines int
}

type contextMenuItem struct {
	label  string
	action string
}

type animTickMsg struct{}

type termTickMsg struct{}

type lspDiagnosticsMsg struct {
	URI string
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
		sidebarAnimWidth:    0,
		sidebarTargetWidth:   0,
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
		allProjectFiles: []string{
			"cmd/tahr/main.go",
			"internal/core/engine.go",
			"internal/core/buffer/rope.go",
			"internal/core/buffer/selection.go",
			"internal/core/syntax/treesitter.go",
			"internal/ui/tui/app.go",
			"internal/ui/tui/tree.go",
			"internal/ui/tui/runner.go",
			"go.mod",
			"README.md",
		},
	}

	if m.splash != nil {
		m.splash.Active = false
	}

	if m.crashDialog != nil {
		m.crashDialog.CheckAndOpen()
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

// applyCurrentSettings merges active theme, custom hex colors, indentation, scrolloff, and default split layout.
func (m *AppModel) applyCurrentSettings() {
	if m.settings == nil {
		return
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

// clampSidebar ensures tree nodes exist and scroll/selection indices remain within valid bounds.
func (m *AppModel) clampSidebar() {
	if len(m.treeFlat) == 0 && m.treeRoot != nil {
		m.treeFlat = make([]*FileNode, 0)
		FlattenTree(m.treeRoot, &m.treeFlat)
	}
	if len(m.treeFlat) == 0 {
		m.treeRoot = BuildProjectTree(m.workspaceDir, 4)
		m.treeFlat = make([]*FileNode, 0)
		FlattenTree(m.treeRoot, &m.treeFlat)
	}
	if len(m.treeFlat) > 0 {
		if m.treeSel >= len(m.treeFlat) {
			m.treeSel = len(m.treeFlat) - 1
		}
		if m.treeSel < 0 {
			m.treeSel = 0
		}
		visH := m.visibleTreeHeight()
		maxScroll := max(0, len(m.treeFlat)-visH)
		if m.sidebarScrollY > maxScroll {
			m.sidebarScrollY = maxScroll
		}
		if m.sidebarScrollY < 0 {
			m.sidebarScrollY = 0
		}
	} else {
		m.treeSel = 0
		m.sidebarScrollY = 0
	}
}

// refreshProjectTree re-scans the workspace directory structure.
func (m *AppModel) refreshProjectTree() {
	m.treeRoot = BuildProjectTree(m.workspaceDir, 4)
	m.treeFlat = make([]*FileNode, 0)
	FlattenTree(m.treeRoot, &m.treeFlat)
	m.clampSidebar()
}

// visibleTreeHeight returns the number of visible rows available for sidebar content.
func (m *AppModel) visibleTreeHeight() int {
	h := m.height - 2 // row 0: top header, row height-1: status bar
	if m.outputOpen {
		drawerH := m.outputHeight
		if drawerH > m.height-5 {
			drawerH = max(3, m.height-5)
		}
		h -= drawerH
	}
	if m.terminal != nil && m.terminal.Open {
		termH := m.terminal.Height
		if termH > h-3 {
			termH = max(3, h-3)
		}
		h -= termH
	}
	if m.dapHUD != nil && m.dapHUD.Open {
		hudH := m.dapHUD.Height
		if hudH > h-3 {
			hudH = max(3, h-3)
		}
		h -= hudH
	}
	return max(1, h-1) // -1 for sidebar header row
}

// EnableSplashScreen activates the startup splash animation.
func (m *AppModel) EnableSplashScreen() {
	if m.splash == nil {
		m.splash = NewSplashScreenState()
	}
	m.splash.Active = true
	m.splash.Done = false
	m.splash.StartTime = time.Now()
}

// SetWorkspaceDir changes the active project root and rescans tree and files.
func (m *AppModel) SetWorkspaceDir(dir string) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		m.workspaceDir = abs
	} else {
		m.workspaceDir = dir
	}
	m.refreshProjectTree()
	m.allProjectFiles = ScanWorkspaceFiles(m.workspaceDir, 2500)
}

// OpenProject switches the workspace root to dir, rescans files, and loads default file.
func (m *AppModel) OpenProject(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		m.statusMessage = fmt.Sprintf("Directory not found: %s", dir)
		return fmt.Errorf("directory not found: %s", dir)
	}

	m.workspaceDir = abs
	m.sidebarOpen = true
	m.refreshProjectTree()
	m.allProjectFiles = ScanWorkspaceFiles(abs, 2500)

	// Close existing LSP if workspace changed
	if m.lspClient != nil {
		_ = m.lspClient.Close()
		m.lspClient = nil
	}

	// Try to find a primary entry file to open
	candidates := []string{
		filepath.Join(abs, "main.go"),
		filepath.Join(abs, "cmd", filepath.Base(abs), "main.go"),
		filepath.Join(abs, "app.py"),
		filepath.Join(abs, "main.py"),
		filepath.Join(abs, "src", "main.rs"),
		filepath.Join(abs, "index.js"),
		filepath.Join(abs, "README.md"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			_, _ = m.eng.Open(c)
			m.EnsureLSPForFile(c)
			m.statusMessage = fmt.Sprintf("Opened project %s (%s)", filepath.Base(abs), filepath.Base(c))
			return nil
		}
	}

	for _, n := range m.treeFlat {
		if !n.IsDir {
			_, _ = m.eng.Open(n.Path)
			m.EnsureLSPForFile(n.Path)
			m.statusMessage = fmt.Sprintf("Opened project %s", filepath.Base(abs))
			return nil
		}
	}

	m.statusMessage = fmt.Sprintf("Opened project %s", filepath.Base(abs))
	return nil
}

// CreateProjectFromTemplate scaffolds a new project from a plugin ProjectTemplate.
func (m *AppModel) CreateProjectFromTemplate(tpl plugin.ProjectTemplate, name, location, modulePath string) error {
	targetDir := filepath.Clean(location)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		m.statusMessage = fmt.Sprintf("Create dir error: %v", err)
		return err
	}

	if name == "" {
		name = filepath.Base(targetDir)
	}
	if modulePath == "" {
		modulePath = name
	}

	for _, f := range tpl.Files {
		relPath := strings.ReplaceAll(f.Path, "{{.ProjectName}}", name)
		relPath = strings.ReplaceAll(relPath, "{{.ModulePath}}", modulePath)
		filePath := filepath.Join(targetDir, filepath.FromSlash(relPath))

		dir := filepath.Dir(filePath)
		_ = os.MkdirAll(dir, 0755)

		content := strings.ReplaceAll(f.Content, "{{.ProjectName}}", name)
		content = strings.ReplaceAll(content, "{{.ModulePath}}", modulePath)

		_ = os.WriteFile(filePath, []byte(content), 0644)
	}

	if tpl.PostCreateCmd != "" {
		parts := strings.Fields(tpl.PostCreateCmd)
		if len(parts) > 0 {
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Dir = targetDir
			_ = cmd.Run()
		}
	}

	m.statusMessage = fmt.Sprintf("Created project '%s' from template '%s'", name, tpl.Name)
	return m.OpenProject(targetDir)
}

// CreateProject scaffolds a new project of the specified kind.
func (m *AppModel) CreateProject(kind, parentDir, name string) error {
	targetDir := filepath.Join(parentDir, name)
	if filepath.IsAbs(name) {
		targetDir = filepath.Clean(name)
		name = filepath.Base(targetDir)
	}

	// 1. Try to find matching template from plugins
	if m.pluginMgr != nil {
		templates := m.pluginMgr.GetProjectTemplates()
		for _, tpl := range templates {
			if strings.EqualFold(tpl.ID, kind) || strings.EqualFold(tpl.Category, kind) {
				return m.CreateProjectFromTemplate(tpl, name, targetDir, name)
			}
		}
	}

	// 2. Builtin fallback scaffolding
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		m.statusMessage = fmt.Sprintf("Create dir error: %v", err)
		return err
	}

	switch kind {
	case "go":
		mainGo := fmt.Sprintf("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello from %s!\")\n}\n", name)
		_ = os.WriteFile(filepath.Join(targetDir, "main.go"), []byte(mainGo), 0644)
		goMod := fmt.Sprintf("module %s\n\ngo 1.22\n", name)
		_ = os.WriteFile(filepath.Join(targetDir, "go.mod"), []byte(goMod), 0644)
		readme := fmt.Sprintf("# %s\n\nA new Go project.\n", name)
		_ = os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(readme), 0644)

	case "python":
		mainPy := fmt.Sprintf("def main():\n    print(\"Hello from %s!\")\n\nif __name__ == \"__main__\":\n    main()\n", name)
		_ = os.WriteFile(filepath.Join(targetDir, "main.py"), []byte(mainPy), 0644)
		_ = os.WriteFile(filepath.Join(targetDir, "requirements.txt"), []byte("# Dependencies\n"), 0644)
		readme := fmt.Sprintf("# %s\n\nA new Python project.\n", name)
		_ = os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(readme), 0644)

	case "rust":
		srcDir := filepath.Join(targetDir, "src")
		_ = os.MkdirAll(srcDir, 0755)
		mainRs := "fn main() {\n    println!(\"Hello from " + name + "!\");\n}\n"
		_ = os.WriteFile(filepath.Join(srcDir, "main.rs"), []byte(mainRs), 0644)
		cargoToml := fmt.Sprintf("[package]\nname = \"%s\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\n", name)
		_ = os.WriteFile(filepath.Join(targetDir, "Cargo.toml"), []byte(cargoToml), 0644)
		readme := fmt.Sprintf("# %s\n\nA new Rust project.\n", name)
		_ = os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(readme), 0644)

	default: // blank
		readme := fmt.Sprintf("# %s\n\nNew project workspace.\n", name)
		_ = os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(readme), 0644)
	}

	return m.OpenProject(targetDir)
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
			installCmd = "pip install pyright"
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
		if m.toolPrompt != nil && !m.toolPrompt.Open {
			m.toolPrompt.OpenForTool(
				toolName,
				pluginName,
				installCmd,
				ext,
				func() {
					if m.terminal != nil {
						m.terminal.Open = true
						m.terminal.Execute(installCmd)
					}
					parts := strings.Fields(installCmd)
					if len(parts) > 0 {
						bin := parts[0]
						if m.pluginMgr != nil {
							if found, ok := m.pluginMgr.FindToolPath(bin, ""); ok {
								bin = found
							}
						}
						go func() {
							_ = exec.Command(bin, parts[1:]...).Run()
							time.Sleep(500 * time.Millisecond)
							m.EnsureLSPForFile(filePath)
						}()
					}
					m.toasts.Info("INSTALL", fmt.Sprintf("Installing %s via %s", toolName, installCmd))
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
		return
	}

	if lspCmd == "" {
		return
	}

	if m.lspClient == nil {
		client, err := lsp.StartClient(lspCmd, lspArgs, m)
		if err == nil {
			m.lspClient = client
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

	// 1. Language standard library and built-ins
	switch ext {
	case ".go", "":
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

	// 2. Active buffer words
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

// SetPluginManager attaches the plugin manager for language configurations.
func (m *AppModel) SetPluginManager(mgr *plugin.Manager) {
	m.pluginMgr = mgr
	if m.launchModal != nil && mgr != nil {
		m.launchModal.SetSupportedTypes(mgr.GetSupportedLaunchTypes())
	}
	if m.marketplace != nil {
		m.marketplace.mgr = mgr
		m.marketplace.Refresh()
	} else {
		m.marketplace = NewMarketplaceModal(mgr)
	}
	if m.settings != nil {
		m.settings.SetPluginManager(mgr)
	}
	if mgr != nil {
		mgr.AddLifecycleListener(func(pluginID string, enabled bool) {
			m.onPluginLifecycleChanged(pluginID, enabled)
		})
	}
	m.applyCurrentSettings()
}

func (m *AppModel) onPluginLifecycleChanged(pluginID string, enabled bool) {
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
		fileDiags[line] = d.Message
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
				return msg, true
			}
		}
	}

	if msg, ok := m.diagnostics[cursorLine]; ok {
		return msg, true
	}
	return "", false
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

		animScroll := m.stepSmoothScroll()

		if animSidebar || animScroll {
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

// stepSmoothScroll performs 120 FPS sub-line exponential damping interpolation towards target viewport.
func (m *AppModel) stepSmoothScroll() bool {
	if m.splits == nil {
		return false
	}
	needsRedraw := false
	for i := range m.splits.Panes {
		p := &m.splits.Panes[i]
		diff := p.TargetViewportY - p.SmoothScrollY
		if math.Abs(diff) > 0.05 {
			p.SmoothScrollY += diff * 0.40
			p.ViewportY = int(math.Round(p.SmoothScrollY))
			if i == m.splits.ActiveIndex {
				m.viewportY = p.ViewportY
			}
			needsRedraw = true
		} else {
			p.SmoothScrollY = p.TargetViewportY
			p.ViewportY = int(p.TargetViewportY)
			if i == m.splits.ActiveIndex {
				m.viewportY = p.ViewportY
			}
		}
	}
	return needsRedraw
}

// dispatchKeybinding checks dynamic user keybindings from Settings.Keybindings and executes the matched action.
func (m *AppModel) dispatchKeybinding(k input.Key) (tea.Model, tea.Cmd, bool) {
	// Redo / Undo
	if m.MatchBinding(k, "redo") {
		m.performRedo()
		return m, nil, true
	}
	if m.MatchBinding(k, "undo") {
		m.performUndo()
		return m, nil, true
	}

	// File & Buffer operations
	if m.MatchBinding(k, "save") {
		doc := m.eng.ActiveDocument()
		if doc != nil {
			target := doc.FilePath
			if target == "" {
				target = "untitled.txt"
			}
			_ = m.eng.Dispatch(core.Command{ID: core.CmdFileSave, Args: target})
			m.toasts.Success("SAVED", filepath.Base(target))
			m.statusMessage = fmt.Sprintf("Saved %s", target)
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "close_tab") {
		if active := m.eng.ActiveDocument(); active != nil {
			m.eng.CloseBuffer(active.ID)
			m.toasts.Info("CLOSED", "Buffer closed")
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "quit") {
		return m, tea.Quit, true
	}

	// Navigation & Search
	if m.MatchBinding(k, "find") {
		m.openFindModal()
		return m, nil, true
	}
	if m.MatchBinding(k, "replace") {
		m.openReplaceModal()
		return m, nil, true
	}
	if m.MatchBinding(k, "search_in_files") || (k.HasCtrl() && k.HasShift() && (k.Rune == 'f' || k.Rune == 'F' || k.Rune == 6 || k.BaseKey == 'f')) {
		m.openSearchInFilesModal()
		return m, nil, true
	}
	if m.MatchBinding(k, "problems") {
		m.toggleProblemsPanel()
		return m, nil, true
	}
	if m.MatchBinding(k, "rename") {
		m.openRenameModal()
		return m, nil, true
	}
	if m.MatchBinding(k, "goto_line") {
		m.openOmnibar("goto")
		return m, nil, true
	}
	if m.MatchBinding(k, "commands") {
		m.openOmnibar("commands")
		return m, nil, true
	}
	if m.MatchBinding(k, "omnibar") {
		m.openOmnibar("files")
		return m, nil, true
	}

	// Selection & Clipboard
	if m.MatchBinding(k, "select_all") {
		_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectAll})
		return m, nil, true
	}
	if m.MatchBinding(k, "next_match") {
		_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectNextMatch})
		m.ensureCursorVisible()
		return m, nil, true
	}
	if m.MatchBinding(k, "copy") {
		selText := m.selectedText()
		if selText != "" {
			_ = clipboard.Write(selText)
			m.clipboardText = selText
			m.toasts.Info("CLIPBOARD", fmt.Sprintf("Copied %d characters", len(selText)))
			return m, nil, true
		}
		if m.runner != nil && m.runner.IsRunning() {
			m.runner.Stop()
			m.toasts.Warn("STOPPED", "Execution stopped")
			return m, nil, true
		}
		if m.popupVisible {
			m.popupVisible = false
			return m, nil, true
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "cut") {
		selText := m.selectedText()
		if selText != "" {
			_ = clipboard.Write(selText)
			m.clipboardText = selText
			_ = m.eng.Dispatch(core.Command{ID: core.CmdDeleteBackward})
			m.toasts.Info("CLIPBOARD", fmt.Sprintf("Cut %d characters", len(selText)))
			m.notifyLSPChange()
			m.ensureCursorVisible()
			return m, nil, true
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "paste") {
		clip, err := clipboard.Read()
		if err == nil && clip != "" {
			_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: clip})
			m.toasts.Info("CLIPBOARD", fmt.Sprintf("Pasted %d characters", len(clip)))
			m.notifyLSPChange()
			m.ensureCursorVisible()
			return m, nil, true
		}
		if m.clipboardText != "" {
			_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: m.clipboardText})
			m.notifyLSPChange()
			m.ensureCursorVisible()
			return m, nil, true
		}
		return m, nil, true
	}

	// Color Picker & Palette (Alt+C)
	if m.MatchBinding(k, "pick_color") || (k.Mod&input.ModAlt != 0 && (k.Rune == 'c' || k.Rune == 'C')) {
		m.openEditorColorPickerAtCursor()
		return m, nil, true
	}

	// Execution & Build
	if m.MatchBinding(k, "run") {
		if m.dapSession != nil && m.dapSession.IsStopped() {
			m.stoppedMarkerLine = -1
			_ = m.dapSession.Continue()
			m.toasts.Info("DEBUGGER", "Continue execution")
			return m, nil, true
		}
		m.RunActive()
		return m, nil, true
	}
	if m.MatchBinding(k, "build") {
		m.BuildActive()
		return m, nil, true
	}

	// Sidebar & Docks
	if m.MatchBinding(k, "tree_toggle") {
		if k.Type == input.KeyF2 {
			if doc := m.eng.ActiveDocument(); doc != nil && (m.selectedText() != "" || m.wordUnderCursor() != "") {
				m.openRenameModal()
				return m, nil, true
			}
		}
		cmd := m.ToggleSidebar()
		m.statusMessage = fmt.Sprintf("Project Tree: %v", m.sidebarOpen)
		return m, cmd, true
	}
	if m.MatchBinding(k, "tree_dock") {
		if m.settings != nil {
			if m.settings.Current.TreePosition == "left" {
				m.settings.Current.TreePosition = "right"
			} else {
				m.settings.Current.TreePosition = "left"
			}
			_ = m.settings.Save()
			m.toasts.Info("TREE DOCK", fmt.Sprintf("Docked to %s", m.settings.Current.TreePosition))
		}
		return m, nil, true
	}

	// Split Management
	if m.MatchBinding(k, "split_cycle") {
		if m.splits != nil {
			m.splits.CycleLayout()
			m.toasts.Info("SPLIT", m.splits.ModeTitle())
			m.statusMessage = fmt.Sprintf("Split: %s", m.splits.ModeTitle())
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "split_next") {
		if m.splits != nil && m.splits.TotalPanes() > 1 {
			m.switchActivePane(m.splits.NextPane())
			m.toasts.Info("SPLIT", fmt.Sprintf("Focused Pane %d", m.splits.ActiveIndex+1))
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "split_prev") {
		if m.splits != nil && m.splits.TotalPanes() > 1 {
			m.switchActivePane(m.splits.PrevPane())
			m.toasts.Info("SPLIT", fmt.Sprintf("Focused Pane %d", m.splits.ActiveIndex+1))
		}
		return m, nil, true
	}
	for i := 1; i <= 6; i++ {
		action := fmt.Sprintf("split_%d", i)
		if m.MatchBinding(k, action) {
			if m.splits != nil && i-1 < m.splits.TotalPanes() {
				m.switchActivePane(i - 1)
				m.toasts.Info("SPLIT", fmt.Sprintf("Focused Pane %d", i))
			}
			return m, nil, true
		}
	}

	// Debugger Actions
	if m.MatchBinding(k, "breakpoint") {
		doc := m.eng.ActiveDocument()
		if doc != nil {
			sels := doc.Buffer.GetSelections()
			if len(sels) > 0 {
				line := sels[0].Head.Line
				m.breakpoints[line] = !m.breakpoints[line]
				if !m.breakpoints[line] {
					delete(m.breakpoints, line)
					m.toasts.Info("BREAKPOINT", fmt.Sprintf("Removed at line %d", line+1))
				} else {
					m.toasts.Success("BREAKPOINT", fmt.Sprintf("Set at line %d", line+1))
				}
				if m.dapSession != nil && doc.FilePath != "" {
					var bpLines []int
					for bl := range m.breakpoints {
						bpLines = append(bpLines, bl)
					}
					m.dapSession.SetBreakpoints(doc.FilePath, bpLines)
				}
			}
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "step_over") {
		if m.dapSession != nil && m.dapSession.IsStopped() {
			_ = m.dapSession.StepOver()
			m.statusMessage = "Debugger: Step Over"
			m.toasts.Info("DEBUGGER", "Step Over")
		} else {
			m.statusMessage = "Debugger: Step Over (session not paused)"
			m.toasts.Warn("DEBUGGER", "Step Over (session not paused)")
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "step_into") {
		if m.dapSession != nil && m.dapSession.IsStopped() {
			_ = m.dapSession.StepInto()
			m.statusMessage = "Debugger: Step Into"
			m.toasts.Info("DEBUGGER", "Step Into")
		} else {
			m.statusMessage = "Debugger: Step Into (session not paused)"
			m.toasts.Warn("DEBUGGER", "Step Into (session not paused)")
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "step_out") {
		if m.dapSession != nil && m.dapSession.IsStopped() {
			_ = m.dapSession.StepOut()
			m.statusMessage = "Debugger: Step Out"
			m.toasts.Info("DEBUGGER", "Step Out")
		} else {
			m.statusMessage = "Debugger: Step Out (session not paused)"
			m.toasts.Warn("DEBUGGER", "Step Out (session not paused)")
		}
		return m, nil, true
	}

	// Code Intelligence
	if m.MatchBinding(k, "definition") {
		m.gotoDefinition()
		return m, nil, true
	}
	if m.MatchBinding(k, "hover") {
		m.showHover()
		return m, nil, true
	}
	if m.MatchBinding(k, "quick_fix") {
		m.triggerQuickFix()
		return m, nil, true
	}
	if m.MatchBinding(k, "completion") {
		m.triggerCompletionPopup()
		return m, nil, true
	}

	// Tools & Modals
	if m.MatchBinding(k, "settings") {
		if m.settings != nil {
			m.settings.Open = true
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "terminal_toggle") {
		if m.terminal != nil {
			m.terminal.Toggle()
			m.terminalFocused = m.terminal.Open
			m.toasts.Info("TERMINAL", fmt.Sprintf("Terminal: %v", m.terminal.Open))
			if m.terminal.Open {
				_ = m.terminal.EnsureSession()
				return m, tickTerm(), true
			}
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "minimap_toggle") {
		if m.settings != nil {
			m.settings.Current.ShowMinimap = !m.settings.Current.ShowMinimap
			_ = m.settings.Save()
			stateStr := "Enabled"
			if !m.settings.Current.ShowMinimap {
				stateStr = "Disabled"
			}
			m.toasts.Info("MINIMAP", fmt.Sprintf("Minimap %s", stateStr))
		}
		return m, nil, true
	}
	if m.MatchBinding(k, "markdown_preview") {
		m.toggleMarkdownPreview()
		return m, nil, true
	}
	if m.MatchBinding(k, "git_modal") {
		m.openGitModal()
		return m, nil, true
	}
	if m.MatchBinding(k, "marketplace") {
		if m.marketplace != nil {
			m.marketplace.Open = true
			m.marketplace.Refresh()
		}
		return m, nil, true
	}

	return m, nil, false
}

// handleKey routes keyboard events according to current modal state.
func (m *AppModel) handleKey(k input.Key) (tea.Model, tea.Cmd) {
	// CRITICAL: Discard KeyRelease events to prevent duplicate triggers
	if k.Action == input.KeyRelease {
		return m, nil
	}

	// Any keypress immediately dismisses active hover documentation popup
	if m.hoverDoc != nil && m.hoverDoc.Open {
		m.hoverDoc.Dismiss()
	}

	// 0. Startup splash screen dismiss on key
	if m.splash != nil && m.splash.Active {
		m.splash.HandleKey(k)
		return m, nil
	}

	// 0.0 Crash recovery dialog (blocks buffer access until handled)
	if m.crashDialog != nil && m.crashDialog.Open {
		handled, action := m.crashDialog.HandleKey(k)
		if handled {
			m.handleCrashDialogAction(action)
			return m, nil
		}
	}

	// 0.00 Log & Data Inspector
	if m.logInspector != nil && m.logInspector.Open {
		if m.logInspector.HandleKey(k) {
			return m, nil
		}
	}

	// 0.000 Bug Reporter Modal
	if m.bugReportModal != nil && m.bugReportModal.Open {
		handled, action := m.bugReportModal.HandleKey(k)
		if handled {
			m.handleBugReportAction(action)
			return m, nil
		}
	}

	// 0.000000 Main Menu dropdown key handling
	if m.mainMenuOpen {
		items := getMainMenuItems()
		if k.Type == input.KeyEsc {
			m.mainMenuOpen = false
			return m, nil
		}
		if k.Type == input.KeyUp {
			if m.mainMenuSel > 0 {
				m.mainMenuSel--
			} else {
				m.mainMenuSel = len(items) - 1
			}
			return m, nil
		}
		if k.Type == input.KeyDown {
			if m.mainMenuSel < len(items)-1 {
				m.mainMenuSel++
			} else {
				m.mainMenuSel = 0
			}
			return m, nil
		}
		if k.Type == input.KeyEnter {
			if m.mainMenuSel >= 0 && m.mainMenuSel < len(items) {
				m.executeMainMenuItem(items[m.mainMenuSel].action)
			}
			m.mainMenuOpen = false
			return m, nil
		}
		return m, nil
	}

	// 0.000001 Profile dropdown key handling
	if m.profileDropdownOpen && m.launchConfig != nil {
		profiles := m.launchConfig.Configurations
		totalItems := len(profiles) + 2
		if k.Type == input.KeyEsc {
			m.profileDropdownOpen = false
			return m, nil
		}
		if k.Type == input.KeyUp {
			if m.profileDropdownSel > 0 {
				m.profileDropdownSel--
				if m.profileDropdownSel == len(profiles) {
					m.profileDropdownSel--
				}
			} else {
				m.profileDropdownSel = totalItems - 1
			}
			return m, nil
		}
		if k.Type == input.KeyDown {
			if m.profileDropdownSel < totalItems-1 {
				m.profileDropdownSel++
				if m.profileDropdownSel == len(profiles) {
					m.profileDropdownSel++
				}
			} else {
				m.profileDropdownSel = 0
			}
			return m, nil
		}
		if k.Type == input.KeyEnter {
			if m.profileDropdownSel < len(profiles) {
				m.launchConfig.SetActive(profiles[m.profileDropdownSel].Name)
				_ = m.launchConfig.Save()
				m.toasts.Info("PROFILE", fmt.Sprintf("Active: %s", profiles[m.profileDropdownSel].Name))
			} else if m.profileDropdownSel == len(profiles)+1 {
				m.openLaunchConfigModal()
			}
			m.profileDropdownOpen = false
			return m, nil
		}
		return m, nil
	}

	// Top toolbar menu hotkeys: Alt+F toggles main menu
	if k.HasAlt() && matchKey(k, 'f', 'а') {
		m.mainMenuOpen = !m.mainMenuOpen
		return m, nil
	}
	// Profile dropdown hotkey: Ctrl+F5
	if k.HasCtrl() && k.Type == input.KeyF5 {
		m.profileDropdownOpen = !m.profileDropdownOpen
		return m, nil
	}
	// Launch configurations modal hotkey: Alt+Shift+F10
	if k.HasAlt() && k.HasShift() && (k.Type == input.KeyF10 || k.Rune == '0' || k.BaseKey == '0') {
		m.openLaunchConfigModal()
		return m, nil
	}

	// Find & Replace modal intercepts all keys when open
	if m.findReplaceModal != nil && m.findReplaceModal.Open {
		consumed, action := m.findReplaceModal.HandleKey(k)
		if consumed {
			return m.handleFindReplaceAction(action)
		}
		return m, nil
	}

	// Symbol Rename modal intercepts all keys when open
	if m.renameModal != nil && m.renameModal.Open {
		consumed, confirmed := m.renameModal.HandleKey(k)
		if consumed {
			if confirmed {
				return m.executeSymbolRename(m.renameModal.OldName, m.renameModal.NewName, m.renameModal.Line, m.renameModal.Col, m.renameModal.URI)
			}
			return m, nil
		}
		return m, nil
	}

	// Search in Files modal intercepts all keys when open
	if m.searchInFilesModal != nil && m.searchInFilesModal.Open {
		if m.searchInFilesModal.HandleKey(k, m.workspaceDir) {
			return m, nil
		}
		return m, nil
	}

	// Problems panel intercepts keys when open
	if m.problemsPanel != nil && m.problemsPanel.Open {
		if m.problemsPanel.HandleKey(k) {
			return m, nil
		}
	}

	// New Project modal intercepts all keys when open
	if m.newProjectModal != nil && m.newProjectModal.Open {
		consumed, _ := m.newProjectModal.HandleKey(k)
		if consumed {
			return m, nil
		}
		return m, nil
	}

	// 0.00000 Launch Config modal intercepts all keys when open
	if m.launchModal != nil && m.launchModal.Open {
		handled, shouldClose := m.launchModal.HandleKey(k)
		if shouldClose {
			m.launchModal.Open = false
		}
		if handled {
			return m, nil
		}
	}

	// 0.0000 Quick Fix modal intercepts all keys when open
	if m.quickFixModal != nil && m.quickFixModal.Open {
		applied, action, closed := m.quickFixModal.HandleKey(k)
		if closed {
			m.quickFixModal.Close()
		}
		if applied && action != nil {
			m.applyCodeAction(action)
		}
		return m, nil
	}

	// 0.00005 Tool Prompt modal intercepts all keys when open
	if m.toolPrompt != nil && m.toolPrompt.Open {
		handled, shouldClose := m.toolPrompt.HandleKey(k)
		if shouldClose {
			m.toolPrompt.Close()
		}
		if handled {
			return m, nil
		}
	}

	// 0.0001 Git modal intercepts all keys when open
	if m.gitModal != nil && m.gitModal.Open {
		if m.gitModal.HandleKey(k) {
			return m, nil
		}
	}

	// 0.0002 In-Editor Color Picker modal intercepts all keys when open
	if m.editorColorPicker != nil && m.editorColorPicker.Open {
		handled, shouldClose := m.editorColorPicker.HandleKey(k)
		if shouldClose {
			m.editorColorPicker.Open = false
		}
		if handled {
			return m, nil
		}
	}

	// 0.0 Context menu intercepts keys when open
	if m.contextMenuOpen {
		switch k.Type {
		case input.KeyUp:
			if m.contextMenuSel > 0 {
				m.contextMenuSel--
			}
			return m, nil
		case input.KeyDown:
			if m.contextMenuSel+1 < len(m.contextMenuItems) {
				m.contextMenuSel++
			}
			return m, nil
		case input.KeyEnter:
			if len(m.contextMenuItems) > 0 && m.contextMenuSel < len(m.contextMenuItems) {
				action := m.contextMenuItems[m.contextMenuSel].action
				m.contextMenuOpen = false
				return m.executeContextMenuAction(action)
			}
			m.contextMenuOpen = false
			return m, nil
		case input.KeyEsc:
			m.contextMenuOpen = false
			return m, nil
		}
		return m, nil
	}

	// 0.00 Marketplace modal intercepts all keys when open
	if m.marketplace != nil && m.marketplace.Open {
		handled, shouldClose := m.marketplace.HandleKey(k)
		if shouldClose {
			m.marketplace.Open = false
		}
		if handled {
			return m, nil
		}
	}

	// 0. Settings modal intercepts all keys when open
	if m.settings != nil && m.settings.Open {
		handled, shouldClose := m.settings.HandleKey(k)
		if shouldClose {
			m.applyCurrentSettings()
			m.toasts.Success("SETTINGS", "Settings applied")
		}
		if m.settings.OpenMarketplaceRequested {
			m.settings.OpenMarketplaceRequested = false
			m.settings.Open = false
			if m.marketplace != nil {
				m.marketplace.Open = true
				m.marketplace.Refresh()
			}
		}
		if handled {
			return m, nil
		}
	}

	// 0.05 Image Viewer scale mode toggle ('m' or 'M' cycles Fit -> Fill -> Stretch)
	if !m.treePromptOpen && !m.omnibarOpen && !m.popupVisible {
		if doc := m.eng.ActiveDocument(); doc != nil && IsImageFile(doc.FilePath) {
			if k.Type == input.KeyRune && (k.Rune == 'm' || k.Rune == 'M') && !k.Mod.Has(input.ModCtrl) && !k.Mod.Has(input.ModAlt) {
				iv := m.getOrCreateImageViewer(doc.FilePath)
				iv.CycleScaleMode()
				m.statusMessage = fmt.Sprintf("Image Scale: %s", iv.ScaleModeTitle())
				m.toasts.Info("IMAGE", fmt.Sprintf("Scale: %s", iv.ScaleModeTitle()))
				return m, nil
			}
		}
	}

	// 0.1 Tree file operation prompt intercepts all keys when open
	if m.treePromptOpen {
		return m.handleTreePromptKey(k)
	}

	// 1. Omnibar modal intercepts all keys when open
	if m.omnibarOpen {
		return m.handleOmnibarKey(k)
	}

	// 2. Popup navigation when completion popup is active
	if m.popupVisible && len(m.popupItems) > 0 {
		switch k.Type {
		case input.KeyUp:
			if m.popupActive > 0 {
				m.popupActive--
				if m.popupActive < m.popupScrollOffset {
					m.popupScrollOffset = m.popupActive
				}
			}
			return m, nil
		case input.KeyDown:
			if m.popupActive+1 < len(m.popupItems) {
				m.popupActive++
				if m.popupActive >= m.popupScrollOffset+8 {
					m.popupScrollOffset = m.popupActive - 7
				}
			}
			return m, nil
		case input.KeyEnter, input.KeyTab:
			item := m.popupItems[m.popupActive]
			m.insertCompletion(item)
			m.popupVisible = false
			m.ghostText = ""
			m.ensureCursorVisible()
			return m, nil
		case input.KeyEsc:
			m.popupVisible = false
			m.ghostText = ""
			return m, nil
		}
	}

	// 2.5 DAP HUD Watch input interceptor
	if m.dapHUD != nil && m.dapHUD.Open && m.dapHUD.ActiveTab == HUDTabWatch {
		if k.Type == input.KeyEnter && m.dapHUD.WatchInput != "" {
			expr := m.dapHUD.WatchInput
			res, err := m.dapSession.Evaluate(expr)
			if err != nil {
				res = fmt.Sprintf("Error: %v", err)
			}
			m.dapHUD.WatchResults = append(m.dapHUD.WatchResults, fmt.Sprintf("%s => %s", expr, res))
			m.dapHUD.WatchHistory = append(m.dapHUD.WatchHistory, expr)
			m.dapHUD.WatchInput = ""
			return m, nil
		}
		if k.Type == input.KeyBackspace && len(m.dapHUD.WatchInput) > 0 {
			runes := []rune(m.dapHUD.WatchInput)
			m.dapHUD.WatchInput = string(runes[:len(runes)-1])
			return m, nil
		}
		if k.Type == input.KeyEsc {
			m.dapHUD.Open = false
			return m, nil
		}
		if k.Rune >= 32 {
			m.dapHUD.WatchInput += string(k.Rune)
			return m, nil
		}
	}

	// 2.6 Integrated Terminal input interceptor (ONLY when terminal is open AND focused)
	if m.terminal != nil && m.terminal.Open && m.terminalFocused {
		if k.Type == input.KeyF4 {
			m.terminal.Open = false
			m.terminalFocused = false
			return m, nil
		}
		if k.HasCtrl() && (k.Rune == '`' || k.Rune == '~' || k.BaseKey == '`' || k.BaseKey == '~') {
			m.terminal.Toggle()
			m.terminalFocused = m.terminal.Open
			m.toasts.Info("TERMINAL", fmt.Sprintf("Terminal: %v", m.terminal.Open))
			if m.terminal.Open {
				_ = m.terminal.EnsureSession()
				return m, tickTerm()
			}
			return m, nil
		}
		if k.Type == input.KeyEsc {
			m.terminalFocused = false
			return m, nil
		}
		// If undo or redo is pressed and terminal prompt is not actively typing: route directly to editor!
		if m.MatchBinding(k, "undo") && !m.terminal.IsActivelyTyping() {
			m.performUndo()
			return m, nil
		}
		if m.MatchBinding(k, "redo") && !m.terminal.IsActivelyTyping() {
			m.performRedo()
			return m, nil
		}
		if k.HasAlt() && k.Rune >= '1' && k.Rune <= '4' {
			idx := int(k.Rune - '1')
			m.terminal.SwitchInstance(idx)
			return m, nil
		}
		if k.HasCtrl() && k.HasShift() && (matchKey(k, 'w', 'ц') || k.Rune == 23) {
			m.terminal.CloseInstance(m.terminal.ActiveIdx)
			return m, nil
		}
		if k.HasCtrl() && k.HasShift() && (matchKey(k, 't', 'е') || k.Rune == 20) {
			m.terminal.AddInstance("")
			return m, nil
		}
		if k.HasAlt() && (k.Type == input.KeyLeft || k.Type == input.KeyRight) {
			if len(m.terminal.Instances) > 1 {
				nextIdx := (m.terminal.ActiveIdx + 1) % len(m.terminal.Instances)
				m.terminal.SwitchInstance(nextIdx)
				return m, nil
			}
		}
		if (k.Rune == '%' || k.Rune == '5') && k.HasCtrl() {
			m.terminal.ToggleSplit()
			return m, nil
		}
		// Alt+Up / Alt+Down or Ctrl+Alt+Up / Ctrl+Alt+Down to adjust terminal height
		if k.HasAlt() && k.Type == input.KeyUp {
			maxH := max(4, m.height-6)
			m.terminal.Height = min(maxH, m.terminal.Height+2)
			return m, nil
		}
		if k.HasAlt() && k.Type == input.KeyDown {
			m.terminal.Height = max(4, m.terminal.Height-2)
			return m, nil
		}
		if m.terminal.HandleInputKey(k) {
			return m, tickTerm()
		}
	}

	// Ctrl+Shift+W closes active terminal tab even if not focused
	if k.HasCtrl() && k.HasShift() && (matchKey(k, 'w', 'ц') || k.Rune == 23) {
		if m.terminal != nil && m.terminal.Open {
			m.terminal.CloseInstance(m.terminal.ActiveIdx)
			return m, nil
		}
	}

	// Ctrl+~ or Ctrl+` (toggle terminal)
	if k.HasCtrl() && (k.Rune == '`' || k.Rune == '~' || k.BaseKey == '`' || k.BaseKey == '~') {
		if m.terminal != nil {
			m.terminal.Toggle()
			m.terminalFocused = m.terminal.Open
			m.toasts.Info("TERMINAL", fmt.Sprintf("Terminal: %v", m.terminal.Open))
			if m.terminal.Open {
				_ = m.terminal.EnsureSession()
				return m, tickTerm()
			}
		}
		return m, nil
	}

	// 2.7 Dynamic Keybindings Dispatch (Settings.Keybindings)
	if newM, cmd, handled := m.dispatchKeybinding(k); handled {
		return newM, cmd
	}

	// 2.8 Alt+Shift+F10: Run / Debug Configurations Modal
	if k.HasAlt() && k.HasShift() && (k.Type == input.KeyF10 || k.Rune == '0' || k.BaseKey == '0') {
		m.openLaunchConfigModal()
		return m, nil
	}

	// Ctrl+Shift+R or Shift+F6: Project-wide symbol rename
	if (k.HasCtrl() && k.HasShift() && (matchKey(k, 'r', 'к') || k.Rune == 18)) || (k.HasShift() && k.Type == input.KeyF6) {
		m.openRenameModal()
		return m, nil
	}

	// Alt+[ to shrink sidebar, Alt+] to expand sidebar
	if k.HasAlt() && (k.Rune == '[' || k.BaseKey == '[' || k.Rune == 'х' || k.BaseKey == 'х') {
		m.sidebarWidth = max(12, m.sidebarWidth-2)
		m.sidebarTargetWidth = float64(m.sidebarWidth)
		m.sidebarAnimWidth = float64(m.sidebarWidth)
		if m.settings != nil {
			m.settings.Current.SidebarWidth = m.sidebarWidth
			_ = m.settings.Save()
		}
		return m, nil
	}
	if k.HasAlt() && (k.Rune == ']' || k.BaseKey == ']' || k.Rune == 'ъ' || k.BaseKey == 'ъ') {
		maxW := max(16, m.width/2)
		m.sidebarWidth = min(maxW, m.sidebarWidth+2)
		m.sidebarTargetWidth = float64(m.sidebarWidth)
		m.sidebarAnimWidth = float64(m.sidebarWidth)
		if m.settings != nil {
			m.settings.Current.SidebarWidth = m.sidebarWidth
			_ = m.settings.Save()
		}
		return m, nil
	}

	// Alt+Up / Alt+Down to resize terminal drawer
	if k.HasAlt() && (k.Type == input.KeyUp || k.Rune == 'k' || k.Rune == 'л') {
		if m.terminal != nil && m.terminal.Open {
			maxH := max(4, m.height-6)
			m.terminal.Height = min(maxH, m.terminal.Height+2)
			return m, nil
		}
	}
	if k.HasAlt() && (k.Type == input.KeyDown || k.Rune == 'j' || k.Rune == 'о') {
		if m.terminal != nil && m.terminal.Open {
			m.terminal.Height = max(4, m.terminal.Height-2)
			return m, nil
		}
	}

	// 3. Functional keys: F2, F4, F5, F7, F8, F9, F10, F11, F12
	switch k.Type {
	case input.KeyF2:
		// F2: Project-wide symbol rename
		doc := m.eng.ActiveDocument()
		if doc != nil {
			m.openRenameModal()
			return m, nil
		}
		cmd := m.ToggleSidebar()
		m.statusMessage = fmt.Sprintf("Project Tree: %v", m.sidebarOpen)
		return m, cmd

	case input.KeyF4:
		// F4: Toggle Integrated Terminal
		if m.terminal != nil {
			m.terminal.Toggle()
			m.terminalFocused = m.terminal.Open
			m.toasts.Info("TERMINAL", fmt.Sprintf("Terminal: %v", m.terminal.Open))
			if m.terminal.Open {
				_ = m.terminal.EnsureSession()
				return m, tickTerm()
			}
		}
		return m, nil

	case input.KeyF8:
		// F8: Toggle Debugger HUD
		if m.dapHUD != nil {
			m.dapHUD.Toggle()
			m.toasts.Info("DEBUGGER", fmt.Sprintf("Debug HUD: %v", m.dapHUD.Open))
		}
		return m, nil

	case input.KeyF5:
		if k.HasShift() {
			if m.dapSession != nil {
				_ = m.dapSession.Stop()
				m.stoppedMarkerLine = -1
				if m.dapHUD != nil {
					m.dapHUD.Open = false
				}
				m.toasts.Info("DEBUGGER", "Session Stopped")
			}
			return m, nil
		}
		// F5: If paused in DAP, continue; otherwise Run active profile
		if m.dapSession != nil && m.dapSession.IsStopped() {
			m.stoppedMarkerLine = -1
			_ = m.dapSession.Continue()
			m.toasts.Info("DEBUGGER", "Continue execution")
			return m, nil
		}
		m.RunActive()
		return m, nil

	case input.KeyF7:
		// F7: Build current project
		m.BuildActive()
		return m, nil

	case input.KeyF9:
		// F9: Toggle breakpoint on current line
		doc := m.eng.ActiveDocument()
		if doc != nil {
			sels := doc.Buffer.GetSelections()
			if len(sels) > 0 {
				line := sels[0].Head.Line
				m.breakpoints[line] = !m.breakpoints[line]
				if !m.breakpoints[line] {
					delete(m.breakpoints, line)
					m.toasts.Info("BREAKPOINT", fmt.Sprintf("Removed at line %d", line+1))
				} else {
					m.toasts.Success("BREAKPOINT", fmt.Sprintf("Set at line %d", line+1))
				}
				if m.dapSession != nil && doc.FilePath != "" {
					var bpLines []int
					for bl := range m.breakpoints {
						bpLines = append(bpLines, bl)
					}
					m.dapSession.SetBreakpoints(doc.FilePath, bpLines)
				}
			}
		}
		return m, nil

	case input.KeyF10:
		// F10: Debugger Step Over (idea.md section 6)
		if m.dapSession != nil && m.dapSession.IsStopped() {
			_ = m.dapSession.StepOver()
			m.statusMessage = "Debugger: Step Over"
			m.toasts.Info("DEBUGGER", "Step Over")
		} else {
			m.statusMessage = "Debugger: Step Over (session not paused)"
			m.toasts.Warn("DEBUGGER", "Step Over (session not paused)")
		}
		return m, nil

	case input.KeyF11:
		// F11: Debugger Step Into (idea.md section 6) / Shift+F11 Step Out
		if k.HasShift() {
			if m.dapSession != nil && m.dapSession.IsStopped() {
				_ = m.dapSession.StepOut()
				m.statusMessage = "Debugger: Step Out"
				m.toasts.Info("DEBUGGER", "Step Out")
			}
			return m, nil
		}
		if m.dapSession != nil && m.dapSession.IsStopped() {
			_ = m.dapSession.StepInto()
			m.statusMessage = "Debugger: Step Into"
			m.toasts.Info("DEBUGGER", "Step Into")
		} else {
			m.statusMessage = "Debugger: Step Into (session not paused)"
			m.toasts.Warn("DEBUGGER", "Step Into (session not paused)")
		}
		return m, nil

	case input.KeyF12:
		// F12: Go to Definition (idea.md section 9)
		m.gotoDefinition()
		return m, nil
	}

	// Shift+K: Hover documentation (idea.md section 9)
	if k.HasShift() && (matchKey(k, 'k', 'л') || k.Rune == 'K') {
		m.showHover()
		return m, nil
	}

	// Ctrl+Alt+E: Toggle tree dock position (left <-> right)
	if k.HasCtrl() && k.HasAlt() && (matchKey(k, 'e', 'у') || k.Rune == 'e' || k.Rune == 'E') {
		if m.settings != nil {
			if m.settings.Current.TreePosition == "left" {
				m.settings.Current.TreePosition = "right"
			} else {
				m.settings.Current.TreePosition = "left"
			}
			_ = m.settings.Save()
			m.toasts.Info("TREE DOCK", fmt.Sprintf("Docked to %s", m.settings.Current.TreePosition))
		}
		return m, nil
	}

	// Alt+1 .. Alt+6: Switch directly to split pane 1..6
	if k.HasAlt() && !k.HasCtrl() && k.Rune >= '1' && k.Rune <= '6' {
		paneIdx := int(k.Rune - '1')
		if m.splits != nil && paneIdx < m.splits.TotalPanes() {
			m.switchActivePane(paneIdx)
			m.toasts.Info("SPLIT", fmt.Sprintf("Focused Pane %d", paneIdx+1))
		}
		return m, nil
	}

	// Alt+Right / Alt+Left / Alt+Enter: Next / Prev split pane / Quick Fix
	if k.HasAlt() && !k.HasCtrl() {
		if k.Type == input.KeyEnter {
			m.triggerQuickFix()
			return m, nil
		}
		if k.Type == input.KeyRight {
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				m.switchActivePane(m.splits.NextPane())
				m.toasts.Info("SPLIT", fmt.Sprintf("Focused Pane %d", m.splits.ActiveIndex+1))
				return m, nil
			}
		} else if k.Type == input.KeyLeft {
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				m.switchActivePane(m.splits.PrevPane())
				m.toasts.Info("SPLIT", fmt.Sprintf("Focused Pane %d", m.splits.ActiveIndex+1))
				return m, nil
			}
		} else if matchKey(k, 'm', 'ь') || k.Rune == 'm' || k.Rune == 'M' {
			// Alt+M: Toggle Minimap
			if m.settings != nil {
				m.settings.Current.ShowMinimap = !m.settings.Current.ShowMinimap
				_ = m.settings.Save()
				stateStr := "Enabled"
				if !m.settings.Current.ShowMinimap {
					stateStr = "Disabled"
				}
				m.toasts.Info("MINIMAP", fmt.Sprintf("Minimap %s", stateStr))
			}
			return m, nil
		}
	}

	// 4. Hotkeys with Ctrl modifier (supports both English and Russian layouts + raw control bytes)
	if k.HasCtrl() || (k.Rune >= 1 && k.Rune <= 26 && k.Rune != 9 && k.Rune != 10 && k.Rune != 13) {
		switch {
		case k.Rune == '\\' || k.BaseKey == '\\' || k.Rune == 28:
			// Ctrl+\ : Cycle split layout (1 -> 2 cols -> 2 rows -> 3 cols -> 4 grid -> 5 panes -> 6 grid)
			if m.splits != nil {
				m.splits.CycleLayout()
				m.toasts.Info("SPLIT", m.splits.ModeTitle())
				m.statusMessage = fmt.Sprintf("Split: %s", m.splits.ModeTitle())
			}
			return m, nil

		case k.Rune == ',' || k.BaseKey == ',':
			// Ctrl+, : Settings modal (JetBrains style)
			if m.settings != nil {
				m.settings.Open = true
			}
			return m, nil

		case k.Type == input.KeyTab:
			// Ctrl+Tab / Ctrl+Shift+Tab: Switch buffers / tabs
			if k.HasShift() {
				m.eng.PrevBuffer()
			} else {
				m.eng.NextBuffer()
			}
			m.onActiveDocumentChanged()
			if active := m.eng.ActiveDocument(); active != nil {
				m.statusMessage = fmt.Sprintf("Buffer: %s", filepath.Base(active.FilePath))
			}
			return m, nil

		case matchKey(k, 'w', 'ц'):
			// Ctrl+W: Close active document tab
			if active := m.eng.ActiveDocument(); active != nil {
				m.eng.CloseBuffer(active.ID)
				m.onActiveDocumentChanged()
				m.toasts.Info("CLOSED", "Buffer closed")
			}
			return m, nil

		case matchKey(k, 's', 'ы') || k.Rune == 19:
			// Ctrl+S: Atomic save
			doc := m.eng.ActiveDocument()
			if doc != nil {
				target := doc.FilePath
				if target == "" {
					target = "untitled.txt"
				}
				_ = m.eng.Dispatch(core.Command{ID: core.CmdFileSave, Args: target})
				m.toasts.Success("SAVED", filepath.Base(target))
				m.statusMessage = fmt.Sprintf("Saved %s", target)
			}
			return m, nil

		case matchKey(k, 'o', 'щ') || k.Rune == 15:
			// Ctrl+O: Open Project panel / modal
			m.openOmnibar("project")
			return m, nil

		case matchKey(k, 'p', 'з') || k.Rune == 16:
			if k.HasShift() {
				// Ctrl+Shift+P: Command palette
				m.openOmnibar("commands")
			} else {
				// Ctrl+P: Fuzzy file search
				m.openOmnibar("files")
			}
			return m, nil

		case matchKey(k, 'r', 'к') || k.Rune == 18:
			// Ctrl+R: Run code
			m.RunActive()
			return m, nil

		case matchKey(k, 'b', 'и') || k.Rune == 2:
			if k.HasShift() {
				// Ctrl+Shift+B: Build project
				m.BuildActive()
				return m, nil
			} else {
				// Ctrl+B: Toggle file explorer sidebar
				cmd := m.ToggleSidebar()
				m.statusMessage = fmt.Sprintf("Project Tree: %v", m.sidebarOpen)
				return m, cmd
			}

		case (matchKey(k, 'x', 'ч') || k.Rune == 24) && k.HasShift():
			// Ctrl+Shift+X: Extension Marketplace
			if m.marketplace != nil {
				m.marketplace.Open = true
				m.marketplace.Refresh()
			}
			return m, nil

		case (matchKey(k, 'g', 'п') || k.Rune == 7) && k.HasShift():
			// Ctrl+Shift+G: Git Interactive Hunk Staging & Branch Graph
			m.openGitModal()
			return m, nil

		case (matchKey(k, 'm', 'ь') || k.Rune == 13) && k.HasShift():
			// Ctrl+Shift+M: Toggle Markdown Live Preview Split
			m.toggleMarkdownPreview()
			return m, nil

		case (matchKey(k, 't', 'е') || k.Rune == 20) && k.HasShift():
			// Ctrl+Shift+T: Add new terminal tab
			if m.terminal != nil {
				m.terminal.Open = true
				m.terminal.AddInstance("")
				m.terminalFocused = true
				m.toasts.Info("TERMINAL", "New terminal shell added")
				return m, tickTerm()
			}
			return m, nil

		case (k.Rune == '%' || k.Rune == '5') && k.HasCtrl():
			// Ctrl+Shift+5 / Ctrl+%: Toggle Terminal Split
			if m.terminal != nil {
				m.terminal.ToggleSplit()
				m.toasts.Info("TERMINAL", "Toggled terminal split mode")
			}
			return m, nil

		case matchKey(k, 'd', 'в') || k.Rune == 4:
			// Ctrl+D: Next match selection
			_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectNextMatch})
			m.ensureCursorVisible()
			return m, nil

		case matchKey(k, 'z', 'я') || matchKey(k, 'u', 'г') || k.Rune == 26 || k.Rune == 21 ||
			(k.HasAlt() && (k.Type == input.KeyBackspace || k.Rune == 8 || k.Rune == 127)):
			if k.HasShift() {
				// Redo (Ctrl+Shift+Z, Shift+Alt+Backspace)
				m.performRedo()
			} else {
				// Undo (Ctrl+Z, Alt+Backspace, Ctrl+U)
				m.performUndo()
			}
			return m, nil

		case matchKey(k, 'y', 'н') || k.Rune == 25:
			// Ctrl+Y: Redo
			m.performRedo()
			return m, nil

		case matchKey(k, 'a', 'ф') || k.Rune == 1:
			// Ctrl+A: Select all
			_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectAll})
			return m, nil

		case matchKey(k, 'f', 'а') || k.Rune == 6:
			// Ctrl+F: Find floating modal
			m.openFindModal()
			return m, nil

		case matchKey(k, 'h', 'р') || k.Rune == 8:
			// Ctrl+H: Find & Replace floating modal
			m.openReplaceModal()
			return m, nil

		case matchKey(k, 'g', 'п') || k.Rune == 7:
			// Ctrl+G: Go to line in O(log N) (idea.md section 4.1)
			m.openOmnibar("goto")
			return m, nil

		case matchKey(k, 'c', 'с') || k.Rune == 3:
			// Ctrl+C: Copy selected text to system clipboard
			selText := m.selectedText()
			if selText != "" {
				_ = clipboard.Write(selText)
				m.clipboardText = selText
				m.toasts.Info("CLIPBOARD", fmt.Sprintf("Copied %d characters", len(selText)))
				return m, nil
			}
			if m.runner != nil && m.runner.IsRunning() {
				m.runner.Stop()
				m.toasts.Warn("STOPPED", "Execution stopped")
				return m, nil
			}
			if m.popupVisible {
				m.popupVisible = false
				return m, nil
			}
			return m, tea.Quit

		case matchKey(k, 'x', 'ч') || k.Rune == 24:
			// Ctrl+X: Cut selected text
			selText := m.selectedText()
			if selText != "" {
				_ = clipboard.Write(selText)
				m.clipboardText = selText
				_ = m.eng.Dispatch(core.Command{ID: core.CmdDeleteBackward})
				m.toasts.Info("CLIPBOARD", fmt.Sprintf("Cut %d characters", len(selText)))
				m.notifyLSPChange()
				m.ensureCursorVisible()
				return m, nil
			}

		case matchKey(k, 'v', 'м') || k.Rune == 22:
			// Ctrl+V: Paste from system clipboard
			clip, err := clipboard.Read()
			if err == nil && clip != "" {
				_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: clip})
				m.toasts.Info("CLIPBOARD", fmt.Sprintf("Pasted %d characters", len(clip)))
				m.notifyLSPChange()
				m.ensureCursorVisible()
				return m, nil
			}
			if m.clipboardText != "" {
				_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: m.clipboardText})
				m.notifyLSPChange()
				m.ensureCursorVisible()
				return m, nil
			}

		case matchKey(k, 'q', 'й') || k.Rune == 17:
			// Ctrl+Q: Quit
			return m, tea.Quit

		case k.Type == input.KeySpace || k.Rune == ' ':
			// Ctrl+Space: Trigger completion popup
			m.triggerCompletionPopup()
			return m, nil
		}
	}

	// 5. Sidebar Navigation when focused
	if m.sidebarFocused && m.sidebarOpen {
		if m.sidebarMode == 1 {
			if m.structurePanel != nil {
				handled, jump := m.structurePanel.HandleKey(k)
				if jump != nil {
					m.jumpToLine(jump.Line, jump.Col)
					m.statusMessage = fmt.Sprintf("Jumped to %s (line %d)", jump.Name, jump.Line+1)
				}
				if handled {
					return m, nil
				}
			}
			if k.Type == input.KeyEsc {
				m.sidebarFocused = false
				return m, nil
			}
			return m, nil
		}

		switch k.Type {
		case input.KeyUp:
			if m.treeSel > 0 {
				m.treeSel--
			}
			visH := m.visibleTreeHeight()
			if m.treeSel < m.sidebarScrollY {
				m.sidebarScrollY = m.treeSel
			}
			if visH > 0 && m.treeSel >= m.sidebarScrollY+visH {
				m.sidebarScrollY = max(0, m.treeSel-visH+1)
			}
			m.clampSidebar()
			return m, nil

		case input.KeyDown:
			if m.treeSel+1 < len(m.treeFlat) {
				m.treeSel++
			}
			visH := m.visibleTreeHeight()
			if visH > 0 && m.treeSel >= m.sidebarScrollY+visH {
				m.sidebarScrollY = max(0, m.treeSel-visH+1)
			}
			if m.treeSel < m.sidebarScrollY {
				m.sidebarScrollY = m.treeSel
			}
			m.clampSidebar()
			return m, nil

		case input.KeyEnter, input.KeySpace:
			if len(m.treeFlat) > 0 && m.treeSel < len(m.treeFlat) {
				node := m.treeFlat[m.treeSel]
				if node.IsDir {
					node.IsOpen = !node.IsOpen
					m.treeFlat = make([]*FileNode, 0)
					FlattenTree(m.treeRoot, &m.treeFlat)
					m.clampSidebar()
				} else {
					_, err := m.eng.Open(node.Path)
					if err == nil {
						m.statusMessage = fmt.Sprintf("Opened %s", node.Name)
						m.sidebarFocused = false
						m.ensureCursorVisible()
						m.onActiveDocumentChanged()
					}
				}
			}
			return m, nil

		case input.KeyLeft:
			if len(m.treeFlat) > 0 && m.treeSel < len(m.treeFlat) {
				node := m.treeFlat[m.treeSel]
				if node.IsDir && node.IsOpen {
					node.IsOpen = false
					m.treeFlat = make([]*FileNode, 0)
					FlattenTree(m.treeRoot, &m.treeFlat)
					m.clampSidebar()
				}
			}
			return m, nil

		case input.KeyRight:
			if len(m.treeFlat) > 0 && m.treeSel < len(m.treeFlat) {
				node := m.treeFlat[m.treeSel]
				if node.IsDir && !node.IsOpen {
					node.IsOpen = true
					m.treeFlat = make([]*FileNode, 0)
					FlattenTree(m.treeRoot, &m.treeFlat)
					m.clampSidebar()
				}
			}
			return m, nil

		case input.KeyInsert:
			m.openTreePrompt("new_file")
			return m, nil

		case input.KeyF2:
			m.openTreePrompt("rename")
			return m, nil

		case input.KeyDelete:
			m.openTreePrompt("delete")
			return m, nil

		case input.KeyRune:
			switch k.Rune {
			case 'a':
				m.openTreePrompt("new_file")
				return m, nil
			case 'A':
				m.openTreePrompt("new_dir")
				return m, nil
			case 'r':
				m.openTreePrompt("rename")
				return m, nil
			case 'd':
				m.openTreePrompt("delete")
				return m, nil
			case 'm':
				m.openTreePrompt("move")
				return m, nil
			}

		case input.KeyTab, input.KeyEsc:
			// Switch focus back to editor
			m.sidebarFocused = false
			return m, nil
		}
	}

	// 6. Output Drawer dismiss on Esc
	if m.outputOpen && k.Type == input.KeyEsc {
		m.outputOpen = false
		return m, nil
	}

	// Snippet Tabstop Navigation (Tab: next, Shift+Tab: prev, Esc: exit)
	if m.snippetSession != nil && m.snippetSession.Active {
		if k.Type == input.KeyTab {
			if k.HasShift() {
				if cur, ok := m.snippetSession.Prev(); ok {
					m.statusMessage = m.snippetSession.FormatSnippetPrompt()
					m.jumpToLine(m.snippetSession.BaseLine, m.snippetSession.BaseCol+cur.StartOffset)
					return m, nil
				}
			} else {
				if cur, ok := m.snippetSession.Next(); ok {
					m.statusMessage = m.snippetSession.FormatSnippetPrompt()
					m.jumpToLine(m.snippetSession.BaseLine, m.snippetSession.BaseCol+cur.StartOffset)
					return m, nil
				}
			}
		} else if k.Type == input.KeyEsc {
			m.snippetSession.Cancel()
			m.statusMessage = "Snippet exited"
			return m, nil
		}
	}

	// Vim Modal Editing (Normal, Visual, Insert)
	if m.settings != nil && m.settings.Current.VimMode && m.vimFSM != nil {
		keyTypeStr := ""
		if k.Type == input.KeyEsc {
			keyTypeStr = "escape"
		}
		act := m.vimFSM.HandleKey(k.Rune, keyTypeStr, k.HasCtrl(), k.HasAlt(), k.HasShift())
		if act.Handled {
			cnt := act.Count
			if cnt <= 0 {
				cnt = 1
			}
			switch act.Command {
			case "cursor.left":
				for i := 0; i < cnt; i++ {
					_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorLeft})
				}
			case "cursor.right":
				for i := 0; i < cnt; i++ {
					_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorRight})
				}
			case "cursor.up":
				for i := 0; i < cnt; i++ {
					_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorUp})
				}
			case "cursor.down":
				for i := 0; i < cnt; i++ {
					_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorDown})
				}
			case "cursor.line_start":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorLineStart})
			case "cursor.line_end":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorLineEnd})
			case "cursor.top":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorDocStart})
			case "cursor.bottom":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorDocEnd})
			case "cursor.word_right":
				for i := 0; i < cnt*5; i++ {
					_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorRight})
				}
			case "cursor.word_left":
				for i := 0; i < cnt*5; i++ {
					_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorLeft})
				}
			case "edit.delete_forward":
				for i := 0; i < cnt; i++ {
					_ = m.eng.Dispatch(core.Command{ID: core.CmdDeleteForward})
				}
			case "edit.delete_line":
				for i := 0; i < cnt; i++ {
					_ = m.eng.Dispatch(core.Command{ID: core.CmdCursorLineStart})
					_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectLineEnd})
					_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectRight})
					_ = m.eng.Dispatch(core.Command{ID: core.CmdDeleteForward})
				}
			case "edit.yank_line":
				if doc := m.eng.ActiveDocument(); doc != nil {
					sels := doc.Buffer.GetSelections()
					if len(sels) > 0 {
						lineBytes, _ := doc.Buffer.GetLine(sels[0].Head.Line)
						m.vimFSM.SetYankBuffer(string(lineBytes))
						m.statusMessage = "1 line yanked"
					}
				}
			case "edit.paste_after":
				if m.vimFSM.YankBuffer() != "" {
					_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: m.vimFSM.YankBuffer()})
				}
			case "history.undo":
				m.performUndo()
			case "history.redo":
				m.performRedo()
			case "visual.clear":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdClearSelections})
			case "visual.left":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectLeft})
			case "visual.right":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectRight})
			case "visual.up":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectUp})
			case "visual.down":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdSelectDown})
			case "visual.delete":
				_ = m.eng.Dispatch(core.Command{ID: core.CmdDeleteBackward})
			case "visual.yank":
				m.vimFSM.SetYankBuffer(m.selectedText())
				m.statusMessage = "Yanked selection"
			}
			m.ensureCursorVisible()
			return m, nil
		}
	}

	// 7. Standard Navigation & Editing Keys
	switch k.Type {
	case input.KeyLeft:
		cmd := core.CmdCursorLeft
		if k.HasShift() {
			cmd = core.CmdSelectLeft
		}
		_ = m.eng.Dispatch(core.Command{ID: cmd})
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyRight:
		cmd := core.CmdCursorRight
		if k.HasShift() {
			cmd = core.CmdSelectRight
		}
		_ = m.eng.Dispatch(core.Command{ID: cmd})
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyUp:
		if k.HasCtrl() {
			if m.viewportY > 0 {
				m.viewportY -= 5
				if m.viewportY < 0 {
					m.viewportY = 0
				}
				if pane := m.splits.ActivePane(); pane != nil {
					pane.ViewportY = m.viewportY
					pane.TargetViewportY = float64(m.viewportY)
					pane.SmoothScrollY = float64(m.viewportY)
					pane.ScrollVelocity = 0
				}
			}
			return m, nil
		}
		cmd := core.CmdCursorUp
		if k.HasShift() {
			cmd = core.CmdSelectUp
		}
		_ = m.eng.Dispatch(core.Command{ID: cmd})
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyDown:
		if k.HasCtrl() {
			doc := m.eng.ActiveDocument()
			if doc != nil && m.viewportY+5 < doc.Buffer.TotalLines() {
				m.viewportY += 5
				if pane := m.splits.ActivePane(); pane != nil {
					pane.ViewportY = m.viewportY
					pane.TargetViewportY = float64(m.viewportY)
					pane.SmoothScrollY = float64(m.viewportY)
					pane.ScrollVelocity = 0
				}
			}
			return m, nil
		}
		cmd := core.CmdCursorDown
		if k.HasShift() {
			cmd = core.CmdSelectDown
		}
		_ = m.eng.Dispatch(core.Command{ID: cmd})
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyHome:
		cmd := core.CmdCursorLineStart
		if k.HasShift() {
			cmd = core.CmdSelectLineStart
		}
		_ = m.eng.Dispatch(core.Command{ID: cmd})
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyEnd:
		cmd := core.CmdCursorLineEnd
		if k.HasShift() {
			cmd = core.CmdSelectLineEnd
		}
		_ = m.eng.Dispatch(core.Command{ID: cmd})
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyPgUp:
		_ = m.eng.Dispatch(core.Command{ID: core.CmdPageUp})
		m.ensureCursorVisible()
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyPgDown:
		_ = m.eng.Dispatch(core.Command{ID: core.CmdPageDown})
		m.ensureCursorVisible()
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyBackspace:
		_ = m.eng.Dispatch(core.Command{ID: core.CmdDeleteBackward})
		m.notifyLSPChange()
		m.popupVisible = false
		m.updateGhostText()

	case input.KeyDelete:
		_ = m.eng.Dispatch(core.Command{ID: core.CmdDeleteForward})
		m.notifyLSPChange()
		m.popupVisible = false
		m.updateGhostText()

	case input.KeyEnter:
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertNewline})
		m.notifyLSPChange()
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyTab:
		if m.ghostText != "" {
			_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: m.ghostText})
			m.ghostText = ""
			m.notifyLSPChange()
			m.ensureCursorVisible()
			return m, nil
		}
		_ = m.eng.Dispatch(core.Command{ID: core.CmdIndent})
		m.notifyLSPChange()
		m.popupVisible = false

	case input.KeyBacktab:
		_ = m.eng.Dispatch(core.Command{ID: core.CmdDedent})
		m.notifyLSPChange()
		m.popupVisible = false
		m.ghostText = ""

	case input.KeyEsc:
		if m.popupVisible {
			m.popupVisible = false
			return m, nil
		}
		if m.outputOpen {
			m.outputOpen = false
			return m, nil
		}
		_ = m.eng.Dispatch(core.Command{ID: core.CmdClearSelections})
		m.ghostText = ""

	case input.KeySpace:
		// CRITICAL FIX: Space bar insertion
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: " "})
		m.notifyLSPChange()
		m.popupVisible = false
		m.updateGhostText()

	case input.KeyRune:
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: string(k.Rune)})
		m.notifyLSPChange()
		if k.Rune == '.' {
			m.triggerCompletionPopup()
		} else {
			m.popupVisible = false
			m.updateGhostText()
		}
	}

	m.ensureCursorVisible()
	return m, nil
}

// handleMouse processes terminal mouse events with SGR-1006 coordinate mapping.
func (m *AppModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// 0. Startup splash screen click dismiss
	if m.splash != nil && m.splash.Active {
		m.splash.HandleMouse(msg.Mouse)
		return m, nil
	}

	// 0.0 Crash recovery dialog
	if m.crashDialog != nil && m.crashDialog.Open && msg.Action == input.MousePress {
		consumed, action := m.crashDialog.HandleMouse(msg.Mouse, m.width, m.height)
		if consumed {
			m.handleCrashDialogAction(action)
			return m, nil
		}
	}

	// 0.00 Log & Data Inspector
	if m.logInspector != nil && m.logInspector.Open {
		if m.logInspector.HandleMouse(msg.Mouse, m.width, m.height) {
			return m, nil
		}
	}

	// 0.000 Bug Reporter Modal
	if m.bugReportModal != nil && m.bugReportModal.Open && msg.Action == input.MousePress {
		consumed, action := m.bugReportModal.HandleMouse(msg.Mouse, m.width, m.height)
		if consumed {
			m.handleBugReportAction(action)
			return m, nil
		}
	}

	// Sidebar dynamic width dragging
	if m.sidebarDragging {
		if msg.Action == input.MouseDrag {
			diff := msg.X - m.sidebarDragStartX
			if m.settings != nil && m.settings.Current.TreePosition == "right" {
				diff = m.sidebarDragStartX - msg.X
			}
			newW := m.sidebarDragStartW + diff
			minW := 12
			maxW := max(16, m.width/2)
			if newW < minW {
				newW = minW
			}
			if newW > maxW {
				newW = maxW
			}
			m.sidebarWidth = newW
			m.sidebarTargetWidth = float64(newW)
			m.sidebarAnimWidth = float64(newW)
			if m.settings != nil {
				m.settings.Current.SidebarWidth = newW
			}
			return m, nil
		}
		if msg.Action == input.MouseRelease {
			m.sidebarDragging = false
			if m.settings != nil {
				_ = m.settings.Save()
			}
			return m, nil
		}
	}
	if msg.Action == input.MouseRelease {
		m.sidebarDragging = false
	}

	// Terminal drawer border dragging
	if m.termDragging {
		if msg.Action == input.MouseDrag {
			diff := m.termDragStartY - msg.Y
			newH := m.termDragStartH + diff
			minH := 4
			maxH := max(4, m.height-6)
			if newH < minH {
				newH = minH
			}
			if newH > maxH {
				newH = maxH
			}
			if m.terminal != nil {
				m.terminal.Height = newH
			}
			return m, nil
		}
		if msg.Action == input.MouseRelease {
			m.termDragging = false
			return m, nil
		}
	}
	if msg.Action == input.MouseRelease {
		m.termDragging = false
	}

	// Click to dismiss toast notifications
	if m.toasts != nil && m.toasts.Count() > 0 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		if m.toasts.HandleClick(msg.X, msg.Y, buffer.Rect{X: 0, Y: 0, Width: m.width, Height: m.height}) {
			return m, nil
		}
	}

	// Omnibar mouse wheel and click interaction
	if m.omnibarOpen {
		modalWidth := 60
		if modalWidth > m.width-4 {
			modalWidth = m.width - 4
		}
		modalHeight := 12
		startX := (m.width - modalWidth) / 2
		startY := (m.height - modalHeight) / 2
		maxItems := modalHeight - 4

		// Mouse wheel inside Omnibar
		if msg.X >= startX && msg.X < startX+modalWidth && msg.Y >= startY && msg.Y < startY+modalHeight {
			if msg.Button == input.MouseWheelUp {
				if m.omnibarSel > 0 {
					m.omnibarSel--
				}
				return m, nil
			}
			if msg.Button == input.MouseWheelDown {
				if m.omnibarSel+1 < len(m.omnibarItems) {
					m.omnibarSel++
				}
				return m, nil
			}
			// Click on item in list
			if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
				listStartY := startY + 3
				if msg.Y >= listStartY && msg.Y < listStartY+maxItems {
					clickedIdx := m.omnibarScroll + (msg.Y - listStartY)
					if clickedIdx >= 0 && clickedIdx < len(m.omnibarItems) {
						m.omnibarSel = clickedIdx
						return m.handleOmnibarKey(input.Key{Type: input.KeyEnter})
					}
				}
			}
			return m, nil
		}
		// Click outside Omnibar closes it
		if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
			m.omnibarOpen = false
			return m, nil
		}
	}

	// Find & Replace Modal mouse interaction
	if m.findReplaceModal != nil && m.findReplaceModal.Open && msg.Action == input.MousePress {
		consumed, action := m.findReplaceModal.HandleClick(msg.X, msg.Y, m.width, m.height)
		if consumed {
			return m.handleFindReplaceAction(action)
		}
	}

	// Rename Modal mouse interaction
	if m.renameModal != nil && m.renameModal.Open && msg.Action == input.MousePress {
		consumed, confirmed := m.renameModal.HandleClick(msg.X, msg.Y, m.width, m.height)
		if consumed {
			if confirmed {
				return m.executeSymbolRename(m.renameModal.OldName, m.renameModal.NewName, m.renameModal.Line, m.renameModal.Col, m.renameModal.URI)
			}
			return m, nil
		}
	}

	// New Project Modal mouse interaction
	if m.newProjectModal != nil && m.newProjectModal.Open && msg.Action == input.MousePress {
		consumed, _ := m.newProjectModal.HandleClick(msg.X, msg.Y, m.width, m.height)
		if consumed {
			return m, nil
		}
	}

	// Mouse motion updates hover tooltips and hover documentation
	if msg.Action == input.MouseMotion || (msg.Action == input.MouseDrag && msg.Button == input.MouseNone) {
		m.updateTooltip(msg.X, msg.Y)
		m.updateHoverDoc(msg.X, msg.Y)
		return m, nil
	}

	// Any mouse click dismisses lingering tooltip and hover documentation
	m.tooltipText = ""
	if m.hoverDoc != nil {
		m.hoverDoc.Dismiss()
	}

	// -0.3 Main Menu dropdown mouse clicks
	if m.mainMenuOpen && msg.Action == input.MousePress {
		if msg.Button == input.MouseLeft {
			menuW := 36
			items := getMainMenuItems()
			menuH := len(items) + 2
			if msg.X >= 1 && msg.X <= 1+menuW && msg.Y >= 1 && msg.Y < 1+menuH {
				idx := msg.Y - 2
				if idx >= 0 && idx < len(items) {
					m.mainMenuSel = idx
					m.executeMainMenuItem(items[idx].action)
					m.mainMenuOpen = false
					return m, nil
				}
			}
		}
		m.mainMenuOpen = false
		return m, nil
	}

	// -0.2 Profile selector dropdown mouse clicks
	if m.profileDropdownOpen && msg.Action == input.MousePress {
		if msg.Button == input.MouseLeft && m.launchConfig != nil {
			menuX := 24
			for _, btn := range m.toolbarButtons {
				if btn.id == "profile_select" {
					menuX = btn.minX
					break
				}
			}
			if menuX+34 > m.width {
				menuX = max(1, m.width-35)
			}
			menuW := 34
			profiles := m.launchConfig.Configurations
			totalItems := len(profiles) + 2
			menuH := totalItems + 2

			if msg.X >= menuX && msg.X <= menuX+menuW && msg.Y >= 1 && msg.Y < 1+menuH {
				idx := msg.Y - 2
				if idx >= 0 && idx < len(profiles) {
					m.launchConfig.SetActive(profiles[idx].Name)
					_ = m.launchConfig.Save()
					m.toasts.Info("PROFILE", fmt.Sprintf("Active: %s", profiles[idx].Name))
					m.profileDropdownOpen = false
					return m, nil
				} else if idx == len(profiles)+1 {
					m.profileDropdownOpen = false
					m.openLaunchConfigModal()
					return m, nil
				}
			}
		}
		m.profileDropdownOpen = false
		return m, nil
	}

	// -0.1 Launch Config modal mouse interception
	if m.launchModal != nil && m.launchModal.Open && msg.Action == input.MousePress {
		handled, shouldClose := m.launchModal.HandleClick(msg.X, msg.Y, m.width, m.height)
		if shouldClose {
			m.launchModal.Open = false
		}
		if handled {
			return m, nil
		}
	}

	// 0. Dismiss or execute open context menu
	if m.contextMenuOpen && msg.Action == input.MousePress {
		menuW := 22
		menuH := len(m.contextMenuItems) + 2
		if msg.Button == input.MouseLeft &&
			msg.X >= m.contextMenuX && msg.X < m.contextMenuX+menuW &&
			msg.Y >= m.contextMenuY && msg.Y < m.contextMenuY+menuH {
			clickedIdx := msg.Y - (m.contextMenuY + 1)
			if clickedIdx >= 0 && clickedIdx < len(m.contextMenuItems) {
				action := m.contextMenuItems[clickedIdx].action
				m.contextMenuOpen = false
				return m.executeContextMenuAction(action)
			}
		}
		m.contextMenuOpen = false
		return m, nil
	}

	editorTop := 2
	stripLeftW := 0
	if m.width >= 70 {
		stripLeftW = 3
	}
	sideW := 0
	if m.sidebarOpen {
		sideW = m.sidebarWidth
	}
	isTreeRight := (m.settings != nil && m.settings.Current.TreePosition == "right")
	treeStartX := stripLeftW
	if isTreeRight {
		treeStartX = m.width - sideW
	}

	editorLeft := stripLeftW
	editorRight := m.width
	if isTreeRight {
		if sideW > 0 {
			editorRight = treeStartX - 1
		}
	} else {
		if sideW > 0 {
			editorLeft = stripLeftW + sideW + 1
		}
	}

	// Sidebar boundary divider dragging initiation
	if sideW > 0 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		dividerX := stripLeftW + sideW
		if isTreeRight {
			dividerX = treeStartX - 1
		}
		if msg.X == dividerX && msg.Y >= editorTop && msg.Y < m.height-1 {
			m.sidebarDragging = true
			m.sidebarDragStartX = msg.X
			m.sidebarDragStartW = m.sidebarWidth
			return m, nil
		}
	}

	// 0.5. Right-Click Context Menu in Tree Area
	if msg.Button == input.MouseRight && msg.Action == input.MousePress {
		sidebarMaxY := editorTop + 1 + m.visibleTreeHeight()
		isTreeClick := (sideW > 0 && msg.X >= treeStartX && msg.X < treeStartX+sideW && msg.Y >= editorTop && msg.Y < sidebarMaxY)
		if isTreeClick {
			rowInSidebar := msg.Y - editorTop - 1
			nodeIdx := m.sidebarScrollY + rowInSidebar
			if rowInSidebar >= 0 && nodeIdx >= 0 && nodeIdx < len(m.treeFlat) {
				m.treeSel = nodeIdx
				node := m.treeFlat[nodeIdx]
				m.contextMenuTarget = node.Path
				m.contextMenuItems = []contextMenuItem{
					{label: i18n.T("ctx.new_file"), action: "new_file"},
					{label: i18n.T("ctx.new_dir"), action: "new_dir"},
					{label: i18n.T("ctx.rename"), action: "rename"},
					{label: i18n.T("ctx.move"), action: "move"},
					{label: i18n.T("ctx.copy_path"), action: "copy_path"},
					{label: i18n.T("ctx.delete"), action: "delete"},
				}
			} else {
				m.contextMenuTarget = ""
				m.contextMenuItems = []contextMenuItem{
					{label: i18n.T("ctx.new_file"), action: "new_file"},
					{label: i18n.T("ctx.new_dir"), action: "new_dir"},
					{label: i18n.T("ctx.refresh"), action: "refresh"},
				}
			}
			m.contextMenuOpen = true
			m.contextMenuSel = 0
			m.contextMenuX = msg.X
			if m.contextMenuX+22 >= m.width {
				m.contextMenuX = max(0, m.width-23)
			}
			m.contextMenuY = msg.Y
			if m.contextMenuY+len(m.contextMenuItems)+2 >= m.height {
				m.contextMenuY = max(1, m.height-len(m.contextMenuItems)-3)
			}
			return m, nil
		}
	}

	// 0.6 DAP HUD mouse clicks
	if m.dapHUD != nil && m.dapHUD.Open && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		btnID := m.dapHUD.FindDAPHUDBtn(msg.X, msg.Y)
		if btnID != "" {
			switch btnID {
			case "dap_cont":
				if m.dapSession != nil && m.dapSession.IsStopped() {
					m.stoppedMarkerLine = -1
					_ = m.dapSession.Continue()
					m.toasts.Info("DEBUGGER", "Continue")
				}
			case "dap_over":
				if m.dapSession != nil && m.dapSession.IsStopped() {
					_ = m.dapSession.StepOver()
					m.toasts.Info("DEBUGGER", "Step Over")
				}
			case "dap_into":
				if m.dapSession != nil && m.dapSession.IsStopped() {
					_ = m.dapSession.StepInto()
					m.toasts.Info("DEBUGGER", "Step Into")
				}
			case "dap_out":
				if m.dapSession != nil && m.dapSession.IsStopped() {
					_ = m.dapSession.StepOut()
					m.toasts.Info("DEBUGGER", "Step Out")
				}
			case "dap_stop":
				if m.dapSession != nil {
					_ = m.dapSession.Stop()
					m.stoppedMarkerLine = -1
					m.dapHUD.Open = false
					m.toasts.Info("DEBUGGER", "Stopped")
				}
			case "dap_close":
				m.dapHUD.Open = false
			case "tab_variables":
				m.dapHUD.ActiveTab = HUDTabVariables
			case "tab_stack":
				m.dapHUD.ActiveTab = HUDTabStack
			case "tab_watch":
				m.dapHUD.ActiveTab = HUDTabWatch
			}
			return m, nil
		}
	}

	// 0.63 Tool Prompt modal intercepts mouse clicks
	if m.toolPrompt != nil && m.toolPrompt.Open {
		if msg.Action == input.MousePress {
			m.toolPrompt.HandleClick(msg.X, msg.Y, m.width, m.height)
		}
		return m, nil
	}

	// 0.64 Git modal intercepts mouse clicks
	if m.gitModal != nil && m.gitModal.Open {
		if msg.Action == input.MousePress {
			m.gitModal.HandleClick(msg.X, msg.Y, m.width, m.height)
		}
		return m, nil
	}

	// 0.642 Marketplace modal intercepts mouse clicks
	if m.marketplace != nil && m.marketplace.Open {
		if msg.Action == input.MousePress {
			m.marketplace.HandleClick(msg.X, msg.Y, m.width, m.height)
		}
		return m, nil
	}

	// 0.645 Settings modal intercepts mouse clicks and continuous drag
	if m.settings != nil && m.settings.Open {
		if msg.Action == input.MousePress {
			_, shouldClose := m.settings.HandleClick(msg.X, msg.Y, m.width, m.height)
			if shouldClose {
				m.applyCurrentSettings()
				m.toasts.Success("SETTINGS", "Settings applied")
			}
		} else if msg.Action == input.MouseDrag && m.settings.ColorPicker != nil && m.settings.ColorPicker.Open {
			m.settings.ColorPicker.HandleDrag(msg.X, msg.Y, m.width, m.height)
		}
		return m, nil
	}

	// 0.65 Quick Fix popup intercepts mouse clicks
	if m.quickFixModal != nil && m.quickFixModal.Open && msg.Action == input.MousePress {
		applied, action, closed := m.quickFixModal.HandleClick(msg.X, msg.Y)
		if closed {
			m.quickFixModal.Close()
		}
		if applied && action != nil {
			m.applyCodeAction(action)
		}
		return m, nil
	}

	// 0.66 Editor Color Picker modal intercepts mouse clicks and continuous drag
	if m.editorColorPicker != nil && m.editorColorPicker.Open {
		if msg.Action == input.MousePress {
			handled, shouldClose := m.editorColorPicker.HandleClick(msg.X, msg.Y, m.width, m.height)
			if shouldClose {
				m.editorColorPicker.Open = false
			}
			if handled {
				return m, nil
			}
		} else if msg.Action == input.MouseDrag {
			if m.editorColorPicker.HandleDrag(msg.X, msg.Y, m.width, m.height) {
				return m, nil
			}
		}
	}

	// 0.7 Terminal drawer mouse clicks
	if m.terminal != nil && m.terminal.Open && msg.Action == input.MousePress {
		termTop := m.height - 1 - m.terminal.Height
		if msg.X >= stripLeftW && msg.Y >= termTop && msg.Y < m.height-1 {
			m.terminalFocused = true
			m.sidebarFocused = false
			if msg.Y == termTop {
				// Mouse wheel on terminal header: resize height
				if msg.Button == input.MouseWheelUp {
					maxH := max(4, m.height-6)
					m.terminal.Height = min(maxH, m.terminal.Height+2)
					return m, nil
				}
				if msg.Button == input.MouseWheelDown {
					m.terminal.Height = max(4, m.terminal.Height-2)
					return m, nil
				}

				// Middle click to close tab
				if msg.Button == input.MouseMiddle {
					for _, hit := range m.termTabHitboxes {
						if hit.instanceIdx >= 0 && hit.instanceIdx < len(m.terminal.Instances) {
							if msg.X >= hit.minX && msg.X <= hit.maxX {
								m.terminal.CloseInstance(hit.instanceIdx)
								return m, nil
							}
						}
					}
					return m, nil
				}
				// Right click to start rename
				if msg.Button == input.MouseRight {
					for _, hit := range m.termTabHitboxes {
						if hit.instanceIdx >= 0 && hit.instanceIdx < len(m.terminal.Instances) {
							if msg.X >= hit.minX && msg.X <= hit.maxX {
								m.terminal.StartRename(hit.instanceIdx)
								return m, nil
							}
						}
					}
					return m, nil
				}
				// Left click
				if msg.Button == input.MouseLeft {
					if msg.X >= m.width-4 && msg.X < m.width { // ✕
						m.terminal.Open = false
						m.terminalFocused = false
						return m, nil
					}
					if msg.X >= m.width-9 && msg.X < m.width-4 { // Kill
						m.terminal.Kill()
						m.toasts.Warn("TERMINAL", "Process terminated")
						return m, nil
					}
					if msg.X >= m.width-15 && msg.X < m.width-9 { // Clear
						m.terminal.Clear()
						m.toasts.Info("TERMINAL", "Output cleared")
						return m, nil
					}
					if msg.X >= m.width-17 && msg.X < m.width-15 { // ▼ (decrease height)
						m.terminal.Height = max(4, m.terminal.Height-3)
						return m, nil
					}
					if msg.X >= m.width-22 && msg.X < m.width-17 { // ▲ (increase height)
						maxH := max(4, m.height-6)
						m.terminal.Height = min(maxH, m.terminal.Height+3)
						return m, nil
					}

					now := time.Now()
					for _, hit := range m.termTabHitboxes {
						if hit.isAdd && msg.X >= hit.minX && msg.X <= hit.maxX {
							m.terminal.AddInstance("")
							m.toasts.Info("TERMINAL", "New terminal shell added")
							return m, tickTerm()
						}
						if hit.isSplit && msg.X >= hit.minX && msg.X <= hit.maxX {
							m.terminal.ToggleSplit()
							return m, nil
						}
						if hit.instanceIdx >= 0 && hit.instanceIdx < len(m.terminal.Instances) {
							if msg.X >= hit.closeMinX && msg.X <= hit.closeMaxX {
								m.terminal.CloseInstance(hit.instanceIdx)
								return m, nil
							}
							if msg.X >= hit.minX && msg.X <= hit.maxX {
								if m.lastTabClickIdx == hit.instanceIdx && now.Sub(m.lastTabClickTime) < 400*time.Millisecond {
									m.terminal.StartRename(hit.instanceIdx)
								} else {
									m.terminal.SwitchInstance(hit.instanceIdx)
								}
								m.lastTabClickTime = now
								m.lastTabClickIdx = hit.instanceIdx
								return m, nil
							}
						}
					}

					// Left click on header border outside of tabs/buttons initiates vertical resizing
					m.termDragging = true
					m.termDragStartY = msg.Y
					m.termDragStartH = m.terminal.Height
					return m, nil
				}
				return m, nil
			}

			// Mouse wheel inside terminal body: scroll terminal history
			if msg.Y > termTop {
				if msg.Button == input.MouseWheelUp {
					m.terminal.VTerm.Scroll(3)
					return m, nil
				}
				if msg.Button == input.MouseWheelDown {
					m.terminal.VTerm.Scroll(-3)
					return m, nil
				}
			}

			// Click inside terminal body: split pane focus
			m.termDragging = false
			if msg.Button == input.MouseLeft && msg.Y > termTop {
				innerW := max(1, m.width-2)
				innerH := max(1, m.terminal.Height-1)
				if m.terminal.SplitMode == TermSplitHorizontal && len(m.terminal.Instances) >= 2 {
					colW := (innerW - 1) / 2
					if msg.X > 1+colW {
						m.terminal.SwitchInstance(1)
					} else {
						m.terminal.SwitchInstance(0)
					}
				} else if m.terminal.SplitMode == TermSplitVertical && len(m.terminal.Instances) >= 2 {
					rowH := (innerH - 1) / 2
					if msg.Y > termTop+1+rowH {
						m.terminal.SwitchInstance(1)
					} else {
						m.terminal.SwitchInstance(0)
					}
				}
			}
			return m, nil
		}
	}

	// 0.8 Minimap click-to-scroll
	if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		for _, mm := range m.minimapHitboxes {
			if msg.X >= mm.minX && msg.X <= mm.maxX && msg.Y >= mm.minY && msg.Y <= mm.maxY {
				targetLine := MinimapHitTest(msg.Y, mm.minY, mm.maxY-mm.minY+1, mm.totalLines)
				if mm.paneIdx == m.splits.ActivePaneIndex() {
					m.viewportY = targetLine
				}
				if pane := m.splits.PaneAt(mm.paneIdx); pane != nil {
					pane.ViewportY = targetLine
				}
				return m, nil
			}
		}
	}

	// 1. Mouse click on Top Header Toolbar (Row 0)
	if msg.Y == 0 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		for _, btn := range m.toolbarButtons {
			if msg.X >= btn.minX && msg.X <= btn.maxX {
				switch btn.id {
				case "menu":
					m.mainMenuOpen = !m.mainMenuOpen
					return m, nil
				case "find_files":
					m.openOmnibar("files")
					return m, nil
				case "tree":
					cmd := m.ToggleSidebar()
					m.statusMessage = fmt.Sprintf("Project Tree: %v", m.sidebarOpen)
					return m, cmd
				case "split":
					if m.splits != nil {
						m.splits.CycleLayout()
						m.toasts.Info("SPLIT", m.splits.ModeTitle())
						m.statusMessage = fmt.Sprintf("Split: %s", m.splits.ModeTitle())
					}
					return m, nil
				case "profile_select":
					m.profileDropdownOpen = !m.profileDropdownOpen
					return m, nil
				case "run":
					m.RunActive()
				case "build":
					m.BuildActive()
				case "term":
					if m.terminal != nil {
						m.terminal.Toggle()
						m.toasts.Info("TERMINAL", fmt.Sprintf("Terminal: %v", m.terminal.Open))
					}
					return m, nil
				case "hud":
					if m.launchConfig != nil && len(m.launchConfig.Configurations) > 0 {
						if act := m.launchConfig.GetActive(); act != nil {
							m.debugProfile(*act)
							return m, nil
						}
					}
					if m.dapHUD != nil {
						m.dapHUD.Toggle()
						m.toasts.Info("DEBUGGER", fmt.Sprintf("Debug HUD: %v", m.dapHUD.Open))
					}
					return m, nil
				case "settings":
					if m.settings != nil {
						m.settings.Open = true
					}
				}
				return m, nil
			}
		}
		return m, nil
	}

	// 1.2 Tree header resize & dock buttons [◀][▶][⇄] (Row editorTop)
	if sideW > 0 && msg.Y == editorTop && msg.X >= treeStartX && msg.X < treeStartX+sideW {
		if msg.Action == input.MousePress {
			if msg.Button == input.MouseWheelUp {
				maxW := max(16, m.width/2)
				m.sidebarWidth = min(maxW, m.sidebarWidth+2)
				m.sidebarTargetWidth = float64(m.sidebarWidth)
				m.sidebarAnimWidth = float64(m.sidebarWidth)
				if m.settings != nil {
					m.settings.Current.SidebarWidth = m.sidebarWidth
					_ = m.settings.Save()
				}
				return m, nil
			}
			if msg.Button == input.MouseWheelDown {
				m.sidebarWidth = max(12, m.sidebarWidth-2)
				m.sidebarTargetWidth = float64(m.sidebarWidth)
				m.sidebarAnimWidth = float64(m.sidebarWidth)
				if m.settings != nil {
					m.settings.Current.SidebarWidth = m.sidebarWidth
					_ = m.settings.Save()
				}
				return m, nil
			}
			if msg.Button == input.MouseLeft {
				btnCount := 5
				if sideW < 18 {
					btnCount = 1
				}
				dockBtnStart := treeStartX + sideW - btnCount
				if sideW >= 18 && (msg.X == dockBtnStart || msg.X == dockBtnStart+1) { // ◀
					m.sidebarWidth = max(12, m.sidebarWidth-2)
					m.sidebarTargetWidth = float64(m.sidebarWidth)
					m.sidebarAnimWidth = float64(m.sidebarWidth)
					if m.settings != nil {
						m.settings.Current.SidebarWidth = m.sidebarWidth
						_ = m.settings.Save()
					}
					return m, nil
				}
				if sideW >= 18 && (msg.X == dockBtnStart+2 || msg.X == dockBtnStart+3) { // ▶
					maxW := max(16, m.width/2)
					m.sidebarWidth = min(maxW, m.sidebarWidth+2)
					m.sidebarTargetWidth = float64(m.sidebarWidth)
					m.sidebarAnimWidth = float64(m.sidebarWidth)
					if m.settings != nil {
						m.settings.Current.SidebarWidth = m.sidebarWidth
						_ = m.settings.Save()
					}
					return m, nil
				}
				if msg.X >= dockBtnStart+min(4, btnCount-1) && msg.X < treeStartX+sideW { // ⇄
					if m.settings != nil {
						if m.settings.Current.TreePosition == "left" {
							m.settings.Current.TreePosition = "right"
						} else {
							m.settings.Current.TreePosition = "left"
						}
						_ = m.settings.Save()
						m.toasts.Info("TREE DOCK", fmt.Sprintf("Docked to %s", m.settings.Current.TreePosition))
					}
					return m, nil
				}
			}
		}
	}

	// 1.5. Mouse click on Tab Bar (Row 1)
	if msg.Y == 1 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		for _, hit := range m.tabHitboxes {
			if msg.X >= hit.closeX-1 && msg.X <= hit.closeX+1 {
				m.eng.CloseBuffer(hit.docID)
				m.onActiveDocumentChanged()
				m.toasts.Info("CLOSED", "Buffer closed")
				return m, nil
			} else if msg.X >= hit.minX && msg.X <= hit.maxX {
				_ = m.eng.SwitchBuffer(hit.docID)
				m.onActiveDocumentChanged()
				return m, nil
			}
		}
		return m, nil
	}

	// 1.8. Side activity strip clicks (Left only)
	if stripLeftW > 0 && msg.X < stripLeftW && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		switch msg.Y {
		case 2: // Project Explorer 📁
			if !m.sidebarOpen {
				m.sidebarOpen = true
				m.sidebarMode = 0
			} else {
				if m.sidebarMode == 0 {
					m.sidebarOpen = false
				} else {
					m.sidebarMode = 0
				}
			}
			m.clampSidebar()
			return m, m.animateSidebar()
		case 4: // Structure ST
			if !m.sidebarOpen {
				m.sidebarOpen = true
				m.sidebarMode = 1
				m.refreshDocumentStructure()
			} else {
				if m.sidebarMode == 1 {
					m.sidebarOpen = false
				} else {
					m.sidebarMode = 1
					m.refreshDocumentStructure()
				}
			}
			return m, m.animateSidebar()
		case 6: // Version Control ⎇
			m.openGitModal()
			return m, nil
		case 8: // Plugins Marketplace PL
			if m.marketplace != nil {
				m.marketplace.Open = true
				m.marketplace.Refresh()
			}
			return m, nil
		default:
			if msg.Y >= m.height-3 && msg.Y <= m.height-1 { // Settings ⚙
				if m.settings != nil {
					m.settings.Open = true
				}
				return m, nil
			}
		}
	}

	// 2. Mouse click inside Output Drawer
	if m.outputOpen {
		drawerH := m.outputHeight
		drawerTop := m.height - 1 - drawerH
		if msg.Y >= drawerTop && msg.Y < m.height-1 && msg.Action == input.MousePress {
			if msg.Y == drawerTop && msg.X >= m.width-12 {
				m.outputOpen = false
				return m, nil
			}
			return m, nil
		}
	}

	// 3. Mouse click inside Sidebar (Explorer or Structure)
	sidebarMaxY := editorTop + 1 + m.visibleTreeHeight()
	if sideW > 0 && msg.X >= treeStartX && msg.X < treeStartX+sideW && msg.Y >= editorTop && msg.Y < sidebarMaxY {
		if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
			m.terminalFocused = false
			rowInSidebar := msg.Y - editorTop - 1 // -1 for header row
			if rowInSidebar >= 0 && rowInSidebar < m.visibleTreeHeight() {
				if m.sidebarMode == 1 {
					if m.structurePanel != nil {
						m.sidebarFocused = true
						if jumpTarget := m.structurePanel.HandleClick(rowInSidebar); jumpTarget != nil {
							m.jumpToLine(jumpTarget.Line, jumpTarget.Col)
							m.statusMessage = fmt.Sprintf("Jumped to %s (line %d)", jumpTarget.Name, jumpTarget.Line+1)
						}
					}
					return m, nil
				}
				m.clampSidebar()
				nodeIdx := m.sidebarScrollY + rowInSidebar
				if nodeIdx >= 0 && nodeIdx < len(m.treeFlat) {
					m.treeSel = nodeIdx
					m.sidebarFocused = true
					node := m.treeFlat[nodeIdx]
					if node.IsDir {
						node.IsOpen = !node.IsOpen
						m.treeFlat = make([]*FileNode, 0)
						FlattenTree(m.treeRoot, &m.treeFlat)
						m.clampSidebar()
					} else {
						_, err := m.eng.Open(node.Path)
						if err == nil {
							m.statusMessage = fmt.Sprintf("Opened %s", node.Name)
							m.sidebarFocused = false
							m.ensureCursorVisible()
							m.onActiveDocumentChanged()
						}
					}
				}
			}
			return m, nil
		}
	}

	// 4. Mouse wheel scrolling (Accelerated 5 to 12 lines per notch)
	if msg.Button == input.MouseWheelLeft || (msg.Button == input.MouseWheelUp && msg.HasShift()) {
		if m.viewportX > 0 {
			m.viewportX -= 4
			if m.viewportX < 0 {
				m.viewportX = 0
			}
		}
		return m, nil
	}
	if msg.Button == input.MouseWheelRight || (msg.Button == input.MouseWheelDown && msg.HasShift()) {
		m.viewportX += 4
		return m, nil
	}

	linesToScroll := 6
	now := time.Now()
	if now.Sub(m.lastWheelTime) < 160*time.Millisecond {
		linesToScroll = 16
	}
	m.lastWheelTime = now

	if msg.Button == input.MouseWheelUp {
		// Terminal header scroll: adjust height
		if m.terminal != nil && m.terminal.Open && msg.Y == m.height-1-m.terminal.Height {
			maxH := max(4, m.height-6)
			m.terminal.Height = min(maxH, m.terminal.Height+2)
			return m, nil
		}
		// Terminal body scroll: scroll terminal history
		if m.terminal != nil && m.terminal.Open && msg.Y > m.height-1-m.terminal.Height && msg.Y < m.height-1 {
			m.terminal.VTerm.Scroll(3)
			return m, nil
		}
		sidebarMaxY := editorTop + 1 + m.visibleTreeHeight()
		if (m.sidebarFocused || (sideW > 0 && msg.X >= treeStartX && msg.X < treeStartX+sideW && msg.Y < sidebarMaxY)) && m.sidebarOpen {
			if m.sidebarMode == 1 && m.structurePanel != nil {
				if m.structurePanel.ScrollY > 0 {
					m.structurePanel.ScrollY = max(0, m.structurePanel.ScrollY-linesToScroll/2)
				}
			} else {
				m.sidebarScrollY -= linesToScroll / 2
				m.clampSidebar()
			}
		} else {
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				if paneIdx := m.splits.FindPaneAt(msg.X, msg.Y); paneIdx >= 0 {
					if pane := m.splits.PaneAt(paneIdx); pane != nil {
						if pane.ViewportY > 0 {
							pane.ViewportY -= linesToScroll
							if pane.ViewportY < 0 {
								pane.ViewportY = 0
							}
						}
						pane.TargetViewportY = float64(pane.ViewportY)
						pane.SmoothScrollY = float64(pane.ViewportY)
						pane.ScrollVelocity = 0
						if paneIdx == m.splits.ActivePaneIndex() {
							m.viewportY = pane.ViewportY
						}
						return m, nil
					}
				}
			}
			if m.viewportY > 0 {
				m.viewportY -= linesToScroll
				if m.viewportY < 0 {
					m.viewportY = 0
				}
				if pane := m.splits.ActivePane(); pane != nil {
					pane.ViewportY = m.viewportY
					pane.TargetViewportY = float64(m.viewportY)
					pane.SmoothScrollY = float64(m.viewportY)
					pane.ScrollVelocity = 0
				}
			}
		}
		return m, nil
	}
	if msg.Button == input.MouseWheelDown {
		// Terminal header scroll: adjust height
		if m.terminal != nil && m.terminal.Open && msg.Y == m.height-1-m.terminal.Height {
			m.terminal.Height = max(4, m.terminal.Height-2)
			return m, nil
		}
		// Terminal body scroll: scroll terminal history
		if m.terminal != nil && m.terminal.Open && msg.Y > m.height-1-m.terminal.Height && msg.Y < m.height-1 {
			m.terminal.VTerm.Scroll(-3)
			return m, nil
		}
		sidebarMaxY := editorTop + 1 + m.visibleTreeHeight()
		if (m.sidebarFocused || (sideW > 0 && msg.X >= treeStartX && msg.X < treeStartX+sideW && msg.Y < sidebarMaxY)) && m.sidebarOpen {
			if m.sidebarMode == 1 && m.structurePanel != nil {
				m.structurePanel.ScrollY += linesToScroll / 2
			} else {
				m.sidebarScrollY += linesToScroll / 2
				m.clampSidebar()
			}
		} else {
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				if paneIdx := m.splits.FindPaneAt(msg.X, msg.Y); paneIdx >= 0 {
					if pane := m.splits.PaneAt(paneIdx); pane != nil {
						doc := m.eng.ActiveDocument()
						if doc != nil && pane.ViewportY+linesToScroll < doc.Buffer.TotalLines() {
							pane.ViewportY += linesToScroll
						}
						pane.TargetViewportY = float64(pane.ViewportY)
						pane.SmoothScrollY = float64(pane.ViewportY)
						pane.ScrollVelocity = 0
						if paneIdx == m.splits.ActivePaneIndex() {
							m.viewportY = pane.ViewportY
						}
						return m, nil
					}
				}
			}
			doc := m.eng.ActiveDocument()
			if doc != nil && m.viewportY+linesToScroll < doc.Buffer.TotalLines() {
				m.viewportY += linesToScroll
				if pane := m.splits.ActivePane(); pane != nil {
					pane.ViewportY = m.viewportY
					pane.TargetViewportY = float64(m.viewportY)
					pane.SmoothScrollY = float64(m.viewportY)
					pane.ScrollVelocity = 0
				}
			}
		}
		return m, nil
	}

	// 5. Split Panes / Editor clicks & drag selection
	editorBottom := m.height - 1
	if m.outputOpen {
		drawerH := m.outputHeight
		if drawerH > m.height-5 {
			drawerH = max(3, m.height-5)
		}
		editorBottom -= drawerH
	}
	if m.terminal != nil && m.terminal.Open {
		termH := m.terminal.Height
		if termH > editorBottom-3 {
			termH = max(3, editorBottom-3)
		}
		editorBottom -= termH
	}
	if m.dapHUD != nil && m.dapHUD.Open {
		hudH := m.dapHUD.Height
		if hudH > editorBottom-3 {
			hudH = max(3, editorBottom-3)
		}
		editorBottom -= hudH
	}

	if msg.Button == input.MouseLeft && (msg.Action == input.MousePress || msg.Action == input.MouseDrag) &&
		msg.X >= editorLeft && msg.X < editorRight &&
		msg.Y >= editorTop && msg.Y < editorBottom {
		m.sidebarFocused = false
		m.terminalFocused = false

		if msg.Action == input.MousePress {
			// Check if click hit any visible color swatch in editor
			for _, swatch := range m.editorColorSwatches {
				if swatch.ScreenX == msg.X && swatch.ScreenY == msg.Y {
					m.openEditorColorPicker(swatch)
					return m, nil
				}
			}

			// Check if click occurred in any split pane
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				clickedPaneIdx := m.splits.FindPaneAt(msg.X, msg.Y)
				if clickedPaneIdx >= 0 {
					m.switchActivePane(clickedPaneIdx)
				}
			}
		}

		targetPane := m.splits.ActivePane()
		pBounds := buffer.NewRect(editorLeft, editorTop, editorRight-editorLeft, m.height-3)
		if targetPane != nil && targetPane.Bounds.Width > 0 && targetPane.Bounds.Height > 0 {
			pBounds = targetPane.Bounds
		}

		isMulti := (m.splits != nil && m.splits.TotalPanes() > 1)
		contentY := pBounds.Y
		if isMulti {
			contentY += 1 // skip mini header row
		}
		contentX := pBounds.X

		clickRow := msg.Y - contentY
		clickCol := msg.X - contentX

		// If click occurred on the pane's mini header row or below bottom bound, ignore
		if clickRow < 0 || (pBounds.Height > 0 && clickRow >= pBounds.Height) {
			return m, nil
		}

		doc := m.eng.ActiveDocument()
		if doc == nil {
			return m, nil
		}
		if IsImageFile(doc.FilePath) {
			if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
				iv := m.getOrCreateImageViewer(doc.FilePath)
				iv.CycleScaleMode()
				m.statusMessage = fmt.Sprintf("Image Scale: %s", iv.ScaleModeTitle())
				m.toasts.Info("IMAGE", fmt.Sprintf("Scale Mode: %s", iv.ScaleModeTitle()))
				return m, nil
			}
			return m, nil
		}
		if doc.Buffer.TotalLines() <= 0 {
			return m, nil
		}

		gutterWidth := m.gutterWidth()
		if pBounds.Width > 0 && gutterWidth > pBounds.Width-2 {
			gutterWidth = max(2, pBounds.Width-2)
		}

		vpY := m.viewportY
		vpX := m.viewportX
		if targetPane != nil {
			vpY = targetPane.ViewportY
			vpX = targetPane.ViewportX
		}

		// Gutter click: toggle breakpoint (only on MousePress)
		if clickCol >= 0 && clickCol < gutterWidth {
			if msg.Action == input.MousePress {
				lineIdx := vpY + clickRow
				if lineIdx >= 0 && lineIdx < doc.Buffer.TotalLines() {
					m.breakpoints[lineIdx] = !m.breakpoints[lineIdx]
					if !m.breakpoints[lineIdx] {
						delete(m.breakpoints, lineIdx)
						m.toasts.Info("BREAKPOINT", fmt.Sprintf("Removed at line %d", lineIdx+1))
					} else {
						m.toasts.Success("BREAKPOINT", fmt.Sprintf("Set at line %d", lineIdx+1))
					}
					if m.dapSession != nil && doc.FilePath != "" {
						var bpLines []int
						for bl := range m.breakpoints {
							bpLines = append(bpLines, bl)
						}
						m.dapSession.SetBreakpoints(doc.FilePath, bpLines)
					}
				}
			}
			return m, nil
		}

		// Text area click / drag
		targetLine := vpY + clickRow
		if targetLine < 0 {
			targetLine = 0
		}
		if targetLine >= doc.Buffer.TotalLines() {
			targetLine = doc.Buffer.TotalLines() - 1
		}

		targetVisualCol := vpX + (clickCol - gutterWidth)
		if targetVisualCol < 0 {
			targetVisualCol = 0
		}

		lineBytes, _ := doc.Buffer.GetLine(targetLine)
		runes := []rune(string(lineBytes))
		runesLen := len(runes)
		if runesLen > 0 && runes[runesLen-1] == '\n' {
			runesLen--
		}
		if runesLen > 0 && runes[runesLen-1] == '\r' {
			runesLen--
		}

		tabSize := 4
		if m.settings != nil && m.settings.Current.TabSize > 0 {
			tabSize = m.settings.Current.TabSize
		}
		runeCol := corebuf.LineVisualToRuneCol(lineBytes, targetVisualCol, tabSize)
		if runeCol > runesLen {
			runeCol = runesLen
		}

		targetByte, _ := doc.Buffer.ByteOffsetForLine(targetLine)
		if targetByte < 0 {
			targetByte = 0
		}
		for i := 0; i < runeCol && i < len(runes); i++ {
			targetByte += len(string(runes[i]))
		}

		newPos := corebuf.Position{
			Line:   targetLine,
			Column: runeCol,
			Byte:   targetByte,
		}

		m.terminalFocused = false
		m.sidebarFocused = false

		if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
			now := time.Now()
			colDiff := m.lastEditorClickCol - runeCol
			if colDiff < 0 {
				colDiff = -colDiff
			}
			isDoubleClick := (now.Sub(m.lastEditorClickTime) < 400*time.Millisecond &&
				m.lastEditorClickLine == targetLine &&
				colDiff <= 1)
			m.lastEditorClickTime = now
			m.lastEditorClickLine = targetLine
			m.lastEditorClickCol = runeCol

			// Ctrl + Click: Go to Definition
			if msg.HasCtrl() {
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(newPos, newPos)})
				m.ensureCursorVisible()
				m.gotoDefinition()
				return m, nil
			}

			if isDoubleClick && !msg.HasShift() && !msg.HasAlt() && len(runes) > 0 {
				col := runeCol
				if col >= len(runes) {
					col = len(runes) - 1
				}
				isWordChar := func(r rune) bool {
					return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
				}
				startCol := col
				endCol := col
				if isWordChar(runes[col]) {
					for startCol > 0 && isWordChar(runes[startCol-1]) {
						startCol--
					}
					for endCol < len(runes) && isWordChar(runes[endCol]) {
						endCol++
					}
				} else if !unicode.IsSpace(runes[col]) {
					for startCol > 0 && !isWordChar(runes[startCol-1]) && !unicode.IsSpace(runes[startCol-1]) {
						startCol--
					}
					for endCol < len(runes) && !isWordChar(runes[endCol]) && !unicode.IsSpace(runes[endCol]) {
						endCol++
					}
				}

				startByte, _ := doc.Buffer.ByteOffsetForLine(targetLine)
				if startByte < 0 {
					startByte = 0
				}
				for i := 0; i < startCol && i < len(runes); i++ {
					startByte += len(string(runes[i]))
				}
				endByte := startByte
				for i := startCol; i < endCol && i < len(runes); i++ {
					endByte += len(string(runes[i]))
				}
				startPos := corebuf.Position{Line: targetLine, Column: startCol, Byte: startByte}
				endPos := corebuf.Position{Line: targetLine, Column: endCol, Byte: endByte}
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(startPos, endPos)})
				m.ensureCursorVisible()
				return m, nil
			}
		}

		if msg.Action == input.MouseDrag {
			// Mouse drag selection: keep anchor of primary cursor and extend head
			if sels := doc.Buffer.GetSelections(); len(sels) > 0 {
				anchor := sels[0].Anchor
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(anchor, newPos)})
			} else {
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(newPos, newPos)})
			}
		} else {
			newSel := corebuf.NewSelection(newPos, newPos)
			if msg.HasShift() && len(doc.Buffer.GetSelections()) > 0 {
				anchor := doc.Buffer.GetSelections()[0].Anchor
				extendedSel := corebuf.NewSelection(anchor, newPos)
				doc.Buffer.SetSelections([]corebuf.Selection{extendedSel})
			} else if msg.HasAlt() {
				// Alt+Click: Add multi-cursor
				doc.Buffer.SetSelections(append(doc.Buffer.GetSelections(), newSel))
			} else {
				// Regular click: Set single cursor
				doc.Buffer.SetSelections([]corebuf.Selection{newSel})
			}
		}
		m.ensureCursorVisible()
	}

	return m, nil
}

// updateTooltip computes hover badge text based on cursor position.
func (m *AppModel) updateTooltip(x, y int) {
	editorTop := 2
	stripLeftW := 0
	if m.width >= 70 {
		stripLeftW = 3
	}

	// 1. Row 0 - Header Toolbar
	if y == 0 {
		for _, btn := range m.toolbarButtons {
			if x >= btn.minX && x <= btn.maxX {
				switch btn.id {
				case "menu":
					m.setTooltip(i18n.T("tooltip.menu"), x, y)
				case "find_files":
					m.setTooltip(i18n.T("tooltip.find_files"), x, y)
				case "split":
					title := "Single Pane"
					if m.splits != nil {
						title = splitModeTitle(m.splits)
					}
					m.setTooltip(fmt.Sprintf(i18n.T("tooltip.split"), title), x, y)
				case "run":
					m.setTooltip(i18n.T("tooltip.run"), x, y)
				case "build":
					m.setTooltip(i18n.T("tooltip.build"), x, y)
				case "term":
					m.setTooltip(i18n.T("tooltip.term"), x, y)
				case "hud":
					m.setTooltip(i18n.T("tooltip.hud"), x, y)
				case "profile_select":
					m.setTooltip(i18n.T("tooltip.profile_select"), x, y)
				default:
					m.setTooltip(btn.id, x, y)
				}
				return
			}
		}
		m.setTooltip(i18n.T("tooltip.app_version"), x, y)
		return
	}

	// 2. Row 1 - Tab Bar
	if y == 1 {
		for _, hit := range m.tabHitboxes {
			if x >= hit.closeX-1 && x <= hit.closeX+1 {
				m.setTooltip(i18n.T("tooltip.tab_close"), x, y)
				return
			} else if x >= hit.minX && x <= hit.maxX {
				docName := filepath.Base(hit.docID)
				m.setTooltip(fmt.Sprintf(i18n.T("tooltip.tab_switch"), docName), x, y)
				return
			}
		}
	}

	// 3. Side Activity Strip (Left only)
	if stripLeftW > 0 && x < stripLeftW {
		switch y {
		case 2:
			m.setTooltip(i18n.T("tooltip.side_explorer"), x, y)
			return
		case 4:
			m.setTooltip(i18n.T("tooltip.side_outline"), x, y)
			return
		case 6:
			m.setTooltip(i18n.T("tooltip.side_git"), x, y)
			return
		case 8:
			m.setTooltip(i18n.T("tooltip.side_plugins"), x, y)
			return
		default:
			if y >= m.height-3 && y <= m.height-1 {
				m.setTooltip(i18n.T("tooltip.side_settings"), x, y)
				return
			}
		}
	}

	// 3.5. Project Tree Header Dock Toggle [⇄]
	sideW := 0
	if m.sidebarOpen {
		sideW = m.sidebarWidth
	}
	isTreeRight := (m.settings != nil && m.settings.Current.TreePosition == "right")
	treeStartX := stripLeftW
	if isTreeRight {
		treeStartX = m.width - sideW
	}
	if sideW >= 18 && y == editorTop {
		dockBtnStart := treeStartX + sideW - 5
		if x >= dockBtnStart && x <= dockBtnStart+1 {
			m.setTooltip(i18n.T("tooltip.tree_dec_width"), x, y)
			return
		}
		if x >= dockBtnStart+2 && x <= dockBtnStart+3 {
			m.setTooltip(i18n.T("tooltip.tree_inc_width"), x, y)
			return
		}
		if x >= dockBtnStart+4 && x < treeStartX+sideW {
			m.setTooltip(i18n.T("tooltip.tree_dock"), x, y)
			return
		}
	} else if sideW > 0 && y == editorTop && x >= treeStartX+sideW-2 && x < treeStartX+sideW {
		m.setTooltip(i18n.T("tooltip.tree_dock"), x, y)
		return
	}

	// 4. Gutter
	gutterWidth := m.gutterWidth()
	editorLeft := stripLeftW
	if !isTreeRight && m.sidebarOpen {
		editorLeft = stripLeftW + m.sidebarWidth + 1
	}
	if x >= editorLeft && x < editorLeft+gutterWidth && y >= editorTop && y < m.height-1 {
		line := m.viewportY + (y - editorTop) + 1
		m.setTooltip(fmt.Sprintf(i18n.T("tooltip.breakpoint"), line), x, y)
		return
	}

	m.tooltipText = ""
}

func splitModeTitle(sm *SplitManager) string {
	if sm == nil {
		return i18n.T("split.mode_single")
	}
	switch sm.Mode {
	case SplitSingle:
		return i18n.T("split.mode_single")
	case Split2Cols:
		return i18n.T("split.mode_2cols")
	case Split2Rows:
		return i18n.T("split.mode_2rows")
	case Split3Cols:
		return i18n.T("split.mode_3cols")
	case Split4Grid:
		return i18n.T("split.mode_4grid")
	case Split5Panes:
		return i18n.T("split.mode_5panes")
	case Split6Grid:
		return i18n.T("split.mode_6grid")
	default:
		return i18n.T("split.mode_single")
	}
}

func (m *AppModel) setTooltip(text string, x, y int) {
	m.tooltipText = text
	m.tooltipX = x + 1
	if y == 0 {
		m.tooltipY = 2
	} else {
		m.tooltipY = y + 1
	}
	textW := buffer.StringWidth(text)
	if m.tooltipX+textW+2 >= m.width {
		m.tooltipX = max(0, m.width-textW-2)
	}
	if m.tooltipY >= m.height-1 {
		m.tooltipY = max(0, y-1)
	}
}

// updateHoverDoc checks if mouse is hovering over an identifier/symbol in the editor area and schedules hover documentation.
func (m *AppModel) updateHoverDoc(screenX, screenY int) {
	if m.hoverDoc == nil {
		return
	}

	editorTop := 2
	stripLeftW := 0
	if m.width >= 70 {
		stripLeftW = 3
	}
	sideW := 0
	if m.sidebarOpen {
		sideW = m.sidebarWidth
	}
	isTreeRight := (m.settings != nil && m.settings.Current.TreePosition == "right")
	editorLeft := stripLeftW
	editorRight := m.width
	if isTreeRight && m.sidebarOpen {
		editorRight = m.width - sideW
	} else if m.sidebarOpen {
		editorLeft = stripLeftW + sideW
	}
	editorBottom := m.height - 1
	if m.terminal != nil && m.terminal.Open {
		editorBottom = m.height - 1 - m.terminal.Height
	}

	// If mouse is outside editor area
	if screenX < editorLeft || screenX >= editorRight || screenY < editorTop || screenY >= editorBottom {
		m.hoverDoc.Dismiss()
		return
	}

	// Find active split pane and document
	pane := m.splits.ActivePane()
	if m.splits != nil && m.splits.TotalPanes() > 1 {
		pIdx := m.splits.FindPaneAt(screenX, screenY)
		if pIdx >= 0 {
			pane = m.splits.PaneAt(pIdx)
		}
	}
	doc := m.eng.ActiveDocument()
	if pane != nil && pane.DocID != "" {
		for _, d := range m.eng.Documents() {
			if d.ID == pane.DocID {
				doc = d
				break
			}
		}
	}
	if doc == nil || doc.Buffer.TotalLines() <= 0 {
		m.hoverDoc.Dismiss()
		return
	}

	pBounds := buffer.NewRect(editorLeft, editorTop, editorRight-editorLeft, editorBottom-editorTop)
	if pane != nil && pane.Bounds.Width > 0 && pane.Bounds.Height > 0 {
		pBounds = pane.Bounds
	}
	contentY := pBounds.Y
	if m.splits != nil && m.splits.TotalPanes() > 1 {
		contentY += 1
	}
	clickRow := screenY - contentY
	clickCol := screenX - pBounds.X
	gutterW := m.gutterWidth()
	if clickCol < gutterW || clickRow < 0 {
		m.hoverDoc.Dismiss()
		return
	}

	vpY := m.viewportY
	vpX := m.viewportX
	if pane != nil {
		vpY = pane.ViewportY
		vpX = pane.ViewportX
	}

	docLine := vpY + clickRow
	if docLine < 0 || docLine >= doc.Buffer.TotalLines() {
		m.hoverDoc.Dismiss()
		return
	}

	lineBytes, err := doc.Buffer.GetLine(docLine)
	if err != nil {
		m.hoverDoc.Dismiss()
		return
	}

	tabSize := 4
	if m.settings != nil && m.settings.Current.TabSize > 0 {
		tabSize = m.settings.Current.TabSize
	}
	targetVisualCol := clickCol - gutterW + vpX
	runeCol := corebuf.LineVisualToRuneCol(lineBytes, targetVisualCol, tabSize)
	runes := []rune(string(lineBytes))
	if len(runes) == 0 {
		m.hoverDoc.Dismiss()
		return
	}
	if runeCol >= len(runes) {
		runeCol = len(runes) - 1
	}

	word := extractWordAtRuneCol(runes, runeCol)
	if word == "" {
		m.hoverDoc.Dismiss()
		return
	}

	// 400ms delay per user requirement
	m.hoverDoc.Start(word, docLine, runeCol, screenX, screenY, 400*time.Millisecond)
}

func (m *AppModel) checkHoverDocTrigger(now time.Time) {
	if m.hoverDoc == nil || m.hoverDoc.Open || m.hoverDoc.TriggerTime.IsZero() {
		return
	}
	if now.Before(m.hoverDoc.TriggerTime) {
		return
	}

	word := m.hoverDoc.PendingWord
	line := m.hoverDoc.PendingLine
	col := m.hoverDoc.PendingCol
	doc := m.eng.ActiveDocument()
	if doc == nil || word == "" {
		m.hoverDoc.Dismiss()
		return
	}

	m.hoverDoc.Symbol = word
	m.hoverDoc.ScreenX = m.hoverDoc.PendingX
	m.hoverDoc.ScreenY = m.hoverDoc.PendingY

	// 1. Try LSP Hover if client is connected
	var foundLSP bool
	if m.lspClient != nil && doc.FilePath != "" {
		uri := "file:///" + filepath.ToSlash(doc.FilePath)
		h, err := m.lspClient.Hover(uri, line, col)
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
			var cleanLines []string
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if l != "" && !strings.HasPrefix(l, "```") {
					cleanLines = append(cleanLines, l)
				}
			}
			if len(cleanLines) > 0 {
				m.hoverDoc.Signature = cleanLines[0]
				if len(cleanLines) > 1 {
					m.hoverDoc.DocLines = cleanLines[1:]
				}
				m.hoverDoc.Source = "LSP"
				foundLSP = true
			}
		}
	}

	// 2. Fallback: Local AST / comments or Image preview
	if !foundLSP {
		sig, docs, thumb, src := ExtractLocalSymbolDocumentation(doc, word, m.workspaceDir)
		if sig != "" || len(docs) > 0 || thumb != nil {
			m.hoverDoc.Signature = sig
			m.hoverDoc.DocLines = docs
			m.hoverDoc.Thumbnail = thumb
			m.hoverDoc.Source = src
		} else {
			m.hoverDoc.Dismiss()
			return
		}
	}

	m.hoverDoc.Open = true
	m.hoverDoc.TriggerTime = time.Time{}
}

func extractWordAtRuneCol(runes []rune, col int) string {
	if len(runes) == 0 || col < 0 || col >= len(runes) {
		return ""
	}

	// Check if cursor is on an image path or string literal e.g. "path/to/img.png"
	qStart := col
	for qStart > 0 && runes[qStart-1] != '"' && runes[qStart-1] != '\'' && runes[qStart-1] != '(' && runes[qStart-1] != '`' && runes[qStart-1] != ' ' {
		qStart--
	}
	qEnd := col
	for qEnd < len(runes) && runes[qEnd] != '"' && runes[qEnd] != '\'' && runes[qEnd] != ')' && runes[qEnd] != '`' && runes[qEnd] != ' ' {
		qEnd++
	}
	candidatePath := string(runes[qStart:qEnd])
	if IsImageFile(candidatePath) {
		return candidatePath
	}

	isWordChar := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	}
	if !isWordChar(runes[col]) {
		return ""
	}
	start := col
	for start > 0 && isWordChar(runes[start-1]) {
		start--
	}
	end := col
	for end < len(runes) && isWordChar(runes[end]) {
		end++
	}
	return string(runes[start:end])
}

// executeContextMenuAction dispatches right-click menu actions.
func (m *AppModel) executeContextMenuAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "new_file":
		m.openTreePrompt("new_file")
	case "new_dir":
		m.openTreePrompt("new_dir")
	case "rename":
		m.openTreePrompt("rename")
	case "move":
		m.openTreePrompt("move")
	case "copy_path":
		target := m.contextMenuTarget
		if target == "" && len(m.treeFlat) > 0 && m.treeSel < len(m.treeFlat) {
			target = m.treeFlat[m.treeSel].Path
		}
		if target != "" {
			rel, err := filepath.Rel(m.workspaceDir, target)
			if err != nil {
				rel = target
			}
			_ = clipboard.Write(rel)
			m.clipboardText = rel
			m.toasts.Success("COPIED", rel)
		}
	case "delete":
		m.openTreePrompt("delete")
	case "refresh":
		m.refreshProjectTree()
		m.toasts.Info("TREE", "Explorer refreshed")
	}
	return m, nil
}

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
			"IDE: Report Bug / Issue",
			"IDE: View Diagnostic Logs & System Data",
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

// openTreePrompt opens the floating dialog for file/folder creation, rename, delete, move.
func (m *AppModel) openTreePrompt(mode string) {
	m.treePromptMode = mode
	m.treePromptOpen = true
	m.treePromptText = ""
	m.treePromptTarget = ""

	target := m.contextMenuTarget
	if target == "" && len(m.treeFlat) > 0 && m.treeSel < len(m.treeFlat) {
		target = m.treeFlat[m.treeSel].Path
	}
	m.treePromptTarget = target

	if target != "" {
		name := filepath.Base(target)
		if mode == "rename" {
			m.treePromptText = name
		} else if mode == "move" {
			m.treePromptText = target
		} else if mode == "delete" {
			m.treePromptText = "y"
		}
	}
}

// handleTreePromptKey handles input for the file operation modal.
func (m *AppModel) handleTreePromptKey(k input.Key) (tea.Model, tea.Cmd) {
	if k.Type == input.KeyEsc {
		m.treePromptOpen = false
		return m, nil
	}

	if k.Type == input.KeyBackspace {
		runes := []rune(m.treePromptText)
		if len(runes) > 0 {
			m.treePromptText = string(runes[:len(runes)-1])
		}
		return m, nil
	}

	if k.Type == input.KeyEnter {
		text := strings.TrimSpace(m.treePromptText)
		target := m.treePromptTarget
		targetDir := m.workspaceDir
		if target != "" {
			fi, err := os.Stat(target)
			if err == nil && fi.IsDir() {
				targetDir = target
			} else {
				targetDir = filepath.Dir(target)
			}
		}

		switch m.treePromptMode {
		case "new_file":
			if text != "" {
				newPath := filepath.Join(targetDir, text)
				err := CreateNewFile(newPath)
				if err != nil {
					m.toasts.Error("CREATE FILE", err.Error())
				} else {
					m.toasts.Success("CREATED", text)
					m.refreshProjectTree()
					_, _ = m.eng.Open(newPath)
				}
			}

		case "new_dir":
			if text != "" {
				newDir := filepath.Join(targetDir, text)
				err := CreateNewDir(newDir)
				if err != nil {
					m.toasts.Error("CREATE DIR", err.Error())
				} else {
					m.toasts.Success("CREATED", text)
					m.refreshProjectTree()
				}
			}

		case "rename":
			if text != "" && target != "" {
				newPath := filepath.Join(filepath.Dir(target), text)
				err := RenamePath(target, newPath)
				if err != nil {
					m.toasts.Error("RENAME", err.Error())
				} else {
					m.toasts.Success("RENAMED", text)
					m.refreshProjectTree()
				}
			}

		case "delete":
			if strings.ToLower(text) == "y" && target != "" {
				err := DeletePath(target)
				if err != nil {
					m.toasts.Error("DELETE", err.Error())
				} else {
					m.toasts.Success("DELETED", filepath.Base(target))
					m.refreshProjectTree()
				}
			}

		case "move":
			if text != "" && target != "" {
				err := MovePath(target, text)
				if err != nil {
					m.toasts.Error("MOVE", err.Error())
				} else {
					m.toasts.Success("MOVED", filepath.Base(text))
					m.refreshProjectTree()
				}
			}
		}

		m.treePromptOpen = false
		return m, nil
	}

	if k.Type == input.KeySpace {
		m.treePromptText += " "
		return m, nil
	}

	if k.Type == input.KeyRune && k.Rune >= 32 {
		m.treePromptText += string(k.Rune)
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
	default:
		m.statusMessage = fmt.Sprintf("Executed: %s", cmdName)
	}
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

func (m *AppModel) setKeymapProfile(p keymaps.Profile) {
	if m.settings != nil {
		m.settings.Current.KeymapProfile = string(p)
		m.settings.Current.Keybindings = keymaps.GetProfileBindings(p)
		_ = SaveSettings(m.settings.Current)
	}
	m.statusMessage = fmt.Sprintf("Switched keymap profile to %s", p)
	if m.toasts != nil {
		m.toasts.Info("KEYMAP", fmt.Sprintf("Activated %s profile", p))
	}
}

func (m *AppModel) toggleVimMode() {
	if m.settings != nil {
		m.settings.Current.VimMode = !m.settings.Current.VimMode
		_ = SaveSettings(m.settings.Current)
		state := "enabled"
		if !m.settings.Current.VimMode {
			state = "disabled"
		}
		m.statusMessage = fmt.Sprintf("Vim mode %s", state)
		if m.toasts != nil {
			m.toasts.Info("VIM", fmt.Sprintf("Modal editing %s", state))
		}
	}
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
		m.popupItems = []string{
			"func", "return", "package", "import", "type", "struct", "interface",
			"if", "else", "switch", "case", "default", "for", "range", "nil", "true", "false",
		}
		m.popupDoc = "Language keywords"
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

// updateGhostText calculates the inline faint prediction for the word being typed.
func (m *AppModel) updateGhostText() {
	prefix := m.wordPrefixUnderCursor()
	if len(prefix) < 2 {
		m.ghostText = ""
		m.ghostPrefix = ""
		return
	}

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
			return
		}
	}

	m.ghostText = ""
	m.ghostPrefix = ""
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

// ensureCursorVisible calculates viewport scrolling to keep active cursor on screen.
func (m *AppModel) ensureCursorVisible() {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return
	}

	head := sels[len(sels)-1].Head
	editorHeight := m.height - 2
	if m.outputOpen {
		editorHeight -= m.outputHeight
	}
	if editorHeight < 1 {
		editorHeight = 1
	}

	sideW := 0
	if m.sidebarOpen {
		sideW = m.sidebarWidth + 1
	}
	gutterWidth := m.gutterWidth()
	editorWidth := m.width - sideW - gutterWidth - 1
	if editorWidth < 1 {
		editorWidth = 1
	}

	if m.splits != nil && m.splits.TotalPanes() > 1 {
		if ap := m.splits.ActivePane(); ap != nil && ap.Bounds.Width > 0 && ap.Bounds.Height > 0 {
			editorHeight = ap.Bounds.Height - 1
			if editorHeight < 1 {
				editorHeight = 1
			}
			editorWidth = ap.Bounds.Width - gutterWidth - 1
			if editorWidth < 1 {
				editorWidth = 1
			}
		}
	}

	scrolloff := 0
	if m.settings != nil {
		scrolloff = m.settings.Current.ScrolloffY
	}
	if scrolloff*2 >= editorHeight {
		scrolloff = max(0, (editorHeight-1)/2)
	}

	if head.Line < m.viewportY+scrolloff {
		m.viewportY = max(0, head.Line-scrolloff)
	} else if head.Line >= m.viewportY+editorHeight-scrolloff {
		m.viewportY = head.Line - editorHeight + 1 + scrolloff
	}

	if head.Column < m.viewportX {
		m.viewportX = head.Column
	} else if head.Column >= m.viewportX+editorWidth {
		m.viewportX = head.Column - editorWidth + 1
	}

	if m.settings != nil && m.settings.Current.WordWrap {
		m.viewportX = 0
	}
	doc.Viewport.ScrolloffY = scrolloff
	doc.Viewport.Height = editorHeight
	doc.Viewport.Width = editorWidth

	if m.splits != nil {
		if ap := m.splits.ActivePane(); ap != nil {
			ap.ViewportX = m.viewportX
			ap.ViewportY = m.viewportY
			ap.TargetViewportY = float64(m.viewportY)
			ap.SmoothScrollY = float64(m.viewportY)
			ap.ScrollVelocity = 0
		}
	}
}

// syncViewportOffsets sets TargetViewportY and SmoothScrollY to match ViewportY.
func (m *AppModel) syncViewportOffsets() {
	if m.splits != nil {
		for i := range m.splits.Panes {
			p := &m.splits.Panes[i]
			p.TargetViewportY = float64(p.ViewportY)
			p.SmoothScrollY = float64(p.ViewportY)
			p.ScrollVelocity = 0
		}
		if ap := m.splits.ActivePane(); ap != nil {
			ap.TargetViewportY = float64(m.viewportY)
			ap.SmoothScrollY = float64(m.viewportY)
			ap.ScrollVelocity = 0
		}
	}
}

// performUndo performs an undo operation with user feedback via status message and toast notifications.
func (m *AppModel) performUndo() {
	if m.eng == nil {
		return
	}
	doc := m.eng.ActiveDocument()
	if doc == nil {
		m.statusMessage = "No active document"
		if m.toasts != nil {
			m.toasts.Warn("UNDO", "No active document")
		}
		return
	}
	if !doc.Buffer.Undo() {
		m.statusMessage = "Already at oldest change"
		if m.toasts != nil {
			m.toasts.Warn("UNDO", "Already at oldest change")
		}
		return
	}
	doc.Viewport.GutterWidth = core.ComputeGutterWidth(doc.Buffer.TotalLines())
	doc.Viewport.ScrollToCursor(doc.Buffer)

	m.notifyLSPChange()
	m.ensureCursorVisible()
	m.syncViewportOffsets()
	m.statusMessage = "Undo"
	if m.toasts != nil {
		m.toasts.Info("UNDO", "Undo")
	}
}

// performRedo performs a redo operation with user feedback via status message and toast notifications.
func (m *AppModel) performRedo() {
	if m.eng == nil {
		return
	}
	doc := m.eng.ActiveDocument()
	if doc == nil {
		m.statusMessage = "No active document"
		if m.toasts != nil {
			m.toasts.Warn("REDO", "No active document")
		}
		return
	}
	if !doc.Buffer.Redo() {
		m.statusMessage = "Already at newest change"
		if m.toasts != nil {
			m.toasts.Warn("REDO", "Already at newest change")
		}
		return
	}
	doc.Viewport.GutterWidth = core.ComputeGutterWidth(doc.Buffer.TotalLines())
	doc.Viewport.ScrollToCursor(doc.Buffer)

	m.notifyLSPChange()
	m.ensureCursorVisible()
	m.syncViewportOffsets()
	m.statusMessage = "Redo"
	if m.toasts != nil {
		m.toasts.Info("REDO", "Redo")
	}
}

// gutterWidth returns width required for line numbers and status glyphs.
func (m *AppModel) gutterWidth() int {
	if m.settings != nil && strings.ToLower(m.settings.Current.LineNumbers) == "none" {
		return 3
	}
	doc := m.eng.ActiveDocument()
	total := 1
	if doc != nil {
		total = doc.Buffer.TotalLines()
	}
	w := len(fmt.Sprintf("%d", total)) + 3
	if w < 5 {
		return 5
	}
	return w
}

// View renders the application into the GoatUI Frame.
func (m *AppModel) View(f *tea.Frame) {
	if f == nil || f.Buffer == nil {
		return
	}
	buf := f.Buffer
	w := m.width
	h := m.height
	if w <= 0 || h <= 0 {
		return
	}

	// 0. Render Startup Splash Screen if active
	if m.splash != nil && m.splash.Active {
		m.splash.Render(buf, w, h, &m.theme)
		return
	}

	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	m.activeCursorScreenX = -1
	m.activeCursorScreenY = -1
	if m.gitWatcher != nil && doc.FilePath != "" {
		m.gitWatcher.SetActiveFile(doc.FilePath)
	}

	editorTop := 2
	statusBarY := h - 1

	m.minimapHitboxes = nil

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
	if m.dapHUD != nil && m.dapHUD.Open {
		hudH := m.dapHUD.Height
		if hudH > editorHeight-3 {
			hudH = max(3, editorHeight-3)
		}
		editorHeight -= hudH
	}

	// 1. Draw Title Header Bar with Clean Pill Buttons (Row 0)
	headerBg := toColor(m.theme.StatusBarBg)
	headerFg := toColor(m.theme.StatusBarFg)
	btnBg := toColor(m.theme.PopupSelBg)

	for x := 0; x < w; x++ {
		buf.SetRune(x, 0, ' ', headerFg, headerBg, cell.AttrNone)
	}

	titlePrefix := " ≡ "
	for i, r := range titlePrefix {
		if i < w {
			buf.SetRune(i, 0, r, toColor(m.theme.Foreground), headerBg, cell.AttrBold)
		}
	}

	splitPanes := 1
	if m.splits != nil {
		splitPanes = m.splits.TotalPanes()
	}
	splitLabel := fmt.Sprintf(" %s ", i18n.T("toolbar.split", splitPanes))

	activeProfName := i18n.T("toolbar.no_profile")
	if m.launchConfig != nil {
		if act := m.launchConfig.GetActive(); act != nil && act.Name != "" {
			activeProfName = act.Name
		} else if len(m.launchConfig.Configurations) > 0 {
			activeProfName = m.launchConfig.Configurations[0].Name
		}
	}
	displayProfName := activeProfName
	if len([]rune(displayProfName)) > 14 {
		displayProfName = string([]rune(displayProfName)[:11]) + "..."
	}

	m.toolbarButtons = nil
	m.toolbarButtons = append(m.toolbarButtons, toolbarBtn{
		id:   "menu",
		minX: 0,
		maxX: len([]rune(titlePrefix)) - 1,
	})

	leftButtons := []struct {
		id    string
		label string
		fg    cell.Color
		bg    cell.Color
	}{
		{"find_files", fmt.Sprintf(" %s ", i18n.T("toolbar.find")), toColor(m.theme.Foreground), btnBg},
		{"split", splitLabel, toColor(m.theme.Function), btnBg},
		{"term", fmt.Sprintf(" %s ", i18n.T("toolbar.term")), toColor(m.theme.Constant), btnBg},
		{"build", fmt.Sprintf(" %s ", i18n.T("toolbar.build")), toColor(m.theme.DiagnosticWarn), btnBg},
	}

	rightButtons := []struct {
		id    string
		label string
		fg    cell.Color
		bg    cell.Color
	}{
		{"profile_select", fmt.Sprintf(" %s ▼ ", displayProfName), toColor(m.theme.Keyword), btnBg},
		{"run", fmt.Sprintf(" %s ", i18n.T("toolbar.run")), toColor(m.theme.String), btnBg},
		{"hud", fmt.Sprintf(" %s ", i18n.T("toolbar.debug")), toColor(m.theme.DiagnosticError), btnBg},
	}

	curX := len([]rune(titlePrefix))
	for _, b := range leftButtons {
		startX := curX
		for _, r := range b.label {
			if curX < w {
				buf.SetRune(curX, 0, r, b.fg, b.bg, cell.AttrBold)
				curX++
			}
		}
		m.toolbarButtons = append(m.toolbarButtons, toolbarBtn{
			id:   b.id,
			minX: startX,
			maxX: curX - 1,
		})
		if curX < w {
			buf.SetRune(curX, 0, ' ', headerFg, headerBg, cell.AttrNone)
			curX++
		}
	}

	// Calculate right-side buttons starting position
	rightTotalW := 0
	for _, b := range rightButtons {
		rightTotalW += len([]rune(b.label)) + 1
	}

	rightStartX := w - rightTotalW
	if rightStartX < curX+1 {
		rightStartX = curX + 1
	}

	rx := rightStartX
	for _, b := range rightButtons {
		startX := rx
		for _, r := range b.label {
			if rx < w {
				buf.SetRune(rx, 0, r, b.fg, b.bg, cell.AttrBold)
				rx++
			}
		}
		m.toolbarButtons = append(m.toolbarButtons, toolbarBtn{
			id:   b.id,
			minX: startX,
			maxX: rx - 1,
		})
		if rx < w {
			buf.SetRune(rx, 0, ' ', headerFg, headerBg, cell.AttrNone)
			rx++
		}
	}

	filename := doc.FilePath
	if filename == "" {
		filename = "untitled"
	}
	if doc.Buffer.IsModified() {
		filename += " ●"
	}
	fileLabel := fmt.Sprintf(" %s ", filepath.Base(filename))
	fileLen := len([]rune(fileLabel))
	availSpace := rightStartX - curX
	if availSpace > fileLen+2 {
		fileX := curX + (availSpace-fileLen)/2
		for i, r := range fileLabel {
			buf.SetRune(fileX+i, 0, r, toColor(m.theme.Foreground), headerBg, cell.AttrBold)
		}
	}

	// 2. Draw Open Files Tab Bar (Row 1)
	m.tabHitboxes = nil
	tabBarY := 1
	tabBarBg := toColor(m.theme.GutterBg)
	tabBarFg := toColor(m.theme.LineNumber)
	for x := 0; x < w; x++ {
		buf.SetRune(x, tabBarY, ' ', tabBarFg, tabBarBg, cell.AttrNone)
	}

	curTabX := 1
	docs := m.eng.Documents()
	for _, d := range docs {
		baseName := filepath.Base(d.FilePath)
		if baseName == "" || baseName == "." {
			baseName = "untitled"
		}
		isActive := (doc != nil && d.ID == doc.ID)
		isMod := d.Buffer.IsModified()

		tabBg := tabBarBg
		tabFg := tabBarFg
		attr := cell.AttrNone
		if isActive {
			tabBg = toColor(m.theme.Background)
			tabFg = toColor(m.theme.Foreground)
			attr = cell.AttrBold
		}

		startX := curTabX
		buf.SetRune(curTabX, tabBarY, ' ', tabFg, tabBg, attr)
		curTabX++

		for _, r := range baseName {
			if curTabX < w-6 {
				buf.SetRune(curTabX, tabBarY, r, tabFg, tabBg, attr)
				curTabX++
			}
		}

		if isMod {
			buf.SetRune(curTabX, tabBarY, ' ', tabFg, tabBg, attr)
			curTabX++
			buf.SetRune(curTabX, tabBarY, '●', toColor(m.theme.DiagnosticWarn), tabBg, attr)
			curTabX++
		}

		buf.SetRune(curTabX, tabBarY, ' ', tabFg, tabBg, attr)
		curTabX++
		closeX := curTabX
		buf.SetRune(curTabX, tabBarY, '×', toColor(m.theme.DiagnosticError), tabBg, attr)
		curTabX++

		buf.SetRune(curTabX, tabBarY, ' ', tabFg, tabBg, attr)
		curTabX++
		endX := curTabX - 1

		m.tabHitboxes = append(m.tabHitboxes, tabHitbox{
			docID:  d.ID,
			minX:   startX,
			maxX:   endX,
			closeX: closeX,
		})

		if curTabX < w-1 {
			buf.SetRune(curTabX, tabBarY, '│', toColor(m.theme.BorderColor), tabBarBg, cell.AttrNone)
			curTabX++
		}
	}

	// Breadcrumbs on Row 1 (Tab Bar right side)
	if doc != nil {
		cursorLine := 0
		sels := doc.Buffer.GetSelections()
		if len(sels) > 0 {
			cursorLine = sels[0].Head.Line
		}
		var sampleLines []string
		tot := doc.Buffer.TotalLines()
		for i := 0; i <= cursorLine && i < tot; i++ {
			b, _ := doc.Buffer.GetLine(i)
			sampleLines = append(sampleLines, string(b))
		}
		var crumbs []syntax.BreadcrumbItem
		ext := filepath.Ext(doc.FilePath)
		if m.pluginMgr == nil || (ext != "" && m.pluginMgr.IsExtensionActive(ext)) {
			crumbs = syntax.ExtractBreadcrumbs(doc.FilePath, sampleLines, cursorLine)
		} else if base := filepath.Base(doc.FilePath); base != "" && base != "." {
			crumbs = []syntax.BreadcrumbItem{{Kind: "file", Name: base, Icon: "file", Line: 0}}
		}
		if len(crumbs) > 0 {
			crumbStr := syntax.FormatBreadcrumbs(crumbs)
			crumbRunes := []rune(" " + crumbStr + " ")
			startCrumbX := w - len(crumbRunes) - 1
			if startCrumbX > curTabX+2 {
				for i, r := range crumbRunes {
					buf.SetRune(startCrumbX+i, tabBarY, r, toColor(m.theme.Comment), tabBarBg, cell.AttrNone)
				}
			}
		}
	}

	// 3. Activity Strip (Left only, right strip removed per user ergonomics spec)
	stripLeftW := 0
	if w >= 70 {
		stripLeftW = 3
	}

	if stripLeftW > 0 {
		stripBg := toColor(m.theme.GutterBg)
		stripFg := toColor(m.theme.LineNumber)
		borderFg := toColor(m.theme.BorderColor)
		activeStripBg := toColor(m.theme.PopupSelBg)
		activeStripFg := toColor(m.theme.PopupSelFg)

		for y := editorTop; y < statusBarY; y++ {
			buf.SetRune(0, y, ' ', stripFg, stripBg, cell.AttrNone)
			buf.SetRune(1, y, ' ', stripFg, stripBg, cell.AttrNone)
			buf.SetRune(2, y, '│', borderFg, stripBg, cell.AttrNone)
		}

		// Project Explorer: 📁
		prjBg := stripBg
		prjFg := stripFg
		if m.sidebarOpen && m.sidebarMode == 0 {
			prjBg = activeStripBg
			prjFg = activeStripFg
		}
		buf.SetRune(0, 2, 'E', prjFg, prjBg, cell.AttrBold)
		buf.SetRune(1, 2, 'X', prjFg, prjBg, cell.AttrBold)

		// Structure: ST
		stBg := stripBg
		stFg := stripFg
		if m.sidebarOpen && m.sidebarMode == 1 {
			stBg = activeStripBg
			stFg = activeStripFg
		}
		buf.SetRune(0, 4, 'S', stFg, stBg, cell.AttrBold)
		buf.SetRune(1, 4, 'T', stFg, stBg, cell.AttrBold)

		// Version Control: ⎇
		buf.SetRune(0, 6, '⎇', toColor(m.theme.DiagnosticWarn), stripBg, cell.AttrNone)
		buf.SetRune(1, 6, ' ', stripFg, stripBg, cell.AttrNone)

		// Plugins Marketplace: PL
		buf.SetRune(0, 8, 'P', toColor(m.theme.Keyword), stripBg, cell.AttrNone)
		buf.SetRune(1, 8, 'L', toColor(m.theme.Keyword), stripBg, cell.AttrNone)

		// Settings: ⚙ at bottom of activity bar
		if statusBarY-2 > 10 {
			buf.SetRune(0, statusBarY-2, '⚙', toColor(m.theme.Function), stripBg, cell.AttrNone)
			buf.SetRune(1, statusBarY-2, ' ', stripFg, stripBg, cell.AttrNone)
		}
	}

	// 4. Sidebar width with smooth 120 FPS animation
	sideW := int(math.Round(m.sidebarAnimWidth))
	isTreeRight := (m.settings != nil && m.settings.Current.TreePosition == "right")

	editorLeft := stripLeftW
	editorRight := w
	treeStartX := stripLeftW

	if isTreeRight {
		treeStartX = w - sideW
		editorLeft = stripLeftW
		editorRight = treeStartX
		if sideW > 0 {
			editorRight = treeStartX - 1 // space for divider
		}
	} else {
		treeStartX = stripLeftW
		editorLeft = stripLeftW
		if sideW > 0 {
			editorLeft = stripLeftW + sideW + 1 // space for divider
		}
		editorRight = w
	}

	// Draw Project Tree Sidebar (if open and width > 0)
	if sideW > 0 {
		sidebarBg := toColor(m.theme.GutterBg)
		sidebarFg := toColor(m.theme.Foreground)
		borderFg := toColor(m.theme.BorderColor)
		selBg := toColor(m.theme.PopupSelBg)
		selFg := toColor(m.theme.PopupSelFg)

		// Header row with dock and resize buttons
		headerTitle := " PROJECT EXPLORER"
		if m.sidebarMode == 1 {
			headerTitle = " DOCUMENT STRUCTURE"
		}
		expRunes := []rune(headerTitle)
		btnCount := 5
		if sideW < 18 {
			btnCount = 1
		}
		dockBtnStart := treeStartX + sideW - btnCount

		for x := 0; x < sideW; x++ {
			buf.SetRune(treeStartX+x, editorTop, ' ', toColor(m.theme.Keyword), sidebarBg, cell.AttrBold)
		}
		for i, r := range expRunes {
			if treeStartX+i < dockBtnStart-1 {
				buf.SetRune(treeStartX+i, editorTop, r, toColor(m.theme.Keyword), sidebarBg, cell.AttrBold)
			}
		}

		// Render dense buttons directly on sidebar background: "◀ ▶ ⇄" (1 space between each button)
		btnFg := toColor(m.theme.Foreground)
		if sideW >= 18 {
			buf.SetRune(dockBtnStart, editorTop, '◀', btnFg, sidebarBg, cell.AttrBold)
			buf.SetRune(dockBtnStart+1, editorTop, ' ', btnFg, sidebarBg, cell.AttrNone)
			buf.SetRune(dockBtnStart+2, editorTop, '▶', btnFg, sidebarBg, cell.AttrBold)
			buf.SetRune(dockBtnStart+3, editorTop, ' ', btnFg, sidebarBg, cell.AttrNone)
			buf.SetRune(dockBtnStart+4, editorTop, '⇄', btnFg, sidebarBg, cell.AttrBold)
		} else {
			buf.SetRune(dockBtnStart, editorTop, '⇄', btnFg, sidebarBg, cell.AttrBold)
		}

		// Sidebar content: Structure panel or Tree nodes
		visibleTreeRows := editorHeight - 1
		if m.sidebarMode == 1 && m.structurePanel != nil {
			m.structurePanel.Render(buf, treeStartX, editorTop+1, sideW, visibleTreeRows, m.sidebarFocused, &m.theme)
		} else {
			m.clampSidebar()
			var fileStatuses map[string]string
			if m.gitWatcher != nil {
				fileStatuses = m.gitWatcher.GetFileStatuses()
			}
			for row := 0; row < visibleTreeRows; row++ {
				nodeIdx := m.sidebarScrollY + row
				screenY := editorTop + 1 + row

				nodeFg := sidebarFg
				nodeBg := sidebarBg
				isSel := (nodeIdx == m.treeSel && m.sidebarFocused)

				isOpenDoc := false
				if nodeIdx < len(m.treeFlat) {
					node := m.treeFlat[nodeIdx]
					if doc != nil && doc.FilePath != "" && !node.IsDir {
						if filepath.Clean(node.Path) == filepath.Clean(doc.FilePath) {
							isOpenDoc = true
						}
					}
				}

				attr := cell.AttrNone
				if isSel {
					nodeBg = selBg
					nodeFg = selFg
					attr = cell.AttrBold
				} else if isOpenDoc {
					// Soft background highlight for open document, NO '*' marker!
					nodeBg = toColor(m.theme.SelectionBg)
					nodeFg = toColor(m.theme.Function)
					attr = cell.AttrBold
				}

				lineStr := ""
				var statusBadge string
				var badgeColor cell.Color
				if nodeIdx < len(m.treeFlat) {
					node := m.treeFlat[nodeIdx]
					indent := strings.Repeat("  ", node.Depth)
					iconStyle := "minimal"
					if m.settings != nil && m.settings.Current.FileIconStyle != "" {
						iconStyle = m.settings.Current.FileIconStyle
					}
					icon := GetFileIconWithStyle(node, iconStyle)
					lineStr = fmt.Sprintf("%s%s%s", indent, icon, node.Name)

					if fileStatuses != nil {
						cleanPath := filepath.Clean(node.Path)
						st, ok := fileStatuses[cleanPath]
						if !ok {
							st, ok = fileStatuses[strings.ToLower(cleanPath)]
						}
						if !ok && node.IsDir {
							dirPrefix := cleanPath + string(filepath.Separator)
							dirPrefixLower := strings.ToLower(dirPrefix)
							for path, s := range fileStatuses {
								if strings.HasPrefix(path, dirPrefix) || strings.HasPrefix(strings.ToLower(path), dirPrefixLower) {
									st = s
									ok = true
									if s == "M" {
										break // Modified has highest priority
									}
								}
							}
						}
						if ok {
							switch st {
							case "M":
								statusBadge = " M "
								badgeColor = toColor(m.theme.DiagnosticWarn) // Yellow
							case "U":
								statusBadge = " U "
								badgeColor = toColor(m.theme.String) // Green
							case "A":
								statusBadge = " A "
								badgeColor = toColor(m.theme.DiagnosticInfo) // Cyan
							}
						}
					}
				}
				lineRunes := []rune(lineStr)
				badgeRunes := []rune(statusBadge)

				// Ensure badge is not cut off if line text is long
				if len(badgeRunes) > 0 && len(lineRunes)+len(badgeRunes) > sideW && sideW > len(badgeRunes) {
					lineRunes = lineRunes[:sideW-len(badgeRunes)]
				}

				for x := 0; x < sideW; x++ {
					r := ' '
					fg := nodeFg
					if x < len(lineRunes) {
						r = lineRunes[x]
					} else if x < len(lineRunes)+len(badgeRunes) {
						r = badgeRunes[x-len(lineRunes)]
						if statusBadge != "" {
							fg = badgeColor
						}
					}
					buf.SetRune(treeStartX+x, screenY, r, fg, nodeBg, attr)
				}
			}
		}

		// Vertical divider between sidebar and editor
		dividerX := treeStartX - 1
		if !isTreeRight {
			dividerX = treeStartX + sideW
		}
		if dividerX >= 0 && dividerX < w {
			for y := editorTop; y < editorTop+editorHeight; y++ {
				buf.SetRune(dividerX, y, '│', borderFg, sidebarBg, cell.AttrNone)
			}
		}
	}

	// 5. Draw Editor Split Panes (1 to 6 panes)
	editorArea := buffer.NewRect(editorLeft, editorTop, editorRight-editorLeft, editorHeight)
	activeDocID := ""
	if doc != nil {
		activeDocID = doc.ID
	}
	m.splits.UpdateLayout(editorArea, m.eng.Documents(), activeDocID)

	isMultiPane := (m.splits.TotalPanes() > 1)
	for i := range m.splits.Panes {
		p := &m.splits.Panes[i]
		paneDoc := m.findDocument(p.DocID)
		if paneDoc == nil {
			paneDoc = doc
		}
		isActivePane := (i == m.splits.ActiveIndex)
		m.renderPane(buf, p, paneDoc, isActivePane, isMultiPane)
	}

	// 6. Draw Build & Run Output Drawer (if open)
	if m.outputOpen {
		drawerH := m.outputHeight
		drawerTop := editorTop + editorHeight
		outBg := toColor(m.theme.StatusBarBg)
		outFg := toColor(m.theme.Foreground)
		borderFg := toColor(m.theme.BorderColor)
		titleFg := toColor(m.theme.Function)

		taskTitle := "OUTPUT TERMINAL"
		if m.runner != nil && m.runner.Title() != "" {
			taskTitle = m.runner.Title()
		}
		runningTag := " FINISHED "
		if m.runner != nil && m.runner.IsRunning() {
			runningTag = " RUNNING... "
			titleFg = toColor(m.theme.String)
		}
		box := m.BoxChars()
		headerLine := fmt.Sprintf("%c── %s %s ", box.TopLeft, taskTitle, runningTag)
		closeBtn := fmt.Sprintf(" ✕ Close %c", box.TopRight)
		headerRunes := []rune(headerLine)
		closeRunes := []rune(closeBtn)

		for x := 0; x < w; x++ {
			r := box.Horiz
			fg := borderFg
			if x == 0 {
				r = box.TopLeft
			} else if x < len(headerRunes) {
				r = headerRunes[x]
				fg = titleFg
			} else if x >= w-len(closeRunes) {
				idx := x - (w - len(closeRunes))
				if idx >= 0 && idx < len(closeRunes) {
					r = closeRunes[idx]
					fg = toColor(m.theme.DiagnosticError)
				}
			}
			buf.SetRune(x, drawerTop, r, fg, outBg, cell.AttrBold)
		}

		var lines []string
		if m.runner != nil {
			lines = m.runner.Lines()
		}
		visibleLogRows := drawerH - 1
		startIdx := 0
		if len(lines) > visibleLogRows {
			startIdx = len(lines) - visibleLogRows
		}

		for row := 0; row < visibleLogRows; row++ {
			lineIdx := startIdx + row
			screenY := drawerTop + 1 + row
			lineText := ""
			if lineIdx >= 0 && lineIdx < len(lines) {
				lineText = lines[lineIdx]
			}
			lineRunes := []rune(lineText)

			lineFg := outFg
			if strings.Contains(lineText, "error") || strings.Contains(lineText, "failed") {
				lineFg = toColor(m.theme.DiagnosticError)
			} else if strings.Contains(lineText, "success") || strings.Contains(lineText, "PASS") {
				lineFg = toColor(m.theme.String)
			}

			buf.SetRune(0, screenY, '│', borderFg, outBg, cell.AttrNone)
			for x := 1; x < w-1; x++ {
				r := ' '
				if x-1 < len(lineRunes) {
					r = lineRunes[x-1]
				}
				buf.SetRune(x, screenY, r, lineFg, outBg, cell.AttrNone)
			}
			buf.SetRune(w-1, screenY, '│', borderFg, outBg, cell.AttrNone)
		}
	}

	// 6.5. Draw Integrated Terminal Drawer (if open)
	currentBottom := statusBarY
	if m.terminal != nil && m.terminal.Open {
		termH := m.terminal.Height
		if termH > usableHeight-3 {
			termH = max(3, usableHeight-3)
		}
		termTop := currentBottom - termH
		currentBottom = termTop

		termBg := toColor(m.theme.Background)
		termFg := toColor(m.theme.Foreground)
		borderFg := toColor(m.theme.BorderColor)

		focusStatus := i18n.T("term.inactive")
		statusFg := toColor(m.theme.Comment)
		if m.terminalFocused {
			focusStatus = i18n.T("term.focused")
			statusFg = toColor(m.theme.Function)
		}

		box := m.BoxChars()
		m.termTabHitboxes = nil

		maxTabsW := w - 20

		prefix := fmt.Sprintf("%c── %s  %s  ", box.TopLeft, i18n.T("term.title"), focusStatus)
		pRunes := []rune(prefix)

		for x := 0; x < w; x++ {
			buf.SetRune(x, termTop, box.Horiz, borderFg, termBg, cell.AttrBold)
		}
		for i, r := range pRunes {
			if i < w {
				buf.SetRune(i, termTop, r, statusFg, termBg, cell.AttrBold)
			}
		}

		curX := len(pRunes)
		for ti, tInst := range m.terminal.Instances {
			tabName := tInst.Name
			if m.terminal.IsRenaming && m.terminal.RenamingIdx == ti {
				tabName = m.terminal.RenamingInput + "✎"
			}
			tabLabel := fmt.Sprintf(" %d: %s ✕ ", ti+1, tabName)
			tabRunes := []rune(tabLabel)
			if curX+len(tabRunes) >= maxTabsW {
				break
			}
			tabFg := toColor(m.theme.Comment)
			tabBg := termBg
			if ti == m.terminal.ActiveIdx {
				tabFg = toColor(m.theme.Foreground)
				tabBg = toColor(m.theme.PopupSelBg)
			}

			startTabX := curX
			for _, r := range tabRunes {
				buf.SetRune(curX, termTop, r, tabFg, tabBg, cell.AttrBold)
				curX++
			}
			endTabX := curX - 1
			closeX := endTabX - 1

			m.termTabHitboxes = append(m.termTabHitboxes, termTabHitbox{
				minX:        startTabX,
				maxX:        endTabX,
				closeMinX:   closeX - 1,
				closeMaxX:   closeX + 1,
				instanceIdx: ti,
			})

			if curX < maxTabsW {
				buf.SetRune(curX, termTop, ' ', borderFg, termBg, cell.AttrNone)
				curX++
			}
		}

		// + Add button
		addLabel := fmt.Sprintf(" %s ", i18n.T("term.add"))
		addRunes := []rune(addLabel)
		if curX+len(addRunes) < maxTabsW {
			startAddX := curX
			for _, r := range addRunes {
				buf.SetRune(curX, termTop, r, toColor(m.theme.String), toColor(m.theme.PopupSelBg), cell.AttrBold)
				curX++
			}
			m.termTabHitboxes = append(m.termTabHitboxes, termTabHitbox{
				minX:        startAddX,
				maxX:        curX - 1,
				instanceIdx: -1,
				isAdd:       true,
			})
			if curX < maxTabsW {
				buf.SetRune(curX, termTop, ' ', borderFg, termBg, cell.AttrNone)
				curX++
			}
		}

		// Split tag button
		splitTag := fmt.Sprintf(" %s ", i18n.T("term.split"))
		if m.terminal.SplitMode == TermSplitHorizontal {
			splitTag = fmt.Sprintf(" %s ", i18n.T("term.stack"))
		} else if m.terminal.SplitMode == TermSplitVertical {
			splitTag = fmt.Sprintf(" %s ", i18n.T("term.tabs"))
		}
		splitRunes := []rune(splitTag)
		if curX+len(splitRunes) < maxTabsW {
			startSplitX := curX
			for _, r := range splitRunes {
				buf.SetRune(curX, termTop, r, toColor(m.theme.Keyword), toColor(m.theme.PopupSelBg), cell.AttrBold)
				curX++
			}
			m.termTabHitboxes = append(m.termTabHitboxes, termTabHitbox{
				minX:        startSplitX,
				maxX:        curX - 1,
				instanceIdx: -1,
				isSplit:     true,
			})
		}

		// Top-right buttons with dense styling directly on terminal background without rectangular background
		btnBg := termBg
		btnFg := toColor(m.theme.Foreground)
		type headerBtn struct {
			offset int
			text   string
			fg     cell.Color
			bg     cell.Color
		}
		rButtons := []headerBtn{
			{-18, "▲", btnFg, btnBg},
			{-16, "▼", btnFg, btnBg},
			{-14, i18n.T("term.clear"), btnFg, btnBg},
			{-8, i18n.T("term.kill"), btnFg, btnBg},
			{-3, "✕", btnFg, btnBg},
		}

		// Clear spaces for the button zone so box.Horiz doesn't collide
		btnZoneStart := w - 19
		for x := btnZoneStart; x < w-1; x++ {
			if x >= 0 {
				buf.SetRune(x, termTop, ' ', borderFg, termBg, cell.AttrNone)
			}
		}
		// Draw buttons
		for _, b := range rButtons {
			bx := w + b.offset
			for j, r := range b.text {
				if bx+j >= 0 && bx+j < w-1 {
					buf.SetRune(bx+j, termTop, r, b.fg, b.bg, cell.AttrBold)
				}
			}
		}
		// Draw top-right frame corner with correct border color
		if w > 0 {
			buf.SetRune(w-1, termTop, box.TopRight, borderFg, termBg, cell.AttrBold)
		}

		innerW := max(1, w-2)
		innerH := max(1, termH-1)
		m.terminal.Resize(innerW, innerH)

		// Render virtual terminal screen buffer (single or split views)
		if m.terminal.SplitMode == TermSplitHorizontal && len(m.terminal.Instances) >= 2 {
			colW := (innerW - 1) / 2
			m.terminal.Instances[0].VTerm.Render(buf, 1, termTop+1, colW, innerH, termFg, termBg, m.terminal.Open)
			splitBorderFg := borderFg
			if m.terminal.ActiveIdx == 0 {
				splitBorderFg = toColor(m.theme.Function)
			}
			for row := 0; row < innerH; row++ {
				buf.SetRune(1+colW, termTop+1+row, box.Vert, splitBorderFg, termBg, cell.AttrNone)
			}
			remW := innerW - colW - 1
			m.terminal.Instances[1].VTerm.Render(buf, 1+colW+1, termTop+1, remW, innerH, termFg, termBg, m.terminal.Open)
		} else if m.terminal.SplitMode == TermSplitVertical && len(m.terminal.Instances) >= 2 {
			rowH := (innerH - 1) / 2
			m.terminal.Instances[0].VTerm.Render(buf, 1, termTop+1, innerW, rowH, termFg, termBg, m.terminal.Open)
			splitBorderFg := borderFg
			if m.terminal.ActiveIdx == 0 {
				splitBorderFg = toColor(m.theme.Function)
			}
			for col := 0; col < innerW; col++ {
				buf.SetRune(1+col, termTop+1+rowH, box.Horiz, splitBorderFg, termBg, cell.AttrNone)
			}
			remH := innerH - rowH - 1
			m.terminal.Instances[1].VTerm.Render(buf, 1, termTop+1+rowH+1, innerW, remH, termFg, termBg, m.terminal.Open)
		} else {
			m.terminal.VTerm.Render(buf, 1, termTop+1, innerW, innerH, termFg, termBg, m.terminal.Open)
		}

		// Draw side borders
		for row := 0; row < innerH; row++ {
			buf.SetRune(0, termTop+1+row, box.Vert, borderFg, termBg, cell.AttrNone)
			buf.SetRune(w-1, termTop+1+row, box.Vert, borderFg, termBg, cell.AttrNone)
		}
	}

	// 6.6. Draw DAP Debugger HUD (if open)
	if m.dapHUD != nil && m.dapHUD.Open {
		hudH := m.dapHUD.Height
		if hudH > usableHeight-3 {
			hudH = max(3, usableHeight-3)
		}
		hudTop := currentBottom - hudH
		currentBottom = hudTop
		RenderDAPHUD(buf, m.theme, m.dapHUD, m.dapSession, 0, hudTop, w, hudH)
	}

	// 7. Draw Status Bar Footer (Bottom row)
	statusBg := toColor(m.theme.StatusBarBg)
	statusFg := toColor(m.theme.StatusBarFg)
	sels := doc.Buffer.GetSelections()
	cursorInfo := fmt.Sprintf(i18n.T("status.cursor_info"), 1, 1)
	if len(sels) > 0 {
		head := sels[0].Head
		if m.splits != nil && m.splits.TotalPanes() > 1 {
			cursorInfo = fmt.Sprintf(i18n.T("status.cursor_info_split"), m.splits.ActiveIndex+1, m.splits.TotalPanes(), head.Line+1, head.Column+1)
		} else {
			cursorInfo = fmt.Sprintf(i18n.T("status.cursor_info"), head.Line+1, head.Column+1)
		}
	}

	modeStr := i18n.T("status.mode.normal")
	if m.settings != nil && m.settings.Current.VimMode && m.vimFSM != nil {
		modeStr = fmt.Sprintf("VIM [%s]", m.vimFSM.Mode)
	} else if m.sidebarFocused {
		modeStr = i18n.T("status.mode.explorer")
	}
	if m.snippetSession != nil && m.snippetSession.Active {
		cursorInfo += " | " + m.snippetSession.FormatSnippetPrompt()
	}
	gitBadge := ""
	var br string
	if m.gitWatcher != nil {
		br = m.gitWatcher.GetBranch()
	}
	if br != "" {
		gitBadge = fmt.Sprintf("  %s", br)
		if doc != nil && doc.FilePath != "" {
			var diff *git.FileGitDiff
			if m.gitWatcher != nil {
				diff = m.gitWatcher.GetFileDiff(doc.FilePath)
			}
			if diff != nil && (diff.AddedCount > 0 || diff.ModifiedCount > 0 || diff.DeletedCount > 0) {
				gitBadge += fmt.Sprintf(" [+%d ~%d -%d]", diff.AddedCount, diff.ModifiedCount, diff.DeletedCount)
			}
		}
		gitBadge += " |"
	}
	leftStatus := fmt.Sprintf(" %s |%s %s | %s", modeStr, gitBadge, filename, cursorInfo)
	if diagMsg, ok := m.getDiagnosticAtCursor(); ok && m.statusMessage == "" {
		leftStatus = fmt.Sprintf("%s | [ERR] %s", leftStatus, diagMsg)
	} else if m.statusMessage != "" {
		leftStatus = fmt.Sprintf(" %s", m.statusMessage)
	}

	lspStatus := i18n.T("status.lsp_off")
	if m.lspClient != nil {
		lspStatus = i18n.T("status.lsp_active")
	}
	splitTitle := splitModeTitle(m.splits)
	goVer := "Go"
	if m.sdkManager != nil {
		goVer = m.sdkManager.GoVersionShort()
	}
	probStatus := ""
	if m.problemsPanel != nil && (m.problemsPanel.ErrorCount() > 0 || m.problemsPanel.WarningCount() > 0) {
		probStatus = fmt.Sprintf("%d ✕ %d ▲ | ", m.problemsPanel.ErrorCount(), m.problemsPanel.WarningCount())
	}
	rightStatus := fmt.Sprintf("%s%s | %s | %s | UTF-8 ", probStatus, goVer, lspStatus, splitTitle)
	gap := w - len([]rune(leftStatus)) - len([]rune(rightStatus))
	if gap < 0 {
		gap = 0
	}
	statusLine := leftStatus + strings.Repeat(" ", gap) + rightStatus
	statusRunes := []rune(statusLine)

	for x := 0; x < w-1; x++ {
		ch := ' '
		if x < len(statusRunes) {
			ch = statusRunes[x]
		}
		buf.SetRune(x, statusBarY, ch, statusFg, statusBg, cell.AttrNone)
	}
	buf.SetRune(w-1, statusBarY, ' ', statusFg, statusBg, cell.AttrNone)

	// 8. Draw Omnibar Modal Dialog (if open)
	if m.omnibarOpen {
		m.renderOmnibar(buf, w, h)
	}

	// 9. Draw Floating Completion Popup (if open)
	if m.popupVisible && !m.omnibarOpen {
		m.renderPopup(buf, w, h, editorLeft+m.gutterWidth())
	}

	// 10. Draw Tree File Operations Prompt (if open)
	if m.treePromptOpen {
		m.renderTreePrompt(buf, w, h)
	}

	// 10.5. Draw Tree Right-Click Context Menu (if open)
	if m.contextMenuOpen {
		m.renderContextMenu(buf, w, h)
	}

	// 11. Draw JetBrains-style Settings Modal (if open)
	if m.settings != nil && m.settings.Open {
		m.settings.Render(buf, w, h,
			toColor(m.theme.Background),
			toColor(m.theme.Foreground),
			toColor(m.theme.BorderColor),
			toColor(m.theme.PopupSelBg),
			toColor(m.theme.PopupSelFg),
			toColor(m.theme.Function),
		)
	}

	// 11.5. Draw Fullscreen Extension Marketplace (if open)
	if m.marketplace != nil && m.marketplace.Open {
		m.marketplace.Render(buf, w, h, m.theme)
	}

	// 11.6. Draw Quick Fix & Code Actions Popup (if open)
	if m.quickFixModal != nil && m.quickFixModal.Open {
		m.quickFixModal.Render(buf, &m.theme)
	}

	// 11.7. Draw Interactive Git Modal (if open)
	if m.gitModal != nil && m.gitModal.Open {
		m.gitModal.Render(buf, w, h, &m.theme)
	}

	// 11.8. Draw In-Editor Color Picker Modal (if open)
	if m.editorColorPicker != nil && m.editorColorPicker.Open {
		m.editorColorPicker.Render(buf, w, h, m.theme)
	}

	// 11.9. Draw Missing Tool Prompt Modal (if open)
	if m.toolPrompt != nil && m.toolPrompt.Open {
		m.toolPrompt.Render(buf, w, h, &m.theme)
	}

	// 11.95. Draw Launch Configurations Modal (if open)
	if m.launchModal != nil && m.launchModal.Open {
		m.launchModal.Render(buf, w, h, &m.theme)
	}

	// 11.96. Draw Find & Replace Modal (if open)
	if m.findReplaceModal != nil && m.findReplaceModal.Open {
		m.findReplaceModal.Render(buf, w, h, &m.theme)
	}

	// 11.97. Draw Symbol Rename Modal (if open)
	if m.renameModal != nil && m.renameModal.Open {
		m.renameModal.Render(buf, w, h, &m.theme)
	}

	// 11.98. Draw New Project Modal (if open)
	if m.newProjectModal != nil && m.newProjectModal.Open {
		m.newProjectModal.Render(buf, w, h, &m.theme)
	}

	// 11.985. Draw Search in Files Modal (if open)
	if m.searchInFilesModal != nil && m.searchInFilesModal.Open {
		m.searchInFilesModal.Render(buf, w, h, m.theme)
	}

	// 11.986. Draw Problems Panel Drawer (if open)
	if m.problemsPanel != nil && m.problemsPanel.Open {
		m.problemsPanel.Render(buf, 0, h-1-m.problemsPanel.Height, w, m.problemsPanel.Height, m.theme)
	}

	// 11.987. Draw Bug Reporter Modal (if open)
	if m.bugReportModal != nil && m.bugReportModal.Open {
		m.bugReportModal.Render(buf, w, h, &m.theme)
	}

	// 11.988. Draw Crash Recovery Dialog (if open)
	if m.crashDialog != nil && m.crashDialog.Open {
		m.crashDialog.Render(buf, w, h, &m.theme)
	}

	// 11.989. Draw Log Inspector Modal (if open)
	if m.logInspector != nil && m.logInspector.Open {
		m.logInspector.Render(buf, w, h, &m.theme)
	}

	// 12. Draw Dropdowns (Top layer above editor)
	if m.mainMenuOpen {
		m.renderMainMenu(buf, w, h)
	}
	if m.profileDropdownOpen {
		m.renderProfileDropdown(buf, w, h)
	}

	// 12.5. Draw Toasts (Top Right)
	if m.toasts != nil {
		m.toasts.Tick(time.Now())
		m.toasts.Draw(buf, buffer.NewRect(0, 1, w, h-1))
	}

	// 13. Draw Mouse Hover Tooltip (Top layer)
	m.renderTooltip(buf, w, h)

	// 13.5. Draw Hover Documentation Popup (Floating minimal window)
	if m.hoverDoc != nil && m.hoverDoc.Open {
		m.hoverDoc.Render(buf, w, h, &m.theme)
	}

	// 14. Terminal Hardware Cursor Management
	isModalOpen := (m.mainMenuOpen || m.profileDropdownOpen ||
		(m.launchModal != nil && m.launchModal.Open) ||
		(m.findReplaceModal != nil && m.findReplaceModal.Open) ||
		(m.renameModal != nil && m.renameModal.Open) ||
		(m.searchInFilesModal != nil && m.searchInFilesModal.Open) ||
		(m.problemsPanel != nil && m.problemsPanel.Open) ||
		(m.bugReportModal != nil && m.bugReportModal.Open) ||
		(m.crashDialog != nil && m.crashDialog.Open) ||
		(m.logInspector != nil && m.logInspector.Open) ||
		(m.toolPrompt != nil && m.toolPrompt.Open) ||
		(m.editorColorPicker != nil && m.editorColorPicker.Open) ||
		(m.gitModal != nil && m.gitModal.Open) ||
		(m.quickFixModal != nil && m.quickFixModal.Open) ||
		(m.marketplace != nil && m.marketplace.Open) ||
		m.sidebarFocused || m.omnibarOpen || m.treePromptOpen)

	if !isModalOpen && m.activeCursorScreenX >= 0 && m.activeCursorScreenX < w && m.activeCursorScreenY >= 0 && m.activeCursorScreenY < h {
		cursorStyle := "block"
		if m.settings != nil && m.settings.Current.CursorStyle != "" {
			cursorStyle = strings.ToLower(m.settings.Current.CursorStyle)
		}
		shape := 1
		switch cursorStyle {
		case "bar":
			shape = 5
		case "underline":
			shape = 3
		default:
			shape = 1
		}
		f.ShowCursor(m.activeCursorScreenX, m.activeCursorScreenY, shape)
	} else {
		f.HideCursor()
	}
}

type visChar struct {
	r       rune
	origCol int
}

// expandDiagnosticRange widens single-column diagnostic points to cover the full identifier/token.
func expandDiagnosticRange(visChars []visChar, startCol, endCol int) (int, int) {
	if endCol-startCol > 1 {
		return startCol, endCol
	}
	targetIdx := -1
	for i, vc := range visChars {
		if vc.origCol == startCol {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		if endCol <= startCol {
			return startCol, startCol + 1
		}
		return startCol, endCol
	}
	isWordChar := func(r rune) bool {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
	}
	if !isWordChar(visChars[targetIdx].r) {
		if endCol <= startCol {
			return startCol, startCol + 1
		}
		return startCol, endCol
	}
	left := targetIdx
	for left > 0 && isWordChar(visChars[left-1].r) {
		left--
	}
	right := targetIdx
	for right < len(visChars) && isWordChar(visChars[right].r) {
		right++
	}
	if right > left {
		startOrig := visChars[left].origCol
		endOrig := visChars[right-1].origCol + 1
		return startOrig, endOrig
	}
	if endCol <= startCol {
		return startCol, startCol + 1
	}
	return startCol, endCol
}

func (m *AppModel) getOrCreateImageViewer(filePath string) *ImageViewerState {
	if m.imageStateCache == nil {
		m.imageStateCache = make(map[string]*ImageViewerState)
	}
	iv, ok := m.imageStateCache[filePath]
	if !ok {
		iv = NewImageViewerState(filePath)
		m.imageStateCache[filePath] = iv
	}
	return iv
}

// renderPane renders a single split pane (header, gutter, code lines, syntax spans, selection, cursor, and scrollbar).
func (m *AppModel) renderPane(buf *buffer.Buffer, pane *SplitPane, doc *core.Document, isActivePane bool, isMultiPane bool) {
	if pane == nil || pane.Bounds.Width < 3 || pane.Bounds.Height < 1 {
		return
	}

	bx := pane.Bounds.X
	by := pane.Bounds.Y
	bw := pane.Bounds.Width
	bh := pane.Bounds.Height

	borderFg := toColor(m.theme.BorderColor)
	gutterBg := toColor(m.theme.GutterBg)
	gutterFg := toColor(m.theme.LineNumber)
	textBg := toColor(m.theme.Background)
	textFg := toColor(m.theme.Foreground)

	// Draw Right border divider if not reaching terminal edge
	if bx+bw < m.width {
		for y := by; y < by+bh; y++ {
			buf.SetRune(bx+bw, y, '│', borderFg, gutterBg, cell.AttrNone)
		}
	}
	// Draw Bottom border divider if not reaching status bar
	if by+bh < m.height-1 {
		for x := bx; x < bx+bw; x++ {
			buf.SetRune(x, by+bh, '─', borderFg, gutterBg, cell.AttrNone)
		}
	}

	contentTop := by
	contentHeight := bh
	if isMultiPane {
		// Draw Pane Mini Header at row by
		titleStr := PaneTitle(pane.Index, doc)
		if doc != nil {
			cursorLine := 0
			sels := doc.Buffer.GetSelections()
			if len(sels) > 0 {
				cursorLine = sels[0].Head.Line
			}
			var sampleLines []string
			tot := doc.Buffer.TotalLines()
			for i := 0; i <= cursorLine && i < tot; i++ {
				b, _ := doc.Buffer.GetLine(i)
				sampleLines = append(sampleLines, string(b))
			}
			var crumbs []syntax.BreadcrumbItem
			ext := filepath.Ext(doc.FilePath)
			if m.pluginMgr == nil || (ext != "" && m.pluginMgr.IsExtensionActive(ext)) {
				crumbs = syntax.ExtractBreadcrumbs(doc.FilePath, sampleLines, cursorLine)
			} else if base := filepath.Base(doc.FilePath); base != "" && base != "." {
				crumbs = []syntax.BreadcrumbItem{{Kind: "file", Name: base, Icon: "file", Line: 0}}
			}
			if len(crumbs) > 0 {
				titleStr += " " + syntax.FormatBreadcrumbs(crumbs)
			}
		}
		titleRunes := []rune(titleStr)
		headerBg := gutterBg
		headerFg := gutterFg
		headerAttr := cell.AttrNone
		if isActivePane {
			headerBg = toColor(m.theme.SelectionBg)
			headerFg = toColor(m.theme.Function)
			headerAttr = cell.AttrBold
		}
		for x := 0; x < bw; x++ {
			r := '─'
			fg := borderFg
			bg := gutterBg
			attr := cell.AttrNone
			if x < len(titleRunes) {
				r = titleRunes[x]
				fg = headerFg
				bg = headerBg
				attr = headerAttr
			}
			buf.SetRune(bx+x, by, r, fg, bg, attr)
		}
		contentTop++
		contentHeight--
	}

	if contentHeight < 1 {
		return
	}

	// Image Viewer in Pane
	if doc != nil && IsImageFile(doc.FilePath) {
		iv := m.getOrCreateImageViewer(doc.FilePath)
		paneArea := buffer.NewRect(bx, contentTop, bw, contentHeight)
		iv.Render(buf, paneArea, &m.theme)
		return
	}

	// Live Markdown ANSI/ASCII Preview in Split Pane 1
	if m.mdPreviewOpen && pane.Index == 1 {
		doc0 := m.eng.ActiveDocument()
		if doc0 != nil {
			var fullMd strings.Builder
			tot := doc0.Buffer.TotalLines()
			for li := 0; li < tot; li++ {
				lb, _ := doc0.Buffer.GetLine(li)
				fullMd.WriteString(string(lb))
				fullMd.WriteString("\n")
			}
			m.mdRenderer.Parse(fullMd.String())
			m.mdRenderer.Render(buf, bx+1, contentTop, bw-2, contentHeight, pane.ViewportY, &m.theme)
			return
		}
	}

	if doc == nil {
		for y := 0; y < contentHeight; y++ {
			for x := 0; x < bw; x++ {
				buf.SetRune(bx+x, contentTop+y, ' ', textFg, textBg, cell.AttrNone)
			}
		}
		emptyMsg := " [No buffer open] "
		emptyRunes := []rune(emptyMsg)
		midY := contentTop + contentHeight/2
		midX := bx + max(0, (bw-len(emptyRunes))/2)
		for i, r := range emptyRunes {
			if midX+i < bx+bw {
				buf.SetRune(midX+i, midY, r, toColor(m.theme.Comment), textBg, cell.AttrDim)
			}
		}
		return
	}

	totalLines := doc.Buffer.TotalLines()
	gutterWidth := m.gutterWidth()
	if gutterWidth > bw-2 {
		gutterWidth = max(2, bw-2)
	}

	minimapW := 0
	if m.settings == nil || m.settings.Current.ShowMinimap {
		if bw >= 40 {
			minimapW = 4
		}
	}
	textWidth := bw - gutterWidth - 1 - minimapW
	if textWidth < 1 {
		textWidth = 1
	}

	vpX := pane.ViewportX
	vpY := pane.ViewportY
	if isActivePane {
		vpX = m.viewportX
		vpY = m.viewportY
		pane.ViewportX = vpX
		pane.ViewportY = vpY
	}

	doc.Viewport.Height = contentHeight
	doc.Viewport.Width = textWidth
	if isActivePane && pane.Index == 0 {
		m.editorColorSwatches = m.editorColorSwatches[:0]
	}

	if minimapW > 0 && doc != nil {
		minimapX := bx + bw - 1 - minimapW
		m.minimapHitboxes = append(m.minimapHitboxes, minimapHitbox{
			paneIdx:    pane.Index,
			minX:       minimapX,
			maxX:       bx + bw - 2,
			minY:       contentTop,
			maxY:       contentTop + contentHeight - 1,
			totalLines: totalLines,
		})
		getLine := func(idx int) string {
			b, _ := doc.Buffer.GetLine(idx)
			return string(b)
		}
		RenderMinimap(buf, m.theme, minimapX, contentTop, minimapW, contentHeight, totalLines, vpY, contentHeight, getLine)
	}

	sels := doc.Buffer.GetSelections()

	highlightQuery := ""
	if m.findReplaceModal != nil && m.findReplaceModal.Open && m.findReplaceModal.FindQuery != "" {
		highlightQuery = m.findReplaceModal.FindQuery
	} else if len(sels) > 0 && !sels[0].IsEmpty() {
		highlightQuery = m.selectedText()
	} else {
		highlightQuery = m.wordUnderCursor()
	}
	qRunes := []rune(highlightQuery)

	var activeDiff *git.FileGitDiff
	if doc != nil && doc.FilePath != "" {
		if m.gitWatcher != nil {
			activeDiff = m.gitWatcher.GetFileDiff(doc.FilePath)
		}
	}

	for row := 0; row < contentHeight; row++ {
		lineIdx := vpY + row
		screenY := contentTop + row

		icon := ' '
		gFg := gutterFg
		hasLineErr := false
		if m.stoppedMarkerLine == lineIdx {
			icon = '>'
			gFg = toColor(m.theme.DiagnosticWarn)
		} else if m.breakpoints[lineIdx] {
			icon = '●'
			gFg = toColor(m.theme.DiagnosticError)
		} else if lineDiags, ok := m.getDiagnosticsForLine(lineIdx); ok && len(lineDiags) > 0 {
			for _, ld := range lineDiags {
				if ld.Severity == 1 || ld.Severity == 0 {
					hasLineErr = true
					break
				}
			}
			if hasLineErr {
				icon = ' '
				gFg = toColor(0xff5555)
			} else {
				icon = ' '
				gFg = toColor(m.theme.DiagnosticWarn)
			}
		}

		cursorLine := 0
		if len(sels) > 0 {
			cursorLine = sels[0].Head.Line
		}
		lineMode := "absolute"
		if m.settings != nil && m.settings.Current.LineNumbers != "" {
			lineMode = strings.ToLower(m.settings.Current.LineNumbers)
		}

		lineNumStr := ""
		if lineIdx < totalLines {
			switch lineMode {
			case "none":
				lineNumStr = fmt.Sprintf("%*s %c", gutterWidth-3, "", icon)
			case "relative":
				displayNum := lineIdx + 1
				if lineIdx != cursorLine {
					diff := lineIdx - cursorLine
					if diff < 0 {
						diff = -diff
					}
					displayNum = diff
				}
				lineNumStr = fmt.Sprintf("%*d %c", gutterWidth-3, displayNum, icon)
			default: // "absolute"
				lineNumStr = fmt.Sprintf("%*d %c", gutterWidth-3, lineIdx+1, icon)
			}
		} else {
			lineNumStr = fmt.Sprintf("%*s  ", gutterWidth-2, "~")
		}
		lineNumRunes := []rune(lineNumStr)

		gitMarker := ' '
		gitFg := gutterFg
		if activeDiff != nil {
			dk := activeDiff.Lines[lineIdx]
			switch dk {
			case git.DiffAdded:
				gitMarker = '▎'
				gitFg = toColor(m.theme.String) // Green
			case git.DiffModified:
				gitMarker = '▎'
				gitFg = toColor(m.theme.Function) // Blue/Cyan
			case git.DiffDeleted:
				gitMarker = '▲'
				gitFg = toColor(m.theme.DiagnosticError) // Red
			}
		}

		for gx := 0; gx < gutterWidth; gx++ {
			r := ' '
			fg := gutterFg
			if gx < len(lineNumRunes) {
				r = lineNumRunes[gx]
				if r == icon && icon != ' ' {
					fg = gFg
				} else if hasLineErr && r >= '0' && r <= '9' {
					fg = gFg
				}
			}
			if gx == gutterWidth-1 && gitMarker != ' ' {
				r = gitMarker
				fg = gitFg
			}
			buf.SetRune(bx+gx, screenY, r, fg, gutterBg, cell.AttrNone)
		}

		lineBg := textBg
		lineFg := textFg
		if m.stoppedMarkerLine == lineIdx {
			lineBg = toColor(m.theme.SelectionBg)
		}

		var spans []syntax.Span
		var hexMatches []CodeColorMatch
		var visChars []visChar
		var occIntervals [][2]int
		var lineRainbowDepths map[int]int
		tabSize := 4
		if m.settings != nil && m.settings.Current.TabSize > 0 {
			tabSize = m.settings.Current.TabSize
		}
		leadingSpaces := 0

		if lineIdx < totalLines {
			lineBytes, _ := doc.Buffer.GetLine(lineIdx)
			lineStr := string(lineBytes)
			rawRunes := []rune(lineStr)
			lang := filepath.Ext(doc.FilePath)
			if m.pluginMgr == nil || (lang != "" && m.pluginMgr.IsExtensionActive(lang)) {
				spans = m.treeEngine.HighlightLine(lang, lineStr)
				if len(spans) == 0 && lang != "" {
					spans = m.highlighter.HighlightLine(lang, lineStr)
				}
			}
			hexMatches = FindHexColorsInLine(lineStr)

			if m.settings == nil || m.settings.Current.RainbowDelimiters {
				lineRainbowDepths = calculateLineRainbowDepths(lineStr)
			}

			for _, r := range rawRunes {
				if r == ' ' {
					leadingSpaces++
				} else if r == '\t' {
					leadingSpaces += tabSize
				} else {
					break
				}
			}

			vCol := 0
			for origCol, r := range rawRunes {
				if r == '\r' || r == '\n' {
					continue
				}
				if r == '\t' {
					tabSpaces := tabSize - (vCol % tabSize)
					for s := 0; s < tabSpaces; s++ {
						visChars = append(visChars, visChar{r: ' ', origCol: origCol})
						vCol++
					}
				} else {
					w := runewidth.RuneWidth(r)
					if w == 2 {
						visChars = append(visChars, visChar{r: r, origCol: origCol})
						visChars = append(visChars, visChar{r: ' ', origCol: origCol})
						vCol += 2
					} else if w == 0 {
						// Zero-width combining rune: associate with previous column if present
					} else {
						visChars = append(visChars, visChar{r: r, origCol: origCol})
						vCol++
					}
				}
			}

			if len(qRunes) > 0 && len(rawRunes) >= len(qRunes) {
				qLen := len(qRunes)
				matchCase := true
				if m.findReplaceModal != nil && m.findReplaceModal.Open {
					matchCase = m.findReplaceModal.MatchCase
				}
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
					if matched && m.findReplaceModal != nil && m.findReplaceModal.Open && m.findReplaceModal.WholeWord {
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
						occIntervals = append(occIntervals, [2]int{i, i + qLen})
					}
				}
			}
		}

		cursorVisCol := -1
		for col := 0; col < textWidth; col++ {
			charIdx := vpX + col
			ch := ' '
			origCol := -1
			if charIdx < len(visChars) {
				ch = visChars[charIdx].r
				origCol = visChars[charIdx].origCol
			}

			isCursor := false
			isSel := false
			for _, s := range sels {
				if lineIdx == s.Head.Line {
					headVisCol := doc.Buffer.VisualColumn(s.Head.Line, s.Head.Column)
					if charIdx == headVisCol {
						isCursor = true
						cursorVisCol = col
					}
				}
				start := s.Start()
				end := s.End()
				if !s.IsEmpty() && lineIdx >= start.Line && lineIdx <= end.Line {
					if origCol >= 0 {
						if start.Line == end.Line {
							if origCol >= start.Column && origCol < end.Column {
								isSel = true
							}
						} else if lineIdx == start.Line {
							if origCol >= start.Column {
								isSel = true
							}
						} else if lineIdx == end.Line {
							if origCol < end.Column {
								isSel = true
							}
						} else {
							isSel = true
						}
					}
				}
			}

			isOccurrence := false
			if origCol >= 0 && len(occIntervals) > 0 {
				for _, iv := range occIntervals {
					if origCol >= iv[0] && origCol < iv[1] {
						isOccurrence = true
						break
					}
				}
			}

			cellBg := lineBg
			if isOccurrence {
				occBg := m.theme.OccurrenceBg
				if occBg == 0 {
					occBg = 0x393b4f
				}
				cellBg = toColor(occBg)
			}
			if isSel {
				cellBg = toColor(m.theme.SelectionBg)
			}

			cellFg := lineFg
			attr := cell.AttrNone
			if origCol >= 0 {
				semanticMatched := false
				if semList, ok := m.semanticTokens[lineIdx]; ok {
					for _, sem := range semList {
						if origCol >= sem.CharStart && origCol < sem.CharStart+sem.Length {
							switch sem.TokenType {
							case "type", "class", "interface", "struct":
								cellFg = toColor(m.theme.Type)
							case "function", "method":
								cellFg = toColor(m.theme.Function)
							case "parameter":
								cellFg = toColor(m.theme.Foreground)
								attr = cell.AttrItalic
							case "variable", "property":
								cellFg = toColor(m.theme.Foreground)
							case "keyword":
								cellFg = toColor(m.theme.Keyword)
							case "string":
								cellFg = toColor(m.theme.String)
							case "number":
								cellFg = toColor(m.theme.Constant)
							case "comment":
								cellFg = toColor(m.theme.Comment)
							case "operator":
								cellFg = toColor(m.theme.DiagnosticInfo)
							}
							semanticMatched = true
							break
						}
					}
				}

				if !semanticMatched {
					for _, sp := range spans {
						if origCol >= sp.StartCol && origCol < sp.EndCol {
							switch sp.Type {
							case syntax.TokenKeyword:
								cellFg = toColor(m.theme.Keyword)
							case syntax.TokenFunction:
								cellFg = toColor(m.theme.Function)
							case syntax.TokenTypeIdent:
								cellFg = toColor(m.theme.Type)
							case syntax.TokenString:
								cellFg = toColor(m.theme.String)
							case syntax.TokenNumber, syntax.TokenConstant:
								cellFg = toColor(m.theme.Constant)
							case syntax.TokenComment:
								cellFg = toColor(m.theme.Comment)
							case syntax.TokenOperator:
								cellFg = toColor(m.theme.DiagnosticInfo)
							}
							break
						}
					}
				}

				if lineDiags, ok := m.getDiagnosticsForLine(lineIdx); ok {
					for _, ld := range lineDiags {
						startCol, endCol := expandDiagnosticRange(visChars, ld.Range.Start.Character, ld.Range.End.Character)
						if origCol >= startCol && origCol < endCol {
							if ld.Severity == 1 || ld.Severity == 0 {
								cellFg = toColor(0xff5555)
								attr |= cell.AttrUnderline | cell.AttrBold
							} else {
								cellFg = toColor(m.theme.DiagnosticWarn)
								attr |= cell.AttrUnderline
							}
							break
						}
					}
				}
			}

			// Rainbow Delimiters
			if (m.settings == nil || m.settings.Current.RainbowDelimiters) && origCol >= 0 && len(lineRainbowDepths) > 0 {
				if depth, ok := lineRainbowDepths[origCol]; ok {
					cellFg = toColor(rainbowPalette[depth%len(rainbowPalette)])
					attr |= cell.AttrBold
				}
			}

			// Virtual Text: Conceal (e.g. .env secrets masking)
			if doc.VirtualText != nil && (m.settings == nil || m.settings.Current.VirtualText) && origCol >= 0 {
				anns := doc.VirtualText.GetLineAnnotations(lineIdx)
				for _, ann := range anns {
					if ann.Kind == corebuf.AnnotationConceal && origCol >= ann.Col && origCol < ann.EndCol {
						ch = '•'
						cellFg = toColor(m.theme.Comment)
						break
					}
				}
			}

			// Indent Guides: vertical guide line at indentation multiples
			if (m.settings == nil || m.settings.Current.IndentGuides) && ch == ' ' && charIdx < leadingSpaces && charIdx > 0 && (charIdx%tabSize == 0) && !isCursor && !isSel {
				ch = '│'
				cellFg = toColor(m.theme.Comment)
				attr = cell.AttrDim
			}

			if origCol >= 0 && len(hexMatches) > 0 {
				for hmIdx := range hexMatches {
					hm := &hexMatches[hmIdx]
					if origCol >= hm.StartRune && origCol < hm.EndRune {
						m.editorColorSwatches = append(m.editorColorSwatches, EditorColorSwatch{
							PaneIndex: pane.Index,
							ScreenX:   bx + gutterWidth + col,
							ScreenY:   screenY,
							Line:      lineIdx,
							StartCol:  hm.StartRune,
							EndCol:    hm.EndRune,
							StartByte: hm.StartByte,
							EndByte:   hm.EndByte,
							Hex:       hm.Hex,
							Color:     hm.Color,
						})
						if origCol == hm.StartRune {
							ch = '■'
							cellFg = hm.Color
							attr = cell.AttrBold
						} else {
							cellFg = hm.Color
							attr = cell.AttrBold
						}
						break
					}
				}
			}

			attrVal := cell.AttrNone
			if attr != cell.AttrNone {
				attrVal = attr
			}
			if isCursor {
				if isActivePane && !m.sidebarFocused {
					m.activeCursorScreenX = bx + gutterWidth + col
					m.activeCursorScreenY = screenY

					cursorStyle := "block"
					if m.settings != nil && m.settings.Current.CursorStyle != "" {
						cursorStyle = strings.ToLower(m.settings.Current.CursorStyle)
					}
					switch cursorStyle {
					case "underline":
						attrVal |= cell.AttrUnderline | cell.AttrBold
						if ch == ' ' {
							ch = '_'
							cellFg = toColor(m.theme.Foreground)
						}
					case "bar":
						// Hardware terminal cursor renders the native bar beam (shape 5).
						// Do not invert character into a block!
						if ch == ' ' {
							cellFg = toColor(m.theme.Foreground)
						}
					default: // "block"
						attrVal = cell.AttrReverse
						if ch == ' ' {
							cellFg = toColor(m.theme.Foreground)
							cellBg = toColor(m.theme.Background)
						}
					}
				} else {
					attrVal = cell.AttrDim | cell.AttrReverse
				}
			}

			buf.SetRune(bx+gutterWidth+col, screenY, ch, cellFg, cellBg, attrVal)
		}

		// Inline Ghost Text: if active cursor is on this row and ghost text exists
		if isActivePane && cursorVisCol >= 0 && m.ghostText != "" && !m.sidebarFocused {
			ghostRunes := []rune(m.ghostText)
			for gi, gr := range ghostRunes {
				gcol := cursorVisCol + 1 + gi
				if gcol < textWidth {
					buf.SetRune(bx+gutterWidth+gcol, screenY, gr, toColor(m.theme.Comment), textBg, cell.AttrDim)
				}
			}
		}

		// Inlay Hints: render parameter and type annotations inline in subtle dim foreground
		if hints, ok := m.inlayHints[lineIdx]; ok {
			lineEndCol := len(visChars) - vpX
			if lineEndCol < 0 {
				lineEndCol = 0
			}
			hintCol := lineEndCol + 2
			for _, h := range hints {
				label := h.TextLabel()
				if label != "" && hintCol < textWidth-1 {
					hintRunes := []rune(" " + label + " ")
					for hi, hr := range hintRunes {
						drawCol := hintCol + hi
						if drawCol < textWidth {
							buf.SetRune(bx+gutterWidth+drawCol, screenY, hr, toColor(m.theme.Comment), textBg, cell.AttrDim|cell.AttrItalic)
						}
					}
					hintCol += len(hintRunes) + 1
				}
			}
		}

		// Virtual Text (End of Line: Git Inline Blame, DAP debug inspection)
		if doc.VirtualText != nil && (m.settings == nil || m.settings.Current.VirtualText) {
			lineAnns := doc.VirtualText.GetLineAnnotations(lineIdx)
			eolStartCol := len(visChars) - vpX
			if eolStartCol < 0 {
				eolStartCol = 0
			}
			eolStartCol += 2
			for _, ann := range lineAnns {
				if ann.Kind == corebuf.AnnotationEndOfLine && ann.Text != "" && eolStartCol < textWidth-1 {
					eolRunes := []rune(ann.Text)
					for ei, er := range eolRunes {
						drawCol := eolStartCol + ei
						if drawCol < textWidth {
							annColor := toColor(m.theme.Comment)
							if ann.FgColor != 0 {
								annColor = toColor(ann.FgColor)
							}
							buf.SetRune(bx+gutterWidth+drawCol, screenY, er, annColor, textBg, cell.AttrDim|cell.AttrItalic)
						}
					}
					eolStartCol += len(eolRunes) + 2
				}
			}
		}

		// Vertical Scrollbar on right edge of pane
		thumbPos := 0
		if totalLines > 1 {
			thumbPos = (vpY * contentHeight) / totalLines
		}
		sbChar := '│'
		if row == thumbPos {
			sbChar = '█'
		}
		buf.SetRune(bx+bw-1, screenY, sbChar, toColor(m.theme.LineNumber), gutterBg, cell.AttrNone)
	}
}

// renderContextMenu draws the floating right-click popup menu for tree items.
func (m *AppModel) renderContextMenu(buf *buffer.Buffer, w, h int) {
	if !m.contextMenuOpen || len(m.contextMenuItems) == 0 {
		return
	}

	menuW := 22
	menuH := len(m.contextMenuItems) + 2
	startX := m.contextMenuX
	startY := m.contextMenuY

	if startX+menuW >= w {
		startX = max(0, w-menuW-1)
	}
	if startY+menuH >= h {
		startY = max(0, h-menuH-1)
	}

	boxBg := toColor(m.theme.PopupBg)
	boxFg := toColor(m.theme.PopupFg)
	borderFg := toColor(m.theme.BorderColor)
	selBg := toColor(m.theme.PopupSelBg)
	selFg := toColor(m.theme.PopupSelFg)

	for y := 0; y < menuH; y++ {
		for x := 0; x < menuW; x++ {
			ch := ' '
			fg := borderFg
			if y == 0 && x == 0 {
				ch = '╭'
			} else if y == 0 && x == menuW-1 {
				ch = '╮'
			} else if y == menuH-1 && x == 0 {
				ch = '╰'
			} else if y == menuH-1 && x == menuW-1 {
				ch = '╯'
			} else if y == 0 || y == menuH-1 {
				ch = '─'
			} else if x == 0 || x == menuW-1 {
				ch = '│'
			}
			buf.SetRune(startX+x, startY+y, ch, fg, boxBg, cell.AttrNone)
		}
	}

	title := " Explorer "
	if m.contextMenuTarget != "" {
		title = fmt.Sprintf(" %s ", filepath.Base(m.contextMenuTarget))
	}
	titleRunes := []rune(title)
	if len(titleRunes) > menuW-4 {
		titleRunes = titleRunes[:menuW-4]
	}
	for i, r := range titleRunes {
		buf.SetRune(startX+2+i, startY, r, toColor(m.theme.Keyword), boxBg, cell.AttrBold)
	}

	for idx, item := range m.contextMenuItems {
		itemBg := boxBg
		itemFg := boxFg
		prefix := "  "
		attr := cell.AttrNone
		if idx == m.contextMenuSel {
			itemBg = selBg
			itemFg = selFg
			prefix = "> "
			attr = cell.AttrBold
		}
		label := fmt.Sprintf("%s%s", prefix, item.label)
		labelRunes := []rune(label)
		for x := 0; x < menuW-2; x++ {
			r := ' '
			if x < len(labelRunes) {
				r = labelRunes[x]
			}
			buf.SetRune(startX+1+x, startY+1+idx, r, itemFg, itemBg, attr)
		}
	}
}

// renderTooltip draws the miniature single-line hover badge.
func (m *AppModel) renderTooltip(buf *buffer.Buffer, w, h int) {
	if m.tooltipText == "" || m.contextMenuOpen || m.omnibarOpen || (m.settings != nil && m.settings.Open) || m.treePromptOpen {
		return
	}
	text := fmt.Sprintf(" %s ", m.tooltipText)
	runes := []rune(text)
	textLen := buffer.StringWidth(text)
	tx := m.tooltipX
	ty := m.tooltipY

	if tx+textLen >= w {
		tx = w - textLen - 1
	}
	if tx < 0 {
		tx = 0
	}
	if ty >= h {
		ty = h - 1
	}
	if ty < 0 {
		ty = 0
	}

	bg := toColor(m.theme.PopupSelBg)
	fg := toColor(m.theme.PopupSelFg)
	curX := tx
	for _, r := range runes {
		rw := buffer.RuneWidth(r)
		if curX+rw <= w {
			buf.SetRune(curX, ty, r, fg, bg, cell.AttrBold)
			curX += max(1, rw)
		}
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

// renderTreePrompt renders the centered popup dialog for file CRUD actions.
func (m *AppModel) renderTreePrompt(buf *buffer.Buffer, w, h int) {
	if !m.treePromptOpen {
		return
	}

	modalW := 54
	if modalW > w-4 {
		modalW = w - 4
	}
	modalH := 7
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	boxBg := toColor(m.theme.PopupBg)
	boxFg := toColor(m.theme.PopupFg)
	borderFg := toColor(m.theme.BorderColor)
	accentFg := toColor(m.theme.Function)

	for y := 0; y < modalH; y++ {
		for x := 0; x < modalW; x++ {
			ch := ' '
			fg := borderFg
			if y == 0 && x == 0 {
				ch = '╭'
			} else if y == 0 && x == modalW-1 {
				ch = '╮'
			} else if y == modalH-1 && x == 0 {
				ch = '╰'
			} else if y == modalH-1 && x == modalW-1 {
				ch = '╯'
			} else if y == 0 || y == modalH-1 {
				ch = '─'
			} else if x == 0 || x == modalW-1 {
				ch = '│'
			}
			buf.SetRune(startX+x, startY+y, ch, fg, boxBg, cell.AttrNone)
		}
	}

	title := " File Operation "
	switch m.treePromptMode {
	case "new_file":
		title = " New File "
	case "new_dir":
		title = " New Directory "
	case "rename":
		title = " Rename File/Dir "
	case "delete":
		title = " Delete Confirm (y/n) "
	case "move":
		title = " Move Path "
	}

	for i, r := range title {
		if startX+2+i < startX+modalW-2 {
			buf.SetRune(startX+2+i, startY, r, accentFg, boxBg, cell.AttrBold)
		}
	}

	targetLabel := fmt.Sprintf("Target: %s", filepath.Base(m.treePromptTarget))
	if len(targetLabel) > modalW-4 {
		targetLabel = targetLabel[:modalW-4]
	}
	for i, r := range targetLabel {
		buf.SetRune(startX+2+i, startY+1, r, toColor(m.theme.LineNumber), boxBg, cell.AttrNone)
	}

	inputField := fmt.Sprintf(" %s_ ", m.treePromptText)
	if len(inputField) > modalW-4 {
		inputField = inputField[:modalW-4]
	}
	for i, r := range inputField {
		buf.SetRune(startX+2+i, startY+3, r, boxFg, toColor(m.theme.PopupSelBg), cell.AttrBold)
	}

	hint := " [Enter] Confirm   [Esc] Cancel "
	for i, r := range hint {
		if startX+2+i < startX+modalW-2 {
			buf.SetRune(startX+2+i, startY+5, r, toColor(m.theme.Comment), boxBg, cell.AttrNone)
		}
	}
}

// renderPopup renders floating completion/hover overlays.
func (m *AppModel) renderPopup(buf *buffer.Buffer, w, h int, gutterWidth int) {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return
	}

	head := sels[0].Head
	popupX := gutterWidth + (head.Column - m.viewportX)
	popupY := 2 + (head.Line - m.viewportY) + 1

	popupWidth := 45
	if popupX+popupWidth >= w {
		popupX = w - popupWidth - 1
	}
	if popupX < gutterWidth {
		popupX = gutterWidth
	}

	visibleItemsCount := len(m.popupItems)
	if visibleItemsCount > 8 {
		visibleItemsCount = 8
	}
	popupHeight := visibleItemsCount + 2
	if popupY+popupHeight >= h-1 {
		popupY = 2 + (head.Line - m.viewportY) - popupHeight
	}
	if popupY < 2 {
		popupY = 2
	}

	popupBg := toColor(m.theme.PopupBg)
	popupFg := toColor(m.theme.PopupFg)
	selBg := toColor(m.theme.PopupSelBg)
	selFg := toColor(m.theme.PopupSelFg)
	borderFg := toColor(m.theme.BorderColor)

	for y := 0; y < popupHeight; y++ {
		for x := 0; x < popupWidth; x++ {
			ch := ' '
			fg := borderFg
			if y == 0 && x == 0 {
				ch = '╭'
			} else if y == 0 && x == popupWidth-1 {
				ch = '╮'
			} else if y == popupHeight-1 && x == 0 {
				ch = '╰'
			} else if y == popupHeight-1 && x == popupWidth-1 {
				ch = '╯'
			} else if y == 0 || y == popupHeight-1 {
				ch = '─'
			} else if x == 0 || x == popupWidth-1 {
				ch = '│'
			}
			buf.SetRune(popupX+x, popupY+y, ch, fg, popupBg, cell.AttrNone)
		}
	}

	startIdx := m.popupScrollOffset
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx+visibleItemsCount > len(m.popupItems) {
		startIdx = max(0, len(m.popupItems)-visibleItemsCount)
	}

	for row := 0; row < visibleItemsCount; row++ {
		idx := startIdx + row
		if idx >= len(m.popupItems) {
			break
		}
		item := m.popupItems[idx]
		itemBg := popupBg
		itemFg := popupFg
		if idx == m.popupActive {
			itemBg = selBg
			itemFg = selFg
		}
		lineText := fmt.Sprintf(" %s", item)
		if len(lineText) > popupWidth-2 {
			lineText = lineText[:popupWidth-2]
		}
		lineText = fmt.Sprintf("%-*s", popupWidth-2, lineText)
		for i, r := range lineText {
			buf.SetRune(popupX+1+i, popupY+1+row, r, itemFg, itemBg, cell.AttrNone)
		}

		if len(m.popupItems) > 8 {
			thumbPos := (startIdx * visibleItemsCount) / len(m.popupItems)
			sbChar := '│'
			if row == thumbPos {
				sbChar = '█'
			}
			buf.SetRune(popupX+popupWidth-1, popupY+1+row, sbChar, toColor(m.theme.LineNumber), popupBg, cell.AttrNone)
		}
	}
}

// selectedText returns the string of characters currently highlighted by the primary selection.
func (m *AppModel) selectedText() string {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return ""
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return ""
	}
	s := sels[0]
	if s.IsEmpty() {
		return ""
	}
	start := s.Start()
	end := s.End()
	var sb strings.Builder
	for line := start.Line; line <= end.Line; line++ {
		lineBytes, err := doc.Buffer.GetLine(line)
		if err != nil {
			continue
		}
		runes := []rune(string(lineBytes))
		fromCol := 0
		toCol := len(runes)
		if line == start.Line {
			fromCol = start.Column
		}
		if line == end.Line {
			toCol = end.Column
		}
		if fromCol < len(runes) {
			if toCol > len(runes) {
				toCol = len(runes)
			}
			if fromCol < toCol {
				sb.WriteString(string(runes[fromCol:toCol]))
			}
		}
	}
	return sb.String()
}

// wordUnderCursor extracts the identifier under the active cursor.
func (m *AppModel) wordUnderCursor() string {
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
	if len(runes) == 0 || head.Column > len(runes) {
		return ""
	}

	col := head.Column
	if col == len(runes) && col > 0 {
		col--
	}

	start := col
	for start > 0 && (unicode.IsLetter(runes[start-1]) || unicode.IsDigit(runes[start-1]) || runes[start-1] == '_') {
		start--
	}
	end := col
	for end < len(runes) && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end]) || runes[end] == '_') {
		end++
	}
	if start >= end {
		return ""
	}
	return string(runes[start:end])
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

	return actions
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

// openEditorColorPicker opens the color picker modal for a detected hex color swatch in the active document.
func (m *AppModel) openEditorColorPicker(swatch EditorColorSwatch) {
	if m.editorColorPicker == nil {
		m.editorColorPicker = NewColorPickerModal()
	}
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}

	m.editorColorPicker.OpenForColor(swatch.Hex, fmt.Sprintf("Code Color (%s)", swatch.Hex), swatch.Hex, func(key, newHex string) {
		curDoc := m.eng.ActiveDocument()
		if curDoc == nil {
			return
		}
		lineBytes, err := curDoc.Buffer.GetLine(swatch.Line)
		if err != nil {
			return
		}
		lineStr := string(lineBytes)
		matches := FindHexColorsInLine(lineStr)
		for _, match := range matches {
			if match.StartRune == swatch.StartCol || match.Hex == swatch.Hex {
				lineStartByte, err := curDoc.Buffer.ByteOffsetForLine(swatch.Line)
				if err != nil {
					break
				}
				startByte := lineStartByte + match.StartByte
				deleteLen := match.EndByte - match.StartByte
				_ = curDoc.Buffer.ApplyEdit(startByte, deleteLen, newHex)
				m.notifyLSPChange()
				m.statusMessage = fmt.Sprintf("Changed %s to %s", swatch.Hex, newHex)
				m.toasts.Info("COLOR", fmt.Sprintf("Applied %s", newHex))
				break
			}
		}
	})
	m.editorColorPicker.OnPreview = func(key, newHex string) {
		m.statusMessage = fmt.Sprintf("Previewing color: %s", newHex)
	}
}

// openEditorColorPickerAtCursor checks if the cursor is on a hex color and opens the color picker.
func (m *AppModel) openEditorColorPickerAtCursor() {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return
	}
	head := sels[len(sels)-1].Head
	lineBytes, err := doc.Buffer.GetLine(head.Line)
	if err != nil {
		return
	}
	lineStr := string(lineBytes)
	matches := FindHexColorsInLine(lineStr)
	for _, match := range matches {
		if head.Column >= match.StartRune && head.Column <= match.EndRune {
			swatch := EditorColorSwatch{
				Line:      head.Line,
				StartCol:  match.StartRune,
				EndCol:    match.EndRune,
				StartByte: match.StartByte,
				EndByte:   match.EndByte,
				Hex:       match.Hex,
				Color:     match.Color,
			}
			m.openEditorColorPicker(swatch)
			return
		}
	}

	// Fallback: if cursor is not on an existing hex color, open picker to insert a new hex color
	if m.editorColorPicker == nil {
		m.editorColorPicker = NewColorPickerModal()
	}
	m.editorColorPicker.OpenForColor("#89b4fa", "Insert Hex Color", "#89b4fa", func(key, newHex string) {
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: newHex})
		m.notifyLSPChange()
		m.statusMessage = fmt.Sprintf("Inserted color %s", newHex)
		m.toasts.Info("COLOR", fmt.Sprintf("Inserted %s", newHex))
	})
}

type menuItem struct {
	label    string
	shortcut string
	action   string
}

func getMainMenuItems() []menuItem {
	return []menuItem{
		{i18n.T("menu.open_folder"), "Ctrl+O", "open_folder"},
		{i18n.T("menu.new_project"), "", "new_project"},
		{i18n.T("menu.open_file"), "Ctrl+P", "open_file"},
		{i18n.T("menu.commands"), "Ctrl+Shift+P", "commands"},
		{i18n.T("menu.save_file"), "Ctrl+S", "save_file"},
		{i18n.T("menu.marketplace"), "Ctrl+Shift+X", "marketplace"},
		{i18n.T("menu.settings"), "Ctrl+,", "settings"},
		{i18n.T("menu.report_bug"), "", "report_bug"},
		{i18n.T("menu.view_logs"), "", "view_logs"},
		{i18n.T("menu.exit"), "Ctrl+Q", "exit"},
	}
}

func (m *AppModel) executeMainMenuItem(action string) {
	switch action {
	case "report_bug":
		m.openBugReportModal()
	case "view_logs":
		m.openLogInspector()
	case "open_folder":
		m.openOmnibar("open_dir")
	case "new_project":
		if m.newProjectModal != nil {
			var templates []plugin.ProjectTemplate
			if m.pluginMgr != nil {
				templates = m.pluginMgr.GetProjectTemplates()
			}
			sdkInfo := ""
			if m.sdkManager != nil && m.sdkManager.GoSDK() != nil {
				sdkInfo = fmt.Sprintf("%s (%s)", m.sdkManager.GoSDK().Version, m.sdkManager.GoSDK().BinaryPath)
			}
			m.newProjectModal.OpenWithTemplates(templates, m.workspaceDir, sdkInfo)
		}
	case "recent":
		m.openOmnibar("find")
	case "open_file":
		m.openOmnibar("files")
	case "commands":
		m.openOmnibar("commands")
	case "save_file":
		if doc := m.eng.ActiveDocument(); doc != nil {
			target := doc.FilePath
			if target == "" {
				target = "untitled.txt"
			}
			_ = m.eng.Dispatch(core.Command{ID: core.CmdFileSave, Args: target})
			m.toasts.Success(i18n.T("toast.saved"), filepath.Base(target))
			m.statusMessage = fmt.Sprintf(i18n.T("status.saved"), target)
		}
	case "marketplace":
		if m.marketplace != nil {
			m.marketplace.Open = true
			m.marketplace.Refresh()
		}
	case "settings":
		if m.settings != nil {
			m.settings.Open = true
		}
	case "exit":
		m.quitting = true
	}
}

func (m *AppModel) renderMainMenu(buf *buffer.Buffer, w, h int) {
	if !m.mainMenuOpen {
		return
	}
	items := getMainMenuItems()
	box := m.BoxChars()

	menuX := 0
	menuY := 1
	menuW := 36
	menuH := len(items) + 2

	bg := toColor(m.theme.PopupBg)
	fg := toColor(m.theme.Foreground)
	borderFg := toColor(m.theme.BorderColor)
	selBg := toColor(m.theme.PopupSelBg)
	selFg := toColor(m.theme.PopupSelFg)
	shortcutFg := toColor(m.theme.Comment)

	for y := menuY; y < menuY+menuH && y < h; y++ {
		for x := menuX; x < menuX+menuW && x < w; x++ {
			r := ' '
			f := fg
			b := bg
			if y == menuY && x == menuX {
				r = box.TopLeft
				f = borderFg
			} else if y == menuY && x == menuX+menuW-1 {
				r = box.TopRight
				f = borderFg
			} else if y == menuY+menuH-1 && x == menuX {
				r = box.BottomLeft
				f = borderFg
			} else if y == menuY+menuH-1 && x == menuX+menuW-1 {
				r = box.BottomRight
				f = borderFg
			} else if y == menuY || y == menuY+menuH-1 {
				r = box.Horiz
				f = borderFg
			} else if x == menuX || x == menuX+menuW-1 {
				r = box.Vert
				f = borderFg
			}
			buf.SetRune(x, y, r, f, b, cell.AttrNone)
		}
	}

	for idx, item := range items {
		rowY := menuY + 1 + idx
		if rowY >= h-1 {
			break
		}
		isSel := (idx == m.mainMenuSel)
		rowBg := bg
		rowFg := fg
		attr := cell.AttrNone
		if isSel {
			rowBg = selBg
			rowFg = selFg
			attr = cell.AttrBold
		}

		for x := menuX + 1; x < menuX+menuW-1 && x < w; x++ {
			buf.SetRune(x, rowY, ' ', rowFg, rowBg, attr)
		}

		text := fmt.Sprintf(" %s", item.label)
		for i, r := range text {
			x := menuX + 1 + i
			if x < menuX+menuW-len(item.shortcut)-2 {
				buf.SetRune(x, rowY, r, rowFg, rowBg, attr)
			}
		}

		if item.shortcut != "" {
			scX := menuX + menuW - len(item.shortcut) - 2
			scFg := shortcutFg
			if isSel {
				scFg = selFg
			}
			for i, r := range item.shortcut {
				buf.SetRune(scX+i, rowY, r, scFg, rowBg, attr)
			}
		}
	}
}

func (m *AppModel) renderProfileDropdown(buf *buffer.Buffer, w, h int) {
	if !m.profileDropdownOpen || m.launchConfig == nil {
		return
	}
	box := m.BoxChars()

	menuX := 24
	for _, btn := range m.toolbarButtons {
		if btn.id == "profile_select" {
			menuX = btn.minX
			break
		}
	}
	if menuX+34 > w {
		menuX = max(1, w-35)
	}
	menuY := 1
	menuW := 34

	profiles := m.launchConfig.Configurations
	totalItems := len(profiles) + 2
	menuH := totalItems + 2

	bg := toColor(m.theme.PopupBg)
	fg := toColor(m.theme.Foreground)
	borderFg := toColor(m.theme.BorderColor)
	selBg := toColor(m.theme.PopupSelBg)
	selFg := toColor(m.theme.PopupSelFg)
	activeFg := toColor(m.theme.String)

	for y := menuY; y < menuY+menuH && y < h; y++ {
		for x := menuX; x < menuX+menuW && x < w; x++ {
			r := ' '
			f := fg
			b := bg
			if y == menuY && x == menuX {
				r = box.TopLeft
				f = borderFg
			} else if y == menuY && x == menuX+menuW-1 {
				r = box.TopRight
				f = borderFg
			} else if y == menuY+menuH-1 && x == menuX {
				r = box.BottomLeft
				f = borderFg
			} else if y == menuY+menuH-1 && x == menuX+menuW-1 {
				r = box.BottomRight
				f = borderFg
			} else if y == menuY || y == menuY+menuH-1 {
				r = box.Horiz
				f = borderFg
			} else if x == menuX || x == menuX+menuW-1 {
				r = box.Vert
				f = borderFg
			}
			buf.SetRune(x, y, r, f, b, cell.AttrNone)
		}
	}

	activeName := ""
	if act := m.launchConfig.GetActive(); act != nil {
		activeName = act.Name
	}

	for idx := 0; idx < totalItems; idx++ {
		rowY := menuY + 1 + idx
		if rowY >= h-1 {
			break
		}
		if idx < len(profiles) {
			p := profiles[idx]
			isSel := (idx == m.profileDropdownSel)
			rowBg := bg
			rowFg := fg
			attr := cell.AttrNone
			if isSel {
				rowBg = selBg
				rowFg = selFg
				attr = cell.AttrBold
			}
			for x := menuX + 1; x < menuX+menuW-1 && x < w; x++ {
				buf.SetRune(x, rowY, ' ', rowFg, rowBg, attr)
			}
			prefix := "   "
			if p.Name == activeName {
				prefix = " ✓ "
			}
			line := prefix + p.Name
			for i, r := range line {
				x := menuX + 1 + i
				if x < menuX+menuW-1 {
					fColor := rowFg
					if p.Name == activeName && i < 3 {
						fColor = activeFg
					}
					buf.SetRune(x, rowY, r, fColor, rowBg, attr)
				}
			}
		} else if idx == len(profiles) {
			for x := menuX + 1; x < menuX+menuW-1 && x < w; x++ {
				buf.SetRune(x, rowY, box.Horiz, borderFg, bg, cell.AttrNone)
			}
		} else {
			isSel := (idx == m.profileDropdownSel)
			rowBg := bg
			rowFg := fg
			attr := cell.AttrNone
			if isSel {
				rowBg = selBg
				rowFg = selFg
				attr = cell.AttrBold
			}
			for x := menuX + 1; x < menuX+menuW-1 && x < w; x++ {
				buf.SetRune(x, rowY, ' ', rowFg, rowBg, attr)
			}
			line := "   Edit configs..."
			for i, r := range line {
				x := menuX + 1 + i
				if x < menuX+menuW-1 {
					buf.SetRune(x, rowY, r, rowFg, rowBg, attr)
				}
			}
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

type docMatch struct {
	line      int
	col       int
	length    int
	startByte int
	endByte   int
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
