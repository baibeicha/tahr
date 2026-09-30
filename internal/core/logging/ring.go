package logging

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const defaultRingCapacity = 200

// LogRing is a thread-safe circular buffer for storing formatted recent log entries.
type LogRing struct {
	mu       sync.RWMutex
	entries  []string
	capacity int
}

var globalRing = NewLogRing(defaultRingCapacity)

// NewLogRing creates a new LogRing with a given capacity.
func NewLogRing(capacity int) *LogRing {
	if capacity <= 0 {
		capacity = defaultRingCapacity
	}
	return &LogRing{
		entries:  make([]string, 0, capacity),
		capacity: capacity,
	}
}

// Append adds a formatted log entry into the ring buffer.
func (r *LogRing) Append(level, msg string, args ...any) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	ts := time.Now().Format("15:04:05.000")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s [%s] %s", ts, level, msg))
	if len(args) > 0 {
		sb.WriteString(" (")
		for i, a := range args {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%v", a))
		}
		sb.WriteString(")")
	}

	entry := sb.String()
	if len(r.entries) >= r.capacity {
		r.entries = r.entries[1:]
	}
	r.entries = append(r.entries, entry)
}

// GetRecent returns up to n most recent log entries.
func (r *LogRing) GetRecent(n int) []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := len(r.entries)
	if n <= 0 || n > total {
		n = total
	}
	start := total - n
	res := make([]string, n)
	copy(res, r.entries[start:])
	return res
}

// GetRawRecent returns recent log entries joined by newlines.
func (r *LogRing) GetRawRecent(n int) string {
	lines := r.GetRecent(n)
	return strings.Join(lines, "\n")
}

// Clear flushes all entries in the ring buffer.
func (r *LogRing) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = r.entries[:0]
}

// GetRecentLogs retrieves up to n recent log entries from the global ring.
func GetRecentLogs(n int) []string {
	return globalRing.GetRecent(n)
}

// GetRecentLogsRaw retrieves up to n recent log entries as a single string from the global ring.
func GetRecentLogsRaw(n int) string {
	return globalRing.GetRawRecent(n)
}
