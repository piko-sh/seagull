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

package harness

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// plainDecimal matches a decimal number written without an exponent, as DECIMAL values
// are returned.
var plainDecimal = regexp.MustCompile(`^-?[0-9]+\.[0-9]+$`)

// fuzzExactResultsEqual retains storage kinds for the exact-value model.
//
// Takes want ([][]any) which contains independent reference rows.
// Takes got ([][]any) which contains generated execution rows.
// Takes ordered (bool) which selects sequence or multiset comparison.
//
// Returns bool which rejects changed values, storage kinds or multiplicity.
func fuzzExactResultsEqual(want, got [][]any, ordered bool) bool {
	expected, actual := fuzzExactSequence(want), fuzzExactSequence(got)
	if !ordered {
		slices.Sort(expected)
		slices.Sort(actual)
	}
	return slices.Equal(expected, actual)
}

// fuzzExactSequence encodes values with an independent storage discriminator.
//
// Takes rows ([][]any) which contains raw driver observations.
//
// Returns []string which preserves row boundaries and arbitrary bytes.
func fuzzExactSequence(rows [][]any) []string {
	return fuzzEncodeRows(rows, fuzzExactValue)
}

// fuzzExactValue encodes one value with its storage kind, NULL included.
//
// Takes value (any) which is a raw driver observation.
//
// Returns string which holds the storage kind and the value's bytes.
func fuzzExactValue(value any) string {
	return fuzzStorageKind(value) + ":" + base64.StdEncoding.EncodeToString([]byte(fuzzValue(value)))
}

// fuzzEncodeRows encodes each row as one string, keeping row and value boundaries.
//
// Takes rows ([][]any) which contains raw driver observations.
// Takes encode (func(any) string) which encodes one value.
//
// Returns []string which holds one unambiguous encoding per row.
func fuzzEncodeRows(rows [][]any, encode func(any) string) []string {
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		values := make([]string, len(row))
		for index, value := range row {
			values[index] = strconv.Quote(encode(value))
		}
		result = append(result, "["+strings.Join(values, ",")+"]")
	}
	return result
}

// fuzzStorageKind distinguishes SQL value classes while normalising numeric widths.
//
// Takes value (any) which is the actual driver's scalar representation.
//
// Returns string which keeps NULL, text, binary, integers and reals distinct.
func fuzzStorageKind(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return "text"
	case []byte:
		return numericBytesKind(typed)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		return "real"
	default:
		return fmt.Sprintf("%T", value)
	}
}

// numericBytesKind classifies text-encoded numbers returned as bytes by MySQL drivers.
// MySQL text protocols return numbers as text, and both protocols return DECIMAL as text.
//
// Takes value ([]byte) which is the bytes.
//
// Returns string which is integer or real for a plain number, and binary otherwise.
func numericBytesKind(value []byte) string {
	text := string(value)
	if _, err := strconv.ParseInt(text, 10, 64); err == nil {
		return "integer"
	}
	if _, err := strconv.ParseUint(text, 10, 64); err == nil {
		return "integer"
	}
	if plainDecimal.MatchString(text) {
		return "real"
	}
	return "binary"
}
