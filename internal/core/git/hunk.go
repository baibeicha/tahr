package git

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Hunk represents an individual diff chunk that can be staged or unstaged independently.
type Hunk struct {
	Index    int
	Header   string
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []string // Lines with '+', '-', or ' ' prefixes
	Staged   bool
}

// Summary returns a brief human-readable description of changes in this hunk.
func (h *Hunk) Summary() string {
	add := 0
	del := 0
	for _, l := range h.Lines {
		if strings.HasPrefix(l, "+") {
			add++
		} else if strings.HasPrefix(l, "-") {
			del++
		}
	}
	return fmt.Sprintf("%s (+%d, -%d lines)", h.Header, add, del)
}

// CommitNode represents a single commit in the visual branch history graph.
type CommitNode struct {
	Hash        string
	GraphPrefix string // ASCII / Unicode branch lines: '*', '|', '| \', etc.
	Refs        string // e.g. "HEAD -> main, origin/main, v1.0.0"
	Author      string
	Date        string
	Message     string
}

// ParseDiffHunks parses unified diff output into structured Hunk objects.
func ParseDiffHunks(diffOutput string) []Hunk {
	var hunks []Hunk
	if strings.TrimSpace(diffOutput) == "" {
		return hunks
	}

	scanner := bufio.NewScanner(strings.NewReader(diffOutput))
	var currentHunk *Hunk

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "@@") {
			if currentHunk != nil {
				hunks = append(hunks, *currentHunk)
			}

			currentHunk = &Hunk{
				Index:  len(hunks),
				Header: line,
				Lines:  make([]string, 0),
			}

			// Parse @@ -from,count +to,count @@
			parts := strings.Split(line, "@@")
			if len(parts) >= 3 {
				coords := strings.Fields(strings.TrimSpace(parts[1]))
				if len(coords) >= 2 {
					// -from,count
					fromStr := strings.TrimPrefix(coords[0], "-")
					fromParts := strings.Split(fromStr, ",")
					currentHunk.OldStart, _ = strconv.Atoi(fromParts[0])
					if len(fromParts) > 1 {
						currentHunk.OldCount, _ = strconv.Atoi(fromParts[1])
					} else {
						currentHunk.OldCount = 1
					}

					// +to,count
					toStr := strings.TrimPrefix(coords[1], "+")
					toParts := strings.Split(toStr, ",")
					currentHunk.NewStart, _ = strconv.Atoi(toParts[0])
					if len(toParts) > 1 {
						currentHunk.NewCount, _ = strconv.Atoi(toParts[1])
					} else {
						currentHunk.NewCount = 1
					}
				}
			}
			continue
		}

		if currentHunk != nil {
			if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") {
				currentHunk.Lines = append(currentHunk.Lines, line)
			}
		}
	}

	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}

	return hunks
}

// GetFileHunks queries git diff and returns all distinct hunks for the specified file.
func GetFileHunks(dir, filePath string) ([]Hunk, error) {
	if dir == "" {
		dir = "."
	}
	clean := filepath.Clean(filePath)

	cmd := exec.Command("git", "diff", "-U3", "HEAD", "--", clean)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// Fallback without HEAD
		cmd = exec.Command("git", "diff", "-U3", "--", clean)
		cmd.Dir = dir
		out, err = cmd.Output()
		if err != nil {
			return nil, err
		}
	}

	return ParseDiffHunks(string(out)), nil
}

// StageHunk stages a single hunk into the git staging index via git apply --cached.
func StageHunk(dir, filePath string, hunk Hunk) error {
	patch := GeneratePatchForHunk(filePath, hunk)
	return applyPatch(dir, patch, false)
}

// UnstageHunk removes a single staged hunk from the git index via git apply --cached --reverse.
func UnstageHunk(dir, filePath string, hunk Hunk) error {
	patch := GeneratePatchForHunk(filePath, hunk)
	return applyPatch(dir, patch, true)
}

// GeneratePatchForHunk creates a valid Git patch document for a single hunk.
func GeneratePatchForHunk(filePath string, hunk Hunk) string {
	relPath := filepath.ToSlash(filePath)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("--- a/%s\n", relPath))
	b.WriteString(fmt.Sprintf("+++ b/%s\n", relPath))
	b.WriteString(hunk.Header + "\n")
	for _, l := range hunk.Lines {
		b.WriteString(l + "\n")
	}
	return b.String()
}

func applyPatch(dir, patchContent string, reverse bool) error {
	tmpFile, err := os.CreateTemp("", "tahr_hunk_*.patch")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(patchContent); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	args := []string{"apply", "--cached"}
	if reverse {
		args = append(args, "--reverse")
	}
	args = append(args, tmpFile.Name())

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git apply failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ParseCommitGraphLines parses git log graph output into CommitNode structures.
func ParseCommitGraphLines(output string) []CommitNode {
	var nodes []CommitNode
	if strings.TrimSpace(output) == "" {
		return nodes
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.Split(line, "|||")
		if len(parts) >= 5 {
			firstPart := parts[0]
			hashIdx := strings.LastIndex(firstPart, " ")
			graphPrefix := "*"
			hash := strings.TrimSpace(firstPart)
			if hashIdx != -1 {
				graphPrefix = strings.TrimSpace(firstPart[:hashIdx])
				hash = strings.TrimSpace(firstPart[hashIdx+1:])
			}
			if graphPrefix == "" {
				graphPrefix = "*"
			}

			refs := strings.Trim(strings.TrimSpace(parts[1]), "()")
			author := strings.TrimSpace(parts[2])
			date := strings.TrimSpace(parts[3])
			msg := strings.TrimSpace(parts[4])

			nodes = append(nodes, CommitNode{
				Hash:        hash,
				GraphPrefix: graphPrefix,
				Refs:        refs,
				Author:      author,
				Date:        date,
				Message:     msg,
			})
		} else {
			// Line is pure graph symbols without commit content
			nodes = append(nodes, CommitNode{
				GraphPrefix: line,
			})
		}
	}

	return nodes
}

// GetCommitGraph retrieves formatted commit history and branch graph using git log.
func GetCommitGraph(dir string, maxCount int) ([]CommitNode, error) {
	if dir == "" {
		dir = "."
	}
	if maxCount <= 0 {
		maxCount = 50
	}

	format := "%h|||%d|||%an|||%cr|||%s"
	cmd := exec.Command("git", "log", "--graph", fmt.Sprintf("--pretty=format:%s", format), fmt.Sprintf("-n%d", maxCount))
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return ParseCommitGraphLines(string(out)), nil
}
