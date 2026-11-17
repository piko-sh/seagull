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

package fuzzbuilder

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is the decoder version byte of runtime builder sequences.
const Version = 16

// MaxBytes bounds one structured input.
const MaxBytes = 4096

const (
	// maxRows bounds the modelled table.
	maxRows = 10

	// maxSteps bounds the calls one input makes.
	maxSteps = 32

	// rowWidth is the number of bytes describing one row.
	rowWidth = 3

	// headerWidth is the number of bytes before the row data.
	headerWidth = 5

	// modeByte selects the generated emitter.
	modeByte = 1

	// queryByte selects the initial query of both builder slots.
	queryByte = 2

	// parameterByte selects the initial query's own parameter.
	parameterByte = 3

	// rowsByte selects the row count and the orphan peer row.
	rowsByte = 4

	// orphanFlag adds a peer row no builder row matches.
	orphanFlag = 0x80

	// orphanID identifies the unmatched peer row.
	orphanID = 99

	// peerFlag gives a builder row a peer row.
	peerFlag = 0x80

	// peerNullFlag makes a peer row's note NULL.
	peerNullFlag = 0x40

	// scoreRange bounds non-NULL scores, whose higher choices mean NULL.
	scoreRange = 13

	// scoreChoices is the number of score choices including NULL ones.
	scoreChoices = 16

	// groupCount is the number of distinct categories.
	groupCount = 3

	// mixedCount is the number of distinct Mixed values.
	mixedCount = 4

	// dottedCount is the number of dotted choices, the first of which is NULL.
	dottedCount = 4

	// slotCount is the number of builders a sequence holds at once.
	slotCount = 2

	// parameterCount is the number of values of the query's own parameter.
	parameterCount = 4
)

// Mode selects the generated emitter a sequence runs through.
type Mode uint8

const (
	// ModeDatabaseSQL runs the database/sql emitter.
	ModeDatabaseSQL Mode = iota

	// ModePrepared runs the database/sql emitter with prepared statements.
	ModePrepared

	// ModeNative runs the dedicated pgx or ClickHouse emitter.
	ModeNative
)

// Operation identifies one builder call.
type Operation uint8

const (
	// OperationNew replaces a slot's builder with a new one.
	OperationNew Operation = iota

	// OperationWhere adds a condition.
	OperationWhere

	// OperationOrder adds an ordering.
	OperationOrder

	// OperationLimit sets the row limit.
	OperationLimit

	// OperationOffset sets the row offset.
	OperationOffset

	// OperationAll runs the All terminal.
	OperationAll

	// OperationOne runs the One terminal.
	OperationOne

	// OperationCount runs the Count terminal.
	OperationCount

	// operationCount bounds the operation choice.
	operationCount
)

// operandWidths records how many bytes each operation reads.
var operandWidths = [operationCount]int{2, 5, 2, 1, 1, 0, 0, 0}

// labels are the text values rows hold, chosen so every engine orders them alike.
var labels = []string{"", "a", "ab", "abc", "b", "ba"}

// patterns are the LIKE patterns conditions use in addition to the labels.
var patterns = []string{"a%", "%a", "_", "%b%", "a_c", "%"}

// Case is one decoded runtime builder sequence.
type Case struct {
	// Engine names the dialect.
	Engine string

	// Data retains the original input.
	Data []byte

	// Rows holds the modelled table rows in identifier order.
	Rows []Row

	// Peers holds the peer table rows.
	Peers []Peer

	// Steps holds the calls in order.
	Steps []Step

	// Query selects both slots' initial catalogue query.
	Query int

	// Parameter is the initial query's own parameter value.
	Parameter int64

	// Mode selects the generated emitter.
	Mode Mode
}

// Row is one modelled table row.
type Row struct {
	// Score is the nullable integer, nil for NULL.
	Score *int64

	// Dotted is the nullable column whose name holds a dot, nil for NULL.
	Dotted *int64

	// Label is the short text column.
	Label string

	// Select is the text column named after a reserved word.
	Select string

	// ID is the unique identifier.
	ID int64

	// Group is the repeated category.
	Group int64

	// Mixed is the column named Mixed.
	Mixed int64
}

// Peer is one modelled peer table row.
type Peer struct {
	// Note is the nullable text, nil for NULL.
	Note *string

	// ID matches a builder row's identifier, or none.
	ID int64
}

// Step is one builder call with its arguments.
type Step struct {
	// Value is the Where value: nil, int64, string, []int64 or []string.
	Value any

	// Column is the Where or OrderBy column name.
	Column string

	// Operator is the Where operator.
	Operator string

	// Direction is the OrderBy direction.
	Direction string

	// Parameter is the new builder's own parameter value.
	Parameter int64

	// Op identifies the call.
	Op Operation

	// Slot selects the builder the call uses.
	Slot int

	// Query selects the new builder's catalogue query.
	Query int

	// Number is the Limit or Offset argument.
	Number int
}

// Inserts writes the modelled rows as literal INSERT statements.
//
// Returns []string which holds one statement per row.
func (c Case) Inserts() []string {
	quote := quoter(c.Engine)
	statements := make([]string, 0, len(c.Rows)+len(c.Peers))
	for _, row := range c.Rows {
		statements = append(statements, fmt.Sprintf("INSERT INTO %s (id, grp, score, label, %s, %s, %s) VALUES (%d, %d, %s, '%s', %d, '%s', %s)",
			builderTable, quote("Mixed"), quote("select"), quote("a.b"), row.ID, row.Group, integerLiteral(row.Score), row.Label,
			row.Mixed, row.Select, integerLiteral(row.Dotted)))
	}
	for _, peer := range c.Peers {
		note := "NULL"
		if peer.Note != nil {
			note = "'" + *peer.Note + "'"
		}
		statements = append(statements, fmt.Sprintf("INSERT INTO %s (id, note) VALUES (%d, %s)", peerTable, peer.ID, note))
	}
	return statements
}

// String describes the case for failure messages.
//
// Returns string which lists the steps.
func (c Case) String() string {
	var text strings.Builder
	fmt.Fprintf(&text, "%s mode %d rows %d query %s(%d)", c.Engine, c.Mode, len(c.Rows), QueryName(c.Query), c.Parameter)
	for _, step := range c.Steps {
		fmt.Fprintf(&text, "; %s", step)
	}
	return text.String()
}

// Decode reads a version 16 input without consulting Seagull.
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
	original := data
	body := strings.TrimSuffix(string(data), RegressionMarker)
	data = []byte(body)
	at := func(index int) byte {
		if index < len(data) {
			return data[index]
		}
		return 0
	}
	result := Case{Engine: engine, Data: append([]byte(nil), original...), Rows: nil, Peers: nil, Steps: nil,
		Query: int(at(queryByte)) % int(queryCount), Parameter: int64(at(parameterByte) % parameterCount),
		Mode: decodeMode(engine, at(modeByte))}
	count := int(at(rowsByte)&^orphanFlag) % (maxRows + 1)
	for index := range count {
		start := headerWidth + index*rowWidth
		row, peer := decodeRow(int64(index+1), at(start), at(start+1), at(start+2))
		result.Rows = append(result.Rows, row)
		if peer != nil {
			result.Peers = append(result.Peers, *peer)
		}
	}
	if at(rowsByte)&orphanFlag != 0 {
		note := labels[len(labels)-1]
		result.Peers = append(result.Peers, Peer{Note: &note, ID: orphanID})
	}
	result.Steps = decodeSteps(data[min(headerWidth+count*rowWidth, len(data)):], result.Query)
	return result, true
}

// decodeSteps reads the calls that follow the row data.
//
// Takes data ([]byte) which holds the call codes and operands.
// Takes query (int) which is both slots' initial query.
//
// Returns []Step which holds at most maxSteps calls.
func decodeSteps(data []byte, query int) []Step {
	var steps []Step
	slots := [slotCount]int{query, query}
	for position := 0; position < len(data) && len(steps) < maxSteps; {
		code := data[position]
		operands := make([]byte, operandWidths[Operation(code%byte(operationCount))])
		for index := range operands {
			if position+1+index < len(data) {
				operands[index] = data[position+1+index]
			}
		}
		step := decodeStep(code, operands, &slots)
		position += 1 + len(operands)
		steps = append(steps, step)
	}
	return steps
}

// decodeMode selects an emitter the engine supports.
//
// Takes engine (string) which names the dialect.
// Takes choice (byte) which holds the requested mode in its low bits.
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
		switch engine {
		case enginePostgres, engineTimescale, engineCockroach, engineClickHouse:
			return true
		default:
			return false
		}
	default:
		return true
	}
}

// decodeRow builds one row and its optional peer from three choice bytes.
//
// Takes id (int64) which is the row's identifier.
// Takes first (byte) which selects the category, Mixed value and peer note.
// Takes second (byte) which selects the score, dotted value and peer presence.
// Takes third (byte) which selects the labels.
//
// Returns Row which is the builder row.
// Returns *Peer which is its peer row, or nil.
func decodeRow(id int64, first, second, third byte) (Row, *Peer) {
	row := Row{Score: nil, Dotted: nil, Label: labels[int(third)%len(labels)], Select: labels[int(third)/len(labels)%len(labels)],
		ID: id, Group: int64(first % groupCount), Mixed: int64(first / groupCount % mixedCount)}
	if score := int64(second % scoreChoices); score < scoreRange {
		row.Score = &score
	}
	if dotted := int64(second / scoreChoices % dottedCount); dotted != 0 {
		row.Dotted = &dotted
	}
	if second&peerFlag == 0 {
		return row, nil
	}
	peer := &Peer{Note: nil, ID: id}
	if second&peerNullFlag == 0 {
		note := labels[int(first>>groupCount)%len(labels)]
		peer.Note = &note
	}
	return row, peer
}

// decodeStep builds one call from its code and operands.
//
// Takes code (byte) which selects the operation and slot.
// Takes operands ([]byte) which hold the operation's choices.
// Takes slots (*[slotCount]int) which tracks each slot's current query.
//
// Returns Step which is the call.
func decodeStep(code byte, operands []byte, slots *[slotCount]int) Step {
	step := Step{Value: nil, Column: "", Operator: "", Direction: "", Parameter: 0, Op: Operation(code % byte(operationCount)),
		Slot: int(code/byte(operationCount)) % slotCount, Query: 0, Number: 0}
	switch step.Op {
	case OperationNew:
		step.Query, step.Parameter = int(operands[0])%int(queryCount), int64(operands[1]%parameterCount)
		slots[step.Slot] = step.Query
	case OperationLimit, OperationOffset:
		step.Number = int(operands[0]%(maxRows+1)) - 1
	case OperationWhere:
		var text bool
		step.Column, text = chooseColumn(&catalogue[slots[step.Slot]], operands[0])
		step.Operator = operators[int(operands[1])%len(operators)]
		if step.Operator == operatorLike && !text {
			step.Operator = operatorEqual
		}
		step.Value = chooseValue(step.Operator, text, operands[2:])
	case OperationOrder:
		step.Column, _ = chooseColumn(&catalogue[slots[step.Slot]], operands[0])
		step.Direction = chooseDirection(operands[1])
	default:
	}
	return step
}

// integerLiteral writes a nullable integer.
//
// Takes value (*int64) which is nil for NULL.
//
// Returns string which is the SQL literal.
func integerLiteral(value *int64) string {
	if value == nil {
		return "NULL"
	}
	return strconv.FormatInt(*value, decimalBase)
}

// QueryName returns a catalogue query's generated method name.
//
// Takes index (int) which selects the query.
//
// Returns string which is the method name.
func QueryName(index int) string {
	return catalogue[index%int(queryCount)].name
}

// QueryHasParameter reports whether a catalogue query takes its own parameter.
//
// Takes index (int) which selects the query.
//
// Returns bool which is true when the builder constructor takes an argument.
func QueryHasParameter(index int) bool {
	return catalogue[index%int(queryCount)].kind == kindParameter
}

// QueryWidth returns a catalogue query's output width.
//
// Takes index (int) which selects the query.
//
// Returns int which is the number of output columns.
func QueryWidth(index int) int {
	return len(catalogue[index%int(queryCount)].columns)
}
