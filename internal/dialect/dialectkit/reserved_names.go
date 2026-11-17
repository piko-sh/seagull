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

package dialectkit

// tableConstraintStarts lists the words that open a table constraint, whose parser, where
// a parenthesis follows the word, reports a malformed constraint better than a reserved
// name would.
var tableConstraintStarts = map[string]bool{
	"CONSTRAINT": true, "PRIMARY": true, "UNIQUE": true, "CHECK": true, "FOREIGN": true, "EXCLUDE": true,
}

// ReservedNames lists the keywords an engine refuses, written unquoted, as the name a
// definition gives a table or a column. A quoted name is never refused.
type ReservedNames struct {
	// columns holds the upper-cased keywords refused as a column's name.
	columns map[string]bool

	// tables holds the upper-cased keywords refused as a table's name.
	tables map[string]bool
}

// NewReservedNames builds the lists from whitespace-separated keywords.
//
// Takes columns (string) which lists the keywords refused as a column's name.
// Takes tables (string) which lists the keywords refused as a table's name.
//
// Returns ReservedNames which holds the lists.
func NewReservedNames(columns, tables string) ReservedNames {
	return ReservedNames{columns: keywordSet(columns), tables: keywordSet(tables)}
}

// RefusesColumn reports whether the engine refuses a keyword as a column's name.
//
// Takes word (string) which is the upper-cased keyword, or empty for a quoted name.
//
// Returns bool which is true when the engine refuses the name.
func (r ReservedNames) RefusesColumn(word string) bool {
	return word != "" && r.columns[word]
}

// RefusesColumnDefinition reports whether the engine refuses a keyword as the name of a
// column a table body defines. A word that opens a table constraint, followed by its
// parenthesis, is left to the constraint parser, which reports a malformed constraint
// better.
//
// Takes word (string) which is the upper-cased keyword, or empty for a quoted name.
// Takes parenthesisFollows (bool) which is true when an opening parenthesis comes next.
//
// Returns bool which is true when the engine refuses the name.
func (r ReservedNames) RefusesColumnDefinition(word string, parenthesisFollows bool) bool {
	if !r.RefusesColumn(word) {
		return false
	}
	return !parenthesisFollows || !startsTableConstraint(word)
}

// RefusesTable reports whether the engine refuses a keyword as a table's name.
//
// Takes word (string) which is the upper-cased keyword, or empty for a quoted name.
//
// Returns bool which is true when the engine refuses the name.
func (r ReservedNames) RefusesTable(word string) bool {
	return word != "" && r.tables[word]
}

// ReservedNameMessage explains a refused name.
//
// Takes word (string) which is the keyword.
// Takes kind (string) which names what the definition names, such as column or table.
//
// Returns string which is the message.
func ReservedNameMessage(word, kind string) string {
	return "this engine reserves " + word + "; quote it to name a " + kind + " with it"
}

// startsTableConstraint reports whether a word opens a table constraint rather than
// naming a column.
//
// Takes word (string) which is the upper-cased word.
//
// Returns bool which is true for such a word.
func startsTableConstraint(word string) bool {
	return tableConstraintStarts[word]
}
