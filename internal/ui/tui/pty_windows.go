//go:build windows

package tui

import (
	"github.com/baibeicha/goatui/pkg/terminal"
)

// StartShell launches an interactive shell session using Windows ConPTY (with pipe fallback).
func StartShell(cwd string, cols, rows int) (PTYSession, error) {
	return terminal.StartShell(cwd, cols, rows)
}
