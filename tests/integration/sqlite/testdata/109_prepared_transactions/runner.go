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

package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"

	"case_runner/db"
)

var (
	directRuns    atomic.Int64
	statementRuns atomic.Int64
	prepares      atomic.Int64
)

type countingDriver struct {
	inner driver.Driver
}

func (counting countingDriver) Open(name string) (driver.Conn, error) {
	conn, err := counting.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &countingConn{inner: conn}, nil
}

type countingConn struct {
	inner driver.Conn
}

func (conn *countingConn) Prepare(query string) (driver.Stmt, error) {
	return conn.PrepareContext(context.Background(), query)
}

func (conn *countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	statement, err := conn.inner.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	prepares.Add(1)
	return &countingStatement{inner: statement}, nil
}

func (conn *countingConn) Close() error {
	return conn.inner.Close()
}

func (conn *countingConn) Begin() (driver.Tx, error) {
	return conn.BeginTx(context.Background(), driver.TxOptions{})
}

func (conn *countingConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return conn.inner.(driver.ConnBeginTx).BeginTx(ctx, options)
}

func (conn *countingConn) ExecContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	directRuns.Add(1)
	return conn.inner.(driver.ExecerContext).ExecContext(ctx, query, arguments)
}

func (conn *countingConn) QueryContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	directRuns.Add(1)
	return conn.inner.(driver.QueryerContext).QueryContext(ctx, query, arguments)
}

type countingStatement struct {
	inner driver.Stmt
}

func (statement *countingStatement) Close() error {
	return statement.inner.Close()
}

func (statement *countingStatement) NumInput() int {
	return statement.inner.NumInput()
}

func (statement *countingStatement) Exec(arguments []driver.Value) (driver.Result, error) {
	statementRuns.Add(1)
	return statement.inner.Exec(arguments)
}

func (statement *countingStatement) Query(arguments []driver.Value) (driver.Rows, error) {
	statementRuns.Add(1)
	return statement.inner.Query(arguments)
}

func (statement *countingStatement) ExecContext(ctx context.Context, arguments []driver.NamedValue) (driver.Result, error) {
	statementRuns.Add(1)
	return statement.inner.(driver.StmtExecContext).ExecContext(ctx, arguments)
}

func (statement *countingStatement) QueryContext(ctx context.Context, arguments []driver.NamedValue) (driver.Rows, error) {
	statementRuns.Add(1)
	return statement.inner.(driver.StmtQueryContext).QueryContext(ctx, arguments)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	probe, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		return err
	}
	sql.Register("counting-sqlite", countingDriver{inner: probe.Driver()})
	if err := probe.Close(); err != nil {
		return err
	}

	conn, err := sql.Open("counting-sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL, qty INTEGER NOT NULL)`); err != nil {
		return err
	}

	prepared, err := db.Prepare(ctx, conn)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer prepared.Close()
	queries := db.New(prepared)

	directRuns.Store(0)
	statementRuns.Store(0)
	prepares.Store(0)

	var found db.GetItemRow
	var sorted []db.ListItemsSortedRow
	err = queries.RunInTx(ctx, conn, func(transaction *db.Queries) error {
		if err := transaction.AddItem(ctx, db.AddItemParams{Name: "rope", Qty: 3}); err != nil {
			return fmt.Errorf("add rope: %w", err)
		}
		if err := transaction.AddItem(ctx, db.AddItemParams{Name: "anchor", Qty: 1}); err != nil {
			return fmt.Errorf("add anchor: %w", err)
		}
		row, err := transaction.GetItem(ctx, "rope")
		if err != nil {
			return fmt.Errorf("get rope: %w", err)
		}
		found = row
		sorted, err = transaction.ListItemsSorted(ctx, db.ListItemsSortedParams{
			OrderBy:          db.ListItemsSortedOrderByName,
			OrderByDirection: db.OrderAsc,
		})
		if err != nil {
			return fmt.Errorf("list sorted: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("transaction: %w", err)
	}

	inTransaction := map[string]int64{
		"direct_runs":    directRuns.Load(),
		"statement_runs": statementRuns.Load(),
		"new_prepares":   prepares.Load(),
	}

	committed, err := queries.GetItem(ctx, "anchor")
	if err != nil {
		return fmt.Errorf("get anchor after commit: %w", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"in_transaction": inTransaction,
		"found":          found,
		"sorted":         sorted,
		"committed":      committed,
	})
}
