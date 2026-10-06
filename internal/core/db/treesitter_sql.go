package db

import (
	"regexp"
	"strings"
)

var (
	createTableRe = regexp.MustCompile("(?is)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?([a-zA-Z0-9_\".\\[\\]`]+)\\s*\\((.*?)\\);")
	fkConstraintRe = regexp.MustCompile("(?i)FOREIGN\\s+KEY\\s*\\(([a-zA-Z0-9_\".\\[\\]`]+)\\)\\s*REFERENCES\\s+([a-zA-Z0-9_\".\\[\\]`]+)\\s*\\(([a-zA-Z0-9_\".\\[\\]`]+)\\)")
)

// ParseSQLDDL extracts a SchemaAST from raw SQL DDL migration text without requiring a live database.
func ParseSQLDDL(sqlContent string) *SchemaAST {
	schema := NewSchemaAST()

	matches := createTableRe.FindAllStringSubmatch(sqlContent, -1)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		tableName := cleanIdentifier(m[1])
		body := m[2]

		table := &Table{
			Name:        tableName,
			Columns:     make([]Column, 0),
			ForeignKeys: make([]ForeignKey, 0),
		}

		// Split columns by comma outside parentheses
		lines := splitSQLDefinitions(body)
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Check if table-level FOREIGN KEY constraint
			if fkMatches := fkConstraintRe.FindStringSubmatch(line); len(fkMatches) >= 4 {
				table.ForeignKeys = append(table.ForeignKeys, ForeignKey{
					Name:       cleanIdentifier(line),
					FromTable:  tableName,
					FromColumn: cleanIdentifier(fkMatches[1]),
					ToTable:    cleanIdentifier(fkMatches[2]),
					ToColumn:   cleanIdentifier(fkMatches[3]),
				})
				continue
			}

			// Check if table-level PRIMARY KEY constraint
			if strings.HasPrefix(strings.ToUpper(line), "PRIMARY KEY") {
				continue
			}

			// Column definition: [name] [type] [constraints...]
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				colName := cleanIdentifier(parts[0])
				colType := strings.ToLower(parts[1])
				upperLine := strings.ToUpper(line)

				isPK := strings.Contains(upperLine, "PRIMARY KEY")
				isNullable := !strings.Contains(upperLine, "NOT NULL") && !isPK

				table.Columns = append(table.Columns, Column{
					Name:       colName,
					DataType:   colType,
					IsPK:       isPK,
					IsNullable: isNullable,
				})
			}
		}

		schema.Tables[tableName] = table
	}

	return schema
}

func cleanIdentifier(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.LastIndex(s, "."); idx >= 0 && idx < len(s)-1 {
		s = s[idx+1:]
	}
	s = strings.Trim(s, `"'`+"`[]")
	return s
}

func splitSQLDefinitions(body string) []string {
	var parts []string
	var curr strings.Builder
	depth := 0

	for _, r := range body {
		switch r {
		case '(':
			depth++
			curr.WriteRune(r)
		case ')':
			depth--
			curr.WriteRune(r)
		case ',':
			if depth == 0 {
				parts = append(parts, curr.String())
				curr.Reset()
			} else {
				curr.WriteRune(r)
			}
		default:
			curr.WriteRune(r)
		}
	}
	if curr.Len() > 0 {
		parts = append(parts, curr.String())
	}
	return parts
}
