package todotree

import (
	"fmt"
	"strings"
)

// Priority levels for detected TODO items.
type Priority int

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// String returns human-readable priority name.
func (p Priority) String() string {
	switch p {
	case PriorityCritical:
		return "CRITICAL"
	case PriorityHigh:
		return "HIGH"
	case PriorityNormal:
		return "NORMAL"
	default:
		return "LOW"
	}
}

// TodoItem represents a discovered code annotation/todo comment.
type TodoItem struct {
	Tag      string   `json:"tag"`       // "TODO", "FIXME", "BUG", "HACK", "NOTE", "PERF"
	Message  string   `json:"message"`   // Comment body after the tag
	Author   string   `json:"author"`    // Optional author from tag, e.g. "TODO(john):" -> "john"
	FilePath string   `json:"file_path"` // Relative or absolute file path
	Line     int      `json:"line"`      // 1-based line number
	Column   int      `json:"column"`    // 1-based character column
	Priority Priority `json:"priority"`  // Categorized severity
	RawLine  string   `json:"raw_line"`  // Full trimmed source line
}

// TagPriority assigns standard severity priority based on tag convention.
func TagPriority(tag string) Priority {
	switch strings.ToUpper(strings.TrimSpace(tag)) {
	case "BUG", "CRITICAL":
		return PriorityCritical
	case "FIXME", "XXX":
		return PriorityHigh
	case "TODO", "HACK":
		return PriorityNormal
	case "NOTE", "PERF", "OPTIMIZE", "REVIEW":
		return PriorityLow
	default:
		return PriorityNormal
	}
}

// FormatDisplay returns a clean developer string for list views.
func (item *TodoItem) FormatDisplay() string {
	authorPart := ""
	if item.Author != "" {
		authorPart = fmt.Sprintf("(%s)", item.Author)
	}
	return fmt.Sprintf("%s%s: %s [%s:%d]", item.Tag, authorPart, item.Message, item.FilePath, item.Line)
}
