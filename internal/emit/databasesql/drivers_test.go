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

package databasesql_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	postgresSchema   = "CREATE TABLE tagged (id BIGINT PRIMARY KEY, tags TEXT[] NOT NULL);\n"
	clickHouseSchema = "CREATE TABLE events (id UInt64, host String, rank UInt32) ENGINE = MergeTree ORDER BY id;\n"
)

const duckDBVoyageSchema = `CREATE TABLE voyages (
    id INTEGER PRIMARY KEY,
    stops TIMESTAMP[] NOT NULL,
    berth STRUCT(pontoon VARCHAR, arrived TIMESTAMP),
    fees MAP(DATE, DECIMAL(8, 2)),
    log JSON,
    amount DECIMAL(8, 2)
);
`

const clickHouseEnumSchema = `CREATE TABLE items (
    id UInt64,
    mood Enum8('calm' = 1, 'stormy' = 2),
    maybe Nullable(Enum8('calm' = 1, 'stormy' = 2)),
    moods Array(Enum8('calm' = 1, 'stormy' = 2)),
    maybes Array(Nullable(Enum8('calm' = 1, 'stormy' = 2))),
    labels Map(String, Enum8('calm' = 1, 'stormy' = 2)),
    grid Array(Array(Enum8('calm' = 1, 'stormy' = 2)))
) ENGINE = MergeTree ORDER BY id;
`

const duckDBListSchema = "CREATE TYPE mood AS ENUM ('calm', 'stormy');\n" +
	"CREATE TABLE things (id INTEGER PRIMARY KEY, moods mood[] NOT NULL, decimals DECIMAL(10, 2)[] NOT NULL, " +
	"blobs BLOB[] NOT NULL, uuids UUID[] NOT NULL, spans INTERVAL[] NOT NULL, tiny UTINYINT[] NOT NULL, " +
	"nested mood[][] NOT NULL, fixed mood[2] NOT NULL, optional mood[], scores INTEGER[] NOT NULL, " +
	"names VARCHAR[] NOT NULL, stamps TIMESTAMP[] NOT NULL, berth STRUCT(pontoon VARCHAR, number INTEGER), " +
	"fees MAP(VARCHAR, INTEGER), price STRUCT(amount DECIMAL(10, 2), currency VARCHAR), " +
	"rates MAP(VARCHAR, DECIMAL(10, 2)), owners MAP(UUID, INTEGER), visits STRUCT(guest UUID)[], " +
	"feelings MAP(VARCHAR, mood), keyed MAP(mood, INTEGER), entry STRUCT(note VARCHAR, feeling mood), " +
	"history STRUCT(feelings mood[]));\n"

var duckDBListParameters = []struct {
	column  string
	field   string
	wrapped bool
}{
	{column: "moods", field: "Moods", wrapped: true},
	{column: "decimals", field: "Decimals", wrapped: true},
	{column: "blobs", field: "Blobs", wrapped: true},
	{column: "uuids", field: "Uuids", wrapped: true},
	{column: "spans", field: "Spans", wrapped: true},
	{column: "tiny", field: "Tiny", wrapped: true},
	{column: "nested", field: "Nested", wrapped: true},
	{column: "fixed", field: "Fixed", wrapped: true},
	{column: "optional", field: "Optional", wrapped: true},
	{column: "scores", field: "Scores", wrapped: false},
	{column: "names", field: "Names", wrapped: false},
	{column: "stamps", field: "Stamps", wrapped: false},
	{column: "berth", field: "Berth", wrapped: false},
}

const overriddenSchema = "CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL, flag BOOLEAN, rank INTEGER NOT NULL);\n"

const clickHouseDocumentsSchema = `CREATE TABLE documents (
    id UInt64,
    data JSON,
    listed Array(JSON),
    optional_listed Array(Nullable(JSON)),
    optional_keyed Map(String, Nullable(JSON)),
    pair Tuple(doc JSON, n Int64),
    unnamed Tuple(JSON, Int64),
    keyed Map(String, JSON),
    listed_keyed Array(Map(String, JSON)),
    counts Array(UInt32)
) ENGINE = MergeTree ORDER BY id;
`

func generateDatabaseSQL(t *testing.T, engine dialect.DialectPort, schema, queries string) (string, []diag.SourceError) {
	t.Helper()
	generator, err := app.New(engine, app.WithEmitter(databasesql.New()))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      "store",
		Migrations:   fstest.MapFS{"migrations/0001_init.up.sql": {Data: []byte(schema)}, "queries/queries.sql": {Data: []byte(queries)}},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	for _, file := range result.Files {
		if file.Name == "queries.sql.go" {
			return string(file.Content), result.Diagnostics
		}
	}
	return "", result.Diagnostics
}

func TestTheDriverProfileDecidesHowAColumnIsRead(t *testing.T) {
	t.Parallel()

	source, diagnostics := generateDatabaseSQL(t, postgres.New(), postgresSchema, "-- sgl.query(GetTagged, one)\nSELECT id, tags FROM tagged WHERE id = $1;\n")

	require.Empty(t, diagnostics)
	require.Contains(t, source, `to_json(tags) AS "tags"`, "pgx's stdlib cannot scan an array into a slice, so it is read as JSON")
	require.Contains(t, source, "sglScanInto(&row.Tags)")
}

func generateDatabaseSQLFiles(t *testing.T, engine dialect.DialectPort, schema, queries string) (map[string]string, []diag.SourceError) {
	t.Helper()
	generator, err := app.New(engine, app.WithEmitter(databasesql.New()))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      "store",
		Migrations:   fstest.MapFS{"migrations/0001_init.up.sql": {Data: []byte(schema)}, "queries/queries.sql": {Data: []byte(queries)}},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	files := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		files[file.Name] = string(file.Content)
	}
	return files, result.Diagnostics
}

func TestDuckDBReadsCompositesThroughTheScanAdapterAndJSONAsText(t *testing.T) {
	t.Parallel()

	files, diagnostics := generateDatabaseSQLFiles(t, duckdb.New(), duckDBVoyageSchema,
		"-- sgl.query(GetVoyage, one)\nSELECT id, stops, berth, fees, log, amount FROM voyages WHERE id = $1;\n")

	require.Empty(t, diagnostics)
	source := files["queries.sql.go"]
	require.NotContains(t, source, "to_json", "the driver's own list, struct and map values are converted in Go")
	require.Contains(t, source, `SELECT id, stops, berth, fees, CAST(log AS VARCHAR) AS "log", CAST(amount AS VARCHAR) AS "amount" FROM voyages`)
	require.Contains(t, source, ".Scan(&row.ID, sglScanDriverComposite(&row.Stops), sglScanDriverComposite(&row.Berth), sglScanDriverComposite(&row.Fees), &row.Log, &row.Amount)")
	helper := files["driver_composites.go"]
	require.Contains(t, helper, "func sglScanDriverComposite[T any](dest *T) sql.Scanner")
	require.Contains(t, helper, "case duckdb.Interval:")
	require.NotContains(t, helper, "func sglDriverComposite(", "no parameter binds through the conversion")
}

func TestDuckDBReadsTheColumnsOfACompoundQueryThroughTheScanAdapter(t *testing.T) {
	t.Parallel()

	files, diagnostics := generateDatabaseSQLFiles(t, duckdb.New(), duckDBVoyageSchema,
		"-- sgl.query(AllStops, many)\nSELECT stops, log, amount FROM voyages UNION ALL SELECT stops, log, amount FROM voyages;\n")

	require.Empty(t, diagnostics, "a list column of a compound query no longer needs wrapping in to_json")
	require.Contains(t, files["queries.sql.go"], ".Scan(sglScanDriverComposite(&row.Stops), sglScanDriverComposite(&row.Log), sglScanDriverComposite(&row.Amount))",
		"a JSON or decimal column the query cannot cast is converted from the driver's own value")
}

func TestClickHouseBindsEachValueByNameInTheTextTheServerParses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries string
		wants   []string
	}{
		{
			name:    "a scalar",
			queries: "-- sgl.query(GetEvent, one)\nSELECT id FROM events WHERE host = {host:String};\n",
			wants:   []string{`clickhouse.Named("p_host", sglClickHouseShapedValue(host, sglClickHouseShapes[0]))`},
		},
		{
			name:    "a slice binds whole as an array",
			queries: "-- sgl.query(EventsIn, many)\n-- {ids:UInt64} as sgl.param(ids, kind: slice)\nSELECT id FROM events WHERE id IN ({ids:UInt64});\n",
			wants:   []string{"WHERE id IN ({p_ids:Array(UInt64)})", `clickhouse.Named("p_ids", sglClickHouseShapedValue(params.IDs, sglClickHouseShapes[0]))`},
		},
		{
			name:    "a batch names each row's values apart and keeps its expressions",
			queries: "-- sgl.query(InsertEvents, batch)\nINSERT INTO events (id, host, rank) VALUES ({id:UInt64}, lower({host:String}), {rank:UInt32} + 1);\n",
			wants: []string{
				`values.WriteString(":String}), {p_rank_")`,
				`clickhouse.Named("p_id_"+row, sglClickHouseShapedValue(item.ID, sglClickHouseShapes[0]))`,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source, diagnostics := generateDatabaseSQL(t, clickhouse.New(), clickHouseSchema, testCase.queries)
			require.Empty(t, diagnostics)
			for _, want := range testCase.wants {
				require.Contains(t, source, want)
			}
		})
	}
}

func TestClickHouseRefusesACommandWhoseResultTheDriverDoesNotReport(t *testing.T) {
	t.Parallel()

	source, diagnostics := generateDatabaseSQL(t, clickhouse.New(), clickHouseSchema,
		"-- sgl.query(PurgeEvents, execrows)\nALTER TABLE events DELETE WHERE host = {host:String};\n")

	require.Empty(t, source)
	require.Len(t, diagnostics, 1)
	require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
	require.Equal(t, "queries.sql", diagnostics[0].Filename)
	require.Contains(t, diagnostics[0].Message, "command: execrows")
}

func TestClickHouseReadsAnEnumSliceOrMapThroughGoStrings(t *testing.T) {
	t.Parallel()

	source, diagnostics := generateDatabaseSQL(t, clickhouse.New(), clickHouseEnumSchema,
		"-- sgl.query(GetItem, one)\nSELECT id, mood, maybe, moods, maybes, labels FROM items WHERE id = {raw:UInt64};\n")

	require.Empty(t, diagnostics)
	require.Contains(t, source, ".Scan(&row.ID, &row.Mood, &row.Maybe, &raw.Moods, &raw.Maybes, &raw.Labels)",
		"database/sql stores a string into a named string type, but only an identical slice or map type into a slice or map")
	require.Contains(t, source, "row.Moods = sglConvertStrings[ItemsMood](raw.Moods)")
	require.Contains(t, source, "row.Maybes = sglConvertStringPointers[ItemsMood](raw.Maybes)")
	require.Contains(t, source, "row.Labels = sglConvertStringMap[ItemsMood](raw.Labels)")
	require.NotContains(t, source, "raw uint64", "the parameter keeps clear of the scan's raw local")
}

func TestClickHouseRefusesAnEnumItCannotRead(t *testing.T) {
	t.Parallel()

	source, diagnostics := generateDatabaseSQL(t, clickhouse.New(), clickHouseEnumSchema,
		"-- sgl.query(GetGrid, one)\nSELECT grid FROM items WHERE id = {id:UInt64};\n")

	require.Empty(t, source)
	require.Len(t, diagnostics, 1)
	require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
	require.Contains(t, diagnostics[0].Message, "column grid holds the generated enum type ItemsMood nested in a slice or a struct, "+
		"which the github.com/ClickHouse/clickhouse-go/v2 driver behind database/sql cannot read")
}

func TestOnlyADriverReadingEnumsThroughGoStringsReservesTheScanLocal(t *testing.T) {
	t.Parallel()

	source, diagnostics := generateDatabaseSQL(t, postgres.New(), "CREATE TABLE items (id BIGINT PRIMARY KEY);\n",
		"-- sgl.query(GetItem, one)\n-- $1 as sgl.param(raw)\nSELECT id FROM items WHERE id = $1;\n")

	require.Empty(t, diagnostics)
	require.Contains(t, source, "raw int64")
}

func TestPgxStdlibBindsAnEnumArrayAsGoStrings(t *testing.T) {
	t.Parallel()

	schema := "CREATE TYPE mood AS ENUM ('calm', 'stormy');\nCREATE TABLE items (id BIGINT PRIMARY KEY, mood mood, moods mood[]);\n"
	source, diagnostics := generateDatabaseSQL(t, postgres.New(), schema,
		"-- sgl.query(SetMoods, exec)\nUPDATE items SET moods = $1 WHERE id = $2;\n"+
			"-- sgl.query(ItemsWithMoods, many)\n-- $1 as sgl.param(moods, kind: slice)\nSELECT id FROM items WHERE mood IN ($1);\n"+
			"-- sgl.query(GetItem, one)\nSELECT moods FROM items WHERE id = $1;\n"+
			"-- sgl.query(InsertItems, batch)\nINSERT INTO items (id, moods) VALUES ($1, $2);\n")

	require.Empty(t, diagnostics)
	require.Contains(t, source, "sglConvertStringPointers[string](params.Moods), params.ID)",
		"pgx has no encoding for an enum's array type it has not loaded, but sends []*string as text the server parses")
	require.Contains(t, source, "args = append(args, item.ID, sglConvertStringPointers[string](item.Moods))", "a batch row binds it the same way")
	require.Contains(t, source, "args = append(args, v)", "each element of a slice parameter binds as the string it holds")
	require.Contains(t, source, "sglScanInto(&row.Moods)", "the enum array column is still read as JSON")
	require.NotContains(t, source, "var raw struct")
}

func TestAnArrayInAnEmbedIsReadAsJSONLikeAnyOtherColumn(t *testing.T) {
	t.Parallel()

	schema := "CREATE TABLE authors (id BIGINT PRIMARY KEY, name TEXT NOT NULL, tags TEXT[]);\n" +
		"CREATE TABLE books (id BIGINT PRIMARY KEY, author_id BIGINT NOT NULL REFERENCES authors (id), labels TEXT[] NOT NULL);\n"
	testCases := []struct {
		name    string
		queries string
		sql     string
		scan    string
	}{
		{
			name:    "an embed of one row",
			queries: "-- sgl.query(BooksWithAuthors, many)\n-- sgl.embed(authors)\nSELECT books.id, authors.* FROM books JOIN authors ON authors.id = books.author_id;\n",
			sql:     `SELECT books.id, "authors"."id", "authors"."name", to_json("authors"."tags") AS "tags" FROM books`,
			scan:    "rows.Scan(&row.ID, &row.Authors.ID, &row.Authors.Name, sglScanInto(&row.Authors.Tags))",
		},
		{
			name: "a grouped query",
			queries: "-- sgl.query(name: AuthorsWithBooks, command: many, group_by: authors.id)\n-- sgl.embed(books)\n" +
				"SELECT authors.id, authors.tags, books.* FROM authors JOIN books ON books.author_id = authors.id;\n",
			sql:  `SELECT authors.id, to_json(authors.tags) AS "tags", "books"."id", "books"."author_id", to_json("books"."labels") AS "labels" FROM authors`,
			scan: "rows.Scan(&colID, sglScanInto(&colTags), &booksID, &booksAuthorID, sglScanInto(&booksLabels))",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source, diagnostics := generateDatabaseSQL(t, postgres.New(), schema, testCase.queries)

			require.Empty(t, diagnostics)
			require.Contains(t, source, testCase.sql)
			require.Contains(t, source, testCase.scan)
		})
	}
}

func TestPgxStdlibRefusesAMultiDimensionalEnumArrayItCannotBind(t *testing.T) {
	t.Parallel()

	schema := "CREATE TYPE mood AS ENUM ('calm', 'stormy');\nCREATE TABLE items (id BIGINT PRIMARY KEY, moods mood[], grid mood[][]);\n"
	source, diagnostics := generateDatabaseSQL(t, postgres.New(), schema,
		"-- sgl.query(SetGrid, exec)\nUPDATE items SET grid = $1 WHERE id = $2;\n")

	require.Empty(t, source)
	require.Len(t, diagnostics, 1)
	require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
	require.Contains(t, diagnostics[0].Message, "parameter grid holds the generated enum type Mood nested in a slice or a struct, "+
		"which the github.com/jackc/pgx/v5 driver behind database/sql cannot bind")
}

func TestDuckDBBindsAListOfElementsItCannotBuildByTheTypesOfItsElements(t *testing.T) {
	t.Parallel()

	var queries strings.Builder
	for _, parameter := range duckDBListParameters {
		queries.WriteString("-- sgl.query(Set" + parameter.field + ", exec)\nUPDATE things SET " + parameter.column + " = $1 WHERE id = $2;\n")
	}
	source, diagnostics := generateDatabaseSQL(t, duckdb.New(), duckDBListSchema, queries.String())
	require.Empty(t, diagnostics)

	for _, parameter := range duckDBListParameters {
		t.Run(parameter.column, func(t *testing.T) {
			t.Parallel()

			wrapped := "sglDriverInferredList(params." + parameter.field + ")"
			if !parameter.wrapped {
				require.NotContains(t, source, wrapped)
				return
			}
			require.Contains(t, source, wrapped, "the driver builds a list parameter's elements from the parameter's type, "+
				"which fails for these, but binds a list a driver.Valuer returns by the types of its elements")
		})
	}
}

func TestDuckDBRefusesAMapOrStructParameterHoldingAnElementItCannotBuild(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		column  string
		element string
	}{
		{column: "price", element: "numeric"},
		{column: "rates", element: "numeric"},
		{column: "owners", element: "uuid"},
		{column: "visits", element: "uuid"},
		{column: "feelings", element: "mood"},
		{column: "keyed", element: "mood"},
		{column: "entry", element: "mood"},
		{column: "history", element: "mood"},
		{column: "fees"},
		{column: "berth"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.column, func(t *testing.T) {
			t.Parallel()

			source, diagnostics := generateDatabaseSQL(t, duckdb.New(), duckDBListSchema,
				"-- sgl.query(SetValue, exec)\nUPDATE things SET "+testCase.column+" = $1 WHERE id = $2;\n")

			if testCase.element == "" {
				require.Empty(t, diagnostics)
				require.NotEmpty(t, source)
				return
			}
			require.Empty(t, source)
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
			require.Equal(t, "parameter "+testCase.column+" holds a value of type "+testCase.element+" inside a map or a struct, "+
				"which the github.com/duckdb/duckdb-go/v2 driver behind database/sql cannot bind; build the struct in the SQL from "+
				"a parameter for each field, as {'name': $1, 'kind': $2}, or the map from a list of keys and a list of values, "+
				"as map($1, $2)", diagnostics[0].Message)
		})
	}
}

func TestOtherDriversBindAnArrayOfEnumsWithoutTheInferredList(t *testing.T) {
	t.Parallel()

	schema := "CREATE TYPE mood AS ENUM ('calm', 'stormy');\nCREATE TABLE items (id BIGINT PRIMARY KEY, moods mood[] NOT NULL, " +
		"prices NUMERIC[] NOT NULL);\n"
	source, diagnostics := generateDatabaseSQL(t, postgres.New(), schema,
		"-- sgl.query(SetMoods, exec)\nUPDATE items SET moods = $1, prices = $2 WHERE id = $3;\n")

	require.Empty(t, diagnostics)
	require.NotContains(t, source, "sglDriverInferredList")
}

func TestClickHouseReadsANullableAddressAsAPointerThatTakesNULL(t *testing.T) {
	t.Parallel()

	slots := databasesql.New().DefaultTypes(new(dialect.Wrap(clickhouse.New()).Info()))

	require.Equal(t, sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: "net", Name: "IP"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: "net", Name: "*IP"},
	}, slots.IPAddress)
}

func generateOverridden(t *testing.T, engine dialect.DialectPort, schema, queries string, overrides ...app.TypeOverride) map[string]string {
	t.Helper()
	generator, err := app.New(engine, app.WithEmitter(databasesql.New()), app.WithTypeOverrides(overrides...))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      "store",
		Migrations:   fstest.MapFS{"migrations/0001_init.up.sql": {Data: []byte(schema)}, "queries/queries.sql": {Data: []byte(queries)}},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	require.Empty(t, result.Diagnostics)
	files := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		files[file.Name] = string(file.Content)
	}
	return files
}

func generateOverriddenWithDiagnostics(t *testing.T, engine dialect.DialectPort, schema, queries string) map[string]string {
	t.Helper()
	generator, err := app.New(engine, app.WithEmitter(databasesql.New()))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      "store",
		Migrations:   fstest.MapFS{"migrations/0001_init.up.sql": {Data: []byte(schema)}, "queries/queries.sql": {Data: []byte(queries)}},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	for _, diagnostic := range result.Diagnostics {
		require.NotEqual(t, diag.SeverityError, diagnostic.Severity, diagnostic.Message)
	}
	files := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		files[file.Name] = string(file.Content)
	}
	return files
}

func TestAColumnWhoseGoTypeAnOverrideChoseReadsThroughTheOverrideScanAdapter(t *testing.T) {
	t.Parallel()

	overrides := []app.TypeOverride{
		{Column: "notes.body", GoPackage: "encoding/json", GoName: "RawMessage"},
		{Column: "notes.flag", GoPackage: "example.com/kinds", GoName: "Flag"},
		{Column: "notes.rank", GoName: "int32"},
	}
	testCases := []struct {
		engine      dialect.DialectPort
		name        string
		placeholder string
	}{
		{name: "postgres", engine: postgres.New(), placeholder: "$1"},
		{name: "mysql", engine: mysql.New(), placeholder: "?"},
		{name: "sqlite", engine: sqlite.New(), placeholder: "?"},
		{name: "duckdb", engine: duckdb.New(), placeholder: "$1"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := generateOverridden(t, testCase.engine, overriddenSchema,
				"-- sgl.query(GetNote, one)\nSELECT id, body, flag, rank FROM notes WHERE id = "+testCase.placeholder+";\n", overrides...)

			require.Contains(t, files["queries.sql.go"], ".Scan(&row.ID, sglScanOverride(&row.Body), sglScanOverride(&row.Flag), &row.Rank)",
				"a column of one of Go's own types is scanned as it is")
			helper := files["driver_composites.go"]
			require.Contains(t, helper, "func sglScanOverride[T any](dest *T) sql.Scanner")
			require.NotContains(t, helper, "func sglScanDriverComposite[", "no column reads the driver's own composites")
			require.NotContains(t, helper, "func sglDriverComposite(", "no parameter binds through the conversion")
		})
	}
}

func TestAJSONColumnWhoseGoTypeAnOverrideChoseDecodesItsDocument(t *testing.T) {
	t.Parallel()

	overrides := []app.TypeOverride{
		{Column: "shows.data", GoPackage: "example.com/shows", GoName: "Show"},
		{Column: "shows.tags", GoName: "[]string"},
		{Column: "shows.raw", GoName: "string"},
	}
	testCases := []struct {
		engine      dialect.DialectPort
		name        string
		schema      string
		placeholder string
		bind        string
	}{
		{name: "postgres", engine: postgres.New(), placeholder: "$1",
			schema: "CREATE TABLE shows (id BIGINT PRIMARY KEY, data JSONB NOT NULL, tags JSONB, raw JSONB NOT NULL);\n",
			bind:   "setShowSQL, params.ID, sglDriverJSON{value: params.Data}, sglDriverJSON{value: params.Tags}, params.Raw)"},
		{name: "mysql", engine: mysql.New(), placeholder: "?",
			schema: "CREATE TABLE shows (id BIGINT PRIMARY KEY, data JSON NOT NULL, tags JSON, raw JSON NOT NULL);\n",
			bind:   "setShowSQL, sglDriverJSON{value: params.Data}, sglDriverJSON{value: params.Tags}, params.Raw, params.ID)"},
		{name: "sqlite", engine: sqlite.New(), placeholder: "?",
			schema: "CREATE TABLE shows (id INTEGER PRIMARY KEY, data JSON NOT NULL, tags JSON, raw JSON NOT NULL);\n",
			bind:   "setShowSQL, sglDriverJSON{value: params.Data}, sglDriverJSON{value: params.Tags}, params.Raw, params.ID)"},
		{name: "duckdb", engine: duckdb.New(), placeholder: "$1",
			schema: "CREATE TABLE shows (id BIGINT PRIMARY KEY, data JSON NOT NULL, tags JSON, raw JSON NOT NULL);\n",
			bind:   "setShowSQL, params.ID, sglDriverJSON{value: params.Data}, sglDriverJSON{value: params.Tags},"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			update := "UPDATE shows SET data = $2, tags = $3, raw = $4 WHERE id = $1;"
			if testCase.placeholder == "?" {
				update = "UPDATE shows SET data = ?, tags = ?, raw = ? WHERE id = ?;"
			}
			files := generateOverridden(t, testCase.engine, testCase.schema,
				"-- sgl.query(GetShow, one)\nSELECT id, data, tags, raw FROM shows WHERE id = "+testCase.placeholder+";\n"+
					"-- sgl.query(SetShow, exec)\n"+update+"\n", overrides...)

			queries := files["queries.sql.go"]
			require.Contains(t, queries, ".Scan(&row.ID, sglScanOverrideJSON(&row.Data), sglScanOverrideJSON(&row.Tags), &row.Raw)",
				"a JSON document reads into a plain struct or slice as pgx reads it, and into a string as it is")
			require.Contains(t, queries, testCase.bind)
			require.Contains(t, files["driver_composites.go"], "func sglScanOverrideJSON[T any](dest *T) sql.Scanner")
			require.NotContains(t, files["driver_composites.go"], "func sglScanOverride[T any](dest *T) sql.Scanner",
				"no column reads through the plain override adapter")
			require.Contains(t, files["driver_values.go"], "func (wrapped sglDriverJSON) Value() (driver.Value, error)")
		})
	}
}

func TestAResultTypedAnyWhoseGoTypeAnOverrideChoseReadsThroughTheOverrideScanAdapter(t *testing.T) {
	t.Parallel()

	files := generateOverriddenWithDiagnostics(t, sqlite.New(), "CREATE TABLE test (id INTEGER PRIMARY KEY, data BLOB);\n",
		"-- sgl.query(GetData, many)\n-- sgl.column(doc, go_type: \"encoding/json.RawMessage\")\nSELECT json_extract(data, '$') AS doc FROM test;\n")

	require.Contains(t, files["queries.sql.go"], ".Scan(sglScanOverride(&row.Doc))")
	require.Contains(t, files["driver_composites.go"], "func sglScanOverride[T any](dest *T) sql.Scanner")
}

func TestAColumnDatabaseSQLConvertsItselfNeedsNoOverrideScanAdapter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		overrides []app.TypeOverride
	}{
		{name: "no override", overrides: nil},
		{name: "a defined integer type, which database/sql converts an integer into", overrides: []app.TypeOverride{
			{Column: "notes.rank", GoPackage: "example.com/kinds", GoName: "Rank"},
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := generateOverridden(t, postgres.New(), overriddenSchema, "-- sgl.query(GetNote, one)\nSELECT id, body, rank FROM notes WHERE id = $1;\n",
				testCase.overrides...)

			require.Contains(t, files["queries.sql.go"], ".Scan(&row.ID, &row.Body, &row.Rank)")
			require.NotContains(t, files, "driver_composites.go")
		})
	}
}

func TestAnOverriddenParameterBindsAsTheDriverTakesIt(t *testing.T) {
	t.Parallel()

	overrides := []app.TypeOverride{
		{Column: "notes.body", GoPackage: "encoding/json", GoName: "RawMessage"},
		{Column: "notes.flag", GoPackage: "example.com/kinds", GoName: "Flag"},
		{Column: "notes.rank", GoPackage: "example.com/kinds", GoName: "Rank"},
	}
	testCases := []struct {
		engine dialect.DialectPort
		name   string
		where  string
		want   string
	}{
		{name: "sqlite binds bytes for text as text", engine: sqlite.New(), where: "body = ? AND flag = ? AND rank = ?",
			want: "findNotesSQL, sglDriverText{value: params.Body}, params.Flag, params.Rank)"},
		{name: "duckdb binds bytes for text as text and converts a defined type it refuses", engine: duckdb.New(),
			where: "body = $1 AND flag = $2 AND rank = $3",
			want:  "findNotesSQL, sglDriverText{value: params.Body}, sglDriverComposite(params.Flag), params.Rank)"},
		{name: "postgres binds through database/sql as it is", engine: postgres.New(), where: "body = $1 AND flag = $2 AND rank = $3",
			want: "findNotesSQL, params.Body, params.Flag, params.Rank)"},
		{name: "mysql binds through database/sql as it is", engine: mysql.New(), where: "body = ? AND flag = ? AND rank = ?",
			want: "findNotesSQL, params.Body, params.Flag, params.Rank)"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := generateOverridden(t, testCase.engine, overriddenSchema,
				"-- sgl.query(FindNotes, many)\nSELECT id FROM notes WHERE "+testCase.where+";\n", overrides...)

			require.Contains(t, files["queries.sql.go"], testCase.want)
		})
	}
}

func TestClickHouseReadsAColumnHoldingJSONThroughTheScanAdapter(t *testing.T) {
	t.Parallel()

	files, diagnostics := generateDatabaseSQLFiles(t, clickhouse.New(), clickHouseDocumentsSchema, "-- sgl.query(GetDocument, one)\n"+
		"SELECT id, data, listed, optional_listed, optional_keyed, pair, unnamed, counts FROM documents WHERE id = {id:UInt64};\n"+
		"-- sgl.query(ListDocuments, many)\n"+
		"SELECT listed FROM documents UNION ALL SELECT listed FROM documents;\n")

	require.Empty(t, diagnostics)
	require.Contains(t, files["queries.sql.go"], ".Scan(&row.ID, &row.Data, sglScanDriverComposite(&row.Listed), "+
		"sglScanDriverComposite(&row.OptionalListed), sglScanDriverComposite(&row.OptionalKeyed), sglScanDriverComposite(&row.Pair), "+
		"sglScanDriverComposite(&row.Unnamed), &row.Counts)",
		"database/sql reads a JSON column through the JSON type's Scan, and a composite holding one through the adapter")
	require.Contains(t, files["queries.sql.go"], ".Scan(sglScanDriverComposite(&row.Listed))")
	helper := files["driver_composites.go"]
	require.Contains(t, helper, "func sglScanDriverComposite[T any](dest *T) sql.Scanner")
	require.Contains(t, helper, "sglReadClickHouseDocuments(target, source)")
	require.NotContains(t, helper, "github.com/duckdb", "the DuckDB driver's own types are left out")
	require.Contains(t, files["clickhouse_documents.go"], "case *chcol.JSON:")
}

func TestClickHouseReadsAJSONColumnAsItIsWithoutTheAdapter(t *testing.T) {
	t.Parallel()

	files, diagnostics := generateDatabaseSQLFiles(t, clickhouse.New(), clickHouseDocumentsSchema,
		"-- sgl.query(GetDocument, one)\nSELECT id, data FROM documents WHERE id = {id:UInt64};\n")

	require.Empty(t, diagnostics)
	require.Contains(t, files["queries.sql.go"], ".Scan(&row.ID, &row.Data)")
	require.NotContains(t, files, "driver_composites.go")
	require.NotContains(t, files, "clickhouse_documents.go")
}

func TestClickHouseRefusesAMapOfJSONTheDriverPanicsReading(t *testing.T) {
	t.Parallel()

	for _, column := range []string{"keyed", "listed_keyed"} {
		t.Run(column, func(t *testing.T) {
			t.Parallel()

			_, diagnostics := generateDatabaseSQLFiles(t, clickhouse.New(), clickHouseDocumentsSchema,
				"-- sgl.query(GetKeyed, one)\nSELECT "+column+" FROM documents WHERE id = {id:UInt64};\n")

			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Contains(t, diagnostics[0].Message, `column "`+column+`" holds a Map whose values are JSON documents, which the `+
				`github.com/ClickHouse/clickhouse-go/v2 driver behind database/sql panics reading`)
		})
	}
}

func TestAClickHouseRuntimeBuilderBindsANilPointerAsNull(t *testing.T) {
	t.Parallel()

	files, diagnostics := generateDatabaseSQLFiles(t, clickhouse.New(),
		"CREATE TABLE prices (id UInt64, amount Decimal(10, 2), discount Nullable(Decimal(10, 2)), owner Nullable(UUID)) "+
			"ENGINE = MergeTree ORDER BY id;\n",
		"-- sgl.query(name: BrowsePrices, command: many, dynamic: runtime)\n"+
			"SELECT id, amount FROM prices WHERE discount >= {discount:Nullable(Decimal(10, 2))} AND amount >= {amount:Decimal(10, 2)} "+
			"AND owner = {owner:Nullable(UUID)}\n")

	require.Len(t, diagnostics, 1)
	require.Contains(t, files["queries.sql.go"], "args = append(args, sglClickHouseNullable(builder.params.Discount))")
	require.Contains(t, files["queries.sql.go"], "args = append(args, builder.params.Amount)")
	require.Contains(t, files["queries.sql.go"], "args = append(args, sglClickHouseNullable(builder.params.Owner))")
	require.Contains(t, files["clickhouse_values.go"], "func sglClickHouseNullable(value any) any")
}
