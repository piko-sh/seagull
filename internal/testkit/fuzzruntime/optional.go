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

// optionalOperationCount preserves the runtime sequence's operation identifiers.
const optionalOperationCount = 12

// optionalSelectionCount includes all combinations of two optional predicates.
const optionalSelectionCount = 4

// OptionalSQL supplies the independent optional-filter execution expectation.
//
// Takes operation (byte) which selects absent or present filter values.
// Takes count (bool) which removes caller pagination while retaining filters.
//
// Returns string which contains literal values and no generated placeholders.
func OptionalSQL(operation byte, count bool) string {
	projection := "id, score"
	if count {
		projection = "COUNT(*)"
	}
	minimum, category := "NULL", "NULL"
	selection := int(operation/optionalOperationCount) % optionalSelectionCount
	if selection&1 != 0 {
		minimum = "6000"
	}
	if selection&2 != 0 {
		category = "6001"
	}
	statement := "SELECT " + projection + " FROM fuzz_items WHERE id > 0 AND (id >= " + minimum + " OR " + minimum + " IS NULL) AND (category = " +
		category + " OR " + category + " IS NULL) AND id < 7000"
	if !count {
		statement += " LIMIT 2"
	}
	return statement
}
