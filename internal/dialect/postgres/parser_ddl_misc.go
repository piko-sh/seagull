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
	"errors"
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// sequenceRelationColumns holds the columns a query reading a sequence as a relation
// sees, as SELECT * FROM a sequence returns them: its last value, the count of values
// logged ahead and whether nextval has been called, none of them ever NULL.
var sequenceRelationColumns = []sqlmodel.Column{
	sqlmodel.NewColumn("last_value", sqlmodel.NewIntegerType("int8", widthBigint, false), false),
	sqlmodel.NewColumn("log_cnt", sqlmodel.NewIntegerType("int8", widthBigint, false), false),
	sqlmodel.NewColumn("is_called", sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "bool"), false),
}

// parseDropTable parses a DROP TABLE statement.
//
// Returns *ast.CatalogueMutation which describes the drop mutation.
// Returns error when the schema-qualified name fails to parse.
func (p *parser) parseDropTable() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordDROP, keywordTABLE); err != nil {
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

// parseAlterTable parses an ALTER TABLE statement and dispatches to the matching action
// handler.
//
// Takes engine (*Dialect) which supplies type-resolution context.
//
// Returns *ast.CatalogueMutation which describes the mutation, or nil when no recognised
// action follows.
// Returns error when a sub-clause fails to parse.
func (p *parser) parseAlterTable(engine *Dialect) (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence("ALTER", keywordTABLE); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()
	only := p.MatchKeyword("ONLY")

	schema, tableName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	var first, tail *ast.CatalogueMutation
	for {
		next, parseError := p.parseAlterTableAction(engine, schema, tableName)
		if parseError != nil {
			return nil, parseError
		}
		p.skipToActionEnd()
		for _, action := range flattenMutations(next) {
			action.IfExists, action.Only = ifExists, only
			switch {
			case first == nil:
				first, tail = action, action
			case tail.Kind == ast.MutationAlterTableAddColumn && action.Kind == ast.MutationAlterTableAddColumn &&
				tail.IfNotExists == action.IfNotExists:
				tail.Columns = append(tail.Columns, action.Columns...)
			default:
				first.AdditionalMutations = append(first.AdditionalMutations, action)
				tail = action
			}
		}
		if p.Current().Kind != tokenComma {
			return first, nil
		}
		p.Advance()
		if p.Current().Kind == tokenComma {
			return nil, p.syntaxError(fmt.Sprintf("ALTER TABLE %q: consecutive commas in action list", tableName))
		}
	}
}

// parseAlterTableAction parses a single ALTER TABLE action (ADD COLUMN, DROP, ALTER
// COLUMN, RENAME, SET, ATTACH PARTITION, DETACH PARTITION).
//
// parseAlterTable uses this to loop over comma-separated actions.
//
// Takes engine (*Dialect) which supplies type-resolution context.
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the action, or nil when no recognised
// action follows.
// Returns error when the action sub-clause fails to parse.
func (p *parser) parseAlterTableAction(
	engine *Dialect, schema, tableName string,
) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword("ADD") {
		return p.parseAlterTableAdd(engine, schema, tableName)
	}
	if p.MatchKeyword(keywordDROP) {
		return p.parseAlterTableDrop(schema, tableName)
	}
	if p.indexOptions && p.IsKeyword(keywordALTER) && p.Peek().IsKeyword(tokenIdentifier, keywordPRIMARY) &&
		p.PeekAt(2).IsKeyword(tokenIdentifier, keywordKEY) {
		return p.parseAlterPrimaryKey(schema, tableName)
	}
	if p.MatchKeyword("ALTER") {
		return p.parseAlterTableAlterColumn(engine, schema, tableName)
	}
	if p.MatchKeyword("RENAME") {
		return p.parseAlterTableRename(schema, tableName)
	}
	if p.MatchKeyword(keywordSET) {
		return p.parseAlterTableSet(schema, tableName)
	}
	if p.IsKeyword("ATTACH", "DETACH") && p.Peek().IsKeyword(tokenIdentifier, "PARTITION") {
		return p.parseAlterTablePartition(schema, tableName)
	}
	return nil, nil
}

// parseAlterPrimaryKey parses an ALTER PRIMARY KEY USING COLUMNS (...) action, which
// gives the table another primary key where the flavour reads index options, and the
// index parameters that may follow its column list.
//
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which adds the new primary key.
// Returns error when USING COLUMNS or the column list is missing or malformed.
func (p *parser) parseAlterPrimaryKey(schema, tableName string) (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordALTER, keywordPRIMARY, keywordKEY, keywordUSING, "COLUMNS"); err != nil {
		return nil, err
	}
	columns, err := p.parsePostgresColumnList()
	if err != nil {
		return nil, err
	}
	if err := p.skipIndexParameters(); err != nil {
		return nil, err
	}
	return ast.NewCatalogueMutation(ast.MutationAlterTableAddConstraint, schema, tableName, ast.WithPrimaryKey(columns)), nil
}

// parseAlterTablePartition parses an ATTACH PARTITION or DETACH PARTITION action.
//
// It records the changed parent relationship while leaving non-catalogue options for the
// caller to skip.
//
// Takes schema (string) which is the schema name of the partitioned table.
// Takes tableName (string) which is the partitioned table's name.
//
// Returns *ast.CatalogueMutation which attaches or detaches the partition.
// Returns error when the partition's name is missing.
func (p *parser) parseAlterTablePartition(schema, tableName string) (*ast.CatalogueMutation, error) {
	kind := ast.MutationAttachPartition
	if p.IsKeyword("DETACH") {
		kind = ast.MutationDetachPartition
	}
	p.Advance()
	p.Advance()
	if p.IsKeyword(keywordFOR, keywordDEFAULT) {
		return nil, p.syntaxError("expected the name of the partition")
	}
	partitionSchema, partitionName, err := p.requireSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	return ast.NewCatalogueMutation(
		kind,
		partitionSchema,
		partitionName,
		ast.WithPartitionOf(ast.TableReference{Schema: schema, Name: tableName, Alias: ""}),
	), nil
}

// parseAlterTableAdd parses an ALTER TABLE ... ADD action.
//
// Takes engine (*Dialect) which supplies type-resolution context.
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the add mutation.
// Returns error when the added column or constraint fails to parse.
func (p *parser) parseAlterTableAdd(
	engine *Dialect, schema, tableName string,
) (*ast.CatalogueMutation, error) {
	if p.isPostgresTableConstraint() {
		primaryKey, constraint, constraintError := p.parsePostgresTableConstraint()
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
		mutation.AdditionalMutations = keyColumnsNotNull(schema, tableName, primaryKey)
		return mutation, nil
	}
	p.MatchKeyword(keywordCOLUMN)
	ifNotExists := p.matchIfNotExists()
	column, columnPrimaryKey, columnError := p.parsePostgresColumnDefinition(engine)
	if columnError != nil {
		return nil, columnError
	}
	mutation := ast.NewCatalogueMutation(
		ast.MutationAlterTableAddColumn,
		schema,
		tableName,
		ast.WithColumns([]sqlmodel.Column{column}),
		ast.WithIfNotExists(ifNotExists),
	)
	if columnPrimaryKey {
		mutation.AdditionalMutations = []*ast.CatalogueMutation{ast.NewCatalogueMutation(
			ast.MutationAlterTableAddConstraint,
			schema,
			tableName,
			ast.WithPrimaryKey([]string{column.Name}),
		)}
	}
	return mutation, nil
}

// parseAlterTableDrop parses an ALTER TABLE ... DROP action.
//
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the drop mutation.
// Returns error when the dropped item name fails to parse.
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
	mutation := ast.NewCatalogueMutation(
		ast.MutationAlterTableDropColumn,
		schema,
		tableName,
		ast.WithColumnName(columnName),
	)
	mutation.IfColumnExists = ifColumnExists
	return mutation, nil
}

// parseAlterTableAlterColumn records column type and nullability changes.
//
// Other ALTER COLUMN actions are consumed but do not change the catalogue.
//
// Takes engine (*Dialect) which normalises the type a TYPE action names.
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the alter mutation.
// Returns error when the column name fails to parse.
func (p *parser) parseAlterTableAlterColumn(engine *Dialect, schema, tableName string) (*ast.CatalogueMutation, error) {
	p.MatchKeyword(keywordCOLUMN)
	columnName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nil, nameError
	}
	options := []ast.CatalogueMutationOption{ast.WithColumnName(columnName)}
	switch {
	case p.IsKeyword(keywordSET) && p.Peek().IsKeyword(tokenIdentifier, keywordNOT):
		p.Advance()
		p.Advance()
		p.MatchKeyword(keywordNULL)
		options = append(options, ast.WithColumnNullability(false))
	case p.IsKeyword(keywordDROP) && p.Peek().IsKeyword(tokenIdentifier, keywordNOT):
		p.Advance()
		p.Advance()
		p.MatchKeyword(keywordNULL)
		options = append(options, ast.WithColumnNullability(true))
	case p.IsKeyword(keywordSET) && p.Peek().IsKeyword(tokenIdentifier, "DATA"):
		p.Advance()
		p.Advance()
		if err := p.requireKeyword(keywordTYPE); err != nil {
			return nil, err
		}
		sqlType, arrayDimensions := p.parseColumnType(engine)
		options = append(options, ast.WithColumnType(sqlType, arrayDimensions))
	case p.MatchKeyword(keywordTYPE):
		sqlType, arrayDimensions := p.parseColumnType(engine)
		options = append(options, ast.WithColumnType(sqlType, arrayDimensions))
	}
	p.skipToActionEnd()
	return ast.NewCatalogueMutation(
		ast.MutationAlterTableAlterColumn,
		schema,
		tableName,
		options...,
	), nil
}

// skipToActionEnd advances past the rest of an ALTER TABLE action: to the comma that
// starts the next action, outside any parentheses, or to the end of the statement.
func (p *parser) skipToActionEnd() {
	for !p.AtEnd() && p.Current().Kind != tokenComma && p.Current().Kind != tokenSemicolon && p.Current().Kind != tokenEOF {
		if p.Current().Kind == tokenLeftParen {
			if p.requireSkipParenthesised() != nil {
				return
			}
			continue
		}
		p.Advance()
	}
}

// parseAlterTableRename parses an ALTER TABLE ... RENAME action.
//
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the rename mutation.
// Returns error when a renamed name fails to parse.
func (p *parser) parseAlterTableRename(schema, tableName string) (*ast.CatalogueMutation, error) {
	if p.MatchKeyword(keywordCONSTRAINT) {
		return nil, p.skipRenamedName()
	}
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
	if err := p.requireKeyword("TO"); err != nil {
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

// skipRenamedName consumes the old name, TO and the new name of a RENAME of something the
// catalogue does not record, such as a constraint.
//
// Returns error when a name or TO is missing.
func (p *parser) skipRenamedName() error {
	if _, err := p.parseIdentifierOrKeyword(); err != nil {
		return err
	}
	if err := p.requireKeyword("TO"); err != nil {
		return err
	}
	_, err := p.parseIdentifierOrKeyword()
	return err
}

// parseAlterTableSet parses an ALTER TABLE ... SET action.
//
// Takes schema (string) which is the schema name of the target table.
// Takes tableName (string) which is the target table name.
//
// Returns *ast.CatalogueMutation which describes the set mutation, or nil when no
// recognised SET sub-clause is present.
// Returns error when the new schema name fails to parse or a parenthesised option list is
// unbalanced.
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

	if p.Current().Kind == tokenLeftParen {
		return nil, p.requireSkipParenthesised()
	}
	return nil, nil
}

// parseAlterView parses an ALTER VIEW or ALTER MATERIALIZED VIEW statement: RENAME TO,
// SET SCHEMA and RENAME COLUMN change the catalogue, and any other action, such as OWNER
// TO or SET (...), leaves it as it is.
//
// Returns *ast.CatalogueMutation which describes the change, or nil when the action
// changes nothing the catalogue holds.
// Returns error when a name fails to parse.
func (p *parser) parseAlterView() (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword("ALTER"); err != nil {
		return nil, err
	}
	p.MatchKeyword(keywordMATERIALIZED)
	if err := p.requireKeyword(keywordVIEW); err != nil {
		return nil, err
	}
	ifExists := p.matchIfExists()
	schema, viewName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	var mutation *ast.CatalogueMutation
	switch {
	case p.MatchKeyword("RENAME"):
		mutation, err = p.parseAlterTableRename(schema, viewName)
	case p.MatchKeyword(keywordSET):
		mutation, err = p.parseAlterTableSet(schema, viewName)
	}
	if mutation != nil {
		mutation.IfExists = ifExists
	}
	return mutation, err
}

// parseCreateView parses a CREATE [MATERIALIZED] VIEW statement: its name, an optional
// column list, the USING, WITH (...) and TABLESPACE clauses that may come before AS, and
// the query after AS, which gives the view its columns.
//
// Returns *ast.CatalogueMutation which describes the view mutation.
// Returns error when the view name, column list or a clause fails to parse, AS is
// missing, or the query is malformed.
func (p *parser) parseCreateView() (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword(keywordCREATE); err != nil {
		return nil, err
	}

	p.skipOrReplace()

	p.MatchKeyword("TEMP")
	p.MatchKeyword("TEMPORARY")
	p.MatchKeyword(keywordMATERIALIZED)

	if err := p.requireKeyword(keywordVIEW); err != nil {
		return nil, err
	}

	ifNotExists := p.matchIfNotExists()

	schema, viewName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	var explicitColumnNames []string
	if p.Current().Kind == tokenLeftParen {
		names, listError := p.parsePostgresColumnList()
		if listError != nil {
			return nil, listError
		}
		explicitColumnNames = names
	}

	mutation := ast.NewCatalogueMutation(ast.MutationCreateView, schema, viewName, ast.WithIfNotExists(ifNotExists))

	if err := p.skipStorageClauses(); err != nil {
		return nil, err
	}
	if err := p.requireKeyword(keywordAS); err != nil {
		return nil, err
	}
	viewDefinition, bodyErr := p.analyseViewBody(explicitColumnNames)
	if bodyErr != nil {
		return nil, bodyErr
	}
	mutation.ViewDefinition = viewDefinition
	mutation.Columns = dialectkit.ColumnsFromNames(explicitColumnNames)

	return mutation, nil
}

// skipOrReplace consumes an optional `OR REPLACE` qualifier when it is present.
func (p *parser) skipOrReplace() {
	if p.MatchKeyword("OR") {
		p.MatchKeyword("REPLACE")
	}
}

// analyseViewBody analyses the query body of a CREATE VIEW, CREATE TABLE ... AS or an
// extension's view-like statement, so the catalogue can store typed columns.
//
// A trailing WITH [CASCADED | LOCAL] CHECK OPTION or WITH [NO] DATA belongs to the
// statement, not its body, so it is left out; anything else left after the body makes it
// malformed. A malformed body, or one whose analysis stops, fails the statement: its
// error is recorded as the statement's and returned. A body the parser does not analyse,
// one that is no query or one nested beyond the parse depth limit, is recorded as an
// unanalysed part of the statement, which its mutation reports, and gives no analysis, so
// the catalogue falls back to the declared column names.
//
// Takes columnNames ([]string) which is the declared column list overlaid onto the
// inferred projection names.
//
// Returns *ast.RawQueryAnalysis which describes the body, or nil when it was not
// analysed.
// Returns error when the body is missing or malformed, or its analysis stops.
func (p *parser) analyseViewBody(columnNames []string) (*ast.RawQueryAnalysis, error) {
	body := trimViewBodySuffix(p.Tokens[p.Position:p.statementEnd()])
	if len(body) == 0 {
		return nil, p.syntaxError("expected the query the statement is defined by")
	}

	viewAnalysis, err := p.analyseIsolatedViewBody(body)
	switch {
	case errors.Is(err, dialectkit.ErrParseDepth):
		p.recordUnanalysedPart(body, "the query was not analysed, so the columns it gives are unknown: "+err.Error())
		return nil, nil
	case err != nil:
		return nil, p.recordSyntaxError(err)
	case viewAnalysis == nil:
		p.recordUnanalysedPart(body, "the body is not a query Seagull analyses, so the columns it gives are unknown")
		return nil, nil
	}

	if len(columnNames) > 0 && !slices.ContainsFunc(viewAnalysis.OutputColumns, dialectkit.IsStarColumn) {
		dialectkit.OverlayViewColumnNames(viewAnalysis, columnNames, dialectkit.UndeclaredColumnsKept)
	}

	return viewAnalysis, nil
}

// recordUnanalysedPart records a part of the statement the parser did not analyse, which
// the statement's mutation reports.
//
// Takes tokens ([]token) which are the part's tokens; there is at least one.
// Takes message (string) which says what was not analysed and why.
func (p *parser) recordUnanalysedPart(tokens []token, message string) {
	p.unanalysedParts = append(p.unanalysedParts, ast.Warning{
		Message: message,
		Span:    dialectkit.TokenRangeSpan(tokens, 0, len(tokens)),
	})
}

// analyseIsolatedViewBody analyses a view body in a child parser with its own syntax
// record, requiring the body to end where its tokens do.
//
// Takes tokens ([]token) which are the view body's tokens.
//
// Returns *ast.RawQueryAnalysis which describes the body, or nil when it is not a query.
// Returns error when the body is malformed or has tokens left over.
func (p *parser) analyseIsolatedViewBody(tokens []token) (*ast.RawQueryAnalysis, error) {
	viewParser := p.newChildParser(tokens)
	viewParser.syntax = new(syntaxState)
	viewParser.parameterCount = 0
	if viewParser.Current().Kind != tokenLeftParen && !startsQuery(viewParser.Current()) {
		return nil, nil
	}
	analysis, err := viewParser.analyseSelect()
	if err == nil {
		err = viewParser.requireEnd()
	}
	if viewParser.syntax.err != nil {
		return nil, viewParser.syntax.err
	}
	return analysis, err
}

// parseDropView parses a DROP VIEW statement.
//
// Returns *ast.CatalogueMutation which describes the drop mutation.
// Returns error when the view name fails to parse.
func (p *parser) parseDropView() (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword(keywordDROP); err != nil {
		return nil, err
	}

	p.MatchKeyword(keywordMATERIALIZED)

	if err := p.requireKeyword(keywordVIEW); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schema, viewName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(ast.MutationDropView, schema, viewName, ast.WithIfExists(ifExists)), nil
}

// parseCreateIndex parses a CREATE INDEX statement.
//
// Returns *ast.CatalogueMutation which describes the index mutation.
// Returns error when the index or table name fails to parse.
func (p *parser) parseCreateIndex() (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword(keywordCREATE); err != nil {
		return nil, err
	}

	p.MatchKeyword(keywordUNIQUE)

	if err := p.requireKeyword("INDEX"); err != nil {
		return nil, err
	}

	p.MatchKeyword("CONCURRENTLY")

	p.matchIfNotExists()

	indexName := ""
	if !p.IsKeyword(keywordON) {
		name, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			return nil, nameError
		}
		indexName = name
	}

	if err := p.requireKeyword(keywordON); err != nil {
		return nil, err
	}

	p.MatchKeyword("ONLY")

	schema, tableName, tableError := p.parseSchemaQualifiedName()
	if tableError != nil {
		return nil, tableError
	}

	if p.MatchKeyword(keywordUSING) {
		p.Advance()
	}

	return ast.NewCatalogueMutation(
		ast.MutationCreateIndex,
		schema,
		tableName,
		ast.WithNewName(indexName),
	), nil
}

// parseDropIndex parses a DROP INDEX statement.
//
// Returns *ast.CatalogueMutation which describes the drop mutation.
// Returns error when the index name fails to parse.
func (p *parser) parseDropIndex() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordDROP, "INDEX"); err != nil {
		return nil, err
	}

	p.MatchKeyword("CONCURRENTLY")

	ifExists := p.matchIfExists()

	schema, indexName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(
		ast.MutationDropIndex,
		schema,
		"",
		ast.WithNewName(indexName),
		ast.WithIfExists(ifExists),
	), nil
}

// parseCreateOrDropTrigger dispatches to the create or drop trigger parser.
//
// Takes kind (statementKind) which selects the create or drop branch.
//
// Returns *ast.CatalogueMutation which describes the trigger mutation.
// Returns error when the chosen parser fails.
func (p *parser) parseCreateOrDropTrigger(kind statementKind) (*ast.CatalogueMutation, error) {
	if kind == statementKindCreateTrigger {
		return p.parseCreateTrigger()
	}
	return p.parseDropTrigger()
}

// parseCreateTrigger parses a CREATE [OR REPLACE] [CONSTRAINT] TRIGGER statement.
//
// Returns *ast.CatalogueMutation which describes the trigger mutation.
// Returns error when the trigger name fails to parse.
func (p *parser) parseCreateTrigger() (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	p.skipOrReplace()
	p.MatchKeyword(keywordCONSTRAINT)
	if err := p.requireKeyword("TRIGGER"); err != nil {
		return nil, err
	}

	triggerName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	for !p.AtEnd() && !p.IsKeyword(keywordON) {
		p.Advance()
	}

	tableName := ""
	if p.MatchKeyword(keywordON) {
		_, name, nameError := p.parseSchemaQualifiedName()
		if nameError == nil {
			tableName = name
		}
	}

	return ast.NewCatalogueMutation(
		ast.MutationCreateTrigger,
		"",
		tableName,
		ast.WithTriggerName(triggerName),
	), nil
}

// parseDropTrigger parses a DROP TRIGGER statement.
//
// Returns *ast.CatalogueMutation which describes the drop mutation.
// Returns error when the trigger name fails to parse.
func (p *parser) parseDropTrigger() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordDROP, "TRIGGER"); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	triggerName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	tableName := ""
	if p.MatchKeyword(keywordON) {
		_, name, nameError := p.parseSchemaQualifiedName()
		if nameError == nil {
			tableName = name
		}
	}

	return ast.NewCatalogueMutation(
		ast.MutationDropTrigger,
		"",
		tableName,
		ast.WithTriggerName(triggerName),
		ast.WithIfExists(ifExists),
	), nil
}

// parseCreateSequence parses a CREATE SEQUENCE statement. The sequence carries the
// columns a query reading it as a relation sees.
//
// Returns *ast.CatalogueMutation which describes the sequence mutation.
// Returns error when the sequence name or options fail to parse.
func (p *parser) parseCreateSequence() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordCREATE, "SEQUENCE"); err != nil {
		return nil, err
	}

	ifNotExists := p.matchIfNotExists()

	schema, sequenceName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	ownedByTable, ownedByColumn, ownedError := p.parseSequenceOptions()
	if ownedError != nil {
		return nil, ownedError
	}

	return ast.NewCatalogueMutation(
		ast.MutationCreateSequence,
		schema,
		"",
		ast.WithSequenceOwner(ownedByTable, ownedByColumn),
		ast.WithSequence(sequenceName),
		ast.WithIfNotExists(ifNotExists),
		ast.WithColumns(slices.Clone(sequenceRelationColumns)),
	), nil
}

// parseSequenceOptions parses CREATE SEQUENCE options up to the terminator.
//
// Returns ownedTable (string) which is the OWNED BY table when present.
// Returns ownedColumn (string) which is the OWNED BY column when present.
// Returns err (error) when the owned-by target fails to parse.
func (p *parser) parseSequenceOptions() (ownedTable string, ownedColumn string, err error) {
	var ownedByTable, ownedByColumn string
	for !p.AtEnd() && p.Current().Kind != tokenSemicolon && p.Current().Kind != tokenEOF {
		if !p.MatchKeyword("OWNED") {
			p.Advance()
			continue
		}
		p.MatchKeyword(keywordBY)
		if p.MatchKeyword("NONE") {
			continue
		}
		table, column, ownedError := p.parseOwnedByTarget()
		if ownedError != nil {
			return "", "", ownedError
		}
		ownedByTable = table
		ownedByColumn = column
	}
	return ownedByTable, ownedByColumn, nil
}

// parseOwnedByTarget parses an OWNED BY target of table[.column].
//
// Returns tableName (string) which is the owning table name.
// Returns columnName (string) which is the owning column name when present.
// Returns err (error) when an identifier fails to parse.
func (p *parser) parseOwnedByTarget() (tableName string, columnName string, err error) {
	tableName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return "", "", nameError
	}
	if p.Current().Kind != tokenDot {
		return tableName, "", nil
	}
	p.Advance()
	columnName, columnError := p.parseIdentifierOrKeyword()
	if columnError != nil {
		return "", "", columnError
	}
	return tableName, columnName, nil
}

// parseAlterSequence parses an ALTER SEQUENCE statement: RENAME TO and SET SCHEMA move
// the sequence, as on a table; its other options leave the catalogue alone.
//
// The mutation is restricted to a sequence, so the catalogue never renames or moves a
// table or view of the same name, as PostgreSQL refuses to.
//
// Returns *ast.CatalogueMutation which renames or moves the sequence, or nil when the
// statement changes only its options.
// Returns error when the sequence's name or the new name is missing.
func (p *parser) parseAlterSequence() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordALTER, "SEQUENCE"); err != nil {
		return nil, err
	}
	ifExists := p.matchIfExists()
	schema, sequenceName, err := p.requireSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	kind := ast.MutationAlterTableRenameTable
	switch {
	case p.IsKeyword("RENAME") && p.Peek().IsKeyword(tokenIdentifier, keywordTO):
		p.Advance()
		p.Advance()
	case p.IsKeyword(keywordSET) && p.Peek().IsKeyword(tokenIdentifier, keywordSCHEMA):
		p.Advance()
		p.Advance()
		kind = ast.MutationAlterTableSetSchema
	default:
		return nil, nil
	}
	newName, err := p.requireIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	return ast.NewCatalogueMutation(kind, schema, sequenceName, ast.WithNewName(newName), ast.WithIfExists(ifExists),
		ast.WithTargetKind(ast.ObjectKindSequence)), nil
}

// parseDropSequence parses a DROP SEQUENCE statement.
//
// Returns *ast.CatalogueMutation which describes the drop mutation.
// Returns error when the sequence name fails to parse.
func (p *parser) parseDropSequence() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordDROP, "SEQUENCE"); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schema, sequenceName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	p.MatchKeyword(keywordCASCADE)
	p.MatchKeyword(keywordRESTRICT)

	return ast.NewCatalogueMutation(
		ast.MutationDropSequence,
		schema,
		"",
		ast.WithSequence(sequenceName),
		ast.WithIfExists(ifExists),
	), nil
}

// parseComment parses a COMMENT ON statement.
//
// Returns *ast.CatalogueMutation which describes the comment mutation.
// Returns error when the target identifier fails to parse.
func (p *parser) parseComment() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence("COMMENT", keywordON); err != nil {
		return nil, err
	}

	mutation := ast.NewCatalogueMutation(ast.MutationComment, "", "")

	if parseError := p.parseCommentTarget(mutation); parseError != nil {
		return nil, parseError
	}

	mutation.Comment = p.parseCommentValue()

	return mutation, nil
}

// parseCommentTarget dispatches to the parser for the target object type.
//
// Takes mutation (*ast.CatalogueMutation) which is populated with the parsed target.
//
// Returns error when the target name fails to parse.
func (p *parser) parseCommentTarget(mutation *ast.CatalogueMutation) error {
	if p.MatchKeyword(keywordTABLE) {
		return p.parseCommentOnTable(mutation)
	}
	if p.MatchKeyword(keywordCOLUMN) {
		return p.parseCommentOnColumn(mutation)
	}
	if p.MatchKeyword(keywordTYPE) {
		return p.parseCommentOnType(mutation)
	}
	if p.MatchKeyword("FUNCTION") {
		return p.parseCommentOnFunction(mutation)
	}
	if p.MatchKeyword(keywordSCHEMA) {
		return p.parseCommentOnSchema(mutation)
	}
	p.MatchKeyword(keywordMATERIALIZED)
	if p.MatchKeyword(keywordVIEW) {
		return p.parseCommentOnTable(mutation)
	}
	for !p.AtEnd() && !p.IsKeyword("IS") {
		p.Advance()
	}
	return nil
}

// parseCommentOnTable parses the target of COMMENT ON TABLE, VIEW or MATERIALIZED VIEW, a
// relation the catalogue finds among its tables, then its views.
//
// Takes mutation (*ast.CatalogueMutation) which is populated with the schema and relation
// names.
//
// Returns error when the schema-qualified name fails to parse.
func (p *parser) parseCommentOnTable(mutation *ast.CatalogueMutation) error {
	schema, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nameError
	}
	mutation.SchemaName = schema
	mutation.TableName = tableName
	return nil
}

// parseCommentOnColumn parses COMMENT ON COLUMN target metadata.
//
// Takes mutation (*ast.CatalogueMutation) which is populated with the schema, table, and
// column names.
//
// Returns error when an identifier fails to parse.
func (p *parser) parseCommentOnColumn(mutation *ast.CatalogueMutation) error {
	parts := make([]string, 0, maxCommentColumnParts)
	for {
		part, partError := p.parseIdentifierOrKeyword()
		if partError != nil {
			return partError
		}
		parts = append(parts, part)
		if p.Current().Kind != tokenDot || len(parts) == maxCommentColumnParts {
			break
		}
		p.Advance()
	}
	if len(parts) < minCommentColumnParts {
		return p.syntaxError("expected a column qualified by its table")
	}
	parts = parts[max(0, len(parts)-minCommentColumnParts-1):]
	if len(parts) > minCommentColumnParts {
		mutation.SchemaName, parts = parts[0], parts[1:]
	}
	mutation.TableName = parts[0]
	mutation.ColumnName = parts[1]
	return nil
}

// parseCommentOnType parses COMMENT ON TYPE target metadata.
//
// Takes mutation (*ast.CatalogueMutation) which is populated with the schema and type
// names.
//
// Returns error when the schema-qualified name fails to parse.
func (p *parser) parseCommentOnType(mutation *ast.CatalogueMutation) error {
	schema, typeName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nameError
	}
	mutation.SchemaName = schema
	mutation.EnumName = typeName
	return nil
}

// parseCommentOnFunction parses COMMENT ON FUNCTION target metadata.
//
// Takes mutation (*ast.CatalogueMutation) which is populated with the function signature
// and schema.
//
// Returns error when the schema-qualified name fails to parse or the argument list is
// unbalanced.
func (p *parser) parseCommentOnFunction(mutation *ast.CatalogueMutation) error {
	schema, functionName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nameError
	}
	mutation.SchemaName = schema
	mutation.Function = ast.NewFunctionReference(schema, functionName)
	if p.Current().Kind == tokenLeftParen {
		return p.requireSkipParenthesised()
	}
	return nil
}

// parseCommentOnSchema parses COMMENT ON SCHEMA target metadata.
//
// Takes mutation (*ast.CatalogueMutation) which is populated with the schema name.
//
// Returns error when the schema identifier fails to parse.
func (p *parser) parseCommentOnSchema(mutation *ast.CatalogueMutation) error {
	schemaName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nameError
	}
	mutation.SchemaName = schemaName
	return nil
}

// parseCommentValue consumes the IS '...' or IS NULL trailing value clause. The comment
// may be written as any string constant: plain, E'...' with escapes, or dollar-quoted.
//
// Returns string which is the comment, or empty for IS NULL, which removes it.
func (p *parser) parseCommentValue() string {
	if !p.MatchKeyword("IS") {
		return ""
	}
	if kind := p.Current().Kind; kind == tokenString || kind == tokenEscapeString || kind == tokenDollarString {
		comment := p.Current().Value
		p.Advance()
		return comment
	}
	p.MatchKeyword(keywordNULL)
	return ""
}

// flattenMutations lists a mutation followed by its additional mutations, each with none
// of its own, so a statement can carry them all on its first mutation.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation, or nil.
//
// Returns []*ast.CatalogueMutation which holds the mutations in order, or nil.
func flattenMutations(mutation *ast.CatalogueMutation) []*ast.CatalogueMutation {
	if mutation == nil {
		return nil
	}
	mutations := append([]*ast.CatalogueMutation{mutation}, mutation.AdditionalMutations...)
	mutation.AdditionalMutations = nil
	return mutations
}

// trimViewBodySuffix drops the clause that may end a view statement after its body: a
// WITH [CASCADED | LOCAL] CHECK OPTION, or the WITH [NO] DATA of a materialised view.
//
// Takes tokens ([]token) which are the tokens after AS in a CREATE VIEW.
//
// Returns []token which are the tokens of the body alone.
func trimViewBodySuffix(tokens []token) []token {
	count := len(tokens)
	if count >= 2 && tokens[count-1].IsKeyword(tokenIdentifier, "DATA") {
		withIndex := count - 2
		if tokens[withIndex].IsKeyword(tokenIdentifier, "NO") {
			withIndex--
		}
		if withIndex >= 0 && tokens[withIndex].IsKeyword(tokenIdentifier, keywordWITH) {
			return tokens[:withIndex]
		}
		return tokens
	}
	if count < checkOptionClauseLength || !tokens[count-1].IsKeyword(tokenIdentifier, "OPTION") ||
		!tokens[count-2].IsKeyword(tokenIdentifier, keywordCHECK) {
		return tokens
	}
	withIndex := count - checkOptionClauseLength
	if tokens[withIndex].IsKeyword(tokenIdentifier, "CASCADED") || tokens[withIndex].IsKeyword(tokenIdentifier, "LOCAL") {
		withIndex--
	}
	if withIndex < 0 || !tokens[withIndex].IsKeyword(tokenIdentifier, keywordWITH) {
		return tokens
	}
	return tokens[:withIndex]
}
