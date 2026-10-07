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
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestArgsBuildsWellFormedArguments(t *testing.T) {
	t.Parallel()

	builder := &FunctionCatalogueBuilder{catalogueBuilder: dialectkit.NewCatalogueBuilder()}
	textType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}
	integerType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "integer"}

	arguments := builder.Args(Arg{Name: "str", Type: textType}, Arg{Name: "start", Type: integerType})
	require.Len(t, arguments, 2)
	assert.Equal(t, "str", arguments[0].Name)
	assert.Equal(t, textType, arguments[0].Type)
	assert.Equal(t, "start", arguments[1].Name)
	assert.Equal(t, integerType, arguments[1].Type)
}

func TestBuildFunctionCatalogueRegistersDualArityOverloads(t *testing.T) {
	t.Parallel()

	catalogue := builtinCatalogue(t)

	wantArities := map[string][]int{
		"substr":    {2, 3},
		"substring": {2, 3},
		"round":     {1, 2},
		"trim":      {1, 2},
		"ltrim":     {1, 2},
		"rtrim":     {1, 2},
	}

	for name, arities := range wantArities {
		overloads, found := catalogue.Functions[name]
		require.True(t, found, "function %q is not registered", name)
		registered := make(map[int]bool, len(overloads))
		for _, signature := range overloads {
			registered[len(signature.Arguments)] = true
		}
		for _, arity := range arities {
			assert.Truef(t, registered[arity],
				"function %q is missing the %d-argument overload", name, arity)
		}
	}
}

func TestBuildFunctionCatalogueOverloadsAreConsistent(t *testing.T) {
	t.Parallel()

	catalogue := builtinCatalogue(t)
	require.NotEmpty(t, catalogue.Functions)

	for name, overloads := range catalogue.Functions {
		require.NotEmpty(t, overloads, "function %q registered with no overloads", name)
		for index, signature := range overloads {
			require.NotNil(t, signature, "function %q overload %d is nil", name, index)
			assert.Equal(t, name, signature.Name, "function %q overload %d carries the wrong name", name, index)
			if signature.MinArguments > 0 {
				assert.LessOrEqual(t, signature.MinArguments, len(signature.Arguments),
					"function %q overload %d declares MinArguments beyond its argument count", name, index)
			}
			for argumentIndex, argument := range signature.Arguments {
				assert.NotEmpty(t, argument.Name,
					"function %q overload %d argument %d has an empty name", name, index, argumentIndex)
			}
		}
	}
}

func TestBuildFunctionCatalogueRegistersTheJSONFunctions(t *testing.T) {
	t.Parallel()

	catalogue := builtinCatalogue(t)

	for _, name := range []string{
		"json", "jsonb", "json_array", "jsonb_array", "json_array_length", "json_error_position",
		"json_extract", "jsonb_extract", "json_insert", "jsonb_insert", "json_object", "jsonb_object",
		"json_patch", "jsonb_patch", "json_pretty", "json_quote", "json_remove", "jsonb_remove",
		"json_replace", "jsonb_replace", "json_set", "jsonb_set", "json_type", "json_valid",
		"json_group_array", "jsonb_group_array", "json_group_object", "jsonb_group_object",
	} {
		overloads, found := catalogue.Functions[name]
		require.Truef(t, found, "JSON function %q is not registered", name)
		for _, signature := range overloads {
			assert.Equalf(t, sqlmodel.DataAccessReadOnly, signature.DataAccess,
				"JSON function %q must be read-only so its queries go to the reader", name)
		}
	}
}

func TestBuildFunctionCatalogueTypesFunctionResults(t *testing.T) {
	t.Parallel()

	catalogue := builtinCatalogue(t)

	testCases := []struct {
		name       string
		function   string
		engineName string
		arity      int
	}{
		{name: "unixepoch is a 64-bit count of seconds", function: "unixepoch", arity: 0, engineName: "bigint"},
		{name: "unixepoch with a time value", function: "unixepoch", arity: 2, engineName: "bigint"},
		{name: "json_array_length counts elements", function: "json_array_length", arity: 1, engineName: "integer"},
		{name: "jsonb returns a blob", function: "jsonb", arity: 1, engineName: "blob"},
		{name: "printf takes its values", function: "printf", arity: 3, engineName: "text"},
		{name: "char takes its code points", function: "char", arity: 2, engineName: "text"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var matched *sqlmodel.Function
			for _, signature := range catalogue.Functions[testCase.function] {
				if acceptsArity(signature, testCase.arity) {
					matched = signature
					break
				}
			}
			require.NotNilf(t, matched, "no overload of %q accepts %d arguments", testCase.function, testCase.arity)
			assert.Equal(t, testCase.engineName, matched.ReturnType.EngineName)
		})
	}
}

func TestWithExtraFunctionsRegistersAfterTheBuiltIns(t *testing.T) {
	t.Parallel()

	engine := New(
		WithExtraTypes(map[string]sqlmodel.SQLType{"Vector": sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "vector")}),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			builder.NullOnNull("vec_length", builder.Args(Arg{Name: "v", Type: builder.Type("vector")}), builder.Integer())
		}),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
			builder.NeverNull("app_version", nil, builder.Text())
		}),
	)

	functions := engine.BuiltinFunctions().Functions
	require.Len(t, functions["vec_length"], 1)
	assert.Equal(t, "vector", functions["vec_length"][0].Arguments[0].Type.EngineName)
	require.Len(t, functions["app_version"], 1)
	assert.NotEmpty(t, functions["lower"], "the built-in functions stay registered")
}

func acceptsArity(signature *sqlmodel.Function, arity int) bool {
	if signature.IsVariadic {
		return arity >= signature.MinArguments
	}
	minimum := signature.MinArguments
	if minimum == 0 {
		minimum = len(signature.Arguments)
	}
	return arity >= minimum && arity <= len(signature.Arguments)
}

func TestBuiltinFunctionSpecDecodes(t *testing.T) {
	t.Parallel()

	_, err := builtinFunctionSpec()

	require.NoError(t, err)
}

func TestBuiltinFunctionSpecNamesOnlyKnownTypes(t *testing.T) {
	t.Parallel()

	spec, err := builtinFunctionSpec()
	require.NoError(t, err)

	var unknown []string
	spec.Register(dialectkit.FunctionSpecHooks{
		ResolveType: func(spelling string) sqlmodel.SQLType {
			if _, known := builtinTypeMap[strings.ToLower(spelling)]; !known && spelling != anyTypeSpelling {
				unknown = append(unknown, spelling)
			}
			return sqlmodel.SQLType{}
		},
		DefaultNullability: nil,
		Add: func(_ string, signature *sqlmodel.Function) *sqlmodel.Function {
			return signature
		},
	})

	require.Empty(t, unknown)
}

func TestBuiltinFunctionCatalogueKeepsEverySignature(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	signatures := 0
	for _, overloads := range functions {
		signatures += len(overloads)
	}

	require.Len(t, functions, 132)
	require.Equal(t, 150, signatures)
}

func TestBuiltinFunctionsIgnoreExtraTypes(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraTypes(map[string]sqlmodel.SQLType{
		"text": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "custom_text"),
	}))

	assert.Equal(t, "text", engine.BuiltinFunctions().Functions["upper"][0].ReturnType.EngineName)
}

func TestJSONFunctionsReadADocumentHeldAsABlob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []sqlmodel.SQLTypeCategory
	}{
		{name: "json", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary}},
		{name: "jsonb", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary}},
		{name: "json_pretty", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary}},
		{name: "json_error_position", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary}},
		{name: "json_array_length", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText}},
		{name: "json_type", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText}},
		{name: "json_valid", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary}},
		{name: "json_extract", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText}},
		{name: "jsonb_extract", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText}},
		{name: "json_set", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryInteger}},
		{name: "jsonb_set", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryInteger}},
		{name: "json_insert", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryText}},
		{name: "jsonb_insert", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryText}},
		{name: "json_replace", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryFloat}},
		{name: "jsonb_replace", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryFloat}},
		{name: "json_remove", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText}},
		{name: "jsonb_remove", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText}},
		{name: "json_patch", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryText}},
		{name: "json_patch", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryBinary}},
		{name: "jsonb_patch", arguments: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryBinary}},
	}
	engine := New()
	functions := engine.BuiltinFunctions().Functions
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Truef(t, slices.ContainsFunc(functions[test.name], func(signature *sqlmodel.Function) bool {
				return acceptsCategories(engine, signature, test.arguments)
			}), "%s takes no %v", test.name, test.arguments)
		})
	}
}

func acceptsCategories(engine *Dialect, signature *sqlmodel.Function, categories []sqlmodel.SQLTypeCategory) bool {
	if !acceptsArity(signature, len(categories)) {
		return false
	}
	for index, category := range categories {
		parameter := signature.Arguments[min(index, len(signature.Arguments)-1)].Type
		argument := sqlmodel.NewSQLType(category, "")
		_, coerced := engine.CoerceArgument(dialect.CallArgument{Type: argument, Literal: false}, parameter)
		if parameter.Category != sqlmodel.TypeCategoryUnknown && parameter.Category != category &&
			!engine.CanImplicitCast(argument, parameter) && !coerced {
			return false
		}
	}
	return true
}

func TestJSONFunctionsTypeAParameterDocumentAsText(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	for _, name := range []string{"json", "jsonb", "json_extract", "json_set", "json_patch", "json_valid"} {
		for _, overload := range functions[name] {
			require.Equal(t, sqlmodel.TypeCategoryText, overload.Arguments[0].Type.Category, name)
		}
	}
}

func builtinCatalogue(t *testing.T) *ast.FunctionCatalogue {
	t.Helper()

	catalogue, err := buildFunctionCatalogue(builtinFunctionSpec, nil, buildTypeCatalogue(nil))
	require.NoError(t, err)
	return catalogue
}
