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
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// fixtureRowCount is how many rows EngineFixture.Table holds.
	fixtureRowCount = 3

	// lastRow is the identifier of the last row EngineFixture.Table holds.
	lastRow = 3

	// unmatchedCondition is a condition no row meets, which lets a conversion be checked
	// without converting any value.
	unmatchedCondition = "1 = 0"

	// failedFormat reports a statement the engine rejected and why.
	failedFormat = "%s failed: %v"
)

// EngineFixture is a table of a running database, which RunEngineFacts checks a dialect's
// Info against.
type EngineFixture struct {
	// Database is a connection to the engine.
	Database *sql.DB

	// Table names a table holding exactly three rows, (1, 'one'), (2, 'two') and (3,
	// 'three'), in a 64-bit integer column id and a text column label.
	Table string

	// Info is the Info of the dialect for the engine.
	Info dialect.Info
}

// aliasOperandFact is a query that writes a select-list alias as an operand of an
// expression in one clause, which the engine accepts where Info.SelectAliasOperands lists
// the clause.
type aliasOperandFact struct {
	// query is the query, with %s standing for the fixture's table.
	query string

	// clause is the clause the alias is an operand in.
	clause ast.Clause
}

var (
	// aliasOperandFacts holds queries testing select-list aliases within expressions for
	// each supported clause.
	aliasOperandFacts = []aliasOperandFact{
		{clause: ast.ClauseWhere, query: "SELECT id + 1 AS bumped FROM %s WHERE bumped + 0 > 1"},
		{clause: ast.ClauseGroupBy, query: "SELECT 1 AS k, count(*) AS c FROM %s GROUP BY k + 1"},
		{clause: ast.ClauseHaving, query: "SELECT count(*) AS c FROM %s HAVING c + 0 > 0"},
		{clause: ast.ClauseOrderBy, query: "SELECT id + 1 AS bumped FROM %s ORDER BY bumped * 2"},
	}
)

// RunEngineFacts checks generated list, pagination and cast syntax, how the engine treats
// a column read outside every aggregate of a query without GROUP BY, that it accepts a
// select-list alias as an operand in each clause Info.SelectAliasOperands lists, and
// whether a view's star follows its table, against a live engine.
//
// An unusable fixture stops the test before any fact is checked.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database, its table and the Info.
// RunEngineFacts does not change it.
func RunEngineFacts(t *testing.T, fixture *EngineFixture) {
	t.Helper()

	if problems := validateEngineFixture(fixture); len(problems) > 0 {
		t.Fatalf("dialecttest: invalid EngineFixture:\n%s", strings.Join(problems, "\n"))
	}
	t.Run("EmptyListTestsMatchNoRowAndEveryRow", func(t *testing.T) {
		checkEmptyLists(t, fixture)
	})
	t.Run("ArrayFormsTestMembership", func(t *testing.T) {
		checkArrayForms(t, fixture)
	})
	t.Run("PaginationLimitsAndSkipsRows", func(t *testing.T) {
		checkPagination(t, fixture)
	})
	t.Run("CastsConvertToEveryListedType", func(t *testing.T) {
		checkCasts(t, fixture)
	})
	t.Run("UngroupedColumnsFollowTheEngine", func(t *testing.T) {
		checkUngroupedColumns(t, fixture)
	})
	t.Run("SelectAliasOperandsAreAcceptedByTheEngine", func(t *testing.T) {
		checkSelectAliasOperands(t, fixture)
	})
	t.Run("ViewsFollowTheirTablesAsTheInfoSays", func(t *testing.T) {
		checkViewsFollowTheirTables(t, fixture)
	})
}

// validateEngineFixture reports what makes an engine fixture unusable: a missing fixture,
// database or table name.
//
// Takes fixture (*EngineFixture) which is the fixture to validate.
//
// Returns []string which describes each problem.
func validateEngineFixture(fixture *EngineFixture) []string {
	if fixture == nil {
		return []string{"the EngineFixture is nil"}
	}
	var problems []string
	if fixture.Database == nil {
		problems = append(problems, "EngineFixture.Database is nil")
	}
	if strings.TrimSpace(fixture.Table) == "" {
		problems = append(problems, "EngineFixture.Table names no table")
	}
	return problems
}

// checkViewsFollowTheirTables checks whether views reflect later table changes as
// Info.ViewsFollowTheirTables specifies. It adds a column, so it runs after the other
// checks.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database, its table and the Info.
func checkViewsFollowTheirTables(t *testing.T, fixture *EngineFixture) {
	t.Helper()

	view := fixture.Table + "_star"
	for _, statement := range []string{
		"CREATE VIEW " + view + " AS SELECT * FROM " + fixture.Table,
		"ALTER TABLE " + fixture.Table + " ADD COLUMN extra INTEGER",
	} {
		if _, err := fixture.Database.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf(failedFormat, statement, err)
		}
	}
	query := "SELECT * FROM " + view + " WHERE " + unmatchedCondition
	columns := resultColumns(t, fixture, query)
	want := []string{"id", "label"}
	if fixture.Info.ViewsFollowTheirTables {
		want = append(want, "extra")
	}
	if !slices.Equal(columns, want) {
		t.Errorf("%s gave the columns %v, want %v as Info.ViewsFollowTheirTables is %v", query, columns, want, fixture.Info.ViewsFollowTheirTables)
	}
}

// checkSelectAliasOperands checks Info.SelectAliasOperands against the engine: in each
// clause it lists, the engine runs a query that writes a select-list alias as an operand
// of an expression.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database and the Info.
func checkSelectAliasOperands(t *testing.T, fixture *EngineFixture) {
	t.Helper()

	for _, fact := range aliasOperandFacts {
		if fixture.Info.SelectAliasOperands.Includes(fact.clause) {
			requireAccepted(t, fixture, fmt.Sprintf(fact.query, fixture.Table))
		}
	}
}

// checkUngroupedColumns checks Info.UngroupedColumns against the engine: a query reading
// label beside count(*) without GROUP BY and matching no row is rejected where the
// dialect says the engine rejects it, in its default mode or always, and otherwise gives
// one row whose label is NULL.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database and the Info.
func checkUngroupedColumns(t *testing.T, fixture *EngineFixture) {
	t.Helper()

	query := "SELECT label, count(*) FROM " + fixture.Table + " WHERE " + unmatchedCondition
	labels, err := selectUngroupedLabels(t, fixture, query)
	switch fixture.Info.UngroupedColumns {
	case dialect.UngroupedColumnsRejected, dialect.UngroupedColumnsWarned:
		if err == nil {
			t.Errorf("%s gave %d rows, but Info.UngroupedColumns says the engine rejects it in its default mode", query, len(labels))
		}
	default:
		if err != nil {
			t.Errorf("%s failed, but Info.UngroupedColumns says the engine returns its one row: %v", query, err)
			return
		}
		if len(labels) != 1 || labels[0].Valid {
			t.Errorf("%s gave %v, want one row whose label is NULL", query, labels)
		}
	}
}

// selectUngroupedLabels runs a query whose first column is the fixture's label and whose
// second is a count, and returns the labels in the order the rows arrive.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database.
// Takes query (string) which is the query.
//
// Returns []sql.NullString which holds the labels.
// Returns error when the engine rejects the query or a row cannot be read.
func selectUngroupedLabels(t *testing.T, fixture *EngineFixture, query string) ([]sql.NullString, error) {
	t.Helper()

	rows, err := fixture.Database.QueryContext(t.Context(), query)
	if err != nil {
		return nil, err
	}
	defer closeRows(t, rows)
	var labels []sql.NullString
	for rows.Next() {
		var label sql.NullString
		var count any
		if err := rows.Scan(&label, &count); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

// checkEmptyLists checks that the test written for IN an empty list matches no row and
// the one for NOT IN an empty list matches every row.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database and the Info.
func checkEmptyLists(t *testing.T, fixture *EngineFixture) {
	t.Helper()

	lists := fixture.Info.Lists
	for _, column := range []string{"id", "label"} {
		for test, expected := range map[string]int{lists.EmptyMembership: 0, lists.EmptyExclusion: fixtureRowCount} {
			query := "SELECT id FROM " + fixture.Table + " WHERE " + column + " " + test
			if found := len(selectIdentifiers(t, fixture, query)); found != expected {
				t.Errorf("%s matched %d rows, want %d", query, found, expected)
			}
		}
	}
}

// checkArrayForms checks the array membership and exclusion forms against an array of one
// element and an empty one.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database and the Info.
func checkArrayForms(t *testing.T, fixture *EngineFixture) {
	t.Helper()

	lists := fixture.Info.Lists
	if lists.ArrayMembership == (dialect.Affix{}) {
		t.Skip("the dialect has no array forms")
	}
	placeholder := fixture.Info.Placeholders.Execution.Render(1, "values", "")
	testCases := []struct {
		affix    dialect.Affix
		values   []int64
		expected []int64
	}{
		{affix: lists.ArrayMembership, values: []int64{2}, expected: []int64{2}},
		{affix: lists.ArrayMembership, values: []int64{}, expected: nil},
		{affix: lists.ArrayExclusion, values: []int64{2}, expected: []int64{1, lastRow}},
		{affix: lists.ArrayExclusion, values: []int64{}, expected: []int64{1, 2, lastRow}},
	}
	for _, testCase := range testCases {
		query := "SELECT id FROM " + fixture.Table + " WHERE id " + testCase.affix.Before + placeholder + testCase.affix.After + " ORDER BY id"
		if found := selectIdentifiers(t, fixture, query, testCase.values); !slices.Equal(found, testCase.expected) {
			t.Errorf("%s with %v matched %v, want %v", query, testCase.values, found, testCase.expected)
		}
	}
}

// checkPagination checks a limit alone, an offset alone and both together.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database and the Info.
func checkPagination(t *testing.T, fixture *EngineFixture) {
	t.Helper()

	testCases := []struct {
		limit    string
		offset   string
		expected []int64
	}{
		{limit: "2", offset: "", expected: []int64{1, 2}},
		{limit: "", offset: "1", expected: []int64{2, lastRow}},
		{limit: "1", offset: "1", expected: []int64{2}},
	}
	for _, testCase := range testCases {
		query := "SELECT id FROM " + fixture.Table + " ORDER BY id " + paginationClause(&fixture.Info.Pagination, testCase.limit, testCase.offset)
		if found := selectIdentifiers(t, fixture, query); !slices.Equal(found, testCase.expected) {
			t.Errorf("%s returned %v, want %v", query, found, testCase.expected)
		}
	}
}

// checkCasts checks that every conversion form accepts every listed type.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database and the Info.
func checkCasts(t *testing.T, fixture *EngineFixture) {
	t.Helper()

	filters := fixture.Info.Filters
	if len(filters.Casts) == 0 {
		t.Skip("the dialect lists no conversions")
	}
	for _, cast := range filters.Casts {
		for _, castType := range filters.CastTypes {
			expression := cast.Before + "label" + cast.Middle + castType + cast.After
			requireAccepted(t, fixture, "SELECT "+expression+" FROM "+fixture.Table+" WHERE "+unmatchedCondition)
		}
	}
}

// paginationClause writes a limit, an offset or both as the dialect does, a lone offset
// after the unbounded limit when the dialect needs one.
//
// Takes pagination (*dialect.PaginationSyntax) which is the dialect's form.
// Takes limit (string) which is the limit, or empty for none.
// Takes offset (string) which is the offset, or empty for none.
//
// Returns string which is the clause.
func paginationClause(pagination *dialect.PaginationSyntax, limit, offset string) string {
	if limit == "" && offset != "" {
		limit = pagination.UnboundedLimit
	}
	var limitClause, offsetClause string
	if limit != "" {
		limitClause = pagination.Limit.Before + limit + pagination.Limit.After
	}
	if offset != "" {
		offsetClause = pagination.Offset.Before + offset + pagination.Offset.After
	}
	if pagination.OffsetFirst {
		limitClause, offsetClause = offsetClause, limitClause
	}
	if limitClause == "" || offsetClause == "" {
		return limitClause + offsetClause
	}
	return limitClause + " " + offsetClause
}

// selectIdentifiers runs a query whose one column is the fixture's id and returns the
// identifiers in the order the rows arrive.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database.
// Takes query (string) which is the query.
// Takes arguments (...any) which are its arguments.
//
// Returns []int64 which holds the identifiers, or nil when no row matched or the query
// failed, which is reported.
func selectIdentifiers(t *testing.T, fixture *EngineFixture, query string, arguments ...any) []int64 {
	t.Helper()

	rows, err := fixture.Database.QueryContext(t.Context(), query, arguments...)
	if err != nil {
		t.Errorf(failedFormat, query, err)
		return nil
	}
	defer closeRows(t, rows)
	var identifiers []int64
	for rows.Next() {
		var identifier int64
		if err := rows.Scan(&identifier); err != nil {
			t.Errorf("%s: reading a row: %v", query, err)
			return nil
		}
		identifiers = append(identifiers, identifier)
	}
	if err := rows.Err(); err != nil {
		t.Errorf("%s: %v", query, err)
	}
	return identifiers
}

// resultColumns runs a query and returns the names of the columns it gives, stopping the
// test when the engine rejects it.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database.
// Takes query (string) which is the query.
//
// Returns []string which holds the column names.
func resultColumns(t *testing.T, fixture *EngineFixture, query string) []string {
	t.Helper()

	rows, err := fixture.Database.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatalf(failedFormat, query, err)
	}
	defer closeRows(t, rows)
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("%s: reading its columns: %v", query, err)
	}
	return columns
}

// requireAccepted runs a query and reads its rows, reporting a failure.
//
// Takes t (*testing.T) which reports test failures.
// Takes fixture (*EngineFixture) which supplies the database.
// Takes query (string) which is the query.
func requireAccepted(t *testing.T, fixture *EngineFixture, query string) {
	t.Helper()

	rows, err := fixture.Database.QueryContext(t.Context(), query)
	if err != nil {
		t.Errorf(failedFormat, query, err)
		return
	}
	defer closeRows(t, rows)
	for rows.Next() {
		continue
	}
	if err := rows.Err(); err != nil {
		t.Errorf("%s: %v", query, err)
	}
}

// closeRows closes a result set, reporting a failure to close it.
//
// Takes t (*testing.T) which reports test failures.
// Takes rows (*sql.Rows) which is the result set.
func closeRows(t *testing.T, rows *sql.Rows) {
	t.Helper()

	if err := rows.Close(); err != nil {
		t.Errorf("closing rows: %v", err)
	}
}
