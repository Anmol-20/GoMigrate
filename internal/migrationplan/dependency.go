package migrationplan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
)

// validateForeignKeyReferences verifies that every source foreign key points
// to an existing source table and valid columns, with a suitable referenced
// index available on the parent table.
func validateForeignKeyReferences(schema *mysqlclient.Schema) error {
	tables := make(map[string]mysqlclient.Table, len(schema.Tables))

	for _, table := range schema.Tables {
		key := strings.ToLower(table.Name)
		if _, exists := tables[key]; exists {
			return fmt.Errorf("duplicate table name %q in source schema", table.Name)
		}
		tables[key] = table
	}

	for _, child := range schema.Tables {
		childColumns := make(map[string]bool, len(child.Columns))
		for _, column := range child.Columns {
			childColumns[strings.ToLower(column.Name)] = true
		}

		for _, fk := range child.ForeignKeys {
			childNames, parentNames, err := foreignKeyColumns(fk)
			if err != nil {
				return fmt.Errorf("table %q foreign key %q: %w", child.Name, fk.Name, err)
			}

			if len(childNames) != len(parentNames) {
				return fmt.Errorf(
					"table %q foreign key %q has mismatched child and referenced column counts",
					child.Name, fk.Name,
				)
			}

			for _, column := range childNames {
				if !childColumns[strings.ToLower(column)] {
					return fmt.Errorf(
						"table %q foreign key %q references missing child column %q",
						child.Name, fk.Name, column,
					)
				}
			}

			parent, exists := tables[strings.ToLower(fk.ReferencedTable)]
			if !exists {
				return fmt.Errorf(
					"table %q foreign key %q references missing source table %q",
					child.Name, fk.Name, fk.ReferencedTable,
				)
			}

			parentColumns := make(map[string]bool, len(parent.Columns))
			for _, column := range parent.Columns {
				parentColumns[strings.ToLower(column.Name)] = true
			}

			for _, column := range parentNames {
				if !parentColumns[strings.ToLower(column)] {
					return fmt.Errorf(
						"table %q foreign key %q references missing column %q on table %q",
						child.Name, fk.Name, column, parent.Name,
					)
				}
			}

			if !hasReferencedIndex(parent.Indexes, parentNames) {
				return fmt.Errorf(
					"table %q foreign key %q references columns %v on %q, "+
						"but no suitable index starts with those columns",
					child.Name, fk.Name, parentNames, parent.Name,
				)
			}
		}
	}

	return nil
}

func foreignKeyColumns(fk mysqlclient.ForeignKey) ([]string, []string, error) {
	child := fk.Columns
	if len(child) == 0 && fk.Column != "" {
		child = []string{fk.Column}
	}

	parent := fk.ReferencedColumns
	if len(parent) == 0 && fk.ReferencedColumn != "" {
		parent = []string{fk.ReferencedColumn}
	}

	if len(child) == 0 || len(parent) == 0 {
		return nil, nil, fmt.Errorf("foreign key has empty column definitions")
	}

	if len(child) != len(parent) {
		return nil, nil, fmt.Errorf(
			"has %d child columns and %d referenced columns",
			len(child), len(parent),
		)
	}

	for _, name := range append(append([]string{}, child...), parent...) {
		if strings.TrimSpace(name) == "" {
			return nil, nil, fmt.Errorf("foreign key contains an empty column name")
		}
	}

	if strings.TrimSpace(fk.ReferencedTable) == "" {
		return nil, nil, fmt.Errorf("foreign key has no referenced table")
	}

	return child, parent, nil
}

// hasReferencedIndex checks whether an index begins with the complete
// referenced column sequence. Prefix and functional index parts are excluded.
func hasReferencedIndex(indexes []mysqlclient.Index, columns []string) bool {
	for _, index := range indexes {
		parts := index.Parts

		if len(parts) == 0 {
			parts = make([]mysqlclient.IndexPart, 0, len(index.Columns))
			for _, column := range index.Columns {
				parts = append(parts, mysqlclient.IndexPart{Column: column})
			}
		}

		if len(parts) < len(columns) {
			continue
		}

		matches := true

		for i, column := range columns {
			part := parts[i]

			if part.Expression != "" ||
				part.PrefixLength != 0 ||
				!strings.EqualFold(part.Column, column) {
				matches = false
				break
			}
		}

		if matches {
			return true
		}
	}

	return false
}

// orderNewTables returns source-only tables in parent-before-child order.
// Cycles are retained in deterministic order and reported as warnings because
// foreign keys are created separately after all tables exist.
func orderNewTables(
	source *mysqlclient.Schema,
	sourceOnly []string,
) ([]string, []string) {
	tableByName := make(map[string]mysqlclient.Table, len(source.Tables))
	for _, table := range source.Tables {
		tableByName[strings.ToLower(table.Name)] = table
	}

	newTables := make(map[string]bool, len(sourceOnly))
	for _, name := range sourceOnly {
		newTables[strings.ToLower(name)] = true
	}

	names := make([]string, 0, len(newTables))
	for name := range newTables {
		names = append(names, name)
	}
	sort.Strings(names)

	// state: 0 = unvisited, 1 = visiting, 2 = visited
	state := make(map[string]int, len(names))
	ordered := make([]string, 0, len(names))
	warnings := make([]string, 0)
	cycleWarnings := make(map[string]bool)

	var visit func(string, []string)

	visit = func(name string, stack []string) {
		if state[name] == 2 {
			return
		}

		if state[name] == 1 {
			start := 0
			for i, item := range stack {
				if item == name {
					start = i
					break
				}
			}

			cycle := append(append([]string{}, stack[start:]...), name)
			key := strings.Join(cycle, " -> ")

			if !cycleWarnings[key] {
				cycleWarnings[key] = true
				warnings = append(
					warnings,
					"Foreign-key dependency cycle detected: "+key+
						". Tables will be created before their foreign keys are added.",
				)
			}
			return
		}

		state[name] = 1
		stack = append(stack, name)

		table, exists := tableByName[name]
		if exists {
			dependencies := make([]string, 0)

			for _, fk := range table.ForeignKeys {
				parent := strings.ToLower(fk.ReferencedTable)
				if newTables[parent] {
					dependencies = append(dependencies, parent)
				}
			}

			sort.Strings(dependencies)

			seen := make(map[string]bool)
			for _, dependency := range dependencies {
				if seen[dependency] {
					continue
				}
				seen[dependency] = true
				visit(dependency, stack)
			}
		}

		state[name] = 2

		if exists {
			ordered = append(ordered, table.Name)
		} else {
			ordered = append(ordered, name)
		}
	}

	for _, name := range names {
		visit(name, nil)
	}

	return ordered, warnings
}
