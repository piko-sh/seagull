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
	"encoding"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	reading "seagull.sh/seagull/internal/emit/internal/valuereading/source"
)

const (
	// sglMicrosecondsPerHour is the number of microseconds in an hour.
	//
	//sgl:when reads ownTypes
	sglMicrosecondsPerHour = 3_600_000_000

	// sglMicrosecondsPerMinute is the number of microseconds in a minute.
	//
	//sgl:when reads ownTypes
	sglMicrosecondsPerMinute = 60_000_000

	// sglMicrosecondsPerSecond is the number of microseconds in a second.
	//
	//sgl:when reads ownTypes
	sglMicrosecondsPerSecond = 1_000_000

	// sglMonthsPerYear is the number of months in a year.
	//
	//sgl:when reads ownTypes
	sglMonthsPerYear = 12

	// sglMinus is the sign written before a negative number.
	//
	//sgl:when reads ownTypes
	sglMinus = "-"

	// sglDriverMaxDepth bounds how many pointers and nested maps, slices and structs the
	// parameter conversion follows, so a value that refers to itself is refused instead of
	// followed for ever.
	//
	//sgl:when bind
	sglDriverMaxDepth = 32

	// sglDriverReadsOwnTypes reports that the driver returns a DECIMAL and an INTERVAL in
	// types of its own, which the scan adapters read through their text.
	//
	//sgl:when reads
	//sgl:inline readsOwnTypes
	sglDriverReadsOwnTypes = true

	// sglDriverReadsDocuments reports that the driver hands a JSON value inside a list, a
	// map or a tuple as a document of its own, clickhouse-go's chcol.JSON, which
	// clickhouse_documents.go reads, and an element that is NULL as a nil pointer.
	//
	//sgl:when reads
	//sgl:inline readsDocuments
	sglDriverReadsDocuments = true
)

// sglDriverMap stands for the map type the driver binds a MAP from.
//
//sgl:when bind
//sgl:inline mapType
type sglDriverMap map[any]any

// sglDriverInterval stands for the type the driver returns an INTERVAL in.
//
//sgl:when reads ownTypes
//sgl:inline intervalType
type sglDriverInterval struct {
	// Days is the signed day component of the interval.
	Days int32

	// Months is the signed month component of the interval.
	Months int32

	// Micros is the signed duration component in microseconds.
	Micros int64
}

// sglDriverDecimal stands for the type the driver returns a DECIMAL in: the unscaled
// integer, its scale and the type's width.
//
//sgl:when reads ownTypes
//sgl:inline decimalType
type sglDriverDecimal struct {
	// Value is the unscaled integer coefficient.
	Value *big.Int

	// Width is the decimal type's total digit precision.
	Width uint8

	// Scale is the number of fractional decimal digits.
	Scale uint8
}

// sglDriverRefusal stands in for a parameter whose conversion failed: database/sql asks
// it for its value when it binds the parameter, and it returns the conversion's error, so
// the statement fails with that error instead of binding a wrong value.
//
//sgl:when bind
type sglDriverRefusal struct {
	// err is the conversion failure reported when the value is bound.
	err error
}

// Value returns the error the conversion failed with.
//
// Returns driver.Value which is always nil.
// Returns error which is the original parameter conversion failure.
//
//sgl:when bind
func (refusal sglDriverRefusal) Value() (driver.Value, error) {
	return nil, refusal.err
}

// sglDriverList lets DuckDB infer list bindings from their Go elements.
//
// It is a list the DuckDB driver binds by the types of its elements: it builds a list
// parameter from the type the statement gives it, which fails on an element such as an
// ENUM, a DECIMAL, a BLOB or a UUID, but binds a list a driver.Valuer returns by the
// types of the Go values it holds, and DuckDB casts that list to the parameter's type.
//
//sgl:when bind lists
type sglDriverList []any

// Value returns the list's elements.
//
// Returns driver.Value which holds the list elements as a slice.
// Returns error which is always nil.
//
//sgl:when bind lists
func (list sglDriverList) Value() (driver.Value, error) {
	return []any(list), nil
}

// sglDriverCompositeScanner reads a column the driver returns in its own forms into a
// typed destination.
//
//sgl:when scan
type sglDriverCompositeScanner[T any] struct {
	// dest points to the value filled by the scan adapter.
	dest *T
}

// Scan implements sql.Scanner.
//
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the column value cannot be decoded into the destination.
//
//sgl:when scan
func (scanner sglDriverCompositeScanner[T]) Scan(source any) error {
	if err := sglReadDriverValue(reflect.ValueOf(scanner.dest).Elem(), source); err != nil {
		return fmt.Errorf("sgl: read %T into %T: %w", source, *scanner.dest, err)
	}
	return nil
}

// sglOverrideScanner reads a column into the Go type an override chose.
//
//sgl:when override
type sglOverrideScanner[T any] struct {
	// dest points to the value filled by the scan adapter.
	dest *T
}

// Scan implements sql.Scanner, returning database/sql's own error when neither it nor the
// override contract reads the value.
//
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the column value cannot be decoded into the destination.
//
//sgl:when override
func (scanner sglOverrideScanner[T]) Scan(source any) error {
	target := reflect.ValueOf(scanner.dest).Elem()
	if source == nil {
		return sglReadDriverNull(target)
	}
	var read sql.Null[T]
	err := read.Scan(source)
	if err == nil {
		*scanner.dest = read.V
		return nil
	}
	if sglReadOverride(target, source) != nil {
		return err
	}
	return nil
}

// sglOverrideJSONScanner reads a JSON column into the Go type an override chose.
//
//sgl:when overrideJSON
type sglOverrideJSONScanner[T any] struct {
	// dest points to the value filled by the scan adapter.
	dest *T
}

// Scan implements sql.Scanner.
//
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the column value cannot be decoded into the destination.
//
//sgl:when overrideJSON
func (scanner sglOverrideJSONScanner[T]) Scan(source any) error {
	if err := sglReadOverrideJSON(reflect.ValueOf(scanner.dest).Elem(), source); err != nil {
		return fmt.Errorf("sgl: read a JSON document into %T: %w", *scanner.dest, err)
	}
	return nil
}

// sglReadClickHouseDocuments stands for the function of that name in
// clickhouse_documents.go, which a package whose driver hands documents of its own holds
// beside this file: it reads a value that is, or may hold, a document the driver decoded,
// and reports false for any other.
//
// Returns bool which is always false.
// Returns error which is always nil.
//
//sgl:when standIns
func sglReadClickHouseDocuments(_ reflect.Value, _ any) (bool, error) {
	return false, nil
}

// sglReadList stands for the function of that name in value_reading.go, which a package
// reading the driver's values holds beside this file: it reads a list into a slice, each
// element through read.
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

// sglReadMap stands for the function of that name in value_reading.go: it reads a Go map
// or the driver's ordered map into a map, each key and value through read.
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

// sglDriverComposite adapts nested parameters to DuckDB driver values.
//
// It hands the DuckDB driver a MAP, STRUCT or LIST parameter in the forms it binds: a map
// as a duckdb.Map, a struct as a map keyed by its fields' json names, and a slice as a
// []any, converting nested values the same way. A nil map, slice or pointer binds as
// NULL. A driver.Valuer is converted through its Value, and when that fails, at any
// depth, the whole parameter is refused with the Valuer's error; a value of the driver's
// own types, a byte slice, and a struct with unexported fields, such as a time.Time, pass
// through unchanged. A value nesting more than sglDriverMaxDepth levels deep, or one that
// refers to itself, is refused.
//
// Takes value (any) which is the caller-supplied value to adapt.
//
// Returns any which holds the adapted value or an error-bearing driver.Valuer.
//
//sgl:when bind
func sglDriverComposite(value any) any {
	converted, err := sglDriverConvert(value, 0)
	if err != nil {
		return sglDriverRefusal{err: err}
	}
	return converted
}

// sglDriverInferredList adapts a composite parameter while retaining list inference.
//
// It hands the DuckDB driver a LIST or ARRAY parameter whose elements are of a type it
// cannot build from the type the statement gives the parameter: each element is converted
// as sglDriverComposite converts it, so an enum becomes a string and a Valuer, such as a
// decimal or a UUID, the value it writes, and the list is handed over as an
// sglDriverList, which DuckDB casts to the parameter's type. The outer slice is always a
// list, a []uint8 of UTINYINT values included, while a byte slice inside it is a BLOB. A
// nil slice or pointer binds as NULL, and any other value as sglDriverComposite binds it.
// A chain of more than sglDriverMaxDepth pointers, or one that refers to itself, is
// refused.
//
// Takes value (any) which is the caller-supplied value to adapt.
//
// Returns any which adapts nested composite values while preserving list inference.
//
//sgl:when bind lists
func sglDriverInferredList(value any) any {
	reflected := reflect.ValueOf(value)
	depth := 0
	for reflected.Kind() == reflect.Pointer && !reflected.IsNil() {
		if _, ok := value.(driver.Valuer); ok {
			break
		}
		depth++
		if depth > sglDriverMaxDepth {
			return sglDriverRefusal{err: sglDriverTooDeep()}
		}
		value = reflected.Elem().Interface()
		reflected = reflect.ValueOf(value)
	}
	if _, ok := value.(driver.Valuer); ok || reflected.Kind() != reflect.Slice {
		return sglDriverComposite(value)
	}
	if reflected.IsNil() {
		return nil
	}
	elements := make([]any, reflected.Len())
	for index := range reflected.Len() {
		element, err := sglDriverConvert(reflected.Index(index).Interface(), depth+1)
		if err != nil {
			return sglDriverRefusal{err: err}
		}
		elements[index] = element
	}
	return sglDriverList(elements)
}

// sglDriverConvert converts one value, or returns the error of a Valuer inside it.
//
// Takes value (any) which is the caller-supplied value to adapt.
// Takes depth (int) which counts the pointers, maps, slices and structs followed to reach
// the value, checked against sglDriverMaxDepth.
//
// Returns any which is the value in the driver's supported Go forms.
// Returns error when a nested value cannot be converted, or nests too deep.
//
//sgl:when bind
func sglDriverConvert(value any, depth int) (any, error) {
	if value == nil {
		return nil, nil
	}
	if depth > sglDriverMaxDepth {
		return nil, sglDriverTooDeep()
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer && reflected.IsNil() {
		return nil, nil
	}
	if valuer, ok := value.(driver.Valuer); ok {
		return valuer.Value()
	}
	if reflected.Kind() == reflect.Pointer {
		return sglDriverConvert(reflected.Elem().Interface(), depth+1)
	}
	if reflected.Type().PkgPath() == reflect.TypeFor[sglDriverMap]().PkgPath() {
		return value, nil
	}
	switch reflected.Kind() {
	case reflect.Map:
		return sglDriverCompositeMap(reflected, depth)
	case reflect.Slice:
		return sglDriverCompositeSlice(reflected, depth)
	case reflect.Struct:
		return sglDriverCompositeStruct(reflected, value, depth)
	default:
		return sglDriverBasic(reflected), nil
	}
}

// sglDriverTooDeep returns the error of a parameter nesting more than sglDriverMaxDepth
// levels deep, which is how a value that refers to itself shows.
//
// Returns error which names the limit.
//
//sgl:when bind
func sglDriverTooDeep() error {
	return fmt.Errorf("sgl: a parameter nests more than %d levels deep or refers to itself", sglDriverMaxDepth)
}

// sglDriverBasic returns a value of a defined type over a string, boolean, integer or
// float kind as the predeclared type of its kind and width, which the driver binds where
// it refuses the defined type, and any other value, a predeclared one included, as it is.
//
// Takes reflected (reflect.Value) which is the reflected source value.
//
// Returns any which is the corresponding basic Go value, or the original value.
//
//sgl:when bind
func sglDriverBasic(reflected reflect.Value) any {
	if reflected.Type().PkgPath() == "" {
		return reflected.Interface()
	}
	switch reflected.Kind() {
	case reflect.String:
		return reflected.String()
	case reflect.Bool:
		return reflected.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return reflected.Convert(sglDriverNumberType(reflected.Kind())).Interface()
	default:
		return reflected.Interface()
	}
}

// sglDriverNumberType returns the predeclared type of a number kind.
//
// Takes kind (reflect.Kind) which is the Go reflection kind to classify.
//
// Returns reflect.Type which is the built-in numeric type, defaulting to int64.
//
//sgl:when bind
func sglDriverNumberType(kind reflect.Kind) reflect.Type {
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

// sglDriverCompositeMap copies a map into a duckdb.Map, converting each value, and each
// key of a defined type over a basic kind into the predeclared type of its kind.
//
// Takes reflected (reflect.Value) which is the reflected source value.
// Takes depth (int) which counts the levels followed to reach the map.
//
// Returns any which is the value in the driver's supported Go forms.
// Returns error when a nested value cannot be converted.
//
//sgl:when bind
func sglDriverCompositeMap(reflected reflect.Value, depth int) (any, error) {
	if reflected.IsNil() {
		return nil, nil
	}
	converted := make(sglDriverMap, reflected.Len())
	entries := reflected.MapRange()
	for entries.Next() {
		element, err := sglDriverConvert(entries.Value().Interface(), depth+1)
		if err != nil {
			return nil, err
		}
		converted[sglDriverBasic(entries.Key())] = element
	}
	return converted, nil
}

// sglDriverCompositeSlice copies a slice into a []any, converting each element. A byte
// slice passes through as a []byte.
//
// Takes reflected (reflect.Value) which is the reflected source value.
// Takes depth (int) which counts the levels followed to reach the slice.
//
// Returns any which is the value in the driver's supported Go forms.
// Returns error when a nested value cannot be converted.
//
//sgl:when bind
func sglDriverCompositeSlice(reflected reflect.Value, depth int) (any, error) {
	if reflected.IsNil() {
		return nil, nil
	}
	if reflected.Type().Elem().Kind() == reflect.Uint8 {
		return reflected.Bytes(), nil
	}
	converted := make([]any, reflected.Len())
	for index := range reflected.Len() {
		element, err := sglDriverConvert(reflected.Index(index).Interface(), depth+1)
		if err != nil {
			return nil, err
		}
		converted[index] = element
	}
	return converted, nil
}

// sglDriverCompositeStruct copies a struct into a map keyed by its fields' json names, or
// their Go names when they have none, converting each field. A struct with an unexported
// field passes through unchanged.
//
// Takes reflected (reflect.Value) which is the reflected source value.
// Takes value (any) which is the caller-supplied value to adapt.
// Takes depth (int) which counts the levels followed to reach the struct.
//
// Returns any which is the value in the driver's supported Go forms.
// Returns error when a nested value cannot be converted.
//
//sgl:when bind
func sglDriverCompositeStruct(reflected reflect.Value, value any, depth int) (any, error) {
	structType := reflected.Type()
	fields := make(map[string]any, structType.NumField())
	for index := range structType.NumField() {
		field := structType.Field(index)
		if !field.IsExported() {
			return value, nil
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		element, err := sglDriverConvert(reflected.Field(index).Interface(), depth+1)
		if err != nil {
			return nil, err
		}
		fields[name] = element
	}
	return fields, nil
}

// sglScanDriverComposite adapts composite driver values into a typed destination.
//
// It adapts dest into a sql.Scanner that reads the value the driver returns for a list,
// struct or map column in forms of its own into dest's slices, structs and maps at any
// depth: the DuckDB driver's []any, map keyed by field name or duckdb.OrderedMap holding
// its own values, or clickhouse-go's slices, maps and tuples holding the JSON documents
// it decodes. A struct field is matched by its json name, or its Go name when it has
// none, ignoring case when no name matches exactly. It also reads a value the driver
// returns in a type of its own where the query could not cast it to text: a DECIMAL
// through its exact text, an INTERVAL as the text DuckDB casts it to, and a JSON value
// encoded again. A NULL, the column's or an element's or a map value's, reads only into a
// Go type that holds it, as sglReadDriverNull describes.
//
// Takes dest (*T) which receives the decoded value.
//
// Returns sql.Scanner which decodes a driver value into dest.
//
//sgl:when scan
func sglScanDriverComposite[T any](dest *T) sql.Scanner {
	return sglDriverCompositeScanner[T]{dest: dest}
}

// sglScanOverride adapts driver values into an overridden Go destination.
//
// It adapts dest, the destination of a column whose Go type an override chose, into a
// sql.Scanner that reads the driver's value as database/sql reads it into dest, and,
// where database/sql cannot, as the override contract asks: text, an address's included,
// into a type that unmarshals itself from text, a defined string type or a defined []byte
// type such as json.RawMessage; an integer or text that database/sql reads as a boolean
// into a defined boolean type; and a list or a map of the driver's values into a slice or
// map of defined types, element by element. A NULL reads as database/sql reads it.
//
// Takes dest (*T) which receives the decoded value.
//
// Returns sql.Scanner which decodes a driver value into dest.
//
//sgl:when override
func sglScanOverride[T any](dest *T) sql.Scanner {
	return sglOverrideScanner[T]{dest: dest}
}

// sglScanOverrideJSON adapts JSON driver values into an overridden Go destination.
//
// It adapts dest, the destination of a JSON column whose Go type an override chose, into
// a sql.Scanner that reads the column as pgx reads one: through the type's own Scan
// method when it has one, and otherwise by decoding the document into it, so a plain
// struct, slice or map holds the document. The driver's text or bytes are the document;
// any other value, a document the driver decoded, is encoded again first. A NULL reads as
// database/sql reads one, as sglReadDriverNull describes.
//
// Takes dest (*T) which receives the decoded value.
//
// Returns sql.Scanner which decodes a driver value into dest.
//
//sgl:when overrideJSON
func sglScanOverrideJSON[T any](dest *T) sql.Scanner {
	return sglOverrideJSONScanner[T]{dest: dest}
}

// sglReadOverrideJSON reads a JSON column's value into an override's Go type, as
// sglScanOverrideJSON describes, through a pointer for a nullable column.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the source cannot be represented by the destination.
//
//sgl:when overrideJSON
func sglReadOverrideJSON(target reflect.Value, source any) error {
	if scanner, scans := reflect.TypeAssert[sql.Scanner](target.Addr()); scans {
		return scanner.Scan(source)
	}
	if source == nil {
		return sglReadDriverNull(target)
	}
	if target.Kind() == reflect.Pointer {
		element := reflect.New(target.Type().Elem())
		if err := sglReadOverrideJSON(element.Elem(), source); err != nil {
			return err
		}
		target.Set(element)
		return nil
	}
	var document []byte
	switch typed := source.(type) {
	case []byte:
		document = typed
	case string:
		document = []byte(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		document = encoded
	}
	return json.Unmarshal(document, target.Addr().Interface())
}

// sglReadOverride reads a value database/sql cannot store into an override's Go type, as
// sglScanOverride describes, through a pointer for a nullable column.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the source cannot be represented by the destination.
//
//sgl:when override
func sglReadOverride(target reflect.Value, source any) error {
	if target.Kind() == reflect.Pointer {
		element := reflect.New(target.Type().Elem())
		if err := sglReadOverride(element.Elem(), source); err != nil {
			return err
		}
		target.Set(element)
		return nil
	}
	if read, err := sglReadOverrideText(target, source); read {
		return err
	}
	if target.Kind() == reflect.Bool {
		converted, err := driver.Bool.ConvertValue(source)
		if err != nil {
			return err
		}
		isTrue, isBool := converted.(bool)
		target.SetBool(isBool && isTrue)
		return nil
	}
	return sglReadDriverValue(target, source)
}

// sglReadOverrideText reads text, the text of a value that marshals itself as text, such
// as a net.IP's dotted address, included, into a type that unmarshals itself from text, a
// defined string type or a defined []byte type, reporting false when the value is not
// text or the target holds none.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes source (any) which is the value supplied by the driver.
//
// Returns bool which reports whether the source was handled as text.
// Returns error when the text cannot be decoded into the destination.
//
//sgl:when override
func sglReadOverrideText(target reflect.Value, source any) (bool, error) {
	var text []byte
	switch typed := source.(type) {
	case encoding.TextMarshaler:
		marshalled, err := typed.MarshalText()
		if err != nil {
			return false, nil
		}
		text = marshalled
	case string:
		text = []byte(typed)
	case []byte:
		text = typed
	default:
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
		target.SetBytes(append([]byte(nil), text...))
		return true, nil
	default:
		return false, nil
	}
}

// sglReadDriverValue reads one of the driver's values into target.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the source cannot be represented by the destination.
//
//sgl:when reads
func sglReadDriverValue(target reflect.Value, source any) error {
	if source == nil {
		return sglReadDriverNull(target)
	}
	value := reflect.ValueOf(source)
	if sglDriverReadsDocuments {
		if value.Kind() == reflect.Pointer && value.IsNil() {
			target.SetZero()
			return nil
		}
		if read, err := sglReadClickHouseDocuments(target, source); read {
			return err
		}
	}
	if value.Type().AssignableTo(target.Type()) {
		target.Set(value)
		return nil
	}
	if target.Kind() == reflect.Pointer {
		element := reflect.New(target.Type().Elem())
		if err := sglReadDriverValue(element.Elem(), source); err != nil {
			return err
		}
		target.Set(element)
		return nil
	}
	if read, err := sglReadDriverLeaf(target, source); read {
		return err
	}
	return sglReadDriverShape(target, value)
}

// sglReadDriverNull reads a NULL column, list element or map value as the zero value of a
// pointer, slice, map or interface, through another type's own sql.Scanner, and as an
// error into any other type, which would otherwise hold a zero value the database does
// not hold.
//
// Takes target (reflect.Value) which is the settable destination.
//
// Returns error when the destination cannot hold NULL.
//
//sgl:when reads
func sglReadDriverNull(target reflect.Value) error {
	switch target.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		target.SetZero()
		return nil
	}
	if scanner, scans := reflect.TypeAssert[sql.Scanner](target.Addr()); scans {
		return scanner.Scan(nil)
	}
	return fmt.Errorf("converting NULL to %s is unsupported", target.Type())
}

// sglReadDriverShape reads a list into a slice, a map keyed by field name into a struct,
// a map into a map and a scalar into a scalar of the same kind.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns error when the source cannot be represented by the destination.
//
//sgl:when reads
func sglReadDriverShape(target reflect.Value, value reflect.Value) error {
	switch target.Kind() {
	case reflect.Slice:
		return sglReadList(target, value, sglReadDriverValue)
	case reflect.Map:
		return sglReadMap(target, value, sglReadDriverValue)
	case reflect.Struct:
		return sglReadDriverStruct(target, value)
	default:
		return sglReadScalar(target, value)
	}
}

// sglReadDriverLeaf reads a value whose type is the driver's own or one the target reads
// itself: a decimal, an interval read as text, a JSON value, or a value of a type that
// scans itself. It reports false when it does not read the value.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes source (any) which is the value supplied by the driver.
//
// Returns bool which reports whether a driver or destination adapter handled the value.
// Returns error when that adapter cannot decode the value.
//
//sgl:when reads
func sglReadDriverLeaf(target reflect.Value, source any) (bool, error) {
	if sglDriverReadsOwnTypes {
		switch typed := source.(type) {
		case sglDriverDecimal:
			return true, sglReadDriverDecimal(target, typed)
		case sglDriverInterval:
			if target.Kind() == reflect.String {
				target.SetString(sglDriverIntervalText(typed))
				return true, nil
			}
			return true, sglUnreadable(reflect.TypeOf(source), target.Type())
		}
	}
	address := target.Addr().Interface()
	if unmarshaler, ok := address.(json.Unmarshaler); ok {
		encoded, err := json.Marshal(source)
		if err != nil {
			return true, err
		}
		return true, unmarshaler.UnmarshalJSON(encoded)
	}
	if scanner, ok := address.(sql.Scanner); ok {
		return true, scanner.Scan(source)
	}
	return false, nil
}

// sglReadDriverDecimal reads a decimal through the text DuckDB casts it to, which keeps
// every digit of its scale, into a type that reads text, or into a string or a float.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes number (sglDriverDecimal) which is the driver decimal to decode.
//
// Returns error when the source cannot be represented by the destination.
//
//sgl:when reads ownTypes
func sglReadDriverDecimal(target reflect.Value, number sglDriverDecimal) error {
	text := sglDriverDecimalText(number)
	address := target.Addr().Interface()
	if unmarshaler, ok := address.(encoding.TextUnmarshaler); ok {
		return unmarshaler.UnmarshalText([]byte(text))
	}
	switch target.Kind() {
	case reflect.String:
		target.SetString(text)
		return nil
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(text, target.Type().Bits())
		if err != nil {
			return err
		}
		target.SetFloat(parsed)
		return nil
	default:
		return fmt.Errorf("cannot read a decimal into %s", target.Type())
	}
}

// sglDriverDecimalText returns a decimal as the text DuckDB casts it to: its unscaled
// integer with a point before the last digits its scale counts, such as 1.50 for 150 at
// scale 2.
//
// Takes number (sglDriverDecimal) which is the driver decimal, including its scale.
//
// Returns string which is the decimal text with its scale preserved.
//
//sgl:when reads ownTypes
func sglDriverDecimalText(number sglDriverDecimal) string {
	if number.Value == nil {
		return "0"
	}
	digits := new(big.Int).Abs(number.Value).String()
	scale := int(number.Scale)
	if scale == 0 {
		return number.Value.String()
	}
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	sign := ""
	if number.Value.Sign() < 0 {
		sign = sglMinus
	}
	return sign + digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
}

// sglReadDriverStruct reads a map keyed by field name into a struct, field by field; a
// field the map does not hold keeps its zero value.
//
// Takes target (reflect.Value) which is the settable destination.
// Takes value (reflect.Value) which is the reflected source value.
//
// Returns error when the source cannot be represented by the destination.
//
//sgl:when reads
func sglReadDriverStruct(target reflect.Value, value reflect.Value) error {
	source := value.Interface()
	fields, ok := source.(map[string]any)
	if !ok {
		return sglUnreadable(value.Type(), target.Type())
	}
	target.SetZero()
	structType := target.Type()
	for index := range structType.NumField() {
		name, readable := sglDriverFieldName(structType.Field(index))
		if !readable {
			continue
		}
		element, found := sglDriverField(fields, name)
		if !found {
			continue
		}
		if err := sglReadDriverValue(target.Field(index), element); err != nil {
			return fmt.Errorf("field %s: %w", name, err)
		}
	}
	return nil
}

// sglDriverFieldName returns the name a struct field is read by, its json name or its Go
// name, or false for a field that is not read.
//
// Takes field (reflect.StructField) which is the struct field whose tags and visibility
// are inspected.
//
// Returns string which is the field's column name.
// Returns bool which reports whether the field is exported and not excluded.
//
//sgl:when reads
func sglDriverFieldName(field reflect.StructField) (string, bool) {
	if !field.IsExported() {
		return "", false
	}
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	switch name {
	case "-":
		return "", false
	case "":
		return field.Name, true
	default:
		return name, true
	}
}

// sglDriverField returns the value of a field from a map keyed by field name, matching
// the name exactly, or ignoring case when no key matches exactly.
//
// Takes fields (map[string]any) which maps tuple field names to their values.
// Takes name (string) which is the field name to match, ignoring case as a fallback.
//
// Returns any which is the matching field value, if found.
// Returns bool which reports whether the field exists.
//
//sgl:when reads
func sglDriverField(fields map[string]any, name string) (any, bool) {
	if element, found := fields[name]; found {
		return element, true
	}
	for key, element := range fields {
		if strings.EqualFold(key, name) {
			return element, true
		}
	}
	return nil, false
}

// sglDriverIntervalText returns an interval as the text DuckDB casts it to: its years,
// months and days, each with its unit, then its time of day as hours, minutes and seconds
// with the fraction it has, or 00:00:00 for an empty interval.
//
// Takes interval (sglDriverInterval) which is the driver interval to format.
//
// Returns string which is the interval in DuckDB text form.
//
//sgl:when reads ownTypes
func sglDriverIntervalText(interval sglDriverInterval) string {
	var parts []string
	parts = sglDriverIntervalPart(parts, int64(interval.Months/sglMonthsPerYear), "year")
	parts = sglDriverIntervalPart(parts, int64(interval.Months%sglMonthsPerYear), "month")
	parts = sglDriverIntervalPart(parts, int64(interval.Days), "day")
	if interval.Micros != 0 {
		parts = append(parts, sglDriverIntervalClock(interval.Micros))
	}
	if len(parts) == 0 {
		return "00:00:00"
	}
	return strings.Join(parts, " ")
}

// sglDriverIntervalPart appends a count of a unit, such as 2 days, unless it is zero.
//
// Takes parts ([]string) which holds the interval components written so far.
// Takes count (int64) which is the signed number of units.
// Takes unit (string) which is the singular unit name.
//
// Returns []string which holds the components with a nonzero count appended.
//
//sgl:when reads ownTypes
func sglDriverIntervalPart(parts []string, count int64, unit string) []string {
	if count == 0 {
		return parts
	}
	if count != 1 && count != -1 {
		unit += "s"
	}
	return append(parts, strconv.FormatInt(count, 10)+" "+unit)
}

// sglDriverIntervalClock returns microseconds as hours, minutes and seconds, such as
// -01:30:00.5.
//
// Takes micros (int64) which is the signed interval duration in microseconds.
//
// Returns string which is the signed hours, minutes and seconds with any fractional part.
//
//sgl:when reads ownTypes
func sglDriverIntervalClock(micros int64) string {
	sign := ""
	remainder := micros
	if micros < 0 {
		sign = sglMinus
	} else {
		remainder = -micros
	}
	hours := -(remainder / sglMicrosecondsPerHour)
	remainder += hours * sglMicrosecondsPerHour
	minutes := -(remainder / sglMicrosecondsPerMinute)
	remainder += minutes * sglMicrosecondsPerMinute
	seconds := -(remainder / sglMicrosecondsPerSecond)
	clock := fmt.Sprintf("%s%02d:%02d:%02d", sign, hours, minutes, seconds)
	if fraction := -(remainder + seconds*sglMicrosecondsPerSecond); fraction != 0 {
		clock += "." + strings.TrimRight(fmt.Sprintf("%06d", fraction), "0")
	}
	return clock
}
