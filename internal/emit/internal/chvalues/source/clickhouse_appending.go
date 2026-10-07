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
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"strings"
)

var (
	// errSglClickHouseAppend reports a value a native batch cannot append for its parameter.
	errSglClickHouseAppend = errors.New("sgl: cannot append the value of ClickHouse parameter")

	// sglAppendNativeUnknown is the shape of a part of a value the row's shape does not
	// describe, which is checked only as a netip.Addr and otherwise appended as it is.
	sglAppendNativeUnknown = sglClickHouseShape{names: nil, elements: nil, digits: -1, kind: 'a', checked: false, driverMap: false, prepared: false}
)

// sglAppendNativeRow appends one row to a native batch through appendRow, the batch's
// Append, after checking and converting each value as its element of shape says: shape is
// a tuple shape naming each value after its parameter, such as ('id'=a,'host'=4), with a
// for a value appended as it is.
//
// The native clickhouse-go driver appends an address to an IPv4 value through
// netip.Addr.As4, which panics on the zero netip.Addr and on an IPv6 address, and an IPv6
// value stores the zero netip.Addr as the unspecified address, so every address a value
// holds, at any depth, is checked first, as sglClickHouseAddress reads it: the zero
// netip.Addr, an empty net.IP and one of neither four nor sixteen bytes, an IPv6 address
// for an IPv4 value and text that is not an address are refused with an error naming the
// parameter and the reason, before the row is appended, so the caller sends nothing. A
// nil pointer is NULL. The driver appends an IPv4-mapped address to an IPv4 value as
// IPv4.
//
// The driver appends a Map only from a map of exactly the Go types it reads the column
// as, so a value holding a Map whose values hold an address or a tuple is converted as
// sglAppendNative converts it.
//
// The driver appends a value of a type it does not know, such as a defined type over a
// string or a boolean, or a pointer to a defined type over a number, only through its
// driver.Valuer, so every such value, at any depth within pointers, slices and maps, is
// appended as the predeclared type of its kind, a named byte slice as a []byte; a type
// that converts itself as a driver.Valuer, and a byte slice that marshals itself as text,
// such as a net.IP, are appended as they are.
//
// Takes appendRow (func(...any) error) which appends the converted row to the driver
// batch.
// Takes shape (string) which is the encoded SQL type descriptor.
// Takes values (...any) which holds row arguments and may be updated in place.
//
// Returns error when the descriptor, a value or the driver append fails.
func sglAppendNativeRow(appendRow func(...any) error, shape string, values ...any) error {
	row, err := sglClickHouseParseShape(shape)
	if err != nil {
		return err
	}
	return sglAppendNativeShapedRow(appendRow, row, shape, values...)
}

// sglAppendNativeShapedRow checks and converts one row using its compiled descriptor.
//
// Takes appendRow (func(...any) error) which appends the converted row to the driver
// batch.
// Takes row (sglClickHouseShape) which is the precompiled tuple descriptor.
// Takes shape (string) which is the encoded SQL type descriptor.
// Takes values (...any) which holds row arguments and is updated with converted values.
//
// Returns error when the descriptor, a value or the driver append fails.
func sglAppendNativeShapedRow(appendRow func(...any) error, row sglClickHouseShape, shape string, values ...any) error {
	if row.kind != '(' || len(row.elements) != len(values) {
		return fmt.Errorf("%w: %q does not describe a row of %d values", errSglClickHouseShape, shape, len(values))
	}
	for index, value := range values {
		converted, failure := sglAppendNativeValue(value, row.elements[index])
		if failure != "" {
			return fmt.Errorf("%w %s: %s", errSglClickHouseAppend, row.names[index], failure)
		}
		values[index] = converted
	}
	return appendRow(values...)
}

// sglAppendNativeValue checks and converts a native batch argument.
//
// It checks and converts one value of a row as its shape says, returning it as it is when
// its shape holds neither an address nor a Map the driver takes only in its own Go types
// and its type holds no defined type over a basic kind, and the reason it is refused, if
// it is.
//
// Takes value (any) which is the caller-supplied value to adapt.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns any which is the converted value, or the original value when unchanged.
// Returns string which gives a refusal reason, or is empty on success.
func sglAppendNativeValue(value any, shape sglClickHouseShape) (any, string) {
	if value == nil {
		return nil, ""
	}
	source := reflect.TypeOf(value)
	checked, driverMap := shape.checked, shape.driverMap
	if !shape.prepared {
		checked = sglAppendNativeHolds(shape, sglAppendNativeChecked)
		driverMap = sglAppendNativeHolds(shape, sglAppendNativeDriverMap)
	}
	if !checked && sglAppendNativeConverted(source, false) == source {
		return value, ""
	}
	converted, changed, failure := sglAppendNative(reflect.ValueOf(value), shape, driverMap, 0)
	if failure != "" || !changed {
		return value, failure
	}
	return converted.Interface(), ""
}

// sglAppendNativeHolds reports whether a shape, or a shape within it at any depth, is one
// part reports.
//
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes part (func(sglClickHouseShape) bool) which recognises a shape requiring the
// property being tested.
//
// Returns bool which reports whether this shape or any nested shape matches.
func sglAppendNativeHolds(shape sglClickHouseShape, part func(sglClickHouseShape) bool) bool {
	return part(shape) || slices.ContainsFunc(shape.elements, func(element sglClickHouseShape) bool {
		return sglAppendNativeHolds(element, part)
	})
}

// sglAppendNativeChecked reports whether a shape is one whose value the conversion checks
// or converts: an address, or a Map the driver takes only in its own Go types.
//
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns bool which reports whether the shape needs address or map conversion checks.
func sglAppendNativeChecked(shape sglClickHouseShape) bool {
	return shape.kind == '4' || shape.kind == '6' || sglAppendNativeDriverMap(shape)
}

// sglAppendNativeDriverMap identifies maps that require the driver's native value forms.
//
// It reports whether a shape is a Map the driver appends only from a map of its own Go
// types where the generated code holds others: one whose values hold an address, which it
// takes as a net.IP, or a tuple, a named one of which it takes as a map[string]any, at
// any depth.
//
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns bool which reports whether map values require the driver's native forms.
func sglAppendNativeDriverMap(shape sglClickHouseShape) bool {
	return shape.kind == '{' && len(shape.elements) == 2 && sglAppendNativeHolds(shape.elements[1], func(element sglClickHouseShape) bool {
		return element.kind == '4' || element.kind == '6' || element.kind == '('
	})
}

// sglAppendNative checks and converts a value recursively for native batch appending.
//
// It checks a value as its shape says, at any depth within pointers, interface values,
// slices, maps and named tuples, reporting the reason it is refused. It converts a
// defined type over a basic kind into the predeclared type of its kind, as
// sglAppendNativeBasic gives it. With forms set, it also converts the value into the Go
// types the driver appends a Map in: a netip.Addr as a net.IP, and a named tuple, a
// struct with exported fields that is not a driver.Valuer, as a map[string]any keyed by
// element name, reporting whether any part changed; a value whose type
// sglAppendNativeTarget changes always changes, and comes back in that type. A part a
// shape does not describe, such as the elements of an unnamed tuple's slice beyond the
// tuple's, is checked only as a netip.Addr.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns reflect.Value which is the converted value, or the source when unchanged.
// Returns bool which reports whether the value changed.
// Returns string which gives a refusal reason, or is empty on success.
func sglAppendNative(value reflect.Value, shape sglClickHouseShape, forms bool, depth int) (reflect.Value, bool, string) {
	if depth > sglClickHouseMaxDepth {
		return value, false, fmt.Sprintf("it nests more than %d levels deep or refers to itself", sglClickHouseMaxDepth)
	}
	if kind, isAddress := sglAppendNativeAddressKind(value.Type(), shape); isAddress {
		return sglAppendNativeAddress(value, kind, forms)
	}
	if sglAppendNativeTuple(value.Type()) {
		return sglAppendNativeFields(value, shape, forms, forms || sglAppendNativeFieldsChange(value.Type(), depth), depth)
	}
	if basic := sglAppendNativeBasic(value.Type()); basic != nil {
		return value.Convert(basic), true, ""
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return value, false, ""
		}
		return sglAppendNative(value.Elem(), shape, forms, depth+1)
	case reflect.Pointer:
		return sglAppendNativePointer(value, shape, forms, depth)
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return value, false, ""
		}
		return sglAppendNativeList(value, shape, forms, depth)
	case reflect.Map:
		return sglAppendNativeMap(value, shape, forms, depth)
	default:
		return value, false, ""
	}
}

// sglAppendNativeBasic finds the built-in type needed for a defined basic type.
//
// It returns the predeclared type the driver appends a value of a type as when the type
// is a defined type over a basic kind it does not know: the predeclared type of a string,
// boolean, integer or float kind, or []byte for a byte slice. It returns nil for a
// predeclared type, for a type that converts itself as a driver.Valuer, and for a byte
// slice that marshals itself as text, such as a net.IP, which the driver appends itself.
//
// Takes source (reflect.Type) which is the source Go type.
//
// Returns reflect.Type which is the built-in equivalent type, or nil when no conversion
// applies.
func sglAppendNativeBasic(source reflect.Type) reflect.Type {
	if source.Implements(reflect.TypeFor[driver.Valuer]()) {
		return nil
	}
	var basic reflect.Type
	switch source.Kind() {
	case reflect.String:
		basic = reflect.TypeFor[string]()
	case reflect.Bool:
		basic = reflect.TypeFor[bool]()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		basic = sglAppendNativeKindType(source.Kind())
	case reflect.Slice:
		if source.Elem().Kind() != reflect.Uint8 || source.Implements(reflect.TypeFor[encoding.TextMarshaler]()) {
			return nil
		}
		basic = reflect.TypeFor[[]byte]()
	default:
		return nil
	}
	if basic == source {
		return nil
	}
	return basic
}

// sglAppendNativeKindType returns the predeclared type of a number kind.
//
// Takes kind (reflect.Kind) which is the Go reflection kind to classify.
//
// Returns reflect.Type which is the built-in numeric type, defaulting to int64.
func sglAppendNativeKindType(kind reflect.Kind) reflect.Type {
	switch kind {
	case reflect.Int:
		return reflect.TypeFor[int]()
	case reflect.Int8:
		return reflect.TypeFor[int8]()
	case reflect.Int16:
		return reflect.TypeFor[int16]()
	case reflect.Int32:
		return reflect.TypeFor[int32]()
	case reflect.Uint:
		return reflect.TypeFor[uint]()
	case reflect.Uint8:
		return reflect.TypeFor[uint8]()
	case reflect.Uint16:
		return reflect.TypeFor[uint16]()
	case reflect.Uint32:
		return reflect.TypeFor[uint32]()
	case reflect.Uint64:
		return reflect.TypeFor[uint64]()
	case reflect.Float32:
		return reflect.TypeFor[float32]()
	case reflect.Float64:
		return reflect.TypeFor[float64]()
	default:
		return reflect.TypeFor[int64]()
	}
}

// sglAppendNativeAddressKind reports whether a value of a type is checked as an address,
// and the shape it is checked with: a netip.Addr, a net.IP or text where the shape is an
// address, and a netip.Addr anywhere else, as an address of either version.
//
// Takes source (reflect.Type) which is the source Go type.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns byte which is the IPv4 or IPv6 shape marker when recognised.
// Returns bool which reports whether the type and shape describe an address.
func sglAppendNativeAddressKind(source reflect.Type, shape sglClickHouseShape) (byte, bool) {
	address := source == reflect.TypeFor[netip.Addr]()
	if shape.kind == '4' || shape.kind == '6' {
		return shape.kind, address || source == reflect.TypeFor[net.IP]() || source.Kind() == reflect.String
	}
	return '6', address
}

// sglAppendNativeAddress checks an address, read from text as netip.ParseAddr reads it,
// as the driver does, and with forms set returns a netip.Addr as the net.IP the driver
// appends a Map's address as, IPv4 for an IPv4 value.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes kind (byte) which is the IPv4 or IPv6 shape marker.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
//
// Returns reflect.Value which is the converted value, or the source when unchanged.
// Returns bool which reports whether the value changed.
// Returns string which gives a refusal reason, or is empty on success.
func sglAppendNativeAddress(value reflect.Value, kind byte, forms bool) (reflect.Value, bool, string) {
	read := value
	if value.Kind() == reflect.String {
		parsed, err := netip.ParseAddr(value.String())
		if err != nil {
			return value, false, fmt.Sprintf("%q is not an IP address", value.String())
		}
		read = reflect.ValueOf(parsed)
	}
	address, _, failure := sglClickHouseAddress(read, kind)
	switch {
	case failure != "":
		return value, false, failure
	case value.Kind() == reflect.String && value.Type() != reflect.TypeFor[string]():
		return value.Convert(reflect.TypeFor[string]()), true, ""
	case !forms || value.Type() != reflect.TypeFor[netip.Addr]():
		return value, false, ""
	default:
		return reflect.ValueOf(net.IP(address.AsSlice())), true, ""
	}
}

// sglAppendNativeType resolves the Go type required inside a native driver Map.
//
// It returns the Go type the driver appends a value of a type as in a Map: a net.IP for a
// netip.Addr, a map[string]any for a named tuple, the predeclared type of a defined type
// over a basic kind, and a pointer, slice or map of such a type for one of the generated
// type. A map's keys keep their type, unless it is a defined type over a basic kind.
//
// Takes source (reflect.Type) which is the source Go type.
//
// Returns reflect.Type which is the driver-compatible Go type, or source when unchanged.
func sglAppendNativeType(source reflect.Type) reflect.Type {
	return sglAppendNativeConverted(source, true)
}

// sglAppendNativeTarget returns the type a value of a type comes back in: the type
// sglAppendNativeType gives it with forms set, and otherwise its own with every defined
// type over a basic kind the predeclared type of its kind.
//
// Takes source (reflect.Type) which is the source Go type.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
//
// Returns reflect.Type which is the driver-compatible Go type, or source when unchanged.
func sglAppendNativeTarget(source reflect.Type, forms bool) reflect.Type {
	return sglAppendNativeConverted(source, forms)
}

// sglAppendNativeConverted returns the type a value of a type comes back in, with forms
// set or not, as sglAppendNativeTarget describes it.
//
// Takes source (reflect.Type) which is the source Go type.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
//
// Returns reflect.Type which is the driver-compatible Go type, or source when unchanged.
func sglAppendNativeConverted(source reflect.Type, forms bool) reflect.Type {
	return sglAppendNativeConvertedAt(source, forms, 0)
}

// sglAppendNativeConvertedAt resolves a converted Go type with a nesting limit.
//
// It is sglAppendNativeConverted at a depth of a type, bounded so a type that refers to
// itself ends the walk: a named tuple's struct comes back as a map[string]any with forms
// set or when a field's type changes, which the driver appends a named tuple from by
// element name, and a map's key of a defined type over a basic kind as the predeclared
// type of its kind.
//
// Takes source (reflect.Type) which is the source Go type.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns reflect.Type which is the driver-compatible Go type, or source when unchanged.
func sglAppendNativeConvertedAt(source reflect.Type, forms bool, depth int) reflect.Type {
	if depth > sglClickHouseMaxDepth {
		return source
	}
	if forms && source == reflect.TypeFor[netip.Addr]() {
		return reflect.TypeFor[net.IP]()
	}
	if sglAppendNativeTuple(source) && (forms || sglAppendNativeFieldsChange(source, depth)) {
		return reflect.TypeFor[map[string]any]()
	}
	if basic := sglAppendNativeBasic(source); basic != nil {
		return basic
	}
	return sglAppendNativeContainer(source, forms, depth)
}

// sglAppendNativeContainer returns the type a pointer, slice or map comes back in when
// the type it holds does, a map's key of a defined type over a basic kind as the
// predeclared type of its kind, and any other type as it is.
//
// Takes source (reflect.Type) which is the source Go type.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns reflect.Type which is the driver-compatible Go type, or source when unchanged.
func sglAppendNativeContainer(source reflect.Type, forms bool, depth int) reflect.Type {
	switch source.Kind() {
	case reflect.Pointer:
		if element := sglAppendNativeConvertedAt(source.Elem(), forms, depth+1); element != source.Elem() {
			return reflect.PointerTo(element)
		}
	case reflect.Slice:
		if element := sglAppendNativeConvertedAt(source.Elem(), forms, depth+1); element != source.Elem() {
			return reflect.SliceOf(element)
		}
	case reflect.Map:
		key := source.Key()
		if basic := sglAppendNativeBasic(key); basic != nil {
			key = basic
		}
		if element := sglAppendNativeConvertedAt(source.Elem(), forms, depth+1); element != source.Elem() || key != source.Key() {
			return reflect.MapOf(key, element)
		}
	default:
	}
	return source
}

// sglAppendNativeFieldsChange reports whether the type of a field of a named tuple's
// struct, one sglAppendNativeFieldName keeps, comes back in another type, such as a
// defined string type the driver refuses, so the tuple is appended as a map instead.
//
// Takes source (reflect.Type) which is the source Go type.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns bool which reports whether any exported tuple field needs conversion.
func sglAppendNativeFieldsChange(source reflect.Type, depth int) bool {
	for field := range source.Fields() {
		if _, kept := sglAppendNativeFieldName(field); kept && sglAppendNativeConvertedAt(field.Type, false, depth+1) != field.Type {
			return true
		}
	}
	return false
}

// sglAppendNativeTuple reports whether a type is one the generated code holds a named
// tuple in: a struct with an exported field that does not convert itself as a
// driver.Valuer, which time.Time, big.Int and decimal.Decimal are not.
//
// Takes source (reflect.Type) which is the source Go type.
//
// Returns bool which reports whether the type is a named tuple rather than a scalar
// adapter.
func sglAppendNativeTuple(source reflect.Type) bool {
	valuer := reflect.TypeFor[driver.Valuer]()
	if source.Kind() != reflect.Struct || source.Implements(valuer) || reflect.PointerTo(source).Implements(valuer) {
		return false
	}
	return slices.ContainsFunc(reflect.VisibleFields(source), func(field reflect.StructField) bool {
		return field.IsExported()
	})
}

// sglAppendNativePointer checks and converts the value a pointer points at; a nil pointer
// is NULL, and with forms set one of a type that changes comes back as a nil pointer of
// the changed type.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns reflect.Value which is the converted value, or the source when unchanged.
// Returns bool which reports whether the value changed.
// Returns string which gives a refusal reason, or is empty on success.
func sglAppendNativePointer(value reflect.Value, shape sglClickHouseShape, forms bool, depth int) (reflect.Value, bool, string) {
	target := sglAppendNativeTarget(value.Type(), forms)
	if value.IsNil() {
		return reflect.Zero(target), target != value.Type(), ""
	}
	element, changed, failure := sglAppendNative(value.Elem(), shape, forms, depth+1)
	if failure != "" || !changed {
		return value, false, failure
	}
	pointer := reflect.New(target.Elem())
	pointer.Elem().Set(element)
	return pointer, true, ""
}

// sglAppendNativeList checks and converts each element of a slice: an array's, by the
// array's element shape, or an unnamed tuple's, by the shape of its own element; with
// forms set, a nil slice of a type that changes comes back as a nil slice of the changed
// type.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns reflect.Value which is the converted value, or the source when unchanged.
// Returns bool which reports whether the value changed.
// Returns string which gives a refusal reason, or is empty on success.
func sglAppendNativeList(value reflect.Value, shape sglClickHouseShape, forms bool, depth int) (reflect.Value, bool, string) {
	target := sglAppendNativeTarget(value.Type(), forms)
	if value.IsNil() {
		return reflect.Zero(target), target != value.Type(), ""
	}
	changed := target != value.Type()
	rebuilt := forms || changed
	converted := value
	if rebuilt {
		converted = reflect.MakeSlice(target, value.Len(), value.Len())
	}
	for index := range value.Len() {
		element, elementChanged, failure := sglAppendNative(value.Index(index), sglAppendNativeItem(shape, index), forms, depth+1)
		if failure != "" {
			return value, false, failure
		}
		changed = changed || elementChanged
		if rebuilt {
			converted.Index(index).Set(element)
		}
	}
	if !changed {
		return value, false, ""
	}
	return converted, true, ""
}

// sglAppendNativeMap checks and converts map entries for native batch appending.
//
// It checks and converts each value of a map: a Map's by its value shape, or a named
// tuple's by the shape of the element its key names. Keys of defined basic types are
// converted to their predeclared types. A nil map comes back as a nil map of the target
// type.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns reflect.Value which is the converted value, or the source when unchanged.
// Returns bool which reports whether the value changed.
// Returns string which gives a refusal reason, or is empty on success.
func sglAppendNativeMap(value reflect.Value, shape sglClickHouseShape, forms bool, depth int) (reflect.Value, bool, string) {
	target := sglAppendNativeTarget(value.Type(), forms)
	if value.IsNil() {
		return reflect.Zero(target), target != value.Type(), ""
	}
	changed := target != value.Type()
	rebuilt := forms || changed
	converted := value
	if rebuilt {
		converted = reflect.MakeMapWithSize(target, value.Len())
	}
	iterator := value.MapRange()
	for iterator.Next() {
		element, elementChanged, failure := sglAppendNative(iterator.Value(), sglAppendNativeEntry(shape, iterator.Key()), forms, depth+1)
		if failure != "" {
			return value, false, failure
		}
		changed = changed || elementChanged
		if rebuilt {
			converted.SetMapIndex(iterator.Key().Convert(target.Key()), element)
		}
	}
	if !changed {
		return value, false, ""
	}
	return converted, true, ""
}

// sglAppendNativeFields converts a tuple struct to the required driver form.
//
// It checks a named tuple's struct, each field kept by sglAppendNativeFieldName by the
// shape of the tuple's element in the same place, and with asMap set converts it into the
// map[string]any the driver appends a named tuple of a Map as, or one whose fields it
// refuses, keyed by each field's element name and holding its value converted.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes forms (bool) which requests the native Go forms required inside driver maps.
// Takes asMap (bool) which requests a map keyed by field name instead of a converted
// struct.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns reflect.Value which is the converted value, or the source when unchanged.
// Returns bool which reports whether the value changed.
// Returns string which gives a refusal reason, or is empty on success.
func sglAppendNativeFields(value reflect.Value, shape sglClickHouseShape, forms, asMap bool, depth int) (reflect.Value, bool, string) {
	structType := value.Type()
	var names []string
	var indexes []int
	for index := range structType.NumField() {
		if name, kept := sglAppendNativeFieldName(structType.Field(index)); kept {
			names = append(names, name)
			indexes = append(indexes, index)
		}
	}
	if shape.kind != '(' || len(shape.elements) != len(indexes) {
		shape = sglAppendNativeUnknown
	}
	var fields map[string]any
	if asMap {
		fields = make(map[string]any, len(indexes))
	}
	for position, index := range indexes {
		element, _, failure := sglAppendNative(value.Field(index), sglAppendNativeItem(shape, position), forms, depth+1)
		if failure != "" {
			return value, false, failure
		}
		if asMap {
			fields[names[position]] = element.Interface()
		}
	}
	if !asMap {
		return value, false, ""
	}
	return reflect.ValueOf(fields), true, ""
}

// sglAppendNativeItem returns the shape of the element of a value at a position: an
// array's element shape, a tuple's element there, or the unknown shape.
//
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes position (int) which is the zero-based tuple or array element index.
//
// Returns sglClickHouseShape which describes the element, or is the unknown shape.
func sglAppendNativeItem(shape sglClickHouseShape, position int) sglClickHouseShape {
	switch {
	case shape.kind == '[' && len(shape.elements) == 1:
		return shape.elements[0]
	case shape.kind == '(' && position < len(shape.elements):
		return shape.elements[position]
	default:
		return sglAppendNativeUnknown
	}
}

// sglAppendNativeEntry returns the shape of a map's value under a key: a Map's value
// shape, the shape of the named tuple's element the key names, or the unknown shape.
//
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes key (reflect.Value) which is the reflected map key used to identify a named tuple
// element.
//
// Returns sglClickHouseShape which describes the entry value, or is the unknown shape.
func sglAppendNativeEntry(shape sglClickHouseShape, key reflect.Value) sglClickHouseShape {
	switch {
	case shape.kind == '{' && len(shape.elements) == 2:
		return shape.elements[1]
	case shape.kind == '(' && key.Kind() == reflect.String:
		if position := slices.Index(shape.names, key.String()); position >= 0 && key.String() != "" {
			return shape.elements[position]
		}
	default:
	}
	return sglAppendNativeUnknown
}

// sglAppendNativeFieldName returns the element name of a struct field as the driver names
// it: its json name, then its ch name, then its Go name. An unexported field, and one a
// tag names "-", is left out.
//
// Takes field (reflect.StructField) which is the struct field whose tags and visibility
// are inspected.
//
// Returns string which is the tuple element name selected from tags or the Go name.
// Returns bool which reports whether the field is exported and not excluded.
func sglAppendNativeFieldName(field reflect.StructField) (string, bool) {
	if !field.IsExported() {
		return "", false
	}
	for _, key := range []string{"json", "ch"} {
		tag := field.Tag.Get(key)
		if tag == "-" {
			return "", false
		}
		if name, _, _ := strings.Cut(tag, ","); name != "" {
			return name, true
		}
	}
	return field.Name, true
}
