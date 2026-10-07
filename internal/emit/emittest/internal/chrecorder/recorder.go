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

// Package chrecorder records generated code's interactions with the native ClickHouse
// driver.
//
// Connections record SQL and arguments, and queries return no rows. Batches record each
// appended row. The harness reproduces relevant driver conversion restrictions so tests
// catch values that the real driver cannot read or write.
package chrecorder

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sync"

	"seagull.sh/seagull/internal/emit/emittest/internal/stubs/clickhouse/lib/chcol"
	"seagull.sh/seagull/internal/emit/emittest/internal/stubs/clickhouse/lib/driver"
)

// maxTypeDepth bounds the walk through a type's elements and fields.
const maxTypeDepth = 16

// Statement is one statement the connection received: its SQL and its arguments, each as
// the generated code passed it, a named one as a sql.NamedArg.
type Statement struct {
	// SQL is the statement's text.
	SQL string

	// Arguments holds the arguments in order.
	Arguments []any
}

var (
	// errNamedString reports a value of a named string type, which the driver neither
	// appends nor scans.
	errNamedString = errors.New("a named string type is unsupported; use string or *string")

	// errDefinedType reports a value of a defined boolean, number or byte slice type, which
	// the driver neither scans nor, other than a number it is handed by value, appends.
	errDefinedType = errors.New("a defined type over a basic kind is unsupported; use the predeclared type")

	// valuerType is the interface of a value that converts itself for the driver.
	valuerType = reflect.TypeFor[sqldriver.Valuer]()

	// textMarshalerType is the interface of a value that writes itself as text, such as a
	// net.IP, which the driver appends itself.
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()

	// errJSONDestination reports a destination the driver cannot read a JSON column into.
	errJSONDestination = errors.New("destination must be a pointer to struct or map, or chcol.JSON")

	// scannerType is the interface of a destination that scans itself.
	scannerType = reflect.TypeFor[sql.Scanner]()

	// unmarshalerType is the interface of a destination that reads itself from JSON.
	unmarshalerType = reflect.TypeFor[json.Unmarshaler]()

	// deserialiserType is the interface of a destination the driver hands a JSON column.
	deserialiserType = reflect.TypeFor[chcol.JSONDeserializer]()

	// placeholderName reads the name of each typed placeholder an INSERT writes, such as
	// p_id in {p_id:UInt64}.
	placeholderName = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*):`)

	// recordedMutex guards recorded.
	recordedMutex sync.Mutex

	// recorded holds the statements received since the last Take.
	recorded []Statement
)

// Connection is the recording connection.
type Connection struct{}

// Exec records a statement.
//
// Takes query (string) which is the statement's text.
// Takes arguments (...any) which are its arguments.
//
// Returns error which is always nil.
func (*Connection) Exec(_ context.Context, query string, arguments ...any) error {
	record(query, arguments)
	return nil
}

// Query records a query and answers it with no rows.
//
// Takes query (string) which is the statement's text.
// Takes arguments (...any) which are its arguments.
//
// Returns driver.Rows which holds no rows.
// Returns error which is always nil.
func (*Connection) Query(_ context.Context, query string, arguments ...any) (driver.Rows, error) {
	record(query, arguments)
	return emptyRows{}, nil
}

// QueryRow records a query read for one row, which it does not find.
//
// Takes query (string) which is the statement's text.
// Takes arguments (...any) which are its arguments.
//
// Returns driver.Row which reports sql.ErrNoRows.
func (*Connection) QueryRow(_ context.Context, query string, arguments ...any) driver.Row {
	record(query, arguments)
	return missingRow{}
}

// PrepareBatch starts a batch for an INSERT.
//
// Takes query (string) which is the INSERT.
//
// Returns driver.Batch which collects the rows.
// Returns error which is always nil.
func (*Connection) PrepareBatch(_ context.Context, query string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
	return &batch{query: query, rows: nil}, nil
}

// emptyRows is a result with no rows.
type emptyRows struct{}

// Close ends the result.
//
// Returns error which is always nil.
func (emptyRows) Close() error {
	return nil
}

// Err reports no error.
//
// Returns error which is always nil.
func (emptyRows) Err() error {
	return nil
}

// Next reports the end of the result.
//
// Returns bool which is always false.
func (emptyRows) Next() bool {
	return false
}

// Scan reads no row, after checking that the driver could scan into every destination.
//
// Takes destinations (...any) which are the destinations.
//
// Returns error which is sql.ErrNoRows, or the error a destination meets.
func (emptyRows) Scan(destinations ...any) error {
	if err := scannable(destinations); err != nil {
		return err
	}
	return sql.ErrNoRows
}

// missingRow is a row read from a result with no rows.
type missingRow struct{}

// Err reports that the row was not found.
//
// Returns error which is always sql.ErrNoRows.
func (missingRow) Err() error {
	return sql.ErrNoRows
}

// Scan reports that the row was not found, after checking that the driver could scan into
// every destination.
//
// Takes destinations (...any) which are the destinations.
//
// Returns error which is sql.ErrNoRows, or the error a destination meets.
func (missingRow) Scan(destinations ...any) error {
	if err := scannable(destinations); err != nil {
		return err
	}
	return sql.ErrNoRows
}

// batch collects the rows of one INSERT.
type batch struct {
	// query is the batch's INSERT.
	query string

	// rows holds the rows appended.
	rows [][]any
}

// Append adds a row.
//
// Takes values (...any) which are the row's values.
//
// Returns error when a value holds a named string type, or is a defined type the driver
// does not append alone, as appendRefused says.
func (collected *batch) Append(values ...any) error {
	for index, value := range values {
		if holdsNamedString(reflect.TypeOf(value), 0) {
			return fmt.Errorf("clickhouse [AppendRow]: converting value %d of type %T: %w", index, value, errNamedString)
		}
		if appendRefused(reflect.TypeOf(value)) {
			return fmt.Errorf("clickhouse [AppendRow]: converting value %d of type %T: %w", index, value, errDefinedType)
		}
	}
	collected.rows = append(collected.rows, values)
	return nil
}

// Send records each row as a statement of the batch's INSERT, its values named after the
// INSERT's placeholders where it writes one per value.
//
// Returns error which is always nil.
func (collected *batch) Send() error {
	matches := placeholderName.FindAllStringSubmatch(collected.query, -1)
	for _, row := range collected.rows {
		if len(matches) != len(row) {
			record(collected.query, row)
			continue
		}
		named := make([]any, len(row))
		for index, value := range row {
			named[index] = driver.NamedValue{Name: matches[index][1], Value: value}
		}
		record(collected.query, named)
	}
	collected.rows = nil
	return nil
}

// Close ends the batch.
//
// Returns error which is always nil.
func (*batch) Close() error {
	return nil
}

// Connect returns a recording connection.
//
// Returns *Connection which records every statement run on it.
func Connect() *Connection {
	return &Connection{}
}

// Take returns the statements received since the last call, in order, and forgets them.
//
// Returns []Statement which holds the statements.
//
// Concurrency: atomically drains the shared recording under its mutex.
func Take() []Statement {
	recordedMutex.Lock()
	defer recordedMutex.Unlock()
	taken := recorded
	recorded = nil
	return taken
}

// holdsNamedString reports whether a type is a named string type or holds one, through
// pointers, slices, arrays, map keys and values and struct fields.
//
// Takes candidate (reflect.Type) which is the type, or nil.
// Takes depth (int) which is how deep the walk is, bounded so a recursive type ends it.
//
// Returns bool which is true when the Go type contains a named string type.
func holdsNamedString(candidate reflect.Type, depth int) bool {
	if candidate == nil || depth > maxTypeDepth {
		return false
	}
	switch candidate.Kind() {
	case reflect.String:
		return candidate != reflect.TypeFor[string]()
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return holdsNamedString(candidate.Elem(), depth+1)
	case reflect.Map:
		return holdsNamedString(candidate.Key(), depth+1) || holdsNamedString(candidate.Elem(), depth+1)
	case reflect.Struct:
		for field := range candidate.Fields() {
			if holdsNamedString(field.Type, depth+1) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// appendRefused reports whether the driver refuses to append a value of a type alone: a
// defined boolean type, a defined byte slice type other than json.RawMessage or one that
// writes itself as text, or a pointer to a defined number type, none of which converts
// itself as a driver.Valuer.
//
// Takes candidate (reflect.Type) which is the value's type, or nil.
//
// Returns bool which is true when the driver refuses the value.
func appendRefused(candidate reflect.Type) bool {
	if candidate == nil || candidate.Implements(valuerType) {
		return false
	}
	switch candidate.Kind() {
	case reflect.Bool:
		return candidate.PkgPath() != ""
	case reflect.Slice:
		return candidate.Elem().Kind() == reflect.Uint8 && candidate.PkgPath() != "" &&
			candidate != reflect.TypeFor[json.RawMessage]() && !candidate.Implements(textMarshalerType)
	case reflect.Pointer:
		return isDefinedNumber(candidate.Elem()) && !candidate.Elem().Implements(valuerType)
	default:
		return false
	}
}

// isDefinedNumber reports whether a type is a defined type over an integer or a float.
//
// Takes candidate (reflect.Type) which is the type.
//
// Returns bool which is true for a defined number type.
func isDefinedNumber(candidate reflect.Type) bool {
	switch candidate.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return candidate.PkgPath() != ""
	default:
		return false
	}
}

// scanRefusal reports destination types the simulated driver cannot scan. Arrays and
// tuples allow element conversion; maps require Go's built-in types.
//
// Takes candidate (reflect.Type) which is the part's type.
// Takes collected (bool) which is true when an array or a tuple lies on the way to it.
// Takes mapped (bool) which is true when a map lies on the way to it.
// Takes depth (int) which is how deep the walk is, bounded so a recursive type ends it.
//
// Returns error which says why the driver refuses the part, or nil.
func scanRefusal(candidate reflect.Type, collected, mapped bool, depth int) error {
	if candidate == nil || depth > maxTypeDepth {
		return nil
	}
	if !collected || mapped {
		if err := leafRefusal(candidate); err != nil {
			return err
		}
	}
	switch candidate.Kind() {
	case reflect.Pointer:
		return scanRefusal(candidate.Elem(), collected, mapped, depth+1)
	case reflect.Slice, reflect.Array:
		return scanRefusal(candidate.Elem(), true, mapped, depth+1)
	case reflect.Map:
		return errors.Join(scanRefusal(candidate.Key(), collected, true, depth+1), scanRefusal(candidate.Elem(), collected, true, depth+1))
	case reflect.Struct:
		return fieldRefusal(candidate, mapped, depth)
	default:
		return nil
	}
}

// fieldRefusal reports why the driver refuses to scan into a field of a tuple's struct,
// if it does, as scanRefusal says for each field.
//
// Takes candidate (reflect.Type) which is the struct type.
// Takes mapped (bool) which is true when a map lies on the way to it.
// Takes depth (int) which is how deep the walk is.
//
// Returns error which says why the driver refuses a field, or nil.
func fieldRefusal(candidate reflect.Type, mapped bool, depth int) error {
	for field := range candidate.Fields() {
		if err := scanRefusal(field.Type, true, mapped, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// leafRefusal reports why the driver refuses to scan into a type it meets alone, if it
// does: a named string type, or a defined boolean, number or byte slice type other than
// json.RawMessage or one that writes itself as text.
//
// Takes candidate (reflect.Type) which is the type.
//
// Returns error which says why the driver refuses the type, or nil.
func leafRefusal(candidate reflect.Type) error {
	switch {
	case candidate.Kind() == reflect.String && candidate != reflect.TypeFor[string]():
		return errNamedString
	case candidate.Kind() == reflect.Bool && candidate.PkgPath() != "", isDefinedNumber(candidate):
		return errDefinedType
	case candidate.Kind() == reflect.Slice && candidate.Elem().Kind() == reflect.Uint8 && candidate.PkgPath() != "" &&
		candidate != reflect.TypeFor[json.RawMessage]() && !candidate.Implements(textMarshalerType):
		return errDefinedType
	default:
		return nil
	}
}

// scannable returns the error the driver reports for the first destination it cannot scan
// into, or nil.
//
// Takes destinations ([]any) which are the destinations.
//
// Returns error which names the destination, or nil.
func scannable(destinations []any) error {
	for index, destination := range destinations {
		candidate := reflect.TypeOf(destination)
		if candidate != nil && holdsDocument(candidate) && !candidate.Implements(deserialiserType) {
			return fmt.Errorf("clickhouse []: converting into dest[%d] of type %T: %w", index, destination, errJSONDestination)
		}
		if candidate == nil || candidate.Implements(scannerType) {
			continue
		}
		if err := scanRefusal(candidate, false, false, 0); err != nil {
			return fmt.Errorf("clickhouse [ScanRow]: converting into dest[%d] of type %T: %w", index, destination, err)
		}
	}
	return nil
}

// holdsDocument reports whether a destination is a pointer, through pointers alone, to a
// type that holds a JSON document's bytes and reads itself from JSON and as a
// sql.Scanner, such as the generated JSON type, which a query reads only from a JSON
// column.
//
// Takes candidate (reflect.Type) which is the destination's type.
//
// Returns bool which is true for such a destination.
func holdsDocument(candidate reflect.Type) bool {
	if candidate.Kind() != reflect.Pointer {
		return false
	}
	base := candidate.Elem()
	for depth := 0; base.Kind() == reflect.Pointer && depth < maxTypeDepth; depth++ {
		base = base.Elem()
	}
	address := reflect.PointerTo(base)
	return base.Kind() == reflect.Slice && base.Elem().Kind() == reflect.Uint8 &&
		address.Implements(unmarshalerType) && address.Implements(scannerType)
}

// record keeps one statement, binding each named value as a sql.NamedArg.
//
// Takes query (string) which is the statement's text.
// Takes arguments ([]any) which are its arguments.
//
// Concurrency: appends to the shared recording under its mutex.
func record(query string, arguments []any) {
	values := make([]any, 0, len(arguments))
	for _, argument := range arguments {
		if named, ok := argument.(driver.NamedValue); ok {
			values = append(values, sql.Named(named.Name, named.Value))
			continue
		}
		values = append(values, argument)
	}
	recordedMutex.Lock()
	defer recordedMutex.Unlock()
	recorded = append(recorded, Statement{SQL: query, Arguments: values})
}
