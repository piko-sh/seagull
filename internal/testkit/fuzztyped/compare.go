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

package fuzztyped

import (
	"fmt"
	"slices"
	"strconv"
)

// Problems compares every observed call with the model.
//
// Takes observations ([]Observation) which holds one observation per call, in order.
//
// Returns []string which describes each disagreement, empty when none.
func (c Case) Problems(observations []Observation) []string {
	if len(observations) != len(c.Calls) {
		return []string{fmt.Sprintf("observed %d calls, expected %d", len(observations), len(c.Calls))}
	}
	var problems []string
	for index, got := range observations {
		call := c.Calls[index]
		if got.Error != "" {
			problems = append(problems, fmt.Sprintf("call %d %s%v failed: %s", index, call.Method, call.Arguments, got.Error))
			continue
		}
		if call.Exec {
			continue
		}
		if problem := c.compareRows(call.Want, got.Rows); problem != "" {
			problems = append(problems, fmt.Sprintf("call %d %s%v: %s", index, call.Method, call.Arguments, problem))
		}
	}
	return problems
}

// compareRows compares rows in order, value by value and kind by kind.
//
// Takes want ([][]Value) which holds the expected rows.
// Takes got ([][]Value) which holds the observed rows.
//
// Returns string which describes the first disagreement, empty when none.
func (c Case) compareRows(want, got [][]Value) string {
	if len(want) != len(got) {
		return fmt.Sprintf("returned %d rows, expected %d: observed %v, expected %v", len(got), len(want), got, want)
	}
	for index := range want {
		if len(want[index]) != len(got[index]) {
			return fmt.Sprintf("row %d has %d columns, expected %d", index, len(got[index]), len(want[index]))
		}
		for column := range want[index] {
			if !c.equal(want[index][column], got[index][column]) {
				return fmt.Sprintf("row %d column %d is %v, expected %v", index, column, got[index][column], want[index][column])
			}
		}
	}
	return ""
}

// equal compares one value, reading the integers zero and one as a truth value on engines
// that return comparisons as integers.
//
// Takes want (Value) which is the expected value.
// Takes got (Value) which is the observed value.
//
// Returns bool which reports a match.
func (c Case) equal(want, got Value) bool {
	if want.Kind == KindBoolean && got.Kind == KindInteger && slices.Contains(integerBooleanEngines, c.Engine) {
		return (want.Text == strconv.FormatBool(true) && got.Text == "1") || (want.Text == strconv.FormatBool(false) && got.Text == "0")
	}
	return want.Equal(got)
}

// integerOf reads an integer value.
//
// Takes value (Value) which is an integer.
//
// Returns int64 which is its value, zero when it is not an integer.
func integerOf(value Value) int64 {
	number, err := strconv.ParseInt(value.Text, decimalBase, floatBits)
	if err != nil {
		return 0
	}
	return number
}

// integerLiteral writes a nullable integer.
//
// Takes value (*int64) which is nil for NULL.
//
// Returns string which is the SQL literal.
func integerLiteral(value *int64) string {
	if value == nil {
		return "NULL"
	}
	return strconv.FormatInt(*value, decimalBase)
}

// textLiteral writes a nullable text without quotes or backslashes inside.
//
// Takes value (*string) which is nil for NULL.
//
// Returns string which is the SQL literal.
func textLiteral(value *string) string {
	if value == nil {
		return "NULL"
	}
	return "'" + *value + "'"
}

// nullableInteger builds an integer value that may be NULL.
//
// Takes value (*int64) which is nil for NULL.
//
// Returns Value which holds it.
func nullableInteger(value *int64) Value {
	if value == nil {
		return Null()
	}
	return Integer(*value)
}

// nullableText builds a text value that may be NULL.
//
// Takes value (*string) which is nil for NULL.
//
// Returns Value which holds it.
func nullableText(value *string) Value {
	if value == nil {
		return Null()
	}
	return Text(*value)
}
