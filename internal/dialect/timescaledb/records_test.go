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

package timescaledb_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestSetReturningFunctionsAreTableValuedWithTheirColumns(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	byteCounts := []string{"table_bytes", "index_bytes", "toast_bytes", "total_bytes"}
	compressionCounts := []string{
		"before_compression_table_bytes", "before_compression_index_bytes", "before_compression_toast_bytes",
		"before_compression_total_bytes", "after_compression_table_bytes", "after_compression_index_bytes",
		"after_compression_toast_bytes", "after_compression_total_bytes",
	}
	joined := func(parts ...[]string) []string {
		var names []string
		for _, part := range parts {
			names = append(names, part...)
		}
		return names
	}
	testCases := []struct {
		function string
		want     []string
	}{
		{function: "topn", want: []string{"topn"}},
		{function: "live_ranges", want: []string{"start", "end"}},
		{function: "dead_ranges", want: []string{"start", "end"}},
		{function: "state_timeline", want: []string{"state", "start_time", "end_time"}},
		{function: "state_int_timeline", want: []string{"state", "start_time", "end_time"}},
		{function: "state_periods", want: []string{"start_time", "end_time"}},
		{function: "interpolated_state_timeline", want: []string{"state", "start_time", "end_time"}},
		{function: "interpolated_state_periods", want: []string{"start_time", "end_time"}},
		{function: "show_chunks", want: []string{"show_chunks"}},
		{function: "drop_chunks", want: []string{"drop_chunks"}},
		{function: "hypertable_detailed_size", want: joined(byteCounts, []string{"node_name"})},
		{function: "hypertable_approximate_detailed_size", want: byteCounts},
		{function: "chunks_detailed_size", want: joined([]string{"chunk_schema", "chunk_name"}, byteCounts, []string{"node_name"})},
		{function: "hypertable_compression_stats", want: joined([]string{"total_chunks", "number_compressed_chunks"}, compressionCounts, []string{"node_name"})},
		{function: "hypertable_columnstore_stats", want: joined([]string{"total_chunks", "number_compressed_chunks"}, compressionCounts, []string{"node_name"})},
		{function: "chunk_compression_stats", want: joined([]string{"chunk_schema", "chunk_name", "compression_status"}, compressionCounts, []string{"node_name"})},
		{function: "chunk_columnstore_stats", want: joined([]string{"chunk_schema", "chunk_name", "compression_status"}, compressionCounts, []string{"node_name"})},
		{function: "into_values", want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.function, func(t *testing.T) {
			t.Parallel()

			columns := engine.TableValuedFunctionColumns(testCase.function)
			var names []string
			for _, column := range columns {
				names = append(names, column.Name)
			}
			require.Equal(t, testCase.want, names)
		})
	}
}

func TestSetReturningFunctionColumnsAreTyped(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	timestamp := engine.NormaliseTypeName("timestamptz")
	testCases := []struct {
		function string
		want     []ast.ScopedColumn
	}{
		{function: "topn", want: []ast.ScopedColumn{{Name: "topn", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: false}}},
		{function: "live_ranges", want: []ast.ScopedColumn{
			{Name: "start", SQLType: timestamp, Nullable: true},
			{Name: "end", SQLType: timestamp, Nullable: true},
		}},
		{function: "state_int_timeline", want: []ast.ScopedColumn{
			{Name: "state", SQLType: engine.NormaliseTypeName("int8"), Nullable: true},
			{Name: "start_time", SQLType: timestamp, Nullable: true},
			{Name: "end_time", SQLType: timestamp, Nullable: true},
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.function, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, engine.TableValuedFunctionColumns(testCase.function))
		})
	}
}

func TestTopNInAFromClauseNamesItsColumnAfterTheAlias(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT value FROM agg, topn(state, 5) AS value")
	require.NoError(t, err)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	require.Len(t, analysis.RawTableValuedFunctions, 1)
	require.Equal(t, []ast.TVFColumnDefinition{{Name: "value", TypeName: ""}}, analysis.RawTableValuedFunctions[0].ColumnDefinitions)
}
