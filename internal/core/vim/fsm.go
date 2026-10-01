package vim

// Mode represents the active Vim editing mode.
type Mode string

const (
	ModeNormal     Mode = "NORMAL"
	ModeInsert     Mode = "INSERT"
	ModeVisual     Mode = "VISUAL"
	ModeVisualLine Mode = "V-LINE"
)

// EventType categorizes the action output from the Vim FSM.
type EventType int

const (
	EventNone EventType = iota
	EventInsertChar
	EventCommand
	EventEnterInsert
	EventEnterNormal
	EventEnterVisual
)

// VimAction is emitted by the FSM to be executed by the editor engine.
type VimAction struct {
	Type    EventType
	Char    rune
	Command string
	Count   int
	Handled bool
}

// FSM is a deterministic finite-state machine implementing modal Vim editing.
type FSM struct {
	Mode         Mode
	pendingOp    rune
	count        int
	yankRegister string
}

// NewFSM creates a new Vim FSM starting in Normal mode.
func NewFSM() *FSM {
	return &FSM{
		Mode: ModeNormal,
	}
}

// Reset clears pending operators and multipliers.
func (f *FSM) Reset() {
	f.pendingOp = 0
	f.count = 0
}

// GetCountAndReset extracts the accumulated count (default 1) and resets it.
func (f *FSM) GetCountAndReset() int {
	c := f.count
	if c <= 0 {
		c = 1
	}
	f.count = 0
	return c
}

// SetYankBuffer saves text in the unnamed yank register.
func (f *FSM) SetYankBuffer(text string) {
	f.yankRegister = text
}

// YankBuffer returns text in the unnamed yank register.
func (f *FSM) YankBuffer() string {
	return f.yankRegister
}

// HandleKey handles key input and transitions the FSM.
func (f *FSM) HandleKey(r rune, keyType string, ctrl, alt, shift bool) VimAction {
	switch f.Mode {
	case ModeInsert:
		return f.handleInsert(r, keyType, ctrl, alt, shift)
	case ModeVisual, ModeVisualLine:
		return f.handleVisual(r, keyType, ctrl, alt, shift)
	case ModeNormal:
		fallthrough
	default:
		return f.handleNormal(r, keyType, ctrl, alt, shift)
	}
}

func (f *FSM) handleInsert(r rune, keyType string, ctrl, alt, shift bool) VimAction {
	if keyType == "escape" || r == 27 {
		f.Mode = ModeNormal
		f.Reset()
		return VimAction{
			Type:    EventEnterNormal,
			Command: "cursor.left",
			Handled: true,
		}
	}
	return VimAction{
		Type:    EventInsertChar,
		Char:    r,
		Handled: false, // Let normal editor typing handle it
	}
}

func (f *FSM) handleVisual(r rune, keyType string, ctrl, alt, shift bool) VimAction {
	if keyType == "escape" || r == 27 {
		f.Mode = ModeNormal
		f.Reset()
		return VimAction{
			Type:    EventEnterNormal,
			Command: "visual.clear",
			Handled: true,
		}
	}

	switch r {
	case 'y':
		f.Mode = ModeNormal
		f.Reset()
		return VimAction{
			Type:    EventEnterNormal,
			Command: "visual.yank",
			Handled: true,
		}
	case 'd', 'x':
		f.Mode = ModeNormal
		f.Reset()
		return VimAction{
			Type:    EventEnterNormal,
			Command: "visual.delete",
			Handled: true,
		}
	case 'c':
		f.Mode = ModeInsert
		f.Reset()
		return VimAction{
			Type:    EventEnterInsert,
			Command: "visual.change",
			Handled: true,
		}
	case 'h':
		return VimAction{Type: EventCommand, Command: "visual.left", Handled: true}
	case 'l':
		return VimAction{Type: EventCommand, Command: "visual.right", Handled: true}
	case 'j':
		return VimAction{Type: EventCommand, Command: "visual.down", Handled: true}
	case 'k':
		return VimAction{Type: EventCommand, Command: "visual.up", Handled: true}
	case 'w':
		return VimAction{Type: EventCommand, Command: "visual.word_right", Handled: true}
	case 'b':
		return VimAction{Type: EventCommand, Command: "visual.word_left", Handled: true}
	case '$':
		return VimAction{Type: EventCommand, Command: "visual.line_end", Handled: true}
	case '0', '^':
		return VimAction{Type: EventCommand, Command: "visual.line_start", Handled: true}
	}

	return VimAction{Handled: false}
}

func (f *FSM) handleNormal(r rune, keyType string, ctrl, alt, shift bool) VimAction {
	if keyType == "escape" || r == 27 {
		f.Reset()
		return VimAction{Type: EventNone, Handled: true}
	}

	// Handle redo Ctrl+R
	if ctrl && (r == 'r' || r == 18) {
		f.Reset()
		return VimAction{Type: EventCommand, Command: "history.redo", Handled: true}
	}

	// Handle counts (e.g. 5j, 10dd)
	if f.pendingOp == 0 && r >= '1' && r <= '9' {
		f.count = f.count*10 + int(r-'0')
		return VimAction{Type: EventNone, Handled: true}
	} else if f.pendingOp == 0 && f.count > 0 && r == '0' {
		f.count = f.count * 10
		return VimAction{Type: EventNone, Handled: true}
	}

	// Two-key operators (dd, yy, gg)
	if f.pendingOp != 0 {
		op := f.pendingOp
		f.pendingOp = 0
		cnt := f.GetCountAndReset()

		if op == 'd' && r == 'd' {
			return VimAction{Type: EventCommand, Command: "edit.delete_line", Count: cnt, Handled: true}
		}
		if op == 'd' && r == 'w' {
			return VimAction{Type: EventCommand, Command: "edit.delete_word", Count: cnt, Handled: true}
		}
		if op == 'y' && r == 'y' {
			return VimAction{Type: EventCommand, Command: "edit.yank_line", Count: cnt, Handled: true}
		}
		if op == 'c' && r == 'w' {
			f.Mode = ModeInsert
			return VimAction{Type: EventEnterInsert, Command: "edit.change_word", Count: cnt, Handled: true}
		}
		if op == 'g' && r == 'g' {
			return VimAction{Type: EventCommand, Command: "cursor.top", Handled: true}
		}
		return VimAction{Type: EventNone, Handled: true}
	}

	// Check if this key starts an operator (d, y, c, g)
	if r == 'd' || r == 'y' || r == 'c' || r == 'g' {
		f.pendingOp = r
		return VimAction{Type: EventNone, Handled: true}
	}

	cnt := f.GetCountAndReset()

	switch r {
	case 'h':
		return VimAction{Type: EventCommand, Command: "cursor.left", Count: cnt, Handled: true}
	case 'j':
		return VimAction{Type: EventCommand, Command: "cursor.down", Count: cnt, Handled: true}
	case 'k':
		return VimAction{Type: EventCommand, Command: "cursor.up", Count: cnt, Handled: true}
	case 'l':
		return VimAction{Type: EventCommand, Command: "cursor.right", Count: cnt, Handled: true}
	case 'w':
		return VimAction{Type: EventCommand, Command: "cursor.word_right", Count: cnt, Handled: true}
	case 'b':
		return VimAction{Type: EventCommand, Command: "cursor.word_left", Count: cnt, Handled: true}
	case '0', '^':
		return VimAction{Type: EventCommand, Command: "cursor.line_start", Handled: true}
	case '$':
		return VimAction{Type: EventCommand, Command: "cursor.line_end", Handled: true}
	case 'G':
		return VimAction{Type: EventCommand, Command: "cursor.bottom", Handled: true}
	case 'x':
		return VimAction{Type: EventCommand, Command: "edit.delete_forward", Count: cnt, Handled: true}
	case 'u':
		return VimAction{Type: EventCommand, Command: "history.undo", Handled: true}
	case 'p':
		return VimAction{Type: EventCommand, Command: "edit.paste_after", Handled: true}
	case 'P':
		return VimAction{Type: EventCommand, Command: "edit.paste_before", Handled: true}
	case 'i':
		f.Mode = ModeInsert
		return VimAction{Type: EventEnterInsert, Handled: true}
	case 'a':
		f.Mode = ModeInsert
		return VimAction{Type: EventEnterInsert, Command: "cursor.right", Handled: true}
	case 'o':
		f.Mode = ModeInsert
		return VimAction{Type: EventEnterInsert, Command: "edit.open_line_below", Handled: true}
	case 'O':
		f.Mode = ModeInsert
		return VimAction{Type: EventEnterInsert, Command: "edit.open_line_above", Handled: true}
	case 'v':
		f.Mode = ModeVisual
		return VimAction{Type: EventEnterVisual, Handled: true}
	case 'V':
		f.Mode = ModeVisualLine
		return VimAction{Type: EventEnterVisual, Command: "visual.line", Handled: true}
	}

	return VimAction{Handled: false}
}
