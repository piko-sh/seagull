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

package ast

// Span is a half-open range of byte offsets, from the first byte it covers to one past
// the last.
//
// The field holding a span says what its offsets count from. The zero span means the
// dialect recorded nothing there.
type Span struct {
	// Start is the offset of the first byte.
	Start int

	// End is the offset one past the last byte.
	End int
}

// Known reports whether the span covers anything.
//
// Returns bool which is true when the span covers at least one byte.
func (span Span) Known() bool {
	return span.End > span.Start
}

// Contains reports whether another span lies within this one.
//
// Takes other (Span) which is the span to test.
//
// Returns bool which is true when both spans are known and other starts and ends inside
// this span.
func (span Span) Contains(other Span) bool {
	return span.Known() && other.Known() && other.Start >= span.Start && other.End <= span.End
}

// StatementSpans locates the parts of a statement a code generator cuts or rewrites, as
// byte offsets from the statement's first byte (ParsedStatement.Location), so that
// ParsedStatement.Location plus an offset indexes the SQL the dialect parsed.
//
// A dialect records them on the top-level analysis only. The analyses nested in it, of
// CTE bodies, derived tables, subqueries, an INSERT's SELECT and compound branches, carry
// none, but Placeholders covers the placeholders inside them too. For a compound
// statement only Placeholders, OrderBy, OrderByPlace, Pagination, Locking, Trailing and
// Terminator are recorded, since the other clauses belong to its branches.
type StatementSpans struct {
	// Placeholders lists every placeholder occurrence in the statement, in source order,
	// including those inside CTE bodies, derived tables, subqueries, an INSERT's SELECT and
	// compound branches.
	Placeholders []PlaceholderSpan

	// ProjectionItems lists the items of the top-level select list, parallel to
	// OutputColumns as the dialect returns them, a star being one item.
	ProjectionItems []ItemSpan

	// RowShaping lists the clauses an engine applies to the rows after HAVING that change
	// which rows the statement returns, such as QUALIFY or ClickHouse's LIMIT n BY, so a
	// statement counting the rows must keep them.
	RowShaping []Span

	// ReturningItems lists the items of the RETURNING clause, parallel to OutputColumns as
	// the dialect returns them for a statement with RETURNING.
	ReturningItems []ItemSpan

	// Values lists the parenthesised rows of an INSERT ... VALUES, in order.
	Values []TupleSpan

	// Literals lists the string literals of the statement, in order. A dialect may leave it
	// empty.
	Literals []Span

	// Quantifier covers a DISTINCT, DISTINCT ON (...) or ALL after SELECT.
	Quantifier Span

	// Projection covers the top-level select list, after SELECT and any quantifier and
	// before FROM.
	Projection Span

	// From covers the FROM keyword and everything up to WHERE or the first clause that must
	// follow WHERE, so joins, ARRAY JOIN, SAMPLE, FINAL and PREWHERE belong to it.
	From Span

	// Where covers the WHERE keyword through the end of its condition.
	Where Span

	// WhereCondition covers the condition of the WHERE clause alone.
	WhereCondition Span

	// GroupBy covers the GROUP BY clause, modifiers such as WITH ROLLUP or WITH TOTALS
	// included.
	GroupBy Span

	// Having covers the HAVING clause.
	Having Span

	// OrderBy covers the ORDER BY clause, modifiers such as NULLS FIRST or WITH FILL
	// included.
	OrderBy Span

	// OrderByPlace marks where an ORDER BY clause starts or can be inserted in a SELECT
	// statement. Other statements leave it zero.
	OrderByPlace Span

	// Pagination covers the LIMIT, OFFSET and FETCH clauses as written, in whatever order,
	// such as MySQL's LIMIT a, b.
	Pagination Span

	// Locking covers the row-locking clauses, such as FOR UPDATE or FOR SHARE.
	Locking Span

	// Trailing covers the clauses that must stay last, even in a statement counting the
	// rows, such as ClickHouse's SETTINGS and FORMAT.
	Trailing Span

	// Returning covers the RETURNING clause.
	Returning Span

	// Terminator covers the terminator that ends the statement, such as ";". It follows the
	// statement's last token, so it lies at or past ParsedStatement.Length.
	Terminator Span
}

// PlaceholderSpan locates one placeholder occurrence, such as "$1", "?", "?3", ":name",
// "@p2" or "{id:UInt64}".
type PlaceholderSpan struct {
	// Name is the parameter's name for a named form, without its sigil or braces.
	Name string

	// List locates the IN list the placeholder is the sole element of, and is zero
	// otherwise.
	List ListSpan

	// Span covers the whole placeholder.
	Span Span

	// Type covers the type a typed form writes, such as "UInt64" in {id:UInt64}, and is zero
	// otherwise.
	Type Span

	// Number is the parameter's number for a numbered form, and its 1-based ordinal among
	// the statement's anonymous placeholders for an anonymous form.
	Number int
}

// ListSpan locates an IN list whose sole element is a placeholder, so a code generator
// can expand the placeholder into one per element of a slice.
type ListSpan struct {
	// Operator covers the operator, from NOT, when present, through IN.
	Operator Span

	// List covers the list from its opening parenthesis through its closing one.
	List Span

	// Negated reports NOT IN.
	Negated bool
}

// ItemSpan locates one item of a select list or a RETURNING clause.
type ItemSpan struct {
	// Span covers the whole item, its alias included.
	Span Span

	// Expression covers the item's expression alone.
	Expression Span

	// Alias covers the alias, the AS keyword included, and is zero when the item has none.
	Alias Span

	// Star reports an item that is a star, such as * or q.*.
	Star bool
}

// TupleSpan locates one parenthesised row of an INSERT ... VALUES.
type TupleSpan struct {
	// Items covers each value of the row, in order, without the commas between them.
	Items []Span

	// Span covers the row from its opening parenthesis through its closing one.
	Span Span
}
