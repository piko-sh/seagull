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

package sqlexec

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errNoCreatePrivilege = errors.New("permission denied to create")

type privilegeRecorder struct {
	readable string
	executed []string
	mutex    sync.Mutex
}

type privilegeConnector struct{ recorder *privilegeRecorder }

type privilegeConn struct{ recorder *privilegeRecorder }

type privilegeRows struct{}

func (connector privilegeConnector) Connect(context.Context) (driver.Conn, error) {
	return privilegeConn(connector), nil
}

func (privilegeConnector) Driver() driver.Driver { return nil }

func (privilegeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not prepared") }

func (privilegeConn) Close() error { return nil }

func (privilegeConn) Begin() (driver.Tx, error) { return nil, errors.New("no transactions") }

func (connection privilegeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if query == "SELECT 1 FROM "+connection.recorder.readable+" WHERE 1 = 0" {
		return privilegeRows{}, nil
	}
	return nil, errors.New("no such table")
}

func (connection privilegeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	connection.recorder.mutex.Lock()
	defer connection.recorder.mutex.Unlock()
	connection.recorder.executed = append(connection.recorder.executed, query)
	return nil, errNoCreatePrivilege
}

func (privilegeRows) Columns() []string { return []string{"1"} }

func (privilegeRows) Close() error { return nil }

func (privilegeRows) Next([]driver.Value) error { return io.EOF }

func TestEnsureTableLeavesATableItCanReadUncreated(t *testing.T) {
	t.Parallel()

	recorder := &privilegeRecorder{readable: "sgl_migrations"}
	database := sql.OpenDB(privilegeConnector{recorder: recorder})
	t.Cleanup(func() { _ = database.Close() })

	err := ensureTable(t.Context(), database, "sgl_migrations", "CREATE TABLE IF NOT EXISTS sgl_migrations (version BIGINT)")

	require.NoError(t, err, "a user who may read the table but not create one is not refused")
	assert.Empty(t, recorder.executed)
}

func TestEnsureTableCreatesATableItCannotRead(t *testing.T) {
	t.Parallel()

	recorder := &privilegeRecorder{readable: "sgl_migrations"}
	database := sql.OpenDB(privilegeConnector{recorder: recorder})
	t.Cleanup(func() { _ = database.Close() })

	err := ensureTable(t.Context(), database, "sgl_seeds", "CREATE TABLE IF NOT EXISTS sgl_seeds (name TEXT)")

	require.ErrorIs(t, err, errNoCreatePrivilege)
	require.Len(t, recorder.executed, 1)
	assert.True(t, strings.HasPrefix(recorder.executed[0], "CREATE TABLE IF NOT EXISTS sgl_seeds"))
}
