//go:build !windows

package e2e

import (
	"testing"
)

func TestT4_SubprocessSupervision_AndCleanup(t *testing.T) {
	t.Skip("Win32 Job Object subprocess supervision test is Windows-specific")
}
