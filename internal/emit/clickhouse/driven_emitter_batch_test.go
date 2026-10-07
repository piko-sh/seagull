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
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	dialect_clickhouse "seagull.sh/seagull/internal/dialect/clickhouse"
	emit_clickhouse "seagull.sh/seagull/internal/emit/clickhouse"
)

const appendedMapsSchema = `CREATE TABLE hosts (
    id UInt64,
    keyed Map(String, IPv4),
    optional Map(String, Nullable(IPv6)),
    listed Map(String, Array(IPv4)),
    maps Array(Map(String, IPv4)),
    holder Tuple(a UInt8, m Map(String, IPv4)),
    pairs Map(String, Tuple(x UInt8, y String)),
    wide Map(String, Int128),
    unnamed Map(String, Tuple(UInt8, IPv4)),
    addresses Array(IPv4),
    addressed Tuple(ip IPv4, big Int128),
    address_keyed Map(IPv4, String),
    decimal_keyed Map(Decimal(9, 2), String),
    list_keyed Map(Array(UInt8), String),
    pair_keyed Map(Tuple(x UInt8), String),
    listed_address_keyed Array(Map(IPv6, String))
) ENGINE = MergeTree ORDER BY id;
`

func TestBatchAppendsLonePlaceholdersAsColumnsInTheRowsOrder(t *testing.T) {
	t.Parallel()

	source := generateNative(t, "-- sgl.query(InsertEvents, batch)\n"+
		"INSERT INTO events (host, id, in_stock) VALUES ({host:String}, {id:UInt64}, {in_stock:Bool});\n")["events.sql.go"]
	method := methodSource(t, source, "InsertEvents")

	require.Contains(t, method, "batch.Append(item.Host, item.ID, item.InStock)")
	require.Less(t, strings.Index(method, "if len(params) == 0 {"), strings.Index(method, "PrepareBatch"),
		"a batch without rows returns before preparing one")
	require.NotContains(t, method, "clickhouse.Named")
}

func TestBatchAppendsANamedEnumAsItsUnderlyingString(t *testing.T) {
	t.Parallel()

	schema := "CREATE TABLE moods (id UInt64, mood Enum8('calm' = 1, 'stormy' = 2), maybe Nullable(Enum8('calm' = 1, 'stormy' = 2))," +
		" moods Array(Enum8('calm' = 1, 'stormy' = 2)), maybes Array(Nullable(Enum8('calm' = 1, 'stormy' = 2)))," +
		" labels Map(String, Enum8('calm' = 1, 'stormy' = 2)), host String) ENGINE = MergeTree ORDER BY id;\n"
	enum := "Enum8('calm' = 1, 'stormy' = 2)"
	appended := "batch.Append(item.ID, string(item.Mood), (*string)(item.Maybe), sglConvertStrings[string](item.Moods), " +
		"sglConvertStringPointers[string](item.Maybes), sglConvertStringMap[string](item.Labels), item.Host)"
	testCases := []struct {
		name    string
		command string
		want    string
	}{
		{name: "a batch converts a NOT NULL enum, a nullable one, a slice, a slice of nullable ones and a map of them", command: "batch", want: appended},
		{name: "a copyfrom converts them the same way", command: "copyfrom", want: appended},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := generateNativeFrom(t, schema, "-- sgl.query(InsertMoods, "+testCase.command+")\n"+
				"INSERT INTO moods (id, mood, maybe, moods, maybes, labels, host) VALUES ({id:UInt64}, {mood:"+enum+"}, "+
				"{maybe:Nullable("+enum+")}, {moods:Array("+enum+")}, {maybes:Array(Nullable("+enum+"))}, "+
				"{labels:Map(String, "+enum+")}, {host:String});\n")
			require.Contains(t, files["models.go"], "type MoodsMood string")

			require.Contains(t, methodSource(t, files["events.sql.go"], "InsertMoods"), testCase.want)
			require.Contains(t, files["convert_strings.go"], "func sglConvertStrings[To, From ~string](values []From) []To {")
			require.Contains(t, files["convert_strings.go"], "func sglConvertStringPointers[To, From ~string](values []*From) []*To {")
			require.Contains(t, files["convert_strings.go"],
				"func sglConvertStringMap[To ~string, Key comparable, From ~string](values map[Key]From) map[Key]To {")
		})
	}
}

func TestBatchWithExpressionsWritesEachRowWithNamesOfItsOwn(t *testing.T) {
	t.Parallel()

	files := generateNative(t, "-- sgl.query(InsertEvents, batch)\n"+
		"INSERT INTO events (id, host, note) VALUES ({id:UInt64}, lower({host:String}), 'n');\n")
	method := methodSource(t, files["events.sql.go"], "InsertEvents")

	require.Contains(t, method, "row := strconv.Itoa(i)")
	require.Contains(t, method, `values.WriteString(":UInt64}, lower({p_host_")`)
	require.Contains(t, method, `clickhouse.Named("p_id_"+row, sglClickHouseShapedValue(item.ID, sglClickHouseShapes[0]))`)
	require.Contains(t, method, "if err := queries.db.Exec(ctx, values.String(), args...)")
	require.NotContains(t, method, "PrepareBatch")
	require.Contains(t, files, "clickhouse_values.go", "the rows' named values need the value helper")
}

func TestCopyFromRefusesARowItCannotAppendAsColumns(t *testing.T) {
	t.Parallel()

	generator, err := app.New(dialect_clickhouse.New(), app.WithEmitter(emit_clickhouse.New()))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package: "store",
		Migrations: fstest.MapFS{
			"migrations/0001_init.up.sql": {Data: []byte(namedSchema)},
			"queries/events.sql":          {Data: []byte("-- sgl.query(CopyEvents, copyfrom)\nINSERT INTO events (id, host) VALUES ({id:UInt64}, lower({host:String}));\n")},
		},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)

	require.Empty(t, result.Files)
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, diag.CodeBatchShape, result.Diagnostics[0].Code)
	require.Contains(t, result.Diagnostics[0].Message, "command: copyfrom sends each row's values as columns")
}

func TestBatchAppendsARowHoldingAnAddressOrAMapOfTuplesThroughTheConversion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		columns  string
		values   string
		appended string
	}{
		{
			name:    "maps, tuples and arrays holding addresses or tuples beside values appended as they are",
			columns: "id, keyed, optional, listed, maps, holder, pairs, wide, unnamed, addresses, addressed",
			values: "{id:UInt64}, {keyed:Map(String, IPv4)}, {optional:Map(String, Nullable(IPv6))}, {listed:Map(String, Array(IPv4))}, " +
				"{maps:Array(Map(String, IPv4))}, {holder:Tuple(a UInt8, m Map(String, IPv4))}, {pairs:Map(String, Tuple(x UInt8, y String))}, " +
				"{wide:Map(String, Int128)}, {unnamed:Map(String, Tuple(UInt8, IPv4))}, {addresses:Array(IPv4)}, {addressed:Tuple(ip IPv4, big Int128)}",
			appended: `sglAppendNativeShapedRow(batch.Append, sglClickHouseShapes[0], "('id'=a,'keyed'={q:4},'optional'={q:6},'listed'={q:[4]},'maps'=[{q:4}],` +
				`'holder'=('a'=n,'m'={q:4}),'pairs'={q:('x'=n,'y'=q)},'wide'=a,'unnamed'={q:('_1'=n,'_2'=4)},'addresses'=[4],` +
				`'addressed'=('ip'=4,'big'=n))", item.ID, item.Keyed, item.Optional, item.Listed, item.Maps, item.Holder, item.Pairs, ` +
				`item.Wide, item.Unnamed, item.Addresses, item.Addressed)`,
		},
		{
			name:     "an array of addresses alone",
			columns:  "id, addresses",
			values:   "{id:UInt64}, {addresses:Array(IPv4)}",
			appended: `sglAppendNativeShapedRow(batch.Append, sglClickHouseShapes[0], "('id'=a,'addresses'=[4])", item.ID, item.Addresses)`,
		},
	}
	for _, testCase := range testCases {
		for _, command := range []string{"batch", "copyfrom"} {
			t.Run(testCase.name+" in a "+command, func(t *testing.T) {
				t.Parallel()

				files, diagnostics := generateNativeDiagnostics(t, appendedMapsSchema, "-- sgl.query(AppendHosts, "+command+")\n"+
					"INSERT INTO hosts ("+testCase.columns+") VALUES ("+testCase.values+");\n")

				require.Empty(t, diagnostics)
				method := methodSource(t, files["events.sql.go"], "AppendHosts")
				require.Contains(t, method, "if err := "+testCase.appended+"; err != nil {")
				require.NotContains(t, method, "batch.Append(")
				require.Contains(t, files["clickhouse_appending.go"],
					"func sglAppendNativeRow(appendRow func(...any) error, shape string, values ...any) error {")
				require.Contains(t, files["clickhouse_values.go"], "func sglClickHouseParseShape(text string) (sglClickHouseShape, error) {",
					"the conversion reads its shapes with the value helper's parser")
				require.NotContains(t, files, "clickhouse_composites.go", "nothing is read through the converting scan adapter")
			})
		}
	}
}

func TestNativeDriverAddsTheAppendingHelperOnlyWhereABatchAppendsThroughIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries string
		binds   bool
	}{
		{name: "a batch appending values the driver takes as they are",
			queries: "-- sgl.query(AppendHosts, copyfrom)\nINSERT INTO hosts (id, wide) VALUES ({id:UInt64}, {wide:Map(String, Int128)});\n"},
		{name: "an exec binding a map of addresses by name", binds: true,
			queries: "-- sgl.query(InsertHost, exec)\nINSERT INTO hosts (id, keyed) VALUES ({id:UInt64}, {keyed:Map(String, IPv4)});\n"},
		{name: "a batch writing rows of expressions", binds: true,
			queries: "-- sgl.query(InsertHosts, batch)\nINSERT INTO hosts (id, keyed) VALUES ({id:UInt64} + 1, {keyed:Map(String, IPv4)});\n"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := generateNativeDiagnostics(t, appendedMapsSchema, testCase.queries)

			require.Empty(t, diagnostics)
			require.NotContains(t, files["events.sql.go"], "sglAppendNativeShapedRow")
			require.NotContains(t, files, "clickhouse_appending.go")
			_, values := files["clickhouse_values.go"]
			require.Equal(t, testCase.binds, values)
		})
	}
}

func TestNativeBatchRefusesAMapKeyedByATypeTheDriverCannotDecode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		column string
		value  string
		key    string
	}{
		{name: "an address", column: "address_keyed", value: "Map(IPv4, String)", key: "an IPv4 or IPv6 address"},
		{name: "a decimal", column: "decimal_keyed", value: "Map(Decimal(9, 2), String)", key: "a Decimal"},
		{name: "an array", column: "list_keyed", value: "Map(Array(UInt8), String)", key: "an Array"},
		{name: "a tuple", column: "pair_keyed", value: "Map(Tuple(x UInt8), String)", key: "a Tuple"},
		{name: "an address within an array", column: "listed_address_keyed", value: "Array(Map(IPv6, String))", key: "an IPv4 or IPv6 address"},
	}
	for _, testCase := range testCases {
		for _, command := range []string{"batch", "copyfrom"} {
			t.Run(testCase.name+" in a "+command, func(t *testing.T) {
				t.Parallel()

				files, diagnostics := generateNativeDiagnostics(t, appendedMapsSchema, "-- sgl.query(AppendHosts, "+command+")\n"+
					"INSERT INTO hosts (id, "+testCase.column+") VALUES ({id:UInt64}, {"+testCase.column+":"+testCase.value+"});\n")

				require.Empty(t, files)
				require.Len(t, diagnostics, 1)
				require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
				require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
				require.Contains(t, diagnostics[0].Message, "parameter "+testCase.column+" holds a Map keyed by "+testCase.key+
					", which the native ClickHouse driver cannot decode, so it cannot prepare a batch for the column")
			})
		}
	}
}

func TestNativeExecBindsAMapKeyedByAnAddressAsText(t *testing.T) {
	t.Parallel()

	files, diagnostics := generateNativeDiagnostics(t, appendedMapsSchema, "-- sgl.query(InsertHost, exec)\n"+
		"INSERT INTO hosts (id, address_keyed) VALUES ({id:UInt64}, {address_keyed:Map(IPv4, String)});\n")

	require.Empty(t, diagnostics)
	require.Contains(t, methodSource(t, files["events.sql.go"], "InsertHost"),
		`clickhouse.Named("p_address_keyed", sglClickHouseShapedValue(params.AddressKeyed, sglClickHouseShapes[1]))`)
}

func TestANativeBatchRowHoldingAnOverriddenValueAppendsThroughTheConversion(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, overriddenSchema, "-- sgl.query(CopyLabels, copyfrom)\n"+
		"INSERT INTO labels (id, label, plain) VALUES ({id:UInt64}, {label:String}, {plain:String});\n"+
		"-- sgl.query(CopyPlain, copyfrom)\n"+
		"INSERT INTO labels (id, plain) VALUES ({id:UInt64}, {plain:String});\n")

	require.Contains(t, methodSource(t, files["events.sql.go"], "CopyLabels"),
		`sglAppendNativeShapedRow(batch.Append, sglClickHouseShapes[0], "('id'=a,'label'=a,'plain'=a)", item.ID, item.Label, item.Plain)`)
	require.Contains(t, methodSource(t, files["events.sql.go"], "CopyPlain"), "batch.Append(item.ID, item.Plain)")
	require.Contains(t, files["clickhouse_appending.go"], "func sglAppendNativeBasic(source reflect.Type) reflect.Type {")
}
