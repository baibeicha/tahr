package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/db"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

// DBTypeOption holds standard defaults for a specific database engine.
type DBTypeOption struct {
	Name        string
	DefaultPort string
	DefaultUser string
	DefaultDB   string
	IsFileBased bool
	DefaultFile string
}

var standardDBTypes = []DBTypeOption{
	{Name: "PostgreSQL", DefaultPort: "5432", DefaultUser: "postgres", DefaultDB: "postgres", IsFileBased: false},
	{Name: "MySQL", DefaultPort: "3306", DefaultUser: "root", DefaultDB: "mysql", IsFileBased: false},
	{Name: "MariaDB", DefaultPort: "3306", DefaultUser: "root", DefaultDB: "mariadb", IsFileBased: false},
	{Name: "SQLite", DefaultPort: "", DefaultUser: "", DefaultDB: "", IsFileBased: true, DefaultFile: "./app.db"},
	{Name: "DuckDB", DefaultPort: "", DefaultUser: "", DefaultDB: "", IsFileBased: true, DefaultFile: "./app.duckdb"},
	{Name: "MSSQL", DefaultPort: "1433", DefaultUser: "sa", DefaultDB: "master", IsFileBased: false},
	{Name: "CockroachDB", DefaultPort: "26257", DefaultUser: "root", DefaultDB: "defaultdb", IsFileBased: false},
	{Name: "ClickHouse", DefaultPort: "8123", DefaultUser: "default", DefaultDB: "default", IsFileBased: false},
	{Name: "Redis", DefaultPort: "6379", DefaultUser: "", DefaultDB: "0", IsFileBased: false},
}

// DBConnectModal provides a JetBrains-style database connection dialog.
type DBConnectModal struct {
	Visible       bool
	Types         []DBTypeOption
	SelectedType  int
	ActiveField   int // 0=Type, 1=Host, 2=Port, 3=DBName, 4=User, 5=Password, 6=FilePath, 7=TestBtn, 8=ConnectBtn, 9=CancelBtn
	Host          string
	Port          string
	Database      string
	User          string
	Password      string
	FilePath      string
	StatusMsg     string
	StatusSuccess bool
	Theme         *ui.Theme

	OnSave func(profile db.ConnectionProfile)
}

// NewDBConnectModal constructs the database connection modal with sane defaults.
func NewDBConnectModal(theme *ui.Theme) *DBConnectModal {
	m := &DBConnectModal{
		Visible:      false,
		Types:        standardDBTypes,
		SelectedType: 0,
		ActiveField:  1,
		Host:         "localhost",
		Port:         "5432",
		Database:     "postgres",
		User:         "postgres",
		Password:     "",
		FilePath:     "./app.db",
		Theme:        theme,
	}
	return m
}

// OpenDialog displays the connection modal and resets status.
func (m *DBConnectModal) OpenDialog() {
	m.Visible = true
	m.StatusMsg = ""
	m.ActiveField = 0
}

// CloseDialog hides the connection modal.
func (m *DBConnectModal) CloseDialog() {
	m.Visible = false
}

// SwitchType updates the active database engine and fills standard defaults.
func (m *DBConnectModal) SwitchType(delta int) {
	tot := len(m.Types)
	m.SelectedType = (m.SelectedType + delta + tot) % tot
	cur := m.Types[m.SelectedType]
	m.Port = cur.DefaultPort
	if cur.DefaultDB != "" {
		m.Database = cur.DefaultDB
	}
	if cur.DefaultUser != "" {
		m.User = cur.DefaultUser
	}
	if cur.DefaultFile != "" {
		m.FilePath = cur.DefaultFile
	}
	m.StatusMsg = ""
}

// CurrentProfile returns the configured connection profile.
func (m *DBConnectModal) CurrentProfile() db.ConnectionProfile {
	cur := m.Types[m.SelectedType]
	return db.ConnectionProfile{
		Type:     cur.Name,
		Host:     m.Host,
		Port:     m.Port,
		Database: m.Database,
		User:     m.User,
		Password: m.Password,
		FilePath: m.FilePath,
	}
}

// TestConnection simulates a live driver ping and schema probe.
func (m *DBConnectModal) TestConnection() {
	cur := m.Types[m.SelectedType]
	if cur.IsFileBased {
		if strings.TrimSpace(m.FilePath) == "" {
			m.StatusMsg = i18n.T("dbconnect.err_path")
			m.StatusSuccess = false
			return
		}
		m.StatusMsg = fmt.Sprintf(i18n.T("dbconnect.success_file"), cur.Name)
		m.StatusSuccess = true
		return
	}

	if strings.TrimSpace(m.Host) == "" {
		m.StatusMsg = i18n.T("dbconnect.err_host")
		m.StatusSuccess = false
		return
	}
	if strings.TrimSpace(m.Port) == "" {
		m.StatusMsg = i18n.T("dbconnect.err_port")
		m.StatusSuccess = false
		return
	}

	m.StatusMsg = fmt.Sprintf(i18n.T("dbconnect.success_net"), cur.Name, m.Host, m.Port)
	m.StatusSuccess = true
}

// HandleKey processes keyboard input when modal is visible. Returns true if key was consumed.
func (m *DBConnectModal) HandleKey(k input.Key) bool {
	if !m.Visible {
		return false
	}

	cur := m.Types[m.SelectedType]

	// Esc: close modal
	if k.Type == input.KeyEsc {
		m.CloseDialog()
		return true
	}

	// Tab / Shift+Tab navigation
	if k.Type == input.KeyTab {
		if k.HasShift() {
			m.prevField(cur.IsFileBased)
		} else {
			m.nextField(cur.IsFileBased)
		}
		return true
	}
	if k.Type == input.KeyDown {
		m.nextField(cur.IsFileBased)
		return true
	}
	if k.Type == input.KeyUp {
		m.prevField(cur.IsFileBased)
		return true
	}

	// Left/Right arrow on type selector
	if m.ActiveField == 0 {
		if k.Type == input.KeyLeft {
			m.SwitchType(-1)
			return true
		}
		if k.Type == input.KeyRight {
			m.SwitchType(1)
			return true
		}
	}

	// Enter: execute buttons or submit
	if k.Type == input.KeyEnter {
		switch m.ActiveField {
		case 7: // Test Connection
			m.TestConnection()
			return true
		case 8: // Save & Connect
			if m.OnSave != nil {
				m.OnSave(m.CurrentProfile())
			}
			m.CloseDialog()
			return true
		case 9: // Cancel
			m.CloseDialog()
			return true
		default:
			m.nextField(cur.IsFileBased)
			return true
		}
	}

	// Backspace
	if k.Type == input.KeyBackspace {
		m.deleteChar()
		return true
	}

	// Typing runes into active input field
	if k.Type == input.KeyRune || (k.Type == input.KeySpace && k.Rune == ' ') {
		m.insertChar(k.Rune)
		return true
	}

	return true
}

func (m *DBConnectModal) nextField(isFile bool) {
	if isFile {
		// Flow: 0(Type) -> 6(FilePath) -> 7(Test) -> 8(Connect) -> 9(Cancel) -> 0
		switch m.ActiveField {
		case 0:
			m.ActiveField = 6
		case 6:
			m.ActiveField = 7
		case 7:
			m.ActiveField = 8
		case 8:
			m.ActiveField = 9
		default:
			m.ActiveField = 0
		}
	} else {
		// Flow: 0(Type) -> 1(Host) -> 2(Port) -> 3(DB) -> 4(User) -> 5(Pass) -> 7(Test) -> 8(Connect) -> 9(Cancel) -> 0
		switch m.ActiveField {
		case 0:
			m.ActiveField = 1
		case 1:
			m.ActiveField = 2
		case 2:
			m.ActiveField = 3
		case 3:
			m.ActiveField = 4
		case 4:
			m.ActiveField = 5
		case 5:
			m.ActiveField = 7
		case 7:
			m.ActiveField = 8
		case 8:
			m.ActiveField = 9
		default:
			m.ActiveField = 0
		}
	}
}

func (m *DBConnectModal) prevField(isFile bool) {
	if isFile {
		switch m.ActiveField {
		case 0:
			m.ActiveField = 9
		case 6:
			m.ActiveField = 0
		case 7:
			m.ActiveField = 6
		case 8:
			m.ActiveField = 7
		case 9:
			m.ActiveField = 8
		default:
			m.ActiveField = 0
		}
	} else {
		switch m.ActiveField {
		case 0:
			m.ActiveField = 9
		case 1:
			m.ActiveField = 0
		case 2:
			m.ActiveField = 1
		case 3:
			m.ActiveField = 2
		case 4:
			m.ActiveField = 3
		case 5:
			m.ActiveField = 4
		case 7:
			m.ActiveField = 5
		case 8:
			m.ActiveField = 7
		case 9:
			m.ActiveField = 8
		default:
			m.ActiveField = 0
		}
	}
}

func (m *DBConnectModal) insertChar(r rune) {
	switch m.ActiveField {
	case 1:
		m.Host += string(r)
	case 2:
		m.Port += string(r)
	case 3:
		m.Database += string(r)
	case 4:
		m.User += string(r)
	case 5:
		m.Password += string(r)
	case 6:
		m.FilePath += string(r)
	}
	m.StatusMsg = ""
}

func (m *DBConnectModal) deleteChar() {
	switch m.ActiveField {
	case 1:
		if len(m.Host) > 0 {
			m.Host = m.Host[:len(m.Host)-1]
		}
	case 2:
		if len(m.Port) > 0 {
			m.Port = m.Port[:len(m.Port)-1]
		}
	case 3:
		if len(m.Database) > 0 {
			m.Database = m.Database[:len(m.Database)-1]
		}
	case 4:
		if len(m.User) > 0 {
			m.User = m.User[:len(m.User)-1]
		}
	case 5:
		if len(m.Password) > 0 {
			m.Password = m.Password[:len(m.Password)-1]
		}
	case 6:
		if len(m.FilePath) > 0 {
			m.FilePath = m.FilePath[:len(m.FilePath)-1]
		}
	}
	m.StatusMsg = ""
}

// Render draws the modal dialog centered on the terminal screen.
func (m *DBConnectModal) Render(buf *buffer.Buffer, screenW, screenH int) {
	if !m.Visible || screenW < 40 || screenH < 15 {
		return
	}

	dialogW := 64
	dialogH := 18
	if dialogW > screenW-4 {
		dialogW = screenW - 4
	}
	if dialogH > screenH-2 {
		dialogH = screenH - 2
	}

	startX := (screenW - dialogW) / 2
	startY := (screenH - dialogH) / 2

	bg := toColor(m.Theme.Background)
	fg := toColor(m.Theme.Foreground)
	borderFg := toColor(m.Theme.BorderColor)
	hdrBg := toColor(m.Theme.GutterBg)
	hdrFg := toColor(m.Theme.Function)
	accentFg := toColor(m.Theme.Keyword)
	focusBg := toColor(m.Theme.PopupSelBg)
	focusFg := toColor(m.Theme.PopupSelFg)
	successFg := toColor(m.Theme.String)
	errorFg := toColor(m.Theme.DiagnosticError)

	// Draw box background
	for y := startY; y < startY+dialogH; y++ {
		for x := startX; x < startX+dialogW; x++ {
			buf.SetRune(x, y, ' ', fg, bg, cell.AttrNone)
		}
	}

	// Draw outer border
	for x := startX; x < startX+dialogW; x++ {
		buf.SetRune(x, startY, '─', borderFg, bg, cell.AttrNone)
		buf.SetRune(x, startY+dialogH-1, '─', borderFg, bg, cell.AttrNone)
	}
	for y := startY; y < startY+dialogH; y++ {
		buf.SetRune(startX, y, '│', borderFg, bg, cell.AttrNone)
		buf.SetRune(startX+dialogW-1, y, '│', borderFg, bg, cell.AttrNone)
	}
	buf.SetRune(startX, startY, '╭', borderFg, bg, cell.AttrNone)
	buf.SetRune(startX+dialogW-1, startY, '╮', borderFg, bg, cell.AttrNone)
	buf.SetRune(startX, startY+dialogH-1, '╰', borderFg, bg, cell.AttrNone)
	buf.SetRune(startX+dialogW-1, startY+dialogH-1, '╯', borderFg, bg, cell.AttrNone)

	// Header row
	title := i18n.T("dbconnect.title")
	for i, r := range []rune(title) {
		buf.SetRune(startX+2+i, startY, r, hdrFg, hdrBg, cell.AttrBold)
	}

	cur := m.Types[m.SelectedType]

	// Row 1: DB Type Selector
	typeY := startY + 2
	printLabel(buf, startX+3, typeY, i18n.T("dbconnect.label_type"), accentFg, bg)
	typeStr := fmt.Sprintf(" ◀  %s  ▶ ", cur.Name)
	tFg := fg
	tBg := bg
	if m.ActiveField == 0 {
		tFg = focusFg
		tBg = focusBg
	}
	printBox(buf, startX+16, typeY, 32, typeStr, tFg, tBg, borderFg)

	if cur.IsFileBased {
		// Row 2: File Path
		fileY := startY + 5
		printLabel(buf, startX+3, fileY, i18n.T("dbconnect.label_file"), accentFg, bg)
		fVal := m.FilePath
		fFg := fg
		fBg := bg
		if m.ActiveField == 6 {
			fFg = focusFg
			fBg = focusBg
			fVal += "_"
		}
		printBox(buf, startX+16, fileY, 42, fVal, fFg, fBg, borderFg)
	} else {
		// Row 2: Host & Port
		hostY := startY + 4
		printLabel(buf, startX+3, hostY, i18n.T("dbconnect.label_host"), accentFg, bg)
		hVal := m.Host
		hFg := fg
		hBg := bg
		if m.ActiveField == 1 {
			hFg = focusFg
			hBg = focusBg
			hVal += "_"
		}
		printBox(buf, startX+16, hostY, 24, hVal, hFg, hBg, borderFg)

		printLabel(buf, startX+42, hostY, i18n.T("dbconnect.label_port"), accentFg, bg)
		pVal := m.Port
		pFg := fg
		pBg := bg
		if m.ActiveField == 2 {
			pFg = focusFg
			pBg = focusBg
			pVal += "_"
		}
		printBox(buf, startX+48, hostY, 10, pVal, pFg, pBg, borderFg)

		// Row 3: Database Name
		dbY := startY + 6
		printLabel(buf, startX+3, dbY, i18n.T("dbconnect.label_db"), accentFg, bg)
		dVal := m.Database
		dFg := fg
		dBg := bg
		if m.ActiveField == 3 {
			dFg = focusFg
			dBg = focusBg
			dVal += "_"
		}
		printBox(buf, startX+16, dbY, 42, dVal, dFg, dBg, borderFg)

		// Row 4: User & Password
		userY := startY + 8
		printLabel(buf, startX+3, userY, i18n.T("dbconnect.label_user"), accentFg, bg)
		uVal := m.User
		uFg := fg
		uBg := bg
		if m.ActiveField == 4 {
			uFg = focusFg
			uBg = focusBg
			uVal += "_"
		}
		printBox(buf, startX+16, userY, 18, uVal, uFg, uBg, borderFg)

		printLabel(buf, startX+36, userY, i18n.T("dbconnect.label_pass"), accentFg, bg)
		passMask := strings.Repeat("•", len(m.Password))
		pwFg := fg
		pwBg := bg
		if m.ActiveField == 5 {
			pwFg = focusFg
			pwBg = focusBg
			passMask += "_"
		}
		printBox(buf, startX+44, userY, 14, passMask, pwFg, pwBg, borderFg)
	}

	// Status Message row
	statusY := startY + 11
	if m.StatusMsg != "" {
		sColor := errorFg
		if m.StatusSuccess {
			sColor = successFg
		}
		printLabel(buf, startX+4, statusY, m.StatusMsg, sColor, bg)
	}

	// Divider before buttons
	divY := startY + dialogH - 4
	for x := startX + 1; x < startX+dialogW-1; x++ {
		buf.SetRune(x, divY, '┄', borderFg, bg, cell.AttrNone)
	}

	// Button row
	btnY := startY + dialogH - 2
	b1Text := i18n.T("dbconnect.btn_test")
	b1Fg := fg
	b1Bg := hdrBg
	if m.ActiveField == 7 {
		b1Fg = focusFg
		b1Bg = focusBg
	}
	printLabel(buf, startX+4, btnY, b1Text, b1Fg, b1Bg)

	b2Text := i18n.T("dbconnect.btn_connect")
	b2Fg := successFg
	b2Bg := hdrBg
	if m.ActiveField == 8 {
		b2Fg = focusFg
		b2Bg = focusBg
	}
	printLabel(buf, startX+22, btnY, b2Text, b2Fg, b2Bg)

	b3Text := i18n.T("dbconnect.btn_cancel")
	b3Fg := fg
	b3Bg := hdrBg
	if m.ActiveField == 9 {
		b3Fg = focusFg
		b3Bg = focusBg
	}
	printLabel(buf, startX+42, btnY, b3Text, b3Fg, b3Bg)
}

func printLabel(buf *buffer.Buffer, x, y int, text string, fg, bg cell.Color) {
	for i, r := range []rune(text) {
		buf.SetRune(x+i, y, r, fg, bg, cell.AttrNone)
	}
}

func printBox(buf *buffer.Buffer, x, y, width int, text string, fg, bg, borderFg cell.Color) {
	runes := []rune(text)
	for i := 0; i < width; i++ {
		r := ' '
		if i < len(runes) {
			r = runes[i]
		}
		buf.SetRune(x+i, y, r, fg, bg, cell.AttrNone)
	}
}

// HandleMouse processes mouse events for DBConnectModal.
func (m *DBConnectModal) HandleMouse(mouse input.Mouse, screenW, screenH int) (bool, string) {
	if !m.Visible || screenW < 40 || screenH < 15 {
		return false, ""
	}

	dialogW := 64
	dialogH := 18
	if dialogW > screenW-4 {
		dialogW = screenW - 4
	}
	if dialogH > screenH-2 {
		dialogH = screenH - 2
	}

	startX := (screenW - dialogW) / 2
	startY := (screenH - dialogH) / 2

	// If clicked outside modal, close dialog
	if mouse.Action == input.MousePress && mouse.Button == input.MouseLeft {
		if mouse.X < startX || mouse.X >= startX+dialogW || mouse.Y < startY || mouse.Y >= startY+dialogH {
			m.CloseDialog()
			return true, "cancel"
		}
	}

	cur := m.Types[m.SelectedType]
	typeY := startY + 2

	if mouse.Button == input.MouseWheelUp && mouse.Y == typeY {
		m.SwitchType(-1)
		return true, ""
	}
	if mouse.Button == input.MouseWheelDown && mouse.Y == typeY {
		m.SwitchType(1)
		return true, ""
	}

	if mouse.Action == input.MousePress && mouse.Button == input.MouseLeft {
		// Close button on header
		if mouse.Y == startY && mouse.X >= startX+dialogW-3 {
			m.CloseDialog()
			return true, "cancel"
		}

		// Row 1: DB Type Selector
		if mouse.Y == typeY {
			if mouse.X >= startX+16 && mouse.X <= startX+20 {
				m.SwitchType(-1)
				return true, ""
			}
			if mouse.X >= startX+44 && mouse.X <= startX+48 {
				m.SwitchType(1)
				return true, ""
			}
			m.ActiveField = 0
			return true, ""
		}

		if cur.IsFileBased {
			// Row 2: File Path
			fileY := startY + 5
			if mouse.Y == fileY && mouse.X >= startX+16 && mouse.X <= startX+58 {
				m.ActiveField = 6
				return true, ""
			}
		} else {
			// Row 2: Host & Port
			hostY := startY + 4
			if mouse.Y == hostY {
				if mouse.X >= startX+16 && mouse.X < startX+40 {
					m.ActiveField = 1
					return true, ""
				}
				if mouse.X >= startX+48 && mouse.X <= startX+58 {
					m.ActiveField = 2
					return true, ""
				}
			}

			// Row 3: Database Name
			dbY := startY + 6
			if mouse.Y == dbY && mouse.X >= startX+16 && mouse.X <= startX+58 {
				m.ActiveField = 3
				return true, ""
			}

			// Row 4: User & Password
			userY := startY + 8
			if mouse.Y == userY {
				if mouse.X >= startX+16 && mouse.X < startX+34 {
					m.ActiveField = 4
					return true, ""
				}
				if mouse.X >= startX+44 && mouse.X <= startX+58 {
					m.ActiveField = 5
					return true, ""
				}
			}
		}

		// Button row
		btnY := startY + dialogH - 2
		if mouse.Y == btnY {
			// Проверить
			if mouse.X >= startX+4 && mouse.X <= startX+18 {
				m.ActiveField = 7
				m.TestConnection()
				return true, "test"
			}
			// Подключить
			if mouse.X >= startX+22 && mouse.X <= startX+38 {
				m.ActiveField = 8
				if m.OnSave != nil {
					m.OnSave(m.CurrentProfile())
				}
				m.CloseDialog()
				return true, "connect"
			}
			// Отмена (Esc)
			if mouse.X >= startX+42 && mouse.X <= startX+60 {
				m.ActiveField = 9
				m.CloseDialog()
				return true, "cancel"
			}
		}

		return true, ""
	}

	return false, ""
}
