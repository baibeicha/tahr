package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/launch"
	"tahr/internal/core/plugin"
	"tahr/internal/ui"
)

// LaunchModalMode defines whether the modal is in list view or editing/adding a profile.
type LaunchModalMode int

const (
	LaunchModeList LaunchModalMode = iota
	LaunchModeEdit
	LaunchModeAdd
)

// LaunchConfigModal provides an interactive UI for managing .tahr/launch.json configurations.
type LaunchConfigModal struct {
	Open                bool
	Config              *launch.Config
	SelectedIdx         int
	Mode                LaunchModalMode
	EditFieldIdx        int
	EditProfile         launch.Profile
	InputBuffer         string
	StatusMsg           string
	Templates           []plugin.LaunchTemplate
	SupportedTypes      []string
	SelectedTemplateIdx int
	BorderRounded       bool
	OnOpenFile          func(path string)

	OnLaunch func(p launch.Profile)
	OnDebug  func(p launch.Profile)
}

func defaultLaunchTemplates() []plugin.LaunchTemplate {
	return []plugin.LaunchTemplate{
		{
			Name:    "Shell: Custom Command",
			Type:    "shell",
			Request: "launch",
			Target:  "echo 'Running custom command'",
			Console: "integratedTerminal",
		},
	}
}

// SetSupportedTypes configures available execution types discovered from installed plugins.
func (lm *LaunchConfigModal) SetSupportedTypes(types []string) {
	if len(types) > 0 {
		lm.SupportedTypes = types
	}
}

// SetTemplates sets available launch templates.
func (lm *LaunchConfigModal) SetTemplates(templates []plugin.LaunchTemplate) {
	if len(templates) > 0 {
		lm.Templates = templates
	}
}

// NewLaunchConfigModal creates a new modal instance.
func NewLaunchConfigModal(cfg *launch.Config) *LaunchConfigModal {
	return &LaunchConfigModal{
		Open:           false,
		Config:         cfg,
		SelectedIdx:    0,
		Mode:           LaunchModeList,
		Templates:      defaultLaunchTemplates(),
		SupportedTypes: []string{"go", "shell"},
	}
}

// OpenModal activates the configuration dialog.
func (lm *LaunchConfigModal) OpenModal(cfg *launch.Config) {
	if cfg != nil {
		lm.Config = cfg
	}
	lm.Open = true
	lm.Mode = LaunchModeList
	lm.StatusMsg = ""
	if lm.Config != nil && len(lm.Config.Configurations) > 0 {
		for i, p := range lm.Config.Configurations {
			if p.Name == lm.Config.ActiveProfile {
				lm.SelectedIdx = i
				return
			}
		}
		if lm.SelectedIdx >= len(lm.Config.Configurations) {
			lm.SelectedIdx = 0
		}
	} else {
		lm.SelectedIdx = 0
	}
}

// HandleKey handles keyboard navigation, CRUD actions, and launching.
func (lm *LaunchConfigModal) HandleKey(k input.Key) (bool, bool) {
	if !lm.Open {
		return false, false
	}

	if k.Type == input.KeyEsc {
		if lm.Mode != LaunchModeList {
			lm.Mode = LaunchModeList
			lm.StatusMsg = "Cancelled editing"
			return true, false
		}
		lm.Open = false
		return true, true
	}

	if lm.Mode == LaunchModeList {
		return lm.handleListKey(k)
	}

	return lm.handleEditKey(k)
}

func (lm *LaunchConfigModal) handleListKey(k input.Key) (bool, bool) {
	if lm.Config == nil || len(lm.Config.Configurations) == 0 {
		if k.Rune == 'a' || k.Rune == 'A' {
			lm.startAdd()
			return true, false
		}
		return true, false
	}

	count := len(lm.Config.Configurations)
	totalSelectable := count + 1 // count is "+ New from template"

	switch k.Type {
	case input.KeyUp:
		if lm.SelectedIdx > 0 {
			lm.SelectedIdx--
		}
		return true, false

	case input.KeyDown:
		if lm.SelectedIdx+1 < totalSelectable {
			lm.SelectedIdx++
		}
		return true, false

	case input.KeyEnter, input.KeySpace:
		if lm.SelectedIdx == count {
			lm.startAdd()
			return true, false
		}
		if lm.SelectedIdx >= 0 && lm.SelectedIdx < count {
			name := lm.Config.Configurations[lm.SelectedIdx].Name
			lm.Config.SetActive(name)
			_ = lm.Config.Save()
			lm.StatusMsg = fmt.Sprintf("Active profile set to '%s'", name)
		}
		return true, false

	case input.KeyDelete:
		if lm.SelectedIdx < count {
			lm.deleteSelected()
		}
		return true, false

	case input.KeyF5:
		return lm.launchSelected(false)

	case input.KeyF9:
		return lm.launchSelected(true)
	}

	switch k.Rune {
	case 'k', 'K':
		if lm.SelectedIdx > 0 {
			lm.SelectedIdx--
		}
		return true, false

	case 'j', 'J':
		if lm.SelectedIdx+1 < totalSelectable {
			lm.SelectedIdx++
		}
		return true, false

	case 'a', 'A':
		lm.startAdd()
		return true, false

	case 'e', 'E':
		if lm.SelectedIdx == count {
			lm.startAdd()
		} else {
			lm.startEdit()
		}
		return true, false

	case 'd', 'D', 'x', 'X':
		if k.Rune == 'D' {
			// Capital D launches debug
			return lm.launchSelected(true)
		}
		if lm.SelectedIdx < count {
			lm.deleteSelected()
		}
		return true, false

	case 'r', 'R':
		return lm.launchSelected(false)

	case 'o', 'O':
		lm.openInEditor()
		return true, true
	}

	return true, false
}

func (lm *LaunchConfigModal) openInEditor() {
	if lm.Config != nil {
		_ = lm.Config.Save()
		target := lm.Config.FilePath()
		if target != "" && lm.OnOpenFile != nil {
			lm.Open = false
			lm.OnOpenFile(target)
		}
	}
}

func (lm *LaunchConfigModal) launchSelected(debug bool) (bool, bool) {
	if lm.Config == nil || len(lm.Config.Configurations) == 0 {
		return true, false
	}
	if lm.SelectedIdx < 0 || lm.SelectedIdx >= len(lm.Config.Configurations) {
		lm.SelectedIdx = 0
	}
	p := lm.Config.Configurations[lm.SelectedIdx]
	lm.Open = false
	if debug {
		if lm.OnDebug != nil {
			lm.OnDebug(p)
		}
	} else {
		if lm.OnLaunch != nil {
			lm.OnLaunch(p)
		}
	}
	return true, true
}

func (lm *LaunchConfigModal) startAdd() {
	lm.Mode = LaunchModeAdd
	lm.EditProfile = launch.Profile{
		Name:    "New Profile",
		Type:    "go",
		Request: "launch",
		Target:  ".",
		Console: "integratedTerminal",
	}
	lm.EditFieldIdx = 0
	lm.InputBuffer = lm.EditProfile.Name
	lm.StatusMsg = "Adding new configuration profile"
}

func (lm *LaunchConfigModal) startEdit() {
	if lm.Config == nil || len(lm.Config.Configurations) == 0 {
		return
	}
	if lm.SelectedIdx < 0 || lm.SelectedIdx >= len(lm.Config.Configurations) {
		lm.SelectedIdx = 0
	}
	lm.Mode = LaunchModeEdit
	lm.EditProfile = lm.Config.Configurations[lm.SelectedIdx]
	lm.EditFieldIdx = 0
	lm.InputBuffer = lm.EditProfile.Name
	lm.StatusMsg = fmt.Sprintf("Editing profile '%s'", lm.EditProfile.Name)
}

func (lm *LaunchConfigModal) deleteSelected() {
	if lm.Config == nil || len(lm.Config.Configurations) == 0 {
		return
	}
	if lm.SelectedIdx >= 0 && lm.SelectedIdx < len(lm.Config.Configurations) {
		deletedName := lm.Config.Configurations[lm.SelectedIdx].Name
		lm.Config.DeleteProfile(lm.SelectedIdx)
		if lm.SelectedIdx >= len(lm.Config.Configurations) {
			lm.SelectedIdx = max(0, len(lm.Config.Configurations)-1)
		}
		lm.StatusMsg = fmt.Sprintf("Deleted profile '%s'", deletedName)
	}
}

func (lm *LaunchConfigModal) handleEditKey(k input.Key) (bool, bool) {
	// Ctrl+S saves profile immediately
	if k.Rune == 19 || (k.HasCtrl() && (k.Rune == 's' || k.Rune == 'S' || matchKey(k, 's', 'ы'))) {
		lm.commitCurrentField()
		lm.saveProfile()
		return true, false
	}

	switch k.Type {
	case input.KeyTab:
		lm.commitCurrentField()
		lm.EditFieldIdx = (lm.EditFieldIdx + 1) % 8
		lm.loadFieldToInputBuffer()
		return true, false

	case input.KeyBacktab:
		lm.commitCurrentField()
		lm.EditFieldIdx = (lm.EditFieldIdx + 7) % 8
		lm.loadFieldToInputBuffer()
		return true, false

	case input.KeyUp:
		if lm.EditFieldIdx == 1 {
			lm.cycleType(-1)
			return true, false
		} else if lm.EditFieldIdx == 2 {
			lm.cycleRequest()
			return true, false
		} else if lm.EditFieldIdx == 6 {
			lm.cycleConsole()
			return true, false
		} else {
			lm.commitCurrentField()
			if lm.EditFieldIdx > 0 {
				lm.EditFieldIdx--
				lm.loadFieldToInputBuffer()
			}
			return true, false
		}

	case input.KeyDown:
		if lm.EditFieldIdx == 1 {
			lm.cycleType(1)
			return true, false
		} else if lm.EditFieldIdx == 2 {
			lm.cycleRequest()
			return true, false
		} else if lm.EditFieldIdx == 6 {
			lm.cycleConsole()
			return true, false
		} else {
			lm.commitCurrentField()
			if lm.EditFieldIdx < 7 {
				lm.EditFieldIdx++
				lm.loadFieldToInputBuffer()
			}
			return true, false
		}

	case input.KeyEnter:
		lm.commitCurrentField()
		if lm.EditFieldIdx == 7 {
			lm.saveProfile()
		} else {
			lm.EditFieldIdx++
			lm.loadFieldToInputBuffer()
		}
		return true, false

	case input.KeyBackspace:
		if lm.EditFieldIdx != 2 && lm.EditFieldIdx != 6 {
			runes := []rune(lm.InputBuffer)
			if len(runes) > 0 {
				lm.InputBuffer = string(runes[:len(runes)-1])
			}
		}
		return true, false

	case input.KeySpace:
		if lm.EditFieldIdx == 1 {
			lm.cycleType(1)
		} else if lm.EditFieldIdx == 2 {
			lm.cycleRequest()
		} else if lm.EditFieldIdx == 6 {
			lm.cycleConsole()
		} else {
			lm.InputBuffer += " "
		}
		return true, false

	case input.KeyRune:
		if lm.EditFieldIdx != 2 && lm.EditFieldIdx != 6 && k.Rune >= 32 {
			lm.InputBuffer += string(k.Rune)
		}
		return true, false
	}

	return true, false
}

func (lm *LaunchConfigModal) cycleType(dir int) {
	types := lm.SupportedTypes
	if len(types) == 0 {
		types = []string{"go", "shell"}
	}
	cur := strings.ToLower(lm.EditProfile.Type)
	idx := 0
	for i, t := range types {
		if t == cur {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(types)) % len(types)
	lm.EditProfile.Type = types[idx]
	lm.InputBuffer = lm.EditProfile.Type
}

func (lm *LaunchConfigModal) cycleRequest() {
	if strings.ToLower(lm.EditProfile.Request) == "debug" {
		lm.EditProfile.Request = "launch"
	} else {
		lm.EditProfile.Request = "debug"
	}
	lm.InputBuffer = lm.EditProfile.Request
}

func (lm *LaunchConfigModal) cycleConsole() {
	if strings.ToLower(lm.EditProfile.Console) == "integratedterminal" {
		lm.EditProfile.Console = "internalConsole"
	} else {
		lm.EditProfile.Console = "integratedTerminal"
	}
	lm.InputBuffer = lm.EditProfile.Console
}

func (lm *LaunchConfigModal) commitCurrentField() {
	switch lm.EditFieldIdx {
	case 0:
		lm.EditProfile.Name = strings.TrimSpace(lm.InputBuffer)
	case 1:
		lm.EditProfile.Type = strings.TrimSpace(lm.InputBuffer)
	case 2:
		lm.EditProfile.Request = strings.TrimSpace(lm.InputBuffer)
	case 3:
		lm.EditProfile.Target = strings.TrimSpace(lm.InputBuffer)
	case 4:
		argsStr := strings.TrimSpace(lm.InputBuffer)
		if argsStr == "" {
			lm.EditProfile.Args = nil
		} else {
			lm.EditProfile.Args = strings.Fields(argsStr)
		}
	case 5:
		lm.EditProfile.Cwd = strings.TrimSpace(lm.InputBuffer)
	case 6:
		lm.EditProfile.Console = strings.TrimSpace(lm.InputBuffer)
	case 7:
		lm.EditProfile.PreLaunchTask = strings.TrimSpace(lm.InputBuffer)
	}
}

func (lm *LaunchConfigModal) loadFieldToInputBuffer() {
	switch lm.EditFieldIdx {
	case 0:
		lm.InputBuffer = lm.EditProfile.Name
	case 1:
		lm.InputBuffer = lm.EditProfile.Type
	case 2:
		lm.InputBuffer = lm.EditProfile.Request
	case 3:
		lm.InputBuffer = lm.EditProfile.Target
	case 4:
		lm.InputBuffer = strings.Join(lm.EditProfile.Args, " ")
	case 5:
		lm.InputBuffer = lm.EditProfile.Cwd
	case 6:
		lm.InputBuffer = lm.EditProfile.Console
	case 7:
		lm.InputBuffer = lm.EditProfile.PreLaunchTask
	}
}

func (lm *LaunchConfigModal) saveProfile() {
	if lm.EditProfile.Name == "" {
		lm.EditProfile.Name = "Unnamed Profile"
	}
	if lm.EditProfile.Type == "" {
		lm.EditProfile.Type = "go"
	}
	if lm.EditProfile.Request == "" {
		lm.EditProfile.Request = "launch"
	}

	if lm.Mode == LaunchModeAdd {
		lm.Config.AddProfile(lm.EditProfile)
		lm.SelectedIdx = len(lm.Config.Configurations) - 1
		lm.StatusMsg = fmt.Sprintf("Added profile '%s'", lm.EditProfile.Name)
	} else if lm.Mode == LaunchModeEdit {
		lm.Config.UpdateProfile(lm.SelectedIdx, lm.EditProfile)
		lm.StatusMsg = fmt.Sprintf("Updated profile '%s'", lm.EditProfile.Name)
	}
	lm.Mode = LaunchModeList
}

// HandleClick handles mouse clicks inside the launch modal.
func (lm *LaunchConfigModal) HandleClick(mouseX, mouseY, screenW, screenH int) (bool, bool) {
	if !lm.Open {
		return false, false
	}

	modalW := screenW - 12
	modalH := screenH - 6
	if modalW > 84 {
		modalW = 84
	}
	if modalH > 22 {
		modalH = 22
	}
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	if mouseX < startX || mouseX >= startX+modalW || mouseY < startY || mouseY >= startY+modalH {
		if lm.Mode != LaunchModeList {
			lm.Mode = LaunchModeList
			return true, false
		}
		lm.Open = false
		return true, true
	}

	if lm.Mode == LaunchModeList && lm.Config != nil {
		contentTop := startY + 3
		leftColW := 28
		if leftColW > modalW/2 {
			leftColW = modalW / 2
		}
		dividerX := startX + leftColW
		cfgCount := len(lm.Config.Configurations)
		maxItems := modalH - 7

		// 1. Check "+ New from template" button click
		tplBtnY := contentTop + 1 + min(maxItems-1, max(1, cfgCount))
		if mouseY == tplBtnY && mouseX >= startX+1 && mouseX < dividerX {
			lm.SelectedIdx = cfgCount
			lm.startAdd()
			return true, false
		}

		// 2. Check profile row clicks
		listTop := contentTop + 1
		visibleCount := min(maxItems, cfgCount)
		if mouseY >= listTop && mouseY < listTop+visibleCount && mouseX >= startX+1 && mouseX < dividerX {
			clicked := mouseY - listTop
			if clicked == lm.SelectedIdx {
				// Double-click / re-click launches
				return lm.launchSelected(false)
			}
			lm.SelectedIdx = clicked
			return true, false
		}
		return true, false
	}

	if lm.Mode == LaunchModeEdit || lm.Mode == LaunchModeAdd {
		contentTop := startY + 3
		// Check clicking on individual fields
		for i := 0; i < 8; i++ {
			rowY := contentTop + (i * 2)
			if mouseY == rowY && mouseX >= startX+3 && mouseX < startX+modalW-3 {
				lm.commitCurrentField()
				lm.EditFieldIdx = i
				lm.loadFieldToInputBuffer()
				return true, false
			}
		}

		// Check clicking on bottom action buttons
		bottomY := startY + modalH - 2
		if mouseY == bottomY {
			saveBtnLen := len(" Save Profile (Ctrl+S) ")
			if mouseX >= startX+3 && mouseX < startX+3+saveBtnLen {
				lm.commitCurrentField()
				lm.saveProfile()
				return true, false
			}
			cancelX := startX + 3 + saveBtnLen + 2
			cancelBtnLen := len(" Cancel (Esc) ")
			if mouseX >= cancelX && mouseX < cancelX+cancelBtnLen {
				lm.Mode = LaunchModeList
				return true, false
			}
		}
		return true, false
	}

	return true, false
}

// Render draws the interactive launch configuration modal into the buffer.
func (lm *LaunchConfigModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !lm.Open || buf == nil {
		return
	}

	modalW := screenW - 12
	modalH := screenH - 6
	if modalW > 84 {
		modalW = 84
	}
	if modalH > 22 {
		modalH = 22
	}
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	boxBg := toColor(theme.PopupBg)
	boxFg := toColor(theme.PopupFg)
	borderFg := toColor(theme.BorderColor)
	selBg := toColor(theme.PopupSelBg)
	selFg := toColor(theme.PopupSelFg)
	accentFg := toColor(theme.Function)
	keywordFg := toColor(theme.Keyword)
	commentFg := toColor(theme.Comment)

	// Draw modal background and border
	tl, tr, bl, br := '┌', '┐', '└', '┘'
	if lm.BorderRounded {
		tl, tr, bl, br = '╭', '╮', '╰', '╯'
	}
	for y := startY; y < startY+modalH; y++ {
		for x := startX; x < startX+modalW; x++ {
			r := ' '
			fg := boxFg
			if y == startY || y == startY+modalH-1 || x == startX || x == startX+modalW-1 {
				fg = borderFg
				if y == startY && x == startX {
					r = tl
				} else if y == startY && x == startX+modalW-1 {
					r = tr
				} else if y == startY+modalH-1 && x == startX {
					r = bl
				} else if y == startY+modalH-1 && x == startX+modalW-1 {
					r = br
				} else if y == startY || y == startY+modalH-1 {
					r = '─'
				} else {
					r = '│'
				}
			}
			buf.SetRune(x, y, r, fg, boxBg, cell.AttrNone)
		}
	}

	// Title
	title := " Launch & Debug Configurations (.tahr/launch.json) "
	for i, r := range title {
		if startX+3+i < startX+modalW-2 {
			buf.SetRune(startX+3+i, startY, r, keywordFg, boxBg, cell.AttrBold)
		}
	}

	if lm.Mode == LaunchModeList {
		lm.renderListView(buf, startX, startY, modalW, modalH, boxBg, boxFg, selBg, selFg, accentFg, commentFg)
	} else {
		lm.renderEditView(buf, startX, startY, modalW, modalH, boxBg, boxFg, selBg, selFg, accentFg, commentFg)
	}
}

func (lm *LaunchConfigModal) renderListView(buf *buffer.Buffer, startX, startY, modalW, modalH int, boxBg, boxFg, selBg, selFg, accentFg, commentFg cell.Color) {
	// Top hint without brackets
	hint := " ↑/↓: Navigate  │  Space: Set Active  │  a: Add  │  e: Edit  │  d: Delete  │  o: Open File  │  F5: Run "
	for i, r := range hint {
		if startX+2+i < startX+modalW-2 {
			buf.SetRune(startX+2+i, startY+1, r, commentFg, boxBg, cell.AttrNone)
		}
	}

	// Divider
	for x := startX + 1; x < startX+modalW-1; x++ {
		buf.SetRune(x, startY+2, '─', commentFg, boxBg, cell.AttrNone)
	}

	contentTop := startY + 3
	leftColW := 28
	if leftColW > modalW/2 {
		leftColW = modalW / 2
	}
	dividerX := startX + leftColW

	// Vertical divider between left and right column
	for y := startY + 2; y < startY+modalH-2; y++ {
		buf.SetRune(dividerX, y, '│', commentFg, boxBg, cell.AttrNone)
	}

	// Left column header
	leftHeader := " Profiles "
	for i, r := range leftHeader {
		if startX+2+i < dividerX {
			buf.SetRune(startX+2+i, contentTop, r, accentFg, boxBg, cell.AttrBold)
		}
	}

	maxItems := modalH - 7
	cfgCount := 0
	if lm.Config != nil {
		cfgCount = len(lm.Config.Configurations)
	}

	if cfgCount == 0 {
		emptyMsg := "No launch profiles"
		for i, r := range emptyMsg {
			if startX+2+i < dividerX {
				buf.SetRune(startX+2+i, contentTop+2, r, commentFg, boxBg, cell.AttrItalic)
			}
		}
	} else {
		for i := 0; i < maxItems && i < cfgCount; i++ {
			p := lm.Config.Configurations[i]
			isSel := (i == lm.SelectedIdx)
			rowY := contentTop + 1 + i

			rowBg := boxBg
			rowFg := boxFg
			attr := cell.AttrNone
			if isSel {
				rowBg = selBg
				rowFg = selFg
				attr = cell.AttrBold
			}

			activeMarker := "  "
			if p.Name == lm.Config.ActiveProfile {
				activeMarker = "✓ "
			}

			rowText := fmt.Sprintf("%s%s", activeMarker, p.Name)
			rowRunes := []rune(rowText)

			for x := 0; x < leftColW-2; x++ {
				r := ' '
				fg := rowFg
				if x < len(rowRunes) {
					r = rowRunes[x]
					if !isSel && p.Name == lm.Config.ActiveProfile && x < 2 {
						fg = accentFg
					}
				}
				buf.SetRune(startX+1+x, rowY, r, fg, rowBg, attr)
			}
		}
	}

	// "+ New from template" button at bottom of left column
	tplBtnY := contentTop + 1 + min(maxItems-1, max(1, cfgCount))
	if tplBtnY < startY+modalH-2 {
		tplLabel := "+ New from template"
		isTplSel := (lm.SelectedIdx == cfgCount)
		btnBg := boxBg
		btnFg := accentFg
		btnAttr := cell.AttrBold
		if isTplSel {
			btnBg = selBg
			btnFg = selFg
		}
		for x := 0; x < leftColW-2; x++ {
			r := ' '
			if x >= 1 && x-1 < len(tplLabel) {
				r = rune(tplLabel[x-1])
			}
			buf.SetRune(startX+1+x, tplBtnY, r, btnFg, btnBg, btnAttr)
		}
	}

	// Right column: Details for selected profile
	rightStartX := dividerX + 2
	rightHeader := " Configuration Details "
	for i, r := range rightHeader {
		if rightStartX+i < startX+modalW-2 {
			buf.SetRune(rightStartX+i, contentTop, r, accentFg, boxBg, cell.AttrBold)
		}
	}

	if lm.Config != nil && lm.SelectedIdx >= 0 && lm.SelectedIdx < cfgCount {
		p := lm.Config.Configurations[lm.SelectedIdx]
		targetLbl := "Target:"
		if strings.EqualFold(p.Type, "shell") {
			targetLbl = "Command:"
		}
		details := []struct {
			lbl string
			val string
		}{
			{"Name:", p.Name},
			{"Type:", p.Type},
			{"Request:", p.Request},
			{targetLbl, p.Target},
			{"Args:", strings.Join(p.Args, " ")},
			{"Cwd:", p.Cwd},
			{"Console:", p.Console},
			{"PreLaunch:", p.PreLaunchTask},
		}

		for di, d := range details {
			dRowY := contentTop + 1 + di
			if dRowY >= startY+modalH-3 {
				break
			}
			for li, r := range d.lbl {
				if rightStartX+li < startX+modalW-2 {
					buf.SetRune(rightStartX+li, dRowY, r, commentFg, boxBg, cell.AttrBold)
				}
			}
			valX := rightStartX + 12
			for vi, r := range d.val {
				if valX+vi < startX+modalW-2 {
					buf.SetRune(valX+vi, dRowY, r, boxFg, boxBg, cell.AttrNone)
				}
			}
		}

		// Action buttons at bottom of right column without brackets
		actionRowY := startY + modalH - 3
		actions := " Edit (e)   Delete (d)   Open launch.json (o)   Run (F5) "
		for ai, r := range actions {
			if rightStartX+ai < startX+modalW-2 {
				buf.SetRune(rightStartX+ai, actionRowY, r, accentFg, boxBg, cell.AttrBold)
			}
		}
	} else if lm.SelectedIdx == cfgCount {
		helpLines := []string{
			"Create a new launch profile.",
			"",
			"Available templates from plugins:",
		}
		for ti, tmpl := range lm.Templates {
			if ti >= 5 {
				break
			}
			helpLines = append(helpLines, fmt.Sprintf("• %s (%s)", tmpl.Name, tmpl.Type))
		}
		helpLines = append(helpLines, "", "Press Enter or click to create new profile.")
		for hi, hLine := range helpLines {
			hRowY := contentTop + 1 + hi
			if hRowY >= startY+modalH-3 {
				break
			}
			for li, r := range hLine {
				if rightStartX+li < startX+modalW-2 {
					fg := boxFg
					attr := cell.AttrNone
					if strings.HasPrefix(hLine, "•") {
						fg = accentFg
						attr = cell.AttrBold
					} else if hi == 0 {
						fg = commentFg
					}
					buf.SetRune(rightStartX+li, hRowY, r, fg, boxBg, attr)
				}
			}
		}
	}

	// Status line at bottom
	statusY := startY + modalH - 2
	statusText := lm.StatusMsg
	if statusText == "" && lm.Config != nil {
		statusText = fmt.Sprintf("Active: %s  │  Total Configurations: %d", lm.Config.ActiveProfile, len(lm.Config.Configurations))
	}
	for i, r := range statusText {
		if startX+2+i < startX+modalW-2 {
			buf.SetRune(startX+2+i, statusY, r, accentFg, boxBg, cell.AttrNone)
		}
	}
}

func (lm *LaunchConfigModal) renderEditView(buf *buffer.Buffer, startX, startY, modalW, modalH int, boxBg, boxFg, selBg, selFg, accentFg, commentFg cell.Color) {
	subTitle := " EDIT CONFIGURATION PROFILE "
	if lm.Mode == LaunchModeAdd {
		subTitle = " ADD CONFIGURATION PROFILE "
	}
	for i, r := range subTitle {
		if startX+3+i < startX+modalW-2 {
			buf.SetRune(startX+3+i, startY+1, r, accentFg, boxBg, cell.AttrBold)
		}
	}

	// Divider
	for x := startX + 1; x < startX+modalW-1; x++ {
		buf.SetRune(x, startY+2, '─', commentFg, boxBg, cell.AttrNone)
	}

	typeHint := "(Space/Up/Down: select, or type custom)"
	if len(lm.SupportedTypes) > 0 {
		typeHint = fmt.Sprintf("(Space/Up/Down: %s, or type custom)", strings.Join(lm.SupportedTypes, ", "))
	}

	targetLabel := "Target:"
	targetHint := "e.g. main.go, ./cmd/tahr, app.py"
	if strings.EqualFold(lm.EditProfile.Type, "shell") {
		targetLabel = "Command:"
		targetHint = "e.g. echo hello, fflow stats -re .go, git status"
	}

	fields := []struct {
		label string
		val   string
		hint  string
	}{
		{"Name:", lm.EditProfile.Name, ""},
		{"Type:", lm.EditProfile.Type, typeHint},
		{"Request:", lm.EditProfile.Request, "(Space/Up/Down: launch, debug)"},
		{targetLabel, lm.EditProfile.Target, targetHint},
		{"Args:", strings.Join(lm.EditProfile.Args, " "), "space separated"},
		{"Cwd:", lm.EditProfile.Cwd, "working directory (optional)"},
		{"Console:", lm.EditProfile.Console, "(Space/Up/Down: integratedTerminal, internalConsole)"},
		{"Pre-Launch:", lm.EditProfile.PreLaunchTask, "task/command before launch"},
	}

	contentTop := startY + 3
	for i, f := range fields {
		rowY := contentTop + (i * 2)
		if rowY >= startY+modalH-3 {
			break
		}
		isCur := (i == lm.EditFieldIdx)

		lblFg := boxFg
		valBg := boxBg
		valFg := boxFg
		if isCur {
			lblFg = accentFg
			valBg = selBg
			valFg = selFg
		}

		// Draw label
		for lx, r := range f.label {
			buf.SetRune(startX+3+lx, rowY, r, lblFg, boxBg, cell.AttrBold)
		}

		// Draw value input box
		valText := f.val
		if isCur && i != 2 && i != 6 {
			valText = lm.InputBuffer + "█"
		}
		valRunes := []rune(valText)

		inputStart := startX + 16
		inputW := modalW - 20
		for vx := 0; vx < inputW; vx++ {
			r := ' '
			if vx < len(valRunes) {
				r = valRunes[vx]
			}
			buf.SetRune(inputStart+vx, rowY, r, valFg, valBg, cell.AttrNone)
		}

		// Hint
		if f.hint != "" && isCur {
			for hx, r := range f.hint {
				if inputStart+hx < startX+modalW-2 {
					buf.SetRune(inputStart+hx, rowY+1, r, commentFg, boxBg, cell.AttrDim)
				}
			}
		}
	}

	// Bottom action buttons
	bottomY := startY + modalH - 2
	saveBtn := " Save Profile (Ctrl+S) "
	cancelBtn := " Cancel (Esc) "
	for i, r := range saveBtn {
		if startX+3+i < startX+modalW-2 {
			buf.SetRune(startX+3+i, bottomY, r, accentFg, boxBg, cell.AttrBold)
		}
	}
	cancelX := startX + 3 + len(saveBtn) + 2
	for i, r := range cancelBtn {
		if cancelX+i < startX+modalW-2 {
			buf.SetRune(cancelX+i, bottomY, r, commentFg, boxBg, cell.AttrNone)
		}
	}
}
