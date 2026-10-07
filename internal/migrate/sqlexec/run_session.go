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
	"fmt"
)

// session returns the connection the run works on: its work connection, or else the
// lock's session, which is nil when the lock pinned none.
//
// Returns *sql.Conn which is the run's session.
func (held *heldLock) session() *sql.Conn {
	if held.workConnection != nil {
		return held.workConnection
	}
	return held.handle.connection
}

// lockTransaction returns the open transaction that holds the lock, or nil when nothing
// is held or the lock is held without a transaction.
//
// Returns *sql.Tx which is the lock transaction, or nil.
func (held *heldLock) lockTransaction() *sql.Tx {
	if held == nil {
		return nil
	}
	return held.handle.transaction
}

// keepsSessionSchema reports whether the engine's sessions keep a current schema that a
// migration can change, so runs pin a session, qualify their tables and close the session
// afterwards.
//
// Returns bool which is true when the engine has a current-schema query.
func (e *engine) keepsSessionSchema() bool {
	return e.currentSchemaSQL != ""
}

// prepareRun completes what a migration or seed run holds once its lock is taken.
//
// It pins a connection for the run's work when the caller asks for one apart from the
// lock's session, and, on an engine whose sessions keep a current schema, whenever the
// lock pinned no session, so that every statement of the run shares one session: a SET in
// one migration then lasts for the later migrations of the run, as the generator assumes
// when it replays them. On such an engine it then reads the schema current on that
// session, before the run executes anything, so the run's history and seed tables can be
// qualified by it.
//
// Takes database (*sql.DB) which is the connection pool.
// Takes engine (*engine) which describes the database.
// Takes handle (lockHandle) which is what the lock's acquisition pinned.
// Takes workApart (bool) which asks for a work connection apart from the lock's session.
// Takes label (string) which names the run in error messages, such as "migration".
//
// Returns *heldLock which records the lock, any work connection and the schema.
// Returns error when no work connection can be pinned or the schema cannot be read; any
// connection pinned here is discarded then, and the caller releases the lock.
func prepareRun(ctx context.Context, database *sql.DB, engine *engine, handle lockHandle, workApart bool, label string) (*heldLock, error) {
	held := &heldLock{workConnection: nil, schema: "", handle: handle}
	if workApart || (engine.keepsSessionSchema() && handle.connection == nil) {
		workConnection, connectionError := database.Conn(ctx)
		if connectionError != nil {
			return nil, fmt.Errorf("pinning %s connection: %w", label, connectionError)
		}
		held.workConnection = workConnection
	}
	if !engine.keepsSessionSchema() {
		return held, nil
	}
	schema, schemaError := readSessionSchema(ctx, held.session(), engine.currentSchemaSQL)
	if schemaError != nil {
		if held.workConnection != nil {
			discardConnection(held.workConnection)
		}
		return nil, fmt.Errorf("preparing the %s session: %w", label, schemaError)
	}
	held.schema = schema
	return held, nil
}

// readSessionSchema reads the schema a session creates unqualified tables in.
//
// Takes session (*sql.Conn) which is the run's session.
// Takes query (string) which is the engine's current-schema query.
//
// Returns string which is the schema, or empty when the session has none, as when no
// schema on its search path exists.
// Returns error when the query fails.
func readSessionSchema(ctx context.Context, session *sql.Conn, query string) (string, error) {
	var schema sql.NullString
	if err := session.QueryRowContext(ctx, query).Scan(&schema); err != nil {
		return "", fmt.Errorf("reading the current schema: %w", err)
	}
	return schema.String, nil
}

// finishRun ends what a run pinned besides its lock, and returns the lock's handle ready
// for Release.
//
// On an engine whose sessions keep a current schema the run's session is closed rather
// than returned to the pool, whether it is a work connection or the lock's own session,
// so that a setting a migration made, such as its search path, reaches neither the
// application's queries nor a later run.
//
// Takes engine (*engine) which describes the database.
// Takes held (*heldLock) which is what the run held.
//
// Returns lockHandle which is the lock's handle, marked for discarding when the run
// worked on the lock's session.
// Returns error when closing the work connection fails.
func finishRun(engine *engine, held *heldLock) (lockHandle, error) {
	handle := held.handle
	if held.workConnection == nil {
		handle.discard = engine.keepsSessionSchema()
		return handle, nil
	}
	if engine.keepsSessionSchema() {
		discardConnection(held.workConnection)
		return handle, nil
	}
	return handle, held.workConnection.Close()
}

// qualifiedTable names a persisted table as a run's SQL refers to it: qualified by the
// schema the run's session started in, when the run read one, and as the backend names it
// otherwise.
//
// Takes engine (*engine) which renders the qualified name.
// Takes held (*heldLock) which is what the run holds, or nil outside a run.
// Takes table (string) which is the validated table name.
//
// Returns string which is the name to interpolate.
func qualifiedTable(engine *engine, held *heldLock, table string) string {
	if held == nil || held.schema == "" {
		return table
	}
	return engine.qualifyTable(held.schema, table)
}
