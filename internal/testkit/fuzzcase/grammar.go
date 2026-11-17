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

// grammarVersion selects the composable grammar without changing version one.
const grammarVersion = 2

// grammarSourceCount bounds the permanently assigned relation operations.
const grammarSourceCount = 5

// expressionDepth bounds nested expression nodes.
const expressionDepth = 5

// expressionNodes bounds branching operations across the expression forest.
const expressionNodes = 64

// expressionParameters bounds placeholder occurrences across the query.
const expressionParameters = 8

// grammarDistinct enables duplicate elimination.
const grammarDistinct = 2

// grammarGrouped enables grouped aggregate projections.
const grammarGrouped = 4

// grammarWindow enables a deterministic window projection.
const grammarWindow = 8

// grammarExpressionByte locates the first expression operation.
const grammarExpressionByte = 10

// grammarProjectionByte locates the projection count.
const grammarProjectionByte = 8

// grammarProjectionCount bounds additional output expressions.
const grammarProjectionCount = 3

// grammarIdentityCount bounds distinct logical parameter identities.
const grammarIdentityCount = 4

// grammarLiteralRange bounds literal magnitudes.
const grammarLiteralRange = 17

// grammarLiteralOffset centres generated literals around zero.
const grammarLiteralOffset = 8

// peerScoreColumn identifies the nullable joined column.
const peerScoreColumn = 3

// comparisonCount bounds comparison operations.
const comparisonCount = 6

// expressionOpen starts a grouped expression.
const expressionOpen = "("

// expressionClose ends a grouped expression.
const expressionClose = ")"

// expressionSpace separates SQL operands.
const expressionSpace = " "

// expressionColumn identifies a modelled relation column.
const expressionColumn = "column"

// expressionLiteral identifies a bounded constant operand.
const expressionLiteral = "literal"

const (
	// sourceTable identifies a stable grammar operation.
	sourceTable = iota

	// sourceLeftJoin identifies a stable grammar operation.
	sourceLeftJoin

	// sourceInnerJoin identifies a stable grammar operation.
	sourceInnerJoin

	// sourceCTE identifies a stable grammar operation.
	sourceCTE

	// sourceDerived identifies a stable grammar operation.
	sourceDerived
)

const (
	// numericColumn identifies a stable grammar operation.
	numericColumn = iota

	// numericLiteral identifies a stable grammar operation.
	numericLiteral

	// numericParameter identifies a stable grammar operation.
	numericParameter

	// numericAdd identifies a stable grammar operation.
	numericAdd

	// numericSubtract identifies a stable grammar operation.
	numericSubtract

	// numericCoalesce identifies a stable grammar operation.
	numericCoalesce

	// numericCase identifies a stable grammar operation.
	numericCase

	// numericNegate identifies a stable grammar operation.
	numericNegate

	// numericOperationCount counts the numeric grammar operations.
	numericOperationCount
)

const (
	// booleanComparison identifies a stable grammar operation.
	booleanComparison = iota

	// booleanAnd identifies a stable grammar operation.
	booleanAnd

	// booleanOr identifies a stable grammar operation.
	booleanOr

	// booleanNot identifies a stable grammar operation.
	booleanNot

	// booleanNull identifies a stable grammar operation.
	booleanNull

	// booleanNotNull identifies a stable grammar operation.
	booleanNotNull

	// booleanIn identifies a stable grammar operation.
	booleanIn

	// booleanNotIn identifies a stable grammar operation.
	booleanNotIn

	// booleanOperationCount counts the Boolean grammar operations.
	booleanOperationCount
)

// expression is a typed node in the independent query grammar.
type expression struct {
	// operation is a stable decoder operation.
	operation string

	// children holds operands with types determined by the operation.
	children []expression

	// value identifies a column, literal or logical parameter.
	value int
}

// grammarReader decodes bounded numeric and Boolean expression trees.
type grammarReader struct {
	// model owns the versioned choices.
	model Model

	// position identifies the next choice byte.
	position int

	// remaining bounds branching operations across all expressions.
	remaining int

	// parameters counts decoded parameter occurrences.
	parameters int
}

// expressionWriter renders an independent tree with chosen aliases and bindings.
type expressionWriter struct {
	// aliases names the item and peer relations.
	aliases [2]string

	// parameter maps logical identities to placeholders or literal values.
	parameter func(int) string

	// integer names an engine's signed integer CAST type.
	integer string

	// parentheses adds redundant grouping for metamorphic checks.
	parentheses bool
}

// grammarQuery renders a composable model and its independent references.
//
// Takes rename (bool) which selects consistent local alias renaming.
// Takes parentheses (bool) which adds redundant expression grouping.
//
// Returns Query which supplies SQL and binding expectations.
func (m Model) grammarQuery(rename, parentheses bool) Query {
	projections, predicate := m.grammarExpressions()
	return m.writeGrammar(projections, predicate, rename, parentheses)
}

// grammarExpressions decodes typed output and predicate expressions.
//
// Returns []expression which holds numeric output expressions.
// Returns expression which is a Boolean predicate.
func (m Model) grammarExpressions() ([]expression, expression) {
	reader := grammarReader{model: m, position: grammarExpressionByte, remaining: expressionNodes, parameters: 0}
	projections := make([]expression, 1+int(m.at(grammarProjectionByte)%grammarProjectionCount))
	for index := range projections {
		projections[index] = reader.numeric(expressionDepth)
	}
	predicate := reader.boolean(expressionDepth)
	return projections, predicate
}

// writeGrammar renders typed expressions with chosen aliases and grouping.
//
// Takes projections ([]expression) which holds numeric outputs.
// Takes predicate (expression) which filters input rows.
// Takes rename (bool) which selects alternate aliases.
// Takes parentheses (bool) which adds redundant grouping.
//
// Returns Query which includes independent reference SQL and bindings.
func (m Model) writeGrammar(projections []expression, predicate expression, rename, parentheses bool) Query {
	aliases := [2]string{"i", "p"}
	if rename {
		aliases = [2]string{"renamed_items", "renamed_peers"}
	}
	writer := expressionWriter{aliases: aliases, parentheses: parentheses, parameter: nil, integer: m.integerType()}
	values := make([]int64, grammarIdentityCount)
	for index := range values {
		values[index] = int64(m.at(valueByte+index)%parameterRange) - int64(index+1)
	}
	var bindings, occurrences []int64
	numbers := make(map[int]int)
	writer.parameter = func(identity int) string {
		value := values[identity]
		occurrences = append(occurrences, value)
		if mysqlFamily(m.Engine) {
			bindings = append(bindings, value)
			return "?"
		}
		number, exists := numbers[identity]
		if !exists {
			number = len(numbers) + 1
			numbers[identity] = number
			bindings = append(bindings, value)
		}
		return integerPlaceholder(m.Engine, number)
	}
	sql, columns := m.grammarBody(writer, projections, predicate)
	writer.parameter = func(identity int) string { return strconv.FormatInt(values[identity], 10) }
	reference, _ := m.grammarBody(writer, projections, predicate)
	countReference := reference
	if m.Flags&1 != 0 {
		countReference += m.ending(columns)
	}
	return Query{
		SQL:       "-- sgl.query(name: FuzzQuery, command: many, dynamic: runtime)\n" + sql + m.ending(columns) + ";",
		Reference: reference + m.ending(columns), CountReference: countReference,
		Values: integerValues(bindings), Occurrences: integerValues(occurrences), Columns: columns, Writes: false, Ordered: true,
	}
}

// integerType returns the model's exact signed integer type.
//
// Returns string which can be used in a CAST expression.
func (m Model) integerType() string {
	switch m.Engine {
	case engineClickHouse:
		return "Int64"
	case engineSQLite:
		return "INTEGER"
	case engineMySQL, engineMariaDB:
		return "SIGNED"
	default:
		return "BIGINT"
	}
}

// grammarBody composes projections, a relation and a typed predicate.
//
// Takes writer (expressionWriter) which renders independent expression nodes.
// Takes projections ([]expression) which holds numeric output expressions.
// Takes predicate (expression) which holds a Boolean filter.
//
// Returns string which contains SQL without removable outer clauses.
// Returns int which is the output width.
func (m Model) grammarBody(writer expressionWriter, projections []expression, predicate expression) (string, int) {
	alias := writer.aliases[0]
	columns := []string{alias + ".id AS id"}
	grouped := m.Flags&grammarGrouped != 0
	if grouped {
		columns = []string{alias + ".category AS category", "COUNT(*) AS total"}
	}
	for index, projection := range projections {
		value := writer.write(projection)
		if grouped {
			value = "MAX(" + value + ")"
		}
		columns = append(columns, fmt.Sprintf("%s AS value_%d", value, index+1))
	}
	if !grouped && m.Flags&grammarWindow != 0 {
		columns = append(columns, "ROW_NUMBER() OVER (PARTITION BY "+alias+".category ORDER BY "+alias+".id) AS position")
	}
	prefix, relation := m.grammarRelation(writer.aliases)
	selectSQL := "SELECT "
	if m.Flags&grammarDistinct != 0 {
		selectSQL += "DISTINCT "
	}
	sql := prefix + selectSQL + strings.Join(columns, columnSeparator) + " FROM " + relation + " WHERE " + writer.write(predicate)
	if grouped {
		sql += " GROUP BY " + alias + ".category HAVING COUNT(*) >= 1"
	}
	return sql, len(columns)
}

// grammarRelation renders independently selected relation operations.
//
// Takes aliases ([2]string) which names the item and peer relations.
//
// Returns string which is an optional CTE prefix.
// Returns string which is the FROM relation.
func (m Model) grammarRelation(aliases [2]string) (prefix, relation string) {
	items := "fuzz_items " + aliases[0]
	peers := "fuzz_peers " + aliases[1] + " ON " + aliases[1] + ".id = " + aliases[0] + ".id"
	switch m.Shape {
	case sourceLeftJoin:
		return "", items + " LEFT JOIN " + peers
	case sourceInnerJoin:
		return "", items + " INNER JOIN " + peers
	case sourceCTE:
		return "WITH chosen AS (SELECT id, score, category FROM fuzz_items) ", "chosen " + aliases[0]
	case sourceDerived:
		return "", "(SELECT id, score, category FROM fuzz_items) " + aliases[0]
	default:
		return "", items
	}
}

// next consumes a byte, using zero after the end of the input.
//
// Returns byte which selects a stable operation or operand.
func (r *grammarReader) next() byte {
	value := r.model.at(r.position)
	r.position++
	return value
}

// numeric decodes a numeric expression with bounded depth and size.
//
// Takes depth (int) which is the remaining nesting allowance.
//
// Returns expression which has an integer or nullable integer value.
func (r *grammarReader) numeric(depth int) expression {
	if depth <= 1 || r.remaining == 0 {
		return expression{operation: expressionColumn, value: 0, children: nil}
	}
	r.remaining--
	operation := r.next() % numericOperationCount
	switch operation {
	case numericColumn:
		column := int(r.next() % tableColumns)
		if (r.model.Shape == sourceLeftJoin || r.model.Shape == sourceInnerJoin) && r.next()%2 != 0 {
			column = peerScoreColumn
		}
		return expression{operation: expressionColumn, value: column, children: nil}
	case numericLiteral:
		return expression{operation: expressionLiteral, value: int(r.next()%grammarLiteralRange) - grammarLiteralOffset, children: nil}
	case numericParameter:
		identity := int(r.next() % grammarIdentityCount)
		if r.parameters == expressionParameters {
			return expression{operation: expressionLiteral, value: identity, children: nil}
		}
		r.parameters++
		return expression{operation: "parameter", value: identity, children: nil}
	case numericAdd, numericSubtract:
		operation := "+"
		if r.model.at(r.position-1)%numericOperationCount == numericSubtract {
			operation = "-"
		}
		return expression{operation: operation, value: 0, children: []expression{r.numeric(depth - 1), r.numeric(depth - 1)}}
	case numericCoalesce:
		return expression{operation: "coalesce", value: 0, children: []expression{r.numeric(depth - 1), r.numeric(depth - 1)}}
	case numericCase:
		return expression{operation: "case", value: 0, children: []expression{r.boolean(depth - 1), r.numeric(depth - 1), r.numeric(depth - 1)}}
	default:
		return expression{operation: "negate", value: 0, children: []expression{r.numeric(depth - 1)}}
	}
}

// boolean decodes a predicate with bounded depth and size.
//
// Takes depth (int) which is the remaining nesting allowance.
//
// Returns expression which has a Boolean or nullable Boolean value.
func (r *grammarReader) boolean(depth int) expression {
	if depth <= 1 || r.remaining == 0 {
		return expression{operation: "true", value: 0, children: nil}
	}
	r.remaining--
	switch r.next() % booleanOperationCount {
	case booleanComparison:
		operators := [...]string{"=", "<>", ">", ">=", "<", "<="}
		operation := operators[int(r.next())%len(operators)]
		return expression{operation: operation, value: 0, children: []expression{r.numeric(depth - 1), r.numeric(depth - 1)}}
	case booleanAnd, booleanOr:
		operation := "AND"
		if r.model.at(r.position-1)%booleanOperationCount == booleanOr {
			operation = "OR"
		}
		return expression{operation: operation, value: 0, children: []expression{r.boolean(depth - 1), r.boolean(depth - 1)}}
	case booleanNot:
		return expression{operation: "NOT", value: 0, children: []expression{r.boolean(depth - 1)}}
	case booleanNull, booleanNotNull:
		operation := "IS NULL"
		if r.model.at(r.position-1)%booleanOperationCount == booleanNotNull {
			operation = "IS NOT NULL"
		}
		return expression{operation: operation, value: 0, children: []expression{r.numeric(depth - 1)}}
	default:
		operation := "IN"
		if r.model.at(r.position-1)%booleanOperationCount == booleanNotIn {
			operation = "NOT IN"
		}
		operands := []expression{r.numeric(depth - 1), r.numeric(depth - 1), r.numeric(depth - 1)}
		if r.model.Engine == engineClickHouse {
			operands[1] = constantMembershipOperand(operands[1])
			operands[2] = constantMembershipOperand(operands[2])
		}
		return expression{operation: operation, value: 0, children: operands}
	}
}

// write renders an expression while preserving its typed tree structure.
//
// Takes node (expression) which owns its operands.
//
// Returns string which is a SQL expression.
func (w expressionWriter) write(node expression) string {
	sql := w.body(node)
	if w.parentheses {
		return expressionOpen + sql + expressionClose
	}
	return sql
}

// body renders one operation and recursively renders its operands.
//
// Takes node (expression) which has a stable operation and typed children.
//
// Returns string which is the SQL expression body.
func (w expressionWriter) body(node expression) string {
	switch node.operation {
	case expressionColumn:
		columns := [...]string{w.aliases[0] + ".id", w.aliases[0] + ".score", w.aliases[0] + ".category", w.aliases[1] + ".score"}
		return columns[node.value]
	case expressionLiteral:
		return "CAST(" + strconv.Itoa(node.value) + " AS " + w.integer + expressionClose
	case "parameter":
		return "CAST(" + w.parameter(node.value) + " AS " + w.integer + expressionClose
	case "true":
		return "1 = 1"
	case "coalesce":
		return "COALESCE(" + w.write(node.children[0]) + columnSeparator + w.write(node.children[1]) + expressionClose
	case "case":
		return "CASE WHEN " + w.write(node.children[0]) + " THEN " + w.write(node.children[1]) + " ELSE " + w.write(node.children[2]) + " END"
	case "negate":
		return "(-" + w.write(node.children[0]) + expressionClose
	case "NOT":
		return "(NOT (" + w.write(node.children[0]) + "))"
	case "IS NULL", "IS NOT NULL":
		return expressionOpen + w.write(node.children[0]) + expressionSpace + node.operation + expressionClose
	case "IN", "NOT IN":
		return expressionOpen + w.write(node.children[0]) + expressionSpace + node.operation + " (" + w.write(node.children[1]) + columnSeparator + w.write(node.children[2]) + "))"
	default:
		return expressionOpen + w.write(node.children[0]) + expressionSpace + node.operation + expressionSpace + w.write(node.children[1]) + expressionClose
	}
}

// Variants supplies controlled rewrites from the independent syntax tree.
//
// Returns []Query which retains values, output names and logical bindings.
func (m Model) Variants() []Query {
	if m.at(0) != grammarVersion {
		return nil
	}
	return []Query{m.grammarQuery(true, false), m.grammarQuery(false, true)}
}

// constantMembershipOperand removes row dependencies from a ClickHouse set element.
//
// Takes node (expression) which preserves its constants, parameters and operations.
//
// Returns expression which can appear in a native constant membership set.
func constantMembershipOperand(node expression) expression {
	if node.operation == expressionColumn {
		node.operation = expressionLiteral
	}
	for index := range node.children {
		node.children[index] = constantMembershipOperand(node.children[index])
	}
	return node
}

// grammarSeeds covers relations, modifiers and typed expression operations.
//
// Takes engine (string) which selects an admitted dialect.
//
// Returns [][]byte which contains stable compositional examples.
func grammarSeeds(engine string) [][]byte {
	if !supportsShape(engine, shapeSelect) {
		return nil
	}
	seeds := [][]byte{
		[]byte("\x0200000000071"),
		[]byte("\x0200000000000120"),
	}
	for source := range grammarSourceCount {
		for _, flags := range []byte{0, 1, grammarDistinct, grammarGrouped, grammarWindow, grammarDistinct | grammarGrouped | 1} {

			seeds = append(seeds, []byte{grammarVersion, byte(source), flags, maxRows, seedLower, seedUpper, 1, 2, 0, 0, numericAdd, 0, 1, 1, 2, 0, 1, 0, numericAdd, 2, 0, 0, 0, 1, 2, 1})
		}
	}
	for operation := range byte(numericOperationCount) {

		seeds = append(seeds,
			[]byte{grammarVersion, 1, 1, maxRows, seedLower, seedUpper, 1, 2, 0, 0, operation, 2, 0, 2, 1, 2, 0, 2, 1},
			[]byte{grammarVersion, 0, 0, maxRows, seedLower, seedUpper, 1, 2, 0, 0, 0, 0, operation, 2, 0, 2, 1, 2, 0, 2, 1},
		)
	}
	for comparison := range byte(comparisonCount) {
		seeds = append(seeds, []byte{grammarVersion, 0, 0, maxRows, seedLower, seedUpper, 1, 2, 0, 0, 0, 0, 0, comparison, 0, 1, 2, 0})
	}
	return seeds
}
