package jupyter

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ClientConfig holds configuration for the Jupyter engine client.
type ClientConfig struct {
	ServerURL    string        `json:"server_url"`    // e.g. "http://localhost:8888" or "http://127.0.0.1:8888/?token=..."
	Token        string        `json:"token"`         // Jupyter authentication token
	KernelName   string        `json:"kernel_name"`   // e.g. "python3"
	WorkspaceDir string        `json:"workspace_dir"` // Workspace root for virtualenv detection
	Timeout      time.Duration `json:"timeout"`       // Request and execution timeout
}

// DefaultClientConfig provides standard defaults for connecting to Jupyter.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		ServerURL:  "http://localhost:8888",
		KernelName: "python3",
		Timeout:    30 * time.Second,
	}
}

// ServerStatus describes the status payload from GET /api/status.
type ServerStatus struct {
	Started      string `json:"started,omitempty"`
	LastActivity string `json:"last_activity,omitempty"`
	Kernels      int    `json:"kernels,omitempty"`
	Connections  int    `json:"connections,omitempty"`
	Version      string `json:"version,omitempty"`
}

// KernelInfo represents an active Jupyter kernel from /api/kernels.
type KernelInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	LastActivity   string `json:"last_activity,omitempty"`
	ExecutionState string `json:"execution_state,omitempty"`
	Connections    int    `json:"connections,omitempty"`
}

// Client coordinates REST API communication and WebSocket channels with a Jupyter server,
// with automatic fallback to local python execution.
type Client struct {
	config       ClientConfig
	httpClient   *http.Client
	sessionID    string
	activeKernel *KernelInfo
	wsConn       *WSConn
	wsMu         sync.Mutex
	execCount    int
	mu           sync.Mutex
}

// NewClient creates a new Jupyter client instance.
func NewClient(cfg ClientConfig) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.KernelName == "" {
		cfg.KernelName = "python3"
	}

	// Parse token from ServerURL query string if provided
	if cfg.ServerURL != "" {
		if u, err := url.Parse(cfg.ServerURL); err == nil {
			if qToken := u.Query().Get("token"); qToken != "" && cfg.Token == "" {
				cfg.Token = qToken
			}
		}
	}

	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		sessionID: generateClientUUID(),
	}
}

// Config returns a copy of the current client configuration.
func (c *Client) Config() ClientConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config
}

// UpdateConfig updates the client settings.
func (c *Client) UpdateConfig(cfg ClientConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.KernelName == "" {
		cfg.KernelName = "python3"
	}
	if cfg.ServerURL != "" {
		if u, err := url.Parse(cfg.ServerURL); err == nil {
			if qToken := u.Query().Get("token"); qToken != "" && cfg.Token == "" {
				cfg.Token = qToken
			}
		}
	}
	c.config = cfg
	c.httpClient.Timeout = cfg.Timeout
}

// ActiveKernel returns the active kernel info, if any.
func (c *Client) ActiveKernel() *KernelInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.activeKernel
}

// SetActiveKernel sets or clears the current active kernel.
func (c *Client) SetActiveKernel(k *KernelInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeKernel = k
}

// buildURL constructs a full REST URL with auth headers/query.
func (c *Client) buildURL(endpoint string) string {
	base := strings.TrimRight(c.config.ServerURL, "/")
	if u, err := url.Parse(base); err == nil {
		u.RawQuery = "" // Remove query from base for path joining
		base = u.String()
	}
	return base + endpoint
}

// newRequest creates an authenticated HTTP request for the Jupyter REST API.
func (c *Client) newRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	fullURL := c.buildURL(endpoint)
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.config.Token != "" {
		req.Header.Set("Authorization", "token "+c.config.Token)
	}
	return req, nil
}

// GetStatus checks server status via GET /api/status.
func (c *Client) GetStatus(ctx context.Context) (*ServerStatus, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/status", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach jupyter server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status request failed with HTTP %d: %s", resp.StatusCode, string(body))
	}

	var status ServerStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode status response: %w", err)
	}
	return &status, nil
}

// ListKernels retrieves existing active kernels via GET /api/kernels.
func (c *Client) ListKernels(ctx context.Context) ([]KernelInfo, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/kernels", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list kernels: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list kernels failed with HTTP %d: %s", resp.StatusCode, string(body))
	}

	var kernels []KernelInfo
	if err := json.NewDecoder(resp.Body).Decode(&kernels); err != nil {
		return nil, fmt.Errorf("failed to decode kernels response: %w", err)
	}
	return kernels, nil
}

// StartKernel initiates a new kernel on the Jupyter Server via POST /api/kernels.
func (c *Client) StartKernel(ctx context.Context, kernelName string) (*KernelInfo, error) {
	if kernelName == "" {
		kernelName = c.config.KernelName
	}
	payload := map[string]string{"name": kernelName}
	buf, _ := json.Marshal(payload)

	req, err := c.newRequest(ctx, http.MethodPost, "/api/kernels", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to start kernel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("start kernel failed with HTTP %d: %s", resp.StatusCode, string(body))
	}

	var kernel KernelInfo
	if err := json.NewDecoder(resp.Body).Decode(&kernel); err != nil {
		return nil, fmt.Errorf("failed to decode kernel response: %w", err)
	}

	c.mu.Lock()
	c.activeKernel = &kernel
	c.mu.Unlock()

	return &kernel, nil
}

// DeleteKernel terminates a kernel on the Jupyter Server via DELETE /api/kernels/<id>.
func (c *Client) DeleteKernel(ctx context.Context, kernelID string) error {
	if kernelID == "" {
		return errors.New("kernel ID cannot be empty")
	}

	req, err := c.newRequest(ctx, http.MethodDelete, "/api/kernels/"+kernelID, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete kernel: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete kernel failed with HTTP %d: %s", resp.StatusCode, string(body))
	}

	c.mu.Lock()
	if c.activeKernel != nil && c.activeKernel.ID == kernelID {
		c.activeKernel = nil
	}
	c.mu.Unlock()

	return nil
}

// ConnectKernelWS establishes a WebSocket connection to /api/kernels/<id>/channels.
func (c *Client) ConnectKernelWS(ctx context.Context, kernelID string) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()

	if c.wsConn != nil {
		_ = c.wsConn.Close()
		c.wsConn = nil
	}

	base := strings.TrimRight(c.config.ServerURL, "/")
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("invalid server url: %w", err)
	}

	scheme := "ws"
	if u.Scheme == "https" || u.Scheme == "wss" {
		scheme = "wss"
	}

	wsURL := fmt.Sprintf("%s://%s/api/kernels/%s/channels", scheme, u.Host, kernelID)
	if c.config.Token != "" {
		wsURL += "?token=" + url.QueryEscape(c.config.Token)
	}

	headers := make(http.Header)
	if c.config.Token != "" {
		headers.Set("Authorization", "token "+c.config.Token)
	}

	conn, err := DialWS(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect kernel websocket: %w", err)
	}

	c.wsConn = conn
	return nil
}

// ExecuteCell executes a cell either remotely via Jupyter Server or locally via python fallback.
func (c *Client) ExecuteCell(ctx context.Context, cell *Cell) error {
	if cell == nil {
		return errors.New("cell cannot be nil")
	}
	if cell.CellType != CellTypeCode {
		return nil // Only code cells execute
	}

	// 1. Try remote server execution if server is reachable
	if c.config.ServerURL != "" {
		if err := c.ExecuteCellRemote(ctx, cell); err == nil {
			return nil
		}
	}

	// 2. Fallback to local python execution
	return c.ExecuteCellLocal(ctx, cell)
}

// ExecuteCellRemote executes a code cell via Jupyter Server REST API and WebSocket channel.
func (c *Client) ExecuteCellRemote(ctx context.Context, cell *Cell) error {
	if cell == nil || cell.CellType != CellTypeCode {
		return nil
	}

	// Ensure kernel is active
	c.mu.Lock()
	kernel := c.activeKernel
	c.mu.Unlock()

	if kernel == nil {
		k, err := c.StartKernel(ctx, c.config.KernelName)
		if err != nil {
			return err
		}
		kernel = k
	}

	c.wsMu.Lock()
	hasWS := c.wsConn != nil
	c.wsMu.Unlock()

	if !hasWS {
		if err := c.ConnectKernelWS(ctx, kernel.ID); err != nil {
			return err
		}
	}

	msgID := generateClientUUID()
	req := map[string]interface{}{
		"header": map[string]interface{}{
			"msg_id":   msgID,
			"username": "tahr",
			"session":  c.sessionID,
			"msg_type": "execute_request",
			"version":  "5.3",
			"date":     time.Now().UTC().Format(time.RFC3339),
		},
		"parent_header": map[string]interface{}{},
		"metadata":      map[string]interface{}{},
		"content": map[string]interface{}{
			"code":             cell.GetSource(),
			"silent":           false,
			"store_history":    true,
			"user_expressions": map[string]interface{}{},
			"allow_stdin":      false,
			"stop_on_error":    true,
		},
		"channel": "shell",
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to serialize execute request: %w", err)
	}

	c.wsMu.Lock()
	ws := c.wsConn
	c.wsMu.Unlock()
	if ws == nil {
		return errors.New("websocket connection not available")
	}

	if err := ws.WriteTextMessage(reqBytes); err != nil {
		return fmt.Errorf("failed to send execute request: %w", err)
	}

	cell.ClearOutputs()
	timeout := c.config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)

	gotReply := false
	gotIdle := false

	for !gotIdle && time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		rawMsg, err := ws.ReadTextMessage(remaining)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("websocket connection closed by server")
			}
			return fmt.Errorf("error reading execution response: %w", err)
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			continue
		}

		header, _ := msg["header"].(map[string]interface{})
		msgType, _ := header["msg_type"].(string)

		parentHeader, _ := msg["parent_header"].(map[string]interface{})
		parentID, _ := parentHeader["msg_id"].(string)
		if parentID != "" && parentID != msgID {
			// Message belongs to another request
			continue
		}

		content, _ := msg["content"].(map[string]interface{})

		switch msgType {
		case "status":
			state, _ := content["execution_state"].(string)
			if state == "idle" && gotReply {
				gotIdle = true
			}
		case "execute_input":
			if cnt, ok := content["execution_count"].(float64); ok {
				cNum := int(cnt)
				cell.ExecutionCount = &cNum
			}
		case "stream":
			name, _ := content["name"].(string)
			var textLines []string
			switch t := content["text"].(type) {
			case string:
				textLines = splitSourceLines(t)
			case []interface{}:
				for _, line := range t {
					textLines = append(textLines, fmt.Sprint(line))
				}
			}
			cell.AddOutput(Output{
				OutputType: OutputTypeStream,
				Name:       name,
				Text:       textLines,
			})
		case "execute_result":
			var cnt *int
			if cVal, ok := content["execution_count"].(float64); ok {
				iVal := int(cVal)
				cnt = &iVal
				cell.ExecutionCount = cnt
			}
			data, _ := content["data"].(map[string]interface{})
			metadata, _ := content["metadata"].(map[string]interface{})
			cell.AddOutput(Output{
				OutputType:     OutputTypeExecuteResult,
				ExecutionCount: cnt,
				Data:           data,
				Metadata:       metadata,
			})
		case "display_data":
			data, _ := content["data"].(map[string]interface{})
			metadata, _ := content["metadata"].(map[string]interface{})
			cell.AddOutput(Output{
				OutputType: OutputTypeDisplayData,
				Data:       data,
				Metadata:   metadata,
			})
		case "error":
			ename, _ := content["ename"].(string)
			evalue, _ := content["evalue"].(string)
			var tbLines []string
			if tb, ok := content["traceback"].([]interface{}); ok {
				for _, l := range tb {
					tbLines = append(tbLines, fmt.Sprint(l))
				}
			}
			cell.AddOutput(Output{
				OutputType: OutputTypeError,
				EName:      ename,
				EValue:     evalue,
				Traceback:  tbLines,
			})
		case "execute_reply":
			gotReply = true
			if cnt, ok := content["execution_count"].(float64); ok {
				cNum := int(cnt)
				cell.ExecutionCount = &cNum
			}
			// If reply status is received and we haven't seen status idle, loop until status idle
		}
	}

	return nil
}

// ExecuteCellLocal executes cell code locally using python -c with stdout/stderr/result capture.
func (c *Client) ExecuteCellLocal(ctx context.Context, cell *Cell) error {
	if cell == nil || cell.CellType != CellTypeCode {
		return nil
	}

	pyPath, err := c.DetectPython()
	if err != nil {
		return fmt.Errorf("local execution failed: %w", err)
	}

	c.mu.Lock()
	c.execCount++
	currentExecCount := c.execCount
	c.mu.Unlock()

	cell.ExecutionCount = &currentExecCount
	cell.ClearOutputs()

	srcCode := cell.GetSource()
	if strings.TrimSpace(srcCode) == "" {
		return nil
	}

	b64Src := base64.StdEncoding.EncodeToString([]byte(srcCode))

	runnerCode := fmt.Sprintf(`import sys, base64, ast, traceback

raw = base64.b64decode("%s").decode("utf-8")
try:
    tree = ast.parse(raw)
except Exception:
    traceback.print_exc()
    sys.exit(1)

if not tree.body:
    sys.exit(0)

ns = {}
if isinstance(tree.body[-1], ast.Expr):
    last = tree.body.pop()
    try:
        if tree.body:
            exec(compile(tree, '<cell>', 'exec'), ns)
        val = eval(compile(ast.Expression(last.value), '<cell>', 'eval'), ns)
        if val is not None:
            sys.stdout.write("__TAHR_EXEC_RESULT__\n")
            sys.stdout.write(repr(val) + "\n")
    except Exception:
        traceback.print_exc()
        sys.exit(1)
else:
    try:
        exec(compile(tree, '<cell>', 'exec'), ns)
    except Exception:
        traceback.print_exc()
        sys.exit(1)
`, b64Src)

	cmd := exec.CommandContext(ctx, pyPath, "-")
	cmd.Stdin = strings.NewReader(runnerCode)
	if c.config.WorkspaceDir != "" {
		cmd.Dir = c.config.WorkspaceDir
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	rawStdout := strings.ReplaceAll(stdoutBuf.String(), "\r\n", "\n")
	rawStderr := strings.ReplaceAll(stderrBuf.String(), "\r\n", "\n")

	// Parse stdout
	marker := "__TAHR_EXEC_RESULT__\n"
	if idx := strings.Index(rawStdout, marker); idx != -1 {
		streamPart := rawStdout[:idx]
		resultPart := rawStdout[idx+len(marker):]

		if len(streamPart) > 0 {
			cell.AddOutput(Output{
				OutputType: OutputTypeStream,
				Name:       "stdout",
				Text:       splitSourceLines(streamPart),
			})
		}

		if len(resultPart) > 0 {
			cell.AddOutput(Output{
				OutputType:     OutputTypeExecuteResult,
				ExecutionCount: &currentExecCount,
				Text:           splitSourceLines(resultPart),
				Data: map[string]interface{}{
					"text/plain": strings.TrimRight(resultPart, "\r\n"),
				},
			})
		}
	} else if len(rawStdout) > 0 {
		cell.AddOutput(Output{
			OutputType: OutputTypeStream,
			Name:       "stdout",
			Text:       splitSourceLines(rawStdout),
		})
	}

	// Parse stderr or error
	if runErr != nil || len(rawStderr) > 0 {
		if runErr != nil {
			tbLines := splitSourceLines(rawStderr)
			ename, evalue := parsePythonException(rawStderr)
			cell.AddOutput(Output{
				OutputType: OutputTypeError,
				EName:      ename,
				EValue:     evalue,
				Traceback:  tbLines,
			})
		} else {
			// Warnings / stderr without failure
			cell.AddOutput(Output{
				OutputType: OutputTypeStream,
				Name:       "stderr",
				Text:       splitSourceLines(rawStderr),
			})
		}
	}

	return nil
}

// parsePythonException extracts exception name and message from standard traceback.
func parsePythonException(stderr string) (string, string) {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if len(lines) == 0 {
		return "Error", "Execution failed"
	}
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if idx := strings.Index(lastLine, ":"); idx != -1 {
		ename := strings.TrimSpace(lastLine[:idx])
		evalue := strings.TrimSpace(lastLine[idx+1:])
		return ename, evalue
	}
	return "Exception", lastLine
}

// DetectPython finds the appropriate Python binary in workspace virtual environments or system PATH.
func (c *Client) DetectPython() (string, error) {
	return DetectPython(c.config.WorkspaceDir)
}

// DetectPython locates a python interpreter prioritizing workspace virtual environments.
func DetectPython(workspaceDir string) (string, error) {
	if workspaceDir != "" {
		venvCandidates := []string{
			filepath.Join(workspaceDir, ".venv", "Scripts", "python.exe"),
			filepath.Join(workspaceDir, ".venv", "bin", "python"),
			filepath.Join(workspaceDir, ".venv", "bin", "python3"),
			filepath.Join(workspaceDir, "venv", "Scripts", "python.exe"),
			filepath.Join(workspaceDir, "venv", "bin", "python"),
			filepath.Join(workspaceDir, "venv", "bin", "python3"),
			filepath.Join(workspaceDir, "env", "Scripts", "python.exe"),
			filepath.Join(workspaceDir, "env", "bin", "python"),
		}
		for _, cand := range venvCandidates {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand, nil
			}
		}
	}

	var systemCommands []string
	if runtime.GOOS == "windows" {
		systemCommands = []string{"python", "py", "python3"}
	} else {
		systemCommands = []string{"python3", "python"}
	}

	for _, cmdName := range systemCommands {
		if path, err := exec.LookPath(cmdName); err == nil {
			// Verify this candidate is executable and not a dummy redirector
			testCmd := exec.Command(path, "-c", "import sys; sys.exit(0)")
			if err := testCmd.Run(); err == nil {
				return path, nil
			}
		}
	}

	return "", errors.New("python interpreter not found in workspace venv or PATH")
}

// Close releases resources associated with the client.
func (c *Client) Close() error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.wsConn != nil {
		_ = c.wsConn.Close()
		c.wsConn = nil
	}
	return nil
}

// generateClientUUID produces an RFC 4122 v4 UUID string.
func generateClientUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[0], b[1], b[2], b[3],
		b[4], b[5],
		b[6], b[7],
		b[8], b[9],
		b[10], b[11], b[12], b[13], b[14], b[15])
}
