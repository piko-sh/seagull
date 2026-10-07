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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// rowNameSeparator ends the name a named form binds an argument under before the row's
	// number, so the argument id of the fourth row binds as id_3.
	rowNameSeparator = "_"
)

var (
	// errRowTemplate reports a query whose VALUES row cannot be read as a row template.
	errRowTemplate = errors.New("emitkit: cannot read the VALUES row as a row template")
)

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
type Row struct {
	// Parts holds the row's text around its places, one more than Slots, with each
	// placeholder's prefix, name and type written around its place.
	Parts []string

	// Slots holds, for each place, the index into Arguments of the argument written there,
	// so a placeholder written twice reuses its argument.
	Slots []int

	// Arguments holds, in the order a row binds them, the index into
	// AnalysedQuery.Parameters of each argument: each parameter once, in the template's
	// canonical number order, for a numbered or named form, and one argument per
	// placeholder, in the order they are written, for an anonymous form.
	Arguments []int

	// Names holds, parallel to Arguments for a named form, the name each argument binds
	// under before the row's index, such as p_id_; it is nil for any other form.
	Names []string

	// Values covers the VALUES row in the SQL Render writes for the query with the same
	// renderer: the generated code keeps the SQL before and after it, which binds no
	// parameter, and writes its rows in its place.
	Values Range

	// Kind is the kind of the renderer's placeholder form, which says what a place takes.
	Kind dialect.PlaceholderKind
}

// rowBuilder gathers a row template from the segments of a VALUES row.
type rowBuilder struct {
	// renderer is the renderer the row is written with.
	renderer *Renderer

	// part holds the text of the part being written.
	part strings.Builder

	// row is the row template so far.
	row Row
}

// write adds segments of the VALUES row to the template.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments.
//
// Returns error when a segment is neither text nor a placeholder.
func (builder *rowBuilder) write(segments []sqlmodel.SQLSegment) error {
	for index := range segments {
		segment := &segments[index]
		switch segment.Kind {
		case sqlmodel.SegmentText:
			builder.part.WriteString(segment.Text)
		case sqlmodel.SegmentParameter:
			if err := builder.placeholder(segment); err != nil {
				return err
			}
		default:
			return fmt.Errorf("a %s segment cannot be repeated in a VALUES row", segment.Kind)
		}
	}
	return nil
}

// placeholder adds one placeholder to the template: a place for a numbered or named form,
// and the placeholder itself with an argument of its own for an anonymous one.
//
// Takes segment (*sqlmodel.SQLSegment) which is the parameter segment.
//
// Returns error when the parameter is out of range or a named form has no name or type
// for it.
func (builder *rowBuilder) placeholder(segment *sqlmodel.SQLSegment) error {
	parameters := builder.renderer.Query.Parameters
	if segment.Parameter < 0 || segment.Parameter >= len(parameters) {
		return fmt.Errorf("parameter %d is out of range; the query has %d", segment.Parameter, len(parameters))
	}
	form := builder.renderer.Form
	switch form.Kind {
	case dialect.PlaceholderAnonymous:
		builder.part.WriteString(form.Render(0, "", ""))
		builder.row.Arguments = append(builder.row.Arguments, segment.Parameter)
		return nil
	case dialect.PlaceholderNamed:
		return builder.namedPlace(segment, &parameters[segment.Parameter])
	default:
		builder.place(form.Prefix, segment.Parameter, form.Suffix)
		return nil
	}
}

// namedPlace adds the place of a named placeholder, whose name ends with the row's index.
//
// Takes segment (*sqlmodel.SQLSegment) which is the parameter segment.
// Takes parameter (*sqlmodel.QueryParameter) which is its parameter.
//
// Returns error when the parameter has no name, or a typed form no type.
func (builder *rowBuilder) namedPlace(segment *sqlmodel.SQLSegment, parameter *sqlmodel.QueryParameter) error {
	form := builder.renderer.Form
	if parameter.Name == "" {
		return fmt.Errorf("parameter %d has no name to bind it by", segment.Parameter)
	}
	after := form.Suffix
	if form.TypeSeparator != "" {
		if segment.TypeText == "" {
			return fmt.Errorf("parameter %q is written in a typed form but its placeholder carries no type", parameter.Name)
		}
		after = form.TypeSeparator + segment.TypeText + form.Suffix
	}
	name := builder.renderer.Placeholders.BoundName(parameter.Name) + rowNameSeparator
	if builder.row.Names == nil {
		builder.row.Names = make([]string, len(builder.row.Arguments))
	}
	builder.row.Names[slices.Index(builder.row.Arguments, segment.Parameter)] = name
	builder.place(form.Prefix+name, segment.Parameter, after)
	return nil
}

// place ends the part being written with the text before a place, records the place's
// argument, and starts the next part with the text after it.
//
// Takes before (string) which is written before the place.
// Takes parameter (int) which is the index into AnalysedQuery.Parameters of the argument
// written there.
// Takes after (string) which starts the next part.
func (builder *rowBuilder) place(before string, parameter int, after string) {
	builder.part.WriteString(before)
	builder.row.Parts = append(builder.row.Parts, builder.part.String())
	builder.row.Slots = append(builder.row.Slots, slices.Index(builder.row.Arguments, parameter))
	builder.part.Reset()
	builder.part.WriteString(after)
}

// RowTemplate reads the row template of a query whose template holds a VALUES row,
// rendered with a renderer.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes renderer (*Renderer) which says how the row is written. RowTemplate does not
// change it.
//
// Returns Row which is the row template.
// Returns error when CheckBatch finds a problem with batching the query's rows, or the
// template does not render.
func RowTemplate(query *sqlmodel.AnalysedQuery, renderer *Renderer) (Row, error) {
	if problems := CheckBatch(query, BatchRows); len(problems) > 0 {
		messages := make([]string, len(problems))
		for index := range problems {
			messages[index] = problems[index].Message
		}
		return Row{}, fmt.Errorf("%w: query %s: %s", errRowTemplate, query.Name, strings.Join(messages, "; "))
	}
	rendered, err := Render(query.Template, renderer)
	if err != nil {
		return Row{}, err
	}
	if rendered.Marks.Values == nil {
		return Row{}, fmt.Errorf("%w: query %s has no VALUES row to repeat", errRowTemplate, query.Name)
	}
	builder := rowBuilder{
		renderer: renderer,
		row:      Row{Parts: nil, Slots: nil, Arguments: nil, Names: nil, Values: *rendered.Marks.Values, Kind: renderer.Form.Kind},
		part:     strings.Builder{},
	}
	if builder.row.Kind != dialect.PlaceholderAnonymous {
		builder.row.Arguments = slices.Clone(numberParameters(query.Template.Segments, 0).order)
	}
	if err := builder.write(valuesRow(query.Template.Segments).Children); err != nil {
		return Row{}, fmt.Errorf("%w: query %s: %w", errRowTemplate, query.Name, err)
	}
	builder.row.Parts = append(builder.row.Parts, builder.part.String())
	return builder.row, nil
}
