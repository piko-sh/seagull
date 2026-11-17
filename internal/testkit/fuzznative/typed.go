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
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"seagull.sh/seagull/internal/testkit/fuzztyped"
)

const (
	// typedWallLayout spells a wall-clock timestamp as the typed model does.
	typedWallLayout = "2006-01-02T15:04:05.999999999"

	// typedTrue is the typed model's spelling of a true Boolean.
	typedTrue = "true"
)

// typedTextLayouts are the timestamp spellings a driver may return as text.
var typedTextLayouts = []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", typedWallLayout}

// RunTyped returns the runner of version 15 typed value and expression inputs.
//
// Takes engine (string) which names the dialect the helper was compiled for.
//
// Returns SequenceRunner which decodes an input and calls the generated methods.
func RunTyped(engine string) SequenceRunner {
	return func(ctx context.Context, session *Session, data []byte) (any, error) {
		model, ok := fuzztyped.Decode(engine, data)
		if !ok {
			return nil, errors.New("invalid typed value model")
		}
		queries := reflect.ValueOf(session.Queries)
		observations := make([]fuzztyped.Observation, 0, len(model.Calls))
		for _, current := range model.Calls {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%s: %w", current.Method, context.Cause(ctx))
			}
			observation, err := typedCall(ctx, queries, current)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", current.Method, err)
			}
			observations = append(observations, observation)
		}
		return observations, nil
	}
}

// typedCall calls one generated method and records its rows or failure.
//
// Takes queries (reflect.Value) which is the generated query receiver.
// Takes current (fuzztyped.Call) which is the call and its expected result shape.
//
// Returns fuzztyped.Observation which records the rows or the failure text.
// Returns error when the generated API has an unexpected shape.
func typedCall(ctx context.Context, queries reflect.Value, current fuzztyped.Call) (fuzztyped.Observation, error) {
	observation := fuzztyped.Observation{Error: "", Rows: nil}
	method := queries.MethodByName(current.Method)
	if !method.IsValid() {
		return observation, errors.New("generated method is missing")
	}
	arguments, err := typedArguments(method.Type(), current)
	if err != nil {
		return observation, err
	}
	results, err := call(method, append([]reflect.Value{reflect.ValueOf(ctx)}, arguments...)...)
	if err != nil {
		observation.Error = err.Error()
		return observation, nil
	}
	if current.Exec {
		return observation, nil
	}
	if len(results) != 1 || results[0].Kind() != reflect.Slice || results[0].Len() > nativeRowBound {
		return observation, errors.New("generated read does not return a bounded slice")
	}
	for index := range results[0].Len() {
		row, err := typedRow(results[0].Index(index), current.Method+"Row", current.Shapes)
		if err != nil {
			return observation, fmt.Errorf("row %d: %w", index, err)
		}
		observation.Rows = append(observation.Rows, row)
	}
	return observation, nil
}

// typedArguments builds a call's arguments after its context.
//
// A single parameter is passed directly and several fill the parameter struct, by field
// name when the call names them and in declaration order otherwise.
//
// Takes signature (reflect.Type) which is the generated method's type.
// Takes current (fuzztyped.Call) which holds the values.
//
// Returns []reflect.Value which holds the arguments after the context.
// Returns error when the signature does not fit the values.
func typedArguments(signature reflect.Type, current fuzztyped.Call) ([]reflect.Value, error) {
	switch {
	case signature.NumIn() == 1 && len(current.Arguments) == 0:
		return nil, nil
	case signature.NumIn() != 2:
		return nil, fmt.Errorf("generated method takes %d parameters", signature.NumIn())
	case len(current.Arguments) == 1:
		argument, err := typedArgument(signature.In(1), current.Arguments[0])
		return []reflect.Value{argument}, err
	}
	target := signature.In(1)
	if target.Kind() != reflect.Struct || target.NumField() != len(current.Arguments) {
		return nil, fmt.Errorf("generated parameters %s do not hold %d values", target, len(current.Arguments))
	}
	if current.Names != nil && len(current.Names) != len(current.Arguments) {
		return nil, fmt.Errorf("the model names %d of %d parameters", len(current.Names), len(current.Arguments))
	}
	parameters := reflect.New(target).Elem()
	for index, value := range current.Arguments {
		field := parameters.Field(index)
		if current.Names != nil {
			field = parameters.FieldByName(current.Names[index])
		}
		if !field.IsValid() {
			return nil, fmt.Errorf("generated parameters omit %s", current.Names[index])
		}
		converted, err := typedArgument(field.Type(), value)
		if err != nil {
			return nil, err
		}
		field.Set(converted)
	}
	return []reflect.Value{parameters}, nil
}

// typedRow reads one generated row, a row struct or one bare value.
//
// Takes row (reflect.Value) which is one element of the generated result.
// Takes name (string) which is the generated row struct's type name.
// Takes shapes ([]string) which gives each column's kind.
//
// Returns []fuzztyped.Value which holds the row's values.
// Returns error when the row does not hold the columns.
func typedRow(row reflect.Value, name string, shapes []string) ([]fuzztyped.Value, error) {
	if row.Type().Name() != name && len(shapes) == 1 {
		value, err := typedObserve(row, shapes[0])
		return []fuzztyped.Value{value}, err
	}
	row = followValue(row)
	if !row.IsValid() || row.Kind() != reflect.Struct || row.NumField() != len(shapes) {
		return nil, fmt.Errorf("generated row does not hold %d columns", len(shapes))
	}
	values := make([]fuzztyped.Value, 0, len(shapes))
	for index, shape := range shapes {
		value, err := typedObserve(row.Field(index), shape)
		if err != nil {
			return nil, fmt.Errorf("column %d: %w", index, err)
		}
		values = append(values, value)
	}
	return values, nil
}

// typedArgument builds a generated parameter value from a model value.
//
// Takes target (reflect.Type) which is the generated parameter's type.
// Takes value (fuzztyped.Value) which is the model value.
//
// Returns reflect.Value which is assignable to the parameter.
// Returns error when the type cannot hold the value exactly.
func typedArgument(target reflect.Type, value fuzztyped.Value) (reflect.Value, error) {
	element, wrapped := nullElement(target)
	if value.Kind == fuzztyped.KindNull {
		switch {
		case wrapped, target.Kind() == reflect.Pointer, target.Kind() == reflect.Slice, target.Kind() == reflect.Map, target.Kind() == reflect.Interface:
			return reflect.Zero(target), nil
		default:
			return reflect.Value{}, fmt.Errorf("generated parameter %s cannot hold NULL", target)
		}
	}
	result := reflect.New(target).Elem()
	switch {
	case wrapped:
		inner, err := typedArgument(element, value)
		if err != nil {
			return inner, err
		}
		result.Field(0).Set(inner)
		result.Field(1).SetBool(true)
		return result, nil
	case target.Kind() == reflect.Pointer:
		inner, err := typedArgument(target.Elem(), value)
		if err != nil {
			return inner, err
		}
		result.Set(reflect.New(target.Elem()))
		result.Elem().Set(inner)
		return result, nil
	case target.Kind() == reflect.Interface:
		natural, err := typedNatural(value)
		if err == nil {
			result.Set(reflect.ValueOf(natural))
		}
		return result, err
	case target == reflect.TypeFor[time.Time]():
		moment, err := typedTime(value)
		result.Set(reflect.ValueOf(moment))
		return result, err
	}
	if scanner, ok := reflect.TypeAssert[sql.Scanner](result.Addr()); ok && value.Kind == fuzztyped.KindDecimal {
		return result, scanner.Scan(value.Text)
	}
	return result, typedAssign(result, value)
}

// typedAssign fills a scalar or collection from a model value.
//
// Takes target (reflect.Value) which is addressable.
// Takes value (fuzztyped.Value) which is non-NULL.
//
// Returns error when the type cannot hold the value exactly.
func typedAssign(target reflect.Value, value fuzztyped.Value) error {
	switch {
	case target.CanInt() && value.Kind == fuzztyped.KindInteger:
		number, err := strconv.ParseInt(value.Text, scalarBase, target.Type().Bits())
		if err != nil {
			return fmt.Errorf("generated parameter %s: %w", target.Type(), err)
		}
		target.SetInt(number)
		return nil
	case target.CanUint() && value.Kind == fuzztyped.KindInteger:
		number, err := strconv.ParseUint(value.Text, scalarBase, target.Type().Bits())
		if err != nil {
			return fmt.Errorf("generated parameter %s: %w", target.Type(), err)
		}
		target.SetUint(number)
		return nil
	case target.CanFloat() && value.Kind == fuzztyped.KindFloat:
		target.SetFloat(value.Float())
		if target.Type().Bits() == scalarBits || math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) || float64(float32(value.Float())) == value.Float() {
			return nil
		}
		return errors.New("float parameter loses precision")
	case target.Kind() == reflect.String && value.Kind == fuzztyped.KindText:
		target.SetString(value.Text)
		return nil
	case target.Kind() == reflect.Bool && value.Kind == fuzztyped.KindBoolean:
		target.SetBool(value.Text == typedTrue)
		return nil
	case target.Kind() == reflect.Slice && value.Kind == fuzztyped.KindList:
		return typedAssignList(target, value)
	default:
		return fmt.Errorf("generated parameter %s cannot hold %s", target.Type(), value.Kind)
	}
}

// typedNatural gives a model value's ordinary Go value for an interface parameter.
//
// Takes value (fuzztyped.Value) which is non-NULL.
//
// Returns any which is int64, float64, string, bool or time.Time.
// Returns error when the value has no ordinary Go form.
func typedNatural(value fuzztyped.Value) (any, error) {
	switch value.Kind {
	case fuzztyped.KindInteger:
		return strconv.ParseInt(value.Text, scalarBase, scalarBits)
	case fuzztyped.KindFloat:
		return value.Float(), nil
	case fuzztyped.KindText, fuzztyped.KindDecimal:
		return value.Text, nil
	case fuzztyped.KindBoolean:
		return value.Text == typedTrue, nil
	case fuzztyped.KindTime, fuzztyped.KindInstant:
		return typedTime(value)
	default:
		return nil, fmt.Errorf("%s has no ordinary Go value", value.Kind)
	}
}

// typedTime parses a wall-clock timestamp in UTC or an instant with its own offset.
//
// Takes value (fuzztyped.Value) which is a time or instant.
//
// Returns time.Time which is the timestamp.
// Returns error when the spelling is invalid.
func typedTime(value fuzztyped.Value) (time.Time, error) {
	if value.Kind == fuzztyped.KindInstant {
		return time.Parse(time.RFC3339Nano, value.Text)
	}
	return time.Parse(typedWallLayout, value.Text)
}

// typedObserve reads a generated value of a known kind.
//
// Takes value (reflect.Value) which is a generated field or element.
// Takes shape (string) which is the kind, a list's followed by its member's.
//
// Returns fuzztyped.Value which is the observation, NULL for a nil pointer, slice or
// invalid Null[T].
// Returns error when the Go type cannot hold the kind.
func typedObserve(value reflect.Value, shape string) (fuzztyped.Value, error) {
	current := followValue(value)
	if nilValue(current) {
		return fuzztyped.Null(), nil
	}
	kind, member, _ := strings.Cut(shape, " ")
	switch {
	case kind == fuzztyped.KindList && (current.Kind() == reflect.Slice || current.Kind() == reflect.Array):
		items := make([]fuzztyped.Value, 0, current.Len())
		for index := range current.Len() {
			item, err := typedObserve(current.Index(index), member)
			if err != nil {
				return item, err
			}
			items = append(items, item)
		}
		return fuzztyped.List(items...), nil
	case kind == fuzztyped.KindTime || kind == fuzztyped.KindInstant:
		return typedObserveTime(current, kind)
	case kind == fuzztyped.KindDecimal:
		text := observedText(current)
		if _, ok := new(big.Rat).SetString(text); !ok {
			return fuzztyped.Null(), fmt.Errorf("generated decimal %q is not a number", text)
		}
		return fuzztyped.Scalar(kind, text), nil
	}
	return typedObserveScalar(current)
}

// typedObserveScalar reads an integer, float, text or truth value by its Go kind.
//
// Takes current (reflect.Value) which is a non-NULL generated value.
//
// Returns fuzztyped.Value which is the observation of the Go value's own kind.
// Returns error when the Go kind is none of those.
func typedObserveScalar(current reflect.Value) (fuzztyped.Value, error) {
	switch {
	case current.CanInt():
		return fuzztyped.Integer(current.Int()), nil
	case current.CanUint():
		number, err := signedInteger(current.Uint())
		return fuzztyped.Integer(number), err
	case current.CanFloat():
		return fuzztyped.Float(current.Float()), nil
	case current.Kind() == reflect.String:
		return fuzztyped.Text(current.String()), nil
	case current.Kind() == reflect.Bool:
		return fuzztyped.Boolean(current.Bool()), nil
	case current.Kind() == reflect.Slice && current.Type().Elem().Kind() == reflect.Uint8:
		return fuzztyped.Text(string(current.Bytes())), nil
	default:
		return fuzztyped.Null(), fmt.Errorf("generated value has unmodelled type %s", current.Type())
	}
}

// typedObserveTime reads a timestamp as a wall clock or as an instant in UTC.
//
// Takes current (reflect.Value) which is a time.Time or its text.
// Takes kind (string) which is KindTime or KindInstant.
//
// Returns fuzztyped.Value which is the observation.
// Returns error when the value is not a timestamp.
func typedObserveTime(current reflect.Value, kind string) (fuzztyped.Value, error) {
	moment, ok := reflect.TypeAssert[time.Time](current)
	if !ok && current.Kind() == reflect.String {
		var err error
		for _, layout := range typedTextLayouts {
			if moment, err = time.Parse(layout, current.String()); err == nil {
				break
			}
		}
		ok = err == nil
	}
	if !ok {
		return fuzztyped.Null(), fmt.Errorf("generated timestamp has type %s", current.Type())
	}
	if kind == fuzztyped.KindInstant {
		return fuzztyped.Scalar(kind, moment.UTC().Format(time.RFC3339Nano)), nil
	}
	return fuzztyped.Scalar(kind, moment.Format(typedWallLayout)), nil
}

// typedAssignList fills a slice from a model list.
//
// Takes target (reflect.Value) which is an addressable slice.
// Takes value (fuzztyped.Value) which is a list.
//
// Returns error when an element cannot be held exactly.
func typedAssignList(target reflect.Value, value fuzztyped.Value) error {
	target.Set(reflect.MakeSlice(target.Type(), len(value.Items), len(value.Items)))
	for index, item := range value.Items {
		converted, err := typedArgument(target.Type().Elem(), item)
		if err != nil {
			return err
		}
		target.Index(index).Set(converted)
	}
	return nil
}
