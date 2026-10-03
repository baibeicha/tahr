package crash

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// PlatformInfo holds runtime architecture, OS, and terminal environment details.
type PlatformInfo struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Frontend string `json:"frontend"`
	Terminal string `json:"terminal"`
	Wayland  bool   `json:"wayland"`
}

// CurrentPlatformInfo captures current runtime platform metadata.
func CurrentPlatformInfo() PlatformInfo {
	term := os.Getenv("TERM")
	if term == "" {
		term = os.Getenv("WT_SESSION")
		if term != "" {
			term = "WindowsTerminal"
		} else {
			term = "conhost"
		}
	}
	wayland := os.Getenv("WAYLAND_DISPLAY") != ""

	return PlatformInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Frontend: "tui",
		Terminal: term,
		Wayland:  wayland,
	}
}

// CrashReportPayload models the complete JSON crash report structure.
type CrashReportPayload struct {
	ReportID    string       `json:"report_id"`
	Timestamp   string       `json:"timestamp"`
	AppVersion  string       `json:"app_version"`
	Platform    PlatformInfo `json:"platform"`
	PanicReason string       `json:"panic_reason"`
	StackTrace  string       `json:"stack_trace"`
	RecentLogs  []string     `json:"recent_logs"`
}

// GetCrashesDir resolves the OS XDG-compliant storage directory for crashes.
func GetCrashesDir() string {
	var dir string
	switch runtime.GOOS {
	case "windows":
		localApp := os.Getenv("LOCALAPPDATA")
		if localApp != "" {
			dir = filepath.Join(localApp, "tahr", "crashes")
		} else {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, "AppData", "Local", "tahr", "crashes")
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Library", "Application Support", "tahr", "crashes")
	default: // Linux and others
		xdgData := os.Getenv("XDG_DATA_HOME")
		if xdgData != "" {
			dir = filepath.Join(xdgData, "tahr", "crashes")
		} else {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, ".local", "share", "tahr", "crashes")
		}
	}
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// newUUID generates a pseudo-random UUIDv4 string.
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// SaveCrashReport serializes panic, stack trace and recent logs into a timestamped JSON file.
func SaveCrashReport(version, panicReason, stackTrace string, logs []string) (string, error) {
	crashesDir := GetCrashesDir()
	now := time.Now()

	// Sanitize recent logs before serialization
	sanitizedLogs := make([]string, len(logs))
	for i, l := range logs {
		sanitizedLogs[i] = string(SanitizeLogData([]byte(l)))
	}

	payload := CrashReportPayload{
		ReportID:    newUUID(),
		Timestamp:   now.Format(time.RFC3339),
		AppVersion:  version,
		Platform:    CurrentPlatformInfo(),
		PanicReason: panicReason,
		StackTrace:  string(SanitizeLogData([]byte(stackTrace))),
		RecentLogs:  sanitizedLogs,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}

	fileName := fmt.Sprintf("crash-%d.json", now.Unix())
	targetPath := filepath.Join(crashesDir, fileName)

	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return "", err
	}

	return targetPath, nil
}

// FindPendingCrashReports returns paths to any unhandled crash dump files, ordered newest first.
func FindPendingCrashReports() ([]string, error) {
	dir := GetCrashesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var reports []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "crash-") && strings.HasSuffix(e.Name(), ".json") {
			reports = append(reports, filepath.Join(dir, e.Name()))
		}
	}

	sort.Slice(reports, func(i, j int) bool {
		// Newest first based on filename timestamp
		return reports[i] > reports[j]
	})

	return reports, nil
}

// LoadCrashReport deserializes a crash report JSON payload from disk.
func LoadCrashReport(path string) (*CrashReportPayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var payload CrashReportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

// DeleteCrashReport removes a local crash report file.
func DeleteCrashReport(path string) error {
	return os.Remove(path)
}
