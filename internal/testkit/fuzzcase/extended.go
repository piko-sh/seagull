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

// extendedVersion adds independently numbered row-shaping and write families.
const extendedVersion = 3

// extendedShapeStart separates the original and extended operation identifiers.
const extendedShapeStart = 24

// extendedShapeCount fixes the number of version three operations.
const extendedShapeCount = 4

// extendedWidthByte selects expansion size, bulk width and branch pagination.
const extendedWidthByte = 8

// extendedMaximumWidth bounds expansion and bulk insertion.
const extendedMaximumWidth = 4

const (
	// shapeVariableExpansion varies zero, one and several set-returning rows.
	shapeVariableExpansion = extendedShapeStart + iota

	// shapeBranchClauses keeps branch pagination separate from outer pagination.
	shapeBranchClauses

	// shapeBulkInsert inserts several bounded rows with repeated parameters.
	shapeBulkInsert

	// shapeLocking applies a row lock after outer pagination.
	shapeLocking
)

// extendedInsertID keeps inserted keys outside the fixture row range.
const extendedInsertID = 99

// extendedFeatures names permanently assigned version three operations.
var extendedFeatures = [...]string{"variable-set-returning", "compound-branch-clauses", "bulk-insert", "locking"}

// extendedBody renders one extended family with independent parameter expressions.
//
// Takes parameter (func(int) string) which writes logical placeholders or literal values.
//
// Returns string which holds SQL before removable outer clauses.
// Returns int which counts projected columns.
// Returns bool which reports a write.
func (m Model) extendedBody(parameter func(int) string) (string, int, bool) {
	switch m.Shape {
	case shapeVariableExpansion:
		return "SELECT i.id, " + m.expansionExpression() + " AS expanded FROM fuzz_items i", 2, false
	case shapeBranchClauses:
		width := strconv.Itoa(1 + int(m.at(extendedWidthByte)%extendedMaximumWidth))
		left := "SELECT id FROM fuzz_items WHERE id >= " + parameter(0) + " ORDER BY id LIMIT " + width
		right := "SELECT id FROM fuzz_peers WHERE id < " + parameter(1) + " ORDER BY id DESC LIMIT " + width + " OFFSET 1"
		return "SELECT chosen.id FROM (" + left + ") chosen UNION ALL SELECT peers.id FROM (" + right + ") peers", 1, false
	case shapeBulkInsert:
		rows := 1 + int(m.at(extendedWidthByte)%extendedMaximumWidth)
		values := make([]string, 0, rows)
		for row := range rows {
			values = append(values, fmt.Sprintf("(%d, %s, %s)", extendedInsertID+row, parameter(0), parameter(1)))
		}
		return "INSERT INTO fuzz_items (id, score, category) VALUES " + strings.Join(values, columnSeparator), 0, true
	default:
		return "SELECT i.id, i.score FROM fuzz_items i WHERE i.id >= " + parameter(0), 2, false
	}
}

// expansionExpression writes bounded zero, one or multiple result elements.
//
// Returns string which is a supported set-returning projection expression.
func (m Model) expansionExpression() string {
	width := int(m.at(extendedWidthByte) % extendedMaximumWidth)
	if m.Engine != engineClickHouse && m.Engine != engineDuckDB {
		return fmt.Sprintf("generate_series(0, %d)", width-1)
	}
	values := make([]string, 0, width)
	for value := range width {
		values = append(values, strconv.Itoa(value))
	}
	array := "[" + strings.Join(values, columnSeparator) + "]"
	if m.Engine == engineClickHouse {
		return "arrayJoin(CAST(" + array + " AS Array(Int64)))"
	}
	return "unnest(CAST(" + array + " AS BIGINT[]))"
}

// extendedSeeds exercises each supported family and every bounded expansion size.
//
// Takes engine (string) which selects admitted syntax.
//
// Returns [][]byte which includes empty and populated tables.
func extendedSeeds(engine string) [][]byte {
	var seeds [][]byte
	for shape := range extendedShapeCount {
		if !supportsShape(engine, extendedShapeStart+shape) {
			continue
		}
		for _, rows := range []byte{0, maxRows} {
			for width := range extendedMaximumWidth {
				seeds = append(seeds, []byte{extendedVersion, byte(shape), 1, rows, seedLower, seedUpper, 1, 1, byte(width)})
			}
		}
	}
	return seeds
}
