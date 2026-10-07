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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// sglClickHouseMaxDepth bounds how many pointers, interfaces and nested composites
	// sglClickHouseValue follows, so a value that refers to itself is refused instead of
	// followed for ever.
	sglClickHouseMaxDepth = 32

	// sglClickHouseNanosecondDigits is the number of fraction digits a nanosecond takes.
	sglClickHouseNanosecondDigits = 9

	// sglClickHouseDigitGroup is the number of fraction digits a millisecond, a microsecond
	// and a nanosecond each add.
	sglClickHouseDigitGroup = 3

	// sglClickHouseWritesDurations reports that the package binds a time.Duration to a Time
	// or Time64 parameter, whose text sglClickHouseDuration writes.
	//
	//sgl:inline writesDurations
	sglClickHouseWritesDurations = true
)

var (
	// sglClickHouseTextEscaper escapes the text of a whole parameter value, which ClickHouse
	// reads in its escaped format: a backslash starts an escape sequence, and a tab or a
	// newline ends the value.
	sglClickHouseTextEscaper = strings.NewReplacer("\\", "\\\\", "\t", "\\t", "\n", "\\n", "\r", "\\r", "\x00", "\\0")

	// sglClickHouseQuotedEscaper escapes text written between the quotes of an element of an
	// array, map or tuple, so the text cannot end the element or add another.
	sglClickHouseQuotedEscaper = strings.NewReplacer("\\", "\\\\", "'", "\\'", "\t", "\\t", "\n", "\\n", "\r", "\\r", "\x00", "\\0")

	// sglClickHouseRefusalCleaner keeps a refusal's reason on one line.
	sglClickHouseRefusalCleaner = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")

	// errSglClickHouseShape reports a shape the helper cannot read.
	errSglClickHouseShape = errors.New("sgl: malformed ClickHouse parameter shape")

	// errSglClickHouseValue reports a value the helper cannot write for its parameter.
	errSglClickHouseValue = errors.New("sgl: cannot write the value of a ClickHouse parameter")
)

// sglClickHouseShape describes one level of a ClickHouse parameter type.
//
// The shape grammar uses the following markers.
//
//	q          text, quoted inside an array, map or tuple (String, UUID, an Enum)
//	4          an IP address of a type holding only IPv4 ones, quoted like text
//	6          an IP address of a type holding either version, quoted like text
//	n          a number or a boolean, written bare
//	d          a date: the calendar date of a time.Time in its own location
//	t, t0..t9  a date and time: a time.Time as Unix seconds, with that many fraction digits
//	a          any: whatever the Go value is, a number bare, a slice an array, other text quoted
//	[s]        an array whose elements have shape s
//	{k:v}      a map whose keys have shape k and values shape v
//	(s,'n'=s)  a tuple of the shapes listed, each optionally named as a quoted string
type sglClickHouseShape struct {
	// names holds tuple element names in descriptor order.
	names []string

	// elements holds descriptors for nested values.
	elements []sglClickHouseShape

	// digits is the timestamp precision, or -1 when unspecified.
	digits int

	// prepared reports whether the native conversion flags are precomputed.
	prepared bool

	// checked reports whether native value checks are required.
	checked bool

	// driverMap reports whether maps require the driver's native Go forms.
	driverMap bool

	// kind is the shape grammar's type marker.
	kind byte
}

// sglClickHouseShapes holds the package's generation-time shape descriptors.
//
//sgl:data shapes
//nolint:unused // read by generated query methods
var sglClickHouseShapes = []sglClickHouseShape{}

// sglClickHouseEncodedDocument is a JSON parameter's value written as its JSON encoding.
//
//sgl:when json
type sglClickHouseEncodedDocument struct {
	// value is the original parameter wrapped for driver conversion.
	value any
}

// String returns the document's JSON encoding, or the reason it has none.
//
// Returns string which is the encoded JSON document.
// Returns error when JSON encoding fails.
//
//sgl:when json
func (document sglClickHouseEncodedDocument) String() (string, error) {
	encoded, err := json.Marshal(document.value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// sglClickHouseValue writes a value bound to a {name:Type} parameter as the text
// ClickHouse parses for the parameter's type, which the shape describes.
//
// nil, and a nil pointer or interface, are \N, the NULL of a Nullable type. A string is
// written escaped, so a backslash, a tab, a newline, a carriage return or a NUL reaches
// the server as written. A time.Time is a calendar date in its own location for a date,
// and Unix seconds for a date and time, so the moment it names is kept whatever the
// server's time zone; the zero time is no exception. Inside an array, a map or a tuple,
// text is quoted and numbers are bare, with a map's entries in a fixed order.
//
// An IP address, a netip.Addr or a net.IP, is written as sglClickHouseAddress reads it
// for the type: an IPv4-mapped one as IPv4 for a type holding only IPv4 addresses, and
// without its zone.
//
// A value the helper cannot write faithfully, such as one that refers to itself, an
// error, a String method that fails, text where a number belongs, or an address the type
// cannot hold, such as the zero netip.Addr, is refused: the text bound in its place is
// the reason followed by a tab. A tab ends a value in the format ClickHouse reads
// parameters in, so no type parses that text completely, and the server rejects the
// statement with an error quoting the reason rather than storing a wrong value.
//
// Takes value (any) which is the caller-supplied value to adapt.
// Takes shape (string) which is the encoded SQL type descriptor.
//
// Returns string which is the encoded parameter or refusal text that ClickHouse rejects.
func sglClickHouseValue(value any, shape string) string {
	parsed, err := sglClickHouseParseShape(shape)
	if err == nil {
		return sglClickHouseShapedValue(value, parsed)
	}
	return sglClickHouseRefusalCleaner.Replace(err.Error()) + "\t"
}

// sglClickHouseShapedValue formats a value with its generation-time descriptor.
//
// Takes value (any) which is the caller-supplied value to adapt.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns string which is the encoded parameter or refusal text that ClickHouse rejects.
func sglClickHouseShapedValue(value any, shape sglClickHouseShape) string {
	text, err := sglClickHouseWhole(reflect.ValueOf(value), shape)
	if err != nil {
		return sglClickHouseRefusalCleaner.Replace(err.Error()) + "\t"
	}
	return text
}

// sglClickHouseEncoded returns a JSON parameter's value, whose Go type an override chose,
// as the document it encodes to, as pgx binds one: nil, the NULL of a Nullable type, for
// a nil pointer, slice, map or interface, and otherwise a value sglClickHouseValue writes
// as the text of its JSON encoding.
//
// Takes value (any) which is the caller-supplied value to adapt.
//
// Returns any which is a JSON formatting adapter, or nil for a nil value.
//
//sgl:when json
func sglClickHouseEncoded(value any) any {
	if value == nil {
		return nil
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		if reflected.IsNil() {
			return nil
		}
	}
	return sglClickHouseEncodedDocument{value: value}
}

// sglClickHouseNullable converts nil pointers to untyped nil before driver binding.
//
// It hands either driver a nullable parameter a runtime builder binds through a pointer,
// whose value the driver formats into the statement's text itself through the value's
// driver.Valuer: nil for a nil pointer, since the driver calls the driver.Valuer of a nil
// pointer to a type whose Value has a value receiver, such as *decimal.Decimal or
// *uuid.UUID, and panics; and the value as it is otherwise.
//
// Takes value (any) which is the caller-supplied value to adapt.
//
// Returns any which is nil for a nil pointer, or the unchanged value.
//
//sgl:when positional
func sglClickHouseNullable(value any) any {
	if reflected := reflect.ValueOf(value); reflected.Kind() == reflect.Pointer && reflected.IsNil() {
		return nil
	}
	return value
}

// sglClickHouseWhole writes a whole parameter value.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns string which is the encoded parameter text.
// Returns error when the value cannot be encoded for its SQL type.
func sglClickHouseWhole(value reflect.Value, shape sglClickHouseShape) (string, error) {
	current, err := sglClickHouseFollow(value, 0)
	if err != nil {
		return "", err
	}
	if !current.IsValid() {
		return "\\N", nil
	}
	shape = sglClickHouseResolve(current, shape)
	switch shape.kind {
	case '[', '{', '(':
		return sglClickHouseComposite(current, shape, 0)
	}
	text, err := sglClickHouseScalar(current, shape)
	if err != nil {
		return "", err
	}
	return sglClickHouseTextEscaper.Replace(text), nil
}

// sglClickHouseAppendElement writes one element into the collection's shared buffer.
//
// Takes text ([]byte) which is the output buffer to append to.
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns []byte which is the buffer with the encoded value appended.
// Returns error when the value cannot be encoded for its SQL type.
func sglClickHouseAppendElement(text []byte, value reflect.Value, shape sglClickHouseShape, depth int) ([]byte, error) {
	current, err := sglClickHouseFollow(value, depth)
	if err != nil {
		return nil, err
	}
	if !current.IsValid() {
		return append(text, "NULL"...), nil
	}
	shape = sglClickHouseResolve(current, shape)
	switch shape.kind {
	case '[', '{', '(':
		return sglClickHouseAppendComposite(text, current, shape, depth)
	}
	if shape.kind == 'n' && sglClickHouseIsNumber(current) {
		return sglClickHouseAppendNumber(text, current), nil
	}
	scalar, err := sglClickHouseScalar(current, shape)
	if err != nil {
		return nil, err
	}
	if shape.kind == 'n' {
		return append(text, scalar...), nil
	}
	text = append(text, '\'')
	text = append(text, sglClickHouseQuotedEscaper.Replace(scalar)...)
	return append(text, '\''), nil
}

// sglClickHouseAppendNumber writes numeric values without allocating their text.
//
// Takes text ([]byte) which is the output buffer to append to.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns []byte which is the buffer with the number appended.
func sglClickHouseAppendNumber(text []byte, value reflect.Value) []byte {
	const decimalBase = 10
	switch value.Kind() {
	case reflect.Bool:
		return strconv.AppendBool(text, value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.AppendInt(text, value.Int(), decimalBase)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.AppendUint(text, value.Uint(), decimalBase)
	}
	number := value.Float()
	switch {
	case math.IsNaN(number):
		return append(text, "nan"...)
	case math.IsInf(number, 1):
		return append(text, "inf"...)
	case math.IsInf(number, -1):
		return append(text, "-inf"...)
	}
	return strconv.AppendFloat(text, number, 'g', -1, value.Type().Bits())
}

// sglClickHouseFollow follows pointers and interfaces to the value they hold, returning
// the invalid value for nil.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns reflect.Value which is the underlying value, or an invalid value for nil.
// Returns error when following pointers exceeds the nesting limit.
func sglClickHouseFollow(value reflect.Value, depth int) (reflect.Value, error) {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}, nil
		}
		depth++
		if depth > sglClickHouseMaxDepth {
			return reflect.Value{}, fmt.Errorf("%w: it nests more than %d levels deep or refers to itself", errSglClickHouseValue, sglClickHouseMaxDepth)
		}
		value = value.Elem()
	}
	return value, nil
}

// sglClickHouseResolve chooses the shape of a value written with the a shape from the Go
// value itself, and returns any other shape as it is.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns sglClickHouseShape which is the supplied shape or the shape inferred from the
// Go value.
func sglClickHouseResolve(value reflect.Value, shape sglClickHouseShape) sglClickHouseShape {
	if shape.kind != 'a' {
		return shape
	}
	unknown := sglClickHouseShape{names: nil, elements: nil, digits: -1, kind: 'a', checked: false, driverMap: false, prepared: false}
	if _, moment := sglClickHouseMoment(value); moment {
		return sglClickHouseShape{names: nil, elements: nil, digits: -1, kind: 't', checked: false, driverMap: false, prepared: false}
	}
	if sglClickHouseIsNumber(value) {
		return sglClickHouseShape{names: nil, elements: nil, digits: -1, kind: 'n', checked: false, driverMap: false, prepared: false}
	}
	if sglClickHouseHasText(value) {
		return sglClickHouseShape{names: nil, elements: nil, digits: -1, kind: 'q', checked: false, driverMap: false, prepared: false}
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() != reflect.Uint8 {
			return sglClickHouseShape{names: nil, elements: []sglClickHouseShape{unknown}, digits: -1, kind: '[', checked: false, driverMap: false, prepared: false}
		}
	case reflect.Map:
		return sglClickHouseShape{names: nil, elements: []sglClickHouseShape{unknown, unknown}, digits: -1, kind: '{', checked: false, driverMap: false, prepared: false}
	case reflect.Struct:
		fields := make([]sglClickHouseShape, value.NumField())
		for index := range fields {
			fields[index] = unknown
		}
		return sglClickHouseShape{names: nil, elements: fields, digits: -1, kind: '(', checked: false, driverMap: false, prepared: false}
	}
	return sglClickHouseShape{names: nil, elements: nil, digits: -1, kind: 'q', checked: false, driverMap: false, prepared: false}
}

// sglClickHouseComposite writes an array, a map or a tuple.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns string which is the encoded parameter text.
// Returns error when the value cannot be encoded for its SQL type.
func sglClickHouseComposite(value reflect.Value, shape sglClickHouseShape, depth int) (string, error) {
	const initialCapacity = 64
	text, err := sglClickHouseAppendComposite(make([]byte, 0, initialCapacity), value, shape, depth)
	return string(text), err
}

// sglClickHouseAppendComposite writes nested collections into one buffer. Map entries
// retain their encoded text until sorted, preserving ordering even when keys encode
// alike.
//
// Takes text ([]byte) which is the output buffer to append to.
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns []byte which is the buffer with the encoded value appended.
// Returns error when the value cannot be encoded for its SQL type.
func sglClickHouseAppendComposite(text []byte, value reflect.Value, shape sglClickHouseShape, depth int) ([]byte, error) {
	if depth >= sglClickHouseMaxDepth {
		return nil, fmt.Errorf("%w: it nests more than %d levels deep or refers to itself", errSglClickHouseValue, sglClickHouseMaxDepth)
	}
	depth++
	switch shape.kind {
	case '[':
		return sglClickHouseAppendArray(text, value, shape, depth)
	case '{':
		return sglClickHouseAppendMap(text, value, shape, depth)
	default:
		return sglClickHouseAppendTuple(text, value, shape, depth)
	}
}

// sglClickHouseAppendArray appends a array to the shared buffer.
//
// Takes text ([]byte) which is the output buffer to append to.
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns []byte which is the buffer with the encoded value appended.
// Returns error when the value cannot be encoded for its SQL type.
func sglClickHouseAppendArray(text []byte, value reflect.Value, shape sglClickHouseShape, depth int) ([]byte, error) {
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return nil, fmt.Errorf("%w: an array needs a slice or an array, not %s", errSglClickHouseValue, value.Type())
	}
	text = append(text, '[')
	for index := 0; index < value.Len(); index++ {
		if index > 0 {
			text = append(text, ',')
		}
		var err error
		text, err = sglClickHouseAppendElement(text, value.Index(index), shape.elements[0], depth)
		if err != nil {
			return nil, err
		}
	}
	return append(text, ']'), nil
}

// sglClickHouseAppendMap appends a map with stable ordering to the shared buffer.
//
// Takes text ([]byte) which is the output buffer to append to.
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns []byte which is the buffer with the encoded value appended.
// Returns error when the value cannot be encoded for its SQL type.
func sglClickHouseAppendMap(text []byte, value reflect.Value, shape sglClickHouseShape, depth int) ([]byte, error) {
	if value.Kind() != reflect.Map {
		return nil, fmt.Errorf("%w: a map needs a Go map, not %s", errSglClickHouseValue, value.Type())
	}
	parts := make([]string, 0, value.Len())
	entries := value.MapRange()
	for entries.Next() {
		part, err := sglClickHouseAppendElement(nil, entries.Key(), shape.elements[0], depth)
		if err != nil {
			return nil, err
		}
		part = append(part, ':')
		part, err = sglClickHouseAppendElement(part, entries.Value(), shape.elements[1], depth)
		if err != nil {
			return nil, err
		}
		parts = append(parts, string(part))
	}
	slices.Sort(parts)
	text = append(text, '{')
	for index, part := range parts {
		if index > 0 {
			text = append(text, ',')
		}
		text = append(text, part...)
	}
	return append(text, '}'), nil
}

// sglClickHouseAppendTuple appends a tuple to the shared buffer.
//
// Takes text ([]byte) which is the output buffer to append to.
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns []byte which is the buffer with the encoded value appended.
// Returns error when the value cannot be encoded for its SQL type.
func sglClickHouseAppendTuple(text []byte, value reflect.Value, shape sglClickHouseShape, depth int) ([]byte, error) {
	elements, err := sglClickHouseTupleElements(value, shape)
	if err != nil {
		return nil, err
	}
	text = append(text, '(')
	for index, element := range elements {
		if index > 0 {
			text = append(text, ", "...)
		}
		text, err = sglClickHouseAppendElement(text, element, shape.elements[index], depth)
		if err != nil {
			return nil, err
		}
	}
	return append(text, ')'), nil
}

// sglClickHouseTupleElements lists a tuple's elements in the tuple's order, the invalid
// value standing for a field a map leaves out.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns []reflect.Value which holds the tuple's fields in descriptor order.
// Returns error when the value cannot supply the required tuple elements.
func sglClickHouseTupleElements(value reflect.Value, shape sglClickHouseShape) ([]reflect.Value, error) {
	var elements []reflect.Value
	switch value.Kind() {
	case reflect.Struct:
		for index := range value.NumField() {
			if !value.Type().Field(index).IsExported() {
				return nil, fmt.Errorf("%w: a tuple cannot be read from %s, which has unexported fields", errSglClickHouseValue, value.Type())
			}
			elements = append(elements, value.Field(index))
		}
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String || slices.Contains(shape.names, "") {
			return nil, fmt.Errorf("%w: a tuple is read from a map only by field names", errSglClickHouseValue)
		}
		for _, name := range shape.names {
			elements = append(elements, value.MapIndex(reflect.ValueOf(name).Convert(value.Type().Key())))
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			elements = append(elements, value.Index(index))
		}
	default:
		return nil, fmt.Errorf("%w: a tuple needs a struct, a map or a slice, not %s", errSglClickHouseValue, value.Type())
	}
	if len(elements) != len(shape.elements) {
		return nil, fmt.Errorf("%w: the tuple has %d fields but the value holds %d", errSglClickHouseValue, len(shape.elements), len(elements))
	}
	return elements, nil
}

// sglClickHouseScalar returns the text of a value that is neither an array, a map nor a
// tuple, before it is escaped or quoted. A number written bare comes from the number a Go
// number holds, never from a String method, so its text cannot leave the element.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns string which is the encoded parameter text.
// Returns error when the value cannot be encoded for its SQL type.
func sglClickHouseScalar(value reflect.Value, shape sglClickHouseShape) (string, error) {
	if shape.kind == '4' || shape.kind == '6' {
		address, isAddress, failure := sglClickHouseAddress(value, shape.kind)
		if failure != "" {
			return "", fmt.Errorf("%w: %s", errSglClickHouseValue, failure)
		}
		if isAddress {
			return address.String(), nil
		}
	}
	if moment, ok := sglClickHouseMoment(value); ok {
		return sglClickHouseTime(moment, shape)
	}
	if sglClickHouseWritesDurations {
		if duration, isDuration := reflect.TypeAssert[time.Duration](value); isDuration && shape.kind == 'q' {
			return sglClickHouseDuration(duration), nil
		}
	}
	if shape.kind == 'n' && sglClickHouseIsNumber(value) {
		return sglClickHouseNumber(value), nil
	}
	text, err := sglClickHouseText(value)
	if err == nil && shape.kind == 'n' && !sglClickHouseIsNumberText(text) {
		err = fmt.Errorf("%w: %q is not a number", errSglClickHouseValue, text)
	}
	return text, err
}

// sglClickHouseAddress validates an address against an IPv4 or IPv6 shape.
//
// It reads the IP address a netip.Addr or a net.IP holds as an address of a type whose
// shape is 4, which holds only IPv4 addresses, or 6, which holds either version. The zero
// netip.Addr and an empty net.IP hold no address and a net.IP of neither four nor sixteen
// bytes is not one, so each is refused with the reason, as is an IPv6 address for 4; an
// IPv4-mapped address comes back as IPv4 for 4, and every address without its zone, which
// no ClickHouse address holds. A value of any other type is reported as not an address,
// so the caller writes it as it is.
//
// Takes value (reflect.Value) which is the reflected source value.
// Takes kind (byte) which is the IPv4 or IPv6 shape marker.
//
// Returns netip.Addr which is the address without its zone, IPv4 when required.
// Returns bool which reports whether the source is an address type.
// Returns string which gives a refusal reason, or is empty on success.
func sglClickHouseAddress(value reflect.Value, kind byte) (netip.Addr, bool, string) {
	var address netip.Addr
	switch value.Type() {
	case reflect.TypeFor[netip.Addr]():
		reflect.ValueOf(&address).Elem().Set(value)
		if !address.IsValid() {
			return address, true, "the zero netip.Addr holds no address"
		}
	case reflect.TypeFor[net.IP]():
		bytes := value.Bytes()
		if len(bytes) == 0 {
			return address, true, "an empty net.IP holds no address"
		}
		parsed, valid := netip.AddrFromSlice(bytes)
		if !valid {
			return address, true, fmt.Sprintf("a net.IP of %d bytes is not an IP address", len(bytes))
		}
		address = parsed
	default:
		return address, false, ""
	}
	address = address.WithZone("")
	if kind != '4' {
		return address, true, ""
	}
	if address.Is6() && !address.Is4In6() {
		return address, true, address.String() + " is an IPv6 address, which an IPv4 value cannot hold"
	}
	return address.Unmap(), true, ""
}

// sglClickHouseMoment returns a value that is a time.Time.
//
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns time.Time which is the time value when recognised.
// Returns bool which reports whether the source holds a time.Time.
func sglClickHouseMoment(value reflect.Value) (time.Time, bool) {
	return reflect.TypeAssert[time.Time](value)
}

// sglClickHouseTime writes a time.Time for a shape.
//
// Takes moment (time.Time) which is the date or timestamp to format.
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
//
// Returns string which is the date, timestamp or RFC 3339 text required by the shape.
// Returns error when the shape requires a time.Time to be written as a number.
func sglClickHouseTime(moment time.Time, shape sglClickHouseShape) (string, error) {
	switch shape.kind {
	case 'd':
		return moment.Format("2006-01-02"), nil
	case 't':
		return sglClickHouseEpoch(moment, shape.digits), nil
	case 'q':
		return moment.Format(time.RFC3339Nano), nil
	default:
		return "", fmt.Errorf("%w: a time.Time cannot be written as a number", errSglClickHouseValue)
	}
}

// sglClickHouseDuration writes a time.Duration as the text ClickHouse parses for a Time
// or Time64 value: hours, which may pass 24, minutes and seconds, a minus sign before a
// negative span, and the fraction of a second it holds, without trailing zeros.
//
// Takes duration (time.Duration) which is the signed Go duration.
//
// Returns string which is the signed hours, minutes and seconds with any fractional part.
//
//sgl:when durations
func sglClickHouseDuration(duration time.Duration) string {
	sign := ""
	if duration < 0 {
		sign = "-"
	}
	magnitude := func(part time.Duration) int64 {
		if part < 0 {
			return -int64(part)
		}
		return int64(part)
	}
	text := fmt.Sprintf("%s%d:%02d:%02d", sign, magnitude(duration/time.Hour), magnitude(duration%time.Hour/time.Minute),
		magnitude(duration%time.Minute/time.Second))
	if fraction := magnitude(duration % time.Second); fraction != 0 {
		text += strings.TrimRight(fmt.Sprintf(".%09d", fraction), "0")
	}
	return text
}

// sglClickHouseEpoch writes a time as Unix seconds with a number of fraction digits, or
// with as many as the time needs when digits is negative, dropping anything finer.
//
// Takes moment (time.Time) which is the timestamp to format.
// Takes digits (int) which is the number of fractional second digits.
//
// Returns string which is the Unix timestamp at the requested precision.
func sglClickHouseEpoch(moment time.Time, digits int) string {
	const decimalBase = 10
	nanoseconds := int64(moment.Nanosecond())
	if digits < 0 {
		digits = sglClickHouseNanosecondDigits
		for digits > 0 && nanoseconds%int64(math.Pow10(sglClickHouseNanosecondDigits-digits+sglClickHouseDigitGroup)) == 0 {
			digits -= sglClickHouseDigitGroup
		}
	}
	seconds := moment.Unix()
	if digits == 0 {
		return strconv.FormatInt(seconds, decimalBase)
	}
	scale := int64(math.Pow10(digits))
	fraction := nanoseconds / int64(math.Pow10(sglClickHouseNanosecondDigits-digits))
	sign := ""
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
		if fraction > 0 {
			seconds--
			fraction = scale - fraction
		}
	}
	return fmt.Sprintf("%s%d.%0*d", sign, seconds, digits, fraction)
}

// sglClickHouseIsNumber reports a value of a Go number or boolean kind.
//
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns bool which reports whether the value has a numeric or boolean kind.
func sglClickHouseIsNumber(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// sglClickHouseNumber writes a value of a Go number or boolean kind from the number it
// holds, never from a String method.
//
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns string which is the number or boolean in ClickHouse text form.
func sglClickHouseNumber(value reflect.Value) string {
	const decimalBase = 10
	switch value.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), decimalBase)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(value.Uint(), decimalBase)
	}
	number := value.Float()
	switch {
	case math.IsNaN(number):
		return "nan"
	case math.IsInf(number, 1):
		return "inf"
	case math.IsInf(number, -1):
		return "-inf"
	}
	return strconv.FormatFloat(number, 'g', -1, value.Type().Bits())
}

// sglClickHouseMethods lists the values whose methods say what a value's text is: the
// value, and its pointer when it was reached through one.
//
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns []any which holds the value and any address through which methods can be
// called.
func sglClickHouseMethods(value reflect.Value) []any {
	if value.CanAddr() {
		return []any{value.Interface(), value.Addr().Interface()}
	}
	return []any{value.Interface()}
}

// sglClickHouseHasText reports a value with a String method, or an error, whose text the
// helper takes from its methods rather than its kind.
//
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns bool which reports whether the value supplies a supported text conversion.
func sglClickHouseHasText(value reflect.Value) bool {
	for _, candidate := range sglClickHouseMethods(value) {
		switch candidate.(type) {
		case fmt.Stringer, interface{ String() (string, error) }, error:
			return true
		}
	}
	return false
}

// sglClickHouseText returns the text of a value: what its String method returns, or by
// its kind a string, bytes, a boolean or a number. An error, a String method that fails
// and any other kind are refused.
//
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns string which is the encoded parameter text.
// Returns error when the value cannot be encoded for its SQL type.
func sglClickHouseText(value reflect.Value) (string, error) {
	for _, candidate := range sglClickHouseMethods(value) {
		switch typed := candidate.(type) {
		case fmt.Stringer:
			return typed.String(), nil
		case interface{ String() (string, error) }:
			text, err := typed.String()
			if err != nil {
				return "", fmt.Errorf("%w: its String method failed: %w", errSglClickHouseValue, err)
			}
			return text, nil
		case error:
			return "", fmt.Errorf("%w: an error is not a value", errSglClickHouseValue)
		}
	}
	switch value.Kind() {
	case reflect.String:
		return value.String(), nil
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			bytes := make([]byte, value.Len())
			reflect.Copy(reflect.ValueOf(bytes), value)
			return string(bytes), nil
		}
	}
	if sglClickHouseIsNumber(value) {
		return sglClickHouseNumber(value), nil
	}
	return "", fmt.Errorf("%w: a %s cannot be written as text", errSglClickHouseValue, value.Type())
}

// sglClickHouseIsNumberText reports text that is a number, nan, inf or a boolean, the
// only text written bare.
//
// Takes text (string) which is the candidate decimal number text.
//
// Returns bool which reports whether the text has the accepted numeric grammar.
func sglClickHouseIsNumberText(text string) bool {
	if text == "true" || text == "false" {
		return true
	}
	lower := strings.ToLower(sglClickHouseUnsigned(text))
	if lower == "nan" || lower == "inf" {
		return true
	}
	mantissa, exponent, scientific := strings.Cut(lower, "e")
	if scientific {
		exponent = sglClickHouseUnsigned(exponent)
		if exponent == "" || strings.Trim(exponent, "0123456789") != "" {
			return false
		}
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	return whole+fraction != "" && strings.Trim(whole+fraction, "0123456789") == ""
}

// sglClickHouseUnsigned returns text without one leading sign.
//
// Takes text (string) which is the possibly signed decimal text.
//
// Returns string which is the text without a leading plus or minus sign.
func sglClickHouseUnsigned(text string) string {
	if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
		return text[1:]
	}
	return text
}

// sglClickHouseParseShape reads a shape the generated code passes.
//
// Takes text (string) which is the complete encoded shape.
//
// Returns sglClickHouseShape which is the parsed descriptor.
// Returns error when the descriptor is malformed or has trailing text.
func sglClickHouseParseShape(text string) (sglClickHouseShape, error) {
	shape, rest, err := sglClickHouseReadShape(text, 0)
	if err == nil && rest != "" {
		err = fmt.Errorf("%w: %q has %q left over", errSglClickHouseShape, text, rest)
	}
	return shape, err
}

// sglClickHouseReadShape reads one shape from the start of text, returning what follows.
//
// Takes text (string) which is the unconsumed descriptor text.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns sglClickHouseShape which is the descriptor with the parsed elements.
// Returns string which is the remaining unconsumed descriptor text.
// Returns error when the descriptor is malformed or exceeds the nesting limit.
func sglClickHouseReadShape(text string, depth int) (sglClickHouseShape, string, error) {
	shape := sglClickHouseShape{names: nil, elements: nil, digits: -1, kind: 0, checked: false, driverMap: false, prepared: false}
	if text == "" || depth > sglClickHouseMaxDepth {
		return shape, "", fmt.Errorf("%w: a shape is missing or nests too deeply", errSglClickHouseShape)
	}
	shape.kind, text = text[0], text[1:]
	switch shape.kind {
	case 'q', 'n', 'd', 'a', '4', '6':
		return shape, text, nil
	case 't':
		shape.digits = 0
		if text != "" && text[0] >= '0' && text[0] <= '9' {
			shape.digits, text = int(text[0]-'0'), text[1:]
		}
		return shape, text, nil
	case '[':
		return sglClickHouseReadElements(shape, text, depth, ']')
	case '{':
		return sglClickHouseReadElements(shape, text, depth, '}')
	case '(':
		return sglClickHouseReadElements(shape, text, depth, ')')
	}
	return shape, "", fmt.Errorf("%w: %q is not a shape", errSglClickHouseShape, shape.kind)
}

// sglClickHouseReadElements reads the shapes inside an array, a map or a tuple, up to the
// bracket closing it: one for an array, a key and a value separated by a colon for a map,
// and any number separated by commas for a tuple.
//
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes text (string) which is the unconsumed descriptor text.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
// Takes closing (byte) which is the delimiter that ends this collection.
//
// Returns sglClickHouseShape which is the descriptor with the parsed elements.
// Returns string which is the remaining unconsumed descriptor text.
// Returns error when the descriptor is malformed or exceeds the nesting limit.
func sglClickHouseReadElements(shape sglClickHouseShape, text string, depth int, closing byte) (sglClickHouseShape, string, error) {
	for {
		var err error
		if shape, text, err = sglClickHouseReadElement(shape, text, depth); err != nil {
			return shape, "", err
		}
		if text == "" {
			return shape, "", fmt.Errorf("%w: %q is not closed", errSglClickHouseShape, string(shape.kind))
		}
		switch {
		case text[0] == closing && (shape.kind != '{' || len(shape.elements) == 2):
			return shape, text[1:], nil
		case shape.kind == '(' && text[0] == ',', shape.kind == '{' && text[0] == ':' && len(shape.elements) == 1:
			text = text[1:]
		default:
			return shape, "", fmt.Errorf("%w: %q is out of place", errSglClickHouseShape, text[:1])
		}
	}
}

// sglClickHouseReadElement reads one shape inside an array, a map or a tuple, with a
// tuple field's name before it, and returns what follows.
//
// Takes shape (sglClickHouseShape) which describes the SQL type and its nested elements.
// Takes text (string) which is the unconsumed descriptor text.
// Takes depth (int) which is the current nesting depth, checked against the recursion
// limit.
//
// Returns sglClickHouseShape which is the descriptor with the parsed elements.
// Returns string which is the remaining unconsumed descriptor text.
// Returns error when the descriptor is malformed or exceeds the nesting limit.
func sglClickHouseReadElement(shape sglClickHouseShape, text string, depth int) (sglClickHouseShape, string, error) {
	name := ""
	if shape.kind == '(' && strings.HasPrefix(text, "'") {
		var err error
		if name, text, err = sglClickHouseReadName(text); err != nil {
			return shape, "", err
		}
	}
	element, rest, err := sglClickHouseReadShape(text, depth+1)
	if err != nil {
		return shape, "", err
	}
	shape.elements = append(shape.elements, element)
	shape.names = append(shape.names, name)
	return shape, rest, nil
}

// sglClickHouseReadName reads a tuple field's quoted name and the = after it.
//
// Takes text (string) which starts at the quoted tuple element name.
//
// Returns name (string) which is the decoded tuple element name.
// Returns rest (string) which is the text following the name.
// Returns err (error) when the quoted name is malformed.
func sglClickHouseReadName(text string) (name string, rest string, err error) {
	var written strings.Builder
	index := 1
	for index < len(text) {
		if text[index] != '\'' {
			written.WriteByte(text[index])
			index++
			continue
		}
		if index+1 < len(text) && text[index+1] == '\'' {
			written.WriteByte('\'')
			index += 2
			continue
		}
		if index+1 < len(text) && text[index+1] == '=' {
			return written.String(), text[index+2:], nil
		}
		break
	}
	return "", "", fmt.Errorf("%w: a tuple field's name is not closed by '=", errSglClickHouseShape)
}
