package schemadiff

import (
	"strings"

	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
)

type TableDifference struct {
	Field  string `json:"field"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type ForeignKeyDifference struct {
	Name   string `json:"name"`
	Field  string `json:"field"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type Result struct {
	SourceDatabase   string            `json:"source_database"`
	TargetDatabase   string            `json:"target_database"`
	SourceOnlyTables []string          `json:"source_only_tables"`
	TargetOnlyTables []string          `json:"target_only_tables"`
	CommonTables     []TableComparison `json:"common_tables"`
}

type TableComparison struct {
	Name                       string                 `json:"name"`
	TableDifferences           []TableDifference      `json:"table_differences"`
	MissingColumnsInTarget     []string               `json:"missing_columns_in_target"`
	ExtraColumnsInTarget       []string               `json:"extra_columns_in_target"`
	ColumnDifferences          []ColumnDifference     `json:"column_differences"`
	MissingIndexesInTarget     []string               `json:"missing_indexes_in_target"`
	ExtraIndexesInTarget       []string               `json:"extra_indexes_in_target"`
	IndexDifferences           []IndexDifference      `json:"index_differences"`
	MissingForeignKeysInTarget []string               `json:"missing_foreign_keys_in_target"`
	ExtraForeignKeysInTarget   []string               `json:"extra_foreign_keys_in_target"`
	ForeignKeyDifferences      []ForeignKeyDifference `json:"foreign_key_differences"`
}

type ColumnDifference struct {
	ColumnName string `json:"column_name"`
	Field      string `json:"field"`
	Source     string `json:"source"`
	Target     string `json:"target"`
}

type IndexDefinition struct {
	Unique  bool                    `json:"unique"`
	Primary bool                    `json:"primary"`
	Type    string                  `json:"type,omitempty"`
	Columns []string                `json:"columns"`
	Parts   []mysqlclient.IndexPart `json:"parts,omitempty"`
}

type IndexDifference struct {
	IndexName string          `json:"index_name"`
	Source    IndexDefinition `json:"source"`
	Target    IndexDefinition `json:"target"`
}

func equalStringSlicesFold(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}

	return true
}

// indexParts returns the complete ordered index definition.
//
// Parts is authoritative when available. Columns is used as a
// backward-compatible fallback for older manually constructed schemas.
func indexParts(index mysqlclient.Index) []mysqlclient.IndexPart {
	if len(index.Parts) > 0 {
		return index.Parts
	}

	parts := make([]mysqlclient.IndexPart, 0, len(index.Columns))

	for _, column := range index.Columns {
		parts = append(parts, mysqlclient.IndexPart{
			Column: column,
		})
	}

	return parts
}

// equalIndexParts compares index parts in order.
// Column names and expressions are compared case-insensitively.
func equalIndexParts(
	a []mysqlclient.IndexPart,
	b []mysqlclient.IndexPart,
) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !strings.EqualFold(a[i].Column, b[i].Column) ||
			!strings.EqualFold(a[i].Expression, b[i].Expression) ||
			a[i].PrefixLength != b[i].PrefixLength ||
			!strings.EqualFold(a[i].Order, b[i].Order) {
			return false
		}
	}

	return true
}

func indexDefinition(index mysqlclient.Index) IndexDefinition {
	return IndexDefinition{
		Unique:  index.Unique,
		Primary: index.Primary,
		Type:    index.Type,
		Columns: index.Columns,
		Parts:   indexParts(index),
	}
}

func Compare(
	source *mysqlclient.Schema,
	target *mysqlclient.Schema,
) *Result {

	result := &Result{
		SourceDatabase:   source.Database,
		TargetDatabase:   target.Database,
		SourceOnlyTables: make([]string, 0),
		TargetOnlyTables: make([]string, 0),
		CommonTables:     make([]TableComparison, 0),
	}

	sourceTables := make(map[string]mysqlclient.Table)
	targetTables := make(map[string]mysqlclient.Table)

	// Index source tables.
	for _, table := range source.Tables {
		sourceTables[strings.ToLower(table.Name)] = table
	}

	// Index target tables.
	for _, table := range target.Tables {
		targetTables[strings.ToLower(table.Name)] = table
	}

	// Find source-only and common tables.
	for _, sourceTable := range source.Tables {

		key := strings.ToLower(sourceTable.Name)

		targetTable, exists := targetTables[key]

		if !exists {
			result.SourceOnlyTables = append(
				result.SourceOnlyTables,
				sourceTable.Name,
			)
			continue
		}

		comparison := compareTable(sourceTable, targetTable)

		result.CommonTables = append(
			result.CommonTables,
			comparison,
		)
	}

	// Find target-only tables.
	for _, targetTable := range target.Tables {

		key := strings.ToLower(targetTable.Name)

		if _, exists := sourceTables[key]; !exists {
			result.TargetOnlyTables = append(
				result.TargetOnlyTables,
				targetTable.Name,
			)
		}
	}

	return result
}

func compareTable(
	source mysqlclient.Table,
	target mysqlclient.Table,
) TableComparison {

	result := TableComparison{
		Name:                       source.Name,
		MissingColumnsInTarget:     make([]string, 0),
		ExtraColumnsInTarget:       make([]string, 0),
		ColumnDifferences:          make([]ColumnDifference, 0),
		MissingIndexesInTarget:     make([]string, 0),
		ExtraIndexesInTarget:       make([]string, 0),
		IndexDifferences:           make([]IndexDifference, 0),
		MissingForeignKeysInTarget: make([]string, 0),
		ExtraForeignKeysInTarget:   make([]string, 0),
		ForeignKeyDifferences:      make([]ForeignKeyDifference, 0),
		TableDifferences:           make([]TableDifference, 0),
	}

	compareTableProperties(&result, source, target)
	sourceColumns := make(map[string]mysqlclient.Column)
	targetColumns := make(map[string]mysqlclient.Column)

	for _, column := range source.Columns {
		sourceColumns[strings.ToLower(column.Name)] = column
	}

	for _, column := range target.Columns {
		targetColumns[strings.ToLower(column.Name)] = column
	}

	// Compare source columns against target.
	for _, sourceColumn := range source.Columns {

		key := strings.ToLower(sourceColumn.Name)

		targetColumn, exists := targetColumns[key]

		if !exists {
			result.MissingColumnsInTarget = append(
				result.MissingColumnsInTarget,
				sourceColumn.Name,
			)
			continue
		}

		compareColumn(
			&result,
			sourceColumn,
			targetColumn,
		)
	}

	// Find columns present only in target.
	for _, targetColumn := range target.Columns {

		key := strings.ToLower(targetColumn.Name)

		if _, exists := sourceColumns[key]; !exists {
			result.ExtraColumnsInTarget = append(
				result.ExtraColumnsInTarget,
				targetColumn.Name,
			)
		}
	}

	compareIndexes(
		&result,
		source.Indexes,
		target.Indexes,
	)

	compareForeignKeys(
		&result,
		source.ForeignKeys,
		target.ForeignKeys,
	)

	return result
}

func compareTableProperties(
	result *TableComparison,
	source mysqlclient.Table,
	target mysqlclient.Table,
) {
	compareField := func(field, sourceValue, targetValue string) {
		if !strings.EqualFold(sourceValue, targetValue) {
			result.TableDifferences = append(
				result.TableDifferences,
				TableDifference{
					Field:  field,
					Source: sourceValue,
					Target: targetValue,
				},
			)
		}
	}

	compareField("engine", source.Engine, target.Engine)
	compareField("charset", source.Charset, target.Charset)
	compareField("collation", source.Collation, target.Collation)
}

func compareColumn(
	result *TableComparison,
	source mysqlclient.Column,
	target mysqlclient.Column,
) {

	addDifference := func(field, sourceValue, targetValue string) {
		result.ColumnDifferences = append(
			result.ColumnDifferences,
			ColumnDifference{
				ColumnName: source.Name,
				Field:      field,
				Source:     sourceValue,
				Target:     targetValue,
			},
		)
	}

	if !strings.EqualFold(source.ColumnType, target.ColumnType) {
		addDifference(
			"column_type",
			source.ColumnType,
			target.ColumnType,
		)
	}

	if source.Nullable != target.Nullable {
		addDifference(
			"nullable",
			boolString(source.Nullable),
			boolString(target.Nullable),
		)
	}

	if !equalDefault(source.DefaultValue, target.DefaultValue) {
		addDifference(
			"default_value",
			defaultString(source.DefaultValue),
			defaultString(target.DefaultValue),
		)
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func defaultString(value *string) string {
	if value == nil {
		return "<NULL>"
	}
	return *value
}

func equalDefault(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return strings.EqualFold(*a, *b)
}

func compareIndexes(
	result *TableComparison,
	source []mysqlclient.Index,
	target []mysqlclient.Index,
) {
	sourceMap := make(map[string]mysqlclient.Index)
	targetMap := make(map[string]mysqlclient.Index)

	for _, index := range source {
		sourceMap[strings.ToLower(index.Name)] = index
	}

	for _, index := range target {
		targetMap[strings.ToLower(index.Name)] = index
	}

	// Compare source indexes against target indexes.
	for _, sourceIndex := range source {
		targetIndex, exists := targetMap[strings.ToLower(sourceIndex.Name)]

		if !exists {
			result.MissingIndexesInTarget = append(
				result.MissingIndexesInTarget,
				sourceIndex.Name,
			)
			continue
		}

		sourceParts := indexParts(sourceIndex)
		targetParts := indexParts(targetIndex)

		if sourceIndex.Unique != targetIndex.Unique ||
			sourceIndex.Primary != targetIndex.Primary ||
			!strings.EqualFold(sourceIndex.Type, targetIndex.Type) ||
			!equalIndexParts(sourceParts, targetParts) {

			result.IndexDifferences = append(
				result.IndexDifferences,
				IndexDifference{
					IndexName: sourceIndex.Name,
					Source:    indexDefinition(sourceIndex),
					Target:    indexDefinition(targetIndex),
				},
			)
		}
	}

	// Find indexes present only in target.
	for _, targetIndex := range target {
		if _, exists := sourceMap[strings.ToLower(targetIndex.Name)]; !exists {
			result.ExtraIndexesInTarget = append(
				result.ExtraIndexesInTarget,
				targetIndex.Name,
			)
		}
	}
}

func foreignKeyColumns(fk mysqlclient.ForeignKey) []string {
	if len(fk.Columns) > 0 {
		return fk.Columns
	}

	// Backward compatibility with existing single-column FK definitions.
	if fk.Column != "" {
		return []string{fk.Column}
	}

	return nil
}

func foreignKeyReferencedColumns(fk mysqlclient.ForeignKey) []string {
	if len(fk.ReferencedColumns) > 0 {
		return fk.ReferencedColumns
	}

	// Backward compatibility with existing single-column FK definitions.
	if fk.ReferencedColumn != "" {
		return []string{fk.ReferencedColumn}
	}

	return nil
}

func compareForeignKeys(
	result *TableComparison,
	source []mysqlclient.ForeignKey,
	target []mysqlclient.ForeignKey,
) {
	sourceMap := make(map[string]mysqlclient.ForeignKey)
	targetMap := make(map[string]mysqlclient.ForeignKey)

	for _, fk := range source {
		sourceMap[strings.ToLower(fk.Name)] = fk
	}

	for _, fk := range target {
		targetMap[strings.ToLower(fk.Name)] = fk
	}

	for _, sourceFK := range source {
		targetFK, exists := targetMap[strings.ToLower(sourceFK.Name)]

		if !exists {
			result.MissingForeignKeysInTarget = append(
				result.MissingForeignKeysInTarget,
				sourceFK.Name,
			)
			continue
		}

		compareField := func(
			field, sourceValue, targetValue string,
		) {
			if !strings.EqualFold(sourceValue, targetValue) {
				result.ForeignKeyDifferences = append(
					result.ForeignKeyDifferences,
					ForeignKeyDifference{
						Name:   sourceFK.Name,
						Field:  field,
						Source: sourceValue,
						Target: targetValue,
					},
				)
			}
		}

		sourceColumns := foreignKeyColumns(sourceFK)
		targetColumns := foreignKeyColumns(targetFK)

		columnField := "columns"
		if len(sourceColumns) <= 1 && len(targetColumns) <= 1 {
			columnField = "column"
		}

		if !equalStringSlicesFold(sourceColumns, targetColumns) {
			compareField(
				columnField,
				strings.Join(sourceColumns, ", "),
				strings.Join(targetColumns, ", "),
			)
		}

		compareField(
			"referenced_table",
			sourceFK.ReferencedTable,
			targetFK.ReferencedTable,
		)

		sourceReferencedColumns := foreignKeyReferencedColumns(sourceFK)
		targetReferencedColumns := foreignKeyReferencedColumns(targetFK)

		referencedColumnField := "referenced_columns"
		if len(sourceReferencedColumns) <= 1 &&
			len(targetReferencedColumns) <= 1 {
			referencedColumnField = "referenced_column"
		}

		if !equalStringSlicesFold(
			sourceReferencedColumns,
			targetReferencedColumns,
		) {
			compareField(
				referencedColumnField,
				strings.Join(sourceReferencedColumns, ", "),
				strings.Join(targetReferencedColumns, ", "),
			)
		}

		compareField(
			"update_rule",
			sourceFK.UpdateRule,
			targetFK.UpdateRule,
		)

		compareField(
			"delete_rule",
			sourceFK.DeleteRule,
			targetFK.DeleteRule,
		)
	}

	for _, targetFK := range target {
		if _, exists := sourceMap[strings.ToLower(targetFK.Name)]; !exists {
			result.ExtraForeignKeysInTarget = append(
				result.ExtraForeignKeysInTarget,
				targetFK.Name,
			)
		}
	}
}
