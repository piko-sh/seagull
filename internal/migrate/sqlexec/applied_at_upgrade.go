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
	"errors"
	"fmt"
)

// appliedAtUpgrade moves a history table's applied_at column off a type that cannot hold
// every time a run may record, such as MySQL's TIMESTAMP(6), which refuses any value
// after 2038-01-19 03:14:07 UTC and converts values through the session's time zone.
type appliedAtUpgrade struct {
	// alter renders the statement that changes a table's applied_at column to the new type.
	alter func(table string) string

	// probe counts, given the table's name, its applied_at columns of the old type.
	probe string

	// restoreZone sets the session's time zone back to the one saved.
	restoreZone string

	// zoneStatements save the session's time zone and set it to UTC, so the old values
	// convert as the UTC times they were recorded as.
	zoneStatements []string
}

// mysqlAppliedAtUpgrade changes a MySQL or MariaDB history table's applied_at from
// TIMESTAMP(6) to DATETIME(6) defaulting to UTC_TIMESTAMP(6), which holds times to the
// year 9999 and records them in UTC whatever the session's time zone.
var mysqlAppliedAtUpgrade = &appliedAtUpgrade{
	probe: "SELECT count(*) FROM information_schema.columns AS candidate, (SELECT ? AS name) AS probe " +
		"WHERE candidate.column_name = 'applied_at' AND candidate.data_type = 'timestamp' " +
		"AND ((candidate.table_schema = DATABASE() AND candidate.table_name = probe.name) " +
		"OR CONCAT('`', REPLACE(candidate.table_schema, '`', '``'), '`.', candidate.table_name) = probe.name)",
	alter: func(table string) string {
		return "ALTER TABLE " + table + " MODIFY applied_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6))"
	},
	zoneStatements: []string{"SET @sgl_time_zone = @@session.time_zone", "SET time_zone = '+00:00'"},
	restoreZone:    "SET time_zone = @sgl_time_zone",
}

// upgradeAppliedAt changes a history table's applied_at column to the engine's new type
// when it still has the old one, on one connection, with the session in UTC while it
// does.
//
// Takes runner (queryRunner) which is the connection or pool the run works on.
// Takes upgrade (*appliedAtUpgrade) which describes the change, or nil for none.
// Takes table (string) which is the table as the run names it.
//
// Returns error when the column cannot be probed or changed.
func upgradeAppliedAt(ctx context.Context, runner queryRunner, upgrade *appliedAtUpgrade, table string) (err error) {
	if upgrade == nil {
		return nil
	}
	var old int
	if err := runner.QueryRowContext(ctx, upgrade.probe, table).Scan(&old); err != nil {
		return fmt.Errorf("probing %s.applied_at: %w", table, err)
	}
	if old == 0 {
		return nil
	}
	if pool, isPool := runner.(*sql.DB); isPool {
		connection, connectError := pool.Conn(ctx)
		if connectError != nil {
			return fmt.Errorf("taking a connection to change %s.applied_at: %w", table, connectError)
		}
		defer func() { _ = connection.Close() }()
		runner = connection
	}
	for _, statement := range upgrade.zoneStatements {
		if _, err := runner.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("setting the session to UTC to change %s.applied_at: %w", table, err)
		}
	}
	defer func() {
		restoreContext, cancel := checkpointContext(ctx)
		defer cancel()
		if _, restoreError := runner.ExecContext(restoreContext, upgrade.restoreZone); restoreError != nil {
			err = errors.Join(err, fmt.Errorf("restoring the session's time zone: %w", restoreError))
		}
	}()
	if _, err := runner.ExecContext(ctx, upgrade.alter(table)); err != nil {
		return fmt.Errorf("changing %s.applied_at to hold times after 2038: %w", table, err)
	}
	return nil
}
