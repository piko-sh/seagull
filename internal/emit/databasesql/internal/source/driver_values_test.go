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
	"database/sql/driver"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var errDriverValueRefused = errors.New("the value refused to convert")

type driverValueCelsius float64

type driverValueLabel string

type driverValueValuer struct {
	text string
}

func (valuer driverValueValuer) Value() (driver.Value, error) {
	return "valued " + valuer.text, nil
}

type driverValueFailingValuer struct{}

func (driverValueFailingValuer) Value() (driver.Value, error) {
	return nil, errDriverValueRefused
}

func TestDriverValueConvertsTheWrappedParameterToADriverValue(t *testing.T) {
	t.Parallel()

	moment := time.Date(2024, 3, 5, 10, 11, 12, 0, time.UTC)
	count := 9
	var missing *int

	testCases := []struct {
		value any
		want  driver.Value
		name  string
	}{
		{name: "nil", value: nil, want: nil},
		{name: "an int", value: 5, want: int64(5)},
		{name: "a narrow int", value: int32(-6), want: int64(-6)},
		{name: "an unsigned int", value: uint16(7), want: int64(7)},
		{name: "a wide unsigned int below the sign bit", value: uint64(8), want: int64(8)},
		{name: "a narrow float", value: float32(1.5), want: float64(1.5)},
		{name: "a named float", value: driverValueCelsius(21.5), want: float64(21.5)},
		{name: "a boolean", value: true, want: true},
		{name: "a string", value: "anchor", want: "anchor"},
		{name: "a named string", value: driverValueLabel("buoy"), want: "buoy"},
		{name: "bytes", value: []byte("raw"), want: []byte("raw")},
		{name: "a time", value: moment, want: moment},
		{name: "a pointer", value: &count, want: int64(9)},
		{name: "a nil pointer", value: missing, want: nil},
		{name: "a valuer", value: driverValueValuer{text: "cleat"}, want: "valued cleat"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted, err := sglDriverValue{value: testCase.value}.Value()
			require.NoError(t, err)
			require.Equal(t, testCase.want, converted)
		})
	}
}

func TestDriverValueReportsAParameterWithNoDriverValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value     any
		name      string
		wantError string
	}{
		{name: "an unsigned int with the sign bit set", value: uint64(1) << 63, wantError: "high bit set"},
		{name: "a struct", value: struct{ Length int }{Length: 3}, wantError: "unsupported type"},
		{name: "a slice of anything but bytes", value: []int{1, 2}, wantError: "unsupported type"},
		{name: "a map", value: map[string]int{"a": 1}, wantError: "unsupported type"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted, err := sglDriverValue{value: testCase.value}.Value()
			require.ErrorContains(t, err, testCase.wantError)
			require.Nil(t, converted)
		})
	}
}

func TestDriverValuePassesOnTheErrorOfAFailingValuer(t *testing.T) {
	t.Parallel()

	_, err := sglDriverValue{value: driverValueFailingValuer{}}.Value()

	require.ErrorIs(t, err, errDriverValueRefused)
}

type driverValueBytes []byte

func TestDriverTextBindsBytesAsText(t *testing.T) {
	t.Parallel()

	note := driverValueBytes(`{"n":1}`)
	var missing *driverValueBytes
	testCases := []struct {
		value any
		want  driver.Value
		name  string
	}{
		{name: "bytes", value: []byte("raw"), want: "raw"},
		{name: "a named byte slice", value: note, want: `{"n":1}`},
		{name: "a pointer to a named byte slice", value: &note, want: `{"n":1}`},
		{name: "a nil pointer", value: missing, want: nil},
		{name: "a named string", value: driverValueLabel("buoy"), want: "buoy"},
		{name: "a valuer", value: driverValueValuer{text: "cleat"}, want: "valued cleat"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted, err := sglDriverText{value: testCase.value}.Value()
			require.NoError(t, err)
			require.Equal(t, testCase.want, converted)
		})
	}

	_, err := sglDriverText{value: driverValueFailingValuer{}}.Value()
	require.ErrorIs(t, err, errDriverValueRefused)
}

func TestDriverJSONBindsTheDocumentTheValueEncodesTo(t *testing.T) {
	t.Parallel()

	var missing *driverValueValuer
	var noTags []string
	testCases := []struct {
		value any
		want  driver.Value
		name  string
	}{
		{name: "a plain struct as its document", value: struct {
			Title string `json:"title"`
		}{Title: "gulls"}, want: `{"title":"gulls"}`},
		{name: "a slice of strings as its document", value: []string{"sea", "sky"}, want: `["sea","sky"]`},
		{name: "a value with a driver value of its own", value: driverValueValuer{text: "gulls"}, want: "valued gulls"},
		{name: "a nil pointer as NULL", value: missing, want: nil},
		{name: "a nil slice as NULL", value: noTags, want: nil},
		{name: "nothing as NULL", value: nil, want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, err := sglDriverJSON{value: testCase.value}.Value()

			require.NoError(t, err)
			require.Equal(t, testCase.want, value)
		})
	}
}

func TestDriverJSONReportsAValueThatDoesNotEncode(t *testing.T) {
	t.Parallel()

	_, err := sglDriverJSON{value: make(chan int)}.Value()

	require.ErrorContains(t, err, "unsupported type")
}

type driverBinaryIdentifier [4]byte

func (identifier driverBinaryIdentifier) MarshalBinary() ([]byte, error) {
	return identifier[:], nil
}

func (identifier driverBinaryIdentifier) Value() (driver.Value, error) {
	return "text of the identifier", nil
}

type driverBinaryArray [2]byte

func TestDriverBinaryBindsTheBytesAValueMarshalsItselfTo(t *testing.T) {
	t.Parallel()

	identifier := driverBinaryIdentifier{1, 2, 3, 4}
	var missing *driverBinaryIdentifier

	testCases := []struct {
		value any
		want  driver.Value
		name  string
	}{
		{name: "nil", value: nil, want: nil},
		{name: "a binary marshaler ahead of its valuer", value: identifier, want: []byte{1, 2, 3, 4}},
		{name: "a pointer to a binary marshaler", value: &identifier, want: []byte{1, 2, 3, 4}},
		{name: "a nil pointer", value: missing, want: nil},
		{name: "a valuer", value: driverValueValuer{text: "cleat"}, want: "valued cleat"},
		{name: "bytes", value: []byte("raw"), want: []byte("raw")},
		{name: "a value database/sql cannot convert, for the driver to bind", value: driverBinaryArray{5, 6}, want: driverBinaryArray{5, 6}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, err := sglDriverBinary{value: testCase.value}.Value()
			require.NoError(t, err)
			require.Equal(t, testCase.want, value)
		})
	}
}

type driverDecimalAmount struct {
	text string
}

func (amount driverDecimalAmount) Value() (driver.Value, error) {
	return amount.text, nil
}

func (*driverDecimalAmount) Decompose(_ []byte) (byte, bool, []byte, int32) {
	return 0, false, nil, 0
}

func TestDatabaseSQLHandsAPointerWithADecomposeMethodToTheDriverAsItIs(t *testing.T) {
	t.Parallel()

	price := &driverDecimalAmount{text: "12.50"}

	converted, err := driver.DefaultParameterConverter.ConvertValue(price)

	require.NoError(t, err)
	require.Same(t, price, converted)
}

func TestDriverDecimalBindsADecimalHeldByAPointerAsItsValue(t *testing.T) {
	t.Parallel()

	var missing *driverDecimalAmount

	testCases := []struct {
		value any
		want  driver.Value
		name  string
	}{
		{name: "a nil pointer binds NULL", value: missing, want: nil},
		{name: "nil binds NULL", value: nil, want: nil},
		{name: "a decimal binds the value its driver.Valuer writes", value: &driverDecimalAmount{text: "12.50"}, want: "12.50"},
		{name: "a decimal held by value binds the same", value: driverDecimalAmount{text: "7"}, want: "7"},
		{name: "a value of another type converts as database/sql converts it", value: int32(4), want: int64(4)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted, err := driver.DefaultParameterConverter.ConvertValue(sglDriverDecimalValue{value: testCase.value})
			require.NoError(t, err)
			require.Equal(t, testCase.want, converted)
		})
	}
}

type namedUnsigned uint64

func TestUnsignedBindingsPreserveAllBitsAndNulls(t *testing.T) {
	high := uint64(math.MaxUint64)
	named := namedUnsigned(high)
	pointer := &high
	var missing *uint64
	for _, value := range []any{high, named, &high, &named, &pointer} {
		got, err := (sglDriverUnsigned{value: value}).Value()
		require.NoError(t, err)
		require.Equal(t, "18446744073709551615", got)
	}
	for _, value := range []any{nil, missing, &missing} {
		got, err := (sglDriverUnsigned{value: value}).Value()
		require.NoError(t, err)
		require.Nil(t, got)
	}
	got, err := (sglDriverUnsigned{value: driverValueValuer{text: "override"}}).Value()
	require.NoError(t, err)
	require.Equal(t, "valued override", got)
	_, err = (sglDriverUnsigned{value: driverValueFailingValuer{}}).Value()
	require.ErrorIs(t, err, errDriverValueRefused)
}

func TestUnsignedRuntimeConversionLeavesNativeValuesUntouched(t *testing.T) {
	high := uint64(math.MaxUint64)
	composite := map[string]any{"wide": high}
	array := []uint64{high}
	original := []any{nil, int64(-1), "18446744073709551615", []byte{0, 255}, composite, array, driverValueValuer{text: "native"}}
	got := sglUnsignedArguments(append([]any(nil), original...))
	require.Equal(t, original, got)
	for _, value := range sglUnsignedArguments([]any{high, &high, namedUnsigned(high)}) {
		wrapper, ok := value.(driver.Valuer)
		require.True(t, ok)
		converted, err := wrapper.Value()
		require.NoError(t, err)
		require.Equal(t, "18446744073709551615", converted)
	}
}

type cyclicPointer *cyclicPointer

func TestUnsignedBindingsRefuseCyclicPointers(t *testing.T) {
	cycle := new(cyclicPointer)
	*cycle = cycle
	_, err := (sglDriverUnsigned{value: cycle}).Value()
	require.Error(t, err)
	got := sglUnsignedArguments([]any{cycle})
	require.Equal(t, []any{cycle}, got)
}

func TestBinaryBindingsDistinguishNullAndEmpty(t *testing.T) {
	var missing []byte
	empty := []byte{}
	type namedBytes []byte
	for _, value := range []any{missing, &missing, namedBytes(nil)} {
		got, err := (sglDriverBinary{value: value}).Value()
		require.NoError(t, err)
		require.Nil(t, got)
		require.Nil(t, sglBinaryArguments([]any{value})[0])
	}
	for _, value := range []any{empty, &empty, namedBytes{}} {
		got, err := (sglDriverBinary{value: value}).Value()
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Empty(t, got)
		require.Equal(t, value, sglBinaryArguments([]any{value})[0])
	}
	valuer := driverValueValuer{text: "kept"}
	require.Equal(t, valuer, sglBinaryArguments([]any{valuer})[0])
	var cycle any
	cycle = &cycle
	require.False(t, sglNilBinary(cycle))
}

type Null[T any] struct {
	V     T
	Valid bool
}

func TestUnsignedBindingsReadTheGeneratedNullType(t *testing.T) {
	t.Parallel()

	value, err := sglDriverUnsigned{value: Null[uint64]{V: 1 << 63, Valid: true}}.Value()
	require.NoError(t, err)
	require.Equal(t, "9223372036854775808", value, "a set Null writes its unsigned value with every bit")

	value, err = sglDriverUnsigned{value: Null[uint64]{}}.Value()
	require.NoError(t, err)
	require.Nil(t, value, "an unset Null writes NULL")
}
