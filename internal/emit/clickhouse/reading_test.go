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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
)

const namedTupleMapsSchema = `CREATE TABLE stock (
    id UInt64,
    keyed Map(String, Tuple(a UInt8, b String)),
    listed_keyed Array(Map(String, Tuple(a UInt8, b String))),
    keyed_lists Map(String, Array(Tuple(a UInt8, b String))),
    keyed_keyed Map(String, Map(UInt8, Tuple(a UInt8, b String))),
    listed_holders Array(Tuple(a UInt8, m Map(String, Tuple(x UInt8)))),
    holder Tuple(a UInt8, m Map(String, Tuple(x UInt8))),
    keyed_pairs Map(String, Tuple(UInt8, String)),
    pair_holding_keyed Tuple(UInt8, Map(String, Tuple(x UInt8))),
    fields Tuple(a UInt8, b String),
    counts Map(String, UInt32),
    listed_fields Array(Tuple(a UInt8, b String)),
    addressed Tuple(ip IPv4, big Int128),
    addresses Array(IPv4),
    keyed_addresses Map(String, IPv6),
    addressed_list Array(Tuple(a UInt8, ip IPv4)),
    wide_list Array(Tuple(a UInt8, big UInt256)),
    wides Array(Int128)
) ENGINE = MergeTree ORDER BY id;
`

const overriddenSchema = `-- sgl.column(labels.label, go_type: "example.com/kinds.Label")
-- sgl.column(labels.rank, go_type: "example.com/kinds.Rank")
-- sgl.column(labels.host, go_type: "encoding/json.Number")
-- sgl.column(labels.pair, go_type: "example.com/kinds.Pair")
CREATE TABLE labels (
    id UInt64,
    label String,
    rank Nullable(Int64),
    host IPv4,
    pair Tuple(a UInt8, b String),
    plain String
) ENGINE = MergeTree ORDER BY id;
`

const documentsSchema = `CREATE TABLE documents (
    id UInt64,
    data JSON,
    optional Nullable(JSON),
    listed Array(JSON),
    keyed Map(String, JSON),
    optional_keyed Map(String, Nullable(JSON)),
    pair Tuple(doc JSON, n Int64),
    unnamed Tuple(JSON, Int64),
    listed_keyed Array(Map(String, JSON)),
    holding_keyed Tuple(m Map(String, JSON))
) ENGINE = MergeTree ORDER BY id;

CREATE TABLE owners (
    id UInt64,
    name String
) ENGINE = MergeTree ORDER BY id;
`

func TestNativeDriverReadsMapsOfNamedTuplesThroughTheConvertingScan(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, namedTupleMapsSchema, "-- sgl.query(ListStock, many)\n"+
		"SELECT id, keyed, listed_keyed, keyed_lists, keyed_keyed, listed_holders, keyed_pairs, pair_holding_keyed, fields, counts, "+
		"listed_fields, holder.m AS held FROM stock;\n")
	method := methodSource(t, files["events.sql.go"], "ListStock")

	require.Contains(t, method, ".Scan(&row.ID, sglScanNativeComposite(&row.Keyed), sglScanNativeComposite(&row.ListedKeyed), "+
		"sglScanNativeComposite(&row.KeyedLists), sglScanNativeComposite(&row.KeyedKeyed), sglScanNativeComposite(&row.ListedHolders), "+
		"&row.KeyedPairs, &row.PairHoldingKeyed, &row.Fields, &row.Counts, &row.ListedFields, sglScanNativeComposite(&row.Held))")
	require.Contains(t, files["clickhouse_composites.go"], "func sglScanNativeComposite[T any](dest *T) sql.Scanner {")
}

func TestNativeDriverAddsNoConvertingScanWhereNoColumnNeedsIt(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, namedTupleMapsSchema, "-- sgl.query(ListStock, many)\n"+
		"SELECT id, keyed_pairs, fields, counts, listed_fields FROM stock;\n")

	require.NotContains(t, files["events.sql.go"], "sglScanNativeComposite")
	require.NotContains(t, files, "clickhouse_composites.go")
}

func TestNativeDriverReadsANamedTupleItCannotFillThroughARawMap(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		query string
		field string
	}{
		{name: "a stored column holding a map to named tuples", query: "SELECT holder FROM stock", field: "Holder"},
		{name: "a built tuple holding a map to named tuples",
			query: "SELECT CAST((id, keyed) AS Tuple(id UInt64, keyed Map(String, Tuple(a UInt8, b String)))) AS built FROM stock", field: "Built"},
		{name: "a stored column holding an address and a wide integer", query: "SELECT addressed FROM stock", field: "Addressed"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := generateNativeFrom(t, namedTupleMapsSchema, "-- sgl.query(GetHolder, many)\n"+testCase.query+";\n")
			method := methodSource(t, files["events.sql.go"], "GetHolder")

			require.Contains(t, method, ".Scan(&raw."+testCase.field+")")
			require.Contains(t, method, "if err := sglReadNativeColumn(&row."+testCase.field+", raw."+testCase.field+"); err != nil {")
			require.Contains(t, files["clickhouse_composites.go"], "func sglReadNativeColumn[T any](dest *T, source any) error {")
		})
	}
}

func TestNativeDriverReadsAddressesAndWideIntegersInsideContainersThroughTheConvertingScan(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, namedTupleMapsSchema, "-- sgl.query(ListAddresses, many)\n"+
		"SELECT addresses, keyed_addresses, addressed_list, wide_list, wides FROM stock;\n")
	method := methodSource(t, files["events.sql.go"], "ListAddresses")

	require.Contains(t, method, ".Scan(sglScanNativeComposite(&row.Addresses), sglScanNativeComposite(&row.KeyedAddresses), "+
		"sglScanNativeComposite(&row.AddressedList), sglScanNativeComposite(&row.WideList), &row.Wides)")
}

func TestNativeDriverReadsAColumnWhoseGoTypeAnOverrideChoseThroughTheConvertingScan(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, overriddenSchema, "-- sgl.query(ListLabels, many)\nSELECT id, label, rank, host, pair, plain FROM labels;\n")
	method := methodSource(t, files["events.sql.go"], "ListLabels")

	require.Contains(t, method, ".Scan(&row.ID, sglScanNativeComposite(&row.Label), sglScanNativeComposite(&row.Rank), "+
		"sglScanNativeComposite(&row.Host), &row.Pair, &row.Plain)",
		"a tuple is read into a struct by its fields, without consulting a scanner, so its override is read as it is")
	require.Contains(t, files["clickhouse_composites.go"], "func sglReadNativeText(target reflect.Value, value reflect.Value) (bool, error) {")
}

func TestNativeDriverReadsEveryJSONColumnThroughTheConversion(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, documentsSchema, "-- sgl.query(GetDocument, one)\n"+
		"SELECT id, data, optional, listed, keyed, optional_keyed, pair, unnamed FROM documents WHERE id = {id:UInt64};\n"+
		"-- sgl.query(ListDocuments, many)\n"+
		"SELECT id, data FROM documents WHERE id = {id:UInt64} UNION ALL SELECT id, data FROM documents WHERE id = {id:UInt64};\n"+
		"-- sgl.query(ListOwners, many)\n"+
		"-- sgl.embed(documents, from: d)\n"+
		"SELECT o.id, o.name, d.id, d.data FROM owners o INNER JOIN documents d ON d.id = o.id;\n")
	queries := files["events.sql.go"]

	require.Contains(t, methodSource(t, queries, "GetDocument"), ".Scan(&row.ID, sglScanNativeComposite(&row.Data), "+
		"sglScanNativeComposite(&row.Optional), sglScanNativeComposite(&row.Listed), &raw.Keyed, sglScanNativeComposite(&row.OptionalKeyed), "+
		"&raw.Pair, &raw.Unnamed)")
	require.Contains(t, methodSource(t, queries, "GetDocument"), "Keyed   sglNativeEntries")
	require.Contains(t, methodSource(t, queries, "GetDocument"), "sglReadNativeColumn(&row.Keyed, raw.Keyed)")
	require.Contains(t, methodSource(t, queries, "GetDocument"), "Unnamed []any")
	require.Contains(t, methodSource(t, queries, "ListDocuments"), ".Scan(&row.ID, sglScanNativeComposite(&row.Data))",
		"a compound statement's JSON column is read through the adapter, its SQL as written")
	require.Contains(t, methodSource(t, queries, "ListOwners"), "sglScanNativeComposite(&row.Documents.Data)",
		"an embed's JSON column is read through the adapter")
	require.Contains(t, files["clickhouse_composites.go"], "DeserializeClickHouseJSON(document *chcol.JSON) error {")
	require.Contains(t, files["clickhouse_composites.go"], "type sglNativeEntries map[any]any")
	require.Contains(t, files["clickhouse_documents.go"], "reflect.TypeFor[JSON]()")
	require.Contains(t, files, "sgl_json.go")
}

func TestNativeDriverReadsAJSONColumnWhoseGoTypeAnOverrideChoseThroughTheConversion(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, documentsSchema, "-- sgl.query(GetRaw, one)\n"+
		"-- sgl.column(data, go_type: \"encoding/json.RawMessage\")\n"+
		"SELECT data FROM documents WHERE id = {id:UInt64};\n")

	require.Contains(t, methodSource(t, files["events.sql.go"], "GetRaw"), ".Scan(sglScanNativeComposite(&row.Data))")
	require.Contains(t, files["clickhouse_composites.go"], "DeserializeClickHouseJSON(document *chcol.JSON) error {")
	require.Contains(t, files, "clickhouse_documents.go")
}

func TestNativeDriverLeavesAPackageReadingNoJSONDocumentAsItWas(t *testing.T) {
	t.Parallel()

	files := generateNativeFrom(t, namedTupleMapsSchema, "-- sgl.query(ListStock, many)\nSELECT id, keyed FROM stock;\n")

	require.NotContains(t, files, "clickhouse_documents.go")
	require.NotContains(t, files["clickhouse_composites.go"], "chcol")
	require.NotContains(t, files["clickhouse_composites.go"], "Entries")
}

func TestNativeDriverRefusesAMapOfJSONItCannotRead(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries string
		want    string
	}{
		{
			name:    "an array of maps of documents",
			queries: "-- sgl.query(GetListed, one)\nSELECT listed_keyed FROM documents WHERE id = {id:UInt64};\n",
			want: "column listed_keyed holds a Map whose values are JSON documents inside an Array, a Map or a Tuple, which the " +
				"native ClickHouse driver panics reading",
		},
		{
			name:    "a tuple holding a map of documents",
			queries: "-- sgl.query(GetHolding, one)\nSELECT holding_keyed FROM documents WHERE id = {id:UInt64};\n",
			want: "column holding_keyed holds a Map whose values are JSON documents inside an Array, a Map or a Tuple, which the " +
				"native ClickHouse driver panics reading",
		},
		{
			name: "a map of documents whose Go type an override chose",
			queries: "-- sgl.query(GetKeyed, one)\n-- sgl.column(keyed, go_type: \"example.com/documents.Keyed\")\n" +
				"SELECT keyed FROM documents WHERE id = {id:UInt64};\n",
			want: "column keyed is a Map whose values are JSON documents, which the native ClickHouse driver reads only entry by " +
				"entry into the generated map type",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := generateNativeDiagnostics(t, documentsSchema, testCase.queries)

			require.Empty(t, files)
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Contains(t, diagnostics[0].Message, testCase.want)
		})
	}
}
