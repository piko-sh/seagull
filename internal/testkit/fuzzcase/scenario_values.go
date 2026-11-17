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

package fuzzcase

import (
	"math"
	"strconv"
)

// scenarioBooleanCount counts the Boolean boundaries, false and true.
const scenarioBooleanCount = 2

var (
	// typedSignedBoundaries retains independent representation boundaries.
	typedSignedBoundaries = []string{"-8", "0", "1", "-1", "127", "128", "32767", "32768", "2147483647", "2147483648", "9223372036854775807", "-9223372036854775808"}

	// typedUnsignedBoundaries retains independent representation boundaries.
	typedUnsignedBoundaries = []string{"8", "0", "1", "255", "256", "65535", "65536", "4294967295", "4294967296", "9223372036854775807", "9223372036854775808", "18446744073709551615"}

	// typedDecimalBoundaries retains independent representation boundaries.
	typedDecimalBoundaries = []string{"-8000000", "0", "1", "-1", "500000", "-500000", "1500000", "-1500000", "8500000", "-8500000", "9223372036854775808000000", "-9223372036854775808000000"}

	// typedFloatBoundaries retains independent representation boundaries.
	typedFloatBoundaries = []float64{-8, 0, 0.5, -0.5, 1.5, -1.5, 1.0 / 3, math.SmallestNonzeroFloat64, math.MaxFloat64}

	// typedTextBoundaries retains independent representation boundaries.
	typedTextBoundaries = []string{"", "NULL", "O'Reilly", "\\", "café", "船", "a:b;c"}

	// typedBinaryBoundaries retains independent representation boundaries.
	typedBinaryBoundaries = [][]byte{{}, {0}, {255, 0, 128}, []byte("NULL"), {39, 92}}

	// typedDateBoundaries retains independent representation boundaries.
	typedDateBoundaries = []string{"2000-02-29", "1970-01-01", "2038-01-19"}

	// typedTimestampBoundaries retains independent representation boundaries.
	typedTimestampBoundaries = []string{"2000-02-29 23:59:59.999999", "1970-01-01 00:00:00"}

	// typedTimestamptzBoundaries retains independent representation boundaries.
	typedTimestamptzBoundaries = []string{"2000-02-29T23:59:59.999999Z", "2000-02-29T23:59:59+05:30"}

	// typedUUIDBoundaries retains independent representation boundaries.
	typedUUIDBoundaries = []string{"00000000-0000-0000-0000-000000000000", "ffffffff-ffff-ffff-ffff-ffffffffffff"}

	// typedJSONBoundaries retains independent representation boundaries.
	typedJSONBoundaries = []string{"null", "{}", "[]", `{"n":9007199254740993,"empty":""}`, `[null,true,1.25]`}
)

// scenarioBoundaryCounts counts the independent boundaries of each family.
var scenarioBoundaryCounts = map[string]int{
	typedSigned: len(typedSignedBoundaries), typedUnsigned: len(typedUnsignedBoundaries), typedDecimal: len(typedDecimalBoundaries),
	typedFloat: len(typedFloatBoundaries), typedText: len(typedTextBoundaries), typedBinary: len(typedBinaryBoundaries),
	typedDate: len(typedDateBoundaries), typedTimestamp: len(typedTimestampBoundaries), typedTimestamptz: len(typedTimestamptzBoundaries),
	typedUUID: len(typedUUIDBoundaries), typedJSON: len(typedJSONBoundaries), typedBoolean: scenarioBooleanCount, typedArray: scenarioArrayCount,
}

// scenarioValue chooses exact values at useful representation boundaries.
//
// Takes kind (string) which selects the independent value family.
// Takes choice (byte) which selects a representation boundary or SQL NULL.
//
// Returns TypedValue which preserves the selected representation.
func scenarioValue(kind string, choice byte) TypedValue {
	value := TypedNumber(kind, typedZero, 0)
	if choice&scenarioNullFlag != 0 {
		value.Null = true
		return value
	}
	switch kind {
	case typedSigned:
		value.Number = typedSignedBoundaries[int(choice)%len(typedSignedBoundaries)]
	case typedUnsigned:
		value.Number = typedUnsignedBoundaries[int(choice)%len(typedUnsignedBoundaries)]
	case typedDecimal:
		value.Number, value.Scale = typedDecimalBoundaries[int(choice)%len(typedDecimalBoundaries)], scenarioDecimalScale
	case typedFloat:
		value.Bits = math.Float64bits(typedFloatBoundaries[int(choice)%len(typedFloatBoundaries)])
	case typedBoolean:
		value.Number = strconv.Itoa(int(choice % scenarioBooleanCount))
	case typedText:
		value.Text = typedTextBoundaries[int(choice)%len(typedTextBoundaries)]
	case typedBinary:
		value.Bytes = typedBinaryBoundaries[int(choice)%len(typedBinaryBoundaries)]
	case typedDate:
		value.Number = typedDateBoundaries[int(choice)%len(typedDateBoundaries)]
	case typedTimestamp:
		value.Number = typedTimestampBoundaries[int(choice)%len(typedTimestampBoundaries)]
	case typedTimestamptz:
		value.Number = typedTimestamptzBoundaries[int(choice)%len(typedTimestamptzBoundaries)]
	case typedUUID:
		value.Text = typedUUIDBoundaries[int(choice)%len(typedUUIDBoundaries)]
	case typedArray:
		for index := range int(choice % scenarioArrayCount) {
			value.Elements = append(value.Elements, TypedNumber(typedSigned, strconv.Itoa(index-1), 0))
		}
	case typedJSON:
		value.Text = typedJSONBoundaries[int(choice)%len(typedJSONBoundaries)]
	}
	return value
}

// scenarioValueCount records the independent boundary set for a family.
//
// Takes kind (string) which selects the independent value family.
//
// Returns int which counts the independent boundaries, or zero for an unknown family.
func scenarioValueCount(kind string) int {
	return scenarioBoundaryCounts[kind]
}
