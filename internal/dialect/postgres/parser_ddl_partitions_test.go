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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestAPartitionsElementListDeclaresItsKeyConstraintsAndNotNullColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		sql             string
		wantPrimaryKey  []string
		wantConstraints []sqlmodel.ConstraintKind
		wantNotNull     []string
	}{
		{name: "no element list", sql: "CREATE TABLE r_1 PARTITION OF r FOR VALUES IN (1)"},
		{
			name:            "columns with options and a constraint",
			sql:             "CREATE TABLE r_1 PARTITION OF app.public.r (note WITH OPTIONS NOT NULL DEFAULT 'x', value NOT NULL, CONSTRAINT positive CHECK (value > 0)) FOR VALUES IN (1)",
			wantConstraints: []sqlmodel.ConstraintKind{sqlmodel.ConstraintCheck},
			wantNotNull:     []string{"note", "value"},
		},
		{
			name:           "a table primary key",
			sql:            "CREATE TABLE r_1 PARTITION OF r (PRIMARY KEY (id, kind)) FOR VALUES IN (1)",
			wantPrimaryKey: []string{"id", "kind"},
			wantNotNull:    []string{"id", "kind"},
		},
		{
			name:           "a column primary key",
			sql:            "CREATE TABLE r_1 PARTITION OF r (id PRIMARY KEY) DEFAULT",
			wantPrimaryKey: []string{"id"},
			wantNotNull:    []string{"id"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation)
			require.Equal(t, ast.MutationCreateTable, mutation.Kind)
			require.Equal(t, &ast.TableReference{Schema: mutation.PartitionOf.Schema, Name: "r", Alias: ""}, mutation.PartitionOf)
			require.Equal(t, testCase.wantPrimaryKey, mutation.PrimaryKey)
			var kinds []sqlmodel.ConstraintKind
			for _, constraint := range mutation.Constraints {
				kinds = append(kinds, constraint.Kind)
			}
			require.Equal(t, testCase.wantConstraints, kinds)
			var notNull []string
			for _, additional := range mutation.AdditionalMutations {
				require.True(t, additional.SetsNotNull)
				notNull = append(notNull, additional.ColumnName)
			}
			require.Equal(t, testCase.wantNotNull, notNull)
		})
	}
}

func TestAMalformedPartitionElementListIsAnError(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE TABLE r_1 PARTITION OF r (note NOT NULL FOR VALUES IN (1)",
		"CREATE TABLE r_1 PARTITION OF r (CONSTRAINT) FOR VALUES IN (1)",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.Error(t, applyDDLExpectError(t, sql))
		})
	}
}
