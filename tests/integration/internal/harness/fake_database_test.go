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
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const (
	fakeDriverName  = "harness-fake"
	schemeSeparator = "://"
)

var (
	errRefused         = errors.New("the fake server refused the statement")
	errNoFakeServer    = errors.New("no fake server has that name")
	errNotSupported    = errors.New("the fake connection does not support this")
	fakeServers        sync.Map
	fakeServerCount    atomic.Uint64
	registerFakeDriver = sync.OnceFunc(func() { sql.Register(fakeDriverName, fakeDriver{}) })
)

type fakeServer struct {
	results    map[string][][]driver.Value
	refuse     string
	statements []string
	mu         sync.Mutex
}

type fakeDriver struct{}

type fakeConnection struct {
	server *fakeServer
}

type fakeRows struct {
	rows  [][]driver.Value
	width int
}

func (server *fakeServer) record(statement string) error {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.statements = append(server.statements, statement)
	if server.refuse != "" && strings.Contains(statement, server.refuse) {
		return fmt.Errorf("%w: %s", errRefused, statement)
	}
	return nil
}

func (server *fakeServer) recorded() []string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return slices.Clone(server.statements)
}

func (server *fakeServer) answer(query string, rows ...[]driver.Value) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.results[query] = rows
}

func (server *fakeServer) refuseStatements(text string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.refuse = text
}

func (server *fakeServer) rowsFor(query string) *fakeRows {
	server.mu.Lock()
	defer server.mu.Unlock()
	rows := slices.Clone(server.results[query])
	width := 0
	if len(rows) > 0 {
		width = len(rows[0])
	}
	return &fakeRows{rows: rows, width: width}
}

func (fakeDriver) Open(name string) (driver.Conn, error) {
	host := name
	if _, afterScheme, found := strings.Cut(host, schemeSeparator); found {
		host = afterScheme
	}
	host, _, _ = strings.Cut(host, "/")
	host, _, _ = strings.Cut(host, "?")
	server, found := fakeServers.Load(host)
	if !found {
		return nil, fmt.Errorf("%w: %q", errNoFakeServer, host)
	}
	return &fakeConnection{server: server.(*fakeServer)}, nil
}

func (*fakeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errNotSupported
}

func (*fakeConnection) Close() error {
	return nil
}

func (*fakeConnection) Begin() (driver.Tx, error) {
	return nil, errNotSupported
}

func (connection *fakeConnection) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := connection.server.record(query); err != nil {
		return nil, err
	}
	return driver.RowsAffected(0), nil
}

func (connection *fakeConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := connection.server.record(query); err != nil {
		return nil, err
	}
	return connection.server.rowsFor(query), nil
}

func (rows *fakeRows) Columns() []string {
	names := make([]string, 0, rows.width)
	for index := range rows.width {
		names = append(names, "c"+strconv.Itoa(index))
	}
	return names
}

func (rows *fakeRows) Close() error {
	rows.rows = nil
	return nil
}

func (rows *fakeRows) Next(destination []driver.Value) error {
	if len(rows.rows) == 0 {
		return io.EOF
	}
	copy(destination, rows.rows[0])
	rows.rows = rows.rows[1:]
	return nil
}

func newFakeServer(t *testing.T) (*fakeServer, string) {
	t.Helper()

	registerFakeDriver()
	name := "fake" + strconv.FormatUint(fakeServerCount.Add(1), 10)
	server := &fakeServer{results: map[string][][]driver.Value{}}
	fakeServers.Store(name, server)
	t.Cleanup(func() { fakeServers.Delete(name) })
	return server, name
}
