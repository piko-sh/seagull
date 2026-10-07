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

// commandOutputPass validates that the query command is consistent with the output
// columns. SELECT-style commands (one, many, stream) must produce columns; exec-style
// commands must not, unless every column is of the dialect's void type.
type commandOutputPass struct {
	// dialect holds what the pass reads of the dialect, its void type and lexical rules; nil
	// when there is no dialect.
	dialect *dialectFacts
}

// Analyse checks command and output column consistency.
//
// Takes subject (*DiagnosticContext) which holds the query and analysis state.
//
// Returns []diag.SourceError which holds any diagnostics for command/output mismatches.
func (p *commandOutputPass) Analyse(_ context.Context, subject *DiagnosticContext) []diag.SourceError {
	var diagnostics []diag.SourceError

	switch subject.Query.Command {
	case sqlmodel.QueryCommandOne, sqlmodel.QueryCommandMany, sqlmodel.QueryCommandStream:
		if len(subject.Query.OutputColumns) == 0 {
			diagnostics = append(diagnostics, diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeCommandOutputMismatch,
				diag.SeverityWarning, fmt.Sprintf(
					"query %q uses command %q but produces no output columns",
					subject.Query.Name, subject.Query.Command,
				)))
		}

	case sqlmodel.QueryCommandExec, sqlmodel.QueryCommandExecResult, sqlmodel.QueryCommandExecRows,
		sqlmodel.QueryCommandAsyncExec:
		if len(subject.Query.OutputColumns) > 0 && !returnsOnlyVoid(subject, p.dialect) {
			diagnostics = append(diagnostics, diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeCommandOutputMismatch,
				diag.SeverityWarning, fmt.Sprintf(
					"query %q uses command %q but produces %d output columns",
					subject.Query.Name, subject.Query.Command, len(subject.Query.OutputColumns),
				)))
		}

	default:
	}

	if diagnostic := discardedRowsDiagnostic(subject, p.dialect); diagnostic != nil {
		diagnostics = append(diagnostics, *diagnostic)
	}
	if diagnostic := conflictDoNothingReturningDiagnostic(subject, p.dialect); diagnostic != nil {
		diagnostics = append(diagnostics, *diagnostic)
	}
	if diagnostic := optionalMisuseDiagnostic(subject); diagnostic != nil {
		diagnostics = append(diagnostics, *diagnostic)
	}

	return diagnostics
}

// discardedRowsDiagnostic warns about a batch or copyfrom query whose statement returns
// rows, a SELECT or a statement with RETURNING: the generated method returns only an
// error, so the rows are discarded.
//
// Takes subject (*DiagnosticContext) which holds the query.
// Takes facts (*dialectFacts) which gives the dialect's void type, or nil.
//
// Returns *diag.SourceError which is the Q009 warning, or nil when the query's command
// returns no rows or its statement returns none.
func discardedRowsDiagnostic(subject *DiagnosticContext, facts *dialectFacts) *diag.SourceError {
	command := subject.Query.Command
	if command != sqlmodel.QueryCommandBatch && command != sqlmodel.QueryCommandCopyFrom {
		return nil
	}
	if len(subject.Query.OutputColumns) == 0 || returnsOnlyVoid(subject, facts) {
		return nil
	}
	return new(diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeCommandOutputMismatch, diag.SeverityWarning,
		fmt.Sprintf(
			"query %q uses command %q, whose method returns no rows, but its statement returns %d output columns, which "+
				"are discarded; run a query that reads rows with one or many for each item, or leave RETURNING out",
			subject.Query.Name, command, len(subject.Query.OutputColumns),
		)))
}

// returnsOnlyVoid reports whether every output column of a query is of the dialect's void
// type, as SELECT pg_advisory_xact_lock($1) is: the statement returns nothing to read, so
// an exec command fits it.
//
// Takes subject (*DiagnosticContext) which holds the query.
// Takes facts (*dialectFacts) which gives the dialect's void type, or nil.
//
// Returns bool which is true when the dialect has a void type and every column is of it.
func returnsOnlyVoid(subject *DiagnosticContext, facts *dialectFacts) bool {
	if facts == nil {
		return false
	}
	void := facts.void
	if void.EngineName == "" {
		return false
	}
	for index := range subject.Query.OutputColumns {
		columnType := subject.Query.OutputColumns[index].SQLType
		if columnType.Category != void.Category || columnType.EngineName != void.EngineName {
			return false
		}
	}
	return true
}

// conflictDoNothingReturningDiagnostic warns (Q046) when a command:one query pairs ON
// CONFLICT DO NOTHING with RETURNING, because a conflict skips the insert and the scan
// returns the driver no-rows sentinel. It returns nil when optional: true handles the
// zero row, or when the statement does not skip conflicting rows.
//
// Takes subject (*DiagnosticContext) which holds the query and raw analysis.
// Takes facts (*dialectFacts) which gives the dialect's lexical rules, or nil.
//
// Returns *diag.SourceError which is the warning, or nil when it does not apply.
func conflictDoNothingReturningDiagnostic(subject *DiagnosticContext, facts *dialectFacts) *diag.SourceError {
	if subject.Query.Command != sqlmodel.QueryCommandOne || subject.Query.Optional ||
		subject.RawAnalysis == nil || !subject.RawAnalysis.HasReturning || !skipsConflictingRows(subject, facts) {
		return nil
	}

	return new(diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeConflictDoNothingReturning, diag.SeverityWarning,
		fmt.Sprintf(
			"query %q uses command %q with ON CONFLICT DO NOTHING and RETURNING; a conflict skips the "+
				"insert so RETURNING yields no row and the call returns the driver no-rows sentinel "+
				"(sql.ErrNoRows, or pgx.ErrNoRows for native pgx). Add optional: true to return "+
				"(row, false, nil), use command \"exec\"/\"execrows\", or handle errors.Is with that sentinel.",
			subject.Query.Name, subject.Query.Command,
		)))
}

// skipsConflictingRows reports whether a statement skips a row that conflicts with an
// existing one, as ON CONFLICT DO NOTHING does: as the dialect recorded it on the
// analysis, or, when it recorded nothing, as the statement's code says, its comments and
// string literals left out.
//
// Takes subject (*DiagnosticContext) which holds the query and its raw analysis.
// Takes facts (*dialectFacts) which gives the dialect's lexical rules, or nil.
//
// Returns bool which is true when a conflicting row is skipped.
func skipsConflictingRows(subject *DiagnosticContext, facts *dialectFacts) bool {
	switch subject.RawAnalysis.ConflictAction {
	case ast.ConflictActionDoNothing:
		return true
	case ast.ConflictActionUnrecorded:
		var rules sqllex.Rules
		if facts != nil {
			rules = facts.info.Lexical
		}
		return codeSaysDoNothingOnConflict(rules, subject.Query.SQL)
	default:
		return false
	}
}

// optionalMisuseDiagnostic reports (Q047, error) when optional: true is misplaced. It
// returns nil when optional is unset or valid.
//
// Takes subject (*DiagnosticContext) which holds the query.
//
// Returns *diag.SourceError which is the error, or nil when it does not apply.
func optionalMisuseDiagnostic(subject *DiagnosticContext) *diag.SourceError {
	reason := optionalMisuseReason(subject.Query)
	if reason == "" {
		return nil
	}

	return new(diagbuild.Block(subject.Filename, subject.Query.Line, diag.CodeOptionalNonOneCommand, diag.SeverityError,
		fmt.Sprintf(
			"query %q sets optional: true but %s. Remove optional or use a static command:one.",
			subject.Query.Name, reason,
		)))
}

// optionalMisuseReason returns a reason when optional: true is set where the flag has no
// meaning, or an empty string when optional is unset or valid.
//
// The flag applies only to a static command:one, where a zero row becomes (row, false,
// nil); any other command, a runtime builder, or a query requiring dynamic parameter
// preparation is rejected.
//
// Takes query (*sqlmodel.AnalysedQuery) whose command and dynamic flags are inspected.
//
// Returns string which is the misuse reason, or empty when optional is unset or valid.
func optionalMisuseReason(query *sqlmodel.AnalysedQuery) string {
	if !query.Optional {
		return ""
	}

	switch {
	case query.Command != sqlmodel.QueryCommandOne:
		return fmt.Sprintf("optional applies only to command:one, not command %q", query.Command)
	case query.DynamicRuntime:
		return "optional is not supported with dynamic: runtime; the builder's One() terminal already " +
			"returns the no-rows sentinel"
	case query.IsDynamic:
		return "optional is not supported on a query with optional predicate parameters"
	default:
		return ""
	}
}

// codeSaysDoNothingOnConflict reports whether SQL holds the words ON CONFLICT followed,
// later, by DO NOTHING, each pair written next to each other, reading only its code:
// whitespace, comments and string literals are passed over, and a word inside a quoted
// identifier is no keyword.
//
// Takes rules (sqllex.Rules) which is how the dialect writes SQL.
// Takes sql (string) which is the query's SQL.
//
// Returns bool which is true when the code holds ON CONFLICT ... DO NOTHING; the text
// read before a lexical error is all that counts.
func codeSaysDoNothingOnConflict(rules sqllex.Rules, sql string) bool {
	scanner := sqllex.NewScanner(rules, sql)
	var previous sqllex.Token
	conflict := false
	for {
		token, ok := scanner.NextSignificant()
		if !ok {
			return false
		}
		switch {
		case !conflict && previous.IsWord("ON") && token.IsWord("CONFLICT"):
			conflict = true
		case conflict && previous.IsWord("DO") && token.IsWord("NOTHING"):
			return true
		}
		previous = token
	}
}
