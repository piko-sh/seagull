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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestApplyDDLRequiresWhatACompoundTypeNeeds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "a struct field with no type", sql: "CREATE TABLE t (a STRUCT(x, y INTEGER))", wantMessage: "expected a type"},
		{name: "a struct with no fields", sql: "CREATE TABLE t (a STRUCT())", wantMessage: "STRUCT needs at least one entry"},
		{name: "struct fields with no comma", sql: "CREATE TABLE t (a STRUCT(x INTEGER y VARCHAR))",
			wantMessage: "expected ')' to close STRUCT"},
		{name: "a map with no value type", sql: "CREATE TABLE t (a MAP(INTEGER))",
			wantMessage: "expected ',' between the MAP's key and value types"},
		{name: "a map whose value type is missing", sql: "CREATE TABLE t (a MAP(INTEGER, ))", wantMessage: "expected a type"},
		{name: "a map with three types", sql: "CREATE TABLE t (a MAP(INTEGER, VARCHAR, BOOLEAN))",
			wantMessage: "expected ')' to close MAP"},
		{name: "a union member with no type", sql: "CREATE TABLE t (a UNION(x))", wantMessage: "expected a type"},
		{name: "a union with no members", sql: "CREATE TABLE t (a UNION())", wantMessage: "UNION needs at least one entry"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := applyDDLError(t, testCase.sql)

			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestApplyDDLReadsWellFormedCompoundTypes(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE TABLE t (a STRUCT(x INTEGER, y VARCHAR[]), b MAP(VARCHAR, INTEGER), c UNION(n INTEGER, s VARCHAR))")

	require.Len(t, mutation.Columns, 3)
	assert.Equal(t, sqlmodel.TypeCategoryStruct, mutation.Columns[0].SQLType.Category)
	assert.Len(t, mutation.Columns[0].SQLType.StructFields, 2)
	assert.Equal(t, sqlmodel.TypeCategoryMap, mutation.Columns[1].SQLType.Category)
	require.NotNil(t, mutation.Columns[1].SQLType.ElementType)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[1].SQLType.ElementType.Category)
	assert.Equal(t, sqlmodel.TypeCategoryUnion, mutation.Columns[2].SQLType.Category)
	assert.Len(t, mutation.Columns[2].SQLType.UnionMembers, 2)
}
