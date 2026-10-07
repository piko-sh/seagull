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

package duckdb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestParametersComparedWithBuiltinCallsTakeTheCallType(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT name FROM items WHERE length(name) > $1 AND lower(email) = $2 AND my_macro(code) = $3")

	references := make(map[int]*ast.RawParameterReference, len(analysis.ParameterReferences))
	for index := range analysis.ParameterReferences {
		references[analysis.ParameterReferences[index].Number] = &analysis.ParameterReferences[index]
	}

	require.NotNil(t, references[1].CastType)
	require.Equal(t, sqlmodel.TypeCategoryInteger, references[1].CastType.Category)
	require.Equal(t, "name", references[1].ColumnReference.ColumnName)
	require.NotNil(t, references[2].CastType)
	require.Equal(t, sqlmodel.TypeCategoryText, references[2].CastType.Category)
	require.Nil(t, references[3].CastType)
	require.Equal(t, "code", references[3].ColumnReference.ColumnName)
}

func TestAParameterComparedWithAStructFieldIsComparedWithNoColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		sql    string
		column string
	}{
		{name: "a field struct_extract reads", sql: "SELECT id FROM boats WHERE struct_extract(berth, 'pontoon') = $1", column: ""},
		{name: "a field struct_extract reads, under IS", sql: "SELECT id FROM boats WHERE struct_extract(berth, 'pontoon') IS $1", column: ""},
		{name: "a column a macro takes", sql: "SELECT id FROM boats WHERE my_macro(berth) = $1", column: "berth"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nestedTypeCatalogue(), testCase.sql)

			require.Len(t, analysis.ParameterReferences, 1)
			column := ""
			if reference := analysis.ParameterReferences[0].ColumnReference; reference != nil {
				column = reference.ColumnName
			}
			require.Equal(t, testCase.column, column)
		})
	}
}

func describeDeclaredParameters(references []ast.RawParameterReference) []string {
	described := make([]string, 0, len(references))
	for _, reference := range references {
		column, declared := "-", "-"
		if reference.ColumnReference != nil {
			column = reference.ColumnReference.TableAlias + "." + reference.ColumnReference.ColumnName
		}
		if reference.CastType != nil {
			declared = reference.CastType.EngineName
		}
		described = append(described, fmt.Sprintf("%d %s %s %s", reference.Number, reference.Context, column, declared))
	}
	return described
}

func TestAParameterCastAloneDeclaresTheCastTypeWhereItIsAssigned(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a value of an INSERT cast to a type",
			sql:      "INSERT INTO events (id, token) VALUES ($1, CAST($2 AS UUID))",
			expected: []string{"1 assignment events.id -", "2 assignment events.token uuid"},
		},
		{
			name:     "a parenthesised value of an INSERT cast to a type",
			sql:      "INSERT INTO events (token) VALUES ((CAST($1 AS UUID)))",
			expected: []string{"1 assignment events.token uuid"},
		},
		{
			name:     "a value an UPDATE sets cast to a type",
			sql:      "UPDATE events SET token = CAST($1 AS UUID) WHERE id = $2",
			expected: []string{"1 assignment events.token uuid", "2 comparison .id -"},
		},
		{
			name:     "a parameter inside an expression a CAST converts",
			sql:      "UPDATE events SET token = CAST($1 || '' AS UUID)",
			expected: []string{"1 assignment events.token -"},
		},
		{
			name:     "a parameter cast inline inside a CAST",
			sql:      "INSERT INTO events (token) VALUES (CAST($1::VARCHAR AS UUID))",
			expected: []string{"1 assignment events.token varchar"},
		},
		{
			name:     "a parameter cast inline in the select list of an INSERT",
			sql:      "INSERT INTO events (id, token) SELECT $1, $2::UUID",
			expected: []string{"1 assignment events.id -", "2 assignment events.token uuid"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			require.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}
