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

package seagull

import (
	"io"

	"seagull.sh/seagull/internal/diag"
)

const (
	// CodeUnknownColumn indicates a column reference that could not be resolved in any table
	// or alias in scope.
	CodeUnknownColumn = diag.CodeUnknownColumn

	// CodeAmbiguousColumn indicates a column name that matches more than one table in the
	// current scope.
	CodeAmbiguousColumn = diag.CodeAmbiguousColumn

	// CodeUnknownTable indicates a table, CTE, or table-valued function that is not present
	// in the catalogue.
	CodeUnknownTable = diag.CodeUnknownTable

	// CodeExpressionTypeError indicates a failure to infer the type of an expression during
	// type resolution.
	CodeExpressionTypeError = diag.CodeExpressionTypeError

	// CodeUnknownFunction indicates a function call that could not be resolved or has no
	// matching overload for the given argument count.
	CodeUnknownFunction = diag.CodeUnknownFunction

	// CodeDuplicateQueryName indicates two queries declare names that match after ignoring
	// letter case and underscores, such as get_user and GetUser.
	CodeDuplicateQueryName = diag.CodeDuplicateQueryName

	// CodeDirectiveSyntax indicates a malformed sgl.* directive in a SQL query file.
	CodeDirectiveSyntax = diag.CodeDirectiveSyntax

	// CodeMissingDirective indicates a required query name or command is absent from a
	// query's directive header.
	CodeMissingDirective = diag.CodeMissingDirective

	// CodeCommandOutputMismatch indicates the declared command does not match the actual
	// query output (e.g. exec with SELECT, or one with INSERT without RETURNING).
	CodeCommandOutputMismatch = diag.CodeCommandOutputMismatch

	// CodeParseError indicates a SQL parse failure, empty query, or DDL interpretation error
	// during catalogue building.
	CodeParseError = diag.CodeParseError

	// CodeSortableColumnMissing indicates a sgl.sortable parameter references a column that
	// is not present in the query output.
	CodeSortableColumnMissing = diag.CodeSortableColumnMissing

	// CodeMultipleStatements indicates a query block contains more than one SQL statement;
	// only the last is analysed, and the query cannot be generated.
	CodeMultipleStatements = diag.CodeMultipleStatements

	// CodeGeneratedColumn indicates a parameter attempts to assign to a generated
	// (non-writable) column.
	CodeGeneratedColumn = diag.CodeGeneratedColumn

	// CodeGroupByColumnMissing indicates a sgl.group_by references a column that is not
	// present in the query output.
	CodeGroupByColumnMissing = diag.CodeGroupByColumnMissing

	// CodeGroupByMissingEmbed indicates sgl.group_by is present but no sgl.embed directive
	// exists on non-key tables.
	CodeGroupByMissingEmbed = diag.CodeGroupByMissingEmbed

	// CodeGroupByWrongCommand indicates sgl.group_by is used with a command other than many.
	CodeGroupByWrongCommand = diag.CodeGroupByWrongCommand

	// CodeSliceBatchCopyFrom indicates a kind: slice parameter was used with the batch or
	// copyfrom command. These commands send one row of parameters per item and cannot expand
	// a slice.
	CodeSliceBatchCopyFrom = diag.CodeSliceBatchCopyFrom

	// CodeSliceDynamicRuntime indicates a kind: slice parameter was used with dynamic:
	// runtime. The runtime query builder constructs WHERE clauses dynamically and cannot
	// expand slice placeholders.
	CodeSliceDynamicRuntime = diag.CodeSliceDynamicRuntime

	// CodeCompoundColumnCount indicates a UNION/INTERSECT/EXCEPT branch has a different
	// number of columns than the primary SELECT.
	CodeCompoundColumnCount = diag.CodeCompoundColumnCount

	// CodeRuntimeBuilderBaseHasWhere is informational.
	//
	// A dynamic: runtime query's static SQL already contains a WHERE clause, so the builder
	// appends additional runtime predicates with " AND " instead of " WHERE ". Emitted so
	// the user knows the merged SQL semantics without having to read the generated code.
	CodeRuntimeBuilderBaseHasWhere = diag.CodeRuntimeBuilderBaseHasWhere

	// CodeCountSemanticsWrapped is informational.
	//
	// The generated <query>CountSQL constant wraps the original SELECT in `SELECT COUNT(*)
	// FROM (<original>) sub` because the query contains GROUP BY, HAVING, DISTINCT, an
	// aggregate or window function, or a compound query. Wrapping makes .Count(ctx) return
	// the outer-result-row cardinality rather than the inner-table cardinality.
	CodeCountSemanticsWrapped = diag.CodeCountSemanticsWrapped

	// CodeCountRewriteUnavailable is a warning.
	//
	// A dynamic: runtime query could not be rewritten into a COUNT query (the shared
	// rewriter did not recognise a top-level SELECT), so the generated builder has no
	// .Count(ctx) support. The query still generates; only the count terminal is
	// unavailable.
	CodeCountRewriteUnavailable = diag.CodeCountRewriteUnavailable

	// CodeUnknownDirective indicates a sgl.X(...) directive whose name is not in the
	// registry. The diagnostic carries a Levenshtein-derived Suggestion when a close
	// neighbour exists in the directive vocabulary.
	CodeUnknownDirective = diag.CodeUnknownDirective

	// CodeUnknownKeywordArgument indicates a keyword argument key inside sgl(...) or
	// sgl.X(...) that the directive's spec does not accept. The diagnostic carries a
	// Suggestion when a close neighbour exists in the directive's keyword argument
	// vocabulary.
	CodeUnknownKeywordArgument = diag.CodeUnknownKeywordArgument

	// CodeInvalidKeywordArgumentValue indicates a keyword argument value that fails the
	// directive's value-shape check (closed enum, integer, bool, list, etc.). The diagnostic
	// carries a Suggestion for the closest allowed value when the keyword argument has a
	// closed AllowedValues set.
	CodeInvalidKeywordArgumentValue = diag.CodeInvalidKeywordArgumentValue

	// CodeDuplicateKeywordArgument indicates the same keyword argument key appears twice in
	// one directive call.
	CodeDuplicateKeywordArgument = diag.CodeDuplicateKeywordArgument

	// CodeInternalNilGuard is a defensive diagnostic for nil columns, derived tables, or
	// compound branches during type resolution. This should not normally fire.
	CodeInternalNilGuard = diag.CodeInternalNilGuard

	// CodeMissingRequired indicates a required positional argument or a required keyword
	// argument is absent from a directive call.
	CodeMissingRequired = diag.CodeMissingRequired

	// CodeUnclosedDirective indicates a multi-line directive call whose paren depth never
	// returns to zero before the directive header terminates.
	CodeUnclosedDirective = diag.CodeUnclosedDirective

	// CodeParameterBindingSyntax indicates a malformed parameter-binding line: the anchor /
	// "as" keyword / "sgl." prefix / role token did not appear in the expected position. The
	// diagnostic may carry a Suggestion when the offending token is a near-miss typo of the
	// expected keyword ("as" / "sgl").
	CodeParameterBindingSyntax = diag.CodeParameterBindingSyntax

	// CodeInvalidListLiteral indicates a bracketed list value where the closing token was
	// not the expected ']' (commonly a stray ')' or missing ']'). The malformed value is
	// rejected so downstream validation does not see partial data.
	CodeInvalidListLiteral = diag.CodeInvalidListLiteral

	// CodeUnterminatedString indicates a string literal whose opening quote was not matched
	// before the end of the directive line or the end of the file. Reported before
	// multi-line continuation kicks in so the user sees the real cause instead of a
	// downstream "unclosed directive" diagnostic.
	CodeUnterminatedString = diag.CodeUnterminatedString

	// CodeUnknownOverrideColumn indicates a sgl.column(name, ...) directive in a query
	// header references an output column name that does not appear in the query's SELECT
	// projection. The diagnostic carries a Levenshtein Suggestion of the closest actual
	// output column name.
	CodeUnknownOverrideColumn = diag.CodeUnknownOverrideColumn

	// CodeUnknownOverrideMigrationColumn indicates a sgl.column(table.col, ...) directive in
	// a migration file references a column that does not exist on the named table in the
	// catalogue. The diagnostic carries a Levenshtein Suggestion of the closest known
	// table.column reference.
	CodeUnknownOverrideMigrationColumn = diag.CodeUnknownOverrideMigrationColumn

	// CodeMutuallyExclusiveKeywordArgument indicates that two keyword arguments were
	// declared on the same directive call when only one of them may be set at a time (e.g.
	// `type:` and `go_type:` on sgl.column).
	//
	// The span points at the second of the two; the quick-fix is to delete that keyword
	// argument.
	CodeMutuallyExclusiveKeywordArgument = diag.CodeMutuallyExclusiveKeywordArgument

	// CodeDirectiveLineLimit indicates a single query block contained more
	// directive-eligible logical lines than the parser is willing to process, or one
	// directive continued over more lines than the parser joins.
	//
	// Emitted as a defence-in-depth cap so a malformed or hostile input cannot cause
	// unbounded parsing work; the cap is well above any realistic legitimate directive
	// header.
	CodeDirectiveLineLimit = diag.CodeDirectiveLineLimit

	// CodeAsyncExecRecommended is a hint.
	//
	// A query whose command is exec runs against an engine that surfaces asynchronous
	// mutations (e.g. ClickHouse ALTER UPDATE / ALTER DELETE) and the underlying statement
	// is one of those async forms. Switching the command to asyncexec surfaces the
	// fire-and-forget semantics so callers do not mistake nil error for "mutation has
	// completed". The hint does not block code generation.
	CodeAsyncExecRecommended = diag.CodeAsyncExecRecommended

	// CodeAsyncExecNotSupported indicates a query declared with the asyncexec command landed
	// on a dialect that reports Info.AsyncMutations as false.
	//
	// asyncexec is meaningful only on engines that distinguish mutation acceptance from
	// completion (e.g. ClickHouse); on other engines it would quietly downgrade to a
	// synchronous Exec which would hide the intent. This is a hard error so the user picks
	// exec / execresult / execrows instead.
	CodeAsyncExecNotSupported = diag.CodeAsyncExecNotSupported

	// CodeUnreferencedParameter indicates a parameter directive was declared (named or
	// positional) but the corresponding placeholder never appears in the query body.
	CodeUnreferencedParameter = diag.CodeUnreferencedParameter

	// CodeMigrationMixedTransaction indicates a migration file contains multiple statements
	// and must run without a transaction.
	//
	// A sgl.migration(no_transaction: true) directive or an auto-detected statement such as
	// CREATE INDEX CONCURRENTLY makes the whole migration non-transactional. Isolating the
	// statements that require this in their own migration lets the other statements run
	// atomically.
	CodeMigrationMixedTransaction = diag.CodeMigrationMixedTransaction

	// CodeDirectiveWrongContext indicates a directive was used in the wrong kind of file: a
	// sgl.migration directive in a query file, or a sgl.query header in a migration file.
	// The directive is ignored; the warning points the author at the correct one.
	CodeDirectiveWrongContext = diag.CodeDirectiveWrongContext

	// CodeFunctionDataAccessUndeclared indicates a dialect's function resolver returned a
	// resolution without declaring its data access (DataAccessUnknown in sdk/dialect).
	//
	// The resolver should report DataAccessReadOnly or DataAccessModifiesData explicitly.
	// Resolution falls back to read-only, which is correct for the read-only built-ins these
	// resolvers handle, but a resolver that ever returned a data-modifying function this way
	// would be misrouted to a read replica, so the gap is surfaced rather than hidden.
	CodeFunctionDataAccessUndeclared = diag.CodeFunctionDataAccessUndeclared

	// CodeConflictDoNothingReturning indicates a command:one query pairs ON CONFLICT DO
	// NOTHING with a RETURNING clause.
	//
	// When a conflict occurs the insert is skipped and RETURNING yields no row, so the
	// generated QueryRow scan returns the driver no-rows sentinel (sql.ErrNoRows, or
	// pgx.ErrNoRows for native pgx) on the conflict path. The query is valid; the warning
	// steers the author towards optional: true (which returns (row, false, nil)),
	// command:exec / execrows, or explicit errors.Is handling.
	CodeConflictDoNothingReturning = diag.CodeConflictDoNothingReturning

	// CodeOptionalNonOneCommand indicates optional: true was set on a query where it does
	// not apply.
	//
	// The optional flag only makes sense on a static command:one, where it turns a zero-row
	// result into (row, false, nil) rather than the driver no-rows sentinel. On any other
	// command, or combined with dynamic runtime builders or optional predicate params, it
	// has no meaning, so it is rejected rather than silently ignored.
	CodeOptionalNonOneCommand = diag.CodeOptionalNonOneCommand

	// CodeIgnoredMigrationFile indicates a .sql file in the migration directory was skipped
	// because its name ends in neither .up.sql nor .down.sql, or sits in a subdirectory, or
	// that an up migration holds the down section of another tool's single-file layout.
	CodeIgnoredMigrationFile = diag.CodeIgnoredMigrationFile

	// CodeUnusableGoTypeOverride indicates a go_type, from a migration or query column
	// directive or a configured type override, names a type the generator cannot write, so
	// the override is not applied.
	CodeUnusableGoTypeOverride = diag.CodeUnusableGoTypeOverride

	// CodeAliasPrefix indicates a directive written with an alias of the canonical prefix,
	// such as seagull.query where sgl.query is canonical.
	//
	// The directive is parsed as usual. The hint is reported only when the configuration
	// requires the canonical prefix, so a codebase can converge on one spelling.
	CodeAliasPrefix = diag.CodeAliasPrefix

	// CodeForeignPrefix indicates a directive written with a foreign prefix: one that
	// belongs to another tool and is not enabled for this run.
	//
	// The directive is not parsed; the warning names the prefix and how to enable it or
	// rewrite the file to the canonical prefix.
	CodeForeignPrefix = diag.CodeForeignPrefix

	// CodeReturningNotSupported indicates an INSERT, UPDATE or DELETE with a RETURNING
	// clause on a dialect whose Info does not list that kind of statement in Returning.
	//
	// The database rejects the statement, so the generated method could only fail at run
	// time. The analyser judges the clause against the dialect's declared capability, never
	// its name, so a dialect option that turns RETURNING on or off decides the outcome.
	CodeReturningNotSupported = diag.CodeReturningNotSupported

	// CodeDuplicateMigrationVersion indicates two migration files share a version and a
	// direction, which the migration runner refuses to run. The catalogue replays only the
	// first of them.
	CodeDuplicateMigrationVersion = diag.CodeDuplicateMigrationVersion

	// CodeDepthLimit indicates analysis stopped descending into something nested more deeply
	// than a fixed limit allows, such as a chain of casts, nested subqueries or a deeply
	// nested type, so the part beyond the limit was not analysed.
	CodeDepthLimit = diag.CodeDepthLimit

	// CodeSliceParameterShape indicates a parameter declared kind: slice whose type is an
	// array already, or whose placeholder is not the sole element of an IN list, so the
	// generated code has no list to splice the slice's elements into.
	CodeSliceParameterShape = diag.CodeSliceParameterShape

	// CodeOptionalNotNullable indicates an optional parameter whose placeholder carries a
	// type that cannot hold NULL, so passing nil matches no row instead of switching the
	// predicate off.
	CodeOptionalNotNullable = diag.CodeOptionalNotNullable

	// CodeSortableOrderByParameter indicates a parameter written in the ORDER BY of a
	// sortable query, which the generated code replaces with the caller's ordering, so the
	// parameter would never be bound.
	CodeSortableOrderByParameter = diag.CodeSortableOrderByParameter

	// CodeParametersBeforeStatement indicates a placeholder in a statement before the
	// query's own, which the generated code binds no value to.
	CodeParametersBeforeStatement = diag.CodeParametersBeforeStatement

	// CodeEmitterUnsupported indicates a query the configured emitter cannot generate code
	// for, such as a command its driver has no way to run.
	CodeEmitterUnsupported = diag.CodeEmitterUnsupported

	// CodeBatchShape indicates a batch or copyfrom query whose statement the generated code
	// cannot repeat for each row: not an INSERT with exactly one VALUES row, a parameter
	// outside that row, or a row item that is not a placeholder alone where the emitter
	// needs one.
	CodeBatchShape = diag.CodeBatchShape

	// CodeUnmatchedTypeOverride indicates a type override for a SQL type that names no type
	// the dialect knows and no type of a column in the schema, so it changes nothing.
	CodeUnmatchedTypeOverride = diag.CodeUnmatchedTypeOverride

	// CodeUngroupedColumn indicates a column a query reads outside every aggregate when the
	// query aggregates without a GROUP BY clause, which the engine rejects, or rejects in
	// its default mode.
	CodeUngroupedColumn = diag.CodeUngroupedColumn

	// CodeEngineReading indicates SQL the engine runs but reads other than the statement
	// appears to mean, as the dialect warns, such as MySQL reading || as a logical OR.
	CodeEngineReading = diag.CodeEngineReading

	// CodeInsertShape indicates an INSERT whose shape the engine refuses: a row giving
	// another number of values than the statement names target columns, or an INSERT ...
	// SELECT whose select list gives another number of columns.
	CodeInsertShape = diag.CodeInsertShape

	// CodeRowType indicates that a requested model or shared row type cannot represent the
	// query's results or conflicts with another generated name.
	CodeRowType = diag.CodeRowType

	// CodeDiagnosticLimit indicates further diagnostics were left out because a limit on how
	// many are reported was reached. It carries the highest severity among those left out.
	CodeDiagnosticLimit = diag.CodeDiagnosticLimit

	// CodeDuplicateObject indicates a statement that creates an object, or renames or moves
	// one, onto a name an existing object already holds.
	CodeDuplicateObject = diag.CodeDuplicateObject

	// CodeUnknownObject indicates a statement that alters, drops or renames an object that
	// does not exist, or that is of another kind than the statement names.
	CodeUnknownObject = diag.CodeUnknownObject

	// CodePartlyAnalysed indicates part of a statement, such as the body of a view or a
	// table macro, could not be analysed, so the columns it defines are unknown.
	CodePartlyAnalysed = diag.CodePartlyAnalysed

	// CodeRecoveredPanic indicates Seagull, a dialect or code the host supplied panicked
	// while handling a statement; the panic was recovered and its stack logged.
	CodeRecoveredPanic = diag.CodeRecoveredPanic

	// CodeViewDerivationUnsettled indicates that deriving again the columns of views that
	// follow their tables did not settle within the limit on rounds.
	CodeViewDerivationUnsettled = diag.CodeViewDerivationUnsettled

	// SeverityError marks a diagnostic that prevents code generation: Result.Err and
	// Report.Err return a *DiagnosticsError while one is present.
	SeverityError = diag.SeverityError

	// SeverityWarning marks a potential problem that does not block generation.
	SeverityWarning = diag.SeverityWarning

	// SeverityHint marks a suggestion for improvement.
	SeverityHint = diag.SeverityHint
)

// CodeInfo documents one diagnostic code: its title, the severity it is normally reported
// with and a Markdown explanation of its cause and fix. Codes lists every one and
// LookupCode finds one, as the sgl explain command does.
type CodeInfo = diag.CodeInfo

// Presentation is one diagnostic laid out for display: a header, the quoted source line
// with carets under the span, and a help line. Present fills it and WriteText prints it;
// other renderers, such as an HTML panel, can lay the same fields out their own way.
type Presentation = diag.Presentation

// SourceError is one diagnostic mapped back to the SQL file it concerns: a stable code
// such as "Q001" (one of the Code constants), a severity, a message, an optional
// suggestion and the line and column span.
//
// Result.Diagnostics, Report.Diagnostics and the second result of Generator.Catalogue
// hold them. Present lays one out for display, WriteText prints it, and LookupCode
// documents its code.
type SourceError = diag.SourceError

// ErrorSeverity classifies a diagnostic as SeverityError, SeverityWarning or
// SeverityHint. It travels in JSON and other text encodings by name: error, warning or
// hint.
type ErrorSeverity = diag.ErrorSeverity

// Codes returns the documentation of every diagnostic code in ascending order.
//
// Returns []CodeInfo which is a fresh copy the caller may modify.
func Codes() []CodeInfo {
	return diag.Codes()
}

// LookupCode returns the documentation of one diagnostic code.
//
// Takes code (string) which names the code in any case and with or without the leading
// letter and zero padding, such as "Q012", "q012", "Q12" or "12".
//
// Returns CodeInfo which documents the code.
// Returns bool which is false when no code matches.
func LookupCode(code string) (CodeInfo, bool) {
	return diag.LookupCode(code)
}

// Present lays a diagnostic out for display.
//
// The caller supplies the display path and source line; no files are read. Lines longer
// than 160 runes are clipped to a window of at most 160 runes around the span. Clipping
// preserves rune boundaries and marks omitted text with "...".
//
// Takes diagnostic (SourceError) which is the diagnostic.
// Takes display (string) which is the path to show for diagnostic.Filename, or empty.
// Takes sourceLine (string) which is the text of line diagnostic.Line, or empty when it
// is unavailable.
//
// Returns Presentation which holds the parts to print.
func Present(diagnostic SourceError, display string, sourceLine string) Presentation {
	return diag.Present(diagnostic, display, sourceLine)
}

// WriteText prints presentations for a terminal, one block each, separated by a blank
// line.
//
//	db/queries/tasks.sql:3:8: error[Q001]: unknown column "tilte"
//	   |
//	 3 | SELECT tilte FROM tasks
//	   |        ^^^^^
//	help: did you mean "title"?
//
// A presentation without a source line prints the header and the help line only. Each
// block is written as soon as it is laid out, so the text of every block is never held at
// once, and writing stops at the first error.
//
// Takes w (io.Writer) which receives the text.
// Takes items ([]Presentation) which are printed in order.
//
// Returns error when writing fails.
func WriteText(w io.Writer, items []Presentation) error {
	return diag.WriteText(w, items)
}

// DidYouMean phrases a suggestion as the question a diagnostic's message and help line
// ask, such as `did you mean "title"?`, so every producer and renderer words it alike.
//
// Takes suggestion (string) which is the suggested name or value.
//
// Returns string which is the question.
func DidYouMean(suggestion string) string {
	return diag.DidYouMean(suggestion)
}
