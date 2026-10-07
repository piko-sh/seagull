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

package catalogue

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestMergeCustomFunctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		catalogue       *sqlmodel.Catalogue
		customFunctions []sqlmodel.CustomFunctionConfig
		expectFunctions map[string]int
	}{
		{
			name:            "empty list leaves catalogue unchanged",
			catalogue:       fakedialect.NewCatalogue("public"),
			customFunctions: nil,
			expectFunctions: map[string]int{},
		},
		{
			name:      "adds function to default schema",
			catalogue: fakedialect.NewCatalogue("public"),
			customFunctions: []sqlmodel.CustomFunctionConfig{
				{Name: "my_func", ReturnType: "integer", Arguments: []string{"text"}},
			},
			expectFunctions: map[string]int{"my_func": 1},
		},
		{
			name: "a missing default schema is created",
			catalogue: &sqlmodel.Catalogue{
				DefaultSchema: "missing",
				Schemas:       map[string]*sqlmodel.Schema{},
			},
			customFunctions: []sqlmodel.CustomFunctionConfig{
				{Name: "some_func", ReturnType: "text"},
			},
			expectFunctions: map[string]int{"some_func": 1},
		},
		{
			name:      "multiple functions are all added",
			catalogue: fakedialect.NewCatalogue("public"),
			customFunctions: []sqlmodel.CustomFunctionConfig{
				{Name: "func_a", ReturnType: "integer"},
				{Name: "func_b", ReturnType: "text", Arguments: []string{"integer", "text"}},
				{Name: "func_c", ReturnType: "boolean"},
			},
			expectFunctions: map[string]int{"func_a": 1, "func_b": 1, "func_c": 1},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			MergeCustomFunctions(testCase.catalogue, &fakedialect.Engine{}, testCase.customFunctions)

			schema := testCase.catalogue.Schemas[testCase.catalogue.DefaultSchema]
			if len(testCase.expectFunctions) == 0 {
				if schema != nil {
					assert.Empty(t, schema.Functions)
				}
				return
			}
			require.NotNil(t, schema, "default schema should exist")
			for functionName, expectedCount := range testCase.expectFunctions {
				signatures, exists := schema.Functions[functionName]
				assert.True(t, exists, "function %q should be present", functionName)
				assert.Len(t, signatures, expectedCount)
				for _, signature := range signatures {
					assert.Equal(t, schema.Name, signature.Schema)
				}
			}
			assert.Len(t, schema.Functions, len(testCase.expectFunctions))
		})
	}
}

func TestMergeCustomFunctionsIgnoresANilCatalogue(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		MergeCustomFunctions(nil, &fakedialect.Engine{}, []sqlmodel.CustomFunctionConfig{{Name: "f", ReturnType: "text"}})
	})
}

func TestConvertCustomFunction(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{}

	tests := []struct {
		name               string
		config             sqlmodel.CustomFunctionConfig
		expectName         string
		expectArguments    int
		expectMinArguments int
		expectAggregate    bool
		expectVariadic     bool
		expectDataAccess   sqlmodel.FunctionDataAccess
	}{
		{
			name:               "valid with arguments maps arguments correctly",
			config:             sqlmodel.CustomFunctionConfig{Name: "concat_ws", ReturnType: "text", Arguments: []string{"text", "integer"}},
			expectName:         "concat_ws",
			expectArguments:    2,
			expectMinArguments: 2,
			expectDataAccess:   sqlmodel.DataAccessReadOnly,
		},
		{
			name: "MinArguments explicitly set uses provided value",
			config: sqlmodel.CustomFunctionConfig{
				Name: "variadic_fn", ReturnType: "text", Arguments: []string{"text", "integer", "boolean"}, MinArguments: 1,
			},
			expectName:         "variadic_fn",
			expectArguments:    3,
			expectMinArguments: 1,
			expectDataAccess:   sqlmodel.DataAccessReadOnly,
		},
		{
			name:               "MinArguments zero defaults to len of arguments",
			config:             sqlmodel.CustomFunctionConfig{Name: "all_required", ReturnType: "integer", Arguments: []string{"text", "text"}},
			expectName:         "all_required",
			expectArguments:    2,
			expectMinArguments: 2,
			expectDataAccess:   sqlmodel.DataAccessReadOnly,
		},
		{
			name:               "aggregate function sets IsAggregate",
			config:             sqlmodel.CustomFunctionConfig{Name: "my_sum", ReturnType: "numeric", Arguments: []string{"numeric"}, IsAggregate: true},
			expectName:         "my_sum",
			expectArguments:    1,
			expectMinArguments: 1,
			expectAggregate:    true,
			expectDataAccess:   sqlmodel.DataAccessReadOnly,
		},
		{
			name:               "variadic function sets IsVariadic",
			config:             sqlmodel.CustomFunctionConfig{Name: "format_str", ReturnType: "text", Arguments: []string{"text"}, IsVariadic: true},
			expectName:         "format_str",
			expectArguments:    1,
			expectMinArguments: 1,
			expectVariadic:     true,
			expectDataAccess:   sqlmodel.DataAccessReadOnly,
		},
		{
			name:             "a function declared as changing data says so",
			config:           sqlmodel.CustomFunctionConfig{Name: "bump", ReturnType: "integer", DataAccess: "modifies_data"},
			expectName:       "bump",
			expectDataAccess: sqlmodel.DataAccessModifiesData,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := convertCustomFunction(&testCase.config, engine)

			require.NotNil(t, result)
			assert.Equal(t, testCase.expectName, result.Name)
			assert.Len(t, result.Arguments, testCase.expectArguments)
			assert.Equal(t, testCase.expectMinArguments, result.MinArguments)
			assert.Equal(t, testCase.expectAggregate, result.IsAggregate)
			assert.Equal(t, testCase.expectVariadic, result.IsVariadic)
			assert.Equal(t, testCase.expectDataAccess, result.DataAccess)
			for argumentIndex, argument := range result.Arguments {
				assert.Empty(t, argument.Name, "a declaration gives no argument names")
				assert.Equal(t, testCase.config.Arguments[argumentIndex], argument.Type.EngineName)
			}
			assert.Equal(t, testCase.config.ReturnType, result.ReturnType.EngineName)
		})
	}
}

func TestValidateCustomFunctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      sqlmodel.CustomFunctionConfig
		wantMessage string
	}{
		{name: "no name", config: sqlmodel.CustomFunctionConfig{ReturnType: "text"}, wantMessage: "custom function 1: the name is empty"},
		{name: "no return type", config: sqlmodel.CustomFunctionConfig{Name: "f"}, wantMessage: "custom function 1 (f): the return type is empty"},
		{name: "negative minimum", config: sqlmodel.CustomFunctionConfig{Name: "f", ReturnType: "text", MinArguments: -1}, wantMessage: "negative"},
		{name: "minimum above arguments", config: sqlmodel.CustomFunctionConfig{Name: "f", ReturnType: "text", MinArguments: 2, Arguments: []string{"text"}}, wantMessage: "larger than the 1 declared"},
		{name: "variadic without arguments", config: sqlmodel.CustomFunctionConfig{Name: "f", ReturnType: "text", IsVariadic: true}, wantMessage: "variadic"},
		{name: "argument without type", config: sqlmodel.CustomFunctionConfig{Name: "f", ReturnType: "text", Arguments: []string{"text", " "}}, wantMessage: "argument 2 has no type"},
		{name: "bad nullable", config: sqlmodel.CustomFunctionConfig{Name: "f", ReturnType: "text", Nullable: "sometimes"}, wantMessage: "unrecognised nullable behaviour \"sometimes\""},
		{name: "bad data access", config: sqlmodel.CustomFunctionConfig{Name: "f", ReturnType: "text", DataAccess: "volatile"}, wantMessage: "unrecognised data access \"volatile\""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			valid := sqlmodel.CustomFunctionConfig{Name: "ok", ReturnType: "text"}
			err := ValidateCustomFunctions([]sqlmodel.CustomFunctionConfig{valid, testCase.config})
			require.ErrorIs(t, err, fault.ErrInvalidConfig)
			assert.Contains(t, err.Error(), testCase.wantMessage)
		})
	}

	require.NoError(t, ValidateCustomFunctions(nil))
	require.NoError(t, ValidateCustomFunctions([]sqlmodel.CustomFunctionConfig{
		{Name: "f", ReturnType: "text", Nullable: "NEVER_NULL", DataAccess: "read_only", Arguments: []string{"text"}, MinArguments: 1, IsVariadic: true},
	}))
}

func TestParseNullableBehaviour(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		expected    sqlmodel.FunctionNullableBehaviour
		expectError bool
	}{
		{name: "never_null", input: "never_null", expected: sqlmodel.FunctionNullableNeverNull},
		{name: "called_on_null", input: "called_on_null", expected: sqlmodel.FunctionNullableCalledOnNull},
		{name: "returns_null_on_null", input: "returns_null_on_null", expected: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "empty string", input: "", expected: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "unknown value", input: "something_unexpected", expected: sqlmodel.FunctionNullableReturnsNullOnNull, expectError: true},
		{name: "case insensitive", input: "NEVER_NULL", expected: sqlmodel.FunctionNullableNeverNull},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, parseError := parseNullableBehaviour(testCase.input)
			assert.Equal(t, testCase.expected, result)
			if testCase.expectError {
				assert.Error(t, parseError)
			} else {
				assert.NoError(t, parseError)
			}
		})
	}
}

func TestParseDataAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		expected    sqlmodel.FunctionDataAccess
		expectError bool
	}{
		{name: "empty is read-only", input: "", expected: sqlmodel.DataAccessReadOnly},
		{name: "read_only", input: "read_only", expected: sqlmodel.DataAccessReadOnly},
		{name: "modifies_data", input: "MODIFIES_DATA", expected: sqlmodel.DataAccessModifiesData},
		{name: "unknown value", input: "volatile", expected: sqlmodel.DataAccessReadOnly, expectError: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, parseError := parseDataAccess(testCase.input)
			assert.Equal(t, testCase.expected, result)
			if testCase.expectError {
				assert.Error(t, parseError)
			} else {
				assert.NoError(t, parseError)
			}
		})
	}
}

func TestMergeCustomFunctionsReadsANameAsSQLWrittenWithoutQuotes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		identifiers sqlmodel.IdentifierCase
		declared    string
		wantKey     string
		wantName    string
	}{
		{name: "a dialect folding unquoted names to lower case files it lower-cased",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false},
			declared:    "MyFunc", wantKey: "myfunc", wantName: "myfunc"},
		{name: "a case-sensitive dialect keeping names as written files it as written",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: false},
			declared:    "MyFunc", wantKey: "MyFunc", wantName: "MyFunc"},
		{name: "a case-insensitive dialect files it under its lower-case key",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
			declared:    "vec_distance_L2", wantKey: "vec_distance_l2", wantName: "vec_distance_L2"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Identifiers = testCase.identifiers

			MergeCustomFunctions(catalogue, &fakedialect.Engine{}, []sqlmodel.CustomFunctionConfig{{Name: testCase.declared, ReturnType: "text"}})

			functions := catalogue.Schemas["public"].Functions
			require.Len(t, functions, 1)
			require.Len(t, functions[testCase.wantKey], 1)
			assert.Equal(t, testCase.wantName, functions[testCase.wantKey][0].Name)
		})
	}
}
