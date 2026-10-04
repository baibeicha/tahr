package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/crash"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

func TestSplashScreen_RenderingAndSkip(t *testing.T) {
	i18n.SetLocale("en")
	splash := NewSplashScreenState()
	if !splash.Active || splash.Done {
		t.Fatalf("Expected splash active and not done on initialization")
	}

	buf := buffer.NewBuffer(80, 24)
	theme := ui.DefaultTheme()
	splash.Render(buf, 80, 24, &theme)

	// Check if TAHR letters and progress bar exist in the rendered buffer
	var textDump strings.Builder
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			c := buf.Cell(x, y)
			if c.Rune != 0 {
				textDump.WriteRune(c.Rune)
			} else {
				textDump.WriteRune(' ')
			}
		}
		textDump.WriteRune('\n')
	}

	rendered := textDump.String()
	if !strings.Contains(rendered, "████") {
		t.Errorf("Expected block characters in splash screen")
	}
	if !strings.Contains(rendered, "THE AGILITY-FIRST") {
		t.Errorf("Expected tagline in splash screen")
	}

	// Test Tick
	splash.StartTime = time.Now().Add(-700 * time.Millisecond)
	splash.Tick()
	if splash.Progress < 0.8 {
		t.Errorf("Expected progress >= 0.8 at 700ms, got %f", splash.Progress)
	}

	// Test key skip
	splash.HandleKey(input.Key{Type: input.KeyRune, Rune: 'x'})
	if splash.Active || !splash.Done {
		t.Errorf("Expected splash to be dismissed on key press")
	}

	// Test mouse skip
	splash2 := NewSplashScreenState()
	splash2.HandleMouse(input.Mouse{Button: input.MouseLeft})
	if splash2.Active || !splash2.Done {
		t.Errorf("Expected splash to be dismissed on mouse click")
	}
}

func TestCrashRecoveryDialog_NoBracketsAndKeyHandling(t *testing.T) {
	i18n.SetLocale("en")
	dlg := NewCrashRecoveryDialog()
	dlg.Open = true
	dlg.Payload = &crash.CrashReportPayload{
		ReportID:    "test-report-id",
		Timestamp:   "2026-10-03T12:15:04Z",
		AppVersion:  "0.1.0-alpha",
		PanicReason: "slice bounds out of range",
		Platform:    crash.CurrentPlatformInfo(),
	}

	buf := buffer.NewBuffer(80, 24)
	theme := ui.DefaultTheme()
	dlg.Render(buf, 80, 24, &theme)

	var textDump strings.Builder
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			c := buf.Cell(x, y)
			if c.Rune != 0 {
				textDump.WriteRune(c.Rune)
			} else {
				textDump.WriteRune(' ')
			}
		}
		textDump.WriteRune('\n')
	}

	rendered := textDump.String()

	// CRITICAL USER REQUIREMENT: all buttons without [ ]
	if strings.Contains(rendered, "[ Send") || strings.Contains(rendered, "[Send") {
		t.Errorf("Found forbidden square brackets around Send button")
	}
	if strings.Contains(rendered, "[ Delete") || strings.Contains(rendered, "[Delete") {
		t.Errorf("Found forbidden square brackets around Delete button")
	}
	if strings.Contains(rendered, "[ Ignore") || strings.Contains(rendered, "[Ignore") {
		t.Errorf("Found forbidden square brackets around Ignore button")
	}
	if strings.Contains(rendered, "[ View") || strings.Contains(rendered, "[View") {
		t.Errorf("Found forbidden square brackets around View button")
	}

	// Verify button text exists cleanly
	if !strings.Contains(rendered, "Send to Developer") {
		t.Logf("Rendered text:\n%s", rendered)
		t.Errorf("Expected 'Send to Developer' button text")
	}
	if !strings.Contains(rendered, "Delete") {
		t.Errorf("Expected 'Delete' button text")
	}
	if !strings.Contains(rendered, "Ignore") {
		t.Errorf("Expected 'Ignore' button text")
	}

	// Test Key Navigation
	dlg.FocusedButton = 0
	dlg.HandleKey(input.Key{Type: input.KeyRight})
	if dlg.FocusedButton != 1 {
		t.Errorf("Expected focus 1 after KeyRight, got %d", dlg.FocusedButton)
	}

	dlg.HandleKey(input.Key{Type: input.KeyLeft})
	if dlg.FocusedButton != 0 {
		t.Errorf("Expected focus 0 after KeyLeft, got %d", dlg.FocusedButton)
	}

	// Test Enter triggers send
	_, action := dlg.HandleKey(input.Key{Type: input.KeyEnter})
	if action != CrashActionSend {
		t.Errorf("Expected action CrashActionSend on Enter, got %s", action)
	}

	// Test 'v' triggers view logs
	_, actionV := dlg.HandleKey(input.Key{Type: input.KeyRune, Rune: 'v'})
	if actionV != CrashActionViewLogs {
		t.Errorf("Expected action CrashActionViewLogs on 'v', got %s", actionV)
	}

	// Test Esc triggers ignore
	_, actionEsc := dlg.HandleKey(input.Key{Type: input.KeyEsc})
	if actionEsc != CrashActionIgnore {
		t.Errorf("Expected action CrashActionIgnore on Esc, got %s", actionEsc)
	}
	if dlg.Open {
		t.Errorf("Expected dialog to close on Esc")
	}
}

func TestBugReportModal_NoBracketsAndScreenshotValidation(t *testing.T) {
	i18n.SetLocale("en")
	modal := NewBugReportModal()
	modal.OpenModal("0.1.0-alpha", []string{"go", "git"})

	buf := buffer.NewBuffer(80, 24)
	theme := ui.DefaultTheme()
	modal.Render(buf, 80, 24, &theme)

	var textDump strings.Builder
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			c := buf.Cell(x, y)
			if c.Rune != 0 {
				textDump.WriteRune(c.Rune)
			} else {
				textDump.WriteRune(' ')
			}
		}
		textDump.WriteRune('\n')
	}

	rendered := textDump.String()

	// CRITICAL USER REQUIREMENT: all buttons without [ ]
	if strings.Contains(rendered, "[ Browse") || strings.Contains(rendered, "[Browse") {
		t.Errorf("Found forbidden square brackets around Browse button")
	}
	if strings.Contains(rendered, "[ View") || strings.Contains(rendered, "[View") {
		t.Errorf("Found forbidden square brackets around View Data button")
	}
	if strings.Contains(rendered, "[ Send") || strings.Contains(rendered, "[Send") {
		t.Errorf("Found forbidden square brackets around Send button")
	}
	if strings.Contains(rendered, "[ Copy") || strings.Contains(rendered, "[Copy") {
		t.Errorf("Found forbidden square brackets around Copy button")
	}
	if strings.Contains(rendered, "[ Cancel") || strings.Contains(rendered, "[Cancel") {
		t.Errorf("Found forbidden square brackets around Cancel button")
	}

	// Test Screenshot validation
	modal.Screenshot = "non_existent_file_12345.png"
	modal.ValidateScreenshot()
	if modal.ScreenshotValid {
		t.Errorf("Expected screenshot validation to fail for non-existent file")
	}
	if !strings.Contains(modal.ScreenshotStatus, "not found") {
		t.Errorf("Expected 'not found' status, got %s", modal.ScreenshotStatus)
	}

	// Create valid dummy PNG
	tmpDir := t.TempDir()
	validImg := filepath.Join(tmpDir, "bug.png")
	_ = os.WriteFile(validImg, []byte("\x89PNG\r\n\x1a\nfakeimage"), 0644)

	modal.Screenshot = validImg
	modal.ValidateScreenshot()
	if !modal.ScreenshotValid {
		t.Errorf("Expected screenshot validation to succeed for valid PNG")
	}
	if !strings.Contains(modal.ScreenshotStatus, "verified") {
		t.Errorf("Expected 'verified' status, got %s", modal.ScreenshotStatus)
	}

	// Test Title typing
	modal.FocusField = 0
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 'T'})
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 'e'})
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 's'})
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 't'})
	if modal.Title != "Test" {
		t.Errorf("Expected title 'Test', got '%s'", modal.Title)
	}

	// Test Description typing
	modal.FocusField = 1
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 'D'})
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 'e'})
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 's'})
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 'c'})
	if modal.Description != "Desc" {
		t.Errorf("Expected description 'Desc', got '%s'", modal.Description)
	}

	// Test Markdown generation for GitHub
	ghMd := modal.GetGitHubMarkdown()
	if !strings.Contains(ghMd, "Desc") {
		t.Errorf("Expected description in GitHub markdown")
	}
	if !strings.Contains(ghMd, "0.1.0-alpha") {
		t.Errorf("Expected app version in GitHub markdown")
	}
}

func TestLogInspectorModal_TabsAndSearch(t *testing.T) {
	insp := NewLogInspectorModal()
	traceLines := []string{"log line 1", "error in line 2: nil dereference", "log line 3"}
	metaLines := []string{"OS: windows", "Arch: amd64"}
	insp.SetContent("Тест инспектора", traceLines, metaLines, `{"status": "ok"}`)

	if !insp.Open {
		t.Fatalf("Expected inspector to be open")
	}

	// Check Tab 0
	lines0 := insp.currentLines()
	if len(lines0) != 3 {
		t.Errorf("Expected 3 lines on tab 0, got %d", len(lines0))
	}

	// Switch to Tab 1
	insp.HandleKey(input.Key{Type: input.KeyTab})
	if insp.ActiveTab != 1 {
		t.Errorf("Expected tab 1, got %d", insp.ActiveTab)
	}
	lines1 := insp.currentLines()
	if len(lines1) != 2 {
		t.Errorf("Expected 2 lines on tab 1, got %d", len(lines1))
	}

	// Switch back to Tab 0
	insp.HandleKey(input.Key{Type: input.KeyBacktab})
	if insp.ActiveTab != 0 {
		t.Errorf("Expected tab 0, got %d", insp.ActiveTab)
	}

	// Search filter
	insp.HandleKey(input.Key{Type: input.KeyRune, Rune: '/'})
	insp.HandleKey(input.Key{Type: input.KeyRune, Rune: 'e'})
	insp.HandleKey(input.Key{Type: input.KeyRune, Rune: 'r'})
	insp.HandleKey(input.Key{Type: input.KeyRune, Rune: 'r'})
	insp.HandleKey(input.Key{Type: input.KeyEnter})

	filtered := insp.currentLines()
	if len(filtered) != 1 {
		t.Errorf("Expected 1 filtered line matching 'err', got %d", len(filtered))
	}

	// Close inspector on Esc
	insp.HandleKey(input.Key{Type: input.KeyEsc})
	if insp.Open {
		t.Errorf("Expected inspector to be closed on Esc")
	}
}

func TestScanWorkspaceFiles_Optimization(t *testing.T) {
	tmpDir := t.TempDir()

	// Create structure with ignored directories
	_ = os.MkdirAll(filepath.Join(tmpDir, ".git", "objects"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644)

	_ = os.MkdirAll(filepath.Join(tmpDir, "node_modules", "pkg"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "node_modules", "pkg", "index.js"), []byte("// js"), 0644)

	_ = os.MkdirAll(filepath.Join(tmpDir, "target", "debug"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "target", "debug", "binary"), []byte("bin"), 0644)

	_ = os.MkdirAll(filepath.Join(tmpDir, ".cache"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, ".cache", "tmp"), []byte("cache"), 0644)

	// Valid project files
	_ = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	_ = os.MkdirAll(filepath.Join(tmpDir, "pkg"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "pkg", "lib.go"), []byte("package pkg"), 0644)

	start := time.Now()
	files := ScanWorkspaceFiles(tmpDir, 1000)
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("Scan took too long: %v", elapsed)
	}

	for _, f := range files {
		if strings.Contains(f, ".git") || strings.Contains(f, "node_modules") || strings.Contains(f, "target") || strings.Contains(f, ".cache") {
			t.Errorf("ScanWorkspaceFiles did not ignore directory: %s", f)
		}
	}

	if len(files) != 2 {
		t.Errorf("Expected exactly 2 valid project files, got %d: %v", len(files), files)
	}
}

func TestI18nLiveSwitchingAndRussianLanguagePack(t *testing.T) {
	// 1. Ensure English is default
	if i18n.GetLocale() != "en" {
		i18n.SetLocale("en")
	}

	dlg := NewCrashRecoveryDialog()
	dlg.Open = true
	dlg.ReportPath = "crash-123.json"
	dlg.Payload = &crash.CrashReportPayload{
		PanicReason: "test panic",
	}

	bufEn := buffer.NewBuffer(80, 24)
	theme := ui.DefaultTheme()
	dlg.Render(bufEn, 80, 24, &theme)

	var textEn strings.Builder
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			c := bufEn.Cell(x, y)
			if c.Rune != 0 {
				textEn.WriteRune(c.Rune)
			} else {
				textEn.WriteRune(' ')
			}
		}
		textEn.WriteRune('\n')
	}
	renderedEn := textEn.String()
	if !strings.Contains(renderedEn, "Send to Developer") {
		t.Errorf("Expected English 'Send to Developer' in default locale, got:\n%s", renderedEn)
	}

	// 2. Load Russian translations from plugins/tahr-ru/locales/ru.json
	ruData, err := os.ReadFile(filepath.Join("..", "..", "..", "plugins", "tahr-ru", "locales", "ru.json"))
	if err != nil {
		t.Fatalf("Failed to read ru.json: %v", err)
	}
	var ruDict map[string]string
	if err := json.Unmarshal(ruData, &ruDict); err != nil {
		t.Fatalf("Failed to parse ru.json: %v", err)
	}
	i18n.RegisterTranslations("ru", "Русский", ruDict)

	// 3. Switch to Russian live
	i18n.SetLocale("ru")
	if i18n.GetLocale() != "ru" {
		t.Fatalf("Expected active locale 'ru', got '%s'", i18n.GetLocale())
	}

	bufRu := buffer.NewBuffer(80, 24)
	dlg.Render(bufRu, 80, 24, &theme)

	var textRu strings.Builder
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			c := bufRu.Cell(x, y)
			if c.Rune != 0 {
				textRu.WriteRune(c.Rune)
			} else {
				textRu.WriteRune(' ')
			}
		}
		textRu.WriteRune('\n')
	}
	renderedRu := textRu.String()

	// CRITICAL: verify Russian text exists and no square brackets
	if !strings.Contains(renderedRu, "Отправить разработчику") {
		t.Errorf("Expected Russian 'Отправить разработчику' in ru locale, got:\n%s", renderedRu)
	}
	if !strings.Contains(renderedRu, "Удалить") {
		t.Errorf("Expected Russian 'Удалить' in ru locale")
	}
	if !strings.Contains(renderedRu, "Игнорировать") {
		t.Errorf("Expected Russian 'Игнорировать' in ru locale")
	}
	if strings.Contains(renderedRu, "[ Отправить") || strings.Contains(renderedRu, "[Отправить") {
		t.Errorf("Found forbidden square brackets in Russian button rendering")
	}

	// 4. Switch back to English
	i18n.SetLocale("en")
	if i18n.GetLocale() != "en" {
		t.Fatalf("Expected active locale 'en', got '%s'", i18n.GetLocale())
	}
}

