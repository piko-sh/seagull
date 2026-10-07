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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestReturningIsRecordedForTheAnalyserToReport(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		keyword        string
		wantChange     ast.DataChange
		wantParameters int
	}{
		{
			name: "INSERT", sql: "INSERT INTO t (id, name) VALUES ({id:UInt64}, {name:String}) RETURNING id",
			wantChange: ast.DataChangeInsert, wantParameters: 2,
		},
		{name: "DELETE", sql: "DELETE FROM t WHERE id = {id:UInt64} RETURNING id", wantChange: ast.DataChangeDelete, wantParameters: 1},
		{
			name: "lightweight UPDATE", sql: "UPDATE t SET name = {name:String} WHERE id = {id:UInt64} RETURNING id",
			wantChange: ast.DataChangeUpdate, wantParameters: 2,
		},
		{
			name: "ALTER TABLE UPDATE", sql: "ALTER TABLE t UPDATE name = {name:String} WHERE id = {id:UInt64} RETURNING id",
			wantChange: ast.DataChangeUpdate, wantParameters: 2,
		},
		{name: "ALTER TABLE DELETE", sql: "ALTER TABLE t DELETE WHERE id = {id:UInt64} RETURNING id", wantChange: ast.DataChangeDelete, wantParameters: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			assert.True(t, analysis.HasReturning)
			assert.Equal(t, testCase.wantChange, analysis.DataChange)
			assert.Equal(t, len(testCase.sql)-len("RETURNING id"), analysis.ReturningLocation)
			assert.Len(t, analysis.ParameterReferences, testCase.wantParameters)
		})
	}
}

func TestStatementsWithoutReturningRecordNone(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"INSERT INTO t VALUES ({id:UInt64}, 'returning')",
		"DELETE FROM t WHERE id = {id:UInt64} SETTINGS mutations_sync = 2",
		"ALTER TABLE t UPDATE note = 'RETURNING' WHERE id IN (SELECT id FROM u)",
	} {
		analysis := analyse(t, sql)
		assert.False(t, analysis.HasReturning, sql)
	}
	assert.Equal(t, "note = 'RETURNING' WHERE id IN ( SELECT id FROM u )",
		detailOf(analyse(t, "ALTER TABLE t UPDATE note = 'RETURNING' WHERE id IN (SELECT id FROM u)")).asyncBody)
}

func TestLightweightUpdateIsAnalysed(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "UPDATE analytics.events ON CLUSTER main SET payload = {payload:String} WHERE id = {id:UInt64}")
	assert.Equal(t, ast.DataChangeUpdate, analysis.DataChange)
	assert.True(t, analysis.HasWhereClause)
	require.Len(t, analysis.FromTables, 1)
	assert.Equal(t, ast.TableReference{Schema: "analytics", Name: "events", Alias: ""}, analysis.FromTables[0])
	require.Len(t, analysis.ParameterReferences, 2)
	assert.Equal(t, sqlmodel.ParameterContextAssignment, analysis.ParameterReferences[0].Context)
	assert.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[1].Context)

	require.Error(t, analyseError(t, "UPDATE events payload = 1"), "SET is required")
}
