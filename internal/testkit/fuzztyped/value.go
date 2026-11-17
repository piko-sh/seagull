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

package fuzztyped

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// KindNull is SQL NULL.
	KindNull = "null"

	// KindInteger is an exact integer held in Text.
	KindInteger = "integer"

	// KindFloat is a binary64 value held in Bits.
	KindFloat = "float"

	// KindText is text held in Text.
	KindText = "text"

	// KindDecimal is an exact decimal held in Text.
	KindDecimal = "decimal"

	// KindTime is a wall-clock timestamp held in Text without a zone.
	KindTime = "time"

	// KindInstant is an instant held in Text as UTC RFC 3339 with nanoseconds.
	KindInstant = "instant"

	// KindBoolean is a truth value held in Text as true or false.
	KindBoolean = "boolean"

	// KindList is an ordered collection held in Items.
	KindList = "list"

	// textDescriptionBound is the number of characters failure messages show of long text.
	textDescriptionBound = 40

	// decimalBase is the base of integer spellings.
	decimalBase = 10

	// hexBase is the base of float bit patterns in failure messages.
	hexBase = 16

	// floatBits is the width of the modelled floats.
	floatBits = 64
)

// Value is one typed value: a scalar, NULL or a list of values.
type Value struct {
	// Kind identifies the value family.
	Kind string

	// Text holds the scalar's exact spelling.
	Text string

	// Items holds list members.
	Items []Value

	// Bits holds a float's binary64 representation.
	Bits uint64
}

// Equal compares two values exactly and by kind.
//
// Floats compare by bits, so negative zero differs from zero and every NaN payload is
// distinct. Decimals compare numerically, so 1.50 equals 1.5.
//
// Takes other (Value) which is the value to compare with.
//
// Returns bool which is true when the values are identical.
func (v Value) Equal(other Value) bool {
	if v.Kind != other.Kind || len(v.Items) != len(other.Items) {
		return false
	}
	switch v.Kind {
	case KindFloat:
		return v.Bits == other.Bits || (math.IsNaN(v.Float()) && math.IsNaN(other.Float()))
	case KindDecimal:
		left, leftOK := new(big.Rat).SetString(v.Text)
		right, rightOK := new(big.Rat).SetString(other.Text)
		return leftOK && rightOK && left.Cmp(right) == 0
	case KindList:
		for index := range v.Items {
			if !v.Items[index].Equal(other.Items[index]) {
				return false
			}
		}
		return true
	default:
		return v.Text == other.Text
	}
}

// Float reads a float value.
//
// Returns float64 which is the value its bits hold.
func (v Value) Float() float64 {
	return math.Float64frombits(v.Bits)
}

// String describes a value for failure messages.
//
// Returns string which distinguishes kinds and keeps every float bit.
func (v Value) String() string {
	switch v.Kind {
	case KindNull:
		return "NULL"
	case KindFloat:
		return strconv.FormatFloat(v.Float(), 'g', -1, floatBits) + "(0x" + strconv.FormatUint(v.Bits, hexBase) + ")"
	case KindList:
		items := make([]string, 0, len(v.Items))
		for _, item := range v.Items {
			items = append(items, item.String())
		}
		return "[" + strings.Join(items, ", ") + "]"
	case KindText:
		return describeText(v.Text)
	default:
		return v.Kind + ":" + v.Text
	}
}

// Null returns SQL NULL.
//
// Returns Value which is NULL.
func Null() Value {
	return Value{Kind: KindNull, Text: "", Items: nil, Bits: 0}
}

// Integer returns an exact integer.
//
// Takes number (int64) which is the integer.
//
// Returns Value which holds it.
func Integer(number int64) Value {
	return Value{Kind: KindInteger, Text: strconv.FormatInt(number, decimalBase), Items: nil, Bits: 0}
}

// Float returns a binary64 value.
//
// Takes number (float64) which is the value, including negative zero and NaN.
//
// Returns Value which holds its exact bits.
func Float(number float64) Value {
	return Value{Kind: KindFloat, Text: "", Items: nil, Bits: math.Float64bits(number)}
}

// Text returns a text value.
//
// Takes text (string) which is the text.
//
// Returns Value which holds it.
func Text(text string) Value {
	return Value{Kind: KindText, Text: text, Items: nil, Bits: 0}
}

// Boolean returns a truth value.
//
// Takes truth (bool) which is the value.
//
// Returns Value which holds it.
func Boolean(truth bool) Value {
	return Value{Kind: KindBoolean, Text: strconv.FormatBool(truth), Items: nil, Bits: 0}
}

// Scalar returns a value of a kind held in Text.
//
// Takes kind (string) which is KindDecimal, KindTime or KindInstant.
// Takes text (string) which is the exact spelling.
//
// Returns Value which holds it.
func Scalar(kind, text string) Value {
	return Value{Kind: kind, Text: text, Items: nil, Bits: 0}
}

// List returns an ordered collection.
//
// Takes items ([]Value) which are the members.
//
// Returns Value which holds them, empty but not NULL when items is empty.
func List(items ...Value) Value {
	return Value{Kind: KindList, Text: "", Items: append([]Value{}, items...), Bits: 0}
}

// describeText quotes text for failure messages, cutting long text at a character
// boundary and saying how much was left out.
//
// Takes text (string) which is the value.
//
// Returns string which is the quoted text, shortened when long.
func describeText(text string) string {
	count := 0
	for index := range text {
		if count == textDescriptionBound {
			return strconv.Quote(text[:index]) + "...(" + strconv.Itoa(utf8.RuneCountInString(text)) + " characters)"
		}
		count++
	}
	return strconv.Quote(text)
}
