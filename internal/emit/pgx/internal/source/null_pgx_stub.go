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

//go:build ignore

package source

import (
	"database/sql"
	"database/sql/driver"
)

// Null stands in for the generated Null type, which the shared helpers declare, so the
// pgx methods compile on their own.
type Null[T any] struct {
	// V is the value.
	V T

	// Valid is false when the value is SQL NULL.
	Valid bool
}

// Scan stands in for the generated Null type's Scan.
//
// Takes source (any) which is the driver value.
//
// Returns error when the value cannot be stored in T.
func (n *Null[T]) Scan(source any) error {
	var held sql.Null[T]
	if err := held.Scan(source); err != nil {
		return err
	}
	*n = Null[T]{V: held.V, Valid: held.Valid}
	return nil
}

// Value stands in for the generated Null type's Value.
//
// Returns driver.Value which is the value, or nil for NULL.
// Returns error when the value cannot be written.
func (n Null[T]) Value() (driver.Value, error) {
	return sql.Null[T]{V: n.V, Valid: n.Valid}.Value()
}
