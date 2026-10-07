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

package source

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func allowedColumns() map[string]string {
	return map[string]string{"id": `"t"."id"`, "data": `"t"."data"`, "café": `"t"."café"`}
}

func TestResolveColumnWritesTheQualifiedSourceOfASelectedColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		column string
		want   string
	}{
		{column: "id", want: `"t"."id"`},
		{column: "  id  ", want: `"t"."id"`},
		{column: "café", want: `"t"."café"`},
		{column: "convert(id to whole)", want: `CONVERT("t"."id" TO WHOLE)`},
		{column: "CONVERT( data  TO  Text )", want: `CONVERT("t"."data" TO TEXT)`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.column, func(t *testing.T) {
			t.Parallel()

			resolved, ok := sglResolveColumn(testCase.column, allowedColumns())

			require.True(t, ok)
			require.Equal(t, testCase.want, resolved)
		})
	}
}

func TestResolveColumnRejectsWhatTheGrammarOrTheQueryDoesNotAllow(t *testing.T) {
	t.Parallel()

	for _, column := range []string{
		"",
		"   ",
		"2fast",
		"name",
		"id OR 1=1",
		"id;DROP TABLE users",
		"id UNION SELECT password FROM users",
		"data --comment",
		"data /* hidden */",
		"convert(id to blob)",
		"convert(name to whole)",
		"CAST(id AS whole)",
		"1=1",
		strings.Repeat("a", sglMaxColumnExpressionLength+1),
	} {
		_, ok := sglResolveColumn(column, allowedColumns())
		require.Falsef(t, ok, "expected %q to be rejected", column)
	}
}

func TestExtractColumnRootFindsTheColumnAnExpressionReadsAndWhereItStarts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		expression string
		want       string
		wantStart  int
	}{
		{expression: "version_data", want: "version_data", wantStart: 0},
		{expression: "  version_data  ", want: "version_data", wantStart: 2},
		{expression: "version_data~>'foo'", want: "version_data", wantStart: 0},
		{expression: "version_data~>'meta'~>'kind'", want: "version_data", wantStart: 0},
		{expression: "path(version_data, 'foo')", want: "version_data", wantStart: 5},
		{expression: "PATH( version_data , 'foo')", want: "version_data", wantStart: 6},
		{expression: " path(a, 'foo')", want: "a", wantStart: 6},
		{expression: "path(at, 'path')", want: "at", wantStart: 5},
		{expression: "café", want: "café", wantStart: 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.expression, func(t *testing.T) {
			t.Parallel()

			root, start := sglExtractColumnRoot(testCase.expression)

			require.Equal(t, testCase.want, root)
			require.Equal(t, testCase.wantStart, start)
			require.Equal(t, root, testCase.expression[start:start+len(root)])
		})
	}
}

func TestResolveColumnWritesTheSourceWhereTheColumnStandsNotWhereItsNameFirstAppears(t *testing.T) {
	t.Parallel()

	allowed := map[string]string{"a": `"t"."a"`, "at": `"t"."at"`, "th": `"t"."th"`, "path": `"t"."path"`, "data": `"t"."data"`}
	testCases := []struct {
		column string
		want   string
	}{
		{column: "path(a, '$.a')", want: `path("t"."a", '$.a')`},
		{column: "path(at, '$.at')", want: `path("t"."at", '$.at')`},
		{column: "PATH(th,'th')", want: `PATH("t"."th",'th')`},
		{column: "path(path, 'path')", want: `path("t"."path", 'path')`},
		{column: "a~>'a'~>'a'", want: `"t"."a"~>'a'~>'a'`},
		{column: "  data~>0  ", want: `"t"."data"~>0`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.column, func(t *testing.T) {
			t.Parallel()

			resolved, ok := sglResolveColumn(testCase.column, allowed)

			require.True(t, ok)
			require.Equal(t, testCase.want, resolved)
		})
	}
}

func TestResolveColumnRefusesABackslashOrANulInAQuotedLiteral(t *testing.T) {
	t.Parallel()

	for _, column := range []string{
		`data~>'\'`,
		`data~>'\\'`,
		`data~>'a\'`,
		`data~>'\\'~>'x'`,
		`path(data, '\\')`,
		`path(data, '$.a\\')`,
		"data~>'\x00'",
		"path(data, '\x00')",
		"data\x00",
	} {
		_, ok := sglResolveColumn(column, allowedColumns())
		require.Falsef(t, ok, "expected %q to be rejected", column)
	}
}

func TestResolveColumnRefusesABackslashOrANulWhateverTheGrammarAllows(t *testing.T) {
	grammar := sglColumnExpressionRegex
	sglColumnExpressionRegex = regexp.MustCompile(`(?s)^.*$`)
	t.Cleanup(func() { sglColumnExpressionRegex = grammar })

	testCases := []struct {
		column string
		want   bool
	}{
		{column: "data~>'kind'", want: true},
		{column: `data~>'\'`, want: false},
		{column: `path(data, '\')`, want: false},
		{column: "data~>'\x00'", want: false},
		{column: "id\x00", want: false},
	}
	for _, testCase := range testCases {
		_, ok := sglResolveColumn(testCase.column, allowedColumns())
		require.Equalf(t, testCase.want, ok, "resolving %q", testCase.column)
	}
}

func TestNewPredicateBuildsTheConditionACallerAsksFor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value    any
		name     string
		operator string
		want     sglPredicate
	}{
		{name: "a binary operator binds its value", operator: "=", value: 7, want: sglPredicate{text: `"t"."id" = `, values: []any{7}, list: false}},
		{name: "a unary operator binds nothing", operator: "IS NULL", value: "ignored", want: sglPredicate{text: `"t"."id" IS NULL`, values: nil, list: false}},
		{name: "IN binds each element as a list", operator: "IN", value: []int{1, 2}, want: sglPredicate{text: `"t"."id" IN `, values: []any{1, 2}, list: true}},
		{name: "NOT IN binds an array's elements", operator: "NOT IN", value: [2]string{"a", "b"}, want: sglPredicate{text: `"t"."id" NOT IN `, values: []any{"a", "b"}, list: true}},
		{name: "IN binds a byte slice as one value", operator: "IN", value: []byte("blob"), want: sglPredicate{text: `"t"."id" IN `, values: []any{[]byte("blob")}, list: true}},
		{name: "an empty IN list writes the empty membership test", operator: "IN", value: []int{}, want: sglPredicate{text: `"t"."id" ` + sglEmptyMembership, values: nil, list: false}},
		{name: "a nil NOT IN value writes the empty exclusion test", operator: "NOT IN", value: nil, want: sglPredicate{text: `"t"."id" ` + sglEmptyExclusion, values: nil, list: false}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			predicate, err := sglNewPredicate("id", testCase.operator, testCase.value, allowedColumns())

			require.NoError(t, err)
			require.Equal(t, testCase.want, predicate)
		})
	}
}

func TestNewPredicateRefusesWhatTheAllowListsDoNotHold(t *testing.T) {
	t.Parallel()

	_, err := sglNewPredicate("name", "=", 1, allowedColumns())
	require.ErrorIs(t, err, ErrSglUnknownColumn)

	_, err = sglNewPredicate("id", "= 1 OR 1", 1, allowedColumns())
	require.ErrorIs(t, err, ErrSglUnknownOperator)

	_, err = sglNewPredicate("id", "IN", make([]int, sglMaxBindVariables+1), allowedColumns())
	require.ErrorIs(t, err, ErrSglTooManyBindVariables)
	require.ErrorContains(t, err, fmt.Sprintf(`sgl: column "id" IN/NOT IN list of %d exceeds the limit of %d`, sglMaxBindVariables+1, sglMaxBindVariables))
}

func TestFilterNumbersItsPlaceholdersFromTheNextNumber(t *testing.T) {
	t.Parallel()

	predicates := []sglPredicate{
		{text: "a = ", values: []any{1}, list: false},
		{text: "b IS NULL", values: nil, list: false},
		{text: "c IN ", values: []any{2, 3}, list: true},
	}

	filter, values := sglFilter(predicates, 4)

	require.Equal(t, "a = #4 AND b IS NULL AND c IN (#5, #6)", filter)
	require.Equal(t, []any{1, 2, 3}, values)

	empty, none := sglFilter(nil, 1)
	require.Empty(t, empty)
	require.Nil(t, none)
}

func TestWhereEditsWrapTheConditionAQueryWrites(t *testing.T) {
	t.Parallel()

	written := "SELECT id FROM t WHERE a = 1 OR b = 2 LIMIT 3"
	require.Equal(t, "SELECT id FROM t WHERE (a = 1 OR b = 2) AND c = #1 LIMIT 3",
		sglSplice(written, sglWhereOpen("c = #1", 23, true), sglWhereClose("c = #1", 37, true)))

	absent := "SELECT id FROM t LIMIT 3"
	require.Equal(t, "SELECT id FROM t WHERE c = #1 LIMIT 3",
		sglSplice(absent, sglWhereOpen("c = #1", 16, false), sglWhereClose("c = #1", 16, false)))

	require.Equal(t, written, sglSplice(written, sglWhereOpen("", 23, true), sglWhereClose("", 37, true)))
}

func TestOrderAddsKeysAfterAWrittenOrderOrWritesOne(t *testing.T) {
	t.Parallel()

	written := "SELECT id FROM t ORDER BY id LIMIT 3"
	require.Equal(t, "SELECT id FROM t ORDER BY id, a DESC LIMIT 3", sglSplice(written, sglOrder([]string{"a DESC"}, 17, 28, true)))

	absent := "SELECT id FROM t LIMIT 3"
	require.Equal(t, "SELECT id FROM t ORDER BY a DESC, b ASC LIMIT 3", sglSplice(absent, sglOrder([]string{"a DESC", "b ASC"}, 16, 16, false)))
	require.Equal(t, absent, sglSplice(absent, sglOrder(nil, 16, 16, false)))
}

func TestNewOrderChecksTheColumnAndTheDirection(t *testing.T) {
	t.Parallel()

	order, err := sglNewOrder("id", " desc nulls last ", allowedColumns())
	require.NoError(t, err)
	require.Equal(t, `"t"."id" DESC NULLS LAST`, order)

	_, err = sglNewOrder("name", "ASC", allowedColumns())
	require.ErrorIs(t, err, ErrSglUnknownColumn)

	_, err = sglNewOrder("id", "ASC; DROP TABLE users", allowedColumns())
	require.ErrorIs(t, err, ErrSglUnknownDirection)
}

func TestPaginationWritesTheDialectsSyntax(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		want   string
		values []any
		limit  int
		offset int
	}{
		{name: "neither writes nothing", limit: 0, offset: 0, want: "", values: nil},
		{name: "a limit alone", limit: 5, offset: 0, want: " FETCH NEXT #3 ROWS ONLY", values: []any{5}},
		{name: "an offset alone", limit: 0, offset: 10, want: " OFFSET #3 ROWS", values: []any{10}},
		{name: "both in the dialect's order", limit: 5, offset: 10, want: " FETCH NEXT #3 ROWS ONLY OFFSET #4 ROWS", values: []any{5, 10}},
		{name: "a negative limit is none", limit: -1, offset: 0, want: "", values: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			text, values := sglPagination(testCase.limit, testCase.offset, 3)

			require.Equal(t, testCase.want, text)
			require.Equal(t, testCase.values, values)
		})
	}
}

func TestAtInsertsTextOrNothing(t *testing.T) {
	t.Parallel()

	require.Equal(t, "SELECT id FROM t LIMIT 1", sglSplice("SELECT id FROM t", sglAt(" LIMIT 1", 16)))
	require.Nil(t, sglAt("", 16))
}

func TestOperatorClassesIgnoreCaseAndPadding(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		operator  string
		wantUnary bool
		wantMulti bool
	}{
		{operator: "IS NULL", wantUnary: true},
		{operator: "is not null", wantUnary: true},
		{operator: "  Is Null  ", wantUnary: true},
		{operator: "IN", wantMulti: true},
		{operator: " not in ", wantMulti: true},
		{operator: "="},
		{operator: "LIKE"},
		{operator: "IS"},
		{operator: "NOT"},
		{operator: "ISNULL"},
		{operator: "NOTIN"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.operator, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.wantUnary, sglIsUnaryOperator(testCase.operator))
			require.Equal(t, testCase.wantMulti, sglIsMultiOperator(testCase.operator))
		})
	}
}

func TestAllowedOperatorsAdmitOnlyAnExactEntry(t *testing.T) {
	t.Parallel()

	require.True(t, sglAllowedOperators["="])
	require.False(t, sglAllowedOperators[" = "])
	require.False(t, sglAllowedOperators["= 1 OR 1"])
}

func TestReflectSliceFlattensAMultiValueIntoArguments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value any
		name  string
		want  []any
	}{
		{name: "nil is an empty set", value: nil, want: nil},
		{name: "an any slice is kept", value: []any{1, "a"}, want: []any{1, "a"}},
		{name: "a typed slice is boxed element by element", value: []string{"a", "b"}, want: []any{"a", "b"}},
		{name: "an array is boxed element by element", value: [3]int{1, 2, 3}, want: []any{1, 2, 3}},
		{name: "an empty typed slice is an empty set", value: []int{}, want: []any{}},
		{name: "a byte slice is one blob value", value: []byte("blob"), want: []any{[]byte("blob")}},
		{name: "a scalar is a one-element set", value: 42, want: []any{42}},
		{name: "a string is a one-element set", value: "single", want: []any{"single"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglReflectSlice(testCase.value))
		})
	}
}

func TestReflectSliceReturnsAnAnySliceWithoutCopyingIt(t *testing.T) {
	t.Parallel()

	values := []any{1, 2}

	flattened := sglReflectSlice(values)

	require.Same(t, &values[0], &flattened[0])
}

func TestMultiValueLenCountsTheBindValuesReflectSliceWouldProduce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value any
		name  string
		want  int
	}{
		{name: "nil", value: nil, want: 0},
		{name: "an any slice", value: []any{1, "a", nil}, want: 3},
		{name: "a typed slice", value: []string{"a", "b"}, want: 2},
		{name: "an array", value: [4]int{}, want: 4},
		{name: "an empty typed slice", value: []int{}, want: 0},
		{name: "a nil typed slice", value: []int(nil), want: 0},
		{name: "a byte slice", value: []byte("blob"), want: 1},
		{name: "a scalar", value: 42, want: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglMultiValueLen(testCase.value))
			require.Len(t, sglReflectSlice(testCase.value), testCase.want)
		})
	}
}

func TestNormaliseDirectionFoldsTheDirectionTheAllowListChecks(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		direction      string
		wantNormalised string
		wantAllowed    bool
	}{
		{direction: "asc", wantNormalised: "ASC", wantAllowed: true},
		{direction: "Asc", wantNormalised: "ASC", wantAllowed: true},
		{direction: "DESC", wantNormalised: "DESC", wantAllowed: true},
		{direction: " desc nulls last ", wantNormalised: "DESC NULLS LAST", wantAllowed: true},
		{direction: "desc nulls first", wantNormalised: "DESC NULLS FIRST", wantAllowed: true},
		{direction: "asc nulls first", wantNormalised: "ASC NULLS FIRST", wantAllowed: true},
		{direction: "\tASC NULLS LAST\n", wantNormalised: "ASC NULLS LAST", wantAllowed: true},
		{direction: "ASC; DROP TABLE users", wantNormalised: "ASC; DROP TABLE USERS", wantAllowed: false},
		{direction: "ASC, name", wantNormalised: "ASC, NAME", wantAllowed: false},
		{direction: "nulls last", wantNormalised: "NULLS LAST", wantAllowed: false},
		{direction: "desc  nulls last", wantNormalised: "DESC  NULLS LAST", wantAllowed: false},
		{direction: "ascending", wantNormalised: "ASCENDING", wantAllowed: false},
		{direction: "", wantNormalised: "", wantAllowed: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.direction, func(t *testing.T) {
			t.Parallel()

			normalised := sglNormaliseDirection(testCase.direction)
			require.Equal(t, testCase.wantNormalised, normalised)
			require.Equal(t, testCase.wantAllowed, sglAllowedDirections[normalised])
		})
	}
}

func TestRuntimeBuilderErrorValuesAreDistinctAndMatchThroughWrapping(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		err     error
		name    string
		message string
	}{
		{name: "unknown column", err: ErrSglUnknownColumn, message: "sgl: unknown column in runtime query filter"},
		{name: "unknown operator", err: ErrSglUnknownOperator, message: "sgl: unknown operator in runtime query filter"},
		{name: "unknown direction", err: ErrSglUnknownDirection, message: "sgl: unknown sort direction in runtime query order"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("listing posts: %w", testCase.err)

			require.EqualError(t, testCase.err, testCase.message)
			require.ErrorIs(t, wrapped, testCase.err)
			require.NotErrorIs(t, wrapped, ErrSglTooManyBindVariables)
			for _, other := range testCases {
				if other.name != testCase.name {
					require.NotErrorIs(t, wrapped, other.err)
				}
			}
		})
	}
}
