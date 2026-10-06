package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ServiceConfig holds the parsed definition of a single Docker Compose service.
type ServiceConfig struct {
	Name          string            `json:"name"`
	Image         string            `json:"image,omitempty"`
	Build         string            `json:"build,omitempty"`
	BuildContext  string            `json:"build_context,omitempty"`
	Dockerfile    string            `json:"dockerfile,omitempty"`
	ContainerName string            `json:"container_name,omitempty"`
	Ports         []string          `json:"ports,omitempty"`
	Environment   map[string]string `json:"environment,omitempty"`
	Volumes       []string          `json:"volumes,omitempty"`
	Command       string            `json:"command,omitempty"`
}

// ComposeProject represents a parsed docker compose configuration.
type ComposeProject struct {
	Version  string                   `json:"version,omitempty"`
	Services map[string]ServiceConfig `json:"services"`
	FilePath string                   `json:"file_path,omitempty"`
}

// CandidateComposeFiles defines the standard Compose file names checked in order of precedence.
var CandidateComposeFiles = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

// FindComposeFile searches for standard docker-compose files in dir.
func FindComposeFile(dir string) (string, error) {
	for _, name := range CandidateComposeFiles {
		candidate := filepath.Join(dir, name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no docker-compose file found in %s", dir)
}

// ParseComposeFile resolves either a compose file directly or searches within a workspace directory.
func ParseComposeFile(pathOrDir string) (*ComposeProject, error) {
	targetPath := pathOrDir
	fi, err := os.Stat(pathOrDir)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		found, err := FindComposeFile(pathOrDir)
		if err != nil {
			return nil, err
		}
		targetPath = found
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("reading compose file: %w", err)
	}

	project, err := ParseComposeYAML(string(content))
	if err != nil {
		return nil, err
	}
	project.FilePath = targetPath
	return project, nil
}

// yamlNode represents an AST node extracted from YAML text.
type yamlNode struct {
	Indent   int
	Key      string
	Value    string
	IsList   bool
	Children []*yamlNode
	Parent   *yamlNode
}

// ParseComposeYAML parses a docker-compose YAML string into a ComposeProject structure.
func ParseComposeYAML(content string) (*ComposeProject, error) {
	root := &yamlNode{Indent: -1}
	stack := []*yamlNode{root}

	lines := strings.Split(content, "\n")
	for lineIdx := 0; lineIdx < len(lines); lineIdx++ {
		rawLine := lines[lineIdx]
		trimmed := stripYAMLComment(rawLine)
		if strings.TrimSpace(trimmed) == "" {
			continue
		}

		indent := countLeadingSpaces(trimmed)
		lineContent := strings.TrimSpace(trimmed)

		// Pop stack until current top has indent strictly less than current line indent
		for len(stack) > 1 && stack[len(stack)-1].Indent >= indent {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]

		// Check if list item
		if strings.HasPrefix(lineContent, "-") {
			itemContent := strings.TrimSpace(strings.TrimPrefix(lineContent, "-"))
			node := &yamlNode{
				Indent: indent,
				IsList: true,
				Parent: parent,
			}

			// Check if list item has key-value e.g. "- target: 80" or "- POSTGRES_USER=app"
			if k, v, ok := splitKeyValue(itemContent); ok {
				node.Key = k
				node.Value = v
			} else {
				node.Value = itemContent
			}

			parent.Children = append(parent.Children, node)
			stack = append(stack, node)
			continue
		}

		// Key-value pair or section header
		k, v, ok := splitKeyValue(lineContent)
		if !ok {
			// Plain value without colon
			k = lineContent
			v = ""
		}

		node := &yamlNode{
			Indent: indent,
			Key:    k,
			Value:  v,
			Parent: parent,
		}
		parent.Children = append(parent.Children, node)
		stack = append(stack, node)
	}

	project := &ComposeProject{
		Services: make(map[string]ServiceConfig),
	}

	// 1. Check version
	for _, child := range root.Children {
		if strings.EqualFold(child.Key, "version") {
			project.Version = cleanValue(child.Value)
			break
		}
	}

	// 2. Find services node
	var servicesNode *yamlNode
	for _, child := range root.Children {
		if strings.EqualFold(child.Key, "services") {
			servicesNode = child
			break
		}
	}

	// If no "services:" top-level key, check Compose v1 style where root children are services
	targetNodes := root.Children
	if servicesNode != nil {
		targetNodes = servicesNode.Children
	}

	for _, sNode := range targetNodes {
		sName := cleanValue(sNode.Key)
		if sName == "" || strings.EqualFold(sName, "version") || strings.EqualFold(sName, "volumes") ||
			strings.EqualFold(sName, "networks") || strings.EqualFold(sName, "configs") || strings.EqualFold(sName, "secrets") {
			continue
		}

		svc := ServiceConfig{
			Name:        sName,
			Environment: make(map[string]string),
		}

		for _, prop := range sNode.Children {
			propKey := strings.ToLower(cleanValue(prop.Key))
			propVal := cleanValue(prop.Value)

			switch propKey {
			case "image":
				svc.Image = propVal

			case "container_name":
				svc.ContainerName = propVal

			case "build":
				if propVal != "" {
					svc.Build = propVal
					svc.BuildContext = propVal
				}
				for _, bChild := range prop.Children {
					bKey := strings.ToLower(cleanValue(bChild.Key))
					bVal := cleanValue(bChild.Value)
					if bKey == "context" {
						svc.BuildContext = bVal
						if svc.Build == "" {
							svc.Build = bVal
						}
					} else if bKey == "dockerfile" {
						svc.Dockerfile = bVal
					}
				}

			case "ports":
				if isInlineList(propVal) {
					svc.Ports = append(svc.Ports, parseInlineList(propVal)...)
				}
				for _, pChild := range prop.Children {
					if pChild.IsList {
						if len(pChild.Children) > 0 || (pChild.Key != "" && pChild.Value != "") {
							// Structured port mapping: target, published, protocol
							target := ""
							published := ""
							proto := "tcp"
							if pChild.Key == "target" {
								target = cleanValue(pChild.Value)
							} else if pChild.Key == "published" {
								published = cleanValue(pChild.Value)
							}
							for _, sub := range pChild.Children {
								subKey := strings.ToLower(cleanValue(sub.Key))
								subVal := cleanValue(sub.Value)
								if subKey == "target" {
									target = subVal
								} else if subKey == "published" {
									published = subVal
								} else if subKey == "protocol" {
									proto = subVal
								}
							}
							if published != "" && target != "" {
								svc.Ports = append(svc.Ports, fmt.Sprintf("%s:%s/%s", published, target, proto))
							} else if target != "" {
								svc.Ports = append(svc.Ports, target)
							}
						} else {
							val := cleanValue(pChild.Value)
							if val != "" {
								svc.Ports = append(svc.Ports, val)
							}
						}
					}
				}

			case "environment":
				// Could be map format (KEY: VAL) or list format (- KEY=VAL)
				for _, envChild := range prop.Children {
					if envChild.IsList {
						envVal := cleanValue(envChild.Value)
						if envChild.Key != "" && envChild.Value != "" {
							svc.Environment[cleanValue(envChild.Key)] = envVal
						} else if eqIdx := strings.Index(envVal, "="); eqIdx != -1 {
							k := strings.TrimSpace(envVal[:eqIdx])
							v := cleanValue(envVal[eqIdx+1:])
							svc.Environment[k] = v
						} else if envVal != "" {
							svc.Environment[envVal] = ""
						}
					} else if envChild.Key != "" {
						svc.Environment[cleanValue(envChild.Key)] = cleanValue(envChild.Value)
					}
				}

			case "volumes":
				if isInlineList(propVal) {
					svc.Volumes = append(svc.Volumes, parseInlineList(propVal)...)
				}
				for _, vChild := range prop.Children {
					if vChild.IsList {
						if len(vChild.Children) > 0 || (vChild.Key != "" && vChild.Value != "") {
							// Structured volume mapping: source, target
							source := ""
							target := ""
							if vChild.Key == "source" {
								source = cleanValue(vChild.Value)
							} else if vChild.Key == "target" {
								target = cleanValue(vChild.Value)
							}
							for _, sub := range vChild.Children {
								subKey := strings.ToLower(cleanValue(sub.Key))
								subVal := cleanValue(sub.Value)
								if subKey == "source" {
									source = subVal
								} else if subKey == "target" {
									target = subVal
								}
							}
							if source != "" && target != "" {
								svc.Volumes = append(svc.Volumes, fmt.Sprintf("%s:%s", source, target))
							}
						} else {
							val := cleanValue(vChild.Value)
							if val != "" {
								svc.Volumes = append(svc.Volumes, val)
							}
						}
					}
				}

			case "command":
				if propVal != "" {
					if isInlineList(propVal) {
						items := parseInlineList(propVal)
						svc.Command = strings.Join(items, " ")
					} else {
						svc.Command = propVal
					}
				} else if len(prop.Children) > 0 {
					var parts []string
					for _, cChild := range prop.Children {
						val := cleanValue(cChild.Value)
						if val != "" {
							parts = append(parts, val)
						}
					}
					svc.Command = strings.Join(parts, " ")
				}
			}
		}

		project.Services[sName] = svc
	}

	return project, nil
}

// countLeadingSpaces counts indentation in spaces (tabs count as 4 spaces).
func countLeadingSpaces(s string) int {
	spaces := 0
	for _, r := range s {
		if r == ' ' {
			spaces++
		} else if r == '\t' {
			spaces += 4
		} else {
			break
		}
	}
	return spaces
}

// stripYAMLComment removes comments starting with '#' outside quotes.
func stripYAMLComment(s string) string {
	inSingle := false
	inDouble := false
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\'' && !inDouble {
			inSingle = !inSingle
		} else if r == '"' && !inSingle {
			inDouble = !inDouble
		} else if r == '#' && !inSingle && !inDouble {
			return string(runes[:i])
		}
	}
	return s
}

// splitKeyValue splits a YAML line into key and value on the first unquoted colon followed by space or line end.
func splitKeyValue(line string) (string, string, bool) {
	inSingle := false
	inDouble := false
	runes := []rune(line)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\'' && !inDouble {
			inSingle = !inSingle
		} else if r == '"' && !inSingle {
			inDouble = !inDouble
		} else if r == ':' && !inSingle && !inDouble {
			// Colon must be followed by space, tab, or be at the end of the line
			if i+1 == len(runes) || unicode.IsSpace(runes[i+1]) {
				k := strings.TrimSpace(string(runes[:i]))
				v := strings.TrimSpace(string(runes[i+1:]))
				return k, v, true
			}
		}
	}
	return "", "", false
}

// cleanValue trims whitespace and strips enclosing single or double quotes.
func cleanValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	return strings.TrimSpace(s)
}

// isInlineList checks if string starts with '[' and ends with ']'.
func isInlineList(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]")
}

// parseInlineList parses comma-separated items inside '[ item1, "item2" ]'.
func parseInlineList(s string) []string {
	s = strings.TrimSpace(s)
	if !isInlineList(s) {
		return nil
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil
	}

	var items []string
	inSingle := false
	inDouble := false
	runes := []rune(inner)
	cur := strings.Builder{}

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\'' && !inDouble {
			inSingle = !inSingle
		} else if r == '"' && !inSingle {
			inDouble = !inDouble
		} else if r == ',' && !inSingle && !inDouble {
			item := cleanValue(cur.String())
			if item != "" {
				items = append(items, item)
			}
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}

	if cur.Len() > 0 {
		item := cleanValue(cur.String())
		if item != "" {
			items = append(items, item)
		}
	}

	return items
}
