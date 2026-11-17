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

import "strings"

const (
	// reasonNoFullJoin explains why the MySQL family has no FULL joins.
	reasonNoFullJoin = "The engine has no FULL JOIN."

	// reasonNoGroupingFunctions explains why the MySQL family has no CUBE or GROUPING SETS.
	reasonNoGroupingFunctions = "The engine has no CUBE or GROUPING SETS (MariaDB has only WITH ROLLUP)."

	// reasonNoArray explains why an engine has no wrapped array column.
	reasonNoArray = "The engine has no array type."

	// reasonNoInterval explains why an engine has no wrapped interval column.
	reasonNoInterval = "The engine has no interval result type."

	// reasonNoUUID explains why an engine has no wrapped UUID column.
	reasonNoUUID = "The engine has no UUID type."

	// reasonNoFetch explains why an engine has no FETCH FIRST clause.
	reasonNoFetch = "The engine has no FETCH FIRST clause."

	// reasonWithRollup explains why an engine has no WITH ROLLUP modifier.
	reasonWithRollup = "WITH ROLLUP is MySQL, MariaDB and ClickHouse syntax."

	// reasonNoGroupedJoin explains why ClickHouse has no join groups.
	reasonNoGroupedJoin = "ClickHouse has no parenthesised join syntax."
)

// queryGrammarRule records one version 14 construct and the engines that cannot express
// it.
type queryGrammarRule struct {
	// excluded maps an engine to the reason it cannot run the construct.
	excluded map[string]string

	// feature names the construct.
	feature string

	// family is the family that uses the construct.
	family int
}

// queryGrammarFamilyExclusions records whole families an engine cannot express.
var queryGrammarFamilyExclusions = map[int]map[string]string{
	queryGrammarJoinGroup: {engineClickHouse: reasonNoGroupedJoin},
	queryGrammarLateral: {engineClickHouse: "ClickHouse has no LATERAL joins.", engineMariaDB: "MariaDB has no LATERAL derived tables.",
		engineSQLite: "SQLite has no LATERAL joins."},
	queryGrammarNatural: {engineClickHouse: "ClickHouse has no NATURAL joins."},
	queryGrammarGrouping: {engineSQLite: "SQLite has no ROLLUP, CUBE or GROUPING SETS.",
		engineCockroach: "CockroachDB refuses ROLLUP, CUBE and GROUPING SETS."},
}

// queryGrammarVariantRules lists every construct within the families, in stable order.
var queryGrammarVariantRules = []queryGrammarRule{
	{feature: "join/inner", family: queryGrammarOuterJoin, excluded: nil},
	{feature: "join/left", family: queryGrammarOuterJoin, excluded: nil},
	{feature: "join/right", family: queryGrammarOuterJoin, excluded: nil},
	{feature: "join/full", family: queryGrammarOuterJoin, excluded: map[string]string{engineMySQL: reasonNoFullJoin, engineMariaDB: reasonNoFullJoin}},
	{feature: "group/left-nested", family: queryGrammarJoinGroup, excluded: nil},
	{feature: "group/right-nested", family: queryGrammarJoinGroup, excluded: nil},
	{feature: "cross/chained", family: queryGrammarCrossJoin, excluded: nil},
	{feature: "cross/grouped", family: queryGrammarCrossJoin, excluded: map[string]string{engineClickHouse: reasonNoGroupedJoin}},
	{feature: "lateral/aggregate", family: queryGrammarLateral, excluded: nil},
	{feature: "lateral/limit", family: queryGrammarLateral, excluded: nil},
	{feature: "lateral/left", family: queryGrammarLateral, excluded: nil},
	{feature: "lateral/comma", family: queryGrammarLateral, excluded: nil},
	{feature: "merge/derived", family: queryGrammarUsing, excluded: nil},
	{feature: "recursive/shift", family: queryGrammarRecursive, excluded: nil},
	{feature: "recursive/identity", family: queryGrammarRecursive, excluded: nil},
	{feature: "recursive/rotate", family: queryGrammarRecursive, excluded: nil},
	{feature: "recursive/reset", family: queryGrammarRecursive, excluded: nil},
	{feature: "window/lag", family: queryGrammarWindow, excluded: nil},
	{feature: "window/lead", family: queryGrammarWindow, excluded: nil},
	{feature: "window/lag-default", family: queryGrammarWindow, excluded: map[string]string{engineMariaDB: "MariaDB's LAG and LEAD take no default argument."}},
	{feature: "window/first-value", family: queryGrammarWindow, excluded: nil},
	{feature: "window/last-value", family: queryGrammarWindow, excluded: nil},
	{feature: "window/nth-value", family: queryGrammarWindow, excluded: nil},
	{feature: "window/sum", family: queryGrammarWindow, excluded: nil},
	{feature: "window/min", family: queryGrammarWindow, excluded: nil},
	{feature: "window/max", family: queryGrammarWindow, excluded: nil},
	{feature: "window/avg", family: queryGrammarWindow, excluded: nil},
	{feature: "window/count", family: queryGrammarWindow, excluded: nil},
	{feature: "frame/rows-to-current", family: queryGrammarWindow, excluded: nil},
	{feature: "frame/rows-preceding", family: queryGrammarWindow, excluded: nil},
	{feature: "frame/rows-following", family: queryGrammarWindow, excluded: nil},
	{feature: "frame/range-preceding", family: queryGrammarWindow, excluded: nil},
	{feature: "frame/current-row", family: queryGrammarWindow, excluded: nil},
	{feature: "frame/range-all", family: queryGrammarWindow, excluded: nil},
	{feature: "scalar/count", family: queryGrammarScalarLimit, excluded: nil},
	{feature: "scalar/limit-one", family: queryGrammarScalarLimit, excluded: nil},
	{feature: "scalar/limit-zero", family: queryGrammarScalarLimit, excluded: nil},
	{feature: "scalar/offset-one", family: queryGrammarScalarLimit, excluded: nil},
	{feature: "scalar/offset-zero", family: queryGrammarScalarLimit, excluded: nil},
	{feature: "scalar/max", family: queryGrammarScalarLimit, excluded: nil},
	{feature: "scalar/first-row", family: queryGrammarScalarLimit, excluded: nil},
	{feature: "scalar/fetch-zero", family: queryGrammarScalarLimit, excluded: map[string]string{engineMySQL: reasonNoFetch, engineMariaDB: reasonNoFetch,
		engineSQLite: reasonNoFetch, engineClickHouse: "ClickHouse accepts FETCH only after ORDER BY with OFFSET."}},
	{feature: "scalar/coalesced", family: queryGrammarScalarLimit, excluded: nil},
	{feature: "grouping/rollup", family: queryGrammarGrouping, excluded: map[string]string{engineMariaDB: reasonNoGroupingFunctions}},
	{feature: "grouping/rollup-pair", family: queryGrammarGrouping, excluded: map[string]string{engineMariaDB: reasonNoGroupingFunctions}},
	{feature: "grouping/cube", family: queryGrammarGrouping, excluded: map[string]string{engineMySQL: reasonNoGroupingFunctions, engineMariaDB: reasonNoGroupingFunctions}},
	{feature: "grouping/sets", family: queryGrammarGrouping, excluded: map[string]string{engineMySQL: reasonNoGroupingFunctions, engineMariaDB: reasonNoGroupingFunctions,
		engineClickHouse: "ClickHouse 25.4, 26.8 and 26.10 count one row too few for a GROUPING SETS subquery with an empty set, so Count cannot be checked."}},
	{feature: "grouping/sets-without-total", family: queryGrammarGrouping, excluded: map[string]string{engineMySQL: reasonNoGroupingFunctions, engineMariaDB: reasonNoGroupingFunctions}},
	{feature: "grouping/with-rollup", family: queryGrammarGrouping, excluded: map[string]string{enginePostgres: reasonWithRollup, engineTimescale: reasonWithRollup,
		engineDuckDB: reasonWithRollup}},
	{feature: "wrapped/decimal", family: queryGrammarWrapped, excluded: nil},
	{feature: "wrapped/array", family: queryGrammarWrapped, excluded: map[string]string{engineMySQL: reasonNoArray, engineMariaDB: reasonNoArray, engineSQLite: reasonNoArray}},
	{feature: "wrapped/json", family: queryGrammarWrapped, excluded: map[string]string{engineClickHouse: "The fixture has no ClickHouse JSON value contract."}},
	{feature: "wrapped/interval", family: queryGrammarWrapped, excluded: map[string]string{engineMySQL: reasonNoInterval, engineMariaDB: reasonNoInterval,
		engineSQLite: reasonNoInterval, engineClickHouse: "The fixture has no ClickHouse interval value contract."}},
	{feature: "wrapped/uuid", family: queryGrammarWrapped, excluded: map[string]string{engineMySQL: reasonNoUUID, engineMariaDB: reasonNoUUID,
		engineSQLite: reasonNoUUID, engineClickHouse: "The model has no deterministic ClickHouse UUID derivation for nullable input."}},
	{feature: "wrapped/distinct-alias", family: queryGrammarWrapped, excluded: nil},
	{feature: "wrapped/distinct-ordinal", family: queryGrammarWrapped, excluded: nil},
	{feature: "wrapped/union", family: queryGrammarWrapped, excluded: nil},
	{feature: "wrapped/group-alias", family: queryGrammarWrapped, excluded: nil},
	{feature: "wrapped/group-ordinal", family: queryGrammarWrapped, excluded: nil},
	{feature: "wrapped/distinct-derived", family: queryGrammarWrapped, excluded: nil},
	{feature: "identifier/mixed-case", family: queryGrammarIdentifiers, excluded: nil},
	{feature: "identifier/reserved", family: queryGrammarIdentifiers, excluded: nil},
	{feature: "identifier/dotted", family: queryGrammarIdentifiers, excluded: nil},
	{feature: "identifier/spaced", family: queryGrammarIdentifiers, excluded: nil},
}

// queryGrammarCapability classifies one family or construct for an engine.
//
// Takes engine (string) which selects the native grammar.
// Takes feature (string) which names a family or construct.
//
// Returns Capability which carries the exclusion reason when there is one.
func queryGrammarCapability(engine, feature string) Capability {
	capability := Capability{Feature: feature, NativeType: "", CastType: "", Status: capabilitySupported, Reason: ""}
	if reason := queryGrammarExclusion(engine, feature); reason != "" {
		capability.Status, capability.Reason = capabilityUnsupportedNative, reason
	}
	return capability
}

// queryGrammarExclusion finds the reason an engine cannot run a family or construct.
//
// Takes engine (string) which selects the native grammar.
// Takes feature (string) which names a family or construct.
//
// Returns string which is empty when the engine supports the feature.
func queryGrammarExclusion(engine, feature string) string {
	for family, name := range queryGrammarFamilies {
		if queryGrammarFamilyPrefix+name == feature {
			return queryGrammarFamilyExclusions[family][engine]
		}
	}
	for _, rule := range queryGrammarVariantRules {
		if rule.feature != feature {
			continue
		}
		if reason := queryGrammarFamilyExclusions[rule.family][engine]; reason != "" {
			return reason
		}
		return rule.excluded[engine]
	}
	return "The feature is not part of version 14."
}

// queryGrammarSupports reports whether an engine runs a family or construct.
//
// Takes engine (string) which selects the native grammar.
// Takes feature (string) which names a family or construct.
//
// Returns bool which is true when nothing excludes the feature.
func queryGrammarSupports(engine, feature string) bool {
	return queryGrammarExclusion(engine, feature) == ""
}

// queryGrammarSupportedOf keeps the admitted constructs of a list in order.
//
// Takes engine (string) which selects the native grammar.
// Takes prefix (string) which starts every construct name.
// Takes names ([]string) which lists the candidate constructs.
//
// Returns []string which holds the admitted names without the prefix.
func queryGrammarSupportedOf(engine, prefix string, names []string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		if queryGrammarSupports(engine, prefix+name) {
			result = append(result, name)
		}
	}
	return result
}

// queryGrammarQuote quotes an identifier in the engine's own spelling.
//
// Takes engine (string) which selects the quote character.
// Takes name (string) which holds no quote character.
//
// Returns string which is the quoted identifier.
func queryGrammarQuote(engine, name string) string {
	if mysqlFamily(engine) {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// queryGrammarFillsDefaults reports an engine that writes type defaults where others
// write NULL.
//
// ClickHouse runs with join_use_nulls and group_by_use_nulls off, so the columns an outer
// join, an empty window frame, a grouping set or an aggregate over no rows leaves without
// a value hold the type's default unless the column is already Nullable.
//
// Takes engine (string) which names the dialect.
//
// Returns bool which is true for ClickHouse.
func queryGrammarFillsDefaults(engine string) bool {
	return engine == engineClickHouse
}

// queryGrammarTotalOverEmptyInput reports an engine that may return a grand total row for
// no input rows. MySQL 9 does and MySQL 8.4 does not, so MySQL counts as one that may.
//
// Takes engine (string) which names the dialect.
//
// Returns bool which is true when the empty grouping set may yield a row over an empty
// table.
func queryGrammarTotalOverEmptyInput(engine string) bool {
	switch engine {
	case enginePostgres, engineTimescale, engineDuckDB, engineMySQL:
		return true
	default:
		return false
	}
}
