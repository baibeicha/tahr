package plugin

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zip"
	"github.com/klauspost/compress/zstd"
)

// MethodZstandard is the standard ZIP compression method ID for Zstandard (93).
const MethodZstandard uint16 = 93

// PackDirectory creates a .tahr archive from a directory using Zstandard compression.
func PackDirectory(srcDir, destTahrPath string) error {
	outFile, err := os.Create(destTahrPath)
	if err != nil {
		return fmt.Errorf("create destination archive: %w", err)
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	// Register Zstandard compressor (Method 93)
	zw.RegisterCompressor(MethodZstandard, func(out io.Writer) (io.WriteCloser, error) {
		return zstd.NewWriter(out, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	})

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		headerName := filepath.ToSlash(relPath)

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = headerName
		header.Method = MethodZstandard

		if info.IsDir() {
			header.Name += "/"
		}

		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}

		if !info.IsDir() {
			fileData, err := os.Open(path)
			if err != nil {
				return err
			}
			defer fileData.Close()

			if _, err := io.Copy(w, fileData); err != nil {
				return err
			}
		}
		return nil
	})
}

// UnpackArchive extracts a .tahr Zstandard archive into targetDir with Zip Slip protection.
func UnpackArchive(tahrFilePath, targetDir string) (*Manifest, error) {
	r, err := zip.OpenReader(tahrFilePath)
	if err != nil {
		return nil, fmt.Errorf("open tahr archive: %w", err)
	}
	defer r.Close()

	// Register fast Zstandard decompressor
	r.RegisterDecompressor(MethodZstandard, func(in io.Reader) io.ReadCloser {
		zr, _ := zstd.NewReader(in)
		return zr.IOReadCloser()
	})

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("create target dir: %w", err)
	}

	for _, f := range r.File {
		// Strict Zip Slip defense: prevent directory traversal via .. or absolute paths
		cleanName := filepath.Clean(f.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) || strings.Contains(f.Name, "../") || strings.Contains(f.Name, `..\`) {
			return nil, fmt.Errorf("security rejection: Zip Slip attempt detected: %s", f.Name)
		}

		destPath := filepath.Join(targetDir, cleanName)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return nil, err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return nil, err
		}

		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open archived file %s: %w", f.Name, err)
		}

		outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return nil, fmt.Errorf("create extracted file %s: %w", destPath, err)
		}

		_, copyErr := io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("extract file %s: %w", f.Name, copyErr)
		}
	}

	// Validate manifest exists after extraction
	manifestPath := filepath.Join(targetDir, "plugin.json")
	return LoadManifest(manifestPath)
}
