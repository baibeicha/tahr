package gogen

import (
	"fmt"
	"strings"

	"tahr/internal/core/lsp"
)

// DeepCopyOptions configures Clone / DeepCopy method generation.
type DeepCopyOptions struct {
	ReceiverName string // receiver identifier (default: first letter lowercase)
	MethodName   string // method name (default: "Clone", or "DeepCopy")
	Deep         bool   // if true: duplicates slices, maps, pointers; if false: memberwise copy
}

// DefaultDeepCopyOptions returns standard options for clone generation.
func DefaultDeepCopyOptions(info *StructInfo) DeepCopyOptions {
	rec := "s"
	if info != nil {
		rec = info.ReceiverName
	}
	return DeepCopyOptions{
		ReceiverName: rec,
		MethodName:   "Clone",
		Deep:         true,
	}
}

// GenerateDeepCopyCode produces the Go source string for the Clone() method.
func GenerateDeepCopyCode(info *StructInfo, opts DeepCopyOptions) string {
	if info == nil {
		return ""
	}

	recName := opts.ReceiverName
	if recName == "" {
		recName = info.ReceiverName
	}
	if recName == "" {
		recName = "s"
	}

	methodName := opts.MethodName
	if methodName == "" {
		methodName = "Clone"
	}

	structType := info.Name + info.TypeArgs
	receiver := fmt.Sprintf("(%s *%s)", recName, structType)

	var sb strings.Builder

	// Shallow / memberwise copy mode
	if !opts.Deep || !hasReferenceFields(info) {
		sb.WriteString(fmt.Sprintf("// %s creates a copy of %s.\n", methodName, info.Name))
		sb.WriteString(fmt.Sprintf("func %s %s() *%s {\n", receiver, methodName, structType))
		sb.WriteString(fmt.Sprintf("\tif %s == nil {\n\t\treturn nil\n\t}\n", recName))
		sb.WriteString(fmt.Sprintf("\tclone := *%s\n", recName))
		sb.WriteString("\treturn &clone\n")
		sb.WriteString("}")
		return sb.String()
	}

	// Deep duplication mode
	sb.WriteString(fmt.Sprintf("// %s creates a deep copy of %s, duplicating slices, maps, and pointers.\n", methodName, info.Name))
	sb.WriteString(fmt.Sprintf("func %s %s() *%s {\n", receiver, methodName, structType))
	sb.WriteString(fmt.Sprintf("\tif %s == nil {\n\t\treturn nil\n\t}\n\n", recName))

	var valueAssignments []string
	var deepCopyStmts []string

	for _, field := range info.Fields {
		for _, name := range field.Names {
			if name == "" || name == "_" {
				continue
			}

			if field.IsSlice {
				// Slices: allocate make() and copy()
				elemType := strings.TrimPrefix(field.TypeStr, "[]")
				stmt := fmt.Sprintf("\tif %s.%s != nil {\n\t\tclone.%s = make(%s, len(%s.%s))\n\t\tcopy(clone.%s, %s.%s)\n\t}",
					recName, name, name, field.TypeStr, recName, name, name, recName, name)
				deepCopyStmts = append(deepCopyStmts, stmt)
				_ = elemType
			} else if field.IsMap {
				// Maps: allocate make() and copy key-values
				stmt := fmt.Sprintf("\tif %s.%s != nil {\n\t\tclone.%s = make(%s, len(%s.%s))\n\t\tfor k, v := range %s.%s {\n\t\t\tclone.%s[k] = v\n\t\t}\n\t}",
					recName, name, name, field.TypeStr, recName, name, recName, name, name)
				deepCopyStmts = append(deepCopyStmts, stmt)
			} else if field.IsPointer {
				// Pointers: duplicate pointed-to value
				stmt := fmt.Sprintf("\tif %s.%s != nil {\n\t\tval := *%s.%s\n\t\tclone.%s = &val\n\t}",
					recName, name, recName, name, name)
				deepCopyStmts = append(deepCopyStmts, stmt)
			} else {
				// Value types (primitives, structs, interfaces, etc.)
				valueAssignments = append(valueAssignments, fmt.Sprintf("\t\t%s: %s.%s,", name, recName, name))
			}
		}
	}

	if len(valueAssignments) == 0 {
		sb.WriteString(fmt.Sprintf("\tclone := &%s{}\n", structType))
	} else {
		sb.WriteString(fmt.Sprintf("\tclone := &%s{\n", structType))
		for _, va := range valueAssignments {
			sb.WriteString(va)
			sb.WriteString("\n")
		}
		sb.WriteString("\t}\n")
	}

	if len(deepCopyStmts) > 0 {
		sb.WriteString("\n")
		for _, stmt := range deepCopyStmts {
			sb.WriteString(stmt)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n\treturn clone\n")
	sb.WriteString("}")
	return sb.String()
}

// GenerateDeepCopyEdits generates LSP TextEdits inserting the Clone method after the struct.
func GenerateDeepCopyEdits(src []byte, structName string, opts DeepCopyOptions) ([]lsp.TextEdit, error) {
	info, err := FindStructByName(src, structName)
	if err != nil {
		return nil, err
	}

	code := GenerateDeepCopyCode(info, opts)
	if code == "" {
		return nil, fmt.Errorf("gogen: empty clone method generated for %s", structName)
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

// GenerateDeepCopy returns modified source code with the Clone method appended.
func GenerateDeepCopy(src []byte, structName string, opts DeepCopyOptions) ([]byte, error) {
	edits, err := GenerateDeepCopyEdits(src, structName, opts)
	if err != nil {
		return nil, err
	}
	return ApplyEdits(src, edits)
}

func hasReferenceFields(info *StructInfo) bool {
	for _, f := range info.Fields {
		if f.IsSlice || f.IsMap || f.IsPointer {
			return true
		}
	}
	return false
}
