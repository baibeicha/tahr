package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"tahr/internal/core/buffer"
	"tahr/internal/core/lsp"
)

// Viewport represents the visible viewing window into a document buffer.
type Viewport struct {
	ViewportX   int // Horizontal scroll offset (in visual display columns)
	ViewportY   int // Vertical scroll offset (0-based document line index)
	Width       int // Total visible editor width (including gutter)
	Height      int // Total visible editor height (rows)
	GutterWidth int // Reserved columns for gutter line numbers & glyphs
	ScrolloffX  int // Horizontal scroll padding margin
	ScrolloffY  int // Vertical scroll padding margin
}

// ContentWidth returns usable width for text content.
func (v *Viewport) ContentWidth() int {
	w := v.Width - v.GutterWidth
	if w < 1 {
		return 1
	}
	return w
}

// ScrollToCursor adjusts ViewportX and ViewportY so the primary cursor is visible within margins.
func (v *Viewport) ScrollToCursor(buf *buffer.BufferImpl) {
	if buf == nil {
		return
	}
	primary := buf.PrimarySelection()
	head := primary.Head

	// 1. Vertical auto-scroll
	curLine := head.Line
	contentH := v.Height
	if contentH <= 0 {
		contentH = 1
	}

	scrolloffY := v.ScrolloffY
	if scrolloffY*2 >= contentH {
		scrolloffY = max(0, (contentH-1)/2)
	}

	if curLine < v.ViewportY+scrolloffY {
		v.ViewportY = curLine - scrolloffY
	} else if curLine >= v.ViewportY+contentH-scrolloffY {
		v.ViewportY = curLine - contentH + 1 + scrolloffY
	}

	totalLines := buf.TotalLines()
	if totalLines > 0 && v.ViewportY > totalLines-1 {
		v.ViewportY = totalLines - 1
	}
	if v.ViewportY < 0 {
		v.ViewportY = 0
	}

	// 2. Horizontal auto-scroll (based on VisualColumn)
	curVisualCol := buf.VisualColumn(head.Line, head.Column)
	contentW := v.ContentWidth()

	scrolloffX := v.ScrolloffX
	if scrolloffX*2 >= contentW {
		scrolloffX = max(0, (contentW-1)/2)
	}

	if curVisualCol < v.ViewportX+scrolloffX {
		v.ViewportX = curVisualCol - scrolloffX
	} else if curVisualCol >= v.ViewportX+contentW-scrolloffX {
		v.ViewportX = curVisualCol - contentW + 1 + scrolloffX
	}
	if v.ViewportX < 0 {
		v.ViewportX = 0
	}
}

// ComputeGutterWidth calculates dynamic gutter width based on total lines.
// Standard layout: [Marker(1)] [Digits(D)] [Separator(1)] = D + 2, with minimum 5.
func ComputeGutterWidth(totalLines int) int {
	digits := 1
	for n := max(1, totalLines); n >= 10; n /= 10 {
		digits++
	}
	w := digits + 2
	if w < 5 {
		return 5
	}
	return w
}

// Document wraps a BufferImpl with per-buffer viewport state, file metadata, breakpoints, and diagnostics.
type Document struct {
	ID          string
	FilePath    string
	Buffer      *buffer.BufferImpl
	Viewport    Viewport
	LanguageID  string
	Breakpoints map[int]bool
	Diagnostics map[int]string
	VirtualText *buffer.VirtualTextManager
}

// NewDocument creates a new Document with initialized buffer and viewport.
func NewDocument(id, filePath string, buf *buffer.BufferImpl) *Document {
	if buf == nil {
		buf = buffer.NewBuffer()
	}
	doc := &Document{
		ID:          id,
		FilePath:    filePath,
		Buffer:      buf,
		Breakpoints: make(map[int]bool),
		Diagnostics: make(map[int]string),
		VirtualText: buffer.NewVirtualTextManager(),
		Viewport: Viewport{
			ViewportX:   0,
			ViewportY:   0,
			Width:       80,
			Height:      23, // 24 total - 1 status row
			GutterWidth: 5,
			ScrolloffX:  2,
			ScrolloffY:  2,
		},
	}
	doc.Viewport.GutterWidth = ComputeGutterWidth(buf.TotalLines())
	return doc
}

// ToggleBreakpoint toggles a breakpoint at the given 0-based document line.
func (d *Document) ToggleBreakpoint(line int) bool {
	if line < 0 || line >= d.Buffer.TotalLines() {
		return false
	}
	if d.Breakpoints[line] {
		delete(d.Breakpoints, line)
		return false
	}
	d.Breakpoints[line] = true
	return true
}

// HasBreakpoint returns whether a breakpoint is set on line.
func (d *Document) HasBreakpoint(line int) bool {
	return d.Breakpoints[line]
}

// SetDiagnostic sets a diagnostic message on the specified 0-based line.
func (d *Document) SetDiagnostic(line int, msg string) {
	d.Diagnostics[line] = msg
}

// ClearDiagnostics removes all diagnostic markers from this document.
func (d *Document) ClearDiagnostics() {
	d.Diagnostics = make(map[int]string)
}

// ApplyTextEdits applies LSP TextEdits bottom-to-top to preserve coordinate stability.
func (d *Document) ApplyTextEdits(edits []lsp.TextEdit) error {
	if len(edits) == 0 || d.Buffer == nil {
		return nil
	}

	type sortableEdit struct {
		startPos buffer.Position
		endPos   buffer.Position
		newText  string
	}

	converted := make([]sortableEdit, 0, len(edits))
	for _, e := range edits {
		startPos := d.Buffer.LSPToPosition(e.Range.Start.Line, e.Range.Start.Character)
		endPos := d.Buffer.LSPToPosition(e.Range.End.Line, e.Range.End.Character)
		converted = append(converted, sortableEdit{
			startPos: startPos,
			endPos:   endPos,
			newText:  e.NewText,
		})
	}

	// Sort bottom-to-top: highest start byte offset first
	sort.Slice(converted, func(i, j int) bool {
		return converted[i].startPos.Byte > converted[j].startPos.Byte
	})

	for _, ce := range converted {
		delLen := ce.endPos.Byte - ce.startPos.Byte
		if delLen < 0 {
			delLen = 0
		}
		if err := d.Buffer.ApplyEdit(ce.startPos.Byte, delLen, ce.newText); err != nil {
			return err
		}
	}

	d.Viewport.ScrollToCursor(d.Buffer)
	return nil
}

// CommandID represents an abstract editor action.
type CommandID string

const (
	// Navigation
	CmdCursorLeft      CommandID = "cursor.left"
	CmdCursorRight     CommandID = "cursor.right"
	CmdCursorUp        CommandID = "cursor.up"
	CmdCursorDown      CommandID = "cursor.down"
	CmdCursorLineStart CommandID = "cursor.line_start"
	CmdCursorLineEnd   CommandID = "cursor.line_end"
	CmdCursorDocStart  CommandID = "cursor.doc_start"
	CmdCursorDocEnd    CommandID = "cursor.doc_end"
	CmdPageUp          CommandID = "cursor.page_up"
	CmdPageDown        CommandID = "cursor.page_down"

	// Selection
	CmdSelectLeft      CommandID = "select.left"
	CmdSelectRight     CommandID = "select.right"
	CmdSelectUp        CommandID = "select.up"
	CmdSelectDown      CommandID = "select.down"
	CmdSelectLineStart CommandID = "select.line_start"
	CmdSelectLineEnd   CommandID = "select.line_end"
	CmdSelectDocStart  CommandID = "select.doc_start"
	CmdSelectDocEnd    CommandID = "select.doc_end"
	CmdSelectAll       CommandID = "select.all"
	CmdSelectNextMatch CommandID = "select.next_match"
	CmdClearSelections CommandID = "select.clear"
	CmdAddCursor       CommandID = "select.add_cursor"

	// Editing
	CmdInsertChar       CommandID = "edit.insert_char"
	CmdInsertText       CommandID = "edit.insert_text"
	CmdInsertNewline    CommandID = "edit.insert_newline"
	CmdDeleteBackward   CommandID = "edit.delete_backward"
	CmdDeleteForward    CommandID = "edit.delete_forward"
	CmdIndent           CommandID = "edit.indent"
	CmdDedent           CommandID = "edit.dedent"
	CmdToggleAutoPairs  CommandID = "edit.toggle_auto_pairs"

	// History
	CmdUndo CommandID = "history.undo"
	CmdRedo CommandID = "history.redo"

	// Buffer & File
	CmdFileSave     CommandID = "file.save"
	CmdFileOpen     CommandID = "file.open"
	CmdBufferClose  CommandID = "buffer.close"
	CmdBufferNext   CommandID = "buffer.next"
	CmdBufferPrev   CommandID = "buffer.prev"
	CmdBufferSwitch CommandID = "buffer.switch"

	// Viewport & Gutter
	CmdToggleBreakpoint CommandID = "gutter.toggle_breakpoint"
	CmdScroll           CommandID = "view.scroll"
	CmdResize           CommandID = "view.resize"
)

// Command carries an action ID and optional payload.
type Command struct {
	ID   CommandID
	Args any
}

// Engine is the central headless coordinator managing open documents, viewports, and action dispatch.
type Engine struct {
	mu               sync.RWMutex
	docs             map[string]*Document
	docOrder         []string
	activeID         string
	nextID           int
	width            int
	height           int
	statusMsg        string
	handlers         map[CommandID]func(cmd Command) error
	tabSize          int
	useSpaces        bool
	autoPairsEnabled bool
}

// NewEngine initializes an empty headless engine with a default untitled document.
func NewEngine() *Engine {
	e := &Engine{
		docs:             make(map[string]*Document),
		docOrder:         make([]string, 0),
		width:            80,
		height:           24,
		statusMsg:        "Ready",
		handlers:         make(map[CommandID]func(cmd Command) error),
		tabSize:          4,
		useSpaces:        true,
		autoPairsEnabled: true,
	}
	e.registerDefaultCommands()
	e.NewUntitled()
	return e
}

// SetIndentation configures the engine's tab size and space indentation preference.
func (e *Engine) SetIndentation(tabSize int, useSpaces bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if tabSize <= 0 {
		tabSize = 4
	}
	e.tabSize = tabSize
	e.useSpaces = useSpaces
}

// Indentation returns the current tab size and space preference.
func (e *Engine) Indentation() (int, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ts := e.tabSize
	if ts <= 0 {
		ts = 4
	}
	return ts, e.useSpaces
}

// Width returns current engine width.
func (e *Engine) Width() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.width
}

// Height returns current engine height.
func (e *Engine) Height() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.height
}

// StatusMessage returns the current status message.
func (e *Engine) StatusMessage() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.statusMsg
}

// SetStatusMessage sets the engine status message.
func (e *Engine) SetStatusMessage(msg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.statusMsg = msg
}

// Open loads a file into the editor, activating the existing document if already open.
func (e *Engine) Open(path string) (*Document, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cleanPath := filepath.Clean(path)
	if abs, err := filepath.Abs(path); err == nil {
		cleanPath = filepath.Clean(abs)
	}
	for _, doc := range e.docs {
		if filepath.Clean(doc.FilePath) == cleanPath {
			e.activeID = doc.ID
			return doc, nil
		}
	}

	buf := buffer.NewBuffer()
	if _, err := os.Stat(cleanPath); err == nil {
		if err := buf.Load(cleanPath); err != nil {
			return nil, err
		}
	} else {
		buf.SetFilePath(cleanPath)
	}

	contentH := e.height - 1
	if contentH < 1 {
		contentH = 1
	}

	doc := NewDocument(cleanPath, path, buf)
	doc.Viewport.Width = e.width
	doc.Viewport.Height = contentH
	doc.Viewport.GutterWidth = ComputeGutterWidth(buf.TotalLines())
	doc.Viewport.ScrollToCursor(buf)

	e.docs[doc.ID] = doc
	e.docOrder = append(e.docOrder, doc.ID)
	e.activeID = doc.ID
	return doc, nil
}

// NewUntitled creates and activates a new untitled buffer.
func (e *Engine) NewUntitled() *Document {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.nextID++
	id := fmt.Sprintf("untitled-%d", e.nextID)

	contentH := e.height - 1
	if contentH < 1 {
		contentH = 1
	}

	doc := NewDocument(id, id, buffer.NewBuffer())
	doc.Viewport.Width = e.width
	doc.Viewport.Height = contentH
	doc.Viewport.GutterWidth = 5

	e.docs[id] = doc
	e.docOrder = append(e.docOrder, id)
	e.activeID = id
	return doc
}

// CloseBuffer closes the document by ID. If empty, creates a new untitled buffer.
func (e *Engine) CloseBuffer(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if id == "" {
		id = e.activeID
	}
	delete(e.docs, id)

	newOrder := make([]string, 0, len(e.docOrder))
	for _, docID := range e.docOrder {
		if docID != id {
			newOrder = append(newOrder, docID)
		}
	}
	e.docOrder = newOrder

	if e.activeID == id {
		if len(e.docOrder) > 0 {
			e.activeID = e.docOrder[len(e.docOrder)-1]
		} else {
			e.mu.Unlock()
			e.NewUntitled()
			e.mu.Lock()
		}
	}
}

// ActiveDocument returns the active document under read lock.
func (e *Engine) ActiveDocument() *Document {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.docs[e.activeID]
}

// Documents returns a slice of all open documents in order.
func (e *Engine) Documents() []*Document {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*Document, len(e.docOrder))
	for i, id := range e.docOrder {
		out[i] = e.docs[id]
	}
	return out
}

// SwitchBuffer activates the document with the given ID or matching file path.
func (e *Engine) SwitchBuffer(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.docs[id]; ok {
		e.activeID = id
		return nil
	}

	clean := filepath.Clean(id)
	for _, doc := range e.docs {
		if filepath.Clean(doc.FilePath) == clean || doc.ID == id {
			e.activeID = doc.ID
			return nil
		}
	}

	return fmt.Errorf("document not found: %s", id)
}

// NextBuffer switches to the next open buffer in round-robin order.
func (e *Engine) NextBuffer() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.docOrder) <= 1 {
		return
	}
	for i, id := range e.docOrder {
		if id == e.activeID {
			nextIdx := (i + 1) % len(e.docOrder)
			e.activeID = e.docOrder[nextIdx]
			return
		}
	}
}

// PrevBuffer switches to the previous open buffer in round-robin order.
func (e *Engine) PrevBuffer() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.docOrder) <= 1 {
		return
	}
	for i, id := range e.docOrder {
		if id == e.activeID {
			prevIdx := (i - 1 + len(e.docOrder)) % len(e.docOrder)
			e.activeID = e.docOrder[prevIdx]
			return
		}
	}
}

// Resize updates the editor dimensions and viewports.
func (e *Engine) Resize(width, height int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	e.width = width
	e.height = height
	contentH := height - 1
	if contentH < 1 {
		contentH = 1
	}

	for _, doc := range e.docs {
		doc.Viewport.Width = width
		doc.Viewport.Height = contentH
		doc.Viewport.GutterWidth = ComputeGutterWidth(doc.Buffer.TotalLines())
		doc.Viewport.ScrollToCursor(doc.Buffer)
	}
}

// RegisterHandler binds a custom command handler.
func (e *Engine) RegisterHandler(id CommandID, handler func(cmd Command) error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[id] = handler
}

// Dispatch executes a command on the active document.
func (e *Engine) Dispatch(cmd Command) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	handler, ok := e.handlers[cmd.ID]
	if !ok {
		return fmt.Errorf("unhandled command: %s", cmd.ID)
	}
	return handler(cmd)
}

// registerDefaultCommands sets up built-in editor action handlers.
func (e *Engine) registerDefaultCommands() {
	// Navigation & Selection
	e.handlers[CmdCursorLeft] = func(cmd Command) error { return e.handleMoveCursor(0, -1, false) }
	e.handlers[CmdCursorRight] = func(cmd Command) error { return e.handleMoveCursor(0, 1, false) }
	e.handlers[CmdCursorUp] = func(cmd Command) error { return e.handleMoveCursor(-1, 0, false) }
	e.handlers[CmdCursorDown] = func(cmd Command) error { return e.handleMoveCursor(1, 0, false) }
	e.handlers[CmdCursorLineStart] = func(cmd Command) error { return e.handleMoveLineStart(false) }
	e.handlers[CmdCursorLineEnd] = func(cmd Command) error { return e.handleMoveLineEnd(false) }
	e.handlers[CmdCursorDocStart] = func(cmd Command) error { return e.handleMoveDocStart(false) }
	e.handlers[CmdCursorDocEnd] = func(cmd Command) error { return e.handleMoveDocEnd(false) }
	e.handlers[CmdPageUp] = func(cmd Command) error { return e.handlePageScroll(-1, false) }
	e.handlers[CmdPageDown] = func(cmd Command) error { return e.handlePageScroll(1, false) }

	e.handlers[CmdSelectLeft] = func(cmd Command) error { return e.handleMoveCursor(0, -1, true) }
	e.handlers[CmdSelectRight] = func(cmd Command) error { return e.handleMoveCursor(0, 1, true) }
	e.handlers[CmdSelectUp] = func(cmd Command) error { return e.handleMoveCursor(-1, 0, true) }
	e.handlers[CmdSelectDown] = func(cmd Command) error { return e.handleMoveCursor(1, 0, true) }
	e.handlers[CmdSelectLineStart] = func(cmd Command) error { return e.handleMoveLineStart(true) }
	e.handlers[CmdSelectLineEnd] = func(cmd Command) error { return e.handleMoveLineEnd(true) }
	e.handlers[CmdSelectDocStart] = func(cmd Command) error { return e.handleMoveDocStart(true) }
	e.handlers[CmdSelectDocEnd] = func(cmd Command) error { return e.handleMoveDocEnd(true) }
	e.handlers[CmdSelectAll] = func(cmd Command) error { return e.handleSelectAll() }
	e.handlers[CmdSelectNextMatch] = func(cmd Command) error { return e.handleSelectNextMatch() }
	e.handlers[CmdClearSelections] = func(cmd Command) error { return e.handleClearSelections() }
	e.handlers[CmdAddCursor] = func(cmd Command) error { return e.handleAddCursor(cmd.Args) }

	// Editing
	e.handlers[CmdInsertChar] = func(cmd Command) error {
		if r, ok := cmd.Args.(rune); ok {
			return e.handleInsertText(string(r))
		}
		if s, ok := cmd.Args.(string); ok {
			return e.handleInsertText(s)
		}
		return errors.New("CmdInsertChar requires rune or string arg")
	}
	e.handlers[CmdInsertText] = func(cmd Command) error {
		if s, ok := cmd.Args.(string); ok {
			return e.handleInsertText(s)
		}
		return errors.New("CmdInsertText requires string arg")
	}
	e.handlers[CmdInsertNewline] = func(cmd Command) error { return e.handleInsertText("\n") }
	e.handlers[CmdDeleteBackward] = func(cmd Command) error { return e.handleDeleteBackward() }
	e.handlers[CmdDeleteForward] = func(cmd Command) error { return e.handleDeleteForward() }
	e.handlers[CmdIndent] = func(cmd Command) error { return e.handleIndent() }
	e.handlers[CmdDedent] = func(cmd Command) error { return e.handleDedent() }
	e.handlers[CmdToggleAutoPairs] = func(cmd Command) error {
		e.autoPairsEnabled = !e.autoPairsEnabled
		return nil
	}

	// History
	e.handlers[CmdUndo] = func(cmd Command) error { return e.handleUndo() }
	e.handlers[CmdRedo] = func(cmd Command) error { return e.handleRedo() }

	// File & Buffer
	e.handlers[CmdFileSave] = func(cmd Command) error { return e.handleFileSave(cmd.Args) }
	e.handlers[CmdFileOpen] = func(cmd Command) error {
		if path, ok := cmd.Args.(string); ok {
			_, err := e.openLocked(path)
			return err
		}
		return errors.New("CmdFileOpen requires string path")
	}
	e.handlers[CmdBufferClose] = func(cmd Command) error {
		id := ""
		if s, ok := cmd.Args.(string); ok {
			id = s
		}
		e.closeBufferLocked(id)
		return nil
	}
	e.handlers[CmdBufferNext] = func(cmd Command) error {
		e.nextBufferLocked()
		return nil
	}
	e.handlers[CmdBufferPrev] = func(cmd Command) error {
		e.prevBufferLocked()
		return nil
	}
	e.handlers[CmdBufferSwitch] = func(cmd Command) error {
		if id, ok := cmd.Args.(string); ok {
			return e.switchBufferLocked(id)
		}
		return errors.New("CmdBufferSwitch requires string ID")
	}

	// Gutter & View
	e.handlers[CmdToggleBreakpoint] = func(cmd Command) error { return e.handleToggleBreakpoint(cmd.Args) }
	e.handlers[CmdScroll] = func(cmd Command) error { return e.handleScroll(cmd.Args) }
	e.handlers[CmdResize] = func(cmd Command) error {
		if dims, ok := cmd.Args.([2]int); ok {
			e.resizeLocked(dims[0], dims[1])
			return nil
		}
		return errors.New("CmdResize requires [2]int{width, height}")
	}
}

// ============================================================================
// Internal Command Implementations (called under e.mu.Lock)
// ============================================================================

func (e *Engine) currentDoc() *Document {
	return e.docs[e.activeID]
}

func (e *Engine) handleMoveCursor(dRow, dCol int, extend bool) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	buf := doc.Buffer
	sels := buf.GetSelections()
	newSels := make([]buffer.Selection, len(sels))

	for i, sel := range sels {
		head := sel.Head
		var newHead buffer.Position

		if dCol != 0 {
			// Horizontal movement
			if dCol < 0 {
				// Move left
				if head.Byte > 0 {
					lineStart, _ := buf.ByteOffsetForLine(head.Line)
					if head.Byte > lineStart {
						lineBytes, _ := buf.GetLine(head.Line)
						offsetInLine := head.Byte - lineStart
						if offsetInLine > len(lineBytes) {
							offsetInLine = len(lineBytes)
						}
						_, sz := utf8.DecodeLastRune(lineBytes[:offsetInLine])
						newHead = buf.ByteToPosition(head.Byte - sz)
					} else {
						// Jump to end of previous line
						newHead = buf.ByteToPosition(head.Byte - 1)
					}
				} else {
					newHead = head
				}
			} else {
				// Move right
				totalBytes := buf.TotalBytes()
				if head.Byte < totalBytes {
					lineStart, _ := buf.ByteOffsetForLine(head.Line)
					lineBytes, _ := buf.GetLine(head.Line)
					offsetInLine := head.Byte - lineStart
					if offsetInLine < len(lineBytes) {
						_, sz := utf8.DecodeRune(lineBytes[offsetInLine:])
						newHead = buf.ByteToPosition(head.Byte + sz)
					} else {
						// Jump past newline to start of next line
						newHead = buf.ByteToPosition(head.Byte + 1)
					}
				} else {
					newHead = head
				}
			}
		} else if dRow != 0 {
			// Vertical movement preserving visual column
			targetLine := head.Line + dRow
			if targetLine < 0 {
				targetLine = 0
			}
			if targetLine >= buf.TotalLines() {
				targetLine = buf.TotalLines() - 1
			}

			curVisualCol := buf.VisualColumn(head.Line, head.Column)
			targetRuneCol := buf.RuneColFromVisual(targetLine, curVisualCol)

			lineStart, _ := buf.ByteOffsetForLine(targetLine)
			lineBytes, _ := buf.GetLine(targetLine)

			// Walk targetRuneCol runes into lineBytes
			byteOffset := 0
			rCount := 0
			for byteOffset < len(lineBytes) && rCount < targetRuneCol {
				_, sz := utf8.DecodeRune(lineBytes[byteOffset:])
				byteOffset += sz
				rCount++
			}
			newHead = buf.ByteToPosition(lineStart + byteOffset)
		} else {
			newHead = head
		}

		anchor := sel.Anchor
		if !extend {
			anchor = newHead
		}
		newSels[i] = buffer.NewSelection(anchor, newHead)
	}

	buf.SetSelections(newSels)
	doc.Viewport.ScrollToCursor(buf)
	return nil
}

func (e *Engine) handleMoveLineStart(extend bool) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	buf := doc.Buffer
	sels := buf.GetSelections()
	newSels := make([]buffer.Selection, len(sels))

	for i, sel := range sels {
		lineStart, _ := buf.ByteOffsetForLine(sel.Head.Line)
		newHead := buf.ByteToPosition(lineStart)
		anchor := sel.Anchor
		if !extend {
			anchor = newHead
		}
		newSels[i] = buffer.NewSelection(anchor, newHead)
	}

	buf.SetSelections(newSels)
	doc.Viewport.ScrollToCursor(buf)
	return nil
}

func (e *Engine) handleMoveLineEnd(extend bool) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	buf := doc.Buffer
	sels := buf.GetSelections()
	newSels := make([]buffer.Selection, len(sels))

	for i, sel := range sels {
		lineStart, _ := buf.ByteOffsetForLine(sel.Head.Line)
		lineBytes, _ := buf.GetLine(sel.Head.Line)
		endOffset := lineStart + len(lineBytes)
		for endOffset > lineStart && (lineBytes[endOffset-lineStart-1] == '\n' || lineBytes[endOffset-lineStart-1] == '\r') {
			endOffset--
		}
		newHead := buf.ByteToPosition(endOffset)
		anchor := sel.Anchor
		if !extend {
			anchor = newHead
		}
		newSels[i] = buffer.NewSelection(anchor, newHead)
	}

	buf.SetSelections(newSels)
	doc.Viewport.ScrollToCursor(buf)
	return nil
}

func (e *Engine) handleMoveDocStart(extend bool) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	buf := doc.Buffer
	newHead := buf.ByteToPosition(0)
	primary := buf.PrimarySelection()
	anchor := primary.Anchor
	if !extend {
		anchor = newHead
	}
	buf.SetSelections([]buffer.Selection{buffer.NewSelection(anchor, newHead)})
	doc.Viewport.ScrollToCursor(buf)
	return nil
}

func (e *Engine) handleMoveDocEnd(extend bool) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	buf := doc.Buffer
	newHead := buf.ByteToPosition(buf.TotalBytes())
	primary := buf.PrimarySelection()
	anchor := primary.Anchor
	if !extend {
		anchor = newHead
	}
	buf.SetSelections([]buffer.Selection{buffer.NewSelection(anchor, newHead)})
	doc.Viewport.ScrollToCursor(buf)
	return nil
}

func (e *Engine) handlePageScroll(direction int, extend bool) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	pageSize := doc.Viewport.Height - 2
	if pageSize < 1 {
		pageSize = 1
	}
	return e.handleMoveCursor(direction*pageSize, 0, extend)
}

func (e *Engine) handleSelectAll() error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	buf := doc.Buffer
	start := buf.ByteToPosition(0)
	end := buf.ByteToPosition(buf.TotalBytes())
	buf.SetSelections([]buffer.Selection{buffer.NewSelection(start, end)})
	return nil
}

func (e *Engine) handleSelectNextMatch() error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	ok := doc.Buffer.SelectNextOccurrence()
	if ok {
		doc.Viewport.ScrollToCursor(doc.Buffer)
	}
	return nil
}

func (e *Engine) handleClearSelections() error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	doc.Buffer.ClearSelections()
	return nil
}

func (e *Engine) handleAddCursor(args any) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	if pos, ok := args.(buffer.Position); ok {
		doc.Buffer.AddCursor(pos)
		doc.Viewport.ScrollToCursor(doc.Buffer)
		return nil
	}
	if coords, ok := args.([2]int); ok {
		line := coords[0]
		col := coords[1]
		if line < 0 {
			line = 0
		}
		if line >= doc.Buffer.TotalLines() {
			line = doc.Buffer.TotalLines() - 1
		}
		lineStart, _ := doc.Buffer.ByteOffsetForLine(line)
		lineBytes, _ := doc.Buffer.GetLine(line)
		byteOffset := 0
		rCount := 0
		for byteOffset < len(lineBytes) && rCount < col {
			_, sz := utf8.DecodeRune(lineBytes[byteOffset:])
			byteOffset += sz
			rCount++
		}
		pos := doc.Buffer.ByteToPosition(lineStart + byteOffset)
		doc.Buffer.AddCursor(pos)
		doc.Viewport.ScrollToCursor(doc.Buffer)
		return nil
	}
	return errors.New("CmdAddCursor requires Position or [2]int{line, col}")
}

func (e *Engine) handleInsertText(text string) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}

	if e.autoPairsEnabled && utf8.RuneCountInString(text) == 1 {
		r, _ := utf8.DecodeRuneInString(text)
		if e.tryHandleAutoPairs(doc, r) {
			doc.Viewport.GutterWidth = ComputeGutterWidth(doc.Buffer.TotalLines())
			doc.Viewport.ScrollToCursor(doc.Buffer)
			return nil
		}
	}

	doc.Buffer.InsertAtSelections(text)
	doc.Viewport.GutterWidth = ComputeGutterWidth(doc.Buffer.TotalLines())
	doc.Viewport.ScrollToCursor(doc.Buffer)
	return nil
}

func (e *Engine) tryHandleAutoPairs(doc *Document, r rune) bool {
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return false
	}

	totalBytes := doc.Buffer.TotalBytes()

	// 1. Check if selections are non-empty: wrap text!
	hasNonEmpty := false
	for _, s := range sels {
		if !s.IsEmpty() {
			hasNonEmpty = true
			break
		}
	}

	if hasNonEmpty {
		var openChar, closeChar rune
		switch r {
		case '(':
			openChar, closeChar = '(', ')'
		case '[':
			openChar, closeChar = '[', ']'
		case '{':
			openChar, closeChar = '{', '}'
		case '"':
			openChar, closeChar = '"', '"'
		case '\'':
			openChar, closeChar = '\'', '\''
		case '`':
			openChar, closeChar = '`', '`'
		default:
			return false
		}

		// Apply wrapping from bottom to top so offsets remain valid
		sort.Slice(sels, func(i, j int) bool {
			return sels[i].Start().Byte > sels[j].Start().Byte
		})

		newSels := make([]buffer.Selection, len(sels))
		for idx, s := range sels {
			start := s.Start().Byte
			end := s.End().Byte
			selectedBytes, _ := doc.Buffer.Rope().Slice(start, end)
			wrapped := string(openChar) + string(selectedBytes) + string(closeChar)
			_ = doc.Buffer.ApplyEdit(start, end-start, wrapped)
			newStart := doc.Buffer.ByteToPosition(start + 1)
			newEnd := doc.Buffer.ByteToPosition(start + 1 + len(selectedBytes))
			newSels[idx] = buffer.NewSelection(newStart, newEnd)
		}
		doc.Buffer.SetSelections(newSels)
		return true
	}

	// 2. Empty selection (normal point insertion)
	primary := sels[0]
	headByte := primary.Head.Byte

	// A. Skip closing character if typing the closing character right in front of it
	isClosingRune := (r == ')' || r == ']' || r == '}' || r == '"' || r == '\'' || r == '`')
	if isClosingRune && headByte < totalBytes {
		nextB, err := doc.Buffer.Rope().Slice(headByte, headByte+1)
		if err == nil && len(nextB) > 0 && rune(nextB[0]) == r {
			// Advance all cursors by 1 byte
			newSels := make([]buffer.Selection, len(sels))
			for i, s := range sels {
				nextPos := doc.Buffer.ByteToPosition(s.Head.Byte + 1)
				newSels[i] = buffer.NewSelection(nextPos, nextPos)
			}
			doc.Buffer.SetSelections(newSels)
			return true
		}
	}

	// B. Open pair insertion
	var closePair rune
	switch r {
	case '(':
		closePair = ')'
	case '[':
		closePair = ']'
	case '{':
		closePair = '}'
	case '"':
		closePair = '"'
	case '\'':
		closePair = '\''
	case '`':
		closePair = '`'
	default:
		return false
	}

	// Don't auto-quote if the next character is alphanumeric
	if headByte < totalBytes {
		nextB, _ := doc.Buffer.Rope().Slice(headByte, headByte+1)
		if len(nextB) > 0 {
			nr := rune(nextB[0])
			if unicode.IsLetter(nr) || unicode.IsDigit(nr) {
				return false
			}
		}
	}

	pairStr := string(r) + string(closePair)
	doc.Buffer.InsertAtSelections(pairStr)

	// Reposition cursor between the pair (1 char back from end of pair)
	curSels := doc.Buffer.GetSelections()
	adjustedSels := make([]buffer.Selection, len(curSels))
	for i, s := range curSels {
		midPos := doc.Buffer.ByteToPosition(s.Head.Byte - len(string(closePair)))
		adjustedSels[i] = buffer.NewSelection(midPos, midPos)
	}
	doc.Buffer.SetSelections(adjustedSels)
	return true
}

func (e *Engine) handleDeleteBackward() error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}

	if e.autoPairsEnabled {
		sels := doc.Buffer.GetSelections()
		if len(sels) == 1 && sels[0].IsEmpty() {
			headByte := sels[0].Head.Byte
			totalBytes := doc.Buffer.TotalBytes()
			if headByte > 0 && headByte < totalBytes {
				prevB, _ := doc.Buffer.Rope().Slice(headByte-1, headByte)
				nextB, _ := doc.Buffer.Rope().Slice(headByte, headByte+1)
				if len(prevB) > 0 && len(nextB) > 0 {
					p := rune(prevB[0])
					n := rune(nextB[0])
					isPair := (p == '(' && n == ')') ||
						(p == '[' && n == ']') ||
						(p == '{' && n == '}') ||
						(p == '"' && n == '"') ||
						(p == '\'' && n == '\'') ||
						(p == '`' && n == '`')
					if isPair {
						_ = doc.Buffer.ApplyEdit(headByte-1, 2, "")
						newPos := doc.Buffer.ByteToPosition(headByte - 1)
						doc.Buffer.SetSelections([]buffer.Selection{buffer.NewSelection(newPos, newPos)})
						doc.Viewport.GutterWidth = ComputeGutterWidth(doc.Buffer.TotalLines())
						doc.Viewport.ScrollToCursor(doc.Buffer)
						return nil
					}
				}
			}
		}
	}

	doc.Buffer.DeleteAtSelections()
	doc.Viewport.GutterWidth = ComputeGutterWidth(doc.Buffer.TotalLines())
	doc.Viewport.ScrollToCursor(doc.Buffer)
	return nil
}

// AutoPairsEnabled returns whether auto-closing brackets and quotes are enabled.
func (e *Engine) AutoPairsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.autoPairsEnabled
}

// SetAutoPairsEnabled sets whether auto-pairs are active.
func (e *Engine) SetAutoPairsEnabled(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.autoPairsEnabled = enabled
}

// ToggleAutoPairs switches auto-pairs state and returns the new value.
func (e *Engine) ToggleAutoPairs() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.autoPairsEnabled = !e.autoPairsEnabled
	return e.autoPairsEnabled
}

func (e *Engine) handleDeleteForward() error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	doc.Buffer.DeleteForwardAtSelections()
	doc.Viewport.GutterWidth = ComputeGutterWidth(doc.Buffer.TotalLines())
	doc.Viewport.ScrollToCursor(doc.Buffer)
	return nil
}

func (e *Engine) handleIndent() error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	buf := doc.Buffer
	sels := buf.GetSelections()
	tabSize := e.tabSize
	if tabSize <= 0 {
		tabSize = 4
	}
	useSpaces := e.useSpaces

	indentStr := "\t"
	if useSpaces {
		indentStr = strings.Repeat(" ", tabSize)
	}

	// Check if any selection is multi-line
	hasMultiLine := false
	for _, s := range sels {
		if s.Start().Line != s.End().Line {
			hasMultiLine = true
			break
		}
	}

	if !hasMultiLine {
		// Single-line carets: insert spaces/tab
		buf.InsertAtSelections(indentStr)
		doc.Viewport.ScrollToCursor(buf)
		return nil
	}

	// Multi-line block indentation
	// Collect unique affected lines
	lineMap := make(map[int]bool)
	for _, s := range sels {
		startLine := s.Start().Line
		endLine := s.End().Line
		if s.End().Column == 0 && endLine > startLine {
			// Selection ends at start of line: exclude that line
			endLine--
		}
		for l := startLine; l <= endLine; l++ {
			lineMap[l] = true
		}
	}

	// Apply bottom-to-top
	for l := buf.TotalLines() - 1; l >= 0; l-- {
		if lineMap[l] {
			lineBytes, _ := buf.GetLine(l)
			if len(lineBytes) > 0 { // Preserve empty lines without trailing spaces
				offset, err := buf.ByteOffsetForLine(l)
				if err == nil {
					_ = buf.ApplyEdit(offset, 0, indentStr)
				}
			}
		}
	}

	doc.Viewport.ScrollToCursor(buf)
	return nil
}

func (e *Engine) handleDedent() error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	buf := doc.Buffer
	sels := buf.GetSelections()

	tabSize := e.tabSize
	if tabSize <= 0 {
		tabSize = 4
	}

	spacePrefix := []byte(strings.Repeat(" ", tabSize))

	lineMap := make(map[int]bool)
	for _, s := range sels {
		startLine := s.Start().Line
		endLine := s.End().Line
		if s.End().Column == 0 && endLine > startLine {
			endLine--
		}
		for l := startLine; l <= endLine; l++ {
			lineMap[l] = true
		}
	}

	// Apply bottom-to-top
	for l := buf.TotalLines() - 1; l >= 0; l-- {
		if lineMap[l] {
			lineBytes, _ := buf.GetLine(l)
			offset, err := buf.ByteOffsetForLine(l)
			if err != nil {
				continue
			}
			if bytes.HasPrefix(lineBytes, []byte("\t")) {
				_ = buf.ApplyEdit(offset, 1, "")
			} else if bytes.HasPrefix(lineBytes, spacePrefix) {
				_ = buf.ApplyEdit(offset, tabSize, "")
			} else {
				// Remove up to (tabSize-1) leading spaces
				spaces := 0
				for spaces < len(lineBytes) && spaces < tabSize && lineBytes[spaces] == ' ' {
					spaces++
				}
				if spaces > 0 {
					_ = buf.ApplyEdit(offset, spaces, "")
				}
			}
		}
	}

	doc.Viewport.ScrollToCursor(buf)
	return nil
}

func (e *Engine) handleUndo() error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	if !doc.Buffer.Undo() {
		return errors.New("already at oldest change")
	}
	doc.Viewport.GutterWidth = ComputeGutterWidth(doc.Buffer.TotalLines())
	doc.Viewport.ScrollToCursor(doc.Buffer)
	return nil
}

func (e *Engine) handleRedo() error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	if !doc.Buffer.Redo() {
		return errors.New("already at newest change")
	}
	doc.Viewport.GutterWidth = ComputeGutterWidth(doc.Buffer.TotalLines())
	doc.Viewport.ScrollToCursor(doc.Buffer)
	return nil
}

func (e *Engine) handleFileSave(args any) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	targetPath := doc.FilePath
	if s, ok := args.(string); ok && s != "" {
		targetPath = s
	}
	if targetPath == "" || strings.HasPrefix(targetPath, "untitled-") {
		return errors.New("cannot save untitled buffer without destination path")
	}
	err := doc.Buffer.SaveAtomic(targetPath)
	if err == nil {
		doc.FilePath = targetPath
		e.statusMsg = fmt.Sprintf("Saved %s", filepath.Base(targetPath))
	}
	return err
}

func (e *Engine) handleToggleBreakpoint(args any) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	line := doc.Buffer.PrimarySelection().Head.Line
	if l, ok := args.(int); ok {
		line = l
	}
	doc.ToggleBreakpoint(line)
	return nil
}

func (e *Engine) handleScroll(args any) error {
	doc := e.currentDoc()
	if doc == nil {
		return errors.New("no active document")
	}
	if dY, ok := args.(int); ok {
		doc.Viewport.ViewportY += dY
		if doc.Viewport.ViewportY < 0 {
			doc.Viewport.ViewportY = 0
		}
		totalLines := doc.Buffer.TotalLines()
		if totalLines > 0 && doc.Viewport.ViewportY > totalLines-1 {
			doc.Viewport.ViewportY = totalLines - 1
		}
		return nil
	}
	return errors.New("CmdScroll requires int delta")
}

// Helpers called under lock
func (e *Engine) openLocked(path string) (*Document, error) {
	cleanPath := filepath.Clean(path)
	if abs, err := filepath.Abs(path); err == nil {
		cleanPath = filepath.Clean(abs)
	}
	for _, doc := range e.docs {
		if filepath.Clean(doc.FilePath) == cleanPath {
			e.activeID = doc.ID
			return doc, nil
		}
	}

	buf := buffer.NewBuffer()
	if _, err := os.Stat(cleanPath); err == nil {
		if err := buf.Load(cleanPath); err != nil {
			return nil, err
		}
	} else {
		buf.SetFilePath(cleanPath)
	}

	contentH := e.height - 1
	if contentH < 1 {
		contentH = 1
	}

	doc := NewDocument(cleanPath, path, buf)
	doc.Viewport.Width = e.width
	doc.Viewport.Height = contentH
	doc.Viewport.GutterWidth = ComputeGutterWidth(buf.TotalLines())
	doc.Viewport.ScrollToCursor(buf)

	e.docs[doc.ID] = doc
	e.docOrder = append(e.docOrder, doc.ID)
	e.activeID = doc.ID
	return doc, nil
}

func (e *Engine) closeBufferLocked(id string) {
	if id == "" {
		id = e.activeID
	}
	delete(e.docs, id)

	newOrder := make([]string, 0, len(e.docOrder))
	for _, docID := range e.docOrder {
		if docID != id {
			newOrder = append(newOrder, docID)
		}
	}
	e.docOrder = newOrder

	if e.activeID == id {
		if len(e.docOrder) > 0 {
			e.activeID = e.docOrder[len(e.docOrder)-1]
		} else {
			e.nextID++
			newID := fmt.Sprintf("untitled-%d", e.nextID)
			contentH := e.height - 1
			if contentH < 1 {
				contentH = 1
			}
			doc := NewDocument(newID, newID, buffer.NewBuffer())
			doc.Viewport.Width = e.width
			doc.Viewport.Height = contentH
			doc.Viewport.GutterWidth = 5
			e.docs[newID] = doc
			e.docOrder = append(e.docOrder, newID)
			e.activeID = newID
		}
	}
}

func (e *Engine) nextBufferLocked() {
	if len(e.docOrder) <= 1 {
		return
	}
	for i, id := range e.docOrder {
		if id == e.activeID {
			nextIdx := (i + 1) % len(e.docOrder)
			e.activeID = e.docOrder[nextIdx]
			return
		}
	}
}

func (e *Engine) prevBufferLocked() {
	if len(e.docOrder) <= 1 {
		return
	}
	for i, id := range e.docOrder {
		if id == e.activeID {
			prevIdx := (i - 1 + len(e.docOrder)) % len(e.docOrder)
			e.activeID = e.docOrder[prevIdx]
			return
		}
	}
}

func (e *Engine) switchBufferLocked(id string) error {
	if _, ok := e.docs[id]; ok {
		e.activeID = id
		return nil
	}
	clean := filepath.Clean(id)
	for _, doc := range e.docs {
		if filepath.Clean(doc.FilePath) == clean || doc.ID == id {
			e.activeID = doc.ID
			return nil
		}
	}
	return fmt.Errorf("document not found: %s", id)
}

func (e *Engine) resizeLocked(width, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	e.width = width
	e.height = height
	contentH := height - 1
	if contentH < 1 {
		contentH = 1
	}

	for _, doc := range e.docs {
		doc.Viewport.Width = width
		doc.Viewport.Height = contentH
		doc.Viewport.GutterWidth = ComputeGutterWidth(doc.Buffer.TotalLines())
		doc.Viewport.ScrollToCursor(doc.Buffer)
	}
}

// GetTotalLines returns the total lines in the active document.
func (e *Engine) GetTotalLines() int {
	doc := e.ActiveDocument()
	if doc == nil {
		return 0
	}
	return doc.Buffer.TotalLines()
}

// GetLine returns the raw bytes of line in the active document.
func (e *Engine) GetLine(line int) ([]byte, error) {
	doc := e.ActiveDocument()
	if doc == nil {
		return nil, errors.New("no active document")
	}
	return doc.Buffer.GetLine(line)
}

// InsertText inserts text at (line, col) in active document.
func (e *Engine) InsertText(line, col int, text string) error {
	doc := e.ActiveDocument()
	if doc == nil {
		return errors.New("no active document")
	}
	lineStart, err := doc.Buffer.ByteOffsetForLine(line)
	if err != nil {
		return err
	}
	lineBytes, _ := doc.Buffer.GetLine(line)
	byteOffset := 0
	rCount := 0
	for byteOffset < len(lineBytes) && rCount < col {
		_, sz := utf8.DecodeRune(lineBytes[byteOffset:])
		byteOffset += sz
		rCount++
	}
	pos := doc.Buffer.ByteToPosition(lineStart + byteOffset)
	doc.Buffer.SetSelections([]buffer.Selection{buffer.NewSelection(pos, pos)})
	return e.Dispatch(Command{ID: CmdInsertText, Args: text})
}

// DeleteRange removes text between start and end.
func (e *Engine) DeleteRange(startLine, startCol, endLine, endCol int) error {
	doc := e.ActiveDocument()
	if doc == nil {
		return errors.New("no active document")
	}
	startOffset, err := doc.Buffer.ByteOffsetForLine(startLine)
	if err != nil {
		return err
	}
	startBytes, _ := doc.Buffer.GetLine(startLine)
	sByte := 0
	for sByte < len(startBytes) && sByte < startCol {
		_, sz := utf8.DecodeRune(startBytes[sByte:])
		sByte += sz
	}
	startPos := doc.Buffer.ByteToPosition(startOffset + sByte)

	endOffset, err := doc.Buffer.ByteOffsetForLine(endLine)
	if err != nil {
		return err
	}
	endBytes, _ := doc.Buffer.GetLine(endLine)
	eByte := 0
	for eByte < len(endBytes) && eByte < endCol {
		_, sz := utf8.DecodeRune(endBytes[eByte:])
		eByte += sz
	}
	endPos := doc.Buffer.ByteToPosition(endOffset + eByte)

	doc.Buffer.SetSelections([]buffer.Selection{buffer.NewSelection(startPos, endPos)})
	return e.Dispatch(Command{ID: CmdDeleteForward})
}

// GetCursor returns current line and column of primary cursor.
func (e *Engine) GetCursor() (line, col int) {
	doc := e.ActiveDocument()
	if doc == nil {
		return 0, 0
	}
	pos := doc.Buffer.PrimarySelection().Head
	return pos.Line, pos.Column
}

// SetCursor sets the primary cursor position.
func (e *Engine) SetCursor(line, col int) error {
	doc := e.ActiveDocument()
	if doc == nil {
		return errors.New("no active document")
	}
	lineStart, err := doc.Buffer.ByteOffsetForLine(line)
	if err != nil {
		return err
	}
	lineBytes, _ := doc.Buffer.GetLine(line)
	byteOffset := 0
	rCount := 0
	for byteOffset < len(lineBytes) && rCount < col {
		_, sz := utf8.DecodeRune(lineBytes[byteOffset:])
		byteOffset += sz
		rCount++
	}
	pos := doc.Buffer.ByteToPosition(lineStart + byteOffset)
	doc.Buffer.SetSelections([]buffer.Selection{buffer.NewSelection(pos, pos)})
	doc.Viewport.ScrollToCursor(doc.Buffer)
	return nil
}

// Save saves active document atomically.
func (e *Engine) Save() error {
	doc := e.ActiveDocument()
	if doc == nil {
		return errors.New("no active document")
	}
	path := doc.FilePath
	if path == "" {
		path = "untitled.txt"
	}
	return e.Dispatch(Command{ID: CmdFileSave, Args: path})
}

// Log sets the editor status message.
func (e *Engine) Log(msg string) {
	e.SetStatusMessage(msg)
}
