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

import "strings"

// Clause identifies one or more statement clauses where expressions or aliases occur.
//
// The zero value is an empty set.
type Clause uint16

const (
	// ClauseWhere is a WHERE clause, or a filter an engine applies like one before grouping,
	// such as ClickHouse's PREWHERE.
	ClauseWhere Clause = 1 << iota

	// ClauseJoinCondition is the ON condition of a join.
	ClauseJoinCondition

	// ClauseGroupBy is a GROUP BY clause.
	ClauseGroupBy

	// ClauseHaving is a HAVING clause.
	ClauseHaving

	// ClauseRowShaping is a clause an engine applies to the rows after HAVING, such as
	// QUALIFY or ClickHouse's LIMIT n BY.
	ClauseRowShaping

	// ClauseOrderBy is an ORDER BY clause.
	ClauseOrderBy

	// ClauseSetTarget is a column an UPDATE's SET clause assigns.
	ClauseSetTarget

	// ClauseSetValue is a value an UPDATE's SET clause assigns.
	ClauseSetValue

	// ClauseSelectList identifies select-list alias references to earlier items. Dialects
	// use it in Info.SelectAliases; clause expressions never use it.
	ClauseSelectList

	// ClauseInsertValue is a value of a row of an INSERT's VALUES list, recorded in the
	// order the rows and their values are written.
	ClauseInsertValue
)

var (
	// clauseKeywords names each clause of a Clause set, in the order String lists them.
	clauseKeywords = []struct {
		keyword string
		clause  Clause
	}{
		{keyword: "SELECT list", clause: ClauseSelectList},
		{keyword: "WHERE", clause: ClauseWhere},
		{keyword: "JOIN ON", clause: ClauseJoinCondition},
		{keyword: "GROUP BY", clause: ClauseGroupBy},
		{keyword: "HAVING", clause: ClauseHaving},
		{keyword: "row shaping", clause: ClauseRowShaping},
		{keyword: "ORDER BY", clause: ClauseOrderBy},
		{keyword: "SET target", clause: ClauseSetTarget},
		{keyword: "SET value", clause: ClauseSetValue},
		{keyword: "VALUES row value", clause: ClauseInsertValue},
	}
)

// Includes reports whether the set holds every clause of another set. The empty set is
// included in no set.
//
// Takes other (Clause) which is the set to look for.
//
// Returns bool which is true when other is not empty and every clause of it is in the
// set.
func (clause Clause) Includes(other Clause) bool {
	return other != 0 && clause&other == other
}

// String lists the clauses of the set, such as "GROUP BY, ORDER BY", or returns "none"
// for the empty set.
//
// Returns string which is the list.
func (clause Clause) String() string {
	keywords := make([]string, 0, len(clauseKeywords))
	for _, entry := range clauseKeywords {
		if clause&entry.clause != 0 {
			keywords = append(keywords, entry.keyword)
		}
	}
	if len(keywords) == 0 {
		return "none"
	}
	return strings.Join(keywords, ", ")
}

// ClauseExpression records an expression outside a statement's select list.
//
// The analyser uses it to validate clause references and locate unknown columns or
// functions.
type ClauseExpression struct {
	// Expression is the parsed expression. A column an UPDATE assigns is a
	// ColumnRefExpression.
	Expression Expression

	// Clause is the clause the expression sits in.
	Clause Clause
}
