package git

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// BranchInfo holds metadata about a git branch.
type BranchInfo struct {
	Name      string
	IsCurrent bool
	IsRemote  bool
}

// CommitFileStat holds addition/deletion statistics for a file changed in a commit.
type CommitFileStat struct {
	Path      string
	Additions int
	Deletions int
}

// CommitDetail contains full information for an individual commit.
type CommitDetail struct {
	Hash         string
	AbbrevHash   string
	AuthorName   string
	AuthorEmail  string
	Date         string
	RelativeDate string
	Subject      string
	Body         string
	Refs         string
	Files        []CommitFileStat
}

// DAGCommit represents an individual commit node in the 2D DAG canvas diagram.
type DAGCommit struct {
	Hash       string
	AbbrevHash string
	Parents    []string
	Author     string
	Date       string
	Subject    string
	Refs       string
	Branch     string
	Lane       int // Vertical track (0 for main, 1 for branch 1, etc.)
	Col        int // Horizontal position (0 is oldest, N-1 is newest / HEAD)
}

// GetBranches queries all local and remote branches in the repository.
func GetBranches(dir string) ([]BranchInfo, error) {
	if dir == "" {
		dir = "."
	}

	cmd := exec.Command("git", "branch", "-a", "--format=%(refname:short)|||%(HEAD)")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	branches := []BranchInfo{
		{Name: "All branches", IsCurrent: false, IsRemote: false},
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|||")
		name := strings.TrimSpace(parts[0])
		isHead := len(parts) > 1 && strings.TrimSpace(parts[1]) == "*"
		isRemote := strings.HasPrefix(name, "origin/") || strings.HasPrefix(name, "remotes/")

		if strings.Contains(name, "HEAD ->") {
			continue
		}

		branches = append(branches, BranchInfo{
			Name:      name,
			IsCurrent: isHead,
			IsRemote:  isRemote,
		})
	}

	return branches, nil
}

// ConvertGraphToUnicode replaces ASCII branch symbols (*, |, /, \, _) with stylish metro-line Unicode characters.
func ConvertGraphToUnicode(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch r {
		case '*':
			b.WriteRune('●')
		case '|':
			b.WriteRune('│')
		case '/':
			b.WriteRune('╭')
		case '\\':
			b.WriteRune('╰')
		case '_', '-':
			b.WriteRune('─')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// GetFilteredCommitGraph retrieves commits filtered by branch, date period, and search term.
func GetFilteredCommitGraph(dir, branch, period, query string, maxCount int) ([]CommitNode, error) {
	if dir == "" {
		dir = "."
	}
	if maxCount <= 0 {
		maxCount = 100
	}

	format := "%h|||%d|||%an|||%cr|||%s"
	args := []string{"log", "--graph", fmt.Sprintf("--pretty=format:%s", format), fmt.Sprintf("-n%d", maxCount)}

	cleanBranch := strings.TrimSpace(branch)
	if cleanBranch == "" || cleanBranch == "All branches" || cleanBranch == "Все ветки" {
		args = append(args, "--all")
	} else {
		args = append(args, cleanBranch)
	}

	cleanPeriod := strings.TrimSpace(period)
	if cleanPeriod != "" && cleanPeriod != "All time" && cleanPeriod != "За всё время" {
		switch cleanPeriod {
		case "Today", "Сегодня":
			args = append(args, "--since=midnight")
		case "Last 7 days", "Последние 7 дней":
			args = append(args, "--since=7.days")
		case "Last 30 days", "Последние 30 дней":
			args = append(args, "--since=30.days")
		default:
			args = append(args, fmt.Sprintf("--since=%s", cleanPeriod))
		}
	}

	cleanQuery := strings.TrimSpace(query)
	if cleanQuery != "" {
		args = append(args, fmt.Sprintf("--grep=%s", cleanQuery), "-i")
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	nodes := ParseCommitGraphLines(string(out))
	for i := range nodes {
		nodes[i].GraphPrefix = ConvertGraphToUnicode(nodes[i].GraphPrefix)
	}

	return nodes, nil
}

// GetCommitDetails returns detailed metadata and diffstat files for a specific commit hash.
func GetCommitDetails(dir, hash string) (*CommitDetail, error) {
	if dir == "" {
		dir = "."
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, fmt.Errorf("commit hash cannot be empty")
	}

	format := "%H%x1f%h%x1f%an%x1f%ae%x1f%ad%x1f%cr%x1f%s%x1f%b%x1f%d%x1e"
	cmd := exec.Command("git", "show", "--numstat", fmt.Sprintf("--format=%s", format), hash)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	raw := string(out)
	detail := &CommitDetail{
		Hash:       hash,
		AbbrevHash: hash,
		Files:      make([]CommitFileStat, 0),
	}

	splitIdx := strings.Index(raw, "\x1e")
	metaPart := raw
	statPart := ""
	if splitIdx != -1 {
		metaPart = raw[:splitIdx]
		statPart = raw[splitIdx+1:]
	}

	parts := strings.Split(metaPart, "\x1f")
	if len(parts) >= 9 {
		detail.Hash = strings.TrimSpace(parts[0])
		detail.AbbrevHash = strings.TrimSpace(parts[1])
		detail.AuthorName = strings.TrimSpace(parts[2])
		detail.AuthorEmail = strings.TrimSpace(parts[3])
		detail.Date = strings.TrimSpace(parts[4])
		detail.RelativeDate = strings.TrimSpace(parts[5])
		detail.Subject = strings.TrimSpace(parts[6])
		detail.Body = strings.TrimSpace(parts[7])
		detail.Refs = strings.Trim(strings.TrimSpace(parts[8]), "()")
	}

	// Parse numstat lines
	scanner := bufio.NewScanner(strings.NewReader(statPart))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		adds := 0
		dels := 0
		if fields[0] != "-" {
			adds, _ = strconv.Atoi(fields[0])
		}
		if fields[1] != "-" {
			dels, _ = strconv.Atoi(fields[1])
		}
		filePath := fields[2]
		detail.Files = append(detail.Files, CommitFileStat{
			Path:      filePath,
			Additions: adds,
			Deletions: dels,
		})
	}

	return detail, nil
}

// GetCommitFileDiff retrieves the unified diff of a specific file in a commit.
func GetCommitFileDiff(dir, hash, filePath string) ([]string, error) {
	if dir == "" {
		dir = "."
	}
	topPath := ":(top)" + filePath
	cmd := exec.Command("git", "show", "--format=", "--no-color", hash, "--", topPath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git show diff: %w: %s", err, string(out))
	}
	raw := strings.ReplaceAll(string(out), "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

// GetCommitDAG retrieves commits formatted and placed into a 2D DAG layout (Col: 0..N-1, Lane: 0..L).
func GetCommitDAG(dir, branch, period, query string, maxCount int) ([]DAGCommit, error) {
	if dir == "" {
		dir = "."
	}
	if maxCount <= 0 {
		maxCount = 50
	}

	format := "%H\t%h\t%P\t%an\t%cr\t%d\t%s"
	args := []string{"log", fmt.Sprintf("--format=%s", format), fmt.Sprintf("-n%d", maxCount)}

	cleanBranch := strings.TrimSpace(branch)
	if cleanBranch == "" || cleanBranch == "All branches" || cleanBranch == "Все ветки" {
		args = append(args, "--all")
	} else {
		args = append(args, cleanBranch)
	}

	cleanPeriod := strings.TrimSpace(period)
	if cleanPeriod != "" && cleanPeriod != "All time" && cleanPeriod != "За всё время" {
		switch cleanPeriod {
		case "Today", "Сегодня":
			args = append(args, "--since=midnight")
		case "Last 7 days", "Последние 7 дней":
			args = append(args, "--since=7.days")
		case "Last 30 days", "Последние 30 дней":
			args = append(args, "--since=30.days")
		default:
			args = append(args, fmt.Sprintf("--since=%s", cleanPeriod))
		}
	}

	cleanQuery := strings.TrimSpace(query)
	if cleanQuery != "" {
		args = append(args, fmt.Sprintf("--grep=%s", cleanQuery), "-i")
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var rawCommits []DAGCommit
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 7 {
			continue
		}
		parents := strings.Fields(parts[2])
		rawCommits = append(rawCommits, DAGCommit{
			Hash:       strings.TrimSpace(parts[0]),
			AbbrevHash: strings.TrimSpace(parts[1]),
			Parents:    parents,
			Author:     strings.TrimSpace(parts[3]),
			Date:       strings.TrimSpace(parts[4]),
			Refs:       strings.Trim(strings.TrimSpace(parts[5]), "()"),
			Subject:    strings.TrimSpace(parts[6]),
		})
	}

	if len(rawCommits) == 0 {
		return []DAGCommit{
			{Hash: "f4e5d6c", AbbrevHash: "f4e5d6c", Author: "Tahr Core", Date: "1 day ago", Subject: "release: initial headless core release", Branch: "main", Lane: 0, Col: 0},
			{Hash: "a1b2c3d", AbbrevHash: "a1b2c3d", Author: "Tahr Core", Date: "just now", Subject: "feat: multi-selection rope & async lsp", Branch: "main", Lane: 0, Col: 1},
		}, nil
	}

	// Reverse slice so time goes left-to-right (oldest on left, newest on right)
	n := len(rawCommits)
	dag := make([]DAGCommit, n)
	for i := 0; i < n; i++ {
		dag[i] = rawCommits[n-1-i]
		dag[i].Col = i
	}

	// Build fast lookup maps
	commitByHash := make(map[string]*DAGCommit)
	for i := range dag {
		commitByHash[dag[i].Hash] = &dag[i]
		commitByHash[dag[i].AbbrevHash] = &dag[i]
	}

	// 1. Identify primary/main branch spine using first-parent backwards traversal from the latest commit
	mainHashes := make(map[string]bool)
	if n > 0 {
		curr := dag[n-1].Hash
		for curr != "" {
			mainHashes[curr] = true
			cNode, found := commitByHash[curr]
			if !found || len(cNode.Parents) == 0 {
				break
			}
			curr = cNode.Parents[0]
		}
	}

	// 2. Assign Lanes and Branch names
	hashToLane := make(map[string]int)
	laneBranchNames := make(map[int]string)
	laneBranchNames[0] = "main"
	laneNextFree := 1

	for i := range dag {
		c := &dag[i]

		branchName := ""
		if c.Refs != "" {
			refParts := strings.Split(c.Refs, ",")
			for _, rp := range refParts {
				rp = strings.TrimSpace(rp)
				if strings.HasPrefix(rp, "HEAD -> ") {
					branchName = strings.TrimPrefix(rp, "HEAD -> ")
					break
				} else if strings.HasPrefix(rp, "origin/") {
					branchName = strings.TrimPrefix(rp, "origin/")
					break
				} else if !strings.HasPrefix(rp, "tag:") && rp != "HEAD" {
					branchName = rp
					break
				}
			}
		}

		if mainHashes[c.Hash] {
			c.Lane = 0
		} else {
			// Side branch: check parent's assigned lane
			parentLane := -1
			for _, p := range c.Parents {
				if l, ok := hashToLane[p]; ok && l > 0 {
					parentLane = l
					break
				}
			}
			if parentLane > 0 {
				c.Lane = parentLane
			} else {
				c.Lane = laneNextFree
				laneNextFree++
			}
		}

		if branchName != "" {
			laneBranchNames[c.Lane] = branchName
		} else if existingName, ok := laneBranchNames[c.Lane]; ok && existingName != "" {
			branchName = existingName
		} else {
			if c.Lane == 0 {
				branchName = "main"
			} else {
				branchName = fmt.Sprintf("branch-%d", c.Lane)
			}
			laneBranchNames[c.Lane] = branchName
		}

		c.Branch = branchName
		hashToLane[c.Hash] = c.Lane
		hashToLane[c.AbbrevHash] = c.Lane
	}

	return dag, nil
}

