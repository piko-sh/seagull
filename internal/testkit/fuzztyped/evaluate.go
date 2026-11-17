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
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// identifierBase starts the identifiers of inserted expression rows.
	identifierBase = 100

	// identifierChoices bounds the inserted identifiers.
	identifierChoices = 50
)

// exprRow is one modelled row of the expression table.
type exprRow struct {
	// a is the 32-bit integer, nil for NULL.
	a *int64

	// b is the 64-bit integer, nil for NULL.
	b *int64

	// s is the text, nil for NULL.
	s *string

	// id is the unique identifier.
	id int64
}

var (
	// smallRows holds the small integer row choices.
	smallRows = []int64{0, 1, -1, 7, -50}

	// largeRows holds the integer row choices beyond 32 bits.
	largeRows = []int64{0, 3000000000, -3000000000, 42}

	// textRows holds the text row choices.
	textRows = []string{"", "abc", "AbC", "a_c", "%x", "é", "É", "\U0001F600x"}

	// smallValues holds the small integer parameter choices.
	smallValues = []int64{-3, -1, 0, 1, 2, 5}

	// textValues holds the text parameter choices.
	textValues = []string{"", "a", "abc", "é", "AbC", "\U0001F600"}

	// patternValues holds the LIKE pattern parameter choices.
	patternValues = []string{"a%", "%c", "_", "%", "A%", "é%", "%b%", "___", "%x"}

	// needleValues holds the substring parameter choices.
	needleValues = []string{"a", "b", "é", "C"}
)

// expressionShapes gives the result column kinds of each expression query that reads.
var expressionShapes = map[string][]string{
	"ExprArithmetic": {KindInteger, KindInteger, KindInteger, KindInteger, KindInteger},
	"ExprCompare":    {KindInteger, KindBoolean, KindBoolean, KindBoolean},
	"ExprText":       {KindInteger, KindText, KindText, KindText, KindText, KindText, KindInteger},
	"ExprFilter":     {KindInteger},
	"ExprJoin":       {KindInteger, KindInteger},
	"ExprCoalesce":   {KindInteger, KindInteger, KindInteger},
	"ExprRead":       {KindInteger, KindInteger, KindInteger, KindText},
}

// asciiCaseEngines change only ASCII letters in lower and upper.
var asciiCaseEngines = []string{engineSQLite, engineClickHouse}

// integerBooleanEngines return comparisons as the integers zero and one.
var integerBooleanEngines = []string{engineMySQL, engineMariaDB, engineSQLite, engineClickHouse}

// choose picks a parameter value of the domain.
//
// Takes choice (int) which selects the value.
//
// Returns Value which is the parameter.
func (d domain) choose(choice int) Value {
	switch d {
	case domainText:
		return Text(textValues[choice%len(textValues)])
	case domainPattern:
		return Text(patternValues[choice%len(patternValues)])
	case domainNeedle:
		return Text(needleValues[choice%len(needleValues)])
	case domainIdentifier:
		return Integer(int64(identifierBase + choice%identifierChoices))
	default:
		return Integer(smallValues[choice%len(smallValues)])
	}
}

// decodeExpressions builds the expression rows and calls.
//
// Takes engine (string) which names the dialect.
// Takes count (int) which is the number of rows.
// Takes choices (*choiceReader) which supplies the row and parameter choices.
//
// Returns []string which inserts the rows directly.
// Returns []Call which runs every admitted expression query.
func decodeExpressions(engine string, count int, choices *choiceReader) ([]string, []Call) {
	rows := make([]exprRow, 0, count)
	inserts := make([]string, 0, count)
	for index := range count {
		row := exprRow{a: pick(smallRows, choices.next()), b: pick(largeRows, choices.next()), s: pick(textRows, choices.next()), id: int64(index + 1)}
		rows = append(rows, row)
		inserts = append(inserts, fmt.Sprintf("INSERT INTO typed_expr (id, a, b, s) VALUES (%d, %s, %s, %s)", row.id, integerLiteral(row.a),
			integerLiteral(row.b), textLiteral(row.s)))
	}
	usable := admittedExpressions(engine)
	calls := make([]Call, 0, len(usable))
	for _, entry := range usable {
		arguments := make([]Value, 0, len(entry.params))
		for _, current := range entry.params {
			arguments = append(arguments, current.values.choose(choices.next()))
		}
		call := Call{Method: entry.name, Arguments: arguments, Names: nil, Shapes: nil, Want: nil, Exec: entry.writes}
		if entry.writes {
			rows = apply(entry.name, rows, arguments)
		} else {
			call.Shapes, call.Want = evaluate(engine, entry.name, rows, arguments)
		}
		calls = append(calls, call)
	}
	return inserts, calls
}

// pick chooses a value or NULL, NULL being the choice after the last value.
//
// Takes values ([]T) which are the non-NULL choices.
// Takes choice (int) which selects one.
//
// Returns *T which is nil for NULL.
func pick[T any](values []T, choice int) *T {
	selected := choice % (len(values) + 1)
	if selected == len(values) {
		return nil
	}
	return &values[selected]
}

// apply changes the modelled table as an exec query does.
//
// Takes name (string) which selects the query.
// Takes rows ([]exprRow) which is the table.
// Takes arguments ([]Value) which are the parameters.
//
// Returns []exprRow which is the table afterwards, in identifier order.
func apply(name string, rows []exprRow, arguments []Value) []exprRow {
	number := func(index int) int64 { return integerOf(arguments[index]) }
	switch name {
	case "ExprInsert":
		a, b, s := number(1)+1, number(2)*2, arguments[len(arguments)-1].Text
		rows = append(slices.Clone(rows), exprRow{a: &a, b: &b, s: &s, id: number(0)})
	default:
		rows = slices.Clone(rows)
		for index := range rows {
			if rows[index].id != number(2) {
				continue
			}
			if rows[index].a != nil {
				sum := *rows[index].a + number(0)
				rows[index].a = &sum
			}
			text := arguments[1].Text
			rows[index].s = &text
		}
	}
	slices.SortFunc(rows, func(left, right exprRow) int { return cmp.Compare(left.id, right.id) })
	return rows
}

// evaluate gives a query's result kinds and rows over the modelled table.
//
// Takes engine (string) which names the dialect.
// Takes name (string) which selects the query.
// Takes rows ([]exprRow) which is the table in identifier order.
// Takes arguments ([]Value) which are the parameters.
//
// Returns []string which gives each column's kind.
// Returns [][]Value which holds the expected rows in order.
func evaluate(engine, name string, rows []exprRow, arguments []Value) ([]string, [][]Value) {
	var result [][]Value
	shapes := expressionShapes[name]
	for _, row := range rows {
		switch name {
		case "ExprFilter":
			if row.s != nil && row.a != nil && like(engine, *row.s, arguments[0].Text) && between(*row.a, arguments[1], arguments[2]) {
				result = append(result, []Value{Integer(row.id)})
			}
		case "ExprJoin":
			for _, other := range rows {
				if row.a != nil && other.a != nil && *other.a == *row.a+integerOf(arguments[0]) {
					result = append(result, []Value{Integer(row.id), Integer(other.id)})
				}
			}
		default:
			result = append(result, project(engine, name, row, arguments))
		}
	}
	return shapes, result
}

// project evaluates one row of a query that returns a row per table row.
//
// Takes engine (string) which names the dialect.
// Takes name (string) which selects the query.
// Takes row (exprRow) which is the table row.
// Takes arguments ([]Value) which are the parameters.
//
// Returns []Value which is the result row.
func project(engine, name string, row exprRow, arguments []Value) []Value {
	id := Integer(row.id)
	switch name {
	case "ExprArithmetic":
		return []Value{id, arithmetic(row.a, nil, integerOf(arguments[0]), '+'), arithmetic(row.a, nil, integerOf(arguments[1]), '*'),
			arithmetic(row.b, row.a, 0, '-'), arithmetic(row.a, nil, 0, 'n')}
	case "ExprCompare":
		below, inside, same := Null(), Null(), Null()
		if row.a != nil {
			below, inside = Boolean(*row.a < integerOf(arguments[0])), Boolean(between(*row.a, arguments[1], arguments[2]))
		}
		if row.s != nil {
			same = Boolean(*row.s == arguments[len(arguments)-1].Text)
		}
		return []Value{id, below, inside, same}
	case "ExprText":
		if row.s == nil {
			return []Value{id, Null(), Null(), Null(), Null(), Null(), Null()}
		}
		cleared := Text(*row.s)
		if *row.s == arguments[1].Text {
			cleared = Null()
		}
		return []Value{id, Text(*row.s + arguments[0].Text), cleared, Text(changeCase(engine, *row.s, unicode.ToLower)),
			Text(changeCase(engine, *row.s, unicode.ToUpper)), Text(strings.ReplaceAll(*row.s, arguments[2].Text, arguments[len(arguments)-1].Text)),
			Integer(int64(utf8.RuneCountInString(*row.s)))}
	case "ExprCoalesce":
		filled := Integer(integerOf(arguments[0]))
		if row.a != nil {
			filled = Integer(*row.a)
		}
		return []Value{id, filled, arithmetic(row.b, nil, 0, 'a')}
	default:
		return []Value{id, nullableInteger(row.a), nullableInteger(row.b), nullableText(row.s)}
	}
}

// arithmetic applies one integer operation with NULL propagation.
//
// Takes left (*int64) which is the first operand, nil for NULL.
// Takes right (*int64) which is a column second operand, nil to use constant.
// Takes constant (int64) which is the parameter second operand.
// Takes operation (byte) which is +, *, -, n for negation or a for absolute value.
//
// Returns Value which is the result or NULL.
func arithmetic(left, right *int64, constant int64, operation byte) Value {
	if left == nil {
		return Null()
	}
	if right != nil {
		constant = *right
	} else if operation == '-' {
		return Null()
	}
	switch operation {
	case '+':
		return Integer(*left + constant)
	case '*':
		return Integer(*left * constant)
	case '-':
		return Integer(*left - constant)
	case 'n':
		return Integer(-*left)
	default:
		return Integer(max(*left, -*left))
	}
}

// between tests an inclusive range given by two parameters.
//
// Takes value (int64) which is the tested value.
// Takes low (Value) which is the lower bound.
// Takes high (Value) which is the upper bound.
//
// Returns bool which reports low <= value <= high.
func between(value int64, low, high Value) bool {
	return integerOf(low) <= value && value <= integerOf(high)
}

// changeCase applies a case mapping, only to ASCII letters on engines that do so.
//
// Takes engine (string) which names the dialect.
// Takes text (string) which is the text.
// Takes mapping (func(rune) rune) which is unicode.ToLower or unicode.ToUpper.
//
// Returns string which is the mapped text.
func changeCase(engine, text string, mapping func(rune) rune) string {
	ascii := slices.Contains(asciiCaseEngines, engine)
	return strings.Map(func(letter rune) rune {
		if ascii && letter >= utf8.RuneSelf {
			return letter
		}
		return mapping(letter)
	}, text)
}

// like matches SQL LIKE by character, without an escape, folding ASCII case on SQLite.
//
// Takes engine (string) which names the dialect.
// Takes text (string) which is the value.
// Takes pattern (string) which is the pattern.
//
// Returns bool which reports a match.
func like(engine, text, pattern string) bool {
	if engine == engineSQLite {
		text, pattern = changeCase("", text, asciiLower), changeCase("", pattern, asciiLower)
	}
	return likeRunes([]rune(text), []rune(pattern))
}

// asciiLower lowers ASCII letters only.
//
// Takes letter (rune) which is one character.
//
// Returns rune which is its ASCII lower case.
func asciiLower(letter rune) rune {
	if letter >= 'A' && letter <= 'Z' {
		return letter + ('a' - 'A')
	}
	return letter
}

// likeRunes matches percent and underscore wildcards over characters.
//
// Takes text ([]rune) which is the value.
// Takes pattern ([]rune) which is the pattern.
//
// Returns bool which reports a match.
func likeRunes(text, pattern []rune) bool {
	if len(pattern) == 0 {
		return len(text) == 0
	}
	switch pattern[0] {
	case '%':
		for start := 0; start <= len(text); start++ {
			if likeRunes(text[start:], pattern[1:]) {
				return true
			}
		}
		return false
	case '_':
		return len(text) > 0 && likeRunes(text[1:], pattern[1:])
	default:
		return len(text) > 0 && text[0] == pattern[0] && likeRunes(text[1:], pattern[1:])
	}
}
