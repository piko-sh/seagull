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

package harness

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

const (
	// splitterLogTable creates the table the cases count their effects in.
	splitterLogTable = "CREATE TABLE splitter_log (n INT);\n"

	// splitterOneRow is the row count of a case that leaves one row.
	splitterOneRow = 1

	// splitterTwoRows is the row count of a case that leaves two rows.
	splitterTwoRows = 2

	// splitterThreeRows is the row count of a case that leaves three rows.
	splitterThreeRows = 3

	// splitterFourRows is the row count of a case that leaves four rows.
	splitterFourRows = 4
)

// splitterCases lists the cases for each engine.
var splitterCases = map[string][]splitterCase{
	enginePostgres:    postgresSplitterCases(),
	engineTimescaleDB: postgresSplitterCases(),
	engineCockroachDB: cockroachSplitterCases(),
	engineMySQL:       mysqlSplitterCases(),
	engineMariaDB:     mariadbSplitterCases(),
	engineSQLite:      sqliteSplitterCases(),
	engineDuckDB: {
		{name: "quotes and comments", rows: splitterThreeRows, migration: "CREATE TABLE splitter_log (n INT, note VARCHAR);\n" +
			"INSERT INTO splitter_log VALUES (1, 'a;b'), (2, 'it''s;'); -- trailing; comment\n" +
			"/* block; comment */ INSERT INTO splitter_log VALUES (3, E'x\\';y');\n"},
	},
	engineClickHouse: {
		{name: "quotes and comments", rows: splitterThreeRows, migration: "CREATE TABLE splitter_log (n Int32, note String) ENGINE = MergeTree ORDER BY n;\n" +
			"INSERT INTO splitter_log VALUES (1, 'a;b'), (2, 'it\\'s;'); -- trailing; comment\n" +
			"/* block; comment */ INSERT INTO splitter_log VALUES (3, 'c;d');\n"},
	},
}

// splitterCase is a migration whose statements hold the separators, quotes, comments and
// procedural bodies a statement splitter must see past, and the number of rows its
// statements leave in the splitter_log table when each runs whole and once.
type splitterCase struct {
	// name names the case.
	name string

	// migration is the migration's text.
	migration string

	// rows is how many rows splitter_log holds after the migration.
	rows int
}

// cockroachSplitterCases lists the cases every PostgreSQL family engine runs, which are
// all CockroachDB runs.
//
// Returns []splitterCase which holds the cases.
func cockroachSplitterCases() []splitterCase {
	return []splitterCase{
		{name: "quotes and comments", rows: splitterFourRows, migration: "CREATE TABLE splitter_log (n INT, note TEXT);\n" +
			"INSERT INTO splitter_log VALUES (1, 'a;b'), (2, 'it''s;'); -- trailing; comment\n" +
			"/* block; /* nested; */ comment */ INSERT INTO splitter_log VALUES (3, E'x\\';y');\n" +
			"INSERT INTO splitter_log VALUES (4, $q$dollar; 'quoted'$q$);\n"},
		{name: "quoted identifiers", rows: splitterOneRow, migration: "CREATE TABLE \"splitter;log\" (n INT);\n" +
			splitterLogTable + "INSERT INTO splitter_log SELECT 1;\n"},
	}
}

// postgresSplitterCases lists the PostgreSQL and TimescaleDB cases, which add DO blocks
// and PL/pgSQL bodies to the family's cases.
//
// Returns []splitterCase which holds the cases.
func postgresSplitterCases() []splitterCase {
	return append(cockroachSplitterCases(),
		splitterCase{name: "do block", rows: splitterThreeRows, migration: splitterLogTable +
			"DO $$ BEGIN INSERT INTO splitter_log VALUES (1); INSERT INTO splitter_log VALUES (2); END $$;\n" +
			"INSERT INTO splitter_log VALUES (3);\n"},
		splitterCase{name: "function body", rows: splitterTwoRows, migration: splitterLogTable +
			"CREATE FUNCTION splitter_one() RETURNS int LANGUAGE plpgsql AS $fn$ BEGIN RETURN 1; END $fn$;\n" +
			"INSERT INTO splitter_log SELECT splitter_one();\n" +
			"CREATE FUNCTION splitter_two() RETURNS int LANGUAGE sql AS 'SELECT 2; ';\n" +
			"INSERT INTO splitter_log SELECT splitter_two();\n"},
	)
}

// mariadbSplitterCases lists MariaDB's cases, which add a BEGIN NOT ATOMIC block to the
// MySQL family's cases.
//
// Returns []splitterCase which holds the cases.
func mariadbSplitterCases() []splitterCase {
	return append(mysqlSplitterCases(), splitterCase{name: "begin not atomic", rows: splitterTwoRows, migration: splitterLogTable +
		"BEGIN NOT ATOMIC\n  INSERT INTO splitter_log VALUES (1);\n  INSERT INTO splitter_log VALUES (2);\nEND;\n"})
}

// mysqlSplitterCases lists the MySQL family's cases.
//
// Returns []splitterCase which holds the cases.
func mysqlSplitterCases() []splitterCase {
	return []splitterCase{
		{name: "quotes and comments", rows: splitterFourRows, migration: "CREATE TABLE splitter_log (n INT, note TEXT);\n" +
			"INSERT INTO splitter_log VALUES (1, 'a;b'), (2, 'it\\'s;'); -- trailing; comment\n" +
			"# hash; comment\n/* block; comment */ INSERT INTO splitter_log VALUES (3, \"double;quoted\");\n" +
			"INSERT INTO `splitter_log` VALUES (4, 'x');\n"},
		{name: "trigger body", rows: splitterTwoRows, migration: "CREATE TABLE splitter_log (n INT);\nCREATE TABLE splitter_source (n INT);\n" +
			"CREATE TRIGGER splitter_copy AFTER INSERT ON splitter_source FOR EACH ROW BEGIN\n" +
			"  INSERT INTO splitter_log VALUES (NEW.n);\n  INSERT INTO splitter_log VALUES (NEW.n + 10);\nEND;\n" +
			"INSERT INTO splitter_source VALUES (1);\n"},
		{name: "delimiter", rows: splitterTwoRows, migration: "CREATE TABLE splitter_log (n INT);\nDELIMITER //\n" +
			"CREATE PROCEDURE splitter_fill() BEGIN\n  INSERT INTO splitter_log VALUES (1);\n  INSERT INTO splitter_log VALUES (2);\nEND //\n" +
			"DELIMITER ;\nCALL splitter_fill();\n"},
		{name: "event body", rows: splitterOneRow, migration: splitterLogTable + "CREATE TABLE splitter_events (n INT);\n" +
			"CREATE EVENT splitter_tidy ON SCHEDULE EVERY 1 DAY DO BEGIN\n  DELETE FROM splitter_events WHERE n < 0;\n" +
			"  INSERT INTO splitter_events VALUES (-1);\nEND;\nINSERT INTO splitter_log VALUES (1);\n"},
		{name: "if and case blocks", rows: splitterOneRow, migration: splitterLogTable +
			"CREATE PROCEDURE splitter_branch(x INT) BEGIN\n  IF x > 0 THEN\n    INSERT INTO splitter_log VALUES (CASE WHEN x > 1 THEN 2 ELSE 1 END);\n" +
			"  END IF;\nEND;\nCALL splitter_branch(1);\n"},
	}
}

// sqliteSplitterCases lists SQLite's cases.
//
// Returns []splitterCase which holds the cases.
func sqliteSplitterCases() []splitterCase {
	return []splitterCase{
		{name: "quotes and comments", rows: splitterThreeRows, migration: "CREATE TABLE splitter_log (n INT, note TEXT);\n" +
			"INSERT INTO splitter_log VALUES (1, 'a;b'), (2, 'it''s;'); -- trailing; comment\n" +
			"/* block; comment */ INSERT INTO splitter_log VALUES (3, \"double;quoted\");\n"},
		{name: "trigger body", rows: splitterTwoRows, migration: "CREATE TABLE splitter_log (n INT);\nCREATE TABLE splitter_source (n INT);\n" +
			"CREATE TRIGGER splitter_copy AFTER INSERT ON splitter_source BEGIN\n" +
			"  INSERT INTO splitter_log VALUES (NEW.n);\n  INSERT INTO splitter_log VALUES (CASE WHEN NEW.n > 0 THEN NEW.n + 10 ELSE 0 END);\nEND;\n" +
			"INSERT INTO splitter_source VALUES (1);\n"},
		{name: "bracketed and backquoted names", rows: splitterOneRow, migration: "CREATE TABLE [splitter;a] (n INT);\nCREATE TABLE `splitter;b` (n INT);\n" +
			"CREATE TABLE splitter_log (n INT);\nINSERT INTO splitter_log VALUES (1);\n"},
	}
}

// CheckSplitterConstructs applies, each in a database of its own, migrations whose
// statements hold separators inside quotes, comments and procedural bodies, and checks
// that every statement ran whole and once by the rows they leave.
//
// Takes t (*testing.T) which reports a case that failed or left other rows.
// Takes suite (FuzzSuite) which supplies databases.
func CheckSplitterConstructs(t *testing.T, suite FuzzSuite) {
	t.Helper()
	cases, known := splitterCases[suite.Engine]
	if !known {
		t.Skipf("capability exclusion: no splitter cases are written for %s", suite.Engine)
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkSplitterCase(t.Context(), t, suite, testCase)
		})
	}
}

// checkSplitterCase applies one case and counts the rows it leaves.
//
// Takes t (*testing.T) which reports the outcome.
// Takes suite (FuzzSuite) which supplies the database.
// Takes testCase (splitterCase) which is the case.
func checkSplitterCase(ctx context.Context, t *testing.T, suite FuzzSuite, testCase splitterCase) {
	t.Helper()
	database := suite.Open(t)
	files := fstest.MapFS{"migrations/0001_split.up.sql": {Data: []byte(testCase.migration)}}
	migrator, err := migrate.NewMigrator(database, executionBackend(suite.Engine), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.NoError(t, err, "the migration did not apply")
	var rows int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count(*) FROM splitter_log").Scan(&rows))
	assert.Equal(t, testCase.rows, rows, "the migration left the wrong number of rows, so a statement ran in parts or more than once")
}
