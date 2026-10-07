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

package sqlexec

import (
	"testing"

	"github.com/stretchr/testify/require"

	mysql_lexical "seagull.sh/seagull/internal/dialect/mysql/lexical"
	"seagull.sh/seagull/internal/sqllex"
)

var postgresSplitterCorpus = []splitterCase{
	{name: "a terminator inside a quoted identifier", input: `CREATE TABLE "semi;colon" (id int); CREATE TABLE b (id int);`, expected: []string{`CREATE TABLE "semi;colon" (id int)`, "CREATE TABLE b (id int)"}},
	{name: "an apostrophe inside a quoted identifier", input: `CREATE TABLE "o'brien" (id int); SELECT 1;`, expected: []string{`CREATE TABLE "o'brien" (id int)`, "SELECT 1"}},
	{name: "an escaped quote inside an E string", input: `SELECT E'a\';b'; SELECT 2;`, expected: []string{`SELECT E'a\';b'`, "SELECT 2"}},
	{name: "a column named begin", input: "CREATE TABLE bookings (id int, begin timestamptz); CREATE TABLE u (id int);", expected: []string{"CREATE TABLE bookings (id int, begin timestamptz)", "CREATE TABLE u (id int)"}},
	{name: "renaming a column named begin", input: "ALTER TABLE shifts RENAME COLUMN begin TO starts_at; SELECT 1;", expected: []string{"ALTER TABLE shifts RENAME COLUMN begin TO starts_at", "SELECT 1"}},
	{name: "a nested block comment", input: "/* outer /* inner */ still; */ SELECT 1; SELECT 2;", expected: []string{"SELECT 1", "SELECT 2"}},
	{name: "a dollar-quoted function body", input: "CREATE FUNCTION f() RETURNS int AS $body$ SELECT 1; $body$ LANGUAGE sql; SELECT f();", expected: []string{"CREATE FUNCTION f() RETURNS int AS $body$ SELECT 1; $body$ LANGUAGE sql", "SELECT f()"}},
	{name: "a comment-only tail", input: "SELECT 1;\n-- trailing note\n", expected: []string{"SELECT 1"}},
	{name: "a leading byte order mark", input: sqllex.ByteOrderMark + "CREATE TABLE t (id int); SELECT 1;", expected: []string{"CREATE TABLE t (id int)", "SELECT 1"}},
	{name: "pg_dump's restrict lines", input: "\\restrict abc123\nSET statement_timeout = 0;\nCREATE TABLE public.items (id bigint);\n\\unrestrict abc123\n", expected: []string{"SET statement_timeout = 0", "CREATE TABLE public.items (id bigint)"}},
	{name: "a meta-command after a statement on its line", input: "SELECT 1; \\connect app\nSELECT 2;", expected: []string{"SELECT 1", "SELECT 2"}},
	{name: "a backslash inside a dollar-quoted body", input: "CREATE FUNCTION f() RETURNS text AS $$ SELECT '\\n' $$ LANGUAGE sql;\n\\echo done\n", expected: []string{"CREATE FUNCTION f() RETURNS text AS $$ SELECT '\\n' $$ LANGUAGE sql"}},
}

var mysqlSplitterCorpus = []splitterCase{
	{name: "a terminator inside a double-quoted string", input: `INSERT INTO settings VALUES ("theme", "a;b"); SELECT 1;`, expected: []string{`INSERT INTO settings VALUES ("theme", "a;b")`, "SELECT 1"}},
	{name: "a terminator inside a backtick identifier", input: "CREATE TABLE `a;b` (id INT); SELECT 1;", expected: []string{"CREATE TABLE `a;b` (id INT)", "SELECT 1"}},
	{name: "an apostrophe inside a hash comment", input: "INSERT INTO t VALUES (1); # don't run this twice\nINSERT INTO t VALUES (2);", expected: []string{"INSERT INTO t VALUES (1)", "INSERT INTO t VALUES (2)"}},
	{name: "a backslash-escaped quote", input: `INSERT INTO t VALUES ('it\'s; fine'); SELECT 1;`, expected: []string{`INSERT INTO t VALUES ('it\'s; fine')`, "SELECT 1"}},
	{name: "a DELIMITER block", input: "DELIMITER //\nCREATE FUNCTION f() RETURNS INT BEGIN RETURN 1; END //\nDELIMITER ;\nSELECT f();", expected: []string{"CREATE FUNCTION f() RETURNS INT BEGIN RETURN 1; END", "SELECT f()"}},
	{name: "a procedure body without DELIMITER", input: "CREATE PROCEDURE p() BEGIN SELECT 1; SELECT 2; END; SELECT 3;", expected: []string{"CREATE PROCEDURE p() BEGIN SELECT 1; SELECT 2; END", "SELECT 3"}},
	{name: "a mysqldump executable comment statement", input: "/*!40101 SET NAMES utf8mb4 */;\nCREATE TABLE t (id INT);", expected: []string{"/*!40101 SET NAMES utf8mb4 */", "CREATE TABLE t (id INT)"}},
	{name: "a trailing executable comment table option", input: "CREATE TABLE t (id INT) /*!50100 ENGINE=InnoDB */;", expected: []string{"CREATE TABLE t (id INT) /*!50100 ENGINE=InnoDB */"}},
	{name: "a mariadb executable comment", input: "/*M!100100 SET sql_mode = 'ANSI' */; SELECT 1;", expected: []string{"/*M!100100 SET sql_mode = 'ANSI' */", "SELECT 1"}},
}

var sqliteSplitterCorpus = []splitterCase{
	{name: "a terminator inside a quoted identifier", input: `CREATE TABLE "a;b" (id INTEGER); SELECT 1;`, expected: []string{`CREATE TABLE "a;b" (id INTEGER)`, "SELECT 1"}},
	{name: "a terminator inside a bracketed identifier", input: "CREATE TABLE [a;b] (id INTEGER); SELECT 1;", expected: []string{"CREATE TABLE [a;b] (id INTEGER)", "SELECT 1"}},
	{name: "a trigger body", input: "CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET c = CASE WHEN 1 THEN 2 END; END; SELECT 1;", expected: []string{"CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET c = CASE WHEN 1 THEN 2 END; END", "SELECT 1"}},
	{name: "a column named begin", input: "CREATE TABLE shifts (begin TEXT); SELECT 1;", expected: []string{"CREATE TABLE shifts (begin TEXT)", "SELECT 1"}},
}

var duckDBSplitterCorpus = []splitterCase{
	{name: "a terminator inside a quoted identifier", input: `CREATE TABLE "a;b" (id INTEGER); SELECT 1;`, expected: []string{`CREATE TABLE "a;b" (id INTEGER)`, "SELECT 1"}},
	{name: "a dollar-quoted string", input: "SELECT $$a;b$$; SELECT 2;", expected: []string{"SELECT $$a;b$$", "SELECT 2"}},
	{name: "a nested block comment", input: "/* a /* b */ c; */ SELECT 1;", expected: []string{"SELECT 1"}},
}

var clickHouseSplitterCorpus = []splitterCase{
	{name: "a terminator inside a backtick identifier", input: "CREATE TABLE `a;b` (id Int32) ENGINE = Memory; SELECT 1;", expected: []string{"CREATE TABLE `a;b` (id Int32) ENGINE = Memory", "SELECT 1"}},
	{name: "a hash comment", input: "SELECT 1; # it's a note\nSELECT 2;", expected: []string{"SELECT 1", "SELECT 2"}},
	{name: "a backslash-escaped quote", input: `SELECT 'a\';b'; SELECT 2;`, expected: []string{`SELECT 'a\';b'`, "SELECT 2"}},
}

type splitterCase struct {
	name     string
	input    string
	expected []string
}

func TestBackendsSplitLikeTheirDialects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		backend Backend
		corpus  []splitterCase
	}{
		{name: "postgres", backend: Postgres(), corpus: postgresSplitterCorpus},
		{name: "postgres_pgbouncer", backend: PostgresPgBouncer(), corpus: postgresSplitterCorpus},
		{name: "cockroachdb", backend: CockroachDB(), corpus: postgresSplitterCorpus},
		{name: "timescaledb", backend: TimescaleDB(), corpus: postgresSplitterCorpus},
		{name: "mysql", backend: MySQL(), corpus: mysqlSplitterCorpus},
		{name: "sqlite", backend: SQLite(), corpus: sqliteSplitterCorpus},
		{name: "duckdb", backend: DuckDB(), corpus: duckDBSplitterCorpus},
		{name: "clickhouse", backend: ClickHouse(), corpus: clickHouseSplitterCorpus},
	}
	for _, testCase := range testCases {
		for _, entry := range testCase.corpus {
			t.Run(testCase.name+"/"+entry.name, func(t *testing.T) {
				t.Parallel()

				statements, err := testCase.backend.Split(entry.input)
				require.NoError(t, err)
				require.Equal(t, entry.expected, statements)
			})
		}
	}
}

func TestTheMySQLBackendReadsDoubleQuotesAsTheGeneratorDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		input      string
		message    string
		ansiQuotes bool
	}{
		{name: "a string by default", input: `SELECT "open; SELECT 2;`, message: "unterminated string literal"},
		{name: "a quoted identifier under ANSI_QUOTES", input: `SELECT "open; SELECT 2;`, ansiQuotes: true, message: "unterminated quoted identifier"},
		{name: "a backslash ending an identifier under ANSI_QUOTES", input: `CREATE TABLE "a\" (id INT); SELECT 1;`, ansiQuotes: true},
		{name: "a terminator inside an identifier under ANSI_QUOTES", input: `CREATE TABLE "a;b" (id INT); SELECT 1;`, ansiQuotes: true},
		{name: "explicitly off", input: `INSERT INTO t VALUES ("a;b"); SELECT 1;`, ansiQuotes: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rules := mysql_lexical.Rules()
			if testCase.ansiQuotes {
				rules = mysql_lexical.ANSIQuotesRules()
			}
			want, wantError := sqllex.Split(rules, testCase.input)
			statements, err := MySQL(WithANSIQuotes(testCase.ansiQuotes)).Split(testCase.input)
			if testCase.message != "" {
				require.ErrorContains(t, err, testCase.message)
				require.ErrorContains(t, wantError, testCase.message)
				return
			}
			require.NoError(t, err)
			require.NoError(t, wantError)
			texts := make([]string, len(want))
			for index, statement := range want {
				texts[index] = statement.Text
			}
			require.Equal(t, texts, statements)
		})
	}
}

func TestOnlyTheMySQLBackendReadsTheANSIQuotesOption(t *testing.T) {
	t.Parallel()

	for _, backend := range []func(...BackendOption) Backend{Postgres, SQLite, DuckDB, ClickHouse} {
		plain, plainError := backend().Split(`SELECT 1; SELECT 2;`)
		configured, configuredError := backend(WithANSIQuotes(true)).Split(`SELECT 1; SELECT 2;`)
		require.NoError(t, plainError)
		require.NoError(t, configuredError)
		require.Equal(t, plain, configured)
	}
	require.Equal(t, MySQL().engine.rules.Quotes.DoubleQuote, MySQL(WithANSIQuotes(false)).engine.rules.Quotes.DoubleQuote)
	require.Equal(t, sqllex.DoubleQuoteIdentifier, MySQL(WithANSIQuotes(true)).engine.rules.Quotes.DoubleQuote)
}
