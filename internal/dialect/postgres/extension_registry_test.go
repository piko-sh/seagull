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

package postgres

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestEachExtensionAddsItsFunctionsAsReadOnly(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		extension string
		wantNames []string
	}{
		{name: "pgcrypto", extension: "pgcrypto", wantNames: []string{"gen_random_uuid", "crypt", "digest", "gen_salt", "hmac"}},
		{name: "uuid-ossp", extension: "uuid-ossp", wantNames: []string{
			"uuid_generate_v1", "uuid_generate_v1mc", "uuid_generate_v3", "uuid_generate_v4", "uuid_generate_v5", "uuid_nil",
		}},
		{name: "pg_trgm", extension: "pg_trgm", wantNames: []string{"similarity", "word_similarity", "strict_word_similarity", "show_trgm"}},
		{name: "hstore", extension: "hstore", wantNames: []string{"akeys", "avals", "hstore_to_json"}},
		{name: "ltree", extension: "ltree", wantNames: []string{"nlevel", "lca"}},
		{name: "an extension with no registered functions", extension: "postgis", wantNames: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var names []string
			for _, function := range New().LoadExtensionFunctions(testCase.extension) {
				require.NotNil(t, function)
				names = append(names, function.Name)
				assert.Equal(t, sqlmodel.DataAccessReadOnly, function.DataAccess, "function %s", function.Name)
			}
			assert.Equal(t, testCase.wantNames, names)
		})
	}
}

func TestExtensionLookupsShareTheRegistryBuiltOnTheFirst(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		extension string
	}{
		{name: "pgcrypto", extension: "pgcrypto"},
		{name: "ltree", extension: "ltree"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			first := lookupExtensionFunctions(testCase.extension)
			second := lookupExtensionFunctions(testCase.extension)
			require.NotEmpty(t, first)
			require.Len(t, second, len(first))
			for index := range first {
				assert.Same(t, first[index], second[index])
			}
		})
	}
}

func TestChangingLoadedExtensionFunctionsLeavesTheNextLoadAlone(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		extension string
	}{
		{name: "pgcrypto", extension: "pgcrypto"},
		{name: "uuid-ossp", extension: "uuid-ossp"},
		{name: "pg_trgm", extension: "pg_trgm"},
		{name: "hstore", extension: "hstore"},
		{name: "ltree", extension: "ltree"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			want := engine.LoadExtensionFunctions(testCase.extension)
			require.NotEmpty(t, want)

			changed := engine.LoadExtensionFunctions(testCase.extension)
			for index, function := range changed {
				assert.NotSame(t, want[index], function)
				function.Name = "changed"
				function.DataAccess = sqlmodel.DataAccessModifiesData
				function.ReturnType.EngineName = "changed"
				for argumentIndex := range function.Arguments {
					function.Arguments[argumentIndex].Type.EngineName = "changed"
				}
				function.Arguments = append(function.Arguments, sqlmodel.FunctionArgument{Name: "added"})
			}

			assert.Equal(t, want, engine.LoadExtensionFunctions(testCase.extension))
			assert.Equal(t, want, New().LoadExtensionFunctions(testCase.extension))
		})
	}
}

func TestRegisteredExtensionTypesHoldNothingACopyWouldShare(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		extension string
	}{
		{name: "pgcrypto", extension: "pgcrypto"},
		{name: "uuid-ossp", extension: "uuid-ossp"},
		{name: "pg_trgm", extension: "pg_trgm"},
		{name: "hstore", extension: "hstore"},
		{name: "ltree", extension: "ltree"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			for _, function := range lookupExtensionFunctions(testCase.extension) {
				types := []sqlmodel.SQLType{function.ReturnType}
				for _, argument := range function.Arguments {
					types = append(types, argument.Type)
				}
				for _, sqlType := range types {
					fields := reflect.ValueOf(sqlType)
					for index := range fields.NumField() {
						field := fields.Field(index)
						switch field.Kind() {
						case reflect.Pointer, reflect.Slice, reflect.Map:
							assert.True(t, field.IsNil(), "function %s type field %s", function.Name, fields.Type().Field(index).Name)
						default:
						}
					}
				}
			}
		})
	}
}
