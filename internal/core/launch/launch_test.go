package launch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLaunch_Profiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-launch-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a dummy go.mod to trigger Go detection
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testapp\n"), 0644)

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	names := cfg.ProfileNames()
	if len(names) < 2 {
		t.Fatalf("expected at least 2 profiles for Go project, got: %v", names)
	}

	active := cfg.GetActive()
	if active == nil || active.Type != "go" {
		t.Fatalf("expected Go active profile, got %+v", active)
	}

	// Switch profile
	cfg.SetActive(names[1])
	if cfg.ActiveProfile != names[1] {
		t.Fatalf("expected active profile to switch to %s, got %s", names[1], cfg.ActiveProfile)
	}

	// Verify persistence in .tahr/launch.json
	launchFile := filepath.Join(tmpDir, ".tahr", "launch.json")
	if _, err := os.Stat(launchFile); os.IsNotExist(err) {
		t.Fatalf("expected %s to be created", launchFile)
	}
}

func TestLaunch_CRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-launch-crud-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	initCount := len(cfg.Configurations)
	newP := Profile{
		Name:    "Custom Run",
		Type:    "shell",
		Request: "launch",
		Target:  "build.sh",
	}
	cfg.AddProfile(newP)

	if len(cfg.Configurations) != initCount+1 {
		t.Fatalf("expected %d profiles after add, got %d", initCount+1, len(cfg.Configurations))
	}

	// Update profile
	idx := len(cfg.Configurations) - 1
	updatedP := newP
	updatedP.Name = "Updated Custom Run"
	cfg.UpdateProfile(idx, updatedP)

	if cfg.Configurations[idx].Name != "Updated Custom Run" {
		t.Fatalf("expected profile name 'Updated Custom Run', got %s", cfg.Configurations[idx].Name)
	}

	// Reload from disk to verify persistence
	reloaded, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if len(reloaded.Configurations) != initCount+1 {
		t.Fatalf("expected %d persisted profiles, got %d", initCount+1, len(reloaded.Configurations))
	}

	// Delete profile
	cfg.DeleteProfile(idx)
	if len(cfg.Configurations) != initCount {
		t.Fatalf("expected %d profiles after delete, got %d", initCount, len(cfg.Configurations))
	}
}
