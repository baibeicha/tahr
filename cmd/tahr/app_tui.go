package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core"
	"tahr/internal/core/crash"
	"tahr/internal/core/logging"
	"tahr/internal/core/plugin"
	"tahr/internal/core/sdk"
	"tahr/internal/ui/tui"
)

func runTUI(projectDir string, files []string, themeName string) error {
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			reason := fmt.Sprintf("%v", r)
			recentLogs := logging.GetRecentLogs(200)
			dumpPath, err := crash.SaveCrashReport(Version, reason, stack, recentLogs)
			if err == nil {
				fmt.Fprintf(os.Stderr, "\n[TAHR CRASH] Unexpected panic recorded to: %s\nReason: %s\n", dumpPath, reason)
			} else {
				fmt.Fprintf(os.Stderr, "\n[TAHR CRASH] Unexpected panic: %s\nStack:\n%s\n", reason, stack)
			}
			panic(r)
		}
	}()

	eng := core.NewEngine()

	// Determine workspace root directory
	workspaceRoot := projectDir
	if workspaceRoot == "" {
		if len(files) > 0 {
			workspaceRoot = filepath.Dir(files[0])
		} else {
			workspaceRoot, _ = os.Getwd()
		}
	}
	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err == nil {
		workspaceRoot = absWorkspace
	}

	// Initialize file-only logging (strictly no stdout/stderr writes)
	_, _ = logging.Init(workspaceRoot)
	defer logging.Close()
	logging.Info("Initializing Tahr IDE TUI", "workspace", workspaceRoot)

	// Ensure canonical reference Go plugin exists in project and packages directory
	_ = plugin.EnsureReferenceGoPlugin(workspaceRoot)

	// Initialize Go SDK detection (detects real go version and prepends to PATH/GOROOT)
	sdkMgr := sdk.GetManager()
	logging.Info("Go SDK detected", "version", sdkMgr.GoVersionShort())

	// Initial untitled buffer ID
	initialUntitledID := ""
	if active := eng.ActiveDocument(); active != nil {
		initialUntitledID = active.ID
	}

	// Open files requested by the user
	openedCount := 0
	for _, f := range files {
		if strings.TrimSpace(f) == "" {
			continue
		}
		if _, err := eng.Open(f); err == nil {
			openedCount++
		}
	}

	// If no files passed directly, check for default project entry file
	if openedCount == 0 && workspaceRoot != "" {
		candidates := []string{
			filepath.Join(workspaceRoot, "main.go"),
			filepath.Join(workspaceRoot, "cmd", filepath.Base(workspaceRoot), "main.go"),
			filepath.Join(workspaceRoot, "app.py"),
			filepath.Join(workspaceRoot, "main.py"),
			filepath.Join(workspaceRoot, "src", "main.rs"),
			filepath.Join(workspaceRoot, "index.js"),
			filepath.Join(workspaceRoot, "README.md"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				if _, err := eng.Open(c); err == nil {
					openedCount++
					break
				}
			}
		}
	}

	// If at least one file was opened, close the initial blank untitled buffer
	if openedCount > 0 && initialUntitledID != "" {
		eng.CloseBuffer(initialUntitledID)
	}

	// Initialize TUI App Model
	app := tui.NewAppModel(eng)
	app.SetWorkspaceDir(workspaceRoot)
	app.SetSidebarOpen(true)
	app.EnableSplashScreen()

	// Scan workspace asynchronously for Omnibar file switcher (Ctrl+P) to ensure instant startup
	go func() {
		workspaceFiles := tui.ScanWorkspaceFiles(workspaceRoot, 2500)
		if len(workspaceFiles) > 0 {
			app.SetProjectFiles(workspaceFiles)
		}
	}()

	// Initialize Plugin Manager and discover installed language servers / extensions
	var pluginMgr *plugin.Manager
	if mgr, err := plugin.NewManager(""); err == nil {
		pluginMgr = mgr
		defer pluginMgr.Close()
		pluginMgr.SetEditorHost(eng)
		app.SetPluginManager(pluginMgr)
	}

	// Set theme: if explicit CLI flag was passed, override the theme from settings.
	if strings.TrimSpace(themeName) != "" {
		app.SetThemeByName(themeName)
	}

	// Ensure LSP initialized for active document
	activeDoc := eng.ActiveDocument()
	if activeDoc != nil && activeDoc.FilePath != "" {
		app.EnsureLSPForFile(activeDoc.FilePath)
	}

	// Launch GoatUI TEA Loop with Kitty disambiguation and Ctrl+C catching
	p := tea.NewProgram(app, tea.WithKittyKeyboard(), tea.WithCatchCtrlC(true))
	app.SetProgram(p)
	_, err = p.Run()
	if err != nil {
		return fmt.Errorf("tui runtime error: %w", err)
	}

	return nil
}
