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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestBinaryResultTypesPostgreSQLOperators(t *testing.T) {
	t.Parallel()

	engine := New()
	jsonb := engine.NormaliseTypeName("jsonb")
	text := engine.NormaliseTypeName("text")
	integer := engine.NormaliseTypeName("integer")
	bigint := engine.NormaliseTypeName("bigint")
	inet := engine.NormaliseTypeName("inet")
	textArray := engine.NormaliseTypeName("text[]")

	testCases := []struct {
		name      string
		operator  string
		left      sqlmodel.SQLType
		right     sqlmodel.SQLType
		expected  string
		behaviour sqlmodel.FunctionNullableBehaviour
		typed     bool
	}{
		{name: "-> on jsonb keeps jsonb", operator: "->", left: jsonb, right: text, expected: "jsonb", behaviour: sqlmodel.FunctionNullableCalledOnNull, typed: true},
		{name: "#> on jsonb keeps jsonb", operator: "#>", left: jsonb, right: textArray, expected: "jsonb", behaviour: sqlmodel.FunctionNullableCalledOnNull, typed: true},
		{name: "-> on an untyped value yields json", operator: "->", left: text, right: text, expected: "json", behaviour: sqlmodel.FunctionNullableCalledOnNull, typed: true},
		{name: "->> yields text", operator: "->>", left: jsonb, right: text, expected: "text", behaviour: sqlmodel.FunctionNullableCalledOnNull, typed: true},
		{name: "#>> yields text", operator: "#>>", left: jsonb, right: textArray, expected: "text", behaviour: sqlmodel.FunctionNullableCalledOnNull, typed: true},
		{name: "|| on text yields text", operator: "||", left: text, right: integer, expected: "text", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull, typed: true},
		{name: "|| on an array yields the array", operator: "||", left: textArray, right: text, expected: "text[]", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull, typed: true},
		{name: "|| on jsonb yields jsonb", operator: "||", left: jsonb, right: jsonb, expected: "jsonb", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull, typed: true},
		{name: "? tests key existence", operator: "?", left: jsonb, right: text, expected: "bool", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull, typed: true},
		{name: "<< shifts an integer", operator: "<<", left: integer, right: integer, expected: "int4", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull, typed: true},
		{name: "<< between networks tests containment", operator: "<<", left: inet, right: inet, expected: "bool", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull, typed: true},
		{name: "& is left to the structural default", operator: "&", left: integer, right: bigint, typed: false},
		{name: "/ is left to the structural default", operator: "/", left: integer, right: integer, typed: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, behaviour, typed := engine.BinaryResult(testCase.operator, testCase.left, testCase.right)
			require.Equal(t, testCase.typed, typed)
			if !typed {
				return
			}
			require.Equal(t, testCase.expected, result.EngineName)
			require.Equal(t, testCase.behaviour, behaviour)
		})
	}
}

func TestUnaryResultTypesNotAsABooleanNullOnANullOperand(t *testing.T) {
	t.Parallel()

	engine := New()
	result, behaviour, typed := engine.UnaryResult("not", engine.NormaliseTypeName("boolean"))
	require.True(t, typed)
	require.Equal(t, sqlmodel.TypeCategoryBoolean, result.Category)
	require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)

	_, _, typed = engine.UnaryResult("-", engine.NormaliseTypeName("integer"))
	require.False(t, typed)
}

func TestCoreTypesComeFromTheNormaliser(t *testing.T) {
	t.Parallel()

	core := New().CoreTypes()
	require.Equal(t, "bool", core.Boolean.EngineName)
	require.Equal(t, "text", core.Text.EngineName)
	require.Equal(t, 64, core.Count.Width)
	require.Equal(t, sqlmodel.TypeCategoryJSON, core.JSON.Category)
	require.Equal(t, 32, core.RowCount.Width)
	require.Equal(t, sqlmodel.TypeCategoryUnknown, core.RowID.Category)
}

func TestUnquotedIdentifiersFoldToLowerCase(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise(`SELECT Name, "Mixed" FROM Users`)
	require.NoError(t, err)
	values := make([]string, 0, len(tokens))
	for _, current := range tokens {
		if current.Kind == tokenIdentifier {
			values = append(values, current.Value)
		}
	}
	require.Contains(t, values, "name")
	require.Contains(t, values, "Mixed")
	require.Contains(t, values, "users")
}

func TestDistinctFromComparisonsAreNullTests(t *testing.T) {
	t.Parallel()

	features := New().Info().OperatorFeatures
	for _, operator := range []string{"IS DISTINCT FROM", "IS NOT DISTINCT FROM"} {
		require.Equal(t, ast.SQLFeatureIsNull, features[operator], operator)
	}
}

func accessorDialect(options ...Option) *Dialect {
	state := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "summary")
	other := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "sketch")
	accessor := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "accessor_mean")
	float := sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "float8")
	integer := sqlmodel.NewIntegerType("int8", widthBigint, false)
	register := WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
		builder.Catalogue.Functions["mean"] = []*sqlmodel.Function{
			{Name: "mean", ReturnType: accessor},
			{Name: "mean", Arguments: []sqlmodel.FunctionArgument{{Name: "state", Type: state}}, ReturnType: float},
			{Name: "mean", Arguments: []sqlmodel.FunctionArgument{{Name: "state", Type: other}}, ReturnType: integer},
		}
	})
	return New(append([]Option{register}, options...)...)
}

func TestAccessorArrowsApplyTheAccessorsFunctionToTheValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		engine   *Dialect
		left     string
		wantType string
	}{
		{name: "the overload that takes the state", engine: accessorDialect(WithAccessorArrows("accessor_mean")), left: "summary", wantType: "float8"},
		{name: "the overload that takes another state", engine: accessorDialect(WithAccessorArrows("accessor_mean")), left: "sketch", wantType: "int8"},
		{name: "no overload takes the value", engine: accessorDialect(WithAccessorArrows("accessor_mean")), left: "text", wantType: ""},
		{name: "a JSON document keeps the JSON operator", engine: accessorDialect(WithAccessorArrows("accessor_mean")), left: "jsonb", wantType: "jsonb"},
		{name: "a dialect without accessors reads the JSON operator", engine: accessorDialect(), left: "summary", wantType: "json"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			left := testCase.engine.NormaliseTypeName(testCase.left)
			right := testCase.engine.NormaliseTypeName("accessor_mean")
			result, _, typed := testCase.engine.BinaryResult("->", left, right)

			require.True(t, typed)
			require.Equal(t, testCase.wantType, result.EngineName)
		})
	}
}

func TestBinaryResultTypesTheDistanceOperators(t *testing.T) {
	t.Parallel()

	engine := New()
	point := engine.NormaliseTypeName("point")
	vector := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "vector")
	text := engine.NormaliseTypeName("text")
	tsquery := engine.NormaliseTypeName("tsquery")

	testCases := []struct {
		name     string
		operator string
		left     sqlmodel.SQLType
		right    sqlmodel.SQLType
		expected string
	}{
		{name: "the distance between points", operator: "<->", left: point, right: point, expected: "float8"},
		{name: "the distance between vectors", operator: "<->", left: vector, right: vector, expected: "float8"},
		{name: "the cosine distance between vectors", operator: "<=>", left: vector, right: vector, expected: "float8"},
		{name: "the negative inner product of vectors", operator: "<#>", left: vector, right: vector, expected: "float8"},
		{name: "the taxicab distance between vectors", operator: "<+>", left: vector, right: vector, expected: "float8"},
		{name: "the trigram distance between texts", operator: "<->", left: text, right: text, expected: "float4"},
		{name: "the word trigram distance between texts", operator: "<<->", left: text, right: text, expected: "float4"},
		{name: "a phrase of text search queries", operator: "<->", left: tsquery, right: tsquery, expected: "tsquery"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, behaviour, typed := engine.BinaryResult(testCase.operator, testCase.left, testCase.right)
			require.True(t, typed)
			require.Equal(t, testCase.expected, result.EngineName)
			require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)
		})
	}
}
