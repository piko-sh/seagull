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
	"errors"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// analyseViewBody analyses the query body of a CREATE VIEW so the catalogue can store
// typed columns, naming them by the declared column list when the view has one.
//
// Takes columnNames ([]string) which is the declared view column list overlaid onto the
// inferred projection when non-empty.
//
// Returns *ast.RawQueryAnalysis which holds the analysed view body, or nil when the body
// is not analysed; the catalogue then falls back to the bare column-name list.
// Returns *ast.Warning which says why the body was not analysed, or nil.
// Returns error when the body is missing or malformed.
func (p *parser) analyseViewBody(columnNames []string) (*ast.RawQueryAnalysis, *ast.Warning, error) {
	viewAnalysis, unanalysed, err := p.analyseQueryBody("the view's query")
	if viewAnalysis == nil {
		return nil, unanalysed, err
	}

	if len(columnNames) > 0 && !slices.ContainsFunc(viewAnalysis.OutputColumns, dialectkit.IsStarColumn) {
		dialectkit.OverlayViewColumnNames(viewAnalysis, columnNames, dialectkit.UndeclaredColumnsDropped)
	}

	return viewAnalysis, nil, nil
}

// analyseQueryBody analyses the query that runs to the end of the statement, the body of
// a view, a CREATE TABLE ... AS or a table macro, in a parser with its own statement
// state.
//
// A body Seagull cannot analyse, one whose columns depend on the data, one that nests
// deeper than the parse depth limit or one that makes more pivot columns than the limit,
// is not analysed, and the warning returned says so, so the statement still applies. A
// malformed body fails the statement, and so does a walk that is interrupted.
//
// Takes what (string) which names the body in the warning, such as "the view's query".
//
// Returns *ast.RawQueryAnalysis which describes the body, or nil when it is not analysed.
// Returns *ast.Warning which says why the body was not analysed, or nil.
// Returns error when the body is missing or malformed.
func (p *parser) analyseQueryBody(what string) (*ast.RawQueryAnalysis, *ast.Warning, error) {
	tokens := p.Tokens[p.Position:]
	if len(tokens) == 0 {
		return nil, nil, p.syntaxErrorAt("expected " + what)
	}
	p.Position = len(p.Tokens)

	analysis, err := p.analyseIsolatedViewBody(tokens)
	if err == nil {
		return analysis, nil, nil
	}
	if !errors.Is(err, errUnanalysable) && !errors.Is(err, dialectkit.ErrParseDepth) && !errors.Is(err, errPivotColumnLimit) {
		return nil, nil, err
	}
	return nil, &ast.Warning{
		Message: what + " was not analysed, so its columns are not typed: " + err.Error(),
		Span:    dialectkit.TokenRangeSpan(tokens, 0, len(tokens)),
	}, nil
}

// analyseIsolatedViewBody analyses a view or table macro body in a child parser with its
// own statement state, requiring the body to end where its tokens do.
//
// Takes tokens ([]token) which are the body's tokens.
//
// Returns *ast.RawQueryAnalysis which describes the body.
// Returns error when the body is malformed or has tokens left over: the first problem
// found in it.
func (p *parser) analyseIsolatedViewBody(tokens []token) (*ast.RawQueryAnalysis, error) {
	viewParser := p.newChildParser(tokens)
	viewParser.statement = newStatementState(tokens)
	viewParser.namedParameterMap = make(map[string]int)
	_, analysis, err := viewParser.analyseNestedQuery(tokens)
	if syntaxErr := viewParser.syntaxError(); syntaxErr != nil {
		return nil, syntaxErr
	}
	if err != nil {
		return nil, err
	}
	resolveStructFieldReferences(analysis, nil, nil)
	return analysis, nil
}

// withUnanalysedBody records a statement's query body as not analysed when a warning says
// why.
//
// Takes unanalysed (*ast.Warning) which says why the body was not analysed, or nil when
// it was.
//
// Returns ast.CatalogueMutationOption which records the warning, or does nothing for nil.
func withUnanalysedBody(unanalysed *ast.Warning) ast.CatalogueMutationOption {
	return func(mutation *ast.CatalogueMutation) {
		if unanalysed != nil {
			ast.WithUnanalysedPart(*unanalysed)(mutation)
		}
	}
}
