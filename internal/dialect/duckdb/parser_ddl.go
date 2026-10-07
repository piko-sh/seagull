// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package duckdb

import (
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// typeNormaliser narrows column-type parsing to a single dependency: resolving raw SQL
// type names into structured SQLType values. Dialect satisfies the interface.
type typeNormaliser interface {
	// NormaliseTypeName resolves a raw SQL type name to a structured SQLType.
	//
	// Takes name (string) which is the raw type name as written.
	// Takes modifiers (...int) which holds the numeric type modifiers, such as precision and
	// scale.
	//
	// Returns sqlmodel.SQLType which is the normalised type.
	NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType
}

// tableBody accumulates the elements of a CREATE TABLE body.
type tableBody struct {
	// columns holds the columns in declaration order.
	columns []sqlmodel.Column

	// columnPrimaryKey holds the columns declared PRIMARY KEY on their own definition.
	columnPrimaryKey []string

	// tablePrimaryKey holds the columns of a table-level PRIMARY KEY constraint.
	tablePrimaryKey []string

	// constraints holds the table-level constraints other than the primary key.
	constraints []sqlmodel.Constraint
}

// parseCreateTable parses a CREATE [OR REPLACE] [TEMP] TABLE statement into a mutation.
//
// CREATE OR REPLACE TABLE drops any existing table of the same name before creating the
// new one, so the mutation is a drop with the create as its follow-up.
//
// Takes engine (typeNormaliser) which resolves raw column type names.
//
// Returns *ast.CatalogueMutation which describes the table to create, including columns,
// primary key, and constraints.
// Returns error when the table name or body cannot be parsed.
func (p *parser) parseCreateTable(engine typeNormaliser) (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}

	replaceExisting := p.skipOrReplace()

	p.MatchKeyword("TEMP")
	p.MatchKeyword("TEMPORARY")
	if _, err := p.ExpectKeyword(keywordTABLE); err != nil {
		return nil, err
	}

	ifNotExists := p.matchIfNotExists()

	schema, tableName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	create, createError := p.parseCreateTableDefinition(engine, schema, tableName)
	if create != nil {
		create.IfNotExists = ifNotExists
	}
	if createError != nil || !replaceExisting {
		return create, createError
	}

	return ast.NewCatalogueMutation(
		ast.MutationDropTable,
		schema,
		tableName,
		ast.WithIfExists(true),
		withAdditionalMutation(create),
	), nil
}

// parseCreateTableDefinition parses an AS query or parenthesised column and constraint
// list following the table name in a CREATE TABLE statement.
//
// Takes engine (typeNormaliser) which resolves raw column type names.
// Takes schema (string) which is the table's schema, or empty for the default.
// Takes tableName (string) which is the table's name.
//
// Returns *ast.CatalogueMutation which describes the table to create.
// Returns error when the body cannot be parsed.
func (p *parser) parseCreateTableDefinition(
	engine typeNormaliser,
	schema, tableName string,
) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword(keywordAS) {
		analysis, unanalysed, err := p.analyseQueryBody("the table's query")
		if err != nil {
			return nil, err
		}
		mutation := ast.NewCatalogueMutation(ast.MutationCreateTable, schema, tableName, withUnanalysedBody(unanalysed))
		mutation.ViewDefinition = analysis
		return mutation, nil
	}

	if p.Current().Kind != tokenLeftParen {
		return nil, p.syntaxErrorAt("expected '(' or AS after the table name")
	}
	p.Advance()

	columns, primaryKeyColumns, constraints, bodyError := p.parseCreateTableBody(engine)
	if bodyError != nil {
		return nil, bodyError
	}

	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxErrorAt("expected ')' to close the table definition")
	}
	p.Advance()

	if err := p.requireEnd(); err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(
		ast.MutationCreateTable,
		schema,
		tableName,
		ast.WithColumns(columns),
		ast.WithPrimaryKey(primaryKeyColumns),
		ast.WithConstraints(constraints),
	), nil
}

// parseCreateTableBody walks the parenthesised column and constraint list of a CREATE
// TABLE statement. Each element must be followed by a comma or the closing parenthesis,
// so a clause the parser does not know fails the statement instead of being read as a
// further column.
//
// Takes engine (typeNormaliser) which resolves raw column type names.
//
// Returns []sqlmodel.Column which is the parsed column list.
// Returns []string which is the primary key column list, derived from either column-level
// PRIMARY KEY or a table-level constraint.
// Returns []sqlmodel.Constraint which is the list of table-level constraints other than
// the primary key.
// Returns error when a column or constraint cannot be parsed.
func (p *parser) parseCreateTableBody(
	engine typeNormaliser,
) ([]sqlmodel.Column, []string, []sqlmodel.Constraint, error) {
	var body tableBody

	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if err := p.parseTableElement(engine, &body); err != nil {
			return nil, nil, nil, err
		}
		if err := p.requireElementSeparator(); err != nil {
			return nil, nil, nil, err
		}
	}

	primaryKeyColumns := body.columnPrimaryKey
	if len(body.tablePrimaryKey) > 0 {
		primaryKeyColumns = body.tablePrimaryKey
		for index := range body.columns {
			if slices.Contains(body.tablePrimaryKey, body.columns[index].Name) {
				body.columns[index].Nullable = false
			}
		}
	}

	return body.columns, primaryKeyColumns, body.constraints, nil
}

// parseTableElement parses one element of a CREATE TABLE body: a table constraint or a
// column definition.
//
// Takes engine (typeNormaliser) which resolves raw column type names.
// Takes body (*tableBody) which receives the element.
//
// Returns error when the element cannot be parsed.
func (p *parser) parseTableElement(engine typeNormaliser, body *tableBody) error {
	if p.isDuckDBTableConstraint() {
		constraintPrimaryKey, constraint, err := p.parseDuckDBTableConstraint()
		if err != nil {
			return err
		}
		body.tablePrimaryKey = appendConstraintPrimaryKey(body.tablePrimaryKey, constraintPrimaryKey)
		body.constraints = appendConstraint(body.constraints, constraint)
		return nil
	}
	column, columnPrimaryKey, err := p.parseDuckDBColumnDefinition(engine)
	if err != nil {
		return err
	}
	body.columns = append(body.columns, column)
	if columnPrimaryKey {
		body.columnPrimaryKey = append(body.columnPrimaryKey, column.Name)
	}
	return nil
}

// requireElementSeparator consumes the comma after a table element, and records a syntax
// error naming the token when neither a comma nor the closing parenthesis follows it.
//
// Returns error when the element is followed by anything else.
func (p *parser) requireElementSeparator() error {
	if p.Current().Kind == tokenComma {
		p.Advance()
		return nil
	}
	if p.Current().Kind == tokenRightParen {
		return nil
	}
	return p.syntaxErrorAt("expected ',' or ')' after the table element")
}

// parseDuckDBColumnDefinition parses a single column definition, including its type,
// array suffix, and constraint suffixes.
//
// Takes engine (typeNormaliser) which resolves the raw type name.
//
// Returns sqlmodel.Column which is the parsed column descriptor.
// Returns bool which is true when the column-level PRIMARY KEY constraint applied to this
// column.
// Returns error when the column name cannot be parsed.
func (p *parser) parseDuckDBColumnDefinition(engine typeNormaliser) (sqlmodel.Column, bool, error) {
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		p.recordSyntaxError(err)
		return sqlmodel.Column{}, false, err
	}

	sqlType, arrayDimensions := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), 0
	if !p.IsKeyword(keywordAS, keywordGENERATED) {
		sqlType, arrayDimensions = p.parseColumnType(engine)
	}

	column := sqlmodel.NewColumn(name, sqlType, true)
	column.IsArray = arrayDimensions > 0
	column.ArrayDimensions = arrayDimensions

	isPrimaryKey, constraintError := p.parseColumnConstraints(&column)
	if constraintError != nil {
		return sqlmodel.Column{}, false, constraintError
	}

	return column, isPrimaryKey, nil
}

// parseColumnConstraints reads zero or more column-level constraint suffixes, mutating
// column in place.
//
// Takes column (*sqlmodel.Column) which receives nullability, default, and generated
// state.
//
// Returns bool which is true when a PRIMARY KEY suffix was seen.
// Returns error when a constraint is malformed.
func (p *parser) parseColumnConstraints(column *sqlmodel.Column) (bool, error) {
	isPrimaryKey := false

	for !p.AtEnd() && p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen {
		primary, handled, err := p.parseOneDuckDBColumnConstraint(column)
		if err != nil {
			return false, err
		}
		if primary {
			isPrimaryKey = true
		}
		if !handled {
			break
		}
	}

	return isPrimaryKey, nil
}

// parseOneDuckDBColumnConstraint consumes one column-level constraint when the current
// token introduces one.
//
// Takes column (*sqlmodel.Column) which is mutated according to the constraint.
//
// Returns isPrimary (bool) which is true when PRIMARY KEY was seen.
// Returns handled (bool) which is true when a constraint was consumed.
// Returns err (error) when the constraint is malformed.
func (p *parser) parseOneDuckDBColumnConstraint(column *sqlmodel.Column) (isPrimary bool, handled bool, err error) {
	if p.MatchKeyword(keywordPRIMARY) {
		if _, err := p.ExpectKeyword(keywordKEY); err != nil {
			p.recordSyntaxError(err)
			return false, false, err
		}
		column.Nullable = false
		return true, true, nil
	}

	if p.MatchKeyword(keywordNOT) {
		if _, err := p.ExpectKeyword(keywordNULL); err != nil {
			p.recordSyntaxError(err)
			return false, false, err
		}
		column.Nullable = false
		return false, true, nil
	}

	if p.MatchKeyword(keywordAS) {
		return false, true, p.parseGeneratedExpression(column)
	}

	if p.MatchKeyword(keywordNULL) {
		column.Nullable = true
		return false, true, nil
	}

	if p.MatchKeyword(keywordUNIQUE) {
		return false, true, nil
	}

	if p.MatchKeyword(keywordCHECK) {
		return false, true, p.skipParenthesised()
	}

	if p.MatchKeyword(keywordDEFAULT) {
		column.HasDefault = true
		return false, true, p.skipDuckDBDefaultValue()
	}

	handled, err = p.parseDuckDBSecondaryConstraint(column)
	return false, handled, err
}

// skipParenthesisedIfPresent consumes a balanced parenthesised group when the current
// token opens one.
//
// Returns error when the group is unbalanced.
func (p *parser) skipParenthesisedIfPresent() error {
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	return p.skipParenthesised()
}

// parseDuckDBSecondaryConstraint handles the less common column-level constraints
// (REFERENCES, GENERATED, COLLATE, CONSTRAINT).
//
// Takes column (*sqlmodel.Column) which is mutated as appropriate.
//
// Returns bool which is true when a constraint was consumed.
// Returns error when the constraint is malformed.
func (p *parser) parseDuckDBSecondaryConstraint(column *sqlmodel.Column) (bool, error) {
	if p.MatchKeyword(keywordREFERENCES) {
		return true, p.skipDuckDBForeignKeyClause()
	}

	if p.MatchKeyword(keywordGENERATED) {
		return true, p.parseGeneratedClause(column)
	}

	if p.MatchKeyword("COLLATE") {
		p.Advance()
		return true, nil
	}

	if p.MatchKeyword(keywordCONSTRAINT) {
		p.Advance()
		return true, nil
	}

	return false, nil
}

// parseGeneratedClause parses the GENERATED ALWAYS or GENERATED BY DEFAULT clause on a
// column.
//
// Takes column (*sqlmodel.Column) which is mutated when a generated or identity clause is
// recognised.
//
// Returns error when a parenthesised option or expression is unbalanced.
func (p *parser) parseGeneratedClause(column *sqlmodel.Column) error {
	if p.MatchKeyword("ALWAYS") {
		return p.parseGeneratedAlways(column)
	}
	if p.MatchKeyword(keywordBY) {
		p.MatchKeyword(keywordDEFAULT)
		p.MatchKeyword(keywordAS)
		p.MatchKeyword("IDENTITY")
		column.HasDefault = true
		return p.skipParenthesisedIfPresent()
	}
	return nil
}

// parseGeneratedAlways parses the body of a GENERATED ALWAYS AS clause, recognising both
// identity and stored expression variants.
//
// Takes column (*sqlmodel.Column) which receives the identity or generated state.
//
// Returns error when a parenthesised option or expression is unbalanced.
func (p *parser) parseGeneratedAlways(column *sqlmodel.Column) error {
	if !p.MatchKeyword(keywordAS) {
		return nil
	}
	if p.MatchKeyword("IDENTITY") {
		column.HasDefault = true
		return p.skipParenthesisedIfPresent()
	}
	return p.parseGeneratedExpression(column)
}

// parseGeneratedExpression parses the parenthesised expression of a generated column,
// written GENERATED ALWAYS AS (expression) or AS (expression), and its optional VIRTUAL
// or STORED.
//
// Takes column (*sqlmodel.Column) which is marked as generated.
//
// Returns error when the expression is missing or unbalanced.
func (p *parser) parseGeneratedExpression(column *sqlmodel.Column) error {
	if err := p.skipParenthesised(); err != nil {
		p.recordSyntaxError(err)
		return err
	}
	column.IsGenerated = true
	column.GeneratedKind = sqlmodel.GeneratedKindVirtual
	if p.MatchKeyword("STORED") {
		column.GeneratedKind = sqlmodel.GeneratedKindStored
	} else {
		p.MatchKeyword("VIRTUAL")
	}
	return nil
}

// parseColumnType reads a column's type, transparently unwrapping a leading SETOF
// keyword.
//
// A type that, with the array dimensions written after it, nests sqlmodel.MaxTypeDepth
// levels or more records a limit error at its first token, so a type no reader of the
// catalogue can walk whole fails its statement.
//
// Takes engine (typeNormaliser) which resolves the raw type name.
//
// Returns sqlmodel.SQLType which is the parsed type.
// Returns int which is the array dimension count appended via [] or [N] suffixes.
func (p *parser) parseColumnType(engine typeNormaliser) (sqlmodel.SQLType, int) {
	outermost := p.typeDepth == 0
	p.MatchKeyword("SETOF")
	first := p.Current()
	sqlType, dimensions := p.parseColumnTypeInner(engine)
	if outermost {
		p.refuseTypeTooDeep(first, sqlType, dimensions)
	}
	return sqlType, dimensions
}

// refuseTypeTooDeep records a limit error at a type's first token when the type, wrapped
// in a list for each array dimension written after it, nests sqlmodel.MaxTypeDepth levels
// or more.
//
// Takes first (token) which is the type's first token.
// Takes sqlType (sqlmodel.SQLType) which is the type.
// Takes dimensions (int) which is the number of array dimensions written after it.
func (p *parser) refuseTypeTooDeep(first token, sqlType sqlmodel.SQLType, dimensions int) {
	if sqlmodel.TypeTooDeep(new(functionArgumentArrayType(sqlType, dimensions))) {
		p.recordSyntaxError(typeDepthError(first))
	}
}

// parseColumnTypeInner does the bulk of column-type parsing, handling compound types,
// schema-qualified names, multi-word built-ins, and type modifiers.
//
// A type is required: a caller where one may be left out, such as a generated column's
// definition, does not call it. When no type name follows, a syntax error is recorded.
//
// Takes engine (typeNormaliser) which resolves the raw type name.
//
// Returns sqlmodel.SQLType which is the parsed type, or an unknown type when none
// follows.
// Returns int which is the array dimension count parsed from [] or [N] suffixes.
func (p *parser) parseColumnTypeInner(engine typeNormaliser) (sqlmodel.SQLType, int) {
	if p.typeDepth >= p.maxParseDepth {
		p.recordSyntaxError(dialectkit.DepthError(p.maxParseDepth))
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), 0
	}
	if p.typeDepth >= sqlmodel.MaxTypeDepth {
		p.recordSyntaxError(typeDepthError(p.Current()))
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), 0
	}
	p.typeDepth++
	defer func() { p.typeDepth-- }()

	if p.Current().Kind != tokenIdentifier || p.isDuckDBColumnConstraintKeyword() {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected a type"))
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), 0
	}

	firstWord := p.Advance().Value
	lower := strings.ToLower(firstWord)

	if p.Current().Kind == tokenLeftParen {
		if compoundType, isCompound := p.tryParseCompoundType(engine, lower); isCompound {
			arrayDimensions := p.parseArrayDimensions()
			return compoundType, arrayDimensions
		}
	}

	typeSchema := ""
	if p.Current().Kind == tokenDot && !isMultiWordTypePrefix(lower) {
		p.Advance()
		qualifiedName, qualifiedError := p.parseIdentifierOrKeyword()
		if qualifiedError != nil {
			return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, lower), 0
		}
		typeSchema = firstWord
		firstWord = qualifiedName
		lower = strings.ToLower(firstWord)
	}

	fullName := lower
	if isMultiWordTypePrefix(lower) {
		fullName = p.consumeMultiWordType(lower)
	}

	var modifiers []int
	if p.Current().Kind == tokenLeftParen {
		modifiers = p.parseTypeModifiers()
	}

	arrayDimensions := p.parseArrayDimensions()

	sqlType := engine.NormaliseTypeName(fullName, modifiers...)
	if typeSchema != "" {
		sqlType.Schema = typeSchema
	}

	return sqlType, arrayDimensions
}

// parseArrayDimensions counts consecutive [] or [N] suffixes after a type name.
//
// More than maxArrayDimensions suffixes records a dialectkit.ErrParseDepth error and the
// count stops at the cap, so a hostile `[][][]...` suffix cannot build ever-deeper
// wrapped types.
//
// Returns int which is the number of array dimensions consumed, at most
// maxArrayDimensions.
func (p *parser) parseArrayDimensions() int {
	dimensions := 0
	for p.Current().Kind == tokenLeftBracket {
		p.Advance()
		if p.Current().Kind == tokenNumber {
			p.Advance()
		}
		if p.Current().Kind == tokenRightBracket {
			p.Advance()
		}
		if dimensions == maxArrayDimensions {
			p.recordSyntaxError(dialectkit.DepthError(maxArrayDimensions))
			continue
		}
		dimensions++
	}
	return dimensions
}

// consumeMultiWordType reads the trailing keywords of a multi-word built-in type.
//
// Takes lower (string) which is the first keyword lower-cased.
//
// Returns string which is the full canonical type spelling such as "double precision" or
// "timestamp with time zone".
func (p *parser) consumeMultiWordType(lower string) string {
	switch lower {
	case "double":
		if p.MatchKeyword("PRECISION") {
			return "double precision"
		}
		return lower

	case "character":
		if p.MatchKeyword("VARYING") {
			return "character varying"
		}
		return "character"

	case "timestamp":
		return p.consumeTemporalZoneSuffix("timestamp")

	case "time":
		return p.consumeTemporalZoneSuffix("time")
	}

	return lower
}

// consumeTemporalZoneSuffix reads an optional WITH/WITHOUT TIME ZONE suffix following a
// temporal type keyword.
//
// Takes base (string) which is the base type name to extend.
//
// Returns string which is base, possibly extended with the zone suffix.
func (p *parser) consumeTemporalZoneSuffix(base string) string {
	if p.MatchKeyword(keywordWITH) {
		p.MatchKeyword(keywordTIME)
		p.MatchKeyword(keywordZONE)
		return base + " with time zone"
	}
	if p.MatchKeyword("WITHOUT") {
		p.MatchKeyword(keywordTIME)
		p.MatchKeyword(keywordZONE)
		return base + " without time zone"
	}
	return base
}

// parseTypeModifiers reads the parenthesised numeric modifier list of a type, such as the
// (10, 2) on numeric(10, 2).
//
// Returns []int which is the parsed modifier list, or nil when no parenthesised group is
// present.
func (p *parser) parseTypeModifiers() []int {
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	p.Advance()

	var modifiers []int
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.Current().Kind == tokenNumber {
			if value, usable := parseModifierValue(p.Current().Value); usable {
				modifiers = append(modifiers, value)
			}
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	return modifiers
}

// isDuckDBColumnConstraintKeyword reports whether the current token introduces a
// column-level constraint.
//
// Returns bool which is true when the token matches any column constraint keyword
// recognised by the parser.
func (p *parser) isDuckDBColumnConstraintKeyword() bool {
	return p.IsKeyword(keywordPRIMARY, keywordNOT, keywordNULL, keywordUNIQUE, keywordCHECK, keywordDEFAULT,
		"COLLATE", keywordREFERENCES, keywordGENERATED, keywordCONSTRAINT)
}

// isDuckDBTableConstraint reports whether the current token starts a table-level
// constraint declaration.
//
// Returns bool which is true for CONSTRAINT, PRIMARY KEY, UNIQUE (with open paren),
// CHECK, or FOREIGN.
func (p *parser) isDuckDBTableConstraint() bool {
	if p.IsKeyword(keywordCONSTRAINT) {
		return true
	}

	if p.IsKeyword(keywordPRIMARY) && p.Peek().Kind == tokenIdentifier && p.Peek().IsKeyword(tokenIdentifier, keywordKEY) {
		return true
	}

	if p.IsKeyword(keywordUNIQUE) && p.Peek().Kind == tokenLeftParen {
		return true
	}

	if p.IsKeyword(keywordCHECK) {
		return true
	}

	if p.IsKeyword("FOREIGN") {
		return true
	}

	return false
}

// parseDuckDBTableConstraint parses one table-level constraint and returns either its
// primary key columns or the constraint record.
//
// Returns []string which is the primary key column list when the constraint is a PRIMARY
// KEY, else nil.
// Returns *sqlmodel.Constraint which is the parsed constraint for UNIQUE, CHECK, or
// FOREIGN KEY, else nil.
// Returns error when sub-parsing fails.
func (p *parser) parseDuckDBTableConstraint() ([]string, *sqlmodel.Constraint, error) {
	constraintName := p.parseOptionalConstraintName()

	if p.MatchKeyword(keywordPRIMARY) {
		return p.parseTablePrimaryKey()
	}
	if p.MatchKeyword(keywordUNIQUE) {
		return p.parseTableUnique(constraintName)
	}
	if p.MatchKeyword(keywordCHECK) {
		return p.parseTableCheck(constraintName)
	}
	if p.MatchKeyword("FOREIGN") {
		return p.parseTableForeignKey(constraintName)
	}

	p.Advance()
	return nil, nil, nil
}

// parseOptionalConstraintName consumes an optional CONSTRAINT name prefix.
//
// Returns string which is the constraint name when present, else empty.
func (p *parser) parseOptionalConstraintName() string {
	if !p.MatchKeyword(keywordCONSTRAINT) {
		return ""
	}
	name, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return ""
	}
	return name
}

// parseTablePrimaryKey parses a table-level PRIMARY KEY constraint.
//
// Returns []string which is the primary key column list.
// Returns *sqlmodel.Constraint which is always nil for the primary key constraint kind.
// Returns error when the column list cannot be parsed.
func (p *parser) parseTablePrimaryKey() ([]string, *sqlmodel.Constraint, error) {
	p.MatchKeyword(keywordKEY)
	if p.Current().Kind != tokenLeftParen {
		return nil, nil, dialectkit.NewParseError(p.Current(), "expected '(' after PRIMARY KEY")
	}
	columns, err := p.parseDuckDBColumnList()
	if err != nil {
		return nil, nil, err
	}
	return columns, nil, nil
}

// parseTableUnique parses a table-level UNIQUE constraint.
//
// Takes constraintName (string) which is the optional CONSTRAINT prefix name.
//
// Returns []string which is always nil because UNIQUE columns live on the constraint
// record.
// Returns *sqlmodel.Constraint which is the parsed UNIQUE constraint, or nil when no
// column list follows.
// Returns error when the column list cannot be parsed.
func (p *parser) parseTableUnique(constraintName string) ([]string, *sqlmodel.Constraint, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, nil, nil
	}
	columns, columnError := p.parseDuckDBColumnList()
	if columnError != nil {
		return nil, nil, columnError
	}
	return nil, &sqlmodel.Constraint{
		Name:           constraintName,
		Kind:           sqlmodel.ConstraintUnique,
		Columns:        columns,
		ForeignTable:   "",
		ForeignColumns: nil,
		Origin:         sqlmodel.MigrationOrigin{},
	}, nil
}

// parseTableCheck parses a table-level CHECK constraint and skips its expression.
//
// Takes constraintName (string) which is the optional CONSTRAINT prefix name.
//
// Returns []string which is always nil for CHECK constraints.
// Returns *sqlmodel.Constraint which is the CHECK constraint record.
// Returns error when the CHECK expression is unbalanced.
func (p *parser) parseTableCheck(constraintName string) ([]string, *sqlmodel.Constraint, error) {
	if err := p.skipParenthesisedIfPresent(); err != nil {
		return nil, nil, err
	}
	return nil, &sqlmodel.Constraint{
		Name:           constraintName,
		Kind:           sqlmodel.ConstraintCheck,
		ForeignTable:   "",
		Columns:        nil,
		ForeignColumns: nil,
		Origin:         sqlmodel.MigrationOrigin{},
	}, nil
}

// parseTableForeignKey parses a table-level FOREIGN KEY constraint.
//
// Takes constraintName (string) which is the optional CONSTRAINT prefix name.
//
// Returns []string which is always nil for foreign key constraints.
// Returns *sqlmodel.Constraint which is the foreign key constraint record including the
// referenced table and columns.
// Returns error when sub-parsing fails.
func (p *parser) parseTableForeignKey(constraintName string) ([]string, *sqlmodel.Constraint, error) {
	p.MatchKeyword(keywordKEY)
	var columns []string
	if p.Current().Kind == tokenLeftParen {
		parsed, columnError := p.parseDuckDBColumnList()
		if columnError != nil {
			return nil, nil, columnError
		}
		columns = parsed
	}
	foreignTable, foreignColumns, referenceError := p.parseDuckDBForeignKeyReference()
	if referenceError != nil {
		return nil, nil, referenceError
	}
	return nil, &sqlmodel.Constraint{
		Name:           constraintName,
		Kind:           sqlmodel.ConstraintForeignKey,
		Columns:        columns,
		ForeignTable:   foreignTable,
		ForeignColumns: foreignColumns,
		Origin:         sqlmodel.MigrationOrigin{},
	}, nil
}

// parseDuckDBForeignKeyReference parses the REFERENCES clause that follows a FOREIGN KEY
// column list.
//
// Returns string which is the referenced table name, or empty when the reference cannot
// be parsed.
// Returns []string which is the referenced column list, or nil when none was given.
// Returns error when the trailing clause is malformed.
func (p *parser) parseDuckDBForeignKeyReference() (string, []string, error) {
	if !p.MatchKeyword(keywordREFERENCES) {
		return "", nil, p.skipDuckDBForeignKeyClause()
	}
	_, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return "", nil, nil
	}
	var columns []string
	if p.Current().Kind == tokenLeftParen {
		parsed, columnError := p.parseDuckDBColumnList()
		if columnError != nil {
			return tableName, nil, nil
		}
		columns = parsed
	}
	return tableName, columns, p.skipDuckDBForeignKeyClause()
}

// parseDuckDBColumnList parses a parenthesised, comma-separated list of column names with
// optional ordering and COLLATE clauses.
//
// Returns []string which is the parsed column name list.
// Returns error when no opening parenthesis is found or a name cannot be parsed.
func (p *parser) parseDuckDBColumnList() ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()

	var columns []string
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		name, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		columns = append(columns, name)

		p.MatchKeyword(keywordASC)
		p.MatchKeyword(keywordDESC)
		if p.MatchKeyword("COLLATE") {
			p.Advance()
		}

		if p.Current().Kind == tokenComma {
			p.Advance()
		}
	}

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	return columns, nil
}

// skipDuckDBDefaultValue advances past the expression following DEFAULT, stopping at a
// top-level comma or the next constraint keyword.
//
// Returns error when a parenthesised default is unbalanced.
func (p *parser) skipDuckDBDefaultValue() error {
	if p.Current().Kind == tokenLeftParen {
		return p.skipParenthesised()
	}

	depth := 0
	for !p.AtEnd() {
		if p.Current().Kind == tokenLeftParen {
			depth++
			p.Advance()
			continue
		}
		if p.Current().Kind == tokenRightParen {
			if depth == 0 {
				return nil
			}
			depth--
			p.Advance()
			continue
		}
		if depth == 0 && p.Current().Kind == tokenComma {
			return nil
		}
		if depth == 0 && p.isDuckDBColumnConstraintKeyword() {
			return nil
		}
		p.Advance()
	}
	return nil
}

// skipDuckDBForeignKeyClause skips the optional REFERENCES tail and trailing
// ON/MATCH/DEFERRABLE/INITIALLY modifier clauses.
//
// Returns error when the referenced name or column list is malformed.
func (p *parser) skipDuckDBForeignKeyClause() error {
	if p.Current().Kind == tokenIdentifier && !p.isDuckDBForeignKeyActionKeyword() {
		if _, _, err := p.parseSchemaQualifiedName(); err != nil {
			return err
		}
	}
	if err := p.skipParenthesisedIfPresent(); err != nil {
		return err
	}
	for p.MatchKeyword(keywordON) || p.MatchKeyword("MATCH") || p.MatchKeyword(keywordNOT) ||
		p.MatchKeyword("DEFERRABLE") || p.MatchKeyword("INITIALLY") {
		for !p.AtEnd() && p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen &&
			!p.isDuckDBForeignKeyActionKeyword() {
			p.Advance()
		}
	}
	return nil
}

// isDuckDBForeignKeyActionKeyword reports whether the current token begins (or
// terminates) a foreign-key action clause. Used by skipDuckDBForeignKeyClause to
// recognise the boundary between the REFERENCES <table>(cols) prefix and the ON / MATCH /
// NOT / DEFERRABLE / INITIALLY action clauses that may follow it.
//
// Returns bool which is true when the current keyword starts a FK action clause.
func (p *parser) isDuckDBForeignKeyActionKeyword() bool {
	return p.IsKeyword(keywordON, "MATCH", keywordNOT, "DEFERRABLE", "INITIALLY")
}

// parseDropTable parses a DROP TABLE statement into a mutation.
//
// Returns *ast.CatalogueMutation which describes the table to drop.
// Returns error when the table name cannot be parsed.
func (p *parser) parseDropTable() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords(keywordDROP, keywordTABLE); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schema, tableName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	p.MatchKeyword(keywordCASCADE)
	p.MatchKeyword(keywordRESTRICT)

	return ast.NewCatalogueMutation(ast.MutationDropTable, schema, tableName, ast.WithIfExists(ifExists)), nil
}

// parseAlterTable parses an ALTER TABLE statement and dispatches to the matching variant
// parser.
//
// Takes engine (typeNormaliser) which resolves column type names for ALTER TABLE ADD
// COLUMN.
//
// Returns *ast.CatalogueMutation which describes the mutation, or nil when no recognised
// ALTER subcommand follows.
// Returns error when the target table name or subcommand cannot be parsed.
func (p *parser) parseAlterTable(engine typeNormaliser) (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords("ALTER", keywordTABLE); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schema, tableName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	mutation, err := p.parseAlterTableAction(engine, schema, tableName)
	if err != nil {
		return nil, err
	}
	if err := p.requireEnd(); err != nil {
		return nil, err
	}
	markIfExists(mutation, ifExists)
	return mutation, nil
}

// parseAlterTableAction parses the single action of an ALTER TABLE statement.
//
// Takes engine (typeNormaliser) which resolves column type names.
// Takes schema (string) which is the target table's schema.
// Takes tableName (string) which is the target table's name.
//
// Returns *ast.CatalogueMutation which describes the action.
// Returns error when the action is missing or malformed.
func (p *parser) parseAlterTableAction(engine typeNormaliser, schema, tableName string) (*ast.CatalogueMutation, error) {
	switch {
	case p.MatchKeyword("ADD"):
		return p.parseAlterTableAdd(engine, schema, tableName)
	case p.MatchKeyword(keywordDROP):
		return p.parseAlterTableDrop(schema, tableName)
	case p.MatchKeyword("ALTER"):
		return p.parseAlterTableAlterColumn(engine, schema, tableName)
	case p.MatchKeyword("RENAME"):
		return p.parseAlterTableRename(schema, tableName)
	case p.MatchKeyword(keywordSET):
		return p.parseAlterTableSet(schema, tableName)
	default:
		return nil, p.syntaxErrorAt("expected ADD, DROP, ALTER, RENAME or SET")
	}
}

// parseAlterTableAdd parses the body of ALTER TABLE ... ADD ..., for either a constraint
// or a new column.
//
// Takes engine (typeNormaliser) which resolves column type names.
// Takes schema (string) which is the target table's schema.
// Takes tableName (string) which is the target table's name.
//
// Returns *ast.CatalogueMutation which describes the addition.
// Returns error when the constraint or column cannot be parsed.
func (p *parser) parseAlterTableAdd(
	engine typeNormaliser, schema, tableName string,
) (*ast.CatalogueMutation, error) {
	if p.isDuckDBTableConstraint() {
		primaryKey, constraint, constraintError := p.parseDuckDBTableConstraint()
		if constraintError != nil {
			return nil, constraintError
		}
		var constraints []sqlmodel.Constraint
		if constraint != nil {
			constraints = append(constraints, *constraint)
		}
		mutation := ast.NewCatalogueMutation(
			ast.MutationAlterTableAddConstraint,
			schema,
			tableName,
			ast.WithConstraints(constraints),
			ast.WithPrimaryKey(primaryKey),
		)
		for _, column := range primaryKey {
			mutation.AdditionalMutations = append(mutation.AdditionalMutations, ast.NewCatalogueMutation(
				ast.MutationAlterTableAlterColumn,
				schema,
				tableName,
				ast.WithColumnName(column),
				ast.WithColumnNullability(false),
			))
		}
		return mutation, nil
	}
	p.MatchKeyword(keywordCOLUMN)
	ifNotExists := p.matchIfNotExists()
	start := p.Position
	column, _, columnError := p.parseDuckDBColumnDefinition(engine)
	if columnError != nil {
		return nil, columnError
	}
	if err := p.rejectAddedColumnConstraint(start); err != nil {
		return nil, err
	}
	return ast.NewCatalogueMutation(
		ast.MutationAlterTableAddColumn,
		schema,
		tableName,
		ast.WithColumns([]sqlmodel.Column{column}),
		ast.WithIfNotExists(ifNotExists),
	), nil
}

// rejectAddedColumnConstraint reports a syntax error for a column ALTER TABLE ... ADD
// COLUMN declares with a constraint or as generated, which DuckDB refuses: such a
// migration would pass code generation and then fail when it runs.
//
// Takes start (int) which is the index of the column definition's first token.
//
// Returns error which is the recorded syntax error, or nil when the column declares
// neither.
func (p *parser) rejectAddedColumnConstraint(start int) error {
	depth := 0
	for index := start; index < p.Position; index++ {
		current := p.Tokens[index]
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		default:
		}
		if depth != 0 || index == start {
			continue
		}
		switch current.Keyword(tokenIdentifier) {
		case keywordNOT, keywordPRIMARY, keywordUNIQUE, keywordCHECK, keywordREFERENCES, keywordGENERATED, keywordAS:
			err := dialectkit.NewParseError(current, "DuckDB cannot add a column with a constraint or a generated "+
				"column to an existing table: add the column, then use ALTER TABLE ... ALTER COLUMN ... SET NOT NULL")
			p.recordSyntaxError(err)
			return err
		}
	}
	return nil
}

// parseAlterTableDrop parses the body of ALTER TABLE ... DROP ..., for either a column or
// a constraint.
//
// Takes schema (string) which is the target table's schema.
// Takes tableName (string) which is the target table's name.
//
// Returns *ast.CatalogueMutation which describes the drop.
// Returns error when the identifier cannot be parsed.
func (p *parser) parseAlterTableDrop(schema, tableName string) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword(keywordCONSTRAINT) {
		p.matchIfExists()
		constraintName, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			return nil, nameError
		}
		p.MatchKeyword(keywordCASCADE)
		p.MatchKeyword(keywordRESTRICT)
		return ast.NewCatalogueMutation(
			ast.MutationAlterTableDropConstraint,
			schema,
			tableName,
			ast.WithConstraintName(constraintName),
		), nil
	}
	p.MatchKeyword(keywordCOLUMN)
	ifColumnExists := p.matchIfExists()
	columnName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nil, nameError
	}
	if !p.MatchKeyword(keywordCASCADE) {
		p.MatchKeyword(keywordRESTRICT)
	}
	mutation := ast.NewCatalogueMutation(
		ast.MutationAlterTableDropColumn,
		schema,
		tableName,
		ast.WithColumnName(columnName),
	)
	mutation.IfColumnExists = ifColumnExists
	return mutation, nil
}

// parseAlterTableAlterColumn parses ALTER TABLE ... ALTER [COLUMN] name and its action:
// SET NOT NULL and DROP NOT NULL, which change whether the column may hold NULL, and [SET
// DATA] TYPE, SET DEFAULT and DROP DEFAULT.
//
// Takes engine (typeNormaliser) which resolves the type a TYPE action names.
// Takes schema (string) which is the target table's schema.
// Takes tableName (string) which is the target table's name.
//
// Returns *ast.CatalogueMutation which records the column being altered, with its
// nullability or type change when the action makes one.
// Returns error when the column name or the action is malformed.
func (p *parser) parseAlterTableAlterColumn(engine typeNormaliser, schema, tableName string) (*ast.CatalogueMutation, error) {
	p.MatchKeyword(keywordCOLUMN)
	columnName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		p.recordSyntaxError(nameError)
		return nil, nameError
	}
	options := []ast.CatalogueMutationOption{ast.WithColumnName(columnName)}
	switch {
	case p.MatchKeyword(keywordSET):
		option, err := p.parseAlterColumnSet(engine)
		if err != nil {
			return nil, err
		}
		if option != nil {
			options = append(options, option)
		}
	case p.MatchKeyword(keywordDROP):
		if p.MatchKeyword(keywordNOT) {
			if err := p.expectKeywords(keywordNULL); err != nil {
				p.recordSyntaxError(err)
				return nil, err
			}
			options = append(options, ast.WithColumnNullability(true))
		} else if err := p.expectKeywords(keywordDEFAULT); err != nil {
			p.recordSyntaxError(err)
			return nil, err
		}
	case p.MatchKeyword(keywordTYPE):
		sqlType, arrayDimensions := p.parseColumnType(engine)
		p.skipAlterColumnUsing()
		options = append(options, ast.WithColumnType(sqlType, arrayDimensions))
	default:
		return nil, p.syntaxErrorAt("expected SET, DROP or TYPE after the column name")
	}
	return ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, schema, tableName, options...), nil
}

// parseAlterColumnSet parses what follows SET in an ALTER COLUMN action: NOT NULL,
// DEFAULT expression or DATA TYPE type.
//
// Takes engine (typeNormaliser) which resolves the type a DATA TYPE action names.
//
// Returns ast.CatalogueMutationOption which records a nullability or type change, or nil
// for an action that makes neither.
// Returns error when the action is malformed.
func (p *parser) parseAlterColumnSet(engine typeNormaliser) (ast.CatalogueMutationOption, error) {
	switch {
	case p.MatchKeyword(keywordNOT):
		if err := p.expectKeywords(keywordNULL); err != nil {
			p.recordSyntaxError(err)
			return nil, err
		}
		return ast.WithColumnNullability(false), nil
	case p.MatchKeyword(keywordDEFAULT):
		return nil, p.skipDuckDBDefaultValue()
	case p.MatchKeyword("DATA"):
		if err := p.expectKeywords(keywordTYPE); err != nil {
			p.recordSyntaxError(err)
			return nil, err
		}
		sqlType, arrayDimensions := p.parseColumnType(engine)
		p.skipAlterColumnUsing()
		return ast.WithColumnType(sqlType, arrayDimensions), nil
	default:
		return nil, p.syntaxErrorAt("expected NOT NULL, DEFAULT or DATA TYPE after SET")
	}
}

// skipAlterColumnUsing consumes the USING expression that converts a column's values to
// its new type.
func (p *parser) skipAlterColumnUsing() {
	if !p.MatchKeyword(keywordUSING) {
		return
	}
	for !p.AtEnd() {
		p.Advance()
	}
}

// parseAlterTableRename parses ALTER TABLE ... RENAME for both the table itself and
// individual columns.
//
// Takes schema (string) which is the target table's schema.
// Takes tableName (string) which is the target table's name.
//
// Returns *ast.CatalogueMutation which describes the rename.
// Returns error when any identifier cannot be parsed.
func (p *parser) parseAlterTableRename(schema, tableName string) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword("TO") {
		newName, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			return nil, nameError
		}
		return ast.NewCatalogueMutation(
			ast.MutationAlterTableRenameTable,
			schema,
			tableName,
			ast.WithNewName(newName),
		), nil
	}

	p.MatchKeyword(keywordCOLUMN)
	oldName, oldError := p.parseIdentifierOrKeyword()
	if oldError != nil {
		return nil, oldError
	}
	if _, err := p.ExpectKeyword("TO"); err != nil {
		return nil, err
	}
	newName, newError := p.parseIdentifierOrKeyword()
	if newError != nil {
		return nil, newError
	}
	return ast.NewCatalogueMutation(
		ast.MutationAlterTableRenameColumn,
		schema,
		tableName,
		ast.WithColumnName(oldName),
		ast.WithNewName(newName),
	), nil
}

// parseAlterTableSet parses ALTER TABLE ... SET ..., currently only the SET SCHEMA
// variant.
//
// Takes schema (string) which is the target table's schema.
// Takes tableName (string) which is the target table's name.
//
// Returns *ast.CatalogueMutation which describes the schema change, or nil when SET is
// followed by an unsupported option.
// Returns error when the new schema name cannot be parsed.
func (p *parser) parseAlterTableSet(schema, tableName string) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword(keywordSCHEMA) {
		newSchema, schemaError := p.parseIdentifierOrKeyword()
		if schemaError != nil {
			return nil, schemaError
		}
		return ast.NewCatalogueMutation(
			ast.MutationAlterTableSetSchema,
			schema,
			tableName,
			ast.WithNewName(newSchema),
		), nil
	}
	return nil, nil
}

// parseCreateView parses a CREATE VIEW statement and analyses the view body when AS is
// present.
//
// Returns *ast.CatalogueMutation which describes the view to create, including its
// analysed query when one is available.
// Returns error when the view name or column list cannot be parsed.
func (p *parser) parseCreateView() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}

	p.skipOrReplace()

	p.MatchKeyword("TEMP")
	p.MatchKeyword("TEMPORARY")

	if _, err := p.ExpectKeyword("VIEW"); err != nil {
		return nil, err
	}

	ifNotExists := p.matchIfNotExists()

	schema, viewName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	var columnNames []string
	if p.Current().Kind == tokenLeftParen {
		names, listError := p.parseDuckDBColumnList()
		if listError != nil {
			return nil, listError
		}
		columnNames = names
	}

	mutation := ast.NewCatalogueMutation(ast.MutationCreateView, schema, viewName, ast.WithIfNotExists(ifNotExists))

	if p.MatchKeyword(keywordAS) {
		analysis, unanalysed, bodyError := p.analyseViewBody(columnNames)
		if bodyError != nil {
			return nil, bodyError
		}
		mutation.ViewDefinition = analysis
		withUnanalysedBody(unanalysed)(mutation)
	}

	mutation.Columns = dialectkit.ColumnsFromNames(columnNames)

	return mutation, nil
}

// skipOrReplace consumes an optional OR REPLACE prefix.
//
// Returns bool which is true when OR REPLACE was present.
func (p *parser) skipOrReplace() bool {
	if !p.MatchKeyword("OR") {
		return false
	}
	p.MatchKeyword("REPLACE")
	return true
}

// skipTemporary consumes an optional TEMP or TEMPORARY modifier.
func (p *parser) skipTemporary() {
	if !p.MatchKeyword("TEMP") {
		p.MatchKeyword("TEMPORARY")
	}
}

// expectKeywords consumes each keyword in order, failing at the first mismatch.
//
// Takes keywords (...string) which are the keywords that must follow, in order.
//
// Returns error when a token does not match its expected keyword.
func (p *parser) expectKeywords(keywords ...string) error {
	for _, keyword := range keywords {
		if _, err := p.ExpectKeyword(keyword); err != nil {
			return err
		}
	}
	return nil
}

// parseDropView parses a DROP VIEW statement into a mutation.
//
// Returns *ast.CatalogueMutation which describes the drop.
// Returns error when the view name cannot be parsed.
func (p *parser) parseDropView() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords(keywordDROP, "VIEW"); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schema, viewName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(ast.MutationDropView, schema, viewName, ast.WithIfExists(ifExists)), nil
}

// appendConstraintPrimaryKey replaces existing when candidate has columns, mirroring
// "last declared wins" semantics.
//
// Takes existing ([]string) which is the current primary key column list.
// Takes candidate ([]string) which is the columns from a newly parsed constraint.
//
// Returns []string which is candidate when non-empty, else existing.
func appendConstraintPrimaryKey(existing, candidate []string) []string {
	if len(candidate) > 0 {
		return candidate
	}
	return existing
}

// appendConstraint appends constraint to constraints when non-nil.
//
// Takes constraints ([]sqlmodel.Constraint) which is the existing constraint list.
// Takes constraint (*sqlmodel.Constraint) which is the newly parsed constraint or nil.
//
// Returns []sqlmodel.Constraint which is the updated list.
func appendConstraint(constraints []sqlmodel.Constraint, constraint *sqlmodel.Constraint) []sqlmodel.Constraint {
	if constraint != nil {
		return append(constraints, *constraint)
	}
	return constraints
}

// isMultiWordTypePrefix reports whether lower introduces a built-in type spelled across
// several keywords.
//
// Takes lower (string) which is the candidate first keyword lower-cased.
//
// Returns bool which is true for double, character, timestamp, time.
func isMultiWordTypePrefix(lower string) bool {
	switch lower {
	case "double", "character", "timestamp", "time":
		return true
	}
	return false
}

// parseModifierValue parses a numeric modifier literal into a bounded integer.
//
// Takes literal (string) which is the raw numeric token value.
//
// Returns int which is the parsed modifier value when usable.
// Returns bool which is true when the literal is a plain decimal integer that fits
// without overflow, and false when it should be skipped (non-integer form or out of
// range).
func parseModifierValue(literal string) (int, bool) {
	value, err := strconv.Atoi(literal)
	if err != nil {
		return 0, false
	}
	return value, true
}

// withAdditionalMutation queues a follow-up mutation that the catalogue builder applies
// straight after the primary one, such as the create half of CREATE OR REPLACE TABLE.
//
// Takes next (*ast.CatalogueMutation) which is the follow-up mutation.
//
// Returns ast.CatalogueMutationOption which appends next to AdditionalMutations.
func withAdditionalMutation(next *ast.CatalogueMutation) ast.CatalogueMutationOption {
	return func(mutation *ast.CatalogueMutation) {
		mutation.AdditionalMutations = append(mutation.AdditionalMutations, next)
	}
}
