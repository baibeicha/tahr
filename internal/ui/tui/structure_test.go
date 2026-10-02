package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/lsp"
	"tahr/internal/ui"
)

func TestStructure_RegexExtraction_Go(t *testing.T) {
	code := `package main

type Server struct {
	port int
}

type Runner interface {
	Run()
}

func (s *Server) Start() error {
	return nil
}

func main() {
}
`
	items := ExtractSymbolsRegex(".go", code)
	if len(items) < 4 {
		t.Fatalf("expected at least 4 symbols, got %d", len(items))
	}

	foundStruct := false
	foundInterface := false
	foundMethod := false
	foundFunc := false

	for _, item := range items {
		switch item.Name {
		case "Server":
			foundStruct = (item.Icon == "[S]")
		case "Runner":
			foundInterface = (item.Icon == "[I]")
		case "Start":
			foundMethod = (item.Icon == "[M]")
		case "main":
			foundFunc = (item.Icon == "[F]")
		}
	}

	if !foundStruct {
		t.Errorf("expected struct Server with [S]")
	}
	if !foundInterface {
		t.Errorf("expected interface Runner with [I]")
	}
	if !foundMethod {
		t.Errorf("expected method Start with [M]")
	}
	if !foundFunc {
		t.Errorf("expected function main with [F]")
	}
}

func TestStructure_ConvertLSPSymbols(t *testing.T) {
	lspSymbols := []lsp.DocumentSymbol{
		{
			Name: "MyFunc",
			Kind: lsp.SymbolKindFunction,
			Range: lsp.Range{
				Start: lsp.Position{Line: 12, Character: 0},
				End:   lsp.Position{Line: 18, Character: 1},
			},
		},
		{
			Name: "Config",
			Kind: lsp.SymbolKindStruct,
			Range: lsp.Range{
				Start: lsp.Position{Line: 5, Character: 0},
				End:   lsp.Position{Line: 9, Character: 1},
			},
		},
	}

	items := ConvertLSPSymbols(lspSymbols)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Name != "MyFunc" || items[0].Icon != "[F]" || items[0].Line != 12 {
		t.Errorf("unexpected first item: %+v", items[0])
	}
	if items[1].Name != "Config" || items[1].Icon != "[S]" || items[1].Line != 5 {
		t.Errorf("unexpected second item: %+v", items[1])
	}
}

func TestStructurePanel_NavigationAndJump(t *testing.T) {
	panel := NewStructurePanel()
	panel.SetItems([]StructureItem{
		{Name: "Init", Kind: "Function", Icon: "[F]", Line: 10},
		{Name: "Update", Kind: "Function", Icon: "[F]", Line: 20},
		{Name: "View", Kind: "Function", Icon: "[F]", Line: 30},
	})

	// Down arrow moves selection
	panel.HandleKey(input.Key{Type: input.KeyDown})
	if panel.Selected != 1 {
		t.Errorf("expected Selected 1, got %d", panel.Selected)
	}

	// Enter returns jump target
	handled, jump := panel.HandleKey(input.Key{Type: input.KeyEnter})
	if !handled || jump == nil || jump.Line != 20 {
		t.Fatalf("expected jump to line 20, got %+v", jump)
	}

	// Click on row 2 (which is index 2)
	clicked := panel.HandleClick(2)
	if clicked == nil || clicked.Line != 30 {
		t.Fatalf("expected click to return line 30, got %+v", clicked)
	}

	// Render
	buf := buffer.NewBuffer(30, 10)
	th := ui.DefaultTheme()
	panel.Render(buf, 0, 0, 30, 10, true, &th)
}

func TestStructure_VarAndConstBlocks(t *testing.T) {
	code := `package main

const (
	Version   = "1.0.0"
	BuildDate = "2026-10-01"
)

var (
	GlobalConfig string
	Counter      int
)

func run() {}
`
	items := ExtractSymbolsRegex(".go", code)
	found := make(map[string]string)
	for _, it := range items {
		found[it.Name] = it.Icon
	}

	if found["Version"] != "[C]" {
		t.Errorf("expected Version to be [C], got %q", found["Version"])
	}
	if found["BuildDate"] != "[C]" {
		t.Errorf("expected BuildDate to be [C], got %q", found["BuildDate"])
	}
	if found["GlobalConfig"] != "[V]" {
		t.Errorf("expected GlobalConfig to be [V], got %q", found["GlobalConfig"])
	}
	if found["Counter"] != "[V]" {
		t.Errorf("expected Counter to be [V], got %q", found["Counter"])
	}
	if found["run"] != "[F]" {
		t.Errorf("expected run to be [F], got %q", found["run"])
	}
}

func TestStructure_ConvertLSPSymbols_Children(t *testing.T) {
	syms := []lsp.DocumentSymbol{
		{
			Name: "MyClass",
			Kind: lsp.SymbolKindClass,
			Children: []lsp.DocumentSymbol{
				{
					Name: "MyField",
					Kind: lsp.SymbolKindField,
				},
				{
					Name: "MyMethod",
					Kind: lsp.SymbolKindMethod,
				},
			},
		},
	}

	items := ConvertLSPSymbols(syms)
	if len(items) != 3 {
		t.Fatalf("expected 3 items from recursive traversal, got %d", len(items))
	}
	if items[0].Name != "MyClass" || items[0].Icon != "[C]" {
		t.Errorf("unexpected item 0: %+v", items[0])
	}
	if items[1].Name != "MyField" || items[1].Icon != "[V]" {
		t.Errorf("unexpected item 1: %+v", items[1])
	}
	if items[2].Name != "MyMethod" || items[2].Icon != "[M]" {
		t.Errorf("unexpected item 2: %+v", items[2])
	}
}

func TestStructure_MergeSymbols_Deduplication(t *testing.T) {
	// LSP symbol has Line: 0 (from legacy format) while local regex has accurate Line: 12
	lspItems := []StructureItem{
		{Name: "Version", Kind: "Constant", Icon: "[C]", Line: 0},
		{Name: "Run", Kind: "Function", Icon: "[F]", Line: 25},
	}
	localItems := []StructureItem{
		{Name: "Version", Kind: "Constant", Icon: "[C]", Line: 12},
		{Name: "InternalVar", Kind: "Variable", Icon: "[V]", Line: 15},
	}

	merged := MergeSymbols(lspItems, localItems)
	// Must have exactly 3 items: Version, InternalVar, Run (no duplicates!)
	if len(merged) != 3 {
		t.Fatalf("expected 3 items, got %d: %+v", len(merged), merged)
	}

	// Verify Version has its line aligned to 12
	if merged[0].Name != "Version" || merged[0].Line != 12 {
		t.Errorf("expected Version at line 12, got %+v", merged[0])
	}
	if merged[1].Name != "InternalVar" || merged[1].Line != 15 {
		t.Errorf("expected InternalVar at line 15, got %+v", merged[1])
	}
	if merged[2].Name != "Run" || merged[2].Line != 25 {
		t.Errorf("expected Run at line 25, got %+v", merged[2])
	}
}

