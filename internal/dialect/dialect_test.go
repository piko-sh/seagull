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

package dialect_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
)

func TestInfoCloneSharesNoSliceOrMapWithTheOriginal(t *testing.T) {
	t.Parallel()

	var original dialect.Info
	fillCollections(reflect.ValueOf(&original).Elem())

	clone := original.Clone()

	require.Equal(t, original, clone)
	paths := assertNothingShared(t, reflect.ValueOf(original), reflect.ValueOf(clone), "Info")
	assert.NotEmpty(t, paths, "the walk must reach the slices and maps Info holds")
}

func TestInfoCloneOfANilInfoIsTheZeroInfo(t *testing.T) {
	t.Parallel()

	var info *dialect.Info

	require.Equal(t, dialect.Info{}, info.Clone())
}

func TestInfoCloneKeepsNilCollectionsNilAndCopiesValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		info dialect.Info
	}{
		{name: "zero value", info: dialect.Info{}},
		{
			name: "populated",
			info: dialect.Info{
				Name:             "probe",
				Drivers:          []string{"example.com/driver"},
				SchemaSearchPath: []string{"public", "extra"},
				Filters:          dialect.FilterSyntax{Operators: []string{"@>"}, JSONPathFunctions: nil, Casts: nil, CastTypes: nil},
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clone := testCase.info.Clone()

			assert.Equal(t, testCase.info, clone)
			assert.Equal(t, testCase.info.Drivers == nil, clone.Drivers == nil)
			assert.Equal(t, testCase.info.OperatorFeatures == nil, clone.OperatorFeatures == nil)
			if len(clone.SchemaSearchPath) > 0 {
				clone.SchemaSearchPath[0] = "changed"
				assert.Equal(t, "public", testCase.info.SchemaSearchPath[0])
			}
		})
	}
}

func fillCollections(value reflect.Value) {
	switch value.Kind() {
	case reflect.Struct:
		for _, field := range value.Fields() {
			if field.CanSet() {
				fillCollections(field)
			}
		}
	case reflect.Slice:
		value.Set(reflect.MakeSlice(value.Type(), 1, 1))
		fillCollections(value.Index(0))
	case reflect.Map:
		filled := reflect.MakeMapWithSize(value.Type(), 1)
		filled.SetMapIndex(reflect.Zero(value.Type().Key()), reflect.Zero(value.Type().Elem()))
		value.Set(filled)
	default:
	}
}

func assertNothingShared(t *testing.T, original, clone reflect.Value, path string) []string {
	t.Helper()
	var compared []string
	switch original.Kind() {
	case reflect.Struct:
		for field := range original.Type().Fields() {
			name := path + "." + field.Name
			compared = append(compared, assertNothingShared(t, original.FieldByIndex(field.Index), clone.FieldByIndex(field.Index), name)...)
		}
	case reflect.Slice:
		compared = append(compared, path)
		if assert.Positive(t, original.Len(), "%s was not filled", path) {
			assert.NotEqual(t, original.Pointer(), clone.Pointer(), "Clone shares the slice %s", path)
			compared = append(compared, assertNothingShared(t, original.Index(0), clone.Index(0), path+"[0]")...)
		}
	case reflect.Map:
		compared = append(compared, path)
		assert.Positive(t, original.Len(), "%s was not filled", path)
		assert.NotEqual(t, original.Pointer(), clone.Pointer(), "Clone shares the map %s", path)
	default:
	}
	return compared
}
