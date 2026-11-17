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

package fuzztyped

import (
	"fmt"
	"strings"
)

// Version is the decoder version byte of typed value and expression inputs.
const Version = 15

// MaxBytes bounds one structured input.
const MaxBytes = 4096

const (
	// maxRows bounds the rows one input writes.
	maxRows = 6

	// modeByte selects the generated emitter.
	modeByte = 1

	// familyByte selects the value family or the expression case.
	familyByte = 2

	// countByte selects the row count.
	countByte = 3

	// choiceStart is the first value choice.
	choiceStart = 4

	// expressionRowWidth is the number of choice bytes per expression row.
	expressionRowWidth = 3

	// expressionFamily names the expression case in failure messages.
	expressionFamily = "Expr"

	// checkCalls counts the read, filter, join, update and second read after the inserts.
	checkCalls = 5
)

// Mode selects the generated emitter an input runs through.
type Mode uint8

const (
	// ModeDatabaseSQL runs the database/sql emitter.
	ModeDatabaseSQL Mode = iota

	// ModePrepared runs the database/sql emitter with prepared statements.
	ModePrepared

	// ModeNative runs the dedicated pgx or ClickHouse emitter.
	ModeNative
)

// Case is one decoded typed value or expression input.
type Case struct {
	// Engine names the dialect.
	Engine string

	// Family names the value family, or Expr for the expression case.
	Family string

	// Data retains the original input.
	Data []byte

	// Calls holds the generated method calls in order.
	Calls []Call

	// Inserts holds the expression rows written directly before the calls.
	Inserts []string

	// Mode selects the generated emitter.
	Mode Mode
}

// Call is one generated method call and the result the model expects.
type Call struct {
	// Method is the generated method name.
	Method string

	// Arguments holds the parameters in placeholder order.
	Arguments []Value

	// Names gives each argument's generated field name, nil to fill fields in order.
	Names []string

	// Shapes gives each result column's kind, a list kind followed by its member's.
	Shapes []string

	// Want holds the expected rows of a query.
	Want [][]Value

	// Exec reports a call that returns no rows.
	Exec bool
}

// Observation records what one call returned.
type Observation struct {
	// Error holds the failure text, empty for success.
	Error string

	// Rows holds the returned rows.
	Rows [][]Value
}

// choiceReader reads choice bytes in order, zero past the end.
type choiceReader struct {
	// data holds the input.
	data []byte

	// position is the next choice's index.
	position int
}

// at reads one choice byte by index.
//
// Takes index (int) which locates the byte.
//
// Returns byte which is zero past the end.
func (r *choiceReader) at(index int) byte {
	if index < len(r.data) {
		return r.data[index]
	}
	return 0
}

// next reads the next choice.
//
// Returns int which is the choice.
func (r *choiceReader) next() int {
	choice := r.at(r.position)
	r.position++
	return int(choice)
}

// choose picks a boundary value the engine stores, skipping those it refuses.
//
// Takes engine (string) which names the dialect.
// Takes choice (int) which selects the first candidate.
//
// Returns written (Value) which is the value written.
// Returns stored (Value) which is the value a read returns.
func (f *family) choose(engine string, choice int) (written, stored Value) {
	for offset := range f.values {
		value := f.values[(choice+offset)%len(f.values)]
		read := value
		if f.required && read.Kind == KindNull {
			read = List()
		}
		if unsendableEpoch(engine, value) {
			continue
		}
		if after, ok := f.stored(engine, read); ok {
			return value, after
		}
	}
	return Null(), Null()
}

// read builds the call that reads every row back.
//
// Takes stored ([]Value) which holds each row's expected value.
// Takes shape (string) which is the value column's kind.
//
// Returns Call which expects the rows in identifier order.
func (f *family) read(stored []Value, shape string) Call {
	want := make([][]Value, 0, len(stored))
	for index, value := range stored {
		want = append(want, []Value{Integer(int64(index + 1)), value})
	}
	return Call{Method: "Read" + f.name, Arguments: nil, Names: nil, Shapes: []string{KindInteger, shape}, Want: want, Exec: false}
}

// shape gives the kind of the family's values, a list's kind followed by its member's.
//
// Returns string which is the kind.
func (f *family) shape() string {
	longest := ""
	for _, value := range f.values {
		if shape := valueShape(value); value.Kind != KindNull && len(shape) > len(longest) {
			longest = shape
		}
	}
	return longest
}

// String describes the case for failure messages.
//
// Returns string which lists the calls.
func (c Case) String() string {
	var text strings.Builder
	fmt.Fprintf(&text, "%s mode %d family %s", c.Engine, c.Mode, c.Family)
	for _, call := range c.Calls {
		fmt.Fprintf(&text, "; %s%v", call.Method, call.Arguments)
	}
	return text.String()
}

// Decode reads a version 15 input without consulting Seagull.
//
// Takes engine (string) which names the dialect.
// Takes data ([]byte) which holds the version and choices.
//
// Returns Case which owns a copy of the input.
// Returns bool which is false for another version, an unmodelled engine or an oversized
// input.
func Decode(engine string, data []byte) (Case, bool) {
	if len(data) == 0 || len(data) > MaxBytes || data[0] != Version || !Supported(engine) {
		return Case{}, false
	}
	body := strings.TrimSuffix(string(data), RegressionMarker)
	choices := choiceReader{data: []byte(body), position: choiceStart}
	result := Case{Engine: engine, Family: "", Data: append([]byte(nil), data...), Calls: nil, Inserts: nil,
		Mode: decodeMode(engine, choices.at(modeByte))}
	rows := 1 + int(choices.at(countByte))%maxRows
	usable := admitted(engine)
	selected := int(choices.at(familyByte)) % (len(usable) + 1)
	if selected == len(usable) {
		result.Family = expressionFamily
		result.Inserts, result.Calls = decodeExpressions(engine, rows, &choices)
		return result, true
	}
	result.Family = usable[selected].name
	result.Calls = decodeValues(engine, usable[selected], rows, &choices)
	return result, true
}

// decodeMode selects an emitter the engine supports.
//
// Takes engine (string) which names the dialect.
// Takes choice (byte) which holds the requested mode.
//
// Returns Mode which falls back to database/sql where the engine lacks the mode.
func decodeMode(engine string, choice byte) Mode {
	mode := Mode(choice % (byte(ModeNative) + 1))
	if !ModeSupported(engine, mode) {
		return ModeDatabaseSQL
	}
	return mode
}

// ModeSupported reports whether an engine has a generated emitter mode.
//
// Takes engine (string) which names the dialect.
// Takes mode (Mode) which selects the emitter.
//
// Returns bool which excludes preparation on ClickHouse, whose database/sql driver
// prepares only INSERT batches, and native drivers where the engine has none.
func ModeSupported(engine string, mode Mode) bool {
	switch mode {
	case ModePrepared:
		return engine != engineClickHouse
	case ModeNative:
		return engine == enginePostgres || engine == engineTimescale || engine == engineCockroach || engine == engineClickHouse
	default:
		return true
	}
}

// decodeValues builds a value family's calls.
//
// Takes engine (string) which names the dialect.
// Takes entry (*family) which is the family.
// Takes rows (int) which is the number of rows to write.
// Takes choices (*choiceReader) which supplies the value choices.
//
// Returns []Call which inserts, reads, filters, joins, updates and reads again.
func decodeValues(engine string, entry *family, rows int, choices *choiceReader) []Call {
	written := make([]Value, rows)
	stored := make([]Value, rows)
	calls := make([]Call, 0, rows+checkCalls)
	for index := range written {
		written[index], stored[index] = entry.choose(engine, choices.next())
		calls = append(calls, Call{Method: "Insert" + entry.name, Arguments: []Value{Integer(int64(index + 1)), written[index]},
			Names: []string{"ID", "V"}, Shapes: nil, Want: nil, Exec: true})
	}
	shape := entry.shape()
	calls = append(calls, entry.read(stored, shape))
	if entry.filtered {
		choice := choices.next()
		filter, filterStored := written[choice/2%rows], stored[choice/2%rows]
		if choice%2 != 0 {
			filter, filterStored = entry.choose(engine, choice/2)
		}
		var matched [][]Value
		for index, value := range stored {
			if entry.equal(engine, value, filterStored) {
				matched = append(matched, []Value{Integer(int64(index + 1))})
			}
		}
		for _, method := range []string{"Where", "Join"} {
			calls = append(calls, Call{Method: method + entry.name, Arguments: []Value{filter}, Names: nil, Shapes: []string{KindInteger}, Want: matched, Exec: false})
		}
	}
	if engine != engineClickHouse {
		target := choices.next() % rows
		value, after := entry.choose(engine, choices.next())
		if keepsEqualFloat(engine, stored[target], after) {
			after = stored[target]
		}
		stored[target] = after
		calls = append(calls, Call{Method: "Update" + entry.name, Arguments: []Value{value, Integer(int64(target + 1))}, Names: []string{"V", "ID"},
			Shapes: nil, Want: nil, Exec: true},
			entry.read(stored, shape))
	}
	return calls
}

// keepsEqualFloat reports an update DuckDB skips. Writing a float equal to the stored
// one, as zero is to negative zero, leaves the stored bits.
//
// Takes engine (string) which names the dialect.
// Takes before (Value) which is the stored value.
// Takes after (Value) which is the value written.
//
// Returns bool which is true when the stored value stays.
func keepsEqualFloat(engine string, before, after Value) bool {
	return engine == engineDuckDB && before.Kind == KindFloat && after.Kind == KindFloat && before.Float() == after.Float()
}

// valueShape describes a value's kind with its list members' kinds.
//
// Takes value (Value) which is a non-NULL value.
//
// Returns string which is the kind, such as list list integer.
func valueShape(value Value) string {
	if value.Kind != KindList {
		return value.Kind
	}
	for _, item := range value.Items {
		if item.Kind != KindNull {
			return KindList + " " + valueShape(item)
		}
	}
	return KindList + " " + KindInteger
}
