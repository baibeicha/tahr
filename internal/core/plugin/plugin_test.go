package plugin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPackAndUnpack(t *testing.T) {
	tempDir := t.TempDir()
	srcDir := filepath.Join(tempDir, "src_plugin")
	destTahr := filepath.Join(tempDir, "my-plugin.tahr")
	unpackDir := filepath.Join(tempDir, "unpacked")

	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	manifestData := `{
		"id": "tahr.test-lang",
		"name": "Test Language Support",
		"version": "1.0.0",
		"author": "Tahr Team",
		"description": "Test plugin for unit testing",
		"capabilities": ["fs:read"],
		"languages": [
			{
				"id": "testlang",
				"extensions": [".tst", ".test"],
				"comment_token": "//"
			}
		],
		"lsp": {
			"server_name": "test-lsp",
			"command": "test-lsp",
			"args": ["--stdio"]
		}
	}`

	if err := os.WriteFile(filepath.Join(srcDir, "plugin.json"), []byte(manifestData), 0644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "readme.txt"), []byte("Hello plugin world!"), 0644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	// 1. Pack
	if err := PackDirectory(srcDir, destTahr); err != nil {
		t.Fatalf("PackDirectory failed: %v", err)
	}

	// 2. Unpack
	manifest, err := UnpackArchive(destTahr, unpackDir)
	if err != nil {
		t.Fatalf("UnpackArchive failed: %v", err)
	}

	if manifest == nil {
		t.Fatalf("expected parsed manifest, got nil")
	}
	if manifest.ID != "tahr.test-lang" {
		t.Errorf("manifest ID mismatch: expected 'tahr.test-lang', got '%s'", manifest.ID)
	}
	if len(manifest.Languages) != 1 || manifest.Languages[0].ID != "testlang" {
		t.Errorf("manifest languages mismatch: %+v", manifest.Languages)
	}
	if manifest.LSP == nil || manifest.LSP.Command != "test-lsp" {
		t.Errorf("manifest LSP mismatch: %+v", manifest.LSP)
	}

	// Check unpacked file content
	readBack, err := os.ReadFile(filepath.Join(unpackDir, "readme.txt"))
	if err != nil {
		t.Fatalf("read unpacked readme.txt: %v", err)
	}
	if string(readBack) != "Hello plugin world!" {
		t.Errorf("file content mismatch: got %q", string(readBack))
	}
}

func TestZipSlipDefense(t *testing.T) {
	tempDir := t.TempDir()
	maliciousTahr := filepath.Join(tempDir, "evil.tahr")
	unpackDir := filepath.Join(tempDir, "unpacked")

	// Create a zip with directory traversal
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	h := &zip.FileHeader{Name: "../evil.txt", Method: zip.Deflate}
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatalf("create header: %v", err)
	}
	if _, err := w.Write([]byte("malicious content")); err != nil {
		t.Fatalf("write content: %v", err)
	}
	zw.Close()

	if err := os.WriteFile(maliciousTahr, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write evil zip: %v", err)
	}

	// Unpack must fail and detect Zip Slip
	_, err = UnpackArchive(maliciousTahr, unpackDir)
	if err == nil {
		t.Fatalf("expected Zip Slip security error, got nil")
	}
}

func TestManager(t *testing.T) {
	tempDir := t.TempDir()
	pluginsDir := filepath.Join(tempDir, "plugins")
	pluginPath := filepath.Join(pluginsDir, "python-support")
	if err := os.MkdirAll(pluginPath, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	manifestData := `{
		"id": "python-support",
		"name": "Python Language Support",
		"version": "0.1.0",
		"languages": [
			{
				"id": "python",
				"extensions": [".py"],
				"comment_token": "#"
			}
		],
		"lsp": {
			"command": "pyright-langserver",
			"args": ["--stdio"]
		}
	}`
	if err := os.WriteFile(filepath.Join(pluginPath, "plugin.json"), []byte(manifestData), 0644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}

	mgr, err := NewManager(pluginsDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	plugins := mgr.InstalledPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}

	// Test finding language config for .py
	cfg := mgr.GetLanguageConfig(".py")
	if cfg == nil {
		t.Fatalf("expected to find language config for .py")
	}
	if cfg.ID != "python" {
		t.Errorf("unexpected language ID: %s", cfg.ID)
	}

	// Test non-existing extension
	cfgNonExist := mgr.GetLanguageConfig(".xyz")
	if cfgNonExist != nil {
		t.Fatalf("expected nil for nonexistent extension")
	}
}

func TestCLIInit(t *testing.T) {
	tempDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	_ = os.Chdir(tempDir)
	pluginName := "my-awesome-plugin"

	if err := InitPlugin(pluginName, "declarative"); err != nil {
		t.Fatalf("InitPlugin failed: %v", err)
	}

	manifestFile := filepath.Join(tempDir, pluginName, "plugin.json")
	m, err := LoadManifest(manifestFile)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	if m.ID != pluginName {
		t.Errorf("manifest ID mismatch: expected '%s', got '%s'", pluginName, m.ID)
	}
}

func TestWasmHostLifecycle(t *testing.T) {
	host, err := NewWASMHost()
	if err != nil {
		t.Fatalf("NewWASMHost failed: %v", err)
	}
	defer host.Close()

	// Test capability checking on Manifest
	m := &Manifest{
		Capabilities: []string{"fs:read"},
	}

	if !m.HasCapability("fs:read") {
		t.Errorf("expected fs:read capability to be allowed")
	}
	if m.HasCapability("process:exec") {
		t.Errorf("expected process:exec capability to be denied")
	}

	// Execute invalid wasm bytes should fail gracefully without crash
	_, err = host.ExecuteGuest([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}, m, "test", nil, 50*time.Millisecond)
	if err == nil {
		t.Logf("empty wasm executed")
	}
}

type mockEditor struct {
	lines   []string
	cursor  [2]int
	logs    []string
	saved   bool
}

func (m *mockEditor) GetTotalLines() int { return len(m.lines) }
func (m *mockEditor) GetLine(line int) ([]byte, error) {
	if line < 0 || line >= len(m.lines) {
		return nil, fmt.Errorf("out of range")
	}
	return []byte(m.lines[line]), nil
}
func (m *mockEditor) InsertText(line, col int, text string) error {
	if line < len(m.lines) {
		m.lines[line] += text
	}
	return nil
}
func (m *mockEditor) DeleteRange(startLine, startCol, endLine, endCol int) error { return nil }
func (m *mockEditor) GetCursor() (line, col int) { return m.cursor[0], m.cursor[1] }
func (m *mockEditor) SetCursor(line, col int) error {
	m.cursor[0] = line
	m.cursor[1] = col
	return nil
}
func (m *mockEditor) Save() error {
	m.saved = true
	return nil
}
func (m *mockEditor) Log(msg string) {
	m.logs = append(m.logs, msg)
}

func TestWasmHost_EditorHostBinding(t *testing.T) {
	host, err := NewWASMHost()
	if err != nil {
		t.Fatalf("NewWASMHost failed: %v", err)
	}
	defer host.Close()

	ed := &mockEditor{
		lines:  []string{"line 1", "line 2"},
		cursor: [2]int{1, 3},
	}
	host.SetEditorHost(ed)

	if ed.GetTotalLines() != 2 {
		t.Errorf("expected 2 lines")
	}
	line0, err := ed.GetLine(0)
	if err != nil || string(line0) != "line 1" {
		t.Errorf("unexpected line 0: %q, %v", line0, err)
	}
	ed.Log("test message")
	if len(ed.logs) != 1 || ed.logs[0] != "test message" {
		t.Errorf("unexpected logs: %v", ed.logs)
	}
}

func TestMultiRepositoryAndCatalog(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// 1. Initial repositories
	repos := mgr.GetRepositories()
	if len(repos) < 1 || repos[0].ID != "official-github" {
		t.Fatalf("expected official-github repository, got %v", repos)
	}

	// 2. Add custom corporate HTTP repository
	corpRepo := PluginRepository{
		ID:      "corp-server",
		Name:    "Internal Corporate Registry",
		URL:     "https://plugins.mycompany.corp/v1",
		Type:    "http",
		Enabled: true,
	}
	if err := mgr.AddRepository(corpRepo); err != nil {
		t.Fatalf("AddRepository failed: %v", err)
	}

	// 3. Verify added
	reposAfter := mgr.GetRepositories()
	found := false
	for _, r := range reposAfter {
		if r.ID == "corp-server" {
			found = true
			if r.Type != "http" {
				t.Errorf("expected type http, got %s", r.Type)
			}
		}
	}
	if !found {
		t.Errorf("expected corp-server to be found in repositories")
	}

	// 4. Toggle repository
	if err := mgr.ToggleRepository("corp-server"); err != nil {
		t.Fatalf("ToggleRepository failed: %v", err)
	}
	for _, r := range mgr.GetRepositories() {
		if r.ID == "corp-server" && r.Enabled {
			t.Errorf("expected corp-server to be disabled after toggle")
		}
	}

	// 5. Remove repository
	if err := mgr.RemoveRepository("corp-server"); err != nil {
		t.Fatalf("RemoveRepository failed: %v", err)
	}
	for _, r := range mgr.GetRepositories() {
		if r.ID == "corp-server" {
			t.Errorf("expected corp-server to be removed")
		}
	}

	// 6. Test catalog search and categories
	catalog, err := mgr.FetchCatalog("go", "lsp")
	if err != nil {
		t.Fatalf("FetchCatalog failed: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatalf("expected at least 1 result for 'go' in 'lsp', got 0")
	}
	if catalog[0].ID != "tahr-go" {
		t.Errorf("expected 'tahr-go', got %s", catalog[0].ID)
	}
}

func TestPluginPersistenceAndLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// 1. Install declarative test plugin
	testPlugin := Manifest{
		ID:          "test-custom-plugin",
		Name:        "Test Custom",
		Version:     "1.0.0",
		Description: "A custom test plugin",
		Languages: []LanguageConfig{
			{
				ID:         "custom",
				Extensions: []string{".custom"},
			},
		},
		Themes: []ThemeConfig{
			{
				ID:   "custom-theme",
				Name: "Custom Theme",
				Colors: map[string]string{
					"bg": "#123456",
				},
			},
		},
	}
	if _, err := mgr.InstallDeclarative(testPlugin); err != nil {
		t.Fatalf("InstallDeclarative failed: %v", err)
	}

	// Verify installed and enabled by default
	if !mgr.IsEnabled("test-custom-plugin") {
		t.Errorf("expected plugin to be enabled by default")
	}
	if mgr.GetLanguageConfig(".custom") == nil {
		t.Errorf("expected .custom language config to be active")
	}
	if len(mgr.InstalledThemes()) == 0 {
		t.Errorf("expected installed themes to include custom-theme")
	}

	// 2. Disable plugin
	if err := mgr.DisablePlugin("test-custom-plugin"); err != nil {
		t.Fatalf("DisablePlugin failed: %v", err)
	}
	if mgr.IsEnabled("test-custom-plugin") {
		t.Errorf("expected plugin to be disabled")
	}
	if mgr.GetLanguageConfig(".custom") != nil {
		t.Errorf("expected .custom language config to be removed when disabled")
	}
	if _, ok := mgr.InstalledThemes()["custom-theme"]; ok {
		t.Errorf("expected custom-theme to be omitted when plugin disabled")
	}

	// 3. Verify state persisted on disk in plugins_state.json
	statePath := filepath.Join(tempDir, "plugins_state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Fatalf("plugins_state.json was not created at %s", statePath)
	}

	// Reload manager from disk to verify persistence across reboots
	mgr2, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager reload failed: %v", err)
	}
	defer mgr2.Close()

	if mgr2.IsEnabled("test-custom-plugin") {
		t.Errorf("expected plugin to remain disabled after manager reload")
	}

	// 4. Re-enable plugin
	if err := mgr2.EnablePlugin("test-custom-plugin"); err != nil {
		t.Fatalf("EnablePlugin failed: %v", err)
	}
	if !mgr2.IsEnabled("test-custom-plugin") {
		t.Errorf("expected plugin to be enabled after EnablePlugin")
	}
	if mgr2.GetLanguageConfig(".custom") == nil {
		t.Errorf("expected .custom to be restored after EnablePlugin")
	}

	// 5. Uninstall plugin
	if err := mgr2.UninstallPlugin("test-custom-plugin"); err != nil {
		t.Fatalf("UninstallPlugin failed: %v", err)
	}
	pluginDir := filepath.Join(tempDir, "test-custom-plugin")
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Errorf("expected plugin directory %s to be removed from disk", pluginDir)
	}
	if mgr2.IsEnabled("test-custom-plugin") {
		t.Errorf("expected uninstalled plugin to be disabled/removed")
	}
}

func TestGetSupportedLaunchTypes(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// Default without plugins: only shell
	types := mgr.GetSupportedLaunchTypes()
	if len(types) == 0 {
		t.Fatalf("expected at least 'shell' type")
	}
	foundShell := false
	for _, tp := range types {
		if tp == "shell" {
			foundShell = true
		}
	}
	if !foundShell {
		t.Errorf("expected 'shell' in supported launch types")
	}

	// Install a plugin with languages and launch templates
	manifest := Manifest{
		ID:      "test-zig-plugin",
		Name:    "Zig Language Support",
		Version: "1.0.0",
		Languages: []LanguageConfig{
			{ID: "zig", Extensions: []string{".zig"}},
		},
		LaunchTemplates: []LaunchTemplate{
			{Name: "Zig Test", Type: "zig"},
		},
	}
	if _, err := mgr.InstallDeclarative(manifest); err != nil {
		t.Fatalf("failed to install test manifest: %v", err)
	}

	typesWithPlugin := mgr.GetSupportedLaunchTypes()
	foundZig := false
	for _, tp := range typesWithPlugin {
		if tp == "zig" {
			foundZig = true
		}
	}
	if !foundZig {
		t.Errorf("expected 'zig' in supported launch types after plugin install, got: %+v", typesWithPlugin)
	}
}

func TestTier1PluginsPackagingAndInstallation(t *testing.T) {
	// Find repo root plugins directory
	rootPluginsDir := filepath.Join("..", "..", "..", "plugins")
	registryPath := filepath.Join(rootPluginsDir, "registry.json")

	data, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("failed to read registry.json: %v", err)
	}

	var registry []RemotePluginInfo
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatalf("failed to parse registry.json: %v", err)
	}

	if len(registry) != 40 {
		t.Fatalf("expected 40 plugins in registry.json, got %d", len(registry))
	}

	tempDir := t.TempDir()
	mgr, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	for _, item := range registry {
		archivePath := filepath.Join(rootPluginsDir, fmt.Sprintf("%s.tahr", item.ID))
		if _, err := os.Stat(archivePath); err != nil {
			t.Fatalf("missing expected archive %s: %v", archivePath, err)
		}

		m, err := mgr.Install(archivePath)
		if err != nil {
			t.Fatalf("failed to install plugin %s from %s: %v", item.ID, archivePath, err)
		}
		if m.ID != item.ID {
			t.Errorf("manifest ID mismatch: expected %s, got %s", item.ID, m.ID)
		}
	}

	installed := mgr.InstalledPlugins()
	if len(installed) < 40 {
		t.Fatalf("expected at least 40 installed plugins, got %d", len(installed))
	}

	// Verify specific capabilities and language configs (Tier 1, Tier 2, Tier 3)
	goConfig := mgr.GetLanguageConfig(".go")
	if goConfig == nil || goConfig.ID != "go" {
		t.Errorf("expected .go language config registered, got %+v", goConfig)
	}

	rustConfig := mgr.GetLanguageConfig(".rs")
	if rustConfig == nil || rustConfig.ID != "rust" {
		t.Errorf("expected .rs language config registered, got %+v", rustConfig)
	}

	pyConfig := mgr.GetLanguageConfig(".py")
	if pyConfig == nil || pyConfig.ID != "python" {
		t.Errorf("expected .py language config registered, got %+v", pyConfig)
	}

	tsConfig := mgr.GetLanguageConfig(".ts")
	if tsConfig == nil || tsConfig.ID != "typescript" {
		t.Errorf("expected .ts language config registered, got %+v", tsConfig)
	}

	cConfig := mgr.GetLanguageConfig(".cpp")
	if cConfig == nil || cConfig.ID != "c_cpp" {
		t.Errorf("expected .cpp language config registered, got %+v", cConfig)
	}

	httpConfig := mgr.GetLanguageConfig(".http")
	if httpConfig == nil || httpConfig.ID != "http" {
		t.Errorf("expected .http language config registered, got %+v", httpConfig)
	}

	protoConfig := mgr.GetLanguageConfig(".proto")
	if protoConfig == nil || protoConfig.ID != "proto" {
		t.Errorf("expected .proto language config registered, got %+v", protoConfig)
	}

	envConfig := mgr.GetLanguageConfig(".env")
	if envConfig == nil || envConfig.ID != "dotenv" {
		t.Errorf("expected .env language config registered, got %+v", envConfig)
	}

	sqliteConfig := mgr.GetLanguageConfig(".sqlite")
	if sqliteConfig == nil || sqliteConfig.ID != "sqlite" {
		t.Errorf("expected .sqlite language config registered, got %+v", sqliteConfig)
	}

	logConfig := mgr.GetLanguageConfig(".log")
	if logConfig == nil || logConfig.ID != "log" {
		t.Errorf("expected .log language config registered, got %+v", logConfig)
	}

	sqlConfig := mgr.GetLanguageConfig(".sql")
	if sqlConfig == nil || sqlConfig.ID != "sql" {
		t.Errorf("expected .sql language config registered, got %+v", sqlConfig)
	}

	pprofConfig := mgr.GetLanguageConfig(".pprof")
	if pprofConfig == nil || pprofConfig.ID != "pprof" {
		t.Errorf("expected .pprof language config registered, got %+v", pprofConfig)
	}

	jupyterConfig := mgr.GetLanguageConfig(".ipynb")
	if jupyterConfig == nil || jupyterConfig.ID != "jupyter" {
		t.Errorf("expected .ipynb language config registered, got %+v", jupyterConfig)
	}

	absRegistryPath, _ := filepath.Abs(registryPath)
	_ = mgr.AddRepository(PluginRepository{
		ID:      "test-local-registry",
		Name:    "Local Registry",
		URL:     absRegistryPath,
		Type:    "local",
		Enabled: true,
	})

	catalog, err := mgr.FetchCatalog("", "")
	if err != nil {
		t.Fatalf("FetchCatalog failed: %v", err)
	}
	if len(catalog) < 40 {
		t.Fatalf("expected at least 40 catalog items from registry.json, got %d", len(catalog))
	}
}

func TestPluginDisable_SuppressesLSPAndDAPAndLanguages(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	testGoManifest := Manifest{
		ID:          "tahr-go",
		Name:        "Go Language Support",
		Version:     "1.0.0",
		Description: "Go tooling",
		Languages: []LanguageConfig{
			{
				ID:         "go",
				Extensions: []string{".go"},
			},
		},
		LSP: &LSPConfig{
			ServerName: "gopls",
			Command:    "gopls",
		},
		DAP: &DAPConfig{
			AdapterName: "delve",
			Command:     "dlv",
		},
	}

	if _, err := mgr.InstallDeclarative(testGoManifest); err != nil {
		t.Fatalf("failed to install declarative plugin: %v", err)
	}

	// 1. Initially enabled
	if !mgr.IsEnabled("tahr-go") {
		t.Fatalf("expected plugin to be enabled initially")
	}
	if !mgr.IsExtensionActive(".go") {
		t.Fatalf("expected .go extension to be active")
	}
	if mgr.GetLanguageConfig(".go") == nil {
		t.Fatalf("expected .go language config to be present")
	}
	if lspCfg := mgr.GetLSPForExt(".go"); lspCfg == nil || lspCfg.ServerName != "gopls" {
		t.Fatalf("expected gopls LSP config, got %+v", lspCfg)
	}
	if dapCfg := mgr.GetDAPForExt(".go"); dapCfg == nil || dapCfg.AdapterName != "delve" {
		t.Fatalf("expected delve DAP config, got %+v", dapCfg)
	}

	// 2. Disable plugin
	var notifiedID string
	var notifiedEnabled bool
	mgr.AddLifecycleListener(func(id string, enabled bool) {
		notifiedID = id
		notifiedEnabled = enabled
	})

	if err := mgr.DisablePlugin("tahr-go"); err != nil {
		t.Fatalf("DisablePlugin failed: %v", err)
	}
	if notifiedID != "tahr-go" || notifiedEnabled != false {
		t.Fatalf("expected lifecycle notification (tahr-go, false), got (%s, %v)", notifiedID, notifiedEnabled)
	}

	// Verify all features for .go are completely suppressed
	if mgr.IsEnabled("tahr-go") {
		t.Errorf("expected plugin to be disabled")
	}
	if mgr.IsExtensionActive(".go") {
		t.Errorf("expected .go extension to be inactive when plugin disabled")
	}
	if mgr.GetLanguageConfig(".go") != nil {
		t.Errorf("expected .go language config to be nil when plugin disabled")
	}
	if lspCfg := mgr.GetLSPForExt(".go"); lspCfg != nil {
		t.Errorf("expected nil LSP config when plugin disabled, got %+v", lspCfg)
	}
	if dapCfg := mgr.GetDAPForExt(".go"); dapCfg != nil {
		t.Errorf("expected nil DAP config when plugin disabled, got %+v", dapCfg)
	}

	// 3. Simulate IDE restart by creating a new Manager on the same directory
	mgr2, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager reload failed: %v", err)
	}
	defer mgr2.Close()

	if mgr2.IsEnabled("tahr-go") {
		t.Errorf("expected plugin to remain disabled across restarts")
	}
	if mgr2.IsExtensionActive(".go") {
		t.Errorf("expected .go to remain inactive across restarts")
	}
	if mgr2.GetLanguageConfig(".go") != nil {
		t.Errorf("expected .go language config to remain nil across restarts")
	}
	if lspCfg := mgr2.GetLSPForExt(".go"); lspCfg != nil {
		t.Errorf("expected nil LSP config across restarts, got %+v", lspCfg)
	}
	if dapCfg := mgr2.GetDAPForExt(".go"); dapCfg != nil {
		t.Errorf("expected nil DAP config across restarts, got %+v", dapCfg)
	}

	// 4. Re-enable plugin
	if err := mgr2.EnablePlugin("tahr-go"); err != nil {
		t.Fatalf("EnablePlugin failed: %v", err)
	}
	if !mgr2.IsEnabled("tahr-go") {
		t.Errorf("expected plugin to be re-enabled")
	}
	if !mgr2.IsExtensionActive(".go") {
		t.Errorf("expected .go to be active after re-enable")
	}
	if mgr2.GetLSPForExt(".go") == nil {
		t.Errorf("expected LSP config restored after re-enable")
	}
	if mgr2.GetDAPForExt(".go") == nil {
		t.Errorf("expected DAP config restored after re-enable")
	}
}






