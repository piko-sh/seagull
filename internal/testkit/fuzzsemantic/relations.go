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
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	// KindNativeError marks a call the engine, driver or builder refused.
	KindNativeError = "native-error"

	// KindMismatch marks a relation the results break.
	KindMismatch = "semantic-mismatch"

	// KindHarness marks observations that do not answer the calls.
	KindHarness = "harness"
)

const (
	// aggregateSum is the SUM column, after the two counts.
	aggregateSum = 2

	// aggregateMinimum is the MIN column.
	aggregateMinimum = 3

	// windowNumber is the window query's row_number column.
	windowNumber = 2

	// windowCount is the window query's partition count column.
	windowCount = 3

	// windowRunning is the window query's running total column.
	windowRunning = 4

	// windowTotal is the window query's partition total column.
	windowTotal = 5

	// windowPrevious is the window query's LAG column.
	windowPrevious = 6

	// pageKey is the page rows' ordering column.
	pageKey = 2

	// pageWidth is the page rows' column count.
	pageWidth = 3
)

// Problem is one failed call or broken relation.
type Problem struct {
	// Kind separates a refused call from a broken relation.
	Kind string

	// Relation names the relation or the failed call.
	Relation string

	// Detail describes the disagreement.
	Detail string
}

// results indexes the successful observations of one input.
type results map[string]*Observation

// relationCheck checks one relation family.
type relationCheck struct {
	// check returns the disagreements, empty when the relation holds.
	check func(c *Case, observed results) []string

	// labels lists the calls the relation reads.
	labels func(c *Case) []string

	// feature names the relation.
	feature string
}

// relationChecks lists the relation families in report order.
var relationChecks = []relationCheck{
	{feature: featurePivot, check: checkPivot, labels: fixedLabels(labelWhere, labelNot, labelNull)},
	{feature: featurePartitionRows, check: checkPartitionRows, labels: fixedLabels(labelWhere, labelNot, labelNull, labelAll)},
	{feature: featurePartitionAggregates, check: checkPartitionAggregates,
		labels: fixedLabels(labelWhere, labelNot, labelNull, labelAggregateWhere, labelAggregateNot, labelAggregateNull, labelAggregateAll)},
	{feature: featureSubqueryCounts, check: checkSubqueryCounts, labels: fixedLabels(labelWhere, labelNot, labelCommonCount, labelDerivedCount)},
	{feature: featureWindow, check: checkWindow, labels: fixedLabels(labelAll, labelWindow, labelGroupSum)},
	{feature: featureWindowFilter, check: checkWindowFilter, labels: fixedLabels(labelWindowFull, labelWindowFiltered)},
	{feature: featureLeftJoin, check: checkLeftJoin, labels: fixedLabels(labelAll, labelLeftJoin, labelInnerCount, labelAntiCount)},
	{feature: featureFullJoin, check: checkFullJoin, labels: fixedLabels(labelLeftJoin, labelFullJoin, labelOrphanCount)},
	{feature: featureSemiJoin, check: checkSemiJoin, labels: fixedLabels(labelSemiIn, labelSemiJoin, labelSemiExists)},
	{feature: featureUnionAll, check: checkUnionAll, labels: fixedLabels(labelBranchItems, labelBranchPeers, labelUnionAll)},
	{feature: featureUnion, check: checkUnion, labels: fixedLabels(labelBranchItems, labelBranchPeers, labelUnion, labelUnionDistinct)},
	{feature: featureSetOperations, check: checkSetOperations, labels: (*Case).setLabels},
	{feature: featureStaticPages, check: checkStaticPages, labels: func(c *Case) []string { return c.pageLabels(labelPageFull, labelPage) }},
	{feature: featureBuilderPages, check: checkBuilderPages, labels: func(c *Case) []string {
		return append(c.pageLabels(labelBuilderFull, labelBuilderPage), labelBuilderCount, labelBuilderWhere, labelStaticWhere)
	}},
	{feature: featureBuilderLimit, check: checkBuilderLimit, labels: (*Case).limitLabels},
}

// aggregateColumns names the aggregate query's columns.
var aggregateColumns = []string{"COUNT(*)", "COUNT(a)", "SUM(a)", "MIN(a)", "MAX(a)", "MAX(label)"}

// windowGroup collects one partition's window rows.
type windowGroup struct {
	// ids holds the identifiers in identifier order.
	ids []int64

	// numbers holds the row numbers.
	numbers []int64

	// rows holds the window rows in identifier order.
	rows [][]Cell
}

// String describes the problem for failure messages.
//
// Returns string which names the kind, the relation and the detail.
func (p Problem) String() string {
	return p.Kind + " " + p.Relation + ": " + p.Detail
}

// Problems checks every enabled relation against the observed calls.
//
// Takes observations ([]Observation) which holds one observation per call, in order.
//
// Returns []Problem which lists refused calls and broken relations, empty when none.
func (c Case) Problems(observations []Observation) []Problem {
	calls := c.Calls()
	if len(observations) != len(calls) {
		return []Problem{{Kind: KindHarness, Relation: "calls", Detail: fmt.Sprintf("observed %d calls, expected %d", len(observations), len(calls))}}
	}
	var problems []Problem
	observed := results{}
	failed := map[string]bool{}
	for index := range observations {
		observation := &observations[index]
		if observation.Label != calls[index].Label {
			return []Problem{{Kind: KindHarness, Relation: "calls", Detail: fmt.Sprintf("observation %d answers %q, expected %q", index, observation.Label, calls[index].Label)}}
		}
		if observation.Class != "" {
			failed[observation.Label] = true
			problems = append(problems, Problem{Kind: KindNativeError, Relation: calls[index].Method + " (" + observation.Label + ")",
				Detail: observation.Class + ": " + observation.Error})
			continue
		}
		if row := wrongWidth(observation.Rows, calls[index].Width); row != nil {
			failed[observation.Label] = true
			problems = append(problems, Problem{Kind: KindMismatch, Relation: calls[index].Method + " (" + observation.Label + ")",
				Detail: fmt.Sprintf("row %s does not hold %d columns", describeRow(row), calls[index].Width)})
			continue
		}
		observed[observation.Label] = observation
	}
	for _, relation := range relationChecks {
		if !c.enabled(relation.feature) || slices.ContainsFunc(relation.labels(&c), func(label string) bool { return failed[label] }) {
			continue
		}
		for _, detail := range relation.check(&c, observed) {
			problems = append(problems, Problem{Kind: KindMismatch, Relation: relation.feature, Detail: detail})
		}
	}
	return problems
}

// pageLabels lists a pagination relation's calls.
//
// Takes full (string) which labels the unpaginated read.
// Takes prefix (string) which prefixes each page's offset.
//
// Returns []string which holds the full read and every page.
func (c *Case) pageLabels(full, prefix string) []string {
	offsets := c.offsets()
	labels := make([]string, 0, 1+len(offsets))
	labels = append(labels, full)
	for _, offset := range offsets {
		labels = append(labels, prefix+strconv.Itoa(offset))
	}
	return labels
}

// wrongWidth finds a row of another width.
//
// Takes rows ([][]Cell) which holds the rows.
// Takes width (int) which is the expected column count.
//
// Returns []Cell which is the first row of another width, nil when none.
func wrongWidth(rows [][]Cell, width int) []Cell {
	for _, row := range rows {
		if len(row) != width {
			return row
		}
	}
	return nil
}

// fixedLabels returns a label list that does not depend on the case.
//
// Takes labels (...string) which lists the calls.
//
// Returns func(*Case) []string which returns them.
func fixedLabels(labels ...string) func(*Case) []string {
	return func(*Case) []string { return labels }
}

// checkPivot requires the pivot in the partition its predicate value names, and only
// there.
//
// Takes c (*Case) which holds the pivot and the predicate.
// Takes observed (results) which holds the partitions.
//
// Returns []string which describes the disagreements.
func checkPivot(c *Case, observed results) []string {
	if len(c.Items) == 0 {
		return nil
	}
	pivot := c.Items[c.Pivot]
	expected := map[truth]string{truthTrue: labelWhere, truthFalse: labelNot, truthUnknown: labelNull}[c.truthOf(pivot)]
	row := itemCells(pivot)
	var problems []string
	for _, label := range []string{labelWhere, labelNot, labelNull} {
		count := 0
		for _, candidate := range observed[label].Rows {
			if rowKey(candidate) == rowKey(row) {
				count++
			}
		}
		switch {
		case label == expected && count != 1:
			problems = append(problems, fmt.Sprintf("pivot %s must appear once in the %q partition, the model evaluates the predicate there, but it appears %d times in %s",
				describeRow(row), expected, count, describeRows(observed[label].Rows)))
		case label != expected && count != 0:
			problems = append(problems, fmt.Sprintf("pivot %s belongs to the %q partition but also appears in the %q partition", describeRow(row), expected, label))
		}
	}
	return problems
}

// checkPartitionRows requires the three partitions to make up every row exactly once.
//
// Takes observed (results) which holds the partitions and every row.
//
// Returns []string which describes the disagreement.
func checkPartitionRows(_ *Case, observed results) []string {
	parts := slices.Concat(observed[labelWhere].Rows, observed[labelNot].Rows, observed[labelNull].Rows)
	if problem := compareBags("WHERE p, WHERE NOT p and WHERE p IS NULL together", parts, "every row", observed[labelAll].Rows); problem != "" {
		return []string{problem}
	}
	return nil
}

// checkPartitionAggregates requires the partitions' aggregates to combine to the whole
// table's, and their counts to match the partitions' rows.
//
// Takes observed (results) which holds the partitions and their aggregates.
//
// Returns []string which describes the disagreements.
func checkPartitionAggregates(_ *Case, observed results) []string {
	var problems []string
	labels := []string{labelAggregateWhere, labelAggregateNot, labelAggregateNull}
	rows := map[string][]Cell{}
	for _, label := range append(slices.Clone(labels), labelAggregateAll) {
		if len(observed[label].Rows) != 1 {
			return []string{fmt.Sprintf("%s returned %s, expected one row of aggregates", label, describeRows(observed[label].Rows))}
		}
		rows[label] = observed[label].Rows[0]
	}
	for index, partition := range []string{labelWhere, labelNot, labelNull} {
		count := strconv.Itoa(len(observed[partition].Rows))
		if cell := rows[labels[index]][0]; cell.Null || cell.Text != count {
			problems = append(problems, fmt.Sprintf("%s COUNT(*) is %s but the partition holds %s rows", labels[index], describeCell(cell), count))
		}
	}
	for column, name := range aggregateColumns {
		var cells []Cell
		for _, label := range labels {
			cells = append(cells, rows[label][column])
		}
		combined, err := combine(column, cells)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s over the partitions: %v", name, err))
			continue
		}
		if whole := rows[labelAggregateAll][column]; whole != combined {
			problems = append(problems, fmt.Sprintf("%s is %s over every row but the partitions %s combine to %s", name, describeCell(whole), describeRow(cells),
				describeCell(combined)))
		}
	}
	return problems
}

// combine merges one aggregate over disjoint partitions.
//
// Takes column (int) which selects the aggregate.
// Takes cells ([]Cell) which holds each partition's value.
//
// Returns Cell which is the merged value.
// Returns error when a value is not of the aggregate's kind.
func combine(column int, cells []Cell) (Cell, error) {
	result := Cell{Text: "", Null: true}
	for _, cell := range cells {
		switch {
		case cell.Null && column < aggregateSum:
			return result, errors.New("a count is NULL")
		case cell.Null:
		case column == len(aggregateColumns)-1:
			if result.Null || cell.Text > result.Text {
				result = cell
			}
		default:
			merged, err := combineInteger(column, result, cell)
			if err != nil {
				return result, err
			}
			result = merged
		}
	}
	if result.Null && column < aggregateSum {
		return result, errors.New("no partition counted")
	}
	return result, nil
}

// combineInteger merges one integer aggregate value into the running result.
//
// Takes column (int) which selects COUNT, SUM, MIN or MAX.
// Takes result (Cell) which is the running result, NULL before the first value.
// Takes cell (Cell) which is a non-NULL partition value.
//
// Returns Cell which is the merged value.
// Returns error when the value is not an integer.
func combineInteger(column int, result, cell Cell) (Cell, error) {
	value, err := cellInteger(cell)
	if err != nil {
		return result, err
	}
	if result.Null {
		return Cell{Text: strconv.FormatInt(value, decimalBase), Null: false}, nil
	}
	current, err := cellInteger(result)
	if err != nil {
		return result, err
	}
	switch {
	case column <= aggregateSum:
		current += value
	case column == aggregateMinimum:
		current = min(current, value)
	default:
		current = max(current, value)
	}
	return Cell{Text: strconv.FormatInt(current, decimalBase), Null: false}, nil
}

// checkSubqueryCounts requires the CTE and derived table counts to match the partitions.
//
// Takes observed (results) which holds the partitions and their counts.
//
// Returns []string which describes the disagreements.
func checkSubqueryCounts(_ *Case, observed results) []string {
	var problems []string
	for _, pair := range [][2]string{{labelCommonCount, labelWhere}, {labelDerivedCount, labelNot}} {
		count, err := singleInteger(observed[pair[0]])
		if err != nil {
			problems = append(problems, pair[0]+": "+err.Error())
			continue
		}
		if count != int64(len(observed[pair[1]].Rows)) {
			problems = append(problems, fmt.Sprintf("%s counts %d rows but the %q partition holds %d", pair[0], count, pair[1], len(observed[pair[1]].Rows)))
		}
	}
	return problems
}

// checkWindow requires each group's row numbers to be a permutation, its counts and
// totals to agree with each other and with GROUP BY, its running total to end at the
// total, and LAG to name the previous identifier.
//
// Takes observed (results) which holds every row, the window rows and the group totals.
//
// Returns []string which describes the disagreements.
func checkWindow(_ *Case, observed results) []string {
	rows := observed[labelWindow].Rows
	keys, expected := make([][]Cell, 0, len(rows)), make([][]Cell, 0, len(observed[labelAll].Rows))
	for _, row := range rows {
		keys = append(keys, row[:windowNumber])
	}
	for _, row := range observed[labelAll].Rows {
		expected = append(expected, row[:windowNumber])
	}
	if problem := compareBags("window (id, grp)", keys, "every (id, grp)", expected); problem != "" {
		return []string{problem}
	}
	groups, err := windowGroups(rows)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	totals := map[string][2]Cell{}
	for name, group := range groups {
		problems = append(problems, checkWindowGroup(name, group)...)
		size := Cell{Text: strconv.Itoa(len(group.ids)), Null: false}
		totals[name] = [2]Cell{size, group.rows[0][windowTotal]}
	}
	if len(observed[labelGroupSum].Rows) != len(totals) {
		problems = append(problems, fmt.Sprintf("GROUP BY returned %d groups, the window rows hold %d", len(observed[labelGroupSum].Rows), len(totals)))
	}
	for _, row := range observed[labelGroupSum].Rows {
		want, ok := totals[rowKey(row[:1])]
		if !ok || row[1] != want[0] || row[2] != want[1] {
			problems = append(problems, fmt.Sprintf("GROUP BY row %s disagrees with the window count and total %s", describeRow(row), describeRow(want[:])))
		}
	}
	slices.Sort(problems)
	return problems
}

// windowGroups splits window rows by group in identifier order.
//
// Takes rows ([][]Cell) which holds the window rows.
//
// Returns map[string]*windowGroup which is keyed by the group cell.
// Returns error when an identifier or row number is not an integer.
func windowGroups(rows [][]Cell) (map[string]*windowGroup, error) {
	groups := map[string]*windowGroup{}
	for _, row := range rows {
		id, err := cellInteger(row[0])
		if err != nil {
			return nil, fmt.Errorf("window id: %w", err)
		}
		number, err := cellInteger(row[windowNumber])
		if err != nil {
			return nil, fmt.Errorf("window row_number: %w", err)
		}
		key := rowKey(row[1:2])
		if groups[key] == nil {
			groups[key] = &windowGroup{ids: nil, numbers: nil, rows: nil}
		}
		group := groups[key]
		group.ids, group.numbers, group.rows = append(group.ids, id), append(group.numbers, number), append(group.rows, row)
	}
	for _, group := range groups {
		order := make([]int, len(group.ids))
		for index := range order {
			order[index] = index
		}
		slices.SortFunc(order, func(left, right int) int { return int(group.ids[left] - group.ids[right]) })
		ids, numbers, sorted := make([]int64, 0, len(order)), make([]int64, 0, len(order)), make([][]Cell, 0, len(order))
		for _, index := range order {
			ids, numbers, sorted = append(ids, group.ids[index]), append(numbers, group.numbers[index]), append(sorted, group.rows[index])
		}
		group.ids, group.numbers, group.rows = ids, numbers, sorted
	}
	return groups, nil
}

// checkWindowGroup checks one group's window values.
//
// Takes name (string) which identifies the group.
// Takes group (*windowGroup) which holds its rows in identifier order.
//
// Returns []string which describes the disagreements.
func checkWindowGroup(name string, group *windowGroup) []string {
	var problems []string
	numbers := slices.Clone(group.numbers)
	slices.Sort(numbers)
	for index, number := range numbers {
		if number != int64(index+1) {
			problems = append(problems, fmt.Sprintf("group %s row numbers %v are not a permutation of 1..%d", name, group.numbers, len(numbers)))
			break
		}
	}
	size := Cell{Text: strconv.Itoa(len(group.ids)), Null: false}
	total := group.rows[0][windowTotal]
	for index, row := range group.rows {
		if row[windowCount] != size {
			problems = append(problems, fmt.Sprintf("group %s row %s counts %s rows in its partition, which holds %s", name, describeRow(row),
				describeCell(row[windowCount]), size.Text))
		}
		if row[windowTotal] != total {
			problems = append(problems, fmt.Sprintf("group %s rows disagree on the partition total: %s and %s", name, describeCell(total),
				describeCell(row[windowTotal])))
		}
		previous := Cell{Text: "", Null: true}
		if index > 0 {
			previous = Cell{Text: strconv.FormatInt(group.ids[index-1], decimalBase), Null: false}
		}
		if row[windowPrevious] != previous {
			problems = append(problems, fmt.Sprintf("group %s row %s has LAG(id) %s, the previous identifier is %s", name, describeRow(row),
				describeCell(row[windowPrevious]), describeCell(previous)))
		}
	}
	if last := group.rows[len(group.rows)-1]; last[windowRunning] != total {
		problems = append(problems, fmt.Sprintf("group %s running total ends at %s, the partition total is %s", name, describeCell(last[windowRunning]),
			describeCell(total)))
	}
	return problems
}

// checkWindowFilter requires a builder condition to keep the unfiltered window rows it
// matches, unchanged, rather than renumbering the rows that remain.
//
// Takes c (*Case) which holds the condition's value.
// Takes observed (results) which holds both builder reads.
//
// Returns []string which describes the disagreement.
func checkWindowFilter(c *Case, observed results) []string {
	var expected [][]Cell
	for _, row := range observed[labelWindowFull].Rows {
		if id, err := cellInteger(row[0]); err == nil && id > c.Parameter {
			expected = append(expected, row)
		}
	}
	if problem := compareBags("Where(id > k) on the window builder", observed[labelWindowFiltered].Rows, "the unfiltered window rows with id > k",
		expected); problem != "" {
		return []string{problem}
	}
	return nil
}

// checkLeftJoin requires a LEFT JOIN to keep every item and to hold the inner join's rows
// plus one row per unmatched item.
//
// Takes observed (results) which holds every item, the join and its counts.
//
// Returns []string which describes the disagreements.
func checkLeftJoin(_ *Case, observed results) []string {
	var problems []string
	joined := observed[labelLeftJoin].Rows
	if len(joined) < len(observed[labelAll].Rows) {
		problems = append(problems, fmt.Sprintf("LEFT JOIN returned %d rows, fewer than the %d items", len(joined), len(observed[labelAll].Rows)))
	}
	present := map[string]bool{}
	for _, row := range joined {
		present[rowKey(row[:1])] = true
	}
	for _, row := range observed[labelAll].Rows {
		if !present[rowKey(row[:1])] {
			problems = append(problems, fmt.Sprintf("LEFT JOIN lost item %s", describeCell(row[0])))
		}
	}
	inner, err := singleInteger(observed[labelInnerCount])
	if err != nil {
		return append(problems, "inner join count: "+err.Error())
	}
	anti, err := singleInteger(observed[labelAntiCount])
	if err != nil {
		return append(problems, "unmatched item count: "+err.Error())
	}
	if int64(len(joined)) != inner+anti {
		problems = append(problems, fmt.Sprintf("LEFT JOIN returned %d rows but the inner join counts %d and the unmatched items %d", len(joined), inner, anti))
	}
	return problems
}

// checkFullJoin requires a FULL JOIN to hold the LEFT JOIN's rows plus one per unmatched
// peer.
//
// Takes observed (results) which holds both joins and the unmatched peer count.
//
// Returns []string which describes the disagreement.
func checkFullJoin(_ *Case, observed results) []string {
	orphans, err := singleInteger(observed[labelOrphanCount])
	if err != nil {
		return []string{"unmatched peer count: " + err.Error()}
	}
	full, left := len(observed[labelFullJoin].Rows), len(observed[labelLeftJoin].Rows)
	if int64(full) != int64(left)+orphans {
		return []string{fmt.Sprintf("FULL JOIN returned %d rows but the LEFT JOIN returned %d and %d peers are unmatched", full, left, orphans)}
	}
	return nil
}

// checkSemiJoin requires the IN, EXISTS and DISTINCT join formulations to agree.
//
// Takes c (*Case) which reports whether EXISTS was called.
// Takes observed (results) which holds the formulations.
//
// Returns []string which describes the disagreements.
func checkSemiJoin(c *Case, observed results) []string {
	var problems []string
	in := observed[labelSemiIn].Rows
	for _, label := range []string{labelSemiIn, labelSemiJoin} {
		if duplicate := firstDuplicate(observed[label].Rows); duplicate != nil {
			problems = append(problems, fmt.Sprintf("%s repeats %s", label, describeRow(duplicate)))
		}
	}
	others := []string{labelSemiJoin}
	if c.enabled(featureCorrelated) {
		others = append(others, labelSemiExists)
	}
	for _, label := range others {
		if problem := compareSets(labelSemiIn, in, label, observed[label].Rows); problem != "" {
			problems = append(problems, problem)
		}
	}
	return problems
}

// checkUnionAll requires UNION ALL to hold both branches' rows.
//
// Takes observed (results) which holds the branches and the compound.
//
// Returns []string which describes the disagreement.
func checkUnionAll(_ *Case, observed results) []string {
	branches := slices.Concat(observed[labelBranchItems].Rows, observed[labelBranchPeers].Rows)
	if problem := compareBags("UNION ALL", observed[labelUnionAll].Rows, "both branches", branches); problem != "" {
		return []string{problem}
	}
	return nil
}

// checkUnion requires UNION and DISTINCT over UNION ALL to hold each branch value once.
//
// Takes observed (results) which holds the branches and both compounds.
//
// Returns []string which describes the disagreements.
func checkUnion(_ *Case, observed results) []string {
	branches := slices.Concat(observed[labelBranchItems].Rows, observed[labelBranchPeers].Rows)
	var problems []string
	for _, label := range []string{labelUnion, labelUnionDistinct} {
		if duplicate := firstDuplicate(observed[label].Rows); duplicate != nil {
			problems = append(problems, fmt.Sprintf("%s repeats %s", label, describeRow(duplicate)))
		}
		if problem := compareSets(label, observed[label].Rows, "both branches", branches); problem != "" {
			problems = append(problems, problem)
		}
	}
	return problems
}

// checkSetOperations requires INTERSECT and EXCEPT to hold the branches' common and
// first-only values once each. ClickHouse runs its bare forms under its default ALL
// modes, so they must hold every first-branch row the second branch does or does not
// hold, and its DISTINCT forms are held to the distinct rule.
//
// Takes c (*Case) which selects the engine's semantics.
// Takes observed (results) which holds the branches and the compounds.
//
// Returns []string which describes the disagreements.
func checkSetOperations(c *Case, observed results) []string {
	second := map[string]bool{}
	for _, row := range observed[labelBranchPeers].Rows {
		second[rowKey(row)] = true
	}
	var common, only [][]Cell
	for _, row := range observed[labelBranchItems].Rows {
		if second[rowKey(row)] {
			common = append(common, row)
		} else {
			only = append(only, row)
		}
	}
	expectations := map[string][][]Cell{labelIntersect: common, labelExcept: only, labelIntersectDistinct: common, labelExceptDistinct: only}
	labels := c.setLabels()[2:]
	problems := make([]string, 0, len(labels))
	for _, label := range labels {
		bag := c.Engine == engineClickHouse && (label == labelIntersect || label == labelExcept)
		problems = append(problems, checkSetOperation(label, observed[label].Rows, expectations[label], bag)...)
	}
	return problems
}

// checkSetOperation checks one INTERSECT or EXCEPT read against its rule.
//
// Takes label (string) which names the read.
// Takes rows ([][]Cell) which holds what it returned.
// Takes expected ([][]Cell) which holds the first branch's rows the rule keeps.
// Takes bag (bool) which is true under ClickHouse's ALL modes, which keep every such row.
//
// Returns []string which describes the disagreements.
func checkSetOperation(label string, rows, expected [][]Cell, bag bool) []string {
	if bag {
		if problem := compareBags(label, rows, "the first branch's rows", expected); problem != "" {
			return []string{problem}
		}
		return nil
	}
	var problems []string
	if duplicate := firstDuplicate(rows); duplicate != nil {
		problems = append(problems, fmt.Sprintf("%s repeats %s", label, describeRow(duplicate)))
	}
	if problem := compareSets(label, rows, "the branch values", expected); problem != "" {
		problems = append(problems, problem)
	}
	return problems
}

// checkBuilderLimit requires Where(a > k) on a builder over a query that limits its own
// rows to keep the unfiltered rows it matches, rather than limiting the matching rows,
// and its Count to count them.
//
// Takes c (*Case) which holds the condition's value.
// Takes observed (results) which holds each limited builder's reads and count.
//
// Returns []string which describes the disagreements.
func checkBuilderLimit(c *Case, observed results) []string {
	var problems []string
	for _, limited := range c.limitedBuilders() {
		var expected [][]Cell
		for _, row := range observed[limited.full].Rows {
			if value, err := cellInteger(row[pageKey]); err == nil && value > c.Parameter {
				expected = append(expected, row)
			}
		}
		if problem := compareBags("Where(a > k) on "+limited.method, observed[limited.where].Rows, "its unfiltered rows with a > k", expected); problem != "" {
			problems = append(problems, problem)
		}
		if count := observed[limited.count].Count; count != int64(len(expected)) {
			problems = append(problems, fmt.Sprintf("Count with Where(a > k) on %s returned %d, its unfiltered rows hold %d with a > k", limited.method, count,
				len(expected)))
		}
	}
	return problems
}

// checkStaticPages requires static LIMIT and OFFSET pages to concatenate to the ordered
// rows.
//
// Takes c (*Case) which holds the page size and offsets.
// Takes observed (results) which holds the ordered rows and every page.
//
// Returns []string which describes the disagreements.
func checkStaticPages(c *Case, observed results) []string {
	return checkPages(c, observed, labelPageFull, labelPage, false)
}

// checkBuilderPages requires builder pages to concatenate to the builder's ordered rows,
// its count to match them, and its condition to match the static query's.
//
// Takes c (*Case) which holds the page size, offsets and direction.
// Takes observed (results) which holds the builder reads and the static query.
//
// Returns []string which describes the disagreements.
func checkBuilderPages(c *Case, observed results) []string {
	problems := checkPages(c, observed, labelBuilderFull, labelBuilderPage, c.Descending)
	full := observed[labelBuilderFull].Rows
	if count := observed[labelBuilderCount].Count; count != int64(len(full)) {
		problems = append(problems, fmt.Sprintf("Count returned %d, All returned %d rows", count, len(full)))
	}
	static := make([][]Cell, 0, len(observed[labelStaticWhere].Rows))
	for _, row := range observed[labelStaticWhere].Rows {
		static = append(static, row[:pageWidth])
	}
	if problem := compareBags("builder Where(a > k)", observed[labelBuilderWhere].Rows, "the static WHERE i.a > k", static); problem != "" {
		problems = append(problems, problem)
	}
	return problems
}

// checkPages requires pages to concatenate to the full ordered read, which must hold
// every item once in key order.
//
// Takes c (*Case) which holds the page size, offsets and items.
// Takes observed (results) which holds the reads.
// Takes full (string) which labels the unpaginated read.
// Takes prefix (string) which prefixes each page's offset.
// Takes descending (bool) which reports the read's direction.
//
// Returns []string which describes the disagreements.
func checkPages(c *Case, observed results, full, prefix string, descending bool) []string {
	var problems []string
	rows := observed[full].Rows
	if len(rows) != len(c.Items) {
		problems = append(problems, fmt.Sprintf("%s returned %d rows for %d items", full, len(rows), len(c.Items)))
	}
	if problem := checkOrder(rows, descending); problem != "" {
		problems = append(problems, full+" "+problem)
	}
	var pages [][]Cell
	for _, offset := range c.offsets() {
		page := observed[prefix+strconv.Itoa(offset)].Rows
		if len(page) > c.PageSize {
			problems = append(problems, fmt.Sprintf("page at offset %d holds %d rows, more than the limit %d", offset, len(page), c.PageSize))
		}
		pages = append(pages, page...)
	}
	if describeRows(pages) != describeRows(rows) {
		problems = append(problems, fmt.Sprintf("pages of %d concatenate to %s, the ordered read is %s", c.PageSize, describeRows(pages), describeRows(rows)))
	}
	return problems
}

// checkOrder requires rows to be ordered by their third column and then their first, with
// the NULL keys together at either end, and identifiers unique.
//
// Takes rows ([][]Cell) which holds (id, grp, a) rows.
// Takes descending (bool) which reports the direction.
//
// Returns string which describes the first violation, empty when none.
func checkOrder(rows [][]Cell, descending bool) string {
	if duplicate := firstDuplicate(rows); duplicate != nil {
		return "repeats " + describeRow(duplicate)
	}
	for index := 1; index < len(rows); index++ {
		previous, current := rows[index-1], rows[index]
		if previous[pageKey].Null != current[pageKey].Null {
			continue
		}
		order, err := compareKeys(previous, current)
		if err != nil {
			return err.Error()
		}
		if descending {
			order = -order
		}
		if order >= 0 {
			return fmt.Sprintf("is out of order at %s then %s", describeRow(previous), describeRow(current))
		}
	}
	if !nullsTogether(rows) {
		return "does not keep its NULL keys together at one end: " + describeRows(rows)
	}
	return ""
}

// nullsTogether reports whether the rows' NULL keys form one run at either end.
//
// Takes rows ([][]Cell) which holds (id, grp, a) rows.
//
// Returns bool which is true when no NULL key lies between two non-NULL keys.
func nullsTogether(rows [][]Cell) bool {
	runs := 0
	for index, row := range rows {
		if row[pageKey].Null && (index == 0 || !rows[index-1][pageKey].Null) {
			runs++
		}
	}
	return runs == 0 || (runs == 1 && (rows[0][pageKey].Null || rows[len(rows)-1][pageKey].Null))
}

// compareKeys compares two rows by their third and then first column as integers.
//
// Takes left ([]Cell) which is the earlier row.
// Takes right ([]Cell) which is the later row.
//
// Returns int which is negative, zero or positive.
// Returns error when a key is not an integer.
func compareKeys(left, right []Cell) (int, error) {
	for _, column := range []int{pageKey, 0} {
		if left[column].Null && right[column].Null {
			continue
		}
		first, err := cellInteger(left[column])
		if err != nil {
			return 0, err
		}
		second, err := cellInteger(right[column])
		if err != nil {
			return 0, err
		}
		if first != second {
			if first < second {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, nil
}

// itemCells writes an item as the item projection returns it.
//
// Takes item (Item) which is the row.
//
// Returns []Cell which holds id, grp, a, b and label.
func itemCells(item Item) []Cell {
	label := Cell{Text: "", Null: true}
	if item.Label != nil {
		label = Cell{Text: *item.Label, Null: false}
	}
	return []Cell{integerCell(&item.ID), integerCell(&item.Group), integerCell(item.A), integerCell(item.B), label}
}

// integerCell writes a nullable integer.
//
// Takes value (*int64) which is nil for NULL.
//
// Returns Cell which holds its canonical decimal.
func integerCell(value *int64) Cell {
	if value == nil {
		return Cell{Text: "", Null: true}
	}
	return Cell{Text: strconv.FormatInt(*value, decimalBase), Null: false}
}

// cellInteger reads an integer cell.
//
// Takes cell (Cell) which holds the value.
//
// Returns int64 which is the integer.
// Returns error when the cell is NULL or not an integer.
func cellInteger(cell Cell) (int64, error) {
	if cell.Null {
		return 0, errors.New("unexpected NULL")
	}
	value, err := strconv.ParseInt(cell.Text, decimalBase, integerBits)
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer: %w", cell.Text, err)
	}
	return value, nil
}

// singleInteger reads a one-row, one-column count.
//
// Takes observation (*Observation) which holds the read.
//
// Returns int64 which is the count.
// Returns error when the read is not one integer.
func singleInteger(observation *Observation) (int64, error) {
	if len(observation.Rows) != 1 || len(observation.Rows[0]) != 1 {
		return 0, fmt.Errorf("returned %s, expected one count", describeRows(observation.Rows))
	}
	return cellInteger(observation.Rows[0][0])
}

// compareBags compares two row multisets.
//
// Takes leftName (string) which describes the first.
// Takes left ([][]Cell) which holds the first.
// Takes rightName (string) which describes the second.
// Takes right ([][]Cell) which holds the second.
//
// Returns string which describes the difference, empty when none.
func compareBags(leftName string, left [][]Cell, rightName string, right [][]Cell) string {
	if slices.Equal(sortedKeys(left), sortedKeys(right)) {
		return ""
	}
	return fmt.Sprintf("%s returned %s, %s are %s", leftName, describeRows(sortedRows(left)), rightName, describeRows(sortedRows(right)))
}

// compareSets compares the distinct rows of two results.
//
// Takes leftName (string) which describes the first.
// Takes left ([][]Cell) which holds the first.
// Takes rightName (string) which describes the second.
// Takes right ([][]Cell) which holds the second.
//
// Returns string which describes the difference, empty when none.
func compareSets(leftName string, left [][]Cell, rightName string, right [][]Cell) string {
	if slices.Equal(slices.Compact(sortedKeys(left)), slices.Compact(sortedKeys(right))) {
		return ""
	}
	return fmt.Sprintf("%s holds %s, %s hold %s", leftName, describeRows(sortedRows(left)), rightName, describeRows(sortedRows(right)))
}

// firstDuplicate finds a repeated row.
//
// Takes rows ([][]Cell) which holds the rows.
//
// Returns []Cell which is the first repeated row, nil when none.
func firstDuplicate(rows [][]Cell) []Cell {
	seen := map[string]bool{}
	for _, row := range rows {
		key := rowKey(row)
		if seen[key] {
			return row
		}
		seen[key] = true
	}
	return nil
}

// sortedKeys returns the rows' keys in sorted order.
//
// Takes rows ([][]Cell) which holds the rows.
//
// Returns []string which holds one key per row.
func sortedKeys(rows [][]Cell) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, rowKey(row))
	}
	slices.Sort(keys)
	return keys
}

// sortedRows returns the rows in key order for stable messages.
//
// Takes rows ([][]Cell) which holds the rows.
//
// Returns [][]Cell which is a sorted copy.
func sortedRows(rows [][]Cell) [][]Cell {
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(left, right []Cell) int { return strings.Compare(rowKey(left), rowKey(right)) })
	return sorted
}

// rowKey encodes a row so equal rows, NULLs included, have equal keys.
//
// Takes row ([]Cell) which holds the values.
//
// Returns string which is the key.
func rowKey(row []Cell) string {
	var key strings.Builder
	for _, cell := range row {
		if cell.Null {
			key.WriteString("\x00N")
		} else {
			key.WriteString("\x00V" + cell.Text)
		}
	}
	return key.String()
}

// describeCell writes a cell for failure messages.
//
// Takes cell (Cell) which holds the value.
//
// Returns string which is NULL or the quoted text.
func describeCell(cell Cell) string {
	if cell.Null {
		return "NULL"
	}
	if _, err := strconv.ParseInt(cell.Text, decimalBase, integerBits); err == nil {
		return cell.Text
	}
	return strconv.Quote(cell.Text)
}

// describeRow writes a row for failure messages.
//
// Takes row ([]Cell) which holds the values.
//
// Returns string which lists them in parentheses.
func describeRow(row []Cell) string {
	parts := make([]string, 0, len(row))
	for _, cell := range row {
		parts = append(parts, describeCell(cell))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// describeRows writes rows for failure messages.
//
// Takes rows ([][]Cell) which holds the rows.
//
// Returns string which lists them in brackets.
func describeRows(rows [][]Cell) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, describeRow(row))
	}
	return "[" + strings.Join(parts, " ") + "]"
}
