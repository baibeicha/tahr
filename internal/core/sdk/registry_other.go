//go:build !windows

package sdk

// CheckUserWindowsPath returns false on non-Windows systems.
func CheckUserWindowsPath(dir string) (bool, error) {
	return false, nil
}

// AddUserWindowsPath is a no-op on non-Windows systems.
func AddUserWindowsPath(dir string) error {
	return nil
}
