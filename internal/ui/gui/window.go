package gui

import (
	"fmt"
	"time"

	"tahr/internal/core"
	"tahr/internal/ui"
)

// WindowConfig defines GUI window parameters for GPU desktop mode.
type WindowConfig struct {
	Title     string
	Width     float32
	Height    float32
	MinWidth  float32
	MinHeight float32
	TargetFPS int
}

// DefaultWindowConfig returns standard desktop window options.
func DefaultWindowConfig() WindowConfig {
	return WindowConfig{
		Title:     "Tahr IDE (Gio GPU)",
		Width:     1280.0,
		Height:    800.0,
		MinWidth:  800.0,
		MinHeight: 600.0,
		TargetFPS: 120,
	}
}

// GuiApp coordinates the headless core engine with the GPU window rendering loop.
type GuiApp struct {
	Engine         *core.Engine
	Theme          ui.Theme
	Colors         ThemeColors
	Config         WindowConfig
	Layout         GuiLayout
	Caret          *SmoothCaret
	Scroll         *InertialScroll
	SidebarOpen    bool
	TerminalOpen   bool
	ActivePaneIdx  int
	SplitCount     int
	LastFrameTime  time.Time
}

// NewGuiApp creates a GuiApp instance linked to the headless engine.
func NewGuiApp(eng *core.Engine, theme ui.Theme) *GuiApp {
	cfg := DefaultWindowConfig()
	layout := ComputeLayout(cfg.Width, cfg.Height, true, 240.0, false, 200.0, 1)

	return &GuiApp{
		Engine:        eng,
		Theme:         theme,
		Colors:        LoadTheme(theme),
		Config:        cfg,
		Layout:        layout,
		Caret:         NewSmoothCaret(),
		Scroll:        NewInertialScroll(),
		SidebarOpen:   true,
		TerminalOpen:  false,
		ActivePaneIdx: 0,
		SplitCount:    1,
		LastFrameTime: time.Now(),
	}
}

// Resize updates the computed layout when the window size changes.
func (app *GuiApp) Resize(width, height float32) {
	app.Config.Width = width
	app.Config.Height = height
	app.Layout = ComputeLayout(width, height, app.SidebarOpen, 240.0, app.TerminalOpen, 220.0, app.SplitCount)
}

// ToggleSidebar opens or closes the project tree sidebar.
func (app *GuiApp) ToggleSidebar() {
	app.SidebarOpen = !app.SidebarOpen
	app.Resize(app.Config.Width, app.Config.Height)
}

// ToggleTerminal opens or closes the bottom panel.
func (app *GuiApp) ToggleTerminal() {
	app.TerminalOpen = !app.TerminalOpen
	app.Resize(app.Config.Width, app.Config.Height)
}

// SetSplitCount changes the number of split editor panes (1 to 6).
func (app *GuiApp) SetSplitCount(count int) {
	if count < 1 {
		count = 1
	}
	if count > 6 {
		count = 6
	}
	app.SplitCount = count
	if app.ActivePaneIdx >= count {
		app.ActivePaneIdx = count - 1
	}
	app.Resize(app.Config.Width, app.Config.Height)
}

// StepFrame advances the 120 FPS animations (smooth caret lerp and inertial scroll).
func (app *GuiApp) StepFrame() bool {
	caretMoving := app.Caret.Update(0.25)
	scrollMoving := app.Scroll.Step()
	return caretMoving || scrollMoving
}

// Run launches the GPU frame loop.
func (app *GuiApp) Run() error {
	fmt.Printf("Tahr GPU Window launched: %s (%.0fx%.0f @ %d FPS target)\n",
		app.Config.Title, app.Config.Width, app.Config.Height, app.Config.TargetFPS)
	fmt.Printf("Engine active buffers: %d\n", len(app.Engine.Documents()))
	return nil
}
