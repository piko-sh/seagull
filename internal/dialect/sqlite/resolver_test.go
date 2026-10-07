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

package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestResolveFunctionCall(t *testing.T) {
	t.Parallel()

	engine := New()

	integerType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "integer"}
	realType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryFloat, EngineName: "real"}
	textType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}
	blobType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryBinary, EngineName: "blob"}
	unknownType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: ""}

	type testCase struct {
		description         string
		functionName        string
		argumentTypes       []sqlmodel.SQLType
		expectNil           bool
		expectedEngineName  string
		expectedCategory    sqlmodel.SQLTypeCategory
		expectedIsAggregate bool
	}

	cases := []testCase{
		{
			description:         "min with integer returns integer",
			functionName:        "min",
			argumentTypes:       []sqlmodel.SQLType{integerType},
			expectedEngineName:  "integer",
			expectedCategory:    sqlmodel.TypeCategoryInteger,
			expectedIsAggregate: true,
		},
		{
			description:         "min with text returns text",
			functionName:        "min",
			argumentTypes:       []sqlmodel.SQLType{textType},
			expectedEngineName:  "text",
			expectedCategory:    sqlmodel.TypeCategoryText,
			expectedIsAggregate: true,
		},
		{
			description:         "min with real returns real",
			functionName:        "min",
			argumentTypes:       []sqlmodel.SQLType{realType},
			expectedEngineName:  "real",
			expectedCategory:    sqlmodel.TypeCategoryFloat,
			expectedIsAggregate: true,
		},
		{
			description:         "max with integer returns integer",
			functionName:        "max",
			argumentTypes:       []sqlmodel.SQLType{integerType},
			expectedEngineName:  "integer",
			expectedCategory:    sqlmodel.TypeCategoryInteger,
			expectedIsAggregate: true,
		},
		{
			description:         "max with text returns text",
			functionName:        "max",
			argumentTypes:       []sqlmodel.SQLType{textType},
			expectedEngineName:  "text",
			expectedCategory:    sqlmodel.TypeCategoryText,
			expectedIsAggregate: true,
		},
		{
			description:         "max with blob returns blob",
			functionName:        "max",
			argumentTypes:       []sqlmodel.SQLType{blobType},
			expectedEngineName:  "blob",
			expectedCategory:    sqlmodel.TypeCategoryBinary,
			expectedIsAggregate: true,
		},
		{
			description:         "sum with integer returns 64-bit integer",
			functionName:        "sum",
			argumentTypes:       []sqlmodel.SQLType{integerType},
			expectedEngineName:  "bigint",
			expectedCategory:    sqlmodel.TypeCategoryInteger,
			expectedIsAggregate: true,
		},
		{
			description:         "sum with real returns real",
			functionName:        "sum",
			argumentTypes:       []sqlmodel.SQLType{realType},
			expectedEngineName:  "real",
			expectedCategory:    sqlmodel.TypeCategoryFloat,
			expectedIsAggregate: true,
		},
		{
			description:         "sum with text falls back to real",
			functionName:        "sum",
			argumentTypes:       []sqlmodel.SQLType{textType},
			expectedEngineName:  "real",
			expectedCategory:    sqlmodel.TypeCategoryFloat,
			expectedIsAggregate: true,
		},
		{
			description:        "coalesce with integer and text returns integer",
			functionName:       "coalesce",
			argumentTypes:      []sqlmodel.SQLType{integerType, textType},
			expectedEngineName: "integer",
			expectedCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			description:        "coalesce with unknown then text returns text",
			functionName:       "coalesce",
			argumentTypes:      []sqlmodel.SQLType{unknownType, textType},
			expectedEngineName: "text",
			expectedCategory:   sqlmodel.TypeCategoryText,
		},
		{
			description:        "coalesce with all unknown returns unknown",
			functionName:       "coalesce",
			argumentTypes:      []sqlmodel.SQLType{unknownType, unknownType},
			expectedEngineName: "",
			expectedCategory:   sqlmodel.TypeCategoryUnknown,
		},
		{
			description:   "min with no arguments returns nil",
			functionName:  "min",
			argumentTypes: []sqlmodel.SQLType{},
			expectNil:     true,
		},
		{
			description:   "sum with no arguments returns nil",
			functionName:  "sum",
			argumentTypes: []sqlmodel.SQLType{},
			expectNil:     true,
		},
		{
			description:   "unknown function returns nil",
			functionName:  "totally_unknown_function",
			argumentTypes: []sqlmodel.SQLType{integerType},
			expectNil:     true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.description, func(t *testing.T) {
			t.Parallel()

			resolution, err := engine.ResolveFunctionCall(t.Context(), nil, testCase.functionName, "", testCase.argumentTypes)
			require.NoError(t, err)

			if testCase.expectNil {
				assert.Nil(t, resolution, "expected nil resolution")
				return
			}

			require.NotNil(t, resolution, "expected non-nil resolution")
			assert.Equal(t, testCase.expectedEngineName, resolution.ReturnType.EngineName, "return type engine name")
			assert.Equal(t, testCase.expectedCategory, resolution.ReturnType.Category, "return type category")
			assert.Equal(t, testCase.expectedIsAggregate, resolution.IsAggregate, "is aggregate")
		})
	}
}

func TestResolveFunctionCallTypesValueFunctionsFromTheirArguments(t *testing.T) {
	t.Parallel()

	engine := New()
	textType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	integerType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4")
	realType := sqlmodel.NewFloatType("real", 64)
	unknownType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")

	testCases := []struct {
		name          string
		function      string
		argumentTypes []sqlmodel.SQLType
		want          sqlmodel.SQLType
	}{
		{name: "lag takes its value's type", function: "lag", argumentTypes: []sqlmodel.SQLType{textType}, want: textType},
		{name: "lead with an offset and default", function: "lead", argumentTypes: []sqlmodel.SQLType{integerType, integerType, integerType}, want: integerType},
		{name: "first_value", function: "first_value", argumentTypes: []sqlmodel.SQLType{textType}, want: textType},
		{name: "last_value", function: "last_value", argumentTypes: []sqlmodel.SQLType{integerType}, want: integerType},
		{name: "nth_value", function: "nth_value", argumentTypes: []sqlmodel.SQLType{textType, integerType}, want: textType},
		{name: "iif takes its branches' type", function: "iif", argumentTypes: []sqlmodel.SQLType{integerType, textType, textType}, want: textType},
		{name: "iif skips an untyped branch", function: "iif", argumentTypes: []sqlmodel.SQLType{integerType, unknownType, textType}, want: textType},
		{name: "ifnull takes its value's type", function: "ifnull", argumentTypes: []sqlmodel.SQLType{textType, textType}, want: textType},
		{name: "nullif takes its first value's type", function: "nullif", argumentTypes: []sqlmodel.SQLType{integerType, integerType}, want: integerType},
		{name: "abs keeps its argument's type", function: "abs", argumentTypes: []sqlmodel.SQLType{integerType}, want: integerType},
		{name: "ceil keeps an integer's type", function: "ceil", argumentTypes: []sqlmodel.SQLType{integerType}, want: integerType},
		{name: "floor keeps a real's type", function: "floor", argumentTypes: []sqlmodel.SQLType{realType}, want: realType},
		{name: "trunc keeps a real's type", function: "trunc", argumentTypes: []sqlmodel.SQLType{realType}, want: realType},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := engine.ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.argumentTypes)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			assert.Equal(t, testCase.want, resolution.ReturnType)
			assert.False(t, resolution.IsAggregate)
			assert.Equal(t, sqlmodel.DataAccessReadOnly, resolution.DataAccess)
		})
	}
}

func TestConditionalValuePositionsSkipTheConditions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		count int
		want  []int
	}{
		{name: "a condition and a value", count: 2, want: []int{1}},
		{name: "a condition, a value and an else", count: 3, want: []int{1, 2}},
		{name: "two pairs", count: 4, want: []int{1, 3}},
		{name: "two pairs and an else", count: 5, want: []int{1, 3, 4}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, conditionalValuePositions(testCase.count))
		})
	}
}

func TestIfIsIIFsSpelling(t *testing.T) {
	t.Parallel()

	integerType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "integer")
	textType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	resolution, err := New().ResolveFunctionCall(t.Context(), nil, "if", "", []sqlmodel.SQLType{integerType, textType, integerType, textType, textType})

	require.NoError(t, err)
	require.NotNil(t, resolution)
	require.Equal(t, textType, resolution.ReturnType)
	require.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
}

func TestResolveFunctionCallReturnsWhatStoppedItAsAnError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wrap        func(ctx context.Context) context.Context
		wantErr     error
		name        string
		wantMessage string
		wantRecords int
	}{
		{
			name:        "a context whose Err panics is returned as a panic error and logged once",
			wrap:        func(ctx context.Context) context.Context { return panickingContext{Context: ctx} },
			wantErr:     fault.ErrPanic,
			wantMessage: "sqlite: resolve function call: panic: context bug",
			wantRecords: 1,
		},
		{
			name: "a cancelled context stops the resolution with its cause",
			wrap: func(ctx context.Context) context.Context {
				cancelled, cancel := context.WithCancelCause(ctx)
				cancel(errAnalysisAbandoned)
				return cancelled
			},
			wantErr:     errAnalysisAbandoned,
			wantMessage: errAnalysisAbandoned.Error(),
			wantRecords: 0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newCapturingContext(t)
			integerType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "integer")

			var (
				resolution *ast.FunctionResolution
				err        error
			)
			require.NotPanics(t, func() {
				resolution, err = New().ResolveFunctionCall(testCase.wrap(ctx), nil, "sum", "", []sqlmodel.SQLType{integerType})
			})

			assert.Nil(t, resolution)
			require.ErrorIs(t, err, testCase.wantErr)
			require.EqualError(t, err, testCase.wantMessage)
			records := decodeRecords(t, logs)
			require.Len(t, records, testCase.wantRecords)
			for _, record := range records {
				assert.Equal(t, "WARN", record["level"])
				assert.Equal(t, "sqlite: resolve function call", record["operation"])
				assert.Contains(t, record["stack"], "seagull.sh/seagull/internal/dialect/sqlite.")
			}
		})
	}
}
