package gui

import (
	"math"
	"tahr/internal/ui"
)

// SmoothCaret interpolates caret position with sub-pixel precision for 120 FPS animation.
type SmoothCaret struct {
	CurrentX float32
	CurrentY float32
	TargetX  float32
	TargetY  float32
	Width    float32
	Height   float32
	Visible  bool
	Alpha    float32
}

// NewSmoothCaret creates a smooth caret state.
func NewSmoothCaret() *SmoothCaret {
	return &SmoothCaret{
		Width:   2.0,
		Height:  18.0,
		Visible: true,
		Alpha:   1.0,
	}
}

// Update calculates the next interpolation step towards the target position.
// lerpFactor is typically 0.25 at 120 FPS for snappy yet fluid transitions.
func (c *SmoothCaret) Update(lerpFactor float32) bool {
	dx := c.TargetX - c.CurrentX
	dy := c.TargetY - c.CurrentY

	if math.Abs(float64(dx)) < 0.05 && math.Abs(float64(dy)) < 0.05 {
		c.CurrentX = c.TargetX
		c.CurrentY = c.TargetY
		return false // settled
	}

	c.CurrentX += dx * lerpFactor
	c.CurrentY += dy * lerpFactor
	return true // animating
}

// SetTarget updates the target coordinates when the cursor moves in the text buffer.
func (c *SmoothCaret) SetTarget(x, y float32) {
	c.TargetX = x
	c.TargetY = y
}

// InertialScroll manages smooth inertial scrolling with friction damping.
type InertialScroll struct {
	OffsetY   float32
	TargetY   float32
	VelocityY float32
	Friction  float32
}

// NewInertialScroll creates an inertial scrolling state.
func NewInertialScroll() *InertialScroll {
	return &InertialScroll{
		Friction: 0.88,
	}
}

// AddDelta adds a scroll impulse from a trackpad or mouse wheel event.
func (s *InertialScroll) AddDelta(deltaY float32) {
	s.VelocityY += deltaY
	s.TargetY += deltaY
}

// Step performs one physical simulation tick.
func (s *InertialScroll) Step() bool {
	if math.Abs(float64(s.VelocityY)) < 0.1 && math.Abs(float64(s.TargetY-s.OffsetY)) < 0.1 {
		s.OffsetY = s.TargetY
		s.VelocityY = 0
		return false
	}

	s.OffsetY += (s.TargetY - s.OffsetY) * 0.2
	s.VelocityY *= s.Friction
	s.TargetY += s.VelocityY * 0.1
	return true
}

// ColorRGBA unpacks a 24-bit RGB TrueColor uint32 into normalized float32 RGBA components.
func ColorRGBA(rgb uint32, alpha float32) (r, g, b, a float32) {
	r = float32((rgb>>16)&0xFF) / 255.0
	g = float32((rgb>>8)&0xFF) / 255.0
	b = float32(rgb&0xFF) / 255.0
	a = alpha
	return
}

// RenderToken represents a formatted text run for GPU glyph rasterization.
type RenderToken struct {
	Text      string
	X         float32
	Y         float32
	Color     uint32
	Bold      bool
	Italic    bool
	Underline bool
}

// ThemeColors maps an internal ui.Theme to GPU render styles.
type ThemeColors struct {
	Background      uint32
	Foreground      uint32
	SelectionBg     uint32
	CursorLineBg    uint32
	GutterBg        uint32
	BorderColor     uint32
	Keyword         uint32
	Function        uint32
	String          uint32
	Comment         uint32
	Type            uint32
	Constant        uint32
	DiagnosticError uint32
	DiagnosticWarn  uint32
}

// LoadTheme loads colors from ui.Theme.
func LoadTheme(t ui.Theme) ThemeColors {
	return ThemeColors{
		Background:      t.Background,
		Foreground:      t.Foreground,
		SelectionBg:     t.SelectionBg,
		CursorLineBg:    t.CursorLineBg,
		GutterBg:        t.GutterBg,
		BorderColor:     t.BorderColor,
		Keyword:         t.Keyword,
		Function:        t.Function,
		String:          t.String,
		Comment:         t.Comment,
		Type:            t.Type,
		Constant:        t.Constant,
		DiagnosticError: t.DiagnosticError,
		DiagnosticWarn:  t.DiagnosticWarn,
	}
}
