package tui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/baibeicha/goatui/pkg/driver/input"
)

// TerminalSplitMode defines the split presentation in the terminal drawer.
type TerminalSplitMode int

const (
	TermSplitTabs TerminalSplitMode = iota // Single active tab
	TermSplitHorizontal                    // 2 panes side-by-side
	TermSplitVertical                      // 2 panes stacked vertically
)

// TerminalInstance represents an independent virtual terminal and PTY shell.
type TerminalInstance struct {
	ID               int
	Name             string
	VTerm            *VTerm
	Session          PTYSession
	IsRunning        bool
	Lines            []string
	lastSpawnAttempt time.Time
	spawnFailures    int
}

// TerminalDrawer manages an interactive virtual terminal emulator and shell session.
type TerminalDrawer struct {
	mu          sync.Mutex
	Open        bool
	Height      int
	Cwd         string
	Lines       []string // Retained for compatibility and logging
	ScrollY     int
	InputText   string
	CursorPos   int
	History     []string
	HistoryIdx  int
	IsRunning   bool
	CurrentTask string

	// Virtual Terminal Emulator and PTY (delegates to active instance)
	VTerm   *VTerm
	Session PTYSession

	// Multi-terminal tabs & splits
	Instances  []*TerminalInstance
	ActiveIdx  int
	SplitMode  TerminalSplitMode
	nextInstID int

	// Dimension dampener to prevent 120 FPS resize storm
	lastCols int
	lastRows int

	// Tab inline renaming
	RenamingIdx   int
	RenamingInput string
	IsRenaming    bool

	// Callback when new bytes are written to VTerm from shell
	OnData func()
}

// NewTerminalDrawer initializes an integrated terminal drawer with a virtual terminal emulator.
func NewTerminalDrawer(cwd string) *TerminalDrawer {
	if cwd == "" {
		cwd = "."
	}
	vt := NewVTerm(80, 10)
	welcome := "\x1b[1;36mTahr Integrated Terminal Emulator\x1b[0m\r\nType shell commands or interactive CLI tools below.\r\n\r\n"
	_, _ = vt.Write([]byte(welcome))

	firstInst := &TerminalInstance{
		ID:        1,
		Name:      "Shell 1",
		VTerm:     vt,
		Lines:     []string{"Tahr Virtual Terminal Emulator v1.0.0", "Interactive PTY Session ready."},
		IsRunning: false,
	}

	td := &TerminalDrawer{
		Open:       false,
		Height:     10,
		Cwd:        cwd,
		Lines:      firstInst.Lines,
		History:    make([]string, 0),
		HistoryIdx: -1,
		VTerm:      vt,
		Instances:  []*TerminalInstance{firstInst},
		ActiveIdx:  0,
		SplitMode:  TermSplitTabs,
		nextInstID: 2,
	}

	return td
}

// ActiveInstance returns the currently focused terminal instance.
func (td *TerminalDrawer) ActiveInstance() *TerminalInstance {
	td.mu.Lock()
	defer td.mu.Unlock()
	if len(td.Instances) == 0 {
		return nil
	}
	if td.ActiveIdx < 0 || td.ActiveIdx >= len(td.Instances) {
		td.ActiveIdx = 0
	}
	return td.Instances[td.ActiveIdx]
}

// AddInstance spawns a new concurrent terminal shell instance.
func (td *TerminalDrawer) AddInstance(name string) *TerminalInstance {
	td.mu.Lock()
	id := td.nextInstID
	td.nextInstID++
	if name == "" {
		name = fmt.Sprintf("Shell %d", id)
	}
	vt := NewVTerm(80, 10)
	welcome := fmt.Sprintf("\x1b[1;36mTahr Terminal [%s]\x1b[0m\r\n\r\n", name)
	_, _ = vt.Write([]byte(welcome))

	inst := &TerminalInstance{
		ID:        id,
		Name:      name,
		VTerm:     vt,
		Lines:     []string{fmt.Sprintf("Terminal instance [%s] ready.", name)},
		IsRunning: false,
	}
	td.Instances = append(td.Instances, inst)
	td.ActiveIdx = len(td.Instances) - 1
	td.VTerm = inst.VTerm
	td.Session = nil
	td.IsRunning = false
	td.mu.Unlock()

	// Launch shell for new instance
	_ = td.EnsureSession()
	return inst
}

// CloseInstance terminates and removes the instance at idx.
func (td *TerminalDrawer) CloseInstance(idx int) {
	td.mu.Lock()
	if idx < 0 || idx >= len(td.Instances) {
		td.mu.Unlock()
		return
	}
	closing := td.Instances[idx]
	if closing.Session != nil {
		_ = closing.Session.Close()
	}

	td.Instances = append(td.Instances[:idx], td.Instances[idx+1:]...)
	if len(td.Instances) == 0 {
		// If last instance was closed, create a clean one
		vt := NewVTerm(80, 10)
		firstInst := &TerminalInstance{
			ID:    td.nextInstID,
			Name:  "Shell 1",
			VTerm: vt,
			Lines: []string{"New shell ready."},
		}
		td.nextInstID++
		td.Instances = []*TerminalInstance{firstInst}
		td.ActiveIdx = 0
	} else if td.ActiveIdx >= len(td.Instances) {
		td.ActiveIdx = len(td.Instances) - 1
	}

	active := td.Instances[td.ActiveIdx]
	td.VTerm = active.VTerm
	td.Session = active.Session
	td.IsRunning = active.IsRunning
	td.Lines = active.Lines
	td.mu.Unlock()
}

// SwitchInstance activates the terminal instance at idx.
func (td *TerminalDrawer) SwitchInstance(idx int) {
	td.mu.Lock()
	defer td.mu.Unlock()
	if idx < 0 || idx >= len(td.Instances) {
		return
	}
	td.ActiveIdx = idx
	active := td.Instances[idx]
	td.VTerm = active.VTerm
	td.Session = active.Session
	td.IsRunning = active.IsRunning
	td.Lines = active.Lines
}

// StartRename begins inline editing of tab name.
func (td *TerminalDrawer) StartRename(idx int) {
	td.mu.Lock()
	defer td.mu.Unlock()
	if idx >= 0 && idx < len(td.Instances) {
		td.RenamingIdx = idx
		td.RenamingInput = td.Instances[idx].Name
		td.IsRenaming = true
	}
}

// CommitRename applies the new tab name.
func (td *TerminalDrawer) CommitRename() {
	td.mu.Lock()
	defer td.mu.Unlock()
	if td.IsRenaming && td.RenamingIdx >= 0 && td.RenamingIdx < len(td.Instances) {
		trimmed := strings.TrimSpace(td.RenamingInput)
		if trimmed != "" {
			td.Instances[td.RenamingIdx].Name = trimmed
		}
	}
	td.IsRenaming = false
}

// CancelRename cancels renaming without modifying tab name.
func (td *TerminalDrawer) CancelRename() {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.IsRenaming = false
}

// ToggleSplit switches between tabs, horizontal side-by-side, and vertical stacked splits.
func (td *TerminalDrawer) ToggleSplit() {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.lastCols = 0
	td.lastRows = 0
	switch td.SplitMode {
	case TermSplitTabs:
		td.SplitMode = TermSplitHorizontal
	case TermSplitHorizontal:
		td.SplitMode = TermSplitVertical
	default:
		td.SplitMode = TermSplitTabs
	}
}

// SetSplitMode explicitly sets the split geometry.
func (td *TerminalDrawer) SetSplitMode(mode TerminalSplitMode) {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.lastCols = 0
	td.lastRows = 0
	td.SplitMode = mode
}

// EnsureSession launches the persistent PTY shell session if not already running.
func (td *TerminalDrawer) EnsureSession() error {
	td.mu.Lock()
	if len(td.Instances) == 0 {
		td.mu.Unlock()
		return nil
	}
	if td.ActiveIdx < 0 || td.ActiveIdx >= len(td.Instances) {
		td.ActiveIdx = 0
	}
	inst := td.Instances[td.ActiveIdx]
	if inst.Session != nil && inst.IsRunning {
		td.Session = inst.Session
		td.IsRunning = inst.IsRunning
		td.VTerm = inst.VTerm
		td.mu.Unlock()
		return nil
	}

	now := time.Now()
	// Spawn backoff defense: prevent infinite restart storms
	if inst.spawnFailures >= 10 {
		td.mu.Unlock()
		return fmt.Errorf("terminal spawn halted: maximum retry attempts exceeded (%d consecutive failures); kill or restart terminal", inst.spawnFailures)
	} else if inst.spawnFailures >= 3 {
		backoff := time.Duration(inst.spawnFailures) * time.Second
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
		if now.Sub(inst.lastSpawnAttempt) < backoff {
			td.mu.Unlock()
			return fmt.Errorf("terminal spawn throttled: too many consecutive failures (%d)", inst.spawnFailures)
		}
	} else if now.Sub(inst.lastSpawnAttempt) < 500*time.Millisecond {
		td.mu.Unlock()
		return fmt.Errorf("terminal spawn throttled: please wait")
	}
	inst.lastSpawnAttempt = now

	cols := inst.VTerm.Cols
	rows := inst.VTerm.Rows
	cwd := td.Cwd
	td.mu.Unlock()

	session, err := StartShell(cwd, cols, rows)
	if err != nil {
		td.mu.Lock()
		inst.spawnFailures++
		inst.Lines = append(inst.Lines, fmt.Sprintf("[Error starting shell: %v]", err))
		td.Lines = inst.Lines
		msg := fmt.Sprintf("\r\n\x1b[1;31m[Error starting shell: %v]\x1b[0m\r\n", err)
		_, _ = inst.VTerm.Write([]byte(msg))
		td.mu.Unlock()
		return err
	}

	td.mu.Lock()
	inst.Session = session
	inst.IsRunning = true
	td.Session = session
	td.IsRunning = true
	td.VTerm = inst.VTerm
	td.mu.Unlock()

	startTime := time.Now()
	go func() {
		td.readLoop(session, inst)
		td.mu.Lock()
		if time.Since(startTime) < 1*time.Second {
			inst.spawnFailures++
		} else {
			inst.spawnFailures = 0
		}
		td.mu.Unlock()
	}()

	return nil
}

func (td *TerminalDrawer) readLoop(session PTYSession, inst *TerminalInstance) {
	buf := make([]byte, 4096)
	for {
		n, err := session.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, _ = inst.VTerm.Write(chunk)

			// Also update Lines buffer for text search & inspection
			td.mu.Lock()
			rawText := string(chunk)
			splitLines := strings.Split(rawText, "\n")
			for _, sl := range splitLines {
				trimmed := strings.TrimRight(sl, "\r")
				if trimmed != "" {
					inst.Lines = append(inst.Lines, trimmed)
					if len(inst.Lines) > 1000 {
						inst.Lines = inst.Lines[len(inst.Lines)-1000:]
					}
				}
			}
			if len(td.Instances) > td.ActiveIdx && td.Instances[td.ActiveIdx] == inst {
				td.Lines = inst.Lines
			}
			onData := td.OnData
			td.mu.Unlock()

			if onData != nil {
				onData()
			}
		}

		if err != nil {
			if err != io.EOF {
				td.mu.Lock()
				inst.Lines = append(inst.Lines, fmt.Sprintf("[Shell read error: %v]", err))
				td.mu.Unlock()
			}
			break
		}
	}

	td.mu.Lock()
	if inst.Session == session {
		inst.IsRunning = false
		inst.Session = nil
	}
	if td.Session == session {
		td.IsRunning = false
		td.Session = nil
	}
	td.mu.Unlock()
}

// Toggle flips the terminal open state and ensures an active PTY session.
func (td *TerminalDrawer) Toggle() {
	td.mu.Lock()
	td.Open = !td.Open
	isOpen := td.Open
	td.mu.Unlock()

	if isOpen {
		_ = td.EnsureSession()
	}
}

// Resize updates the terminal emulator grid and notifies the PTY pseudo-console.
func (td *TerminalDrawer) Resize(cols, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	td.mu.Lock()
	defer td.mu.Unlock()

	if td.lastCols == cols && td.lastRows == rows {
		return
	}
	td.lastCols = cols
	td.lastRows = rows

	if td.SplitMode == TermSplitHorizontal && len(td.Instances) >= 2 {
		leftCols := (cols - 1) / 2
		rightCols := cols - leftCols - 1
		if leftCols < 1 {
			leftCols = 1
		}
		if rightCols < 1 {
			rightCols = 1
		}
		td.Instances[0].VTerm.Resize(leftCols, rows)
		if td.Instances[0].Session != nil {
			_ = td.Instances[0].Session.Resize(leftCols, rows)
		}
		td.Instances[1].VTerm.Resize(rightCols, rows)
		if td.Instances[1].Session != nil {
			_ = td.Instances[1].Session.Resize(rightCols, rows)
		}
		return
	}

	if td.SplitMode == TermSplitVertical && len(td.Instances) >= 2 {
		topRows := (rows - 1) / 2
		botRows := rows - topRows - 1
		if topRows < 1 {
			topRows = 1
		}
		if botRows < 1 {
			botRows = 1
		}
		td.Instances[0].VTerm.Resize(cols, topRows)
		if td.Instances[0].Session != nil {
			_ = td.Instances[0].Session.Resize(cols, topRows)
		}
		td.Instances[1].VTerm.Resize(cols, botRows)
		if td.Instances[1].Session != nil {
			_ = td.Instances[1].Session.Resize(cols, botRows)
		}
		return
	}

	for _, inst := range td.Instances {
		inst.VTerm.Resize(cols, rows)
		if inst.Session != nil {
			_ = inst.Session.Resize(cols, rows)
		}
	}
}

// Clear clears the virtual terminal screen and scrollback history.
func (td *TerminalDrawer) Clear() {
	td.mu.Lock()
	td.Lines = nil
	td.ScrollY = 0
	session := td.Session
	td.mu.Unlock()

	td.VTerm.Clear()

	if session != nil {
		// Send clear screen (Ctrl+L) to shell
		_, _ = session.Write([]byte{0x0c})
	}
}

// Kill terminates the running shell process.
func (td *TerminalDrawer) Kill() {
	td.mu.Lock()
	session := td.Session
	td.Session = nil
	td.IsRunning = false
	td.CurrentTask = ""
	for _, inst := range td.Instances {
		inst.spawnFailures = 0
	}
	td.mu.Unlock()

	if session != nil {
		_ = session.Close()
		msg := "\r\n\x1b[1;33m[Shell process terminated by user]\x1b[0m\r\n"
		_, _ = td.VTerm.Write([]byte(msg))
	}
}

// Execute runs a command string in the interactive shell session.
func (td *TerminalDrawer) Execute(cmdLine string) {
	trimmed := strings.TrimSpace(cmdLine)
	if trimmed == "" {
		return
	}

	td.mu.Lock()
	td.History = append(td.History, trimmed)
	td.HistoryIdx = len(td.History)
	td.InputText = ""
	td.CursorPos = 0
	td.CurrentTask = trimmed

	// Built-in cd handling
	if strings.HasPrefix(trimmed, "cd ") {
		newDir := strings.TrimSpace(strings.TrimPrefix(trimmed, "cd "))
		if newDir != "" {
			td.Cwd = newDir
		}
	}
	td.mu.Unlock()

	_ = td.EnsureSession()

	td.mu.Lock()
	session := td.Session
	td.mu.Unlock()

	if session != nil {
		payload := []byte(trimmed + "\r\n")
		_, _ = session.Write(payload)
	} else {
		// Fallback emulation without active PTY
		td.mu.Lock()
		td.Lines = append(td.Lines, fmt.Sprintf("$ %s", trimmed))
		td.mu.Unlock()
		_, _ = td.VTerm.Write([]byte(fmt.Sprintf("$ %s\r\n", trimmed)))
	}
}

// IsActivelyTyping returns true if the user has entered uncommitted text at the shell prompt.
func (td *TerminalDrawer) IsActivelyTyping() bool {
	if td == nil {
		return false
	}
	td.mu.Lock()
	defer td.mu.Unlock()
	return len(td.InputText) > 0
}

// HandleInputKey forwards a GoatUI keyboard event directly to the interactive shell.
func (td *TerminalDrawer) HandleInputKey(k input.Key) bool {
	td.mu.Lock()
	if td.IsRenaming {
		if k.Type == input.KeyEnter {
			td.mu.Unlock()
			td.CommitRename()
			return true
		} else if k.Type == input.KeyEsc {
			td.mu.Unlock()
			td.CancelRename()
			return true
		} else if k.Type == input.KeyBackspace {
			runes := []rune(td.RenamingInput)
			if len(runes) > 0 {
				td.RenamingInput = string(runes[:len(runes)-1])
			}
			td.mu.Unlock()
			return true
		} else if k.Rune >= 32 && !k.HasCtrl() && !k.HasAlt() {
			td.RenamingInput += string(k.Rune)
			td.mu.Unlock()
			return true
		}
		td.mu.Unlock()
		return true
	}
	td.mu.Unlock()

	_ = td.EnsureSession()

	// Scrolling history
	if k.Type == input.KeyPgUp || k.Type == input.KeyKpPageUp {
		if k.HasShift() {
			td.VTerm.Scroll(5)
			return true
		}
	}
	if k.Type == input.KeyPgDown || k.Type == input.KeyKpPageDown {
		if k.HasShift() {
			td.VTerm.Scroll(-5)
			return true
		}
	}

	// Check if key is an undo/redo shortcut
	isUndoRedo := ((k.HasCtrl() || k.Rune == 26 || k.Rune == 25 || k.Rune == 21) &&
		(matchKey(k, 'z', 'я') || matchKey(k, 'y', 'н') || matchKey(k, 'u', 'г') || k.Rune == 26 || k.Rune == 25 || k.Rune == 21)) ||
		(k.HasAlt() && (k.Type == input.KeyBackspace || k.Rune == 8 || k.Rune == 127))

	if isUndoRedo {
		td.mu.Lock()
		hasInput := len(td.InputText) > 0
		td.mu.Unlock()
		if !hasInput {
			// Not actively typing in shell prompt: do NOT swallow, let editor handle undo/redo
			return false
		}
		// Actively typing in shell prompt: clear input line
		td.mu.Lock()
		td.InputText = ""
		td.CursorPos = 0
		td.mu.Unlock()
		return true
	}

	if k.Rune >= 32 && !k.HasCtrl() && !k.HasAlt() {
		td.mu.Lock()
		td.InputText += string(k.Rune)
		td.CursorPos = len([]rune(td.InputText))
		td.mu.Unlock()
	} else if k.Type == input.KeyBackspace {
		td.mu.Lock()
		if len(td.InputText) > 0 {
			runes := []rune(td.InputText)
			td.InputText = string(runes[:len(runes)-1])
			td.CursorPos = len([]rune(td.InputText))
		}
		td.mu.Unlock()
	} else if k.Type == input.KeyEnter {
		td.mu.Lock()
		td.InputText = ""
		td.CursorPos = 0
		td.mu.Unlock()
	}

	vtBytes := KeyToVT(k)
	if len(vtBytes) > 0 {
		td.mu.Lock()
		session := td.Session
		td.mu.Unlock()
		if session != nil {
			_, _ = session.Write(vtBytes)
		}
		return true
	}

	return false
}

// HandleKey provides backwards-compatible keystroke handling for tests and string inputs.
func (td *TerminalDrawer) HandleKey(keyStr string, r rune) bool {
	td.mu.Lock()
	switch keyStr {
	case "enter":
		cmd := td.InputText
		td.mu.Unlock()
		td.Execute(cmd)
		td.mu.Lock()
		if cmd == "clear" || cmd == "cls" {
			td.Lines = nil
			td.ScrollY = 0
			td.VTerm.Clear()
		}
		td.InputText = ""
		td.CursorPos = 0
		td.mu.Unlock()
		return true

	case "backspace":
		if td.CursorPos > 0 && len(td.InputText) > 0 {
			runes := []rune(td.InputText)
			if td.CursorPos <= len(runes) {
				runes = append(runes[:td.CursorPos-1], runes[td.CursorPos:]...)
				td.InputText = string(runes)
				td.CursorPos--
			}
		}
		session := td.Session
		td.mu.Unlock()
		if session != nil {
			_, _ = session.Write([]byte("\x08"))
		}
		return true

	case "left":
		if td.CursorPos > 0 {
			td.CursorPos--
		}
		session := td.Session
		td.mu.Unlock()
		if session != nil {
			_, _ = session.Write([]byte("\x1b[D"))
		}
		return true

	case "right":
		if td.CursorPos < len([]rune(td.InputText)) {
			td.CursorPos++
		}
		session := td.Session
		td.mu.Unlock()
		if session != nil {
			_, _ = session.Write([]byte("\x1b[C"))
		}
		return true

	case "home":
		td.CursorPos = 0
		session := td.Session
		td.mu.Unlock()
		if session != nil {
			_, _ = session.Write([]byte("\x1b[H"))
		}
		return true

	case "end":
		td.CursorPos = len([]rune(td.InputText))
		session := td.Session
		td.mu.Unlock()
		if session != nil {
			_, _ = session.Write([]byte("\x1b[F"))
		}
		return true

	case "up":
		if len(td.History) > 0 && td.HistoryIdx > 0 {
			td.HistoryIdx--
			td.InputText = td.History[td.HistoryIdx]
			td.CursorPos = len([]rune(td.InputText))
		}
		session := td.Session
		td.mu.Unlock()
		if session != nil {
			_, _ = session.Write([]byte("\x1b[A"))
		}
		return true

	case "down":
		if td.HistoryIdx < len(td.History)-1 {
			td.HistoryIdx++
			td.InputText = td.History[td.HistoryIdx]
			td.CursorPos = len([]rune(td.InputText))
		} else {
			td.HistoryIdx = len(td.History)
			td.InputText = ""
			td.CursorPos = 0
		}
		session := td.Session
		td.mu.Unlock()
		if session != nil {
			_, _ = session.Write([]byte("\x1b[B"))
		}
		return true

	default:
		if r >= 32 {
			runes := []rune(td.InputText)
			newRunes := make([]rune, 0, len(runes)+1)
			newRunes = append(newRunes, runes[:td.CursorPos]...)
			newRunes = append(newRunes, r)
			newRunes = append(newRunes, runes[td.CursorPos:]...)
			td.InputText = string(newRunes)
			td.CursorPos++

			session := td.Session
			td.mu.Unlock()
			if session != nil {
				_, _ = session.Write([]byte(string(r)))
			}
			return true
		}
	}
	td.mu.Unlock()

	return false
}

// WaitForOutput polls until the given substring appears in terminal output or timeout occurs.
func (td *TerminalDrawer) WaitForOutput(substr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		td.mu.Lock()
		for _, l := range td.Lines {
			if strings.Contains(l, substr) {
				td.mu.Unlock()
				return true
			}
		}
		td.mu.Unlock()

		lines := td.VTerm.ContentLines()
		for _, l := range lines {
			if strings.Contains(l, substr) {
				return true
			}
		}

		time.Sleep(20 * time.Millisecond)
	}
	return false
}
