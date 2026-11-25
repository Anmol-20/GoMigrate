package migrationplan

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Anmol-20/gomigrate/internal/mysqlclient"
	"github.com/Anmol-20/gomigrate/internal/schemadiff"
)

type Generator struct{}

func NewGenerator() *Generator {
	return &Generator{}
}

func (g *Generator) Generate(
	source *mysqlclient.Schema,
	comparison *schemadiff.Result,
) (*Plan, error) {

	if source == nil || comparison == nil {
		return nil, fmt.Errorf("source schema and comparison result are required")
	}

	if err := validateForeignKeyReferences(source); err != nil {
		return nil, fmt.Errorf("validate source foreign keys: %w", err)
	}

	plan := &Plan{
		SourceDatabase: comparison.SourceDatabase,
		TargetDatabase: comparison.TargetDatabase,
		Operations:     make([]Operation, 0),
		Warnings:       make([]string, 0),
	}

	sourceTables := make(map[string]mysqlclient.Table)

	for _, table := range source.Tables {
		sourceTables[strings.ToLower(table.Name)] = table
	}

	// 1. Create missing tables in dependency-aware order.
	orderedTables, dependencyWarnings := orderNewTables(
		source,
		comparison.SourceOnlyTables,
	)

	plan.Warnings = append(plan.Warnings, dependencyWarnings...)

	for _, tableName := range orderedTables {
		table, exists := sourceTables[strings.ToLower(tableName)]
		if !exists {
			return nil, fmt.Errorf("source table %q not found in schema", tableName)
		}

		sql, err := createTableSQL(table)
		if err != nil {
			return nil, fmt.Errorf(
				"generate CREATE TABLE for %s: %w",
				table.Name,
				err,
			)
		}

		plan.addOperation(
			"CREATE_TABLE",
			table.Name,
			"Create table "+table.Name,
			sql,
		)
	}

	// 2. Add missing columns and modify changed columns.
	for _, tableComparison := range comparison.CommonTables {
		table, exists := sourceTables[strings.ToLower(tableComparison.Name)]
		if !exists {
			return nil, fmt.Errorf(
				"source table %q not found in schema",
				tableComparison.Name,
			)
		}

		sourceColumns := make(map[string]mysqlclient.Column)

		for _, column := range table.Columns {
			sourceColumns[strings.ToLower(column.Name)] = column
		}

		// Add columns that exist in source but not in target.
		for _, columnName := range tableComparison.MissingColumnsInTarget {
			column, exists := sourceColumns[strings.ToLower(columnName)]
			if !exists {
				return nil, fmt.Errorf(
					"source column %q not found in table %q",
					columnName,
					table.Name,
				)
			}

			definition, err := columnDefinition(column)
			if err != nil {
				return nil, err
			}

			sql := fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN %s",
				quoteIdentifier(table.Name),
				definition,
			)

			plan.addOperation(
				"ADD_COLUMN",
				table.Name,
				"Add column "+column.Name+" to "+table.Name,
				sql,
			)
		}

		// A column may have multiple differences (type, nullable, default).
		// Generate only one MODIFY COLUMN operation for that column.
		changedColumns := make(map[string]bool)

		for _, difference := range tableComparison.ColumnDifferences {
			changedColumns[strings.ToLower(difference.ColumnName)] = true
		}

		columnNames := make([]string, 0, len(changedColumns))
		for name := range changedColumns {
			columnNames = append(columnNames, name)
		}
		sort.Strings(columnNames)

		for _, columnName := range columnNames {
			column, exists := sourceColumns[columnName]
			if !exists {
				return nil, fmt.Errorf(
					"source column %q not found in table %q",
					columnName,
					table.Name,
				)
			}

			definition, err := columnDefinition(column)
			if err != nil {
				return nil, err
			}

			sql := fmt.Sprintf(
				"ALTER TABLE %s MODIFY COLUMN %s",
				quoteIdentifier(table.Name),
				definition,
			)

			plan.addOperation(
				"MODIFY_COLUMN",
				table.Name,
				"Modify column "+column.Name+" in "+table.Name,
				sql,
			)
		}

		// Table property differences require manual review.
		for _, difference := range tableComparison.TableDifferences {
			plan.Warnings = append(
				plan.Warnings,
				fmt.Sprintf(
					"Table %s has a different %s: source=%s, target=%s; manual review required",
					table.Name,
					difference.Field,
					difference.Source,
					difference.Target,
				),
			)
		}

		// Extra target columns are intentionally preserved.
		for _, columnName := range tableComparison.ExtraColumnsInTarget {
			plan.Warnings = append(
				plan.Warnings,
				fmt.Sprintf(
					"Table %s has extra target column %s; it will not be removed",
					table.Name,
					columnName,
				),
			)
		}

		// 3. Add indexes missing from the target.
		for _, indexName := range tableComparison.MissingIndexesInTarget {
			index, exists := findIndex(table.Indexes, indexName)
			if !exists {
				return nil, fmt.Errorf(
					"source index %q not found in table %q",
					indexName,
					table.Name,
				)
			}

			sql, err := createIndexSQL(table.Name, index, table.Columns)
			if err != nil {
				return nil, err
			}

			plan.addOperation(
				"ADD_INDEX",
				table.Name,
				"Add index "+index.Name+" to "+table.Name,
				sql,
			)
		}

		// Changed index definitions need a more careful replacement plan.
		for _, difference := range tableComparison.IndexDifferences {
			plan.Warnings = append(
				plan.Warnings,
				fmt.Sprintf(
					"Index %s on table %s has a different definition; manual review required",
					difference.IndexName,
					table.Name,
				),
			)
		}

		for _, indexName := range tableComparison.ExtraIndexesInTarget {
			plan.Warnings = append(
				plan.Warnings,
				fmt.Sprintf(
					"Table %s has extra target index %s; it will not be removed",
					table.Name,
					indexName,
				),
			)
		}
	}

	// 4. Add foreign keys only after all CREATE TABLE operations.
	// This allows a newly created table to reference another newly created table.
	for _, table := range source.Tables {
		isNewTable := containsFold(
			comparison.SourceOnlyTables,
			table.Name,
		)

		var missingForeignKeys []string

		if isNewTable {
			for _, fk := range table.ForeignKeys {
				missingForeignKeys = append(missingForeignKeys, fk.Name)
			}
		} else {
			tableComparison, exists := findTableComparison(
				comparison.CommonTables,
				table.Name,
			)
			if !exists {
				continue
			}

			missingForeignKeys = tableComparison.MissingForeignKeysInTarget

			for _, fk := range tableComparison.ForeignKeyDifferences {
				plan.Warnings = append(
					plan.Warnings,
					fmt.Sprintf(
						"Foreign key %s on table %s differs in field %s; manual review required",
						fk.Name,
						table.Name,
						fk.Field,
					),
				)
			}

			for _, fkName := range tableComparison.ExtraForeignKeysInTarget {
				plan.Warnings = append(
					plan.Warnings,
					fmt.Sprintf(
						"Table %s has extra target foreign key %s; it will not be removed",
						table.Name,
						fkName,
					),
				)
			}
		}

		for _, fkName := range missingForeignKeys {
			fk, exists := findForeignKey(table.ForeignKeys, fkName)
			if !exists {
				return nil, fmt.Errorf(
					"source foreign key %q not found in table %q",
					fkName,
					table.Name,
				)
			}

			sql, err := createForeignKeySQL(table.Name, fk)
			if err != nil {
				return nil, err
			}

			plan.addOperation(
				"ADD_FOREIGN_KEY",
				table.Name,
				"Add foreign key "+fk.Name+" to "+table.Name,
				sql,
			)
		}
	}

	// 5. Preserve target-only tables and report them.
	for _, tableName := range comparison.TargetOnlyTables {
		plan.Warnings = append(
			plan.Warnings,
			fmt.Sprintf(
				"Target-only table %s will be preserved",
				tableName,
			),
		)
	}

	classifyPlan(plan)
	buildImpactSummary(plan)

	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("generated migration plan failed validation: %w", err)
	}

	return plan, nil
}

func normalizeIndexType(index mysqlclient.Index) (string, error) {
	indexType := strings.ToUpper(strings.TrimSpace(index.Type))

	if indexType == "" {
		return "BTREE", nil
	}

	switch indexType {
	case "BTREE", "HASH":
		if index.Unique && index.Primary && indexType != "BTREE" {
			return "", fmt.Errorf(
				"primary index %q cannot use index type %s",
				index.Name,
				indexType,
			)
		}
		return indexType, nil

	case "FULLTEXT", "RTREE":
		if index.Unique || index.Primary {
			return "", fmt.Errorf(
				"index %q of type %s cannot be unique or primary",
				index.Name,
				indexType,
			)
		}
		return indexType, nil

	default:
		return "", fmt.Errorf(
			"index %q has unsupported index type %q",
			index.Name,
			index.Type,
		)
	}
}

func indexKeyword(index mysqlclient.Index) (string, error) {
	indexType, err := normalizeIndexType(index)
	if err != nil {
		return "", err
	}

	if index.Primary {
		return "PRIMARY KEY", nil
	}

	switch indexType {
	case "FULLTEXT":
		return "FULLTEXT KEY", nil
	case "RTREE":
		return "SPATIAL KEY", nil
	default:
		if index.Unique {
			return "UNIQUE KEY", nil
		}
		return "KEY", nil
	}
}

func (p *Plan) addOperation(
	operationType string,
	table string,
	description string,
	sql string,
) {
	operation := Operation{
		ID:          len(p.Operations) + 1,
		Type:        operationType,
		Table:       table,
		Description: description,
		SQL:         sql,
	}

	classifyOperation(&operation)

	p.Operations = append(p.Operations, operation)
}

type indexExpressionParser struct {
	tokens    []string
	position  int
	functions map[string]bool
	columns   []string
}

func validateIndexExpression(expression string) error {
	_, hasFunction, err := parseIndexExpression(expression)
	if err != nil {
		return err
	}

	if !hasFunction {
		return fmt.Errorf("index expression must contain a supported function")
	}

	return nil
}

func validateIndexExpressionColumns(
	expression string,
	tableColumns []mysqlclient.Column,
) error {
	referencedColumns, _, err := parseIndexExpression(expression)
	if err != nil {
		return err
	}

	availableColumns := make(map[string]bool, len(tableColumns))

	for _, column := range tableColumns {
		availableColumns[strings.ToLower(column.Name)] = true
	}

	for _, column := range referencedColumns {
		if !availableColumns[strings.ToLower(column)] {
			return fmt.Errorf(
				"index expression references unknown column %q",
				column,
			)
		}
	}

	return nil
}

func parseIndexExpression(expression string) ([]string, bool, error) {
	expression = strings.TrimSpace(expression)

	if expression == "" {
		return nil, false, fmt.Errorf("index expression cannot be empty")
	}

	upper := strings.ToUpper(expression)

	if strings.Contains(expression, ";") ||
		strings.Contains(expression, "--") ||
		strings.Contains(expression, "/*") ||
		strings.Contains(expression, "*/") ||
		strings.Contains(upper, "#") {
		return nil, false, fmt.Errorf("index expression contains unsupported SQL syntax")
	}

	tokens, err := tokenizeIndexExpression(expression)
	if err != nil {
		return nil, false, err
	}

	parser := &indexExpressionParser{
		tokens: tokens,
		functions: map[string]bool{
			"LOWER": true,
			"UPPER": true,
			"YEAR":  true,
			"ABS":   true,
		},
		columns: make([]string, 0),
	}

	hasFunction := false

	if err := parser.parseNode(&hasFunction); err != nil {
		return nil, false, err
	}

	if parser.position != len(parser.tokens) {
		return nil, false, fmt.Errorf(
			"unexpected token %q in index expression",
			parser.tokens[parser.position],
		)
	}

	return parser.columns, hasFunction, nil
}

func tokenizeIndexExpression(expression string) ([]string, error) {
	tokens := make([]string, 0)

	for i := 0; i < len(expression); {
		ch := expression[i]

		// Ignore whitespace.
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			i++
			continue
		}

		// Parentheses are separate tokens.
		if ch == '(' || ch == ')' {
			tokens = append(tokens, string(ch))
			i++
			continue
		}

		// Handle backtick-quoted identifiers.
		if ch == '`' {
			i++

			var identifier strings.Builder
			closed := false

			for i < len(expression) {
				if expression[i] == '`' {
					// MySQL escapes a backtick using two backticks.
					if i+1 < len(expression) && expression[i+1] == '`' {
						identifier.WriteByte('`')
						i += 2
						continue
					}

					i++
					closed = true
					break
				}

				identifier.WriteByte(expression[i])
				i++
			}

			if !closed || identifier.Len() == 0 {
				return nil, fmt.Errorf(
					"invalid or unterminated quoted identifier in index expression",
				)
			}

			tokens = append(tokens, identifier.String())
			continue
		}

		// Handle ordinary identifiers.
		if isIndexIdentifierStart(ch) {
			start := i
			i++

			for i < len(expression) && isIndexIdentifierPart(expression[i]) {
				i++
			}

			tokens = append(tokens, expression[start:i])
			continue
		}

		return nil, fmt.Errorf(
			"unsupported character %q in index expression",
			ch,
		)
	}

	if len(tokens) == 0 {
		return nil, fmt.Errorf("index expression cannot be empty")
	}

	return tokens, nil
}

func isIndexIdentifierStart(ch byte) bool {
	return ch == '_' ||
		ch == '$' ||
		(ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z')
}

func isIndexIdentifierPart(ch byte) bool {
	return isIndexIdentifierStart(ch) ||
		(ch >= '0' && ch <= '9')
}

func (p *indexExpressionParser) parseNode(hasFunction *bool) error {
	if p.position >= len(p.tokens) {
		return fmt.Errorf("expected a column or function in index expression")
	}

	name := p.tokens[p.position]
	p.position++

	// An identifier followed by '(' represents a function call.
	if p.position < len(p.tokens) && p.tokens[p.position] == "(" {
		functionName := strings.ToUpper(name)

		if !p.functions[functionName] {
			return fmt.Errorf(
				"unsupported function %q in index expression",
				functionName,
			)
		}

		*hasFunction = true
		p.position++

		// Parse the function argument.
		if err := p.parseNode(hasFunction); err != nil {
			return err
		}

		// Every function must have a closing parenthesis.
		if p.position >= len(p.tokens) || p.tokens[p.position] != ")" {
			return fmt.Errorf(
				"missing closing parenthesis for function %q",
				functionName,
			)
		}

		p.position++
		return nil
	}

	// Otherwise, this identifier represents a column reference.
	p.columns = append(p.columns, name)

	return nil
}

func indexPartsSQL(
	index mysqlclient.Index,
	tableColumns ...[]mysqlclient.Column,
) (string, error) {
	parts := index.Parts

	if len(parts) == 0 {
		parts = make([]mysqlclient.IndexPart, 0, len(index.Columns))

		for _, column := range index.Columns {
			parts = append(parts, mysqlclient.IndexPart{
				Column: column,
			})
		}
	}

	if len(parts) == 0 {
		return "", fmt.Errorf(
			"index %q has no columns or expressions",
			index.Name,
		)
	}

	sqlParts := make([]string, 0, len(parts))

	for _, part := range parts {
		column := strings.TrimSpace(part.Column)
		expression := strings.TrimSpace(part.Expression)

		if column != "" && expression != "" {
			return "", fmt.Errorf(
				"index %q has a part containing both a column and an expression",
				index.Name,
			)
		}

		var partSQL string

		switch {
		case column != "":
			if part.PrefixLength < 0 {
				return "", fmt.Errorf(
					"index %q has a negative prefix length",
					index.Name,
				)
			}

			if index.Primary && part.PrefixLength > 0 {
				return "", fmt.Errorf(
					"primary index %q cannot use prefix lengths",
					index.Name,
				)
			}

			partSQL = quoteIdentifier(column)

			if part.PrefixLength > 0 {
				partSQL += fmt.Sprintf("(%d)", part.PrefixLength)
			}

		case expression != "":
			if part.PrefixLength != 0 {
				return "", fmt.Errorf(
					"functional index %q cannot use a prefix length",
					index.Name,
				)
			}

			var validationErr error

			if len(tableColumns) > 0 {
				validationErr = validateIndexExpressionColumns(
					expression,
					tableColumns[0],
				)
			} else {
				validationErr = validateIndexExpression(expression)
			}

			if validationErr != nil {
				return "", fmt.Errorf(
					"invalid expression in index %q: %w",
					index.Name,
					validationErr,
				)
			}

			// MySQL requires an additional pair of parentheses
			// around functional index expressions.
			partSQL = "(" + expression + ")"

		default:
			return "", fmt.Errorf(
				"index %q contains an empty column or expression",
				index.Name,
			)
		}

		direction := strings.ToUpper(strings.TrimSpace(part.Order))

		switch direction {
		case "", "ASC":
		case "DESC":
			partSQL += " DESC"
		default:
			return "", fmt.Errorf(
				"index %q has unsupported sort direction %q",
				index.Name,
				part.Order,
			)
		}

		sqlParts = append(sqlParts, partSQL)
	}

	return strings.Join(sqlParts, ", "), nil
}

func createTableSQL(table mysqlclient.Table) (string, error) {
	// Only BASE TABLE objects are supported.
	// Do not accidentally generate CREATE TABLE for views.
	if !strings.EqualFold(table.Type, "BASE TABLE") {
		return "", fmt.Errorf(
			"unsupported object type %q for %q; only BASE TABLE is supported",
			table.Type,
			table.Name,
		)
	}

	definitions := make([]string, 0, len(table.Columns)+len(table.Indexes))

	for _, column := range table.Columns {
		definition, err := columnDefinition(column)
		if err != nil {
			return "", err
		}
		definitions = append(definitions, definition)
	}

	// Include indexes in CREATE TABLE for new tables.
	for _, index := range table.Indexes {
		definition, err := indexDefinition(index, table.Columns)
		if err != nil {
			return "", err
		}
		definitions = append(definitions, definition)
	}

	if len(definitions) == 0 {
		return "", fmt.Errorf("table %q has no columns", table.Name)
	}

	// Validate table properties before including them in SQL.
	if !isValidSQLIdentifier(table.Engine) {
		return "", fmt.Errorf("invalid or missing storage engine %q for table %q",
			table.Engine, table.Name)
	}

	if (table.Charset == "") != (table.Collation == "") {
		return "", fmt.Errorf(
			"table %q must have both charset and collation, or neither",
			table.Name,
		)
	}

	tableOptions := " ENGINE=" + table.Engine

	if table.Charset != "" {
		if !isValidSQLIdentifier(table.Charset) {
			return "", fmt.Errorf(
				"invalid charset %q for table %q",
				table.Charset,
				table.Name,
			)
		}

		if !isValidSQLIdentifier(table.Collation) {
			return "", fmt.Errorf(
				"invalid collation %q for table %q",
				table.Collation,
				table.Name,
			)
		}

		tableOptions += " DEFAULT CHARACTER SET=" + table.Charset
		tableOptions += " COLLATE=" + table.Collation
	}

	return fmt.Sprintf(
		"CREATE TABLE %s (\n    %s\n)%s",
		quoteIdentifier(table.Name),
		strings.Join(definitions, ",\n    "),
		tableOptions,
	), nil
}

func isValidSQLIdentifier(value string) bool {
	matched, _ := regexp.MatchString(`^[A-Za-z0-9_]+$`, value)
	return matched
}

func columnDefinition(column mysqlclient.Column) (string, error) {
	if strings.TrimSpace(column.ColumnType) == "" {
		return "", fmt.Errorf("column %q has an empty column type", column.Name)
	}

	definition := fmt.Sprintf(
		"%s %s",
		quoteIdentifier(column.Name),
		column.ColumnType,
	)

	if column.Nullable {
		definition += " NULL"
	} else {
		definition += " NOT NULL"
	}

	if column.DefaultValue != nil {
		defaultSQL, err := formatDefault(*column.DefaultValue, column.DataType)
		if err != nil {
			return "", fmt.Errorf("column %q: %w", column.Name, err)
		}
		definition += " DEFAULT " + defaultSQL
	}

	extra := strings.ToLower(column.Extra)

	if strings.Contains(extra, "auto_increment") {
		definition += " AUTO_INCREMENT"
	}

	if strings.Contains(extra, "on update current_timestamp") {
		definition += " ON UPDATE CURRENT_TIMESTAMP"
	}

	return definition, nil
}

func formatDefault(value string, dataType string) (string, error) {
	trimmed := strings.TrimSpace(value)
	upper := strings.ToUpper(trimmed)

	if upper == "NULL" {
		return "NULL", nil
	}

	if strings.HasPrefix(upper, "CURRENT_TIMESTAMP") ||
		upper == "CURRENT_DATE" ||
		upper == "CURRENT_TIME" {
		return trimmed, nil
	}

	numericTypes := map[string]bool{
		"tinyint":   true,
		"smallint":  true,
		"mediumint": true,
		"int":       true,
		"integer":   true,
		"bigint":    true,
		"decimal":   true,
		"numeric":   true,
		"float":     true,
		"double":    true,
		"real":      true,
		"bit":       true,
		"bool":      true,
		"boolean":   true,
	}

	if numericTypes[strings.ToLower(dataType)] {
		matched, _ := regexp.MatchString(
			`^[+-]?[0-9]+(\.[0-9]+)?$`,
			trimmed,
		)
		if !matched {
			return "", fmt.Errorf("unsupported numeric default %q", value)
		}
		return trimmed, nil
	}

	// Treat other defaults as string literals.
	escaped := strings.ReplaceAll(trimmed, "'", "''")
	return "'" + escaped + "'", nil
}

func indexDefinition(
	index mysqlclient.Index,
	tableColumns ...[]mysqlclient.Column,
) (string, error) {
	parts, err := indexPartsSQL(index, tableColumns...)
	if err != nil {
		return "", err
	}

	keyword, err := indexKeyword(index)
	if err != nil {
		return "", err
	}

	if index.Primary {
		return keyword + " (" + parts + ")", nil
	}

	definition := fmt.Sprintf(
		"%s %s (%s)",
		keyword,
		quoteIdentifier(index.Name),
		parts,
	)

	indexType, _ := normalizeIndexType(index)

	if indexType == "HASH" {
		definition += " USING HASH"
	}

	return definition, nil
}

func createIndexSQL(
	tableName string,
	index mysqlclient.Index,
	tableColumns ...[]mysqlclient.Column,
) (string, error) {
	if index.Primary {
		return "", fmt.Errorf(
			"primary index %q should be created with its table",
			index.Name,
		)
	}

	parts, err := indexPartsSQL(index, tableColumns...)
	if err != nil {
		return "", err
	}

	indexType, err := normalizeIndexType(index)
	if err != nil {
		return "", err
	}

	var kind string

	switch indexType {
	case "FULLTEXT":
		kind = "FULLTEXT INDEX"
	case "RTREE":
		kind = "SPATIAL INDEX"
	default:
		if index.Unique {
			kind = "UNIQUE INDEX"
		} else {
			kind = "INDEX"
		}
	}

	using := ""
	if indexType == "HASH" {
		using = " USING HASH"
	}

	return fmt.Sprintf(
		"CREATE %s %s%s ON %s (%s)",
		kind,
		quoteIdentifier(index.Name),
		using,
		quoteIdentifier(tableName),
		parts,
	), nil
}

func createForeignKeySQL(
	tableName string,
	fk mysqlclient.ForeignKey,
) (string, error) {
	updateRule, err := normalizeForeignKeyRule(fk.UpdateRule)
	if err != nil {
		return "", err
	}

	deleteRule, err := normalizeForeignKeyRule(fk.DeleteRule)
	if err != nil {
		return "", err
	}

	columns := fk.Columns
	if len(columns) == 0 && fk.Column != "" {
		// Backward compatibility with existing single-column definitions.
		columns = []string{fk.Column}
	}

	referencedColumns := fk.ReferencedColumns
	if len(referencedColumns) == 0 && fk.ReferencedColumn != "" {
		referencedColumns = []string{fk.ReferencedColumn}
	}

	if len(columns) == 0 {
		return "", fmt.Errorf(
			"foreign key %q has no columns",
			fk.Name,
		)
	}

	if len(referencedColumns) == 0 {
		return "", fmt.Errorf(
			"foreign key %q has no referenced columns",
			fk.Name,
		)
	}

	if len(columns) != len(referencedColumns) {
		return "", fmt.Errorf(
			"foreign key %q has %d columns but %d referenced columns",
			fk.Name,
			len(columns),
			len(referencedColumns),
		)
	}

	for _, column := range columns {
		if strings.TrimSpace(column) == "" {
			return "", fmt.Errorf(
				"foreign key %q contains an empty column name",
				fk.Name,
			)
		}
	}

	for _, column := range referencedColumns {
		if strings.TrimSpace(column) == "" {
			return "", fmt.Errorf(
				"foreign key %q contains an empty referenced column name",
				fk.Name,
			)
		}
	}

	if strings.TrimSpace(fk.ReferencedTable) == "" {
		return "", fmt.Errorf(
			"foreign key %q has no referenced table",
			fk.Name,
		)
	}

	return fmt.Sprintf(
		"ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s) ON UPDATE %s ON DELETE %s",
		quoteIdentifier(tableName),
		quoteIdentifier(fk.Name),
		quoteIdentifiers(columns),
		quoteIdentifier(fk.ReferencedTable),
		quoteIdentifiers(referencedColumns),
		updateRule,
		deleteRule,
	), nil
}

func normalizeForeignKeyRule(rule string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(rule))

	allowed := map[string]bool{
		"RESTRICT":    true,
		"CASCADE":     true,
		"SET NULL":    true,
		"NO ACTION":   true,
		"SET DEFAULT": true,
	}

	if !allowed[normalized] {
		return "", fmt.Errorf("unsupported foreign key rule %q", rule)
	}

	return normalized, nil
}

func quoteIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func quoteIdentifiers(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, quoteIdentifier(value))
	}
	return strings.Join(quoted, ", ")
}

func findIndex(indexes []mysqlclient.Index, name string) (mysqlclient.Index, bool) {
	for _, index := range indexes {
		if strings.EqualFold(index.Name, name) {
			return index, true
		}
	}
	return mysqlclient.Index{}, false
}

func findForeignKey(
	foreignKeys []mysqlclient.ForeignKey,
	name string,
) (mysqlclient.ForeignKey, bool) {
	for _, fk := range foreignKeys {
		if strings.EqualFold(fk.Name, name) {
			return fk, true
		}
	}
	return mysqlclient.ForeignKey{}, false
}

func findTableComparison(
	comparisons []schemadiff.TableComparison,
	name string,
) (schemadiff.TableComparison, bool) {
	for _, comparison := range comparisons {
		if strings.EqualFold(comparison.Name, name) {
			return comparison, true
		}
	}
	return schemadiff.TableComparison{}, false
}

func containsFold(values []string, value string) bool {
	for _, item := range values {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}
