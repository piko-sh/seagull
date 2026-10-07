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
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
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

type appendedPair struct {
	X uint8  `json:"x"`
	Y string `json:"y"`
}

type appendedHolder struct {
	A uint8                 `json:"a"`
	M map[string]netip.Addr `json:"m"`
}

type appendedAddress struct {
	IP netip.Addr `json:"ip"`
	X  uint8      `json:"x"`
}

type nativeAppending uint8

const (
	appendsAsItIs nativeAppending = iota
	appendsChecked
	appendsConverted
	appendRefused
)

var errNotPrepared = errors.New("the batch was not prepared")

func TestNativeAppendsMatchWhatTheDriverAppends(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, dsn := harness.IsolatedDatabase(t, "clickhouse", testConnectionString)
	options, err := clickhouse.ParseDSN(dsn)
	require.NoError(t, err)

	first := netip.MustParseAddr("10.0.0.1")
	second := netip.MustParseAddr("2001:db8::1")
	zero := netip.Addr{}
	firstIP := net.IP(first.AsSlice())
	secondIP := net.IP(second.AsSlice())
	testCases := []struct {
		generated any
		invalid   any
		driver    any
		name      string
		column    string
		shape     string
		appending nativeAppending
	}{
		{name: "a map to addresses", column: "Map(String, IPv4)", appending: appendsConverted, shape: "{q:4}",
			generated: map[string]netip.Addr{"k": first}, driver: map[string]net.IP{"k": firstIP}},
		{name: "a map to nullable addresses", column: "Map(String, Nullable(IPv6))", appending: appendsConverted, shape: "{q:6}",
			generated: map[string]*netip.Addr{"k": &second, "n": nil}, driver: map[string]*net.IP{"k": &secondIP, "n": nil}},
		{name: "a map to arrays of addresses", column: "Map(String, Array(IPv4))", appending: appendsConverted, shape: "{q:[4]}",
			generated: map[string][]netip.Addr{"k": {first}}, driver: map[string][]net.IP{"k": {firstIP}}},
		{name: "a map to arrays of nullable addresses", column: "Map(String, Array(Nullable(IPv4)))", appending: appendsConverted,
			shape: "{q:[4]}", generated: map[string][]*netip.Addr{"k": {&first, nil}}, driver: map[string][]*net.IP{"k": {&firstIP, nil}}},
		{name: "a map to maps to addresses", column: "Map(String, Map(String, IPv6))", appending: appendsConverted, shape: "{q:{q:6}}",
			generated: map[string]map[string]netip.Addr{"k": {"j": second}}, driver: map[string]map[string]net.IP{"k": {"j": secondIP}}},
		{name: "an array of maps to addresses", column: "Array(Map(String, IPv4))", appending: appendsConverted, shape: "[{q:4}]",
			generated: []map[string]netip.Addr{{"k": first}}, driver: []map[string]net.IP{{"k": firstIP}}},
		{name: "a named tuple holding a map to addresses", column: "Tuple(a UInt8, m Map(String, IPv4))", appending: appendsConverted,
			shape: "('a'=n,'m'={q:4})", generated: appendedHolder{A: 1, M: map[string]netip.Addr{"k": first}},
			driver: map[string]any{"a": uint8(1), "m": map[string]net.IP{"k": firstIP}}},
		{name: "a map to named tuples", column: "Map(String, Tuple(x UInt8, y String))", appending: appendsConverted, shape: "{q:('x'=n,'y'=q)}",
			generated: map[string]appendedPair{"k": {X: 1, Y: "y"}}, driver: map[string]map[string]any{"k": {"x": uint8(1), "y": "y"}}},
		{name: "a map to arrays of named tuples", column: "Map(String, Array(Tuple(x UInt8, y String)))", appending: appendsConverted,
			shape: "{q:[('x'=n,'y'=q)]}", generated: map[string][]appendedPair{"k": {{X: 1, Y: "y"}}},
			driver: map[string][]map[string]any{"k": {{"x": uint8(1), "y": "y"}}}},
		{name: "a map to unnamed tuples holding a map to addresses", column: "Map(String, Tuple(UInt8, Map(String, IPv4)))",
			appending: appendsConverted, shape: "{q:('_1'=n,'_2'={q:4})}", generated: map[string][]any{"k": {uint8(1), map[string]netip.Addr{"j": first}}},
			driver: map[string][]any{"k": {uint8(1), map[string]net.IP{"j": firstIP}}}},
		{name: "a map to unnamed tuples holding an address", column: "Map(String, Tuple(UInt8, IPv4))", appending: appendsChecked,
			shape: "{q:('_1'=n,'_2'=4)}", generated: map[string][]any{"k": {uint8(1), first}}, invalid: map[string][]any{"k": {uint8(1), second}}},
		{name: "a map to wide integers", column: "Map(String, Int128)", appending: appendsAsItIs,
			generated: map[string]*big.Int{"k": big.NewInt(-5)}},
		{name: "a map keyed by wide integers", column: "Map(UInt256, String)", appending: appendsAsItIs,
			generated: map[*big.Int]string{big.NewInt(5): "v"}},
		{name: "a map to nullable wide integers", column: "Map(String, Nullable(Int128))", appending: appendsAsItIs,
			generated: map[string]*big.Int{"k": big.NewInt(5), "n": nil}},
		{name: "a map to arrays of wide integers", column: "Map(String, Array(Int256))", appending: appendsAsItIs,
			generated: map[string][]*big.Int{"k": {big.NewInt(5)}}},
		{name: "an address", column: "IPv4", appending: appendsChecked, shape: "4", generated: first, invalid: zero},
		{name: "an IPv6 address for IPv4", column: "IPv4", appending: appendsChecked, shape: "4", generated: first, invalid: second},
		{name: "a nullable address", column: "Nullable(IPv4)", appending: appendsChecked, shape: "4", generated: &first, invalid: &zero},
		{name: "an IPv6 address", column: "IPv6", appending: appendsChecked, shape: "6", generated: second, invalid: zero},
		{name: "an array of addresses", column: "Array(IPv4)", appending: appendsChecked, shape: "[4]",
			generated: []netip.Addr{first}, invalid: []netip.Addr{first, second}},
		{name: "an array of nullable addresses", column: "Array(Nullable(IPv6))", appending: appendsChecked, shape: "[6]",
			generated: []*netip.Addr{&second, nil}, invalid: []*netip.Addr{&zero}},
		{name: "a named tuple holding an address", column: "Tuple(ip IPv4, x UInt8)", appending: appendsChecked, shape: "('ip'=4,'x'=n)",
			generated: appendedAddress{IP: first, X: 1}, invalid: appendedAddress{IP: zero, X: 1}},
		{name: "an unnamed tuple holding an address", column: "Tuple(IPv4, UInt8)", appending: appendsChecked, shape: "('_1'=4,'_2'=n)",
			generated: []any{first, uint8(1)}, invalid: []any{second, uint8(1)}},
		{name: "a map keyed by an address", column: "Map(IPv4, String)", appending: appendRefused},
		{name: "a map keyed by a decimal", column: "Map(Decimal(9, 2), String)", appending: appendRefused},
		{name: "a map keyed by an array", column: "Map(Array(UInt8), String)", appending: appendRefused},
		{name: "a map keyed by a named tuple", column: "Map(Tuple(x UInt8), String)", appending: appendRefused},
		{name: "a map keyed by a map", column: "Map(Map(String, String), String)", appending: appendRefused},
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			table := fmt.Sprintf("appended_%d", index)
			native, err := clickhouse.Open(options)
			require.NoError(t, err)
			t.Cleanup(func() { _ = native.Close() })
			require.NoError(t, native.Exec(t.Context(), "CREATE TABLE "+table+" (id UInt8, v "+testCase.column+") ENGINE = Memory"))

			prepares := !errors.Is(appendOnce(t, native, table, nil), errNotPrepared)
			appendsGenerated := testCase.generated != nil && appendOnce(t, native, table, testCase.generated) == nil

			refused, shape := nativeAppendingOf(t, testCase.column)
			require.Equal(t, testCase.appending == appendRefused, refused, "Seagull refuses the batch")
			require.Equal(t, testCase.shape, shape, "the shape the generated row passes the conversion")
			require.Equal(t, testCase.appending == appendRefused, !prepares, "the native driver prepares a batch for the column")
			require.Equal(t, testCase.appending == appendsAsItIs || testCase.appending == appendsChecked, appendsGenerated,
				"the native driver appends the generated type itself")
			if testCase.appending == appendsConverted {
				require.NoError(t, appendOnce(t, native, table, testCase.driver), "the native driver appends its own types")
			}
			if testCase.invalid != nil {
				failure := appendOnce(t, native, table, testCase.invalid)
				require.True(t, failure == nil || strings.HasPrefix(failure.Error(), "panic: "),
					"the native driver panics on or stores an address the column cannot hold, rather than refusing it: %v", failure)
			}
		})
	}
}

func appendOnce(t *testing.T, native clickhouse.Conn, table string, value any) (failure error) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = fmt.Errorf("panic: %v", recovered)
		}
	}()
	batch, err := native.PrepareBatch(t.Context(), "INSERT INTO "+table+" (id, v)")
	if err != nil {
		return errNotPrepared
	}
	defer func() { _ = batch.Close() }()
	if value == nil {
		return batch.Abort()
	}
	if err := batch.Append(uint8(1), value); err != nil {
		return err
	}
	return batch.Send()
}

func nativeAppendingOf(t *testing.T, column string) (bool, string) {
	t.Helper()

	generator, err := seagull.New(dialect_engine_clickhouse.New(), seagull.WithEmitter(emit_driver_clickhouse.New()))
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations":                     &fstest.MapFile{Mode: fs.ModeDir},
		"migrations/001_appended.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE appended (id UInt8, v " + column + ") ENGINE = MergeTree ORDER BY id;\n")},
		"queries/appended.sql": &fstest.MapFile{Data: []byte("-- sgl.query(Probe, copyfrom)\n" +
			"INSERT INTO appended (id, v) VALUES ({id:UInt8}, {v:" + column + "});\n")},
	}
	result, err := generator.Generate(t.Context(), seagull.Input{Package: "db", Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	for _, diagnostic := range result.Diagnostics {
		require.Equal(t, seagull.CodeEmitterUnsupported, diagnostic.Code, diagnostic.Message)
		return true, ""
	}
	for _, file := range result.Files {
		_, rest, routed := strings.Cut(string(file.Content), `sglAppendNativeShapedRow(batch.Append, sglClickHouseShapes[0], "('id'=a,'v'=`)
		if routed {
			shape, _, found := strings.Cut(rest, `)", item.ID, item.V)`)
			require.True(t, found, "the row passes the two values")
			return false, shape
		}
	}
	return false, ""
}

func TestMapsKeyedByATypeTheDriverCannotDecodeAreRefusedOnRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, dsn := harness.IsolatedDatabase(t, "clickhouse", testConnectionString)
	options, err := clickhouse.ParseDSN(dsn)
	require.NoError(t, err)

	testCases := []struct {
		name      string
		column    string
		value     string
		decodable bool
	}{
		{name: "a map keyed by an address", column: "Map(IPv4, String)", value: "map(toIPv4('10.0.0.1'), 'v')"},
		{name: "a map keyed by an array", column: "Map(Array(UInt8), String)", value: "map([1], 'v')"},
		{name: "a map keyed by a named tuple", column: "Map(Tuple(x UInt8), String)", value: "map(tuple(1), 'v')"},
		{name: "a map keyed by an unnamed tuple", column: "Map(Tuple(UInt8, String), String)", value: "map((1, 'a'), 'v')"},
		{name: "a map keyed by a map", column: "Map(Map(String, String), String)", value: "map(map('k', 'v'), 'v')"},
		{name: "a map keyed by a decimal", column: "Map(Decimal(9, 2), String)", value: "map(toDecimal32(1.5, 2), 'v')"},
		{name: "a map keyed by a UUID", column: "Map(UUID, String)", value: "map(toUUID('6ba7b810-9dad-11d1-80b4-00c04fd430c8'), 'v')", decodable: true},
		{name: "a map keyed by a date", column: "Map(Date, String)", value: "map(toDate('2024-05-06'), 'v')", decodable: true},
		{name: "a map keyed by low-cardinality text", column: "Map(LowCardinality(String), String)", value: "map('k', 'v')", decodable: true},
		{name: "a map keyed by fixed-length text", column: "Map(FixedString(2), String)", value: "map('ab', 'v')", decodable: true},
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			table := fmt.Sprintf("keyed_%d", index)
			native, err := clickhouse.Open(options)
			require.NoError(t, err)
			t.Cleanup(func() { _ = native.Close() })
			require.NoError(t, native.Exec(t.Context(), "CREATE TABLE "+table+" (id UInt8, v "+testCase.column+") ENGINE = Memory"))
			require.NoError(t, native.Exec(t.Context(), "INSERT INTO "+table+" VALUES (1, "+testCase.value+")"))

			standard := clickhouse.OpenDB(options)
			t.Cleanup(func() { _ = standard.Close() })
			nativeRows, nativeErr := native.Query(t.Context(), "SELECT v FROM "+table)
			if nativeErr == nil {
				_ = nativeRows.Close()
			}
			var standardValue any
			standardErr := standard.QueryRowContext(t.Context(), "SELECT v FROM "+table).Scan(&standardValue)

			require.Equal(t, testCase.decodable, nativeErr == nil, "the native driver decodes the column: %v", nativeErr)
			require.Equal(t, testCase.decodable, standardErr == nil, "database/sql decodes the column: %v", standardErr)
			require.Equal(t, !testCase.decodable, readRefused(t, testCase.column), "Seagull refuses reading the column")
		})
	}
}

func readRefused(t *testing.T, column string) bool {
	t.Helper()

	generator, err := seagull.New(dialect_engine_clickhouse.New())
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations":                  &fstest.MapFile{Mode: fs.ModeDir},
		"migrations/001_keyed.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE keyed (id UInt8, v " + column + ") ENGINE = MergeTree ORDER BY id;\n")},
		"queries/keyed.sql":           &fstest.MapFile{Data: []byte("-- sgl.query(Probe, many)\nSELECT v FROM keyed;\n")},
	}
	result, err := generator.Generate(t.Context(), seagull.Input{Package: "db", Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	for _, diagnostic := range result.Diagnostics {
		require.Equal(t, seagull.CodeEmitterUnsupported, diagnostic.Code, diagnostic.Message)
		return true
	}
	return false
}
