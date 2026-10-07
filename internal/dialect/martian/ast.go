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

package martian

import "seagull.sh/seagull/internal/dialect/ast"

// statement is one parsed Martian statement, carried as the Raw of a ast.ParsedStatement.
type statement struct {
	// createTable is set for CREATE TABLE.
	createTable *createTableStatement

	// selection is set for SELECT.
	selection *selectStatement

	// insertion is set for INSERT.
	insertion *insertStatement

	// update is set for UPDATE.
	update *updateStatement

	// deletion is set for DELETE.
	deletion *deleteStatement

	// source is the text the statement was parsed from; offsets index into it.
	source string

	// tokens holds the statement's tokens.
	tokens []token
}

// IsParsedStatement marks statement as a parsed statement.
func (*statement) IsParsedStatement() {}

// syntaxErrorAtStart returns the syntax error at the statement's first token.
//
// Takes message (string) which says what was wrong.
//
// Returns error which is the located syntax error.
func (s *statement) syntaxErrorAtStart(message string) error {
	if len(s.tokens) == 0 {
		return newSyntaxError(s.source, len(s.source), len(s.source), message)
	}
	return newSyntaxError(s.source, s.tokens[0].offset, s.tokens[0].end, message)
}

// qualifiedName is a table name with an optional schema.
type qualifiedName struct {
	// schema is the schema, or empty for the default.
	schema string

	// name is the table name.
	name string
}

// createTableStatement is CREATE TABLE.
type createTableStatement struct {
	// table names the new table.
	table qualifiedName

	// columns holds the column definitions in order.
	columns []columnDefinition

	// primaryKey holds the primary key columns.
	primaryKey []string
}

// columnDefinition is one column of CREATE TABLE.
type columnDefinition struct {
	// name is the column name.
	name string

	// typeName is the Martian type name, upper-cased.
	typeName string

	// modifiers holds the type's numeric modifiers, such as a TEXTUAL length.
	modifiers []int

	// notNull reports NOT NULL.
	notNull bool

	// hasDefault reports a DEFAULT clause.
	hasDefault bool
}

// clause locates a clause of a statement as offsets into the source: the clause from its
// keyword, and its body after the keyword.
type clause struct {
	// whole covers the clause, its keyword included.
	whole ast.Span

	// body covers what follows the keyword.
	body ast.Span
}

// tableReference is the table a DML statement reads or writes, or the function call a
// SELECT reads its one row from.
type tableReference struct {
	// call is the function a SELECT reads from, or nil for a table.
	call *functionExpression

	// table names the table.
	table qualifiedName

	// alias is the alias, or empty.
	alias string
}

// selectItem is one item of a SELECT list.
type selectItem struct {
	// expression is the selected expression; nil for *.
	expression expression

	// alias is the AS alias, or empty.
	alias string

	// start is the offset of the item's first byte.
	start int

	// expressionEnd is the offset one past its expression's last byte.
	expressionEnd int

	// aliasStart is the offset of its alias's first byte, or end when it has none.
	aliasStart int

	// end is the offset one past the item's last byte.
	end int

	// star reports a bare *.
	star bool
}

// orderItem is one ORDER BY entry.
type orderItem struct {
	// expression is the ordering expression.
	expression expression

	// descending reports DESC.
	descending bool
}

// selectStatement is SELECT.
type selectStatement struct {
	// where is the WHERE condition, or nil.
	where expression

	// from is the table read.
	from tableReference

	// items holds the SELECT list.
	items []selectItem

	// orderBy holds the ORDER BY entries.
	orderBy []orderItem

	// whereClause locates the WHERE clause, when there is one.
	whereClause clause

	// orderByClause locates the ORDER BY clause, when there is one.
	orderByClause clause

	// projection locates the SELECT list.
	projection ast.Span

	// fromClause locates the FROM clause.
	fromClause ast.Span
}

// insertStatement is INSERT ... VALUES.
type insertStatement struct {
	// into is the table written.
	into tableReference

	// columns names the target columns.
	columns []string

	// values holds the VALUES row, one expression per column.
	values []expression

	// valueSpans locates each value of the row.
	valueSpans []ast.Span

	// row locates the VALUES row, its parentheses included.
	row ast.Span
}

// assignment is one SET entry of UPDATE.
type assignment struct {
	// value is the assigned expression.
	value expression

	// column is the assigned column.
	column string

	// columnSpan locates the assigned column.
	columnSpan ast.Span
}

// updateStatement is UPDATE.
type updateStatement struct {
	// where is the WHERE condition, or nil.
	where expression

	// target is the table written.
	target tableReference

	// assignments holds the SET entries.
	assignments []assignment

	// whereClause locates the WHERE clause, when there is one.
	whereClause clause
}

// deleteStatement is DELETE.
type deleteStatement struct {
	// where is the WHERE condition, or nil.
	where expression

	// from is the table written.
	from tableReference

	// whereClause locates the WHERE clause, when there is one.
	whereClause clause
}

// expression is a Martian expression node.
type expression interface {
	// martianExpression marks the node as a Martian expression.
	martianExpression()
}

// columnExpression references a column.
type columnExpression struct {
	// qualifier is the table or alias, or empty.
	qualifier string

	// name is the column name.
	name string

	// span locates the reference, its qualifier included, as offsets into the source.
	span ast.Span
}

// martianExpression marks columnExpression as an expression.
func (*columnExpression) martianExpression() {}

// parameterExpression is an @pN placeholder.
type parameterExpression struct {
	// number is N.
	number int
}

// martianExpression marks parameterExpression as an expression.
func (*parameterExpression) martianExpression() {}

// literalExpression is a number or string literal.
type literalExpression struct {
	// typeName is the Martian type of the literal.
	typeName string
}

// martianExpression marks literalExpression as an expression.
func (*literalExpression) martianExpression() {}

// functionExpression is a function call.
type functionExpression struct {
	// name is the function name, upper-cased.
	name string

	// arguments holds the arguments; nil with star set for COUNT(*).
	arguments []expression

	// nameSpan locates the function's name as offsets into the source.
	nameSpan ast.Span

	// star reports a * argument.
	star bool
}

// martianExpression marks functionExpression as an expression.
func (*functionExpression) martianExpression() {}

// binaryExpression is an arithmetic, comparison or logical operator.
type binaryExpression struct {
	// left is the left operand.
	left expression

	// right is the right operand.
	right expression

	// operator is the operator, such as +, = or AND.
	operator string
}

// martianExpression marks binaryExpression as an expression.
func (*binaryExpression) martianExpression() {}

// nullTestExpression is IS NULL or IS NOT NULL.
type nullTestExpression struct {
	// operand is the tested expression.
	operand expression

	// negated reports IS NOT NULL.
	negated bool
}

// martianExpression marks nullTestExpression as an expression.
func (*nullTestExpression) martianExpression() {}

// inListExpression is operand IN (value, ...).
type inListExpression struct {
	// operand is the tested expression.
	operand expression

	// values holds the list members.
	values []expression
}

// martianExpression marks inListExpression as an expression.
func (*inListExpression) martianExpression() {}

// convertExpression is CONVERT(operand TO type), Martian's conversion of a value to a
// type.
type convertExpression struct {
	// operand is the converted expression.
	operand expression

	// typeName is the target type's name, upper-cased.
	typeName string

	// modifiers holds the target type's parenthesised modifiers, such as the length of
	// TEXTUAL(80).
	modifiers []int
}

// martianExpression marks convertExpression as an expression.
func (*convertExpression) martianExpression() {}
