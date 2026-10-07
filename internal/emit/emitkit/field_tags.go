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

package emitkit

import (
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/structtag"
)

const (
	// omitEmptyOption leaves out values encoding/json considers empty.
	omitEmptyOption = ",omitempty"

	// tagSeparator separates one tag of a field from the next.
	tagSeparator = " "
)

// FieldTags builds tags for generated column fields, with column tags taking precedence
// over style tags. The default is a json tag alone; {name} expands to the safe, unique
// column name before case styling.
//
// Takes style (*emit.Style) which holds the json tag settings and the project-wide struct
// tags. FieldTags does not change it.
// Takes names ([]string) which are the names of the struct's columns, in field order.
// Takes columnTags ([]map[string]string) which holds, parallel to names, each column's
// own struct tags, nil for a column with none; it may be shorter than names, or nil.
//
// Returns []string which holds, parallel to names, each field's tag without its back
// quotes, or empty for a field that carries none.
func FieldTags(style *emit.Style, names []string, columnTags []map[string]string) []string {
	baseNames := goast.JSONTagNames(names)
	jsonNames := casedJSONNames(names, style.JSONTags.Case, baseNames)
	tags := make([]string, len(names))
	for index := range names {
		values := make(map[string]string, len(style.StructTags)+1)
		if !style.JSONTags.Omit {
			values[structtag.JSONKey] = jsonNames[index]
			if style.JSONTags.OmitEmpty {
				values[structtag.JSONKey] += omitEmptyOption
			}
		}
		maps.Copy(values, style.StructTags)
		if index < len(columnTags) {
			maps.Copy(values, columnTags[index])
		}
		tags[index] = renderTags(values, baseNames[index])
	}
	return tags
}

// renderTags writes a field's tags: json first, then the others by key, each value's
// {name} replaced by the field's name.
//
// Takes values (map[string]string) which maps each key to its value.
// Takes name (string) which replaces {name}.
//
// Returns string which is the tag, or empty when there is none.
func renderTags(values map[string]string, name string) string {
	keys := slices.Sorted(maps.Keys(values))
	if position := slices.Index(keys, structtag.JSONKey); position > 0 {
		keys = slices.Concat([]string{structtag.JSONKey}, slices.Delete(keys, position, position+1))
	}
	rendered := make([]string, 0, len(keys))
	for _, key := range keys {
		rendered = append(rendered, structtag.Render(key, strings.ReplaceAll(values[key], structtag.Placeholder, name)))
	}
	return strings.Join(rendered, tagSeparator)
}

// casedJSONNames returns the names json tags carry in a case style: the base names as
// they are for names as written, and otherwise each name's words in the style, made safe
// for a tag and unique in the struct.
//
// Takes names ([]string) which are the columns' names.
// Takes style (emit.JSONCase) which is the case style.
// Takes baseNames ([]string) which are the names made safe and unique, as written.
//
// Returns []string which holds the json names, parallel to names.
func casedJSONNames(names []string, style emit.JSONCase, baseNames []string) []string {
	if style == emit.JSONCaseAsWritten {
		return baseNames
	}
	cased := make([]string, len(names))
	for index, name := range names {
		cased[index] = goastutil.SanitiseJSONTagName(caseWords(nameWords(name), style))
	}
	return goast.DisambiguateNames(cased)
}

// caseWords joins a name's words in a case style.
//
// Takes words ([]string) which are the name's words.
// Takes style (emit.JSONCase) which is snake, camel or pascal case.
//
// Returns string which is the joined name.
func caseWords(words []string, style emit.JSONCase) string {
	if style == emit.JSONCaseSnake {
		lowered := make([]string, len(words))
		for index, word := range words {
			lowered[index] = strings.ToLower(word)
		}
		return strings.Join(lowered, snakeSeparator)
	}
	var builder strings.Builder
	for index, word := range words {
		lowered := strings.ToLower(word)
		if index == 0 && style == emit.JSONCaseCamel {
			builder.WriteString(lowered)
			continue
		}
		first, width := utf8.DecodeRuneInString(lowered)
		_, _ = builder.WriteRune(unicode.ToUpper(first))
		builder.WriteString(lowered[width:])
	}
	return builder.String()
}

// nameWords splits column names at separators and case changes, preserving initialisms.
// For example, createdAt and created_at yield created and at, while HTTPStatus yields
// HTTP and Status.
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
