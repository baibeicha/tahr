package crash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultReportEndpoint is the default API endpoint for bug and crash reports.
const DefaultReportEndpoint = "https://api.tahr.dev/api/v1/bugreport"

// BugReportMetadata carries system environment and plugin status.
type BugReportMetadata struct {
	Platform      PlatformInfo `json:"platform"`
	AppVersion    string       `json:"app_version"`
	ActivePlugins []string     `json:"active_plugins"`
}

// BugReportRequest represents a manual issue report payload.
type BugReportRequest struct {
	Title          string
	Description    string
	Metadata       BugReportMetadata
	Logs           []byte
	ScreenshotPath string
}

// ReportResponse models the API response from the reporting service.
type ReportResponse struct {
	Success  bool   `json:"success"`
	ReportID string `json:"report_id,omitempty"`
	Message  string `json:"message,omitempty"`
}

// SendBugReport sends a manual bug report as multipart/form-data to the specified endpoint.
func SendBugReport(ctx context.Context, endpoint string, req BugReportRequest) (*ReportResponse, error) {
	if endpoint == "" {
		endpoint = DefaultReportEndpoint
	}

	// Validate title length (max 120 chars)
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("title cannot be empty")
	}
	if len(title) > 120 {
		title = title[:120]
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 1. title field
	if err := writer.WriteField("title", title); err != nil {
		return nil, err
	}

	// 2. description field
	if err := writer.WriteField("description", req.Description); err != nil {
		return nil, err
	}

	// 3. metadata field (JSON)
	metaBytes, err := json.Marshal(req.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := writer.WriteField("metadata", string(metaBytes)); err != nil {
		return nil, err
	}

	// 4. logs file (optional)
	if len(req.Logs) > 0 {
		sanitizedLogs := SanitizeLogData(req.Logs)
		part, err := writer.CreateFormFile("logs", "session.log")
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(sanitizedLogs); err != nil {
			return nil, err
		}
	}

	// 5. screenshot file (optional)
	if req.ScreenshotPath != "" {
		fInfo, err := os.Stat(req.ScreenshotPath)
		if err != nil {
			return nil, fmt.Errorf("screenshot file not found: %w", err)
		}
		if fInfo.Size() > 10*1024*1024 {
			return nil, fmt.Errorf("screenshot exceeds 10 MB limit (%d bytes)", fInfo.Size())
		}

		ext := strings.ToLower(filepath.Ext(req.ScreenshotPath))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".webp":
			// valid image
		default:
			return nil, fmt.Errorf("unsupported screenshot format '%s' (must be PNG, JPG, or WEBP)", ext)
		}

		file, err := os.Open(req.ScreenshotPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open screenshot: %w", err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("screenshot", filepath.Base(req.ScreenshotPath))
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(part, file); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("User-Agent", "Tahr-IDE/"+req.Metadata.AppVersion)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server responded with status %d: %s", resp.StatusCode, string(respBody))
	}

	var reportResp ReportResponse
	if err := json.Unmarshal(respBody, &reportResp); err != nil {
		// Even if body is not JSON, 2xx indicates success
		return &ReportResponse{Success: true, Message: "Report submitted successfully"}, nil
	}
	reportResp.Success = true
	return &reportResp, nil
}

// SendCrashReport posts an unhandled panic crash report to the endpoint.
func SendCrashReport(ctx context.Context, endpoint string, payload *CrashReportPayload) (*ReportResponse, error) {
	if endpoint == "" {
		endpoint = DefaultReportEndpoint
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Tahr-IDE/"+payload.AppVersion)
	httpReq.Header.Set("X-Tahr-Report-Type", "crash")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server responded with status %d: %s", resp.StatusCode, string(respBody))
	}

	var reportResp ReportResponse
	if err := json.Unmarshal(respBody, &reportResp); err != nil {
		return &ReportResponse{Success: true, ReportID: payload.ReportID, Message: "Crash dump submitted"}, nil
	}
	reportResp.Success = true
	return &reportResp, nil
}

// FormatGitHubIssue builds a clean markdown representation ready to copy-paste into GitHub.
func FormatGitHubIssue(title, description string, meta BugReportMetadata, logs string, crashPayload *CrashReportPayload) string {
	var sb strings.Builder

	sb.WriteString("### Description\n")
	if description != "" {
		sb.WriteString(strings.TrimSpace(description))
	} else {
		sb.WriteString("_No description provided._")
	}
	sb.WriteString("\n\n")

	sb.WriteString("### Environment & Metadata\n")
	sb.WriteString(fmt.Sprintf("- **App Version:** %s\n", meta.AppVersion))
	sb.WriteString(fmt.Sprintf("- **OS / Arch:** %s / %s\n", meta.Platform.OS, meta.Platform.Arch))
	sb.WriteString(fmt.Sprintf("- **Frontend:** %s (Terminal: %s)\n", meta.Platform.Frontend, meta.Platform.Terminal))
	if meta.Platform.Wayland {
		sb.WriteString("- **Wayland:** Yes\n")
	}
	if len(meta.ActivePlugins) > 0 {
		sb.WriteString(fmt.Sprintf("- **Active Plugins:** %s\n", strings.Join(meta.ActivePlugins, ", ")))
	}
	sb.WriteString("\n")

	if crashPayload != nil {
		sb.WriteString("### Crash Details\n")
		sb.WriteString(fmt.Sprintf("- **Report ID:** `%s`\n", crashPayload.ReportID))
		sb.WriteString(fmt.Sprintf("- **Timestamp:** %s\n", crashPayload.Timestamp))
		sb.WriteString(fmt.Sprintf("- **Panic Reason:** `%s`\n\n", crashPayload.PanicReason))

		if crashPayload.StackTrace != "" {
			sb.WriteString("<details><summary>Stack Trace</summary>\n\n```\n")
			sb.WriteString(strings.TrimSpace(crashPayload.StackTrace))
			sb.WriteString("\n```\n</details>\n\n")
		}

		if len(crashPayload.RecentLogs) > 0 {
			sb.WriteString("<details><summary>Recent Logs</summary>\n\n```\n")
			for _, l := range crashPayload.RecentLogs {
				sb.WriteString(l)
				sb.WriteString("\n")
			}
			sb.WriteString("```\n</details>\n\n")
		}
	} else if logs != "" {
		sb.WriteString("<details><summary>Session Logs</summary>\n\n```\n")
		sb.WriteString(strings.TrimSpace(logs))
		sb.WriteString("\n```\n</details>\n\n")
	}

	return sb.String()
}
