package sdk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"tahr/internal/core/logging"
)

// GoSDKInfo holds information about the detected Go compiler and toolchain.
type GoSDKInfo struct {
	BinaryPath   string // Full path to go executable
	BinDir       string // Path to bin directory
	GOROOT       string // Root of the Go distribution
	Version      string // Short version, e.g. "go1.26.2"
	FullVersion  string // Full output of "go version"
	InSystemPath bool   // Whether BinDir is already present in PATH
}

// Manager discovers, caches, and activates Go and other SDK environments.
type Manager struct {
	mu           sync.RWMutex
	goSDK        *GoSDKInfo
	customGoPath string
	listeners    []func(*GoSDKInfo)
}

var (
	defaultManager *Manager
	once           sync.Once
)

// GetManager returns the singleton SDK Manager.
func GetManager() *Manager {
	once.Do(func() {
		defaultManager = NewManager("")
	})
	return defaultManager
}

// NewManager initializes a new SDK manager.
func NewManager(customGoPath string) *Manager {
	m := &Manager{
		customGoPath: customGoPath,
	}
	m.Detect()
	return m
}

// SetCustomGoPath overrides the Go binary path and triggers re-detection.
func (m *Manager) SetCustomGoPath(path string) {
	m.mu.Lock()
	m.customGoPath = path
	m.mu.Unlock()
	m.Detect()
}

// AddListener registers a callback when SDK detection completes or changes.
func (m *Manager) AddListener(fn func(*GoSDKInfo)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, fn)
	if m.goSDK != nil {
		fn(m.goSDK)
	}
}

// GoSDK returns the detected Go SDK info, or nil if not found.
func (m *Manager) GoSDK() *GoSDKInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.goSDK
}

// GoVersionShort returns the short version string (e.g. "go1.26.2") or "Go: Not Found".
func (m *Manager) GoVersionShort() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.goSDK != nil && m.goSDK.Version != "" {
		return m.goSDK.Version
	}
	return "Go: Not Found"
}

// Detect scans known locations and environment variables for the Go toolchain.
func (m *Manager) Detect() *GoSDKInfo {
	logging.Info("Starting Go SDK autodetection")
	candidates := m.collectCandidates()

	var detected *GoSDKInfo
	for _, cand := range candidates {
		info, err := inspectGoBinary(cand)
		if err == nil && info != nil {
			detected = info
			logging.Info("Discovered valid Go SDK", "path", info.BinaryPath, "version", info.Version)
			break
		}
	}

	m.mu.Lock()
	m.goSDK = detected
	listeners := append([]func(*GoSDKInfo){}, m.listeners...)
	m.mu.Unlock()

	if detected != nil {
		m.applyEnvironment(detected)
	} else {
		logging.Warn("No valid Go SDK detected in environment or candidate paths")
	}

	for _, l := range listeners {
		l(detected)
	}

	return detected
}

func (m *Manager) collectCandidates() []string {
	var candidates []string

	// 1. Explicitly configured path in settings
	if m.customGoPath != "" {
		candidates = append(candidates, m.customGoPath)
		candidates = append(candidates, filepath.Join(m.customGoPath, "bin", "go.exe"))
		candidates = append(candidates, filepath.Join(m.customGoPath, "go.exe"))
	}

	// 2. PATH lookups
	if path, err := exec.LookPath("go"); err == nil {
		candidates = append(candidates, path)
	}
	if path, err := exec.LookPath("go.exe"); err == nil {
		candidates = append(candidates, path)
	}

	// 3. GOROOT
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		candidates = append(candidates, filepath.Join(goroot, "bin", "go.exe"))
		candidates = append(candidates, filepath.Join(goroot, "bin", "go"))
	}

	// 4. Windows SDK default paths (including D:\go\sdk\go1.26.2)
	if runtime.GOOS == "windows" {
		winCandidates := []string{
			`D:\go\sdk\go1.26.2\bin\go.exe`,
			`C:\Program Files\Go\bin\go.exe`,
			`C:\Go\bin\go.exe`,
			`D:\Go\bin\go.exe`,
		}
		candidates = append(candidates, winCandidates...)

		// Wildcard scan in D:\go\sdk\* and C:\Users\user\sdk\*
		scanSDKDir(`D:\go\sdk`, &candidates)
		scanSDKDir(`C:\go\sdk`, &candidates)
		if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
			scanSDKDir(filepath.Join(userProfile, "sdk"), &candidates)
			scanSDKDir(filepath.Join(userProfile, "go", "bin"), &candidates)
		}
	} else {
		candidates = append(candidates,
			"/usr/local/go/bin/go",
			"/usr/bin/go",
			"/opt/homebrew/bin/go",
			filepath.Join(os.Getenv("HOME"), "sdk", "go", "bin", "go"),
		)
	}

	return deduplicate(candidates)
}

func scanSDKDir(baseDir string, out *[]string) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		binPath := filepath.Join(baseDir, e.Name(), "bin", "go.exe")
		if _, err := os.Stat(binPath); err == nil {
			*out = append(*out, binPath)
		}
	}
}

func deduplicate(list []string) []string {
	seen := make(map[string]bool)
	var res []string
	for _, p := range list {
		clean := filepath.Clean(p)
		if !seen[clean] {
			seen[clean] = true
			res = append(res, clean)
		}
	}
	return res
}

func inspectGoBinary(binPath string) (*GoSDKInfo, error) {
	fi, err := os.Stat(binPath)
	if err != nil || fi.IsDir() {
		return nil, fmt.Errorf("not executable file: %s", binPath)
	}

	cmd := exec.Command(binPath, "version")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed executing '%s version': %w", binPath, err)
	}

	outStr := strings.TrimSpace(string(out))
	fields := strings.Fields(outStr)
	version := "go"
	for _, f := range fields {
		if strings.HasPrefix(f, "go1.") || strings.HasPrefix(f, "go2.") {
			version = f
			break
		}
	}

	binDir := filepath.Dir(binPath)
	goroot := filepath.Dir(binDir)

	inPath := false
	sysPath := os.Getenv("PATH")
	for _, p := range filepath.SplitList(sysPath) {
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(binDir)) {
			inPath = true
			break
		}
	}

	return &GoSDKInfo{
		BinaryPath:   binPath,
		BinDir:       binDir,
		GOROOT:       goroot,
		Version:      version,
		FullVersion:  outStr,
		InSystemPath: inPath,
	}, nil
}

func (m *Manager) applyEnvironment(sdk *GoSDKInfo) {
	// Prepend Go SDK bin to PATH of current process so gopls, runner, conpty inherit it
	currentPath := os.Getenv("PATH")
	if !sdk.InSystemPath {
		currentPath = sdk.BinDir + string(os.PathListSeparator) + currentPath
		_ = os.Setenv("PATH", currentPath)
		logging.Info("Prepended Go SDK to process PATH", "binDir", sdk.BinDir)
	}
	if os.Getenv("GOROOT") == "" && sdk.GOROOT != "" {
		_ = os.Setenv("GOROOT", sdk.GOROOT)
		logging.Info("Set process GOROOT", "goroot", sdk.GOROOT)
	}

	// Also add user go/bin (GOPATH/bin) to PATH for tools like gopls, dlv
	if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
		userGoBin := filepath.Join(userProfile, "go", "bin")
		if fi, err := os.Stat(userGoBin); err == nil && fi.IsDir() {
			_ = os.Setenv("PATH", userGoBin+string(os.PathListSeparator)+os.Getenv("PATH"))
			logging.Info("Prepended user go/bin to process PATH", "binDir", userGoBin)
		}
	}
}
