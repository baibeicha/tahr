package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/crash"
	"tahr/internal/core/i18n"
	"tahr/internal/core/logging"
	"tahr/internal/ui"
)

// BugReportAction represents an action resulting from user interaction in the bug reporter modal.
type BugReportAction string

const (
	BRActionNone       BugReportAction = ""
	BRActionSend       BugReportAction = "send"
	BRActionCopyGitHub BugReportAction = "copy_github"
	BRActionViewData   BugReportAction = "view_data"
	BRActionClose      BugReportAction = "close"
)

// BugReportModal provides an interactive dialog to compose and submit bug reports.
type BugReportModal struct {
	Open bool

	Title       string
	Description string
	Screenshot  string
	AttachLogs  bool

	FocusField int // 0: Title, 1: Description, 2: Screenshot, 3: Browse, 4: AttachLogs, 5: ViewData, 6: Send, 7: CopyGH, 8: Cancel

	TitleCursor int
	DescCursor  int
	ScreenCursor int

	ScreenshotStatus string
	ScreenshotValid  bool
	ScreenshotSize   int64

	IsSending  bool
	StatusMsg  string
	SendFailed bool

	AppVersion    string
	ActivePlugins []string
	Endpoint      string
}

// NewBugReportModal creates a new bug report modal.
func NewBugReportModal() *BugReportModal {
	return &BugReportModal{
		Open:        false,
		AttachLogs:  true,
		FocusField:  0,
		AppVersion:  "0.1.0-alpha",
		Endpoint:    crash.DefaultReportEndpoint,
	}
}

// OpenModal resets and activates the bug reporting modal.
func (m *BugReportModal) OpenModal(version string, plugins []string) {
	m.Open = true
	m.Title = ""
	m.Description = ""
	m.Screenshot = ""
	m.AttachLogs = true
	m.FocusField = 0
	m.TitleCursor = 0
	m.DescCursor = 0
	m.ScreenCursor = 0
	m.ScreenshotStatus = ""
	m.ScreenshotValid = false
	m.IsSending = false
	m.StatusMsg = ""
	m.SendFailed = false
	if version != "" {
		m.AppVersion = version
	}
	m.ActivePlugins = plugins
}

// ValidateScreenshot checks the screenshot path for existence, extension and size.
func (m *BugReportModal) ValidateScreenshot() {
	path := strings.TrimSpace(m.Screenshot)
	if path == "" {
		m.ScreenshotStatus = ""
		m.ScreenshotValid = false
		m.ScreenshotSize = 0
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		m.ScreenshotStatus = i18n.T("modal.bugreport.file_not_found")
		m.ScreenshotValid = false
		return
	}

	if info.IsDir() {
		m.ScreenshotStatus = i18n.T("modal.bugreport.is_dir")
		m.ScreenshotValid = false
		return
	}

	if info.Size() > 10*1024*1024 {
		m.ScreenshotStatus = i18n.T("modal.bugreport.file_too_big")
		m.ScreenshotValid = false
		return
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		m.ScreenshotStatus = i18n.T("modal.bugreport.invalid_ext")
		m.ScreenshotValid = false
		return
	}

	m.ScreenshotValid = true
	m.ScreenshotSize = info.Size()
	kb := m.ScreenshotSize / 1024
	m.ScreenshotStatus = i18n.T("modal.bugreport.file_verified", kb)
}

// HandleKey processes keyboard input inside the bug reporter modal.
func (m *BugReportModal) HandleKey(key input.Key) (bool, BugReportAction) {
	if !m.Open {
		return false, BRActionNone
	}

	if m.IsSending {
		return true, BRActionNone
	}

	// Hotkeys
	if key.HasCtrl() && (key.Type == input.KeyEnter || key.Rune == '\r' || key.Rune == '\n') {
		return true, BRActionSend
	}

	switch key.Type {
	case input.KeyEsc:
		m.Open = false
		return true, BRActionClose

	case input.KeyTab:
		m.FocusField = (m.FocusField + 1) % 9
		return true, BRActionNone

	case input.KeyBacktab:
		m.FocusField = (m.FocusField + 8) % 9
		return true, BRActionNone
	}

	// Field-specific key handling
	switch m.FocusField {
	case 0: // Title
		return m.handleTitleKey(key)

	case 1: // Description
		return m.handleDescKey(key)

	case 2: // Screenshot
		return m.handleScreenKey(key)

	case 3: // Browse button
		if key.Type == input.KeyEnter || key.Rune == ' ' {
			// Preset a default screenshot pattern if empty
			if m.Screenshot == "" {
				home, _ := os.UserHomeDir()
				m.Screenshot = filepath.Join(home, "screenshot.png")
				m.ValidateScreenshot()
			}
			return true, BRActionNone
		}

	case 4: // AttachLogs checkbox
		if key.Rune == ' ' || key.Type == input.KeyEnter {
			m.AttachLogs = !m.AttachLogs
			return true, BRActionNone
		}

	case 5: // View Data button
		if key.Type == input.KeyEnter || key.Rune == ' ' {
			return true, BRActionViewData
		}

	case 6: // Send button
		if key.Type == input.KeyEnter || key.Rune == ' ' {
			return true, BRActionSend
		}

	case 7: // Copy GitHub button
		if key.Type == input.KeyEnter || key.Rune == ' ' {
			return true, BRActionCopyGitHub
		}

	case 8: // Cancel button
		if key.Type == input.KeyEnter || key.Rune == ' ' {
			m.Open = false
			return true, BRActionClose
		}
	}

	return true, BRActionNone
}

func (m *BugReportModal) handleTitleKey(key input.Key) (bool, BugReportAction) {
	runes := []rune(m.Title)
	switch key.Type {
	case input.KeyLeft:
		if m.TitleCursor > 0 {
			m.TitleCursor--
		}
	case input.KeyRight:
		if m.TitleCursor < len(runes) {
			m.TitleCursor++
		}
	case input.KeyHome:
		m.TitleCursor = 0
	case input.KeyEnd:
		m.TitleCursor = len(runes)
	case input.KeyBackspace:
		if m.TitleCursor > 0 && len(runes) > 0 {
			m.Title = string(runes[:m.TitleCursor-1]) + string(runes[m.TitleCursor:])
			m.TitleCursor--
		}
	case input.KeyDelete:
		if m.TitleCursor < len(runes) {
			m.Title = string(runes[:m.TitleCursor]) + string(runes[m.TitleCursor+1:])
		}
	default:
		if key.Rune != 0 && len(runes) < 120 {
			m.Title = string(runes[:m.TitleCursor]) + string(key.Rune) + string(runes[m.TitleCursor:])
			m.TitleCursor++
		}
	}
	return true, BRActionNone
}

func (m *BugReportModal) handleDescKey(key input.Key) (bool, BugReportAction) {
	runes := []rune(m.Description)
	switch key.Type {
	case input.KeyEnter:
		m.Description = string(runes[:m.DescCursor]) + "\n" + string(runes[m.DescCursor:])
		m.DescCursor++
	case input.KeyLeft:
		if m.DescCursor > 0 {
			m.DescCursor--
		}
	case input.KeyRight:
		if m.DescCursor < len(runes) {
			m.DescCursor++
		}
	case input.KeyHome:
		m.DescCursor = 0
	case input.KeyEnd:
		m.DescCursor = len(runes)
	case input.KeyBackspace:
		if m.DescCursor > 0 && len(runes) > 0 {
			m.Description = string(runes[:m.DescCursor-1]) + string(runes[m.DescCursor:])
			m.DescCursor--
		}
	case input.KeyDelete:
		if m.DescCursor < len(runes) {
			m.Description = string(runes[:m.DescCursor]) + string(runes[m.DescCursor+1:])
		}
	default:
		if key.Rune != 0 {
			m.Description = string(runes[:m.DescCursor]) + string(key.Rune) + string(runes[m.DescCursor:])
			m.DescCursor++
		}
	}
	return true, BRActionNone
}

func (m *BugReportModal) handleScreenKey(key input.Key) (bool, BugReportAction) {
	runes := []rune(m.Screenshot)
	switch key.Type {
	case input.KeyLeft:
		if m.ScreenCursor > 0 {
			m.ScreenCursor--
		}
	case input.KeyRight:
		if m.ScreenCursor < len(runes) {
			m.ScreenCursor++
		}
	case input.KeyHome:
		m.ScreenCursor = 0
	case input.KeyEnd:
		m.ScreenCursor = len(runes)
	case input.KeyBackspace:
		if m.ScreenCursor > 0 && len(runes) > 0 {
			m.Screenshot = string(runes[:m.ScreenCursor-1]) + string(runes[m.ScreenCursor:])
			m.ScreenCursor--
			m.ValidateScreenshot()
		}
	case input.KeyDelete:
		if m.ScreenCursor < len(runes) {
			m.Screenshot = string(runes[:m.ScreenCursor]) + string(runes[m.ScreenCursor+1:])
			m.ValidateScreenshot()
		}
	default:
		if key.Rune != 0 {
			m.Screenshot = string(runes[:m.ScreenCursor]) + string(key.Rune) + string(runes[m.ScreenCursor:])
			m.ScreenCursor++
			m.ValidateScreenshot()
		}
	}
	return true, BRActionNone
}

// HandleMouse processes clicks inside the bug report modal.
func (m *BugReportModal) HandleMouse(ev input.Mouse, screenW, screenH int) (bool, BugReportAction) {
	if !m.Open {
		return false, BRActionNone
	}
	if ev.Button != input.MouseLeft {
		return true, BRActionNone
	}

	modalW := 78
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 24
	if modalH > screenH-2 {
		modalH = screenH - 2
	}
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	// Close X at top right
	if ev.Y == startY && ev.X == startX+modalW-2 {
		m.Open = false
		return true, BRActionClose
	}

	// Click on Title input (startY + 3)
	if ev.Y == startY+3 && ev.X >= startX+4 && ev.X <= startX+modalW-4 {
		m.FocusField = 0
		off := ev.X - (startX + 4)
		runes := []rune(m.Title)
		if off > len(runes) {
			off = len(runes)
		}
		m.TitleCursor = off
		return true, BRActionNone
	}

	// Click on Description input (startY + 6 .. startY + 11)
	if ev.Y >= startY+6 && ev.Y <= startY+11 && ev.X >= startX+4 && ev.X <= startX+modalW-4 {
		m.FocusField = 1
		return true, BRActionNone
	}

	// Click on Screenshot input (startY + 14)
	if ev.Y == startY+14 {
		if ev.X >= startX+4 && ev.X <= startX+modalW-18 {
			m.FocusField = 2
			off := ev.X - (startX + 4)
			runes := []rune(m.Screenshot)
			if off > len(runes) {
				off = len(runes)
			}
			m.ScreenCursor = off
			return true, BRActionNone
		}
		// Browse button (startX+modalW-16 .. startX+modalW-6)
		if ev.X >= startX+modalW-16 && ev.X <= startX+modalW-6 {
			m.FocusField = 3
			if m.Screenshot == "" {
				home, _ := os.UserHomeDir()
				m.Screenshot = filepath.Join(home, "screenshot.png")
				m.ValidateScreenshot()
			}
			return true, BRActionNone
		}
	}

	// Row: Checkbox + View Data (startY + 18)
	if ev.Y == startY+18 {
		// Checkbox (startX+4 .. startX+40)
		if ev.X >= startX+4 && ev.X <= startX+42 {
			m.FocusField = 4
			m.AttachLogs = !m.AttachLogs
			return true, BRActionNone
		}
		// View Data button (startX+46 .. startX+68)
		if ev.X >= startX+46 && ev.X <= startX+68 {
			m.FocusField = 5
			return true, BRActionViewData
		}
	}

	// Bottom action buttons (startY + modalH - 2)
	btnRowY := startY + modalH - 2
	if ev.Y == btnRowY {
		// Button: Отправить (startX+4 .. startX+28)
		if ev.X >= startX+4 && ev.X <= startX+28 {
			m.FocusField = 6
			return true, BRActionSend
		}
		// Button: Скопировать для GitHub (startX+30 .. startX+56)
		if ev.X >= startX+30 && ev.X <= startX+56 {
			m.FocusField = 7
			return true, BRActionCopyGitHub
		}
		// Button: Отмена (startX+58 .. startX+68)
		if ev.X >= startX+58 && ev.X <= startX+70 {
			m.FocusField = 8
			m.Open = false
			return true, BRActionClose
		}
	}

	return true, BRActionNone
}

// BuildReportRequest constructs a complete BugReportRequest.
func (m *BugReportModal) BuildReportRequest() crash.BugReportRequest {
	var logs []byte
	if m.AttachLogs {
		ringEntries := logging.GetRecentLogs(200)
		if len(ringEntries) > 0 {
			logs = []byte(strings.Join(ringEntries, "\n"))
		}
	}

	var screenshotPath string
	if m.ScreenshotValid {
		screenshotPath = strings.TrimSpace(m.Screenshot)
	}

	return crash.BugReportRequest{
		Title:       strings.TrimSpace(m.Title),
		Description: strings.TrimSpace(m.Description),
		Metadata: crash.BugReportMetadata{
			Platform:      crash.CurrentPlatformInfo(),
			AppVersion:    m.AppVersion,
			ActivePlugins: m.ActivePlugins,
		},
		Logs:           logs,
		ScreenshotPath: screenshotPath,
	}
}

// ExecuteSend sends the report payload to the server.
func (m *BugReportModal) ExecuteSend() error {
	if strings.TrimSpace(m.Title) == "" {
		m.StatusMsg = i18n.T("modal.bugreport.err_title")
		m.SendFailed = true
		return fmt.Errorf("title cannot be empty")
	}

	m.IsSending = true
	m.StatusMsg = i18n.T("modal.bugreport.sending")

	req := m.BuildReportRequest()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := crash.SendBugReport(ctx, m.Endpoint, req)
	m.IsSending = false

	if err != nil {
		m.SendFailed = true
		m.StatusMsg = i18n.T("modal.bugreport.send_fail")
		return err
	}

	if resp != nil && resp.Success {
		m.Open = false
		return nil
	}

	m.SendFailed = true
	m.StatusMsg = i18n.T("modal.bugreport.err_generic")
	return fmt.Errorf("send failed")
}

// GetGitHubMarkdown formats the issue for GitHub.
func (m *BugReportModal) GetGitHubMarkdown() string {
	req := m.BuildReportRequest()
	return crash.FormatGitHubIssue(req.Title, req.Description, req.Metadata, string(req.Logs), nil)
}

// Render draws the bug report modal into the buffer.
func (m *BugReportModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !m.Open {
		return
	}

	modalW := 78
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 24
	if modalH > screenH-2 {
		modalH = screenH - 2
	}
	startX := (screenW - modalW) / 2
	if startX < 2 {
		startX = 2
	}
	startY := (screenH - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	bg := toColor(theme.PopupBg)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	accentFg := toColor(theme.Keyword)
	btnBg := toColor(theme.PopupSelBg)
	focusBtnBg := toColor(theme.Keyword)
	focusBtnFg := toColor(theme.Background)
	fieldBg := toColor(theme.Background)
	successCol := toColor(theme.String)
	errorCol := toColor(theme.DiagnosticError)

	// Outer border
	for y := startY; y < startY+modalH; y++ {
		for x := startX; x < startX+modalW; x++ {
			r := ' '
			f := borderFg
			b := bg
			if y == startY && x == startX {
				r = '┌'
			} else if y == startY && x == startX+modalW-1 {
				r = '┐'
			} else if y == startY+modalH-1 && x == startX {
				r = '└'
			} else if y == startY+modalH-1 && x == startX+modalW-1 {
				r = '┘'
			} else if y == startY || y == startY+modalH-1 || y == startY+modalH-4 {
				r = '─'
			} else if x == startX || x == startX+modalW-1 {
				r = '│'
			}
			buf.SetRune(x, y, r, f, b, cell.AttrNone)
		}
	}

	// Title
	headerText := i18n.T("modal.bugreport.title")
	for i, r := range []rune(headerText) {
		if startX+2+i < startX+modalW-4 {
			buf.SetRune(startX+2+i, startY, r, accentFg, bg, cell.AttrBold)
		}
	}
	buf.SetRune(startX+modalW-2, startY, '✕', errorCol, bg, cell.AttrBold)

	// Field 0: Title
	lblTitle := i18n.T("modal.bugreport.field_title")
	drawString(buf, startX+4, startY+2, lblTitle, fg, bg, cell.AttrBold, modalW-8)

	f0Bg := fieldBg
	if m.FocusField == 0 {
		f0Bg = toColor(theme.PopupSelBg)
	}
	titleBoxW := modalW - 8
	for x := 0; x < titleBoxW; x++ {
		buf.SetRune(startX+4+x, startY+3, ' ', fg, f0Bg, cell.AttrNone)
	}
	titleRunes := []rune(m.Title)
	for i, r := range titleRunes {
		if i < titleBoxW {
			buf.SetRune(startX+4+i, startY+3, r, fg, f0Bg, cell.AttrNone)
		}
	}
	// Cursor
	if m.FocusField == 0 && m.TitleCursor <= titleBoxW {
		cursorChar := ' '
		if m.TitleCursor < len(titleRunes) {
			cursorChar = titleRunes[m.TitleCursor]
		}
		buf.SetRune(startX+4+m.TitleCursor, startY+3, cursorChar, focusBtnFg, focusBtnBg, cell.AttrNone)
	}

	// Field 1: Description
	lblDesc := i18n.T("modal.bugreport.field_desc")
	drawString(buf, startX+4, startY+5, lblDesc, fg, bg, cell.AttrBold, modalW-8)

	descBoxH := 5
	f1Bg := fieldBg
	if m.FocusField == 1 {
		f1Bg = toColor(theme.PopupSelBg)
	}
	for row := 0; row < descBoxH; row++ {
		for x := 0; x < titleBoxW; x++ {
			buf.SetRune(startX+4+x, startY+6+row, ' ', fg, f1Bg, cell.AttrNone)
		}
	}

	descLines := strings.Split(m.Description, "\n")
	for row, line := range descLines {
		if row < descBoxH {
			lRunes := []rune(line)
			for col, r := range lRunes {
				if col < titleBoxW {
					buf.SetRune(startX+4+col, startY+6+row, r, fg, f1Bg, cell.AttrNone)
				}
			}
		}
	}

	// Field 2: Screenshot
	lblScreen := i18n.T("modal.bugreport.field_screen")
	drawString(buf, startX+4, startY+12, lblScreen, fg, bg, cell.AttrBold, modalW-8)

	f2Bg := fieldBg
	if m.FocusField == 2 {
		f2Bg = toColor(theme.PopupSelBg)
	}
	screenFieldW := modalW - 22
	for x := 0; x < screenFieldW; x++ {
		buf.SetRune(startX+4+x, startY+13, ' ', fg, f2Bg, cell.AttrNone)
	}
	screenRunes := []rune(m.Screenshot)
	for i, r := range screenRunes {
		if i < screenFieldW {
			buf.SetRune(startX+4+i, startY+13, r, fg, f2Bg, cell.AttrNone)
		}
	}
	// Cursor
	if m.FocusField == 2 && m.ScreenCursor <= screenFieldW {
		cursorChar := ' '
		if m.ScreenCursor < len(screenRunes) {
			cursorChar = screenRunes[m.ScreenCursor]
		}
		buf.SetRune(startX+4+m.ScreenCursor, startY+13, cursorChar, focusBtnFg, focusBtnBg, cell.AttrNone)
	}

	// Button: Browse (strictly without square brackets)
	browseF, browseB := fg, btnBg
	if m.FocusField == 3 {
		browseF, browseB = focusBtnFg, focusBtnBg
	}
	drawButton(buf, startX+modalW-15, startY+13, i18n.T("btn.browse"), browseF, browseB)

	// Screenshot validation status line
	if m.ScreenshotStatus != "" {
		statCol := successCol
		if !m.ScreenshotValid {
			statCol = errorCol
		}
		drawString(buf, startX+4, startY+15, m.ScreenshotStatus, statCol, bg, cell.AttrNone, modalW-8)
	}

	// Field 4: Attach logs checkbox
	chkMark := "[ ] "
	if m.AttachLogs {
		chkMark = "[x] "
	}
	chkText := chkMark + i18n.T("modal.bugreport.attach_logs")
	chkF := fg
	if m.FocusField == 4 {
		chkF = accentFg
	}
	drawString(buf, startX+4, startY+17, chkText, chkF, bg, cell.AttrBold, modalW-8)

	// Field 5: Button: View Data (strictly without square brackets)
	viewDataF, viewDataB := fg, btnBg
	if m.FocusField == 5 {
		viewDataF, viewDataB = focusBtnFg, focusBtnBg
	}
	drawButton(buf, startX+50, startY+17, i18n.T("btn.view_data"), viewDataF, viewDataB)

	// Status text (sending, error)
	if m.StatusMsg != "" {
		statCol := accentFg
		if m.SendFailed {
			statCol = errorCol
		}
		drawString(buf, startX+4, startY+19, m.StatusMsg, statCol, bg, cell.AttrBold, modalW-8)
	}

	// Bottom action buttons (strictly without square brackets)
	btnRowY := startY + modalH - 2

	// Button 6: Send
	b6F, b6B := fg, btnBg
	if m.FocusField == 6 {
		b6F, b6B = focusBtnFg, focusBtnBg
	}
	drawButton(buf, startX+4, btnRowY, i18n.T("btn.send_ctrl_enter"), b6F, b6B)

	// Button 7: Copy for GitHub
	b7F, b7B := fg, btnBg
	if m.FocusField == 7 {
		b7F, b7B = focusBtnFg, focusBtnBg
	}
	drawButton(buf, startX+32, btnRowY, i18n.T("btn.copy_github"), b7F, b7B)

	// Button 8: Cancel
	b8F, b8B := fg, btnBg
	if m.FocusField == 8 {
		b8F, b8B = focusBtnFg, focusBtnBg
	}
	drawButton(buf, startX+60, btnRowY, i18n.T("btn.cancel"), b8F, b8B)
}
