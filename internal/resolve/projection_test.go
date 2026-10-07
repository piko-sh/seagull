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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type expectedProblem struct {
	code       string
	suggestion string
	span       Span
}

type rowValueEngine struct {
	*fakedialect.Engine
	rowValueTyper
}

type knownElementsEngine struct {
	*fakedialect.Engine
}

var _ dialect.FunctionResolverPort = knownElementsEngine{}

func (knownElementsEngine) ResolveFunctionCall(_ context.Context, _ *sqlmodel.Catalogue, name string, _ string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if name != "gather" || len(argumentTypes) != 1 {
		return nil, nil
	}
	return &ast.FunctionResolution{ReturnType: sqlmodel.SQLType{EngineName: "int8[]", Category: sqlmodel.TypeCategoryArray, ElementType: new(argumentTypes[0])},
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull, DataAccess: sqlmodel.DataAccessReadOnly, IsAggregate: true, ElementsKnown: true}, nil
}

func newProjectionResolver() *TypeResolver {
	engine := &fakedialect.Engine{}
	catalogue := fakedialect.NewCatalogue("public")
	integer := sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}
	builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{
		"count": {sqlmodel.NewFunction(nil, integer, sqlmodel.FunctionNullableNeverNull, sqlmodel.WithFunctionName("count"), sqlmodel.WithAggregate())},
		"lower": {sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "value", Type: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}}},
			sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, sqlmodel.FunctionNullableReturnsNullOnNull, sqlmodel.WithFunctionName("lower"))},
	}}
	return NewTypeResolver(catalogue, NewFunctionResolver(builtins, catalogue, engine), engine)
}

func TestResolveProjectionReportsTheItemEachColumnCameFrom(t *testing.T) {
	t.Parallel()

	rawColumns := []ast.RawOutputColumn{
		{ColumnName: "id", TableAlias: "users"},
		{IsStar: true, TableAlias: "users"},
		{Name: "loud", Expression: &ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{&ast.ColumnRefExpression{ColumnName: "name"}}}},
	}

	projection := newProjectionResolver().ResolveProjection(t.Context(), rawColumns, nil, setupTypeResolverScope())

	require.Empty(t, projection.Problems)
	require.Len(t, projection.Columns, 5)
	require.Equal(t, []int{0, 1, 1, 1, 2}, projection.Origins)
	require.False(t, projection.Aggregated)
}

func TestResolveProjectionPlacesProblemsAtTheItem(t *testing.T) {
	t.Parallel()

	itemSpan := Span{Start: 40, End: 52}
	testCases := []struct {
		name     string
		raw      ast.RawOutputColumn
		code     string
		expected Span
	}{
		{
			name:     "an unknown column the dialect located is placed at its reference",
			raw:      ast.RawOutputColumn{Expression: &ast.ColumnRefExpression{ColumnName: "nme", Span: ast.Span{Start: 44, End: 47}}},
			code:     diag.CodeUnknownColumn,
			expected: Span{Start: 44, End: 47},
		},
		{
			name:     "an unknown column the dialect did not locate is placed at the item",
			raw:      ast.RawOutputColumn{ColumnName: "nme", TableAlias: "users"},
			code:     diag.CodeUnknownColumn,
			expected: itemSpan,
		},
		{
			name: "an unknown function is placed at its name",
			raw: ast.RawOutputColumn{Expression: &ast.FunctionCallExpression{
				FunctionName: "shout", Arguments: []ast.Expression{&ast.ColumnRefExpression{ColumnName: "name"}}, Span: ast.Span{Start: 40, End: 45},
			}},
			code:     diag.CodeUnknownFunction,
			expected: Span{Start: 40, End: 45},
		},
		{
			name: "an unknown function inside another expression is placed at its name",
			raw: ast.RawOutputColumn{Expression: &ast.UnaryOpExpression{Operator: "-", Operand: &ast.FunctionCallExpression{
				FunctionName: "shout", Span: ast.Span{Start: 45, End: 50},
			}}},
			code:     diag.CodeUnknownFunction,
			expected: Span{Start: 45, End: 50},
		},
		{
			name: "an unknown function the dialect did not locate inside another expression is placed at the item",
			raw: ast.RawOutputColumn{Expression: &ast.UnaryOpExpression{Operator: "-", Operand: &ast.FunctionCallExpression{
				FunctionName: "shout",
			}}},
			code:     diag.CodeUnknownFunction,
			expected: itemSpan,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			projection := newProjectionResolver().ResolveProjection(t.Context(), []ast.RawOutputColumn{testCase.raw}, []Span{itemSpan},
				setupTypeResolverScope())

			require.Len(t, projection.Problems, 1)
			require.Equal(t, testCase.code, projection.Problems[0].Code)
			require.Equal(t, testCase.expected, projection.Problems[0].Span)
			require.Len(t, projection.Columns, 1)
		})
	}
}

func located(name string, start int) *ast.ColumnRefExpression {
	return &ast.ColumnRefExpression{ColumnName: name, Span: ast.Span{Start: start, End: start + len(name)}}
}

func call(name string, arguments ...ast.Expression) *ast.FunctionCallExpression {
	return &ast.FunctionCallExpression{FunctionName: name, Arguments: arguments}
}

func TestResolveProjectionReportsAProblemInsideAnItemWhereItIs(t *testing.T) {
	t.Parallel()

	text := &ast.LiteralExpression{TypeName: "text"}
	testCases := []struct {
		expression ast.Expression
		name       string
		want       []expectedProblem
		wantType   string
	}{
		{
			name:       "an unknown column passed to a function",
			expression: call("lower", located("nme", 13)),
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 13, End: 16}}},
			wantType:   "text",
		},
		{
			name:       "an unknown function passed to a function",
			expression: call("lower", &ast.FunctionCallExpression{FunctionName: "shout", Span: ast.Span{Start: 13, End: 18}}),
			want:       []expectedProblem{{code: diag.CodeUnknownFunction, suggestion: "", span: Span{Start: 13, End: 18}}},
			wantType:   "text",
		},
		{
			name:       "every unknown column of an item",
			expression: &ast.CoalesceExpression{Arguments: []ast.Expression{located("nme", 16), located("emial", 21), text}},
			want: []expectedProblem{
				{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 16, End: 19}},
				{code: diag.CodeUnknownColumn, suggestion: "email", span: Span{Start: 21, End: 26}},
			},
			wantType: "text",
		},
		{
			name:       "an unknown column cast",
			expression: &ast.CastExpression{Inner: located("nme", 12), TypeName: "text"},
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 12, End: 15}}},
			wantType:   "text",
		},
		{
			name:       "an unknown column in arithmetic",
			expression: &ast.BinaryOpExpression{Operator: "+", Left: located("idd", 7), Right: located("id", 13)},
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "id", span: Span{Start: 7, End: 10}}},
			wantType:   "int4",
		},
		{
			name: "an unknown column a CASE returns",
			expression: &ast.CaseWhenExpression{
				Branches:   []ast.CaseWhenBranch{{Condition: located("id", 17), Result: located("nme", 25)}},
				ElseResult: located("emial", 34),
			},
			want: []expectedProblem{
				{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 25, End: 28}},
				{code: diag.CodeUnknownColumn, suggestion: "email", span: Span{Start: 34, End: 39}},
			},
			wantType: "",
		},
		{
			name:       "an unknown column in a lambda's body",
			expression: call("lower", lambda(located("nme", 18), "x")),
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 18, End: 21}}},
			wantType:   "text",
		},
		{
			name:       "a lambda's parameter is no unknown column",
			expression: call("lower", lambda(located("x", 18), "x")),
			want:       nil,
			wantType:   "text",
		},
		{
			name:       "an unknown column a struct constructor holds",
			expression: &ast.StructExpression{Names: []string{"id", "label"}, Values: []ast.Expression{located("id", 12), located("nme", 23)}},
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 23, End: 26}}},
			wantType:   "struct",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			raw := []ast.RawOutputColumn{{Name: "value", Expression: testCase.expression}}
			projection := newProjectionResolver().ResolveProjection(t.Context(), raw, []Span{{Start: 7, End: 60}}, setupTypeResolverScope())

			var problems []expectedProblem
			for _, problem := range projection.Problems {
				problems = append(problems, expectedProblem{code: problem.Code, suggestion: problem.Suggestion, span: problem.Span})
			}
			require.Equal(t, testCase.want, problems)
			require.Len(t, projection.Columns, 1)
			require.Equal(t, testCase.wantType, projection.Columns[0].SQLType.EngineName)
		})
	}
}

func TestResolveExpressionTypeKeepsTheProblemsOfOperandsToItself(t *testing.T) {
	t.Parallel()

	resolver := newProjectionResolver()

	sqlType, _, err := resolver.ResolveExpressionType(t.Context(), call("lower", located("nme", 13)), setupTypeResolverScope(), new(false))

	require.NoError(t, err)
	require.Equal(t, "text", sqlType.EngineName)
	require.Empty(t, resolver.takeNotices())
}

func TestResolveProjectionKeepsAProblemWithoutAPlaceUnplaced(t *testing.T) {
	t.Parallel()

	raw := []ast.RawOutputColumn{{ColumnName: "nme", TableAlias: "users"}}

	projection := newProjectionResolver().ResolveProjection(t.Context(), raw, nil, setupTypeResolverScope())

	require.Len(t, projection.Problems, 1)
	require.False(t, projection.Problems[0].Span.Known())
}

func TestResolveProjectionReportsAggregation(t *testing.T) {
	t.Parallel()

	count := func() *ast.FunctionCallExpression { return &ast.FunctionCallExpression{FunctionName: "COUNT"} }
	testCases := []struct {
		name       string
		expression ast.Expression
		expected   bool
	}{
		{name: "a column does not aggregate", expression: &ast.ColumnRefExpression{ColumnName: "id"}, expected: false},
		{name: "an aggregate call aggregates", expression: count(), expected: true},
		{name: "an aggregate compared aggregates", expression: &ast.ComparisonExpression{Left: count(), Operator: ">", Right: &ast.LiteralExpression{TypeName: "integer"}}, expected: true},
		{name: "an aggregate inside a scalar function aggregates", expression: &ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{count()}}, expected: true},
		{name: "a window function aggregates", expression: &ast.WindowFunctionExpression{Function: &ast.FunctionCallExpression{FunctionName: "lower"}}, expected: true},
		{name: "an aggregate inside a subquery does not", expression: &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{
			OutputColumns: []ast.RawOutputColumn{{Expression: count()}},
		}}, expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			raw := []ast.RawOutputColumn{{Name: "value", Expression: testCase.expression}}
			projection := newProjectionResolver().ResolveProjection(t.Context(), raw, nil, setupTypeResolverScope())

			require.Equal(t, testCase.expected, projection.Aggregated)
		})
	}
}

func TestResolveProjectionReadsARelationNamedAsAValueAsItsRow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine       TypeRules
		raw          ast.RawOutputColumn
		name         string
		wantProblems []string
		wantEngine   string
	}{
		{
			name:       "a relation projected where the dialect has row values",
			engine:     rowValueEngine{Engine: &fakedialect.Engine{}},
			raw:        ast.RawOutputColumn{ColumnName: "users"},
			wantEngine: "users",
		},
		{
			name:       "a relation passed to a function where the dialect has row values",
			engine:     rowValueEngine{Engine: &fakedialect.Engine{}},
			raw:        ast.RawOutputColumn{Name: "document", Expression: call("to_json", located("users", 15))},
			wantEngine: "json",
		},
		{
			name:         "a relation projected where the dialect has none",
			engine:       &fakedialect.Engine{},
			raw:          ast.RawOutputColumn{ColumnName: "users"},
			wantProblems: []string{diag.CodeUnknownColumn},
		},
		{
			name:         "a qualified name is never a row",
			engine:       rowValueEngine{Engine: &fakedialect.Engine{}},
			raw:          ast.RawOutputColumn{ColumnName: "users", TableAlias: "users"},
			wantProblems: []string{diag.CodeUnknownColumn},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{
				"to_json": {sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "value", Type: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")}},
					sqlmodel.SQLType{EngineName: "json", Category: sqlmodel.TypeCategoryJSON}, sqlmodel.FunctionNullableReturnsNullOnNull,
					sqlmodel.WithFunctionName("to_json"))},
			}}
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(builtins, catalogue, testCase.engine), testCase.engine)
			scope := resolver.NewRootScope(ScopeKindQuery)
			scope.AddTable(ast.TableReference{Name: "users", Schema: "public"}, ast.JoinInner, &sqlmodel.Table{
				Name: "users", Schema: "public",
				Columns: []sqlmodel.Column{{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}}},
			})

			projection := resolver.ResolveProjection(t.Context(), []ast.RawOutputColumn{testCase.raw}, nil, scope)

			var problems []string
			for _, problem := range projection.Problems {
				problems = append(problems, problem.Code)
			}
			require.Equal(t, testCase.wantProblems, problems)
			require.Len(t, projection.Columns, 1)
			if testCase.wantEngine != "" {
				require.Equal(t, testCase.wantEngine, projection.Columns[0].SQLType.EngineName)
			}
		})
	}
}

func TestResolveProjectionPrefersAColumnToARowOfTheSameName(t *testing.T) {
	t.Parallel()

	engine := rowValueEngine{Engine: &fakedialect.Engine{}}
	catalogue := fakedialect.NewCatalogue("public")
	resolver := NewTypeResolver(catalogue, newProjectionResolver().functionResolver, engine)
	scope := resolver.NewRootScope(ScopeKindQuery)
	scope.AddTable(ast.TableReference{Name: "tags"}, ast.JoinInner, &sqlmodel.Table{
		Name:    "tags",
		Columns: []sqlmodel.Column{{Name: "tags", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}}},
	})

	projection := resolver.ResolveProjection(t.Context(), []ast.RawOutputColumn{{ColumnName: "tags"}}, nil, scope)

	require.Empty(t, projection.Problems)
	require.Equal(t, "text", projection.Columns[0].SQLType.EngineName)
	require.Equal(t, "tags", projection.Columns[0].SourceColumn)
}

func lateralAliasScope(lateral bool) *ScopeChain {
	rules := testScopeRules
	rules.LateralAliases = lateral
	scope := NewRootScopeChain(ScopeKindQuery, rules)
	scope.AddTable(
		ast.TableReference{Name: "users", Schema: "public"},
		ast.JoinInner,
		&sqlmodel.Table{
			Name: "users",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
				{Name: "name", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
				{Name: "email", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, Nullable: true},
			},
		},
	)
	return scope
}

func lowerOf(column string) ast.Expression {
	return &ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{&ast.ColumnRefExpression{ColumnName: column}}}
}

func TestResolveProjectionReadsAnEarlierItemByItsAlias(t *testing.T) {
	t.Parallel()

	text := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	testCases := []struct {
		name         string
		items        []ast.RawOutputColumn
		wantType     sqlmodel.SQLType
		wantProblem  string
		wantNamed    []int
		lateral      bool
		wantNullable bool
	}{
		{
			name:      "an expression naming the alias of an earlier expression",
			items:     []ast.RawOutputColumn{{Name: "loud", Expression: lowerOf("name")}, {Name: "again", Expression: lowerOf("loud")}},
			lateral:   true,
			wantType:  text,
			wantNamed: []int{0},
		},
		{
			name:      "an item that is the alias of an earlier item",
			items:     []ast.RawOutputColumn{{Name: "loud", Expression: lowerOf("name")}, {Name: "again", ColumnName: "loud"}},
			lateral:   true,
			wantType:  text,
			wantNamed: []int{0},
		},
		{
			name:         "the alias of a nullable item",
			items:        []ast.RawOutputColumn{{Name: "mail", ColumnName: "email"}, {Name: "again", Expression: &ast.ColumnRefExpression{ColumnName: "mail"}}},
			lateral:      true,
			wantType:     text,
			wantNamed:    []int{0},
			wantNullable: true,
		},
		{
			name:     "a column of the relations wins over an alias of its name",
			items:    []ast.RawOutputColumn{{Name: "id", Expression: lowerOf("name")}, {Name: "again", ColumnName: "id"}},
			lateral:  true,
			wantType: integer,
		},
		{
			name:     "a bare column under its own name gives no alias, so naming it again reads the column",
			items:    []ast.RawOutputColumn{{Name: "name", ColumnName: "name"}, {Name: "again", Expression: lowerOf("name")}},
			lateral:  true,
			wantType: text,
		},
		{
			name:        "the alias of a later item",
			items:       []ast.RawOutputColumn{{Name: "again", ColumnName: "loud"}, {Name: "loud", Expression: lowerOf("name")}},
			lateral:     true,
			wantProblem: diag.CodeUnknownColumn,
		},
		{
			name:        "an alias where the dialect has no lateral aliases",
			items:       []ast.RawOutputColumn{{Name: "loud", Expression: lowerOf("name")}, {Name: "again", ColumnName: "loud"}},
			lateral:     false,
			wantProblem: diag.CodeUnknownColumn,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := lateralAliasScope(testCase.lateral)
			projection := newProjectionResolver().ResolveProjection(t.Context(), testCase.items, nil, scope)

			if testCase.wantProblem != "" {
				require.Len(t, projection.Problems, 1)
				require.Equal(t, testCase.wantProblem, projection.Problems[0].Code)
				return
			}
			require.Empty(t, projection.Problems)
			require.Len(t, projection.Columns, 2)
			require.Equal(t, testCase.wantType.Category, projection.Columns[1].SQLType.Category)
			require.Equal(t, testCase.wantType.EngineName, projection.Columns[1].SQLType.EngineName)
			require.Equal(t, testCase.wantNullable, projection.Columns[1].Nullable)
			require.Equal(t, testCase.wantNamed, projection.NamedItems)
		})
	}
}

func TestResolveProjectionKeepsItsAliasesToItsSelectList(t *testing.T) {
	t.Parallel()

	scope := lateralAliasScope(true)
	projection := newProjectionResolver().ResolveProjection(t.Context(),
		[]ast.RawOutputColumn{{Name: "loud", Expression: lowerOf("name")}}, nil, scope)
	require.Empty(t, projection.Problems)

	_, _, err := scope.ResolveColumn("", "loud")
	require.Error(t, err)
}

func TestProjectionKnowsTheElementsOfACallThatSaysItKnowsThem(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{}
	catalogue := fakedialect.NewCatalogue("public")
	collected := sqlmodel.NewFunction(nil, sqlmodel.SQLType{EngineName: "int8[]", Category: sqlmodel.TypeCategoryArray,
		ElementType: &sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}},
		sqlmodel.FunctionNullableCalledOnNull, sqlmodel.WithFunctionName("collect"), sqlmodel.WithAggregate())
	resolver := NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{"collect": {collected}}},
		catalogue, knownElementsEngine{Engine: engine}), knownElementsEngine{Engine: engine})
	rawColumns := []ast.RawOutputColumn{
		{Name: "ids", Expression: call("gather", column("id"))},
		{Name: "listed", Expression: call("collect", column("id"))},
		{ColumnName: "id"},
	}

	projection := resolver.ResolveProjection(t.Context(), rawColumns, nil, setupTypeResolverScope())

	require.Equal(t, []bool{true, false, false}, projection.ElementsKnown)
}

func TestResolveProjectionReadsAnAliasBeforeAColumnWhereTheDialectsAliasesShadowColumns(t *testing.T) {
	t.Parallel()

	bumped := &ast.BinaryOpExpression{Operator: "+", Left: &ast.ColumnRefExpression{ColumnName: "id"}, Right: &ast.LiteralExpression{TypeName: "int4"}}
	testCases := []struct {
		name      string
		items     []ast.RawOutputColumn
		shadowing ast.Clause
		wantType  string
	}{
		{
			name:      "a later item reads the alias of an earlier one rather than the column of its name",
			items:     []ast.RawOutputColumn{{Name: "name", Expression: bumped}, {Name: "again", ColumnName: "name"}},
			shadowing: ast.ClauseSelectList | ast.ClauseWhere,
			wantType:  "int4",
		},
		{
			name: "a qualified name reads the column",
			items: []ast.RawOutputColumn{{Name: "name", Expression: bumped},
				{Name: "again", Expression: &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "name"}}},
			shadowing: ast.ClauseSelectList | ast.ClauseWhere,
			wantType:  "text",
		},
		{
			name:      "the item giving the alias reads the column of its name",
			items:     []ast.RawOutputColumn{{Name: "first", ColumnName: "id"}, {Name: "name", Expression: lowerOf("name")}},
			shadowing: ast.ClauseSelectList | ast.ClauseWhere,
			wantType:  "text",
		},
		{
			name:     "a dialect whose aliases do not shadow columns reads the column",
			items:    []ast.RawOutputColumn{{Name: "name", Expression: bumped}, {Name: "again", ColumnName: "name"}},
			wantType: "text",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := lateralAliasScope(true)
			scope.rules.ShadowingAliases = testCase.shadowing
			projection := newProjectionResolver().ResolveProjection(t.Context(), testCase.items, nil, scope)

			require.Empty(t, projection.Problems)
			require.Len(t, projection.Columns, 2)
			require.Equal(t, testCase.wantType, projection.Columns[1].SQLType.EngineName)
		})
	}
}

func engineResolvingProjection(resolve func(ctx context.Context, name string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error)) *TypeResolver {
	engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: resolve}
	catalogue := fakedialect.NewCatalogue("public")
	return NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, engine), engine)
}

func TestResolveProjectionReportsAPanicTheDialectRecoveredAtTheCall(t *testing.T) {
	t.Parallel()

	panicked := fmt.Errorf("resolve function call: %w: index out of range", fault.ErrPanic)
	testCases := []struct {
		expression ast.Expression
		name       string
		want       []expectedProblem
	}{
		{
			name:       "a call the select list makes",
			expression: &ast.FunctionCallExpression{FunctionName: "explode", Span: ast.Span{Start: 7, End: 16}},
			want:       []expectedProblem{{code: diag.CodeRecoveredPanic, suggestion: "", span: Span{Start: 7, End: 16}}},
		},
		{
			name:       "a call passed to another",
			expression: call("lower", &ast.FunctionCallExpression{FunctionName: "explode", Span: ast.Span{Start: 13, End: 22}}),
			want:       []expectedProblem{{code: diag.CodeRecoveredPanic, suggestion: "", span: Span{Start: 13, End: 22}}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := engineResolvingProjection(func(_ context.Context, name string, _ []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				if name == "lower" {
					return &ast.FunctionResolution{ReturnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), DataAccess: sqlmodel.DataAccessReadOnly}, nil
				}
				return nil, panicked
			})
			raw := []ast.RawOutputColumn{{Name: "value", Expression: testCase.expression}}

			projection := resolver.ResolveProjection(t.Context(), raw, []Span{{Start: 7, End: 60}}, setupTypeResolverScope())

			var problems []expectedProblem
			for _, problem := range projection.Problems {
				problems = append(problems, expectedProblem{code: problem.Code, suggestion: problem.Suggestion, span: problem.Span})
				require.Equal(t, diag.SeverityError, problem.Severity)
			}
			require.Equal(t, testCase.want, problems)
			require.Len(t, projection.Columns, 1)
		})
	}
}

func TestResolveProjectionStopsWhenTheDialectResolverIsInterrupted(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		failure func(ctx context.Context, cancel context.CancelFunc) error
		name    string
	}{
		{
			name: "the context is cancelled",
			failure: func(ctx context.Context, cancel context.CancelFunc) error {
				cancel()
				return context.Cause(ctx)
			},
		},
		{
			name: "the dialect runs out of time",
			failure: func(context.Context, context.CancelFunc) error {
				return fmt.Errorf("walk: %w", context.DeadlineExceeded)
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var asked []string
			resolver := engineResolvingProjection(func(ctx context.Context, name string, _ []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				asked = append(asked, name)
				return nil, testCase.failure(ctx, cancel)
			})
			raw := []ast.RawOutputColumn{
				{Name: "first", Expression: call("first")},
				{Name: "second", Expression: call("second")},
			}

			projection := resolver.ResolveProjection(ctx, raw, nil, setupTypeResolverScope())

			require.Empty(t, projection.Problems, "an interruption is no problem with the query")
			require.Equal(t, []string{"first"}, asked)
			require.Empty(t, projection.Columns, "no item is resolved once the dialect is interrupted")
		})
	}
}
