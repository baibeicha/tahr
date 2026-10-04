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
	"settings.hint.press_key":      "[Press new key...]",

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
}
