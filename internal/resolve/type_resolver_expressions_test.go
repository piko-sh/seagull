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

package resolve

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func newExpressionTestResolver(t *testing.T) (*TypeResolver, *ScopeChain) {
	t.Helper()

	engine := &fakedialect.Engine{
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			switch name {
			case "int4", "integer":
				return sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
			case "int8", "bigint":
				return sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}
			case "text":
				return sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
			case "boolean", "bool":
				return sqlmodel.SQLType{EngineName: "boolean", Category: sqlmodel.TypeCategoryBoolean}
			case "float8":
				return sqlmodel.SQLType{EngineName: "float8", Category: sqlmodel.TypeCategoryFloat}
			default:
				return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryUnknown}
			}
		},
		BinaryResultFunc: testBinaryResult,
	}

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{
			Name:    "id",
			SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
		},
		sqlmodel.Column{
			Name:    "name",
			SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
		},
		sqlmodel.Column{
			Name:     "email",
			SQLType:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			Nullable: true,
		},
	)

	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(
		ast.TableReference{Name: "users"},
		ast.JoinInner,
		catalogue.Schemas["public"].Tables["users"],
	)

	builtins := engine.BuiltinFunctions()
	functionResolver := NewFunctionResolver(builtins, catalogue, engine)

	resolver := NewTypeResolver(catalogue, functionResolver, engine)
	return resolver, scope
}

func TestResolveExpressionType_Nil(t *testing.T) {
	t.Parallel()

	resolver, scope := newExpressionTestResolver(t)
	sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), nil, scope, new(false))

	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
	assert.True(t, nullable, "nil expression should be nullable")
}

func TestResolveColumnRefExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		tableAlias       string
		columnName       string
		expectCategory   sqlmodel.SQLTypeCategory
		expectEngineName string
		expectNullable   bool
		expectError      bool
	}{
		{
			name:             "existing non-nullable column returns correct type",
			tableAlias:       "",
			columnName:       "id",
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int4",
			expectNullable:   false,
			expectError:      false,
		},
		{
			name:             "existing nullable column preserves nullability",
			tableAlias:       "",
			columnName:       "email",
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
			expectNullable:   true,
			expectError:      false,
		},
		{
			name:             "qualified column reference resolves correctly",
			tableAlias:       "users",
			columnName:       "name",
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
			expectNullable:   false,
			expectError:      false,
		},
		{
			name:        "unknown column returns error",
			tableAlias:  "",
			columnName:  "nonexistent",
			expectError: true,
		},
		{
			name:        "unknown table alias returns error",
			tableAlias:  "orders",
			columnName:  "id",
			expectError: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			expression := &ast.ColumnRefExpression{
				TableAlias: testCase.tableAlias,
				ColumnName: testCase.columnName,
			}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

			if testCase.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expectCategory, sqlType.Category)
			assert.Equal(t, testCase.expectEngineName, sqlType.EngineName)
			assert.Equal(t, testCase.expectNullable, nullable)
		})
	}
}

func TestResolveFunctionCallExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		functionName        string
		arguments           []ast.Expression
		builtinSignatures   map[string][]*sqlmodel.Function
		expectCategory      sqlmodel.SQLTypeCategory
		expectNullable      bool
		expectDataModifying bool
		expectError         bool
	}{
		{
			name:         "known function with never-null behaviour returns not-nullable",
			functionName: "count",
			arguments:    []ast.Expression{&ast.ColumnRefExpression{ColumnName: "id"}},
			builtinSignatures: map[string][]*sqlmodel.Function{
				"count": {
					{
						Name:              "count",
						ReturnType:        sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger},
						Arguments:         []sqlmodel.FunctionArgument{{Name: "value", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown}}},
						NullableBehaviour: sqlmodel.FunctionNullableNeverNull,
						DataAccess:        sqlmodel.DataAccessReadOnly,
					},
				},
			},
			expectCategory: sqlmodel.TypeCategoryInteger,
			expectNullable: false,
			expectError:    false,
		},
		{
			name:         "function with returns-null-on-null and nullable argument is nullable",
			functionName: "lower",
			arguments:    []ast.Expression{&ast.ColumnRefExpression{ColumnName: "email"}},
			builtinSignatures: map[string][]*sqlmodel.Function{
				"lower": {
					{
						Name:              "lower",
						ReturnType:        sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
						Arguments:         []sqlmodel.FunctionArgument{{Name: "input", Type: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}}},
						NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
						DataAccess:        sqlmodel.DataAccessReadOnly,
					},
				},
			},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: true,
			expectError:    false,
		},
		{
			name:         "function with returns-null-on-null and non-nullable argument is not nullable",
			functionName: "lower",
			arguments:    []ast.Expression{&ast.ColumnRefExpression{ColumnName: "name"}},
			builtinSignatures: map[string][]*sqlmodel.Function{
				"lower": {
					{
						Name:              "lower",
						ReturnType:        sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
						Arguments:         []sqlmodel.FunctionArgument{{Name: "input", Type: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}}},
						NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
						DataAccess:        sqlmodel.DataAccessReadOnly,
					},
				},
			},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: false,
			expectError:    false,
		},
		{
			name:         "data-modifying function sets dataModifying flag",
			functionName: "nextval",
			arguments:    []ast.Expression{&ast.LiteralExpression{TypeName: "text"}},
			builtinSignatures: map[string][]*sqlmodel.Function{
				"nextval": {
					{
						Name:              "nextval",
						ReturnType:        sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger},
						Arguments:         []sqlmodel.FunctionArgument{{Name: "seqname", Type: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}}},
						NullableBehaviour: sqlmodel.FunctionNullableNeverNull,
						DataAccess:        sqlmodel.DataAccessModifiesData,
					},
				},
			},
			expectCategory:      sqlmodel.TypeCategoryInteger,
			expectNullable:      false,
			expectDataModifying: true,
			expectError:         false,
		},
		{
			name:              "unknown function returns error",
			functionName:      "nonexistent_func",
			arguments:         nil,
			builtinSignatures: nil,
			expectError:       true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{
				NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
					switch name {
					case "int4", "integer":
						return sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
					case "int8":
						return sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}
					case "text":
						return sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
					default:
						return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryUnknown}
					}
				},
				BuiltinFunctionsFunc: func() *ast.FunctionCatalogue {
					if testCase.builtinSignatures == nil {
						return &ast.FunctionCatalogue{
							Functions: make(map[string][]*sqlmodel.Function),
						}
					}
					return &ast.FunctionCatalogue{Functions: testCase.builtinSignatures}
				},
			}

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
				sqlmodel.Column{
					Name:    "id",
					SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
				},
				sqlmodel.Column{
					Name:    "name",
					SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
				},
				sqlmodel.Column{
					Name:     "email",
					SQLType:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
					Nullable: true,
				},
			)

			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			scope.AddTable(
				ast.TableReference{Name: "users"},
				ast.JoinInner,
				catalogue.Schemas["public"].Tables["users"],
			)

			builtins := engine.BuiltinFunctions()
			functionResolver := NewFunctionResolver(builtins, catalogue, engine)
			resolver := NewTypeResolver(catalogue, functionResolver, engine)

			dataModifying := false

			expression := &ast.FunctionCallExpression{
				FunctionName: testCase.functionName,
				Arguments:    testCase.arguments,
			}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, &dataModifying)

			if testCase.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expectCategory, sqlType.Category)
			assert.Equal(t, testCase.expectNullable, nullable)
			assert.Equal(t, testCase.expectDataModifying, dataModifying)
		})
	}
}

func TestResolveCoalesceExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		arguments      []ast.Expression
		expectCategory sqlmodel.SQLTypeCategory
		expectNullable bool
	}{
		{
			name: "single non-nullable argument yields not-nullable result",
			arguments: []ast.Expression{
				&ast.ColumnRefExpression{ColumnName: "name"},
			},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: false,
		},
		{
			name: "nullable and non-nullable arguments yield not-nullable result",
			arguments: []ast.Expression{
				&ast.ColumnRefExpression{ColumnName: "email"},
				&ast.ColumnRefExpression{ColumnName: "name"},
			},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: false,
		},
		{
			name: "all nullable arguments yield nullable result",
			arguments: []ast.Expression{
				&ast.ColumnRefExpression{ColumnName: "email"},
			},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: true,
		},
		{
			name:           "nil argument in list is skipped",
			arguments:      []ast.Expression{nil, &ast.ColumnRefExpression{ColumnName: "id"}},
			expectCategory: sqlmodel.TypeCategoryInteger,
			expectNullable: false,
		},
		{
			name:           "empty arguments yield unknown type and nullable",
			arguments:      nil,
			expectCategory: sqlmodel.TypeCategoryUnknown,
			expectNullable: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			expression := &ast.CoalesceExpression{
				Arguments: testCase.arguments,
			}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

			require.NoError(t, err)
			assert.Equal(t, testCase.expectCategory, sqlType.Category)
			assert.Equal(t, testCase.expectNullable, nullable)
		})
	}
}

func TestResolveCastExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		inner            ast.Expression
		typeName         string
		expectCategory   sqlmodel.SQLTypeCategory
		expectEngineName string
		expectNullable   bool
	}{
		{
			name:             "cast to known type returns that type",
			inner:            &ast.ColumnRefExpression{ColumnName: "id"},
			typeName:         "text",
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
			expectNullable:   false,
		},
		{
			name:             "inner nullability is preserved through cast",
			inner:            &ast.ColumnRefExpression{ColumnName: "email"},
			typeName:         "int4",
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int4",
			expectNullable:   true,
		},
		{
			name:           "cast with empty type name returns unknown category",
			inner:          &ast.ColumnRefExpression{ColumnName: "id"},
			typeName:       "",
			expectCategory: sqlmodel.TypeCategoryUnknown,
			expectNullable: false,
		},
		{
			name:             "nil inner yields nullable",
			inner:            nil,
			typeName:         "text",
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
			expectNullable:   true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			expression := &ast.CastExpression{
				Inner:    testCase.inner,
				TypeName: testCase.typeName,
			}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

			require.NoError(t, err)
			assert.Equal(t, testCase.expectCategory, sqlType.Category)
			if testCase.expectEngineName != "" {
				assert.Equal(t, testCase.expectEngineName, sqlType.EngineName)
			}
			assert.Equal(t, testCase.expectNullable, nullable)
		})
	}
}

func TestACastToANamedTypeTakesTheTypeTheCatalogueNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		typeName        string
		wantCategory    sqlmodel.SQLTypeCategory
		wantEngineName  string
		wantEnumValues  []string
		wantElementType *sqlmodel.SQLTypeCategory
	}{
		{name: "an enum", typeName: "mood", wantCategory: sqlmodel.TypeCategoryEnum, wantEngineName: "mood",
			wantEnumValues: []string{"calm", "tense"}},
		{name: "a composite type", typeName: "pair", wantCategory: sqlmodel.TypeCategoryComposite, wantEngineName: "pair"},
		{name: "a domain takes its base type", typeName: "positive", wantCategory: sqlmodel.TypeCategoryInteger, wantEngineName: "int4"},
		{name: "an array of an enum", typeName: "mood[]", wantCategory: sqlmodel.TypeCategoryArray, wantEngineName: "mood[]",
			wantElementType: new(sqlmodel.TypeCategoryEnum)},
		{name: "a name the catalogue does not hold", typeName: "unheard", wantCategory: sqlmodel.TypeCategoryUnknown,
			wantEngineName: "unheard"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			addNamedTypes(resolver.catalogue)
			resolver.engine = &fakedialect.Engine{
				NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
					if element, isArray := strings.CutSuffix(name, "[]"); isArray {
						return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryArray,
							ElementType: &sqlmodel.SQLType{EngineName: element, Category: sqlmodel.TypeCategoryUnknown}}
					}
					return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryUnknown}
				},
			}
			expression := &ast.CastExpression{Inner: &ast.ColumnRefExpression{ColumnName: "name"}, TypeName: testCase.typeName}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

			require.NoError(t, err)
			assert.False(t, nullable)
			assert.Equal(t, testCase.wantCategory, sqlType.Category)
			assert.Equal(t, testCase.wantEngineName, sqlType.EngineName)
			assert.Equal(t, testCase.wantEnumValues, sqlType.EnumValues)
			if testCase.wantElementType == nil {
				assert.Nil(t, sqlType.ElementType)
				return
			}
			require.NotNil(t, sqlType.ElementType)
			assert.Equal(t, *testCase.wantElementType, sqlType.ElementType.Category)
		})
	}
}

func addNamedTypes(catalogue *sqlmodel.Catalogue) {
	public := catalogue.Schemas["public"]
	public.Enums["mood"] = &sqlmodel.Enum{Name: "mood", Schema: "public", Values: []string{"calm", "tense"}}
	public.CompositeTypes["pair"] = &sqlmodel.CompositeType{Name: "pair", Schema: "public"}
	public.Domains["positive"] = &sqlmodel.Domain{
		Name: "positive", Schema: "public", BaseType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
	}
}

func TestResolveLiteralExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		typeName         string
		expectCategory   sqlmodel.SQLTypeCategory
		expectEngineName string
		expectNullable   bool
	}{
		{
			name:             "typed literal returns the engine-normalised type",
			typeName:         "int4",
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int4",
			expectNullable:   false,
		},
		{
			name:             "text literal returns text type",
			typeName:         "text",
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
			expectNullable:   false,
		},
		{
			name:           "untyped literal returns unknown category",
			typeName:       "",
			expectCategory: sqlmodel.TypeCategoryUnknown,
			expectNullable: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			expression := &ast.LiteralExpression{
				TypeName: testCase.typeName,
			}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

			require.NoError(t, err)
			assert.Equal(t, testCase.expectCategory, sqlType.Category)
			if testCase.expectEngineName != "" {
				assert.Equal(t, testCase.expectEngineName, sqlType.EngineName)
			}
			assert.Equal(t, testCase.expectNullable, nullable)
		})
	}
}

func testBinaryResult(operator string, _, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	switch operator {
	case "||":
		return sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, sqlmodel.FunctionNullableReturnsNullOnNull, true
	case "->>", "#>>":
		return sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, sqlmodel.FunctionNullableCalledOnNull, true
	case "&", "|", "<<", ">>":
		return sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}, sqlmodel.FunctionNullableReturnsNullOnNull, true
	case "?":
		return sqlmodel.SQLType{EngineName: "boolean", Category: sqlmodel.TypeCategoryBoolean}, sqlmodel.FunctionNullableNeverNull, true
	default:
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	}
}

func TestResultNullable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		behaviour       sqlmodel.FunctionNullableBehaviour
		nullingArgument int
		arguments       []bool
		expected        bool
	}{
		{name: "never null ignores nullable arguments", behaviour: sqlmodel.FunctionNullableNeverNull, arguments: []bool{true}, expected: false},
		{name: "null on null with a nullable argument", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull, arguments: []bool{false, true}, expected: true},
		{name: "null on null without a nullable argument", behaviour: sqlmodel.FunctionNullableReturnsNullOnNull, arguments: []bool{false, false}, expected: false},
		{name: "called on null is always nullable", behaviour: sqlmodel.FunctionNullableCalledOnNull, arguments: []bool{false}, expected: true},
		{name: "the nulling argument decides when it is nullable", behaviour: sqlmodel.FunctionNullableNullWhenArgumentNull, nullingArgument: 0, arguments: []bool{true, false}, expected: true},
		{name: "other nullable arguments do not make it nullable", behaviour: sqlmodel.FunctionNullableNullWhenArgumentNull, nullingArgument: 0, arguments: []bool{false, true, true}, expected: false},
		{name: "a missing nulling argument is treated as nullable", behaviour: sqlmodel.FunctionNullableNullWhenArgumentNull, nullingArgument: 3, arguments: []bool{false}, expected: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, resultNullable(testCase.behaviour, testCase.nullingArgument, testCase.arguments))
		})
	}
}

func TestResolveBinaryOpExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		operator         string
		left             ast.Expression
		right            ast.Expression
		expectCategory   sqlmodel.SQLTypeCategory
		expectEngineName string
		expectNullable   bool
		mayBeNull        bool
	}{
		{
			name:             "an operation the engine may answer with NULL is nullable over non-null operands",
			operator:         "-",
			left:             &ast.ColumnRefExpression{ColumnName: "id"},
			right:            &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int4",
			expectNullable:   true,
			mayBeNull:        true,
		},
		{
			name:             "an operator the dialect types may answer with NULL too",
			operator:         "&",
			left:             &ast.ColumnRefExpression{ColumnName: "id"},
			right:            &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int8",
			expectNullable:   true,
			mayBeNull:        true,
		},
		{
			name:             "concatenation returns text type",
			operator:         "||",
			left:             &ast.ColumnRefExpression{ColumnName: "name"},
			right:            &ast.ColumnRefExpression{ColumnName: "name"},
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
			expectNullable:   false,
		},
		{
			name:             "concatenation with nullable operand is nullable",
			operator:         "||",
			left:             &ast.ColumnRefExpression{ColumnName: "name"},
			right:            &ast.ColumnRefExpression{ColumnName: "email"},
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
			expectNullable:   true,
		},
		{
			name:             "arithmetic operator returns common supertype",
			operator:         "+",
			left:             &ast.ColumnRefExpression{ColumnName: "id"},
			right:            &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int4",
			expectNullable:   false,
		},
		{
			name:             "JSON arrow returns text for ->>",
			operator:         "->>",
			left:             &ast.LiteralExpression{TypeName: "text"},
			right:            &ast.LiteralExpression{TypeName: "text"},
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
			expectNullable:   true,
		},
		{
			name:             "JSON path text returns text for #>>",
			operator:         "#>>",
			left:             &ast.LiteralExpression{TypeName: "text"},
			right:            &ast.LiteralExpression{TypeName: "text"},
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
			expectNullable:   true,
		},
		{
			name:             "bitwise AND returns integer",
			operator:         "&",
			left:             &ast.ColumnRefExpression{ColumnName: "id"},
			right:            &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int8",
			expectNullable:   false,
		},
		{
			name:             "bitwise OR returns integer",
			operator:         "|",
			left:             &ast.ColumnRefExpression{ColumnName: "id"},
			right:            &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int8",
			expectNullable:   false,
		},
		{
			name:             "left shift returns integer",
			operator:         "<<",
			left:             &ast.ColumnRefExpression{ColumnName: "id"},
			right:            &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int8",
			expectNullable:   false,
		},
		{
			name:             "an operator the dialect types never null is not nullable",
			operator:         "?",
			left:             &ast.ColumnRefExpression{ColumnName: "email"},
			right:            &ast.ColumnRefExpression{ColumnName: "email"},
			expectCategory:   sqlmodel.TypeCategoryBoolean,
			expectEngineName: "boolean",
			expectNullable:   false,
		},
		{
			name:             "an operator the dialect leaves alone takes the structural default",
			operator:         "-",
			left:             &ast.ColumnRefExpression{ColumnName: "id"},
			right:            &ast.LiteralExpression{TypeName: "int8"},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int4",
			expectNullable:   false,
		},
		{
			name:             "right shift returns integer",
			operator:         ">>",
			left:             &ast.ColumnRefExpression{ColumnName: "id"},
			right:            &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int8",
			expectNullable:   false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			expression := &ast.BinaryOpExpression{
				Left:      testCase.left,
				Right:     testCase.right,
				Operator:  testCase.operator,
				MayBeNull: testCase.mayBeNull,
			}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

			require.NoError(t, err)
			assert.Equal(t, testCase.expectCategory, sqlType.Category)
			assert.Equal(t, testCase.expectEngineName, sqlType.EngineName)
			assert.Equal(t, testCase.expectNullable, nullable)
		})
	}
}

func TestResolveUnaryOpExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		operator       string
		operand        ast.Expression
		expectCategory sqlmodel.SQLTypeCategory
		expectNullable bool
	}{
		{
			name:           "NOT returns boolean and not-nullable",
			operator:       "NOT",
			operand:        &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory: sqlmodel.TypeCategoryBoolean,
			expectNullable: false,
		},
		{
			name:           "lowercase not also returns boolean",
			operator:       "not",
			operand:        &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory: sqlmodel.TypeCategoryBoolean,
			expectNullable: false,
		},
		{
			name:           "negation preserves operand type and nullability",
			operator:       "-",
			operand:        &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory: sqlmodel.TypeCategoryInteger,
			expectNullable: false,
		},
		{
			name:           "negation of nullable operand preserves nullability",
			operator:       "-",
			operand:        &ast.ColumnRefExpression{ColumnName: "email"},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: true,
		},
		{
			name:           "unary plus preserves operand type",
			operator:       "+",
			operand:        &ast.ColumnRefExpression{ColumnName: "id"},
			expectCategory: sqlmodel.TypeCategoryInteger,
			expectNullable: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			expression := &ast.UnaryOpExpression{
				Operator: testCase.operator,
				Operand:  testCase.operand,
			}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

			require.NoError(t, err)
			assert.Equal(t, testCase.expectCategory, sqlType.Category)
			assert.Equal(t, testCase.expectNullable, nullable)
		})
	}
}

func TestResolveCaseWhenExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		branches       []ast.CaseWhenBranch
		elseResult     ast.Expression
		expectCategory sqlmodel.SQLTypeCategory
		expectNullable bool
	}{
		{
			name: "with ELSE returns common supertype of branches and else",
			branches: []ast.CaseWhenBranch{
				{
					Condition: &ast.ComparisonExpression{Operator: "="},
					Result:    &ast.ColumnRefExpression{ColumnName: "name"},
				},
			},
			elseResult:     &ast.ColumnRefExpression{ColumnName: "name"},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: false,
		},
		{
			name: "without ELSE is always nullable",
			branches: []ast.CaseWhenBranch{
				{
					Condition: &ast.ComparisonExpression{Operator: "="},
					Result:    &ast.ColumnRefExpression{ColumnName: "name"},
				},
			},
			elseResult:     nil,
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: true,
		},
		{
			name: "nullable branch result makes result nullable",
			branches: []ast.CaseWhenBranch{
				{
					Condition: &ast.ComparisonExpression{Operator: "="},
					Result:    &ast.ColumnRefExpression{ColumnName: "email"},
				},
			},
			elseResult:     &ast.ColumnRefExpression{ColumnName: "name"},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: true,
		},
		{
			name: "nil branch result makes result nullable",
			branches: []ast.CaseWhenBranch{
				{
					Condition: &ast.ComparisonExpression{Operator: "="},
					Result:    nil,
				},
			},
			elseResult:     &ast.ColumnRefExpression{ColumnName: "name"},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: true,
		},
		{
			name: "nullable ELSE result makes result nullable",
			branches: []ast.CaseWhenBranch{
				{
					Condition: &ast.ComparisonExpression{Operator: "="},
					Result:    &ast.ColumnRefExpression{ColumnName: "name"},
				},
			},
			elseResult:     &ast.ColumnRefExpression{ColumnName: "email"},
			expectCategory: sqlmodel.TypeCategoryText,
			expectNullable: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			expression := &ast.CaseWhenExpression{
				Branches:   testCase.branches,
				ElseResult: testCase.elseResult,
			}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

			require.NoError(t, err)
			assert.Equal(t, testCase.expectCategory, sqlType.Category)
			assert.Equal(t, testCase.expectNullable, nullable)
		})
	}
}

func TestResolveWindowFunctionExpression(t *testing.T) {
	t.Parallel()

	t.Run("nil inner function returns error", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		expression := &ast.WindowFunctionExpression{}
		_, _, err := resolver.resolveWindowFunctionExpression(t.Context(), expression, scope, new(false))
		require.Error(t, err)
		assert.Equal(t, "Q030", ExtractErrorCode(err))
	})
}

func TestResolveScalarSubqueryExpression(t *testing.T) {
	t.Parallel()

	t.Run("inner query with output column resolves to first column type", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		expression := &ast.ScalarSubqueryExpression{
			InnerQuery: &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{
					{Name: "users"},
				},
				OutputColumns: []ast.RawOutputColumn{
					{
						Expression: &ast.ColumnRefExpression{ColumnName: "id"},
					},
				},
			},
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category)

		assert.True(t, nullable)
	})

	t.Run("inner query WITH clause CTE resolves its own columns", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)

		expression := &ast.ScalarSubqueryExpression{
			InnerQuery: &ast.RawQueryAnalysis{
				CTEDefinitions: []ast.RawCTEDefinition{
					{
						Name:       "c",
						FromTables: []ast.TableReference{{Name: "users"}},
						OutputColumns: []ast.RawOutputColumn{
							{Expression: &ast.ColumnRefExpression{ColumnName: "id"}},
						},
					},
				},
				FromTables: []ast.TableReference{{Name: "c"}},
				OutputColumns: []ast.RawOutputColumn{
					{Expression: &ast.ColumnRefExpression{ColumnName: "id"}},
				},
			},
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category,
			"CTE column should resolve to the underlying integer type, not Unknown")
		assert.True(t, nullable)
	})

	t.Run("nil inner query returns unknown type", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		expression := &ast.ScalarSubqueryExpression{}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
		assert.True(t, nullable)
	})

	t.Run("empty output columns returns unknown type", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		expression := &ast.ScalarSubqueryExpression{
			InnerQuery: &ast.RawQueryAnalysis{},
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
		assert.True(t, nullable)
	})
}

func TestResolveArraySubscriptExpression(t *testing.T) {
	t.Parallel()

	t.Run("array with element type returns element type", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		arrayType := sqlmodel.SQLType{
			EngineName:  "int4[]",
			Category:    sqlmodel.TypeCategoryArray,
			ElementType: &sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
		}

		catalogue := resolver.catalogue
		catalogue.Schemas["public"].Tables["arrays"] = fakedialect.NewTable("arrays",
			sqlmodel.Column{
				Name:    "tags",
				SQLType: arrayType,
			},
		)
		scope.AddTable(
			ast.TableReference{Name: "arrays"},
			ast.JoinInner,
			catalogue.Schemas["public"].Tables["arrays"],
		)

		expression := &ast.ArraySubscriptExpression{
			Array: &ast.ColumnRefExpression{ColumnName: "tags"},
			Index: &ast.LiteralExpression{TypeName: "int4"},
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category)
		assert.Equal(t, "int4", sqlType.EngineName)

		assert.True(t, nullable)
	})

	t.Run("non-array type returns the type itself", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		expression := &ast.ArraySubscriptExpression{
			Array: &ast.ColumnRefExpression{ColumnName: "id"},
			Index: &ast.LiteralExpression{TypeName: "int4"},
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category)
		assert.True(t, nullable)
	})
}

func TestResolveLambdaExpression(t *testing.T) {
	t.Parallel()

	t.Run("body type is returned as lambda type", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		expression := &ast.LambdaExpression{
			Parameters: []string{"x"},
			Body:       &ast.ColumnRefExpression{ColumnName: "id"},
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category)
		assert.False(t, nullable)
	})

	t.Run("nil body returns unknown type", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		expression := &ast.LambdaExpression{
			Parameters: []string{"x"},
			Body:       nil,
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
		assert.True(t, nullable)
	})
}

func TestResolveStructFieldAccessExpression(t *testing.T) {
	t.Parallel()

	t.Run("known field returns its type", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		structType := sqlmodel.SQLType{
			EngineName: "my_struct",
			Category:   sqlmodel.TypeCategoryStruct,
			StructFields: []sqlmodel.StructField{
				{Name: "x", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
				{Name: "y", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
			},
		}
		catalogue := resolver.catalogue
		catalogue.Schemas["public"].Tables["structs"] = fakedialect.NewTable("structs",
			sqlmodel.Column{
				Name:    "data",
				SQLType: structType,
			},
		)
		scope.AddTable(
			ast.TableReference{Name: "structs"},
			ast.JoinInner,
			catalogue.Schemas["public"].Tables["structs"],
		)

		expression := &ast.StructFieldAccessExpression{
			Struct:    &ast.ColumnRefExpression{TableAlias: "structs", ColumnName: "data"},
			FieldName: "x",
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category)
		assert.Equal(t, "int4", sqlType.EngineName)
		assert.False(t, nullable)
	})

	t.Run("unknown field returns unknown type", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		structType := sqlmodel.SQLType{
			EngineName: "my_struct",
			Category:   sqlmodel.TypeCategoryStruct,
			StructFields: []sqlmodel.StructField{
				{Name: "x", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
			},
		}
		catalogue := resolver.catalogue
		catalogue.Schemas["public"].Tables["structs2"] = fakedialect.NewTable("structs2",
			sqlmodel.Column{
				Name:    "data",
				SQLType: structType,
			},
		)
		scope.AddTable(
			ast.TableReference{Name: "structs2"},
			ast.JoinInner,
			catalogue.Schemas["public"].Tables["structs2"],
		)

		expression := &ast.StructFieldAccessExpression{
			Struct:    &ast.ColumnRefExpression{TableAlias: "structs2", ColumnName: "data"},
			FieldName: "xx",
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.Error(t, err)
		require.Equal(t, diag.CodeUnknownColumn, ExtractErrorCode(err))
		var resolveError *Error
		require.ErrorAs(t, err, &resolveError)
		require.Equal(t, "x", resolveError.Suggestion)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
		assert.True(t, nullable)
	})

	t.Run("a value with no declared fields is typed unknown without an error", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		catalogue := resolver.catalogue
		catalogue.Schemas["public"].Tables["documents"] = fakedialect.NewTable("documents",
			sqlmodel.Column{Name: "basket", SQLType: sqlmodel.SQLType{EngineName: "JSON", Category: sqlmodel.TypeCategoryJSON}},
		)
		scope.AddTable(ast.TableReference{Name: "documents"}, ast.JoinInner, catalogue.Schemas["public"].Tables["documents"])

		expression := &ast.StructFieldAccessExpression{
			Struct:    &ast.ColumnRefExpression{ColumnName: "basket"},
			FieldName: "fish",
		}

		sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
		assert.True(t, nullable)
	})

	t.Run("an unknown value keeps the unknown column error", func(t *testing.T) {
		t.Parallel()

		resolver, scope := newExpressionTestResolver(t)
		expression := &ast.StructFieldAccessExpression{
			Struct:    &ast.ColumnRefExpression{ColumnName: "baskit"},
			FieldName: "fish",
		}

		_, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

		require.Error(t, err)
		require.Equal(t, diag.CodeUnknownColumn, ExtractErrorCode(err))
		assert.True(t, nullable)
	})
}

func TestResolveStructFieldAccessExpression_IsNullableWhenTheStructOrTheFieldIs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		structNullable bool
		fieldNullable  bool
		want           bool
	}{
		{name: "a non-null field of a non-null struct", structNullable: false, fieldNullable: false, want: false},
		{name: "a nullable field of a non-null struct", structNullable: false, fieldNullable: true, want: true},
		{name: "a non-null field of a nullable struct", structNullable: true, fieldNullable: false, want: true},
		{name: "a nullable field of a nullable struct", structNullable: true, fieldNullable: true, want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			fieldType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4")
			fieldType.Nullable = testCase.fieldNullable
			structType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "point")
			structType.StructFields = []sqlmodel.StructField{{Name: "x", SQLType: fieldType}}
			table := fakedialect.NewTable("points", sqlmodel.Column{Name: "location", SQLType: structType, Nullable: testCase.structNullable})
			scope.AddTable(ast.TableReference{Name: "points"}, ast.JoinInner, table)

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), &ast.StructFieldAccessExpression{
				Struct:    &ast.ColumnRefExpression{ColumnName: "location"},
				FieldName: "x",
			}, scope, new(false))

			require.NoError(t, err)
			require.Equal(t, "int4", sqlType.EngineName)
			require.Equal(t, testCase.want, nullable)
		})
	}
}

func TestCommonSupertype(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		left             sqlmodel.SQLType
		right            sqlmodel.SQLType
		promoteResult    *sqlmodel.SQLType
		canImplicitCast  func(from, to sqlmodel.SQLType) bool
		expectCategory   sqlmodel.SQLTypeCategory
		expectEngineName string
	}{
		{
			name:             "left unknown returns right",
			left:             sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown},
			right:            sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int4",
		},
		{
			name:             "right unknown returns left",
			left:             sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			right:            sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown},
			expectCategory:   sqlmodel.TypeCategoryText,
			expectEngineName: "text",
		},
		{
			name:             "same type and engine name returns left",
			left:             sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
			right:            sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int4",
		},
		{
			name:  "same category different engine name promotes via engine",
			left:  sqlmodel.SQLType{EngineName: "int2", Category: sqlmodel.TypeCategoryInteger},
			right: sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger},
			promoteResult: &sqlmodel.SQLType{
				EngineName: "int8",
				Category:   sqlmodel.TypeCategoryInteger,
			},
			expectCategory:   sqlmodel.TypeCategoryInteger,
			expectEngineName: "int8",
		},
		{
			name:  "different categories with left-to-right implicit cast returns right",
			left:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			right: sqlmodel.SQLType{EngineName: "json", Category: sqlmodel.TypeCategoryJSON},
			canImplicitCast: func(from, to sqlmodel.SQLType) bool {
				return from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryJSON
			},
			expectCategory:   sqlmodel.TypeCategoryJSON,
			expectEngineName: "json",
		},
		{
			name:  "different categories with right-to-left implicit cast returns left",
			left:  sqlmodel.SQLType{EngineName: "json", Category: sqlmodel.TypeCategoryJSON},
			right: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			canImplicitCast: func(from, to sqlmodel.SQLType) bool {
				return from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryJSON
			},
			expectCategory:   sqlmodel.TypeCategoryJSON,
			expectEngineName: "json",
		},
		{
			name:  "numbers of different categories meet where the engine promotes them, whatever the casts",
			left:  sqlmodel.SQLType{EngineName: "float8", Category: sqlmodel.TypeCategoryFloat},
			right: sqlmodel.SQLType{EngineName: "numeric", Category: sqlmodel.TypeCategoryDecimal},
			promoteResult: &sqlmodel.SQLType{
				EngineName: "float8",
				Category:   sqlmodel.TypeCategoryFloat,
			},
			canImplicitCast: func(_, _ sqlmodel.SQLType) bool {
				return true
			},
			expectCategory:   sqlmodel.TypeCategoryFloat,
			expectEngineName: "float8",
		},
		{
			name:  "no implicit cast returns unknown",
			left:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			right: sqlmodel.SQLType{EngineName: "boolean", Category: sqlmodel.TypeCategoryBoolean},
			canImplicitCast: func(_, _ sqlmodel.SQLType) bool {
				return false
			},
			expectCategory: sqlmodel.TypeCategoryUnknown,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{}
			if testCase.promoteResult != nil {
				promoted := *testCase.promoteResult
				engine.PromoteTypeFunc = func(_, _ sqlmodel.SQLType) sqlmodel.SQLType {
					return promoted
				}
			}
			if testCase.canImplicitCast != nil {
				engine.CanImplicitCastFunc = testCase.canImplicitCast
			}

			catalogue := fakedialect.NewCatalogue("public")
			builtins := engine.BuiltinFunctions()
			functionResolver := NewFunctionResolver(builtins, catalogue, engine)
			resolver := NewTypeResolver(catalogue, functionResolver, engine)

			result := resolver.commonSupertype(testCase.left, testCase.right)

			assert.Equal(t, testCase.expectCategory, result.Category)
			if testCase.expectEngineName != "" {
				assert.Equal(t, testCase.expectEngineName, result.EngineName)
			}
		})
	}
}

func TestIsBooleanExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression ast.Expression
		expected   bool
	}{
		{
			name:       "ComparisonExpression is boolean",
			expression: &ast.ComparisonExpression{Operator: "="},
			expected:   true,
		},
		{
			name:       "IsNullExpression is boolean",
			expression: &ast.IsNullExpression{},
			expected:   true,
		},
		{
			name:       "InListExpression is boolean",
			expression: &ast.InListExpression{},
			expected:   true,
		},
		{
			name:       "BetweenExpression is boolean",
			expression: &ast.BetweenExpression{},
			expected:   true,
		},
		{
			name:       "LogicalOpExpression is boolean",
			expression: &ast.LogicalOpExpression{Operator: "AND"},
			expected:   true,
		},
		{
			name:       "ExistsExpression is boolean",
			expression: &ast.ExistsExpression{},
			expected:   true,
		},
		{
			name:       "ColumnRefExpression is not boolean",
			expression: &ast.ColumnRefExpression{ColumnName: "id"},
			expected:   false,
		},
		{
			name:       "FunctionCallExpression is not boolean",
			expression: &ast.FunctionCallExpression{FunctionName: "count"},
			expected:   false,
		},
		{
			name:       "LiteralExpression is not boolean",
			expression: &ast.LiteralExpression{TypeName: "int4"},
			expected:   false,
		},
		{
			name:       "BinaryOpExpression is not boolean",
			expression: &ast.BinaryOpExpression{Operator: "+"},
			expected:   false,
		},
		{
			name:       "UnaryOpExpression is not boolean",
			expression: &ast.UnaryOpExpression{Operator: "-"},
			expected:   false,
		},
		{
			name:       "CaseWhenExpression is not boolean",
			expression: &ast.CaseWhenExpression{},
			expected:   false,
		},
		{
			name:       "CastExpression is not boolean",
			expression: &ast.CastExpression{TypeName: "int4"},
			expected:   false,
		},
		{
			name:       "CoalesceExpression is not boolean",
			expression: &ast.CoalesceExpression{},
			expected:   false,
		},
		{
			name:       "WindowFunctionExpression is not boolean",
			expression: &ast.WindowFunctionExpression{},
			expected:   false,
		},
		{
			name:       "ScalarSubqueryExpression is not boolean",
			expression: &ast.ScalarSubqueryExpression{},
			expected:   false,
		},
		{
			name:       "LambdaExpression is not boolean",
			expression: &ast.LambdaExpression{},
			expected:   false,
		},
		{
			name:       "StructFieldAccessExpression is not boolean",
			expression: &ast.StructFieldAccessExpression{},
			expected:   false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := isBooleanExpression(testCase.expression)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestResolveExpressionType_ReportsAKindTheDialectDoesNotDeclare(t *testing.T) {
	t.Parallel()

	everyFeature := ^ast.ExpressionFeature(0)
	operators := map[string]ast.ExpressionFeature{"||": ast.SQLFeatureStringConcat, "LIKE": ast.SQLFeaturePatternMatch}
	column := &ast.ColumnRefExpression{ColumnName: "name"}
	testCases := []struct {
		expression ast.Expression
		name       string
		wantError  string
		declared   ast.ExpressionFeature
	}{
		{
			name:       "an operator the dialect lists under an undeclared feature",
			expression: &ast.BinaryOpExpression{Operator: "||", Left: column, Right: column},
			declared:   everyFeature &^ ast.SQLFeatureStringConcat,
			wantError:  ast.SQLFeatureStringConcat.String(),
		},
		{
			name:       "a comparison the dialect lists under an undeclared feature",
			expression: &ast.ComparisonExpression{Operator: "LIKE", Left: column, Right: column},
			declared:   everyFeature &^ ast.SQLFeaturePatternMatch,
			wantError:  ast.SQLFeaturePatternMatch.String(),
		},
		{
			name:       "an unlisted operator needs arithmetic",
			expression: &ast.BinaryOpExpression{Operator: "+", Left: column, Right: column},
			declared:   everyFeature &^ ast.SQLFeatureBinaryArithmetic,
			wantError:  ast.SQLFeatureBinaryArithmetic.String(),
		},
		{
			name:       "a lambda in a dialect without lambdas",
			expression: &ast.LambdaExpression{Parameters: []string{"x"}, Body: column},
			declared:   everyFeature &^ ast.SQLFeatureLambda,
			wantError:  ast.SQLFeatureLambda.String(),
		},
		{
			name:       "a declared operator types",
			expression: &ast.BinaryOpExpression{Operator: "||", Left: column, Right: column},
			declared:   everyFeature,
			wantError:  "",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{}
			info := engine.Info()
			info.Expressions = testCase.declared
			info.OperatorFeatures = operators
			engine.InfoFunc = func() dialect.Info { return info }
			catalogue := fakedialect.NewCatalogue("public")
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine)
			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner,
				fakedialect.NewTable("users", sqlmodel.Column{Name: "name", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")}))

			_, _, err := resolver.ResolveExpressionType(t.Context(), testCase.expression, scope, new(false))

			if testCase.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, diag.CodeExpressionTypeError, ExtractErrorCode(err))
			require.Contains(t, err.Error(), testCase.wantError)
		})
	}
}

func TestResolveExpressionType_BooleanExpressions(t *testing.T) {
	t.Parallel()

	booleanExpressions := []struct {
		name       string
		expression ast.Expression
	}{
		{
			name:       "ComparisonExpression resolves to boolean",
			expression: &ast.ComparisonExpression{Operator: "="},
		},
		{
			name:       "IsNullExpression resolves to boolean",
			expression: &ast.IsNullExpression{},
		},
		{
			name:       "InListExpression resolves to boolean",
			expression: &ast.InListExpression{},
		},
		{
			name:       "BetweenExpression resolves to boolean",
			expression: &ast.BetweenExpression{},
		},
		{
			name:       "LogicalOpExpression resolves to boolean",
			expression: &ast.LogicalOpExpression{Operator: "AND"},
		},
		{
			name:       "ExistsExpression resolves to boolean",
			expression: &ast.ExistsExpression{},
		},
	}

	for _, testCase := range booleanExpressions {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			sqlType, _, err := resolver.ResolveExpressionType(t.Context(), testCase.expression, scope, new(false))

			require.NoError(t, err)
			assert.Equal(t, sqlmodel.TypeCategoryBoolean, sqlType.Category)
			assert.Equal(t, "boolean", sqlType.EngineName)
		})
	}
}

func TestResolveExpressionType_UnknownExpressionType(t *testing.T) {
	t.Parallel()

	resolver, scope := newExpressionTestResolver(t)
	expression := &ast.UnknownExpression{}

	sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
	assert.True(t, nullable)
}

func TestResolveExpressionType_UnsupportedFeature(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{
		SupportedExpressionsFunc: func() ast.ExpressionFeature {
			return 0
		},
	}

	catalogue := fakedialect.NewCatalogue("public")
	builtins := engine.BuiltinFunctions()
	functionResolver := NewFunctionResolver(builtins, catalogue, engine)
	resolver := NewTypeResolver(catalogue, functionResolver, engine)

	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	expression := &ast.CaseWhenExpression{
		Branches: []ast.CaseWhenBranch{
			{Result: &ast.LiteralExpression{TypeName: "int4"}},
		},
	}

	_, _, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported expression")
}

func TestResolveFunctionCallExpression_CalledOnNull(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			if name == "text" {
				return sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
			}
			return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryUnknown}
		},
		BuiltinFunctionsFunc: func() *ast.FunctionCatalogue {
			return &ast.FunctionCatalogue{
				Functions: map[string][]*sqlmodel.Function{
					"my_func": {
						{
							Name:              "my_func",
							ReturnType:        sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
							Arguments:         []sqlmodel.FunctionArgument{{Name: "input", Type: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}}},
							NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
							DataAccess:        sqlmodel.DataAccessReadOnly,
						},
					},
				},
			}
		},
	}

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{
			Name:    "name",
			SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
		},
	)

	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(
		ast.TableReference{Name: "users"},
		ast.JoinInner,
		catalogue.Schemas["public"].Tables["users"],
	)

	builtins := engine.BuiltinFunctions()
	functionResolver := NewFunctionResolver(builtins, catalogue, engine)
	resolver := NewTypeResolver(catalogue, functionResolver, engine)

	expression := &ast.FunctionCallExpression{
		FunctionName: "my_func",
		Arguments:    []ast.Expression{&ast.ColumnRefExpression{ColumnName: "name"}},
	}

	sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryText, sqlType.Category)

	assert.True(t, nullable)
}

func TestResolveFunctionCallExpression_NilArgument(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{
		BuiltinFunctionsFunc: func() *ast.FunctionCatalogue {
			return &ast.FunctionCatalogue{
				Functions: map[string][]*sqlmodel.Function{
					"my_func": {
						{
							Name:              "my_func",
							ReturnType:        sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
							Arguments:         []sqlmodel.FunctionArgument{{Name: "input", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown}}},
							NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
							DataAccess:        sqlmodel.DataAccessReadOnly,
						},
					},
				},
			}
		},
	}

	catalogue := fakedialect.NewCatalogue("public")
	builtins := engine.BuiltinFunctions()
	functionResolver := NewFunctionResolver(builtins, catalogue, engine)
	resolver := NewTypeResolver(catalogue, functionResolver, engine)

	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	expression := &ast.FunctionCallExpression{
		FunctionName: "my_func",
		Arguments:    []ast.Expression{nil},
	}

	sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

	require.NoError(t, err)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category)

	assert.True(t, nullable)
}

func TestAScalarSubqueryTakesTheTypeAndNameOfItsColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		column           ast.RawOutputColumn
		inner            []ast.TableReference
		expectedCategory sqlmodel.SQLTypeCategory
		expectedName     string
	}{
		{
			name:             "a plain column",
			column:           ast.RawOutputColumn{Name: "name", TableAlias: "u", ColumnName: "name"},
			inner:            []ast.TableReference{{Name: "users", Alias: "u"}},
			expectedCategory: sqlmodel.TypeCategoryText,
			expectedName:     "name",
		},
		{
			name:             "a column of the enclosing query",
			column:           ast.RawOutputColumn{Name: "id", TableAlias: "users", ColumnName: "id"},
			inner:            []ast.TableReference{{Name: "users", Alias: "other"}},
			expectedCategory: sqlmodel.TypeCategoryInteger,
			expectedName:     "id",
		},
		{
			name:             "a star over one column",
			column:           ast.RawOutputColumn{IsStar: true},
			inner:            nil,
			expectedCategory: sqlmodel.TypeCategoryUnknown,
			expectedName:     "",
		},
		{
			name:             "an aliased expression",
			column:           ast.RawOutputColumn{Name: "label", Expression: &ast.ColumnRefExpression{TableAlias: "u", ColumnName: "email"}},
			inner:            []ast.TableReference{{Name: "users", Alias: "u"}},
			expectedCategory: sqlmodel.TypeCategoryText,
			expectedName:     "label",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)
			expression := &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{
				FromTables:    testCase.inner,
				OutputColumns: []ast.RawOutputColumn{testCase.column},
			}}

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), expression, scope, new(false))

			require.NoError(t, err)
			require.Equal(t, testCase.expectedCategory, sqlType.Category)
			require.True(t, nullable)
			require.Equal(t, testCase.expectedName, inferExpressionName(expression))
		})
	}
}

func TestAConversionTheEngineMayAnswerWithNullIsNullable(t *testing.T) {
	t.Parallel()

	textToDate := func(mayBeNull bool) func(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
		return func(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
			if argument.Type.Category == sqlmodel.TypeCategoryText && parameter.Category == sqlmodel.TypeCategoryTemporal {
				return dialect.ArgumentCoercion{MayBeNull: mayBeNull}, true
			}
			return dialect.ArgumentCoercion{}, false
		}
	}
	testCases := []struct {
		coerce   func(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool)
		implicit bool
		name     string
		inner    ast.Expression
		typeName string
		want     bool
	}{
		{name: "text the engine may fail to read as a date", coerce: textToDate(true), inner: &ast.ColumnRefExpression{ColumnName: "name"}, typeName: "date", want: true},
		{name: "a text literal the engine may fail to read as a date", coerce: textToDate(true), inner: &ast.LiteralExpression{TypeName: "text"}, typeName: "date", want: true},
		{name: "text the engine always reads as a date", coerce: textToDate(false), inner: &ast.ColumnRefExpression{ColumnName: "name"}, typeName: "date", want: false},
		{name: "a value the engine converts implicitly", coerce: textToDate(true), implicit: true, inner: &ast.ColumnRefExpression{ColumnName: "name"}, typeName: "date", want: false},
		{name: "a number the engine does not coerce", coerce: textToDate(true), inner: &ast.ColumnRefExpression{ColumnName: "id"}, typeName: "date", want: false},
		{name: "a value of unknown type", coerce: textToDate(true), inner: &ast.ColumnRefExpression{ColumnName: "shape"}, typeName: "date", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &coercingEngine{
				FunctionResolverEngine: &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{
					NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
						switch name {
						case "date":
							return sqlmodel.SQLType{EngineName: "date", Category: sqlmodel.TypeCategoryTemporal}
						case "text":
							return sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
						default:
							return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryUnknown}
						}
					},
					CanImplicitCastFunc: func(sqlmodel.SQLType, sqlmodel.SQLType) bool { return testCase.implicit },
				}},
				coerce: testCase.coerce,
			}
			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
				sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
				sqlmodel.Column{Name: "name", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
				sqlmodel.Column{Name: "shape", SQLType: sqlmodel.SQLType{EngineName: "geometry", Category: sqlmodel.TypeCategoryUnknown}},
			)
			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, catalogue.Schemas["public"].Tables["users"])
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine)

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), &ast.CastExpression{Inner: testCase.inner, TypeName: testCase.typeName}, scope, new(false))

			require.NoError(t, err)
			require.Equal(t, sqlmodel.TypeCategoryTemporal, sqlType.Category)
			require.Equal(t, testCase.want, nullable)
		})
	}
}

func TestResolveStructExpressionBuildsTheStructItsNamesAndValuesGive(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	text := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	mayBeNull := func(sqlType sqlmodel.SQLType) sqlmodel.SQLType {
		sqlType.Nullable = true
		return sqlType
	}
	structOf := func(fields ...sqlmodel.StructField) sqlmodel.SQLType {
		built := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
		built.StructFields = fields
		return built
	}
	testCases := []struct {
		expression *ast.StructExpression
		name       string
		want       sqlmodel.SQLType
	}{
		{
			name: "a field for each name, in order, each of which may hold NULL",
			expression: &ast.StructExpression{
				Names:  []string{"id", "label"},
				Values: []ast.Expression{column("id"), &ast.LiteralExpression{TypeName: "text"}},
			},
			want: structOf(
				sqlmodel.StructField{Name: "id", SQLType: mayBeNull(integer), Positional: false},
				sqlmodel.StructField{Name: "label", SQLType: mayBeNull(text), Positional: false},
			),
		},
		{
			name:       "a field whose value may be NULL",
			expression: &ast.StructExpression{Names: []string{"email"}, Values: []ast.Expression{column("email")}},
			want:       structOf(sqlmodel.StructField{Name: "email", SQLType: mayBeNull(text), Positional: false}),
		},
		{
			name: "a struct held in a field of another",
			expression: &ast.StructExpression{
				Names:  []string{"outer"},
				Values: []ast.Expression{&ast.StructExpression{Names: []string{"id"}, Values: []ast.Expression{column("id")}}},
			},
			want: structOf(sqlmodel.StructField{
				Name:       "outer",
				SQLType:    mayBeNull(structOf(sqlmodel.StructField{Name: "id", SQLType: mayBeNull(integer), Positional: false})),
				Positional: false,
			}),
		},
		{
			name:       "a field whose value cannot be typed",
			expression: &ast.StructExpression{Names: []string{"missing"}, Values: []ast.Expression{column("nme")}},
			want: structOf(sqlmodel.StructField{
				Name:       "missing",
				SQLType:    mayBeNull(sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")),
				Positional: false,
			}),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)

			sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), testCase.expression, scope, new(false))

			require.NoError(t, err)
			require.Equal(t, testCase.want, sqlType)
			require.False(t, nullable, "a struct constructor never gives NULL")
		})
	}
}

func TestResolveExpressionTypeReturnsTheInterruptionThatStoppedTheDialect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		expression ast.Expression
		wantErr    error
		name       string
	}{
		{name: "a call returns the interruption", expression: call("probe"), wantErr: context.Canceled},
		{
			name:       "a struct constructor, which types its values leniently, reports nothing of it",
			expression: &ast.StructExpression{Names: []string{"probe"}, Values: []ast.Expression{call("probe")}},
			wantErr:    nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: func(ctx context.Context, _ string, _ []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				cancel()
				return nil, context.Cause(ctx)
			}}
			catalogue := fakedialect.NewCatalogue("public")
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, engine), engine)
			dataModifying := false

			_, _, err := resolver.ResolveExpressionType(ctx, testCase.expression, setupTypeResolverScope(), &dataModifying)

			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, dataModifying, "an interrupted call is not taken to modify data")
			require.Empty(t, resolver.takeNotices(), "an interruption is no problem with the query")
		})
	}
}

func coalesceOfColumns(names ...string) []ast.RawOutputColumn {
	arguments := make([]ast.Expression, 0, len(names))
	for _, name := range names {
		arguments = append(arguments, &ast.ColumnRefExpression{ColumnName: name})
	}
	return []ast.RawOutputColumn{{Name: "picked", Expression: &ast.CoalesceExpression{Arguments: arguments}}}
}

func TestOperandProblemsAreReportedOnceEach(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		want  []string
		names []string
	}{
		{name: "one unknown column", names: []string{"missing"}, want: []string{`unknown column "missing"`}},
		{name: "the same unknown column three times", names: []string{"missing", "missing", "missing"},
			want: []string{`unknown column "missing"`}},
		{name: "two unknown columns among known ones", names: []string{"first", "id", "second", "first", "name"},
			want: []string{`unknown column "first"`, `unknown column "second"`}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newExpressionTestResolver(t)

			projection := resolver.ResolveProjection(t.Context(), coalesceOfColumns(testCase.names...), nil, scope)

			messages := make([]string, 0, len(projection.Problems))
			for _, problem := range projection.Problems {
				messages = append(messages, problem.Message)
			}
			require.Equal(t, testCase.want, messages)
		})
	}
}

func TestOperandProblemsAreCollectedInLinearTime(t *testing.T) {
	t.Parallel()

	resolveColumns := func(count int) func() {
		names := make([]string, 0, count)
		for index := range count {
			names = append(names, fmt.Sprintf("missing_%d", index))
		}
		columns := coalesceOfColumns(names...)
		resolver, scope := newExpressionTestResolver(t)
		return func() {
			projection := resolver.ResolveProjection(t.Context(), columns, nil, scope)
			require.Len(t, projection.Problems, count)
		}
	}

	scaling.Linear(t, resolveColumns(2_000), resolveColumns(8_000))
}

func newIntMappingResolver() *TypeResolver {
	engine := &fakedialect.Engine{
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			if name == "int" {
				return sqlmodel.SQLType{EngineName: "int", Category: sqlmodel.TypeCategoryInteger}
			}
			return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryUnknown}
		},
	}
	return newTestTypeResolverWithEngine(engine)
}

func buildUnaryChain(depth int) ast.Expression {
	var expression ast.Expression = &ast.LiteralExpression{TypeName: "int"}
	for range depth {
		expression = &ast.UnaryOpExpression{Operator: "-", Operand: expression}
	}
	return expression
}

func TestResolveExpressionTypeNestingIsBounded(t *testing.T) {
	t.Parallel()

	const depth = 100_000
	expression := buildUnaryChain(depth)

	resolver := newIntMappingResolver()
	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)

	var sqlType sqlmodel.SQLType
	require.NotPanics(t, func() {
		sqlType, _, _ = resolver.ResolveExpressionType(t.Context(), expression, scope, nil)
	}, "a pathologically deep expression must be bounded, not overflow the stack")

	assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category,
		"a chain past the depth guard must short-circuit to Unknown")
	assert.Equal(t, 0, resolver.expressionDepth,
		"the depth counter must be balanced back to zero after resolution")
}

func TestResolveExpressionTypeNestingBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		depth        int
		wantCategory sqlmodel.SQLTypeCategory
	}{
		{
			name:         "just below the guard resolves the leaf",
			depth:        MaxExpressionResolveDepth - 1,
			wantCategory: sqlmodel.TypeCategoryInteger,
		},
		{
			name:         "exactly at the guard short-circuits to Unknown",
			depth:        MaxExpressionResolveDepth,
			wantCategory: sqlmodel.TypeCategoryUnknown,
		},
		{
			name:         "just above the guard short-circuits to Unknown",
			depth:        MaxExpressionResolveDepth + 1,
			wantCategory: sqlmodel.TypeCategoryUnknown,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newIntMappingResolver()
			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			expression := buildUnaryChain(testCase.depth)

			var sqlType sqlmodel.SQLType
			require.NotPanics(t, func() {
				sqlType, _, _ = resolver.ResolveExpressionType(t.Context(), expression, scope, nil)
			})

			assert.Equal(t, testCase.wantCategory, sqlType.Category)
			assert.Equal(t, 0, resolver.expressionDepth,
				"the depth counter must be balanced back to zero after resolution")
		})
	}
}

func reduceChain(depth int) ast.Expression {
	var expression ast.Expression = &ast.LiteralExpression{TypeName: "int"}
	for range depth {
		inner := expression
		lambda := &ast.LambdaExpression{
			Parameters: []string{"accumulator", "element"},
			Body:       &ast.ColumnRefExpression{ColumnName: "accumulator"},
			Bindings:   []ast.Expression{&ast.ArraySubscriptExpression{Array: inner, Index: &ast.LiteralExpression{TypeName: "int"}}},
		}
		expression = &ast.FunctionCallExpression{FunctionName: "list_reduce", Arguments: []ast.Expression{inner, lambda}}
	}
	return expression
}

func TestNestedLambdaBindingsAreTypedInLinearTime(t *testing.T) {
	t.Parallel()

	resolveChain := func(depth int) func() {
		expression := reduceChain(depth)
		return func() {
			resolver := newIntMappingResolver()
			_, _, _ = resolver.ResolveExpressionType(t.Context(), expression, NewRootScopeChain(ScopeKindQuery, testScopeRules), nil)
		}
	}

	scaling.Linear(t, resolveChain(4), resolveChain(16))
}

func TestABoundLambdaParameterTakesTheTypeOfTheArgumentItStandsFor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		argument ast.Expression
		want     sqlmodel.SQLTypeCategory
	}{
		{name: "an argument of a known type", argument: &ast.LiteralExpression{TypeName: "int"}, want: sqlmodel.TypeCategoryInteger},
		{name: "an argument passed by name", want: sqlmodel.TypeCategoryInteger,
			argument: &ast.NamedArgumentExpression{Name: "list", Value: &ast.LiteralExpression{TypeName: "int"}}},
		{name: "an argument of an unknown type", argument: &ast.LiteralExpression{TypeName: "mystery"}, want: sqlmodel.TypeCategoryUnknown},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			bound := testCase.argument
			if named, isNamed := bound.(*ast.NamedArgumentExpression); isNamed {
				bound = named.Value
			}
			lambda := &ast.LambdaExpression{
				Parameters: []string{"accumulator"},
				Body:       &ast.ColumnRefExpression{ColumnName: "accumulator"},
				Bindings:   []ast.Expression{bound},
			}
			resolver := newIntMappingResolver()
			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)

			arguments := resolver.resolveCallArguments(t.Context(), []ast.Expression{testCase.argument, lambda}, scope, nil)

			require.Len(t, arguments, 2)
			require.Equal(t, testCase.want, arguments[1].sqlType.Category)
			require.Nil(t, resolver.typedArguments, "the arguments are forgotten once the call is typed")
		})
	}
}

func TestResolveCustomTypeFollowsAnArrayTypeToTheTypeDepthLimit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		levels       int
		wantResolved bool
	}{
		{name: "an enum inside arrays within the limit", levels: sqlmodel.MaxTypeDepth - 1, wantResolved: true},
		{name: "an enum inside arrays past the limit", levels: sqlmodel.MaxTypeDepth, wantResolved: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTestTypeResolver()
			resolver.catalogue.Schemas["public"].Enums = map[string]*sqlmodel.Enum{"mood": {Name: "mood", Values: []string{"calm"}}}
			sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "mood")
			for range testCase.levels {
				element := new(sqlType)
				sqlType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "array")
				sqlType.ElementType = element
			}

			resolved := resolver.resolveCustomType(sqlType)

			innermost := resolved
			for innermost.ElementType != nil {
				innermost = *innermost.ElementType
			}
			require.Equal(t, testCase.wantResolved, innermost.Category == sqlmodel.TypeCategoryEnum)
		})
	}
}
