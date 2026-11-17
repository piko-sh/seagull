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
	"database/sql"
	"database/sql/driver"
	"math"
)

// sglNullRawValue makes Value return the value as it is, for a native driver.
//
//sgl:inline rawValue
const sglNullRawValue = false

// Null holds a nullable column or parameter value in place.
//
// Valid distinguishes SQL NULL from V, and JSON encoding writes the value or null.
type Null[T any] struct {
	// V is the value, the zero value of T when Valid is false.
	V T

	// Valid is false when the value is SQL NULL.
	Valid bool
}

// NullOf returns a Null holding a value.
//
// Takes value (T) which is the value.
//
// Returns Null[T] which holds it.
func NullOf[T any](value T) Null[T] {
	return Null[T]{V: value, Valid: true}
}

// NullFrom returns a Null holding the value a pointer points to, or NULL for a nil
// pointer, for code that holds nullable values as pointers.
//
// Takes pointer (*T) which points to the value, or is nil.
//
// Returns Null[T] which holds a copy of the value, or NULL.
func NullFrom[T any](pointer *T) Null[T] {
	if pointer == nil {
		return Null[T]{}
	}
	return Null[T]{V: *pointer, Valid: true}
}

// Get returns the value and whether it is set.
//
// Returns T which is the value, or the zero value of T when it is NULL.
// Returns bool which is false when the value is NULL.
func (n Null[T]) Get() (T, bool) {
	return n.V, n.Valid
}

// Ptr returns a pointer to a copy of the value, or nil when it is NULL.
//
// Returns *T which points to a copy of the value, or nil.
func (n Null[T]) Ptr() *T {
	if !n.Valid {
		return nil
	}
	value := n.V
	return &value
}

// IsZero reports whether the value is NULL, so a field tagged omitzero is left out of
// JSON when it is NULL.
//
// Returns bool which is true when the value is NULL.
func (n Null[T]) IsZero() bool {
	return !n.Valid
}

// Scan stores a driver's value in Null.
//
// A value of T or *T is stored directly. Narrower numbers are stored without text
// conversion when they can hold a driver's int64 or float64 exactly. Other values and SQL
// NULL follow sql.Null[T] scanning rules.
//
// Takes source (any) which is the value the driver supplies.
//
// Returns error when the value cannot be stored in T.
func (n *Null[T]) Scan(source any) error {
	switch value := source.(type) {
	case nil:
		*n = Null[T]{}
		return nil
	case int64:
		if sglNullStoreInteger(any(&n.V), value) {
			n.Valid = true
			return nil
		}
	case float64:
		if sglNullStoreFloat(any(&n.V), value) {
			n.Valid = true
			return nil
		}
	case *T:
		if value == nil {
			*n = Null[T]{}
			return nil
		}
		n.V, n.Valid = *value, true
		return nil
	case T:
		n.V, n.Valid = value, true
		return nil
	}
	var read sql.Null[T]
	if err := read.Scan(source); err != nil {
		return err
	}
	n.V, n.Valid = read.V, read.Valid
	return nil
}

// Value returns SQL NULL or the stored value for driver binding.
//
// Database/sql binding follows sql.Null[T], while native binding returns V directly. A
// uint64 above math.MaxInt64 is returned unchanged for drivers that can bind it.
//
// Returns driver.Value which is the value, or nil for NULL.
// Returns error when the value cannot be written.
func (n Null[T]) Value() (driver.Value, error) {
	if sglNullRawValue {
		if !n.Valid {
			return nil, nil
		}
		return n.V, nil
	}
	if wide, ok := any(n.V).(uint64); ok && n.Valid && wide > math.MaxInt64 {
		return wide, nil
	}
	return sql.Null[T]{V: n.V, Valid: n.Valid}.Value()
}

// MarshalJSON encodes the value, or null when it is NULL.
//
// Returns []byte which is the JSON.
// Returns error when the value cannot be encoded.
func (n Null[T]) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return sglJSONMarshal(&n.V)
}

// UnmarshalJSON decodes a value, or NULL from null.
//
// Takes data ([]byte) which is the JSON.
//
// Returns error when the JSON is not a value of T or null.
func (n *Null[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*n = Null[T]{}
		return nil
	}
	if err := sglJSONUnmarshal(data, &n.V); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

// sglNullStoreInteger stores a driver's int64 in a narrower Go integer when it fits. This
// avoids database/sql's text conversion and its allocation.
//
// Takes target (any) which points to the Null's value.
// Takes value (int64) which is the driver's integer.
//
// Returns bool which is true when the value was stored, and false when the target is of
// another type or the value does not fit it.
func sglNullStoreInteger(target any, value int64) bool {
	switch pointer := target.(type) {
	case *int64:
		*pointer = value
		return true
	case *int:
		return sglStoreExact(pointer, value)
	case *int32:
		return sglStoreExact(pointer, value)
	case *int16:
		return sglStoreExact(pointer, value)
	case *int8:
		return sglStoreExact(pointer, value)
	case *uint32:
		return sglStoreExact(pointer, value)
	case *uint16:
		return sglStoreExact(pointer, value)
	case *uint8:
		return sglStoreExact(pointer, value)
	default:
		return false
	}
}

// sglNullStoreFloat stores a driver's float64 in a float64, or in a float32 that holds it
// exactly.
//
// Takes target (any) which points to the Null's value.
// Takes value (float64) which is the driver's float.
//
// Returns bool which is true when the value was stored.
func sglNullStoreFloat(target any, value float64) bool {
	switch pointer := target.(type) {
	case *float64:
		*pointer = value
		return true
	case *float32:
		if float64(float32(value)) != value {
			return false
		}
		*pointer = float32(value)
		return true
	default:
		return false
	}
}

// sglStoreExact stores an int64 in a narrower integer when the integer holds it exactly.
// An int is narrower on a 32-bit platform.
//
// Takes pointer (*T) which is the integer.
// Takes value (int64) which is the value.
//
// Returns bool which is true when the value was stored.
func sglStoreExact[T ~int | ~int32 | ~int16 | ~int8 | ~uint32 | ~uint16 | ~uint8](pointer *T, value int64) bool {
	converted := T(value)
	if int64(converted) != value {
		return false
	}
	*pointer = converted
	return true
}
