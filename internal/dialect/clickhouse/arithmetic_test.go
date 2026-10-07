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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestArithmeticResultFollowsClickHouse(t *testing.T) {
	t.Parallel()

	dateTime64 := normaliseBuiltinTypeName("DateTime64(3)")
	testCases := []struct {
		name  string
		left  string
		right string
		want  string
		kind  arithmeticKind
	}{
		{name: "UInt8 plus UInt8 widens", kind: arithmeticAddition, left: "UInt8", right: "UInt8", want: "UInt16"},
		{name: "Int32 times Int32 widens", kind: arithmeticMultiplication, left: "Int32", right: "Int32", want: "Int64"},
		{name: "UInt8 minus UInt8 is signed", kind: arithmeticSubtraction, left: "UInt8", right: "UInt8", want: "Int16"},
		{name: "UInt64 minus UInt64 stays 64 bit", kind: arithmeticSubtraction, left: "UInt64", right: "UInt64", want: "Int64"},
		{name: "UInt32 plus Int8 is signed", kind: arithmeticAddition, left: "UInt32", right: "Int8", want: "Int64"},
		{name: "Int64 plus UInt8", kind: arithmeticAddition, left: "Int64", right: "UInt8", want: "Int64"},
		{name: "UInt64 times UInt8", kind: arithmeticMultiplication, left: "UInt64", right: "UInt8", want: "UInt64"},
		{name: "Int128 plus UInt8 keeps its width", kind: arithmeticAddition, left: "Int128", right: "UInt8", want: "Int128"},
		{name: "Float64 times an integer", kind: arithmeticMultiplication, left: "Float64", right: "UInt8", want: "Float64"},
		{name: "Float32 plus Float32", kind: arithmeticAddition, left: "Float32", right: "Float32", want: "Float64"},
		{name: "integer division is a Float64", kind: arithmeticDivision, left: "UInt8", right: "UInt8", want: "Float64"},
		{name: "modulo takes the divisor's width", kind: arithmeticModulo, left: "UInt32", right: "Int8", want: "UInt8"},
		{name: "signed modulo by an unsigned widens", kind: arithmeticModulo, left: "Int64", right: "UInt8", want: "Int16"},
		{name: "modulo by a wider divisor", kind: arithmeticModulo, left: "Int8", right: "Int64", want: "Int64"},
		{name: "signed modulo by an unsigned of the same width", kind: arithmeticModulo, left: "Int32", right: "UInt32", want: "Int64"},
		{name: "intDiv keeps the dividend's width", kind: arithmeticIntegerDivision, left: "UInt8", right: "UInt8", want: "UInt8"},
		{name: "intDiv is signed when either side is", kind: arithmeticIntegerDivision, left: "Int8", right: "UInt64", want: "Int8"},
		{name: "intDiv of a float by an integer", kind: arithmeticIntegerDivision, left: "Float64", right: "UInt8", want: "Int64"},
		{name: "decimal times an integer", kind: arithmeticMultiplication, left: "Decimal64", right: "UInt8", want: "Decimal64"},
		{name: "decimal plus a float", kind: arithmeticAddition, left: "Decimal64", right: "Float64", want: "Float64"},
		{name: "an integer times a decimal", kind: arithmeticMultiplication, left: "UInt8", right: "Decimal64", want: "Decimal64"},
		{name: "two decimals give the wider", kind: arithmeticAddition, left: "Decimal64", right: "Decimal128", want: "Decimal128"},
		{name: "the wider decimal on the left", kind: arithmeticAddition, left: "Decimal128", right: "Decimal64", want: "Decimal128"},
		{name: "decimal division stays decimal", kind: arithmeticDivision, left: "Decimal64", right: "UInt8", want: "Decimal64"},
		{name: "intDiv of an integer by a float", kind: arithmeticIntegerDivision, left: "UInt16", right: "Float64", want: "UInt16"},
		{name: "DateTime plus an integer", kind: arithmeticAddition, left: "DateTime", right: "UInt8", want: "DateTime"},
		{name: "an integer plus a Date", kind: arithmeticAddition, left: "UInt8", right: "Date", want: "Date"},
		{name: "Date minus an interval", kind: arithmeticSubtraction, left: "Date", right: "IntervalDay", want: "Date"},
		{name: "DateTime minus DateTime", kind: arithmeticSubtraction, left: "DateTime", right: "DateTime", want: "Int32"},
		{name: "two dates cannot be added", kind: arithmeticAddition, left: "Date", right: "Date", want: ""},
		{name: "an unknown operand is unknown", kind: arithmeticMultiplication, left: "", right: "UInt8", want: ""},
		{name: "a string operand is unknown", kind: arithmeticAddition, left: "String", right: "UInt8", want: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			left, right := typeOrDecimal(testCase.left), typeOrDecimal(testCase.right)
			assert.Equal(t, testCase.want, arithmeticResult(testCase.kind, left, right).EngineName)
		})
	}

	t.Run("DateTime64 difference is a decimal at the column's precision", func(t *testing.T) {
		t.Parallel()
		difference := arithmeticResult(arithmeticSubtraction, dateTime64, dateTime64)
		assert.Equal(t, sqlmodel.TypeCategoryDecimal, difference.Category)
		assert.Equal(t, 3, *difference.Scale)
	})
}

func typeOrDecimal(name string) sqlmodel.SQLType {
	switch name {
	case "":
		return unknownType()
	case "Decimal64":
		return normaliseBuiltinTypeName("Decimal64(2)")
	case "Decimal128":
		return normaliseBuiltinTypeName("Decimal128(2)")
	default:
		return primitiveType(name)
	}
}

func TestNegationAndAbsoluteResults(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		operand      string
		wantNegation string
		wantAbsolute string
	}{
		{operand: "UInt8", wantNegation: "Int16", wantAbsolute: "UInt8"},
		{operand: "UInt64", wantNegation: "Int64", wantAbsolute: "UInt64"},
		{operand: "Int32", wantNegation: "Int32", wantAbsolute: "UInt32"},
		{operand: "Float32", wantNegation: "Float32", wantAbsolute: "Float32"},
		{operand: "String", wantNegation: "", wantAbsolute: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.operand, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.wantNegation, negationResult(primitiveType(testCase.operand)).EngineName)
			assert.Equal(t, testCase.wantAbsolute, absoluteResult(primitiveType(testCase.operand)).EngineName)
		})
	}
}

func TestOperatorTyperTypesArithmetic(t *testing.T) {
	t.Parallel()

	engine := New()
	product, behaviour, typed := engine.BinaryResult("*", primitiveType("Float64"), primitiveType("UInt8"))
	assert.True(t, typed)
	assert.Equal(t, "Float64", product.EngineName)
	assert.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)

	_, _, typed = engine.BinaryResult("=", primitiveType("UInt8"), primitiveType("UInt8"))
	assert.False(t, typed, "a comparison is left to the structural default")

	negated, _, typed := engine.UnaryResult("-", primitiveType("UInt8"))
	assert.True(t, typed)
	assert.Equal(t, "Int16", negated.EngineName)
}

func TestCanImplicitCastWidensNumbers(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.True(t, engine.CanImplicitCast(primitiveType("UInt8"), primitiveType("Float64")))
	assert.True(t, engine.CanImplicitCast(primitiveType("Int64"), normaliseBuiltinTypeName("Decimal(18, 2)")))
	assert.True(t, engine.CanImplicitCast(normaliseBuiltinTypeName("Decimal(18, 2)"), primitiveType("Float64")))
	assert.False(t, engine.CanImplicitCast(primitiveType("Float64"), primitiveType("UInt8")))
	assert.False(t, engine.CanImplicitCast(primitiveType("String"), primitiveType("UInt8")))
}

func TestRemaindersAndBucketsAreTypedAsClickHouseTypesThem(t *testing.T) {
	t.Parallel()

	resolver := newFunctionResolver()
	testCases := []struct {
		function  string
		arguments []string
		want      string
	}{
		{function: "modulo", arguments: []string{"Int32", "Int16"}, want: "Int32"},
		{function: "modulo", arguments: []string{"Int16", "Int32"}, want: "Int64"},
		{function: "modulo", arguments: []string{"Int64", "Int8"}, want: "Int16"},
		{function: "modulo", arguments: []string{"Int8", "Int64"}, want: "Int64"},
		{function: "modulo", arguments: []string{"UInt32", "UInt8"}, want: "UInt8"},
		{function: "modulo", arguments: []string{"Int32", "UInt8"}, want: "Int16"},
		{function: "modulo", arguments: []string{"UInt64", "Int64"}, want: "UInt64"},
		{function: "moduloOrZero", arguments: []string{"Int32", "Int16"}, want: "Int32"},
		{function: "positiveModulo", arguments: []string{"UInt8", "UInt8"}, want: "UInt8"},
		{function: "positiveModulo", arguments: []string{"Int64", "UInt8"}, want: "UInt8"},
		{function: "positiveModulo", arguments: []string{"Int32", "Int16"}, want: "UInt16"},
		{function: "positiveModulo", arguments: []string{"Int8", "Int64"}, want: "UInt64"},
		{function: "positiveModulo", arguments: []string{"UInt8", "UInt32"}, want: "UInt32"},
		{function: "positiveModulo", arguments: []string{"Float64", "Int64"}, want: "Float64"},
		{function: "pmod", arguments: []string{"Int64", "Int64"}, want: "UInt64"},
		{function: "positive_modulo", arguments: []string{"UInt8", "UInt16"}, want: "UInt16"},
		{function: "widthBucket", arguments: []string{"UInt8", "UInt8", "UInt8", "UInt8"}, want: "UInt16"},
		{function: "widthBucket", arguments: []string{"Float64", "Float64", "Float64", "UInt16"}, want: "UInt32"},
		{function: "widthBucket", arguments: []string{"Float64", "Float64", "Float64", "UInt32"}, want: "UInt64"},
		{function: "width_bucket", arguments: []string{"Float64", "Float64", "Float64", "UInt64"}, want: "UInt64"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.function+" "+strings.Join(testCase.arguments, " "), func(t *testing.T) {
			t.Parallel()

			argumentTypes := make([]sqlmodel.SQLType, 0, len(testCase.arguments))
			for _, argument := range testCase.arguments {
				argumentTypes = append(argumentTypes, normaliseBuiltinTypeName(argument))
			}
			resolution, err := resolver.ResolveFunctionCall(t.Context(), nil, testCase.function, "", argumentTypes)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType.EngineName)
		})
	}
}
