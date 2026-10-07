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

package dialectkit_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var errAnalysis = errors.New("analysis failed")

type probeParser struct {
	analysis *ast.RawQueryAnalysis
	err      error
}

func TestChangingRowsRecordsTheChangeOnTheAnalysis(t *testing.T) {
	t.Parallel()

	cases := []struct {
		parser    *probeParser
		wantErr   error
		name      string
		wantNil   bool
		wantKinds ast.DataChange
	}{
		{name: "an analysis", parser: &probeParser{analysis: &ast.RawQueryAnalysis{}, err: nil}, wantKinds: ast.DataChangeUpdate},
		{name: "an analysis with an error", parser: &probeParser{analysis: &ast.RawQueryAnalysis{}, err: errAnalysis},
			wantErr: errAnalysis, wantKinds: ast.DataChangeUpdate},
		{name: "no analysis", parser: &probeParser{analysis: nil, err: errAnalysis}, wantErr: errAnalysis, wantNil: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analyse := dialectkit.ChangingRows(ast.DataChangeUpdate, func(parser *probeParser) (*ast.RawQueryAnalysis, error) {
				return parser.analysis, parser.err
			})

			analysis, err := analyse(testCase.parser)

			assert.ErrorIs(t, err, testCase.wantErr)
			if testCase.wantNil {
				assert.Nil(t, analysis)
				return
			}
			assert.Equal(t, testCase.wantKinds, analysis.DataChange)
		})
	}
}

func TestKeepFirstKeepsAPrefixWithNoSpareCapacity(t *testing.T) {
	t.Parallel()

	items := []int{1, 2, 3, 4}
	cases := []struct {
		name  string
		want  []int
		count int
	}{
		{name: "none", count: 0, want: nil},
		{name: "a negative count", count: -1, want: nil},
		{name: "some", count: 2, want: []int{1, 2}},
		{name: "all", count: 4, want: []int{1, 2, 3, 4}},
		{name: "more than there are", count: 9, want: []int{1, 2, 3, 4}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			kept := dialectkit.KeepFirst(items, testCase.count)

			assert.Equal(t, testCase.want, kept)
			assert.Equal(t, len(kept), cap(kept))
		})
	}
}

func TestIsStarColumnReadsTheStarFlag(t *testing.T) {
	t.Parallel()

	assert.True(t, dialectkit.IsStarColumn(ast.RawOutputColumn{IsStar: true}))
	assert.False(t, dialectkit.IsStarColumn(ast.RawOutputColumn{Name: "id"}))
}

func TestColumnsFromNamesBuildsNullableUnknownColumns(t *testing.T) {
	t.Parallel()

	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	cases := []struct {
		name  string
		names []string
		want  []sqlmodel.Column
	}{
		{name: "no names", names: nil, want: nil},
		{name: "two names", names: []string{"id", "name"},
			want: []sqlmodel.Column{sqlmodel.NewColumn("id", unknown, true), sqlmodel.NewColumn("name", unknown, true)}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, dialectkit.ColumnsFromNames(testCase.names))
		})
	}
}

func TestOverlayViewColumnNamesRenamesTheProjection(t *testing.T) {
	t.Parallel()

	idReference := &ast.ColumnRefExpression{ColumnName: "id"}
	projection := func() []ast.RawOutputColumn {
		return []ast.RawOutputColumn{
			{Name: "id", Expression: idReference, TableAlias: "t", ColumnName: "id"},
			{Name: "", IsStar: true},
		}
	}
	renamed := ast.RawOutputColumn{Name: "key", Expression: idReference, TableAlias: "t", ColumnName: "id"}
	cases := []struct {
		name       string
		names      []string
		want       []ast.RawOutputColumn
		undeclared dialectkit.UndeclaredColumns
	}{
		{name: "a shorter list keeps the rest", names: []string{"key"}, undeclared: dialectkit.UndeclaredColumnsKept,
			want: []ast.RawOutputColumn{renamed, {Name: "", IsStar: true}}},
		{name: "a shorter list drops the rest", names: []string{"key"}, undeclared: dialectkit.UndeclaredColumnsDropped,
			want: []ast.RawOutputColumn{renamed}},
		{name: "a list that names a star", names: []string{"key", "rest"}, undeclared: dialectkit.UndeclaredColumnsDropped,
			want: []ast.RawOutputColumn{renamed, {Name: "rest"}}},
		{name: "a longer list adds name-only columns", names: []string{"key", "rest", "extra"},
			undeclared: dialectkit.UndeclaredColumnsKept, want: []ast.RawOutputColumn{renamed, {Name: "rest"}, {Name: "extra"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{OutputColumns: projection()}

			dialectkit.OverlayViewColumnNames(analysis, testCase.names, testCase.undeclared)

			require.Equal(t, testCase.want, analysis.OutputColumns)
		})
	}
}

func TestOverlayViewColumnNamesLeavesANilAnalysisAlone(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		dialectkit.OverlayViewColumnNames(nil, []string{"id"}, dialectkit.UndeclaredColumnsDropped)
	})
}
