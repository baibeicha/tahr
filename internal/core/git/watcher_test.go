package git

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundGitWatcher(t *testing.T) {
	var updateCalls int32
	watcher := NewBackgroundGitWatcher(".", func() {
		atomic.AddInt32(&updateCalls, 1)
	})
	defer watcher.Stop()

	// Initial reads must not block (0ms)
	start := time.Now()
	_ = watcher.GetBranch()
	_ = watcher.GetFileStatuses()
	_ = watcher.GetFileDiff("main.go")
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("cache read took too long: %v", elapsed)
	}

	watcher.SetActiveFile("go.mod")
	watcher.Trigger()

	// Wait briefly for background refresh
	time.Sleep(150 * time.Millisecond)

	branch := watcher.GetBranch()
	t.Logf("Detected branch: %s", branch)
}
