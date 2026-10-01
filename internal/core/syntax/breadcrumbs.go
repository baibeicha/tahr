package syntax

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// BreadcrumbItem represents a component of the current scope hierarchy.
type BreadcrumbItem struct {
	Kind string // "dir", "file", "package", "struct", "class", "func", "heading"
	Name string
	Icon string
	Line int
}

var (
	goPackageRegex = regexp.MustCompile(`^package\s+([A-Za-z0-9_]+)`)
	goTypeRegex    = regexp.MustCompile(`^type\s+([A-Za-z0-9_]+)\s+(struct|interface)`)
	goFuncRegex    = regexp.MustCompile(`^func\s+(?:\(\s*[^)]+\s*\)\s*)?([A-Za-z0-9_]+)\s*\(`)
	pyClassRegex   = regexp.MustCompile(`^class\s+([A-Za-z0-9_]+)`)
	pyFuncRegex    = regexp.MustCompile(`^\s*def\s+([A-Za-z0-9_]+)\s*\(`)
	mdHeadingRegex = regexp.MustCompile(`^(#{1,6})\s+(.+)`)
)

// ExtractBreadcrumbs computes the symbol hierarchy enclosing the given cursor line.
func ExtractBreadcrumbs(filePath string, lines []string, cursorLine int) []BreadcrumbItem {
	var items []BreadcrumbItem

	if filePath != "" {
		clean := filepath.Clean(filePath)
		dir := filepath.Dir(clean)
		base := filepath.Base(clean)

		if dir != "." && dir != "/" && dir != "\\" {
			items = append(items, BreadcrumbItem{
				Kind: "dir",
				Name: filepath.ToSlash(dir),
				Icon: "dir",
				Line: 0,
			})
		}
		items = append(items, BreadcrumbItem{
			Kind: "file",
			Name: base,
			Icon: "file",
			Line: 0,
		})
	}

	if len(lines) == 0 || cursorLine < 0 {
		return items
	}
	if cursorLine >= len(lines) {
		cursorLine = len(lines) - 1
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		items = append(items, extractGoSymbols(lines, cursorLine)...)
	case ".py":
		items = append(items, extractPythonSymbols(lines, cursorLine)...)
	case ".md", ".markdown":
		items = append(items, extractMarkdownSymbols(lines, cursorLine)...)
	default:
		items = append(items, extractGenericSymbols(lines, cursorLine)...)
	}

	return items
}

func extractGoSymbols(lines []string, cursorLine int) []BreadcrumbItem {
	var symbols []BreadcrumbItem
	var pkgItem *BreadcrumbItem
	var enclosingType *BreadcrumbItem
	var enclosingFunc *BreadcrumbItem

	for i := 0; i <= cursorLine; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		if pkgMatches := goPackageRegex.FindStringSubmatch(line); len(pkgMatches) > 1 {
			pkgItem = &BreadcrumbItem{
				Kind: "package",
				Name: pkgMatches[1],
				Icon: "pkg",
				Line: i,
			}
		}

		if typeMatches := goTypeRegex.FindStringSubmatch(line); len(typeMatches) > 2 {
			enclosingType = &BreadcrumbItem{
				Kind: typeMatches[2],
				Name: typeMatches[1],
				Icon: "type",
				Line: i,
			}
		}

		if funcMatches := goFuncRegex.FindStringSubmatch(line); len(funcMatches) > 1 {
			name := funcMatches[1]
			// Check if receiver method
			if strings.Contains(line, ") " + name) {
				enclosingFunc = &BreadcrumbItem{
					Kind: "method",
					Name: name + "()",
					Icon: "fn",
					Line: i,
				}
			} else {
				enclosingFunc = &BreadcrumbItem{
					Kind: "func",
					Name: name + "()",
					Icon: "fn",
					Line: i,
				}
			}
		}
	}

	if pkgItem != nil {
		symbols = append(symbols, *pkgItem)
	}
	if enclosingType != nil {
		symbols = append(symbols, *enclosingType)
	}
	if enclosingFunc != nil {
		symbols = append(symbols, *enclosingFunc)
	}

	return symbols
}

func extractPythonSymbols(lines []string, cursorLine int) []BreadcrumbItem {
	var symbols []BreadcrumbItem
	var lastClass *BreadcrumbItem
	var lastFunc *BreadcrumbItem

	for i := 0; i <= cursorLine; i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if m := pyClassRegex.FindStringSubmatch(trimmed); len(m) > 1 {
			lastClass = &BreadcrumbItem{
				Kind: "class",
				Name: m[1],
				Icon: "class",
				Line: i,
			}
		}

		if m := pyFuncRegex.FindStringSubmatch(raw); len(m) > 1 {
			lastFunc = &BreadcrumbItem{
				Kind: "func",
				Name: m[1] + "()",
				Icon: "fn",
				Line: i,
			}
		}
	}

	if lastClass != nil {
		symbols = append(symbols, *lastClass)
	}
	if lastFunc != nil {
		symbols = append(symbols, *lastFunc)
	}

	return symbols
}

func extractMarkdownSymbols(lines []string, cursorLine int) []BreadcrumbItem {
	var symbols []BreadcrumbItem
	for i := 0; i <= cursorLine; i++ {
		line := strings.TrimSpace(lines[i])
		if m := mdHeadingRegex.FindStringSubmatch(line); len(m) > 2 {
			symbols = append(symbols, BreadcrumbItem{
				Kind: "heading",
				Name: m[2],
				Icon: "§",
				Line: i,
			})
			if len(symbols) > 2 {
				symbols = symbols[len(symbols)-2:]
			}
		}
	}
	return symbols
}

func extractGenericSymbols(lines []string, cursorLine int) []BreadcrumbItem {
	var symbols []BreadcrumbItem
	funcGeneric := regexp.MustCompile(`(?:function|def|fn|func)\s+([A-Za-z0-9_]+)`)
	for i := 0; i <= cursorLine; i++ {
		line := strings.TrimSpace(lines[i])
		if m := funcGeneric.FindStringSubmatch(line); len(m) > 1 {
			symbols = []BreadcrumbItem{{
				Kind: "func",
				Name: m[1] + "()",
				Icon: "fn",
				Line: i,
			}}
		}
	}
	return symbols
}

// FormatBreadcrumbs formats a list of breadcrumb items into a display string.
func FormatBreadcrumbs(items []BreadcrumbItem) string {
	if len(items) == 0 {
		return ""
	}
	var parts []string
	for _, it := range items {
		if it.Icon != "" {
			parts = append(parts, fmt.Sprintf("%s %s", it.Icon, it.Name))
		} else {
			parts = append(parts, it.Name)
		}
	}
	return strings.Join(parts, " › ")
}
