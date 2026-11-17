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
	"cmp"
	"slices"
	"strconv"
	"strings"
)

const (
	// ClassUnknownColumn is the builder's refusal of a column.
	ClassUnknownColumn = "unknown-column"

	// ClassUnknownOperator is the builder's refusal of an operator.
	ClassUnknownOperator = "unknown-operator"

	// ClassUnknownDirection is the builder's refusal of a direction.
	ClassUnknownDirection = "unknown-direction"

	// ClassNoRows is the One terminal's report of an empty result.
	ClassNoRows = "no-rows"

	// ClassMissingMethod reports a builder without the called method.
	ClassMissingMethod = "missing-method"

	// ClassError is any other failure, which no outcome expects.
	ClassError = "error"
)

// Cell is one result value.
type Cell struct {
	// Text holds a textual value.
	Text string

	// Integer holds an integer value.
	Integer int64

	// Null reports SQL NULL.
	Null bool

	// IsText reports a textual value.
	IsText bool
}

// Observation records what one call returned.
type Observation struct {
	// Class classifies a failure, empty for success.
	Class string

	// Error holds the failure text.
	Error string

	// Rows holds the All rows or the One row.
	Rows [][]Cell

	// Count holds the Count result.
	Count int64
}

// key is one ordering key over an output column.
type key struct {
	// column indexes the output column.
	column int

	// descending reverses the order of values.
	descending bool

	// nullsFirst places NULL before every value.
	nullsFirst bool
}

// predicate is one accepted condition.
type predicate struct {
	// value is the condition's value.
	value any

	// operator is the condition's operator.
	operator string

	// column indexes the output column.
	column int
}

// slot is the modelled state of one builder.
type slot struct {
	// pending is the class of the first refused input, empty when none.
	pending string

	// predicates holds the accepted conditions.
	predicates []predicate

	// keys holds the accepted orderings.
	keys []key

	// parameter is the query's own parameter value.
	parameter int64

	// query indexes the catalogue query.
	query int

	// limit is the row limit, none when nonpositive.
	limit int

	// offset is the row offset, none when nonpositive.
	offset int
}

// outcome is the model's expectation of one call.
type outcome struct {
	// class is the expected failure class, empty for success.
	class string

	// page holds the expected rows in order.
	page [][]Cell

	// filtered holds every row the conditions admit, the rows a page may draw from.
	filtered [][]Cell

	// keys holds the ordering the page follows.
	keys []key

	// count is the expected Count result.
	count int64
}

// outcomes evaluates every call against the row model.
//
// Returns []outcome which holds one expectation per step.
func (c Case) outcomes() []outcome {
	slots := [slotCount]slot{{pending: "", predicates: nil, keys: nil, parameter: c.Parameter, query: c.Query, limit: 0, offset: 0}}
	slots[1] = slots[0]
	results := make([]outcome, 0, len(c.Steps))
	for _, step := range c.Steps {
		state := &slots[step.Slot]
		results = append(results, c.apply(state, step))
	}
	return results
}

// apply changes one builder's state or evaluates its terminal.
//
// Takes state (*slot) which is the builder the call uses.
// Takes step (Step) which is the call.
//
// Returns outcome which is the expectation of the call.
func (c Case) apply(state *slot, step Step) outcome {
	result := outcome{class: "", page: nil, filtered: nil, keys: nil, count: 0}
	entry := &catalogue[state.query]
	switch step.Op {
	case OperationNew:
		*state = slot{pending: "", predicates: nil, keys: nil, parameter: step.Parameter, query: step.Query, limit: 0, offset: 0}
	case OperationWhere:
		if !entry.where {
			result.class = ClassMissingMethod
			return result
		}
		state.where(entry, step)
	case OperationOrder:
		state.order(c.Engine, entry, step)
	case OperationLimit:
		state.limit = step.Number
	case OperationOffset:
		state.offset = step.Number
	default:
		return c.terminal(state, step.Op)
	}
	return result
}

// where records a condition or the refusal of its first invalid input.
//
// Takes entry (*query) which is the builder's query.
// Takes step (Step) which is the Where call.
func (s *slot) where(entry *query, step Step) {
	column := entry.allowed(step.Column)
	switch {
	case column < 0:
		s.refuse(ClassUnknownColumn)
	case !slices.Contains(operators[:len(operators)-refusedSpellings], step.Operator):
		s.refuse(ClassUnknownOperator)
	default:
		s.predicates = append(s.predicates, predicate{value: step.Value, operator: step.Operator, column: column})
	}
}

// order records an ordering or the refusal of its first invalid input.
//
// Takes engine (string) which supplies the default NULL placement.
// Takes entry (*query) which is the builder's query.
// Takes step (Step) which is the OrderBy call.
func (s *slot) order(engine string, entry *query, step Step) {
	column := entry.allowed(step.Column)
	direction := strings.ToUpper(step.Direction)
	switch {
	case column < 0:
		s.refuse(ClassUnknownColumn)
	case !slices.Contains(directions[:len(directions)-refusedSpellings], direction):
		s.refuse(ClassUnknownDirection)
	default:
		descending := strings.HasPrefix(direction, "DESC")
		nullsFirst := defaultNullsFirst(engine, descending)
		placed := strings.Contains(direction, "NULLS")
		if placed && strings.HasSuffix(direction, nullsFirstSuffix) != nullsFirst && engine == engineCockroach && entry.kind == kindDistinct {

			s.refuse(ClassUnknownDirection)
			return
		}
		if placed {
			nullsFirst = strings.HasSuffix(direction, nullsFirstSuffix)
		}
		s.keys = append(s.keys, key{column: column, descending: descending, nullsFirst: nullsFirst})
	}
}

// refuse records the first refused input, which every later terminal returns.
//
// Takes class (string) which classifies the refusal.
func (s *slot) refuse(class string) {
	if s.pending == "" {
		s.pending = class
	}
}

// allowed finds the documented allow-list entry for a name.
//
// Takes name (string) which is the name a call passes.
//
// Returns int which indexes the output column, or -1 when the builder must refuse it. It
// refuses a name no output column has exactly, and a converted, computed or aggregated
// column.
func (q *query) allowed(name string) int {
	for index := range q.columns {
		if q.columns[index].name == name && !q.columns[index].refused {
			return index
		}
	}
	return -1
}

// terminal evaluates All, One or Count.
//
// Takes state (*slot) which is the builder.
// Takes operation (Operation) which selects the terminal.
//
// Returns outcome which is the expectation.
func (c Case) terminal(state *slot, operation Operation) outcome {
	result := outcome{class: state.pending, page: nil, filtered: nil, keys: nil, count: 0}
	if result.class != "" {
		return result
	}
	entry := &catalogue[state.query]
	for _, row := range c.output(entry.kind, state.parameter) {
		if state.admits(row) {
			result.filtered = append(result.filtered, row)
		}
	}
	result.count = int64(len(result.filtered))
	result.keys = append(ownKeys(c.Engine, entry.kind), state.keys...)
	ordered := slices.Clone(result.filtered)
	slices.SortStableFunc(ordered, func(left, right []Cell) int { return compareRows(result.keys, left, right) })
	result.page = ordered[min(max(state.offset, 0), len(ordered)):]
	if state.limit > 0 {
		result.page = result.page[:min(state.limit, len(result.page))]
	}
	if operation == OperationOne {
		if len(result.page) == 0 {
			result.class = ClassNoRows
		}
		result.page = result.page[:min(1, len(result.page))]
	}
	return result
}

// admits applies every condition with SQL's three-valued logic.
//
// Takes row ([]Cell) which is one output row.
//
// Returns bool which is true when every condition is true.
func (s *slot) admits(row []Cell) bool {
	for _, condition := range s.predicates {
		if !condition.holds(row[condition.column]) {
			return false
		}
	}
	return true
}

// holds evaluates one condition, treating unknown as false.
//
// Takes cell (Cell) which is the column's value.
//
// Returns bool which is true only when the condition is true.
func (p predicate) holds(cell Cell) bool {
	switch p.operator {
	case operatorIsNull:
		return cell.Null
	case operatorIsNotNull:
		return !cell.Null
	case operatorIn, operatorNotIn:
		return p.member(cell)
	}
	if cell.Null || p.value == nil {
		return false
	}
	if pattern, ok := p.value.(string); ok && p.operator == operatorLike {
		return like(cell.Text, pattern)
	}
	order := compareValue(cell, p.value)
	switch p.operator {
	case operatorEqual:
		return order == 0
	case operatorLess:
		return order < 0
	case operatorGreater:
		return order > 0
	case operatorAtMost:
		return order <= 0
	case operatorAtLeast:
		return order >= 0
	default:
		return order != 0
	}
}

// member evaluates IN and NOT IN over a list or a single value.
//
// Takes cell (Cell) which is the column's value.
//
// Returns bool which is true only when the membership test is true.
func (p predicate) member(cell Cell) bool {
	var list []any
	switch values := p.value.(type) {
	case []int64:
		for _, value := range values {
			list = append(list, value)
		}
	case []string:
		for _, value := range values {
			list = append(list, value)
		}
	case nil:
	default:
		list = []any{values}
	}
	if len(list) == 0 {
		return p.operator == operatorNotIn
	}
	if cell.Null {
		return false
	}
	found := slices.ContainsFunc(list, func(value any) bool { return compareValue(cell, value) == 0 })
	return found == (p.operator == operatorIn)
}

// defaultNullsFirst gives an engine's NULL placement without NULLS FIRST or LAST.
//
// Takes engine (string) which names the dialect.
// Takes descending (bool) which reports a descending key.
//
// Returns bool which is true when NULL sorts before every value.
func defaultNullsFirst(engine string, descending bool) bool {
	switch engine {
	case enginePostgres, engineTimescale:
		return descending
	case engineDuckDB, engineClickHouse:
		return false
	default:
		return !descending
	}
}

// ownKeys gives the ordering a catalogue query writes itself.
//
// Takes engine (string) which supplies the default NULL placement.
// Takes kind (queryKind) which selects the query.
//
// Returns []key which the builder's orderings follow.
func ownKeys(engine string, kind queryKind) []key {
	if kind == kindOrdered {
		return []key{{column: 1, descending: true, nullsFirst: defaultNullsFirst(engine, true)}}
	}
	return nil
}

// compareValue orders a non-NULL cell against a condition value of its kind.
//
// Takes cell (Cell) which is the column's value.
// Takes value (any) which is an int64 or a string.
//
// Returns int which is negative, zero or positive.
func compareValue(cell Cell, value any) int {
	switch typed := value.(type) {
	case string:
		return strings.Compare(cell.Text, typed)
	case int64:
		return cmp.Compare(cell.Integer, typed)
	default:
		return 1
	}
}

// like matches SQL LIKE with percent and underscore wildcards and no escape.
//
// Takes text (string) which is the value.
// Takes pattern (string) which is the pattern.
//
// Returns bool which reports a match.
func like(text, pattern string) bool {
	if pattern == "" {
		return text == ""
	}
	switch pattern[0] {
	case '%':
		for start := 0; start <= len(text); start++ {
			if like(text[start:], pattern[1:]) {
				return true
			}
		}
		return false
	case '_':
		return text != "" && like(text[1:], pattern[1:])
	default:
		return text != "" && text[0] == pattern[0] && like(text[1:], pattern[1:])
	}
}

// compareRows orders two rows by the keys.
//
// Takes keys ([]key) which is the ordering.
// Takes left ([]Cell) which is the first row.
// Takes right ([]Cell) which is the second row.
//
// Returns int which is negative, zero or positive.
func compareRows(keys []key, left, right []Cell) int {
	for _, current := range keys {
		if order := compareCells(current, left[current.column], right[current.column]); order != 0 {
			return order
		}
	}
	return 0
}

// compareCells orders two values of one key.
//
// Takes current (key) which supplies the direction and NULL placement.
// Takes left (Cell) which is the first value.
// Takes right (Cell) which is the second value.
//
// Returns int which is negative, zero or positive.
func compareCells(current key, left, right Cell) int {
	switch {
	case left.Null && right.Null:
		return 0
	case left.Null || right.Null:
		if left.Null == current.nullsFirst {
			return -1
		}
		return 1
	}
	order := cmp.Compare(left.Integer, right.Integer)
	if left.IsText {
		order = strings.Compare(left.Text, right.Text)
	}
	if current.descending {
		return -order
	}
	return order
}

// describeCell writes a cell for failure messages.
//
// Takes cell (Cell) which is the value.
//
// Returns string which distinguishes NULL, text and integers.
func describeCell(cell Cell) string {
	switch {
	case cell.Null:
		return "NULL"
	case cell.IsText:
		return strconv.Quote(cell.Text)
	default:
		return strconv.FormatInt(cell.Integer, decimalBase)
	}
}
