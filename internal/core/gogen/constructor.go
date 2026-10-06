package gogen

import (
	"fmt"
	"strings"
	"unicode"

	"tahr/internal/core/lsp"
)

// ConstructorOptions configures constructor function generation.
type ConstructorOptions struct {
	Name          string // constructor function name (e.g. "NewUser", default "New" + StructName)
	ReturnPointer bool   // return *StructName (true) or StructName (false), default true
	ExportedOnly  bool   // only include exported fields in constructor signature
	SkipEmbedded  bool   // skip embedded fields
	DocComment    bool   // include doc comment (e.g. "// NewUser returns a new User instance.")
}

// DefaultConstructorOptions returns standard constructor generation options.
func DefaultConstructorOptions(structName string) ConstructorOptions {
	return ConstructorOptions{
		Name:          "New" + structName,
		ReturnPointer: true,
		ExportedOnly:  false,
		SkipEmbedded:  false,
		DocComment:    true,
	}
}

// GenerateConstructorCode produces the Go source string for a typed constructor function.
func GenerateConstructorCode(info *StructInfo, opts ConstructorOptions) string {
	if info == nil {
		return ""
	}

	fnName := opts.Name
	if fnName == "" {
		fnName = "New" + info.Name
	}

	typeParams := info.TypeParams // e.g. "[T any, K comparable]"
	typeArgs := info.TypeArgs     // e.g. "[T, K]"

	var params []string
	var assignments []string
	usedParamNames := make(map[string]int)

	for _, field := range info.Fields {
		if opts.SkipEmbedded && field.IsEmbedded {
			continue
		}
		if opts.ExportedOnly && !field.IsExported {
			continue
		}

		for _, name := range field.Names {
			if name == "" || name == "_" {
				continue
			}

			pName := sanitizeParamName(name)
			if count, exists := usedParamNames[pName]; exists {
				usedParamNames[pName] = count + 1
				pName = fmt.Sprintf("%s%d", pName, count+1)
			} else {
				usedParamNames[pName] = 1
			}

			params = append(params, fmt.Sprintf("%s %s", pName, field.TypeStr))
			assignments = append(assignments, fmt.Sprintf("\t\t%s: %s,", name, pName))
		}
	}

	retType := info.Name + typeArgs
	retValPrefix := ""
	if opts.ReturnPointer {
		retType = "*" + retType
		retValPrefix = "&"
	}

	var sb strings.Builder

	if opts.DocComment {
		sb.WriteString(fmt.Sprintf("// %s creates and initializes a new %s instance.\n", fnName, info.Name))
	}

	paramsJoined := strings.Join(params, ", ")
	sb.WriteString(fmt.Sprintf("func %s%s(%s) %s {\n", fnName, typeParams, paramsJoined, retType))

	if len(assignments) == 0 {
		sb.WriteString(fmt.Sprintf("\treturn %s%s%s{}\n", retValPrefix, info.Name, typeArgs))
	} else {
		sb.WriteString(fmt.Sprintf("\treturn %s%s%s{\n", retValPrefix, info.Name, typeArgs))
		for _, a := range assignments {
			sb.WriteString(a)
			sb.WriteString("\n")
		}
		sb.WriteString("\t}\n")
	}

	sb.WriteString("}")
	return sb.String()
}

// GenerateConstructorEdits generates LSP TextEdits inserting the constructor directly after the struct.
func GenerateConstructorEdits(src []byte, structName string, opts ConstructorOptions) ([]lsp.TextEdit, error) {
	info, err := FindStructByName(src, structName)
	if err != nil {
		return nil, err
	}

	code := GenerateConstructorCode(info, opts)
	if code == "" {
		return nil, fmt.Errorf("gogen: empty constructor generated for %s", structName)
	}

	pos := OffsetToLSP(src, info.EndOffset)
	edit := lsp.TextEdit{
		Range: lsp.Range{
			Start: pos,
			End:   pos,
		},
		NewText: "\n\n" + code,
	}

	return []lsp.TextEdit{edit}, nil
}

// GenerateConstructor returns modified source code with the constructor function appended.
func GenerateConstructor(src []byte, structName string, opts ConstructorOptions) ([]byte, error) {
	edits, err := GenerateConstructorEdits(src, structName, opts)
	if err != nil {
		return nil, err
	}
	return ApplyEdits(src, edits)
}

// sanitizeParamName turns a field name into an idiomatic, non-colliding Go parameter name.
func sanitizeParamName(fieldName string) string {
	if fieldName == "" {
		return "val"
	}

	runes := []rune(fieldName)
	firstLower := string(unicode.ToLower(runes[0])) + string(runes[1:])

	// Handle initialisms: e.g. "URL" -> "url", "ID" -> "id", "HTTP" -> "http"
	allUpper := true
	for _, r := range runes {
		if !unicode.IsUpper(r) {
			allUpper = false
			break
		}
	}
	if allUpper {
		firstLower = strings.ToLower(fieldName)
	}

	switch firstLower {
	case "break", "case", "chan", "const", "continue", "default", "defer",
		"else", "fallthrough", "for", "func", "go", "goto", "if",
		"import", "interface", "map", "package", "range", "return",
		"select", "struct", "switch", "type", "var":
		return firstLower + "Val"
	case "len", "cap", "make", "new", "append", "copy", "close", "delete",
		"complex", "real", "imag", "panic", "recover", "print", "println":
		return firstLower + "Val"
	default:
		return firstLower
	}
}
