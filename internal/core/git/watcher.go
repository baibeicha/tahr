package git

import (
	"flag"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"tahr/internal/core/logging"
)

// BackgroundGitWatcher runs background git polling and maintains a lock-free/instantaneous
// thread-safe cache for UI rendering with 0ms latency in View().
type BackgroundGitWatcher struct {
	mu           sync.RWMutex
	tracker      *Tracker
	workspaceDir string
	activeFile   string

	// Cached state
	cachedBranch   string
	cachedStatuses map[string]string
	cachedDiffs    map[string]*FileGitDiff

	// Control channels
	triggerChan chan struct{}
	stopChan    chan struct{}
	doneChan    chan struct{}
	onUpdate    func()
	subscribers []chan struct{}
}

// NewBackgroundGitWatcher creates a new background git watcher for the workspace.
func NewBackgroundGitWatcher(workspaceDir string, onUpdate func()) *BackgroundGitWatcher {
	w := &BackgroundGitWatcher{
		tracker:        NewTracker(),
		workspaceDir:   workspaceDir,
		cachedStatuses: make(map[string]string),
		cachedDiffs:    make(map[string]*FileGitDiff),
		triggerChan:    make(chan struct{}, 10),
		stopChan:       make(chan struct{}),
		doneChan:       make(chan struct{}),
		onUpdate:       onUpdate,
	}

	runtime.SetFinalizer(w, func(bw *BackgroundGitWatcher) {
		bw.Stop()
	})

	go w.workerLoop()
	w.Trigger()
	return w
}

// Subscribe registers a channel to receive notifications on git state updates.
func (w *BackgroundGitWatcher) Subscribe() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch := make(chan struct{}, 1)
	w.subscribers = append(w.subscribers, ch)
	return ch
}

// Unsubscribe removes a registered notification channel.
func (w *BackgroundGitWatcher) Unsubscribe(target <-chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, ch := range w.subscribers {
		if ch == target {
			w.subscribers = append(w.subscribers[:i], w.subscribers[i+1:]...)
			return
		}
	}
}

// SetCachedForTest directly populates the cache for testing.
func (w *BackgroundGitWatcher) SetCachedForTest(branch string, statuses map[string]string, diffs map[string]*FileGitDiff) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cachedBranch = branch
	if statuses != nil {
		w.cachedStatuses = statuses
	}
	if diffs != nil {
		w.cachedDiffs = diffs
	}
}

// SetWorkspace updates the watched workspace directory.
func (w *BackgroundGitWatcher) SetWorkspace(dir string) {
	w.mu.Lock()
	if w.workspaceDir != dir {
		w.workspaceDir = dir
		w.cachedBranch = ""
		w.cachedStatuses = make(map[string]string)
		w.cachedDiffs = make(map[string]*FileGitDiff)
	}
	w.mu.Unlock()
	w.Trigger()
}

// SetActiveFile informs the watcher of the active file to prioritize diff queries.
func (w *BackgroundGitWatcher) SetActiveFile(filePath string) {
	w.mu.Lock()
	clean := filepath.Clean(filePath)
	if w.activeFile != clean {
		w.activeFile = clean
		w.mu.Unlock()
		w.Trigger()
		return
	}
	w.mu.Unlock()
}

// Trigger queues a background refresh.
func (w *BackgroundGitWatcher) Trigger() {
	select {
	case w.triggerChan <- struct{}{}:
	default:
	}
}

// Stop terminates the background watcher goroutine.
func (w *BackgroundGitWatcher) Stop() {
	select {
	case <-w.stopChan:
		return
	default:
		close(w.stopChan)
		<-w.doneChan
	}
}

func (w *BackgroundGitWatcher) workerLoop() {
	defer close(w.doneChan)
	var tickerChan <-chan time.Time
	if flag.Lookup("test.v") == nil {
		ticker := time.NewTicker(3000 * time.Millisecond)
		defer ticker.Stop()
		tickerChan = ticker.C
	}

	for {
		select {
		case <-w.stopChan:
			return
		case <-tickerChan:
			w.refresh()
		case <-w.triggerChan:
			w.refresh()
		}
	}
}

func (w *BackgroundGitWatcher) refresh() {
	w.mu.RLock()
	wsDir := w.workspaceDir
	actFile := w.activeFile
	w.mu.RUnlock()

	if wsDir == "" {
		return
	}

	branch := w.tracker.GetBranch(wsDir)
	statuses := w.tracker.GetFileStatuses(wsDir)

	var activeDiff *FileGitDiff
	if actFile != "" {
		activeDiff = w.tracker.GetFileDiff(actFile)
	}

	w.mu.Lock()
	changed := false
	if w.cachedBranch != branch {
		w.cachedBranch = branch
		changed = true
	}
	if len(statuses) != len(w.cachedStatuses) {
		changed = true
	} else {
		for k, v := range statuses {
			if w.cachedStatuses[k] != v {
				changed = true
				break
			}
		}
	}
	w.cachedStatuses = statuses
	if actFile != "" {
		if activeDiff != nil {
			w.cachedDiffs[actFile] = activeDiff
			changed = true
		} else {
			if _, exists := w.cachedDiffs[actFile]; exists {
				delete(w.cachedDiffs, actFile)
				changed = true
			}
		}
	}
	onUp := w.onUpdate
	subs := append([]chan struct{}{}, w.subscribers...)
	w.mu.Unlock()

	logging.Debug("GitWatcher refreshed", "branch", branch, "statusesCount", len(statuses))

	if changed {
		if onUp != nil {
			onUp()
		}
		for _, ch := range subs {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

// GetBranch returns the cached branch name with 0 ms overhead.
func (w *BackgroundGitWatcher) GetBranch() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cachedBranch
}

// GetFileStatuses returns the cached file porcelain status map with 0 ms overhead.
func (w *BackgroundGitWatcher) GetFileStatuses() map[string]string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cachedStatuses
}

// GetFileDiff returns cached diff markers for filePath with 0 ms overhead.
func (w *BackgroundGitWatcher) GetFileDiff(filePath string) *FileGitDiff {
	if filePath == "" {
		return nil
	}
	clean := filepath.Clean(filePath)
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cachedDiffs[clean]
}
