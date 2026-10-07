package gogen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"tahr/internal/core/lsp"
)

// TagCase defines the casing convention for generated struct tag values.
type TagCase string

const (
	CaseSnake TagCase = "snake_case"
	CaseCamel TagCase = "camelCase"
	CaseKebab TagCase = "kebab-case"
	CaseLower TagCase = "lowercase"
	CaseKeep  TagCase = "keep"
)

// TagOptions configures struct tag manipulation operations.
type TagOptions struct {
	TagKeys      []string // e.g. ["json"], ["db"], ["yaml"], ["xml"]
	Casing       TagCase  // snake_case, camelCase, kebab-case, lowercase, keep
	OmitEmpty    bool     // if true, adds ",omitempty" to tags that support it
	Remove       bool     // if true, removes the specified TagKeys
	Overwrite    bool     // if true, updates existing tag value; if false, preserves
	ExportedOnly bool     // if true, only touches exported fields (default true)
	SkipEmbedded bool     // if true, ignores embedded struct fields (default true)
}

// DefaultTagOptions returns sensible defaults for adding struct tags.
func DefaultTagOptions(tag string) TagOptions {
	return TagOptions{
		TagKeys:      []string{tag},
		Casing:       CaseSnake,
		OmitEmpty:    false,
		Overwrite:    true,
		ExportedOnly: true,
		SkipEmbedded: true,
	}
}

// StructInfo holds detailed AST information about a parsed Go struct.
type StructInfo struct {
	Name         string
	ReceiverName string
	TypeParams   string // generic type params, e.g. "[T any, K comparable]"
	TypeArgs     string // type arguments for receiver/call, e.g. "[T, K]"
	StartLine    int    // 0-based LSP line
	EndLine      int    // 0-based LSP line
	StartOffset  int    // byte offset in source
	EndOffset    int    // byte offset in source (at closing brace)
	Fields       []FieldInfo
	Node         *ast.TypeSpec
	DeclNode     *ast.GenDecl
	StructType   *ast.StructType
	Fset         *token.FileSet
	File         *ast.File
}

// FieldInfo holds detailed metadata for each field in a struct.
type FieldInfo struct {
	Names        []string     // field names (can be multiple: "X, Y int")
	TypeStr      string       // formatted type expression, e.g. "*User", "[]string"
	Tag          string       // raw tag literal, e.g. `json:"id"`
	TagStartOff  int          // byte offset of tag literal start (-1 if none)
	TagEndOff    int          // byte offset of tag literal end (-1 if none)
	TagStartPos  lsp.Position // LSP 0-based position of tag start
	TagEndPos    lsp.Position // LSP 0-based position of tag end
	EndPos       lsp.Position // LSP 0-based position at end of field type
	EndOffset    int          // byte offset at end of field type
	IsExported   bool         // whether field is exported (upper case)
	IsEmbedded   bool         // whether field is embedded/anonymous
	IsPointer    bool         // whether field type is a pointer
	IsSlice      bool         // whether field type is a slice
	IsMap        bool         // whether field type is a map
}

// TagEntry represents a single key:"value" pair inside a struct tag literal.
type TagEntry struct {
	Key   string
	Value string
}

// ParseStructs parses all struct declarations found in Go source code.
func ParseStructs(src []byte) ([]*StructInfo, error) {
	fset := token.NewFileSet()
	file, offsetAdjustment, err := parseSourceWithFallback(fset, src)
	if err != nil {
		return nil, fmt.Errorf("gogen: failed to parse source: %w", err)
	}

	var results []*StructInfo
	ast.Inspect(file, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			return true
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			startPos := fset.Position(genDecl.Pos())
			endPos := fset.Position(structType.End())

			startOffset := startPos.Offset - offsetAdjustment
			endOffset := endPos.Offset - offsetAdjustment
			if startOffset < 0 {
				startOffset = 0
			}
			if endOffset > len(src) {
				endOffset = len(src)
			}

			startLSP := OffsetToLSP(src, startOffset)
			endLSP := OffsetToLSP(src, endOffset)

			// Extract type parameters if generic
			typeParams, typeArgs := extractTypeParams(typeSpec, fset)

			// Extract fields
			var fields []FieldInfo
			if structType.Fields != nil {
				for _, f := range structType.Fields.List {
					fi := extractFieldInfo(f, fset, src, offsetAdjustment)
					fields = append(fields, fi)
				}
			}

			structName := typeSpec.Name.Name
			receiverName := DefaultReceiverName(structName)

			results = append(results, &StructInfo{
				Name:         structName,
				ReceiverName: receiverName,
				TypeParams:   typeParams,
				TypeArgs:     typeArgs,
				StartLine:    startLSP.Line,
				EndLine:      endLSP.Line,
				StartOffset:  startOffset,
				EndOffset:    endOffset,
				Fields:       fields,
				Node:         typeSpec,
				DeclNode:     genDecl,
				StructType:   structType,
				Fset:         fset,
				File:         file,
			})
		}
		return true
	})

	return results, nil
}

// FindStructAtCursor returns the StructInfo enclosing the given 0-based LSP line and col.
func FindStructAtCursor(src []byte, line, col int) (*StructInfo, error) {
	structs, err := ParseStructs(src)
	if err != nil {
		return nil, err
	}
	for _, s := range structs {
		if line >= s.StartLine && line <= s.EndLine {
			return s, nil
		}
	}
	return nil, fmt.Errorf("no struct declaration found at line %d", line)
}

// FindStructByName finds a struct declaration matching the given name.
func FindStructByName(src []byte, name string) (*StructInfo, error) {
	structs, err := ParseStructs(src)
	if err != nil {
		return nil, err
	}
	for _, s := range structs {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("struct '%s' not found", name)
}

// ModifyTagsForStruct generates TextEdits to add, update, or remove struct tags on a named struct.
func ModifyTagsForStruct(src []byte, structName string, opts TagOptions) ([]lsp.TextEdit, error) {
	info, err := FindStructByName(src, structName)
	if err != nil {
		return nil, err
	}
	return generateTagEditsForStruct(src, info, opts)
}

// ModifyTagsAtCursor generates TextEdits to modify struct tags for the struct at the cursor.
func ModifyTagsAtCursor(src []byte, line, col int, opts TagOptions) ([]lsp.TextEdit, error) {
	info, err := FindStructAtCursor(src, line, col)
	if err != nil {
		return nil, err
	}
	return generateTagEditsForStruct(src, info, opts)
}

// AddTagsToStruct is a convenience method that returns modified source code with added tags.
func AddTagsToStruct(src []byte, structName string, tagKeys []string, casing TagCase, omitempty bool) ([]byte, error) {
	opts := TagOptions{
		TagKeys:      tagKeys,
		Casing:       casing,
		OmitEmpty:    omitempty,
		Remove:       false,
		Overwrite:    true,
		ExportedOnly: true,
		SkipEmbedded: true,
	}
	edits, err := ModifyTagsForStruct(src, structName, opts)
	if err != nil {
		return nil, err
	}
	return ApplyEdits(src, edits)
}

// RemoveTagsFromStruct is a convenience method that returns modified source with removed tags.
func RemoveTagsFromStruct(src []byte, structName string, tagKeys []string) ([]byte, error) {
	opts := TagOptions{
		TagKeys:      tagKeys,
		Remove:       true,
		ExportedOnly: false,
		SkipEmbedded: false,
	}
	edits, err := ModifyTagsForStruct(src, structName, opts)
	if err != nil {
		return nil, err
	}
	return ApplyEdits(src, edits)
}

// generateTagEditsForStruct calculates precise LSP text edits for all affected fields.
func generateTagEditsForStruct(src []byte, info *StructInfo, opts TagOptions) ([]lsp.TextEdit, error) {
	if info == nil {
		return nil, fmt.Errorf("gogen: struct info is nil")
	}

	var edits []lsp.TextEdit

	for _, field := range info.Fields {
		if opts.SkipEmbedded && field.IsEmbedded {
			continue
		}
		if opts.ExportedOnly && !field.IsExported {
			continue
		}

		fieldName := ""
		if len(field.Names) > 0 {
			fieldName = field.Names[0]
		}
		if fieldName == "" || fieldName == "_" {
			continue
		}

		currentEntries := ParseStructTag(field.Tag)
		updatedEntries := make([]TagEntry, len(currentEntries))
		copy(updatedEntries, currentEntries)

		if opts.Remove {
			for _, tagKey := range opts.TagKeys {
				updatedEntries = removeTagKey(updatedEntries, tagKey)
			}
		} else {
			for _, tagKey := range opts.TagKeys {
				tagVal := FormatTagKey(fieldName, opts.Casing)
				if opts.OmitEmpty && tagSupportsOmitEmpty(tagKey) {
					tagVal += ",omitempty"
				}
				updatedEntries = upsertTagKey(updatedEntries, tagKey, tagVal, opts.Overwrite, opts.OmitEmpty)
			}
		}

		newTagLiteral := FormatStructTags(updatedEntries)

		// Determine TextEdit for this field
		if field.Tag != "" {
			if newTagLiteral == "" {
				// Delete tag and preceding space up to end of type
				startPos := OffsetToLSP(src, field.EndOffset)
				endPos := OffsetToLSP(src, field.TagEndOff)
				edits = append(edits, lsp.TextEdit{
					Range: lsp.Range{
						Start: startPos,
						End:   endPos,
					},
					NewText: "",
				})
			} else if newTagLiteral != field.Tag {
				// Replace existing tag literal
				startPos := OffsetToLSP(src, field.TagStartOff)
				endPos := OffsetToLSP(src, field.TagEndOff)
				edits = append(edits, lsp.TextEdit{
					Range: lsp.Range{
						Start: startPos,
						End:   endPos,
					},
					NewText: newTagLiteral,
				})
			}
		} else if newTagLiteral != "" {
			// Field has no tag yet: insert space + tag at end of field type
			pos := OffsetToLSP(src, field.EndOffset)
			edits = append(edits, lsp.TextEdit{
				Range: lsp.Range{
					Start: pos,
					End:   pos,
				},
				NewText: " " + newTagLiteral,
			})
		}
	}

	return edits, nil
}

// ParseStructTag parses key:"value" pairs from a raw struct tag literal.
func ParseStructTag(raw string) []TagEntry {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "`") && strings.HasSuffix(raw, "`") && len(raw) >= 2 {
		raw = raw[1 : len(raw)-1]
	} else if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) && len(raw) >= 2 {
		raw = raw[1 : len(raw)-1]
	}
	if raw == "" {
		return nil
	}

	re := regexp.MustCompile(`([a-zA-Z0-9_\-]+):"([^"]*)"`)
	matches := re.FindAllStringSubmatchIndex(raw, -1)
	var entries []TagEntry
	for _, m := range matches {
		k := raw[m[2]:m[3]]
		v := raw[m[4]:m[5]]
		entries = append(entries, TagEntry{Key: k, Value: v})
	}
	return entries
}

// FormatStructTags builds standard Go backticked struct tag string from entries.
func FormatStructTags(entries []TagEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var parts []string
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf(`%s:"%s"`, e.Key, e.Value))
	}
	return "`" + strings.Join(parts, " ") + "`"
}

func upsertTagKey(entries []TagEntry, key, val string, overwrite, omitempty bool) []TagEntry {
	for i, e := range entries {
		if e.Key == key {
			if overwrite {
				entries[i].Value = val
			} else if omitempty && !strings.Contains(e.Value, "omitempty") && tagSupportsOmitEmpty(key) {
				entries[i].Value += ",omitempty"
			}
			return entries
		}
	}
	return append(entries, TagEntry{Key: key, Value: val})
}

func removeTagKey(entries []TagEntry, key string) []TagEntry {
	var result []TagEntry
	for _, e := range entries {
		if e.Key != key {
			result = append(result, e)
		}
	}
	return result
}

func tagSupportsOmitEmpty(tagKey string) bool {
	switch tagKey {
	case "json", "yaml", "xml", "toml":
		return true
	default:
		return false
	}
}

// FormatTagKey formats a field name according to the specified casing style.
func FormatTagKey(fieldName string, casing TagCase) string {
	words := splitIdentifierWords(fieldName)
	if len(words) == 0 {
		return strings.ToLower(fieldName)
	}

	switch casing {
	case CaseSnake:
		parts := make([]string, len(words))
		for i, w := range words {
			parts[i] = strings.ToLower(w)
		}
		return strings.Join(parts, "_")

	case CaseKebab:
		parts := make([]string, len(words))
		for i, w := range words {
			parts[i] = strings.ToLower(w)
		}
		return strings.Join(parts, "-")

	case CaseLower:
		var b strings.Builder
		for _, w := range words {
			b.WriteString(strings.ToLower(w))
		}
		return b.String()

	case CaseCamel:
		var b strings.Builder
		for i, w := range words {
			if i == 0 {
				b.WriteString(strings.ToLower(w))
			} else {
				if len(w) == 1 {
					b.WriteString(strings.ToUpper(w))
				} else {
					b.WriteString(strings.ToUpper(w[:1]) + strings.ToLower(w[1:]))
				}
			}
		}
		return b.String()

	case CaseKeep:
		return fieldName

	default:
		// Default to snake_case
		parts := make([]string, len(words))
		for i, w := range words {
			parts[i] = strings.ToLower(w)
		}
		return strings.Join(parts, "_")
	}
}

// splitIdentifierWords parses camelCase, PascalCase, or snake_case identifiers into individual words.
func splitIdentifierWords(s string) []string {
	s = strings.Trim(s, "_- ")
	if s == "" {
		return nil
	}

	var words []string
	runes := []rune(s)
	start := 0

	for i := 0; i < len(runes); i++ {
		if runes[i] == '_' || runes[i] == '-' {
			if i > start {
				words = append(words, string(runes[start:i]))
			}
			start = i + 1
			continue
		}

		if i > start && unicode.IsUpper(runes[i]) {
			prev := runes[i-1]
			// Between lower/digit and upper: "user[I]d" -> "user", "Id"
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				words = append(words, string(runes[start:i]))
				start = i
				continue
			}

			// Between multiple upper letters and upper followed by lower: "HTTP[S]erver" -> "HTTP", "Server"
			if i+1 < len(runes) && unicode.IsLower(runes[i+1]) && (i-start > 1) {
				words = append(words, string(runes[start:i]))
				start = i
				continue
			}
		}
	}

	if start < len(runes) {
		words = append(words, string(runes[start:]))
	}
	return words
}

// DefaultReceiverName computes standard Go receiver variable name (first letter lowercase).
func DefaultReceiverName(structName string) string {
	if structName == "" {
		return "s"
	}
	runes := []rune(structName)
	return strings.ToLower(string(runes[:1]))
}

// OffsetToLSP converts a 0-based byte offset into an LSP 0-based line and character coordinate.
func OffsetToLSP(src []byte, offset int) lsp.Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(src) {
		offset = len(src)
	}

	line := 0
	lineStart := 0
	for i := 0; i < offset; i++ {
		if src[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}

	charCount := 0
	sub := src[lineStart:offset]
	for len(sub) > 0 {
		r, size := utf8.DecodeRune(sub)
		sub = sub[size:]
		if r >= 0x10000 {
			charCount += 2 // UTF-16 surrogate pair
		} else {
			charCount++
		}
	}
	return lsp.Position{Line: line, Character: charCount}
}

// LSPToOffset converts an LSP 0-based line and character coordinate into a 0-based byte offset.
func LSPToOffset(src []byte, pos lsp.Position) int {
	currLine := 0
	lineStart := 0
	for i := 0; i < len(src) && currLine < pos.Line; i++ {
		if src[i] == '\n' {
			currLine++
			lineStart = i + 1
		}
	}
	if currLine < pos.Line {
		return len(src)
	}

	sub := src[lineStart:]
	currChar := 0
	offsetInLine := 0
	for offsetInLine < len(sub) && currChar < pos.Character {
		if sub[offsetInLine] == '\n' {
			break
		}
		r, size := utf8.DecodeRune(sub[offsetInLine:])
		if r >= 0x10000 {
			currChar += 2
		} else {
			currChar++
		}
		offsetInLine += size
	}
	return lineStart + offsetInLine
}

// ApplyEdits applies a slice of LSP TextEdits safely to a source buffer (bottom-to-top).
func ApplyEdits(src []byte, edits []lsp.TextEdit) ([]byte, error) {
	if len(edits) == 0 {
		return src, nil
	}

	type byteEdit struct {
		start   int
		end     int
		newText string
	}

	bEdits := make([]byteEdit, 0, len(edits))
	for _, e := range edits {
		start := LSPToOffset(src, e.Range.Start)
		end := LSPToOffset(src, e.Range.End)
		if start > end {
			start, end = end, start
		}
		if start < 0 {
			start = 0
		}
		if end > len(src) {
			end = len(src)
		}
		bEdits = append(bEdits, byteEdit{start: start, end: end, newText: e.NewText})
	}

	// Sort bottom-to-top (descending start byte offset)
	sort.Slice(bEdits, func(i, j int) bool {
		if bEdits[i].start != bEdits[j].start {
			return bEdits[i].start > bEdits[j].start
		}
		return bEdits[i].end > bEdits[j].end
	})

	res := make([]byte, len(src))
	copy(res, src)
	for _, be := range bEdits {
		prefix := res[:be.start]
		suffix := res[be.end:]
		buf := make([]byte, 0, len(prefix)+len(be.newText)+len(suffix))
		buf = append(buf, prefix...)
		buf = append(buf, be.newText...)
		buf = append(buf, suffix...)
		res = buf
	}
	return res, nil
}

// parseSourceWithFallback parses Go source code, providing a synthetic package header if needed.
func parseSourceWithFallback(fset *token.FileSet, src []byte) (*ast.File, int, error) {
	file, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
	if err == nil {
		return file, 0, nil
	}

	// If missing package clause, prepend synthetic header
	if strings.Contains(err.Error(), "expected 'package'") || !bytes.Contains(src, []byte("package ")) {
		syntheticHeader := "package main\n"
		combined := append([]byte(syntheticHeader), src...)
		file, altErr := parser.ParseFile(fset, "src.go", combined, parser.ParseComments)
		if altErr == nil {
			return file, len(syntheticHeader), nil
		}
	}

	return nil, 0, err
}

func extractTypeParams(ts *ast.TypeSpec, fset *token.FileSet) (paramsStr, argsStr string) {
	if ts.TypeParams == nil || len(ts.TypeParams.List) == 0 {
		return "", ""
	}

	var pBuf bytes.Buffer
	pBuf.WriteString("[")
	var args []string

	first := true
	for _, field := range ts.TypeParams.List {
		if !first {
			pBuf.WriteString(", ")
		}
		first = false

		var names []string
		for _, name := range field.Names {
			names = append(names, name.Name)
			args = append(args, name.Name)
		}
		pBuf.WriteString(strings.Join(names, ", "))
		pBuf.WriteString(" ")
		_ = printer.Fprint(&pBuf, fset, field.Type)
	}
	pBuf.WriteString("]")

	argsStr = "[" + strings.Join(args, ", ") + "]"
	return pBuf.String(), argsStr
}

func extractFieldInfo(f *ast.Field, fset *token.FileSet, src []byte, offsetAdjustment int) FieldInfo {
	var names []string
	isExported := false
	isEmbedded := len(f.Names) == 0

	if isEmbedded {
		name := extractTypeName(f.Type)
		names = append(names, name)
		if len(name) > 0 && unicode.IsUpper([]rune(name)[0]) {
			isExported = true
		}
	} else {
		for _, ident := range f.Names {
			names = append(names, ident.Name)
			if ident.IsExported() {
				isExported = true
			}
		}
	}

	var typeBuf bytes.Buffer
	_ = printer.Fprint(&typeBuf, fset, f.Type)
	typeStr := typeBuf.String()

	typeEndPos := fset.Position(f.Type.End())
	endOffset := typeEndPos.Offset - offsetAdjustment
	if endOffset < 0 {
		endOffset = 0
	}
	if endOffset > len(src) {
		endOffset = len(src)
	}
	endLSP := OffsetToLSP(src, endOffset)

	tag := ""
	tagStartOff := -1
	tagEndOff := -1
	var tagStartLSP, tagEndLSP lsp.Position

	if f.Tag != nil {
		tag = f.Tag.Value
		tStart := fset.Position(f.Tag.Pos()).Offset - offsetAdjustment
		tEnd := fset.Position(f.Tag.End()).Offset - offsetAdjustment
		if tStart >= 0 && tEnd <= len(src) {
			tagStartOff = tStart
			tagEndOff = tEnd
			tagStartLSP = OffsetToLSP(src, tagStartOff)
			tagEndLSP = OffsetToLSP(src, tagEndOff)
		}
	}

	isPointer := strings.HasPrefix(typeStr, "*")
	isSlice := strings.HasPrefix(typeStr, "[]")
	isMap := strings.HasPrefix(typeStr, "map[")

	return FieldInfo{
		Names:        names,
		TypeStr:      typeStr,
		Tag:          tag,
		TagStartOff:  tagStartOff,
		TagEndOff:    tagEndOff,
		TagStartPos:  tagStartLSP,
		TagEndPos:    tagEndLSP,
		EndPos:       endLSP,
		EndOffset:    endOffset,
		IsExported:   isExported,
		IsEmbedded:   isEmbedded,
		IsPointer:    isPointer,
		IsSlice:      isSlice,
		IsMap:        isMap,
	}
}

func extractTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return extractTypeName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

// GenerateQuickFixActions provides Quick Fix menu actions when cursor is inside a Go struct declaration.
// Actions offered:
//   - "Add JSON Tags"
//   - "Add DB Tags"
//   - "Generate Constructor"
//   - "Generate Getters/Setters"
//   - "Generate Clone / DeepCopy"
func GenerateQuickFixActions(uri string, src []byte, line, col int) []lsp.CodeAction {
	info, err := FindStructAtCursor(src, line, col)
	if err != nil || info == nil {
		return nil
	}

	var actions []lsp.CodeAction

	// 1. Add JSON Tags
	jsonEdits, err := ModifyTagsForStruct(src, info.Name, TagOptions{
		TagKeys:      []string{"json"},
		Casing:       CaseSnake,
		OmitEmpty:    false,
		Overwrite:    true,
		ExportedOnly: true,
		SkipEmbedded: true,
	})
	if err == nil && len(jsonEdits) > 0 {
		actions = append(actions, lsp.CodeAction{
			Title: "Add JSON Tags",
			Kind:  "quickfix",
			Edit: &lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					uri: jsonEdits,
				},
			},
		})
	}

	// 2. Add DB Tags
	dbEdits, err := ModifyTagsForStruct(src, info.Name, TagOptions{
		TagKeys:      []string{"db"},
		Casing:       CaseSnake,
		OmitEmpty:    false,
		Overwrite:    true,
		ExportedOnly: true,
		SkipEmbedded: true,
	})
	if err == nil && len(dbEdits) > 0 {
		actions = append(actions, lsp.CodeAction{
			Title: "Add DB Tags",
			Kind:  "quickfix",
			Edit: &lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					uri: dbEdits,
				},
			},
		})
	}

	// 3. Generate Constructor
	ctorEdits, err := GenerateConstructorEdits(src, info.Name, DefaultConstructorOptions(info.Name))
	if err == nil && len(ctorEdits) > 0 {
		actions = append(actions, lsp.CodeAction{
			Title: "Generate Constructor",
			Kind:  "refactor",
			Edit: &lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					uri: ctorEdits,
				},
			},
		})
	}

	// 4. Generate Getters/Setters
	gsEdits, err := GenerateGetSetEdits(src, info.Name, DefaultGetSetOptions(info))
	if err == nil && len(gsEdits) > 0 {
		actions = append(actions, lsp.CodeAction{
			Title: "Generate Getters/Setters",
			Kind:  "refactor",
			Edit: &lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					uri: gsEdits,
				},
			},
		})
	}

	// 5. Generate Clone / DeepCopy
	cloneEdits, err := GenerateDeepCopyEdits(src, info.Name, DefaultDeepCopyOptions(info))
	if err == nil && len(cloneEdits) > 0 {
		actions = append(actions, lsp.CodeAction{
			Title: "Generate Clone / DeepCopy",
			Kind:  "refactor",
			Edit: &lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					uri: cloneEdits,
				},
			},
		})
	}

	// 6. Add YAML Tags
	yamlEdits, err := ModifyTagsForStruct(src, info.Name, TagOptions{
		TagKeys:      []string{"yaml"},
		Casing:       CaseSnake,
		OmitEmpty:    false,
		Overwrite:    true,
		ExportedOnly: true,
		SkipEmbedded: true,
	})
	if err == nil && len(yamlEdits) > 0 {
		actions = append(actions, lsp.CodeAction{
			Title: "Add YAML Tags",
			Kind:  "quickfix",
			Edit: &lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					uri: yamlEdits,
				},
			},
		})
	}

	// 7. Remove Struct Tags
	removeEdits, err := ModifyTagsForStruct(src, info.Name, TagOptions{
		TagKeys:      []string{"json", "db", "yaml", "xml"},
		Remove:       true,
		ExportedOnly: false,
		SkipEmbedded: false,
	})
	if err == nil && len(removeEdits) > 0 {
		actions = append(actions, lsp.CodeAction{
			Title: "Remove Struct Tags",
			Kind:  "quickfix",
			Edit: &lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					uri: removeEdits,
				},
			},
		})
	}

	return actions
}

