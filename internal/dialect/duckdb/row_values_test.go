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

package duckdb_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestRowValueTypeIsAStructOfTheRelationsColumns(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("integer", 32, false)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	relation := dialect.RowRelation{Name: "harbours", Schema: "main", Columns: []ast.ScopedColumn{
		{Name: "id", SQLType: integer, Nullable: false},
		{Name: "name", SQLType: text, Nullable: true},
	}}

	row := duckdb.New().RowValueType(relation)

	require.Equal(t, sqlmodel.TypeCategoryStruct, row.Category)
	require.Len(t, row.StructFields, 2)
	require.Equal(t, "id", row.StructFields[0].Name)
	require.Equal(t, sqlmodel.TypeCategoryInteger, row.StructFields[0].SQLType.Category)
	require.True(t, row.StructFields[0].SQLType.Nullable)
	require.Equal(t, "name", row.StructFields[1].Name)
	require.Equal(t, sqlmodel.TypeCategoryText, row.StructFields[1].SQLType.Category)
}

func TestRowValueTypeOfARelationWithoutColumnsIsAnEmptyStruct(t *testing.T) {
	t.Parallel()

	row := duckdb.New().RowValueType(dialect.RowRelation{Name: "", Schema: "", Columns: nil})

	require.Equal(t, sqlmodel.TypeCategoryStruct, row.Category)
	require.Empty(t, row.StructFields)
}

func TestRowValueTypeIsNeverNullOnTheNullableSideOfAnOuterJoin(t *testing.T) {
	t.Parallel()

	row := duckdb.New().RowValueType(dialect.RowRelation{OuterJoined: true, Columns: []ast.ScopedColumn{
		{Name: "skipper", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), Nullable: true},
	}})

	require.False(t, row.Nullable)
	require.True(t, row.StructFields[0].SQLType.Nullable)
}

func TestRowValueTypeOfAWideRelationCostsLinearTimeInItsColumns(t *testing.T) {
	t.Parallel()

	engine := duckdb.New()
	describe := func(columns int) func() {
		relation := dialect.RowRelation{Name: "wide", Schema: "main", Columns: make([]ast.ScopedColumn, columns)}
		for index := range relation.Columns {
			relation.Columns[index] = ast.ScopedColumn{Name: "c" + strconv.Itoa(index),
				SQLType: sqlmodel.NewIntegerType("integer", 32, false), Nullable: false}
		}
		return func() {
			row := engine.RowValueType(relation)
			require.Len(t, row.StructFields, columns)
		}
	}

	scaling.Linear(t, describe(4000), describe(16000))
}
