package gogen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"

	"tahr/internal/core/lsp"
)

// ImplOptions configures interface method stub generation.
type ImplOptions struct {
	ReceiverName      string // receiver identifier (e.g. "s", default: first letter lowercase)
	PointerReceiver   bool   // pointer receiver `(s *Struct)` or value `(s Struct)`, default: true
	StubBody          string // "panic" (default: panic("unimplemented")) or "zero" (return zero values)
	OverwriteExisting bool   // if false: skips methods already defined on the struct in source
}

// DefaultImplOptions returns standard options for interface implementation.
func DefaultImplOptions(info *StructInfo) ImplOptions {
	rec := "s"
	if info != nil {
		rec = info.ReceiverName
	}
	return ImplOptions{
		ReceiverName:      rec,
		PointerReceiver:   true,
		StubBody:          "panic",
		OverwriteExisting: false,
	}
}

// MethodSig represents a parsed or registered interface method signature.
type MethodSig struct {
	Name    string
	Params  []ParamInfo
	Results []ParamInfo
	Doc     string
}

// ParamInfo represents a parameter or return value in a method signature.
type ParamInfo struct {
	Name string
	Type string
}

// InterfaceInfo contains the name and method signatures of an interface.
type InterfaceInfo struct {
	Name    string
	Methods []MethodSig
	Imports []string
}

// standardInterfaces holds a catalog of well-known Go standard library interfaces.
var standardInterfaces = map[string]InterfaceInfo{
	"io.Reader": {
		Name: "io.Reader",
		Imports: []string{"io"},
		Methods: []MethodSig{
			{
				Name:    "Read",
				Params:  []ParamInfo{{Name: "p", Type: "[]byte"}},
				Results: []ParamInfo{{Name: "n", Type: "int"}, {Name: "err", Type: "error"}},
			},
		},
	},
	"io.Writer": {
		Name: "io.Writer",
		Imports: []string{"io"},
		Methods: []MethodSig{
			{
				Name:    "Write",
				Params:  []ParamInfo{{Name: "p", Type: "[]byte"}},
				Results: []ParamInfo{{Name: "n", Type: "int"}, {Name: "err", Type: "error"}},
			},
		},
	},
	"io.Closer": {
		Name: "io.Closer",
		Imports: []string{"io"},
		Methods: []MethodSig{
			{
				Name:    "Close",
				Params:  nil,
				Results: []ParamInfo{{Name: "", Type: "error"}},
			},
		},
	},
	"io.ReadCloser": {
		Name: "io.ReadCloser",
		Imports: []string{"io"},
		Methods: []MethodSig{
			{
				Name:    "Read",
				Params:  []ParamInfo{{Name: "p", Type: "[]byte"}},
				Results: []ParamInfo{{Name: "n", Type: "int"}, {Name: "err", Type: "error"}},
			},
			{
				Name:    "Close",
				Params:  nil,
				Results: []ParamInfo{{Name: "", Type: "error"}},
			},
		},
	},
	"io.WriteCloser": {
		Name: "io.WriteCloser",
		Imports: []string{"io"},
		Methods: []MethodSig{
			{
				Name:    "Write",
				Params:  []ParamInfo{{Name: "p", Type: "[]byte"}},
				Results: []ParamInfo{{Name: "n", Type: "int"}, {Name: "err", Type: "error"}},
			},
			{
				Name:    "Close",
				Params:  nil,
				Results: []ParamInfo{{Name: "", Type: "error"}},
			},
		},
	},
	"io.ReadWriter": {
		Name: "io.ReadWriter",
		Imports: []string{"io"},
		Methods: []MethodSig{
			{
				Name:    "Read",
				Params:  []ParamInfo{{Name: "p", Type: "[]byte"}},
				Results: []ParamInfo{{Name: "n", Type: "int"}, {Name: "err", Type: "error"}},
			},
			{
				Name:    "Write",
				Params:  []ParamInfo{{Name: "p", Type: "[]byte"}},
				Results: []ParamInfo{{Name: "n", Type: "int"}, {Name: "err", Type: "error"}},
			},
		},
	},
	"io.Seeker": {
		Name: "io.Seeker",
		Imports: []string{"io"},
		Methods: []MethodSig{
			{
				Name:    "Seek",
				Params:  []ParamInfo{{Name: "offset", Type: "int64"}, {Name: "whence", Type: "int"}},
				Results: []ParamInfo{{Name: "", Type: "int64"}, {Name: "", Type: "error"}},
			},
		},
	},
	"error": {
		Name: "error",
		Methods: []MethodSig{
			{
				Name:    "Error",
				Params:  nil,
				Results: []ParamInfo{{Name: "", Type: "string"}},
			},
		},
	},
	"fmt.Stringer": {
		Name: "fmt.Stringer",
		Imports: []string{"fmt"},
		Methods: []MethodSig{
			{
				Name:    "String",
				Params:  nil,
				Results: []ParamInfo{{Name: "", Type: "string"}},
			},
		},
	},
	"sort.Interface": {
		Name: "sort.Interface",
		Imports: []string{"sort"},
		Methods: []MethodSig{
			{
				Name:    "Len",
				Params:  nil,
				Results: []ParamInfo{{Name: "", Type: "int"}},
			},
			{
				Name:    "Less",
				Params:  []ParamInfo{{Name: "i", Type: "int"}, {Name: "j", Type: "int"}},
				Results: []ParamInfo{{Name: "", Type: "bool"}},
			},
			{
				Name:    "Swap",
				Params:  []ParamInfo{{Name: "i", Type: "int"}, {Name: "j", Type: "int"}},
				Results: nil,
			},
		},
	},
	"http.Handler": {
		Name: "http.Handler",
		Imports: []string{"net/http"},
		Methods: []MethodSig{
			{
				Name:    "ServeHTTP",
				Params:  []ParamInfo{{Name: "w", Type: "http.ResponseWriter"}, {Name: "r", Type: "*http.Request"}},
				Results: nil,
			},
		},
	},
}

// GetStandardInterface retrieves standard library interface metadata if recognized.
func GetStandardInterface(name string) (*InterfaceInfo, bool) {
	// Normalize: "Reader" -> "io.Reader", "Stringer" -> "fmt.Stringer"
	if iface, ok := standardInterfaces[name]; ok {
		return &iface, true
	}
	switch strings.ToLower(name) {
	case "reader":
		iface := standardInterfaces["io.Reader"]
		return &iface, true
	case "writer":
		iface := standardInterfaces["io.Writer"]
		return &iface, true
	case "closer":
		iface := standardInterfaces["io.Closer"]
		return &iface, true
	case "readcloser":
		iface := standardInterfaces["io.ReadCloser"]
		return &iface, true
	case "writecloser":
		iface := standardInterfaces["io.WriteCloser"]
		return &iface, true
	case "readwriter":
		iface := standardInterfaces["io.ReadWriter"]
		return &iface, true
	case "seeker":
		iface := standardInterfaces["io.Seeker"]
		return &iface, true
	case "stringer":
		iface := standardInterfaces["fmt.Stringer"]
		return &iface, true
	case "handler":
		iface := standardInterfaces["http.Handler"]
		return &iface, true
	}
	return nil, false
}

// ParseInterfaceFromSource extracts an interface declared in the Go source code.
func ParseInterfaceFromSource(src []byte, ifaceName string) (*InterfaceInfo, error) {
	fset := token.NewFileSet()
	file, _, err := parseSourceWithFallback(fset, src)
	if err != nil {
		return nil, err
	}

	var found *InterfaceInfo
	ast.Inspect(file, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			return true
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != ifaceName {
				continue
			}

			ifaceType, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}

			methods := extractInterfaceMethods(ifaceType, fset)
			found = &InterfaceInfo{
				Name:    ifaceName,
				Methods: methods,
			}
			return false
		}
		return true
	})

	if found != nil {
		return found, nil
	}

	// Also attempt parsing ifaceName as an interface literal snippet, e.g. "interface { Foo() string }"
	trimmed := strings.TrimSpace(ifaceName)
	if strings.HasPrefix(trimmed, "interface") || strings.HasPrefix(trimmed, "type ") {
		snippet := trimmed
		if !strings.HasPrefix(snippet, "type ") {
			snippet = "type Anon " + snippet
		}
		snippetSrc := "package main\n" + snippet
		sFset := token.NewFileSet()
		sFile, sErr := parser.ParseFile(sFset, "snippet.go", snippetSrc, 0)
		if sErr == nil {
			for _, decl := range sFile.Decls {
				if gd, ok := decl.(*ast.GenDecl); ok {
					for _, sp := range gd.Specs {
						if ts, ok := sp.(*ast.TypeSpec); ok {
							if it, ok := ts.Type.(*ast.InterfaceType); ok {
								return &InterfaceInfo{
									Name:    ts.Name.Name,
									Methods: extractInterfaceMethods(it, sFset),
								}, nil
							}
						}
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("interface '%s' not found", ifaceName)
}

func extractInterfaceMethods(iface *ast.InterfaceType, fset *token.FileSet) []MethodSig {
	var methods []MethodSig
	if iface.Methods == nil {
		return methods
	}

	for _, m := range iface.Methods.List {
		funcType, ok := m.Type.(*ast.FuncType)
		if !ok || len(m.Names) == 0 {
			// Embedded interface or constraint
			continue
		}

		methodName := m.Names[0].Name
		var params []ParamInfo
		if funcType.Params != nil {
			for _, p := range funcType.Params.List {
				var pBuf bytes.Buffer
				_ = printer.Fprint(&pBuf, fset, p.Type)
				pType := pBuf.String()

				if len(p.Names) == 0 {
					params = append(params, ParamInfo{Name: "", Type: pType})
				} else {
					for _, name := range p.Names {
						params = append(params, ParamInfo{Name: name.Name, Type: pType})
					}
				}
			}
		}

		var results []ParamInfo
		if funcType.Results != nil {
			for _, r := range funcType.Results.List {
				var rBuf bytes.Buffer
				_ = printer.Fprint(&rBuf, fset, r.Type)
				rType := rBuf.String()

				if len(r.Names) == 0 {
					results = append(results, ParamInfo{Name: "", Type: rType})
				} else {
					for _, name := range r.Names {
						results = append(results, ParamInfo{Name: name.Name, Type: rType})
					}
				}
			}
		}

		methods = append(methods, MethodSig{
			Name:    methodName,
			Params:  params,
			Results: results,
		})
	}

	return methods
}

// GenerateInterfaceStubs produces the Go source code for receiver method stubs.
func GenerateInterfaceStubs(info *StructInfo, iface *InterfaceInfo, existingMethods map[string]bool, opts ImplOptions) string {
	if info == nil || iface == nil || len(iface.Methods) == 0 {
		return ""
	}

	recName := opts.ReceiverName
	if recName == "" {
		recName = info.ReceiverName
	}
	if recName == "" {
		recName = "s"
	}

	structType := info.Name + info.TypeArgs
	var receiver string
	if opts.PointerReceiver {
		receiver = fmt.Sprintf("(%s *%s)", recName, structType)
	} else {
		receiver = fmt.Sprintf("(%s %s)", recName, structType)
	}

	var stubs []string
	for _, m := range iface.Methods {
		if !opts.OverwriteExisting && existingMethods != nil && existingMethods[m.Name] {
			continue
		}

		var paramStrs []string
		for i, p := range m.Params {
			pName := p.Name
			if pName == "" {
				pName = fmt.Sprintf("p%d", i+1)
			}
			paramStrs = append(paramStrs, fmt.Sprintf("%s %s", pName, p.Type))
		}

		var resultStr string
		if len(m.Results) == 1 && m.Results[0].Name == "" {
			resultStr = " " + m.Results[0].Type
		} else if len(m.Results) > 0 {
			var rStrs []string
			for i, r := range m.Results {
				if r.Name != "" {
					rStrs = append(rStrs, fmt.Sprintf("%s %s", r.Name, r.Type))
				} else {
					rStrs = append(rStrs, fmt.Sprintf("res%d %s", i+1, r.Type))
				}
			}
			resultStr = fmt.Sprintf(" (%s)", strings.Join(rStrs, ", "))
		}

		body := "\tpanic(\"unimplemented\")"
		if opts.StubBody == "zero" {
			if len(m.Results) > 0 {
				var zeroVals []string
				for _, r := range m.Results {
					zeroVals = append(zeroVals, zeroValueForType(r.Type))
				}
				body = fmt.Sprintf("\treturn %s", strings.Join(zeroVals, ", "))
			} else {
				body = "\t// no-op"
			}
		}

		doc := fmt.Sprintf("// %s implements %s.", m.Name, iface.Name)
		stub := fmt.Sprintf("%s\nfunc %s %s(%s)%s {\n%s\n}",
			doc, receiver, m.Name, strings.Join(paramStrs, ", "), resultStr, body)
		stubs = append(stubs, stub)
	}

	return strings.Join(stubs, "\n\n")
}

// GenerateInterfaceImplEdits generates LSP TextEdits inserting interface method stubs after the struct.
func GenerateInterfaceImplEdits(src []byte, structName, ifaceName string, opts ImplOptions) ([]lsp.TextEdit, error) {
	info, err := FindStructByName(src, structName)
	if err != nil {
		return nil, err
	}

	// Locate interface definition
	iface, found := GetStandardInterface(ifaceName)
	if !found {
		parsedIface, parseErr := ParseInterfaceFromSource(src, ifaceName)
		if parseErr != nil {
			return nil, fmt.Errorf("gogen: unknown interface '%s': %w", ifaceName, parseErr)
		}
		iface = parsedIface
	}

	existingMethods := findExistingReceiverMethods(src, structName)
	code := GenerateInterfaceStubs(info, iface, existingMethods, opts)
	if code == "" {
		return nil, fmt.Errorf("gogen: all methods for %s already implemented on %s", ifaceName, structName)
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

// GenerateInterfaceImpl returns modified source code with interface method stubs appended.
func GenerateInterfaceImpl(src []byte, structName, ifaceName string, opts ImplOptions) ([]byte, error) {
	edits, err := GenerateInterfaceImplEdits(src, structName, ifaceName, opts)
	if err != nil {
		return nil, err
	}
	return ApplyEdits(src, edits)
}

func findExistingReceiverMethods(src []byte, structName string) map[string]bool {
	methods := make(map[string]bool)
	fset := token.NewFileSet()
	file, _, err := parseSourceWithFallback(fset, src)
	if err != nil {
		return methods
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}

		recvType := fn.Recv.List[0].Type
		name := extractTypeName(recvType)
		if name == structName {
			methods[fn.Name.Name] = true
		}
	}

	return methods
}

func zeroValueForType(t string) string {
	t = strings.TrimSpace(t)
	switch t {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune":
		return "0"
	case "float32", "float64":
		return "0.0"
	case "complex64", "complex128":
		return "0i"
	case "bool":
		return "false"
	case "string":
		return `""`
	case "error":
		return "nil"
	default:
		if strings.HasPrefix(t, "*") || strings.HasPrefix(t, "[]") ||
			strings.HasPrefix(t, "map[") || strings.HasPrefix(t, "chan ") ||
			t == "any" || t == "interface{}" {
			return "nil"
		}
		return fmt.Sprintf("%s{}", t)
	}
}
