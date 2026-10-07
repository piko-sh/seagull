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

package check

import (
	"context"
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

// returningSupportPass reports a statement with a RETURNING clause the dialect does not
// accept on that kind of statement (Q052).
//
// The dialect's parser records the clause in RawQueryAnalysis.HasReturning, and the kind
// of statement in RawQueryAnalysis.DataChange, whatever the database accepts;
// Info.Returning, the set of statements that accept the clause, is the capability the
// pass judges them against. A statement whose kind the dialect did not record is accepted
// when the dialect supports RETURNING on any statement. The pass never looks at the
// dialect's name, so a dialect option that changes the set decides the outcome through
// Info alone.
type returningSupportPass struct {
	// dialect holds what the pass reads of the dialect, whose Info decides the outcome; nil
	// when there is no dialect, and the pass then reports nothing.
	dialect *dialectFacts
}

// Analyse reports a RETURNING clause the dialect cannot run.
//
// Takes subject (*DiagnosticContext) which holds the query and its raw analysis. A nil
// RawAnalysis is tolerated so the pass can run in isolation; it then reports nothing.
//
// Returns []diag.SourceError which holds at most one error diagnostic, pointing at the
// RETURNING keyword when the dialect recorded its location and at the query otherwise.
func (p *returningSupportPass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	if p.dialect == nil || subject.RawAnalysis == nil || subject.Query == nil {
		return nil
	}
	if !subject.RawAnalysis.HasReturning {
		return nil
	}
	info := &p.dialect.info
	if returningAccepted(info.Returning, subject.RawAnalysis.DataChange) {
		return nil
	}
	span := returningClauseSpan(subject.Query, info.Lexical, subject.RawAnalysis.ReturningLocation)
	return []diag.SourceError{
		diagbuild.Located(subject.Filename, span, diag.CodeReturningNotSupported, diag.SeverityError,
			returningMessage(subject.Query.Name, info.Name, info.Returning, subject.RawAnalysis.DataChange), ""),
	}
}

// returningAccepted reports whether a dialect accepts a RETURNING clause on a statement.
//
// Takes supported (ast.DataChange) which is the set of statements the dialect accepts the
// clause on.
// Takes change (ast.DataChange) which is the statement's kind, or zero when the dialect
// did not record it.
//
// Returns bool which is true when the set includes the statement, or when the kind is
// unknown and the set is not empty.
func returningAccepted(supported ast.DataChange, change ast.DataChange) bool {
	if change == 0 {
		return supported != 0
	}
	return supported.Includes(change)
}

// returningMessage describes a RETURNING clause unsupported by the dialect or statement
// kind.
//
// Takes queryName (string) which names the query.
// Takes dialectName (string) which names the dialect.
// Takes supported (ast.DataChange) which is the set of statements the dialect accepts the
// clause on.
// Takes change (ast.DataChange) which is the statement's kind.
//
// Returns string which is the message.
func returningMessage(queryName string, dialectName string, supported ast.DataChange, change ast.DataChange) string {
	switch {
	case change == 0:
		return fmt.Sprintf("query %q has a RETURNING clause, but the %s dialect does not support RETURNING", queryName, dialectName)
	case supported == 0:
		return fmt.Sprintf("query %q has a RETURNING clause on its %s, but the %s dialect does not support RETURNING on any statement",
			queryName, change, dialectName)
	default:
		return fmt.Sprintf("query %q has a RETURNING clause, but the %s dialect does not support RETURNING on %s",
			queryName, dialectName, change)
	}
}

// returningClauseSpan turns the byte offset of a RETURNING keyword in the query's SQL
// into a span over the keyword, read with the dialect's lexical rules. The query's SQL is
// the text the dialect parsed and starts on the query's line, so the offset maps onto the
// file directly.
//
// Takes query (*sqlmodel.AnalysedQuery) which holds the SQL and the line it starts on.
// Takes rules (sqllex.Rules) which is how the dialect writes SQL.
// Takes location (int) which is the keyword's byte offset, or zero when the dialect did
// not record it.
//
// Returns diagbuild.TextSpan which covers the keyword, or is the query's first column
// when the location is missing or outside the SQL; a zero end means a single position.
func returningClauseSpan(query *sqlmodel.AnalysedQuery, rules sqllex.Rules, location int) diagbuild.TextSpan {
	if location <= 0 || location >= len(query.SQL) {
		return diagbuild.TextSpan{Line: query.Line, Column: 1, EndLine: 0, EndColumn: 0}
	}
	end := location
	if token, ok := sqllex.NewScanner(rules, query.SQL[location:]).Next(); ok && token.Kind == sqllex.KindWord {
		end = location + token.End
	}
	span := diagbuild.SpanOf(query.SQL, query.Line, location, end)
	if end == location {
		return diagbuild.TextSpan{Line: span.Line, Column: span.Column, EndLine: 0, EndColumn: 0}
	}
	return span
}
