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

package fuzzbuilder

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	// operatorEqual is the equality operator.
	operatorEqual = "="

	// operatorLess is the less-than operator.
	operatorLess = "<"

	// operatorGreater is the greater-than operator.
	operatorGreater = ">"

	// operatorAtMost is the less-than-or-equal operator.
	operatorAtMost = "<="

	// operatorAtLeast is the greater-than-or-equal operator.
	operatorAtLeast = ">="

	// operatorLike is the pattern-matching operator.
	operatorLike = "LIKE"

	// operatorIn is the list membership operator.
	operatorIn = "IN"

	// operatorNotIn is the list exclusion operator.
	operatorNotIn = "NOT IN"

	// operatorIsNull tests for NULL.
	operatorIsNull = "IS NULL"

	// operatorIsNotNull tests for a value.
	operatorIsNotNull = "IS NOT NULL"

	// missingColumn names no catalogue column.
	missingColumn = "missing_column"

	// columnVariants is the number of refused spellings offered besides the columns.
	columnVariants = 4

	// integerChoices is the number of integer filter choices, the last meaning nil.
	integerChoices = 17

	// integerLow is the smallest integer filter value.
	integerLow = 2

	// listChoices is the number of list shapes: lengths zero to three and one scalar.
	listChoices = 5

	// directionChoices is the number of direction spellings before case variants.
	directionChoices = 8

	// caseVariants is the number of letter-case spellings of a direction.
	caseVariants = 3

	// caseLower spells a direction in lower case.
	caseLower = 1

	// caseTitle spells a direction with each word capitalised.
	caseTitle = 2

	// variantFlipped is the column variant whose letter case is swapped.
	variantFlipped = 0

	// variantMissing is the column variant that names no column.
	variantMissing = 1

	// variantQualified is the column variant qualified by the table name.
	variantQualified = 2

	// refusedSpellings is the number of refused entries that end operators and directions.
	refusedSpellings = 2

	// nullsFirstSuffix ends a direction that places NULL first.
	nullsFirstSuffix = "NULLS FIRST"
)

// operators lists the documented operators followed by two the allow-list refuses.
var operators = []string{operatorEqual, "!=", "<>", operatorLess, operatorGreater, operatorAtMost, operatorAtLeast, operatorLike, operatorIsNull, operatorIsNotNull, operatorIn, operatorNotIn,
	"like", "=="}

// likeValues lists the labels followed by the patterns, which LIKE conditions draw from.
var likeValues = append(append([]string(nil), labels...), patterns...)

// directions lists the documented directions followed by two refused spellings.
var directions = []string{"ASC", "DESC", "ASC NULLS FIRST", "ASC NULLS LAST", "DESC NULLS FIRST", "DESC NULLS LAST", "", "SIDEWAYS"}

// String describes a call for failure messages.
//
// Returns string which spells the call in Go syntax.
func (s Step) String() string {
	slot := fmt.Sprintf("b%d.", s.Slot)
	switch s.Op {
	case OperationNew:
		return fmt.Sprintf("b%d = %s(%d)", s.Slot, QueryName(s.Query), s.Parameter)
	case OperationWhere:
		return fmt.Sprintf("%sWhere(%q, %q, %#v)", slot, s.Column, s.Operator, s.Value)
	case OperationOrder:
		return fmt.Sprintf("%sOrderBy(%q, %q)", slot, s.Column, s.Direction)
	case OperationLimit:
		return fmt.Sprintf("%sLimit(%d)", slot, s.Number)
	case OperationOffset:
		return fmt.Sprintf("%sOffset(%d)", slot, s.Number)
	case OperationAll:
		return slot + "All()"
	case OperationOne:
		return slot + "One()"
	default:
		return slot + "Count()"
	}
}

// chooseColumn picks a column name or a refused spelling of one.
//
// Takes entry (*query) which is the slot's current query.
// Takes choice (byte) which selects the spelling.
//
// Returns string which is the name the call passes.
// Returns bool which reports a textual value, so conditions bind text.
func chooseColumn(entry *query, choice byte) (string, bool) {
	count := len(entry.columns)
	selected := int(choice) % (count + columnVariants)
	if selected < count {
		return entry.columns[selected].name, entry.columns[selected].text
	}
	other := &entry.columns[int(choice)/(count+columnVariants)%count]
	switch selected - count {
	case variantFlipped:
		return flipCase(other.name), other.text
	case variantMissing:
		return missingColumn, false
	case variantQualified:
		return builderTable + "." + other.name, other.text
	default:
		for index := range entry.columns {
			if entry.columns[index].source != "" {
				return entry.columns[index].source, entry.columns[index].text
			}
		}
		return missingColumn, false
	}
}

// flipCase swaps the letter case of every letter.
//
// Takes name (string) which is a column name.
//
// Returns string which differs from name in every letter's case.
func flipCase(name string) string {
	return strings.Map(func(letter rune) rune {
		if unicode.IsUpper(letter) {
			return unicode.ToLower(letter)
		}
		return unicode.ToUpper(letter)
	}, name)
}

// chooseValue picks a condition value of the column's kind.
//
// Takes operator (string) which selects a scalar or list value.
// Takes text (bool) which selects text or integer values.
// Takes choices ([]byte) which hold three value choices.
//
// Returns any which is nil, int64, string, []int64 or []string.
func chooseValue(operator string, text bool, choices []byte) any {
	switch operator {
	case operatorIsNull, operatorIsNotNull:
		return nil
	case operatorIn, operatorNotIn:
		length := int(choices[0]) % listChoices
		if length == listChoices-1 {
			return scalarValue(operator, text, choices[1])
		}
		if text {
			list := make([]string, 0, length)
			for index := range length {
				list = append(list, labels[int(choices[(index+1)%len(choices)])%len(labels)])
			}
			return list
		}
		list := make([]int64, 0, length)
		for index := range length {
			list = append(list, int64(choices[(index+1)%len(choices)]%(integerChoices-1))-integerLow)
		}
		return list
	default:
		return scalarValue(operator, text, choices[0])
	}
}

// scalarValue picks one condition value.
//
// Takes operator (string) which selects patterns for LIKE.
// Takes text (bool) which selects text or integer values.
// Takes choice (byte) which selects the value.
//
// Returns any which is nil, int64 or string.
func scalarValue(operator string, text bool, choice byte) any {
	if text {
		if operator == operatorLike {
			return likeValues[int(choice)%len(likeValues)]
		}
		return labels[int(choice)%len(labels)]
	}
	number := int(choice) % integerChoices
	if number == integerChoices-1 {
		return nil
	}
	return int64(number - integerLow)
}

// chooseDirection picks a direction spelling in one of three letter cases.
//
// Takes choice (byte) which selects the direction and its case.
//
// Returns string which is the direction the call passes.
func chooseDirection(choice byte) string {
	direction := directions[int(choice)%directionChoices]
	switch int(choice) / directionChoices % caseVariants {
	case caseLower:
		return strings.ToLower(direction)
	case caseTitle:
		words := strings.Fields(strings.ToLower(direction))
		for index, word := range words {
			words[index] = strings.ToUpper(word[:1]) + word[1:]
		}
		return strings.Join(words, " ")
	default:
		return direction
	}
}
