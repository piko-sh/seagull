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
	"slices"
	"strconv"
	"strings"
)

const (
	// queryGrammarItems names the three-column fixture table.
	queryGrammarItems = "fuzz_items"

	// queryGrammarPeers names the two-column fixture table.
	queryGrammarPeers = "fuzz_peers"

	// queryGrammarOffsetCount bounds the join key offsets.
	queryGrammarOffsetCount = 3

	// queryGrammarJoinPrefix starts every join construct name.
	queryGrammarJoinPrefix = "join/"

	// queryGrammarID names the non-null key column.
	queryGrammarID = "id"

	// queryGrammarScore names the nullable column.
	queryGrammarScore = "score"

	// queryGrammarCategory names the non-null column only the items table has.
	queryGrammarCategory = "category"

	// queryGrammarInner names the inner join kind.
	queryGrammarInner = "inner"

	// queryGrammarLeft names the left outer join kind.
	queryGrammarLeft = "left"

	// queryGrammarRight names the right outer join kind.
	queryGrammarRight = "right"

	// queryGrammarFull names the full outer join kind.
	queryGrammarFull = "full"

	// queryGrammarCross names the cross join kind.
	queryGrammarCross = "cross"

	// queryGrammarAliasA names the first relation.
	queryGrammarAliasA = "a"

	// queryGrammarAliasB names the second relation.
	queryGrammarAliasB = "b"

	// queryGrammarAliasC names the third relation.
	queryGrammarAliasC = "c"

	// queryGrammarLateralForms counts the lateral join forms.
	queryGrammarLateralForms = 5

	// queryGrammarLateralLeftLimit is the left join form over a LIMIT 1 derived table.
	queryGrammarLateralLeftLimit = 3

	// queryGrammarLateralComma is the comma form over a LIMIT 1 derived table.
	queryGrammarLateralComma = 4
)

// queryGrammarJoinKinds lists the join kinds a construct choice selects from.
var queryGrammarJoinKinds = []string{queryGrammarInner, queryGrammarLeft, queryGrammarRight, queryGrammarFull}

// queryGrammarRelation is a table, a derived table or a join in the independent
// nullability model.
type queryGrammarRelation struct {
	// left is the left operand of a join.
	left *queryGrammarRelation

	// right is the right operand of a join.
	right *queryGrammarRelation

	// source spells a leaf table or derived table.
	source string

	// alias names a leaf.
	alias string

	// kind names the join kind of an inner node.
	kind string

	// on holds the join condition.
	on string

	// columns lists the columns a leaf exposes.
	columns []string

	// grouped encloses the join in parentheses.
	grouped bool
}

// render spells the relation as a FROM item.
//
// Returns string which is the native SQL.
func (relation *queryGrammarRelation) render() string {
	if relation.left == nil {
		return relation.source + " " + relation.alias
	}
	text := relation.left.render() + " " + strings.ToUpper(relation.kind) + " JOIN " + relation.right.render()
	if relation.on != "" {
		text += " ON " + relation.on
	}
	if relation.grouped {
		return "(" + text + ")"
	}
	return text
}

// leaves lists the leaf relations from left to right.
//
// Returns []*queryGrammarRelation which holds every leaf.
func (relation *queryGrammarRelation) leaves() []*queryGrammarRelation {
	if relation.left == nil {
		return []*queryGrammarRelation{relation}
	}
	return append(relation.left.leaves(), relation.right.leaves()...)
}

// extend records the leaves an outer join pads with NULL.
//
// A LEFT join pads every leaf of its right operand, a RIGHT join every leaf of its left
// operand and a FULL join both, whatever the nesting of the operands.
//
// Takes padded (map[string]bool) which accumulates padded aliases.
func (relation *queryGrammarRelation) extend(padded map[string]bool) {
	if relation.left == nil {
		return
	}
	relation.left.extend(padded)
	relation.right.extend(padded)
	if relation.kind == queryGrammarLeft || relation.kind == queryGrammarFull {
		for _, leaf := range relation.right.leaves() {
			padded[leaf.alias] = true
		}
	}
	if relation.kind == queryGrammarRight || relation.kind == queryGrammarFull {
		for _, leaf := range relation.left.leaves() {
			padded[leaf.alias] = true
		}
	}
}

// queryGrammarLeaf builds a table or derived table relation.
//
// Takes source (int) which selects items, peers or a shifted derived peers table.
// Takes alias (string) which names the relation.
// Takes offset (int) which shifts the derived table's keys.
//
// Returns *queryGrammarRelation which is a leaf.
func queryGrammarLeaf(source int, alias string, offset int) *queryGrammarRelation {
	leaf := &queryGrammarRelation{left: nil, right: nil, source: queryGrammarItems, alias: alias, kind: "", on: "",
		columns: []string{queryGrammarID, queryGrammarScore, queryGrammarCategory}, grouped: false}
	switch source {
	case 1:
		leaf.source, leaf.columns = queryGrammarPeers, []string{queryGrammarID, queryGrammarScore}
	case 2:
		leaf.source, leaf.columns = "(SELECT id + "+strconv.Itoa(offset+1)+" AS id, score FROM fuzz_peers)", []string{queryGrammarID, queryGrammarScore}
	}
	return leaf
}

// queryGrammarJoin builds a join of two relations.
//
// Takes left (*queryGrammarRelation) which is the left operand.
// Takes kind (string) which names the join kind.
// Takes right (*queryGrammarRelation) which is the right operand.
// Takes on (string) which is the condition and is empty for a cross join.
//
// Returns *queryGrammarRelation which is a join node.
func queryGrammarJoin(left *queryGrammarRelation, kind string, right *queryGrammarRelation, on string) *queryGrammarRelation {
	return &queryGrammarRelation{left: left, right: right, source: "", alias: "", kind: kind, on: on, columns: nil, grouped: false}
}

// queryGrammarBaseNullable reports whether a fixture column admits NULL before any join.
//
// Takes column (string) which names a fixture column.
//
// Returns bool which is true only for the score column.
func queryGrammarBaseNullable(column string) bool {
	return column == queryGrammarScore
}

// queryGrammarJoinColumns projects every leaf's key and score with join nullability.
//
// Takes engine (string) which selects the padding rule.
// Takes relation (*queryGrammarRelation) which is the FROM tree.
//
// Returns []queryGrammarColumn which holds the leaf columns and a COALESCE of the outer
// keys.
func queryGrammarJoinColumns(engine string, relation *queryGrammarRelation) []queryGrammarColumn {
	padded := make(map[string]bool)
	relation.extend(padded)
	leaves := relation.leaves()
	columns := make([]queryGrammarColumn, 0, 2*len(leaves)+1)
	for _, leaf := range leaves {
		for _, column := range []string{queryGrammarID, queryGrammarScore} {
			nullable := queryGrammarBaseNullable(column) || (padded[leaf.alias] && !queryGrammarFillsDefaults(engine))
			columns = append(columns, queryGrammarColumn{expression: leaf.alias + "." + column, alias: leaf.alias + "_" + column, nullable: nullable})
		}
	}
	first, last := columns[0], columns[len(columns)-2]
	columns = append(columns, queryGrammarColumn{expression: "COALESCE(" + last.expression + columnSeparator + first.expression + ")", alias: "either_id",
		nullable: first.nullable && last.nullable})
	return columns
}

// queryGrammarJoinOn spells an equality between two keys with an offset.
//
// Takes right (string) which is the alias whose key is shifted to.
// Takes left (string) which is the alias whose key is shifted.
// Takes offset (int) which is added to the left key.
//
// Returns string which is the condition.
func queryGrammarJoinOn(right, left string, offset int) string {
	condition := right + ".id = " + left + ".id"
	if offset > 0 {
		condition += " + " + strconv.Itoa(offset)
	}
	return condition
}

// queryGrammarJoinQuery wraps a FROM tree into a query.
//
// Takes engine (string) which selects the padding rule.
// Takes relation (*queryGrammarRelation) which is the FROM tree.
// Takes features ([]string) which names the constructs used.
//
// Returns queryGrammarQuery which projects every leaf.
func queryGrammarJoinQuery(engine string, relation *queryGrammarRelation, features []string) queryGrammarQuery {
	columns := queryGrammarJoinColumns(engine, relation)
	return queryGrammarQuery{sql: queryGrammarSelect(columns) + " FROM " + relation.render(), columns: columns, features: features, ordered: false}
}

// queryGrammarOuterJoinQuery builds one inner or outer join of two relations.
//
// Takes engine (string) which selects the join kinds.
// Takes choices (queryGrammarChoices) which selects tables, kind and key offset.
//
// Returns queryGrammarQuery which projects both relations.
func queryGrammarOuterJoinQuery(engine string, choices queryGrammarChoices) queryGrammarQuery {
	kinds := queryGrammarSupportedOf(engine, queryGrammarJoinPrefix, queryGrammarJoinKinds)
	kind := kinds[choices.pick(1, len(kinds))]
	offset := choices.pick(queryGrammarChoice3, queryGrammarOffsetCount)
	left := queryGrammarLeaf(choices.pick(0, 2), queryGrammarAliasA, 0)
	right := queryGrammarLeaf(choices.pick(2, queryGrammarOffsetCount), queryGrammarAliasB, offset)
	return queryGrammarJoinQuery(engine, queryGrammarJoin(left, kind, right, queryGrammarJoinOn(queryGrammarAliasB, queryGrammarAliasA, offset)), []string{queryGrammarJoinPrefix + kind})
}

// queryGrammarJoinGroupQuery builds a parenthesised join group under another join.
//
// Takes engine (string) which selects the join kinds.
// Takes choices (queryGrammarChoices) which selects kinds, nesting side and key offsets.
//
// Returns queryGrammarQuery which projects all three relations.
func queryGrammarJoinGroupQuery(engine string, choices queryGrammarChoices) queryGrammarQuery {
	kinds := queryGrammarSupportedOf(engine, queryGrammarJoinPrefix, queryGrammarJoinKinds)
	outer, inner := kinds[choices.pick(0, len(kinds))], kinds[choices.pick(1, len(kinds))]
	offset := choices.pick(queryGrammarChoice3, queryGrammarOffsetCount)
	first := queryGrammarLeaf(choices.pick(queryGrammarChoice4, 2), queryGrammarAliasA, 0)
	second := queryGrammarLeaf(1, queryGrammarAliasB, 0)
	third := queryGrammarLeaf(choices.pick(queryGrammarChoice5, queryGrammarOffsetCount), queryGrammarAliasC, offset)
	var relation *queryGrammarRelation
	nesting := "group/right-nested"
	if choices.pick(2, 2) == 0 {
		group := queryGrammarJoin(second, inner, third, queryGrammarJoinOn(queryGrammarAliasC, queryGrammarAliasB, offset))
		group.grouped = true
		relation = queryGrammarJoin(first, outer, group, queryGrammarJoinOn([]string{queryGrammarAliasB, queryGrammarAliasC}[choices.pick(queryGrammarChoice6, 2)], queryGrammarAliasA, 0))
	} else {
		nesting = "group/left-nested"
		group := queryGrammarJoin(first, inner, second, queryGrammarJoinOn(queryGrammarAliasB, queryGrammarAliasA, 0))
		group.grouped = true
		relation = queryGrammarJoin(group, outer, third, queryGrammarJoinOn(queryGrammarAliasC, []string{queryGrammarAliasA, queryGrammarAliasB}[choices.pick(queryGrammarChoice6, 2)], offset))
	}
	features := []string{nesting, queryGrammarJoinPrefix + outer, queryGrammarJoinPrefix + inner}
	return queryGrammarJoinQuery(engine, relation, features)
}

// queryGrammarCrossJoinQuery combines a cross join with an inner or outer join.
//
// Takes engine (string) which selects the join kinds and grouping.
// Takes choices (queryGrammarChoices) which selects the shape, kind and key offset.
//
// Returns queryGrammarQuery which projects all three relations.
func queryGrammarCrossJoinQuery(engine string, choices queryGrammarChoices) queryGrammarQuery {
	kinds := queryGrammarSupportedOf(engine, queryGrammarJoinPrefix, queryGrammarJoinKinds)
	kind := kinds[choices.pick(1, len(kinds))]
	offset := choices.pick(queryGrammarChoice3, queryGrammarOffsetCount)
	first := queryGrammarLeaf(choices.pick(2, 2), queryGrammarAliasA, 0)
	second := queryGrammarLeaf(1, queryGrammarAliasB, 0)
	third := queryGrammarLeaf(choices.pick(queryGrammarChoice4, 2), queryGrammarAliasC, 0)
	features := []string{queryGrammarJoinPrefix + kind}
	if choices.pick(0, 2) == 1 && queryGrammarSupports(engine, "cross/grouped") {
		group := queryGrammarJoin(second, queryGrammarCross, third, "")
		group.grouped = true
		return queryGrammarJoinQuery(engine, queryGrammarJoin(first, kind, group, queryGrammarJoinOn(queryGrammarAliasC, queryGrammarAliasA, offset)), append(features, "cross/grouped"))
	}
	relation := queryGrammarJoin(queryGrammarJoin(first, queryGrammarCross, second, ""), kind, third, queryGrammarJoinOn(queryGrammarAliasC, queryGrammarAliasA, offset))
	return queryGrammarJoinQuery(engine, relation, append(features, "cross/chained"))
}

// queryGrammarLateralQuery joins each row to a derived table that reads it.
//
// An aggregate without GROUP BY returns one row, so neither a cross nor a left lateral
// join pads its count. A LIMIT 1 derived table may return no row, so a left lateral join
// pads it and a cross lateral join drops the outer row.
//
// Takes choices (queryGrammarChoices) which selects the join form and comparison.
//
// Returns queryGrammarQuery which projects the outer row and the lateral columns.
func queryGrammarLateralQuery(choices queryGrammarChoices) queryGrammarQuery {
	comparison := []string{" > ", " >= ", " = "}[choices.pick(1, queryGrammarOffsetCount)]
	where := " FROM fuzz_peers p WHERE p.id" + comparison + "a.id"
	form := choices.pick(0, queryGrammarLateralForms)
	aggregate := form < 2
	left := form == 1 || form == queryGrammarLateralLeftLimit
	derived := "(SELECT p.id AS pid, p.score AS pscore" + where + " ORDER BY p.id LIMIT 1) l"
	columns := []queryGrammarColumn{{expression: "a.id", alias: "a_id", nullable: false}, {expression: "a.score", alias: "a_score", nullable: true},
		{expression: "l.pid", alias: "l_pid", nullable: left}, {expression: "l.pscore", alias: "l_pscore", nullable: true}}
	features := []string{"lateral/limit"}
	if aggregate {
		derived = "(SELECT COUNT(*) AS total, MAX(p.score) AS top" + where + ") l"
		columns[2] = queryGrammarColumn{expression: "l.total", alias: "l_total", nullable: false}
		columns[len(columns)-1] = queryGrammarColumn{expression: "l.top", alias: "l_top", nullable: true}
		features = []string{"lateral/aggregate"}
	}
	join := " CROSS JOIN LATERAL " + derived
	switch {
	case left:
		join = " LEFT JOIN LATERAL " + derived + " ON TRUE"
		features = append(features, "lateral/left")
	case form == queryGrammarLateralComma:
		join = ", LATERAL " + derived
		features = append(features, "lateral/comma")
	}
	return queryGrammarQuery{sql: queryGrammarSelect(columns) + " FROM fuzz_items a" + join, columns: columns, features: features, ordered: false}
}

// queryGrammarMergeQuery builds a USING or NATURAL join whose common columns merge.
//
// A merged column of an inner equi-join equals both sides, so it is never NULL. A left
// join takes the left value, a right join the right value, and a full join the first
// value present, which is NULL only when both sides admit NULL.
//
// Takes engine (string) which selects the join kinds and padding rule.
// Takes choices (queryGrammarChoices) which selects the tables, kind and key offset.
// Takes natural (bool) which merges every common column instead of the key.
//
// Returns queryGrammarQuery which projects the merged columns and both qualified keys.
func queryGrammarMergeQuery(engine string, choices queryGrammarChoices, natural bool) queryGrammarQuery {
	kinds := queryGrammarSupportedOf(engine, queryGrammarJoinPrefix, queryGrammarJoinKinds)
	kind := kinds[choices.pick(1, len(kinds))]
	left, right := queryGrammarLeaf(choices.pick(0, 2), queryGrammarAliasA, 0), queryGrammarLeaf(choices.pick(2, queryGrammarOffsetCount), queryGrammarAliasB, choices.pick(queryGrammarChoice3, 2))
	merged := []string{queryGrammarID}
	join := " " + strings.ToUpper(kind) + " JOIN "
	condition := " USING (id)"
	if natural {
		merged = slices.DeleteFunc(slices.Clone(left.columns), func(column string) bool { return !slices.Contains(right.columns, column) })
		join, condition = " NATURAL"+join, ""
		if kind == queryGrammarInner {
			join = " NATURAL JOIN "
		}
	}
	defaults := queryGrammarFillsDefaults(engine)
	columns := make([]queryGrammarColumn, 0, len(merged)+2)
	for _, column := range merged {
		base := queryGrammarBaseNullable(column)
		nullable := base && kind != queryGrammarInner
		columns = append(columns, queryGrammarColumn{expression: column, alias: "merged_" + column, nullable: nullable})
	}
	columns = append(columns, queryGrammarColumn{expression: "a.id", alias: "left_id", nullable: !defaults && (kind == queryGrammarRight || kind == queryGrammarFull)},
		queryGrammarColumn{expression: "b.id", alias: "right_id", nullable: !defaults && (kind == queryGrammarLeft || kind == queryGrammarFull)})
	features := []string{queryGrammarJoinPrefix + kind}
	if right.source != queryGrammarItems && right.source != queryGrammarPeers {
		features = append(features, "merge/derived")
	}
	sql := queryGrammarSelect(columns) + " FROM " + left.render() + join + right.render() + condition
	return queryGrammarQuery{sql: sql, columns: columns, features: features, ordered: false}
}
