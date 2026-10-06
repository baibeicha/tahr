package db

import (
	"strings"
	"testing"
)

func TestExportMermaidAndDDL(t *testing.T) {
	schema := DefaultSampleSchema()
	if len(schema.Tables) != 3 {
		t.Fatalf("expected 3 sample tables, got %d", len(schema.Tables))
	}

	mermaid := ExportMermaid(schema)
	if !strings.HasPrefix(mermaid, "erDiagram") {
		t.Fatalf("expected mermaid diagram to start with erDiagram, got: %s", mermaid)
	}
	if !strings.Contains(mermaid, "users {") || !strings.Contains(mermaid, "orders {") {
		t.Fatalf("expected mermaid to contain users and orders entities: %s", mermaid)
	}
	if !strings.Contains(mermaid, "orders }o--|| users") {
		t.Fatalf("expected orders to users foreign key relation in mermaid: %s", mermaid)
	}

	ddl := ExportFullDDL(schema)
	if !strings.Contains(ddl, "CREATE TABLE users") || !strings.Contains(ddl, "CREATE TABLE orders") {
		t.Fatalf("expected DDL to contain table creation statements: %s", ddl)
	}
	if !strings.Contains(ddl, "FOREIGN KEY (user_id) REFERENCES users(id)") {
		t.Fatalf("expected DDL to contain foreign key constraint: %s", ddl)
	}
}
