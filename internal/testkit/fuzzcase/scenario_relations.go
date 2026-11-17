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

import "strconv"

const (
	// relationPartition splits rows by a predicate, its negation and its unknown result.
	relationPartition = iota

	// relationCountTruth compares a filtered count with a sum of truth values.
	relationCountTruth

	// relationEqual compares a projection with an equivalent CASE expression.
	relationEqual

	// relationConstantSubstitution compares a repeated constant subquery.
	relationConstantSubstitution
)

// relationScenario constructs independent three-valued and expression checks.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes data ([]byte) which supplies bounded versioned choices.
//
// Returns Scenario which includes independently related queries.
// Returns bool which is false when the engine has no base fixture.
func relationScenario(engine string, data []byte) (Scenario, bool) {
	base, ok := Decode(engine, []byte{Version, 0, 0, scenarioByte(data, scenarioRelationRowsByte) % (maxRows + 1), seedLower, seedUpper})
	if !ok {
		return Scenario{}, false
	}
	predicate := "i.score > " + strconv.Itoa(int(scenarioByte(data, scenarioValueByte)%scoreRange)-scoreOffset)
	from := " FROM fuzz_items i"
	kind := scenarioByte(data, 1) % scenarioRelationCount
	var sqlTexts []string
	relation := "equal"
	switch kind {
	case relationPartition:
		relation = "partition"
		projection := relationProjection + from
		sqlTexts = []string{projection, projection + " WHERE " + predicate, projection + " WHERE NOT (" + predicate + ")", projection + " WHERE (" + predicate + ") IS NULL"}
	case relationCountTruth:
		relation = "count-truth"
		sqlTexts = []string{"SELECT COUNT(*) AS total" + from + " WHERE " + predicate, "SELECT CASE WHEN " + predicate + " THEN 1 ELSE 0 END AS truth" + from}
	case relationEqual:
		sqlTexts = []string{relationProjection + from, "SELECT i.id, CASE WHEN " + predicate + " THEN i.score ELSE i.score END AS score" + from}
	case relationConstantSubstitution:
		sqlTexts = []string{relationProjection + from + " WHERE i.category < (SELECT MAX(category) FROM fuzz_items)",
			relationProjection + from + " WHERE i.category < (SELECT MAX(category) FROM fuzz_items)"}
		relation = "constant-substitution"
	}
	queries := make([]ScenarioQuery, 0, len(sqlTexts))
	indices := make([]int, len(sqlTexts))
	for index, sqlText := range sqlTexts {
		width := 2
		if kind == relationCountTruth {
			width = 1
		}
		queries = append(queries, scenarioQuery("Relation"+strconv.Itoa(index), sqlText, nil, nil, width, false))
		indices[index] = index
	}
	return Scenario{Engine: engine, Data: append([]byte(nil), data...), Schema: base.Migrations(), Inserts: base.Inserts(), Queries: queries,
		Relations: []Relation{{Kind: relation, Queries: indices}}, Features: []string{"relation/" + relation}}, true
}
