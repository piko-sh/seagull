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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
)

func TestInsertDataReadsMapLiteralsBesidePlaceholders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantParameters []string
	}{
		{
			name:           "a map literal beside a placeholder in one row",
			sql:            "INSERT INTO t (id, m) VALUES ({id:UInt64}, {'a': 1, 'b': 2})",
			wantParameters: []string{"id"},
		},
		{
			name:           "empty and number-keyed map literals over rows",
			sql:            "INSERT INTO t VALUES (1, {}), ({id:UInt64}, {1: 'x'})",
			wantParameters: []string{"id"},
		},
		{
			name:           "map literals inside an array, a tuple and another map",
			sql:            "INSERT INTO t VALUES ({id:UInt64}, [{'a': 1}, {}], ({'b': 2}, 3), {'c': {'d': 4}}, {note:String})",
			wantParameters: []string{"id", "note"},
		},
		{
			name:           "boolean-keyed map literals beside a placeholder",
			sql:            "INSERT INTO t (id, flags) VALUES ({id:UInt64}, {true: 1, false: 0})",
			wantParameters: []string{"id"},
		},
		{
			name: "map literals whose values are spelt like type names beside placeholders",
			sql: "INSERT INTO t (id, flags, more, ratios, note) VALUES " +
				"({id:UInt64}, {true: false}, {true: false, false: {TRUE: NULL}}, {true: nan, false: inf}, {note:String})",
			wantParameters: []string{"id", "note"},
		},
		{
			name:           "map literals of literal words after FORMAT Values",
			sql:            "INSERT INTO t FORMAT Values (1, {true: false, false: true})",
			wantParameters: nil,
		},
		{
			name:           "inline JSON rows after FORMAT",
			sql:            `INSERT INTO t FORMAT JSONEachRow {"id": 1, "m": {"a": 2}}`,
			wantParameters: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			var names []string
			for _, reference := range analysis.ParameterReferences {
				names = append(names, reference.Name)
			}
			require.Equal(t, testCase.wantParameters, names)
			require.Equal(t, "t", analysis.InsertTable)
		})
	}
}

func TestMapLiteralsOutsideInsertDataAreRefused(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT {'a': 1} AS m",
		"SELECT {id:UInt64} AS id, {'a': 1, 'b': 2} AS m",
		"SELECT id FROM t WHERE m = {'a': 1}",
		"SELECT {} AS m",
		"INSERT INTO t SELECT {'a': 1}",
		"ALTER TABLE t UPDATE m = {'a': 1} WHERE id = {id:UInt64}",
		"SELECT {true: false} AS m",
		"INSERT INTO t SELECT {true: false, false: true}",
		"ALTER TABLE t UPDATE flags = {true: false} WHERE id = {id:UInt64}",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			err := analyseError(t, sql)
			require.ErrorContains(t, err, "map literal")
			require.ErrorContains(t, err, "map(key, value")
		})
	}
}

func TestPlaceholdersInsideMapLiteralsAreRefused(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"INSERT INTO t VALUES ({'a': {p:UInt8}})",
		"INSERT INTO t VALUES ([{'a': 1}, {'b': {p:UInt8}}], 1)",
		"INSERT INTO t VALUES ({{key:String}: 1})",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			err := analyseError(t, sql)
			require.ErrorContains(t, err, "substitutes no placeholder inside it")
			require.ErrorContains(t, err, "{name:Map(K, V)}")
		})
	}
}

func TestMisplacedBracesAreLocatedParseErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		token    string
		message  string
		position int
	}{
		{name: "a map literal in a select list", sql: "SELECT {'a': 1} AS m", token: "{", message: messageMapLiteralOutsideValues, position: 7},
		{name: "a map literal in a condition", sql: "SELECT id FROM t WHERE m = {'a': 1}", token: "{", message: messageMapLiteralOutsideValues, position: 27},
		{name: "a closing brace with no map literal", sql: "SELECT 1 }", token: "}", message: messageUnmatchedBrace, position: 9},
		{
			name:     "a placeholder inside a map literal",
			sql:      "INSERT INTO t (id, m) VALUES (1, {'a': {n:UInt32}})",
			token:    "{n:UInt32}",
			message:  messagePlaceholderInMapLiteral,
			position: 39,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseError(t, testCase.sql)
			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, &dialectkit.ParseError{Token: testCase.token, Message: testCase.message, Position: testCase.position}, parseError)
		})
	}
}
