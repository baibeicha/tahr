package dap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Message is the base DAP protocol container.
type Message struct {
	Seq  int64  `json:"seq"`
	Type string `json:"type"` // "request", "response", "event"
}

// Request is a client-to-adapter call.
type Request struct {
	Seq       int64  `json:"seq"`
	Type      string `json:"type"` // "request"
	Command   string `json:"command"`
	Arguments any    `json:"arguments,omitempty"`
}

// Response is an adapter-to-client reply.
type Response struct {
	Seq        int64           `json:"seq"`
	Type       string          `json:"type"` // "response"
	RequestSeq int64           `json:"request_seq"`
	Success    bool            `json:"success"`
	Command    string          `json:"command"`
	Message    string          `json:"message,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// Event is an unsolicited adapter notification.
type Event struct {
	Seq   int64           `json:"seq"`
	Type  string          `json:"type"` // "event"
	Event string          `json:"event"`
	Body  json.RawMessage `json:"body,omitempty"`
}

// StoppedEventBody is payload for "stopped" event.
type StoppedEventBody struct {
	Reason            string `json:"reason"` // "breakpoint", "step", "pause"
	Description       string `json:"description,omitempty"`
	ThreadID          int    `json:"threadId"`
	PreserveFocusHint bool   `json:"preserveFocusHint,omitempty"`
	AllThreadsStopped bool   `json:"allThreadsStopped,omitempty"`
}

// SourceBreakpoint represents a breakpoint in a file.
type SourceBreakpoint struct {
	Line   int `json:"line"`
	Column int `json:"column,omitempty"`
}

// SetBreakpointsArguments configures breakpoints for a file.
type SetBreakpointsArguments struct {
	Source      Source             `json:"source"`
	Breakpoints []SourceBreakpoint `json:"breakpoints"`
}

// Source identifies a source code file.
type Source struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path"`
}

// StackFrame represents a call frame in the debugger.
type StackFrame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Source Source `json:"source"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Variable represents an in-scope variable.
type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"type,omitempty"`
}

// Handler receives asynchronous DAP events.
type Handler interface {
	OnStopped(reason string, threadID int)
	OnTerminated()
	OnOutput(category, output string)
}

// Client manages communication with a DAP server (dlv dap / debugpy).
type Client struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	seqCounter int64
	pending    map[int64]chan *Response
	handler    Handler
	closed     bool
}

// StartClient spawns a DAP adapter process.
func StartClient(command string, args []string, handler Handler) (*Client, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("start DAP %s: %w", command, err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		pending: make(map[int64]chan *Response),
		handler: handler,
	}

	go c.listenStdout()
	return c, nil
}

// Close terminates the DAP connection and kills the adapter process.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.stdout != nil {
		_ = c.stdout.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return nil
}

// NextSeq generates a sequential sequence number.
func (c *Client) NextSeq() int64 {
	return atomic.AddInt64(&c.seqCounter, 1)
}

// SendRequest dispatches a DAP request and waits for response.
func (c *Client) SendRequest(command string, args any) (*Response, error) {
	seq := c.NextSeq()
	req := Request{
		Seq:       seq,
		Type:      "request",
		Command:   command,
		Arguments: args,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	respChan := make(chan *Response, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("dap client closed")
	}
	c.pending[seq] = respChan
	c.mu.Unlock()

	if err := c.writePayload(data); err != nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return nil, err
	}

	resp := <-respChan
	return resp, nil
}

func (c *Client) writePayload(body []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("dap client closed")
	}

	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return err
	}
	_, err := c.stdin.Write(body)
	return err
}

func (c *Client) listenStdout() {
	reader := bufio.NewReader(c.stdout)
	for {
		contentLength := -1
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(line), "content-length:") {
				parts := strings.Split(line, ":")
				if len(parts) == 2 {
					if cl, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
						contentLength = cl
					}
				}
			}
		}

		if contentLength <= 0 {
			continue
		}

		payload := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return
		}

		var base Message
		if err := json.Unmarshal(payload, &base); err != nil {
			continue
		}

		switch base.Type {
		case "response":
			var resp Response
			if err := json.Unmarshal(payload, &resp); err == nil {
				c.mu.Lock()
				if ch, ok := c.pending[resp.RequestSeq]; ok {
					delete(c.pending, resp.RequestSeq)
					ch <- &resp
				}
				c.mu.Unlock()
			}

		case "event":
			var ev Event
			if err := json.Unmarshal(payload, &ev); err == nil {
				if c.handler != nil {
					switch ev.Event {
					case "stopped":
						var stopped StoppedEventBody
						_ = json.Unmarshal(ev.Body, &stopped)
						c.handler.OnStopped(stopped.Reason, stopped.ThreadID)
					case "terminated":
						c.handler.OnTerminated()
					}
				}
			}
		}
	}
}
