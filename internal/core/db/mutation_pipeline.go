package db

import (
	"fmt"
	"strings"
)

// IntentType represents an explicit user action performed on the canvas.
type IntentType int

const (
	IntentAddTable IntentType = iota
	IntentDropTable
	IntentAddColumn
	IntentDropColumn
	IntentRenameColumn
	IntentAddForeignKey
	IntentDropForeignKey
)

// UserIntent captures user intention to distinguish renames from drop+add.
type UserIntent struct {
	Type     IntentType
	Table    string
	OldName  string
	NewName  string
	Column   Column
	Relation ForeignKey
}

// ColumnRename captures an explicit column rename.
type ColumnRename struct {
	Table   string
	OldName string
	NewName string
}

// ColumnChange captures a column addition or deletion.
type ColumnChange struct {
	Table  string
	Column Column
}

// SchemaDiff represents the calculated delta between original and mutated schemas.
type SchemaDiff struct {
	AddedTables        []Table
	DroppedTables      []string
	RenamedColumns     []ColumnRename
	AddedColumns       []ColumnChange
	DroppedColumns     []ColumnChange
	AddedForeignKeys   []ForeignKey
	DroppedForeignKeys []ForeignKey
	HasDestructive     bool
}

// ComputeDiff calculates the delta using explicit intents and state diffing.
func ComputeDiff(orig, curr *SchemaAST, intents []UserIntent) *SchemaDiff {
	diff := &SchemaDiff{
		AddedTables:        make([]Table, 0),
		DroppedTables:      make([]string, 0),
		RenamedColumns:     make([]ColumnRename, 0),
		AddedColumns:       make([]ColumnChange, 0),
		DroppedColumns:     make([]ColumnChange, 0),
		AddedForeignKeys:   make([]ForeignKey, 0),
		DroppedForeignKeys: make([]ForeignKey, 0),
	}

	renamedColsMap := make(map[string]bool) // "table.oldName" -> true

	// 1. Process explicit user intents first (Intent Tracking)
	for _, intent := range intents {
		switch intent.Type {
		case IntentRenameColumn:
			diff.RenamedColumns = append(diff.RenamedColumns, ColumnRename{
				Table:   intent.Table,
				OldName: intent.OldName,
				NewName: intent.NewName,
			})
			renamedColsMap[fmt.Sprintf("%s.%s", intent.Table, intent.OldName)] = true
		}
	}

	// 2. Diff Tables
	for name, currTbl := range curr.Tables {
		if _, exists := orig.Tables[name]; !exists {
			diff.AddedTables = append(diff.AddedTables, *currTbl)
		}
	}
	for name := range orig.Tables {
		if _, exists := curr.Tables[name]; !exists {
			diff.DroppedTables = append(diff.DroppedTables, name)
			diff.HasDestructive = true
		}
	}

	// 3. Diff Columns
	for name, currTbl := range curr.Tables {
		origTbl, exists := orig.Tables[name]
		if !exists {
			continue
		}

		origCols := make(map[string]Column)
		for _, col := range origTbl.Columns {
			origCols[col.Name] = col
		}
		currCols := make(map[string]Column)
		for _, col := range currTbl.Columns {
			currCols[col.Name] = col
		}

		// Added columns
		for cName, c := range currCols {
			if _, exists := origCols[cName]; !exists {
				// Ensure this was not part of an explicit rename
				isRenameTarget := false
				for _, r := range diff.RenamedColumns {
					if r.Table == name && r.NewName == cName {
						isRenameTarget = true
						break
					}
				}
				if !isRenameTarget {
					diff.AddedColumns = append(diff.AddedColumns, ColumnChange{Table: name, Column: c})
				}
			}
		}

		// Dropped columns
		for cName, c := range origCols {
			if _, exists := currCols[cName]; !exists {
				if !renamedColsMap[fmt.Sprintf("%s.%s", name, cName)] {
					diff.DroppedColumns = append(diff.DroppedColumns, ColumnChange{Table: name, Column: c})
					diff.HasDestructive = true
				}
			}
		}
	}

	// 4. Diff ForeignKeys
	for name, currTbl := range curr.Tables {
		origTbl, exists := orig.Tables[name]
		if !exists {
			continue
		}
		origFKs := make(map[string]ForeignKey)
		for _, fk := range origTbl.ForeignKeys {
			key := fmt.Sprintf("%s.%s->%s.%s", fk.FromTable, fk.FromColumn, fk.ToTable, fk.ToColumn)
			origFKs[key] = fk
		}
		for _, fk := range currTbl.ForeignKeys {
			key := fmt.Sprintf("%s.%s->%s.%s", fk.FromTable, fk.FromColumn, fk.ToTable, fk.ToColumn)
			if _, exists := origFKs[key]; !exists {
				diff.AddedForeignKeys = append(diff.AddedForeignKeys, fk)
			}
		}
		currFKs := make(map[string]ForeignKey)
		for _, fk := range currTbl.ForeignKeys {
			key := fmt.Sprintf("%s.%s->%s.%s", fk.FromTable, fk.FromColumn, fk.ToTable, fk.ToColumn)
			currFKs[key] = fk
		}
		for _, fk := range origTbl.ForeignKeys {
			key := fmt.Sprintf("%s.%s->%s.%s", fk.FromTable, fk.FromColumn, fk.ToTable, fk.ToColumn)
			if _, exists := currFKs[key]; !exists {
				diff.DroppedForeignKeys = append(diff.DroppedForeignKeys, fk)
			}
		}
	}

	return diff
}

// GenerateDDLPostgres synthesizes Up and Down SQL migration scripts for PostgreSQL.
func (diff *SchemaDiff) GenerateDDLPostgres() (upSQL string, downSQL string) {
	var up, down strings.Builder

	// 1. Renamed Columns
	for _, r := range diff.RenamedColumns {
		up.WriteString(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;\n", r.Table, r.OldName, r.NewName))
		down.WriteString(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;\n", r.Table, r.NewName, r.OldName))
	}

	// 2. Added Columns
	for _, ac := range diff.AddedColumns {
		nullable := ""
		if !ac.Column.IsNullable {
			nullable = " NOT NULL"
		}
		up.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s%s;\n", ac.Table, ac.Column.Name, strings.ToUpper(ac.Column.DataType), nullable))
		down.WriteString(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;\n", ac.Table, ac.Column.Name))
	}

	// 3. Dropped Columns (Destructive!)
	for _, dc := range diff.DroppedColumns {
		up.WriteString(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;\n", dc.Table, dc.Column.Name))
		down.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;\n", dc.Table, dc.Column.Name, strings.ToUpper(dc.Column.DataType)))
	}

	// 4. Added ForeignKeys
	for _, fk := range diff.AddedForeignKeys {
		up.WriteString(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT fk_%s_%s FOREIGN KEY (%s) REFERENCES %s(%s);\n",
			fk.FromTable, fk.FromTable, fk.FromColumn, fk.FromColumn, fk.ToTable, fk.ToColumn))
		down.WriteString(fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT fk_%s_%s;\n", fk.FromTable, fk.FromTable, fk.FromColumn))
	}

	// 5. Added Tables
	for _, tbl := range diff.AddedTables {
		up.WriteString(fmt.Sprintf("CREATE TABLE %s (\n", tbl.Name))
		for i, col := range tbl.Columns {
			pk := ""
			if col.IsPK {
				pk = " PRIMARY KEY"
			}
			comma := ","
			if i == len(tbl.Columns)-1 {
				comma = ""
			}
			up.WriteString(fmt.Sprintf("    %s %s%s%s\n", col.Name, strings.ToUpper(col.DataType), pk, comma))
		}
		up.WriteString(");\n")
		down.WriteString(fmt.Sprintf("DROP TABLE IF EXISTS %s;\n", tbl.Name))
	}

	// 6. Dropped Tables
	for _, tName := range diff.DroppedTables {
		up.WriteString(fmt.Sprintf("DROP TABLE IF EXISTS %s;\n", tName))
		down.WriteString(fmt.Sprintf("-- Restore table %s manually or from backup\n", tName))
	}

	return up.String(), down.String()
}
