package lsp

import (
	"os/exec"
	"testing"
	"time"
)

func TestLiveGoplsIntegration(t *testing.T) {
	cmd, err := exec.LookPath("C:\\Users\\user\\go\\bin\\gopls.exe")
	if err != nil {
		t.Skip("gopls not found")
	}

	h := &mockHandler{}
	client, err := StartClient(cmd, []string{}, h)
	if err != nil {
		t.Fatalf("StartClient failed: %v", err)
	}
	defer client.Close()

	rootURI := "file:///d:/tahr"
	if err := client.Initialize(rootURI); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Logf("Initialized successfully")

	fileURI := "file:///d:/tahr/cmd/tahr/main.go"
	brokenCode := "package main\n\nfun c m ain() {\n\tva r a int\n\tb := \"hello\n}\n"

	err = client.DidOpen(fileURI, "go", brokenCode, 1)
	if err != nil {
		t.Fatalf("DidOpen failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.diags) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Logf("Diagnostics received: %d for URI: %s", len(h.diags), h.uri)
	for i, d := range h.diags {
		t.Logf("  diag[%d]: line=%d col=%d..%d msg=%q sev=%d", i, d.Range.Start.Line, d.Range.Start.Character, d.Range.End.Character, d.Message, d.Severity)
	}

	validURI := "file:///d:/tahr/cmd/tahr/test_valid.go"
	validCode := "package main\n\nconst Version = \"1.0.0\"\n\nfunc Hello() {}\n\nfunc World() {}\n"
	_ = client.DidOpen(validURI, "go", validCode, 1)
	time.Sleep(200 * time.Millisecond)
	syms, err := client.DocumentSymbols(validURI)
	t.Logf("DocumentSymbols valid result: count=%d err=%v", len(syms), err)
	for i, s := range syms {
		t.Logf("  sym[%d]: name=%s line=%d range=%+v", i, s.Name, s.Range.Start.Line, s.Range)
	}
}
