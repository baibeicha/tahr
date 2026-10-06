package taskrunner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TaskSource represents the origin of discovered tasks.
type TaskSource string

const (
	SourceMakefile    TaskSource = "Makefile"
	SourcePackageJSON TaskSource = "package.json"
	SourceTaskfile    TaskSource = "Taskfile"
	SourceJustfile    TaskSource = "justfile"
)

// Task represents an executable task discovered in the workspace.
type Task struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Source      TaskSource `json:"source"`
	SourceFile  string     `json:"source_file"`
	Command     string     `json:"command"`
	Args        []string   `json:"args"`
	WorkingDir  string     `json:"working_dir"`
}

// TaskGroup groups tasks by source file.
type TaskGroup struct {
	SourceFile string     `json:"source_file"`
	SourceType TaskSource `json:"source_type"`
	Tasks      []Task     `json:"tasks"`
}

// Title returns a human-readable title e.g. "Makefile (3 tasks)".
func (g TaskGroup) Title() string {
	unit := "tasks"
	if len(g.Tasks) == 1 {
		unit = "task"
	}
	return fmt.Sprintf("%s (%d %s)", g.SourceFile, len(g.Tasks), unit)
}

// Target identifier regex for Makefile targets.
var makeTargetRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-\./]+$`)

// Justfile recipe identifier regex.
var justRecipeRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// ParseMakefile parses tasks from Makefile content.
// It detects targets (e.g. "build:", "test:", "lint:"), while ignoring internal
// targets starting with '.' (like .PHONY, .DEFAULT), pattern rules (%),
// and variable assignments.
func ParseMakefile(content []byte, filePath string) ([]Task, error) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var tasks []Task
	seen := make(map[string]bool)

	workingDir := "."
	if filePath != "" {
		workingDir = filepath.Dir(filePath)
	}

	var pendingDoc []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// 1. Comments
		if strings.HasPrefix(trimmed, "#") {
			commentText := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if commentText != "" {
				pendingDoc = append(pendingDoc, commentText)
			}
			continue
		}

		// 2. Empty line resets pending comment
		if trimmed == "" {
			pendingDoc = nil
			continue
		}

		// 3. Skip indented lines (recipe instructions)
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ") {
			continue
		}

		// 4. Skip Makefile directives (include, ifeq, export, etc.)
		lowerTrimmed := strings.ToLower(trimmed)
		if strings.HasPrefix(lowerTrimmed, "include ") ||
			strings.HasPrefix(lowerTrimmed, "-include ") ||
			strings.HasPrefix(lowerTrimmed, "sinclude ") ||
			strings.HasPrefix(lowerTrimmed, "ifeq ") ||
			strings.HasPrefix(lowerTrimmed, "ifneq ") ||
			strings.HasPrefix(lowerTrimmed, "ifdef ") ||
			strings.HasPrefix(lowerTrimmed, "ifndef ") ||
			strings.HasPrefix(lowerTrimmed, "else") ||
			strings.HasPrefix(lowerTrimmed, "endif") ||
			strings.HasPrefix(lowerTrimmed, "export ") ||
			strings.HasPrefix(lowerTrimmed, "unexport ") ||
			strings.HasPrefix(lowerTrimmed, "vpath ") ||
			strings.HasPrefix(lowerTrimmed, "override ") ||
			strings.HasPrefix(lowerTrimmed, "define ") ||
			strings.HasPrefix(lowerTrimmed, "endef") {
			pendingDoc = nil
			continue
		}

		// 5. Look for target separator ':'
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			pendingDoc = nil
			continue
		}

		// Check if it's a variable assignment like 'FOO := bar', 'FOO = bar', 'FOO ?= bar', 'FOO += bar'
		if colonIdx+1 < len(trimmed) && trimmed[colonIdx+1] == '=' {
			pendingDoc = nil
			continue
		}
		eqIdx := strings.Index(trimmed, "=")
		if eqIdx != -1 && eqIdx < colonIdx {
			pendingDoc = nil
			continue
		}

		// Target portion before ':'
		targetsPart := strings.TrimSpace(trimmed[:colonIdx])
		restPart := trimmed[colonIdx+1:]
		if strings.HasPrefix(restPart, ":") {
			// Double colon rule 'target::'
			restPart = restPart[1:]
		}

		// Extract doc comment either from trailing ## or # on the same line, or preceding comment
		desc := ""
		if hashIdx := strings.Index(restPart, "##"); hashIdx != -1 {
			desc = strings.TrimSpace(restPart[hashIdx+2:])
		} else if hashIdx := strings.Index(restPart, "#"); hashIdx != -1 {
			desc = strings.TrimSpace(restPart[hashIdx+1:])
		} else if len(pendingDoc) > 0 {
			desc = strings.Join(pendingDoc, " ")
		}

		// Parse targets in this rule (e.g. "build test: deps")
		targets := strings.Fields(targetsPart)
		for _, target := range targets {
			// Ignore internal / special targets starting with '.' (like .PHONY)
			if strings.HasPrefix(target, ".") {
				continue
			}
			// Ignore pattern rules containing '%'
			if strings.Contains(target, "%") {
				continue
			}
			// Ignore variable substitutions containing '$'
			if strings.Contains(target, "$") {
				continue
			}
			if !makeTargetRegex.MatchString(target) {
				continue
			}

			if seen[target] {
				continue
			}
			seen[target] = true

			taskID := fmt.Sprintf("%s:%s", filePath, target)
			if filePath == "" {
				taskID = fmt.Sprintf("Makefile:%s", target)
			}

			tasks = append(tasks, Task{
				ID:          taskID,
				Name:        target,
				Description: desc,
				Source:      SourceMakefile,
				SourceFile:  filePath,
				Command:     fmt.Sprintf("make %s", target),
				Args:        []string{"make", target},
				WorkingDir:  workingDir,
			})
		}

		pendingDoc = nil
	}

	return tasks, scanner.Err()
}

// ParsePackageJSON parses tasks from package.json content (the "scripts" object).
// It preserves declaration order and extracts each script name and command.
func ParsePackageJSON(content []byte, filePath string) ([]Task, error) {
	workingDir := "."
	if filePath != "" {
		workingDir = filepath.Dir(filePath)
	}

	// First verify valid JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("invalid package.json: %w", err)
	}

	scriptsRaw, exists := raw["scripts"]
	if !exists || len(scriptsRaw) == 0 {
		return []Task{}, nil
	}

	// Extract scripts preserving order via streaming token decoder
	dec := json.NewDecoder(bytes.NewReader(scriptsRaw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to read scripts object: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return []Task{}, nil
	}

	var tasks []Task
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			break
		}
		scriptName, ok := keyTok.(string)
		if !ok {
			continue
		}

		var scriptCmd string
		if err := dec.Decode(&scriptCmd); err != nil {
			continue
		}

		taskID := fmt.Sprintf("%s:%s", filePath, scriptName)
		if filePath == "" {
			taskID = fmt.Sprintf("package.json:%s", scriptName)
		}

		tasks = append(tasks, Task{
			ID:          taskID,
			Name:        scriptName,
			Description: scriptCmd,
			Source:      SourcePackageJSON,
			SourceFile:  filePath,
			Command:     fmt.Sprintf("npm run %s", scriptName),
			Args:        []string{"npm", "run", scriptName},
			WorkingDir:  workingDir,
		})
	}

	return tasks, nil
}

// ParseTaskfile parses tasks from Taskfile.yml / Taskfile.yaml content.
// It extracts tasks declared under the "tasks:" key along with their "desc:" or "summary:".
func ParseTaskfile(content []byte, filePath string) ([]Task, error) {
	workingDir := "."
	if filePath != "" {
		workingDir = filepath.Dir(filePath)
	}

	scanner := bufio.NewScanner(bytes.NewReader(content))
	var tasks []Task

	inTasksSection := false
	tasksIndent := -1
	currentTaskIndent := -1
	var currentTask *Task

	flushCurrent := func() {
		if currentTask != nil && currentTask.Name != "" {
			tasks = append(tasks, *currentTask)
			currentTask = nil
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Ignore empty lines and pure comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Calculate indentation
		indent := len(line) - len(strings.TrimLeft(line, " "))

		// Check root level "tasks:" section
		if !inTasksSection {
			if strings.HasPrefix(trimmed, "tasks:") {
				inTasksSection = true
				tasksIndent = indent
			}
			continue
		}

		// If we encounter a new top-level key (indent <= tasksIndent), tasks section ended
		if indent <= tasksIndent {
			flushCurrent()
			inTasksSection = false
			continue
		}

		// Inside tasks section:
		// A task name is a key with indent > tasksIndent
		if currentTaskIndent == -1 || indent == currentTaskIndent || (currentTaskIndent > 0 && indent < currentTaskIndent) {
			colonIdx := strings.Index(trimmed, ":")
			if colonIdx > 0 {
				potentialKey := strings.TrimSpace(trimmed[:colonIdx])
				potentialKey = strings.Trim(potentialKey, `"'`)

				// Check if this is a sub-property of existing task (like desc, summary, cmds)
				isSubProperty := false
				if currentTask != nil && indent > currentTaskIndent {
					isSubProperty = true
				}

				if !isSubProperty {
					flushCurrent()
					currentTaskIndent = indent

					taskID := fmt.Sprintf("%s:%s", filePath, potentialKey)
					if filePath == "" {
						taskID = fmt.Sprintf("Taskfile:%s", potentialKey)
					}

					currentTask = &Task{
						ID:         taskID,
						Name:       potentialKey,
						Source:     SourceTaskfile,
						SourceFile: filePath,
						Command:    fmt.Sprintf("task %s", potentialKey),
						Args:       []string{"task", potentialKey},
						WorkingDir: workingDir,
					}

					// Check if there is an inline command on the same line: "taskname: echo 1"
					afterColon := strings.TrimSpace(trimmed[colonIdx+1:])
					if afterColon != "" && !strings.HasPrefix(afterColon, "#") {
						currentTask.Description = cleanYAMLValue(afterColon)
					}
					continue
				}
			}
		}

		// Inside current task properties
		if currentTask != nil && indent > currentTaskIndent {
			colonIdx := strings.Index(trimmed, ":")
			if colonIdx > 0 {
				propName := strings.TrimSpace(trimmed[:colonIdx])
				propVal := strings.TrimSpace(trimmed[colonIdx+1:])
				propVal = cleanYAMLValue(propVal)

				switch propName {
				case "desc":
					if propVal != "" {
						currentTask.Description = propVal
					}
				case "summary":
					if currentTask.Description == "" && propVal != "" {
						currentTask.Description = propVal
					}
				case "cmd":
					if currentTask.Description == "" && propVal != "" {
						currentTask.Description = propVal
					}
				}
			}
		}
	}

	flushCurrent()
	return tasks, scanner.Err()
}

// ParseJustfile parses tasks from justfile content.
// It extracts recipes and their doc comments while ignoring assignments,
// directives, and private recipes (e.g. starting with '_').
func ParseJustfile(content []byte, filePath string) ([]Task, error) {
	workingDir := "."
	if filePath != "" {
		workingDir = filepath.Dir(filePath)
	}

	scanner := bufio.NewScanner(bytes.NewReader(content))
	var tasks []Task
	seen := make(map[string]bool)

	var pendingDoc []string
	isPrivate := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// 1. Comments: capture doc comment
		if strings.HasPrefix(trimmed, "#") {
			commentText := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if commentText != "" {
				pendingDoc = append(pendingDoc, commentText)
			}
			continue
		}

		// 2. Empty lines reset doc comments
		if trimmed == "" {
			pendingDoc = nil
			isPrivate = false
			continue
		}

		// 3. Skip indented lines (recipe commands)
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}

		// 4. Attributes line: e.g. [private], [no-exit-message], [linux]
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if strings.Contains(trimmed, "private") {
				isPrivate = true
			}
			continue
		}

		// 5. Skip variable assignments, settings, and directives
		// justfile assignments: 'name := val' or 'name = val'
		if strings.Contains(trimmed, ":=") {
			pendingDoc = nil
			isPrivate = false
			continue
		}

		lowerTrimmed := strings.ToLower(trimmed)
		if strings.HasPrefix(lowerTrimmed, "set ") ||
			strings.HasPrefix(lowerTrimmed, "alias ") ||
			strings.HasPrefix(lowerTrimmed, "export ") ||
			strings.HasPrefix(lowerTrimmed, "unexport ") ||
			strings.HasPrefix(lowerTrimmed, "import ") ||
			strings.HasPrefix(lowerTrimmed, "mod ") {
			pendingDoc = nil
			isPrivate = false
			continue
		}

		// 6. Look for ':' separating recipe header from dependencies
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			pendingDoc = nil
			isPrivate = false
			continue
		}

		headerPart := strings.TrimSpace(trimmed[:colonIdx])
		restPart := trimmed[colonIdx+1:]

		// Recipe name can have '@' prefix (meaning quiet)
		headerPart = strings.TrimPrefix(headerPart, "@")

		// First word of header is the recipe name
		fields := strings.Fields(headerPart)
		if len(fields) == 0 {
			pendingDoc = nil
			isPrivate = false
			continue
		}

		recipeName := fields[0]

		// Skip private recipes starting with '_' or marked [private]
		if strings.HasPrefix(recipeName, "_") || isPrivate {
			pendingDoc = nil
			isPrivate = false
			continue
		}

		if !justRecipeRegex.MatchString(recipeName) {
			pendingDoc = nil
			isPrivate = false
			continue
		}

		if seen[recipeName] {
			pendingDoc = nil
			isPrivate = false
			continue
		}
		seen[recipeName] = true

		// Extract description
		desc := ""
		if hashIdx := strings.Index(restPart, "##"); hashIdx != -1 {
			desc = strings.TrimSpace(restPart[hashIdx+2:])
		} else if hashIdx := strings.Index(restPart, "#"); hashIdx != -1 {
			desc = strings.TrimSpace(restPart[hashIdx+1:])
		} else if len(pendingDoc) > 0 {
			desc = strings.Join(pendingDoc, " ")
		}

		taskID := fmt.Sprintf("%s:%s", filePath, recipeName)
		if filePath == "" {
			taskID = fmt.Sprintf("justfile:%s", recipeName)
		}

		tasks = append(tasks, Task{
			ID:          taskID,
			Name:        recipeName,
			Description: desc,
			Source:      SourceJustfile,
			SourceFile:  filePath,
			Command:     fmt.Sprintf("just %s", recipeName),
			Args:        []string{"just", recipeName},
			WorkingDir:  workingDir,
		})

		pendingDoc = nil
		isPrivate = false
	}

	return tasks, scanner.Err()
}

// DiscoverTasks scans the specified workspace directory for task files:
// - Makefile
// - package.json
// - Taskfile.yml / Taskfile.yaml
// - justfile
// Returns task groups grouped by source file.
func DiscoverTasks(workspaceDir string) ([]TaskGroup, error) {
	if workspaceDir == "" {
		var err error
		workspaceDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	var groups []TaskGroup

	// Helper to parse a file if exists
	checkAndParse := func(relPath string, srcType TaskSource, parseFn func([]byte, string) ([]Task, error)) {
		fullPath := filepath.Join(workspaceDir, relPath)
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() {
			return
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			return
		}

		tasks, err := parseFn(content, relPath)
		if err == nil && len(tasks) > 0 {
			groups = append(groups, TaskGroup{
				SourceFile: relPath,
				SourceType: srcType,
				Tasks:      tasks,
			})
		}
	}

	// 1. Makefile
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		prevLen := len(groups)
		checkAndParse(name, SourceMakefile, ParseMakefile)
		if len(groups) > prevLen {
			break
		}
	}

	// 2. package.json
	checkAndParse("package.json", SourcePackageJSON, ParsePackageJSON)

	// 3. Taskfile.yml / Taskfile.yaml
	for _, name := range []string{"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml"} {
		prevLen := len(groups)
		checkAndParse(name, SourceTaskfile, ParseTaskfile)
		if len(groups) > prevLen {
			break
		}
	}

	// 4. justfile
	for _, name := range []string{"justfile", "Justfile", ".justfile"} {
		prevLen := len(groups)
		checkAndParse(name, SourceJustfile, ParseJustfile)
		if len(groups) > prevLen {
			break
		}
	}

	return groups, nil
}

// cleanYAMLValue trims quotes and comments from YAML scalar value.
func cleanYAMLValue(val string) string {
	val = strings.TrimSpace(val)
	if hashIdx := strings.Index(val, " #"); hashIdx != -1 {
		val = strings.TrimSpace(val[:hashIdx])
	}
	// Trim YAML block indicators if at beginning
	val = strings.TrimPrefix(val, "|")
	val = strings.TrimPrefix(val, ">")
	val = strings.TrimPrefix(val, "-")
	val = strings.TrimSpace(val)

	// If enclosed in matching quotes, strip them
	if len(val) >= 2 {
		if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
			(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
			val = val[1 : len(val)-1]
		}
	}
	return strings.TrimSpace(val)
}
