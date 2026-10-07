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
	"context"
	"fmt"
	"io"
	"os"

	"seagull.sh/seagull/sdk/migrate"
)

// runSeed applies, inspects or re-applies seed data.
//
// Takes arguments ([]string) which holds the subcommand and the flags.
//
// Returns int which is the exit status.
func (inv *invocation) runSeed(ctx context.Context, arguments []string) int {
	flags := inv.newFlags("seed", "seed apply|status|reseed [--dsn DSN] [--database name] [--lock-timeout d] [--non-blocking]")
	options := migrateFlagDefaults
	flags.StringVar(&options.database, databaseFlag, inv.database, "seed the `name`d database")
	flags.StringVar(&options.dsn, dsnFlag, "", dsnUsage)
	options.defineLockFlags(flags, "seed")
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if len(positional) != 1 {
		return flags.misuse()
	}
	subcommand := positional[0]
	switch subcommand {
	case "apply", "status", "reseed":
	default:
		return inv.fail(usageError("unknown seed subcommand %q; want apply, status or reseed", subcommand))
	}

	opened, err := inv.connect(ctx, options.database, options.dsn)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = opened.Close() }()
	seeder, err := inv.newSeeder(opened, &options)
	if err != nil {
		return inv.fail(err)
	}

	switch subcommand {
	case "apply":
		return inv.reportApplied(seeder.Apply(ctx))
	case "reseed":
		return inv.reportApplied(seeder.Reseed(ctx))
	default:
		statuses, err := seeder.Status(ctx)
		if err != nil {
			return inv.fail(migrationFailure(err))
		}
		writeSeedStatus(inv.streams.Stdout, statuses)
		return exitOK
	}
}

// newSeeder creates the seeder for a connection.
//
// Takes opened (*connection) which is the database.
// Takes flags (*migrateFlags) which hold the lock flags.
//
// Returns *migrate.Seeder which is ready to run.
// Returns error when no seed directory is configured or the seeder cannot be created.
func (inv *invocation) newSeeder(opened *connection, flags *migrateFlags) (*migrate.Seeder, error) {
	if opened.configured.Seeds == "" {
		return nil, usageError("database %q has no seeds directory; set seeds in the configuration", opened.configured.Name)
	}
	backend, err := opened.backend()
	if err != nil {
		return nil, err
	}
	lockOptions, err := flags.lockOptions()
	if err != nil {
		return nil, err
	}
	seeder, err := migrate.NewSeeder(opened.database, backend, os.DirFS(opened.configured.Seeds), ".", append(opened.migrateOptions(inv), lockOptions...)...)
	if err != nil {
		return nil, migrationFailure(err)
	}
	return seeder, nil
}

// writeSeedStatus prints one line per seed.
//
// Takes writer (io.Writer) which receives the table.
// Takes statuses ([]migrate.SeedStatus) which are printed.
func writeSeedStatus(writer io.Writer, statuses []migrate.SeedStatus) {
	table := newTable(writer)
	fmt.Fprintln(table, "VERSION\tNAME\tSTATE\tAPPLIED")
	for _, status := range statuses {
		state := "pending"
		switch {
		case status.Applied && !status.ChecksumMatch:
			state = "changed"
		case status.Applied:
			state = "applied"
		}
		fmt.Fprintf(table, "%d\t%s\t%s\t%s\n", status.Version, status.Name, state, appliedTime(status.Applied, status.AppliedAt))
	}
	_ = table.Flush()
}
