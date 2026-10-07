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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// itemNodes builds template nodes for the query's output items.
//
// It uses RETURNING when present and the select list otherwise. Unknown or mismatched
// output origins produce no nodes.
//
// Returns []*node which holds the nodes.
func (b *builder) itemNodes() []*node {
	items := b.spans.ReturningItems
	returning := len(items) > 0
	if !returning {
		items = b.spans.ProjectionItems
	}
	if len(items) == 0 || len(items) != len(b.input.Analysis.OutputColumns) {
		return nil
	}
	columns := b.columnsByItem(len(items))
	if columns == nil {
		return nil
	}
	nodes := make([]*node, 0, len(items))
	for index, item := range items {
		span := b.absolute(item.Expression)
		segment := sqlmodel.SQLSegment{}
		switch {
		case !span.Known():
			continue
		case item.Star:
			segment.Kind = sqlmodel.SegmentStar
			segment.Text = b.kept(span.Start, span.End)
			segment.Star = b.starColumns(columns[index], returning && bareStar(segment.Text))
			segment.AsWritten = !b.referable(columns[index])
			nodes = append(nodes, &node{children: nil, segment: segment, span: span, priority: priorityLeaf, leaf: true})
		case slices.Contains(b.input.NamedItems, index):
			continue
		case len(columns[index]) == 1:
			segment.Kind = sqlmodel.SegmentColumn
			segment.Column = columns[index][0]
			segment.Aliased = item.Alias.Known()
			nodes = append(nodes, &node{children: nil, segment: segment, span: span, priority: priorityColumn, leaf: false})
		}
	}
	return nodes
}

// columnsByItem groups the output columns by the item they came from.
//
// Takes count (int) which is how many items there are.
//
// Returns [][]int which holds, for each item, the indices of its output columns, or nil
// when the origins are unknown or do not fit the items.
func (b *builder) columnsByItem(count int) [][]int {
	origins := b.input.Origins
	if origins == nil || len(origins) != len(b.input.Query.OutputColumns) {
		return nil
	}
	columns := make([][]int, count)
	for column, origin := range origins {
		if origin < 0 || origin >= count {
			return nil
		}
		columns[origin] = append(columns[origin], column)
	}
	return columns
}

// starColumns builds names and qualifiers for expanding a star. Columns merged by FULL
// joins are unqualified, and unqualified RETURNING stars use bare names where unambiguous
// for dialect compatibility.
//
// Takes columns ([]int) which are the indices of the star's output columns.
// Takes returning (bool) which is true for an unqualified star in a RETURNING clause.
//
// Returns []sqlmodel.StarColumn which holds the columns.
func (b *builder) starColumns(columns []int, returning bool) []sqlmodel.StarColumn {
	star := make([]sqlmodel.StarColumn, 0, len(columns))
	for _, column := range columns {
		output := &b.input.Query.OutputColumns[column]
		entry := sqlmodel.StarColumn{Qualifier: output.SourceQualifier, Name: output.SourceColumn, Column: column}
		if entry.Name == "" && b.referableColumn(column) {
			entry.Qualifier, entry.Name = "", output.Name
		}
		if returning && b.aliasQualified(output) && b.uniqueInStar(columns, column) {
			entry.Qualifier = ""
		}
		star = append(star, entry)
	}
	return star
}

// referable reports whether every column a star stands for can be named in its place, so
// a renderer may expand it.
//
// Takes columns ([]int) which are the indices of the star's output columns.
//
// Returns bool which is true when the star can be expanded.
func (b *builder) referable(columns []int) bool {
	for _, column := range columns {
		if !b.referableColumn(column) {
			return false
		}
	}
	return len(columns) > 0
}

// referableColumn reports whether an output column a star stands for can be named in the
// star's place, as Input.Referable says.
//
// Takes column (int) which is the index into Query.OutputColumns.
//
// Returns bool which is true when it can.
func (b *builder) referableColumn(column int) bool {
	referable := b.input.Referable
	return len(referable) == len(b.input.Query.OutputColumns) && referable[column]
}

// aliasQualified reports whether an output column is qualified by an alias rather than by
// the name of the table it comes from.
//
// Takes output (*sqlmodel.OutputColumn) which is the column.
//
// Returns bool which is true when the qualifier is an alias.
func (b *builder) aliasQualified(output *sqlmodel.OutputColumn) bool {
	return output.SourceQualifier != "" && !b.input.Info.Identifiers.Equal(output.SourceQualifier, output.SourceTable)
}

// uniqueInStar reports whether no other column a star stands for has a column's name,
// compared under the dialect's identifier rules, so the name alone reads it.
//
// Takes columns ([]int) which are the indices of the star's output columns.
// Takes column (int) which is the index of the column.
//
// Returns bool which is true when the name is the star's only one of its kind.
func (b *builder) uniqueInStar(columns []int, column int) bool {
	outputs := b.input.Query.OutputColumns
	name := starColumnName(&outputs[column])
	for _, other := range columns {
		if other != column && b.input.Info.Identifiers.Equal(starColumnName(&outputs[other]), name) {
			return false
		}
	}
	return true
}

// clauseNodes makes, for a dynamic or runtime builder query that reads rows, the nodes of
// the clauses it edits: the WHERE condition, unless the statement is compound, the ORDER
// BY and the pagination, each covering the clause when it is written and marking the
// place it goes otherwise.
//
// Returns []*node which holds the nodes.
func (b *builder) clauseNodes() []*node {
	query := b.input.Query
	compound := len(b.input.Analysis.CompoundBranches) > 0
	if !query.IsDynamic && !query.DynamicRuntime || !compound && !b.selectsRows() {
		return nil
	}
	nodes := make([]*node, 0, clauseCount)
	if filter := b.filterNode(compound); filter != nil {
		nodes = append(nodes, filter)
	}
	return append(nodes,
		b.clauseNode(sqlmodel.SegmentOrderBy, b.spans.OrderBy, b.orderByPlace()),
		b.clauseNode(sqlmodel.SegmentPagination, b.spans.Pagination, b.placeBefore(b.spans.Locking, b.spans.Trailing)),
	)
}

// selectsRows reports whether the statement is a SELECT that is not compound: one the
// dialect recorded a select list for that writes no table.
//
// Returns bool which is true for such a SELECT.
func (b *builder) selectsRows() bool {
	return b.spans.Projection.Known() && b.input.Analysis.InsertTarget.Name == ""
}

// filterNode makes the node of the WHERE condition: the condition when written, the place
// after FROM, or after the select list of a SELECT without FROM, when not.
//
// Takes compound (bool) which is true for a compound statement, which has no WHERE of its
// own.
//
// Returns *node which is the node, or nil when there is no place for one.
func (b *builder) filterNode(compound bool) *node {
	if compound {
		return nil
	}
	segment := sqlmodel.SQLSegment{}
	segment.Kind = sqlmodel.SegmentFilter
	if condition := b.absolute(b.spans.WhereCondition); condition.Known() {
		segment.Present = true
		return &node{children: nil, segment: segment, span: condition, priority: priorityClause, leaf: false}
	}
	if b.spans.Where.Known() {
		return nil
	}
	place := b.absolute(b.spans.From)
	if !place.Known() {
		place = b.absolute(b.spans.Projection)
	}
	if !place.Known() {
		return nil
	}
	return &node{children: nil, segment: segment, span: ast.Span{Start: place.End, End: place.End}, priority: priorityClause, leaf: false}
}

// clauseNode makes the node of an ORDER BY or pagination clause: the clause when written,
// and its place otherwise.
//
// Takes kind (sqlmodel.SQLSegmentKind) which is the clause's segment kind.
// Takes written (ast.Span) which is the clause's span, relative to the statement.
// Takes place (int) which is where the clause goes when it is not written.
//
// Returns *node which is the node.
func (b *builder) clauseNode(kind sqlmodel.SQLSegmentKind, written ast.Span, place int) *node {
	segment := sqlmodel.SQLSegment{}
	segment.Kind = kind
	span := b.absolute(written)
	if span.Known() {
		segment.Present = true
	} else {
		span = ast.Span{Start: place, End: place}
	}
	return &node{children: nil, segment: segment, span: span, priority: priorityClause, leaf: false}
}

// orderByPlace returns the place an ORDER BY goes when the statement writes none: the
// place the dialect recorded, which knows the clauses its engine applies after the
// ordering, such as ClickHouse's LIMIT n BY, or, when it recorded none, the place before
// the pagination, locking and trailing clauses.
//
// Returns int which is the place, as an offset into Input.SQL.
func (b *builder) orderByPlace() int {
	if place := b.spans.OrderByPlace; place.Start > 0 {
		return b.statement.Location + place.Start
	}
	return b.placeBefore(b.spans.Pagination, b.spans.Locking, b.spans.Trailing)
}

// placeBefore returns the place a clause goes that precedes the first of some clauses
// written in the statement: the end of the last token before it, or of the statement when
// none is written.
//
// Takes following (...ast.Span) which are the clauses, relative to the statement.
//
// Returns int which is the place, as an offset into Input.SQL.
func (b *builder) placeBefore(following ...ast.Span) int {
	return b.lastTokenEndBefore(b.firstStart(b.statementEnd(), following...))
}

// firstStart returns where the first of some clauses written in the statement starts.
//
// Takes fallback (int) which is returned when none is written.
// Takes clauses (...ast.Span) which are the clauses, relative to the statement.
//
// Returns int which is the offset into Input.SQL.
func (b *builder) firstStart(fallback int, clauses ...ast.Span) int {
	first := fallback
	for _, clause := range clauses {
		if span := b.absolute(clause); span.Known() && span.Start < first {
			first = span.Start
		}
	}
	return first
}

// bareStar reports whether a star item is written without a qualifier.
//
// Takes text (string) which is the item as written.
//
// Returns bool which is true for a lone star.
func bareStar(text string) bool {
	return strings.TrimSpace(text) == "*"
}

// starColumnName returns the name an output column of a star has in its relation, or its
// output name for a column no one relation holds.
//
// Takes output (*sqlmodel.OutputColumn) which is the column.
//
// Returns string which is the name.
func starColumnName(output *sqlmodel.OutputColumn) string {
	if output.SourceColumn != "" {
		return output.SourceColumn
	}
	return output.Name
}
