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

package sqlmodel

const (
	// SegmentText is SQL text, written as Text holds it.
	SegmentText SQLSegmentKind = iota

	// SegmentParameter is one placeholder occurrence of the parameter at index Parameter of
	// AnalysedQuery.Parameters.
	//
	// TypeText is the type a typed placeholder form carries, as the query writes it, such as
	// UInt64 in {id:UInt64}, and is empty for a form without a type. A renderer writes the
	// placeholder in the form the code executes.
	SegmentParameter

	// SegmentList represents a slice parameter used as an IN or NOT IN list. The renderer
	// chooses expanded placeholders or the dialect's array form.
	SegmentList

	// SegmentColumn represents a top-level output item and its expression.
	//
	// Renderers can wrap the expression while preserving its output alias.
	SegmentColumn

	// SegmentStar represents a star output item and the columns used to expand it. AsWritten
	// preserves the original star unless one of its columns needs wrapping.
	SegmentStar

	// SegmentFilter is the WHERE condition a dynamic query or a runtime builder adds
	// predicates to.
	//
	// When Present, Children are the condition alone, the WHERE keyword preceding the
	// segment as text. Otherwise the segment has no children and marks where a WHERE clause
	// goes; code inserting one writes its own leading space.
	SegmentFilter

	// SegmentOrderBy is the ORDER BY clause, its keywords and modifiers included, as
	// Children when Present; otherwise it has no children and marks where the clause goes.
	SegmentOrderBy

	// SegmentPagination is the LIMIT, OFFSET and FETCH clauses as written, keywords
	// included, as Children when Present; otherwise it has no children and marks where they
	// go.
	SegmentPagination

	// SegmentValuesRow is the single row of an INSERT ... VALUES with exactly one row, from
	// its opening parenthesis through its closing one, as Children.
	SegmentValuesRow
)

var (
	// sqlSegmentKindNames names each SQLSegmentKind, indexed by the kind's value.
	sqlSegmentKindNames = [...]string{
		SegmentText:       "text",
		SegmentParameter:  "parameter",
		SegmentList:       "list",
		SegmentColumn:     "column",
		SegmentStar:       "star",
		SegmentFilter:     "filter",
		SegmentOrderBy:    "order-by",
		SegmentPagination: "pagination",
		SegmentValuesRow:  "values-row",
	}
)

// SQLSegmentKind says what one segment of a SQLTemplate holds.
type SQLSegmentKind uint8

// String returns the kind's name, such as parameter or order-by.
//
// Returns string which is the name, or SQLSegmentKind(n) for an undeclared value.
func (kind SQLSegmentKind) String() string {
	return enumName(kind, sqlSegmentKindNames[:], "SQLSegmentKind")
}

// SQLTemplate is a query's executable SQL as segments: text as written, and the parts
// code generation renders per driver, which are the placeholders, slice lists, output
// columns and the places a dynamic query edits. Rendering it in a dialect's placeholder
// form gives driver-correct SQL; see emit_kit.Render.
//
// The analyser builds it from the statement's spans. Statements before the query's own
// stay as text, and directive comment lines are left out. The zero template holds no SQL.
type SQLTemplate struct {
	// Segments holds the SQL in order.
	Segments []SQLSegment

	// Arguments lists parameter indexes in driver bind order. A slice parameter's entry
	// represents all of its elements.
	Arguments []int
}

// SQLSegment is one part of a SQLTemplate.
//
// Which fields it uses depends on its Kind; see the SQLSegmentKind constants. A field a
// kind does not use is zero.
type SQLSegment struct {
	// Text is SQL written as is for SegmentText, the item as written for SegmentStar, and
	// the operator as written for SegmentList.
	Text string

	// TypeText is the type a typed placeholder carries, such as UInt64, for SegmentParameter
	// and SegmentList.
	TypeText string

	// Children holds the segments inside a SegmentColumn, SegmentFilter, SegmentOrderBy,
	// SegmentPagination or SegmentValuesRow.
	Children []SQLSegment

	// Star lists the columns a SegmentStar stands for, in order.
	Star []StarColumn

	// Parameter is the index into AnalysedQuery.Parameters of the parameter a
	// SegmentParameter or SegmentList binds.
	Parameter int

	// Column is the index into AnalysedQuery.OutputColumns of a SegmentColumn's column.
	Column int

	// Kind says what the segment holds.
	Kind SQLSegmentKind

	// Negated reports a SegmentList written NOT IN.
	Negated bool

	// Aliased reports a SegmentColumn whose item gives itself an alias.
	Aliased bool

	// Present reports that the clause of a SegmentFilter, SegmentOrderBy or
	// SegmentPagination is written in the query, rather than only its place marked.
	Present bool

	// AsWritten preserves a SegmentStar whose output columns cannot be named reliably in its
	// place. Wrapping a column still requires expansion.
	AsWritten bool
}

// StarColumn is one output column a star stands for.
type StarColumn struct {
	// Qualifier is the table or alias that qualifies the column when the star is expanded,
	// or empty to write the name alone.
	Qualifier string

	// Name is the column's name in its relation, unquoted.
	Name string

	// Column is the index into AnalysedQuery.OutputColumns of the column.
	Column int
}

// TableName names a table as a query writes it.
type TableName struct {
	// Schema is the schema the query writes, or empty when the table resolves through the
	// dialect's search path.
	Schema string

	// Name is the table's name.
	Name string
}
