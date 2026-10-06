package todotree

import (
	"sort"
	"strings"
)

// FileGroup groups todos belonging to the same file.
type FileGroup struct {
	FilePath string     `json:"file_path"`
	Items    []TodoItem `json:"items"`
}

// TagGroup groups todos with the same tag (e.g. all TODOs, all FIXMEs).
type TagGroup struct {
	Tag      string     `json:"tag"`
	Priority Priority   `json:"priority"`
	Items    []TodoItem `json:"items"`
}

// TodoStats summarizes discovered items.
type TodoStats struct {
	TotalCount      int              `json:"total_count"`
	CountByTag      map[string]int   `json:"count_by_tag"`
	CountByPriority map[Priority]int `json:"count_by_priority"`
}

// CalculateStats produces aggregate metrics over TODO items.
func CalculateStats(items []TodoItem) TodoStats {
	stats := TodoStats{
		TotalCount:      len(items),
		CountByTag:      make(map[string]int),
		CountByPriority: make(map[Priority]int),
	}

	for _, item := range items {
		stats.CountByTag[item.Tag]++
		stats.CountByPriority[item.Priority]++
	}

	return stats
}

// GroupByFile groups items by file path preserving file sort order.
func GroupByFile(items []TodoItem) []*FileGroup {
	groupMap := make(map[string]*FileGroup)
	var orderedFiles []string

	for _, item := range items {
		g, exists := groupMap[item.FilePath]
		if !exists {
			g = &FileGroup{FilePath: item.FilePath}
			groupMap[item.FilePath] = g
			orderedFiles = append(orderedFiles, item.FilePath)
		}
		g.Items = append(g.Items, item)
	}

	sort.Strings(orderedFiles)
	result := make([]*FileGroup, len(orderedFiles))
	for i, file := range orderedFiles {
		result[i] = groupMap[file]
	}

	return result
}

// GroupByTag groups items by tag name, sorted by priority (Critical down to Low).
func GroupByTag(items []TodoItem) []*TagGroup {
	groupMap := make(map[string]*TagGroup)
	var tags []string

	for _, item := range items {
		tag := item.Tag
		g, exists := groupMap[tag]
		if !exists {
			g = &TagGroup{
				Tag:      tag,
				Priority: TagPriority(tag),
			}
			groupMap[tag] = g
			tags = append(tags, tag)
		}
		g.Items = append(g.Items, item)
	}

	// Sort tags by Priority descending, then alphabetically
	sort.Slice(tags, func(i, j int) bool {
		pi := groupMap[tags[i]].Priority
		pj := groupMap[tags[j]].Priority
		if pi != pj {
			return pi > pj
		}
		return tags[i] < tags[j]
	})

	result := make([]*TagGroup, len(tags))
	for i, tag := range tags {
		result[i] = groupMap[tag]
	}

	return result
}

// FilterOptions defines search and filter parameters.
type FilterOptions struct {
	Query       string   // Substring search in message or file
	Tags        []string // Allowed tags (empty = all)
	Author      string   // Filter by author name
	MinPriority Priority // Minimum severity priority
}

// Filter filters items based on query, tags, author, and priority.
func Filter(items []TodoItem, opts FilterOptions) []TodoItem {
	var allowedTags map[string]bool
	if len(opts.Tags) > 0 {
		allowedTags = make(map[string]bool)
		for _, t := range opts.Tags {
			allowedTags[strings.ToUpper(strings.TrimSpace(t))] = true
		}
	}

	queryLower := strings.ToLower(strings.TrimSpace(opts.Query))
	authorLower := strings.ToLower(strings.TrimSpace(opts.Author))

	var filtered []TodoItem
	for _, it := range items {
		// Priority filter
		if it.Priority < opts.MinPriority {
			continue
		}

		// Tag filter
		if allowedTags != nil && !allowedTags[it.Tag] {
			continue
		}

		// Author filter
		if authorLower != "" && !strings.Contains(strings.ToLower(it.Author), authorLower) {
			continue
		}

		// Query filter
		if queryLower != "" {
			inMsg := strings.Contains(strings.ToLower(it.Message), queryLower)
			inFile := strings.Contains(strings.ToLower(it.FilePath), queryLower)
			inTag := strings.Contains(strings.ToLower(it.Tag), queryLower)
			if !inMsg && !inFile && !inTag {
				continue
			}
		}

		filtered = append(filtered, it)
	}

	return filtered
}

// TreeNode represents a node in a hierarchical TUI tree view.
type TreeNode struct {
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	IsLeaf   bool        `json:"is_leaf"`
	Item     *TodoItem   `json:"item,omitempty"`
	Children []*TreeNode `json:"children,omitempty"`
	Expanded bool        `json:"expanded"`
	Badge    string      `json:"badge,omitempty"`
}

// BuildFileTree constructs a 2-level tree (File -> Todo items).
func BuildFileTree(items []TodoItem) []*TreeNode {
	groups := GroupByFile(items)
	nodes := make([]*TreeNode, len(groups))

	for i, g := range groups {
		fileNode := &TreeNode{
			ID:       g.FilePath,
			Label:    g.FilePath,
			IsLeaf:   false,
			Expanded: true,
			Badge:    strings.TrimSpace(string(rune('0' + len(g.Items)))),
		}

		for j, it := range g.Items {
			itemCopy := it
			label := it.Tag
			if it.Author != "" {
				label += "(" + it.Author + ")"
			}
			label += ": " + it.Message

			fileNode.Children = append(fileNode.Children, &TreeNode{
				ID:     strings.TrimSpace(g.FilePath) + ":" + strings.TrimSpace(it.RawLine) + string(rune('0'+j)),
				Label:  label,
				IsLeaf: true,
				Item:   &itemCopy,
				Badge:  it.Tag,
			})
		}

		nodes[i] = fileNode
	}

	return nodes
}

// BuildTagTree constructs a 2-level tree (Tag -> Todo items).
func BuildTagTree(items []TodoItem) []*TreeNode {
	groups := GroupByTag(items)
	nodes := make([]*TreeNode, len(groups))

	for i, g := range groups {
		tagNode := &TreeNode{
			ID:       g.Tag,
			Label:    g.Tag,
			IsLeaf:   false,
			Expanded: true,
			Badge:    g.Priority.String(),
		}

		for j, it := range g.Items {
			itemCopy := it
			label := it.FilePath
			if it.Author != "" {
				label += " (" + it.Author + ")"
			}
			label += " - " + it.Message

			tagNode.Children = append(tagNode.Children, &TreeNode{
				ID:     g.Tag + ":" + it.FilePath + ":" + string(rune('0'+j)),
				Label:  label,
				IsLeaf: true,
				Item:   &itemCopy,
				Badge:  it.FilePath,
			})
		}

		nodes[i] = tagNode
	}

	return nodes
}
