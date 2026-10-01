package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Profile represents a single run or debug configuration.
type Profile struct {
	Name           string            `json:"name"`
	Type           string            `json:"type"`           // "go", "python", "rust", "shell"
	Request        string            `json:"request"`        // "launch", "debug"
	Target         string            `json:"target"`         // e.g. "main.go", "./cmd/tahr", "app.py"
	Args           []string          `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Cwd            string            `json:"cwd,omitempty"`
	Console        string            `json:"console,omitempty"` // "integratedTerminal", "internalConsole"
	PreLaunchTask  string            `json:"preLaunchTask,omitempty"`
	PostLaunchTask string            `json:"postLaunchTask,omitempty"`
}

// Config manages the collection of configurations stored in .tahr/launch.json.
type Config struct {
	mu             sync.RWMutex
	Version        string    `json:"version"`
	ActiveProfile  string    `json:"activeProfile,omitempty"`
	Configurations []Profile `json:"configurations"`
	filePath       string
}

// FilePath returns the backing file path for the configuration.
func (c *Config) FilePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.filePath
}

// SetFilePath updates the backing file path for the configuration.
func (c *Config) SetFilePath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.filePath = path
}

// Exists checks whether .tahr/launch.json exists in the project directory.
func Exists(projectDir string) bool {
	if projectDir == "" {
		return false
	}
	launchFile := filepath.Join(projectDir, ".tahr", "launch.json")
	fi, err := os.Stat(launchFile)
	return err == nil && !fi.IsDir()
}

// Load reads .tahr/launch.json from the project directory, or generates defaults if absent.
func Load(projectDir string) (*Config, error) {
	tahrDir := filepath.Join(projectDir, ".tahr")
	launchFile := filepath.Join(tahrDir, "launch.json")

	data, err := os.ReadFile(launchFile)
	if err == nil {
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err == nil && len(cfg.Configurations) > 0 {
			cfg.filePath = launchFile
			if cfg.ActiveProfile == "" {
				cfg.ActiveProfile = cfg.Configurations[0].Name
			}
			return &cfg, nil
		}
	}

	// Auto-detect project type and generate default profiles
	cfg := DefaultProfiles(projectDir)
	cfg.filePath = launchFile
	_ = cfg.Save()
	return cfg, nil
}

// DefaultProfiles creates candidate profiles based on detected project files.
func DefaultProfiles(projectDir string) *Config {
	var profiles []Profile

	// Go project detection
	if _, err := os.Stat(filepath.Join(projectDir, "go.mod")); err == nil {
		profiles = append(profiles, Profile{
			Name:          "Run Go Project",
			Type:          "go",
			Request:       "launch",
			Target:        ".",
			Args:          nil,
			Env:           map[string]string{"CGO_ENABLED": "0"},
			PreLaunchTask: "go vet ./...",
		}, Profile{
			Name:    "Debug Go (Delve)",
			Type:    "go",
			Request: "debug",
			Target:  ".",
		})
	}

	// Python project detection
	if _, err := os.Stat(filepath.Join(projectDir, "requirements.txt")); err == nil || hasPythonFiles(projectDir) {
		profiles = append(profiles, Profile{
			Name:    "Run Python Main",
			Type:    "python",
			Request: "launch",
			Target:  "main.py",
		}, Profile{
			Name:    "Debug Python",
			Type:    "python",
			Request: "debug",
			Target:  "main.py",
		})
	}

	// Rust project detection
	if _, err := os.Stat(filepath.Join(projectDir, "Cargo.toml")); err == nil {
		profiles = append(profiles, Profile{
			Name:          "Cargo Run",
			Type:          "rust",
			Request:       "launch",
			Target:        "src/main.rs",
			PreLaunchTask: "cargo check",
		})
	}

	// Default fallback
	if len(profiles) == 0 {
		profiles = append(profiles, Profile{
			Name:    "Run Active File",
			Type:    "shell",
			Request: "launch",
			Target:  "main.go",
		})
	}

	return &Config{
		Version:        "1.0",
		ActiveProfile:  profiles[0].Name,
		Configurations: profiles,
	}
}

func hasPythonFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".py" {
			return true
		}
	}
	return false
}

// Save writes current configurations to .tahr/launch.json atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
	if c.filePath == "" {
		return nil
	}
	dir := filepath.Dir(c.filePath)
	_ = os.MkdirAll(dir, 0755)

	bytes, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal launch config: %w", err)
	}

	return os.WriteFile(c.filePath, bytes, 0644)
}

// AddProfile appends a new profile and saves configuration.
func (c *Config) AddProfile(p Profile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Configurations = append(c.Configurations, p)
	if c.ActiveProfile == "" {
		c.ActiveProfile = p.Name
	}
	_ = c.saveLocked()
}

// UpdateProfile updates the profile at idx and saves configuration.
func (c *Config) UpdateProfile(idx int, p Profile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx >= 0 && idx < len(c.Configurations) {
		oldName := c.Configurations[idx].Name
		c.Configurations[idx] = p
		if c.ActiveProfile == oldName {
			c.ActiveProfile = p.Name
		}
		_ = c.saveLocked()
	}
}

// DeleteProfile removes the profile at idx and saves configuration.
func (c *Config) DeleteProfile(idx int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx >= 0 && idx < len(c.Configurations) {
		deletedName := c.Configurations[idx].Name
		c.Configurations = append(c.Configurations[:idx], c.Configurations[idx+1:]...)
		if c.ActiveProfile == deletedName {
			if len(c.Configurations) > 0 {
				c.ActiveProfile = c.Configurations[0].Name
			} else {
				c.ActiveProfile = ""
			}
		}
		_ = c.saveLocked()
	}
}

// GetActive returns the currently selected profile.
func (c *Config) GetActive() *Profile {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for i := range c.Configurations {
		if c.Configurations[i].Name == c.ActiveProfile {
			return &c.Configurations[i]
		}
	}
	if len(c.Configurations) > 0 {
		return &c.Configurations[0]
	}
	return nil
}

// SetActive switches the active profile name.
func (c *Config) SetActive(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.Configurations {
		if p.Name == name {
			c.ActiveProfile = name
			return
		}
	}
}

// Profiles returns a slice of all profile names.
func (c *Config) ProfileNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, len(c.Configurations))
	for i, p := range c.Configurations {
		names[i] = p.Name
	}
	return names
}
