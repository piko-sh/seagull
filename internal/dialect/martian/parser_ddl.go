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

// parseCreateTable parses CREATE TABLE name (element, ...).
//
// Returns *createTableStatement which is the parsed statement.
// Returns error when the statement is malformed.
func (p *parser) parseCreateTable() (*createTableStatement, error) {
	p.advance()
	if err := p.expectKeyword("TABLE"); err != nil {
		return nil, err
	}
	table, err := p.parseQualifiedName()
	if err != nil {
		return nil, err
	}
	created := &createTableStatement{table: table, columns: nil, primaryKey: nil}
	if err := p.expectSymbol(symbolOpen); err != nil {
		return nil, err
	}
	for {
		if err := p.parseTableElement(created); err != nil {
			return nil, err
		}
		if !p.acceptSymbol(symbolComma) {
			break
		}
	}
	return created, p.expectSymbol(symbolClose)
}

// parseTableElement parses one element of CREATE TABLE: a column definition or a
// table-level PRIMARY KEY.
//
// Takes created (*createTableStatement) which receives the element.
//
// Returns error when the element is malformed.
func (p *parser) parseTableElement(created *createTableStatement) error {
	if p.acceptKeyword(keywordPrimary) {
		if err := p.expectKeyword(keywordKey); err != nil {
			return err
		}
		names, err := p.parseNameList()
		if err != nil {
			return err
		}
		created.primaryKey = append(created.primaryKey, names...)
		return nil
	}
	column, primary, err := p.parseColumnDefinition()
	if err != nil {
		return err
	}
	created.columns = append(created.columns, column)
	if primary {
		created.primaryKey = append(created.primaryKey, column.name)
	}
	return nil
}

// parseColumnDefinition parses a column name, its type and its constraints.
//
// Returns columnDefinition which is the parsed column.
// Returns bool which is true when the column is declared PRIMARY KEY inline.
// Returns error when the definition is malformed.
func (p *parser) parseColumnDefinition() (columnDefinition, bool, error) {
	name, err := p.expectName()
	if err != nil {
		return columnDefinition{}, false, err
	}
	if p.current().kind != tokenIdentifier {
		return columnDefinition{}, false, p.unexpected("a type name")
	}
	column := columnDefinition{name: name, typeName: p.advance().value, modifiers: nil, notNull: false, hasDefault: false}
	if p.current().isSymbol(symbolOpen) {
		column.modifiers, err = p.parseIntegerList()
		if err != nil {
			return columnDefinition{}, false, err
		}
	}
	primary, err := p.parseColumnConstraints(&column)
	return column, primary, err
}

// parseColumnConstraints parses NOT NULL, NULL, DEFAULT and PRIMARY KEY after a column's
// type.
//
// Takes column (*columnDefinition) which receives the constraints.
//
// Returns bool which is true when the column is declared PRIMARY KEY.
// Returns error when a constraint is malformed.
func (p *parser) parseColumnConstraints(column *columnDefinition) (bool, error) {
	primary := false
	for {
		switch {
		case p.acceptKeyword(keywordNot):
			if err := p.expectKeyword(keywordNull); err != nil {
				return false, err
			}
			column.notNull = true
		case p.acceptKeyword(keywordNull):
			column.notNull = false
		case p.acceptKeyword("DEFAULT"):
			if _, err := p.parsePrimary(); err != nil {
				return false, err
			}
			column.hasDefault = true
		case p.acceptKeyword(keywordPrimary):
			if err := p.expectKeyword(keywordKey); err != nil {
				return false, err
			}
			column.notNull = true
			primary = true
		default:
			return primary, nil
		}
	}
}
