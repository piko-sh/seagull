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

package mysql

import (
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// tableOptionKeywords lists the CREATE TABLE options that may be written without an '='
	// between the option and its value.
	tableOptionKeywords = map[string]struct{}{
		keywordENGINE: {}, keywordAUTO: {}, keywordCOMMENT: {}, "ROW_FORMAT": {}, "COMPRESSION": {},
		"KEY_BLOCK_SIZE": {}, "MAX_ROWS": {}, "MIN_ROWS": {}, "PACK_KEYS": {}, "STATS_PERSISTENT": {},
		"STATS_AUTO_RECALC": {}, "STATS_SAMPLE_PAGES": {}, "AVG_ROW_LENGTH": {}, "CHECKSUM": {},
		"CONNECTION": {}, "DELAY_KEY_WRITE": {}, "ENCRYPTION": {}, "INSERT_METHOD": {}, "PASSWORD": {},
		"AUTOEXTEND_SIZE": {}, "ENGINE_ATTRIBUTE": {}, "SECONDARY_ENGINE_ATTRIBUTE": {}, "UNION": {},
		"SECONDARY_ENGINE": {}, "PAGE_CHECKSUM": {}, "PAGE_COMPRESSED": {}, "PAGE_COMPRESSION_LEVEL": {},
		"TRANSACTIONAL": {}, "ENCRYPTED": {}, "ENCRYPTION_KEY_ID": {}, "IETF_QUOTES": {}, "SEQUENCE": {},
	}
)

// parseCreateTable parses a CREATE [TEMPORARY] TABLE statement.
//
// Takes engine (*Dialect) which supplies type normalisation for column definitions.
//
// Returns *ast.CatalogueMutation which describes the new table's columns, primary key,
// and constraints.
// Returns error when the statement is malformed.
func (p *parser) parseCreateTable(engine *Dialect) (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	replace := p.matchOrReplace()

	p.MatchKeyword("TEMPORARY")
	if _, err := p.ExpectKeyword(keywordTABLE); err != nil {
		return nil, err
	}

	ifNotExists := p.matchIfNotExists()

	schema, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
	}
	if replace {
		created, createError := p.parseCreateTableRest(engine, schema, tableName, ifNotExists)
		if created == nil {
			return nil, createError
		}
		return replacing(ast.NewCatalogueMutation(ast.MutationDropTable, schema, tableName), created), createError
	}
	return p.parseCreateTableRest(engine, schema, tableName, ifNotExists)
}

// parseCreateTableRest parses what follows the table's name in a CREATE TABLE: AS and a
// query, LIKE and the table copied, or the column list and the table options.
//
// Takes engine (*Dialect) which supplies type normalisation for column definitions.
// Takes schema (string) which is the table's database, or empty.
// Takes tableName (string) which is the table's name.
// Takes ifNotExists (bool) which is true when the statement said IF NOT EXISTS.
//
// Returns *ast.CatalogueMutation which describes the new table.
// Returns error when the rest of the statement is malformed.
func (p *parser) parseCreateTableRest(
	engine *Dialect,
	schema, tableName string,
	ifNotExists bool,
) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword(keywordAS) {
		mutation := ast.NewCatalogueMutation(ast.MutationCreateTable, schema, tableName, ast.WithIfNotExists(ifNotExists))
		viewDefinition, unanalysed, err := p.analyseViewBody(nil, "table "+tableName)
		if err != nil {
			return nil, err
		}
		mutation.ViewDefinition = viewDefinition
		mutation.UnanalysedParts = unanalysed
		return mutation, nil
	}

	if copied, isLike, likeError := p.parseCreateTableLike(); isLike || likeError != nil {
		return ast.NewCatalogueMutation(
			ast.MutationCreateTable,
			schema,
			tableName,
			ast.WithCopiedTables([]ast.CopiedTable{copied}),
			ast.WithIfNotExists(ifNotExists),
		), likeError
	}

	mutation, err := p.parseCreateTableDefinition(engine, schema, tableName)
	if mutation != nil {
		mutation.IfNotExists = ifNotExists
	}
	return mutation, err
}

// parseCreateTableDefinition parses the parenthesised column and constraint list of a
// CREATE TABLE, the table options after it, and the partitioning and query that may
// follow them.
//
// Takes engine (*Dialect) which supplies type normalisation for column definitions.
// Takes schema (string) which is the table's database, or empty.
// Takes tableName (string) which is the table's name.
//
// Returns *ast.CatalogueMutation which describes the new table.
// Returns error when the list or an option is malformed.
func (p *parser) parseCreateTableDefinition(engine *Dialect, schema, tableName string) (*ast.CatalogueMutation, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, p.syntaxErrorAt("expected '(' after the table name " + tableName)
	}
	p.Advance()

	columns, primaryKeyColumns, constraints, bodyError := p.parseCreateTableBody(engine)
	if bodyError != nil {
		return nil, bodyError
	}

	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxErrorAt("expected ')' to close the column list of " + tableName)
	}
	p.Advance()

	comment, err := p.parseTableOptions()
	if err != nil {
		return nil, err
	}
	if err := p.parseCreateTableTail(); err != nil {
		return nil, err
	}

	mutation := ast.NewCatalogueMutation(
		ast.MutationCreateTable,
		schema,
		tableName,
		ast.WithColumns(columns),
		ast.WithPrimaryKey(primaryKeyColumns),
		ast.WithConstraints(constraints),
	)
	mutation.Comment = comment
	return mutation, nil
}

// parseCreateTableLike parses the LIKE form of CREATE TABLE, written LIKE old or (LIKE
// old), which copies the old table's columns, primary key and indexes.
//
// Returns ast.CopiedTable which names the table copied.
// Returns bool which is true when the statement takes the LIKE form.
// Returns error when the copied table's name is malformed or the parenthesis is not
// closed.
func (p *parser) parseCreateTableLike() (ast.CopiedTable, bool, error) {
	parenthesised := p.Current().Kind == tokenLeftParen && p.Peek().IsKeyword(tokenIdentifier, "LIKE")
	if !parenthesised && !p.IsKeyword("LIKE") {
		return ast.CopiedTable{}, false, nil
	}
	if parenthesised {
		p.Advance()
	}
	p.Advance()
	schema, name, err := p.parseSchemaQualifiedName()
	if err != nil {
		return ast.CopiedTable{}, true, err
	}
	if parenthesised {
		if p.Current().Kind != tokenRightParen {
			return ast.CopiedTable{}, true, p.syntaxErrorAt("expected ')' to close the LIKE clause")
		}
		p.Advance()
	}
	return ast.CopiedTable{Table: ast.TableReference{Schema: schema, Name: name, Alias: ""}, Position: 0, Keys: true}, true, nil
}

// parseCreateTableBody parses the column and constraint list inside CREATE TABLE.
//
// Takes engine (*Dialect) which supplies type normalisation for column definitions.
//
// Returns []sqlmodel.Column which is the parsed column list.
// Returns []string which is the primary key column list.
// Returns []sqlmodel.Constraint which is the parsed table constraint set.
// Returns error when a column or constraint clause is malformed.
func (p *parser) parseCreateTableBody(
	engine *Dialect,
) ([]sqlmodel.Column, []string, []sqlmodel.Constraint, error) {
	var columns []sqlmodel.Column
	var primaryKeyColumns []string
	var tableConstraintPrimaryKey []string
	var constraints []sqlmodel.Constraint

	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.isMySQLTableConstraint() {
			constraintPrimaryKey, constraint, constraintError := p.parseMySQLTableConstraint()
			if constraintError != nil {
				return nil, nil, nil, constraintError
			}
			tableConstraintPrimaryKey = appendConstraintPrimaryKey(tableConstraintPrimaryKey, constraintPrimaryKey)
			constraints = appendConstraint(constraints, constraint)
		} else if err := p.appendColumnDefinition(engine, &columns, &primaryKeyColumns); err != nil {
			return nil, nil, nil, err
		}
		if err := p.endTableElement(); err != nil {
			return nil, nil, nil, err
		}
	}

	if len(tableConstraintPrimaryKey) > 0 {
		primaryKeyColumns = tableConstraintPrimaryKey
		markKeyColumnsNotNull(columns, tableConstraintPrimaryKey)
	}

	return columns, primaryKeyColumns, constraints, nil
}

// appendColumnDefinition parses one column definition and appends it to columns, and its
// name to primaryKeyColumns when it carries an inline PRIMARY KEY.
//
// Takes engine (*Dialect) which supplies type normalisation.
// Takes columns (*[]sqlmodel.Column) which receives the column.
// Takes primaryKeyColumns (*[]string) which receives an inline primary key column.
//
// Returns error when the column definition is malformed.
func (p *parser) appendColumnDefinition(engine *Dialect, columns *[]sqlmodel.Column, primaryKeyColumns *[]string) error {
	column, columnPrimaryKey, columnError := p.parseMySQLColumnDefinition(engine)
	if columnError != nil {
		return columnError
	}
	*columns = append(*columns, column)
	if columnPrimaryKey {
		*primaryKeyColumns = append(*primaryKeyColumns, column.Name)
	}
	return nil
}

// endTableElement requires a ',' or ')' after a column or table element, consuming the
// comma, so a clause the parser does not know fails the statement instead of becoming the
// name of a phantom column.
//
// Returns error which names the unexpected token.
func (p *parser) endTableElement() error {
	switch p.Current().Kind {
	case tokenComma:
		p.Advance()
		return nil
	case tokenRightParen:
		return nil
	default:
	}
	return p.syntaxErrorAt("expected ',' or ')' after a column or constraint definition")
}

// isMySQLTableConstraint reports whether the current token starts a table-level
// constraint or index rather than a column definition. SPATIAL and MariaDB's VECTOR start
// an index only before KEY, INDEX or the key parts, since either may also name a column.
//
// Returns bool reporting whether a table-level constraint clause begins here.
func (p *parser) isMySQLTableConstraint() bool {
	switch {
	case p.IsKeyword(keywordCONSTRAINT, keywordUNIQUE, keywordKEY, keywordINDEX, keywordCHECK, keywordFOREIGN, "FULLTEXT"):
		return true
	case p.IsKeyword(keywordPRIMARY):
		return p.Peek().IsKeyword(tokenIdentifier, keywordKEY)
	case p.IsKeyword("SPATIAL", "VECTOR"):
		next := p.Peek()
		return next.IsKeyword(tokenIdentifier, keywordKEY) || next.IsKeyword(tokenIdentifier, keywordINDEX) || next.Kind == tokenLeftParen
	default:
		return false
	}
}

// parseMySQLTableConstraint parses one table-level constraint clause.
//
// Returns []string which is the primary key column list when the clause is a PRIMARY KEY
// constraint.
// Returns *sqlmodel.Constraint which describes a non-primary-key constraint, or nil when
// none applies.
// Returns error when the constraint clause is malformed.
func (p *parser) parseMySQLTableConstraint() ([]string, *sqlmodel.Constraint, error) {
	constraintName := p.parseOptionalConstraintName()

	switch {
	case p.MatchKeyword(keywordPRIMARY):
		return p.parseTablePrimaryKey()
	case p.MatchKeyword(keywordUNIQUE):
		p.MatchKeyword(keywordKEY)
		p.MatchKeyword(keywordINDEX)
		return p.parseTableIndexOrUnique(constraintName, true)
	case p.MatchKeyword(keywordKEY), p.MatchKeyword(keywordINDEX):
		return p.parseTableIndexOrUnique(constraintName, false)
	case p.MatchKeyword("FULLTEXT"), p.MatchKeyword("SPATIAL"), p.MatchKeyword("VECTOR"):
		p.MatchKeyword(keywordKEY)
		p.MatchKeyword(keywordINDEX)
		return p.parseTableIndexOrUnique(constraintName, false)
	case p.MatchKeyword(keywordCHECK):
		return p.parseTableCheck(constraintName)
	case p.MatchKeyword(keywordFOREIGN):
		return p.parseTableForeignKey(constraintName)
	}

	return nil, nil, p.syntaxErrorAt("expected PRIMARY KEY, UNIQUE, KEY, INDEX, CHECK or FOREIGN KEY")
}

// parseOptionalConstraintName consumes an optional CONSTRAINT name prefix.
//
// Returns string which is the constraint name or the empty string when none was present.
func (p *parser) parseOptionalConstraintName() string {
	if !p.MatchKeyword(keywordCONSTRAINT) {
		return ""
	}
	if p.IsKeyword(keywordPRIMARY, keywordUNIQUE, keywordKEY, keywordINDEX, keywordCHECK, keywordFOREIGN) {
		return ""
	}
	name, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return ""
	}
	return name
}

// parseTablePrimaryKey parses a PRIMARY KEY [USING type] (...) [index options] table
// constraint.
//
// Returns []string which is the column list forming the primary key.
// Returns *sqlmodel.Constraint which is always nil since the primary key is reported
// separately.
// Returns error when the column list is missing or malformed.
func (p *parser) parseTablePrimaryKey() ([]string, *sqlmodel.Constraint, error) {
	p.MatchKeyword(keywordKEY)
	p.skipIndexOptions()
	if p.Current().Kind != tokenLeftParen {
		return nil, nil, p.syntaxErrorAt("expected '(' after PRIMARY KEY")
	}
	columns, columnError := p.parseMySQLColumnList()
	if columnError != nil {
		return nil, nil, columnError
	}
	p.skipIndexOptions()
	return columns, nil, nil
}

// parseTableIndexOrUnique parses a UNIQUE, INDEX, KEY, FULLTEXT, SPATIAL or VECTOR table
// clause: an optional index name, an optional index type, the key parts and the index
// options.
//
// Takes constraintName (string) which is the optional constraint name.
// Takes isUnique (bool) which selects UNIQUE versus plain index parsing.
//
// Returns []string which is always nil since these clauses do not affect the primary key.
// Returns *sqlmodel.Constraint which describes the unique constraint, or nil for a plain
// index clause.
// Returns error when the key part list is missing or malformed.
func (p *parser) parseTableIndexOrUnique(constraintName string, isUnique bool) ([]string, *sqlmodel.Constraint, error) {
	if p.Current().Kind == tokenIdentifier && !p.IsKeyword(keywordUSING) {
		p.Advance()
	}
	p.skipIndexOptions()
	if p.Current().Kind != tokenLeftParen {
		return nil, nil, p.syntaxErrorAt("expected '(' to open the index key parts")
	}
	columns, columnError := p.parseMySQLColumnList()
	if columnError != nil {
		return nil, nil, columnError
	}
	p.skipIndexOptions()
	if !isUnique {
		return nil, nil, nil
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

// parseTableCheck parses a CHECK (...) [[NOT] ENFORCED] table constraint clause.
//
// Takes constraintName (string) which is the optional constraint name.
//
// Returns []string which is always nil since CHECK does not contribute to the primary
// key.
// Returns *sqlmodel.Constraint which describes the CHECK constraint.
// Returns error when the condition is missing or malformed.
func (p *parser) parseTableCheck(constraintName string) ([]string, *sqlmodel.Constraint, error) {
	if err := p.parseCheckBody(); err != nil {
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

// parseTableForeignKey parses a FOREIGN KEY (...) REFERENCES clause.
//
// Takes constraintName (string) which is the optional constraint name.
//
// Returns []string which is always nil since foreign keys do not affect the primary key.
// Returns *sqlmodel.Constraint which describes the foreign key.
// Returns error when the clause is malformed.
func (p *parser) parseTableForeignKey(constraintName string) ([]string, *sqlmodel.Constraint, error) {
	p.MatchKeyword(keywordKEY)

	if p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenLeftParen {
		p.Advance()
	}

	var columns []string
	if p.Current().Kind == tokenLeftParen {
		parsed, columnError := p.parseMySQLColumnList()
		if columnError != nil {
			return nil, nil, columnError
		}
		columns = parsed
	}

	foreignTable, foreignColumns, referenceError := p.parseMySQLForeignKeyReference()
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

// parseMySQLForeignKeyReference parses REFERENCES table(columns) ON ... actions.
//
// Returns string which is the referenced table name.
// Returns []string which is the referenced column list, or nil when none was specified.
// Returns error when REFERENCES, the table's name or the column list is missing or
// malformed.
func (p *parser) parseMySQLForeignKeyReference() (string, []string, error) {
	if _, err := p.ExpectKeyword("REFERENCES"); err != nil {
		return "", nil, err
	}
	_, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return "", nil, nameError
	}
	var columns []string
	if p.Current().Kind == tokenLeftParen {
		parsed, columnError := p.parseMySQLColumnList()
		if columnError != nil {
			return "", nil, columnError
		}
		columns = parsed
	}
	p.skipForeignKeyActions()
	return tableName, columns, nil
}

// parseMySQLColumnList parses a parenthesised list of key parts: column names with an
// optional prefix length and ASC or DESC, or parenthesised functional key parts, which
// name no column.
//
// Returns []string which is the parsed column name list.
// Returns error when the list is malformed or not closed.
func (p *parser) parseMySQLColumnList() ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, p.syntaxErrorAt("expected '('")
	}
	p.Advance()

	var columns []string
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if err := p.parseKeyPart(&columns); err != nil {
			return nil, err
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxErrorAt("expected ',' or ')' in the column list")
	}
	p.Advance()

	return columns, nil
}

// parseKeyPart parses one key part of a column list and appends its column name.
//
// Takes columns (*[]string) which receives the key part's column name, unless it is a
// functional key part.
//
// Returns error when the key part is malformed.
func (p *parser) parseKeyPart(columns *[]string) error {
	if p.Current().Kind == tokenLeftParen {
		if err := p.skipParenthesised(); err != nil {
			return err
		}
	} else {
		name, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			return nameError
		}
		*columns = append(*columns, name)
		if err := p.skipParenthesisedIfPresent(); err != nil {
			return err
		}
	}
	p.MatchKeyword(keywordASC)
	p.MatchKeyword(keywordDESC)
	return nil
}

// parseDropTable parses a DROP [TEMPORARY] TABLE statement, which may name several
// tables, and MariaDB's WAIT n or NOWAIT and the RESTRICT or CASCADE after them.
//
// Returns *ast.CatalogueMutation which drops the first table, carrying a drop of each
// other table as an additional mutation, each marked IF EXISTS when the statement says
// so.
// Returns error when the statement is malformed.
func (p *parser) parseDropTable() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordDROP); err != nil {
		return nil, err
	}

	p.MatchKeyword("TEMPORARY")
	if _, err := p.ExpectKeyword(keywordTABLE); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	drops, err := p.parseDroppedNames(ast.MutationDropTable, ifExists)
	if err != nil {
		return nil, err
	}

	if p.MatchKeyword("WAIT") {
		if err := p.expectOptionValue(); err != nil {
			return nil, err
		}
	} else {
		p.MatchKeyword("NOWAIT")
	}
	if !p.MatchKeyword(keywordRESTRICT) {
		p.MatchKeyword(keywordCASCADE)
	}

	return collectAlterTableActions(drops), nil
}

// parseAlterTable parses an ALTER TABLE statement and every action in its action list.
// The first schema-affecting action's mutation carries the others, in order, as its
// additional mutations, so ALTER TABLE t ADD COLUMN a int, DROP COLUMN b applies both.
//
// Takes engine (*Dialect) which supplies type normalisation for any column definitions in
// the action list.
//
// Returns *ast.CatalogueMutation which describes the mutations, or nil when no
// schema-affecting action is found.
// Returns error when the statement is malformed.
func (p *parser) parseAlterTable(engine *Dialect) (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence("ALTER", keywordTABLE); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schema, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
	}

	var actions []*ast.CatalogueMutation
	for !p.AtEnd() && p.Current().Kind != tokenSemicolon {
		result, actionError := p.parseAlterTableAction(engine, schema, tableName)
		if actionError != nil {
			return nil, actionError
		}
		if result != nil {
			actions = append(actions, result)
			actions = append(actions, result.AdditionalMutations...)
			result.AdditionalMutations = nil
		}

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	for _, action := range actions {
		action.IfExists = ifExists
	}
	return collectAlterTableActions(actions), nil
}

// parseAlterTableAction dispatches one ALTER TABLE action keyword. An action the
// catalogue does not record, such as ALGORITHM = INPLACE or ADD PARTITION, is passed over
// up to the next comma; any other action must be read whole.
//
// Takes engine (*Dialect) which supplies type normalisation for nested column
// definitions.
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the action, or nil when the action does
// not affect the schema.
// Returns error when the action clause is malformed.
func (p *parser) parseAlterTableAction(
	engine *Dialect, schema, tableName string,
) (*ast.CatalogueMutation, error) {
	if p.IsKeyword("ADD", keywordDROP) && p.unrecordedAlterTableTarget() {
		return nil, p.skipAlterTableMiscAction()
	}
	if p.MatchKeyword("ADD") {
		return p.parseAlterTableAdd(engine, schema, tableName)
	}
	if p.MatchKeyword(keywordDROP) {
		return p.parseAlterTableDrop(schema, tableName)
	}
	if p.MatchKeyword("MODIFY") {
		return p.parseAlterTableModify(engine, schema, tableName)
	}
	if p.MatchKeyword("CHANGE") {
		return p.parseAlterTableChange(engine, schema, tableName)
	}
	if p.MatchKeyword("RENAME") {
		return p.parseAlterTableRename(schema, tableName)
	}

	return nil, p.skipAlterTableMiscAction()
}

// parseAlterTableAdd parses an ADD COLUMN or ADD CONSTRAINT action, including the
// parenthesised column list of ADD [COLUMN] (column, ...).
//
// Takes engine (*Dialect) which supplies type normalisation for the new column
// definition.
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the addition.
// Returns error when the action clause is malformed.
func (p *parser) parseAlterTableAdd(
	engine *Dialect, schema, tableName string,
) (*ast.CatalogueMutation, error) {
	if p.isMySQLTableConstraint() {
		primaryKey, constraint, constraintError := p.parseMySQLTableConstraint()
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
	if p.Current().Kind == tokenLeftParen {
		return p.parseAlterTableAddColumns(engine, schema, tableName, ifNotExists)
	}

	column, _, columnError := p.parseMySQLColumnDefinition(engine)
	if columnError != nil {
		return nil, columnError
	}

	placement, placementError := p.parsePlacement()
	if placementError != nil {
		return nil, placementError
	}

	return ast.NewCatalogueMutation(
		ast.MutationAlterTableAddColumn,
		schema,
		tableName,
		ast.WithColumns([]sqlmodel.Column{column}),
		ast.WithIfNotExists(ifNotExists),
		ast.WithPlacement(placement.Kind, placement.Anchor),
	), nil
}

// parseAlterTableAddColumns parses the parenthesised column list of ADD [COLUMN] (column,
// ...), which adds each column in turn at the end of the table.
//
// Takes engine (*Dialect) which supplies type normalisation for the new columns.
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
// Takes ifNotExists (bool) which is true when the action says IF NOT EXISTS.
//
// Returns *ast.CatalogueMutation which adds the first column and carries the others, in
// order, as its additional mutations.
// Returns error when a column definition is malformed or the list is not closed.
func (p *parser) parseAlterTableAddColumns(
	engine *Dialect, schema, tableName string, ifNotExists bool,
) (*ast.CatalogueMutation, error) {
	p.Advance()
	var added []*ast.CatalogueMutation
	for {
		column, _, columnError := p.parseMySQLColumnDefinition(engine)
		if columnError != nil {
			return nil, columnError
		}
		added = append(added, ast.NewCatalogueMutation(
			ast.MutationAlterTableAddColumn,
			schema,
			tableName,
			ast.WithColumns([]sqlmodel.Column{column}),
			ast.WithIfNotExists(ifNotExists),
			ast.WithPlacement(ast.PlacementDefault, ""),
		))
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxErrorAt("expected ',' or ')' after a column definition")
	}
	p.Advance()
	return collectAlterTableActions(added), nil
}

// parseAlterTableDrop parses a DROP COLUMN/CONSTRAINT/INDEX/etc. action.
//
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the drop, or nil when the dropped item
// does not affect the catalogue.
// Returns error when the action clause is malformed.
func (p *parser) parseAlterTableDrop(schema, tableName string) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword(keywordPRIMARY) {
		p.MatchKeyword(keywordKEY)
		return nil, nil
	}
	if p.MatchKeyword(keywordINDEX) || p.MatchKeyword(keywordKEY) {
		p.matchIfExists()
		_, err := p.parseIdentifierOrKeyword()
		return nil, err
	}
	if p.MatchKeyword(keywordFOREIGN) {
		if _, err := p.ExpectKeyword(keywordKEY); err != nil {
			return nil, err
		}
		p.matchIfExists()
		_, err := p.parseIdentifierOrKeyword()
		return nil, err
	}
	if p.MatchKeyword(keywordCONSTRAINT) || p.MatchKeyword(keywordCHECK) {
		p.matchIfExists()
		constraintName, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			return nil, nameError
		}
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

	mutation := ast.NewCatalogueMutation(
		ast.MutationAlterTableDropColumn,
		schema,
		tableName,
		ast.WithColumnName(columnName),
	)
	mutation.IfColumnExists = ifColumnExists
	return mutation, nil
}

// parseAlterTableModify parses a MODIFY COLUMN action.
//
// Takes engine (*Dialect) which supplies type normalisation for the modified column.
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the column change.
// Returns error when the column definition is malformed.
func (p *parser) parseAlterTableModify(
	engine *Dialect, schema, tableName string,
) (*ast.CatalogueMutation, error) {
	p.MatchKeyword(keywordCOLUMN)
	ifColumnExists := p.matchIfExists()

	column, _, columnError := p.parseMySQLColumnDefinition(engine)
	if columnError != nil {
		return nil, columnError
	}

	placement, placementError := p.parsePlacement()
	if placementError != nil {
		return nil, placementError
	}

	mutation := ast.NewCatalogueMutation(
		ast.MutationAlterTableAlterColumn,
		schema,
		tableName,
		ast.WithColumnName(column.Name),
		ast.WithColumns([]sqlmodel.Column{column}),
		ast.WithPlacement(placement.Kind, placement.Anchor),
	)
	mutation.IfColumnExists = ifColumnExists
	return mutation, nil
}

// parseAlterTableChange parses a CHANGE COLUMN action.
//
// Takes engine (*Dialect) which supplies type normalisation for the new column
// definition.
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes an in-place change when the name stays,
// or a rename that carries the change of the renamed column as an additional mutation.
// Returns error when the column definition is malformed.
func (p *parser) parseAlterTableChange(
	engine *Dialect, schema, tableName string,
) (*ast.CatalogueMutation, error) {
	p.MatchKeyword(keywordCOLUMN)
	ifColumnExists := p.matchIfExists()

	oldName, oldError := p.parseIdentifierOrKeyword()
	if oldError != nil {
		return nil, oldError
	}

	column, _, columnError := p.parseMySQLColumnDefinition(engine)
	if columnError != nil {
		return nil, columnError
	}

	placement, placementError := p.parsePlacement()
	if placementError != nil {
		return nil, placementError
	}

	change := ast.NewCatalogueMutation(
		ast.MutationAlterTableAlterColumn,
		schema,
		tableName,
		ast.WithColumnName(column.Name),
		ast.WithColumns([]sqlmodel.Column{column}),
		ast.WithPlacement(placement.Kind, placement.Anchor),
	)
	change.IfColumnExists = ifColumnExists
	if strings.EqualFold(oldName, column.Name) {
		return change, nil
	}

	rename := ast.NewCatalogueMutation(
		ast.MutationAlterTableRenameColumn,
		schema,
		tableName,
		ast.WithColumnName(oldName),
		ast.WithNewName(column.Name),
		ast.WithColumns([]sqlmodel.Column{column}),
	)
	rename.IfColumnExists = ifColumnExists
	rename.AdditionalMutations = []*ast.CatalogueMutation{change}
	return rename, nil
}

// parseAlterTableRename parses a RENAME TABLE or RENAME COLUMN action.
//
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the current target table name.
//
// Returns *ast.CatalogueMutation which describes the rename.
// Returns error when the rename clause is malformed.
func (p *parser) parseAlterTableRename(schema, tableName string) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword(keywordCOLUMN) {
		oldName, oldError := p.parseIdentifierOrKeyword()
		if oldError != nil {
			return nil, oldError
		}
		if _, err := p.ExpectKeyword(keywordTO); err != nil {
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

	if p.MatchKeyword(keywordINDEX) || p.MatchKeyword(keywordKEY) {
		return nil, p.skipRenamedName()
	}

	if !p.MatchKeyword(keywordTO) {
		p.MatchKeyword(keywordAS)
	}
	newSchema, newName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
	}
	return tableRename(schema, tableName, newSchema, newName), nil
}

// skipRenamedName consumes the old name, TO and the new name of a RENAME of something the
// catalogue does not record, such as an index.
//
// Returns error when a name or TO is missing.
func (p *parser) skipRenamedName() error {
	if _, err := p.parseIdentifierOrKeyword(); err != nil {
		return err
	}
	if _, err := p.ExpectKeyword(keywordTO); err != nil {
		return err
	}
	_, err := p.parseIdentifierOrKeyword()
	return err
}

// parseRenameTable parses a RENAME TABLE statement, applying each table or view rename in
// order and changing its database when the new name specifies one.
//
// Returns *ast.CatalogueMutation which renames the first table, carrying the rest as
// additional mutations.
// Returns error when a name or TO is missing.
func (p *parser) parseRenameTable() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence("RENAME", keywordTABLE); err != nil {
		return nil, err
	}
	var renames []*ast.CatalogueMutation
	for {
		schema, tableName, nameError := p.parseSchemaQualifiedName()
		if nameError != nil {
			return nil, nameError
		}
		if _, err := p.ExpectKeyword(keywordTO); err != nil {
			return nil, err
		}
		newSchema, newName, newError := p.parseSchemaQualifiedName()
		if newError != nil {
			return nil, newError
		}
		rename := tableRename(schema, tableName, newSchema, newName)
		renames = append(renames, rename)
		renames = append(renames, rename.AdditionalMutations...)
		rename.AdditionalMutations = nil
		if p.Current().Kind != tokenComma {
			return collectAlterTableActions(renames), nil
		}
		p.Advance()
	}
}

// unrecordedAlterTableTarget reports whether the ADD or DROP at the current token acts on
// something the catalogue does not record: a partition, MariaDB's system versioning, or
// an application-time period.
//
// Returns bool which is true for such an action.
func (p *parser) unrecordedAlterTableTarget() bool {
	target := p.Peek()
	switch {
	case target.IsKeyword(tokenIdentifier, "PARTITION"):
		return true
	case target.IsKeyword(tokenIdentifier, "SYSTEM"):
		return p.PeekAt(2).IsKeyword(tokenIdentifier, "VERSIONING")
	case target.IsKeyword(tokenIdentifier, "PERIOD"):
		return p.PeekAt(2).IsKeyword(tokenIdentifier, keywordFOR)
	default:
		return false
	}
}

// skipAlterTableMiscAction advances past an unrecognised ALTER TABLE action.
//
// Returns error when a parenthesised group in the action is unmatched.
func (p *parser) skipAlterTableMiscAction() error {
	for !p.AtEnd() && p.Current().Kind != tokenComma &&
		p.Current().Kind != tokenSemicolon && p.Current().Kind != tokenEOF {
		if p.Current().Kind == tokenLeftParen {
			if err := p.skipParenthesised(); err != nil {
				return err
			}
			continue
		}
		p.Advance()
	}
	return nil
}

// parseCreateTableTail consumes what may follow a CREATE TABLE's table options: a
// partitioning clause, and a query whose rows fill the table, written after an optional
// IGNORE or REPLACE and AS. The query is not analysed.
//
// Returns error when a partitioning clause is unbalanced, or IGNORE, REPLACE or AS is
// followed by no query.
func (p *parser) parseCreateTableTail() error {
	if p.IsKeyword("PARTITION") && p.Peek().IsKeyword(tokenIdentifier, keywordBY) {
		if err := p.skipPartitioning(); err != nil {
			return err
		}
	}
	introduced := p.MatchKeyword("IGNORE") || p.MatchKeyword(keywordREPLACE)
	introduced = p.MatchKeyword(keywordAS) || introduced
	if p.opensQueryHere() {
		p.Position = len(p.Tokens)
		return nil
	}
	if introduced {
		return p.syntaxErrorAt("expected a query to fill the table")
	}
	return nil
}

// skipPartitioning consumes a CREATE TABLE's PARTITION BY clause with its partition and
// subpartition definitions, up to the end of the statement or the query that fills the
// table.
//
// Returns error when a parenthesised group in the clause is unmatched.
func (p *parser) skipPartitioning() error {
	for !p.AtEnd() && !p.IsKeyword("IGNORE", keywordREPLACE, keywordAS) && !p.opensQueryHere() {
		if p.Current().Kind == tokenLeftParen {
			if err := p.skipParenthesised(); err != nil {
				return err
			}
			continue
		}
		p.Advance()
	}
	return nil
}

// opensQueryHere reports whether a query starts at the current token, written alone or in
// parentheses.
//
// Returns bool which is true at the start of a query.
func (p *parser) opensQueryHere() bool {
	if startsQuery(p.Current()) {
		return true
	}
	return p.Current().Kind == tokenLeftParen && classifyParenthesisedStatement(p.Tokens[p.Position:]) == statementKindSelect
}

// matchIfNotExists consumes an optional IF NOT EXISTS clause.
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

// matchIfExists consumes an optional IF EXISTS clause.
//
// Returns bool which is true when the clause was present.
func (p *parser) matchIfExists() bool {
	if !p.MatchKeyword("IF") {
		return false
	}
	p.MatchKeyword(keywordEXISTS)
	return true
}

// parsePlacement consumes an optional FIRST or AFTER column placement.
//
// Returns ast.Placement which says where the column goes, the default when neither is
// present.
// Returns error when AFTER names no column.
func (p *parser) parsePlacement() (ast.Placement, error) {
	if p.MatchKeyword(keywordFIRST) {
		return ast.Placement{Anchor: "", Kind: ast.PlacementFirst}, nil
	}
	if !p.MatchKeyword(keywordAFTER) {
		return ast.Placement{Anchor: "", Kind: ast.PlacementDefault}, nil
	}
	anchor, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return ast.Placement{}, err
	}
	return ast.Placement{Anchor: anchor, Kind: ast.PlacementAfter}, nil
}

// skipComma consumes the current token when it is a comma.
func (p *parser) skipComma() {
	if p.Current().Kind == tokenComma {
		p.Advance()
	}
}

// parseTableOptions consumes the table options after a CREATE TABLE column list, such as
// ENGINE=InnoDB AUTO_INCREMENT=42 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
// COMMENT='...', optionally separated by commas.
//
// It stops at the first token that opens no option, such as a PARTITION BY clause or the
// SELECT of CREATE TABLE ... SELECT.
//
// Returns string which is the table's COMMENT, or empty when the options give none.
// Returns error when a character set or collation option names nothing.
func (p *parser) parseTableOptions() (string, error) {
	comment := ""
	for {
		p.skipComma()
		p.MatchKeyword(keywordDEFAULT)
		switch {
		case p.isCharacterSetStart(), p.IsKeyword(keywordCOLLATE):
			if err := p.parseCharacterSetOrCollation(); err != nil {
				return "", err
			}
		case p.IsKeyword(keywordCOMMENT) && p.commentOptionValue() != "":
			comment = p.commentOptionValue()
			p.Advance()
			p.skipTableOptionValue()
		case p.IsKeyword("DATA", keywordINDEX) && p.Peek().IsKeyword(tokenIdentifier, "DIRECTORY"):
			p.Advance()
			p.Advance()
			p.skipTableOptionValue()
		case p.IsKeyword(keywordWITH) && p.Peek().IsKeyword(tokenIdentifier, "SYSTEM"):
			p.Advance()
			p.Advance()
			p.MatchKeyword("VERSIONING")
		case p.IsKeyword("START") && p.Peek().IsKeyword(tokenIdentifier, "TRANSACTION"):
			p.Advance()
			p.Advance()
		case p.MatchKeyword("TABLESPACE"):
			p.skipTableOptionValue()
			if p.MatchKeyword("STORAGE") {
				p.skipTableOptionValue()
			}
		case p.isTableOption():
			p.Advance()
			p.skipTableOptionValue()
		default:
			return comment, nil
		}
	}
}

// commentOptionValue returns the string a COMMENT table option at the current token
// gives, written with or without an '=' before it.
//
// Returns string which is the comment, or empty when no string follows.
func (p *parser) commentOptionValue() string {
	value := p.Peek()
	if value.Kind == tokenOperator && value.Value == operatorEquals {
		value = p.PeekAt(2)
	}
	if value.Kind != tokenString {
		return ""
	}
	return value.Value
}

// isTableOption reports whether the current token names a table option: a known option
// keyword, or any bare word followed by '=', so an option the parser does not list is
// still consumed whole.
//
// Returns bool which is true at a table option.
func (p *parser) isTableOption() bool {
	keyword := p.Current().Keyword(tokenIdentifier)
	if keyword == "" {
		return false
	}
	if _, known := tableOptionKeywords[keyword]; known {
		return true
	}
	next := p.Peek()
	return next.Kind == tokenOperator && next.Value == operatorEquals
}

// skipTableOptionValue consumes an option's optional '=' and its value: a word, number,
// string or parenthesised list such as the table list of UNION.
func (p *parser) skipTableOptionValue() {
	p.matchOperator(operatorEquals)
	switch p.Current().Kind {
	case tokenIdentifier, tokenNumber, tokenString:
		p.Advance()
	case tokenLeftParen:
		if err := p.skipParenthesised(); err != nil {
			p.recordSyntaxError(err)
		}
	default:
	}
}

// skipDefaultExpressionTokens advances past a DEFAULT expression body.
func (p *parser) skipDefaultExpressionTokens() {
	depth := 0
	for !p.AtEnd() {
		if p.Current().Kind == tokenLeftParen {
			depth++
			p.Advance()
			continue
		}
		if p.Current().Kind == tokenRightParen {
			if depth == 0 {
				return
			}
			depth--
			p.Advance()
			continue
		}
		if depth == 0 && p.isDefaultExpressionTerminator() {
			return
		}
		p.Advance()
	}
}

// isDefaultExpressionTerminator reports whether the current token ends a DEFAULT
// expression on a column.
//
// Returns bool reporting whether the cursor sits on a terminator token.
func (p *parser) isDefaultExpressionTerminator() bool {
	return p.Current().Kind == tokenComma || p.isColumnAttributeStart()
}

// skipDefaultExpression skips a DEFAULT expression on a column.
//
// The expression can be a literal, a function call with parentheses, or a more complex
// expression terminated by the next column clause.
//
// Returns error when a parenthesised default expression is unmatched.
func (p *parser) skipDefaultExpression() error {
	if p.Current().Kind == tokenLeftParen {
		return p.skipParenthesised()
	}

	p.skipDefaultExpressionTokens()
	return nil
}

// markKeyColumnsNotNull makes every column a table-level primary key names NOT NULL, as
// MySQL makes a key's columns.
//
// Takes columns ([]sqlmodel.Column) which are the declared columns, changed in place.
// Takes primaryKey ([]string) which names the key's columns.
func markKeyColumnsNotNull(columns []sqlmodel.Column, primaryKey []string) {
	for index := range columns {
		if slices.ContainsFunc(primaryKey, func(name string) bool { return strings.EqualFold(name, columns[index].Name) }) {
			columns[index].Nullable = false
		}
	}
}

// collectAlterTableActions makes one mutation of the mutations an ALTER TABLE statement's
// actions produce: the first, carrying the rest, in order, as its additional mutations,
// so every action is applied.
//
// Takes actions ([]*ast.CatalogueMutation) which are the actions' mutations, in order.
//
// Returns *ast.CatalogueMutation which is the first mutation, or nil when there are none.
func collectAlterTableActions(actions []*ast.CatalogueMutation) *ast.CatalogueMutation {
	if len(actions) == 0 {
		return nil
	}
	first := actions[0]
	first.AdditionalMutations = append(first.AdditionalMutations, actions[1:]...)
	return first
}

// tableRename returns the mutations that rename a table, and move it to another database
// when the new name names one, as RENAME TABLE and ALTER TABLE ... RENAME do.
//
// Takes schema (string) which is the table's database, or empty.
// Takes tableName (string) which is the table's name.
// Takes newSchema (string) which is the database the new name names, or empty.
// Takes newName (string) which is the table's new name.
//
// Returns *ast.CatalogueMutation which renames the table, carrying the move as an
// additional mutation.
func tableRename(schema, tableName, newSchema, newName string) *ast.CatalogueMutation {
	rename := ast.NewCatalogueMutation(
		ast.MutationAlterTableRenameTable,
		schema,
		tableName,
		ast.WithNewName(newName),
	)
	if newSchema != "" && !strings.EqualFold(newSchema, schema) {
		rename.AdditionalMutations = []*ast.CatalogueMutation{ast.NewCatalogueMutation(
			ast.MutationAlterTableSetSchema,
			schema,
			newName,
			ast.WithNewName(newSchema),
		)}
	}
	return rename
}

// appendConstraintPrimaryKey returns candidate when non-empty, else existing.
//
// Takes existing ([]string) which is the running primary key accumulator.
// Takes candidate ([]string) which is the latest candidate column list.
//
// Returns []string which is the chosen primary key column list.
func appendConstraintPrimaryKey(existing, candidate []string) []string {
	if len(candidate) > 0 {
		return candidate
	}
	return existing
}

// appendConstraint appends a non-nil constraint to the accumulator slice.
//
// Takes constraints ([]sqlmodel.Constraint) which is the running list.
// Takes constraint (*sqlmodel.Constraint) which is the candidate to add.
//
// Returns []sqlmodel.Constraint which is the updated accumulator.
func appendConstraint(constraints []sqlmodel.Constraint, constraint *sqlmodel.Constraint) []sqlmodel.Constraint {
	if constraint != nil {
		return append(constraints, *constraint)
	}
	return constraints
}
