package keymaps

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Profile represents a hotkey preset family.
type Profile string

const (
	ProfileVSCode    Profile = "vscode"
	ProfileJetBrains Profile = "jetbrains"
	ProfileEmacs     Profile = "emacs"
)

// Profiles returns the supported keymap profiles.
func Profiles() []Profile {
	return []Profile{ProfileVSCode, ProfileJetBrains, ProfileEmacs}
}

// vscodeBindings returns the default VS Code keymap layout.
func vscodeBindings() map[string]string {
	return map[string]string{
		"save":               "Ctrl+S",
		"run":                "F5",
		"build":              "F7",
		"tree_toggle":        "F2",
		"tree_dock":          "Ctrl+Alt+E",
		"split_cycle":        "Ctrl+\\",
		"split_next":         "Alt+Right",
		"split_prev":         "Alt+Left",
		"split_1":            "Alt+1",
		"split_2":            "Alt+2",
		"split_3":            "Alt+3",
		"split_4":            "Alt+4",
		"split_5":            "Alt+5",
		"split_6":            "Alt+6",
		"find":               "Ctrl+F",
		"replace":            "Ctrl+H",
		"search_in_files":    "Ctrl+Shift+F",
		"rename":             "F2",
		"goto_line":          "Ctrl+G",
		"omnibar":            "Ctrl+P",
		"commands":           "Ctrl+Shift+P",
		"undo":               "Ctrl+Z",
		"redo":               "Ctrl+Y",
		"copy":               "Ctrl+C",
		"cut":                "Ctrl+X",
		"paste":              "Ctrl+V",
		"select_all":         "Ctrl+A",
		"next_match":         "Ctrl+D",
		"tab_next":           "Ctrl+Tab",
		"tab_prev":           "Ctrl+Shift+Tab",
		"breakpoint":         "F9",
		"step_over":          "F10",
		"step_into":          "F11",
		"step_out":           "Shift+F11",
		"definition":         "F12",
		"hover":              "Shift+K",
		"settings":           "Ctrl+,",
		"close_tab":          "Ctrl+W",
		"quit":               "Ctrl+Q",
		"terminal_toggle":    "F4",
		"terminal_split":     "Ctrl+Shift+5",
		"terminal_tab_new":   "Ctrl+Shift+T",
		"terminal_tab_close": "Ctrl+Shift+W",
		"quick_fix":          "Alt+Enter",
		"minimap_toggle":     "Alt+M",
		"markdown_preview":   "Ctrl+Shift+M",
		"git_modal":          "Ctrl+Shift+G",
		"marketplace":        "Ctrl+Shift+X",
		"completion":         "Ctrl+Space",
		"pick_color":         "Alt+C",
		"problems":           "Ctrl+Shift+P",
		"format":             "Shift+Alt+F",
	}
}

// jetbrainsBindings returns the JetBrains IntelliJ/GoLand keymap layout.
func jetbrainsBindings() map[string]string {
	return map[string]string{
		"save":               "Ctrl+S",
		"run":                "Shift+F10",
		"build":              "Ctrl+F9",
		"tree_toggle":        "Alt+1",
		"tree_dock":          "Ctrl+Alt+E",
		"split_cycle":        "Ctrl+\\",
		"split_next":         "Alt+Right",
		"split_prev":         "Alt+Left",
		"split_1":            "Alt+1",
		"split_2":            "Alt+2",
		"split_3":            "Alt+3",
		"split_4":            "Alt+4",
		"split_5":            "Alt+5",
		"split_6":            "Alt+6",
		"find":               "Ctrl+F",
		"replace":            "Ctrl+R",
		"search_in_files":    "Ctrl+Shift+F",
		"rename":             "Shift+F6",
		"goto_line":          "Ctrl+G",
		"omnibar":            "Ctrl+Shift+N",
		"commands":           "Ctrl+Shift+A",
		"undo":               "Ctrl+Z",
		"redo":               "Ctrl+Shift+Z",
		"copy":               "Ctrl+C",
		"cut":                "Ctrl+X",
		"paste":              "Ctrl+V",
		"select_all":         "Ctrl+A",
		"next_match":         "Alt+J",
		"tab_next":           "Alt+Right",
		"tab_prev":           "Alt+Left",
		"breakpoint":         "Ctrl+F8",
		"step_over":          "F8",
		"step_into":          "F7",
		"step_out":           "Shift+F8",
		"definition":         "Ctrl+B",
		"hover":              "Ctrl+Q",
		"settings":           "Ctrl+Alt+S",
		"close_tab":          "Ctrl+F4",
		"quit":               "Ctrl+Q",
		"terminal_toggle":    "Alt+F12",
		"terminal_split":     "Ctrl+Shift+5",
		"terminal_tab_new":   "Ctrl+Shift+T",
		"terminal_tab_close": "Ctrl+Shift+W",
		"quick_fix":          "Alt+Enter",
		"minimap_toggle":     "Alt+M",
		"markdown_preview":   "Ctrl+Shift+M",
		"git_modal":          "Ctrl+K",
		"marketplace":        "Ctrl+Shift+X",
		"completion":         "Ctrl+Space",
		"pick_color":         "Alt+C",
		"problems":           "Alt+6",
		"format":             "Ctrl+Alt+L",
	}
}

// emacsBindings returns the Emacs keymap layout.
func emacsBindings() map[string]string {
	return map[string]string{
		"save":               "Ctrl+X Ctrl+S",
		"run":                "Ctrl+C Ctrl+C",
		"build":              "Ctrl+C Ctrl+B",
		"tree_toggle":        "Ctrl+C Ctrl+D",
		"tree_dock":          "Ctrl+X D",
		"split_cycle":        "Ctrl+X O",
		"split_next":         "Ctrl+X Right",
		"split_prev":         "Ctrl+X Left",
		"split_1":            "Ctrl+X 1",
		"split_2":            "Ctrl+X 2",
		"split_3":            "Ctrl+X 3",
		"split_4":            "Ctrl+X 4",
		"split_5":            "Ctrl+X 5",
		"split_6":            "Ctrl+X 6",
		"find":               "Ctrl+S",
		"replace":            "Alt+%",
		"search_in_files":    "Ctrl+C S",
		"rename":             "Ctrl+C R",
		"goto_line":          "Alt+G G",
		"omnibar":            "Ctrl+X Ctrl+F",
		"commands":           "Alt+X",
		"undo":               "Ctrl+_",
		"redo":               "Ctrl+G Ctrl+_",
		"copy":               "Alt+W",
		"cut":                "Ctrl+W",
		"paste":              "Ctrl+Y",
		"select_all":         "Ctrl+X H",
		"next_match":         "Ctrl+S",
		"tab_next":           "Ctrl+X Right",
		"tab_prev":           "Ctrl+X Left",
		"breakpoint":         "F9",
		"step_over":          "F10",
		"step_into":          "F11",
		"step_out":           "Shift+F11",
		"definition":         "Alt+.",
		"hover":              "Ctrl+C H",
		"settings":           "Ctrl+C ,",
		"close_tab":          "Ctrl+X K",
		"quit":               "Ctrl+X Ctrl+C",
		"terminal_toggle":    "F4",
		"terminal_split":     "Ctrl+Shift+5",
		"terminal_tab_new":   "Ctrl+Shift+T",
		"terminal_tab_close": "Ctrl+Shift+W",
		"quick_fix":          "Alt+Enter",
		"minimap_toggle":     "Alt+M",
		"markdown_preview":   "Ctrl+Shift+M",
		"git_modal":          "Ctrl+X V",
		"marketplace":        "Ctrl+Shift+X",
		"completion":         "Alt+/",
		"pick_color":         "Alt+C",
		"problems":           "Ctrl+C X",
		"format":             "Ctrl+C F",
	}
}

// GetProfileBindings returns hotkeys for the given profile.
func GetProfileBindings(p Profile) map[string]string {
	switch p {
	case ProfileJetBrains:
		return jetbrainsBindings()
	case ProfileEmacs:
		return emacsBindings()
	case ProfileVSCode:
		fallthrough
	default:
		return vscodeBindings()
	}
}

// MergeBindings combines base bindings with user overrides.
func MergeBindings(base, overrides map[string]string) map[string]string {
	res := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		res[k] = v
	}
	for k, v := range overrides {
		if v != "" {
			res[k] = v
		}
	}
	return res
}

// LoadCustomKeymaps reads user keybindings from keymaps.json.
func LoadCustomKeymaps(dir string) (map[string]string, error) {
	fp := filepath.Join(dir, "keymaps.json")
	data, err := os.ReadFile(fp)
	if err != nil {
		return nil, err
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveCustomKeymaps persists user keybindings to keymaps.json.
func SaveCustomKeymaps(dir string, bindings map[string]string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	fp := filepath.Join(dir, "keymaps.json")
	data, err := json.MarshalIndent(bindings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fp, data, 0644)
}
