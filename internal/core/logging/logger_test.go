package logging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoggingInitAndWrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tahr-log-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	l, err := Init(tempDir)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if l == nil {
		t.Fatal("expected non-nil logger")
	}

	Info("test info message", "user", "alice")
	Debug("test debug message", "id", 123)
	Warn("test warn message", "code", 404)
	Error("test error message", "err", "sample")

	_ = Close()

	expectedPath := filepath.Join(tempDir, ".tahr", "tahr.log")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty log file")
	}
}
