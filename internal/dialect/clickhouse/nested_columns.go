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

package clickhouse

import (
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// nestedFieldSeparator joins the name of a Nested column and the name of one of its
	// fields into the name of the column ClickHouse stores the field as, such as
	// legs.colony.
	nestedFieldSeparator = "."

	// nestedTypePrefix starts the declaration of a Nested column's type, compared without
	// regard to case.
	nestedTypePrefix = "nested("
)

// parseTableColumns reads one column definition of a table, which a Nested column
// declares several columns with.
//
// ClickHouse stores a Nested(x T, y U) column n as one array column per field, n.x of
// type Array(T) and n.y of type Array(U): DESCRIBE, SELECT * and an INSERT's column list
// see those, and DROP COLUMN n drops them all. A query may still name n itself, which
// reads the fields as one array of tuples, so n is kept as a hidden column of that type:
// it can be named, but SELECT * leaves it out.
//
// Returns []sqlmodel.Column which holds the column, or the columns a Nested column is
// stored as.
// Returns error when the definition is malformed.
func (p *parser) parseTableColumns() ([]sqlmodel.Column, error) {
	column, nested, err := p.parseColumnDefinition()
	if err != nil {
		return nil, err
	}
	if !nested {
		return []sqlmodel.Column{column}, nil
	}
	return nestedColumns(&column), nil
}

// parseColumnPath reads the name of a column in DDL, which for a field of a Nested column
// is the column's name and the field's joined by a dot, such as legs.colony, written as
// two names or quoted as one.
//
// Returns string which is the column's name.
// Returns error when no name is next.
func (p *parser) parseColumnPath() (string, error) {
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return "", err
	}
	for p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
		p.Advance()
		name += nestedFieldSeparator + p.Current().Value
		p.Advance()
	}
	return name, nil
}

// nestedColumns returns the columns ClickHouse stores a Nested column as: the column
// itself, hidden, typed as the array of tuples it reads as, and one array column per
// field, named after the column and the field.
//
// Takes column (*sqlmodel.Column) which is the Nested column, typed as an array of
// tuples.
//
// Returns []sqlmodel.Column which holds the hidden column and its fields' columns.
func nestedColumns(column *sqlmodel.Column) []sqlmodel.Column {
	whole := *column
	whole.Hidden = true
	columns := []sqlmodel.Column{whole}
	if column.SQLType.ElementType == nil {
		return columns
	}
	fields := column.SQLType.ElementType.StructFields
	for index := range fields {
		columns = append(columns, sqlmodel.NewColumn(column.Name+nestedFieldSeparator+fields[index].Name, arrayOf(fields[index].SQLType), false))
	}
	return columns
}

// declaresNested reports whether a column's type text declares a Nested column.
//
// Takes typeText (string) which is the column's type as written.
//
// Returns bool which is true for Nested(...).
func declaresNested(typeText string) bool {
	return len(typeText) >= len(nestedTypePrefix) && strings.EqualFold(typeText[:len(nestedTypePrefix)], nestedTypePrefix)
}
