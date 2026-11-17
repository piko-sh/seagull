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

package fuzzsemantic

import (
	"slices"
	"strconv"
)

const (
	// itemColumns is the item projection's column count.
	itemColumns = 5

	// aggregateWidth is the aggregate projection's column count.
	aggregateWidth = 6

	// windowColumns is the window query's column count.
	windowColumns = 7

	// limitedCalls is the number of calls each limited builder makes.
	limitedCalls = 3
)

const (
	// ClassNative is a failure the generated call returned from the engine or driver.
	ClassNative = "native"

	// ClassBuilder is a refusal the generated runtime builder returned.
	ClassBuilder = "builder"

	// ClassMissingMethod reports a builder without the called method.
	ClassMissingMethod = "missing-method"
)

const (
	// labelWhere reads the rows the predicate holds for.
	labelWhere = "where"

	// labelNot reads the rows the predicate fails for.
	labelNot = "not"

	// labelNull reads the rows the predicate is unknown for.
	labelNull = "null"

	// labelAggregateWhere aggregates the rows the predicate holds for.
	labelAggregateWhere = "aggregate-where"

	// labelAggregateNot aggregates the rows the predicate fails for.
	labelAggregateNot = "aggregate-not"

	// labelAggregateNull aggregates the rows the predicate is unknown for.
	labelAggregateNull = "aggregate-null"

	// labelCommonCount counts the predicate's rows through a CTE.
	labelCommonCount = "cte-count"

	// labelDerivedCount counts the predicate's failures through a derived table.
	labelDerivedCount = "derived-count"

	// labelAll reads every item.
	labelAll = "all"

	// labelAggregateAll aggregates every item.
	labelAggregateAll = "aggregate-all"

	// labelWindow reads the window functions.
	labelWindow = "window"

	// labelGroupSum reads the group totals.
	labelGroupSum = "group-sum"

	// labelWindowFull reads the window builder unfiltered.
	labelWindowFull = "window-builder"

	// labelWindowFiltered reads the window builder with a condition.
	labelWindowFiltered = "window-builder-filtered"

	// labelLeftJoin reads the LEFT JOIN.
	labelLeftJoin = "left-join"

	// labelInnerCount counts the inner join.
	labelInnerCount = "inner-count"

	// labelAntiCount counts items without peers.
	labelAntiCount = "anti-count"

	// labelFullJoin reads the FULL JOIN.
	labelFullJoin = "full-join"

	// labelOrphanCount counts peers without items.
	labelOrphanCount = "orphan-count"

	// labelSemiIn reads the IN semi-join.
	labelSemiIn = "semi-in"

	// labelSemiExists reads the EXISTS semi-join.
	labelSemiExists = "semi-exists"

	// labelSemiJoin reads the DISTINCT join semi-join.
	labelSemiJoin = "semi-join"

	// labelBranchItems reads the first branch.
	labelBranchItems = "branch-items"

	// labelBranchPeers reads the second branch.
	labelBranchPeers = "branch-peers"

	// labelUnionAll reads UNION ALL.
	labelUnionAll = "union-all"

	// labelUnion reads UNION.
	labelUnion = "union"

	// labelUnionDistinct reads DISTINCT over UNION ALL.
	labelUnionDistinct = "union-distinct"

	// labelIntersect reads INTERSECT.
	labelIntersect = "intersect"

	// labelExcept reads EXCEPT.
	labelExcept = "except"

	// labelPageFull reads the static ordered rows.
	labelPageFull = "page-full"

	// labelPage prefixes a static page's offset.
	labelPage = "page/"

	// labelBuilderFull reads the builder's ordered rows.
	labelBuilderFull = "builder-full"

	// labelBuilderCount counts the builder's rows.
	labelBuilderCount = "builder-count"

	// labelBuilderPage prefixes a builder page's offset.
	labelBuilderPage = "builder-page/"

	// labelBuilderWhere reads the builder with a condition.
	labelBuilderWhere = "builder-where"

	// labelStaticWhere reads the static query the builder condition matches.
	labelStaticWhere = "static-where"

	// labelIntersectDistinct reads ClickHouse's INTERSECT DISTINCT.
	labelIntersectDistinct = "intersect-distinct"

	// labelExceptDistinct reads ClickHouse's EXCEPT DISTINCT.
	labelExceptDistinct = "except-distinct"

	// labelLimitFull reads the limited builder unfiltered.
	labelLimitFull = "limit-builder"

	// labelLimitWhere reads the limited builder with a condition.
	labelLimitWhere = "limit-builder-where"

	// labelLimitCount counts the limited builder's rows with a condition.
	labelLimitCount = "limit-builder-count"

	// labelLimitByFull reads the LIMIT n BY builder unfiltered.
	labelLimitByFull = "limit-by-builder"

	// labelLimitByWhere reads the LIMIT n BY builder with a condition.
	labelLimitByWhere = "limit-by-builder-where"

	// labelLimitByCount counts the LIMIT n BY builder's rows with a condition.
	labelLimitByCount = "limit-by-builder-count"
)

const (
	// stepWhere adds a builder condition.
	stepWhere = "Where"

	// stepOrder adds a builder ordering.
	stepOrder = "OrderBy"

	// stepLimit sets the builder limit.
	stepLimit = "Limit"

	// stepOffset sets the builder offset.
	stepOffset = "Offset"

	// directionAscending orders from the smallest key.
	directionAscending = "ASC"

	// directionDescending orders from the largest key.
	directionDescending = "DESC"
)

// widths records each call's column count, with pages under their prefixes.
var widths = map[string]int{
	labelWhere: itemColumns, labelNot: itemColumns, labelNull: itemColumns, labelAll: itemColumns, labelStaticWhere: itemColumns,
	labelAggregateWhere: aggregateWidth, labelAggregateNot: aggregateWidth, labelAggregateNull: aggregateWidth, labelAggregateAll: aggregateWidth,
	labelCommonCount: 1, labelDerivedCount: 1, labelInnerCount: 1, labelAntiCount: 1, labelOrphanCount: 1,
	labelWindow: windowColumns, labelGroupSum: 3, labelWindowFull: 3, labelWindowFiltered: 3, labelLeftJoin: 3, labelFullJoin: 2,
	labelSemiIn: 1, labelSemiExists: 1, labelSemiJoin: 1, labelBranchItems: 1, labelBranchPeers: 1, labelUnionAll: 1, labelUnion: 1,
	labelUnionDistinct: 1, labelIntersect: 1, labelExcept: 1, labelPageFull: 3, labelPage: 3, labelBuilderFull: 3, labelBuilderCount: 0,
	labelBuilderPage: 3, labelBuilderWhere: 3, labelIntersectDistinct: 1, labelExceptDistinct: 1, labelLimitFull: 3, labelLimitWhere: 3,
	labelLimitCount: 0, labelLimitByFull: 3, labelLimitByWhere: 3, labelLimitByCount: 0,
}

// limitedBuilder names the calls of one builder over a query that limits its own rows.
type limitedBuilder struct {
	// method names the builder constructor.
	method string

	// full labels the unfiltered read.
	full string

	// where labels the read with a condition.
	where string

	// count labels the count with the same condition.
	count string
}

// Call is one generated method call with its arguments.
type Call struct {
	// Label identifies the call to the relations.
	Label string

	// Method is the generated method name.
	Method string

	// Arguments holds the method's parameters in declaration order.
	Arguments []int64

	// Steps holds builder calls, and is nil for a static method.
	Steps []BuilderStep

	// Width is the number of columns each returned row holds.
	Width int

	// Dynamic calls the method's runtime builder.
	Dynamic bool

	// Count runs the builder's Count terminal instead of All.
	Count bool
}

// BuilderStep is one runtime builder call before the terminal.
type BuilderStep struct {
	// Name is the builder method.
	Name string

	// Column is the Where or OrderBy column.
	Column string

	// Operator is the Where operator.
	Operator string

	// Direction is the OrderBy direction.
	Direction string

	// Value is the Where value.
	Value int64

	// Number is the Limit or Offset argument.
	Number int
}

// Cell is one result value in canonical text.
type Cell struct {
	// Text holds the value, integers in canonical decimal.
	Text string

	// Null reports SQL NULL.
	Null bool
}

// Observation records what one call returned.
type Observation struct {
	// Label identifies the call.
	Label string

	// Class classifies a failure, empty for success.
	Class string

	// Error holds the failure text.
	Error string

	// Rows holds the rows of a read.
	Rows [][]Cell

	// Count holds the Count terminal's result.
	Count int64
}

// Calls lists the generated calls the case's relations observe, in order.
//
// Returns []Call which holds each call once.
func (c Case) Calls() []Call {
	var calls []Call
	seen := map[string]bool{}
	add := func(call Call) {
		if !seen[call.Label] {
			seen[call.Label] = true
			calls = append(calls, call)
		}
	}
	suffix := strconv.Itoa(c.Predicate)
	parameter := []int64{c.Parameter}
	if c.enabled(featurePivot) || c.enabled(featurePartitionRows) || c.enabled(featurePartitionAggregates) || c.enabled(featureSubqueryCounts) {
		add(static(labelWhere, "SemWhere"+suffix, parameter))
		add(static(labelNot, "SemNot"+suffix, parameter))
		add(static(labelNull, "SemNull"+suffix, parameter))
	}
	if c.enabled(featurePartitionRows) || c.enabled(featureWindow) || c.enabled(featureLeftJoin) {
		add(static(labelAll, methodAll, nil))
	}
	if c.enabled(featurePartitionAggregates) {
		add(static(labelAggregateWhere, "SemAggWhere"+suffix, parameter))
		add(static(labelAggregateNot, "SemAggNot"+suffix, parameter))
		add(static(labelAggregateNull, "SemAggNull"+suffix, parameter))
		add(static(labelAggregateAll, methodAggregateAll, nil))
	}
	if c.enabled(featureSubqueryCounts) {
		add(static(labelCommonCount, "SemCteCount"+suffix, parameter))
		add(static(labelDerivedCount, "SemDerivedCount"+suffix, parameter))
	}
	if c.enabled(featureWindow) {
		add(static(labelWindow, methodWindow, nil))
		add(static(labelGroupSum, methodGroupSum, nil))
	}
	if c.enabled(featureWindowFilter) {
		add(builder(labelWindowFull, methodWindowBuilder, nil))
		add(builder(labelWindowFiltered, methodWindowBuilder, []BuilderStep{condition("id", ">", c.Parameter)}))
	}
	for _, call := range slices.Concat(c.joinCalls(), c.compoundCalls(), c.pageCalls(), c.limitCalls()) {
		add(call)
	}
	return calls
}

// joinCalls lists the join relations' calls.
//
// Returns []Call which holds the enabled join and semi-join formulations.
func (c Case) joinCalls() []Call {
	var calls []Call
	if c.enabled(featureLeftJoin) || c.enabled(featureFullJoin) {
		calls = append(calls, static(labelLeftJoin, methodLeftJoin, nil))
	}
	if c.enabled(featureLeftJoin) {
		calls = append(calls, static(labelInnerCount, methodInnerCount, nil), static(labelAntiCount, methodAntiCount, nil))
	}
	if c.enabled(featureFullJoin) {
		calls = append(calls, static(labelFullJoin, methodFullJoin, nil), static(labelOrphanCount, methodOrphanCount, nil))
	}
	parameter := []int64{c.Parameter}
	if c.enabled(featureSemiJoin) {
		calls = append(calls, static(labelSemiIn, methodSemiIn, parameter), static(labelSemiJoin, methodSemiJoin, parameter))
	}
	if c.enabled(featureSemiJoin) && c.enabled(featureCorrelated) {
		calls = append(calls, static(labelSemiExists, methodSemiExists, parameter))
	}
	return calls
}

// compoundCalls lists the compound relations' calls.
//
// Returns []Call which holds the branches and their enabled combinations.
func (c Case) compoundCalls() []Call {
	if !c.enabled(featureUnionAll) && !c.enabled(featureUnion) && !c.enabled(featureSetOperations) {
		return nil
	}
	calls := []Call{static(labelBranchItems, methodBranchItems, nil), static(labelBranchPeers, methodBranchPeers, nil)}
	if c.enabled(featureUnionAll) {
		calls = append(calls, static(labelUnionAll, methodUnionAll, nil))
	}
	if c.enabled(featureUnion) {
		calls = append(calls, static(labelUnion, methodUnion, nil), static(labelUnionDistinct, methodUnionDistinct, nil))
	}
	if c.enabled(featureSetOperations) {
		calls = append(calls, static(labelIntersect, methodIntersect, nil), static(labelExcept, methodExcept, nil))
	}
	if c.enabled(featureSetOperations) && c.Engine == engineClickHouse {
		calls = append(calls, static(labelIntersectDistinct, methodIntersectDistinct, nil), static(labelExceptDistinct, methodExceptDistinct, nil))
	}
	return calls
}

// setLabels lists the set operation relation's calls.
//
// Returns []string which holds the branches, the bare forms and, on ClickHouse, the
// DISTINCT forms.
func (c *Case) setLabels() []string {
	labels := []string{labelBranchItems, labelBranchPeers, labelIntersect, labelExcept}
	if c.Engine == engineClickHouse {
		labels = append(labels, labelIntersectDistinct, labelExceptDistinct)
	}
	return labels
}

// limitedBuilders lists the builders over queries that limit their own rows.
//
// Returns []limitedBuilder which holds the LIMIT and OFFSET builder and, on ClickHouse,
// the LIMIT n BY builder.
func (c *Case) limitedBuilders() []limitedBuilder {
	builders := []limitedBuilder{{method: methodLimitBuilder, full: labelLimitFull, where: labelLimitWhere, count: labelLimitCount}}
	if c.Engine == engineClickHouse {
		builders = append(builders, limitedBuilder{method: methodLimitByBuilder, full: labelLimitByFull, where: labelLimitByWhere, count: labelLimitByCount})
	}
	return builders
}

// limitLabels lists the limited builder relation's calls.
//
// Returns []string which holds every read and count of each limited builder.
func (c *Case) limitLabels() []string {
	builders := c.limitedBuilders()
	labels := make([]string, 0, len(builders)*limitedCalls)
	for _, limited := range builders {
		labels = append(labels, limited.full, limited.where, limited.count)
	}
	return labels
}

// limitCalls lists the limited builder relation's calls.
//
// Returns []Call which reads each limited builder without and with Where(a > k), and
// counts it with the condition.
func (c Case) limitCalls() []Call {
	if !c.enabled(featureBuilderLimit) {
		return nil
	}
	var calls []Call
	for _, limited := range c.limitedBuilders() {
		steps := []BuilderStep{condition("a", ">", c.Parameter)}
		counted := builder(limited.count, limited.method, steps)
		counted.Count = true
		calls = append(calls, builder(limited.full, limited.method, nil), builder(limited.where, limited.method, steps), counted)
	}
	return calls
}

// pageCalls lists the pagination relations' calls.
//
// Returns []Call which holds the ordered reads and every page up to one past the rows.
func (c Case) pageCalls() []Call {
	var calls []Call
	if c.enabled(featureStaticPages) {
		calls = append(calls, static(labelPageFull, methodPageFull, nil))
		for _, offset := range c.offsets() {
			page := static(labelPage+strconv.Itoa(offset), methodPage, []int64{int64(c.PageSize), int64(offset)})
			page.Width = widths[labelPage]
			calls = append(calls, page)
		}
	}
	if !c.enabled(featureBuilderPages) {
		return calls
	}
	direction := directionAscending
	if c.Descending {
		direction = directionDescending
	}
	orders := []BuilderStep{order("a", direction), order("id", direction)}
	calls = append(calls, builder(labelBuilderFull, methodBuilder, orders))
	counted := builder(labelBuilderCount, methodBuilder, nil)
	counted.Count = true
	calls = append(calls, counted)
	for _, offset := range c.offsets() {
		steps := append(slices.Clone(orders), BuilderStep{Name: stepLimit, Column: "", Operator: "", Direction: "", Value: 0, Number: c.PageSize},
			BuilderStep{Name: stepOffset, Column: "", Operator: "", Direction: "", Value: 0, Number: offset})
		page := builder(labelBuilderPage+strconv.Itoa(offset), methodBuilder, steps)
		page.Width = widths[labelBuilderPage]
		calls = append(calls, page)
	}
	calls = append(calls, builder(labelBuilderWhere, methodBuilder, []BuilderStep{condition("a", ">", c.Parameter), order("id", directionAscending)}))
	return append(calls, static(labelStaticWhere, "SemWhere0", []int64{c.Parameter}))
}

// offsets lists the page offsets, ending with the first page past the last row.
//
// Returns []int which holds multiples of the page size.
func (c Case) offsets() []int {
	var result []int
	for offset := 0; offset <= len(c.Items); offset += c.PageSize {
		result = append(result, offset)
	}
	return result
}

// static builds a call of a static method.
//
// Takes label (string) which identifies the call.
// Takes method (string) which names the generated method.
// Takes arguments ([]int64) which holds its parameters.
//
// Returns Call which runs the method once.
func static(label, method string, arguments []int64) Call {
	return Call{Label: label, Method: method, Arguments: arguments, Steps: nil, Width: widths[label], Dynamic: false, Count: false}
}

// builder builds a call of a runtime builder's All terminal.
//
// Takes label (string) which identifies the call.
// Takes method (string) which names the builder constructor.
// Takes steps ([]BuilderStep) which holds the calls before the terminal.
//
// Returns Call which runs the builder once.
func builder(label, method string, steps []BuilderStep) Call {
	return Call{Label: label, Method: method, Arguments: nil, Steps: steps, Width: widths[label], Dynamic: true, Count: false}
}

// condition builds a builder Where step.
//
// Takes column (string) which names the output column.
// Takes operator (string) which is the comparison.
// Takes value (int64) which is the compared value.
//
// Returns BuilderStep which adds the condition.
func condition(column, operator string, value int64) BuilderStep {
	return BuilderStep{Name: stepWhere, Column: column, Operator: operator, Direction: "", Value: value, Number: 0}
}

// order builds a builder OrderBy step.
//
// Takes column (string) which names the output column.
// Takes direction (string) which is ASC or DESC.
//
// Returns BuilderStep which adds the ordering.
func order(column, direction string) BuilderStep {
	return BuilderStep{Name: stepOrder, Column: column, Operator: "", Direction: direction, Value: 0, Number: 0}
}
