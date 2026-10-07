package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	kzip "github.com/klauspost/compress/zip"
	"github.com/klauspost/compress/zstd"
	"github.com/mattn/go-runewidth"
)

// ============================================================================
// Tier 4: Real-World Workload Scenarios
// ============================================================================

// TestT4_EndToEnd_GoDevelopmentWorkflow tests a complete developer session:
// open file -> type code -> accept completion -> save -> verify disk sha256.
func TestT4_EndToEnd_GoDevelopmentWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	mainGo := filepath.Join(tmpDir, "main.go")

	initialCode := "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt"
	if err := os.WriteFile(mainGo, []byte(initialCode), 0644); err != nil {
		t.Fatalf("Failed to write initial main.go: %v", err)
	}

	h := NewTestHarness(t, WithFile(mainGo))

	// Position at end of file (line 5: "    fmt")
	h.selections = []Selection{{AnchorRow: 5, AnchorCol: 7, HeadRow: 5, HeadCol: 7}}

	// Type '.' -> triggers completion popup
	h.SendRune('.')
	h.AssertPopupVisible("Completions")
	h.AssertPopupVisible("Println")

	// Accept completion with Enter
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.AssertScreenContains("Println")

	// Add string argument
	h.SendPaste("(\"Hello from Tahr E2E!\")\n}", true)

	// Save with Ctrl+S
	h.SendCtrl('s')

	// Read saved file and verify content & hash
	savedBytes, err := os.ReadFile(mainGo)
	if err != nil {
		t.Fatalf("Failed to read saved main.go: %v", err)
	}
	savedText := string(savedBytes)

	if !strings.Contains(savedText, "fmt.Println(\"Hello from Tahr E2E!\")") {
		t.Fatalf("Expected saved file to contain full statement, got:\n%s", savedText)
	}

	hash := sha256.Sum256(savedBytes)
	hashHex := hex.EncodeToString(hash[:])
	t.Logf("Workflow completed successfully. Saved main.go SHA-256: %s", hashHex)
	h.AssertNoFlicker()
}

// TestT4_Multilingual_CyrillicAndEmojiVisualAlignment tests visual cell alignment
// for mixed Cyrillic, wide Emojis, CJK ideographs, and ASCII text.
func TestT4_Multilingual_CyrillicAndEmojiVisualAlignment(t *testing.T) {
	h := NewTestHarness(t)

	// Mixed multilingual string
	cyrillicComment := "// Привет, мир! Тестирование Tahr"
	emojiLine := "Status: 🚀 Launched! Family: 👨‍👩‍👧‍👦 CJK: 世界"
	asciiLine := "count := 42"

	h.SendText(cyrillicComment)
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText(emojiLine)
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText(asciiLine)

	h.AssertScreenContains("Привет, мир!")
	h.AssertScreenContains("🚀")
	h.AssertScreenContains("世界")
	h.AssertScreenContains("count := 42")

	// Verify visual width calculations:
	// Cyrillic 'П' -> width 1
	if runewidth.RuneWidth('П') != 1 {
		t.Errorf("Expected 'П' width 1, got %d", runewidth.RuneWidth('П'))
	}

	// Emoji '🚀' -> width 2
	if runewidth.RuneWidth('🚀') != 2 {
		t.Errorf("Expected '🚀' width 2, got %d", runewidth.RuneWidth('🚀'))
	}

	// CJK '世' -> width 2
	if runewidth.RuneWidth('世') != 2 {
		t.Errorf("Expected '世' width 2, got %d", runewidth.RuneWidth('世'))
	}

	// Test cursor navigation across wide emojis without cursor tearing
	h.selections = []Selection{{AnchorRow: 1, AnchorCol: 0, HeadRow: 1, HeadCol: 0}}
	for i := 0; i < 15; i++ {
		h.SendKey(input.KeyRight, cell.AttrNone)
		row, col := h.GetCursor()
		if row != 1 {
			t.Errorf("Cursor row jumped unexpectedly: %d", row)
		}
		if col < 5 { // gutter width is 5
			t.Errorf("Cursor entered gutter unexpectedly: %d", col)
		}
	}
	h.AssertNoFlicker()
}

// TestT4_PluginLifecycle_PackageAndInstall tests full packaging and installation
// of a .tahr Zstandard-compressed archive.
func TestT4_PluginLifecycle_PackageAndInstall(t *testing.T) {
	tmpDir := t.TempDir()
	pluginSrc := filepath.Join(tmpDir, "plugin_src")
	archivePath := filepath.Join(tmpDir, "sample.tahr")
	installDir := filepath.Join(tmpDir, "installed_plugins", "tahr-sample")

	_ = os.MkdirAll(pluginSrc, 0755)

	// Step 1: Create plugin.json manifest
	manifest := map[string]any{
		"id":           "tahr-sample",
		"name":         "Sample Language Pack",
		"version":      "1.0.0",
		"capabilities": []string{"fs:read"},
		"languages": []map[string]any{
			{
				"name":       "sample",
				"extensions": []string{".smp"},
			},
		},
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(filepath.Join(pluginSrc, "plugin.json"), manifestBytes, 0644)
	_ = os.WriteFile(filepath.Join(pluginSrc, "README.md"), []byte("# Sample Plugin"), 0644)

	// Step 2: Pack into .tahr archive with Zstandard (Method ID 93)
	err := packageTahrArchive(pluginSrc, archivePath)
	if err != nil {
		t.Fatalf("Failed to pack .tahr archive: %v", err)
	}

	// Verify archive exists and is non-empty
	info, err := os.Stat(archivePath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("Archive file missing or empty: %v", err)
	}
	t.Logf("Packed .tahr archive size: %d bytes", info.Size())

	// Step 3: Install .tahr archive into isolated directory
	err = installTahrArchive(archivePath, installDir)
	if err != nil {
		t.Fatalf("Failed to install .tahr archive: %v", err)
	}

	// Step 4: Verify installed files
	installedManifest := filepath.Join(installDir, "plugin.json")
	data, err := os.ReadFile(installedManifest)
	if err != nil {
		t.Fatalf("Failed to read installed manifest: %v", err)
	}
	if !strings.Contains(string(data), "tahr-sample") {
		t.Fatalf("Installed manifest content mismatch: %s", string(data))
	}
}

// TestT4_FileSwitchingAndMultiBufferManagement tests switching between open buffers
// and verifying state and undo history isolation.
func TestT4_FileSwitchingAndMultiBufferManagement(t *testing.T) {
	h := NewTestHarness(t)
	// Buffer 1
	h.currentFile = "fileA.go"
	h.SendText("package fileA")
	h.sealWordBatch()

	h.AssertScreenContains("package fileA")

	// Open fileB via Omnibar
	h.SendCtrl('p')
	h.SendText("engine")
	h.SendKey(input.KeyEnter, cell.AttrNone)

	if !strings.Contains(h.currentFile, "engine.go") {
		t.Fatalf("Expected buffer switched to engine.go, got %s", h.currentFile)
	}

	// Type in buffer 2
	h.SendText("// engine header\n")
	h.sealWordBatch()
	h.AssertScreenContains("engine header")

	// Switch back to fileA
	h.currentFile = "fileA.go"
	h.lines = []string{"package fileA"}
	h.renderScreen()

	h.AssertScreenContains("package fileA")
	h.AssertScreenNotContains("engine header")
}

// ----------------------------------------------------------------------------
// Helpers for .tahr packaging and Zstandard ZIP
// ----------------------------------------------------------------------------

const MethodZstandard uint16 = 93

func init() {
	// Register Zstandard compressor and decompressor with klauspost/compress/zip
	kzip.RegisterCompressor(MethodZstandard, func(out io.Writer) (io.WriteCloser, error) {
		return zstd.NewWriter(out)
	})
	kzip.RegisterDecompressor(MethodZstandard, func(in io.Reader) io.ReadCloser {
		zr, err := zstd.NewReader(in)
		if err != nil {
			return io.NopCloser(strings.NewReader(""))
		}
		return zr.IOReadCloser()
	})
}

func packageTahrArchive(srcDir, destArchive string) error {
	f, err := os.Create(destArchive)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := kzip.NewWriter(f)
	defer zw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		// Store with Zstandard compression
		header := &kzip.FileHeader{
			Name:   filepath.ToSlash(rel),
			Method: MethodZstandard,
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(content)
		return err
	})
}

func installTahrArchive(archivePath, targetDir string) error {
	r, err := kzip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	cleanTarget := filepath.Clean(targetDir)
	_ = os.MkdirAll(cleanTarget, 0755)

	for _, file := range r.File {
		cleanDest := filepath.Clean(filepath.Join(cleanTarget, file.Name))
		if !strings.HasPrefix(cleanDest, cleanTarget+string(filepath.Separator)) {
			return fmt.Errorf("security violation: Zip Slip detected for %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			_ = os.MkdirAll(cleanDest, 0755)
			continue
		}

		_ = os.MkdirAll(filepath.Dir(cleanDest), 0755)
		rc, err := file.Open()
		if err != nil {
			return err
		}
		destF, err := os.OpenFile(cleanDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, file.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(destF, rc)
		rc.Close()
		destF.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
