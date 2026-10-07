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

package postgres

import (
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxArrayDimensions caps the recorded depth of an array type. Postgres itself caps
	// arrays at 6 dimensions; we accept the same upper bound and silently stop counting
	// beyond it so a maliciously deep `[][][]...` suffix cannot inflate downstream
	// allocations.
	maxArrayDimensions = 6
)

// createTableBody is what the element list of a CREATE TABLE declares.
type createTableBody struct {
	// columns are the declared columns, in order.
	columns []sqlmodel.Column

	// primaryKey lists the primary-key columns, a table-level constraint taking precedence
	// over a column-level one.
	primaryKey []string

	// constraints are the secondary constraints.
	constraints []sqlmodel.Constraint

	// copiedTables are the tables named by LIKE elements, whose columns are copied where the
	// element stands.
	copiedTables []ast.CopiedTable

	// keyColumnsElsewhere lists the primary-key columns the element list does not declare,
	// such as columns a LIKE element or INHERITS supplies.
	keyColumnsElsewhere []string

	// sharding records whether USING HASH shards the primary key.
	sharding keySharding
}

// parseCreateTable parses a CREATE TABLE statement into a catalogue mutation.
//
// The element list must separate every column and table constraint with a comma and close
// with a parenthesis, so a type spelling the parser does not understand fails the
// statement instead of becoming phantom columns. A LIKE element and an INHERITS clause
// copy the parent's columns. A PARTITION OF table records its parent so the catalogue can
// copy the parent's columns into the partition. CREATE FOREIGN TABLE parses the same way,
// its SERVER and OPTIONS clauses skipped. CREATE TABLE ... AS takes its columns from its
// query, named by the column list before AS when it has one.
//
// Takes engine (*Dialect) which resolves column type names against the dialect's type
// catalogue.
//
// Returns *ast.CatalogueMutation which describes the new table.
// Returns error when the statement is malformed.
func (p *parser) parseCreateTable(engine *Dialect) (*ast.CatalogueMutation, error) {
	ifNotExists, err := p.parseCreateTableHeader()
	if err != nil {
		return nil, err
	}

	schema, tableName, err := p.requireSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	if isQuery, columnNames, queryErr := p.parseCreateTableAsHead(); queryErr != nil || isQuery {
		if queryErr != nil {
			return nil, queryErr
		}
		mutation := ast.NewCatalogueMutation(ast.MutationCreateTable, schema, tableName, ast.WithIfNotExists(ifNotExists))
		viewDefinition, bodyErr := p.analyseViewBody(columnNames)
		if bodyErr != nil {
			return nil, bodyErr
		}
		mutation.ViewDefinition = viewDefinition
		return mutation, nil
	}

	if p.MatchKeyword("PARTITION") {
		mutation, partitionError := p.parsePartitionOf(schema, tableName)
		if mutation != nil {
			mutation.IfNotExists = ifNotExists
		}
		return mutation, partitionError
	}

	body, bodyError := p.parseCreateTableElements(engine)
	if bodyError != nil {
		return nil, bodyError
	}

	inheritsTables, inheritsError := p.parseInheritsClause()
	if inheritsError != nil {
		return nil, inheritsError
	}

	storageParameters, storageError := p.parseTrailingTableClauses()
	if storageError != nil {
		return nil, storageError
	}

	mutation := newCreateTableMutation(schema, tableName, &body)
	mutation.InheritsTables = inheritsTables
	mutation.IfNotExists = ifNotExists
	recordStorageParameters(mutation, storageParameters)
	return mutation, nil
}

// parseCreateTableAsHead reads what comes between the name of a CREATE TABLE ... AS and
// its query: an optional list naming the query's columns, and the USING, WITH (...),
// WITHOUT OIDS, ON COMMIT and TABLESPACE clauses, up to and including AS.
//
// A list of bare names, unlike an element list, gives no column a type.
//
// Returns bool which is true when the statement is a CREATE TABLE ... AS, with the cursor
// on its query; false leaves the cursor where it was.
// Returns []string which holds the names the column list gives, or nil.
// Returns error when a column list of bare names is not followed by AS.
func (p *parser) parseCreateTableAsHead() (bool, []string, error) {
	start := p.Position
	var columnNames []string
	if p.isColumnNameList() {
		names, err := p.parsePostgresColumnList()
		if err != nil {
			return false, nil, err
		}
		columnNames = names
	}
	if err := p.skipStorageClauses(); err != nil {
		return false, nil, err
	}
	if p.MatchKeyword(keywordAS) {
		return true, columnNames, nil
	}
	if columnNames != nil {
		return false, nil, p.syntaxError("expected AS before the query whose columns the list names")
	}
	p.Position = start
	return false, nil, nil
}

// isColumnNameList reports whether the cursor is on a parenthesised list of bare names,
// such as the (a, b) of CREATE TABLE t (a, b) AS ..., rather than an element list, whose
// columns each have a type.
//
// Returns bool which is true for a non-empty list of names separated by commas.
func (p *parser) isColumnNameList() bool {
	if p.Current().Kind != tokenLeftParen {
		return false
	}
	for offset := 1; p.PeekAt(offset).Kind == tokenIdentifier; offset += 2 {
		switch p.PeekAt(offset + 1).Kind {
		case tokenComma:
		case tokenRightParen:
			return true
		default:
			return false
		}
	}
	return false
}

// parseCreateTableHeader consumes CREATE, the table's persistence and FOREIGN qualifiers,
// TABLE and an optional IF NOT EXISTS.
//
// Returns bool which is true when the statement said IF NOT EXISTS.
// Returns error when CREATE or TABLE is missing.
func (p *parser) parseCreateTableHeader() (bool, error) {
	if err := p.requireKeyword(keywordCREATE); err != nil {
		return false, err
	}
	if p.MatchKeyword("GLOBAL") || p.MatchKeyword("LOCAL") {
		p.MatchKeyword("TEMP")
		p.MatchKeyword("TEMPORARY")
	}
	p.MatchKeyword("TEMP")
	p.MatchKeyword("TEMPORARY")
	p.MatchKeyword("UNLOGGED")
	p.MatchKeyword(keywordFOREIGN)
	if err := p.requireKeyword(keywordTABLE); err != nil {
		return false, err
	}
	return p.matchIfNotExists(), nil
}

// matchIfNotExists consumes an optional "IF NOT EXISTS" clause.
//
// Returns bool which is true when the clause was present.
func (p *parser) matchIfNotExists() bool {
	if !p.MatchKeyword("IF") {
		return false
	}
	p.MatchKeyword(keywordNOT)
	p.MatchKeyword(keywordEXISTS)
	return true
}

// matchIfExists consumes an optional "IF EXISTS" clause.
//
// Returns bool which is true when the clause was present.
func (p *parser) matchIfExists() bool {
	if !p.MatchKeyword("IF") {
		return false
	}
	p.MatchKeyword(keywordEXISTS)
	return true
}

// skipToStatementEnd advances the parser past the end of the current statement.
func (p *parser) skipToStatementEnd() {
	for !p.AtEnd() && p.Current().Kind != tokenSemicolon && p.Current().Kind != tokenEOF {
		p.Advance()
	}
}

// parseCreateTableElements parses the parenthesised column and constraint list of a
// CREATE TABLE, requiring a comma or the closing parenthesis after each element.
//
// Takes engine (*Dialect) which resolves column type names against the dialect's type
// catalogue.
//
// Returns createTableBody which holds the declared columns, keys and constraints.
// Returns error when an element is malformed or not followed by ',' or ')'.
func (p *parser) parseCreateTableElements(engine *Dialect) (createTableBody, error) {
	var body createTableBody
	if p.Current().Kind != tokenLeftParen {
		return body, p.syntaxError("expected '(' to open the column list")
	}
	p.Advance()

	var tableConstraintPrimaryKey []string
	for p.Current().Kind != tokenRightParen {
		elementPrimaryKey, err := p.parseCreateTableElement(engine, &body)
		if err != nil {
			return body, err
		}
		tableConstraintPrimaryKey = appendConstraintPrimaryKey(tableConstraintPrimaryKey, elementPrimaryKey)
		if p.Current().Kind == tokenComma {
			p.Advance()
			continue
		}
		if p.Current().Kind != tokenRightParen {
			return body, p.syntaxError("expected ',' or ')' after the table element")
		}
	}
	p.Advance()

	if len(tableConstraintPrimaryKey) > 0 {
		body.primaryKey = tableConstraintPrimaryKey
		body.keyColumnsElsewhere = markKeyColumnsNotNull(body.columns, tableConstraintPrimaryKey)
	}
	body.sharding = p.primaryKeySharding
	return body, nil
}

// parseCreateTableElement parses one element of a CREATE TABLE list: a LIKE clause, a
// table constraint or a column, recording it on body.
//
// Takes engine (*Dialect) which resolves column type names.
// Takes body (*createTableBody) which receives the element.
//
// Returns []string which lists a table-level primary key's columns, or nil.
// Returns error when the element is malformed.
func (p *parser) parseCreateTableElement(engine *Dialect, body *createTableBody) ([]string, error) {
	if p.AtEnd() {
		return nil, p.syntaxError("expected a column definition")
	}
	if p.MatchKeyword("LIKE") {
		return nil, p.parseLikeElement(body)
	}
	if engine.settings.InlineIndexElements && p.isInlineIndexElement() {
		constraint, err := p.parseInlineIndexElement()
		if err != nil {
			return nil, err
		}
		body.constraints = appendConstraint(body.constraints, constraint)
		return nil, nil
	}
	if p.isPostgresTableConstraint() {
		constraintPrimaryKey, constraint, err := p.parsePostgresTableConstraint()
		if err != nil {
			return nil, err
		}
		body.constraints = appendConstraint(body.constraints, constraint)
		return constraintPrimaryKey, nil
	}
	column, columnPrimaryKey, err := p.parsePostgresColumnDefinition(engine)
	if err != nil {
		return nil, err
	}
	body.columns = append(body.columns, column)
	if columnPrimaryKey {
		body.primaryKey = append(body.primaryKey, column.Name)
	}
	return nil, nil
}

// parseLikeElement parses the rest of a LIKE element of a CREATE TABLE, the table whose
// columns the new table copies and its INCLUDING and EXCLUDING options, recording the
// table on body where the element stands. INCLUDING INDEXES, or ALL, copies the primary
// key too.
//
// Takes body (*createTableBody) which receives the copied table.
//
// Returns error when the table name is malformed.
func (p *parser) parseLikeElement(body *createTableBody) error {
	parentSchema, parentName, err := p.requireSchemaQualifiedName()
	if err != nil {
		return err
	}
	keys := false
	for p.IsKeyword("INCLUDING", "EXCLUDING") {
		including := p.MatchKeyword("INCLUDING")
		if !including {
			p.Advance()
		}
		option := p.Advance()
		if option.IsKeyword(tokenIdentifier, "ALL") || option.IsKeyword(tokenIdentifier, "INDEXES") {
			keys = including
		}
	}
	body.copiedTables = append(body.copiedTables, ast.CopiedTable{
		Table:    ast.TableReference{Schema: parentSchema, Name: parentName, Alias: ""},
		Position: len(body.columns),
		Keys:     keys,
	})
	return nil
}

// isInlineIndexElement reports whether the current token starts an index or column-family
// element of a CREATE TABLE: INDEX or FAMILY, or INDEX after UNIQUE, INVERTED or VECTOR,
// then a parenthesised list, either directly or after a name.
//
// A column named index or family is followed by its type instead, and a delimited
// identifier is always a column.
//
// Returns bool which is true at such an element.
func (p *parser) isInlineIndexElement() bool {
	offset := 0
	if p.IsKeyword(keywordUNIQUE, "INVERTED", "VECTOR") {
		offset = 1
	}
	introducer := p.PeekAt(offset)
	if !introducer.IsKeyword(tokenIdentifier, "INDEX") && (offset != 0 || !introducer.IsKeyword(tokenIdentifier, "FAMILY")) {
		return false
	}
	next := p.PeekAt(offset + 1)
	if next.Kind == tokenLeftParen {
		return true
	}
	return next.Kind == tokenIdentifier && p.PeekAt(offset+2).Kind == tokenLeftParen
}

// parseInlineIndexElement parses an index or column-family element of a CREATE TABLE: the
// introducer, an optional name and the parenthesised list, then whatever the element
// carries up to the next element, such as USING HASH, STORING (...), a WHERE predicate or
// NOT VISIBLE.
//
// Returns *sqlmodel.Constraint which is the unique constraint a UNIQUE INDEX declares, or
// nil for any other element.
// Returns error when the list is missing, or a parenthesis is unbalanced.
func (p *parser) parseInlineIndexElement() (*sqlmodel.Constraint, error) {
	unique := p.MatchKeyword(keywordUNIQUE)
	if !unique && !p.MatchKeyword("INVERTED") {
		p.MatchKeyword("VECTOR")
	}
	p.Advance()
	name := ""
	if p.Current().Kind == tokenIdentifier {
		name = p.Advance().Value
	}
	var columns []string
	if unique {
		parsed, err := p.parseIndexColumnNames()
		if err != nil {
			return nil, err
		}
		columns = parsed
	} else if err := p.requireSkipParenthesised(); err != nil {
		return nil, err
	}
	if err := p.skipToListElementEnd(); err != nil {
		return nil, err
	}
	if !unique {
		return nil, nil
	}
	return &sqlmodel.Constraint{
		Name:           name,
		Kind:           sqlmodel.ConstraintUnique,
		Columns:        columns,
		ForeignTable:   "",
		ForeignColumns: nil,
		Origin:         sqlmodel.MigrationOrigin{},
	}, nil
}

// parseIndexColumnNames parses the parenthesised list of an index, keeping the name of
// each element that is a plain column and skipping its direction, operator class and any
// element that is an expression.
//
// Returns []string which lists the plain columns in order.
// Returns error when the list is missing or a parenthesis is unbalanced.
func (p *parser) parseIndexColumnNames() ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, p.syntaxError("expected '(' to open the index's column list")
	}
	p.Advance()
	var columns []string
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.Current().Kind == tokenIdentifier && (p.Peek().Kind == tokenComma || p.Peek().Kind == tokenRightParen || p.Peek().Kind == tokenIdentifier) {
			columns = append(columns, p.Current().Value)
		}
		if err := p.skipToListElementEnd(); err != nil {
			return nil, err
		}
		if p.Current().Kind == tokenComma {
			p.Advance()
		}
	}
	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxError("expected ')' to close the index's column list")
	}
	p.Advance()
	return columns, nil
}

// skipToListElementEnd consumes the rest of an element of a parenthesised list, such as a
// CREATE TABLE element or an index's column, up to the comma or closing parenthesis that
// ends it at the list's own depth.
//
// Returns error when a parenthesis inside the element is unbalanced.
func (p *parser) skipToListElementEnd() error {
	depth := 0
	for !p.AtEnd() {
		kind := p.Current().Kind
		if depth == 0 && (kind == tokenComma || kind == tokenRightParen) {
			return nil
		}
		switch kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		default:
		}
		p.Advance()
	}
	if depth > 0 {
		return p.syntaxError("unmatched parenthesis")
	}
	return nil
}

// parseInheritsClause parses an optional INHERITS (...) clause.
//
// Returns []ast.TableReference which lists the parent tables, or nil when no clause is
// present.
// Returns error when a parent table name cannot be parsed.
func (p *parser) parseInheritsClause() ([]ast.TableReference, error) {
	if !p.MatchKeyword("INHERITS") {
		return nil, nil
	}
	if p.Current().Kind != tokenLeftParen {
		return nil, nil
	}

	p.Advance()
	var tables []ast.TableReference
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		parentSchema, parentName, inheritsError := p.parseSchemaQualifiedName()
		if inheritsError != nil {
			return nil, inheritsError
		}
		tables = append(tables, ast.TableReference{
			Schema: parentSchema,
			Name:   parentName,
			Alias:  "",
		})
		if p.Current().Kind == tokenComma {
			p.Advance()
		}
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	return tables, nil
}

// parsePostgresColumnDefinition parses a single column declaration.
//
// A serial column is NOT NULL with a default, as PostgreSQL makes it. The constraints
// that follow the type are read until none matches; the caller decides whether what
// remains may end the column.
//
// Takes engine (*Dialect) which resolves the column type name.
//
// Returns sqlmodel.Column which describes the parsed column.
// Returns bool which is true when the column carries an inline PRIMARY KEY constraint.
// Returns error when the column name cannot be parsed.
func (p *parser) parsePostgresColumnDefinition(engine *Dialect) (sqlmodel.Column, bool, error) {
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return sqlmodel.Column{}, false, fmt.Errorf("parsing column name: %w", err)
	}

	isSerial := isSerialTypeName(p.Current())
	sqlType, arrayDimensions := p.parseColumnType(engine)

	column := newNullableColumn(name, sqlType, arrayDimensions)
	if isSerial {
		column.Nullable = false
		column.HasDefault = true
	}

	isPrimaryKey, constraintError := p.parseColumnConstraints(&column)
	if constraintError != nil {
		return sqlmodel.Column{}, false, constraintError
	}

	return column, isPrimaryKey, nil
}

// parseColumnConstraints parses zero or more column-level constraints.
//
// Takes column (*sqlmodel.Column) which is updated with each constraint.
//
// Returns bool which is true when any constraint declared PRIMARY KEY.
// Returns error when a constraint is malformed.
func (p *parser) parseColumnConstraints(column *sqlmodel.Column) (bool, error) {
	isPrimaryKey := false

	for !p.AtEnd() && p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen {
		primary, handled, err := p.parseOnePostgresColumnConstraint(column)
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

// parseOnePostgresColumnConstraint parses a single column-level constraint.
//
// Takes column (*sqlmodel.Column) which is updated according to the recognised
// constraint.
//
// Returns isPrimary (bool) which is true when the constraint is PRIMARY KEY.
// Returns handled (bool) which is true when a constraint was consumed.
// Returns err (error) when the constraint is malformed.
func (p *parser) parseOnePostgresColumnConstraint(column *sqlmodel.Column) (isPrimary bool, handled bool, err error) {
	if handled, err = p.parseFlavourColumnForm(column); handled || err != nil {
		return false, handled, err
	}
	switch {
	case p.IsKeyword(keywordPRIMARY) && p.Peek().IsKeyword(tokenIdentifier, keywordKEY):
		p.Advance()
		p.Advance()
		column.Nullable = false
		return true, true, p.parsePrimaryKeyIndexParameters()
	case p.IsKeyword(keywordNOT) && p.Peek().IsKeyword(tokenIdentifier, keywordNULL):
		p.Advance()
		p.Advance()
		column.Nullable = false
		return false, true, nil
	case p.MatchKeyword(keywordNULL):
		column.Nullable = true
		return false, true, nil
	case p.MatchKeyword(keywordDEFAULT):
		column.HasDefault = true
		return false, true, p.skipPostgresDefaultValue()
	case p.MatchKeyword("GENERATED"):
		return false, true, p.parseGeneratedClause(column)
	}

	handled, err = p.parsePostgresSecondaryConstraint()
	return false, handled, err
}

// parseFlavourColumnForm parses a column clause a flavour enables: AS (expression) STORED
// or VIRTUAL, which declares a computed column, and NOT VISIBLE, which hides the column
// from SELECT *.
//
// Takes column (*sqlmodel.Column) which is updated according to the clause.
//
// Returns bool which is true when a clause was consumed.
// Returns error when the computed column's expression is missing or unbalanced.
func (p *parser) parseFlavourColumnForm(column *sqlmodel.Column) (bool, error) {
	switch {
	case p.computedColumnShorthand && p.IsKeyword(keywordAS) && p.Peek().Kind == tokenLeftParen:
		p.Advance()
		if err := p.requireSkipParenthesised(); err != nil {
			return true, err
		}
		column.IsGenerated = true
		column.GeneratedKind = sqlmodel.GeneratedKindVirtual
		if p.MatchKeyword("STORED") {
			column.GeneratedKind = sqlmodel.GeneratedKindStored
		} else {
			p.MatchKeyword("VIRTUAL")
		}
		return true, nil
	case p.notVisibleColumns && p.IsKeyword(keywordNOT) && p.Peek().IsKeyword(tokenIdentifier, "VISIBLE"):
		p.Advance()
		p.Advance()
		column.Hidden = true
		return true, nil
	default:
		return false, nil
	}
}

// parsePostgresSecondaryConstraint parses the column constraints that leave the column's
// type, nullability and default alone: UNIQUE, CHECK, REFERENCES, a constraint name, a
// collation, the compression, storage and foreign-table options, and the deferral
// attributes.
//
// Returns bool which is true when a clause was consumed.
// Returns error when the clause is malformed.
func (p *parser) parsePostgresSecondaryConstraint() (bool, error) {
	switch {
	case p.MatchKeyword(keywordUNIQUE):
		p.skipNullsDistinct()
		return true, p.skipIndexParameters()
	case p.MatchKeyword(keywordCHECK):
		return true, p.skipCheckBody()
	case p.MatchKeyword("REFERENCES"):
		_, _, err := p.parseReferencedTable()
		return true, err
	case p.MatchKeyword(keywordCONSTRAINT):
		_, err := p.requireIdentifierOrKeyword()
		return true, err
	case p.MatchKeyword("COLLATE"):
		_, _, err := p.requireSchemaQualifiedName()
		return true, err
	case p.MatchKeyword("COMPRESSION") || p.MatchKeyword("STORAGE"):
		_, err := p.requireIdentifierOrKeyword()
		return true, err
	case p.IsKeyword("OPTIONS") && p.Peek().Kind == tokenLeftParen:
		p.Advance()
		return true, p.requireSkipParenthesised()
	case p.IsKeyword(keywordWITH) && p.Peek().IsKeyword(tokenIdentifier, "OPTIONS"):
		p.Advance()
		p.Advance()
		return true, nil
	}
	return p.skipConstraintAttribute(), nil
}

// skipConstraintAttribute consumes one constraint attribute: DEFERRABLE, NOT DEFERRABLE,
// INITIALLY DEFERRED or IMMEDIATE, NOT VALID, or NO INHERIT.
//
// Returns bool which is true when an attribute was consumed.
func (p *parser) skipConstraintAttribute() bool {
	switch {
	case p.MatchKeyword("DEFERRABLE"):
		return true
	case p.IsKeyword(keywordNOT) && p.Peek().IsKeyword(tokenIdentifier, "DEFERRABLE"),
		p.IsKeyword(keywordNOT) && p.Peek().IsKeyword(tokenIdentifier, "VALID"),
		p.IsKeyword("NO") && p.Peek().IsKeyword(tokenIdentifier, "INHERIT"),
		p.IsKeyword("INITIALLY") && p.Peek().Kind == tokenIdentifier:
		p.Advance()
		p.Advance()
		return true
	default:
		return false
	}
}

// skipConstraintAttributes consumes every constraint attribute that follows.
func (p *parser) skipConstraintAttributes() {
	for p.skipConstraintAttribute() {
	}
}

// skipNullsDistinct consumes a NULLS [NOT] DISTINCT clause of a unique constraint.
func (p *parser) skipNullsDistinct() {
	if !p.IsKeyword(keywordNULLS) {
		return
	}
	next := p.Peek()
	if next.IsKeyword(tokenIdentifier, "DISTINCT") ||
		(next.IsKeyword(tokenIdentifier, keywordNOT) && p.PeekAt(2).IsKeyword(tokenIdentifier, "DISTINCT")) {
		p.Advance()
		p.MatchKeyword(keywordNOT)
		p.Advance()
	}
}

// skipCheckBody consumes the parenthesised expression of a CHECK constraint and an
// optional NO INHERIT.
//
// Returns error when the expression is missing or unbalanced.
func (p *parser) skipCheckBody() error {
	if err := p.requireSkipParenthesised(); err != nil {
		return err
	}
	if p.IsKeyword("NO") && p.Peek().IsKeyword(tokenIdentifier, "INHERIT") {
		p.Advance()
		p.Advance()
	}
	return nil
}

// parseGeneratedClause parses a GENERATED column clause.
//
// An identity column is NOT NULL with a default, as PostgreSQL makes it; a stored
// generated column is marked generated.
//
// Takes column (*sqlmodel.Column) which is updated with default and generated-kind
// metadata.
//
// Returns error when the clause is malformed or its parentheses are unbalanced.
func (p *parser) parseGeneratedClause(column *sqlmodel.Column) error {
	if !p.MatchKeyword("ALWAYS") {
		if err := p.requireKeywordSequence(keywordBY, keywordDEFAULT); err != nil {
			return err
		}
	}
	if err := p.requireKeyword(keywordAS); err != nil {
		return err
	}
	if p.MatchKeyword("IDENTITY") {
		column.HasDefault = true
		column.Nullable = false
		if p.Current().Kind == tokenLeftParen {
			return p.requireSkipParenthesised()
		}
		return nil
	}
	if err := p.requireSkipParenthesised(); err != nil {
		return err
	}
	column.IsGenerated = true
	column.GeneratedKind = sqlmodel.GeneratedKindStored
	p.MatchKeyword("STORED")
	p.MatchKeyword("VIRTUAL")
	return nil
}

// parseColumnType parses a column type, accepting an optional SETOF prefix.
//
// Takes engine (*Dialect) which normalises the parsed type name.
//
// Returns sqlmodel.SQLType which is the resolved column type.
// Returns int which is the number of trailing array dimensions.
func (p *parser) parseColumnType(engine *Dialect) (sqlmodel.SQLType, int) {
	if p.MatchKeyword("SETOF") {
		sqlType, dimensions := p.parseColumnTypeInner(engine.typeResolver)
		return sqlType, dimensions
	}

	return p.parseColumnTypeInner(engine.typeResolver)
}

// parseColumnTypeInner parses the type body after any SETOF prefix: a possibly
// schema-qualified or multi-word name, its modifiers, a time zone qualifier after
// timestamp or time, interval fields, and array brackets or the ARRAY keyword.
//
// An unquoted name is folded to lower case, as the dialect folds identifiers, and a
// quoted one keeps its letters and is never read as a keyword, so a type the catalogue
// does not know carries the name it was created with.
//
// Takes types (*typeResolver) which resolves the parsed type name.
//
// Returns sqlmodel.SQLType which is the resolved column type.
// Returns int which is the number of trailing array dimensions.
func (p *parser) parseColumnTypeInner(types *typeResolver) (sqlmodel.SQLType, int) {
	if p.Current().Kind != tokenIdentifier {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), 0
	}

	if p.isPostgresColumnConstraintKeyword() {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), 0
	}

	nameToken := p.Advance()
	typeName, quoted := nameToken.Value, nameToken.Quoted

	typeSchema := ""
	if p.Current().Kind == tokenDot && (quoted || !isMultiWordTypePrefix(typeName)) {
		p.Advance()
		qualifiedToken := p.Current()
		qualifiedName, qualifiedError := p.parseIdentifierOrKeyword()
		if qualifiedError != nil {
			return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, typeName), 0
		}
		typeSchema, typeName, quoted = typeName, qualifiedName, qualifiedToken.Quoted
	}

	fullName := typeName
	if !quoted {
		fullName = p.consumeMultiWordType(typeName)
	}

	var modifiers []int
	if p.Current().Kind == tokenLeftParen {
		modifiers = p.parseTypeModifiers()
	}
	if !quoted {
		switch fullName {
		case "timestamp", "time":
			fullName = p.consumeTemporalZoneSuffix(fullName)
		case "interval":
			p.skipIntervalFields()
		default:
		}
	}

	arrayDimensions := p.parseArrayDimensions()

	sqlType := types.resolveName(fullName, quoted, modifiers)
	if typeSchema != "" {
		sqlType.Schema = typeSchema
	}

	return sqlType, arrayDimensions
}

// parseArrayDimensions consumes trailing array subscript brackets after a type name,
// counting how many dimensions were declared, and the SQL-standard ARRAY or ARRAY[n]
// suffix, which declares one. Counting stops at maxArrayDimensions so a pathologically
// deep bracket suffix cannot inflate downstream allocations.
//
// Returns int which is the number of array dimensions, capped at maxArrayDimensions.
func (p *parser) parseArrayDimensions() int {
	dimensions := 0
	for p.Current().Kind == tokenLeftBracket {
		p.skipArrayBound()
		if dimensions < maxArrayDimensions {
			dimensions++
		}
	}
	if dimensions == 0 && p.MatchKeyword("ARRAY") {
		if p.Current().Kind == tokenLeftBracket {
			p.skipArrayBound()
		}
		dimensions = 1
	}
	return dimensions
}

// skipArrayBound consumes one bracketed array bound, [] or [n], recording a syntax error
// when the bracket is not closed.
func (p *parser) skipArrayBound() {
	p.Advance()
	if p.Current().Kind == tokenNumber {
		p.Advance()
	}
	if p.Current().Kind != tokenRightBracket {
		_ = p.syntaxError("expected ']' to close the array bound")
		return
	}
	p.Advance()
}

// skipIntervalFields consumes the field qualifier of an interval type, such as YEAR, DAY
// TO SECOND or SECOND(3).
func (p *parser) skipIntervalFields() {
	for p.IsKeyword("YEAR", "MONTH", "DAY", "HOUR", "MINUTE", "SECOND") {
		p.Advance()
		if !p.MatchKeyword("TO") {
			break
		}
	}
	if p.Current().Kind == tokenLeftParen {
		p.parseTypeModifiers()
	}
}

// consumeMultiWordType assembles the full name of a multi-word type, such as double
// precision, from its unquoted first word.
//
// Takes firstWord (string) which is the type's first word, folded to lower case.
//
// Returns string which is the multi-word type name, or firstWord unchanged when no
// further word of a multi-word type follows it.
func (p *parser) consumeMultiWordType(firstWord string) string {
	switch firstWord {
	case "double":
		if p.MatchKeyword("PRECISION") {
			return "double precision"
		}
	case "character":
		if p.MatchKeyword("VARYING") {
			return "character varying"
		}
	case "bit":
		if p.MatchKeyword("VARYING") {
			return "bit varying"
		}
	default:
	}

	return firstWord
}

// consumeTemporalZoneSuffix appends a WITH TIME ZONE or WITHOUT TIME ZONE qualifier,
// consuming it only when all three words are present.
//
// Takes base (string) which is the timestamp or time base name.
//
// Returns string which is the base, optionally extended with the qualifier.
func (p *parser) consumeTemporalZoneSuffix(base string) string {
	if !p.Peek().IsKeyword(tokenIdentifier, keywordTIME) || !p.PeekAt(2).IsKeyword(tokenIdentifier, keywordZONE) {
		return base
	}
	switch {
	case p.IsKeyword(keywordWITH):
		p.Position += timeZoneClauseLength
		return base + " with time zone"
	case p.IsKeyword("WITHOUT"):
		p.Position += timeZoneClauseLength
		return base + " without time zone"
	default:
		return base
	}
}

// parseTypeModifiers parses parenthesised numeric type modifiers, reading them as
// readTypeModifiers does. The list is skipped as a balanced group, recording a syntax
// error when it is not closed.
//
// Returns []int which lists the parsed numeric modifiers, or nil when no parenthesised
// list is present.
func (p *parser) parseTypeModifiers() []int {
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	open := p.Position
	_ = p.requireSkipParenthesised()
	return readTypeModifiers(p.Tokens[open+1 : p.Position])
}

// isPostgresColumnConstraintKeyword reports whether the current token starts a
// column-level constraint clause.
//
// Returns bool which is true for any recognised constraint keyword.
func (p *parser) isPostgresColumnConstraintKeyword() bool {
	return p.IsKeyword(keywordPRIMARY, keywordNOT, keywordNULL, keywordUNIQUE, keywordCHECK, keywordDEFAULT,
		"COLLATE", "REFERENCES", "GENERATED", keywordCONSTRAINT)
}

// isPostgresTableConstraint reports whether the current token starts a table-level
// constraint clause. A delimited identifier never does, so a column named "check" stays a
// column.
//
// Returns bool which is true for any recognised table-level keyword.
func (p *parser) isPostgresTableConstraint() bool {
	switch {
	case p.IsKeyword(keywordCONSTRAINT, keywordCHECK, "EXCLUDE"):
		return true
	case p.IsKeyword(keywordPRIMARY):
		return p.Peek().IsKeyword(tokenIdentifier, keywordKEY)
	case p.IsKeyword(keywordFOREIGN):
		return p.Peek().IsKeyword(tokenIdentifier, keywordKEY)
	case p.IsKeyword(keywordUNIQUE):
		return p.Peek().Kind == tokenLeftParen || p.Peek().IsKeyword(tokenIdentifier, keywordNULLS)
	default:
		return false
	}
}

// parsePostgresTableConstraint parses a single table-level constraint and its attributes.
//
// Returns []string which lists primary-key columns when applicable, else nil.
// Returns *sqlmodel.Constraint which describes the constraint when not a primary key,
// else nil.
// Returns error when the constraint body cannot be parsed.
func (p *parser) parsePostgresTableConstraint() ([]string, *sqlmodel.Constraint, error) {
	constraintName, err := p.parseOptionalConstraintName()
	if err != nil {
		return nil, nil, err
	}

	var primaryKey []string
	var constraint *sqlmodel.Constraint
	switch {
	case p.MatchKeyword(keywordPRIMARY):
		primaryKey, err = p.parseTablePrimaryKey()
	case p.MatchKeyword(keywordUNIQUE):
		constraint, err = p.parseTableUnique(constraintName)
	case p.MatchKeyword(keywordCHECK):
		constraint, err = p.parseTableCheck(constraintName)
	case p.MatchKeyword(keywordFOREIGN):
		constraint, err = p.parseTableForeignKey(constraintName)
	case p.MatchKeyword("EXCLUDE"):
		err = p.parseTableExclude()
	default:
		err = p.syntaxError("expected a table constraint")
	}
	if err != nil {
		return nil, nil, err
	}
	p.skipConstraintAttributes()
	return primaryKey, constraint, nil
}

// parseOptionalConstraintName consumes an optional CONSTRAINT name clause.
//
// Returns string which is the constraint name, or empty when absent.
// Returns error when CONSTRAINT is not followed by a name.
func (p *parser) parseOptionalConstraintName() (string, error) {
	if !p.MatchKeyword(keywordCONSTRAINT) {
		return "", nil
	}
	return p.requireIdentifierOrKeyword()
}

// parseTablePrimaryKey parses a PRIMARY KEY (...) table constraint.
//
// Returns []string which lists the primary-key columns.
// Returns error when the column list is missing or malformed.
func (p *parser) parseTablePrimaryKey() ([]string, error) {
	if err := p.requireKeyword(keywordKEY); err != nil {
		return nil, err
	}
	columns, err := p.parsePostgresColumnList()
	if err != nil {
		return nil, err
	}
	return columns, p.parsePrimaryKeyIndexParameters()
}

// parseTableUnique parses a UNIQUE [NULLS [NOT] DISTINCT] (...) table constraint.
//
// Takes constraintName (string) which optionally names the constraint.
//
// Returns *sqlmodel.Constraint which describes the UNIQUE clause.
// Returns error when the column list is missing or malformed.
func (p *parser) parseTableUnique(constraintName string) (*sqlmodel.Constraint, error) {
	p.skipNullsDistinct()
	columns, columnError := p.parsePostgresColumnList()
	if columnError != nil {
		return nil, columnError
	}
	return &sqlmodel.Constraint{
		Name:           constraintName,
		Kind:           sqlmodel.ConstraintUnique,
		Columns:        columns,
		ForeignTable:   "",
		ForeignColumns: nil,
		Origin:         sqlmodel.MigrationOrigin{},
	}, p.skipIndexParameters()
}

// parseTableCheck parses a CHECK (...) table constraint.
//
// Takes constraintName (string) which optionally names the constraint.
//
// Returns *sqlmodel.Constraint which describes the CHECK clause.
// Returns error when the CHECK expression is missing or unbalanced.
func (p *parser) parseTableCheck(constraintName string) (*sqlmodel.Constraint, error) {
	if err := p.skipCheckBody(); err != nil {
		return nil, err
	}
	return &sqlmodel.Constraint{
		Name:           constraintName,
		Kind:           sqlmodel.ConstraintCheck,
		ForeignTable:   "",
		Columns:        nil,
		ForeignColumns: nil,
		Origin:         sqlmodel.MigrationOrigin{},
	}, nil
}

// parseTableForeignKey parses a FOREIGN KEY table constraint.
//
// Takes constraintName (string) which optionally names the constraint.
//
// Returns *sqlmodel.Constraint which describes the foreign key clause.
// Returns error when the column list or the reference is malformed.
func (p *parser) parseTableForeignKey(constraintName string) (*sqlmodel.Constraint, error) {
	if err := p.requireKeyword(keywordKEY); err != nil {
		return nil, err
	}
	columns, columnError := p.parsePostgresColumnList()
	if columnError != nil {
		return nil, columnError
	}
	if err := p.requireKeyword("REFERENCES"); err != nil {
		return nil, err
	}
	foreignTable, foreignColumns, referenceError := p.parseReferencedTable()
	if referenceError != nil {
		return nil, referenceError
	}
	return &sqlmodel.Constraint{
		Name:           constraintName,
		Kind:           sqlmodel.ConstraintForeignKey,
		Columns:        columns,
		ForeignTable:   foreignTable,
		ForeignColumns: foreignColumns,
		Origin:         sqlmodel.MigrationOrigin{},
	}, nil
}

// parseTableExclude skips an EXCLUDE table constraint without modelling it.
//
// Returns error when the constraint's parentheses are missing or unbalanced.
func (p *parser) parseTableExclude() error {
	if p.MatchKeyword(keywordUSING) {
		if _, err := p.requireIdentifierOrKeyword(); err != nil {
			return err
		}
	}
	if err := p.requireSkipParenthesised(); err != nil {
		return err
	}
	if err := p.skipIndexParameters(); err != nil {
		return err
	}
	if p.MatchKeyword(keywordWHERE) {
		return p.requireSkipParenthesised()
	}
	return nil
}

// parseReferencedTable parses the part of a foreign key after REFERENCES: the table, an
// optional column list, and the MATCH and ON DELETE / ON UPDATE clauses.
//
// Returns string which is the referenced table name.
// Returns []string which lists the referenced columns, or nil when absent.
// Returns error when the table name, column list or an action is malformed.
func (p *parser) parseReferencedTable() (string, []string, error) {
	_, tableName, nameError := p.requireSchemaQualifiedName()
	if nameError != nil {
		return "", nil, nameError
	}
	var columns []string
	if p.Current().Kind == tokenLeftParen {
		parsed, columnError := p.parsePostgresColumnList()
		if columnError != nil {
			return "", nil, columnError
		}
		columns = parsed
	}
	for {
		switch {
		case p.MatchKeyword("MATCH"):
			if _, err := p.requireIdentifierOrKeyword(); err != nil {
				return "", nil, err
			}
		case p.IsKeyword(keywordON) && p.Peek().IsKeyword(tokenIdentifier, "DELETE"),
			p.IsKeyword(keywordON) && p.Peek().IsKeyword(tokenIdentifier, "UPDATE"):
			p.Advance()
			p.Advance()
			if err := p.parseReferentialAction(); err != nil {
				return "", nil, err
			}
		default:
			return tableName, columns, nil
		}
	}
}

// parseReferentialAction parses the action of an ON DELETE or ON UPDATE clause: CASCADE,
// RESTRICT, NO ACTION, or SET NULL or SET DEFAULT with an optional column list.
//
// Returns error when the action is missing or malformed.
func (p *parser) parseReferentialAction() error {
	switch {
	case p.MatchKeyword(keywordCASCADE), p.MatchKeyword(keywordRESTRICT):
		return nil
	case p.MatchKeyword("NO"):
		return p.requireKeyword("ACTION")
	case p.MatchKeyword(keywordSET):
		if err := p.requireKeyword(keywordNULL, keywordDEFAULT); err != nil {
			return err
		}
		if p.Current().Kind == tokenLeftParen {
			return p.requireSkipParenthesised()
		}
		return nil
	default:
		return p.syntaxError("expected a referential action")
	}
}

// parsePostgresColumnList parses a parenthesised column list, allowing trailing
// attributes such as WITHOUT OVERLAPS. It rejects ASC or DESC unless the dialect supports
// ordered inline index elements.
//
// Returns []string which lists the column names.
// Returns error when the opening parenthesis is missing, an entry is ordered where the
// dialect takes no ordering, or the list is malformed.
func (p *parser) parsePostgresColumnList() ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, p.syntaxError("expected '(' to open the column list")
	}
	p.Advance()

	var columns []string
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		name, err := p.requireIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		columns = append(columns, name)

		for !p.AtEnd() && p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen {
			if !p.indexOptions && p.IsKeyword("ASC", "DESC") {
				return nil, p.syntaxError("a key's column takes no sort order; an index takes one")
			}
			p.Advance()
		}

		if p.Current().Kind == tokenComma {
			p.Advance()
		}
	}

	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxError("expected ')' to close the column list")
	}
	p.Advance()

	return columns, nil
}

// skipPostgresDefaultValue consumes the tokens of a DEFAULT expression.
//
// Returns error when a parenthesised default is unbalanced.
func (p *parser) skipPostgresDefaultValue() error {
	depth := 0
	for !p.AtEnd() {
		kind := p.Current().Kind
		switch {
		case kind == tokenLeftParen:
			depth++
		case kind == tokenRightParen:
			if depth == 0 {
				return nil
			}
			depth--
		case depth == 0 && (kind == tokenComma || p.isPostgresColumnConstraintKeyword()):
			return nil
		}
		p.Advance()
	}
	if depth > 0 {
		return p.syntaxError("unmatched parenthesis")
	}
	return nil
}

// newCreateTableMutation builds the MutationCreateTable of a table whose element list
// declared body, with the changes its key makes to columns the list does not declare as
// its additional mutations, and a hash-sharded key recorded under
// pgext.EngineSpecificHashShardBuckets.
//
// Takes schema (string) which is the table's schema.
// Takes tableName (string) which is the table's name.
// Takes body (*createTableBody) which is what the element list declared.
//
// Returns *ast.CatalogueMutation which creates the table.
func newCreateTableMutation(schema, tableName string, body *createTableBody) *ast.CatalogueMutation {
	mutation := ast.NewCatalogueMutation(
		ast.MutationCreateTable,
		schema,
		tableName,
		ast.WithColumns(body.columns),
		ast.WithPrimaryKey(body.primaryKey),
		ast.WithConstraints(body.constraints),
		ast.WithCopiedTables(body.copiedTables),
	)
	mutation.AdditionalMutations = keyColumnsNotNull(schema, tableName, body.keyColumnsElsewhere)
	if body.sharding.hashed {
		mutation.EngineSpecific = map[string]string{pgext.EngineSpecificHashShardBuckets: body.sharding.buckets}
	}
	return mutation
}

// recordStorageParameters records the storage parameters a CREATE TABLE's WITH (...)
// clauses give on its mutation, each under pgext.EngineSpecificStorageParameterPrefix and
// its lower-cased name, a later one replacing an earlier one of the same name.
//
// Takes mutation (*ast.CatalogueMutation) which is the CREATE TABLE mutation.
// Takes parameters ([]reloption) which are the storage parameters in order.
func recordStorageParameters(mutation *ast.CatalogueMutation, parameters []reloption) {
	if len(parameters) == 0 {
		return
	}
	if mutation.EngineSpecific == nil {
		mutation.EngineSpecific = make(map[string]string, len(parameters))
	}
	for _, parameter := range parameters {
		mutation.EngineSpecific[pgext.EngineSpecificStorageParameterPrefix+strings.ToLower(parameter.key)] = parameter.value
	}
}

// keyColumnsNotNull returns the mutations that make primary-key columns the table does
// not declare itself NOT NULL, as a primary key does to every column it names, such as a
// column the table inherits or copies.
//
// Takes schema (string) which is the table's schema.
// Takes tableName (string) which is the table's name.
// Takes columns ([]string) which are the key columns.
//
// Returns []*ast.CatalogueMutation which holds one SET NOT NULL per column.
func keyColumnsNotNull(schema, tableName string, columns []string) []*ast.CatalogueMutation {
	if len(columns) == 0 {
		return nil
	}
	mutations := make([]*ast.CatalogueMutation, 0, len(columns))
	for _, column := range columns {
		mutations = append(mutations, ast.NewCatalogueMutation(
			ast.MutationAlterTableAlterColumn,
			schema,
			tableName,
			ast.WithColumnName(column),
			ast.WithColumnNullability(false),
		))
	}
	return mutations
}

// markKeyColumnsNotNull makes every declared column a table-level primary key names NOT
// NULL, as PostgreSQL does.
//
// Takes columns ([]sqlmodel.Column) which are the declared columns, changed in place.
// Takes primaryKey ([]string) which names the key's columns.
//
// Returns []string which lists the key columns not among the declared columns.
func markKeyColumnsNotNull(columns []sqlmodel.Column, primaryKey []string) []string {
	var elsewhere []string
	for _, name := range primaryKey {
		index := slices.IndexFunc(columns, func(column sqlmodel.Column) bool { return column.Name == name })
		if index < 0 {
			elsewhere = append(elsewhere, name)
			continue
		}
		columns[index].Nullable = false
	}
	return elsewhere
}

// isSerialTypeName reports whether candidate names one of PostgreSQL's serial
// pseudo-types, which give a column a sequence default and NOT NULL.
//
// Takes candidate (token) which is the first token of a column type.
//
// Returns bool which is true for serial, smallserial, bigserial and their numbered
// spellings.
func isSerialTypeName(candidate token) bool {
	switch candidate.Keyword(tokenIdentifier) {
	case "SERIAL", "SERIAL2", "SERIAL4", "SERIAL8", "SMALLSERIAL", "BIGSERIAL":
		return true
	default:
		return false
	}
}

// newNullableColumn builds a nullable column of the given type, marked as an array when
// the parsed type carried array dimensions.
//
// Takes name (string) which is the column's name.
// Takes sqlType (sqlmodel.SQLType) which is the column's parsed type.
// Takes arrayDimensions (int) which is the number of array dimensions, zero for a scalar.
//
// Returns sqlmodel.Column which is the nullable column.
func newNullableColumn(name string, sqlType sqlmodel.SQLType, arrayDimensions int) sqlmodel.Column {
	column := sqlmodel.NewColumn(name, sqlType, true)
	column.IsArray = arrayDimensions > 0
	column.ArrayDimensions = arrayDimensions
	return column
}

// appendConstraintPrimaryKey selects the candidate list when it is non-empty.
//
// Takes existing ([]string) which is the current primary-key column list.
// Takes candidate ([]string) which is the newly parsed constraint columns.
//
// Returns []string which is the candidate list when non-empty, else existing.
func appendConstraintPrimaryKey(existing, candidate []string) []string {
	if len(candidate) > 0 {
		return candidate
	}
	return existing
}

// appendConstraint appends a non-nil constraint to the slice.
//
// Takes constraints ([]sqlmodel.Constraint) which is the accumulator.
// Takes constraint (*sqlmodel.Constraint) which is the optional new entry.
//
// Returns []sqlmodel.Constraint which is the input with the entry appended when non-nil,
// else unchanged.
func appendConstraint(constraints []sqlmodel.Constraint, constraint *sqlmodel.Constraint) []sqlmodel.Constraint {
	if constraint != nil {
		return append(constraints, *constraint)
	}
	return constraints
}

// isMultiWordTypePrefix reports whether the keyword starts a multi-word type.
//
// Takes lower (string) which is the lower-cased candidate token.
//
// Returns bool which is true for double, character, bit, timestamp, and time.
func isMultiWordTypePrefix(lower string) bool {
	switch lower {
	case "double", "character", "bit", "timestamp", "time":
		return true
	}
	return false
}

// readTypeModifiers reads the numbers of a type's modifier list from the tokens inside
// its parentheses.
//
// Values are read through dialectkit.NumberRules.TypeModifierMagnitude, which accepts
// decimal and base-prefixed integers, fractions and exponents. Values are truncated to
// integers and capped at dialectkit.MaxTypeModifier. A preceding minus sign makes the
// value negative, as in numeric(5, -2). Only the first maxTypeModifiers values are kept.
//
// Takes tokens ([]token) which are the tokens inside the list's parentheses, optionally
// followed by its closing parenthesis.
//
// Returns []int which holds the modifiers in order, or nil when the list holds none.
func readTypeModifiers(tokens []token) []int {
	var modifiers []int
	for index := 0; index < len(tokens) && len(modifiers) < maxTypeModifiers; index++ {
		if tokens[index].Kind != tokenNumber {
			continue
		}
		magnitude, isModifier := lexicalRules.Numbers.TypeModifierMagnitude(tokens[index].Value)
		if !isModifier {
			continue
		}
		if index > 0 && tokens[index-1].Kind == tokenOperator && tokens[index-1].Value == "-" {
			magnitude = -magnitude
		}
		modifiers = append(modifiers, magnitude)
	}
	return modifiers
}
