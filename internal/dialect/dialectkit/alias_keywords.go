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

import "strings"

// AliasKeywords lists the keywords an engine refuses, written unquoted, as the alias of
// an output column, after AS and written straight after the item. A quoted name is never
// refused.
type AliasKeywords struct {
	// afterAs holds the upper-cased keywords refused after AS.
	afterAs map[string]bool

	// bare holds the upper-cased keywords refused straight after the item.
	bare map[string]bool
}

// NewAliasKeywords builds the lists from whitespace-separated keywords.
//
// Takes afterAs (string) which lists the keywords refused after AS.
// Takes bare (string) which lists the keywords refused straight after the item.
//
// Returns AliasKeywords which holds the lists.
func NewAliasKeywords(afterAs, bare string) AliasKeywords {
	return AliasKeywords{afterAs: keywordSet(afterAs), bare: keywordSet(bare)}
}

// RefusesAfterAs reports whether the engine refuses a keyword as an alias written after
// AS.
//
// Takes word (string) which is the upper-cased keyword, or empty for a quoted name or a
// token that is no word.
//
// Returns bool which is true when the engine refuses the alias.
func (k AliasKeywords) RefusesAfterAs(word string) bool {
	return word != "" && k.afterAs[word]
}

// RefusesBare reports whether the engine refuses a keyword as an alias written straight
// after the item.
//
// Takes word (string) which is the upper-cased keyword, or empty for a quoted name or a
// token that is no word.
//
// Returns bool which is true when the engine refuses the alias.
func (k AliasKeywords) RefusesBare(word string) bool {
	return word != "" && k.bare[word]
}

// RefusalMessage explains a refused alias.
//
// Takes word (string) which is the keyword.
//
// Returns string which is the message.
func RefusalMessage(word string) string {
	return "this engine reserves " + word + " here; quote it to use it as an alias"
}

// keywordSet builds a set from whitespace-separated words.
//
// Takes words (string) which lists the words.
//
// Returns map[string]bool which holds each upper-cased word.
func keywordSet(words string) map[string]bool {
	fields := strings.Fields(words)
	set := make(map[string]bool, len(fields))
	for _, field := range fields {
		set[strings.ToUpper(field)] = true
	}
	return set
}
