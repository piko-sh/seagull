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
	// queryGrammarWrappedPrefix starts every wrapped column construct name.
	queryGrammarWrappedPrefix = "wrapped/"

	// queryGrammarWrappedAlias names the converted output column.
	queryGrammarWrappedAlias = "wrapped"

	// queryGrammarUnion names the compound wrapped shape.
	queryGrammarUnion = "union"

	// queryGrammarDecimal names the decimal conversion.
	queryGrammarDecimal = "decimal"

	// queryGrammarDistinctAlias names DISTINCT ordered by alias.
	queryGrammarDistinctAlias = "distinct-alias"

	// queryGrammarDistinctOrdinal names DISTINCT ordered by ordinal.
	queryGrammarDistinctOrdinal = "distinct-ordinal"

	// queryGrammarGroupAlias names GROUP BY and ORDER BY an alias.
	queryGrammarGroupAlias = "group-alias"

	// queryGrammarDistinctDerived names DISTINCT inside a derived table.
	queryGrammarDistinctDerived = "distinct-derived"

	// queryGrammarArray names the array conversion.
	queryGrammarArray = "array"

	// queryGrammarShapeChoice locates the wrapped shape choice.
	queryGrammarShapeChoice = 1
)

// queryGrammarJSONArrayFunctions names the function that builds a JSON array on engines
// outside the PostgreSQL family.
var queryGrammarJSONArrayFunctions = map[string]string{engineMySQL: "JSON_ARRAY", engineMariaDB: "JSON_ARRAY", engineSQLite: "json_array", engineDuckDB: "to_json"}

// queryGrammarWrappedTypes lists the converted column constructs in choice order.
var queryGrammarWrappedTypes = []string{queryGrammarDecimal, queryGrammarArray, "json", "interval", "uuid"}

// queryGrammarWrappedShapes lists the clause constructs over a converted column in choice
// order.
var queryGrammarWrappedShapes = []string{queryGrammarDistinctAlias, queryGrammarDistinctOrdinal, queryGrammarUnion, queryGrammarGroupAlias, "group-ordinal", queryGrammarDistinctDerived}

// queryGrammarAwkwardNames lists awkward aliases with the construct each one exercises.
//
// Their Go names differ, so no two of them can collide as generated fields.
var queryGrammarAwkwardNames = []struct {
	name    string
	feature string
}{
	{name: "MixedCase", feature: "mixed-case"}, {name: "select", feature: "reserved"}, {name: "a.b", feature: "dotted"},
	{name: "Weird Name", feature: "spaced"}, {name: "Order", feature: "reserved"}, {name: "user", feature: "reserved"},
	{name: "from", feature: "reserved"}, {name: "x.Y", feature: "dotted"},
}

// queryGrammarWrappedExpression converts a column to a type some drivers cannot read
// directly.
//
// A decimal, interval or UUID conversion keeps the source's nullability. An array and the
// JSON array builders give a value even for a NULL element, but DuckDB's to_json gives
// NULL for NULL.
//
// Takes engine (string) which selects the native spelling.
// Takes kind (string) which names the conversion.
// Takes alias (string) which qualifies the source columns.
// Takes nullable (bool) which reads the nullable score instead of the key.
//
// Returns queryGrammarColumn which has the wrapped alias.
func queryGrammarWrappedExpression(engine, kind, alias string, nullable bool) queryGrammarColumn {
	source := alias + ".id * 7"
	if nullable {
		source = alias + ".score * 7"
	}
	postgresLike := postgresFamily(engine)
	expression, result := "CAST("+source+" AS DECIMAL(12, 2))", nullable
	switch kind {
	case queryGrammarDecimal:
		if engine == engineClickHouse && nullable {
			expression = "CAST(" + source + " AS Nullable(Decimal(12, 2)))"
		}
	case queryGrammarArray:
		expression, result = "["+source+columnSeparator+alias+".id]", false
		if postgresLike {
			expression = "ARRAY" + expression
		}
	case "json":
		expression, result = queryGrammarJSONArrayFunctions[engine], false
		if postgresLike {
			expression = "jsonb_build_array"
		}
		expression += "(" + source + ")"
		result = engine == engineDuckDB && nullable
	case "interval":
		expression = source + " * INTERVAL '1 day'"
	default:
		expression = "CAST(md5(CAST(" + source + " AS TEXT)) AS UUID)"
	}
	return queryGrammarColumn{expression: expression, alias: queryGrammarWrappedAlias, nullable: result}
}

// queryGrammarWrappedQuery puts a converted column under DISTINCT, UNION, GROUP BY and
// ORDER BY.
//
// DISTINCT, UNION and GROUP BY leave one row per value, so ordering by every output
// column gives a total order. ClickHouse applies a trailing ORDER BY to the last UNION
// branch only, so its compound stays unordered.
//
// Takes engine (string) which selects the admitted conversions and spelling.
// Takes choices (queryGrammarChoices) which selects the conversion, the shape and the
// sources.
//
// Returns queryGrammarQuery which projects the converted column.
func queryGrammarWrappedQuery(engine string, choices queryGrammarChoices) queryGrammarQuery {
	kinds := queryGrammarSupportedOf(engine, queryGrammarWrappedPrefix, queryGrammarWrappedTypes)
	kind := kinds[choices.pick(0, len(kinds))]
	shapes := queryGrammarWrappedShapes
	if kind == queryGrammarArray && postgresFamily(engine) {

		shapes = []string{queryGrammarDistinctDerived}
	}
	shape := shapes[choices.pick(queryGrammarShapeChoice, len(shapes))]
	column := queryGrammarWrappedExpression(engine, kind, "i", choices.pick(2, 2) == 1)
	columns := []queryGrammarColumn{column}
	from := " FROM fuzz_items i"
	sql, ordered := "", true
	switch shape {
	case queryGrammarDistinctAlias:
		sql = "SELECT DISTINCT " + column.expression + " AS wrapped" + from + " ORDER BY wrapped"
	case queryGrammarDistinctOrdinal:

		ordered = !column.nullable
		columns = append(columns, queryGrammarColumn{expression: "i.category", alias: queryGrammarCategory, nullable: false})
		sql = "SELECT DISTINCT" + strings.TrimPrefix(queryGrammarSelect(columns), "SELECT") + from + " ORDER BY 1, 2"
	case queryGrammarUnion:
		other := queryGrammarWrappedExpression(engine, kind, "p", choices.pick(queryGrammarChoice3, 2) == 1)
		columns[0].nullable = column.nullable || other.nullable
		sql = queryGrammarSelect(columns[:1]) + from + " UNION SELECT " + other.expression + " FROM fuzz_peers p ORDER BY 1"
		if engine == engineClickHouse {
			sql, ordered = queryGrammarSelect(columns[:1])+from+" UNION DISTINCT SELECT "+other.expression+" FROM fuzz_peers p", false
		}
	case queryGrammarDistinctDerived:
		sql, ordered = "SELECT d.wrapped AS wrapped FROM (SELECT DISTINCT "+column.expression+" AS wrapped"+from+") d", false
	default:
		columns = append(columns, queryGrammarColumn{expression: "COUNT(*)", alias: "total", nullable: false})
		key := "1"
		if shape == queryGrammarGroupAlias {
			key = queryGrammarWrappedAlias
		}
		sql = queryGrammarSelect(columns) + from + " GROUP BY " + key + " ORDER BY " + key
	}
	return queryGrammarQuery{sql: sql, columns: columns, features: []string{queryGrammarWrappedPrefix + kind, queryGrammarWrappedPrefix + shape}, ordered: ordered}
}

// queryGrammarIdentifierQuery reads a CTE through awkward names used as aliases and sort
// keys.
//
// Takes engine (string) which selects the quote character.
// Takes choices (queryGrammarChoices) which selects the names.
//
// Returns queryGrammarQuery which orders by the quoted alias of the key.
func queryGrammarIdentifierQuery(engine string, choices queryGrammarChoices) queryGrammarQuery {
	count := len(queryGrammarAwkwardNames)
	name := func(choice, offset int) string {
		return queryGrammarAwkwardNames[(choices.pick(choice, count)+offset)%count].name
	}
	features := make([]string, 0, count)
	use := func(choice, offset int) string {
		identifier := queryGrammarAwkwardNames[(choices.pick(choice, count)+offset)%count]
		features = append(features, "identifier/"+identifier.feature)
		return queryGrammarQuote(engine, identifier.name)
	}
	cte, table := use(0, 0), use(1, 0)
	sources := []string{queryGrammarItemID, queryGrammarItemScore, "i.category"}
	nullable := []bool{false, true, false}
	inner := make([]string, 0, len(sources))
	columns := make([]queryGrammarColumn, 0, len(sources))
	for index, source := range sources {
		inner = append(inner, source+" AS "+use(2, index))
		alias := name(queryGrammarChoice3, index)
		columns = append(columns, queryGrammarColumn{expression: table + "." + queryGrammarQuote(engine, name(2, index)), alias: alias, nullable: nullable[index]})
	}
	items := make([]string, len(columns))
	for index, column := range columns {
		items[index] = column.expression + " AS " + use(queryGrammarChoice3, index)
	}
	as := " "
	if engine == engineClickHouse {

		as = " AS "
	}
	sql := "WITH " + cte + " AS (SELECT " + strings.Join(inner, columnSeparator) + " FROM fuzz_items i) SELECT " + strings.Join(items, columnSeparator) +
		" FROM " + cte + as + table + " ORDER BY " + queryGrammarQuote(engine, columns[0].alias)
	return queryGrammarQuery{sql: sql, columns: columns, features: features, ordered: true}
}
