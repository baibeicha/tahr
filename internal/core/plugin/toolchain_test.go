package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginToolchainResolution(t *testing.T) {
	tempPlugins := t.TempDir()
	mgr, err := NewManager(tempPlugins)
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}

	// 1. Install custom declarative plugin with toolchain config
	zigManifest := Manifest{
		ID:          "custom-zig",
		Name:        "Zig Language Support",
		Version:     "1.0.0",
		Toolchain: &ToolchainConfig{
			Name: "Zig",
		},
		Languages: []LanguageConfig{
			{
				ID:         "zig",
				Extensions: []string{".zig", ".zon"},
			},
		},
	}
	if _, err := mgr.InstallDeclarative(zigManifest); err != nil {
		t.Fatalf("failed to install custom-zig: %v", err)
	}

	// Verify Zig toolchain label
	label, ok := mgr.GetToolchainLabel(".zig", "main.zig", "")
	if !ok || label != "Zig" {
		t.Fatalf("expected ('Zig', true), got (%q, %v)", label, ok)
	}

	// 2. Disable custom-zig -> should return ("", false)
	if err := mgr.DisablePlugin("custom-zig"); err != nil {
		t.Fatalf("failed to disable custom-zig: %v", err)
	}
	labelDisabled, okDisabled := mgr.GetToolchainLabel(".zig", "main.zig", "")
	if okDisabled || labelDisabled != "" {
		t.Fatalf("expected ('', false) for disabled plugin, got (%q, %v)", labelDisabled, okDisabled)
	}

	// Re-enable custom-zig
	if err := mgr.EnablePlugin("custom-zig"); err != nil {
		t.Fatalf("failed to enable custom-zig: %v", err)
	}
	labelReenabled, okReenabled := mgr.GetToolchainLabel(".zig", "main.zig", "")
	if !okReenabled || labelReenabled != "Zig" {
		t.Fatalf("expected ('Zig', true) after re-enable, got (%q, %v)", labelReenabled, okReenabled)
	}

	// 3. Dynamic SDK resolution (e.g. Go version provider)
	goManifest := Manifest{
		ID:      "test-go",
		Name:    "Go Test Support",
		Version: "1.0.0",
		Toolchain: &ToolchainConfig{
			Name:       "Go",
			DynamicSDK: "go",
		},
		Languages: []LanguageConfig{
			{
				ID:         "go",
				Extensions: []string{".go"},
				Filenames:  []string{"go.mod"},
			},
		},
	}
	if _, err := mgr.InstallDeclarative(goManifest); err != nil {
		t.Fatalf("failed to install test-go: %v", err)
	}

	// Without provider -> returns base name "Go"
	labelGo, okGo := mgr.GetToolchainLabel(".go", "main.go", "")
	if !okGo || labelGo != "Go" {
		t.Fatalf("expected ('Go', true) without dynamic provider, got (%q, %v)", labelGo, okGo)
	}

	// With provider registered
	mgr.SetDynamicSDKProvider(func(sdkID string) string {
		if sdkID == "go" {
			return "go1.26.2"
		}
		return ""
	})
	labelGoDyn, okGoDyn := mgr.GetToolchainLabel(".go", "main.go", "")
	if !okGoDyn || labelGoDyn != "go1.26.2" {
		t.Fatalf("expected ('go1.26.2', true) with dynamic provider, got (%q, %v)", labelGoDyn, okGoDyn)
	}

	// Match by filename: go.mod
	labelMod, okMod := mgr.GetToolchainLabel(".mod", "go.mod", "")
	if !okMod || labelMod != "go1.26.2" {
		t.Fatalf("expected ('go1.26.2', true) for go.mod, got (%q, %v)", labelMod, okMod)
	}

	// 4. Virtual environment detection with env_markers and env_variable
	pyManifest := Manifest{
		ID:      "test-py",
		Name:    "Python Test Support",
		Version: "1.0.0",
		Toolchain: &ToolchainConfig{
			Name:        "Python",
			EnvMarkers:  []string{".venv", "venv", "env"},
			EnvVariable: "VIRTUAL_ENV",
			EnvSuffix:   " (.venv)",
		},
		Languages: []LanguageConfig{
			{
				ID:         "python",
				Extensions: []string{".py"},
			},
		},
	}
	if _, err := mgr.InstallDeclarative(pyManifest); err != nil {
		t.Fatalf("failed to install test-py: %v", err)
	}

	wsDir := t.TempDir()
	// No venv yet
	labelPy, okPy := mgr.GetToolchainLabel(".py", "main.py", wsDir)
	if !okPy || labelPy != "Python" {
		t.Fatalf("expected ('Python', true), got (%q, %v)", labelPy, okPy)
	}

	// With .venv directory
	_ = os.MkdirAll(filepath.Join(wsDir, ".venv"), 0755)
	labelPyVenv, okPyVenv := mgr.GetToolchainLabel(".py", "main.py", wsDir)
	if !okPyVenv || labelPyVenv != "Python (.venv)" {
		t.Fatalf("expected ('Python (.venv)', true), got (%q, %v)", labelPyVenv, okPyVenv)
	}

	// With VIRTUAL_ENV environment variable
	wsDir2 := t.TempDir()
	t.Setenv("VIRTUAL_ENV", filepath.Join(wsDir2, "myenv"))
	labelPyEnvVar, okPyEnvVar := mgr.GetToolchainLabel(".py", "main.py", wsDir2)
	if !okPyEnvVar || labelPyEnvVar != "Python (.venv)" {
		t.Fatalf("expected ('Python (.venv)', true) via VIRTUAL_ENV, got (%q, %v)", labelPyEnvVar, okPyEnvVar)
	}

	// 5. Unknown extension returns false
	labelUnknown, okUnknown := mgr.GetToolchainLabel(".foobar", "test.foobar", "")
	if okUnknown || labelUnknown != "" {
		t.Fatalf("expected ('', false) for unknown extension, got (%q, %v)", labelUnknown, okUnknown)
	}
}
