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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type lambdaCallRecorder struct {
	arguments map[string][]sqlmodel.SQLType
	mu        sync.Mutex
}

func (recorder *lambdaCallRecorder) resolve(_ context.Context, name string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.arguments[name] = append([]sqlmodel.SQLType(nil), argumentTypes...)
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
	}, nil
}

func (recorder *lambdaCallRecorder) argument(name string, index int) sqlmodel.SQLType {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.arguments[name][index]
}

func listOf(element sqlmodel.SQLType) sqlmodel.SQLType {
	list := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, element.EngineName+"[]")
	list.ElementType = &element
	return list
}

func newLambdaTestResolver(t *testing.T) (*TypeResolver, *ScopeChain, *lambdaCallRecorder) {
	t.Helper()

	recorder := &lambdaCallRecorder{arguments: make(map[string][]sqlmodel.SQLType)}
	engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: recorder.resolve}
	integer := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4")
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	nullableInteger := integer
	nullableInteger.Nullable = true
	berth := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "berth")
	berth.StructFields = []sqlmodel.StructField{{Name: "pontoon", SQLType: text}}
	table := fakedialect.NewTable("boats",
		sqlmodel.Column{Name: "id", SQLType: integer},
		sqlmodel.Column{Name: "name", SQLType: text},
		sqlmodel.Column{Name: "crew", SQLType: listOf(integer)},
		sqlmodel.Column{Name: "ratings", SQLType: listOf(nullableInteger)},
		sqlmodel.Column{Name: "tags", SQLType: listOf(text)},
		sqlmodel.Column{Name: "berths", SQLType: listOf(berth)},
		sqlmodel.Column{Name: "document", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json")},
	)
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["boats"] = table
	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(ast.TableReference{Name: "boats"}, ast.JoinInner, table)
	resolver := NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine)
	return resolver, scope, recorder
}

func column(name string) *ast.ColumnRefExpression {
	return &ast.ColumnRefExpression{ColumnName: name}
}

func lambda(body ast.Expression, parameters ...string) *ast.LambdaExpression {
	return &ast.LambdaExpression{Parameters: parameters, Body: body}
}

func TestLambdaParametersBindToTheElementsOfTheCallsLists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		arguments    []ast.Expression
		wantCategory sqlmodel.SQLTypeCategory
		wantEngine   string
		lambdaIn     string
		lambdaAt     int
	}{
		{
			name:         "a lambda after its list reads an element",
			arguments:    []ast.Expression{column("crew"), lambda(column("x"), "x")},
			lambdaIn:     "apply",
			lambdaAt:     1,
			wantCategory: sqlmodel.TypeCategoryInteger,
			wantEngine:   "int4",
		},
		{
			name:         "a lambda before its list reads an element",
			arguments:    []ast.Expression{lambda(column("x"), "x"), column("tags")},
			lambdaIn:     "apply",
			lambdaAt:     0,
			wantCategory: sqlmodel.TypeCategoryText,
			wantEngine:   "text",
		},
		{
			name:         "each parameter reads an element of the list in its place",
			arguments:    []ast.Expression{lambda(column("y"), "x", "y"), column("crew"), column("tags")},
			lambdaIn:     "apply",
			lambdaAt:     0,
			wantCategory: sqlmodel.TypeCategoryText,
			wantEngine:   "text",
		},
		{
			name:         "a parameter with no list of its own is of unknown type",
			arguments:    []ast.Expression{column("crew"), lambda(column("position"), "x", "position")},
			lambdaIn:     "apply",
			lambdaAt:     1,
			wantCategory: sqlmodel.TypeCategoryUnknown,
			wantEngine:   "",
		},
		{
			name:         "a parameter hides a column of the same name",
			arguments:    []ast.Expression{column("crew"), lambda(column("name"), "name")},
			lambdaIn:     "apply",
			lambdaAt:     1,
			wantCategory: sqlmodel.TypeCategoryInteger,
			wantEngine:   "int4",
		},
		{
			name:         "a body still reads the query's columns",
			arguments:    []ast.Expression{column("crew"), lambda(column("name"), "x")},
			lambdaIn:     "apply",
			lambdaAt:     1,
			wantCategory: sqlmodel.TypeCategoryText,
			wantEngine:   "text",
		},
		{
			name:         "a parameter's field reads the field of the element",
			arguments:    []ast.Expression{column("berths"), lambda(&ast.ColumnRefExpression{TableAlias: "b", ColumnName: "pontoon"}, "b")},
			lambdaIn:     "apply",
			lambdaAt:     1,
			wantCategory: sqlmodel.TypeCategoryText,
			wantEngine:   "text",
		},
		{
			name:         "a field of a parameter whose type declares no fields is of unknown type",
			arguments:    []ast.Expression{column("crew"), lambda(&ast.ColumnRefExpression{TableAlias: "x", ColumnName: "pontoon"}, "x")},
			lambdaIn:     "apply",
			lambdaAt:     1,
			wantCategory: sqlmodel.TypeCategoryUnknown,
			wantEngine:   "",
		},
		{
			name:         "a nested lambda reads the parameters of the lambda around it",
			arguments:    []ast.Expression{column("crew"), lambda(&ast.FunctionCallExpression{FunctionName: "first", Arguments: []ast.Expression{lambda(column("x"), "y")}}, "x")},
			lambdaIn:     "first",
			lambdaAt:     0,
			wantCategory: sqlmodel.TypeCategoryInteger,
			wantEngine:   "int4",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope, recorder := newLambdaTestResolver(t)
			call := &ast.FunctionCallExpression{FunctionName: "apply", Arguments: testCase.arguments}

			_, _, err := resolver.resolveExpressionType(t.Context(), call, scope, new(false))

			require.NoError(t, err)
			require.Empty(t, resolver.takeNotices())
			lambdaType := recorder.argument(testCase.lambdaIn, testCase.lambdaAt)
			require.Equal(t, testCase.wantCategory, lambdaType.Category)
			require.Equal(t, testCase.wantEngine, lambdaType.EngineName)
		})
	}
}

func TestLambdaParameterIsNullableWhenItsElementIs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		list string
		want bool
	}{
		{name: "an element that is never NULL", list: "crew", want: false},
		{name: "an element that may be NULL", list: "ratings", want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope, _ := newLambdaTestResolver(t)
			call := &ast.FunctionCallExpression{FunctionName: "apply", Arguments: []ast.Expression{column(testCase.list), lambda(column("x"), "x")}}

			_, nullable, err := resolver.ResolveExpressionType(t.Context(), call, scope, new(false))

			require.NoError(t, err)
			require.Equal(t, testCase.want, nullable)
		})
	}
}

func TestLambdaOutsideACallBindsItsParametersToUnknownValues(t *testing.T) {
	t.Parallel()

	resolver, scope, _ := newLambdaTestResolver(t)

	sqlType, nullable, err := resolver.ResolveExpressionType(t.Context(), lambda(column("x"), "x"), scope, new(false))

	require.NoError(t, err)
	require.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
	require.True(t, nullable)
}

func TestLambdaParametersAreForgottenAfterTheLambda(t *testing.T) {
	t.Parallel()

	resolver, scope, _ := newLambdaTestResolver(t)
	call := &ast.FunctionCallExpression{FunctionName: "apply", Arguments: []ast.Expression{column("crew"), lambda(column("x"), "x")}}
	_, _, err := resolver.ResolveExpressionType(t.Context(), call, scope, new(false))
	require.NoError(t, err)

	_, _, err = resolver.ResolveExpressionType(t.Context(), column("x"), scope, new(false))

	require.Error(t, err)
	require.Equal(t, "Q001", ExtractErrorCode(err))
}

func TestAListOfWhatALambdaReturnsHoldsNullWhereTheBodyMayBeNull(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body ast.Expression
		want bool
	}{
		{name: "a body that is never NULL", body: column("x"), want: false},
		{name: "a body reading a column that may be NULL", body: column("note"), want: true},
		{name: "a body reading an element that may be NULL", body: column("y"), want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: func(_ context.Context, _ string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				return &ast.FunctionResolution{ReturnType: listOf(argumentTypes[0]), DataAccess: sqlmodel.DataAccessReadOnly}, nil
			}}
			integer := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4")
			nullableInteger := integer
			nullableInteger.Nullable = true
			table := fakedialect.NewTable("boats",
				sqlmodel.Column{Name: "note", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: true},
				sqlmodel.Column{Name: "crew", SQLType: listOf(integer)},
				sqlmodel.Column{Name: "ratings", SQLType: listOf(nullableInteger)},
			)
			catalogue := fakedialect.NewCatalogue("public")
			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			scope.AddTable(ast.TableReference{Name: "boats"}, ast.JoinInner, table)
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine)
			call := &ast.FunctionCallExpression{FunctionName: "map_each", Arguments: []ast.Expression{lambda(testCase.body, "x", "y"), column("crew"), column("ratings")}}

			sqlType, _, err := resolver.ResolveExpressionType(t.Context(), call, scope, new(false))

			require.NoError(t, err)
			require.NotNil(t, sqlType.ElementType)
			require.Equal(t, testCase.want, sqlType.ElementType.Nullable)
		})
	}
}

func boundLambda(body ast.Expression, parameters []string, bindings ...ast.Expression) *ast.LambdaExpression {
	return &ast.LambdaExpression{Parameters: parameters, Body: body, Bindings: bindings}
}

func TestLambdaParametersTheDialectBindsStandForTheirBindings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		lambda       *ast.LambdaExpression
		wantCategory sqlmodel.SQLTypeCategory
		wantEngine   string
	}{
		{
			name:         "a bound parameter takes its binding's type",
			lambda:       boundLambda(column("position"), []string{"x", "position"}, nil, column("id")),
			wantCategory: sqlmodel.TypeCategoryInteger,
			wantEngine:   "int4",
		},
		{
			name:         "an unbound parameter after a bound one takes the first list's element",
			lambda:       boundLambda(column("x"), []string{"total", "x"}, column("name")),
			wantCategory: sqlmodel.TypeCategoryText,
			wantEngine:   "text",
		},
		{
			name:         "a parameter bound to an element of a list takes the element's type",
			lambda:       boundLambda(column("y"), []string{"x", "y"}, nil, &ast.ArraySubscriptExpression{Array: column("tags"), Index: column("id")}),
			wantCategory: sqlmodel.TypeCategoryText,
			wantEngine:   "text",
		},
		{
			name:         "a nil binding leaves the parameter to the call's lists",
			lambda:       boundLambda(column("x"), []string{"x"}, nil),
			wantCategory: sqlmodel.TypeCategoryText,
			wantEngine:   "text",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope, recorder := newLambdaTestResolver(t)
			call := &ast.FunctionCallExpression{FunctionName: "apply", Arguments: []ast.Expression{column("tags"), testCase.lambda}}

			_, _, err := resolver.resolveExpressionType(t.Context(), call, scope, new(false))

			require.NoError(t, err)
			require.Empty(t, resolver.takeNotices())
			lambdaType := recorder.argument("apply", 1)
			require.Equal(t, testCase.wantCategory, lambdaType.Category)
			require.Equal(t, testCase.wantEngine, lambdaType.EngineName)
		})
	}
}
