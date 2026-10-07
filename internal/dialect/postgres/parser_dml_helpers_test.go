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

package postgres

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestPredicateParametersInFunctionCallsAreFunctionArguments(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT id FROM items WHERE scale_price(id, $1) > 3 AND billing.discount(rate => $2, cap := $3) "+
		"AND length(f(id, (x + $4))) > 0 AND id = ANY($5) ORDER BY greatest(id, $6)")

	type argument struct {
		function string
		context  sqlmodel.ParameterContext
		ordinal  int
	}
	arguments := make(map[int]argument, len(analysis.ParameterReferences))
	for _, reference := range analysis.ParameterReferences {
		arguments[reference.Number] = argument{function: reference.EnclosingFunctionName, context: reference.Context, ordinal: reference.ArgumentOrdinal}
	}

	require.Equal(t, argument{function: "scale_price", context: sqlmodel.ParameterContextFunctionArgument, ordinal: 1}, arguments[1])
	require.Equal(t, argument{function: "billing.discount", context: sqlmodel.ParameterContextFunctionArgument, ordinal: 0}, arguments[2])
	require.Equal(t, argument{function: "billing.discount", context: sqlmodel.ParameterContextFunctionArgument, ordinal: 1}, arguments[3])
	require.Equal(t, "", arguments[4].function)
	require.Equal(t, "", arguments[5].function)
	require.Equal(t, argument{function: "greatest", context: sqlmodel.ParameterContextFunctionArgument, ordinal: 1}, arguments[6])
	require.Equal(t, map[int]string{2: "rate", 3: "cap"}, analysis.ArgumentNames)
}

func TestParametersComparedWithBuiltinCallsTakeTheCallType(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT name FROM items WHERE length(name) > $1 AND lower(email) = $2 AND my_function(code) = $3 "+
		"GROUP BY name HAVING count(*) > $4")

	references := make(map[int]*ast.RawParameterReference, len(analysis.ParameterReferences))
	for index := range analysis.ParameterReferences {
		references[analysis.ParameterReferences[index].Number] = &analysis.ParameterReferences[index]
	}

	require.NotNil(t, references[1].CastType)
	require.Equal(t, sqlmodel.TypeCategoryInteger, references[1].CastType.Category)
	require.Equal(t, "name", references[1].ColumnReference.ColumnName)
	require.NotNil(t, references[2].CastType)
	require.Equal(t, sqlmodel.TypeCategoryText, references[2].CastType.Category)
	require.Equal(t, "email", references[2].ColumnReference.ColumnName)
	require.Nil(t, references[3].CastType)
	require.Equal(t, "code", references[3].ColumnReference.ColumnName)
	require.NotNil(t, references[4].CastType)
	require.Equal(t, sqlmodel.TypeCategoryInteger, references[4].CastType.Category)
	require.Equal(t, sqlmodel.ParameterContextComparison, references[4].Context)
}

func TestACastToAQuotedTypeKeepsItsLettersAndAnUnquotedOneFolds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantTypeName   string
		wantSchema     string
		wantEngineName string
	}{
		{name: "a quoted cast", sql: `SELECT $1::"Mood" AS mood`, wantTypeName: `"Mood"`, wantEngineName: "Mood"},
		{name: "an unquoted cast of the same letters", sql: "SELECT $1::MOOD AS mood", wantTypeName: "mood", wantEngineName: "mood"},
		{name: "a quoted CAST", sql: `SELECT CAST($1 AS "Mood") AS mood`, wantTypeName: `"Mood"`, wantEngineName: "Mood"},
		{name: "a quoted qualified cast", sql: `SELECT $1::"Shapes"."Pair" AS pair`, wantTypeName: `"Shapes"."Pair"`,
			wantSchema: "Shapes", wantEngineName: "Pair"},
		{name: "a quoted qualified CAST", sql: `SELECT CAST($1 AS "Shapes"."Pair") AS pair`, wantTypeName: `"Shapes"."Pair"`,
			wantSchema: "Shapes", wantEngineName: "Pair"},
		{name: "a quoted array cast", sql: `SELECT $1::"Mood"[] AS moods`, wantTypeName: `"Mood"[]`, wantEngineName: "Mood[]"},
		{name: "a quoted array CAST", sql: `SELECT CAST($1 AS "Mood"[]) AS moods`, wantTypeName: `"Mood"[]`, wantEngineName: "Mood[]"},
		{name: "a quote held in a quoted name", sql: `SELECT $1::"Say ""Hi""" AS greeting`, wantTypeName: `"Say ""Hi"""`,
			wantEngineName: `Say "Hi"`},
		{name: "a quoted qualified CAST in a predicate", sql: `SELECT id FROM readings WHERE pair = CAST($1 AS "Shapes"."Pair")`,
			wantSchema: "Shapes", wantEngineName: "Pair"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)
			require.Len(t, analysis.ParameterReferences, 1)
			castType := analysis.ParameterReferences[0].CastType
			require.NotNil(t, castType)
			assert.Equal(t, testCase.wantSchema, castType.Schema)
			assert.Equal(t, testCase.wantEngineName, castType.EngineName)
			if testCase.wantTypeName == "" {
				return
			}
			require.Len(t, analysis.OutputColumns, 1)
			cast, isCast := analysis.OutputColumns[0].Expression.(*ast.CastExpression)
			require.True(t, isCast, "expression %T", analysis.OutputColumns[0].Expression)
			assert.Equal(t, testCase.wantTypeName, cast.TypeName)
		})
	}
}

func TestACastKeepsItsTypesModifiersAndTimeZone(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantTypeName   string
		wantEngineName string
		wantPrecision  *int
		wantScale      *int
		wantLength     *int
	}{
		{name: "a numeric cast", sql: "SELECT $1::numeric(10, 2) AS price", wantTypeName: "numeric(10,2)",
			wantEngineName: "numeric", wantPrecision: new(10), wantScale: new(2)},
		{name: "a numeric CAST", sql: "SELECT CAST($1 AS numeric(10, 2)) AS price", wantTypeName: "numeric(10,2)",
			wantEngineName: "numeric", wantPrecision: new(10), wantScale: new(2)},
		{name: "a negative scale", sql: "SELECT $1::numeric(5, -2) AS hundreds", wantTypeName: "numeric(5,-2)",
			wantEngineName: "numeric", wantPrecision: new(5), wantScale: new(-2)},
		{name: "a varchar CAST", sql: "SELECT CAST($1 AS character varying(40)) AS sku", wantTypeName: "character varying(40)",
			wantEngineName: "varchar", wantLength: new(40)},
		{name: "a timestamp with time zone", sql: "SELECT $1::timestamp(3) with time zone AS seen",
			wantTypeName: "timestamp(3) with time zone", wantEngineName: "timestamptz", wantPrecision: new(3)},
		{name: "a timestamp without time zone", sql: "SELECT $1::timestamp(6) WITHOUT TIME ZONE AS seen",
			wantTypeName: "timestamp(6) without time zone", wantEngineName: "timestamp", wantPrecision: new(6)},
		{name: "a time with time zone", sql: "SELECT $1::time(3) with time zone AS seen",
			wantTypeName: "time(3) with time zone", wantEngineName: "timetz", wantPrecision: new(3)},
		{name: "a time without time zone", sql: "SELECT $1::time(0) without time zone AS seen",
			wantTypeName: "time(0) without time zone", wantEngineName: "time", wantPrecision: new(0)},
		{name: "a timestamp with time zone CAST", sql: "SELECT CAST($1 AS timestamp(3) with time zone) AS seen",
			wantTypeName: "timestamp(3) with time zone", wantEngineName: "timestamptz", wantPrecision: new(3)},
		{name: "a numeric CAST in a predicate", sql: "SELECT id FROM orders WHERE price = CAST($1 AS numeric(12, 4))",
			wantEngineName: "numeric", wantPrecision: new(12), wantScale: new(4)},
		{name: "a time zone CAST in a predicate", sql: "SELECT id FROM orders WHERE seen = CAST($1 AS time(3) with time zone)",
			wantEngineName: "timetz", wantPrecision: new(3)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)
			require.Len(t, analysis.ParameterReferences, 1)
			castType := analysis.ParameterReferences[0].CastType
			require.NotNil(t, castType)
			assert.Equal(t, testCase.wantEngineName, castType.EngineName)
			assert.Equal(t, testCase.wantPrecision, castType.Precision)
			assert.Equal(t, testCase.wantScale, castType.Scale)
			assert.Equal(t, testCase.wantLength, castType.Length)
			if testCase.wantTypeName == "" {
				return
			}
			require.Len(t, analysis.OutputColumns, 1)
			cast, isCast := analysis.OutputColumns[0].Expression.(*ast.CastExpression)
			require.True(t, isCast, "expression %T", analysis.OutputColumns[0].Expression)
			assert.Equal(t, testCase.wantTypeName, cast.TypeName)
		})
	}
}

func describeDeclaredParameters(references []ast.RawParameterReference) []string {
	described := make([]string, 0, len(references))
	for _, reference := range references {
		column, declared := "-", "-"
		if reference.ColumnReference != nil {
			column = reference.ColumnReference.TableAlias + "." + reference.ColumnReference.ColumnName
		}
		if reference.CastType != nil {
			declared = reference.CastType.EngineName
		}
		described = append(described, fmt.Sprintf("%d %s %s %s", reference.Number, reference.Context, column, declared))
	}
	return described
}

func TestAParameterCastAloneDeclaresTheCastTypeWhereItIsAssigned(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a value of an INSERT cast to a type",
			sql:      "INSERT INTO hosts (id, addr) VALUES ($1, CAST($2 AS inet))",
			expected: []string{"1 assignment hosts.id -", "2 assignment hosts.addr inet"},
		},
		{
			name:     "a parenthesised value of an INSERT cast to a type",
			sql:      "INSERT INTO hosts (addr) VALUES ((CAST($1 AS inet)))",
			expected: []string{"1 assignment hosts.addr inet"},
		},
		{
			name:     "a value an UPDATE sets cast to a type",
			sql:      "UPDATE hosts SET addr = CAST($1 AS inet) WHERE id = $2",
			expected: []string{"1 assignment hosts.addr inet", "2 comparison .id -"},
		},
		{
			name:     "a parameter inside an expression a CAST converts",
			sql:      "UPDATE hosts SET addr = CAST($1 || '/32' AS inet)",
			expected: []string{"1 assignment hosts.addr -"},
		},
		{
			name:     "a parameter cast inline inside a CAST",
			sql:      "INSERT INTO hosts (addr) VALUES (CAST($1::text AS inet))",
			expected: []string{"1 assignment hosts.addr text"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			require.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}

func TestATypeNameAfterACastIsNoColumnAParameterIsComparedWith(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantColumn string
	}{
		{name: "a parameter after arithmetic on a cast", sql: "SELECT id FROM users WHERE (name)::int - $1 > 0", wantColumn: ""},
		{name: "a parameter compared with a cast", sql: "SELECT id FROM users WHERE (name)::numeric = $1", wantColumn: ""},
		{name: "a parameter after arithmetic on a column", sql: "SELECT id FROM users WHERE id - $1 > 0", wantColumn: "id"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			require.Len(t, analysis.ParameterReferences, 1)
			column := ""
			if reference := analysis.ParameterReferences[0].ColumnReference; reference != nil {
				column = reference.ColumnName
			}
			require.Equal(t, testCase.wantColumn, column)
		})
	}
}

func TestAParameterComparedWithAWrappedQualifiedColumnIsNamedAfterTheColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a qualified column a CAST converts",
			sql:      "SELECT id FROM reviews r WHERE cast(r.listing_id AS int) < $1",
			expected: []string{"1 comparison r.listing_id int4"},
		},
		{
			name:     "a qualified column a built-in call takes",
			sql:      "SELECT id FROM users u WHERE length(u.name) < $1",
			expected: []string{"1 comparison u.name int4"},
		},
		{
			name:     "a parenthesised qualified column",
			sql:      "SELECT id FROM users u WHERE (u.name) = $1",
			expected: []string{"1 comparison u.name -"},
		},
		{
			name:     "a schema-qualified column a CAST converts",
			sql:      "SELECT id FROM public.users WHERE CAST(public.users.id AS bigint) = $1",
			expected: []string{"1 comparison users.id int8"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			require.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}

func TestAParameterComparedWithACastTakesTheCastType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a column cast to text",
			sql:      "SELECT id FROM users WHERE cast(id AS text) = $1",
			expected: []string{"1 comparison .id text"},
		},
		{
			name:     "a column cast to a wider integer, upper case",
			sql:      "SELECT id FROM users WHERE CAST(id AS bigint) > $1",
			expected: []string{"1 comparison .id int8"},
		},
		{
			name:     "a column cast to a type of several words",
			sql:      "SELECT id FROM users WHERE CAST(id AS double precision) <= $1",
			expected: []string{"1 comparison .id float8"},
		},
		{
			name:     "an expression a CAST converts",
			sql:      "SELECT id FROM users WHERE CAST(name || email AS varchar(20)) = $1",
			expected: []string{"1 comparison .name varchar"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			require.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}

func TestAParameterBesideAnOperatorOfOneTypeIsComparedWithTheOtherOperand(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{name: "a distance between points", sql: "SELECT id FROM places ORDER BY location <-> $1", expected: []string{"1 comparison .location -"}},
		{name: "a qualified vector distance", sql: "SELECT id FROM items i ORDER BY i.embedding <=> $1 LIMIT 5", expected: []string{"1 comparison i.embedding -"}},
		{name: "a trigram word distance", sql: "SELECT id FROM docs WHERE body <<-> $1 < 0.5", expected: []string{"1 comparison .body -"}},
		{name: "a bitwise and", sql: "SELECT id FROM flags WHERE (mask & $1) <> 0", expected: []string{"1 comparison .mask -"}},
		{name: "a bitwise or", sql: "SELECT id FROM flags WHERE mask | $1 = mask", expected: []string{"1 comparison .mask -"}},
		{name: "a shift takes no partner type", sql: "SELECT id FROM flags WHERE mask << $1 > 8", expected: []string{"1 unknown - -"}},
		{name: "a JSON key takes no partner type", sql: "SELECT id FROM docs WHERE doc -> $1 IS NOT NULL", expected: []string{"1 unknown - -"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			require.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}
