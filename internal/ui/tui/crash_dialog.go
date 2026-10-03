package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/crash"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

// CrashDialogAction represents the user decision in the crash recovery dialog.
type CrashDialogAction string

const (
	CrashActionNone     CrashDialogAction = ""
	CrashActionSend     CrashDialogAction = "send"
	CrashActionDelete   CrashDialogAction = "delete"
	CrashActionIgnore   CrashDialogAction = "ignore"
	CrashActionViewLogs CrashDialogAction = "view_logs"
)

// CrashRecoveryDialog handles startup detection of prior session panics.
type CrashRecoveryDialog struct {
	Open          bool
	ReportPath    string
	Payload       *crash.CrashReportPayload
	FocusedButton int // 0: Send, 1: Delete, 2: Ignore, 3: View Details
	StatusMsg     string
	IsSending     bool
	SendFailed    bool
	Endpoint      string
}

// NewCrashRecoveryDialog initializes the dialog state.
func NewCrashRecoveryDialog() *CrashRecoveryDialog {
	return &CrashRecoveryDialog{
		Open:          false,
		FocusedButton: 0,
		Endpoint:      crash.DefaultReportEndpoint,
	}
}

// CheckAndOpen checks for pending crash files and loads the most recent if present.
func (d *CrashRecoveryDialog) CheckAndOpen() bool {
	reports, err := crash.FindPendingCrashReports()
	if err != nil || len(reports) == 0 {
		d.Open = false
		return false
	}

	latest := reports[0]
	payload, err := crash.LoadCrashReport(latest)
	if err != nil {
		return false
	}

	d.ReportPath = latest
	d.Payload = payload
	d.Open = true
	d.FocusedButton = 0
	d.StatusMsg = ""
	d.IsSending = false
	d.SendFailed = false
	return true
}

// HandleKey handles navigation and button activation.
func (d *CrashRecoveryDialog) HandleKey(key input.Key) (bool, CrashDialogAction) {
	if !d.Open {
		return false, CrashActionNone
	}

	if d.IsSending {
		return true, CrashActionNone
	}

	if key.Rune == 'v' || key.Rune == 'V' {
		return true, CrashActionViewLogs
	}

	switch key.Type {
	case input.KeyEsc:
		d.Open = false
		return true, CrashActionIgnore

	case input.KeyLeft:
		if d.FocusedButton > 0 {
			d.FocusedButton--
		}
		return true, CrashActionNone

	case input.KeyRight:
		if d.FocusedButton < 2 {
			d.FocusedButton++
		}
		return true, CrashActionNone

	case input.KeyTab:
		d.FocusedButton = (d.FocusedButton + 1) % 4
		return true, CrashActionNone

	case input.KeyBacktab:
		d.FocusedButton = (d.FocusedButton + 3) % 4
		return true, CrashActionNone

	case input.KeyEnter:
		if d.SendFailed {
			d.Open = false
			return true, CrashActionIgnore
		}
		switch d.FocusedButton {
		case 0:
			return true, CrashActionSend
		case 1:
			return true, CrashActionDelete
		case 2:
			d.Open = false
			return true, CrashActionIgnore
		case 3:
			return true, CrashActionViewLogs
		}
	}

	return true, CrashActionNone
}

// HandleMouse handles mouse clicks on dialog buttons.
func (d *CrashRecoveryDialog) HandleMouse(m input.Mouse, screenW, screenH int) (bool, CrashDialogAction) {
	if !d.Open {
		return false, CrashActionNone
	}
	if m.Button != input.MouseLeft {
		return true, CrashActionNone
	}

	modalW := 74
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 18
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	// Close X at top right
	if m.Y == startY && m.X == startX+modalW-2 {
		d.Open = false
		return true, CrashActionIgnore
	}

	// "Просмотреть содержимое отчета" button row (startY + 11)
	if m.Y == startY+11 && m.X >= startX+4 && m.X <= startX+38 {
		d.FocusedButton = 3
		return true, CrashActionViewLogs
	}

	// Action buttons bottom row (startY + 15)
	if m.Y == startY+15 {
		// Button 0: Отправить разработчику (startX+4 .. startX+38)
		if m.X >= startX+4 && m.X <= startX+38 {
			d.FocusedButton = 0
			return true, CrashActionSend
		}
		// Button 1: Удалить (startX+41 .. startX+52)
		if m.X >= startX+41 && m.X <= startX+52 {
			d.FocusedButton = 1
			return true, CrashActionDelete
		}
		// Button 2: Игнорировать (startX+54 .. startX+76)
		if m.X >= startX+54 && m.X <= startX+76 {
			d.FocusedButton = 2
			d.Open = false
			return true, CrashActionIgnore
		}
	}

	return true, CrashActionNone
}

// ExecuteSend performs synchronous/asynchronous sending of the crash dump.
func (d *CrashRecoveryDialog) ExecuteSend() error {
	if d.Payload == nil {
		return fmt.Errorf("no crash payload loaded")
	}

	d.IsSending = true
	d.StatusMsg = i18n.T("dialog.crash.sending")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := crash.SendCrashReport(ctx, d.Endpoint, d.Payload)
	d.IsSending = false

	if err != nil {
		d.SendFailed = true
		d.StatusMsg = i18n.T("dialog.crash.send_failed")
		return err
	}

	if resp != nil && resp.Success {
		_ = crash.DeleteCrashReport(d.ReportPath)
		d.Open = false
		return nil
	}

	d.SendFailed = true
	d.StatusMsg = i18n.T("dialog.crash.server_error")
	return fmt.Errorf("unexpected server response")
}

// ExecuteDelete deletes the current crash dump and closes the dialog.
func (d *CrashRecoveryDialog) ExecuteDelete() {
	if d.ReportPath != "" {
		_ = crash.DeleteCrashReport(d.ReportPath)
	}
	d.Open = false
}

// Render renders the crash recovery dialog into the buffer.
func (d *CrashRecoveryDialog) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !d.Open {
		return
	}

	modalW := 74
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 18
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
	warnCol := toColor(theme.DiagnosticWarn)
	accentFg := toColor(theme.Keyword)
	commentCol := toColor(theme.Comment)
	btnBg := toColor(theme.PopupSelBg)
	focusBtnBg := toColor(theme.Keyword)
	focusBtnFg := toColor(theme.Background)

	// Draw frame and background
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
			} else if y == startY || y == startY+modalH-1 || y == startY+2 || y == startY+modalH-4 {
				r = '─'
			} else if x == startX || x == startX+modalW-1 {
				r = '│'
			}
			buf.SetRune(x, y, r, f, b, cell.AttrNone)
		}
	}

	// Header
	headerText := i18n.T("dialog.crash.title")
	for i, r := range []rune(headerText) {
		if startX+2+i < startX+modalW-4 {
			buf.SetRune(startX+2+i, startY+1, r, warnCol, bg, cell.AttrBold)
		}
	}
	buf.SetRune(startX+modalW-2, startY+1, '✕', commentCol, bg, cell.AttrNone)

	// Content lines
	timeStr := ""
	reasonStr := "Unknown error"
	if d.Payload != nil {
		if d.Payload.Timestamp != "" {
			t, err := time.Parse(time.RFC3339, d.Payload.Timestamp)
			if err == nil {
				timeStr = t.Format("2006-01-02 15:04:05")
			} else {
				timeStr = d.Payload.Timestamp
			}
		}
		if d.Payload.PanicReason != "" {
			reasonStr = d.Payload.PanicReason
		}
	}

	var line1 string
	if timeStr != "" {
		line1 = i18n.T("dialog.crash.crashed_at", timeStr)
	} else {
		line1 = i18n.T("dialog.crash.crashed_recent")
	}
	drawString(buf, startX+4, startY+4, line1, fg, bg, cell.AttrNone, modalW-8)

	line2 := i18n.T("dialog.crash.reason", reasonStr)
	drawString(buf, startX+4, startY+5, line2, warnCol, bg, cell.AttrNone, modalW-8)

	dumpName := filepath.Base(d.ReportPath)
	line3 := i18n.T("dialog.crash.dump_file", dumpName)
	drawString(buf, startX+4, startY+7, line3, commentCol, bg, cell.AttrNone, modalW-8)

	info1 := i18n.T("dialog.crash.info1")
	info2 := i18n.T("dialog.crash.info2")
	drawString(buf, startX+4, startY+9, info1, fg, bg, cell.AttrNone, modalW-8)
	drawString(buf, startX+4, startY+10, info2, fg, bg, cell.AttrNone, modalW-8)

	btnViewText := i18n.T("dialog.crash.view_content")
	bF, bB := fg, btnBg
	if d.FocusedButton == 3 {
		bF, bB = focusBtnFg, focusBtnBg
	}
	drawButton(buf, startX+4, startY+12, btnViewText, bF, bB)

	// Status line if present
	if d.StatusMsg != "" {
		statCol := accentFg
		if d.SendFailed {
			statCol = toColor(theme.DiagnosticError)
		}
		drawString(buf, startX+4, startY+14, d.StatusMsg, statCol, bg, cell.AttrBold, modalW-8)
	}

	// Bottom action buttons (strictly NO square brackets)
	btnRowY := startY + 15
	if d.SendFailed {
		drawButton(buf, startX+4, btnRowY, i18n.T("dialog.crash.continue_work"), focusBtnFg, focusBtnBg)
	} else {
		b0F, b0B := fg, btnBg
		if d.FocusedButton == 0 {
			b0F, b0B = focusBtnFg, focusBtnBg
		}
		drawButton(buf, startX+4, btnRowY, i18n.T("btn.send_dev"), b0F, b0B)

		b1F, b1B := fg, btnBg
		if d.FocusedButton == 1 {
			b1F, b1B = focusBtnFg, focusBtnBg
		}
		drawButton(buf, startX+41, btnRowY, i18n.T("btn.delete"), b1F, b1B)

		b2F, b2B := fg, btnBg
		if d.FocusedButton == 2 {
			b2F, b2B = focusBtnFg, focusBtnBg
		}
		drawButton(buf, startX+54, btnRowY, i18n.T("btn.ignore"), b2F, b2B)
	}
}

// drawButton draws clean text on a styled background without any square brackets.
func drawButton(buf *buffer.Buffer, x, y int, text string, fg, bg cell.Color) {
	for i, r := range []rune(text) {
		buf.SetRune(x+i, y, r, fg, bg, cell.AttrNone)
	}
}

// drawString writes text with boundary clipping.
func drawString(buf *buffer.Buffer, x, y int, text string, fg, bg cell.Color, attr cell.Modifier, maxLen int) {
	runes := []rune(text)
	if len(runes) > maxLen {
		runes = runes[:maxLen]
	}
	for i, r := range runes {
		buf.SetRune(x+i, y, r, fg, bg, attr)
	}
}
