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
	"errors"
	"fmt"
	"sync"
)

// sglStaticQueries lists the SQL constants of the package's static queries.
//
//sgl:inline staticQueries
var sglStaticQueries = []string{}

// PreparedDBTX holds prepared queries over an application-owned connection pool.
type PreparedDBTX struct {
	// db is the application-owned connection pool.
	db *sql.DB

	// stmts maps static SQL text to statements owned by this wrapper.
	stmts map[string]*sql.Stmt

	// mu protects access to the prepared statement map.
	mu sync.RWMutex
}

// ExecContext executes SQL using a cached statement when available.
//
// Takes query (string) which is the SQL statement to execute.
// Takes arguments (...any) which holds its bound parameter values.
//
// Returns sql.Result which describes the completed statement.
// Returns error when preparation, binding or execution fails.
func (prepared *PreparedDBTX) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	statement := prepared.cachedStatement(query)
	if statement == nil {
		return prepared.db.ExecContext(ctx, query, arguments...)
	}
	return statement.ExecContext(ctx, arguments...)
}

// QueryContext queries rows using a cached statement when available.
//
// Takes query (string) which is the SQL statement to query.
// Takes arguments (...any) which holds its bound parameter values.
//
// Returns *sql.Rows which holds the result rows and must be closed by the caller.
// Returns error when preparation, binding or querying fails.
func (prepared *PreparedDBTX) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	statement := prepared.cachedStatement(query)
	if statement == nil {
		return prepared.db.QueryContext(ctx, query, arguments...)
	}
	return statement.QueryContext(ctx, arguments...)
}

// QueryRowContext queries one row using a cached statement when available.
//
// Takes query (string) which is the SQL statement to query.
// Takes arguments (...any) which holds its bound parameter values.
//
// Returns *sql.Row which reports the result or any query error when scanned.
func (prepared *PreparedDBTX) QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row {
	statement := prepared.cachedStatement(query)
	if statement == nil {
		return prepared.db.QueryRowContext(ctx, query, arguments...)
	}
	return statement.QueryRowContext(ctx, arguments...)
}

// BeginTx starts a transaction on the underlying connection pool.
//
// Takes options (*sql.TxOptions) which controls isolation and read-only mode, or is nil
// for defaults.
//
// Returns *sql.Tx which is the transaction the caller must commit or roll back.
// Returns error when the connection cannot start the transaction.
func (prepared *PreparedDBTX) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	return prepared.db.BeginTx(ctx, options)
}

// PingContext checks connectivity through the underlying connection pool.
//
// Returns error when a connection cannot be established or verified.
func (prepared *PreparedDBTX) PingContext(ctx context.Context) error {
	return prepared.db.PingContext(ctx)
}

// UnderlyingDB returns the connection pool supplied to Prepare.
//
// Returns *sql.DB which is the application-owned connection pool.
func (prepared *PreparedDBTX) UnderlyingDB() *sql.DB {
	return prepared.db
}

// Close closes the cached statements and leaves the connection pool open.
//
// Returns error which joins statement-closing errors, or is nil on success.
//
// Concurrency: The cache write lock serialises closing with statement lookup.
func (prepared *PreparedDBTX) Close() error {
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	err := sglCloseStatements(prepared.stmts)
	prepared.stmts = make(map[string]*sql.Stmt)
	return err
}

// WrapDBTX adapts transactions to use the cached prepared statements.
//
// Takes inner (any) which is the database or transaction to adapt.
//
// Returns any which wraps a transaction, or is inner unchanged for another type.
func (prepared *PreparedDBTX) WrapDBTX(inner any) any {
	transaction, ok := inner.(*sql.Tx)
	if !ok {
		return inner
	}
	return &preparedTx{source: prepared, transaction: transaction}
}

// cachedStatement looks up a prepared statement under the cache read lock.
//
// Takes query (string) which is the SQL text used as the cache key.
//
// Returns *sql.Stmt which is the cached statement, or nil when absent.
//
// Concurrency: The cache read lock synchronises lookup with Close.
func (prepared *PreparedDBTX) cachedStatement(query string) *sql.Stmt {
	prepared.mu.RLock()
	defer prepared.mu.RUnlock()
	return prepared.stmts[query]
}

// preparedTx binds cached statements to an application-owned transaction.
type preparedTx struct {
	// source owns the cached statements used by this transaction.
	source *PreparedDBTX

	// transaction is the application-owned transaction to execute through.
	transaction *sql.Tx
}

// ExecContext executes SQL using a cached statement when available.
//
// Takes query (string) which is the SQL statement to execute.
// Takes arguments (...any) which holds its bound parameter values.
//
// Returns sql.Result which describes the completed statement.
// Returns error when preparation, binding or execution fails.
func (bound *preparedTx) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	statement := bound.source.cachedStatement(query)
	if statement == nil {
		return bound.transaction.ExecContext(ctx, query, arguments...)
	}
	return bound.transaction.StmtContext(ctx, statement).ExecContext(ctx, arguments...)
}

// QueryContext queries rows using a cached statement when available.
//
// Takes query (string) which is the SQL statement to query.
// Takes arguments (...any) which holds its bound parameter values.
//
// Returns *sql.Rows which holds the result rows and must be closed by the caller.
// Returns error when preparation, binding or querying fails.
func (bound *preparedTx) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	statement := bound.source.cachedStatement(query)
	if statement == nil {
		return bound.transaction.QueryContext(ctx, query, arguments...)
	}
	return bound.transaction.StmtContext(ctx, statement).QueryContext(ctx, arguments...)
}

// QueryRowContext queries one row using a cached statement when available.
//
// Takes query (string) which is the SQL statement to query.
// Takes arguments (...any) which holds its bound parameter values.
//
// Returns *sql.Row which reports the result or any query error when scanned.
func (bound *preparedTx) QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row {
	statement := bound.source.cachedStatement(query)
	if statement == nil {
		return bound.transaction.QueryRowContext(ctx, query, arguments...)
	}
	return bound.transaction.StmtContext(ctx, statement).QueryRowContext(ctx, arguments...)
}

// Prepare prepares the package's static queries on an existing connection pool.
//
// Takes db (*sql.DB) which is the application-owned connection pool.
//
// Returns *PreparedDBTX which owns the prepared statements and retains the supplied pool.
// Returns error when a static query cannot be prepared, joined with any error closing the
// statements prepared before it.
func Prepare(ctx context.Context, db *sql.DB) (*PreparedDBTX, error) {
	staticQueries := sglStaticQueries
	stmts := make(map[string]*sql.Stmt, len(staticQueries))
	for _, query := range staticQueries {
		statement, err := db.PrepareContext(ctx, query)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("preparing statement: %w", err), sglCloseStatements(stmts))
		}
		stmts[query] = statement
	}
	return &PreparedDBTX{db: db, stmts: stmts, mu: sync.RWMutex{}}, nil
}

// sglCloseStatements closes every statement of a cache.
//
// Takes stmts (map[string]*sql.Stmt) which maps static SQL text to its statement.
//
// Returns error which joins the statements' closing errors, or is nil on success.
func sglCloseStatements(stmts map[string]*sql.Stmt) error {
	var closeErrors []error
	for _, statement := range stmts {
		if err := statement.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}
