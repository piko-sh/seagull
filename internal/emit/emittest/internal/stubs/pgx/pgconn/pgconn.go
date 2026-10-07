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

// Package pgconn stands in for github.com/jackc/pgx/v5/pgconn in the runtime lane of
// emittest: it declares the command tag generated pgx code returns, and nothing else.
package pgconn

// CommandTag is the result of a statement: here only the number of rows it changed.
type CommandTag struct {
	// rows is the number of rows the statement changed.
	rows int64
}

// NewCommandTag returns the tag of a statement that changed a number of rows.
//
// Takes rows (int64) which is the number of rows changed.
//
// Returns CommandTag which is the tag.
func NewCommandTag(rows int64) CommandTag {
	return CommandTag{rows: rows}
}

// RowsAffected returns the number of rows the statement changed.
//
// Returns int64 which is the number.
func (tag CommandTag) RowsAffected() int64 {
	return tag.rows
}
