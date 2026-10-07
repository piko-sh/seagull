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

package postgres

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

// keywordArgument is the keyword a function takes as one of its arguments.
type keywordArgument struct {
	// keywords holds the keywords the argument takes, in upper case.
	keywords map[string]bool

	// position is the zero-based position of the argument.
	position int
}

var (
	// keywordArguments maps each function that takes a keyword, written as a bare word, at
	// an argument position to that position and the keywords it takes there, as the Unicode
	// normalisation form NFC the string normalising function takes second. Names are in
	// lower case and keywords in upper case.
	keywordArguments = map[string]keywordArgument{
		"normalize": {position: 1, keywords: map[string]bool{"NFC": true, "NFD": true, "NFKC": true, "NFKD": true}},
	}
)

// readKeywordArgument returns a call's arguments with a keyword the called function
// accepts as an argument, as the normalisation form NFC, read as the keyword's name, a
// text value, rather than as a column of that name.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes arguments ([]ast.Expression) which are the call's arguments, changed in place.
//
// Returns []ast.Expression which holds the arguments.
func readKeywordArgument(loweredName string, arguments []ast.Expression) []ast.Expression {
	argument, takesKeyword := keywordArguments[loweredName]
	if !takesKeyword || argument.position >= len(arguments) {
		return arguments
	}
	reference, isReference := arguments[argument.position].(*ast.ColumnRefExpression)
	if !isReference || reference.TableAlias != "" || !argument.keywords[strings.ToUpper(reference.ColumnName)] {
		return arguments
	}
	arguments[argument.position] = &ast.LiteralExpression{TypeName: "text"}
	return arguments
}
