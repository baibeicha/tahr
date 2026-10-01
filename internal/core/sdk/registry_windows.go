//go:build windows

package sdk

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// CheckUserWindowsPath checks whether dir is in the user's HKCU\Environment PATH.
func CheckUserWindowsPath(dir string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer k.Close()

	val, _, err := k.GetStringValue("Path")
	if err != nil {
		// Key might not exist yet
		return false, nil
	}

	cleanDir := strings.ToLower(filepath.Clean(dir))
	for _, p := range strings.Split(val, ";") {
		if strings.ToLower(filepath.Clean(p)) == cleanDir {
			return true, nil
		}
	}
	return false, nil
}

// AddUserWindowsPath appends dir to the user's HKCU\Environment PATH.
func AddUserWindowsPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("open HKCU\\Environment: %w", err)
	}
	defer k.Close()

	existingPath, _, err := k.GetStringValue("Path")
	if err != nil {
		existingPath = ""
	}

	cleanDir := filepath.Clean(dir)
	for _, p := range strings.Split(existingPath, ";") {
		if strings.EqualFold(filepath.Clean(p), cleanDir) {
			return nil // already present
		}
	}

	newPath := existingPath
	if newPath != "" && !strings.HasSuffix(newPath, ";") {
		newPath += ";"
	}
	newPath += cleanDir

	return k.SetStringValue("Path", newPath)
}
