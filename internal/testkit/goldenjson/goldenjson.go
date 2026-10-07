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

package goldenjson

import (
	"encoding"
	"fmt"
	"reflect"
)

// Project prepares a value for readable, stable golden files.
//
// It omits empty struct fields, uses textual names where available, writes a query
// template in its compact form (see Template), and preserves collection positions.
// Non-nil pointers remain distinct from absent values, even when they point to zero.
//
// Takes value (any) which is the value to project.
//
// Returns any which is a tree of maps, slices and scalars ready for JSON encoding.
func Project(value any) any {
	if value == nil {
		return nil
	}
	return projectValue(reflect.ValueOf(value))
}

// projectValue projects one reflected value; see Project for the rules.
//
// Takes value (reflect.Value) which is the value to project.
//
// Returns any which is the projected form.
func projectValue(value reflect.Value) any {
	if name, isEnum := enumText(value); isEnum {
		return name
	}
	if text, isTemplate := templateText(value); isTemplate {
		return text
	}
	if text, isText := marshalledText(value); isText {
		return text
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return nil
		}
		return projectValue(value.Elem())
	case reflect.Struct:
		return projectStruct(value)
	case reflect.Slice, reflect.Array:
		return projectSequence(value)
	case reflect.Map:
		return projectMap(value)
	default:
		return value.Interface()
	}
}

// projectStruct projects a struct into an object of its non-empty exported fields.
//
// Takes value (reflect.Value) which is the struct.
//
// Returns map[string]any which maps field names to projected values.
func projectStruct(value reflect.Value) map[string]any {
	fields := make(map[string]any)
	for field, fieldValue := range value.Fields() {
		if !field.IsExported() {
			continue
		}
		if isEmptyField(fieldValue) {
			continue
		}
		fields[field.Name] = projectValue(fieldValue)
	}
	return fields
}

// projectSequence projects every element of a slice or array, keeping empty elements.
//
// Takes value (reflect.Value) which is the slice or array.
//
// Returns []any which holds the projected elements.
func projectSequence(value reflect.Value) []any {
	elements := make([]any, value.Len())
	for index := range value.Len() {
		elements[index] = projectValue(value.Index(index))
	}
	return elements
}

// projectMap projects every value of a map, keyed by the key's text.
//
// Takes value (reflect.Value) which is the map.
//
// Returns map[string]any which maps each key's text to its projected value.
func projectMap(value reflect.Value) map[string]any {
	entries := make(map[string]any, value.Len())
	iterator := value.MapRange()
	for iterator.Next() {
		entries[fmt.Sprint(iterator.Key().Interface())] = projectValue(iterator.Value())
	}
	return entries
}

// isEmptyField reports whether a struct field is left out of the projection. Enums are
// retained unless their IsZero method reports them absent.
//
// Takes value (reflect.Value) which is the field's value.
//
// Returns bool which is true for a zero scalar, a nil pointer or interface, an empty
// slice or map, or a struct whose projection is empty.
func isEmptyField(value reflect.Value) bool {
	if _, isEnum := enumText(value); isEnum {
		return reportsAbsent(value)
	}
	if text, isTemplate := templateText(value); isTemplate {
		return text == ""
	}
	if text, isText := marshalledText(value); isText {
		return text == ""
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Map:
		return value.Len() == 0
	case reflect.Struct:
		return len(projectStruct(value)) == 0
	default:
		return value.IsZero()
	}
}

// reportsAbsent reports whether an enum value has an IsZero method that reports it
// absent.
//
// Takes value (reflect.Value) which is the enum value.
//
// Returns bool which is true when the value's IsZero method returns true.
func reportsAbsent(value reflect.Value) bool {
	zeroer, isZeroer := reflect.TypeAssert[interface{ IsZero() bool }](value)
	return isZeroer && zeroer.IsZero()
}

// enumText returns the name of a named integer value with a String method.
//
// Takes value (reflect.Value) which is the candidate enum.
//
// Returns string which is the value's name.
// Returns bool which is true when the value is such an enum.
func enumText(value reflect.Value) (string, bool) {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		return "", false
	}
	if !value.CanInterface() {
		return "", false
	}
	stringer, isStringer := reflect.TypeAssert[fmt.Stringer](value)
	if !isStringer {
		return "", false
	}
	return stringer.String(), true
}

// marshalledText returns the text of a value that implements encoding.TextMarshaler.
//
// Takes value (reflect.Value) which is the candidate.
//
// Returns string which is the value's text.
// Returns bool which is true when the value implements encoding.TextMarshaler and
// marshals without error.
func marshalledText(value reflect.Value) (string, bool) {
	if value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface || !value.CanInterface() {
		return "", false
	}
	marshaler, isMarshaler := reflect.TypeAssert[encoding.TextMarshaler](value)
	if !isMarshaler {
		return "", false
	}
	text, err := marshaler.MarshalText()
	if err != nil {
		return "", false
	}
	return string(text), true
}
