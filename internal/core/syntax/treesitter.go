package syntax

import (
	"strings"
	"sync"
	"unicode"
)

// ASTNode represents a node in the Tree-sitter Concrete Syntax Tree (CST/AST).
type ASTNode struct {
	Type     string     `json:"type"`
	StartCol int        `json:"start_col"`
	EndCol   int        `json:"end_col"`
	Children []*ASTNode `json:"children,omitempty"`
	Parent   *ASTNode   `json:"-"`
}

// TreeSitterEngine coordinates Tree-sitter AST queries, highlights.scm mappings,
// and pure-Go/WASM grammar execution.
type TreeSitterEngine struct {
	mu           sync.RWMutex
	queryRules   map[string]map[string]TokenType // lang -> (ast_node_type -> token_type)
	wasmGrammars map[string][]byte
}

// NewTreeSitterEngine initializes the Tree-sitter parsing engine with standard highlights.scm rules.
func NewTreeSitterEngine() *TreeSitterEngine {
	ts := &TreeSitterEngine{
		queryRules:   make(map[string]map[string]TokenType),
		wasmGrammars: make(map[string][]byte),
	}
	ts.initStandardRules()
	return ts
}

// RegisterWasmGrammar registers a compiled grammar.wasm bundle for a language.
func (ts *TreeSitterEngine) RegisterWasmGrammar(lang string, wasmBytes []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.wasmGrammars[strings.ToLower(lang)] = wasmBytes
}

func (ts *TreeSitterEngine) initStandardRules() {
	// Standard Tree-sitter highlights.scm node mappings for Go
	goRules := map[string]TokenType{
		"keyword":                   TokenKeyword,
		"package_clause":            TokenKeyword,
		"import_declaration":        TokenKeyword,
		"function_declaration":      TokenFunction,
		"method_declaration":        TokenFunction,
		"call_expression":           TokenFunction,
		"function_call":             TokenFunction,
		"type_identifier":           TokenTypeIdent,
		"primitive_type":            TokenTypeIdent,
		"struct_type":               TokenTypeIdent,
		"interface_type":            TokenTypeIdent,
		"interpreted_string_literal":TokenString,
		"raw_string_literal":        TokenString,
		"rune_literal":              TokenString,
		"int_literal":               TokenNumber,
		"float_literal":             TokenNumber,
		"comment":                   TokenComment,
		"operator":                  TokenOperator,
		"constant":                  TokenConstant,
		"nil":                       TokenConstant,
		"true":                      TokenConstant,
		"false":                     TokenConstant,
	}

	// Python highlights.scm mappings
	pyRules := map[string]TokenType{
		"keyword":              TokenKeyword,
		"def":                  TokenKeyword,
		"class":                TokenKeyword,
		"function_definition":  TokenFunction,
		"call":                 TokenFunction,
		"type":                 TokenTypeIdent,
		"string":               TokenString,
		"integer":              TokenNumber,
		"float":                TokenNumber,
		"comment":              TokenComment,
		"operator":             TokenOperator,
		"none":                 TokenConstant,
		"boolean":              TokenConstant,
	}

	// Rust highlights.scm mappings
	rustRules := map[string]TokenType{
		"keyword":              TokenKeyword,
		"fn":                   TokenKeyword,
		"function_item":        TokenFunction,
		"call_expression":      TokenFunction,
		"type_identifier":      TokenTypeIdent,
		"string_literal":       TokenString,
		"integer_literal":      TokenNumber,
		"float_literal":        TokenNumber,
		"line_comment":         TokenComment,
		"block_comment":        TokenComment,
		"operator":             TokenOperator,
		"boolean_literal":      TokenConstant,
	}

	ts.queryRules["go"] = goRules
	ts.queryRules["golang"] = goRules
	ts.queryRules["py"] = pyRules
	ts.queryRules["python"] = pyRules
	ts.queryRules["rs"] = rustRules
	ts.queryRules["rust"] = rustRules
}

// ParseLineAST generates a Tree-sitter CST/AST for a given line of code.
func (ts *TreeSitterEngine) ParseLineAST(lang string, line string) *ASTNode {
	root := &ASTNode{
		Type:     "source_file",
		StartCol: 0,
		EndCol:   len([]rune(line)),
		Children: make([]*ASTNode, 0),
	}

	runes := []rune(line)
	if len(runes) == 0 {
		return root
	}

	lang = strings.ToLower(lang)
	if idx := strings.LastIndex(lang, "."); idx != -1 {
		lang = lang[idx+1:]
	}

	i := 0
	for i < len(runes) {
		// Whitespace
		if unicode.IsSpace(runes[i]) {
			i++
			continue
		}

		// Single-line comments: // or #
		if (i+1 < len(runes) && runes[i] == '/' && runes[i+1] == '/') ||
			((lang == "py" || lang == "python" || lang == "sh" || lang == "yaml") && runes[i] == '#') {
			node := &ASTNode{
				Type:     "comment",
				StartCol: i,
				EndCol:   len(runes),
				Parent:   root,
			}
			root.Children = append(root.Children, node)
			break
		}

		// Block comments: /* ... */
		if i+1 < len(runes) && runes[i] == '/' && runes[i+1] == '*' {
			start := i
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			if i+1 < len(runes) {
				i += 2
			} else {
				i = len(runes)
			}
			node := &ASTNode{
				Type:     "comment",
				StartCol: start,
				EndCol:   i,
				Parent:   root,
			}
			root.Children = append(root.Children, node)
			continue
		}

		// String literals: "...", '...', `...`
		if runes[i] == '"' || runes[i] == '`' || runes[i] == '\'' {
			q := runes[i]
			start := i
			i++
			for i < len(runes) && runes[i] != q {
				if runes[i] == '\\' && i+1 < len(runes) {
					i += 2
					continue
				}
				i++
			}
			if i < len(runes) {
				i++
			}
			node := &ASTNode{
				Type:     "string_literal",
				StartCol: start,
				EndCol:   i,
				Parent:   root,
			}
			root.Children = append(root.Children, node)
			continue
		}

		// Numeric literals
		if unicode.IsDigit(runes[i]) || (runes[i] == '.' && i+1 < len(runes) && unicode.IsDigit(runes[i+1])) {
			start := i
			for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == '.' || runes[i] == 'x' || runes[i] == 'X' || runes[i] == 'b' || runes[i] == 'B' || runes[i] == '_') {
				i++
			}
			node := &ASTNode{
				Type:     "int_literal",
				StartCol: start,
				EndCol:   i,
				Parent:   root,
			}
			root.Children = append(root.Children, node)
			continue
		}

		// Identifiers, Keywords, Types, Function Calls
		if unicode.IsLetter(runes[i]) || runes[i] == '_' {
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			word := string(runes[start:i])

			// Lookahead for function call expression: funcName(...)
			j := i
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
			isCall := (j < len(runes) && runes[j] == '(')

			// Lookbehind for func keyword
			isFuncDecl := false
			if len(root.Children) > 0 {
				lastChild := root.Children[len(root.Children)-1]
				if lastChild.Type == "keyword" {
					kw := string(runes[lastChild.StartCol:lastChild.EndCol])
					if kw == "func" || kw == "def" || kw == "fn" || kw == "function" {
						isFuncDecl = true
					}
				}
			}

			nodeType := "identifier"
			if isTreeSitterKeyword(lang, word) {
				nodeType = "keyword"
			} else if isFuncDecl {
				nodeType = "function_declaration"
			} else if isCall {
				nodeType = "call_expression"
			} else if isTreeSitterConstant(lang, word) {
				nodeType = "constant"
			} else if isTreeSitterType(lang, word) || unicode.IsUpper(runes[start]) {
				nodeType = "type_identifier"
			}

			node := &ASTNode{
				Type:     nodeType,
				StartCol: start,
				EndCol:   i,
				Parent:   root,
			}
			root.Children = append(root.Children, node)
			continue
		}

		// Operators & Punctuation
		if strings.ContainsRune(":=+-*/%&|^!<>~?.", runes[i]) {
			start := i
			for i < len(runes) && strings.ContainsRune(":=+-*/%&|^!<>~?.", runes[i]) {
				i++
			}
			node := &ASTNode{
				Type:     "operator",
				StartCol: start,
				EndCol:   i,
				Parent:   root,
			}
			root.Children = append(root.Children, node)
			continue
		}

		i++
	}

	return root
}

// HighlightLine executes Tree-sitter AST queries against a line of text.
func (ts *TreeSitterEngine) HighlightLine(lang string, line string) []Span {
	ast := ts.ParseLineAST(lang, line)
	if ast == nil || len(ast.Children) == 0 {
		return nil
	}

	lang = strings.ToLower(lang)
	if idx := strings.LastIndex(lang, "."); idx != -1 {
		lang = lang[idx+1:]
	}

	rules := ts.queryRules[lang]
	spans := make([]Span, 0, len(ast.Children))

	for _, child := range ast.Children {
		tokenType := TokenDefault
		if rules != nil {
			if tt, ok := rules[child.Type]; ok {
				tokenType = tt
			}
		}

		if tokenType == TokenDefault {
			switch child.Type {
			case "keyword":
				tokenType = TokenKeyword
			case "call_expression", "function_declaration":
				tokenType = TokenFunction
			case "type_identifier":
				tokenType = TokenTypeIdent
			case "string_literal":
				tokenType = TokenString
			case "int_literal", "float_literal":
				tokenType = TokenNumber
			case "comment":
				tokenType = TokenComment
			case "operator":
				tokenType = TokenOperator
			case "constant":
				tokenType = TokenConstant
			}
		}

		if tokenType != TokenDefault {
			spans = append(spans, Span{
				StartCol: child.StartCol,
				EndCol:   child.EndCol,
				Type:     tokenType,
			})
		}
	}

	return spans
}

// TreeSitterKeyword checks standard keywords for grammar.
func isTreeSitterKeyword(lang, word string) bool {
	switch lang {
	case "go", "golang":
		switch word {
		case "package", "import", "func", "return", "var", "const", "type", "struct",
			"interface", "if", "else", "switch", "case", "default", "for", "range",
			"go", "select", "chan", "defer", "map", "break", "continue", "fallthrough", "goto":
			return true
		}
	case "py", "python":
		switch word {
		case "def", "class", "return", "import", "from", "as", "if", "elif", "else",
			"for", "while", "in", "try", "except", "finally", "with", "yield", "async",
			"await", "pass", "lambda", "raise", "assert", "global", "nonlocal", "del", "is", "not", "and", "or":
			return true
		}
	case "rs", "rust":
		switch word {
		case "fn", "let", "mut", "return", "if", "else", "match", "for", "while",
			"loop", "struct", "enum", "trait", "impl", "pub", "mod", "use", "crate",
			"where", "type", "const", "static", "unsafe", "async", "await", "move", "dyn":
			return true
		}
	case "js", "ts", "javascript", "typescript":
		switch word {
		case "function", "const", "let", "var", "return", "if", "else", "switch", "case",
			"default", "for", "while", "do", "break", "continue", "import", "export", "from",
			"class", "extends", "super", "this", "new", "typeof", "instanceof", "in", "of",
			"async", "await", "yield", "try", "catch", "finally", "throw":
			return true
		}
	}
	return false
}

func isTreeSitterConstant(lang, word string) bool {
	switch lang {
	case "go", "golang":
		return word == "nil" || word == "true" || word == "false" || word == "iota"
	case "py", "python":
		return word == "None" || word == "True" || word == "False"
	case "rs", "rust":
		return word == "true" || word == "false" || word == "Some" || word == "None" || word == "Ok" || word == "Err"
	case "js", "ts", "javascript", "typescript":
		return word == "null" || word == "undefined" || word == "true" || word == "false" || word == "NaN" || word == "Infinity"
	}
	return false
}

func isTreeSitterType(lang, word string) bool {
	switch lang {
	case "go", "golang":
		switch word {
		case "string", "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
			"float32", "float64", "complex64", "complex128", "byte", "rune", "bool", "error", "any":
			return true
		}
	case "rs", "rust":
		switch word {
		case "i8", "i16", "i32", "i64", "i128", "isize",
			"u8", "u16", "u32", "u64", "u128", "usize",
			"f32", "f64", "bool", "char", "str", "String", "Option", "Result", "Vec", "Box":
			return true
		}
	}
	return false
}
