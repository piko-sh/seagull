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
	"fmt"
	"slices"
	"strings"
)

// Problems compares every observed call with the model.
//
// Takes observations ([]Observation) which holds one observation per step, in order.
//
// Returns []string which describes each disagreement, empty when none.
func (c Case) Problems(observations []Observation) []string {
	expected := c.outcomes()
	if len(observations) != len(expected) {
		return []string{fmt.Sprintf("observed %d calls, expected %d", len(observations), len(expected))}
	}
	var problems []string
	for index, want := range expected {
		if problem := compareOutcome(c.Steps[index], want, &observations[index]); problem != "" {
			problems = append(problems, fmt.Sprintf("step %d %s: %s", index, c.Steps[index], problem))
		}
	}
	return problems
}

// compareOutcome compares one call.
//
// Takes step (Step) which is the call.
// Takes want (outcome) which is the model's expectation.
// Takes got (*Observation) which is what the generated code returned.
//
// Returns string which describes the disagreement, empty when none.
func compareOutcome(step Step, want outcome, got *Observation) string {
	if want.class != got.Class {
		return fmt.Sprintf("expected outcome %q, observed %q (%s)", describeClass(want.class), describeClass(got.Class), got.Error)
	}
	if want.class != "" {
		return ""
	}
	switch step.Op {
	case OperationAll, OperationOne:
		return comparePage(want, got.Rows)
	case OperationCount:
		if want.count != got.Count {
			return fmt.Sprintf("Count returned %d, the model counts %d", got.Count, want.count)
		}
	default:
	}
	return ""
}

// comparePage checks the rows, their order under the keys and their kinds.
//
// A page under an ordering with ties may hold any of the tied rows, so each observed row
// must have the keys of the expected row at its position and be a distinct admitted row.
//
// Takes want (outcome) which holds the expected page, admitted rows and keys.
// Takes rows ([][]Cell) which holds the observed rows.
//
// Returns string which describes the disagreement, empty when none.
func comparePage(want outcome, rows [][]Cell) string {
	if len(rows) != len(want.page) {
		return fmt.Sprintf("returned %d rows, expected %d: observed %s, expected %s", len(rows), len(want.page), describeRows(rows), describeRows(want.page))
	}
	used := make([]bool, len(want.filtered))
	for index, row := range rows {
		if len(row) != len(want.page[index]) {
			return fmt.Sprintf("row %d has %d columns, expected %d", index, len(row), len(want.page[index]))
		}
		if compareRows(want.keys, row, want.page[index]) != 0 {
			return fmt.Sprintf("row %d is out of order: observed %s, expected %s", index, describeRows(rows), describeRows(want.page))
		}
		if !claim(want.filtered, used, row) {
			return fmt.Sprintf("row %d %s is not an admitted row: observed %s, admitted %s", index, describeRows([][]Cell{row}), describeRows(rows),
				describeRows(want.filtered))
		}
	}
	return ""
}

// claim marks the first unused admitted row equal to row.
//
// Takes filtered ([][]Cell) which holds the admitted rows.
// Takes used ([]bool) which marks rows already matched.
// Takes row ([]Cell) which is the observed row.
//
// Returns bool which is false when no unused admitted row equals it.
func claim(filtered [][]Cell, used []bool, row []Cell) bool {
	for index, candidate := range filtered {
		if !used[index] && slices.Equal(candidate, row) {
			used[index] = true
			return true
		}
	}
	return false
}

// describeRows writes rows for failure messages.
//
// Takes rows ([][]Cell) which holds the rows.
//
// Returns string which lists each row's values.
func describeRows(rows [][]Cell) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, 0, len(row))
		for _, cell := range row {
			cells = append(cells, describeCell(cell))
		}
		parts = append(parts, "("+strings.Join(cells, columnSeparator)+")")
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// describeClass names success for failure messages.
//
// Takes class (string) which is an outcome class.
//
// Returns string which is the class or "success".
func describeClass(class string) string {
	if class == "" {
		return "success"
	}
	return class
}
