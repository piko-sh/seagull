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

package cli_test

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/internal/clitest"
	"seagull.sh/seagull/internal/testkit/fuzzcli"
)

const seededTable = "m1"

type sqliteWorkspace struct {
	t       *testing.T
	project *clitest.Project
	dsn     string
}

func (workspace *sqliteWorkspace) Write(name, content string) {
	workspace.project.Write(name, content)
}

func (workspace *sqliteWorkspace) Read(name string) string {
	return workspace.project.Read(name)
}

func (workspace *sqliteWorkspace) Remove(name string) {
	err := os.Remove(workspace.project.Path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	require.NoError(workspace.t, err)
}

func (workspace *sqliteWorkspace) Run(arguments ...string) fuzzcli.Result {
	result := workspace.project.Run(append(arguments, "--dsn", workspace.dsn)...)
	return fuzzcli.Result{Stdout: result.Stdout, Stderr: result.Stderr, Code: result.Code}
}

func FuzzCLIMigrate(f *testing.F) {
	for _, seed := range fuzzcli.MigrateSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		project, _ := databaseProject(t)

		dsn := project.Path("app.db")
		workspace := &sqliteWorkspace{t: t, project: project, dsn: dsn}
		fuzzcli.RunMigrate(t, data, workspace, fuzzcli.DialectFor("sqlite"), func() fuzzcli.Observation {
			return observeSQLite(t, dsn)
		})
	})
}

func observeSQLite(t *testing.T, dsn string) fuzzcli.Observation {
	t.Helper()
	database, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	rows, err := database.QueryContext(t.Context(), "SELECT name FROM sqlite_master WHERE type = 'table' AND name GLOB 'm[0-9]*'")
	require.NoError(t, err)
	observed := fuzzcli.Observation{Tables: nil, SeedRows: 0}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		observed.Tables = append(observed.Tables, name)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	if !slices.Contains(observed.Tables, seededTable) {
		return observed
	}
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT count(*) FROM "+seededTable).Scan(&observed.SeedRows))
	return observed
}
