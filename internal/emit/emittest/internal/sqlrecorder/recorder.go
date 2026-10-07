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

// Package sqlrecorder is the runtime harness emittest builds code written against
// database/sql with: a database/sql driver that records every statement and the arguments
// it receives, answers every query with no rows, and reports no rows changed.
//
// emittest compiles this file into the program it builds, as package main, so it uses
// only the standard library and declares what the harness contract asks for: Statement,
// Connect and Take.
package sqlrecorder

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"sync"
)

// Statement is one statement the driver received: its SQL and its arguments, each as the
// generated code passed it, a named one as a sql.NamedArg.
type Statement struct {
	// SQL is the statement's text.
	SQL string

	// Arguments holds the arguments in order.
	Arguments []any
}

var (
	// recordedMutex guards recorded.
	recordedMutex sync.Mutex

	// recorded holds the statements received since the last Take.
	recorded []Statement
)

// recordingDriver is the database/sql driver that records statements.
type recordingDriver struct{}

// Open opens a recording connection.
//
// Returns driver.Conn which is the connection.
// Returns error which is always nil.
func (recordingDriver) Open(_ string) (driver.Conn, error) {
	return connection{}, nil
}

// connection is one connection of the recording driver.
type connection struct{}

// Prepare prepares a statement, which records what it is run with.
//
// Takes query (string) which is the statement's text.
//
// Returns driver.Stmt which is the statement.
// Returns error which is always nil.
func (connection) Prepare(query string) (driver.Stmt, error) {
	return preparedStatement{query: query}, nil
}

// Close closes the connection.
//
// Returns error which is always nil.
func (connection) Close() error {
	return nil
}

// Begin starts a transaction that does nothing.
//
// Returns driver.Tx which is the transaction.
// Returns error which is always nil.
func (connection) Begin() (driver.Tx, error) {
	return transaction{}, nil
}

// BeginTx starts a transaction that does nothing.
//
// Returns driver.Tx which is the transaction.
// Returns error which is always nil.
func (connection) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return transaction{}, nil
}

// QueryContext records a query and answers it with no rows.
//
// Takes query (string) which is the statement's text.
// Takes arguments ([]driver.NamedValue) which are its arguments.
//
// Returns driver.Rows which holds no rows.
// Returns error which is always nil.
func (connection) QueryContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	record(query, arguments)
	return emptyRows{}, nil
}

// ExecContext records a statement that changes nothing.
//
// Takes query (string) which is the statement's text.
// Takes arguments ([]driver.NamedValue) which are its arguments.
//
// Returns driver.Result which reports no change.
// Returns error which is always nil.
func (connection) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	record(query, arguments)
	return noChange{}, nil
}

// CheckNamedValue accepts every argument as it is, so the record holds what the generated
// code passed.
//
// Returns error which is always nil.
func (connection) CheckNamedValue(_ *driver.NamedValue) error {
	return nil
}

// preparedStatement is a statement prepared on a connection.
type preparedStatement struct {
	// query is the statement's text.
	query string
}

// Close closes the statement.
//
// Returns error which is always nil.
func (preparedStatement) Close() error {
	return nil
}

// NumInput reports that the statement takes any number of arguments.
//
// Returns int which is -1.
func (preparedStatement) NumInput() int {
	return -1
}

// Exec records the statement run with positional arguments.
//
// Takes arguments ([]driver.Value) which are its arguments.
//
// Returns driver.Result which reports no change.
// Returns error which is always nil.
func (statement preparedStatement) Exec(arguments []driver.Value) (driver.Result, error) {
	record(statement.query, namedValues(arguments))
	return noChange{}, nil
}

// Query records the statement run with positional arguments and answers it with no rows.
//
// Takes arguments ([]driver.Value) which are its arguments.
//
// Returns driver.Rows which holds no rows.
// Returns error which is always nil.
func (statement preparedStatement) Query(arguments []driver.Value) (driver.Rows, error) {
	record(statement.query, namedValues(arguments))
	return emptyRows{}, nil
}

// ExecContext records the statement run.
//
// Takes arguments ([]driver.NamedValue) which are its arguments.
//
// Returns driver.Result which reports no change.
// Returns error which is always nil.
func (statement preparedStatement) ExecContext(_ context.Context, arguments []driver.NamedValue) (driver.Result, error) {
	record(statement.query, arguments)
	return noChange{}, nil
}

// QueryContext records the statement run and answers it with no rows.
//
// Takes arguments ([]driver.NamedValue) which are its arguments.
//
// Returns driver.Rows which holds no rows.
// Returns error which is always nil.
func (statement preparedStatement) QueryContext(_ context.Context, arguments []driver.NamedValue) (driver.Rows, error) {
	record(statement.query, arguments)
	return emptyRows{}, nil
}

// CheckNamedValue accepts every argument as it is.
//
// Returns error which is always nil.
func (preparedStatement) CheckNamedValue(_ *driver.NamedValue) error {
	return nil
}

// emptyRows is a result set with no columns and no rows.
type emptyRows struct{}

// Columns reports no columns.
//
// Returns []string which is empty.
func (emptyRows) Columns() []string {
	return nil
}

// Close closes the result set.
//
// Returns error which is always nil.
func (emptyRows) Close() error {
	return nil
}

// Next reports the end of the result set.
//
// Returns error which is always io.EOF.
func (emptyRows) Next(_ []driver.Value) error {
	return io.EOF
}

// noChange is the result of a statement that changed nothing.
type noChange struct{}

// LastInsertId reports no inserted row.
//
// Returns int64 which is zero.
// Returns error which is always nil.
func (noChange) LastInsertId() (int64, error) {
	return 0, nil
}

// RowsAffected reports no changed rows.
//
// Returns int64 which is zero.
// Returns error which is always nil.
func (noChange) RowsAffected() (int64, error) {
	return 0, nil
}

// transaction is a transaction that does nothing.
type transaction struct{}

// Commit ends the transaction.
//
// Returns error which is always nil.
func (transaction) Commit() error {
	return nil
}

// Rollback ends the transaction.
//
// Returns error which is always nil.
func (transaction) Rollback() error {
	return nil
}

// connector opens recording connections.
type connector struct{}

// Connect opens a recording connection.
//
// Returns driver.Conn which is the connection.
// Returns error which is always nil.
func (connector) Connect(context.Context) (driver.Conn, error) {
	return connection{}, nil
}

// Driver returns the recording driver.
//
// Returns driver.Driver which is the driver.
func (connector) Driver() driver.Driver {
	return recordingDriver{}
}

// Connect opens a database handle on the recording driver.
//
// Returns *sql.DB which records every statement run on it.
func Connect() *sql.DB {
	return sql.OpenDB(connector{})
}

// Take returns the statements received since the last call, in order, and forgets them.
//
// Returns []Statement which holds the statements.
//
// Concurrency: atomically drains the shared recording under its mutex.
func Take() []Statement {
	recordedMutex.Lock()
	defer recordedMutex.Unlock()
	taken := recorded
	recorded = nil
	return taken
}

// record keeps one statement.
//
// Takes query (string) which is the statement's text.
// Takes arguments ([]driver.NamedValue) which are its arguments.
//
// Concurrency: appends to the shared recording under its mutex.
func record(query string, arguments []driver.NamedValue) {
	values := make([]any, 0, len(arguments))
	for _, argument := range arguments {
		if argument.Name != "" {
			values = append(values, sql.Named(argument.Name, argument.Value))
			continue
		}
		values = append(values, argument.Value)
	}
	recordedMutex.Lock()
	defer recordedMutex.Unlock()
	recorded = append(recorded, Statement{SQL: query, Arguments: values})
}

// namedValues numbers positional arguments.
//
// Takes arguments ([]driver.Value) which are the arguments.
//
// Returns []driver.NamedValue which holds them with their ordinals.
func namedValues(arguments []driver.Value) []driver.NamedValue {
	named := make([]driver.NamedValue, 0, len(arguments))
	for index, argument := range arguments {
		named = append(named, driver.NamedValue{Name: "", Ordinal: index + 1, Value: argument})
	}
	return named
}
