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
)

func analyseWith(t *testing.T, engine *Dialect, sql string) *ast.RawQueryAnalysis {
	t.Helper()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	require.NotNil(t, analysis)
	return analysis
}

func TestOuterJoinNullabilityFollowsJoinUseNulls(t *testing.T) {
	t.Parallel()

	const leftJoin = "SELECT u.id FROM users u LEFT JOIN accounts a ON a.user_id = u.id"
	padded := func(keyword string) ast.JoinKind {
		return ast.JoinKind{Keyword: keyword, Nullable: 0, Filters: false}
	}
	testCases := []struct {
		name         string
		sql          string
		want         ast.JoinKind
		joinUseNulls bool
	}{
		{name: "LEFT JOIN fills defaults by default", sql: leftJoin, want: padded("LEFT")},
		{name: "LEFT JOIN with join_use_nulls on", sql: leftJoin, joinUseNulls: true, want: ast.JoinLeft},
		{name: "query setting turns NULLs on", sql: leftJoin + " SETTINGS join_use_nulls = 1", want: ast.JoinLeft},
		{name: "query setting in a string", sql: leftJoin + " SETTINGS max_threads = 2, join_use_nulls = '1'", want: ast.JoinLeft},
		{name: "query setting turns NULLs off", sql: leftJoin + " SETTINGS join_use_nulls = 0", joinUseNulls: true, want: padded("LEFT")},
		{name: "query setting as a word", sql: leftJoin + " SETTINGS join_use_nulls = true", want: ast.JoinLeft},
		{
			name: "RIGHT OUTER JOIN", sql: "SELECT u.id FROM users u RIGHT OUTER JOIN accounts a ON a.user_id = u.id", joinUseNulls: true,
			want: ast.JoinRight,
		},
		{name: "FULL JOIN fills defaults by default", sql: "SELECT u.id FROM users u FULL JOIN accounts a ON a.user_id = u.id", want: padded("FULL")},
		{name: "ANY LEFT JOIN", sql: "SELECT u.id FROM users u ANY LEFT JOIN accounts a ON a.user_id = u.id", joinUseNulls: true, want: ast.JoinLeft},
		{name: "GLOBAL LEFT JOIN", sql: "SELECT u.id FROM users u GLOBAL LEFT JOIN accounts a ON a.user_id = u.id", want: padded("LEFT")},
		{name: "LEFT ANY JOIN", sql: "SELECT u.id FROM users u LEFT ANY JOIN accounts a ON a.user_id = u.id", joinUseNulls: true, want: joinLeftAny},
		{name: "LEFT ASOF JOIN", sql: "SELECT u.id FROM users u LEFT ASOF JOIN accounts a ON a.user_id = u.id AND a.ts <= u.ts", want: padded("LEFT ASOF")},
		{name: "RIGHT ALL JOIN", sql: "SELECT u.id FROM users u RIGHT ALL JOIN accounts a ON a.user_id = u.id", joinUseNulls: true, want: joinRightAll},
		{name: "LEFT SEMI JOIN is unaffected", sql: "SELECT u.id FROM users u LEFT SEMI JOIN accounts a ON a.user_id = u.id", want: joinLeftSemi},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseWith(t, New(WithJoinUseNulls(testCase.joinUseNulls)), testCase.sql)
			require.Len(t, analysis.JoinClauses, 1)
			assert.Equal(t, testCase.want, analysis.JoinClauses[0].Kind)
			assert.Equal(t, "accounts", analysis.JoinClauses[0].Table.Name)
		})
	}
}

func TestStatementSettingReadsTheSettingsClause(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		setting     string
		wantFound   bool
		wantEnabled bool
	}{
		{name: "no settings", sql: "SELECT 1"},
		{name: "other settings", sql: "SELECT 1 SETTINGS max_threads = 1"},
		{name: "enabled", sql: "SELECT 1 SETTINGS join_use_nulls = 1", wantFound: true, wantEnabled: true},
		{name: "disabled", sql: "SELECT 1 SETTINGS join_use_nulls = 0", wantFound: true},
		{name: "not after SETTINGS", sql: "SELECT join_use_nulls = 1 FROM t", wantFound: false},
		{name: "quoted name is not the setting", sql: "SELECT 1 SETTINGS `join_use_nulls` = 1", wantFound: false},
		{name: "unreadable value", sql: "SELECT 1 SETTINGS join_use_nulls = 2", wantFound: false},
		{name: "group_by_use_nulls enabled", sql: "SELECT 1 SETTINGS max_threads = 1, group_by_use_nulls = true", setting: settingGroupByUseNulls, wantFound: true, wantEnabled: true},
		{name: "join_use_nulls is not group_by_use_nulls", sql: "SELECT 1 SETTINGS join_use_nulls = 1", setting: settingGroupByUseNulls, wantFound: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			setting := testCase.setting
			if setting == "" {
				setting = settingJoinUseNulls
			}
			enabled, found := statementSetting(tokeniseStatement(t, testCase.sql), setting)
			assert.Equal(t, testCase.wantFound, found)
			assert.Equal(t, testCase.wantEnabled, enabled)
		})
	}
}
