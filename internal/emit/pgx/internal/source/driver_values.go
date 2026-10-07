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
	"database/sql/driver"
	"encoding"
	"reflect"
)

// sglDriverBinary adapts an overridden parameter to its binary encoding.
//
// It hands pgx a binary parameter whose Go type an override chose: the bytes the value
// marshals itself to when it implements encoding.BinaryMarshaler, as a UUID marshals into
// its 16 bytes, rather than the text its driver.Valuer writes, which pgx would otherwise
// store in a bytea column; a nil pointer as NULL; and any other value as pgx binds it.
type sglDriverBinary struct {
	// value is the original parameter wrapped for driver conversion.
	value any
}

// Value returns the driver value of the wrapped parameter.
//
// Returns driver.Value which is the adapted parameter value.
// Returns error when the parameter cannot be converted or encoded.
func (wrapped sglDriverBinary) Value() (driver.Value, error) {
	if wrapped.value == nil {
		return nil, nil
	}
	if reflected := reflect.ValueOf(wrapped.value); reflected.Kind() == reflect.Pointer && reflected.IsNil() {
		return nil, nil
	}
	if marshaler, marshals := wrapped.value.(encoding.BinaryMarshaler); marshals {
		return marshaler.MarshalBinary()
	}
	return wrapped.value, nil
}
