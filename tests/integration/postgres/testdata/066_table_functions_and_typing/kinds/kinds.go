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

// Package kinds holds the Go type a column override names for a bytea column.
package kinds

import (
	"database/sql/driver"
	"encoding/hex"
	"errors"
)

// Token is a four-byte identifier, as a UUID is a sixteen-byte one: it marshals itself to
// its bytes, while its driver.Valuer writes the text of its digits.
type Token [4]byte

// MarshalBinary returns the token's bytes.
func (token Token) MarshalBinary() ([]byte, error) {
	return token[:], nil
}

// Value returns the text of the token's digits.
func (token Token) Value() (driver.Value, error) {
	return hex.EncodeToString(token[:]), nil
}

// Scan reads the token from its bytes.
func (token *Token) Scan(source any) error {
	raw, isBytes := source.([]byte)
	if !isBytes || len(raw) != len(token) {
		return errors.New("a token is four bytes")
	}
	copy(token[:], raw)
	return nil
}
