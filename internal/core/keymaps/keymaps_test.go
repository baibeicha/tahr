package keymaps

import (
	"os"
	"testing"
)

func TestKeymapsProfiles(t *testing.T) {
	profiles := Profiles()
	if len(profiles) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(profiles))
	}

	vscode := GetProfileBindings(ProfileVSCode)
	jetbrains := GetProfileBindings(ProfileJetBrains)
	emacs := GetProfileBindings(ProfileEmacs)

	if vscode["save"] != "Ctrl+S" {
		t.Errorf("expected vscode save=Ctrl+S, got %s", vscode["save"])
	}
	if jetbrains["commands"] != "Ctrl+Shift+A" {
		t.Errorf("expected jetbrains commands=Ctrl+Shift+A, got %s", jetbrains["commands"])
	}
	if emacs["find"] != "Ctrl+S" {
		t.Errorf("expected emacs find=Ctrl+S, got %s", emacs["find"])
	}
}

func TestMergeBindings(t *testing.T) {
	base := map[string]string{
		"save": "Ctrl+S",
		"find": "Ctrl+F",
	}
	overrides := map[string]string{
		"save": "Ctrl+Alt+S",
		"quit": "Ctrl+Q",
	}
	merged := MergeBindings(base, overrides)
	if merged["save"] != "Ctrl+Alt+S" {
		t.Errorf("expected save=Ctrl+Alt+S, got %s", merged["save"])
	}
	if merged["find"] != "Ctrl+F" {
		t.Errorf("expected find=Ctrl+F, got %s", merged["find"])
	}
	if merged["quit"] != "Ctrl+Q" {
		t.Errorf("expected quit=Ctrl+Q, got %s", merged["quit"])
	}
}

func TestSaveAndLoadCustomKeymaps(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-keymaps-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	bindings := map[string]string{
		"save": "Ctrl+S",
		"tree": "F2",
	}

	if err := SaveCustomKeymaps(tmpDir, bindings); err != nil {
		t.Fatalf("SaveCustomKeymaps failed: %v", err)
	}

	loaded, err := LoadCustomKeymaps(tmpDir)
	if err != nil {
		t.Fatalf("LoadCustomKeymaps failed: %v", err)
	}

	if loaded["save"] != "Ctrl+S" || loaded["tree"] != "F2" {
		t.Fatalf("unexpected loaded keymaps: %+v", loaded)
	}
}
