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

//go:build ignore

package main

import (
	"bufio"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"
)

// request carries one bounded operation sequence to the worker.
type request struct {
	// ID identifies the request in its response.
	ID uint64

	// Data holds the operation bytes.
	Data []byte
}

// argument records one driver argument in the response.
type argument struct {
	// Name holds the optional argument name.
	Name string

	// Values holds scalar text or list elements in order.
	Values []string

	// List reports whether Values contains list elements.
	List bool
}

// statement records one SQL call and its arguments.
type statement struct {
	// SQL holds the executed query.
	SQL string

	// Arguments holds driver arguments in binding order.
	Arguments []argument
}

// call records one terminal operation and its driver observations.
type call struct {
	// Step identifies the input operation.
	Step int

	// Statements holds the SQL calls made by the operation.
	Statements []statement

	// Error holds an unexpected method error.
	Error string

	// Values holds scanned scalar values in column order.
	Values []string

	// OpenRows counts result sets left open after the operation.
	OpenRows int
}

// preparedEvent records one prepared statement lifecycle operation.
type preparedEvent struct {
	// Operation identifies the lifecycle event.
	Operation string

	// SQL identifies the prepared statement.
	SQL string
}

// response records the worker's observations for one request.
type response struct {
	// ID matches the request identifier.
	ID uint64

	// Calls holds terminal operations in execution order.
	Calls []call

	// Panic records a caught panic, leaked rows or a cleanup failure.
	Panic string

	// PreparationError describes a failed preparation attempt.
	PreparationError string

	// Prepared holds prepared statement lifecycle events.
	Prepared []preparedEvent
}

// fuzzRowMode selects the row behavior for the current request.
var fuzzRowMode int

// fuzzOpenRows counts result sets still owned by the worker.
var fuzzOpenRows int

// fuzzReadError is the injected row read failure.
var fuzzReadError = errors.New("injected row failure")

// fuzzNoRowsError is the injected empty result error.
var fuzzNoRowsError = sql.ErrNoRows

// main reads JSON requests and writes one JSON response for each request.
// It panics on malformed input or a protocol I/O error.
func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 65536), 65536)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var input request
		if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
			panic(err)
		}
		output := execute(input)
		if err := encoder.Encode(output); err != nil {
			panic(err)
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
}

// execute runs the bounded operation sequence and records driver behavior.
// It captures panics and preparation failures in the response.
//
// Takes input (request) which holds the operations and request identifier.
//
// Returns response which holds calls, lifecycle events and failures.
func execute(input request) (output response) {
	output.ID = input.ID
	fuzzRowMode = 0
	if len(input.Data) > 0 {
		fuzzRowMode = int(input.Data[0]/48) % 5
	}
	if fuzzOpenRows != 0 {
		output.Panic = "previous request leaked rows"
		return
	}
	defer func() {
		if value := recover(); value != nil {
			output.Panic = fmt.Sprint(value)
		}
	}()
	connection := Connect()
	if closer, ok := any(connection).(interface{ Close() error }); ok {
		defer closer.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	queries, cleanup, err := fuzzNewQueries(ctx, connection, input.Data)
	defer func() {
		if cleanup != nil {
			if err := cleanup(); err != nil {
				output.Panic = "closing prepared statements: " + err.Error()
			}
		}
		output.Prepared = fuzzTakePrepared()
	}()
	if err != nil {
		output.PreparationError = err.Error()
		return
	}
	target := reflect.ValueOf(queries)
	Take()
	value := int64(1001)
	builder := target.MethodByName("FuzzBuilder").Call([]reflect.Value{reflect.ValueOf(value)})[0]
	terminal := func(step int, method reflect.Value, arguments []reflect.Value, count bool) {
		Take()
		result := method.Call(arguments)
		current := call{Step: step, OpenRows: fuzzOpenRows}
		if len(result) > 1 {
			current.Values = fuzzScannedValues(result[0])
		}
		if len(result) > 0 {
			last := result[len(result)-1]
			if last.Type().Implements(reflect.TypeFor[error]()) && !last.IsNil() {
				err := last.Interface().(error)
				leaf := err
				for errors.Unwrap(leaf) != nil {
					leaf = errors.Unwrap(leaf)
				}
				if !count || !(errors.Is(err, sql.ErrNoRows) || leaf.Error() == "no rows in result set") {
					current.Error = err.Error()
				}
			}
		}
		for _, recorded := range Take() {
			current.Statements = append(current.Statements, statement{SQL: recorded.SQL, Arguments: renderArguments(recorded.Arguments)})
		}
		output.Calls = append(output.Calls, current)
	}
	steps := input.Data
	if len(steps) > 32 {
		steps = steps[:32]
	}
	for step, operation := range steps {
		switch operation % 12 {
		case 0:
			builder = target.MethodByName("FuzzBuilder").Call([]reflect.Value{reflect.ValueOf(value)})[0]
		case 1:
			builder = builder.MethodByName("Where").Call([]reflect.Value{reflect.ValueOf("id"), reflect.ValueOf(">="), reflect.ValueOf(int64(2000 + int(operation)))})[0]
		case 2:
			builder = builder.MethodByName("Where").Call([]reflect.Value{reflect.ValueOf("score"), reflect.ValueOf("IS NULL"), reflect.Zero(reflect.TypeFor[any]())})[0]
		case 3:
			direction := "ASC"
			if operation&1 != 0 {
				direction = "DESC"
			}
			builder = builder.MethodByName("OrderBy").Call([]reflect.Value{reflect.ValueOf("id"), reflect.ValueOf(direction)})[0]
		case 4:
			builder = builder.MethodByName("Limit").Call([]reflect.Value{reflect.ValueOf(int(operation%8) + 1)})[0]
		case 5:
			builder = builder.MethodByName("Limit").Call([]reflect.Value{reflect.ValueOf(9)})[0]
			builder = builder.MethodByName("Offset").Call([]reflect.Value{reflect.ValueOf(int(operation % 8))})[0]
		case 6, 7:
			name := "All"
			if operation%12 == 7 {
				name = "Count"
			}
			terminal(step, builder.MethodByName(name), []reflect.Value{reflect.ValueOf(ctx)}, name == "Count")
		case 8:
			terminal(step, target.MethodByName("FuzzPlain"), []reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(value)}, false)
		case 9:
			values := make([]int64, int(operation/12)%9)
			for index := range values {
				values[index] = int64(3000 + index)
			}
			method := target.MethodByName("FuzzList")
			argument := reflect.ValueOf(values)
			if method.Type().In(1).Kind() == reflect.Struct {
				argument = reflect.New(method.Type().In(1)).Elem()
				argument.Field(0).Set(reflect.ValueOf(values))
			}
			terminal(step, method, []reflect.Value{reflect.ValueOf(ctx), argument}, false)
		case 10:
			method := target.MethodByName("FuzzInsert")
			values := reflect.MakeSlice(method.Type().In(1), int(operation/12)%9, int(operation/12)%9)
			for index := 0; index < values.Len(); index++ {
				row := values.Index(index)
				row.FieldByName("ID").SetInt(int64(4000 + index))
				row.FieldByName("Category").SetInt(int64(5000 + index))
			}
			terminal(step, method, []reflect.Value{reflect.ValueOf(ctx), values}, false)
		case 11:
			fresh := target.MethodByName("FuzzBuilder").Call([]reflect.Value{reflect.ValueOf(value)})[0]
			terminal(step, fresh.MethodByName("All"), []reflect.Value{reflect.ValueOf(ctx)}, false)
			optional := target.MethodByName("FuzzOptional")
			parameters := reflect.New(optional.Type().In(0)).Elem()
			selection := int(operation/12) % 4
			for index, name := range []string{"Minimum", "Category"} {
				if selection&(1<<index) != 0 {
					field := parameters.FieldByName(name)
					if field.Kind() == reflect.Struct {
						field.FieldByName("V").SetInt(int64(6000 + index))
						field.FieldByName("Valid").SetBool(true)
						continue
					}
					field.Set(reflect.New(field.Type().Elem()))
					field.Elem().SetInt(int64(6000 + index))
				}
			}
			filtered := optional.Call([]reflect.Value{parameters})[0]
			filtered = filtered.MethodByName("Where").Call([]reflect.Value{reflect.ValueOf("id"), reflect.ValueOf("<"), reflect.ValueOf(int64(7000))})[0]
			filtered = filtered.MethodByName("Limit").Call([]reflect.Value{reflect.ValueOf(2)})[0]
			terminal(step, filtered.MethodByName("All"), []reflect.Value{reflect.ValueOf(ctx)}, false)
			terminal(step, filtered.MethodByName("Count"), []reflect.Value{reflect.ValueOf(ctx)}, true)
		}
	}
	terminal(len(steps), builder.MethodByName("All"), []reflect.Value{reflect.ValueOf(ctx)}, false)
	return output
}

// renderArguments converts driver arguments to comparable names and text values.
// It preserves list boundaries and represents nil scalar values as NULL.
//
// Takes values ([]any) which holds driver arguments in binding order.
//
// Returns []argument which holds their names, values and list markers.
func renderArguments(values []any) []argument {
	var result []argument
	for _, value := range values {
		current := argument{}
		switch named := value.(type) {
		case sql.NamedArg:
			current.Name, value = named.Name, named.Value
		case driver.NamedValue:
			current.Name, value = named.Name, named.Value
		}
		if valuer, ok := value.(driver.Valuer); ok && strings.HasPrefix(reflect.TypeOf(value).Name(), "Null[") {
			value, _ = valuer.Value()
		}
		reflected := reflect.ValueOf(value)
		if reflected.IsValid() && reflected.Kind() == reflect.Struct {
			name, field := reflected.FieldByName("Name"), reflected.FieldByName("Value")
			if name.IsValid() && name.Kind() == reflect.String && field.IsValid() {
				current.Name = name.String()
				reflected = field
			}
		}
		for reflected.IsValid() && (reflected.Kind() == reflect.Pointer || reflected.Kind() == reflect.Interface) {
			if reflected.IsNil() {
				reflected = reflect.Value{}
				break
			}
			reflected = reflected.Elem()
		}
		if !reflected.IsValid() {
			current.Values = []string{"NULL"}
		} else if reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Array {
			current.List = true
			for index := 0; index < reflected.Len(); index++ {
				current.Values = append(current.Values, fmt.Sprint(reflected.Index(index).Interface()))
			}
		} else {
			text := fmt.Sprint(reflected.Interface())
			if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
				current.List = true
				text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "["), "]"))
				if text != "" {
					for _, part := range strings.Split(text, ",") {
						current.Values = append(current.Values, strings.TrimSpace(part))
					}
				}
			} else {
				current.Values = []string{text}
			}
		}
		result = append(result, current)
	}
	return result
}

// fuzzRowValues supplies a fixed result for the query's selected columns.
// A non-count query containing SCORE but no FROM clause panics.
//
// Takes query (string) which contains the generated SQL query.
//
// Returns []any which holds a count, one ID or an ID and score.
func fuzzRowValues(query string) []any {
	upper := strings.ToUpper(query)
	if strings.Contains(upper, "COUNT(") {
		return []any{int64(7)}
	}
	if strings.Contains(upper, "SCORE") && strings.Contains(upper[:strings.Index(upper, "FROM")], "SCORE") {
		if fuzzRowMode == 2 {
			return []any{int64(6101), nil}
		}
		return []any{int64(6101), int64(6202)}
	}
	return []any{int64(6101)}
}

// fuzzAssignRow copies row values into matching scan destinations.
// A destination that is neither a writable pointer nor a scanner panics.
//
// Takes destinations ([]any) which holds the scan targets in column order.
// Takes values ([]any) which holds the row values in column order.
//
// Returns error when widths differ, conversion fails or a scanner rejects a value.
func fuzzAssignRow(destinations, values []any) error {
	if len(destinations) != len(values) {
		return fmt.Errorf("scan width %d, expected %d", len(destinations), len(values))
	}
	for index, destination := range destinations {
		if scanner, ok := destination.(sql.Scanner); ok {
			if err := scanner.Scan(values[index]); err != nil {
				return err
			}
			continue
		}
		target := reflect.ValueOf(destination).Elem()
		if values[index] == nil {
			target.SetZero()
			continue
		}
		for target.Kind() == reflect.Pointer {
			target.Set(reflect.New(target.Type().Elem()))
			target = target.Elem()
		}
		value := reflect.ValueOf(values[index])
		if !value.Type().ConvertibleTo(target.Type()) {
			return fmt.Errorf("cannot scan %T into %T", values[index], destination)
		}
		target.Set(value.Convert(target.Type()))
	}
	return nil
}

// fuzzScannedValues flattens a result into scalar text values.
// It represents nil pointers, nil interfaces and invalid null values as NULL.
//
// Takes value (reflect.Value) which holds the result to flatten.
//
// Returns []string which holds the values in field or element order.
func fuzzScannedValues(value reflect.Value) []string {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return []string{"NULL"}
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return []string{"NULL"}
	}
	if value.Kind() == reflect.Struct && strings.HasPrefix(value.Type().Name(), "Null[") {
		if !value.FieldByName("Valid").Bool() {
			return []string{"NULL"}
		}
		return fuzzScannedValues(value.FieldByName("V"))
	}
	var result []string
	switch value.Kind() {
	case reflect.Slice:
		for index := 0; index < value.Len(); index++ {
			result = append(result, fuzzScannedValues(value.Index(index))...)
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			result = append(result, fuzzScannedValues(value.Field(index))...)
		}
	default:
		result = append(result, fmt.Sprint(value.Interface()))
	}
	return result
}
