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
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

// unknownFunctionPattern reads the name from Seagull's unknown-function diagnostic.
var unknownFunctionPattern = regexp.MustCompile(`unknown function "([^"]+)"`)

// unknownFunctionMessages lists, per engine, the error text a call of an unknown function
// raises.
var unknownFunctionMessages = map[string][]string{
	engineCockroachDB: {"unknown function"},
	engineMySQL:       {"Error 1305", "Error 1630"},
	engineMariaDB:     {"Error 1305", "Error 1630"},
	engineSQLite:      {"no such function"},
	engineClickHouse:  {"UNKNOWN_FUNCTION"},
}

// postgresSyntaxFunctions are the names the PostgreSQL family's grammar reads as keywords
// rather than calls of a pg_proc function.
var postgresSyntaxFunctions = map[string]bool{
	"coalesce": true, "current_catalog": true, "current_date": true, "current_role": true,
	"current_schema": true, "current_time": true, "current_timestamp": true, "current_user": true,
	"extract": true, "greatest": true, "grouping": true, "least": true, "localtime": true,
	"localtimestamp": true, "nullif": true, "overlay": true, "position": true, "session_user": true,
	"substring": true, "trim": true, "user": true,
}

// duckdbSyntaxFunctions are the names DuckDB's grammar reads as keywords or rewrites,
// which duckdb_functions() does not list. current_time, current_timestamp, localtime and
// localtimestamp are keywords only, which DuckDB refuses with parentheses.
var duckdbSyntaxFunctions = map[string]bool{
	"columns": true, "current_time": true, "current_timestamp": true, "extract": true,
	"grouping": true, "grouping_id": true, "if": true, "ifnull": true, "localtime": true,
	"localtimestamp": true, "percentile_cont": true, "percentile_disc": true, "try": true,
}

// CheckFunctionNames calls each function a dialect's builtin_functions.spec names and
// Seagull accepts, and fails for each one the engine says does not exist, whose query
// Seagull would generate and the engine refuse when it runs.
//
// A call refused for another reason, such as its arguments or a keyword syntax, says
// nothing about the name and passes.
//
// Takes t (*testing.T) which reports the unknown names.
// Takes suite (FuzzSuite) which supplies a database and the dialect.
// Takes specPath (string) which is the spec file.
// Takes exclusions (map[string]string) which maps names the engine's build leaves out to
// the reason, for a driver whose bundled engine omits them.
func CheckFunctionNames(t *testing.T, suite FuzzSuite, specPath string, exclusions map[string]string) {
	t.Helper()
	names, err := readSpecNames(specPath)
	require.NoError(t, err)
	unknownToSeagull := seagullUnknownFunctions(t, suite, names)
	database := suite.Open(t)
	checked, missing := 0, 0
	for _, name := range names {
		if unknownToSeagull[strings.ToLower(name)] {
			continue
		}
		if reason, excluded := exclusions[name]; excluded {
			t.Logf("capability exclusion: %s: %s", name, reason)
			continue
		}
		checked++
		lacks, err := engineLacksFunction(t.Context(), database, suite.Engine, name)
		require.NoError(t, err, "asking the engine for %s", name)
		if lacks {
			missing++
			assert.Failf(t, "unknown function", "Seagull accepts calls of %s, which the engine does not have", name)
		}
	}
	t.Logf("function name audit: %d names read, %d accepted by Seagull and checked, %d unknown to the engine", len(names), checked, missing)
}

// readSpecNames reads the distinct function names of a builtin_functions.spec, aggregates
// and every other kind included.
//
// Takes path (string) which is the spec file.
//
// Returns []string which holds the names in spec order.
// Returns error when the file cannot be read.
func readSpecNames(path string) ([]string, error) {
	lines, err := readSpecLines(path)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var names []string
	for _, line := range lines {
		open := strings.Index(line, "(")
		if line == "" || strings.HasPrefix(line, "#") || open <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:open])
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, nil
}

// seagullUnknownFunctions vets one query calling each name and reads which names Seagull
// does not know, such as those a dialect built on another one's spec leaves out.
//
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes names ([]string) which are the names.
//
// Returns map[string]bool which holds the lower-cased names Seagull reports as unknown.
func seagullUnknownFunctions(t *testing.T, suite FuzzSuite, names []string) map[string]bool {
	t.Helper()
	var queries strings.Builder
	for index, name := range names {
		fmt.Fprintf(&queries, "-- sgl.query(name: Name%d, command: many)\nSELECT %s() AS r;\n\n", index, name)
	}
	files := fstest.MapFS{
		"migrations/0001_names.up.sql": {Data: []byte("CREATE TABLE function_names (id INT)" + tableSuffix(suite.Engine) + ";\n")},
		"queries/names.sql":            {Data: []byte(queries.String())},
	}
	generator, err := seagull.New(suite.Dialect(), seagull.WithSkipFailingQueries())
	require.NoError(t, err)
	report, err := generator.Vet(t.Context(), seagull.Input{Package: "", PackagePath: "", Queries: files, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	unknown := make(map[string]bool)
	for _, diagnostic := range report.Diagnostics {
		if match := unknownFunctionPattern.FindStringSubmatch(diagnostic.Message); match != nil {
			unknown[strings.ToLower(match[1])] = true
		}
	}
	return unknown
}

// engineLacksFunction checks whether an engine recognizes a function name. It calls the
// function when the engine's catalogue may omit valid functions.
//
// Takes database (*sql.DB) which is connected to the engine.
// Takes engine (string) which names the engine.
// Takes name (string) which is the function's name.
//
// Returns bool which reports whether the engine says the name is unknown.
// Returns error when a catalogue query fails.
func engineLacksFunction(ctx context.Context, database *sql.DB, engine, name string) (bool, error) {
	known, decided, err := catalogueKnowsFunction(ctx, database, engine, name)
	if err != nil || known {
		return false, err
	}
	if decided {
		return true, nil
	}
	return callReportsUnknownFunction(ctx, database, engine, name), nil
}

// catalogueKnowsFunction asks the engine's catalogue for a function name.
//
// Takes database (*sql.DB) which is connected to the engine.
// Takes engine (string) which names the engine.
// Takes name (string) which is the function's name.
//
// Returns known (bool) which is true when the catalogue or grammar has the name.
// Returns decided (bool) which is true when the catalogue's answer is final.
// Returns err (error) when a catalogue query fails.
func catalogueKnowsFunction(ctx context.Context, database *sql.DB, engine, name string) (known, decided bool, err error) {
	lower := strings.ToLower(name)
	switch {
	case isPostgresFamily(engine):
		if postgresSyntaxFunctions[lower] {
			return true, true, nil
		}
		known, err = inCatalogue(ctx, database, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_proc WHERE proname = lower($1))", name)
		return known, engine != engineCockroachDB, err
	case engine == engineDuckDB:
		if duckdbSyntaxFunctions[lower] {
			return true, true, nil
		}
		known, err = inCatalogue(ctx, database, "SELECT EXISTS (SELECT 1 FROM duckdb_functions() WHERE function_name = ?)", name)
		return known, true, err
	case engine == engineClickHouse:
		known, err = inCatalogue(ctx, database, "SELECT count() > 0 FROM system.functions WHERE name = ? OR (case_insensitive AND lower(name) = lower(?))", name, name)
		if err != nil || known {
			return known, true, err
		}
		known, err = inCatalogue(ctx, database, "SELECT count() > 0 FROM system.table_functions WHERE name = ?", name)
		return known, false, err
	default:
		return false, false, nil
	}
}

// callReportsUnknownFunction calls a function with no arguments and reads whether the
// engine's error says the function does not exist.
//
// Takes database (*sql.DB) which is connected to the engine.
// Takes engine (string) which names the engine.
// Takes name (string) which is the function's name.
//
// Returns bool which is true only when the error names an unknown function.
func callReportsUnknownFunction(ctx context.Context, database *sql.DB, engine, name string) bool {
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	rows, err := database.QueryContext(ctx, "SELECT "+name+"()") //nolint:gosec // the name is a function the spec in the repository declares
	if err == nil {
		_ = rows.Close()
		return false
	}
	message := err.Error()
	return slices.ContainsFunc(unknownFunctionMessages[engine], func(marker string) bool { return strings.Contains(message, marker) })
}

// inCatalogue reports whether a catalogue query finds a name.
//
// Takes database (*sql.DB) which is connected to the engine.
// Takes query (string) which selects one boolean.
// Takes arguments (...any) which bind its placeholders.
//
// Returns bool which is true when the catalogue holds the name.
// Returns error when the query fails.
func inCatalogue(ctx context.Context, database *sql.DB, query string, arguments ...any) (bool, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	var found bool
	if err := database.QueryRowContext(ctx, query, arguments...).Scan(&found); err != nil {
		return false, fmt.Errorf("querying the function catalogue: %w", err)
	}
	return found, nil
}
