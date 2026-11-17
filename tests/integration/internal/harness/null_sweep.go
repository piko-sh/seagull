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
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_kit"
	"seagull.sh/seagull/sdk/migrate"
)

const (
	// nullSweepRowLimit bounds the rows the sweep reads from one query.
	nullSweepRowLimit = 64

	// nullSweepDeadline bounds one golden case, with its migrations, rows and queries.
	nullSweepDeadline = 30 * time.Second
)

// nullSweepReport counts what a NULL saturation sweep reached, so a run that reached
// nothing is not mistaken for one that found nothing.
type nullSweepReport struct {
	// Skipped lists the cases Seagull could not analyse, so they reached nothing.
	Skipped []string

	// QueryErrors lists the queries and migrations Seagull accepted silently and the engine
	// refused, which a parity check should explain.
	QueryErrors []string

	// ValueErrors lists the queries the engine prepared but failed with the sweep's argument
	// values, which say nothing of the statement.
	ValueErrors []string

	// Cases is the number of golden cases read.
	Cases int

	// Migrated is the number of cases whose migrations the engine applied.
	Migrated int

	// Queries is the number of queries the engine ran.
	Queries int

	// Rows is the number of result rows checked.
	Rows int
}

// CheckGoldenNullSweep fails when the engine returns NULL in a column Seagull types as
// never NULL, which generated code could not scan.
//
// It fills the tables of each golden case's schema with a row whose nullable columns hold
// NULL and a row whose columns all hold values. It then runs every read query the case
// analyses with typed default arguments.
//
// A case whose migrations the engine refuses is skipped and counted, and a query the
// engine refuses is listed in the report rather than failed, since a golden case may use
// a feature of another version of the engine. The sweep fails when it ran no query.
//
// Takes t (*testing.T) which reports nullability violations.
// Takes suite (FuzzSuite) which supplies the dialect and isolated databases.
// Takes testdata (string) which is the directory of the dialect's golden cases.
func CheckGoldenNullSweep(t *testing.T, suite FuzzSuite, testdata string) {
	t.Helper()
	var report nullSweepReport
	entries, err := os.ReadDir(testdata)
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(testdata, entry.Name())
		if _, statErr := os.Stat(filepath.Join(directory, "testspec.json")); statErr != nil {
			continue
		}
		report.Cases++
		t.Run(entry.Name(), func(t *testing.T) {
			sweepCase(t, suite, directory, &report)
		})
	}
	t.Logf("null sweep: %d cases, %d skipped, %d migrated, %d queries, %d rows, %d refused statements, %d value errors",
		report.Cases, len(report.Skipped), report.Migrated, report.Queries, report.Rows, len(report.QueryErrors), len(report.ValueErrors))
	for _, problem := range report.Skipped {
		t.Logf("skipped case: %s", problem)
	}
	for _, problem := range report.QueryErrors {
		t.Logf("query error: %s", problem)
	}
	require.NotZero(t, report.Queries, "the null sweep ran no query")
}

// sweepCase sweeps one golden case in a database of its own.
//
// Takes t (*testing.T) which reports nullability violations.
// Takes suite (FuzzSuite) which supplies the dialect and a database.
// Takes directory (string) which is the case.
// Takes report (*nullSweepReport) which receives the counts.
func sweepCase(t *testing.T, suite FuzzSuite, directory string, report *nullSweepReport) {
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(nullSweepDeadline), errFuzzInputDeadline)
	defer cancel()
	files := caseFiles(t, directory)
	generator, err := seagull.New(suite.Dialect(), seagull.WithSkipFailingQueries())
	require.NoError(t, err)
	details, err := generator.GenerateDetailed(ctx, seagull.Input{Package: "sweep", PackagePath: "", Queries: nil, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	if err != nil || details.Catalogue == nil || details.Report == nil {
		report.Skipped = append(report.Skipped, fmt.Sprintf("%s: Seagull did not analyse the case: %v", filepath.Base(directory), err))
		return
	}
	database := suite.Open(t)
	dropCreatedServerFunctions(t, database, suite.Engine)
	migrator, err := migrate.NewMigrator(database, executionBackend(suite.Engine), files, "migrations")
	require.NoError(t, err)
	if _, upErr := migrator.Up(ctx); upErr != nil {
		if !migrationsFlagged(details.Report.Diagnostics, files) {
			report.QueryErrors = append(report.QueryErrors, fmt.Sprintf("%s/migrations: %v", filepath.Base(directory), upErr))
		}
		return
	}
	report.Migrated++
	seedSweepRows(ctx, database, suite.Engine, details.Catalogue)
	flagged := flaggedQueries(details.Report.Queries, details.Report.Diagnostics)
	for _, query := range details.Report.Queries {
		if !query.ReadOnly || (query.Command != emit.QueryCommandMany && query.Command != emit.QueryCommandOne) || flagged[query] {
			continue
		}
		sweepQuery(ctx, t, database, suite, filepath.Base(directory), query, report)
	}
}

// flaggedQueries finds the queries whose block holds an error or a warning, such as an
// unknown column, which Seagull reported rather than accepted silently.
//
// Takes queries ([]*emit.AnalysedQuery) which are the analysed queries.
// Takes diagnostics ([]seagull.SourceError) which are the run's diagnostics.
//
// Returns map[*emit.AnalysedQuery]bool which holds the queries with such a diagnostic.
func flaggedQueries(queries []*emit.AnalysedQuery, diagnostics []seagull.SourceError) map[*emit.AnalysedQuery]bool {
	flagged := make(map[*emit.AnalysedQuery]bool)
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != seagull.SeverityError && diagnostic.Severity != seagull.SeverityWarning {
			continue
		}
		var owner *emit.AnalysedQuery
		for _, query := range queries {
			if filepath.Base(query.Filename) == filepath.Base(diagnostic.Filename) && query.Line <= diagnostic.Line && (owner == nil || query.Line > owner.Line) {
				owner = query
			}
		}
		if owner != nil {
			flagged[owner] = true
		}
	}
	return flagged
}

// dropCreatedServerFunctions drops, once a case finishes, the ClickHouse user-defined
// functions its migrations created. They belong to the server rather than the case's
// database, so one left behind would collide with a later test that creates a function of
// the same name.
//
// Takes t (*testing.T) which registers the cleanup.
// Takes database (*sql.DB) which is connected to the server.
// Takes engine (string) which names the engine. Only ClickHouse is cleaned.
func dropCreatedServerFunctions(t *testing.T, database *sql.DB, engine string) {
	t.Helper()
	if engine != engineClickHouse {
		return
	}
	before, err := userDefinedFunctions(t.Context(), database)
	require.NoError(t, err, "listing the server's user-defined functions")
	t.Cleanup(func() {
		ctx, cancel := CleanupContext(t)
		defer cancel()
		after, err := userDefinedFunctions(ctx, database)
		if !assert.NoError(t, err, "listing the server's user-defined functions") {
			return
		}
		for name := range after {
			if !before[name] {
				_, err := database.ExecContext(ctx, "DROP FUNCTION IF EXISTS "+quoteSweepName(engine, name))
				assert.NoError(t, err, "dropping the user-defined function %s", name)
			}
		}
	})
}

// userDefinedFunctions lists the ClickHouse server's SQL user-defined functions.
//
// Takes database (*sql.DB) which is connected to the server.
//
// Returns map[string]bool which holds the functions' names.
// Returns error when the server cannot list them.
func userDefinedFunctions(ctx context.Context, database *sql.DB) (map[string]bool, error) {
	rows, err := database.QueryContext(ctx, "SELECT name FROM system.functions WHERE origin = 'SQLUserDefined'")
	if err != nil {
		return nil, fmt.Errorf("querying system.functions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	names := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning a function name: %w", err)
		}
		names[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading system.functions: %w", err)
	}
	return names, nil
}

// migrationsFlagged reports whether Seagull reported an error or a warning in a
// migration, so a migration the engine refuses was not accepted silently.
//
// Takes diagnostics ([]seagull.SourceError) which are the run's diagnostics.
// Takes files (fstest.MapFS) which holds the case's files, so a diagnostic is matched to
// a migration by its file's name.
//
// Returns bool which is true when a migration holds such a diagnostic.
func migrationsFlagged(diagnostics []seagull.SourceError, files fstest.MapFS) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != seagull.SeverityError && diagnostic.Severity != seagull.SeverityWarning {
			continue
		}
		if _, isMigration := files["migrations/"+filepath.Base(diagnostic.Filename)]; isMigration {
			return true
		}
	}
	return false
}

// caseFiles reads a golden case's migration and query files.
//
// Takes t (*testing.T) which reports unreadable files.
// Takes directory (string) which is the case.
//
// Returns fstest.MapFS which holds the files as the generator and the migrator read them.
func caseFiles(t *testing.T, directory string) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	caseFS := os.DirFS(directory)
	for _, folder := range []string{"migrations", "queries"} {
		names, err := fs.Glob(caseFS, folder+"/*.sql")
		require.NoError(t, err)
		for _, name := range names {
			content, readErr := fs.ReadFile(caseFS, name)
			require.NoError(t, readErr)
			files[name] = &fstest.MapFile{Data: content}
		}
	}
	return files
}

// seedSweepRows inserts NULL and non-NULL sample rows into each default-schema table. A
// rejected row leaves earlier accepted rows in place and is not reported.
//
// Takes database (*sql.DB) which holds the schema.
// Takes engine (string) which selects literal spellings.
// Takes catalogue (*dialect.Catalogue) which describes the tables.
func seedSweepRows(ctx context.Context, database *sql.DB, engine string, catalogue *dialect.Catalogue) {
	schema := catalogue.LookupSchema(catalogue.DefaultSchema)
	if schema == nil {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(schema.Tables)) {
		table := schema.Tables[name]
		if table.IsVirtual || table.PartitionOf != nil {
			continue
		}
		for row, saturated := range []bool{true, false} {
			if statement, usable := sweepInsert(engine, table, row+1, saturated); usable {
				_, _ = database.ExecContext(ctx, statement)
			}
		}
	}
}

// sweepInsert builds an INSERT for one sample row. A saturated row uses NULL for every
// nullable column.
//
// Takes engine (string) which selects literal and identifier spellings.
// Takes table (*dialect.Table) which is the table.
// Takes row (int) which numbers the row, so its key values differ from the other row's.
// Takes saturated (bool) which writes NULL in the nullable columns.
//
// Returns string which is the statement.
// Returns bool which is false when a required column has a type the sweep cannot write.
func sweepInsert(engine string, table *dialect.Table, row int, saturated bool) (string, bool) {
	columns, values := make([]string, 0, len(table.Columns)), make([]string, 0, len(table.Columns))
	for index := range table.Columns {
		column := &table.Columns[index]
		if column.IsGenerated || column.Hidden {
			continue
		}
		value := "NULL"
		if !column.Nullable || !saturated {
			literal, known := sweepLiteral(engine, column.SQLType, row)
			if !known && column.HasDefault {
				continue
			}
			if !known {
				return "", false
			}
			value = literal
		}
		columns, values = append(columns, quoteSweepName(engine, column.Name)), append(values, value)
	}
	if len(columns) == 0 {
		return "", false
	}
	return "INSERT INTO " + quoteSweepName(engine, table.Name) + " (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(values, ", ") + ")", true
}

// sweepQuery runs one query with typed default arguments and checks the nullability of
// every value it returns.
//
// Takes t (*testing.T) which reports a NULL in a column typed never NULL.
// Takes database (*sql.DB) which holds the rows.
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes name (string) which names the case.
// Takes query (*emit.AnalysedQuery) which is the analysed query.
// Takes report (*nullSweepReport) which receives the counts.
func sweepQuery(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, name string, query *emit.AnalysedQuery, report *nullSweepReport) {
	t.Helper()
	info := suite.Dialect().Info()
	rendered, err := emit_kit.Render(query.Template, &emit_kit.Renderer{Query: query, Quote: info.IdentifierQuote, Lists: info.Lists,
		Form: emit_kit.ExecutionForm(&info, query), Placeholders: info.Placeholders, ListMode: emit_kit.ListExpand, KeepStars: true, WrapColumn: nil, Prefix: "", Omit: 0})
	if err != nil {
		report.QueryErrors = append(report.QueryErrors, fmt.Sprintf("%s/%s: rendering: %v", name, query.Name, err))
		return
	}
	arguments, err := sweepArguments(query, &rendered)
	if err != nil {
		report.QueryErrors = append(report.QueryErrors, fmt.Sprintf("%s/%s: %v", name, query.Name, err))
		return
	}
	if prepareErr := engineAccepts(ctx, database, suite.Engine, rendered.SQL, arguments...); prepareErr != nil {
		report.QueryErrors = append(report.QueryErrors, fmt.Sprintf("%s/%s: refused: %v", name, query.Name, prepareErr))
		return
	}
	rows, err := database.QueryContext(ctx, rendered.SQL, arguments...)
	if err != nil {
		report.ValueErrors = append(report.ValueErrors, fmt.Sprintf("%s/%s: %v", name, query.Name, err))
		return
	}
	defer func() { _ = rows.Close() }()
	report.Queries++
	columns, err := rows.Columns()
	require.NoError(t, err, "%s/%s: reading the result columns", name, query.Name)
	for read := 0; rows.Next(); read++ {
		if read == nullSweepRowLimit {
			t.Logf("%s/%s: the sweep checked only the first %d rows", name, query.Name, nullSweepRowLimit)
			break
		}
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for index := range values {
			targets[index] = &values[index]
		}
		if scanErr := rows.Scan(targets...); scanErr != nil {
			assert.NoError(t, scanErr, "%s/%s: scanning a row", name, query.Name)
			return
		}
		report.Rows++
		checkSweepRow(t, name, query, rendered.SQL, values)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		report.QueryErrors = append(report.QueryErrors, fmt.Sprintf("%s/%s: %v", name, query.Name, rowsErr))
	}
}

// sweepArguments builds the sweep's typed default argument for each rendered binding.
//
// Takes query (*emit.AnalysedQuery) which holds the parameters' types.
// Takes rendered (*emit_kit.Rendered) which holds the rendered bindings.
//
// Returns []any which holds the driver arguments in binding order.
// Returns error when a binding names a parameter the query does not have.
func sweepArguments(query *emit.AnalysedQuery, rendered *emit_kit.Rendered) ([]any, error) {
	arguments := make([]any, 0, len(rendered.Arguments))
	for _, argument := range rendered.Arguments {
		if argument.Parameter < 0 || argument.Parameter >= len(query.Parameters) {
			return nil, fmt.Errorf("rendered binding %d names no parameter", argument.Parameter)
		}
		value := sweepArgument(query.Parameters[argument.Parameter].SQLType)
		if argument.Name != "" {
			value = sql.Named(argument.Name, value)
		}
		arguments = append(arguments, value)
	}
	return arguments, nil
}

// checkSweepRow fails for a NULL in a column the query's analysis types as never NULL.
//
// Takes t (*testing.T) which reports the violation.
// Takes name (string) which names the case.
// Takes query (*emit.AnalysedQuery) which holds the inferred nullability.
// Takes statement (string) which is the SQL the row came from.
// Takes values ([]any) which holds the row.
func checkSweepRow(t *testing.T, name string, query *emit.AnalysedQuery, statement string, values []any) {
	t.Helper()
	for index, value := range values {
		if index < len(query.OutputColumns) && value == nil && !query.OutputColumns[index].Nullable {
			assert.Failf(t, "NULL in a never NULL column", "%s/%s: column %q is typed never NULL, but the engine returned NULL\n%s",
				name, query.Name, query.OutputColumns[index].Name, statement)
		}
	}
}

// sweepArgument returns a Go value of a parameter's type for the sweep to bind.
//
// Takes sqlType (dialect.SQLType) which is the parameter's type.
//
// Returns any which is the value.
func sweepArgument(sqlType dialect.SQLType) any {
	switch sqlType.Category {
	case dialect.TypeCategoryInteger:
		return int64(1)
	case dialect.TypeCategoryFloat, dialect.TypeCategoryDecimal:
		return float64(1)
	case dialect.TypeCategoryBoolean:
		return true
	case dialect.TypeCategoryTemporal:
		return time.Date(2024, 1, 31, 10, 0, 0, 0, time.UTC)
	case dialect.TypeCategoryBinary:
		return []byte{0}
	case dialect.TypeCategoryUUID:
		return "00000000-0000-0000-0000-000000000001"
	default:
		return "a"
	}
}

// sweepLiteral spells a value of a column's type as a SQL literal of the engine, distinct
// for each row so a key column stays unique.
//
// Takes engine (string) which selects the spelling.
// Takes sqlType (dialect.SQLType) which is the column's type.
// Takes row (int) which numbers the row.
//
// Returns string which is the literal.
// Returns bool which is false for a type the sweep cannot spell.
func sweepLiteral(engine string, sqlType dialect.SQLType, row int) (string, bool) {
	switch sqlType.Category {
	case dialect.TypeCategoryInteger, dialect.TypeCategoryDecimal, dialect.TypeCategoryFloat:
		return strconv.Itoa(row), true
	case dialect.TypeCategoryText:
		return fmt.Sprintf("'v%d'", row), true
	case dialect.TypeCategoryBoolean:
		return "TRUE", true
	case dialect.TypeCategoryUUID:
		return fmt.Sprintf("'00000000-0000-0000-0000-00000000000%d'", row), true
	case dialect.TypeCategoryJSON:
		return "'{}'", true
	case dialect.TypeCategoryBinary:
		if isPostgresFamily(engine) {
			return "'\\x00'", true
		}
		return "X'00'", true
	case dialect.TypeCategoryTemporal:
		switch sqlType.Temporal {
		case dialect.TemporalKindDate:
			return fmt.Sprintf("'2024-01-%02d'", row), true
		case dialect.TemporalKindTime:
			return "'10:00:00'", true
		case dialect.TemporalKindTimestamp:
			return fmt.Sprintf("'2024-01-%02d 10:00:00'", row), true
		default:
			return "", false
		}
	case dialect.TypeCategoryEnum:
		if len(sqlType.EnumValues) > 0 {
			return "'" + strings.ReplaceAll(sqlType.EnumValues[0], "'", "''") + "'", true
		}
		return "", false
	default:
		return "", false
	}
}

// quoteSweepName quotes an identifier for the engine.
//
// Takes engine (string) which selects the quote character.
// Takes name (string) which is the identifier.
//
// Returns string which is the quoted identifier.
func quoteSweepName(engine, name string) string {
	if engine == engineMySQL || engine == engineMariaDB || engine == engineClickHouse {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
