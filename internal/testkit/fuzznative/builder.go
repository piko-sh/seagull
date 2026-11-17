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
	"errors"
	"fmt"
	"reflect"
	"strings"

	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
)

// builderErrors maps the default preset's builder error texts to model classes.
var builderErrors = map[string]string{
	"sgl: unknown column in runtime query filter":        fuzzbuilder.ClassUnknownColumn,
	"sgl: unknown operator in runtime query filter":      fuzzbuilder.ClassUnknownOperator,
	"sgl: unknown sort direction in runtime query order": fuzzbuilder.ClassUnknownDirection,
}

// builderTerminals names the generated method of each terminal operation.
var builderTerminals = map[fuzzbuilder.Operation]string{
	fuzzbuilder.OperationAll:   "All",
	fuzzbuilder.OperationOne:   "One",
	fuzzbuilder.OperationCount: "Count",
}

// builderRun holds the generated API and builders of one version 16 sequence.
type builderRun struct {
	// queries is the generated query receiver.
	queries reflect.Value

	// slots holds the current builders.
	slots [2]reflect.Value
}

// create calls a catalogue query's generated builder constructor.
//
// Takes query (int) which selects the catalogue query.
// Takes parameter (int64) which is the query's own parameter value.
//
// Returns reflect.Value which is the new builder.
// Returns error when the constructor is missing or has an unexpected signature.
func (run *builderRun) create(query int, parameter int64) (reflect.Value, error) {
	method := run.queries.MethodByName(fuzzbuilder.QueryName(query))
	if !method.IsValid() {
		return reflect.Value{}, fmt.Errorf("generated builder constructor %s is missing", fuzzbuilder.QueryName(query))
	}
	var arguments []reflect.Value
	if fuzzbuilder.QueryHasParameter(query) {
		if method.Type().NumIn() != 1 || !method.Type().In(0).ConvertibleTo(reflect.TypeFor[int64]()) {
			return reflect.Value{}, errors.New("generated builder constructor does not take one integer parameter")
		}
		arguments = append(arguments, reflect.ValueOf(parameter).Convert(method.Type().In(0)))
	}
	if method.Type().NumIn() != len(arguments) || method.Type().NumOut() != 1 {
		return reflect.Value{}, errors.New("generated builder constructor has an unexpected signature")
	}
	return method.Call(arguments)[0], nil
}

// step performs one call and records what it returned.
//
// Takes step (fuzzbuilder.Step) which is the call.
//
// Returns fuzzbuilder.Observation which records the result or its failure class.
// Returns error when the generated API has an unexpected shape.
func (run *builderRun) step(ctx context.Context, step fuzzbuilder.Step) (fuzzbuilder.Observation, error) {
	observation := fuzzbuilder.Observation{Class: "", Error: "", Rows: nil, Count: 0}
	if step.Slot < 0 || step.Slot >= len(run.slots) {
		return observation, fmt.Errorf("builder slot %d is outside the model", step.Slot)
	}
	builder := run.slots[step.Slot]
	var arguments []reflect.Value
	name := ""
	switch step.Op {
	case fuzzbuilder.OperationNew:
		created, err := run.create(step.Query, step.Parameter)
		run.slots[step.Slot] = created
		return observation, err
	case fuzzbuilder.OperationWhere:
		value := step.Value
		name, arguments = "Where", []reflect.Value{reflect.ValueOf(step.Column), reflect.ValueOf(step.Operator), reflect.ValueOf(&value).Elem()}
	case fuzzbuilder.OperationOrder:
		name, arguments = "OrderBy", []reflect.Value{reflect.ValueOf(step.Column), reflect.ValueOf(step.Direction)}
	case fuzzbuilder.OperationLimit:
		name, arguments = "Limit", []reflect.Value{reflect.ValueOf(step.Number)}
	case fuzzbuilder.OperationOffset:
		name, arguments = "Offset", []reflect.Value{reflect.ValueOf(step.Number)}
	default:
		return terminal(ctx, builder, step.Op)
	}
	method := builder.MethodByName(name)
	if !method.IsValid() {
		observation.Class = fuzzbuilder.ClassMissingMethod
		return observation, nil
	}
	if method.Type().NumIn() != len(arguments) || method.Type().NumOut() != 1 {
		return observation, fmt.Errorf("generated %s has an unexpected signature", name)
	}
	method.Call(arguments)
	return observation, nil
}

// RunBuilder returns the runner of version 16 runtime builder sequences.
//
// Takes engine (string) which names the dialect the helper was compiled for.
//
// Returns SequenceRunner which decodes an input and calls the generated builders.
func RunBuilder(engine string) SequenceRunner {
	return func(ctx context.Context, session *Session, data []byte) (any, error) {
		model, ok := fuzzbuilder.Decode(engine, data)
		if !ok {
			return nil, errors.New("invalid runtime builder model")
		}
		run := builderRun{queries: reflect.ValueOf(session.Queries), slots: [2]reflect.Value{}}
		for index := range run.slots {
			builder, err := run.create(model.Query, model.Parameter)
			if err != nil {
				return nil, err
			}
			run.slots[index] = builder
		}
		observations := make([]fuzzbuilder.Observation, 0, len(model.Steps))
		for _, step := range model.Steps {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("%s: %w", step, context.Cause(ctx))
			}
			observation, err := run.step(ctx, step)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", step, err)
			}
			observations = append(observations, observation)
		}
		return observations, nil
	}
}

// terminal runs All, One or Count and records the rows, count or failure class.
//
// Takes builder (reflect.Value) which is the generated builder.
// Takes operation (fuzzbuilder.Operation) which selects the terminal.
//
// Returns fuzzbuilder.Observation which records the result.
// Returns error when the terminal has an unexpected shape.
func terminal(ctx context.Context, builder reflect.Value, operation fuzzbuilder.Operation) (fuzzbuilder.Observation, error) {
	observation := fuzzbuilder.Observation{Class: "", Error: "", Rows: nil, Count: 0}
	name, ok := builderTerminals[operation]
	if !ok {
		return observation, fmt.Errorf("builder operation %v is not modelled", operation)
	}
	method := builder.MethodByName(name)
	if !method.IsValid() {
		observation.Class = fuzzbuilder.ClassMissingMethod
		return observation, nil
	}
	results, err := call(method, reflect.ValueOf(ctx))
	if err != nil {
		observation.Class, observation.Error = classifyBuilderError(err), err.Error()
		return observation, nil
	}
	if len(results) != 1 {
		return observation, fmt.Errorf("generated %s returned %d values", name, len(results))
	}
	switch operation {
	case fuzzbuilder.OperationCount:
		if !results[0].CanInt() {
			return observation, errors.New("generated Count does not return an integer")
		}
		observation.Count = results[0].Int()
	case fuzzbuilder.OperationOne:
		row, err := builderCells(results[0])
		observation.Rows = [][]fuzzbuilder.Cell{row}
		return observation, err
	default:
		if results[0].Kind() != reflect.Slice || results[0].Len() > nativeRowBound {
			return observation, errors.New("generated All does not return a bounded slice")
		}
		for index := range results[0].Len() {
			row, err := builderCells(results[0].Index(index))
			if err != nil {
				return observation, err
			}
			observation.Rows = append(observation.Rows, row)
		}
	}
	return observation, nil
}

// classifyBuilderError names a terminal failure's class. A refusal's text ends the error,
// or begins it when the builder explains the refusal after it.
//
// Takes err (error) which the terminal returned.
//
// Returns string which is a builder refusal class, no-rows or the general error class.
func classifyBuilderError(err error) string {
	text := err.Error()
	for message, class := range builderErrors {
		if builderRefusal(text, message) {
			return class
		}
	}
	if strings.Contains(text, noRowsText) {
		return fuzzbuilder.ClassNoRows
	}
	return fuzzbuilder.ClassError
}

// builderCells reads a generated row's fields in declaration order.
//
// Takes row (reflect.Value) which is a generated row struct.
//
// Returns []fuzzbuilder.Cell which holds one cell per field.
// Returns error when a field holds neither an integer nor text.
func builderCells(row reflect.Value) ([]fuzzbuilder.Cell, error) {
	row = followValue(row)
	if !row.IsValid() || row.Kind() != reflect.Struct {
		return nil, errors.New("generated row is not a struct")
	}
	cells := make([]fuzzbuilder.Cell, 0, row.NumField())
	for index := range row.NumField() {
		value := followValue(row.Field(index))
		cell := fuzzbuilder.Cell{Text: "", Integer: 0, Null: false, IsText: false}
		switch {
		case !value.IsValid():
			cell.Null = true
		case value.Kind() == reflect.String:
			cell.Text, cell.IsText = value.String(), true
		case value.CanInt():
			cell.Integer = value.Int()
		case value.CanUint():
			number, err := signedInteger(value.Uint())
			if err != nil {
				return nil, fmt.Errorf("generated field %s: %w", row.Type().Field(index).Name, err)
			}
			cell.Integer = number
		default:
			return nil, fmt.Errorf("generated field %s has unmodelled type %s", row.Type().Field(index).Name, value.Type())
		}
		cells = append(cells, cell)
	}
	return cells, nil
}

// builderRefusal reports whether an error text carries one builder refusal message, at
// its end or before an explanation.
//
// Takes text (string) which is the error text.
// Takes message (string) which is the refusal message.
//
// Returns bool which is true when the text carries the message.
func builderRefusal(text, message string) bool {
	return strings.HasSuffix(text, message) || strings.HasPrefix(text, message+": ")
}
