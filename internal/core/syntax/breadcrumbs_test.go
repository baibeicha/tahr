package syntax

import (
	"strings"
	"testing"
)

func TestExtractBreadcrumbs_Go(t *testing.T) {
	code := `package main

type AppModel struct {
	width int
}

func (m *AppModel) View() {
	println("hello")
}
`
	lines := strings.Split(code, "\n")
	// Cursor on line 7 (inside View)
	items := ExtractBreadcrumbs("internal/ui/tui/app.go", lines, 7)
	formatted := FormatBreadcrumbs(items)

	if !strings.Contains(formatted, "app.go") {
		t.Errorf("expected breadcrumbs to contain app.go, got: %s", formatted)
	}
	if !strings.Contains(formatted, "main") {
		t.Errorf("expected breadcrumbs to contain package main, got: %s", formatted)
	}
	if !strings.Contains(formatted, "AppModel") {
		t.Errorf("expected breadcrumbs to contain AppModel, got: %s", formatted)
	}
	if !strings.Contains(formatted, "View()") {
		t.Errorf("expected breadcrumbs to contain View(), got: %s", formatted)
	}
}

func TestExtractBreadcrumbs_Python(t *testing.T) {
	code := `class MyEditor:
    def open_file(self, path):
        print(path)
`
	lines := strings.Split(code, "\n")
	items := ExtractBreadcrumbs("editor.py", lines, 2)
	formatted := FormatBreadcrumbs(items)

	if !strings.Contains(formatted, "MyEditor") {
		t.Errorf("expected MyEditor, got: %s", formatted)
	}
	if !strings.Contains(formatted, "open_file()") {
		t.Errorf("expected open_file(), got: %s", formatted)
	}
}

func TestExtractBreadcrumbs_Markdown(t *testing.T) {
	code := `# Tahr IDE
Some text
## Installation
Run go build
`
	lines := strings.Split(code, "\n")
	items := ExtractBreadcrumbs("README.md", lines, 3)
	formatted := FormatBreadcrumbs(items)

	if !strings.Contains(formatted, "Installation") {
		t.Errorf("expected Installation heading, got: %s", formatted)
	}
}
