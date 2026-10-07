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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestNotIsBoolOnlyForABoolOperand(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		operand string
		want    string
	}{
		{name: "a Bool", operand: "Bool", want: "Bool"},
		{name: "an integer", operand: "UInt32", want: "UInt8"},
		{name: "a predicate's UInt8", operand: "UInt8", want: "UInt8"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, behaviour, typed := unaryResult("NOT", normaliseBuiltinTypeName(testCase.operand))
			require.True(t, typed)
			require.Equal(t, testCase.want, result.EngineName)
			require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)
		})
	}
}

func TestPredicatesAreTypedAsClickHouseTypesThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		predicate ast.Expression
		name      string
		operands  []string
		want      string
		behaviour sqlmodel.FunctionNullableBehaviour
	}{
		{name: "a comparison", predicate: &ast.ComparisonExpression{Operator: "="}, operands: []string{"Int32", "Int32"}, want: "UInt8", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "a comparison of Bools", predicate: &ast.ComparisonExpression{Operator: "="}, operands: []string{"Bool", "Bool"}, want: "UInt8", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "a pattern match", predicate: &ast.ComparisonExpression{Operator: "LIKE"}, operands: []string{"String", "String"}, want: "UInt8", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "a BETWEEN", predicate: &ast.BetweenExpression{}, operands: []string{"Int32", "Int32", "Int32"}, want: "UInt8", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "an IN list is NULL only for its tested value", predicate: &ast.InListExpression{}, operands: []string{"Int32", "Int32"}, want: "UInt8", behaviour: sqlmodel.FunctionNullableNullWhenArgumentNull},
		{name: "an IS NULL test", predicate: &ast.IsNullExpression{}, operands: []string{"Int32"}, want: "UInt8", behaviour: sqlmodel.FunctionNullableNeverNull},
		{name: "an EXISTS", predicate: &ast.ExistsExpression{}, operands: nil, want: "UInt8", behaviour: sqlmodel.FunctionNullableNeverNull},
		{name: "AND of predicates", predicate: &ast.LogicalOpExpression{Operator: "AND"}, operands: []string{"UInt8", "UInt8"}, want: "UInt8", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "AND with a Bool operand", predicate: &ast.LogicalOpExpression{Operator: "AND"}, operands: []string{"UInt8", "Bool"}, want: "Bool", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "OR of an integer and a Bool", predicate: &ast.LogicalOpExpression{Operator: "OR"}, operands: []string{"Int32", "Bool"}, want: "Bool", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			operands := make([]sqlmodel.SQLType, len(testCase.operands))
			for index, operand := range testCase.operands {
				operands[index] = normaliseBuiltinTypeName(operand)
			}

			result, behaviour, typed := predicateResult(testCase.predicate, operands)

			require.True(t, typed)
			require.Equal(t, testCase.want, result.EngineName)
			require.Equal(t, testCase.behaviour, behaviour)
		})
	}
}

func TestPredicateResultLeavesAnythingElseAlone(t *testing.T) {
	t.Parallel()

	_, _, typed := predicateResult(&ast.BinaryOpExpression{Operator: "+"}, nil)

	require.False(t, typed)
}
