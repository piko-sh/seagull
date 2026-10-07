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

package mysql

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

var (
	// keywordFirstFunctions maps each function whose first argument MySQL reads as a
	// keyword, written as a bare word, to the keywords it takes there: the interval unit of
	// TIMESTAMPDIFF and TIMESTAMPADD and the temporal type of GET_FORMAT. Names are in lower
	// case and keywords in upper case.
	keywordFirstFunctions = map[string]map[string]bool{
		"timestampdiff": intervalUnitKeywords,
		"timestampadd":  intervalUnitKeywords,
		"get_format":    {"DATE": true, "TIME": true, "DATETIME": true, "TIMESTAMP": true},
	}

	// intervalUnitKeywords names the interval units TIMESTAMPDIFF and TIMESTAMPADD take,
	// with the SQL_TSI_ synonyms MySQL accepts for them.
	intervalUnitKeywords = map[string]bool{
		"MICROSECOND": true, "SECOND": true, "MINUTE": true, "HOUR": true, "DAY": true,
		"WEEK": true, "MONTH": true, "QUARTER": true, "YEAR": true,
		"SQL_TSI_MICROSECOND": true, "SQL_TSI_SECOND": true, "SQL_TSI_MINUTE": true, "SQL_TSI_HOUR": true,
		"SQL_TSI_DAY": true, "SQL_TSI_WEEK": true, "SQL_TSI_MONTH": true, "SQL_TSI_QUARTER": true, "SQL_TSI_YEAR": true,
	}
)

// readKeywordArgument returns a call's arguments with the keyword a function takes first,
// as the HOUR of TIMESTAMPDIFF(HOUR, start, end), read as the keyword's name, a text
// value, rather than as a column of that name.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes arguments ([]ast.Expression) which are the call's arguments, changed in place.
//
// Returns []ast.Expression which holds the arguments.
func readKeywordArgument(loweredName string, arguments []ast.Expression) []ast.Expression {
	keywords, takesKeyword := keywordFirstFunctions[loweredName]
	if !takesKeyword || len(arguments) == 0 {
		return arguments
	}
	reference, isReference := arguments[0].(*ast.ColumnRefExpression)
	if !isReference || reference.TableAlias != "" || !keywords[strings.ToUpper(reference.ColumnName)] {
		return arguments
	}
	arguments[0] = &ast.LiteralExpression{TypeName: "text"}
	return arguments
}
