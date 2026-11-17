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

package fuzzcase

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// queryGrammarRecursiveColumns is the number of shifted recursive CTE columns.
	queryGrammarRecursiveColumns = 3

	// queryGrammarRecursiveDepths bounds the recursion depth.
	queryGrammarRecursiveDepths = 4

	// queryGrammarNullSource marks a recursive column fed by a NULL literal.
	queryGrammarNullSource = -1

	// queryGrammarWindowPrefix starts every window function construct name.
	queryGrammarWindowPrefix = "window/"

	// queryGrammarFramePrefix starts every frame construct name.
	queryGrammarFramePrefix = "frame/"

	// queryGrammarScalarPrefix starts every scalar subquery construct name.
	queryGrammarScalarPrefix = "scalar/"

	// queryGrammarGroupingPrefix starts every grouping construct name.
	queryGrammarGroupingPrefix = "grouping/"

	// queryGrammarCountPeers counts the peers table in a scalar subquery.
	queryGrammarCountPeers = "(SELECT COUNT(*) FROM fuzz_peers p"

	// queryGrammarLag names the lag window function.
	queryGrammarLag = "lag"

	// queryGrammarLead names the lead window function.
	queryGrammarLead = "lead"

	// queryGrammarLagDefault names lag with a default value.
	queryGrammarLagDefault = "lag-default"

	// queryGrammarSecondWindow locates the choices of the second window column.
	queryGrammarSecondWindow = 3

	// queryGrammarPartitionChoice locates the partition choice.
	queryGrammarPartitionChoice = 6
)

// queryGrammarFrame is one window frame clause.
type queryGrammarFrame struct {
	// name names the frame in audit reports.
	name string

	// sql is the frame clause.
	sql string

	// empty reports a frame that can hold no row.
	empty bool
}

// queryGrammarWindowCalls spells each window function around its argument.
var queryGrammarWindowCalls = map[string]string{queryGrammarLag: "LAG(%s)", queryGrammarLead: "LEAD(%s)", queryGrammarLagDefault: "LAG(%s, 1, 0)",
	"first-value": "FIRST_VALUE(%s)", "last-value": "LAST_VALUE(%s)", "nth-value": "NTH_VALUE(%s, 2)",
	"sum": "SUM(%s)", "min": "MIN(%s)", "max": "MAX(%s)", "avg": "AVG(%s)", "count": "COUNT(%s)"}

// queryGrammarClickHouseOffsetCalls spells the ClickHouse offset functions that read the
// whole frame.
var queryGrammarClickHouseOffsetCalls = map[string]string{queryGrammarLag: "lagInFrame(%s)", queryGrammarLead: "leadInFrame(%s)", queryGrammarLagDefault: "lagInFrame(%s, 1, 0)"}

// queryGrammarRecursivePatterns maps each recursive pattern to the previous column each
// column reads.
var queryGrammarRecursivePatterns = []struct {
	name    string
	sources [queryGrammarRecursiveColumns]int
}{
	{name: "shift", sources: [queryGrammarRecursiveColumns]int{1, 2, queryGrammarNullSource}},
	{name: "identity", sources: [queryGrammarRecursiveColumns]int{0, 1, 2}},
	{name: "rotate", sources: [queryGrammarRecursiveColumns]int{2, 0, 1}},
	{name: "reset", sources: [queryGrammarRecursiveColumns]int{0, queryGrammarNullSource, 1}},
}

// queryGrammarWindowFunctions lists the window function constructs in choice order.
var queryGrammarWindowFunctions = []string{queryGrammarLag, queryGrammarLead, queryGrammarLagDefault, "first-value", "last-value", "nth-value", "sum", "min", "max", "avg", "count"}

// queryGrammarFrames lists each frame construct with its SQL and whether it can hold no
// row.
var queryGrammarFrames = []queryGrammarFrame{
	{name: "rows-to-current", sql: "ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW", empty: false},
	{name: "rows-preceding", sql: "ROWS BETWEEN 2 PRECEDING AND 1 PRECEDING", empty: true},
	{name: "rows-following", sql: "ROWS BETWEEN 1 FOLLOWING AND 2 FOLLOWING", empty: true},
	{name: "range-preceding", sql: "RANGE BETWEEN 3 PRECEDING AND 1 PRECEDING", empty: true},
	{name: "current-row", sql: "ROWS BETWEEN CURRENT ROW AND CURRENT ROW", empty: false},
	{name: "range-all", sql: "RANGE BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING", empty: false},
}

// queryGrammarScalars lists each scalar subquery construct with its SQL and whether it
// admits NULL.
//
// A subquery admits NULL when it can return no row (LIMIT 0, OFFSET 1, no matching row)
// or when its single value can be NULL (MAX over an empty table).
var queryGrammarScalars = []struct {
	name     string
	sql      string
	nullable bool
}{
	{name: "count", sql: queryGrammarCountPeers + expressionClose, nullable: false},
	{name: "limit-one", sql: queryGrammarCountPeers + " LIMIT 1)", nullable: false},
	{name: "limit-zero", sql: queryGrammarCountPeers + " LIMIT 0)", nullable: true},
	{name: "offset-one", sql: queryGrammarCountPeers + " LIMIT 1 OFFSET 1)", nullable: true},
	{name: "offset-zero", sql: queryGrammarCountPeers + " LIMIT 1 OFFSET 0)", nullable: false},
	{name: "max", sql: "(SELECT MAX(p.id) FROM fuzz_peers p)", nullable: true},
	{name: "first-row", sql: "(SELECT p.id FROM fuzz_peers p ORDER BY p.id LIMIT 1)", nullable: true},
	{name: "correlated", sql: queryGrammarCountPeers + " WHERE p.id = i.id)", nullable: false},
	{name: "correlated-offset", sql: queryGrammarCountPeers + " WHERE p.id = i.id LIMIT 1 OFFSET 1)", nullable: true},
	{name: "fetch-zero", sql: queryGrammarCountPeers + " FETCH FIRST 0 ROWS ONLY)", nullable: true},
	{name: "coalesced", sql: "COALESCE(" + queryGrammarCountPeers + " LIMIT 1 OFFSET 1), -1)", nullable: false},
}

// queryGrammarGroupings lists each grouping construct with its clause and its key rules.
var queryGrammarGroupings = []struct {
	name   string
	clause string
	// pair groups by the nullable score as well as the category.
	pair bool
	// total includes the empty grouping set.
	total bool
	// keyed keeps the category in every grouping set.
	keyed bool
}{
	{name: "rollup", clause: "ROLLUP (i.category)", pair: false, total: true, keyed: false},
	{name: "rollup-pair", clause: "ROLLUP (i.category, i.score)", pair: true, total: true, keyed: false},
	{name: "cube", clause: "CUBE (i.category, i.score)", pair: true, total: true, keyed: false},
	{name: "sets", clause: "GROUPING SETS ((i.category), (i.score), ())", pair: true, total: true, keyed: false},
	{name: "sets-without-total", clause: "GROUPING SETS ((i.category, i.score), (i.category))", pair: true, total: false, keyed: true},
	{name: "with-rollup", clause: "i.category WITH ROLLUP", pair: false, total: true, keyed: false},
	{name: "with-rollup", clause: "i.category, i.score WITH ROLLUP", pair: true, total: true, keyed: false},
}

// queryGrammarRecursiveQuery builds a recursive CTE whose columns read each other between
// rounds.
//
// A column admits NULL when its anchor does or when, at the fixed point of the round
// rule, it reads NULL or a column that admits NULL.
//
// Takes engine (string) which selects the native nullable anchor spelling.
// Takes choices (queryGrammarChoices) which selects the pattern, anchor, depth and anchor
// rows.
//
// Returns queryGrammarQuery which projects the round counter and the three columns.
func queryGrammarRecursiveQuery(engine string, choices queryGrammarChoices) queryGrammarQuery {
	pattern := queryGrammarRecursivePatterns[choices.pick(0, len(queryGrammarRecursivePatterns))]
	anchors := [queryGrammarRecursiveColumns]string{queryGrammarItemID, "i.category", queryGrammarItemID}
	nullable := [queryGrammarRecursiveColumns]bool{}
	if choices.pick(1, 2) == 1 {
		anchors[1], nullable[1] = queryGrammarItemScore, true
	}
	anchorNullable := nullable
	for changed := true; changed; {
		changed = false
		for column, source := range pattern.sources {
			next := source == queryGrammarNullSource || nullable[column] || nullable[source]
			changed = changed || next != nullable[column]
			nullable[column] = next
		}
	}
	names := [queryGrammarRecursiveColumns]string{"a", "b", "c"}
	anchor, round := "SELECT 1", "SELECT n + 1"
	columns := make([]queryGrammarColumn, 0, len(pattern.sources)+1)
	columns = append(columns, queryGrammarColumn{expression: "n", alias: "step", nullable: false})
	for column, source := range pattern.sources {
		expression := anchors[column]
		if engine == engineClickHouse && nullable[column] && !anchorNullable[column] {
			expression = "toNullable(" + expression + ")"
		}
		anchor += columnSeparator + expression
		if source == queryGrammarNullSource {
			round += ", NULL"
		} else {
			round += columnSeparator + names[source]
		}
		columns = append(columns, queryGrammarColumn{expression: names[column], alias: "value_" + names[column], nullable: nullable[column]})
	}
	anchor += " FROM fuzz_items i WHERE i.id <= " + strconv.Itoa(1+choices.pick(queryGrammarChoice3, 2))
	round += " FROM chain WHERE n < " + strconv.Itoa(1+choices.pick(2, queryGrammarRecursiveDepths))
	sql := "WITH RECURSIVE chain (n, a, b, c) AS (" + anchor + " UNION ALL " + round + ") " + queryGrammarSelect(columns) + " FROM chain"
	return queryGrammarQuery{sql: sql, columns: columns, features: []string{"recursive/" + pattern.name}, ordered: false}
}

// queryGrammarWindowQuery builds two window functions over explicit frames.
//
// Takes engine (string) which selects native function names and the default rule.
// Takes choices (queryGrammarChoices) which selects functions, frames, arguments and
// partitioning.
//
// Returns queryGrammarQuery which projects the key and both window values.
func queryGrammarWindowQuery(engine string, choices queryGrammarChoices) queryGrammarQuery {
	columns := make([]queryGrammarColumn, 0, queryGrammarChoice3)
	columns = append(columns, queryGrammarColumn{expression: queryGrammarItemID, alias: queryGrammarID, nullable: false})
	features := make([]string, 0, queryGrammarChoice4)
	partition := ""
	if choices.pick(queryGrammarPartitionChoice, 2) == 1 {
		partition = "PARTITION BY i.category "
	}
	functions := queryGrammarSupportedOf(engine, queryGrammarWindowPrefix, queryGrammarWindowFunctions)
	for index, first := range []int{0, queryGrammarSecondWindow} {
		name := functions[choices.pick(first, len(functions))]
		frame := queryGrammarFrames[choices.pick(first+1, len(queryGrammarFrames))]
		argument := []string{queryGrammarItemID, queryGrammarItemScore}[choices.pick(first+2, 2)]
		if name == "count" && engine == engineClickHouse {

			argument = queryGrammarItemID
		}
		column, frameUsed := queryGrammarWindowColumn(engine, name, argument, partition+"ORDER BY i.id", frame)
		column.alias = "w" + strconv.Itoa(index)
		columns = append(columns, column)
		features = append(features, queryGrammarWindowPrefix+name)
		if frameUsed {
			features = append(features, queryGrammarFramePrefix+frame.name)
		}
	}
	return queryGrammarQuery{sql: queryGrammarSelect(columns) + " FROM fuzz_items i", columns: columns, features: features, ordered: false}
}

// queryGrammarWindowColumn spells one window function and derives its nullability.
//
// LAG and LEAD read outside the frame and give NULL past the partition edge unless they
// carry a default. FIRST_VALUE, LAST_VALUE and the aggregates give NULL for an empty
// frame, NTH_VALUE for a frame shorter than its position, and COUNT never gives NULL.
// ClickHouse gives the type's default instead of NULL, so only a Nullable argument admits
// NULL there.
//
// Takes engine (string) which selects native function names and the default rule.
// Takes name (string) which names the window function construct.
// Takes argument (string) which is the column the function reads.
// Takes order (string) which holds the partition and ordering clauses.
// Takes frame (queryGrammarFrame) which is the frame clause and whether it can hold no
// row.
//
// Returns queryGrammarColumn which has no alias yet.
// Returns bool which reports whether the frame clause was used.
func queryGrammarWindowColumn(engine, name, argument, order string, frame queryGrammarFrame) (queryGrammarColumn, bool) {
	argumentNullable := argument == queryGrammarItemScore
	defaults := queryGrammarFillsDefaults(engine)
	call := fmt.Sprintf(queryGrammarWindowCalls[name], argument)
	empty := frame.empty
	switch name {
	case queryGrammarLag, queryGrammarLead, queryGrammarLagDefault:
		over := queryGrammarOver + order + ")"
		if defaults {
			call = fmt.Sprintf(queryGrammarClickHouseOffsetCalls[name], argument)
			over = queryGrammarOver + order + " ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)"
		}
		nullable := argumentNullable || (name != queryGrammarLagDefault && !defaults)
		return queryGrammarColumn{expression: call + over, alias: "", nullable: nullable}, false
	case "count":
		return queryGrammarColumn{expression: call + queryGrammarOver + order + " " + frame.sql + expressionClose, alias: "", nullable: false}, true
	case "nth-value":
		empty = true
	}
	nullable := argumentNullable || (empty && !defaults)
	return queryGrammarColumn{expression: call + queryGrammarOver + order + " " + frame.sql + expressionClose, alias: "", nullable: nullable}, true
}

// queryGrammarScalarQuery builds scalar subqueries with literal LIMIT, OFFSET and FETCH
// bounds.
//
// Takes engine (string) which selects the admitted constructs.
// Takes choices (queryGrammarChoices) which selects two subqueries.
//
// Returns queryGrammarQuery which projects the key and both subqueries.
func queryGrammarScalarQuery(engine string, choices queryGrammarChoices) queryGrammarQuery {
	names := make([]string, 0, len(queryGrammarScalars))
	for _, scalar := range queryGrammarScalars {
		names = append(names, scalar.name)
	}
	admitted := queryGrammarSupportedOf(engine, queryGrammarScalarPrefix, names)
	columns := make([]queryGrammarColumn, 0, queryGrammarChoice3)
	columns = append(columns, queryGrammarColumn{expression: queryGrammarItemID, alias: queryGrammarID, nullable: false})
	features := make([]string, 0, 2)
	for index := range 2 {
		name := admitted[choices.pick(index, len(admitted))]
		for _, scalar := range queryGrammarScalars {
			if scalar.name != name {
				continue
			}
			expression := scalar.sql
			if engine == engineClickHouse {

				expression = strings.Replace(expression, ", -1)", ", 0)", 1)
			}
			nullable := scalar.nullable
			if scalar.name == "max" && queryGrammarFillsDefaults(engine) {

				nullable = false
			}
			columns = append(columns, queryGrammarColumn{expression: expression, alias: "s" + strconv.Itoa(index), nullable: nullable})
		}
		features = append(features, queryGrammarScalarPrefix+name)
	}
	return queryGrammarQuery{sql: queryGrammarSelect(columns) + " FROM fuzz_items i", columns: columns, features: features, ordered: false}
}

// queryGrammarGroupingQuery builds ROLLUP, CUBE, GROUPING SETS and WITH ROLLUP
// aggregates.
//
// A key admits NULL in the rows of a grouping set that leaves it out. ClickHouse gives
// the type's default there instead. SUM over the empty grouping set admits NULL only on
// engines that return a grand total row for an empty table.
//
// Takes engine (string) which selects the admitted constructs and rules.
// Takes choices (queryGrammarChoices) which selects the grouping.
//
// Returns queryGrammarQuery which projects the keys and three aggregates.
func queryGrammarGroupingQuery(engine string, choices queryGrammarChoices) queryGrammarQuery {
	var admitted []int
	for index, grouping := range queryGrammarGroupings {
		if queryGrammarSupports(engine, queryGrammarGroupingPrefix+grouping.name) {
			admitted = append(admitted, index)
		}
	}
	grouping := queryGrammarGroupings[admitted[choices.pick(0, len(admitted))]]
	columns := []queryGrammarColumn{{expression: "i.category", alias: "category", nullable: !grouping.keyed && !queryGrammarFillsDefaults(engine)}}
	if grouping.pair {
		columns = append(columns, queryGrammarColumn{expression: queryGrammarItemScore, alias: "score", nullable: true})
	}
	columns = append(columns, queryGrammarColumn{expression: "COUNT(*)", alias: "total", nullable: false},
		queryGrammarColumn{expression: "SUM(i.id)", alias: "id_sum", nullable: grouping.total && queryGrammarTotalOverEmptyInput(engine)},
		queryGrammarColumn{expression: "MAX(i.score)", alias: "top", nullable: true})
	sql := queryGrammarSelect(columns) + " FROM fuzz_items i GROUP BY " + grouping.clause
	return queryGrammarQuery{sql: sql, columns: columns, features: []string{queryGrammarGroupingPrefix + grouping.name}, ordered: false}
}
