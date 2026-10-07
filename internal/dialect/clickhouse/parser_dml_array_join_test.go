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

package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestArrayJoinEntriesRecordWhatTheyUnfold(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantSource ast.Expression
		wantAlias  string
		wantColumn string
		wantPrefix string
	}{
		{
			name:       "a column without an alias unfolds in place with its flattened columns",
			sql:        "SELECT id FROM flights ARRAY JOIN legs",
			wantSource: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "legs", Span: ast.Span{Start: 34, End: 38}},
			wantAlias:  "legs", wantColumn: "legs", wantPrefix: "legs.",
		},
		{
			name:       "an aliased column names its element by the alias alone",
			sql:        "SELECT leg FROM flights ARRAY JOIN legs AS leg",
			wantSource: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "legs", Span: ast.Span{Start: 35, End: 39}},
			wantAlias:  "leg", wantColumn: "legs", wantPrefix: "",
		},
		{
			name:       "a flattened column of a nested structure is one column",
			sql:        "SELECT id FROM flights ARRAY JOIN legs.colony",
			wantSource: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "legs.colony", Span: ast.Span{Start: 34, End: 45}},
			wantAlias:  "legs.colony", wantColumn: "legs.colony", wantPrefix: "legs.colony.",
		},
		{
			name:       "a column qualified by its table keeps the qualifier",
			sql:        "SELECT id FROM flights AS f ARRAY JOIN f.legs",
			wantSource: &ast.ColumnRefExpression{TableAlias: "f", ColumnName: "legs", Span: ast.Span{Start: 39, End: 45}},
			wantAlias:  "legs", wantColumn: "legs", wantPrefix: "legs.",
		},
		{
			name:       "an expression source is parsed for the analyser",
			sql:        "SELECT x FROM flights ARRAY JOIN [1, 2] AS x",
			wantSource: &ast.FunctionCallExpression{FunctionName: "array", Arguments: []ast.Expression{&ast.LiteralExpression{TypeName: engineNameUInt8}, &ast.LiteralExpression{TypeName: engineNameUInt8}}},
			wantAlias:  "x", wantColumn: "", wantPrefix: "",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.Len(t, analysis.ArrayJoinClauses, 1)
			clause := analysis.ArrayJoinClauses[0]
			require.Equal(t, testCase.wantAlias, clause.Alias)
			require.Equal(t, testCase.wantColumn, clause.SourceColumn)
			require.Equal(t, testCase.wantPrefix, clause.ElementPrefix)
			if call, isCall := clause.Source.(*ast.FunctionCallExpression); isCall {
				call.Span = ast.Span{}
			}
			require.Equal(t, testCase.wantSource, clause.Source)
		})
	}
}
