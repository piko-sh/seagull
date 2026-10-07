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
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type resultCheckingEngine struct {
	*fakedialect.Engine
}

func (resultCheckingEngine) UnreadableResult(sqlType sqlmodel.SQLType) (dialect.ResultProblem, bool) {
	for _, field := range sqlType.StructFields {
		if field.Positional {
			return dialect.ResultProblem{Reason: "the driver cannot read a struct whose fields have no names", Suggestion: "name the fields"}, true
		}
	}
	return dialect.ResultProblem{}, false
}

func TestResolveProjectionRefusesAValueTheDialectsDriversCannotRead(t *testing.T) {
	t.Parallel()

	itemSpans := []Span{{Start: 7, End: 9}, {Start: 11, End: 15}, {Start: 17, End: 18}}
	pair := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	pair.StructFields = []sqlmodel.StructField{
		{Name: "element1", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}, Positional: true},
	}
	testCases := []struct {
		engine    TypeRules
		name      string
		raw       []ast.RawOutputColumn
		wantSpans []Span
	}{
		{
			name:      "a column the drivers cannot read is refused at its item",
			engine:    resultCheckingEngine{Engine: &fakedialect.Engine{}},
			raw:       []ast.RawOutputColumn{{ColumnName: "id"}, {ColumnName: "pair"}},
			wantSpans: []Span{{Start: 11, End: 15}},
		},
		{
			name:      "a star standing for a column the drivers cannot read is refused at the star",
			engine:    resultCheckingEngine{Engine: &fakedialect.Engine{}},
			raw:       []ast.RawOutputColumn{{ColumnName: "id"}, {ColumnName: "id", Name: "other"}, {IsStar: true}},
			wantSpans: []Span{{Start: 17, End: 18}},
		},
		{
			name:   "columns the drivers read are accepted",
			engine: resultCheckingEngine{Engine: &fakedialect.Engine{}},
			raw:    []ast.RawOutputColumn{{ColumnName: "id"}},
		},
		{
			name:   "a dialect that checks no result accepts every column",
			engine: &fakedialect.Engine{},
			raw:    []ast.RawOutputColumn{{ColumnName: "id"}, {ColumnName: "pair"}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, testCase.engine), testCase.engine)
			scope := resolver.NewRootScope(ScopeKindQuery)
			scope.AddTable(ast.TableReference{Name: "items", Schema: "public"}, ast.JoinInner, &sqlmodel.Table{
				Name: "items", Schema: "public",
				Columns: []sqlmodel.Column{
					{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
					{Name: "pair", SQLType: pair},
				},
			})

			projection := resolver.ResolveProjection(t.Context(), testCase.raw, itemSpans, scope)

			var spans []Span
			for _, problem := range projection.Problems {
				require.Equal(t, diag.CodeEmitterUnsupported, problem.Code)
				require.Equal(t, diag.SeverityError, problem.Severity)
				require.Contains(t, problem.Message, "the driver cannot read a struct whose fields have no names")
				require.Contains(t, problem.Message, `"pair"`)
				require.Equal(t, "name the fields", problem.Suggestion)
				spans = append(spans, problem.Span)
			}
			require.Equal(t, testCase.wantSpans, spans)
		})
	}
}
