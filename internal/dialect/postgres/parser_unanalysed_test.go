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

package postgres

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func TestStatementsWithoutAnalysis(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		message string
	}{
		{name: "a misspelt statement", sql: "SELEC id FROM items", message: "not a statement Seagull can analyse"},
		{name: "an unsupported MERGE", sql: "MERGE INTO items USING other s ON s.id = items.id WHEN MATCHED THEN DO NOTHING",
			message: "not a statement Seagull can analyse"},
		{name: "parentheses holding no query", sql: "(1)", message: "expected a query inside the parentheses"},
		{name: "a WITH list before an unanalysed statement", sql: "WITH x AS (SELECT 1) MERGE INTO items USING x ON true WHEN MATCHED THEN DO NOTHING",
			message: "expected keyword SELECT"},
		{name: "a session statement", sql: "SET search_path = public"},
		{name: "a maintenance statement", sql: "VACUUM items"},
		{name: "a truncation", sql: "TRUNCATE items"},
		{name: "a notification", sql: "NOTIFY changes, 'x'"},
		{name: "a transaction statement", sql: "BEGIN"},
		{name: "DDL in a query file", sql: "CREATE TABLE IF NOT EXISTS t (id int)"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			if testCase.message != "" {
				require.ErrorContains(t, err, testCase.message)
				return
			}
			require.NoError(t, err)
			require.Empty(t, analysis.OutputColumns)
		})
	}
}

func TestEmptySelectList(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{"SELECT FROM items", "SELECT", "SELECT WHERE true", "SELECT FROM items WHERE id = $1"} {
		engine := New()
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
		require.NoError(t, err, sql)
		require.Empty(t, analysis.OutputColumns, sql)
	}

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT FROM;")
	require.NoError(t, err)
	_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.ErrorContains(t, err, "expected a table name")
}

func TestParenthesisedAndTableQueries(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		columns    int
		parameters int
		branches   int
	}{
		{name: "a parenthesised compound", sql: "(SELECT id FROM items WHERE id = $1) UNION (SELECT id FROM other WHERE id = $2) ORDER BY id LIMIT $3",
			columns: 1, parameters: 3, branches: 1},
		{name: "one parenthesised query", sql: "(SELECT id, name FROM items ORDER BY id LIMIT 1)", columns: 2, parameters: 0, branches: 0},
		{name: "a parenthesised second branch", sql: "SELECT id FROM items UNION ALL (SELECT id FROM other ORDER BY id LIMIT $1)",
			columns: 1, parameters: 1, branches: 1},
		{name: "nested parentheses", sql: "((SELECT id FROM items WHERE name = :name))", columns: 1, parameters: 1, branches: 0},
		{name: "a TABLE command", sql: "TABLE items", columns: 1, parameters: 0, branches: 0},
		{name: "a TABLE command with ordering", sql: "TABLE ONLY public.items ORDER BY id LIMIT $1", columns: 1, parameters: 1, branches: 0},
		{name: "a compound of TABLE commands", sql: "TABLE items UNION ALL TABLE other", columns: 1, parameters: 0, branches: 1},
		{name: "a TABLE command after a WITH list", sql: "WITH x AS (SELECT id FROM items) TABLE x", columns: 1, parameters: 0, branches: 0},
		{name: "a derived table inside the parentheses", sql: "(SELECT d.x FROM (SELECT id AS x FROM items) d)", columns: 1, parameters: 0, branches: 0},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			require.Len(t, analysis.OutputColumns, testCase.columns)
			require.Len(t, analysis.ParameterReferences, testCase.parameters)
			require.Len(t, analysis.CompoundBranches, testCase.branches)
			require.True(t, analysis.ReadOnly)
		})
	}
}

func TestAParameterInAStatementRunWithoutAnalysisIsRefusedAtTheParameter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		engine   *Dialect
		sql      string
		token    string
		contains string
		excludes string
	}{
		{name: "a local setting", engine: New(), sql: "SET LOCAL app.user_id = $1", token: "$1",
			contains: "no parameter can be bound in SET statements, which run without analysis; the server takes no parameter " +
				"in SET either, so set it with SELECT set_config('setting', $1, true), true for SET LOCAL and false for the session"},
		{name: "a session setting through a named parameter", engine: New(), sql: "SET SESSION app.user_id = :user_id", token: ":user_id",
			contains: "set_config('setting', $1, true)"},
		{name: "a notification", engine: New(), sql: "NOTIFY changes, $1", token: "$1",
			contains: "no parameter can be bound in NOTIFY statements, which run without analysis; the server takes no parameter " +
				"in NOTIFY either, so send the payload with SELECT pg_notify('channel', $1)"},
		{name: "a comment", engine: New(), sql: "COMMENT ON TABLE items IS $1", token: "$1",
			contains: "no parameter can be bound in COMMENT statements, which run without analysis", excludes: ";"},
		{name: "an explained query", engine: New(), sql: "EXPLAIN SELECT id FROM items WHERE id = $1", token: "$1",
			contains: "no parameter can be bound in EXPLAIN statements"},
		{name: "a notification where the server has no pg_notify", engine: New(WithoutBuiltinFunctions("pg_notify")),
			sql: "NOTIFY changes, $1", token: "$1", contains: "no parameter can be bound in NOTIFY statements", excludes: "pg_notify"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := testCase.engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			_, err = testCase.engine.AnalyseQuery(t.Context(), nil, statements[0])

			parseError, ok := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, ok, "want a parse error, got %v", err)
			require.Equal(t, testCase.token, parseError.Token)
			require.Contains(t, parseError.Message, testCase.contains)
			if testCase.excludes != "" {
				require.NotContains(t, parseError.Message, testCase.excludes)
			}
		})
	}
}
