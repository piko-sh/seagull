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

package duckdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

const walkedItems = 3000

var (
	errHookFault   = errors.New("hook fault")
	errWalkStopped = errors.New("walk stopped by the caller")
)

type panickingErrContext struct {
	context.Context
}

type foreignStatement struct{}

func (panickingErrContext) Err() error {
	panic(errHookFault)
}

func (foreignStatement) IsParsedStatement() {}

func TestNewDuckDBEngine(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, "duckdb", engine.Info().Name, "default dialect should be duckdb")
	assert.Equal(t, []string{"main"}, engine.Info().SchemaSearchPath, "DuckDB default schema should be main")
	assert.Equal(t, dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}, engine.Info().Placeholders.Execution, "DuckDB uses dollar-sign parameters")
	assert.NotZero(t, engine.Info().Returning, "DuckDB supports RETURNING clauses")
	assert.NotNil(t, engine.BuiltinFunctions(), "function catalogue should be initialised")
	assert.NotNil(t, engine.BuiltinTypes(), "type catalogue should be initialised")
}

func TestNormaliseTypeName(t *testing.T) {
	t.Parallel()

	engine := New()

	tests := []struct {
		name           string
		input          string
		wantEngineName string
		wantCategory   sqlmodel.SQLTypeCategory
	}{

		{
			name:           "int normalises to int4",
			input:          "int",
			wantEngineName: "int4",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "integer normalises to int4",
			input:          "integer",
			wantEngineName: "int4",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "bigint normalises to int8",
			input:          "bigint",
			wantEngineName: "int8",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "smallint normalises to int2",
			input:          "smallint",
			wantEngineName: "int2",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "tinyint normalises to int1",
			input:          "tinyint",
			wantEngineName: "int1",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},

		{
			name:           "hugeint normalises to hugeint",
			input:          "hugeint",
			wantEngineName: "hugeint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "uinteger normalises to uinteger",
			input:          "uinteger",
			wantEngineName: "uinteger",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "ubigint normalises to ubigint",
			input:          "ubigint",
			wantEngineName: "ubigint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "utinyint normalises to utinyint",
			input:          "utinyint",
			wantEngineName: "utinyint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},
		{
			name:           "uhugeint normalises to uhugeint",
			input:          "uhugeint",
			wantEngineName: "uhugeint",
			wantCategory:   sqlmodel.TypeCategoryInteger,
		},

		{
			name:           "real normalises to float4",
			input:          "real",
			wantEngineName: "float4",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "double normalises to float8",
			input:          "double",
			wantEngineName: "float8",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},
		{
			name:           "float normalises to float4",
			input:          "float",
			wantEngineName: "float4",
			wantCategory:   sqlmodel.TypeCategoryFloat,
		},

		{
			name:           "varchar normalises to varchar",
			input:          "varchar",
			wantEngineName: "varchar",
			wantCategory:   sqlmodel.TypeCategoryText,
		},
		{
			name:           "text normalises to varchar",
			input:          "text",
			wantEngineName: "varchar",
			wantCategory:   sqlmodel.TypeCategoryText,
		},

		{
			name:           "boolean normalises to bool",
			input:          "boolean",
			wantEngineName: "bool",
			wantCategory:   sqlmodel.TypeCategoryBoolean,
		},
		{
			name:           "bool normalises to bool",
			input:          "bool",
			wantEngineName: "bool",
			wantCategory:   sqlmodel.TypeCategoryBoolean,
		},

		{
			name:           "numeric normalises to numeric",
			input:          "numeric",
			wantEngineName: "numeric",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
		},
		{
			name:           "decimal normalises to numeric",
			input:          "decimal",
			wantEngineName: "numeric",
			wantCategory:   sqlmodel.TypeCategoryDecimal,
		},

		{
			name:           "timestamp normalises to timestamp",
			input:          "timestamp",
			wantEngineName: "timestamp",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
		},
		{
			name:           "date normalises to date",
			input:          "date",
			wantEngineName: "date",
			wantCategory:   sqlmodel.TypeCategoryTemporal,
		},

		{
			name:           "json normalises to json",
			input:          "json",
			wantEngineName: "json",
			wantCategory:   sqlmodel.TypeCategoryJSON,
		},

		{
			name:           "uuid normalises to uuid",
			input:          "uuid",
			wantEngineName: "uuid",
			wantCategory:   sqlmodel.TypeCategoryUUID,
		},

		{
			name:           "case insensitive normalisation",
			input:          "VARCHAR",
			wantEngineName: "varchar",
			wantCategory:   sqlmodel.TypeCategoryText,
		},

		{
			name:           "unknown type falls back to unknown category",
			input:          "nonexistent_type",
			wantEngineName: "nonexistent_type",
			wantCategory:   sqlmodel.TypeCategoryUnknown,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := engine.NormaliseTypeName(testCase.input)

			assert.Equal(t, testCase.wantEngineName, result.EngineName, "engine name mismatch")
			assert.Equal(t, testCase.wantCategory, result.Category, "category mismatch")
		})
	}
}

func TestPromoteType(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("integer promotion selects the wider type", func(t *testing.T) {
		t.Parallel()

		int1 := sqlmodel.NewIntegerType("int1", 8, false)
		integer8 := sqlmodel.NewIntegerType("int8", 64, false)

		result := engine.PromoteType(int1, integer8)
		assert.Equal(t, "int8", result.EngineName, "int8 should win over int1")

		result = engine.PromoteType(integer8, int1)
		assert.Equal(t, "int8", result.EngineName, "int8 should still win when on the left")
	})

	t.Run("float promotion selects the wider type", func(t *testing.T) {
		t.Parallel()

		float4 := sqlmodel.NewFloatType("float4", 32)
		float8 := sqlmodel.NewFloatType("float8", 64)

		result := engine.PromoteType(float4, float8)
		assert.Equal(t, "float8", result.EngineName, "float8 should win over float4")
	})

	t.Run("hugeint promotion over int4", func(t *testing.T) {
		t.Parallel()

		int4 := sqlmodel.NewIntegerType("int4", 32, false)
		hugeint := sqlmodel.NewIntegerType("hugeint", 128, false)

		result := engine.PromoteType(int4, hugeint)
		assert.Equal(t, "hugeint", result.EngineName, "hugeint should win over int4")
	})

	t.Run("same type returns identity", func(t *testing.T) {
		t.Parallel()

		int4 := sqlmodel.NewIntegerType("int4", 32, false)

		result := engine.PromoteType(int4, int4)
		assert.Equal(t, "int4", result.EngineName, "same type should return itself")
	})
}

func TestCanImplicitCast(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("integer to float is allowed", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "")))
	})

	t.Run("integer to decimal is allowed", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "")))
	})

	t.Run("float to integer is not allowed", func(t *testing.T) {
		t.Parallel()

		assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
	})

	t.Run("text to text is allowed", func(t *testing.T) {
		t.Parallel()

		assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "")))
	})

	t.Run("text to integer is not allowed", func(t *testing.T) {
		t.Parallel()

		assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")))
	})

	t.Run("decimal to float is allowed", func(t *testing.T) {
		t.Parallel()

		require.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "decimal"), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "double")))
	})

	t.Run("text to json is allowed", func(t *testing.T) {
		t.Parallel()

		require.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json")))
	})

	t.Run("json to text is not allowed", func(t *testing.T) {
		t.Parallel()

		require.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"), sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")))
	})
}

func TestCoerceArgumentPassesJSONToTextParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		argument  sqlmodel.SQLType
		parameter sqlmodel.SQLType
		coerced   bool
	}{
		{name: "a JSON document to a VARCHAR", argument: builtinTypeMap["json"], parameter: builtinTypeMap["varchar"], coerced: true},
		{name: "a VARCHAR to a DATE", argument: builtinTypeMap["varchar"], parameter: builtinTypeMap["date"]},
		{name: "an integer to a VARCHAR", argument: builtinTypeMap["integer"], parameter: builtinTypeMap["varchar"]},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			coercion, coerced := New().CoerceArgument(dialect.CallArgument{Type: testCase.argument, Literal: false}, testCase.parameter)

			require.Equal(t, testCase.coerced, coerced)
			require.False(t, coercion.MayBeNull)
		})
	}
}

func TestCoerceArgumentReadsAQuotedLiteralAsATemporalParameter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		argument  dialect.CallArgument
		parameter string
		coerced   bool
	}{
		{name: "a quoted literal to a DATE", argument: dialect.CallArgument{Type: builtinTypeMap["varchar"], Literal: true}, parameter: "date", coerced: true},
		{name: "a quoted literal to a TIMESTAMP", argument: dialect.CallArgument{Type: builtinTypeMap["varchar"], Literal: true}, parameter: "timestamp", coerced: true},
		{name: "a quoted literal to a TIMESTAMPTZ", argument: dialect.CallArgument{Type: builtinTypeMap["varchar"], Literal: true}, parameter: "timestamptz", coerced: true},
		{name: "a quoted literal to a TIME", argument: dialect.CallArgument{Type: builtinTypeMap["varchar"], Literal: true}, parameter: "time", coerced: true},
		{name: "a quoted literal to an INTERVAL", argument: dialect.CallArgument{Type: builtinTypeMap["varchar"], Literal: true}, parameter: "interval", coerced: true},
		{name: "a VARCHAR column to a DATE", argument: dialect.CallArgument{Type: builtinTypeMap["varchar"], Literal: false}, parameter: "date", coerced: false},
		{name: "a quoted literal to an INTEGER", argument: dialect.CallArgument{Type: builtinTypeMap["varchar"], Literal: true}, parameter: "integer", coerced: false},
		{name: "a number literal to a DATE", argument: dialect.CallArgument{Type: builtinTypeMap["integer"], Literal: true}, parameter: "date", coerced: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			coercion, coerced := New().CoerceArgument(testCase.argument, builtinTypeMap[testCase.parameter])

			require.Equal(t, testCase.coerced, coerced)
			require.False(t, coercion.MayBeNull)
		})
	}
}

func TestSupportedExpressions(t *testing.T) {
	t.Parallel()

	engine := New()
	features := engine.Info().Expressions

	assert.NotZero(t, features&ast.SQLFeatureLambda,
		"DuckDB should support lambda expressions")

	assert.NotZero(t, features&ast.SQLFeatureStructFieldAccess,
		"DuckDB should support struct field access")

	assert.NotZero(t, features&ast.SQLFeatureStringConcat,
		"DuckDB should support string concatenation")

	assert.NotZero(t, features&ast.SQLFeatureWindowFunction,
		"DuckDB should support window functions")

	assert.NotZero(t, features&ast.SQLFeatureJSONOp,
		"DuckDB should support JSON operators")
}

func TestDefaultSchema(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, []string{"main"}, engine.Info().SchemaSearchPath,
		"DuckDB default schema should be main")
}

func TestBuiltinFunctions(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := engine.BuiltinFunctions()

	require.NotNil(t, catalogue, "function catalogue must not be nil")
	require.NotNil(t, catalogue.Functions, "function map must not be nil")

	expectedFunctions := []string{"concat", "count", "abs"}
	for _, name := range expectedFunctions {
		signatures, exists := catalogue.Functions[name]
		assert.True(t, exists, "expected built-in function %q to be registered", name)
		assert.NotEmpty(t, signatures, "expected at least one signature for %q", name)
	}
}

func TestBuiltinTypes(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := engine.BuiltinTypes()

	require.NotNil(t, catalogue, "type catalogue must not be nil")
	require.NotNil(t, catalogue.Types, "type map must not be nil")

	expectedTypes := []string{"int", "bigint", "varchar", "boolean", "json", "hugeint", "uinteger", "uuid", "timestamp"}
	for _, name := range expectedTypes {
		_, exists := catalogue.Types[name]
		assert.True(t, exists, "expected built-in type %q to be registered", name)
	}
}

func TestIsParsedStatement(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT 1")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	parsed, ok := statements[0].Raw.(*parsedStatement)
	require.True(t, ok, "raw should be a *parsedStatement")
	parsed.IsParsedStatement()
}

func TestWithName(t *testing.T) {
	t.Parallel()

	info := New(WithName("motherduck")).Info()

	assert.Equal(t, "motherduck", info.Name)
	assert.Equal(t, Family, info.Family)
}

func TestWithExtraTypes(t *testing.T) {
	t.Parallel()

	customTypes := map[string]sqlmodel.SQLType{
		"custom_geo": {Category: sqlmodel.TypeCategoryUnknown, EngineName: "custom_geo"},
	}
	engine := New(WithExtraTypes(customTypes))

	catalogue := engine.BuiltinTypes()
	_, exists := catalogue.Types["custom_geo"]
	assert.True(t, exists, "custom type should be merged into catalogue")
}

func TestWithExtraFunctions(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraFunctions(func(builder *FunctionCatalogueBuilder) {
		builder.NullOnNull("my_custom_fn", nil, sqlmodel.NewIntegerType("int4", 32, false))
	}))

	catalogue := engine.BuiltinFunctions()
	_, exists := catalogue.Functions["my_custom_fn"]
	assert.True(t, exists, "custom function should be registered")
}

func TestWithTypeNormaliserHook(t *testing.T) {
	t.Parallel()

	hook := func(name string, _ []int) *sqlmodel.SQLType {
		if name == "my_special_type" {
			return &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "my_special_type"}
		}
		return nil
	}
	engine := New(WithTypeNormaliserHook(hook))

	result := engine.NormaliseTypeName("my_special_type")
	assert.Equal(t, "my_special_type", result.EngineName)
	assert.Equal(t, sqlmodel.TypeCategoryText, result.Category)

	standard := engine.NormaliseTypeName("integer")
	assert.Equal(t, "int4", standard.EngineName)
}

func TestWithImplicitCastHook(t *testing.T) {
	t.Parallel()

	hook := func(from, to sqlmodel.SQLType) *bool {

		if from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryInteger {
			return new(true)
		}
		return nil
	}
	engine := New(WithImplicitCastHook(hook))

	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")),
		"hook should allow text to integer cast")

	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "")))
}

func TestWithPromoteTypeHook(t *testing.T) {
	t.Parallel()

	hook := func(left, right sqlmodel.SQLType) *sqlmodel.SQLType {
		if left.EngineName == "int4" && right.EngineName == "int4" {
			return new(sqlmodel.NewIntegerType("int8", 64, false))
		}
		return nil
	}
	engine := New(WithPromoteTypeHook(hook))

	int4 := sqlmodel.NewIntegerType("int4", 32, false)
	result := engine.PromoteType(int4, int4)
	assert.Equal(t, "int8", result.EngineName, "hook should promote int4+int4 to int8")
}

func TestTableValuedFunctionColumnsFromCatalogue(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("resolves composite type columns", func(t *testing.T) {
		t.Parallel()

		catalogue := &sqlmodel.Catalogue{
			DefaultSchema: "main",
			Schemas: map[string]*sqlmodel.Schema{
				"main": {
					Name: "main",
					Functions: map[string][]*sqlmodel.Function{
						"get_records": {
							{
								Name:       "get_records",
								ReturnsSet: true,
								ReturnType: sqlmodel.SQLType{
									Category:   sqlmodel.TypeCategoryUnknown,
									EngineName: "point",
								},
							},
						},
					},
					CompositeTypes: map[string]*sqlmodel.CompositeType{
						"point": {
							Name: "point",
							Fields: []sqlmodel.Column{
								{Name: "x", SQLType: sqlmodel.NewFloatType("float8", 64)},
								{Name: "y", SQLType: sqlmodel.NewFloatType("float8", 64)},
							},
						},
					},
					Tables:    map[string]*sqlmodel.Table{},
					Views:     map[string]*sqlmodel.View{},
					Enums:     map[string]*sqlmodel.Enum{},
					Sequences: map[string]*sqlmodel.Sequence{},
				},
			},
		}

		columns := catalogueFunctionColumns(t, engine, catalogue, "get_records")
		require.Len(t, columns, 2)
		assert.Equal(t, "x", columns[0].Name)
		assert.Equal(t, "y", columns[1].Name)
	})

	t.Run("returns nil for unknown function", func(t *testing.T) {
		t.Parallel()

		catalogue := &sqlmodel.Catalogue{
			DefaultSchema: "main",
			Schemas: map[string]*sqlmodel.Schema{
				"main": {
					Name:           "main",
					Functions:      map[string][]*sqlmodel.Function{},
					CompositeTypes: map[string]*sqlmodel.CompositeType{},
					Tables:         map[string]*sqlmodel.Table{},
					Views:          map[string]*sqlmodel.View{},
					Enums:          map[string]*sqlmodel.Enum{},
					Sequences:      map[string]*sqlmodel.Sequence{},
				},
			},
		}

		columns := catalogueFunctionColumns(t, engine, catalogue, "nonexistent")
		assert.Nil(t, columns)
	})

	t.Run("returns nil for non-set-returning function", func(t *testing.T) {
		t.Parallel()

		catalogue := &sqlmodel.Catalogue{
			DefaultSchema: "main",
			Schemas: map[string]*sqlmodel.Schema{
				"main": {
					Name: "main",
					Functions: map[string][]*sqlmodel.Function{
						"scalar_fn": {
							{Name: "scalar_fn", ReturnsSet: false},
						},
					},
					CompositeTypes: map[string]*sqlmodel.CompositeType{},
					Tables:         map[string]*sqlmodel.Table{},
					Views:          map[string]*sqlmodel.View{},
					Enums:          map[string]*sqlmodel.Enum{},
					Sequences:      map[string]*sqlmodel.Sequence{},
				},
			},
		}

		columns := catalogueFunctionColumns(t, engine, catalogue, "scalar_fn")
		assert.Nil(t, columns)
	})
}

func TestResolveFunctionCall(t *testing.T) {
	t.Parallel()

	engine := New()
	catalogue := &sqlmodel.Catalogue{}

	t.Run("array_agg returns array type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "array_agg", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
		assert.True(t, result.IsAggregate)
	})

	t.Run("unnest with single array argument", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "unnest", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryArray, EngineName: "int4[]", ElementType: new(sqlmodel.NewIntegerType("int4", 32, false))}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, result.ReturnType.Category)
		assert.True(t, result.ReturnsSet)
	})

	t.Run("unnest with multiple arguments returns record", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "unnest", "",
			[]sqlmodel.SQLType{
				{Category: sqlmodel.TypeCategoryArray, EngineName: "int4[]"},
				{Category: sqlmodel.TypeCategoryArray, EngineName: "varchar[]"},
			})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "record", result.ReturnType.EngineName)
		assert.True(t, result.ReturnsSet)
	})

	t.Run("unnest with non-array argument", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "unnest", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.ReturnsSet)
	})

	t.Run("sum with small integer returns hugeint", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "sum", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "hugeint", result.ReturnType.EngineName)
	})

	t.Run("sum with large integer returns hugeint", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "sum", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "hugeint", result.ReturnType.EngineName)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, result.ReturnType.Category)
	})

	t.Run("sum with hugeint returns hugeint", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "sum", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryInteger, EngineName: "hugeint"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "hugeint", result.ReturnType.EngineName)
	})

	t.Run("sum with float returns float8", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "sum", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryFloat, EngineName: "float4"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "float8", result.ReturnType.EngineName)
	})

	t.Run("sum with decimal returns numeric", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "sum", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryDecimal, EngineName: "numeric"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "numeric", result.ReturnType.EngineName)
	})

	t.Run("avg with float returns float8", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "avg", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryFloat, EngineName: "float4"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "float8", result.ReturnType.EngineName)
	})

	t.Run("avg with integer returns float8", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "avg", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "float8", result.ReturnType.EngineName)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, result.ReturnType.Category)
	})

	t.Run("avg with decimal returns float8", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "avg", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryDecimal, EngineName: "numeric"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "float8", result.ReturnType.EngineName)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, result.ReturnType.Category)
	})

	t.Run("min returns same type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "min", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "int4", result.ReturnType.EngineName)
	})

	t.Run("max returns same type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "max", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "varchar", result.ReturnType.EngineName)
	})

	t.Run("coalesce returns first known type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "coalesce", "",
			[]sqlmodel.SQLType{
				{Category: sqlmodel.TypeCategoryUnknown},
				{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"},
			})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "varchar", result.ReturnType.EngineName)
	})

	t.Run("coalesce with all unknown types", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "coalesce", "",
			[]sqlmodel.SQLType{
				{Category: sqlmodel.TypeCategoryUnknown},
			})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, result.ReturnType.Category)
	})

	t.Run("typeof returns varchar", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "typeof", "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "varchar", result.ReturnType.EngineName)
		require.Equal(t, sqlmodel.FunctionNullableNeverNull, result.NullableBehaviour)
	})

	t.Run("struct_pack returns struct", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "struct_pack", "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryStruct, result.ReturnType.Category)
	})

	t.Run("struct_extract returns unknown", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "struct_extract", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryStruct, EngineName: "struct"}})
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("struct_insert returns input type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "struct_insert", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryStruct, EngineName: "my_struct"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "my_struct", result.ReturnType.EngineName)
	})

	t.Run("struct_insert with no arguments", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "struct_insert", "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryStruct, result.ReturnType.Category)
	})

	t.Run("map returns map type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "map", "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryMap, result.ReturnType.Category)
	})

	t.Run("map_keys returns array of key type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "map_keys", "", []sqlmodel.SQLType{{
			Category:   sqlmodel.TypeCategoryMap,
			EngineName: "map",
			KeyType:    new(sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}),
		}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
	})

	t.Run("map_keys with no arguments", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "map_keys", "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
	})

	t.Run("map_values returns array of value type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "map_values", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryMap, EngineName: "map", ElementType: new(sqlmodel.NewIntegerType("int4", 32, false))}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
	})

	t.Run("map_values with no arguments", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "map_values", "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
	})

	t.Run("map_entries returns array", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "map_entries", "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
	})

	t.Run("element_at with map type returns a list of the value type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "element_at", "", []sqlmodel.SQLType{{
			Category:    sqlmodel.TypeCategoryMap,
			EngineName:  "map",
			ElementType: new(sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}),
		}})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
		require.NotNil(t, result.ReturnType.ElementType)
		assert.Equal(t, "varchar", result.ReturnType.ElementType.EngineName)
	})

	t.Run("map_extract_value returns the value type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "map_extract_value", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryMap, EngineName: "map", ElementType: new(sqlmodel.NewIntegerType("int4", 32, false))}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "int4", result.ReturnType.EngineName)
		assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, result.NullableBehaviour)
	})

	t.Run("list_extract returns the element type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "list_extract", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryArray, EngineName: "int4[]", ElementType: new(sqlmodel.NewIntegerType("int4", 32, false))}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "int4", result.ReturnType.EngineName)
	})

	t.Run("element_at with no arguments returns a list", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "element_at", "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
	})

	t.Run("list_value is a list of its first typed argument", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "list_value", "",
			[]sqlmodel.SQLType{sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), sqlmodel.NewIntegerType("int4", 32, false)})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.ReturnType.ElementType)
		assert.Equal(t, "int4", result.ReturnType.ElementType.EngineName)
	})

	t.Run("list_transform returns input type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "list_transform", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryArray, EngineName: "int4[]"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
	})

	t.Run("list_filter with no arguments", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "list_filter", "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.ReturnType.Category)
	})

	t.Run("array_append returns array passthrough", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "array_append", "",
			[]sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryArray, EngineName: "int4[]"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "int4[]", result.ReturnType.EngineName)
	})

	t.Run("array_prepend returns second argument type", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "array_prepend", "",
			[]sqlmodel.SQLType{
				{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"},
				{Category: sqlmodel.TypeCategoryArray, EngineName: "int4[]"},
			})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "int4[]", result.ReturnType.EngineName)
	})

	t.Run("unknown function returns nil", func(t *testing.T) {
		t.Parallel()

		result, err := engine.ResolveFunctionCall(t.Context(), catalogue, "unknown_function", "", nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("empty arguments return nil for aggregate functions", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"array_agg", "unnest", "min", "sum", "avg"} {
			result, err := engine.ResolveFunctionCall(t.Context(), catalogue, name, "", nil)
			require.NoError(t, err)
			assert.Nil(t, result, "expected nil for %s with no arguments", name)
		}
	})
}

func TestNormaliseTypeName_ArrayType(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("varchar array normalises to array category", func(t *testing.T) {
		t.Parallel()

		result := engine.NormaliseTypeName("varchar[]")
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.Category)
		require.NotNil(t, result.ElementType)
		assert.Equal(t, "varchar", result.ElementType.EngineName)
	})

	t.Run("multi-dimensional array normalises element type", func(t *testing.T) {
		t.Parallel()

		result := engine.NormaliseTypeName("int4[][]")
		assert.Equal(t, sqlmodel.TypeCategoryArray, result.Category)
	})

	t.Run("empty type name normalises to varchar", func(t *testing.T) {
		t.Parallel()

		result := engine.NormaliseTypeName("")
		assert.Equal(t, "varchar", result.EngineName)
		assert.Equal(t, sqlmodel.TypeCategoryText, result.Category)
	})
}

func TestNormaliseTypeName_Modifiers(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("numeric with precision and scale", func(t *testing.T) {
		t.Parallel()

		result := engine.NormaliseTypeName("numeric", 10, 2)
		assert.Equal(t, sqlmodel.TypeCategoryDecimal, result.Category)
		require.NotNil(t, result.Precision)
		assert.Equal(t, 10, *result.Precision)
		require.NotNil(t, result.Scale)
		assert.Equal(t, 2, *result.Scale)
	})

	t.Run("varchar with length modifier", func(t *testing.T) {
		t.Parallel()

		result := engine.NormaliseTypeName("varchar", 255)
		assert.Equal(t, sqlmodel.TypeCategoryText, result.Category)
		require.NotNil(t, result.Length)
		assert.Equal(t, 255, *result.Length)
	})

	t.Run("timestamp with precision modifier", func(t *testing.T) {
		t.Parallel()

		result := engine.NormaliseTypeName("timestamp", 6)
		assert.Equal(t, sqlmodel.TypeCategoryTemporal, result.Category)
		require.NotNil(t, result.Precision)
		assert.Equal(t, 6, *result.Precision)
	})
}

func TestPromoteType_CrossCategory(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("different categories return left", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.NewIntegerType("int4", 32, false)
		right := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}
		result := engine.PromoteType(left, right)
		assert.Equal(t, "int4", result.EngineName, "cross-category should return left type")
	})

	t.Run("text category returns left", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}
		right := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}
		result := engine.PromoteType(left, right)
		assert.Equal(t, "varchar", result.EngineName)
	})

	t.Run("usmallint promoted over int1", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.NewIntegerType("int1", 8, false)
		right := sqlmodel.NewIntegerType("usmallint", 16, true)
		result := engine.PromoteType(left, right)
		assert.Equal(t, "usmallint", result.EngineName)
	})

	t.Run("ubigint promoted over int4", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.NewIntegerType("int4", 32, false)
		right := sqlmodel.NewIntegerType("ubigint", 64, true)
		result := engine.PromoteType(left, right)
		assert.Equal(t, "ubigint", result.EngineName)
	})

	t.Run("uhugeint promoted over hugeint", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.NewIntegerType("hugeint", 128, false)
		right := sqlmodel.NewIntegerType("uhugeint", 128, true)
		result := engine.PromoteType(left, right)
		assert.Equal(t, "uhugeint", result.EngineName)
	})

	t.Run("utinyint promoted over int1", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.NewIntegerType("int1", 8, false)
		right := sqlmodel.NewIntegerType("utinyint", 8, true)
		result := engine.PromoteType(left, right)
		assert.Equal(t, "utinyint", result.EngineName)
	})

	t.Run("uinteger promoted over int2", func(t *testing.T) {
		t.Parallel()

		left := sqlmodel.NewIntegerType("int2", 16, false)
		right := sqlmodel.NewIntegerType("uinteger", 32, true)
		result := engine.PromoteType(left, right)
		assert.Equal(t, "uinteger", result.EngineName)
	})
}

func TestCanImplicitCast_FloatToDecimal(t *testing.T) {
	t.Parallel()

	engine := New()
	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, ""), sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "")),
		"float to decimal should be allowed")
}

func panicOnType(typeName string) Option {
	return WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
		if name == typeName {
			panic(errHookFault)
		}
		return nil
	})
}

func cancelOnType(typeName string, cancel context.CancelCauseFunc) Option {
	return WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
		if name == typeName {
			cancel(errWalkStopped)
		}
		return nil
	})
}

func newLoggingContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	return logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(logs, nil))), logs
}

func loggedRecords(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any
	decoder := json.NewDecoder(logs)
	for decoder.More() {
		record := map[string]any{}
		require.NoError(t, decoder.Decode(&record))
		records = append(records, record)
	}
	return records
}

func parseOne(t *testing.T, engine *Dialect, sql string) ast.ParsedStatement {
	t.Helper()

	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	return statements[0]
}

func numberedList(format string, count int) string {
	items := make([]string, count)
	for index := range items {
		items[index] = fmt.Sprintf(format, index)
	}
	return strings.Join(items, ", ")
}

func tableMacroCatalogue(body string) *sqlmodel.Catalogue {
	catalogue := buildTestCatalogue()
	composite := sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, "rows_of_row")
	catalogue.Schemas["main"].CompositeTypes["rows_of_row"] = &sqlmodel.CompositeType{
		Name:   "rows_of_row",
		Schema: "main",
		Fields: []sqlmodel.Column{{Name: "a", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: true}},
	}
	macro := sqlmodel.NewFunction(nil, composite, sqlmodel.FunctionNullableCalledOnNull, sqlmodel.WithFunctionName("rows_of"))
	macro.ReturnsSet = true
	macro.Language = "sql"
	macro.BodySQL = body
	catalogue.Schemas["main"].Functions["rows_of"] = []*sqlmodel.Function{macro}
	return catalogue
}

func TestEntryPointsReturnAPanicAsAPanicErrorAndLogItOnce(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	unreadable := ast.ParsedStatement{Raw: (*parsedStatement)(nil), Location: 0, Length: 0}
	testCases := []struct {
		call      func(t *testing.T, ctx context.Context) (any, error)
		name      string
		operation string
	}{
		{
			name:      "parse statements with a context that panics",
			operation: "duckdb: parse statements",
			call: func(_ *testing.T, ctx context.Context) (any, error) {
				statements, err := New().ParseStatements(panickingErrContext{Context: ctx}, "SELECT 1")
				return statements, err
			},
		},
		{
			name:      "apply DDL with a type hook that panics",
			operation: "duckdb: apply DDL",
			call: func(t *testing.T, ctx context.Context) (any, error) {
				t.Helper()
				engine := New(panicOnType("hooked"))
				mutation, err := engine.ApplyDDL(ctx, parseOne(t, engine, "CREATE TABLE t (a hooked)"))
				return mutation, err
			},
		},
		{
			name:      "apply DDL with a statement it cannot read",
			operation: "duckdb: apply DDL",
			call: func(_ *testing.T, ctx context.Context) (any, error) {
				mutation, err := New().ApplyDDL(ctx, unreadable)
				return mutation, err
			},
		},
		{
			name:      "analyse query with a type hook that panics",
			operation: "duckdb: analyse query",
			call: func(t *testing.T, ctx context.Context) (any, error) {
				t.Helper()
				engine := New(panicOnType("hooked"))
				analysis, err := engine.AnalyseQuery(ctx, nil, parseOne(t, engine, "SELECT CAST($1 AS hooked)"))
				return analysis, err
			},
		},
		{
			name:      "analyse query with a statement it cannot read",
			operation: "duckdb: analyse query",
			call: func(_ *testing.T, ctx context.Context) (any, error) {
				analysis, err := New().AnalyseQuery(ctx, nil, unreadable)
				return analysis, err
			},
		},
		{
			name:      "resolve function call with a type hook that panics while typing a macro",
			operation: "duckdb: resolve function call",
			call: func(t *testing.T, ctx context.Context) (any, error) {
				t.Helper()
				catalogue := macroCatalogue(t, "CREATE MACRO convert(x) AS CAST(x AS hooked)")
				resolution, err := New(panicOnType("hooked")).ResolveFunctionCall(ctx, catalogue, "convert", "", []sqlmodel.SQLType{integer})
				return resolution, err
			},
		},
		{
			name:      "table-valued function columns with a type hook that panics while analysing a table macro",
			operation: "duckdb: table-valued function columns",
			call: func(_ *testing.T, ctx context.Context) (any, error) {
				catalogue := tableMacroCatalogue("SELECT CAST($1 AS hooked) AS a")
				columns, err := New(panicOnType("hooked")).TableValuedFunctionColumnsFromCatalogue(ctx, catalogue, "rows_of")
				return columns, err
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newLoggingContext(t)
			result, err := testCase.call(t, ctx)

			assert.Nil(t, result)
			require.ErrorIs(t, err, fault.ErrPanic)
			panicError, ok := errors.AsType[*dialectkit.PanicError](err)
			require.True(t, ok, "want a *dialectkit.PanicError, got %v", err)
			assert.Equal(t, testCase.operation, panicError.Operation)
			assert.NotContains(t, err.Error(), "goroutine")
			records := loggedRecords(t, logs)
			require.Len(t, records, 1)
			assert.Equal(t, slog.LevelWarn.String(), records[0][slog.LevelKey])
			assert.Equal(t, testCase.operation, records[0]["operation"])
			assert.Contains(t, records[0]["stack"], "goroutine")
		})
	}
}

func TestEntryPointsStopWithTheContextsErrorOnceItIsCancelled(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	additions := strings.Repeat(" + 1", walkedItems)
	testCases := []struct {
		wantError error
		call      func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error)
		name      string
	}{
		{
			name:      "parse statements cancelled before it starts",
			wantError: errWalkStopped,
			call: func(_ *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				cancel(errWalkStopped)
				statements, err := New().ParseStatements(ctx, "SELECT 1")
				return statements, err
			},
		},
		{
			name:      "apply DDL cancelled before it starts",
			wantError: errWalkStopped,
			call: func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				t.Helper()
				engine := New()
				statement := parseOne(t, engine, "CREATE TABLE t (a INTEGER)")
				cancel(errWalkStopped)
				mutation, err := engine.ApplyDDL(ctx, statement)
				return mutation, err
			},
		},
		{
			name:      "analyse query cancelled before it starts",
			wantError: errWalkStopped,
			call: func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				t.Helper()
				engine := New()
				statement := parseOne(t, engine, "SELECT 1")
				cancel(errWalkStopped)
				analysis, err := engine.AnalyseQuery(ctx, nil, statement)
				return analysis, err
			},
		},
		{
			name:      "apply DDL cancelled while it walks the statement",
			wantError: errWalkStopped,
			call: func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				t.Helper()
				engine := New(cancelOnType("hooked", cancel))
				statement := parseOne(t, engine, "CREATE TABLE t (a hooked, "+numberedList("c%d INTEGER", walkedItems)+")")
				mutation, err := engine.ApplyDDL(ctx, statement)
				return mutation, err
			},
		},
		{
			name:      "analyse query cancelled while it walks the statement",
			wantError: errWalkStopped,
			call: func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				t.Helper()
				engine := New(cancelOnType("hooked", cancel))
				statement := parseOne(t, engine, "SELECT $1::hooked, "+numberedList("%d", walkedItems))
				analysis, err := engine.AnalyseQuery(ctx, nil, statement)
				return analysis, err
			},
		},
		{
			name:      "resolve function call cancelled while it walks a macro body",
			wantError: errWalkStopped,
			call: func(t *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				t.Helper()
				catalogue := macroCatalogue(t, "CREATE MACRO grow(x) AS x"+additions)
				cancel(errWalkStopped)
				resolution, err := New().ResolveFunctionCall(ctx, catalogue, "grow", "", []sqlmodel.SQLType{integer})
				return resolution, err
			},
		},
		{
			name:      "table-valued function columns cancelled while it walks a table macro body",
			wantError: errWalkStopped,
			call: func(_ *testing.T, ctx context.Context, cancel context.CancelCauseFunc) (any, error) {
				catalogue := tableMacroCatalogue("SELECT 1" + additions + " AS a")
				cancel(errWalkStopped)
				columns, err := New().TableValuedFunctionColumnsFromCatalogue(ctx, catalogue, "rows_of")
				return columns, err
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			logged, logs := newLoggingContext(t)
			ctx, cancel := context.WithCancelCause(logged)
			defer cancel(nil)

			result, err := testCase.call(t, ctx, cancel)

			assert.Nil(t, result)
			require.ErrorIs(t, err, testCase.wantError)
			assert.NotErrorIs(t, err, fault.ErrPanic)
			assert.Empty(t, loggedRecords(t, logs))
		})
	}
}

func TestEntryPointsRefuseAStatementAnotherDialectParsed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		raw      ast.ParsedStatementRaw
		name     string
		wantText string
		isDDL    bool
	}{
		{name: "apply DDL to another dialect's statement", raw: foreignStatement{}, wantText: "duckdb.foreignStatement", isDDL: true},
		{name: "apply DDL to no statement", raw: nil, wantText: "<nil>", isDDL: true},
		{name: "analyse another dialect's statement", raw: foreignStatement{}, wantText: "duckdb.foreignStatement"},
		{name: "analyse no statement", raw: nil, wantText: "<nil>"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statement := ast.ParsedStatement{Raw: testCase.raw, Location: 0, Length: 0}
			var err error
			if testCase.isDDL {
				_, err = New().ApplyDDL(t.Context(), statement)
			} else {
				_, err = New().AnalyseQuery(t.Context(), nil, statement)
			}

			require.ErrorIs(t, err, dialect.ErrForeignStatement)
			assert.NotErrorIs(t, err, fault.ErrPanic)
			assert.Contains(t, err.Error(), testCase.wantText)
		})
	}
}

func readCurrentToken(ctx context.Context, statementParser *parser) (err error) {
	defer func() { dialectkit.Recover[any](ctx, "duckdb: read a token", recover(), nil, &err) }()
	statementParser.Current()
	return nil
}

func TestEveryParserOfAStatementReadsUnderTheInterruptTheEngineGives(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		walker func(statementParser *parser) *parser
		name   string
	}{
		{name: "the statement's parser", walker: func(statementParser *parser) *parser { return statementParser }},
		{name: "a parser of a nested construct", walker: func(statementParser *parser) *parser { return statementParser.newChildParser(statementParser.Tokens) }},
		{
			name: "a parser of a construct nested in a nested construct",
			walker: func(statementParser *parser) *parser {
				return statementParser.newChildParser(statementParser.Tokens).newChildParser(statementParser.Tokens)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			parsed, ok := parseOne(t, engine, "SELECT 1").Raw.(*parsedStatement)
			require.True(t, ok)
			reads := 0
			statementParser := engine.newStatementParser(func() error {
				reads++
				return errWalkStopped
			}, parsed)

			err := readCurrentToken(t.Context(), testCase.walker(statementParser))

			require.ErrorIs(t, err, errWalkStopped)
			assert.Equal(t, 1, reads)
		})
	}
}

func TestApplyDDLReturnsSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantError error
		name      string
		sql       string
		wantText  string
		options   []Option
	}{
		{name: "create table without a name", sql: "CREATE TABLE", wantText: "expected an identifier"},
		{name: "drop table without a name", sql: "DROP TABLE", wantText: "expected an identifier"},
		{name: "unbalanced column check", sql: "CREATE TABLE t (x INTEGER CHECK (x > 1", wantError: fault.ErrParse},
		{name: "unbalanced table check", sql: "CREATE TABLE t (x INTEGER, CHECK (x > 1", wantError: fault.ErrParse},
		{name: "unbalanced column default", sql: "CREATE TABLE t (x INTEGER DEFAULT (1", wantError: fault.ErrParse},
		{name: "unbalanced generated expression", sql: "CREATE TABLE t (x INTEGER GENERATED ALWAYS AS (x + 1", wantError: fault.ErrParse},
		{name: "unbalanced identity options", sql: "CREATE TABLE t (x INTEGER GENERATED BY DEFAULT AS IDENTITY (START 1", wantError: fault.ErrParse},
		{name: "unbalanced foreign key reference", sql: "CREATE TABLE t (x INTEGER REFERENCES other (id", wantError: fault.ErrParse},
		{name: "rename column without TO", sql: "ALTER TABLE t RENAME COLUMN a b", wantText: "expected keyword TO"},
		{name: "rename enum value without TO", sql: "ALTER TYPE mood RENAME VALUE 'a' 'b'", wantText: "expected keyword TO"},
		{name: "create index without ON", sql: "CREATE INDEX idx t (x)", wantText: "expected keyword ON"},
		{name: "unbalanced comment on function", sql: "COMMENT ON FUNCTION f((", wantError: fault.ErrParse},
		{name: "unbalanced drop macro arguments", sql: "DROP MACRO m((", wantError: fault.ErrParse},
		{name: "create schema without a name", sql: "CREATE SCHEMA", wantText: "expected an identifier"},
		{name: "create sequence without a name", sql: "CREATE SEQUENCE", wantText: "expected an identifier"},
		{name: "drop view without a name", sql: "DROP VIEW", wantText: "expected an identifier"},
		{name: "too many array dimensions", sql: "CREATE TABLE t (x INTEGER[][][][][][][])", wantError: dialectkit.ErrParseDepth},
		{
			name:      "type nesting deeper than the parse depth",
			sql:       "CREATE TABLE t (x STRUCT(a STRUCT(b STRUCT(c INTEGER))))",
			options:   []Option{WithMaxParseDepth(2)},
			wantError: dialectkit.ErrParseDepth,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])

			require.Error(t, err)
			assert.Nil(t, mutation)
			assert.NotContains(t, err.Error(), "panic")
			if testCase.wantError != nil {
				assert.ErrorIs(t, err, testCase.wantError)
			}
			if testCase.wantText != "" {
				assert.Contains(t, err.Error(), testCase.wantText)
			}
		})
	}
}

func TestAnalyseQueryReturnsSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantError error
		name      string
		sql       string
		wantText  string
		options   []Option
	}{
		{name: "unclosed parenthesised compound arm", sql: "SELECT 1 UNION (SELECT 2", wantError: fault.ErrParse},
		{name: "update without SET", sql: "UPDATE t x = 1", wantText: "expected keyword SET"},
		{name: "delete without FROM", sql: "DELETE t", wantText: "expected keyword FROM"},
		{name: "insert without a table", sql: "INSERT INTO", wantText: "expected an identifier"},
		{name: "unbalanced join USING list", sql: "SELECT * FROM t JOIN u USING (a", wantError: fault.ErrParse},
		{name: "unbalanced ANY list", sql: "SELECT x = ANY (1", wantError: fault.ErrParse},
		{name: "unbalanced cast modifier", sql: "SELECT x::decimal(10 FROM t", wantError: fault.ErrParse},
		{name: "unbalanced CAST modifier", sql: "SELECT CAST(x AS decimal(10", wantError: fault.ErrParse},
		{name: "unbalanced pivot body", sql: "SELECT * FROM PIVOT (", wantError: fault.ErrParse},
		{name: "unbalanced conflict target", sql: "INSERT INTO t VALUES (1) ON CONFLICT (a DO NOTHING", wantError: fault.ErrParse},
		{name: "unbalanced derived table column list", sql: "SELECT * FROM (SELECT 1) s (a", wantError: fault.ErrParse},
		{name: "schema-qualified table without a name", sql: "SELECT * FROM s.", wantText: "expected an identifier"},
		{name: "positional parameter zero", sql: "SELECT $0", wantError: fault.ErrParse},
		{
			name:      "expression nesting deeper than the parse depth",
			sql:       "SELECT ((((((1))))))",
			options:   []Option{WithMaxParseDepth(4)},
			wantError: dialectkit.ErrParseDepth,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

			require.Error(t, err)
			assert.Nil(t, analysis)
			assert.NotContains(t, err.Error(), "panic")
			if testCase.wantError != nil {
				assert.ErrorIs(t, err, testCase.wantError)
			}
			if testCase.wantText != "" {
				assert.Contains(t, err.Error(), testCase.wantText)
			}
		})
	}
}

func TestWithMaxTokensPerStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "positive limit is applied", limit: 50, want: 50},
		{name: "zero keeps the default", limit: 0, want: dialectkit.DefaultMaxTokensPerStatement},
		{name: "negative keeps the default", limit: -1, want: dialectkit.DefaultMaxTokensPerStatement},
		{name: "a limit above the ceiling is lowered to it", limit: dialectkit.MaxTokensPerStatementCeiling + 1,
			want: dialectkit.MaxTokensPerStatementCeiling},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(testCase.limit))

			assert.Equal(t, testCase.want, engine.settings.Limits.TokensPerStatement())
		})
	}
}

func TestTokenBudgetRejectsOversizedStatements(t *testing.T) {
	t.Parallel()

	engine := New(WithMaxTokensPerStatement(10))

	testCases := []struct {
		name       string
		sql        string
		wantReject bool
		isDDL      bool
	}{
		{name: "analysed query over budget", sql: "SELECT a, b, c, d, e, f FROM t", wantReject: true},
		{name: "analysed query within budget", sql: "SELECT a FROM t", wantReject: false},
		{name: "DDL over budget", sql: "CREATE TABLE t (a INTEGER, b INTEGER, c INTEGER)", wantReject: true, isDDL: true},
		{name: "statement without a DDL handler is not counted", sql: "INSERT INTO t VALUES (1, 2, 3, 4, 5, 6)", wantReject: false, isDDL: true},
		{name: "a query with nothing to analyse over budget", sql: "SET threads = 1 + 2 + 3 + 4 + 5", wantReject: true},
		{name: "an extension load over budget", sql: "LOAD 'a' || 'b' || 'c' || 'd' || 'e' || 'f'", wantReject: true, isDDL: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			if testCase.isDDL {
				_, err = engine.ApplyDDL(t.Context(), statements[0])
			} else {
				_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
			}

			if !testCase.wantReject {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, dialectkit.ErrTokenBudget)
			assert.Contains(t, err.Error(), "more than 10 tokens")
		})
	}
}

func TestOptionsAreAdditive(t *testing.T) {
	t.Parallel()

	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	engine := New(
		WithExtraTypes(map[string]sqlmodel.SQLType{"First_Type": text}),
		WithExtraTypes(map[string]sqlmodel.SQLType{"second_type": text}),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) { builder.NeverNull("first_function", nil, builder.Text()) }),
		WithExtraFunctions(func(builder *FunctionCatalogueBuilder) { builder.NeverNull("second_function", nil, builder.Bigint()) }),
		WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
			if name == "hooked" || name == "shared" {
				return new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"))
			}
			return nil
		}),
		WithTypeNormaliserHook(func(name string, _ []int) *sqlmodel.SQLType {
			if name == "shared" {
				return new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "uuid"))
			}
			return nil
		}),
	)

	assert.Equal(t, sqlmodel.TypeCategoryText, engine.NormaliseTypeName("FIRST_TYPE").Category, "an extra type reaches the normaliser, ignoring case")
	assert.Equal(t, sqlmodel.TypeCategoryText, engine.NormaliseTypeName("second_type").Category)
	assert.Equal(t, sqlmodel.TypeCategoryArray, engine.NormaliseTypeName("second_type[]").Category)
	assert.Contains(t, engine.BuiltinTypes().Types, "first_type")
	assert.Contains(t, engine.BuiltinFunctions().Functions, "first_function")
	assert.Contains(t, engine.BuiltinFunctions().Functions, "second_function")
	assert.Contains(t, engine.BuiltinFunctions().Functions, "lower", "an extra registration keeps the built-ins")
	assert.Equal(t, sqlmodel.TypeCategoryJSON, engine.NormaliseTypeName("hooked").Category, "an earlier hook still answers")
	assert.Equal(t, sqlmodel.TypeCategoryUUID, engine.NormaliseTypeName("shared").Category, "the latest hook answers first")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, engine.NormaliseTypeName("integer").Category, "declining hooks fall through")
}

func TestExtraTypesResolveInDDL(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraTypes(map[string]sqlmodel.SQLType{"geo": sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "geo")}))
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE places (id INTEGER, shape GEO NOT NULL, shapes geo[])")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])

	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryBinary, mutation.Columns[1].SQLType.Category)
	assert.True(t, mutation.Columns[2].IsArray)
	assert.Equal(t, sqlmodel.TypeCategoryBinary, mutation.Columns[2].SQLType.Category)
}

func TestImplicitCastAndPromoteHooksChain(t *testing.T) {
	t.Parallel()

	engine := New(
		WithImplicitCastHook(func(from, _ sqlmodel.SQLType) *bool {
			if from.Category == sqlmodel.TypeCategoryBoolean {
				return new(true)
			}
			return nil
		}),
		WithImplicitCastHook(func(from, _ sqlmodel.SQLType) *bool {
			if from.Category == sqlmodel.TypeCategoryUUID {
				return new(false)
			}
			return nil
		}),
		WithPromoteTypeHook(func(left, _ sqlmodel.SQLType) *sqlmodel.SQLType {
			if left.EngineName == "first" {
				return &left
			}
			return nil
		}),
		WithPromoteTypeHook(func(_, right sqlmodel.SQLType) *sqlmodel.SQLType {
			if right.EngineName == "second" {
				return &right
			}
			return nil
		}),
	)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")

	assert.True(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "bool"), text))
	assert.False(t, engine.CanImplicitCast(sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "uuid"), text))
	assert.Equal(t, "first", engine.PromoteType(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "first"), text).EngineName)
	assert.Equal(t, "second", engine.PromoteType(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "first"),
		sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "second")).EngineName)
}

func TestErrorsWrapTheGeneratorSentinels(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine   *Dialect
		sentinel error
		name     string
		sql      string
		isDDL    bool
	}{
		{name: "syntax error", engine: New(), sql: "SELECT id FROM users WHERE id = $1 ORDER", sentinel: fault.ErrParse},
		{name: "DDL syntax error", engine: New(), sql: "CREATE TABLE t (a INTEGER b INTEGER)", sentinel: fault.ErrParse, isDDL: true},
		{name: "analysis depth", engine: New(WithMaxParseDepth(2)), sql: "SELECT id FROM (SELECT id FROM (SELECT id FROM (SELECT 1 AS id) a) b) c", sentinel: fault.ErrLimit},
		{name: "expression depth", engine: New(WithMaxParseDepth(3)), sql: "SELECT ((((((1))))))", sentinel: fault.ErrLimit},
		{name: "token budget", engine: New(WithMaxTokensPerStatement(3)), sql: "SELECT id FROM users WHERE id = 1", sentinel: fault.ErrLimit},
		{name: "type nesting", engine: New(WithMaxParseDepth(2)), sql: "CREATE TABLE t (x STRUCT(a STRUCT(b STRUCT(c INTEGER))))", sentinel: fault.ErrLimit, isDDL: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := testCase.engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			if testCase.isDDL {
				_, err = testCase.engine.ApplyDDL(t.Context(), statements[0])
			} else {
				_, err = testCase.engine.AnalyseQuery(t.Context(), nil, statements[0])
			}
			require.ErrorIs(t, err, testCase.sentinel)
		})
	}
}

func TestMacroDataAccessComesFromTheBody(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want sqlmodel.FunctionDataAccess
	}{
		{name: "arithmetic", sql: "CREATE MACRO tonnes(kg) AS kg / 1000.0", want: sqlmodel.DataAccessReadOnly},
		{name: "built-in call", sql: "CREATE MACRO shout(word) AS upper(word) || '!'", want: sqlmodel.DataAccessReadOnly},
		{name: "polymorphic built-in", sql: "CREATE MACRO total(xs) AS list_reduce(xs, (a, b) -> a + b)", want: sqlmodel.DataAccessReadOnly},
		{name: "sequence advance", sql: "CREATE MACRO next_id() AS nextval('ids')", want: sqlmodel.DataAccessModifiesData},
		{name: "sequence advance inside a lambda", sql: "CREATE MACRO next_ids(l) AS list_transform(l, x -> nextval('ids'))",
			want: sqlmodel.DataAccessModifiesData},
		{name: "sequence advance inside a list comprehension", sql: "CREATE MACRO next_ids(l) AS [nextval('ids') FOR x IN l]",
			want: sqlmodel.DataAccessModifiesData},
		{name: "sequence advance in a window function", sql: "CREATE MACRO m() AS max(nextval('ids')) OVER ()",
			want: sqlmodel.DataAccessModifiesData},
		{name: "another macro", sql: "CREATE MACRO twice(x) AS other_macro(x) * 2", want: sqlmodel.DataAccessUnknown},
		{name: "table macro body", sql: "CREATE MACRO pick(n) AS TABLE SELECT * FROM t WHERE id = n", want: sqlmodel.DataAccessUnknown},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation.Function)
			assert.Equal(t, testCase.want, mutation.Function.DataAccess)
		})
	}
}

func TestTableMacroRecordsItsBody(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE MACRO harbour_landings(name) AS TABLE SELECT species, kg FROM landings WHERE harbour = name")

	require.NotNil(t, mutation.Function)
	assert.True(t, mutation.Function.ReturnsSet)
	assert.Equal(t, "sql", mutation.Function.Language)
	assert.Equal(t, "SELECT species, kg FROM landings WHERE harbour = name", mutation.Function.BodySQL)
	require.Len(t, mutation.Columns, 2)
}

func TestTableMacroColumnsAreTypedFromTheCatalogue(t *testing.T) {
	t.Parallel()

	engine := New()
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	integer := sqlmodel.NewIntegerType("int4", 32, false)
	catalogue := buildTestCatalogue()
	catalogue.Schemas["main"].Tables["landings"] = &sqlmodel.Table{
		Name:    "landings",
		Schema:  "main",
		Columns: []sqlmodel.Column{{Name: "species", SQLType: text, Nullable: false}, {Name: "kg", SQLType: integer, Nullable: true}},
	}
	composite := sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, "macro_row")
	catalogue.Schemas["main"].CompositeTypes["macro_row"] = &sqlmodel.CompositeType{
		Name:   "macro_row",
		Schema: "main",
		Fields: []sqlmodel.Column{
			{Name: "species", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: true},
			{Name: "total", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: true},
			{Name: "kg", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: true},
		},
	}
	macro := sqlmodel.NewFunction(nil, composite, sqlmodel.FunctionNullableCalledOnNull, sqlmodel.WithFunctionName("harbour_landings"))
	macro.ReturnsSet = true
	macro.Language = "sql"
	macro.BodySQL = "SELECT species, kg * 2 AS total, kg FROM landings WHERE harbour = name"
	catalogue.Schemas["main"].Functions["harbour_landings"] = []*sqlmodel.Function{macro}

	columns := catalogueFunctionColumns(t, engine, catalogue, "harbour_landings")

	require.Len(t, columns, 3)
	assert.Equal(t, "varchar", columns[0].SQLType.EngineName)
	assert.False(t, columns[0].Nullable)
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, columns[1].SQLType.Category, "a computed column keeps its recorded type")
	assert.Equal(t, "int4", columns[2].SQLType.EngineName)
	assert.True(t, columns[2].Nullable)
}

func TestAnalyseQueryRejectsUnrecognisedStatements(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{"SELEC id FROM items", "GRANT SELECT ON items TO someone", "(DELETE FROM items)"} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, sql)

			require.ErrorIs(t, err, fault.ErrParse)
		})
	}
}

func TestAnalyseQueryAcceptsStatementsWithNothingToAnalyse(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"TRUNCATE items", "PRAGMA version", "SET threads = 2", "RESET threads", "CHECKPOINT", "FORCE CHECKPOINT",
		"SHOW TABLES", "DESCRIBE items", "SUMMARIZE items", "CALL pragma_version()", "BEGIN TRANSACTION", "COMMIT",
		"ROLLBACK", "START TRANSACTION", "ABORT", "VACUUM", "ANALYZE", "USE memory", "EXPLAIN SELECT 1",
		"ATTACH 'other.db' AS other", "DETACH other", "EXPORT DATABASE 'directory'", "IMPORT DATABASE 'directory'",
		"COPY items TO 'items.csv'", "PREPARE fetch_item AS SELECT 1", "EXECUTE fetch_item", "DEALLOCATE fetch_item",
		"CREATE TABLE extra (id INTEGER)", "MERGE INTO items USING other ON other.id = items.id WHEN MATCHED THEN DELETE",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, sql)

			assert.Empty(t, analysis.OutputColumns)
		})
	}
}

func TestResolveFunctionCallTypesListLambdaFunctions(t *testing.T) {
	t.Parallel()

	varchar := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	bigint := sqlmodel.NewIntegerType("int8", 64, false)
	integer := sqlmodel.NewIntegerType("int4", 32, false)
	textList := newArrayType(varchar)
	integerList := newArrayType(integer)
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")

	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
		want      string
	}{
		{name: "a transform takes the lambda's type", function: "list_transform", arguments: []sqlmodel.SQLType{textList, bigint},
			want: "int8[]"},
		{name: "a transform whose lambda is untyped keeps the list", function: "array_transform",
			arguments: []sqlmodel.SQLType{integerList, unknown}, want: "int4[]"},
		{name: "apply is a transform", function: "apply", arguments: []sqlmodel.SQLType{integerList, varchar}, want: "varchar[]"},
		{name: "list_apply is a transform", function: "list_apply", arguments: []sqlmodel.SQLType{integerList, varchar},
			want: "varchar[]"},
		{name: "a filter keeps the list", function: "filter", arguments: []sqlmodel.SQLType{textList, varchar}, want: "varchar[]"},
		{name: "a reduce takes the element's type", function: "reduce", arguments: []sqlmodel.SQLType{textList, varchar},
			want: "varchar"},
		{name: "a reduce whose lambda gives another type still takes the element's", function: "list_reduce",
			arguments: []sqlmodel.SQLType{integerList, bigint}, want: "int4"},
		{name: "a reduce whose lambda is untyped takes the element", function: "list_reduce",
			arguments: []sqlmodel.SQLType{integerList, unknown}, want: "int4"},
		{name: "a reduce from a wider starting value takes the starting value's type", function: "list_reduce",
			arguments: []sqlmodel.SQLType{integerList, unknown, bigint}, want: "int8"},
		{name: "a reduce from a narrower starting value keeps the element's type", function: "array_reduce",
			arguments: []sqlmodel.SQLType{newArrayType(bigint), unknown, integer}, want: "int8"},
		{name: "a reduce over a list of unknown elements takes the lambda's type", function: "list_reduce",
			arguments: []sqlmodel.SQLType{unknown, varchar}, want: "varchar"},
		{name: "a row is an unnamed struct", function: "row", arguments: []sqlmodel.SQLType{integer, varchar}, want: "struct"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := New().ResolveFunctionCall(t.Context(), &sqlmodel.Catalogue{}, testCase.function, "", testCase.arguments)

			require.NoError(t, err)
			require.NotNil(t, resolution)
			assert.Equal(t, testCase.want, resolution.ReturnType.EngineName)
		})
	}
}

func TestRowIsAStructOfPositionalFieldsThatMayHoldNull(t *testing.T) {
	t.Parallel()

	resolution, err := New().ResolveFunctionCall(t.Context(), &sqlmodel.Catalogue{}, "row", "",
		[]sqlmodel.SQLType{sqlmodel.NewIntegerType("int4", 32, false), sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")})

	require.NoError(t, err)
	require.Equal(t, sqlmodel.TypeCategoryStruct, resolution.ReturnType.Category)
	require.Equal(t, sqlmodel.FunctionNullableNeverNull, resolution.NullableBehaviour)
	require.Len(t, resolution.ReturnType.StructFields, 2)
	for index, name := range []string{"element1", "element2"} {
		field := resolution.ReturnType.StructFields[index]
		require.Equal(t, name, field.Name)
		require.True(t, field.Positional)
		require.True(t, field.SQLType.Nullable)
	}
	require.Equal(t, sqlmodel.TypeCategoryText, resolution.ReturnType.StructFields[1].SQLType.Category)
}

func catalogueFunctionColumns(t *testing.T, engine *Dialect, catalogue *sqlmodel.Catalogue, name string) []ast.ScopedColumn {
	t.Helper()
	columns, err := engine.TableValuedFunctionColumnsFromCatalogue(t.Context(), catalogue, name)
	require.NoError(t, err)
	return columns
}

func TestApplyDDLRefusesAStatementThatRunsOnIntoTheNext(t *testing.T) {
	t.Parallel()

	const next = "\nCREATE TABLE b (x INTEGER)"
	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "CREATE INDEX", sql: "CREATE INDEX i ON t (a)" + next, wantToken: "CREATE"},
		{name: "DROP TABLE", sql: "DROP TABLE a" + next, wantToken: "CREATE"},
		{name: "CREATE VIEW", sql: "CREATE VIEW v AS SELECT 1" + next, wantToken: "CREATE"},
		{name: "CREATE TYPE", sql: "CREATE TYPE mood AS ENUM ('happy')" + next, wantToken: "CREATE"},
		{name: "CREATE SEQUENCE", sql: "CREATE SEQUENCE s" + next, wantToken: "CREATE"},
		{name: "COMMENT ON", sql: "COMMENT ON TABLE t IS 'x'" + next, wantToken: "CREATE"},
		{name: "INSTALL", sql: "INSTALL httpfs" + next, wantToken: "CREATE"},
		{name: "LOAD", sql: "LOAD httpfs" + next, wantToken: "CREATE"},
		{name: "ALTER TABLE", sql: "ALTER TABLE t ADD COLUMN c INTEGER" + next, wantToken: "CREATE"},
		{name: "a seed INSERT", sql: "INSERT INTO t VALUES (1)" + next, wantToken: "CREATE"},
		{name: "a table followed by LOAD", sql: "CREATE TABLE a (x INTEGER)\nLOAD httpfs", wantToken: "LOAD"},
		{name: "a table followed by INSTALL", sql: "CREATE TABLE a (x INTEGER)\nINSTALL httpfs", wantToken: "INSTALL"},
		{name: "a table followed by an ALTER", sql: "CREATE TABLE a (x INTEGER)\nALTER TABLE a ADD COLUMN y INTEGER", wantToken: "ALTER"},
		{name: "a table followed by a DROP", sql: "CREATE TABLE a (x INTEGER)\nDROP VIEW v", wantToken: "DROP"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			mutation, err := engine.ApplyDDL(t.Context(), parseOne(t, engine, testCase.sql))

			assert.Nil(t, mutation)
			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantToken, parseError.Token)
			assert.Equal(t, strings.LastIndex(testCase.sql, "\n")+1, parseError.Position)
		})
	}
}

func TestApplyDDLAcceptsWordsThatOnlyLookLikeTheNextStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "columns named load and install in a view", sql: "CREATE VIEW v AS SELECT id,\n  load l,\n  install\nFROM t"},
		{name: "a column named comment in a view", sql: "CREATE VIEW v AS SELECT id,\n  comment\nFROM t"},
		{name: "columns named load and install in a table", sql: "CREATE TABLE t (\n  load INTEGER,\n  install INTEGER\n)"},
		{name: "an ALTER TABLE altering a column named type", sql: "ALTER TABLE t\n  ALTER type SET NOT NULL"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			mutation, err := engine.ApplyDDL(t.Context(), parseOne(t, engine, testCase.sql))

			require.NoError(t, err)
			assert.NotNil(t, mutation)
		})
	}
}

func searchPathCatalogue(searchPath []string) *sqlmodel.Catalogue {
	rows := func(schema, column string) *sqlmodel.Schema {
		composite := sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, "f_row")
		function := sqlmodel.NewFunction(nil, composite, sqlmodel.FunctionNullableCalledOnNull, sqlmodel.WithFunctionName("f"))
		function.ReturnsSet = true
		return &sqlmodel.Schema{
			Name:      schema,
			Functions: map[string][]*sqlmodel.Function{"f": {nil, function}},
			CompositeTypes: map[string]*sqlmodel.CompositeType{"f_row": {Name: "f_row", Schema: schema, Fields: []sqlmodel.Column{
				{Name: column, SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), Nullable: true},
			}}},
		}
	}
	return &sqlmodel.Catalogue{
		DefaultSchema: "main",
		SearchPath:    searchPath,
		Identifiers:   sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
		Schemas:       map[string]*sqlmodel.Schema{"aaa": rows("aaa", "code"), "main": rows("main", "id"), "empty": nil},
	}
}

func TestTableValuedFunctionColumnsFollowTheSearchPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		catalogue    *sqlmodel.Catalogue
		name         string
		functionName string
		wantColumns  []string
	}{
		{name: "an unqualified name resolves in the default schema", catalogue: searchPathCatalogue(nil), functionName: "f",
			wantColumns: []string{"id"}},
		{name: "a qualified name resolves in its schema", catalogue: searchPathCatalogue(nil), functionName: "aaa.f",
			wantColumns: []string{"code"}},
		{name: "an unqualified name resolves in search path order", catalogue: searchPathCatalogue([]string{"aaa", "main"}),
			functionName: "f", wantColumns: []string{"code"}},
		{name: "a schema off the search path is not searched", catalogue: searchPathCatalogue([]string{"main"}),
			functionName: "F", wantColumns: []string{"id"}},
		{name: "a qualified name in a schema the catalogue lacks", catalogue: searchPathCatalogue(nil), functionName: "nowhere.f",
			wantColumns: nil},
		{name: "a schema the catalogue holds as nil", catalogue: searchPathCatalogue(nil), functionName: "empty.f",
			wantColumns: nil},
		{name: "a nil catalogue", catalogue: nil, functionName: "f", wantColumns: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns, err := New().TableValuedFunctionColumnsFromCatalogue(t.Context(), testCase.catalogue, testCase.functionName)

			require.NoError(t, err)
			var names []string
			for _, column := range columns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.wantColumns, names)
		})
	}
}

func TestEntryPointsReturnTheErrorBuildingTheFunctionCatalogue(t *testing.T) {
	t.Parallel()

	errCatalogue := errors.New("the built-in catalogue did not decode")
	engine := New()
	engine.initErr = errCatalogue
	statement := parseOne(t, engine, "SELECT 1")

	_, applyErr := engine.ApplyDDL(t.Context(), statement)
	_, analyseErr := engine.AnalyseQuery(t.Context(), nil, statement)

	require.ErrorIs(t, applyErr, errCatalogue)
	require.ErrorIs(t, analyseErr, errCatalogue)
}

func TestInfoHandsOutACopyEachTime(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		change func(info *dialect.Info)
		read   func(info dialect.Info) any
		name   string
	}{
		{name: "the system columns", change: func(info *dialect.Info) { info.SystemColumns[0] = "changed" },
			read: func(info dialect.Info) any { return info.SystemColumns }},
		{name: "the filter operators", change: func(info *dialect.Info) { info.Filters.Operators[0] = "changed" },
			read: func(info dialect.Info) any { return info.Filters.Operators }},
		{name: "the JSON path functions", change: func(info *dialect.Info) { info.Filters.JSONPathFunctions[0] = "changed" },
			read: func(info dialect.Info) any { return info.Filters.JSONPathFunctions }},
		{name: "the cast forms", change: func(info *dialect.Info) { info.Filters.Casts[0].Before = "changed" },
			read: func(info dialect.Info) any { return info.Filters.Casts }},
		{name: "the cast types", change: func(info *dialect.Info) { info.Filters.CastTypes[0] = "changed" },
			read: func(info dialect.Info) any { return info.Filters.CastTypes }},
		{name: "the placeholder sigils", change: func(info *dialect.Info) { info.Placeholders.Sigils[0].Prefix = "changed" },
			read: func(info dialect.Info) any { return info.Placeholders.Sigils }},
		{name: "the search path", change: func(info *dialect.Info) { info.SchemaSearchPath[0] = "changed" },
			read: func(info dialect.Info) any { return info.SchemaSearchPath }},
		{name: "the drivers", change: func(info *dialect.Info) { info.Drivers[0] = "changed" },
			read: func(info dialect.Info) any { return info.Drivers }},
		{name: "the operator features", change: func(info *dialect.Info) {
			info.OperatorFeatures["||"] = ast.SQLFeatureLambda
			delete(info.OperatorFeatures, "->")
		}, read: func(info dialect.Info) any { return info.OperatorFeatures }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			want := testCase.read(New().Info())
			first := engine.Info()
			testCase.change(&first)

			assert.NotEqual(t, want, testCase.read(first))
			assert.Equal(t, want, testCase.read(engine.Info()))
			assert.Equal(t, want, testCase.read(New().Info()))
		})
	}
}

func TestApplyDDLFindsAMissingSemicolonPastTheTokenBudget(t *testing.T) {
	t.Parallel()

	const limit = 20
	rows := strings.TrimSuffix(strings.Repeat("(1), ", 4*limit), ", ")
	const next = "\nCREATE TABLE b (x INTEGER)"
	testCases := []struct {
		name       string
		sql        string
		wantToken  string
		overBudget bool
	}{
		{name: "a seed INSERT that runs on past the budget", sql: "INSERT INTO t VALUES " + rows + next, wantToken: "CREATE", overBudget: true},
		{name: "a seed INSERT that ends where it should", sql: "INSERT INTO t VALUES " + rows, wantToken: "", overBudget: true},
		{name: "a seed INSERT that runs on within the budget", sql: "INSERT INTO t VALUES (1)" + next, wantToken: "CREATE", overBudget: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxTokensPerStatement(limit))
			statement := parseOne(t, engine, testCase.sql)
			parsed, ok := statement.Raw.(*parsedStatement)
			require.True(t, ok)
			require.Equal(t, testCase.overBudget, parsed.overBudget)

			mutation, err := engine.ApplyDDL(t.Context(), statement)
			assert.Nil(t, mutation)
			if testCase.wantToken == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, fault.ErrParse)
			parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, isParseError)
			assert.Equal(t, testCase.wantToken, parseError.Token)
			assert.Equal(t, strings.LastIndex(testCase.sql, "\n")+1, parseError.Position)
		})
	}
}

func TestApplyDDLReadsAnOverBudgetStatementInLinearTime(t *testing.T) {
	engine := New(WithMaxTokensPerStatement(20))
	applyRows := func(count int) func() {
		statement := parseOne(t, engine, "INSERT INTO t VALUES "+strings.TrimSuffix(strings.Repeat("(1, 'a'), ", count), ", "))
		return func() {
			_, err := engine.ApplyDDL(t.Context(), statement)
			assert.NoError(t, err)
		}
	}
	scaling.Linear(t, applyRows(20_000), applyRows(80_000))
}
