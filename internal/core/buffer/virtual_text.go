package buffer

import (
	"sort"
	"sync"
)

// AnnotationKind distinguishes between different visual virtual text presentations.
type AnnotationKind int

const (
	// AnnotationInlay represents inline text inserted at a specific column (e.g., parameter names, inferred types).
	AnnotationInlay AnnotationKind = iota
	// AnnotationEndOfLine represents annotations appended to the end of the line (e.g., Git Inline Blame, DAP variable inspection).
	AnnotationEndOfLine
	// AnnotationConceal replaces a range of real characters with masked text (e.g., •••••••• for .env secrets).
	AnnotationConceal
)

// VirtualAnnotation defines a virtual decoration that does not modify the underlying document bytes.
type VirtualAnnotation struct {
	ID       string         `json:"id"`
	Source   string         `json:"source"` // "lsp", "git", "env", "dap", etc.
	Kind     AnnotationKind `json:"kind"`
	Line     int            `json:"line"`    // 0-based document line
	Col      int            `json:"col"`     // 0-based visual column
	EndCol   int            `json:"end_col"` // 0-based end column (for Conceal)
	Text     string         `json:"text"`    // Inlay text, Blame string, or Conceal mask
	FgColor  uint32         `json:"fg_color"`
	BgColor  uint32         `json:"bg_color"`
	Priority int            `json:"priority"` // Higher priority renders first
}

// VirtualTextManager coordinates virtual text decorations attached to a document.
type VirtualTextManager struct {
	mu          sync.RWMutex
	sourceItems map[string][]VirtualAnnotation
	byLine      map[int][]VirtualAnnotation
}

// NewVirtualTextManager creates an empty virtual text decoration manager.
func NewVirtualTextManager() *VirtualTextManager {
	return &VirtualTextManager{
		sourceItems: make(map[string][]VirtualAnnotation),
		byLine:      make(map[int][]VirtualAnnotation),
	}
}

// SetAnnotations replaces all annotations for a specific source (e.g., "git", "lsp").
func (vm *VirtualTextManager) SetAnnotations(source string, annotations []VirtualAnnotation) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Update source items
	if len(annotations) == 0 {
		delete(vm.sourceItems, source)
	} else {
		copied := make([]VirtualAnnotation, len(annotations))
		copy(copied, annotations)
		vm.sourceItems[source] = copied
	}

	vm.rebuildLineIndexLocked()
}

// ClearSource removes all annotations contributed by source.
func (vm *VirtualTextManager) ClearSource(source string) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	delete(vm.sourceItems, source)
	vm.rebuildLineIndexLocked()
}

// ClearAll removes all virtual annotations across all sources.
func (vm *VirtualTextManager) ClearAll() {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	vm.sourceItems = make(map[string][]VirtualAnnotation)
	vm.byLine = make(map[int][]VirtualAnnotation)
}

// GetLineAnnotations returns all annotations for line, sorted by column and priority.
func (vm *VirtualTextManager) GetLineAnnotations(line int) []VirtualAnnotation {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	items, ok := vm.byLine[line]
	if !ok || len(items) == 0 {
		return nil
	}

	out := make([]VirtualAnnotation, len(items))
	copy(out, items)
	return out
}

// HasAnnotations checks whether any virtual annotations exist on line.
func (vm *VirtualTextManager) HasAnnotations(line int) bool {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	items, ok := vm.byLine[line]
	return ok && len(items) > 0
}

// AdjustOnLineDelta adjusts line numbers when lines are inserted (delta > 0) or deleted (delta < 0).
func (vm *VirtualTextManager) AdjustOnLineDelta(startLine, lineDelta int) {
	if lineDelta == 0 {
		return
	}

	vm.mu.Lock()
	defer vm.mu.Unlock()

	for src, items := range vm.sourceItems {
		var updated []VirtualAnnotation
		for _, ann := range items {
			if ann.Line < startLine {
				updated = append(updated, ann)
			} else if lineDelta < 0 && ann.Line >= startLine && ann.Line < startLine-lineDelta {
				// Line was deleted: drop annotation
				continue
			} else {
				// Line shifted
				ann.Line += lineDelta
				updated = append(updated, ann)
			}
		}
		vm.sourceItems[src] = updated
	}

	vm.rebuildLineIndexLocked()
}

func (vm *VirtualTextManager) rebuildLineIndexLocked() {
	vm.byLine = make(map[int][]VirtualAnnotation)

	for _, items := range vm.sourceItems {
		for _, ann := range items {
			vm.byLine[ann.Line] = append(vm.byLine[ann.Line], ann)
		}
	}

	// Sort each line's annotations by Col ascending, then Priority descending
	for line := range vm.byLine {
		items := vm.byLine[line]
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Col != items[j].Col {
				return items[i].Col < items[j].Col
			}
			return items[i].Priority > items[j].Priority
		})
	}
}
