package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tahr/internal/core"
	"tahr/internal/core/buffer"
	"tahr/internal/core/lsp"
	"tahr/internal/core/plugin"
)

func TestCleanDiagnosticMessage(t *testing.T) {
	// 1. Multiline Pyright diagnostic message
	multiline := "No overloads for \"sort_values\" match the provided arguments\n  Argument types: (list[str])\n  Overloads:\n    ..."
	cleaned := cleanDiagnosticMessage(multiline)

	if strings.Contains(cleaned, "\n") || strings.Contains(cleaned, "\r") {
		t.Fatalf("cleaned message must not contain newlines: %q", cleaned)
	}
	expectedFirstLine := "No overloads for \"sort_values\" match the provided arguments"
	if cleaned != expectedFirstLine {
		t.Fatalf("expected first line %q, got %q", expectedFirstLine, cleaned)
	}

	// 2. Control characters (e.g. carriage returns, tabs, escape runes)
	dirty := "Error\x1b[31m:\x00 missing parameter\r\nnext line"
	cleanedDirty := cleanDiagnosticMessage(dirty)
	if strings.Contains(cleanedDirty, "\n") || strings.Contains(cleanedDirty, "\x00") || strings.Contains(cleanedDirty, "\x1b") {
		t.Fatalf("cleanedDirty contains control runes: %q", cleanedDirty)
	}
}

func TestToolchainLabel_PythonAndVirtualenv(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	tempDir := t.TempDir()
	app.workspaceDir = tempDir

	// 1. Standard python file without venv
	pyDoc := core.NewDocument("doc1", filepath.Join(tempDir, "script.py"), buffer.NewBuffer())
	label := app.getToolchainLabel(pyDoc)
	if label != "Python" {
		t.Fatalf("expected 'Python', got %q", label)
	}

	// 2. Create .venv folder in workspace
	venvDir := filepath.Join(tempDir, ".venv")
	_ = os.MkdirAll(venvDir, 0755)

	labelVenv := app.getToolchainLabel(pyDoc)
	if labelVenv != "Python (.venv)" {
		t.Fatalf("expected 'Python (.venv)', got %q", labelVenv)
	}

	// 3. Go document
	goDoc := core.NewDocument("doc2", filepath.Join(tempDir, "main.go"), buffer.NewBuffer())
	labelGo := app.getToolchainLabel(goDoc)
	if !strings.Contains(labelGo, "Go") && !strings.Contains(labelGo, "go") {
		t.Fatalf("expected Go toolchain, got %q", labelGo)
	}

	// 4. Rust document
	rsDoc := core.NewDocument("doc3", filepath.Join(tempDir, "lib.rs"), buffer.NewBuffer())
	labelRs := app.getToolchainLabel(rsDoc)
	if labelRs != "Rust" {
		t.Fatalf("expected 'Rust', got %q", labelRs)
	}

	// 5. TypeScript / JavaScript document
	tsDoc := core.NewDocument("doc4", filepath.Join(tempDir, "app.ts"), buffer.NewBuffer())
	labelTs := app.getToolchainLabel(tsDoc)
	if labelTs != "TypeScript / JavaScript" {
		t.Fatalf("expected 'TypeScript / JavaScript', got %q", labelTs)
	}

	// 6. C/C++ document
	cDoc := core.NewDocument("doc5", filepath.Join(tempDir, "main.cpp"), buffer.NewBuffer())
	labelC := app.getToolchainLabel(cDoc)
	if labelC != "C/C++" {
		t.Fatalf("expected 'C/C++', got %q", labelC)
	}

	// 7. Plain text document
	txtDoc := core.NewDocument("doc6", filepath.Join(tempDir, "notes.txt"), buffer.NewBuffer())
	labelTxt := app.getToolchainLabel(txtDoc)
	if labelTxt != "Plain Text" {
		t.Fatalf("expected 'Plain Text', got %q", labelTxt)
	}

	// 8. Disabling python plugin should fall back to 'Plain Text'
	if app.pluginMgr != nil {
		_ = app.pluginMgr.DisablePlugin("tahr-python")
		labelDisabled := app.getToolchainLabel(pyDoc)
		if labelDisabled != "Plain Text" {
			t.Fatalf("expected 'Plain Text' when plugin is disabled, got %q", labelDisabled)
		}

		// Re-enabling returns toolchain label
		_ = app.pluginMgr.EnablePlugin("tahr-python")
		labelReenabled := app.getToolchainLabel(pyDoc)
		if labelReenabled != "Python (.venv)" {
			t.Fatalf("expected 'Python (.venv)' after re-enabling, got %q", labelReenabled)
		}
	}
}

func TestOnDiagnostics_SanitizationAndStatusBarSafety(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	tempDir := t.TempDir()
	testPy := filepath.Join(tempDir, "main.py")
	_ = os.WriteFile(testPy, []byte("print('hello')\n"), 0644)
	doc, err := eng.Open(testPy)
	if err != nil {
		t.Fatalf("eng.Open failed: %v", err)
	}
	_ = doc

	// Send multiline diagnostic from simulated LSP
	multilineDiag := []lsp.Diagnostic{
		{
			Range: lsp.Range{
				Start: lsp.Position{Line: 1, Character: 0},
				End:   lsp.Position{Line: 1, Character: 10},
			},
			Severity: lsp.SeverityError,
			Message:  "Argument types: (list[str])\n  Did you mean something else?\n  Details...",
		},
	}

	uri := "file:///" + filepath.ToSlash(testPy)
	app.OnDiagnostics(uri, multilineDiag)

	app.diagMu.RLock()
	cleanDiag := app.diagnostics[1]
	app.diagMu.RUnlock()

	if strings.Contains(cleanDiag, "\n") || strings.Contains(cleanDiag, "\r") {
		t.Fatalf("stored diagnostic must be single line, got %q", cleanDiag)
	}
	if cleanDiag != "Argument types: (list[str])" {
		t.Fatalf("expected 'Argument types: (list[str])', got %q", cleanDiag)
	}
}

func TestEnsureLSPForFile_SmartInstallResolved(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	tempDir := t.TempDir()
	mgr, _ := plugin.NewManager(tempDir)
	app.SetPluginManager(mgr)

	// Install declarative python plugin with fallback installCmd
	_, _ = mgr.InstallDeclarative(plugin.Manifest{
		ID:       "tahr-python",
		Name:     "Python Support",
		Version:  "1.0.0",
		Category: "lsp",
		LSP: &plugin.LSPConfig{
			ServerName: "pyright",
			Command:    "custom-pyright-nonexistent",
			InstallCmd: "npm install -g pyright || pip install pyright",
		},
		Languages: []plugin.LanguageConfig{
			{ID: "python", Extensions: []string{".py"}},
		},
	})

	testPy := filepath.Join(tempDir, "main.py")
	app.EnsureLSPForFile(testPy)

	// Tool prompt should be opened with a smart resolved command without '||'
	if app.toolPrompt == nil || !app.toolPrompt.Open {
		t.Fatalf("expected tool prompt to be open for missing tool")
	}

	if strings.Contains(app.toolPrompt.InstallCmd, "||") {
		t.Fatalf("toolPrompt.InstallCmd must not contain '||', got: %s", app.toolPrompt.InstallCmd)
	}
}
