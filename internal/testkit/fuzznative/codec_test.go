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

package fuzznative

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

func textValue(kind, text string) fuzzcase.TypedValue {
	value := fuzzcase.TypedNumber(kind, numberZero, 0)
	value.Text = text
	return value
}

func TestArgumentsRoundTripThroughGeneratedTypes(t *testing.T) {
	t.Parallel()
	float := fuzzcase.TypedNumber(kindFloat, numberZero, 0)
	float.Bits = math.Float64bits(-2.5)
	binary := fuzzcase.TypedNumber(kindBinary, numberZero, 0)
	binary.Bytes = []byte{0, 1, 2}
	array := fuzzcase.TypedNumber(kindArray, numberZero, 0)
	array.Elements = []fuzzcase.TypedValue{fuzzcase.TypedNumber(kindSigned, "4", 0), fuzzcase.TypedNumber(kindSigned, "-9", 0)}
	for _, test := range []struct {
		target reflect.Type
		value  fuzzcase.TypedValue
	}{
		{target: reflect.TypeFor[int32](), value: fuzzcase.TypedNumber(kindSigned, "-7", 0)},
		{target: reflect.TypeFor[*int64](), value: fuzzcase.TypedNumber(kindSigned, "8", 0)},
		{target: reflect.TypeFor[uint16](), value: fuzzcase.TypedNumber(kindUnsigned, "65535", 0)},
		{target: reflect.TypeFor[string](), value: fuzzcase.TypedNumber(kindUnsigned, "18446744073709551615", 0)},
		{target: reflect.TypeFor[float64](), value: float},
		{target: reflect.TypeFor[bool](), value: fuzzcase.TypedNumber(kindBoolean, numberOne, 0)},
		{target: reflect.TypeFor[Null[bool]](), value: fuzzcase.TypedNumber(kindBoolean, numberZero, 0)},
		{target: reflect.TypeFor[string](), value: fuzzcase.TypedNumber(kindDecimal, "12345", 2)},
		{target: reflect.TypeFor[string](), value: textValue(kindText, "héllo")},
		{target: reflect.TypeFor[string](), value: textValue(kindUUID, "123e4567-e89b-12d3-a456-426614174000")},
		{target: reflect.TypeFor[[]byte](), value: binary},
		{target: reflect.TypeFor[string](), value: textValue(kindJSON, `{"a":[1,2]}`)},
		{target: reflect.TypeFor[[]byte](), value: textValue(kindJSON, `[true]`)},
		{target: reflect.TypeFor[time.Time](), value: fuzzcase.TypedNumber(kindDate, "2024-02-29", 0)},
		{target: reflect.TypeFor[time.Time](), value: fuzzcase.TypedNumber(kindTimestamp, "2024-02-29 01:02:03.000004", 0)},
		{target: reflect.TypeFor[time.Time](), value: fuzzcase.TypedNumber(kindTimestampTZ, "2024-02-29T01:02:03.000004Z", 0)},
		{target: reflect.TypeFor[[]int64](), value: array},
		{target: reflect.TypeFor[[2]int64](), value: array},
	} {
		argument, err := Argument(test.target, test.value)
		require.NoError(t, err, "%s into %s", test.value.Kind, test.target)
		observed, err := Observed(argument, test.value.Kind, false)
		require.NoError(t, err, "%s from %s", test.value.Kind, test.target)
		assert.NoError(t, CompareValue(test.value, observed), "%s through %s", test.value.Kind, test.target)
	}
}

func TestArgumentRefusesLossyTargets(t *testing.T) {
	t.Parallel()
	array := fuzzcase.TypedNumber(kindArray, numberZero, 0)
	array.Elements = []fuzzcase.TypedValue{fuzzcase.TypedNumber(kindSigned, "1", 0)}
	for _, test := range []struct {
		target reflect.Type
		value  fuzzcase.TypedValue
	}{
		{target: reflect.TypeFor[int8](), value: fuzzcase.TypedNumber(kindSigned, "300", 0)},
		{target: reflect.TypeFor[uint8](), value: fuzzcase.TypedNumber(kindUnsigned, "300", 0)},
		{target: reflect.TypeFor[bool](), value: fuzzcase.TypedNumber(kindSigned, "1", 0)},
		{target: reflect.TypeFor[float64](), value: fuzzcase.TypedNumber(kindDecimal, "1", 0)},
		{target: reflect.TypeFor[[]int64](), value: fuzzcase.TypedNumber(kindSigned, "1", 0)},
		{target: reflect.TypeFor[[3]int64](), value: array},
		{target: reflect.TypeFor[time.Time](), value: fuzzcase.TypedNumber(kindSigned, "1", 0)},
		{target: reflect.TypeFor[map[string]int](), value: fuzzcase.TypedNumber(kindSigned, "1", 0)},
	} {
		_, err := Argument(test.target, test.value)
		assert.Error(t, err, "%s into %s", test.value.Kind, test.target)
	}
}

func TestObservedRefusesContradictoryFields(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value any
		kind  string
	}{
		{value: "x", kind: kindSigned},
		{value: 1, kind: kindFloat},
		{value: 1, kind: kindBoolean},
		{value: 1, kind: kindBinary},
		{value: 1, kind: kindDate},
		{value: 1, kind: kindArray},
		{value: "{", kind: kindJSON},
		{value: "x", kind: kindDecimal},
		{value: "-1", kind: kindUnsigned},
		{value: 1, kind: "unknown"},
	} {
		_, err := Observed(reflect.ValueOf(test.value), test.kind, false)
		assert.Error(t, err, "%v as %s", test.value, test.kind)
	}
	var missing *int64
	_, err := Observed(reflect.ValueOf(missing), kindSigned, false)
	require.Error(t, err)
	observed, err := Observed(reflect.ValueOf(missing), kindJSON, false)
	require.NoError(t, err)
	require.Equal(t, jsonNull, observed.Text)
}

func TestSignedIntegerRefusesValuesBeyondInt64(t *testing.T) {
	t.Parallel()
	number, err := signedInteger(math.MaxInt64)
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), number)
	_, err = signedInteger(math.MaxInt64 + 1)
	require.Error(t, err)
}

func TestCanonicalTimeKeepsEachTemporalMeaning(t *testing.T) {
	t.Parallel()
	moment := time.Date(2024, 2, 29, 1, 2, 3, 4000, time.FixedZone("x", 3600))
	assert.Equal(t, "2024-02-29", CanonicalTime(kindDate, moment).Number)
	assert.Equal(t, "2024-02-29 01:02:03.000004", CanonicalTime(kindTimestamp, moment).Number)
	assert.Equal(t, "2024-02-29T00:02:03.000004Z", CanonicalTime(kindTimestampTZ, moment).Number)
}
