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

package pgxrecorder

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit/emittest/internal/stubs/pgx"
)

type mood string

type moodPair struct {
	First mood
	Label string
}

func TestRecorderKeepsEveryStatementInOrder(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	tag, err := connection.Exec(ctx, "DELETE FROM items WHERE id = $1", int64(7))
	require.NoError(t, err)
	require.Zero(t, tag.RowsAffected())
	rows, err := connection.Query(ctx, "SELECT id FROM items WHERE id = ANY($1)", []int64{1, 2})
	require.NoError(t, err)
	require.False(t, rows.Next())
	rows.Close()
	var id int64
	require.True(t, errors.Is(connection.QueryRow(ctx, "SELECT id FROM items WHERE kind = $1", "a").Scan(&id), pgx.ErrNoRows))

	require.Equal(t, []Statement{
		{SQL: "DELETE FROM items WHERE id = $1", Arguments: []any{int64(7)}},
		{SQL: "SELECT id FROM items WHERE id = ANY($1)", Arguments: []any{[]int64{1, 2}}},
		{SQL: "SELECT id FROM items WHERE kind = $1", Arguments: []any{"a"}},
	}, Take())
	require.Empty(t, Take())
}

func TestRecorderRecordsEachStatementOfABatchAndEachRowCopied(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	transaction, err := connection.Begin(ctx)
	require.NoError(t, err)
	Take()

	batch := &pgx.Batch{}
	batch.Queue("INSERT INTO items (id) VALUES ($1)", int64(1))
	batch.Queue("INSERT INTO items (id) VALUES ($1)", int64(2))
	results := transaction.SendBatch(ctx, batch)
	_, err = results.Exec()
	require.NoError(t, err)
	require.NoError(t, results.Close())
	rows := [][]any{{int64(3), "a"}, {int64(4), "b"}}
	copied, err := transaction.CopyFrom(ctx, pgx.Identifier{"audit", "items"}, []string{"id", "kind"},
		pgx.CopyFromSlice(len(rows), func(index int) ([]any, error) { return rows[index], nil }))
	require.NoError(t, err)
	require.Equal(t, int64(2), copied)
	require.NoError(t, transaction.Commit(ctx))

	require.Equal(t, []Statement{
		{SQL: "INSERT INTO items (id) VALUES ($1)", Arguments: []any{int64(1)}},
		{SQL: "INSERT INTO items (id) VALUES ($1)", Arguments: []any{int64(2)}},
		{SQL: "INSERT INTO audit.items (id, kind) VALUES (\x001, \x002)", Arguments: []any{int64(3), "a"}},
		{SQL: "INSERT INTO audit.items (id, kind) VALUES (\x001, \x002)", Arguments: []any{int64(4), "b"}},
	}, Take())
}

func TestRecorderRefusesASliceOfANamedStringAsPgxDoes(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	_, err := connection.Exec(ctx, "DELETE FROM items WHERE mood = ANY($1)", []mood{"happy"})
	require.ErrorIs(t, err, errUnregisteredType)
	_, err = connection.Query(ctx, "SELECT id FROM items WHERE mood <> ALL($1)", &[]mood{"happy"})
	require.ErrorIs(t, err, errUnregisteredType)
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT id FROM items WHERE mood = ANY($1)", []mood{}).Scan(), errUnregisteredType)
	batch := &pgx.Batch{}
	batch.Queue("INSERT INTO items (moods) VALUES ($1)", []mood{"sad"})
	_, err = connection.SendBatch(ctx, batch).Exec()
	require.ErrorIs(t, err, errUnregisteredType)
	_, err = connection.CopyFrom(ctx, pgx.Identifier{"items"}, []string{"moods"},
		pgx.CopyFromSlice(1, func(int) ([]any, error) { return []any{[]mood{"sad"}}, nil }))
	require.ErrorIs(t, err, errUnregisteredType)
	var moods []mood
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT moods FROM items").Scan(&moods), errUnregisteredType)

	require.Equal(t, []Statement{{SQL: "SELECT moods FROM items", Arguments: nil}}, Take())
}

func TestRecorderPassesPlainStringsAndANamedStringAlone(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	_, err := connection.Exec(ctx, "UPDATE items SET mood = $1 WHERE label = ANY($2)", mood("sad"), []string{"a"})
	require.NoError(t, err)
	var labels []string
	var current mood
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT labels, mood FROM items").Scan(&labels, &current), pgx.ErrNoRows)

	require.Len(t, Take(), 2)
}

func TestRecorderRefusesANamedStringHeldInAnyContainerAsPgxDoes(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	happy := mood("happy")
	testCases := []struct {
		value any
		name  string
	}{
		{name: "a slice of pointers to one", value: []*mood{&happy}},
		{name: "a slice of slices of one", value: [][]mood{{happy}}},
		{name: "an array of one", value: [1]mood{happy}},
		{name: "a map whose values are one", value: map[string]mood{"a": happy}},
		{name: "a map whose keys are one", value: map[mood]string{happy: "a"}},
		{name: "a struct holding one", value: moodPair{First: happy, Label: "a"}},
	}
	for _, testCase := range testCases {
		_, err := connection.Exec(ctx, "UPDATE items SET moods = $1", testCase.value)
		require.ErrorIs(t, err, errUnregisteredType, testCase.name)
	}
	var pointers []*mood
	var labels map[string]mood
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT maybes FROM items").Scan(&pointers), errUnregisteredType)
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT labels FROM items").Scan(&labels), errUnregisteredType)

	require.Len(t, Take(), 2)
}

func TestRecorderPassesContainersOfPlainStrings(t *testing.T) {
	ctx := t.Context()
	connection := Connect()
	Take()

	for _, value := range []any{[]*string{nil}, [][]string{{"a"}}, map[string]string{"a": "b"}, &[]string{"a"}} {
		_, err := connection.Exec(ctx, "UPDATE items SET labels = $1", value)
		require.NoError(t, err)
	}
	var pointers []*string
	var labels map[string]string
	require.ErrorIs(t, connection.QueryRow(ctx, "SELECT maybes, labels FROM items").Scan(&pointers, &labels), pgx.ErrNoRows)

	require.Len(t, Take(), 5)
}
