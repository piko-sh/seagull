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

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestForeignKeysFollowTheTablesAndColumnsTheyReference(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		migrations  []string
		wantTable   string
		wantColumns []string
	}{
		{
			name: "a referenced table renamed",
			migrations: []string{
				"CREATE TABLE owners (id int PRIMARY KEY);",
				"CREATE TABLE pets (id int PRIMARY KEY, owner int, CONSTRAINT pets_owner FOREIGN KEY (owner) REFERENCES owners (id));",
				"ALTER TABLE owners RENAME TO keepers;",
			},
			wantTable: "keepers", wantColumns: []string{"id"},
		},
		{
			name: "a referenced table moved to another schema",
			migrations: []string{
				"CREATE SCHEMA archive;",
				"CREATE TABLE owners (id int PRIMARY KEY);",
				"CREATE TABLE pets (id int PRIMARY KEY, owner int, CONSTRAINT pets_owner FOREIGN KEY (owner) REFERENCES owners (id));",
				"ALTER TABLE owners SET SCHEMA archive;",
			},
			wantTable: "archive.owners", wantColumns: []string{"id"},
		},
		{
			name: "a referenced column renamed",
			migrations: []string{
				"CREATE TABLE owners (code int PRIMARY KEY);",
				"CREATE TABLE pets (id int PRIMARY KEY, owner int, CONSTRAINT pets_owner FOREIGN KEY (owner) REFERENCES public.owners (code));",
				"ALTER TABLE owners RENAME COLUMN code TO id;",
			},
			wantTable: "public.owners", wantColumns: []string{"id"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, testCase.migrations...)
			require.Empty(t, diagnosticMessages(diagnostics))
			pets := catalogue.LookupTable("", "pets")
			require.NotNil(t, pets)
			require.Equal(t, []string{"pets_pkey", "pets_owner"}, constraintNamesOf(pets.Constraints))
			require.Equal(t, sqlmodel.ConstraintForeignKey, pets.Constraints[1].Kind)
			require.Equal(t, testCase.wantTable, pets.Constraints[1].ForeignTable)
			require.Equal(t, testCase.wantColumns, pets.Constraints[1].ForeignColumns)
		})
	}
}

func TestDroppingAPrimaryKeyConstraintDropsTheKey(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t,
		"CREATE TABLE pets (id int NOT NULL, CONSTRAINT pets_key PRIMARY KEY (id));",
		"ALTER TABLE pets DROP CONSTRAINT pets_key;",
		"CREATE TABLE owners (id int NOT NULL);",
		"ALTER TABLE owners ADD CONSTRAINT owners_key PRIMARY KEY (id);",
		"ALTER TABLE owners RENAME COLUMN id TO code;",
	)
	require.Empty(t, diagnosticMessages(diagnostics))
	pets := catalogue.LookupTable("", "pets")
	require.Empty(t, pets.PrimaryKey)
	require.Empty(t, pets.Constraints)
	owners := catalogue.LookupTable("", "owners")
	require.Equal(t, []string{"code"}, owners.PrimaryKey)
	require.Equal(t, []string{"code"}, owners.Constraints[0].Columns)
}
