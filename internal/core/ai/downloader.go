package ai

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultModelURL  = "https://huggingface.co/Qwen/Qwen2.5-Coder-0.5B-Instruct-GGUF/resolve/main/qwen2.5-coder-0.5b-instruct-q4_k_m.gguf"
	DefaultWinZipURL = "https://github.com/ggml-org/llama.cpp/releases/download/b11408/llama-b11408-bin-win-cpu-x64.zip"
)

// Downloader manages asynchronous background acquisition of model weights and server binaries.
type Downloader struct {
	mu          sync.Mutex
	downloading atomic.Bool
	cancel      context.CancelFunc
	pct         atomic.Int32
	statusMsg   string
	lastError   error
}

// NewDownloader creates a new AI asset downloader.
func NewDownloader() *Downloader {
	return &Downloader{}
}

// IsDownloading returns true if a background download is currently in flight.
func (d *Downloader) IsDownloading() bool {
	return d.downloading.Load()
}

// Progress returns the current completion percentage (0-100) and status description.
func (d *Downloader) Progress() (int, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return int(d.pct.Load()), d.statusMsg
}

// Cancel terminates any ongoing background download.
func (d *Downloader) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

// StartBackgroundDownload begins asynchronous downloading of missing AI dependencies.
func (d *Downloader) StartBackgroundDownload(
	modelsDir, binDir string,
	needServer, needModel bool,
	onStart func(),
	onDone func(),
	onError func(err error),
) bool {
	d.mu.Lock()
	if d.downloading.Load() {
		d.mu.Unlock()
		return false
	}
	d.downloading.Store(true)
	d.pct.Store(0)
	d.statusMsg = "Starting download..."
	d.lastError = nil

	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.mu.Unlock()

	if onStart != nil {
		onStart()
	}

	go func() {
		defer func() {
			d.downloading.Store(false)
			d.mu.Lock()
			d.cancel = nil
			d.mu.Unlock()
		}()

		err := d.runDownload(ctx, modelsDir, binDir, needServer, needModel)
		if err != nil {
			d.mu.Lock()
			d.lastError = err
			d.statusMsg = fmt.Sprintf("Error: %v", err)
			d.mu.Unlock()
			if onError != nil {
				onError(err)
			}
			return
		}

		d.mu.Lock()
		d.pct.Store(100)
		d.statusMsg = "Complete"
		d.mu.Unlock()

		if onDone != nil {
			onDone()
		}
	}()

	return true
}

func (d *Downloader) runDownload(ctx context.Context, modelsDir, binDir string, needServer, needModel bool) error {
	// 1. Download llama-server binary if needed
	if needServer {
		if runtime.GOOS == "windows" {
			d.setStatus(5, "Downloading llama-server (~19 MB)...")
			zipPath := filepath.Join(binDir, "llama-server-win.zip")
			err := downloadToFile(ctx, DefaultWinZipURL, zipPath, func(pct int, cur, tot int64) {
				// Server accounts for 0-20% of total progress
				scaled := 5 + (pct * 15 / 100)
				d.setStatus(scaled, fmt.Sprintf("Downloading llama-server (%d%%)...", pct))
			})
			if err != nil {
				return fmt.Errorf("llama-server download failed: %w", err)
			}
			defer os.Remove(zipPath)

			d.setStatus(20, "Extracting llama-server...")
			if err := extractZip(zipPath, binDir); err != nil {
				return fmt.Errorf("extracting llama-server failed: %w", err)
			}
		}
	}

	// 2. Download Qwen model weights if needed
	if needModel {
		targetModel := filepath.Join(modelsDir, "qwen2.5-coder-0.5b.gguf")
		d.setStatus(25, "Downloading Qwen2.5-Coder model (~390 MB)...")
		startPct := 25
		if !needServer {
			startPct = 5
		}
		err := downloadToFile(ctx, DefaultModelURL, targetModel, func(pct int, cur, tot int64) {
			rangeSpan := 100 - startPct
			scaled := startPct + (pct * rangeSpan / 100)
			mbCur := float64(cur) / (1024 * 1024)
			mbTot := float64(tot) / (1024 * 1024)
			d.setStatus(scaled, fmt.Sprintf("Downloading model: %.1f / %.1f MB (%d%%)", mbCur, mbTot, pct))
		})
		if err != nil {
			return fmt.Errorf("model download failed: %w", err)
		}
	}

	return nil
}

func (d *Downloader) setStatus(pct int, msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pct.Store(int32(pct))
	d.statusMsg = msg
}

type progressReader struct {
	reader     io.Reader
	total      int64
	current    int64
	onProgress func(pct int, current, total int64)
	lastUpdate time.Time
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.current += int64(n)
		now := time.Now()
		if pr.total > 0 && pr.onProgress != nil && now.Sub(pr.lastUpdate) > 100*time.Millisecond {
			pr.lastUpdate = now
			pct := int(float64(pr.current) / float64(pr.total) * 100)
			if pct > 100 {
				pct = 100
			}
			pr.onProgress(pct, pr.current, pr.total)
		}
	}
	return n, err
}

func downloadToFile(ctx context.Context, url, targetPath string, onProgress func(pct int, cur, tot int64)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	// Follow redirects up to 10
	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d downloading %s", resp.StatusCode, url)
	}

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := targetPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		_ = os.Remove(tmpPath)
	}()

	pr := &progressReader{
		reader:     resp.Body,
		total:      resp.ContentLength,
		onProgress: onProgress,
	}

	if _, err := io.Copy(f, pr); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	_ = os.Remove(targetPath) // remove target if exists before renaming
	return os.Rename(tmpPath, targetPath)
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	for _, f := range r.File {
		base := filepath.Base(f.Name)
		lower := strings.ToLower(base)
		// Extract llama-server and supporting DLLs
		if strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".dll") {
			destPath := filepath.Join(destDir, base)
			outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				outFile.Close()
				return err
			}
			_, copyErr := io.Copy(outFile, rc)
			rc.Close()
			outFile.Close()
			if copyErr != nil {
				return copyErr
			}
		}
	}
	return nil
}
