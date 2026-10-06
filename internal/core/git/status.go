package git

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FileItem represents a file with modified, staged, or untracked changes in the repository.
type FileItem struct {
	Path   string // Relative path to workspace root
	Status string // "M", "A", "D", "R", "?"
	Staged bool   // True if in index, false if working tree or untracked
}

// RepoStatus holds the categorized file changes in the workspace.
type RepoStatus struct {
	Branch    string
	Staged    []FileItem
	Unstaged  []FileItem
	Untracked []FileItem
}

// TotalChanges returns the count of files across all categories.
func (rs *RepoStatus) TotalChanges() int {
	return len(rs.Staged) + len(rs.Unstaged) + len(rs.Untracked)
}

// GetRepositoryStatus scans the git repository and returns all staged, unstaged, and untracked files.
func GetRepositoryStatus(dir string) (*RepoStatus, error) {
	if dir == "" {
		dir = "."
	}

	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = dir
	branchOut, _ := branchCmd.Output()
	branch := strings.TrimSpace(string(branchOut))

	cmd := exec.Command("git", "status", "--porcelain=v1", "-u")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return &RepoStatus{Branch: branch}, err
	}

	res := &RepoStatus{
		Branch:    branch,
		Staged:    make([]FileItem, 0),
		Unstaged:  make([]FileItem, 0),
		Untracked: make([]FileItem, 0),
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 3 {
			continue
		}

		x := line[0] // Staged status
		y := line[1] // Working tree status
		rawPath := strings.TrimSpace(line[3:])

		// Handle renames "orig -> new"
		if strings.Contains(rawPath, " -> ") {
			parts := strings.Split(rawPath, " -> ")
			if len(parts) >= 2 {
				rawPath = strings.TrimSpace(parts[len(parts)-1])
			}
		}
		rawPath = strings.Trim(rawPath, "\"")
		cleanRel := filepath.Clean(filepath.FromSlash(rawPath))

		if x == '?' && y == '?' {
			res.Untracked = append(res.Untracked, FileItem{
				Path:   cleanRel,
				Status: "?",
				Staged: false,
			})
			continue
		}

		// Staged changes (X)
		if x != ' ' && x != '?' {
			res.Staged = append(res.Staged, FileItem{
				Path:   cleanRel,
				Status: string(x),
				Staged: true,
			})
		}

		// Working tree changes (Y)
		if y != ' ' && y != '?' {
			res.Unstaged = append(res.Unstaged, FileItem{
				Path:   cleanRel,
				Status: string(y),
				Staged: false,
			})
		}
	}

	return res, nil
}

// StageFile adds a specific file to the git index.
func StageFile(dir, relPath string) error {
	if dir == "" {
		dir = "."
	}
	clean := filepath.Clean(relPath)
	cmd := exec.Command("git", "add", "--", clean)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git add failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UnstageFile removes a specific file from the git staging area.
func UnstageFile(dir, relPath string) error {
	if dir == "" {
		dir = "."
	}
	clean := filepath.Clean(relPath)
	cmd := exec.Command("git", "restore", "--staged", "--", clean)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback for older git versions
		resetCmd := exec.Command("git", "reset", "HEAD", "--", clean)
		resetCmd.Dir = dir
		if resetOut, rErr := resetCmd.CombinedOutput(); rErr != nil {
			return fmt.Errorf("git unstage failed: %v (%s / %s)", err, strings.TrimSpace(string(out)), strings.TrimSpace(string(resetOut)))
		}
	}
	return nil
}

// StageAll stages all modifications, deletions, and new untracked files.
func StageAll(dir string) error {
	if dir == "" {
		dir = "."
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git add -A failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UnstageAll unstages all files from the git index.
func UnstageAll(dir string) error {
	if dir == "" {
		dir = "."
	}
	cmd := exec.Command("git", "restore", "--staged", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		resetCmd := exec.Command("git", "reset", "HEAD")
		resetCmd.Dir = dir
		if resetOut, rErr := resetCmd.CombinedOutput(); rErr != nil {
			return fmt.Errorf("git reset HEAD failed: %v (%s / %s)", err, strings.TrimSpace(string(out)), strings.TrimSpace(string(resetOut)))
		}
	}
	return nil
}

// Commit creates a commit with the specified message.
func Commit(dir, message string) error {
	if dir == "" {
		dir = "."
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return fmt.Errorf("commit message cannot be empty")
	}
	cmd := exec.Command("git", "commit", "-m", msg)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// GetFileUnifiedDiff returns a unified diff string for the given file.
func GetFileUnifiedDiff(dir, relPath string, staged, untracked bool) (string, error) {
	if dir == "" {
		dir = "."
	}
	clean := filepath.Clean(relPath)

	if untracked {
		fullPath := filepath.Join(dir, clean)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", err
		}
		// If binary or too huge, limit preview
		text := string(data)
		lines := strings.Split(text, "\n")
		var b strings.Builder
		b.WriteString(fmt.Sprintf("--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", filepath.ToSlash(clean), len(lines)))
		for i, line := range lines {
			if i >= 500 {
				b.WriteString(fmt.Sprintf("+ ... [truncated, %d more lines]\n", len(lines)-500))
				break
			}
			b.WriteString("+" + line + "\n")
		}
		return b.String(), nil
	}

	var args []string
	if staged {
		args = []string{"diff", "--cached", "-U3", "--", clean}
	} else {
		args = []string{"diff", "-U3", "--", clean}
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
