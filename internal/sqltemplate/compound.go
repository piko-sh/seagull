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

package sqltemplate

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// rowsWrapOpen opens the derived table a statement is wrapped in.
	rowsWrapOpen = "SELECT * FROM ("

	// rowsWrapClose closes the derived table, whose rows a builder filters, orders and
	// limits. Its alias holds no generator vocabulary, so a preset's own prefix is the only
	// one its generated code names.
	rowsWrapClose = ") AS result_rows"
)

// wrapsCompound reports whether a builder query's compound statement is wrapped in a
// derived table before the builder orders or limits it. Some dialects apply an ORDER BY
// or limit written after a compound statement to its last branch alone.
//
// Returns bool which is true when the statement is wrapped.
func (b *builder) wrapsCompound() bool {
	query := b.input.Query
	return (query.IsDynamic || query.DynamicRuntime) && len(b.input.Analysis.CompoundBranches) > 0 &&
		b.input.Info != nil && b.input.Info.Pagination.CompoundBindsLastBranch
}

// wrappedCompound writes the template of a wrapped compound statement. The statement
// keeps its own ORDER BY and limit inside the derived table, and the builder's ordering
// and pagination follow it.
//
// Takes root (*node) which is the query's tree.
//
// Returns []sqlmodel.SQLSegment which holds the segments.
// Returns bool which is false when the segments cannot be written.
func (b *builder) wrappedCompound(root *node) ([]sqlmodel.SQLSegment, bool) {
	return b.wrappedRows(root, sqlmodel.SegmentOrderBy, sqlmodel.SegmentPagination)
}

// wrappedRows writes a template that reads the rows of the query's statement wrapped in a
// derived table. It keeps what follows the statement, and a limit inside the derived
// table keeps the statement's order where the dialect needs one.
//
// Takes root (*node) which is the query's tree.
// Takes places (...sqlmodel.SQLSegmentKind) which are the clauses a builder adds after
// the derived table, in order.
//
// Returns []sqlmodel.SQLSegment which holds the segments.
// Returns bool which is false when the segments cannot be written.
func (b *builder) wrappedRows(root *node, places ...sqlmodel.SQLSegmentKind) ([]sqlmodel.SQLSegment, bool) {
	segments, trailing, ok := b.wrapStatement(root, rowsWrapOpen, b.derivedOrderLimit(), places)
	if !ok {
		return nil, false
	}
	return append(segments, trailing...), true
}

// derivedCount writes a template that counts the rows of the query's statement wrapped in
// a derived table.
//
// Takes root (*node) which is the query's tree.
// Takes places (...sqlmodel.SQLSegmentKind) which are the clauses a builder adds after
// the derived table, in order.
//
// Returns []sqlmodel.SQLSegment which holds the segments.
// Returns bool which is false when the segments cannot be written.
func (b *builder) derivedCount(root *node, places ...sqlmodel.SQLSegmentKind) ([]sqlmodel.SQLSegment, bool) {
	segments, _, ok := b.wrapStatement(root, countWrapOpen, "", places)
	return segments, ok
}

// wrapStatement writes what precedes the statement, open, the statement with its own
// clauses kept as plain text, the derived table's close and the places of the clauses a
// builder adds.
//
// Takes root (*node) which is the query's tree.
// Takes open (string) which opens the derived table.
// Takes limit (string) which is written inside the derived table before any locking
// clause, or empty for none.
// Takes places ([]sqlmodel.SQLSegmentKind) which are the clauses a builder adds after the
// derived table, in order.
//
// Returns segments ([]sqlmodel.SQLSegment) which holds the wrapped statement.
// Returns trailing ([]sqlmodel.SQLSegment) which holds what follows the statement.
// Returns ok (bool) which is false when the segments cannot be written.
func (b *builder) wrapStatement(root *node, open, limit string, places []sqlmodel.SQLSegmentKind) (
	segments, trailing []sqlmodel.SQLSegment, ok bool) {
	start, end := b.statement.Location, b.statementEnd()
	split := end
	if limit != "" {
		split = b.placeBefore(b.spans.Locking, b.spans.Trailing)
	}
	leading, leadingOK := b.segmentsWithin(root, ast.Span{Start: root.span.Start, End: start}, nil)
	head, headOK := b.segmentsWithin(root, ast.Span{Start: start, End: split}, countTreatment)
	tail, tailOK := b.segmentsWithin(root, ast.Span{Start: split, End: end}, countTreatment)
	trailing, trailingOK := b.segmentsWithin(root, ast.Span{Start: end, End: root.span.End}, countTreatment)
	if !leadingOK || !headOK || !tailOK || !trailingOK {
		return nil, nil, false
	}
	segments = appendText(leading, open)
	segments = append(segments, head...)
	segments = appendText(segments, limit)
	segments = append(segments, tail...)
	segments = appendText(trimTrailingSpace(segments), rowsWrapClose)
	for _, kind := range places {
		segments = append(segments, placeSegment(kind))
	}
	return segments, trailing, true
}

// derivedOrderLimit returns the limit written inside the derived table so the engine
// keeps the statement's ORDER BY, on a dialect that drops the order of a derived table
// without a limit, such as MySQL.
//
// Returns string which is the limit, or empty when the statement writes no ORDER BY,
// writes its own limit, or the dialect keeps the order without one.
func (b *builder) derivedOrderLimit() string {
	if b.input.Info == nil || !b.spans.OrderBy.Known() || b.spans.Pagination.Known() {
		return ""
	}
	pagination := &b.input.Info.Pagination
	if pagination.DerivedOrderLimit == "" {
		return ""
	}
	return " " + pagination.Limit.Before + pagination.DerivedOrderLimit + pagination.Limit.After
}

// placeSegment returns the place of a clause the statement does not write, where a
// builder writes its own.
//
// Takes kind (sqlmodel.SQLSegmentKind) which is the clause's kind.
//
// Returns sqlmodel.SQLSegment which is the empty place.
func placeSegment(kind sqlmodel.SQLSegmentKind) sqlmodel.SQLSegment {
	segment := sqlmodel.SQLSegment{}
	segment.Kind = kind
	return segment
}
