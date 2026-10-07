package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/style"
	"github.com/baibeicha/goatui/pkg/ui"
)

// DefaultToastDuration is the duration before notifications auto-expire (compact & fast).
const DefaultToastDuration = 2000 * time.Millisecond

// postToast adds a new toast notification with the configured default duration.
func (m *AppModel) postToast(level, title, message string) {
	if m.toasts == nil {
		return
	}
	switch strings.ToLower(level) {
	case "success", "ok":
		m.toasts.Add(ui.ToastSuccess, title, message, DefaultToastDuration)
	case "warn", "warning":
		m.toasts.Add(ui.ToastWarn, title, message, DefaultToastDuration)
	case "error", "err":
		m.toasts.Add(ui.ToastError, title, message, DefaultToastDuration)
	default:
		m.toasts.Add(ui.ToastInfo, title, message, DefaultToastDuration)
	}
}

// wrapToastText formats notification text into at most maxLines lines of maxWidth columns.
func wrapToastText(msg string, maxWidth, maxLines int) []string {
	if maxWidth <= 0 || maxLines <= 0 {
		return nil
	}
	words := strings.Fields(msg)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	var curLine strings.Builder

	for _, w := range words {
		wordLen := buffer.StringWidth(w)
		if curLine.Len() == 0 {
			if wordLen <= maxWidth {
				curLine.WriteString(w)
			} else {
				runes := []rune(w)
				for len(runes) > 0 {
					take := min(len(runes), maxWidth)
					lines = append(lines, string(runes[:take]))
					runes = runes[take:]
					if len(lines) >= maxLines {
						break
					}
				}
				if len(lines) >= maxLines {
					break
				}
			}
		} else {
			if buffer.StringWidth(curLine.String())+1+wordLen <= maxWidth {
				curLine.WriteByte(' ')
				curLine.WriteString(w)
			} else {
				lines = append(lines, curLine.String())
				curLine.Reset()
				if len(lines) >= maxLines {
					break
				}
				curLine.WriteString(w)
			}
		}
	}
	if curLine.Len() > 0 && len(lines) < maxLines {
		lines = append(lines, curLine.String())
	}

	if len(lines) == maxLines && buffer.StringWidth(msg) > buffer.StringWidth(strings.Join(lines, " ")) {
		last := lines[maxLines-1]
		runes := []rune(last)
		ellW := buffer.StringWidth("…")
		for len(runes) > 0 && buffer.StringWidth(string(runes))+ellW > maxWidth {
			runes = runes[:len(runes)-1]
		}
		lines[maxLines-1] = string(runes) + "…"
	}

	return lines
}

// handleToastClick checks if a mouse click hit any active toast card or its close button [✕].
func (m *AppModel) handleToastClick(x, y int) bool {
	if m.toasts == nil || m.toasts.Count() == 0 {
		return false
	}
	screen := buffer.Rect{X: 0, Y: 0, Width: m.width, Height: m.height}
	if screen.Width < 12 || screen.Height < 6 {
		return false
	}

	toastWidth := min(30, screen.Width-4)
	if toastWidth < 8 {
		return false
	}
	toastHeight := 5
	currY := screen.Y + 1

	for i := 0; i < m.toasts.Count(); i++ {
		if currY+toastHeight > screen.Bottom()-2 {
			break
		}
		cardX := screen.Right() - toastWidth - 1
		cardRect := buffer.NewRect(cardX, currY, toastWidth, toastHeight)
		if x >= cardRect.X && x < cardRect.Right() && y >= cardRect.Y && y < cardRect.Bottom() {
			m.toasts.Dismiss(i)
			return true
		}
		currY += toastHeight + 1
	}
	return false
}

// renderToasts renders active toast notifications into the top-right corner with compact 30-col width,
// 5-row height, 2-line message text wrapping, and close buttons.
func (m *AppModel) renderToasts(buf *buffer.Buffer, screen buffer.Rect) {
	if m.toasts == nil || m.toasts.Count() == 0 || screen.IsEmpty() || screen.Width < 12 || screen.Height < 6 {
		return
	}

	toastWidth := min(30, screen.Width-4)
	if toastWidth < 8 {
		return
	}
	toastHeight := 5

	now := time.Now()
	currY := screen.Y + 1

	for _, t := range m.toasts.Toasts() {
		if currY+toastHeight > screen.Bottom()-2 {
			break
		}

		cardX := screen.Right() - toastWidth - 1
		cardRect := buffer.NewRect(cardX, currY, toastWidth, toastHeight)

		borderFg := cell.ColorHex("#00D2FF")
		icon := "[INFO]"
		switch t.Level {
		case ui.ToastSuccess:
			borderFg = cell.ColorHex("#00FFAA")
			icon = "[OK]"
		case ui.ToastWarn:
			borderFg = cell.ColorHex("#FFB86C")
			icon = "[WARN]"
		case ui.ToastError:
			borderFg = cell.ColorHex("#FF5555")
			icon = "[ERR]"
		}

		cardBg := cell.Color256(234)

		// Draw card frame
		titleText := fmt.Sprintf(" %s %s ", icon, t.Title)
		st := style.NewStyle().
			Border(style.BorderRounded).
			BorderForeground(borderFg).
			BorderBackground(cardBg).
			Background(cardBg).
			Title(titleText)
		st.Draw(buf, cardRect, "")

		// Draw close button [✕] in top-right of card
		closeBtnX := cardRect.Right() - 3
		if closeBtnX > cardRect.X+len(titleText) {
			buf.SetRune(closeBtnX, cardRect.Y, '✕', cell.ColorHex("#FF5555"), cardBg, cell.AttrBold)
		}

		// Message text (wrapped up to 2 lines)
		innerMsgW := cardRect.Width - 4
		if innerMsgW > 3 {
			lines := wrapToastText(t.Message, innerMsgW, 2)
			for li, line := range lines {
				if li < 2 {
					buf.SetString(cardRect.X+2, cardRect.Y+1+li, line, cell.ColorHex("#FFFFFF"), cardBg, cell.AttrNone)
				}
			}
		}

		// Expiration bar (row cardRect.Y+3)
		remaining := t.ExpiresAt.Sub(now)
		progressRatio := float64(remaining) / float64(t.Duration)
		if progressRatio < 0 {
			progressRatio = 0
		} else if progressRatio > 1.0 {
			progressRatio = 1.0
		}

		barW := cardRect.Width - 4
		filledW := int(float64(barW) * progressRatio)
		for i := 0; i < barW; i++ {
			bx := cardRect.X + 2 + i
			by := cardRect.Y + 3
			if i < filledW {
				buf.SetRune(bx, by, '━', borderFg, cardBg, cell.AttrNone)
			} else {
				buf.SetRune(bx, by, '─', cell.ColorHex("#444455"), cardBg, cell.AttrDim)
			}
		}

		currY += toastHeight + 1
	}
}
