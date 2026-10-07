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

package clickhouse

import (
	"seagull.sh/seagull/internal/dialect/ast"
)

// parseExplain handles `EXPLAIN [AST | PLAN | PIPELINE | ESTIMATE | SYNTAX] [settings
// ...] <statement>`.
//
// The body is consumed opaquely because EXPLAIN is read-only and the catalogue does not
// record it.
//
// Returns *ast.CatalogueMutation which is always nil because EXPLAIN is read-only.
// Returns error which is always nil.
func (p *parser) parseExplain() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("EXPLAIN"); err != nil {
		return nil, err
	}
	p.consumeRemainder()
	return nil, nil
}

// parseDescribeTable handles `DESCRIBE [TABLE] [db.]name` / `DESC [TABLE] [db.]name`.
//
// Read-only introspection with no catalogue effect.
//
// Returns *ast.CatalogueMutation which is always nil because DESCRIBE is read-only.
// Returns error when the DESCRIBE or DESC keyword is absent.
func (p *parser) parseDescribeTable() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("DESCRIBE", "DESC"); err != nil {
		return nil, err
	}
	p.consumeRemainder()
	return nil, nil
}

// parseCheckTable handles `CHECK TABLE [db.]name [PARTITION expr | PART 'name']`.
//
// Read-only data-integrity verification.
//
// Returns *ast.CatalogueMutation which is always nil because CHECK is read-only.
// Returns error which is always nil.
func (p *parser) parseCheckTable() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence("CHECK", "TABLE"); err != nil {
		return nil, err
	}
	p.consumeRemainder()
	return nil, nil
}

// parseBackup handles `BACKUP {TABLE | DATABASE | DICTIONARY} name [, ...] TO destination
// [SETTINGS ...]`.
//
// Returns *ast.CatalogueMutation which carries the statement body under EngineSpecific.
// Returns error which is always nil.
func (p *parser) parseBackup() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("BACKUP"); err != nil {
		return nil, err
	}
	body := p.consumeRemainderAsText()
	return ast.NewCatalogueMutation(ast.MutationBackup, "", "", ast.WithEngineSpecific(map[string]string{
		engineKeyStatementBody: body,
	})), nil
}

// parseRestore handles `RESTORE {TABLE | DATABASE | DICTIONARY} name [, ...] FROM source
// [SETTINGS ...]`.
//
// Returns *ast.CatalogueMutation which carries the statement body under EngineSpecific.
// Returns error which is always nil.
func (p *parser) parseRestore() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("RESTORE"); err != nil {
		return nil, err
	}
	body := p.consumeRemainderAsText()
	return ast.NewCatalogueMutation(ast.MutationRestore, "", "", ast.WithEngineSpecific(map[string]string{
		engineKeyStatementBody: body,
	})), nil
}

// parseKillQuery handles `KILL QUERY [ON CLUSTER c] WHERE predicate [SYNC | ASYNC |
// TEST]`.
//
// Returns *ast.CatalogueMutation which carries the statement body under EngineSpecific.
// Returns error which is always nil.
func (p *parser) parseKillQuery() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence("KILL", "QUERY"); err != nil {
		return nil, err
	}
	body := p.consumeRemainderAsText()
	return ast.NewCatalogueMutation(ast.MutationKillQuery, "", "", ast.WithEngineSpecific(map[string]string{
		engineKeyStatementBody: body,
	})), nil
}

// parseKillMutation handles `KILL MUTATION [ON CLUSTER c] WHERE predicate [SYNC | ASYNC |
// TEST]`.
//
// Returns *ast.CatalogueMutation which carries the statement body under EngineSpecific.
// Returns error which is always nil.
func (p *parser) parseKillMutation() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence("KILL", "MUTATION"); err != nil {
		return nil, err
	}
	body := p.consumeRemainderAsText()
	return ast.NewCatalogueMutation(ast.MutationKillMutation, "", "", ast.WithEngineSpecific(map[string]string{
		engineKeyStatementBody: body,
	})), nil
}

// parseAttachTable handles `ATTACH {TABLE | VIEW | DICTIONARY | DATABASE} [IF NOT EXISTS]
// [db.]name [ON CLUSTER c] [...]`.
//
// The object kind keyword is required because classifyAttachStatement only routes here
// when one of the recognised object keywords is present; bare `ATTACH name` is rejected
// upstream.
//
// Returns *ast.CatalogueMutation which describes the attach and carries the statement
// body under EngineSpecific.
// Returns error when the object kind keyword or the qualified name fails to parse.
func (p *parser) parseAttachTable() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordAttach); err != nil {
		return nil, err
	}
	if _, err := p.ExpectKeyword(objectKindTable, objectKindView, objectKindDictionary, objectKindDatabase); err != nil {
		return nil, err
	}
	p.matchIfNotExists()
	database, name, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	return ast.NewCatalogueMutation(ast.MutationAttachTable, database, name, ast.WithEngineSpecific(map[string]string{
		engineKeyStatementBody: p.consumeRemainderAsText(),
	})), nil
}

// parseDetachTable handles `DETACH {TABLE | VIEW | DICTIONARY | DATABASE} [IF EXISTS]
// [db.]name [ON CLUSTER c] [PERMANENTLY] [SYNC]`.
//
// The object kind keyword is required because classifyDetachStatement only routes here
// when one of the recognised object keywords is present; bare `DETACH name` is rejected
// upstream.
//
// Returns *ast.CatalogueMutation which describes the detach and carries the statement
// body under EngineSpecific.
// Returns error when the object kind keyword or the qualified name fails to parse.
func (p *parser) parseDetachTable() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordDetach); err != nil {
		return nil, err
	}
	if _, err := p.ExpectKeyword(objectKindTable, objectKindView, objectKindDictionary, objectKindDatabase); err != nil {
		return nil, err
	}
	ifExists := p.matchIfExists()
	database, name, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	return ast.NewCatalogueMutation(ast.MutationDetachTable, database, name, ast.WithIfExists(ifExists),
		ast.WithEngineSpecific(map[string]string{engineKeyStatementBody: p.consumeRemainderAsText()})), nil
}
