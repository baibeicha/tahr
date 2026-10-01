package lsp

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
	"time"
)

// Handler receives asynchronous diagnostics and notifications.
type Handler interface {
	OnDiagnostics(uri string, diagnostics []Diagnostic)
}

// Client manages a language server subprocess and JSON-RPC 2.0 communication.
type Client struct {
	mu         sync.RWMutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	supervisor *ProcessSupervisor

	reqCounter int64
	pending    map[int64]chan *Response
	handler    Handler
	closed     bool
}

// StartClient spawns an LSP server process and connects JSON-RPC 2.0 via stdio.
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

	stderr, err := cmd.StderrPipe()
	if err == nil {
		go func() {
			_, _ = io.Copy(io.Discard, stderr)
		}()
	}

	sup, _ := NewProcessSupervisor()
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		if sup != nil {
			_ = sup.Close()
		}
		return nil, fmt.Errorf("start LSP server %s: %w", command, err)
	}

	if sup != nil {
		_ = sup.Attach(cmd)
	}

	c := &Client{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		supervisor: sup,
		pending:    make(map[int64]chan *Response),
		handler:    handler,
	}

	go c.listenStdout()

	return c, nil
}

// Close shuts down the client and terminates the server process.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.stdout != nil {
		_ = c.stdout.Close()
	}
	if c.supervisor != nil {
		_ = c.supervisor.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return nil
}

// NextID generates a strictly monotonic request ID.
func (c *Client) NextID() int64 {
	return atomic.AddInt64(&c.reqCounter, 1)
}

// SendRequest sends a JSON-RPC 2.0 request and waits for response or cancellation.
func (c *Client) SendRequest(method string, params any) (*Response, error) {
	id := c.NextID()
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	respChan := make(chan *Response, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client closed")
	}
	c.pending[id] = respChan
	c.mu.Unlock()

	if err := c.writePayload(data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp, ok := <-respChan:
		if !ok || resp == nil {
			return nil, fmt.Errorf("lsp client closed")
		}
		return resp, nil
	case <-time.After(10 * time.Second):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("lsp request %s timed out", method)
	}
}

// SendNotification sends a one-way notification without waiting for response.
func (c *Client) SendNotification(method string, params any) error {
	notif := Notification{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	return c.writePayload(data)
}

// CancelRequest notifies the language server to abort processing of reqID.
func (c *Client) CancelRequest(reqID int64) error {
	c.mu.Lock()
	if ch, ok := c.pending[reqID]; ok {
		delete(c.pending, reqID)
		close(ch)
	}
	c.mu.Unlock()

	return c.SendNotification("$/cancelRequest", CancelParams{ID: reqID})
}

// Initialize performs the standard LSP handshake: sends initialize request and initialized notification.
func (c *Client) Initialize(rootURI string) error {
	params := map[string]any{
		"processId": 0,
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization": map[string]any{
					"dynamicRegistration": false,
					"change":              1,
					"willSave":            false,
					"willSaveWaitUntil":   false,
					"didSave":             true,
				},
				"completion": map[string]any{
					"dynamicRegistration": false,
					"completionItem": map[string]any{
						"snippetSupport": true,
						"documentationFormat": []string{"markdown", "plaintext"},
					},
				},
				"hover": map[string]any{
					"dynamicRegistration": false,
					"contentFormat": []string{"markdown", "plaintext"},
				},
				"definition": map[string]any{
					"dynamicRegistration": false,
				},
				"documentSymbol": map[string]any{
					"dynamicRegistration":               false,
					"hierarchicalDocumentSymbolSupport": true,
				},
				"inlayHint": map[string]any{
					"dynamicRegistration": false,
				},
				"semanticTokens": map[string]any{
					"dynamicRegistration": false,
					"requests": map[string]any{
						"full": true,
					},
					"formats": []string{"relative"},
				},
				"publishDiagnostics": map[string]any{
					"relatedInformation": true,
					"versionSupport":    true,
					"tagSupport": map[string]any{
						"valueSet": []int{1, 2},
					},
					"codeDescriptionSupport": true,
					"dataSupport":           true,
				},
				"codeAction": map[string]any{
					"dynamicRegistration": false,
					"codeActionLiteralSupport": map[string]any{
						"codeActionKind": map[string]any{
							"valueSet": []string{
								"quickfix", "refactor", "source.organizeImports",
							},
						},
					},
				},
			},
			"workspace": map[string]any{
				"configuration": true,
			},
		},
		"initializationOptions": map[string]any{
			"diagnosticsDelay": "100ms",
			"staticcheck":      true,
			"analyses": map[string]any{
				"unusedparams": true,
				"nilness":      true,
				"shadow":       true,
			},
		},
	}
	_, err := c.SendRequest("initialize", params)
	if err != nil {
		return err
	}
	if err := c.SendNotification("initialized", map[string]any{}); err != nil {
		return err
	}
	_ = c.SendNotification("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{
			"gopls": map[string]any{
				"diagnosticsDelay": "100ms",
				"staticcheck":      true,
				"analyses": map[string]any{
					"unusedparams": true,
					"nilness":      true,
					"shadow":       true,
				},
			},
		},
	})
	return nil
}

// DidOpen sends textDocument/didOpen.
func (c *Client) DidOpen(uri, languageID, text string, version int) error {
	return c.SendNotification("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    version,
			Text:       text,
		},
	})
}

// DidChange sends an incremental textDocument/didChange event.
func (c *Client) DidChange(uri string, version int, startLine, startCol, endLine, endCol int, text string) error {
	var changes []TextDocumentContentChangeEvent
	if startLine >= 0 {
		changes = []TextDocumentContentChangeEvent{
			{
				Range: &Range{
					Start: Position{Line: startLine, Character: startCol},
					End:   Position{Line: endLine, Character: endCol},
				},
				Text: text,
			},
		}
	} else {
		// Full sync fallback
		changes = []TextDocumentContentChangeEvent{
			{Text: text},
		}
	}

	return c.SendNotification("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{
			URI:     uri,
			Version: version,
		},
		ContentChanges: changes,
	})
}

// writePayload frames data with HTTP-like Content-Length headers.
func (c *Client) writePayload(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.stdin == nil {
		return fmt.Errorf("client closed")
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	if _, err := c.stdin.Write(payload); err != nil {
		return err
	}
	return nil
}

// listenStdout processes the framed incoming JSON-RPC stream.
func (c *Client) listenStdout() {
	defer func() {
		c.mu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}()

	reader := bufio.NewReader(c.stdout)

	for {
		// Read headers until empty line
		contentLength := 0
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
					val, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
					contentLength = val
				}
			}
		}

		if contentLength <= 0 {
			continue
		}

		body := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, body); err != nil {
			return
		}

		c.dispatchIncoming(body)
	}
}

// dispatchIncoming matches incoming messages to pending response channels or handlers.
func (c *Client) dispatchIncoming(data []byte) {
	// 1. Check if it's a publishDiagnostics notification
	var notif Notification
	if err := json.Unmarshal(data, &notif); err == nil && notif.Method == "textDocument/publishDiagnostics" {
		if c.handler != nil {
			var diagParams PublishDiagnosticsParams
			paramBytes, _ := json.Marshal(notif.Params)
			if err := json.Unmarshal(paramBytes, &diagParams); err == nil {
				c.handler.OnDiagnostics(diagParams.URI, diagParams.Diagnostics)
			}
		}
		return
	}

	// 2. Check if it's a server-to-client request (e.g. workspace/configuration)
	var serverReq struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(data, &serverReq); err == nil && serverReq.Method != "" && serverReq.ID != nil {
		if serverReq.Method == "workspace/configuration" {
			var configParams struct {
				Items []struct {
					ScopeURI string `json:"scopeUri"`
					Section  string `json:"section"`
				} `json:"items"`
			}
			_ = json.Unmarshal(serverReq.Params, &configParams)
			resList := make([]any, len(configParams.Items))
			for i, item := range configParams.Items {
				if item.Section == "gopls" || item.Section == "" {
					resList[i] = map[string]any{
						"diagnosticsDelay": "100ms",
						"staticcheck":      true,
						"analyses": map[string]any{
							"unusedparams": true,
							"nilness":      true,
							"shadow":       true,
						},
					}
				} else {
					resList[i] = map[string]any{}
				}
			}
			respBytes, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      serverReq.ID,
				"result":  resList,
			})
			_ = c.writePayload(respBytes)
			return
		}
		// For other server requests, return empty success response
		respBytes, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      serverReq.ID,
			"result":  nil,
		})
		_ = c.writePayload(respBytes)
		return
	}

	// 3. Check if it's a response with an ID
	var resp Response
	if err := json.Unmarshal(data, &resp); err == nil && resp.ID != 0 {
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.mu.Unlock()

		if ok && ch != nil {
			ch <- &resp
			close(ch)
		}
	}
}

// Definition queries textDocument/definition for symbol locations.
func (c *Client) Definition(uri string, line, character int) (*Location, error) {
	resp, err := c.SendRequest("textDocument/definition", DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Result == nil {
		return nil, nil
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	var loc Location
	if err := json.Unmarshal(data, &loc); err == nil && loc.URI != "" {
		return &loc, nil
	}

	var locs []Location
	if err := json.Unmarshal(data, &locs); err == nil && len(locs) > 0 {
		return &locs[0], nil
	}

	return nil, nil
}

// Hover queries textDocument/hover for symbol documentation and signatures.
func (c *Client) Hover(uri string, line, character int) (*Hover, error) {
	resp, err := c.SendRequest("textDocument/hover", HoverParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Result == nil {
		return nil, nil
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	var h Hover
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// InlayHints queries textDocument/inlayHint for parameter and type annotations.
func (c *Client) InlayHints(uri string, startLine, endLine int) ([]InlayHint, error) {
	resp, err := c.SendRequest("textDocument/inlayHint", InlayHintParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range: Range{
			Start: Position{Line: startLine, Character: 0},
			End:   Position{Line: endLine, Character: 9999},
		},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Result == nil {
		return nil, nil
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	var hints []InlayHint
	if err := json.Unmarshal(data, &hints); err != nil {
		return nil, err
	}
	return hints, nil
}

// SemanticTokens queries textDocument/semanticTokens/full and returns decoded tokens.
func (c *Client) SemanticTokens(uri string) ([]DecodedSemanticToken, error) {
	resp, err := c.SendRequest("textDocument/semanticTokens/full", SemanticTokensParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Result == nil {
		return nil, nil
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	var st SemanticTokens
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}

	return DecodeSemanticTokens(st.Data, nil, nil), nil
}

// CodeActions queries textDocument/codeAction for quick fixes and refactorings.
func (c *Client) CodeActions(uri string, startLine, startCol, endLine, endCol int, diags []Diagnostic) ([]CodeAction, error) {
	resp, err := c.SendRequest("textDocument/codeAction", CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range: Range{
			Start: Position{Line: startLine, Character: startCol},
			End:   Position{Line: endLine, Character: endCol},
		},
		Context: CodeActionContext{
			Diagnostics: diags,
		},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Result == nil {
		return nil, nil
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	var actions []CodeAction
	if err := json.Unmarshal(data, &actions); err != nil {
		return nil, err
	}
	return actions, nil
}

// DocumentSymbols queries textDocument/documentSymbol and returns a flattened list of symbols.
func (c *Client) DocumentSymbols(uri string) ([]DocumentSymbol, error) {
	resp, err := c.SendRequest("textDocument/documentSymbol", DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Result == nil {
		return nil, nil
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	var rawItems []json.RawMessage
	if err := json.Unmarshal(data, &rawItems); err != nil || len(rawItems) == 0 {
		return nil, nil
	}

	// 1. Check if elements contain "location" (LSP 2.0 flat SymbolInformation)
	var firstItem map[string]json.RawMessage
	if err := json.Unmarshal(rawItems[0], &firstItem); err == nil {
		if _, hasLocation := firstItem["location"]; hasLocation {
			var symInfos []SymbolInformation
			if err := json.Unmarshal(data, &symInfos); err == nil && len(symInfos) > 0 {
				var converted []DocumentSymbol
				for _, si := range symInfos {
					converted = append(converted, DocumentSymbol{
						Name:           si.Name,
						Kind:           si.Kind,
						Range:          si.Location.Range,
						SelectionRange: si.Location.Range,
					})
				}
				return converted, nil
			}
		}
	}

	// 2. Otherwise parse as hierarchical or flat DocumentSymbol (LSP 3.0)
	var docSymbols []DocumentSymbol
	if err := json.Unmarshal(data, &docSymbols); err == nil && len(docSymbols) > 0 {
		var flattened []DocumentSymbol
		var flatten func(symbols []DocumentSymbol)
		flatten = func(symbols []DocumentSymbol) {
			for _, s := range symbols {
				copyS := s
				copyS.Children = nil
				flattened = append(flattened, copyS)
				if len(s.Children) > 0 {
					flatten(s.Children)
				}
			}
		}
		flatten(docSymbols)
		return flattened, nil
	}

	return nil, nil
}

// Rename queries textDocument/rename and returns a WorkspaceEdit containing changes across files.
func (c *Client) Rename(uri string, line, character int, newName string) (*WorkspaceEdit, error) {
	resp, err := c.SendRequest("textDocument/rename", RenameParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
		NewName:      newName,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Result == nil {
		return nil, nil
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	var we WorkspaceEdit
	if err := json.Unmarshal(data, &we); err != nil {
		return nil, err
	}
	return &we, nil
}
