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

package dialecttest

import (
	"context"
	"io/fs"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

// passed is the outcome of a check that found no problems.
var passed = knownfailure.Passed()

// ElementFixture describes a table whose column holds an array, list or map, for the
// check that its elements are nullable where the engine can hold NULL among them and the
// check of the values the dialect writes for it.
type ElementFixture struct {
	// Migration creates the table samples with an integer id and a column readings of an
	// array, list or map type of integers declared without nullable elements, such as
	// integer[] or Array(Int32).
	Migration string

	// Aggregate is a call that collects the id column of samples, which is never NULL, into
	// an array or a list, such as array_agg(id), or is empty when the dialect has none, and
	// the check that such elements are never NULL is then skipped.
	Aggregate string

	// Parameter is a query whose single parameter takes the readings column's type. Empty
	// skips parameter-element checks for dialects that cannot bind such values.
	Parameter string

	// Quantified is a query reading samples whose one parameter is an array a comparison
	// quantified with ANY, ALL or SOME reads, such as SELECT id FROM samples WHERE id =
	// ANY($1), or is empty when the dialect has no such comparison.
	Quantified string

	// Literals lists bracketed or braced readings values used to check parameter positions
	// in INSERT rows. Empty skips this check.
	Literals []string
}

// Fixture is the dialect-specific SQL the conformance suite runs.
//
// Each SQL field must be written in the dialect's syntax. Optional fields document when
// they may be left empty; their checks report themselves skipped rather than passing.
type Fixture struct {
	// KnownFailures maps conformance check names to reasons for expected failures. An entry
	// must be removed when its check starts passing.
	KnownFailures map[string]string

	// NewDialect returns a fresh instance of the dialect under test.
	NewDialect func() dialect.DialectPort

	// NewDialectWithExtraType returns the dialect built with its extra-types option
	// registering sqlType under name, or nil when the dialect has no such option.
	NewDialectWithExtraType func(name string, sqlType sqlmodel.SQLType) dialect.DialectPort

	// Flavour, set only for a flavour of a base dialect, describes the flavour built with
	// options of the caller's own, so the suite can check that they add to the flavour's
	// settings rather than replace them.
	Flavour *FlavourFixture

	// Spans supplies the statements the span checks examine; the checks are skipped when it
	// is nil.
	Spans *SpanFixture

	// Tokeniser supplies the dialect's tokeniser for comparison with the shared lexical
	// scanner, sqllex.Scanner. Nil limits the check to statement boundaries.
	Tokeniser *TokeniserFixture

	// DeclaredParameters supplies the queries that check how a column override reaches a
	// parameter declaring its own type; the check is skipped when it is nil.
	DeclaredParameters *DeclaredParameterFixture

	// Elements describes a table whose column holds an array, list or map, or is nil for a
	// dialect without such types, and the element check is then skipped.
	Elements *ElementFixture

	// Replay supplies the migrations the checks that DDL replays as the engine applies it
	// run, or is nil, and those checks are then skipped.
	Replay *ReplayFixture

	// BaseMigration creates a table named items with an integer primary key id and a text
	// column name.
	BaseMigration string

	// ParamQuery is a query named GetItem, in the sgl.query directive form, that selects id
	// and name from items filtered by exactly one parameter, ending in a semicolon.
	ParamQuery string

	// SplitterMigration holds exactly two statements, creating the tables alpha and beta.
	SplitterMigration string

	// QuotedMigration creates a table whose delimited name is "weird name", quoted the
	// dialect's way.
	QuotedMigration string

	// ReservedWordMigration creates a table named reserved_items with an integer column id
	// and the delimited columns check, constraint, index, primary, group and where, in that
	// order.
	ReservedWordMigration string

	// ReservedWordQuery is a query named GetReserved that selects id and the delimited group
	// column from reserved_items, filtered by the delimited where column equalling exactly
	// one parameter.
	ReservedWordQuery string

	// TypedMigration creates a table named typed_items whose columns use the dialect's
	// richer type spellings, such as character sets, time zones or array suffixes.
	TypedMigration string

	// PlaceholderQuery is a query named CountPlaceholders, against the items table, whose
	// SQL holds PlaceholderCount distinct parameter placeholders.
	PlaceholderQuery string

	// NotInQuery is a query named ExcludeItems that selects id from items filtered by id NOT
	// IN a list holding exactly one placeholder.
	NotInQuery string

	// GroupingQuery tests the dialect's grouping modifier with id and count(*) AS total from
	// items. Empty skips the check.
	GroupingQuery string

	// UnboundStatement is a statement the dialect runs without analysing, such as a SET,
	// that holds one parameter written in the dialect's placeholder form; the generated code
	// could not bind it, so it must be refused with a parse error naming the statement.
	// Empty skips the check.
	UnboundStatement string

	// TypedColumns lists the column names TypedMigration declares, in order.
	TypedColumns []string

	// RowLocks lists row-locking clauses used to check that queries, including nested
	// queries, run on the writer. Empty skips this check.
	RowLocks []string

	// PlaceholderCount is the number of distinct placeholders in PlaceholderQuery.
	PlaceholderCount int

	// DerivedColumnLists reports that the dialect accepts a column list after a derived
	// table's alias, as in (SELECT ...) AS d (a, b); the column list check covers it only
	// then.
	DerivedColumnLists bool

	// CTEColumnLists reports that the dialect accepts a column list after a CTE's name, as
	// in WITH t (a, b) AS (...); the column list check covers it only then.
	CTEColumnLists bool

	// TableColumnLists reports that the dialect accepts a column alias list after a table's
	// alias, as in items AS i (a, b), which renames the table's leading columns for the
	// query; the column list check covers it only then.
	TableColumnLists bool

	// JoinAliases reports that the dialect accepts an alias on a parenthesised join, as in
	// (a JOIN b USING (id)) AS r, which names a relation of every column of the join; the
	// aliased join check is skipped otherwise.
	JoinAliases bool

	// QuantifiedArrays reports that the dialect compares a value with each element of an
	// array, as in id = ANY($1); the quantified array check covers it only then.
	QuantifiedArrays bool

	// RowComparisons reports that the dialect compares row values, as in (id, name) < ($1,
	// $2) and (id, name) IN (($1, $2)), giving each parameter of one row the type of the
	// other row's element at its position; the row comparison check is skipped otherwise.
	RowComparisons bool

	// ValuesArms reports that the dialect accepts a VALUES list written as VALUES (...)
	// where a query stands: as an arm of a set operation, as the body of a CTE that names
	// its columns and as the anchor of a recursive one; the VALUES check is skipped
	// otherwise.
	ValuesArms bool
}

// SpanFixture supplies the statements whose spans the span checks examine.
type SpanFixture struct {
	// Statements supplies valid SQL statements for the conformance suite. They should
	// exercise the dialect's supported clauses and statement kinds.
	Statements []string

	// Corpora holds trees of golden-style cases whose queries/*.sql files the span checks
	// also examine, such as os.DirFS("testdata"). A statement the dialect cannot analyse
	// there is skipped.
	Corpora []fs.FS
}

// FlavourFixture describes a flavour built with options of the caller's own.
type FlavourFixture struct {
	// New returns the flavour built with the caller's options, for example an
	// extra-functions option registering UserFunction and a type normaliser hook that
	// declines every name.
	New func() dialect.DialectPort

	// UserFunction is the function the caller's options register.
	UserFunction string

	// FlavourFunctions lists functions only the flavour registers, all of which must survive
	// the caller's options.
	FlavourFunctions []string

	// FlavourTypes lists type names only the flavour resolves, all of which must survive the
	// caller's options.
	FlavourTypes []string

	// WithheldFunctions lists functions the base dialect registers and the flavour's engine
	// lacks, none of which the flavour may register, the caller's options applied or not, so
	// a call of one is reported as an unknown function rather than accepted to fail on the
	// server.
	WithheldFunctions []string
}

// conformanceCheck is one behaviour the conformance suite checks.
type conformanceCheck struct {
	// run performs the check and reports its outcome.
	run func(ctx context.Context, environment checkEnvironment, fixture *Fixture) checkOutcome

	// name is the check's subtest name and its KnownFailures key.
	name string
}

// conformanceChecks lists every check the suite runs, in order; the last ones check that
// migrations replay into the catalogue as the engine applies them.
var conformanceChecks = []conformanceCheck{
	{name: "StatementSplitting", run: checkStatementSplitting},
	{name: "BeginAliasSplitsBothStatements", run: checkBeginAlias},
	{name: "ParameterTracking", run: checkParameterTracking},
	{name: "QuotedIdentifier", run: checkQuotedIdentifier},
	{name: "CommentStripping", run: checkCommentStripping},
	{name: "UnterminatedBlockCommentErrors", run: checkUnterminatedBlockComment},
	{name: "DiagnosticsIgnoreTheDirectivePrefix", run: checkDiagnosticsIgnoreTheDirectivePrefix},
	{name: "DeepNestingTerminates", run: checkDeepNesting},
	{name: "InfoIsValid", run: checkInfoIsValid},
	{name: "ReservedWordColumns", run: checkReservedWordColumns},
	{name: "DeclaredColumnsReachCatalogue", run: checkDeclaredColumns},
	{name: "PlaceholdersBecomeParameters", run: checkPlaceholders},
	{name: "NotInListBindsTheColumn", run: checkNotInList},
	{name: "UsingJoinColumnAppearsOnce", run: checkUsingJoinColumnAppearsOnce},
	{name: "TrailingTokensRejected", run: checkTrailingTokens},
	{name: "UnrecognisedStatementRejected", run: checkUnrecognisedStatement},
	{name: "UnbalancedConditionRejected", run: checkUnbalancedCondition},
	{name: "EmptyConditionRejected", run: checkEmptyCondition},
	{name: "DanglingOperatorRejected", run: checkDanglingOperator},
	{name: "ConditionLeftoversRejected", run: checkConditionLeftovers},
	{name: "FromIsNeverASelectedColumn", run: checkFromIsNeverASelectedColumn},
	{name: "ExpressionKindsFollowInfo", run: checkExpressionKindsFollowInfo},
	{name: "LexicalReadsPlaceholderForms", run: checkLexicalReadsPlaceholderForms},
	{name: "ByteOrderMarkMigration", run: checkByteOrderMarkMigration},
	{name: "ByteOrderMarkQuery", run: checkByteOrderMarkQuery},
	{name: "UnknownColumnInWhere", run: checkUnknownColumnInWhere},
	{name: "UnknownColumnInOrderBy", run: checkUnknownColumnInOrderBy},
	{name: "UnknownColumnInSet", run: checkUnknownColumnInSet},
	{name: "UnknownNamesInNestedQueriesAreReported", run: checkUnknownNamesInNestedQueriesAreReported},
	{name: "InsertShapesTheEngineRefusesAreReported", run: checkInsertShapesTheEngineRefusesAreReported},
	{name: "UnknownFunctionInWhere", run: checkUnknownFunctionInWhere},
	{name: "UnknownColumnInSelectListReportedOnce", run: checkUnknownColumnInSelectListReportedOnce},
	{name: "SelectListAliasesFollowInfo", run: checkSelectListAliasesFollowInfo},
	{name: "PredicatesHaveTheStatedType", run: checkPredicatesHaveTheStatedType},
	{name: "PredicatesOverNullableOperandsAreNullable", run: checkPredicatesOverNullableOperandsAreNullable},
	{name: "OuterJoinedRelationsAreNullable", run: checkOuterJoinedRelationsAreNullable},
	{name: "UngroupedColumnsFollowTheDialect", run: checkUngroupedColumnsFollowTheDialect},
	{name: "AssignedExpressionParametersAreNotTypedAsTheColumn", run: checkAssignedExpressionParametersAreNotTypedAsTheColumn},
	{name: "ComparedExpressionParametersAreNotTypedAsTheColumn", run: checkComparedExpressionParametersAreNotTypedAsTheColumn},
	{name: "RowComparisonsTypeTheirParameters", run: checkRowComparisonsTypeTheirParameters},
	{name: "SharedTypeArgumentsTakeTheirSiblingsType", run: checkSharedTypeArgumentsTakeTheirSiblingsType},
	{name: "RowValuesFollowTheDialect", run: checkRowValuesFollowTheDialect},
	{name: "ParameterExpressionsCarryTheirNumbers", run: checkParameterExpressionsCarryTheirNumbers},
	{name: "ParametersInStatementsRunWithoutAnalysisAreRefused", run: checkParametersInStatementsRunWithoutAnalysisAreRefused},
	{name: "ComparedParametersAreTyped", run: checkComparedParametersAreTyped},
	{name: "CoalescedParametersTakeTheirSiblingsType", run: checkCoalescedParametersTakeTheirSiblingsType},
	{name: "QuantifiedArrayParametersAreArraysOfTheComparedType", run: checkQuantifiedArrayParametersAreArraysOfTheComparedType},
	{name: "ComparedParametersAreNamedAfterTheirColumns", run: checkComparedParametersAreNamedAfterTheirColumns},
	{name: "ParametersStandingForConditionsAreBooleans", run: checkParametersStandingForConditionsAreBooleans},
	{name: "ValuesWithoutAColumnListTypeTheirParametersByPosition", run: checkValuesWithoutAColumnListTypeTheirParametersByPosition},
	{name: "SelectAliasOperandsTypeParameters", run: checkSelectAliasOperandsTypeParameters},
	{name: "NestedQueriesSeeEnclosingRelations", run: checkNestedQueriesSeeEnclosingRelations},
	{name: "RowLocksAnywhereRouteToTheWriter", run: checkRowLocksAnywhereRouteToTheWriter},
	{name: "ScalarSubqueriesTakeTheirColumnsType", run: checkScalarSubqueriesTakeTheirColumnsType},
	{name: "ParametersComparedWithScalarSubqueriesAreTyped", run: checkParametersComparedWithScalarSubqueriesAreTyped},
	{name: "ColumnListsNameTheColumns", run: checkColumnListsNameTheColumns},
	{name: "AliasedJoinsAreRelations", run: checkAliasedJoinsAreRelations},
	{name: "ValuesListsStandWhereQueriesDo", run: checkValuesListsStandWhereQueriesDo},
	{name: "ScalarFunctionsInFromFollowTheDialect", run: checkScalarFunctionsInFromFollowTheDialect},
	{name: "ColumnOverridesFollowDeclaredTypes", run: checkColumnOverridesFollowDeclaredTypes},
	{name: "ColumnOverridesFollowProjectedTypes", run: checkColumnOverridesFollowProjectedTypes},
	{name: "PlaceholderSpansCoverEveryParameterToken", run: checkPlaceholderSpans},
	{name: "PlaceholdersAreParametersOfTheStatement", run: checkPlaceholdersAreParameters},
	{name: "ClauseSpansAreOrderedAndInside", run: checkClauseSpans},
	{name: "ProjectionItemsMatchOutputColumns", run: checkProjectionItems},
	{name: "InListSpansOnlyForSoleElements", run: checkInListSpans},
	{name: "ValuesTupleItemsAreCommaSeparated", run: checkValuesTupleSpans},
	{name: "OrderByPlaceIsRecorded", run: checkOrderByPlace},
	{name: "TemplatesRenderTheKeptSQL", run: checkTemplatesRenderTheKeptSQL},
	{name: "ExpandedStarsReadTheStarsColumns", run: checkExpandedStarsReadTheStarsColumns},
	{name: "ExtraTypesResolve", run: checkExtraTypesResolve},
	{name: "FlavourOptionsAreAdditive", run: checkFlavourOptionsAreAdditive},
	{name: "FlavourWithholdsWhatItsEngineLacks", run: checkFlavourWithholdsWhatItsEngineLacks},
	{name: "BuiltinTypesCarryStructuralAttributes", run: checkStructuralAttributes},
	{name: "TokeniserSpansMatchSharedScanner", run: checkTokeniserSpans},
	{name: "ReturningFollowsInfo", run: checkReturningFollowsInfo},
	{name: "GroupingModifiersLeaveKeysOut", run: checkGroupingModifiersLeaveKeysOut},
	{name: "ElementsTheEngineCanHoldNullInArePointers", run: checkElementsTheEngineCanHoldNullInArePointers},
	{name: "AggregatedNonNullElementsAreNotPointers", run: checkAggregatedNonNullElementsAreNotPointers},
	{name: "ArrayParametersHoldNullElementsWhereTheEngineCan", run: checkArrayParametersHoldNullElements},
	{name: "BracketedValuesLeaveLaterParametersTheirColumns", run: checkBracketedValuesLeaveLaterParametersTheirColumns},
	{name: "CancelledContextStopsWork", run: checkCancelledContext},
	{name: "CancellationStopsARunningAnalysis", run: checkCancellationStopsARunningAnalysis},
	{name: "IfExistsFormsReplayAsTheEngineDoes", run: checkIfExistsFormsReplayAsTheEngineDoes},
	{name: "PrimaryKeyColumnsFollowTheEngine", run: checkPrimaryKeyColumnsFollowTheEngine},
	{name: "ColumnPlacementsReplayAsTheEngineDoes", run: checkColumnPlacementsReplayAsTheEngineDoes},
	{name: "RenamesReplayAsTheEngineDoes", run: checkRenamesReplayAsTheEngineDoes},
	{name: "ViewStarsExpandToTheirColumns", run: checkViewStarsExpandToTheirColumns},
	{name: "ViewsFollowTheirTablesAsTheDialectSays", run: checkViewsFollowTheirTablesAsTheDialectSays},
	{name: "MissingSemicolonIsReported", run: checkMissingSemicolonIsReported},
	{name: "AcceptedDDLReplaysWithoutAnError", run: checkAcceptedDDLReplaysWithoutAnError},
	{name: "DerivedRelationsAreQueryable", run: checkDerivedRelationsAreQueryable},
}

// checkEnvironment is what a check knows about the subtest running it.
type checkEnvironment struct {
	// testName is the full name of the subtest.
	testName string

	// inChild is true when the subtest runs inside the subprocess of an isolated check.
	inChild bool
}

// checkOutcome is the result of one check: the problems found, or why it does not apply.
type checkOutcome = knownfailure.Outcome

// RunConformance checks a dialect's parsing, typing, capabilities and generated SQL
// contracts.
//
// Checks run as subtests. KnownFailures entries must still fail, and each must give a
// reason; unknown entries are rejected. An unusable fixture stops the test before any
// check runs. A check that panics, or whose dialect panics, fails alone, and the panic's
// stack is logged.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*Fixture) which supplies the dialect and its SQL; the suite does not
// change it.
func RunConformance(t *testing.T, fixture *Fixture) {
	t.Helper()

	if problems := validateFixture(fixture, conformanceChecks); len(problems) > 0 {
		t.Fatalf("dialecttest: invalid Fixture:\n%s", strings.Join(problems, "\n"))
	}
	failures := knownFailureTable(fixture, conformanceChecks)
	for _, check := range conformanceChecks {
		t.Run(check.name, func(t *testing.T) {
			t.Parallel()
			runConformanceCheck(t, check, fixture, failures)
		})
	}
}

// validateFixture reports what makes a fixture unusable: a missing fixture, dialect or
// flavour constructor, and a known failure that names no check or gives no reason.
//
// Takes fixture (*Fixture) which is the fixture to validate.
// Takes checks ([]conformanceCheck) which are the suite's checks.
//
// Returns []string which describes each problem.
func validateFixture(fixture *Fixture, checks []conformanceCheck) []string {
	if fixture == nil {
		return []string{"the Fixture is nil"}
	}
	var problems []string
	if fixture.NewDialect == nil {
		problems = append(problems, "Fixture.NewDialect is nil")
	}
	if fixture.Flavour != nil && fixture.Flavour.New == nil {
		problems = append(problems, "Fixture.Flavour is set but its New is nil")
	}
	return append(problems, knownFailureTable(fixture, checks).Problems()...)
}

// knownFailureTable returns the fixture's known failures as the table that judges them.
// The fixture drives one dialect, so each entry names that dialect alone.
//
// Takes fixture (*Fixture) which holds the known failures.
// Takes checks ([]conformanceCheck) which are the suite's checks.
//
// Returns *knownfailure.Table which judges each check's outcome.
func knownFailureTable(fixture *Fixture, checks []conformanceCheck) *knownfailure.Table {
	names := make([]string, 0, len(checks))
	for _, check := range checks {
		names = append(names, check.name)
	}
	return &knownfailure.Table{
		Failures: fixture.KnownFailures,
		Field:    "Fixture.KnownFailures",
		Subject:  "dialect",
		Checks:   names,
		Subjects: nil,
	}
}

// runConformanceCheck runs one check and reports its verdict, holding a known failure to
// still failing.
//
// Takes t (*testing.T) which reports test failures.
// Takes check (conformanceCheck) which is the check to run.
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
// Takes failures (*knownfailure.Table) which holds the fixture's known failures.
func runConformanceCheck(t *testing.T, check conformanceCheck, fixture *Fixture, failures *knownfailure.Table) {
	t.Helper()

	environment := checkEnvironment{
		testName: t.Name(),
		inChild:  os.Getenv(isolatedCheckEnvironment) == t.Name(),
	}
	outcome := runCheckRecovered(t.Context(), t.Logf, check, environment, fixture)
	verdict := judgeCheck(failures, check.name, outcome, environment.inChild)
	if verdict.Skip != "" {
		t.Skip(verdict.Skip)
	}
	if verdict.Note != "" {
		t.Log(verdict.Note)
	}
	for _, problem := range verdict.Errors {
		t.Error(problem)
	}
}

// judgeCheck decides what a subtest reports for a check's outcome. Inside the subprocess
// of an isolated check the problems are reported as they are, and the parent judges the
// subprocess's result.
//
// Takes failures (*knownfailure.Table) which holds the fixture's known failures.
// Takes name (string) which is the check's name.
// Takes outcome (checkOutcome) which is the check's result.
// Takes inChild (bool) which is true inside an isolated check's subprocess.
//
// Returns knownfailure.Verdict which says what to report.
func judgeCheck(failures *knownfailure.Table, name string, outcome checkOutcome, inChild bool) knownfailure.Verdict {
	record := failures.Lookup("", name)
	if inChild {
		record = knownfailure.Record{Reason: "", Known: false, Scoped: true}
	}
	return failures.Judge(name, outcome, record)
}

// runCheckRecovered runs one check, turning a panic in the check, or in the dialect it
// drives, into a failed outcome, so one broken check fails alone instead of ending the
// test binary. The panic's stack is logged once and kept out of the problem.
//
// Takes logf (func(string, ...any)) which logs the stack, such as t.Logf.
// Takes check (conformanceCheck) which is the check to run.
// Takes environment (checkEnvironment) which describes the subtest.
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns outcome (checkOutcome) which is the check's outcome, or the panic as a problem.
func runCheckRecovered(
	ctx context.Context,
	logf func(format string, arguments ...any),
	check conformanceCheck,
	environment checkEnvironment,
	fixture *Fixture,
) (outcome checkOutcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logf("%s panicked: %v\n%s", check.name, recovered, debug.Stack())
			outcome = failed("check panicked: %v", recovered)
		}
	}()
	return check.run(ctx, environment, fixture)
}

// failed reports a check that found one problem.
//
// Takes format (string) which is the problem's format string.
// Takes arguments (...any) which fill the format.
//
// Returns checkOutcome which holds the problem.
func failed(format string, arguments ...any) checkOutcome {
	return knownfailure.Failed(format, arguments...)
}

// notApplicable reports a check that does not apply to the fixture.
//
// Takes reason (string) which says why.
//
// Returns checkOutcome which carries the reason.
func notApplicable(reason string) checkOutcome {
	return knownfailure.NotApplicable(reason)
}

// outcomeOf turns a list of problems into an outcome.
//
// Takes problems ([]string) which may be empty.
//
// Returns checkOutcome which holds the problems.
func outcomeOf(problems []string) checkOutcome {
	return knownfailure.OutcomeOf(problems)
}

// directiveLine builds an sgl.query directive line in the dialect's own comment syntax,
// falling back to the SQL default when Info declares none.
//
// Takes engine (dialect.DialectPort) which supplies the comment syntax.
// Takes name (string) which is the query name.
// Takes command (string) which is the query command, such as one or exec.
//
// Returns string which is the directive line, ending in a newline.
func directiveLine(engine dialect.DialectPort, name, command string) string {
	prefix := engine.Info().Comments.LinePrefix
	if prefix == "" {
		prefix = ast.DefaultSQLCommentStyle().LinePrefix
	}
	return prefix + " sgl.query(name: " + name + ", command: " + command + ")\n"
}
