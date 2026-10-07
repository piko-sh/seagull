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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func TestSpanRecorderRecordsTheFirstClauseOfEachKind(t *testing.T) {
	t.Parallel()

	recorder := dialectkit.NewSpanRecorder(100)
	recorder.Record(dialectkit.SpanWhere, 110, 120)
	recorder.Record(dialectkit.SpanWhere, 130, 140)
	recorder.Extend(dialectkit.SpanPagination, 150, 155)
	recorder.Extend(dialectkit.SpanPagination, 160, 170)
	recorder.AddRowShaping(140, 145)
	recorder.RecordProjectionItems([]ast.ItemSpan{recorder.Item(101, 103, 104, 108, false)})
	recorder.RecordProjectionItems(nil)

	spans := recorder.Finish(&ast.RawQueryAnalysis{})

	require.True(t, recorder.Recorded(dialectkit.SpanWhere))
	require.False(t, recorder.Recorded(dialectkit.SpanHaving))
	require.Equal(t, ast.Span{Start: 10, End: 20}, spans.Where)
	require.Equal(t, ast.Span{Start: 50, End: 70}, spans.Pagination)
	require.Equal(t, []ast.Span{{Start: 40, End: 45}}, spans.RowShaping)
	require.Equal(t, []ast.ItemSpan{{Span: ast.Span{Start: 1, End: 8}, Expression: ast.Span{Start: 1, End: 3}, Alias: ast.Span{Start: 4, End: 8}}},
		spans.ProjectionItems)
}

func TestSpanRecorderKeepsTheClausesTheStatementOwns(t *testing.T) {
	t.Parallel()

	recorder := dialectkit.NewSpanRecorder(0)
	for _, field := range []dialectkit.SpanField{dialectkit.SpanProjection, dialectkit.SpanWhere, dialectkit.SpanWhereCondition,
		dialectkit.SpanOrderBy, dialectkit.SpanReturning} {
		recorder.Record(field, int(field)*10, int(field)*10+5)
	}
	recorder.AddValuesRow(ast.TupleSpan{Items: nil, Span: ast.Span{Start: 200, End: 210}})

	insert := recorder.Finish(&ast.RawQueryAnalysis{DataChange: ast.DataChangeInsert})
	update := recorder.Finish(&ast.RawQueryAnalysis{DataChange: ast.DataChangeUpdate})
	compound := recorder.Finish(&ast.RawQueryAnalysis{CompoundBranches: []ast.RawCompoundBranch{{}}})
	query := recorder.Finish(&ast.RawQueryAnalysis{})

	require.Len(t, insert.Values, 1)
	require.False(t, insert.Where.Known())
	require.True(t, insert.Returning.Known())
	require.True(t, update.Where.Known())
	require.False(t, update.Projection.Known())
	require.True(t, compound.OrderBy.Known())
	require.False(t, compound.Projection.Known())
	require.True(t, query.Projection.Known())
	require.Empty(t, query.Values)
}

func TestSpanRecorderKeepsTheLastOrderByPlaceForQueries(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		analysis *ast.RawQueryAnalysis
		name     string
		places   []int
		want     ast.Span
	}{
		{name: "a SELECT keeps its place", analysis: &ast.RawQueryAnalysis{}, places: []int{120}, want: ast.Span{Start: 20, End: 20}},
		{
			name:     "a compound query keeps the place after its last branch",
			analysis: &ast.RawQueryAnalysis{CompoundBranches: []ast.RawCompoundBranch{{}}},
			places:   []int{120, 160},
			want:     ast.Span{Start: 60, End: 60},
		},
		{
			name:     "an INSERT keeps none",
			analysis: &ast.RawQueryAnalysis{DataChange: ast.DataChangeInsert},
			places:   []int{120},
			want:     ast.Span{},
		},
		{name: "a place at the statement start is not recorded", analysis: &ast.RawQueryAnalysis{}, places: []int{100}, want: ast.Span{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := dialectkit.NewSpanRecorder(100)
			for _, place := range testCase.places {
				recorder.RecordOrderByPlace(place)
			}

			require.Equal(t, testCase.want, recorder.Finish(testCase.analysis).OrderByPlace)
		})
	}
}

func TestNilSpanRecorderRecordsNothing(t *testing.T) {
	t.Parallel()

	var recorder *dialectkit.SpanRecorder
	recorder.Record(dialectkit.SpanWhere, 1, 2)
	recorder.Extend(dialectkit.SpanPagination, 1, 2)
	recorder.AddRowShaping(1, 2)
	recorder.RecordProjectionItems([]ast.ItemSpan{{}})
	recorder.AddValuesRow(ast.TupleSpan{})
	recorder.RecordOrderByPlace(5)

	require.False(t, recorder.Recorded(dialectkit.SpanWhere))
	require.Equal(t, ast.StatementSpans{}, recorder.Finish(&ast.RawQueryAnalysis{}))
	require.Equal(t, ast.ItemSpan{}, recorder.Item(1, 2, 3, 4, false))
}

func TestValuesRowSplitsAtTopLevelCommas(t *testing.T) {
	t.Parallel()

	text := "VALUES ($1, f($2, [3, 4]), 5)"
	tokens := spanTokens(t, text)
	open, closing := -1, -1
	for index, token := range tokens {
		if token.Raw == "(" && open < 0 {
			open = index
		}
		if token.Raw == ")" {
			closing = index
		}
	}
	kinds := dialectkit.ListTokens[spanKind]{Comma: spanOther, Openers: []spanKind{spanLeft}, Closers: []spanKind{spanRight}}
	for index := range tokens {
		if tokens[index].Raw == "," {
			tokens[index].Kind = spanOther
		} else if tokens[index].Kind == spanOther {
			tokens[index].Kind = spanWord
		}
	}

	row := dialectkit.ValuesRow(tokens, open, closing, kinds, dialectkit.NewSpanRecorder(0))

	require.Equal(t, "($1, f($2, [3, 4]), 5)", spanText(text, 0, row.Span))
	items := make([]string, 0, len(row.Items))
	for _, item := range row.Items {
		items = append(items, spanText(text, 0, item))
	}
	require.Equal(t, []string{"$1", "f($2, [3, 4])", "5"}, items)
}
