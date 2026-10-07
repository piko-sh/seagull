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

package session

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"piko.sh/goastutil"

	"seagull.sh/seagull"
)

const (
	// snakeSeparator joins the words of a name in snake case.
	snakeSeparator = "_"
)

// jsonTagName returns the name a generated field's json tag gives a column, as the
// emitters write it: the column's name as written, or its words in snake, camel or pascal
// case, made safe for a tag.
//
// Takes column (string) which is the column's name.
// Takes style (seagull.JSONCase) which is go.json_tags.case.
//
// Returns string which is the name the tag carries.
func jsonTagName(column string, style seagull.JSONCase) string {
	if style == seagull.JSONCaseAsWritten {
		return goastutil.SanitiseJSONTagName(column)
	}
	return goastutil.SanitiseJSONTagName(caseWords(nameWords(column), style))
}

// caseWords joins a name's words in a case style.
//
// Takes words ([]string) which are the name's words.
// Takes style (seagull.JSONCase) which is snake, camel or pascal case.
//
// Returns string which is the joined name.
func caseWords(words []string, style seagull.JSONCase) string {
	if style == seagull.JSONCaseSnake {
		lowered := make([]string, len(words))
		for index, word := range words {
			lowered[index] = strings.ToLower(word)
		}
		return strings.Join(lowered, snakeSeparator)
	}
	var builder strings.Builder
	for index, word := range words {
		lowered := strings.ToLower(word)
		if index == 0 && style == seagull.JSONCaseCamel {
			builder.WriteString(lowered)
			continue
		}
		first, width := utf8.DecodeRuneInString(lowered)
		_, _ = builder.WriteRune(unicode.ToUpper(first))
		builder.WriteString(lowered[width:])
	}
	return builder.String()
}

// nameWords splits a column's name at separators and case changes, keeping initialisms
// whole: createdAt and created_at give created and at, and HTTPStatus gives HTTP and
// Status.
//
// Takes name (string) which is the column's name.
//
// Returns []string which holds the words, in order.
func nameWords(name string) []string {
	var words []string
	var current []rune
	runes := []rune(name)
	for index, character := range runes {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			words = appendWord(words, current)
			current = nil
			continue
		}
		if len(current) > 0 && unicode.IsUpper(character) && startsWord(runes, index) {
			words = appendWord(words, current)
			current = nil
		}
		current = append(current, character)
	}
	return appendWord(words, current)
}

// startsWord reports whether the upper-case letter at an index starts a word: after a
// lower-case letter or a digit, or as the last capital of a run followed by a lower-case
// letter.
//
// Takes runes ([]rune) which hold the name.
// Takes index (int) which is the letter's index, after the first.
//
// Returns bool which is true when a word starts there.
func startsWord(runes []rune, index int) bool {
	previous := runes[index-1]
	if unicode.IsLower(previous) || unicode.IsDigit(previous) {
		return true
	}
	return unicode.IsUpper(previous) && index+1 < len(runes) && unicode.IsLower(runes[index+1])
}

// appendWord adds a word to a list when it holds any character.
//
// Takes words ([]string) which holds the words so far.
// Takes word ([]rune) which is the word.
//
// Returns []string which holds the words.
func appendWord(words []string, word []rune) []string {
	if len(word) == 0 {
		return words
	}
	return append(words, string(word))
}
