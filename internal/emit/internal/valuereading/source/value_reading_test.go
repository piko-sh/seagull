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
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

var errElementRefused = errors.New("the element is refused")

type orderedEntries struct {
	keys   []any
	values []any
}

func (entries *orderedEntries) Keys() []any {
	return entries.keys
}

func (entries *orderedEntries) Values() []any {
	return entries.values
}

type mood string

func readElement(target reflect.Value, source any) error {
	if source == nil {
		target.SetZero()
		return nil
	}
	value := reflect.ValueOf(source)
	if value.Type().AssignableTo(target.Type()) {
		target.Set(value)
		return nil
	}
	switch target.Kind() {
	case reflect.Slice:
		return sglReadList(target, value, readElement)
	case reflect.Map:
		return sglReadMap(target, value, readElement)
	default:
		return sglReadScalar(target, value)
	}
}

func readInto[T any](source any) (T, error) {
	var target T
	err := readElement(reflect.ValueOf(&target).Elem(), source)
	return target, err
}

func TestValueReadingReadsTheDriversValuesIntoTheGoType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		read func(source any) (any, error)
		want any
		from any
		name string
	}{
		{name: "a list into a slice", read: reader[[]int64], from: []any{int64(1), int32(2)}, want: []int64{1, 2}},
		{name: "an array into a slice", read: reader[[]int32], from: [2]int64{3, 4}, want: []int32{3, 4}},
		{name: "a nil list into a nil slice", read: reader[[]int64], from: []any(nil), want: []int64(nil)},
		{name: "an empty list into an empty slice", read: reader[[]int64], from: []any{}, want: []int64{}},
		{name: "nested lists into nested slices", read: reader[[][]string], from: []any{[]any{"a"}, []any{"b", "c"}}, want: [][]string{{"a"}, {"b", "c"}}},
		{name: "a Go map into a map", read: reader[map[string]int8], from: map[any]any{"a": int64(1)}, want: map[string]int8{"a": 1}},
		{name: "a nil Go map into a nil map", read: reader[map[string]int8], from: map[any]any(nil), want: map[string]int8(nil)},
		{
			name: "an ordered map into a map", read: reader[map[int32]mood],
			from: orderedEntries{keys: []any{int64(1), int64(2)}, values: []any{"calm", "stormy"}},
			want: map[int32]mood{1: "calm", 2: "stormy"},
		},
		{name: "an integer into a narrower integer", read: reader[int8], from: int64(-12), want: int8(-12)},
		{name: "an unsigned integer into a signed one", read: reader[int64], from: uint32(7), want: int64(7)},
		{name: "a float into a narrower float", read: reader[float32], from: 1.5, want: float32(1.5)},
		{name: "a string into a defined string", read: reader[mood], from: "calm", want: mood("calm")},
		{name: "a boolean into a boolean", read: reader[bool], from: true, want: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := testCase.read(testCase.from)

			require.NoError(t, err)
			require.Equal(t, testCase.want, got)
		})
	}
}

func reader[T any](source any) (any, error) {
	return readInto[T](source)
}

func TestValueReadingRefusesAValueItCannotRead(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		read func(source any) (any, error)
		from any
		name string
		want string
	}{
		{name: "an integer that overflows", read: reader[int8], from: int64(300), want: "300 overflows int8"},
		{name: "a negative integer into an unsigned one", read: reader[uint16], from: int64(-1), want: "-1 overflows uint16"},
		{name: "an unsigned integer past the signed range", read: reader[int64], from: uint64(math.MaxUint64), want: "18446744073709551615 overflows int64"},
		{name: "a string into an integer", read: reader[int64], from: "seven", want: "cannot read string into int64"},
		{name: "a float into an integer", read: reader[int64], from: 1.5, want: "cannot read float64 into int64"},
		{name: "a struct into an integer", read: reader[int64], from: struct{}{}, want: "cannot read struct {} into int64"},
		{name: "a scalar into a slice", read: reader[[]int64], from: int64(1), want: "cannot read int64 into []int64"},
		{name: "a scalar into a map", read: reader[map[string]int64], from: int64(1), want: "cannot read int64 into map[string]int64"},
		{name: "an element that cannot be read", read: reader[[]int8], from: []any{int64(1), int64(300)}, want: "element 1: 300 overflows int8"},
		{name: "a key that cannot be read", read: reader[map[int8]string], from: map[any]any{int64(300): "a"}, want: "key 300: 300 overflows int8"},
		{name: "a value that cannot be read", read: reader[map[string]int8], from: map[any]any{"a": int64(300)}, want: "value of key a: 300 overflows int8"},
		{
			name: "an ordered map whose keys and values differ in number", read: reader[map[string]int8],
			from: orderedEntries{keys: []any{"a"}, values: nil}, want: "cannot read source.orderedEntries into map[string]int8",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := testCase.read(testCase.from)

			require.EqualError(t, err, testCase.want)
		})
	}
}

func TestValueReadingStopsAtTheFirstElementTheReaderRefuses(t *testing.T) {
	t.Parallel()

	refusing := func(reflect.Value, any) error { return errElementRefused }
	testCases := []struct {
		read func() error
		name string
	}{
		{name: "a list", read: func() error {
			var list []int
			return sglReadList(reflect.ValueOf(&list).Elem(), reflect.ValueOf([]any{1}), refusing)
		}},
		{name: "a map", read: func() error {
			var entries map[string]int
			return sglReadMap(reflect.ValueOf(&entries).Elem(), reflect.ValueOf(map[string]any{"a": 1}), refusing)
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, testCase.read(), errElementRefused)
		})
	}
}

func TestExportsReadAsTheRenderedReadersDo(t *testing.T) {
	t.Parallel()

	var list []int8
	require.NoError(t, ReadList(reflect.ValueOf(&list).Elem(), reflect.ValueOf([]any{int64(1)}), readElement))
	require.Equal(t, []int8{1}, list)

	var entries map[string]int8
	require.NoError(t, ReadMap(reflect.ValueOf(&entries).Elem(), reflect.ValueOf(map[string]any{"a": int64(2)}), readElement))
	require.Equal(t, map[string]int8{"a": 2}, entries)

	var number int16
	require.NoError(t, ReadScalar(reflect.ValueOf(&number).Elem(), reflect.ValueOf(int64(3))))
	require.Equal(t, int16(3), number)

	require.EqualError(t, Unreadable(reflect.TypeFor[string](), reflect.TypeFor[int]()), "cannot read string into int")
}
