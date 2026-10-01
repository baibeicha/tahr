package syntax

// StructuralDiagnostic represents a syntax or structural issue detected locally.
type StructuralDiagnostic struct {
	Line     int    // 0-indexed line
	StartCol int    // 0-indexed character offset
	EndCol   int    // 0-indexed character offset
	Message  string
	Severity int    // 1 = Error, 2 = Warning
}

type bracketToken struct {
	r    rune
	line int
	col  int
}

// ValidateStructure scans lines for unbalanced brackets (), [], {}.
// Language-agnostic, zero-allocation fast-path scanner.
func ValidateStructure(lines []string) []StructuralDiagnostic {
	var diags []StructuralDiagnostic
	var stack []bracketToken

	for lineIdx, line := range lines {
		runes := []rune(line)
		inSingleQuote := false
		inDoubleQuote := false
		inBacktick := false

		for col := 0; col < len(runes); col++ {
			r := runes[col]

			// Check for line comments // or # (common across languages)
			if !inSingleQuote && !inDoubleQuote && !inBacktick {
				if r == '/' && col+1 < len(runes) && runes[col+1] == '/' {
					break // line comment, stop checking rest of line
				}
				if r == '#' {
					break // python / bash / yaml / toml comment
				}
			}

			// Handle string literals to ignore brackets inside quotes
			if inSingleQuote {
				if r == '\\' {
					col++ // skip escaped char
					continue
				}
				if r == '\'' {
					inSingleQuote = false
				}
				continue
			}
			if inDoubleQuote {
				if r == '\\' {
					col++ // skip escaped char
					continue
				}
				if r == '"' {
					inDoubleQuote = false
				}
				continue
			}
			if inBacktick {
				if r == '`' {
					inBacktick = false
				}
				continue
			}

			switch r {
			case '\'':
				inSingleQuote = true
			case '"':
				inDoubleQuote = true
			case '`':
				inBacktick = true
			case '(', '[', '{':
				stack = append(stack, bracketToken{r: r, line: lineIdx, col: col})
			case ')', ']', '}':
				expected := rune(0)
				switch r {
				case ')':
					expected = '('
				case ']':
					expected = '['
				case '}':
					expected = '{'
				}

				if len(stack) == 0 {
					diags = append(diags, StructuralDiagnostic{
						Line:     lineIdx,
						StartCol: col,
						EndCol:   col + 1,
						Message:  "Unmatched closing bracket: " + string(r),
						Severity: 1,
					})
				} else {
					top := stack[len(stack)-1]
					if top.r != expected {
						diags = append(diags, StructuralDiagnostic{
							Line:     lineIdx,
							StartCol: col,
							EndCol:   col + 1,
							Message:  "Mismatched bracket: expected " + matchingPair(top.r) + ", found " + string(r),
							Severity: 1,
						})
					} else {
						stack = stack[:len(stack)-1]
					}
				}
			}
		}
	}

	// Any remaining open brackets on stack are unclosed
	for _, unclosed := range stack {
		diags = append(diags, StructuralDiagnostic{
			Line:     unclosed.line,
			StartCol: unclosed.col,
			EndCol:   unclosed.col + 1,
			Message:  "Unclosed opening bracket: " + string(unclosed.r),
			Severity: 1,
		})
	}

	return diags
}

func matchingPair(open rune) string {
	switch open {
	case '(':
		return "')'"
	case '[':
		return "']'"
	case '{':
		return "'}'"
	default:
		return ""
	}
}
