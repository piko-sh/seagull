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

package cli

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/sgl/internal/output"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

func TestLiveCatalogueReadsThroughTheOpenConnection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cancelled bool
		wantTable bool
	}{
		{name: "the connection already open", cancelled: false, wantTable: true},
		{name: "a cancelled run", cancelled: true, wantTable: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "app.db"))
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, database.Close()) })
			_, err = database.ExecContext(t.Context(), "CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")
			require.NoError(t, err)
			configured := &config.Database{Name: "app", Dialect: "sqlite"}
			opened := &connection{
				dialect: dialect_engine_sqlite.New(), database: database, configured: configured, backends: nil,
				loaded: &project{assembled: nil, config: nil, path: "sgl.yaml", databases: []*config.Database{configured}},
				dsn:    "file:/never/opened/twice.db?_pragma=busy_timeout(1)",
			}
			var stderr bytes.Buffer
			inv := &invocation{streams: output.IO{Stdin: nil, Stdout: io.Discard, Stderr: &stderr}, directory: t.TempDir()}
			ctx, cancel := context.WithCancel(t.Context())
			if testCase.cancelled {
				cancel()
			}
			defer cancel()

			catalogue, err := inv.liveCatalogue(ctx, opened)

			if !testCase.wantTable {
				require.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, exitInterrupted, inv.fail(err), "an interrupted introspection is not a database failure")
				return
			}
			require.NoError(t, err)
			assert.Contains(t, dumpCatalogue("app", catalogue).Tables[0].Name, "authors")
		})
	}
}
