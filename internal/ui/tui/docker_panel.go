package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/docker"
	"tahr/internal/ui"
)

// DockerButtonHit records the bounding box of an interactive button.
type DockerButtonHit struct {
	Action string // "up", "down", "restart", "logs", "exec", "refresh"
	X, Y   int
	Width  int
}

// DockerServiceHit records the row bounding box for a service item in the table.
type DockerServiceHit struct {
	Index int
	Y     int
	X, W  int
}

// DockerPanel provides an interactive management UI for Docker Compose services and logs.
// It can be docked in either the Right Sidebar or Bottom Panel.
type DockerPanel struct {
	WorkspaceDir string
	Client       *docker.Client
	Position     string // "right" or "bottom"
	Open         bool
	Height       int // used when docked in bottom panel

	Services      []docker.ContainerInfo
	SelectedIndex int
	TableOffset   int

	// Logs viewer
	Logs         []string
	LogsOffset   int
	LogStreaming bool
	LogCancel    context.CancelFunc
	ActiveLogSvc string
	autoScroll   bool

	// Health and configuration state
	Status      docker.DockerStatus
	HasCompose  bool
	ComposePath string
	LastStatusMsg string
	LastStatusErr bool

	// Interactive hitboxes
	ButtonHits  []DockerButtonHit
	ServiceHits []DockerServiceHit

	// Scroll buttons for logs
	ScrollUpHit   DockerButtonHit
	ScrollDownHit DockerButtonHit

	// Callbacks
	OnExec  func(cmdArgs []string)
	OnToast func(level, title, msg string)

	mu sync.RWMutex
}

// NewDockerPanel initializes a Docker Compose management panel.
func NewDockerPanel(workspaceDir string) *DockerPanel {
	client := docker.NewClient()
	panel := &DockerPanel{
		WorkspaceDir: workspaceDir,
		Client:       client,
		Position:     "right",
		Height:       14,
		autoScroll:   true,
		Services:     make([]docker.ContainerInfo, 0),
		Logs:         make([]string, 0),
	}
	panel.Refresh()
	return panel
}

// Toggle toggles the visibility of the panel.
func (p *DockerPanel) Toggle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Open = !p.Open
	if p.Open {
		go p.Refresh()
	}
}

// SetPosition sets the docking location ("right" or "bottom").
func (p *DockerPanel) SetPosition(pos string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pos == "bottom" || pos == "right" {
		p.Position = pos
	}
}

// Refresh re-checks Docker status and reloads services from the compose project and daemon.
func (p *DockerPanel) Refresh() {
	p.mu.Lock()
	workspaceDir := p.WorkspaceDir
	client := p.Client
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	status := client.CheckStatus(ctx)
	cancel()

	var composePath string
	hasCompose := false
	if workspaceDir != "" {
		if found, err := docker.FindComposeFile(workspaceDir); err == nil {
			hasCompose = true
			composePath = found
		}
	}

	services, err := client.GetServicesWithCompose(workspaceDir)
	if err != nil && len(services) == 0 {
		p.mu.Lock()
		p.Status = status
		p.HasCompose = hasCompose
		p.ComposePath = composePath
		p.LastStatusMsg = err.Error()
		p.LastStatusErr = true
		p.mu.Unlock()
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.Status = status
	p.HasCompose = hasCompose
	p.ComposePath = composePath
	p.Services = services
	if p.SelectedIndex >= len(services) {
		p.SelectedIndex = max(0, len(services)-1)
	}
	p.LastStatusErr = false
	if status.DaemonRunning {
		p.LastStatusMsg = "Daemon ready"
	} else if !status.BinaryFound {
		p.LastStatusMsg = "Docker CLI missing"
		p.LastStatusErr = true
	} else {
		p.LastStatusMsg = "Daemon offline"
		p.LastStatusErr = true
	}
}

// TriggerUp starts the selected service (or all services if none selected).
func (p *DockerPanel) TriggerUp() {
	p.mu.RLock()
	client := p.Client
	workspaceDir := p.WorkspaceDir
	svcName := ""
	if p.SelectedIndex >= 0 && p.SelectedIndex < len(p.Services) {
		svcName = p.Services[p.SelectedIndex].Service
	}
	p.mu.RUnlock()

	p.postToast("info", "DOCKER", fmt.Sprintf("Starting service %s...", svcName))
	go func() {
		err := client.Up(workspaceDir, svcName)
		if err != nil {
			p.postToast("error", "DOCKER", fmt.Sprintf("Up failed: %v", err))
		} else {
			p.postToast("success", "DOCKER", fmt.Sprintf("Service %s started", svcName))
		}
		p.Refresh()
	}()
}

// TriggerDown stops all compose services.
func (p *DockerPanel) TriggerDown() {
	p.mu.RLock()
	client := p.Client
	workspaceDir := p.WorkspaceDir
	p.mu.RUnlock()

	p.postToast("info", "DOCKER", "Stopping compose services...")
	go func() {
		err := client.Down(workspaceDir)
		if err != nil {
			p.postToast("error", "DOCKER", fmt.Sprintf("Down failed: %v", err))
		} else {
			p.postToast("success", "DOCKER", "Services stopped")
		}
		p.Refresh()
	}()
}

// TriggerRestart restarts the selected service.
func (p *DockerPanel) TriggerRestart() {
	p.mu.RLock()
	client := p.Client
	workspaceDir := p.WorkspaceDir
	svcName := ""
	if p.SelectedIndex >= 0 && p.SelectedIndex < len(p.Services) {
		svcName = p.Services[p.SelectedIndex].Service
	}
	p.mu.RUnlock()

	if svcName == "" {
		p.postToast("warn", "DOCKER", "Select a service to restart")
		return
	}

	p.postToast("info", "DOCKER", fmt.Sprintf("Restarting service %s...", svcName))
	go func() {
		err := client.Restart(workspaceDir, svcName)
		if err != nil {
			p.postToast("error", "DOCKER", fmt.Sprintf("Restart failed: %v", err))
		} else {
			p.postToast("success", "DOCKER", fmt.Sprintf("Service %s restarted", svcName))
		}
		p.Refresh()
	}()
}

// TriggerLogs toggles or restarts real-time log streaming for the selected service.
func (p *DockerPanel) TriggerLogs() {
	p.mu.Lock()
	svcName := ""
	if p.SelectedIndex >= 0 && p.SelectedIndex < len(p.Services) {
		svcName = p.Services[p.SelectedIndex].Service
	}

	if p.LogStreaming && p.ActiveLogSvc == svcName {
		// Stop current stream
		if p.LogCancel != nil {
			p.LogCancel()
			p.LogCancel = nil
		}
		p.LogStreaming = false
		p.mu.Unlock()
		p.postToast("info", "DOCKER", fmt.Sprintf("Stopped logs for %s", svcName))
		return
	}

	// Stop previous stream if any
	if p.LogCancel != nil {
		p.LogCancel()
		p.LogCancel = nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.LogCancel = cancel
	p.LogStreaming = true
	p.ActiveLogSvc = svcName
	p.Logs = []string{fmt.Sprintf("── Stream started for %s at %s ──", svcName, time.Now().Format("15:04:05"))}
	p.LogsOffset = 0
	p.autoScroll = true
	client := p.Client
	workspaceDir := p.WorkspaceDir
	p.mu.Unlock()

	p.postToast("info", "DOCKER", fmt.Sprintf("Streaming logs for %s", svcName))
	go func() {
		_ = client.StreamLogs(ctx, workspaceDir, svcName, func(line string) {
			p.mu.Lock()
			p.Logs = append(p.Logs, line)
			if len(p.Logs) > 2000 {
				p.Logs = p.Logs[len(p.Logs)-2000:]
			}
			p.mu.Unlock()
		})
		p.mu.Lock()
		if p.ActiveLogSvc == svcName {
			p.LogStreaming = false
		}
		p.mu.Unlock()
	}()
}

// TriggerExec launches an interactive shell inside the selected container.
func (p *DockerPanel) TriggerExec() {
	p.mu.RLock()
	client := p.Client
	workspaceDir := p.WorkspaceDir
	svcName := ""
	if p.SelectedIndex >= 0 && p.SelectedIndex < len(p.Services) {
		svcName = p.Services[p.SelectedIndex].Service
	}
	onExec := p.OnExec
	p.mu.RUnlock()

	if svcName == "" {
		p.postToast("warn", "DOCKER", "Select a running service to attach shell")
		return
	}

	cmdArgs := client.ExecCommand(workspaceDir, svcName)
	if onExec != nil {
		onExec(cmdArgs)
	} else {
		p.postToast("info", "DOCKER", fmt.Sprintf("Exec command: %s", strings.Join(cmdArgs, " ")))
	}
}

func (p *DockerPanel) postToast(level, title, msg string) {
	p.mu.RLock()
	fn := p.OnToast
	p.mu.RUnlock()
	if fn != nil {
		fn(level, title, msg)
	}
}

// HandleKey handles keyboard navigation and action triggers.
func (p *DockerPanel) HandleKey(k input.Key) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch k.Type {
	case input.KeyUp:
		if p.SelectedIndex > 0 {
			p.SelectedIndex--
			if p.SelectedIndex < p.TableOffset {
				p.TableOffset = p.SelectedIndex
			}
			return true
		}
	case input.KeyDown:
		if p.SelectedIndex < len(p.Services)-1 {
			p.SelectedIndex++
			return true
		}
	case input.KeyPgUp:
		if p.LogsOffset > 5 {
			p.LogsOffset -= 5
		} else {
			p.LogsOffset = 0
		}
		p.autoScroll = false
		return true
	case input.KeyPgDown:
		p.LogsOffset += 5
		return true
	case input.KeyHome:
		p.LogsOffset = 0
		p.autoScroll = false
		return true
	case input.KeyEnd:
		p.autoScroll = true
		return true
	case input.KeyEnter:
		go p.TriggerLogs()
		return true
	case input.KeyRune:
		switch k.Rune {
		case 'u', 'U':
			go p.TriggerUp()
			return true
		case 'd', 'D':
			go p.TriggerDown()
			return true
		case 'r', 'R':
			go p.TriggerRestart()
			return true
		case 'l', 'L':
			go p.TriggerLogs()
			return true
		case 'x', 'X':
			go p.TriggerExec()
			return true
		case 'f', 'F':
			go p.Refresh()
			return true
		}
	case input.KeyF5:
		go p.Refresh()
		return true
	}

	return false
}

// HandleClick processes mouse clicks on buttons, service rows, or scroll controls.
func (p *DockerPanel) HandleClick(x, y int) bool {
	p.mu.Lock()
	btnHits := append([]DockerButtonHit(nil), p.ButtonHits...)
	svcHits := append([]DockerServiceHit(nil), p.ServiceHits...)
	upHit := p.ScrollUpHit
	downHit := p.ScrollDownHit
	p.mu.Unlock()

	// Check action buttons
	for _, hit := range btnHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.Width {
			switch hit.Action {
			case "up":
				p.TriggerUp()
			case "down":
				p.TriggerDown()
			case "restart":
				p.TriggerRestart()
			case "logs":
				p.TriggerLogs()
			case "exec":
				p.TriggerExec()
			case "refresh":
				go p.Refresh()
			}
			return true
		}
	}

	// Check scroll buttons
	if y == upHit.Y && x >= upHit.X && x < upHit.X+upHit.Width {
		p.mu.Lock()
		if p.LogsOffset > 3 {
			p.LogsOffset -= 3
		} else {
			p.LogsOffset = 0
		}
		p.autoScroll = false
		p.mu.Unlock()
		return true
	}
	if y == downHit.Y && x >= downHit.X && x < downHit.X+downHit.Width {
		p.mu.Lock()
		p.LogsOffset += 3
		p.mu.Unlock()
		return true
	}

	// Check service row clicks
	for _, hit := range svcHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.W {
			p.mu.Lock()
			p.SelectedIndex = hit.Index
			p.mu.Unlock()
			return true
		}
	}

	return false
}

// RenderRect renders the Docker panel inside a buffer.Rect.
func (p *DockerPanel) RenderRect(buf *buffer.Buffer, bounds buffer.Rect, theme *ui.Theme) {
	p.Render(buf, bounds.X, bounds.Y, bounds.Width, bounds.Height, theme)
}

// Render draws the complete Docker Compose management panel at (x, y, w, h).
func (p *DockerPanel) Render(buf *buffer.Buffer, x, y, w, h int, theme *ui.Theme) {
	if w <= 0 || h <= 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.ButtonHits = p.ButtonHits[:0]
	p.ServiceHits = p.ServiceHits[:0]

	bg := toColor(theme.Background)
	sidebarBg := toColor(theme.StatusBarBg)
	textFg := toColor(theme.Foreground)
	dimFg := toColor(theme.Comment)
	borderFg := toColor(theme.BorderColor)
	selBg := toColor(theme.PopupSelBg)
	activeFg := toColor(theme.Function)
	errFg := toColor(theme.DiagnosticError)
	warnFg := toColor(theme.DiagnosticWarn)
	greenFg := toColor(theme.String)

	// Fill background
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			buf.SetRune(x+col, y+row, ' ', textFg, sidebarBg, cell.AttrNone)
		}
	}

	curY := y

	// 1. Header Bar: Title + Status Indicator
	title := "Docker Compose"
	statusTag := "● online"
	tagFg := greenFg
	if !p.Status.BinaryFound {
		statusTag = "○ missing"
		tagFg = errFg
	} else if !p.Status.DaemonRunning {
		statusTag = "○ offline"
		tagFg = errFg
	}

	for col := 0; col < w; col++ {
		buf.SetRune(x+col, curY, ' ', textFg, bg, cell.AttrNone)
	}
	drawText(buf, x+1, curY, title, activeFg, bg, cell.AttrBold)
	tagCol := x + w - len([]rune(statusTag)) - 1
	if tagCol > x+len(title)+2 {
		drawText(buf, tagCol, curY, statusTag, tagFg, bg, cell.AttrBold)
	}
	curY++

	// 2. Action Buttons Bar:  Up   Down   Restart   Logs   Exec   Refresh 
	// Strictly NO brackets [ ] around buttons!
	buttons := []struct {
		Label  string
		Action string
		Bg     cell.Color
		Fg     cell.Color
	}{
		{" Up ", "up", toColor(theme.SelectionBg), textFg},
		{" Down ", "down", toColor(theme.SelectionBg), textFg},
		{" Restart ", "restart", toColor(theme.SelectionBg), textFg},
		{" Logs ", "logs", toColor(theme.SelectionBg), textFg},
		{" Exec ", "exec", toColor(theme.SelectionBg), textFg},
		{" Refresh ", "refresh", toColor(theme.CursorLineBg), dimFg},
	}

	btnCol := x + 1
	for _, b := range buttons {
		bLen := len([]rune(b.Label))
		if btnCol+bLen <= x+w-1 {
			p.ButtonHits = append(p.ButtonHits, DockerButtonHit{
				Action: b.Action,
				X:      btnCol,
				Y:      curY,
				Width:  bLen,
			})
			btnBg := b.Bg
			if b.Action == "logs" && p.LogStreaming {
				btnBg = toColor(theme.PopupSelBg)
			}
			drawText(buf, btnCol, curY, b.Label, b.Fg, btnBg, cell.AttrBold)
			btnCol += bLen + 1
		}
	}
	curY++

	// 3. Graceful offline/missing banner if needed
	if !p.Status.BinaryFound || !p.Status.DaemonRunning || (!p.HasCompose && len(p.Services) == 0) {
		bannerText := ""
		bannerSub := ""
		if !p.Status.BinaryFound {
			bannerText = "● Docker CLI not found on PATH"
			bannerSub = "Install Docker Desktop or Docker Engine to use Compose"
		} else if !p.Status.DaemonRunning {
			bannerText = "● Docker daemon is not running"
			bannerSub = "Start Docker Desktop or Engine service to manage containers"
		} else {
			bannerText = "● No docker-compose.yml found"
			bannerSub = "Add compose.yaml to active workspace to manage services"
		}

		if curY < y+h-4 {
			drawText(buf, x+1, curY, bannerText, errFg, sidebarBg, cell.AttrBold)
			curY++
			if w > 30 {
				drawText(buf, x+3, curY, bannerSub, dimFg, sidebarBg, cell.AttrNone)
				curY++
			}
		}
	}

	// Divider below action/banner
	for col := 0; col < w; col++ {
		buf.SetRune(x+col, curY, '─', borderFg, sidebarBg, cell.AttrNone)
	}
	curY++

	// 4. Split Heights calculation
	remainingH := (y + h) - curY
	if remainingH < 4 {
		return
	}

	tableH := remainingH / 2
	if tableH < 3 {
		tableH = 3
	}
	if tableH > remainingH-3 {
		tableH = remainingH - 3
	}

	// 5. Service Table Header
	headerY := curY
	nameW := 14
	statusW := 12
	portsW := 14
	if w < 45 {
		nameW = 10
		statusW = 11
		portsW = 8
	} else if w > 70 {
		nameW = 18
		statusW = 14
		portsW = 20
	}

	drawText(buf, x+1, headerY, "NAME", dimFg, sidebarBg, cell.AttrBold)
	drawText(buf, x+1+nameW, headerY, "STATUS", dimFg, sidebarBg, cell.AttrBold)
	drawText(buf, x+1+nameW+statusW, headerY, "PORTS", dimFg, sidebarBg, cell.AttrBold)
	if x+1+nameW+statusW+portsW < x+w-4 {
		drawText(buf, x+1+nameW+statusW+portsW, headerY, "IMAGE", dimFg, sidebarBg, cell.AttrBold)
	}
	curY++

	// Service Rows
	tableRows := tableH - 1
	if p.SelectedIndex >= p.TableOffset+tableRows {
		p.TableOffset = p.SelectedIndex - tableRows + 1
	}
	if p.SelectedIndex < p.TableOffset {
		p.TableOffset = p.SelectedIndex
	}

	for r := 0; r < tableRows; r++ {
		rowY := curY + r
		sIdx := p.TableOffset + r

		if sIdx >= len(p.Services) {
			if len(p.Services) == 0 && r == 0 {
				drawText(buf, x+2, rowY, "No services configured", dimFg, sidebarBg, cell.AttrNone)
			}
			continue
		}

		svc := p.Services[sIdx]
		isSel := sIdx == p.SelectedIndex
		rowBg := sidebarBg
		if isSel {
			rowBg = selBg
			for col := 0; col < w; col++ {
				buf.SetRune(x+col, rowY, ' ', textFg, rowBg, cell.AttrNone)
			}
		}

		p.ServiceHits = append(p.ServiceHits, DockerServiceHit{
			Index: sIdx,
			Y:     rowY,
			X:     x,
			W:     w,
		})

		// Selection cursor
		prefix := "  "
		if isSel {
			prefix = "▶ "
		}
		drawText(buf, x, rowY, prefix+truncate(svc.Service, nameW-2), textFg, rowBg, cell.AttrBold)

		// Status glyph + state
		stGlyph := "○"
		stText := svc.State
		stColor := dimFg
		switch strings.ToLower(svc.State) {
		case "running":
			stGlyph = "●"
			stColor = greenFg
		case "exited", "stopped", "dead":
			stGlyph = "○"
			stColor = errFg
		case "created", "restarting":
			stGlyph = "◐"
			stColor = warnFg
		}

		drawText(buf, x+1+nameW, rowY, fmt.Sprintf("%s %s", stGlyph, truncate(stText, statusW-3)), stColor, rowBg, cell.AttrNone)

		// Ports
		drawText(buf, x+1+nameW+statusW, rowY, truncate(svc.Ports, portsW-1), activeFg, rowBg, cell.AttrNone)

		// Image
		if x+1+nameW+statusW+portsW < x+w-2 {
			imgW := (x + w) - (x + 1 + nameW + statusW + portsW) - 1
			drawText(buf, x+1+nameW+statusW+portsW, rowY, truncate(svc.Image, imgW), dimFg, rowBg, cell.AttrNone)
		}
	}
	curY += tableRows

	// 6. Split Logs Divider
	divY := curY
	logHeader := "Logs"
	if p.ActiveLogSvc != "" {
		if p.LogStreaming {
			logHeader = fmt.Sprintf("Logs: %s (live)", p.ActiveLogSvc)
		} else {
			logHeader = fmt.Sprintf("Logs: %s", p.ActiveLogSvc)
		}
	}

	for col := 0; col < w; col++ {
		buf.SetRune(x+col, divY, '─', borderFg, sidebarBg, cell.AttrNone)
	}
	drawText(buf, x+2, divY, fmt.Sprintf(" %s ", logHeader), activeFg, sidebarBg, cell.AttrBold)

	// Scroll buttons ▲ and ▼
	scrollText := " ▲ ▼ "
	sX := x + w - len(scrollText) - 1
	if sX > x+len(logHeader)+6 {
		drawText(buf, sX, divY, scrollText, dimFg, sidebarBg, cell.AttrNone)
		p.ScrollUpHit = DockerButtonHit{Action: "scroll_up", X: sX + 1, Y: divY, Width: 1}
		p.ScrollDownHit = DockerButtonHit{Action: "scroll_down", X: sX + 3, Y: divY, Width: 1}
	}
	curY++

	// 7. Live Logs Viewport (Lower Half)
	logAreaH := (y + h) - curY
	if logAreaH <= 0 {
		return
	}

	if len(p.Logs) == 0 {
		msg := "Select a service and click Logs to stream output."
		if !p.Status.DaemonRunning {
			msg = "Docker daemon is offline. Logs unavailable."
		}
		drawText(buf, x+2, curY, msg, dimFg, sidebarBg, cell.AttrNone)
		return
	}

	if p.autoScroll {
		p.LogsOffset = max(0, len(p.Logs)-logAreaH)
	}
	if p.LogsOffset > len(p.Logs)-1 {
		p.LogsOffset = max(0, len(p.Logs)-1)
	}

	for r := 0; r < logAreaH; r++ {
		lIdx := p.LogsOffset + r
		if lIdx >= len(p.Logs) {
			break
		}
		lineY := curY + r
		lineText := p.Logs[lIdx]

		lFg := textFg
		if strings.Contains(strings.ToUpper(lineText), "ERROR") || strings.Contains(strings.ToUpper(lineText), "FATAL") {
			lFg = errFg
		} else if strings.Contains(strings.ToUpper(lineText), "WARN") {
			lFg = warnFg
		}

		drawText(buf, x+1, lineY, truncate(lineText, w-2), lFg, sidebarBg, cell.AttrNone)
	}
}

// drawText renders a UTF-8 string onto the goatui buffer starting at (x, y).
func drawText(buf *buffer.Buffer, x, y int, str string, fg, bg cell.Color, attr cell.Modifier) {
	curX := x
	for _, r := range str {
		buf.SetRune(curX, y, r, fg, bg, attr)
		curX++
	}
}

// truncate limits a string to maxLen runes.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}
