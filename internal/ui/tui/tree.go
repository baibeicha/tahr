package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileNode represents a single item (file or directory) in the project tree.
type FileNode struct {
	Name     string
	Path     string
	IsDir    bool
	IsOpen   bool
	Depth    int
	Children []*FileNode
}

// BuildProjectTree constructs a hierarchical directory tree starting at rootPath.
func BuildProjectTree(rootPath string, maxDepth int) *FileNode {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		absRoot = rootPath
	}
	rootName := filepath.Base(absRoot)
	if rootName == "." || rootName == "" || rootName == string(filepath.Separator) {
		rootName = "WORKSPACE"
	}

	rootNode := &FileNode{
		Name:     rootName,
		Path:     absRoot,
		IsDir:    true,
		IsOpen:   true,
		Depth:    0,
		Children: make([]*FileNode, 0),
	}

	buildTreeRecursive(rootNode, 1, maxDepth)
	return rootNode
}

var ignoredTreeNames = map[string]bool{
	".git":         true,
	".svn":         true,
	".hg":          true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"bin":          true,
	"obj":          true,
	".tahr_cache":  true,
	".agents":      true,
	".gemini":      true,
	".idea":        true,
	".vscode":      true,
}

func buildTreeRecursive(parent *FileNode, currentDepth, maxDepth int) {
	if currentDepth > maxDepth {
		return
	}

	entries, err := os.ReadDir(parent.Path)
	if err != nil {
		return
	}

	var dirs []*FileNode
	var files []*FileNode

	for _, entry := range entries {
		name := entry.Name()
		if ignoredTreeNames[name] || (strings.HasPrefix(name, ".") && name != ".") {
			continue
		}

		fullPath := filepath.Join(parent.Path, name)
		node := &FileNode{
			Name:  name,
			Path:  fullPath,
			IsDir: entry.IsDir(),
			Depth: currentDepth,
		}

		if node.IsDir {
			// Auto-open first level subdirectories that are common project folders (cmd, internal, pkg, src)
			if currentDepth == 1 && (name == "cmd" || name == "internal" || name == "pkg" || name == "src") {
				node.IsOpen = true
			}
			buildTreeRecursive(node, currentDepth+1, maxDepth)
			dirs = append(dirs, node)
		} else {
			files = append(files, node)
		}
	}

	// Sort directories alphabetically, then files alphabetically
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	parent.Children = append(dirs, files...)
}

// FlattenTree flattens the tree hierarchy into a linear slice of visible nodes.
func FlattenTree(node *FileNode, list *[]*FileNode) {
	if node == nil {
		return
	}
	*list = append(*list, node)
	if node.IsDir && node.IsOpen {
		for _, child := range node.Children {
			FlattenTree(child, list)
		}
	}
}

// GetFileIconWithStyle returns an icon styled according to file_icon_style ("minimal", "unicode", "nerd_fonts").
func GetFileIconWithStyle(node *FileNode, style string) string {
	if node == nil {
		return ""
	}
	if style == "nerd_fonts" {
		if node.IsDir {
			if node.IsOpen {
				return " "
			}
			return " "
		}
		ext := strings.ToLower(filepath.Ext(node.Name))
		switch ext {
		case ".go":
			return " "
		case ".py":
			return " "
		case ".rs":
			return " "
		case ".md":
			return " "
		case ".json", ".yaml", ".yml", ".toml":
			return " "
		case ".js", ".ts", ".jsx", ".tsx":
			return " "
		case ".html", ".css":
			return " "
		default:
			return " "
		}
	}

	// Clean minimal / unicode: strict triangular folder glyphs and typographical symbols
	if node.IsDir {
		if node.IsOpen {
			return "▾ "
		}
		return "▸ "
	}
	name := strings.ToLower(node.Name)
	ext := strings.ToLower(filepath.Ext(name))

	switch {
	case ext == ".go":
		return "◆ "
	case ext == ".py":
		return "◇ "
	case ext == ".rs":
		return "◈ "
	case ext == ".md":
		return "≡ "
	case ext == ".json" || ext == ".yaml" || ext == ".yml" || ext == ".toml":
		return "▪ "
	case ext == ".sh" || ext == ".bat" || ext == ".ps1":
		return "› "
	case ext == ".js" || ext == ".ts" || ext == ".jsx" || ext == ".tsx":
		return "◇ "
	case ext == ".html" || ext == ".css":
		return "◈ "
	case name == "license" || strings.HasPrefix(name, "license."):
		return "§ "
	case strings.HasPrefix(name, "docker"):
		return "▫ "
	case name == "go.mod" || name == "go.sum" || name == "cargo.toml":
		return "▪ "
	default:
		return "· "
	}
}

// GetFileIcon returns a clean, typographical visual glyph for a file or directory.
func GetFileIcon(node *FileNode) string {
	return GetFileIconWithStyle(node, "minimal")
}

// CreateNewFile creates an empty file at path.
func CreateNewFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

// CreateNewDir creates a new directory at path.
func CreateNewDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// RenamePath renames an existing file or directory.
func RenamePath(oldPath, newPath string) error {
	dir := filepath.Dir(newPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}

// DeletePath deletes a file or directory.
func DeletePath(path string) error {
	return os.RemoveAll(path)
}

// MovePath moves a file or directory into a destination directory.
func MovePath(srcPath, destDir string) error {
	base := filepath.Base(srcPath)
	target := filepath.Join(destDir, base)
	return RenamePath(srcPath, target)
}

// ScanWorkspaceFiles gathers candidate files for Omnibar fuzzy search.
func ScanWorkspaceFiles(root string, maxFiles int) []string {
	var files []string
	ignoredDirs := map[string]bool{
		".git":         true,
		".svn":         true,
		".hg":          true,
		"node_modules": true,
		"vendor":       true,
		"dist":         true,
		"build":        true,
		"bin":          true,
		".bin":         true,
		".cache":       true,
		"target":       true,
		"obj":          true,
		".tahr_cache":  true,
		".agents":      true,
		".gemini":      true,
		".idea":        true,
		".vscode":      true,
	}

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if ignoredDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err == nil {
			files = append(files, filepath.ToSlash(rel))
			if len(files) >= maxFiles {
				return filepath.SkipDir
			}
		}
		return nil
	})

	return files
}

