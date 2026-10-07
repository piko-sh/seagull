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

package emitkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func values(children ...sqlmodel.SQLSegment) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentValuesRow, Children: children}
}

func batchQuery(command sqlmodel.QueryCommand, segments ...sqlmodel.SQLSegment) *sqlmodel.AnalysedQuery {
	query := testQuery()
	query.Name = "InsertBirds"
	query.Command = command
	query.Template = template(segments...)
	return query
}

func insertBirds(row ...sqlmodel.SQLSegment) []sqlmodel.SQLSegment {
	return []sqlmodel.SQLSegment{text("INSERT INTO birds (id, name) VALUES "), values(row...)}
}

func TestCheckBatchRefusesWhatCannotBeRepeatedPerRow(t *testing.T) {
	t.Parallel()

	lone := insertBirds(text("("), parameter(0), text(", "), parameter(1), text(")"))
	expression := insertBirds(text("("), parameter(0), text(", lower("), parameter(1), text("))"))

	testCases := []struct {
		name     string
		segments []sqlmodel.SQLSegment
		want     []string
		shape    emitkit.BatchShape
	}{
		{name: "a row of lone placeholders repeats", segments: lone, shape: emitkit.BatchRows},
		{name: "a row of lone placeholders sends columns", segments: lone, shape: emitkit.BatchColumns},
		{name: "a row of expressions repeats", segments: expression, shape: emitkit.BatchRows},
		{name: "a row with spaces around its placeholders sends columns", shape: emitkit.BatchColumns,
			segments: insertBirds(text("( "), parameter(0), text(" ,\n "), parameter(1), text(" )"))},
		{
			name: "a statement without a VALUES row", shape: emitkit.BatchRows,
			segments: []sqlmodel.SQLSegment{text("INSERT INTO birds (id) SELECT "), parameter(0)},
			want:     []string{"command: batch repeats the statement's VALUES row for each row, so it needs an INSERT with exactly one VALUES row"},
		},
		{
			name: "a parameter after the VALUES row", shape: emitkit.BatchRows,
			segments: append(insertBirds(text("("), parameter(0), text(")")), text(" ON CONFLICT (id) DO UPDATE SET name = "), parameter(1)),
			want: []string{"command: batch binds parameter name outside its VALUES row, which a statement of many rows would bind " +
				"once for all of them; refer to the inserted value through the engine's own form, such as EXCLUDED.column or VALUES(column)"},
		},
		{
			name: "a slice parameter in the VALUES row", shape: emitkit.BatchRows,
			segments: insertBirds(text("("), list(2, "IN ", false), text(")")),
			want:     []string{"command: batch writes the slice parameter ids in its VALUES row, which takes one value per column"},
		},
		{
			name: "an expression where columns are sent", shape: emitkit.BatchColumns, segments: expression,
			want: []string{"command: batch sends each row's values as columns, so every item of its VALUES row must be a placeholder alone, each parameter written once"},
		},
		{
			name: "a literal where columns are sent", shape: emitkit.BatchColumns,
			segments: insertBirds(text("("), parameter(0), text(", 'x')")),
			want:     []string{"command: batch sends each row's values as columns, so every item of its VALUES row must be a placeholder alone, each parameter written once"},
		},
		{
			name: "a parameter written twice where columns are sent", shape: emitkit.BatchColumns,
			segments: insertBirds(text("("), parameter(0), text(", "), parameter(0), text(")")),
			want:     []string{"command: batch sends each row's values as columns, so every item of its VALUES row must be a placeholder alone, each parameter written once"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := batchQuery(sqlmodel.QueryCommandBatch, testCase.segments...)
			problems := emitkit.CheckBatch(query, testCase.shape)

			messages := make([]string, 0, len(problems))
			for _, problem := range problems {
				require.Same(t, query, problem.Query)
				require.Equal(t, diag.CodeBatchShape, problem.Code)
				require.Equal(t, diag.SeverityError, problem.Severity)
				messages = append(messages, problem.Message)
			}
			if testCase.want == nil {
				require.Empty(t, messages)
				return
			}
			require.Equal(t, testCase.want, messages)
		})
	}
}

func TestCheckBatchRefusesARowBindingMoreVariablesThanAStatementBinds(t *testing.T) {
	t.Parallel()

	numbered := dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}
	anonymous := dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous}
	reused := insertBirds(text("("), parameter(0), text(", "), parameter(1), text(", "), parameter(0), text(")"))

	testCases := []struct {
		name   string
		limits []emitkit.BindLimit
		want   []string
	}{
		{name: "no limit checks nothing"},
		{name: "a row within a numbered limit", limits: []emitkit.BindLimit{{Form: numbered, Limit: 2}}},
		{name: "a limit of zero checks nothing", limits: []emitkit.BindLimit{{Form: anonymous, Limit: 0}}},
		{
			name: "a row past an anonymous limit", limits: []emitkit.BindLimit{{Form: anonymous, Limit: 2}},
			want: []string{"command: batch binds 3 variables in each VALUES row, more than the 2 one statement of the driver binds, " +
				"so not even one row could be sent; insert fewer columns, or write the values every row shares into the SQL"},
		},
		{
			name: "a row past a numbered limit", limits: []emitkit.BindLimit{{Form: numbered, Limit: 1}},
			want: []string{"command: batch binds 2 variables in each VALUES row, more than the 1 one statement of the driver binds, " +
				"so not even one row could be sent; insert fewer columns, or write the values every row shares into the SQL"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := batchQuery(sqlmodel.QueryCommandBatch, reused...)
			problems := emitkit.CheckBatch(query, emitkit.BatchRows, testCase.limits...)

			messages := make([]string, 0, len(problems))
			for _, problem := range problems {
				require.Same(t, query, problem.Query)
				require.Equal(t, diag.CodeBatchShape, problem.Code)
				require.Equal(t, diag.SeverityError, problem.Severity)
				messages = append(messages, problem.Message)
			}
			if testCase.want == nil {
				require.Empty(t, messages)
				return
			}
			require.Equal(t, testCase.want, messages)
		})
	}
}

func TestRowColumnsMapsEachItemToItsParameterByPosition(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		segments []sqlmodel.SQLSegment
		want     []int
		ok       bool
	}{
		{name: "items in parameter order", segments: insertBirds(text("("), parameter(0), text(", "), parameter(1), text(")")), want: []int{0, 1}, ok: true},
		{name: "items in another order than the parameters", segments: insertBirds(text("("), parameter(1), text(", "), parameter(0), text(")")), want: []int{1, 0}, ok: true},
		{name: "one item", segments: insertBirds(text("("), parameter(4), text(")")), want: []int{4}, ok: true},
		{name: "an expression", segments: insertBirds(text("(lower("), parameter(1), text("))"))},
		{name: "no VALUES row", segments: []sqlmodel.SQLSegment{text("SELECT 1")}},
		{name: "an empty row", segments: insertBirds(text("()"))},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns, ok := emitkit.RowColumns(batchQuery(sqlmodel.QueryCommandCopyFrom, testCase.segments...))
			require.Equal(t, testCase.ok, ok)
			if testCase.ok {
				require.Equal(t, testCase.want, columns)
			}
		})
	}
}
