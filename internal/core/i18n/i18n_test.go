package i18n

import (
	"strings"
	"testing"
)

func TestI18n_DefaultEnglishAndFormatting(t *testing.T) {
	SetLocale("en")
	if GetLocale() != "en" {
		t.Fatalf("Expected active locale 'en', got '%s'", GetLocale())
	}

	title := T("dialog.crash.title")
	if !strings.Contains(title, "Attention") {
		t.Errorf("Expected English crash title, got: %s", title)
	}

	formatted := T("modal.bugreport.file_verified", 512)
	if formatted != "✓ File verified (512 KB)" {
		t.Errorf("Expected formatted string, got: %s", formatted)
	}

	// Missing key fallback
	missing := T("some.unknown.key")
	if missing != "some.unknown.key" {
		t.Errorf("Expected fallback to raw key, got: %s", missing)
	}
}

func TestI18n_PluginRegistrationAndSwitching(t *testing.T) {
	// Register Russian dictionary
	ruDict := map[string]string{
		"dialog.crash.title":      "⚠  Внимание: Предыдущая сессия завершилась со сбоем",
		"btn.send_dev":            "  Отправить разработчику (Enter)  ",
		"modal.bugreport.file_verified": "✓ Файл проверен (%d КБ)",
	}
	RegisterTranslations("ru", "Русский", ruDict)

	locales := AvailableLocales()
	if len(locales) < 2 {
		t.Fatalf("Expected at least 2 locales (en, ru), got %d", len(locales))
	}
	if locales[0].Code != "en" {
		t.Errorf("Expected English to be first locale")
	}

	// Switch to Russian
	SetLocale("ru")
	if GetLocale() != "ru" {
		t.Errorf("Expected active locale 'ru', got '%s'", GetLocale())
	}

	ruTitle := T("dialog.crash.title")
	if !strings.Contains(ruTitle, "Внимание") {
		t.Errorf("Expected Russian title, got: %s", ruTitle)
	}

	ruBtn := T("btn.send_dev")
	if !strings.Contains(ruBtn, "Отправить разработчику") {
		t.Errorf("Expected Russian button, got: %s", ruBtn)
	}
	if strings.Contains(ruBtn, "[") || strings.Contains(ruBtn, "]") {
		t.Errorf("Button contains forbidden square brackets: %s", ruBtn)
	}

	ruFormatted := T("modal.bugreport.file_verified", 256)
	if ruFormatted != "✓ Файл проверен (256 КБ)" {
		t.Errorf("Expected formatted Russian string, got: %s", ruFormatted)
	}

	// Key not in Russian, must fall back to English
	enFallback := T("dialog.crash.dump_file", "crash-123.json")
	if enFallback != "Dump file: crash-123.json" {
		t.Errorf("Expected English fallback, got: %s", enFallback)
	}

	// Switch back to English
	SetLocale("en")
	enTitle := T("dialog.crash.title")
	if !strings.Contains(enTitle, "Attention") {
		t.Errorf("Expected English title after switching back, got: %s", enTitle)
	}
}
