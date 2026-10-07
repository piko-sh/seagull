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

package lexdiff_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	clickhouse_lexical "seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/duckdb"
	duckdb_lexical "seagull.sh/seagull/internal/dialect/duckdb/lexical"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/mysql"
	mysql_lexical "seagull.sh/seagull/internal/dialect/mysql/lexical"
	"seagull.sh/seagull/internal/dialect/postgres"
	postgres_lexical "seagull.sh/seagull/internal/dialect/postgres/lexical"
	"seagull.sh/seagull/internal/dialect/sqlite"
	sqlite_lexical "seagull.sh/seagull/internal/dialect/sqlite/lexical"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	timescaledb_lexical "seagull.sh/seagull/internal/dialect/timescaledb/lexical"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/migrate/sqlexec"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/testkit/lexdiff"
)

type statementParser interface {
	ParseStatements(ctx context.Context, sql string) ([]ast.ParsedStatement, error)
}

type engine struct {
	parser  statementParser
	backend sqlexec.Backend
	name    string
	corpora []string
	rules   sqllex.Rules
}

type differences map[string]string

func engines() []engine {
	return []engine{
		{name: "postgres", corpora: []string{"postgres"}, parser: postgres.New(), rules: postgres_lexical.Rules(), backend: sqlexec.Postgres()},
		{name: "cockroachdb", corpora: []string{"cockroachdb"}, parser: cockroachdb.New(), rules: postgres_lexical.Rules(), backend: sqlexec.CockroachDB()},
		{name: "timescaledb", corpora: []string{"timescaledb"}, parser: timescaledb.New(), rules: timescaledb_lexical.Rules(), backend: sqlexec.TimescaleDB()},
		{name: "mysql", corpora: []string{"mysql"}, parser: mysql.New(), rules: mysql_lexical.Rules(), backend: sqlexec.MySQL()},
		{name: "mariadb", corpora: []string{"mariadb"}, parser: mariadb.New(), rules: mysql_lexical.Rules(), backend: sqlexec.MySQL()},
		{name: "sqlite", corpora: []string{"sqlite", "sqlite_cgo"}, parser: sqlite.New(), rules: sqlite_lexical.Rules(), backend: sqlexec.SQLite()},
		{name: "duckdb", corpora: []string{"duckdb"}, parser: duckdb.New(), rules: duckdb_lexical.Rules(), backend: sqlexec.DuckDB()},
		{name: "clickhouse", corpora: []string{"clickhouse"}, parser: clickhouse.New(), rules: clickhouse_lexical.Rules(), backend: sqlexec.ClickHouse()},
	}
}

func lexedStarts(statements []sqllex.Statement) []int {
	starts := make([]int, 0, len(statements))
	for _, statement := range statements {
		starts = append(starts, statement.Start)
	}
	return starts
}

func lexedTexts(statements []sqllex.Statement) []string {
	texts := make([]string, 0, len(statements))
	for _, statement := range statements {
		texts = append(texts, statement.Text)
	}
	return texts
}

func parsedStarts(statements []ast.ParsedStatement) []int {
	starts := make([]int, 0, len(statements))
	for _, statement := range statements {
		starts = append(starts, statement.Location)
	}
	return starts
}

func parsedLengths(statements []ast.ParsedStatement) []int {
	lengths := make([]int, 0, len(statements))
	for _, statement := range statements {
		lengths = append(lengths, statement.Length)
	}
	return lengths
}

func lexedLengths(statements []sqllex.Statement) []int {
	lengths := make([]int, 0, len(statements))
	for _, statement := range statements {
		lengths = append(lengths, statement.End-statement.Start)
	}
	return lengths
}

func compareWithParser(ctx context.Context, subject engine, content string, lexed []sqllex.Statement, lexErr error) (string, bool) {
	parsed, parseErr := subject.parser.ParseStatements(ctx, content)
	switch {
	case lexErr != nil && parseErr != nil:
		if !errors.Is(parseErr, sqllex.ErrMalformed) {
			return fmt.Sprintf("the parser rejects the text sqllex rejects, but not with the lexical error: %v", parseErr), true
		}
		return "", false
	case lexErr != nil:
		return fmt.Sprintf("the parser splits where sqllex fails: %v", lexErr), true
	case parseErr != nil:
		return fmt.Sprintf("the parser rejects text sqllex splits: %v", parseErr), true
	}
	if !slices.Equal(lexedStarts(lexed), parsedStarts(parsed)) {
		return fmt.Sprintf("starts differ: sqllex %v, parser %v", lexedStarts(lexed), parsedStarts(parsed)), true
	}
	if !slices.Equal(lexedLengths(lexed), parsedLengths(parsed)) {
		return fmt.Sprintf("lengths differ: sqllex %v, parser %v", lexedLengths(lexed), parsedLengths(parsed)), true
	}
	return "", false
}

func compareWithRuntime(subject engine, content string, lexed []sqllex.Statement, lexErr error) (string, bool) {
	runtime, runtimeErr := subject.backend.Split(content)
	switch {
	case runtimeErr != nil && lexErr != nil:
		if !errors.Is(runtimeErr, sqlexec.ErrMalformedStatement) || !errors.Is(runtimeErr, sqllex.ErrMalformed) {
			return fmt.Sprintf("the runtime splitter rejects the text with an unexpected error: %v", runtimeErr), true
		}
		return "", false
	case runtimeErr != nil:
		return fmt.Sprintf("the runtime splitter fails: %v", runtimeErr), true
	case lexErr != nil:
		return fmt.Sprintf("sqllex fails where the runtime splitter splits: %v", lexErr), true
	}
	if !slices.Equal(lexedTexts(lexed), runtime) {
		return fmt.Sprintf("statements differ: sqllex %d %q, runtime %d %q", len(lexed), lexedTexts(lexed), len(runtime), runtime), true
	}
	return "", false
}

func fileKey(subject engine, file lexdiff.File) string {
	return subject.name + " " + file.Corpus + " " + file.Name
}

func collectDifferences(t *testing.T) (againstParser, againstRuntime differences, counts map[string]int) {
	t.Helper()

	againstParser = differences{}
	againstRuntime = differences{}
	counts = map[string]int{}
	for _, subject := range engines() {
		files, err := lexdiff.Corpus(subject.corpora...)
		require.NoError(t, err)
		require.NotEmpty(t, files, subject.name)
		dialectFiles := 0
		for _, file := range files {
			counts[subject.name]++
			if file.Corpus == lexdiff.CorpusDialect {
				dialectFiles++
			}
			lexed, lexErr := sqllex.Split(subject.rules, file.Content)
			if lexErr != nil {
				require.ErrorIs(t, lexErr, sqllex.ErrMalformed, "%s: not a lexical error", file.Path)
			}
			if detail, differs := compareWithParser(t.Context(), subject, file.Content, lexed, lexErr); differs {
				againstParser[fileKey(subject, file)] = detail
			}
			if detail, differs := compareWithRuntime(subject, file.Content, lexed, lexErr); differs {
				againstRuntime[fileKey(subject, file)] = detail
			}
		}
		require.Positive(t, dialectFiles, "%s has no dialect golden cases in the corpus", subject.name)
	}
	return againstParser, againstRuntime, counts
}

func engineNamed(t *testing.T, name string) engine {
	t.Helper()

	for _, subject := range engines() {
		if subject.name == name {
			return subject
		}
	}
	require.FailNowf(t, "no such engine", "no engine named %s", name)
	return engine{}
}

func TestBoundariesOverTheCorpora(t *testing.T) {
	t.Parallel()

	againstParser, againstRuntime, counts := collectDifferences(t)
	for _, subject := range engines() {
		require.Positive(t, counts[subject.name], subject.name)
	}

	for _, key := range slices.Sorted(maps.Keys(againstParser)) {
		assert.Failf(t, "sqllex and the dialect parser disagree", "%s: %s", key, againstParser[key])
	}

	for _, key := range slices.Sorted(maps.Keys(againstRuntime)) {
		assert.Failf(t, "sqllex and the migration runtime disagree", "%s: %s", key, againstRuntime[key])
	}
}

func TestRuntimeSplitterBugsAreFixed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		engine   string
		input    string
		expected []string
	}{
		{
			name:     "a terminator inside a double-quoted identifier",
			engine:   "postgres",
			input:    `CREATE TABLE "semi;colon" (id int); CREATE TABLE b (id int);`,
			expected: []string{`CREATE TABLE "semi;colon" (id int)`, "CREATE TABLE b (id int)"},
		},
		{
			name:     "an apostrophe inside a double-quoted identifier",
			engine:   "postgres",
			input:    `CREATE TABLE "o'brien" (id int); SELECT 1;`,
			expected: []string{`CREATE TABLE "o'brien" (id int)`, "SELECT 1"},
		},
		{
			name:     "an escaped quote inside an E string",
			engine:   "postgres",
			input:    `SELECT E'a\';b'; SELECT 2;`,
			expected: []string{`SELECT E'a\';b'`, "SELECT 2"},
		},
		{
			name:     "a column named begin",
			engine:   "postgres",
			input:    "CREATE TABLE bookings (id int, begin timestamptz); CREATE TABLE u (id int);",
			expected: []string{"CREATE TABLE bookings (id int, begin timestamptz)", "CREATE TABLE u (id int)"},
		},
		{
			name:     "a quoted column named begin",
			engine:   "postgres",
			input:    `CREATE TABLE bookings (id int, "begin" timestamptz); SELECT 1;`,
			expected: []string{`CREATE TABLE bookings (id int, "begin" timestamptz)`, "SELECT 1"},
		},
		{
			name:     "renaming a column named begin",
			engine:   "postgres",
			input:    "ALTER TABLE shifts RENAME COLUMN begin TO starts_at; SELECT 1;",
			expected: []string{"ALTER TABLE shifts RENAME COLUMN begin TO starts_at", "SELECT 1"},
		},
		{
			name:     "a nested block comment",
			engine:   "postgres",
			input:    "/* outer /* inner */ still; */ SELECT 1; SELECT 2;",
			expected: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:     "a terminator inside a double-quoted string",
			engine:   "mysql",
			input:    `INSERT INTO settings VALUES ("theme", "a;b"); SELECT 1;`,
			expected: []string{`INSERT INTO settings VALUES ("theme", "a;b")`, "SELECT 1"},
		},
		{
			name:     "a terminator inside a backtick identifier",
			engine:   "mariadb",
			input:    "CREATE TABLE `a;b` (id INT); SELECT 1;",
			expected: []string{"CREATE TABLE `a;b` (id INT)", "SELECT 1"},
		},
		{
			name:     "an apostrophe inside a hash comment",
			engine:   "mysql",
			input:    "INSERT INTO t VALUES (1); # don't run this twice\nINSERT INTO t VALUES (2);",
			expected: []string{"INSERT INTO t VALUES (1)", "INSERT INTO t VALUES (2)"},
		},
		{
			name:     "a DELIMITER block",
			engine:   "mysql",
			input:    "DELIMITER //\nCREATE FUNCTION f() RETURNS INT BEGIN RETURN 1; END //\nDELIMITER ;\nSELECT f();",
			expected: []string{"CREATE FUNCTION f() RETURNS INT BEGIN RETURN 1; END", "SELECT f()"},
		},
		{
			name:     "a terminator inside a double-quoted identifier on SQLite",
			engine:   "sqlite",
			input:    `CREATE TABLE "a;b" (id INTEGER); SELECT 1;`,
			expected: []string{`CREATE TABLE "a;b" (id INTEGER)`, "SELECT 1"},
		},
		{
			name:     "a terminator inside a backtick identifier on ClickHouse",
			engine:   "clickhouse",
			input:    "CREATE TABLE `a;b` (id Int32) ENGINE = Memory; SELECT 1;",
			expected: []string{"CREATE TABLE `a;b` (id Int32) ENGINE = Memory", "SELECT 1"},
		},
		{
			name:     "a comment-only tail",
			engine:   "sqlite",
			input:    "SELECT 1;\n-- trailing note",
			expected: []string{"SELECT 1"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			subject := engineNamed(t, testCase.engine)
			lexed, err := sqllex.Split(subject.rules, testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, lexedTexts(lexed))

			parsed, err := subject.parser.ParseStatements(t.Context(), testCase.input)
			require.NoError(t, err)
			require.Equal(t, parsedStarts(parsed), lexedStarts(lexed), "sqllex must agree with the dialect parser")

			runtime, err := subject.backend.Split(testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, runtime, "the migration runtime must split as the dialect does")
		})
	}
}

func TestFormerParserDifferencesAgree(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		engine   string
		input    string
		expected []string
	}{
		{
			name:     "a parenthesised list of statements stays one statement, as in psql",
			engine:   "postgres",
			input:    "CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2)); SELECT 1;",
			expected: []string{"CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2))", "SELECT 1"},
		},
		{
			name:     "a dollar inside an identifier opens no dollar quote",
			engine:   "postgres",
			input:    "CREATE TABLE prices (amount$gbp$net numeric); SELECT 1;",
			expected: []string{"CREATE TABLE prices (amount$gbp$net numeric)", "SELECT 1"},
		},
		{
			name:     "a dollar inside a MySQL identifier",
			engine:   "mysql",
			input:    "CREATE TABLE prices (amount$gbp INT); SELECT amount$gbp FROM prices;",
			expected: []string{"CREATE TABLE prices (amount$gbp INT)", "SELECT amount$gbp FROM prices"},
		},
		{
			name:     "END CASE closes a CASE statement without reopening it",
			engine:   "mysql",
			input:    "CREATE PROCEDURE p() BEGIN CASE x WHEN 1 THEN SELECT 1; END CASE; END; SELECT 2;",
			expected: []string{"CREATE PROCEDURE p() BEGIN CASE x WHEN 1 THEN SELECT 1; END CASE; END", "SELECT 2"},
		},
		{
			name:     "a backslash-escaped quote inside a double-quoted string",
			engine:   "mysql",
			input:    `INSERT INTO t VALUES ("say \"hi\"; ok"); SELECT 1;`,
			expected: []string{`INSERT INTO t VALUES ("say \"hi\"; ok")`, "SELECT 1"},
		},
		{
			name:     "a hash comment between statements",
			engine:   "clickhouse",
			input:    "SELECT 1; # it's a note\nSELECT 2;",
			expected: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:     "a hash comment inside a statement",
			engine:   "clickhouse",
			input:    "SELECT 1 # it's a note; still the note\n, 2; SELECT 3;",
			expected: []string{"SELECT 1 # it's a note; still the note\n, 2", "SELECT 3"},
		},
		{
			name:     "a backslash-escaped backtick inside an identifier",
			engine:   "clickhouse",
			input:    "CREATE TABLE `a\\`;b` (id Int32) ENGINE = Memory; SELECT 1;",
			expected: []string{"CREATE TABLE `a\\`;b` (id Int32) ENGINE = Memory", "SELECT 1"},
		},
		{
			name:     "a leading byte order mark",
			engine:   "sqlite",
			input:    sqllex.ByteOrderMark + "CREATE TABLE t (id INTEGER); SELECT 1;",
			expected: []string{"CREATE TABLE t (id INTEGER)", "SELECT 1"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			subject := engineNamed(t, testCase.engine)
			lexed, err := sqllex.Split(subject.rules, testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, lexedTexts(lexed))

			parsed, err := subject.parser.ParseStatements(t.Context(), testCase.input)
			require.NoError(t, err)
			require.Equal(t, lexedStarts(lexed), parsedStarts(parsed))
			require.Equal(t, lexedLengths(lexed), parsedLengths(parsed))
		})
	}
}

func TestDialectsReportLexicalErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		engine   string
		input    string
		sentinel error
	}{
		{name: "an unterminated procedural block", engine: "mysql", input: "CREATE PROCEDURE p() BEGIN SELECT 1;", sentinel: sqllex.ErrUnterminatedBlock},
		{name: "an unterminated trigger body", engine: "sqlite", input: "CREATE TRIGGER t AFTER INSERT ON a BEGIN DELETE FROM b;", sentinel: sqllex.ErrUnterminatedBlock},
		{name: "an unterminated string", engine: "postgres", input: "SELECT 'abc", sentinel: sqllex.ErrUnterminatedString},
		{name: "an unterminated block comment", engine: "duckdb", input: "SELECT 1 /* open", sentinel: sqllex.ErrUnterminatedComment},
		{name: "an unterminated quoted identifier", engine: "clickhouse", input: "SELECT `abc", sentinel: sqllex.ErrUnterminatedQuotedIdentifier},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			subject := engineNamed(t, testCase.engine)
			_, err := subject.parser.ParseStatements(t.Context(), testCase.input)
			require.ErrorIs(t, err, testCase.sentinel)
			require.ErrorIs(t, err, sqllex.ErrMalformed)
		})
	}
}

func TestNoTransactionDetection(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		engine   string
		input    string
		expected bool
	}{
		{name: "an index built concurrently", engine: "postgres", input: "CREATE INDEX CONCURRENTLY i ON t (c);", expected: true},
		{name: "a vacuum", engine: "postgres", input: "VACUUM t;", expected: true},
		{name: "a vacuum on SQLite", engine: "sqlite", input: "VACUUM;", expected: true},
		{name: "a literal mention", engine: "postgres", input: "SELECT 'CONCURRENTLY';", expected: false},
		{name: "a continuous aggregate", engine: "timescaledb", input: "CREATE MATERIALIZED VIEW d WITH (timescaledb.continuous) AS SELECT 1;", expected: true},
		{name: "a continuous aggregate refresh", engine: "timescaledb", input: "CALL refresh_continuous_aggregate('d', NULL, NULL);", expected: true},
		{name: "concurrently is not MySQL syntax", engine: "mysql", input: "CREATE INDEX CONCURRENTLY i ON t (c);", expected: false},
		{name: "vacuum is not ClickHouse syntax", engine: "clickhouse", input: "VACUUM;", expected: false},
		{name: "concurrently is not DuckDB syntax", engine: "duckdb", input: "CREATE INDEX CONCURRENTLY i ON t (c);", expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			subject := engineNamed(t, testCase.engine)
			required, err := sqllex.RequiresNoTransaction(subject.rules, testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, required)
			lexicon := syntax.Lexicon{LinePrefix: "--", Rules: subject.rules}
			require.Equal(t, testCase.expected, syntax.NoTransactionRequired(testCase.input, lexicon, namespace.Default()))
		})
	}
}

func TestNoTransactionDetectionOverTheCorpora(t *testing.T) {
	t.Parallel()

	for _, subject := range engines() {
		files, err := lexdiff.Corpus(subject.corpora...)
		require.NoError(t, err)
		for _, file := range files {
			required, err := sqllex.RequiresNoTransaction(subject.rules, file.Content)
			lexicon := syntax.Lexicon{LinePrefix: "--", Rules: subject.rules}
			if err != nil || syntax.ScanMigration(file.Content, lexicon, namespace.Default()).NoTransaction {
				continue
			}
			require.Equal(t, required, syntax.NoTransactionRequired(file.Content, lexicon, namespace.Default()), "%s %s", subject.name, file.Path)
		}
	}
}
