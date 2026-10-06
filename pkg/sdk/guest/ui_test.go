package guest

import (
	"testing"

	core "tahr/internal/core/sdk"
)

func TestGuestSDK_HostDecoderCrossCompatibility(t *testing.T) {
	// 1. Build tree using Guest SDK
	tree := VStack("root",
		HStack("header",
			Text("lbl_title", "Kafka Realtime Monitor"),
			Button("btn_refresh", "Refresh", "cmd.refresh"),
		),
		SearchInput("filter", "Filter topics...", "", "cmd.filter"),
		DataGrid("topics_grid",
			[]string{"Topic", "Partitions", "Messages/sec"},
			[][]string{
				{"orders-v1", "12", "5400"},
				{"payments", "6", "1200"},
			},
		),
		GraphCanvas("dag_canvas"),
	)

	// 2. Encode using Guest SDK
	encodedBytes := ToBytes(tree)
	if len(encodedBytes) == 0 {
		t.Fatalf("expected non-empty byte buffer from Guest SDK")
	}

	// 3. Decode on Host Core using DecodeBinaryTree
	hostDecoded, err := core.DecodeBinaryTree(encodedBytes)
	if err != nil {
		t.Fatalf("Host core failed to decode Guest SDK output: %v", err)
	}

	if hostDecoded.ID != "root" || hostDecoded.Type != core.NodeTypeVStack {
		t.Fatalf("unexpected root: ID=%s, Type=%v", hostDecoded.ID, hostDecoded.Type)
	}

	if len(hostDecoded.Children) != 4 {
		t.Fatalf("expected 4 children, got %d", len(hostDecoded.Children))
	}

	// Verify Header
	hdr := hostDecoded.Children[0]
	if hdr.ID != "header" || hdr.Type != core.NodeTypeHStack {
		t.Errorf("expected HStack header, got %+v", hdr)
	}

	// Verify DataGrid
	grid := hostDecoded.Children[2]
	if grid.ID != "topics_grid" || len(grid.DataGridCols) != 3 || len(grid.DataGridRows) != 2 {
		t.Errorf("unexpected DataGrid decoded by host: %+v", grid)
	}

	// Verify Canvas
	canvas := hostDecoded.Children[3]
	if canvas.ID != "dag_canvas" || canvas.Type != core.NodeTypeGraphCanvas {
		t.Errorf("unexpected Canvas decoded by host: %+v", canvas)
	}
}
