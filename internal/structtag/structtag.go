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

package structtag

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	// Placeholder stands, in a struct tag's value, for the name the field's json tag is made
	// from: the column's name made safe for a tag and unique in its struct.
	Placeholder = "{name}"

	// JSONKey is the key of the json tag.
	JSONKey = "json"

	// keySeparator separates a tag's key from its quoted value.
	keySeparator = ':'

	// quote opens and closes a tag's value.
	quote = '"'

	// escape escapes the character after it inside a quoted value.
	escape = '\\'

	// pairSeparator separates one key:"value" pair from the next.
	pairSeparator = " "

	// backQuote closes the raw string literal a generated tag is written in, so a value
	// cannot hold one.
	backQuote = '`'
)

var (
	// errEmptyKey reports a tag with no key.
	errEmptyKey = errors.New("a struct tag key must not be empty")

	// errNoTag reports a tag text holding no key:"value" pair.
	errNoTag = errors.New("the struct tag holds no tag; write key:\"value\" pairs separated by spaces")
)

// Check validates a key and value for a generated struct tag written as a raw string
// literal. Keys must satisfy reflect.StructTag syntax; both keys and values must exclude
// back quotes and control characters.
//
// Takes key (string) which is the tag's key, such as validate.
// Takes value (string) which is the tag's value, such as required.
//
// Returns error which describes the problem, or nil.
func Check(key, value string) error {
	if key == "" {
		return errEmptyKey
	}
	for _, character := range key {
		if character == ' ' || character == quote || character == keySeparator || character == backQuote || unicode.IsControl(character) {
			return fmt.Errorf("struct tag key %q holds %q, which a key cannot hold", key, character)
		}
	}
	for _, character := range value {
		if character == backQuote {
			return fmt.Errorf("struct tag %s has the value %q, which holds a back quote the generated tag cannot hold", key, value)
		}
		if unicode.IsControl(character) {
			return fmt.Errorf("struct tag %s has the value %q, which holds a control character", key, value)
		}
	}
	return nil
}

// CheckAll checks every tag of a set with Check, in key order.
//
// Takes tags (map[string]string) which maps each key to its value, or is nil.
//
// Returns error which describes the first problem, or nil.
func CheckAll(tags map[string]string) error {
	for _, key := range slices.Sorted(maps.Keys(tags)) {
		if err := Check(key, tags[key]); err != nil {
			return err
		}
	}
	return nil
}

// Parse reads a struct tag written as Go writes one: key:"value" pairs separated by
// spaces, such as validate:"required" db:"email", each value a quoted Go string.
//
// Takes text (string) which is the tag, without back quotes.
//
// Returns map[string]string which maps each key to its value.
// Returns error when the text is not written that way, repeats a key, holds no tag, or a
// tag fails Check.
func Parse(text string) (map[string]string, error) {
	tags := make(map[string]string)
	rest := strings.TrimLeft(text, pairSeparator)
	for rest != "" {
		key, value, remainder, err := readPair(text, rest)
		if err != nil {
			return nil, err
		}
		if _, repeated := tags[key]; repeated {
			return nil, fmt.Errorf("the struct tag %q gives the key %s more than once", text, key)
		}
		if err := Check(key, value); err != nil {
			return nil, err
		}
		tags[key] = value
		if remainder != "" && !strings.HasPrefix(remainder, pairSeparator) {
			return nil, fmt.Errorf("the struct tag %q runs one pair into the next; separate key:\"value\" pairs by spaces", text)
		}
		rest = strings.TrimLeft(remainder, pairSeparator)
	}
	if len(tags) == 0 {
		return nil, errNoTag
	}
	return tags, nil
}

// Render writes one tag as a generated struct tag holds it: the key, a colon and the
// value as a quoted Go string.
//
// Takes key (string) which is the tag's key.
// Takes value (string) which is the tag's value.
//
// Returns string which is the key:"value" pair.
func Render(key, value string) string {
	return key + string(keySeparator) + strconv.Quote(value)
}

// readPair reads the key:"value" pair a tag's text starts with.
//
// Takes text (string) which is the whole tag, for messages.
// Takes rest (string) which is the text from the pair on.
//
// Returns key (string) which is the key.
// Returns value (string) which is the unquoted value.
// Returns remainder (string) which is the text after the pair.
// Returns err (error) when the pair is not written key:"value" or its value is not
// closed.
func readPair(text, rest string) (key, value, remainder string, err error) {
	key, quoted, found := strings.Cut(rest, string(keySeparator))
	if !found || key == "" || !strings.HasPrefix(quoted, string(quote)) {
		return "", "", "", fmt.Errorf("the struct tag %q must be written as key:\"value\" pairs separated by spaces", text)
	}
	closing := closingQuote(quoted, 0)
	if closing < 0 {
		return "", "", "", fmt.Errorf("the struct tag %q has a value whose quote is not closed", text)
	}
	value, err = strconv.Unquote(quoted[:closing+1])
	if err != nil {
		return "", "", "", fmt.Errorf("the struct tag %q has a value that is not a quoted Go string: %w", text, err)
	}
	return key, value, quoted[closing+1:], nil
}

// closingQuote returns the index of the quote that closes the quoted value opening at an
// index, past any escaped character.
//
// Takes text (string) which holds the value.
// Takes opening (int) which is the index of the opening quote.
//
// Returns int which is the index of the closing quote, or -1 when the value is not
// closed.
func closingQuote(text string, opening int) int {
	for index := opening + 1; index < len(text); index++ {
		switch text[index] {
		case escape:
			index++
		case quote:
			return index
		}
	}
	return -1
}
