package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"tahr/internal/core/logging"
)

// ReferenceGoPluginManifest returns the canonical tahr-go plugin definition.
func ReferenceGoPluginManifest() Manifest {
	return Manifest{
		ID:          "tahr-go",
		Name:        "Go Language Support",
		Version:     "1.0.0",
		Author:      "Tahr Team",
		Description: "Language Server (gopls), Delve debugger, launch templates and SDK autodetection",
		Capabilities: []string{"fs:read", "process:exec"},
		Toolchain: &ToolchainConfig{
			Name:       "Go",
			DynamicSDK: "go",
		},
		Languages: []LanguageConfig{
			{
				ID:           "go",
				Name:         "Go",
				Extensions:   []string{".go"},
				Filenames:    []string{"go.mod", "go.sum", "go.work"},
				CommentToken: "//",
				BuildCmd:     "go",
				BuildArgs:    []string{"build", "./..."},
				RunCmd:       "go",
				RunArgs:      []string{"run", "."},
				InstallCmd:   "go install golang.org/x/tools/gopls@latest",
			},
		},
		LSP: &LSPConfig{
			ServerName:  "gopls",
			Command:     "gopls",
			Args:        []string{},
			RootMarkers: []string{"go.mod", "go.work", ".git"},
			InstallCmd:  "go install golang.org/x/tools/gopls@latest",
		},
		DAP: &DAPConfig{
			AdapterName: "delve",
			Command:     "dlv",
			Args:        []string{"dap"},
			InstallCmd:  "go install github.com/go-delve/delve/cmd/dlv@latest",
		},
		LaunchTemplates: []LaunchTemplate{
			{
				Name:          "Go: Run Package",
				Type:          "go",
				Request:       "launch",
				Target:        ".",
				Args:          []string{},
				Env:           map[string]string{"CGO_ENABLED": "0"},
				PreLaunchTask: "go vet ./...",
			},
			{
				Name:    "Go: Debug Test",
				Type:    "go",
				Request: "debug",
				Target:  ".",
				Args:    []string{"-test.v"},
			},
			{
				Name:    "Go: Build",
				Type:    "go",
				Request: "launch",
				Target:  "build",
				Args:    []string{"-v", "./..."},
			},
		},
		ProjectTemplates: []ProjectTemplate{
			{
				ID:            "go",
				Name:          "Go (Standard Module)",
				Description:   "Standard Go module with main package, go.mod, and .gitignore",
				Category:      "Go",
				Icon:          "",
				DefaultModule: "example.com/{{.ProjectName}}",
				Files: []ProjectTemplateFile{
					{
						Path:    "go.mod",
						Content: "module {{.ModulePath}}\n\ngo 1.22\n",
					},
					{
						Path: "main.go",
						Content: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello from {{.ProjectName}}!\")\n}\n",
					},
					{
						Path:    ".gitignore",
						Content: "bin/\n*.exe\n*.test\nvendor/\n",
					},
					{
						Path:    "README.md",
						Content: "# {{.ProjectName}}\n\nA Go project.\n",
					},
				},
			},
			{
				ID:            "go-cli",
				Name:          "Go (CLI Application)",
				Description:   "Command-line application layout with cmd/ and internal/ packages",
				Category:      "Go",
				Icon:          "",
				DefaultModule: "example.com/{{.ProjectName}}",
				Files: []ProjectTemplateFile{
					{
						Path:    "go.mod",
						Content: "module {{.ModulePath}}\n\ngo 1.22\n",
					},
					{
						Path:    "cmd/{{.ProjectName}}/main.go",
						Content: "package main\n\nimport (\n\t\"flag\"\n\t\"fmt\"\n)\n\nfunc main() {\n\tname := flag.String(\"name\", \"world\", \"name to greet\")\n\tflag.Parse()\n\tfmt.Printf(\"Hello, %s! Welcome to {{.ProjectName}}.\\n\", *name)\n}\n",
					},
					{
						Path:    "internal/app/app.go",
						Content: "package app\n\n// Run executes application logic.\nfunc Run() error {\n\treturn nil\n}\n",
					},
					{
						Path:    ".gitignore",
						Content: "bin/\n*.exe\n*.test\nvendor/\n",
					},
					{
						Path:    "README.md",
						Content: "# {{.ProjectName}} CLI\n\nCommand-line tool built with Go.\n",
					},
				},
			},
		},
		SDKAutodetect: &SDKAutodetectConfig{
			Binaries: []string{"go", "gopls", "dlv"},
			WindowsPaths: []string{
				`D:\go\sdk\*`,
				`C:\Program Files\Go\bin`,
				`C:\Go\bin`,
				`%USERPROFILE%\go\bin`,
			},
			UnixPaths: []string{
				"/usr/local/go/bin",
				"/usr/bin",
				"/opt/homebrew/bin",
				"~/go/bin",
			},
		},
	}
}

// EnsureReferenceGoPlugin installs the unpacked tahr-go plugin into <workspaceDir>/.tahr/plugins/tahr-go/
// and packs the reference archive into ~/.config/tahr/packages/tahr-go-1.0.0.tahr.
func EnsureReferenceGoPlugin(workspaceDir string) error {
	m := ReferenceGoPluginManifest()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	// 1. Write unpacked plugin to workspace if workspaceDir is valid
	if workspaceDir != "" {
		targetDir := filepath.Join(workspaceDir, ".tahr", "plugins", "tahr-go")
		_ = os.MkdirAll(targetDir, 0755)
		manifestPath := filepath.Join(targetDir, "plugin.json")
		_ = os.WriteFile(manifestPath, data, 0644)
		readmePath := filepath.Join(targetDir, "README.md")
		if _, err := os.Stat(readmePath); os.IsNotExist(err) {
			_ = os.WriteFile(readmePath, []byte("# Go Language Support\n\nReference plugin for Tahr IDE.\n"), 0644)
		}
	}

	// 2. Package into ~/.config/tahr/packages/tahr-go-1.0.0.tahr
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	packagesDir := filepath.Join(home, ".config", "tahr", "packages")
	_ = os.MkdirAll(packagesDir, 0755)
	destTahr := filepath.Join(packagesDir, fmt.Sprintf("%s-%s.tahr", m.ID, m.Version))

	// Create temporary staging directory to pack
	tempStaging, err := os.MkdirTemp("", "tahr-go-pack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempStaging)

	_ = os.WriteFile(filepath.Join(tempStaging, "plugin.json"), data, 0644)
	_ = os.WriteFile(filepath.Join(tempStaging, "README.md"), []byte("# Go Language Support\n\nReference plugin for Tahr IDE.\n"), 0644)

	if err := PackDirectory(tempStaging, destTahr); err != nil {
		logging.Warn("Failed packing reference tahr-go package", "err", err)
		return err
	}

	logging.Info("Created reference tahr-go package", "dest", destTahr)
	return nil
}
