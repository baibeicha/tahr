package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core"
	"tahr/internal/core/i18n"
	"tahr/internal/core/launch"
	"tahr/internal/core/plugin"
	"tahr/internal/ui"
)

// TestShellProfileExecution verifies that a shell profile with target "." and preLaunchTask
// routes to the integrated terminal and executes the preLaunchTask as command.
func TestShellProfileExecution(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	p := launch.Profile{
		Name:          "Lines count",
		Type:          "shell",
		Request:       "launch",
		Target:        ".",
		Console:       "integratedTerminal",
		PreLaunchTask: "fflow stats -re .go",
	}

	m.runProfile(p)

	if m.terminal == nil || !m.terminal.Open {
		t.Fatalf("expected terminal to be open")
	}
	if !m.terminalFocused {
		t.Fatalf("expected terminal to be focused")
	}

	m.terminal.mu.Lock()
	lastCmd := m.terminal.CurrentTask
	m.terminal.mu.Unlock()

	if lastCmd != "fflow stats -re .go" {
		t.Fatalf("expected terminal command to be 'fflow stats -re .go', got %q", lastCmd)
	}
}

// TestShellProfileSequentialExecution verifies that when both PreLaunchTask and Target are set,
// they are chained with && in the terminal.
func TestShellProfileSequentialExecution(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	p := launch.Profile{
		Name:          "Build & Run",
		Type:          "shell",
		Request:       "launch",
		Target:        "go test ./...",
		Console:       "integratedTerminal",
		PreLaunchTask: "go vet ./...",
	}

	m.runProfile(p)

	if m.terminal == nil || !m.terminal.Open {
		t.Fatalf("expected terminal to be open")
	}

	m.terminal.mu.Lock()
	lastCmd := m.terminal.CurrentTask
	m.terminal.mu.Unlock()

	expected := "go vet ./... && go test ./..."
	if lastCmd != expected {
		t.Fatalf("expected chained command %q, got %q", expected, lastCmd)
	}
}

// TestLaunchModal_ShellCommandLabel verifies that the Launch modal displays "Command:"
// for shell profiles in both form and details views.
func TestLaunchModal_ShellCommandLabel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-shell-lbl-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	i18n.SetLocale("en")

	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testapp\n"), 0644)
	cfg, _ := launch.Load(tmpDir)

	// Add custom shell configuration
	cfg.Configurations = append(cfg.Configurations, launch.Profile{
		Name:    "Custom Shell",
		Type:    "shell",
		Target:  "git status",
		Console: "integratedTerminal",
	})

	modal := NewLaunchConfigModal(cfg)
	modal.OpenModal(cfg)
	modal.SelectedIdx = len(cfg.Configurations) - 1

	buf := buffer.NewBuffer(120, 40)
	theme := ui.CatppuccinMocha()
	modal.Render(buf, 120, 40, &theme)

	// Scan buffer for "Command:" in details view
	foundCommand := false
	for y := 0; y < 40; y++ {
		var line strings.Builder
		for x := 0; x < 120; x++ {
			c := buf.Cell(x, y)
			if c.Rune != 0 {
				line.WriteRune(c.Rune)
			} else {
				line.WriteRune(' ')
			}
		}
		if strings.Contains(line.String(), "Command:") {
			foundCommand = true
			break
		}
	}

	if !foundCommand {
		t.Fatalf("expected details view to render 'Command:' for shell profile")
	}

	// Now switch to edit mode
	modal.startEdit()
	buf = buffer.NewBuffer(120, 40)
	modal.Render(buf, 120, 40, &theme)

	foundFormCommand := false
	for y := 0; y < 40; y++ {
		var line strings.Builder
		for x := 0; x < 120; x++ {
			c := buf.Cell(x, y)
			if c.Rune != 0 {
				line.WriteRune(c.Rune)
			} else {
				line.WriteRune(' ')
			}
		}
		if strings.Contains(line.String(), "Command:") {
			foundFormCommand = true
			break
		}
	}

	if !foundFormCommand {
		t.Fatalf("expected form view to render 'Command:' for shell profile")
	}
}

// TestDefaultRepositories_OnlyOfficialGithub verifies that only the single official GitHub repo exists.
func TestDefaultRepositories_OnlyOfficialGithub(t *testing.T) {
	repos := plugin.DefaultRepositories()
	if len(repos) != 1 {
		t.Fatalf("expected exactly 1 default repository, got %d", len(repos))
	}
	if repos[0].ID != "official-github" {
		t.Fatalf("expected repository ID 'official-github', got %q", repos[0].ID)
	}
	if !strings.Contains(repos[0].URL, "baibeicha/tahr") {
		t.Fatalf("expected URL pointing to baibeicha/tahr, got %q", repos[0].URL)
	}
}
