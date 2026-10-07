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

//go:build integration

package clickhouse_test

import (
	"io/fs"
	"math/big"
	"net/netip"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/emit/emit_driver_clickhouse"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const namedTupleMapsSchema = `CREATE TABLE holders (
    id UInt16,
    keyed Map(String, Tuple(a UInt8, b String)),
    listed_keyed Array(Map(String, Tuple(a UInt8, b String))),
    holder Tuple(a UInt8, m Map(String, Tuple(x UInt8))),
    listed_holders Array(Tuple(a UInt8, m Map(String, Tuple(x UInt8)))),
    keyed_holders Map(String, Tuple(a UInt8, m Map(String, Tuple(x UInt8)))),
    keyed_pairs Map(String, Tuple(UInt8, String)),
    fields Tuple(a UInt8, b String),
    listed_fields Array(Tuple(a UInt8, b String)),
    pair_holding_keyed Tuple(UInt8, Map(String, Tuple(x UInt8))),
    addressed Tuple(ip IPv4, big Int128),
    listed_addressed Array(Tuple(a UInt8, ip IPv4)),
    listed_wide Array(Tuple(a UInt8, big UInt256)),
    addresses Array(IPv4),
    nullable_addresses Array(Nullable(IPv4)),
    keyed_addresses Map(String, IPv6),
    nullable_wide_fields Tuple(a UInt8, n Nullable(Int128)),
    wide_list Array(Int128),
    keyed_wide Map(String, Int128),
    address_keyed Map(IPv4, String),
    wide_pair Tuple(UInt8, Int128),
    address_pair Tuple(UInt8, IPv4)
) ENGINE = MergeTree ORDER BY id`

const namedTupleMapsRow = `INSERT INTO holders VALUES (1, {'k': (1, 'x')}, [{'k': (2, 'y')}], (3, {'z': (4)}), [(5, {'z': (6)})],
    {'h': (7, {'z': (8)})}, {'p': (9, 'nine')}, (10, 'ten'), [(11, 'eleven')], (12, {'z': (13)}),
    ('10.0.0.1', 14), [(15, '10.0.0.2')], [(16, 17)], ['10.0.0.3'], ['10.0.0.4', NULL], {'v6': '::1'},
    (18, 19), [20], {'w': 21}, {'10.0.0.5': 'five'}, (22, 23), (24, '10.0.0.6'))`

type holderPair struct {
	A uint8  `json:"a"`
	B string `json:"b"`
}

type holderElement struct {
	X uint8 `json:"x"`
}

type holderOfMap struct {
	A uint8                    `json:"a"`
	M map[string]holderElement `json:"m"`
}

type holderAddressed struct {
	IP  netip.Addr `json:"ip"`
	Big *big.Int   `json:"big"`
}

type holderListedAddress struct {
	A  uint8      `json:"a"`
	IP netip.Addr `json:"ip"`
}

type holderListedWide struct {
	A   uint8    `json:"a"`
	Big *big.Int `json:"big"`
}

type holderNullableWide struct {
	A uint8    `json:"a"`
	N *big.Int `json:"n"`
}

type holderWidePair struct {
	A uint8    `json:"a"`
	B *big.Int `json:"b"`
}

type holderScanner struct {
	received bool
}

func (scanner *holderScanner) Scan(any) error {
	scanner.received = true
	return nil
}

type namedTupleReading uint8

const (
	readsAsItIs namedTupleReading = iota
	readsConverted
	readsRaw
	refused
)

func TestNativeReadingsMatchWhatTheDriverReads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, dsn := harness.IsolatedDatabase(t, "clickhouse", testConnectionString)
	options, err := clickhouse.ParseDSN(dsn)
	require.NoError(t, err)
	native, err := clickhouse.Open(options)
	require.NoError(t, err)
	for _, statement := range []string{namedTupleMapsSchema, namedTupleMapsRow} {
		require.NoError(t, native.Exec(t.Context(), statement))
	}
	require.NoError(t, native.Close())

	testCases := []struct {
		destination func() any
		name        string
		column      string
		reading     namedTupleReading
	}{
		{name: "a map to named tuples", column: "keyed", destination: func() any { return new(map[string]holderPair) }, reading: readsConverted},
		{name: "an array of maps to named tuples", column: "listed_keyed", destination: func() any { return new([]map[string]holderPair) }, reading: readsConverted},
		{name: "a named tuple holding a map to named tuples", column: "holder", destination: func() any { return new(holderOfMap) }, reading: readsRaw},
		{name: "an element of a named tuple holding a map to named tuples", column: "holder.m", destination: func() any { return new(map[string]holderElement) }, reading: readsConverted},
		{name: "an array of named tuples holding a map to named tuples", column: "listed_holders", destination: func() any { return new([]holderOfMap) }, reading: readsConverted},
		{name: "a map to named tuples holding a map to named tuples", column: "keyed_holders", destination: func() any { return new(map[string]holderOfMap) }, reading: readsConverted},
		{name: "a map to unnamed tuples", column: "keyed_pairs", destination: func() any { return new(map[string][]any) }, reading: readsAsItIs},
		{name: "a named tuple", column: "fields", destination: func() any { return new(holderPair) }, reading: readsAsItIs},
		{name: "an array of named tuples", column: "listed_fields", destination: func() any { return new([]holderPair) }, reading: readsAsItIs},
		{name: "an unnamed tuple holding a map to named tuples", column: "pair_holding_keyed", destination: func() any { return new([]any) }, reading: readsAsItIs},
		{name: "a named tuple holding an address and a wide integer", column: "addressed", destination: func() any { return new(holderAddressed) }, reading: readsRaw},
		{name: "an array of named tuples holding an address", column: "listed_addressed", destination: func() any { return new([]holderListedAddress) }, reading: readsConverted},
		{name: "an array of named tuples holding a wide integer", column: "listed_wide", destination: func() any { return new([]holderListedWide) }, reading: readsConverted},
		{name: "an array of addresses", column: "addresses", destination: func() any { return new([]netip.Addr) }, reading: readsConverted},
		{name: "an array of nullable addresses", column: "nullable_addresses", destination: func() any { return new([]*netip.Addr) }, reading: readsConverted},
		{name: "a map to addresses", column: "keyed_addresses", destination: func() any { return new(map[string]netip.Addr) }, reading: readsConverted},
		{name: "a named tuple holding a nullable wide integer", column: "nullable_wide_fields", destination: func() any { return new(holderNullableWide) }, reading: readsAsItIs},
		{name: "an array of wide integers", column: "wide_list", destination: func() any { return new([]*big.Int) }, reading: readsAsItIs},
		{name: "a map to wide integers", column: "keyed_wide", destination: func() any { return new(map[string]*big.Int) }, reading: refused},
		{name: "a map keyed by addresses", column: "address_keyed", destination: func() any { return new(map[netip.Addr]string) }, reading: refused},
		{name: "an unnamed tuple holding a wide integer", column: "wide_pair", destination: func() any { return new([]any) }, reading: refused},
		{name: "an unnamed tuple holding an address", column: "address_pair", destination: func() any { return new([]any) }, reading: readsAsItIs},
		{name: "a map to wide integers cast to nullable ones", column: "CAST(keyed_wide AS Map(String, Nullable(Int128)))",
			destination: func() any { return new(map[string]*big.Int) }, reading: readsAsItIs},
		{name: "the keys of a map to wide integers", column: "mapKeys(keyed_wide)", destination: func() any { return new([]string) }, reading: readsAsItIs},
		{name: "a map keyed by addresses cast to text keys", column: "CAST(address_keyed AS Map(String, String))",
			destination: func() any { return new(map[string]string) }, reading: readsAsItIs},
		{name: "an unnamed tuple holding a wide integer cast to a named one", column: "CAST(wide_pair AS Tuple(a UInt8, b Int128))",
			destination: func() any { return new(holderWidePair) }, reading: readsRaw},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			query := "SELECT " + testCase.column + " AS value FROM holders"
			scan := func(destination any) error { return scanOnce(t, options, query, destination) }
			readsDirectly := scansWithoutError(scan, testCase.destination())
			var scanner holderScanner
			scanned := scansWithoutError(scan, &scanner) && scanner.received
			readsIntoMap := scansWithoutError(scan, new(map[string]any))

			require.Equal(t, testCase.reading, nativeReading(t, query))
			require.Equal(t, testCase.reading == readsAsItIs, readsDirectly, "the native driver reads the generated type itself")
			require.Equal(t, testCase.reading == readsConverted, !readsDirectly && scanned, "the native driver hands a scanner its value")
			require.Equal(t, testCase.reading == readsRaw, !readsDirectly && !scanned && readsIntoMap,
				"the native driver fills a map with its own values")
		})
	}
}

func scanOnce(t *testing.T, options *clickhouse.Options, query string, destination any) error {
	t.Helper()

	native, err := clickhouse.Open(options)
	require.NoError(t, err)
	defer func() { _ = native.Close() }()
	return native.QueryRow(t.Context(), query).Scan(destination)
}

func scansWithoutError(scan func(destination any) error, destination any) (scans bool) {
	defer func() {
		if recover() != nil {
			scans = false
		}
	}()
	return scan(destination) == nil
}

func nativeReading(t *testing.T, query string) namedTupleReading {
	t.Helper()

	generator, err := seagull.New(dialect_engine_clickhouse.New(), seagull.WithEmitter(emit_driver_clickhouse.New()))
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations":                    &fstest.MapFile{Mode: fs.ModeDir},
		"migrations/001_holders.up.sql": &fstest.MapFile{Data: []byte(namedTupleMapsSchema + ";\n")},
		"queries/holders.sql":           &fstest.MapFile{Data: []byte("-- sgl.query(Probe, many)\n" + query + ";\n")},
	}
	result, err := generator.Generate(t.Context(), seagull.Input{Package: "db", Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	for _, diagnostic := range result.Diagnostics {
		require.Equal(t, seagull.CodeEmitterUnsupported, diagnostic.Code, diagnostic.Message)
		return refused
	}
	for _, file := range result.Files {
		if strings.Contains(string(file.Content), "sglScanNativeComposite(&row.Value)") {
			return readsConverted
		}
		if strings.Contains(string(file.Content), "sglReadNativeColumn(&row.Value, raw.Value)") {
			return readsRaw
		}
	}
	return readsAsItIs
}
