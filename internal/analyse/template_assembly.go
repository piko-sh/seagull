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

package analyse

import (
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/sqltemplate"
)

// buildTemplates fills a query's template, count template and insert target from the
// spans of its statement, and turns the problems found on the way into diagnostics.
//
// Takes query (*sqlmodel.AnalysedQuery) which receives the templates.
// Takes input (*assembleQueryInput) which holds the block, its statements, the
// statement's analysis and where the output columns came from.
//
// Returns assembly which says whether the count wraps the query and holds the
// diagnostics.
func (a *QueryAnalyser) buildTemplates(query *sqlmodel.AnalysedQuery, input *assembleQueryInput) assembly {
	built, problems := sqltemplate.Build(&sqltemplate.Input{
		Info:       &a.info,
		Query:      query,
		Analysis:   input.rawAnalysis,
		SQL:        input.block.SQL,
		Tokens:     input.block.StrictTokens(),
		Statements: input.location.statements,
		Kept:       a.keptRanges(input.block, &a.info),
		Origins:    input.origins,
		Referable:  input.referable,
		NamedItems: input.namedItems,
		Aggregated: input.aggregated,
	})
	query.Template = built.Template
	query.CountTemplate = built.CountTemplate
	query.InsertTarget = built.InsertTarget
	return assembly{diagnostics: input.location.templateDiagnostics(problems), countWrapped: built.CountWrapped}
}

// keptRanges returns the ranges of a block's SQL its constant keeps once its directive
// lines are left out.
//
// Takes block (sqlfile.QueryBlock) which holds the text and its tokens.
// Takes info (*dialect.Info) which gives the dialect's lexical rules and directive
// comment prefix.
//
// Returns []ast.Span which holds the kept ranges, in order.
func (a *QueryAnalyser) keptRanges(block sqlfile.QueryBlock, info *dialect.Info) []ast.Span {
	lexicon := syntax.NeutralLexicon()
	if info.Comments.LinePrefix != "" {
		lexicon = syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	}
	var ranges []syntax.Range
	if info.Comments.LinePrefix != "" {
		ranges = syntax.StripTokenRanges(block.SQL, lexicon, a.prefixes, block.Tokens())
	} else {
		ranges = syntax.StripRanges(block.SQL, lexicon, a.prefixes)
	}
	kept := make([]ast.Span, 0, len(ranges))
	for _, keptRange := range ranges {
		kept = append(kept, ast.Span{Start: keptRange.Start, End: keptRange.End})
	}
	return kept
}

// itemSpans returns where the expression of each output item of the analysed statement is
// written, parallel to its raw output columns: the RETURNING items when it has them, and
// the select list's otherwise.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the statement's analysis.
//
// Returns []resolve.Span which holds the spans as offsets into the block's text, or nil
// when the dialect recorded no items that match the output columns.
func (location statementLocation) itemSpans(analysis *ast.RawQueryAnalysis) []resolve.Span {
	items := analysis.Spans.ReturningItems
	if len(items) == 0 {
		items = analysis.Spans.ProjectionItems
	}
	if len(items) == 0 || len(items) != len(analysis.OutputColumns) {
		return nil
	}
	spans := make([]resolve.Span, len(items))
	for index, item := range items {
		if item.Expression.Known() {
			spans[index] = resolve.Span{Start: location.offset + item.Expression.Start, End: location.offset + item.Expression.End}
		}
	}
	return spans
}

// templateDiagnostics turns the problems building a query's templates found into
// diagnostics, each placed as place places it.
//
// Takes problems ([]sqltemplate.Problem) which are the problems.
//
// Returns []diag.SourceError which holds one diagnostic per problem.
func (location statementLocation) templateDiagnostics(problems []sqltemplate.Problem) []diag.SourceError {
	if len(problems) == 0 {
		return nil
	}
	diagnostics := make([]diag.SourceError, 0, len(problems))
	for index := range problems {
		problem := &problems[index]
		diagnostics = append(diagnostics, location.place(index, problem.Span, problem.Code, problem.Severity, problem.Message, ""))
	}
	return diagnostics
}

// parseFailure reports a statement the dialect could not parse or analyse: at the place
// the dialect's error names, when it names one that is where it says in the block's text,
// and at the query's header otherwise, as diagbuild.Failure places it.
//
// Takes err (error) which is the dialect's error.
// Takes context (string) which says what failed, such as `failed to parse query "Get"`.
//
// Returns diag.SourceError which is the Q010 diagnostic.
func (location statementLocation) parseFailure(err error, context string) diag.SourceError {
	return diagbuild.Failure(location.filename, location.sql, location.startLine, context, err)
}
