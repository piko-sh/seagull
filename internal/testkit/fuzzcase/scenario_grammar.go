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
	"math/rand/v2"
	"slices"
	"strings"
)

// QueryGrammarVersion identifies join, recursive, window, grouping and identifier
// grammar.
const QueryGrammarVersion = 14

const (
	// queryGrammarFamilyByte locates the query family.
	queryGrammarFamilyByte = 1

	// queryGrammarRowsByte locates the row count of both fixture tables.
	queryGrammarRowsByte = 2

	// queryGrammarScoreByte locates the first nullable score choice.
	queryGrammarScoreByte = 3

	// queryGrammarChoiceByte locates the first family choice.
	queryGrammarChoiceByte = queryGrammarScoreByte + maxRows

	// queryGrammarChoiceCount is the number of family choices a seed spells out.
	queryGrammarChoiceCount = 8

	// queryGrammarSeedsPerFamily bounds the systematic seeds of one family.
	queryGrammarSeedsPerFamily = 16

	// queryGrammarSeedKeyBytes is the key size of the seed generator.
	queryGrammarSeedKeyBytes = 32

	// queryGrammarChoice3 locates the fourth family choice.
	queryGrammarChoice3 = 3

	// queryGrammarChoice4 locates the fifth family choice.
	queryGrammarChoice4 = 4

	// queryGrammarChoice5 locates the sixth family choice.
	queryGrammarChoice5 = 5

	// queryGrammarChoice6 locates the seventh family choice.
	queryGrammarChoice6 = 6

	// queryGrammarItemID names the items key in a query over the items table.
	queryGrammarItemID = "i.id"

	// queryGrammarItemScore names the items score in a query over the items table.
	queryGrammarItemScore = "i.score"

	// queryGrammarOver starts a window specification.
	queryGrammarOver = " OVER ("

	// queryGrammarFamilyPrefix names family features.
	queryGrammarFamilyPrefix = "grammar/"
)

const (
	// queryGrammarOuterJoin selects a single inner or outer join.
	queryGrammarOuterJoin = iota

	// queryGrammarJoinGroup selects a parenthesised join group under another join.
	queryGrammarJoinGroup

	// queryGrammarCrossJoin selects a cross join combined with an outer join.
	queryGrammarCrossJoin

	// queryGrammarLateral selects a lateral derived table.
	queryGrammarLateral

	// queryGrammarUsing selects a join whose key columns merge through USING.
	queryGrammarUsing

	// queryGrammarNatural selects a join whose common columns merge through NATURAL.
	queryGrammarNatural

	// queryGrammarRecursive selects a recursive CTE whose columns shift towards NULL.
	queryGrammarRecursive

	// queryGrammarWindow selects window functions over explicit frames.
	queryGrammarWindow

	// queryGrammarScalarLimit selects scalar subqueries with literal LIMIT and OFFSET.
	queryGrammarScalarLimit

	// queryGrammarGrouping selects ROLLUP, CUBE and GROUPING SETS.
	queryGrammarGrouping

	// queryGrammarWrapped selects DISTINCT, UNION and GROUP BY over converted columns.
	queryGrammarWrapped

	// queryGrammarIdentifiers selects mixed-case, reserved and dotted aliases.
	queryGrammarIdentifiers

	// queryGrammarFamilyCount bounds the permanently assigned families.
	queryGrammarFamilyCount
)

// queryGrammarBuilders constructs a query of each family, indexed by the family.
var queryGrammarBuilders = [queryGrammarFamilyCount]func(engine string, choices queryGrammarChoices) queryGrammarQuery{
	queryGrammarOuterJoin: queryGrammarOuterJoinQuery,
	queryGrammarJoinGroup: queryGrammarJoinGroupQuery,
	queryGrammarCrossJoin: queryGrammarCrossJoinQuery,
	queryGrammarLateral: func(_ string, choices queryGrammarChoices) queryGrammarQuery {
		return queryGrammarLateralQuery(choices)
	},
	queryGrammarUsing: func(engine string, choices queryGrammarChoices) queryGrammarQuery {
		return queryGrammarMergeQuery(engine, choices, false)
	},
	queryGrammarNatural: func(engine string, choices queryGrammarChoices) queryGrammarQuery {
		return queryGrammarMergeQuery(engine, choices, true)
	},
	queryGrammarRecursive:   queryGrammarRecursiveQuery,
	queryGrammarWindow:      queryGrammarWindowQuery,
	queryGrammarScalarLimit: queryGrammarScalarQuery,
	queryGrammarGrouping:    queryGrammarGroupingQuery,
	queryGrammarWrapped:     queryGrammarWrappedQuery,
	queryGrammarIdentifiers: queryGrammarIdentifierQuery,
}

// queryGrammarFamilies names each permanently assigned family in byte order.
var queryGrammarFamilies = [queryGrammarFamilyCount]string{"outer-join", "join-group", "cross-join", "lateral", "using", "natural",
	"recursive-cte", "window-frame", "scalar-limit", "grouping-sets", "wrapped-columns", "identifiers"}

// queryGrammarColumn is one output column with its independently derived nullability.
type queryGrammarColumn struct {
	// expression is the select-list expression.
	expression string

	// alias names the output column without quotes.
	alias string

	// nullable reports that the rule table admits NULL in the column.
	nullable bool
}

// queryGrammarQuery is one generated query before its directive is attached.
type queryGrammarQuery struct {
	// sql is the complete statement without a terminator.
	sql string

	// columns lists the output columns in order.
	columns []queryGrammarColumn

	// features records the constructs the query uses.
	features []string

	// ordered reports a total order over the output rows.
	ordered bool
}

// queryGrammarChoices reads bounded family choices from an input.
type queryGrammarChoices struct {
	// data holds the complete versioned input.
	data []byte
}

// pick reads one family choice within a bound.
//
// Takes index (int) which counts choices from the first family choice.
// Takes count (int) which bounds the result.
//
// Returns int which lies in [0, count).
func (choices queryGrammarChoices) pick(index, count int) int {
	return int(scenarioByte(choices.data, queryGrammarChoiceByte+index)) % count
}

// QueryGrammarCapabilities lists the families and constructs of version 14 for an engine.
//
// Each excluded entry records the engine or fixture reason that keeps it out.
//
// Takes engine (string) which selects the native grammar.
//
// Returns []Capability which includes admitted and excluded features.
func QueryGrammarCapabilities(engine string) []Capability {
	if !supportsShape(engine, shapeSelect) {
		return nil
	}
	result := make([]Capability, 0, len(queryGrammarFamilies)+len(queryGrammarVariantRules))
	for _, family := range queryGrammarFamilies {
		result = append(result, queryGrammarCapability(engine, queryGrammarFamilyPrefix+family))
	}
	for _, rule := range queryGrammarVariantRules {
		result = append(result, queryGrammarCapability(engine, rule.feature))
	}
	return result
}

// QueryGrammarFeatures lists the version 14 features seeds must reach on an engine.
//
// Takes engine (string) which selects the native grammar.
//
// Returns []string which holds every admitted feature in sorted order.
func QueryGrammarFeatures(engine string) []string {
	var features []string
	for _, capability := range QueryGrammarCapabilities(engine) {
		if capability.Status == capabilitySupported {
			features = append(features, capability.Feature)
		}
	}
	slices.Sort(features)
	return features
}

// QueryGrammarSeeds covers every admitted family and construct of version 14.
//
// The first two choices of each seed count through the family's main constructs and the
// other choices come from a fixed pseudo-random stream.
//
// Takes engine (string) which selects the native grammar.
//
// Returns [][]byte which holds bounded inputs with empty, single and full tables.
func QueryGrammarSeeds(engine string) [][]byte {
	if !supportsShape(engine, shapeSelect) {
		return nil
	}
	rows := [...]byte{maxRows, maxRows, 1, maxRows, 0, maxRows, categoryCount, maxRows}
	var seeds [][]byte
	for family := range queryGrammarFamilyCount {
		if !queryGrammarSupports(engine, queryGrammarFamilyPrefix+queryGrammarFamilies[family]) {
			continue
		}
		random := rand.NewChaCha8([queryGrammarSeedKeyBytes]byte{QueryGrammarVersion, byte(family)})
		for variant := range queryGrammarSeedsPerFamily {
			seed := make([]byte, queryGrammarChoiceByte+queryGrammarChoiceCount)
			if _, err := random.Read(seed); err != nil {

				continue
			}
			seed[0], seed[queryGrammarFamilyByte], seed[queryGrammarRowsByte] = QueryGrammarVersion, byte(family), rows[variant%len(rows)]
			seed[queryGrammarChoiceByte], seed[queryGrammarChoiceByte+1] = byte(variant), byte(variant/2)
			seeds = append(seeds, seed)
		}
	}
	return seeds
}

// queryGrammarScenario decodes one version 14 input.
//
// Takes engine (string) which selects the native grammar.
// Takes data ([]byte) which holds the versioned choices.
//
// Returns Scenario which holds one query with expected nullability per column.
// Returns bool which is false for an oversized input or an excluded family.
func queryGrammarScenario(engine string, data []byte) (Scenario, bool) {
	family := int(scenarioByte(data, queryGrammarFamilyByte)) % queryGrammarFamilyCount
	if len(data) > MaxBytes || !queryGrammarSupports(engine, queryGrammarFamilyPrefix+queryGrammarFamilies[family]) {
		return Scenario{}, false
	}
	base := []byte{Version, 0, 0, scenarioByte(data, queryGrammarRowsByte) % (maxRows + 1)}
	for row := range maxRows {
		base = append(base, scenarioByte(data, queryGrammarScoreByte+row))
	}
	model, ok := Decode(engine, base)
	if !ok {
		return Scenario{}, false
	}
	query := queryGrammarBuilders[family](engine, queryGrammarChoices{data: data})
	nullable := make([]bool, len(query.columns))
	for index, column := range query.columns {
		nullable[index] = column.nullable
	}
	features := append([]string{queryGrammarFamilyPrefix + queryGrammarFamilies[family]}, query.features...)
	slices.Sort(features)
	return Scenario{Engine: engine, Data: append([]byte(nil), data...), Schema: model.Migrations(), Inserts: model.Inserts(),
		Queries: []ScenarioQuery{{Name: "Grammar", SQL: "-- sgl.query(Grammar, many)\n" + query.sql + ";", Reference: query.sql,
			Values: nil, Bindings: nil, MayBeNull: nullable, Columns: len(query.columns), Ordered: query.ordered, Writes: false}},
		Relations: nil, Features: slices.Compact(features)}, true
}

// queryGrammarSelect renders a select list with plain aliases.
//
// Takes columns ([]queryGrammarColumn) which lists the outputs in order.
//
// Returns string which starts with SELECT.
func queryGrammarSelect(columns []queryGrammarColumn) string {
	items := make([]string, len(columns))
	for index, column := range columns {
		items[index] = column.expression + " AS " + column.alias
	}
	return "SELECT " + strings.Join(items, columnSeparator)
}
