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
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// checkConstraint records a CHECK constraint by name, as the catalogue keeps no
// expression.
//
// Takes name (string) which is the constraint's name.
//
// Returns *sqlmodel.Constraint which is the constraint.
func checkConstraint(name string) *sqlmodel.Constraint {
	return &sqlmodel.Constraint{Name: name, Kind: sqlmodel.ConstraintCheck, ForeignTable: "", Columns: nil, ForeignColumns: nil,
		Origin: sqlmodel.MigrationOrigin{}}
}

// checkSecondaryDeclaration rejects malformed secondary declarations.
//
// It accepts INDEX name expression TYPE type, CONSTRAINT name CHECK or ASSUME expression,
// and PROJECTION name (query). It also rejects a column named with one of these keywords
// when the engine would reject it.
//
// Returns error when the entry does not have its declaration's shape.
func (p *parser) checkSecondaryDeclaration() error {
	keyword := p.Current().Keyword(tokenIdentifier)
	name, after := p.Peek(), p.PeekAt(2)
	if name.Kind != tokenIdentifier {
		return dialectkit.NewParseError(name, "expected a name after "+keyword)
	}
	switch keyword {
	case "CONSTRAINT":
		if !after.IsKeyword(tokenIdentifier, "CHECK") && !after.IsKeyword(tokenIdentifier, "ASSUME") {
			return dialectkit.NewParseError(after, "expected CHECK or ASSUME after the constraint's name")
		}
	case "PROJECTION":
		if after.Kind != tokenLeftParen {
			return dialectkit.NewParseError(after, "expected the projection's query in parentheses")
		}
	default:
		if !p.entryHolds("TYPE") {
			return dialectkit.NewParseError(after, "expected an expression and TYPE after the index's name")
		}
	}
	return nil
}

// entryHolds reports whether the current CREATE TABLE entry holds a keyword outside
// parentheses before the comma or closing parenthesis that ends it.
//
// Takes keyword (string) which is the keyword.
//
// Returns bool which is true when the entry holds it.
func (p *parser) entryHolds(keyword string) bool {
	depth := 0
	for index := p.Position; index < len(p.Tokens); index++ {
		current := p.Tokens[index]
		switch {
		case current.Kind == tokenLeftParen:
			depth++
		case current.Kind == tokenRightParen && depth == 0, current.Kind == tokenComma && depth == 0:
			return false
		case current.Kind == tokenRightParen:
			depth--
		case depth == 0 && current.IsKeyword(tokenIdentifier, keyword):
			return true
		}
	}
	return false
}
