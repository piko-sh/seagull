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
)

// sglDuckDBContextRows is how many rows an appender takes between checks of the context.
const sglDuckDBContextRows = 1024

// sglDuckDBAppender is what the batch reads of duckdb-go's *duckdb.Appender, which the
// generated method opens.
type sglDuckDBAppender interface {
	// AppendRow appends one row's values, in the appender's column order.
	//
	// Takes values (...driver.Value) which are the row's values.
	//
	// Returns error when the row cannot be appended.
	AppendRow(values ...driver.Value) error

	// Clear drops the rows appended since the last flush.
	//
	// Returns error when the rows cannot be dropped.
	Clear() error

	// Close flushes the appended rows and releases the appender.
	//
	// Returns error when the rows cannot be flushed.
	Close() error
}

// sglDuckDBAppend writes a batch's rows through DuckDB's appender.
//
// It uses a connection of its own taken from db, with one appender for each chunk of rows
// a multi-row INSERT would send, so each chunk is written whole or not at all, as its
// statement would be, and the rows of earlier chunks stay written when a later one fails.
// Columns the INSERT does not list take their defaults. When the driver's connection is
// not one DuckDB's appender opens on, or the first chunk's appender cannot be opened, it
// writes nothing and reports that, so the method sends its statements as it would without
// the appender. Cancellation is checked before each chunk and every sglDuckDBContextRows
// rows.
//
// Takes db (*sql.DB) which the connection is taken from.
// Takes rows (int) which is how many rows the batch writes.
// Takes chunk (int) which is how many rows one chunk holds.
// Takes open (func(driver.Conn) (sglDuckDBAppender, error)) which opens an appender on
// the driver's connection for the INSERT's table and columns.
// Takes appendRow (func(sglDuckDBAppender, driver.NamedValueChecker, int) error) which
// appends the row at an index.
//
// Returns appended (bool) which is false when nothing was appended and the statements are
// to be sent instead.
// Returns failed (int) which is the index of the first row of the chunk that failed.
// Returns err (error) when the connection cannot be had, a row cannot be appended, or a
// chunk cannot be written.
func sglDuckDBAppend(ctx context.Context, db *sql.DB, rows, chunk int, open func(driver.Conn) (sglDuckDBAppender, error),
	appendRow func(sglDuckDBAppender, driver.NamedValueChecker, int) error) (appended bool, failed int, err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return true, 0, err
	}
	defer func() { _ = conn.Close() }()
	for offset := 0; offset < rows; offset += chunk {
		end := min(offset+chunk, rows)
		opened := false
		if err := conn.Raw(func(driverConn any) error {
			var chunkErr error
			opened, chunkErr = sglDuckDBAppendChunk(ctx, driverConn, offset, end, open, appendRow)
			return chunkErr
		}); err != nil || !opened {

			if !opened && offset == 0 {
				return false, 0, nil
			}
			return true, offset, err
		}
	}
	return true, 0, nil
}

// sglDuckDBAppendChunk appends one chunk's rows and flushes them, or drops them all when
// a row cannot be appended or the context is done. Cancellation is checked every
// sglDuckDBContextRows rows.
//
// Takes driverConn (any) which is the driver's connection.
// Takes offset (int) which is the index of the chunk's first row.
// Takes end (int) which is the index past its last row.
// Takes open (func(driver.Conn) (sglDuckDBAppender, error)) which opens the appender.
// Takes appendRow (func(sglDuckDBAppender, driver.NamedValueChecker, int) error) which
// appends one row.
//
// Returns opened (bool) which is false when the driver's connection takes no appender.
// Returns err (error) when a row cannot be appended or the chunk cannot be flushed, or
// the appender cannot be opened.
func sglDuckDBAppendChunk(ctx context.Context, driverConn any, offset, end int, open func(driver.Conn) (sglDuckDBAppender, error),
	appendRow func(sglDuckDBAppender, driver.NamedValueChecker, int) error) (opened bool, err error) {
	raw, isConn := driverConn.(driver.Conn)
	checker, checks := driverConn.(driver.NamedValueChecker)
	if !isConn || !checks {
		return false, nil
	}
	appender, err := open(raw)
	if err != nil {
		return false, err
	}
	for index := offset; index < end; index++ {
		if (index-offset)%sglDuckDBContextRows == 0 {
			err = ctx.Err()
		}
		if err == nil {
			err = appendRow(appender, checker, index)
		}
		if err != nil {
			return true, errors.Join(err, appender.Clear(), appender.Close())
		}
	}
	return true, appender.Close()
}

// sglDuckDBAppendRow appends one row, converting each value as database/sql converts an
// argument for the driver. The driver's own check runs first, then database/sql's default
// conversion, which calls a driver.Valuer and reads through a pointer.
//
// Takes appender (sglDuckDBAppender) which receives the row.
// Takes checker (driver.NamedValueChecker) which is the driver's check of an argument.
// Takes values (...driver.Value) which are the row's values in appender column order.
//
// Returns error when a value cannot be converted or the row cannot be appended.
func sglDuckDBAppendRow(appender sglDuckDBAppender, checker driver.NamedValueChecker, values ...driver.Value) error {
	for index, value := range values {
		named := driver.NamedValue{Name: "", Ordinal: index + 1, Value: value}
		err := checker.CheckNamedValue(&named)
		if errors.Is(err, driver.ErrSkip) {
			named.Value, err = driver.DefaultParameterConverter.ConvertValue(value)
		}
		if err != nil {
			return err
		}
		values[index] = named.Value
	}
	return appender.AppendRow(values...)
}
