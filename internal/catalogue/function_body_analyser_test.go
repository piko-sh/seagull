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

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestAnalyseFunctionBody_LiteralReturn(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			if name == "Int64" {
				return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int64"}
			}
			return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: name}
		},
	}
	resolver := newBodyTypeResolverWithEngine(engine)

	signature := &ast.FunctionDefinition{
		Function: &sqlmodel.Function{
			Name:       "fortytwo",
			ReturnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		},
		Body: &ast.LiteralExpression{
			TypeName: "Int64",
		},
	}

	err := analyseFunctionBody(t.Context(), signature, resolver)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, signature.ReturnType.Category)
	assert.Equal(t, "Int64", signature.ReturnType.EngineName)
}

func TestAnalyseFunctionBody_FunctionCallReturn(t *testing.T) {
	t.Parallel()

	uint64Type := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "UInt64"}
	builtins := &ast.FunctionCatalogue{
		Functions: map[string][]*sqlmodel.Function{
			"arraylength": {
				{
					Name:       "arrayLength",
					ReturnType: uint64Type,
					Arguments: []sqlmodel.FunctionArgument{
						{Name: "arr", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray}},
					},
					NullableBehaviour: sqlmodel.FunctionNullableNeverNull,
					DataAccess:        sqlmodel.DataAccessReadOnly,
				},
			},
		},
	}
	engine := &fakedialect.Engine{
		BuiltinFunctionsFunc: func() *ast.FunctionCatalogue { return builtins },
	}
	resolver := newBodyTypeResolverWithEngine(engine)

	signature := &ast.FunctionDefinition{
		Function: &sqlmodel.Function{
			Name:       "lengthof",
			ReturnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		},
		BodyParameters: []string{"x"},
		Body: &ast.FunctionCallExpression{
			FunctionName: "arrayLength",
			Arguments: []ast.Expression{
				&ast.ColumnRefExpression{ColumnName: "x"},
			},
		},
	}

	err := analyseFunctionBody(t.Context(), signature, resolver)
	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, signature.ReturnType.Category)
	assert.Equal(t, "UInt64", signature.ReturnType.EngineName)
}

func TestAnalyseFunctionBody_ArithmeticWithParameter(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			if name == "Int64" {
				return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int64"}
			}
			return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: name}
		},
	}
	resolver := newBodyTypeResolverWithEngine(engine)

	signature := &ast.FunctionDefinition{
		Function: &sqlmodel.Function{
			Name:       "twice",
			ReturnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		},
		BodyParameters: []string{"x"},
		Body: &ast.BinaryOpExpression{
			Operator: "+",
			Left:     &ast.ColumnRefExpression{ColumnName: "x"},
			Right:    &ast.LiteralExpression{TypeName: "Int64"},
		},
	}

	err := analyseFunctionBody(t.Context(), signature, resolver)
	require.NoError(t, err)

	assert.Equal(t, sqlmodel.TypeCategoryInteger, signature.ReturnType.Category)
	assert.Equal(t, "Int64", signature.ReturnType.EngineName)
}

func TestAnalyseFunctionBody_MultiParameter(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{}
	resolver := newBodyTypeResolverWithEngine(engine)

	signature := &ast.FunctionDefinition{
		Function: &sqlmodel.Function{
			Name:       "addxy",
			ReturnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		},
		BodyParameters: []string{"x", "y"},
		Body: &ast.BinaryOpExpression{
			Operator: "+",
			Left:     &ast.ColumnRefExpression{ColumnName: "x"},
			Right:    &ast.ColumnRefExpression{ColumnName: "y"},
		},
	}

	err := analyseFunctionBody(t.Context(), signature, resolver)
	require.NoError(t, err)

	assert.Equal(t, sqlmodel.TypeCategoryUnknown, signature.ReturnType.Category)
}

func TestAnalyseFunctionBody_NilSignature(t *testing.T) {
	t.Parallel()

	resolver := newBodyTypeResolver()

	err := analyseFunctionBody(t.Context(), nil, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil signature")
}

func TestAnalyseFunctionBody_NilBody(t *testing.T) {
	t.Parallel()

	resolver := newBodyTypeResolver()
	preset := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}
	signature := &sqlmodel.Function{
		Name:       "noop",
		ReturnType: preset,
	}

	err := analyseFunctionBody(t.Context(), ast.NewFunctionDefinition(signature), resolver)
	require.NoError(t, err)

	assert.Equal(t, preset, signature.ReturnType)
}

func TestAnalyseFunctionBody_DoesNotOverwriteSetReturnType(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			if name == "Int64" {
				return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int64"}
			}
			return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: name}
		},
	}
	resolver := newBodyTypeResolverWithEngine(engine)

	preset := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}
	signature := &ast.FunctionDefinition{
		Function: &sqlmodel.Function{
			Name:       "preset",
			ReturnType: preset,
		},
		Body: &ast.LiteralExpression{
			TypeName: "Int64",
		},
	}

	err := analyseFunctionBody(t.Context(), signature, resolver)
	require.NoError(t, err)
	assert.Equal(t, preset, signature.ReturnType, "ReturnType must not be overwritten when set")
}

func TestAnalyseFunctionBody_NilResolver(t *testing.T) {
	t.Parallel()

	signature := &ast.FunctionDefinition{
		Function: &sqlmodel.Function{
			Name:       "literal",
			ReturnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		},
		Body: &ast.LiteralExpression{
			TypeName: "Int64",
		},
	}

	err := analyseFunctionBody(t.Context(), signature, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil resolver")
}

func newBodyTypeResolver() *resolve.TypeResolver {
	engine := &fakedialect.Engine{}
	catalogue := fakedialect.NewCatalogue("public")
	builtins := &ast.FunctionCatalogue{
		Functions: make(map[string][]*sqlmodel.Function),
	}
	funcResolver := resolve.NewFunctionResolver(builtins, catalogue, engine)

	return resolve.NewTypeResolver(catalogue, funcResolver, engine)
}

func newBodyTypeResolverWithEngine(engine *fakedialect.Engine) *resolve.TypeResolver {
	catalogue := fakedialect.NewCatalogue("public")
	builtins := engine.BuiltinFunctions()
	funcResolver := resolve.NewFunctionResolver(builtins, catalogue, engine)

	return resolve.NewTypeResolver(catalogue, funcResolver, engine)
}

func TestAnalyseFunctionBodyInfersOnlyAReturnTypeTheEngineLeftUndeclared(t *testing.T) {
	t.Parallel()

	inferred := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}
	testCases := []struct {
		name     string
		declared sqlmodel.SQLType
		want     sqlmodel.SQLType
	}{
		{name: "an unknown type with no engine name is inferred", declared: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), want: inferred},
		{name: "an integer declared without an engine name is kept", declared: sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, ""),
			want: sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "")},
		{name: "an unknown type the engine named is kept", declared: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "mystery"),
			want: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "mystery")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{
				NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
					return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: name}
				},
			}
			definition := &ast.FunctionDefinition{
				Function: &sqlmodel.Function{Name: "f", ReturnType: testCase.declared},
				Body:     &ast.LiteralExpression{TypeName: "String"},
			}

			require.NoError(t, analyseFunctionBody(t.Context(), definition, newBodyTypeResolverWithEngine(engine)))
			assert.Equal(t, testCase.want, definition.ReturnType)
		})
	}
}
