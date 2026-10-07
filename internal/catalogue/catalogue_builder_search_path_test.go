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

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/namespace"
)

func TestTheSearchPathAMigrationSetsDecidesWhereItsObjectsGo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		migrations []string
		wantTables map[string][]string
	}{
		{
			name:       "a table created after the path is set",
			migrations: []string{"CREATE SCHEMA shop;\nSET search_path TO shop;\nCREATE TABLE accounts (id integer PRIMARY KEY);"},
			wantTables: map[string][]string{"shop": {"accounts"}, "public": nil},
		},
		{
			name: "the path lasts for the later migrations",
			migrations: []string{
				"CREATE SCHEMA shop;\nSET search_path TO shop, public;",
				"CREATE TABLE accounts (id integer PRIMARY KEY);\nALTER TABLE accounts ADD COLUMN email text;",
			},
			wantTables: map[string][]string{"shop": {"accounts"}, "public": nil},
		},
		{
			name: "a local path lasts for its migration alone",
			migrations: []string{
				"CREATE SCHEMA shop;\nSET LOCAL search_path TO shop;\nCREATE TABLE accounts (id integer);",
				"CREATE TABLE orders (id integer);",
			},
			wantTables: map[string][]string{"shop": {"accounts"}, "public": {"orders"}},
		},
		{
			name: "a reset restores the path the migrations started with",
			migrations: []string{
				"CREATE SCHEMA shop;\nSET search_path TO shop;\nCREATE TABLE accounts (id integer);\nRESET search_path;\nCREATE TABLE orders (id integer);",
			},
			wantTables: map[string][]string{"shop": {"accounts"}, "public": {"orders"}},
		},
		{
			name: "objects go to the first schema on the path that exists",
			migrations: []string{
				"CREATE SCHEMA stock;\nSET search_path TO missing, stock;\nCREATE TABLE items (id integer);\n" +
					"CREATE SCHEMA missing;\nCREATE TABLE orders (id integer);",
			},
			wantTables: map[string][]string{"stock": {"items"}, "missing": {"orders"}, "public": nil},
		},
		{
			name: "a schema made outside the migrations takes the objects when none on the path exists",
			migrations: []string{
				"SET search_path TO addresses;\nCREATE TABLE addresses (id integer);\nCREATE INDEX addresses_id ON addresses (id);",
			},
			wantTables: map[string][]string{"addresses": {"addresses"}, "public": nil},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, testCase.migrations...)

			require.Empty(t, diagnosticMessages(diagnostics))
			for schemaName, wantTables := range testCase.wantTables {
				require.Equal(t, wantTables, schemaTableNames(catalogue, schemaName), schemaName)
			}
		})
	}
}

func TestUnqualifiedNamesInAMigrationFollowTheSearchPathItSets(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, `
CREATE SCHEMA shop;
CREATE TABLE public.accounts (id integer PRIMARY KEY);
SET search_path TO shop, public;
CREATE TABLE orders (id integer PRIMARY KEY, account_id integer REFERENCES accounts (id));
CREATE VIEW order_accounts AS SELECT orders.id, accounts.id AS account FROM orders JOIN accounts ON accounts.id = orders.account_id;
ALTER TABLE accounts ADD COLUMN email text;
`)

	require.Empty(t, diagnosticMessages(diagnostics))
	require.Equal(t, []string{"orders"}, schemaTableNames(catalogue, "shop"))
	require.NotNil(t, catalogue.LookupView("shop", "order_accounts"))
	require.Equal(t, []string{"id", "email"}, columnNamesOf(catalogue.LookupTable("public", "accounts").Columns))
}

func TestTheCatalogueKeepsTheSearchPathTheMigrationsStartedWith(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, "CREATE SCHEMA shop;\nSET search_path TO shop;\nCREATE TABLE accounts (id integer);")

	require.Empty(t, diagnosticMessages(diagnostics))
	require.Equal(t, []string{"public"}, catalogue.SearchPath)
	require.Equal(t, "public", catalogue.DefaultSchema)
	require.Nil(t, catalogue.LookupTable("", "accounts"))
	require.NotNil(t, catalogue.LookupTable("shop", "accounts"))
}

func TestTheConfiguredSearchPathStartsTheMigrations(t *testing.T) {
	t.Parallel()

	builder := newCatalogueBuilder(postgres.New(postgres.WithSearchPath("auth", "public")), namespace.Default())
	diagnostics := applyMigration(t, builder, "001.sql",
		"CREATE SCHEMA IF NOT EXISTS auth;\nCREATE TABLE account (id integer PRIMARY KEY);\nCREATE TABLE public.audit (id integer);", 0)
	diagnostics = append(diagnostics, applyMigration(t, builder, "002.sql",
		"SET search_path TO public;\nCREATE TABLE notes (id integer);\nALTER TABLE audit ADD COLUMN note text;", 1)...)
	catalogue := builder.Catalogue()

	require.Empty(t, diagnosticMessages(diagnostics))
	require.Equal(t, []string{"account"}, schemaTableNames(catalogue, "auth"))
	require.Equal(t, []string{"audit", "notes"}, schemaTableNames(catalogue, "public"))
	require.Equal(t, []string{"auth", "public"}, catalogue.SearchPath)
	require.NotNil(t, catalogue.LookupTable("", "account"))
}

func TestAnUnqualifiedChangeFindsItsObjectAlongTheSearchPath(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, `
CREATE SCHEMA shop;
CREATE TABLE accounts (id integer PRIMARY KEY);
CREATE TABLE stale (id integer);
CREATE VIEW account_ids AS SELECT id FROM accounts;
CREATE TYPE mood AS ENUM ('calm');
CREATE SEQUENCE counter;
CREATE FUNCTION answer() RETURNS integer LANGUAGE sql AS 'SELECT 42';
SET search_path TO shop, public;
ALTER TABLE accounts RENAME TO users;
ALTER TABLE users SET SCHEMA shop;
DROP TABLE stale;
DROP VIEW account_ids;
DROP TYPE mood;
ALTER SEQUENCE counter RENAME TO numbers;
DROP FUNCTION answer();
`)

	require.Empty(t, diagnosticMessages(diagnostics))
	require.Equal(t, []string{"users"}, schemaTableNames(catalogue, "shop"))
	require.Nil(t, schemaTableNames(catalogue, "public"))
	public := catalogue.Schemas["public"]
	require.Empty(t, public.Views)
	require.Empty(t, public.Enums)
	require.Empty(t, public.Functions)
	require.NotNil(t, public.Sequences["numbers"])
	require.Nil(t, public.Sequences["counter"])
}

func TestASequenceIsRenamedAndMovedAsARelation(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, `
CREATE SCHEMA archive;
CREATE SEQUENCE counter;
ALTER SEQUENCE counter RENAME TO numbers;
ALTER SEQUENCE IF EXISTS missing RENAME TO other;
ALTER SEQUENCE numbers INCREMENT BY 2 RESTART;
ALTER TABLE numbers RENAME TO tallies;
ALTER SEQUENCE tallies SET SCHEMA archive;
CREATE SEQUENCE spare;
ALTER TABLE spare SET SCHEMA archive;
`)

	require.Empty(t, diagnosticMessages(diagnostics))
	require.Empty(t, catalogue.Schemas["public"].Sequences)
	tallies := catalogue.LookupSequence("archive", "tallies")
	require.NotNil(t, tallies)
	require.Equal(t, "archive", tallies.Schema)
	require.Len(t, tallies.Columns, 3)
	require.NotNil(t, catalogue.LookupSequence("archive", "spare"))
}

func TestDroppingAnObjectThatIsMissingCreatesNoSchema(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		statement string
	}{
		{name: "a table", statement: "DROP TABLE nowhere.items;"},
		{name: "a view", statement: "DROP VIEW nowhere.recent;"},
		{name: "a sequence", statement: "DROP SEQUENCE nowhere.counter;"},
		{name: "a type", statement: "DROP TYPE nowhere.mood;"},
		{name: "a function", statement: "DROP FUNCTION nowhere.touch();"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, "CREATE TABLE items (id int);\n"+testCase.statement+
				"\nCREATE TABLE notes (id int);")

			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeUnknownObject, diagnostics[0].Code)
			require.NotContains(t, catalogue.Schemas, "nowhere")
			require.Equal(t, []string{"items", "notes"}, schemaTableNames(catalogue, "public"))
		})
	}
}
