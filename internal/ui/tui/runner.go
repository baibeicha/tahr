package tui

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tahr/internal/core"
)

// Runner manages asynchronous build and run tasks.
type Runner struct {
	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	lines     []string
	title     string
	exitCode  int
	startTime time.Time
}

// NewRunner creates a new Runner.
func NewRunner() *Runner {
	return &Runner{
		lines: make([]string, 0),
	}
}

// IsRunning reports whether a process is currently executing.
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Lines returns a snapshot copy of the output log lines.
func (r *Runner) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make([]string, len(r.lines))
	copy(copied, r.lines)
	return copied
}

// Title returns the title of the current task.
func (r *Runner) Title() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.title
}

// Stop cancels any running process.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.running = false
}

// DetectRunCommand discovers the execution command for the current project.
func DetectRunCommand(workspaceRoot, activeFilePath string) (string, []string) {
	if workspaceRoot == "" {
		workspaceRoot, _ = os.Getwd()
	}

	// 1. Go project detection
	if _, err := os.Stat(filepath.Join(workspaceRoot, "go.mod")); err == nil {
		// Look for cmd/* packages
		cmdDir := filepath.Join(workspaceRoot, "cmd")
		if entries, err := os.ReadDir(cmdDir); err == nil && len(entries) > 0 {
			for _, entry := range entries {
				if entry.IsDir() {
					return "go", []string{"run", "./cmd/" + entry.Name()}
				}
			}
		}
		if _, err := os.Stat(filepath.Join(workspaceRoot, "main.go")); err == nil {
			return "go", []string{"run", "."}
		}
		if strings.HasSuffix(activeFilePath, ".go") {
			return "go", []string{"run", activeFilePath}
		}
		return "go", []string{"run", "."}
	}

	// 2. Rust project
	if _, err := os.Stat(filepath.Join(workspaceRoot, "Cargo.toml")); err == nil {
		return "cargo", []string{"run"}
	}

	// 3. Node.js project
	if _, err := os.Stat(filepath.Join(workspaceRoot, "package.json")); err == nil {
		return "npm", []string{"start"}
	}

	// 4. Python file
	if strings.HasSuffix(activeFilePath, ".py") {
		return "python", []string{activeFilePath}
	}

	// Fallback to active file or go run
	if strings.HasSuffix(activeFilePath, ".go") {
		return "go", []string{"run", activeFilePath}
	}

	return "go", []string{"run", "."}
}

// DetectBuildCommand discovers the compilation command for the current project.
func DetectBuildCommand(workspaceRoot, activeFilePath string) (string, []string) {
	if workspaceRoot == "" {
		workspaceRoot, _ = os.Getwd()
	}

	// 1. Go project
	if _, err := os.Stat(filepath.Join(workspaceRoot, "go.mod")); err == nil {
		return "go", []string{"build", "-v", "./..."}
	}

	// 2. Rust project
	if _, err := os.Stat(filepath.Join(workspaceRoot, "Cargo.toml")); err == nil {
		return "cargo", []string{"build"}
	}

	// 3. Node project
	if _, err := os.Stat(filepath.Join(workspaceRoot, "package.json")); err == nil {
		return "npm", []string{"run", "build"}
	}

	// 4. Makefile
	if _, err := os.Stat(filepath.Join(workspaceRoot, "Makefile")); err == nil {
		return "make", []string{}
	}

	return "go", []string{"build", "-v", "."}
}

// Run parses a command string and launches the process.
func (r *Runner) Run(command, workspaceRoot string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil
	}
	cmdName := parts[0]
	args := parts[1:]
	return r.StartProcess(workspaceRoot, cmdName, args, command)
}

// StartProcess launches an external command asynchronously and streams lines to the runner log.
func (r *Runner) StartProcess(workspaceRoot, cmdName string, args []string, title string) error {
	return r.StartProcessWithEnv(workspaceRoot, cmdName, args, nil, title)
}

// StartProcessWithEnvAndPreTask launches a pre-launch task (if any) followed by the main command.
func (r *Runner) StartProcessWithEnvAndPreTask(workspaceRoot, cmdName string, args []string, env map[string]string, title, preTask string) error {
	if strings.TrimSpace(preTask) == "" {
		return r.StartProcessWithEnv(workspaceRoot, cmdName, args, env, title)
	}

	r.Stop()

	ctx, cancel := context.WithCancel(context.Background())

	r.mu.Lock()
	r.cancel = cancel
	r.running = true
	r.title = title
	r.lines = []string{
		fmt.Sprintf("─── %s ───", title),
		fmt.Sprintf("[PreLaunchTask] $ %s", preTask),
		fmt.Sprintf("Working dir: %s", workspaceRoot),
		"──────────────────────────────────────────────",
	}
	r.startTime = time.Now()
	r.mu.Unlock()

	go func() {
		parts := strings.Fields(preTask)
		if len(parts) == 0 {
			_ = r.StartProcessWithEnv(workspaceRoot, cmdName, args, env, title)
			return
		}

		preCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
		preCmd.Stdin = bytes.NewReader(nil)
		isolateProcess(preCmd)
		if workspaceRoot != "" {
			preCmd.Dir = workspaceRoot
		}
		if len(env) > 0 {
			preCmd.Env = os.Environ()
			for k, v := range env {
				preCmd.Env = append(preCmd.Env, fmt.Sprintf("%s=%s", k, v))
			}
		}

		outBytes, err := preCmd.CombinedOutput()
		if len(outBytes) > 0 {
			r.mu.Lock()
			outLines := strings.Split(strings.ReplaceAll(string(outBytes), "\r\n", "\n"), "\n")
			for _, ol := range outLines {
				if ol != "" {
					r.lines = append(r.lines, ol)
				}
			}
			r.mu.Unlock()
		}

		if err != nil {
			r.mu.Lock()
			r.running = false
			r.exitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				r.exitCode = exitErr.ExitCode()
			}
			r.lines = append(r.lines, fmt.Sprintf("─── PreLaunchTask failed with code %d. Aborting launch. ───", r.exitCode))
			r.mu.Unlock()
			return
		}

		r.mu.Lock()
		r.lines = append(r.lines, "─── PreLaunchTask passed. Starting main process... ───")
		r.mu.Unlock()

		_ = r.StartProcessWithEnv(workspaceRoot, cmdName, args, env, title)
	}()

	return nil
}

// StartProcessWithEnv launches an external command with custom environment variables.
func (r *Runner) StartProcessWithEnv(workspaceRoot, cmdName string, args []string, env map[string]string, title string) error {
	r.Stop()

	ctx, cancel := context.WithCancel(context.Background())

	r.mu.Lock()
	r.cancel = cancel
	r.running = true
	r.title = title
	r.lines = []string{
		fmt.Sprintf("─── %s ───", title),
		fmt.Sprintf("$ %s %s", cmdName, strings.Join(args, " ")),
		fmt.Sprintf("Working dir: %s", workspaceRoot),
		"──────────────────────────────────────────────",
	}
	r.startTime = time.Now()
	r.mu.Unlock()

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Stdin = bytes.NewReader(nil)
	isolateProcess(cmd)
	if workspaceRoot != "" {
		cmd.Dir = workspaceRoot
	}
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		r.mu.Lock()
		r.running = false
		r.lines = append(r.lines, fmt.Sprintf("Error creating stdout pipe: %v", err))
		r.mu.Unlock()
		return err
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		r.mu.Lock()
		r.running = false
		r.lines = append(r.lines, fmt.Sprintf("Error creating stderr pipe: %v", err))
		r.mu.Unlock()
		return err
	}

	if err := cmd.Start(); err != nil {
		r.mu.Lock()
		r.running = false
		r.lines = append(r.lines, fmt.Sprintf("Failed to start %s: %v", cmdName, err))
		r.mu.Unlock()
		return err
	}

	// Stream combined output
	multiReader := io.MultiReader(stdoutPipe, stderrPipe)
	go func() {
		scanner := bufio.NewScanner(multiReader)
		for scanner.Scan() {
			line := scanner.Text()
			r.mu.Lock()
			r.lines = append(r.lines, line)
			if len(r.lines) > 2000 {
				r.lines = r.lines[len(r.lines)-1500:]
			}
			r.mu.Unlock()
		}

		err := cmd.Wait()
		duration := time.Since(r.startTime).Round(time.Millisecond)

		r.mu.Lock()
		r.running = false
		if err != nil {
			r.exitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				r.exitCode = exitErr.ExitCode()
			}
			r.lines = append(r.lines, fmt.Sprintf("─── Process failed with code %d (%v) ───", r.exitCode, duration))
		} else {
			r.exitCode = 0
			r.lines = append(r.lines, fmt.Sprintf("─── Process completed successfully (%v) ───", duration))
		}
		r.mu.Unlock()
	}()

	return nil
}

// RunActive runs the current project or active document.
func (m *AppModel) RunActive() {
	if m.launchConfig != nil && len(m.launchConfig.Configurations) > 0 {
		active := m.launchConfig.GetActive()
		if active != nil {
			m.runProfile(*active)
			return
		}
	}

	// If no profile exists or is selected, open launch configuration editor
	m.openLaunchConfigModal()
}

// BuildActive compiles the current project.
func (m *AppModel) BuildActive() {
	// Auto-save active document first
	doc := m.eng.ActiveDocument()
	if doc != nil && doc.Buffer.IsModified() {
		target := doc.FilePath
		if target == "" {
			target = "untitled.txt"
		}
		_ = m.eng.Dispatch(core.Command{ID: core.CmdFileSave, Args: target})
	}

	activePath := ""
	if doc != nil {
		activePath = doc.FilePath
	}

	cmdName, args := DetectBuildCommand(m.workspaceDir, activePath)
	if cmdName == "go" && m.sdkManager != nil && m.sdkManager.GoSDK() != nil {
		cmdName = m.sdkManager.GoSDK().BinaryPath
	}
	title := fmt.Sprintf("BUILD: %s %s", filepath.Base(cmdName), strings.Join(args, " "))
	m.outputOpen = true
	m.statusMessage = fmt.Sprintf("Building project (%s %s)...", filepath.Base(cmdName), strings.Join(args, " "))
	_ = m.runner.StartProcess(m.workspaceDir, cmdName, args, title)
}
