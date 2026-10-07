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

package emit_kit

import (
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// ListExpand writes a slice parameter's list as one placeholder in parentheses, which
	// the generated code splices into one placeholder per element at run time.
	ListExpand = emitkit.ListExpand

	// ListArray writes, in place of the operator and its list, one placeholder bound to the
	// whole slice as an array inside the dialect's array membership form, or its exclusion
	// form for NOT IN.
	ListArray = emitkit.ListArray

	// ListAsWritten writes the operator and one placeholder in parentheses, bound to the
	// whole slice as one value, for a driver that expands a slice itself.
	ListAsWritten = emitkit.ListAsWritten

	// ColumnWrapJSON wraps a column in the dialect's to-JSON function; the generated code
	// decodes the JSON into the column's Go type.
	ColumnWrapJSON = emitkit.ColumnWrapJSON

	// ColumnWrapText casts a column to the dialect's text type; the generated code scans the
	// text.
	ColumnWrapText = emitkit.ColumnWrapText

	// BatchRows repeats a query's VALUES row once per row of a multi-row INSERT, as the row
	// is written, expressions and literals included.
	BatchRows = emitkit.BatchRows

	// BatchColumns sends each row's values as data, column by column, as a copy protocol or
	// a column-oriented batch does, so every item of the VALUES row must be one placeholder
	// alone, whose position names the column it fills.
	BatchColumns = emitkit.BatchColumns
)

// Rendering a query's template as SQL for one driver.
type (
	// Renderer says how Render writes a template for one driver: the placeholder form, how
	// slice lists are written, which output columns are wrapped and which clauses are left
	// out. One renderer serves one query, whose parameters and output columns the template's
	// indices refer to.
	Renderer = emitkit.Renderer

	// Rendered is a template written as SQL for one driver: the SQL, the arguments in the
	// order the driver binds them, and the Marks the generated code edits at.
	Rendered = emitkit.Rendered

	// Marks locates in Rendered.SQL the parts the generated code edits at run time. A mark
	// for a segment the template does not hold is nil; a template holds at most one filter,
	// ORDER BY, pagination and VALUES row segment.
	Marks = emitkit.Marks

	// ColumnWrap is how Render wraps one output column the driver cannot read as it is, as
	// Renderer.WrapColumn returns it.
	ColumnWrap = emitkit.ColumnWrap

	// SegmentSet is a set of segment kinds, such as the clauses Renderer.Omit leaves out.
	// NewSegmentSet builds one.
	SegmentSet = emitkit.SegmentSet
)

// NewSegmentSet returns the set holding the given kinds.
//
// Takes kinds (...emit.SQLSegmentKind) which are the kinds in the set.
//
// Returns SegmentSet which is the set.
func NewSegmentSet(kinds ...emit.SQLSegmentKind) SegmentSet {
	return emitkit.NewSegmentSet(kinds...)
}

// ListMode says how Render writes the list of a slice parameter.
type ListMode = emitkit.ListMode

// ColumnWrapKind says how the generated code reads a column Render wrapped.
type ColumnWrapKind = emitkit.ColumnWrapKind

// Batching the rows of a multi-row INSERT.
type (
	// Row is the template of one row of a multi-row INSERT, read from a query's VALUES row,
	// which the generated code writes once per row it batches, after the SQL before the row
	// and before the SQL after it.
	//
	// A row writes Parts in order with a place between each two. In a numbered form a place
	// takes the number, in the statement, of the argument Slots names: the row's index times
	// the number of Arguments, plus the slot, plus one. In a named form it takes the row's
	// index, which ends the names of the row's arguments, so each row binds its own. An
	// anonymous form has no places, so its whole row is the one part, binding Arguments in
	// order.
	Row = emitkit.Row

	// BindLimit is the most variables one statement of a batch binds, which CheckBatch
	// checks a VALUES row the emitter repeats within a statement against.
	BindLimit = emitkit.BindLimit
)

// BatchShape says how an emitter batches a query: by repeating its VALUES row, or by
// sending the row's values as columns.
type BatchShape = emitkit.BatchShape

// Render writes a template as SQL for one driver, with the order its arguments bind in
// and the marks the generated code edits at.
//
// Parameters are numbered canonically: those written only as single placeholders are
// numbered from one in index order, then the slice parameters, written as lists, continue
// in index order. Only the parameters the rendered segments bind are numbered, so leaving
// out a clause renumbers the rest densely. A parameter is written as a placeholder in the
// renderer's form, a named form binding it under the dialect's NamePrefix and its name.
// An output column the renderer wraps is written inside the wrap with an alias keeping
// its name, unless the item names itself; a star covering a wrapped column is expanded
// into its columns, quoted, and so is every other star unless the renderer keeps stars or
// the template marks it AsWritten. The renderer's Prefix is written first.
//
// Takes template (emit.SQLTemplate) which is the template to write.
// Takes renderer (*Renderer) which says how to write it. Render does not change it.
//
// Returns Rendered which holds the SQL, its arguments and its marks.
// Returns error when the renderer lacks a query or a placeholder form, or the template
// does not fit the query or the renderer: an index out of range, a parameter written both
// as a list and alone, a typed form for a placeholder without a type, a list mode the
// dialect has no array form for, a wrapped column or star column without a name, a
// repeated or malformed clause segment, or a segment of an unknown kind.
func Render(template emit.SQLTemplate, renderer *Renderer) (Rendered, error) {
	return emitkit.Render(template, renderer)
}

// ExecutionForm returns the form a query's SQL executes in: the dialect's positional form
// for a runtime builder query of a dialect that binds by name, since the builder appends
// predicates it binds by position, and the dialect's Execution form otherwise.
//
// Takes info (*dialect.Facts) which describes the dialect. ExecutionForm does not change
// it.
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns dialect.PlaceholderSigil which is the form.
func ExecutionForm(info *dialect.Facts, query *emit.AnalysedQuery) dialect.PlaceholderSigil {
	return emitkit.ExecutionForm(info, query)
}

// CommentLine returns a line comment holding a text, opened with the dialect's
// CommentPrefix, which seagull.New requires, and ending in a newline: the Renderer Prefix
// that names a query whose SQL another query of the package writes alike.
//
// Takes info (*dialect.Facts) which supplies the comment syntax.
// Takes text (string) which is the comment's text, such as the query's name.
//
// Returns string which is the line.
func CommentLine(info *dialect.Facts, text string) string {
	return emitkit.CommentLine(info, text)
}

// MethodName returns a query's Queries method name before the naming plan makes it
// unique. It converts snake_case to PascalCase with common initialisms, and otherwise
// capitalises the first letter.
//
// Takes name (string) which is the query's name, as its directive writes it.
//
// Returns string which is the method name.
// Returns error when the name cannot become an exported Go identifier.
func MethodName(name string) (string, error) {
	return emitkit.MethodName(name)
}

// CheckBatch reports query shapes that cannot be emitted as the requested batch form.
//
// A batch needs one INSERT VALUES row with no parameters outside it or slice parameters
// inside it. Columnar batches also require lone placeholders. A row binding more
// variables than a limit allows could not be sent even alone, so it is refused too. Each
// problem is reported under seagull.CodeBatchShape.
//
// Takes query (*emit.AnalysedQuery) which is the query.
// Takes shape (BatchShape) which says how the emitter batches it.
// Takes limits (...BindLimit) which bound the variables one statement binds, for an
// emitter that repeats the row within a statement.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func CheckBatch(query *emit.AnalysedQuery, shape BatchShape, limits ...BindLimit) []emit.Problem {
	return emitkit.CheckBatch(query, shape, limits...)
}

// ClausesAfterValues reports whether a query has a clause after its VALUES row, ignoring
// whitespace, comments and terminators. Such clauses can change behaviour when rows share
// one statement and cannot be applied by a copy protocol.
//
// Takes query (*emit.AnalysedQuery) which is the query.
// Takes rules (*sqllex.Rules) which are the dialect's lexical rules, the Lexical field of
// its facts, whose type sdk/migrate names Rules. ClausesAfterValues does not change them.
//
// Returns bool which is true when such a clause follows the row, and false when the query
// has no VALUES row.
func ClausesAfterValues(query *emit.AnalysedQuery, rules *sqllex.Rules) bool {
	return emitkit.ClausesAfterValues(query, rules)
}

// RowColumns returns, for each item of a query's VALUES row in order, the index into
// AnalysedQuery.Parameters of the parameter the item holds, when every item is one
// placeholder alone and no parameter is written twice: the order a batch sending the
// row's values as columns appends them in.
//
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns []int which holds the parameter of each item.
// Returns bool which is false when the query has no VALUES row, or one whose items are
// not all lone placeholders of distinct parameters.
func RowColumns(query *emit.AnalysedQuery) ([]int, bool) {
	return emitkit.RowColumns(query)
}

// RowTemplate reads the row template of a query whose template holds a VALUES row,
// rendered with a renderer.
//
// Takes query (*emit.AnalysedQuery) which is the query.
// Takes renderer (*Renderer) which says how the row is written. RowTemplate does not
// change it.
//
// Returns Row which is the row template.
// Returns error when CheckBatch finds a problem with batching the query's rows, or the
// template does not render.
func RowTemplate(query *emit.AnalysedQuery, renderer *Renderer) (Row, error) {
	return emitkit.RowTemplate(query, renderer)
}

// CheckTypeDepth reports every parameter and output column of a request's queries, and
// every column of a table it models, whose SQL type nests element, key, field or member
// types to dialect.MaxTypeDepth, 64, or deeper.
//
// Every walk an emitter makes through a type stops at that depth, so code generated for
// such a type could not read or bind its innermost parts; each is reported as an
// emit.CodeEmitterUnsupported error, at its query, or against the request for a table's
// column. Each built-in emitter's Check calls it first.
//
// Takes request (*emit.Request) which holds the queries and the catalogue. CheckTypeDepth
// does not change it.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func CheckTypeDepth(request *emit.Request) []emit.Problem {
	return emitkit.CheckTypeDepth(request)
}

// DefaultReservation returns the reservation of an emitter that does not implement
// emit.ReserverPort: every name any built-in emitter's generated code uses where a
// planned name could meet it, so a plan made with it is safe for any of them.
//
// It reserves more than one emitter needs, so an emitter that knows its own names should
// implement emit.ReserverPort.
//
// Takes info (*dialect.Facts) which describes the dialect; the default reservation does
// not depend on it.
// Takes style (*emit.Style) which holds the helper and error prefixes the helper names
// start with. DefaultReservation does not change it.
//
// Returns emit.Reservation which holds the names.
func DefaultReservation(info *dialect.Facts, style *emit.Style) emit.Reservation {
	return emitkit.DefaultReservation(info, style)
}

// ReservationFor returns the names an emitter reserves: those its emit.ReserverPort
// gives, or DefaultReservation's when it does not implement one.
//
// Takes emitter (emit.EmitterPort) which is the emitter.
// Takes info (*dialect.Facts) which describes the dialect. ReservationFor does not change
// it.
// Takes style (*emit.Style) which holds the style of the generated code. ReservationFor
// does not change it.
//
// Returns emit.Reservation which holds the names.
func ReservationFor(emitter emit.EmitterPort, info *dialect.Facts, style *emit.Style) emit.Reservation {
	return emitkit.ReservationFor(emitter, info, style)
}
