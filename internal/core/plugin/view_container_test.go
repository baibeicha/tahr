package plugin

import (
	"testing"
)

func TestViewContainerRegistry(t *testing.T) {
	reg := NewViewContainerRegistry()

	// Register a view from ext-kafka
	tabKafka, err := reg.RegisterView("ext-kafka", ViewContribution{
		ContainerID: "workbench.view.data-services",
		TabID:       "kafka-topics",
		Title:       "Kafka",
		Icon:        "nf-dev-kafka",
		Slot:        string(SlotEditorArea),
	})
	if err != nil {
		t.Fatalf("RegisterView failed: %v", err)
	}

	if tabKafka.ID != "ext-kafka.kafka-topics" {
		t.Errorf("expected tab ID 'ext-kafka.kafka-topics', got %s", tabKafka.ID)
	}

	// Register a second view from ext-postgres into the same container
	tabPG, err := reg.RegisterView("ext-postgres", ViewContribution{
		ContainerID: "workbench.view.data-services",
		TabID:       "pg-tables",
		Title:       "Postgres",
		Icon:        "nf-dev-postgresql",
		Slot:        string(SlotEditorArea),
	})
	if err != nil {
		t.Fatalf("RegisterView failed: %v", err)
	}

	container := reg.GetContainer("workbench.view.data-services")
	if container == nil {
		t.Fatalf("expected container to exist")
	}

	if len(container.Tabs) != 2 {
		t.Fatalf("expected 2 tabs in container, got %d", len(container.Tabs))
	}

	if container.Tabs[0].ID != tabKafka.ID || container.Tabs[1].ID != tabPG.ID {
		t.Errorf("unexpected tabs order: %+v", container.Tabs)
	}

	// Tab selection
	if !container.SelectTab("pg-tables") {
		t.Errorf("failed to select pg-tables tab")
	}
	if container.ActiveTab().ID != tabPG.ID {
		t.Errorf("expected active tab to be pg, got %s", container.ActiveTab().ID)
	}

	// Slots query
	editorContainers := reg.ContainersInSlot(SlotEditorArea)
	if len(editorContainers) != 1 || editorContainers[0].ID != "workbench.view.data-services" {
		t.Errorf("unexpected containers in SlotEditorArea: %+v", editorContainers)
	}
}
