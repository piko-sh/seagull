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

package directive

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// BindNamedAnchors renumbers each named parameter directive to the number the dialect
// gave the placeholder it names.
//
// The parser numbers a named directive in the order the directive lines appear, because
// it has not seen the statement. A dialect numbers a named placeholder by where it first
// appears in the statement, so the two orders disagree whenever the lines are written in
// another order, and the directive's name, type and qualities would land on a different
// parameter. A named directive whose anchor the statement never uses is given a number
// past every placeholder and positional directive, so it cannot take over another
// parameter; the unreferenced-parameter check reports it. When the dialect names none of
// the statement's placeholders there is nothing to match, and the directives keep the
// numbers the parser gave them.
//
// Takes parameters ([]*ParameterDirective) which are the query's parameter directives,
// renumbered in place.
// Takes references ([]ast.RawParameterReference) which are the placeholder uses the
// dialect found in the statement.
func BindNamedAnchors(parameters []*ParameterDirective, references []ast.RawParameterReference) {
	numbers, highest := namedReferenceNumbers(references)
	if len(numbers) == 0 {
		return
	}
	for _, parameter := range parameters {
		if !parameter.IsNamed && parameter.Kind != sqlmodel.ParameterDirectiveSortable {
			highest = max(highest, parameter.Number)
		}
	}
	for _, parameter := range parameters {
		if !parameter.IsNamed || parameter.Anchor == "" {
			continue
		}
		if number, found := numbers[parameter.Anchor]; found {
			parameter.Number = number
			continue
		}
		highest++
		parameter.Number = highest
	}
}

// namedReferenceNumbers maps each named placeholder to the number of its first use.
//
// Takes references ([]ast.RawParameterReference) which are the placeholder uses the
// dialect found in the statement.
//
// Returns map[string]int which maps a placeholder identifier to its number.
// Returns int which is the highest number any placeholder carries.
func namedReferenceNumbers(references []ast.RawParameterReference) (map[string]int, int) {
	numbers := make(map[string]int, len(references))
	highest := 0
	for _, reference := range references {
		highest = max(highest, reference.Number)
		if reference.Name == "" {
			continue
		}
		if _, seen := numbers[reference.Name]; !seen {
			numbers[reference.Name] = reference.Number
		}
	}
	return numbers, highest
}
