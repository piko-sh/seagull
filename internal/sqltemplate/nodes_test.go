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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func spanNode(start, end, priority int) *node {
	return &node{children: nil, segment: sqlmodel.SQLSegment{}, span: ast.Span{Start: start, End: end}, priority: priority, leaf: priority == priorityLeaf}
}

func placeNode(at int) *node {
	return &node{children: nil, segment: sqlmodel.SQLSegment{}, span: ast.Span{Start: at, End: at}, priority: priorityClause, leaf: false}
}

func keptPositions(nodes, kept []*node) []int {
	positions := make([]int, 0, len(kept))
	for _, current := range kept {
		for index := range nodes {
			if nodes[index] == current {
				positions = append(positions, index)
			}
		}
	}
	return positions
}

func TestDropConflictsKeepsTheStrongerOfNodesThatCross(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		nodes []*node
		want  []int
	}{
		{
			name:  "nested nodes are all kept",
			nodes: []*node{spanNode(0, 20, priorityClause), spanNode(5, 8, priorityLeaf), spanNode(2, 10, priorityColumn)},
			want:  []int{0, 1, 2},
		},
		{
			name:  "a leaf crossing an output item is kept",
			nodes: []*node{spanNode(0, 10, priorityColumn), spanNode(8, 12, priorityLeaf)},
			want:  []int{1},
		},
		{
			name:  "a clause crossing an output item is kept",
			nodes: []*node{spanNode(0, 10, priorityColumn), spanNode(5, 15, priorityClause)},
			want:  []int{1},
		},
		{
			name:  "the first of two crossing nodes of equal priority is kept",
			nodes: []*node{spanNode(0, 10, priorityColumn), spanNode(5, 15, priorityColumn)},
			want:  []int{0},
		},
		{
			name:  "a node is left out only for a stronger node that stays",
			nodes: []*node{spanNode(0, 10, priorityColumn), spanNode(5, 15, priorityClause), spanNode(12, 20, priorityLeaf)},
			want:  []int{0, 2},
		},
		{
			name:  "nodes with the same span are both kept",
			nodes: []*node{spanNode(3, 5, priorityLeaf), spanNode(3, 5, priorityColumn)},
			want:  []int{0, 1},
		},
		{
			name:  "a place inside a leaf is left out",
			nodes: []*node{spanNode(0, 10, priorityLeaf), placeNode(5)},
			want:  []int{0},
		},
		{
			name:  "a place inside an output item is left out",
			nodes: []*node{placeNode(5), spanNode(0, 10, priorityColumn)},
			want:  []int{1},
		},
		{
			name:  "a place inside a clause is kept",
			nodes: []*node{spanNode(0, 10, priorityClause), placeNode(5)},
			want:  []int{0, 1},
		},
		{
			name:  "a place at the edge of a leaf is kept",
			nodes: []*node{spanNode(0, 10, priorityLeaf), placeNode(0), placeNode(10)},
			want:  []int{0, 1, 2},
		},
		{
			name:  "a place inside a leaf that is left out is kept",
			nodes: []*node{spanNode(0, 10, priorityLeaf), spanNode(5, 15, priorityLeaf), placeNode(12)},
			want:  []int{0, 2},
		},
		{
			name:  "no nodes",
			nodes: nil,
			want:  []int{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, keptPositions(testCase.nodes, dropConflicts(testCase.nodes)))
		})
	}
}

func TestDropConflictsScalesWithTheNumberOfNodes(t *testing.T) {
	t.Parallel()

	template := func(items int) []*node {
		nodes := make([]*node, 0, 1+4*items)
		nodes = append(nodes, spanNode(0, 8*items, priorityClause))
		for index := range items {
			start := 8 * index
			nodes = append(nodes,
				spanNode(start, start+6, priorityColumn),
				spanNode(start+1, start+3, priorityLeaf),
				placeNode(start+2),
				spanNode(start+4, start+8, priorityColumn),
			)
		}
		return nodes
	}
	smaller := template(1_000)
	larger := template(4 * 1_000)

	kept := dropConflicts(smaller)
	require.Len(t, kept, 1+2*1_000)
	scaling.Linear(t, func() { dropConflicts(smaller) }, func() { dropConflicts(larger) })
}
