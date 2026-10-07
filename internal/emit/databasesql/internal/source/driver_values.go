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
	"encoding/json"
	"reflect"
)

// sglDriverValue hands database/sql a parameter through its driver.Valuer, for a driver
// that would otherwise bind the parameter's own type as it is.
//
//sgl:when value
type sglDriverValue struct {
	// value is the original parameter wrapped for driver conversion.
	value any
}

// Value returns the driver value of the wrapped parameter.
//
// Returns driver.Value which is the adapted parameter value.
// Returns error when the parameter cannot be converted or encoded.
//
//sgl:when value
func (wrapped sglDriverValue) Value() (driver.Value, error) {
	return driver.DefaultParameterConverter.ConvertValue(wrapped.value)
}

// sglDriverText hands database/sql a text parameter whose Go type an override chose, for
// a driver that binds a []byte as a binary value whatever the parameter's type: its value
// converted as database/sql converts one, with bytes, a json.RawMessage's for instance,
// bound as the text they hold.
//
//sgl:when text
type sglDriverText struct {
	// value is the original parameter wrapped for driver conversion.
	value any
}

// Value returns the driver value of the wrapped parameter, bytes as text.
//
// Returns driver.Value which is the adapted parameter value.
// Returns error when the parameter cannot be converted or encoded.
//
//sgl:when text
func (wrapped sglDriverText) Value() (driver.Value, error) {
	converted, err := driver.DefaultParameterConverter.ConvertValue(wrapped.value)
	if text, isBytes := converted.([]byte); isBytes && err == nil {
		return string(text), nil
	}
	return converted, err
}

// sglDriverBinary adapts an overridden parameter to its binary encoding.
//
// It hands database/sql a binary parameter whose Go type an override chose: the bytes the
// value marshals itself to when it implements encoding.BinaryMarshaler, as a UUID
// marshals into the 16 bytes a BINARY(16) column holds, rather than the text its
// driver.Valuer writes; a nil pointer as NULL; and any other value converted as
// database/sql converts one, or, where it converts none, as the driver binds it.
//
//sgl:when binary
type sglDriverBinary struct {
	// value is the original parameter wrapped for driver conversion.
	value any
}

// Value returns the driver value of the wrapped parameter.
//
// Returns driver.Value which is the adapted parameter value.
// Returns error when the parameter cannot be converted or encoded.
//
//sgl:when binary
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
	converted, err := driver.DefaultParameterConverter.ConvertValue(wrapped.value)
	if err != nil {
		return wrapped.value, nil
	}
	return converted, nil
}

// sglDriverJSON adapts an overridden parameter to its JSON encoding.
//
// It hands database/sql a JSON parameter whose Go type an override chose as the document
// it encodes to, as pgx binds one, for a driver that binds only its own basic types:
// through the value's own driver.Valuer when it has one, as NULL for a nil pointer,
// slice, map or interface, and otherwise as the text of its JSON encoding.
//
//sgl:when json
type sglDriverJSON struct {
	// value is the original parameter wrapped for driver conversion.
	value any
}

// Value returns the driver value of the wrapped parameter.
//
// Returns driver.Value which is the adapted parameter value.
// Returns error when the parameter cannot be converted or encoded.
//
//sgl:when json
func (wrapped sglDriverJSON) Value() (driver.Value, error) {
	if wrapped.value == nil {
		return nil, nil
	}
	reflected := reflect.ValueOf(wrapped.value)
	switch reflected.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		if reflected.IsNil() {
			return nil, nil
		}
	}
	if valuer, values := wrapped.value.(driver.Valuer); values {
		return valuer.Value()
	}
	encoded, err := json.Marshal(wrapped.value)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

// sglDriverDecimalValue converts decimal pointers through their driver.Valuer.
//
// It hands database/sql a decimal parameter held by a pointer whose type has a Decompose
// method, such as *apd.Decimal, which database/sql hands the driver as it is and which a
// driver refuses, or binds as NULL whatever its value: a nil pointer as NULL, and any
// other the value its driver.Valuer writes, as a decimal held by value binds.
//
//sgl:when decimal
type sglDriverDecimalValue struct {
	// value is the original parameter wrapped for driver conversion.
	value any
}

// Value returns the driver value of the wrapped parameter.
//
// Returns driver.Value which is the adapted parameter value.
// Returns error when the parameter cannot be converted or encoded.
//
//sgl:when decimal
func (wrapped sglDriverDecimalValue) Value() (driver.Value, error) {
	reflected := reflect.ValueOf(wrapped.value)
	if !reflected.IsValid() || reflected.Kind() == reflect.Pointer && reflected.IsNil() {
		return nil, nil
	}
	if valuer, values := wrapped.value.(driver.Valuer); values {
		return valuer.Value()
	}
	return driver.DefaultParameterConverter.ConvertValue(wrapped.value)
}
