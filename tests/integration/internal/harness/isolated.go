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

package harness

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// isolatedPrefix starts the name of every schema or database a test has to itself.
	isolatedPrefix = "sgl_it_"

	// nameDigestBytes is how many bytes of the test name's digest an isolated name keeps.
	nameDigestBytes = 8

	// cleanupTimeout bounds each clean-up the harness registers, so a server that has gone
	// away cannot hold the test binary open.
	cleanupTimeout = 30 * time.Second

	// urlSchemeSeparator separates a URL-style connection string's scheme from its host.
	urlSchemeSeparator = "://"

	// searchPathParameter is the connection parameter that sets a session's search path.
	searchPathParameter = "search_path"
)

// isolatedCount numbers the isolated names the test binary hands out, so two in one test
// differ.
var isolatedCount atomic.Uint64

// IsolatedSchema creates a schema of the test's own on the server the connection string
// names, drops it when the test ends, and returns a pool whose sessions start in it
// through the search_path parameter, with the connection string that pool opened.
//
// Takes driver (string) which is the database/sql driver name.
// Takes connection (string) which is the server's URL-style connection string.
//
// Returns *sql.DB which is the pool, closed when the test ends.
// Returns string which is the pool's connection string.
func IsolatedSchema(t *testing.T, driver, connection string) (*sql.DB, string) {
	t.Helper()

	name := UniqueName(t)
	drop := "DROP SCHEMA IF EXISTS " + name + " CASCADE"
	runOnServer(t, driver, connection, drop, "CREATE SCHEMA "+name)
	dropOnCleanup(t, driver, connection, drop)
	isolated := WithParameter(connection, searchPathParameter, name)
	return openIsolated(t, driver, isolated), isolated
}

// IsolatedDatabase creates a database of the test's own on the server the connection
// string names, drops it when the test ends, and returns a pool on it, with the
// connection string that pool opened: the server's, naming the new database, its
// parameters kept.
//
// Takes driver (string) which is the database/sql driver name.
// Takes connection (string) which is the server's connection string, URL-style or a
// go-sql-driver/mysql data source name.
//
// Returns *sql.DB which is the pool, closed when the test ends.
// Returns string which is the pool's connection string.
func IsolatedDatabase(t *testing.T, driver, connection string) (*sql.DB, string) {
	t.Helper()

	name := UniqueName(t)
	drop := "DROP DATABASE IF EXISTS " + name
	runOnServer(t, driver, connection, drop, "CREATE DATABASE "+name)
	dropOnCleanup(t, driver, connection, drop)
	isolated := WithDatabase(connection, name)
	return openIsolated(t, driver, isolated), isolated
}

// UniqueName returns a schema or database name for the test: a prefix, a digest of the
// test's name and a number no other call in the test binary returns, so tests running at
// once never share one and a failed test's leftovers are easy to trace.
//
// Returns string which is the name, a valid unquoted identifier on every engine.
func UniqueName(t *testing.T) string {
	digest := sha256.Sum256([]byte(t.Name()))
	return isolatedPrefix + hex.EncodeToString(digest[:nameDigestBytes]) + "_" + strconv.FormatUint(isolatedCount.Add(1), 10)
}

// WithDatabase returns a connection string naming another database on the same server,
// its parameters kept: the path after the host of a URL-style string, or what follows the
// last slash of a go-sql-driver/mysql data source name, is replaced, or added when there
// is none.
//
// Takes connection (string) which is the connection string.
// Takes name (string) which is the database to name.
//
// Returns string which is the rewritten connection string.
func WithDatabase(connection, name string) string {
	address, parameters, hasParameters := strings.Cut(connection, "?")
	server := address
	if beforeSlash, _, found := strings.CutLast(address, "/"); found && len(beforeSlash) >= hostStart(address) {
		server = beforeSlash
	}
	rewritten := server + "/" + name
	if hasParameters {
		rewritten += "?" + parameters
	}
	return rewritten
}

// CleanupContext returns the context a clean-up runs under: the test's values without its
// cancellation, which has already happened when clean-ups run, ending with a cause once
// cleanupTimeout has passed.
//
// Returns context.Context which bounds the clean-up.
// Returns context.CancelFunc which releases it.
func CleanupContext(t *testing.T) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(context.WithoutCancel(t.Context()), cleanupTimeout, errCleanupTimeout)
}

// hostStart returns where the host of a connection string starts: after the scheme of a
// URL-style string, or at its start.
//
// Takes address (string) which is the connection string without its parameters.
//
// Returns int which is the host's offset.
func hostStart(address string) int {
	if scheme, _, found := strings.Cut(address, urlSchemeSeparator); found {
		return len(scheme) + len(urlSchemeSeparator)
	}
	return 0
}

// runOnServer runs statements, in order, on a connection the test opens and closes.
//
// Takes driver (string) which is the database/sql driver name.
// Takes connection (string) which is the server's connection string.
// Takes statements (...string) which are run.
func runOnServer(t *testing.T, driver, connection string, statements ...string) {
	t.Helper()

	server := openServer(t, driver, connection)
	defer func() { assert.NoError(t, server.Close()) }()
	for _, statement := range statements {
		_, err := server.ExecContext(t.Context(), statement)
		require.NoError(t, err, "running %s", statement)
	}
}

// dropOnCleanup runs a statement that drops what a test created once the test ends, on a
// connection of its own, and logs it when the drop fails.
//
// Takes driver (string) which is the database/sql driver name.
// Takes connection (string) which is the server's connection string.
// Takes statement (string) which drops the schema or database.
func dropOnCleanup(t *testing.T, driver, connection, statement string) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := CleanupContext(t)
		defer cancel()
		server, err := sql.Open(driver, connection)
		if err != nil {
			t.Logf("opening the server to run %s: %v", statement, err)
			return
		}
		defer func() {
			if closeErr := server.Close(); closeErr != nil {
				t.Logf("closing the server after %s: %v", statement, closeErr)
			}
		}()
		if _, execErr := server.ExecContext(ctx, statement); execErr != nil {
			t.Logf("running %s: %v", statement, execErr)
		}
	})
}

// execOnCleanup runs a statement on a pool once the test ends, before the pool is closed
// when the pool was opened earlier in the test, and logs it when the statement fails.
//
// Takes database (*sql.DB) which runs the statement.
// Takes statement (string) which is run.
func execOnCleanup(t *testing.T, database *sql.DB, statement string) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := CleanupContext(t)
		defer cancel()
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Logf("running %s: %v", statement, err)
		}
	})
}

// openIsolated opens a pool on an isolated schema or database and closes it when the test
// ends, before the schema or database is dropped.
//
// Takes driver (string) which is the database/sql driver name.
// Takes connection (string) which names the schema or database.
//
// Returns *sql.DB which is the pool.
func openIsolated(t *testing.T, driver, connection string) *sql.DB {
	t.Helper()

	database := openServer(t, driver, connection)
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	return database
}
