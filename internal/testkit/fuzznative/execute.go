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
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// nativeInputBound limits the native operations performed for one request.
const nativeInputBound = 20 * time.Second

var (
	// errInputBound is the cause when one request outlives nativeInputBound.
	errInputBound = errors.New("native request exceeded its time bound")

	// errInvalidEnvelope reports a request with the wrong version or no ID.
	errInvalidEnvelope = errors.New("invalid native request envelope")

	// errIncompleteSession reports a factory that left out a required operation.
	errIncompleteSession = errors.New("native factory returned an incomplete session")

	// errYieldAfterStop reports an iterator that kept calling yield after it returned false.
	errYieldAfterStop = errors.New("generated iterator called yield after it was told to stop")

	// errStreamBound reports an iterator that yields more rows than nativeRowBound.
	errStreamBound = errors.New("generated stream exceeded the row bound")

	// errReadBound reports a read that returns more rows than nativeRowBound.
	errReadBound = errors.New("generated read exceeded the row bound")

	// runStages lists the stages Run performs, in order.
	runStages = []struct {
		run  func(*execution, context.Context) error
		name string
	}{
		{name: "insert", run: (*execution).insert},
		{name: "plain", run: (*execution).plain},
		{name: "stream", run: (*execution).stream},
		{name: "early", run: (*execution).early},
		{name: "builder", run: (*execution).builder},
		{name: "fresh", run: (*execution).fresh},
		{name: "transactions", run: (*execution).transactions},
		{name: "cancellation", run: (*execution).cancellation},
	}
)

// Factory opens an isolated session using the actual compiled driver API.
//
// Takes context.Context which bounds connection and preparation.
// Takes string which contains the private connection details.
//
// Returns *Session which owns generated queries and the native connection.
// Returns error when connection or preparation fails.
type Factory func(context.Context, string) (*Session, error)

// Session owns actual generated queries and native lifecycle operations.
type Session struct {
	// Queries supplies the compiled generated API.
	Queries any

	// Close releases generated statements and the native connection.
	Close func() error

	// Begin binds the generated API to a real native transaction.
	Begin func(context.Context) (*Transaction, error)

	// Prepared identifies explicitly prepared database/sql statements.
	Prepared bool

	// Native identifies a dedicated pgx or ClickHouse emitter.
	Native bool
}

// Transaction owns a generated API bound to one real native transaction.
type Transaction struct {
	// Queries supplies the generated transaction receiver.
	Queries any

	// Commit persists the transaction's writes.
	Commit func(context.Context) error

	// Rollback discards the transaction's writes.
	Rollback func(context.Context) error
}

// execution holds one independent request and the generated API it exercises.
type execution struct {
	// session owns native connections and transaction factories.
	session *Session

	// queries is the actual generated query receiver.
	queries reflect.Value

	// model supplies independent typed values and operation choices.
	model fuzzcase.RuntimeCase

	// response records every required observation.
	response Response
}

// complete reports whether a factory filled every operation a request needs.
//
// Returns bool which is false for a nil session or a missing operation.
func (session *Session) complete() bool {
	return session != nil && session.Queries != nil && session.Close != nil
}

// complete reports whether a transaction factory filled every operation.
//
// Returns bool which is false for a nil transaction or a missing operation.
func (transaction *Transaction) complete() bool {
	return transaction != nil && transaction.Queries != nil && transaction.Commit != nil && transaction.Rollback != nil
}

// insert writes every model value through the actual generated insert method.
//
// Returns error when a generated parameter or native write fails.
func (state *execution) insert(ctx context.Context) error {
	for index, value := range state.model.Values {
		if err := state.insertValue(ctx, state.queries, int64(index+1), value); err != nil {
			return err
		}
	}
	return nil
}

// insertValue binds one independent value through an actual generated insert.
//
// Takes receiver (reflect.Value) which selects ordinary or transaction queries.
// Takes identifier (int64) which establishes a unique ordered row identity.
// Takes value (fuzzcase.TypedValue) which supplies an independent representation.
//
// Returns error when parameter conversion or native execution fails.
func (state *execution) insertValue(ctx context.Context, receiver reflect.Value, identifier int64, value fuzzcase.TypedValue) error {
	method := receiver.MethodByName("Insert" + fuzzcase.RuntimeSuffix(state.model.Kind))
	if !method.IsValid() || method.Type().NumIn() != 2 || method.Type().In(1).Kind() != reflect.Struct {
		return errors.New("generated insert method has the wrong signature")
	}
	parameters := reflect.New(method.Type().In(1)).Elem()
	id, field := parameters.FieldByName("ID"), parameters.FieldByName("Value")
	if !id.IsValid() || !id.CanInt() || !field.IsValid() {
		return errors.New("generated insert parameters omit ID or Value")
	}
	id.SetInt(identifier)
	converted, err := Argument(field.Type(), value)
	if err != nil {
		return err
	}
	field.Set(converted)
	_, err = call(method, reflect.ValueOf(ctx), parameters)
	return err
}

// readMethod names the generated many method of the model's value family.
//
// Returns string which is the method name.
func (state *execution) readMethod() string {
	return "Read" + fuzzcase.RuntimeSuffix(state.model.Kind)
}

// plain reads the complete ordered data through the generated many method.
//
// Returns error when execution or exact result encoding fails.
func (state *execution) plain(ctx context.Context) error {
	rows, err := state.read(ctx, state.queries, state.readMethod())
	state.response.Plain = rows
	return err
}

// stream consumes every row from the generated iterator.
//
// Returns error when the iterator or exact result encoding fails.
func (state *execution) stream(ctx context.Context) error {
	rows, err := state.iterate(ctx, func() bool { return true })
	state.response.Stream = rows
	return err
}

// early stops a generated iterator after its first row.
//
// Returns error when early termination or result encoding fails.
func (state *execution) early(ctx context.Context) error {
	rows, err := state.iterate(ctx, func() bool { return false })
	state.response.Early = rows
	return err
}

// builder checks filtered counts, pagination, reuse and empty-list predicates.
//
// Returns error when a generated builder operation fails.
func (state *execution) builder(ctx context.Context) error {
	if err := state.page(ctx); err != nil {
		return err
	}
	empty, err := state.newBuilder()
	if err != nil {
		return err
	}
	if _, err := call(empty.MethodByName("Where"), reflect.ValueOf("id"), reflect.ValueOf("IN"), reflect.ValueOf([]int64{})); err != nil {
		return err
	}
	state.response.EmptyCount, err = builderCount(ctx, empty)
	return err
}

// page reads a filtered page, its unpaginated count and the page again from one builder.
//
// Returns error when a generated builder operation fails.
func (state *execution) page(ctx context.Context) error {
	builder, err := state.newBuilder()
	if err != nil {
		return err
	}
	for _, operation := range []struct {
		name      string
		arguments []reflect.Value
	}{
		{name: "Where", arguments: []reflect.Value{reflect.ValueOf("id"), reflect.ValueOf(">="), reflect.ValueOf(int64(state.model.Minimum))}},
		{name: "Limit", arguments: []reflect.Value{reflect.ValueOf(state.model.Limit)}},
		{name: "Offset", arguments: []reflect.Value{reflect.ValueOf(state.model.Offset)}},
	} {
		if _, err := call(builder.MethodByName(operation.name), operation.arguments...); err != nil {
			return err
		}
	}
	if state.response.Page, err = state.read(ctx, builder, "All"); err != nil {
		return err
	}
	if state.response.Count, err = builderCount(ctx, builder); err != nil {
		return err
	}
	state.response.Reused, err = state.read(ctx, builder, "All")
	return err
}

// fresh reads a new builder after another builder has accumulated state.
//
// Returns error when constructing or reading the independent builder fails.
func (state *execution) fresh(ctx context.Context) error {
	builder, err := state.newBuilder()
	if err != nil {
		return err
	}
	state.response.Fresh, err = state.read(ctx, builder, "All")
	return err
}

// newBuilder calls the generated parameterless runtime builder constructor.
//
// Returns reflect.Value which is the generated builder.
// Returns error when the constructor does not return exactly one value.
func (state *execution) newBuilder() (reflect.Value, error) {
	values, err := call(state.queries.MethodByName("Browse" + fuzzcase.RuntimeSuffix(state.model.Kind)))
	if err != nil {
		return reflect.Value{}, err
	}
	if len(values) != 1 {
		return reflect.Value{}, errors.New("generated builder constructor returned the wrong arity")
	}
	return values[0], nil
}

// read invokes a generated many terminal and records ordered rows.
//
// Takes receiver (reflect.Value) which owns the generated method.
// Takes method (string) which identifies the terminal operation.
//
// Returns []Row which preserves every generated value and its NULL flag.
// Returns error when the method or row shape is invalid.
func (state *execution) read(ctx context.Context, receiver reflect.Value, method string) ([]Row, error) {
	values, err := call(receiver.MethodByName(method), reflect.ValueOf(ctx))
	if err != nil {
		return nil, err
	}
	if len(values) != 1 || values[0].Kind() != reflect.Slice {
		return nil, errors.New("generated read returned the wrong shape")
	}
	if values[0].Len() > nativeRowBound {
		return nil, errReadBound
	}
	rows := make([]Row, 0, values[0].Len())
	for index := range values[0].Len() {
		row, err := observedRow(values[0].Index(index), state.model.Kind)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// iterate consumes the generated stream using its actual yield type.
//
// Takes continueAfterRow (func() bool) which decides whether to ask for another row.
//
// Returns []Row which contains the rows delivered before stopping.
// Returns error when the generated iterator or row shape fails.
func (state *execution) iterate(ctx context.Context, continueAfterRow func() bool) ([]Row, error) {
	values, err := call(state.queries.MethodByName("Stream"+fuzzcase.RuntimeSuffix(state.model.Kind)), reflect.ValueOf(ctx))
	if err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, errors.New("generated stream returned the wrong shape")
	}
	var rows []Row
	err = consumeIterator(values[0], func(arguments []reflect.Value) (bool, error) {
		row, err := yieldedRow(arguments, state.model.Kind)
		if err != nil {
			return false, err
		}
		rows = append(rows, row)
		return continueAfterRow(), nil
	})
	return rows, err
}

// Serve handles bounded requests for one compiled engine and real native driver.
//
// Takes engine (string) which selects the independent runtime model.
// Takes factory (Factory) which constructs the generated API.
// Takes input (io.Reader) which supplies private request frames.
// Takes output (io.Writer) which receives complete response frames.
//
// Returns error when the protocol is invalid or a response cannot be written.
func Serve(engine string, factory Factory, input io.Reader, output io.Writer) error {
	return serveFrames(input, output, func(request *Request) (any, error) {
		if request.Version != ProtocolVersion || request.ID == 0 {
			return nil, errInvalidEnvelope
		}
		model, ok := fuzzcase.DecodeRuntime(engine, request.Data)
		if !ok {
			return nil, errors.New("invalid native runtime model")
		}
		return serveRequest(factory, request, model), nil
	})
}

// Run invokes actual generated methods and records their complete observations.
//
// Takes session (*Session) which owns the actual generated query API.
// Takes model (fuzzcase.RuntimeCase) which supplies independent values and operations.
//
// Returns Response which records any error or panic without treating it as success.
func Run(ctx context.Context, session *Session, model fuzzcase.RuntimeCase) (response Response) {
	state := execution{queries: reflect.ValueOf(session.Queries), session: session, model: model, response: Response{}}
	state.response.Prepared, state.response.Native = session.Prepared, session.Native
	defer func() {
		if recovered := recover(); recovered != nil {
			state.response.Panic = fmt.Sprint(recovered)
		}
		response = state.response
	}()
	for _, stage := range runStages {
		state.response.Stage = stage.name
		if ctx.Err() != nil {
			state.response.Error = context.Cause(ctx).Error()
			return state.response
		}
		if err := stage.run(&state, ctx); err != nil {
			state.response.Error = err.Error()
			return state.response
		}
		state.response.Completed = append(state.response.Completed, stage.name)
	}
	state.response.Stage = ""
	return state.response
}

// runStageNames lists the stage names Run completes on success.
//
// Returns []string which holds the names in order.
func runStageNames() []string {
	names := make([]string, 0, len(runStages))
	for _, stage := range runStages {
		names = append(names, stage.name)
	}
	return names
}

// serveRequest owns a native connection for exactly one model execution.
//
// Takes factory (Factory) which constructs the generated API.
// Takes request (*Request) which contains private connection details and identity.
// Takes model (fuzzcase.RuntimeCase) which supplies validated independent choices.
//
// Returns Response which reports failures without exposing the connection string.
func serveRequest(factory Factory, request *Request, model fuzzcase.RuntimeCase) (response Response) {
	response.ID, response.Version = request.ID, ProtocolVersion
	defer func() {
		if recovered := recover(); recovered != nil {
			response.Panic = fmt.Sprint(recovered)
		}
		response.redact(request.Connection)
	}()
	ctx, cancel := context.WithTimeoutCause(context.Background(), nativeInputBound, errInputBound)
	defer cancel()
	response.Stage = "factory"
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
	response = Run(ctx, session, model)
	response.ID, response.Version = request.ID, ProtocolVersion
	return response
}

// builderCount runs a generated builder's Count terminal.
//
// Takes builder (reflect.Value) which is the generated builder.
//
// Returns int64 which is the count.
// Returns error when Count fails or does not return an integer.
func builderCount(ctx context.Context, builder reflect.Value) (int64, error) {
	counted, err := call(builder.MethodByName("Count"), reflect.ValueOf(ctx))
	if err != nil {
		return 0, err
	}
	if len(counted) != 1 || !counted[0].CanInt() {
		return 0, errors.New("generated Count did not return an integer")
	}
	return counted[0].Int(), nil
}

// consumeIterator calls a generated iterator and passes each yield to visit.
//
// The iterator stops when visit returns false or an error. A yield after that, or past
// the row bound, is a failure of the generated iterator.
//
// Takes iterator (reflect.Value) which is the generated iterator function.
// Takes visit (func([]reflect.Value) (bool, error)) which records one yield and says
// whether to continue.
//
// Returns error when the iterator has the wrong shape or misbehaves, or visit fails.
func consumeIterator(iterator reflect.Value, visit func([]reflect.Value) (bool, error)) error {
	if !iteratorShape(iterator) {
		return errors.New("generated stream returned the wrong shape")
	}
	var failure error
	stopped, count := false, 0
	yield := reflect.MakeFunc(iterator.Type().In(0), func(arguments []reflect.Value) []reflect.Value {
		switch {
		case stopped:
			failure = cmp.Or(failure, errYieldAfterStop)
		case count >= nativeRowBound:
			failure, stopped = errStreamBound, true
		default:
			count++
			keep, err := visit(arguments)
			failure, stopped = err, err != nil || !keep
		}
		return []reflect.Value{reflect.ValueOf(!stopped)}
	})
	iterator.Call([]reflect.Value{yield})
	return failure
}

// iteratorShape reports whether a value is a func(yield func(...) bool).
//
// Takes iterator (reflect.Value) which is the generated stream result.
//
// Returns bool which is true when the iterator can be called with a made yield.
func iteratorShape(iterator reflect.Value) bool {
	if !iterator.IsValid() || iterator.Kind() != reflect.Func || iterator.IsNil() {
		return false
	}
	signature := iterator.Type()
	if signature.NumIn() != 1 || signature.NumOut() != 0 || signature.In(0).Kind() != reflect.Func {
		return false
	}
	yield := signature.In(0)
	return yield.NumOut() == 1 && yield.Out(0).Kind() == reflect.Bool
}

// observedRow checks the generated row's identity, value and SQL NULL indicator.
//
// Takes value (reflect.Value) which holds one actual generated row.
// Takes kind (string) which selects the independent value family.
//
// Returns Row which retains exact generated observations.
// Returns error when the generated row omits or misrepresents a required field.
func observedRow(value reflect.Value, kind string) (Row, error) {
	value = followValue(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return Row{}, errors.New("generated row is not a struct")
	}
	id, field, null := value.FieldByName("ID"), value.FieldByName("Value"), value.FieldByName("IsNull")
	if !id.IsValid() || !id.CanInt() || !field.IsValid() || !null.IsValid() {
		return Row{}, errors.New("generated row omits or mistypes ID, Value or IsNull")
	}
	isNull, err := observedBoolean(null)
	if err != nil {
		return Row{}, err
	}
	observed, err := Observed(field, kind, isNull)
	if err != nil {
		return Row{}, err
	}
	return Row{ID: id.Int(), Value: observed, Null: isNull}, nil
}

// call invokes a generated method and separates its terminal error result.
//
// Takes method (reflect.Value) which identifies the generated method.
// Takes arguments (...reflect.Value) which contains assignable generated arguments.
//
// Returns []reflect.Value which omits a trailing error result.
// Returns error when the method is missing or reports a native failure.
func call(method reflect.Value, arguments ...reflect.Value) ([]reflect.Value, error) {
	if !method.IsValid() || method.Kind() != reflect.Func {
		return nil, errors.New("required generated method is missing")
	}
	values := method.Call(arguments)
	if len(values) > 0 && values[len(values)-1].Type() == reflect.TypeFor[error]() {
		last := values[len(values)-1]
		if !last.IsNil() {
			return nil, generatedError(last)
		}
		values = values[:len(values)-1]
	}
	return values, nil
}

// yieldedRow validates one iterator callback before recording its observation.
//
// Takes arguments ([]reflect.Value) which contains the generated row and error.
// Takes kind (string) which supplies the independent value family.
//
// Returns Row which contains the generated observation.
// Returns error when the callback reports a failure or has an invalid signature.
func yieldedRow(arguments []reflect.Value, kind string) (Row, error) {
	if len(arguments) != 2 || arguments[1].Type() != reflect.TypeFor[error]() {
		return Row{}, errors.New("generated iterator yield has the wrong signature")
	}
	if !arguments[1].IsNil() {
		return Row{}, generatedError(arguments[1])
	}
	return observedRow(arguments[0], kind)
}

// generatedError extracts an error without silently accepting a mismatched type.
//
// Takes value (reflect.Value) which holds a non-nil generated error result.
//
// Returns error which preserves the generated failure or reports an invalid result.
func generatedError(value reflect.Value) error {
	failure, ok := reflect.TypeAssert[error](value)
	if !ok || failure == nil {
		return errors.New("generated failure is not an error")
	}
	return failure
}
