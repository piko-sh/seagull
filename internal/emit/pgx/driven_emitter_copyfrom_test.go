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

package pgx_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/emit/pgx"
)

const auditSchema = `CREATE SCHEMA audit;
CREATE TABLE audit.events (
    id BIGINT PRIMARY KEY,
    action TEXT NOT NULL,
    actor TEXT NOT NULL,
    rank INTEGER NOT NULL
);
CREATE TABLE events (
    id BIGINT PRIMARY KEY,
    action TEXT NOT NULL
);
`

func generatePgx(t *testing.T, queries string) (map[string]string, []diag.SourceError) {
	t.Helper()
	generator, err := app.New(postgres.New(), app.WithEmitter(pgx.New()))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      "store",
		Migrations:   fstest.MapFS{"migrations/0001_init.up.sql": {Data: []byte(auditSchema)}, "queries/audit.sql": {Data: []byte(queries)}},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	generated := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		generated[file.Name] = string(file.Content)
	}
	return generated, result.Diagnostics
}

func TestCopyFromWritesTheTargetAndItsColumnsAsTheQueryWritesThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries string
		wants   []string
	}{
		{
			name:    "a schema-qualified table",
			queries: "-- sgl.query(CopyAudit, copyfrom)\nINSERT INTO audit.events (id, action, actor) VALUES ($1, $2, $3);\n",
			wants: []string{
				`queries.db.CopyFrom(ctx, pgx.Identifier{"audit", "events"}, []string{"id", "action", "actor"}`,
				"args[0] = params[i].ID\n\t\targs[1] = params[i].Action\n\t\targs[2] = params[i].Actor\n\t\treturn args, nil",
			},
		},
		{
			name:    "a table on the search path",
			queries: "-- sgl.query(CopyEvents, copyfrom)\nINSERT INTO events (id, action) VALUES ($1, $2);\n",
			wants:   []string{`queries.db.CopyFrom(ctx, pgx.Identifier{"events"}, []string{"id", "action"}`},
		},
		{
			name:    "columns filled by placeholders in another order",
			queries: "-- sgl.query(CopyShuffled, copyfrom)\nINSERT INTO audit.events (actor, action, id) VALUES ($3, $2, $1);\n",
			wants: []string{
				`[]string{"actor", "action", "id"}`,
				"args[0] = params[i].Actor\n\t\targs[1] = params[i].Action\n\t\targs[2] = params[i].ID\n\t\treturn args, nil",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := generatePgx(t, testCase.queries)
			require.Empty(t, diagnostics)
			source := files["audit.sql.go"]
			for _, want := range testCase.wants {
				require.Contains(t, source, want)
			}
		})
	}
}

func TestCopyFromRefusesARowItCannotSendAsColumns(t *testing.T) {
	t.Parallel()

	files, diagnostics := generatePgx(t, "-- sgl.query(CopyRanked, copyfrom)\nINSERT INTO audit.events (id, action, actor, rank) VALUES ($1, $2, $3, $4 + 1);\n")

	require.Empty(t, files)
	require.Len(t, diagnostics, 1)
	require.Equal(t, diag.CodeBatchShape, diagnostics[0].Code)
	require.Equal(t, "audit.sql", diagnostics[0].Filename)
	require.True(t, strings.HasPrefix(diagnostics[0].Message, "command: copyfrom sends each row's values as columns"), diagnostics[0].Message)
}

func TestBatchQueuesTheStatementOncePerRowWithItsArguments(t *testing.T) {
	t.Parallel()

	files, diagnostics := generatePgx(t, "-- sgl.query(UpsertEvents, batch)\n"+
		"INSERT INTO events (id, action) VALUES ($1, lower($2)) ON CONFLICT (id) DO UPDATE SET action = EXCLUDED.action;\n")

	require.Empty(t, diagnostics)
	source := files["audit.sql.go"]
	require.Contains(t, source, "batch.Queue(upsertEventsSQL, item.ID, item.Action)")
	require.Contains(t, source, "results := queries.db.SendBatch(ctx, batch)")
}

func TestCopyFromBindsABinaryOverrideAsTheBytesItMarshalsItselfTo(t *testing.T) {
	t.Parallel()

	generator, err := app.New(postgres.New(), app.WithEmitter(pgx.New()), app.WithTypeOverrides(app.TypeOverride{
		SQLTypeName: "", Column: "tokens.id", GoPackage: "github.com/google/uuid", GoName: "UUID",
	}))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package: "store",
		Migrations: fstest.MapFS{
			"migrations/0001_init.up.sql": {Data: []byte("CREATE TABLE tokens (id bytea PRIMARY KEY, raw bytea, label text);\n")},
			"queries/tokens.sql":          {Data: []byte("-- sgl.query(CopyTokens, copyfrom)\nINSERT INTO tokens (id, raw, label) VALUES ($1, $2, $3);\n")},
		},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	require.Empty(t, result.Diagnostics)
	generated := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		generated[file.Name] = string(file.Content)
	}

	require.Contains(t, generated["tokens.sql.go"], "args[0] = sglDriverBinary{value: params[i].ID}\n\t\targs[1] = params[i].Raw\n\t\targs[2] = params[i].Label\n\t\treturn args, nil")
	require.Contains(t, generated["driver_values.go"], "func (wrapped sglDriverBinary) Value() (driver.Value, error)")
}
