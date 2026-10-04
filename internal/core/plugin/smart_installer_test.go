package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveSmartInstallCommand_PythonAlternatives(t *testing.T) {
	cmd := "npm install -g pyright || pip install pyright"
	bestCmd, alts := ResolveSmartInstallCommand(cmd, "pyright-langserver", nil)

	if strings.Contains(bestCmd, "||") {
		t.Fatalf("bestCmd must not contain '||': %s", bestCmd)
	}

	for _, alt := range alts {
		if strings.Contains(alt, "||") {
			t.Fatalf("alternative must not contain '||': %s", alt)
		}
	}

	// On Windows, pip or npm should be resolved cleanly without shell operators
	if !strings.HasPrefix(bestCmd, "pip") && !strings.HasPrefix(bestCmd, "npm") && !strings.HasPrefix(bestCmd, "python -m pip") {
		t.Fatalf("unexpected bestCmd for pyright: %s", bestCmd)
	}
}

func TestResolveSmartInstallCommand_ClangdOSDispatched(t *testing.T) {
	cmd := "winget install LLVM.LLVM || brew install llvm"
	bestCmd, _ := ResolveSmartInstallCommand(cmd, "clangd", nil)

	if strings.Contains(bestCmd, "||") {
		t.Fatalf("bestCmd must not contain '||': %s", bestCmd)
	}

	if runtime.GOOS == "windows" {
		if !strings.Contains(bestCmd, "winget") {
			t.Fatalf("expected winget on Windows, got %s", bestCmd)
		}
	} else if runtime.GOOS == "darwin" {
		if !strings.Contains(bestCmd, "brew") {
			t.Fatalf("expected brew on Darwin, got %s", bestCmd)
		}
	}
}

func TestFindToolPath_WorkspaceVenv(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	mgr.SetWorkspaceRoot(tempDir)
	if mgr.WorkspaceRoot() != tempDir {
		t.Fatalf("expected %s, got %s", tempDir, mgr.WorkspaceRoot())
	}

	// Simulate a virtualenv tool in .venv
	var scriptDir string
	if runtime.GOOS == "windows" {
		scriptDir = filepath.Join(tempDir, ".venv", "Scripts")
	} else {
		scriptDir = filepath.Join(tempDir, ".venv", "bin")
	}
	_ = os.MkdirAll(scriptDir, 0755)
	var mockToolFile string
	if runtime.GOOS == "windows" {
		mockToolFile = filepath.Join(scriptDir, "custom-lsp.cmd")
	} else {
		mockToolFile = filepath.Join(scriptDir, "custom-lsp")
	}
	_ = os.WriteFile(mockToolFile, []byte("@echo off\n"), 0755)

	// Verify FindToolPath finds the virtualenv tool!
	foundPath, ok := mgr.FindToolPath("custom-lsp", "")
	if !ok || foundPath == "" {
		t.Fatalf("expected to find custom-lsp in workspace .venv, got %s", foundPath)
	}

	// Verify FindToolPath does not panic on empty or non-existent tool
	path, ok := mgr.FindToolPath("non_existent_tool_xyz", "")
	if ok || path != "" {
		t.Fatalf("expected non_existent_tool_xyz to not be found")
	}

	// Test alias resolution
	aliasPath, ok2 := mgr.FindToolPath("pyright-langserver", "")
	// If pyright or pyright-langserver is on the host system, it should resolve cleanly
	if ok2 && aliasPath == "" {
		t.Fatalf("expected valid path when ok is true")
	}
	_ = scriptDir
}
