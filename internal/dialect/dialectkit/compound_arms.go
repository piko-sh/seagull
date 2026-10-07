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
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
)

// gatheredLists is the number of lists a Gathered holds and an ArmMark counts.
const gatheredLists = 4

// Gathered holds parameter references, derived tables, table functions and predicate
// subqueries collected across all compound-query arms in encounter order. Parsers pass
// these statement-wide lists to SplitCompoundArms to assign each item to its arm.
type Gathered struct {
	// Parameters holds the parameter references.
	Parameters []ast.RawParameterReference

	// DerivedTables holds the derived tables of FROM clauses.
	DerivedTables []ast.RawDerivedTableReference

	// TableFunctions holds the table function references of FROM clauses.
	TableFunctions []ast.RawTableValuedFunctionReference

	// PredicateSubqueries holds the subqueries of predicates.
	PredicateSubqueries []*ast.RawQueryAnalysis
}

// Mark records how much has been gathered.
//
// Returns ArmMark which holds the counts.
func (gathered Gathered) Mark() ArmMark {
	return ArmMark{
		parameters:          len(gathered.Parameters),
		derivedTables:       len(gathered.DerivedTables),
		tableFunctions:      len(gathered.TableFunctions),
		predicateSubqueries: len(gathered.PredicateSubqueries),
	}
}

// ArmMark is how much a parser had gathered at one point of a query.
type ArmMark struct {
	// parameters is the number of parameter references gathered.
	parameters int

	// derivedTables is the number of derived tables gathered.
	derivedTables int

	// tableFunctions is the number of table function references gathered.
	tableFunctions int

	// predicateSubqueries is the number of predicate subqueries gathered.
	predicateSubqueries int
}

// ArmExtent is the part of what a parser gathered that one arm of a compound query holds:
// what it gathered between the arm's start and its end.
type ArmExtent struct {
	// Start is the mark taken when the arm started.
	Start ArmMark

	// End is the mark taken when the arm ended.
	End ArmMark
}

// SplitCompoundArms gives each arm of a compound query the parameter references, derived
// tables, table function references and predicate subqueries gathered while it was
// parsed, so a relation one arm reads never joins another arm's FROM clause.
//
// The query itself, which is the first arm, keeps every parameter reference of the
// statement, the statement-wide list the analyser types, and what was gathered outside
// the other arms, such as in its own FROM clause or in an ORDER BY after the last arm.
// Each other arm is given its read-only flag and argument names.
//
// The extents are checked before anything changes: each arm must start no later than it
// ends, end within what was gathered, and start no earlier than the arm before it ends.
// Extents that do not fit leave the analysis unchanged.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query, whose CompoundBranches are
// the other arms.
// Takes gathered (Gathered) which holds everything gathered for the query.
// Takes extents ([]ArmExtent) which holds, parallel to analysis.CompoundBranches, the
// part each arm gathered, as marks taken before and after parsing it.
//
// Returns error which wraps ErrArmExtent and names the arm when the extents do not fit
// what was gathered, or nil.
func SplitCompoundArms(analysis *ast.RawQueryAnalysis, gathered Gathered, extents []ArmExtent) error {
	if err := checkArmExtents(gathered.Mark(), extents); err != nil {
		return err
	}
	analysis.ParameterReferences = gathered.Parameters
	for index := range analysis.CompoundBranches {
		arm := analysis.CompoundBranches[index].Query
		if arm == nil || index >= len(extents) {
			continue
		}
		start, end := extents[index].Start, extents[index].End
		arm.ReadOnly = analysis.ReadOnly
		arm.ArgumentNames = analysis.ArgumentNames
		arm.ParameterReferences = slices.Clone(gathered.Parameters[start.parameters:end.parameters])
		arm.RawDerivedTables = slices.Clone(gathered.DerivedTables[start.derivedTables:end.derivedTables])
		arm.RawTableValuedFunctions = slices.Clone(gathered.TableFunctions[start.tableFunctions:end.tableFunctions])
		arm.PredicateSubqueries = slices.Clone(gathered.PredicateSubqueries[start.predicateSubqueries:end.predicateSubqueries])
	}
	analysis.RawDerivedTables = outsideArms(gathered.DerivedTables, extents, func(mark ArmMark) int { return mark.derivedTables })
	analysis.RawTableValuedFunctions = outsideArms(gathered.TableFunctions, extents,
		func(mark ArmMark) int { return mark.tableFunctions })
	analysis.PredicateSubqueries = outsideArms(gathered.PredicateSubqueries, extents,
		func(mark ArmMark) int { return mark.predicateSubqueries })
	return nil
}

// armMarkCounts lists the counts a mark holds, one per gathered list, in a fixed order.
//
// Takes mark (ArmMark) which is the mark.
//
// Returns [gatheredLists]int which holds its counts of parameters, derived tables, table
// functions and predicate subqueries.
func armMarkCounts(mark ArmMark) [gatheredLists]int {
	return [gatheredLists]int{mark.parameters, mark.derivedTables, mark.tableFunctions, mark.predicateSubqueries}
}

// checkArmExtents reports extents that do not fit what was gathered: a count below zero
// or past the end of its list, an arm that ends before it starts, or an arm that starts
// before the one before it ends.
//
// Takes total (ArmMark) which holds how much was gathered in all.
// Takes extents ([]ArmExtent) which holds the part each arm gathered, in order.
//
// Returns error which wraps ErrArmExtent and names the first arm that does not fit, or
// nil.
func checkArmExtents(total ArmMark, extents []ArmExtent) error {
	limits := armMarkCounts(total)
	var previousEnd [gatheredLists]int
	for index, extent := range extents {
		starts, ends := armMarkCounts(extent.Start), armMarkCounts(extent.End)
		for list := range limits {
			if starts[list] < previousEnd[list] || starts[list] > ends[list] || ends[list] > limits[list] {
				return fmt.Errorf("%w: compound branch %d of %d", ErrArmExtent, index+1, len(extents))
			}
		}
		previousEnd = ends
	}
	return nil
}

// outsideArms returns the entries of a gathered list that no arm holds.
//
// Takes entries ([]T) which is the list.
// Takes extents ([]ArmExtent) which holds the part each arm gathered, in order, already
// checked to fit the list.
// Takes count (func(ArmMark) int) which reads the list's count from a mark.
//
// Returns []T which holds the other entries, in order, or nil when there are none.
func outsideArms[T any](entries []T, extents []ArmExtent, count func(ArmMark) int) []T {
	var kept []T
	next := 0
	for _, extent := range extents {
		kept = append(kept, entries[next:count(extent.Start)]...)
		next = count(extent.End)
	}
	return append(kept, entries[next:]...)
}
