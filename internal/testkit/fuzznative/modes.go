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
	"io"
	"reflect"
	"strings"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// ModesProtocolVersion identifies the execution-mode helper protocol.
const ModesProtocolVersion = 1

const (
	// noRowsText is the message every supported driver's no-rows error ends with.
	noRowsText = "no rows in result set"

	// commandStream is the query command of a generated iterator method.
	commandStream = "stream"

	// parameterStructSuffix ends the name of a generated parameter struct.
	parameterStructSuffix = "Params"
)

// ModeCall invokes one generated read method.
type ModeCall struct {
	// Method names the generated method.
	Method string

	// Command is the method's query command: many, stream or one.
	Command string

	// Arguments supplies the logical parameters in declaration order.
	Arguments []fuzzcase.TypedValue
}

// ModeRequest runs reads or a version 17 sequence through one execution mode.
type ModeRequest struct {
	// Connection is transmitted only through the helper's private stdin pipe.
	Connection string

	// Mode selects the generated package and driver profile.
	Mode string

	// Calls holds the reads to run.
	Calls []ModeCall

	// DML holds a version 17 input to run instead of reads.
	DML []byte

	// ID identifies the expected response.
	ID uint64

	// Version rejects incompatible protocol frames.
	Version int
}

// ModeResult records one generated read.
type ModeResult struct {
	// Method names the generated method.
	Method string

	// Command is the method's query command.
	Command string

	// Error describes a failed call.
	Error string

	// Rows holds each row's fields in canonical form.
	Rows [][]string

	// NoRows records the driver's no-rows error from a one method.
	NoRows bool
}

// ModeResponse records every observation of one request.
type ModeResponse struct {
	// Stage identifies a failure outside the generated calls.
	Stage string

	// Error describes that failure.
	Error string

	// Panic records a recovered panic as an unconditional test failure.
	Panic string

	// Calls holds the read observations in request order.
	Calls []ModeResult

	// DML holds the observation of each version 17 statement.
	DML []DMLObservation

	// ID matches the request being answered.
	ID uint64

	// Version identifies the response schema.
	Version int
}

// envelope returns the answered request ID and protocol version.
//
// Returns uint64 which is the request ID.
// Returns int which is the protocol version.
func (response *ModeResponse) envelope() (uint64, int) { return response.ID, response.Version }

// redact removes the connection string from the failure text.
//
// Takes connection (string) which is the private connection string.
func (response *ModeResponse) redact(connection string) {
	response.Error = redactConnection(response.Error, connection)
	response.Panic = redactConnection(response.Panic, connection)
}

// ExchangeModes runs one request through the helper.
// A worker must call ExchangeModes and Close sequentially on a process.
//
// Takes request (ModeRequest) which supplies the mode, connection and work.
//
// Returns ModeResponse which belongs to this exact request.
// Returns error when the helper fails, ignores cancellation or violates the protocol.
func (process *Process) ExchangeModes(ctx context.Context, request ModeRequest) (ModeResponse, error) {
	request.ID, request.Version = process.next(), ModesProtocolVersion
	return exchange[ModeResponse](ctx, process, request, request.Connection, request.ID, ModesProtocolVersion)
}

// ServeModes answers requests for every execution mode compiled into one helper.
//
// Takes engine (string) which selects the version 17 model.
// Takes factories (map[string]Factory) which opens each mode's generated API.
// Takes input (io.Reader) which supplies private request frames.
// Takes output (io.Writer) which receives complete response frames.
//
// Returns error when the protocol is invalid or a response cannot be written.
func ServeModes(engine string, factories map[string]Factory, input io.Reader, output io.Writer) error {
	return serveFrames(input, output, func(request *ModeRequest) (any, error) {
		if request.Version != ModesProtocolVersion || request.ID == 0 {
			return nil, errors.New("invalid mode request envelope")
		}
		return serveMode(engine, factories[request.Mode], request), nil
	})
}

// serveMode opens one mode's session and runs the request through it.
//
// Takes engine (string) which selects the version 17 model.
// Takes factory (Factory) which opens the mode's generated API.
// Takes request (ModeRequest) which supplies the connection and work.
//
// Returns ModeResponse which reports failures without the connection string.
func serveMode(engine string, factory Factory, request *ModeRequest) (response ModeResponse) {
	response.ID, response.Version = request.ID, ModesProtocolVersion
	defer func() {
		if recovered := recover(); recovered != nil {
			response.Panic = fmt.Sprint(recovered)
		}
		response.redact(request.Connection)
	}()
	ctx, cancel := context.WithTimeoutCause(context.Background(), nativeInputBound, errInputBound)
	defer cancel()
	response.Stage = "factory"
	if factory == nil {
		response.Error = "the helper has no factory for mode " + request.Mode
		return response
	}
	session, err := factory(ctx, request.Connection)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	if !session.complete() {
		response.Error = errIncompleteSession.Error()
		return response
	}
	defer func() {
		if err := session.Close(); err != nil && response.Error == "" {
			response.Stage, response.Error = "close", err.Error()
		}
	}()
	response.Stage = ""
	if len(request.DML) > 0 {
		model, ok := fuzzcase.DecodeDML(engine, request.DML)
		if !ok {
			response.Stage, response.Error = "decode", "invalid version 17 input"
			return response
		}
		response.DML = runDML(ctx, session, model)
		return response
	}
	queries := reflect.ValueOf(session.Queries)
	for _, item := range request.Calls {
		if ctx.Err() != nil {
			response.Stage, response.Error = "calls", context.Cause(ctx).Error()
			return response
		}
		response.Calls = append(response.Calls, runCall(ctx, queries, item))
	}
	return response
}

// runCall invokes one generated read and records its rows canonically.
//
// Takes queries (reflect.Value) which is the generated receiver.
// Takes item (ModeCall) which names the method and its arguments.
//
// Returns ModeResult which records rows or the failure.
func runCall(ctx context.Context, queries reflect.Value, item ModeCall) ModeResult {
	result := ModeResult{Method: item.Method, Command: item.Command, Error: "", Rows: nil, NoRows: false}
	method := queries.MethodByName(item.Method)
	if !method.IsValid() {
		result.Error = "required generated method is missing"
		return result
	}
	arguments, err := callArguments(ctx, method.Type(), item.Arguments, nil)
	if err != nil {
		result.Error = "arguments: " + err.Error()
		return result
	}
	if item.Command == commandStream {
		result.Rows, err = streamRows(method, arguments)
	} else {
		var values []reflect.Value
		values, err = call(method, arguments...)
		if err == nil && len(values) == 1 {
			result.Rows, err = canonicalRows(values[0])
		}
	}
	if err != nil {
		result.Error = err.Error()
		result.NoRows = strings.HasSuffix(err.Error(), noRowsText)
	}
	return result
}

// callArguments binds logical values to a generated method's parameters, filling a
// parameter struct's fields or the positional parameters in declaration order.
//
// Takes method (reflect.Type) which is the generated method's type.
// Takes values ([]fuzzcase.TypedValue) which holds the logical parameters in order.
// Takes nilBytes ([]bool) which marks values to send as nil slices, when not nil.
//
// Returns []reflect.Value which can be passed to the method.
// Returns error when the counts or types do not fit.
func callArguments(ctx context.Context, method reflect.Type, values []fuzzcase.TypedValue, nilBytes []bool) ([]reflect.Value, error) {
	arguments := []reflect.Value{reflect.ValueOf(ctx)}
	if method.NumIn() == 2 && method.In(1).Kind() == reflect.Struct && strings.HasSuffix(method.In(1).Name(), parameterStructSuffix) {
		parameters, err := fillParameters(method.In(1), values, nilBytes)
		return append(arguments, parameters), err
	}
	if method.NumIn() != len(values)+1 {
		return nil, fmt.Errorf("generated method takes %d parameters for %d values", method.NumIn()-1, len(values))
	}
	for index, value := range values {
		converted, err := Argument(method.In(index+1), value)
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, converted)
	}
	return arguments, nil
}

// fillParameters builds a generated parameter struct from values in field order.
//
// Takes target (reflect.Type) which is the generated parameter struct.
// Takes values ([]fuzzcase.TypedValue) which holds one value per exported field.
// Takes nilBytes ([]bool) which marks values to send as nil slices, when not nil.
//
// Returns reflect.Value which is the filled struct.
// Returns error when the field count or a field type does not fit.
func fillParameters(target reflect.Type, values []fuzzcase.TypedValue, nilBytes []bool) (reflect.Value, error) {
	parameters := reflect.New(target).Elem()
	if target.NumField() != len(values) {
		return parameters, fmt.Errorf("generated %s has %d fields for %d values", target.Name(), target.NumField(), len(values))
	}
	for index, value := range values {
		field := parameters.Field(index)
		converted, err := Argument(field.Type(), value)
		if err != nil {
			return parameters, fmt.Errorf("%s.%s: %w", target.Name(), target.Field(index).Name, err)
		}
		if index < len(nilBytes) && nilBytes[index] && converted.Kind() == reflect.Slice && converted.Len() == 0 {
			converted = reflect.Zero(field.Type())
		}
		field.Set(converted)
	}
	return parameters, nil
}

// streamRows consumes a generated iterator and records its rows canonically.
//
// Takes method (reflect.Value) which is the generated stream method.
// Takes arguments ([]reflect.Value) which holds its arguments.
//
// Returns [][]string which holds the rows delivered.
// Returns error when the iterator reports a failure or has the wrong shape.
func streamRows(method reflect.Value, arguments []reflect.Value) ([][]string, error) {
	values, err := call(method, arguments...)
	if err != nil {
		return nil, err
	}
	if len(values) != 1 || values[0].Kind() != reflect.Func || values[0].Type().NumIn() != 1 {
		return nil, errors.New("generated stream returned the wrong shape")
	}
	var rows [][]string
	err = consumeIterator(values[0], func(yielded []reflect.Value) (bool, error) {
		if len(yielded) != 2 {
			return false, errors.New("generated stream has the wrong shape")
		}
		if !yielded[1].IsNil() {
			return false, generatedError(yielded[1])
		}
		rows = append(rows, CanonicalFields(yielded[0]))
		return true, nil
	})
	return rows, err
}

// canonicalRows records a generated row or slice of rows.
//
// Takes value (reflect.Value) which is a row struct or a slice of them.
//
// Returns [][]string which holds each row's canonical fields.
// Returns error when the result exceeds the row bound.
func canonicalRows(value reflect.Value) ([][]string, error) {
	if value.Kind() != reflect.Slice {
		return [][]string{CanonicalFields(value)}, nil
	}
	if value.Len() > nativeRowBound {
		return nil, errors.New("generated read exceeded the row bound")
	}
	rows := make([][]string, 0, value.Len())
	for index := range value.Len() {
		rows = append(rows, CanonicalFields(value.Index(index)))
	}
	return rows, nil
}
