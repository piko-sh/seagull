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
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_kit"
)

const (
	// engineSQLite names the SQLite engine in fuzz models.
	engineSQLite = "sqlite"

	// enginePostgres names the PostgreSQL engine in fuzz models.
	enginePostgres = "postgres"

	// engineMySQL names the MySQL engine in fuzz models.
	engineMySQL = "mysql"

	// engineMariaDB names the MariaDB engine in fuzz models.
	engineMariaDB = "mariadb"

	// engineDuckDB names the DuckDB engine in fuzz models.
	engineDuckDB = "duckdb"

	// engineClickHouse names the ClickHouse engine in fuzz models.
	engineClickHouse = "clickhouse"

	// engineCockroachDB names the CockroachDB engine in fuzz models.
	engineCockroachDB = "cockroachdb"

	// engineTimescaleDB names the TimescaleDB engine in fuzz models.
	engineTimescaleDB = "timescaledb"

	// OracleQuery compares generated results with an independently written query.
	OracleQuery FuzzOracle = "query"

	// OracleCount compares the generated count with an independent count.
	OracleCount FuzzOracle = "count"

	// OracleTypes compares inferred types and nullability with the engine's.
	OracleTypes FuzzOracle = "types"

	// OracleMigration compares replayed schemas with the live schema.
	OracleMigration FuzzOracle = "migration"

	// databaseInputDeadline bounds one semantic input.
	databaseInputDeadline = 20 * time.Second

	// databaseWatchdogDeadline terminates unresponsive workers.
	databaseWatchdogDeadline = 40 * time.Second

	// databaseStatementDeadline bounds an individual database operation.
	databaseStatementDeadline = 5 * time.Second

	// databaseRowBound prevents unexpectedly unbounded results.
	databaseRowBound = 512

	// migrationChoiceCount includes the unchanged schema and three alterations.
	migrationChoiceCount = 4

	// fuzzNullText spells SQL NULL in encoded results.
	fuzzNullText = "NULL"

	// fuzzModelPrefix starts the name of every relation a fuzz model creates.
	fuzzModelPrefix = "fuzz_"

	// fuzzItemsSnapshot reads every modelled row after a write.
	fuzzItemsSnapshot = "SELECT id, score, category FROM fuzz_items"
)

var (
	// errFuzzInputDeadline is the cause of a fuzz input's context once its time limit has
	// passed.
	errFuzzInputDeadline = errors.New("the fuzz input did not finish within its time limit")

	// errFuzzStatementDeadline is the cause of one database statement's context once its
	// time limit has passed.
	errFuzzStatementDeadline = errors.New("the database statement did not finish within its time limit")

	// fuzzOracles lists every oracle FuzzDatabase accepts.
	fuzzOracles = []FuzzOracle{OracleQuery, OracleCount, OracleTypes, OracleMigration}

	// migrationIntegerTypes spells the added column's integer type where BIGINT does not
	// fit.
	migrationIntegerTypes = map[string]string{engineSQLite: "INTEGER", engineClickHouse: "Int64"}

	// postgresTypeAliases maps PostgreSQL's internal type names to their SQL spellings.
	postgresTypeAliases = map[string]string{"int2": "smallint", "int4": "integer", "int8": "bigint", "float4": "real", "float8": "double precision"}
)

// FuzzOracle selects what FuzzDatabase compares for each input.
type FuzzOracle string

// FuzzSuite supplies isolated databases and dialect facts for semantic fuzzing.
type FuzzSuite struct {
	// Dialect creates a fresh compiler dialect.
	Dialect func() dialect.Dialect

	// Open creates a database owned by one fuzz input and registers cleanup.
	Open func(*testing.T) *sql.DB

	// ParseType reads engine type names using the existing integration adapter.
	ParseType func(string) (ReportedType, bool)

	// Engine names the SQL engine independently of the driver variant.
	Engine string

	// TypeFunction observes expression types when result metadata is insufficient.
	TypeFunction string

	// Width enables exact numeric width and signedness comparisons.
	Width bool
}

// FuzzDatabase runs one semantic oracle over bounded structured inputs.
//
// Takes f (*testing.F) which registers corpus inputs.
// Takes suite (FuzzSuite) which supplies engine-specific adapters.
// Takes oracle (FuzzOracle) which selects query, count, types or migration checks.
func FuzzDatabase(f *testing.F, suite FuzzSuite, oracle FuzzOracle) {
	f.Helper()
	if suite.Open == nil || suite.Dialect == nil || suite.ParseType == nil {
		require.Fail(f, "incomplete database fuzz suite")
	}
	require.Contains(f, fuzzOracles, oracle, "unknown database oracle")
	for _, seed := range fuzzcase.Seeds(suite.Engine) {

		model, ok := fuzzcase.Decode(suite.Engine, seed)
		if ok && oracleNeedsRows(oracle) && model.Query().Writes {
			continue
		}
		f.Add(seed)
	}
	extra := fuzzcase.ScenarioSeeds(suite.Engine)
	if oracle == OracleMigration {
		extra = fuzzcase.MigrationSeeds(suite.Engine)
	}
	for _, seed := range extra {
		f.Add(seed)
	}
	var metadata sync.Once
	f.Fuzz(func(t *testing.T, data []byte) {
		runFuzzDatabaseInput(t, suite, oracle, data, &metadata)
	})
}

// runFuzzDatabaseInput dispatches supported inputs to their semantic oracle.
//
// Takes t (*testing.T) which reports input failures.
// Takes suite (FuzzSuite) which supplies engine adapters.
// Takes oracle (FuzzOracle) which selects the required observations.
// Takes data ([]byte) which holds a bounded versioned model.
// Takes metadata (*sync.Once) which records the live engine version.
func runFuzzDatabaseInput(t *testing.T, suite FuzzSuite, oracle FuzzOracle, data []byte, metadata *sync.Once) {
	t.Helper()
	if scenario, ok := fuzzcase.DecodeScenario(suite.Engine, data); ok && oracle != OracleMigration {
		runFuzzScenario(t, suite, oracle, scenario, metadata)
		return
	}
	if oracle == OracleMigration {
		if model, ok := fuzzcase.DecodeMigration(suite.Engine, data); ok {
			runFuzzMigrationSequence(t, suite, model, metadata)
			return
		}
	}
	model, ok := fuzzcase.Decode(suite.Engine, data)
	if !ok {
		t.Skip("unsupported version, shape or size")
	}
	if oracleNeedsRows(oracle) && model.Query().Writes {
		t.Skip("oracle requires a row query")
	}
	runFuzzDatabase(t, suite, oracle, model, metadata)
}

// oracleNeedsRows reports whether an oracle can only check a query that returns rows.
//
// Takes oracle (FuzzOracle) which is the oracle.
//
// Returns bool which is true for the count and types oracles.
func oracleNeedsRows(oracle FuzzOracle) bool {
	return oracle == OracleCount || oracle == OracleTypes
}

// fuzzAnalyse requires a positive model to generate a query and files.
//
// Takes t (*testing.T) which reports failures.
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes model (fuzzcase.Model) which supplies SQL.
//
// Returns *seagull.DetailedResult which holds successful generation.
func fuzzAnalyse(ctx context.Context, t *testing.T, suite FuzzSuite, model fuzzcase.Model) *seagull.DetailedResult {
	t.Helper()
	return fuzzGenerate(ctx, t, suite, strings.Join(model.Migrations(), "\n"), model.Query().SQL)
}

// fuzzGenerate generates one migration and one query file, and requires one generated
// query and clean generation.
//
// Takes t (*testing.T) which reports failures.
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes migration (string) which is the schema.
// Takes queries (string) which is the annotated query file.
//
// Returns *seagull.DetailedResult which holds successful generation.
func fuzzGenerate(ctx context.Context, t *testing.T, suite FuzzSuite, migration, queries string) *seagull.DetailedResult {
	t.Helper()
	generator, err := seagull.New(suite.Dialect())
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations/001_fuzz.up.sql": &fstest.MapFile{Data: []byte(migration)},
		"queries/fuzz.sql":           &fstest.MapFile{Data: []byte(queries)},
	}
	details, err := generator.GenerateDetailed(ctx, seagull.Input{Package: "generated", PackagePath: "", Queries: nil, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	require.NoError(t, details.Result.Err())
	require.Len(t, details.Report.Queries, 1, "the query did not reach generation")
	require.NotEmpty(t, details.Result.Files, "the query did not reach generation")
	return details
}

// fuzzRender binds independently modelled values to the rendered argument identities.
//
// Takes t (*testing.T) which reports problems.
// Takes suite (FuzzSuite) which supplies execution facts.
// Takes model (fuzzcase.Model) which supplies argument values.
// Takes query (*emit.AnalysedQuery) which owns the template.
// Takes template (emit.SQLTemplate) which is the query or count template.
//
// Returns string which is executable SQL.
// Returns []any which holds driver arguments.
func fuzzRender(t *testing.T, suite FuzzSuite, model fuzzcase.Model, query *emit.AnalysedQuery, template emit.SQLTemplate) (string, []any) {
	t.Helper()
	rendered := fuzzRenderTemplate(t, suite, query, template)
	values := model.Query().Values
	arguments := make([]any, 0, len(rendered.Arguments))
	for _, argument := range rendered.Arguments {
		require.True(t, argument.Parameter >= 0 && argument.Parameter < len(query.Parameters), "rendered parameter index is out of bounds")
		number := query.Parameters[argument.Parameter].Number
		require.True(t, number >= 1 && number <= len(values), "parameter number %d is outside the independent model", number)
		value := values[number-1].DriverValue(suite.Engine)
		if argument.Name != "" {
			value = sql.Named(argument.Name, value)
		}
		arguments = append(arguments, value)
	}
	return rendered.SQL, arguments
}

// fuzzRenderTemplate renders a template the way generated execution code does.
//
// Takes t (*testing.T) which reports render failures.
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes query (*emit.AnalysedQuery) which owns the template.
// Takes template (emit.SQLTemplate) which is the query or count template.
//
// Returns emit_kit.Rendered which holds the SQL and its argument identities.
func fuzzRenderTemplate(t *testing.T, suite FuzzSuite, query *emit.AnalysedQuery, template emit.SQLTemplate) emit_kit.Rendered {
	t.Helper()
	info := suite.Dialect().Info()
	rendered, err := emit_kit.Render(template, &emit_kit.Renderer{Query: query, Quote: info.IdentifierQuote, Lists: info.Lists,
		Form: emit_kit.ExecutionForm(&info, query), Placeholders: info.Placeholders, ListMode: emit_kit.ListExpand, KeepStars: true, WrapColumn: nil, Prefix: "", Omit: 0})
	require.NoError(t, err)
	return rendered
}

// fuzzEquivalent compares rendered execution with independently written literal SQL.
//
// Takes t (*testing.T) which reports mismatches.
// Takes database (*sql.DB) which contains the input data.
// Takes suite (FuzzSuite) which supplies isolated reference databases.
// Takes model (fuzzcase.Model) which supplies the reference query.
// Takes query (*emit.AnalysedQuery) which holds the generated template.
func fuzzEquivalent(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, model fuzzcase.Model, query *emit.AnalysedQuery) {
	t.Helper()
	sqlText, arguments := fuzzRender(t, suite, model, query, query.Template)
	reference := model.Query().Reference
	var want, got [][]any
	if model.Query().Writes {
		other := suite.Open(t)
		other.SetMaxOpenConns(1)
		fuzzApply(ctx, t, other, append(model.Migrations(), model.Inserts()...))
		wantResult := fuzzExec(ctx, t, other, reference, nil)
		gotResult := fuzzExec(ctx, t, database, sqlText, arguments)
		checkFuzzAffectedRows(t, suite.Engine, wantResult, gotResult)
		want = fuzzRows(ctx, t, other, fuzzItemsSnapshot, nil)
		got = fuzzRows(ctx, t, database, fuzzItemsSnapshot, nil)
	} else {
		want = fuzzRows(ctx, t, database, reference, nil)
		got = fuzzRows(ctx, t, database, sqlText, arguments)
	}
	compare := fuzzResultsEqual
	if model.ExactValues() {
		compare = fuzzExactResultsEqual
	}
	require.True(t, compare(want, got, model.Query().Ordered),
		"result mismatch: input %v\nreference: %s\ngenerated: %s\narguments: %#v\nwant: %#v\ngot: %#v",
		model.Data, reference, sqlText, arguments, want, got)
}

// fuzzExec runs one statement with a shorter deadline than the input.
//
// Takes t (*testing.T) which reports execution failures.
// Takes database (*sql.DB) which executes the SQL.
// Takes statement (string) which is executable SQL.
// Takes arguments ([]any) which holds driver arguments.
//
// Returns sql.Result which retains driver-supported affected-row information.
func fuzzExec(ctx context.Context, t *testing.T, database *sql.DB, statement string, arguments []any) sql.Result {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	result, err := database.ExecContext(ctx, statement, arguments...)
	require.NoError(t, err, "executing %s with %#v", statement, arguments)
	return result
}

// checkFuzzAffectedRows compares write counts where the driver exposes them.
//
// Takes t (*testing.T) which reports unsupported or different counts.
// Takes engine (string) which selects the driver contract.
// Takes want (sql.Result) which came from the independent reference.
// Takes got (sql.Result) which came from generated SQL.
func checkFuzzAffectedRows(t *testing.T, engine string, want, got sql.Result) {
	t.Helper()
	if engine == engineClickHouse {

		return
	}
	expected, err := want.RowsAffected()
	require.NoError(t, err, "reference affected-row metadata")
	actual, err := got.RowsAffected()
	require.NoError(t, err, "generated affected-row metadata")
	require.Equal(t, expected, actual, "affected-row mismatch")
}

// fuzzApply executes ordered schema or data statements.
//
// Takes t (*testing.T) which reports failures.
// Takes database (*sql.DB) which receives the changes.
// Takes statements ([]string) which holds ordered SQL.
func fuzzApply(ctx context.Context, t *testing.T, database *sql.DB, statements []string) {
	t.Helper()
	for _, statement := range statements {
		fuzzExec(ctx, t, database, statement, nil)
	}
}

// fuzzRows reads a bounded result without discarding duplicate rows or NULLs.
//
// Takes t (*testing.T) which reports execution failures.
// Takes database (*sql.DB) which executes the query.
// Takes statement (string) which is executable SQL.
// Takes arguments ([]any) which holds driver arguments.
//
// Returns [][]any which owns every scanned byte slice.
func fuzzRows(ctx context.Context, t *testing.T, database *sql.DB, statement string, arguments []any) [][]any {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	rows, err := database.QueryContext(ctx, statement, arguments...)
	require.NoError(t, err, "querying %s with %#v", statement, arguments)
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	require.NoError(t, err)
	var result [][]any
	for rows.Next() {
		require.Less(t, len(result), databaseRowBound, "model exceeded the %d-row reference bound", databaseRowBound)
		result = append(result, fuzzScanRow(t, rows, len(columns)))
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	return result
}

// fuzzValue normalises exact numeric driver representations.
//
// Takes value (any) which is a value from the numeric model.
//
// Returns string which preserves NULL separately from every numeric value.
func fuzzValue(value any) string {
	if value == nil {
		return fuzzNullText
	}
	if bytes, ok := value.([]byte); ok {
		return string(bytes)
	}
	return fuzzDeepValue(reflect.ValueOf(value))
}

// fuzzDeepValue spells pointers, slices and arrays through their elements, so values such
// as a ClickHouse Array(Nullable(Int64)) read as []*int64 compare by content rather than
// by address.
//
// Takes value (reflect.Value) which is a driver value or one of its elements.
//
// Returns string which writes NULL for a nil pointer.
func fuzzDeepValue(value reflect.Value) string {
	switch value.Kind() {
	case reflect.Invalid:
		return fuzzNullText
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return fuzzNullText
		}
		return fuzzDeepValue(value.Elem())
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 {
			return string(value.Bytes())
		}
		items := make([]string, value.Len())
		for index := range items {
			items[index] = fuzzDeepValue(value.Index(index))
		}
		return "[" + strings.Join(items, " ") + "]"
	default:
		return fmt.Sprint(value.Interface())
	}
}

// fuzzMultiset gives result rows a stable order while retaining multiplicity.
//
// Takes rows ([][]any) which holds numeric model results.
//
// Returns []string which holds unambiguous encoded rows.
func fuzzMultiset(rows [][]any) []string {
	result := fuzzSequence(rows)
	slices.Sort(result)
	return result
}

// fuzzResultsEqual compares row order only when the model guarantees it.
//
// Takes want ([][]any) which holds independent reference rows.
// Takes got ([][]any) which holds generated execution rows.
// Takes ordered (bool) which reports a modelled unique ordering key.
//
// Returns bool which reports equal rows with their required multiplicity and order.
func fuzzResultsEqual(want, got [][]any, ordered bool) bool {
	if ordered {
		return slices.Equal(fuzzSequence(want), fuzzSequence(got))
	}
	return slices.Equal(fuzzMultiset(want), fuzzMultiset(got))
}

// fuzzSequence encodes rows without conflating NULL, text, arbitrary bytes, integers or
// reals, so a rendered query that returns a value of another storage kind than the
// reference query differs even when both print the same.
//
// Takes rows ([][]any) which contains independently scanned values.
//
// Returns []string which holds unambiguous row encodings.
func fuzzSequence(rows [][]any) []string {
	return fuzzEncodeRows(rows, func(value any) string {
		if value == nil {
			return fuzzNullText
		}
		return fuzzExactValue(value)
	})
}

// fuzzTypes compares observed values and type metadata with inferred outputs.
//
// Takes t (*testing.T) which reports mismatches.
// Takes database (*sql.DB) which supplies engine results.
// Takes suite (FuzzSuite) which supplies type parsing.
// Takes model (fuzzcase.Model) which supplies independent argument values.
// Takes query (*emit.AnalysedQuery) which holds inferred columns.
func fuzzTypes(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, model fuzzcase.Model, query *emit.AnalysedQuery) {
	t.Helper()
	if suite.Engine == enginePostgres {
		checkFuzzPreparedParameters(ctx, t, database, suite, model, query)
	}
	sqlText, arguments := fuzzRender(t, suite, model, query, query.Template)
	values := fuzzRows(ctx, t, database, sqlText, arguments)
	checkFuzzNulls(t, values, query)
	types := fuzzColumnTypes(ctx, t, database, sqlText, arguments)
	require.Len(t, types, len(query.OutputColumns), "the engine reports a different number of columns")
	if model.SQLiteAffinity() {
		checkFuzzSQLiteStorage(ctx, t, database, suite, model, query, types)
		return
	}
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		name := types[index].DatabaseTypeName()
		if len(values) > 0 {
			name = fuzzObservedType(ctx, t, database, suite, sqlText, arguments, types[index])
		}
		if name == "" && suite.Engine == engineSQLite {
			continue
		}
		name = fuzzResultTypeName(suite, column.SQLType, len(query.Parameters) > 0, name)
		checkFuzzType(t, suite, column.SQLType, name, column.Name)
	}
}

// fuzzColumnTypes reads the driver's column metadata for a query.
//
// Takes t (*testing.T) which reports execution failures.
// Takes database (*sql.DB) which executes the query.
// Takes statement (string) which is executable SQL.
// Takes arguments ([]any) which holds driver arguments.
//
// Returns []*sql.ColumnType which holds one entry per result column.
func fuzzColumnTypes(ctx context.Context, t *testing.T, database *sql.DB, statement string, arguments []any) []*sql.ColumnType {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	rows, err := database.QueryContext(ctx, statement, arguments...)
	require.NoError(t, err, "querying %s with %#v", statement, arguments)
	types, err := rows.ColumnTypes()
	require.NoError(t, rows.Close())
	require.NoError(t, err)
	return types
}

// fuzzResultTypeName accounts for MariaDB's value-dependent parameter promotion.
//
// A decimal result must also hold integer executions of the same prepared query. Integer
// inference still fails when the database produces a decimal result.
//
// Takes suite (FuzzSuite) which supplies the native metadata adapter.
// Takes inferred (dialect.SQLType) which must cover every parameter value.
// Takes parameters (bool) which identifies a bound query.
// Takes name (string) which is the observed native type name.
//
// Returns string which preserves unsafe or unrelated category differences.
func fuzzResultTypeName(suite FuzzSuite, inferred dialect.SQLType, parameters bool, name string) string {
	if suite.Engine == engineMariaDB && parameters && inferred.Category == dialect.TypeCategoryDecimal {
		if reported, known := suite.ParseType(name); known && reported.Category == dialect.TypeCategoryInteger {
			return "DECIMAL"
		}
	}
	return name
}

// fuzzMigration compares column order after each modelled schema change.
//
// Takes t (*testing.T) which reports differences.
// Takes database (*sql.DB) which holds the actual schema.
// Takes suite (FuzzSuite) which supplies the replay engine.
// Takes model (fuzzcase.Model) which supplies bounded schema choices.
func fuzzMigration(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, model fuzzcase.Model) {
	t.Helper()
	statements := model.Migrations()
	integer := cmp.Or(migrationIntegerTypes[suite.Engine], "BIGINT")
	operations := []string{"", "ALTER TABLE fuzz_items ADD COLUMN extra " + integer,
		"ALTER TABLE fuzz_items RENAME COLUMN extra TO renamed", "ALTER TABLE fuzz_items DROP COLUMN renamed"}
	last := int(model.Flags % migrationChoiceCount)
	for index, operation := range operations[:last+1] {
		if operation != "" {
			fuzzExec(ctx, t, database, operation, nil)
			statements = append(statements, operation+";")
		}
		checkFuzzMigration(ctx, t, database, suite, statements, index)
	}
}

// runFuzzDatabase executes one positive model against its selected oracle.
//
// Takes t (*testing.T) which owns the isolated input.
// Takes suite (FuzzSuite) which supplies engine adapters.
// Takes oracle (FuzzOracle) which selects the check.
// Takes model (fuzzcase.Model) which supplies SQL and data.
// Takes metadata (*sync.Once) which records the engine version once per process.
//
// Panics in the watchdog callback if the input ignores cancellation.
func runFuzzDatabase(t *testing.T, suite FuzzSuite, oracle FuzzOracle, model fuzzcase.Model, metadata *sync.Once) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	watchdog := time.AfterFunc(scaled(databaseWatchdogDeadline), func() { panic("database fuzz input ignored cancellation: " + t.Name()) })
	defer watchdog.Stop()
	database := suite.Open(t)
	database.SetMaxOpenConns(1)
	metadata.Do(func() { writeFuzzEngineMetadata(ctx, t, database, suite.Engine) })
	fuzzApply(ctx, t, database, model.Migrations())
	if oracle == OracleMigration {
		fuzzMigration(ctx, t, database, suite, model)
		return
	}
	fuzzApply(ctx, t, database, model.Inserts())
	details := fuzzAnalyse(ctx, t, suite, model)
	query := details.Report.Queries[0]
	switch oracle {
	case OracleQuery:
		fuzzEquivalent(ctx, t, database, suite, model, query)
	case OracleCount:
		fuzzCount(ctx, t, database, suite, model, query)
	case OracleTypes:
		fuzzTypes(ctx, t, database, suite, model, query)
	case OracleMigration:

	}
}

// fuzzCount compares the generated count query with the model's independent count.
//
// Takes t (*testing.T) which reports mismatches.
// Takes database (*sql.DB) which contains the input data.
// Takes suite (FuzzSuite) which supplies execution facts.
// Takes model (fuzzcase.Model) which supplies the reference count.
// Takes query (*emit.AnalysedQuery) which holds the count template.
func fuzzCount(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, model fuzzcase.Model, query *emit.AnalysedQuery) {
	t.Helper()
	require.NotNil(t, query.CountTemplate, "positive shape %d has no count template", model.Shape)
	sqlText, arguments := fuzzRender(t, suite, model, query, *query.CountTemplate)
	want := fuzzRows(ctx, t, database, model.Query().CountReference, nil)
	got := fuzzRows(ctx, t, database, sqlText, arguments)
	require.Empty(t, fuzzCountProblem(want, got),
		"count mismatch: shape %d, input %v\nreference: %s\ncount: %s\narguments: %#v\nwant %d, got %#v",
		model.Shape, model.Data, model.Query().CountReference, sqlText, arguments, len(want), got)
}

// fuzzScanRow owns the values scanned from the current database row.
//
// Takes t (*testing.T) which reports scan failures.
// Takes rows (*sql.Rows) which is positioned at a row.
// Takes width (int) which is the result width.
//
// Returns []any which copies transient driver byte slices.
func fuzzScanRow(t *testing.T, rows *sql.Rows, width int) []any {
	t.Helper()
	values := make([]any, width)
	destinations := make([]any, width)
	for index := range values {
		destinations[index] = &values[index]
	}
	require.NoError(t, rows.Scan(destinations...))
	for index, value := range values {
		if bytes, ok := value.([]byte); ok {
			values[index] = slices.Clone(bytes)
		}
	}
	return values
}

// fuzzObservedType combines driver metadata with non-null expression observations.
//
// Takes t (*testing.T) which reports execution failures.
// Takes database (*sql.DB) which supplies metadata.
// Takes suite (FuzzSuite) which supplies type functions.
// Takes sqlText (string) which is the rendered query.
// Takes arguments ([]any) which supplies its independently modelled bindings.
// Takes column (*sql.ColumnType) which holds driver metadata.
//
// Returns string which names the observed database type.
func fuzzObservedType(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, sqlText string, arguments []any, column *sql.ColumnType) string {
	t.Helper()
	name := column.DatabaseTypeName()
	if suite.TypeFunction == "" || suite.Engine == engineCockroachDB {
		return name
	}
	quote := suite.Dialect().Info().IdentifierQuote
	sqlText = strings.TrimSuffix(strings.TrimSpace(sqlText), ";")
	probe := "SELECT " + fmt.Sprintf(suite.TypeFunction, quote.Quote(column.Name())) + " FROM (" + sqlText + ") fuzz_types"
	for _, row := range fuzzRows(ctx, t, database, probe, arguments) {
		if row[0] == nil {
			continue
		}
		observed := fuzzValue(row[0])
		if observed != "null" && observed != "unknown" {
			return observed
		}
	}
	if suite.Engine == engineSQLite {
		return ""
	}
	return name
}

// checkFuzzType compares inferred types with independently observed engine metadata.
//
// Takes t (*testing.T) which reports mismatches.
// Takes suite (FuzzSuite) which supplies type parsing.
// Takes inferred (dialect.SQLType) which is the compiler's prediction.
// Takes name (string) which names the observed database type.
// Takes column (string) which identifies the output column.
func checkFuzzType(t *testing.T, suite FuzzSuite, inferred dialect.SQLType, name, column string) {
	t.Helper()
	if isPostgresFamily(suite.Engine) {
		if canonical, ok := postgresTypeAliases[strings.ToLower(name)]; ok {
			name = canonical
		}
	}
	reported, ok := suite.ParseType(name)
	if !ok {
		reported, ok = fuzzScalarType(suite.Engine, name)
	}
	require.True(t, ok, "unrecognised database type %q for %s", name, column)
	for _, problem := range compareTypes(inferredType{sqlType: inferred, problem: ""}, name, reported, suite.Width) {
		assert.Failf(t, "column type mismatch", "column %s: %s", column, problem)
	}
}

// checkFuzzMigration compares replay and live metadata at one schema checkpoint.
//
// Takes t (*testing.T) which reports mismatches.
// Takes database (*sql.DB) which holds the actual schema.
// Takes suite (FuzzSuite) which supplies the replay engine.
// Takes statements ([]string) which contains migration history.
// Takes index (int) which identifies the checkpoint.
func checkFuzzMigration(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, statements []string, index int) {
	t.Helper()
	catalogue := replayFuzzCatalogue(ctx, t, suite, statements)
	table := catalogue.LookupTable("", "fuzz_items")
	require.NotNil(t, table, "replay lost the modelled table")
	actual := fuzzColumnNames(ctx, t, database, fmt.Sprintf("SELECT * FROM fuzz_items WHERE 1 = 0 /* schema step %d */", index))
	var expected []string
	for columnIndex := range table.Columns {
		column := &table.Columns[columnIndex]
		if !column.Hidden {
			expected = append(expected, column.Name)
		}
	}
	checkFuzzSchema(ctx, t, database, suite, table.Columns)
	require.Equal(t, expected, actual, "migration step %d: replay and database columns differ", index)
}

// fuzzScalar reads a query that returns one value.
//
// Takes t (*testing.T) which reports a failed query or another shape.
// Takes database (*sql.DB) which executes the query.
// Takes statement (string) which is executable SQL.
//
// Returns any which is the value.
func fuzzScalar(ctx context.Context, t *testing.T, database *sql.DB, statement string) any {
	t.Helper()
	rows := fuzzRows(ctx, t, database, statement, nil)
	require.Len(t, rows, 1, "%s returned another number of rows", statement)
	require.Len(t, rows[0], 1, "%s returned another number of columns", statement)
	return rows[0][0]
}

// fuzzColumnNames reads the column names of a query's result.
//
// Takes t (*testing.T) which reports execution failures.
// Takes database (*sql.DB) which executes the query.
// Takes statement (string) which is executable SQL.
// Takes arguments (...any) which holds driver arguments.
//
// Returns []string which holds the names in result order.
func fuzzColumnNames(ctx context.Context, t *testing.T, database *sql.DB, statement string, arguments ...any) []string {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	rows, err := database.QueryContext(ctx, statement, arguments...)
	require.NoError(t, err, "querying %s with %#v", statement, arguments)
	names, err := rows.Columns()
	require.NoError(t, rows.Close())
	require.NoError(t, err)
	return names
}

// isPostgresFamily reports whether an engine speaks PostgreSQL's catalogue and type
// names.
//
// Takes engine (string) which names the engine.
//
// Returns bool which is true for PostgreSQL, CockroachDB and TimescaleDB.
func isPostgresFamily(engine string) bool {
	return engine == enginePostgres || engine == engineCockroachDB || engine == engineTimescaleDB
}

// checkFuzzNulls rejects observed NULLs in columns inferred as non-nullable.
//
// Takes t (*testing.T) which reports mismatches.
// Takes values ([][]any) which contains engine results.
// Takes query (*emit.AnalysedQuery) which holds inferred columns.
func checkFuzzNulls(t *testing.T, values [][]any, query *emit.AnalysedQuery) {
	t.Helper()
	for _, problem := range fuzzNullProblems(values, query.OutputColumns) {
		assert.Fail(t, problem)
	}
}

// replayFuzzCatalogue rebuilds migration history and requires clean diagnostics.
//
// Takes t (*testing.T) which reports unexpected replay errors.
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes statements ([]string) which holds the applied migration prefix.
//
// Returns *dialect.Catalogue which contains replayed schema objects.
func replayFuzzCatalogue(ctx context.Context, t *testing.T, suite FuzzSuite, statements []string) *dialect.Catalogue {
	t.Helper()
	generator, err := seagull.New(suite.Dialect())
	require.NoError(t, err)
	files := fstest.MapFS{"migrations/001_fuzz.up.sql": &fstest.MapFile{Data: []byte(strings.Join(statements, "\n"))}}
	catalogue, diagnostics, err := generator.Catalogue(ctx, seagull.Input{Migrations: files, MigrationDir: "migrations", Queries: nil, Package: "", PackagePath: "", QueryDir: ""})
	require.NoError(t, err)
	for _, diagnostic := range diagnostics {
		require.NotEqual(t, seagull.SeverityError, diagnostic.Severity, "migration replay: %+v", diagnostic)
	}
	return catalogue
}
