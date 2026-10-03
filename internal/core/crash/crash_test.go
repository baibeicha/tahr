package crash

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeLogData(t *testing.T) {
	input := `User path: C:\Users\user\project\secret.go
Auth: Bearer secret_token_1234567890abcdef
GitHub: ghp_123456789012345678901234567890123456
Key: -----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA0123456789
-----END RSA PRIVATE KEY-----
Password: password=supersecretpass;
Host: connecting to 127.0.0.1 and 192.168.1.50
`
	sanitized := string(SanitizeLogData([]byte(input)))

	if strings.Contains(sanitized, "secret_token_1234567890abcdef") {
		t.Errorf("Bearer token was not masked: %s", sanitized)
	}
	if !strings.Contains(sanitized, "Bearer [REDACTED]") {
		t.Errorf("Expected 'Bearer [REDACTED]' in sanitized logs")
	}

	if strings.Contains(sanitized, "ghp_123456789012345678901234567890123456") {
		t.Errorf("GitHub PAT was not masked: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[GITHUB_TOKEN_REDACTED]") {
		t.Errorf("Expected '[GITHUB_TOKEN_REDACTED]' in sanitized logs")
	}

	if strings.Contains(sanitized, "MIIEowIBAAKCAQEA0123456789") {
		t.Errorf("Private key was not masked: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[PRIVATE_KEY_REDACTED]") {
		t.Errorf("Expected '[PRIVATE_KEY_REDACTED]' in sanitized logs")
	}

	if strings.Contains(sanitized, "supersecretpass") {
		t.Errorf("Password was not masked: %s", sanitized)
	}

	if strings.Contains(sanitized, "127.0.0.1") || strings.Contains(sanitized, "192.168.1.50") {
		t.Errorf("Local IPs were not masked: %s", sanitized)
	}
}

func TestCrashReportSaveAndLoad(t *testing.T) {
	// Set temporary LOCALAPPDATA / XDG_DATA_HOME
	tempDir, err := os.MkdirTemp("", "tahr_crash_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	origLocalApp := os.Getenv("LOCALAPPDATA")
	origXdg := os.Getenv("XDG_DATA_HOME")
	os.Setenv("LOCALAPPDATA", tempDir)
	os.Setenv("XDG_DATA_HOME", tempDir)
	defer func() {
		os.Setenv("LOCALAPPDATA", origLocalApp)
		os.Setenv("XDG_DATA_HOME", origXdg)
	}()

	path, err := SaveCrashReport("0.1.0-test", "runtime error: nil pointer dereference", "goroutine 1 [running]:\nmain.go:10", []string{"log 1", "log 2"})
	if err != nil {
		t.Fatalf("SaveCrashReport failed: %v", err)
	}

	reports, err := FindPendingCrashReports()
	if err != nil {
		t.Fatalf("FindPendingCrashReports failed: %v", err)
	}
	if len(reports) == 0 {
		t.Fatalf("Expected at least 1 pending report, got 0")
	}

	payload, err := LoadCrashReport(path)
	if err != nil {
		t.Fatalf("LoadCrashReport failed: %v", err)
	}

	if payload.AppVersion != "0.1.0-test" {
		t.Errorf("Expected version 0.1.0-test, got %s", payload.AppVersion)
	}
	if payload.PanicReason != "runtime error: nil pointer dereference" {
		t.Errorf("Expected panic reason match, got %s", payload.PanicReason)
	}
	if len(payload.RecentLogs) != 2 {
		t.Errorf("Expected 2 logs, got %d", len(payload.RecentLogs))
	}

	if err := DeleteCrashReport(path); err != nil {
		t.Fatalf("DeleteCrashReport failed: %v", err)
	}

	reportsAfter, _ := FindPendingCrashReports()
	if len(reportsAfter) != 0 {
		t.Errorf("Expected 0 reports after deletion, got %d", len(reportsAfter))
	}
}

func TestSendBugReport(t *testing.T) {
	var receivedTitle string
	var receivedDesc string
	var receivedMeta BugReportMetadata
	var receivedLogContent string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			t.Errorf("Failed to parse multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		receivedTitle = r.FormValue("title")
		receivedDesc = r.FormValue("description")
		_ = json.Unmarshal([]byte(r.FormValue("metadata")), &receivedMeta)

		file, _, err := r.FormFile("logs")
		if err == nil {
			defer file.Close()
			b, _ := io.ReadAll(file)
			receivedLogContent = string(b)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ReportResponse{
			Success:  true,
			ReportID: "rep-12345",
			Message:  "Received",
		})
	}))
	defer server.Close()

	// Create a dummy screenshot
	tmpImg := filepath.Join(t.TempDir(), "screenshot.png")
	_ = os.WriteFile(tmpImg, []byte("\x89PNG\r\n\x1a\nfakeimagecontent"), 0644)

	req := BugReportRequest{
		Title:       "Test Bug Title",
		Description: "Detailed description of the issue",
		Metadata: BugReportMetadata{
			Platform:      CurrentPlatformInfo(),
			AppVersion:    "1.0.0",
			ActivePlugins: []string{"go", "git"},
		},
		Logs:           []byte("session log info Bearer 1234567890abcdef"),
		ScreenshotPath: tmpImg,
	}

	resp, err := SendBugReport(context.Background(), server.URL, req)
	if err != nil {
		t.Fatalf("SendBugReport returned error: %v", err)
	}
	if !resp.Success {
		t.Errorf("Expected Success=true, got false")
	}
	if resp.ReportID != "rep-12345" {
		t.Errorf("Expected ReportID rep-12345, got %s", resp.ReportID)
	}

	if receivedTitle != "Test Bug Title" {
		t.Errorf("Expected title match, got %s", receivedTitle)
	}
	if receivedDesc != "Detailed description of the issue" {
		t.Errorf("Expected desc match, got %s", receivedDesc)
	}
	if receivedMeta.AppVersion != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", receivedMeta.AppVersion)
	}
	if strings.Contains(receivedLogContent, "1234567890abcdef") {
		t.Errorf("Logs sent were not sanitized: %s", receivedLogContent)
	}
}

func TestFormatGitHubIssue(t *testing.T) {
	meta := BugReportMetadata{
		Platform: PlatformInfo{
			OS:       "windows",
			Arch:     "amd64",
			Frontend: "tui",
			Terminal: "conhost",
		},
		AppVersion:    "1.2.3",
		ActivePlugins: []string{"go", "json"},
	}

	ghText := FormatGitHubIssue("My Title", "Steps to reproduce:\n1. Click button", meta, "my log output", nil)
	if !strings.Contains(ghText, "Steps to reproduce:") {
		t.Errorf("Expected description in markdown")
	}
	if !strings.Contains(ghText, "App Version:** 1.2.3") {
		t.Errorf("Expected version in markdown")
	}
	if !strings.Contains(ghText, "my log output") {
		t.Errorf("Expected log content in markdown")
	}
}
