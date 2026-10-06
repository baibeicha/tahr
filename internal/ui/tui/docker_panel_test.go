package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/docker"
	"tahr/internal/ui"
)

func TestDockerPanel_InitAndRender(t *testing.T) {
	theme := ui.CatppuccinMocha()
	panel := NewDockerPanel("")

	// Populate mock services
	panel.Services = []docker.ContainerInfo{
		{
			Service:     "web",
			ContainerID: "a1b2c3d4e5f6",
			State:       "running",
			Status:      "Up 2 hours",
			Ports:       "8080:80/tcp",
			Image:       "nginx:alpine",
		},
		{
			Service:     "db",
			ContainerID: "f6e5d4c3b2a1",
			State:       "exited",
			Status:      "Exited (0)",
			Ports:       "5432:5432/tcp",
			Image:       "postgres:15-alpine",
		},
	}
	panel.Logs = []string{
		"[nginx] 127.0.0.1 - GET / 200",
		"[nginx] 127.0.0.1 - GET /api/health 200",
		"[nginx] ERROR: upstream timed out",
	}

	buf := buffer.NewBuffer(80, 24)
	panel.Render(buf, 0, 0, 80, 24, &theme)

	// Convert first few lines of buffer to text to check formatting
	var lines []string
	for y := 0; y < 24; y++ {
		var sb strings.Builder
		for x := 0; x < 80; x++ {
			c := buf.Cell(x, y)
			if c != nil && c.Rune != 0 {
				sb.WriteRune(c.Rune)
			} else {
				sb.WriteRune(' ')
			}
		}
		lines = append(lines, sb.String())
	}
	renderedText := strings.Join(lines, "\n")

	// 1. Verify buttons do NOT have brackets [ ]
	if strings.Contains(renderedText, "[ Up ]") || strings.Contains(renderedText, "[Up]") {
		t.Fatalf("Constraint violation: detected brackets around 'Up' button!")
	}
	if strings.Contains(renderedText, "[ Down ]") || strings.Contains(renderedText, "[Down]") {
		t.Fatalf("Constraint violation: detected brackets around 'Down' button!")
	}
	if strings.Contains(renderedText, "[ Restart ]") || strings.Contains(renderedText, "[Restart]") {
		t.Fatalf("Constraint violation: detected brackets around 'Restart' button!")
	}
	if strings.Contains(renderedText, "[ Logs ]") || strings.Contains(renderedText, "[Logs]") {
		t.Fatalf("Constraint violation: detected brackets around 'Logs' button!")
	}
	if strings.Contains(renderedText, "[ Exec ]") || strings.Contains(renderedText, "[Exec]") {
		t.Fatalf("Constraint violation: detected brackets around 'Exec' button!")
	}

	// Verify action buttons present with clean spacing
	if !strings.Contains(renderedText, " Up ") || !strings.Contains(renderedText, " Down ") ||
		!strings.Contains(renderedText, " Restart ") || !strings.Contains(renderedText, " Logs ") ||
		!strings.Contains(renderedText, " Exec ") {
		t.Fatalf("Expected clean action buttons in rendered output, got:\n%s", renderedText)
	}

	// 2. Verify services rendered with glyphs
	if !strings.Contains(renderedText, "● running") {
		t.Errorf("Expected '● running' for web service in output, got:\n%s", renderedText)
	}
	if !strings.Contains(renderedText, "○ exited") {
		t.Errorf("Expected '○ exited' for db service in output, got:\n%s", renderedText)
	}

	// 3. Verify lower split logs
	if !strings.Contains(renderedText, "upstream timed out") {
		t.Errorf("Expected log content in lower half of panel, got:\n%s", renderedText)
	}
}

func TestDockerPanel_KeyNavigation(t *testing.T) {
	panel := NewDockerPanel("")
	panel.Services = []docker.ContainerInfo{
		{Service: "srv1", State: "running"},
		{Service: "srv2", State: "running"},
		{Service: "srv3", State: "exited"},
	}

	if panel.SelectedIndex != 0 {
		t.Fatalf("initial SelectedIndex = %d, want 0", panel.SelectedIndex)
	}

	// KeyDown
	panel.HandleKey(input.Key{Type: input.KeyDown})
	if panel.SelectedIndex != 1 {
		t.Fatalf("SelectedIndex after KeyDown = %d, want 1", panel.SelectedIndex)
	}

	// KeyDown again
	panel.HandleKey(input.Key{Type: input.KeyDown})
	if panel.SelectedIndex != 2 {
		t.Fatalf("SelectedIndex after KeyDown = %d, want 2", panel.SelectedIndex)
	}

	// KeyUp
	panel.HandleKey(input.Key{Type: input.KeyUp})
	if panel.SelectedIndex != 1 {
		t.Fatalf("SelectedIndex after KeyUp = %d, want 1", panel.SelectedIndex)
	}
}

func TestDockerPanel_ButtonClicks(t *testing.T) {
	theme := ui.CatppuccinMocha()
	panel := NewDockerPanel("")
	panel.Services = []docker.ContainerInfo{
		{Service: "api", State: "running"},
	}

	buf := buffer.NewBuffer(80, 24)
	panel.Render(buf, 0, 0, 80, 24, &theme)

	var upHit *DockerButtonHit
	for i := range panel.ButtonHits {
		if panel.ButtonHits[i].Action == "up" {
			upHit = &panel.ButtonHits[i]
			break
		}
	}

	if upHit == nil {
		t.Fatalf("no 'up' button hit area registered during render")
	}

	// Click on the 'up' button
	clicked := panel.HandleClick(upHit.X+1, upHit.Y)
	if !clicked {
		t.Fatalf("expected HandleClick on 'up' button to return true")
	}
}

func TestDockerPanel_OfflineGraceful(t *testing.T) {
	theme := ui.CatppuccinMocha()
	panel := NewDockerPanel("")
	panel.Status = docker.DockerStatus{
		BinaryFound:   true,
		DaemonRunning: false,
		Error:         "daemon not responding",
	}

	buf := buffer.NewBuffer(80, 24)
	panel.Render(buf, 0, 0, 80, 24, &theme)

	var sb strings.Builder
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			c := buf.Cell(x, y)
			if c != nil && c.Rune != 0 {
				sb.WriteRune(c.Rune)
			}
		}
		sb.WriteRune('\n')
	}
	output := sb.String()

	if !strings.Contains(output, "Docker daemon is not running") {
		t.Errorf("expected offline banner in output, got:\n%s", output)
	}
}

func TestDockerPanel_DockingModes(t *testing.T) {
	theme := ui.CatppuccinMocha()
	panel := NewDockerPanel("")
	panel.Services = []docker.ContainerInfo{
		{Service: "worker", State: "running", Ports: "9000:9000", Image: "worker:v1"},
	}

	// 1. Right Sidebar Dock (narrow, tall)
	panel.SetPosition("right")
	bufSide := buffer.NewBuffer(35, 30)
	panel.Render(bufSide, 0, 0, 35, 30, &theme)

	// 2. Bottom Panel Dock (wide, short)
	panel.SetPosition("bottom")
	bufBottom := buffer.NewBuffer(120, 10)
	panel.Render(bufBottom, 0, 0, 120, 10, &theme)
}
