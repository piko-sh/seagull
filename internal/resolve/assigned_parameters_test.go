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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func assignedTo(number int, column string) ast.RawParameterReference {
	return ast.RawParameterReference{Number: number, Context: sqlmodel.ParameterContextAssignment,
		ColumnReference: &ast.ColumnReference{TableAlias: "users", ColumnName: column}}
}

func valuesItem(expression ast.Expression) ast.ClauseExpression {
	return ast.ClauseExpression{Clause: ast.ClauseInsertValue, Expression: expression}
}

func setValue(expression ast.Expression) ast.ClauseExpression {
	return ast.ClauseExpression{Clause: ast.ClauseSetValue, Expression: expression}
}

func newAssignedParametersResolver() *TypeResolver {
	engine := &fakedialect.Engine{
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			switch name {
			case "timestamp":
				return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryTemporal}
			case "int[]":
				return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryArray}
			}
			return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryUnknown}
		},
		BinaryResultFunc: func(operator string, left, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
			if operator == "||" && left.Category == sqlmodel.TypeCategoryInteger {
				return sqlmodel.SQLType{EngineName: "bool", Category: sqlmodel.TypeCategoryBoolean}, sqlmodel.FunctionNullableReturnsNullOnNull, true
			}
			return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
		},
	}
	catalogue := fakedialect.NewCatalogue("public")
	anyType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown}
	text := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{
		"lower": {sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "value", Type: text}}, text,
			sqlmodel.FunctionNullableReturnsNullOnNull, sqlmodel.WithFunctionName("lower"))},
		"greatest": {sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "value", Type: anyType}}, anyType,
			sqlmodel.FunctionNullableCalledOnNull, sqlmodel.WithFunctionName("greatest"), sqlmodel.WithVariadic(1))},
		"max": {
			sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "x", Type: anyType}}, anyType,
				sqlmodel.FunctionNullableCalledOnNull, sqlmodel.WithFunctionName("max"), sqlmodel.WithAggregate()),
			sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "x", Type: anyType}, {Name: "y", Type: anyType}}, anyType,
				sqlmodel.FunctionNullableCalledOnNull, sqlmodel.WithFunctionName("max")),
		},
		"tag": {sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "key", Type: anyType}, {Name: "value", Type: anyType}}, text,
			sqlmodel.FunctionNullableReturnsNullOnNull, sqlmodel.WithFunctionName("tag"))},
		"array_append": {sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "array", Type: anyType}, {Name: "element", Type: anyType}}, anyType,
			sqlmodel.FunctionNullableCalledOnNull, sqlmodel.WithFunctionName("array_append"))},
	}}
	return NewTypeResolver(catalogue, NewFunctionResolver(builtins, catalogue, engine), engine)
}

func TestParametersAssignedAsPartOfAnExpressionAreTypedFromTheExpression(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		clauses  []ast.ClauseExpression
		wantType map[string]sqlmodel.SQLTypeCategory
		wantName map[int]string
	}{
		{
			name:     "a parameter that is the whole value takes the column's type",
			clauses:  []ast.ClauseExpression{valuesItem(&ast.ParameterExpression{Number: 1})},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryInteger},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter under a COALESCE takes the column's type",
			clauses:  []ast.ClauseExpression{valuesItem(&ast.CoalesceExpression{Arguments: []ast.Expression{&ast.ParameterExpression{Number: 1}, integerLiteral()}})},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryInteger},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter beside a number in arithmetic takes the number's type",
			clauses:  []ast.ClauseExpression{setValue(&ast.BinaryOpExpression{Operator: "+", Left: column("id"), Right: &ast.ParameterExpression{Number: 1}})},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryInteger},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter beside text takes the text's type rather than the column's",
			clauses:  []ast.ClauseExpression{setValue(&ast.BinaryOpExpression{Operator: "||", Left: column("name"), Right: &ast.ParameterExpression{Number: 1}})},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryText},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter beside a timestamp keeps the column's name but not its type",
			clauses:  []ast.ClauseExpression{valuesItem(&ast.BinaryOpExpression{Operator: "+", Left: &ast.LiteralExpression{TypeName: "timestamp"}, Right: &ast.ParameterExpression{Number: 1}})},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryUnknown},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter beside an operand the operator turns into another kind of value is left untyped",
			clauses:  []ast.ClauseExpression{setValue(&ast.BinaryOpExpression{Operator: "||", Left: column("id"), Right: &ast.ParameterExpression{Number: 1}})},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryUnknown},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter that is a result of a CASE takes the column's type",
			clauses:  []ast.ClauseExpression{setValue(&ast.CaseWhenExpression{Branches: []ast.CaseWhenBranch{{Condition: column("name"), Result: &ast.ParameterExpression{Number: 1}}}, ElseResult: column("id")})},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryInteger},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter NULLIF passes through takes the column's type",
			clauses:  []ast.ClauseExpression{setValue(&ast.CoalesceExpression{Arguments: []ast.Expression{call("NULLIF", &ast.ParameterExpression{Number: 1}, integerLiteral()), column("id")}})},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryInteger},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter beside a column in GREATEST takes the column's type",
			clauses:  []ast.ClauseExpression{setValue(call("GREATEST", column("id"), &ast.ParameterExpression{Number: 1}))},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryInteger},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter beside a column in a scalar max takes the column's type",
			clauses:  []ast.ClauseExpression{setValue(call("max", &ast.ParameterExpression{Number: 1}, column("name")))},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryText},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter of a function whose result is not its arguments' type is left untyped",
			clauses:  []ast.ClauseExpression{setValue(call("tag", column("name"), &ast.ParameterExpression{Number: 1}))},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryUnknown},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter beside an array is left untyped rather than typed as the array",
			clauses:  []ast.ClauseExpression{setValue(call("array_append", &ast.LiteralExpression{TypeName: "int[]"}, &ast.ParameterExpression{Number: 1}))},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryUnknown},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter NULLIF compares with is part of the value",
			clauses:  []ast.ClauseExpression{setValue(call("nullif", column("id"), &ast.ParameterExpression{Number: 1}))},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryUnknown},
			wantName: map[int]string{1: "id"},
		},
		{
			name:     "a parameter passed to a function takes the function's argument type",
			clauses:  []ast.ClauseExpression{setValue(call("lower", &ast.ParameterExpression{Number: 1}))},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryText},
			wantName: map[int]string{1: "id"},
		},
		{
			name: "a parameter that is a whole value elsewhere keeps the column's type",
			clauses: []ast.ClauseExpression{
				valuesItem(&ast.ParameterExpression{Number: 1}),
				setValue(call("lower", &ast.ParameterExpression{Number: 1})),
			},
			wantType: map[string]sqlmodel.SQLTypeCategory{"id": sqlmodel.TypeCategoryInteger},
			wantName: map[int]string{1: "id"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{ParameterReferences: []ast.RawParameterReference{assignedTo(1, "id")}, ClauseExpressions: testCase.clauses}
			parameters, _ := newAssignedParametersResolver().ResolveParameters(t.Context(), analysis, setupTypeResolverScope(), nil)

			require.Len(t, parameters, 1)
			require.Equal(t, testCase.wantName[1], parameters[0].Name)
			require.Equal(t, testCase.wantType["id"], parameters[0].SQLType.Category)
		})
	}
}

func TestAPlaceholderHiddenInsideAValuesRowValueIsPartOfIt(t *testing.T) {
	t.Parallel()

	analysis := &ast.RawQueryAnalysis{
		ParameterReferences: []ast.RawParameterReference{assignedTo(1, "id"), assignedTo(2, "name")},
		ClauseExpressions:   []ast.ClauseExpression{valuesItem(&ast.ScalarSubqueryExpression{}), valuesItem(&ast.ParameterExpression{Number: 2})},
		Spans: ast.StatementSpans{
			Values:       []ast.TupleSpan{{Span: ast.Span{Start: 20, End: 60}, Items: []ast.Span{{Start: 21, End: 55}, {Start: 57, End: 59}}}},
			Placeholders: []ast.PlaceholderSpan{{Number: 1, Span: ast.Span{Start: 50, End: 52}}, {Number: 2, Span: ast.Span{Start: 57, End: 59}}},
		},
	}

	classified := classifyAssignedParameters(analysis)

	require.Equal(t, map[int]bool{1: true}, classified.embedded)
}

func multiRowInsert(rows int) *ast.RawQueryAnalysis {
	analysis := &ast.RawQueryAnalysis{InsertTable: "users", InsertTarget: ast.TableReference{Name: "users"}}
	offset := len("INSERT INTO users VALUES ")
	for range rows {
		row := ast.TupleSpan{Span: ast.Span{Start: offset, End: offset + len("(?, ?)")}}
		for item := range 2 {
			start := offset + 1 + item*len("?, ")
			number := len(analysis.Spans.Placeholders) + 1
			span := ast.Span{Start: start, End: start + 1}
			row.Items = append(row.Items, span)
			analysis.Spans.Placeholders = append(analysis.Spans.Placeholders, ast.PlaceholderSpan{Number: number, Span: span})
			analysis.ClauseExpressions = append(analysis.ClauseExpressions, valuesItem(&ast.ParameterExpression{Number: number}))
		}
		analysis.Spans.Values = append(analysis.Spans.Values, row)
		offset = row.Span.End + len(", ")
	}
	return analysis
}

func TestMultiRowInsertParametersAreClassifiedInLinearTime(t *testing.T) {
	t.Parallel()

	classify := func(rows int) func() {
		analysis := multiRowInsert(rows)
		scope := setupTypeResolverScope()
		return func() {
			classified := classifyAssignedParameters(analysis)
			require.Empty(t, classified.embedded)
			require.Len(t, positionalTargets(analysis, scope), 2*rows)
		}
	}

	scaling.Linear(t, classify(1_000), classify(4_000))
}

func TestPositionalTargetsFollowEachValuesRow(t *testing.T) {
	t.Parallel()

	analysis := multiRowInsert(3)

	targets := positionalTargets(analysis, setupTypeResolverScope())

	require.Equal(t, map[int]ast.ColumnReference{
		1: {TableAlias: "users", ColumnName: "id"}, 2: {TableAlias: "users", ColumnName: "name"},
		3: {TableAlias: "users", ColumnName: "id"}, 4: {TableAlias: "users", ColumnName: "name"},
		5: {TableAlias: "users", ColumnName: "id"}, 6: {TableAlias: "users", ColumnName: "name"},
	}, targets)
}

func TestAHiddenPlaceholderIsFoundWhereverTheDialectListsIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		placeholders []ast.PlaceholderSpan
	}{
		{name: "placeholders in source order", placeholders: []ast.PlaceholderSpan{
			{Number: 1, Span: ast.Span{Start: 50, End: 52}}, {Number: 2, Span: ast.Span{Start: 57, End: 59}}}},
		{name: "placeholders out of source order", placeholders: []ast.PlaceholderSpan{
			{Number: 2, Span: ast.Span{Start: 57, End: 59}}, {Number: 1, Span: ast.Span{Start: 50, End: 52}}}},
		{name: "a placeholder at the value's first byte", placeholders: []ast.PlaceholderSpan{
			{Number: 1, Span: ast.Span{Start: 21, End: 23}}, {Number: 2, Span: ast.Span{Start: 57, End: 59}}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{
				ParameterReferences: []ast.RawParameterReference{assignedTo(1, "id"), assignedTo(2, "name")},
				ClauseExpressions:   []ast.ClauseExpression{valuesItem(&ast.ScalarSubqueryExpression{}), valuesItem(&ast.ParameterExpression{Number: 2})},
				Spans: ast.StatementSpans{
					Values:       []ast.TupleSpan{{Span: ast.Span{Start: 20, End: 60}, Items: []ast.Span{{Start: 21, End: 55}, {Start: 57, End: 59}}}},
					Placeholders: testCase.placeholders,
				},
			}

			require.Equal(t, map[int]bool{1: true}, classifyAssignedParameters(analysis).embedded)
		})
	}
}

func TestQualifiedFunctionNameKeepsTheNameTheDialectGave(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		call *ast.FunctionCallExpression
		name string
		want string
	}{
		{name: "an unqualified name", call: &ast.FunctionCallExpression{FunctionName: "scale"}, want: "scale"},
		{name: "a quoted mixed-case name keeps its case", call: &ast.FunctionCallExpression{FunctionName: "Scale"}, want: "Scale"},
		{name: "a qualified name keeps the case of both parts", call: &ast.FunctionCallExpression{FunctionName: "Scale", Schema: "Reports"},
			want: "Reports.Scale"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, qualifiedFunctionName(testCase.call))
		})
	}
}
