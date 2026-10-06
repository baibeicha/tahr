package gogen

import (
	"fmt"
	"strings"
	"unicode"

	"tahr/internal/core/lsp"
)

// GetSetOptions configures getter and setter method generation.
type GetSetOptions struct {
	ReceiverName    string   // receiver identifier (default: first letter lowercase)
	PointerReceiver bool     // pointer receiver `(s *Struct)` or value `(s Struct)` (default: true)
	PrefixGet       bool     // whether to always prefix getters with "Get" (e.g. GetName vs Name)
	GenerateGetters bool     // generate getter methods (default: true)
	GenerateSetters bool     // generate setter methods (default: true)
	Fields          []string // specific field names to generate for (nil = all fields)
	ValParamName    string   // parameter name for setters (default: "val")
}

// DefaultGetSetOptions returns standard getter and setter options for a struct.
func DefaultGetSetOptions(info *StructInfo) GetSetOptions {
	rec := "s"
	if info != nil {
		rec = info.ReceiverName
	}
	return GetSetOptions{
		ReceiverName:    rec,
		PointerReceiver: true,
		PrefixGet:       false,
		GenerateGetters: true,
		GenerateSetters: true,
		ValParamName:    "val",
	}
}

// GenerateGettersSettersCode generates Go source for getter and setter methods.
func GenerateGettersSettersCode(info *StructInfo, opts GetSetOptions) string {
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

	valParam := opts.ValParamName
	if valParam == "" {
		valParam = "val"
	}

	receiverType := info.Name + info.TypeArgs
	var receiver string
	if opts.PointerReceiver {
		receiver = fmt.Sprintf("(%s *%s)", recName, receiverType)
	} else {
		receiver = fmt.Sprintf("(%s %s)", recName, receiverType)
	}

	// Always use pointer receiver for setters so modifications persist
	setterReceiver := fmt.Sprintf("(%s *%s)", recName, receiverType)

	var methods []string
	for _, field := range info.Fields {
		if field.IsEmbedded {
			continue
		}

		for _, name := range field.Names {
			if name == "" || name == "_" {
				continue
			}

			if opts.Fields != nil && !sliceContains(opts.Fields, name) {
				continue
			}

			capName := capitalizeField(name)

			// Getter
			if opts.GenerateGetters {
				getterName := capName
				// In Go, if a field is already exported "Field", having method "Field()" causes
				// compilation error: "type X has both field and method named Field".
				// In that case, or when PrefixGet is requested, use Get<Field>()
				if opts.PrefixGet || name == capName {
					getterName = "Get" + capName
				}

				getterBody := fmt.Sprintf("// %s returns the %s field value.\nfunc %s %s() %s {\n\treturn %s.%s\n}",
					getterName, name, receiver, getterName, field.TypeStr, recName, name)
				methods = append(methods, getterBody)
			}

			// Setter
			if opts.GenerateSetters {
				setterName := "Set" + capName
				setterBody := fmt.Sprintf("// %s sets the %s field value.\nfunc %s %s(%s %s) {\n\t%s.%s = %s\n}",
					setterName, name, setterReceiver, setterName, valParam, field.TypeStr, recName, name, valParam)
				methods = append(methods, setterBody)
			}
		}
	}

	return strings.Join(methods, "\n\n")
}

// GenerateGetSetEdits generates LSP TextEdits inserting getters/setters after the struct.
func GenerateGetSetEdits(src []byte, structName string, opts GetSetOptions) ([]lsp.TextEdit, error) {
	info, err := FindStructByName(src, structName)
	if err != nil {
		return nil, err
	}

	code := GenerateGettersSettersCode(info, opts)
	if code == "" {
		return nil, fmt.Errorf("gogen: no getters or setters generated for %s", structName)
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

// GenerateGettersSetters returns modified source code with getters and setters appended.
func GenerateGettersSetters(src []byte, structName string, opts GetSetOptions) ([]byte, error) {
	edits, err := GenerateGetSetEdits(src, structName, opts)
	if err != nil {
		return nil, err
	}
	return ApplyEdits(src, edits)
}

func capitalizeField(name string) string {
	if name == "" {
		return ""
	}
	runes := []rune(name)
	return string(unicode.ToUpper(runes[0])) + string(runes[1:])
}

func sliceContains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
