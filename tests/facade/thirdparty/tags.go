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

package thirdparty

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"seagull.sh/seagull/sdk/emit"
)

const (
	// jsonKey is the key of the json tag.
	jsonKey = "json"

	// namePlaceholder stands, in a struct tag's value, for the name the field's json tag is
	// made from.
	namePlaceholder = "{name}"

	// omitEmpty leaves out values encoding/json considers empty.
	omitEmpty = ",omitempty"

	// jsonPunctuation lists the punctuation encoding/json accepts in a tag's name.
	jsonPunctuation = "!#$%&()*+-./:<=>?@[]^_{|}~ "

	// fallbackJSONName is the json name of a column whose name leaves nothing usable.
	fallbackJSONName = "field"

	// snakeCase writes json names in snake case, such as created_at.
	snakeCase = "snake"

	// camelCase writes json names in lower camel case, such as createdAt.
	camelCase = "camel"
)

// fieldTags writes the struct tags of the fields holding columns: a json tag as the style
// shapes it, then the style's own struct tags and each column's, a column's taking
// precedence. {name} in a value stands for the column's name made safe for a tag and
// unique in the struct.
//
// Takes style (*emit.Style) which holds the json tag settings and the project's struct
// tags. fieldTags does not change it.
// Takes names ([]string) which are the columns' names, in field order.
// Takes columnTags ([]map[string]string) which holds, parallel to names, each column's
// own struct tags, or nil for a column with none; it may be shorter than names, or nil.
//
// Returns []string which holds, parallel to names, each field's tag without its back
// quotes, or empty for a field with none.
func fieldTags(style *emit.Style, names []string, columnTags []map[string]string) []string {
	safeNames := make([]string, len(names))
	for index, name := range names {
		safeNames[index] = safeJSONName(name)
	}
	safeNames = uniqueNames(safeNames)
	jsonNames := safeNames
	if style.JSONTags.Case != "" {
		jsonNames = make([]string, len(names))
		for index, name := range names {
			jsonNames[index] = safeJSONName(casedName(nameWords(name), string(style.JSONTags.Case)))
		}
		jsonNames = uniqueNames(jsonNames)
	}
	tags := make([]string, len(names))
	for index := range names {
		values := make(map[string]string, len(style.StructTags)+1)
		if !style.JSONTags.Omit {
			values[jsonKey] = jsonNames[index]
			if style.JSONTags.OmitEmpty {
				values[jsonKey] += omitEmpty
			}
		}
		maps.Copy(values, style.StructTags)
		if index < len(columnTags) {
			maps.Copy(values, columnTags[index])
		}
		tags[index] = tagSource(values, safeNames[index])
	}
	return tags
}

// tagSource writes a field's tags: json first, then the others by key, {name} in each
// value replaced by the field's name.
//
// Takes values (map[string]string) which maps each key to its value.
// Takes name (string) which replaces {name}.
//
// Returns string which is the tag, or empty when there is none.
func tagSource(values map[string]string, name string) string {
	keys := slices.SortedFunc(maps.Keys(values), func(left, right string) int {
		switch {
		case left == right:
			return 0
		case left == jsonKey:
			return -1
		case right == jsonKey:
			return 1
		default:
			return strings.Compare(left, right)
		}
	})
	written := make([]string, 0, len(keys))
	for _, key := range keys {
		written = append(written, key+":"+strconv.Quote(strings.ReplaceAll(values[key], namePlaceholder, name)))
	}
	return strings.Join(written, " ")
}

// safeJSONName makes a name usable in a json tag: each rune encoding/json does not accept
// there becomes an underscore, and a name left empty, or one json reads as leaving the
// field out, becomes field.
//
// Takes name (string) which is the name.
//
// Returns string which is the safe name.
func safeJSONName(name string) string {
	safe := strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune(jsonPunctuation, character) {
			return character
		}
		return '_'
	}, name)
	if safe == "" || safe == "-" {
		return fallbackJSONName
	}
	return safe
}

// uniqueNames makes names unique in order: the first of each name stays as it is, and a
// later one takes the first of name2, name3 and so on not yet taken.
//
// Takes names ([]string) which are the names, in order.
//
// Returns []string which holds the unique names, in the same order.
func uniqueNames(names []string) []string {
	taken := make(map[string]bool, len(names))
	next := make(map[string]int)
	unique := make([]string, len(names))
	for index, name := range names {
		candidate := name
		number := max(next[name], firstNumber)
		for taken[candidate] {
			candidate = name + strconv.Itoa(number)
			number++
		}
		if candidate != name {
			next[name] = number
		}
		taken[candidate] = true
		unique[index] = candidate
	}
	return unique
}

// casedName joins a name's words in a case style: snake, camel, or Pascal for any other.
//
// Takes words ([]string) which are the name's words.
// Takes style (string) which is the case style.
//
// Returns string which is the joined name.
func casedName(words []string, style string) string {
	if style == snakeCase {
		lowered := make([]string, len(words))
		for index, word := range words {
			lowered[index] = strings.ToLower(word)
		}
		return strings.Join(lowered, "_")
	}
	var builder strings.Builder
	for index, word := range words {
		lowered := strings.ToLower(word)
		if index == 0 && style == camelCase {
			builder.WriteString(lowered)
			continue
		}
		first, width := utf8.DecodeRuneInString(lowered)
		_, _ = builder.WriteRune(unicode.ToUpper(first))
		builder.WriteString(lowered[width:])
	}
	return builder.String()
}

// nameWords splits a column's name into words at separators and changes of case, keeping
// initialisms whole, so createdAt and created_at give created and at, and HTTPStatus
// gives HTTP and Status.
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

// startsWord reports whether the capital at an index starts a word: after a lower-case
// letter or a digit, or as the last capital of a run before a lower-case letter.
//
// Takes runes ([]rune) which hold the name.
// Takes index (int) which is the capital's index, after the first.
//
// Returns bool which is true when a word starts there.
func startsWord(runes []rune, index int) bool {
	previous := runes[index-1]
	if unicode.IsLower(previous) || unicode.IsDigit(previous) {
		return true
	}
	return unicode.IsUpper(previous) && index+1 < len(runes) && unicode.IsLower(runes[index+1])
}

// appendWord adds a word to a list when it holds any rune.
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
