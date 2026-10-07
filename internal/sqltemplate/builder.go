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

package sqltemplate

import (
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Input is what Build reads about one query.
type Input struct {
	// Info describes the dialect: its placeholder forms and lexical rules.
	Info *dialect.Info

	// Query is the assembled query, whose parameters, output columns and dynamic flags the
	// template refers to. Build does not change it.
	Query *sqlmodel.AnalysedQuery

	// Analysis is the dialect's analysis of the query's own statement, whose spans locate
	// its parts.
	Analysis *ast.RawQueryAnalysis

	// SQL is the query block's text, which the statements and the kept ranges index.
	SQL string

	// Tokens optionally holds a successful scan of SQL; Build treats it as immutable.
	Tokens []sqllex.Token

	// Statements lists the block's statements in order; the last is the query's own.
	Statements []ast.ParsedStatement

	// Kept lists, in order, the ranges of SQL the query's SQL keeps once its directive lines
	// are left out, as syntax.StripRanges gives them.
	Kept []ast.Span

	// Origins holds, parallel to Query.OutputColumns, the index of the select-list or
	// RETURNING item each column came from, or is nil when the analyser could not tell.
	Origins []int

	// Referable parallels Query.OutputColumns and marks star columns that can be safely
	// referenced by name. A star containing an unreferable column remains intact unless a
	// column is wrapped; nil treats every star this way.
	Referable []bool

	// NamedItems lists the select-list items a later item of the same select list names by
	// their alias, in a dialect with lateral column aliases. Their columns are written as
	// they are, never wrapped, since a wrap would change what the later item reads.
	NamedItems []int

	// Aggregated reports a select-list item that holds an aggregate or a window function
	// outside any subquery.
	Aggregated bool
}

// Built is what Build made of a query.
type Built struct {
	// CountTemplate counts the rows a runtime builder query returns, or is nil when counting
	// does not apply.
	CountTemplate *sqlmodel.SQLTemplate

	// InsertTarget is the table an INSERT writes, its schema as written, and is zero for any
	// other statement.
	InsertTarget sqlmodel.TableName

	// Template is the query's executable SQL.
	Template sqlmodel.SQLTemplate

	// CountWrapped reports a count template that counts the query's rows by wrapping it in a
	// subquery, because its rows are not the rows it reads.
	CountWrapped bool
}

// Problem is something Build found wrong with a query.
type Problem struct {
	// Code is the diagnostic code.
	Code string

	// Message describes the problem.
	Message string

	// Span locates the problem in Input.SQL, or is zero when it concerns the whole query.
	Span ast.Span

	// Severity is how serious the problem is.
	Severity diag.ErrorSeverity
}

// builder is the progress of one Build call.
type builder struct {
	// input is what Build was given.
	input *Input

	// spans are the statement's spans.
	spans *ast.StatementSpans

	// occurrences holds the statement's placeholders, each matched to its parameter.
	occurrences []occurrence

	// tokens holds the statement's significant tokens, with offsets into Input.SQL.
	tokens []sqllex.Token

	// problems gathers the problems found so far.
	problems []Problem

	// statement is the query's own statement.
	statement ast.ParsedStatement
}

// newBuilder starts a Build call.
//
// Takes input (*Input) which describes the query.
//
// Returns *builder which is ready to build.
func newBuilder(input *Input) *builder {
	statement := input.Statements[len(input.Statements)-1]
	created := &builder{
		input:       input,
		spans:       &input.Analysis.Spans,
		occurrences: nil,
		tokens:      nil,
		problems:    nil,
		statement:   statement,
	}
	created.tokens = created.significantTokens(statement)
	return created
}

// withArguments makes a template of segments, with the arguments the query executes them
// with.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the template's segments.
//
// Returns sqlmodel.SQLTemplate which is the template.
func (b *builder) withArguments(segments []sqlmodel.SQLSegment) sqlmodel.SQLTemplate {
	template := sqlmodel.SQLTemplate{Segments: coalesceText(segments), Arguments: nil}
	var facts dialect.Facts
	facts.Placeholders = b.input.Info.Placeholders
	template.Arguments = emitkit.ArgumentOrder(template, emitkit.ExecutionForm(&facts, b.input.Query))
	return template
}

// absolute turns a span the dialect recorded relative to the statement into offsets into
// Input.SQL.
//
// Takes span (ast.Span) which is relative to the statement's first byte.
//
// Returns ast.Span which is the span in Input.SQL, or zero when it was not recorded.
func (b *builder) absolute(span ast.Span) ast.Span {
	if !span.Known() {
		return ast.Span{}
	}
	return ast.Span{Start: b.statement.Location + span.Start, End: b.statement.Location + span.End}
}

// statementEnd returns the offset one past the statement's last token.
//
// Returns int which is the offset into Input.SQL.
func (b *builder) statementEnd() int {
	return b.statement.Location + b.statement.Length
}

// kept returns the text of a range of Input.SQL the query keeps, its directive lines left
// out.
//
// Takes start (int) which is the offset of the first byte.
// Takes end (int) which is the offset one past the last.
//
// Returns string which is the kept text.
func (b *builder) kept(start, end int) string {
	var builder strings.Builder
	for _, kept := range b.input.Kept {
		from, to := max(start, kept.Start), min(end, kept.End)
		if from < to {
			if len(b.input.Kept) == 1 {
				return b.input.SQL[from:to]
			}
			builder.WriteString(b.input.SQL[from:to])
		}
	}
	return builder.String()
}

// significantTokens reads a statement's tokens that are not whitespace or comments.
//
// Takes statement (ast.ParsedStatement) which is the statement.
//
// Returns []sqllex.Token which holds the tokens, with offsets into Input.SQL, or nil when
// the statement does not read under the dialect's lexical rules.
func (b *builder) significantTokens(statement ast.ParsedStatement) []sqllex.Token {
	if statement.Location < 0 || statement.Length < 0 || statement.Location+statement.Length > len(b.input.SQL) {
		return nil
	}
	if b.input.Tokens != nil {
		return b.reusedStatementTokens(statement)
	}
	return b.scanStatement(statement)
}

// scanStatement handles callers without reusable tokens and statement boundaries inside
// tokens.
//
// Takes statement (ast.ParsedStatement) which identifies the statement within the
// original SQL.
//
// Returns []sqllex.Token which holds significant tokens in source coordinates, or nil on
// a scan error.
func (b *builder) scanStatement(statement ast.ParsedStatement) []sqllex.Token {
	tokens, err := sqllex.Tokens(b.input.Info.Lexical, b.input.SQL[statement.Location:statement.Location+statement.Length])
	if err != nil {
		return nil
	}
	tokens = slices.DeleteFunc(tokens, func(token sqllex.Token) bool { return token.Kind.IsNoise() })
	for index := range tokens {
		tokens[index].Start += statement.Location
		tokens[index].End += statement.Location
	}
	return tokens
}

// lastTokenEndBefore returns the end of the statement's last token that ends at or before
// an offset: the place a clause that goes before something at that offset is written.
//
// Takes offset (int) which is the offset into Input.SQL.
//
// Returns int which is the end of the token, or the statement's start when no token ends
// there.
func (b *builder) lastTokenEndBefore(offset int) int {
	end := b.statement.Location
	for _, token := range b.tokens {
		if token.End > offset {
			break
		}
		end = token.End
	}
	return end
}

// report records a problem.
//
// Takes code (string) which is the diagnostic code.
// Takes span (ast.Span) which locates it in Input.SQL, or is zero.
// Takes format (string) which is the message's format.
// Takes arguments (...any) which fill the format.
func (b *builder) report(code string, span ast.Span, format string, arguments ...any) {
	b.problems = append(b.problems, Problem{
		Code:     code,
		Message:  fmt.Sprintf(format, arguments...),
		Span:     span,
		Severity: diag.SeverityError,
	})
}

// reusedStatementTokens filters a cached scan, falling back at partial token boundaries.
//
// Takes statement (ast.ParsedStatement) which identifies the statement within the
// original SQL.
//
// Returns []sqllex.Token which holds significant tokens in source coordinates, or nil on
// a scan error.
func (b *builder) reusedStatementTokens(statement ast.ParsedStatement) []sqllex.Token {
	var tokens []sqllex.Token
	end := statement.Location + statement.Length
	for _, token := range b.input.Tokens {
		if token.Kind.IsNoise() || token.End <= statement.Location {
			continue
		}
		if token.Start >= end {
			break
		}
		if token.Start < statement.Location || token.End > end {
			return b.scanStatement(statement)
		}
		tokens = append(tokens, token)
	}
	return tokens
}

// Build constructs SQL templates and insert targets while reporting invalid placeholders.
//
// Unmatched or unlocated placeholders produce CodeInternalNilGuard and prevent a template
// from being built.
//
// Takes input (*Input) which describes the query. Build does not change it.
//
// Returns Built which holds the templates, zero when no template could be made.
// Returns []Problem which holds the problems found.
func Build(input *Input) (Built, []Problem) {
	built := Built{CountTemplate: nil, InsertTarget: insertTarget(input.Analysis), Template: sqlmodel.SQLTemplate{}, CountWrapped: false}
	if input.Analysis == nil || len(input.Statements) == 0 {
		return built, nil
	}
	templateBuilder := newBuilder(input)
	templateBuilder.checkPrecedingStatements()
	if !templateBuilder.matchPlaceholders() {
		return built, templateBuilder.problems
	}
	templateBuilder.checkParameterShapes()
	root, ok := templateBuilder.tree()
	if !ok {
		return built, templateBuilder.problems
	}
	segments, written := templateBuilder.segmentsWithin(root, root.span, nil)
	if !written {
		templateBuilder.report(diag.CodeInternalNilGuard, ast.Span{}, "the template made from the query's spans could not be written")
		return built, templateBuilder.problems
	}
	built.Template = templateBuilder.withArguments(segments)
	if count, wrapped, applies := templateBuilder.countTemplate(root); applies {
		built.CountTemplate = &count
		built.CountWrapped = wrapped
	}
	return built, templateBuilder.problems
}

// insertTarget returns the table an INSERT writes, as the analysis records it.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the statement's analysis, or nil.
//
// Returns sqlmodel.TableName which is the table, zero for any other statement.
func insertTarget(analysis *ast.RawQueryAnalysis) sqlmodel.TableName {
	if analysis == nil {
		return sqlmodel.TableName{}
	}
	return sqlmodel.TableName{Schema: analysis.InsertTarget.Schema, Name: analysis.InsertTarget.Name}
}
