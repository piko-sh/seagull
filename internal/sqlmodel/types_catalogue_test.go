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

package sqlmodel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctionRequiredArguments(t *testing.T) {
	t.Parallel()

	required := FunctionArgument{Name: "required", Type: NewSQLType(TypeCategoryText, "text"), IsOptional: false}
	optional := FunctionArgument{Name: "optional", Type: NewSQLType(TypeCategoryText, "text"), IsOptional: true}

	testCases := []struct {
		name         string
		arguments    []FunctionArgument
		minArguments int
		want         int
		isVariadic   bool
	}{
		{name: "no arguments", arguments: nil, want: 0},
		{name: "every argument required", arguments: []FunctionArgument{required, required}, want: 2},
		{name: "trailing optional argument", arguments: []FunctionArgument{required, optional}, want: 1},
		{name: "leading optional argument", arguments: []FunctionArgument{optional, optional}, want: 0},
		{name: "declared minimum wins", arguments: []FunctionArgument{required, required}, minArguments: 1, want: 1},
		{name: "variadic minimum is authoritative", arguments: []FunctionArgument{required}, isVariadic: true, want: 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			function := &Function{}
			function.Arguments = testCase.arguments
			function.MinArguments = testCase.minArguments
			function.IsVariadic = testCase.isVariadic
			assert.Equal(t, testCase.want, function.RequiredArguments())
		})
	}
}

func TestNewColumn(t *testing.T) {
	t.Parallel()

	sqlType := NewSQLType(TypeCategoryInteger, "int4")

	column := NewColumn("id", sqlType, true)

	want := Column{}
	want.Name = "id"
	want.SQLType = sqlType
	want.Nullable = true
	assert.Equal(t, want, column)
}

func TestNewFunction(t *testing.T) {
	t.Parallel()

	arguments := []FunctionArgument{{Name: "value", Type: NewSQLType(TypeCategoryText, "text"), IsOptional: false}}
	returnType := NewSQLType(TypeCategoryJSON, "json")

	testCases := []struct {
		want    func() *Function
		name    string
		options []FunctionOption
	}{
		{
			name:    "no options leaves every other attribute unset",
			options: nil,
			want:    func() *Function { return &Function{} },
		},
		{
			name:    "name",
			options: []FunctionOption{WithFunctionName("json_build_array")},
			want: func() *Function {
				signature := &Function{}
				signature.Name = "json_build_array"
				return signature
			},
		},
		{
			name:    "aggregate",
			options: []FunctionOption{WithAggregate()},
			want: func() *Function {
				signature := &Function{}
				signature.IsAggregate = true
				return signature
			},
		},
		{
			name:    "variadic with a zero minimum",
			options: []FunctionOption{WithVariadic(0)},
			want: func() *Function {
				signature := &Function{}
				signature.IsVariadic = true
				return signature
			},
		},
		{
			name:    "variadic with a minimum",
			options: []FunctionOption{WithVariadic(2)},
			want: func() *Function {
				signature := &Function{}
				signature.IsVariadic = true
				signature.MinArguments = 2
				return signature
			},
		},
		{
			name:    "minimum arguments",
			options: []FunctionOption{WithMinArguments(1)},
			want: func() *Function {
				signature := &Function{}
				signature.MinArguments = 1
				return signature
			},
		},
		{
			name:    "returns set",
			options: []FunctionOption{WithReturnsSet()},
			want: func() *Function {
				signature := &Function{}
				signature.ReturnsSet = true
				return signature
			},
		},
		{
			name:    "options apply in order",
			options: []FunctionOption{WithFunctionName("first"), WithFunctionName("second")},
			want: func() *Function {
				signature := &Function{}
				signature.Name = "second"
				return signature
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			signature := NewFunction(arguments, returnType, FunctionNullableNeverNull, testCase.options...)

			want := testCase.want()
			want.Arguments = arguments
			want.ReturnType = returnType
			want.NullableBehaviour = FunctionNullableNeverNull
			assert.Equal(t, want, signature)
		})
	}
}

func TestColumnValueTypeAppliesArrayDimensions(t *testing.T) {
	t.Parallel()

	text := NewSQLType(TypeCategoryText, "text")
	scalar := NewColumn("note", text, true)
	require.Equal(t, text, scalar.ValueType())

	matrix := NewColumn("grid", text, false)
	matrix.IsArray = true
	matrix.ArrayDimensions = 2
	valueType := matrix.ValueType()
	require.Equal(t, TypeCategoryArray, valueType.Category)
	require.NotNil(t, valueType.ElementType)
	require.Equal(t, TypeCategoryArray, valueType.ElementType.Category)
	require.Equal(t, text, *valueType.ElementType.ElementType)
}
