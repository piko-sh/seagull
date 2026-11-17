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

package source

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// sglArrayElement lists the Go types of the elements an array's text form is read into.
type sglArrayElement interface {
	string | *string | int64 | *int64 | int32 | *int32 | int16 | *int16 | int | *int |
		float64 | *float64 | float32 | *float32 | bool | *bool
}

// sglArrayItem is one element of an array's text form. It holds the unquoted, unescaped
// text, or marks a NULL.
type sglArrayItem struct {
	// text is the element's text.
	text string

	// null is true for an unquoted NULL.
	null bool
}

const (
	// sglArrayWideBits is the width of an int64 or a float64.
	sglArrayWideBits = 64

	// sglArrayNarrowBits is the width of a float32.
	sglArrayNarrowBits = 32
)

// errSglArrayText reports text that is not a one-dimensional array's text form.
var errSglArrayText = errors.New("sgl: not the text form of a one-dimensional array")

// sglArrayScanner reads the text form of a one-dimensional PostgreSQL array, as
// PostgreSQL and CockroachDB write it, into a slice, without the column being converted
// to JSON and decoded. It holds only a pointer, so passing it as an any allocates
// nothing.
type sglArrayScanner[E sglArrayElement] struct {
	// dest is the slice the array is read into.
	dest *[]E
}

// sglScanArray returns the scanner reading an array column's text form into a slice.
//
// Takes dest (*[]E) which is the slice.
//
// Returns sglArrayScanner[E] which reads into it.
func sglScanArray[E sglArrayElement](dest *[]E) sglArrayScanner[E] {
	return sglArrayScanner[E]{dest: dest}
}

// Scan implements sql.Scanner for an array column's text form.
//
// A NULL column reads as a nil slice and an empty array as an empty one. An array's lower
// bounds are dropped, a NULL element reads as a nil pointer, and is refused by an element
// type that is not a pointer, and an array of more than one dimension is refused, as
// reading its JSON would. The slice is left nil when the array cannot be read.
//
// Takes source (any) which is the driver value, as text or bytes.
//
// Returns error when the value is not an array's text form or an element cannot be read
// into E.
func (scanner sglArrayScanner[E]) Scan(source any) error {
	var text string
	switch value := source.(type) {
	case nil:
		*scanner.dest = nil
		return nil
	case string:
		text = value
	case []byte:
		text = string(value)
	default:
		*scanner.dest = nil
		return fmt.Errorf("sgl: read array column: cannot read %T as an array's text", source)
	}
	items, err := sglArrayItems(text)
	if err == nil {
		err = sglFillArray(scanner.dest, items)
	}
	if err != nil {
		*scanner.dest = nil
		return fmt.Errorf("sgl: read array column: %w", err)
	}
	return nil
}

// sglArrayItems splits an array's text form into its elements. An unquoted element keeps
// its text, an unquoted NULL is NULL, and a quoted one loses its quotes and escapes.
//
// Takes text (string) which is the array's text form, such as {a,"b c",NULL}, after any
// [lower:upper]= bounds.
//
// Returns []sglArrayItem which holds the elements in order.
// Returns error when the text is not a one-dimensional array's text form.
func sglArrayItems(text string) ([]sglArrayItem, error) {
	if strings.HasPrefix(text, "[") {
		_, after, found := strings.Cut(text, "=")
		if !found {
			return nil, errSglArrayText
		}
		text = after
	}
	if len(text) < 2 || text[0] != '{' || text[len(text)-1] != '}' {
		return nil, errSglArrayText
	}
	body := text[1 : len(text)-1]
	if strings.TrimSpace(body) == "" {
		return []sglArrayItem{}, nil
	}
	items := make([]sglArrayItem, 0, strings.Count(body, ",")+1)
	for position := 0; ; {
		item, next, err := sglArrayNextItem(body, position)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if next == len(body) {
			return items, nil
		}
		position = next + 1
	}
}

// sglArrayNextItem reads the element of an array's text form that starts at a position,
// with any space around it.
//
// Takes body (string) which is the text between the array's braces.
// Takes position (int) which is where the element starts.
//
// Returns sglArrayItem which is the element.
// Returns int which is the position of the comma after it, or len(body) for the last.
// Returns error when the element is malformed or is itself an array.
func sglArrayNextItem(body string, position int) (sglArrayItem, int, error) {
	for position < len(body) && sglArraySpace(body[position]) {
		position++
	}
	if position < len(body) && body[position] == '"' {
		return sglArrayQuotedItem(body, position+1)
	}
	end := position
	for end < len(body) && body[end] != ',' {
		if body[end] == '{' || body[end] == '}' || body[end] == '"' || body[end] == '\\' {
			return sglArrayItem{}, 0, errSglArrayText
		}
		end++
	}
	last := end
	for last > position && sglArraySpace(body[last-1]) {
		last--
	}
	element := body[position:last]
	if element == "" {
		return sglArrayItem{}, 0, errSglArrayText
	}
	return sglArrayItem{text: element, null: strings.EqualFold(element, "NULL")}, end, nil
}

// sglArrayQuotedItem reads a quoted element of an array's text form.
//
// Takes body (string) which is the text between the array's braces.
// Takes position (int) which is just after the element's opening quote.
//
// Returns sglArrayItem which is the element, unescaped.
// Returns int which is the position of the comma after it, or len(body) for the last.
// Returns error when the quote is not closed or other text follows it.
func sglArrayQuotedItem(body string, position int) (sglArrayItem, int, error) {
	start := position
	var unescaped *strings.Builder
	for position < len(body) && body[position] != '"' {
		if body[position] == '\\' {
			if unescaped == nil {
				unescaped = &strings.Builder{}
			}
			unescaped.WriteString(body[start:position])
			position++
			if position == len(body) {
				return sglArrayItem{}, 0, errSglArrayText
			}
			start = position
		}
		position++
	}
	if position == len(body) {
		return sglArrayItem{}, 0, errSglArrayText
	}
	element := body[start:position]
	if unescaped != nil {
		unescaped.WriteString(element)
		element = unescaped.String()
	}
	next, err := sglArrayAfterItem(body, position+1)
	return sglArrayItem{text: element, null: false}, next, err
}

// sglArrayAfterItem skips the space after a quoted element of an array's text form.
//
// Takes body (string) which is the text between the array's braces.
// Takes position (int) which is just after the element's closing quote.
//
// Returns int which is the position of the comma after it, or len(body) for the last.
// Returns error when other text follows the element.
func sglArrayAfterItem(body string, position int) (int, error) {
	for position < len(body) && sglArraySpace(body[position]) {
		position++
	}
	if position < len(body) && body[position] != ',' {
		return 0, errSglArrayText
	}
	return position, nil
}

// sglArraySpace reports whether a byte is space PostgreSQL skips around an array's
// elements.
//
// Takes character (byte) which is the byte.
//
// Returns bool which is true for a space, tab, newline, carriage return, vertical tab or
// form feed.
func sglArraySpace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

// sglFillArray stores an array's elements in a slice of E, reading each from its text.
// The values a slice of pointers points to share one backing array.
//
// Takes dest (*[]E) which is the slice.
// Takes items ([]sglArrayItem) which holds the elements.
//
// Returns error when an element is NULL and E is not a pointer, or its text is not a
// value of E.
func sglFillArray[E sglArrayElement](dest *[]E, items []sglArrayItem) error {
	switch typed := any(dest).(type) {
	case *[]string:
		return sglFillArrayValues(typed, items, sglArrayText)
	case *[]*string:
		return sglFillArrayPointers(typed, items, sglArrayText)
	case *[]int64:
		return sglFillArrayValues(typed, items, sglArrayInteger[int64])
	case *[]*int64:
		return sglFillArrayPointers(typed, items, sglArrayInteger[int64])
	case *[]int32:
		return sglFillArrayValues(typed, items, sglArrayInteger[int32])
	case *[]*int32:
		return sglFillArrayPointers(typed, items, sglArrayInteger[int32])
	case *[]int16:
		return sglFillArrayValues(typed, items, sglArrayInteger[int16])
	case *[]*int16:
		return sglFillArrayPointers(typed, items, sglArrayInteger[int16])
	case *[]int:
		return sglFillArrayValues(typed, items, sglArrayInteger[int])
	case *[]*int:
		return sglFillArrayPointers(typed, items, sglArrayInteger[int])
	case *[]float64:
		return sglFillArrayValues(typed, items, sglArrayFloat[float64])
	case *[]*float64:
		return sglFillArrayPointers(typed, items, sglArrayFloat[float64])
	case *[]float32:
		return sglFillArrayValues(typed, items, sglArrayFloat[float32])
	case *[]*float32:
		return sglFillArrayPointers(typed, items, sglArrayFloat[float32])
	case *[]bool:
		return sglFillArrayValues(typed, items, sglArrayBool)
	case *[]*bool:
		return sglFillArrayPointers(typed, items, sglArrayBool)
	default:
		return fmt.Errorf("cannot read an array into %T", *dest)
	}
}

// sglFillArrayValues stores an array's elements in a slice of values, refusing a NULL.
//
// Takes dest (*[]T) which is the slice.
// Takes items ([]sglArrayItem) which holds the elements.
// Takes parse (func(string) (T, error)) which reads an element's text.
//
// Returns error when an element is NULL or cannot be read.
func sglFillArrayValues[T any](dest *[]T, items []sglArrayItem, parse func(string) (T, error)) error {
	values := make([]T, len(items))
	for index, item := range items {
		if item.null {
			return fmt.Errorf("element %d: converting NULL to %T is unsupported", index, values[index])
		}
		value, err := parse(item.text)
		if err != nil {
			return fmt.Errorf("element %d: %w", index, err)
		}
		values[index] = value
	}
	*dest = values
	return nil
}

// sglFillArrayPointers stores an array's elements in a slice of pointers, a NULL as nil.
//
// Takes dest (*[]*T) which is the slice.
// Takes items ([]sglArrayItem) which holds the elements.
// Takes parse (func(string) (T, error)) which reads an element's text.
//
// Returns error when an element cannot be read.
func sglFillArrayPointers[T any](dest *[]*T, items []sglArrayItem, parse func(string) (T, error)) error {
	pointers := make([]*T, len(items))
	values := make([]T, len(items))
	for index, item := range items {
		if item.null {
			continue
		}
		value, err := parse(item.text)
		if err != nil {
			return fmt.Errorf("element %d: %w", index, err)
		}
		values[index] = value
		pointers[index] = &values[index]
	}
	*dest = pointers
	return nil
}

// sglArrayText reads a text element, which is its text.
//
// Takes text (string) which is the element's text.
//
// Returns string which is the text.
// Returns error which is always nil.
func sglArrayText(text string) (string, error) {
	return text, nil
}

// sglArrayInteger reads an integer element, refusing one T cannot hold.
//
// Takes text (string) which is the element's text.
//
// Returns T which is the integer.
// Returns error when the text is not an integer T holds.
func sglArrayInteger[T int64 | int32 | int16 | int](text string) (T, error) {
	value, err := strconv.ParseInt(text, 10, sglArrayWideBits)
	if err != nil {
		return 0, err
	}
	if converted := T(value); int64(converted) == value {
		return converted, nil
	}
	return 0, &strconv.NumError{Func: "ParseInt", Num: text, Err: strconv.ErrRange}
}

// sglArrayFloat reads a floating-point element, including NaN, Infinity and -Infinity.
//
// Takes text (string) which is the element's text.
//
// Returns T which is the number.
// Returns error when the text is not a number T holds.
func sglArrayFloat[T float64 | float32](text string) (T, error) {
	var zero T
	bits := sglArrayWideBits
	if _, single := any(zero).(float32); single {
		bits = sglArrayNarrowBits
	}
	value, err := strconv.ParseFloat(text, bits)
	return T(value), err
}

// sglArrayBool reads a boolean element, written t or f.
//
// Takes text (string) which is the element's text.
//
// Returns bool which is the boolean.
// Returns error when the text is neither t nor f.
func sglArrayBool(text string) (bool, error) {
	switch text {
	case "t":
		return true, nil
	case "f":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q", text)
	}
}
