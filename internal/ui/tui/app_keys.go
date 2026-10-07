package tui

import (
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core"
	"tahr/internal/core/ai"
	"tahr/internal/core/clipboard"
	"tahr/internal/core/keymaps"
)

// matchKey checks whether a pressed key corresponds to a given latin/cyrillic shortcut.
func matchKey(k input.Key, latin, cyrillic rune) bool {
	r := unicode.ToLower(k.Rune)
	b := unicode.ToLower(k.BaseKey)
	s := unicode.ToLower(k.ShiftedKey)
	lat := unicode.ToLower(latin)
	cyr := unicode.ToLower(cyrillic)
	if lat != 0 && (r == lat || b == lat || s == lat) {
		return true
	}
	if cyr != 0 && (r == cyr || b == cyr || s == cyr) {
		return true
	}
	if len(k.Text) > 0 {
		t := []rune(k.Text)
		if len(t) == 1 {
			tr := unicode.ToLower(t[0])
			if (lat != 0 && tr == lat) || (cyr != 0 && tr == cyr) {
				return true
			}
		}
	}
	return false
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

		lat := tokLower
		cyr := latinToCyrillic[tokLower]
		if cyr == 0 {
			if l, ok := cyrillicToLatin[tokLower]; ok {
				lat = l
				cyr = tokLower
			}
		}

		if lat >= 'a' && lat <= 'z' {
			ctrlByte := lat - 'a' + 1
			if k.Rune == ctrlByte || (k.BaseKey != 0 && k.BaseKey == ctrlByte) {
				return true
			}
		}

		if matchKey(k, lat, cyr) {
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
		if (!k.HasShift() && (k.Rune == 26 || k.BaseKey == 26 || (k.HasCtrl() && (matchKey(k, 'z', 'я') || matchKey(k, 'u', 'г') || k.Rune == 21)))) ||
			(!k.HasShift() && (k.BaseKey == 'z' || k.BaseKey == 'я') && (k.HasCtrl() || k.Rune == 26)) ||
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
		if m.splits != nil && m.splits.TotalPanes() > 1 && (m.splits.ActiveIndex > 0 || m.splits.Panes[m.splits.ActiveIndex].IsView()) {
			m.closeSplitPane(m.splits.ActiveIndex)
			return m, nil, true
		}
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

	// P2P Collaboration modal intercepts keys when open
	if m.p2pModal != nil && m.p2pModal.Visible {
		keyStr := ""
		switch k.Type {
		case input.KeyEsc:
			keyStr = "Escape"
		case input.KeyEnter:
			keyStr = "Enter"
		default:
			if k.Rune != 0 {
				keyStr = string(k.Rune)
			}
		}
		if keyStr != "" && m.p2pModal.HandleKeyEvent(keyStr) {
			return m, nil
		}
		return m, nil
	}

	// DB Diff modal intercepts keys when open
	if m.dbDiffModal != nil && m.dbDiffModal.Visible {
		if k.Type == input.KeyEsc {
			m.dbDiffModal.Visible = false
			return m, nil
		}
		if k.Type == input.KeyTab {
			m.dbDiffModal.ActiveTab = (m.dbDiffModal.ActiveTab + 1) % 2
			return m, nil
		}
		return m, nil
	}

	// DB Connection modal intercepts keys when open
	if m.dbConnectModal != nil && m.dbConnectModal.Visible {
		if m.dbConnectModal.HandleKey(k) {
			return m, nil
		}
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

	// AI Assistant Chat Panel intercepts keys when focused or model menu is open
	if m.rightSidebarOpen && (m.rightSidebarMode == "ai-chat" || m.rightSidebarMode == "ai") && m.chatPanel != nil {
		if m.chatPanel.InputFocused || m.chatPanel.ModelMenuOpen {
			if !(k.HasCtrl() && (matchKey(k, 'l', 'д') || k.Rune == 12)) {
				activeDocPath := ""
				doc := m.eng.ActiveDocument()
				if doc != nil {
					activeDocPath = doc.FilePath
				}
				activeSelection := m.selectedText()
				activeDiagnostics, _ := m.getDiagnosticAtCursor()
				activeTerminal := ""
				if m.chatPanel.HandleKey(k, activeDocPath, activeSelection, activeDiagnostics, activeTerminal) {
					return m, nil
				}
			}
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
			if m.gitModal.DetailOpen && m.gitModal.AnimProgress < 1.0 {
				return m, func() tea.Msg { return animTickMsg{} }
			}
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

	// DevTools Modal intercepts keys when open
	if m.devtoolsModal != nil && m.devtoolsModal.Open {
		if m.devtoolsModal.HandleKey(k) {
			return m, nil
		}
		return m, nil
	}

	// Regex Tester Modal intercepts keys when open
	if m.regexModal != nil && m.regexModal.Open {
		if m.regexModal.HandleKey(k) {
			return m, nil
		}
		return m, nil
	}

	// Bookmarks Modal intercepts keys when open
	if m.bookmarksModal != nil && m.bookmarksModal.Open {
		if m.bookmarksModal.HandleKey(k) {
			return m, nil
		}
		return m, nil
	}

	// Right Sidebar tool window key intercept
	if m.rightSidebarOpen {
		switch m.rightSidebarMode {
		case "docker":
			if m.dockerPanel != nil && m.dockerPanel.HandleKey(k) {
				return m, nil
			}
		case "rest-client", "rest":
			if m.restClientPanel != nil && m.restClientPanel.HandleKey(k) {
				return m, nil
			}
		case "grpc":
			if m.grpcPanel != nil && m.grpcPanel.HandleKey(k) {
				return m, nil
			}
		case "log-viewer", "logs":
			if m.logPanel != nil && m.logPanel.HandleKey(k) {
				return m, nil
			}
		case "task-runner", "tasks":
			if m.taskPanel != nil && m.taskPanel.HandleKey(k) {
				return m, nil
			}
		case "todo-tree", "todo":
			if m.todoPanel != nil && m.todoPanel.HandleKey(k) {
				return m, nil
			}
		case "test-runner", "tests":
			if m.testRunnerPanel != nil && m.testRunnerPanel.HandleKey(k) {
				return m, nil
			}
		case "jupyter-notebook", "jupyter":
			if m.jupyterPanel != nil && m.jupyterPanel.HandleKey(k) {
				return m, nil
			}
		case "p2p-collab", "collab", "p2p":
			if m.p2pPanel != nil && m.p2pPanel.HandleKey(k) {
				return m, nil
			}
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

	// 2.85 Ecosystem Modal & Utility Shortcuts
	// Ctrl+Shift+U: DevTools Utilities
	if k.HasCtrl() && k.HasShift() && (matchKey(k, 'u', 'г') || k.Rune == 21) {
		m.openDevToolsModal()
		return m, nil
	}

	// Ctrl+Shift+R: Regex Tester & Replacer
	if k.HasCtrl() && k.HasShift() && (matchKey(k, 'r', 'к') || k.Rune == 18) {
		m.openRegexModal()
		return m, nil
	}

	// Ctrl+Shift+B: Bookmarks Navigator
	if k.HasCtrl() && k.HasShift() && (matchKey(k, 'b', 'и') || k.Rune == 2) {
		m.openBookmarksModal()
		return m, nil
	}

	// Ctrl+Shift+L: Toggle P2P Collaboration Panel
	if k.HasCtrl() && k.HasShift() && (matchKey(k, 'l', 'д') || k.Rune == 12) {
		cmd := m.ToggleRightSidebar("p2p-collab")
		return m, cmd
	}

	// Ctrl+F2: Toggle Bookmark at current line
	if k.HasCtrl() && k.Type == input.KeyF2 {
		m.toggleActiveBookmark()
		return m, nil
	}

	// Alt+F2: Next Bookmark
	if k.HasAlt() && k.Type == input.KeyF2 {
		m.jumpNextBookmark()
		return m, nil
	}

	// Shift+F2: Previous Bookmark
	if k.HasShift() && k.Type == input.KeyF2 {
		m.jumpPrevBookmark()
		return m, nil
	}

	// Shift+F6: Project-wide symbol rename
	if k.HasShift() && k.Type == input.KeyF6 {
		m.openRenameModal()
		return m, nil
	}

	// Ctrl+Enter on .http/.rest file: Execute HTTP request
	if k.HasCtrl() && (k.Type == input.KeyEnter || k.Rune == '\r' || k.Rune == '\n') {
		if doc := m.eng.ActiveDocument(); doc != nil && (strings.HasSuffix(doc.FilePath, ".http") || strings.HasSuffix(doc.FilePath, ".rest")) {
			m.executeActiveHTTPRequest()
			return m, nil
		}
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

	case input.KeyF3:
		// F3: Open Project Graphs & Call Hierarchy in split pane
		m.OpenProjectGraphInSplit()
		return m, nil

	case input.KeyF4:
		// F4: Toggle Integrated Terminal
		if m.terminal != nil {
			m.terminal.Toggle()
			m.terminalFocused = m.terminal.Open
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

	case input.KeyF6:
		// F6: Open Database ER Diagram in editor tab
		m.OpenERDTab()
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
		}
		return m, nil
	}

	// Alt+1 .. Alt+6: Switch directly to split pane 1..6
	if k.HasAlt() && !k.HasCtrl() && k.Rune >= '1' && k.Rune <= '6' {
		paneIdx := int(k.Rune - '1')
		if m.splits != nil && paneIdx < m.splits.TotalPanes() {
			m.switchActivePane(paneIdx)
		}
		return m, nil
	}

	// Alt+Right / Alt+Left / Alt+Enter / Alt+\: Next / Prev split pane / Quick Fix / AI Completion
	if k.HasAlt() && !k.HasCtrl() {
		if k.Rune == '\\' || k.BaseKey == '\\' {
			if m.aiEngine == nil || (m.pluginMgr != nil && !m.pluginMgr.IsEnabled("ai-completion")) {
				m.toasts.Warn("AI", "Plugin 'ai-completion' is disabled (press Ctrl+, to enable)")
				return m, nil
			}
			if m.aiEngine.IsDownloading() {
				pct, msg := m.aiEngine.DownloadProgress()
				m.toasts.Info("AI", fmt.Sprintf("AI model is downloading in background (%d%%: %s)", pct, msg))
				return m, nil
			}
			st, reason := m.aiEngine.Status()
			if st == ai.StatusNoModel || st == ai.StatusNoServer {
				if m.triggerAIAutoDownload() {
					return m, nil
				}
				m.toasts.Error("AI", fmt.Sprintf("AI Completion error: %s (%s)", st, reason))
				return m, nil
			}
			if st == ai.StatusOffline || st == ai.StatusNoAPIKey {
				m.toasts.Error("AI", fmt.Sprintf("AI Completion error: %s (%s)", st, reason))
				return m, nil
			}
			m.triggerAICompletion(true)
			return m, nil
		}
		if k.Type == input.KeyEnter {
			m.triggerQuickFix()
			return m, nil
		}
		if k.Type == input.KeyRight {
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				m.switchActivePane(m.splits.NextPane())
				return m, nil
			}
		} else if k.Type == input.KeyLeft {
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				m.switchActivePane(m.splits.PrevPane())
				return m, nil
			}
		} else if matchKey(k, 'm', 'ь') || k.Rune == 'm' || k.Rune == 'M' {
			// Alt+M: Toggle Minimap
			if m.settings != nil {
				m.settings.Current.ShowMinimap = !m.settings.Current.ShowMinimap
				_ = m.settings.Save()
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
			// Ctrl+W: Close split pane if focused or multiple panes, otherwise close buffer
			if m.splits != nil && m.splits.TotalPanes() > 1 && (m.splits.ActiveIndex > 0 || m.splits.Panes[m.splits.ActiveIndex].IsView()) {
				m.closeSplitPane(m.splits.ActiveIndex)
				return m, nil
			}
			if active := m.eng.ActiveDocument(); active != nil {
				m.eng.CloseBuffer(active.ID)
				m.onActiveDocumentChanged()
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
			} else if k.HasAlt() {
				// Ctrl+Alt+B: Toggle secondary right sidebar
				cmd := m.ToggleRightSidebar("")
				return m, cmd
			} else {
				// Ctrl+B: Toggle file explorer sidebar
				cmd := m.ToggleSidebar()
				m.statusMessage = fmt.Sprintf("Project Tree: %v", m.sidebarOpen)
				return m, cmd
			}

		case matchKey(k, 'l', 'д') || k.Rune == 12:
			// Ctrl+L: Toggle AI Assistant Right Sidebar & focus input
			cmd := m.ToggleRightSidebar("ai-chat")
			if m.rightSidebarOpen && m.chatPanel != nil {
				m.chatPanel.InputFocused = true
			} else if !m.rightSidebarOpen && m.chatPanel != nil {
				m.chatPanel.InputFocused = false
			}
			return m, cmd

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
				m.notifyLSPChange()
				m.ensureCursorVisible()
				return m, nil
			}

		case matchKey(k, 'v', 'м') || k.Rune == 22:
			// Ctrl+V: Paste from system clipboard
			clip, err := clipboard.Read()
			if err == nil && clip != "" {
				_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: clip})
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

	// 6.9 Interactive Split Views Key Dispatch (DAG Canvas, Project Graphs, etc.)
	if m.splits != nil && m.splits.ActivePane() != nil && m.splits.ActivePane().IsView() {
		ap := m.splits.ActivePane()
		if (ap.ViewID == "dag-canvas" || ap.ViewID == "db-canvas") && m.dagCanvasWidget != nil {
			switch k.Type {
			case input.KeyUp:
				m.dagCanvasWidget.Pan(0, 2)
				return m, nil
			case input.KeyDown:
				m.dagCanvasWidget.Pan(0, -2)
				return m, nil
			case input.KeyLeft:
				m.dagCanvasWidget.Pan(4, 0)
				return m, nil
			case input.KeyRight:
				m.dagCanvasWidget.Pan(-4, 0)
				return m, nil
			case input.KeyTab:
				if k.HasShift() {
					m.dagCanvasWidget.SelectPrevNode()
				} else {
					m.dagCanvasWidget.SelectNextNode()
				}
				return m, nil
			case input.KeyEsc:
				m.closeSplitPane(ap.Index)
				return m, nil
			}
			if k.Rune == 'c' || k.Rune == 'C' || k.Rune == 'с' || k.Rune == 'С' {
				m.dagCanvasWidget.PanX = 0
				m.dagCanvasWidget.PanY = 0
				if m.toasts != nil {
					m.toasts.Info("DAG CAMERA", "Camera centered at origin (0, 0)")
				}
				return m, nil
			}
			if k.Rune == 'w' || k.Rune == 'W' || k.Rune == 'ц' || k.Rune == 'Ц' {
				m.dagCanvasWidget.Pan(0, 2)
				return m, nil
			}
			if k.Rune == 's' || k.Rune == 'S' || k.Rune == 'ы' || k.Rune == 'Ы' {
				m.dagCanvasWidget.Pan(0, -2)
				return m, nil
			}
			if k.Rune == 'a' || k.Rune == 'A' || k.Rune == 'ф' || k.Rune == 'Ф' {
				m.dagCanvasWidget.Pan(4, 0)
				return m, nil
			}
			if k.Rune == 'd' || k.Rune == 'D' || k.Rune == 'в' || k.Rune == 'В' {
				m.dagCanvasWidget.Pan(-4, 0)
				return m, nil
			}
		}
		if ap.ViewID == "project-graph" && m.projectGraphPanel != nil {
			switch k.Type {
			case input.KeyUp:
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(0, 2)
				}
				return m, nil
			case input.KeyDown:
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(0, -2)
				}
				return m, nil
			case input.KeyLeft:
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(4, 0)
				}
				return m, nil
			case input.KeyRight:
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(-4, 0)
				}
				return m, nil
			case input.KeyTab:
				if m.projectGraphPanel.Canvas != nil {
					if k.HasShift() {
						m.projectGraphPanel.Canvas.SelectPrevNode()
					} else {
						m.projectGraphPanel.Canvas.SelectNextNode()
					}
				}
				return m, nil
			case input.KeyEsc:
				m.closeSplitPane(ap.Index)
				return m, nil
			}
			if k.Rune == 'r' || k.Rune == 'R' || k.Rune == 'к' || k.Rune == 'К' {
				m.projectGraphPanel.CycleMode()
				docToUse := m.eng.ActiveDocument()
				if docToUse == nil && len(m.splits.Panes) > 0 {
					docToUse = m.findDocument(m.splits.Panes[0].DocID)
				}
				m.projectGraphPanel.RebuildWithLSP(docToUse, m.workspaceDir, m.lspClient)
				m.statusMessage = fmt.Sprintf("Graph Mode: %s", m.projectGraphPanel.ModeTitle())
				return m, nil
			}
			if k.Rune == 'c' || k.Rune == 'C' || k.Rune == 'с' || k.Rune == 'С' {
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.PanX = 0
					m.projectGraphPanel.Canvas.PanY = 0
				}
				if m.toasts != nil {
					m.toasts.Info("GRAPH CAMERA", "Camera centered at origin (0, 0)")
				}
				return m, nil
			}
		}
		if k.Type == input.KeyEsc {
			m.closeSplitPane(ap.Index)
			return m, nil
		}
		return m, nil
	}

	// 6.10 Interactive ER Diagram Editor Tab Key Dispatch
	if activeDoc := m.eng.ActiveDocument(); activeDoc != nil && (filepath.Base(activeDoc.FilePath) == "schema.erd" || strings.HasSuffix(activeDoc.FilePath, ".erd")) {
		if m.dagCanvasWidget != nil {
			switch k.Type {
			case input.KeyUp:
				m.dagCanvasWidget.Pan(0, 2)
				return m, nil
			case input.KeyDown:
				m.dagCanvasWidget.Pan(0, -2)
				return m, nil
			case input.KeyLeft:
				m.dagCanvasWidget.Pan(4, 0)
				return m, nil
			case input.KeyRight:
				m.dagCanvasWidget.Pan(-4, 0)
				return m, nil
			case input.KeyTab:
				if k.HasShift() {
					m.dagCanvasWidget.SelectPrevNode()
				} else {
					m.dagCanvasWidget.SelectNextNode()
				}
				return m, nil
			}
			if k.Rune == 'c' || k.Rune == 'C' || k.Rune == 'с' || k.Rune == 'С' {
				m.dagCanvasWidget.PanX = 0
				m.dagCanvasWidget.PanY = 0
				if m.toasts != nil {
					m.toasts.Info("ER DIAGRAM", "Camera centered")
				}
				return m, nil
			}
			if k.Rune == 'w' || k.Rune == 'W' || k.Rune == 'ц' || k.Rune == 'Ц' {
				m.dagCanvasWidget.Pan(0, 2)
				return m, nil
			}
			if k.Rune == 's' || k.Rune == 'S' || k.Rune == 'ы' || k.Rune == 'Ы' {
				m.dagCanvasWidget.Pan(0, -2)
				return m, nil
			}
			if k.Rune == 'a' || k.Rune == 'A' || k.Rune == 'ф' || k.Rune == 'Ф' {
				m.dagCanvasWidget.Pan(4, 0)
				return m, nil
			}
			if k.Rune == 'd' || k.Rune == 'D' || k.Rune == 'в' || k.Rune == 'В' {
				m.dagCanvasWidget.Pan(-4, 0)
				return m, nil
			}
		}
		return m, nil
	}

	// 6.11 Interactive DataGrid Tab Key Dispatch
	if activeDoc := m.eng.ActiveDocument(); activeDoc != nil && strings.HasSuffix(activeDoc.FilePath, ".datagrid") {
		tblName := strings.TrimSuffix(filepath.Base(activeDoc.FilePath), ".datagrid")
		grid := m.getOrCreateDataGrid(tblName)
		switch k.Type {
		case input.KeyUp:
			grid.SelectPrevRow()
			return m, nil
		case input.KeyDown:
			grid.SelectNextRow()
			return m, nil
		case input.KeyPgUp:
			grid.PrevPage()
			return m, nil
		case input.KeyPgDown:
			grid.NextPage()
			return m, nil
		}
		if (k.HasCtrl() && (k.Rune == 'e' || k.Rune == 's')) || k.Rune == 'e' || k.Rune == 'E' {
			grid.ExportCSV(m.workspaceDir)
			if m.toasts != nil {
				m.toasts.Success("EXPORT CSV", fmt.Sprintf("Saved table %s to %s_export.csv", grid.TableName, grid.TableName))
			}
			return m, nil
		}
		return m, nil
	}

	// 6.12 Interactive Project Graph Editor Tab Key Dispatch
	if activeDoc := m.eng.ActiveDocument(); activeDoc != nil && (filepath.Base(activeDoc.FilePath) == "project.graph" || strings.HasSuffix(activeDoc.FilePath, ".graph")) {
		if m.projectGraphPanel != nil {
			switch k.Type {
			case input.KeyUp:
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(0, 2)
				}
				return m, nil
			case input.KeyDown:
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(0, -2)
				}
				return m, nil
			case input.KeyLeft:
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(4, 0)
				}
				return m, nil
			case input.KeyRight:
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(-4, 0)
				}
				return m, nil
			case input.KeyTab:
				if m.projectGraphPanel.Canvas != nil {
					if k.HasShift() {
						m.projectGraphPanel.Canvas.SelectPrevNode()
					} else {
						m.projectGraphPanel.Canvas.SelectNextNode()
					}
				}
				return m, nil
			case input.KeyEsc:
				m.eng.CloseBuffer(activeDoc.ID)
				m.statusMessage = "Closed Project Graph tab"
				return m, nil
			case input.KeyEnter:
				if m.projectGraphPanel.Canvas != nil && m.projectGraphPanel.Canvas.SelectedNodeID != "" {
					m.jumpToSymbolFromGraph(m.projectGraphPanel.Canvas.SelectedNodeID)
					return m, nil
				}
			}
			if k.Rune == 'r' || k.Rune == 'R' || k.Rune == 'к' || k.Rune == 'К' {
				m.projectGraphPanel.CycleMode()
				var docToUse *core.Document
				for _, d := range m.eng.Documents() {
					b := filepath.Base(d.FilePath)
					if b != "project.graph" && !strings.HasSuffix(b, ".graph") && b != "schema.erd" && !strings.HasSuffix(b, ".erd") {
						docToUse = d
						break
					}
				}
				m.projectGraphPanel.RebuildWithLSP(docToUse, m.workspaceDir, m.lspClient)
				m.statusMessage = fmt.Sprintf("Graph Mode: %s", m.projectGraphPanel.ModeTitle())
				return m, nil
			}
			if k.Rune == 'c' || k.Rune == 'C' || k.Rune == 'с' || k.Rune == 'С' {
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.PanX = 0
					m.projectGraphPanel.Canvas.PanY = 0
				}
				if m.toasts != nil {
					m.toasts.Info("GRAPH CAMERA", "Camera centered at origin (0, 0)")
				}
				return m, nil
			}
			if k.Rune == 'w' || k.Rune == 'W' || k.Rune == 'ц' || k.Rune == 'Ц' {
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(0, 2)
				}
				return m, nil
			}
			if k.Rune == 's' || k.Rune == 'S' || k.Rune == 'ы' || k.Rune == 'Ы' {
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(0, -2)
				}
				return m, nil
			}
			if k.Rune == 'a' || k.Rune == 'A' || k.Rune == 'ф' || k.Rune == 'Ф' {
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(4, 0)
				}
				return m, nil
			}
			if k.Rune == 'd' || k.Rune == 'D' || k.Rune == 'в' || k.Rune == 'В' {
				if m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(-4, 0)
				}
				return m, nil
			}
		}
		return m, nil
	}

	// 6.13 SQL Console Execute Key Dispatch (Ctrl+Enter)
	if (k.Type == input.KeyEnter || k.Rune == 10 || k.Rune == 13) && k.HasCtrl() {
		activeDoc := m.eng.ActiveDocument()
		if activeDoc != nil && strings.HasSuffix(activeDoc.FilePath, ".sql") {
			m.executeSQLQueryAtCursor()
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
		if k.HasCtrl() && m.ghostText != "" {
			word := ai.NextWordFromGhostText(m.ghostText)
			if word != "" {
				_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: word})
				m.ghostText = m.ghostText[len(word):]
				m.notifyLSPChange()
				m.ensureCursorVisible()
				return m, nil
			}
		}
		cmd := core.CmdCursorRight
		if k.HasShift() {
			cmd = core.CmdSelectRight
		}
		_ = m.eng.Dispatch(core.Command{ID: cmd})
		m.popupVisible = false
		m.dismissGhostText()

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
			m.dismissGhostText()
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
		m.dismissGhostText()

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
		m.dismissGhostText()

	case input.KeySpace:
		// CRITICAL FIX: Space bar insertion
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: " "})
		m.notifyLSPChange()
		m.popupVisible = false
		m.updateGhostText()

	case input.KeyRune:
		if k.HasCtrl() || (k.Rune >= 1 && k.Rune <= 26 && k.Rune != 9 && k.Rune != 10 && k.Rune != 13) {
			// Discard unhandled Ctrl combinations to avoid corrupting buffer with control bytes
			return m, nil
		}
		typedStr := string(k.Rune)
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: typedStr})
		m.notifyLSPChange()
		if k.Rune == '.' {
			m.triggerCompletionPopup()
		} else {
			m.popupVisible = false
			if m.ghostText != "" && strings.HasPrefix(m.ghostText, typedStr) {
				m.ghostText = m.ghostText[len(typedStr):]
			} else {
				m.updateGhostText()
			}
		}
	}

	m.ensureCursorVisible()
	return m, nil
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

