package lsp

import (
	"testing"
)

type mockHandler struct {
	diags []Diagnostic
	uri   string
}

func (m *mockHandler) OnDiagnostics(uri string, d []Diagnostic) {
	m.uri = uri
	m.diags = d
}

func TestLSP_ProtocolTypes(t *testing.T) {
	h := &mockHandler{}
	c := &Client{
		handler: h,
		pending: make(map[int64]chan *Response),
	}

	id1 := c.NextID()
	id2 := c.NextID()
	if id2 != id1+1 {
		t.Fatalf("expected monotonic IDs, got %d then %d", id1, id2)
	}

	// Test incoming diagnostic dispatch
	payload := []byte(`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{"uri":"file:///test.go","diagnostics":[{"range":{"start":{"line":10,"character":2},"end":{"line":10,"character":5}},"severity":1,"message":"syntax error"}]}}`)
	c.dispatchIncoming(payload)

	if h.uri != "file:///test.go" {
		t.Fatalf("expected URI 'file:///test.go', got %q", h.uri)
	}
	if len(h.diags) != 1 || h.diags[0].Message != "syntax error" {
		t.Fatalf("unexpected diagnostics: %+v", h.diags)
	}
}

func TestLSP_SemanticTokensAndInlayHints(t *testing.T) {
	// Test InlayHint string and composite label
	h1 := InlayHint{
		Position: Position{Line: 5, Character: 12},
		Label:    "x: int",
		Kind:     InlayHintType,
	}
	if h1.TextLabel() != "x: int" {
		t.Errorf("expected 'x: int', got %q", h1.TextLabel())
	}

	h2 := InlayHint{
		Position: Position{Line: 6, Character: 10},
		Label: []any{
			map[string]any{"value": "param:"},
			map[string]any{"value": " string"},
		},
		Kind: InlayHintParameter,
	}
	if h2.TextLabel() != "param: string" {
		t.Errorf("expected 'param: string', got %q", h2.TextLabel())
	}

	// Test SemanticTokens decoding
	// Delta encoded: [deltaLine, deltaChar, length, tokenType, tokenModifiers]
	// Token 1: line 2, char 4, len 3, tokenType 11 (function), mod 0
	// Token 2: line 2, char 8 (deltaChar=4), len 6, tokenType 7 (variable), mod 1 (declaration)
	// Token 3: line 3 (deltaLine=1), char 2, len 5, tokenType 14 (keyword), mod 0
	encoded := []uint32{
		2, 4, 3, 11, 0,
		0, 4, 6, 7, 1,
		1, 2, 5, 14, 0,
	}

	decoded := DecodeSemanticTokens(encoded, nil, nil)
	if len(decoded) != 3 {
		t.Fatalf("expected 3 decoded tokens, got %d", len(decoded))
	}

	if decoded[0].Line != 2 || decoded[0].CharStart != 4 || decoded[0].TokenType != "function" {
		t.Errorf("unexpected token 0: %+v", decoded[0])
	}
	if decoded[1].Line != 2 || decoded[1].CharStart != 8 || decoded[1].TokenType != "variable" {
		t.Errorf("unexpected token 1: %+v", decoded[1])
	}
	if len(decoded[1].Modifiers) != 1 || decoded[1].Modifiers[0] != "declaration" {
		t.Errorf("unexpected modifiers for token 1: %+v", decoded[1].Modifiers)
	}
	if decoded[2].Line != 3 || decoded[2].CharStart != 2 || decoded[2].TokenType != "keyword" {
		t.Errorf("unexpected token 2: %+v", decoded[2])
	}
}
