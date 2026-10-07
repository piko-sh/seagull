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

package dialectkit

import (
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
)

// SpanField names one clause span of ast.StatementSpans.
type SpanField uint8

const (
	// SpanQuantifier is StatementSpans.Quantifier.
	SpanQuantifier SpanField = iota + 1

	// SpanProjection is StatementSpans.Projection.
	SpanProjection

	// SpanFrom is StatementSpans.From.
	SpanFrom

	// SpanWhere is StatementSpans.Where.
	SpanWhere

	// SpanWhereCondition is StatementSpans.WhereCondition.
	SpanWhereCondition

	// SpanGroupBy is StatementSpans.GroupBy.
	SpanGroupBy

	// SpanHaving is StatementSpans.Having.
	SpanHaving

	// SpanOrderBy is StatementSpans.OrderBy.
	SpanOrderBy

	// SpanPagination is StatementSpans.Pagination.
	SpanPagination

	// SpanLocking is StatementSpans.Locking.
	SpanLocking

	// SpanTrailing is StatementSpans.Trailing.
	SpanTrailing

	// SpanReturning is StatementSpans.Returning.
	SpanReturning
)

// SpanRecorder collects spans belonging to the top-level statement.
//
// Nested parsers use a nil recorder, whose methods do nothing, so their clauses cannot
// overwrite the outer statement's spans.
type SpanRecorder struct {
	// spans holds what has been recorded.
	spans ast.StatementSpans

	// start is the offset of the statement's first byte.
	start int
}

// NewSpanRecorder returns a recorder for the statement starting at an offset.
//
// Takes statementStart (int) which is the offset of the statement's first byte in the
// text its tokens were read from.
//
// Returns *SpanRecorder which records nothing yet.
func NewSpanRecorder(statementStart int) *SpanRecorder {
	return &SpanRecorder{spans: ast.StatementSpans{}, start: statementStart}
}

// Span returns the span between two offsets of the text, from the statement's first byte.
//
// Takes start (int) which is the offset of the first byte.
// Takes end (int) which is the offset one past the last byte.
//
// Returns ast.Span which is the span, or zero when the range is empty or the recorder is
// nil.
func (recorder *SpanRecorder) Span(start, end int) ast.Span {
	if recorder == nil || end <= start {
		return ast.Span{}
	}
	return ast.Span{Start: start - recorder.start, End: end - recorder.start}
}

// Record records a clause into a field the statement has not filled yet, so the first
// clause of its kind, the top-level one, wins.
//
// Takes field (SpanField) which names the field.
// Takes start (int) which is the offset of the clause's first byte.
// Takes end (int) which is the offset one past its last byte.
func (recorder *SpanRecorder) Record(field SpanField, start, end int) {
	if recorder == nil {
		return
	}
	target := recorder.field(field)
	if target == nil || target.Known() {
		return
	}
	*target = recorder.Span(start, end)
}

// Recorded reports whether a field has been filled.
//
// Takes field (SpanField) which names the field.
//
// Returns bool which is true once the field holds a span.
func (recorder *SpanRecorder) Recorded(field SpanField) bool {
	if recorder == nil {
		return false
	}
	target := recorder.field(field)
	return target != nil && target.Known()
}

// Extend widens a field to cover a further part of its clause, for a clause written in
// several parts, such as LIMIT and OFFSET.
//
// Takes field (SpanField) which names the field.
// Takes start (int) which is the offset of the part's first byte.
// Takes end (int) which is the offset one past its last byte.
func (recorder *SpanRecorder) Extend(field SpanField, start, end int) {
	if recorder == nil || end <= start {
		return
	}
	target := recorder.field(field)
	if target == nil {
		return
	}
	part := recorder.Span(start, end)
	if !target.Known() {
		*target = part
		return
	}
	target.Start = min(target.Start, part.Start)
	target.End = max(target.End, part.End)
}

// AddRowShaping records a clause that shapes the rows after HAVING, such as QUALIFY.
//
// Takes start (int) which is the offset of the clause's first byte.
// Takes end (int) which is the offset one past its last byte.
func (recorder *SpanRecorder) AddRowShaping(start, end int) {
	if recorder == nil || end <= start {
		return
	}
	recorder.spans.RowShaping = append(recorder.spans.RowShaping, recorder.Span(start, end))
}

// RecordOrderByPlace records where the statement's ORDER BY clause starts, or would
// start: the end of the last token before it.
//
// A later call replaces an earlier one, so a parser that records the place for each
// branch of a compound query leaves the place after the last branch, where the compound's
// ORDER BY goes.
//
// Takes offset (int) which is the offset one past the last byte of the token before the
// place.
func (recorder *SpanRecorder) RecordOrderByPlace(offset int) {
	if recorder == nil || offset <= recorder.start {
		return
	}
	place := offset - recorder.start
	recorder.spans.OrderByPlace = ast.Span{Start: place, End: place}
}

// Item returns the span of one select-list or RETURNING item.
//
// Takes start (int) which is the offset of the item's first byte.
// Takes expressionEnd (int) which is the offset one past its expression's last byte.
// Takes aliasStart (int) which is the offset of its alias's first byte, or of end when it
// has none.
// Takes end (int) which is the offset one past the item's last byte.
// Takes star (bool) which is true for a star item.
//
// Returns ast.ItemSpan which locates the item.
func (recorder *SpanRecorder) Item(start, expressionEnd, aliasStart, end int, star bool) ast.ItemSpan {
	return ast.ItemSpan{
		Span:       recorder.Span(start, end),
		Expression: recorder.Span(start, expressionEnd),
		Alias:      recorder.Span(aliasStart, end),
		Star:       star,
	}
}

// RecordProjectionItems records the items of the select list unless the statement's
// select list has been recorded already.
//
// Takes items ([]ast.ItemSpan) which are the items.
func (recorder *SpanRecorder) RecordProjectionItems(items []ast.ItemSpan) {
	if recorder != nil && recorder.spans.ProjectionItems == nil {
		recorder.spans.ProjectionItems = slices.Clone(items)
	}
}

// RecordReturningItems records the items of the RETURNING clause unless they have been
// recorded already.
//
// Takes items ([]ast.ItemSpan) which are the items.
func (recorder *SpanRecorder) RecordReturningItems(items []ast.ItemSpan) {
	if recorder != nil && recorder.spans.ReturningItems == nil {
		recorder.spans.ReturningItems = slices.Clone(items)
	}
}

// AddValuesRow records one parenthesised VALUES row.
//
// Takes row (ast.TupleSpan) which is the row, its spans from the statement's first byte.
func (recorder *SpanRecorder) AddValuesRow(row ast.TupleSpan) {
	if recorder != nil {
		recorder.spans.Values = append(recorder.spans.Values, row)
	}
}

// Finish returns the spans belonging to the analysed statement's own clauses.
//
// Clauses from nested queries and compound branches are excluded. The caller adds
// placeholders and the terminator.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the statement's analysis.
//
// Returns ast.StatementSpans which holds the statement's clause spans.
func (recorder *SpanRecorder) Finish(analysis *ast.RawQueryAnalysis) ast.StatementSpans {
	if recorder == nil || analysis == nil {
		return ast.StatementSpans{}
	}
	recorded := recorder.spans
	kept := ast.StatementSpans{
		Placeholders:    nil,
		ProjectionItems: nil,
		RowShaping:      nil,
		ReturningItems:  nil,
		Values:          nil,
		Literals:        nil,
		Quantifier:      ast.Span{},
		Projection:      ast.Span{},
		From:            ast.Span{},
		Where:           ast.Span{},
		WhereCondition:  ast.Span{},
		GroupBy:         ast.Span{},
		Having:          ast.Span{},
		OrderBy:         ast.Span{},
		OrderByPlace:    ast.Span{},
		Pagination:      ast.Span{},
		Locking:         ast.Span{},
		Trailing:        ast.Span{},
		Returning:       ast.Span{},
		Terminator:      ast.Span{},
	}
	switch {
	case analysis.DataChange == ast.DataChangeInsert:
		kept.Values, kept.Returning, kept.ReturningItems = recorded.Values, recorded.Returning, recorded.ReturningItems
	case analysis.DataChange != 0:
		kept.Where, kept.WhereCondition = recorded.Where, recorded.WhereCondition
		kept.Returning, kept.ReturningItems = recorded.Returning, recorded.ReturningItems
	case len(analysis.CompoundBranches) > 0:
		kept.OrderBy, kept.OrderByPlace = recorded.OrderBy, recorded.OrderByPlace
		kept.Pagination, kept.Locking, kept.Trailing = recorded.Pagination, recorded.Locking, recorded.Trailing
	default:
		kept = recorded
		kept.Values = nil
	}
	return kept
}

// field returns the field a SpanField names.
//
// Takes field (SpanField) which names the field.
//
// Returns *ast.Span which is the field, or nil for an unknown name.
func (recorder *SpanRecorder) field(field SpanField) *ast.Span {
	spans := &recorder.spans
	fields := [...]*ast.Span{
		SpanQuantifier:     &spans.Quantifier,
		SpanProjection:     &spans.Projection,
		SpanFrom:           &spans.From,
		SpanWhere:          &spans.Where,
		SpanWhereCondition: &spans.WhereCondition,
		SpanGroupBy:        &spans.GroupBy,
		SpanHaving:         &spans.Having,
		SpanOrderBy:        &spans.OrderBy,
		SpanPagination:     &spans.Pagination,
		SpanLocking:        &spans.Locking,
		SpanTrailing:       &spans.Trailing,
		SpanReturning:      &spans.Returning,
	}
	if int(field) >= len(fields) {
		return nil
	}
	return fields[field]
}

// ListTokens tells ValuesRow how a dialect classifies the tokens that nest and separate a
// list.
type ListTokens[K comparable] struct {
	// Comma is the kind of ",".
	Comma K

	// Openers lists the kinds that open a nested group, such as "(" and "[".
	Openers []K

	// Closers lists the kinds that close one, such as ")" and "]".
	Closers []K
}

// ValuesRow returns the span of the parenthesised VALUES row between the tokens at open
// and closing, with an item for each part between the commas outside every nested
// parenthesis or bracket.
//
// Takes tokens ([]Token[K]) which are the statement's tokens.
// Takes open (int) which is the index of the row's opening parenthesis.
// Takes closing (int) which is the index of its closing parenthesis.
// Takes kinds (ListTokens[K]) which classifies the tokens.
// Takes recorder (*SpanRecorder) which turns offsets into spans.
//
// Returns ast.TupleSpan which locates the row.
func ValuesRow[K comparable](tokens []Token[K], open, closing int, kinds ListTokens[K], recorder *SpanRecorder) ast.TupleSpan {
	if recorder == nil || open < 0 || closing <= open || closing >= len(tokens) {
		return ast.TupleSpan{Items: nil, Span: ast.Span{}}
	}
	row := ast.TupleSpan{Items: nil, Span: recorder.Span(tokens[open].Start, tokens[closing].End)}
	depth := 0
	itemStart := open + 1
	for index := open + 1; index <= closing; index++ {
		kind := tokens[index].Kind
		switch {
		case slices.Contains(kinds.Openers, kind):
			depth++
		case slices.Contains(kinds.Closers, kind) && index < closing:
			depth--
		}
		if (depth == 0 && kind == kinds.Comma) || index == closing {
			if index > itemStart {
				row.Items = append(row.Items, recorder.Span(tokens[itemStart].Start, tokens[index-1].End))
			}
			itemStart = index + 1
		}
	}
	return row
}
