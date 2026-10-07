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

package source

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	staticQuery  = "SELECT id FROM boats WHERE id = #1"
	dynamicQuery = "SELECT id FROM boats WHERE name = #1"
)

var errFakePrepareRefused = errors.New("the fake database refused to prepare the statement")

type queryRunner interface {
	ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row
}

type fakeDatabase struct {
	refusedQuery string
	events       []string
	mu           sync.Mutex
}

func (database *fakeDatabase) record(format string, arguments ...any) {
	database.mu.Lock()
	defer database.mu.Unlock()
	database.events = append(database.events, fmt.Sprintf(format, arguments...))
}

func (database *fakeDatabase) recorded() []string {
	database.mu.Lock()
	defer database.mu.Unlock()
	return slices.Clone(database.events)
}

func (database *fakeDatabase) Connect(context.Context) (driver.Conn, error) {
	return &fakeConnection{database: database}, nil
}

func (database *fakeDatabase) Driver() driver.Driver {
	return fakeDriver{database: database}
}

type fakeDriver struct {
	database *fakeDatabase
}

func (fake fakeDriver) Open(string) (driver.Conn, error) {
	return &fakeConnection{database: fake.database}, nil
}

type fakeConnection struct {
	database *fakeDatabase
}

func (connection *fakeConnection) Prepare(query string) (driver.Stmt, error) {
	if query == connection.database.refusedQuery {
		return nil, errFakePrepareRefused
	}
	connection.database.record("prepare %s", query)
	return &fakeStatement{database: connection.database, query: query}, nil
}

func (*fakeConnection) Close() error {
	return nil
}

func (*fakeConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("the fake database begins transactions through BeginTx")
}

func (connection *fakeConnection) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	connection.database.record("begin read-only=%t", options.ReadOnly)
	return fakeTransaction{database: connection.database}, nil
}

func (connection *fakeConnection) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	connection.database.record("direct exec %s %v", query, namedValues(arguments))
	return driver.RowsAffected(1), nil
}

func (connection *fakeConnection) QueryContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	connection.database.record("direct query %s %v", query, namedValues(arguments))
	return &fakeRows{}, nil
}

func (connection *fakeConnection) Ping(context.Context) error {
	connection.database.record("ping")
	return nil
}

type fakeStatement struct {
	database *fakeDatabase
	query    string
}

func (statement *fakeStatement) Close() error {
	statement.database.record("close %s", statement.query)
	return nil
}

func (*fakeStatement) NumInput() int {
	return -1
}

func (statement *fakeStatement) Exec(arguments []driver.Value) (driver.Result, error) {
	statement.database.record("statement exec %s %v", statement.query, arguments)
	return driver.RowsAffected(1), nil
}

func (statement *fakeStatement) Query(arguments []driver.Value) (driver.Rows, error) {
	statement.database.record("statement query %s %v", statement.query, arguments)
	return &fakeRows{}, nil
}

type fakeTransaction struct {
	database *fakeDatabase
}

func (transaction fakeTransaction) Commit() error {
	transaction.database.record("commit")
	return nil
}

func (transaction fakeTransaction) Rollback() error {
	transaction.database.record("rollback")
	return nil
}

type fakeRows struct {
	served bool
}

func (*fakeRows) Columns() []string {
	return []string{"id"}
}

func (*fakeRows) Close() error {
	return nil
}

func (rows *fakeRows) Next(destination []driver.Value) error {
	if rows.served {
		return io.EOF
	}
	rows.served = true
	destination[0] = int64(1)
	return nil
}

func namedValues(arguments []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, 0, len(arguments))
	for _, argument := range arguments {
		values = append(values, argument.Value)
	}
	return values
}

func openFakeDatabase(t *testing.T, refusedQuery string) (*sql.DB, *fakeDatabase) {
	t.Helper()
	fake := &fakeDatabase{refusedQuery: refusedQuery}
	database := sql.OpenDB(fake)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		require.NoError(t, database.Close())
	})
	return database, fake
}

func prepareStatically(t *testing.T, database *sql.DB, queries ...string) *PreparedDBTX {
	t.Helper()
	statements := make(map[string]*sql.Stmt, len(queries))
	for _, query := range queries {
		statement, err := database.PrepareContext(t.Context(), query)
		require.NoError(t, err)
		statements[query] = statement
	}
	return &PreparedDBTX{db: database, stmts: statements}
}

func useStaticQueries(t *testing.T, queries ...string) {
	t.Helper()
	original := sglStaticQueries
	t.Cleanup(func() {
		sglStaticQueries = original
	})
	sglStaticQueries = queries
}

func runEveryRoute(t *testing.T, runner queryRunner, query string) {
	t.Helper()
	ctx := t.Context()

	result, err := runner.ExecContext(ctx, query, 7)
	require.NoError(t, err)
	affected, err := result.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)

	rows, err := runner.QueryContext(ctx, query, 8)
	require.NoError(t, err)
	require.True(t, rows.Next())
	require.NoError(t, rows.Close())

	var id int64
	require.NoError(t, runner.QueryRowContext(ctx, query, 9).Scan(&id))
	require.Equal(t, int64(1), id)
}

func TestPrepareWithNoStaticQueriesPreparesNothing(t *testing.T) {
	t.Parallel()

	database, fake := openFakeDatabase(t, "")

	prepared, err := Prepare(t.Context(), database)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, prepared.Close())
	})

	require.Same(t, database, prepared.UnderlyingDB())
	require.Empty(t, fake.recorded())
}

func TestPreparePreparesEveryStaticQueryOnce(t *testing.T) {
	useStaticQueries(t, staticQuery, "DELETE FROM boats WHERE id = #1")
	database, fake := openFakeDatabase(t, "")

	prepared, err := Prepare(t.Context(), database)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, prepared.Close())
	})
	runEveryRoute(t, prepared, staticQuery)

	require.Equal(t, []string{
		"prepare " + staticQuery,
		"prepare DELETE FROM boats WHERE id = #1",
		"statement exec " + staticQuery + " [7]",
		"statement query " + staticQuery + " [8]",
		"statement query " + staticQuery + " [9]",
	}, fake.recorded())
}

func TestPrepareClosesThePreparedStatementsWhenAStaticQueryFailsToPrepare(t *testing.T) {
	useStaticQueries(t, staticQuery, "SELECT broken")
	database, fake := openFakeDatabase(t, "SELECT broken")

	prepared, err := Prepare(t.Context(), database)

	require.ErrorIs(t, err, errFakePrepareRefused)
	require.ErrorContains(t, err, "preparing statement: ")
	require.Nil(t, prepared)
	require.Equal(t, []string{"prepare " + staticQuery, "close " + staticQuery}, fake.recorded())
}

func TestPrepareClosesEveryStatementPreparedBeforeTheOneThatFails(t *testing.T) {
	testCases := []struct {
		name    string
		queries []string
		closed  []string
	}{
		{name: "the first query fails", queries: []string{"SELECT broken", staticQuery}, closed: nil},
		{
			name:    "a later query fails",
			queries: []string{staticQuery, "DELETE FROM boats WHERE id = #1", "SELECT broken"},
			closed:  []string{"close " + staticQuery, "close DELETE FROM boats WHERE id = #1"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			useStaticQueries(t, testCase.queries...)
			database, fake := openFakeDatabase(t, "SELECT broken")

			prepared, err := Prepare(t.Context(), database)

			require.Nil(t, prepared)
			require.ErrorIs(t, err, errFakePrepareRefused)
			require.EqualError(t, err, "preparing statement: "+errFakePrepareRefused.Error())
			var closed []string
			for _, event := range fake.recorded() {
				if strings.HasPrefix(event, "close ") {
					closed = append(closed, event)
				}
			}
			require.ElementsMatch(t, testCase.closed, closed)
		})
	}
}

func TestPreparedDBTXCloseClosesEveryCachedStatementOnce(t *testing.T) {
	t.Parallel()

	database, fake := openFakeDatabase(t, "")
	prepared := prepareStatically(t, database, staticQuery, "DELETE FROM boats WHERE id = #1")

	require.NoError(t, prepared.Close())
	require.NoError(t, prepared.Close(), "a second close finds nothing left to close")

	require.ElementsMatch(t, []string{
		"prepare " + staticQuery, "prepare DELETE FROM boats WHERE id = #1",
		"close " + staticQuery, "close DELETE FROM boats WHERE id = #1",
	}, fake.recorded())
}

func TestPreparedDBTXRunsAStaticQueryThroughItsCachedStatement(t *testing.T) {
	t.Parallel()

	database, fake := openFakeDatabase(t, "")
	prepared := prepareStatically(t, database, staticQuery)

	runEveryRoute(t, prepared, staticQuery)

	require.Equal(t, []string{
		"prepare " + staticQuery,
		"statement exec " + staticQuery + " [7]",
		"statement query " + staticQuery + " [8]",
		"statement query " + staticQuery + " [9]",
	}, fake.recorded())
}

func TestPreparedDBTXRunsAnyOtherQueryDirectlyOnTheDatabase(t *testing.T) {
	t.Parallel()

	database, fake := openFakeDatabase(t, "")
	prepared := prepareStatically(t, database, staticQuery)

	runEveryRoute(t, prepared, dynamicQuery)

	require.Equal(t, []string{
		"prepare " + staticQuery,
		"direct exec " + dynamicQuery + " [7]",
		"direct query " + dynamicQuery + " [8]",
		"direct query " + dynamicQuery + " [9]",
	}, fake.recorded())
}

func TestPreparedDBTXRoutesQueriesInsideATransactionTheSameWay(t *testing.T) {
	t.Parallel()

	database, fake := openFakeDatabase(t, "")
	prepared := prepareStatically(t, database, staticQuery)

	transaction, err := prepared.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	bound, ok := prepared.WrapDBTX(transaction).(*preparedTx)
	require.True(t, ok)
	runEveryRoute(t, bound, staticQuery)
	runEveryRoute(t, bound, dynamicQuery)
	require.NoError(t, transaction.Commit())

	require.Equal(t, []string{
		"prepare " + staticQuery,
		"begin read-only=false",
		"statement exec " + staticQuery + " [7]",
		"statement query " + staticQuery + " [8]",
		"statement query " + staticQuery + " [9]",
		"direct exec " + dynamicQuery + " [7]",
		"direct query " + dynamicQuery + " [8]",
		"direct query " + dynamicQuery + " [9]",
		"commit",
	}, fake.recorded())
}

func TestPreparedDBTXWrapDBTXLeavesAnythingButATransactionAsItIs(t *testing.T) {
	t.Parallel()

	database, _ := openFakeDatabase(t, "")
	prepared := prepareStatically(t, database)

	wrappedDatabase, ok := prepared.WrapDBTX(database).(*sql.DB)
	require.True(t, ok)
	require.Same(t, database, wrappedDatabase)

	wrappedPrepared, ok := prepared.WrapDBTX(prepared).(*PreparedDBTX)
	require.True(t, ok)
	require.Same(t, prepared, wrappedPrepared)

	require.Nil(t, prepared.WrapDBTX(nil))
}

func TestPreparedDBTXBeginTxPassesItsOptionsToTheDatabase(t *testing.T) {
	t.Parallel()

	database, fake := openFakeDatabase(t, "")
	prepared := prepareStatically(t, database)

	transaction, err := prepared.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	require.NoError(t, transaction.Rollback())

	require.Equal(t, []string{"begin read-only=true", "rollback"}, fake.recorded())
}

func TestPreparedDBTXPingContextPingsTheDatabase(t *testing.T) {
	t.Parallel()

	database, fake := openFakeDatabase(t, "")
	prepared := prepareStatically(t, database)

	require.NoError(t, prepared.PingContext(t.Context()))

	require.Equal(t, []string{"ping"}, fake.recorded())
}

func TestPreparedDBTXCloseClosesItsStatementsAndRunsTheirQueriesDirectlyAfterwards(t *testing.T) {
	t.Parallel()

	database, fake := openFakeDatabase(t, "")
	prepared := prepareStatically(t, database, staticQuery)

	require.NoError(t, prepared.Close())
	_, err := prepared.ExecContext(t.Context(), staticQuery, 7)
	require.NoError(t, err)
	require.NoError(t, prepared.Close())

	require.Equal(t, []string{
		"prepare " + staticQuery,
		"close " + staticQuery,
		"direct exec " + staticQuery + " [7]",
	}, fake.recorded())
}

func TestPreparedTransactionPreservesDoneErrorsAfterWarmCalls(t *testing.T) {
	for _, commit := range []bool{false, true} {
		database, _ := openFakeDatabase(t, "")
		prepared := prepareStatically(t, database, staticQuery)
		transaction, err := prepared.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		bound, ok := prepared.WrapDBTX(transaction).(*preparedTx)
		require.True(t, ok)
		runEveryRoute(t, bound, staticQuery)
		if commit {
			require.NoError(t, transaction.Commit())
		} else {
			require.NoError(t, transaction.Rollback())
		}
		_, err = bound.ExecContext(t.Context(), staticQuery)
		require.ErrorIs(t, err, sql.ErrTxDone)
		_, err = bound.QueryContext(t.Context(), staticQuery)
		require.ErrorIs(t, err, sql.ErrTxDone)
		require.ErrorIs(t, bound.QueryRowContext(t.Context(), staticQuery).Err(), sql.ErrTxDone)
	}
}
