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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestRowConstructorsAreRowValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantElements int
	}{
		{name: "a parenthesised list compared", sql: "SELECT id FROM users WHERE (id, name) < (?, ?)", wantElements: 2},
		{name: "the ROW keyword", sql: "SELECT id FROM users WHERE ROW(id, name) = ROW(?, ?)", wantElements: 2},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.Len(t, analysis.ClauseExpressions, 1)
			comparison, isComparison := analysis.ClauseExpressions[0].Expression.(*ast.ComparisonExpression)
			require.True(t, isComparison)
			left, leftIsRow := comparison.Left.(*ast.RowValueExpression)
			right, rightIsRow := comparison.Right.(*ast.RowValueExpression)
			require.True(t, leftIsRow && rightIsRow)
			require.Len(t, left.Elements, testCase.wantElements)
			require.Len(t, right.Elements, testCase.wantElements)
		})
	}
}

func TestARowTestedAgainstAListOfRowsIsRowValues(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, "SELECT code FROM tuples WHERE (country, type) IN ((?, ?), (?, ?))")

	require.Len(t, analysis.ClauseExpressions, 1)
	list, isList := analysis.ClauseExpressions[0].Expression.(*ast.InListExpression)
	require.True(t, isList)
	_, innerIsRow := list.Inner.(*ast.RowValueExpression)
	require.True(t, innerIsRow)
	require.Len(t, list.Values, 2)
	for _, value := range list.Values {
		_, isRow := value.(*ast.RowValueExpression)
		require.True(t, isRow)
	}
}

func TestStringsWrittenTogetherAreOneString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		columns []string
	}{
		{name: "two quoted strings", sql: `SELECT 'a' "b" AS joined FROM items`, columns: []string{"joined"}},
		{name: "three strings", sql: `SELECT 'a' 'b' 'c' AS joined FROM items`, columns: []string{"joined"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.Equal(t, testCase.columns, outputColumnNames(analysis))
			literal, isLiteral := analysis.OutputColumns[0].Expression.(*ast.LiteralExpression)
			require.True(t, isLiteral)
			require.Equal(t, "text", literal.TypeName)
		})
	}
}

func TestPipesWithATextOperandAreWarnedOf(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		warnedAt []string
	}{
		{name: "a pattern built with pipes", sql: "SELECT id FROM users WHERE name LIKE '%' || ? || '%'", warnedAt: []string{"||"}},
		{name: "names joined with a space", sql: "SELECT first_name || ' ' || last_name AS full_name FROM users", warnedAt: []string{"||"}},
		{name: "a text conversion", sql: "SELECT CAST(id AS CHAR) || name AS label FROM users", warnedAt: []string{"||"}},
		{name: "a text conversion with a length", sql: "SELECT CAST(id AS CHAR(10)) || name AS label FROM users",
			warnedAt: []string{"||"}},
		{name: "a text function", sql: "SELECT CONCAT(name, 'x') || id AS label FROM users", warnedAt: []string{"||"}},
		{name: "each chain once", sql: "SELECT 'a' || 'b' AS one, 'c' || 'd' AS two FROM users", warnedAt: []string{"||", "||"}},
		{name: "a text operand opening a comparison", sql: "SELECT id FROM users WHERE name || 'x' = ?", warnedAt: []string{"||"}},
		{name: "a text operand ending a comparison", sql: "SELECT id FROM users WHERE name = 'x' || ?", warnedAt: []string{"||"}},
		{name: "a text operand opening a pattern", sql: "SELECT id FROM users WHERE name || '%' LIKE ?", warnedAt: []string{"||"}},
		{name: "a text operand opening an IN list", sql: "SELECT id FROM users WHERE name || 'x' IN (?, ?)", warnedAt: []string{"||"}},
		{name: "a text operand under AND", sql: "SELECT id FROM users WHERE active || 'x' = name AND admin", warnedAt: []string{"||"}},
		{name: "pipes between flags", sql: "SELECT id FROM users WHERE active || admin", warnedAt: nil},
		{name: "pipes between comparisons of columns", sql: "SELECT id FROM users WHERE id = ? || name = last_name", warnedAt: nil},
		{name: "OR with a text operand", sql: "SELECT id FROM users WHERE name = 'a' OR 'b'", warnedAt: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			var warnedAt []string
			for _, warning := range analysis.Warnings {
				warnedAt = append(warnedAt, testCase.sql[warning.Span.Start:warning.Span.End])
				require.Contains(t, warning.Message, "CONCAT")
			}
			require.Equal(t, testCase.warnedAt, warnedAt)
		})
	}
}

func TestAPatternBuiltWithPipesIsNoPatternOfTheColumn(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, "SELECT id FROM users WHERE name LIKE '%' || ? || '%'")

	require.Equal(t, []string{"unknown::"}, parameterSummary(analysis))
}

func TestADivisionByNoConstantOtherThanZeroMayBeNull(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		mayBeNull bool
	}{
		{name: "divided by a column", sql: "SELECT a / b AS x FROM t", mayBeNull: true},
		{name: "remainder of a column", sql: "SELECT a % b AS x FROM t", mayBeNull: true},
		{name: "integer division by a column", sql: "SELECT a DIV b AS x FROM t", mayBeNull: true},
		{name: "modulo keyword by a column", sql: "SELECT a MOD b AS x FROM t", mayBeNull: true},
		{name: "divided by a parameter", sql: "SELECT a / ? AS x FROM t", mayBeNull: true},
		{name: "divided by zero", sql: "SELECT a / 0 AS x FROM t", mayBeNull: true},
		{name: "divided by a decimal zero", sql: "SELECT a / 0.0 AS x FROM t", mayBeNull: true},
		{name: "divided by a hexadecimal zero", sql: "SELECT a / 0x00 AS x FROM t", mayBeNull: true},
		{name: "divided by two", sql: "SELECT a / 2 AS x FROM t", mayBeNull: false},
		{name: "divided by minus a half", sql: "SELECT a / -0.5 AS x FROM t", mayBeNull: false},
		{name: "remainder of three", sql: "SELECT a % 3 AS x FROM t", mayBeNull: false},
		{name: "a product", sql: "SELECT a * b AS x FROM t", mayBeNull: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			operation, isOperation := analysis.OutputColumns[0].Expression.(*ast.BinaryOpExpression)
			require.True(t, isOperation)
			require.Equal(t, testCase.mayBeNull, operation.MayBeNull)
		})
	}
}

func TestPredicatesTagEveryParameterTheyHoldWhateverItsNumber(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a named parameter used again in a second IN list",
			sql:      "SELECT a IN (:x) AS p, b IN (:x) AS q FROM t",
			expected: []string{"1 in-list .a -", "1 in-list .b -"},
		},
		{
			name:     "numbered placeholders written out of order in BETWEEN",
			sql:      "SELECT a BETWEEN ?2 AND ?1 AS p FROM t",
			expected: []string{"2 between .a -", "1 between .a -"},
		},
		{
			name:     "a named parameter used again in a second BETWEEN",
			sql:      "SELECT a BETWEEN :low AND :high AS p, b BETWEEN :low AND 9 AS q FROM t",
			expected: []string{"1 between .a -", "2 between .a -", "1 between .b -"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			assert.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}

func TestTaggingPredicateParametersGrowsLinearlyWithTheParameters(t *testing.T) {
	t.Parallel()

	const smaller = 2_500
	engine := New(WithMaxTokensPerStatement(dialectkit.MaxTokensPerStatementCeiling))

	analyse := func(count int) func() {
		sql := "SELECT " + strings.TrimSuffix(strings.Repeat("a IN (?, ?) OR b BETWEEN ? AND ? OR ", count), " OR ") + " AS p FROM t"
		return func() {
			analysis, err := analyseWith(t, engine, sql)
			require.NoError(t, err)
			require.Len(t, analysis.ParameterReferences, 4*count)
		}
	}

	scaling.Linear(t, analyse(smaller), analyse(4*smaller))
}

func TestAppendTypeModifiersWritesANumericListAfterATypeName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		typeName string
		sql      string
		want     string
		wantNext string
		wantErr  bool
	}{
		{name: "a precision and a scale", typeName: "decimal", sql: "(10, 2) x", want: "decimal(10,2)", wantNext: "x"},
		{name: "a length", typeName: "char", sql: "(20) x", want: "char(20)", wantNext: "x"},
		{name: "no list", typeName: "date", sql: "x", want: "date", wantNext: "x"},
		{name: "a list with no type name before it is left out", typeName: "", sql: "(10) x", want: "", wantNext: "x"},
		{name: "the values of an ENUM are left out", typeName: "enum", sql: "('a', 'b') x", want: "enum", wantNext: "x"},
		{name: "more modifiers than a type takes are left out", typeName: "decimal", sql: "(10, 2, 3) x",
			want: "decimal", wantNext: "x"},
		{name: "a list left open", typeName: "decimal", sql: "(10, 2", want: "decimal", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)
			p := newParser(tokens)
			var builder strings.Builder
			builder.WriteString(testCase.typeName)

			err = p.appendTypeModifiers(&builder)

			assert.Equal(t, testCase.want, builder.String())
			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantNext, p.Current().Value)
		})
	}
}
