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

// parseSelect parses SELECT items FROM table-or-call [WHERE condition] [ORDER BY
// entries].
//
// Returns *selectStatement which is the parsed statement.
// Returns error when the statement is malformed.
func (p *parser) parseSelect() (*selectStatement, error) {
	p.advance()
	selected := &selectStatement{
		where:         nil,
		from:          tableReference{},
		items:         nil,
		orderBy:       nil,
		whereClause:   clause{},
		orderByClause: clause{},
		projection:    ast.Span{},
		fromClause:    ast.Span{},
	}
	projectionStart := p.current().offset
	for {
		item, err := p.parseSelectItem()
		if err != nil {
			return nil, err
		}
		selected.items = append(selected.items, item)
		if !p.acceptSymbol(symbolComma) {
			break
		}
	}
	selected.projection = p.spanFrom(projectionStart)
	fromStart := p.current().offset
	if err := p.expectKeyword(keywordFrom); err != nil {
		return nil, err
	}
	from, err := p.parseFromItem()
	if err != nil {
		return nil, err
	}
	selected.from = from
	selected.fromClause = p.spanFrom(fromStart)
	if selected.where, selected.whereClause, err = p.parseOptionalWhere(); err != nil {
		return nil, err
	}
	selected.orderBy, selected.orderByClause, err = p.parseOptionalOrderBy()
	return selected, err
}

// parseSelectItem parses one SELECT list item: * or an expression with an optional alias.
//
// Returns selectItem which is the parsed item.
// Returns error when the item is malformed.
func (p *parser) parseSelectItem() (selectItem, error) {
	start := p.current().offset
	if p.acceptSymbol("*") {
		end := p.previousEnd()
		return selectItem{expression: nil, alias: "", start: start, expressionEnd: end, aliasStart: end, end: end, star: true}, nil
	}
	value, err := p.parseExpression()
	if err != nil {
		return selectItem{}, err
	}
	expressionEnd := p.previousEnd()
	aliasStart := p.current().offset
	alias, err := p.parseOptionalAlias()
	end := p.previousEnd()
	if alias == "" {
		aliasStart = end
	}
	return selectItem{expression: value, alias: alias, start: start, expressionEnd: expressionEnd, aliasStart: aliasStart,
		end: end, star: false}, err
}

// parseOptionalWhere parses a WHERE clause when one follows.
//
// Returns expression which is the condition, or nil when there is no WHERE.
// Returns clause which locates the clause, or is zero when there is no WHERE.
// Returns error when the condition is malformed.
func (p *parser) parseOptionalWhere() (expression, clause, error) {
	start := p.current().offset
	if !p.acceptKeyword(keywordWhere) {
		return nil, clause{}, nil
	}
	bodyStart := p.current().offset
	condition, err := p.parseExpression()
	return condition, clause{whole: p.spanFrom(start), body: p.spanFrom(bodyStart)}, err
}

// parseOptionalOrderBy parses an ORDER BY clause when one follows.
//
// Returns []orderItem which holds the entries, or nil when there is no ORDER BY.
// Returns clause which locates the clause, or is zero when there is no ORDER BY.
// Returns error when the clause is malformed.
func (p *parser) parseOptionalOrderBy() ([]orderItem, clause, error) {
	start := p.current().offset
	if !p.acceptKeyword("ORDER") {
		return nil, clause{}, nil
	}
	if err := p.expectKeyword("BY"); err != nil {
		return nil, clause{}, err
	}
	bodyStart := p.current().offset
	var entries []orderItem
	for {
		value, err := p.parseExpression()
		if err != nil {
			return nil, clause{}, err
		}
		descending := p.acceptKeyword("DESC")
		if !descending {
			p.acceptKeyword("ASC")
		}
		entries = append(entries, orderItem{expression: value, descending: descending})
		if !p.acceptSymbol(symbolComma) {
			return entries, clause{whole: p.spanFrom(start), body: p.spanFrom(bodyStart)}, nil
		}
	}
}

// parseInsert parses INSERT INTO table (columns) VALUES (values).
//
// Returns *insertStatement which is the parsed statement.
// Returns error when the statement is malformed or the value count differs from the
// column count.
func (p *parser) parseInsert() (*insertStatement, error) {
	p.advance()
	if err := p.expectKeyword("INTO"); err != nil {
		return nil, err
	}
	into, err := p.parseTableReference()
	if err != nil {
		return nil, err
	}
	columns, err := p.parseNameList()
	if err != nil {
		return nil, err
	}
	if err := p.expectKeyword("VALUES"); err != nil {
		return nil, err
	}
	rowStart := p.current().offset
	if err := p.expectSymbol(symbolOpen); err != nil {
		return nil, err
	}
	var values []expression
	var valueSpans []ast.Span
	for {
		valueStart := p.current().offset
		value, valueError := p.parseExpression()
		if valueError != nil {
			return nil, valueError
		}
		values = append(values, value)
		valueSpans = append(valueSpans, p.spanFrom(valueStart))
		if !p.acceptSymbol(symbolComma) {
			break
		}
	}
	if err := p.expectSymbol(symbolClose); err != nil {
		return nil, err
	}
	if len(values) != len(columns) {
		return nil, p.unexpected("one value per named column")
	}
	return &insertStatement{into: into, columns: columns, values: values, valueSpans: valueSpans, row: p.spanFrom(rowStart)}, nil
}

// parseUpdate parses UPDATE table SET column = value, ... [WHERE condition].
//
// Returns *updateStatement which is the parsed statement.
// Returns error when the statement is malformed.
func (p *parser) parseUpdate() (*updateStatement, error) {
	p.advance()
	target, err := p.parseTableReference()
	if err != nil {
		return nil, err
	}
	if err := p.expectKeyword("SET"); err != nil {
		return nil, err
	}
	updated := &updateStatement{where: nil, target: target, assignments: nil, whereClause: clause{}}
	for {
		columnStart := p.current().offset
		column, nameError := p.expectName()
		if nameError != nil {
			return nil, nameError
		}
		columnSpan := p.spanFrom(columnStart)
		if err := p.expectSymbol("="); err != nil {
			return nil, err
		}
		value, valueError := p.parseExpression()
		if valueError != nil {
			return nil, valueError
		}
		updated.assignments = append(updated.assignments, assignment{value: value, column: column, columnSpan: columnSpan})
		if !p.acceptSymbol(symbolComma) {
			break
		}
	}
	updated.where, updated.whereClause, err = p.parseOptionalWhere()
	return updated, err
}

// parseDelete parses DELETE FROM table [WHERE condition].
//
// Returns *deleteStatement which is the parsed statement.
// Returns error when the statement is malformed.
func (p *parser) parseDelete() (*deleteStatement, error) {
	p.advance()
	if err := p.expectKeyword(keywordFrom); err != nil {
		return nil, err
	}
	from, err := p.parseTableReference()
	if err != nil {
		return nil, err
	}
	where, whereClause, err := p.parseOptionalWhere()
	return &deleteStatement{where: where, from: from, whereClause: whereClause}, err
}
