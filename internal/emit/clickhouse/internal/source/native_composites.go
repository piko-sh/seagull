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
	"bytes"
	"database/sql"
	"encoding"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"strings"

	reading "seagull.sh/seagull/internal/emit/internal/valuereading/source"
)

const (
	// sglNativeReadsDocuments enables decoding of driver JSON documents.
	//
	// It reports that the package reads JSON documents, which the driver decodes into its
	// own chcol.JSON, through clickhouse_documents.go: a document, or a value holding one,
	// reads as that file reads it, before any other reading, whether the document is the
	// column, an element of an Array, a value of a Map or an element of a Tuple.
	//
	//sgl:inline readsDocuments
	sglNativeReadsDocuments = true

	// sglNativeReadsEntries reports that the package reads a Map column whose values are
	// JSON documents through sglNativeEntries, whose entries read as a map.
	//
	//sgl:inline readsEntries
	sglNativeReadsEntries = true
)

// sglNativeDocument stands for the type the driver decodes a JSON value into, chcol.JSON,
// which the rendered file names in its place.
//
//sgl:inline documentType
type sglNativeDocument struct{}

// sglNativeCompositeScanner reads a column the driver returns in its own forms into a
// typed destination.
type sglNativeCompositeScanner[T any] struct {
	// dest points to the value filled by the scan adapter.
	dest *T
}

// Scan implements sql.Scanner.
//
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the column value cannot be decoded into the destination.
func (scanner *sglNativeCompositeScanner[T]) Scan(source any) error {
	if err := sglReadNativeValue(reflect.ValueOf(scanner.dest).Elem(), source); err != nil {
		return fmt.Errorf("sgl: read %T into %T: %w", source, *scanner.dest, err)
	}
	return nil
}

// DeserializeClickHouseJSON reads a driver JSON document through the scan adapter.
//
// It implements chcol.JSONDeserializer: the driver reads a JSON column only into a
// chcol.JSON, a chcol.JSONDeserializer, a struct or a map, filling a struct, the adapter
// included, by its fields without consulting a sql.Scanner, so it hands the adapter the
// document here, which Scan reads as it reads a document inside another column. A NULL of
// a Nullable(JSON) column reaches Scan.
//
// Takes document (*sglNativeDocument) which is the document the driver decoded.
//
// Returns error when the document cannot be read into the destination.
//
//sgl:when documents
func (scanner *sglNativeCompositeScanner[T]) DeserializeClickHouseJSON(document *sglNativeDocument) error {
	return scanner.Scan(document)
}

// sglNativeEntries collects Map entries whose values are JSON documents.
//
// It takes the entries of a Map column whose values are JSON documents one by one, as the
// driver's OrderedMap: clickhouse-go v2.48 builds such a Map for any other destination by
// setting each value, a *chcol.JSON, into a map of chcol.JSON, and panics. The generated
// code scans the column into it and converts it after the scan; it is no sql.Scanner,
// which the driver would hand the Map it fails to build.
//
//sgl:when entries
type sglNativeEntries map[any]any

// Get returns the value of a key.
//
// Takes key (any) which is the key.
//
// Returns any which is the value.
// Returns bool which is false when the Map holds no such key.
//
//sgl:when entries
func (entries *sglNativeEntries) Get(key any) (any, bool) {
	value, found := (*entries)[key]
	return value, found
}

// Put records one entry.
//
// Takes key (any) which is the key.
// Takes value (any) which is the value.
//
//sgl:when entries
func (entries *sglNativeEntries) Put(key any, value any) {
	if *entries == nil {
		*entries = make(sglNativeEntries)
	}
	(*entries)[key] = value
}

// Keys returns the keys recorded, in no particular order.
//
// Returns <-chan any which yields each key and then closes.
//
//sgl:when entries
func (entries *sglNativeEntries) Keys() <-chan any {
	keys := make(chan any, len(*entries))
	for key := range *entries {
		keys <- key
	}
	close(keys)
	return keys
}

// sglReadClickHouseDocuments stands for the function of that name in
// clickhouse_documents.go, which a package reading JSON documents holds beside this file:
// it reads a value that is, or may hold, a document the driver decoded, and reports false
// for any other.
//
// Returns bool which is always false.
// Returns error which is always nil.
//
//sgl:when standIns
func sglReadClickHouseDocuments(_ reflect.Value, _ any) (bool, error) {
	return false, nil
}

// sglReadList stands for the function of that name in value_reading.go, which the package
// holds beside this file: it reads a list into a slice, each element through read.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
// Takes read (func(reflect.Value, any) error) which reads one element.
//
// Returns error when the source cannot be represented by the destination.
//
//sgl:when standIns
func sglReadList(target reflect.Value, value reflect.Value, read func(reflect.Value, any) error) error {
	return reading.ReadList(target, value, read)
}

// sglReadMap stands for the function of that name in value_reading.go: it reads a map
// into a map, each key and value through read.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
// Takes read (func(reflect.Value, any) error) which reads one key or value.
//
// Returns error when the source cannot be represented by the destination.
//
//sgl:when standIns
func sglReadMap(target reflect.Value, value reflect.Value, read func(reflect.Value, any) error) error {
	return reading.ReadMap(target, value, read)
}

// sglReadScalar stands for the function of that name in value_reading.go: it converts
// within scalar kinds, integers with overflow checks.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns error when the source cannot be represented by the destination.
//
//sgl:when standIns
func sglReadScalar(target reflect.Value, value reflect.Value) error {
	return reading.ReadScalar(target, value)
}

// sglUnreadable stands for the function of that name in value_reading.go: it returns the
// error of a value of one type a reader cannot read into another.
//
// Takes source (reflect.Type) which is the source Go type.
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns error which describes the unsupported source and destination types.
//
//sgl:when standIns
func sglUnreadable(source reflect.Type, target reflect.Type) error {
	return reading.Unreadable(source, target)
}

// sglScanNativeComposite adapts native ClickHouse values into a typed scan destination.
//
// It adapts dest into a sql.Scanner that reads the value the native clickhouse-go driver
// hands a scanner for a Map or Array column holding named tuples, a map or slice of its
// own Go types with each named tuple a map[string]any keyed by element name, into dest's
// slices, maps and structs at any depth. The driver reads such a map only into exactly
// the Go type it builds, never into a generated struct. A struct field is matched as the
// driver matches one: by its json name, then its ch name, then its Go name; an element no
// field matches is left out. A NULL leaves dest at its zero value.
//
// It also reads a column whose Go type an override chose, which the driver scans only
// into exactly its own Go types: integers with overflow checks, floats converted within
// floating-point kinds, a boolean into one over a boolean, text into one over a string or
// a []byte, an address into a string, as its text, and text into a type that unmarshals
// itself from text, such as a netip.Addr.
//
// Takes dest (*T) which receives the decoded value.
//
// Returns sql.Scanner which decodes a driver value into dest.
func sglScanNativeComposite[T any](dest *T) sql.Scanner {
	return &sglNativeCompositeScanner[T]{dest: dest}
}

// sglReadNativeColumn converts a native driver column into its typed destination.
//
// It converts the value the native clickhouse-go driver filled for a column in its own Go
// types, such as the map[string]any keyed by element name it fills for a named tuple,
// into dest, as sglScanNativeComposite converts the value the driver hands a scanner. The
// driver reads a tuple column into a struct by its fields without consulting a
// sql.Scanner, so the generated code scans such a column into the map and converts it
// after the scan.
//
// Takes dest (*T) which receives the decoded value.
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the source cannot be represented by the destination.
func sglReadNativeColumn[T any](dest *T, source any) error {
	if err := sglReadNativeValue(reflect.ValueOf(dest).Elem(), source); err != nil {
		return fmt.Errorf("sgl: read %T into %T: %w", source, *dest, err)
	}
	return nil
}

// sglReadNativeEntries reads the entries of a Map into target, a map, empty when the Map
// held none.
//
// Takes target (reflect.Value) which is the settable map.
// Takes entries (sglNativeEntries) which holds the entries.
//
// Returns error when an entry cannot be read.
//
//sgl:when entries
func sglReadNativeEntries(target reflect.Value, entries sglNativeEntries) error {
	if entries == nil {
		entries = sglNativeEntries{}
	}
	return sglReadMap(target, reflect.ValueOf(map[any]any(entries)), sglReadNativeValue)
}

// sglReadNativeValue reads one of the driver's values into target: as it is when its type
// fits, through a pointer, through target's own sql.Scanner, as a netip.Addr from the
// driver's net.IP, and otherwise by shape.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the source cannot be represented by the destination.
func sglReadNativeValue(target reflect.Value, source any) error {
	value := reflect.ValueOf(source)
	if sglNativeNull(value) {
		target.SetZero()
		return nil
	}
	if sglNativeReadsDocuments {
		if read, err := sglReadNativeDocuments(target, source); read {
			return err
		}
	}
	if value.Type().AssignableTo(target.Type()) {
		target.Set(value)
		return nil
	}
	if target.Kind() == reflect.Pointer {
		element := reflect.New(target.Type().Elem())
		if err := sglReadNativeValue(element.Elem(), source); err != nil {
			return err
		}
		target.Set(element)
		return nil
	}
	if value.Kind() == reflect.Pointer {
		return sglReadNativeValue(target, value.Elem().Interface())
	}
	address := target.Addr().Interface()
	if scanner, isScanner := address.(sql.Scanner); isScanner {
		return scanner.Scan(source)
	}
	if ip, isIP := source.(net.IP); isIP && target.Type() == reflect.TypeFor[netip.Addr]() {
		return sglReadNativeAddress(target, ip)
	}
	return sglReadNativeShape(target, value)
}

// sglNativeNull reports whether one of the driver's values is NULL: nil, or a nil
// pointer.
//
// Takes value (reflect.Value) which is the value.
//
// Returns bool which is true for NULL.
func sglNativeNull(value reflect.Value) bool {
	return !value.IsValid() || value.Kind() == reflect.Pointer && value.IsNil()
}

// sglReadNativeDocuments reads a value the driver hands for a JSON document, or a value
// holding one, as clickhouse_documents.go reads it, and the entries of a Map of documents
// as a map, reporting false for any other value.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes source (any) which is the driver's value.
//
// Returns bool which is false when the value is no document and holds none.
// Returns error when the value cannot be read into target.
//
//sgl:when documents
func sglReadNativeDocuments(target reflect.Value, source any) (bool, error) {
	if sglNativeReadsEntries {
		if entries, isEntries := source.(sglNativeEntries); isEntries {
			return true, sglReadNativeEntries(target, entries)
		}
	}
	return sglReadClickHouseDocuments(target, source)
}

// sglReadNativeShape reads text into a type that holds text, and otherwise a value into a
// struct, a map or a slice by its parts, or into a type of the value's own kind, such as
// an enum's string type.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns error when the source cannot be represented by the destination.
func sglReadNativeShape(target reflect.Value, value reflect.Value) error {
	if read, err := sglReadNativeText(target, value); read {
		return err
	}
	switch target.Kind() {
	case reflect.Struct:
		return sglReadNativeStruct(target, value)
	case reflect.Map:
		return sglReadMap(target, value, sglReadNativeValue)
	case reflect.Slice:
		return sglReadList(target, value, sglReadNativeValue)
	default:
		return sglReadScalar(target, value)
	}
}

// sglReadNativeText reads text, an address's included, into a type that unmarshals itself
// from text, a string or a []byte, reporting false when the value is not text or the
// target holds none.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns bool which reports whether a text conversion was attempted.
// Returns error when the text cannot be decoded into the destination.
func sglReadNativeText(target reflect.Value, value reflect.Value) (bool, error) {
	text, isText := sglNativeText(value)
	if !isText {
		return false, nil
	}
	if unmarshaler, unmarshals := reflect.TypeAssert[encoding.TextUnmarshaler](target.Addr()); unmarshals {
		return true, unmarshaler.UnmarshalText(text)
	}
	switch {
	case target.Kind() == reflect.String:
		target.SetString(string(text))
		return true, nil
	case target.Kind() == reflect.Slice && target.Type().Elem().Kind() == reflect.Uint8:
		target.SetBytes(bytes.Clone(text))
		return true, nil
	default:
		return false, nil
	}
}

// sglNativeText returns the text a value is: the text a value that marshals itself as
// text gives, such as a net.IP's dotted address, the bytes of a string, or a []byte.
//
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns []byte which holds the text bytes when recognised.
// Returns bool which reports whether the value supplies text, bytes or an address.
func sglNativeText(value reflect.Value) ([]byte, bool) {
	if marshaler, marshals := reflect.TypeAssert[encoding.TextMarshaler](value); marshals {
		text, err := marshaler.MarshalText()
		return text, err == nil
	}
	switch {
	case value.Kind() == reflect.String:
		return []byte(value.String()), true
	case value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8:
		return value.Bytes(), true
	default:
		return nil, false
	}
}

// sglReadNativeAddress reads the driver's net.IP of an IPv4 or IPv6 value into a
// netip.Addr, as the driver reads such a column itself: four bytes as an IPv4 address and
// sixteen as an IPv6 one.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes address (net.IP) which is the driver's IP address.
//
// Returns error when the source cannot be represented by the destination.
func sglReadNativeAddress(target reflect.Value, address net.IP) error {
	parsed, valid := netip.AddrFromSlice(address)
	if !valid {
		return fmt.Errorf("cannot read the address %v into %s", address, target.Type())
	}
	target.Set(reflect.ValueOf(parsed))
	return nil
}

// sglReadNativeStruct reads a named tuple, which the driver returns as a map keyed by
// element name, into a struct.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns error when the source cannot be represented by the destination.
func sglReadNativeStruct(target reflect.Value, value reflect.Value) error {
	if value.Kind() != reflect.Map || value.Type().Key().Kind() != reflect.String {
		return sglUnreadable(value.Type(), target.Type())
	}
	iterator := value.MapRange()
	for iterator.Next() {
		name := iterator.Key().String()
		field, found := sglNativeField(target, name)
		if !found {
			continue
		}
		if err := sglReadNativeValue(field, iterator.Value().Interface()); err != nil {
			return fmt.Errorf("element %s: %w", name, err)
		}
	}
	return nil
}

// sglNativeField returns the field of a struct the driver fills from the element of a
// name: the first field whose json or ch name is the name, or else the exported field of
// that Go name.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes name (string) which is the tuple element name to match.
//
// Returns reflect.Value which is the matching destination field, if found.
// Returns bool which reports whether a matching field exists.
func sglNativeField(target reflect.Value, name string) (reflect.Value, bool) {
	structType := target.Type()
	for index := range structType.NumField() {
		field := structType.Field(index)
		if !field.IsExported() {
			continue
		}
		if sglNativeTagName(field.Tag.Get("json")) == name || sglNativeTagName(field.Tag.Get("ch")) == name {
			return target.Field(index), true
		}
	}
	field, found := structType.FieldByName(name)
	if !found || !field.IsExported() || len(field.Index) != 1 {
		return reflect.Value{}, false
	}
	return target.FieldByIndex(field.Index), true
}

// sglNativeTagName returns the name a struct tag gives, without its options.
//
// Takes tag (string) which is the complete struct tag value.
//
// Returns string which is the name before any comma-separated options.
func sglNativeTagName(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	return name
}
