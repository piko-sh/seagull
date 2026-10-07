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
	"seagull.sh/seagull/internal/dialect/ast"
)

// matchIfNotExists consumes an IF NOT EXISTS clause when present.
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

// matchIfExists consumes an IF EXISTS clause when present.
//
// Returns bool which is true when the clause was present.
func (p *parser) matchIfExists() bool {
	if !p.MatchKeyword("IF") {
		return false
	}
	p.MatchKeyword(keywordEXISTS)
	return true
}

// parseCreateSchema parses a CREATE SCHEMA statement.
//
// Returns *ast.CatalogueMutation which describes the create schema mutation.
// Returns error when the schema identifier cannot be parsed.
func (p *parser) parseCreateSchema() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords(keywordCREATE, keywordSCHEMA); err != nil {
		return nil, err
	}

	p.matchIfNotExists()

	schemaName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(ast.MutationCreateSchema, schemaName, ""), nil
}

// parseDropSchema parses a DROP SCHEMA statement.
//
// Returns *ast.CatalogueMutation which describes the drop schema mutation.
// Returns error when the schema identifier cannot be parsed.
func (p *parser) parseDropSchema() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords(keywordDROP, keywordSCHEMA); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schemaName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	p.MatchKeyword(keywordCASCADE)
	p.MatchKeyword(keywordRESTRICT)

	return ast.NewCatalogueMutation(ast.MutationDropSchema, schemaName, "", ast.WithIfExists(ifExists)), nil
}

// parseCreateSequence parses a CREATE SEQUENCE statement.
//
// Returns *ast.CatalogueMutation which describes the create sequence mutation, including
// any OWNED BY target.
// Returns error when the sequence name or options cannot be parsed.
func (p *parser) parseCreateSequence() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords(keywordCREATE, "SEQUENCE"); err != nil {
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
		ast.WithSequence(sequenceName),
		ast.WithSequenceOwner(ownedByTable, ownedByColumn),
		ast.WithIfNotExists(ifNotExists),
	), nil
}

// parseSequenceOptions scans sequence options for an OWNED BY target.
//
// Returns ownedTable (string) which is the owning table, empty when none.
// Returns ownedColumn (string) which is the owning column, empty when none.
// Returns error when the OWNED BY target cannot be parsed.
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

// parseOwnedByTarget parses an OWNED BY table or table.column reference.
//
// Returns tableName (string) which is the owning table name.
// Returns columnName (string) which is the owning column name, empty when only a table
// was provided.
// Returns error when an identifier cannot be parsed.
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

// parseDropSequence parses a DROP SEQUENCE statement.
//
// Returns *ast.CatalogueMutation which describes the drop sequence mutation.
// Returns error when the sequence name cannot be parsed.
func (p *parser) parseDropSequence() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords(keywordDROP, "SEQUENCE"); err != nil {
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

// parseCreateIndex parses a CREATE INDEX statement.
//
// Returns *ast.CatalogueMutation which describes the create index mutation including
// target table and optional index name.
// Returns error when an identifier or schema name cannot be parsed.
func (p *parser) parseCreateIndex() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}

	p.MatchKeyword(keywordUNIQUE)

	if _, err := p.ExpectKeyword("INDEX"); err != nil {
		return nil, err
	}

	p.matchIfNotExists()

	indexName := ""
	if !p.IsKeyword(keywordON) {
		name, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			return nil, nameError
		}
		indexName = name
	}

	if _, err := p.ExpectKeyword(keywordON); err != nil {
		return nil, err
	}

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
// Returns *ast.CatalogueMutation which describes the drop index mutation.
// Returns error when the index name cannot be parsed.
func (p *parser) parseDropIndex() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords(keywordDROP, "INDEX"); err != nil {
		return nil, err
	}

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

// parseComment parses a COMMENT ON target IS value statement.
//
// Returns *ast.CatalogueMutation which describes the comment target.
// Returns error when the comment target cannot be parsed.
func (p *parser) parseComment() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords("COMMENT", keywordON); err != nil {
		return nil, err
	}

	mutation := ast.NewCatalogueMutation(ast.MutationComment, "", "")

	if parseError := p.parseCommentTarget(mutation); parseError != nil {
		return nil, parseError
	}

	mutation.Comment = p.parseCommentValue()

	return mutation, nil
}

// parseCommentTarget dispatches COMMENT ON to the appropriate parser.
//
// Takes mutation (*ast.CatalogueMutation) which receives the target identifiers for the
// comment.
//
// Returns error when the target identifiers cannot be parsed.
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
	if p.MatchKeyword("FUNCTION") || p.MatchKeyword(keywordMACRO) {
		return p.parseCommentOnFunction(mutation)
	}
	if p.MatchKeyword(keywordSCHEMA) {
		return p.parseCommentOnSchema(mutation)
	}
	if p.MatchKeyword("VIEW") {
		return p.parseCommentOnTable(mutation)
	}
	for !p.AtEnd() && !p.IsKeyword("IS") {
		p.Advance()
	}
	return nil
}

// parseCommentOnTable parses the relation name for a COMMENT ON TABLE or COMMENT ON VIEW
// statement, a relation the catalogue finds among its tables, then its views.
//
// Takes mutation (*ast.CatalogueMutation) which receives the schema and relation name.
//
// Returns error when the schema-qualified name cannot be parsed.
func (p *parser) parseCommentOnTable(mutation *ast.CatalogueMutation) error {
	schema, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nameError
	}
	mutation.SchemaName = schema
	mutation.TableName = tableName
	return nil
}

// parseCommentOnColumn parses the column reference for COMMENT ON COLUMN.
//
// Takes mutation (*ast.CatalogueMutation) which receives the schema, table and column
// names.
//
// Returns error when the qualified column reference cannot be parsed.
func (p *parser) parseCommentOnColumn(mutation *ast.CatalogueMutation) error {
	schema, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nameError
	}
	if p.Current().Kind != tokenDot {
		mutation.TableName = schema
		mutation.ColumnName = tableName
		return nil
	}
	p.Advance()
	columnName, columnError := p.parseIdentifierOrKeyword()
	if columnError != nil {
		return columnError
	}
	mutation.SchemaName = schema
	mutation.TableName = tableName
	mutation.ColumnName = columnName
	return nil
}

// parseCommentOnType parses the type name for a COMMENT ON TYPE statement.
//
// Takes mutation (*ast.CatalogueMutation) which receives the schema and type name.
//
// Returns error when the schema-qualified name cannot be parsed.
func (p *parser) parseCommentOnType(mutation *ast.CatalogueMutation) error {
	schema, typeName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nameError
	}
	mutation.SchemaName = schema
	mutation.EnumName = typeName
	return nil
}

// parseCommentOnFunction parses the function name for COMMENT ON FUNCTION.
//
// Takes mutation (*ast.CatalogueMutation) which receives the schema and function
// signature.
//
// Returns error when the schema-qualified name cannot be parsed or the argument list is
// unbalanced.
func (p *parser) parseCommentOnFunction(mutation *ast.CatalogueMutation) error {
	schema, functionName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nameError
	}
	mutation.SchemaName = schema
	mutation.Function = ast.NewFunctionReference(schema, functionName)
	return p.skipParenthesisedIfPresent()
}

// parseCommentOnSchema parses the schema name for COMMENT ON SCHEMA.
//
// Takes mutation (*ast.CatalogueMutation) which receives the schema name.
//
// Returns error when the schema identifier cannot be parsed.
func (p *parser) parseCommentOnSchema(mutation *ast.CatalogueMutation) error {
	schemaName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nameError
	}
	mutation.SchemaName = schemaName
	return nil
}

// parseCommentValue consumes the IS '...' or IS NULL value of a comment.
//
// Returns string which is the comment, or empty for IS NULL, which removes it.
func (p *parser) parseCommentValue() string {
	if !p.MatchKeyword("IS") {
		return ""
	}
	if p.Current().Kind == tokenString {
		comment := p.Current().Value
		p.Advance()
		return comment
	}
	p.MatchKeyword(keywordNULL)
	return ""
}

// parseAlterView parses ALTER VIEW [IF EXISTS] name RENAME TO new_name, the one ALTER
// VIEW form that changes the catalogue.
//
// Returns *ast.CatalogueMutation which renames the view, or nil for another action.
// Returns error when a name cannot be parsed.
func (p *parser) parseAlterView() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords("ALTER", "VIEW"); err != nil {
		return nil, err
	}
	ifExists := p.matchIfExists()
	schema, viewName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	if !p.MatchKeyword("RENAME") {
		return nil, nil
	}
	mutation, err := p.parseAlterTableRename(schema, viewName)
	markIfExists(mutation, ifExists)
	return mutation, err
}

// markIfExists marks a mutation and each of its additional mutations as written IF EXISTS
// of the object they act on.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation, or nil.
// Takes ifExists (bool) which is true when the statement said IF EXISTS.
func markIfExists(mutation *ast.CatalogueMutation, ifExists bool) {
	if mutation == nil {
		return
	}
	mutation.IfExists = ifExists
	for _, additional := range mutation.AdditionalMutations {
		additional.IfExists = ifExists
	}
}
