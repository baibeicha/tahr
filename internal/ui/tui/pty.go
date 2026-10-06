package tui

import (
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/terminal"
)

// PTYSession represents an active interactive terminal pseudo-console session.
type PTYSession = terminal.PTYSession

// KeyToVT translates a GoatUI keyboard event into standard VT / xterm escape byte sequences.
func KeyToVT(k input.Key) []byte {
	return terminal.KeyToVT(k)
}
