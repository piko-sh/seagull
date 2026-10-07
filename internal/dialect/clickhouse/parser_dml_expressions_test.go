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

package clickhouse

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func parseExpressionFromSource(t *testing.T, source string) ast.Expression {
	t.Helper()
	tokens, err := tokenise(source)
	require.NoError(t, err)
	parserInstance := newParser(tokens)
	expression := parserInstance.parseExpression()
	require.NotNil(t, expression)
	require.True(t, parserInstance.AtEnd(), "the expression %q must be parsed whole, stopped at %q", source, parserInstance.Current().Raw)
	require.NoError(t, parserInstance.syntaxError)
	return expression
}

func TestParseExpressionBuildsTheTree(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		want   ast.Expression
		name   string
		source string
	}{
		{name: "small integer is a UInt8", source: "42", want: &ast.LiteralExpression{TypeName: "UInt8"}},
		{name: "integer takes the smallest unsigned type", source: "300", want: &ast.LiteralExpression{TypeName: "UInt16"}},
		{name: "large integer", source: "5000000000", want: &ast.LiteralExpression{TypeName: "UInt64"}},
		{name: "integer beyond UInt64 is a Float64", source: "18446744073709551616", want: &ast.LiteralExpression{TypeName: "Float64"}},
		{name: "negative integer is signed", source: "-1", want: &ast.LiteralExpression{TypeName: "Int8"}},
		{name: "negative integer takes the smallest signed type", source: "-129", want: &ast.LiteralExpression{TypeName: "Int16"}},
		{name: "fraction is a Float64", source: "1.5", want: &ast.LiteralExpression{TypeName: "Float64"}},
		{name: "leading dot fraction", source: ".5", want: &ast.LiteralExpression{TypeName: "Float64"}},
		{name: "exponent is a Float64", source: "1e3", want: &ast.LiteralExpression{TypeName: "Float64"}},
		{name: "hexadecimal holding E is an integer", source: "0x1E", want: &ast.LiteralExpression{TypeName: "UInt8"}},
		{name: "string", source: "'cod'", want: &ast.LiteralExpression{TypeName: "String"}},
		{name: "boolean", source: "true", want: &ast.LiteralExpression{TypeName: "Bool"}},
		{name: "DATE before a string is a Date literal", source: "DATE '2024-01-01'", want: &ast.LiteralExpression{TypeName: "Date"}},
		{name: "TIMESTAMP before a string is a DateTime literal", source: "timestamp '2024-01-01 10:00:00'", want: &ast.LiteralExpression{TypeName: "DateTime"}},
		{name: "a column named date is a column", source: "date", want: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "date"}},
		{name: "NULL may be NULL", source: "NULL", want: &ast.CastExpression{Inner: nil, TypeName: ""}},
		{name: "bare column", source: "name", want: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "name"}},
		{name: "qualified column", source: "sightings.id", want: &ast.ColumnRefExpression{TableAlias: "sightings", ColumnName: "id"}},
		{
			name:   "placeholder is its parameter typed by its tag and never NULL",
			source: "{id:UInt64}",
			want: &ast.CastExpression{
				Inner: &ast.CoalesceExpression{Arguments: []ast.Expression{
					&ast.ParameterExpression{Number: 0}, &ast.LiteralExpression{TypeName: "UInt64"},
				}},
				TypeName: "UInt64",
			},
		},
		{
			name:   "nullable placeholder is its parameter, which may be NULL",
			source: "{note:Nullable(String)}",
			want:   &ast.CastExpression{Inner: &ast.ParameterExpression{Number: 0}, TypeName: "Nullable(String)"},
		},
		{
			name:   "multiplication by star",
			source: "price * 2",
			want: &ast.BinaryOpExpression{
				Left: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "price"}, Operator: "*", Right: &ast.LiteralExpression{TypeName: "UInt8"},
			},
		},
		{
			name:   "precedence of multiplication over addition",
			source: "a + b * c",
			want: &ast.BinaryOpExpression{
				Left:     &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"},
				Operator: "+",
				Right: &ast.BinaryOpExpression{
					Left: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "b"}, Operator: "*", Right: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "c"},
				},
			},
		},
		{
			name:   "DIV is integer division",
			source: "a DIV 2",
			want:   newFunctionCall("intDiv", &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, &ast.LiteralExpression{TypeName: "UInt8"}),
		},
		{
			name:   "negation of a column",
			source: "-a",
			want:   &ast.UnaryOpExpression{Operator: "-", Operand: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}},
		},
		{
			name:   "comparison",
			source: "a >= 1",
			want:   &ast.ComparisonExpression{Left: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Operator: ">=", Right: &ast.LiteralExpression{TypeName: "UInt8"}},
		},
		{
			name:   "logical chain",
			source: "a AND b AND c",
			want: &ast.LogicalOpExpression{Operator: "AND", Operands: []ast.Expression{
				&ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"},
				&ast.ColumnRefExpression{TableAlias: "", ColumnName: "b"},
				&ast.ColumnRefExpression{TableAlias: "", ColumnName: "c"},
			}},
		},
		{
			name:   "NOT",
			source: "NOT a",
			want:   &ast.UnaryOpExpression{Operator: "NOT", Operand: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}},
		},
		{
			name:   "IS NOT NULL",
			source: "a IS NOT NULL",
			want:   &ast.IsNullExpression{Inner: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Negated: true},
		},
		{
			name:   "BETWEEN",
			source: "a BETWEEN 1 AND 2",
			want: &ast.BetweenExpression{
				Inner: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Low: &ast.LiteralExpression{TypeName: "UInt8"}, High: &ast.LiteralExpression{TypeName: "UInt8"},
			},
		},
		{
			name:   "NOT IN list",
			source: "a NOT IN (1, 2)",
			want: &ast.InListExpression{
				Inner: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Values: []ast.Expression{&ast.LiteralExpression{TypeName: "UInt8"}, &ast.LiteralExpression{TypeName: "UInt8"}},
			},
		},
		{
			name:   "NOT ILIKE",
			source: "a NOT ILIKE 'x%'",
			want:   &ast.ComparisonExpression{Left: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Operator: "NOT ILIKE", Right: &ast.LiteralExpression{TypeName: "String"}},
		},
		{
			name:   "double colon cast",
			source: "a::Decimal(18, 4)",
			want:   &ast.CastExpression{Inner: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, TypeName: "Decimal(18, 4)"},
		},
		{
			name:   "CAST AS a nullable type may be NULL",
			source: "CAST(a AS Nullable(String))",
			want:   &ast.CastExpression{Inner: nil, TypeName: "Nullable(String)"},
		},
		{
			name:   "CAST AS a tuple keeps its element names",
			source: "CAST(a AS Tuple(UInt8, Tuple(s String, n UInt16)))",
			want: &ast.CastExpression{
				Inner:    &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"},
				TypeName: "Tuple(UInt8, Tuple(s String, n UInt16))",
			},
		},
		{
			name:   "double colon cast to a named tuple keeps its element names",
			source: "a::Tuple(s String, n Nullable(UInt16))",
			want: &ast.CastExpression{
				Inner:    &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"},
				TypeName: "Tuple(s String, n Nullable(UInt16))",
			},
		},
		{
			name:   "subscript reads the element",
			source: "species[1]",
			want:   newFunctionCall("arrayElement", &ast.ColumnRefExpression{TableAlias: "", ColumnName: "species"}, &ast.LiteralExpression{TypeName: "UInt8"}),
		},
		{
			name:   "count of distinct values",
			source: "count(DISTINCT id)",
			want:   newFunctionCall("count", &ast.ColumnRefExpression{TableAlias: "", ColumnName: "id"}),
		},
		{
			name:   "count of a qualified column",
			source: "count(sightings.id)",
			want:   newFunctionCall("count", &ast.ColumnRefExpression{TableAlias: "sightings", ColumnName: "id"}),
		},
		{name: "count star", source: "count(*)", want: newFunctionCall("count")},
		{
			name:   "parametric aggregate",
			source: "quantile(0.5)(latency)",
			want:   newFunctionCall("quantile", &ast.ColumnRefExpression{TableAlias: "", ColumnName: "latency"}),
		},
		{
			name:   "coalesce is a coalesce expression",
			source: "coalesce(nickname, 'x')",
			want: &ast.CoalesceExpression{Arguments: []ast.Expression{
				&ast.ColumnRefExpression{TableAlias: "", ColumnName: "nickname"}, &ast.LiteralExpression{TypeName: "String"},
			}},
		},
		{
			name:   "ifNull is a coalesce expression",
			source: "ifNull(nickname, 'x')",
			want: &ast.CoalesceExpression{Arguments: []ast.Expression{
				&ast.ColumnRefExpression{TableAlias: "", ColumnName: "nickname"}, &ast.LiteralExpression{TypeName: "String"},
			}},
		},
		{
			name:   "window function",
			source: "rank() OVER (PARTITION BY beach ORDER BY id)",
			want:   &ast.WindowFunctionExpression{Function: newFunctionCall("rank")},
		},
		{
			name:   "aggregate over a named window",
			source: "sum(id) OVER w",
			want:   &ast.WindowFunctionExpression{Function: newFunctionCall("sum", &ast.ColumnRefExpression{TableAlias: "", ColumnName: "id"})},
		},
		{
			name:   "searched CASE",
			source: "CASE WHEN a THEN 1 ELSE 0 END",
			want: &ast.CaseWhenExpression{
				Branches:   []ast.CaseWhenBranch{{Condition: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Result: &ast.LiteralExpression{TypeName: "UInt8"}}},
				ElseResult: &ast.LiteralExpression{TypeName: "UInt8"},
			},
		},
		{
			name:   "simple CASE compares the subject",
			source: "CASE a WHEN 1 THEN 'one' END",
			want: &ast.CaseWhenExpression{
				Branches: []ast.CaseWhenBranch{{
					Condition: &ast.ComparisonExpression{Left: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Operator: "=", Right: &ast.LiteralExpression{TypeName: "UInt8"}},
					Result:    &ast.LiteralExpression{TypeName: "String"},
				}},
				ElseResult: nil,
			},
		},
		{
			name:   "lambda",
			source: "x -> x + 1",
			want: &ast.LambdaExpression{Parameters: []string{"x"}, Body: &ast.BinaryOpExpression{
				Left: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "x"}, Operator: "+", Right: &ast.LiteralExpression{TypeName: "UInt8"},
			}},
		},
		{
			name:   "lambda with two parameters",
			source: "(x, y) -> x",
			want:   &ast.LambdaExpression{Parameters: []string{"x", "y"}, Body: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "x"}},
		},
		{
			name:   "array literal",
			source: "[1, 2]",
			want:   newFunctionCall("array", &ast.LiteralExpression{TypeName: "UInt8"}, &ast.LiteralExpression{TypeName: "UInt8"}),
		},
		{
			name:   "tuple literal",
			source: "(1, 'a')",
			want:   newFunctionCall("tuple", &ast.LiteralExpression{TypeName: "UInt8"}, &ast.LiteralExpression{TypeName: "String"}),
		},
		{
			name:   "named element access",
			source: "location.lat",
			want:   &ast.ColumnRefExpression{TableAlias: "location", ColumnName: "lat"},
		},
		{
			name:   "positional element access",
			source: "pair.1",
			want:   subcolumnAccess(&ast.ColumnRefExpression{TableAlias: "", ColumnName: "pair"}, "_1"),
		},
		{
			name:   "chained positional element access reads each element in turn",
			source: "pair.2.1",
			want:   subcolumnAccess(subcolumnAccess(&ast.ColumnRefExpression{TableAlias: "", ColumnName: "pair"}, "_2"), "_1"),
		},
		{
			name:   "three positional elements deep",
			source: "pair.3.2.1",
			want: subcolumnAccess(subcolumnAccess(subcolumnAccess(&ast.ColumnRefExpression{TableAlias: "", ColumnName: "pair"},
				"_3"), "_2"), "_1"),
		},
		{
			name:   "element of a qualified column",
			source: "orders.basket.fish",
			want:   subcolumnAccess(&ast.ColumnRefExpression{TableAlias: "orders", ColumnName: "basket"}, "fish"),
		},
		{name: "interval", source: "INTERVAL 1 DAY", want: &ast.LiteralExpression{TypeName: "IntervalDay"}},
		{name: "interval of a negative number", source: "INTERVAL -3 hours", want: &ast.LiteralExpression{TypeName: "IntervalHour"}},
		{
			name:   "aggregate with a FILTER clause",
			source: "count() FILTER (WHERE score > 1)",
			want: &ast.FunctionCallExpression{
				FunctionName: "count",
				Arguments:    nil,
				FilterExpression: &ast.ComparisonExpression{
					Left: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "score"}, Operator: ">", Right: &ast.LiteralExpression{TypeName: "UInt8"},
				},
				Schema: "",
			},
		},
		{
			name:   "window function respecting NULLs",
			source: "first_value(name) RESPECT NULLS OVER (ORDER BY id)",
			want:   &ast.WindowFunctionExpression{Function: newFunctionCall("first_value", &ast.ColumnRefExpression{TableAlias: "", ColumnName: "name"})},
		},
		{
			name:   "MOD is the remainder",
			source: "a MOD 3",
			want:   &ast.BinaryOpExpression{Left: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Operator: "%", Right: &ast.LiteralExpression{TypeName: "UInt8"}},
		},
		{
			name:   "CAST in its function form",
			source: "CAST(a, 'UInt32')",
			want:   &ast.CastExpression{Inner: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, TypeName: "UInt32"},
		},
		{
			name:   "GLOBAL NOT IN",
			source: "a GLOBAL NOT IN (1)",
			want:   &ast.InListExpression{Inner: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Values: []ast.Expression{&ast.LiteralExpression{TypeName: "UInt8"}}},
		},
		{name: "interval in a string", source: "INTERVAL '2 hours'", want: &ast.LiteralExpression{TypeName: "IntervalHour"}},
		{
			name:   "elements of tuple() named with AS",
			source: "tuple(1 AS x, 'a' AS y)",
			want:   newFunctionCall(functionTuple, &ast.LiteralExpression{TypeName: "UInt8"}, &ast.LiteralExpression{TypeName: "String"}),
		},
		{
			name:   "elements of a parenthesised tuple named with AS",
			source: "(id AS x, name AS `y`)",
			want: newFunctionCall(functionTuple,
				&ast.ColumnRefExpression{TableAlias: "", ColumnName: "id"}, &ast.ColumnRefExpression{TableAlias: "", ColumnName: "name"}),
		},
		{
			name:   "a parenthesised expression named with AS",
			source: "(a + 1 AS b) * 2",
			want: &ast.BinaryOpExpression{
				Left: &ast.BinaryOpExpression{
					Left: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"}, Operator: "+", Right: &ast.LiteralExpression{TypeName: "UInt8"},
				},
				Operator: "*",
				Right:    &ast.LiteralExpression{TypeName: "UInt8"},
			},
		},
		{
			name:   "an argument and an array element named with AS",
			source: "plus(a AS b, [1 AS c, 2][1])",
			want: newFunctionCall("plus", &ast.ColumnRefExpression{TableAlias: "", ColumnName: "a"},
				newFunctionCall(functionArrayElement,
					newFunctionCall(functionArray, &ast.LiteralExpression{TypeName: "UInt8"}, &ast.LiteralExpression{TypeName: "UInt8"}),
					&ast.LiteralExpression{TypeName: "UInt8"})),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, withoutSpans(parseExpressionFromSource(t, testCase.source)))
		})
	}
}

func TestParseExpressionReadsSubqueries(t *testing.T) {
	t.Parallel()

	exists, ok := parseExpressionFromSource(t, "EXISTS (SELECT 1 FROM t WHERE id = {id:UInt64})").(*ast.ExistsExpression)
	require.True(t, ok)
	require.NotNil(t, exists.InnerQuery)
	require.Len(t, exists.InnerQuery.ParameterReferences, 1)

	scalar, ok := parseExpressionFromSource(t, "(SELECT max(id) FROM t)").(*ast.ScalarSubqueryExpression)
	require.True(t, ok)
	require.Len(t, scalar.InnerQuery.OutputColumns, 1)

	membership, ok := parseExpressionFromSource(t, "id IN (SELECT id FROM t)").(*ast.InListExpression)
	require.True(t, ok)
	require.Len(t, membership.Values, 1)
	assert.IsType(t, &ast.ScalarSubqueryExpression{}, membership.Values[0])
}

func TestParseExpressionLeavesUnmodelledFormsUnknown(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"a IS 1", "CASE WHEN a THEN 1", "CAST(a)", "count(a", "a BETWEEN 1", "count() FILTER (score)", "INTERVAL 1", "INTERVAL 'day'",
		"[1, 2", "(1, 2", "f(x)[1", "CASE a WHEN 1 'x' END", "count() FILTER (WHERE a", "tuple(1 AS)", "tuple(1 AS 2, 3)",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			tokens, err := tokenise(source)
			require.NoError(t, err)
			expression := newParser(tokens).parseExpression()
			assert.True(t, containsUnknownExpression(expression), "got %#v", expression)
		})
	}
}

func TestIntervalUnitsAreLookedUpAndUnknownOnesRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		source       string
		wantType     string
		wantErrorAt  string
		wantPosition int
	}{
		{name: "a unit in upper case", source: "INTERVAL 1 DAY", wantType: "IntervalDay"},
		{name: "a plural unit in mixed case", source: "INTERVAL 2 Weeks", wantType: "IntervalWeek"},
		{name: "an abbreviated unit", source: "INTERVAL 1 d", wantType: "IntervalDay"},
		{name: "an abbreviation that is not a prefix of its unit", source: "INTERVAL 5 mcs", wantType: "IntervalMicrosecond"},
		{name: "an SQL_TSI_ unit", source: "INTERVAL 1 sql_tsi_month", wantType: "IntervalMonth"},
		{name: "a unit inside the string", source: "INTERVAL '3 qq'", wantType: "IntervalQuarter"},
		{name: "a value in a string followed by its unit", source: "INTERVAL '1' HOUR", wantType: "IntervalHour"},
		{name: "an unknown unit", source: "INTERVAL 1 foo", wantErrorAt: "foo", wantPosition: 11},
		{name: "a unit starting with a multi-byte letter", source: "INTERVAL 1 éon", wantErrorAt: "éon", wantPosition: 11},
		{name: "a letter that upper-cases to an ASCII unit", source: "INTERVAL 1 ſ", wantErrorAt: "ſ", wantPosition: 11},
		{name: "a quoted unit", source: "INTERVAL 1 `day`", wantErrorAt: "`day`", wantPosition: 11},
		{name: "an unknown unit inside the string", source: "INTERVAL '1 éon'", wantErrorAt: "'1 éon'", wantPosition: 9},
		{name: "a value in a string followed by an unknown unit", source: "INTERVAL '1' fortnight", wantErrorAt: "fortnight", wantPosition: 13},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.source)
			require.NoError(t, err)
			parserInstance := newParser(tokens)
			expression := parserInstance.parseExpression()
			require.True(t, parserInstance.AtEnd(), "stopped at %q", parserInstance.Current().Raw)
			if testCase.wantErrorAt == "" {
				require.NoError(t, parserInstance.syntaxError)
				assert.Equal(t, &ast.LiteralExpression{TypeName: testCase.wantType}, withoutSpans(expression))
				return
			}
			assert.IsType(t, &ast.UnknownExpression{}, expression)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, parserInstance.syntaxError, &parseError)
			assert.Equal(t, testCase.wantErrorAt, parseError.Token)
			assert.Equal(t, testCase.wantPosition, parseError.Position)
			assert.Contains(t, parseError.Message, "unknown interval unit")
			assert.True(t, utf8.ValidString(parseError.Error()))
			assert.NotContains(t, parseError.Error(), string(utf8.RuneError))
		})
	}
}

func TestAQueryWithAnUnknownIntervalUnitIsALocatedSyntaxError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{name: "a known unit", sql: "SELECT now() + INTERVAL 1 DAY AS later"},
		{name: "an unknown unit", sql: "SELECT now() + INTERVAL 1 foo AS later", wantErr: true},
		{name: "an unknown unit in a condition", sql: "SELECT 1 FROM t WHERE ts > now() - INTERVAL 3 lunar_months", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseError(t, testCase.sql)
			if !testCase.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, fault.ErrParse)
			assert.Contains(t, err.Error(), "unknown interval unit")
		})
	}
}

func TestLiteralNumberTypeClassifiesByShape(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		literal string
		want    string
	}{
		{literal: "0", want: "UInt8"},
		{literal: "255", want: "UInt8"},
		{literal: "256", want: "UInt16"},
		{literal: "65536", want: "UInt32"},
		{literal: "4294967296", want: "UInt64"},
		{literal: "0x1E", want: "UInt8"},
		{literal: "0xFFFF", want: "UInt16"},
		{literal: "0b101", want: "UInt8"},
		{literal: "0o17", want: "UInt8"},
		{literal: "1.1", want: "Float64"},
		{literal: "1e3", want: "Float64"},
		{literal: "2.5E-2", want: "Float64"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.literal, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, literalNumberType(testCase.literal).EngineName)
		})
	}
}

func TestNegativeLiteralTypeNameTakesTheSmallestSignedType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		literal string
		want    string
	}{
		{literal: "128", want: "Int8"},
		{literal: "129", want: "Int16"},
		{literal: "32769", want: "Int32"},
		{literal: "2147483649", want: "Int64"},
		{literal: "9223372036854775809", want: "Float64"},
		{literal: "0.5", want: "Float64"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.literal, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, negativeLiteralTypeName(testCase.literal))
		})
	}
}

func TestExpressionNestingPastTheCapRecordsASyntaxError(t *testing.T) {
	t.Parallel()

	const (
		capacity = 10
		nesting  = 30
	)
	testCases := []struct {
		name          string
		source        string
		analysisDepth int
	}{
		{name: "nested parentheses", source: strings.Repeat("(", nesting) + "1" + strings.Repeat(")", nesting)},
		{name: "NOT chain", source: strings.Repeat("NOT ", nesting) + "1"},
		{name: "unary minus chain", source: strings.Repeat("- ", nesting) + "a"},
		{name: "EXISTS subquery at the analysis cap", source: "EXISTS (SELECT 1 FROM t)", analysisDepth: capacity},
		{name: "IN subquery at the analysis cap", source: "x IN (SELECT y FROM t)", analysisDepth: capacity},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.source)
			require.NoError(t, err)
			parserInstance := newParser(tokens)
			parserInstance.maxParseDepth = capacity
			parserInstance.analysisDepth = testCase.analysisDepth

			require.NotNil(t, parserInstance.parseExpression())
			assert.ErrorIs(t, parserInstance.syntaxError, dialectkit.ErrParseDepth)
		})
	}
}

func TestExpressionNestingWithinTheCapRecordsNothing(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("((1 + 2)) AND NOT NOT x IN (SELECT y FROM t)")
	require.NoError(t, err)
	parserInstance := newParser(tokens)

	require.NotNil(t, parserInstance.parseExpression())
	assert.NoError(t, parserInstance.syntaxError)
}

func TestPeelParenthesesRemovesOnlyTheWrappingPairs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		sql    string
		inner  string
		layers int
	}{
		{name: "no parentheses", sql: "SELECT 1", inner: "SELECT 1", layers: 0},
		{name: "two wrapping pairs", sql: "((SELECT 1))", inner: "SELECT 1", layers: 2},
		{name: "two groups side by side", sql: "(a) + (b)", inner: "(a) + (b)", layers: 0},
		{name: "a pair around two groups", sql: "((a) + (b))", inner: "(a) + (b)", layers: 1},
		{name: "a pair around a group and more", sql: "(((a)) + b)", inner: "((a)) + b", layers: 1},
		{name: "an empty pair", sql: "()", inner: "", layers: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens := tokeniseStatement(t, testCase.sql)
			layers, inner := peelParentheses(tokens)
			assert.Equal(t, testCase.layers, layers)
			assert.Equal(t, testCase.inner, dialectkit.SourceBetween(testCase.sql, inner, 0, len(inner)))
			assert.Equal(t, len(inner), cap(inner), "the run has no spare capacity over the tokens after it")
		})
	}
}

func TestParenthesesAroundASubqueryCountAgainstTheDepthLimit(t *testing.T) {
	t.Parallel()

	wrapped := func(layers int) string {
		return "SELECT 1 WHERE x IN " + strings.Repeat("(", layers) + "SELECT 1" + strings.Repeat(")", layers)
	}
	testCases := []struct {
		name    string
		sql     string
		tooDeep bool
	}{
		{name: "a few pairs", sql: wrapped(4), tooDeep: false},
		{name: "more pairs than the limit", sql: wrapped(40), tooDeep: true},
		{name: "more pairs than the limit in a select list", sql: "SELECT " + strings.Repeat("(", 40) + "SELECT 1" + strings.Repeat(")", 40), tooDeep: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxParseDepth(32))
			_, err := engine.AnalyseQuery(t.Context(), nil, parseOneStatement(t, engine, testCase.sql))
			if !testCase.tooDeep {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, dialectkit.ErrParseDepth)
		})
	}
}

func TestParenthesesAroundASubqueryAreReadInLinearTime(t *testing.T) {
	t.Parallel()

	engine := New()
	analyseWrapped := func(layers int) func() {
		statement := parseOneStatement(t, engine, "SELECT "+strings.Repeat("(", layers)+"SELECT 1"+strings.Repeat(")", layers))
		return func() {
			_, err := engine.AnalyseQuery(t.Context(), nil, statement)
			require.ErrorIs(t, err, dialectkit.ErrParseDepth)
		}
	}
	scaling.Linear(t, analyseWrapped(10_000), analyseWrapped(40_000))
}
