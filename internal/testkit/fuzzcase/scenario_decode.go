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

// DecodeScenario decodes an independent typed scenario.
//
// Takes engine (string) which selects admitted native features.
// Takes data ([]byte) which contains bounded versioned choices.
//
// Returns Scenario which includes SQL and required comparisons.
// Returns bool which rejects unsupported versions and envelopes.
func DecodeScenario(engine string, data []byte) (Scenario, bool) {
	if len(data) == 0 || len(data) > ScenarioMaxBytes || !supportsShape(engine, shapeSelect) {
		return Scenario{}, false
	}
	switch data[0] {
	case TypedVersion:
		scenario, err := typedScenario(engine, data)
		return scenario, err == nil
	case relationVersion:
		return relationScenario(engine, data)
	case QueryGrammarVersion:
		return queryGrammarScenario(engine, data)
	default:
		return Scenario{}, false
	}
}

// ScenarioSeeds covers admitted types and each expression position.
//
// Takes engine (string) which selects capabilities.
//
// Returns [][]byte which reaches typed and semantic checks.
func ScenarioSeeds(engine string) [][]byte {
	types := admittedTypes(engine)
	seeds := make([][]byte, 0, len(types)*scenarioPositionCount+scenarioRelationCount)
	for index, capability := range types {
		seeds = append(seeds, typedScenarioSeeds(engine, byte(index), capability)...)
	}
	for relation := range scenarioRelationCount {
		seeds = append(seeds, []byte{relationVersion, byte(relation), 0, scenarioReservedSeed, scenarioRelationRowsSeed})
	}
	return append(seeds, QueryGrammarSeeds(engine)...)
}

// typedScenarioSeeds covers each position, boundary and wrapper for one family.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes index (byte) which identifies the family in the admitted type order.
// Takes capability (Capability) which supplies its native representation.
//
// Returns [][]byte which covers its positions and value boundaries.
func typedScenarioSeeds(engine string, index byte, capability Capability) [][]byte {
	var seeds [][]byte
	for position := range scenarioPositionCount {
		seeds = append(seeds, []byte{TypedVersion, index, byte(position), 0, scenarioReservedSeed})
	}
	kind := strings.TrimPrefix(capability.Feature, typeFeaturePrefix)
	for boundary := range scenarioValueCount(kind) {
		for _, position := range []byte{positionLiteral, positionParameter} {
			seeds = append(seeds, []byte{TypedVersion, index, position, byte(boundary), scenarioReservedSeed})
		}
	}
	if engine != engineClickHouse || kind != typedArray {
		for position := range scenarioPositionCount {
			seeds = append(seeds, []byte{TypedVersion, index, byte(position), scenarioNullFlag, scenarioReservedSeed})
		}
	}
	for wrapper := range scenarioWrapperCount {
		seeds = append(seeds, []byte{TypedVersion, index, 2, 0, scenarioReservedSeed, byte(wrapper), byte((wrapper + 1) % scenarioWrapperCount)})
	}
	return seeds
}

// admittedTypes retains stable type order while excluding explicit limitations.
//
// Takes engine (string) which selects native syntax and binding rules.
//
// Returns []Capability which retains the stable order of admitted families.
func admittedTypes(engine string) []Capability {
	var result []Capability
	for _, capability := range typeCapabilities(engine) {
		if capability.Status == capabilitySupported {
			result = append(result, capability)
		}
	}
	return result
}

// typedScenario constructs scalar positions and type-preserving expressions.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes data ([]byte) which supplies bounded versioned choices.
//
// Returns Scenario which pairs a typed expression with native observations.
// Returns error when an internally constructed value cannot render as SQL.
func typedScenario(engine string, data []byte) (Scenario, error) {
	types := admittedTypes(engine)
	capability := types[int(scenarioByte(data, 1))%len(types)]
	kind := strings.TrimPrefix(capability.Feature, typeFeaturePrefix)
	value := scenarioValue(kind, scenarioByte(data, scenarioValueByte))
	if engine == engineClickHouse && kind == typedArray {
		value.Null = false
	}
	literal, err := value.Literal(engine)
	if err != nil {
		return Scenario{}, fmt.Errorf("rendering typed scenario value: %w", err)
	}
	if value.Null && engine == engineClickHouse {
		capability.NativeType = clickHouseNullable(capability.NativeType)
		capability.CastType = clickHouseNullable(capability.CastType)
	}
	mode := scenarioByte(data, 2) % scenarioPositionCount
	cast := "CAST(" + literal + " AS " + capability.CastType + scenarioClose
	expression, err := scenarioExpression(engine, data, capability, value)
	if err != nil {
		return Scenario{}, err
	}
	var values []TypedValue
	var bindings []int
	if mode == positionParameter {
		values, bindings = []TypedValue{value}, []int{0}
	}

	sqlText := "SELECT v.id, " + expression + " AS result FROM fuzz_values v ORDER BY v.id"
	schemaType := capability.NativeType
	suffix := ""
	if engine == engineClickHouse {
		suffix = " ENGINE = Memory"
	}
	schema := "CREATE TABLE fuzz_values (id BIGINT, value " + schemaType + scenarioClose + suffix
	inserts := []string{"INSERT INTO fuzz_values (id, value) VALUES (1, " + cast + "), (2, " + cast + scenarioClose}
	return Scenario{Engine: engine, Data: append([]byte(nil), data...), Schema: []string{schema}, Inserts: inserts,
		Queries: []ScenarioQuery{scenarioQuery("Typed", sqlText, values, bindings, 2, true)}, Relations: nil,
		Features: []string{capability.Feature, "position/" + strconv.Itoa(int(mode))}}, nil
}

// scenarioQuery attaches a directive to independent executable SQL.
//
// Takes name (string) which identifies the generated method.
// Takes sqlText (string) which contains independently constructed SQL.
// Takes values ([]TypedValue) which preserves bound values.
// Takes bindings ([]int) which maps occurrences to value identities.
// Takes columns (int) which records the expected output width.
// Takes ordered (bool) which selects sequence comparison.
//
// Returns ScenarioQuery which pairs executable SQL with its directive.
func scenarioQuery(name, sqlText string, values []TypedValue, bindings []int, columns int, ordered bool) ScenarioQuery {
	return ScenarioQuery{Name: name, SQL: "-- sgl.query(" + name + ", many)\n" + sqlText + ";", Reference: sqlText,
		Values: values, Bindings: bindings, MayBeNull: nil, Columns: columns, Ordered: ordered, Writes: false}
}

// scenarioPlaceholder preserves native binding identities and type annotations.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes number (int) which identifies the parameter.
// Takes kind (string) which selects the parameter representation.
// Takes nullable (bool) which admits SQL NULL.
//
// Returns string which spells the native placeholder.
func scenarioPlaceholder(engine string, number int, kind string, nullable bool) string {
	switch engine {
	case engineMySQL, engineMariaDB:
		return "?"
	case engineSQLite:
		return "?" + strconv.Itoa(number)
	case engineClickHouse:
		native := clickHouseParameterType(kind)
		if nullable {
			native = clickHouseNullable(native)
		}
		return fmt.Sprintf("{p%d:%s}", number, native)
	default:
		return "$" + strconv.Itoa(number)
	}
}

// clickHouseParameterType names the ClickHouse type of a named parameter for a family.
//
// Takes kind (string) which selects the independent value family.
//
// Returns string which sends decimals, temporal values and UUIDs as text.
func clickHouseParameterType(kind string) string {
	switch kind {
	case typedDecimal, typedDate, typedTimestamp, typedTimestamptz, typedUUID:
		return "String"
	default:
		return clickHouseNativeTypes[kind]
	}
}
