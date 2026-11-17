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

package fuzzbuilder

import (
	"slices"
	"strconv"
)

// output evaluates a catalogue query over the modelled rows before any builder input.
//
// Takes kind (queryKind) which selects the query.
// Takes parameter (int64) which is the query's own parameter value.
//
// Returns [][]Cell which holds the query's rows in no particular order.
func (c Case) output(kind queryKind, parameter int64) [][]Cell {
	switch kind {
	case kindGrouped:
		return c.grouped()
	case kindDistinct:
		return c.distinct()
	case kindUnion:
		rows := c.project(kind)
		for _, peer := range c.Peers {
			rows = append(rows, []Cell{integerCell(peer.ID), nullCell()})
		}
		return rows
	case kindParameter:
		projected := c.project(kind)
		var rows [][]Cell
		for index, row := range c.Rows {
			if row.Group == parameter || row.Score == nil {
				rows = append(rows, projected[index])
			}
		}
		return rows
	default:
		return c.project(kind)
	}
}

// project maps every modelled row to a query's columns.
//
// Takes kind (queryKind) which selects the query.
//
// Returns [][]Cell which holds one output row per modelled row.
func (c Case) project(kind queryKind) [][]Cell {
	entry := &catalogue[kind]
	rows := make([][]Cell, 0, len(c.Rows))
	for _, row := range c.Rows {
		cells := make([]Cell, 0, len(entry.columns))
		for _, current := range entry.columns {
			cells = append(cells, c.value(row, current.value))
		}
		rows = append(rows, cells)
	}
	return rows
}

// value reads one modelled value of a row.
//
// Takes row (Row) which is the modelled row.
// Takes selected (field) which selects the value.
//
// Returns Cell which is the value.
func (c Case) value(row Row, selected field) Cell {
	switch selected {
	case fieldID:
		return integerCell(row.ID)
	case fieldGroup:
		return integerCell(row.Group)
	case fieldScore:
		return nullableCell(row.Score)
	case fieldLabel:
		return textCell(row.Label)
	case fieldMixed:
		return integerCell(row.Mixed)
	case fieldSelect:
		return textCell(row.Select)
	case fieldDotted:
		return nullableCell(row.Dotted)
	case fieldNote:
		for _, peer := range c.Peers {
			if peer.ID == row.ID && peer.Note != nil {
				return textCell(*peer.Note)
			}
		}
		return nullCell()
	case fieldScoreText:
		if row.Score == nil {
			return nullCell()
		}
		return textCell(strconv.FormatInt(*row.Score, decimalBase))
	default:
		if row.Score == nil {
			return nullCell()
		}
		return integerCell(*row.Score + row.Group)
	}
}

// grouped counts the rows of each category.
//
// Returns [][]Cell which holds one row per present category.
func (c Case) grouped() [][]Cell {
	var groups []int64
	counts := make(map[int64]int64)
	for _, row := range c.Rows {
		if counts[row.Group] == 0 {
			groups = append(groups, row.Group)
		}
		counts[row.Group]++
	}
	rows := make([][]Cell, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, []Cell{integerCell(group), integerCell(counts[group])})
	}
	return rows
}

// distinct removes duplicate category and label pairs.
//
// Returns [][]Cell which holds each pair once.
func (c Case) distinct() [][]Cell {
	var rows [][]Cell
	for _, row := range c.project(kindDistinct) {
		if !slices.ContainsFunc(rows, func(seen []Cell) bool { return slices.Equal(seen, row) }) {
			rows = append(rows, row)
		}
	}
	return rows
}

// integerCell builds a non-NULL integer cell.
//
// Takes value (int64) which is the integer.
//
// Returns Cell which holds it.
func integerCell(value int64) Cell {
	return Cell{Text: "", Integer: value, Null: false, IsText: false}
}

// textCell builds a non-NULL text cell.
//
// Takes value (string) which is the text.
//
// Returns Cell which holds it.
func textCell(value string) Cell {
	return Cell{Text: value, Integer: 0, Null: false, IsText: true}
}

// nullCell builds a NULL cell.
//
// Returns Cell which is NULL.
func nullCell() Cell {
	return Cell{Text: "", Integer: 0, Null: true, IsText: false}
}

// nullableCell builds an integer cell that may be NULL.
//
// Takes value (*int64) which is nil for NULL.
//
// Returns Cell which holds the value or NULL.
func nullableCell(value *int64) Cell {
	if value == nil {
		return nullCell()
	}
	return integerCell(*value)
}
