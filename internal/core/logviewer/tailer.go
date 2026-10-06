package logviewer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// TailerConfig defines configuration settings for the asynchronous file tailer.
type TailerConfig struct {
	FilePath      string        `json:"file_path"`
	PollInterval  time.Duration `json:"poll_interval"`  // e.g. 50ms
	FromBeginning bool          `json:"from_beginning"` // Read from start or seek to end
	BufferSize    int           `json:"buffer_size"`    // Chunk read buffer size, default 32KB
}

// DefaultTailerConfig returns recommended tailer settings.
func DefaultTailerConfig(filePath string) TailerConfig {
	return TailerConfig{
		FilePath:      filePath,
		PollInterval:  50 * time.Millisecond,
		FromBeginning: true,
		BufferSize:    32 * 1024,
	}
}

// Tailer is an asynchronous streaming log file reader that continuously polls
// and ingests lines into a RingBuffer and triggers subscriber callbacks.
type Tailer struct {
	config  TailerConfig
	buffer  *RingBuffer
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
	running bool
	paused  bool
	offset  int64

	OnLine  func(line LogLine)
	OnError func(err error)
}

// NewTailer creates a new tailer instance targeting the given file and ring buffer.
func NewTailer(filePath string, buffer *RingBuffer) *Tailer {
	return NewTailerWithConfig(DefaultTailerConfig(filePath), buffer)
}

// NewTailerWithConfig creates a new tailer instance with custom configuration.
func NewTailerWithConfig(cfg TailerConfig, buffer *RingBuffer) *Tailer {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 50 * time.Millisecond
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 32 * 1024
	}

	return &Tailer{
		config: cfg,
		buffer: buffer,
	}
}

// Start begins asynchronous streaming in a background goroutine.
func (t *Tailer) Start() error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return errors.New("tailer already running")
	}

	t.ctx, t.cancel = context.WithCancel(context.Background())
	t.running = true
	t.paused = false
	t.offset = 0
	t.mu.Unlock()

	t.wg.Add(1)
	go t.tailLoop()

	return nil
}

// Stop cleanly terminates the asynchronous streaming tailer.
func (t *Tailer) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	cancel := t.cancel
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	t.wg.Wait()
	return nil
}

// Pause temporarily suspends ingesting new lines.
func (t *Tailer) Pause() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.paused = true
}

// Resume restarts ingesting new lines.
func (t *Tailer) Resume() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.paused = false
}

// IsRunning reports whether the tailer goroutine is active.
func (t *Tailer) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

// IsPaused reports whether line processing is paused.
func (t *Tailer) IsPaused() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.paused
}

// Offset returns current file byte read position.
func (t *Tailer) Offset() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.offset
}

// tailLoop handles the continuous polling and reading of the log file.
func (t *Tailer) tailLoop() {
	defer t.wg.Done()

	var file *os.File
	var pending bytes.Buffer
	readBuf := make([]byte, t.config.BufferSize)
	initialized := false

	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()

	for {
		select {
		case <-t.ctx.Done():
			// Flush any remaining line in pending buffer
			if pending.Len() > 0 && !t.IsPaused() {
				lineStr := pending.String()
				t.emitLine(lineStr)
				pending.Reset()
			}
			return
		default:
		}

		// Ensure file is open
		if file == nil {
			f, err := os.Open(t.config.FilePath)
			if err != nil {
				// File may not exist yet, wait and retry
				select {
				case <-t.ctx.Done():
					return
				case <-time.After(t.config.PollInterval):
					continue
				}
			}
			file = f

			// Initial seek if configured
			if !initialized {
				initialized = true
				if !t.config.FromBeginning {
					fi, err := file.Stat()
					if err == nil {
						t.offset = fi.Size()
						_, _ = file.Seek(t.offset, io.SeekStart)
					}
				} else {
					t.offset = 0
				}
			} else {
				// Re-seek to last recorded offset
				_, _ = file.Seek(t.offset, io.SeekStart)
			}
		}

		// Check for file truncation or log rotation
		fi, err := file.Stat()
		if err != nil {
			_ = file.Close()
			file = nil
			continue
		}

		if fi.Size() < t.offset {
			// File was truncated
			t.offset = 0
			_, _ = file.Seek(0, io.SeekStart)
			pending.Reset()
		}

		// Read available bytes
		n, readErr := file.Read(readBuf)
		if n > 0 {
			t.offset += int64(n)
			chunk := readBuf[:n]

			for len(chunk) > 0 {
				newlineIdx := bytes.IndexByte(chunk, '\n')
				if newlineIdx == -1 {
					// No newline in rest of chunk, append to pending
					pending.Write(chunk)
					break
				}

				// Complete line found
				pending.Write(chunk[:newlineIdx])
				lineStr := pending.String()
				pending.Reset()
				chunk = chunk[newlineIdx+1:]

				if !t.IsPaused() {
					t.emitLine(lineStr)
				}
			}
		}

		if readErr != nil && !errors.Is(readErr, io.EOF) {
			if t.OnError != nil {
				t.OnError(readErr)
			}
		}

		// If no bytes were read or EOF reached, wait for poll interval
		if n == 0 {
			select {
			case <-t.ctx.Done():
				return
			case <-time.After(t.config.PollInterval):
			}
		}
	}
}

// emitLine parses the raw string and dispatches it to the buffer and listener.
func (t *Tailer) emitLine(raw string) {
	parsed := ParseLine(raw, 0)
	if t.buffer != nil {
		parsed = t.buffer.AppendEntry(parsed)
	}
	if t.OnLine != nil {
		t.OnLine(parsed)
	}
}
