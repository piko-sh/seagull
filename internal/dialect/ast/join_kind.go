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

// JoinSides is a set of the sides of a join.
type JoinSides uint8

const (
	// JoinSideLeft is the left side of a join: the rows of the tables the join follows.
	JoinSideLeft JoinSides = 1 << iota

	// JoinSideRight is the right side of a join: the rows of the table it joins.
	JoinSideRight
)

// Includes reports whether the set holds every side of another set.
//
// Takes side (JoinSides) which is the side or sides to look for.
//
// Returns bool which is true when side is not empty and every side of it is in the set.
func (sides JoinSides) Includes(side JoinSides) bool {
	return side != 0 && sides&side == side
}

// JoinKind describes how a join preserves rows from its two sides.
//
// The analyser uses those properties to determine nullability, allowing dialect-specific
// joins to share the same resolution rules.
type JoinKind struct {
	// Keyword is the join as the dialect writes it, such as LEFT or ASOF LEFT. It is kept
	// for display only; the analyser never reads it.
	Keyword string

	// Nullable is the set of sides whose columns may be NULL in a result row: a side the
	// join can leave without a matching row while keeping the row of the other side.
	Nullable JoinSides

	// Filters reports whether the join drops a row of either side that has no match on the
	// other, as an inner or a semi join does, rather than keeping or padding it.
	Filters bool
}

var (
	// JoinInner is an inner join: a row of either side without a match is dropped, and
	// neither side's columns become nullable.
	JoinInner = JoinKind{Keyword: "INNER", Nullable: 0, Filters: true}

	// JoinLeft is a left outer join: every left row is kept, and the right side's columns
	// are NULL where it has no match.
	JoinLeft = JoinKind{Keyword: "LEFT", Nullable: JoinSideRight, Filters: false}

	// JoinRight is a right outer join: every right row is kept, and the left side's columns
	// are NULL where it has no match.
	JoinRight = JoinKind{Keyword: "RIGHT", Nullable: JoinSideLeft, Filters: false}

	// JoinFull is a full outer join: every row of both sides is kept, and either side's
	// columns are NULL where it has no match.
	JoinFull = JoinKind{Keyword: "FULL", Nullable: JoinSideLeft | JoinSideRight, Filters: false}

	// JoinCross is a cross join: every pairing of the two sides, with no condition to drop a
	// row and no side made nullable.
	JoinCross = JoinKind{Keyword: "CROSS", Nullable: 0, Filters: false}
)

// String returns the join's keyword, as the dialect writes it.
//
// Returns string which is the keyword.
func (kind JoinKind) String() string {
	return kind.Keyword
}
