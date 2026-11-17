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

package clickhouse

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEveryTypeAliasNamesAKnownType(t *testing.T) {
	t.Parallel()

	types := primitiveTypes()
	for alias, canonical := range clickHouseTypeAliases {
		native, found := types[strings.ToLower(canonical)]
		require.True(t, found, "alias %q names %q, which is not a primitive type", alias, canonical)
		assert.Equal(t, native, types[alias], "alias %q", alias)
	}
}

func TestTypeAliasPrefixKnowsEveryLeadingRunOfWords(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		want bool
	}{
		{name: "national", want: true},
		{name: "NATIONAL CHARACTER", want: true},
		{name: "national character large object", want: true},
		{name: "national character large", want: true},
		{name: "national object", want: false},
		{name: "Int32", want: false},
		{name: "", want: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, typeAliasPrefix(testCase.name))
		})
	}
}

func TestTypeAliasesParseToTheirNativeTypes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input string
		want  string
	}{
		{input: "BIGINT UNSIGNED", want: "UInt64"},
		{input: "int(11) unsigned", want: "UInt32"},
		{input: "double precision", want: "Float64"},
		{input: "national character varying", want: "String"},
		{input: "TINYINT", want: "Int8"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			result, err := parseClickHouseType(testCase.input, defaultMaxTypeParseDepth)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, result.SQLType.EngineName)
		})
	}
}

func TestTypeAliasRefusesAMalformedDisplayWidth(t *testing.T) {
	t.Parallel()

	_, err := parseClickHouseType("int(11", defaultMaxTypeParseDepth)
	require.Error(t, err)
}

func TestColumnTypeAliasKeepsItsWordsInTheColumnType(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE TABLE t (id int unsigned, name character varying(20)) ENGINE = MergeTree ORDER BY id")
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 2)
	assert.Equal(t, "UInt32", mutation.Columns[0].SQLType.EngineName)
	assert.Equal(t, "String", mutation.Columns[1].SQLType.EngineName)
}
