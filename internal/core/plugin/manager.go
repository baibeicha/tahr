package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"tahr/internal/core/i18n"
)

// PluginRepository defines a remote server or local source for plugins.
type PluginRepository struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`  // HTTP/HTTPS REST endpoint, GitHub JSON, or local directory
	Type      string `json:"type"` // "http", "github", "local"
	Enabled   bool   `json:"enabled"`
	AuthToken string `json:"auth_token,omitempty"`
}

// RemotePluginInfo represents metadata for a plugin available in a repository.
type RemotePluginInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Category    string   `json:"category"` // "lsp", "dap", "theme", "tools", "formatter"
	DownloadURL string   `json:"download_url"`
	RepoID      string   `json:"repo_id"`
	RepoName    string   `json:"repo_name"`
	Tags        []string `json:"tags,omitempty"`
	Installed   bool     `json:"installed"`
	Enabled     bool     `json:"enabled"`
}

// PluginState defines persistent enabled/disabled status for plugins.
type PluginState struct {
	Enabled map[string]bool `json:"enabled"`
}

// LifecycleListener is invoked whenever a plugin is enabled or disabled.
type LifecycleListener func(pluginID string, enabled bool)

// DynamicSDKProvider resolves dynamic SDK versions by identifier (e.g. "go").
type DynamicSDKProvider func(sdkID string) string

// Manager coordinates plugin discovery, installation, and registration.
type Manager struct {
	mu                sync.RWMutex
	pluginsDir        string
	projectPluginsDir string
	isDefaultDir      bool
	reposFile         string
	stateFile         string
	state             PluginState
	repositories      []PluginRepository
	workspaceRoot     string
	installed         map[string]*Manifest
	languages         map[string]*LanguageConfig // ext -> config
	filenameLanguages map[string]*LanguageConfig // filename (lower) -> config
	languageOwners    map[*LanguageConfig]*Manifest
	dynamicSDK        DynamicSDKProvider
	host              *WASMHost
	listeners         []LifecycleListener
}

// NewManager creates a plugin manager rooted at pluginsDir.
func NewManager(pluginsDir string) (*Manager, error) {
	isDefaultDir := false
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".config", "tahr")
	if envDir := os.Getenv("TAHR_CONFIG_DIR"); envDir != "" {
		configDir = envDir
	}
	_ = os.MkdirAll(configDir, 0755)
	reposFile := filepath.Join(configDir, "repositories.json")
	stateFile := filepath.Join(configDir, "plugins_state.json")

	if pluginsDir == "" {
		pluginsDir = filepath.Join(configDir, "plugins")
		isDefaultDir = true
	} else {
		reposFile = filepath.Join(pluginsDir, "repositories.json")
		stateFile = filepath.Join(pluginsDir, "plugins_state.json")
	}
	_ = os.MkdirAll(pluginsDir, 0755)

	host, _ := NewWASMHost()

	m := &Manager{
		pluginsDir:   pluginsDir,
		isDefaultDir: isDefaultDir,
		reposFile:    reposFile,
		stateFile:    stateFile,
		state:        PluginState{Enabled: make(map[string]bool)},
		repositories: make([]PluginRepository, 0),
		installed:         make(map[string]*Manifest),
		languages:         make(map[string]*LanguageConfig),
		filenameLanguages: make(map[string]*LanguageConfig),
		languageOwners:    make(map[*LanguageConfig]*Manifest),
		host:              host,
	}

	_ = m.LoadRepositories()
	_ = m.loadStateLocked()

	if isDefaultDir {
		m.SeedDefaultPlugins()
	}


	_ = m.Discover()
	return m, nil
}

// Close terminates the underlying WASM host.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.host != nil {
		return m.host.Close()
	}
	return nil
}

// SetEditorHost binds the active editor instance to the underlying WASM host.
func (m *Manager) SetEditorHost(ed EditorHost) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.host != nil {
		m.host.SetEditorHost(ed)
	}
}

// Host returns the underlying WASM host.
func (m *Manager) Host() *WASMHost {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.host
}

// LoadState reads plugin enabled/disabled states from disk.
func (m *Manager) LoadState() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadStateLocked()
}

func (m *Manager) loadStateLocked() error {
	if m.state.Enabled == nil {
		m.state.Enabled = make(map[string]bool)
	}
	if m.stateFile == "" {
		return nil
	}
	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		return nil
	}
	var st PluginState
	if err := json.Unmarshal(data, &st); err == nil && st.Enabled != nil {
		m.state = st
	}
	return nil
}

// SaveState persists plugin enabled/disabled states to disk.
func (m *Manager) SaveState() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveStateLocked()
}

func (m *Manager) saveStateLocked() error {
	if m.stateFile == "" {
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(m.stateFile), 0755)
	b, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.stateFile, b, 0644)
}

// SetProjectDir configures the project workspace directory and scans .tahr/plugins.
func (m *Manager) SetProjectDir(projectDir string) {
	if projectDir == "" {
		return
	}
	m.mu.Lock()
	m.projectPluginsDir = filepath.Join(projectDir, ".tahr", "plugins")
	m.mu.Unlock()
	_ = m.Discover()
}

// candidatePluginDirs returns all potential directories where plugins or plugin archives may be located.
func (m *Manager) candidatePluginDirs() []string {
	dirs := []string{m.pluginsDir}
	if m.projectPluginsDir != "" {
		dirs = append(dirs, m.projectPluginsDir)
	}
	if !m.isDefaultDir {
		return dirs
	}
	if home, err := os.UserHomeDir(); err == nil && os.Getenv("TAHR_CONFIG_DIR") == "" {
		altDir := filepath.Join(home, ".tahr", "plugins")
		if altDir != m.pluginsDir && altDir != m.projectPluginsDir {
			dirs = append(dirs, altDir)
		}
	}
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		dirs = append(dirs, filepath.Join(execDir, "plugins"))
		dirs = append(dirs, filepath.Join(execDir, "..", "plugins"))
	}
	if cwd, err := os.Getwd(); err == nil {
		cur := cwd
		for i := 0; i < 5; i++ {
			p := filepath.Join(cur, "plugins")
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				dirs = append(dirs, p)
				break
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			cur = parent
		}
	}

	seen := make(map[string]bool)
	var result []string
	for _, d := range dirs {
		if d == "" {
			continue
		}
		abs, err := filepath.Abs(d)
		if err != nil {
			abs = d
		}
		if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
			if !seen[abs] {
				seen[abs] = true
				result = append(result, abs)
			}
		}
	}
	return result
}

// Discover scans pluginsDir and loads all installed manifests.
func (m *Manager) Discover() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dirsToScan := m.candidatePluginDirs()

	for _, pDir := range dirsToScan {
		entries, err := os.ReadDir(pDir)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() {
				manifestPath := filepath.Join(pDir, e.Name(), "plugin.json")
				manifest, err := LoadManifest(manifestPath)
				if err != nil {
					continue
				}

				if _, exists := m.installed[manifest.ID]; !exists {
					m.installed[manifest.ID] = manifest
					if m.state.Enabled == nil {
						m.state.Enabled = make(map[string]bool)
					}
					enabled, hasState := m.state.Enabled[manifest.ID]
					if !hasState {
						enabled = true
						m.state.Enabled[manifest.ID] = true
						_ = m.saveStateLocked()
					}
					if enabled {
						for i := range manifest.Languages {
							lang := &manifest.Languages[i]
							for _, ext := range lang.Extensions {
								m.languages[ext] = lang
							}
						}
						m.registerLocalizationsLocked(manifest, filepath.Join(pDir, e.Name()))
					}

					// Ensure plugin is also deployed to user pluginsDir for persistence
					if pDir != m.pluginsDir && m.pluginsDir != "" {
						destDir := filepath.Join(m.pluginsDir, manifest.ID)
						if _, err := os.Stat(destDir); os.IsNotExist(err) {
							_ = copyDir(filepath.Join(pDir, e.Name()), destDir)
						}
					}
				}
			} else if strings.HasSuffix(e.Name(), ".tahr") {
				pluginID := strings.TrimSuffix(e.Name(), ".tahr")
				if _, exists := m.installed[pluginID]; !exists {
					targetDir := filepath.Join(m.pluginsDir, pluginID)
					archivePath := filepath.Join(pDir, e.Name())
					if _, err := os.Stat(filepath.Join(targetDir, "plugin.json")); os.IsNotExist(err) {
						manifest, err := UnpackArchive(archivePath, targetDir)
						if err == nil && manifest != nil {
							m.installed[manifest.ID] = manifest
							if m.state.Enabled == nil {
								m.state.Enabled = make(map[string]bool)
							}
							enabled, hasState := m.state.Enabled[manifest.ID]
							if !hasState {
								enabled = true
								m.state.Enabled[manifest.ID] = true
								_ = m.saveStateLocked()
							}
							if enabled {
								for i := range manifest.Languages {
									lang := &manifest.Languages[i]
									for _, ext := range lang.Extensions {
										m.languages[ext] = lang
									}
								}
								m.registerLocalizationsLocked(manifest, targetDir)
							}
						}
					}
				}
			}
		}
	}

	m.rebuildActiveLanguagesLocked()
	return nil
}

// Install extracts a .tahr Zstandard archive into pluginsDir/<id>, or installs a builtin manifest by ID.
func (m *Manager) Install(archivePath string) (*Manifest, error) {
	if mMan := GetBuiltinManifest(archivePath); mMan != nil {
		return m.InstallDeclarative(*mMan)
	}

	tempDir, err := os.MkdirTemp("", "tahr-plugin-stage-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	// Unpack and validate in staging
	manifest, err := UnpackArchive(archivePath, tempDir)
	if err != nil {
		return nil, fmt.Errorf("unpack staged archive: %w", err)
	}

	destDir := filepath.Join(m.pluginsDir, manifest.ID)
	_ = os.RemoveAll(destDir)

	if err := os.Rename(tempDir, destDir); err != nil {
		// Fallback to copy if cross-device rename
		if err := copyDir(tempDir, destDir); err != nil {
			return nil, fmt.Errorf("deploy plugin to %s: %w", destDir, err)
		}
	}

	m.mu.Lock()
	m.installed[manifest.ID] = manifest
	for i := range manifest.Languages {
		lang := &manifest.Languages[i]
		for _, ext := range lang.Extensions {
			m.languages[ext] = lang
		}
	}
	m.registerLocalizationsLocked(manifest, destDir)
	m.mu.Unlock()

	return manifest, nil
}

// Link creates a directory link (or staging copy) into pluginsDir/<id>.
func (m *Manager) Link(sourceDir string) (*Manifest, error) {
	manifestPath := filepath.Join(sourceDir, "plugin.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("source missing valid plugin.json: %w", err)
	}

	destDir := filepath.Join(m.pluginsDir, manifest.ID)
	_ = os.RemoveAll(destDir)

	if err := os.Symlink(sourceDir, destDir); err != nil {
		// If symlinks not permitted (e.g. unprivileged Windows), copy directory
		if err := copyDir(sourceDir, destDir); err != nil {
			return nil, fmt.Errorf("link plugin: %w", err)
		}
	}

	m.mu.Lock()
	m.installed[manifest.ID] = manifest
	for i := range manifest.Languages {
		lang := &manifest.Languages[i]
		for _, ext := range lang.Extensions {
			m.languages[ext] = lang
		}
	}
	m.registerLocalizationsLocked(manifest, destDir)
	m.mu.Unlock()

	return manifest, nil
}

// CanonicalLanguagePlugins maps file extensions and language IDs to their canonical plugin ID.
var CanonicalLanguagePlugins = map[string]string{
	".go":        "tahr-go",
	"go":         "tahr-go",
	".py":        "tahr-python",
	".pyw":       "tahr-python",
	".pyi":       "tahr-python",
	"python":     "tahr-python",
	".rs":        "tahr-rust",
	"rust":       "tahr-rust",
	".ts":        "tahr-ts",
	".tsx":       "tahr-ts",
	".js":        "tahr-ts",
	".jsx":       "tahr-ts",
	".mjs":       "tahr-ts",
	".cjs":       "tahr-ts",
	"typescript": "tahr-ts",
	"javascript": "tahr-ts",
	".c":         "tahr-clangd",
	".h":         "tahr-clangd",
	".cpp":       "tahr-clangd",
	".cc":        "tahr-clangd",
	".cxx":       "tahr-clangd",
	".hpp":       "tahr-clangd",
	".hxx":       "tahr-clangd",
	"c":          "tahr-clangd",
	"cpp":        "tahr-clangd",
	"c_cpp":      "tahr-clangd",
}

// GetLanguageConfig returns the configuration associated with file extension.
func (m *Manager) GetLanguageConfig(ext string) *LanguageConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	normExt := strings.ToLower(ext)
	if primaryID, ok := CanonicalLanguagePlugins[normExt]; ok {
		if m.installed[primaryID] != nil && !m.isEnabledLocked(primaryID) {
			return nil
		}
	}
	return m.languages[normExt]
}

// InstalledPlugins returns a list of all loaded manifests.
func (m *Manager) InstalledPlugins() []*Manifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]*Manifest, 0, len(m.installed))
	for _, p := range m.installed {
		res = append(res, p)
	}
	return res
}

// Installed returns a map of all currently installed manifests by ID.
func (m *Manager) Installed() map[string]*Manifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]*Manifest, len(m.installed))
	for k, v := range m.installed {
		res[k] = v
	}
	return res
}

func (m *Manager) isEnabledLocked(id string) bool {
	if m.state.Enabled != nil {
		if enabled, ok := m.state.Enabled[id]; ok {
			return enabled
		}
	}
	_, ok := m.installed[id]
	return ok
}

// IsEnabled returns whether the specified plugin ID is currently active.
func (m *Manager) IsEnabled(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isEnabledLocked(id)
}

// IsExtensionActive returns true if an enabled plugin provides support for this file extension.
func (m *Manager) IsExtensionActive(ext string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	normExt := strings.ToLower(ext)
	if primaryID, ok := CanonicalLanguagePlugins[normExt]; ok {
		if m.installed[primaryID] != nil && !m.isEnabledLocked(primaryID) {
			return false
		}
	}
	return m.languages[normExt] != nil
}

// AddLifecycleListener registers a callback invoked when a plugin is enabled or disabled.
func (m *Manager) AddLifecycleListener(listener LifecycleListener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, listener)
}

func (m *Manager) notifyLifecycle(pluginID string, enabled bool) {
	m.mu.RLock()
	var list []LifecycleListener
	if len(m.listeners) > 0 {
		list = append([]LifecycleListener(nil), m.listeners...)
	}
	m.mu.RUnlock()
	for _, l := range list {
		l(pluginID, enabled)
	}
}

func (m *Manager) rebuildActiveLanguagesLocked() {
	m.languages = make(map[string]*LanguageConfig)
	m.filenameLanguages = make(map[string]*LanguageConfig)
	m.languageOwners = make(map[*LanguageConfig]*Manifest)
	for _, inst := range m.installed {
		if !m.isEnabledLocked(inst.ID) {
			continue
		}
		for i := range inst.Languages {
			lang := &inst.Languages[i]
			m.languageOwners[lang] = inst
			for _, ext := range lang.Extensions {
				normExt := strings.ToLower(ext)
				if primaryID, ok := CanonicalLanguagePlugins[normExt]; ok {
					if inst.ID != primaryID && m.installed[primaryID] != nil && !m.isEnabledLocked(primaryID) {
						continue
					}
				}
				m.languages[normExt] = lang
			}
			for _, fn := range lang.Filenames {
				normFn := strings.ToLower(fn)
				m.filenameLanguages[normFn] = lang
			}
		}
	}
}

// SetDynamicSDKProvider registers a callback to resolve runtime SDK versions (e.g. for Go).
func (m *Manager) SetDynamicSDKProvider(provider DynamicSDKProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dynamicSDK = provider
}

// GetLanguageConfigForFile returns the language configuration for a given filename or extension.
func (m *Manager) GetLanguageConfigForFile(filename string) *LanguageConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" {
		if primaryID, ok := CanonicalLanguagePlugins[ext]; ok {
			if m.installed[primaryID] != nil && !m.isEnabledLocked(primaryID) {
				return nil
			}
		}
		if lang := m.languages[ext]; lang != nil {
			return lang
		}
	}
	base := strings.ToLower(filepath.Base(filename))
	return m.filenameLanguages[base]
}

// GetToolchainLabel resolves the status bar toolchain display label based on the active plugin manifest.
// It returns the label and true if an enabled language plugin claims this file; otherwise ("", false).
func (m *Manager) GetToolchainLabel(ext, filename, workspaceDir string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normExt := strings.ToLower(ext)
	normFn := strings.ToLower(filename)

	var lang *LanguageConfig
	if normExt != "" {
		if primaryID, ok := CanonicalLanguagePlugins[normExt]; ok {
			if m.installed[primaryID] != nil && !m.isEnabledLocked(primaryID) {
				return "", false
			}
		}
		lang = m.languages[normExt]
	}

	if lang == nil && normFn != "" {
		lang = m.filenameLanguages[normFn]
	}

	if lang == nil {
		return "", false
	}

	owner := m.languageOwners[lang]
	if owner == nil || !m.isEnabledLocked(owner.ID) {
		return "", false
	}

	// Toolchain configuration priority:
	// 1. Language-level Toolchain
	// 2. Manifest-level Toolchain
	var tc *ToolchainConfig
	if lang.Toolchain != nil {
		tc = lang.Toolchain
	} else if owner.Toolchain != nil {
		tc = owner.Toolchain
	}

	label := ""
	if tc != nil && tc.Name != "" {
		label = tc.Name
	}
	if label == "" && lang.Name != "" {
		label = lang.Name
	}
	if label == "" && lang.ID != "" {
		label = strings.ToUpper(lang.ID[:1]) + lang.ID[1:]
	}
	if label == "" {
		label = "Plain Text"
	}

	// Dynamic SDK resolution (e.g. Go version)
	if tc != nil && tc.DynamicSDK != "" && m.dynamicSDK != nil {
		if dyn := m.dynamicSDK(tc.DynamicSDK); dyn != "" && dyn != "Go: Not Found" {
			label = dyn
		}
	}

	// Virtual environment or environment marker checks
	if tc != nil && tc.EnvSuffix != "" {
		hasEnv := false
		if workspaceDir != "" && len(tc.EnvMarkers) > 0 {
			for _, marker := range tc.EnvMarkers {
				p := filepath.Join(workspaceDir, marker)
				if _, err := os.Stat(p); err == nil {
					hasEnv = true
					break
				}
			}
		}
		if !hasEnv && tc.EnvVariable != "" {
			if os.Getenv(tc.EnvVariable) != "" {
				hasEnv = true
			}
		}
		if hasEnv {
			label += tc.EnvSuffix
		}
	}

	return label, true
}

// EnablePlugin loads and activates an installed plugin from disk.
func (m *Manager) EnablePlugin(id string) error {
	m.mu.Lock()
	if m.state.Enabled == nil {
		m.state.Enabled = make(map[string]bool)
	}
	m.state.Enabled[id] = true
	_ = m.saveStateLocked()

	manifest, ok := m.installed[id]
	if !ok {
		manifestPath := filepath.Join(m.pluginsDir, id, "plugin.json")
		var err error
		manifest, err = LoadManifest(manifestPath)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		m.installed[manifest.ID] = manifest
	}
	m.rebuildActiveLanguagesLocked()
	m.registerLocalizationsLocked(manifest, filepath.Join(m.pluginsDir, id))
	m.mu.Unlock()

	m.notifyLifecycle(id, true)
	return nil
}

// DisablePlugin deactivates a plugin without deleting disk files.
func (m *Manager) DisablePlugin(id string) error {
	m.mu.Lock()
	if m.state.Enabled == nil {
		m.state.Enabled = make(map[string]bool)
	}
	m.state.Enabled[id] = false
	_ = m.saveStateLocked()

	manifest, ok := m.installed[id]
	if ok {
		for _, loc := range manifest.Localizations {
			i18n.DeregisterTranslations(loc.Locale)
		}
	}
	m.rebuildActiveLanguagesLocked()
	m.mu.Unlock()

	m.notifyLifecycle(id, false)
	return nil
}

func (m *Manager) registerLocalizationsLocked(manifest *Manifest, pluginDir string) {
	if manifest == nil || len(manifest.Localizations) == 0 {
		return
	}
	for _, loc := range manifest.Localizations {
		if loc.Locale == "" || loc.File == "" {
			continue
		}
		dictPath := filepath.Join(pluginDir, loc.File)
		data, err := os.ReadFile(dictPath)
		if err != nil {
			continue
		}
		var dict map[string]string
		if err := json.Unmarshal(data, &dict); err != nil {
			continue
		}
		name := loc.Name
		if name == "" {
			name = loc.Locale
		}
		i18n.RegisterTranslations(loc.Locale, name, dict)
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// SeedDefaultPlugins ensures standard plugins exist in pluginsDir and purges legacy auto-seeded stubs.
func (m *Manager) SeedDefaultPlugins() {
	// 1. Purge legacy auto-seeded stubs from earlier versions (themes and uninstalled languages)
	legacyStubs := []string{
		"tahr-markdown",
		"tahr-theme-dracula",
		"tahr-theme-nord",
		"tahr-theme-monokai",
		"tahr-theme-tokyo",
		"tahr-theme-catppuccin",
		"tahr-theme-gruvbox",
	}
	for _, id := range legacyStubs {
		pDir := filepath.Join(m.pluginsDir, id)
		_ = os.RemoveAll(pDir)
		if home, err := os.UserHomeDir(); err == nil {
			_ = os.RemoveAll(filepath.Join(home, ".config", "tahr", "plugins", id))
		}
		if m.state.Enabled != nil {
			delete(m.state.Enabled, id)
		}
		delete(m.installed, id)
	}

	// 2. Only seed the canonical reference Go plugin
	builtins := []string{
		"tahr-go",
	}

	for _, id := range builtins {
		mMan := GetBuiltinManifest(id)
		if mMan == nil {
			continue
		}
		pDir := filepath.Join(m.pluginsDir, id)
		manifestFile := filepath.Join(pDir, "plugin.json")
		_ = os.MkdirAll(pDir, 0755)
		bytes, err := json.MarshalIndent(*mMan, "", "  ")
		if err == nil {
			_ = os.WriteFile(manifestFile, bytes, 0644)
		}
		if m.state.Enabled != nil {
			if _, ok := m.state.Enabled[id]; !ok {
				m.state.Enabled[id] = true
			}
		}
	}

	// 3. Ensure standard plugins (tahr-ru, tahr-python, tahr-rust, tahr-ts, tahr-clangd) are seeded and updated if available in any candidate dir
	autoSeedPlugins := []string{"tahr-ru", "tahr-python", "tahr-rust", "tahr-ts", "tahr-clangd"}
	for _, pid := range autoSeedPlugins {
		targetDir := filepath.Join(m.pluginsDir, pid)
		var sourceDir string
		for _, cd := range m.candidatePluginDirs() {
			if cd == m.pluginsDir {
				continue
			}
			candDir := filepath.Join(cd, pid)
			if fi, err := os.Stat(filepath.Join(candDir, "plugin.json")); err == nil && !fi.IsDir() {
				sourceDir = candDir
				break
			}
		}

		targetManifestPath := filepath.Join(targetDir, "plugin.json")
		if _, err := os.Stat(targetManifestPath); os.IsNotExist(err) {
			if sourceDir != "" {
				_ = copyDir(sourceDir, targetDir)
			} else {
				for _, cd := range m.candidatePluginDirs() {
					candArchive := filepath.Join(cd, pid+".tahr")
					if fi, err := os.Stat(candArchive); err == nil && !fi.IsDir() {
						_, _ = UnpackArchive(candArchive, targetDir)
						break
					}
				}
			}
		} else if sourceDir != "" {
			// Update target if source manifest has toolchain configuration but target does not
			srcM, errSrc := LoadManifest(filepath.Join(sourceDir, "plugin.json"))
			dstM, errDst := LoadManifest(targetManifestPath)
			if errSrc == nil && errDst == nil {
				if srcM.Toolchain != nil && dstM.Toolchain == nil {
					_ = copyDir(sourceDir, targetDir)
				}
			}
		}
		if _, err := os.Stat(filepath.Join(targetDir, "plugin.json")); err == nil {
			if m.state.Enabled != nil {
				if _, ok := m.state.Enabled[pid]; !ok {
					m.state.Enabled[pid] = true
				}
			}
		}
	}
	_ = m.saveStateLocked()
}

// SetWorkspaceRoot sets the current active workspace directory for tool and virtualenv discovery.
func (m *Manager) SetWorkspaceRoot(root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspaceRoot = root
}

// WorkspaceRoot returns the current active workspace directory.
func (m *Manager) WorkspaceRoot() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.workspaceRoot
}

// fileWithExtensions returns file paths with appropriate platform executable extensions.
func fileWithExtensions(baseDir, toolName string) []string {
	if toolName == "" || baseDir == "" {
		return nil
	}
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(toolName))
		if ext == ".exe" || ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
			return []string{filepath.Join(baseDir, toolName)}
		}
		return []string{
			filepath.Join(baseDir, toolName+".exe"),
			filepath.Join(baseDir, toolName+".cmd"),
			filepath.Join(baseDir, toolName+".bat"),
			filepath.Join(baseDir, toolName),
		}
	}
	return []string{filepath.Join(baseDir, toolName)}
}

// FindToolPath resolves the absolute or executable path of a tool binary.
// It checks in order: customPath -> direct LookPath -> workspace venvs -> VIRTUAL_ENV ->
// npm global -> Python user/system scripts -> GOPATH/GOROOT -> Cargo -> known SDK dirs -> aliases.
func (m *Manager) FindToolPath(toolName string, customPath string) (string, bool) {
	if customPath != "" {
		if fi, err := os.Stat(customPath); err == nil {
			if !fi.IsDir() {
				return customPath, true
			}
			var dirCandidates []string
			dirCandidates = append(dirCandidates, fileWithExtensions(customPath, toolName)...)
			dirCandidates = append(dirCandidates, fileWithExtensions(filepath.Join(customPath, "bin"), toolName)...)
			dirCandidates = append(dirCandidates, fileWithExtensions(filepath.Join(customPath, "Scripts"), toolName)...)
			for _, dc := range dirCandidates {
				if dfi, err := os.Stat(dc); err == nil && !dfi.IsDir() {
					return dc, true
				}
			}
		}
	}

	if toolName == "" {
		return "", false
	}

	// 1. Direct LookPath
	if p, err := exec.LookPath(toolName); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs, true
		}
		return p, true
	}

	// Windows fallback with standard extensions
	if runtime.GOOS == "windows" {
		for _, ext := range []string{".exe", ".cmd", ".bat"} {
			if !strings.HasSuffix(strings.ToLower(toolName), ext) {
				if p, err := exec.LookPath(toolName + ext); err == nil {
					if abs, err := filepath.Abs(p); err == nil {
						return abs, true
					}
					return p, true
				}
			}
		}
	}

	var candidates []string

	// 2. Workspace & CWD virtual environment detection (.venv, venv, env)
	var searchRoots []string
	if m != nil {
		m.mu.RLock()
		ws := m.workspaceRoot
		m.mu.RUnlock()
		if ws != "" {
			searchRoots = append(searchRoots, ws)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		alreadyAdded := false
		for _, r := range searchRoots {
			if r == cwd {
				alreadyAdded = true
				break
			}
		}
		if !alreadyAdded {
			searchRoots = append(searchRoots, cwd)
		}
	}

	for _, root := range searchRoots {
		for _, vName := range []string{".venv", "venv", "env"} {
			if runtime.GOOS == "windows" {
				candidates = append(candidates, fileWithExtensions(filepath.Join(root, vName, "Scripts"), toolName)...)
			} else {
				candidates = append(candidates, filepath.Join(root, vName, "bin", toolName))
			}
		}
	}

	// 3. VIRTUAL_ENV environment variable
	if venv := os.Getenv("VIRTUAL_ENV"); venv != "" {
		if runtime.GOOS == "windows" {
			candidates = append(candidates, fileWithExtensions(filepath.Join(venv, "Scripts"), toolName)...)
		} else {
			candidates = append(candidates, filepath.Join(venv, "bin", toolName))
		}
	}

	// 4. User home paths (~/go/bin, ~/.cargo/bin, ~/.npm-global/bin)
	home, _ := os.UserHomeDir()
	if home != "" {
		if runtime.GOOS == "windows" {
			candidates = append(candidates, fileWithExtensions(filepath.Join(home, "go", "bin"), toolName)...)
			candidates = append(candidates, fileWithExtensions(filepath.Join(home, ".cargo", "bin"), toolName)...)
		} else {
			candidates = append(candidates,
				filepath.Join(home, "go", "bin", toolName),
				filepath.Join(home, ".cargo", "bin", toolName),
				filepath.Join(home, ".npm-global", "bin", toolName),
				filepath.Join(home, ".local", "bin", toolName),
			)
		}
	}

	// 5. Global Node/npm and Python Scripts on Windows
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			candidates = append(candidates, fileWithExtensions(filepath.Join(appdata, "npm"), toolName)...)
			candidates = append(candidates, fileWithExtensions(filepath.Join(appdata, "Python", "Scripts"), toolName)...)
		}
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			pyBase := filepath.Join(localApp, "Programs", "Python")
			if entries, err := os.ReadDir(pyBase); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						candidates = append(candidates, fileWithExtensions(filepath.Join(pyBase, e.Name(), "Scripts"), toolName)...)
						candidates = append(candidates, fileWithExtensions(filepath.Join(pyBase, e.Name()), toolName)...)
					}
				}
			}
		}
		// Known Python installations on Windows (clean absolute paths)
		for _, pyRoot := range []string{`D:\Python`, `C:\Python313`, `C:\Python312`, `C:\Python311`, `C:\Python310`, `C:\Python`} {
			candidates = append(candidates, fileWithExtensions(filepath.Join(pyRoot, "Scripts"), toolName)...)
			candidates = append(candidates, fileWithExtensions(pyRoot, toolName)...)
		}
	}

	// 6. GOPATH & GOROOT
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		candidates = append(candidates, fileWithExtensions(filepath.Join(gopath, "bin"), toolName)...)
	}
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		candidates = append(candidates, fileWithExtensions(filepath.Join(goroot, "bin"), toolName)...)
	}

	// 7. Known SDK installation directories (use clean absolute Windows paths with backslashes)
	sdkDirs := []string{
		`D:\go\sdk\go1.26.2\bin`,
		`C:\Program Files\Go\bin`,
		`C:\Go\bin`,
		`C:\Program Files\LLVM\bin`,
		`D:\LLVM\bin`,
	}
	for _, sd := range sdkDirs {
		candidates = append(candidates, fileWithExtensions(sd, toolName)...)
	}

	// 8. Search candidate paths
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			if abs, err := filepath.Abs(c); err == nil {
				return abs, true
			}
			return c, true
		}
	}

	// 9. Tool alias fallback
	switch toolName {
	case "pyright-langserver":
		if p, ok := m.findToolPathWithoutAlias("pyright", customPath); ok {
			return p, true
		}
	case "pyright":
		if p, ok := m.findToolPathWithoutAlias("pyright-langserver", customPath); ok {
			return p, true
		}
	case "pylsp":
		if p, ok := m.findToolPathWithoutAlias("python-lsp-server", customPath); ok {
			return p, true
		}
	}

	return "", false
}

func (m *Manager) findToolPathWithoutAlias(toolName string, customPath string) (string, bool) {
	if customPath != "" {
		if fi, err := os.Stat(customPath); err == nil && !fi.IsDir() {
			return customPath, true
		}
	}
	if p, err := exec.LookPath(toolName); err == nil {
		return p, true
	}
	if runtime.GOOS == "windows" {
		for _, ext := range []string{".exe", ".cmd", ".bat"} {
			if p, err := exec.LookPath(toolName + ext); err == nil {
				return p, true
			}
		}
	}
	var candidates []string
	home, _ := os.UserHomeDir()
	if home != "" {
		candidates = append(candidates, fileWithExtensions(filepath.Join(home, "go", "bin"), toolName)...)
	}
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			candidates = append(candidates, fileWithExtensions(filepath.Join(appdata, "npm"), toolName)...)
		}
		for _, pyRoot := range []string{`D:\Python`, `C:\Python312`, `C:\Python311`, `C:\Python310`} {
			candidates = append(candidates, fileWithExtensions(filepath.Join(pyRoot, "Scripts"), toolName)...)
			candidates = append(candidates, fileWithExtensions(pyRoot, toolName)...)
		}
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, true
		}
	}
	return "", false
}

// SmartInstallOption represents a scored package manager command alternative.
type SmartInstallOption struct {
	Command        string
	PackageManager string
	Score          int
}

// ResolveSmartInstallCommand analyzes rawCmd (which may contain '||' fallbacks)
// and selects the best, PowerShell-safe, single command that matches the current
// OS and available package managers on the host machine.
func ResolveSmartInstallCommand(rawCmd, toolName string, mgr *Manager) (bestCmd string, alternatives []string) {
	rawCmd = strings.TrimSpace(rawCmd)
	if rawCmd == "" {
		return "", nil
	}

	parts := strings.Split(rawCmd, "||")
	var candidates []SmartInstallOption

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		pm := fields[0]

		// 1. OS Compatibility Check
		switch pm {
		case "winget", "choco", "scoop":
			if runtime.GOOS != "windows" {
				continue
			}
		case "brew":
			if runtime.GOOS == "windows" {
				continue
			}
		case "apt", "apt-get", "dnf", "yum", "pacman":
			if runtime.GOOS != "linux" {
				continue
			}
		}

		// 2. Package manager availability and adaptation
		score := 0
		finalCmd := p

		switch pm {
		case "pip", "pip3":
			pipPath := ""
			if mgr != nil {
				pipPath, _ = mgr.FindToolPath("pip", "")
			}
			if pipPath == "" {
				pipPath, _ = exec.LookPath("pip")
			}
			if pipPath != "" {
				score = 120 // Highest preference for Python tools
			} else {
				// Check if python/python3 is available to run `python -m pip`
				pyPath := ""
				if mgr != nil {
					pyPath, _ = mgr.FindToolPath("python", "")
					if pyPath == "" {
						pyPath, _ = mgr.FindToolPath("python3", "")
					}
				}
				if pyPath == "" {
					pyPath, _ = exec.LookPath("python")
					if pyPath == "" {
						pyPath, _ = exec.LookPath("python3")
					}
				}
				if pyPath != "" {
					score = 115
					subArgs := strings.Join(fields[1:], " ")
					finalCmd = "python -m pip " + subArgs
				} else {
					score = 10
				}
			}
		case "npm":
			npmPath := ""
			if mgr != nil {
				npmPath, _ = mgr.FindToolPath("npm", "")
			}
			if npmPath == "" {
				npmPath, _ = exec.LookPath("npm")
			}
			if npmPath != "" {
				score = 100
			} else {
				score = 10
			}
		case "go":
			goPath := ""
			if mgr != nil {
				goPath, _ = mgr.FindToolPath("go", "")
			}
			if goPath == "" {
				goPath, _ = exec.LookPath("go")
			}
			if goPath != "" {
				score = 100
			} else {
				score = 10
			}
		case "cargo", "rustup":
			cPath := ""
			if mgr != nil {
				cPath, _ = mgr.FindToolPath(pm, "")
			}
			if cPath == "" {
				cPath, _ = exec.LookPath(pm)
			}
			if cPath != "" {
				score = 100
			} else {
				score = 10
			}
		case "winget":
			if _, err := exec.LookPath("winget"); err == nil {
				score = 100
			} else {
				score = 20
			}
		default:
			if _, err := exec.LookPath(pm); err == nil {
				score = 80
			} else {
				score = 10
			}
		}

		candidates = append(candidates, SmartInstallOption{
			Command:        finalCmd,
			PackageManager: pm,
			Score:          score,
		})
	}

	if len(candidates) == 0 {
		first := strings.TrimSpace(parts[0])
		return first, nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	bestCmd = candidates[0].Command
	for i := 1; i < len(candidates); i++ {
		alternatives = append(alternatives, candidates[i].Command)
	}
	return bestCmd, alternatives
}

// InstallDeclarative writes a declarative plugin manifest to pluginsDir/<id>/plugin.json and registers it.
func (m *Manager) InstallDeclarative(manifest Manifest) (*Manifest, error) {
	if manifest.ID == "" {
		return nil, fmt.Errorf("manifest ID cannot be empty")
	}

	destDir := filepath.Join(m.pluginsDir, manifest.ID)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create plugin dir %s: %w", destDir, err)
	}

	bytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	manifestFile := filepath.Join(destDir, "plugin.json")
	if err := os.WriteFile(manifestFile, bytes, 0644); err != nil {
		return nil, fmt.Errorf("write plugin manifest %s: %w", manifestFile, err)
	}

	m.mu.Lock()
	m.installed[manifest.ID] = &manifest
	if m.state.Enabled == nil {
		m.state.Enabled = make(map[string]bool)
	}
	m.state.Enabled[manifest.ID] = true
	_ = m.saveStateLocked()
	m.rebuildActiveLanguagesLocked()
	m.registerLocalizationsLocked(&manifest, destDir)
	m.mu.Unlock()

	m.notifyLifecycle(manifest.ID, true)
	return &manifest, nil
}

// InstalledThemes returns all themes provided by installed plugins.
func (m *Manager) InstalledThemes() map[string]ThemeConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make(map[string]ThemeConfig)
	for _, p := range m.installed {
		if m.state.Enabled != nil {
			if enabled, ok := m.state.Enabled[p.ID]; ok && !enabled {
				continue
			}
		}
		for _, th := range p.Themes {
			res[th.ID] = th
		}
	}
	return res
}

// GetLSPForExt returns LSP launch parameters configured for the given file extension.
func (m *Manager) GetLSPForExt(ext string) *LSPConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	normExt := strings.ToLower(ext)
	if primaryID, ok := CanonicalLanguagePlugins[normExt]; ok {
		if m.installed[primaryID] != nil && !m.isEnabledLocked(primaryID) {
			return nil
		}
	}
	for _, p := range m.installed {
		if !m.isEnabledLocked(p.ID) {
			continue
		}
		for _, l := range p.Languages {
			for _, e := range l.Extensions {
				if strings.ToLower(e) == normExt && p.LSP != nil {
					return p.LSP
				}
			}
		}
	}
	return nil
}

// GetDAPForExt returns DAP launch parameters configured for the given file extension.
func (m *Manager) GetDAPForExt(ext string) *DAPConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	normExt := strings.ToLower(ext)
	if primaryID, ok := CanonicalLanguagePlugins[normExt]; ok {
		if m.installed[primaryID] != nil && !m.isEnabledLocked(primaryID) {
			return nil
		}
	}
	for _, p := range m.installed {
		if !m.isEnabledLocked(p.ID) {
			continue
		}
		for _, l := range p.Languages {
			for _, e := range l.Extensions {
				if strings.ToLower(e) == normExt && p.DAP != nil {
					return p.DAP
				}
			}
		}
	}
	return nil
}


// DefaultRepositories returns the initial set of marketplace sources.
func DefaultRepositories() []PluginRepository {
	return []PluginRepository{
		{
			ID:      "official-github",
			Name:    "Tahr Official (GitHub)",
			URL:     "https://raw.githubusercontent.com/baibeicha/tahr/main/plugins/registry.json",
			Type:    "github",
			Enabled: true,
		},
	}
}

// LoadRepositories reads configured repositories from disk or sets defaults.
func (m *Manager) LoadRepositories() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	saveDefaults := func() error {
		m.repositories = DefaultRepositories()
		_ = os.MkdirAll(filepath.Dir(m.reposFile), 0755)
		b, err := json.MarshalIndent(m.repositories, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(m.reposFile, b, 0644)
	}

	data, err := os.ReadFile(m.reposFile)
	if err != nil {
		return saveDefaults()
	}

	var repos []PluginRepository
	if err := json.Unmarshal(data, &repos); err != nil || len(repos) == 0 {
		return saveDefaults()
	}

	hasOldFake := false
	for _, r := range repos {
		if strings.Contains(r.URL, "plugins.tahr.dev") ||
			r.ID == "tahr-official" ||
			r.ID == "corporate-server" ||
			r.ID == "local-share" ||
			strings.Contains(r.URL, "tahr-ide") ||
			strings.Contains(r.ID, "tahr-ide") {
			hasOldFake = true
			break
		}
	}

	if hasOldFake {
		return saveDefaults()
	}

	m.repositories = repos
	return nil
}

// SaveRepositories persists repositories to disk.
func (m *Manager) SaveRepositories(repos []PluginRepository) error {
	m.mu.Lock()
	m.repositories = repos
	reposCopy := make([]PluginRepository, len(repos))
	copy(reposCopy, repos)
	reposFile := m.reposFile
	m.mu.Unlock()

	_ = os.MkdirAll(filepath.Dir(reposFile), 0755)
	b, err := json.MarshalIndent(reposCopy, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(reposFile, b, 0644)
}

// GetRepositories returns a copy of configured repositories.
func (m *Manager) GetRepositories() []PluginRepository {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]PluginRepository, len(m.repositories))
	copy(res, m.repositories)
	return res
}

// AddRepository adds a new repository configuration.
func (m *Manager) AddRepository(repo PluginRepository) error {
	if repo.ID == "" {
		repo.ID = fmt.Sprintf("repo-%d", time.Now().UnixNano()%100000)
	}
	if repo.Name == "" {
		repo.Name = repo.URL
	}
	if repo.Type == "" {
		if strings.HasPrefix(repo.URL, "http://") || strings.HasPrefix(repo.URL, "https://") {
			repo.Type = "http"
		} else {
			repo.Type = "local"
		}
	}
	repo.Enabled = true

	m.mu.Lock()
	m.repositories = append(m.repositories, repo)
	m.mu.Unlock()

	return m.SaveRepositories(m.GetRepositories())
}

// UpdateRepository modifies an existing repository configuration.
func (m *Manager) UpdateRepository(repo PluginRepository) error {
	m.mu.Lock()
	found := false
	for i, r := range m.repositories {
		if r.ID == repo.ID {
			m.repositories[i] = repo
			found = true
			break
		}
	}
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("repository %q not found", repo.ID)
	}
	return m.SaveRepositories(m.GetRepositories())
}

// RemoveRepository removes a repository by ID.
func (m *Manager) RemoveRepository(id string) error {
	m.mu.Lock()
	newRepos := make([]PluginRepository, 0, len(m.repositories))
	for _, r := range m.repositories {
		if r.ID != id {
			newRepos = append(newRepos, r)
		}
	}
	m.repositories = newRepos
	m.mu.Unlock()

	return m.SaveRepositories(m.GetRepositories())
}

// ToggleRepository flips the enabled flag of a repository.
func (m *Manager) ToggleRepository(id string) error {
	m.mu.Lock()
	for i := range m.repositories {
		if m.repositories[i].ID == id {
			m.repositories[i].Enabled = !m.repositories[i].Enabled
			break
		}
	}
	m.mu.Unlock()

	return m.SaveRepositories(m.GetRepositories())
}

// TestRepository verifies connectivity to a repository URL.
func (m *Manager) TestRepository(repo PluginRepository) (bool, string, error) {
	if repo.Type == "local" || strings.HasPrefix(repo.URL, "file://") {
		path := strings.TrimPrefix(repo.URL, "file://")
		if _, err := os.Stat(path); err != nil {
			return false, fmt.Sprintf("Local path not accessible: %v", err), err
		}
		return true, "Local directory accessible", nil
	}

	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest("GET", repo.URL, nil)
	if err != nil {
		return false, fmt.Sprintf("Invalid URL: %v", err), err
	}
	if repo.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+repo.AuthToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("Connection failed: %v", err), err
	}
	_ = resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true, fmt.Sprintf("Server responded with HTTP %d OK", resp.StatusCode), nil
	}
	return false, fmt.Sprintf("Server returned status HTTP %d", resp.StatusCode), nil
}

// GetLaunchTemplates returns all launch templates exposed by active installed plugins.
func (m *Manager) GetLaunchTemplates() []LaunchTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []LaunchTemplate
	for _, inst := range m.installed {
		if m.state.Enabled != nil {
			if enabled, ok := m.state.Enabled[inst.ID]; ok && !enabled {
				continue
			}
		}
		res = append(res, inst.LaunchTemplates...)
	}
	return res
}

// GetProjectTemplates returns all project templates exposed by active installed plugins,
// plus a default Blank Project template.
func (m *Manager) GetProjectTemplates() []ProjectTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var res []ProjectTemplate
	for _, inst := range m.installed {
		if m.state.Enabled != nil {
			if enabled, ok := m.state.Enabled[inst.ID]; ok && !enabled {
				continue
			}
		}
		for _, tpl := range inst.ProjectTemplates {
			if tpl.PluginID == "" {
				tpl.PluginID = inst.ID
			}
			res = append(res, tpl)
		}
	}

	// Always append the universal Blank Project template
	res = append(res, ProjectTemplate{
		ID:          "blank",
		Name:        "Blank Project",
		Description: "An empty project directory with a README.md",
		Category:    "General",
		Icon:        "",
		Files: []ProjectTemplateFile{
			{
				Path:    "README.md",
				Content: "# {{.ProjectName}}\n\nNew project workspace.\n",
			},
		},
	})

	// Sort templates stably: "go" always first, followed by others alphabetically by ID, with "blank" at the very end
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].ID == "go" {
			return true
		}
		if res[j].ID == "go" {
			return false
		}
		if res[i].ID == "blank" {
			return false
		}
		if res[j].ID == "blank" {
			return true
		}
		return res[i].ID < res[j].ID
	})

	return res
}

// GetSupportedLaunchTypes returns all execution/profile types supported by installed plugins + "shell".
func (m *Manager) GetSupportedLaunchTypes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	typeSet := make(map[string]bool)
	typeSet["shell"] = true

	for _, inst := range m.installed {
		if m.state.Enabled != nil {
			if enabled, ok := m.state.Enabled[inst.ID]; ok && !enabled {
				continue
			}
		}
		for _, lang := range inst.Languages {
			if lang.ID != "" {
				lID := strings.ToLower(lang.ID)
				if primaryID, ok := CanonicalLanguagePlugins[lID]; ok {
					if inst.ID != primaryID && m.installed[primaryID] != nil && !m.isEnabledLocked(primaryID) {
						continue
					}
				}
				typeSet[lID] = true
			}
		}
		for _, tmpl := range inst.LaunchTemplates {
			if tmpl.Type != "" {
				tID := strings.ToLower(tmpl.Type)
				if primaryID, ok := CanonicalLanguagePlugins[tID]; ok {
					if inst.ID != primaryID && m.installed[primaryID] != nil && !m.isEnabledLocked(primaryID) {
						continue
					}
				}
				typeSet[tID] = true
			}
		}
	}

	var res []string
	for t := range typeSet {
		if t != "shell" {
			res = append(res, t)
		}
	}
	res = append(res, "shell")
	return res
}

// GetBuiltinManifest returns declarative manifest definitions for built-in plugins.
func GetBuiltinManifest(id string) *Manifest {
	switch id {
	case "tahr-go", "go":
		m := ReferenceGoPluginManifest()
		return &m
	case "tahr-theme-dracula":
		m := Manifest{
			ID:          "tahr-theme-dracula",
			Name:        "Dracula Theme",
			Version:     "1.0.0",
			Author:      "Tahr Team",
			Description: "Dracula official color palette",
			Themes: []ThemeConfig{
				{
					ID:   "dracula",
					Name: "Dracula",
					Colors: map[string]string{
						"background":     "#282a36",
						"foreground":     "#f8f8f2",
						"line_number":    "#6272a4",
						"cursor_line_bg": "#44475a",
						"selection_bg":   "#44475a",
						"gutter_bg":      "#21222c",
						"status_bar_bg":  "#191a21",
						"status_bar_fg":  "#f8f8f2",
					},
				},
			},
		}
		return &m
	case "tahr-theme-nord":
		m := Manifest{
			ID:          "tahr-theme-nord",
			Name:        "Nord Theme",
			Version:     "1.0.0",
			Author:      "Tahr Team",
			Description: "Nord official color palette",
			Themes: []ThemeConfig{
				{
					ID:   "nord",
					Name: "Nord",
					Colors: map[string]string{
						"background":     "#2e3440",
						"foreground":     "#d8dee9",
						"line_number":    "#4c566a",
						"cursor_line_bg": "#3b4252",
						"selection_bg":   "#434c5e",
						"gutter_bg":      "#242933",
						"status_bar_bg":  "#242933",
						"status_bar_fg":  "#d8dee9",
					},
				},
			},
		}
		return &m
	case "tahr-theme-monokai":
		m := Manifest{
			ID:          "tahr-theme-monokai",
			Name:        "Monokai Pro Theme",
			Version:     "1.0.0",
			Author:      "Tahr Team",
			Description: "Monokai Pro official color palette",
			Themes: []ThemeConfig{
				{
					ID:   "monokai",
					Name: "Monokai",
					Colors: map[string]string{
						"background":     "#272822",
						"foreground":     "#f8f8f2",
						"line_number":    "#75715e",
						"cursor_line_bg": "#3e3d32",
						"selection_bg":   "#49483e",
						"gutter_bg":      "#1e1f1c",
						"status_bar_bg":  "#1e1f1c",
						"status_bar_fg":  "#f8f8f2",
					},
				},
			},
		}
		return &m
	case "tahr-theme-tokyo":
		m := Manifest{
			ID:          "tahr-theme-tokyo",
			Name:        "Tokyo Night Theme",
			Version:     "1.0.0",
			Author:      "Tahr Team",
			Description: "Tokyo Night official color palette",
			Themes: []ThemeConfig{
				{
					ID:   "tokyo-night",
					Name: "Tokyo Night",
					Colors: map[string]string{
						"background":     "#1a1b26",
						"foreground":     "#c0caf5",
						"line_number":    "#565f89",
						"cursor_line_bg": "#292e42",
						"selection_bg":   "#364a82",
						"gutter_bg":      "#16161e",
						"status_bar_bg":  "#16161e",
						"status_bar_fg":  "#7aa2f7",
					},
				},
			},
		}
		return &m
	case "tahr-theme-catppuccin":
		m := Manifest{
			ID:          "tahr-theme-catppuccin",
			Name:        "Catppuccin Mocha Theme",
			Version:     "1.0.0",
			Author:      "Tahr Team",
			Description: "Catppuccin Mocha official color palette",
			Themes: []ThemeConfig{
				{
					ID:   "catppuccin",
					Name: "Catppuccin Mocha",
					Colors: map[string]string{
						"background":     "#1e1e2e",
						"foreground":     "#cdd6f4",
						"line_number":    "#6c7086",
						"cursor_line_bg": "#313244",
						"selection_bg":   "#45475a",
						"gutter_bg":      "#181825",
						"status_bar_bg":  "#181825",
						"status_bar_fg":  "#cdd6f4",
					},
				},
			},
		}
		return &m
	case "tahr-theme-gruvbox":
		m := Manifest{
			ID:          "tahr-theme-gruvbox",
			Name:        "Gruvbox Dark Theme",
			Version:     "1.0.0",
			Author:      "Tahr Team",
			Description: "Gruvbox Dark official color palette",
			Themes: []ThemeConfig{
				{
					ID:   "gruvbox",
					Name: "Gruvbox Dark",
					Colors: map[string]string{
						"background":     "#282828",
						"foreground":     "#ebdbb2",
						"line_number":    "#7c6f64",
						"cursor_line_bg": "#3c3836",
						"selection_bg":   "#504945",
						"gutter_bg":      "#1d2021",
						"status_bar_bg":  "#3c3836",
						"status_bar_fg":  "#d5c4a1",
					},
				},
			},
		}
		return &m
	}
	return nil
}

// FetchCatalog queries all active repositories and merges the catalog.
func (m *Manager) FetchCatalog(query, category string) ([]RemotePluginInfo, error) {
	repos := m.GetRepositories()
	resultsMap := make(map[string]RemotePluginInfo)

	// Concurrently query remote HTTP/GitHub/local repositories
	client := &http.Client{Timeout: 3 * time.Second}
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, repo := range repos {
		if !repo.Enabled {
			continue
		}
		wg.Add(1)
		go func(r PluginRepository) {
			defer wg.Done()
			var fetched []RemotePluginInfo
			if strings.HasPrefix(r.URL, "http://") || strings.HasPrefix(r.URL, "https://") {
				req, err := http.NewRequest("GET", r.URL, nil)
				if err == nil {
					if r.AuthToken != "" {
						req.Header.Set("Authorization", "Bearer "+r.AuthToken)
					}
					resp, err := client.Do(req)
					if err == nil && resp.StatusCode == 200 {
						body, _ := io.ReadAll(resp.Body)
						_ = resp.Body.Close()
						var items []RemotePluginInfo
						if err := json.Unmarshal(body, &items); err == nil {
							fetched = items
						}
					}
				}
			} else if strings.HasPrefix(r.URL, "file://") || r.Type == "local" || filepath.IsAbs(r.URL) {
				filePath := strings.TrimPrefix(r.URL, "file://")
				if data, err := os.ReadFile(filePath); err == nil {
					var items []RemotePluginInfo
					_ = json.Unmarshal(data, &items)
					fetched = items
				}
			}

			if len(fetched) > 0 {
				mu.Lock()
				for _, item := range fetched {
					item.RepoID = r.ID
					item.RepoName = r.Name
					resultsMap[item.ID] = item
				}
				mu.Unlock()
			}
		}(repo)
	}

	wg.Wait()

	// Local fallback: scan candidate directories for registry.json
	for _, d := range m.candidatePluginDirs() {
		regPath := filepath.Join(d, "registry.json")
		if data, err := os.ReadFile(regPath); err == nil {
			var localItems []RemotePluginInfo
			if err := json.Unmarshal(data, &localItems); err == nil {
				for _, item := range localItems {
					if _, exists := resultsMap[item.ID]; !exists {
						item.RepoID = "official-local"
						item.RepoName = "Tahr Local Registry"
						resultsMap[item.ID] = item
					}
				}
			}
		}
	}

	if _, ok := resultsMap["tahr-go"]; !ok {
		resultsMap["tahr-go"] = RemotePluginInfo{
			ID:          "tahr-go",
			Name:        "Go Language Support",
			Version:     "1.0.0",
			Author:      "Tahr Team",
			Description: "Language Server (gopls), Delve debugger, launch templates and SDK autodetection",
			Category:    "lsp",
			RepoID:      "official",
			RepoName:    "Official Tahr Registry",
			Tags:        []string{"go", "golang", "lsp", "dap"},
		}
	}

	m.mu.RLock()
	installed := m.installed
	m.mu.RUnlock()

	// Filter and convert to slice
	queryLower := strings.ToLower(strings.TrimSpace(query))
	catLower := strings.ToLower(strings.TrimSpace(category))
	var result []RemotePluginInfo

	for _, p := range resultsMap {
		if _, ok := installed[p.ID]; ok {
			p.Installed = true
		}

		// Category filter
		if catLower != "" && catLower != "all" {
			catMatch := strings.EqualFold(p.Category, catLower)
			if !catMatch {
				for _, t := range p.Tags {
					if strings.EqualFold(t, catLower) {
						catMatch = true
						break
					}
				}
			}
			if !catMatch {
				continue
			}
		}

		// Search query filter
		if queryLower != "" {
			match := strings.Contains(strings.ToLower(p.Name), queryLower) ||
				strings.Contains(strings.ToLower(p.ID), queryLower) ||
				strings.Contains(strings.ToLower(p.Description), queryLower) ||
				strings.Contains(strings.ToLower(p.Author), queryLower)
			if !match {
				for _, t := range p.Tags {
					if strings.Contains(strings.ToLower(t), queryLower) {
						match = true
						break
					}
				}
			}
			if !match {
				continue
			}
		}

		result = append(result, p)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

// InstallFromURL downloads a .tahr plugin archive and installs it, or installs a built-in manifest by ID.
func (m *Manager) InstallFromURL(downloadURL string) (*Manifest, error) {
	if downloadURL == "" {
		return nil, fmt.Errorf("empty download URL")
	}

	if strings.HasPrefix(downloadURL, "http://") || strings.HasPrefix(downloadURL, "https://") {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(downloadURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			tmpFile, err := os.CreateTemp("", "tahr-download-*.tahr")
			if err != nil {
				return nil, err
			}
			defer os.Remove(tmpFile.Name())

			if _, err := io.Copy(tmpFile, resp.Body); err != nil {
				_ = tmpFile.Close()
				return nil, err
			}
			_ = tmpFile.Close()

			return m.Install(tmpFile.Name())
		}
		if resp != nil {
			_ = resp.Body.Close()
		}

		// Fallback: check candidate directories for matching .tahr archive or directory
		base := filepath.Base(downloadURL)
		for _, d := range m.candidatePluginDirs() {
			candArchive := filepath.Join(d, base)
			if fi, err := os.Stat(candArchive); err == nil && !fi.IsDir() {
				return m.Install(candArchive)
			}
			candID := strings.TrimSuffix(base, ".tahr")
			candDir := filepath.Join(d, candID)
			if fi, err := os.Stat(filepath.Join(candDir, "plugin.json")); err == nil && !fi.IsDir() {
				destDir := filepath.Join(m.pluginsDir, candID)
				if err := copyDir(candDir, destDir); err == nil {
					manifest, err := LoadManifest(filepath.Join(destDir, "plugin.json"))
					if err == nil {
						m.mu.Lock()
						m.installed[manifest.ID] = manifest
						m.registerLocalizationsLocked(manifest, destDir)
						m.mu.Unlock()
						return manifest, nil
					}
				}
			}
		}

		if err != nil {
			return nil, fmt.Errorf("download plugin: %w", err)
		}
		return nil, fmt.Errorf("download plugin failed: HTTP %d", resp.StatusCode)
	}

	if strings.HasPrefix(downloadURL, "file://") {
		path := strings.TrimPrefix(downloadURL, "file://")
		return m.Install(path)
	}

	// If downloadURL is an ID, check candidate directories
	for _, d := range m.candidatePluginDirs() {
		candArchive := filepath.Join(d, downloadURL+".tahr")
		if fi, err := os.Stat(candArchive); err == nil && !fi.IsDir() {
			return m.Install(candArchive)
		}
		candDir := filepath.Join(d, downloadURL)
		if fi, err := os.Stat(filepath.Join(candDir, "plugin.json")); err == nil && !fi.IsDir() {
			destDir := filepath.Join(m.pluginsDir, downloadURL)
			if err := copyDir(candDir, destDir); err == nil {
				manifest, err := LoadManifest(filepath.Join(destDir, "plugin.json"))
				if err == nil {
					m.mu.Lock()
					m.installed[manifest.ID] = manifest
					m.registerLocalizationsLocked(manifest, destDir)
					m.mu.Unlock()
					return manifest, nil
				}
			}
		}
	}

	return m.Install(downloadURL)
}

// Uninstall deletes an installed plugin by ID.
func (m *Manager) Uninstall(id string) error {
	m.mu.Lock()

	pDir := filepath.Join(m.pluginsDir, id)
	_ = os.RemoveAll(pDir)

	delete(m.installed, id)
	if m.state.Enabled != nil {
		delete(m.state.Enabled, id)
		_ = m.saveStateLocked()
	}
	m.rebuildActiveLanguagesLocked()
	m.mu.Unlock()

	m.notifyLifecycle(id, false)
	return nil
}

// UninstallPlugin removes an installed plugin from disk and unregisters it.
func (m *Manager) UninstallPlugin(id string) error {
	return m.Uninstall(id)
}

