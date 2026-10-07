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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func TestConditionsMustBeOneCompleteExpression(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "an empty WHERE", sql: "SELECT id FROM t WHERE", token: ""},
		{name: "an empty WHERE before GROUP BY", sql: "SELECT id FROM t WHERE GROUP BY id", token: "GROUP"},
		{name: "an empty HAVING", sql: "SELECT id FROM t GROUP BY id HAVING", token: ""},
		{name: "an empty ON", sql: "SELECT a.id FROM t a JOIN t b ON", token: ""},
		{name: "an empty PREWHERE", sql: "SELECT id FROM t PREWHERE WHERE id = 1", token: "WHERE"},
		{name: "an empty QUALIFY", sql: "SELECT id FROM t QUALIFY ORDER BY id", token: "ORDER"},
		{name: "an unclosed parenthesis", sql: "SELECT id FROM t WHERE id = (", token: "("},
		{name: "an unclosed group", sql: "SELECT id FROM t WHERE (id = 1", token: "("},
		{name: "a dangling AND", sql: "SELECT id FROM t WHERE id = 1 AND", token: ""},
		{name: "a dangling OR before ORDER BY", sql: "SELECT id FROM t WHERE id = 1 OR ORDER BY id", token: "ORDER"},
		{name: "a missing right operand", sql: "SELECT id FROM t WHERE id =", token: ""},
		{name: "a word after the WHERE condition", sql: "SELECT id FROM t WHERE id = 1 name", token: "name"},
		{name: "a word after the HAVING condition", sql: "SELECT id FROM t GROUP BY id HAVING count() > 1 name", token: "name"},
		{name: "a word after the ON condition", sql: "SELECT a.id FROM t a JOIN t b ON a.id = b.id name", token: "name"},
		{name: "a word after the PREWHERE condition", sql: "SELECT id FROM t PREWHERE id = 1 name", token: "name"},
		{name: "a word after the QUALIFY condition", sql: "SELECT id FROM t QUALIFY id > 1 name", token: "name"},
		{name: "a word after a DELETE condition", sql: "DELETE FROM t WHERE id = 1 name", token: "name"},
		{name: "a word after an UPDATE condition", sql: "UPDATE t SET name = 'x' WHERE id = 1 name", token: "name"},
		{name: "a word after a mutation condition", sql: "ALTER TABLE t DELETE WHERE id = 1 name", token: "name"},
		{name: "an empty mutation condition", sql: "ALTER TABLE t DELETE WHERE", token: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var parseError *dialectkit.ParseError
			require.ErrorAs(t, analyseError(t, testCase.sql), &parseError)
			require.Equal(t, testCase.token, parseError.Token)
		})
	}
}

func TestCompleteConditionsAreRecordedOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		sql    string
		clause ast.Clause
	}{
		{name: "WHERE", sql: "SELECT id FROM t WHERE id IN (SELECT id FROM u WHERE x = 1) AND arrayExists(v -> v > 1, values)", clause: ast.ClauseWhere},
		{name: "PREWHERE", sql: "SELECT id FROM t PREWHERE id = {id:UInt64} WHERE name != ''", clause: ast.ClauseWhere},
		{name: "HAVING", sql: "SELECT id FROM t GROUP BY id HAVING count() BETWEEN 1 AND 2", clause: ast.ClauseHaving},
		{name: "ON", sql: "SELECT a.id FROM t a JOIN t b ON a.id = b.id AND b.name LIKE 'x%'", clause: ast.ClauseJoinCondition},
		{name: "QUALIFY", sql: "SELECT id FROM t QUALIFY row_number() OVER (PARTITION BY id) = 1", clause: ast.ClauseRowShaping},
		{name: "a mutation", sql: "ALTER TABLE t DELETE WHERE id = {id:UInt64} AND NOT done", clause: ast.ClauseWhere},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			count := 0
			for _, expression := range analysis.ClauseExpressions {
				if expression.Clause == testCase.clause {
					count++
				}
			}
			require.GreaterOrEqual(t, count, 1)
		})
	}
}

func TestStatementsMustOpenWithAStatementWord(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		rejected bool
	}{
		{name: "a misspelt SELECT", sql: "SELEC id FROM t", rejected: true},
		{name: "a parenthesised query", sql: "(SELECT id FROM t) UNION ALL (SELECT id FROM t)", rejected: true},
		{name: "a bare word", sql: "items", rejected: true},
		{name: "SHOW", sql: "SHOW TABLES", rejected: false},
		{name: "DESCRIBE", sql: "DESCRIBE TABLE t", rejected: false},
		{name: "EXISTS", sql: "EXISTS TABLE t", rejected: false},
		{name: "OPTIMIZE", sql: "OPTIMIZE TABLE t FINAL", rejected: false},
		{name: "SYSTEM", sql: "SYSTEM FLUSH LOGS", rejected: false},
		{name: "KILL", sql: "KILL QUERY WHERE query_id = 'x'", rejected: false},
		{name: "TRUNCATE", sql: "TRUNCATE TABLE t", rejected: false},
		{name: "SET", sql: "SET max_threads = 4", rejected: false},
		{name: "WATCH", sql: "WATCH live_view", rejected: false},
		{name: "UNDROP", sql: "UNDROP TABLE t", rejected: false},
		{name: "a CREATE of an object the dialect does not model", sql: "CREATE NAMED COLLECTION c AS a = 1", rejected: false},
		{name: "EXPLAIN", sql: "EXPLAIN SELECT 1", rejected: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseError(t, testCase.sql)
			if !testCase.rejected {
				require.NoError(t, err)
				return
			}
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			require.Zero(t, parseError.Position)
		})
	}
}

func TestFromIsReadAsAColumnOnlyWhenNothingFollows(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT FROM")
	require.Len(t, analysis.OutputColumns, 1)
	require.Equal(t, "FROM", analysis.OutputColumns[0].ColumnName)

	analysis = analyse(t, "SELECT from FROM t")
	require.Len(t, analysis.OutputColumns, 1)
	require.Equal(t, "from", analysis.OutputColumns[0].ColumnName)

	var parseError *dialectkit.ParseError
	require.ErrorAs(t, analyseError(t, "SELECT FROM t"), &parseError)
	require.Equal(t, "t", parseError.Token)
}

func TestOrderByPlaceIsRecordedWhereOrderByGoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		after string
	}{
		{name: "after the select list", sql: "SELECT 1", after: "SELECT 1"},
		{name: "after WHERE", sql: "SELECT id FROM t WHERE id = 1 LIMIT 1", after: "SELECT id FROM t WHERE id = 1"},
		{name: "before ORDER BY", sql: "SELECT id FROM t ORDER BY id LIMIT 1", after: "SELECT id FROM t"},
		{name: "after QUALIFY", sql: "SELECT id FROM t QUALIFY id > 1 SETTINGS max_threads = 1", after: "SELECT id FROM t QUALIFY id > 1"},
		{
			name:  "before LIMIT n BY",
			sql:   "SELECT id FROM t GROUP BY id HAVING count() > 1 LIMIT 1 BY id LIMIT 5",
			after: "SELECT id FROM t GROUP BY id HAVING count() > 1",
		},
		{name: "before FORMAT", sql: "SELECT id FROM t WINDOW w AS (ORDER BY id) FORMAT JSON", after: "SELECT id FROM t WINDOW w AS (ORDER BY id)"},
		{
			name:  "in the last branch of a compound",
			sql:   "SELECT id FROM t ORDER BY id UNION ALL SELECT id FROM u WHERE id > 1 LIMIT 2",
			after: "SELECT id FROM t ORDER BY id UNION ALL SELECT id FROM u WHERE id > 1",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.Equal(t, ast.Span{Start: len(testCase.after), End: len(testCase.after)}, analysis.Spans.OrderByPlace)
		})
	}
}
