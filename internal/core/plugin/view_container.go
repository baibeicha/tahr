package plugin

import (
	"fmt"
	"sync"

	"tahr/internal/core/sdk"
)

// SlotID defines standard IDE UI docking slots for custom views.
type SlotID string

const (
	SlotLeftSidebar  SlotID = "workbench.slot.left-sidebar"
	SlotRightSidebar SlotID = "workbench.slot.right-sidebar"
	SlotBottomPanel  SlotID = "workbench.slot.bottom-panel"
	SlotEditorArea   SlotID = "workbench.slot.editor-area"
	SlotModalOverlay SlotID = "workbench.slot.modal-overlay"
)

// ViewTab represents an individual tab inside a ViewContainer.
type ViewTab struct {
	ID          string    // Unique composite key: PluginID + "." + TabID
	PluginID    string
	ContainerID string
	TabID       string
	Title       string
	Icon        string
	Slot        SlotID
	LatestNode  *sdk.Node // Cached declarative UI node tree
	RenderFunc  func() (*sdk.Node, error)
	EventFunc   func(eventID, payload string) error
}

// ViewContainer groups one or more tabs in a shared UI slot.
type ViewContainer struct {
	ID             string
	Slot           SlotID
	Title          string
	Icon           string
	Tabs           []*ViewTab
	ActiveTabIndex int
}

// ActiveTab returns the currently focused tab in this container, or nil if empty.
func (vc *ViewContainer) ActiveTab() *ViewTab {
	if vc.ActiveTabIndex >= 0 && vc.ActiveTabIndex < len(vc.Tabs) {
		return vc.Tabs[vc.ActiveTabIndex]
	}
	if len(vc.Tabs) > 0 {
		return vc.Tabs[0]
	}
	return nil
}

// SelectTab focuses a tab by its tab ID.
func (vc *ViewContainer) SelectTab(tabID string) bool {
	for i, t := range vc.Tabs {
		if t.TabID == tabID || t.ID == tabID {
			vc.ActiveTabIndex = i
			return true
		}
	}
	return false
}

// ViewContainerRegistry orchestrates all contributed views and containers.
type ViewContainerRegistry struct {
	mu         sync.RWMutex
	containers map[string]*ViewContainer // ContainerID -> ViewContainer
	tabs       map[string]*ViewTab       // Tab composite ID -> ViewTab
}

// NewViewContainerRegistry initializes an empty view container registry.
func NewViewContainerRegistry() *ViewContainerRegistry {
	return &ViewContainerRegistry{
		containers: make(map[string]*ViewContainer),
		tabs:       make(map[string]*ViewTab),
	}
}

// RegisterContainer explicitly registers or updates a ViewContainer.
func (r *ViewContainerRegistry) RegisterContainer(id string, slot SlotID, title, icon string) *ViewContainer {
	r.mu.Lock()
	defer r.mu.Unlock()

	c, exists := r.containers[id]
	if !exists {
		c = &ViewContainer{
			ID:    id,
			Slot:  slot,
			Title: title,
			Icon:  icon,
			Tabs:  make([]*ViewTab, 0),
		}
		r.containers[id] = c
	}
	return c
}

// RegisterView adds a contributed view tab to the registry and binds it to its container.
func (r *ViewContainerRegistry) RegisterView(pluginID string, contrib ViewContribution) (*ViewTab, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if contrib.ContainerID == "" {
		contrib.ContainerID = "workbench.view.default"
	}
	slot := SlotID(contrib.Slot)
	if slot == "" {
		slot = SlotEditorArea
	}

	container, exists := r.containers[contrib.ContainerID]
	if !exists {
		container = &ViewContainer{
			ID:    contrib.ContainerID,
			Slot:  slot,
			Title: contrib.Title,
			Icon:  contrib.Icon,
			Tabs:  make([]*ViewTab, 0),
		}
		r.containers[contrib.ContainerID] = container
	}

	tabCompositeID := fmt.Sprintf("%s.%s", pluginID, contrib.TabID)
	tab := &ViewTab{
		ID:          tabCompositeID,
		PluginID:    pluginID,
		ContainerID: contrib.ContainerID,
		TabID:       contrib.TabID,
		Title:       contrib.Title,
		Icon:        contrib.Icon,
		Slot:        slot,
	}

	// Update existing tab if re-registered or append
	found := false
	for i, existing := range container.Tabs {
		if existing.ID == tabCompositeID {
			container.Tabs[i] = tab
			found = true
			break
		}
	}
	if !found {
		container.Tabs = append(container.Tabs, tab)
	}
	r.tabs[tabCompositeID] = tab

	return tab, nil
}

// GetContainer returns a container by ID, or nil if not found.
func (r *ViewContainerRegistry) GetContainer(id string) *ViewContainer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.containers[id]
}

// GetTab returns a tab by its composite ID, or nil if not found.
func (r *ViewContainerRegistry) GetTab(id string) *ViewTab {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tabs[id]
}

// ContainersInSlot returns all view containers mapped to a given slot.
func (r *ViewContainerRegistry) ContainersInSlot(slot SlotID) []*ViewContainer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*ViewContainer
	for _, c := range r.containers {
		if c.Slot == slot {
			result = append(result, c)
		}
	}
	return result
}
