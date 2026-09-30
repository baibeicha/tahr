package clipboard

import "sync"

var (
	memMu   sync.RWMutex
	memText string
)

// Read reads text from the operating system clipboard, with fallback to in-memory buffer.
func Read() (string, error) {
	text, err := readOS()
	if err == nil && text != "" {
		return text, nil
	}
	memMu.RLock()
	defer memMu.RUnlock()
	return memText, nil
}

// Write writes text to both the operating system clipboard and in-memory buffer.
func Write(text string) error {
	memMu.Lock()
	memText = text
	memMu.Unlock()

	return writeOS(text)
}
