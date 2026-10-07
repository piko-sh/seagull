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

package harness_test

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/tests/integration/internal/harness"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func TestUniqueNameIsAnIdentifierNoOtherCallReturns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		calls int
	}{
		{name: "one name", calls: 1},
		{name: "several names in one test", calls: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			seen := map[string]bool{}
			for range test.calls {
				name := harness.UniqueName(t)
				require.Regexp(t, identifier, name)
				require.False(t, seen[name], "%s was handed out twice", name)
				seen[name] = true
			}
		})
	}
}

func TestWithDatabaseNamesAnotherDatabaseOnTheSameServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		connection string
		want       string
	}{
		{name: "a MySQL data source name keeps its parameters", connection: "root:test@tcp(localhost:3306)/sgl_test?parseTime=true&multiStatements=true",
			want: "root:test@tcp(localhost:3306)/other?parseTime=true&multiStatements=true"},
		{name: "a MySQL data source name without parameters", connection: "root:test@tcp(localhost:3306)/sgl_test",
			want: "root:test@tcp(localhost:3306)/other"},
		{name: "a ClickHouse URL keeps its parameters", connection: "clickhouse://default:test@localhost:9000/default?dial_timeout=10s",
			want: "clickhouse://default:test@localhost:9000/other?dial_timeout=10s"},
		{name: "a URL without a path gains one", connection: "clickhouse://localhost:9000?dial_timeout=10s",
			want: "clickhouse://localhost:9000/other?dial_timeout=10s"},
		{name: "a data source name without a slash gains one", connection: "root:test@tcp(localhost:3306)",
			want: "root:test@tcp(localhost:3306)/other"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, harness.WithDatabase(test.connection, "other"))
		})
	}
}

func TestCleanupContextOutlivesTheTestContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "a clean-up gets a live context with a deadline"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			t.Cleanup(func() {
				require.Error(t, t.Context().Err(), "the test context ends before clean-ups run")
				ctx, cancel := harness.CleanupContext(t)
				defer cancel()
				assert.NoError(t, ctx.Err())
				_, hasDeadline := ctx.Deadline()
				assert.True(t, hasDeadline)
				cancel()
				assert.ErrorIs(t, context.Cause(ctx), context.Canceled)
			})
		})
	}
}

func TestIsolatedSchemaAndDatabaseCreateWhatTheTestUsesAndDropItAfterwards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		isolate        func(t *testing.T, driver, connection string) (*sql.DB, string)
		name           string
		connection     string
		create         string
		drop           string
		wantConnection string
		refuseDrop     bool
	}{
		{name: "a schema on the search path", isolate: harness.IsolatedSchema, connection: "fake://%[1]s/postgres?sslmode=disable",
			create: "CREATE SCHEMA ", drop: "DROP SCHEMA IF EXISTS %s CASCADE", wantConnection: "fake://%[1]s/postgres?sslmode=disable&search_path=%[2]s"},
		{name: "a database the connection names", isolate: harness.IsolatedDatabase, connection: "fake://%[1]s/default?dial_timeout=10s",
			create: "CREATE DATABASE ", drop: "DROP DATABASE IF EXISTS %s", wantConnection: "fake://%[1]s/%[2]s?dial_timeout=10s"},
		{name: "a drop the server refuses is only logged", isolate: harness.IsolatedDatabase, connection: "fake://%[1]s",
			create: "CREATE DATABASE ", drop: "DROP DATABASE IF EXISTS %s", wantConnection: "fake://%[1]s/%[2]s", refuseDrop: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, serverName := newFakeServer(t)
			var isolatedName string
			t.Run("the test", func(t *testing.T) {
				database, connection := test.isolate(t, fakeDriverName, fmt.Sprintf(test.connection, serverName))
				_, err := database.ExecContext(t.Context(), "SELECT 1")
				require.NoError(t, err)
				statements := server.recorded()
				require.Len(t, statements, 3)
				isolatedName = strings.TrimPrefix(statements[1], test.create)
				require.Regexp(t, identifier, isolatedName)
				require.Equal(t, fmt.Sprintf(test.wantConnection, serverName, isolatedName), connection)
				if test.refuseDrop {
					server.refuseStatements("DROP")
				}
			})
			drop := fmt.Sprintf(test.drop, isolatedName)
			require.Equal(t, []string{drop, test.create + isolatedName, "SELECT 1", drop}, server.recorded())
		})
	}
}
