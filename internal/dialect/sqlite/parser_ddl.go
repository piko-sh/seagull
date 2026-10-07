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

package sqlite

import (
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxTypeModifiers is how many modifiers a type name takes: a length, or a precision and
	// a scale.
	maxTypeModifiers = 2
)

// primaryKeyOrder records whether, and in which order, a column declares an inline
// PRIMARY KEY constraint.
type primaryKeyOrder uint8

const (
	// primaryKeyNone marks a column with no inline PRIMARY KEY constraint.
	primaryKeyNone primaryKeyOrder = iota

	// primaryKeyAscending marks a column with an inline PRIMARY KEY constraint in ascending
	// (the default) order.
	primaryKeyAscending

	// primaryKeyDescending marks a column with an inline PRIMARY KEY DESC constraint, which
	// SQLite never makes the rowid alias.
	primaryKeyDescending
)

// tableOptions holds the options written after a CREATE TABLE body.
type tableOptions struct {
	// withoutRowID is true for a WITHOUT ROWID table.
	withoutRowID bool

	// strict is true for a STRICT table.
	strict bool
}

// tableBodyResult accumulates columns, primary key, and constraints extracted from a
// CREATE TABLE body.
type tableBodyResult struct {
	// integerSpelled names the columns whose declared type is exactly "INTEGER", which is
	// what makes a single-column primary key an alias for the 64-bit rowid.
	integerSpelled map[string]bool

	// descendingInlineKeys names the columns declared INTEGER PRIMARY KEY DESC, which SQLite
	// keeps as an ordinary indexed column rather than the rowid alias.
	descendingInlineKeys map[string]bool

	// declaredTypes maps each column name to its raw declared type spelling, used to
	// validate a STRICT table's column datatypes against the spellings SQLite's STRICT mode
	// permits.
	declaredTypes map[string]string

	// columns are the column definitions parsed from the body.
	columns []sqlmodel.Column

	// primaryKey holds the column names that form the primary key.
	primaryKey []string

	// constraints holds non-primary table-level constraints.
	constraints []sqlmodel.Constraint
}

// parseCreateTable parses a CREATE TABLE statement.
//
// Takes engine (*Dialect) which provides type normalisation.
//
// Returns *ast.CatalogueMutation which describes the table to create.
// Returns error when the statement is malformed.
func (p *parser) parseCreateTable(engine *Dialect) (*ast.CatalogueMutation, error) {
	tableName, ifNotExists, err := p.parseCreateTableHeader()
	if err != nil {
		return nil, err
	}

	if p.MatchKeyword(keywordAS) {
		return p.parseCreateTableAs(tableName, ifNotExists)
	}

	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), fmt.Sprintf("expected ( after table name %q", tableName))
	}
	p.Advance()

	body, err := p.parseCreateTableBody(engine)
	if err != nil {
		return nil, err
	}

	options, err := p.parseTableOptions()
	if err != nil {
		return nil, err
	}
	isWithoutRowID, isStrict := options.withoutRowID, options.strict

	if err := validateStrictColumnTypes(&body, isStrict); err != nil {
		return nil, err
	}

	if isWithoutRowID || isStrict {
		markPrimaryKeyNotNull(&body)
	}
	widenRowidAlias(&body, isWithoutRowID)
	widenStrictIntegers(&body, isStrict)

	return ast.NewCatalogueMutation(
		ast.MutationCreateTable,
		"",
		tableName,
		ast.WithColumns(body.columns),
		ast.WithPrimaryKey(body.primaryKey),
		ast.WithConstraints(body.constraints),
		ast.WithWithoutRowID(isWithoutRowID),
		ast.WithIfNotExists(ifNotExists),
	), nil
}

// parseCreateTableAs parses the query of a CREATE TABLE ... AS, which gives the new table
// its columns, up to the end of the statement.
//
// Takes tableName (string) which is the table's name.
// Takes ifNotExists (bool) which is true when the statement said IF NOT EXISTS.
//
// Returns *ast.CatalogueMutation which describes the table to create.
// Returns error when the query is malformed.
func (p *parser) parseCreateTableAs(tableName string, ifNotExists bool) (*ast.CatalogueMutation, error) {
	definition, unanalysed, err := p.analyseViewBody(nil, "table")
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(ast.MutationCreateTable, "", tableName, ast.WithIfNotExists(ifNotExists))
	mutation.ViewDefinition = definition
	mutation.UnanalysedParts = unanalysed
	return mutation, nil
}

// parseTableOptions reads the options written after a CREATE TABLE body as SQLite reads
// them: WITHOUT ROWID and STRICT, in either order, separated by commas, where the list
// may open with a comma and an option may be repeated.
//
// Returns tableOptions which holds the options read.
// Returns error when an option is not WITHOUT ROWID or STRICT, or a comma is not followed
// by one.
func (p *parser) parseTableOptions() (tableOptions, error) {
	var options tableOptions
	if p.Current().Kind == tokenIdentifier {
		if err := p.parseTableOption(&options); err != nil {
			return tableOptions{}, err
		}
	}
	for p.Current().Kind == tokenComma {
		p.Advance()
		if err := p.parseTableOption(&options); err != nil {
			return tableOptions{}, err
		}
	}
	return options, nil
}

// parseTableOption reads one table option, WITHOUT ROWID or STRICT.
//
// Takes options (*tableOptions) which records the option read.
//
// Returns error when the next words are not a table option.
func (p *parser) parseTableOption(options *tableOptions) error {
	option := p.Current()
	switch {
	case p.MatchKeyword("WITHOUT"):
		if !p.MatchKeyword("ROWID") {
			return dialectkit.NewParseError(p.Current(), "unknown table option: expected ROWID after WITHOUT")
		}
		options.withoutRowID = true
	case p.MatchKeyword("STRICT"):
		options.strict = true
	default:
		return dialectkit.NewParseError(option, "unknown table option: expected WITHOUT ROWID or STRICT")
	}
	return nil
}

// parseCreateTableHeader consumes `CREATE [TEMP | TEMPORARY] TABLE [IF NOT EXISTS] name`.
//
// Returns string which is the table name.
// Returns bool which is true when the statement said IF NOT EXISTS.
// Returns error when a keyword or the name is missing.
func (p *parser) parseCreateTableHeader() (string, bool, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return "", false, err
	}

	p.MatchKeyword("TEMP")
	p.MatchKeyword("TEMPORARY")
	if _, err := p.ExpectKeyword(keywordTABLE); err != nil {
		return "", false, err
	}

	ifNotExists, err := p.matchIfNotExists()
	if err != nil {
		return "", false, err
	}
	tableName, err := p.parseTableName()
	return tableName, ifNotExists, err
}

// matchIfNotExists consumes an optional IF NOT EXISTS clause.
//
// Returns bool which is true when the clause was present.
// Returns error when IF is not followed by NOT EXISTS.
func (p *parser) matchIfNotExists() (bool, error) {
	if !p.MatchKeyword(keywordIF) {
		return false, nil
	}
	if err := p.expectKeywordSequence(keywordNOT, keywordEXISTS); err != nil {
		return false, err
	}
	return true, nil
}

// matchIfExists consumes an optional IF EXISTS clause.
//
// Returns bool which is true when the clause was present.
// Returns error when IF is not followed by EXISTS.
func (p *parser) matchIfExists() (bool, error) {
	if !p.MatchKeyword(keywordIF) {
		return false, nil
	}
	if _, err := p.ExpectKeyword(keywordEXISTS); err != nil {
		return false, err
	}
	return true, nil
}

// parseCreateTableBody parses the parenthesised body of a CREATE TABLE statement, from
// its first element through its closing parenthesis.
//
// Each element must be followed by a comma or the closing parenthesis. Unrecognised
// syntax is rejected instead of becoming a phantom column. Empty bodies, trailing commas
// and missing closing parentheses also fail the statement.
//
// Takes engine (*Dialect) which provides type normalisation.
//
// Returns tableBodyResult which holds the columns, primary key, constraints, and the set
// of integer-spelled columns used for rowid-alias widening.
// Returns error when a body element fails to parse or the body is not closed.
func (p *parser) parseCreateTableBody(engine *Dialect) (tableBodyResult, error) {
	result := tableBodyResult{
		integerSpelled:       map[string]bool{},
		descendingInlineKeys: map[string]bool{},
		declaredTypes:        map[string]string{},
		columns:              nil,
		primaryKey:           nil,
		constraints:          nil,
	}

	opening := p.PeekAt(-1)
	for {
		if p.AtEnd() {
			return tableBodyResult{}, dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
		}
		if err := p.parseTableBodyElement(engine, &result); err != nil {
			return tableBodyResult{}, err
		}
		switch p.Current().Kind {
		case tokenComma:
			p.Advance()
		case tokenRightParen:
			p.Advance()
			return result, nil
		default:
			if p.AtEnd() {
				return tableBodyResult{}, dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
			}
			return tableBodyResult{}, dialectkit.NewParseError(p.Current(), "expected , or ) after a column or table constraint")
		}
	}
}

// parseTableBodyElement parses one column or table-level constraint.
//
// Takes engine (*Dialect) which provides type normalisation.
// Takes result (*tableBodyResult) which receives the parsed element.
//
// Returns error when the element fails to parse.
func (p *parser) parseTableBodyElement(engine *Dialect, result *tableBodyResult) error {
	if p.isTableConstraint() {
		return p.parseTableBodyConstraint(result)
	}
	return p.parseTableBodyColumn(engine, result)
}

// parseTableBodyConstraint parses one table-level constraint and stores its outcome.
//
// Takes result (*tableBodyResult) which receives the parsed constraint or primary key
// column list.
//
// Returns error when the constraint fails to parse.
func (p *parser) parseTableBodyConstraint(result *tableBodyResult) error {
	constraintPrimaryKey, constraint, err := p.parseTableConstraint()
	if err != nil {
		return err
	}
	if len(constraintPrimaryKey) > 0 {
		result.primaryKey = constraintPrimaryKey
	}
	if constraint != nil {
		result.constraints = append(result.constraints, *constraint)
	}
	return nil
}

// parseTableBodyColumn parses one column definition and tracks primary key membership.
//
// Takes engine (*Dialect) which provides type normalisation.
// Takes result (*tableBodyResult) which receives the parsed column.
//
// Returns error when the column fails to parse.
func (p *parser) parseTableBodyColumn(engine *Dialect, result *tableBodyResult) error {
	column, columnPrimaryKey, declaredType, err := p.parseColumnDefinition(engine)
	if err != nil {
		return err
	}
	result.columns = append(result.columns, column)
	result.declaredTypes[column.Name] = declaredType
	if columnPrimaryKey != primaryKeyNone {
		result.primaryKey = append(result.primaryKey, column.Name)
	}
	if columnPrimaryKey == primaryKeyDescending {
		result.descendingInlineKeys[column.Name] = true
	}
	if isRowidAliasType(declaredType) {
		result.integerSpelled[column.Name] = true
	}
	return nil
}

// parseColumnDefinition parses a single column name, type, and inline constraints.
//
// Takes engine (*Dialect) which provides type normalisation.
//
// Returns column (sqlmodel.Column) which describes the parsed column.
// Returns primaryKey (primaryKeyOrder) which records an inline PRIMARY KEY constraint and
// its order.
// Returns declaredType (string) which is the raw declared type spelling, used for
// rowid-alias detection and STRICT-table type validation.
// Returns err (error) which is non-nil when the column fails to parse.
func (p *parser) parseColumnDefinition(
	engine *Dialect,
) (column sqlmodel.Column, primaryKey primaryKeyOrder, declaredType string, err error) {
	name, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return sqlmodel.Column{}, primaryKeyNone, "", fmt.Errorf("parsing column name: %w", nameError)
	}

	typeName, modifiers, typeError := p.parseTypeName()
	if typeError != nil {
		return sqlmodel.Column{}, primaryKeyNone, "", typeError
	}
	column = sqlmodel.NewColumn(name, engine.NormaliseTypeName(typeName, modifiers...), true)

	primaryKey, err = p.parseColumnConstraints(&column)
	if err != nil {
		return sqlmodel.Column{}, primaryKeyNone, "", err
	}
	return column, primaryKey, typeName, nil
}

// parseColumnConstraints consumes every inline constraint following a column type.
//
// Takes column (*sqlmodel.Column) which receives constraint metadata.
//
// Returns primaryKeyOrder which records an inline PRIMARY KEY constraint and its order.
// Returns error when a constraint is malformed.
func (p *parser) parseColumnConstraints(column *sqlmodel.Column) (primaryKeyOrder, error) {
	primaryKey := primaryKeyNone

	for !p.AtEnd() && p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen {
		handled, primary, err := p.parseOneColumnConstraint(column)
		if err != nil {
			return primaryKeyNone, err
		}
		if primary != primaryKeyNone {
			primaryKey = primary
		}
		if !handled {
			break
		}
	}

	return primaryKey, nil
}

// parseOneColumnConstraint consumes a single inline column constraint.
//
// Takes column (*sqlmodel.Column) which receives constraint metadata.
//
// Returns handled (bool) which is true when a constraint was consumed.
// Returns primaryKey (primaryKeyOrder) which records a PRIMARY KEY constraint and its
// order.
// Returns err (error) which is non-nil when the constraint is malformed.
func (p *parser) parseOneColumnConstraint(column *sqlmodel.Column) (handled bool, primaryKey primaryKeyOrder, err error) {
	if p.MatchKeyword(keywordPRIMARY) {
		order, keyError := p.parsePrimaryKeyColumnConstraint()
		return true, order, keyError
	}

	if p.MatchKeyword(keywordNOT) {
		if _, nullError := p.ExpectKeyword(keywordNULL); nullError != nil {
			return true, primaryKeyNone, nullError
		}
		column.Nullable = false
		return true, primaryKeyNone, p.skipConflictClause()
	}

	if p.MatchKeyword(keywordNULL) {
		column.Nullable = true
		return true, primaryKeyNone, p.skipConflictClause()
	}

	if p.MatchKeyword(keywordUNIQUE) {
		return true, primaryKeyNone, p.skipConflictClause()
	}

	if p.MatchKeyword(keywordCHECK) {
		return true, primaryKeyNone, p.skipParenthesised()
	}

	if p.MatchKeyword("DEFAULT") {
		column.HasDefault = true
		return true, primaryKeyNone, p.skipDefaultValue()
	}

	return p.parseSecondaryColumnConstraint(column)
}

// parsePrimaryKeyColumnConstraint consumes the body of an inline PRIMARY KEY column
// constraint.
//
// The key is not marked defaulted or NOT NULL here: only the rowid alias is both, which
// widenRowidAlias decides once the whole table is read, and only a WITHOUT ROWID or
// STRICT table's key is NOT NULL, which markPrimaryKeyNotNull applies, so a TEXT or INT
// primary key of a rowid table may hold NULL and still requires a value on INSERT.
//
// Returns primaryKeyOrder which is primaryKeyDescending for PRIMARY KEY DESC and
// primaryKeyAscending otherwise.
// Returns error when KEY is missing or the conflict clause is malformed.
func (p *parser) parsePrimaryKeyColumnConstraint() (primaryKeyOrder, error) {
	if _, err := p.ExpectKeyword(keywordKEY); err != nil {
		return primaryKeyNone, err
	}
	order := primaryKeyAscending
	if p.MatchKeyword("DESC") {
		order = primaryKeyDescending
	} else {
		p.MatchKeyword("ASC")
	}
	if err := p.skipConflictClause(); err != nil {
		return primaryKeyNone, err
	}
	p.MatchKeyword("AUTOINCREMENT")
	return order, nil
}

// parseSecondaryColumnConstraint handles less common inline column constraints (COLLATE,
// REFERENCES, GENERATED, CONSTRAINT).
//
// Takes column (*sqlmodel.Column) which receives constraint metadata.
//
// Returns handled (bool) which is true when a constraint was consumed.
// Returns primaryKey (primaryKeyOrder) which is always primaryKeyNone for these
// constraints.
// Returns err (error) which is non-nil when the constraint is malformed.
func (p *parser) parseSecondaryColumnConstraint(column *sqlmodel.Column) (handled bool, primaryKey primaryKeyOrder, err error) {
	if p.MatchKeyword(keywordCOLLATE) {
		_, err = p.parseIdentifierOrKeyword()
		return true, primaryKeyNone, err
	}

	if p.MatchKeyword(keywordREFERENCES) {
		_, _, err = p.parseForeignKeyTarget()
		return true, primaryKeyNone, err
	}

	if p.IsKeyword("GENERATED", keywordAS) {
		return true, primaryKeyNone, p.parseGeneratedColumnBody(column)
	}

	if p.MatchKeyword(keywordCONSTRAINT) {
		_, err = p.parseIdentifierOrKeyword()
		return true, primaryKeyNone, err
	}

	return false, primaryKeyNone, nil
}

// parseGeneratedColumnBody consumes a generated column's definition, `[GENERATED ALWAYS]
// AS (expression) [STORED | VIRTUAL]`.
//
// Takes column (*sqlmodel.Column) which receives the generated-column flags and kind.
//
// Returns error when a keyword or the parenthesised expression is missing, or its
// parentheses are unmatched.
func (p *parser) parseGeneratedColumnBody(column *sqlmodel.Column) error {
	if p.MatchKeyword("GENERATED") {
		if _, err := p.ExpectKeyword("ALWAYS"); err != nil {
			return err
		}
	}
	if _, err := p.ExpectKeyword(keywordAS); err != nil {
		return err
	}
	if err := p.skipParenthesised(); err != nil {
		return err
	}
	column.IsGenerated = true
	column.GeneratedKind = p.parseGeneratedKind()
	return nil
}

// skipConflictClause consumes an optional ON CONFLICT clause and the resolution it names:
// ROLLBACK, ABORT, FAIL, IGNORE or REPLACE.
//
// Returns error when ON is not followed by CONFLICT and a resolution.
func (p *parser) skipConflictClause() error {
	if !p.MatchKeyword(keywordON) {
		return nil
	}
	if _, err := p.ExpectKeyword(keywordCONFLICT); err != nil {
		return err
	}
	_, err := p.ExpectKeyword("ROLLBACK", "ABORT", "FAIL", "IGNORE", "REPLACE")
	return err
}

// parseTypeName parses a SQLite column type spelling and its modifiers.
//
// Returns string which is the type spelling joined by spaces.
// Returns []int which holds the parenthesised modifiers, one per modifier written.
// Returns error when the modifiers are malformed.
func (p *parser) parseTypeName() (string, []int, error) {
	if p.Current().Kind != tokenIdentifier {
		return "", nil, nil
	}

	if p.isTableConstraintKeyword() || p.isColumnConstraintKeyword() {
		return "", nil, nil
	}

	var parts []string
	parts = append(parts, p.Advance().Value)

	for p.Current().Kind == tokenIdentifier && !p.isColumnConstraintKeyword() && !p.isTableConstraintKeyword() {
		parts = append(parts, p.Advance().Value)
	}

	modifiers, err := p.parseTypeModifiers()
	return strings.Join(parts, " "), modifiers, err
}

// parseTypeModifiers consumes the parenthesised modifiers attached to a type name, as
// SQLite writes them: one or two numbers, each with an optional sign, as in VARCHAR(255)
// or DECIMAL(10, -2).
//
// Returns []int which holds one value per modifier, in order, so a precision and a scale
// keep their places; nil when the type has no modifiers.
// Returns error when a modifier is not a number, or the list is not closed after one or
// two modifiers.
func (p *parser) parseTypeModifiers() ([]int, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, nil
	}
	opening := p.Advance()

	modifiers := make([]int, 0, maxTypeModifiers)
	for {
		modifier, err := p.parseTypeModifier()
		if err != nil {
			return nil, err
		}
		modifiers = append(modifiers, modifier)
		if len(modifiers) == maxTypeModifiers || p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.AtEnd() {
		return nil, dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
	}
	if p.Current().Kind != tokenRightParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected ) after the type's modifiers")
	}
	p.Advance()
	return modifiers, nil
}

// parseTypeModifier consumes one type modifier: a number with an optional + or - sign. A
// modifier whose magnitude passes dialectkit.MaxTypeModifier is held at it.
//
// Returns int which is the modifier's value.
// Returns error when no number is written.
func (p *parser) parseTypeModifier() (int, error) {
	negative := false
	if sign := p.Current(); sign.Kind == tokenOperator && (sign.Value == "+" || sign.Value == "-") {
		negative = sign.Value == "-"
		p.Advance()
	}
	number := p.Current()
	magnitude, ok := typeModifierMagnitude(number)
	if !ok {
		return 0, dialectkit.NewParseError(number, "expected a number as the type's modifier")
	}
	p.Advance()
	if negative {
		return -magnitude, nil
	}
	return magnitude, nil
}

// isColumnConstraintKeyword reports whether the current token is a column-constraint
// introducer.
//
// Returns bool which is true when the token starts an inline column constraint.
func (p *parser) isColumnConstraintKeyword() bool {
	return p.IsKeyword(keywordPRIMARY, keywordNOT, keywordNULL, keywordUNIQUE, keywordCHECK, "DEFAULT",
		keywordCOLLATE, keywordREFERENCES, "GENERATED", keywordAS, keywordCONSTRAINT, "AUTOINCREMENT")
}

// isTableConstraintKeyword reports whether the current token is a table-constraint
// introducer.
//
// Returns bool which is true when the token starts a table-level constraint.
func (p *parser) isTableConstraintKeyword() bool {
	return p.IsKeyword(keywordPRIMARY, keywordUNIQUE, keywordCHECK, "FOREIGN", keywordCONSTRAINT)
}

// isTableConstraint reports whether the upcoming tokens form a table-level constraint
// rather than a column definition.
//
// Returns bool which is true when the next element is a table constraint.
func (p *parser) isTableConstraint() bool {
	if p.IsKeyword(keywordCONSTRAINT) {
		return true
	}

	if p.IsKeyword(keywordPRIMARY) && p.Peek().IsKeyword(tokenIdentifier, keywordKEY) {
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

// parseTableConstraint parses one table-level constraint.
//
// Returns []string which is the primary-key column list when the constraint is PRIMARY
// KEY.
// Returns *sqlmodel.Constraint which describes the constraint, or nil for PRIMARY KEY.
// Returns error when the constraint fails to parse.
func (p *parser) parseTableConstraint() ([]string, *sqlmodel.Constraint, error) {
	var constraintName string
	if p.MatchKeyword(keywordCONSTRAINT) {
		name, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			return nil, nil, nameError
		}
		constraintName = name
	}

	if p.MatchKeyword(keywordPRIMARY) {
		return p.parsePrimaryKeyConstraint()
	}

	if p.MatchKeyword(keywordUNIQUE) {
		return p.parseUniqueConstraint(constraintName)
	}

	if p.MatchKeyword(keywordCHECK) {
		return p.parseCheckConstraint(constraintName)
	}

	if p.MatchKeyword("FOREIGN") {
		return p.parseForeignKeyConstraint(constraintName)
	}

	if constraintName != "" {
		return nil, nil, nil
	}
	return nil, nil, dialectkit.NewParseError(p.Current(), "expected PRIMARY KEY, UNIQUE, CHECK or FOREIGN KEY")
}

// parsePrimaryKeyConstraint parses a table-level PRIMARY KEY clause.
//
// Returns []string which holds the primary-key column names.
// Returns *sqlmodel.Constraint which is always nil for PRIMARY KEY.
// Returns error when the clause is malformed.
func (p *parser) parsePrimaryKeyConstraint() ([]string, *sqlmodel.Constraint, error) {
	if _, err := p.ExpectKeyword(keywordKEY); err != nil {
		return nil, nil, err
	}
	if p.Current().Kind != tokenLeftParen {
		return nil, nil, dialectkit.NewParseError(p.Current(), "expected ( after PRIMARY KEY")
	}
	columns, err := p.parseColumnList()
	if err != nil {
		return nil, nil, err
	}
	if err := p.skipConflictClause(); err != nil {
		return nil, nil, err
	}
	return columns, nil, nil
}

// parseUniqueConstraint parses a table-level UNIQUE clause and the ON CONFLICT clause
// that may follow its column list.
//
// Takes constraintName (string) which carries the optional CONSTRAINT name.
//
// Returns []string which is always nil for unique constraints.
// Returns *sqlmodel.Constraint which describes the unique constraint.
// Returns error when the clause is malformed.
func (p *parser) parseUniqueConstraint(constraintName string) ([]string, *sqlmodel.Constraint, error) {
	columns, columnError := p.parseColumnList()
	if columnError != nil {
		return nil, nil, columnError
	}
	if err := p.skipConflictClause(); err != nil {
		return nil, nil, err
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

// parseCheckConstraint parses a table-level CHECK clause.
//
// Takes constraintName (string) which carries the optional CONSTRAINT name.
//
// Returns []string which is always nil for check constraints.
// Returns *sqlmodel.Constraint which describes the check constraint.
// Returns error when the parenthesised CHECK expression is missing or unmatched, or the
// conflict clause after it is malformed.
func (p *parser) parseCheckConstraint(constraintName string) ([]string, *sqlmodel.Constraint, error) {
	if err := p.skipParenthesised(); err != nil {
		return nil, nil, err
	}
	if err := p.skipConflictClause(); err != nil {
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

// parseForeignKeyConstraint parses a table-level FOREIGN KEY clause.
//
// Takes constraintName (string) which carries the optional CONSTRAINT name.
//
// Returns []string which is always nil for foreign-key constraints.
// Returns *sqlmodel.Constraint which describes the foreign-key constraint.
// Returns error when the clause is malformed.
func (p *parser) parseForeignKeyConstraint(constraintName string) ([]string, *sqlmodel.Constraint, error) {
	if _, err := p.ExpectKeyword(keywordKEY); err != nil {
		return nil, nil, err
	}
	columns, columnError := p.parseColumnList()
	if columnError != nil {
		return nil, nil, columnError
	}
	if _, err := p.ExpectKeyword(keywordREFERENCES); err != nil {
		return nil, nil, err
	}
	foreignTable, foreignColumns, err := p.parseForeignKeyTarget()
	if err != nil {
		return nil, nil, err
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

// parseForeignKeyTarget parses what follows the REFERENCES of a foreign key: the parent
// table, its optional column list, and the actions and deferral that may follow.
//
// Returns string which is the referenced table name.
// Returns []string which lists the referenced column names.
// Returns error when the table name is missing or a part of the clause is malformed.
func (p *parser) parseForeignKeyTarget() (string, []string, error) {
	tableName, err := p.parseTableName()
	if err != nil {
		return "", nil, err
	}
	var columns []string
	if p.Current().Kind == tokenLeftParen {
		columns, err = p.parseColumnList()
		if err != nil {
			return "", nil, err
		}
	}
	if err := p.skipForeignKeyActions(); err != nil {
		return "", nil, err
	}
	return tableName, columns, nil
}

// skipForeignKeyActions consumes the clauses that may follow a foreign key's parent
// table: ON DELETE, ON UPDATE or ON INSERT with an action, MATCH with a name, and then
// the deferral.
//
// Returns error when a clause is malformed.
func (p *parser) skipForeignKeyActions() error {
	for {
		switch {
		case p.MatchKeyword(keywordON):
			if _, err := p.ExpectKeyword("DELETE", "UPDATE", "INSERT"); err != nil {
				return err
			}
			if err := p.skipForeignKeyAction(); err != nil {
				return err
			}
		case p.MatchKeyword("MATCH"):
			if _, err := p.parseIdentifierOrKeyword(); err != nil {
				return err
			}
		default:
			return p.skipDeferral()
		}
	}
}

// skipForeignKeyAction consumes the action a foreign key takes when its parent row
// changes: SET NULL, SET DEFAULT, CASCADE, RESTRICT or NO ACTION.
//
// Returns error when no action is written.
func (p *parser) skipForeignKeyAction() error {
	var err error
	switch {
	case p.MatchKeyword(keywordSET):
		_, err = p.ExpectKeyword(keywordNULL, "DEFAULT")
	case p.MatchKeyword("NO"):
		_, err = p.ExpectKeyword("ACTION")
	default:
		_, err = p.ExpectKeyword("CASCADE", "RESTRICT")
	}
	return err
}

// skipDeferral consumes an optional foreign key deferral, `[NOT] DEFERRABLE [INITIALLY
// DEFERRED | INITIALLY IMMEDIATE]`. A NOT that DEFERRABLE does not follow is left for the
// next column constraint, as in NOT NULL.
//
// Returns error when INITIALLY is not followed by DEFERRED or IMMEDIATE.
func (p *parser) skipDeferral() error {
	if p.IsKeyword(keywordNOT) && p.Peek().IsKeyword(tokenIdentifier, "DEFERRABLE") {
		p.Advance()
	}
	if !p.MatchKeyword("DEFERRABLE") || !p.MatchKeyword("INITIALLY") {
		return nil
	}
	_, err := p.ExpectKeyword("DEFERRED", "IMMEDIATE")
	return err
}

// parseColumnList parses a parenthesised comma-separated column name list, ignoring sort
// and collate modifiers.
//
// Returns []string which holds the column names in declaration order.
// Returns error when the list is empty, a name is missing, two names are not separated by
// a comma, or the list is not closed.
func (p *parser) parseColumnList() ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected (")
	}
	opening := p.Advance()

	var columns []string
	for {
		if p.AtEnd() {
			return nil, dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
		}
		name, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		columns = append(columns, name)

		if err := p.skipColumnIndexModifiers(); err != nil {
			return nil, err
		}

		switch p.Current().Kind {
		case tokenComma:
			p.Advance()
		case tokenRightParen:
			p.Advance()
			return columns, nil
		default:
			if p.AtEnd() {
				return nil, dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
			}
			return nil, dialectkit.NewParseError(p.Current(), "expected , or ) after a column name")
		}
	}
}

// skipColumnIndexModifiers consumes any per-column index modifiers (ASC / DESC / COLLATE
// <name>) following a column name, in any order. COLLATE is followed by a collation-name
// identifier which must also be consumed; leaving it behind would make the caller read
// the collation name as a phantom column.
//
// Returns error when COLLATE names no collation.
func (p *parser) skipColumnIndexModifiers() error {
	for {
		if p.MatchKeyword("ASC") || p.MatchKeyword("DESC") {
			continue
		}
		if !p.MatchKeyword(keywordCOLLATE) {
			return nil
		}
		if _, err := p.parseIdentifierOrKeyword(); err != nil {
			return err
		}
	}
}

// skipDefaultValue consumes the value that follows DEFAULT: a parenthesised expression, a
// literal, a name, or a number, string, blob, NULL or current time with a sign.
//
// Returns error when no value follows DEFAULT, or a parenthesised default expression is
// unmatched.
func (p *parser) skipDefaultValue() error {
	if p.Current().Kind == tokenLeftParen {
		return p.skipParenthesised()
	}

	signed := false
	if p.Current().Kind == tokenOperator && (p.Current().Value == "+" || p.Current().Value == "-") {
		p.Advance()
		signed = true
	}

	value := p.Current()
	switch value.Kind {
	case tokenNumber, tokenString, tokenBlobLiteral:
		p.Advance()
		return nil
	case tokenIdentifier:
		if !signed || value.IsKeyword(tokenIdentifier, keywordNULL) || currentTimeKeywords[value.Keyword(tokenIdentifier)] {
			p.Advance()
			return nil
		}
	default:
	}
	return dialectkit.NewParseError(value, "expected a default value")
}

// parseDropTable parses a DROP TABLE statement.
//
// Returns *ast.CatalogueMutation which describes the table to drop.
// Returns error when the statement is malformed.
func (p *parser) parseDropTable() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordDROP, keywordTABLE); err != nil {
		return nil, err
	}

	ifExists, err := p.matchIfExists()
	if err != nil {
		return nil, err
	}

	tableName, err := p.parseTableName()
	if err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(ast.MutationDropTable, "", tableName, ast.WithIfExists(ifExists)), nil
}

// parseAlterTable parses an ALTER TABLE statement.
//
// Takes engine (*Dialect) which provides type normalisation for ADD COLUMN clauses.
//
// Returns *ast.CatalogueMutation which describes the alteration.
// Returns error when the action is unsupported or the statement is malformed.
func (p *parser) parseAlterTable(engine *Dialect) (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence("ALTER", keywordTABLE); err != nil {
		return nil, err
	}

	tableName, err := p.parseTableName()
	if err != nil {
		return nil, err
	}

	if p.MatchKeyword("ADD") {
		return p.parseAlterTableAdd(engine, tableName)
	}

	if p.MatchKeyword("RENAME") {
		return p.parseAlterTableRename(tableName)
	}

	if p.MatchKeyword(keywordDROP) {
		return p.parseAlterTableDrop(tableName)
	}

	return nil, dialectkit.NewParseError(p.Current(), "expected ADD, RENAME or DROP: unsupported ALTER TABLE operation")
}

// parseAlterTableAdd parses an ALTER TABLE ADD COLUMN action.
//
// Takes engine (*Dialect) which provides type normalisation.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the new column.
// Returns error when the column fails to parse.
func (p *parser) parseAlterTableAdd(engine *Dialect, tableName string) (*ast.CatalogueMutation, error) {
	p.MatchKeyword("COLUMN")
	column, _, _, columnError := p.parseColumnDefinition(engine)
	if columnError != nil {
		return nil, columnError
	}
	return ast.NewCatalogueMutation(
		ast.MutationAlterTableAddColumn,
		"",
		tableName,
		ast.WithColumns([]sqlmodel.Column{column}),
	), nil
}

// parseAlterTableRename parses an ALTER TABLE RENAME action covering both table rename
// and column rename.
//
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the rename.
// Returns error when the action is malformed.
func (p *parser) parseAlterTableRename(tableName string) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword("TO") {
		newName, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			return nil, nameError
		}
		return ast.NewCatalogueMutation(
			ast.MutationAlterTableRenameTable,
			"",
			tableName,
			ast.WithNewName(newName),
		), nil
	}

	p.MatchKeyword("COLUMN")
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
		"",
		tableName,
		ast.WithColumnName(oldName),
		ast.WithNewName(newName),
	), nil
}

// parseAlterTableDrop parses an ALTER TABLE DROP COLUMN action.
//
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the column to drop.
// Returns error when the column name fails to parse.
func (p *parser) parseAlterTableDrop(tableName string) (*ast.CatalogueMutation, error) {
	p.MatchKeyword("COLUMN")
	columnName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nil, nameError
	}
	return ast.NewCatalogueMutation(
		ast.MutationAlterTableDropColumn,
		"",
		tableName,
		ast.WithColumnName(columnName),
	), nil
}

// parseGeneratedKind reads the optional STORED or VIRTUAL suffix of a GENERATED column.
//
// Returns sqlmodel.GeneratedKind which defaults to virtual when no suffix is present.
func (p *parser) parseGeneratedKind() sqlmodel.GeneratedKind {
	if p.MatchKeyword("STORED") {
		return sqlmodel.GeneratedKindStored
	}
	p.MatchKeyword("VIRTUAL")
	return sqlmodel.GeneratedKindVirtual
}

// isRowidAliasType reports whether a declared column type makes a single-column PRIMARY
// KEY an alias for the 64-bit rowid.
//
// SQLite treats a primary-key column as the rowid alias only when its declared type is
// exactly "INTEGER" (in any case); "INT", "BIGINT", and other integer spellings instead
// create an ordinary indexed column, so they are excluded.
//
// Takes typeName (string) which is the raw declared type spelling.
//
// Returns bool which is true when the type is the rowid alias spelling.
func isRowidAliasType(typeName string) bool {
	return strings.EqualFold(strings.TrimSpace(typeName), "integer")
}

// markPrimaryKeyNotNull makes inline and table-level primary-key columns non-nullable for
// WITHOUT ROWID or STRICT tables. Ordinary rowid tables retain nullable primary keys
// unless a column aliases rowid or declares NOT NULL.
//
// Takes body (*tableBodyResult) whose primary-key columns are updated in place.
func markPrimaryKeyNotNull(body *tableBodyResult) {
	for index := range body.columns {
		if slices.Contains(body.primaryKey, body.columns[index].Name) {
			body.columns[index].Nullable = false
		}
	}
}

// widenRowidAlias marks an INTEGER PRIMARY KEY as SQLite's defaulted, non-null 64-bit
// rowid.
//
// Composite keys, WITHOUT ROWID tables and inline descending primary keys are excluded.
//
// Takes body (*tableBodyResult) whose matching column is updated in place.
// Takes isWithoutRowID (bool) which is true for a WITHOUT ROWID table.
func widenRowidAlias(body *tableBodyResult, isWithoutRowID bool) {
	if isWithoutRowID || len(body.primaryKey) != 1 {
		return
	}
	primaryKeyColumn := body.primaryKey[0]
	if !body.integerSpelled[primaryKeyColumn] || body.descendingInlineKeys[primaryKeyColumn] {
		return
	}
	for index := range body.columns {
		if body.columns[index].Name == primaryKeyColumn {
			body.columns[index].SQLType.EngineName = typeNameBigint
			body.columns[index].Nullable = false
			body.columns[index].HasDefault = true
		}
	}
}

// isStrictAllowedType reports whether a declared column datatype is one of the spellings
// a STRICT table permits: INT, INTEGER, REAL, TEXT, BLOB, or ANY. SQLite rejects any
// other spelling (SMALLINT, BIGINT, VARCHAR, NUMERIC, BOOLEAN, DATE, ...) at execution
// time.
//
// Takes declared (string) which is the raw declared type spelling.
//
// Returns bool which is true when the spelling is STRICT-legal.
func isStrictAllowedType(declared string) bool {
	switch strings.ToLower(strings.TrimSpace(declared)) {
	case "int", "integer", "real", "text", "blob", "any":
		return true
	default:
		return false
	}
}

// validateStrictColumnTypes rejects a STRICT table that declares a column with a datatype
// SQLite's STRICT mode does not permit, turning a runtime migration failure into a loud
// generation-time error. Every column must name an allowed type; STRICT forbids a
// typeless column.
//
// Takes body (*tableBodyResult) whose declaredTypes are checked.
// Takes isStrict (bool) which is true for a STRICT table.
//
// Returns error naming the first offending column, or nil when the table is not STRICT or
// every column is allowed.
func validateStrictColumnTypes(body *tableBodyResult, isStrict bool) error {
	if !isStrict {
		return nil
	}
	for index := range body.columns {
		declared := body.declaredTypes[body.columns[index].Name]
		if !isStrictAllowedType(declared) {
			return fmt.Errorf(
				"STRICT table column %q has datatype %q which is not one of "+
					"INT, INTEGER, REAL, TEXT, BLOB, ANY",
				body.columns[index].Name, declared,
			)
		}
	}
	return nil
}

// widenStrictIntegers widens a STRICT table's integer columns.
//
// Every integer column of a STRICT table is named BIGINT, the name the dialect gives the
// integers it knows hold any 64-bit value. Such a table enforces INTEGER as a signed
// 64-bit value but rejects the BIGINT spelling itself, so validateStrictColumnTypes has
// already ensured the only integer spellings present are INT/INTEGER.
//
// Takes body (*tableBodyResult) whose integer column SQLTypes are widened in place.
// Takes isStrict (bool) which is true for a STRICT table.
func widenStrictIntegers(body *tableBodyResult, isStrict bool) {
	if !isStrict {
		return
	}
	for index := range body.columns {
		if body.columns[index].SQLType.Category == sqlmodel.TypeCategoryInteger {
			body.columns[index].SQLType.EngineName = typeNameBigint
		}
	}
}

// typeModifierMagnitude reads the magnitude of a type modifier from its number token: a
// decimal or hexadecimal integer, or a fraction or exponent, which is truncated. A
// magnitude past dialectkit.MaxTypeModifier is held at it.
//
// Takes number (token) which is the modifier's number, without its sign.
//
// Returns int which is the magnitude.
// Returns bool which is false when the token is not a number.
func typeModifierMagnitude(number token) (int, bool) {
	if number.Kind != tokenNumber {
		return 0, false
	}
	return lexicalRules.Numbers.TypeModifierMagnitude(number.Value)
}
