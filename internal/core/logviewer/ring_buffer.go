package logviewer

import (
	"sync"
	"time"
)

// DefaultCapacity is the default capacity for the circular ring buffer.
const DefaultCapacity = 50000

// LogLine represents a single parsed or raw log entry in the ring buffer.
type LogLine struct {
	Index     int64          `json:"index"`
	Timestamp time.Time      `json:"timestamp,omitempty"`
	Level     LogLevel       `json:"level"`
	Raw       string         `json:"raw"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// RingBuffer is a thread-safe circular ring buffer supporting up to 50,000+
// log lines with sub-millisecond line append and indexed slicing.
type RingBuffer struct {
	mu       sync.RWMutex
	entries  []LogLine
	capacity int
	start    int   // Index of the oldest element in entries
	count    int   // Current number of stored elements
	seq      int64 // Monotonically increasing sequence counter
}

// NewRingBuffer allocates a new RingBuffer with the specified capacity.
// If capacity <= 0, DefaultCapacity (50,000) is used.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &RingBuffer{
		entries:  make([]LogLine, capacity),
		capacity: capacity,
		start:    0,
		count:    0,
		seq:      0,
	}
}

// Append parses and adds a raw log line into the circular ring buffer.
// The append executes in sub-millisecond time.
func (r *RingBuffer) Append(raw string) LogLine {
	entry := ParseLine(raw, 0)
	return r.AppendEntry(entry)
}

// AppendEntry adds a pre-parsed LogLine into the circular ring buffer.
func (r *RingBuffer) AppendEntry(entry LogLine) LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.seq++
	entry.Index = r.seq

	if r.count < r.capacity {
		idx := (r.start + r.count) % r.capacity
		r.entries[idx] = entry
		r.count++
	} else {
		// Buffer is full: overwrite oldest element and advance start pointer
		r.entries[r.start] = entry
		r.start = (r.start + 1) % r.capacity
	}

	return entry
}

// AppendBatch efficiently appends a batch of raw log lines under a single lock.
func (r *RingBuffer) AppendBatch(lines []string) []LogLine {
	if len(lines) == 0 {
		return nil
	}

	parsed := make([]LogLine, len(lines))
	for i, l := range lines {
		parsed[i] = ParseLine(l, 0)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range parsed {
		r.seq++
		parsed[i].Index = r.seq

		if r.count < r.capacity {
			idx := (r.start + r.count) % r.capacity
			r.entries[idx] = parsed[i]
			r.count++
		} else {
			r.entries[r.start] = parsed[i]
			r.start = (r.start + 1) % r.capacity
		}
	}

	return parsed
}

// Len returns the current number of log lines in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

// Cap returns the maximum capacity of the buffer.
func (r *RingBuffer) Cap() int {
	return r.capacity
}

// TotalAppended returns the total number of lines appended over the buffer's lifetime.
func (r *RingBuffer) TotalAppended() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.seq
}

// EvictedCount returns the number of lines dropped due to wrap-around.
func (r *RingBuffer) EvictedCount() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.seq - int64(r.count)
}

// Get returns the logical log line at 0-indexed position idx,
// where 0 is the oldest retained line and Len()-1 is the newest.
func (r *RingBuffer) Get(idx int) (LogLine, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if idx < 0 || idx >= r.count {
		return LogLine{}, false
	}

	actualIdx := (r.start + idx) % r.capacity
	return r.entries[actualIdx], true
}

// Slice returns a logical subslice of log lines in chronological order [from, to).
// The indices are clamped to valid ranges [0, Len()].
func (r *RingBuffer) Slice(from, to int) []LogLine {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if from < 0 {
		from = 0
	}
	if to > r.count {
		to = r.count
	}
	if from >= to {
		return []LogLine{}
	}

	n := to - from
	result := make([]LogLine, n)
	for i := 0; i < n; i++ {
		actualIdx := (r.start + from + i) % r.capacity
		result[i] = r.entries[actualIdx]
	}

	return result
}

// All returns a slice containing all current log lines in chronological order.
func (r *RingBuffer) All() []LogLine {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]LogLine, r.count)
	for i := 0; i < r.count; i++ {
		actualIdx := (r.start + i) % r.capacity
		result[i] = r.entries[actualIdx]
	}
	return result
}

// Tail returns up to n most recent log entries.
func (r *RingBuffer) Tail(n int) []LogLine {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if n <= 0 || r.count == 0 {
		return []LogLine{}
	}
	if n > r.count {
		n = r.count
	}

	startLogical := r.count - n
	result := make([]LogLine, n)
	for i := 0; i < n; i++ {
		actualIdx := (r.start + startLogical + i) % r.capacity
		result[i] = r.entries[actualIdx]
	}
	return result
}

// Head returns up to n oldest retained log entries.
func (r *RingBuffer) Head(n int) []LogLine {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if n <= 0 || r.count == 0 {
		return []LogLine{}
	}
	if n > r.count {
		n = r.count
	}

	result := make([]LogLine, n)
	for i := 0; i < n; i++ {
		actualIdx := (r.start + i) % r.capacity
		result[i] = r.entries[actualIdx]
	}
	return result
}

// Clear flushes all entries from the buffer and resets pointers.
func (r *RingBuffer) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.start = 0
	r.count = 0
}
