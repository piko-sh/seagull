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
	"strconv"
	"strings"
)

// rejectionVersion selects the stable negative structured decoder.
const rejectionVersion = 128

// rejectionShapeCount fixes the number of assigned rejection operations.
const rejectionShapeCount = 6

const (
	// rejectionCompound requires matching branch widths.
	rejectionCompound = iota

	// rejectionDuplicate requires unique query names.
	rejectionDuplicate

	// rejectionStatements requires one statement per query block.
	rejectionStatements

	// rejectionDirective requires a declared directive option value.
	rejectionDirective

	// rejectionRuntimeList forbids runtime list expansion.
	rejectionRuntimeList
)

// Rejection holds a deliberately invalid query and its required diagnostic.
type Rejection struct {
	// SQL is valid source text with one deliberately rejected condition.
	SQL string

	// Code names the required diagnostic independently of the implementation.
	Code string
}

// CountWrapped reports whether row shaping requires a count subquery. A query with its
// own LIMIT is counted as the rows that LIMIT returns.
//
// Returns bool which is derived from model operations independently of SQL spans.
func (m Model) CountWrapped() bool {
	if m.ownsLimit() {
		return true
	}
	if m.at(0) == grammarVersion {
		return m.Flags&(grammarDistinct|grammarGrouped|grammarWindow) != 0
	}
	switch m.Shape {
	case shapeDistinct, shapeGrouped, shapeAggregate, shapeWindow, shapeUnion, shapeHaving, shapeSetReturning, shapeQualify, shapeVariableExpansion, shapeBranchClauses:
		return true
	default:
		return false
	}
}

// ownsLimit reports whether the model's query ends in a LIMIT of its own: every read
// paginates when the first flag is set, except a write and the SETTINGS family.
//
// Returns bool which is true for a query with its own LIMIT.
func (m Model) ownsLimit() bool {
	if m.Flags&1 == 0 {
		return false
	}
	if m.at(0) == grammarVersion || m.at(0) == valueVersion {
		return true
	}
	_, _, writes := m.body(func(int) string { return "0" })
	return !writes && m.Shape != shapeSettings
}

// RejectionSeeds covers every assigned rejection operation.
//
// Returns [][]byte which retains stable operation numbers across campaigns.
func RejectionSeeds() [][]byte {
	seeds := make([][]byte, 0, rejectionShapeCount)
	for shape := range rejectionShapeCount {
		seeds = append(seeds, []byte{rejectionVersion, byte(shape)})
	}
	return seeds
}

// DecodeRejection constructs bounded failures without relying on the parser.
//
// Takes engine (string) which selects the input placeholder spelling.
// Takes data ([]byte) which holds a version, operation and bounded value.
//
// Returns Rejection which specifies the SQL and expected error category.
// Returns bool which is false for unsupported versions, engines or sizes.
func DecodeRejection(engine string, data []byte) (Rejection, bool) {
	if len(data) == 0 || len(data) > MaxBytes || data[0] != rejectionVersion || !supportsShape(engine, shapeSelect) {
		return Rejection{}, false
	}
	choice, value := byte(0), byte(0)
	if len(data) > 1 {
		choice = data[1]
	}
	if len(data) > 2 {
		value = data[2] % parameterRange
	}
	query := "SELECT id FROM fuzz_items WHERE id > " + strconv.Itoa(int(value)) + ";"
	header := "-- sgl.query(FuzzRejected, many)\n"
	var rejected Rejection
	switch choice % rejectionShapeCount {
	case rejectionCompound:
		rejected = Rejection{SQL: header + "SELECT id FROM fuzz_items UNION ALL SELECT id, category FROM fuzz_items;", Code: "Q020"}
	case rejectionDuplicate:
		rejected = Rejection{SQL: header + query + "\n-- sgl.query(fuzz_rejected, many)\n" + query, Code: "Q006"}
	case rejectionStatements:
		rejected = Rejection{SQL: header + query + "\n" + query, Code: "Q012"}
	case rejectionDirective:
		rejected = Rejection{SQL: strings.Replace(header, "many)", "many, dynamic: impossible)", 1) + query, Code: "Q028"}
	case rejectionRuntimeList:
		rejected = rejectionSlice(engine, false)
	default:
		rejected = rejectionSlice(engine, true)
	}
	return rejected, true
}

// rejectionSlice constructs a forbidden list parameter combination.
//
// Takes engine (string) which supplies placeholder spelling.
// Takes batch (bool) which selects insertion instead of a runtime builder.
//
// Returns Rejection which requires a precise list-configuration diagnostic.
func rejectionSlice(engine string, batch bool) Rejection {
	placeholder, anchor := "$1", "$1"
	switch engine {
	case engineSQLite:
		placeholder, anchor = "?1", "?1"
	case engineMySQL, engineMariaDB:
		placeholder, anchor = "?", "?1"
	case engineClickHouse:
		placeholder, anchor = "{ids:Int64}", "{ids:Int64}"
	}
	header := "-- sgl.query(FuzzRejected, many, dynamic: runtime)\n"
	sql, code := "SELECT id FROM fuzz_items WHERE id IN ("+placeholder+");", "Q018"
	if batch {
		header, sql, code = "-- sgl.query(FuzzRejected, batch)\n", "INSERT INTO fuzz_items (id, category) VALUES ("+placeholder+", 1);", "Q017"
	}
	return Rejection{SQL: header + "-- " + anchor + " as sgl.param(ids, kind: slice)\n" + sql, Code: code}
}
