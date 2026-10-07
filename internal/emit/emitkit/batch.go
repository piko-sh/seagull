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
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// rowPunctuation is the text a VALUES row of lone placeholders holds besides them.
	rowPunctuation = "(), \t\r\n"
)

// BatchShape says how an emitter batches a query: by repeating its VALUES row, or by
// sending the row's values as columns.
type BatchShape uint8

const (
	// BatchRows repeats a query's VALUES row once per row of a multi-row INSERT, as the row
	// is written, expressions and literals included.
	BatchRows BatchShape = iota

	// BatchColumns sends each row's values as data, column by column, as a copy protocol or
	// a column-oriented batch does, so every item of the VALUES row must be one placeholder
	// alone, whose position names the column it fills.
	BatchColumns
)

// BindLimit is the most variables one statement of a batch binds, which CheckBatch checks
// a VALUES row the emitter repeats within a statement against.
type BindLimit struct {
	// Form is the form the row's placeholders are written in: a numbered or named form binds
	// a parameter written twice once, and an anonymous form binds each placeholder.
	Form dialect.PlaceholderSigil

	// Limit is the most variables one statement binds; zero or less checks nothing.
	Limit int
}

// afterValues is what a template holds after its VALUES row.
type afterValues struct {
	// text is the text after the row.
	text string

	// segments reports a segment after the row that is not text.
	segments bool
}

// CheckBatch reports query shapes that cannot be emitted as the requested batch form.
//
// A batch needs one INSERT VALUES row with no parameters outside it or slice parameters
// inside it. Columnar batches also require lone placeholders. A row binding more
// variables than a limit allows could not be sent even alone, so it is refused too.
// Failures use CodeBatchShape.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes shape (BatchShape) which says how the emitter batches it.
// Takes limits (...BindLimit) which bound the variables one statement binds, for an
// emitter that repeats the row within a statement.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func CheckBatch(query *sqlmodel.AnalysedQuery, shape BatchShape, limits ...BindLimit) []emit.Problem {
	row := valuesRow(query.Template.Segments)
	if row == nil {
		return []emit.Problem{batchProblem(query, fmt.Sprintf(
			"command: %s repeats the statement's VALUES row for each row, so it needs an INSERT with exactly one VALUES row", query.Command))}
	}
	var problems []emit.Problem
	for _, outside := range parametersOutside(query.Template.Segments, row) {
		problems = append(problems, batchProblem(query, fmt.Sprintf(
			"command: %s binds parameter %s outside its VALUES row, which a statement of many rows would bind once for all "+
				"of them; refer to the inserted value through the engine's own form, such as EXCLUDED.column or VALUES(column)",
			query.Command, parameterName(query, outside))))
	}
	for index := range row.Children {
		if row.Children[index].Kind == sqlmodel.SegmentList {
			problems = append(problems, batchProblem(query, fmt.Sprintf(
				"command: %s writes the slice parameter %s in its VALUES row, which takes one value per column",
				query.Command, parameterName(query, row.Children[index].Parameter))))
		}
	}
	if shape == BatchColumns && len(problems) == 0 {
		if _, ok := RowColumns(query); !ok {
			problems = append(problems, batchProblem(query, fmt.Sprintf(
				"command: %s sends each row's values as columns, so every item of its VALUES row must be a placeholder alone, "+
					"each parameter written once", query.Command)))
		}
	}
	if len(problems) == 0 {
		problems = append(problems, rowBindProblems(query, row, limits)...)
	}
	return problems
}

// ClausesAfterValues reports whether a query has a clause after its VALUES row, ignoring
// whitespace, comments and terminators. Such clauses can change behaviour when rows share
// one statement and cannot be applied by a copy protocol.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes rules (*sqllex.Rules) which are the dialect's lexical rules, which say what a
// comment and the terminator are. ClausesAfterValues does not change them.
//
// Returns bool which is true when such a clause follows the row, and false when the query
// has no VALUES row.
func ClausesAfterValues(query *sqlmodel.AnalysedQuery, rules *sqllex.Rules) bool {
	after, found := textAfterValues(query.Template.Segments, false)
	if !found {
		return false
	}
	terminator := rules.Terminator
	if terminator == "" {
		terminator = sqllex.DefaultTerminator
	}
	for _, token := range sqllex.SignificantTokens(*rules, after.text) {
		if token.Raw != terminator {
			return true
		}
	}
	return after.segments
}

// RowColumns returns, for each item of a query's VALUES row in order, the index into
// AnalysedQuery.Parameters of the parameter the item holds, when every item is one
// placeholder alone and no parameter is written twice: the order a batch sending the
// row's values as columns appends them in.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []int which holds the parameter of each item.
// Returns bool which is false when the query has no VALUES row, or one whose items are
// not all lone placeholders of distinct parameters.
func RowColumns(query *sqlmodel.AnalysedQuery) ([]int, bool) {
	row := valuesRow(query.Template.Segments)
	if row == nil {
		return nil, false
	}
	var columns []int
	seen := make(map[int]bool)
	for index := range row.Children {
		child := &row.Children[index]
		even := index%2 == 0
		switch {
		case even && !rowSeparator(child, index == 0, index == len(row.Children)-1):
			return nil, false
		case !even && (child.Kind != sqlmodel.SegmentParameter || seen[child.Parameter]):
			return nil, false
		case !even:
			seen[child.Parameter] = true
			columns = append(columns, child.Parameter)
		}
	}
	return columns, len(row.Children)%2 == 1
}

// textAfterValues gathers what a template holds after its VALUES row, in order.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments to walk.
// Takes passed (bool) which is true once the walk has passed the row.
//
// Returns afterValues which holds what follows the row.
// Returns bool which is true once the row has been passed.
func textAfterValues(segments []sqlmodel.SQLSegment, passed bool) (afterValues, bool) {
	var after afterValues
	for index := range segments {
		segment := &segments[index]
		switch {
		case !passed && segment.Kind == sqlmodel.SegmentValuesRow:
			passed = true
		case !passed:
			nested, nestedPassed := textAfterValues(segment.Children, false)
			if nestedPassed {
				passed = true
				after = nested
			}
		case segment.Kind == sqlmodel.SegmentText:
			after.text += segment.Text
		default:
			after.segments = true
		}
	}
	return after, passed
}

// rowSeparator reports whether a segment of a VALUES row is the text before, between or
// after its lone placeholders: the opening parenthesis, a comma or the closing one, with
// any space around it.
//
// Takes segment (*sqlmodel.SQLSegment) which is the segment.
// Takes first (bool) which is true for the text opening the row.
// Takes last (bool) which is true for the text closing it.
//
// Returns bool which is true when the segment is only that punctuation.
func rowSeparator(segment *sqlmodel.SQLSegment, first, last bool) bool {
	if segment.Kind != sqlmodel.SegmentText || strings.Trim(segment.Text, rowPunctuation) != "" {
		return false
	}
	text := strings.TrimSpace(segment.Text)
	switch {
	case first:
		return text == "("
	case last:
		return text == ")"
	default:
		return text == ","
	}
}

// valuesRow returns a template's VALUES row segment, or nil when it has none.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the template's segments.
//
// Returns *sqlmodel.SQLSegment which is the VALUES row.
func valuesRow(segments []sqlmodel.SQLSegment) *sqlmodel.SQLSegment {
	for index := range segments {
		segment := &segments[index]
		if segment.Kind == sqlmodel.SegmentValuesRow {
			return segment
		}
		if found := valuesRow(segment.Children); found != nil {
			return found
		}
	}
	return nil
}

// parametersOutside lists the parameters a template binds outside its VALUES row, each
// once, in the order they are first written.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the template's segments.
// Takes row (*sqlmodel.SQLSegment) which is the VALUES row, which is left out.
//
// Returns []int which holds the parameters' indices.
func parametersOutside(segments []sqlmodel.SQLSegment, row *sqlmodel.SQLSegment) []int {
	var outside []int
	var walk func(segments []sqlmodel.SQLSegment)
	walk = func(segments []sqlmodel.SQLSegment) {
		for index := range segments {
			segment := &segments[index]
			switch {
			case segment == row:
			case segment.Kind == sqlmodel.SegmentParameter || segment.Kind == sqlmodel.SegmentList:
				if !slices.Contains(outside, segment.Parameter) {
					outside = append(outside, segment.Parameter)
				}
			default:
				walk(segment.Children)
			}
		}
	}
	walk(segments)
	return outside
}

// parameterName returns the name of a query's parameter, or its position when it has
// none.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes parameter (int) which is the index into AnalysedQuery.Parameters.
//
// Returns string which names the parameter.
func parameterName(query *sqlmodel.AnalysedQuery, parameter int) string {
	if parameter >= 0 && parameter < len(query.Parameters) && query.Parameters[parameter].Name != "" {
		return query.Parameters[parameter].Name
	}
	return fmt.Sprintf("%d", parameter+1)
}

// rowBindProblems reports a VALUES row binding more variables than one statement of the
// batch binds, which the generated method could never send.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes row (*sqlmodel.SQLSegment) which is its VALUES row.
// Takes limits ([]BindLimit) which bound the variables one statement binds.
//
// Returns []emit.Problem which holds a problem for each limit the row exceeds.
func rowBindProblems(query *sqlmodel.AnalysedQuery, row *sqlmodel.SQLSegment, limits []BindLimit) []emit.Problem {
	var problems []emit.Problem
	for _, limit := range limits {
		if limit.Limit <= 0 {
			continue
		}
		binds := len(argumentOrder(row.Children, 0, limit.Form))
		if binds <= limit.Limit {
			continue
		}
		problems = append(problems, batchProblem(query, fmt.Sprintf(
			"command: %s binds %d variables in each VALUES row, more than the %d one statement of the driver binds, "+
				"so not even one row could be sent; insert fewer columns, or write the values every row shares into the SQL",
			query.Command, binds, limit.Limit)))
	}
	return problems
}

// batchProblem returns a CodeBatchShape error at a query.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes message (string) which describes the problem.
//
// Returns emit.Problem which is the problem.
func batchProblem(query *sqlmodel.AnalysedQuery, message string) emit.Problem {
	return emit.Problem{Query: query, Code: diag.CodeBatchShape, Message: message, Severity: diag.SeverityError}
}
