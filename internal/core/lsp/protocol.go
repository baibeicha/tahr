package lsp

import (
	"fmt"
	"strings"
)

// Protocol versions and standard constants
const (
	JSONRPCVersion = "2.0"
)

// Request represents a standard JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Notification represents a standard JSON-RPC 2.0 notification.
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Response represents a standard JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError represents a JSON-RPC error.
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Position in LSP is 0-based line and UTF-16 character code unit offset.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range in a document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// TextDocumentItem describes an opened document.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentIdentifier identifies a document.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier identifies a specific version of a document.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentContentChangeEvent describes an incremental edit.
type TextDocumentContentChangeEvent struct {
	Range       *Range `json:"range,omitempty"`
	RangeLength int    `json:"rangeLength,omitempty"`
	Text        string `json:"text"`
}

// DidOpenTextDocumentParams is sent on textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidChangeTextDocumentParams is sent on textDocument/didChange.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// CompletionParams is sent on textDocument/completion.
type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// CompletionItem represents a code completion candidate.
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Documentation any    `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

// CompletionList represents a collection of completion items.
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// HoverParams is sent on textDocument/hover.
type HoverParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// Hover is returned by textDocument/hover.
type Hover struct {
	Contents any    `json:"contents"`
	Range    *Range `json:"range,omitempty"`
}

// DefinitionParams is sent on textDocument/definition.
type DefinitionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// Location represents a location inside a resource.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// DiagnosticSeverity levels.
type DiagnosticSeverity int

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

// Diagnostic represents an issue reported by the language server.
type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Code     any                `json:"code,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// PublishDiagnosticsParams is received from textDocument/publishDiagnostics.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     int          `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// CancelParams is sent on $/cancelRequest.
type CancelParams struct {
	ID int64 `json:"id"`
}

// InlayHintKind indicates the type of inlay hint.
type InlayHintKind int

const (
	InlayHintType      InlayHintKind = 1
	InlayHintParameter InlayHintKind = 2
)

// InlayHintParams is sent on textDocument/inlayHint.
type InlayHintParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

// InlayHintLabelPart represents a part of a composite inlay hint label.
type InlayHintLabelPart struct {
	Value   string `json:"value"`
	Tooltip any    `json:"tooltip,omitempty"`
}

// InlayHint represents an annotation rendered directly inside text.
type InlayHint struct {
	Position     Position      `json:"position"`
	Label        any           `json:"label"` // string or []InlayHintLabelPart
	Kind         InlayHintKind `json:"kind,omitempty"`
	PaddingLeft  bool          `json:"paddingLeft,omitempty"`
	PaddingRight bool          `json:"paddingRight,omitempty"`
	Tooltip      any           `json:"tooltip,omitempty"`
}

// TextLabel returns the string label of the inlay hint.
func (h *InlayHint) TextLabel() string {
	if s, ok := h.Label.(string); ok {
		return s
	}
	if parts, ok := h.Label.([]any); ok {
		var b strings.Builder
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				if v, ok := pm["value"].(string); ok {
					b.WriteString(v)
				}
			}
		}
		return b.String()
	}
	return fmt.Sprintf("%v", h.Label)
}

// SemanticTokensParams is sent on textDocument/semanticTokens/full.
type SemanticTokensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SemanticTokens represents the encoded integer array of semantic tokens.
type SemanticTokens struct {
	ResultID string   `json:"resultId,omitempty"`
	Data     []uint32 `json:"data"`
}

// Standard LSP semantic token types.
var DefaultSemanticTokenTypes = []string{
	"type", "class", "enum", "interface", "struct", "typeParameter",
	"parameter", "variable", "property", "enumMember", "event",
	"function", "method", "macro", "keyword", "modifier", "comment",
	"string", "number", "regexp", "operator", "decorator",
}

// Standard LSP semantic token modifiers.
var DefaultSemanticTokenModifiers = []string{
	"declaration", "definition", "readonly", "static", "deprecated",
	"abstract", "async", "modification", "documentation", "defaultLibrary",
}

// DecodedSemanticToken represents a parsed token position and classification.
type DecodedSemanticToken struct {
	Line      int
	CharStart int
	Length    int
	TokenType string
	Modifiers []string
}

// DecodeSemanticTokens decodes the 5-integer delta-encoded LSP semantic tokens stream.
func DecodeSemanticTokens(data []uint32, legendTypes, legendModifiers []string) []DecodedSemanticToken {
	if len(data) == 0 {
		return nil
	}
	if len(legendTypes) == 0 {
		legendTypes = DefaultSemanticTokenTypes
	}
	if len(legendModifiers) == 0 {
		legendModifiers = DefaultSemanticTokenModifiers
	}

	tokens := make([]DecodedSemanticToken, 0, len(data)/5)
	currentLine := 0
	currentChar := 0

	for i := 0; i+4 < len(data); i += 5 {
		deltaLine := int(data[i])
		deltaChar := int(data[i+1])
		length := int(data[i+2])
		tokenTypeIdx := int(data[i+3])
		tokenModBits := data[i+4]

		if deltaLine > 0 {
			currentLine += deltaLine
			currentChar = deltaChar
		} else {
			currentChar += deltaChar
		}

		tokenType := "unknown"
		if tokenTypeIdx < len(legendTypes) {
			tokenType = legendTypes[tokenTypeIdx]
		}

		var modifiers []string
		for mIdx, modName := range legendModifiers {
			if mIdx < 32 && (tokenModBits&(1<<mIdx)) != 0 {
				modifiers = append(modifiers, modName)
			}
		}

		tokens = append(tokens, DecodedSemanticToken{
			Line:      currentLine,
			CharStart: currentChar,
			Length:    length,
			TokenType: tokenType,
			Modifiers: modifiers,
		})
	}

	return tokens
}

// TextEdit represents an edit to a range in a document.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit represents changes across documents.
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []TextDocumentEdit    `json:"documentChanges,omitempty"`
}

// TextDocumentEdit represents changes to a specific text document.
type TextDocumentEdit struct {
	TextDocument OptionalVersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                              `json:"edits"`
}

// OptionalVersionedTextDocumentIdentifier represents a versioned text document.
type OptionalVersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int   `json:"version,omitempty"`
}

// RenameParams is sent on textDocument/rename.
type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

// CodeActionContext carries diagnostics relevant to code action query.
type CodeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Only        []string     `json:"only,omitempty"`
}

// CodeActionParams is sent on textDocument/codeAction.
type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      CodeActionContext      `json:"context"`
}

// Command represents an executable command.
type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

// CodeAction represents a quick fix or refactoring option.
type CodeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind,omitempty"` // "quickfix", "refactor", "source.organizeImports"
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	IsPreferred bool           `json:"isPreferred,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
	Command     *Command       `json:"command,omitempty"`
}

// SymbolKind indicates the kind of symbol.
type SymbolKind int

const (
	SymbolKindFile        SymbolKind = 1
	SymbolKindModule      SymbolKind = 2
	SymbolKindNamespace   SymbolKind = 3
	SymbolKindPackage     SymbolKind = 4
	SymbolKindClass       SymbolKind = 5
	SymbolKindMethod      SymbolKind = 6
	SymbolKindProperty    SymbolKind = 7
	SymbolKindField       SymbolKind = 8
	SymbolKindConstructor SymbolKind = 9
	SymbolKindEnum        SymbolKind = 10
	SymbolKindInterface   SymbolKind = 11
	SymbolKindFunction    SymbolKind = 12
	SymbolKindVariable    SymbolKind = 13
	SymbolKindConstant    SymbolKind = 14
	SymbolKindString      SymbolKind = 15
	SymbolKindNumber      SymbolKind = 16
	SymbolKindBoolean     SymbolKind = 17
	SymbolKindArray       SymbolKind = 18
	SymbolKindObject      SymbolKind = 19
	SymbolKindKey         SymbolKind = 20
	SymbolKindNull        SymbolKind = 21
	SymbolKindEnumMember  SymbolKind = 22
	SymbolKindStruct      SymbolKind = 23
	SymbolKindEvent       SymbolKind = 24
	SymbolKindOperator    SymbolKind = 25
	SymbolKindTypeParam   SymbolKind = 26
)

// DocumentSymbolParams is sent on textDocument/documentSymbol.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DocumentSymbol represents programming constructs like variables, functions, classes, etc.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Tags           []int            `json:"tags,omitempty"`
	Deprecated     bool             `json:"deprecated,omitempty"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolInformation represents 3.x flat symbol information.
type SymbolInformation struct {
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	Location      Location   `json:"location"`
	ContainerName string     `json:"containerName,omitempty"`
}
