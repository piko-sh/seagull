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

package emitkit

import (
	"errors"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// aliasKeyword introduces the alias a wrapped column keeps its output name with.
	aliasKeyword = " AS "

	// listOpen opens a list of placeholders.
	listOpen = "("

	// listClose closes a list of placeholders.
	listClose = ")"

	// starSeparator separates the columns an expanded star stands for.
	starSeparator = ", "

	// qualifierSeparator separates a column's qualifier from its name.
	qualifierSeparator = "."
)

const (
	// ListExpand writes a slice parameter's list as one placeholder in parentheses, which
	// the generated code splices into one placeholder per element at run time.
	ListExpand ListMode = iota

	// ListArray writes, in place of the operator and its list, one placeholder bound to the
	// whole slice as an array inside the dialect's array membership form, or its exclusion
	// form for NOT IN.
	ListArray

	// ListAsWritten writes the operator and one placeholder in parentheses, bound to the
	// whole slice as one value, for a driver that expands a slice itself.
	ListAsWritten
)

const (
	// ColumnWrapJSON wraps a column in the dialect's to-JSON function; the generated code
	// decodes the JSON into the column's Go type.
	ColumnWrapJSON ColumnWrapKind = iota

	// ColumnWrapText casts a column to the dialect's text type; the generated code scans the
	// text.
	ColumnWrapText
)

var (
	// errRender reports a template, or a renderer, that Render cannot render.
	errRender = errors.New("emitkit: cannot render the SQL template")

	// omittable is the set of segment kinds a Renderer may omit.
	omittable = NewSegmentSet(sqlmodel.SegmentOrderBy, sqlmodel.SegmentPagination)
)

// ListMode says how Render writes the list of a slice parameter.
type ListMode uint8

// ColumnWrapKind says how the generated code reads a column Render wrapped.
type ColumnWrapKind uint8

// SegmentSet is a set of segment kinds.
type SegmentSet uint32

// NewSegmentSet returns the set holding the given kinds.
//
// Takes kinds (...sqlmodel.SQLSegmentKind) which are the kinds in the set.
//
// Returns SegmentSet which is the set.
func NewSegmentSet(kinds ...sqlmodel.SQLSegmentKind) SegmentSet {
	var set SegmentSet
	for _, kind := range kinds {
		set |= 1 << kind
	}
	return set
}

// Has reports whether the set holds a kind.
//
// Takes kind (sqlmodel.SQLSegmentKind) which is the kind to look for.
//
// Returns bool which is true when the set holds the kind.
func (set SegmentSet) Has(kind sqlmodel.SQLSegmentKind) bool {
	return set&(1<<kind) != 0
}

// ColumnWrap is how Render wraps one output column the driver cannot read as it is.
type ColumnWrap struct {
	// Affix is written around the column's expression, such as "to_json(" and ")".
	Affix dialect.Affix

	// Kind says how the generated code reads the wrapped column.
	Kind ColumnWrapKind
}

// Renderer says how Render writes a template for one driver: the placeholder form, how
// slice lists are written, which output columns are wrapped and which clauses are left
// out. One renderer serves one query, whose parameters and output columns the template's
// indices refer to.
type Renderer struct {
	// WrapColumn returns how to wrap the output column at an index, or false to leave it as
	// it is. A nil WrapColumn wraps no column.
	WrapColumn func(column int) (ColumnWrap, bool)

	// Query is the query the template belongs to. Render reads the names of its parameters,
	// for a named form, and of its output columns, for the alias a wrapped column keeps.
	Query *sqlmodel.AnalysedQuery

	// Prefix is written before the SQL as it is, such as a comment line naming the query,
	// which tells apart the SQL of two queries that would otherwise be written alike. The
	// marks count it, and it binds nothing.
	Prefix string

	// Quote delimits the identifiers Render writes itself: the columns of an expanded star
	// and the alias of a wrapped column.
	Quote dialect.IdentifierQuote

	// Lists holds the dialect's array forms, which ListArray writes.
	Lists dialect.ListSyntax

	// Form is the form every placeholder is written in: the dialect's Execution form, or its
	// positional form where the generated code binds by position. ExecutionForm gives the
	// one a query executes in.
	Form dialect.PlaceholderSigil

	// Placeholders is the dialect's placeholder description, whose NamePrefix a named form
	// binds under.
	Placeholders dialect.Placeholders

	// Omit lists ORDER BY or pagination segments to suppress while retaining their insertion
	// marks. Suppressed segments contribute no binds.
	Omit SegmentSet

	// ListMode says how slice lists are written.
	ListMode ListMode

	// KeepStars preserves stars unless the renderer wraps a column. By default stars expand
	// to the generated columns to keep reads stable after migrations add columns, except
	// stars marked AsWritten by the template.
	KeepStars bool
}

// Rendered is a template written as SQL for one driver.
type Rendered struct {
	// SQL is the SQL the generated code executes, or splices at run time.
	SQL string

	// Arguments lists the arguments the driver binds, in binding order: one per parameter in
	// number order for a numbered or named form, and one per placeholder occurrence for an
	// anonymous form. It is nil when the SQL binds none.
	Arguments []Argument

	// Marks locates the parts of SQL the generated code edits at run time.
	Marks Marks
}

// Argument is one argument the driver binds.
type Argument struct {
	// Name is the name a named form binds the argument under, the dialect's NamePrefix
	// included, and is empty for a positional form.
	Name string

	// Parameter is the index into AnalysedQuery.Parameters of the parameter the argument
	// carries.
	Parameter int

	// Expand reports a slice parameter written with ListExpand, whose elements the generated
	// code binds one by one, in this argument's place.
	Expand bool
}

// Range is a half-open range of byte offsets into Rendered.SQL. A range whose Start
// equals its End is a place, where something may be inserted.
type Range struct {
	// Start is the offset of the first byte.
	Start int

	// End is the offset one past the last byte.
	End int
}

// Marks locates in Rendered.SQL the parts the generated code edits at run time. A mark
// for a segment the template does not hold is nil; a template holds at most one filter,
// ORDER BY, pagination and VALUES row segment.
type Marks struct {
	// Wrapped maps the index of each output column Render wrapped to how it is read, and is
	// nil when no column was wrapped.
	Wrapped map[int]ColumnWrapKind

	// Filter locates the filter segment.
	Filter *FilterMark

	// OrderBy covers the ORDER BY clause, or marks its place when the clause is absent or
	// omitted.
	OrderBy *Range

	// Pagination covers the LIMIT, OFFSET and FETCH clauses, or marks their place when they
	// are absent or omitted.
	Pagination *Range

	// Values covers the VALUES row, from its opening parenthesis through its closing one.
	Values *Range

	// Lists locates each slice parameter's list, in the order they are written.
	Lists []ListMark
}

// ListMark locates one slice parameter's list.
type ListMark struct {
	// Range covers what Render wrote for the list: the parenthesised placeholder for
	// ListExpand and ListAsWritten, and the whole array form for ListArray. The generated
	// code replaces it with one placeholder per element for ListExpand.
	Range Range

	// Operator marks the membership operator before a slice list so empty-list expansion can
	// replace both. Array forms use an empty range at the list's start.
	Operator Range

	// Parameter is the index into AnalysedQuery.Parameters of the slice parameter.
	Parameter int

	// First is the parameter's number in the template's numbering. Slice parameters are
	// numbered after every other parameter, so for a numbered form the first element of the
	// first list spliced takes this number.
	First int

	// Negated reports a NOT IN list, whose empty slice every row passes, rather than an IN
	// list, whose empty slice no row passes.
	Negated bool
}

// FilterMark locates the filter segment: the condition a runtime predicate joins, or the
// place a WHERE clause goes.
type FilterMark struct {
	// Condition covers the condition when Present, and otherwise marks the place where a
	// WHERE clause goes.
	Condition Range

	// InsertAt is where a predicate is added: after the condition, which a caller wraps in
	// parentheses before joining another to it, or at the place a WHERE clause goes.
	InsertAt int

	// Present reports a condition written in the query.
	Present bool
}

// renderState is the progress of one Render call.
type renderState struct {
	// renderer is the renderer the template is written with.
	renderer *Renderer

	// numbering is the template's parameter numbering.
	numbering parameterNumbering

	// marks gathers the marks written so far.
	marks Marks

	// builder receives the SQL.
	builder strings.Builder

	// marked records the clause segment kinds already written.
	marked SegmentSet
}

// writeSegments writes segments in order.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments to write.
//
// Returns error when a segment cannot be written.
func (state *renderState) writeSegments(segments []sqlmodel.SQLSegment) error {
	for index := range segments {
		if err := state.writeSegment(&segments[index]); err != nil {
			return err
		}
	}
	return nil
}

// writeSegment writes one segment as its kind says.
//
// Takes segment (*sqlmodel.SQLSegment) which is the segment to write.
//
// Returns error when the segment cannot be written.
func (state *renderState) writeSegment(segment *sqlmodel.SQLSegment) error {
	switch segment.Kind {
	case sqlmodel.SegmentText:
		return state.writeText(segment)
	case sqlmodel.SegmentParameter:
		return state.writeParameter(segment)
	case sqlmodel.SegmentList:
		return state.writeList(segment)
	case sqlmodel.SegmentColumn:
		return state.writeColumn(segment)
	case sqlmodel.SegmentStar:
		return state.writeStar(segment)
	case sqlmodel.SegmentFilter:
		return state.writeFilter(segment)
	case sqlmodel.SegmentOrderBy, sqlmodel.SegmentPagination, sqlmodel.SegmentValuesRow:
		return state.writeClause(segment)
	default:
		return fmt.Errorf("%w: a segment of kind %s cannot be rendered", errRender, segment.Kind)
	}
}

// writeText writes a text segment as it is.
//
// Takes segment (*sqlmodel.SQLSegment) which is the text segment.
//
// Returns error when the segment has children.
func (state *renderState) writeText(segment *sqlmodel.SQLSegment) error {
	if err := withoutChildren(segment); err != nil {
		return err
	}
	state.builder.WriteString(segment.Text)
	return nil
}

// writeParameter writes one placeholder.
//
// Takes segment (*sqlmodel.SQLSegment) which is the parameter segment.
//
// Returns error when the placeholder cannot be written, or the parameter is also written
// as a list.
func (state *renderState) writeParameter(segment *sqlmodel.SQLSegment) error {
	if err := withoutChildren(segment); err != nil {
		return err
	}
	if state.numbering.listed[segment.Parameter] {
		return fmt.Errorf("%w: parameter %d is written both as a list and alone", errRender, segment.Parameter)
	}
	placeholder, err := state.placeholder(segment)
	if err != nil {
		return err
	}
	state.builder.WriteString(placeholder)
	return nil
}

// writeList writes a slice parameter's list as the renderer's list mode says, and marks
// it.
//
// Takes segment (*sqlmodel.SQLSegment) which is the list segment.
//
// Returns error when the placeholder cannot be written or the dialect has no array form
// for ListArray.
func (state *renderState) writeList(segment *sqlmodel.SQLSegment) error {
	if err := withoutChildren(segment); err != nil {
		return err
	}
	placeholder, err := state.placeholder(segment)
	if err != nil {
		return err
	}
	affix := dialect.Affix{Before: listOpen, After: listClose}
	operatorStart := state.builder.Len()
	if state.renderer.ListMode == ListArray {
		if affix, err = state.arrayForm(segment.Negated); err != nil {
			return err
		}
	} else {
		state.builder.WriteString(segment.Text)
	}
	start := state.builder.Len()
	state.builder.WriteString(affix.Before + placeholder + affix.After)
	state.marks.Lists = append(state.marks.Lists, ListMark{
		Range:     Range{Start: start, End: state.builder.Len()},
		Operator:  Range{Start: operatorStart, End: start},
		Parameter: segment.Parameter,
		First:     state.numbering.numbers[segment.Parameter],
		Negated:   segment.Negated,
	})
	return nil
}

// writeColumn writes an output column, inside its wrap when the renderer wraps it.
//
// Takes segment (*sqlmodel.SQLSegment) which is the column segment.
//
// Returns error when the column is out of range or its expression cannot be written.
func (state *renderState) writeColumn(segment *sqlmodel.SQLSegment) error {
	column, err := state.outputColumn(segment.Column)
	if err != nil {
		return err
	}
	wrap, wraps := state.wrapFor(segment.Column)
	if !wraps {
		return state.writeSegments(segment.Children)
	}
	alias := ""
	if !segment.Aliased {
		if alias, err = state.alias(segment.Column, column); err != nil {
			return err
		}
	}
	state.builder.WriteString(wrap.Affix.Before)
	if err := state.writeSegments(segment.Children); err != nil {
		return err
	}
	state.builder.WriteString(wrap.Affix.After + alias)
	state.markWrapped(segment.Column, wrap.Kind)
	return nil
}

// writeStar writes a star expanded into its columns when the renderer wraps one of them,
// or, unless the renderer keeps stars, when the template can name every column in its
// place; and otherwise as it is written.
//
// Takes segment (*sqlmodel.SQLSegment) which is the star segment.
//
// Returns error when a column of a star the renderer wraps is out of range or has no
// name.
func (state *renderState) writeStar(segment *sqlmodel.SQLSegment) error {
	if err := withoutChildren(segment); err != nil {
		return err
	}
	if !state.wrapsAny(segment.Star) && !state.expands(segment) {
		state.builder.WriteString(segment.Text)
		return nil
	}
	for index := range segment.Star {
		if index > 0 {
			state.builder.WriteString(starSeparator)
		}
		if err := state.writeStarColumn(&segment.Star[index]); err != nil {
			return err
		}
	}
	return nil
}

// expands reports whether a star the renderer wraps none of the columns of is expanded:
// unless the renderer keeps stars or the template marks it AsWritten, when it stands for
// at least one column and every column has a name.
//
// Takes segment (*sqlmodel.SQLSegment) which is the star segment.
//
// Returns bool which is true when the star is expanded.
func (state *renderState) expands(segment *sqlmodel.SQLSegment) bool {
	if state.renderer.KeepStars || segment.AsWritten || len(segment.Star) == 0 {
		return false
	}
	for index := range segment.Star {
		if segment.Star[index].Name == "" {
			return false
		}
	}
	return true
}

// writeStarColumn writes one column of an expanded star, as a quoted reference, inside
// its wrap with an alias keeping its output name when the renderer wraps it.
//
// Takes star (*sqlmodel.StarColumn) which is the column.
//
// Returns error when the column is out of range or has no name.
func (state *renderState) writeStarColumn(star *sqlmodel.StarColumn) error {
	column, err := state.outputColumn(star.Column)
	if err != nil {
		return err
	}
	if star.Name == "" {
		return fmt.Errorf("%w: the star column for output column %d has no name to expand to", errRender, star.Column)
	}
	quote := state.renderer.Quote
	reference := quote.Quote(star.Name)
	if star.Qualifier != "" {
		reference = quote.Quote(star.Qualifier) + qualifierSeparator + reference
	}
	wrap, wraps := state.wrapFor(star.Column)
	if !wraps {
		state.builder.WriteString(reference)
		return nil
	}
	alias, err := state.alias(star.Column, column)
	if err != nil {
		return err
	}
	state.builder.WriteString(wrap.Affix.Before + reference + wrap.Affix.After + alias)
	state.markWrapped(star.Column, wrap.Kind)
	return nil
}

// alias returns the alias a wrapped output column keeps its name with.
//
// Takes index (int) which is the column's index into AnalysedQuery.OutputColumns.
// Takes column (*sqlmodel.OutputColumn) which is the column.
//
// Returns string which is the alias, its keyword included.
// Returns error when the column has no name to keep.
func (state *renderState) alias(index int, column *sqlmodel.OutputColumn) (string, error) {
	if column.Name == "" {
		return "", fmt.Errorf("%w: output column %d is wrapped but has no name to keep", errRender, index)
	}
	return aliasKeyword + state.renderer.Quote.Quote(column.Name), nil
}

// writeFilter writes the filter segment's condition and marks it.
//
// Takes segment (*sqlmodel.SQLSegment) which is the filter segment.
//
// Returns error when the template holds another filter segment, an absent filter has
// children, or the condition cannot be written.
func (state *renderState) writeFilter(segment *sqlmodel.SQLSegment) error {
	if err := state.markOnce(segment); err != nil {
		return err
	}
	start := state.builder.Len()
	if err := state.writeSegments(segment.Children); err != nil {
		return err
	}
	end := state.builder.Len()
	state.marks.Filter = &FilterMark{Condition: Range{Start: start, End: end}, InsertAt: end, Present: segment.Present}
	return nil
}

// writeClause writes an ORDER BY, pagination or VALUES row segment, or nothing when the
// renderer omits it, and marks it.
//
// Takes segment (*sqlmodel.SQLSegment) which is the clause segment.
//
// Returns error when the template holds another segment of the kind, an absent clause has
// children, or the clause cannot be written.
func (state *renderState) writeClause(segment *sqlmodel.SQLSegment) error {
	if err := state.markOnce(segment); err != nil {
		return err
	}
	start := state.builder.Len()
	if !state.renderer.Omit.Has(segment.Kind) {
		if err := state.writeSegments(segment.Children); err != nil {
			return err
		}
	}
	clause := &Range{Start: start, End: state.builder.Len()}
	switch segment.Kind {
	case sqlmodel.SegmentOrderBy:
		state.marks.OrderBy = clause
	case sqlmodel.SegmentPagination:
		state.marks.Pagination = clause
	default:
		state.marks.Values = clause
	}
	return nil
}

// markOnce records that a clause segment is written, refusing a second of its kind and an
// absent clause with children.
//
// Takes segment (*sqlmodel.SQLSegment) which is the clause segment.
//
// Returns error when the segment cannot be marked.
func (state *renderState) markOnce(segment *sqlmodel.SQLSegment) error {
	if state.marked.Has(segment.Kind) {
		return fmt.Errorf("%w: the template holds more than one %s segment", errRender, segment.Kind)
	}
	state.marked |= NewSegmentSet(segment.Kind)
	if !segment.Present && segment.Kind != sqlmodel.SegmentValuesRow && len(segment.Children) > 0 {
		return fmt.Errorf("%w: an absent %s segment has children", errRender, segment.Kind)
	}
	return nil
}

// placeholder returns one placeholder of a parameter or list segment in the renderer's
// form.
//
// Takes segment (*sqlmodel.SQLSegment) which is the parameter or list segment.
//
// Returns string which is the placeholder.
// Returns error when the parameter is out of range, a named form has no name to bind it
// by, or a typed form has no type to write.
func (state *renderState) placeholder(segment *sqlmodel.SQLSegment) (string, error) {
	parameters := state.renderer.Query.Parameters
	if segment.Parameter < 0 || segment.Parameter >= len(parameters) {
		return "", fmt.Errorf("%w: parameter %d is out of range; the query has %d", errRender, segment.Parameter, len(parameters))
	}
	form := state.renderer.Form
	if form.Kind != dialect.PlaceholderNamed {
		return form.Render(state.numbering.numbers[segment.Parameter], "", segment.TypeText), nil
	}
	name := parameters[segment.Parameter].Name
	if name == "" {
		return "", fmt.Errorf("%w: parameter %d has no name to bind it by", errRender, segment.Parameter)
	}
	if form.TypeSeparator != "" && segment.TypeText == "" {
		return "", fmt.Errorf("%w: parameter %q is written in a typed form but its placeholder carries no type", errRender, name)
	}
	return form.Render(0, state.renderer.Placeholders.BoundName(name), segment.TypeText), nil
}

// arrayForm returns the dialect's array form for a list.
//
// Takes negated (bool) which is true for NOT IN.
//
// Returns dialect.Affix which is written around the placeholder.
// Returns error when the dialect has no such form.
func (state *renderState) arrayForm(negated bool) (dialect.Affix, error) {
	affix := state.renderer.Lists.ArrayMembership
	if negated {
		affix = state.renderer.Lists.ArrayExclusion
	}
	if affix.Before == "" && affix.After == "" {
		operator := "IN"
		if negated {
			operator = "NOT IN"
		}
		return affix, fmt.Errorf("%w: the dialect has no array form to write %s with", errRender, operator)
	}
	return affix, nil
}

// outputColumn returns the query's output column at an index.
//
// Takes index (int) which is the index into AnalysedQuery.OutputColumns.
//
// Returns *sqlmodel.OutputColumn which is the column.
// Returns error when the index is out of range.
func (state *renderState) outputColumn(index int) (*sqlmodel.OutputColumn, error) {
	columns := state.renderer.Query.OutputColumns
	if index < 0 || index >= len(columns) {
		return nil, fmt.Errorf("%w: output column %d is out of range; the query has %d", errRender, index, len(columns))
	}
	return &columns[index], nil
}

// wrapFor returns how the renderer wraps an output column.
//
// Takes column (int) which is the index into AnalysedQuery.OutputColumns.
//
// Returns ColumnWrap which is the wrap.
// Returns bool which is false when the column is not wrapped.
func (state *renderState) wrapFor(column int) (ColumnWrap, bool) {
	if state.renderer.WrapColumn == nil {
		return ColumnWrap{}, false
	}
	return state.renderer.WrapColumn(column)
}

// wrapsAny reports whether the renderer wraps any column a star stands for.
//
// Takes columns ([]sqlmodel.StarColumn) which are the star's columns.
//
// Returns bool which is true when one of them is wrapped.
func (state *renderState) wrapsAny(columns []sqlmodel.StarColumn) bool {
	for index := range columns {
		if _, wraps := state.wrapFor(columns[index].Column); wraps {
			return true
		}
	}
	return false
}

// markWrapped records an output column as wrapped.
//
// Takes column (int) which is the index into AnalysedQuery.OutputColumns.
// Takes kind (ColumnWrapKind) which says how the column is read.
func (state *renderState) markWrapped(column int, kind ColumnWrapKind) {
	if state.marks.Wrapped == nil {
		state.marks.Wrapped = make(map[int]ColumnWrapKind)
	}
	state.marks.Wrapped[column] = kind
}

// arguments returns the arguments of the rendered SQL, in binding order.
//
// Takes order ([]int) which holds the parameter index of each argument, in binding order.
//
// Returns []Argument which holds the arguments, or nil when there are none.
func (state *renderState) arguments(order []int) []Argument {
	if len(order) == 0 {
		return nil
	}
	named := state.renderer.Form.Kind == dialect.PlaceholderNamed
	arguments := make([]Argument, len(order))
	for index, parameter := range order {
		arguments[index] = Argument{
			Name:      "",
			Parameter: parameter,
			Expand:    state.numbering.listed[parameter] && state.renderer.ListMode == ListExpand,
		}
		if named {
			arguments[index].Name = state.renderer.Placeholders.BoundName(state.renderer.Query.Parameters[parameter].Name)
		}
	}
	return arguments
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
	return info.CommentPrefix + " " + text + "\n"
}

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
// Takes template (sqlmodel.SQLTemplate) which is the template to write.
// Takes renderer (*Renderer) which says how to write it. Render does not change it.
//
// Returns Rendered which holds the SQL, its arguments and its marks.
// Returns error when the renderer lacks a query or a placeholder form, or the template
// does not fit the query or the renderer: an index out of range, a parameter written both
// as a list and alone, a typed form for a placeholder without a type, a list mode the
// dialect has no array form for, a wrapped column or star column without a name, a
// repeated or malformed clause segment, or a segment of an unknown kind.
func Render(template sqlmodel.SQLTemplate, renderer *Renderer) (Rendered, error) {
	if err := validateRenderer(renderer); err != nil {
		return Rendered{}, err
	}
	state := &renderState{
		renderer:  renderer,
		numbering: numberParameters(template.Segments, renderer.Omit),
		marks:     Marks{},
		builder:   strings.Builder{},
		marked:    0,
	}
	state.builder.WriteString(renderer.Prefix)
	if err := state.writeSegments(template.Segments); err != nil {
		return Rendered{}, err
	}
	return Rendered{
		SQL:       state.builder.String(),
		Arguments: state.arguments(argumentOrder(template.Segments, renderer.Omit, renderer.Form)),
		Marks:     state.marks,
	}, nil
}

// validateRenderer reports a renderer Render cannot write with.
//
// Takes renderer (*Renderer) which is the renderer to check.
//
// Returns error when the renderer is nil, names no query, has no placeholder form, has an
// unknown list mode or omits a segment kind that cannot be omitted.
func validateRenderer(renderer *Renderer) error {
	switch {
	case renderer == nil:
		return fmt.Errorf("%w: there is no renderer", errRender)
	case renderer.Query == nil:
		return fmt.Errorf("%w: the renderer names no query", errRender)
	case renderer.Form.Prefix == "":
		return fmt.Errorf("%w: the renderer has no placeholder form", errRender)
	case renderer.ListMode > ListAsWritten:
		return fmt.Errorf("%w: list mode %d is unknown", errRender, renderer.ListMode)
	case renderer.Omit&^omittable != 0:
		return fmt.Errorf("%w: only ORDER BY and pagination segments can be omitted", errRender)
	default:
		return nil
	}
}

// withoutChildren refuses a segment of a kind that holds no children but has some.
//
// Takes segment (*sqlmodel.SQLSegment) which is the segment.
//
// Returns error when the segment has children.
func withoutChildren(segment *sqlmodel.SQLSegment) error {
	if len(segment.Children) > 0 {
		return fmt.Errorf("%w: a %s segment cannot hold children", errRender, segment.Kind)
	}
	return nil
}
