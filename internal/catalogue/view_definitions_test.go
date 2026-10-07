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

package catalogue

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/sqlmodel"
)

func introspectedSQLiteCatalogue(t *testing.T) *sqlmodel.Catalogue {
	t.Helper()

	engine := sqlite.New()
	declared := func(name string) sqlmodel.SQLType { return engine.NormaliseTypeName(name) }
	authorName := sqlmodel.NewColumn("name", declared("TEXT"), false)
	authorName.GoTypeOverride = &sqlmodel.GoType{Package: "example.com/app/names", Name: "Name"}
	catalogue := &sqlmodel.Catalogue{
		Schemas: map[string]*sqlmodel.Schema{
			"main": {
				Name: "main",
				Tables: map[string]*sqlmodel.Table{
					"authors": {Name: "authors", Schema: "main", Columns: []sqlmodel.Column{
						sqlmodel.NewColumn("id", declared("INTEGER"), false), authorName,
					}},
				},
				Views: map[string]*sqlmodel.View{
					"counted": {Name: "counted", Schema: "main", Definition: "SELECT count(*) FROM authors",
						Columns: []sqlmodel.Column{sqlmodel.NewColumn("total", declared(""), true)}},
					"named": {Name: "named", Schema: "main", Definition: "SELECT id, name AS author FROM authors",
						Columns: []sqlmodel.Column{sqlmodel.NewColumn("id", declared("INTEGER"), true), sqlmodel.NewColumn("author", declared("TEXT"), true)}},
					"stacked": {Name: "stacked", Schema: "main", Definition: "SELECT total FROM counted",
						Columns: []sqlmodel.Column{sqlmodel.NewColumn("total", declared(""), true)}},
					"broken": {Name: "broken", Schema: "main", Definition: "SELECT missing FROM nowhere",
						Columns: []sqlmodel.Column{sqlmodel.NewColumn("missing", declared(""), true)}},
					"narrower": {Name: "narrower", Schema: "main", Definition: "SELECT id, name FROM authors",
						Columns: []sqlmodel.Column{sqlmodel.NewColumn("id", declared(""), true)}},
					"unparsable": {Name: "unparsable", Schema: "main", Definition: "SELECT FROM WHERE (",
						Columns: []sqlmodel.Column{sqlmodel.NewColumn("x", declared(""), true)}},
					"undefined": {Name: "undefined", Schema: "main",
						Columns: []sqlmodel.Column{sqlmodel.NewColumn("id", declared(""), true)}},
				},
			},
		},
	}
	Adopt(catalogue, new(engine.Info()))
	return catalogue
}

func TestTypeViewsFromDefinitionsTypesViewsAsReplayingThemDoes(t *testing.T) {
	t.Parallel()

	catalogue := introspectedSQLiteCatalogue(t)
	_, err := TypeViewsFromDefinitions(t.Context(), sqlite.New(), catalogue)
	require.NoError(t, err)
	views := catalogue.Schemas["main"].Views

	counted := views["counted"].Columns[0]
	assert.Equal(t, "total", counted.Name, "a view keeps its own column names")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, counted.SQLType.Category, "an expression's declared blob gives way to the analysed type")
	assert.False(t, counted.Nullable, "count(*) is never NULL")

	named := views["named"].Columns
	assert.False(t, named[0].Nullable)
	assert.False(t, named[1].Nullable)
	assert.Equal(t, "author", named[1].Name)
	require.NotNil(t, named[1].GoTypeOverride, "a column projected unchanged carries its table column's go_type")
	assert.Equal(t, "Name", named[1].GoTypeOverride.Name)

	stacked := views["stacked"].Columns[0]
	assert.Equal(t, sqlmodel.TypeCategoryInteger, stacked.SQLType.Category, "a view over a view is typed once the view below is")
	assert.False(t, stacked.Nullable)
}

func TestTypeViewsFromDefinitionsKeepsWhatTheAnalysisCannotConclude(t *testing.T) {
	t.Parallel()

	catalogue := introspectedSQLiteCatalogue(t)
	_, err := TypeViewsFromDefinitions(t.Context(), sqlite.New(), catalogue)
	require.NoError(t, err)
	views := catalogue.Schemas["main"].Views

	tests := []struct {
		name string
		view string
	}{
		{name: "a definition that reads unknown objects", view: "broken"},
		{name: "a result of another width", view: "narrower"},
		{name: "a definition that does not parse", view: "unparsable"},
		{name: "a view without a definition", view: "undefined"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			column := views[testCase.view].Columns[0]
			assert.Equal(t, sqlmodel.TypeCategoryBinary, column.SQLType.Category)
			assert.True(t, column.Nullable)
		})
	}
}

func TestTypeViewsFromDefinitionsStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(context.Canceled)

	_, err := TypeViewsFromDefinitions(ctx, sqlite.New(), introspectedSQLiteCatalogue(t))
	require.ErrorIs(t, err, context.Canceled)
}

func TestTypeViewsFromDefinitionsReturnsTheCauseOfACancellation(t *testing.T) {
	t.Parallel()

	cause := errors.New("the run was stopped")
	testCases := []struct {
		name  string
		typed func(ctx context.Context) error
	}{
		{name: "without diagnostics", typed: func(ctx context.Context) error {
			_, err := TypeViewsFromDefinitions(ctx, sqlite.New(), introspectedSQLiteCatalogue(t))
			return err
		}},
		{name: "with diagnostics", typed: func(ctx context.Context) error {
			_, err := TypeViewsFromDefinitions(ctx, sqlite.New(), introspectedSQLiteCatalogue(t))
			return err
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(cause)

			err := testCase.typed(ctx)

			require.ErrorIs(t, err, cause)
			require.Contains(t, err.Error(), "typing views")
		})
	}
}

func TestTypeViewsFromDefinitionsReportsWhatTheDialectCannotSettle(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		parse     func(ctx context.Context, sql string) ([]ast.ParsedStatement, error)
		analyse   func(calls int) *ast.RawQueryAnalysis
		wantCodes []string
		wantText  string
	}{
		{
			name:      "a panic parsing a definition",
			parse:     func(context.Context, string) ([]ast.ParsedStatement, error) { panic("parse") },
			analyse:   nil,
			wantCodes: []string{diag.CodeRecoveredPanic},
			wantText:  "parsing the definition of view main.flip",
		},
		{
			name:      "a panic analysing a definition",
			parse:     nil,
			analyse:   func(int) *ast.RawQueryAnalysis { panic("analyse") },
			wantCodes: []string{diag.CodeRecoveredPanic},
			wantText:  "analysing the definition of view main.flip",
		},
		{
			name:  "a definition whose type keeps changing",
			parse: nil,
			analyse: func(calls int) *ast.RawQueryAnalysis {
				typeName := "integer"
				if calls%2 == 0 {
					typeName = "text"
				}
				return &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{literalColumn(typeName)}, ReadOnly: true}
			},
			wantCodes: []string{diag.CodeViewDerivationUnsettled},
			wantText:  "did not settle",
		},
		{
			name:  "an analysis holding a nil node is reported once",
			parse: nil,
			analyse: func(int) *ast.RawQueryAnalysis {
				return &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Name: "value", Expression: (*ast.CastExpression)(nil)}}, ReadOnly: true}
			},
			wantCodes: []string{diag.CodeInternalNilGuard},
			wantText:  "view main.flip",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			engine := sqlFunctionEngine(nil, "")
			engine.SchemaSearchPathFunc = func() []string { return []string{"main"} }
			engine.ParseStatementsFunc = func(ctx context.Context, sql string) ([]ast.ParsedStatement, error) {
				if testCase.parse != nil {
					return testCase.parse(ctx, sql)
				}
				return []ast.ParsedStatement{{Location: 0, Length: len(sql)}}, nil
			}
			engine.AnalyseQueryFunc = func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
				calls++
				return testCase.analyse(calls), nil
			}
			catalogue := &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Views: map[string]*sqlmodel.View{
				"flip": {Name: "flip", Schema: "main", Definition: "SELECT flip",
					Columns: []sqlmodel.Column{sqlmodel.NewColumn("value", sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true)}},
			}}}}
			Adopt(catalogue, new(engine.Info()))

			var diagnostics []diag.SourceError
			var err error
			require.NotPanics(t, func() {
				diagnostics, err = TypeViewsFromDefinitions(t.Context(), engine, catalogue)
			})

			require.NoError(t, err)
			codes := make([]string, 0, len(diagnostics))
			for _, diagnostic := range diagnostics {
				codes = append(codes, diagnostic.Code)
				assert.Contains(t, diagnostic.Message, testCase.wantText)
			}
			assert.Equal(t, testCase.wantCodes, codes)
		})
	}
}
