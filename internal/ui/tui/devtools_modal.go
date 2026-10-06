package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/clipboard"
	"tahr/internal/core/devtools"
	"tahr/internal/ui"
)

// DevToolsTab identifies the active tool tab inside DevToolsModal.
type DevToolsTab int

const (
	DevToolsTabJWT DevToolsTab = iota
	DevToolsTabUUID
	DevToolsTabBase64
	DevToolsTabHash
	DevToolsTabTimestamp
)

// DevToolsModalButtonHit records the clickable area of an action pill button.
type DevToolsModalButtonHit struct {
	Action string
	X, Y   int
	Width  int
}

// DevToolsModal provides an integrated developer utility toolset (JWT, UUID, Base64, Hash, Timestamp).
type DevToolsModal struct {
	Open       bool
	ActiveTab  DevToolsTab
	FocusField int // 0: Input, 1: Output

	// Input and Output text buffers
	InputText  string
	OutputText string
	StatusMsg  string
	IsError    bool

	// Tab-specific options
	HashAlgo      string
	Base64URLSafe bool
	UUIDVersion   int // 4 or 7

	ButtonHits []DevToolsModalButtonHit
	OnToast    func(level, title, msg string)
}

// NewDevToolsModal creates a new DevTools modal.
func NewDevToolsModal() *DevToolsModal {
	return &DevToolsModal{
		Open:        false,
		ActiveTab:   DevToolsTabJWT,
		FocusField:  0,
		HashAlgo:    "sha256",
		UUIDVersion: 4,
	}
}

// Show opens the modal and focuses the input.
func (m *DevToolsModal) Show() {
	m.Open = true
	m.FocusField = 0
	m.StatusMsg = ""
	m.IsError = false
	if m.ActiveTab == DevToolsTabUUID && m.OutputText == "" {
		m.Execute()
	}
}

// Close closes the modal.
func (m *DevToolsModal) Close() {
	m.Open = false
	m.StatusMsg = ""
}

// SetTab switches the active tab.
func (m *DevToolsModal) SetTab(tab DevToolsTab) {
	m.ActiveTab = tab
	m.InputText = ""
	m.OutputText = ""
	m.StatusMsg = ""
	m.IsError = false
	if tab == DevToolsTabUUID {
		m.Execute()
	}
}

// Execute performs the active utility conversion.
func (m *DevToolsModal) Execute() {
	m.StatusMsg = ""
	m.IsError = false

	switch m.ActiveTab {
	case DevToolsTabJWT:
		m.executeJWT()
	case DevToolsTabUUID:
		m.executeUUID()
	case DevToolsTabBase64:
		m.executeBase64Encode()
	case DevToolsTabHash:
		m.executeHash()
	case DevToolsTabTimestamp:
		m.executeTimestamp()
	}
}

func (m *DevToolsModal) executeJWT() {
	token := strings.TrimSpace(m.InputText)
	if token == "" {
		m.OutputText = ""
		m.StatusMsg = "Enter a valid JWT token string"
		m.IsError = true
		return
	}

	decoded, err := devtools.ParseJWT(token)
	if err != nil {
		m.OutputText = ""
		m.StatusMsg = "JWT error: " + err.Error()
		m.IsError = true
		return
	}

	m.OutputText = fmt.Sprintf("── HEADER ──\n%s\n\n── PAYLOAD ──\n%s\n\n── SIGNATURE ──\n%s",
		decoded.HeaderJSON, decoded.PayloadJSON, decoded.SignatureHex)
	m.StatusMsg = "JWT decoded successfully"
}

func (m *DevToolsModal) executeUUID() {
	uuids := devtools.GenerateUUID(1, false, true)
	if len(uuids) > 0 {
		m.OutputText = uuids[0]
		m.StatusMsg = "Generated UUID v4"
	}
}

func (m *DevToolsModal) executeBase64Encode() {
	encoded := devtools.Base64Encode([]byte(m.InputText))
	m.OutputText = encoded
	m.StatusMsg = "Base64 encoded successfully"
}

func (m *DevToolsModal) executeBase64Decode() {
	decoded, err := devtools.Base64Decode(strings.TrimSpace(m.InputText))
	if err != nil {
		m.OutputText = ""
		m.StatusMsg = "Base64 decode error: " + err.Error()
		m.IsError = true
		return
	}
	m.OutputText = string(decoded)
	m.StatusMsg = "Base64 decoded successfully"
}

func (m *DevToolsModal) executeHash() {
	data := []byte(m.InputText)
	var hashVal string
	switch strings.ToLower(m.HashAlgo) {
	case "md5":
		hashVal = devtools.MD5(data)
	case "sha1":
		hashVal = devtools.SHA1(data)
	case "sha512":
		hashVal = devtools.SHA512(data)
	default:
		hashVal = devtools.SHA256(data)
	}
	m.OutputText = hashVal
	m.StatusMsg = fmt.Sprintf("%s calculated (%d bytes input)", strings.ToUpper(m.HashAlgo), len(data))
}

func (m *DevToolsModal) executeTimestamp() {
	inputStr := strings.TrimSpace(m.InputText)
	var info *devtools.TimestampInfo
	var err error

	if inputStr == "" || strings.EqualFold(inputStr, "now") {
		info = devtools.FromTime(time.Now())
	} else {
		info, err = devtools.ParseTimestamp(inputStr)
		if err != nil {
			m.OutputText = ""
			m.StatusMsg = "Timestamp parse error: " + err.Error()
			m.IsError = true
			return
		}
	}

	m.OutputText = fmt.Sprintf("Unix Sec:      %d\nUnix Millis:   %d\nISO 8601 UTC:  %s\nISO 8601 Local: %s\nHuman UTC:     %s\nRelative:      %s",
		info.UnixSec, info.UnixMilli, info.ISO8601UTC, info.ISO8601Local, info.HumanUTC, info.Relative)
	m.StatusMsg = "Timestamp converted"
}

// CopyOutput copies current output text to system clipboard.
func (m *DevToolsModal) CopyOutput() {
	if m.OutputText == "" {
		return
	}
	_ = clipboard.Write(m.OutputText)
	m.StatusMsg = "Copied output to clipboard"
	if m.OnToast != nil {
		m.OnToast("info", "DEVTOOLS", "Copied to clipboard")
	}
}

// HandleKey handles interactive keyboard events.
func (m *DevToolsModal) HandleKey(k input.Key) bool {
	if !m.Open {
		return false
	}

	switch k.Type {
	case input.KeyEsc:
		m.Close()
		return true

	case input.KeyTab:
		m.ActiveTab = (m.ActiveTab + 1) % 5
		m.SetTab(m.ActiveTab)
		return true

	case input.KeyBacktab:
		m.ActiveTab = (m.ActiveTab + 4) % 5
		m.SetTab(m.ActiveTab)
		return true

	case input.KeyEnter:
		m.Execute()
		return true

	case input.KeyBackspace:
		if len(m.InputText) > 0 {
			m.InputText = m.InputText[:len(m.InputText)-1]
		}
		return true
	}

	if k.Rune != 0 {
		m.InputText += string(k.Rune)
		return true
	}

	return false
}

// HandleClick processes mouse clicks on tabs and action buttons.
func (m *DevToolsModal) HandleClick(x, y int) bool {
	if !m.Open {
		return false
	}

	for _, hit := range m.ButtonHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.Width {
			switch hit.Action {
			case "tab_jwt":
				m.SetTab(DevToolsTabJWT)
			case "tab_uuid":
				m.SetTab(DevToolsTabUUID)
			case "tab_base64":
				m.SetTab(DevToolsTabBase64)
			case "tab_hash":
				m.SetTab(DevToolsTabHash)
			case "tab_ts":
				m.SetTab(DevToolsTabTimestamp)
			case "execute":
				m.Execute()
			case "decode_b64":
				m.executeBase64Decode()
			case "encode_b64":
				m.executeBase64Encode()
			case "copy":
				m.CopyOutput()
			case "clear":
				m.InputText = ""
				m.OutputText = ""
				m.StatusMsg = ""
			case "close":
				m.Close()
			case "uuid_v4":
				m.UUIDVersion = 4
				m.executeUUID()
			case "uuid_v7":
				m.UUIDVersion = 7
				m.executeUUID()
			case "hash_md5":
				m.HashAlgo = "md5"
				m.executeHash()
			case "hash_sha1":
				m.HashAlgo = "sha1"
				m.executeHash()
			case "hash_sha256":
				m.HashAlgo = "sha256"
				m.executeHash()
			case "hash_sha512":
				m.HashAlgo = "sha512"
				m.executeHash()
			}
			return true
		}
	}

	return false
}


// Render draws the DevTools modal centered on the screen.
func (m *DevToolsModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !m.Open {
		return
	}

	modalW := 80
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 24
	if modalH > screenH-4 {
		modalH = screenH - 4
	}

	x := (screenW - modalW) / 2
	y := (screenH - modalH) / 2

	m.ButtonHits = m.ButtonHits[:0]

	bg := toColor(theme.StatusBarBg)
	cardBg := toColor(theme.Background)
	textFg := toColor(theme.Foreground)
	dimFg := toColor(theme.Comment)
	borderFg := toColor(theme.BorderColor)
	selBg := toColor(theme.SelectionBg)
	accentFg := toColor(theme.Function)
	errFg := toColor(theme.DiagnosticError)
	greenFg := toColor(theme.String)

	// Fill backdrop shadow
	for row := 0; row < modalH; row++ {
		for col := 0; col < modalW; col++ {
			buf.SetRune(x+col, y+row, ' ', textFg, bg, cell.AttrNone)
		}
	}

	// Draw border
	drawBorderBox(buf, x, y, modalW, modalH, borderFg, bg)

	// Title
	title := " Developer Utilities (DevTools) "
	drawText(buf, x+2, y, title, accentFg, bg, cell.AttrBold)

	// Close button
	closeLabel := " Close "
	closeX := x + modalW - len(closeLabel) - 2
	drawText(buf, closeX, y, closeLabel, dimFg, toColor(theme.CursorLineBg), cell.AttrNone)
	m.ButtonHits = append(m.ButtonHits, DevToolsModalButtonHit{
		Action: "close",
		X:      closeX,
		Y:      y,
		Width:  len(closeLabel),
	})

	curY := y + 2

	// Tabs row
	tabs := []struct {
		Name   string
		Tab    DevToolsTab
		Action string
	}{
		{" JWT Decoder ", DevToolsTabJWT, "tab_jwt"},
		{" UUID Generator ", DevToolsTabUUID, "tab_uuid"},
		{" Base64 ", DevToolsTabBase64, "tab_base64"},
		{" Hash ", DevToolsTabHash, "tab_hash"},
		{" Timestamp ", DevToolsTabTimestamp, "tab_ts"},
	}

	tabX := x + 2
	for _, t := range tabs {
		isSelected := m.ActiveTab == t.Tab
		tabBg := toColor(theme.CursorLineBg)
		tabFg := dimFg
		if isSelected {
			tabBg = selBg
			tabFg = textFg
		}
		drawText(buf, tabX, curY, t.Name, tabFg, tabBg, cell.AttrBold)
		m.ButtonHits = append(m.ButtonHits, DevToolsModalButtonHit{
			Action: t.Action,
			X:      tabX,
			Y:      curY,
			Width:  len(t.Name),
		})
		tabX += len(t.Name) + 1
	}
	curY += 2

	// Tab-specific controls / buttons
	switch m.ActiveTab {
	case DevToolsTabUUID:
		ctrls := []struct {
			Label  string
			Action string
			Active bool
		}{
			{" UUID v4 ", "uuid_v4", m.UUIDVersion == 4},
			{" UUID v7 ", "uuid_v7", m.UUIDVersion == 7},
			{" Generate New ", "execute", false},
			{" Copy ", "copy", false},
		}
		btnX := x + 2
		for _, c := range ctrls {
			btnBg := toColor(theme.CursorLineBg)
			btnFg := textFg
			if c.Active {
				btnBg = selBg
			}
			drawText(buf, btnX, curY, c.Label, btnFg, btnBg, cell.AttrNone)
			m.ButtonHits = append(m.ButtonHits, DevToolsModalButtonHit{
				Action: c.Action,
				X:      btnX,
				Y:      curY,
				Width:  len(c.Label),
			})
			btnX += len(c.Label) + 1
		}
		curY += 2

	case DevToolsTabHash:
		ctrls := []struct {
			Label  string
			Action string
			Active bool
		}{
			{" SHA-256 ", "hash_sha256", m.HashAlgo == "sha256"},
			{" SHA-512 ", "hash_sha512", m.HashAlgo == "sha512"},
			{" MD5 ", "hash_md5", m.HashAlgo == "md5"},
			{" SHA-1 ", "hash_sha1", m.HashAlgo == "sha1"},
			{" Calculate ", "execute", false},
			{" Copy ", "copy", false},
		}
		btnX := x + 2
		for _, c := range ctrls {
			btnBg := toColor(theme.CursorLineBg)
			btnFg := textFg
			if c.Active {
				btnBg = selBg
			}
			drawText(buf, btnX, curY, c.Label, btnFg, btnBg, cell.AttrNone)
			m.ButtonHits = append(m.ButtonHits, DevToolsModalButtonHit{
				Action: c.Action,
				X:      btnX,
				Y:      curY,
				Width:  len(c.Label),
			})
			btnX += len(c.Label) + 1
		}
		curY += 2

	case DevToolsTabBase64:
		ctrls := []struct {
			Label  string
			Action string
		}{
			{" Encode ", "encode_b64"},
			{" Decode ", "decode_b64"},
			{" Copy ", "copy"},
			{" Clear ", "clear"},
		}
		btnX := x + 2
		for _, c := range ctrls {
			drawText(buf, btnX, curY, c.Label, textFg, toColor(theme.CursorLineBg), cell.AttrNone)
			m.ButtonHits = append(m.ButtonHits, DevToolsModalButtonHit{
				Action: c.Action,
				X:      btnX,
				Y:      curY,
				Width:  len(c.Label),
			})
			btnX += len(c.Label) + 1
		}
		curY += 2

	default:
		ctrls := []struct {
			Label  string
			Action string
		}{
			{" Execute ", "execute"},
			{" Copy ", "copy"},
			{" Clear ", "clear"},
		}
		btnX := x + 2
		for _, c := range ctrls {
			drawText(buf, btnX, curY, c.Label, textFg, toColor(theme.CursorLineBg), cell.AttrNone)
			m.ButtonHits = append(m.ButtonHits, DevToolsModalButtonHit{
				Action: c.Action,
				X:      btnX,
				Y:      curY,
				Width:  len(c.Label),
			})
			btnX += len(c.Label) + 1
		}
		curY += 2
	}

	// Input box
	inputLabel := "Input:"
	if m.ActiveTab == DevToolsTabJWT {
		inputLabel = "JWT Token Input:"
	} else if m.ActiveTab == DevToolsTabTimestamp {
		inputLabel = "Timestamp Input (unix ms / ISO 8601 / 'now'):"
	}
	drawText(buf, x+2, curY, inputLabel, dimFg, bg, cell.AttrNone)
	curY++

	inputBoxH := 3
	drawBorderBox(buf, x+2, curY, modalW-4, inputBoxH, borderFg, cardBg)
	dispInput := m.InputText
	if dispInput == "" {
		dispInput = "Type here and press Enter..."
		drawText(buf, x+3, curY+1, dispInput, dimFg, cardBg, cell.AttrNone)
	} else {
		if len(dispInput) > modalW-6 {
			dispInput = dispInput[len(dispInput)-(modalW-6):]
		}
		drawText(buf, x+3, curY+1, dispInput, textFg, cardBg, cell.AttrNone)
	}
	curY += inputBoxH + 1

	// Output box
	drawText(buf, x+2, curY, "Output Result:", dimFg, bg, cell.AttrNone)
	curY++

	outputBoxH := y + modalH - curY - 2
	if outputBoxH > 2 {
		drawBorderBox(buf, x+2, curY, modalW-4, outputBoxH, borderFg, cardBg)
		outLines := strings.Split(m.OutputText, "\n")
		for i, line := range outLines {
			if i >= outputBoxH-2 {
				break
			}
			if len(line) > modalW-6 {
				line = line[:modalW-6]
			}
			drawText(buf, x+3, curY+1+i, line, textFg, cardBg, cell.AttrNone)
		}
	}

	// Status line at bottom
	if m.StatusMsg != "" {
		statusFg := greenFg
		if m.IsError {
			statusFg = errFg
		}
		drawText(buf, x+2, y+modalH-2, "● "+m.StatusMsg, statusFg, bg, cell.AttrNone)
	}
}

func drawBorderBox(buf *buffer.Buffer, x, y, w, h int, fg, bg cell.Color) {
	if w <= 1 || h <= 1 {
		return
	}
	// Corners
	buf.SetRune(x, y, '┌', fg, bg, cell.AttrNone)
	buf.SetRune(x+w-1, y, '┐', fg, bg, cell.AttrNone)
	buf.SetRune(x, y+h-1, '└', fg, bg, cell.AttrNone)
	buf.SetRune(x+w-1, y+h-1, '┘', fg, bg, cell.AttrNone)

	// Horizontal edges
	for col := 1; col < w-1; col++ {
		buf.SetRune(x+col, y, '─', fg, bg, cell.AttrNone)
		buf.SetRune(x+col, y+h-1, '─', fg, bg, cell.AttrNone)
	}
	// Vertical edges
	for row := 1; row < h-1; row++ {
		buf.SetRune(x, y+row, '│', fg, bg, cell.AttrNone)
		buf.SetRune(x+w-1, y+row, '│', fg, bg, cell.AttrNone)
	}
}
