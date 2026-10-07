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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func wideValuesInsert(rows int) (string, *ast.RawQueryAnalysis) {
	var sql strings.Builder
	sql.WriteString("INSERT INTO items (id) VALUES\n")
	spans := make([]ast.TupleSpan, 0, rows)
	for row := range rows {
		start := sql.Len()
		fmt.Fprintf(&sql, "(%d, 'extra')", row)
		spans = append(spans, ast.TupleSpan{Span: ast.Span{Start: start, End: sql.Len()}, Items: []ast.Span{{}, {}}})
		sql.WriteString(",\n")
	}
	raw := &ast.RawQueryAnalysis{
		InsertTable:   "items",
		InsertTarget:  ast.TableReference{Name: "items"},
		InsertColumns: []string{"id"},
	}
	raw.Spans.Values = spans
	return sql.String(), raw
}

func TestInsertShapePassReportsAtMostTheQueryCapOfRows(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		rows        int
		wantSummary bool
	}{
		{name: "rows within the cap", rows: 3, wantSummary: false},
		{name: "rows past the cap", rows: MaxDiagnosticsPerQuery + 250, wantSummary: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sql, raw := wideValuesInsert(testCase.rows)
			pass := &insertShapePass{catalogue: referenceCatalogue(), dialect: newDialectFacts(&fakedialect.Engine{})}

			diagnostics := pass.Analyse(t.Context(), &DiagnosticContext{
				Query:       &sqlmodel.AnalysedQuery{Line: 4, SQL: sql},
				RawAnalysis: raw,
				Filename:    "items.sql",
			})

			if !testCase.wantSummary {
				require.Len(t, diagnostics, testCase.rows)
				require.Equal(t, 5, diagnostics[0].Line, "the first row sits on the line after the INSERT")
				require.Equal(t, 7, diagnostics[2].Line)
				return
			}
			require.Len(t, diagnostics, MaxDiagnosticsPerQuery+1)
			summary := diagnostics[len(diagnostics)-1]
			require.Equal(t, diag.CodeDiagnosticLimit, summary.Code)
			require.Equal(t, diag.SeverityError, summary.Severity, "the rows left out are errors")
			require.Equal(t, "and 250 more diagnostics were left out", summary.Message)
			require.Equal(t, 4, summary.Line)
		})
	}
}

func shapeEngine() *fakedialect.Engine {
	info := (&fakedialect.Engine{}).Info()
	info.RowIDAliases = []string{"rowid"}
	info.SystemColumns = []string{"_version"}
	return &fakedialect.Engine{InfoFunc: func() dialect.Info { return info }}
}

func shapeCatalogue() *sqlmodel.Catalogue {
	catalogue := referenceCatalogue()
	hidden := sqlmodel.NewColumn("rank", sqlmodel.NewIntegerType("int4", 32, false), false)
	hidden.Hidden = true
	catalogue.Schemas["public"].Tables["notes"] = fakedialect.NewTable("notes",
		sqlmodel.NewColumn("body", sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), false), hidden)
	return catalogue
}

func valuesRows(widths ...int) (string, []ast.TupleSpan) {
	var sql strings.Builder
	sql.WriteString("INSERT INTO t VALUES\n")
	spans := make([]ast.TupleSpan, 0, len(widths))
	for _, width := range widths {
		start := sql.Len()
		sql.WriteString("(" + strings.TrimSuffix(strings.Repeat("1, ", width), ", ") + ")")
		spans = append(spans, ast.TupleSpan{Span: ast.Span{Start: start, End: sql.Len()}, Items: make([]ast.Span, width)})
		sql.WriteString(",\n")
	}
	return sql.String(), spans
}

func TestInsertShapePassChecksTheShapeOfAnInsert(t *testing.T) {
	t.Parallel()

	selectOf := func(columns ...ast.RawOutputColumn) *ast.RawQueryAnalysis {
		return &ast.RawQueryAnalysis{OutputColumns: columns}
	}
	named := func(name string) ast.RawOutputColumn { return ast.RawOutputColumn{Name: name, ColumnName: name} }
	testCases := []struct {
		inserted *ast.RawQueryAnalysis
		name     string
		table    string
		columns  []string
		rows     []int
		prior    []diag.SourceError
		want     []string
		wantLine []int
	}{
		{name: "rows as wide as the listed columns", table: "items", columns: []string{"id", "name"}, rows: []int{2, 2}, want: []string{}},
		{name: "a row wider and a row narrower than the listed columns", table: "items", columns: []string{"id", "name"}, rows: []int{3, 2, 1},
			want: []string{
				"Q064 row 1 of the INSERT gives 3 values for the 2 target columns it lists, which the engine refuses",
				"Q064 row 3 of the INSERT gives 1 value for the 2 target columns it lists, which the engine refuses",
			},
			wantLine: []int{5, 7}},
		{name: "a row wider than the table when no column is listed", table: "items", rows: []int{3, 4},
			want:     []string{"Q064 row 2 of the INSERT gives 4 values for the 3 columns table items has, which the engine refuses"},
			wantLine: []int{6}},
		{name: "a hidden column is not one an insert writes", table: "notes", rows: []int{1, 2},
			want: []string{"Q064 row 2 of the INSERT gives 2 values for the 1 column table notes has, which the engine refuses"}, wantLine: []int{6}},
		{name: "a table the catalogue does not hold", table: "elsewhere", rows: []int{9}, want: []string{}},
		{name: "a SELECT narrower than the listed columns", table: "items", columns: []string{"id", "name"}, inserted: selectOf(named("id")),
			want: []string{"Q064 the SELECT of the INSERT gives 1 column for the 2 target columns it lists, which the engine refuses"}, wantLine: []int{4}},
		{name: "a SELECT with a star", table: "items", columns: []string{"id", "name"}, want: []string{},
			inserted: selectOf(named("id"), ast.RawOutputColumn{IsStar: true})},
		{name: "a SELECT with no column list", table: "items", inserted: selectOf(named("id")), want: []string{}},
		{name: "an unknown target column", table: "items", columns: []string{"id", "nmae"},
			want: []string{`Q001 unknown column "nmae" in table "items"`}, wantLine: []int{4}},
		{name: "an unknown target column reported already", table: "items", columns: []string{"nmae"},
			prior: []diag.SourceError{{Code: diag.CodeUnknownColumn, Message: `unknown column "nmae" in table "items"`}}, want: []string{}},
		{name: "the row identifier and a system column", table: "items", columns: []string{"rowid", "_version"}, want: []string{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sql, rows := valuesRows(testCase.rows...)
			raw := &ast.RawQueryAnalysis{
				InsertTable:   testCase.table,
				InsertTarget:  ast.TableReference{Name: testCase.table},
				InsertColumns: testCase.columns,
				InsertSelect:  testCase.inserted,
			}
			raw.Spans.Values = rows
			pass := &insertShapePass{catalogue: shapeCatalogue(), dialect: newDialectFacts(shapeEngine())}

			diagnostics := pass.Analyse(t.Context(), &DiagnosticContext{
				Query:               &sqlmodel.AnalysedQuery{Line: 4, SQL: sql},
				RawAnalysis:         raw,
				Filename:            "items.sql",
				ExistingDiagnostics: testCase.prior,
			})

			require.Equal(t, testCase.want, describeDiagnostics(diagnostics))
			for index, line := range testCase.wantLine {
				require.Equal(t, line, diagnostics[index].Line)
			}
		})
	}
}

func TestInsertShapePassChecksOnlyAnInsert(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		subject *DiagnosticContext
		name    string
	}{
		{name: "no context", subject: nil},
		{name: "no analysis", subject: &DiagnosticContext{Query: &sqlmodel.AnalysedQuery{}}},
		{name: "a statement that is no insert", subject: &DiagnosticContext{Query: &sqlmodel.AnalysedQuery{}, RawAnalysis: &ast.RawQueryAnalysis{}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &insertShapePass{catalogue: shapeCatalogue(), dialect: nil}

			require.Empty(t, pass.Analyse(t.Context(), testCase.subject))
		})
	}
}
