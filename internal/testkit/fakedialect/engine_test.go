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

package fakedialect_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type callerKey struct{}

var errResolve = errors.New("the resolver failed")

func TestResolverFakesHandTheCallersContextToTheirFunctionFields(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		call      func(ctx context.Context, seen *context.Context) (any, error)
		wantValue any
		wantError error
		name      string
	}{
		{
			name:      "a function resolver",
			wantValue: &ast.FunctionResolution{},
			wantError: nil,
			call: func(ctx context.Context, seen *context.Context) (any, error) {
				engine := &fakedialect.FunctionResolverEngine{
					Engine: &fakedialect.Engine{},
					ResolveFunc: func(ctx context.Context, _ string, _ []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
						*seen = ctx
						return &ast.FunctionResolution{}, nil
					},
				}
				return engine.ResolveFunctionCall(ctx, nil, "length", "", nil)
			},
		},
		{
			name:      "a catalogue function resolver that fails",
			wantValue: []ast.ScopedColumn(nil),
			wantError: errResolve,
			call: func(ctx context.Context, seen *context.Context) (any, error) {
				engine := &fakedialect.CatalogueResolverEngine{
					Engine: &fakedialect.Engine{},
					FromCatalogueFunc: func(ctx context.Context, _ *sqlmodel.Catalogue, _ string) ([]ast.ScopedColumn, error) {
						*seen = ctx
						return nil, errResolve
					},
				}
				return engine.TableValuedFunctionColumnsFromCatalogue(ctx, nil, "my_udf")
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.WithValue(t.Context(), callerKey{}, testCase.name)
			var seen context.Context
			value, err := testCase.call(ctx, &seen)

			require.ErrorIs(t, err, testCase.wantError)
			require.Equal(t, testCase.wantValue, value)
			require.NotNil(t, seen)
			require.Equal(t, testCase.name, seen.Value(callerKey{}))
		})
	}
}

func TestResolverFakesWithoutAFunctionFieldGiveNothing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		call func(ctx context.Context) (any, error)
		want any
		name string
	}{
		{
			name: "a function resolver",
			want: (*ast.FunctionResolution)(nil),
			call: func(ctx context.Context) (any, error) {
				engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: nil}
				return engine.ResolveFunctionCall(ctx, nil, "length", "", nil)
			},
		},
		{
			name: "a catalogue function resolver",
			want: []ast.ScopedColumn(nil),
			call: func(ctx context.Context) (any, error) {
				engine := &fakedialect.CatalogueResolverEngine{Engine: &fakedialect.Engine{}, FromCatalogueFunc: nil}
				return engine.TableValuedFunctionColumnsFromCatalogue(ctx, nil, "my_udf")
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, err := testCase.call(t.Context())

			require.NoError(t, err)
			require.Equal(t, testCase.want, value)
		})
	}
}

func TestEngineLetsAPanicInAFunctionFieldReachTheCaller(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		call func(ctx context.Context) error
		name string
	}{
		{
			name: "analyse query",
			call: func(ctx context.Context) error {
				engine := &fakedialect.Engine{
					AnalyseQueryFunc: func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
						panic("the fake failed")
					},
				}
				_, err := engine.AnalyseQuery(ctx, nil, ast.ParsedStatement{})
				return err
			},
		},
		{
			name: "resolve function call",
			call: func(ctx context.Context) error {
				engine := &fakedialect.FunctionResolverEngine{
					Engine: &fakedialect.Engine{},
					ResolveFunc: func(context.Context, string, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
						panic("the fake failed")
					},
				}
				_, err := engine.ResolveFunctionCall(ctx, nil, "length", "", nil)
				return err
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.PanicsWithValue(t, "the fake failed", func() {
				_ = testCase.call(t.Context())
			})
		})
	}
}

func TestEngineWithoutFunctionFieldsGivesItsDocumentedDefaults(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{}
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	testCases := []struct {
		name     string
		call     func(ctx context.Context) any
		expected any
	}{
		{name: "ParseStatements parses nothing", call: func(ctx context.Context) any {
			statements, err := engine.ParseStatements(ctx, "SELECT 1")
			return []any{statements, err}
		}, expected: []any{[]ast.ParsedStatement(nil), nil}},
		{name: "ApplyDDL mutates nothing", call: func(ctx context.Context) any {
			mutation, err := engine.ApplyDDL(ctx, ast.ParsedStatement{})
			return []any{mutation, err}
		}, expected: []any{(*ast.CatalogueMutation)(nil), nil}},
		{name: "AnalyseQuery analyses nothing", call: func(ctx context.Context) any {
			analysis, err := engine.AnalyseQuery(ctx, nil, ast.ParsedStatement{})
			return []any{analysis, err}
		}, expected: []any{(*ast.RawQueryAnalysis)(nil), nil}},
		{name: "NormaliseTypeName keeps the name as an unknown type", call: func(context.Context) any {
			return engine.NormaliseTypeName("widget", 3)
		}, expected: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "widget")},
		{name: "PromoteType keeps the left type", call: func(context.Context) any {
			return engine.PromoteType(text, sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "boolean"))
		}, expected: text},
		{name: "CanImplicitCast refuses", call: func(context.Context) any {
			return engine.CanImplicitCast(text, text)
		}, expected: false},
		{name: "BinaryResult handles no operator", call: func(context.Context) any {
			_, _, handled := engine.BinaryResult("+", text, text)
			return handled
		}, expected: false},
		{name: "UnaryResult types NOT as a boolean", call: func(context.Context) any {
			result, nullable, handled := engine.UnaryResult("not", text)
			return []any{result, nullable, handled}
		}, expected: []any{fakedialect.DefaultCoreTypes().Boolean, sqlmodel.FunctionNullableReturnsNullOnNull, true}},
		{name: "UnaryResult handles no other operator", call: func(context.Context) any {
			_, _, handled := engine.UnaryResult("-", text)
			return handled
		}, expected: false},
		{name: "CoreTypes are the defaults", call: func(context.Context) any {
			return engine.CoreTypes()
		}, expected: fakedialect.DefaultCoreTypes()},
		{name: "BuiltinFunctions are none", call: func(context.Context) any {
			return len(engine.BuiltinFunctions().Functions)
		}, expected: 0},
		{name: "BuiltinTypes are none", call: func(context.Context) any {
			return len(engine.BuiltinTypes().Types)
		}, expected: 0},
		{name: "TableValuedFunctionColumns are none", call: func(context.Context) any {
			return engine.TableValuedFunctionColumns("generate_series")
		}, expected: []ast.ScopedColumn(nil)},
		{name: "Info names the mock dialect", call: func(context.Context) any {
			info := engine.Info()
			return []any{info.Name, info.Family, info.Comments.LinePrefix, info.SchemaSearchPath, info.AsyncMutations}
		}, expected: []any{"mock", "mock", "--", []string{"public"}, false}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, testCase.call(t.Context()))
		})
	}
}

func TestEngineInfoTakesEachFunctionFieldOverDefaults(t *testing.T) {
	t.Parallel()

	comments := ast.CommentStyle{LinePrefix: "#"}
	testCases := []struct {
		name     string
		engine   *fakedialect.Engine
		inspect  func(info dialect.Info) any
		expected any
	}{
		{name: "the name", engine: &fakedialect.Engine{DialectFunc: func() string { return "martian" }},
			inspect: func(info dialect.Info) any { return info.Name }, expected: "martian"},
		{name: "the comment style", engine: &fakedialect.Engine{CommentStyleFunc: func() ast.CommentStyle { return comments }},
			inspect: func(info dialect.Info) any { return info.Comments }, expected: comments},
		{name: "the expressions", engine: &fakedialect.Engine{SupportedExpressionsFunc: func() ast.ExpressionFeature { return 0 }},
			inspect: func(info dialect.Info) any { return info.Expressions }, expected: ast.ExpressionFeature(0)},
		{name: "the lexical rules", engine: &fakedialect.Engine{LexicalFunc: func() sqllex.Rules { return sqllex.Rules{} }},
			inspect: func(info dialect.Info) any { return info.Lexical.Parameters.DollarNumbers }, expected: false},
		{name: "the placeholders", engine: &fakedialect.Engine{PlaceholdersFunc: func() dialect.Placeholders { return dialect.Placeholders{} }},
			inspect: func(info dialect.Info) any { return info.Placeholders.Execution.Prefix }, expected: ""},
		{name: "the search path", engine: &fakedialect.Engine{SchemaSearchPathFunc: func() []string { return []string{"app"} }},
			inspect: func(info dialect.Info) any { return info.SchemaSearchPath }, expected: []string{"app"}},
		{name: "the returning statements", engine: &fakedialect.Engine{ReturningFunc: func() ast.DataChange { return ast.DataChangeInsert }},
			inspect: func(info dialect.Info) any { return info.Returning }, expected: ast.DataChangeInsert},
		{name: "asynchronous mutations", engine: &fakedialect.Engine{SupportsAsyncMutationsFunc: func() bool { return true }},
			inspect: func(info dialect.Info) any { return info.AsyncMutations }, expected: true},
		{name: "a whole Info", engine: &fakedialect.Engine{InfoFunc: func() dialect.Info { return dialect.Info{Name: "whole"} }},
			inspect: func(info dialect.Info) any { return []any{info.Name, info.Family} }, expected: []any{"whole", ""}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, testCase.inspect(testCase.engine.Info()))
		})
	}
}

func TestFakeConstructorsBuildEmptySchemaObjects(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{}
	files, err := emitter.Emit(t.Context(), &emit.Request{})
	require.NoError(t, err)
	require.Nil(t, files)
	require.Len(t, emitter.Requests, 1)

	catalogue := fakedialect.NewCatalogue("app")
	require.Equal(t, "app", catalogue.DefaultSchema)
	require.Equal(t, []string{"app"}, catalogue.SearchPath)
	require.Contains(t, catalogue.Schemas, "app")
	require.Empty(t, catalogue.Schemas["app"].Tables)

	table := fakedialect.NewTable("items", sqlmodel.Column{Name: "id"})
	require.Equal(t, "items", table.Name)
	require.Len(t, table.Columns, 1)
}
