package taskrunner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMakefile(t *testing.T) {
	content := `
# Global variables
CC := gcc
CFLAGS = -Wall -O2
BIN ?= myapp

.PHONY: all build test lint clean .INTERNAL

all: build

# Build the main binary
build: deps
	$(CC) $(CFLAGS) -o $(BIN) main.c

test: ## Run unit tests
	go test -v ./...

lint:
	golangci-lint run

deploy staging: ## Deploy to environment
	./deploy.sh

clean:
	rm -rf $(BIN)

%.o: %.c
	$(CC) -c $<
`

	tasks, err := ParseMakefile([]byte(content), "Makefile")
	if err != nil {
		t.Fatalf("unexpected error parsing Makefile: %v", err)
	}

	expectedTasks := map[string]string{
		"all":     "",
		"build":   "Build the main binary",
		"test":    "Run unit tests",
		"lint":    "",
		"deploy":  "Deploy to environment",
		"staging": "Deploy to environment",
		"clean":   "",
	}

	if len(tasks) != len(expectedTasks) {
		t.Fatalf("expected %d tasks, got %d", len(expectedTasks), len(tasks))
	}

	taskMap := make(map[string]Task)
	for _, task := range tasks {
		taskMap[task.Name] = task
		if task.Source != SourceMakefile {
			t.Errorf("task %s expected source Makefile, got %s", task.Name, task.Source)
		}
		if task.Command != "make "+task.Name {
			t.Errorf("task %s expected command 'make %s', got '%s'", task.Name, task.Name, task.Command)
		}
		if len(task.Args) != 2 || task.Args[0] != "make" || task.Args[1] != task.Name {
			t.Errorf("task %s has unexpected args: %v", task.Name, task.Args)
		}
	}

	for name, expectedDesc := range expectedTasks {
		task, ok := taskMap[name]
		if !ok {
			t.Errorf("expected task %q not found", name)
			continue
		}
		if expectedDesc != "" && task.Description != expectedDesc {
			t.Errorf("task %q description mismatch: expected %q, got %q", name, expectedDesc, task.Description)
		}
	}

	// Verify .PHONY, %.o, and variables are NOT present
	if _, ok := taskMap[".PHONY"]; ok {
		t.Errorf(".PHONY should be ignored")
	}
	if _, ok := taskMap[".INTERNAL"]; ok {
		t.Errorf(".INTERNAL should be ignored")
	}
	if _, ok := taskMap["%.o"]; ok {
		t.Errorf("pattern rule %%.o should be ignored")
	}
	if _, ok := taskMap["CC"]; ok {
		t.Errorf("variable CC should be ignored")
	}
}

func TestParsePackageJSON(t *testing.T) {
	content := `{
  "name": "tahr-workspace",
  "version": "1.0.0",
  "scripts": {
    "dev": "vite --port 3000",
    "build": "tsc && vite build",
    "test": "vitest run --coverage",
    "lint": "eslint . --ext .ts,.tsx"
  },
  "dependencies": {
    "react": "^19.0.0"
  }
}`

	tasks, err := ParsePackageJSON([]byte(content), "package.json")
	if err != nil {
		t.Fatalf("unexpected error parsing package.json: %v", err)
	}

	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(tasks))
	}

	expected := []struct {
		name string
		desc string
	}{
		{"dev", "vite --port 3000"},
		{"build", "tsc && vite build"},
		{"test", "vitest run --coverage"},
		{"lint", "eslint . --ext .ts,.tsx"},
	}

	for i, exp := range expected {
		if tasks[i].Name != exp.name {
			t.Errorf("index %d expected name %q, got %q", i, exp.name, tasks[i].Name)
		}
		if tasks[i].Description != exp.desc {
			t.Errorf("task %q expected description %q, got %q", exp.name, exp.desc, tasks[i].Description)
		}
		if tasks[i].Command != "npm run "+exp.name {
			t.Errorf("task %q expected command 'npm run %s', got '%s'", exp.name, exp.name, tasks[i].Command)
		}
		if tasks[i].Source != SourcePackageJSON {
			t.Errorf("task %q expected source package.json, got %s", exp.name, tasks[i].Source)
		}
	}
}

func TestParseTaskfile(t *testing.T) {
	content := `
version: '3'

vars:
  PROJECT: tahr

tasks:
  default:
    cmds:
      - task: build

  build:
    desc: Build executable binary
    summary: Compiles the Go source into bin/
    cmds:
      - go build -v ./...

  test:
    desc: Run unit and integration tests
    cmds:
      - go test -v ./...

  lint:
    summary: Run static code analysis
    cmds:
      - golangci-lint run

  quick: echo "quick task"
`

	tasks, err := ParseTaskfile([]byte(content), "Taskfile.yml")
	if err != nil {
		t.Fatalf("unexpected error parsing Taskfile: %v", err)
	}

	expectedTasks := map[string]string{
		"default": "",
		"build":   "Build executable binary",
		"test":    "Run unit and integration tests",
		"lint":    "Run static code analysis",
		"quick":   `echo "quick task"`,
	}

	if len(tasks) != len(expectedTasks) {
		t.Fatalf("expected %d tasks, got %d", len(expectedTasks), len(tasks))
	}

	for _, task := range tasks {
		expectedDesc, exists := expectedTasks[task.Name]
		if !exists {
			t.Errorf("unexpected task found: %s", task.Name)
			continue
		}
		if expectedDesc != "" && task.Description != expectedDesc {
			t.Errorf("task %s expected description %q, got %q", task.Name, expectedDesc, task.Description)
		}
		if task.Command != "task "+task.Name {
			t.Errorf("task %s expected command 'task %s', got '%s'", task.Name, task.Name, task.Command)
		}
		if task.Source != SourceTaskfile {
			t.Errorf("task %s expected source Taskfile, got %s", task.Name, task.Source)
		}
	}
}

func TestParseJustfile(t *testing.T) {
	content := `
# Global variables and settings
set shell := ["bash", "-c"]
export VAR := "value"
alias b := build

# Default recipe listing tasks
default:
	@just --list

# Build project with optimizations
build target="release":
	cargo build --{{target}}

# Run test suite
test *args:
	cargo test {{args}}

@check: ## Quick check
	cargo check

[private]
_hidden:
	echo "should not be discovered"

_internal_recipe:
	echo "also private"
`

	tasks, err := ParseJustfile([]byte(content), "justfile")
	if err != nil {
		t.Fatalf("unexpected error parsing justfile: %v", err)
	}

	expectedTasks := map[string]string{
		"default": "Default recipe listing tasks",
		"build":   "Build project with optimizations",
		"test":    "Run test suite",
		"check":   "Quick check",
	}

	if len(tasks) != len(expectedTasks) {
		t.Fatalf("expected %d tasks, got %d", len(expectedTasks), len(tasks))
	}

	for _, task := range tasks {
		expectedDesc, exists := expectedTasks[task.Name]
		if !exists {
			t.Errorf("unexpected recipe found: %s", task.Name)
			continue
		}
		if expectedDesc != "" && task.Description != expectedDesc {
			t.Errorf("recipe %s expected description %q, got %q", task.Name, expectedDesc, task.Description)
		}
		if task.Command != "just "+task.Name {
			t.Errorf("recipe %s expected command 'just %s', got '%s'", task.Name, task.Name, task.Command)
		}
		if task.Source != SourceJustfile {
			t.Errorf("recipe %s expected source justfile, got %s", task.Name, task.Source)
		}
	}
}

func TestDiscoverTasks(t *testing.T) {
	tmpDir := t.TempDir()

	// Write mock Makefile
	makefileContent := `
all: build
build:
	go build
test:
	go test
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Makefile"), []byte(makefileContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Write mock package.json
	packageJSONContent := `{
  "scripts": {
    "start": "node server.js",
    "test": "jest"
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSONContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Write mock Taskfile.yml
	taskfileContent := `
version: '3'
tasks:
  compile:
    desc: Compile assets
    cmds:
      - esbuild app.js
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Taskfile.yml"), []byte(taskfileContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Write mock justfile
	justfileContent := `
# Build recipe
build:
	cargo build
`
	if err := os.WriteFile(filepath.Join(tmpDir, "justfile"), []byte(justfileContent), 0644); err != nil {
		t.Fatal(err)
	}

	groups, err := DiscoverTasks(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverTasks failed: %v", err)
	}

	if len(groups) != 4 {
		t.Fatalf("expected 4 task groups, got %d", len(groups))
	}

	groupTitles := make(map[string]string)
	for _, g := range groups {
		groupTitles[g.SourceFile] = g.Title()
	}

	if title, ok := groupTitles["Makefile"]; !ok || title != "Makefile (3 tasks)" {
		t.Errorf("expected 'Makefile (3 tasks)', got %q", title)
	}
	if title, ok := groupTitles["package.json"]; !ok || title != "package.json (2 tasks)" {
		t.Errorf("expected 'package.json (2 tasks)', got %q", title)
	}
	if title, ok := groupTitles["Taskfile.yml"]; !ok || title != "Taskfile.yml (1 task)" {
		t.Errorf("expected 'Taskfile.yml (1 task)', got %q", title)
	}
	if title, ok := groupTitles["justfile"]; !ok || title != "justfile (1 task)" {
		t.Errorf("expected 'justfile (1 task)', got %q", title)
	}
}

func TestEdgeCasesAndEmpty(t *testing.T) {
	// Empty content tests
	t.Run("Empty Makefile", func(t *testing.T) {
		tasks, err := ParseMakefile([]byte(""), "Makefile")
		if err != nil || len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d, err: %v", len(tasks), err)
		}
	})

	t.Run("Makefile only .PHONY and comments", func(t *testing.T) {
		content := `
# Just comments
.PHONY: build test
include other.mk
ifeq ($(OS),Windows_NT)
endif
`
		tasks, err := ParseMakefile([]byte(content), "Makefile")
		if err != nil || len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d, err: %v", len(tasks), err)
		}
	})

	t.Run("Empty package.json scripts", func(t *testing.T) {
		content := `{"name": "test"}`
		tasks, err := ParsePackageJSON([]byte(content), "package.json")
		if err != nil || len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d, err: %v", len(tasks), err)
		}
	})

	t.Run("Invalid package.json", func(t *testing.T) {
		content := `invalid json`
		_, err := ParsePackageJSON([]byte(content), "package.json")
		if err == nil {
			t.Errorf("expected error for invalid json, got nil")
		}
	})

	t.Run("Empty Taskfile", func(t *testing.T) {
		content := `version: '3'`
		tasks, err := ParseTaskfile([]byte(content), "Taskfile.yml")
		if err != nil || len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d, err: %v", len(tasks), err)
		}
	})

	t.Run("Empty justfile", func(t *testing.T) {
		content := `# only comments and variables
VAR := 1
export B := 2
`
		tasks, err := ParseJustfile([]byte(content), "justfile")
		if err != nil || len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d, err: %v", len(tasks), err)
		}
	})

	t.Run("Discover in non-existent directory", func(t *testing.T) {
		groups, err := DiscoverTasks(filepath.Join(os.TempDir(), "nonexistent-dir-for-tasks-test"))
		if err != nil || len(groups) != 0 {
			t.Errorf("expected 0 groups, got %d, err: %v", len(groups), err)
		}
	})
}
