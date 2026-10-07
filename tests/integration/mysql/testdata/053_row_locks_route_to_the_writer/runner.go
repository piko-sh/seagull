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

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	"case_runner/db"
)

var lockingClause = regexp.MustCompile(`(?i)\b(FOR\s+(UPDATE|SHARE)|LOCK\s+IN\s+SHARE\s+MODE)\b`)

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

type reader struct {
	*sql.DB
	locking *int
}

func (r reader) count(query string) {
	if lockingClause.MatchString(query) {
		*r.locking++
	}
}

func (r reader) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	r.count(query)
	return r.DB.ExecContext(ctx, query, arguments...)
}

func (r reader) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	r.count(query)
	return r.DB.QueryContext(ctx, query, arguments...)
}

func (r reader) QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row {
	r.count(query)
	return r.DB.QueryRowContext(ctx, query, arguments...)
}

func open(ctx context.Context, statements ...string) *sql.DB {
	conn, err := sql.Open("mysql", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	conn.SetMaxOpenConns(1)
	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	return conn
}

func main() {
	ctx := context.Background()
	writer := open(ctx)
	defer writer.Close()
	standby := open(ctx, "SET SESSION TRANSACTION READ ONLY")
	defer standby.Close()

	locking := 0
	replica := reader{DB: standby, locking: &locking}
	queries := db.NewWithReplica(writer, replica)
	owner := "ops"
	output := results{}
	output.record("add_jobs", func() (any, error) {
		for _, job := range []db.AddJobParams{{ID: 1, State: "ready", Owner: &owner}, {ID: 2, State: "ready", Owner: &owner}, {ID: 3, State: "done", Owner: nil}} {
			if err := queries.AddJob(ctx, job); err != nil {
				return nil, err
			}
		}
		return "added", nil
	})
	output.record("list_jobs", func() (any, error) { return queries.ListJobs(ctx, "ready") })
	output.record("lock_job", func() (any, error) { return queries.LockJob(ctx, 1) })
	output.record("share_lock_job", func() (any, error) { return queries.ShareLockJob(ctx, 3) })
	output.record("lock_in_cte", func() (any, error) { return queries.LockInCTE(ctx, 2) })
	output.record("lock_in_derived_table", func() (any, error) { return queries.LockInDerivedTable(ctx, "ready") })
	output.record("lock_in_subquery", func() (any, error) { return queries.LockInSubquery(ctx, "ready") })
	output.record("lock_in_exists", func() (any, error) { return queries.LockInExists(ctx) })
	output.record("lock_in_scalar_subquery", func() (any, error) { return queries.LockInScalarSubquery(ctx) })
	output.record("locking_statements_on_the_reader", func() (any, error) { return locking, nil })
	output.record("reader_refuses_for_update", func() (any, error) {
		var id int64
		return id, standby.QueryRowContext(ctx, "SELECT id FROM jobs WHERE id = 1 FOR UPDATE").Scan(&id)
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
