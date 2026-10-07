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

package cockroachdb_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestCockroachDBRowValueIsARecordThatIsNeverNull(t *testing.T) {
	t.Parallel()

	jsonb := sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb")
	columns := []ast.ScopedColumn{{Name: "id", SQLType: sqlmodel.NewIntegerType("int8", 64, false), Nullable: false}}
	testCases := []struct {
		name     string
		relation dialect.RowRelation
	}{
		{name: "a table", relation: dialect.RowRelation{Name: "harbours", Schema: "public", Columns: columns}},
		{name: "a table on the nullable side of an outer join", relation: dialect.RowRelation{Name: "berths", Schema: "public", Columns: columns, OuterJoined: true}},
		{name: "a derived table", relation: dialect.RowRelation{Columns: columns}},
		{name: "a derived table on the nullable side of an outer join", relation: dialect.RowRelation{Columns: columns, OuterJoined: true}},
		{
			name:     "a function of one value given a column list",
			relation: dialect.RowRelation{Name: "e", Function: true, Columns: []ast.ScopedColumn{{Name: "x", SQLType: jsonb, Nullable: true}}},
		},
		{
			name: "a function of several values",
			relation: dialect.RowRelation{Name: "e", Function: true, Columns: []ast.ScopedColumn{
				{Name: "key", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")},
				{Name: "value", SQLType: jsonb},
			}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			row := cockroachdb.New().RowValueType(testCase.relation)

			require.Equal(t, sqlmodel.TypeCategoryComposite, row.Category)
			require.Equal(t, "record", row.EngineName)
			require.Empty(t, row.Schema)
			require.False(t, row.Nullable)
		})
	}
}
