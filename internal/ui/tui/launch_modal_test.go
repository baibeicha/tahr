package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/launch"
	"tahr/internal/ui"
)

func TestLaunchConfigModal_NavigationAndActive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-launch-modal-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testapp\n"), 0644)
	cfg, err := launch.Load(tmpDir)
	if err != nil {
		t.Fatalf("launch load failed: %v", err)
	}

	modal := NewLaunchConfigModal(cfg)
	modal.OpenModal(cfg)

	if !modal.Open {
		t.Fatal("expected modal to be open")
	}

	// Move down
	handled, close := modal.HandleKey(input.Key{Type: input.KeyDown})
	if !handled || close {
		t.Errorf("expected handled and not closed on KeyDown")
	}
	if modal.SelectedIdx != 1 {
		t.Errorf("expected selected idx 1, got %d", modal.SelectedIdx)
	}

	// Set active with Space
	handled, close = modal.HandleKey(input.Key{Type: input.KeySpace})
	if !handled || close {
		t.Errorf("expected handled on Space")
	}
	if cfg.ActiveProfile != cfg.Configurations[1].Name {
		t.Errorf("expected active profile to be %s, got %s", cfg.Configurations[1].Name, cfg.ActiveProfile)
	}

	// Move back up
	modal.HandleKey(input.Key{Type: input.KeyUp})
	if modal.SelectedIdx != 0 {
		t.Errorf("expected selected idx 0, got %d", modal.SelectedIdx)
	}
}

func TestLaunchConfigModal_LaunchAndDebugCallbacks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-launch-cb-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testapp\n"), 0644)
	cfg, _ := launch.Load(tmpDir)

	modal := NewLaunchConfigModal(cfg)
	modal.OpenModal(cfg)

	launched := false
	var launchedProfile launch.Profile
	modal.OnLaunch = func(p launch.Profile) {
		launched = true
		launchedProfile = p
	}

	debugged := false
	var debuggedProfile launch.Profile
	modal.OnDebug = func(p launch.Profile) {
		debugged = true
		debuggedProfile = p
	}

	// Press F5 to launch
	handled, shouldClose := modal.HandleKey(input.Key{Type: input.KeyF5})
	if !handled || !shouldClose {
		t.Fatalf("expected handled and shouldClose on F5")
	}
	if !launched || launchedProfile.Name != cfg.Configurations[0].Name {
		t.Fatalf("expected launch callback invoked with profile %s", cfg.Configurations[0].Name)
	}
	if modal.Open {
		t.Fatalf("expected modal to close on launch")
	}

	// Reopen and test F9 for debug
	modal.OpenModal(cfg)
	modal.SelectedIdx = 1
	handled, shouldClose = modal.HandleKey(input.Key{Type: input.KeyF9})
	if !handled || !shouldClose {
		t.Fatalf("expected handled and shouldClose on F9")
	}
	if !debugged || debuggedProfile.Name != cfg.Configurations[1].Name {
		t.Fatalf("expected debug callback invoked with profile %s", cfg.Configurations[1].Name)
	}
}

func TestLaunchConfigModal_AddEditDelete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-launch-aed-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg, _ := launch.Load(tmpDir)
	modal := NewLaunchConfigModal(cfg)
	modal.OpenModal(cfg)

	initCount := len(cfg.Configurations)

	// Press 'a' to add
	modal.HandleKey(input.Key{Rune: 'a'})
	if modal.Mode != LaunchModeAdd {
		t.Fatalf("expected LaunchModeAdd")
	}

	// Change name buffer
	modal.InputBuffer = "My Custom Runner"
	// Save with Ctrl+S (ASCII 19)
	modal.HandleKey(input.Key{Rune: 19})
	if modal.Mode != LaunchModeList {
		t.Fatalf("expected return to LaunchModeList after save")
	}
	if len(cfg.Configurations) != initCount+1 {
		t.Fatalf("expected %d configurations after add, got %d", initCount+1, len(cfg.Configurations))
	}
	if cfg.Configurations[len(cfg.Configurations)-1].Name != "My Custom Runner" {
		t.Fatalf("expected name 'My Custom Runner', got %s", cfg.Configurations[len(cfg.Configurations)-1].Name)
	}

	// Edit the new profile: press 'e'
	modal.SelectedIdx = len(cfg.Configurations) - 1
	modal.HandleKey(input.Key{Rune: 'e'})
	if modal.Mode != LaunchModeEdit {
		t.Fatalf("expected LaunchModeEdit")
	}
	modal.InputBuffer = "Edited Runner"
	modal.HandleKey(input.Key{Rune: 19}) // Save
	if cfg.Configurations[len(cfg.Configurations)-1].Name != "Edited Runner" {
		t.Fatalf("expected name 'Edited Runner', got %s", cfg.Configurations[len(cfg.Configurations)-1].Name)
	}

	// Delete profile: press 'd'
	modal.HandleKey(input.Key{Rune: 'd'})
	if len(cfg.Configurations) != initCount {
		t.Fatalf("expected %d configurations after delete, got %d", initCount, len(cfg.Configurations))
	}
}

func TestLaunchConfigModal_Render(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-launch-render-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg, _ := launch.Load(tmpDir)
	modal := NewLaunchConfigModal(cfg)
	modal.OpenModal(cfg)

	buf := buffer.NewBuffer(80, 24)
	theme := ui.CatppuccinMocha()

	// Render in list mode
	modal.Render(buf, 80, 24, &theme)

	// Render in edit mode
	modal.startEdit()
	modal.Render(buf, 80, 24, &theme)

	// Should not panic or crash
}

func TestLaunchConfigModal_InteractiveNewButtonAndMouse(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-launch-interactive-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg, _ := launch.Load(tmpDir)
	modal := NewLaunchConfigModal(cfg)
	modal.SetSupportedTypes([]string{"go", "custom_lang", "shell"})
	modal.OpenModal(cfg)

	cfgCount := len(cfg.Configurations)

	// 1. Navigate to the last item and then to "+ New from template"
	for i := 0; i < cfgCount; i++ {
		modal.HandleKey(input.Key{Type: input.KeyDown})
	}
	if modal.SelectedIdx != cfgCount {
		t.Fatalf("expected selected index %d (+ New from template), got %d", cfgCount, modal.SelectedIdx)
	}

	// Pressing Enter on "+ New from template" should trigger LaunchModeAdd
	modal.HandleKey(input.Key{Type: input.KeyEnter})
	if modal.Mode != LaunchModeAdd {
		t.Fatalf("expected LaunchModeAdd after Enter on + New, got %v", modal.Mode)
	}

	// 2. Test typing custom type and cycling through dynamic supported types
	modal.EditFieldIdx = 1 // Type field
	modal.cycleType(1)
	if modal.EditProfile.Type != "go" && modal.EditProfile.Type != "custom_lang" && modal.EditProfile.Type != "shell" {
		t.Fatalf("expected supported type, got %s", modal.EditProfile.Type)
	}

	// Test direct typing of custom type
	modal.InputBuffer = ""
	for _, r := range "zig" {
		modal.HandleKey(input.Key{Type: input.KeyRune, Rune: r})
	}
	if modal.InputBuffer != "zig" {
		t.Fatalf("expected InputBuffer 'zig', got %q", modal.InputBuffer)
	}
	modal.commitCurrentField()
	if modal.EditProfile.Type != "zig" {
		t.Fatalf("expected EditProfile.Type 'zig', got %s", modal.EditProfile.Type)
	}

	// 3. Test mouse click on "+ New from template" in list mode
	modal.Mode = LaunchModeList
	modal.SelectedIdx = 0
	screenW, screenH := 80, 24
	modalW := screenW - 12
	modalH := screenH - 6
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2
	contentTop := startY + 3
	maxItems := modalH - 7
	tplBtnY := contentTop + 1 + min(maxItems-1, max(1, cfgCount))

	handled, _ := modal.HandleClick(startX+5, tplBtnY, screenW, screenH)
	if !handled || modal.Mode != LaunchModeAdd {
		t.Fatalf("expected HandleClick on + New to switch to LaunchModeAdd, handled=%v, mode=%v", handled, modal.Mode)
	}

	// 4. Test mouse click on Cancel button in edit mode
	bottomY := startY + modalH - 2
	saveBtnLen := 27
	cancelX := startX + 3 + saveBtnLen + 5
	handled, _ = modal.HandleClick(cancelX, bottomY, screenW, screenH)
	if !handled || modal.Mode != LaunchModeList {
		t.Fatalf("expected Cancel click to return to LaunchModeList, handled=%v, mode=%v", handled, modal.Mode)
	}
}

