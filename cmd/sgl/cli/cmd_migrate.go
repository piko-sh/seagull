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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"piko.sh/safedisk"

	"seagull.sh/seagull/sdk/migrate"
)

const (
	// defaultVersionWidth is the digit count of the first migration's version.
	defaultVersionWidth = 4

	// timeLayout is how applied times are printed.
	timeLayout = "2006-01-02 15:04:05"

	// notApplied is printed in place of an applied time.
	notApplied = "-"

	// outcomeApplied ends the line counting the migrations or seeds a run applied.
	outcomeApplied = "applied"

	// outcomeRolledBack ends the line counting the migrations a run rolled back.
	outcomeRolledBack = "rolled back"

	// subcommandForce records a migration as applied without running it.
	subcommandForce = "force"

	// subcommandRepair re-baselines checksums and resolves dirty migrations.
	subcommandRepair = "repair"
)

// migrateFlags holds the flags shared by migrate and seed.
type migrateFlags struct {
	// database names the database.
	database string

	// dsn is the connection string.
	dsn string

	// lockTimeout bounds the wait for the migration or seed lock; zero waits until the
	// command is interrupted.
	lockTimeout time.Duration

	// completed is the --completed value of migrate repair: how many statements of the dirty
	// migration ran, or -1 when it is not given.
	completed int

	// nonBlocking fails at once when another process holds the lock.
	nonBlocking bool

	// yes confirms migrate force and migrate repair without asking.
	yes bool
}

var (
	// versionPrefix matches the version at the start of a migration file name.
	versionPrefix = regexp.MustCompile(`^(\d+)_.+\.(up|down)\.sql$`)

	// unsafeNameCharacters matches what a migration name may not contain.
	unsafeNameCharacters = regexp.MustCompile(`[^a-z0-9_]+`)

	// migrateFlagDefaults holds the flags before parsing: no lock timeout and no
	// --completed.
	migrateFlagDefaults = migrateFlags{database: "", dsn: "", lockTimeout: 0, completed: -1, nonBlocking: false, yes: false}
)

// defineLockFlags defines --lock-timeout and --non-blocking.
//
// Takes flags (*commandFlags) which receives them.
// Takes lock (string) which names the lock in the usage, such as migration.
func (options *migrateFlags) defineLockFlags(flags *commandFlags, lock string) {
	flags.DurationVar(&options.lockTimeout, "lock-timeout", 0, "give up when the "+lock+" lock is still held elsewhere after this `duration`, such as 30s")
	flags.BoolVar(&options.nonBlocking, "non-blocking", false, "fail at once when another process holds the "+lock+" lock")
}

// lockOptions converts the lock flags into migrate options.
//
// Returns []migrate.Option which bound or skip the wait for the lock.
// Returns error which is a usage error for a negative timeout.
func (options *migrateFlags) lockOptions() ([]migrate.Option, error) {
	if options.lockTimeout < 0 {
		return nil, usageError("--lock-timeout must not be negative, got %s", options.lockTimeout)
	}
	var lockOptions []migrate.Option
	if options.lockTimeout > 0 {
		lockOptions = append(lockOptions, migrate.WithLockTimeout(options.lockTimeout))
	}
	if options.nonBlocking {
		lockOptions = append(lockOptions, migrate.WithNonBlockingLock())
	}
	return lockOptions, nil
}

// migrateArguments holds a migrate subcommand's arguments, parsed once before connecting.
type migrateArguments struct {
	// version is the version to, force or repair names, or zero when none is given.
	version int64

	// steps is how many migrations down rolls back.
	steps int

	// hasVersion reports that the subcommand was given a version.
	hasVersion bool
}

// runMigrate applies, rolls back, inspects, validates or creates migrations.
//
// Takes arguments ([]string) which holds the subcommand, its arguments and the flags.
//
// Returns int which is the exit status.
func (inv *invocation) runMigrate(ctx context.Context, arguments []string) int {
	flags := inv.newFlags("migrate", "migrate up|down [n]|to V|status|validate|new NAME|force V|repair [V] [--dsn DSN] [--database name] "+
		"[--lock-timeout d] [--non-blocking] [--yes] [--completed n]")
	options := migrateFlagDefaults
	flags.StringVar(&options.database, databaseFlag, inv.database, "migrate the `name`d database")
	flags.StringVar(&options.dsn, dsnFlag, "", dsnUsage)
	options.defineLockFlags(flags, "migration")
	flags.BoolVar(&options.yes, "yes", false, "force or repair without asking for confirmation")
	flags.IntVar(&options.completed, "completed", -1, "repair: the `number` of the dirty migration's statements that ran")
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if len(positional) == 0 {
		return flags.misuse()
	}
	subcommand, rest := positional[0], positional[1:]
	if subcommand == "new" {
		return inv.migrateNew(&options, rest)
	}
	checked, err := inv.checkMigrateCommand(subcommand, rest, &options)
	if err != nil {
		return inv.fail(err)
	}

	opened, err := inv.connect(ctx, options.database, options.dsn)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = opened.Close() }()
	migrator, err := inv.newMigrator(opened, &options)
	if err != nil {
		return inv.fail(err)
	}
	return inv.runMigration(ctx, migrator, subcommand, checked, &options)
}

// checkMigrateCommand checks a migrate subcommand, its arguments and its flags before
// connecting.
//
// Takes subcommand (string) which is the subcommand.
// Takes rest ([]string) which holds its arguments.
// Takes options (*migrateFlags) which holds the flags.
//
// Returns migrateArguments which holds the parsed arguments.
// Returns error which is a usage error when they do not fit.
func (inv *invocation) checkMigrateCommand(subcommand string, rest []string, options *migrateFlags) (migrateArguments, error) {
	arguments, err := checkMigrateArguments(subcommand, rest)
	if err != nil {
		return arguments, err
	}
	if options.completed != -1 && subcommand != subcommandRepair {
		return arguments, usageError("--completed applies only to migrate repair")
	}
	if options.completed < -1 {
		return arguments, usageError("--completed must be zero or more, got %d", options.completed)
	}
	if (subcommand == subcommandForce || subcommand == subcommandRepair) && !options.yes && !inv.interactive() {
		return arguments, usageError("migrate %s rewrites the migration history, so it asks for confirmation; "+
			"run it in a terminal or pass --yes", subcommand)
	}
	return arguments, nil
}

// newMigrator creates the migrator for a connection, reporting each migration it runs.
//
// Takes opened (*connection) which is the database.
// Takes flags (*migrateFlags) which hold the lock flags.
//
// Returns *migrate.Migrator which is ready to run.
// Returns error when the backend or migrator cannot be created, or the migration
// directory does not exist.
func (inv *invocation) newMigrator(opened *connection, flags *migrateFlags) (*migrate.Migrator, error) {
	backend, err := opened.backend()
	if err != nil {
		return nil, err
	}
	lockOptions, err := flags.lockOptions()
	if err != nil {
		return nil, err
	}
	options := append(opened.migrateOptions(inv), lockOptions...)
	options = append(options, migrate.WithAfterMigration(func(_ context.Context, hook migrate.MigrationHookContext) error {
		direction := "up"
		if hook.Direction == migrate.MigrationDirectionDown {
			direction = "down"
		}
		fmt.Fprintf(inv.streams.Stdout, "%s %d %s\n", direction, hook.Version, hook.Name)
		return nil
	}))
	migrator, err := migrate.NewMigrator(opened.database, backend, os.DirFS(opened.configured.Migrations), ".", options...)
	if err != nil {
		return nil, migrationFailure(err)
	}
	if err := inv.requireMigrationDirectory(opened.loaded, opened.configured); err != nil {
		return nil, err
	}
	return migrator, nil
}

// runMigration runs one migrate subcommand.
//
// Takes migrator (*migrate.Migrator) which runs it.
// Takes subcommand (string) which is up, down, to, status, validate, force or repair.
// Takes arguments (migrateArguments) which holds its parsed arguments.
// Takes options (*migrateFlags) which holds the flags.
//
// Returns int which is the exit status.
func (inv *invocation) runMigration(ctx context.Context, migrator *migrate.Migrator, subcommand string, arguments migrateArguments, options *migrateFlags) int {
	switch subcommand {
	case subcommandForce:
		return inv.migrateForce(ctx, migrator, arguments.version, options)
	case subcommandRepair:
		return inv.migrateRepair(ctx, migrator, arguments, options)
	case "up":
		return inv.reportApplied(migrator.Up(ctx))
	case "down":
		return inv.reportRolledBack(migrator.Down(ctx, arguments.steps))
	case "to":
		return inv.migrateTo(ctx, migrator, arguments.version)
	case "status":
		statuses, err := migrator.Status(ctx)
		if err != nil {
			return inv.fail(migrationFailure(err))
		}
		writeMigrationStatus(inv.streams.Stdout, statuses)
		return exitOK
	default:
		return inv.validateMigrations(ctx, migrator)
	}
}

// reportApplied prints how many migrations or seeds a run applied, or the error that
// stopped it.
//
// Takes count (int) which is how many were applied.
// Takes err (error) which is the outcome.
//
// Returns int which is the exit status.
func (inv *invocation) reportApplied(count int, err error) int {
	return inv.reportOutcome(count, outcomeApplied, err)
}

// reportRolledBack prints how many migrations a run rolled back, or the error that
// stopped it.
//
// Takes count (int) which is how many were rolled back.
// Takes err (error) which is the outcome.
//
// Returns int which is the exit status.
func (inv *invocation) reportRolledBack(count int, err error) int {
	return inv.reportOutcome(count, outcomeRolledBack, err)
}

// reportOutcome prints how many migrations or seeds a run changed and how, such as "2
// applied", and then the error that stopped it, if one did. The count is printed even
// when the run failed part of the way, so the user knows what changed before it stopped.
//
// Takes count (int) which is how many changed.
// Takes outcome (string) which says how, such as applied or rolled back.
// Takes err (error) which is the outcome.
//
// Returns int which is the exit status.
func (inv *invocation) reportOutcome(count int, outcome string, err error) int {
	if err == nil || count > 0 {
		fmt.Fprintf(inv.streams.Stdout, "%d %s\n", count, outcome)
	}
	if err != nil {
		return inv.fail(migrationFailure(err))
	}
	return exitOK
}

// migrateTo migrates up or down to a version.
//
// Takes migrator (*migrate.Migrator) which runs the migrations.
// Takes target (int64) which is the version to end at.
//
// Returns int which is the exit status.
func (inv *invocation) migrateTo(ctx context.Context, migrator *migrate.Migrator, target int64) int {
	statuses, err := migrator.Status(ctx)
	if err != nil {
		return inv.fail(migrationFailure(err))
	}
	var current int64
	for _, status := range statuses {
		if status.Applied {
			current = max(current, status.Version)
		}
	}
	switch {
	case target > current:
		return inv.reportApplied(migrator.UpTo(ctx, target))
	case target < current:
		return inv.reportRolledBack(migrator.DownTo(ctx, target))
	default:
		fmt.Fprintf(inv.streams.Stdout, "already at version %d\n", target)
		return exitOK
	}
}

// validateMigrations checks the applied migrations against the files.
//
// Takes migrator (*migrate.Migrator) which validates.
//
// Returns int which is 1 when the history and files disagree.
func (inv *invocation) validateMigrations(ctx context.Context, migrator *migrate.Migrator) int {
	err := migrator.Validate(ctx)
	if err == nil {
		fmt.Fprintln(inv.streams.Stdout, "migrations are valid")
		return exitOK
	}
	_, checksum := errors.AsType[*migrate.ChecksumMismatchError](err)
	_, downChecksum := errors.AsType[*migrate.DownChecksumMismatchError](err)
	_, missing := errors.AsType[*migrate.MissingMigrationFileError](err)
	_, dirty := errors.AsType[*migrate.DirtyMigrationError](err)
	if checksum || downChecksum || missing || dirty {
		fmt.Fprintf(inv.streams.Stderr, errorFormat, inv.program, err)
		return exitFindings
	}
	return inv.fail(migrationFailure(err))
}

// migrateNew creates the next pair of migration files.
//
// Takes options (*migrateFlags) which select the database.
// Takes rest ([]string) which holds the migration's name.
//
// Returns int which is the exit status.
func (inv *invocation) migrateNew(options *migrateFlags, rest []string) int {
	if len(rest) != 1 {
		return inv.fail(usageError("migrate new needs one name, such as add_users"))
	}
	name := strings.Trim(unsafeNameCharacters.ReplaceAllString(strings.ToLower(rest[0]), "_"), "_")
	if name == "" {
		return inv.fail(usageError("migration name %q has no letters or digits", rest[0]))
	}
	loaded, err := inv.loadProject(options.database)
	if err != nil {
		return inv.fail(err)
	}
	if len(loaded.databases) != 1 {
		return inv.fail(usageError("the configuration has %d databases; choose one with --database", len(loaded.databases)))
	}
	directory := loaded.databases[0].Migrations
	version, width, err := nextVersion(directory)
	if err != nil {
		return inv.fail(err)
	}
	if err := os.MkdirAll(directory, directoryPermissions); err != nil {
		return inv.fail(err)
	}
	stem := fmt.Sprintf("%0*d_%s", width, version, name)
	for _, direction := range []string{"up", "down"} {
		path := filepath.Join(directory, stem+"."+direction+".sql")
		content := fmt.Sprintf("-- %s: write the %s migration here.\n", stem, direction)
		if err := inv.writeNewFile(path, content); err != nil {
			return inv.fail(err)
		}
		fmt.Fprintf(inv.streams.Stdout, "created %s\n", inv.display(path))
	}
	return exitOK
}

// writeNewFile writes a file that must not exist yet, through a sandbox rooted at its
// directory.
//
// Takes path (string) which is the file.
// Takes content (string) which is written.
//
// Returns error which is a usage error when the file exists.
func (inv *invocation) writeNewFile(path, content string) error {
	sandbox, err := safedisk.NewSandbox(filepath.Dir(path), safedisk.ModeReadWrite)
	if err != nil {
		return fmt.Errorf("opening %s: %w", filepath.Dir(path), err)
	}
	file, err := sandbox.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, sourceFilePermissions)
	if errors.Is(err, fs.ErrExist) {
		return errors.Join(usageError("%s already exists; %s never overwrites it", path, inv.program), sandbox.Close())
	}
	if err != nil {
		return errors.Join(fmt.Errorf("creating %s: %w", path, err), sandbox.Close())
	}
	_, writeErr := file.WriteString(content)
	return errors.Join(writeErr, file.Close(), sandbox.Close())
}

// checkMigrateArguments parses a migrate subcommand's arguments before connecting.
//
// Takes subcommand (string) which is up, down, to, status, validate, force or repair.
// Takes rest ([]string) which holds its arguments.
//
// Returns migrateArguments which holds the parsed version or step count.
// Returns error which is a usage error when they do not fit.
func checkMigrateArguments(subcommand string, rest []string) (migrateArguments, error) {
	arguments := migrateArguments{version: 0, steps: 1, hasVersion: false}
	switch subcommand {
	case "up", "status", "validate":
		if len(rest) > 0 {
			return arguments, usageError("migrate %s takes no arguments", subcommand)
		}
	case "down":
		if len(rest) > 1 {
			return arguments, usageError("migrate down takes at most one step count")
		}
		if len(rest) == 1 {
			steps, err := parsePositive(rest[0])
			arguments.steps = steps
			return arguments, err
		}
	case "to", subcommandForce:
		if len(rest) != 1 {
			return arguments, usageError("migrate %s needs one version", subcommand)
		}
		return versionArgument(subcommand, rest[0])
	case subcommandRepair:
		if len(rest) > 1 {
			return arguments, usageError("migrate repair takes at most one version")
		}
		if len(rest) == 1 {
			return versionArgument(subcommand, rest[0])
		}
	default:
		return arguments, usageError("unknown migrate subcommand %q; want up, down, to, status, validate, new, force or repair", subcommand)
	}
	return arguments, nil
}

// versionArgument parses a subcommand's version argument.
//
// Takes subcommand (string) which names the subcommand in the error.
// Takes argument (string) which is the version as given.
//
// Returns migrateArguments which holds the version.
// Returns error which is a usage error when it is not a number.
func versionArgument(subcommand, argument string) (migrateArguments, error) {
	version, err := strconv.ParseInt(argument, 10, 64)
	if err != nil {
		return migrateArguments{version: 0, steps: 1, hasVersion: false}, usageError("migrate %s: version %q is not a number", subcommand, argument)
	}
	return migrateArguments{version: version, steps: 1, hasVersion: true}, nil
}

// writeMigrationStatus prints one line per migration.
//
// Takes writer (io.Writer) which receives the table.
// Takes statuses ([]migrate.MigrationStatus) which are printed.
func writeMigrationStatus(writer io.Writer, statuses []migrate.MigrationStatus) {
	table := newTable(writer)
	fmt.Fprintln(table, "VERSION\tNAME\tSTATE\tAPPLIED")
	for _, status := range statuses {
		fmt.Fprintf(table, "%d\t%s\t%s\t%s\n", status.Version, status.Name, migrationState(status), appliedTime(status.Applied, status.AppliedAt))
	}
	_ = table.Flush()
}

// migrationState names a migration's state.
//
// Takes status (migrate.MigrationStatus) which is described.
//
// Returns string which is pending, applied, dirty or changed.
func migrationState(status migrate.MigrationStatus) string {
	switch {
	case !status.Applied:
		return "pending"
	case status.Dirty:
		return "dirty"
	case !status.ChecksumMatch:
		return "changed"
	default:
		return "applied"
	}
}

// appliedTime prints when something was applied.
//
// Takes applied (bool) which reports whether it was.
// Takes at (time.Time) which is when.
//
// Returns string which is the time, or "-".
func appliedTime(applied bool, at time.Time) string {
	if !applied || at.IsZero() {
		return notApplied
	}
	return at.UTC().Format(timeLayout)
}

// parsePositive parses a positive step count.
//
// Takes value (string) which is the argument.
//
// Returns int which is the count.
// Returns error which is a usage error when it is not a positive number.
func parsePositive(value string) (int, error) {
	count, err := strconv.Atoi(value)
	if err != nil || count <= 0 {
		return 0, usageError("%q is not a positive number of steps", value)
	}
	return count, nil
}

// nextVersion finds the version after the highest migration in a directory, and the digit
// count the existing files use.
//
// Takes directory (string) which holds the migrations; it need not exist.
//
// Returns int64 which is the migration version to create.
// Returns int which is the digit count to pad it to.
// Returns error when the directory cannot be read, holds more than maxDirectoryEntries
// entries, or its highest version is the largest a version can be.
func nextVersion(directory string) (int64, int, error) {
	entries, err := listDirectory(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return 1, defaultVersionWidth, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var highest int64
	width := defaultVersionWidth
	for _, entry := range entries {
		match := versionPrefix.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		version, parseErr := strconv.ParseInt(match[1], 10, 64)
		if parseErr != nil || version < highest {
			continue
		}
		highest, width = version, len(match[1])
	}
	if highest == math.MaxInt64 {
		return 0, 0, usageError("migration version %d in %s is the largest a version can be, so no migration can follow it", highest, directory)
	}
	return highest + 1, width, nil
}
