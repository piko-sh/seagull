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

package analyse

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/goldenjson"
)

func analysePostgres(t *testing.T, sql string) (*sqlmodel.AnalysedQuery, []diag.SourceError) {
	t.Helper()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["items"] = fakedialect.NewTable("items",
		sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}},
		sqlmodel.Column{Name: "kind", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
		sqlmodel.Column{Name: "owner", SQLType: sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}},
	)
	analyser := NewQueryAnalyser(postgres.New(), catalogue, directive.PrefixPolicy{Prefixes: namespace.Default(), RequireCanonical: false})
	return analyser.AnalyseQuery(t.Context(), sqlfile.QueryBlock{SQL: sql, StartLine: 1}, "items.sql")
}

func compactTemplate(t *testing.T, template *sqlmodel.SQLTemplate) string {
	t.Helper()

	if template == nil {
		return ""
	}
	return goldenjson.Template(*template)
}

func diagnosticWithCode(diagnostics []diag.SourceError, code string) (diag.SourceError, bool) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return diagnostic, true
		}
	}
	return diag.SourceError{}, false
}

func TestAnalyseQueryBuildsTheTemplates(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		template string
		count    string
		target   sqlmodel.TableName
	}{
		{
			name:     "a static query marks its placeholders and output items",
			sql:      "-- sgl.query(name: GetItem, command: one)\nSELECT id, kind FROM items WHERE id = $1",
			template: "SELECT ⟨c0:id⟩, ⟨c1:kind⟩ FROM items WHERE id = ⟨p0⟩⟨arguments p0⟩",
		},
		{
			name:     "a runtime builder query is counted",
			sql:      "-- sgl.query(name: ListItems, command: many, dynamic: runtime)\nSELECT id, kind FROM items WHERE owner = $1 ORDER BY id",
			template: "SELECT ⟨c0:id⟩, ⟨c1:kind⟩ FROM items WHERE ⟨where:owner = ⟨p0⟩⟩ ⟨order:ORDER BY id⟩⟨pagination⟩⟨arguments p0⟩",
			count:    "SELECT COUNT(*) FROM items WHERE ⟨where:owner = ⟨p0⟩⟩⟨arguments p0⟩",
		},
		{
			name:     "an INSERT names its table as written",
			sql:      "-- sgl.query(name: AddItem, command: exec)\nINSERT INTO public.items (id, kind) VALUES ($1, $2)",
			template: "INSERT INTO public.items (id, kind) VALUES ⟨values:(⟨p0⟩, ⟨p1⟩)⟩⟨arguments p0 p1⟩",
			target:   sqlmodel.TableName{Schema: "public", Name: "items"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query, diagnostics := analysePostgres(t, testCase.sql)

			require.NotNil(t, query, "%v", diagnostics)
			require.Equal(t, testCase.template, compactTemplate(t, &query.Template))
			require.Equal(t, testCase.count, compactTemplate(t, query.CountTemplate))
			require.Equal(t, testCase.target, query.InsertTarget)
		})
	}
}

func TestAnalyseQueryPlacesProblemsWhereTheyAreWritten(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		sql    string
		code   string
		line   int
		column int
	}{
		{
			name:   "a slice parameter outside an IN list is reported at its placeholder",
			sql:    "-- sgl.query(name: Pick, command: many)\n-- $1 as sgl.param(ids, kind: slice)\nSELECT id FROM items WHERE id = ANY($1)",
			code:   diag.CodeSliceParameterShape,
			line:   3,
			column: 37,
		},
		{
			name:   "a parse error is reported at the token the dialect stopped at",
			sql:    "-- sgl.query(name: Broken, command: many)\nSELECT id FROM items WHERE id = 1 AND FROM",
			code:   diag.CodeParseError,
			line:   2,
			column: 39,
		},
		{
			name:   "an unknown projected column is reported at the column",
			sql:    "-- sgl.query(name: Typo, command: many)\nSELECT id, nmae FROM items",
			code:   diag.CodeUnknownColumn,
			line:   2,
			column: 12,
		},
		{
			name:   "a placeholder before the query's statement is reported where it is",
			sql:    "-- sgl.query(name: Scoped, command: many)\nSELECT set_config('app.owner', $1, true);\nSELECT id FROM items WHERE owner = $1",
			code:   diag.CodeParametersBeforeStatement,
			line:   2,
			column: 32,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, diagnostics := analysePostgres(t, testCase.sql)

			diagnostic, found := diagnosticWithCode(diagnostics, testCase.code)
			require.True(t, found, "%v", diagnostics)
			require.Equal(t, testCase.line, diagnostic.Line)
			require.Equal(t, testCase.column, diagnostic.Column)
		})
	}
}
