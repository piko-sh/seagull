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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// partitionElements is what the element list of a CREATE TABLE ... PARTITION OF declares.
type partitionElements struct {
	// primaryKey lists the columns of a primary key the partition declares.
	primaryKey []string

	// constraints are the other constraints it declares.
	constraints []sqlmodel.Constraint

	// notNullColumns lists the columns it makes NOT NULL.
	notNullColumns []string
}

// parsePartitionOf parses a CREATE TABLE PARTITION OF statement, whose partition takes
// its parent's columns.
//
// The optional element list may make the partition's columns NOT NULL, carried as
// mutations that follow the creation, and may give it constraints and, where the parent
// has none, a primary key.
//
// Takes schema (string) which is the partition's schema.
// Takes tableName (string) which is the partition's name.
//
// Returns *ast.CatalogueMutation which creates the partition under its parent.
// Returns error when OF or the parent name is missing, or the element list is malformed.
func (p *parser) parsePartitionOf(schema, tableName string) (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword("OF"); err != nil {
		return nil, err
	}
	parentSchema, parentName, err := p.requireSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	elements := partitionElements{primaryKey: nil, constraints: nil, notNullColumns: nil}
	if p.Current().Kind == tokenLeftParen {
		if elements, err = p.parsePartitionElements(); err != nil {
			return nil, err
		}
	}
	p.skipToStatementEnd()
	mutation := ast.NewCatalogueMutation(
		ast.MutationCreateTable,
		schema,
		tableName,
		ast.WithPartitionOf(ast.TableReference{Schema: parentSchema, Name: parentName, Alias: ""}),
		ast.WithPrimaryKey(elements.primaryKey),
		ast.WithConstraints(elements.constraints),
	)
	mutation.AdditionalMutations = keyColumnsNotNull(schema, tableName, append(elements.notNullColumns, elements.primaryKey...))
	return mutation, nil
}

// parsePartitionElements parses the parenthesised element list of a CREATE TABLE ...
// PARTITION OF: table constraints, and columns, each named with optional WITH OPTIONS and
// column constraints but no type.
//
// Returns partitionElements which holds what the list declares.
// Returns error when an element is malformed or the list is not closed.
func (p *parser) parsePartitionElements() (partitionElements, error) {
	var elements partitionElements
	p.Advance()
	for p.Current().Kind != tokenRightParen {
		if p.AtEnd() {
			return elements, p.syntaxError("expected ')' to close the partition's element list")
		}
		if p.isPostgresTableConstraint() {
			primaryKey, constraint, err := p.parsePostgresTableConstraint()
			if err != nil {
				return elements, err
			}
			elements.primaryKey = appendConstraintPrimaryKey(elements.primaryKey, primaryKey)
			elements.constraints = appendConstraint(elements.constraints, constraint)
		} else if err := p.parsePartitionColumnElement(&elements); err != nil {
			return elements, err
		}
		switch p.Current().Kind {
		case tokenComma:
			p.Advance()
		case tokenRightParen:
		default:
			return elements, p.syntaxError("expected , or ) after a partition element")
		}
	}
	p.Advance()
	return elements, nil
}

// parsePartitionColumnElement parses a column element of a partition's element list: the
// column's name, an optional WITH OPTIONS, and its constraints, of which NOT NULL and
// PRIMARY KEY are recorded.
//
// Takes elements (*partitionElements) which receives what the element declares.
//
// Returns error when the column's name is missing.
func (p *parser) parsePartitionColumnElement(elements *partitionElements) error {
	columnName, err := p.requireIdentifierOrKeyword()
	if err != nil {
		return err
	}
	if p.MatchKeyword(keywordWITH) {
		p.MatchKeyword("OPTIONS")
	}
	depth := 0
	for !p.AtEnd() && (depth > 0 || (p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen)) {
		switch {
		case p.Current().Kind == tokenLeftParen:
			depth++
		case p.Current().Kind == tokenRightParen:
			depth--
		case depth == 0 && p.IsKeyword(keywordNOT) && p.Peek().IsKeyword(tokenIdentifier, keywordNULL):
			elements.notNullColumns = append(elements.notNullColumns, columnName)
		case depth == 0 && p.IsKeyword(keywordPRIMARY) && p.Peek().IsKeyword(tokenIdentifier, keywordKEY):
			elements.primaryKey = append(elements.primaryKey, columnName)
		}
		p.Advance()
	}
	return nil
}
