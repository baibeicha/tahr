//go:build !windows

package clipboard

func readOS() (string, error) {
	memMu.RLock()
	defer memMu.RUnlock()
	return memText, nil
}

func writeOS(text string) error {
	return nil
}
