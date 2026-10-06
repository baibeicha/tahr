package sdk

import (
	"encoding/binary"
	"testing"
)

func TestEncodeDecodeSimpleNode(t *testing.T) {
	btn := NewButton("btn_submit", "Отправить", "cmd.submit")
	btn.Style = LayoutStyle{FlexGrow: 1, Width: 120, Height: 30}

	data, err := EncodeBinaryTree(btn)
	if err != nil {
		t.Fatalf("EncodeBinaryTree failed: %v", err)
	}

	decoded, err := DecodeBinaryTree(data)
	if err != nil {
		t.Fatalf("DecodeBinaryTree failed: %v", err)
	}

	if decoded.ID != "btn_submit" {
		t.Errorf("expected ID 'btn_submit', got '%s'", decoded.ID)
	}
	if decoded.Type != NodeTypeButton {
		t.Errorf("expected NodeTypeButton, got %v", decoded.Type)
	}
	if decoded.Text != "Отправить" {
		t.Errorf("expected Text 'Отправить', got '%s'", decoded.Text)
	}
	if decoded.OnClick != "cmd.submit" {
		t.Errorf("expected OnClick 'cmd.submit', got '%s'", decoded.OnClick)
	}
	if decoded.Style.Width != 120 || decoded.Style.Height != 30 {
		t.Errorf("expected Style 120x30, got %dx%d", decoded.Style.Width, decoded.Style.Height)
	}
}

func TestEncodeDecodeComplexTree(t *testing.T) {
	tree := NewVStack("root",
		NewHStack("header",
			NewText("title", "Kafka Consumer Monitor"),
			NewButton("btn_refresh", "Обновить", "cmd.refresh"),
		),
		NewInput("search", "Фильтр топиков...", "", "cmd.filter"),
		NewDataGrid("grid",
			[]string{"Топик", "Партиции", "Сообщений/сек"},
			[][]string{
				{"orders-v1", "12", "4500"},
				{"payments-v2", "6", "1200"},
				{"telemetry", "32", "18000"},
			},
		),
		NewGraphCanvas("dag_topology"),
	)

	data, err := EncodeBinaryTree(tree)
	if err != nil {
		t.Fatalf("EncodeBinaryTree failed: %v", err)
	}

	decoded, err := DecodeBinaryTree(data)
	if err != nil {
		t.Fatalf("DecodeBinaryTree failed: %v", err)
	}

	if decoded.ID != "root" || decoded.Type != NodeTypeVStack {
		t.Fatalf("expected root VStack, got %s (%v)", decoded.ID, decoded.Type)
	}
	if len(decoded.Children) != 4 {
		t.Fatalf("expected 4 children, got %d", len(decoded.Children))
	}

	// Verify Header HStack
	header := decoded.Children[0]
	if header.ID != "header" || header.Type != NodeTypeHStack {
		t.Errorf("expected child 0 to be header HStack, got %s", header.ID)
	}
	if len(header.Children) != 2 {
		t.Fatalf("expected header to have 2 children, got %d", len(header.Children))
	}

	// Verify DataGrid
	grid := decoded.Children[2]
	if grid.ID != "grid" || grid.Type != NodeTypeDataGrid {
		t.Errorf("expected child 2 to be DataGrid, got %s", grid.ID)
	}
	if len(grid.DataGridCols) != 3 {
		t.Errorf("expected 3 cols, got %d", len(grid.DataGridCols))
	}
	if len(grid.DataGridRows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(grid.DataGridRows))
	}
	if grid.DataGridRows[0][0] != "orders-v1" {
		t.Errorf("expected row 0 cell 0 'orders-v1', got '%s'", grid.DataGridRows[0][0])
	}

	// Verify GraphCanvas
	canvas := decoded.Children[3]
	if canvas.ID != "dag_topology" || canvas.Type != NodeTypeGraphCanvas {
		t.Errorf("expected child 3 to be GraphCanvas, got %s (%v)", canvas.ID, canvas.Type)
	}
}

func TestMagicAndVersionValidation(t *testing.T) {
	valid := []byte{TLVMagic0, TLVMagic1, TLVVersion1, 0x00}
	if _, err := DecodeBinaryTree(valid); err != nil {
		t.Errorf("expected valid header to pass, got error: %v", err)
	}

	invalidMagic := []byte{0xDE, 0xAD, TLVVersion1, 0x00}
	if _, err := DecodeBinaryTree(invalidMagic); err == nil {
		t.Error("expected error for invalid magic header, got nil")
	}

	unsupportedVersion := []byte{TLVMagic0, TLVMagic1, 0x99, 0x00}
	if _, err := DecodeBinaryTree(unsupportedVersion); err == nil {
		t.Error("expected error for unsupported version, got nil")
	}
}

func TestForwardCompatibility_SkipUnknownTag(t *testing.T) {
	node := NewText("txt1", "Hello Tahr")
	data, err := EncodeBinaryTree(node)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	// Inject a custom unknown tag (Type=0x0777, Length=8, Value=8 dummy bytes) right before the end
	unknownTag := make([]byte, 6+8)
	binary.LittleEndian.PutUint16(unknownTag[0:2], 0x0777)
	binary.LittleEndian.PutUint32(unknownTag[2:6], 8)
	copy(unknownTag[6:], []byte("UNKNOWN!"))

	injected := append(data, unknownTag...)

	decoded, err := DecodeBinaryTree(injected)
	if err != nil {
		t.Fatalf("DecodeBinaryTree should safely ignore unknown tag, but failed: %v", err)
	}

	if decoded.ID != "txt1" || decoded.Text != "Hello Tahr" {
		t.Errorf("decoded text node corrupted after unknown tag skip")
	}
}

func BenchmarkEncodeDecode(b *testing.B) {
	tree := NewVStack("root",
		NewHStack("header",
			NewText("title", "Kafka Consumer Monitor"),
			NewButton("btn_refresh", "Обновить", "cmd.refresh"),
		),
		NewDataGrid("grid",
			[]string{"Топик", "Партиции", "Сообщений/сек"},
			[][]string{
				{"orders-v1", "12", "4500"},
				{"payments-v2", "6", "1200"},
				{"telemetry", "32", "18000"},
			},
		),
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data, err := EncodeBinaryTree(tree)
		if err != nil {
			b.Fatal(err)
		}
		_, err = DecodeBinaryTree(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}
