package ai

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrBinaryNotFound indicates the llama-server executable could not be found.
	ErrBinaryNotFound = errors.New("llama-server binary not found")
	// ErrModelNotFound indicates the model GGUF weights could not be found.
	ErrModelNotFound = errors.New("model weights file not found")
)

// LlamaServerSidecar manages the background lifecycle of local llama-server.
type LlamaServerSidecar struct {
	mu           sync.Mutex
	cfg          Config
	cmd          *exec.Cmd
	managed      bool
	running      bool
	workspaceDir string
	serverPath   string
	modelPath    string
}

// NewLlamaServerSidecar creates a new sidecar daemon manager.
func NewLlamaServerSidecar(cfg Config, workspaceDir string) *LlamaServerSidecar {
	return &LlamaServerSidecar{
		cfg:          cfg,
		workspaceDir: workspaceDir,
	}
}

// SetWorkspaceDir updates the workspace directory used for relative path searches.
func (s *LlamaServerSidecar) SetWorkspaceDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaceDir = dir
}

// UpdateConfig updates the configuration for the sidecar.
func (s *LlamaServerSidecar) UpdateConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

// ResolveServerBinary searches for llama-server in standard directories.
func (s *LlamaServerSidecar) ResolveServerBinary() string {
	binName := "llama-server"
	if runtime.GOOS == "windows" {
		binName = "llama-server.exe"
	}

	candidates := make([]string, 0, 8)
	if s.cfg.LlamaServerPath != "" {
		candidates = append(candidates, s.cfg.LlamaServerPath)
		if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(s.cfg.LlamaServerPath), ".exe") {
			candidates = append(candidates, s.cfg.LlamaServerPath+".exe")
		}
	}

	if s.workspaceDir != "" {
		candidates = append(candidates,
			filepath.Join(s.workspaceDir, "plugins", "ai-completion", "bin", binName),
			filepath.Join(s.workspaceDir, "bin", binName),
		)
	}

	candidates = append(candidates,
		filepath.Join("plugins", "ai-completion", "bin", binName),
		filepath.Join("bin", binName),
	)

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".config", "tahr", "plugins", "ai-completion", "bin", binName),
		)
	}

	if execPath, err := os.Executable(); err == nil && execPath != "" {
		execDir := filepath.Dir(execPath)
		candidates = append(candidates,
			filepath.Join(execDir, "plugins", "ai-completion", "bin", binName),
			filepath.Join(execDir, binName),
		)
	}

	for _, cand := range candidates {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			if abs, err := filepath.Abs(cand); err == nil {
				return abs
			}
			return cand
		}
	}

	if p, err := exec.LookPath(binName); err == nil {
		return p
	}

	return ""
}

// ResolveModelFile searches for model GGUF weights in standard directories.
func (s *LlamaServerSidecar) ResolveModelFile() string {
	candidates := make([]string, 0, 8)
	if s.cfg.ModelPath != "" {
		candidates = append(candidates, s.cfg.ModelPath)
	}

	dirsToCheck := make([]string, 0, 4)
	if s.workspaceDir != "" {
		dirsToCheck = append(dirsToCheck,
			filepath.Join(s.workspaceDir, "plugins", "ai-completion", "models"),
			filepath.Join(s.workspaceDir, "models"),
		)
	}
	dirsToCheck = append(dirsToCheck,
		filepath.Join("plugins", "ai-completion", "models"),
		"models",
	)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirsToCheck = append(dirsToCheck,
			filepath.Join(home, ".config", "tahr", "plugins", "ai-completion", "models"),
		)
	}

	for _, d := range dirsToCheck {
		candidates = append(candidates, filepath.Join(d, "qwen2.5-coder-0.5b.gguf"))
	}

	for _, cand := range candidates {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			if abs, err := filepath.Abs(cand); err == nil {
				return abs
			}
			return cand
		}
	}

	// Scan directories for any .gguf file
	for _, d := range dirsToCheck {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
				p := filepath.Join(d, e.Name())
				if abs, err := filepath.Abs(p); err == nil {
					return abs
				}
				return p
			}
		}
	}

	return ""
}

// IsAvailable checks if both binary and model files exist on the host system.
func (s *LlamaServerSidecar) IsAvailable() bool {
	return s.ResolveServerBinary() != "" && s.ResolveModelFile() != ""
}

// CheckHealth queries the server's health status endpoint.
func (s *LlamaServerSidecar) CheckHealth(port int) bool {
	client := http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err == nil && resp.StatusCode == http.StatusOK {
		_ = resp.Body.Close()
		return true
	}
	return false
}

// Start launches llama-server in the background if not already active.
func (s *LlamaServerSidecar) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	port := s.cfg.Port
	if port <= 0 {
		port = 8989
	}

	// Check if already running or existing daemon listening
	if s.CheckHealth(port) {
		s.running = true
		return nil
	}

	serverBin := s.ResolveServerBinary()
	if serverBin == "" {
		return ErrBinaryNotFound
	}
	s.serverPath = serverBin

	modelFile := s.ResolveModelFile()
	if modelFile == "" {
		return ErrModelNotFound
	}
	s.modelPath = modelFile

	ctxSize := s.cfg.ContextSize
	if ctxSize <= 0 {
		ctxSize = 4096
	}
	threads := s.cfg.Threads
	if threads <= 0 {
		threads = 2
	}

	args := []string{
		"-m", s.modelPath,
		"--port", strconv.Itoa(port),
		"-c", strconv.Itoa(ctxSize),
		"-t", strconv.Itoa(threads),
		"--cont-batching",
		"-ngl", "0",
		"--log-disable",
	}

	cmd := exec.Command(s.serverPath, args...)
	isolateProcess(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start llama-server: %w", err)
	}

	s.cmd = cmd
	s.managed = true

	// Reap child process when it terminates
	go func(c *exec.Cmd) {
		_ = c.Wait()
		s.mu.Lock()
		if s.cmd == c {
			s.running = false
			s.cmd = nil
			s.managed = false
		}
		s.mu.Unlock()
	}(cmd)

	// Poll health endpoint for up to 8 seconds
	for i := 0; i < 40; i++ {
		time.Sleep(200 * time.Millisecond)
		if s.CheckHealth(port) {
			s.running = true
			return nil
		}
		// If command already died
		if s.cmd == nil {
			return errors.New("llama-server terminated unexpectedly")
		}
	}

	// Startup timed out
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		s.cmd = nil
	}
	s.managed = false
	s.running = false
	return errors.New("llama-server startup health check timed out")
}

// Stop cleanly terminates the managed llama-server process.
func (s *LlamaServerSidecar) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.managed && s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		s.cmd = nil
	}
	s.managed = false
	s.running = false
}

// IsRunning reports whether the server is active and responding.
func (s *LlamaServerSidecar) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return false
	}
	port := s.cfg.Port
	if port <= 0 {
		port = 8989
	}
	return s.CheckHealth(port)
}
