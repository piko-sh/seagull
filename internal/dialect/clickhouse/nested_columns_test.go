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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

type describedColumn struct {
	name     string
	spelling string
	hidden   bool
	nullable bool
}

func fullTypeSpelling(sqlType sqlmodel.SQLType) string {
	switch {
	case sqlType.Category == sqlmodel.TypeCategoryArray && sqlType.ElementType != nil:
		return "Array(" + nullableTypeSpelling(*sqlType.ElementType) + ")"
	case sqlType.Category == sqlmodel.TypeCategoryStruct:
		fields := make([]string, 0, len(sqlType.StructFields))
		for _, field := range sqlType.StructFields {
			fields = append(fields, field.Name+" "+nullableTypeSpelling(field.SQLType))
		}
		return "Tuple(" + strings.Join(fields, ", ") + ")"
	default:
		return sqlType.EngineName
	}
}

func nullableTypeSpelling(sqlType sqlmodel.SQLType) string {
	if sqlType.Nullable {
		return "Nullable(" + fullTypeSpelling(sqlType) + ")"
	}
	return fullTypeSpelling(sqlType)
}

func describeColumns(columns []sqlmodel.Column) []describedColumn {
	described := make([]describedColumn, 0, len(columns))
	for index := range columns {
		described = append(described, describedColumn{
			name:     columns[index].Name,
			spelling: fullTypeSpelling(columns[index].SQLType),
			hidden:   columns[index].Hidden,
			nullable: columns[index].Nullable,
		})
	}
	return described
}

func TestNestedColumnIsStoredAsOneArrayPerField(t *testing.T) {
	t.Parallel()

	wantLegs := []describedColumn{
		{name: "legs", spelling: "Array(Tuple(colony String, minutes Nullable(UInt16)))", hidden: true},
		{name: "legs.colony", spelling: "Array(String)"},
		{name: "legs.minutes", spelling: "Array(Nullable(UInt16))"},
	}

	testCases := []struct {
		name string
		sql  string
		want []describedColumn
	}{
		{
			name: "in CREATE TABLE",
			sql:  "CREATE TABLE flights (id UInt64, legs Nested(colony String, minutes Nullable(UInt16)), note String) ENGINE = MergeTree ORDER BY id",
			want: append(append([]describedColumn{{name: "id", spelling: "UInt64"}}, wantLegs...), describedColumn{name: "note", spelling: "String"}),
		},
		{
			name: "spelt in lower case",
			sql:  "CREATE TABLE flights (legs nested(colony String, minutes Nullable(UInt16))) ENGINE = Memory",
			want: wantLegs,
		},
		{
			name: "in ALTER TABLE ADD COLUMN",
			sql:  "ALTER TABLE flights ADD COLUMN IF NOT EXISTS legs Nested(colony String, minutes Nullable(UInt16)) AFTER id",
			want: wantLegs,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.NoError(t, err)
			require.Equal(t, testCase.want, describeColumns(mutation.Columns))
		})
	}
}

func TestColumnDDLReadsTheDottedNamesOfNestedFields(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantKind     ast.MutationKind
		wantColumn   string
		wantNewName  string
		wantPrefix   string
		wantAdded    []describedColumn
		wantModified string
	}{
		{
			name: "adding a field", sql: "ALTER TABLE flights ADD COLUMN legs.altitude Array(UInt32)",
			wantKind: ast.MutationAlterTableAddColumn, wantAdded: []describedColumn{{name: "legs.altitude", spelling: "Array(UInt32)"}},
		},
		{
			name: "renaming a quoted field", sql: "ALTER TABLE flights RENAME COLUMN `legs.colony` TO `legs.site`",
			wantKind: ast.MutationAlterTableRenameColumn, wantColumn: "legs.colony", wantNewName: "legs.site",
		},
		{
			name: "renaming a field", sql: "ALTER TABLE flights RENAME COLUMN legs.colony TO legs.site",
			wantKind: ast.MutationAlterTableRenameColumn, wantColumn: "legs.colony", wantNewName: "legs.site",
		},
		{
			name: "dropping a field", sql: "ALTER TABLE flights DROP COLUMN IF EXISTS legs.minutes",
			wantKind: ast.MutationAlterTableDropColumn, wantColumn: "legs.minutes", wantPrefix: "legs.minutes.",
		},
		{
			name: "dropping a whole Nested column", sql: "ALTER TABLE flights DROP COLUMN legs",
			wantKind: ast.MutationAlterTableDropColumn, wantColumn: "legs", wantPrefix: "legs.",
		},
		{
			name: "changing the type of a field", sql: "ALTER TABLE flights MODIFY COLUMN legs.minutes Array(UInt32)",
			wantKind: ast.MutationAlterTableAlterColumn, wantColumn: "legs.minutes", wantModified: "Array(UInt32)",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.NoError(t, err)
			require.Equal(t, testCase.wantKind, mutation.Kind)
			require.Equal(t, testCase.wantColumn, mutation.ColumnName)
			require.Equal(t, testCase.wantNewName, mutation.NewName)
			require.Equal(t, testCase.wantPrefix, mutation.DroppedColumnPrefix)
			if testCase.wantAdded != nil {
				require.Equal(t, testCase.wantAdded, describeColumns(mutation.Columns))
			}
			if testCase.wantModified != "" {
				require.Len(t, mutation.Columns, 1)
				require.Equal(t, testCase.wantModified, fullTypeSpelling(mutation.Columns[0].SQLType))
			}
		})
	}
}
