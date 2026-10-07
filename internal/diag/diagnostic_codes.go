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

package diag

const (
	// CodeUnknownColumn indicates a column reference that could not be resolved in any table
	// or alias in scope.
	CodeUnknownColumn = "Q001"

	// CodeAmbiguousColumn indicates a column name that matches more than one table in the
	// current scope.
	CodeAmbiguousColumn = "Q002"

	// CodeUnknownTable indicates a table, CTE, or table-valued function that is not present
	// in the catalogue.
	CodeUnknownTable = "Q003"

	// CodeExpressionTypeError indicates a failure to infer the type of an expression during
	// type resolution.
	CodeExpressionTypeError = "Q004"

	// CodeUnknownFunction indicates a function call that could not be resolved or has no
	// matching overload for the given argument count.
	CodeUnknownFunction = "Q005"

	// CodeDuplicateQueryName indicates two queries declare names that match after ignoring
	// letter case and underscores, such as get_user and GetUser.
	CodeDuplicateQueryName = "Q006"

	// CodeDirectiveSyntax indicates a malformed sgl.* directive in a SQL query file.
	CodeDirectiveSyntax = "Q007"

	// CodeMissingDirective indicates a required query name or command is absent from a
	// query's directive header.
	CodeMissingDirective = "Q008"

	// CodeCommandOutputMismatch indicates the declared command does not match the actual
	// query output (e.g. exec with SELECT, or one with INSERT without RETURNING).
	CodeCommandOutputMismatch = "Q009"

	// CodeParseError indicates a SQL parse failure, empty query, or DDL interpretation error
	// during catalogue building.
	CodeParseError = "Q010"

	// CodeSortableColumnMissing indicates a sgl.sortable parameter references a column that
	// is not present in the query output.
	CodeSortableColumnMissing = "Q011"

	// CodeMultipleStatements indicates a query block contains more than one SQL statement;
	// only the last is analysed, and the query cannot be generated.
	CodeMultipleStatements = "Q012"

	// CodeGeneratedColumn indicates a parameter attempts to assign to a generated
	// (non-writable) column.
	CodeGeneratedColumn = "Q013"

	// CodeGroupByColumnMissing indicates a sgl.group_by references a column that is not
	// present in the query output.
	CodeGroupByColumnMissing = "Q014"

	// CodeGroupByMissingEmbed indicates sgl.group_by is present but no sgl.embed directive
	// exists on non-key tables.
	CodeGroupByMissingEmbed = "Q015"

	// CodeGroupByWrongCommand indicates sgl.group_by is used with a command other than many.
	CodeGroupByWrongCommand = "Q016"

	// CodeSliceBatchCopyFrom indicates a kind: slice parameter was used with the batch or
	// copyfrom command. These commands send one row of parameters per item and cannot expand
	// a slice.
	CodeSliceBatchCopyFrom = "Q017"

	// CodeSliceDynamicRuntime indicates a kind: slice parameter was used with dynamic:
	// runtime. The runtime query builder constructs WHERE clauses dynamically and cannot
	// expand slice placeholders.
	CodeSliceDynamicRuntime = "Q018"

	// CodeCompoundColumnCount indicates a UNION/INTERSECT/EXCEPT branch has a different
	// number of columns than the primary SELECT.
	CodeCompoundColumnCount = "Q020"

	// CodeRuntimeBuilderBaseHasWhere is informational.
	//
	// A dynamic: runtime query's static SQL already contains a WHERE clause, so the builder
	// appends additional runtime predicates with " AND " instead of " WHERE ". Emitted so
	// the user knows the merged SQL semantics without having to read the generated code.
	CodeRuntimeBuilderBaseHasWhere = "Q022"

	// CodeCountSemanticsWrapped is informational.
	//
	// The generated <query>CountSQL constant wraps the original SELECT in `SELECT COUNT(*)
	// FROM (<original>) sub` because the query contains GROUP BY, HAVING, DISTINCT, an
	// aggregate or window function, or a compound query. Wrapping makes .Count(ctx) return
	// the outer-result-row cardinality rather than the inner-table cardinality.
	CodeCountSemanticsWrapped = "Q023"

	// CodeCountRewriteUnavailable is a warning.
	//
	// A dynamic: runtime query could not be rewritten into a COUNT query (the shared
	// rewriter did not recognise a top-level SELECT), so the generated builder has no
	// .Count(ctx) support. The query still generates; only the count terminal is
	// unavailable.
	CodeCountRewriteUnavailable = "Q024"

	// CodeUnknownDirective indicates a sgl.X(...) directive whose name is not in the
	// registry. The diagnostic carries a Levenshtein-derived Suggestion when a close
	// neighbour exists in the directive vocabulary.
	CodeUnknownDirective = "Q026"

	// CodeUnknownKeywordArgument indicates a keyword argument key inside sgl(...) or
	// sgl.X(...) that the directive's spec does not accept. The diagnostic carries a
	// Suggestion when a close neighbour exists in the directive's keyword argument
	// vocabulary.
	CodeUnknownKeywordArgument = "Q027"

	// CodeInvalidKeywordArgumentValue indicates a keyword argument value that fails the
	// directive's value-shape check (closed enum, integer, bool, list, etc.). The diagnostic
	// carries a Suggestion for the closest allowed value when the keyword argument has a
	// closed AllowedValues set.
	CodeInvalidKeywordArgumentValue = "Q028"

	// CodeDuplicateKeywordArgument indicates the same keyword argument key appears twice in
	// one directive call.
	CodeDuplicateKeywordArgument = "Q029"

	// CodeInternalNilGuard is a defensive diagnostic for nil columns, derived tables, or
	// compound branches during type resolution. This should not normally fire.
	CodeInternalNilGuard = "Q030"

	// CodeMissingRequired indicates a required positional argument or a required keyword
	// argument is absent from a directive call.
	CodeMissingRequired = "Q031"

	// CodeUnclosedDirective indicates a multi-line directive call whose paren depth never
	// returns to zero before the directive header terminates.
	CodeUnclosedDirective = "Q032"

	// CodeParameterBindingSyntax indicates a malformed parameter-binding line: the anchor /
	// "as" keyword / "sgl." prefix / role token did not appear in the expected position. The
	// diagnostic may carry a Suggestion when the offending token is a near-miss typo of the
	// expected keyword ("as" / "sgl").
	CodeParameterBindingSyntax = "Q033"

	// CodeInvalidListLiteral indicates a bracketed list value where the closing token was
	// not the expected ']' (commonly a stray ')' or missing ']'). The malformed value is
	// rejected so downstream validation does not see partial data.
	CodeInvalidListLiteral = "Q034"

	// CodeUnterminatedString indicates a string literal whose opening quote was not matched
	// before the end of the directive line or the end of the file. Reported before
	// multi-line continuation kicks in so the user sees the real cause instead of a
	// downstream "unclosed directive" diagnostic.
	CodeUnterminatedString = "Q035"

	// CodeUnknownOverrideColumn indicates a sgl.column(name, ...) directive in a query
	// header references an output column name that does not appear in the query's SELECT
	// projection. The diagnostic carries a Levenshtein Suggestion of the closest actual
	// output column name.
	CodeUnknownOverrideColumn = "Q036"

	// CodeUnknownOverrideMigrationColumn indicates a sgl.column(table.col, ...) directive in
	// a migration file references a column that does not exist on the named table in the
	// catalogue. The diagnostic carries a Levenshtein Suggestion of the closest known
	// table.column reference.
	CodeUnknownOverrideMigrationColumn = "Q037"

	// CodeMutuallyExclusiveKeywordArgument indicates that two keyword arguments were
	// declared on the same directive call when only one of them may be set at a time (e.g.
	// `type:` and `go_type:` on sgl.column).
	//
	// The span points at the second of the two; the quick-fix is to delete that keyword
	// argument.
	CodeMutuallyExclusiveKeywordArgument = "Q038"

	// CodeDirectiveLineLimit indicates a single query block contained more
	// directive-eligible logical lines than the parser is willing to process, or one
	// directive continued over more lines than the parser joins.
	//
	// Emitted as a defence-in-depth cap so a malformed or hostile input cannot cause
	// unbounded parsing work; the cap is well above any realistic legitimate directive
	// header.
	CodeDirectiveLineLimit = "Q039"

	// CodeAsyncExecRecommended is a hint.
	//
	// A query whose command is exec runs against an engine that surfaces asynchronous
	// mutations (e.g. ClickHouse ALTER UPDATE / ALTER DELETE) and the underlying statement
	// is one of those async forms. Switching the command to asyncexec surfaces the
	// fire-and-forget semantics so callers do not mistake nil error for "mutation has
	// completed". The hint does not block code generation.
	CodeAsyncExecRecommended = "Q040"

	// CodeAsyncExecNotSupported indicates a query declared with the asyncexec command landed
	// on a dialect that reports Info.AsyncMutations as false.
	//
	// asyncexec is meaningful only on engines that distinguish mutation acceptance from
	// completion (e.g. ClickHouse); on other engines it would quietly downgrade to a
	// synchronous Exec which would hide the intent. This is a hard error so the user picks
	// exec / execresult / execrows instead.
	CodeAsyncExecNotSupported = "Q041"

	// CodeUnreferencedParameter indicates a parameter directive was declared (named or
	// positional) but the corresponding placeholder never appears in the query body.
	CodeUnreferencedParameter = "Q042"

	// CodeMigrationMixedTransaction indicates a migration file contains multiple statements
	// and must run without a transaction.
	//
	// A sgl.migration(no_transaction: true) directive or an auto-detected statement such as
	// CREATE INDEX CONCURRENTLY makes the whole migration non-transactional. Isolating the
	// statements that require this in their own migration lets the other statements run
	// atomically.
	CodeMigrationMixedTransaction = "Q043"

	// CodeDirectiveWrongContext indicates a directive was used in the wrong kind of file: a
	// sgl.migration directive in a query file, or a sgl.query header in a migration file.
	// The directive is ignored; the warning points the author at the correct one.
	CodeDirectiveWrongContext = "Q044"

	// CodeFunctionDataAccessUndeclared indicates an engine function resolver returned a
	// resolution without declaring its data access (DataAccessUnknown).
	//
	// The resolver should report DataAccessReadOnly or DataAccessModifiesData explicitly.
	// Resolution falls back to read-only, which is correct for the read-only built-ins these
	// resolvers handle, but a resolver that ever returned a data-modifying function this way
	// would be misrouted to a read replica, so the gap is surfaced rather than hidden.
	CodeFunctionDataAccessUndeclared = "Q045"

	// CodeConflictDoNothingReturning indicates a command:one query pairs ON CONFLICT DO
	// NOTHING with a RETURNING clause.
	//
	// When a conflict occurs the insert is skipped and RETURNING yields no row, so the
	// generated QueryRow scan returns the driver no-rows sentinel (sql.ErrNoRows, or
	// pgx.ErrNoRows for native pgx) on the conflict path. The query is valid; the warning
	// steers the author towards optional: true (which returns (row, false, nil)),
	// command:exec / execrows, or explicit errors.Is handling.
	CodeConflictDoNothingReturning = "Q046"

	// CodeOptionalNonOneCommand indicates optional: true was set on a query where it does
	// not apply.
	//
	// The optional flag only makes sense on a static command:one, where it turns a zero-row
	// result into (row, false, nil) rather than the driver no-rows sentinel. On any other
	// command, or combined with dynamic runtime builders or optional predicate params, it
	// has no meaning, so it is rejected rather than silently ignored.
	CodeOptionalNonOneCommand = "Q047"

	// CodeIgnoredMigrationFile indicates a .sql file in the migration directory was skipped
	// because its name ends in neither .up.sql nor .down.sql, or sits in a subdirectory, or
	// that an up migration holds the down section of another tool's single-file layout.
	CodeIgnoredMigrationFile = "Q048"

	// CodeUnusableGoTypeOverride indicates a go_type, from a migration or query column
	// directive or a configured type override, names a type the generator cannot write, so
	// the override is not applied.
	CodeUnusableGoTypeOverride = "Q049"

	// CodeAliasPrefix indicates a directive written with an alias of the canonical prefix,
	// such as seagull.query where sgl.query is canonical.
	//
	// The directive is parsed as usual. The hint is reported only when the configuration
	// requires the canonical prefix, so a codebase can converge on one spelling.
	CodeAliasPrefix = "Q050"

	// CodeForeignPrefix indicates a directive written with a foreign prefix: one that
	// belongs to another tool and is not enabled for this run.
	//
	// The directive is not parsed; the warning names the prefix and how to enable it or
	// rewrite the file to the canonical prefix.
	CodeForeignPrefix = "Q051"

	// CodeReturningNotSupported indicates an INSERT, UPDATE or DELETE with a RETURNING
	// clause on a dialect whose Info reports Returning as false.
	//
	// The database rejects the statement, so the generated method could only fail at run
	// time. The analyser judges the clause against the dialect's declared capability, never
	// its name, so a dialect option that turns RETURNING on or off decides the outcome.
	CodeReturningNotSupported = "Q052"

	// CodeDuplicateMigrationVersion indicates two migration files share a version and a
	// direction, which the migration runner refuses to run. The catalogue replays only the
	// first of them.
	CodeDuplicateMigrationVersion = "Q053"

	// CodeDepthLimit indicates analysis stopped descending into something nested more deeply
	// than a fixed limit allows, such as a chain of casts, nested subqueries or a deeply
	// nested type, so the part beyond the limit was not analysed.
	CodeDepthLimit = "Q054"

	// CodeSliceParameterShape indicates a parameter declared kind: slice whose type is an
	// array already, or whose placeholder is not the sole element of an IN list, so the
	// generated code has no list to splice the slice's elements into.
	CodeSliceParameterShape = "Q055"

	// CodeOptionalNotNullable indicates an optional parameter whose placeholder carries a
	// type that cannot hold NULL, so passing nil matches no row instead of switching the
	// predicate off.
	CodeOptionalNotNullable = "Q056"

	// CodeSortableOrderByParameter indicates a parameter written in the ORDER BY of a
	// sortable query, which the generated code replaces with the caller's ordering, so the
	// parameter would never be bound.
	CodeSortableOrderByParameter = "Q057"

	// CodeParametersBeforeStatement indicates a placeholder in a statement before the
	// query's own, which the generated code binds no value to.
	CodeParametersBeforeStatement = "Q058"

	// CodeEmitterUnsupported indicates a query the configured emitter cannot generate code
	// for, such as a command its driver has no way to run.
	CodeEmitterUnsupported = "Q059"

	// CodeBatchShape indicates a batch or copyfrom query whose statement the generated code
	// cannot repeat for each row: not an INSERT with exactly one VALUES row, a parameter
	// outside that row, or a row item that is not a placeholder alone where the emitter
	// needs one.
	CodeBatchShape = "Q060"

	// CodeUnmatchedTypeOverride indicates a type override for a SQL type that names no type
	// the dialect knows and no type of a column in the schema, so it changes nothing.
	CodeUnmatchedTypeOverride = "Q061"

	// CodeUngroupedColumn indicates a column a query reads outside every aggregate when the
	// query aggregates without a GROUP BY clause, which the engine rejects, or rejects in
	// its default mode.
	CodeUngroupedColumn = "Q062"

	// CodeEngineReading indicates SQL the engine runs but reads other than the statement
	// appears to mean, as the dialect warns, such as MySQL reading || as a logical OR.
	CodeEngineReading = "Q063"

	// CodeInsertShape indicates an INSERT whose shape the engine refuses: a row giving
	// another number of values than the statement names target columns, or an INSERT ...
	// SELECT whose select list gives another number of columns.
	CodeInsertShape = "Q064"

	// CodeRowType indicates that a requested model or shared row type cannot represent the
	// query's results or conflicts with another generated name.
	CodeRowType = "Q065"

	// CodeDiagnosticLimit indicates further diagnostics were left out because a limit on how
	// many are reported was reached. It carries the highest severity among those left out.
	CodeDiagnosticLimit = "Q066"

	// CodeDuplicateObject indicates a statement that creates an object, or renames or moves
	// one, onto a name an existing object already holds.
	CodeDuplicateObject = "Q067"

	// CodeUnknownObject indicates a statement that alters, drops or renames an object that
	// does not exist, or that is of another kind than the statement names.
	CodeUnknownObject = "Q068"

	// CodePartlyAnalysed indicates part of a statement, such as the body of a view or a
	// table macro, could not be analysed, so the columns it defines are unknown.
	CodePartlyAnalysed = "Q069"

	// CodeRecoveredPanic indicates Seagull, a dialect or code the host supplied panicked
	// while handling a statement; the panic was recovered and its stack logged.
	CodeRecoveredPanic = "Q070"

	// CodeViewDerivationUnsettled indicates that deriving again the columns of views that
	// follow their tables did not settle within the limit on rounds.
	CodeViewDerivationUnsettled = "Q071"
)
