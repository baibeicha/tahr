package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/clipboard"
	"tahr/internal/core/grpcproto"
	"tahr/internal/ui"
)

// GRPCFocusArea indicates which section of the gRPC panel currently possesses keyboard focus.
type GRPCFocusArea int

const (
	FocusTree GRPCFocusArea = iota
	FocusEditor
	FocusButtons
)

// GRPCTreeItemKind distinguishes service group headers from individual RPC methods.
type GRPCTreeItemKind int

const (
	TreeItemService GRPCTreeItemKind = iota
	TreeItemRPC
)

// GRPCTreeItem represents a single selectable row in the gRPC service tree.
type GRPCTreeItem struct {
	Kind        GRPCTreeItemKind
	ServiceName string
	RPC         *grpcproto.RPCMethod
	Expanded    bool
	IsLastChild bool
}

// GRPCButtonAction identifies the panel toolbar action buttons.
type GRPCButtonAction int

const (
	BtnGenerateCode GRPCButtonAction = iota
	BtnCreateMock
	BtnCopyPayload
	BtnReset
)

// GRPCButtonHit tracks mouse hit areas for the action buttons without brackets.
type GRPCButtonHit struct {
	Action GRPCButtonAction
	X, Y   int
	Width  int
}

// GRPCPanel is a full-featured tool window for inspecting gRPC services,
// synthesizing dynamic request mocks, editing JSON payloads, and generating language stubs.
type GRPCPanel struct {
	Open        bool
	Workspace   *grpcproto.ProtoWorkspace
	MockBuilder *grpcproto.MockBuilder
	Theme       *ui.Theme

	// Tree State
	TreeItems       []GRPCTreeItem
	SelectedTreeIdx int
	TreeScroll      int
	ActiveRPC       *grpcproto.RPCMethod
	ActiveService   string

	// Editor State
	PayloadText   string
	OriginalMock  string
	PayloadLines  []string
	CursorRow     int
	CursorCol     int
	EditorScrollY int
	EditorScrollX int

	// Focus & Buttons
	FocusArea      GRPCFocusArea
	SelectedButton GRPCButtonAction
	ButtonHits     []GRPCButtonHit

	// Codegen Settings
	SelectedLang string
	OutputDir    string
	LanguageList []string
	LangIndex    int

	// Status & Diagnostics
	StatusMessage string
	StatusIsError bool

	// Callbacks
	OnCopy         func(text string)
	OnGenerateCode func(cmd *grpcproto.CodegenCommand)
	OnClose        func()
	OnToast        func(level, title, msg string)
}

// NewGRPCPanel initializes a new gRPC panel with clean styling and dynamic mock support.
func NewGRPCPanel(ws *grpcproto.ProtoWorkspace, theme *ui.Theme) *GRPCPanel {
	if theme == nil {
		t := ui.CatppuccinMocha()
		theme = &t
	}

	p := &GRPCPanel{
		Open:           true,
		Theme:          theme,
		FocusArea:      FocusTree,
		SelectedButton: BtnGenerateCode,
		SelectedLang:   grpcproto.LanguageGo,
		OutputDir:      "./gen",
		LanguageList:   grpcproto.SupportedLanguages,
		LangIndex:      0,
		PayloadLines:   []string{"{", "}"},
		PayloadText:    "{\n}",
		OriginalMock:   "{\n}",
		ButtonHits:     make([]GRPCButtonHit, 0, 4),
	}

	if ws != nil {
		p.SetWorkspace(ws)
	}

	return p
}

// SetWorkspace binds a proto workspace to the panel, rebuilding the services tree.
func (p *GRPCPanel) SetWorkspace(ws *grpcproto.ProtoWorkspace) {
	p.Workspace = ws
	p.MockBuilder = grpcproto.NewMockBuilder(ws)
	p.rebuildTree()

	// Automatically select first available RPC method
	for _, it := range p.TreeItems {
		if it.Kind == TreeItemRPC && it.RPC != nil {
			p.SelectRPC(it.RPC, it.ServiceName)
			break
		}
	}
}

// SetPayload updates the editable payload text and resets cursor positions.
func (p *GRPCPanel) SetPayload(text string) {
	p.PayloadText = text
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	p.PayloadLines = lines
	p.CursorRow = 0
	p.CursorCol = 0
	p.EditorScrollY = 0
	p.EditorScrollX = 0
}

// SelectRPC selects an RPC method and synthesizes its JSON request template.
func (p *GRPCPanel) SelectRPC(rpc *grpcproto.RPCMethod, serviceName string) {
	if rpc == nil {
		return
	}
	p.ActiveRPC = rpc
	p.ActiveService = serviceName

	if p.MockBuilder != nil {
		mock, err := p.MockBuilder.BuildRPCRequestMock(rpc)
		if err == nil {
			p.SetPayload(mock)
			p.OriginalMock = mock
			p.SetStatus(fmt.Sprintf("Generated mock template for %s.%s", serviceName, rpc.Name), false)
			return
		}
	}

	defaultTemplate := "{\n}"
	p.SetPayload(defaultTemplate)
	p.OriginalMock = defaultTemplate
	p.SetStatus(fmt.Sprintf("Selected %s.%s", serviceName, rpc.Name), false)
}

// CycleLanguage toggles through Go, Python, Rust, TypeScript, and C++.
func (p *GRPCPanel) CycleLanguage() string {
	if len(p.LanguageList) == 0 {
		return p.SelectedLang
	}
	p.LangIndex = (p.LangIndex + 1) % len(p.LanguageList)
	p.SelectedLang = p.LanguageList[p.LangIndex]
	p.SetStatus(fmt.Sprintf("Target language set to %s", p.SelectedLang), false)
	return p.SelectedLang
}

// SetStatus updates the status feedback bar.
func (p *GRPCPanel) SetStatus(msg string, isError bool) {
	p.StatusMessage = msg
	p.StatusIsError = isError
}

func (p *GRPCPanel) rebuildTree() {
	if p.Workspace == nil {
		p.TreeItems = nil
		return
	}

	services := p.Workspace.AllServices()
	items := make([]GRPCTreeItem, 0, len(services)*4)

	for _, svc := range services {
		svcItem := GRPCTreeItem{
			Kind:        TreeItemService,
			ServiceName: svc.Name,
			Expanded:    true, // Expanded by default for direct visibility
		}
		items = append(items, svcItem)

		for j, m := range svc.Methods {
			rpcMethod := m
			items = append(items, GRPCTreeItem{
				Kind:        TreeItemRPC,
				ServiceName: svc.Name,
				RPC:         &rpcMethod,
				IsLastChild: j == len(svc.Methods)-1,
			})
		}
	}

	p.TreeItems = items
	if p.SelectedTreeIdx >= len(items) {
		p.SelectedTreeIdx = max(0, len(items)-1)
	}
}

// HandleKey handles keyboard navigation and payload editing.
func (p *GRPCPanel) HandleKey(k input.Key) bool {
	if !p.Open {
		return false
	}

	// Global Hotkeys
	if k.Type == input.KeyEsc {
		if p.OnClose != nil {
			p.OnClose()
		}
		p.Open = false
		return true
	}

	// Alt+L cycles target language
	if k.HasAlt() && (k.Rune == 'l' || k.Rune == 'L' || k.BaseKey == 'l') {
		p.CycleLanguage()
		return true
	}

	// Tab / Shift+Tab cycles focus areas: Tree -> Editor -> Buttons -> Tree
	if k.Type == input.KeyTab {
		if k.HasShift() {
			switch p.FocusArea {
			case FocusTree:
				p.FocusArea = FocusButtons
			case FocusEditor:
				p.FocusArea = FocusTree
			case FocusButtons:
				p.FocusArea = FocusEditor
			}
		} else {
			switch p.FocusArea {
			case FocusTree:
				p.FocusArea = FocusEditor
			case FocusEditor:
				p.FocusArea = FocusButtons
			case FocusButtons:
				p.FocusArea = FocusTree
			}
		}
		return true
	}

	// Direct action shortcuts
	if k.HasAlt() || k.HasCtrl() {
		switch k.Rune {
		case 'g', 'G':
			p.TriggerButton(BtnGenerateCode)
			return true
		case 'm', 'M':
			p.TriggerButton(BtnCreateMock)
			return true
		case 'c', 'C':
			p.TriggerButton(BtnCopyPayload)
			return true
		case 'r', 'R':
			p.TriggerButton(BtnReset)
			return true
		}
	}

	switch p.FocusArea {
	case FocusTree:
		return p.handleTreeKey(k)
	case FocusEditor:
		return p.handleEditorKey(k)
	case FocusButtons:
		return p.handleButtonsKey(k)
	}

	return false
}

func (p *GRPCPanel) handleTreeKey(k input.Key) bool {
	if len(p.TreeItems) == 0 {
		return false
	}

	switch k.Type {
	case input.KeyUp:
		if p.SelectedTreeIdx > 0 {
			p.SelectedTreeIdx--
		}
		return true

	case input.KeyDown:
		if p.SelectedTreeIdx < len(p.TreeItems)-1 {
			p.SelectedTreeIdx++
		}
		return true

	case input.KeyHome:
		p.SelectedTreeIdx = 0
		return true

	case input.KeyEnd:
		p.SelectedTreeIdx = len(p.TreeItems) - 1
		return true

	case input.KeyEnter:
		item := p.TreeItems[p.SelectedTreeIdx]
		if item.Kind == TreeItemService {
			// Toggle service expansion
			p.toggleService(item.ServiceName)
		} else if item.Kind == TreeItemRPC && item.RPC != nil {
			p.SelectRPC(item.RPC, item.ServiceName)
			p.FocusArea = FocusEditor
		}
		return true

	case input.KeySpace:
		item := p.TreeItems[p.SelectedTreeIdx]
		if item.Kind == TreeItemService {
			p.toggleService(item.ServiceName)
		}
		return true
	}

	return false
}

func (p *GRPCPanel) toggleService(serviceName string) {
	for i := range p.TreeItems {
		if p.TreeItems[i].Kind == TreeItemService && p.TreeItems[i].ServiceName == serviceName {
			p.TreeItems[i].Expanded = !p.TreeItems[i].Expanded
			break
		}
	}
}

func (p *GRPCPanel) handleEditorKey(k input.Key) bool {
	switch k.Type {
	case input.KeyUp:
		if p.CursorRow > 0 {
			p.CursorRow--
			p.clampCol()
		}
		return true

	case input.KeyDown:
		if p.CursorRow < len(p.PayloadLines)-1 {
			p.CursorRow++
			p.clampCol()
		}
		return true

	case input.KeyLeft:
		if p.CursorCol > 0 {
			p.CursorCol--
		} else if p.CursorRow > 0 {
			p.CursorRow--
			p.CursorCol = len([]rune(p.PayloadLines[p.CursorRow]))
		}
		return true

	case input.KeyRight:
		lineRunes := []rune(p.PayloadLines[p.CursorRow])
		if p.CursorCol < len(lineRunes) {
			p.CursorCol++
		} else if p.CursorRow < len(p.PayloadLines)-1 {
			p.CursorRow++
			p.CursorCol = 0
		}
		return true

	case input.KeyHome:
		p.CursorCol = 0
		return true

	case input.KeyEnd:
		p.CursorCol = len([]rune(p.PayloadLines[p.CursorRow]))
		return true

	case input.KeyEnter:
		line := p.PayloadLines[p.CursorRow]
		runes := []rune(line)
		pos := p.CursorCol
		if pos > len(runes) {
			pos = len(runes)
		}

		left := string(runes[:pos])
		right := string(runes[pos:])

		p.PayloadLines[p.CursorRow] = left
		newLines := make([]string, len(p.PayloadLines)+1)
		copy(newLines, p.PayloadLines[:p.CursorRow+1])
		newLines[p.CursorRow+1] = right
		copy(newLines[p.CursorRow+2:], p.PayloadLines[p.CursorRow+1:])
		p.PayloadLines = newLines

		p.CursorRow++
		p.CursorCol = 0
		p.syncPayload()
		return true

	case input.KeyBackspace:
		if p.CursorCol > 0 {
			runes := []rune(p.PayloadLines[p.CursorRow])
			p.PayloadLines[p.CursorRow] = string(runes[:p.CursorCol-1]) + string(runes[p.CursorCol:])
			p.CursorCol--
			p.syncPayload()
		} else if p.CursorRow > 0 {
			prevLine := p.PayloadLines[p.CursorRow-1]
			currLine := p.PayloadLines[p.CursorRow]
			p.CursorCol = len([]rune(prevLine))
			p.PayloadLines[p.CursorRow-1] = prevLine + currLine
			p.PayloadLines = append(p.PayloadLines[:p.CursorRow], p.PayloadLines[p.CursorRow+1:]...)
			p.CursorRow--
			p.syncPayload()
		}
		return true

	case input.KeyDelete:
		line := p.PayloadLines[p.CursorRow]
		runes := []rune(line)
		if p.CursorCol < len(runes) {
			p.PayloadLines[p.CursorRow] = string(runes[:p.CursorCol]) + string(runes[p.CursorCol+1:])
			p.syncPayload()
		} else if p.CursorRow < len(p.PayloadLines)-1 {
			p.PayloadLines[p.CursorRow] = line + p.PayloadLines[p.CursorRow+1]
			p.PayloadLines = append(p.PayloadLines[:p.CursorRow+1], p.PayloadLines[p.CursorRow+2:]...)
			p.syncPayload()
		}
		return true

	default:
		if k.Rune != 0 && !k.HasCtrl() && !k.HasAlt() {
			runes := []rune(p.PayloadLines[p.CursorRow])
			pos := p.CursorCol
			if pos > len(runes) {
				pos = len(runes)
			}
			p.PayloadLines[p.CursorRow] = string(runes[:pos]) + string(k.Rune) + string(runes[pos:])
			p.CursorCol++
			p.syncPayload()
			return true
		}
	}

	return false
}

func (p *GRPCPanel) handleButtonsKey(k input.Key) bool {
	switch k.Type {
	case input.KeyLeft:
		if p.SelectedButton > BtnGenerateCode {
			p.SelectedButton--
		}
		return true

	case input.KeyRight:
		if p.SelectedButton < BtnReset {
			p.SelectedButton++
		}
		return true

	case input.KeyEnter, input.KeySpace:
		p.TriggerButton(p.SelectedButton)
		return true
	}

	return false
}

func (p *GRPCPanel) clampCol() {
	if p.CursorRow >= len(p.PayloadLines) {
		p.CursorRow = max(0, len(p.PayloadLines)-1)
	}
	lineLen := len([]rune(p.PayloadLines[p.CursorRow]))
	if p.CursorCol > lineLen {
		p.CursorCol = lineLen
	}
}

func (p *GRPCPanel) syncPayload() {
	p.PayloadText = strings.Join(p.PayloadLines, "\n")
}

// TriggerButton invokes the action for one of the four toolbar buttons.
func (p *GRPCPanel) TriggerButton(action GRPCButtonAction) {
	switch action {
	case BtnGenerateCode:
		p.handleGenerateCode()
	case BtnCreateMock:
		p.handleCreateMock()
	case BtnCopyPayload:
		p.handleCopyPayload()
	case BtnReset:
		p.handleReset()
	}
}

func (p *GRPCPanel) handleGenerateCode() {
	var protoFiles []string
	if p.Workspace != nil {
		for _, f := range p.Workspace.Files {
			if f.FilePath != "" {
				protoFiles = append(protoFiles, f.FilePath)
			}
		}
	}
	if len(protoFiles) == 0 {
		protoFiles = []string{"service.proto"}
	}

	cfg := grpcproto.CodegenConfig{
		Language:     p.SelectedLang,
		ProtoFiles:   protoFiles,
		OutputDir:    p.OutputDir,
		GenerateGRPC: true,
	}

	cmd, err := grpcproto.BuildCodegenCommand(cfg)
	if err != nil {
		p.SetStatus(fmt.Sprintf("Codegen configuration error: %v", err), true)
		return
	}

	avail, _ := grpcproto.CheckCompilerAvailable(cmd.Executable)
	if !avail {
		help := grpcproto.GetMissingCompilerHelp(p.SelectedLang, cmd.Executable)
		p.SetStatus(fmt.Sprintf("%s compiler not found in PATH", cmd.Executable), true)
		if p.OnToast != nil {
			p.OnToast("warn", "CODEGEN", fmt.Sprintf("Compiler '%s' missing. See feedback.", cmd.Executable))
		}
		// Write instructions to payload editor for user reference if desired
		_ = help
		return
	}

	p.SetStatus(fmt.Sprintf("Codegen command ready: %s", cmd.CommandString()), false)
	if p.OnGenerateCode != nil {
		p.OnGenerateCode(cmd)
	}
	if p.OnToast != nil {
		p.OnToast("info", "CODEGEN", fmt.Sprintf("Prepared stubs for %s (%d files)", p.SelectedLang, len(protoFiles)))
	}
}

func (p *GRPCPanel) handleCreateMock() {
	if p.ActiveRPC == nil {
		p.SetStatus("No RPC method selected to mock", true)
		return
	}

	if p.MockBuilder == nil {
		p.MockBuilder = grpcproto.NewMockBuilder(p.Workspace)
	}

	mock, err := p.MockBuilder.BuildRPCRequestMock(p.ActiveRPC)
	if err != nil {
		p.SetStatus(fmt.Sprintf("Failed to synthesize mock: %v", err), true)
		return
	}

	p.SetPayload(mock)
	p.SetStatus(fmt.Sprintf("Synthesized fresh mock for %s", p.ActiveRPC.Name), false)
	if p.OnToast != nil {
		p.OnToast("info", "gRPC MOCK", fmt.Sprintf("Generated %s payload", p.ActiveRPC.RequestType))
	}
}

func (p *GRPCPanel) handleCopyPayload() {
	_ = clipboard.Write(p.PayloadText)
	if p.OnCopy != nil {
		p.OnCopy(p.PayloadText)
	}
	p.SetStatus(fmt.Sprintf("Payload copied to clipboard (%d bytes)", len(p.PayloadText)), false)
	if p.OnToast != nil {
		p.OnToast("info", "CLIPBOARD", "Copied JSON payload")
	}
}

func (p *GRPCPanel) handleReset() {
	if p.OriginalMock != "" {
		p.SetPayload(p.OriginalMock)
		p.SetStatus("Payload reset to initial mock template", false)
	}
}

// HandleClick processes mouse interactions with tree rows, editor, and action buttons.
func (p *GRPCPanel) HandleClick(x, y int, bounds buffer.Rect) bool {
	if !p.Open || bounds.Width <= 0 || bounds.Height <= 0 {
		return false
	}

	// 1. Close button at top right
	if y == bounds.Y && x >= bounds.X+bounds.Width-3 {
		if p.OnClose != nil {
			p.OnClose()
		}
		p.Open = false
		return true
	}

	treeW := bounds.Width * 38 / 100
	if treeW < 28 {
		treeW = 28
	}

	// 2. Click in Tree pane
	if x >= bounds.X && x < bounds.X+treeW && y > bounds.Y+1 && y < bounds.Y+bounds.Height-1 {
		p.FocusArea = FocusTree
		rowIdx := y - (bounds.Y + 2) + p.TreeScroll
		if rowIdx >= 0 && rowIdx < len(p.TreeItems) {
			p.SelectedTreeIdx = rowIdx
			item := p.TreeItems[rowIdx]
			if item.Kind == TreeItemService {
				p.toggleService(item.ServiceName)
			} else if item.Kind == TreeItemRPC && item.RPC != nil {
				p.SelectRPC(item.RPC, item.ServiceName)
			}
		}
		return true
	}

	// 3. Click on Action Buttons Bar
	for _, hit := range p.ButtonHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.Width {
			p.FocusArea = FocusButtons
			p.SelectedButton = hit.Action
			p.TriggerButton(hit.Action)
			return true
		}
	}

	// 4. Click in Editor pane
	editorX := bounds.X + treeW + 1
	if x >= editorX && y > bounds.Y+4 && y < bounds.Y+bounds.Height-3 {
		p.FocusArea = FocusEditor
		lineIdx := y - (bounds.Y + 5) + p.EditorScrollY
		if lineIdx >= 0 && lineIdx < len(p.PayloadLines) {
			p.CursorRow = lineIdx
			colIdx := x - (editorX + 6) + p.EditorScrollX
			if colIdx < 0 {
				colIdx = 0
			}
			p.CursorCol = colIdx
			p.clampCol()
		}
		return true
	}

	return false
}

// Render draws the complete gRPC tool window interface into the Goatui buffer.
func (p *GRPCPanel) Render(buf *buffer.Buffer, bounds buffer.Rect) {
	p.RenderWithTheme(buf, bounds, p.Theme)
}

// RenderWithTheme renders the gRPC tool window with an explicit color theme.
func (p *GRPCPanel) RenderWithTheme(buf *buffer.Buffer, bounds buffer.Rect, theme *ui.Theme) {
	if !p.Open || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	if theme == nil {
		t := ui.CatppuccinMocha()
		theme = &t
	}

	p.ButtonHits = p.ButtonHits[:0]

	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	headerBg := toColor(theme.StatusBarBg)
	accentFg := toColor(theme.Function)
	keywordFg := toColor(theme.Keyword)
	stringFg := toColor(theme.String)
	commentFg := toColor(theme.Comment)
	selBg := toColor(theme.SelectionBg)
	gutterBg := toColor(theme.GutterBg)
	errFg := toColor(theme.DiagnosticError)
	infoFg := toColor(theme.DiagnosticInfo)

	// Clear full bounding rectangle
	for y := bounds.Y; y < bounds.Y+bounds.Height; y++ {
		for x := bounds.X; x < bounds.X+bounds.Width; x++ {
			buf.SetRune(x, y, ' ', fg, bg, cell.AttrNone)
		}
	}

	// 1. Header Bar: "gRPC & Protobuf Explorer"
	headerTitle := "gRPC & Protobuf Explorer"
	for x := bounds.X; x < bounds.X+bounds.Width; x++ {
		buf.SetRune(x, bounds.Y, ' ', fg, headerBg, cell.AttrNone)
	}
	for i, r := range []rune(headerTitle) {
		if bounds.X+1+i < bounds.X+bounds.Width-4 {
			buf.SetRune(bounds.X+1+i, bounds.Y, r, accentFg, headerBg, cell.AttrBold)
		}
	}
	// Language Badge in Header: " Lang: Go (Alt+L) "
	langBadge := fmt.Sprintf(" Lang: %s (Alt+L) ", p.SelectedLang)
	badgeStart := bounds.X + bounds.Width - len([]rune(langBadge)) - 5
	if badgeStart > bounds.X+len([]rune(headerTitle))+2 {
		for i, r := range []rune(langBadge) {
			buf.SetRune(badgeStart+i, bounds.Y, r, keywordFg, headerBg, cell.AttrNone)
		}
	}
	// Close icon '✕'
	buf.SetRune(bounds.X+bounds.Width-2, bounds.Y, '✕', errFg, headerBg, cell.AttrBold)

	// Calculate 2-pane column split
	treeW := bounds.Width * 38 / 100
	if treeW < 28 {
		treeW = 28
	}
	if treeW > bounds.Width-30 {
		treeW = max(20, bounds.Width-30)
	}
	editorX := bounds.X + treeW + 1
	editorW := bounds.Width - treeW - 1

	// Vertical Separator between tree and editor
	for y := bounds.Y + 1; y < bounds.Y+bounds.Height-1; y++ {
		buf.SetRune(bounds.X+treeW, y, '│', borderFg, bg, cell.AttrNone)
	}

	// 2. Render Left Pane: Services & RPC Tree
	treeHeader := fmt.Sprintf(" Services (%d) ", len(p.WorkspaceServices()))
	for i, r := range []rune(treeHeader) {
		if bounds.X+1+i < bounds.X+treeW {
			buf.SetRune(bounds.X+1+i, bounds.Y+1, r, commentFg, bg, cell.AttrBold)
		}
	}

	visibleTreeRows := bounds.Height - 3
	if len(p.TreeItems) == 0 {
		emptyLines := []string{
			"Сервисы не найдены",
			"─────────────────────",
			"В проекте отсутствуют",
			"файлы .proto",
			"",
			"Инструкция:",
			"• Добавьте .proto файлы",
			"• Или укажите путь к ним",
		}
		for row, el := range emptyLines {
			if row >= visibleTreeRows {
				break
			}
			screenY := bounds.Y + 2 + row
			c := commentFg
			attr := cell.AttrNone
			if row == 0 {
				c = toColor(theme.DiagnosticWarn)
				attr = cell.AttrBold
			} else if strings.HasPrefix(el, "Инструкция") {
				c = toColor(theme.Function)
			} else if strings.HasPrefix(el, "•") {
				c = fg
			}
			for i, r := range []rune(el) {
				if bounds.X+1+i < bounds.X+treeW-1 {
					buf.SetRune(bounds.X+1+i, screenY, r, c, bg, attr)
				}
			}
		}
	} else {
		for row := 0; row < visibleTreeRows; row++ {
			itemIdx := p.TreeScroll + row
			screenY := bounds.Y + 2 + row
			if itemIdx >= len(p.TreeItems) {
				break
			}

		item := p.TreeItems[itemIdx]
		isSel := itemIdx == p.SelectedTreeIdx
		rowBg := bg
		if isSel && p.FocusArea == FocusTree {
			rowBg = selBg
		}

		// Clear row background
		for x := bounds.X; x < bounds.X+treeW; x++ {
			buf.SetRune(x, screenY, ' ', fg, rowBg, cell.AttrNone)
		}

		if item.Kind == TreeItemService {
			// Service node: "▼ ServiceName" or "▶ ServiceName"
			expandIcon := '▼'
			if !item.Expanded {
				expandIcon = '▶'
			}
			buf.SetRune(bounds.X+1, screenY, expandIcon, accentFg, rowBg, cell.AttrBold)
			svcNameRunes := []rune(fmt.Sprintf(" %s", item.ServiceName))
			for si, sr := range svcNameRunes {
				if bounds.X+2+si < bounds.X+treeW-1 {
					buf.SetRune(bounds.X+2+si, screenY, sr, fg, rowBg, cell.AttrBold)
				}
			}
		} else if item.Kind == TreeItemRPC && item.RPC != nil {
			// RPC node: "  ├─ MethodName"
			branchChar := '├'
			if item.IsLastChild {
				branchChar = '└'
			}
			buf.SetRune(bounds.X+3, screenY, branchChar, borderFg, rowBg, cell.AttrNone)
			buf.SetRune(bounds.X+4, screenY, '─', borderFg, rowBg, cell.AttrNone)

			streamTag := ""
			if item.RPC.ClientStreaming && item.RPC.ServerStreaming {
				streamTag = " (bidi)"
			} else if item.RPC.ClientStreaming {
				streamTag = " (stream->)"
			} else if item.RPC.ServerStreaming {
				streamTag = " (->stream)"
			}

			methodLabel := fmt.Sprintf(" %s%s", item.RPC.Name, streamTag)
			rFg := stringFg
			if isSel {
				rFg = accentFg
			}
			for mi, mr := range []rune(methodLabel) {
				if bounds.X+5+mi < bounds.X+treeW-1 {
					buf.SetRune(bounds.X+5+mi, screenY, mr, rFg, rowBg, cell.AttrNone)
				}
			}
		}
	}
	}

	// 3. Render Right Pane: Selected RPC Info & JSON Request Editor
	// Info Card (Top 3 lines of right pane)
	activeSvc := "None"
	activeMethod := "Select an RPC method from tree"
	activeReq := "-"
	activeResp := "-"
	if p.ActiveRPC != nil {
		activeSvc = p.ActiveService
		activeMethod = p.ActiveRPC.Name
		activeReq = p.ActiveRPC.RequestType
		activeResp = p.ActiveRPC.ResponseType
		if p.ActiveRPC.ClientStreaming {
			activeReq = "stream " + activeReq
		}
		if p.ActiveRPC.ServerStreaming {
			activeResp = "stream " + activeResp
		}
	}

	infoLine1 := fmt.Sprintf("Service: %s  │  RPC: %s", activeSvc, activeMethod)
	infoLine2 := fmt.Sprintf("Request: %s  │  Response: %s", activeReq, activeResp)

	for i, r := range []rune(infoLine1) {
		if editorX+1+i < bounds.X+bounds.Width-1 {
			buf.SetRune(editorX+1+i, bounds.Y+1, r, fg, bg, cell.AttrBold)
		}
	}
	for i, r := range []rune(infoLine2) {
		if editorX+1+i < bounds.X+bounds.Width-1 {
			buf.SetRune(editorX+1+i, bounds.Y+2, r, commentFg, bg, cell.AttrNone)
		}
	}

	// Editor Header divider
	editorDivider := "── Request Payload (JSON) "
	for col := 0; col < editorW; col++ {
		r := '─'
		dFg := borderFg
		if col < len([]rune(editorDivider)) {
			r = []rune(editorDivider)[col]
			dFg = accentFg
		}
		buf.SetRune(editorX+col, bounds.Y+3, r, dFg, bg, cell.AttrNone)
	}

	// JSON Payload Text Lines
	editorStartRow := bounds.Y + 4
	buttonsBarY := bounds.Y + bounds.Height - 3
	statusBarY := bounds.Y + bounds.Height - 1
	editorAvailH := buttonsBarY - editorStartRow

	for row := 0; row < editorAvailH; row++ {
		lineIdx := p.EditorScrollY + row
		screenY := editorStartRow + row

		// Gutter background for line numbers
		for gx := 0; gx < 5; gx++ {
			buf.SetRune(editorX+gx, screenY, ' ', commentFg, gutterBg, cell.AttrNone)
		}

		if lineIdx < len(p.PayloadLines) {
			// Line number
			numStr := fmt.Sprintf("%3d ", lineIdx+1)
			for ni, nr := range []rune(numStr) {
				buf.SetRune(editorX+ni, screenY, nr, commentFg, gutterBg, cell.AttrNone)
			}
			buf.SetRune(editorX+4, screenY, '│', borderFg, gutterBg, cell.AttrNone)

			// Line text
			lineRunes := []rune(p.PayloadLines[lineIdx])
			for ci := 0; ci < editorW-6; ci++ {
				charIdx := p.EditorScrollX + ci
				r := ' '
				cFg := fg
				if charIdx < len(lineRunes) {
					r = lineRunes[charIdx]
					if r == '{' || r == '}' || r == '[' || r == ']' {
						cFg = keywordFg
					} else if r == '"' {
						cFg = stringFg
					} else if r == ':' {
						cFg = accentFg
					}
				}

				// Draw cursor block
				if p.FocusArea == FocusEditor && lineIdx == p.CursorRow && charIdx == p.CursorCol {
					buf.SetRune(editorX+6+ci, screenY, r, bg, accentFg, cell.AttrBold)
				} else {
					buf.SetRune(editorX+6+ci, screenY, r, cFg, bg, cell.AttrNone)
				}
			}
		} else {
			buf.SetRune(editorX+4, screenY, '│', borderFg, gutterBg, cell.AttrNone)
		}
	}

	// 4. Action Buttons Bar
	// Requirements:
	// Action buttons without brackets [ ]: ` Generate Code `, ` Create Mock `, ` Copy Payload `, ` Reset `.
	// NO cartoonish emojis! Clean developer TUI style.
	for col := 0; col < editorW; col++ {
		buf.SetRune(editorX+col, buttonsBarY-1, '─', borderFg, bg, cell.AttrNone)
	}

	buttons := []struct {
		action GRPCButtonAction
		label  string
	}{
		{BtnGenerateCode, " Generate Code "},
		{BtnCreateMock, " Create Mock "},
		{BtnCopyPayload, " Copy Payload "},
		{BtnReset, " Reset "},
	}

	curBtnX := editorX + 2
	for _, b := range buttons {
		bLabel := b.label
		isSel := p.FocusArea == FocusButtons && p.SelectedButton == b.action
		btnBg := selBg
		btnFg := fg
		btnAttr := cell.AttrNone

		if isSel {
			btnBg = accentFg
			btnFg = bg
			btnAttr = cell.AttrBold
		}

		p.ButtonHits = append(p.ButtonHits, GRPCButtonHit{
			Action: b.action,
			X:      curBtnX,
			Y:      buttonsBarY,
			Width:  len([]rune(bLabel)),
		})

		for _, r := range []rune(bLabel) {
			buf.SetRune(curBtnX, buttonsBarY, r, btnFg, btnBg, btnAttr)
			curBtnX++
		}
		curBtnX += 2 // Gap between buttons
	}

	// 5. Bottom Status / Feedback Bar
	statusText := p.StatusMessage
	if statusText == "" {
		statusText = "Tab: Switch Focus  │  Enter: Select / Trigger  │  Alt+L: Language  │  Esc: Close"
	}
	sFg := infoFg
	if p.StatusIsError {
		sFg = errFg
	}

	for col := 0; col < bounds.Width; col++ {
		buf.SetRune(bounds.X+col, statusBarY, ' ', fg, headerBg, cell.AttrNone)
	}
	for i, r := range []rune(" " + statusText) {
		if bounds.X+i < bounds.X+bounds.Width-1 {
			buf.SetRune(bounds.X+i, statusBarY, r, sFg, headerBg, cell.AttrNone)
		}
	}
}

// WorkspaceServices retrieves the list of services currently loaded.
func (p *GRPCPanel) WorkspaceServices() []grpcproto.Service {
	if p.Workspace == nil {
		return nil
	}
	return p.Workspace.AllServices()
}
