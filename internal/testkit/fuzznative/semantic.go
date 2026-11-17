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

package fuzznative

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/testkit/fuzzsemantic"
)

// RunSemantic returns the runner of version 19 semantic relation inputs.
//
// Takes engine (string) which names the dialect the helper was compiled for.
//
// Returns SequenceRunner which decodes an input and calls the generated methods.
func RunSemantic(engine string) SequenceRunner {
	return func(ctx context.Context, session *Session, data []byte) (any, error) {
		model, ok := fuzzsemantic.Decode(engine, data)
		if !ok {
			return nil, errors.New("invalid semantic relation model")
		}
		queries := reflect.ValueOf(session.Queries)
		calls := model.Calls()
		observations := make([]fuzzsemantic.Observation, 0, len(calls))
		for _, current := range calls {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%s (%s): %w", current.Method, current.Label, context.Cause(ctx))
			}
			observation, err := semanticCall(ctx, queries, current)
			if err != nil {
				return nil, fmt.Errorf("%s (%s): %w", current.Method, current.Label, err)
			}
			observations = append(observations, observation)
		}
		return observations, nil
	}
}

// semanticCall runs one static method or builder and records its rows or failure.
//
// Takes queries (reflect.Value) which is the generated query receiver.
// Takes current (fuzzsemantic.Call) which is the call.
//
// Returns fuzzsemantic.Observation which records the rows, count or failure class.
// Returns error when the generated API has an unexpected shape.
func semanticCall(ctx context.Context, queries reflect.Value, current fuzzsemantic.Call) (fuzzsemantic.Observation, error) {
	observation := fuzzsemantic.Observation{Label: current.Label, Class: "", Error: "", Rows: nil, Count: 0}
	method := queries.MethodByName(current.Method)
	if !method.IsValid() {
		return observation, errors.New("generated method is missing")
	}
	var results []reflect.Value
	var err error
	if current.Dynamic {
		results, err = semanticBuilder(ctx, method, current, &observation)
		if err != nil || observation.Class != "" {
			return observation, err
		}
	} else {
		arguments, argumentErr := semanticArguments(method.Type(), current.Arguments)
		if argumentErr != nil {
			return observation, argumentErr
		}
		results, err = call(method, append([]reflect.Value{reflect.ValueOf(ctx)}, arguments...)...)
		if err != nil {
			observation.Class, observation.Error = semanticClass(err), err.Error()
			return observation, nil
		}
	}
	err = semanticResult(results, current.Count, &observation)
	return observation, err
}

// semanticResult records a successful call's count or rows.
//
// Takes results ([]reflect.Value) which holds the call's results without its error.
// Takes count (bool) which reports the Count terminal.
// Takes observation (*fuzzsemantic.Observation) which receives the count or rows.
//
// Returns error when the results have an unexpected shape.
func semanticResult(results []reflect.Value, count bool, observation *fuzzsemantic.Observation) error {
	if len(results) != 1 {
		return fmt.Errorf("generated call returned %d values", len(results))
	}
	if count {
		if !results[0].CanInt() {
			return errors.New("generated Count does not return an integer")
		}
		observation.Count = results[0].Int()
		return nil
	}
	if results[0].Kind() != reflect.Slice || results[0].Len() > nativeRowBound {
		return errors.New("generated read does not return a bounded slice")
	}
	for index := range results[0].Len() {
		row, err := semanticRow(results[0].Index(index))
		if err != nil {
			return fmt.Errorf("row %d: %w", index, err)
		}
		observation.Rows = append(observation.Rows, row)
	}
	return nil
}

// semanticBuilder creates a builder, applies its steps and runs its terminal.
//
// Takes constructor (reflect.Value) which is the generated builder constructor.
// Takes current (fuzzsemantic.Call) which holds the steps and terminal.
// Takes observation (*fuzzsemantic.Observation) which receives a failure class.
//
// Returns []reflect.Value which holds the terminal's results without its error.
// Returns error when the generated API has an unexpected shape.
func semanticBuilder(ctx context.Context, constructor reflect.Value, current fuzzsemantic.Call, observation *fuzzsemantic.Observation) ([]reflect.Value, error) {
	if constructor.Type().NumIn() != 0 || constructor.Type().NumOut() != 1 {
		return nil, errors.New("generated builder constructor has an unexpected signature")
	}
	builder := constructor.Call(nil)[0]
	for _, step := range current.Steps {
		var arguments []reflect.Value
		switch step.Name {
		case "Where":
			value := any(step.Value)
			arguments = []reflect.Value{reflect.ValueOf(step.Column), reflect.ValueOf(step.Operator), reflect.ValueOf(&value).Elem()}
		case "OrderBy":
			arguments = []reflect.Value{reflect.ValueOf(step.Column), reflect.ValueOf(step.Direction)}
		default:
			arguments = []reflect.Value{reflect.ValueOf(step.Number)}
		}
		method := builder.MethodByName(step.Name)
		if !method.IsValid() {
			observation.Class, observation.Error = fuzzsemantic.ClassMissingMethod, "generated builder has no "+step.Name
			return nil, nil
		}
		if method.Type().NumIn() != len(arguments) || method.Type().NumOut() != 1 {
			return nil, fmt.Errorf("generated %s has an unexpected signature", step.Name)
		}
		method.Call(arguments)
	}
	name := "All"
	if current.Count {
		name = "Count"
	}
	terminal := builder.MethodByName(name)
	if !terminal.IsValid() {
		observation.Class, observation.Error = fuzzsemantic.ClassMissingMethod, "generated builder has no "+name
		return nil, nil
	}
	results, err := call(terminal, reflect.ValueOf(ctx))
	if err != nil {
		observation.Class, observation.Error = semanticClass(err), err.Error()
		return nil, nil
	}
	return results, nil
}

// semanticClass separates a builder refusal from a native failure.
//
// Takes err (error) which the generated call returned.
//
// Returns string which is the builder or native class.
func semanticClass(err error) string {
	for message := range builderErrors {
		if builderRefusal(err.Error(), message) {
			return fuzzsemantic.ClassBuilder
		}
	}
	return fuzzsemantic.ClassNative
}

// semanticArguments builds a static call's arguments after its context.
//
// A single parameter is passed directly and several fill the parameter struct in
// declaration order.
//
// Takes signature (reflect.Type) which is the generated method's type.
// Takes values ([]int64) which holds the parameters in declaration order.
//
// Returns []reflect.Value which holds the arguments after the context.
// Returns error when the signature does not fit the values.
func semanticArguments(signature reflect.Type, values []int64) ([]reflect.Value, error) {
	switch {
	case signature.NumIn() == 1 && len(values) == 0:
		return nil, nil
	case signature.NumIn() != 2:
		return nil, fmt.Errorf("generated method takes %d parameters for %d values", signature.NumIn(), len(values))
	case len(values) == 1:
		argument, err := semanticArgument(signature.In(1), values[0])
		return []reflect.Value{argument}, err
	}
	target := signature.In(1)
	if target.Kind() != reflect.Struct || target.NumField() != len(values) {
		return nil, fmt.Errorf("generated parameters %s do not hold %d values", target, len(values))
	}
	parameters := reflect.New(target).Elem()
	for index, value := range values {
		argument, err := semanticArgument(target.Field(index).Type, value)
		if err != nil {
			return nil, fmt.Errorf("parameter %s: %w", target.Field(index).Name, err)
		}
		parameters.Field(index).Set(argument)
	}
	return []reflect.Value{parameters}, nil
}

// semanticArgument converts an integer to a generated parameter type.
//
// Takes target (reflect.Type) which is the parameter's type.
// Takes value (int64) which is the integer.
//
// Returns reflect.Value which is assignable to the parameter.
// Returns error when the type cannot hold the integer exactly.
func semanticArgument(target reflect.Type, value int64) (reflect.Value, error) {
	result := reflect.New(target).Elem()
	if element, wrapped := nullElement(target); wrapped {
		inner, err := semanticArgument(element, value)
		if err != nil {
			return result, err
		}
		result.Field(0).Set(inner)
		result.Field(1).SetBool(true)
		return result, nil
	}
	switch {
	case target.Kind() == reflect.Pointer:
		inner, err := semanticArgument(target.Elem(), value)
		if err == nil {
			result.Set(reflect.New(target.Elem()))
			result.Elem().Set(inner)
		}
		return result, err
	case target.Kind() == reflect.Interface:
		result.Set(reflect.ValueOf(value))
	case target.Kind() == reflect.String:
		result.SetString(strconv.FormatInt(value, scalarBase))
	case result.CanInt():
		if result.OverflowInt(value) {
			return result, fmt.Errorf("%d overflows %s", value, target)
		}
		result.SetInt(value)
	case result.CanUint():
		if value < 0 || result.OverflowUint(uint64(value)) {
			return result, fmt.Errorf("%d does not fit %s", value, target)
		}
		result.SetUint(uint64(value))
	case result.CanFloat():
		result.SetFloat(float64(value))
	default:
		if scanner, ok := reflect.TypeAssert[sql.Scanner](result.Addr()); ok {
			return result, scanner.Scan(value)
		}
		return result, fmt.Errorf("generated parameter %s cannot hold an integer", target)
	}
	return result, nil
}

// semanticRow reads one generated row, a row struct or one bare value.
//
// Takes row (reflect.Value) which is one element of the generated result.
//
// Returns []fuzzsemantic.Cell which holds the row's values in declaration order.
// Returns error when a value has an unmodelled type.
func semanticRow(row reflect.Value) ([]fuzzsemantic.Cell, error) {
	current := followValue(row)
	if !current.IsValid() || current.Kind() != reflect.Struct || semanticScalar(current) {
		cell, err := semanticCell(row)
		return []fuzzsemantic.Cell{cell}, err
	}
	cells := make([]fuzzsemantic.Cell, 0, current.NumField())
	for index := range current.NumField() {
		cell, err := semanticCell(current.Field(index))
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", current.Type().Field(index).Name, err)
		}
		cells = append(cells, cell)
	}
	return cells, nil
}

// semanticScalar reports whether a struct is one value, such as a decimal, not a row.
//
// Takes value (reflect.Value) which is a struct.
//
// Returns bool which is true for a Stringer or driver.Valuer.
func semanticScalar(value reflect.Value) bool {
	pointer := reflect.New(value.Type())
	_, stringer := reflect.TypeAssert[fmt.Stringer](pointer)
	_, valuer := reflect.TypeAssert[driver.Valuer](pointer)
	return stringer || valuer
}

// semanticCell reads one value in canonical text.
//
// Integers and exact decimals are written in canonical decimal, so a count typed as an
// unsigned integer, a sum typed as a decimal and a value typed as an integer agree.
//
// Takes value (reflect.Value) which is a generated field or element.
//
// Returns fuzzsemantic.Cell which is NULL for a nil pointer or invalid Null[T].
// Returns error when the value has an unmodelled type.
func semanticCell(value reflect.Value) (fuzzsemantic.Cell, error) {
	current := followValue(value)
	if nilValue(current) {
		return fuzzsemantic.Cell{Text: "", Null: true}, nil
	}
	switch {
	case current.CanInt():
		return semanticText(strconv.FormatInt(current.Int(), scalarBase)), nil
	case current.CanUint():
		return semanticText(strconv.FormatUint(current.Uint(), scalarBase)), nil
	case current.CanFloat():
		number, ok := new(big.Rat).SetString(strconv.FormatFloat(current.Float(), 'g', -1, scalarBits))
		if !ok {
			return fuzzsemantic.Cell{Text: "", Null: false}, fmt.Errorf("generated float %v is not finite", current.Float())
		}
		return semanticText(number.RatString()), nil
	case current.Kind() == reflect.String:
		return semanticText(current.String()), nil
	case current.Kind() == reflect.Bool:
		return fuzzsemantic.Cell{Text: strconv.FormatBool(current.Bool()), Null: false}, nil
	case current.Kind() == reflect.Slice && current.Type().Elem().Kind() == reflect.Uint8:
		return semanticText(string(current.Bytes())), nil
	case current.Kind() == reflect.Struct && semanticScalar(current):
		return semanticStruct(current)
	default:
		return fuzzsemantic.Cell{Text: "", Null: false}, fmt.Errorf("generated value has unmodelled type %s", current.Type())
	}
}

// semanticStruct reads a decimal or other scalar struct through its driver value or text.
//
// Takes current (reflect.Value) which is a non-NULL struct.
//
// Returns fuzzsemantic.Cell which holds its canonical text.
// Returns error when its driver value cannot be read.
func semanticStruct(current reflect.Value) (fuzzsemantic.Cell, error) {
	copied := reflect.New(current.Type())
	copied.Elem().Set(current)
	if valuer, ok := reflect.TypeAssert[driver.Valuer](copied); ok {
		native, err := valuer.Value()
		if err != nil {
			return fuzzsemantic.Cell{Text: "", Null: false}, err
		}
		if native == nil {
			return fuzzsemantic.Cell{Text: "", Null: true}, nil
		}
		return semanticCell(reflect.ValueOf(native))
	}
	return semanticText(observedText(copied.Elem())), nil
}

// semanticText writes a number in canonical decimal and leaves other text unchanged.
//
// Takes text (string) which is the value's spelling.
//
// Returns fuzzsemantic.Cell which holds the canonical spelling.
func semanticText(text string) fuzzsemantic.Cell {
	if number, ok := new(big.Rat).SetString(text); ok && strings.TrimSpace(text) == text && text != "" {
		return fuzzsemantic.Cell{Text: number.RatString(), Null: false}
	}
	return fuzzsemantic.Cell{Text: text, Null: false}
}
