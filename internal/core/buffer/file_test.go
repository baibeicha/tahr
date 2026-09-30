package buffer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type stringChunkSource struct {
	chunks [][]byte
}

func (s *stringChunkSource) Iterator() ChunkIterator {
	r := NewRope()
	for _, c := range s.chunks {
		_ = r.Insert(r.TotalBytes(), c)
	}
	return r.Iterator()
}

func TestComputeTempPath(t *testing.T) {
	target := filepath.Join(os.TempDir(), "testfile.txt")
	tmp := ComputeTempPath(target)

	expectedDir := filepath.Dir(target)
	if filepath.Dir(tmp) != expectedDir {
		t.Fatalf("expected tmp in %s, got %s", expectedDir, filepath.Dir(tmp))
	}

	expectedBase := fmt.Sprintf(".testfile.txt.%d.tahr.tmp", os.Getpid())
	if filepath.Base(tmp) != expectedBase {
		t.Fatalf("expected base %s, got %s", expectedBase, filepath.Base(tmp))
	}
}

func TestSaveAtomicFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_atomic_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "doc.txt")
	source := &stringChunkSource{chunks: [][]byte{[]byte("Hello, "), []byte("World!\n")}}

	// 1. Initial save
	err = SaveAtomicFile(target, source, false)
	if err != nil {
		t.Fatalf("SaveAtomicFile failed: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "Hello, World!\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}

	// Verify temp file does not remain
	tmpPath := ComputeTempPath(target)
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temporary file was not cleaned up: %s", tmpPath)
	}

	// 2. Overwrite save with CRLF
	err = SaveAtomicFile(target, source, true)
	if err != nil {
		t.Fatalf("SaveAtomicFile with CRLF failed: %v", err)
	}
	data, err = os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !bytes.Equal(data, []byte("Hello, World!\r\n")) {
		t.Fatalf("expected CRLF content, got %q", string(data))
	}
}

func TestSaveAtomicFileDirectoryCreation(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_atomic_subdir_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "sub", "folder", "doc.txt")
	source := &stringChunkSource{chunks: [][]byte{[]byte("Nested File")}}

	err = SaveAtomicFile(target, source, false)
	if err != nil {
		t.Fatalf("SaveAtomicFile in nested directory failed: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil || string(data) != "Nested File" {
		t.Fatalf("nested file content mismatch: %q, err=%v", string(data), err)
	}
}

func TestLoadFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_load_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	// CRLF file
	crlfPath := filepath.Join(dir, "crlf.txt")
	err = os.WriteFile(crlfPath, []byte("line1\r\nline2\r\n"), 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	data, hasCRLF, err := LoadFile(crlfPath)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if !hasCRLF {
		t.Fatalf("expected hasCRLF = true")
	}
	if string(data) != "line1\nline2\n" {
		t.Fatalf("expected normalized LF content, got %q", string(data))
	}

	// LF file
	lfPath := filepath.Join(dir, "lf.txt")
	err = os.WriteFile(lfPath, []byte("line1\nline2\n"), 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	data, hasCRLF, err = LoadFile(lfPath)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if hasCRLF {
		t.Fatalf("expected hasCRLF = false")
	}
	if string(data) != "line1\nline2\n" {
		t.Fatalf("expected LF content, got %q", string(data))
	}
}
