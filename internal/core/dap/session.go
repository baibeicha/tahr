package dap

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
)

// Session manages an active debugging session with breakpoints, stepping, and variable inspection.
type Session struct {
	mu          sync.RWMutex
	client      *Client
	isActive    bool
	isStopped   bool
	currentFile string
	currentLine int
	threadID    int
	breakpoints map[string][]int // file path -> 0-based lines
	stackFrames []StackFrame
	variables   []Variable

	onStopped func(file string, line int, reason string)
}

// NewSession creates an unattached debugging session.
func NewSession() *Session {
	return &Session{
		breakpoints: make(map[string][]int),
		stackFrames: make([]StackFrame, 0),
		variables:   make([]Variable, 0),
	}
}

// SetOnStopped registers a listener when the execution stops at a breakpoint or step.
func (s *Session) SetOnStopped(cb func(file string, line int, reason string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onStopped = cb
}

// IsActive returns whether a debug session is currently running.
func (s *Session) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isActive
}

// IsStopped returns whether execution is paused at a breakpoint.
func (s *Session) IsStopped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isStopped
}

// CurrentPosition returns the file and 0-based line where execution is paused.
func (s *Session) CurrentPosition() (string, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentFile, s.currentLine
}

// Variables returns local variables at current breakpoint.
func (s *Session) Variables() []Variable {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copied := make([]Variable, len(s.variables))
	copy(copied, s.variables)
	return copied
}

// StackFrames returns the call stack at the current breakpoint.
func (s *Session) StackFrames() []StackFrame {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copied := make([]StackFrame, len(s.stackFrames))
	copy(copied, s.stackFrames)
	return copied
}

// SetVariables sets the variables for testing or session mocking.
func (s *Session) SetVariables(vars []Variable) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.variables = vars
}

// SetStackFrames sets the call stack for testing or session mocking.
func (s *Session) SetStackFrames(frames []StackFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stackFrames = frames
}

// Evaluate evaluates an expression in the current stack frame context.
func (s *Session) Evaluate(expr string) (string, error) {
	s.mu.RLock()
	client := s.client
	isStopped := s.isStopped
	frameID := 0
	if len(s.stackFrames) > 0 {
		frameID = s.stackFrames[0].ID
	}
	s.mu.RUnlock()

	if client == nil || !isStopped {
		return "", fmt.Errorf("debugger not stopped")
	}

	resp, err := client.SendRequest("evaluate", map[string]any{
		"expression": expr,
		"frameId":    frameID,
		"context":    "watch",
	})
	if err != nil {
		return "", err
	}

	var body struct {
		Result string `json:"result"`
	}
	_ = json.Unmarshal(resp.Body, &body)
	return body.Result, nil
}

// SetBreakpoints updates breakpoints for a given file.
func (s *Session) SetBreakpoints(file string, lines []int) {
	s.mu.Lock()
	clean := filepath.Clean(file)
	s.breakpoints[clean] = lines
	client := s.client
	s.mu.Unlock()

	if client != nil {
		s.syncBreakpoints(clean, lines)
	}
}

func (s *Session) syncBreakpoints(file string, lines []int) {
	sb := make([]SourceBreakpoint, len(lines))
	for i, l := range lines {
		sb[i] = SourceBreakpoint{Line: l + 1} // DAP lines are 1-based
	}
	_, _ = s.client.SendRequest("setBreakpoints", SetBreakpointsArguments{
		Source:      Source{Path: file},
		Breakpoints: sb,
	})
}

// AttachClient binds an active DAP client to this session.
func (s *Session) AttachClient(c *Client) {
	s.mu.Lock()
	s.client = c
	s.isActive = true
	s.mu.Unlock()

	// Sync all pre-configured breakpoints
	s.mu.RLock()
	for file, lines := range s.breakpoints {
		s.syncBreakpoints(file, lines)
	}
	s.mu.RUnlock()
}

// OnStopped implements dap.Handler.
func (s *Session) OnStopped(reason string, threadID int) {
	s.mu.Lock()
	s.isStopped = true
	s.threadID = threadID
	s.mu.Unlock()

	// Fetch stack trace
	if s.client != nil {
		resp, err := s.client.SendRequest("stackTrace", map[string]any{
			"threadId": threadID,
		})
		if err == nil && resp != nil && len(resp.Body) > 0 {
			var body struct {
				StackFrames []StackFrame `json:"stackFrames"`
			}
			if err := json.Unmarshal(resp.Body, &body); err == nil && len(body.StackFrames) > 0 {
				top := body.StackFrames[0]
				s.mu.Lock()
				s.stackFrames = body.StackFrames
				s.currentFile = top.Source.Path
				s.currentLine = max(0, top.Line-1) // convert to 0-based
				file := s.currentFile
				line := s.currentLine
				cb := s.onStopped
				s.mu.Unlock()

				if cb != nil {
					cb(file, line, reason)
				}
				return
			}
		}
	}

	s.mu.RLock()
	cb := s.onStopped
	file := s.currentFile
	line := s.currentLine
	s.mu.RUnlock()
	if cb != nil {
		cb(file, line, reason)
	}
}

// OnTerminated implements dap.Handler.
func (s *Session) OnTerminated() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isActive = false
	s.isStopped = false
	s.stackFrames = nil
	s.variables = nil
}

// OnOutput implements dap.Handler.
func (s *Session) OnOutput(category, output string) {
	// Handled by output drawer
}

// StepOver advances execution to the next line.
func (s *Session) StepOver() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isStopped || s.client == nil {
		return fmt.Errorf("session not stopped")
	}
	s.isStopped = false
	_, err := s.client.SendRequest("next", map[string]any{
		"threadId": s.threadID,
	})
	return err
}

// StepInto steps into the function call at cursor.
func (s *Session) StepInto() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isStopped || s.client == nil {
		return fmt.Errorf("session not stopped")
	}
	s.isStopped = false
	_, err := s.client.SendRequest("stepIn", map[string]any{
		"threadId": s.threadID,
	})
	return err
}

// StepOut steps out of the current function call.
func (s *Session) StepOut() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isStopped || s.client == nil {
		return fmt.Errorf("session not stopped")
	}
	s.isStopped = false
	_, err := s.client.SendRequest("stepOut", map[string]any{
		"threadId": s.threadID,
	})
	return err
}

// Continue resumes execution until the next breakpoint.
func (s *Session) Continue() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isStopped || s.client == nil {
		return fmt.Errorf("session not stopped")
	}
	s.isStopped = false
	_, err := s.client.SendRequest("continue", map[string]any{
		"threadId": s.threadID,
	})
	return err
}

// Stop terminates the debugging session.
func (s *Session) Stop() error {
	s.mu.Lock()
	client := s.client
	s.client = nil
	s.isActive = false
	s.isStopped = false
	s.mu.Unlock()

	if client != nil {
		return client.Close()
	}
	return nil
}
