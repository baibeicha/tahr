package tui

import (
	"fmt"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

// SplashScreenState manages the startup animation, logo rendering, and progress tracking.
type SplashScreenState struct {
	Active     bool
	Done       bool
	Progress   float64 // 0.0 to 1.0
	StatusText string
	StartTime  time.Time
	MinDisplay time.Duration
}

// NewSplashScreenState initializes a new startup splash screen.
func NewSplashScreenState() *SplashScreenState {
	return &SplashScreenState{
		Active:     true,
		Done:       false,
		Progress:   0.05,
		StatusText: i18n.T("splash.starting"),
		StartTime:  time.Now(),
		MinDisplay: 1100 * time.Millisecond,
	}
}

// Tick advances the startup animation smoothly based on elapsed time.
func (s *SplashScreenState) Tick() {
	if !s.Active || s.Done {
		return
	}

	elapsed := time.Since(s.StartTime)
	ms := elapsed.Milliseconds()

	switch {
	case ms < 150:
		s.Progress = 0.15
		s.StatusText = i18n.T("splash.scanning")
	case ms < 350:
		s.Progress = 0.38
		s.StatusText = i18n.T("splash.loading_plugins")
	case ms < 600:
		s.Progress = 0.65
		s.StatusText = i18n.T("splash.init_syntax")
	case ms < 900:
		s.Progress = 0.88
		s.StatusText = i18n.T("splash.connecting_lsp")
	case ms < 1100:
		s.Progress = 1.00
		s.StatusText = i18n.T("splash.ready")
	default:
		s.Progress = 1.00
		s.StatusText = i18n.T("splash.ready")
		s.Done = true
		s.Active = false
	}
}

// Skip immediately terminates the splash screen.
func (s *SplashScreenState) Skip() {
	s.Progress = 1.0
	s.Done = true
	s.Active = false
}

// HandleKey skips the splash screen on any key press.
func (s *SplashScreenState) HandleKey(key input.Key) bool {
	if !s.Active {
		return false
	}
	s.Skip()
	return true
}

// HandleMouse skips the splash screen on any mouse click.
func (s *SplashScreenState) HandleMouse(m input.Mouse) bool {
	if !s.Active {
		return false
	}
	if m.Button == input.MouseLeft || m.Button == input.MouseRight {
		s.Skip()
		return true
	}
	return true
}

var mascotArt = []string{
	`      /|             |\      `,
	`     / \___       ___/ \     `,
	`    /   /  \_____/  \   \    `,
	`   (   (    /   \    )   )   `,
	`    \   \  ( o o )  /   /    `,
	`     \__ \  \ ^ /  / __/     `,
	`        \ \  \=/  / /        `,
	`         ` + "`-------'         ",
}

var tahrLogo = []string{
	`████████╗  █████╗  ██╗  ██╗ ██████╗ `,
	`╚══██╔══╝ ██╔══██╗ ██║  ██║ ██╔══██╗`,
	`   ██║    ███████║ ███████║ ██████╔╝`,
	`   ██║    ██╔══██║ ██║  ██║ ██╔══██╗`,
	`   ██║    ██║  ██║ ██║  ██║ ██║  ██║`,
	`   ╚═╝    ╚═╝  ╚═╝ ╚═╝  ╚═╝ ╚═╝  ╚═╝`,
}

// Render draws the centered splash screen onto the GoatUI buffer.
func (s *SplashScreenState) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !s.Active {
		return
	}

	bg := toColor(theme.Background)
	mascotCol := toColor(theme.Function)
	logoCol := toColor(theme.Keyword)
	subCol := toColor(theme.Type)
	barFillCol := toColor(theme.String)
	barEmptyCol := toColor(theme.BorderColor)
	statusCol := toColor(theme.Foreground)
	hintCol := toColor(theme.Comment)

	// Clear full screen with background
	for y := 0; y < screenH; y++ {
		for x := 0; x < screenW; x++ {
			buf.SetRune(x, y, ' ', statusCol, bg, cell.AttrNone)
		}
	}

	// Calculate layout height
	totalContentH := len(mascotArt) + 1 + len(tahrLogo) + 1 + 1 + 2 + 1 + 1 + 1
	startY := (screenH - totalContentH) / 2
	if startY < 1 {
		startY = 1
	}

	curY := startY

	// 1. Draw Mascot Art
	for _, line := range mascotArt {
		if curY < screenH {
			runes := []rune(line)
			startX := (screenW - len(runes)) / 2
			if startX < 0 {
				startX = 0
			}
			for i, r := range runes {
				if startX+i < screenW {
					buf.SetRune(startX+i, curY, r, mascotCol, bg, cell.AttrBold)
				}
			}
		}
		curY++
	}

	curY++ // blank line

	// 2. Draw TAHR Block Typography
	for _, line := range tahrLogo {
		if curY < screenH {
			runes := []rune(line)
			startX := (screenW - len(runes)) / 2
			if startX < 0 {
				startX = 0
			}
			for i, r := range runes {
				if startX+i < screenW {
					buf.SetRune(startX+i, curY, r, logoCol, bg, cell.AttrBold)
				}
			}
		}
		curY++
	}

	// 3. Tagline / Version
	if curY < screenH {
		tagline := i18n.T("splash.tagline")
		runes := []rune(tagline)
		startX := (screenW - len(runes)) / 2
		if startX < 0 {
			startX = 0
		}
		for i, r := range runes {
			if startX+i < screenW {
				buf.SetRune(startX+i, curY, r, subCol, bg, cell.AttrNone)
			}
		}
	}
	curY += 2 // spacing

	// 4. Progress Bar
	barWidth := 34
	if barWidth > screenW-16 {
		barWidth = screenW - 16
		if barWidth < 10 {
			barWidth = 10
		}
	}

	pct := s.Progress
	if pct < 0 {
		pct = 0
	}
	if pct > 1.0 {
		pct = 1.0
	}
	fillCount := int(pct * float64(barWidth))
	if fillCount > barWidth {
		fillCount = barWidth
	}

	pctText := fmt.Sprintf("%3d%%", int(pct*100))
	totalBarLen := barWidth + 1 + len(pctText)
	barStartX := (screenW - totalBarLen) / 2
	if barStartX < 0 {
		barStartX = 0
	}

	if curY < screenH {
		// Draw filled blocks
		for i := 0; i < fillCount; i++ {
			if barStartX+i < screenW {
				buf.SetRune(barStartX+i, curY, '█', barFillCol, bg, cell.AttrBold)
			}
		}
		// Draw empty blocks
		for i := fillCount; i < barWidth; i++ {
			if barStartX+i < screenW {
				buf.SetRune(barStartX+i, curY, '░', barEmptyCol, bg, cell.AttrNone)
			}
		}
		// Percentage text
		for i, r := range pctText {
			xPos := barStartX + barWidth + 1 + i
			if xPos < screenW {
				buf.SetRune(xPos, curY, r, statusCol, bg, cell.AttrBold)
			}
		}
	}
	curY++

	// 5. Dynamic Status Text
	if curY < screenH {
		statusRunes := []rune(s.StatusText)
		statusStartX := (screenW - len(statusRunes)) / 2
		if statusStartX < 0 {
			statusStartX = 0
		}
		for i, r := range statusRunes {
			if statusStartX+i < screenW {
				buf.SetRune(statusStartX+i, curY, r, statusCol, bg, cell.AttrNone)
			}
		}
	}
	curY += 2

	// 6. Skip Hint
	if curY < screenH {
		hint := i18n.T("splash.skip_hint")
		hintRunes := []rune(hint)
		hintStartX := (screenW - len(hintRunes)) / 2
		if hintStartX < 0 {
			hintStartX = 0
		}
		for i, r := range hintRunes {
			if hintStartX+i < screenW {
				buf.SetRune(hintStartX+i, curY, r, hintCol, bg, cell.AttrNone)
			}
		}
	}
}
