package i18n

import (
	"fmt"
	"sort"
	"sync"
)

// LocaleInfo represents metadata about an available language.
type LocaleInfo struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Registry manages in-memory translations and active locale selection.
type Registry struct {
	mu            sync.RWMutex
	activeLocale  string
	desiredLocale string
	locales       map[string]map[string]string
	localeNames   map[string]string
	listeners     []func(string)
}

var globalRegistry = &Registry{
	activeLocale:  "en",
	desiredLocale: "en",
	locales: map[string]map[string]string{
		"en": defaultEnglishCatalog,
	},
	localeNames: map[string]string{
		"en": "English (US)",
	},
}

// T retrieves the translation for the given key in the active locale.
// If the key is not found in the active locale, it falls back to English, then to the key itself.
// Optional args are formatted via fmt.Sprintf.
func T(key string, args ...any) string {
	return globalRegistry.Translate(key, args...)
}

// SetLocale changes the active UI language and triggers listeners.
func SetLocale(locale string) {
	globalRegistry.SetLocale(locale)
}

// GetLocale returns the currently active language code.
func GetLocale() string {
	return globalRegistry.GetLocale()
}

// RegisterTranslations adds or extends translations for a locale from a plugin.
func RegisterTranslations(locale, localeName string, dict map[string]string) {
	globalRegistry.Register(locale, localeName, dict)
}

// DeregisterTranslations removes a plugin's translations when it is uninstalled.
func DeregisterTranslations(locale string) {
	globalRegistry.Deregister(locale)
}

// AvailableLocales lists all installed language packs, with English first.
func AvailableLocales() []LocaleInfo {
	return globalRegistry.Available()
}

// AddLocaleChangeListener registers a callback executed when the language changes.
func AddLocaleChangeListener(fn func(string)) {
	globalRegistry.AddListener(fn)
}

// Translate performs lookup and formatting with thread safety.
func (r *Registry) Translate(key string, args ...any) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var pattern string
	found := false

	// 1. Check active locale
	if dict, ok := r.locales[r.activeLocale]; ok {
		if val, exists := dict[key]; exists && val != "" {
			pattern = val
			found = true
		}
	}

	// 2. Fallback to English
	if !found && r.activeLocale != "en" {
		if dict, ok := r.locales["en"]; ok {
			if val, exists := dict[key]; exists && val != "" {
				pattern = val
				found = true
			}
		}
	}

	// 3. Fallback to raw key
	if !found {
		pattern = key
	}

	if len(args) > 0 {
		return fmt.Sprintf(pattern, args...)
	}
	return pattern
}

// SetLocale switches the active language.
func (r *Registry) SetLocale(locale string) {
	r.mu.Lock()
	if locale == "" {
		locale = "en"
	}
	r.desiredLocale = locale
	if _, ok := r.locales[locale]; !ok && locale != "en" {
		// Locale not registered, fallback to en
		locale = "en"
	}
	changed := r.activeLocale != locale
	r.activeLocale = locale
	listeners := make([]func(string), len(r.listeners))
	copy(listeners, r.listeners)
	r.mu.Unlock()

	if changed {
		for _, fn := range listeners {
			fn(locale)
		}
	}
}

// GetLocale returns the active locale.
func (r *Registry) GetLocale() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeLocale
}

// Register registers translations for a locale.
func (r *Registry) Register(locale, localeName string, dict map[string]string) {
	r.mu.Lock()
	if _, ok := r.locales[locale]; !ok {
		r.locales[locale] = make(map[string]string)
	}
	for k, v := range dict {
		r.locales[locale][k] = v
	}
	if localeName != "" {
		r.localeNames[locale] = localeName
	}
	shouldActivate := (r.desiredLocale == locale && r.activeLocale != locale)
	if shouldActivate {
		r.activeLocale = locale
	}
	listeners := make([]func(string), len(r.listeners))
	copy(listeners, r.listeners)
	r.mu.Unlock()

	if shouldActivate {
		for _, fn := range listeners {
			fn(locale)
		}
	}
}

// Deregister removes a non-English locale dictionary.
func (r *Registry) Deregister(locale string) {
	if locale == "en" {
		return
	}
	r.mu.Lock()
	delete(r.locales, locale)
	delete(r.localeNames, locale)
	changed := false
	if r.activeLocale == locale {
		r.activeLocale = "en"
		changed = true
	}
	listeners := make([]func(string), len(r.listeners))
	copy(listeners, r.listeners)
	r.mu.Unlock()

	if changed {
		for _, fn := range listeners {
			fn("en")
		}
	}
}

// Available returns registered locales.
func (r *Registry) Available() []LocaleInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := []LocaleInfo{
		{Code: "en", Name: "English (US)"},
	}

	var otherCodes []string
	for code := range r.locales {
		if code != "en" {
			otherCodes = append(otherCodes, code)
		}
	}
	sort.Strings(otherCodes)

	for _, code := range otherCodes {
		name := r.localeNames[code]
		if name == "" {
			name = code
		}
		result = append(result, LocaleInfo{Code: code, Name: name})
	}

	return result
}

// AddListener adds a callback.
func (r *Registry) AddListener(fn func(string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listeners = append(r.listeners, fn)
}

// defaultEnglishCatalog contains all built-in base strings in English.
var defaultEnglishCatalog = map[string]string{
	// Crash Recovery Dialog
	"dialog.crash.title":           "⚠  Attention: Previous session ended unexpectedly",
	"dialog.crash.crashed_at":      "IDE closed unexpectedly on %s.",
	"dialog.crash.crashed_recent":  "IDE closed unexpectedly recently.",
	"dialog.crash.reason":          "Reason: %s",
	"dialog.crash.dump_file":       "Dump file: %s",
	"dialog.crash.info1":           "You can send an anonymous report to help developers fix this problem.",
	"dialog.crash.info2":           "The report contains a call stack trace and system diagnostic logs.",
	"dialog.crash.view_content":    "  View Report Content  ",
	"dialog.crash.sending":         "Sending crash report... ⏳",
	"dialog.crash.send_failed":     "Could not connect to server. Dump file is preserved locally.",
	"dialog.crash.server_error":    "Server response error.",
	"dialog.crash.continue_work":   "  Continue Working  ",
	"dialog.crash.sent_success":    "Crash dump successfully sent!",
	"dialog.crash.deleted":         "Crash dump file deleted",

	// Buttons (strictly no [ ])
	"btn.send_dev":                 "  Send to Developer (Enter)  ",
	"btn.delete":                   "  Delete  ",
	"btn.ignore":                   "  Ignore (Esc)  ",
	"btn.browse":                   "  Browse  ",
	"btn.view_data":                "  View Data  ",
	"btn.send_ctrl_enter":          "  Send (Ctrl+Enter)  ",
	"btn.copy_github":              "  Copy for GitHub  ",
	"btn.cancel":                   "  Cancel  ",
	"btn.close_esc":                "  Close (Esc)  ",

	// Bug Report Modal
	"modal.bugreport.title":        " Report an Issue or Bug ",
	"modal.bugreport.field_title":  "Title:",
	"modal.bugreport.field_desc":   "Description (steps to reproduce, expected behavior):",
	"modal.bugreport.field_screen": "Screenshot (PNG/JPG/WEBP path up to 10 MB):",
	"modal.bugreport.attach_logs":  "Attach current session logs (200 lines)",
	"modal.bugreport.err_title":    "Please enter a title for the issue!",
	"modal.bugreport.sending":      "Submitting bug report... ⏳",
	"modal.bugreport.send_fail":    "Failed to send report to server. You can copy Markdown for GitHub.",
	"modal.bugreport.err_generic":  "Error while sending report.",
	"modal.bugreport.file_not_found": "✗ File not found",
	"modal.bugreport.is_dir":       "✗ Specified path is a directory",
	"modal.bugreport.file_too_big": "✗ File exceeds 10 MB limit",
	"modal.bugreport.invalid_ext":  "✗ Unsupported format (must be PNG, JPG, WEBP)",
	"modal.bugreport.file_verified": "✓ File verified (%d KB)",
	"modal.bugreport.sent_success": "Bug report submitted successfully!",
	"modal.bugreport.copied_github": "Bug report Markdown copied to clipboard!",

	// Log & Data Inspector
	"modal.inspector.title":        "System Diagnostics & Log Inspector",
	"modal.inspector.tab_logs":     " Logs & Stack ",
	"modal.inspector.tab_env":      " System Environment ",
	"modal.inspector.tab_json":     " JSON Dump ",
	"modal.inspector.search_prompt": "/ Search: %s█",
	"modal.inspector.filter_stats": "Filter: \"%s\" (matches: %d) [/ to edit]",
	"modal.inspector.nav_hint":     "Navigation: ↑/↓, PgUp/PgDn, Tab switch tab, / search",
	"modal.inspector.no_json":      "(No JSON available)",
	"modal.inspector.dump_title":   "Crash Dump Contents",
	"modal.inspector.session_title": "Session Diagnostics Data",
	"modal.inspector.sys_logs_title": "System Logs & Diagnostics",

	// Main Menu
	"menu.open_folder":             "Open Folder / Project...",
	"menu.new_project":             "New Project...",
	"menu.open_file":               "Open File (Omnibar)...",
	"menu.commands":                "Command Palette...",
	"menu.save_file":               "Save File",
	"menu.marketplace":             "Plugins Marketplace...",
	"menu.settings":                "Settings",
	"menu.report_bug":              "Report Bug / Issue...",
	"menu.view_logs":               "System Logs & Diagnostics...",
	"menu.exit":                    "Exit",

	// Splash Screen
	"splash.starting":              "Starting Tahr IDE...",
	"splash.scanning":              "Scanning workspace...",
	"splash.loading_plugins":       "Loading plugins & project configuration...",
	"splash.init_syntax":           "Initializing syntax engines & AST cache...",
	"splash.connecting_lsp":        "Connecting language server protocols...",
	"splash.ready":                 "Workspace ready",
	"splash.tagline":               "THE AGILITY-FIRST DEVELOPER ENVIRONMENT",
	"splash.skip_hint":             "Press any key or click to skip",

	// Settings & General UI
	"settings.title":               " Settings ",
	"settings.saved":               "Settings saved",
	"settings.hint.actions":        " Tab: Switch | Enter: Edit/Rebind | Ctrl+S: Save | Ctrl+R: Reset | Esc: Close ",
	"settings.hint.press_key":      "Press new key...",

	// Settings Categories
	"settings.cat.editor":          "Editor & Cursor",
	"settings.cat.appearance":      "Appearance & Layout",
	"settings.cat.splits":          "Split Panes (1-6)",
	"settings.cat.keybindings":     "Keyboard & Shortcuts",
	"settings.cat.colors":          "Color Palette (HEX)",
	"settings.cat.toolchains":      "Toolchains & SDKs",
	"settings.cat.plugins":         "Plugins & Marketplace",

	// Settings Fields
	"settings.field.tab_size":      "Tab Size",
	"settings.field.use_spaces":    "Indentation",
	"settings.field.line_numbers":  "Line Numbers",
	"settings.field.word_wrap":     "Word Wrap",
	"settings.field.cursor_style":  "Cursor Style",
	"settings.field.cursor_blink":  "Cursor Blink",
	"settings.field.scrolloff_y":   "Scroll Margin",
	"settings.field.show_minimap":  "Code Minimap",
	"settings.field.auto_save":     "Auto Save",
	"settings.field.language":      "Language",
	"settings.field.theme":         "Theme Preset",
	"settings.field.tree_position": "Project Tree Dock",
	"settings.field.file_icon_style": "File Icons Style",
	"settings.field.smooth_anim":   "Tree Animations",
	"settings.field.default_split": "Default Split View",
	"settings.field.split_cycle":   "Cycle Layout Hotkey",
	"settings.field.split_next":    "Focus Next Pane",
	"settings.field.split_prev":    "Focus Prev Pane",
	"settings.field.kb_save":       "Save Document",
	"settings.field.kb_run":        "Run Active Profile",
	"settings.field.kb_build":      "Build Project",
	"settings.field.kb_tree_toggle": "Toggle Project Tree",
	"settings.field.kb_tree_dock":  "Swap Tree Left/Right",
	"settings.field.kb_split_cycle": "Cycle Splits (1-6)",
	"settings.field.kb_split_next": "Focus Next Split",
	"settings.field.kb_split_prev": "Focus Prev Split",
	"settings.field.kb_split_1":    "Split: 1 Single",
	"settings.field.kb_split_2":    "Split: 2 Columns",
	"settings.field.kb_split_3":    "Split: 3 Columns",
	"settings.field.kb_split_4":    "Split: 4 Grid (2x2)",
	"settings.field.kb_split_5":    "Split: 5 Panes",
	"settings.field.kb_split_6":    "Split: 6 Grid (3x2)",
	"settings.field.kb_term_toggle": "Toggle Terminal Drawer",
	"settings.field.kb_commands":   "Command Palette (Omnibar)",
	"settings.field.kb_find_files": "Find Files (Fuzzy)",
	"settings.field.kb_marketplace": "Extensions Marketplace",
	"settings.field.kb_settings":   "Open Settings Modal",
	"settings.field.go_sdk":        "Go SDK Path",
	"settings.field.gopls":         "Go LSP (gopls)",
	"settings.field.go_flags":      "Go Build Flags",
	"settings.field.python":        "Python Interpreter",
	"settings.field.python_venv":   "Python Virtualenv",
	"settings.field.pyright":       "Pyright LSP",
	"settings.field.cargo":         "Cargo Binary Path",
	"settings.field.rust_analyzer": "Rust Analyzer Path",
	"settings.field.registry_url":  "Official Registry URL",

	// Settings Values
	"settings.val.enabled":         "Enabled",
	"settings.val.disabled":        "Disabled",
	"settings.val.spaces":          "Spaces",
	"settings.val.tabs":            "Tabs",
	"settings.val.dock_left":       "Left Dock (default)",
	"settings.val.dock_right":      "Right Dock",
	"settings.val.anim_smooth":     "Smooth (120 FPS)",
	"settings.val.anim_instant":    "Instant (0 ms)",
	"settings.val.icons_unicode":   "Unicode Clean",
	"settings.val.icons_nerd":      "Nerd Fonts",
	"settings.val.icons_minimal":   "Minimal",
	"settings.val.n_spaces":        "%d spaces",
	"settings.val.n_lines":         "%d lines",
	"settings.val.n_panes":         "%d Pane(s)",

	// Toolbar
	"toolbar.find":                 "Find",
	"toolbar.term":                 "Term",
	"toolbar.build":                "Build",
	"toolbar.run":                  "▶ Run",
	"toolbar.debug":                "Debug",
	"toolbar.split":                "Split:%d",
	"toolbar.no_profile":           "No Profile",

	// Status Bar
	"status.mode.normal":           "NORMAL",
	"status.mode.explorer":         "EXPLORER",
	"status.cursor_info":           "Ln %d, Col %d",
	"status.cursor_info_split":     "[%d/%d] Ln %d, Col %d",
	"status.lsp_active":            "LSP: ACTIVE",
	"status.lsp_off":               "LSP: OFF",

	// Marketplace
	"market.title":                 " TAHR EXTENSION MARKETPLACE ",
	"market.tab_browse":            " 1: Marketplace ",
	"market.tab_installed":         " 2: Installed ",
	"market.tab_repos":             " 3: Repositories & Servers ",
	"market.search":                "Search: ",
	"market.author":                "Author: %s  |  Source: %s",
	"market.install":               " Enter: Install / Update ",
	"market.enabled":               " Enabled ✓ (Space/Enter: Disable)    d/Del: Uninstall ",
	"market.disabled":              " Disabled (Space/Enter: Enable)    d/Del: Uninstall ",

	// Find & Replace
	"find.title_find":              " FIND ",
	"find.title_replace":           " FIND & REPLACE ",
	"find.btn_replace":             "  Replace  ",
	"find.btn_replace_all":         "  Replace All  ",
	"find.find_label":              "Find: ",
	"find.repl_label":              "Repl: ",
	"find.hint_find":               "Enter: Next │ Shift+Enter: Prev │ Alt+W: Word │ ⇄: Repl │ Alt+Bksp: Undo",
	"find.hint_repl":               "Enter: Replace │ Alt+Enter: All │ Tab: Switch │ ⇄: Find │ Alt+Bksp: Undo",

	// Tooltips
	"tooltip.menu":                 "Main Menu",
	"tooltip.find_files":           "Find File / Omnibar (Ctrl+P)",
	"tooltip.split":                "Split Panes (%s) - Click or Ctrl+\\ to cycle",
	"tooltip.run":                  "Run Active Profile (F5 / Ctrl+R)",
	"tooltip.build":                "Build Active Profile (F7 / Ctrl+Shift+B)",
	"tooltip.term":                 "Integrated Terminal (F4 / Ctrl+~)",
	"tooltip.hud":                  "DAP Debugger (F8)",
	"tooltip.profile_select":       "Select Run/Debug Profile",
	"tooltip.app_version":          "Tahr Terminal IDE v0.4.0",
	"tooltip.tab_close":            "Close Buffer (Ctrl+W)",
	"tooltip.tab_switch":           "Switch to %s",
	"tooltip.side_explorer":        "Toggle Project Explorer (F2)",
	"tooltip.side_outline":         "Structure & Symbols Outline",
	"tooltip.side_git":             "Version Control (Git)",
	"tooltip.side_plugins":         "Plugins & Marketplace (Ctrl+Shift+X)",
	"tooltip.side_settings":        "IDE Settings (Ctrl+,)",
	"tooltip.tree_dec_width":       "Decrease Tree Width (Alt+[)",
	"tooltip.tree_inc_width":       "Increase Tree Width (Alt+])",
	"tooltip.tree_dock":            "Move Tree Dock Left/Right (Ctrl+Alt+E)",
	"tooltip.breakpoint":           "Line %d - Toggle Breakpoint (F9)",

	// Keybinding Descriptions
	"settings.kb.find":             "Find in File",
	"settings.kb.replace":          "Find & Replace",
	"settings.kb.rename":           "Rename Symbol (Project)",
	"settings.kb.goto_line":        "Go to Line",
	"settings.kb.omnibar":          "Find File (Omnibar)",
	"settings.kb.commands":         "Command Palette",
	"settings.kb.select_all":       "Select All",
	"settings.kb.next_match":       "Select Next Match",
	"settings.kb.copy":             "Copy Selection",
	"settings.kb.cut":              "Cut Selection",
	"settings.kb.paste":            "Paste Clipboard",
	"settings.kb.tab_next":         "Next Tab / Buffer",
	"settings.kb.tab_prev":         "Previous Tab / Buffer",
	"settings.kb.undo":             "Undo Edit",
	"settings.kb.redo":             "Redo Edit",
	"settings.kb.breakpoint":       "Toggle Breakpoint",
	"settings.kb.step_over":        "Step Over",
	"settings.kb.step_into":        "Step Into",
	"settings.kb.step_out":         "Step Out",
	"settings.kb.definition":       "Go to Definition",
	"settings.kb.hover":            "Hover Documentation",
	"settings.kb.settings":         "Settings Modal",
	"settings.kb.close_tab":        "Close Tab",
	"settings.kb.quit":             "Quit IDE",
	"settings.kb.terminal_toggle":  "Toggle Terminal",
	"settings.kb.terminal_split":   "Toggle Terminal Split",
	"settings.kb.terminal_tab_new": "New Terminal Tab",
	"settings.kb.terminal_tab_close": "Close Terminal Tab",
	"settings.kb.quick_fix":        "Quick Fix Actions",
	"settings.kb.minimap_toggle":   "Toggle Minimap",
	"settings.kb.markdown_preview": "Markdown Preview Split",
	"settings.kb.git_modal":        "Git Staging & Graph",
	"settings.kb.marketplace":      "Extension Marketplace",
	"settings.kb.completion":       "Trigger Completion",
	"settings.kb.pick_color":       "Pick / Edit Color at Cursor",
	"settings.val.not_bound":       "(not bound)",

	// Color Palette
	"settings.color.bg":            "Background",
	"settings.color.fg":            "Foreground",
	"settings.color.sel_bg":        "Selection Background",
	"settings.color.cursor":        "Cursor Color",
	"settings.color.gutter_bg":     "Gutter Background",
	"settings.color.line_number":   "Line Numbers",
	"settings.color.keyword":       "Keyword Color",
	"settings.color.function":      "Function Color",
	"settings.color.string":        "String Color",
	"settings.color.type":          "Type Color",
	"settings.color.comment":       "Comment Color",
	"settings.color.constant":      "Constant Color",
	"settings.color.error":         "Error Diagnostic",
	"settings.color.warn":          "Warning Diagnostic",
	"settings.color.border":        "Border Color",
	"settings.color.popup_bg":      "Popup Background",
	"settings.color.popup_sel_bg":  "Popup Selection",
	"settings.color.occurrence_bg": "Word / Selection Highlight",

	// Toolchains & SDKs
	"settings.tool.go_sdk_path":    "Go SDK Path",
	"settings.tool.auto_detected":  "(auto-detected)",
	"settings.tool.ready":          "Ready",
	"settings.tool.missing":        "Missing",
	"settings.tool.val_missing":    "%s (Missing)",
	"settings.tool.lbl_compiler":   "%s: Compiler: %s",
	"settings.tool.lbl_lsp":        "%s: LSP: %s",
	"settings.tool.lbl_dap":        "%s: DAP: %s",
	"settings.tool.none_label":     "Language Toolchains",
	"settings.tool.none_val":       "No language plugins currently enabled",

	// Plugins Settings
	"settings.plugin.open_market_lbl": "Extensions Marketplace",
	"settings.plugin.open_market_val": "Open Full Marketplace (Ctrl+Shift+X)",
	"settings.plugin.registry_url_lbl": "Primary Registry URL",
	"settings.plugin.repo_lbl":     "Repo: %s",
	"settings.plugin.enabled":      "Enabled",
	"settings.plugin.disabled":     "Disabled",
	"settings.plugin.plugin_lbl":   "Plugin: %s (v%s)",
	"settings.plugin.status_enabled": "Enabled (Enter: Disable)",
	"settings.plugin.status_disabled": "Disabled (Enter: Enable)",
	"settings.plugin.config_section": "Settings",
	"settings.plugin.installed_summary": "Installed: %d active / %d total",
	"settings.plugin.manage_in_market": "Open Marketplace (Ctrl+Shift+X)",

	// Option Values
	"settings.val.opt.absolute":    "Absolute",
	"settings.val.opt.relative":    "Relative",
	"settings.val.opt.none":        "None",
	"settings.val.opt.block":       "Block",
	"settings.val.opt.bar":         "Bar",
	"settings.val.opt.underline":   "Underline",
	"settings.val.opt.off":         "Off",
	"settings.val.opt.on_focus_loss": "On Focus Loss",
	"settings.val.opt.after_delay": "After Delay",

	// Marketplace Extended
	"market.category":              "Category",
	"market.cat.all":               "All",
	"market.cat.lsp":               "LSP",
	"market.cat.theme":             "Theme",
	"market.cat.tools":             "Tools",
	"market.cat.i18n":              "Localization",
	"market.cat.database":          "Database",
	"market.cat.infrastructure":    "Infrastructure",
	"market.status_tab0":           " Tab: Focus | /: Search | Enter: Install | R: Refresh | Esc: Close ",
	"market.cat.dap":               "DAP",
	"market.add_server":            "+ Add New Plugin Repository Server",
	"market.edit_server":           "✎ Edit Plugin Repository Server",
	"market.field_name":            "Name",
	"market.field_url":             "URL",
	"market.field_type":            "Type (http, github, local)",
	"market.hint_form":             "Tab: Switch Field │ Enter: Save Server │ Esc: Cancel",
	"market.server_name":           "Server: %s",
	"market.server_url":            "URL: %s",
	"market.server_type_status":    "Type: %s │ Status: %s",
	"market.server_actions":        "A: Add Server │ E: Edit │ T: Ping/Test │ Space: Toggle │ D: Delete",
	"market.test_status":           "Test Status: %s",
	"market.capabilities":          "Capabilities: [fs:read] [process:exec] (Wasm Sandboxed)",
	"market.tags":                  "Tags: %s",

	// Context Menu
	"ctx.new_file":                 "New File...",
	"ctx.new_dir":                  "New Directory...",
	"ctx.rename":                   "Rename...",
	"ctx.move":                     "Move...",
	"ctx.copy_path":                "Copy Relative Path",
	"ctx.delete":                   "Delete",
	"ctx.refresh":                  "Refresh Explorer",

	// Settings Tool Actions & Fallbacks
	"settings.tool.action_title":   "Tool Action",
	"settings.tool.plugin":         "Plugin",
	"settings.tool.status":         "Status",
	"settings.tool.path":           "Path",
	"settings.tool.not_found":      "(not found)",
	"settings.tool.opt_download":   "Download & Install Automatically",
	"settings.tool.opt_path":       "Specify Path to Binary Manually",
	"settings.tool.opt_reset":      "Reset / Revert to Auto-Detection",
	"settings.tool.opt_cancel":     "Cancel / Back",
	"settings.tool.input_path":     "Specify Path",
	"settings.tool.hint_save":      "Enter: Save Path",
	"settings.tool.hint_cancel":    "Esc: Cancel",
	"settings.plugin.install":      "Install",

	// Terminal Header
	"term.inactive":                "INACTIVE",
	"term.focused":                 "FOCUSED",
	"term.title":                   ">_ TERMINAL",
	"term.add":                     "+ Add",
	"term.split":                   "Split",
	"term.stack":                   "Stack",
	"term.tabs":                    "Tabs",
	"term.clear":                   "Clear",
	"term.kill":                    "Kill",

	// Color Picker
	"colorpicker.title":            "Color Palette Wheel",
	"colorpicker.brightness":       "Brightness: %3d%% ◄",
	"colorpicker.original":         "Original: ",
	"colorpicker.new":              "New: ",
	"colorpicker.hint_tab":         "Tab: Mode",
	"colorpicker.hint_apply":       "Enter: Apply",
	"colorpicker.hint_cancel":      "Esc: Cancel",

	// Problems Panel
	"problems.title":               "PROBLEMS (%d errors, %d warnings)",
	"problems.clean":               "No problems found in workspace. Code is clean! ✓",
	"problems.tag_error":           "[ERROR] ",
	"problems.tag_warn":            "[WARN]  ",
	"problems.tag_info":            "[INFO]  ",
	"problems.close":               "✕ Close",

	// Git Modal
	"git.window_title":             "GIT REPOSITORY",
	"git.tab_staging":              " 1: Interactive Hunk Staging ",
	"git.tab_graph":                " 2: Visual Branch Graph ",
	"git.tab_commit":               " 1: Interactive Commit ",
	"git.tab_branch_graph":         " 2: Branch & Commit Graph ",
	"git.no_changes":               "Working tree is clean. No unstaged or staged changes.",
	"git.no_hunks":                 "No unstaged diff hunks found for the active file.",
	"git.hunk_num":                 "%s%s Hunk #%d",
	"git.staged_section":           "STAGED CHANGES (%d)",
	"git.unstaged_section":         "UNSTAGED CHANGES (%d)",
	"git.untracked_section":        "UNTRACKED FILES (%d)",
	"git.commit_prompt":            "Commit Message: ",
	"git.commit_empty_warning":     "Nothing staged! Press 'a' to stage all changes, or Space on a file.",
	"git.btn_stage":                "Space: Stage/Unstage",
	"git.btn_stage_all":            "a: Stage All",
	"git.btn_commit":               "c: Commit",
	"git.btn_refresh":              "r: Refresh",
	"git.graph_header":             "2D Branch Diagram  ←/→: Navigate, ↑/↓: Commits, Enter: Details",
	"git.filter_branch":            "Branch: %s",
	"git.filter_period":            "Date: %s",
	"git.filter_search":            "Search: %s",
	"git.col_hash":                 "HASH",
	"git.col_branch":               "BRANCH / REFS",
	"git.col_message":              "MESSAGE",
	"git.col_author":               "AUTHOR",
	"git.col_date":                 "DATE",
	"git.detail_title":             "COMMIT DETAILS",
	"git.detail_author":            "Author: %s <%s>",
	"git.detail_date":              "Date:   %s (%s)",
	"git.detail_refs":              "Refs:   %s",
	"git.detail_message":           "Message:",
	"git.detail_files":             "Changed Files (%d):",
	"git.detail_diff_title":        "DIFF",
	"git.detail_diff_back":         "Esc: Back",
	"git.footer_tab0":              "Space: Toggle  a: Stage All  c: Commit  r: Refresh  Tab: Next Tab  Esc: Close",
	"git.footer_tab1":              "←/→: Navigate  ↑/↓: Commits  Enter: Details  b: Branch  d: Date  /: Search  Esc: Close",

	// New Project Modal
	"newproj.title":                "CREATE NEW PROJECT",
	"newproj.templates":            "PROJECT TEMPLATES",
	"newproj.name":                 "Project Name:",
	"newproj.location":             "Location:    ",
	"newproj.module":               "Module / Pkg:",
	"newproj.sdk":                  "SDK Toolchain:",
	"newproj.sdk_auto":             "Auto-detected SDK toolchain",
	"newproj.preview":              "PROJECT STRUCTURE PREVIEW",
	"newproj.btn_create":           " Create Project ",
	"newproj.btn_cancel":           " Cancel ",
	"newproj.hint":                 "Tab: Next Field │ ↑/↓: Select │ Enter: Create │ Esc: Cancel",

	// Search in Files
	"searchfiles.title":            "Search in Files (Ctrl+Shift+F)",
	"searchfiles.prompt":           "Search: ",
	"searchfiles.type_to_search":   "Type to search project files...",
	"searchfiles.searching":        "Searching...",
	"searchfiles.toggles":          "Alt+C: Case %v │ Alt+R: Regex %v │ Alt+W: Word %v",
	"searchfiles.invalid_regex":    "Invalid regex: %v",
	"searchfiles.matches_found":    "%d matches in %d files",
	"searchfiles.matches_overflow": "%d+ matches in %d files",

	// Rename Modal
	"rename.title":                 "RENAME SYMBOL (PROJECT-WIDE)",
	"rename.current":               "Current: %s",
	"rename.new_name":              "New Name: ",
	"rename.btn_rename":            " Rename ",
	"rename.btn_cancel":            " Cancel ",

	// DAP HUD
	"dap.title":                    "DEBUGGER",
	"dap.btn_cont":                 "▶ Cont",
	"dap.btn_over":                 "↷ Over",
	"dap.btn_into":                 "⇣ Into",
	"dap.btn_out":                  "⇡ Out",
	"dap.btn_stop":                 "■ Stop",
	"dap.tab_vars":                 "Variables",
	"dap.tab_stack":                "Call Stack",
	"dap.tab_watch":                "Watch & Eval",
	"dap.no_session":               "(No active debugging session. Press F5 to launch)",
	"dap.no_vars":                  "(No local variables available at current breakpoint)",
	"dap.no_stack":                 "(No call stack available)",
	"dap.eval_expr":                "Eval Expr: %s█",
	"dap.results_history":          "Results History:",
	"dap.type_expr":                "(Type expression and press Enter to evaluate)",

	// Tool Prompt
	"toolprompt.title":             "Missing Tool: %s",
	"toolprompt.desc":              "Language support for '%s' requires %s (%s).",
	"toolprompt.opt_download":      "D: Download & Install: %s",
	"toolprompt.opt_download_auto": "D: Download & Install Automatically",
	"toolprompt.opt_path":          "P: Specify Path to Binary Manually",
	"toolprompt.opt_ignore":        "Esc: Ignore / Continue without Language Server",
	"toolprompt.input_label":       "Enter Binary Path: ",
	"toolprompt.hint":              "Enter: Confirm │ Esc: Back",

	// Launch Modal
	"launch.title":                 "Launch & Debug Configurations (.tahr/launch.json)",
	"launch.hint_list":             "↑/↓: Navigate │ Space: Set Active │ a: Add │ e: Edit │ d: Delete │ o: Open File │ F5: Run",
	"launch.profiles":              "Profiles",
	"launch.no_profiles":           "No launch profiles",
	"launch.btn_new_template":      "+ New from template",
	"launch.details":               "Configuration Details",
	"launch.lbl_name":              "Name:",
	"launch.lbl_target":            "Target:",
	"launch.lbl_cmd":               "Command:",
	"launch.lbl_args":              "Args:",
	"launch.lbl_cwd":               "Working Dir:",
	"launch.lbl_prelaunch":         "Pre-Launch:",
	"launch.lbl_console":           "Console:",
	"launch.lbl_type":              "Type:",
	"launch.lbl_request":           "Request:",
	"launch.actions_profile":       "Edit (e)   Delete (d)   Open launch.json (o)   Run (F5)",
	"launch.help_create":           "Create a new launch profile.",
	"launch.help_templates":        "Available templates from plugins:",
	"launch.help_enter":            "Press Enter or click to create new profile.",
	"launch.status_active":         "Active: %s  │  Total Configurations: %d",
	"launch.edit_title":            "EDIT CONFIGURATION PROFILE",
	"launch.add_title":             "ADD CONFIGURATION PROFILE",
	"launch.type_hint":             "(Space/Up/Down: select, or type custom)",
	"launch.type_hint_opts":        "(Space/Up/Down: %s, or type custom)",
	"launch.request_hint":          "(Space/Up/Down: launch, debug)",
	"launch.target_hint":           "e.g. main.go, ./cmd/tahr, app.py",
	"launch.cmd_hint":              "e.g. echo hello, fflow stats -re .go, git status",
	"launch.args_hint":             "space separated",
	"launch.cwd_hint":              "working directory (optional)",
	"launch.console_hint":          "(Space/Up/Down: integratedTerminal, internalConsole)",
	"launch.prelaunch_hint":        "task/command before launch",
	"launch.btn_save":              " Save Profile (Ctrl+S) ",
	"launch.btn_cancel":            " Cancel (Esc) ",

	// General
	"toast.saved":                  "SAVED",
	"status.saved":                 "Saved %s",
	"hover.source_lsp":             "LSP",
	"hover.source_comments":        "Code Comments",
	"hover.source_image":           "Image Preview",

	// Split Modes
	"split.mode_single":            "Single Pane",
	"split.mode_2cols":             "2 Columns",
	"split.mode_2rows":             "2 Rows",
	"split.mode_3cols":             "3 Columns",
	"split.mode_4grid":             "4 Grid (2x2)",
	"split.mode_5panes":            "5 Panes",
	"split.mode_6grid":             "6 Grid (3x2)",
}
