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
	"strings"
)

// domain selects the values a parameter takes.
type domain uint8

const (
	// domainSmall is a small integer.
	domainSmall domain = iota

	// domainText is a short text.
	domainText

	// domainPattern is a LIKE pattern.
	domainPattern

	// domainNeedle is a non-empty text to replace.
	domainNeedle

	// domainIdentifier is a new row's identifier.
	domainIdentifier
)

// param describes one expression parameter.
type param struct {
	// native is the parameter's ClickHouse type.
	native string

	// values lists the parameter's choices.
	values domain
}

// expression is one catalogue query over the expression table.
type expression struct {
	// sql writes the query with the engine's placeholders and spellings.
	sql func(engine string, p func(int) string) string

	// excluded explains, per engine, why the query is left out.
	excluded map[string]string

	// name is the generated method name.
	name string

	// params lists the parameters in placeholder order.
	params []param

	// writes reports an exec query, which the read query observes.
	writes bool
}

var (
	// small is the small integer parameter the expression queries share.
	small = param{native: "Int32", values: domainSmall}

	// text is the text parameter the expression queries share.
	text = param{native: "String", values: domainText}

	// pattern is the LIKE pattern parameter the expression queries share.
	pattern = param{native: "String", values: domainPattern}

	// needle is the substring parameter the expression queries share.
	needle = param{native: "String", values: domainNeedle}

	// identifier is the row identifier parameter the expression queries share.
	identifier = param{native: "Int64", values: domainIdentifier}
)

// expressions lists the expression queries in the order a case runs them.
var expressions = []expression{
	{name: "ExprArithmetic", params: []param{small, small}, excluded: nil, writes: false, sql: func(_ string, p func(int) string) string {
		return "SELECT id, a + " + p(1) + " AS plus, a * " + p(2) + " AS times, b - a AS diff, -a AS negated FROM typed_expr ORDER BY id"
	}},
	{name: "ExprCompare", params: []param{small, small, small, text}, excluded: nil, writes: false, sql: func(_ string, p func(int) string) string {
		return "SELECT id, a < " + p(1) + " AS below, a BETWEEN " + p(2) + " AND " + p(3) + " AS inside, s = " + p(4) + " AS same FROM typed_expr ORDER BY id"
	}},
	{name: "ExprText", params: []param{text, text, needle, text}, excluded: nil, writes: false, sql: func(engine string, p func(int) string) string {
		lower, upper, length := "lower", "upper", "char_length"
		switch engine {
		case engineSQLite, engineDuckDB:
			length = "length"
		case engineClickHouse:
			length = "lengthUTF8"
		}
		return "SELECT id, " + concat(engine, "s", p(1)) + " AS joined, NULLIF(s, " + p(2) + ") AS cleared, " + lower + "(s) AS low, " + upper +
			"(s) AS high, replace(s, " + p(3) + ", " + p(4) + ") AS swapped, " + length + "(s) AS size FROM typed_expr ORDER BY id"
	}},
	{name: "ExprFilter", params: []param{pattern, small, small}, excluded: nil, writes: false, sql: func(_ string, p func(int) string) string {
		return "SELECT id FROM typed_expr WHERE s LIKE " + p(1) + " AND a BETWEEN " + p(2) + " AND " + p(3) + " ORDER BY id"
	}},
	{name: "ExprJoin", params: []param{small}, excluded: nil, writes: false, sql: func(_ string, p func(int) string) string {
		return "SELECT x.id, y.id AS other FROM typed_expr x JOIN typed_expr y ON y.a = x.a + " + p(1) + " ORDER BY x.id, y.id"
	}},
	{name: "ExprCoalesce", params: []param{small}, excluded: nil, writes: false, sql: func(_ string, p func(int) string) string {
		return "SELECT id, COALESCE(a, " + p(1) + ") AS filled, abs(b) AS magnitude FROM typed_expr ORDER BY id"
	}},
	{name: "ExprInsert", params: []param{identifier, small, small, text}, excluded: nil, writes: true, sql: func(_ string, p func(int) string) string {
		return "INSERT INTO typed_expr (id, a, b, s) VALUES (" + p(1) + ", " + p(2) + " + 1, " + p(3) + " * 2, " + p(4) + ")"
	}},
	{name: "ExprUpdate", params: []param{small, text, small}, writes: true,
		excluded: map[string]string{engineClickHouse: "ClickHouse has no UPDATE statement. ALTER TABLE UPDATE is an asynchronous mutation."}, sql: func(_ string, p func(int) string) string {
			return "UPDATE typed_expr SET a = a + " + p(1) + ", s = " + p(2) + " WHERE id = " + p(3)
		}},
	{name: "ExprRead", params: nil, excluded: nil, writes: false, sql: func(_ string, _ func(int) string) string {
		return "SELECT id, a, b, s FROM typed_expr ORDER BY id"
	}},
}

// concat writes the engine's text concatenation.
//
// Takes engine (string) which names the dialect.
// Takes left (string) which is the first operand.
// Takes right (string) which is the second operand.
//
// Returns string which concatenates them, NULL when either is NULL.
func concat(engine, left, right string) string {
	switch engine {
	case engineMySQL, engineMariaDB, engineClickHouse:
		return "concat(" + left + ", " + right + ")"
	default:
		return left + " || " + right
	}
}

// expressionSchema creates the expression table.
//
// Takes engine (string) which names the dialect.
// Takes identifier (string) which is the identifier column type.
// Takes suffix (string) which ends the statement.
//
// Returns string which is the CREATE TABLE statement.
func expressionSchema(engine, identifier, suffix string) string {
	small, large := "INTEGER", "BIGINT"
	switch engine {
	case engineCockroach:
		small = "INT4"
	case engineMySQL, engineMariaDB:
		small = "INT"
	case engineClickHouse:
		small, large = "Nullable(Int32)", "Nullable(Int64)"
	}
	return fmt.Sprintf("CREATE TABLE typed_expr (id %s, a %s, b %s, s %s)%s;", identifier, small, large, textType(engine), suffix)
}

// expressionQueries writes the expression queries.
//
// Takes engine (string) which names the dialect.
//
// Returns string which holds them with their directives.
func expressionQueries(engine string) string {
	var file strings.Builder
	for _, entry := range admittedExpressions(engine) {
		command := "many"
		if entry.writes {
			command = "exec"
		}
		sql := entry.sql(engine, func(number int) string { return parameter(engine, number, entry.params[number-1].native) })
		fmt.Fprintf(&file, "-- sgl.query(%s, %s)\n%s;\n\n", entry.name, command, sql)
	}
	return file.String()
}

// admittedExpressions lists the expression queries an engine runs, in order.
//
// Takes engine (string) which names the dialect.
//
// Returns []*expression which excludes the queries the engine lacks.
func admittedExpressions(engine string) []*expression {
	var result []*expression
	for index := range expressions {
		if _, excluded := expressions[index].excluded[engine]; !excluded {
			result = append(result, &expressions[index])
		}
	}
	return result
}
