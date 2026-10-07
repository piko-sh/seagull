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
	// countProjection replaces the select list of a query whose rows are the rows it reads.
	countProjection = " COUNT(*) "

	// countWrapOpen opens the query that counts the rows of a query it wraps.
	countWrapOpen = "SELECT COUNT(*) FROM ("

	// countWrapClose closes it, naming the wrapped query.
	countWrapClose = ") sub"

	// countTrailingSeparator separates the clauses that stay last from what precedes them.
	countTrailingSeparator = " "
)

// countTemplate makes the template that counts the rows a runtime builder query returns,
// from the query's own segments, so the filter a caller adds lands in it too.
//
// When the query's rows are the rows it reads, its select list becomes COUNT(*) and
// everything from its ORDER BY, pagination, locking or terminator on is left out.
// Otherwise, when it groups, has HAVING, a DISTINCT or other quantifier, a clause that
// shapes the rows after HAVING, an item that aggregates, or is compound, it is wrapped
// whole in SELECT COUNT(*) FROM (...) sub, less the same clauses; a compound statement
// keeps its ORDER BY and pagination, which belong to its branches on some engines, and
// any statement keeps an ORDER BY a row-shaping clause follows. Clauses that must stay
// last, such as SETTINGS, follow the count in both forms.
//
// Takes root (*node) which is the query's tree.
//
// Returns count (sqlmodel.SQLTemplate) which is the count template.
// Returns wrapped (bool) which is true when it wraps the query.
// Returns applies (bool) which is false when counting does not apply: the query is not a
// runtime builder query, does not read rows, or has no FROM to count from.
func (b *builder) countTemplate(root *node) (count sqlmodel.SQLTemplate, wrapped bool, applies bool) {
	compound := len(b.input.Analysis.CompoundBranches) > 0
	if !b.input.Query.DynamicRuntime || !compound && !b.selectsRows() {
		return sqlmodel.SQLTemplate{}, false, false
	}
	wrapped = compound || b.countWraps()
	cut := b.countCut(compound)
	var segments []sqlmodel.SQLSegment
	var ok bool
	if wrapped {
		segments, ok = b.wrappedCount(root, cut)
	} else {
		segments, ok = b.plainCount(root, cut)
	}
	if !ok {
		return sqlmodel.SQLTemplate{}, false, false
	}
	if trailing := b.absolute(b.spans.Trailing); trailing.Known() {
		written, trailingOK := b.segmentsWithin(root, trailing, nil)
		if !trailingOK {
			return sqlmodel.SQLTemplate{}, false, false
		}
		segments = append(appendText(segments, countTrailingSeparator), written...)
	}
	return b.withArguments(segments), wrapped, true
}

// countWraps reports whether the rows a non-compound query returns are not the rows it
// reads: it groups, has HAVING, a quantifier such as DISTINCT, a clause that shapes the
// rows after HAVING, or an item that aggregates.
//
// Returns bool which is true when its count must wrap it.
func (b *builder) countWraps() bool {
	return b.spans.GroupBy.Known() || b.spans.Having.Known() || b.spans.Quantifier.Known() ||
		len(b.spans.RowShaping) > 0 || b.input.Aggregated
}

// countCut returns where the part of the query its count keeps ends: at the first clause
// written of the ORDER BY, unless a row-shaping clause follows it, the pagination, the
// locking clause, the clauses that stay last and the terminator; a compound statement
// keeps its ORDER BY and pagination.
//
// Takes compound (bool) which is true for a compound statement.
//
// Returns int which is the offset into Input.SQL.
func (b *builder) countCut(compound bool) int {
	clauses := []ast.Span{b.spans.Locking, b.spans.Trailing, b.spans.Terminator}
	if !compound {
		clauses = append(clauses, b.spans.Pagination)
		if !b.rowShapingFollows(b.spans.OrderBy) {
			clauses = append(clauses, b.spans.OrderBy)
		}
	}
	return b.firstStart(b.statementEnd(), clauses...)
}

// rowShapingFollows reports whether a row-shaping clause comes after a clause.
//
// Takes clause (ast.Span) which is the clause, relative to the statement.
//
// Returns bool which is true when a row-shaping clause starts after it.
func (b *builder) rowShapingFollows(clause ast.Span) bool {
	if !clause.Known() {
		return false
	}
	for _, shaping := range b.spans.RowShaping {
		if shaping.Known() && shaping.Start >= clause.End {
			return true
		}
	}
	return false
}

// wrappedCount writes the count that wraps the query: what precedes the statement, then
// SELECT COUNT(*) FROM ( the statement up to the cut ) sub.
//
// Takes root (*node) which is the query's tree.
// Takes cut (int) which is where the part the count keeps ends.
//
// Returns []sqlmodel.SQLSegment which holds the segments.
// Returns bool which is false when the segments cannot be written.
func (b *builder) wrappedCount(root *node, cut int) ([]sqlmodel.SQLSegment, bool) {
	start := b.statement.Location
	leading, leadingOK := b.segmentsWithin(root, ast.Span{Start: 0, End: start}, nil)
	inner, innerOK := b.segmentsWithin(root, ast.Span{Start: start, End: cut}, countTreatment)
	if !leadingOK || !innerOK {
		return nil, false
	}
	segments := appendText(leading, countWrapOpen)
	segments = append(segments, trimTrailingSpace(inner)...)
	return appendText(segments, countWrapClose), true
}

// plainCount writes the count that replaces the query's select list with COUNT(*): what
// precedes the select list, COUNT(*), and the query from its FROM up to the cut.
//
// Takes root (*node) which is the query's tree.
// Takes cut (int) which is where the part the count keeps ends.
//
// Returns []sqlmodel.SQLSegment which holds the segments.
// Returns bool which is false when the query has no FROM or the segments cannot be
// written.
func (b *builder) plainCount(root *node, cut int) ([]sqlmodel.SQLSegment, bool) {
	projection, from := b.absolute(b.spans.Projection), b.absolute(b.spans.From)
	if !projection.Known() || !from.Known() || from.Start > cut {
		return nil, false
	}
	head, headOK := b.segmentsWithin(root, ast.Span{Start: 0, End: projection.Start}, nil)
	body, bodyOK := b.segmentsWithin(root, ast.Span{Start: from.Start, End: cut}, countTreatment)
	if !headOK || !bodyOK {
		return nil, false
	}
	segments := appendText(trimTrailingSpace(head), countProjection)
	return append(segments, trimTrailingSpace(body)...), true
}

// countTreatment says how a count writes a node: the place of an ORDER BY or pagination a
// runtime builder edits is left out, and such a clause the count keeps, as a compound
// statement's, becomes plain text, so the count holds no place for them; every other node
// is kept.
//
// Takes current (*node) which is the node.
//
// Returns treatment which says how to write it.
func countTreatment(current *node) treatment {
	if current.segment.Kind != sqlmodel.SegmentOrderBy && current.segment.Kind != sqlmodel.SegmentPagination {
		return keep
	}
	if !current.span.Known() {
		return leaveOut
	}
	return unwrap
}
