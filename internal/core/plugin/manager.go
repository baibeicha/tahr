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
	installed         map[string]*Manifest
	languages         map[string]*LanguageConfig // ext -> config
	host              *WASMHost
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
		installed:    make(map[string]*Manifest),
		languages:    make(map[string]*LanguageConfig),
		host:         host,
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

// GetLanguageConfig returns the configuration associated with file extension.
func (m *Manager) GetLanguageConfig(ext string) *LanguageConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.languages[ext]
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

// IsEnabled returns whether the specified plugin ID is currently active.
func (m *Manager) IsEnabled(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state.Enabled != nil {
		if enabled, ok := m.state.Enabled[id]; ok {
			return enabled
		}
	}
	_, ok := m.installed[id]
	return ok
}

// EnablePlugin loads and activates an installed plugin from disk.
func (m *Manager) EnablePlugin(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
			return err
		}
		m.installed[manifest.ID] = manifest
	}
	for i := range manifest.Languages {
		lang := &manifest.Languages[i]
		for _, ext := range lang.Extensions {
			m.languages[ext] = lang
		}
	}
	m.registerLocalizationsLocked(manifest, filepath.Join(m.pluginsDir, id))
	return nil
}

// DisablePlugin deactivates a plugin without deleting disk files.
func (m *Manager) DisablePlugin(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Enabled == nil {
		m.state.Enabled = make(map[string]bool)
	}
	m.state.Enabled[id] = false
	_ = m.saveStateLocked()

	manifest, ok := m.installed[id]
	if ok {
		for _, lang := range manifest.Languages {
			for _, ext := range lang.Extensions {
				if cur := m.languages[ext]; cur != nil && cur.ID == lang.ID {
					delete(m.languages, ext)
				}
			}
		}
		for _, loc := range manifest.Localizations {
			i18n.DeregisterTranslations(loc.Locale)
		}
	}
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
		"tahr-python",
		"tahr-rust",
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

	// 3. Ensure tahr-ru is seeded if available in any candidate dir
	ruDir := filepath.Join(m.pluginsDir, "tahr-ru")
	if _, err := os.Stat(filepath.Join(ruDir, "plugin.json")); os.IsNotExist(err) {
		for _, cd := range m.candidatePluginDirs() {
			if cd == m.pluginsDir {
				continue
			}
			candRu := filepath.Join(cd, "tahr-ru")
			if fi, err := os.Stat(filepath.Join(candRu, "plugin.json")); err == nil && !fi.IsDir() {
				_ = copyDir(candRu, ruDir)
				break
			}
			candArchive := filepath.Join(cd, "tahr-ru.tahr")
			if fi, err := os.Stat(candArchive); err == nil && !fi.IsDir() {
				_, _ = UnpackArchive(candArchive, ruDir)
				break
			}
		}
	}
	if _, err := os.Stat(filepath.Join(ruDir, "plugin.json")); err == nil {
		if m.state.Enabled != nil {
			if _, ok := m.state.Enabled["tahr-ru"]; !ok {
				m.state.Enabled["tahr-ru"] = true
			}
		}
	}
	_ = m.saveStateLocked()
}

// FindToolPath resolves the absolute or executable path of a tool binary.
// It checks in order: customPath -> exec.LookPath -> ~/go/bin -> GOPATH/bin -> GOROOT/bin -> known SDK dirs.
func (m *Manager) FindToolPath(toolName string, customPath string) (string, bool) {
	if customPath != "" {
		if fi, err := os.Stat(customPath); err == nil {
			if !fi.IsDir() {
				return customPath, true
			}
			dirCandidates := []string{
				filepath.Join(customPath, toolName),
				filepath.Join(customPath, toolName+".exe"),
				filepath.Join(customPath, "bin", toolName),
				filepath.Join(customPath, "bin", toolName+".exe"),
			}
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

	// Windows fallback with .exe
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(toolName), ".exe") {
		if p, err := exec.LookPath(toolName + ".exe"); err == nil {
			if abs, err := filepath.Abs(p); err == nil {
				return abs, true
			}
			return p, true
		}
	}

	// 2. User home paths (~/go/bin, ~/.cargo/bin)
	home, _ := os.UserHomeDir()
	var candidates []string
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, "go", "bin", toolName),
			filepath.Join(home, "go", "bin", toolName+".exe"),
			filepath.Join(home, ".cargo", "bin", toolName),
			filepath.Join(home, ".cargo", "bin", toolName+".exe"),
		)
	}

	// 3. GOPATH & GOROOT
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		candidates = append(candidates,
			filepath.Join(gopath, "bin", toolName),
			filepath.Join(gopath, "bin", toolName+".exe"),
		)
	}
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		candidates = append(candidates,
			filepath.Join(goroot, "bin", toolName),
			filepath.Join(goroot, "bin", toolName+".exe"),
		)
	}

	// 4. Known SDK installation directories
	candidates = append(candidates,
		filepath.Join("D:", "go", "sdk", "go1.26.2", "bin", toolName),
		filepath.Join("D:", "go", "sdk", "go1.26.2", "bin", toolName+".exe"),
		filepath.Join("C:", "Users", "user", "go", "bin", toolName),
		filepath.Join("C:", "Users", "user", "go", "bin", toolName+".exe"),
		filepath.Join("C:", "Program Files", "Go", "bin", toolName),
		filepath.Join("C:", "Program Files", "Go", "bin", toolName+".exe"),
		filepath.Join("C:", "Go", "bin", toolName),
		filepath.Join("C:", "Go", "bin", toolName+".exe"),
		filepath.Join("D:", "Python", toolName),
		filepath.Join("D:", "Python", toolName+".exe"),
		filepath.Join("D:", "Python", "Scripts", toolName),
		filepath.Join("D:", "Python", "Scripts", toolName+".exe"),
	)

	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, true
		}
	}

	return "", false
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
	for i := range manifest.Languages {
		lang := &manifest.Languages[i]
		for _, ext := range lang.Extensions {
			m.languages[ext] = lang
		}
	}
	m.registerLocalizationsLocked(&manifest, destDir)
	m.mu.Unlock()

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
	for _, p := range m.installed {
		for _, l := range p.Languages {
			for _, e := range l.Extensions {
				if e == ext && p.LSP != nil {
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
	for _, p := range m.installed {
		for _, l := range p.Languages {
			for _, e := range l.Extensions {
				if e == ext && p.DAP != nil {
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
				typeSet[strings.ToLower(lang.ID)] = true
			}
		}
		for _, tmpl := range inst.LaunchTemplates {
			if tmpl.Type != "" {
				typeSet[strings.ToLower(tmpl.Type)] = true
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
			if strings.ToLower(p.Category) != catLower {
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
	defer m.mu.Unlock()

	pDir := filepath.Join(m.pluginsDir, id)
	_ = os.RemoveAll(pDir)

	manifest := m.installed[id]
	if manifest != nil {
		for _, lang := range manifest.Languages {
			for _, ext := range lang.Extensions {
				if cur := m.languages[ext]; cur != nil && cur.ID == lang.ID {
					delete(m.languages, ext)
				}
			}
		}
	}
	delete(m.installed, id)
	if m.state.Enabled != nil {
		delete(m.state.Enabled, id)
		_ = m.saveStateLocked()
	}
	return nil
}

// UninstallPlugin removes an installed plugin from disk and unregisters it.
func (m *Manager) UninstallPlugin(id string) error {
	return m.Uninstall(id)
}

