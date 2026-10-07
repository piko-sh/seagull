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

package clickhouse_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	dialect_clickhouse "seagull.sh/seagull/internal/dialect/clickhouse"
	emit_clickhouse "seagull.sh/seagull/internal/emit/clickhouse"
)

const enumSchema = `CREATE TABLE items (
    id UInt64,
    mood Enum8('calm' = 1, 'stormy' = 2),
    maybe Nullable(Enum8('calm' = 1, 'stormy' = 2)),
    moods Array(Enum8('calm' = 1, 'stormy' = 2))
) ENGINE = MergeTree ORDER BY id;
`

const shapesSchema = `CREATE TABLE shapes (
    id UInt64,
    maybes Array(Nullable(Enum8('calm' = 1, 'stormy' = 2))),
    labels Map(String, Enum8('calm' = 1, 'stormy' = 2)),
    keyed Map(Enum8('calm' = 1, 'stormy' = 2), String),
    grid Array(Array(Enum8('calm' = 1, 'stormy' = 2))),
    pair Tuple(first Enum8('calm' = 1, 'stormy' = 2), label String)
) ENGINE = MergeTree ORDER BY id;
`

const pricesSchema = `CREATE TABLE prices (
    id UInt64,
    amount Decimal(10, 2),
    discount Nullable(Decimal(10, 2)),
    owner Nullable(UUID),
    note Nullable(String)
) ENGINE = MergeTree ORDER BY id;
`

const (
	namedSchema = `CREATE TABLE events (
    id UInt64,
    host String,
    note Nullable(String),
    day Date,
    in_stock Bool
) ENGINE = MergeTree ORDER BY id;
`
	namedQueries = `-- sgl.query(InsertEvent, exec)
INSERT INTO events (id, host, note, day, in_stock) VALUES ({id:UInt64}, {host:String}, {note:Nullable(String)}, {day:Date}, {in_stock:Bool});
-- sgl.query(PageEvents, many)
SELECT id, host FROM events ORDER BY id LIMIT {limit:UInt32} OFFSET {offset:UInt32};
-- sgl.query(EventsByIDs, many)
SELECT id, host FROM events WHERE id IN {ids:Array(UInt64)} ORDER BY id;
-- sgl.query(EventByHost, one)
SELECT id FROM events WHERE host = {host:String} OR note = {host:String} LIMIT 1;
-- sgl.query(InsertEvents, batch)
INSERT INTO events (id, host, note, day, in_stock) VALUES ({id:UInt64}, {host:String}, {note:Nullable(String)}, {day:Date}, {in_stock:Bool});
`
	importClickHouse = `"github.com/ClickHouse/clickhouse-go/v2"`
)

var (
	namedValuePattern = regexp.MustCompile(`clickhouse\.Named\("([^"]+)"`)

	bracedPlaceholderPattern = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*):`)
)

func TestNativeDriverScansAnEnumColumnThroughGoStrings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		command string
	}{
		{name: "one row", command: "one"},
		{name: "many rows", command: "many"},
		{name: "a stream of rows", command: "stream"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := generateNativeFrom(t, enumSchema, "-- sgl.query(GetItems, "+testCase.command+")\n"+
				"SELECT id, mood, maybe, moods FROM items WHERE id = {id:UInt64};\n")
			require.Contains(t, files["models.go"], "type ItemsMood string")
			method := methodSource(t, files["events.sql.go"], "GetItems")

			require.Contains(t, method, ".Scan(&row.ID, (*string)(&row.Mood), &raw.Maybe, &row.Moods)")
			require.Contains(t, method, "row.Maybe = (*ItemsMood)(raw.Maybe)")
			require.Less(t, strings.Index(method, "var raw struct"), strings.Index(method, ".Scan("))
			require.Less(t, strings.Index(method, ".Scan("), strings.Index(method, "row.Maybe = (*ItemsMood)(raw.Maybe)"))
			require.NotContains(t, method, "raw.Moods", "clickhouse-go fills a slice of a named string type itself")
		})
	}
}

func TestNativeDriverReadsEveryEnumColumnOfARowTheSameWay(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, enumSchema, "-- sgl.query(GetItem, one)\n"+
		"SELECT id, maybe, maybe AS again FROM items WHERE id = {id:UInt64};\n")
	method := methodSource(t, files["events.sql.go"], "GetItem")

	require.Contains(t, method, ".Scan(&row.ID, &raw.Maybe, &raw.Again)")
	require.Contains(t, method, "row.Maybe = (*ItemsMood)(raw.Maybe)")
	require.Contains(t, method, "row.Again = (*ItemsMood)(raw.Again)")
}

func TestNativeDriverKeepsAParameterClearOfTheScanLocal(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, enumSchema, "-- sgl.query(GetItem, one)\n"+
		"SELECT maybe FROM items WHERE id = {raw:UInt64};\n")
	method := methodSource(t, files["events.sql.go"], "GetItem")

	require.NotContains(t, method, "raw uint64")
	require.Contains(t, method, "var raw struct")
}

func TestNativeDriverBindsAnEnumParameterAsItIs(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, enumSchema, "-- sgl.query(InsertItem, exec)\n"+
		"INSERT INTO items (id, mood, maybe, moods) VALUES ({id:UInt64}, {mood:Enum8('calm' = 1, 'stormy' = 2)}, "+
		"{maybe:Nullable(Enum8('calm' = 1, 'stormy' = 2))}, {moods:Array(Enum8('calm' = 1, 'stormy' = 2))});\n")
	method := methodSource(t, files["events.sql.go"], "InsertItem")

	require.Contains(t, method, `clickhouse.Named("p_mood", sglClickHouseShapedValue(params.Mood, sglClickHouseShapes[1]))`)
	require.Contains(t, method, `clickhouse.Named("p_maybe", sglClickHouseShapedValue(params.Maybe, sglClickHouseShapes[1]))`)
	require.Contains(t, method, `clickhouse.Named("p_moods", sglClickHouseShapedValue(params.Moods, sglClickHouseShapes[2]))`)
	require.NotContains(t, files, "convert_strings.go")
}

func generateNativeDiagnostics(t *testing.T, schema, queries string) (map[string]string, []diag.SourceError) {
	t.Helper()
	generator, err := app.New(dialect_clickhouse.New(), app.WithEmitter(emit_clickhouse.New()))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      "store",
		Migrations:   fstest.MapFS{"migrations/0001_init.up.sql": {Data: []byte(schema)}, "queries/events.sql": {Data: []byte(queries)}},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	generated := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		generated[file.Name] = string(file.Content)
	}
	return generated, result.Diagnostics
}

func TestNativeDriverScansAMapOfEnumValuesThroughGoStrings(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, shapesSchema, "-- sgl.query(GetShape, one)\n"+
		"SELECT id, maybes, labels, grid, pair FROM shapes WHERE id = {id:UInt64};\n")
	method := methodSource(t, files["events.sql.go"], "GetShape")

	require.Contains(t, method, ".Scan(&row.ID, &row.Maybes, &raw.Labels, &row.Grid, &row.Pair)",
		"clickhouse-go fills a slice of nullable enums, a slice of slices and a struct itself")
	require.Contains(t, method, "Labels map[string]string")
	require.Contains(t, method, "row.Labels = sglConvertStringMap[ShapesMaybesItem](raw.Labels)")
	require.Contains(t, files["convert_strings.go"], "func sglConvertStringMap[")
	require.NotContains(t, files["convert_strings.go"], "func sglConvertStrings[")
}

func TestNativeDriverRefusesAnEnumItCannotReadOrAppend(t *testing.T) {
	t.Parallel()

	enum := "Enum8('calm' = 1, 'stormy' = 2)"
	testCases := []struct {
		name    string
		queries string
		want    string
	}{
		{
			name:    "a map keyed by an enum read",
			queries: "-- sgl.query(GetKeyed, one)\nSELECT keyed FROM shapes WHERE id = {id:UInt64};\n",
			want: "column keyed holds the generated enum type ShapesMaybesItem inside a map, as a key or within a value, which " +
				"the native ClickHouse driver cannot read",
		},
		{
			name:    "a slice of slices appended to a batch",
			queries: "-- sgl.query(InsertGrids, batch)\nINSERT INTO shapes (id, grid) VALUES ({id:UInt64}, {grid:Array(Array(" + enum + "))});\n",
			want: "parameter grid holds the generated enum type ShapesMaybesItem nested in a slice or a struct, which the native " +
				"ClickHouse driver cannot bind",
		},
		{
			name:    "a struct appended to a batch",
			queries: "-- sgl.query(CopyPairs, copyfrom)\nINSERT INTO shapes (id, pair) VALUES ({id:UInt64}, {pair:Tuple(first " + enum + ", label String)});\n",
			want: "parameter pair holds the generated enum type ShapesMaybesItem nested in a slice or a struct, which the native " +
				"ClickHouse driver cannot bind",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := generateNativeDiagnostics(t, shapesSchema, testCase.queries)

			require.Empty(t, files)
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Contains(t, diagnostics[0].Message, testCase.want)
		})
	}
}

func TestNativeDriverBindsANestedEnumByNameAsText(t *testing.T) {
	t.Parallel()

	enum := "Enum8('calm' = 1, 'stormy' = 2)"
	files, diagnostics := generateNativeDiagnostics(t, shapesSchema, "-- sgl.query(InsertShape, exec)\n"+
		"INSERT INTO shapes (id, keyed, grid) VALUES ({id:UInt64}, {keyed:Map("+enum+", String)}, {grid:Array(Array("+enum+"))});\n"+
		"-- sgl.query(InsertShapes, batch)\n"+
		"INSERT INTO shapes (id, grid) VALUES ({id:UInt64} + 1, {grid:Array(Array("+enum+"))});\n")

	require.Empty(t, diagnostics)
	require.Contains(t, methodSource(t, files["events.sql.go"], "InsertShape"), `clickhouse.Named("p_grid", sglClickHouseShapedValue(params.Grid, sglClickHouseShapes[2]))`)
	require.Contains(t, methodSource(t, files["events.sql.go"], "InsertShapes"), "sglClickHouseShapedValue(item.Grid")
}

func TestNativeRuntimeBuilderBindsANilPointerAsNull(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		query   string
		bound   string
		wrapped bool
	}{
		{
			name:    "a nullable decimal goes through the helper",
			query:   "SELECT id, amount FROM prices WHERE discount >= {discount:Nullable(Decimal(10, 2))}",
			bound:   "sglClickHouseNullable(builder.params)",
			wrapped: true,
		},
		{
			name:    "a nullable UUID goes through the helper",
			query:   "SELECT id, amount FROM prices WHERE owner = {owner:Nullable(UUID)}",
			bound:   "sglClickHouseNullable(builder.params)",
			wrapped: true,
		},
		{
			name:    "a nullable string goes through the helper",
			query:   "SELECT id, amount FROM prices WHERE note = {note:Nullable(String)}",
			bound:   "sglClickHouseNullable(builder.params)",
			wrapped: true,
		},
		{
			name:    "a decimal that cannot be NULL binds as it is",
			query:   "SELECT id, amount FROM prices WHERE amount >= {amount:Decimal(10, 2)}",
			bound:   "args = append(args, builder.params)",
			wrapped: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := generateNativeFrom(t, pricesSchema, "-- sgl.query(name: BrowsePrices, command: many, dynamic: runtime)\n"+testCase.query+"\n")
			require.Contains(t, files["events.sql.go"], testCase.bound)
			if testCase.wrapped {
				require.Contains(t, files["clickhouse_values.go"], "func sglClickHouseNullable(value any) any")
				return
			}
			require.NotContains(t, files["events.sql.go"], "sglClickHouseNullable")
			require.NotContains(t, files["clickhouse_values.go"], "sglClickHouseNullable")
		})
	}
}

func generateNative(t *testing.T, queries string) map[string]string {
	t.Helper()
	return generateNativeFrom(t, namedSchema, queries)
}

func generateNativeFrom(t *testing.T, schema, queries string) map[string]string {
	t.Helper()
	generator, err := app.New(dialect_clickhouse.New(), app.WithEmitter(emit_clickhouse.New()))
	require.NoError(t, err)

	files := fstest.MapFS{
		"migrations/0001_init.up.sql": {Data: []byte(schema)},
		"queries/events.sql":          {Data: []byte(queries)},
	}
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      "store",
		Migrations:   files,
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)

	generated := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		generated[file.Name] = string(file.Content)
	}
	return generated
}

func methodSource(t *testing.T, source, method string) string {
	t.Helper()
	start := strings.Index(source, ") "+method+"(")
	require.GreaterOrEqualf(t, start, 0, "method %s not found in:\n%s", method, source)
	end := strings.Index(source[start:], "\n}\n")
	require.GreaterOrEqual(t, end, 0)
	return source[start : start+end]
}

func uniqueSorted(values []string) []string {
	slices.Sort(values)
	return slices.Compact(values)
}

func submatches(pattern *regexp.Regexp, source string) []string {
	matches := pattern.FindAllStringSubmatch(source, -1)
	found := make([]string, 0, len(matches))
	for _, match := range matches {
		found = append(found, match[1])
	}
	return uniqueSorted(found)
}

func TestNativeEmitterBindsEveryParameterAsAPrefixedNamedValue(t *testing.T) {
	source := generateNative(t, namedQueries)["events.sql.go"]

	require.Contains(t, source, importClickHouse)
	require.Contains(t, source,
		"VALUES ({p_id:UInt64}, {p_host:String}, {p_note:Nullable(String)}, {p_day:Date}, {p_in_stock:Bool})")
	require.Contains(t, methodSource(t, source, "InsertEvent"),
		`return queries.db.Exec(ctx, insertEventSQL, clickhouse.Named("p_id", sglClickHouseShapedValue(params.ID, sglClickHouseShapes[2])), `+
			`clickhouse.Named("p_host", sglClickHouseShapedValue(params.Host, sglClickHouseShapes[0])), clickhouse.Named("p_note", sglClickHouseShapedValue(params.Note, sglClickHouseShapes[0])), `+
			`clickhouse.Named("p_day", sglClickHouseShapedValue(params.Day, sglClickHouseShapes[3])), clickhouse.Named("p_in_stock", sglClickHouseShapedValue(params.InStock, sglClickHouseShapes[2])))`)
}

func TestNativeEmitterPrefixesReservedKeywordParameters(t *testing.T) {
	source := generateNative(t, namedQueries)["events.sql.go"]

	require.Contains(t, source, "LIMIT {p_limit:UInt32} OFFSET {p_offset:UInt32}")
	require.NotContains(t, source, "{limit:")
	require.NotContains(t, source, "{offset:")
	require.Contains(t, methodSource(t, source, "PageEvents"),
		`queries.db.Query(ctx, pageEventsSQL, clickhouse.Named("p_limit", sglClickHouseShapedValue(params.Limit, sglClickHouseShapes[2])), `+
			`clickhouse.Named("p_offset", sglClickHouseShapedValue(params.Offset, sglClickHouseShapes[2])))`)
}

func TestNativeEmitterBindsAnArrayParameterAsOneNamedValue(t *testing.T) {
	source := generateNative(t, namedQueries)["events.sql.go"]

	require.Contains(t, source, "WHERE id IN {p_ids:Array(UInt64)}")
	require.Contains(t, methodSource(t, source, "EventsByIDs"),
		`queries.db.Query(ctx, eventsByIDsSQL, clickhouse.Named("p_ids", sglClickHouseShapedValue(ids, sglClickHouseShapes[1])))`)
}

func TestNativeEmitterBindsARepeatedPlaceholderOnce(t *testing.T) {
	source := generateNative(t, namedQueries)["events.sql.go"]
	method := methodSource(t, source, "EventByHost")

	require.Contains(t, source, "WHERE host = {p_host:String} OR note = {p_host:String}")
	require.Equal(t, 1, strings.Count(method, `clickhouse.Named("p_host", sglClickHouseShapedValue(host, sglClickHouseShapes[0]))`))
}

func TestNativeEmitterNamesMatchTheirPlaceholders(t *testing.T) {
	source := generateNative(t, namedQueries)["events.sql.go"]

	for _, method := range []string{"InsertEvent", "PageEvents", "EventsByIDs", "EventByHost"} {
		t.Run(method, func(t *testing.T) {
			named := submatches(namedValuePattern, methodSource(t, source, method))
			require.NotEmpty(t, named)
			for _, name := range named {
				require.True(t, strings.HasPrefix(name, "p_"), "bound name %q must carry the wire prefix", name)
				require.Contains(t, source, "{"+name+":", "bound name %q has no matching placeholder", name)
			}
		})
	}

	for _, placeholder := range submatches(bracedPlaceholderPattern, source) {
		require.True(t, strings.HasPrefix(placeholder, "p_"), "placeholder %q must carry the wire prefix", placeholder)
	}
}

func TestNativeEmitterKeepsTheColumnarBatchTyped(t *testing.T) {
	source := generateNative(t, namedQueries)["events.sql.go"]
	method := methodSource(t, source, "InsertEvents")

	require.Contains(t, method, "batch, err := queries.db.PrepareBatch(ctx, insertEventsSQL)")
	require.Contains(t, method, "batch.Append(item.ID, item.Host, item.Note, item.Day, item.InStock)")
	require.NotContains(t, method, "clickhouse.Named")
}

func TestNativeEmitterWritesTheParameterHelper(t *testing.T) {
	files := generateNative(t, namedQueries)

	helper, ok := files["clickhouse_values.go"]
	require.True(t, ok, "the native emitter must write the parameter helper when a query binds parameters")
	require.Contains(t, helper, "func sglClickHouseValue(value any, shape string) string")
}

func TestNativeEmitterOmitsTheParameterHelperWithoutParameters(t *testing.T) {
	files := generateNative(t, "-- sgl.query(CountEvents, one)\nSELECT count() AS total FROM events;\n")

	_, ok := files["clickhouse_values.go"]
	require.False(t, ok, "a package that binds no parameters needs no parameter helper")
}
