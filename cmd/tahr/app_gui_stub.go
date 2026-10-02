//go:build !gui

package main

import (
	"fmt"
)

func runGUI(projectDir string, files []string, themeName string) error {
	return fmt.Errorf(
		"graphical frontend (Gio Desktop) is disabled in this binary.\n" +
			"To compile Tahr with GUI support, build with the 'gui' build tag:\n" +
			"  go build -tags gui -o bin/tahr-gui.exe ./cmd/tahr\n" +
			"Running in Terminal UI mode instead: tahr %s",
		files,
	)
}
