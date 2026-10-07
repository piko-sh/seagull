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
	"fmt"
	"io"
	"time"

	"seagull.sh/seagull/examples/clickhouse/036_clickhouse_async_mutations/internal/store"
)

// run issues four fishing licences, then renews, purges and cancels some. ClickHouse
// changes stored rows with mutations: ALTER TABLE ... UPDATE and ALTER TABLE ... DELETE
// rewrite the affected parts in the background. The server replies once it has queued the
// mutation, not when the rows have changed, and it reports no changed rows. So
// RenewLicence and PurgeExpired are declared asyncexec: each method returns only an
// error, and its doc comment warns that a nil error means queued, not done. Declaring one
// exec still works, but sgl vet points it out as hint Q040; asyncexec on an engine whose
// mutations are synchronous is error Q041.
//
// The mutations_sync setting makes the server wait instead. This program's connection
// sets mutations_sync=2, which waits for every replica, so RenewLicence has finished
// before the listing reads it. PurgeExpired carries SETTINGS mutations_sync = 2 in its
// own SQL, so it waits whatever the connection says.
//
// CancelLicence is a lightweight DELETE FROM, which marks the rows deleted and, by
// default, returns once that is done, so it is a plain exec.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the licences before and after.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	licences := []store.IssueLicenceParams{
		{ID: 1, Boat: "Kittiwake", Expires: date(2026, time.March, 31)},
		{ID: 2, Boat: "Salty Chip", Expires: date(2026, time.April, 30)},
		{ID: 3, Boat: "Gull Wing", Expires: date(2026, time.September, 30)},
		{ID: 4, Boat: "Driftwood", Expires: date(2026, time.December, 31)},
	}
	for _, licence := range licences {
		if err := queries.IssueLicence(ctx, licence); err != nil {
			return fmt.Errorf("issuing licence %d: %w", licence.ID, err)
		}
	}
	if err := printLicences(ctx, queries, "issued", output); err != nil {
		return err
	}

	if err := queries.RenewLicence(ctx, 1); err != nil {
		return fmt.Errorf("renewing licence 1: %w", err)
	}
	if err := queries.PurgeExpired(ctx, date(2026, time.June, 1)); err != nil {
		return fmt.Errorf("purging expired licences: %w", err)
	}
	if err := queries.CancelLicence(ctx, 4); err != nil {
		return fmt.Errorf("cancelling licence 4: %w", err)
	}
	return printLicences(ctx, queries, "after renewing #1, purging those expired by 1 June and cancelling #4", output)
}

// printLicences lists every licence under a heading.
//
// Takes queries (*store.Queries) which runs the query.
// Takes heading (string) which introduces the list.
// Takes output (io.Writer) which receives the list.
//
// Returns error when the query fails.
func printLicences(ctx context.Context, queries *store.Queries, heading string, output io.Writer) error {
	licences, err := queries.ListLicences(ctx)
	if err != nil {
		return fmt.Errorf("listing licences: %w", err)
	}
	fmt.Fprintf(output, "%s:\n", heading)
	for _, licence := range licences {
		fmt.Fprintf(output, "  #%d %-10s expires %s, renewals: %d\n", licence.ID, licence.Boat, licence.Expires.Format(time.DateOnly), licence.Renewals)
	}
	return nil
}

// date returns a date at midnight UTC. A Date parameter binds the calendar date a time
// has in its own location.
//
// Takes year (int) which is the year.
// Takes month (time.Month) which is the month.
// Takes day (int) which is the day of the month.
//
// Returns time.Time which is the date.
func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
