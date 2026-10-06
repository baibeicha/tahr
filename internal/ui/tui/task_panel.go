package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/taskrunner"
	"tahr/internal/ui"
)

// TaskPanelItemType indicates whether a line in the list is a section header or a task.
type TaskPanelItemType int

const (
	ItemHeader TaskPanelItemType = iota
	ItemTask
)

// TaskPanelItem represents a row in the flattened task tree view.
type TaskPanelItem struct {
	Type       TaskPanelItemType
	GroupIndex int
	TaskIndex  int
	GroupTitle string
	Task       *taskrunner.Task
}

// btnHitbox stores rectangular coordinates for interactive buttons.
type btnHitbox struct {
	minX int
	maxX int
	y    int
}

func (b btnHitbox) Contains(x, y int) bool {
	return y == b.y && x >= b.minX && x <= b.maxX
}

// TaskPanel is a tool window displaying discovered workspace tasks grouped by source file.
type TaskPanel struct {
	Open          bool
	Height        int
	WorkspaceDir  string
	Groups        []taskrunner.TaskGroup
	items         []TaskPanelItem
	SelectedIndex int
	ScrollOffset  int

	// Execution state
	IsRunning     bool
	RunningTask   *taskrunner.Task
	StatusMessage string

	// Callbacks
	OnRunTask  func(task taskrunner.Task)
	OnStopTask func()
	OnRefresh  func()
	OnClose    func()

	// Collapsed state map: group SourceFile -> collapsed
	collapsed map[string]bool

	// Button hitboxes
	btnRunHitbox     btnHitbox
	btnRefreshHitbox btnHitbox
	btnStopHitbox    btnHitbox
	btnCloseHitbox   btnHitbox

	// Content area layout cache
	lastX int
	lastY int
	lastW int
	lastH int
}

// NewTaskPanel initializes a clean TaskPanel instance.
func NewTaskPanel() *TaskPanel {
	return &TaskPanel{
		Open:          true,
		Height:        10,
		Groups:        make([]taskrunner.TaskGroup, 0),
		items:         make([]TaskPanelItem, 0),
		collapsed:     make(map[string]bool),
		StatusMessage: "Ready",
	}
}

// Toggle toggles the task panel open or closed.
func (p *TaskPanel) Toggle() {
	p.Open = !p.Open
}

// SetWorkspace updates the workspace directory and refreshes tasks.
func (p *TaskPanel) SetWorkspace(dir string) {
	p.WorkspaceDir = dir
	p.Refresh(dir)
}

// SetGroups populates the panel with task groups and rebuilds the flat item list.
func (p *TaskPanel) SetGroups(groups []taskrunner.TaskGroup) {
	p.Groups = groups
	p.rebuildItems()
}

// rebuildItems creates the flat display list based on active groups and collapsed states.
func (p *TaskPanel) rebuildItems() {
	var newItems []TaskPanelItem

	for gIdx, group := range p.Groups {
		// Group header
		headerTitle := group.Title()
		newItems = append(newItems, TaskPanelItem{
			Type:       ItemHeader,
			GroupIndex: gIdx,
			TaskIndex:  -1,
			GroupTitle: headerTitle,
		})

		// If collapsed, skip children
		if p.collapsed[group.SourceFile] {
			continue
		}

		// Group task rows
		for tIdx := range group.Tasks {
			task := &group.Tasks[tIdx]
			newItems = append(newItems, TaskPanelItem{
				Type:       ItemTask,
				GroupIndex: gIdx,
				TaskIndex:  tIdx,
				GroupTitle: headerTitle,
				Task:       task,
			})
		}
	}

	p.items = newItems
	if p.SelectedIndex >= len(p.items) {
		p.SelectedIndex = max(0, len(p.items)-1)
	}
	if p.ScrollOffset > p.SelectedIndex {
		p.ScrollOffset = p.SelectedIndex
	}
}

// Refresh re-scans the workspace directory for task files and updates the panel.
func (p *TaskPanel) Refresh(workspaceDir string) {
	if workspaceDir != "" {
		p.WorkspaceDir = workspaceDir
	}

	groups, err := taskrunner.DiscoverTasks(p.WorkspaceDir)
	if err != nil {
		p.StatusMessage = fmt.Sprintf("Error discovering tasks: %v", err)
		return
	}

	p.SetGroups(groups)

	totalTasks := 0
	for _, g := range groups {
		totalTasks += len(g.Tasks)
	}

	if totalTasks == 0 {
		p.StatusMessage = "No tasks found"
	} else {
		taskUnit := "tasks"
		if totalTasks == 1 {
			taskUnit = "task"
		}
		fileUnit := "files"
		if len(groups) == 1 {
			fileUnit = "file"
		}
		p.StatusMessage = fmt.Sprintf("Found %d %s across %d %s", totalTasks, taskUnit, len(groups), fileUnit)
	}

	if p.OnRefresh != nil {
		p.OnRefresh()
	}
}

// SelectedTask returns the currently selected task, or nil if a header is selected.
func (p *TaskPanel) SelectedTask() *taskrunner.Task {
	if p.SelectedIndex >= 0 && p.SelectedIndex < len(p.items) {
		item := p.items[p.SelectedIndex]
		if item.Type == ItemTask {
			return item.Task
		}
	}
	return nil
}

// RunSelected triggers execution of the currently selected task.
// If a header is selected, it toggles collapse of that group instead.
func (p *TaskPanel) RunSelected() {
	if p.SelectedIndex < 0 || p.SelectedIndex >= len(p.items) {
		return
	}

	item := p.items[p.SelectedIndex]
	if item.Type == ItemHeader {
		p.ToggleCollapse(item.GroupIndex)
		return
	}

	if item.Task != nil {
		p.IsRunning = true
		p.RunningTask = item.Task
		p.StatusMessage = fmt.Sprintf("Running %s (%s)...", item.Task.Name, item.Task.Command)
		if p.OnRunTask != nil {
			p.OnRunTask(*item.Task)
		}
	}
}

// StopRunning stops the currently running task.
func (p *TaskPanel) StopRunning() {
	if p.IsRunning {
		p.IsRunning = false
		taskName := "task"
		if p.RunningTask != nil {
			taskName = p.RunningTask.Name
		}
		p.RunningTask = nil
		p.StatusMessage = fmt.Sprintf("Stopped %s", taskName)
		if p.OnStopTask != nil {
			p.OnStopTask()
		}
	}
}

// SetTaskCompleted marks the active task as finished.
func (p *TaskPanel) SetTaskCompleted(success bool, exitCode int) {
	p.IsRunning = false
	taskName := "task"
	if p.RunningTask != nil {
		taskName = p.RunningTask.Name
	}
	p.RunningTask = nil
	if success {
		p.StatusMessage = fmt.Sprintf("%s finished successfully", taskName)
	} else {
		p.StatusMessage = fmt.Sprintf("%s failed (exit %d)", taskName, exitCode)
	}
}

// ToggleCollapse toggles the expansion state of a task group by index.
func (p *TaskPanel) ToggleCollapse(groupIdx int) {
	if groupIdx >= 0 && groupIdx < len(p.Groups) {
		key := p.Groups[groupIdx].SourceFile
		p.collapsed[key] = !p.collapsed[key]
		p.rebuildItems()
	}
}

// HandleKey handles navigation and shortcut keys inside the TaskPanel.
func (p *TaskPanel) HandleKey(k input.Key) bool {
	if !p.Open {
		return false
	}

	if k.Type == input.KeyEsc {
		p.Open = false
		if p.OnClose != nil {
			p.OnClose()
		}
		return true
	}

	switch k.Type {
	case input.KeyUp:
		if p.SelectedIndex > 0 {
			p.SelectedIndex--
			if p.SelectedIndex < p.ScrollOffset {
				p.ScrollOffset = p.SelectedIndex
			}
		}
		return true

	case input.KeyDown:
		if p.SelectedIndex < len(p.items)-1 {
			p.SelectedIndex++
		}
		return true

	case input.KeyHome:
		p.SelectedIndex = 0
		p.ScrollOffset = 0
		return true

	case input.KeyEnd:
		p.SelectedIndex = max(0, len(p.items)-1)
		return true

	case input.KeyPgUp:
		p.SelectedIndex = max(0, p.SelectedIndex-8)
		p.ScrollOffset = max(0, p.ScrollOffset-8)
		return true

	case input.KeyPgDown:
		p.SelectedIndex = min(len(p.items)-1, p.SelectedIndex+8)
		return true

	case input.KeyEnter, input.KeySpace:
		p.RunSelected()
		return true
	}

	// Shortcuts by rune
	switch k.Rune {
	case 'r', 'R':
		p.Refresh(p.WorkspaceDir)
		return true
	case 's', 'S':
		p.StopRunning()
		return true
	case 'j':
		if p.SelectedIndex < len(p.items)-1 {
			p.SelectedIndex++
		}
		return true
	case 'k':
		if p.SelectedIndex > 0 {
			p.SelectedIndex--
			if p.SelectedIndex < p.ScrollOffset {
				p.ScrollOffset = p.SelectedIndex
			}
		}
		return true
	}

	return false
}

// HandleMouse handles mouse clicks and wheel scrolling for the TaskPanel.
func (p *TaskPanel) HandleMouse(x, y int, action input.MouseAction, btn input.MouseButton) bool {
	if !p.Open {
		return false
	}

	// Check if click falls within panel bounds
	if x < p.lastX || x >= p.lastX+p.lastW || y < p.lastY || y >= p.lastY+p.lastH {
		return false
	}

	// Mouse Wheel
	if action == input.MouseMotion {
		return false
	}

	if btn == input.MouseWheelUp {
		if p.ScrollOffset > 0 {
			p.ScrollOffset = max(0, p.ScrollOffset-2)
			return true
		}
	} else if btn == input.MouseWheelDown {
		maxScroll := max(0, len(p.items)-1)
		if p.ScrollOffset < maxScroll {
			p.ScrollOffset = min(maxScroll, p.ScrollOffset+2)
			return true
		}
	}

	if action != input.MousePress || btn != input.MouseLeft {
		return false
	}

	// 1. Check Button Hitboxes
	if p.btnRunHitbox.Contains(x, y) {
		p.RunSelected()
		return true
	}
	if p.btnRefreshHitbox.Contains(x, y) {
		p.Refresh(p.WorkspaceDir)
		return true
	}
	if p.btnStopHitbox.Contains(x, y) {
		p.StopRunning()
		return true
	}
	if p.btnCloseHitbox.Contains(x, y) {
		p.Open = false
		if p.OnClose != nil {
			p.OnClose()
		}
		return true
	}

	// 2. Check Item Rows
	listStartY := p.lastY + 1
	if y >= listStartY && y < p.lastY+p.lastH-1 {
		rowIdx := y - listStartY + p.ScrollOffset
		if rowIdx >= 0 && rowIdx < len(p.items) {
			if p.SelectedIndex == rowIdx {
				// Double-click / re-click activates item
				p.RunSelected()
			} else {
				p.SelectedIndex = rowIdx
			}
			return true
		}
	}

	return true
}

// RenderBounds renders the TaskPanel inside a bounded rectangle.
func (p *TaskPanel) RenderBounds(buf *buffer.Buffer, bounds buffer.Rect, theme ui.Theme) {
	p.Render(buf, bounds.X, bounds.Y, bounds.Width, bounds.Height, theme)
}

// Render draws the clean TaskPanel tool window at (x, y, w, h).
// Buttons are rendered cleanly without brackets: ' Run ', ' Refresh ', ' Stop '.
func (p *TaskPanel) Render(buf *buffer.Buffer, x, y, w, h int, theme ui.Theme) {
	if !p.Open || h < 3 || w < 24 {
		return
	}

	p.lastX = x
	p.lastY = y
	p.lastW = w
	p.lastH = h

	bg := cell.Color{Type: cell.ColorRGB, Value: theme.StatusBarBg}
	borderFg := cell.Color{Type: cell.ColorRGB, Value: theme.BorderColor}
	textFg := cell.Color{Type: cell.ColorRGB, Value: theme.Foreground}
	dimFg := cell.Color{Type: cell.ColorRGB, Value: theme.Comment}
	selBg := cell.Color{Type: cell.ColorRGB, Value: theme.SelectionBg}
	btnBg := cell.Color{Type: cell.ColorRGB, Value: theme.CursorLineBg}

	runBtnFg := cell.Color{Type: cell.ColorRGB, Value: theme.Function}
	refreshBtnFg := textFg
	stopBtnFg := cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticError}
	if p.IsRunning {
		stopBtnFg = cell.Color{Type: cell.ColorRGB, Value: 0xFF5555}
	}

	// ──────────────────────────────────────────
	// 1. Header & Toolbar Bar (y)
	// ──────────────────────────────────────────
	// Clean button labels without any brackets
	runLabel := " Run "
	refreshLabel := " Refresh "
	stopLabel := " Stop "
	closeLabel := " ✕ "

	runRunes := []rune(runLabel)
	refreshRunes := []rune(refreshLabel)
	stopRunes := []rune(stopLabel)
	closeRunes := []rune(closeLabel)

	// Draw base background and top border line
	for col := 0; col < w; col++ {
		screenX := x + col
		buf.SetRune(screenX, y, '─', borderFg, bg, cell.AttrNone)
	}

	// Layout buttons on toolbar line
	btnX := x + 1

	// Run button
	runStartX := btnX
	if runStartX+len(runRunes) < x+w-5 {
		for i, r := range runRunes {
			buf.SetRune(runStartX+i, y, r, runBtnFg, btnBg, cell.AttrBold)
		}
		p.btnRunHitbox = btnHitbox{minX: runStartX, maxX: runStartX + len(runRunes) - 1, y: y}
		btnX += len(runRunes) + 1
	}

	// Refresh button
	refreshStartX := btnX
	if refreshStartX+len(refreshRunes) < x+w-5 {
		for i, r := range refreshRunes {
			buf.SetRune(refreshStartX+i, y, r, refreshBtnFg, btnBg, cell.AttrNone)
		}
		p.btnRefreshHitbox = btnHitbox{minX: refreshStartX, maxX: refreshStartX + len(refreshRunes) - 1, y: y}
		btnX += len(refreshRunes) + 1
	}

	// Stop button
	stopStartX := btnX
	if stopStartX+len(stopRunes) < x+w-5 {
		for i, r := range stopRunes {
			buf.SetRune(stopStartX+i, y, r, stopBtnFg, btnBg, cell.AttrBold)
		}
		p.btnStopHitbox = btnHitbox{minX: stopStartX, maxX: stopStartX + len(stopRunes) - 1, y: y}
		btnX += len(stopRunes) + 1
	}

	// Close button on far right edge
	closeStartX := x + w - len(closeRunes) - 1
	if closeStartX > btnX {
		for i, r := range closeRunes {
			buf.SetRune(closeStartX+i, y, r, stopBtnFg, bg, cell.AttrBold)
		}
		p.btnCloseHitbox = btnHitbox{minX: closeStartX, maxX: closeStartX + len(closeRunes) - 1, y: y}
	}

	// ──────────────────────────────────────────
	// 2. Task List Body (y+1 to y+h-2)
	// ──────────────────────────────────────────
	contentH := h - 2
	if p.SelectedIndex >= p.ScrollOffset+contentH {
		p.ScrollOffset = p.SelectedIndex - contentH + 1
	}
	if p.SelectedIndex < p.ScrollOffset {
		p.ScrollOffset = p.SelectedIndex
	}

	for row := 0; row < contentH; row++ {
		screenY := y + 1 + row
		idx := p.ScrollOffset + row

		// Fill background
		for col := 0; col < w; col++ {
			buf.SetRune(x+col, screenY, ' ', textFg, bg, cell.AttrNone)
		}

		if idx >= len(p.items) {
			if len(p.items) == 0 {
				emptyLines := []string{
					"Задачи не найдены",
					"───────────────────────────────",
					"Поддерживаемые форматы файлов:",
					"• Makefile (цели make)",
					"• package.json (npm/yarn/pnpm)",
					"• Taskfile.yml (go-task)",
					"• justfile (just)",
					"",
					"Инструкция:",
					"• Нажмите ' Refresh ' для сканирования",
					"• Нажмите ' Run ' для запуска задачи",
				}
				if row < len(emptyLines) {
					el := emptyLines[row]
					c := dimFg
					attr := cell.AttrNone
					if row == 0 {
						c = cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticWarn}
						attr = cell.AttrBold
					} else if strings.HasPrefix(el, "Поддерживаемые") || strings.HasPrefix(el, "Инструкция") {
						c = runBtnFg
					} else if strings.HasPrefix(el, "•") {
						c = textFg
					}
					for ci, cr := range []rune(el) {
						if x+2+ci < x+w-2 {
							buf.SetRune(x+2+ci, screenY, cr, c, bg, attr)
						}
					}
				}
			}
			continue
		}

		item := p.items[idx]
		isSel := idx == p.SelectedIndex
		rowBg := bg
		if isSel {
			rowBg = selBg
		}

		// Apply selection row background
		if isSel {
			for col := 0; col < w; col++ {
				buf.SetRune(x+col, screenY, ' ', textFg, rowBg, cell.AttrNone)
			}
		}

		if item.Type == ItemHeader {
			// Group section header: e.g. "▾ Makefile (3 tasks)" or "▸ package.json (4 tasks)"
			collapseIcon := '▾'
			if p.collapsed[p.Groups[item.GroupIndex].SourceFile] {
				collapseIcon = '▸'
			}

			headerText := fmt.Sprintf(" %c %s", collapseIcon, item.GroupTitle)
			for ci, cr := range []rune(headerText) {
				if x+ci < x+w-1 {
					buf.SetRune(x+ci, screenY, cr, textFg, rowBg, cell.AttrBold)
				}
			}
		} else if item.Task != nil {
			// Task row: indented 2 spaces
			task := item.Task
			isThisRunning := p.IsRunning && p.RunningTask != nil && p.RunningTask.ID == task.ID

			prefix := "  "
			nameFg := textFg
			attr := cell.AttrNone
			if isSel {
				attr = cell.AttrBold
			}
			if isThisRunning {
				prefix = "  ● "
				nameFg = cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticInfo}
			}

			// Render prefix and Task Name
			namePart := prefix + task.Name
			col := 0
			for _, cr := range []rune(namePart) {
				if x+col < x+w-1 {
					buf.SetRune(x+col, screenY, cr, nameFg, rowBg, attr)
					col++
				}
			}

			// Padding to description
			targetDescCol := 24
			for col < targetDescCol && col < w-1 {
				buf.SetRune(x+col, screenY, ' ', textFg, rowBg, cell.AttrNone)
				col++
			}

			// Description or Command text
			descText := task.Description
			if descText == "" {
				descText = task.Command
			}
			if isThisRunning {
				descText = "● Running: " + descText
			}

			for _, cr := range []rune(descText) {
				if x+col < x+w-1 {
					buf.SetRune(x+col, screenY, cr, dimFg, rowBg, cell.AttrNone)
					col++
				}
			}
		}
	}

	// ──────────────────────────────────────────
	// 3. Status Bar Line (y+h-1)
	// ──────────────────────────────────────────
	statusY := y + h - 1
	for col := 0; col < w; col++ {
		buf.SetRune(x+col, statusY, ' ', textFg, bg, cell.AttrNone)
	}

	statusText := p.StatusMessage
	if statusText == "" {
		statusText = "Ready"
	}

	statusFg := dimFg
	if p.IsRunning {
		statusFg = cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticInfo}
	}

	statusPrefix := " " + statusText
	for ci, cr := range []rune(statusPrefix) {
		if x+ci < x+w-1 {
			buf.SetRune(x+ci, statusY, cr, statusFg, bg, cell.AttrNone)
		}
	}
}
