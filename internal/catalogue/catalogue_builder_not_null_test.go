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

package catalogue

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/namespace"
)

func TestPostgresNamesNotNullConstraints(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine       dialect.DialectPort
		wantNullable map[string]bool
		name         string
		migration    string
		wantError    string
		wantNames    []string
	}{
		{
			name:   "dropping a NOT NULL constraint by its generated or declared name lets the column hold NULL",
			engine: postgres.New(),
			migration: "CREATE TABLE t (id int PRIMARY KEY, a int NOT NULL, b int CONSTRAINT b_must NOT NULL, c int);\n" +
				"ALTER TABLE t DROP CONSTRAINT t_a_not_null;\nALTER TABLE t DROP CONSTRAINT b_must;\n" +
				"ALTER TABLE t ADD CONSTRAINT c_nn NOT NULL c;",
			wantNullable: map[string]bool{"id": false, "a": true, "b": true, "c": false},
			wantNames:    []string{"c_nn", "t_pkey"},
		},
		{
			name:         "a table's NOT NULL constraint keeps its column from holding NULL and takes a generated name without one",
			engine:       postgres.New(),
			migration:    "CREATE TABLE t (a int, b int, NOT NULL a, CONSTRAINT bnn NOT NULL b);\nALTER TABLE t ADD COLUMN c int;\nALTER TABLE t ADD NOT NULL c;",
			wantNullable: map[string]bool{"a": false, "b": false, "c": false},
			wantNames:    []string{"bnn", "t_a_not_null", "t_c_not_null"},
		},
		{
			name:         "a generated name taken by a check is numbered, and dropping the numbered name lets the column hold NULL",
			engine:       postgres.New(),
			migration:    "CREATE TABLE t (a int, CONSTRAINT t_a_not_null CHECK (a > 0));\nALTER TABLE t ALTER a SET NOT NULL;\nALTER TABLE t DROP CONSTRAINT t_a_not_null1;",
			wantNullable: map[string]bool{"a": true},
			wantNames:    []string{"t_a_not_null"},
		},
		{
			name:      "the NOT NULL constraint of a primary key's column cannot be dropped",
			engine:    postgres.New(),
			migration: "CREATE TABLE t (id int PRIMARY KEY);\nALTER TABLE t DROP CONSTRAINT t_id_not_null;",
			wantError: "column id of table t is in a primary key, so its NOT NULL constraint t_id_not_null cannot be dropped",
		},
		{
			name:      "a primary key's column cannot drop NOT NULL",
			engine:    postgres.New(),
			migration: "CREATE TABLE t (id int PRIMARY KEY);\nALTER TABLE t ALTER id DROP NOT NULL;",
			wantError: "column id of table t is in a primary key, so it cannot drop NOT NULL",
		},
		{
			name:      "a column's NOT NULL constraint cannot be added again under another name",
			engine:    postgres.New(),
			migration: "CREATE TABLE t (c int NOT NULL);\nALTER TABLE t ADD CONSTRAINT c2 NOT NULL c;",
			wantError: "column c of table t has the NOT NULL constraint t_c_not_null, so c2 cannot be added",
		},
		{
			name:      "CockroachDB takes no NOT NULL table constraint",
			engine:    cockroachdb.New(),
			migration: "CREATE TABLE t (id int PRIMARY KEY, c int);\nALTER TABLE t ADD CONSTRAINT c_nn NOT NULL c;",
			wantError: "expected a table constraint",
		},
		{
			name:      "a CockroachDB primary key's column cannot drop NOT NULL",
			engine:    cockroachdb.New(),
			migration: "CREATE TABLE t (id int PRIMARY KEY);\nALTER TABLE t ALTER id DROP NOT NULL;",
			wantError: "column id of table t is in a primary key, so it cannot drop NOT NULL",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := newCatalogueBuilder(testCase.engine, namespace.Default())
			diagnostics := applyMigration(t, builder, "migration.sql", testCase.migration, 0)
			if testCase.wantError != "" {
				require.NotEmpty(t, diagnostics)
				require.Contains(t, diagnostics[0].Message, testCase.wantError)
				return
			}
			require.Empty(t, diagnostics)
			table := builder.Catalogue().LookupTable("", "t")
			require.NotNil(t, table)
			for _, column := range table.Columns {
				if want, listed := testCase.wantNullable[column.Name]; listed {
					require.Equal(t, want, column.Nullable, column.Name)
				}
			}
			require.Equal(t, testCase.wantNames, keyNamesOf(table))
		})
	}
}
