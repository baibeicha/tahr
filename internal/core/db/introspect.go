package db

import (
	"database/sql"
	"fmt"
	"strings"

	"tahr/internal/core/dag"
)

// Column represents a single database attribute definition.
type Column struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsPK         bool   `json:"is_pk"`
	IsNullable   bool   `json:"is_nullable"`
	DefaultValue string `json:"default_value,omitempty"`
}

// ForeignKey represents a referential integrity constraint.
type ForeignKey struct {
	Name       string `json:"name"`
	FromTable  string `json:"from_table"`
	FromColumn string `json:"from_column"`
	ToTable    string `json:"to_table"`
	ToColumn   string `json:"to_column"`
}

// Table represents a database relation with its columns and keys.
type Table struct {
	Name        string       `json:"name"`
	Columns     []Column     `json:"columns"`
	ForeignKeys []ForeignKey `json:"foreign_keys"`
}

// SchemaAST represents the complete normalized schema structure.
type SchemaAST struct {
	Tables map[string]*Table `json:"tables"`
}

// NewSchemaAST initializes an empty schema AST.
func NewSchemaAST() *SchemaAST {
	return &SchemaAST{
		Tables: make(map[string]*Table),
	}
}

// Clone creates a deep copy of SchemaAST for sandbox state isolation.
func (s *SchemaAST) Clone() *SchemaAST {
	clone := NewSchemaAST()
	for name, tbl := range s.Tables {
		tblCopy := &Table{
			Name:        tbl.Name,
			Columns:     append([]Column(nil), tbl.Columns...),
			ForeignKeys: append([]ForeignKey(nil), tbl.ForeignKeys...),
		}
		clone.Tables[name] = tblCopy
	}
	return clone
}

// ToGraphModel projects the normalized schema into an interactive DAG Canvas model.
func (s *SchemaAST) ToGraphModel() *dag.GraphModel {
	gm := dag.NewGraphModel()

	// 1. Project tables into NodeCards
	for _, tbl := range s.Tables {
		card := &dag.NodeCard{
			ID:    tbl.Name,
			Title: tbl.Name,
			Badge: "Table",
			Rows:  make([]dag.CardRow, 0, len(tbl.Columns)),
			Ports: make([]dag.Port, 0),
		}

		// Detect FK columns
		fkCols := make(map[string]bool)
		for _, fk := range tbl.ForeignKeys {
			fkCols[fk.FromColumn] = true
		}

		for rIdx, col := range tbl.Columns {
			isFK := fkCols[col.Name]
			card.Rows = append(card.Rows, dag.CardRow{
				Name:       col.Name,
				DataType:   col.DataType,
				IsPK:       col.IsPK,
				IsFK:       isFK,
				IsNullable: col.IsNullable,
			})

			// Add port on left (for incoming FK / PK references) and right (for outgoing FK references)
			card.Ports = append(card.Ports, dag.Port{
				ID:       col.Name,
				RowIndex: rIdx,
				Side:     'R',
				Type:     dag.PortBoth,
			})
		}
		gm.AddNode(card)
	}

	// 2. Project ForeignKeys into directed edges
	for _, tbl := range s.Tables {
		for _, fk := range tbl.ForeignKeys {
			if _, exists := gm.Nodes[fk.ToTable]; exists {
				gm.AddEdge(tbl.Name, fk.FromColumn, fk.ToTable, fk.ToColumn, dag.MarkerCrowFootMany)
			}
		}
	}

	return gm
}

// IntrospectSQLite executes PRAGMA statements to extract the schema AST from an open SQLite connection.
func IntrospectSQLite(db *sql.DB) (*SchemaAST, error) {
	schema := NewSchemaAST()

	// 1. Get all tables
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%';")
	if err != nil {
		return nil, fmt.Errorf("query sqlite tables: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			tableNames = append(tableNames, name)
		}
	}

	// 2. Introspect columns & FKs for each table
	for _, tName := range tableNames {
		tbl := &Table{
			Name:        tName,
			Columns:     make([]Column, 0),
			ForeignKeys: make([]ForeignKey, 0),
		}

		// PRAGMA table_info(tbl)
		infoRows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s);", tName))
		if err == nil {
			for infoRows.Next() {
				var cid, notnull, pk int
				var name, dType string
				var dflt *string
				if err := infoRows.Scan(&cid, &name, &dType, &notnull, &dflt, &pk); err == nil {
					defVal := ""
					if dflt != nil {
						defVal = *dflt
					}
					tbl.Columns = append(tbl.Columns, Column{
						Name:         name,
						DataType:     strings.ToLower(dType),
						IsPK:         pk > 0,
						IsNullable:   notnull == 0,
						DefaultValue: defVal,
					})
				}
			}
			infoRows.Close()
		}

		// PRAGMA foreign_key_list(tbl)
		fkRows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list(%s);", tName))
		if err == nil {
			for fkRows.Next() {
				var id, seq int
				var toTable, fromCol, toCol, onUpdate, onDelete, match string
				if err := fkRows.Scan(&id, &seq, &toTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); err == nil {
					tbl.ForeignKeys = append(tbl.ForeignKeys, ForeignKey{
						Name:       fmt.Sprintf("fk_%s_%s", tName, fromCol),
						FromTable:  tName,
						FromColumn: fromCol,
						ToTable:    toTable,
						ToColumn:   toCol,
					})
				}
			}
			fkRows.Close()
		}

		schema.Tables[tName] = tbl
	}

	return schema, nil
}
