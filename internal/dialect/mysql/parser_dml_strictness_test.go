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

package mysql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/fault"
)

func TestAnalyseQueryRejectsStatementsItCannotRead(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "a misspelt verb", sql: "SELEC id FROM items", token: "SELEC"},
		{name: "a word that starts no statement", sql: "FETCH id FROM items", token: "FETCH"},
		{name: "parentheses holding no query", sql: "(1)", token: "("},
		{name: "a TABLE statement", sql: "TABLE items", token: "TABLE"},
		{name: "a parameter in a SET statement", sql: "SET @limit = ?", token: "?"},
		{name: "a parameter in a DO statement", sql: "DO SLEEP(?)", token: "?"},
		{name: "a parameter in an EXPLAIN statement", sql: "EXPLAIN SELECT id FROM items WHERE id = ?", token: "?"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.Nil(t, analysis)
			requireParseErrorAt(t, err, testCase.token)
		})
	}
}

func TestAnalyseQueryAcceptsStatementsWithNothingToAnalyse(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"TRUNCATE TABLE items",
		"FLUSH TABLES",
		"LOCK TABLES items WRITE",
		"UNLOCK TABLES",
		"SET @limit = 10",
		"START TRANSACTION",
		"COMMIT",
		"ANALYZE TABLE items",
		"OPTIMIZE TABLE items",
		"DO SLEEP(1)",
		"CREATE INDEX items_name ON items (name)",
		"ALTER TABLE items ADD COLUMN size int",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), sql)

			require.NoError(t, err)
			require.Empty(t, analysis.OutputColumns)
			require.Empty(t, analysis.ParameterReferences)
		})
	}
}

func TestAnalyseQueryAnalysesTheArgumentsOfACall(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		parameters int
	}{
		{name: "a call with parameters", sql: "CALL refresh_totals(?, 2, ?)", parameters: 2},
		{name: "a call without parentheses", sql: "CALL refresh_totals", parameters: 0},
		{name: "a call of a qualified procedure", sql: "CALL reports.refresh_totals(?)", parameters: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.NoError(t, err)
			require.Len(t, analysis.ParameterReferences, testCase.parameters)
		})
	}
}

func TestAnalyseQueryRejectsMalformedConditions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "an empty WHERE", sql: "SELECT id FROM items WHERE", token: ""},
		{name: "an empty WHERE before a clause", sql: "SELECT id FROM items WHERE ORDER BY id", token: "ORDER"},
		{name: "an empty HAVING", sql: "SELECT id FROM items GROUP BY id HAVING", token: ""},
		{name: "an empty ON", sql: "SELECT a.id FROM items a JOIN items b ON", token: ""},
		{name: "an unclosed parenthesis", sql: "SELECT id FROM items WHERE id = (", token: ""},
		{name: "an unclosed group", sql: "SELECT id FROM items WHERE (id = 1", token: ""},
		{name: "a dangling AND", sql: "SELECT id FROM items WHERE id = 1 AND", token: ""},
		{name: "a dangling OR", sql: "SELECT id FROM items WHERE id = 1 OR", token: ""},
		{name: "a comparison without its right operand", sql: "SELECT id FROM items WHERE id =", token: ""},
		{name: "a word after the condition", sql: "SELECT id FROM items WHERE id = 1 name", token: "name"},
		{name: "a word after a HAVING condition", sql: "SELECT id FROM items GROUP BY id HAVING count(*) > 1 name", token: "name"},
		{name: "a word after a join condition", sql: "SELECT a.id FROM items a JOIN items b ON a.id = b.id name", token: "name"},
		{name: "a PostgreSQL cast", sql: "SELECT id FROM items WHERE id::char = 'a'", token: ":"},
		{name: "a word after an UPDATE condition", sql: "UPDATE items SET name = ? WHERE id = ? name", token: "name"},
		{name: "an empty DELETE condition", sql: "DELETE FROM items WHERE", token: ""},
		{name: "a field of a parenthesised value", sql: "SELECT id FROM items WHERE (name).first = 'a'", token: "."},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.Nil(t, analysis)
			requireParseErrorAt(t, err, testCase.token)
		})
	}
}

func TestAnalyseQueryAcceptsWellFormedConditions(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT id FROM items WHERE id = @limit",
		"SELECT id FROM items WHERE id = @@session.sql_select_limit",
		"SELECT id FROM items WHERE id IN (SELECT id FROM items WHERE id > ?)",
		"SELECT id FROM items WHERE name LIKE ? ESCAPE '!'",
		"SELECT id FROM items WHERE MATCH(name) AGAINST (? IN BOOLEAN MODE)",
		"SELECT id FROM items WHERE id BETWEEN ? AND ?",
		"SELECT id FROM items WHERE NOT EXISTS (SELECT 1 FROM items)",
		"SELECT id FROM items WHERE created_at > NOW() - INTERVAL 1 DAY",
		"SELECT id FROM items WHERE id = ANY (SELECT id FROM items)",
		"SELECT id FROM items WHERE (id, name) = (?, ?)",
		"SELECT id FROM items WHERE ROW(id, name) IN (ROW(1, 'a'), ROW(2, 'b'))",
		"SELECT id FROM items WHERE ? MEMBER OF (tags)",
		"SELECT id FROM items WHERE name IS NOT NULL AND id IS TRUE",
		"SELECT id FROM items WHERE name COLLATE utf8mb4_bin = ?",
		"SELECT id FROM items WHERE CAST(id AS CHAR) = ? OR CONVERT(name USING utf8mb4) = ?",
		"SELECT id FROM items WHERE BINARY name = ? XOR id = 1",
		"SELECT id FROM items WHERE doc->>'$.a' = ? AND doc->'$.b' IS NULL",
		"SELECT id FROM items WHERE CASE WHEN id > 1 THEN 1 ELSE 0 END = 1",
		"SELECT id FROM items WHERE id DIV 2 = 1 AND id MOD 2 = 0 AND id & 1 = 1",
		"SELECT id FROM items WHERE name SOUNDS LIKE ? AND name REGEXP '^a' AND name NOT RLIKE 'b'",
		"SELECT id FROM items WHERE id = 1 || id = 2 && id <> 3",
		"SELECT id FROM items WHERE DATE '2026-01-01' < created_at AND _utf8mb4'x' = name",
		"SELECT a.id FROM items a LEFT JOIN items b ON a.id = b.id AND b.name = LEFT(a.name, 1)",
		"SELECT id FROM items GROUP BY id HAVING count(DISTINCT name) > ? AND sum(id) < ?",
		"SELECT id FROM items WHERE TRIM(BOTH 'x' FROM name) = ? AND EXTRACT(YEAR FROM created_at) = ?",
		"SELECT id FROM items WHERE GROUP_CONCAT(name ORDER BY name SEPARATOR ',') = ?",
		"SELECT id FROM items WHERE JSON_VALUE(doc, '$.a' RETURNING SIGNED) = ?",
		"SELECT id FROM items WHERE IF(id > 1, 1, 0) = 1 AND IFNULL(name, '') = ?",
		"SELECT id FROM items WHERE b'101' = flags AND x'FF' = data AND TRUE",
		"SELECT id FROM items WHERE !(id = 1) AND -id < 0 AND ~id <> 0",
		"SELECT id FROM items WHERE POSITION(? IN name) > 0 AND SUBSTRING(name FROM 1 FOR 2) = ?",
		"SELECT id FROM items WHERE SUBSTR(name FROM ?) = 'a' AND MID(name, 1, 2) = ?",
		"SELECT id FROM items WHERE CHAR(65, 66 USING utf8mb4) = name AND WEIGHT_STRING(name AS CHAR(3)) = ?",
		"SELECT id FROM items WHERE id <=> ? AND id NOT BETWEEN 1 AND ? AND name NOT LIKE ?",
		"SELECT id FROM items WHERE DATE_ADD(created_at, INTERVAL 1 DAY) > NOW() AND TIMESTAMPADD(DAY, 1, created_at) > ?",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			_, err := analyseWith(t, New(), sql)

			require.NoError(t, err)
		})
	}
}

func TestAnalyseQueryNeverReadsAKeywordAsASelectedColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "an empty select list", sql: "SELECT FROM items", token: "FROM"},
		{name: "a select list of FROM alone", sql: "SELECT FROM", token: "FROM"},
		{name: "a trailing comma", sql: "SELECT id, FROM items", token: "FROM"},
		{name: "a WHERE in the select list", sql: "SELECT WHERE id = 1", token: "WHERE"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.Nil(t, analysis)
			requireParseErrorAt(t, err, testCase.token)
		})
	}
}

func TestAnalyseQueryReadsQuotedKeywordsAsColumns(t *testing.T) {
	t.Parallel()

	analysis, err := analyseWith(t, New(), "SELECT `from`, `where` FROM items")

	require.NoError(t, err)
	require.Len(t, analysis.OutputColumns, 2)
	require.Equal(t, "from", analysis.OutputColumns[0].ColumnName)
}

func TestAnalyseQueryRejectsSyntaxMySQLLacks(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "a subscript of a subquery", sql: "SELECT (SELECT 1)[1]", token: "["},
		{name: "a subscript of a column", sql: "SELECT name[1] FROM items", token: "["},
		{name: "a subscript in WHERE", sql: "SELECT id FROM items WHERE name[1] = 'a'", token: "["},
		{name: "a subscript in ORDER BY", sql: "SELECT id FROM items ORDER BY name[1]", token: "["},
		{name: "an ARRAY constructor", sql: "SELECT ARRAY[1, 2]", token: "["},
		{name: "a subscript in a SET value", sql: "UPDATE items SET name = tags[1]", token: "["},
		{name: "a PostgreSQL cast in the select list", sql: "SELECT id::char FROM items", token: ":"},
		{name: "a field of a parenthesised value", sql: "SELECT (name).first FROM items", token: "."},
		{name: "a JSON path that is not a string", sql: "SELECT doc -> x FROM items", token: "x"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.Nil(t, analysis)
			requireParseErrorAt(t, err, testCase.token)
		})
	}
}

func TestParseConditionStrictlyLeavesPanicsAndInterruptsToTheEntryPoint(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		interrupt func() error
		wantError error
		name      string
		wantLogs  int
	}{
		{name: "an interrupt", interrupt: func() error { return errWalkStopped }, wantError: errWalkStopped, wantLogs: 0},
		{name: "a panic", interrupt: func() error { panic("parser bug") }, wantError: fault.ErrPanic, wantLogs: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise("a = 1 AND b = 2")
			require.NoError(t, err)
			p := newParser(tokens)
			p.Interrupt = testCase.interrupt
			ctx, logs := newLoggingContext(t)

			err = walkAtAnEntryPoint(ctx, func() { p.parseConditionStrictly(0, len(tokens)) })

			require.ErrorIs(t, err, testCase.wantError)
			require.Len(t, decodeLogRecords(t, logs), testCase.wantLogs)
		})
	}
}
