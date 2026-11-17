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

package check

import (
	"cmp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type fullTextDetail struct {
	searches []ast.FullTextSearch
}

func (fullTextDetail) EngineDetail() {}

func (d fullTextDetail) FullTextSearches() []ast.FullTextSearch { return d.searches }

func fullTextSubject(t *testing.T, catalogue *sqlmodel.Catalogue, columns ...ast.ColumnReference) *DiagnosticContext {
	t.Helper()
	scope := resolve.NewScopeChain(resolve.ScopeKindQuery, nil)
	for _, name := range []string{"posts", "notes"} {
		table := catalogue.LookupTable("main", name)
		require.NotNil(t, table)
		scope.AddTable(ast.TableReference{Name: name, Alias: name[:1]}, ast.JoinKind{}, table)
	}
	return &DiagnosticContext{
		Query:       &sqlmodel.AnalysedQuery{Name: "Search", Line: 3},
		RawAnalysis: &ast.RawQueryAnalysis{Detail: fullTextDetail{searches: []ast.FullTextSearch{{Columns: columns}}}},
		Scope:       scope,
		Filename:    "search.sql",
	}
}

func fullTextCatalogue() *sqlmodel.Catalogue {
	catalogue := fakedialect.NewCatalogue("main")
	catalogue.Schemas["main"].Tables["posts"] = &sqlmodel.Table{
		Name:    "posts",
		Columns: []sqlmodel.Column{{Name: "Title"}, {Name: "body"}, {Name: "id"}},
		Indexes: []sqlmodel.Index{
			{Name: "posts_title", Columns: []string{"title"}, IsFullText: true},
			{Name: "posts_both", Columns: []string{"body", "title"}, IsFullText: true},
			{Name: "posts_id", Columns: []string{"id"}},
		},
	}
	catalogue.Schemas["main"].Tables["notes"] = &sqlmodel.Table{
		Name:    "notes",
		Columns: []sqlmodel.Column{{Name: "body"}, {Name: "id"}},
	}
	return catalogue
}

func TestFullTextIndexPass(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		code    string
		columns []ast.ColumnReference
		warns   bool
	}{
		{name: "an index of exactly the columns in another order serves the search", warns: false,
			columns: []ast.ColumnReference{{TableAlias: "p", ColumnName: "title"}, {TableAlias: "p", ColumnName: "BODY"}}},
		{name: "a single-column index serves a search of that column", warns: false,
			columns: []ast.ColumnReference{{TableAlias: "p", ColumnName: "title"}}},
		{name: "an index of more columns does not serve the search", warns: true,
			columns: []ast.ColumnReference{{TableAlias: "p", ColumnName: "body"}}},
		{name: "a column that is not indexed for full-text search is reported", warns: true,
			columns: []ast.ColumnReference{{TableAlias: "p", ColumnName: "id"}}},
		{name: "an unqualified column only one table holds is placed in it", warns: false,
			columns: []ast.ColumnReference{{ColumnName: "title"}}},
		{name: "an unqualified column two tables hold is not placed", warns: false,
			columns: []ast.ColumnReference{{ColumnName: "body"}}},
		{name: "columns of two tables are refused", warns: true, code: diag.CodeFullTextWithoutIndex,
			columns: []ast.ColumnReference{{TableAlias: "p", ColumnName: "title"}, {TableAlias: "n", ColumnName: "body"}}},
		{name: "an unknown alias is not placed", warns: false,
			columns: []ast.ColumnReference{{TableAlias: "x", ColumnName: "title"}}},
		{name: "a column its table lacks is unknown", warns: true, code: diag.CodeUnknownColumn,
			columns: []ast.ColumnReference{{TableAlias: "p", ColumnName: "missing"}}},
		{name: "an unqualified column no table holds is left to a nested query's tables", warns: false,
			columns: []ast.ColumnReference{{ColumnName: "missing"}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fullTextCatalogue()
			pass := &fullTextIndexPass{catalogue: catalogue}
			diagnostics := pass.Analyse(t.Context(), fullTextSubject(t, catalogue, testCase.columns...))

			if !testCase.warns {
				assert.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			assert.Equal(t, cmp.Or(testCase.code, diag.CodeFullTextWithoutIndex), diagnostics[0].Code)
			assert.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
			assert.Equal(t, 3, diagnostics[0].Line)
		})
	}
}

func TestFullTextIndexPassSkipsWhatItCannotCheck(t *testing.T) {
	t.Parallel()

	catalogue := fullTextCatalogue()
	subject := fullTextSubject(t, catalogue, ast.ColumnReference{TableAlias: "p", ColumnName: "id"})

	assert.Empty(t, (&fullTextIndexPass{catalogue: nil}).Analyse(t.Context(), subject), "no catalogue")
	assert.Empty(t, (&fullTextIndexPass{catalogue: catalogue}).Analyse(t.Context(), &DiagnosticContext{}), "no analysis")
	assert.Empty(t, (&fullTextIndexPass{catalogue: catalogue}).Analyse(t.Context(),
		&DiagnosticContext{RawAnalysis: &ast.RawQueryAnalysis{}}), "a dialect that records no searches")

	subject.Scope = nil
	assert.Empty(t, (&fullTextIndexPass{catalogue: catalogue}).Analyse(t.Context(), subject), "no scope")
}
