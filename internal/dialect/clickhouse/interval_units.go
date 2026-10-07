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

package clickhouse

import (
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/ast"
)

const (
	// messageUnknownIntervalUnit says that an INTERVAL names a unit ClickHouse does not
	// know.
	messageUnknownIntervalUnit = "unknown interval unit: expected a unit such as NANOSECOND, MICROSECOND, " +
		"MILLISECOND, SECOND, MINUTE, HOUR, DAY, WEEK, MONTH, QUARTER or YEAR"

	// intervalStringFields is how many words the string of INTERVAL 'n unit' holds: the
	// value and its unit.
	intervalStringFields = 2
)

var (
	// unitFirstFunctions names the functions whose first argument ClickHouse reads as an
	// interval unit when it is a bare word, as in date_diff(day, start, end) or
	// date_add(HOUR, 1, ts), keyed in lower case.
	unitFirstFunctions = map[string]bool{
		"datediff": true, "date_diff": true, "timestampdiff": true, "timestamp_diff": true,
		"dateadd": true, "date_add": true, "timestampadd": true, "timestamp_add": true,
		"datesub": true, "date_sub": true, "timestampsub": true, "timestamp_sub": true,
	}

	// intervalUnitWords maps every spelling of an interval unit ClickHouse accepts as a bare
	// word, the unit, its plural, its SQL_TSI_ form and its abbreviations, in upper case, to
	// the unit's name as an Interval type names it.
	intervalUnitWords = map[string]string{
		"NANOSECOND": "Nanosecond", "NANOSECONDS": "Nanosecond", "SQL_TSI_NANOSECOND": "Nanosecond", "NS": "Nanosecond",
		"MICROSECOND": "Microsecond", "MICROSECONDS": "Microsecond", "SQL_TSI_MICROSECOND": "Microsecond", "MCS": "Microsecond",
		"MILLISECOND": "Millisecond", "MILLISECONDS": "Millisecond", "SQL_TSI_MILLISECOND": "Millisecond", "MS": "Millisecond",
		"SECOND": "Second", "SECONDS": "Second", "SQL_TSI_SECOND": "Second", "SS": "Second", "S": "Second",
		"MINUTE": "Minute", "MINUTES": "Minute", "SQL_TSI_MINUTE": "Minute", "MI": "Minute", "N": "Minute",
		"HOUR": "Hour", "HOURS": "Hour", "SQL_TSI_HOUR": "Hour", "HH": "Hour", "H": "Hour",
		"DAY": "Day", "DAYS": "Day", "SQL_TSI_DAY": "Day", "DD": "Day", "D": "Day",
		"WEEK": "Week", "WEEKS": "Week", "SQL_TSI_WEEK": "Week", "WK": "Week", "WW": "Week",
		"MONTH": "Month", "MONTHS": "Month", "SQL_TSI_MONTH": "Month", "MM": "Month", "M": "Month",
		"QUARTER": "Quarter", "QUARTERS": "Quarter", "SQL_TSI_QUARTER": "Quarter", "QQ": "Quarter", "Q": "Quarter",
		"YEAR": "Year", "YEARS": "Year", "SQL_TSI_YEAR": "Year", "YYYY": "Year", "YY": "Year",
	}
)

// readUnitArgument reads a leading bare interval unit as a function argument.
//
// Date-shifting functions receive an interval; date-difference functions receive the
// unit's name as a String.
//
// Takes name (string) which is the function's name.
// Takes arguments ([]ast.Expression) which are the call's arguments, changed in place.
//
// Returns []ast.Expression which holds the arguments.
func readUnitArgument(name string, arguments []ast.Expression) []ast.Expression {
	if !unitFirstFunctions[strings.ToLower(name)] || len(arguments) == 0 {
		return arguments
	}
	reference, isReference := arguments[0].(*ast.ColumnRefExpression)
	if !isReference || reference.TableAlias != "" {
		return arguments
	}
	unit, isUnit := intervalUnitOf(reference.ColumnName)
	switch {
	case !isUnit:
	case unitArgumentAdders[strings.ToLower(name)]:
		arguments[0] = &ast.LiteralExpression{TypeName: intervalTypePrefix + unit}
	default:
		arguments[0] = &ast.LiteralExpression{TypeName: engineNameString}
	}
	return arguments
}

// intervalUnitOf looks up the unit a bare word names, ignoring the case of its ASCII
// letters. A word holding any other character names no unit, so a letter that upper-cases
// to an ASCII one, such as the long s, cannot spell one.
//
// Takes word (string) which is the word as written.
//
// Returns string which is the unit's name as an Interval type names it, such as Day.
// Returns bool which is false when the word names no unit.
func intervalUnitOf(word string) (string, bool) {
	for index := range len(word) {
		if word[index] >= utf8.RuneSelf {
			return "", false
		}
	}
	unit, isUnit := intervalUnitWords[strings.ToUpper(word)]
	return unit, isUnit
}
