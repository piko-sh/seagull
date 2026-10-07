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
	"cmp"
	"math"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// priorityColumn is how strongly an output item's node holds its place when it overlaps
	// another node without nesting: the least, since the template is complete without it.
	priorityColumn = iota + 1

	// priorityClause is how strongly a clause's node holds its place.
	priorityClause

	// priorityLeaf is how strongly a placeholder, list or star holds its place: the most,
	// since leaving one out would leave SQL the template cannot render.
	priorityLeaf
)

// node is one segment of the template, located in Input.SQL, with the nodes inside it.
type node struct {
	// children holds the nodes inside this one, in order.
	children []*node

	// segment is the segment the node writes, its children filled in when it is written.
	segment sqlmodel.SQLSegment

	// span covers the node in Input.SQL; a node whose span is empty marks a place.
	span ast.Span

	// priority is how strongly the node holds its place against an overlapping one.
	priority int

	// leaf reports a node whose text the segment replaces whole.
	leaf bool
}

// offsetTree keeps, for each position among the sorted offsets of the template's nodes,
// the combination of the values raised there, and combines them over a range of positions
// in logarithmic time.
type offsetTree struct {
	// combine joins two values; it is associative and commutative, with identity as its
	// neutral value.
	combine func(first, second int) int

	// values holds the tree: the leaves from size on, and each inner node at half its
	// children's index.
	values []int

	// identity is the value of a position nothing was raised at.
	identity int

	// size is the number of positions.
	size int
}

// newOffsetTree makes a tree over a number of positions, each holding the identity.
//
// Takes size (int) which is the number of positions.
// Takes identity (int) which is combine's neutral value.
// Takes combine (func(first, second int) int) which joins two values.
//
// Returns *offsetTree which holds the identity everywhere.
func newOffsetTree(size, identity int, combine func(first, second int) int) *offsetTree {
	values := make([]int, 2*size)
	for index := range values {
		values[index] = identity
	}
	return &offsetTree{combine: combine, values: values, identity: identity, size: size}
}

// raise combines a value into what a position holds.
//
// Takes position (int) which is the position.
// Takes value (int) which is the value combined in.
func (tree *offsetTree) raise(position, value int) {
	index := position + tree.size
	tree.values[index] = tree.combine(tree.values[index], value)
	for index > 1 {
		index /= 2
		tree.values[index] = tree.combine(tree.values[2*index], tree.values[2*index+1])
	}
}

// query combines what the positions from first up to, but not including, last hold.
//
// Takes first (int) which is the first position.
// Takes last (int) which is the position just past the range.
//
// Returns int which is the combination, or the identity for an empty range.
func (tree *offsetTree) query(first, last int) int {
	result := tree.identity
	for low, high := first+tree.size, last+tree.size; low < high; low, high = low/2, high/2 {
		if low%2 == 1 {
			result = tree.combine(result, tree.values[low])
			low++
		}
		if high%2 == 1 {
			high--
			result = tree.combine(result, tree.values[high])
		}
	}
	return result
}

// tree locates every segment of the template in Input.SQL and nests them.
//
// A node that overlaps another without nesting is left out, the one with the lower
// priority. The nesting must reproduce the text the query keeps; when it does not, the
// invariant is broken and it is reported.
//
// Returns *node which is the root, covering the whole of Input.SQL.
// Returns bool which is false when the invariant is broken.
func (b *builder) tree() (*node, bool) {
	root := &node{children: nil, segment: sqlmodel.SQLSegment{}, span: ast.Span{Start: 0, End: len(b.input.SQL)}, priority: 0, leaf: false}
	nodes := dropConflicts(b.nodes())
	slices.SortStableFunc(nodes, compareNodes)
	stack := []*node{root}
	for _, current := range nodes {
		for len(stack) > 1 && !encloses(stack[len(stack)-1], current) {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]
		parent.children = append(parent.children, current)
		if !current.leaf && current.span.Known() {
			stack = append(stack, current)
		}
	}
	if b.written(root) != b.kept(root.span.Start, root.span.End) {
		b.report(diag.CodeInternalNilGuard, ast.Span{}, "the template made from the query's spans does not reproduce its SQL")
		return nil, false
	}
	return root, true
}

// nodes gathers the template's nodes: the clauses a dynamic query edits, the VALUES row,
// the output items and the placeholders, leaving out any that lies outside Input.SQL.
//
// Returns []*node which holds the nodes, unordered.
func (b *builder) nodes() []*node {
	nodes := slices.Concat(b.clauseNodes(), b.valuesNodes(), b.itemNodes(), b.placeholderNodes())
	return slices.DeleteFunc(nodes, func(current *node) bool {
		return current.span.Start < 0 || current.span.End > len(b.input.SQL) || current.span.End < current.span.Start
	})
}

// placeholderNodes makes a node for each placeholder: a list for the sole element of an
// IN list whose parameter is a slice, and a parameter otherwise.
//
// Returns []*node which holds the nodes.
func (b *builder) placeholderNodes() []*node {
	nodes := make([]*node, 0, len(b.occurrences))
	for index := range b.occurrences {
		current := &b.occurrences[index]
		segment := sqlmodel.SQLSegment{}
		segment.Parameter = current.parameter
		if current.typeSpan.Known() {
			segment.TypeText = b.input.SQL[current.typeSpan.Start:current.typeSpan.End]
		}
		span := current.span
		if current.operator.Known() && current.list.Known() && b.input.Query.Parameters[current.parameter].IsSlice {
			segment.Kind = sqlmodel.SegmentList
			segment.Text = b.kept(current.operator.Start, current.list.Start)
			segment.Negated = current.negated
			span = ast.Span{Start: current.operator.Start, End: current.list.End}
		} else {
			segment.Kind = sqlmodel.SegmentParameter
		}
		nodes = append(nodes, &node{children: nil, segment: segment, span: span, priority: priorityLeaf, leaf: true})
	}
	return nodes
}

// valuesNodes makes the node of the VALUES row of an INSERT with exactly one.
//
// Returns []*node which holds the node, or nil when there is not exactly one row.
func (b *builder) valuesNodes() []*node {
	if len(b.spans.Values) != 1 {
		return nil
	}
	span := b.absolute(b.spans.Values[0].Span)
	if !span.Known() {
		return nil
	}
	segment := sqlmodel.SQLSegment{}
	segment.Kind = sqlmodel.SegmentValuesRow
	return []*node{{segment: segment, children: nil, span: span, priority: priorityClause, leaf: false}}
}

// written returns the text a node stands for, as the query writes it.
//
// Takes current (*node) which is the node.
//
// Returns string which is the text.
func (b *builder) written(current *node) string {
	if current.leaf || len(current.children) == 0 {
		return b.kept(current.span.Start, current.span.End)
	}
	var text strings.Builder
	cursor := current.span.Start
	for _, child := range current.children {
		text.WriteString(b.kept(cursor, child.span.Start))
		text.WriteString(b.written(child))
		cursor = child.span.End
	}
	text.WriteString(b.kept(cursor, current.span.End))
	return text.String()
}

// dropConflicts leaves out each node that overlaps a stronger one without nesting, and
// each place inside a leaf or an output item, where nothing is inserted.
//
// The nodes with a span are taken from the strongest to the weakest, those of equal
// priority in the order given, and each is kept unless it crosses a node kept before it,
// so a node is only ever left out for a stronger node that stays. A place, weaker than
// any node with a span, is then kept unless it lies inside a kept leaf or output item.
// Trees over the nodes' offsets answer each check in logarithmic time, so the whole is
// O(n log n) in the number of nodes.
//
// Takes nodes ([]*node) which are the nodes.
//
// Returns []*node which holds the nodes kept, in the order given.
func dropConflicts(nodes []*node) []*node {
	offsets := nodeOffsets(nodes)
	leastStartByEnd := newOffsetTree(len(offsets), math.MaxInt, func(first, second int) int { return min(first, second) })
	greatestEndByStart := newOffsetTree(len(offsets), math.MinInt, func(first, second int) int { return max(first, second) })
	holderEndByStart := newOffsetTree(len(offsets), math.MinInt, func(first, second int) int { return max(first, second) })
	dropped := make([]bool, len(nodes))
	for _, index := range byStrength(nodes) {
		current := nodes[index]
		start, end := offsetPosition(offsets, current.span.Start), offsetPosition(offsets, current.span.End)
		if leastStartByEnd.query(start+1, end) < current.span.Start || greatestEndByStart.query(start+1, end) > current.span.End {
			dropped[index] = true
			continue
		}
		leastStartByEnd.raise(end, current.span.Start)
		greatestEndByStart.raise(start, current.span.End)
		if current.leaf || current.priority == priorityColumn {
			holderEndByStart.raise(start, current.span.End)
		}
	}
	kept := make([]*node, 0, len(nodes))
	for index, current := range nodes {
		place := !current.span.Known()
		if dropped[index] || (place && holderEndByStart.query(0, offsetPosition(offsets, current.span.Start)) > current.span.Start) {
			continue
		}
		kept = append(kept, current)
	}
	return kept
}

// nodeOffsets returns the offsets the nodes start and end at, sorted, each once.
//
// Takes nodes ([]*node) which are the nodes.
//
// Returns []int which holds the offsets.
func nodeOffsets(nodes []*node) []int {
	offsets := make([]int, 0, 2*len(nodes))
	for _, current := range nodes {
		offsets = append(offsets, current.span.Start, current.span.End)
	}
	slices.Sort(offsets)
	return slices.Compact(offsets)
}

// offsetPosition returns the position of an offset among the sorted offsets of the nodes.
//
// Takes offsets ([]int) which are the sorted offsets.
// Takes offset (int) which is one of them.
//
// Returns int which is its position.
func offsetPosition(offsets []int, offset int) int {
	position, _ := slices.BinarySearch(offsets, offset)
	return position
}

// byStrength returns the positions of the nodes with a span, the strongest first and
// those of equal priority in the order given.
//
// Takes nodes ([]*node) which are the nodes.
//
// Returns []int which holds the positions.
func byStrength(nodes []*node) []int {
	order := make([]int, 0, len(nodes))
	for index, current := range nodes {
		if current.span.Known() {
			order = append(order, index)
		}
	}
	slices.SortStableFunc(order, func(first, second int) int {
		return cmp.Compare(nodes[second].priority, nodes[first].priority)
	})
	return order
}

// compareNodes orders nodes for nesting: by start, a place before a node that starts
// there, a longer node before a shorter one, a container before what it holds, and places
// at one offset in the order the clauses go.
//
// Takes first (*node) which is one node.
// Takes second (*node) which is the other.
//
// Returns int which is negative when first comes first.
func compareNodes(first, second *node) int {
	return cmp.Or(
		cmp.Compare(first.span.Start, second.span.Start),
		cmp.Compare(boolRank(first.span.Known()), boolRank(second.span.Known())),
		cmp.Compare(second.span.End, first.span.End),
		cmp.Compare(first.priority, second.priority),
		cmp.Compare(first.segment.Kind, second.segment.Kind),
	)
}

// boolRank orders false before true.
//
// Takes value (bool) which is the value.
//
// Returns int which is 1 for true and 0 for false.
func boolRank(value bool) int {
	if value {
		return 1
	}
	return 0
}

// encloses reports whether a container holds a node: a place strictly inside it, or a
// node within its bounds.
//
// Takes container (*node) which is the candidate container.
// Takes current (*node) which is the node.
//
// Returns bool which is true when the container holds it.
func encloses(container, current *node) bool {
	if !current.span.Known() {
		return container.span.Start < current.span.Start && current.span.Start < container.span.End
	}
	return container.span.Start <= current.span.Start && current.span.End <= container.span.End
}
