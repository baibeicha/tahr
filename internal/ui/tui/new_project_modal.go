package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/mattn/go-runewidth"
	"tahr/internal/core/plugin"
	"tahr/internal/ui"
)

// Field indices for focus navigation
const (
	FieldTemplates = 0
	FieldName      = 1
	FieldLocation  = 2
	FieldModule    = 3
	FieldSDK       = 4
	FieldCreateBtn = 5
	FieldCancelBtn = 6
)

// treeEntry represents a single visual item in the project structure preview.
type treeEntry struct {
	prefix string // Branch connectors (e.g. "├── ", "└── ", "│   ")
	name   string // Item name (e.g. "main.go", "cmd/")
	isDir  bool
}

// NewProjectModal represents an interactive GoLand-style fullscreen dialog
// for creating projects from plugin-defined templates.
type NewProjectModal struct {
	Open             bool
	Templates        []plugin.ProjectTemplate
	SelectedTemplate int

	ProjectName string
	Location    string
	PathInput   string // Alias for Location (backward compatibility)
	ModuleName  string
	SDKInfo     string

	ActiveField int // FieldTemplates .. FieldCancelBtn

	Suggestions    []string
	SelectedSug    int
	LocationEdited bool
	ModuleEdited   bool
	BaseDir        string

	// Legacy backward compatibility fields
	TemplateLabels []string

	OnCreate         func(kind, targetPath string)
	OnCreateTemplate func(tpl plugin.ProjectTemplate, name, location, module string)
	OnCancel         func()
}

// NewNewProjectModal initializes the modal with standard templates and default workspace path.
func NewNewProjectModal(defaultParent string) *NewProjectModal {
	defaultPath := filepath.Join(defaultParent, "new-project")
	m := &NewProjectModal{
		BaseDir:     defaultParent,
		ProjectName: "new-project",
		Location:    defaultPath,
		PathInput:   defaultPath,
		ActiveField: FieldName,
	}

	// Seed reference Go templates and universal Blank template (no emojis)
	refGo := plugin.ReferenceGoPluginManifest()
	m.Templates = append(m.Templates, refGo.ProjectTemplates...)
	m.Templates = append(m.Templates, plugin.ProjectTemplate{
		ID:          "blank",
		Name:        "Blank Project",
		Description: "An empty project directory with a README.md",
		Category:    "General",
		Icon:        "",
		Files: []plugin.ProjectTemplateFile{
			{
				Path:    "README.md",
				Content: "# {{.ProjectName}}\n\nNew project workspace.\n",
			},
		},
	})

	m.refreshTemplateLabels()
	m.updateDefaultModule()
	m.UpdateSuggestions()
	return m
}

func (m *NewProjectModal) refreshTemplateLabels() {
	m.TemplateLabels = make([]string, len(m.Templates))
	for i, t := range m.Templates {
		m.TemplateLabels[i] = t.Name
	}
}

func (m *NewProjectModal) updateDefaultModule() {
	if m.ModuleEdited {
		return
	}
	if m.SelectedTemplate >= 0 && m.SelectedTemplate < len(m.Templates) {
		tpl := m.Templates[m.SelectedTemplate]
		if tpl.DefaultModule != "" {
			name := m.ProjectName
			if name == "" {
				name = "my-app"
			}
			m.ModuleName = strings.ReplaceAll(tpl.DefaultModule, "{{.ProjectName}}", name)
		} else {
			m.ModuleName = ""
		}
	}
}

// OpenWithTemplates prepares and opens the modal with dynamically discovered plugin templates.
func (m *NewProjectModal) OpenWithTemplates(templates []plugin.ProjectTemplate, defaultParent string, sdkInfo string) {
	if len(templates) > 0 {
		m.Templates = templates
	}
	m.refreshTemplateLabels()
	if m.SelectedTemplate >= len(m.Templates) {
		m.SelectedTemplate = 0
	}
	if defaultParent != "" {
		m.BaseDir = defaultParent
	}
	if m.ProjectName == "" {
		m.ProjectName = "my-app"
	}
	m.Location = filepath.Join(m.BaseDir, m.ProjectName)
	m.PathInput = m.Location
	m.LocationEdited = false
	m.ModuleEdited = false
	m.SDKInfo = sdkInfo
	m.updateDefaultModule()
	m.ActiveField = FieldName
	m.UpdateSuggestions()
	m.Open = true
}

// UpdateSuggestions scans directories for the path entered so far.
func (m *NewProjectModal) UpdateSuggestions() {
	m.Suggestions = nil
	m.SelectedSug = 0
	clean := strings.TrimSpace(m.Location)
	if clean == "" {
		return
	}

	dirToScan := filepath.Dir(clean)
	prefix := strings.ToLower(filepath.Base(clean))
	if strings.HasSuffix(clean, "/") || strings.HasSuffix(clean, "\\") {
		dirToScan = clean
		prefix = ""
	}

	if entries, err := os.ReadDir(dirToScan); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				name := e.Name()
				if prefix == "" || strings.HasPrefix(strings.ToLower(name), prefix) {
					m.Suggestions = append(m.Suggestions, filepath.Join(dirToScan, name))
					if len(m.Suggestions) >= 4 {
						break
					}
				}
			}
		}
	}
}

// CurrentTemplate returns the currently selected project template.
func (m *NewProjectModal) CurrentTemplate() plugin.ProjectTemplate {
	if len(m.Templates) == 0 {
		return plugin.ProjectTemplate{
			ID:          "blank",
			Name:        "Blank Project",
			Description: "An empty project directory",
			Category:    "General",
			Icon:        "",
		}
	}
	if m.SelectedTemplate < 0 || m.SelectedTemplate >= len(m.Templates) {
		return m.Templates[0]
	}
	return m.Templates[m.SelectedTemplate]
}

// ConfirmCreate validates parameters and invokes creation callbacks.
func (m *NewProjectModal) ConfirmCreate() (bool, bool) {
	loc := strings.TrimSpace(m.Location)
	if strings.TrimSpace(m.PathInput) != "" && strings.TrimSpace(m.PathInput) != loc {
		loc = strings.TrimSpace(m.PathInput)
	}
	if loc == "" {
		return false, false
	}
	m.Open = false

	tpl := m.CurrentTemplate()
	name := strings.TrimSpace(m.ProjectName)
	if name == "" || name == "new-project" {
		name = filepath.Base(loc)
	}
	mod := strings.TrimSpace(m.ModuleName)
	if mod == "" {
		mod = name
	}

	if m.OnCreateTemplate != nil {
		m.OnCreateTemplate(tpl, name, loc, mod)
	}
	if m.OnCreate != nil {
		m.OnCreate(tpl.ID, loc)
	}
	return true, true
}

// HandleKey processes keyboard input in the modal.
func (m *NewProjectModal) HandleKey(k input.Key) (bool, bool) {
	if !m.Open {
		return false, false
	}

	// Esc: close modal
	if k.Type == input.KeyEsc || k.Rune == 27 {
		m.Open = false
		if m.OnCancel != nil {
			m.OnCancel()
		}
		return true, false
	}

	tpl := m.CurrentTemplate()
	hasModule := tpl.DefaultModule != ""

	// Tab / Shift+Tab field cycling
	if k.Type == input.KeyTab || k.Type == input.KeyBacktab {
		delta := 1
		if k.Type == input.KeyBacktab || k.HasShift() {
			delta = -1
		}
		for {
			m.ActiveField = (m.ActiveField + delta + 7) % 7
			if m.ActiveField == FieldModule && !hasModule {
				continue
			}
			break
		}
		return true, false
	}

	// Navigation when templates list is focused
	if m.ActiveField == FieldTemplates {
		switch k.Type {
		case input.KeyUp:
			if m.SelectedTemplate > 0 {
				m.SelectedTemplate--
				m.updateDefaultModule()
			}
			return true, false
		case input.KeyDown:
			if m.SelectedTemplate+1 < len(m.Templates) {
				m.SelectedTemplate++
				m.updateDefaultModule()
			}
			return true, false
		case input.KeyRight, input.KeyEnter:
			m.ActiveField = FieldName
			return true, false
		}
		return true, false
	}

	// Enter key handling
	if k.Type == input.KeyEnter {
		if m.ActiveField == FieldCancelBtn {
			m.Open = false
			if m.OnCancel != nil {
				m.OnCancel()
			}
			return true, false
		}
		// Confirm and create project
		return m.ConfirmCreate()
	}

	// Up / Down in Location suggestions
	if m.ActiveField == FieldLocation && len(m.Suggestions) > 0 {
		if k.Type == input.KeyUp {
			if m.SelectedSug > 0 {
				m.SelectedSug--
				m.Location = m.Suggestions[m.SelectedSug]
				m.PathInput = m.Location
				return true, false
			}
		} else if k.Type == input.KeyDown {
			if m.SelectedSug+1 < len(m.Suggestions) {
				m.SelectedSug++
				m.Location = m.Suggestions[m.SelectedSug]
				m.PathInput = m.Location
				return true, false
			}
		}
	}

	// Up / Down arrow field navigation
	if k.Type == input.KeyUp {
		switch m.ActiveField {
		case FieldName:
			m.ActiveField = FieldTemplates
		case FieldLocation:
			m.ActiveField = FieldName
		case FieldModule:
			m.ActiveField = FieldLocation
		case FieldSDK:
			if hasModule {
				m.ActiveField = FieldModule
			} else {
				m.ActiveField = FieldLocation
			}
		case FieldCreateBtn, FieldCancelBtn:
			m.ActiveField = FieldSDK
		}
		return true, false
	}
	if k.Type == input.KeyDown {
		switch m.ActiveField {
		case FieldName:
			m.ActiveField = FieldLocation
		case FieldLocation:
			if hasModule {
				m.ActiveField = FieldModule
			} else {
				m.ActiveField = FieldSDK
			}
		case FieldModule:
			m.ActiveField = FieldSDK
		case FieldSDK:
			m.ActiveField = FieldCreateBtn
		}
		return true, false
	}

	// Left / Right on action buttons
	if m.ActiveField == FieldCreateBtn && k.Type == input.KeyRight {
		m.ActiveField = FieldCancelBtn
		return true, false
	}
	if m.ActiveField == FieldCancelBtn && k.Type == input.KeyLeft {
		m.ActiveField = FieldCreateBtn
		return true, false
	}

	// Text input editing for fields
	switch m.ActiveField {
	case FieldName:
		if k.Type == input.KeyBackspace {
			runes := []rune(m.ProjectName)
			if len(runes) > 0 {
				m.ProjectName = string(runes[:len(runes)-1])
				if !m.LocationEdited {
					m.Location = filepath.Join(m.BaseDir, m.ProjectName)
					m.PathInput = m.Location
					m.UpdateSuggestions()
				}
				m.updateDefaultModule()
			}
			return true, false
		}
		if k.Type == input.KeySpace || (k.Rune >= 32 && !k.HasCtrl() && !k.HasAlt()) {
			char := string(k.Rune)
			if k.Type == input.KeySpace {
				char = " "
			}
			m.ProjectName += char
			if !m.LocationEdited {
				m.Location = filepath.Join(m.BaseDir, m.ProjectName)
				m.PathInput = m.Location
				m.UpdateSuggestions()
			}
			m.updateDefaultModule()
			return true, false
		}

	case FieldLocation:
		if k.Type == input.KeyBackspace {
			runes := []rune(m.Location)
			if len(runes) > 0 {
				m.Location = string(runes[:len(runes)-1])
				m.PathInput = m.Location
				m.LocationEdited = true
				m.UpdateSuggestions()
			}
			return true, false
		}
		if k.Type == input.KeySpace || (k.Rune >= 32 && !k.HasCtrl() && !k.HasAlt()) {
			char := string(k.Rune)
			if k.Type == input.KeySpace {
				char = " "
			}
			m.Location += char
			m.PathInput = m.Location
			m.LocationEdited = true
			m.UpdateSuggestions()
			return true, false
		}

	case FieldModule:
		if k.Type == input.KeyBackspace {
			runes := []rune(m.ModuleName)
			if len(runes) > 0 {
				m.ModuleName = string(runes[:len(runes)-1])
				m.ModuleEdited = true
			}
			return true, false
		}
		if k.Type == input.KeySpace || (k.Rune >= 32 && !k.HasCtrl() && !k.HasAlt()) {
			char := string(k.Rune)
			if k.Type == input.KeySpace {
				char = " "
			}
			m.ModuleName += char
			m.ModuleEdited = true
			return true, false
		}
	}

	return true, false
}

// HandleClick processes mouse clicks on the fullscreen modal.
func (m *NewProjectModal) HandleClick(mouseX, mouseY, screenW, screenH int) (bool, bool) {
	if !m.Open {
		return false, false
	}

	// Close button ✕ at top-right
	if mouseY == 0 && mouseX >= screenW-4 && mouseX < screenW-1 {
		m.Open = false
		if m.OnCancel != nil {
			m.OnCancel()
		}
		return true, false
	}

	leftColW := 26
	divX := leftColW

	// Click in Left Column (Templates list)
	if mouseX >= 1 && mouseX < divX {
		tplStartY := 3
		if mouseY >= tplStartY && mouseY < tplStartY+len(m.Templates) {
			idx := mouseY - tplStartY
			if idx >= 0 && idx < len(m.Templates) {
				m.SelectedTemplate = idx
				m.ActiveField = FieldTemplates
				m.updateDefaultModule()
				return true, false
			}
		}
	}

	// Bottom action buttons on Row: screenH - 2
	bottomY := screenH - 2
	btnCancelText := " Cancel "
	btnCreateText := " Create Project "
	cancelX := screenW - 2 - len([]rune(btnCancelText)) - 1
	createX := cancelX - len([]rune(btnCreateText)) - 2

	if mouseY == bottomY {
		// Create Project button
		if mouseX >= createX && mouseX < createX+len([]rune(btnCreateText)) {
			return m.ConfirmCreate()
		}
		// Cancel button
		if mouseX >= cancelX && mouseX < cancelX+len([]rune(btnCancelText)) {
			m.Open = false
			if m.OnCancel != nil {
				m.OnCancel()
			}
			return true, false
		}
	}

	// Right Area Input Fields
	rightStartX := divX + 2
	rightW := (screenW - 1) - (divX + 1)
	formW := (rightW * 52) / 100
	if formW > rightW-24 {
		formW = rightW - 24
	}
	if formW < 32 {
		formW = rightW
	}
	midDivX := divX + 1 + formW

	if mouseX >= rightStartX && mouseX < midDivX {
		tpl := m.CurrentTemplate()
		hasModule := tpl.DefaultModule != ""

		// Project Name input at row 5
		if mouseY == 5 {
			m.ActiveField = FieldName
			return true, false
		}
		// Location input at row 7
		if mouseY == 7 {
			m.ActiveField = FieldLocation
			return true, false
		}
		// Suggestions under Location
		if len(m.Suggestions) > 0 && mouseY >= 8 && mouseY < 8+len(m.Suggestions) {
			sugIdx := mouseY - 8
			if sugIdx >= 0 && sugIdx < len(m.Suggestions) {
				m.SelectedSug = sugIdx
				m.Location = m.Suggestions[sugIdx]
				m.PathInput = m.Location
				m.ActiveField = FieldLocation
				return true, false
			}
		}

		offset := 0
		if len(m.Suggestions) > 0 && m.ActiveField == FieldLocation {
			offset = len(m.Suggestions)
		}

		// Module Name input
		if hasModule && mouseY == 9+offset {
			m.ActiveField = FieldModule
			return true, false
		}
		// SDK input
		sdkRow := 9 + offset
		if hasModule {
			sdkRow = 11 + offset
		}
		if mouseY == sdkRow {
			m.ActiveField = FieldSDK
			return true, false
		}
	}

	return true, false
}

// buildFileTree constructs entries for ASCII tree visualization without changing connector colors.
func buildFileTree(projName, modPath string, files []plugin.ProjectTemplateFile) []treeEntry {
	if projName == "" {
		projName = "project"
	}
	if len(files) == 0 {
		return []treeEntry{{prefix: "", name: projName + "/", isDir: true}, {prefix: "└── ", name: "(empty)", isDir: false}}
	}

	type node struct {
		name     string
		isDir    bool
		children map[string]*node
		childArr []*node
	}

	newNode := func(name string, isDir bool) *node {
		return &node{
			name:     name,
			isDir:    isDir,
			children: make(map[string]*node),
		}
	}

	root := newNode(projName+"/", true)

	for _, f := range files {
		cleanPath := strings.ReplaceAll(f.Path, "{{.ProjectName}}", projName)
		cleanPath = strings.ReplaceAll(cleanPath, "{{.ModulePath}}", modPath)
		cleanPath = filepath.ToSlash(filepath.Clean(cleanPath))

		parts := strings.Split(cleanPath, "/")
		curr := root
		for i, part := range parts {
			if part == "" || part == "." {
				continue
			}
			isLast := (i == len(parts)-1)
			child, exists := curr.children[part]
			if !exists {
				child = newNode(part, !isLast)
				curr.children[part] = child
				curr.childArr = append(curr.childArr, child)
			}
			curr = child
		}
	}

	var entries []treeEntry
	entries = append(entries, treeEntry{prefix: "", name: root.name, isDir: true})

	var walk func(n *node, prefix string)
	walk = func(n *node, prefix string) {
		for i, child := range n.childArr {
			isLastChild := (i == len(n.childArr)-1)
			connector := "├── "
			nextPrefix := prefix + "│   "
			if isLastChild {
				connector = "└── "
				nextPrefix = prefix + "    "
			}
			displayName := child.name
			if child.isDir {
				displayName += "/"
			}
			entries = append(entries, treeEntry{
				prefix: prefix + connector,
				name:   displayName,
				isDir:  child.isDir,
			})
			if len(child.childArr) > 0 {
				walk(child, nextPrefix)
			}
		}
	}

	walk(root, "")
	return entries
}

// Render draws the GoLand-style fullscreen New Project dialog.
func (m *NewProjectModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !m.Open {
		return
	}

	bg := toColor(theme.PopupBg)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	activeBg := toColor(theme.PopupSelBg)
	accentFg := toColor(theme.Function)
	commentFg := toColor(theme.Comment)
	keywordFg := toColor(theme.Keyword)

	leftColW := 26
	divX := leftColW

	// 1. Draw outer frame covering 100% of the screen (True Fullscreen)
	for y := 0; y < screenH; y++ {
		for x := 0; x < screenW; x++ {
			r := ' '
			f := borderFg
			b := bg
			if y == 0 && x == 0 {
				r = '┌'
			} else if y == 0 && x == screenW-1 {
				r = '┐'
			} else if y == screenH-1 && x == 0 {
				r = '└'
			} else if y == screenH-1 && x == screenW-1 {
				r = '┘'
			} else if y == 0 || y == screenH-1 {
				r = '─'
			} else if x == 0 || x == screenW-1 {
				r = '│'
			}
			buf.SetRune(x, y, r, f, b, cell.AttrNone)
		}
	}

	// Vertical column separator between Templates and Content
	for y := 1; y < screenH-1; y++ {
		buf.SetRune(divX, y, '│', borderFg, bg, cell.AttrNone)
	}
	buf.SetRune(divX, 0, '┬', borderFg, bg, cell.AttrNone)
	buf.SetRune(divX, screenH-1, '┴', borderFg, bg, cell.AttrNone)

	// Top Title (Clean, NO square brackets)
	title := " CREATE NEW PROJECT "
	for i, r := range title {
		if 2+i < divX-1 {
			buf.SetRune(2+i, 0, r, accentFg, bg, cell.AttrBold)
		}
	}

	// Close button at top-right: ✕ (Clean, NO square brackets)
	buf.SetRune(screenW-3, 0, '✕', toColor(theme.DiagnosticError), bg, cell.AttrBold)

	// 2. Left Column: Templates List (NO emojis/smileys)
	lblTemplates := " PROJECT TEMPLATES "
	for i, r := range lblTemplates {
		if 2+i < divX {
			buf.SetRune(2+i, 1, r, commentFg, bg, cell.AttrBold)
		}
	}
	// Divider under templates label
	for x := 1; x < divX; x++ {
		buf.SetRune(x, 2, '─', borderFg, bg, cell.AttrNone)
	}

	tplStartY := 3
	for i, t := range m.Templates {
		if tplStartY+i >= screenH-3 {
			break
		}
		isSel := (i == m.SelectedTemplate)
		rowBg := bg
		rowFg := fg
		prefix := "  "
		if isSel {
			rowBg = activeBg
			rowFg = accentFg
			prefix = "> "
		}

		itemText := fmt.Sprintf("%s%s", prefix, t.Name)

		curX := 2
		for _, r := range itemText {
			if curX < divX {
				w := runewidth.RuneWidth(r)
				buf.SetRune(curX, tplStartY+i, r, rowFg, rowBg, cell.AttrNone)
				curX += w
			}
		}
		// Fill remaining width of template item row with background
		for curX < divX {
			buf.SetRune(curX, tplStartY+i, ' ', rowFg, rowBg, cell.AttrNone)
			curX++
		}
	}

	// 3. Right Area: Template Details and Inputs
	rightStartX := divX + 2
	rightW := (screenW - 1) - (divX + 1)
	rightEndX := screenW - 2

	currTpl := m.CurrentTemplate()
	hasModule := currTpl.DefaultModule != ""

	// Template Header (Clean, NO emojis/smileys)
	headerText := currTpl.Name
	curX := rightStartX
	for _, r := range headerText {
		if curX < rightEndX {
			w := runewidth.RuneWidth(r)
			buf.SetRune(curX, 1, r, accentFg, bg, cell.AttrBold)
			curX += w
		}
	}

	// Category Badge (Clean, NO emojis/smileys)
	if currTpl.Category != "" {
		badgeText := fmt.Sprintf(" %s ", currTpl.Category)
		badgeX := curX + 2
		for _, r := range badgeText {
			if badgeX < rightEndX {
				buf.SetRune(badgeX, 1, r, fg, activeBg, cell.AttrNone)
				badgeX++
			}
		}
	}

	// Description line
	descText := currTpl.Description
	for i, r := range descText {
		if rightStartX+i < rightEndX {
			buf.SetRune(rightStartX+i, 2, r, commentFg, bg, cell.AttrNone)
		}
	}

	// Separator below description
	for x := divX + 1; x < screenW-1; x++ {
		buf.SetRune(x, 3, '─', borderFg, bg, cell.AttrNone)
	}

	// Determine split layout in right panel: form on left, file tree preview on right
	formW := (rightW * 52) / 100
	if formW > rightW-24 {
		formW = rightW - 24
	}
	if formW < 34 {
		formW = rightW
	}
	midDivX := divX + 1 + formW
	previewStartX := midDivX + 2
	previewW := (screenW - 1) - previewStartX

	// Middle vertical divider between Form and Preview (if space permits)
	if previewW >= 20 {
		for y := 4; y < screenH-3; y++ {
			buf.SetRune(midDivX, y, '│', borderFg, bg, cell.AttrNone)
		}
		buf.SetRune(midDivX, 3, '┬', borderFg, bg, cell.AttrNone)
		buf.SetRune(midDivX, screenH-3, '┴', borderFg, bg, cell.AttrNone)
	}

	// Draw Input Fields in Form Area
	drawInputField := func(label, val string, row int, isFocused bool) {
		// Label
		for i, r := range label {
			if rightStartX+i < midDivX {
				buf.SetRune(rightStartX+i, row, r, fg, bg, cell.AttrBold)
			}
		}
		// Input box
		inX := rightStartX + len([]rune(label)) + 1
		inW := midDivX - inX - 2
		if inW < 10 {
			inW = 10
		}
		inBg := bg
		if isFocused {
			inBg = activeBg
		}

		dispVal := val
		if isFocused {
			dispVal += "▏"
		}
		dispRunes := []rune(dispVal)
		for i := 0; i < inW; i++ {
			r := ' '
			if i < len(dispRunes) {
				r = dispRunes[i]
			}
			buf.SetRune(inX+i, row, r, fg, inBg, cell.AttrNone)
		}
	}

	// Field 1: Project Name
	drawInputField("Project Name:", m.ProjectName, 5, m.ActiveField == FieldName)

	// Field 2: Location
	drawInputField("Location:    ", m.Location, 7, m.ActiveField == FieldLocation)

	// Suggestions under Location
	sugOffset := 0
	if len(m.Suggestions) > 0 && m.ActiveField == FieldLocation {
		for idx, sug := range m.Suggestions {
			sugRow := 8 + idx
			if sugRow >= screenH-4 {
				break
			}
			sugFg := commentFg
			sugBg := bg
			prefix := "  "
			if idx == m.SelectedSug {
				sugFg = keywordFg
				sugBg = activeBg
				prefix = "> "
			}
			line := fmt.Sprintf("%s%s", prefix, sug)
			for i, r := range line {
				if rightStartX+i < midDivX {
					buf.SetRune(rightStartX+i, sugRow, r, sugFg, sugBg, cell.AttrNone)
				}
			}
		}
		sugOffset = len(m.Suggestions)
	}

	// Field 3: Module Name (if supported by template)
	modRow := 9 + sugOffset
	if hasModule {
		drawInputField("Module / Pkg:", m.ModuleName, modRow, m.ActiveField == FieldModule)
	}

	// Field 4: SDK Toolchain
	sdkRow := modRow + 2
	if !hasModule {
		sdkRow = 9 + sugOffset
	}
	sdkText := m.SDKInfo
	if sdkText == "" {
		sdkText = "Auto-detected SDK toolchain"
	}
	drawInputField("SDK Toolchain:", sdkText, sdkRow, m.ActiveField == FieldSDK)

	// Draw ASCII File Tree Preview
	if previewW >= 20 {
		treeHeader := " PROJECT STRUCTURE PREVIEW "
		for i, r := range treeHeader {
			if previewStartX+i < rightEndX {
				buf.SetRune(previewStartX+i, 5, r, commentFg, bg, cell.AttrBold)
			}
		}

		projName := strings.TrimSpace(m.ProjectName)
		if projName == "" {
			projName = "my-app"
		}
		modPath := strings.TrimSpace(m.ModuleName)
		if modPath == "" {
			modPath = projName
		}
		treeEntries := buildFileTree(projName, modPath, currTpl.Files)

		treeStartY := 7
		for idx, entry := range treeEntries {
			if treeStartY+idx >= screenH-4 {
				break
			}

			// 1. Draw branch connector in uniform borderFg
			curTreeX := previewStartX
			for _, r := range entry.prefix {
				if curTreeX < rightEndX {
					w := runewidth.RuneWidth(r)
					buf.SetRune(curTreeX, treeStartY+idx, r, borderFg, bg, cell.AttrNone)
					curTreeX += w
				}
			}

			// 2. Draw file or directory name with syntax color
			nameFg := fg
			if entry.isDir {
				nameFg = accentFg
			} else if strings.HasSuffix(entry.name, ".mod") || strings.HasSuffix(entry.name, ".toml") {
				nameFg = keywordFg
			} else if strings.HasPrefix(entry.name, ".") {
				nameFg = commentFg
			}

			for _, r := range entry.name {
				if curTreeX < rightEndX {
					w := runewidth.RuneWidth(r)
					buf.SetRune(curTreeX, treeStartY+idx, r, nameFg, bg, cell.AttrNone)
					curTreeX += w
				}
			}
		}
	}

	// 4. Bottom Separator above Action Bar
	for x := 1; x < screenW-1; x++ {
		r := '─'
		if x == divX {
			r = '┴'
		}
		buf.SetRune(x, screenH-3, r, borderFg, bg, cell.AttrNone)
	}

	// 5. Bottom Action Bar (Row: screenH - 2) (ALL buttons WITHOUT square brackets)
	bottomY := screenH - 2

	// Action buttons (Create Project & Cancel)
	btnCancelText := " Cancel "
	btnCreateText := " Create Project "
	cancelX := screenW - 2 - len([]rune(btnCancelText)) - 1
	createX := cancelX - len([]rune(btnCreateText)) - 2

	// Help shortcuts on bottom-left (guaranteed not to overlap buttons)
	hint := "Tab: Next Field  │  ↑/↓: Select  │  Enter: Create  │  Esc: Cancel"
	for i, r := range hint {
		if 2+i < createX-2 {
			buf.SetRune(2+i, bottomY, r, commentFg, bg, cell.AttrNone)
		}
	}

	// Create Project button
	createBg := activeBg
	createFg := accentFg
	if m.ActiveField == FieldCreateBtn {
		createBg = toColor(theme.Function)
		createFg = toColor(theme.Background)
	}
	for i, r := range btnCreateText {
		buf.SetRune(createX+i, bottomY, r, createFg, createBg, cell.AttrBold)
	}

	// Cancel button
	cancelBg := bg
	cancelFg := commentFg
	if m.ActiveField == FieldCancelBtn {
		cancelBg = activeBg
		cancelFg = fg
	}
	for i, r := range btnCancelText {
		buf.SetRune(cancelX+i, bottomY, r, cancelFg, cancelBg, cell.AttrNone)
	}
}
