package tui

import (
	"github.com/baibeicha/goatui/pkg/terminal"
)

// VTCell represents a styled character cell within the virtual terminal grid.
type VTCell = terminal.VTCell

// VTerm is a virtual terminal screen emulator with ANSI escape sequence decoding.
type VTerm = terminal.VTerm

// NewVTerm creates a new virtual terminal emulator with the given grid dimensions.
func NewVTerm(cols, rows int) *VTerm {
	return terminal.NewVTerm(cols, rows)
}
