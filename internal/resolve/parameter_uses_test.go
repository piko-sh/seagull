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

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func comparedWith(number int, column string) ast.RawParameterReference {
	return ast.RawParameterReference{Number: number, Context: sqlmodel.ParameterContextComparison,
		ColumnReference: &ast.ColumnReference{TableAlias: "users", ColumnName: column}}
}

func newConflictResolver(implicit bool) *TypeResolver {
	engine := &fakedialect.Engine{CanImplicitCastFunc: func(_, _ sqlmodel.SQLType) bool { return implicit }}
	catalogue := fakedialect.NewCatalogue("public")
	builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{}}
	return NewTypeResolver(catalogue, NewFunctionResolver(builtins, catalogue, engine), engine)
}

func TestAParameterUsedWhereTwoTypesAreExpectedIsReported(t *testing.T) {
	t.Parallel()

	castToInteger := comparedWith(1, "id")
	castToInteger.CastType = &sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	insideCall := comparedWith(1, "id")
	insideCall.EnclosingFunctionName = "lower"

	testCases := []struct {
		name       string
		references []ast.RawParameterReference
		clauses    []ast.ClauseExpression
		implicit   bool
		want       []string
	}{
		{
			name:       "a parameter assigned to text and compared with a number is reported",
			references: []ast.RawParameterReference{assignedTo(1, "name"), comparedWith(1, "id")},
			clauses:    []ast.ClauseExpression{setValue(&ast.ParameterExpression{Number: 1})},
			want: []string{"parameter name is used both where text and where int4 is expected, which the engine converts between " +
				"only explicitly; the generated code takes text, so a value of the other type fails; cast one use, or pass the " +
				"value as two parameters"},
		},
		{
			name:       "a parameter every use of which expects one type is not reported",
			references: []ast.RawParameterReference{assignedTo(1, "name"), comparedWith(1, "email")},
			clauses:    []ast.ClauseExpression{setValue(&ast.ParameterExpression{Number: 1})},
		},
		{
			name:       "types the engine converts between implicitly are not reported",
			references: []ast.RawParameterReference{assignedTo(1, "name"), comparedWith(1, "id")},
			clauses:    []ast.ClauseExpression{setValue(&ast.ParameterExpression{Number: 1})},
			implicit:   true,
		},
		{
			name:       "a use under a cast says which type the statement means",
			references: []ast.RawParameterReference{assignedTo(1, "name"), castToInteger},
			clauses:    []ast.ClauseExpression{setValue(&ast.ParameterExpression{Number: 1})},
		},
		{
			name:       "a use inside a call is typed by the call, not the column",
			references: []ast.RawParameterReference{assignedTo(1, "name"), insideCall},
			clauses:    []ast.ClauseExpression{setValue(&ast.ParameterExpression{Number: 1})},
		},
		{
			name:       "a parameter an assigned value holds inside a larger expression is not reported",
			references: []ast.RawParameterReference{assignedTo(1, "name"), assignedTo(1, "id")},
			clauses: []ast.ClauseExpression{
				valuesItem(&ast.ParameterExpression{Number: 1}),
				valuesItem(&ast.IsNullExpression{Inner: &ast.ParameterExpression{Number: 1}}),
			},
		},
		{
			name:       "a column the query does not read fixes no type",
			references: []ast.RawParameterReference{assignedTo(1, "name"), comparedWith(1, "r")},
			clauses:    []ast.ClauseExpression{setValue(&ast.ParameterExpression{Number: 1})},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{ParameterReferences: testCase.references, ClauseExpressions: testCase.clauses}
			_, problems := newConflictResolver(testCase.implicit).ResolveParameters(t.Context(), analysis, setupTypeResolverScope(), nil)

			var reported []string
			for _, problem := range problems {
				if problem.Code == diag.CodeExpressionTypeError {
					reported = append(reported, problem.Message)
				}
			}
			require.Equal(t, testCase.want, reported)
		})
	}
}
