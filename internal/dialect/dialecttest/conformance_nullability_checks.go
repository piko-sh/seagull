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
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// nullabilityQueriesFile is the query file the nullability checks generate from.
	nullabilityQueriesFile = "nullability.sql"
)

// nullabilityQuery is a query a nullability check runs and whether each column it gives
// may be NULL.
type nullabilityQuery struct {
	// nullable maps columns the query gives, by their lower-cased names, to whether they
	// must be nullable.
	nullable map[string]bool

	// follows maps columns the query gives, by their lower-cased names, to the column whose
	// nullability they must have, for a column that may be NULL exactly where an operand is,
	// whatever nullability the dialect gives the operand.
	follows map[string]string

	// name is the query's name.
	name string

	// sql is the query, without its directive.
	sql string
}

var (
	// outerJoinedPredicateQuery tests, with each kind of predicate, columns the outer join
	// of the items table with itself may leave NULL where the second copy has no row, and
	// tests one for NULL, which is never NULL itself. It also selects the columns tested,
	// whose nullability each predicate must have.
	outerJoinedPredicateQuery = nullabilityQuery{
		name: "PredicatesOverNullableOperands",
		sql: "SELECT b.id AS probed_id, b.name AS probed_name, b.id = 1 AS compared, b.id IN (1, 2) AS listed, " +
			"b.id BETWEEN 1 AND 2 AS ranged, b.name LIKE 'x%' AS matched, NOT (b.id = 1) AS negated, " +
			"b.id = 1 OR a.id = 1 AS either, b.id IS NULL AS missing FROM items a LEFT JOIN items b ON a.id = b.id",
		nullable: map[string]bool{"missing": false},
		follows: map[string]string{
			"compared": "probed_id", "listed": "probed_id", "ranged": "probed_id", "matched": "probed_name",
			"negated": "probed_id", "either": "probed_id",
		},
	}

	// ungroupedQuery reads the identifier beside count(*) without GROUP BY, which an engine
	// that returns its one row gives as NULL when it reads no row.
	ungroupedQuery = nullabilityQuery{name: "Ungrouped", sql: "SELECT id, count(*) AS total FROM items",
		nullable: map[string]bool{"id": true, "total": false}, follows: nil}

	// groupedQueries aggregate alone, or group by the column they read, neither of which
	// makes a column nullable.
	groupedQueries = []nullabilityQuery{
		{name: "AggregatesAlone", sql: "SELECT count(*) AS total FROM items", nullable: map[string]bool{"total": false}, follows: nil},
		{name: "GroupedByTheColumn", sql: "SELECT id, count(*) AS total FROM items GROUP BY id",
			nullable: map[string]bool{"id": false, "total": false}, follows: nil},
	}

	// rightJoinBaselineQuery reads a table before a RIGHT join of a table, which every
	// dialect makes as nullable as its engine's RIGHT join leaves it.
	rightJoinBaselineQuery = "SELECT u.id AS table_id FROM items u RIGHT JOIN items t ON u.id = t.id"

	// outerJoinedRelationQueries put a CTE and a derived table beside a table before a RIGHT
	// join, of a CTE or of a table, which leaves all of them as nullable as a table before a
	// RIGHT join of a table.
	outerJoinedRelationQueries = []nullabilityQuery{
		{
			name: "CTEBeforeARightJoin",
			sql: "WITH c AS (SELECT id, name FROM items), r AS (SELECT id FROM items) SELECT c.id AS cte_id, u.id AS table_id " +
				"FROM c JOIN items u ON c.id = u.id RIGHT JOIN r ON u.id = r.id",
			nullable: nil,
			follows:  map[string]string{"cte_id": "table_id"},
		},
		{
			name: "DerivedTableBeforeARightJoin",
			sql: "SELECT d.id AS derived_id, u.id AS table_id " +
				"FROM (SELECT id, name FROM items) d JOIN items u ON d.id = u.id RIGHT JOIN items t ON u.id = t.id",
			nullable: nil,
			follows:  map[string]string{"derived_id": "table_id"},
		},
	}
)

// checkUngroupedColumnsFollowTheDialect checks diagnostics and nullability for ungrouped
// aggregate queries against Info.UngroupedColumns.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkUngroupedColumnsFollowTheDialect(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if !strings.Contains(fixture.BaseMigration, fixtureTable) {
		return notApplicable("the fixture sets no BaseMigration creating items")
	}
	engine := fixture.NewDialect()
	rule := engine.Info().UngroupedColumns
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{nullabilityQueriesFile: directiveLine(engine, ungroupedQuery.name, "one") + ungroupedQuery.sql + ";\n"})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	problems := ungroupedDiagnosticProblems(result.Diagnostics, rule)
	if rule != dialect.UngroupedColumnsRejected {
		problems = append(problems, nullabilityProblems(findQuery(emitter.queries, ungroupedQuery.name), ungroupedQuery)...)
	}
	outcome := runNullabilityQueries(ctx, fixture, fixture.BaseMigration, groupedQueries)
	return outcomeOf(append(problems, outcome.Problems...))
}

// ungroupedDiagnosticProblems compares the diagnostics of the ungrouped column check's
// queries with those the dialect's rule calls for: one ungrouped column diagnostic, an
// error where the engine rejects the query and a warning where its mode decides, or none
// where the engine returns the row.
//
// Takes diagnostics ([]diag.SourceError) which are the diagnostics reported.
// Takes rule (dialect.UngroupedColumnRule) which is the dialect's rule.
//
// Returns []string which describes each problem found.
func ungroupedDiagnosticProblems(diagnostics []diag.SourceError, rule dialect.UngroupedColumnRule) []string {
	want := map[dialect.UngroupedColumnRule]diag.ErrorSeverity{
		dialect.UngroupedColumnsRejected: diag.SeverityError,
		dialect.UngroupedColumnsWarned:   diag.SeverityWarning,
	}
	severity, reported := want[rule]
	switch {
	case !reported && len(diagnostics) > 0:
		return []string{"where the engine returns the row, the queries must analyse without diagnostics, got " +
			formatDiagnostics(diagnostics)}
	case reported && (len(diagnostics) != 1 || diagnostics[0].Code != diag.CodeUngroupedColumn ||
		diagnostics[0].Severity != severity):
		return []string{fmt.Sprintf("the column id read beside count(*) without GROUP BY must be reported once as %s %s, got %s",
			diag.CodeUngroupedColumn, severity, formatDiagnostics(diagnostics))}
	default:
		return nil
	}
}

// checkOuterJoinedRelationsAreNullable checks that outer joins make tables, CTEs and
// derived tables nullable regardless of relation registration order.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkOuterJoinedRelationsAreNullable(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if !strings.Contains(fixture.BaseMigration, fixtureTable) {
		return notApplicable("the fixture sets no BaseMigration creating items")
	}
	engine := fixture.NewDialect()
	emitter, _, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{nullabilityQueriesFile: directiveLine(engine, "RightJoinBaseline", "many") + rightJoinBaselineQuery + ";\n"})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	baseline := findQuery(emitter.queries, "RightJoinBaseline")
	if baseline == nil || len(baseline.OutputColumns) != 1 {
		return failed("%q must be analysed into one column", rightJoinBaselineQuery)
	}
	queries := make([]nullabilityQuery, len(outerJoinedRelationQueries))
	for index, query := range outerJoinedRelationQueries {
		query.nullable = map[string]bool{"table_id": baseline.OutputColumns[0].Nullable}
		queries[index] = query
	}
	return runNullabilityQueries(ctx, fixture, fixture.BaseMigration, queries)
}

// checkPredicatesOverNullableOperandsAreNullable checks predicate nullability under SQL's
// three-valued logic. IS NULL tests must remain non-nullable.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkPredicatesOverNullableOperandsAreNullable(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runNullabilityQueries(ctx, fixture, fixture.BaseMigration, []nullabilityQuery{outerJoinedPredicateQuery})
}

// runNullabilityQueries generates a set of queries over a migration and checks that each
// analyses without diagnostics into columns whose nullability is the query's.
//
// Takes fixture (*Fixture) which supplies the dialect.
// Takes migration (string) which is the migration the queries read.
// Takes queries ([]nullabilityQuery) which are the queries.
//
// Returns checkOutcome which holds any problem found.
func runNullabilityQueries(ctx context.Context, fixture *Fixture, migration string, queries []nullabilityQuery) checkOutcome {
	if !strings.Contains(migration, fixtureTable) {
		return notApplicable("the fixture sets no BaseMigration creating items")
	}
	engine := fixture.NewDialect()
	var source strings.Builder
	for _, query := range queries {
		source.WriteString(directiveLine(engine, query.name, "many") + query.sql + ";\n\n")
	}
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: migration},
		map[string]string{nullabilityQueriesFile: source.String()})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	var problems []string
	if len(result.Diagnostics) > 0 {
		problems = append(problems, "the queries must analyse without diagnostics, got "+formatDiagnostics(result.Diagnostics))
	}
	for _, query := range queries {
		problems = append(problems, nullabilityProblems(findQuery(emitter.queries, query.name), query)...)
	}
	return outcomeOf(problems)
}

// nullabilityProblems compares the nullability of an analysed query's columns with the
// nullability they must have.
//
// Takes analysed (*sqlmodel.AnalysedQuery) which is the analysed query, or nil.
// Takes query (nullabilityQuery) which is the query and its columns' nullability.
//
// Returns []string which describes each problem found.
func nullabilityProblems(analysed *sqlmodel.AnalysedQuery, query nullabilityQuery) []string {
	if analysed == nil {
		return []string{fmt.Sprintf("%q was not analysed", query.sql)}
	}
	columns := make(map[string]bool, len(analysed.OutputColumns))
	for index := range analysed.OutputColumns {
		columns[strings.ToLower(analysed.OutputColumns[index].Name)] = analysed.OutputColumns[index].Nullable
	}
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(query.nullable)) {
		problems = append(problems, columnNullabilityProblem(columns, query, name, query.nullable[name], "")...)
	}
	for _, name := range slices.Sorted(maps.Keys(query.follows)) {
		operand, found := columns[query.follows[name]]
		if !found {
			problems = append(problems, fmt.Sprintf("%q must give a column %s", query.sql, query.follows[name]))
			continue
		}
		problems = append(problems, columnNullabilityProblem(columns, query, name, operand, ", as "+query.follows[name]+" is")...)
	}
	return problems
}

// columnNullabilityProblem compares the nullability of one column with the nullability it
// must have.
//
// Takes columns (map[string]bool) which maps the query's columns, by their lower-cased
// names, to their nullability.
// Takes query (nullabilityQuery) which is the query.
// Takes name (string) which is the column's lower-cased name.
// Takes want (bool) which is the nullability it must have.
// Takes reason (string) which says why, or is empty.
//
// Returns []string which describes the problem, or is empty.
func columnNullabilityProblem(columns map[string]bool, query nullabilityQuery, name string, want bool, reason string) []string {
	got, found := columns[name]
	switch {
	case !found:
		return []string{fmt.Sprintf("%q must give a column %s", query.sql, name)}
	case got != want:
		return []string{fmt.Sprintf("the column %s of %q must be nullable %v%s, got nullable %v", name, query.sql, want, reason, got)}
	default:
		return nil
	}
}
