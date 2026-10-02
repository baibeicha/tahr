//go:build gui

package main

import (
	"tahr/internal/core"
	"tahr/internal/ui"
	"tahr/internal/ui/gui"
)

// runGUI launches the graphical user interface for Tahr using the headless core engine and GPU pipeline.
func runGUI(projectDir string, files []string, themeName string) error {
	var th ui.Theme
	switch themeName {
	case "gruvbox":
		th = ui.GruvboxDark()
	case "tokyo-night":
		th = ui.TokyoNight()
	default:
		th = ui.CatppuccinMocha()
	}

	eng := core.NewEngine()
	for _, f := range files {
		_, _ = eng.Open(f)
	}

	app := gui.NewGuiApp(eng, th)
	return app.Run()
}
