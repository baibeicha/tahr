// Package buffer provides text buffer representation, UTF coordinate conversions,
// and safe atomic file persistence for the Tahr editor engine.
package buffer

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ChunkSource represents an entity that can stream its text content in chunks.
// Both Rope and other buffer implementations satisfy this interface.
type ChunkSource interface {
	Iterator() ChunkIterator
}

// ComputeTempPath generates a hidden, process-isolated temporary file path
// located in the exact same directory as targetPath to guarantee single-volume atomic rename.
func ComputeTempPath(targetPath string) string {
	cleanPath := filepath.Clean(targetPath)
	dir := filepath.Dir(cleanPath)
	base := filepath.Base(cleanPath)
	return filepath.Join(dir, fmt.Sprintf(".%s.%d.tahr.tmp", base, os.Getpid()))
}

// DetectCRLF inspects the raw byte slice for CRLF ("\r\n") sequences.
func DetectCRLF(data []byte) bool {
	return bytes.Contains(data, []byte("\r\n"))
}

// LoadFile safely reads a file from disk, detects CRLF format, and normalizes
// newlines to LF for in-memory buffer ingestion.
func LoadFile(targetPath string) (data []byte, hasCRLF bool, err error) {
	raw, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, false, fmt.Errorf("read file %s: %w", targetPath, err)
	}
	normalized, detectedCRLF := NormalizeCRLF(raw)
	return normalized, detectedCRLF, nil
}

// SaveAtomicFile writes the buffer content to a temporary file in the target directory,
// flushes OS write buffers, synchronizes hardware disk cache (file.Sync),
// and atomically renames the temporary file over targetPath.
//
// Guarantees:
// 1. Zero data corruption: target file is never truncated in-place.
// 2. Failure cleanup: temporary file is immediately removed if any intermediate step fails.
// 3. Permission retention: if targetPath exists, its exact permission bits are preserved.
// 4. CRLF restoration: if hasCRLF is true, '\n' line endings are converted back to '\r\n'.
// 5. Cross-platform correctness: closes open handles before rename (Windows requirement).
func SaveAtomicFile(targetPath string, source ChunkSource, hasCRLF bool) error {
	cleanTarget := filepath.Clean(targetPath)
	dir := filepath.Dir(cleanTarget)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir parent %s: %w", dir, err)
	}

	// Determine file permissions
	perm := os.FileMode(0644)
	if fi, err := os.Stat(cleanTarget); err == nil {
		perm = fi.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat target %s: %w", cleanTarget, err)
	}

	// Compute hidden temporary file path in the same directory
	tmpPath := ComputeTempPath(cleanTarget)

	// Create and truncate the temporary file
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create temp file %s: %w", tmpPath, err)
	}

	// Atomic cleanup guard: unless marked as success, always remove tmpPath on exit
	success := false
	defer func() {
		if !success {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	// 64 KB buffered writer to minimize syscall overhead
	bw := bufio.NewWriterSize(tmpFile, 64*1024)
	it := source.Iterator()
	crlfBytes := []byte("\r\n")

	for {
		chunk, ok := it.Next()
		if !ok {
			break
		}

		if !hasCRLF {
			if _, err := bw.Write(chunk); err != nil {
				return fmt.Errorf("write chunk to %s: %w", tmpPath, err)
			}
		} else {
			// Streamingly restore '\r\n' for every '\n' in the chunk
			start := 0
			for i := 0; i < len(chunk); i++ {
				if chunk[i] == '\n' {
					if _, err := bw.Write(chunk[start:i]); err != nil {
						return fmt.Errorf("write chunk slice: %w", err)
					}
					if _, err := bw.Write(crlfBytes); err != nil {
						return fmt.Errorf("write crlf: %w", err)
					}
					start = i + 1
				}
			}
			if start < len(chunk) {
				if _, err := bw.Write(chunk[start:]); err != nil {
					return fmt.Errorf("write trailing chunk slice: %w", err)
				}
			}
		}
	}

	// Flush buffered data into kernel page cache
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("flush writer to %s: %w", tmpPath, err)
	}

	// Flush OS page cache to physical disk (fsync on POSIX / FlushFileBuffers on Windows)
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file %s: %w", tmpPath, err)
	}

	// Windows Invariant: handle MUST be closed before rename, or MoveFileEx will fail
	// with ERROR_SHARING_VIOLATION / ERROR_ACCESS_DENIED.
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tmpPath, err)
	}

	// Atomic rename replaces cleanTarget with tmpPath in a single filesystem operation
	if err := os.Rename(tmpPath, cleanTarget); err != nil {
		return fmt.Errorf("atomic rename %s -> %s: %w", tmpPath, cleanTarget, err)
	}

	// On POSIX, sync directory entry to guarantee durability of directory metadata
	if runtime.GOOS != "windows" {
		if dirFile, err := os.Open(dir); err == nil {
			_ = dirFile.Sync()
			_ = dirFile.Close()
		}
	}

	success = true
	return nil
}
