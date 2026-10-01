package syntax

import (
	"strings"
	"unicode"
)

// TokenType categorizes syntax tokens for styling.
type TokenType uint8

const (
	TokenDefault TokenType = iota
	TokenKeyword
	TokenFunction
	TokenTypeIdent
	TokenString
	TokenNumber
	TokenComment
	TokenOperator
	TokenConstant
)

// Span represents a highlighted character range in a line.
type Span struct {
	StartCol int
	EndCol   int
	Type     TokenType
}

// Highlighter parses lines and generates syntax color spans.
type Highlighter interface {
	HighlightLine(language string, line string) []Span
}

// DefaultHighlighter is a high-speed, pure-Go lexical tokenizer.
type DefaultHighlighter struct {
	languages map[string]map[string]bool
	types     map[string]map[string]bool
	constants map[string]map[string]bool
}

// NewDefaultHighlighter initializes built-in keyword and type dictionaries.
func NewDefaultHighlighter() *DefaultHighlighter {
	h := &DefaultHighlighter{
		languages: make(map[string]map[string]bool),
		types:     make(map[string]map[string]bool),
		constants: make(map[string]map[string]bool),
	}

	// 1. Go
	goKw := []string{
		"package", "import", "func", "return", "var", "const", "type", "struct",
		"interface", "if", "else", "switch", "case", "default", "for", "range",
		"go", "select", "chan", "defer", "map", "break", "continue", "fallthrough", "goto",
	}
	goTypes := []string{
		"string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"float32", "float64", "complex64", "complex128", "byte", "rune", "bool", "error", "any",
	}
	goConsts := []string{"nil", "true", "false", "iota"}

	// 2. Python
	pyKw := []string{
		"def", "class", "return", "import", "from", "as", "if", "elif", "else",
		"for", "while", "in", "try", "except", "finally", "with", "yield", "async",
		"await", "pass", "lambda", "raise", "assert", "global", "nonlocal", "del", "is", "not", "and", "or",
	}
	pyConsts := []string{"None", "True", "False"}

	// 3. Rust
	rustKw := []string{
		"fn", "let", "mut", "return", "if", "else", "match", "for", "while",
		"loop", "struct", "enum", "trait", "impl", "pub", "mod", "use", "crate",
		"where", "type", "const", "static", "unsafe", "async", "await", "move", "dyn",
	}
	rustTypes := []string{
		"i8", "i16", "i32", "i64", "i128", "isize",
		"u8", "u16", "u32", "u64", "u128", "usize",
		"f32", "f64", "bool", "char", "str", "String", "Option", "Result", "Vec", "Box",
	}
	rustConsts := []string{"true", "false", "Some", "None", "Ok", "Err"}

	// 4. JavaScript / TypeScript
	jsKw := []string{
		"function", "const", "let", "var", "return", "if", "else", "switch", "case",
		"default", "for", "while", "do", "break", "continue", "import", "export", "from",
		"class", "extends", "super", "this", "new", "typeof", "instanceof", "in", "of",
		"async", "await", "yield", "try", "catch", "finally", "throw",
	}
	jsConsts := []string{"null", "undefined", "true", "false", "NaN", "Infinity"}

	// 5. C / C++
	cKw := []string{
		"int", "char", "float", "double", "void", "short", "long", "unsigned", "signed",
		"struct", "union", "enum", "typedef", "sizeof", "auto", "register", "static",
		"extern", "const", "volatile", "if", "else", "switch", "case", "default",
		"for", "while", "do", "break", "continue", "goto", "return", "class", "public",
		"private", "protected", "namespace", "using", "template", "typename",
	}

	h.register("go", goKw, goTypes, goConsts)
	h.register("golang", goKw, goTypes, goConsts)
	h.register("py", pyKw, nil, pyConsts)
	h.register("python", pyKw, nil, pyConsts)
	h.register("rs", rustKw, rustTypes, rustConsts)
	h.register("rust", rustKw, rustTypes, rustConsts)
	h.register("js", jsKw, nil, jsConsts)
	h.register("javascript", jsKw, nil, jsConsts)
	h.register("ts", jsKw, nil, jsConsts)
	h.register("typescript", jsKw, nil, jsConsts)
	h.register("c", cKw, nil, nil)
	h.register("cpp", cKw, nil, nil)

	return h
}

func (h *DefaultHighlighter) register(lang string, kw, types, consts []string) {
	kwMap := make(map[string]bool)
	for _, k := range kw {
		kwMap[k] = true
	}
	h.languages[lang] = kwMap

	if len(types) > 0 {
		typeMap := make(map[string]bool)
		for _, t := range types {
			typeMap[t] = true
		}
		h.types[lang] = typeMap
	}

	if len(consts) > 0 {
		constMap := make(map[string]bool)
		for _, c := range consts {
			constMap[c] = true
		}
		h.constants[lang] = constMap
	}
}

// HighlightLine produces spans for a single text line.
func (h *DefaultHighlighter) HighlightLine(lang string, line string) []Span {
	runes := []rune(line)
	if len(runes) == 0 {
		return nil
	}

	lang = strings.ToLower(lang)
	if idx := strings.LastIndex(lang, "."); idx != -1 {
		lang = lang[idx+1:]
	}

	spans := make([]Span, 0, 16)
	keywords := h.languages[lang]
	types := h.types[lang]
	consts := h.constants[lang]

	i := 0
	for i < len(runes) {
		// Single-line comments //
		if i+1 < len(runes) && runes[i] == '/' && runes[i+1] == '/' {
			spans = append(spans, Span{
				StartCol: i,
				EndCol:   len(runes),
				Type:     TokenComment,
			})
			break
		}

		// Single-line comments # (Python, Shell, YAML)
		if (lang == "py" || lang == "python" || lang == "sh" || lang == "bash" || lang == "yaml" || lang == "toml") && runes[i] == '#' {
			spans = append(spans, Span{
				StartCol: i,
				EndCol:   len(runes),
				Type:     TokenComment,
			})
			break
		}

		// Multi-line comment on single line /* ... */
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
			spans = append(spans, Span{
				StartCol: start,
				EndCol:   i,
				Type:     TokenComment,
			})
			continue
		}

		// Strings (double quotes, single quotes, backticks)
		if runes[i] == '"' || runes[i] == '`' || runes[i] == '\'' {
			quote := runes[i]
			start := i
			i++
			for i < len(runes) && runes[i] != quote {
				if runes[i] == '\\' && i+1 < len(runes) {
					i += 2
					continue
				}
				i++
			}
			if i < len(runes) {
				i++ // include closing quote
			}
			spans = append(spans, Span{
				StartCol: start,
				EndCol:   i,
				Type:     TokenString,
			})
			continue
		}

		// Numbers (decimal, hex, binary, floats)
		if unicode.IsDigit(runes[i]) || (runes[i] == '.' && i+1 < len(runes) && unicode.IsDigit(runes[i+1])) {
			start := i
			for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == '.' || runes[i] == 'x' || runes[i] == 'X' || runes[i] == 'b' || runes[i] == 'B' || runes[i] == '_') {
				i++
			}
			spans = append(spans, Span{
				StartCol: start,
				EndCol:   i,
				Type:     TokenNumber,
			})
			continue
		}

		// Identifiers, Keywords, Types, Functions, Constants
		if unicode.IsLetter(runes[i]) || runes[i] == '_' {
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			word := string(runes[start:i])
			tokenType := TokenDefault

			if keywords != nil && keywords[word] {
				tokenType = TokenKeyword
			} else if consts != nil && consts[word] {
				tokenType = TokenConstant
			} else if types != nil && types[word] {
				tokenType = TokenTypeIdent
			} else if i < len(runes) && runes[i] == '(' {
				tokenType = TokenFunction
			} else if unicode.IsUpper(runes[start]) {
				tokenType = TokenTypeIdent
			}

			if tokenType != TokenDefault {
				spans = append(spans, Span{
					StartCol: start,
					EndCol:   i,
					Type:     tokenType,
				})
			}
			continue
		}

		// Operators & Delimiters
		if strings.ContainsRune(":=+-*/%&|^!<>~?", runes[i]) {
			start := i
			for i < len(runes) && strings.ContainsRune(":=+-*/%&|^!<>~?", runes[i]) {
				i++
			}
			spans = append(spans, Span{
				StartCol: start,
				EndCol:   i,
				Type:     TokenOperator,
			})
			continue
		}

		i++
	}

	return spans
}
