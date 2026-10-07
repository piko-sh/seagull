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

package rowscan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Close closes rows and joins any close failure into the caller's named error result.
//
// Takes rows (*sql.Rows) which is the result set to close.
// Takes err (*error) which is the caller's named error result that receives a close
// failure.
func Close(rows *sql.Rows, err *error) {
	if closeError := rows.Close(); closeError != nil {
		*err = errors.Join(*err, fmt.Errorf("closing rows: %w", closeError))
	}
}

// Strings runs a single-column text query and collects its values in row order.
//
// Takes database (*sql.DB) which is the connection pool to query.
// Takes query (string) which is the SQL selecting one text column per row.
// Takes arguments (...any) which are the positional query parameters.
//
// Returns values ([]string) which holds the scanned values in row order.
// Returns err (error) when the query, a scan, row iteration, or closing the rows fails.
func Strings(ctx context.Context, database *sql.DB, query string, arguments ...any) (values []string, err error) {
	rows, queryError := database.QueryContext(ctx, query, arguments...)
	if queryError != nil {
		return nil, queryError
	}
	defer Close(rows, &err)

	for rows.Next() {
		var value string
		if scanError := rows.Scan(&value); scanError != nil {
			return nil, scanError
		}
		values = append(values, value)
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, rowsError
	}
	return values, nil
}

// Stopped returns why ctx has ended: its error, joined by its cause when the cause says
// more, so the result matches both context.Canceled or context.DeadlineExceeded and the
// cause the caller gave.
//
// Returns error which is nil while ctx has not ended.
func Stopped(ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, err) {
		return fmt.Errorf("%w: %w", err, cause)
	}
	return err
}

// WithCause adds the cause of an ended ctx to a failure, so a query the context cut short
// says why it was cancelled, not only that it was.
//
// Takes failure (error) which is the failure to report.
//
// Returns error which is failure unchanged while ctx has not ended or when failure
// already carries the cause, and failure joined by the cause otherwise.
func WithCause(ctx context.Context, failure error) error {
	if failure == nil || ctx.Err() == nil {
		return failure
	}
	cause := context.Cause(ctx)
	if cause == nil || errors.Is(failure, cause) {
		return failure
	}
	return fmt.Errorf("%w: %w", failure, cause)
}
