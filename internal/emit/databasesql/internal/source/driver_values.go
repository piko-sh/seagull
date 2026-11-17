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
	"errors"
	"reflect"
	"strconv"
	"strings"
)

// sglBinaryPointerBound prevents cyclic runtime arguments from hanging conversion.
//
//sgl:when binary
const sglBinaryPointerBound = 32

// sglUnsignedDecimalBase preserves the exact native decimal spelling.
//
//sgl:when unsigned
const sglUnsignedDecimalBase = 10

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
	if bytes, ok := converted.([]byte); ok && bytes == nil {
		return nil, nil
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
	encoded, err := sglJSONMarshal(wrapped.value)
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

// sglDriverUnsigned preserves unsigned scalars through database/sql conversion.
//
//sgl:when unsigned
type sglDriverUnsigned struct {
	// value retains a scalar, an optional value or a nullable pointer.
	value any
}

// Value returns exact decimal text for an unsigned scalar.
//
// Returns driver.Value which preserves NULL and all unsigned bits.
// Returns error when an override cannot convert itself to a driver value, or the value is
// held through a cyclic or very long chain of pointers.
//
//sgl:when unsigned
func (wrapped sglDriverUnsigned) Value() (driver.Value, error) {
	value, held := sglUnsignedScalar(sglDriverNullValue(reflect.ValueOf(wrapped.value)))
	if !held {
		return nil, errors.New("unsigned parameter is held through too many pointers")
	}
	if !value.IsValid() {
		return nil, nil
	}
	if valuer, ok := reflect.TypeAssert[driver.Valuer](value); ok {
		return valuer.Value()
	}
	if value.Kind() >= reflect.Uint && value.Kind() <= reflect.Uintptr {
		return strconv.FormatUint(value.Uint(), sglUnsignedDecimalBase), nil
	}
	return driver.DefaultParameterConverter.ConvertValue(value.Interface())
}

// sglDriverNullValue unwraps a generated Null value or leaves another value unchanged.
//
// An invalid Null returns an invalid reflect.Value.
//
// Takes value (reflect.Value) which is the reflected parameter.
//
// Returns reflect.Value which is the held value, invalid for NULL, or value itself.
//
//sgl:when unsigned
func sglDriverNullValue(value reflect.Value) reflect.Value {
	const nullFields = 2
	if value.Kind() != reflect.Struct || value.NumField() != nullFields || !strings.HasPrefix(value.Type().Name(), "Null[") {
		return value
	}
	held := value.Type().Field(0)
	valid := value.Type().Field(1)
	heldNamed := held.Name == "V"
	validNamed := valid.Name == "Valid" && valid.Type.Kind() == reflect.Bool
	if !heldNamed || !validNamed {
		return value
	}
	if !value.Field(1).Bool() {
		return reflect.Value{}
	}
	return value.Field(0)
}

// sglUnsignedArguments wraps the unsigned scalars among a runtime builder's arguments,
// leaving the driver's own values as they are.
//
// Takes values ([]any) which holds the assembled arguments in binding order.
//
// Returns []any which is values, with its unsigned scalars wrapped.
//
//sgl:when unsigned
func sglUnsignedArguments(values []any) []any {
	for index, value := range values {
		reached, held := sglUnsignedScalar(reflect.ValueOf(value))
		if !held || !reached.IsValid() {
			continue
		}
		if _, valuer := reflect.TypeAssert[driver.Valuer](reached); valuer {
			continue
		}
		if reached.Kind() >= reflect.Uint && reached.Kind() <= reflect.Uintptr {
			values[index] = sglDriverUnsigned{value: value}
		}
	}
	return values
}

// sglUnsignedScalar follows the pointers and interfaces around a value. It stops at a nil
// one, at a driver.Valuer, or at the value they hold.
//
// Takes value (reflect.Value) which is the reflected argument.
//
// Returns reflect.Value which is the value reached, invalid for a nil pointer or
// interface.
// Returns bool which is false when the chain is cyclic or too long to follow.
//
//sgl:when unsigned
func sglUnsignedScalar(value reflect.Value) (reflect.Value, bool) {

	const pointerBound = 32
	for range pointerBound {
		if !value.IsValid() {
			return value, true
		}
		indirect := value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface
		if indirect && value.IsNil() {
			return reflect.Value{}, true
		}
		if _, ok := reflect.TypeAssert[driver.Valuer](value); ok || !indirect {
			return value, true
		}
		value = value.Elem()
	}
	return reflect.Value{}, false
}

// sglBinaryArguments preserves nil byte slices in runtime bindings.
//
// Takes values ([]any) which contains assembled runtime arguments.
//
// Returns []any which retains empty byte slices and custom valuers.
//
//sgl:when binary
func sglBinaryArguments(values []any) []any {
	for index, value := range values {
		if sglNilBinary(value) {
			values[index] = nil
		}
	}
	return values
}

// sglNilBinary recognises nil byte slices through bounded pointer chains.
//
// Takes value (any) which may contain a binary value or a custom valuer.
//
// Returns bool which selects only nil byte slices without custom conversion.
//
//sgl:when binary
func sglNilBinary(value any) bool {
	reflected := reflect.ValueOf(value)
	for depth := 0; reflected.IsValid() && depth < sglBinaryPointerBound; depth++ {
		if _, ok := reflect.TypeAssert[driver.Valuer](reflected); ok {
			return false
		}
		switch reflected.Kind() {
		case reflect.Pointer, reflect.Interface:
			reflected = reflected.Elem()
		case reflect.Slice:
			return reflected.Type().Elem().Kind() == reflect.Uint8 && reflected.IsNil()
		default:
			return false
		}
	}
	return false
}
