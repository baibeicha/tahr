package sdk

import (
	"testing"
)

func TestSDKManagerDetect(t *testing.T) {
	mgr := NewManager("")
	info := mgr.GoSDK()
	if info == nil {
		t.Log("No Go SDK detected automatically (check test machine)")
	} else {
		t.Logf("Detected Go SDK: Path=%s, Version=%s, BinDir=%s", info.BinaryPath, info.Version, info.BinDir)
		if info.Version == "" {
			t.Fatal("expected non-empty version string")
		}
	}
	verShort := mgr.GoVersionShort()
	t.Logf("GoVersionShort: %s", verShort)
	if verShort == "" {
		t.Fatal("expected non-empty version or 'Go: Not Found'")
	}
}
