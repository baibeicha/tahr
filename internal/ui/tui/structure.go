package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/lsp"
	"tahr/internal/ui"
)

// StructureItem represents a code symbol in the Document Structure outline.
type StructureItem struct {
	Name   string
	Kind   string
	Icon   string // e.g. "[F]", "[S]", "[M]", "[I]", "[T]", "[C]", "[V]"
	Line   int    // 0-indexed line number
	Col    int    // 0-indexed column
	Detail string
}

// StructurePanel manages the document structure outline view and interactions.
type StructurePanel struct {
	Items    []StructureItem
	Selected int
	ScrollY  int
}

// NewStructurePanel creates a new StructurePanel.
func NewStructurePanel() *StructurePanel {
	return &StructurePanel{
		Items:    make([]StructureItem, 0),
		Selected: 0,
		ScrollY:  0,
	}
}

// SetItems updates the structure symbols and resets selection if needed.
func (sp *StructurePanel) SetItems(items []StructureItem) {
	sp.Items = items
	if sp.Selected >= len(items) {
		sp.Selected = max(0, len(items)-1)
	}
}

// HandleKey handles keyboard navigation inside the Structure panel.
func (sp *StructurePanel) HandleKey(k input.Key) (handled bool, jumpTarget *StructureItem) {
	if len(sp.Items) == 0 {
		return false, nil
	}

	switch k.Type {
	case input.KeyUp:
		if sp.Selected > 0 {
			sp.Selected--
			if sp.Selected < sp.ScrollY {
				sp.ScrollY = sp.Selected
			}
		}
		return true, nil

	case input.KeyDown:
		if sp.Selected < len(sp.Items)-1 {
			sp.Selected++
		}
		return true, nil

	case input.KeyHome:
		sp.Selected = 0
		sp.ScrollY = 0
		return true, nil

	case input.KeyEnd:
		sp.Selected = len(sp.Items) - 1
		return true, nil

	case input.KeyEnter:
		if sp.Selected >= 0 && sp.Selected < len(sp.Items) {
			item := sp.Items[sp.Selected]
			return true, &item
		}
	}

	return false, nil
}

// HandleClick processes mouse selection on a row inside the sidebar.
func (sp *StructurePanel) HandleClick(rowInPanel int) *StructureItem {
	idx := sp.ScrollY + rowInPanel
	if idx >= 0 && idx < len(sp.Items) {
		sp.Selected = idx
		item := sp.Items[idx]
		return &item
	}
	return nil
}

// Render draws the document structure symbol list.
func (sp *StructurePanel) Render(buf *buffer.Buffer, startX, startY, width, height int, focused bool, theme *ui.Theme) {
	if buf == nil || width <= 0 || height <= 0 {
		return
	}

	bg := toColor(theme.GutterBg)
	fg := toColor(theme.Foreground)
	selBg := toColor(theme.SelectionBg)
	selFg := toColor(theme.Foreground)
	commentFg := toColor(theme.Comment)

	// Ensure selected item is visible within viewport
	if sp.Selected >= sp.ScrollY+height {
		sp.ScrollY = sp.Selected - height + 1
	}
	if sp.Selected < sp.ScrollY {
		sp.ScrollY = sp.Selected
	}

	for row := 0; row < height; row++ {
		screenY := startY + row
		itemIdx := sp.ScrollY + row

		if itemIdx >= len(sp.Items) {
			for x := 0; x < width; x++ {
				buf.SetRune(startX+x, screenY, ' ', fg, bg, cell.AttrNone)
			}
			continue
		}

		item := sp.Items[itemIdx]
		isSel := (itemIdx == sp.Selected && focused)

		rowBg := bg
		rowFg := fg
		attr := cell.AttrNone
		if isSel {
			rowBg = selBg
			rowFg = selFg
			attr = cell.AttrBold
		}

		// Icon coloring
		iconFg := toColor(theme.Function)
		switch item.Icon {
		case "[F]":
			iconFg = toColor(theme.Function)
		case "[M]":
			iconFg = toColor(theme.Function)
		case "[S]":
			iconFg = toColor(theme.Type)
		case "[I]":
			iconFg = toColor(theme.Type)
		case "[T]":
			iconFg = toColor(theme.Type)
		case "[C]":
			iconFg = toColor(theme.Constant)
		case "[V]":
			iconFg = toColor(theme.Keyword)
		}

		// 1. Draw Icon
		col := 0
		iconRunes := []rune(item.Icon)
		for _, r := range iconRunes {
			if col < width {
				buf.SetRune(startX+col, screenY, r, iconFg, rowBg, attr)
				col++
			}
		}

		// 2. Space
		if col < width {
			buf.SetRune(startX+col, screenY, ' ', rowFg, rowBg, cell.AttrNone)
			col++
		}

		// 3. Name
		nameRunes := []rune(item.Name)
		for _, r := range nameRunes {
			if col < width {
				buf.SetRune(startX+col, screenY, r, rowFg, rowBg, attr)
				col++
			}
		}

		// 4. Line number annotation on right
		lineStr := fmt.Sprintf(":%d", item.Line+1)
		lineRunes := []rune(lineStr)
		lineStart := width - len(lineRunes) - 1
		if lineStart > col+1 {
			for x := col; x < lineStart; x++ {
				buf.SetRune(startX+x, screenY, ' ', rowFg, rowBg, cell.AttrNone)
			}
			for i, r := range lineRunes {
				buf.SetRune(startX+lineStart+i, screenY, r, commentFg, rowBg, cell.AttrNone)
			}
			col = lineStart + len(lineRunes)
		}

		// Pad remainder of row
		for x := col; x < width; x++ {
			buf.SetRune(startX+x, screenY, ' ', rowFg, rowBg, cell.AttrNone)
		}
	}
}

// ConvertLSPSymbols transforms raw LSP DocumentSymbol slice into StructureItem slice.
func ConvertLSPSymbols(symbols []lsp.DocumentSymbol) []StructureItem {
	var items []StructureItem
	var walk func(syms []lsp.DocumentSymbol)
	walk = func(syms []lsp.DocumentSymbol) {
		for _, s := range syms {
			icon := "[?]"
			kindStr := "Symbol"

			switch s.Kind {
			case lsp.SymbolKindFunction:
				icon = "[F]"
				kindStr = "Function"
			case lsp.SymbolKindMethod:
				icon = "[M]"
				kindStr = "Method"
			case lsp.SymbolKindStruct:
				icon = "[S]"
				kindStr = "Struct"
			case lsp.SymbolKindInterface:
				icon = "[I]"
				kindStr = "Interface"
			case lsp.SymbolKindClass:
				icon = "[C]"
				kindStr = "Class"
			case lsp.SymbolKindTypeParam, lsp.SymbolKindEnum:
				icon = "[T]"
				kindStr = "Type"
			case lsp.SymbolKindConstant:
				icon = "[C]"
				kindStr = "Constant"
			case lsp.SymbolKindVariable, lsp.SymbolKindField, lsp.SymbolKindProperty:
				icon = "[V]"
				kindStr = "Variable"
			case lsp.SymbolKindPackage, lsp.SymbolKindModule:
				icon = "[P]"
				kindStr = "Package"
			}

			targetLine := s.SelectionRange.Start.Line
			targetCol := s.SelectionRange.Start.Character
			if targetLine == 0 && targetCol == 0 && (s.Range.Start.Line != 0 || s.Range.Start.Character != 0) {
				targetLine = s.Range.Start.Line
				targetCol = s.Range.Start.Character
			}

			items = append(items, StructureItem{
				Name:   s.Name,
				Kind:   kindStr,
				Icon:   icon,
				Line:   targetLine,
				Col:    targetCol,
				Detail: s.Detail,
			})

			if len(s.Children) > 0 {
				walk(s.Children)
			}
		}
	}
	walk(symbols)
	return items
}

// MergeSymbols combines LSP symbols with local regex/AST symbols, preserving variables and constants without duplicates.
func MergeSymbols(lspItems []StructureItem, localItems []StructureItem) []StructureItem {
	if len(lspItems) == 0 {
		return localItems
	}
	if len(localItems) == 0 {
		return lspItems
	}

	// Map local items by name for accurate line lookup
	localByName := make(map[string]StructureItem)
	for _, loc := range localItems {
		localByName[loc.Name] = loc
	}

	seenNames := make(map[string]bool)
	var merged []StructureItem

	// 1. Add all LSP items with line sanity checks
	for _, it := range lspItems {
		if seenNames[it.Name] {
			continue
		}
		item := it
		// If LSP reported line 0, but local regex located the identifier further down, adopt accurate line
		if item.Line == 0 {
			if loc, ok := localByName[item.Name]; ok && loc.Line > 0 {
				item.Line = loc.Line
				item.Col = loc.Col
			}
		}
		seenNames[item.Name] = true
		merged = append(merged, item)
	}

	// 2. Add local variables, constants, or types not reported by LSP
	for _, it := range localItems {
		if !seenNames[it.Name] {
			seenNames[it.Name] = true
			merged = append(merged, it)
		}
	}

	// 3. Sort by line number ascending
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Line == merged[j].Line {
			return merged[i].Col < merged[j].Col
		}
		return merged[i].Line < merged[j].Line
	})

	return merged
}

// ExtractSymbolsRegex parses source text using language-aware regex when LSP is not active.
func ExtractSymbolsRegex(ext string, content string) []StructureItem {
	var items []StructureItem
	lines := strings.Split(content, "\n")

	switch strings.ToLower(ext) {
	case ".go":
		reMethod := regexp.MustCompile(`^func\s+\([^)]+\)\s+([A-Za-z0-9_]+)`)
		reFunc := regexp.MustCompile(`^func\s+([A-Za-z0-9_]+)`)
		reStruct := regexp.MustCompile(`^type\s+([A-Za-z0-9_]+)\s+struct`)
		reInterface := regexp.MustCompile(`^type\s+([A-Za-z0-9_]+)\s+interface`)
		reType := regexp.MustCompile(`^type\s+([A-Za-z0-9_]+)\s+`)
		reBlockIdent := regexp.MustCompile(`^([A-Za-z0-9_]+)`)

		inConstBlock := false
		inVarBlock := false

		for lineIdx, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}

			// Block boundaries
			if strings.HasPrefix(trimmed, "const (") || trimmed == "const (" || strings.HasPrefix(trimmed, "const(") || trimmed == "const(" {
				inConstBlock = true
				continue
			}
			if strings.HasPrefix(trimmed, "var (") || trimmed == "var (" || strings.HasPrefix(trimmed, "var(") || trimmed == "var(" {
				inVarBlock = true
				continue
			}
			if (inConstBlock || inVarBlock) && (trimmed == ")" || strings.HasPrefix(trimmed, ")")) {
				inConstBlock = false
				inVarBlock = false
				continue
			}

			if inConstBlock {
				parts := strings.Split(trimmed, "=")
				identParts := strings.Split(parts[0], ",")
				for _, ip := range identParts {
					cleanIdent := strings.TrimSpace(ip)
					if idx := strings.IndexAny(cleanIdent, " \t"); idx != -1 {
						cleanIdent = cleanIdent[:idx]
					}
					if m := reBlockIdent.FindStringSubmatch(cleanIdent); len(m) > 1 {
						items = append(items, StructureItem{Name: m[1], Kind: "Constant", Icon: "[C]", Line: lineIdx})
					}
				}
				continue
			}
			if inVarBlock {
				parts := strings.Split(trimmed, "=")
				identParts := strings.Split(parts[0], ",")
				for _, ip := range identParts {
					cleanIdent := strings.TrimSpace(ip)
					if idx := strings.IndexAny(cleanIdent, " \t"); idx != -1 {
						cleanIdent = cleanIdent[:idx]
					}
					if m := reBlockIdent.FindStringSubmatch(cleanIdent); len(m) > 1 {
						items = append(items, StructureItem{Name: m[1], Kind: "Variable", Icon: "[V]", Line: lineIdx})
					}
				}
				continue
			}

			if m := reMethod.FindStringSubmatch(trimmed); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Method", Icon: "[M]", Line: lineIdx})
			} else if m := reFunc.FindStringSubmatch(trimmed); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Function", Icon: "[F]", Line: lineIdx})
			} else if m := reStruct.FindStringSubmatch(trimmed); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Struct", Icon: "[S]", Line: lineIdx})
			} else if m := reInterface.FindStringSubmatch(trimmed); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Interface", Icon: "[I]", Line: lineIdx})
			} else if m := reType.FindStringSubmatch(trimmed); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Type", Icon: "[T]", Line: lineIdx})
			} else if strings.HasPrefix(trimmed, "const ") {
				constPart := strings.TrimPrefix(trimmed, "const ")
				if eqIdx := strings.Index(constPart, "="); eqIdx != -1 {
					constPart = constPart[:eqIdx]
				}
				for _, ip := range strings.Split(constPart, ",") {
					cleanIdent := strings.TrimSpace(ip)
					if idx := strings.IndexAny(cleanIdent, " \t"); idx != -1 {
						cleanIdent = cleanIdent[:idx]
					}
					if m := reBlockIdent.FindStringSubmatch(cleanIdent); len(m) > 1 {
						items = append(items, StructureItem{Name: m[1], Kind: "Constant", Icon: "[C]", Line: lineIdx})
					}
				}
			} else if strings.HasPrefix(trimmed, "var ") {
				varPart := strings.TrimPrefix(trimmed, "var ")
				if eqIdx := strings.Index(varPart, "="); eqIdx != -1 {
					varPart = varPart[:eqIdx]
				}
				for _, ip := range strings.Split(varPart, ",") {
					cleanIdent := strings.TrimSpace(ip)
					if idx := strings.IndexAny(cleanIdent, " \t"); idx != -1 {
						cleanIdent = cleanIdent[:idx]
					}
					if m := reBlockIdent.FindStringSubmatch(cleanIdent); len(m) > 1 {
						items = append(items, StructureItem{Name: m[1], Kind: "Variable", Icon: "[V]", Line: lineIdx})
					}
				}
			}
		}

	case ".py":
		reFunc := regexp.MustCompile(`^\s*def\s+([A-Za-z0-9_]+)`)
		reClass := regexp.MustCompile(`^\s*class\s+([A-Za-z0-9_]+)`)

		for lineIdx, line := range lines {
			if m := reClass.FindStringSubmatch(line); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Class", Icon: "[C]", Line: lineIdx})
			} else if m := reFunc.FindStringSubmatch(line); len(m) > 1 {
				icon := "[F]"
				if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
					icon = "[M]"
				}
				items = append(items, StructureItem{Name: m[1], Kind: "Function", Icon: icon, Line: lineIdx})
			}
		}

	case ".rs":
		reFn := regexp.MustCompile(`^\s*(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z0-9_]+)`)
		reStruct := regexp.MustCompile(`^\s*(?:pub\s+)?struct\s+([A-Za-z0-9_]+)`)
		reTrait := regexp.MustCompile(`^\s*(?:pub\s+)?trait\s+([A-Za-z0-9_]+)`)
		reImpl := regexp.MustCompile(`^\s*impl(?:\s+<[^>]+>)?\s+([A-Za-z0-9_]+)`)

		for lineIdx, line := range lines {
			if m := reStruct.FindStringSubmatch(line); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Struct", Icon: "[S]", Line: lineIdx})
			} else if m := reTrait.FindStringSubmatch(line); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Trait", Icon: "[I]", Line: lineIdx})
			} else if m := reImpl.FindStringSubmatch(line); len(m) > 1 {
				items = append(items, StructureItem{Name: "impl " + m[1], Kind: "Impl", Icon: "[M]", Line: lineIdx})
			} else if m := reFn.FindStringSubmatch(line); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Function", Icon: "[F]", Line: lineIdx})
			}
		}

	default: // JavaScript, TypeScript, etc.
		reFunc := regexp.MustCompile(`(?:function\s+([A-Za-z0-9_]+)|const\s+([A-Za-z0-9_]+)\s*=\s*(?:async\s*)?\([^)]*\)\s*=>)`)
		reClass := regexp.MustCompile(`class\s+([A-Za-z0-9_]+)`)

		for lineIdx, line := range lines {
			if m := reClass.FindStringSubmatch(line); len(m) > 1 {
				items = append(items, StructureItem{Name: m[1], Kind: "Class", Icon: "[C]", Line: lineIdx})
			} else if m := reFunc.FindStringSubmatch(line); len(m) > 1 {
				name := m[1]
				if name == "" {
					name = m[2]
				}
				if name != "" {
					items = append(items, StructureItem{Name: name, Kind: "Function", Icon: "[F]", Line: lineIdx})
				}
			}
		}
	}

	return items
}
