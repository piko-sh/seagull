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
	"testing"

	"github.com/stretchr/testify/require"
)

type mood string

func TestConvertStringsConvertsEachValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		values []mood
		want   []string
	}{
		{name: "nil stays nil", values: nil, want: nil},
		{name: "empty stays empty", values: []mood{}, want: []string{}},
		{name: "values keep their order", values: []mood{"calm", "stormy", "calm"}, want: []string{"calm", "stormy", "calm"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted := sglConvertStrings[string](testCase.values)

			require.Equal(t, testCase.want, converted)
			require.Equal(t, testCase.values, sglConvertStrings[mood](converted))
		})
	}
}

func TestConvertStringsCopiesTheValues(t *testing.T) {
	t.Parallel()

	values := []mood{"calm"}
	converted := sglConvertStrings[string](values)
	values[0] = "stormy"

	require.Equal(t, []string{"calm"}, converted)
}

func TestConvertStringPointersConvertsEachValueAndKeepsEachNil(t *testing.T) {
	t.Parallel()

	calm, stormy := mood("calm"), mood("stormy")
	calmText, stormyText := "calm", "stormy"
	testCases := []struct {
		name   string
		values []*mood
		want   []*string
	}{
		{name: "nil stays nil", values: nil, want: nil},
		{name: "empty stays empty", values: []*mood{}, want: []*string{}},
		{name: "values keep their order and nil elements stay nil", values: []*mood{&calm, nil, &stormy}, want: []*string{&calmText, nil, &stormyText}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted := sglConvertStringPointers[string](testCase.values)

			require.Equal(t, testCase.want, converted)
			require.Equal(t, testCase.values, sglConvertStringPointers[mood](converted))
		})
	}
}

func TestConvertStringPointersCopiesTheValues(t *testing.T) {
	t.Parallel()

	value := mood("calm")
	converted := sglConvertStringPointers[string]([]*mood{&value})
	value = "stormy"

	require.Equal(t, "calm", *converted[0])
	require.Equal(t, mood("stormy"), value)
}

func TestConvertStringMapConvertsEachValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		values map[int]mood
		want   map[int]string
	}{
		{name: "nil stays nil", values: nil, want: nil},
		{name: "empty stays empty", values: map[int]mood{}, want: map[int]string{}},
		{name: "values keep their keys", values: map[int]mood{1: "calm", 2: "stormy"}, want: map[int]string{1: "calm", 2: "stormy"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			converted := sglConvertStringMap[string](testCase.values)

			require.Equal(t, testCase.want, converted)
			require.Equal(t, testCase.values, sglConvertStringMap[mood](converted))
		})
	}
}

func TestConvertStringMapCopiesTheMap(t *testing.T) {
	t.Parallel()

	values := map[string]mood{"a": "calm"}
	converted := sglConvertStringMap[string](values)
	values["a"] = "stormy"

	require.Equal(t, map[string]string{"a": "calm"}, converted)
}

func TestDereferenceReadsThePointedValueOrTheZeroValue(t *testing.T) {
	t.Parallel()

	moods := []mood{"calm"}
	empty := []mood{}
	testCases := []struct {
		name  string
		value *[]mood
		want  []mood
	}{
		{name: "a nil pointer reads as a nil slice", value: nil, want: nil},
		{name: "a pointer to an empty slice reads as the empty slice", value: &empty, want: []mood{}},
		{name: "a pointer to values reads as the values", value: &moods, want: []mood{"calm"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, sglDereference(testCase.value))
		})
	}
}
