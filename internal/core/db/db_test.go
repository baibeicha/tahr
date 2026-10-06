package db

import (
	"strings"
	"testing"
)

func TestParseSQLDDL(t *testing.T) {
	sql := `
CREATE TABLE users (
    id UUID PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    created_at TIMESTAMP
);

CREATE TABLE orders (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    total DECIMAL(10, 2),
    FOREIGN KEY (user_id) REFERENCES users(id)
);
`
	schema := ParseSQLDDL(sql)
	if len(schema.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(schema.Tables))
	}

	users := schema.Tables["users"]
	if users == nil || len(users.Columns) != 3 {
		t.Fatalf("users table missing or invalid columns: %+v", users)
	}
	if !users.Columns[0].IsPK || users.Columns[0].Name != "id" {
		t.Errorf("expected users.id to be PK")
	}

	orders := schema.Tables["orders"]
	if orders == nil || len(orders.ForeignKeys) != 1 {
		t.Fatalf("orders table missing FK: %+v", orders)
	}
	fk := orders.ForeignKeys[0]
	if fk.FromColumn != "user_id" || fk.ToTable != "users" || fk.ToColumn != "id" {
		t.Errorf("unexpected FK: %+v", fk)
	}

	// Test projection to GraphModel
	gm := schema.ToGraphModel()
	if len(gm.Nodes) != 2 {
		t.Errorf("expected 2 graph nodes, got %d", len(gm.Nodes))
	}
	if len(gm.Edges) != 1 {
		t.Errorf("expected 1 graph edge, got %d", len(gm.Edges))
	}

	// Test MSSQL and MySQL dialects (brackets and backticks)
	mssql := `
CREATE TABLE [dbo].[Customers] (
    [CustomerID] INT PRIMARY KEY,
    [CompanyName] NVARCHAR(100) NOT NULL
);
CREATE TABLE [Invoices] (
    [InvoiceID] INT PRIMARY KEY,
    [CustID] INT NOT NULL,
    FOREIGN KEY ([CustID]) REFERENCES [Customers]([CustomerID])
);
`
	msSchema := ParseSQLDDL(mssql)
	if len(msSchema.Tables) < 2 {
		t.Errorf("expected at least 2 tables from MSSQL DDL, got %d", len(msSchema.Tables))
	}
	if msSchema.Tables["Customers"] == nil && msSchema.Tables["dbo.Customers"] == nil {
		t.Errorf("expected Customers table in parsed MSSQL schema")
	}
}

func TestComputeDiff_IntentTrackingRename(t *testing.T) {
	orig := NewSchemaAST()
	orig.Tables["users"] = &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", DataType: "uuid", IsPK: true},
			{Name: "username", DataType: "varchar(64)"},
		},
	}

	curr := orig.Clone()
	curr.Tables["users"].Columns[1].Name = "login_name"

	// Intent tracking: user deliberately renamed username -> login_name
	intents := []UserIntent{
		{
			Type:    IntentRenameColumn,
			Table:   "users",
			OldName: "username",
			NewName: "login_name",
		},
	}

	diff := ComputeDiff(orig, curr, intents)

	if len(diff.RenamedColumns) != 1 {
		t.Fatalf("expected 1 renamed column, got %d", len(diff.RenamedColumns))
	}
	if diff.RenamedColumns[0].OldName != "username" || diff.RenamedColumns[0].NewName != "login_name" {
		t.Errorf("unexpected rename: %+v", diff.RenamedColumns[0])
	}
	if len(diff.DroppedColumns) != 0 || len(diff.AddedColumns) != 0 {
		t.Errorf("intent tracking failed! Dropped=%d, Added=%d", len(diff.DroppedColumns), len(diff.AddedColumns))
	}

	upSQL, downSQL := diff.GenerateDDLPostgres()
	if !strings.Contains(upSQL, "ALTER TABLE users RENAME COLUMN username TO login_name;") {
		t.Errorf("expected ALTER TABLE RENAME in upSQL: %s", upSQL)
	}
	if !strings.Contains(downSQL, "ALTER TABLE users RENAME COLUMN login_name TO username;") {
		t.Errorf("expected reverse ALTER TABLE RENAME in downSQL: %s", downSQL)
	}
}

func TestComputeDiff_DestructiveDrop(t *testing.T) {
	orig := NewSchemaAST()
	orig.Tables["users"] = &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", DataType: "uuid", IsPK: true},
			{Name: "obsolete_token", DataType: "text"},
		},
	}

	curr := orig.Clone()
	// Drop the column
	curr.Tables["users"].Columns = curr.Tables["users"].Columns[:1]

	diff := ComputeDiff(orig, curr, nil)

	if !diff.HasDestructive {
		t.Errorf("expected HasDestructive to be true")
	}
	if len(diff.DroppedColumns) != 1 {
		t.Fatalf("expected 1 dropped column, got %d", len(diff.DroppedColumns))
	}

	upSQL, _ := diff.GenerateDDLPostgres()
	if !strings.Contains(upSQL, "ALTER TABLE users DROP COLUMN obsolete_token;") {
		t.Errorf("expected DROP COLUMN in upSQL: %s", upSQL)
	}
}
