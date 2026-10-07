//go:build !windows

package main

import (
	"github.com/baibeicha/goatui/pkg/driver"
	"github.com/baibeicha/goatui/pkg/driver/input"
)

func newTerminalDriver() (driver.Driver, error) {
	return driver.NewDriver(driver.WithKittyFlags(input.KittyModeDisambiguateEscapeCodes))
}
