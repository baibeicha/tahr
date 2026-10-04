package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/i18n"
	"tahr/internal/core/plugin"
)

func initTestRussianLocale(t *testing.T) {
	t.Helper()
	// Load ru.json from plugins/tahr-ru/locales/ru.json
	ruPath := filepath.Join("..", "..", "..", "plugins", "tahr-ru", "locales", "ru.json")
	data, err := os.ReadFile(ruPath)
	if err != nil {
		t.Fatalf("failed to read ru.json: %v", err)
	}
	var dict map[string]string
	if err := json.Unmarshal(data, &dict); err != nil {
		t.Fatalf("failed to parse ru.json: %v", err)
	}
	i18n.RegisterTranslations("ru", "Русский", dict)
}

func TestTooltip_RussianAndEnglish(t *testing.T) {
	initTestRussianLocale(t)
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 100
	app.height = 30
	if app.splash != nil {
		app.splash.Active = false
	}

	// Draw initial frame to populate toolbar hitboxes
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	app.View(frame)

	// 1. In English
	i18n.SetLocale("en")
	app.updateTooltip(0, 0)
	if app.tooltipText != "Main Menu" {
		t.Errorf("expected 'Main Menu', got %q", app.tooltipText)
	}

	// 2. In Russian
	i18n.SetLocale("ru")
	defer i18n.SetLocale("en")
	app.updateTooltip(0, 0)
	if app.tooltipText != "Главное меню" {
		t.Errorf("expected 'Главное меню', got %q", app.tooltipText)
	}

	// Verify run button tooltip in Russian
	for _, btn := range app.toolbarButtons {
		if btn.id == "run" {
			app.updateTooltip(btn.minX, 0)
			if !strings.Contains(app.tooltipText, "Запуск активного профиля") {
				t.Errorf("expected Russian run tooltip, got %q", app.tooltipText)
			}
			break
		}
	}

	// Test renderTooltip does not panic and uses proper visual bounds
	buf := buffer.NewBuffer(100, 30)
	app.renderTooltip(buf, 100, 30)
	i18n.SetLocale("en")
}

func TestSettings_AllCategoriesLocalized(t *testing.T) {
	initTestRussianLocale(t)

	mgr, _ := plugin.NewManager(t.TempDir())
	defer mgr.Close()
	st := NewSettingsState()
	st.pluginMgr = mgr
	st.Current.Language = "ru"
	i18n.SetLocale("ru")
	defer i18n.SetLocale("en")

	// Check Categories 0 to 6
	for catIdx := 0; catIdx <= 6; catIdx++ {
		fields := st.getCategoryFields(catIdx)
		if len(fields) == 0 {
			t.Errorf("category %d returned no fields", catIdx)
		}
		for _, f := range fields {
			if f.Label == "" {
				t.Errorf("category %d field %q has empty label", catIdx, f.Key)
			}
			if f.Value == "" {
				t.Errorf("category %d field %q has empty value", catIdx, f.Key)
			}
		}
	}

	// Verify Category 3 (Keybindings) labels in Russian
	kbFields := st.getCategoryFields(3)
	for _, f := range kbFields {
		if f.Key == "find" && f.Label != "Поиск в файле" {
			t.Errorf("expected 'Поиск в файле', got %q", f.Label)
		}
		if f.Key == "replace" && f.Label != "Поиск и замена" {
			t.Errorf("expected 'Поиск и замена', got %q", f.Label)
		}
	}

	// Verify Category 4 (Colors) labels in Russian
	colorFields := st.getCategoryFields(4)
	for _, f := range colorFields {
		if f.Key == "bg" && f.Label != "Фон редактора" {
			t.Errorf("expected 'Фон редактора', got %q", f.Label)
		}
	}
}

func TestMarketplace_CategoriesAndFiltering(t *testing.T) {
	initTestRussianLocale(t)
	i18n.SetLocale("ru")
	defer i18n.SetLocale("en")

	mgr, _ := plugin.NewManager(t.TempDir())
	defer mgr.Close()

	// Install dummy i18n and lsp plugins
	_, _ = mgr.InstallDeclarative(plugin.Manifest{
		ID:       "test-lang",
		Name:     "Test Lang",
		Version:  "1.0.0",
		Category: "lsp",
		LSP:      &plugin.LSPConfig{ServerName: "test-lsp", Command: "test-lsp"},
	})
	_, _ = mgr.InstallDeclarative(plugin.Manifest{
		ID:            "test-i18n",
		Name:          "Test Translation",
		Version:       "1.0.0",
		Category:      "i18n",
		Localizations: []plugin.LocalizationConfig{{Locale: "de", File: "de.json"}},
	})

	modal := NewMarketplaceModal(mgr)
	modal.Open = true

	// Check Categories list
	expectedCats := []string{"All", "LSP", "DAP", "Theme", "Tools", "i18n", "Database", "Infrastructure"}
	if len(modal.Categories) != len(expectedCats) {
		t.Fatalf("expected %d categories, got %d", len(expectedCats), len(modal.Categories))
	}

	// Check Installed tab filtering
	modal.ActiveTab = 1 // Installed
	modal.CatIdx = 0    // All
	modal.Refresh()
	if len(modal.Plugins) != 2 {
		t.Errorf("expected 2 installed plugins under All, got %d", len(modal.Plugins))
	}

	// Filter by LSP (idx 1)
	modal.CatIdx = 1
	modal.Refresh()
	if len(modal.Plugins) != 1 || modal.Plugins[0].ID != "test-lang" {
		t.Errorf("expected 1 installed plugin under LSP, got %d", len(modal.Plugins))
	}

	// Filter by i18n (idx 5)
	modal.CatIdx = 5
	modal.Refresh()
	if len(modal.Plugins) != 1 || modal.Plugins[0].ID != "test-i18n" {
		t.Errorf("expected 1 installed plugin under i18n, got %d", len(modal.Plugins))
	}

	// Test mouse click on Category switcher row (startY+3)
	screenW, screenH := 100, 30
	modalW := screenW - 4
	modalH := screenH - 2
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	catDisplay := i18n.T("market.cat." + strings.ToLower(modal.Categories[modal.CatIdx]))
	catLabel := fmt.Sprintf("%s: ◄ %s ►", i18n.T("market.category"), catDisplay)
	catX := startX + modalW - len([]rune(catLabel)) - 3

	// Click on arrow to increment
	consumed := modal.HandleClick(catX+len([]rune(catLabel))-1, startY+3, screenW, screenH)
	if !consumed {
		t.Errorf("expected HandleClick to consume category arrow click")
	}
	if modal.CatIdx != 6 {
		t.Errorf("expected CatIdx to increment to 6, got %d", modal.CatIdx)
	}
}
