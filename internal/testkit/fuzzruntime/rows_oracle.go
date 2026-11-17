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

package fuzzruntime

import (
	"fmt"
	"slices"
	"strings"
)

// rowModeEmpty returns no rows, or a zero count.
const rowModeEmpty = 0

// rowModeNullScore returns a NULL score in the populated row.
const rowModeNullScore = 2

// rowModeStride separates the runtime input's row behaviours.
const rowModeStride = 48

// rowModeCount bounds the selected row behaviour.
const rowModeCount = 5

// scanFailureMode injects an invalid scanned value.
const scanFailureMode = 3

// readFailureMode injects a result iteration failure.
const readFailureMode = 4

// RowProblems checks scanned values, propagated failures and result cleanup.
//
// Takes data ([]byte) which selects the independent row model.
// Takes expected (string) which is the expected query SQL.
// Takes call (Call) which holds the generated method's observations.
//
// Returns []string which describes wrong values, errors or leaked results.
func RowProblems(data []byte, expected string, call Call) []string {
	var problems []string
	if call.OpenRows != 0 {
		problems = append(problems, fmt.Sprintf("generated method leaked %d result sets", call.OpenRows))
	}
	mode := 0
	if len(data) > 0 {
		mode = int(data[0]/rowModeStride) % rowModeCount
	}
	if mode == scanFailureMode || mode == readFailureMode {
		if call.Error == "" {
			problems = append(problems, "generated method discarded an injected row failure")
		}
		return problems
	}
	if call.Error != "" {
		problems = append(problems, "generated method failed to scan rows: "+call.Error)
	}
	want := expectedRowValues(mode, expected)
	if !slices.Equal(want, call.Values) {
		problems = append(problems, fmt.Sprintf("wrong scanned values: want %v, got %v", want, call.Values))
	}
	return problems
}

// expectedRowValues models scalar results without consulting a recording driver.
//
// Takes mode (int) which selects empty, populated or nullable results.
// Takes query (string) which describes the expected projection.
//
// Returns []string which holds scalar values in column order.
func expectedRowValues(mode int, query string) []string {
	if strings.HasPrefix(query, "SELECT COUNT(*)") {
		if mode == rowModeEmpty {
			return []string{"0"}
		}
		return []string{"7"}
	}
	if mode == rowModeEmpty {
		return nil
	}
	values := []string{"6101"}
	if strings.HasPrefix(query, "SELECT id, score") {
		score := "6202"
		if mode == rowModeNullScore {
			score = "NULL"
		}
		values = append(values, score)
	}
	return values
}
