package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/core/keymaps"
	"tahr/internal/core/plugin"
	"tahr/internal/ui"
)

// Settings represents all configurable properties of the Tahr IDE.
type Settings struct {
	// Editor
	TabSize       int    `json:"tab_size"`
	UseSpaces     bool   `json:"use_spaces"`
	LineNumbers   string `json:"line_numbers"` // "absolute", "relative", "none"
	WordWrap      bool   `json:"word_wrap"`
	CursorBlink   bool   `json:"cursor_blink"`
	CursorStyle   string `json:"cursor_style"` // "block", "bar", "underline"
	ScrolloffY    int    `json:"scrolloff_y"`
	AutoSave      string `json:"auto_save"` // "off", "on_focus_loss", "after_delay"
	SmoothAnim    bool   `json:"smooth_anim"`
	ShowMinimap       bool   `json:"show_minimap"`
	AutoPairs         bool   `json:"auto_pairs"`
	IndentGuides      bool   `json:"indent_guides"`
	RainbowDelimiters bool   `json:"rainbow_delimiters"`
	VirtualText       bool   `json:"virtual_text"`

	// Appearance & Layout
	Language      string `json:"language"` // "en" (default), "ru", etc.
	Theme         string `json:"theme"`
	BorderRounded bool   `json:"border_rounded"`
	TreePosition  string `json:"tree_position"` // "left" (default) or "right"
	SidebarWidth  int    `json:"sidebar_width"` // default 26
	FileIconStyle string `json:"file_icon_style"` // "nerd_fonts", "unicode", "minimal"
	DefaultSplit  int    `json:"default_split"`   // 1 to 6

	// Keymap
	LayoutRemap   bool              `json:"layout_remap"`
	KeymapProfile string            `json:"keymap_profile"` // "vscode", "jetbrains", "emacs"
	VimMode       bool              `json:"vim_mode"`
	Keybindings   map[string]string `json:"keybindings"`

	// Custom Colors (Hex strings #RRGGBB)
	CustomColors map[string]string `json:"custom_colors"`

	// Toolchains
	GoSDKPath       string            `json:"go_sdk_path"`
	GoplsPath       string            `json:"gopls_path"`
	GoBuildFlags    string            `json:"go_build_flags"`
	PythonPath      string            `json:"python_path"`
	PythonVenv      string            `json:"python_venv"`
	PyrightPath     string            `json:"pyright_path"`
	CargoPath       string            `json:"cargo_path"`
	RustAnalyzer    string            `json:"rust_analyzer"`
	CustomToolPaths map[string]string `json:"custom_tool_paths,omitempty"`

	// Marketplace / Repositories
	RegistryURL    string                           `json:"registry_url"`
	Repositories   []plugin.PluginRepository        `json:"repositories,omitempty"`
	Plugins        []MarketplacePlugin              `json:"plugins"`
	PluginSettings map[string]map[string]any        `json:"plugin_settings,omitempty"`
}

// MarketplacePlugin represents an installed or available extension.
type MarketplacePlugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
}

// DefaultKeybindings returns the default CUA/IDE hotkey map.
func DefaultKeybindings() map[string]string {
	return keymaps.GetProfileBindings(keymaps.ProfileVSCode)
}

// DefaultCustomColors returns default palette hex strings.
func DefaultCustomColors() map[string]string {
	return map[string]string{
		"bg":            "#1e1e2e",
		"fg":            "#cdd6f4",
		"sel_bg":        "#45475a",
		"cursor":        "#f5e0dc",
		"gutter_bg":     "#181825",
		"line_number":   "#6c7086",
		"keyword":       "#cba6f7",
		"function":      "#89b4fa",
		"string":        "#a6e3a1",
		"type":          "#f9e2af",
		"comment":       "#585b70",
		"constant":      "#fab387",
		"error":         "#f38ba8",
		"warn":          "#fab387",
		"border":        "#313244",
		"popup_bg":      "#181825",
		"popup_sel_bg":  "#313244",
	}
}

// DefaultMarketplaceCatalog returns recommended plugins (empty by default to avoid mock data).
func DefaultMarketplaceCatalog() []MarketplacePlugin {
	return []MarketplacePlugin{}
}

// DefaultSettings returns recommended defaults.
func DefaultSettings() Settings {
	return Settings{
		TabSize:       4,
		UseSpaces:     true,
		LineNumbers:   "absolute",
		WordWrap:      false,
		CursorBlink:   true,
		CursorStyle:   "block",
		ScrolloffY:    2,
		AutoSave:      "off",
		SmoothAnim:    true,
		ShowMinimap:       true,
		AutoPairs:         true,
		IndentGuides:      true,
		RainbowDelimiters: true,
		VirtualText:       true,
		Theme:         "catppuccin",
		Language:      "en",
		BorderRounded: true,
		TreePosition:  "left",
		SidebarWidth:  26,
		FileIconStyle: "minimal",
		DefaultSplit:  1,
		LayoutRemap:   true,
		KeymapProfile: string(keymaps.ProfileVSCode),
		VimMode:       false,
		Keybindings:   DefaultKeybindings(),
		CustomColors:  make(map[string]string),
		GoSDKPath:     "",
		GoplsPath:     "",
		GoBuildFlags:  "-v",
		PythonPath:    "python",
		PythonVenv:    "",
		PyrightPath:   "pyright-langserver",
		CargoPath:       "cargo",
		RustAnalyzer:    "rust-analyzer",
		CustomToolPaths: make(map[string]string),
		RegistryURL:     "https://raw.githubusercontent.com/baibeicha/tahr/main/plugins/registry.json",
		Repositories:    plugin.DefaultRepositories(),
		Plugins:         DefaultMarketplaceCatalog(),
	}
}

// configFilePath returns the destination path for settings.json.
func configFilePath() string {
	if p := os.Getenv("TAHR_CONFIG_DIR"); p != "" {
		return filepath.Join(p, "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "tahr", "settings.json")
}

// LoadSettings loads settings from ~/.config/tahr/settings.json or creates defaults.
func LoadSettings() Settings {
	cfgFile := configFilePath()
	if cfgFile == "" {
		return DefaultSettings()
	}
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		s := DefaultSettings()
		_ = SaveSettings(s)
		return s
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return DefaultSettings()
	}
	if s.Language == "" {
		s.Language = "en"
	}
	i18n.SetLocale(s.Language)
	if s.TreePosition == "" {
		s.TreePosition = "left"
	}
	if s.FileIconStyle == "" {
		s.FileIconStyle = "minimal"
	}
	if s.Theme == "" {
		s.Theme = "catppuccin"
	}
	if s.DefaultSplit < 1 {
		s.DefaultSplit = 1
	}
	if s.SidebarWidth < 12 {
		s.SidebarWidth = 26
	}
	if s.KeymapProfile == "" {
		s.KeymapProfile = string(keymaps.ProfileVSCode)
	}
	profileBindings := keymaps.GetProfileBindings(keymaps.Profile(s.KeymapProfile))
	if s.Keybindings == nil {
		s.Keybindings = profileBindings
	} else {
		for k, v := range profileBindings {
			if _, ok := s.Keybindings[k]; !ok {
				s.Keybindings[k] = v
			}
		}
	}
	if s.CustomColors == nil {
		s.CustomColors = make(map[string]string)
	}
	if s.CustomToolPaths == nil {
		s.CustomToolPaths = make(map[string]string)
	}
	if s.PluginSettings == nil {
		s.PluginSettings = make(map[string]map[string]any)
	}
	return s
}

// SaveSettings persists settings to ~/.config/tahr/settings.json.
func SaveSettings(s Settings) error {
	cfgFile := configFilePath()
	if cfgFile == "" {
		return nil
	}
	cfgDir := filepath.Dir(cfgFile)
	_ = os.MkdirAll(cfgDir, 0755)

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgFile, data, 0644)
}

// Save persists current settings to disk.
func (st *SettingsState) Save() error {
	if st == nil {
		return nil
	}
	return SaveSettings(st.Current)
}

// ToolActionState manages the interactive binary action modal (Auto-Download / Specify Path).
type ToolActionState struct {
	Open        bool
	ToolName    string
	PluginName  string
	InstallCmd  string
	CurrentPath string
	Ready       bool
	InputMode   bool
	InputPath   string
	StatusMsg   string
}

// SettingsState holds UI state for the comprehensive settings modal.
type SettingsState struct {
	Open                     bool
	CategoryIdx              int
	FieldIdx                 int
	FocusRight               bool
	Categories               []string
	Current                  Settings
	Original                 Settings
	EditingText              bool
	RebindingKey             bool
	InputBuffer              string
	PluginViewTab            string // "all", "installed", "marketplace"
	ColorPicker              *ColorPickerModal
	ToolAction               ToolActionState
	OnColorApplied           func(key, hexVal string)
	OnThemeChanged           func(themeName string)
	OnSettingsChanged        func()
	OnToolInstallStarted     func(toolName, cmd string)
	OnToolInstallFinished    func(toolName string, success bool, err string)
	pluginMgr                *plugin.Manager
	OpenMarketplaceRequested bool
}

// SetPluginManager binds the active plugin manager to settings for live querying.
func (st *SettingsState) SetPluginManager(mgr *plugin.Manager) {
	if st == nil {
		return
	}
	st.pluginMgr = mgr
	if st.Current.Language != "" {
		i18n.SetLocale(st.Current.Language)
	}
}

// NewSettingsState creates a fresh settings modal state.
func (st *SettingsState) getCategories() []string {
	return []string{
		i18n.T("settings.cat.editor"),
		i18n.T("settings.cat.appearance"),
		i18n.T("settings.cat.splits"),
		i18n.T("settings.cat.keybindings"),
		i18n.T("settings.cat.colors"),
		i18n.T("settings.cat.toolchains"),
		i18n.T("settings.cat.plugins"),
	}
}

func NewSettingsState() *SettingsState {
	s := LoadSettings()
	st := &SettingsState{
		Open:          false,
		CategoryIdx:   0,
		FieldIdx:      0,
		FocusRight:    false,
		Current:       s,
		Original:      s,
		PluginViewTab: "all",
		ColorPicker:   NewColorPickerModal(),
	}
	st.Categories = st.getCategories()
	return st
}

type Field struct {
	Key   string
	Label string
	Value string
}

func (st *SettingsState) getCategoryFields(catIdx int) []Field {
	switch catIdx {
	case 0: // Editor & Cursor
		wrapStr := i18n.T("settings.val.disabled")
		if st.Current.WordWrap {
			wrapStr = i18n.T("settings.val.enabled")
		}
		spacesStr := i18n.T("settings.val.spaces")
		if !st.Current.UseSpaces {
			spacesStr = i18n.T("settings.val.tabs")
		}
		blinkStr := i18n.T("settings.val.enabled")
		if !st.Current.CursorBlink {
			blinkStr = i18n.T("settings.val.disabled")
		}
		minimapStr := i18n.T("settings.val.enabled")
		if !st.Current.ShowMinimap {
			minimapStr = i18n.T("settings.val.disabled")
		}
		return []Field{
			{"tab_size", i18n.T("settings.field.tab_size"), fmt.Sprintf(i18n.T("settings.val.n_spaces"), st.Current.TabSize)},
			{"use_spaces", i18n.T("settings.field.use_spaces"), spacesStr},
			{"line_numbers", i18n.T("settings.field.line_numbers"), strings.Title(st.Current.LineNumbers)},
			{"word_wrap", i18n.T("settings.field.word_wrap"), wrapStr},
			{"cursor_style", i18n.T("settings.field.cursor_style"), strings.Title(st.Current.CursorStyle)},
			{"cursor_blink", i18n.T("settings.field.cursor_blink"), blinkStr},
			{"scrolloff_y", i18n.T("settings.field.scrolloff_y"), fmt.Sprintf(i18n.T("settings.val.n_lines"), st.Current.ScrolloffY)},
			{"show_minimap", i18n.T("settings.field.show_minimap"), minimapStr},
			{"auto_save", i18n.T("settings.field.auto_save"), strings.Title(st.Current.AutoSave)},
		}

	case 1: // Appearance & Layout
		langInfo := i18n.AvailableLocales()
		currentLangName := "English (US)"
		for _, li := range langInfo {
			if li.Code == st.Current.Language {
				currentLangName = fmt.Sprintf("%s (%s)", li.Name, li.Code)
				break
			}
		}
		themeStr := strings.Title(st.Current.Theme)
		dockStr := i18n.T("settings.val.dock_left")
		if st.Current.TreePosition == "right" {
			dockStr = i18n.T("settings.val.dock_right")
		}
		animStr := i18n.T("settings.val.anim_smooth")
		if !st.Current.SmoothAnim {
			animStr = i18n.T("settings.val.anim_instant")
		}
		iconsStr := i18n.T("settings.val.icons_unicode")
		if st.Current.FileIconStyle == "nerd_fonts" {
			iconsStr = i18n.T("settings.val.icons_nerd")
		} else if st.Current.FileIconStyle == "minimal" {
			iconsStr = i18n.T("settings.val.icons_minimal")
		}
		return []Field{
			{"language", i18n.T("settings.field.language"), currentLangName},
			{"theme", i18n.T("settings.field.theme"), themeStr},
			{"tree_position", i18n.T("settings.field.tree_position"), dockStr},
			{"file_icon_style", i18n.T("settings.field.file_icon_style"), iconsStr},
			{"smooth_anim", i18n.T("settings.field.smooth_anim"), animStr},
		}

	case 2: // Split Panes (1-6)
		splitStr := fmt.Sprintf(i18n.T("settings.val.n_panes"), st.Current.DefaultSplit)
		return []Field{
			{"default_split", i18n.T("settings.field.default_split"), splitStr},
			{"split_cycle", i18n.T("settings.field.split_cycle"), st.Current.Keybindings["split_cycle"]},
			{"split_next", i18n.T("settings.field.split_next"), st.Current.Keybindings["split_next"]},
			{"split_prev", i18n.T("settings.field.split_prev"), st.Current.Keybindings["split_prev"]},
		}

	case 3: // Keyboard & Shortcuts
		var fields []Field
		keys := []struct {
			id    string
			label string
		}{
			{"save", i18n.T("settings.field.kb_save")},
			{"run", i18n.T("settings.field.kb_run")},
			{"build", i18n.T("settings.field.kb_build")},
			{"tree_toggle", i18n.T("settings.field.kb_tree_toggle")},
			{"tree_dock", i18n.T("settings.field.kb_tree_dock")},
			{"split_cycle", i18n.T("settings.field.kb_split_cycle")},
			{"split_next", i18n.T("settings.field.kb_split_next")},
			{"split_prev", i18n.T("settings.field.kb_split_prev")},
			{"split_1", i18n.T("settings.field.kb_split_1")},
			{"split_2", i18n.T("settings.field.kb_split_2")},
			{"split_3", i18n.T("settings.field.kb_split_3")},
			{"split_4", i18n.T("settings.field.kb_split_4")},
			{"split_5", i18n.T("settings.field.kb_split_5")},
			{"split_6", i18n.T("settings.field.kb_split_6")},
			{"find", "Find in File"},
			{"replace", "Find & Replace"},
			{"rename", "Rename Symbol (Project)"},
			{"goto_line", "Go to Line"},
			{"omnibar", "Find File (Omnibar)"},
			{"commands", "Command Palette"},
			{"select_all", "Select All"},
			{"next_match", "Select Next Match"},
			{"copy", "Copy Selection"},
			{"cut", "Cut Selection"},
			{"paste", "Paste Clipboard"},
			{"tab_next", "Next Tab / Buffer"},
			{"tab_prev", "Previous Tab / Buffer"},
			{"undo", "Undo Edit"},
			{"redo", "Redo Edit"},
			{"breakpoint", "Toggle Breakpoint"},
			{"step_over", "Step Over"},
			{"step_into", "Step Into"},
			{"step_out", "Step Out"},
			{"definition", "Go to Definition"},
			{"hover", "Hover Documentation"},
			{"settings", "Settings Modal"},
			{"close_tab", "Close Tab"},
			{"quit", "Quit IDE"},
			{"terminal_toggle", "Toggle Terminal"},
			{"terminal_split", "Toggle Terminal Split"},
			{"terminal_tab_new", "New Terminal Tab"},
			{"terminal_tab_close", "Close Terminal Tab"},
			{"quick_fix", "Quick Fix Actions"},
			{"minimap_toggle", "Toggle Minimap"},
			{"markdown_preview", "Markdown Preview Split"},
			{"git_modal", "Git Staging & Graph"},
			{"marketplace", "Extension Marketplace"},
			{"completion", "Trigger Completion"},
			{"pick_color", "Pick / Edit Color at Cursor"},
		}
		for _, k := range keys {
			val := st.Current.Keybindings[k.id]
			if val == "" {
				val = "(not bound)"
			}
			fields = append(fields, Field{Key: k.id, Label: k.label, Value: val})
		}
		return fields

	case 4: // Color Palette (HEX)
		var fields []Field
		colorKeys := []struct {
			id    string
			label string
		}{
			{"bg", "Background"},
			{"fg", "Foreground"},
			{"sel_bg", "Selection Background"},
			{"cursor", "Cursor Color"},
			{"gutter_bg", "Gutter Background"},
			{"line_number", "Line Numbers"},
			{"keyword", "Keyword Color"},
			{"function", "Function Color"},
			{"string", "String Color"},
			{"type", "Type Color"},
			{"comment", "Comment Color"},
			{"constant", "Constant Color"},
			{"error", "Error Diagnostic"},
			{"warn", "Warning Diagnostic"},
			{"border", "Border Color"},
			{"popup_bg", "Popup Background"},
			{"popup_sel_bg", "Popup Selection"},
			{"occurrence_bg", "Word / Selection Highlight"},
		}
		for _, c := range colorKeys {
			hexVal := st.Current.CustomColors[c.id]
			if hexVal == "" {
				hexVal = st.getThemeHexColor(c.id)
			}
			fields = append(fields, Field{Key: c.id, Label: c.label, Value: hexVal})
		}
		return fields

	case 5: // Toolchains & SDKs (dynamically discovered from plugins)
		var fields []Field
		fields = append(fields, Field{
			Key:   "go_sdk_path",
			Label: "Go SDK Path",
			Value: defaultStr(st.Current.GoSDKPath, "(auto-detected)"),
		})
		if st.pluginMgr != nil {
			plugins := st.pluginMgr.InstalledPlugins()
			sort.Slice(plugins, func(i, j int) bool {
				return plugins[i].ID < plugins[j].ID
			})
			for _, p := range plugins {
				// 1. Language Compiler / Runner
				for _, lang := range p.Languages {
					cmd := lang.BuildCmd
					if cmd == "" {
						cmd = lang.RunCmd
					}
					if cmd != "" {
						custom := ""
						if st.Current.CustomToolPaths != nil {
							custom = st.Current.CustomToolPaths[cmd]
						}
						if custom == "" && cmd == "go" {
							custom = st.Current.GoSDKPath
						}
						foundPath, ok := st.pluginMgr.FindToolPath(cmd, custom)
						status := "[!] Missing"
						val := cmd + " (Missing)"
						if ok {
							status = "[✓] Ready"
							val = foundPath
						}
						fields = append(fields, Field{
							Key:   fmt.Sprintf("tool:compiler:%s:%s", p.ID, cmd),
							Label: fmt.Sprintf("[%s] Compiler: %s", p.Name, cmd),
							Value: fmt.Sprintf("%s (%s)", status, val),
						})
					}
				}
				// 2. LSP Server
				if p.LSP != nil && p.LSP.Command != "" {
					cmd := p.LSP.Command
					custom := ""
					if st.Current.CustomToolPaths != nil {
						custom = st.Current.CustomToolPaths[cmd]
					}
					if custom == "" && (p.LSP.ServerName == "gopls" || cmd == "gopls") {
						custom = st.Current.GoplsPath
						if custom == "" {
							custom = st.Current.GoSDKPath
						}
					}
					foundPath, ok := st.pluginMgr.FindToolPath(cmd, custom)
					status := "[!] Missing"
					val := cmd + " (Missing)"
					if ok {
						status = "[✓] Ready"
						val = foundPath
					}
					fields = append(fields, Field{
						Key:   fmt.Sprintf("tool:lsp:%s:%s", p.ID, cmd),
						Label: fmt.Sprintf("[%s] LSP: %s", p.Name, p.LSP.ServerName),
						Value: fmt.Sprintf("%s (%s)", status, val),
					})
				}
				// 3. DAP Adapter
				if p.DAP != nil && p.DAP.Command != "" {
					cmd := p.DAP.Command
					custom := ""
					if st.Current.CustomToolPaths != nil {
						custom = st.Current.CustomToolPaths[cmd]
					}
					foundPath, ok := st.pluginMgr.FindToolPath(cmd, custom)
					status := "[!] Missing"
					val := cmd + " (Missing)"
					if ok {
						status = "[✓] Ready"
						val = foundPath
					}
					fields = append(fields, Field{
						Key:   fmt.Sprintf("tool:dap:%s:%s", p.ID, cmd),
						Label: fmt.Sprintf("[%s] DAP: %s", p.Name, p.DAP.AdapterName),
						Value: fmt.Sprintf("%s (%s)", status, val),
					})
				}
			}
		}
		if len(fields) <= 1 {
			fields = append(fields,
				Field{Key: "tool:compiler:tahr-go:go", Label: "[Go Language Support] Compiler: go", Value: "[✓] Ready (go)"},
				Field{Key: "tool:lsp:tahr-go:gopls", Label: "[Go Language Support] LSP: gopls", Value: "[✓] Ready (gopls)"},
				Field{Key: "tool:dap:tahr-go:dlv", Label: "[Go Language Support] DAP: delve", Value: "[✓] Ready (dlv)"},
			)
		}
		return fields

	case 6: // Plugins & Marketplace
		var fields []Field
		fields = append(fields, Field{Key: "open_marketplace", Label: "Extension Browser", Value: "Open Full Marketplace (Ctrl+Shift+X)"})
		fields = append(fields, Field{Key: "registry_url", Label: "Primary Registry URL", Value: st.Current.RegistryURL})

		// Repositories from live manager or settings
		var repos []plugin.PluginRepository
		if st.pluginMgr != nil {
			repos = st.pluginMgr.GetRepositories()
		} else if len(st.Current.Repositories) > 0 {
			repos = st.Current.Repositories
		} else {
			repos = plugin.DefaultRepositories()
		}

		for _, r := range repos {
			rStatus := "Enabled"
			if !r.Enabled {
				rStatus = "Disabled"
			}
			label := fmt.Sprintf("Repo: %s", r.Name)
			val := fmt.Sprintf("%s - %s", rStatus, r.URL)
			fields = append(fields, Field{Key: "repo_" + r.ID, Label: label, Value: val})
		}

		// Installed plugins from live manager
		if st.pluginMgr != nil {
			installed := st.pluginMgr.Installed()
			for _, p := range installed {
				pStatus := "Enabled (Enter: Disable)"
				if !st.pluginMgr.IsEnabled(p.ID) {
					pStatus = "Disabled (Enter: Enable)"
				}
				label := fmt.Sprintf("Plugin: %s (v%s)", p.Name, p.Version)
				fields = append(fields, Field{Key: "plugin_" + p.ID, Label: label, Value: pStatus})

				if len(p.Settings) > 0 && st.pluginMgr.IsEnabled(p.ID) {
					for sKey, sSchema := range p.Settings {
						cfgVal := fmt.Sprintf("%v", sSchema.Default)
						if st.Current.PluginSettings != nil && st.Current.PluginSettings[p.ID] != nil {
							if custom, ok := st.Current.PluginSettings[p.ID][sKey]; ok {
								cfgVal = fmt.Sprintf("%v", custom)
							}
						}
						sLabel := fmt.Sprintf("  • %s: %s", sKey, sSchema.Description)
						fields = append(fields, Field{Key: fmt.Sprintf("psetting_%s_%s", p.ID, sKey), Label: sLabel, Value: cfgVal})
					}
				}
			}
		} else {
			for _, p := range st.Current.Plugins {
				status := "Install"
				if p.Installed {
					if p.Enabled {
						status = "Enabled (Enter: Disable)"
					} else {
						status = "Disabled (Enter: Enable)"
					}
				}
				label := fmt.Sprintf("Plugin: %s (%s)", p.Name, p.Version)
				fields = append(fields, Field{Key: "plugin_" + p.ID, Label: label, Value: status})
			}
		}
		return fields
	}
	return nil
}

// Render draws the settings dialog onto the goatui buffer.
func (st *SettingsState) Render(buf *buffer.Buffer, w, h int, themeBg, themeFg, borderFg, selBg, selFg, accentFg cell.Color) {
	if !st.Open {
		return
	}

	if st.ColorPicker != nil && st.ColorPicker.Open {
		th := ui.Theme{
			Background:  themeBg.Value,
			Foreground:  themeFg.Value,
			BorderColor: borderFg.Value,
			SelectionBg: selBg.Value,
			Function:    accentFg.Value,
			Comment:     toColor(0x888888).Value,
		}
		st.ColorPicker.Render(buf, w, h, th)
		return
	}

	modalW := 84
	if modalW > w-4 {
		modalW = w - 4
	}
	modalH := 20
	if modalH > h-4 {
		modalH = h - 4
	}
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	catW := 24

	// 1. Outer box
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
			buf.SetRune(startX+x, startY+y, ch, fg, themeBg, cell.AttrNone)
		}
	}

	// Title
	title := fmt.Sprintf(" %s (Ctrl+,) ", i18n.T("settings.title"))
	drawString(buf, startX+2, startY, title, accentFg, themeBg, cell.AttrBold, modalW-4)

	// 2. Vertical Divider
	divX := startX + catW
	for y := 1; y < modalH-1; y++ {
		buf.SetRune(divX, startY+y, '│', borderFg, themeBg, cell.AttrNone)
	}

	// 3. Categories on Left
	categories := st.getCategories()
	for idx, cat := range categories {
		rowY := startY + 2 + idx
		if rowY >= startY+modalH-2 {
			break
		}
		cBg := themeBg
		cFg := themeFg
		attr := cell.AttrNone
		if idx == st.CategoryIdx {
			if !st.FocusRight {
				cBg = selBg
				cFg = selFg
				attr = cell.AttrBold
			} else {
				cFg = accentFg
				attr = cell.AttrBold
			}
		}
		for x := 1; x < catW; x++ {
			buf.SetRune(startX+x, rowY, ' ', cFg, cBg, attr)
		}
		catRunes := []rune(cat)
		for i, r := range catRunes {
			if 2+i < catW {
				buf.SetRune(startX+2+i, rowY, r, cFg, cBg, attr)
			}
		}
	}

	// 4. Form Fields on Right
	formStartX := divX + 2
	formW := modalW - catW - 4
	fields := st.getCategoryFields(st.CategoryIdx)

	// Keep fieldIdx bounded
	if st.FieldIdx >= len(fields) {
		st.FieldIdx = max(0, len(fields)-1)
	}

	visibleRows := modalH - 4
	scrollOffset := 0
	if st.FieldIdx >= visibleRows {
		scrollOffset = st.FieldIdx - visibleRows + 1
	}

	for row := 0; row < visibleRows; row++ {
		idx := scrollOffset + row
		if idx >= len(fields) {
			break
		}
		f := fields[idx]
		rowY := startY + 2 + row

		fBg := themeBg
		fFg := themeFg
		attr := cell.AttrNone
		isCurrentField := st.FocusRight && idx == st.FieldIdx
		if isCurrentField {
			fBg = selBg
			fFg = selFg
			attr = cell.AttrBold
		}

		for col := 0; col < formW; col++ {
			buf.SetRune(formStartX+col, rowY, ' ', fFg, fBg, attr)
		}

		if st.CategoryIdx == 4 {
			// Color category with TrueColor swatch: ███ #1e1e2e (no brackets)
			labelRunes := []rune(f.Label)
			for i, r := range labelRunes {
				if i < 22 {
					buf.SetRune(formStartX+i, rowY, r, fFg, fBg, attr)
				}
			}
			if formW > 23 {
				buf.SetRune(formStartX+23, rowY, ':', fFg, fBg, attr)
			}
			swatchX := formStartX + 25
			swatchColor, ok := HexToRGBColor(f.Value)
			if !ok {
				swatchColor = fFg
			}
			swatchRunes := []rune("███")
			for i, r := range swatchRunes {
				buf.SetRune(swatchX+i, rowY, r, swatchColor, fBg, cell.AttrNone)
			}
			valX := swatchX + len(swatchRunes) + 1
			for i, r := range []rune(f.Value) {
				if valX+i < formStartX+formW {
					buf.SetRune(valX+i, rowY, r, fFg, fBg, attr)
				}
			}
			continue
		}

		labelRunes := []rune(f.Label)
		for i, r := range labelRunes {
			if i < 22 {
				buf.SetRune(formStartX+i, rowY, r, fFg, fBg, attr)
			}
		}
		if formW > 23 {
			buf.SetRune(formStartX+23, rowY, ':', fFg, fBg, attr)
		}

		valStartX := formStartX + 25
		valText := f.Value
		if isCurrentField && st.RebindingKey {
			valText = i18n.T("settings.hint.press_key")
		} else if isCurrentField && st.EditingText {
			valText = fmt.Sprintf("[%s_]", st.InputBuffer)
		}

		valRunes := []rune(valText)
		for i, r := range valRunes {
			if valStartX+i < formStartX+formW {
				buf.SetRune(valStartX+i, rowY, r, fFg, fBg, attr)
			}
		}
	}

	// 5. Bottom action hints
	actions := i18n.T("settings.hint.actions")
	drawString(buf, startX+2, startY+modalH-1, actions, themeFg, themeBg, cell.AttrNone, modalW-4)

	if st.ToolAction.Open {
		st.renderToolAction(buf, w, h, themeBg, themeFg, borderFg, selBg, selFg, accentFg)
	}
}

func defaultStr(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

func (st *SettingsState) isTextField() bool {
	fields := st.getCategoryFields(st.CategoryIdx)
	if st.FieldIdx < 0 || st.FieldIdx >= len(fields) {
		return false
	}
	f := fields[st.FieldIdx]
	if st.CategoryIdx == 4 { // Color Palette uses ColorPickerModal instead of text input
		return false
	}
	if st.CategoryIdx == 5 {
		if strings.HasPrefix(f.Key, "tool:") {
			return false
		}
		return true
	}
	if st.CategoryIdx == 6 && (f.Key == "registry_url" || strings.HasPrefix(f.Key, "repo_")) {
		return true
	}
	return false
}

func (st *SettingsState) getCurrentFieldValue() string {
	fields := st.getCategoryFields(st.CategoryIdx)
	if st.FieldIdx < 0 || st.FieldIdx >= len(fields) {
		return ""
	}
	f := fields[st.FieldIdx]
	switch st.CategoryIdx {
	case 4: // Colors
		return st.Current.CustomColors[f.Key]
	case 5: // Toolchains
		switch f.Key {
		case "go_sdk_path":
			return st.Current.GoSDKPath
		case "gopls_path":
			return st.Current.GoplsPath
		case "go_build_flags":
			return st.Current.GoBuildFlags
		case "python_path":
			return st.Current.PythonPath
		case "python_venv":
			return st.Current.PythonVenv
		case "pyright_path":
			return st.Current.PyrightPath
		case "cargo_path":
			return st.Current.CargoPath
		case "rust_analyzer":
			return st.Current.RustAnalyzer
		}
	case 6: // Plugins
		if f.Key == "registry_url" {
			return st.Current.RegistryURL
		}
		if strings.HasPrefix(f.Key, "repo_") {
			repoID := strings.TrimPrefix(f.Key, "repo_")
			if st.pluginMgr != nil {
				for _, r := range st.pluginMgr.GetRepositories() {
					if r.ID == repoID {
						return r.URL
					}
				}
			}
			for _, r := range st.Current.Repositories {
				if r.ID == repoID {
					return r.URL
				}
			}
		}
	}
	return ""
}

func (st *SettingsState) setCurrentFieldValue(val string) {
	fields := st.getCategoryFields(st.CategoryIdx)
	if st.FieldIdx < 0 || st.FieldIdx >= len(fields) {
		return
	}
	f := fields[st.FieldIdx]
	switch st.CategoryIdx {
	case 4: // Colors
		st.Current.CustomColors[f.Key] = val
	case 5: // Toolchains
		switch f.Key {
		case "go_sdk_path":
			st.Current.GoSDKPath = val
		case "gopls_path":
			st.Current.GoplsPath = val
		case "go_build_flags":
			st.Current.GoBuildFlags = val
		case "python_path":
			st.Current.PythonPath = val
		case "python_venv":
			st.Current.PythonVenv = val
		case "pyright_path":
			st.Current.PyrightPath = val
		case "cargo_path":
			st.Current.CargoPath = val
		case "rust_analyzer":
			st.Current.RustAnalyzer = val
		}
	case 6: // Plugins
		if f.Key == "registry_url" {
			st.Current.RegistryURL = val
			_ = st.Save()
		}
		if strings.HasPrefix(f.Key, "repo_") {
			repoID := strings.TrimPrefix(f.Key, "repo_")
			if st.pluginMgr != nil {
				for _, r := range st.pluginMgr.GetRepositories() {
					if r.ID == repoID {
						r.URL = val
						_ = st.pluginMgr.UpdateRepository(r)
						break
					}
				}
			}
			for i := range st.Current.Repositories {
				if st.Current.Repositories[i].ID == repoID {
					st.Current.Repositories[i].URL = val
					break
				}
			}
			_ = st.Save()
		}
	}
	_ = st.Save()
	if st.OnSettingsChanged != nil {
		st.OnSettingsChanged()
	}
}

// cycleCurrentField toggles enumerable choices.
func (st *SettingsState) cycleCurrentField() {
	fields := st.getCategoryFields(st.CategoryIdx)
	if st.FieldIdx < 0 || st.FieldIdx >= len(fields) {
		return
	}
	f := fields[st.FieldIdx]

	switch f.Key {
	case "tab_size":
		switch st.Current.TabSize {
		case 2:
			st.Current.TabSize = 4
		case 4:
			st.Current.TabSize = 8
		default:
			st.Current.TabSize = 2
		}
	case "use_spaces":
		st.Current.UseSpaces = !st.Current.UseSpaces
	case "line_numbers":
		switch st.Current.LineNumbers {
		case "absolute":
			st.Current.LineNumbers = "relative"
		case "relative":
			st.Current.LineNumbers = "none"
		default:
			st.Current.LineNumbers = "absolute"
		}
	case "word_wrap":
		st.Current.WordWrap = !st.Current.WordWrap
	case "cursor_style":
		switch st.Current.CursorStyle {
		case "block":
			st.Current.CursorStyle = "bar"
		case "bar":
			st.Current.CursorStyle = "underline"
		default:
			st.Current.CursorStyle = "block"
		}
	case "cursor_blink":
		st.Current.CursorBlink = !st.Current.CursorBlink
	case "scrolloff_y":
		switch st.Current.ScrolloffY {
		case 0:
			st.Current.ScrolloffY = 2
		case 2:
			st.Current.ScrolloffY = 4
		case 4:
			st.Current.ScrolloffY = 8
		default:
			st.Current.ScrolloffY = 0
		}
	case "show_minimap":
		st.Current.ShowMinimap = !st.Current.ShowMinimap
	case "auto_save":
		switch st.Current.AutoSave {
		case "off":
			st.Current.AutoSave = "on_focus_loss"
		case "on_focus_loss":
			st.Current.AutoSave = "after_delay"
		default:
			st.Current.AutoSave = "off"
		}
	case "language":
		locales := i18n.AvailableLocales()
		if len(locales) > 0 {
			cur := st.Current.Language
			if cur == "" {
				cur = "en"
			}
			nextIdx := 0
			for idx, loc := range locales {
				if loc.Code == cur {
					nextIdx = (idx + 1) % len(locales)
					break
				}
			}
			st.Current.Language = locales[nextIdx].Code
			i18n.SetLocale(st.Current.Language)
			_ = st.Save()
			if st.OnSettingsChanged != nil {
				st.OnSettingsChanged()
			}
			return
		}
	case "theme":
		baseThemes := []string{"catppuccin", "gruvbox", "tokyo-night", "dracula", "nord", "monokai"}
		themeMap := make(map[string]bool)
		var themes []string
		for _, t := range baseThemes {
			themes = append(themes, t)
			themeMap[t] = true
		}
		if st.pluginMgr != nil {
			for tName := range st.pluginMgr.InstalledThemes() {
				tl := strings.ToLower(tName)
				if !themeMap[tl] {
					themeMap[tl] = true
					themes = append(themes, tl)
				}
			}
		}
		cur := strings.ToLower(st.Current.Theme)
		nextIdx := 0
		for idx, t := range themes {
			if t == cur {
				nextIdx = (idx + 1) % len(themes)
				break
			}
		}
		st.Current.Theme = themes[nextIdx]
		st.Current.CustomColors = make(map[string]string)
		_ = st.Save()
		if st.OnThemeChanged != nil {
			st.OnThemeChanged(st.Current.Theme)
		}
		if st.OnSettingsChanged != nil {
			st.OnSettingsChanged()
		}
		return
	case "tree_position":
		if st.Current.TreePosition == "right" {
			st.Current.TreePosition = "left"
		} else {
			st.Current.TreePosition = "right"
		}
	case "file_icon_style":
		switch st.Current.FileIconStyle {
		case "unicode":
			st.Current.FileIconStyle = "nerd_fonts"
		case "nerd_fonts":
			st.Current.FileIconStyle = "minimal"
		default:
			st.Current.FileIconStyle = "unicode"
		}
	case "smooth_anim":
		st.Current.SmoothAnim = !st.Current.SmoothAnim
	case "default_split":
		st.Current.DefaultSplit++
		if st.Current.DefaultSplit > 6 {
			st.Current.DefaultSplit = 1
		}
	default:
		// Check if it's a plugin action in Category 6
		if st.CategoryIdx == 6 {
			if f.Key == "open_marketplace" {
				st.OpenMarketplaceRequested = true
				return
			}
			if strings.HasPrefix(f.Key, "repo_") {
				repoID := strings.TrimPrefix(f.Key, "repo_")
				if st.pluginMgr != nil {
					_ = st.pluginMgr.ToggleRepository(repoID)
				}
				_ = st.Save()
				if st.OnSettingsChanged != nil {
					st.OnSettingsChanged()
				}
				return
			}
			if strings.HasPrefix(f.Key, "psetting_") {
				parts := strings.SplitN(strings.TrimPrefix(f.Key, "psetting_"), "_", 2)
				if len(parts) == 2 {
					pID := parts[0]
					sKey := parts[1]
					if st.Current.PluginSettings == nil {
						st.Current.PluginSettings = make(map[string]map[string]any)
					}
					if st.Current.PluginSettings[pID] == nil {
						st.Current.PluginSettings[pID] = make(map[string]any)
					}
					currVal := st.Current.PluginSettings[pID][sKey]
					if bVal, ok := currVal.(bool); ok {
						st.Current.PluginSettings[pID][sKey] = !bVal
					} else {
						st.Current.PluginSettings[pID][sKey] = fmt.Sprint(currVal) != "true"
					}
					_ = st.Save()
					if st.OnSettingsChanged != nil {
						st.OnSettingsChanged()
					}
					return
				}
			}
			if strings.HasPrefix(f.Key, "plugin_") {
				pluginID := strings.TrimPrefix(f.Key, "plugin_")
				if st.pluginMgr != nil {
					if st.pluginMgr.IsEnabled(pluginID) {
						_ = st.pluginMgr.DisablePlugin(pluginID)
					} else {
						_ = st.pluginMgr.EnablePlugin(pluginID)
					}
					_ = st.Save()
					if st.OnSettingsChanged != nil {
						st.OnSettingsChanged()
					}
					return
				}
				for i := range st.Current.Plugins {
					p := &st.Current.Plugins[i]
					if p.ID == pluginID {
						if !p.Installed {
							p.Installed = true
							p.Enabled = true
						} else {
							p.Enabled = !p.Enabled
						}
						break
					}
				}
				_ = st.Save()
				if st.OnSettingsChanged != nil {
					st.OnSettingsChanged()
				}
				return
			}
			for i := range st.Current.Plugins {
				p := &st.Current.Plugins[i]
				if p.ID == f.Key {
					if !p.Installed {
						p.Installed = true
						p.Enabled = true
					} else {
						p.Enabled = !p.Enabled
					}
					break
				}
			}
		}
	}
	_ = st.Save()
	if st.OnSettingsChanged != nil {
		st.OnSettingsChanged()
	}
}

// formatKeyDescription converts an input.Key to a canonical string (e.g. "Ctrl+S", "Alt+1").
func formatKeyDescription(k input.Key) string {
	var parts []string
	if k.HasCtrl() {
		parts = append(parts, "Ctrl")
	}
	if k.HasAlt() {
		parts = append(parts, "Alt")
	}
	if k.HasShift() {
		parts = append(parts, "Shift")
	}

	keyName := ""
	switch k.Type {
	case input.KeyF1:
		keyName = "F1"
	case input.KeyF2:
		keyName = "F2"
	case input.KeyF3:
		keyName = "F3"
	case input.KeyF4:
		keyName = "F4"
	case input.KeyF5:
		keyName = "F5"
	case input.KeyF6:
		keyName = "F6"
	case input.KeyF7:
		keyName = "F7"
	case input.KeyF8:
		keyName = "F8"
	case input.KeyF9:
		keyName = "F9"
	case input.KeyF10:
		keyName = "F10"
	case input.KeyF11:
		keyName = "F11"
	case input.KeyF12:
		keyName = "F12"
	case input.KeyEnter:
		keyName = "Enter"
	case input.KeyTab:
		keyName = "Tab"
	case input.KeySpace:
		keyName = "Space"
	case input.KeyBackspace:
		keyName = "Backspace"
	case input.KeyDelete:
		keyName = "Delete"
	case input.KeyUp:
		keyName = "Up"
	case input.KeyDown:
		keyName = "Down"
	case input.KeyLeft:
		keyName = "Left"
	case input.KeyRight:
		keyName = "Right"
	case input.KeyRune:
		if k.Rune >= 32 {
			keyName = strings.ToUpper(string(k.Rune))
		}
	}

	if keyName == "" {
		return ""
	}
	parts = append(parts, keyName)
	return strings.Join(parts, "+")
}

// HandleKey processes keyboard navigation inside the settings modal.
func (st *SettingsState) HandleKey(k input.Key) (bool, bool) { // (handled, shouldClose)
	if !st.Open {
		return false, false
	}

	// 0. Color Picker intercepts all keys when open
	if st.ColorPicker != nil && st.ColorPicker.Open {
		handled, shouldClose := st.ColorPicker.HandleKey(k)
		if shouldClose {
			st.ColorPicker.Open = false
		}
		if handled {
			return true, false
		}
	}

	// 0.05 Tool Action modal intercepts all keys when open
	if st.ToolAction.Open {
		handled, shouldClose := st.handleToolActionKey(k)
		if shouldClose {
			st.ToolAction.Open = false
		}
		if handled {
			return true, false
		}
	}

	// 0.1 Rebinding key shortcut
	if st.RebindingKey {
		if k.Type == input.KeyEsc {
			st.RebindingKey = false
			return true, false
		}
		shortcut := formatKeyDescription(k)
		if shortcut != "" {
			fields := st.getCategoryFields(st.CategoryIdx)
			if st.FieldIdx >= 0 && st.FieldIdx < len(fields) {
				f := fields[st.FieldIdx]
				st.Current.Keybindings[f.Key] = shortcut
				_ = st.Save()
				if st.OnSettingsChanged != nil {
					st.OnSettingsChanged()
				}
			}
			st.RebindingKey = false
			return true, false
		}
		return true, false
	}

	// 1. Text editing mode intercepts typing
	if st.EditingText {
		switch k.Type {
		case input.KeyEnter:
			st.setCurrentFieldValue(st.InputBuffer)
			st.EditingText = false
			return true, false

		case input.KeyEsc:
			st.EditingText = false
			return true, false

		case input.KeyBackspace:
			runes := []rune(st.InputBuffer)
			if len(runes) > 0 {
				st.InputBuffer = string(runes[:len(runes)-1])
			}
			return true, false

		case input.KeySpace:
			st.InputBuffer += " "
			return true, false

		case input.KeyRune:
			if k.Rune >= 32 {
				st.InputBuffer += string(k.Rune)
			}
			return true, false
		}
		return true, false
	}

	// 2. Modal navigation
	// Ctrl+S: Save and close modal safely
	if k.Rune == 19 || (k.HasCtrl() && (matchKey(k, 's', 'ы') || k.Rune == 's' || k.Rune == 'S')) {
		_ = SaveSettings(st.Current)
		st.Open = false
		return true, true
	}

	// Ctrl+R: Reset settings to default safely
	if k.Rune == 18 || (k.HasCtrl() && (matchKey(k, 'r', 'к') || k.Rune == 'r' || k.Rune == 'R')) {
		st.Current = DefaultSettings()
		_ = st.Save()
		if st.OnSettingsChanged != nil {
			st.OnSettingsChanged()
		}
		if st.OnThemeChanged != nil {
			st.OnThemeChanged(st.Current.Theme)
		}
		return true, false
	}

	switch k.Type {
	case input.KeyEsc:
		st.Open = false
		return true, true

	case input.KeyTab:
		st.FocusRight = !st.FocusRight
		return true, false

	case input.KeyUp:
		if !st.FocusRight {
			if st.CategoryIdx > 0 {
				st.CategoryIdx--
				st.FieldIdx = 0
			}
		} else {
			if st.FieldIdx > 0 {
				st.FieldIdx--
			}
		}
		return true, false

	case input.KeyDown:
		if !st.FocusRight {
			if st.CategoryIdx+1 < len(st.getCategories()) {
				st.CategoryIdx++
				st.FieldIdx = 0
			}
		} else {
			fields := st.getCategoryFields(st.CategoryIdx)
			if st.FieldIdx+1 < len(fields) {
				st.FieldIdx++
			}
		}
		return true, false

	case input.KeyEnter:
		if !st.FocusRight {
			st.FocusRight = true
			return true, false
		}
		// If on Keybindings category (Category 3), enter rebinding mode
		if st.CategoryIdx == 3 {
			st.RebindingKey = true
			return true, false
		}
		// If on Color Palette category (Category 4), open Color Picker Modal
		if st.CategoryIdx == 4 {
			fields := st.getCategoryFields(4)
			if st.FieldIdx >= 0 && st.FieldIdx < len(fields) {
				f := fields[st.FieldIdx]
				if st.ColorPicker == nil {
					st.ColorPicker = NewColorPickerModal()
				}
				saveAndApply := func(key, hexVal string) {
					if st.Current.CustomColors == nil {
						st.Current.CustomColors = DefaultCustomColors()
					}
					st.Current.CustomColors[key] = hexVal
					_ = st.Save()
					if st.OnColorApplied != nil {
						st.OnColorApplied(key, hexVal)
					}
				}
				st.ColorPicker.OpenForColor(f.Key, f.Label, f.Value, saveAndApply)
				st.ColorPicker.OnPreview = func(key, hexVal string) {
					if st.Current.CustomColors == nil {
						st.Current.CustomColors = DefaultCustomColors()
					}
					st.Current.CustomColors[key] = hexVal
					if st.OnColorApplied != nil {
						st.OnColorApplied(key, hexVal)
					}
				}
				return true, false
			}
		}
		if st.CategoryIdx == 5 {
			fields := st.getCategoryFields(5)
			if st.FieldIdx >= 0 && st.FieldIdx < len(fields) {
				f := fields[st.FieldIdx]
				if strings.HasPrefix(f.Key, "tool:") {
					st.openToolActionForField(f)
					return true, false
				}
			}
		}
		if st.isTextField() {
			st.EditingText = true
			st.InputBuffer = st.getCurrentFieldValue()
			return true, false
		}
		st.cycleCurrentField()
		return true, false

	case input.KeySpace, input.KeyLeft, input.KeyRight:
		if !st.FocusRight {
			if k.Type == input.KeyRight {
				st.FocusRight = true
			}
			return true, false
		}
		if st.CategoryIdx == 3 {
			st.RebindingKey = true
			return true, false
		}
		if st.CategoryIdx == 5 && k.Type == input.KeySpace {
			fields := st.getCategoryFields(5)
			if st.FieldIdx >= 0 && st.FieldIdx < len(fields) {
				f := fields[st.FieldIdx]
				if strings.HasPrefix(f.Key, "tool:") {
					st.openToolActionForField(f)
					return true, false
				}
			}
		}
		if st.CategoryIdx == 4 && k.Type == input.KeySpace {
			fields := st.getCategoryFields(4)
			if st.FieldIdx >= 0 && st.FieldIdx < len(fields) {
				f := fields[st.FieldIdx]
				if st.ColorPicker == nil {
					st.ColorPicker = NewColorPickerModal()
				}
				saveAndApply := func(key, hexVal string) {
					if st.Current.CustomColors == nil {
						st.Current.CustomColors = DefaultCustomColors()
					}
					st.Current.CustomColors[key] = hexVal
					_ = st.Save()
					if st.OnColorApplied != nil {
						st.OnColorApplied(key, hexVal)
					}
				}
				st.ColorPicker.OpenForColor(f.Key, f.Label, f.Value, saveAndApply)
				st.ColorPicker.OnPreview = func(key, hexVal string) {
					if st.Current.CustomColors == nil {
						st.Current.CustomColors = DefaultCustomColors()
					}
					st.Current.CustomColors[key] = hexVal
					if st.OnColorApplied != nil {
						st.OnColorApplied(key, hexVal)
					}
				}
				return true, false
			}
		}
		if !st.isTextField() {
			st.cycleCurrentField()
		} else if k.Type == input.KeySpace {
			st.EditingText = true
			st.InputBuffer = st.getCurrentFieldValue()
		}
		return true, false

	case input.KeyRune:
		switch k.Rune {
		case 'd', 'D':
			// Uninstall plugin or remove repo if on Plugins category
			if st.CategoryIdx == 6 && st.FocusRight {
				fields := st.getCategoryFields(st.CategoryIdx)
				if st.FieldIdx >= 0 && st.FieldIdx < len(fields) {
					f := fields[st.FieldIdx]
					if strings.HasPrefix(f.Key, "repo_") {
						repoID := strings.TrimPrefix(f.Key, "repo_")
						if st.pluginMgr != nil {
							_ = st.pluginMgr.RemoveRepository(repoID)
						}
						return true, false
					}
					if strings.HasPrefix(f.Key, "plugin_") {
						pluginID := strings.TrimPrefix(f.Key, "plugin_")
						if st.pluginMgr != nil {
							_ = st.pluginMgr.Uninstall(pluginID)
						}
						return true, false
					}
					for i := range st.Current.Plugins {
						if st.Current.Plugins[i].ID == f.Key {
							st.Current.Plugins[i].Installed = false
							st.Current.Plugins[i].Enabled = false
							break
						}
					}
				}
				return true, false
			}
		}
	}

	return true, false
}

// ParseHexColor parses a hex string like "#1e1e2e" or "1e1e2e" into uint32 0xRRGGBB.
func ParseHexColor(hex string, def uint32) uint32 {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 6 {
		val, err := strconv.ParseUint(hex, 16, 32)
		if err == nil {
			return uint32(val)
		}
	}
	return def
}

// ApplyCustomColors applies hex color overrides from settings to a Theme.
func (st *SettingsState) ApplyCustomColors(t *ui.Theme) {
	if st == nil || t == nil || st.Current.CustomColors == nil {
		return
	}
	for k, hexStr := range st.Current.CustomColors {
		switch k {
		case "bg":
			t.Background = ParseHexColor(hexStr, t.Background)
		case "fg":
			t.Foreground = ParseHexColor(hexStr, t.Foreground)
		case "sel_bg":
			t.SelectionBg = ParseHexColor(hexStr, t.SelectionBg)
		case "cursor":
			t.CursorLineBg = ParseHexColor(hexStr, t.CursorLineBg)
		case "gutter_bg":
			t.GutterBg = ParseHexColor(hexStr, t.GutterBg)
		case "line_number":
			t.LineNumber = ParseHexColor(hexStr, t.LineNumber)
		case "keyword":
			t.Keyword = ParseHexColor(hexStr, t.Keyword)
		case "function":
			t.Function = ParseHexColor(hexStr, t.Function)
		case "string":
			t.String = ParseHexColor(hexStr, t.String)
		case "type":
			t.Type = ParseHexColor(hexStr, t.Type)
		case "comment":
			t.Comment = ParseHexColor(hexStr, t.Comment)
		case "constant":
			t.Constant = ParseHexColor(hexStr, t.Constant)
		case "error":
			t.DiagnosticError = ParseHexColor(hexStr, t.DiagnosticError)
		case "warn":
			t.DiagnosticWarn = ParseHexColor(hexStr, t.DiagnosticWarn)
		case "border":
			t.BorderColor = ParseHexColor(hexStr, t.BorderColor)
		case "popup_bg":
			t.PopupBg = ParseHexColor(hexStr, t.PopupBg)
		case "popup_sel_bg":
			t.PopupSelBg = ParseHexColor(hexStr, t.PopupSelBg)
		}
	}
}

// getThemeHexColor returns the hex color string for a key based on the active theme.
func (st *SettingsState) getThemeHexColor(key string) string {
	var th ui.Theme
	themeName := strings.ToLower(st.Current.Theme)
	switch themeName {
	case "gruvbox", "gruvbox-dark":
		th = ui.GruvboxDark()
	case "tokyo", "tokyonight", "tokyo-night":
		th = ui.TokyoNight()
	case "catppuccin", "catppuccin-mocha":
		th = ui.CatppuccinMocha()
	default:
		if st.pluginMgr != nil {
			for tName, tCfg := range st.pluginMgr.InstalledThemes() {
				if strings.EqualFold(tName, themeName) {
					th = ui.ThemeFromMap(tName, ui.CatppuccinMocha(), tCfg.Colors)
					break
				}
			}
		}
		if th.Name == "" {
			th = ui.CatppuccinMocha()
		}
	}
	switch key {
	case "bg":
		return fmt.Sprintf("#%06x", th.Background)
	case "fg":
		return fmt.Sprintf("#%06x", th.Foreground)
	case "sel_bg":
		return fmt.Sprintf("#%06x", th.SelectionBg)
	case "cursor":
		return fmt.Sprintf("#%06x", th.CursorLineBg)
	case "gutter_bg":
		return fmt.Sprintf("#%06x", th.GutterBg)
	case "line_number":
		return fmt.Sprintf("#%06x", th.LineNumber)
	case "keyword":
		return fmt.Sprintf("#%06x", th.Keyword)
	case "function":
		return fmt.Sprintf("#%06x", th.Function)
	case "string":
		return fmt.Sprintf("#%06x", th.String)
	case "type":
		return fmt.Sprintf("#%06x", th.Type)
	case "comment":
		return fmt.Sprintf("#%06x", th.Comment)
	case "constant":
		return fmt.Sprintf("#%06x", th.Constant)
	case "error":
		return fmt.Sprintf("#%06x", th.DiagnosticError)
	case "warn":
		return fmt.Sprintf("#%06x", th.DiagnosticWarn)
	case "border":
		return fmt.Sprintf("#%06x", th.BorderColor)
	case "popup_bg":
		return fmt.Sprintf("#%06x", th.PopupBg)
	case "popup_sel_bg":
		return fmt.Sprintf("#%06x", th.PopupSelBg)
	case "occurrence_bg", "word_highlight":
		return fmt.Sprintf("#%06x", th.OccurrenceBg)
	default:
		return "#000000"
	}
}

func (st *SettingsState) openToolActionForField(f Field) {
	parts := strings.Split(f.Key, ":")
	if len(parts) < 4 {
		return
	}
	kind := parts[1]
	pluginID := parts[2]
	cmd := parts[3]

	pluginName := pluginID
	installCmd := ""
	if st.pluginMgr != nil {
		for _, p := range st.pluginMgr.InstalledPlugins() {
			if p.ID == pluginID {
				pluginName = p.Name
				switch kind {
				case "compiler", "runner":
					for _, l := range p.Languages {
						if l.BuildCmd == cmd || l.RunCmd == cmd {
							installCmd = l.InstallCmd
						}
					}
				case "lsp":
					if p.LSP != nil {
						installCmd = p.LSP.InstallCmd
					}
				case "dap":
					if p.DAP != nil {
						installCmd = p.DAP.InstallCmd
					}
				}
				break
			}
		}
	}

	custom := ""
	if st.Current.CustomToolPaths != nil {
		custom = st.Current.CustomToolPaths[cmd]
	}
	if custom == "" {
		switch cmd {
		case "gopls":
			custom = st.Current.GoplsPath
			if custom == "" {
				custom = st.Current.GoSDKPath
			}
		case "go":
			custom = st.Current.GoSDKPath
		case "pyright-langserver", "pyright":
			custom = st.Current.PyrightPath
		case "python":
			custom = st.Current.PythonPath
		case "rust-analyzer":
			custom = st.Current.RustAnalyzer
		case "cargo":
			custom = st.Current.CargoPath
		}
	}
	resolvedPath := ""
	ready := false
	if st.pluginMgr != nil {
		resolvedPath, ready = st.pluginMgr.FindToolPath(cmd, custom)
	}

	st.ToolAction = ToolActionState{
		Open:        true,
		ToolName:    cmd,
		PluginName:  pluginName,
		InstallCmd:  installCmd,
		CurrentPath: resolvedPath,
		Ready:       ready,
		InputMode:   false,
		InputPath:   resolvedPath,
	}
}

func (st *SettingsState) triggerToolInstall() {
	if st.ToolAction.InstallCmd == "" {
		return
	}
	cmdStr := st.ToolAction.InstallCmd
	toolName := st.ToolAction.ToolName
	if st.OnToolInstallStarted != nil {
		st.OnToolInstallStarted(toolName, cmdStr)
	}
	go func() {
		parts := strings.Fields(cmdStr)
		if len(parts) == 0 {
			return
		}
		bin := parts[0]
		if bin == "go" && st.Current.GoSDKPath != "" {
			bin = st.Current.GoSDKPath
		} else if st.pluginMgr != nil {
			if found, ok := st.pluginMgr.FindToolPath(bin, ""); ok {
				bin = found
			}
		}
		cmd := exec.Command(bin, parts[1:]...)
		out, err := cmd.CombinedOutput()
		success := (err == nil)
		errMsg := ""
		if err != nil {
			errMsg = strings.TrimSpace(string(out))
			if errMsg == "" {
				errMsg = err.Error()
			}
		} else {
			if st.pluginMgr != nil {
				if found, ok := st.pluginMgr.FindToolPath(toolName, ""); ok {
					if st.Current.CustomToolPaths == nil {
						st.Current.CustomToolPaths = make(map[string]string)
					}
					st.Current.CustomToolPaths[toolName] = found
					_ = st.Save()
				}
			}
		}
		if st.OnToolInstallFinished != nil {
			st.OnToolInstallFinished(toolName, success, errMsg)
		}
	}()
}

func (st *SettingsState) handleToolActionKey(k input.Key) (bool, bool) {
	if st.ToolAction.InputMode {
		switch k.Type {
		case input.KeyEsc:
			st.ToolAction.InputMode = false
			return true, false
		case input.KeyEnter:
			p := strings.TrimSpace(st.ToolAction.InputPath)
			if p != "" {
				if st.Current.CustomToolPaths == nil {
					st.Current.CustomToolPaths = make(map[string]string)
				}
				st.Current.CustomToolPaths[st.ToolAction.ToolName] = p
				switch st.ToolAction.ToolName {
				case "gopls":
					st.Current.GoplsPath = p
				case "go":
					st.Current.GoSDKPath = p
				case "pyright-langserver", "pyright":
					st.Current.PyrightPath = p
				case "python":
					st.Current.PythonPath = p
				case "rust-analyzer":
					st.Current.RustAnalyzer = p
				case "cargo":
					st.Current.CargoPath = p
				}
				_ = st.Save()
			}
			st.ToolAction.InputMode = false
			st.ToolAction.Open = false
			return true, true
		case input.KeyBackspace:
			runes := []rune(st.ToolAction.InputPath)
			if len(runes) > 0 {
				st.ToolAction.InputPath = string(runes[:len(runes)-1])
			}
			return true, false
		case input.KeySpace:
			st.ToolAction.InputPath += " "
			return true, false
		case input.KeyRune:
			if k.Rune >= 32 {
				st.ToolAction.InputPath += string(k.Rune)
			}
			return true, false
		}
		return true, false
	}

	switch k.Type {
	case input.KeyEsc:
		st.ToolAction.Open = false
		return true, true
	case input.KeyEnter:
		if st.ToolAction.InstallCmd != "" {
			st.triggerToolInstall()
		}
		st.ToolAction.Open = false
		return true, true
	case input.KeyRune:
		switch k.Rune {
		case 'd', 'D', 'в', 'В':
			if st.ToolAction.InstallCmd != "" {
				st.triggerToolInstall()
			}
			st.ToolAction.Open = false
			return true, true
		case 'p', 'P', 'з', 'З':
			st.ToolAction.InputMode = true
			st.ToolAction.InputPath = st.ToolAction.CurrentPath
			return true, false
		case 'r', 'R', 'к', 'К':
			if st.Current.CustomToolPaths != nil {
				delete(st.Current.CustomToolPaths, st.ToolAction.ToolName)
				switch st.ToolAction.ToolName {
				case "gopls":
					st.Current.GoplsPath = ""
				case "go":
					st.Current.GoSDKPath = ""
				case "pyright-langserver", "pyright":
					st.Current.PyrightPath = ""
				case "python":
					st.Current.PythonPath = ""
				case "rust-analyzer":
					st.Current.RustAnalyzer = ""
				case "cargo":
					st.Current.CargoPath = ""
				}
				_ = st.Save()
			}
			st.ToolAction.Open = false
			return true, true
		}
	}
	return true, false
}

// HandleToolActionClick processes mouse clicks inside the tool action dialog.
func (st *SettingsState) HandleToolActionClick(mouseX, mouseY, w, h int) bool {
	if !st.ToolAction.Open {
		return false
	}
	modalW := 68
	if modalW > w-4 {
		modalW = w - 4
	}
	modalH := 12
	if st.ToolAction.InputMode {
		modalH = 14
	}
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	// Outside click -> close
	if mouseX < startX || mouseX >= startX+modalW || mouseY < startY || mouseY >= startY+modalH {
		st.ToolAction.Open = false
		return true
	}

	// Click on [D: Download & Install] (startY+5)
	if mouseY == startY+5 {
		if st.ToolAction.InstallCmd != "" {
			st.triggerToolInstall()
		}
		st.ToolAction.Open = false
		return true
	}

	// Click on [P: Specify Path] (startY+6)
	if mouseY == startY+6 {
		st.ToolAction.InputMode = true
		st.ToolAction.InputPath = st.ToolAction.CurrentPath
		return true
	}

	// Click on [R: Reset] (startY+7)
	if mouseY == startY+7 {
		if st.Current.CustomToolPaths != nil {
			delete(st.Current.CustomToolPaths, st.ToolAction.ToolName)
			switch st.ToolAction.ToolName {
			case "gopls":
				st.Current.GoplsPath = ""
			case "go":
				st.Current.GoSDKPath = ""
			case "pyright-langserver", "pyright":
				st.Current.PyrightPath = ""
			case "python":
				st.Current.PythonPath = ""
			case "rust-analyzer":
				st.Current.RustAnalyzer = ""
			case "cargo":
				st.Current.CargoPath = ""
			}
			_ = st.Save()
		}
		st.ToolAction.Open = false
		return true
	}

	// Click on [Esc: Cancel] (startY+8)
	if mouseY == startY+8 {
		st.ToolAction.Open = false
		return true
	}

	return true
}

// HandleClick processes mouse clicks inside and outside the settings modal.
func (st *SettingsState) HandleClick(mouseX, mouseY, w, h int) (handled bool, shouldClose bool) {
	if !st.Open {
		return false, false
	}

	if st.ColorPicker != nil && st.ColorPicker.Open {
		handled, shouldClose := st.ColorPicker.HandleClick(mouseX, mouseY, w, h)
		if shouldClose {
			st.ColorPicker.Open = false
		}
		return handled, false
	}

	if st.ToolAction.Open {
		return st.HandleToolActionClick(mouseX, mouseY, w, h), false
	}

	modalW := 84
	if modalW > w-4 {
		modalW = w - 4
	}
	modalH := 20
	if modalH > h-4 {
		modalH = h - 4
	}
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	// Click outside -> close settings
	if mouseX < startX || mouseX >= startX+modalW || mouseY < startY || mouseY >= startY+modalH {
		st.Open = false
		return true, true
	}

	catW := 24
	divX := startX + catW

	// Click on left category list
	if mouseX >= startX+1 && mouseX < divX {
		for idx := range st.getCategories() {
			rowY := startY + 2 + idx
			if mouseY == rowY {
				st.CategoryIdx = idx
				st.FieldIdx = 0
				st.FocusRight = false
				return true, false
			}
		}
	}

	// Click on right fields form
	if mouseX >= divX+1 && mouseX < startX+modalW-1 {
		fields := st.getCategoryFields(st.CategoryIdx)
		visibleRows := modalH - 4
		scrollOffset := 0
		if st.FieldIdx >= visibleRows {
			scrollOffset = st.FieldIdx - visibleRows + 1
		}
		for row := 0; row < visibleRows; row++ {
			rowY := startY + 2 + row
			idx := scrollOffset + row
			if mouseY == rowY && idx < len(fields) {
				st.FieldIdx = idx
				st.FocusRight = true

				f := fields[idx]
				if st.CategoryIdx == 4 {
					// Open Color Picker
					saveAndApply := func(key, hexVal string) {
						if st.Current.CustomColors == nil {
							st.Current.CustomColors = DefaultCustomColors()
						}
						st.Current.CustomColors[key] = hexVal
						_ = st.Save()
						if st.OnColorApplied != nil {
							st.OnColorApplied(key, hexVal)
						}
					}
					if st.ColorPicker == nil {
						st.ColorPicker = NewColorPickerModal()
					}
					st.ColorPicker.OpenForColor(f.Key, f.Label, f.Value, saveAndApply)
					st.ColorPicker.OnPreview = func(key, hexVal string) {
						if st.Current.CustomColors == nil {
							st.Current.CustomColors = DefaultCustomColors()
						}
						st.Current.CustomColors[key] = hexVal
						if st.OnColorApplied != nil {
							st.OnColorApplied(key, hexVal)
						}
					}
					return true, false
				}
				if st.CategoryIdx == 5 && strings.HasPrefix(f.Key, "tool:") {
					st.openToolActionForField(f)
					return true, false
				}
				return true, false
			}
		}
	}

	return true, false
}

func (st *SettingsState) renderToolAction(buf *buffer.Buffer, w, h int, themeBg, themeFg, borderFg, selBg, selFg, accentFg cell.Color) {
	if !st.ToolAction.Open || buf == nil {
		return
	}

	modalW := 68
	if modalW > w-4 {
		modalW = w - 4
	}
	modalH := 12
	if st.ToolAction.InputMode {
		modalH = 14
	}
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	// Draw rounded frame
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
			buf.SetRune(startX+x, startY+y, ch, fg, themeBg, cell.AttrNone)
		}
	}

	// Title
	title := fmt.Sprintf(" Tool Action: %s ", st.ToolAction.ToolName)
	drawString(buf, startX+2, startY, title, accentFg, themeBg, cell.AttrBold, modalW-4)

	// Details
	statusStr := "[✓] Ready"
	if !st.ToolAction.Ready {
		statusStr = "[!] Missing"
	}
	line1 := fmt.Sprintf("Plugin: %s  |  Status: %s", st.ToolAction.PluginName, statusStr)
	drawString(buf, startX+3, startY+2, line1, themeFg, themeBg, cell.AttrNone, modalW-6)

	curPath := st.ToolAction.CurrentPath
	if curPath == "" {
		curPath = "(not found)"
	}
	line2 := fmt.Sprintf("Path: %s", curPath)
	line2Runes := []rune(line2)
	if len(line2Runes) > modalW-6 {
		line2 = string(line2Runes[:modalW-7]) + "…"
	}
	drawString(buf, startX+3, startY+3, line2, themeFg, themeBg, cell.AttrNone, modalW-6)

	// Options
	opt1 := fmt.Sprintf("  [D] Download & Install Automatically: %s", st.ToolAction.InstallCmd)
	if st.ToolAction.InstallCmd == "" {
		opt1 = "  [D] Download & Install Automatically"
	}
	opt1Runes := []rune(opt1)
	if len(opt1Runes) > modalW-6 {
		opt1Runes = append(opt1Runes[:modalW-7], '…')
	}
	for i, r := range opt1Runes {
		if startX+2+i < startX+modalW-2 {
			fg := accentFg
			if r == '[' || r == ']' || r == 'D' {
				fg = toColor(0xcba6f7) // keyword accent
			}
			buf.SetRune(startX+2+i, startY+5, r, fg, themeBg, cell.AttrBold)
		}
	}

	opt2 := "  [P] Specify Path to Binary Manually"
	for i, r := range []rune(opt2) {
		if startX+2+i < startX+modalW-2 {
			fg := themeFg
			if r == '[' || r == ']' || r == 'P' {
				fg = toColor(0xcba6f7)
			}
			buf.SetRune(startX+2+i, startY+6, r, fg, themeBg, cell.AttrBold)
		}
	}

	opt3 := "  [R] Reset / Revert to Auto-Detection"
	for i, r := range []rune(opt3) {
		if startX+2+i < startX+modalW-2 {
			fg := themeFg
			if r == '[' || r == ']' || r == 'R' {
				fg = toColor(0xcba6f7)
			}
			buf.SetRune(startX+2+i, startY+7, r, fg, themeBg, cell.AttrBold)
		}
	}

	opt4 := "  [Esc] Cancel / Back"
	drawString(buf, startX+2, startY+8, opt4, toColor(0x888888), themeBg, cell.AttrNone, modalW-4)

	if st.ToolAction.InputMode {
		inLbl := "Specify Path: "
		drawString(buf, startX+4, startY+10, inLbl, accentFg, themeBg, cell.AttrBold, modalW-8)
		inX := startX + 4 + len([]rune(inLbl))
		inW := modalW - 8 - len([]rune(inLbl))
		inValRunes := []rune(st.ToolAction.InputPath + "_")
		for i := 0; i < inW; i++ {
			r := ' '
			if i < len(inValRunes) {
				r = inValRunes[i]
			}
			buf.SetRune(inX+i, startY+10, r, selFg, selBg, cell.AttrBold)
		}
		hint := "Enter: Save Path | Esc: Cancel"
		drawString(buf, startX+4, startY+12, hint, themeFg, themeBg, cell.AttrNone, modalW-8)
	}
}
