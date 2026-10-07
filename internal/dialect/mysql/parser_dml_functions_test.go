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

package mysql

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func describeArgumentSlots(references []ast.RawParameterReference) []string {
	described := make([]string, 0, len(references))
	for _, reference := range references {
		castType := "-"
		if reference.CastType != nil {
			castType = reference.CastType.EngineName
		}
		described = append(described, fmt.Sprintf("%d %s %s#%d %s", reference.Number, reference.Context,
			reference.EnclosingFunctionName, reference.ArgumentOrdinal, castType))
	}
	return described
}

func TestFunctionArgumentsTagEachParameterWithTheSlotItIsWrittenIn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "numbered placeholders written out of order",
			sql:      "SELECT LEFT(?2, ?1) AS prefix",
			expected: []string{"2 function-argument left#0 -", "1 function-argument left#1 -"},
		},
		{
			name: "three numbered placeholders in reverse order",
			sql:  "SELECT LPAD(?3, ?1, ?2) AS padded",
			expected: []string{"3 function-argument lpad#0 -", "1 function-argument lpad#1 -",
				"2 function-argument lpad#2 -"},
		},
		{
			name:     "a special argument grammar with placeholders out of order",
			sql:      "SELECT SUBSTRING(?2, ?1) AS part",
			expected: []string{"2 function-argument substring#0 -", "1 function-argument substring#1 -"},
		},
		{
			name: "a named parameter used again in a later call",
			sql:  "SELECT LEFT(:word, 1) AS a, LPAD('x', :size, :word) AS b",
			expected: []string{"1 function-argument left#0 -", "2 function-argument lpad#1 -",
				"1 function-argument lpad#2 -"},
		},
		{
			name:     "a table function's arguments",
			sql:      "SELECT j.v FROM JSON_TABLE(?2, ?1 COLUMNS (v INT PATH '$')) AS j",
			expected: []string{"2 function-argument json_table#0 -", "1 function-argument json_table#1 -"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			assert.Equal(t, testCase.expected, describeArgumentSlots(analysis.ParameterReferences))
		})
	}
}

func TestACastTypesTheParameterItConvertsWhateverItsNumber(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a named parameter cast twice",
			sql:      "SELECT CAST(:n AS SIGNED) AS a, CAST(:n AS SIGNED) AS b",
			expected: []string{"1 cast #0 bigint", "1 cast #0 bigint"},
		},
		{
			name:     "a numbered placeholder cast after a higher one",
			sql:      "SELECT CAST(?2 AS CHAR) AS a, CONVERT(?1, DATE) AS b",
			expected: []string{"2 cast #0 char", "1 cast #0 date"},
		},
		{
			name:     "a cast of an expression holding two parameters types neither",
			sql:      "SELECT CAST(? + ? AS SIGNED) AS a",
			expected: []string{"1 unknown #0 -", "2 unknown #0 -"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			assert.Equal(t, testCase.expected, describeArgumentSlots(analysis.ParameterReferences))
		})
	}
}

func TestACastTargetKeepsTheModifiersOfItsType(t *testing.T) {
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
		{name: "a CAST to a precision and a scale", sql: "SELECT CAST(price AS DECIMAL(10, 2)) AS a FROM t",
			wantTypeName: "decimal(10,2)", wantEngineName: "decimal", wantPrecision: new(10), wantScale: new(2)},
		{name: "a CONVERT to a length", sql: "SELECT CONVERT(name, CHAR(20)) AS a FROM t",
			wantTypeName: "char(20)", wantEngineName: "char", wantLength: new(20)},
		{name: "a CAST to a fractional-second precision", sql: "SELECT CAST(born AS DATETIME(6)) AS a FROM t",
			wantTypeName: "datetime(6)", wantEngineName: "datetime", wantPrecision: new(6)},
		{name: "a CAST to a length with a character set",
			sql:          "SELECT CAST(name AS CHAR(10) CHARACTER SET utf8mb4) AS a FROM t",
			wantTypeName: "char(10)", wantEngineName: "char", wantLength: new(10)},
		{name: "a JSON_VALUE result returned as a precision and a scale",
			sql:          "SELECT JSON_VALUE(doc, '$.price' RETURNING DECIMAL(8, 3)) AS a FROM t",
			wantTypeName: "decimal(8,3)", wantEngineName: "decimal", wantPrecision: new(8), wantScale: new(3)},
		{name: "a CAST to a type written without modifiers", sql: "SELECT CAST(price AS DECIMAL) AS a FROM t",
			wantTypeName: "decimal", wantEngineName: "decimal"},
		{name: "a CAST to a target that names no column type", sql: "SELECT CAST(price AS UNSIGNED INTEGER) AS a FROM t",
			wantTypeName: "bigint unsigned", wantEngineName: "bigint unsigned"},
		{name: "a list of more modifiers than a type takes is left out",
			sql:          "SELECT CAST(price AS DECIMAL(10, 2, 3)) AS a FROM t",
			wantTypeName: "decimal", wantEngineName: "decimal"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)
			require.Len(t, analysis.OutputColumns, 1)
			cast, isCast := analysis.OutputColumns[0].Expression.(*ast.CastExpression)
			require.True(t, isCast, "got %T", analysis.OutputColumns[0].Expression)

			resolved := New().NormaliseTypeName(cast.TypeName)

			assert.Equal(t, testCase.wantTypeName, cast.TypeName)
			assert.Equal(t, testCase.wantEngineName, resolved.EngineName)
			assert.Equal(t, testCase.wantPrecision, resolved.Precision)
			assert.Equal(t, testCase.wantScale, resolved.Scale)
			assert.Equal(t, testCase.wantLength, resolved.Length)
		})
	}
}

func TestACastTypesTheParameterItConvertsWithTheModifiersOfItsType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantEngineName string
		wantPrecision  *int
		wantScale      *int
		wantLength     *int
	}{
		{name: "a parameter cast to a precision and a scale", sql: "SELECT CAST(? AS DECIMAL(10, 2)) AS a",
			wantEngineName: "decimal", wantPrecision: new(10), wantScale: new(2)},
		{name: "a parameter converted to a length", sql: "SELECT CONVERT(?, CHAR(20)) AS a",
			wantEngineName: "char", wantLength: new(20)},
		{name: "a parameter in an expression cast to a fractional-second precision",
			sql:            "SELECT CAST(COALESCE(?, NOW()) AS DATETIME(3)) AS a",
			wantEngineName: "datetime", wantPrecision: new(3)},
		{name: "a parameter cast to a type written without modifiers", sql: "SELECT CAST(? AS DECIMAL) AS a",
			wantEngineName: "decimal"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)
			require.Len(t, analysis.ParameterReferences, 1)
			castType := analysis.ParameterReferences[0].CastType
			require.NotNil(t, castType)

			assert.Equal(t, sqlmodel.ParameterContextCast, analysis.ParameterReferences[0].Context)
			assert.Equal(t, testCase.wantEngineName, castType.EngineName)
			assert.Equal(t, testCase.wantPrecision, castType.Precision)
			assert.Equal(t, testCase.wantScale, castType.Scale)
			assert.Equal(t, testCase.wantLength, castType.Length)
		})
	}
}

func TestAConversionToAFloatOfMoreThanFiftyThreeBitsIsRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		before string
		token  string
		after  string
	}{
		{name: "a CAST", before: "SELECT CAST(price AS FLOAT(", token: "54", after: ")) AS a FROM t"},
		{name: "a CONVERT to FLOAT4", before: "SELECT CONVERT(price, FLOAT4(", token: "60", after: ")) AS a FROM t"},
		{name: "a JSON_VALUE result", before: "SELECT JSON_VALUE(doc, '$.a' RETURNING FLOAT(", token: "54",
			after: ")) AS a FROM t"},
		{name: "a placeholder cast", before: "SELECT CAST(? AS FLOAT(", token: "99", after: ")) AS a"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.before+testCase.token+testCase.after)

			require.Nil(t, analysis)
			parseError, ok := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, ok, "got %v", err)
			assert.Equal(t, testCase.token, parseError.Token)
			assert.Equal(t, len(testCase.before), parseError.Position)
			assert.Equal(t, "FLOAT takes a precision of at most 53 bits", parseError.Message)
		})
	}
}

func TestAConversionToAFloatTakesTheTypeMySQLMakesOfIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		target         string
		wantEngineName string
	}{
		{name: "a float of twenty-four bits", target: "FLOAT(24)", wantEngineName: "float"},
		{name: "a float of fifty-three bits", target: "FLOAT(53)", wantEngineName: "double"},
		{name: "FLOAT4", target: "FLOAT4", wantEngineName: "float"},
		{name: "FLOAT4 of twenty-five bits", target: "FLOAT4(25)", wantEngineName: "double"},
		{name: "FLOAT8", target: "FLOAT8", wantEngineName: "double"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), "SELECT CAST(price AS "+testCase.target+") AS a FROM t")
			require.NoError(t, err)
			require.Len(t, analysis.OutputColumns, 1)
			cast, isCast := analysis.OutputColumns[0].Expression.(*ast.CastExpression)
			require.True(t, isCast, "got %T", analysis.OutputColumns[0].Expression)

			assert.Equal(t, testCase.wantEngineName, New().NormaliseTypeName(cast.TypeName).EngineName)
		})
	}
}

func TestTaggingFunctionArgumentsGrowsLinearlyWithTheParameters(t *testing.T) {
	t.Parallel()

	const smaller = 5_000
	engine := New(WithMaxTokensPerStatement(dialectkit.MaxTokensPerStatementCeiling))

	testCases := []struct {
		name string
		sql  func(count int) string
	}{
		{
			name: "one call holding every placeholder",
			sql: func(count int) string {
				return "SELECT GREATEST(" + strings.TrimSuffix(strings.Repeat("?, ", count), ", ") + ") AS g"
			},
		},
		{
			name: "one call per placeholder",
			sql: func(count int) string {
				return "SELECT " + strings.TrimSuffix(strings.Repeat("LEFT(?, 1), ", count), ", ")
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analyse := func(count int) func() {
				sql := testCase.sql(count)
				return func() {
					analysis, err := analyseWith(t, engine, sql)
					require.NoError(t, err)
					require.Len(t, analysis.ParameterReferences, count)
				}
			}

			scaling.Linear(t, analyse(smaller), analyse(4*smaller))
		})
	}
}
