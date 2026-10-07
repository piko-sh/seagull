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
	"maps"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

// parameterNumbering is the canonical numbering of the parameters a template's rendered
// segments bind.
type parameterNumbering struct {
	// numbers maps each parameter's index to its number, counting from one.
	numbers map[int]int

	// listed holds the parameters written as a slice list.
	listed map[int]bool

	// order lists the parameters in number order.
	order []int
}

// ArgumentOrder lists parameter indexes in the order a driver binds them.
//
// Named and numbered placeholders bind each parameter once. Anonymous placeholders bind
// each occurrence in SQL order. A slice entry represents all of its elements.
//
// Takes template (sqlmodel.SQLTemplate) which is the template.
// Takes form (dialect.PlaceholderSigil) which is the form it renders in.
//
// Returns []int which holds the parameter index of each argument, or nil when the
// template binds none.
func ArgumentOrder(template sqlmodel.SQLTemplate, form dialect.PlaceholderSigil) []int {
	return argumentOrder(template.Segments, 0, form)
}

// ExecutionForm returns the form a query's SQL executes in: the dialect's positional form
// for a runtime builder query of a dialect that binds by name, since the builder appends
// predicates it binds by position, and the dialect's Execution form otherwise.
//
// Takes info (*dialect.Facts) which describes the dialect. ExecutionForm does not change
// it.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns dialect.PlaceholderSigil which is the form.
func ExecutionForm(info *dialect.Facts, query *sqlmodel.AnalysedQuery) dialect.PlaceholderSigil {
	if query.DynamicRuntime && info.Placeholders.BindsByName() {
		return info.Placeholders.PositionalForm()
	}
	return info.Placeholders.Execution
}

// argumentOrder returns the argument order of segments rendered in a form, leaving out
// the omitted clause segments.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the template's segments.
// Takes omit (SegmentSet) which lists the segment kinds left out.
// Takes form (dialect.PlaceholderSigil) which is the form they render in.
//
// Returns []int which holds the parameter index of each argument, or nil when there are
// none.
func argumentOrder(segments []sqlmodel.SQLSegment, omit SegmentSet, form dialect.PlaceholderSigil) []int {
	var order []int
	if form.Kind == dialect.PlaceholderAnonymous {
		visitParameters(segments, omit, func(segment *sqlmodel.SQLSegment) {
			order = append(order, segment.Parameter)
		})
		return order
	}
	if order = numberParameters(segments, omit).order; len(order) == 0 {
		return nil
	}
	return order
}

// numberParameters numbers the parameters segments bind canonically: those written only
// alone from one in index order, then those written as a list in index order.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the template's segments.
// Takes omit (SegmentSet) which lists the segment kinds left out.
//
// Returns parameterNumbering which holds the numbering.
func numberParameters(segments []sqlmodel.SQLSegment, omit SegmentSet) parameterNumbering {
	single := make(map[int]bool)
	listed := make(map[int]bool)
	visitParameters(segments, omit, func(segment *sqlmodel.SQLSegment) {
		if segment.Kind == sqlmodel.SegmentList {
			listed[segment.Parameter] = true
			return
		}
		single[segment.Parameter] = true
	})
	maps.DeleteFunc(single, func(parameter int, _ bool) bool { return listed[parameter] })
	order := slices.Sorted(maps.Keys(single))
	order = append(order, slices.Sorted(maps.Keys(listed))...)
	numbers := make(map[int]int, len(order))
	for index, parameter := range order {
		numbers[parameter] = index + 1
	}
	return parameterNumbering{numbers: numbers, listed: listed, order: order}
}

// visitParameters calls visit for every parameter and list segment, in the order they are
// written, leaving out the children of omitted segments.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments to walk.
// Takes omit (SegmentSet) which lists the segment kinds left out.
// Takes visit (func(*sqlmodel.SQLSegment)) which is called for each one.
func visitParameters(segments []sqlmodel.SQLSegment, omit SegmentSet, visit func(segment *sqlmodel.SQLSegment)) {
	for index := range segments {
		segment := &segments[index]
		switch {
		case segment.Kind == sqlmodel.SegmentParameter || segment.Kind == sqlmodel.SegmentList:
			visit(segment)
		case !omit.Has(segment.Kind):
			visitParameters(segment.Children, omit, visit)
		}
	}
}
