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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// searchPathSetting is the name of the setting that holds the search path.
	searchPathSetting = "search_path"

	// currentUserSchema is the search path entry that stands for the schema named after the
	// session's user, which the migrations cannot know and which the database skips when no
	// such schema exists.
	currentUserSchema = "$user"
)

// parseSearchPath parses a statement that sets or restores the search path: SET [SESSION
// | LOCAL] search_path {TO | =} {schema [, ...] | DEFAULT}, SET [SESSION | LOCAL] SCHEMA
// 'schema', RESET search_path or RESET ALL.
//
// A schema written as a string keeps its case and a bare one folds as a name does;
// "$user", which names the session's user's schema, is left out. SET search_path FROM
// CURRENT changes nothing.
//
// Returns *ast.CatalogueMutation which sets or restores the search path, or nil when the
// statement changes nothing.
// Returns error when the statement is malformed.
func (p *parser) parseSearchPath() (*ast.CatalogueMutation, error) {
	if p.MatchKeyword("RESET") {
		p.Advance()
		return ast.NewCatalogueMutation(ast.MutationResetSearchPath, "", ""), nil
	}
	if err := p.requireKeyword(keywordSET); err != nil {
		return nil, err
	}
	local := p.MatchKeyword("LOCAL")
	p.MatchKeyword("SESSION")
	if p.MatchKeyword(keywordSCHEMA) {
		return p.searchPathOf(local)
	}
	p.Advance()
	if p.MatchKeyword(keywordFROM) {
		return nil, p.requireKeyword("CURRENT")
	}
	switch {
	case p.MatchKeyword("TO"):
	case p.Current().Kind == tokenOperator && p.Current().Value == "=":
		p.Advance()
	default:
		return nil, p.syntaxError("expected TO or = after search_path")
	}
	if p.MatchKeyword(keywordDEFAULT) {
		mutation := ast.NewCatalogueMutation(ast.MutationResetSearchPath, "", "")
		mutation.Local = local
		return mutation, p.requireEnd()
	}
	return p.searchPathOf(local)
}

// searchPathOf parses the comma-separated schemas a SET gives the search path.
//
// Takes local (bool) which is true for a SET LOCAL.
//
// Returns *ast.CatalogueMutation which sets the search path.
// Returns error when a schema is missing or the statement does not end after the list.
func (p *parser) searchPathOf(local bool) (*ast.CatalogueMutation, error) {
	var schemas []string
	for {
		schema, err := p.requireIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		if schema != currentUserSchema && schema != "" {
			schemas = append(schemas, schema)
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	mutation := ast.NewCatalogueMutation(ast.MutationSetSearchPath, "", "")
	mutation.SearchPath = schemas
	mutation.Local = local
	return mutation, p.requireEnd()
}

// classifySetStatement returns the kind of a SET statement: one that sets the search
// path, as SET [SESSION | LOCAL] search_path or SET [SESSION | LOCAL] SCHEMA does, or an
// unknown statement for any other setting, which the catalogue does not model.
//
// Takes tokens ([]token) which is the statement's token stream.
//
// Returns statementKind which is statementKindSetSearchPath or statementKindUnknown.
func classifySetStatement(tokens []token) statementKind {
	index := 1
	if index < len(tokens) && (tokens[index].IsKeyword(tokenIdentifier, "SESSION") || tokens[index].IsKeyword(tokenIdentifier, "LOCAL")) {
		index++
	}
	if index < len(tokens) && (strings.EqualFold(tokens[index].Value, searchPathSetting) || tokens[index].IsKeyword(tokenIdentifier, keywordSCHEMA)) {
		return statementKindSetSearchPath
	}
	return statementKindUnknown
}

// classifyResetStatement returns the kind of a RESET statement: one that restores the
// search path, as RESET search_path and RESET ALL do, or an unknown statement for any
// other setting.
//
// Takes tokens ([]token) which is the statement's token stream.
//
// Returns statementKind which is statementKindSetSearchPath or statementKindUnknown.
func classifyResetStatement(tokens []token) statementKind {
	if len(tokens) > 1 && (strings.EqualFold(tokens[1].Value, searchPathSetting) || tokens[1].IsKeyword(tokenIdentifier, "ALL")) {
		return statementKindSetSearchPath
	}
	return statementKindUnknown
}
