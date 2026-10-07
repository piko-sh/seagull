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

package chrecorder

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit/emittest/internal/stubs/clickhouse"
	"seagull.sh/seagull/internal/emit/emittest/internal/stubs/clickhouse/lib/chcol"
)

type mood string

type scannedMood string

func (value *scannedMood) Scan(source any) error {
	if text, ok := source.(string); ok {
		*value = scannedMood(text)
	}
	return nil
}

type moodPair struct {
	First mood
	Label string
}

type (
	rank  int64
	flag  bool
	bytes []byte
)

type document []byte

func (read *document) Scan(source any) error {
	text, isText := source.(string)
	if !isText {
		return errors.New("not text")
	}
	*read = document(text)
	return nil
}

func (read *document) UnmarshalJSON(data []byte) error {
	*read = append(document(nil), data...)
	return nil
}

type documentReader struct {
	read *document
}

func (*documentReader) Scan(any) error {
	return nil
}

func (*documentReader) DeserializeClickHouseJSON(*chcol.JSON) error {
	return nil
}

func TestRecorderKeepsEveryStatementWithItsNamedValues(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	require.NoError(t, connection.Exec(ctx, "DELETE FROM items WHERE id = {p_id:UInt64}", clickhouse.Named("p_id", "7")))
	rows, err := connection.Query(ctx, "SELECT id FROM items WHERE kind = $1", "a")
	require.NoError(t, err)
	require.False(t, rows.Next())
	require.NoError(t, rows.Close())
	var id uint64
	require.True(t, errors.Is(connection.QueryRow(ctx, "SELECT id FROM items LIMIT 1").Scan(&id), sql.ErrNoRows))

	require.Equal(t, []Statement{
		{SQL: "DELETE FROM items WHERE id = {p_id:UInt64}", Arguments: []any{sql.Named("p_id", "7")}},
		{SQL: "SELECT id FROM items WHERE kind = $1", Arguments: []any{"a"}},
		{SQL: "SELECT id FROM items LIMIT 1", Arguments: []any{}},
	}, Take())
}

func TestRecorderRecordsEachRowABatchSends(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	batch, err := connection.PrepareBatch(ctx, "INSERT INTO items (id, kind)")
	require.NoError(t, err)
	require.NoError(t, batch.Append(uint64(1), "a"))
	require.NoError(t, batch.Append(uint64(2), "b"))
	require.NoError(t, batch.Send())
	require.NoError(t, batch.Close())

	require.Equal(t, []Statement{
		{SQL: "INSERT INTO items (id, kind)", Arguments: []any{uint64(1), "a"}},
		{SQL: "INSERT INTO items (id, kind)", Arguments: []any{uint64(2), "b"}},
	}, Take())
}

func TestRecorderRefusesANamedStringAsClickHouseGoDoes(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	batch, err := connection.PrepareBatch(ctx, "INSERT INTO items (id, mood)")
	require.NoError(t, err)
	happy := mood("happy")
	for _, value := range []any{happy, &happy, []mood{"sad"}} {
		require.ErrorIs(t, batch.Append(uint64(1), value), errNamedString)
	}
	require.NoError(t, batch.Append(uint64(1), "happy"))
	require.NoError(t, batch.Append(uint64(2), &[]string{"sad"}[0]))
	require.NoError(t, batch.Send())

	var current mood
	var optional *mood
	var scanned scannedMood
	var text string
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT mood FROM items").Scan(&current), errNamedString)
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT mood FROM items").Scan(&optional), errNamedString)
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT mood, label FROM items").Scan(&scanned, &text), sql.ErrNoRows)
	rows, err := connection.Query(ctx, "SELECT mood FROM items")
	require.NoError(t, err)
	require.ErrorIs(t, rows.Scan(&current), errNamedString)

	require.Len(t, Take(), 6)
}

func TestRecorderNamesTheValuesOfARowAfterTheInsertsPlaceholders(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	batch, err := connection.PrepareBatch(ctx, "INSERT INTO items (id, kind) VALUES ({p_id:UInt64}, {p_kind:String})")
	require.NoError(t, err)
	require.NoError(t, batch.Append(uint64(1), "a"))
	require.NoError(t, batch.Send())

	require.Equal(t, []Statement{{
		SQL:       "INSERT INTO items (id, kind) VALUES ({p_id:UInt64}, {p_kind:String})",
		Arguments: []any{sql.Named("p_id", uint64(1)), sql.Named("p_kind", "a")},
	}}, Take())
}

func TestRecorderRefusesToAppendANamedStringHeldAnywhereAsClickHouseGoDoes(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	batch, err := connection.PrepareBatch(ctx, "INSERT INTO items (id, moods)")
	require.NoError(t, err)
	happy := mood("happy")
	testCases := []struct {
		value any
		name  string
	}{
		{name: "a slice of pointers to one", value: []*mood{&happy}},
		{name: "a slice of slices of one", value: [][]mood{{happy}}},
		{name: "a map whose values are one", value: map[string]mood{"a": happy}},
		{name: "a map whose keys are one", value: map[mood]string{happy: "a"}},
		{name: "a tuple holding one", value: moodPair{First: happy, Label: "a"}},
	}
	for _, testCase := range testCases {
		require.ErrorIs(t, batch.Append(uint64(1), testCase.value), errNamedString, testCase.name)
	}
	require.NoError(t, batch.Append(uint64(1), []*string{nil}, map[string]string{"a": "b"}, [][]string{{"a"}}))
	require.NoError(t, batch.Send())

	require.Len(t, Take(), 1)
}

func TestRecorderScansArraysAndTuplesOfANamedStringButNoMapAsClickHouseGoDoes(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	var moods []mood
	var maybes []*mood
	var grid [][]mood
	var pair moodPair
	var labels map[string]mood
	var keyed map[mood]string
	var lists map[string][]mood
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT moods, maybes, grid, pair FROM items").Scan(&moods, &maybes, &grid, &pair), sql.ErrNoRows)
	for _, destination := range []any{&labels, &keyed, &lists} {
		require.ErrorIs(t, connection.QueryRow(ctx, "SELECT labels FROM items").Scan(destination), errNamedString)
	}

	require.Len(t, Take(), 4)
}

func TestRecorderRefusesADefinedTypeOverABasicKindAsClickHouseGoDoes(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	batch, err := connection.PrepareBatch(ctx, "INSERT INTO items (id, value)")
	require.NoError(t, err)
	third := rank(3)
	testCases := []struct {
		value any
		name  string
	}{
		{name: "a defined boolean", value: flag(true)},
		{name: "a defined byte slice", value: bytes("raw")},
		{name: "a pointer to a defined integer", value: &third},
	}
	for _, testCase := range testCases {
		require.ErrorIs(t, batch.Append(uint64(1), testCase.value), errDefinedType, testCase.name)
	}
	require.NoError(t, batch.Append(uint64(1), third), "the driver converts a defined number it is handed by value")
	require.NoError(t, batch.Append(uint64(1), json.RawMessage(`{}`), net.IP{10, 0, 0, 1}), "the driver appends these itself")
	require.NoError(t, batch.Send())

	var counted rank
	var optional *flag
	var raw bytes
	var ranks []rank
	for _, destination := range []any{&counted, &optional, &raw} {
		require.ErrorIs(t, connection.QueryRow(ctx, "SELECT value FROM items").Scan(destination), errDefinedType)
	}
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT ranks FROM items").Scan(&ranks), sql.ErrNoRows)

	require.Len(t, Take(), 6)
}

func TestRecorderRefusesAJSONDocumentTypeTheDriverCannotFillAsClickHouseGoDoes(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	var data document
	var optional *document
	var raw json.RawMessage
	var documents []document
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT data FROM documents").Scan(&data), errJSONDestination)
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT data FROM documents").Scan(&optional), errJSONDestination)
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT data FROM documents").Scan(&documentReader{read: &data}), sql.ErrNoRows,
		"the driver hands a JSON column to a chcol.JSONDeserializer")
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT label FROM documents").Scan(&raw), sql.ErrNoRows,
		"a json.RawMessage reads a String column")
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT data FROM documents").Scan(&documents), sql.ErrNoRows,
		"the driver fills an array of documents element by element")

	require.Len(t, Take(), 5)
}
