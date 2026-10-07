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
	"fmt"
	"reflect"
)

// sglOrderedMap is what a pointer to the type a driver returns a map in, in order,
// offers: its keys and, in the same order, their values, as the DuckDB driver's
// OrderedMap does.
type sglOrderedMap interface {
	// Keys returns the map entry keys in driver order.
	//
	// Returns []any which holds the entry keys in the same order as Values.
	Keys() []any

	// Values returns the map entry values in driver order.
	//
	// Returns []any which holds the entry values in the same order as Keys.
	Values() []any
}

// sglReadList reads a slice or an array into a slice, each element through read; a nil
// slice reads as a nil one.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
// Takes read (func(reflect.Value, any) error) which reads one of the driver's values into
// a settable destination.
//
// Returns error when the source cannot be represented by the destination.
func sglReadList(target reflect.Value, value reflect.Value, read func(reflect.Value, any) error) error {
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return sglUnreadable(value.Type(), target.Type())
	}
	if value.Kind() == reflect.Slice && value.IsNil() {
		target.SetZero()
		return nil
	}
	list := reflect.MakeSlice(target.Type(), value.Len(), value.Len())
	for index := range value.Len() {
		if err := read(list.Index(index), value.Index(index).Interface()); err != nil {
			return fmt.Errorf("element %d: %w", index, err)
		}
	}
	target.Set(list)
	return nil
}

// sglReadMap reads a map, a Go map or a driver's ordered map, into a map, each key and
// value through read; a nil Go map reads as a nil one.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
// Takes read (func(reflect.Value, any) error) which reads one of the driver's values into
// a settable destination.
//
// Returns error when the source cannot be represented by the destination.
func sglReadMap(target reflect.Value, value reflect.Value, read func(reflect.Value, any) error) error {
	if value.Kind() == reflect.Map && value.IsNil() {
		target.SetZero()
		return nil
	}
	keys, values, ok := sglMapEntries(value)
	if !ok {
		return sglUnreadable(value.Type(), target.Type())
	}
	converted := reflect.MakeMapWithSize(target.Type(), len(keys))
	for index, key := range keys {
		convertedKey := reflect.New(target.Type().Key()).Elem()
		if err := read(convertedKey, key); err != nil {
			return fmt.Errorf("key %v: %w", key, err)
		}
		element := reflect.New(target.Type().Elem()).Elem()
		if err := read(element, values[index]); err != nil {
			return fmt.Errorf("value of key %v: %w", key, err)
		}
		converted.SetMapIndex(convertedKey, element)
	}
	target.Set(converted)
	return nil
}

// sglMapEntries returns the keys of a map and their values in the same order, or false
// when the value is neither a Go map nor a driver's ordered map.
//
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns keys ([]any) which holds the keys in entry order.
// Returns values ([]any) which holds the values in entry order.
// Returns ok (bool) which reports whether matching key and value slices were obtained.
func sglMapEntries(value reflect.Value) (keys []any, values []any, ok bool) {
	if value.Kind() == reflect.Map {
		keys = make([]any, 0, value.Len())
		values = make([]any, 0, value.Len())
		entries := value.MapRange()
		for entries.Next() {
			keys = append(keys, entries.Key().Interface())
			values = append(values, entries.Value().Interface())
		}
		return keys, values, true
	}
	pointer := reflect.New(value.Type())
	pointer.Elem().Set(value)
	ordered, ordersEntries := reflect.TypeAssert[sglOrderedMap](pointer)
	if !ordersEntries {
		return nil, nil, false
	}
	keys, values = ordered.Keys(), ordered.Values()
	return keys, values, len(keys) == len(values)
}

// sglReadScalar converts within scalar kinds: integers with overflow checks, floats,
// strings and booleans. Floating-point conversions may lose precision or overflow.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns error when the source cannot be represented by the destination.
func sglReadScalar(target reflect.Value, value reflect.Value) error {
	kind := sglScalarKind(value.Kind())
	if kind == "" || kind != sglScalarKind(target.Kind()) || !value.CanConvert(target.Type()) {
		return sglUnreadable(value.Type(), target.Type())
	}
	converted := value.Convert(target.Type())
	if !sglScalarHolds(converted, value) {
		return fmt.Errorf("%v overflows %s", value.Interface(), target.Type())
	}
	target.Set(converted)
	return nil
}

// sglScalarHolds reports whether an integer converted to another type still holds the
// value: it converts back unchanged and keeps its sign. A value of any other kind always
// holds.
//
// Takes converted (reflect.Value) which is the value after conversion.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns bool which reports whether conversion preserves the integer value and sign.
func sglScalarHolds(converted reflect.Value, value reflect.Value) bool {
	switch {
	case !value.CanInt() && !value.CanUint():
		return true
	case !converted.Convert(value.Type()).Equal(value):
		return false
	case value.CanInt() && converted.CanUint():
		return value.Int() >= 0
	case value.CanUint() && converted.CanInt():
		return converted.Int() >= 0
	default:
		return true
	}
}

// sglScalarKind returns the kind of scalar a reflect kind is: integer, float, string,
// boolean, or empty for any other.
//
// Takes kind (reflect.Kind) which is the Go reflection kind to classify.
//
// Returns string which names the scalar category, or is empty for unsupported kinds.
func sglScalarKind(kind reflect.Kind) string {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	default:
		return ""
	}
}

// sglUnreadable returns the error of a value of one type a reader cannot read into
// another.
//
// Takes source (reflect.Type) which is the source Go type.
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns error which describes the unsupported source and destination types.
func sglUnreadable(source reflect.Type, target reflect.Type) error {
	return fmt.Errorf("cannot read %s into %s", source, target)
}
