package mysqlclient

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type Schema struct {
	Database string  `json:"database"`
	Tables   []Table `json:"tables"`
}

type Table struct {
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Engine      string       `json:"engine"`
	Charset     string       `json:"charset"`
	Collation   string       `json:"collation"`
	Columns     []Column     `json:"columns"`
	Indexes     []Index      `json:"indexes"`
	ForeignKeys []ForeignKey `json:"foreign_keys"`
}

type Column struct {
	Name            string  `json:"name"`
	DataType        string  `json:"data_type"`
	ColumnType      string  `json:"column_type"`
	Nullable        bool    `json:"nullable"`
	DefaultValue    *string `json:"default_value"`
	Key             string  `json:"key"`
	Extra           string  `json:"extra"`
	OrdinalPosition int     `json:"ordinal_position"`
}

type Index struct {
	Name    string      `json:"name"`
	Unique  bool        `json:"unique"`
	Primary bool        `json:"primary"`
	Columns []string    `json:"columns"`
	Parts   []IndexPart `json:"parts,omitempty"`

	// MySQL index type: BTREE, HASH, FULLTEXT, or RTREE.
	Type string `json:"type,omitempty"`
}

type IndexPart struct {
	// Column is populated for an ordinary column-based index part.
	Column string `json:"column,omitempty"`

	// Expression is populated for a functional index part.
	Expression string `json:"expression,omitempty"`

	// PrefixLength is the indexed prefix length, if applicable.
	PrefixLength int `json:"prefix_length,omitempty"`

	// Order is ASC or DESC when specified by MySQL metadata.
	Order string `json:"order,omitempty"`
}

type ForeignKey struct {
	Name string `json:"name"`

	// Ordered columns participating in the foreign key.
	Columns []string `json:"columns"`

	// Ordered columns referenced in the parent table.
	ReferencedTable   string   `json:"referenced_table"`
	ReferencedColumns []string `json:"referenced_columns"`

	UpdateRule string `json:"update_rule"`
	DeleteRule string `json:"delete_rule"`

	// Legacy fields retained temporarily for existing code.
	Column           string `json:"column,omitempty"`
	ReferencedColumn string `json:"referenced_column,omitempty"`
}

// DiscoverSchema retrieves tables, columns, indexes and foreign keys.
func DiscoverSchema(
	ctx context.Context,
	db *sql.DB,
) (*Schema, error) {

	schema := &Schema{
		Tables: make([]Table, 0),
	}

	if err := db.QueryRowContext(
		ctx,
		"SELECT DATABASE()",
	).Scan(&schema.Database); err != nil {
		return nil, fmt.Errorf("discover database name: %w", err)
	}

	// Step 1: Discover tables, views and table properties.
	rows, err := db.QueryContext(ctx, `
    SELECT
        t.table_name,
        t.table_type,
        t.engine,
        t.table_collation,
        c.character_set_name
    FROM information_schema.tables t
    LEFT JOIN information_schema.collations c
        ON c.collation_name = t.table_collation
    WHERE t.table_schema = DATABASE()
    ORDER BY t.table_name
`)
	if err != nil {
		return nil, fmt.Errorf("discover tables: %w", err)
	}

	for rows.Next() {
		var table Table

		var engine sql.NullString
		var collation sql.NullString
		var charset sql.NullString

		if err := rows.Scan(
			&table.Name,
			&table.Type,
			&engine,
			&collation,
			&charset,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan table: %w", err)
		}

		if engine.Valid {
			table.Engine = engine.String
		}

		if collation.Valid {
			table.Collation = collation.String
		}

		if charset.Valid {
			table.Charset = charset.String
		}

		table.Columns = make([]Column, 0)
		table.Indexes = make([]Index, 0)
		table.ForeignKeys = make([]ForeignKey, 0)

		schema.Tables = append(schema.Tables, table)
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate tables: %w", err)
	}

	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close table rows: %w", err)
	}

	// Step 2: Discover columns and indexes for each table.
	for i := range schema.Tables {
		table := &schema.Tables[i]

		if err := discoverColumns(ctx, db, table); err != nil {
			return nil, err
		}

		if err := discoverIndexes(ctx, db, table); err != nil {
			return nil, err
		}

		if err := discoverForeignKeys(ctx, db, table); err != nil {
			return nil, err
		}
	}

	return schema, nil
}

func discoverColumns(
	ctx context.Context,
	db *sql.DB,
	table *Table,
) error {

	rows, err := db.QueryContext(ctx, `
        SELECT
            column_name,
            data_type,
            column_type,
            is_nullable,
            column_default,
            column_key,
            extra,
            ordinal_position
        FROM information_schema.columns
        WHERE table_schema = DATABASE()
          AND table_name = ?
        ORDER BY ordinal_position
    `, table.Name)

	if err != nil {
		return fmt.Errorf("discover columns for %s: %w", table.Name, err)
	}
	defer rows.Close()

	for rows.Next() {
		var column Column
		var nullable string
		var defaultValue sql.NullString

		if err := rows.Scan(
			&column.Name,
			&column.DataType,
			&column.ColumnType,
			&nullable,
			&defaultValue,
			&column.Key,
			&column.Extra,
			&column.OrdinalPosition,
		); err != nil {
			return fmt.Errorf("scan column for %s: %w", table.Name, err)
		}

		column.Nullable = nullable == "YES"

		if defaultValue.Valid {
			value := defaultValue.String
			column.DefaultValue = &value
		}

		table.Columns = append(table.Columns, column)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate columns for %s: %w", table.Name, err)
	}

	return nil
}

func discoverIndexes(
	ctx context.Context,
	db *sql.DB,
	table *Table,
) error {

	rows, err := db.QueryContext(ctx, `
        SELECT
    index_name,
    non_unique,
    column_name,
    expression,
    sub_part,
    collation,
    index_type
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = ?
ORDER BY index_name, seq_in_index
    `, table.Name)

	if err != nil {
		return fmt.Errorf("discover indexes for %s: %w", table.Name, err)
	}
	defer rows.Close()

	indexPositions := make(map[string]int)

	for rows.Next() {
		var (
			name         string
			nonUnique    int
			columnName   sql.NullString
			expression   sql.NullString
			prefixLength sql.NullInt64
			collation    sql.NullString
			indexType    string
		)

		if err := rows.Scan(
			&name,
			&nonUnique,
			&columnName,
			&expression,
			&prefixLength,
			&collation,
			&indexType,
		); err != nil {
			return fmt.Errorf("scan index for %s: %w", table.Name, err)
		}

		// Each index can contain multiple ordered parts.
		position, exists := indexPositions[name]

		if !exists {
			position = len(table.Indexes)
			indexPositions[name] = position

			table.Indexes = append(table.Indexes, Index{
				Name:    name,
				Unique:  nonUnique == 0,
				Primary: name == "PRIMARY",
				Type:    indexType,
				Columns: make([]string, 0),
				Parts:   make([]IndexPart, 0),
			})
		}

		index := &table.Indexes[position]
		part := IndexPart{}

		if columnName.Valid {
			part.Column = columnName.String
			index.Columns = append(index.Columns, columnName.String)
		} else if expression.Valid {
			part.Expression = expression.String
		} else {
			return fmt.Errorf(
				"index %s on table %s has neither a column nor an expression",
				name,
				table.Name,
			)
		}

		if prefixLength.Valid {
			part.PrefixLength = int(prefixLength.Int64)
		}

		if collation.Valid {
			switch strings.ToUpper(collation.String) {
			case "A":
				part.Order = "ASC"
			case "D":
				part.Order = "DESC"
			default:
				return fmt.Errorf(
					"index %s on table %s has unsupported sort direction %q",
					name,
					table.Name,
					collation.String,
				)
			}
		}

		index.Parts = append(index.Parts, part)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate indexes for %s: %w", table.Name, err)
	}

	return nil
}

func discoverForeignKeys(
	ctx context.Context,
	db *sql.DB,
	table *Table,
) error {

	rows, err := db.QueryContext(ctx, `
        SELECT
            kcu.constraint_name,
            kcu.column_name,
            kcu.referenced_table_name,
            kcu.referenced_column_name,
            rc.update_rule,
            rc.delete_rule
        FROM information_schema.key_column_usage kcu
        JOIN information_schema.referential_constraints rc
          ON rc.constraint_schema = kcu.constraint_schema
         AND rc.constraint_name = kcu.constraint_name
         AND rc.table_name = kcu.table_name
        WHERE kcu.constraint_schema = DATABASE()
          AND kcu.table_name = ?
          AND kcu.referenced_table_name IS NOT NULL
        ORDER BY kcu.constraint_name, kcu.ordinal_position
    `, table.Name)

	if err != nil {
		return fmt.Errorf(
			"discover foreign keys for %s: %w",
			table.Name,
			err,
		)
	}
	defer rows.Close()

	// Maps each constraint name to its position in table.ForeignKeys.
	foreignKeyPositions := make(map[string]int)

	for rows.Next() {
		var (
			name             string
			column           string
			referencedTable  string
			referencedColumn string
			updateRule       string
			deleteRule       string
		)

		if err := rows.Scan(
			&name,
			&column,
			&referencedTable,
			&referencedColumn,
			&updateRule,
			&deleteRule,
		); err != nil {
			return fmt.Errorf(
				"scan foreign key for %s: %w",
				table.Name,
				err,
			)
		}

		position, exists := foreignKeyPositions[name]

		if !exists {
			position = len(table.ForeignKeys)
			foreignKeyPositions[name] = position

			table.ForeignKeys = append(
				table.ForeignKeys,
				ForeignKey{
					Name:              name,
					ReferencedTable:   referencedTable,
					UpdateRule:        updateRule,
					DeleteRule:        deleteRule,
					Columns:           make([]string, 0),
					ReferencedColumns: make([]string, 0),
				},
			)
		}

		fk := &table.ForeignKeys[position]

		// Append columns in their original constraint order.
		fk.Columns = append(fk.Columns, column)
		fk.ReferencedColumns = append(
			fk.ReferencedColumns,
			referencedColumn,
		)

		// Maintain compatibility with existing single-column code.
		if len(fk.Columns) == 1 {
			fk.Column = column
			fk.ReferencedColumn = referencedColumn
		} else {
			fk.Column = ""
			fk.ReferencedColumn = ""
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf(
			"iterate foreign keys for %s: %w",
			table.Name,
			err,
		)
	}

	return nil
}
