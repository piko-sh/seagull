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

package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestRowValueTypeIsTheCompositeTypeOfTheRelation(t *testing.T) {
	t.Parallel()

	columns := []ast.ScopedColumn{{Name: "id", SQLType: sqlmodel.NewIntegerType("int4", 32, false), Nullable: false}}
	testCases := []struct {
		name       string
		relation   dialect.RowRelation
		wantName   string
		wantSchema string
	}{
		{
			name:       "a table's row is the composite type named after the table",
			relation:   dialect.RowRelation{Name: "harbours", Schema: "public", Columns: columns},
			wantName:   "harbours",
			wantSchema: "public",
		},
		{
			name:       "a relation no table names is a record",
			relation:   dialect.RowRelation{Name: "", Schema: "", Columns: columns},
			wantName:   "record",
			wantSchema: "",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			row := postgres.New().RowValueType(testCase.relation)

			require.Equal(t, sqlmodel.TypeCategoryComposite, row.Category)
			require.Equal(t, testCase.wantName, row.EngineName)
			require.Equal(t, testCase.wantSchema, row.Schema)
		})
	}
}

func TestRowValueTypeOfAFunctionOfOneValueIsTheValue(t *testing.T) {
	t.Parallel()

	jsonb := sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb")
	testCases := []struct {
		name         string
		relation     dialect.RowRelation
		wantCategory sqlmodel.SQLTypeCategory
		wantNullable bool
	}{
		{
			name:         "a function of one value",
			relation:     dialect.RowRelation{Function: true, Columns: []ast.ScopedColumn{{Name: "value", SQLType: jsonb, Nullable: true}}},
			wantCategory: sqlmodel.TypeCategoryJSON,
			wantNullable: true,
		},
		{
			name: "a function of several values is a record",
			relation: dialect.RowRelation{Function: true, Columns: []ast.ScopedColumn{
				{Name: "key", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")},
				{Name: "value", SQLType: jsonb},
			}},
			wantCategory: sqlmodel.TypeCategoryComposite,
			wantNullable: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			row := postgres.New().RowValueType(testCase.relation)

			require.Equal(t, testCase.wantCategory, row.Category)
			require.Equal(t, testCase.wantNullable, row.Nullable)
		})
	}
}

func TestRowValueTypeOnTheNullableSideOfAnOuterJoinMayBeNull(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		relation dialect.RowRelation
		want     bool
	}{
		{name: "a table joined as the nullable side", relation: dialect.RowRelation{Name: "berths", Schema: "public", OuterJoined: true}, want: true},
		{name: "a table joined otherwise", relation: dialect.RowRelation{Name: "berths", Schema: "public"}, want: false},
		{name: "a derived table joined as the nullable side", relation: dialect.RowRelation{OuterJoined: true}, want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, postgres.New().RowValueType(testCase.relation).Nullable)
		})
	}
}

func TestRowValueTypeHookTypesTheRowsItAnswers(t *testing.T) {
	t.Parallel()

	record := new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, "record"))
	jsonb := sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb")
	engine := postgres.New(postgres.WithRowValueTypeHook(func(relation dialect.RowRelation) *sqlmodel.SQLType {
		if relation.Function {
			return nil
		}
		return record
	}))
	testCases := []struct {
		name         string
		relation     dialect.RowRelation
		wantCategory sqlmodel.SQLTypeCategory
		wantName     string
		wantNullable bool
	}{
		{
			name:         "a relation the hook answers takes the hook's type",
			relation:     dialect.RowRelation{Name: "berths", Schema: "public", OuterJoined: true},
			wantCategory: sqlmodel.TypeCategoryComposite,
			wantName:     "record",
			wantNullable: false,
		},
		{
			name:         "a relation the hook passes over falls through to PostgreSQL's rule",
			relation:     dialect.RowRelation{Function: true, Columns: []ast.ScopedColumn{{Name: "value", SQLType: jsonb, Nullable: true}}},
			wantCategory: sqlmodel.TypeCategoryJSON,
			wantName:     "jsonb",
			wantNullable: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			row := engine.RowValueType(testCase.relation)

			require.Equal(t, testCase.wantCategory, row.Category)
			require.Equal(t, testCase.wantName, row.EngineName)
			require.Equal(t, testCase.wantNullable, row.Nullable)
		})
	}
}
