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

// Package duckdb provides an appender stand-in for generated runtime tests.
//
// The appender never opens on the recording driver's connection, so DuckDB batch methods
// send SQL statements that the recorder can inspect.
package duckdb

import (
	"database/sql/driver"
	"errors"
)

// errNoAppender reports that the stand-in opens no appender.
var errNoAppender = errors.New("duckdb stand-in: no appender on this connection")

// Appender stands in for duckdb-go's appender.
type Appender struct{}

// NewAppenderWithColumns refuses to open an appender.
//
// Returns *Appender which is nil.
// Returns error which is always errNoAppender.
func NewAppenderWithColumns(_ driver.Conn, _, _, _ string, _ []string) (*Appender, error) {
	return nil, errNoAppender
}

// AppendRow appends nothing.
//
// Returns error which is always errNoAppender.
func (*Appender) AppendRow(_ ...driver.Value) error {
	return errNoAppender
}

// Clear drops nothing.
//
// Returns error which is always nil.
func (*Appender) Clear() error {
	return nil
}

// Close releases nothing.
//
// Returns error which is always nil.
func (*Appender) Close() error {
	return nil
}
