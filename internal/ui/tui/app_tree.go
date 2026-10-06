package tui

import (
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core/gitlens"
	"tahr/internal/core/plugin"
)

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

// SetWorkspaceDir changes the active project root and rescans tree and files.
func (m *AppModel) SetWorkspaceDir(dir string) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		m.workspaceDir = abs
	} else {
		m.workspaceDir = dir
	}
	if m.aiEngine != nil {
		m.aiEngine.SetWorkspaceDir(m.workspaceDir)
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

	if m.bookmarkStore != nil {
		m.bookmarkStore.SetWorkspaceDir(abs)
		_ = m.bookmarkStore.Load()
	}
	if m.todoPanel != nil {
		m.todoPanel.WorkspaceDir = abs
	}
	if m.testRunnerPanel != nil {
		m.testRunnerPanel.WorkspaceDir = abs
	}
	if m.dockerPanel != nil {
		m.dockerPanel.WorkspaceDir = abs
	}
	if m.jupyterPanel != nil {
		m.jupyterPanel.WorkspaceDir = abs
	}
	if m.gitlensTracker != nil {
		m.gitlensTracker = gitlens.NewGutterTracker()
	}

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

